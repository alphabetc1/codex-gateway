package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderVPSEnvUsesAbsoluteRuntimePaths(t *testing.T) {
	spec := VPSConfig{
		ProjectRoot:  "/srv/codex-gateway",
		BinaryOutput: "/srv/codex-gateway/bin/codex-gateway",
		ServiceName:  "codex-gateway",
		Users: []ProxyUser{
			{Username: "alice", BcryptHash: "$2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuuuu"},
		},
		Runtime: VPSRuntime{
			ProxyListenAddr:               "127.0.0.1",
			ProxyListenPort:               8080,
			AdminListenAddr:               "127.0.0.1",
			AdminListenPort:               9090,
			SourceAllowlistCIDRs:          []string{"127.0.0.1/32"},
			DestPortAllowlist:             []int{443},
			DestHostAllowlist:             []string{"storage.googleapis.com"},
			DestSuffixAllowlist:           []string{".chatgpt.com"},
			MaxConnsPerIP:                 16,
			ServerReadHeaderTimeout:       "10s",
			ServerIdleTimeout:             "90s",
			UpstreamDialTimeout:           "10s",
			UpstreamTLSHandshakeTimeout:   "10s",
			UpstreamResponseHeaderTimeout: "30s",
			TunnelIdleTimeout:             "10m",
			MaxHeaderBytes:                1048576,
			LogLevel:                      "info",
			LogFormat:                     "json",
		},
		ClaudeOAuth: VPSClaudeOAuth{
			Enabled:      true,
			RefreshToken: "refresh-token",
			ClientID:     "client-id",
			TokenURL:     "https://platform.claude.com/v1/oauth/token",
			Scopes:       []string{"user:inference"},
		},
	}

	content, err := RenderVPSEnv(spec)
	if err != nil {
		t.Fatalf("RenderVPSEnv() error = %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "AUTH_USERS_FILE=/srv/codex-gateway/config/users.txt") {
		t.Fatalf("env missing users path: %s", text)
	}
	if !strings.Contains(text, "DEST_SUFFIX_ALLOWLIST=.chatgpt.com") {
		t.Fatalf("env missing suffix allowlist: %s", text)
	}
	if !strings.Contains(text, "DEST_HOST_ALLOWLIST=storage.googleapis.com") {
		t.Fatalf("env missing host allowlist: %s", text)
	}
	if !strings.Contains(text, "CLAUDE_OAUTH_ENABLED=true") {
		t.Fatalf("env missing claude oauth flag: %s", text)
	}
	if !strings.Contains(text, "CLAUDE_OAUTH_REFRESH_TOKEN=refresh-token") {
		t.Fatalf("env missing claude oauth refresh token: %s", text)
	}
}

func TestRenderVPSUsersHashesPlaintextPasswords(t *testing.T) {
	spec := VPSConfig{
		Users: []ProxyUser{
			{Username: "alice", Password: "change-me"},
		},
	}

	content, err := RenderVPSUsers(spec)
	if err != nil {
		t.Fatalf("RenderVPSUsers() error = %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "alice:$2") {
		t.Fatalf("users file does not contain bcrypt hash: %s", text)
	}
	if strings.Contains(text, "change-me") {
		t.Fatalf("users file leaked plaintext password: %s", text)
	}
}

func TestNormalizeVPSConfigAppliesDefaultGitHubAllowlist(t *testing.T) {
	spec := VPSConfig{
		Users: []ProxyUser{
			{Username: "alice", Password: "change-me"},
		},
	}

	normalized, err := normalizeVPSConfig(spec)
	if err != nil {
		t.Fatalf("normalizeVPSConfig() error = %v", err)
	}
	if !contains(normalized.Runtime.DestHostAllowlist, "storage.googleapis.com") {
		t.Fatalf("default host allowlist = %#v, want storage.googleapis.com", normalized.Runtime.DestHostAllowlist)
	}
	if !contains(normalized.Runtime.DestSuffixAllowlist, ".github.com") {
		t.Fatalf("default suffix allowlist = %#v, want .github.com", normalized.Runtime.DestSuffixAllowlist)
	}
	if !contains(normalized.Runtime.DestSuffixAllowlist, ".githubusercontent.com") {
		t.Fatalf("default suffix allowlist = %#v, want .githubusercontent.com", normalized.Runtime.DestSuffixAllowlist)
	}
	if !contains(normalized.Runtime.DestSuffixAllowlist, ".githubcopilot.com") {
		t.Fatalf("default suffix allowlist = %#v, want .githubcopilot.com", normalized.Runtime.DestSuffixAllowlist)
	}
	if !contains(normalized.Runtime.DestSuffixAllowlist, ".ghcr.io") {
		t.Fatalf("default suffix allowlist = %#v, want .ghcr.io", normalized.Runtime.DestSuffixAllowlist)
	}
}

func TestRenderClientArtifacts(t *testing.T) {
	spec := ClientConfig{
		InstallDir:  "/home/test/.config/codex-gateway",
		ServiceName: "codex-gateway-tunnel",
		WrapperName: "codex-gateway-proxy",
		SSH: ClientSSH{
			User:                "admin",
			Host:                "your-vps.example.com",
			Port:                22,
			ServerAliveInterval: 60,
			ServerAliveCountMax: 3,
		},
		Tunnel: ClientTunnel{
			LocalHost:  "127.0.0.1",
			LocalPort:  8080,
			RemoteHost: "127.0.0.1",
			RemotePort: 8080,
		},
		Proxy: ClientProxy{
			Username: "alice",
			Password: "secret",
			NoProxy:  []string{"localhost", "127.0.0.1", "::1"},
		},
	}

	envText := string(RenderClientEnv(spec))
	if !strings.Contains(envText, "HTTP_PROXY='http://alice:secret@127.0.0.1:8080'") {
		t.Fatalf("client env missing HTTP_PROXY: %s", envText)
	}

	wrapperText := string(RenderClientWrapper(spec, filepath.Join(spec.InstallDir, "proxy.env")))
	if !strings.Contains(wrapperText, "env_path='/home/test/.config/codex-gateway/proxy.env'") {
		t.Fatalf("wrapper missing env source: %s", wrapperText)
	}
	if !strings.Contains(wrapperText, `. "$env_path"`) {
		t.Fatalf("wrapper missing dynamic env source: %s", wrapperText)
	}
	if !strings.Contains(wrapperText, `exec "$@"`) {
		t.Fatalf("wrapper missing exec: %s", wrapperText)
	}

	serviceText := string(RenderClientService(spec))
	if !strings.Contains(serviceText, "/usr/bin/ssh") || !strings.Contains(serviceText, "127.0.0.1:8080:127.0.0.1:8080") {
		t.Fatalf("service missing ssh tunnel command: %s", serviceText)
	}
}

func TestNormalizeClientConfigWithClaudeMode(t *testing.T) {
	spec := ClientConfig{
		ProjectRoot: ".",
		Mode:        ClientModeBoth,
		SSH: ClientSSH{
			User: "admin",
			Host: "your-vps.example.com",
		},
		Tunnel: ClientTunnel{
			LocalHost:  "127.0.0.1",
			LocalPort:  8080,
			RemoteHost: "127.0.0.1",
			RemotePort: 8080,
		},
		Proxy: ClientProxy{
			Username: "alice",
			Password: "secret",
		},
		ClaudeCode: ClientClaudeCode{
			Identity: ClaudeIdentity{
				DeviceID: "canonical-device",
			},
			Env: ClaudeEnvProfile{
				Version: "2.1.81",
			},
			PromptEnv: ClaudePromptProfile{
				WorkingDir: "/Users/jack/projects",
			},
		},
	}

	normalized, err := normalizeClientConfig(spec)
	if err != nil {
		t.Fatalf("normalizeClientConfig() error = %v", err)
	}
	if normalized.ClaudeCode.ListenPort != 11443 {
		t.Fatalf("claude listen port = %d, want %d", normalized.ClaudeCode.ListenPort, 11443)
	}
	if normalized.ClaudeCode.AdminLocalPort != 19090 {
		t.Fatalf("claude admin local port = %d, want %d", normalized.ClaudeCode.AdminLocalPort, 19090)
	}
	if normalized.ClaudeCode.BinaryOutput == "" {
		t.Fatalf("claude binary output is empty")
	}
}

func TestRenderClaudeClientArtifacts(t *testing.T) {
	spec := ClientConfig{
		InstallDir:  "/home/test/.config/codex-gateway",
		ServiceName: "codex-gateway-tunnel",
		WrapperName: "codex-gateway-proxy",
		Mode:        ClientModeBoth,
		SSH: ClientSSH{
			User:                "admin",
			Host:                "your-vps.example.com",
			ServerAliveInterval: 60,
			ServerAliveCountMax: 3,
		},
		Tunnel: ClientTunnel{
			LocalHost:  "127.0.0.1",
			LocalPort:  8080,
			RemoteHost: "127.0.0.1",
			RemotePort: 8080,
		},
		Proxy: ClientProxy{
			Username: "alice",
			Password: "secret",
		},
		ClaudeCode: ClientClaudeCode{
			ServiceName:      "codex-gateway-claude-client",
			WrapperName:      "codex-gateway-claude",
			AdminServiceName: "codex-gateway-admin-tunnel",
			ListenHost:       "127.0.0.1",
			ListenPort:       11443,
			AdminLocalHost:   "127.0.0.1",
			AdminLocalPort:   19090,
			AdminRemoteHost:  "127.0.0.1",
			AdminRemotePort:  9090,
			UpstreamURL:      "https://api.anthropic.com",
			BinaryOutput:     "/home/test/.config/codex-gateway/bin/codex-gateway",
			Identity: ClaudeIdentity{
				DeviceID: "canonical-device",
			},
			Env: ClaudeEnvProfile{
				Platform:    "darwin",
				PlatformRaw: "darwin",
				Version:     "2.1.81",
				VersionBase: "2.1.81",
			},
			PromptEnv: ClaudePromptProfile{
				Platform:   "darwin",
				Shell:      "zsh",
				OSVersion:  "Darwin 24.4.0",
				WorkingDir: "/Users/jack/projects",
			},
			Process: ClaudeProcessProfile{
				ConstrainedMemory: 34359738368,
				RSSRange:          [2]int64{300000000, 500000000},
				HeapTotalRange:    [2]int64{40000000, 80000000},
				HeapUsedRange:     [2]int64{100000000, 200000000},
			},
		},
	}

	envText := string(RenderClaudeClientEnv(spec))
	if !strings.Contains(envText, "ANTHROPIC_BASE_URL='http://127.0.0.1:11443'") {
		t.Fatalf("claude env missing ANTHROPIC_BASE_URL: %s", envText)
	}

	configText, err := RenderClaudeClientConfig(spec, primaryClientEndpoint(spec))
	if err != nil {
		t.Fatalf("RenderClaudeClientConfig() error = %v", err)
	}
	if !strings.Contains(string(configText), "oauth_broker_url: http://127.0.0.1:19090/claude/oauth/token") {
		t.Fatalf("claude config missing oauth broker url: %s", string(configText))
	}

	serviceText := string(RenderClaudeClientService(spec, "/home/test/.config/codex-gateway/claude-client.yaml", "codex-gateway-tunnel", "codex-gateway-admin-tunnel"))
	if !strings.Contains(serviceText, "codex-gateway claude-client -config /home/test/.config/codex-gateway/claude-client.yaml") {
		t.Fatalf("claude service missing ExecStart: %s", serviceText)
	}
}

func TestRenderServicesUseScopeSpecificTargets(t *testing.T) {
	vpsSpec := VPSConfig{
		ProjectRoot:  "/srv/codex-gateway",
		BinaryOutput: "/srv/codex-gateway/bin/codex-gateway",
		ServiceName:  "codex-gateway",
		ServiceScope: ServiceScopeSystem,
	}
	vpsService := string(RenderVPSService(vpsSpec))
	if !strings.Contains(vpsService, "WantedBy=multi-user.target") {
		t.Fatalf("system-scoped VPS service missing multi-user target: %s", vpsService)
	}
	if !strings.Contains(vpsService, "Environment=CODEX_GATEWAY_ENV_FILE=/srv/codex-gateway/.env") {
		t.Fatalf("system-scoped VPS service missing env file hint: %s", vpsService)
	}

	clientSpec := ClientConfig{
		ServiceName:  "codex-gateway-tunnel",
		ServiceScope: ServiceScopeUser,
		SSH: ClientSSH{
			User:                "admin",
			Host:                "your-vps.example.com",
			ServerAliveInterval: 60,
			ServerAliveCountMax: 3,
		},
		Tunnel: ClientTunnel{
			LocalHost:  "127.0.0.1",
			LocalPort:  8080,
			RemoteHost: "127.0.0.1",
			RemotePort: 8080,
		},
	}
	clientService := string(RenderClientService(clientSpec))
	if !strings.Contains(clientService, "WantedBy=default.target") {
		t.Fatalf("user-scoped client service missing default target: %s", clientService)
	}
}

func TestClientWrapperScopesProxyEnvToCommand(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh is required for wrapper smoke test")
	}

	spec := ClientConfig{
		InstallDir:  "/home/test/.config/codex-gateway",
		ServiceName: "codex-gateway-tunnel",
		WrapperName: "codex-gateway-proxy",
		Tunnel: ClientTunnel{
			LocalHost:  "127.0.0.1",
			LocalPort:  8080,
			RemoteHost: "127.0.0.1",
			RemotePort: 8080,
		},
		Proxy: ClientProxy{
			Username: "alice",
			Password: "secret",
			NoProxy:  []string{"localhost", "127.0.0.1", "::1"},
		},
	}

	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "proxy.env")
	wrapperPath := filepath.Join(tempDir, "codex-gateway-proxy")
	if err := os.WriteFile(envPath, RenderClientEnv(spec), 0o600); err != nil {
		t.Fatalf("WriteFile(env) error = %v", err)
	}
	if err := os.WriteFile(wrapperPath, RenderClientWrapper(spec, envPath), 0o755); err != nil {
		t.Fatalf("WriteFile(wrapper) error = %v", err)
	}

	direct := exec.Command("/bin/sh", "-c", `printf '%s' "${HTTP_PROXY:-}"`)
	direct.Env = []string{"PATH=/usr/bin:/bin"}
	directOutput, err := direct.Output()
	if err != nil {
		t.Fatalf("direct command error = %v", err)
	}
	if strings.TrimSpace(string(directOutput)) != "" {
		t.Fatalf("direct command unexpectedly saw proxy env: %q", string(directOutput))
	}

	wrapped := exec.Command(wrapperPath, "/bin/sh", "-c", `printf '%s|%s' "$HTTP_PROXY" "$NO_PROXY"`)
	wrapped.Env = []string{"PATH=/usr/bin:/bin"}
	wrappedOutput, err := wrapped.Output()
	if err != nil {
		t.Fatalf("wrapped command error = %v", err)
	}
	if got := string(wrappedOutput); got != "http://alice:secret@127.0.0.1:8080|localhost,127.0.0.1,::1" {
		t.Fatalf("wrapped env = %q", got)
	}
}

func TestNormalizeClientConfigWithMultipleEndpoints(t *testing.T) {
	spec := ClientConfig{
		InstallDir:  "/home/test/.config/codex-gateway",
		ServiceName: "codex-gateway-tunnel",
		WrapperName: "codex-gateway-proxy",
		SSH: ClientSSH{
			User:         "admin",
			IdentityFile: "/home/test/.ssh/id_ed25519",
		},
		Tunnel: ClientTunnel{
			LocalHost:  "127.0.0.1",
			LocalPort:  8080,
			RemoteHost: "127.0.0.1",
			RemotePort: 8080,
		},
		Endpoints: []ClientEndpoint{
			{
				Name: "east",
				SSH: ClientSSH{
					Host: "east.example.com",
				},
			},
			{
				Name: "west",
				SSH: ClientSSH{
					Host: "west.example.com",
				},
			},
		},
		Proxy: ClientProxy{
			Username: "alice",
			Password: "secret",
		},
	}

	normalized, err := normalizeClientConfig(spec)
	if err != nil {
		t.Fatalf("normalizeClientConfig() error = %v", err)
	}
	if len(normalized.Endpoints) != 2 {
		t.Fatalf("len(normalized.Endpoints) = %d, want %d", len(normalized.Endpoints), 2)
	}
	if normalized.Endpoints[0].SSH.User != "admin" || normalized.Endpoints[0].SSH.Host != "east.example.com" {
		t.Fatalf("first endpoint ssh = %#v", normalized.Endpoints[0].SSH)
	}
	if normalized.Endpoints[0].Tunnel.LocalPort != 8080 {
		t.Fatalf("first endpoint local port = %d, want %d", normalized.Endpoints[0].Tunnel.LocalPort, 8080)
	}
	if normalized.Endpoints[1].Tunnel.LocalPort != 8081 {
		t.Fatalf("second endpoint local port = %d, want %d", normalized.Endpoints[1].Tunnel.LocalPort, 8081)
	}
	if normalized.Endpoints[1].SSH.IdentityFile != "/home/test/.ssh/id_ed25519" {
		t.Fatalf("second endpoint identity file = %q", normalized.Endpoints[1].SSH.IdentityFile)
	}
}

func TestMultiEndpointWrapperSelectsNamedEndpoint(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh is required for wrapper smoke test")
	}

	tempDir := t.TempDir()
	primaryEnv := filepath.Join(tempDir, "proxy-primary.env")
	backupEnv := filepath.Join(tempDir, "proxy-backup.env")
	if err := os.WriteFile(primaryEnv, []byte("export HTTP_PROXY='http://primary'\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(primary env) error = %v", err)
	}
	if err := os.WriteFile(backupEnv, []byte("export HTTP_PROXY='http://backup'\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(backup env) error = %v", err)
	}

	spec := ClientConfig{WrapperName: "codex-gateway-proxy"}
	wrapperPath := filepath.Join(tempDir, "codex-gateway-proxy")
	wrapper := RenderClientWrapperForEndpoints(spec, []clientWrapperEndpoint{
		{Name: "primary", EnvPath: primaryEnv, LocalHost: "127.0.0.1", LocalPort: 8080},
		{Name: "backup", EnvPath: backupEnv, LocalHost: "127.0.0.1", LocalPort: 8081},
	})
	if err := os.WriteFile(wrapperPath, wrapper, 0o755); err != nil {
		t.Fatalf("WriteFile(wrapper) error = %v", err)
	}

	command := exec.Command(wrapperPath, "--endpoint", "backup", "/bin/sh", "-c", `printf '%s|%s' "$CODEX_GATEWAY_ACTIVE_ENDPOINT" "$HTTP_PROXY"`)
	command.Env = []string{"PATH=/usr/bin:/bin"}
	output, err := command.Output()
	if err != nil {
		t.Fatalf("wrapped command error = %v", err)
	}
	if got := string(output); got != "backup|http://backup" {
		t.Fatalf("wrapped output = %q", got)
	}
}
