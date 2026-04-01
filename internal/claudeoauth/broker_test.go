package claudeoauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBrokerRefreshesAndCachesToken(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token-next",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	broker, err := New(Config{
		Enabled:      true,
		RefreshToken: "refresh-token",
		TokenURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	first, err := broker.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken() error = %v", err)
	}
	second, err := broker.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken() second error = %v", err)
	}

	if first.AccessToken != "access-token" || second.AccessToken != "access-token" {
		t.Fatalf("unexpected token values: %#v %#v", first, second)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want %d", requests, 1)
	}
	status := broker.Status()
	if !status.Ready {
		t.Fatalf("status.Ready = false, want true")
	}
	if status.ExpiresAt.Before(time.Now()) {
		t.Fatalf("status.ExpiresAt = %s, want future time", status.ExpiresAt)
	}
}

func TestBrokerDisabled(t *testing.T) {
	broker, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if broker.Enabled() {
		t.Fatalf("broker.Enabled() = true, want false")
	}
	if _, err := broker.GetToken(context.Background()); err == nil {
		t.Fatalf("GetToken() error = nil, want non-nil")
	}
}
