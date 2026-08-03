package inspectorca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/tunnel"
	"github.com/zalando/go-keyring"
)

const (
	keyringService = "KubeLoop Inspector"
	rootCAAccount  = "root-ca-v1"
	rootCommonName = "KubeLoop Inspector Root CA"
)

var ErrNotFound = errors.New("Inspector Root CA is not configured")

type SecretStore interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

type systemSecretStore struct{}

func (systemSecretStore) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

func (systemSecretStore) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}

func (systemSecretStore) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

type Manager struct {
	Secrets SecretStore
	Now     func() time.Time

	mu sync.Mutex
}

type Status struct {
	Present     bool      `json:"present"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	NotBefore   time.Time `json:"notBefore,omitempty"`
	NotAfter    time.Time `json:"notAfter,omitempty"`
}

type Root struct {
	CertificatePEM []byte
	PrivateKeyPEM  []byte
	Certificate    *x509.Certificate
	PrivateKey     *ecdsa.PrivateKey
}

type storedRoot struct {
	CertificatePEM string `json:"certificatePEM"`
	PrivateKeyPEM  string `json:"privateKeyPEM"`
}

func NewManager() *Manager {
	return &Manager{Secrets: systemSecretStore{}, Now: time.Now}
}

// EnsureRoot creates the Root CA only when it is absent. Callers must invoke
// this from an explicit user action because it persists a private key.
func (m *Manager) EnsureRoot() (Root, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	root, err := m.load()
	if err == nil {
		return root, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Root{}, err
	}
	root, err = generateRoot(m.now())
	if err != nil {
		return Root{}, err
	}
	value, err := json.Marshal(storedRoot{
		CertificatePEM: string(root.CertificatePEM),
		PrivateKeyPEM:  string(root.PrivateKeyPEM),
	})
	if err != nil {
		return Root{}, fmt.Errorf("encode Inspector Root CA: %w", err)
	}
	if err := m.secretStore().Set(keyringService, rootCAAccount, string(value)); err != nil {
		return Root{}, fmt.Errorf("store Inspector Root CA in OS keyring: %w", err)
	}
	return root, nil
}

func (m *Manager) LoadRoot() (Root, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.load()
}

func (m *Manager) Status() (Status, error) {
	root, err := m.LoadRoot()
	if errors.Is(err, ErrNotFound) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return Status{
		Present:     true,
		Fingerprint: Fingerprint(root.Certificate),
		NotBefore:   root.Certificate.NotBefore,
		NotAfter:    root.Certificate.NotAfter,
	}, nil
}

func (m *Manager) DeleteRoot() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.secretStore().Delete(keyringService, rootCAAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete Inspector Root CA from OS keyring: %w", err)
	}
	return nil
}

func (m *Manager) IssueIntermediate(
	sessionID string, upstreamCAPEM []byte,
) (tunnel.InspectorTLSConfig, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 256 {
		return tunnel.InspectorTLSConfig{}, errors.New("Inspector session ID length is invalid")
	}
	root, err := m.LoadRoot()
	if err != nil {
		return tunnel.InspectorTLSConfig{}, err
	}
	now := m.now()
	notAfter := now.Add(24 * time.Hour)
	if root.Certificate.NotAfter.Before(notAfter) {
		notAfter = root.Certificate.NotAfter
	}
	if !notAfter.After(now) {
		return tunnel.InspectorTLSConfig{}, errors.New("Inspector Root CA is expired")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tunnel.InspectorTLSConfig{}, fmt.Errorf("generate Inspector Intermediate key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return tunnel.InspectorTLSConfig{}, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"KubeLoop"},
			CommonName:   "KubeLoop Inspector Session " + sessionID,
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(
		rand.Reader, template, root.Certificate, &key.PublicKey, root.PrivateKey,
	)
	if err != nil {
		return tunnel.InspectorTLSConfig{}, fmt.Errorf("sign Inspector Intermediate CA: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tunnel.InspectorTLSConfig{}, fmt.Errorf("encode Inspector Intermediate key: %w", err)
	}
	return tunnel.InspectorTLSConfig{
		CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		PrivateKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		ChainPEM:       append([]byte(nil), root.CertificatePEM...),
		UpstreamCAPEM:  append([]byte(nil), upstreamCAPEM...),
	}, nil
}

func Fingerprint(certificate *x509.Certificate) string {
	if certificate == nil {
		return ""
	}
	sum := sha256.Sum256(certificate.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func (m *Manager) load() (Root, error) {
	value, err := m.secretStore().Get(keyringService, rootCAAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return Root{}, ErrNotFound
	}
	if err != nil {
		return Root{}, fmt.Errorf("read Inspector Root CA from OS keyring: %w", err)
	}
	var stored storedRoot
	if err := json.Unmarshal([]byte(value), &stored); err != nil {
		return Root{}, fmt.Errorf("decode Inspector Root CA: %w", err)
	}
	return parseRoot([]byte(stored.CertificatePEM), []byte(stored.PrivateKeyPEM))
}

func (m *Manager) secretStore() SecretStore {
	if m.Secrets != nil {
		return m.Secrets
	}
	return systemSecretStore{}
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func generateRoot(now time.Time) (Root, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Root{}, fmt.Errorf("generate Inspector Root CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return Root{}, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"KubeLoop"},
			CommonName:   rootCommonName,
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return Root{}, fmt.Errorf("create Inspector Root CA: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return Root{}, fmt.Errorf("encode Inspector Root CA key: %w", err)
	}
	return parseRoot(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	)
}

func parseRoot(certificatePEM, privateKeyPEM []byte) (Root, error) {
	certificateBlock, rest := pem.Decode(certificatePEM)
	if certificateBlock == nil || len(rest) != 0 || certificateBlock.Type != "CERTIFICATE" {
		return Root{}, errors.New("Inspector Root CA certificate PEM is invalid")
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil {
		return Root{}, fmt.Errorf("parse Inspector Root CA certificate: %w", err)
	}
	if !certificate.IsCA || certificate.Subject.CommonName != rootCommonName {
		return Root{}, errors.New("stored Inspector Root CA identity is invalid")
	}
	keyBlock, rest := pem.Decode(privateKeyPEM)
	if keyBlock == nil || len(rest) != 0 || keyBlock.Type != "PRIVATE KEY" {
		return Root{}, errors.New("Inspector Root CA private key PEM is invalid")
	}
	value, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return Root{}, fmt.Errorf("parse Inspector Root CA private key: %w", err)
	}
	key, ok := value.(*ecdsa.PrivateKey)
	if !ok || !key.PublicKey.Equal(certificate.PublicKey) {
		return Root{}, errors.New("Inspector Root CA private key does not match certificate")
	}
	return Root{
		CertificatePEM: append([]byte(nil), certificatePEM...),
		PrivateKeyPEM:  append([]byte(nil), privateKeyPEM...),
		Certificate:    certificate,
		PrivateKey:     key,
	}, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	if serial.Sign() == 0 {
		return big.NewInt(1), nil
	}
	return serial, nil
}
