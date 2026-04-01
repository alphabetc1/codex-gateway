package claudeclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"codex-gateway/internal/logging"
)

type tokenManager struct {
	brokerURL *url.URL
	username  string
	password  string
	client    *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	lastErr   error
}

func newTokenManager(config Config) (*tokenManager, error) {
	brokerURL, err := url.Parse(config.OAuthBrokerURL)
	if err != nil {
		return nil, fmt.Errorf("parse oauth broker url: %w", err)
	}
	return &tokenManager{
		brokerURL: brokerURL,
		username:  config.OAuthUsername,
		password:  config.OAuthPassword,
		client:    &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (m *tokenManager) AccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.token != "" && time.Until(m.expiresAt) > 5*time.Minute {
		return m.token, nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, m.brokerURL.String(), http.NoBody)
	if err != nil {
		return "", err
	}
	request.SetBasicAuth(m.username, m.password)
	request.Header.Set("Accept", "application/json")

	response, err := m.client.Do(request)
	if err != nil {
		m.lastErr = err
		return "", err
	}
	defer response.Body.Close()

	var payload struct {
		AccessToken string    `json:"access_token"`
		ExpiresAt   time.Time `json:"expires_at"`
		Error       string    `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		m.lastErr = err
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		if payload.Error == "" {
			payload.Error = http.StatusText(response.StatusCode)
		}
		m.lastErr = fmt.Errorf("oauth broker error: %s", payload.Error)
		return "", m.lastErr
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		m.lastErr = fmt.Errorf("oauth broker returned empty access_token")
		return "", m.lastErr
	}

	m.token = payload.AccessToken
	m.expiresAt = payload.ExpiresAt
	m.lastErr = nil
	return m.token, nil
}

func (m *tokenManager) Status() (bool, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.token != "" && time.Now().Before(m.expiresAt) {
		return true, m.expiresAt, nil
	}
	return false, m.expiresAt, m.lastErr
}

type handler struct {
	config      Config
	logger      *slog.Logger
	upstreamURL *url.URL
	httpClient  *http.Client
	tokens      *tokenManager
}

func Run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("claude-client", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "claude-client.yaml", "path to Claude client YAML")
	if err := fs.Parse(args); err != nil {
		return err
	}

	config, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	loggers, err := logging.New(config.LogLevel, config.LogFormat)
	if err != nil {
		return err
	}

	handler, err := NewHandler(config, loggers.App)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		loggers.App.Info("claude client starting",
			"addr", config.ListenAddr,
			"upstream", config.UpstreamURL,
			"proxy_url", config.ProxyURL,
			"oauth_broker_url", config.OAuthBrokerURL,
		)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func NewHandler(config Config, logger *slog.Logger) (http.Handler, error) {
	upstreamURL, err := url.Parse(config.UpstreamURL)
	if err != nil {
		return nil, err
	}
	proxyURL, err := url.Parse(config.ProxyURL)
	if err != nil {
		return nil, err
	}
	tokens, err := newTokenManager(config)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		Proxy:             http.ProxyURL(proxyURL),
		ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: config.TLSInsecureSkipVerify,
		},
	}

	return &handler{
		config:      config,
		logger:      logger,
		upstreamURL: upstreamURL,
		httpClient:  &http.Client{Transport: transport},
		tokens:      tokens,
	}, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/_health":
		h.handleHealth(w, r)
		return
	case "/_verify":
		writeJSON(w, http.StatusOK, BuildVerificationPayload(h.config))
		return
	}

	accessToken, err := h.tokens.AccessToken(r.Context())
	if err != nil {
		h.logger.Error("claude oauth token unavailable", "error", err.Error())
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "oauth token not available",
		})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read request body failed"})
		return
	}
	if len(body) > 0 {
		body = RewriteBody(body, requestPath(r), h.config)
	}

	outboundURL := *r.URL
	outboundURL.Scheme = h.upstreamURL.Scheme
	outboundURL.Host = h.upstreamURL.Host

	outboundRequest, err := http.NewRequestWithContext(r.Context(), r.Method, outboundURL.String(), bytes.NewReader(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "build upstream request failed"})
		return
	}
	outboundRequest.Header = RewriteHeaders(r.Header, h.config, accessToken)
	outboundRequest.Header.Set("Host", h.upstreamURL.Host)
	outboundRequest.ContentLength = int64(len(body))
	outboundRequest.Host = h.upstreamURL.Host

	response, err := h.httpClient.Do(outboundRequest)
	if err != nil {
		h.logger.Error("claude upstream request failed", "error", err.Error(), "path", requestPath(r))
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "bad gateway", "detail": err.Error()})
		return
	}
	defer response.Body.Close()

	copyHeaders(w.Header(), response.Header)
	w.Header().Del("Transfer-Encoding")
	w.WriteHeader(response.StatusCode)
	if _, err := io.Copy(w, response.Body); err != nil {
		h.logger.Warn("claude response stream failed", "error", err.Error(), "path", requestPath(r))
	}
}

func (h *handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, tokenErr := h.tokens.AccessToken(ctx)
	ok, expiresAt, cachedErr := h.tokens.Status()
	if tokenErr != nil && cachedErr == nil {
		cachedErr = tokenErr
	}

	status := http.StatusOK
	state := "ok"
	if !ok {
		status = http.StatusServiceUnavailable
		state = "degraded"
	}

	payload := map[string]any{
		"status":             state,
		"oauth":              map[string]any{"ready": ok, "expires_at": expiresAt},
		"canonical_device":   truncateID(h.config.Identity.DeviceID),
		"canonical_platform": h.config.Env.Platform,
		"upstream":           h.config.UpstreamURL,
		"proxy_url":          h.config.ProxyURL,
		"oauth_broker_url":   h.config.OAuthBrokerURL,
	}
	if cachedErr != nil {
		payload["oauth_error"] = cachedErr.Error()
	}

	writeJSON(w, status, payload)
}

func requestPath(r *http.Request) string {
	if r.URL == nil {
		return "/"
	}
	if r.URL.RawQuery == "" {
		return r.URL.Path
	}
	return r.URL.Path + "?" + r.URL.RawQuery
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func truncateID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return value
	}
	return value[:8] + "..."
}
