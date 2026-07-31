package singbox

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// ReverseZones builds split-DNS reverse zones for cluster Pod/Service addresses.
// Zones are octet- or nibble-aligned so PTR queries for cluster IPs reach CoreDNS
// without hijacking the entire in-addr.arpa / ip6.arpa tree.
func ReverseZones(cidrs ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(zone string) {
		zone = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone)), ".")
		if zone == "" {
			return
		}
		if _, ok := seen[zone]; ok {
			return
		}
		seen[zone] = struct{}{}
		out = append(out, zone)
	}
	for _, group := range cidrs {
		for _, raw := range group {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if prefix, err := netip.ParsePrefix(raw); err == nil {
				for _, zone := range reverseZonesForPrefix(prefix) {
					add(zone)
				}
				continue
			}
			if addr, err := netip.ParseAddr(raw); err == nil {
				for _, zone := range reverseZonesForPrefix(netip.PrefixFrom(addr, addr.BitLen())) {
					add(zone)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func reverseZonesForPrefix(prefix netip.Prefix) []string {
	prefix = prefix.Masked()
	if prefix.Addr().Is4() {
		return reverseZonesIPv4(prefix)
	}
	if prefix.Addr().Is6() {
		return reverseZonesIPv6(prefix)
	}
	return nil
}

func reverseZonesIPv4(prefix netip.Prefix) []string {
	bits := prefix.Bits()
	if bits <= 0 || bits > 32 {
		return nil
	}
	// Round up to the next octet boundary and enumerate that many prefixes.
	aligned := bits
	if aligned%8 != 0 {
		aligned = ((aligned / 8) + 1) * 8
	}
	octets := aligned / 8
	step := uint32(1) << (32 - aligned)
	start := ipv4ToUint(prefix.Addr())
	end := start + (uint32(1) << (32 - bits))
	out := make([]string, 0, int((end-start)/step))
	for cur := start; cur < end; cur += step {
		addr := uintToIPv4(cur)
		b := addr.As4()
		parts := make([]string, 0, octets+1)
		for i := octets - 1; i >= 0; i-- {
			parts = append(parts, fmt.Sprintf("%d", b[i]))
		}
		parts = append(parts, "in-addr.arpa")
		out = append(out, strings.Join(parts, "."))
	}
	return out
}

func reverseZonesIPv6(prefix netip.Prefix) []string {
	bits := prefix.Bits()
	if bits <= 0 || bits > 128 {
		return nil
	}
	aligned := bits
	if aligned%4 != 0 {
		aligned = ((aligned / 4) + 1) * 4
	}
	// Avoid claiming all of ip6.arpa; keep a single covering zone for short prefixes.
	if aligned < 16 {
		aligned = 16
	}
	return []string{ipv6ReverseZone(prefix.Addr(), aligned)}
}

func ipv6ReverseZone(addr netip.Addr, bits int) string {
	if !addr.Is6() || bits <= 0 || bits > 128 || bits%4 != 0 {
		return ""
	}
	nibbles := bits / 4
	b := addr.As16()
	hex := make([]byte, 0, 32)
	for _, v := range b {
		hex = append(hex, "0123456789abcdef"[v>>4], "0123456789abcdef"[v&0x0f])
	}
	parts := make([]string, 0, nibbles+1)
	for i := nibbles - 1; i >= 0; i-- {
		parts = append(parts, string(hex[i]))
	}
	parts = append(parts, "ip6.arpa")
	return strings.Join(parts, ".")
}

func ipv4ToUint(addr netip.Addr) uint32 {
	b := addr.As4()
	return binary.BigEndian.Uint32(b[:])
}

func uintToIPv4(v uint32) netip.Addr {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	addr, _ := netip.AddrFromSlice(b[:])
	return addr
}
