package netutil

import (
	"context"
	"fmt"
	"net"
	"net/netip"
)

type Resolver interface {
	Resolve(ctx context.Context, host string) ([]netip.Addr, error)
}

type NetResolver struct {
	Resolver *net.Resolver
}

func (r NetResolver) Resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(NormalizeHost(host)); err == nil {
		return []netip.Addr{addr.Unmap()}, nil
	}

	resolver := r.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}

	ips, err := resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("lookup %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("lookup %s: no records", host)
	}

	out := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		out = append(out, addr.Unmap())
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("lookup %s: no usable IPs", host)
	}
	return out, nil
}
