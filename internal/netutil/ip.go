package netutil

import (
	"errors"
	"net"
	"net/netip"
	"strings"
)

var reservedPrefixes = mustPrefixes(
	"0.0.0.0/8",
	"100.64.0.0/10",
	"169.254.0.0/16",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"224.0.0.0/4",
	"240.0.0.0/4",
	"::/128",
	"100::/64",
	"2001:2::/48",
	"2001:db8::/32",
	"fc00::/7",
	"fe80::/10",
	"ff00::/8",
)

type PrefixMatcher struct {
	prefixes []netip.Prefix
}

func NewPrefixMatcher(prefixes []netip.Prefix) PrefixMatcher {
	cloned := make([]netip.Prefix, len(prefixes))
	copy(cloned, prefixes)
	return PrefixMatcher{prefixes: cloned}
}

func (m PrefixMatcher) Allow(addr netip.Addr) bool {
	if len(m.prefixes) == 0 {
		return true
	}
	for _, prefix := range m.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

type HostMatcher struct {
	exact    map[string]struct{}
	suffixes []string
}

func NewHostMatcher(exact map[string]struct{}, suffixes []string) HostMatcher {
	normalized := make(map[string]struct{}, len(exact))
	for host := range exact {
		normalized[NormalizeHost(host)] = struct{}{}
	}
	copiedSuffixes := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		value := NormalizeHost(suffix)
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		copiedSuffixes = append(copiedSuffixes, value)
	}
	return HostMatcher{
		exact:    normalized,
		suffixes: copiedSuffixes,
	}
}

func (m HostMatcher) Match(host string) bool {
	host = NormalizeHost(host)
	if host == "" {
		return false
	}
	if len(m.exact) == 0 && len(m.suffixes) == 0 {
		return true
	}
	if _, ok := m.exact[host]; ok {
		return true
	}
	for _, suffix := range m.suffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func NormalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.Trim(host, "[]")
	return strings.TrimSuffix(host, ".")
}

func ParseRemoteIP(remoteAddr string) (netip.Addr, error) {
	addrPort, err := netip.ParseAddrPort(remoteAddr)
	if err == nil {
		return addrPort.Addr().Unmap(), nil
	}

	host, _, splitErr := net.SplitHostPort(remoteAddr)
	if splitErr != nil {
		return netip.Addr{}, errors.New("invalid remote address")
	}
	ip, parseErr := netip.ParseAddr(host)
	if parseErr != nil {
		return netip.Addr{}, parseErr
	}
	return ip.Unmap(), nil
}

func IsReservedAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return true
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func IsPublicBindHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return true
	case "localhost":
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() {
			return false
		}
		return true
	}
	return true
}

func mustPrefixes(values ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			panic(err)
		}
		out = append(out, prefix)
	}
	return out
}
