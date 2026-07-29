package singbox

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// dnsSearchProxy accepts OS DNS queries on the public split-DNS port, appends
// Kubernetes search suffixes when needed, and forwards to sing-box dns-in.
//
// On macOS, networksetup search domains expand the name and then query the
// primary resolver (e.g. 114.114.114.114), so short names never hit
// /etc/resolver/cluster.local. Matching *.svc via /etc/resolver/svc and
// expanding here makes names like static-web.default.svc work.
type dnsSearchProxy struct {
	public   *dns.Server
	upstream string
	search   []string
	client   *dns.Client

	mu     sync.Mutex
	closed bool
}

func startDNSSearchProxy(publicHost string, publicPort int, upstreamHost string, upstreamPort int, search []string) (*dnsSearchProxy, error) {
	if publicHost == "" {
		publicHost = DefaultDNSListen
	}
	if upstreamHost == "" {
		upstreamHost = DefaultDNSListen
	}
	proxy := &dnsSearchProxy{
		upstream: net.JoinHostPort(upstreamHost, fmt.Sprintf("%d", upstreamPort)),
		search:   append([]string(nil), search...),
		client:   &dns.Client{Net: "udp", Timeout: 3 * time.Second, UDPSize: 1232},
	}
	addr := net.JoinHostPort(publicHost, fmt.Sprintf("%d", publicPort))
	proxy.public = &dns.Server{
		Addr:    addr,
		Net:     "udp",
		Handler: dns.HandlerFunc(proxy.serveDNS),
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- proxy.public.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if err != nil {
			return nil, fmt.Errorf("listen DNS search proxy on %s: %w", addr, err)
		}
	case <-time.After(150 * time.Millisecond):
	}
	return proxy, nil
}

func (p *dnsSearchProxy) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.public == nil {
		return nil
	}
	return p.public.Shutdown()
}

func (p *dnsSearchProxy) serveDNS(w dns.ResponseWriter, req *dns.Msg) {
	if req == nil || len(req.Question) == 0 {
		_ = w.WriteMsg(new(dns.Msg).SetRcode(req, dns.RcodeFormatError))
		return
	}
	original := req.Question[0].Name
	candidates := dnsSearchCandidates(original, p.search)
	var last *dns.Msg
	for _, candidate := range candidates {
		forward := req.Copy()
		forward.Id = dns.Id()
		forward.Question[0].Name = candidate
		resp, _, err := p.client.Exchange(forward, p.upstream)
		if err != nil || resp == nil {
			continue
		}
		last = resp
		if resp.Rcode != dns.RcodeSuccess {
			continue
		}
		if len(resp.Answer) == 0 && candidate != candidates[0] {
			// NODATA on an expanded name — keep trying other suffixes for A/AAAA.
			continue
		}
		out := resp.Copy()
		out.Id = req.Id
		rewriteDNSNames(out, candidate, original)
		_ = w.WriteMsg(out)
		return
	}
	if last != nil {
		out := last.Copy()
		out.Id = req.Id
		rewriteDNSNames(out, last.Question[0].Name, original)
		_ = w.WriteMsg(out)
		return
	}
	nx := new(dns.Msg)
	nx.SetReply(req)
	nx.Rcode = dns.RcodeServerFailure
	_ = w.WriteMsg(nx)
}

func dnsSearchCandidates(qname string, search []string) []string {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(qname)), ".")
	if name == "" {
		return nil
	}
	original := name + "."
	if strings.HasSuffix(name, ".cluster.local") {
		return []string{original}
	}
	out := []string{original}
	seen := map[string]struct{}{original: {}}
	for _, suffix := range search {
		suffix = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(suffix)), ".")
		if suffix == "" {
			continue
		}
		candidate := name + "." + suffix + "."
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func rewriteDNSNames(msg *dns.Msg, from, to string) {
	if msg == nil || from == "" || to == "" || equalDNSName(from, to) {
		return
	}
	for i := range msg.Question {
		if equalDNSName(msg.Question[i].Name, from) {
			msg.Question[i].Name = to
		}
	}
	rewriteRRNames(msg.Answer, from, to)
	rewriteRRNames(msg.Ns, from, to)
	rewriteRRNames(msg.Extra, from, to)
}

func rewriteRRNames(records []dns.RR, from, to string) {
	for _, rr := range records {
		if rr == nil {
			continue
		}
		hdr := rr.Header()
		if equalDNSName(hdr.Name, from) {
			hdr.Name = to
		}
	}
}

func equalDNSName(left, right string) bool {
	return strings.EqualFold(strings.TrimSuffix(left, "."), strings.TrimSuffix(right, "."))
}
