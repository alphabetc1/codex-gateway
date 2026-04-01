package claudeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testClaudeConfig(upstreamURL, proxyURL, brokerURL string) Config {
	return Config{
		ListenAddr:     "127.0.0.1:11443",
		UpstreamURL:    upstreamURL,
		ProxyURL:       proxyURL,
		OAuthBrokerURL: brokerURL,
		OAuthUsername:  "alice",
		OAuthPassword:  "secret",
		LogLevel:       "info",
		LogFormat:      "json",
		Identity: Identity{
			DeviceID: "canonical-device",
			Email:    "shared@example.com",
		},
		Env: EnvProfile{
			Platform:              "darwin",
			PlatformRaw:           "darwin",
			Arch:                  "arm64",
			NodeVersion:           "v24.3.0",
			Terminal:              "iTerm2.app",
			PackageManagers:       "npm,pnpm",
			Runtimes:              "node",
			IsClaudeAIAuth:        true,
			Version:               "2.1.81",
			VersionBase:           "2.1.81",
			BuildTime:             "2026-03-20T21:26:18Z",
			DeploymentEnvironment: "unknown-darwin",
			VCS:                   "git",
		},
		PromptEnv: PromptProfile{
			Platform:   "darwin",
			Shell:      "zsh",
			OSVersion:  "Darwin 24.4.0",
			WorkingDir: "/Users/jack/projects",
		},
		Process: ProcessProfile{
			ConstrainedMemory: 34359738368,
			RSSRange:          [2]int64{300000000, 300000100},
			HeapTotalRange:    [2]int64{40000000, 40000010},
			HeapUsedRange:     [2]int64{100000000, 100000010},
		},
	}
}

func TestRewriteBodyMessages(t *testing.T) {
	config := testClaudeConfig("https://api.anthropic.com", "http://127.0.0.1:8080", "http://127.0.0.1:19090/claude/oauth/token")
	body := []byte(`{"metadata":{"user_id":"{\"device_id\":\"real-device\"}"},"system":[{"text":"Platform: linux\nShell: bash\nOS Version: Linux 6.5\nWorking directory: /home/bob/project"}]}`)

	rewritten := RewriteBody(body, "/v1/messages", config)
	text := string(rewritten)
	if !bytes.Contains(rewritten, []byte(`canonical-device`)) {
		t.Fatalf("rewritten body missing canonical device: %s", text)
	}
	if !bytes.Contains(rewritten, []byte(`Platform: darwin`)) {
		t.Fatalf("rewritten body missing platform rewrite: %s", text)
	}
	if !bytes.Contains(rewritten, []byte(`/Users/jack/projects`)) {
		t.Fatalf("rewritten body missing working dir rewrite: %s", text)
	}
}

func TestHandlerInjectsBrokerTokenAndRewritesRequest(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "alice" || password != "secret" {
			t.Fatalf("unexpected broker auth: %v %q %q", ok, username, password)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "broker-token",
			"expires_at":   time.Now().Add(time.Hour).UTC(),
		})
	}))
	defer broker.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer broker-token" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer broker-token")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if !bytes.Contains(body, []byte("canonical-device")) {
			t.Fatalf("body missing canonical device: %s", string(body))
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	config := testClaudeConfig(upstream.URL, upstream.URL, broker.URL)
	handler, err := NewHandler(config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"metadata":{"user_id":"{\"device_id\":\"real-device\"}"}}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request.WithContext(context.Background()))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}
