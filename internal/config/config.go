package config

import (
	"codex-gateway/internal/claudeoauth"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultProxyListenAddr               = "127.0.0.1"
	defaultProxyListenPort               = 8080
	defaultAdminListenAddr               = "127.0.0.1"
	defaultAdminListenPort               = 9090
	defaultMaxConnsPerIP                 = 128
	defaultReadHeaderTimeout             = 10 * time.Second
	defaultIdleTimeout                   = 90 * time.Second
	defaultUpstreamDialTimeout           = 10 * time.Second
	defaultUpstreamTLSHandshakeTimeout   = 10 * time.Second
	defaultUpstreamResponseHeaderTimeout = 30 * time.Second
	defaultTunnelIdleTimeout             = 10 * time.Minute
	defaultMaxHeaderBytes                = 1 << 20
)

type Config struct {
	ProxyListenAddr string
	ProxyListenPort int
	AdminListenAddr string
	AdminListenPort int

	ProxyTLSEnabled  bool
	ProxyTLSCertFile string
	ProxyTLSKeyFile  string

	AuthUsersFile string
	AuthUsers     string

	SourceAllowlist []netip.Prefix
	DestPorts       map[uint16]struct{}
	DestHosts       map[string]struct{}
	DestSuffixes    []string

	AllowPrivateDestinations bool
	MaxConnsPerIP            int

	ServerReadHeaderTimeout       time.Duration
	ServerIdleTimeout             time.Duration
	UpstreamDialTimeout           time.Duration
	UpstreamTLSHandshakeTimeout   time.Duration
	UpstreamResponseHeaderTimeout time.Duration
	TunnelIdleTimeout             time.Duration
	MaxHeaderBytes                int

	LogLevel          string
	LogFormat         string
	AccessLogEnabled  bool
	MetricsEnabled    bool
	AllowPublicAdmin  bool
	AllowEmptyDestACL bool

	AllowInsecurePublicProxy bool

	ClaudeOAuthEnabled      bool
	ClaudeOAuthRefreshToken string
	ClaudeOAuthClientID     string
	ClaudeOAuthScopes       []string
	ClaudeOAuthTokenURL     string
}

func LoadFromEnv() (Config, error) {
	return loadFromLookup(func(key string) string {
		return os.Getenv(key)
	})
}

func LoadFromMap(values map[string]string) (Config, error) {
	return loadFromLookup(func(key string) string {
		return values[key]
	})
}

func loadFromLookup(lookup func(string) string) (Config, error) {
	cfg := Config{
		ProxyListenAddr:               getenvDefault(lookup, "PROXY_LISTEN_ADDR", defaultProxyListenAddr),
		ProxyListenPort:               getenvInt(lookup, "PROXY_LISTEN_PORT", defaultProxyListenPort),
		AdminListenAddr:               getenvDefault(lookup, "ADMIN_LISTEN_ADDR", defaultAdminListenAddr),
		AdminListenPort:               getenvInt(lookup, "ADMIN_LISTEN_PORT", defaultAdminListenPort),
		ProxyTLSEnabled:               getenvBool(lookup, "PROXY_TLS_ENABLED", false),
		ProxyTLSCertFile:              strings.TrimSpace(lookup("PROXY_TLS_CERT_FILE")),
		ProxyTLSKeyFile:               strings.TrimSpace(lookup("PROXY_TLS_KEY_FILE")),
		AuthUsersFile:                 strings.TrimSpace(lookup("AUTH_USERS_FILE")),
		AuthUsers:                     strings.TrimSpace(lookup("AUTH_USERS")),
		AllowPrivateDestinations:      getenvBool(lookup, "ALLOW_PRIVATE_DESTINATIONS", false),
		MaxConnsPerIP:                 getenvInt(lookup, "MAX_CONNS_PER_IP", defaultMaxConnsPerIP),
		ServerReadHeaderTimeout:       getenvDuration(lookup, "SERVER_READ_HEADER_TIMEOUT", defaultReadHeaderTimeout),
		ServerIdleTimeout:             getenvDuration(lookup, "SERVER_IDLE_TIMEOUT", defaultIdleTimeout),
		UpstreamDialTimeout:           getenvDuration(lookup, "UPSTREAM_DIAL_TIMEOUT", defaultUpstreamDialTimeout),
		UpstreamTLSHandshakeTimeout:   getenvDuration(lookup, "UPSTREAM_TLS_HANDSHAKE_TIMEOUT", defaultUpstreamTLSHandshakeTimeout),
		UpstreamResponseHeaderTimeout: getenvDuration(lookup, "UPSTREAM_RESPONSE_HEADER_TIMEOUT", defaultUpstreamResponseHeaderTimeout),
		TunnelIdleTimeout:             getenvDuration(lookup, "TUNNEL_IDLE_TIMEOUT", defaultTunnelIdleTimeout),
		MaxHeaderBytes:                getenvInt(lookup, "MAX_HEADER_BYTES", defaultMaxHeaderBytes),
		LogLevel:                      strings.TrimSpace(getenvDefault(lookup, "LOG_LEVEL", "info")),
		LogFormat:                     strings.TrimSpace(getenvDefault(lookup, "LOG_FORMAT", "json")),
		AccessLogEnabled:              getenvBool(lookup, "ACCESS_LOG_ENABLED", true),
		MetricsEnabled:                getenvBool(lookup, "METRICS_ENABLED", false),
		AllowPublicAdmin:              getenvBool(lookup, "ALLOW_PUBLIC_ADMIN", false),
		AllowEmptyDestACL:             getenvBool(lookup, "ALLOW_EMPTY_DEST_ALLOWLIST", false),
		AllowInsecurePublicProxy:      getenvBool(lookup, "ALLOW_INSECURE_PUBLIC_PROXY", false),
		ClaudeOAuthEnabled:            getenvBool(lookup, "CLAUDE_OAUTH_ENABLED", false),
		ClaudeOAuthRefreshToken:       strings.TrimSpace(lookup("CLAUDE_OAUTH_REFRESH_TOKEN")),
		ClaudeOAuthClientID:           strings.TrimSpace(getenvDefault(lookup, "CLAUDE_OAUTH_CLIENT_ID", claudeoauth.DefaultClientID)),
		ClaudeOAuthTokenURL:           strings.TrimSpace(getenvDefault(lookup, "CLAUDE_OAUTH_TOKEN_URL", claudeoauth.DefaultTokenURL)),
	}

	var err error
	cfg.SourceAllowlist, err = parsePrefixes(lookup("SOURCE_ALLOWLIST_CIDRS"))
	if err != nil {
		return Config{}, fmt.Errorf("parse SOURCE_ALLOWLIST_CIDRS: %w", err)
	}

	cfg.DestPorts, err = parsePorts(getenvDefault(lookup, "DEST_PORT_ALLOWLIST", "443"))
	if err != nil {
		return Config{}, fmt.Errorf("parse DEST_PORT_ALLOWLIST: %w", err)
	}

	cfg.DestHosts = parseHostSet(lookup("DEST_HOST_ALLOWLIST"))
	cfg.DestSuffixes = parseSuffixes(lookup("DEST_SUFFIX_ALLOWLIST"))
	cfg.ClaudeOAuthScopes = splitCSV(lookup("CLAUDE_OAUTH_SCOPES"))

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	var problems []string

	if err := validateListen(c.ProxyListenAddr, c.ProxyListenPort); err != nil {
		problems = append(problems, "invalid proxy listen address: "+err.Error())
	}
	if err := validateListen(c.AdminListenAddr, c.AdminListenPort); err != nil {
		problems = append(problems, "invalid admin listen address: "+err.Error())
	}

	if c.ProxyTLSEnabled {
		if c.ProxyTLSCertFile == "" || c.ProxyTLSKeyFile == "" {
			problems = append(problems, "PROXY_TLS_CERT_FILE and PROXY_TLS_KEY_FILE are required when PROXY_TLS_ENABLED=true")
		}
		if c.ProxyTLSCertFile != "" {
			if err := requireFile(c.ProxyTLSCertFile); err != nil {
				problems = append(problems, "invalid PROXY_TLS_CERT_FILE: "+err.Error())
			}
		}
		if c.ProxyTLSKeyFile != "" {
			if err := requireFile(c.ProxyTLSKeyFile); err != nil {
				problems = append(problems, "invalid PROXY_TLS_KEY_FILE: "+err.Error())
			}
		}
	}

	if c.AuthUsersFile == "" && c.AuthUsers == "" {
		problems = append(problems, "at least one credential source must be configured via AUTH_USERS_FILE or AUTH_USERS")
	}

	if c.MaxConnsPerIP <= 0 {
		problems = append(problems, "MAX_CONNS_PER_IP must be positive")
	}
	if c.MaxHeaderBytes <= 0 {
		problems = append(problems, "MAX_HEADER_BYTES must be positive")
	}
	if c.ServerReadHeaderTimeout <= 0 {
		problems = append(problems, "SERVER_READ_HEADER_TIMEOUT must be positive")
	}
	if c.ServerIdleTimeout <= 0 {
		problems = append(problems, "SERVER_IDLE_TIMEOUT must be positive")
	}
	if c.UpstreamDialTimeout <= 0 {
		problems = append(problems, "UPSTREAM_DIAL_TIMEOUT must be positive")
	}
	if c.UpstreamTLSHandshakeTimeout <= 0 {
		problems = append(problems, "UPSTREAM_TLS_HANDSHAKE_TIMEOUT must be positive")
	}
	if c.UpstreamResponseHeaderTimeout <= 0 {
		problems = append(problems, "UPSTREAM_RESPONSE_HEADER_TIMEOUT must be positive")
	}
	if c.TunnelIdleTimeout <= 0 {
		problems = append(problems, "TUNNEL_IDLE_TIMEOUT must be positive")
	}

	if len(c.DestHosts) == 0 && len(c.DestSuffixes) == 0 && !c.AllowEmptyDestACL {
		problems = append(problems, "destination allowlist is empty; set DEST_HOST_ALLOWLIST and/or DEST_SUFFIX_ALLOWLIST, or explicitly allow with ALLOW_EMPTY_DEST_ALLOWLIST=true")
	}

	if isPublicBindHost(c.AdminListenAddr) && !c.AllowPublicAdmin {
		problems = append(problems, "admin listener appears public; set a loopback/private ADMIN_LISTEN_ADDR or ALLOW_PUBLIC_ADMIN=true")
	}

	if isPublicBindHost(c.ProxyListenAddr) && !c.ProxyTLSEnabled && !c.AllowInsecurePublicProxy {
		problems = append(problems, "public proxy listener without TLS is refused; enable PROXY_TLS_ENABLED or set ALLOW_INSECURE_PUBLIC_PROXY=true only when access is restricted elsewhere")
	}
	if c.ClaudeOAuthEnabled && c.ClaudeOAuthRefreshToken == "" {
		problems = append(problems, "CLAUDE_OAUTH_REFRESH_TOKEN is required when CLAUDE_OAUTH_ENABLED=true")
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (c Config) ProxyListenAddress() string {
	return net.JoinHostPort(c.ProxyListenAddr, strconv.Itoa(c.ProxyListenPort))
}

func (c Config) AdminListenAddress() string {
	return net.JoinHostPort(c.AdminListenAddr, strconv.Itoa(c.AdminListenPort))
}

func getenvDefault(lookup func(string) string, key, fallback string) string {
	value := strings.TrimSpace(lookup(key))
	if value == "" {
		return fallback
	}
	return value
}

func getenvInt(lookup func(string) string, key string, fallback int) int {
	raw := strings.TrimSpace(lookup(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getenvBool(lookup func(string) string, key string, fallback bool) bool {
	raw := strings.TrimSpace(lookup(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getenvDuration(lookup func(string) string, key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(lookup(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}

func parsePrefixes(raw string) ([]netip.Prefix, error) {
	parts := splitCSV(raw)
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		value := part
		if !strings.Contains(value, "/") {
			addr, err := netip.ParseAddr(value)
			if err != nil {
				return nil, fmt.Errorf("invalid prefix %q: %w", value, err)
			}
			bits := 32
			if addr.Is6() {
				bits = 128
			}
			value = netip.PrefixFrom(addr, bits).String()
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("invalid prefix %q: %w", value, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func parsePorts(raw string) (map[uint16]struct{}, error) {
	parts := splitCSV(raw)
	if len(parts) == 0 {
		return nil, errors.New("must contain at least one port")
	}

	ports := make(map[uint16]struct{}, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", part, err)
		}
		if value <= 0 || value > 65535 {
			return nil, fmt.Errorf("invalid TCP port %d", value)
		}
		ports[uint16(value)] = struct{}{}
	}
	return ports, nil
}

func parseHostSet(raw string) map[string]struct{} {
	parts := splitCSV(raw)
	hosts := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		host := normalizeHost(part)
		if host == "" {
			continue
		}
		hosts[host] = struct{}{}
	}
	return hosts
}

func parseSuffixes(raw string) []string {
	parts := splitCSV(raw)
	suffixes := make([]string, 0, len(parts))
	for _, part := range parts {
		value := normalizeHost(part)
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		suffixes = append(suffixes, value)
	}
	return suffixes
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func validateListen(host string, port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("port %d is out of range", port)
	}
	if strings.TrimSpace(host) == "" {
		return errors.New("host is empty")
	}
	if host == "localhost" {
		return nil
	}
	if strings.Contains(host, ":") {
		if _, err := netip.ParseAddr(host); err == nil {
			return nil
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}
	if strings.Contains(host, " ") {
		return errors.New("host contains spaces")
	}
	return nil
}

func requireFile(path string) error {
	clean := filepath.Clean(path)
	info, err := os.Stat(clean)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("path is a directory")
	}
	return nil
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimSuffix(host, ".")
	return host
}

func isPublicBindHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return true
	case "localhost":
		return false
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() {
			return false
		}
		return true
	}

	return true
}
