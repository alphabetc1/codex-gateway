package deploy

import (
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
	if !strings.Contains(wrapperText, ". '/home/test/.config/codex-gateway/proxy.env'") {
		t.Fatalf("wrapper missing env source: %s", wrapperText)
	}
	if !strings.Contains(wrapperText, `exec "$@"`) {
		t.Fatalf("wrapper missing exec: %s", wrapperText)
	}

	serviceText := string(RenderClientService(spec))
	if !strings.Contains(serviceText, "/usr/bin/ssh") || !strings.Contains(serviceText, "127.0.0.1:8080:127.0.0.1:8080") {
		t.Fatalf("service missing ssh tunnel command: %s", serviceText)
	}
}
