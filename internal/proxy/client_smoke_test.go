package proxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientSmokeThroughProxy(t *testing.T) {
	upstreamHost := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != upstreamHost {
			t.Errorf("host = %q, want %q", r.Host, upstreamHost)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer upstream-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer upstream-token")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxy-http-ok"))
	}))
	defer upstream.Close()

	addr, port := listenerAddrPort(t, upstream.Listener.Addr())
	upstreamHost = fmt.Sprintf("%s:%d", testHostName, port)
	server, _, _ := startProxyServer(t, proxyTestOptions{
		resolver: staticResolver{
			testHostName: {addr},
		},
		allowedHosts: map[string]struct{}{testHostName: {}},
		allowedPorts: []uint16{port},
		allowPrivate: true,
		maxConns:     4,
	})

	client := newProxyHTTPClient(t, server.URL, false)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d/v1/messages", testHostName, port), nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer upstream-token")

	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := strings.TrimSpace(string(body)); got != "proxy-http-ok" {
		t.Fatalf("body = %q, want %q", got, "proxy-http-ok")
	}
}

func TestHTTPSClientSmokeThroughProxy(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/messages" {
			t.Errorf("path = %q, want %q", got, "/v1/messages")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxy-https-ok"))
	}))
	defer upstream.Close()

	addr, port := listenerAddrPort(t, upstream.Listener.Addr())
	server, _, _ := startProxyServer(t, proxyTestOptions{
		resolver: staticResolver{
			testHostName: {addr},
		},
		allowedHosts: map[string]struct{}{testHostName: {}},
		allowedPorts: []uint16{port},
		allowPrivate: true,
		maxConns:     4,
	})

	client := newProxyHTTPClient(t, server.URL, true)
	response, err := client.Get(fmt.Sprintf("https://%s:%d/v1/messages", testHostName, port))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := strings.TrimSpace(string(body)); got != "proxy-https-ok" {
		t.Fatalf("body = %q, want %q", got, "proxy-https-ok")
	}
}

func newProxyHTTPClient(t *testing.T, rawProxyURL string, insecureTLS bool) *http.Client {
	t.Helper()

	proxyURL, err := url.Parse(rawProxyURL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", rawProxyURL, err)
	}
	proxyURL.User = url.UserPassword(testUsername, testPassword)

	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	if insecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
}
