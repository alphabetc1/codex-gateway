package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"codex-gateway/internal/auth"
	"codex-gateway/internal/config"
	"codex-gateway/internal/netutil"
	"codex-gateway/internal/proxy"
)

type runtimeLoader struct {
	envFilePath string
}

func newRuntimeLoader() runtimeLoader {
	return runtimeLoader{envFilePath: resolveEnvFilePath()}
}

func (l runtimeLoader) Load() (config.Config, proxy.RuntimeConfig, error) {
	cfg, err := l.loadConfig()
	if err != nil {
		return config.Config{}, proxy.RuntimeConfig{}, err
	}

	userStore, err := auth.LoadUserStore(cfg.AuthUsersFile, cfg.AuthUsers)
	if err != nil {
		return config.Config{}, proxy.RuntimeConfig{}, err
	}

	return cfg, proxy.RuntimeConfig{
		AuthStore: userStore,
		Policy: proxy.Policy{
			AllowedPorts: cfg.DestPorts,
			HostMatcher:  netutil.NewHostMatcher(cfg.DestHosts, cfg.DestSuffixes),
			Resolver:     netutil.NetResolver{},
			AllowPrivate: cfg.AllowPrivateDestinations,
		},
		SourceIPs: netutil.NewPrefixMatcher(cfg.SourceAllowlist),
	}, nil
}

func (l runtimeLoader) loadConfig() (config.Config, error) {
	if l.envFilePath != "" {
		return config.LoadFromEnvFile(l.envFilePath)
	}
	return config.LoadFromEnv()
}

func resolveEnvFilePath() string {
	if override := strings.TrimSpace(os.Getenv("CODEX_GATEWAY_ENV_FILE")); override != "" {
		return override
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	path := filepath.Join(cwd, ".env")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return path
}

func applyReloadableConfig(current, next config.Config) config.Config {
	current.AuthUsersFile = next.AuthUsersFile
	current.AuthUsers = next.AuthUsers
	current.SourceAllowlist = next.SourceAllowlist
	current.DestPorts = next.DestPorts
	current.DestHosts = next.DestHosts
	current.DestSuffixes = next.DestSuffixes
	current.AllowPrivateDestinations = next.AllowPrivateDestinations
	return current
}

func immutableConfigChanges(current, next config.Config) []string {
	var changes []string

	appendIfChanged := func(field string, different bool) {
		if different {
			changes = append(changes, field)
		}
	}

	appendIfChanged("PROXY_LISTEN_ADDR", current.ProxyListenAddr != next.ProxyListenAddr)
	appendIfChanged("PROXY_LISTEN_PORT", current.ProxyListenPort != next.ProxyListenPort)
	appendIfChanged("ADMIN_LISTEN_ADDR", current.AdminListenAddr != next.AdminListenAddr)
	appendIfChanged("ADMIN_LISTEN_PORT", current.AdminListenPort != next.AdminListenPort)
	appendIfChanged("PROXY_TLS_ENABLED", current.ProxyTLSEnabled != next.ProxyTLSEnabled)
	appendIfChanged("PROXY_TLS_CERT_FILE", current.ProxyTLSCertFile != next.ProxyTLSCertFile)
	appendIfChanged("PROXY_TLS_KEY_FILE", current.ProxyTLSKeyFile != next.ProxyTLSKeyFile)
	appendIfChanged("MAX_CONNS_PER_IP", current.MaxConnsPerIP != next.MaxConnsPerIP)
	appendIfChanged("SERVER_READ_HEADER_TIMEOUT", current.ServerReadHeaderTimeout != next.ServerReadHeaderTimeout)
	appendIfChanged("SERVER_IDLE_TIMEOUT", current.ServerIdleTimeout != next.ServerIdleTimeout)
	appendIfChanged("UPSTREAM_DIAL_TIMEOUT", current.UpstreamDialTimeout != next.UpstreamDialTimeout)
	appendIfChanged("UPSTREAM_TLS_HANDSHAKE_TIMEOUT", current.UpstreamTLSHandshakeTimeout != next.UpstreamTLSHandshakeTimeout)
	appendIfChanged("UPSTREAM_RESPONSE_HEADER_TIMEOUT", current.UpstreamResponseHeaderTimeout != next.UpstreamResponseHeaderTimeout)
	appendIfChanged("TUNNEL_IDLE_TIMEOUT", current.TunnelIdleTimeout != next.TunnelIdleTimeout)
	appendIfChanged("MAX_HEADER_BYTES", current.MaxHeaderBytes != next.MaxHeaderBytes)
	appendIfChanged("LOG_LEVEL", current.LogLevel != next.LogLevel)
	appendIfChanged("LOG_FORMAT", current.LogFormat != next.LogFormat)
	appendIfChanged("ACCESS_LOG_ENABLED", current.AccessLogEnabled != next.AccessLogEnabled)
	appendIfChanged("METRICS_ENABLED", current.MetricsEnabled != next.MetricsEnabled)
	appendIfChanged("ALLOW_PUBLIC_ADMIN", current.AllowPublicAdmin != next.AllowPublicAdmin)
	appendIfChanged("ALLOW_EMPTY_DEST_ALLOWLIST", current.AllowEmptyDestACL != next.AllowEmptyDestACL)
	appendIfChanged("ALLOW_INSECURE_PUBLIC_PROXY", current.AllowInsecurePublicProxy != next.AllowInsecurePublicProxy)

	return changes
}

func changedReloadableFields(current, next config.Config) []string {
	var changes []string

	appendIfChanged := func(field string, different bool) {
		if different {
			changes = append(changes, field)
		}
	}

	appendIfChanged("AUTH_USERS_FILE", current.AuthUsersFile != next.AuthUsersFile)
	appendIfChanged("AUTH_USERS", current.AuthUsers != next.AuthUsers)
	appendIfChanged("SOURCE_ALLOWLIST_CIDRS", !reflect.DeepEqual(current.SourceAllowlist, next.SourceAllowlist))
	appendIfChanged("DEST_PORT_ALLOWLIST", !reflect.DeepEqual(current.DestPorts, next.DestPorts))
	appendIfChanged("DEST_HOST_ALLOWLIST", !reflect.DeepEqual(current.DestHosts, next.DestHosts))
	appendIfChanged("DEST_SUFFIX_ALLOWLIST", !reflect.DeepEqual(current.DestSuffixes, next.DestSuffixes))
	appendIfChanged("ALLOW_PRIVATE_DESTINATIONS", current.AllowPrivateDestinations != next.AllowPrivateDestinations)

	return changes
}
