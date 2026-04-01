package claudeoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	DefaultTokenURL = "https://platform.claude.com/v1/oauth/token"
	DefaultClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
)

var DefaultScopes = []string{
	"user:inference",
	"user:profile",
	"user:sessions:claude_code",
	"user:mcp_servers",
	"user:file_upload",
}

type Config struct {
	Enabled      bool
	RefreshToken string
	ClientID     string
	Scopes       []string
	TokenURL     string
	HTTPClient   *http.Client
}

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Status struct {
	Enabled     bool      `json:"enabled"`
	Ready       bool      `json:"ready"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	LastRefresh time.Time `json:"last_refresh,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

type Broker struct {
	client *http.Client

	mu           sync.Mutex
	enabled      bool
	refreshToken string
	clientID     string
	scopes       []string
	tokenURL     string
	token        Token
	lastRefresh  time.Time
	lastErr      error
}

func New(config Config) (*Broker, error) {
	broker := &Broker{
		enabled:      config.Enabled,
		refreshToken: strings.TrimSpace(config.RefreshToken),
		clientID:     strings.TrimSpace(config.ClientID),
		scopes:       append([]string(nil), config.Scopes...),
		tokenURL:     strings.TrimSpace(config.TokenURL),
		client:       config.HTTPClient,
	}

	if broker.client == nil {
		broker.client = &http.Client{Timeout: 15 * time.Second}
	}
	if broker.clientID == "" {
		broker.clientID = DefaultClientID
	}
	if broker.tokenURL == "" {
		broker.tokenURL = DefaultTokenURL
	}
	if len(broker.scopes) == 0 {
		broker.scopes = append([]string(nil), DefaultScopes...)
	}

	if !broker.enabled {
		return broker, nil
	}
	if broker.refreshToken == "" {
		return nil, errors.New("claude oauth refresh token is required when broker is enabled")
	}

	return broker, nil
}

func (b *Broker) Enabled() bool {
	if b == nil {
		return false
	}
	return b.enabled
}

func (b *Broker) Warmup(ctx context.Context) error {
	if !b.Enabled() {
		return nil
	}
	_, err := b.GetToken(ctx)
	return err
}

func (b *Broker) Status() Status {
	if b == nil {
		return Status{}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	status := Status{
		Enabled:     b.enabled,
		LastRefresh: b.lastRefresh,
	}
	if b.lastErr != nil {
		status.LastError = b.lastErr.Error()
	}
	if b.token.AccessToken != "" {
		status.ExpiresAt = b.token.ExpiresAt
		status.Ready = time.Now().Before(b.token.ExpiresAt)
	}

	return status
}

func (b *Broker) GetToken(ctx context.Context) (Token, error) {
	if !b.Enabled() {
		return Token{}, errors.New("claude oauth broker is disabled")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if tokenStillValid(b.token) {
		return b.token, nil
	}

	token, err := b.refreshLocked(ctx)
	if err != nil {
		b.lastErr = err
		return Token{}, err
	}

	b.token = token
	b.lastRefresh = time.Now().UTC()
	b.lastErr = nil
	if token.RefreshToken != "" {
		b.refreshToken = token.RefreshToken
	}

	return b.token, nil
}

func tokenStillValid(token Token) bool {
	if token.AccessToken == "" {
		return false
	}
	return time.Until(token.ExpiresAt) > 5*time.Minute
}

func (b *Broker) refreshLocked(ctx context.Context) (Token, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": b.refreshToken,
		"client_id":     b.clientID,
		"scope":         strings.Join(b.scopes, " "),
	})
	if err != nil {
		return Token{}, fmt.Errorf("marshal oauth request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.tokenURL, bytes.NewReader(body))
	if err != nil {
		return Token{}, fmt.Errorf("build oauth request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := b.client.Do(request)
	if err != nil {
		return Token{}, fmt.Errorf("refresh oauth token: %w", err)
	}
	defer response.Body.Close()

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Token{}, fmt.Errorf("decode oauth response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		message := payload.Error
		if message == "" {
			message = http.StatusText(response.StatusCode)
		}
		return Token{}, fmt.Errorf("oauth refresh failed: %s", message)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return Token{}, errors.New("oauth refresh returned empty access_token")
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 3600
	}

	return Token{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).UTC(),
	}, nil
}
