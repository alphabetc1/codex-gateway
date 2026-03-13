package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"codex-gateway/internal/netutil"
)

const (
	CategoryAuthFailed       = "auth_failed"
	CategorySourceDenied     = "source_ip_denied"
	CategoryConcurrencyLimit = "concurrency_limited"
	CategoryDestinationDenied = "destination_denied"
	CategoryUpstreamDialFailed = "upstream_dial_failed"
	CategoryUpstreamTimeout  = "upstream_timeout"
	CategoryBadRequest       = "bad_request"
	CategoryTunnelIO         = "tunnel_io_error"
	CategoryInternal         = "internal_error"
)

type HandlerError struct {
	Status   int
	Category string
	Message  string
	Err      error
}

func (e *HandlerError) Error() string {
	if e.Err == nil {
		return e.Message
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *HandlerError) Unwrap() error {
	return e.Err
}

type Destination struct {
	Scheme string
	Host   string
	Port   uint16
}

func (d Destination) Authority() string {
	return net.JoinHostPort(d.Host, strconv.Itoa(int(d.Port)))
}

type Resolution struct {
	Destination Destination
	Resolved    []netip.Addr
	Selected    netip.Addr
}

func (r Resolution) DialAddress() string {
	return net.JoinHostPort(r.Selected.String(), strconv.Itoa(int(r.Destination.Port)))
}

type Policy struct {
	AllowedPorts map[uint16]struct{}
	HostMatcher  netutil.HostMatcher
	Resolver     netutil.Resolver
	AllowPrivate bool
}

func (p Policy) ResolveHTTP(ctx context.Context, r *http.Request) (Resolution, *HandlerError) {
	if r.URL == nil {
		return Resolution{}, badRequest("request URL is missing", nil)
	}

	scheme := strings.ToLower(strings.TrimSpace(r.URL.Scheme))
	if scheme != "http" && scheme != "https" {
		return Resolution{}, badRequest("unsupported proxy scheme", nil)
	}

	host := netutil.NormalizeHost(r.URL.Hostname())
	if host == "" {
		return Resolution{}, badRequest("absolute-form request host is missing", nil)
	}

	port, err := effectivePort(r.URL.Port(), scheme)
	if err != nil {
		return Resolution{}, badRequest("request port is invalid", err)
	}

	return p.resolve(ctx, Destination{
		Scheme: scheme,
		Host:   host,
		Port:   port,
	})
}

func (p Policy) ResolveCONNECT(ctx context.Context, authority string) (Resolution, *HandlerError) {
	host, port, err := parseAuthority(authority)
	if err != nil {
		return Resolution{}, badRequest("CONNECT target is invalid", err)
	}
	return p.resolve(ctx, Destination{
		Scheme: "tcp",
		Host:   host,
		Port:   port,
	})
}

func (p Policy) resolve(ctx context.Context, destination Destination) (Resolution, *HandlerError) {
	if _, ok := p.AllowedPorts[destination.Port]; !ok {
		return Resolution{}, destinationDenied(fmt.Sprintf("destination port %d is not allowed", destination.Port), nil)
	}

	if !p.HostMatcher.Match(destination.Host) {
		return Resolution{}, destinationDenied("destination host is not allowed", nil)
	}

	ips, err := p.Resolver.Resolve(ctx, destination.Host)
	if err != nil {
		return Resolution{}, upstreamDialFailed("destination resolution failed", err)
	}

	if len(ips) == 0 {
		return Resolution{}, upstreamDialFailed("destination resolution returned no addresses", nil)
	}

	if !p.AllowPrivate {
		for _, addr := range ips {
			if netutil.IsReservedAddr(addr) {
				return Resolution{}, destinationDenied("destination resolved to a private or reserved address", nil)
			}
		}
	}

	return Resolution{
		Destination: destination,
		Resolved:    ips,
		Selected:    ips[0],
	}, nil
}

func effectivePort(rawPort, scheme string) (uint16, error) {
	if rawPort == "" {
		switch scheme {
		case "http":
			return 80, nil
		case "https":
			return 443, nil
		default:
			return 0, errors.New("unknown scheme")
		}
	}
	value, err := strconv.Atoi(rawPort)
	if err != nil || value <= 0 || value > 65535 {
		return 0, errors.New("invalid port")
	}
	return uint16(value), nil
}

func parseAuthority(authority string) (string, uint16, error) {
	host, rawPort, err := net.SplitHostPort(strings.TrimSpace(authority))
	if err != nil {
		return "", 0, err
	}
	host = netutil.NormalizeHost(host)
	if host == "" {
		return "", 0, errors.New("host is empty")
	}
	value, err := strconv.Atoi(rawPort)
	if err != nil || value <= 0 || value > 65535 {
		return "", 0, errors.New("invalid port")
	}
	return host, uint16(value), nil
}

func badRequest(message string, err error) *HandlerError {
	return &HandlerError{Status: http.StatusBadRequest, Category: CategoryBadRequest, Message: message, Err: err}
}

func destinationDenied(message string, err error) *HandlerError {
	return &HandlerError{Status: http.StatusForbidden, Category: CategoryDestinationDenied, Message: message, Err: err}
}

func upstreamDialFailed(message string, err error) *HandlerError {
	return &HandlerError{Status: http.StatusBadGateway, Category: CategoryUpstreamDialFailed, Message: message, Err: err}
}

func upstreamTimeout(message string, err error) *HandlerError {
	return &HandlerError{Status: http.StatusGatewayTimeout, Category: CategoryUpstreamTimeout, Message: message, Err: err}
}
