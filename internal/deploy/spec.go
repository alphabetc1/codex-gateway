package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type VPSConfig struct {
	ProjectRoot  string            `yaml:"project_root"`
	BinaryOutput string            `yaml:"binary_output"`
	ServiceName  string            `yaml:"service_name"`
	ServiceScope string            `yaml:"service_scope"`
	WriteOnly    bool              `yaml:"write_only"`
	Users        []ProxyUser       `yaml:"users"`
	Runtime      VPSRuntime        `yaml:"runtime"`
	EnvOverrides map[string]string `yaml:"env_overrides"`
}

type ProxyUser struct {
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
	BcryptHash string `yaml:"bcrypt_hash"`
}

type VPSRuntime struct {
	ProxyListenAddr               string   `yaml:"proxy_listen_addr"`
	ProxyListenPort               int      `yaml:"proxy_listen_port"`
	AdminListenAddr               string   `yaml:"admin_listen_addr"`
	AdminListenPort               int      `yaml:"admin_listen_port"`
	ProxyTLSEnabled               bool     `yaml:"proxy_tls_enabled"`
	ProxyTLSCertFile              string   `yaml:"proxy_tls_cert_file"`
	ProxyTLSKeyFile               string   `yaml:"proxy_tls_key_file"`
	SourceAllowlistCIDRs          []string `yaml:"source_allowlist_cidrs"`
	DestPortAllowlist             []int    `yaml:"dest_port_allowlist"`
	DestHostAllowlist             []string `yaml:"dest_host_allowlist"`
	DestSuffixAllowlist           []string `yaml:"dest_suffix_allowlist"`
	AllowEmptyDestAllowlist       bool     `yaml:"allow_empty_dest_allowlist"`
	AllowPrivateDestinations      bool     `yaml:"allow_private_destinations"`
	MaxConnsPerIP                 int      `yaml:"max_conns_per_ip"`
	ServerReadHeaderTimeout       string   `yaml:"server_read_header_timeout"`
	ServerIdleTimeout             string   `yaml:"server_idle_timeout"`
	UpstreamDialTimeout           string   `yaml:"upstream_dial_timeout"`
	UpstreamTLSHandshakeTimeout   string   `yaml:"upstream_tls_handshake_timeout"`
	UpstreamResponseHeaderTimeout string   `yaml:"upstream_response_header_timeout"`
	TunnelIdleTimeout             string   `yaml:"tunnel_idle_timeout"`
	MaxHeaderBytes                int      `yaml:"max_header_bytes"`
	LogLevel                      string   `yaml:"log_level"`
	LogFormat                     string   `yaml:"log_format"`
	AccessLogEnabled              bool     `yaml:"access_log_enabled"`
	MetricsEnabled                bool     `yaml:"metrics_enabled"`
	AllowPublicAdmin              bool     `yaml:"allow_public_admin"`
	AllowInsecurePublicProxy      bool     `yaml:"allow_insecure_public_proxy"`
}

type ClientConfig struct {
	InstallDir  string       `yaml:"install_dir"`
	ServiceName string       `yaml:"service_name"`
	WrapperName string       `yaml:"wrapper_name"`
	ServiceScope string      `yaml:"service_scope"`
	WriteOnly   bool         `yaml:"write_only"`
	SSH         ClientSSH    `yaml:"ssh"`
	Tunnel      ClientTunnel `yaml:"tunnel"`
	Proxy       ClientProxy  `yaml:"proxy"`
}

type ClientSSH struct {
	User                string   `yaml:"user"`
	Host                string   `yaml:"host"`
	Port                int      `yaml:"port"`
	IdentityFile        string   `yaml:"identity_file"`
	ServerAliveInterval int      `yaml:"server_alive_interval"`
	ServerAliveCountMax int      `yaml:"server_alive_count_max"`
	ExtraArgs           []string `yaml:"extra_args"`
}

type ClientTunnel struct {
	LocalHost  string `yaml:"local_host"`
	LocalPort  int    `yaml:"local_port"`
	RemoteHost string `yaml:"remote_host"`
	RemotePort int    `yaml:"remote_port"`
}

type ClientProxy struct {
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
	NoProxy  []string `yaml:"no_proxy"`
}

func LoadVPSConfig(path string) (VPSConfig, error) {
	var spec VPSConfig
	if err := loadYAML(path, &spec); err != nil {
		return VPSConfig{}, err
	}
	return normalizeVPSConfig(spec)
}

func LoadClientConfig(path string) (ClientConfig, error) {
	var spec ClientConfig
	if err := loadYAML(path, &spec); err != nil {
		return ClientConfig{}, err
	}
	return normalizeClientConfig(spec)
}

func loadYAML(path string, target any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(content, target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func normalizeVPSConfig(spec VPSConfig) (VPSConfig, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return VPSConfig{}, fmt.Errorf("getwd: %w", err)
	}

	if strings.TrimSpace(spec.ProjectRoot) == "" {
		spec.ProjectRoot = cwd
	}
	spec.ProjectRoot, err = filepath.Abs(spec.ProjectRoot)
	if err != nil {
		return VPSConfig{}, fmt.Errorf("resolve project_root: %w", err)
	}

	if strings.TrimSpace(spec.BinaryOutput) == "" {
		spec.BinaryOutput = filepath.Join(spec.ProjectRoot, "bin", "codex-gateway")
	} else if !filepath.IsAbs(spec.BinaryOutput) {
		spec.BinaryOutput = filepath.Join(spec.ProjectRoot, spec.BinaryOutput)
	}
	if strings.TrimSpace(spec.ServiceName) == "" {
		spec.ServiceName = "codex-gateway"
	}
	spec.ServiceScope, err = normalizeServiceScope(spec.ServiceScope)
	if err != nil {
		return VPSConfig{}, err
	}

	runtime := spec.Runtime
	if runtime.ProxyListenAddr == "" {
		runtime.ProxyListenAddr = "127.0.0.1"
	}
	if runtime.ProxyListenPort == 0 {
		runtime.ProxyListenPort = 8080
	}
	if runtime.AdminListenAddr == "" {
		runtime.AdminListenAddr = "127.0.0.1"
	}
	if runtime.AdminListenPort == 0 {
		runtime.AdminListenPort = 9090
	}
	if runtime.ProxyTLSCertFile == "" {
		runtime.ProxyTLSCertFile = filepath.Join(spec.ProjectRoot, "certs", "tls.crt")
	}
	if runtime.ProxyTLSKeyFile == "" {
		runtime.ProxyTLSKeyFile = filepath.Join(spec.ProjectRoot, "certs", "tls.key")
	}
	if len(runtime.SourceAllowlistCIDRs) == 0 {
		runtime.SourceAllowlistCIDRs = []string{"127.0.0.1/32", "::1/128"}
	}
	if len(runtime.DestPortAllowlist) == 0 {
		runtime.DestPortAllowlist = []int{443}
	}
	if len(runtime.DestSuffixAllowlist) == 0 {
		runtime.DestSuffixAllowlist = []string{".anthropic.com", ".openai.com", ".openrouter.ai", ".chatgpt.com"}
	}
	if runtime.MaxConnsPerIP == 0 {
		runtime.MaxConnsPerIP = 16
	}
	if runtime.ServerReadHeaderTimeout == "" {
		runtime.ServerReadHeaderTimeout = "10s"
	}
	if runtime.ServerIdleTimeout == "" {
		runtime.ServerIdleTimeout = "90s"
	}
	if runtime.UpstreamDialTimeout == "" {
		runtime.UpstreamDialTimeout = "10s"
	}
	if runtime.UpstreamTLSHandshakeTimeout == "" {
		runtime.UpstreamTLSHandshakeTimeout = "10s"
	}
	if runtime.UpstreamResponseHeaderTimeout == "" {
		runtime.UpstreamResponseHeaderTimeout = "30s"
	}
	if runtime.TunnelIdleTimeout == "" {
		runtime.TunnelIdleTimeout = "10m"
	}
	if runtime.MaxHeaderBytes == 0 {
		runtime.MaxHeaderBytes = 1048576
	}
	if runtime.LogLevel == "" {
		runtime.LogLevel = "info"
	}
	if runtime.LogFormat == "" {
		runtime.LogFormat = "json"
	}
	spec.Runtime = runtime

	if len(spec.Users) == 0 {
		return VPSConfig{}, fmt.Errorf("at least one proxy user is required")
	}
	for _, user := range spec.Users {
		if strings.TrimSpace(user.Username) == "" {
			return VPSConfig{}, fmt.Errorf("user.username is required")
		}
		if strings.TrimSpace(user.Password) == "" && strings.TrimSpace(user.BcryptHash) == "" {
			return VPSConfig{}, fmt.Errorf("user %q must set password or bcrypt_hash", user.Username)
		}
	}

	return spec, nil
}

func normalizeClientConfig(spec ClientConfig) (ClientConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ClientConfig{}, fmt.Errorf("home dir: %w", err)
	}
	if strings.TrimSpace(spec.InstallDir) == "" {
		spec.InstallDir = filepath.Join(home, ".config", "codex-gateway")
	}
	if !filepath.IsAbs(spec.InstallDir) {
		spec.InstallDir = filepath.Join(home, spec.InstallDir)
	}
	if strings.TrimSpace(spec.ServiceName) == "" {
		spec.ServiceName = "codex-gateway-tunnel"
	}
	if strings.TrimSpace(spec.WrapperName) == "" {
		spec.WrapperName = "codex-gateway-proxy"
	}
	spec.ServiceScope, err = normalizeServiceScope(spec.ServiceScope)
	if err != nil {
		return ClientConfig{}, err
	}

	if strings.TrimSpace(spec.SSH.User) == "" || strings.TrimSpace(spec.SSH.Host) == "" {
		return ClientConfig{}, fmt.Errorf("ssh.user and ssh.host are required")
	}
	if spec.SSH.Port == 0 {
		spec.SSH.Port = 22
	}
	if spec.SSH.ServerAliveInterval == 0 {
		spec.SSH.ServerAliveInterval = 60
	}
	if spec.SSH.ServerAliveCountMax == 0 {
		spec.SSH.ServerAliveCountMax = 3
	}

	if spec.Tunnel.LocalHost == "" {
		spec.Tunnel.LocalHost = "127.0.0.1"
	}
	if spec.Tunnel.LocalPort == 0 {
		spec.Tunnel.LocalPort = 8080
	}
	if spec.Tunnel.RemoteHost == "" {
		spec.Tunnel.RemoteHost = "127.0.0.1"
	}
	if spec.Tunnel.RemotePort == 0 {
		spec.Tunnel.RemotePort = 8080
	}

	if strings.TrimSpace(spec.Proxy.Username) == "" || strings.TrimSpace(spec.Proxy.Password) == "" {
		return ClientConfig{}, fmt.Errorf("proxy.username and proxy.password are required")
	}
	if len(spec.Proxy.NoProxy) == 0 {
		spec.Proxy.NoProxy = []string{"localhost", "127.0.0.1", "::1"}
	}

	return spec, nil
}
