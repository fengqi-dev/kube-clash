package singbox

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDNSSearchProxyUDPAndTCP(t *testing.T) {
	upstreamAddr := startTestDNSUpstream(t)

	publicTCP, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	publicPort := publicTCP.Addr().(*net.TCPAddr).Port
	_ = publicTCP.Close()

	host, upstreamHost, upstreamPort := "127.0.0.1", "127.0.0.1", upstreamAddr.Port
	proxy, err := startDNSSearchProxy(
		host, publicPort, upstreamHost, upstreamPort,
		SearchDomains("default"), "cluster.local",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxy.Close() }()

	req := new(dns.Msg)
	req.SetQuestion("kubernetes.default.svc.cluster.local.", dns.TypeA)
	target := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", publicPort))

	udpClient := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	resp, _, err := udpClient.Exchange(req, target)
	if err != nil {
		t.Fatalf("udp query: %v", err)
	}
	if len(resp.Answer) == 0 {
		t.Fatalf("udp empty answer: %#v", resp)
	}

	tcpClient := &dns.Client{Net: "tcp", Timeout: 2 * time.Second}
	resp, _, err = tcpClient.Exchange(req, target)
	if err != nil {
		t.Fatalf("tcp query: %v", err)
	}
	if len(resp.Answer) == 0 {
		t.Fatalf("tcp empty answer: %#v", resp)
	}
}

func startTestDNSUpstream(t *testing.T) *net.UDPAddr {
	t.Helper()
	handler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.Answer = []dns.RR{
			&dns.A{
				Hdr: dns.RR_Header{
					Name: r.Question[0].Name, Rrtype: dns.TypeA,
					Class: dns.ClassINET, Ttl: 30,
				},
				A: net.ParseIP("10.96.0.1").To4(),
			},
		}
		_ = w.WriteMsg(msg)
	})
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().(*net.UDPAddr)
	udpServer := &dns.Server{PacketConn: pc, Handler: handler}
	go func() { _ = udpServer.ActivateAndServe() }()
	t.Cleanup(func() { _ = udpServer.Shutdown() })

	tcpServer := &dns.Server{
		Addr: addr.String(), Net: "tcp", Handler: handler,
	}
	go func() { _ = tcpServer.ListenAndServe() }()
	t.Cleanup(func() { _ = tcpServer.Shutdown() })
	time.Sleep(30 * time.Millisecond)
	return addr
}
