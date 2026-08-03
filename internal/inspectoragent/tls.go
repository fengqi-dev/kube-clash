package inspectoragent

import (
	"bufio"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
)

type tlsAuthority struct {
	certificate *x509.Certificate
	privateKey  crypto.Signer
	upstreamCAs *x509.CertPool

	mu     sync.Mutex
	leaves map[string]*tls.Certificate
}

type tlsFlowMetadata struct {
	ServerName       string `json:"serverName"`
	Version          string `json:"version"`
	CipherSuite      string `json:"cipherSuite"`
	ALPN             string `json:"alpn,omitempty"`
	UpstreamVerified bool   `json:"upstreamVerified"`
	UpstreamSubject  string `json:"upstreamSubject,omitempty"`
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(value []byte) (int, error) {
	return c.reader.Read(value)
}

func newTLSAuthority(config *tunnel.InspectorTLSConfig) (*tlsAuthority, error) {
	if config == nil {
		return nil, nil
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	certificateBlock, _ := pem.Decode(config.CertificatePEM)
	if certificateBlock == nil || certificateBlock.Type != "CERTIFICATE" {
		return nil, errors.New("Intermediate certificate PEM is invalid")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Intermediate certificate: %w", err)
	}
	if !certificate.IsCA {
		return nil, errors.New("Inspector TLS certificate is not an Intermediate CA")
	}
	chainRoots := x509.NewCertPool()
	if !chainRoots.AppendCertsFromPEM(config.ChainPEM) {
		return nil, errors.New("Inspector CA chain PEM is invalid")
	}
	if _, err := certificate.Verify(x509.VerifyOptions{
		Roots:     chainRoots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return nil, fmt.Errorf("verify Intermediate certificate chain: %w", err)
	}
	keyBlock, _ := pem.Decode(config.PrivateKeyPEM)
	if keyBlock == nil || keyBlock.Type != "PRIVATE KEY" {
		return nil, errors.New("Intermediate private key PEM is invalid")
	}
	value, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Intermediate private key: %w", err)
	}
	privateKey, ok := value.(crypto.Signer)
	if !ok || !publicKeysEqual(privateKey.Public(), certificate.PublicKey) {
		return nil, errors.New("Intermediate private key does not match certificate")
	}
	upstreamCAs, err := x509.SystemCertPool()
	if err != nil || upstreamCAs == nil {
		upstreamCAs = x509.NewCertPool()
	}
	if len(config.UpstreamCAPEM) > 0 &&
		!upstreamCAs.AppendCertsFromPEM(config.UpstreamCAPEM) {
		return nil, errors.New("upstream CA PEM is invalid")
	}
	return &tlsAuthority{
		certificate: certificate,
		privateKey:  privateKey,
		upstreamCAs: upstreamCAs,
		leaves:      make(map[string]*tls.Certificate),
	}, nil
}

func serveHTTPSConnection(
	session *agentSession,
	client net.Conn,
	clientReader *bufio.Reader,
	upstream net.Conn,
	target tunnel.InspectorTarget,
) {
	upstreamTLS, metadata, err := prepareHTTPSUpstream(session, upstream, target)
	if err != nil {
		return
	}
	serveHTTPSClient(session, client, clientReader, upstreamTLS, target, metadata)
}

func prepareHTTPSUpstream(
	session *agentSession,
	upstream net.Conn,
	target tunnel.InspectorTarget,
) (*tls.Conn, *tlsFlowMetadata, error) {
	if session.tls == nil {
		return nil, nil, errors.New("Inspector TLS authority is unavailable")
	}
	timeout := 10 * time.Second
	_ = upstream.SetDeadline(time.Now().Add(timeout))
	upstreamTLS := tls.Client(upstream, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: target.Host,
		RootCAs:    session.tls.upstreamCAs,
		NextProtos: []string{"http/1.1"},
	})
	if err := upstreamTLS.Handshake(); err != nil {
		return nil, nil, fmt.Errorf("verify upstream TLS: %w", err)
	}
	_ = upstream.SetDeadline(time.Time{})

	upstreamState := upstreamTLS.ConnectionState()
	metadata := &tlsFlowMetadata{
		ServerName:       target.Host,
		UpstreamVerified: len(upstreamState.VerifiedChains) > 0,
	}
	if len(upstreamState.PeerCertificates) > 0 {
		metadata.UpstreamSubject = upstreamState.PeerCertificates[0].Subject.String()
	}
	return upstreamTLS, metadata, nil
}

func serveHTTPSClient(
	session *agentSession,
	client net.Conn,
	clientReader *bufio.Reader,
	upstreamTLS *tls.Conn,
	target tunnel.InspectorTarget,
	metadata *tlsFlowMetadata,
) {
	timeout := 10 * time.Second
	leaf, err := session.tls.leaf(target.Host)
	if err != nil {
		return
	}
	buffered := &bufferedConn{Conn: client, reader: clientReader}
	_ = client.SetDeadline(time.Now().Add(timeout))
	clientTLS := tls.Server(buffered, &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{*leaf},
		NextProtos:   []string{"http/1.1"},
	})
	if err := clientTLS.Handshake(); err != nil {
		return
	}
	_ = client.SetDeadline(time.Time{})

	clientState := clientTLS.ConnectionState()
	metadata.Version = tlsVersionName(clientState.Version)
	metadata.CipherSuite = tls.CipherSuiteName(clientState.CipherSuite)
	metadata.ALPN = clientState.NegotiatedProtocol
	serveHTTPConnection(
		session, clientTLS, bufio.NewReader(clientTLS), upstreamTLS, target, metadata,
	)
}

func (a *tlsAuthority) leaf(host string) (*tls.Certificate, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cached := a.leaves[host]; cached != nil {
		return cached, nil
	}
	now := time.Now().UTC()
	notAfter := now.Add(24 * time.Hour)
	if a.certificate.NotAfter.Before(notAfter) {
		notAfter = a.certificate.NotAfter
	}
	if !notAfter.After(now) {
		return nil, errors.New("Inspector Intermediate CA is expired")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate leaf key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate leaf serial: %w", err)
	}
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{"KubeLoop Inspector"}, CommonName: host},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(
		rand.Reader, template, a.certificate, &key.PublicKey, a.privateKey,
	)
	if err != nil {
		return nil, fmt.Errorf("sign leaf certificate: %w", err)
	}
	certificate := &tls.Certificate{
		Certificate: [][]byte{der, a.certificate.Raw},
		PrivateKey:  key,
		Leaf:        template,
	}
	a.leaves[host] = certificate
	return certificate, nil
}

func publicKeysEqual(left, right any) bool {
	leftKey, leftOK := left.(*ecdsa.PublicKey)
	rightKey, rightOK := right.(*ecdsa.PublicKey)
	return leftOK && rightOK && leftKey.Equal(rightKey)
}

func tlsVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	default:
		return fmt.Sprintf("0x%04x", version)
	}
}
