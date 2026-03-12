package proxy

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/netip"
	"testing"

	"claude-gateway/internal/netutil"
)

type staticResolver map[string][]netip.Addr

func (r staticResolver) Resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, ok := r[host]
	if !ok {
		return nil, fmt.Errorf("unexpected host %q", host)
	}
	cloned := make([]netip.Addr, len(ips))
	copy(cloned, ips)
	return cloned, nil
}

func TestPolicyResolveHTTPAllowsExactAndSuffixHosts(t *testing.T) {
	policy := Policy{
		AllowedPorts: map[uint16]struct{}{
			443:  {},
			8443: {},
		},
		HostMatcher: netutil.NewHostMatcher(
			map[string]struct{}{"api.allowed.example": {}},
			[]string{".svc.allowed.example"},
		),
		Resolver: staticResolver{
			"api.allowed.example":         {netip.MustParseAddr("8.8.8.8")},
			"edge.svc.allowed.example":    {netip.MustParseAddr("8.8.4.4")},
		},
	}

	exactRequest := httptest.NewRequest("GET", "https://api.allowed.example/v1/messages", nil)
	resolution, err := policy.ResolveHTTP(context.Background(), exactRequest)
	if err != nil {
		t.Fatalf("ResolveHTTP(exact) error = %v", err)
	}
	if resolution.Destination.Host != "api.allowed.example" {
		t.Fatalf("exact host = %q, want %q", resolution.Destination.Host, "api.allowed.example")
	}

	suffixRequest := httptest.NewRequest("GET", "https://edge.svc.allowed.example:8443/stream", nil)
	resolution, err = policy.ResolveHTTP(context.Background(), suffixRequest)
	if err != nil {
		t.Fatalf("ResolveHTTP(suffix) error = %v", err)
	}
	if resolution.Destination.Port != 8443 {
		t.Fatalf("suffix port = %d, want %d", resolution.Destination.Port, 8443)
	}
}

func TestPolicyResolveRejectsPortAndReservedDestination(t *testing.T) {
	policy := Policy{
		AllowedPorts: map[uint16]struct{}{
			443: {},
		},
		HostMatcher: netutil.NewHostMatcher(
			map[string]struct{}{"api.allowed.example": {}},
			nil,
		),
		Resolver: staticResolver{
			"api.allowed.example": {netip.MustParseAddr("127.0.0.1")},
		},
	}

	disallowedPortRequest := httptest.NewRequest("GET", "https://api.allowed.example:8443/v1/messages", nil)
	if _, err := policy.ResolveHTTP(context.Background(), disallowedPortRequest); err == nil || err.Category != CategoryDestinationDenied {
		t.Fatalf("ResolveHTTP(disallowed port) error = %v, want destination denied", err)
	}

	allowedPortRequest := httptest.NewRequest("GET", "https://api.allowed.example/v1/messages", nil)
	if _, err := policy.ResolveHTTP(context.Background(), allowedPortRequest); err == nil || err.Category != CategoryDestinationDenied {
		t.Fatalf("ResolveHTTP(reserved IP) error = %v, want destination denied", err)
	}
}

func TestPolicyResolveCONNECTParsesAuthorityForm(t *testing.T) {
	policy := Policy{
		AllowedPorts: map[uint16]struct{}{
			443: {},
		},
		HostMatcher: netutil.NewHostMatcher(
			map[string]struct{}{"api.allowed.example": {}},
			nil,
		),
		Resolver: staticResolver{
			"api.allowed.example": {netip.MustParseAddr("8.8.8.8")},
		},
	}

	resolution, err := policy.ResolveCONNECT(context.Background(), "api.allowed.example:443")
	if err != nil {
		t.Fatalf("ResolveCONNECT() error = %v", err)
	}
	if resolution.Destination.Host != "api.allowed.example" || resolution.Destination.Port != 443 {
		t.Fatalf("destination = %#v, want host api.allowed.example port 443", resolution.Destination)
	}
}
