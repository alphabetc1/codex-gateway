package deploy

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
	ClaudeOAuth  VPSClaudeOAuth    `yaml:"claude_oauth"`
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

type VPSClaudeOAuth struct {
	Enabled      bool     `yaml:"enabled"`
	RefreshToken string   `yaml:"refresh_token"`
	ClientID     string   `yaml:"client_id"`
	TokenURL     string   `yaml:"token_url"`
	Scopes       []string `yaml:"scopes"`
}

type ClientConfig struct {
	ProjectRoot  string           `yaml:"project_root"`
	InstallDir   string           `yaml:"install_dir"`
	ServiceName  string           `yaml:"service_name"`
	WrapperName  string           `yaml:"wrapper_name"`
	ServiceScope string           `yaml:"service_scope"`
	WriteOnly    bool             `yaml:"write_only"`
	Mode         string           `yaml:"mode"`
	SSH          ClientSSH        `yaml:"ssh"`
	Tunnel       ClientTunnel     `yaml:"tunnel"`
	Endpoints    []ClientEndpoint `yaml:"endpoints"`
	Proxy        ClientProxy      `yaml:"proxy"`
	ClaudeCode   ClientClaudeCode `yaml:"claude_code"`
}

type ClientEndpoint struct {
	Name   string       `yaml:"name"`
	SSH    ClientSSH    `yaml:"ssh"`
	Tunnel ClientTunnel `yaml:"tunnel"`
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

type ClientClaudeCode struct {
	ServiceName           string               `yaml:"service_name"`
	WrapperName           string               `yaml:"wrapper_name"`
	AdminServiceName      string               `yaml:"admin_service_name"`
	ListenHost            string               `yaml:"listen_host"`
	ListenPort            int                  `yaml:"listen_port"`
	AdminLocalHost        string               `yaml:"admin_local_host"`
	AdminLocalPort        int                  `yaml:"admin_local_port"`
	AdminRemoteHost       string               `yaml:"admin_remote_host"`
	AdminRemotePort       int                  `yaml:"admin_remote_port"`
	UpstreamURL           string               `yaml:"upstream_url"`
	BinaryOutput          string               `yaml:"binary_output"`
	TLSInsecureSkipVerify bool                 `yaml:"tls_insecure_skip_verify"`
	Identity              ClaudeIdentity       `yaml:"identity"`
	Env                   ClaudeEnvProfile     `yaml:"env"`
	PromptEnv             ClaudePromptProfile  `yaml:"prompt_env"`
	Process               ClaudeProcessProfile `yaml:"process"`
}

type ClaudeIdentity struct {
	DeviceID string `yaml:"device_id"`
	Email    string `yaml:"email"`
}

type ClaudeEnvProfile struct {
	Platform              string `yaml:"platform"`
	PlatformRaw           string `yaml:"platform_raw"`
	Arch                  string `yaml:"arch"`
	NodeVersion           string `yaml:"node_version"`
	Terminal              string `yaml:"terminal"`
	PackageManagers       string `yaml:"package_managers"`
	Runtimes              string `yaml:"runtimes"`
	IsRunningWithBun      bool   `yaml:"is_running_with_bun"`
	IsClaudeAIAuth        bool   `yaml:"is_claude_ai_auth"`
	Version               string `yaml:"version"`
	VersionBase           string `yaml:"version_base"`
	BuildTime             string `yaml:"build_time"`
	DeploymentEnvironment string `yaml:"deployment_environment"`
	VCS                   string `yaml:"vcs"`
}

type ClaudePromptProfile struct {
	Platform   string `yaml:"platform"`
	Shell      string `yaml:"shell"`
	OSVersion  string `yaml:"os_version"`
	WorkingDir string `yaml:"working_dir"`
}

type ClaudeProcessProfile struct {
	ConstrainedMemory int64    `yaml:"constrained_memory"`
	RSSRange          [2]int64 `yaml:"rss_range"`
	HeapTotalRange    [2]int64 `yaml:"heap_total_range"`
	HeapUsedRange     [2]int64 `yaml:"heap_used_range"`
}

const (
	ClientModeProxy  = "proxy"
	ClientModeClaude = "claude"
	ClientModeBoth   = "both"
)

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
	if len(runtime.DestHostAllowlist) == 0 {
		runtime.DestHostAllowlist = []string{"storage.googleapis.com"}
	}
	if len(runtime.DestSuffixAllowlist) == 0 {
		runtime.DestSuffixAllowlist = []string{".claude.ai", ".claude.com", ".anthropic.com", ".openai.com", ".openrouter.ai", ".chatgpt.com", ".github.com", ".githubusercontent.com", ".githubcopilot.com", ".ghcr.io"}
	}
	if runtime.MaxConnsPerIP == 0 {
		runtime.MaxConnsPerIP = 128
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
	if spec.ClaudeOAuth.Enabled {
		if strings.TrimSpace(spec.ClaudeOAuth.ClientID) == "" {
			spec.ClaudeOAuth.ClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
		}
		if strings.TrimSpace(spec.ClaudeOAuth.TokenURL) == "" {
			spec.ClaudeOAuth.TokenURL = "https://platform.claude.com/v1/oauth/token"
		}
		if len(spec.ClaudeOAuth.Scopes) == 0 {
			spec.ClaudeOAuth.Scopes = []string{
				"user:inference",
				"user:profile",
				"user:sessions:claude_code",
				"user:mcp_servers",
				"user:file_upload",
			}
		}
		if strings.TrimSpace(spec.ClaudeOAuth.RefreshToken) == "" {
			return VPSConfig{}, fmt.Errorf("claude_oauth.refresh_token is required when claude_oauth.enabled=true")
		}
	}

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
	cwd, err := os.Getwd()
	if err != nil {
		return ClientConfig{}, fmt.Errorf("getwd: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ClientConfig{}, fmt.Errorf("home dir: %w", err)
	}
	if strings.TrimSpace(spec.ProjectRoot) == "" {
		spec.ProjectRoot = cwd
	}
	if !filepath.IsAbs(spec.ProjectRoot) {
		spec.ProjectRoot = filepath.Join(cwd, spec.ProjectRoot)
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
	spec.Mode, err = normalizeClientMode(spec.Mode)
	if err != nil {
		return ClientConfig{}, err
	}
	spec.ServiceScope, err = normalizeServiceScope(spec.ServiceScope)
	if err != nil {
		return ClientConfig{}, err
	}

	if strings.TrimSpace(spec.Proxy.Username) == "" || strings.TrimSpace(spec.Proxy.Password) == "" {
		return ClientConfig{}, fmt.Errorf("proxy.username and proxy.password are required")
	}
	if len(spec.Proxy.NoProxy) == 0 {
		spec.Proxy.NoProxy = []string{"localhost", "127.0.0.1", "::1"}
	}

	endpoints, err := normalizeClientEndpoints(spec)
	if err != nil {
		return ClientConfig{}, err
	}
	spec.Endpoints = endpoints
	spec.SSH = endpoints[0].SSH
	spec.Tunnel = endpoints[0].Tunnel
	if clientModeIncludes(spec.Mode, ClientModeClaude) {
		if len(spec.Endpoints) > 1 {
			return ClientConfig{}, fmt.Errorf("claude mode currently supports a single endpoint")
		}
		spec.ClaudeCode = normalizeClientClaudeCode(spec)
		if strings.TrimSpace(spec.ClaudeCode.Identity.DeviceID) == "" {
			return ClientConfig{}, fmt.Errorf("claude_code.identity.device_id is required when mode includes claude")
		}
		if strings.TrimSpace(spec.ClaudeCode.Env.Version) == "" {
			return ClientConfig{}, fmt.Errorf("claude_code.env.version is required when mode includes claude")
		}
		if strings.TrimSpace(spec.ClaudeCode.PromptEnv.WorkingDir) == "" {
			return ClientConfig{}, fmt.Errorf("claude_code.prompt_env.working_dir is required when mode includes claude")
		}
	}

	return spec, nil
}

func normalizeClientEndpoints(spec ClientConfig) ([]ClientEndpoint, error) {
	baseSSH := spec.SSH
	baseTunnel := spec.Tunnel
	baseLocalPort := baseTunnel.LocalPort
	if baseLocalPort == 0 {
		baseLocalPort = 8080
	}

	if len(spec.Endpoints) == 0 {
		ssh, err := normalizeClientSSH(baseSSH)
		if err != nil {
			return nil, err
		}
		tunnel, err := normalizeClientTunnel(baseTunnel, baseLocalPort)
		if err != nil {
			return nil, err
		}
		return []ClientEndpoint{{
			Name:   "primary",
			SSH:    ssh,
			Tunnel: tunnel,
		}}, nil
	}

	seenNames := make(map[string]struct{}, len(spec.Endpoints))
	seenBindAddrs := make(map[string]struct{}, len(spec.Endpoints))
	endpoints := make([]ClientEndpoint, 0, len(spec.Endpoints))
	for index, raw := range spec.Endpoints {
		name, err := normalizeClientEndpointName(raw.Name, index)
		if err != nil {
			return nil, err
		}
		if _, exists := seenNames[name]; exists {
			return nil, fmt.Errorf("duplicate endpoint name %q", name)
		}
		seenNames[name] = struct{}{}

		ssh, err := normalizeClientSSH(mergeClientSSH(baseSSH, raw.SSH))
		if err != nil {
			return nil, fmt.Errorf("endpoint %q ssh: %w", name, err)
		}

		tunnel := mergeClientTunnel(baseTunnel, raw.Tunnel)
		if raw.Tunnel.LocalPort == 0 {
			tunnel.LocalPort = baseLocalPort + index
		}
		tunnel, err = normalizeClientTunnel(tunnel, tunnel.LocalPort)
		if err != nil {
			return nil, fmt.Errorf("endpoint %q tunnel: %w", name, err)
		}

		bindAddr := net.JoinHostPort(tunnel.LocalHost, strconv.Itoa(tunnel.LocalPort))
		if _, exists := seenBindAddrs[bindAddr]; exists {
			return nil, fmt.Errorf("endpoint %q local bind %s conflicts with another endpoint", name, bindAddr)
		}
		seenBindAddrs[bindAddr] = struct{}{}

		endpoints = append(endpoints, ClientEndpoint{
			Name:   name,
			SSH:    ssh,
			Tunnel: tunnel,
		})
	}

	return endpoints, nil
}

func normalizeClientSSH(ssh ClientSSH) (ClientSSH, error) {
	if strings.TrimSpace(ssh.User) == "" || strings.TrimSpace(ssh.Host) == "" {
		return ClientSSH{}, fmt.Errorf("ssh.user and ssh.host are required")
	}
	if ssh.Port == 0 {
		ssh.Port = 22
	}
	if ssh.ServerAliveInterval == 0 {
		ssh.ServerAliveInterval = 60
	}
	if ssh.ServerAliveCountMax == 0 {
		ssh.ServerAliveCountMax = 3
	}
	return ssh, nil
}

func normalizeClientTunnel(tunnel ClientTunnel, defaultLocalPort int) (ClientTunnel, error) {
	if strings.TrimSpace(tunnel.LocalHost) == "" {
		tunnel.LocalHost = "127.0.0.1"
	}
	if tunnel.LocalPort == 0 {
		tunnel.LocalPort = defaultLocalPort
	}
	if strings.TrimSpace(tunnel.RemoteHost) == "" {
		tunnel.RemoteHost = "127.0.0.1"
	}
	if tunnel.RemotePort == 0 {
		tunnel.RemotePort = 8080
	}
	if tunnel.LocalPort <= 0 || tunnel.LocalPort > 65535 {
		return ClientTunnel{}, fmt.Errorf("local_port must be between 1 and 65535")
	}
	if tunnel.RemotePort <= 0 || tunnel.RemotePort > 65535 {
		return ClientTunnel{}, fmt.Errorf("remote_port must be between 1 and 65535")
	}
	return tunnel, nil
}

func mergeClientSSH(base, override ClientSSH) ClientSSH {
	merged := base
	if strings.TrimSpace(override.User) != "" {
		merged.User = override.User
	}
	if strings.TrimSpace(override.Host) != "" {
		merged.Host = override.Host
	}
	if override.Port != 0 {
		merged.Port = override.Port
	}
	if strings.TrimSpace(override.IdentityFile) != "" {
		merged.IdentityFile = override.IdentityFile
	}
	if override.ServerAliveInterval != 0 {
		merged.ServerAliveInterval = override.ServerAliveInterval
	}
	if override.ServerAliveCountMax != 0 {
		merged.ServerAliveCountMax = override.ServerAliveCountMax
	}
	if len(override.ExtraArgs) > 0 {
		merged.ExtraArgs = append([]string(nil), override.ExtraArgs...)
	}
	return merged
}

func mergeClientTunnel(base, override ClientTunnel) ClientTunnel {
	merged := base
	if strings.TrimSpace(override.LocalHost) != "" {
		merged.LocalHost = override.LocalHost
	}
	if override.LocalPort != 0 {
		merged.LocalPort = override.LocalPort
	}
	if strings.TrimSpace(override.RemoteHost) != "" {
		merged.RemoteHost = override.RemoteHost
	}
	if override.RemotePort != 0 {
		merged.RemotePort = override.RemotePort
	}
	return merged
}

func normalizeClientEndpointName(raw string, index int) (string, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		if index == 0 {
			value = "primary"
		} else {
			value = fmt.Sprintf("endpoint-%d", index+1)
		}
	}

	var builder strings.Builder
	lastHyphen := false
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			builder.WriteRune(ch)
			lastHyphen = false
			continue
		}
		if ch == '-' || ch == '_' || ch == ' ' {
			if !lastHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
				lastHyphen = true
			}
			continue
		}
		return "", fmt.Errorf("endpoint name %q contains unsupported character %q", raw, string(ch))
	}

	name := strings.Trim(builder.String(), "-")
	if name == "" {
		return "", fmt.Errorf("endpoint name %q is empty after normalization", raw)
	}
	return name, nil
}

func normalizeClientMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ClientModeProxy:
		return ClientModeProxy, nil
	case ClientModeClaude:
		return ClientModeClaude, nil
	case ClientModeBoth:
		return ClientModeBoth, nil
	default:
		return "", fmt.Errorf("unsupported mode %q; expected %q, %q, or %q", raw, ClientModeProxy, ClientModeClaude, ClientModeBoth)
	}
}

func clientModeIncludes(mode, target string) bool {
	if mode == ClientModeBoth {
		return true
	}
	return mode == target
}

func normalizeClientClaudeCode(spec ClientConfig) ClientClaudeCode {
	claude := spec.ClaudeCode
	if strings.TrimSpace(claude.ServiceName) == "" {
		claude.ServiceName = "codex-gateway-claude-client"
	}
	if strings.TrimSpace(claude.WrapperName) == "" {
		claude.WrapperName = "codex-gateway-claude"
	}
	if strings.TrimSpace(claude.AdminServiceName) == "" {
		claude.AdminServiceName = "codex-gateway-admin-tunnel"
	}
	if strings.TrimSpace(claude.ListenHost) == "" {
		claude.ListenHost = "127.0.0.1"
	}
	if claude.ListenPort == 0 {
		claude.ListenPort = 11443
	}
	if strings.TrimSpace(claude.AdminLocalHost) == "" {
		claude.AdminLocalHost = "127.0.0.1"
	}
	if claude.AdminLocalPort == 0 {
		claude.AdminLocalPort = 19090
	}
	if strings.TrimSpace(claude.AdminRemoteHost) == "" {
		claude.AdminRemoteHost = "127.0.0.1"
	}
	if claude.AdminRemotePort == 0 {
		claude.AdminRemotePort = 9090
	}
	if strings.TrimSpace(claude.UpstreamURL) == "" {
		claude.UpstreamURL = "https://api.anthropic.com"
	}
	if strings.TrimSpace(claude.BinaryOutput) == "" {
		claude.BinaryOutput = filepath.Join(spec.InstallDir, "bin", "codex-gateway")
	} else if !filepath.IsAbs(claude.BinaryOutput) {
		claude.BinaryOutput = filepath.Join(spec.InstallDir, claude.BinaryOutput)
	}
	if strings.TrimSpace(claude.Env.PlatformRaw) == "" {
		claude.Env.PlatformRaw = claude.Env.Platform
	}
	if strings.TrimSpace(claude.Env.VersionBase) == "" {
		claude.Env.VersionBase = claude.Env.Version
	}
	if !claude.Env.IsClaudeAIAuth {
		claude.Env.IsClaudeAIAuth = true
	}
	if claude.Process.ConstrainedMemory == 0 {
		claude.Process.ConstrainedMemory = 34359738368
	}
	if claude.Process.RSSRange == [2]int64{} {
		claude.Process.RSSRange = [2]int64{300000000, 500000000}
	}
	if claude.Process.HeapTotalRange == [2]int64{} {
		claude.Process.HeapTotalRange = [2]int64{40000000, 80000000}
	}
	if claude.Process.HeapUsedRange == [2]int64{} {
		claude.Process.HeapUsedRange = [2]int64{100000000, 200000000}
	}
	return claude
}
