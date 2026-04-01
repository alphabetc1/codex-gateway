package proxy

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"codex-gateway/internal/auth"
	"codex-gateway/internal/limiter"
	"codex-gateway/internal/logging"
	"codex-gateway/internal/netutil"
)

const (
	testUsername = "alice"
	testPassword = "proxy-secret"
	testHostName = "allowed.example"
)

type proxyTestOptions struct {
	resolver         staticResolver
	sourcePrefixes   []netip.Prefix
	allowedHosts     map[string]struct{}
	allowedSuffixes  []string
	allowedPorts     []uint16
	allowPrivate     bool
	maxConns         int
	accessLogEnabled bool
}

func TestSourceAllowlistDenied(t *testing.T) {
	server, _, _ := startProxyServer(t, proxyTestOptions{
		resolver: staticResolver{
			testHostName: {netip.MustParseAddr("127.0.0.1")},
		},
		sourcePrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		allowedHosts:   map[string]struct{}{testHostName: {}},
		allowedPorts:   []uint16{8080},
		allowPrivate:   true,
		maxConns:       2,
	})

	response := doRawHTTP(t, proxyAddress(server), rawForwardRequest(8080))
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
}

func TestHTTPForwardAbsoluteFormAndHopHeaders(t *testing.T) {
	var seenHeaders http.Header

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeaders = r.Header.Clone()
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want %q", r.URL.Path, "/v1/messages")
		}
		w.Header().Set("Connection", "X-Remove-Resp")
		w.Header().Set("X-Remove-Resp", "secret")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("stream-ok"))
	}))
	defer upstream.Close()

	addr, port := listenerAddrPort(t, upstream.Listener.Addr())
	server, appLogs, auditLogs := startProxyServer(t, proxyTestOptions{
		resolver: staticResolver{
			testHostName: {addr},
		},
		allowedHosts:     map[string]struct{}{testHostName: {}},
		allowedPorts:     []uint16{port},
		allowPrivate:     true,
		maxConns:         4,
		accessLogEnabled: true,
	})

	response := doRawHTTP(t, proxyAddress(server), rawForwardRequest(port))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if body := strings.TrimSpace(response.Body); body != "stream-ok" {
		t.Fatalf("body = %q, want %q", body, "stream-ok")
	}
	if got := seenHeaders.Get("Proxy-Authorization"); got != "" {
		t.Fatalf("upstream Proxy-Authorization = %q, want empty", got)
	}
	if got := seenHeaders.Get("Proxy-Connection"); got != "" {
		t.Fatalf("upstream Proxy-Connection = %q, want empty", got)
	}
	if got := seenHeaders.Get("X-Proxy-Secret"); got != "" {
		t.Fatalf("upstream X-Proxy-Secret = %q, want empty", got)
	}
	if got := seenHeaders.Get("Authorization"); got != "Bearer upstream-token" {
		t.Fatalf("upstream Authorization = %q, want %q", got, "Bearer upstream-token")
	}
	if got := response.Header.Get("Connection"); got != "" {
		t.Fatalf("client Connection header = %q, want empty", got)
	}
	if got := response.Header.Get("X-Remove-Resp"); got != "" {
		t.Fatalf("client X-Remove-Resp = %q, want empty", got)
	}

	combinedLogs := appLogs.String() + auditLogs.String()
	for _, secret := range []string{"Proxy-Authorization", testPassword, "upstream-token"} {
		if strings.Contains(combinedLogs, secret) {
			t.Fatalf("logs contain sensitive value %q: %s", secret, combinedLogs)
		}
	}
}

func TestHTTPForwardFallsBackToNextResolvedAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fallback-ok"))
	}))
	defer upstream.Close()

	addr, port := listenerAddrPort(t, upstream.Listener.Addr())
	server, _, auditLogs := startProxyServer(t, proxyTestOptions{
		resolver: staticResolver{
			testHostName: {
				netip.MustParseAddr("127.0.0.2"),
				addr,
			},
		},
		allowedHosts:     map[string]struct{}{testHostName: {}},
		allowedPorts:     []uint16{port},
		allowPrivate:     true,
		maxConns:         4,
		accessLogEnabled: true,
	})

	response := doRawHTTP(t, proxyAddress(server), rawForwardRequest(port))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if body := strings.TrimSpace(response.Body); body != "fallback-ok" {
		t.Fatalf("body = %q, want %q", body, "fallback-ok")
	}
	if !strings.Contains(auditLogs.String(), "\"resolved_ip\":\""+addr.String()+"\"") {
		t.Fatalf("audit log missing fallback resolved ip %q: %s", addr.String(), auditLogs.String())
	}
}

func TestConcurrencyLimitReturns429(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
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
		maxConns:     1,
	})

	firstConn, firstReader := openRawConn(t, proxyAddress(server))
	defer firstConn.Close()
	if _, err := io.WriteString(firstConn, rawForwardRequest(port)); err != nil {
		t.Fatalf("write first request: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach upstream")
	}

	response := doRawHTTP(t, proxyAddress(server), rawForwardRequest(port))
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusTooManyRequests)
	}

	close(release)

	firstResponse, err := http.ReadResponse(firstReader, nil)
	if err != nil {
		t.Fatalf("read first response: %v", err)
	}
	defer firstResponse.Body.Close()
}

func TestCONNECTAuthFailureReturns407(t *testing.T) {
	server, _, _ := startProxyServer(t, proxyTestOptions{
		resolver: staticResolver{
			testHostName: {netip.MustParseAddr("127.0.0.1")},
		},
		allowedHosts: map[string]struct{}{testHostName: {}},
		allowedPorts: []uint16{443},
		allowPrivate: true,
		maxConns:     2,
	})

	response := doRawHTTP(t, proxyAddress(server), "CONNECT allowed.example:443 HTTP/1.1\r\nHost: allowed.example:443\r\n\r\n")
	if response.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusProxyAuthRequired)
	}
	if got := response.Header.Get("Proxy-Authenticate"); got != auth.ProxyAuthenticateValue {
		t.Fatalf("Proxy-Authenticate = %q, want %q", got, auth.ProxyAuthenticateValue)
	}
}

func TestDeniedCONNECTAuditIncludesDestination(t *testing.T) {
	server, _, auditLogs := startProxyServer(t, proxyTestOptions{
		resolver: staticResolver{
			testHostName: {netip.MustParseAddr("127.0.0.1")},
		},
		allowedHosts: map[string]struct{}{testHostName: {}},
		allowedPorts: []uint16{443},
		allowPrivate: true,
		maxConns:     2,
	})

	response := doRawHTTP(t, proxyAddress(server), "CONNECT denied.example:443 HTTP/1.1\r\nHost: denied.example:443\r\nProxy-Authorization: "+proxyAuthorizationHeader()+"\r\n\r\n")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
	if !strings.Contains(auditLogs.String(), "\"destination\":\"denied.example:443\"") {
		t.Fatalf("audit log missing denied destination: %s", auditLogs.String())
	}
}

func TestCONNECTAuthorityFormTunnel(t *testing.T) {
	addr, port := startTCPServer(t, func(conn net.Conn) {
		defer conn.Close()
		buffer := make([]byte, 4)
		if _, err := io.ReadFull(conn, buffer); err != nil {
			return
		}
		_, _ = conn.Write(buffer)
	})

	server, _, _ := startProxyServer(t, proxyTestOptions{
		resolver: staticResolver{
			testHostName: {addr},
		},
		allowedHosts: map[string]struct{}{testHostName: {}},
		allowedPorts: []uint16{port},
		allowPrivate: true,
		maxConns:     2,
	})

	conn, reader := openRawConn(t, proxyAddress(server))
	defer conn.Close()
	if _, err := io.WriteString(conn, rawConnectRequest(port)); err != nil {
		t.Fatalf("write CONNECT request: %v", err)
	}

	status, _, err := readConnectResponse(reader)
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write tunnel payload: %v", err)
	}

	reply := make([]byte, 4)
	if _, err := io.ReadFull(reader, reply); err != nil {
		t.Fatalf("read tunnel reply: %v", err)
	}
	if string(reply) != "ping" {
		t.Fatalf("reply = %q, want %q", string(reply), "ping")
	}
}

func TestCONNECTHalfCloseDoesNotDeadlock(t *testing.T) {
	addr, port := startTCPServer(t, func(conn net.Conn) {
		defer conn.Close()
		payload, err := io.ReadAll(conn)
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("ack:" + string(payload)))
	})

	server, _, _ := startProxyServer(t, proxyTestOptions{
		resolver: staticResolver{
			testHostName: {addr},
		},
		allowedHosts: map[string]struct{}{testHostName: {}},
		allowedPorts: []uint16{port},
		allowPrivate: true,
		maxConns:     2,
	})

	conn, reader := openRawConn(t, proxyAddress(server))
	defer conn.Close()
	if _, err := io.WriteString(conn, rawConnectRequest(port)); err != nil {
		t.Fatalf("write CONNECT request: %v", err)
	}

	status, _, err := readConnectResponse(reader)
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("write tunnel payload: %v", err)
	}
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		t.Fatalf("connection type = %T, want *net.TCPConn", conn)
	}
	if err := tcpConn.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite() error = %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	reply := make([]byte, len("ack:hello"))
	if _, err := io.ReadFull(reader, reply); err != nil {
		t.Fatalf("read tunnel reply: %v", err)
	}
	if string(reply) != "ack:hello" {
		t.Fatalf("reply = %q, want %q", string(reply), "ack:hello")
	}
}

func TestHandlerUpdateRuntimeSwapsAllowlistAndAuth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("reload-ok"))
	}))
	defer upstream.Close()

	addr, port := listenerAddrPort(t, upstream.Listener.Addr())

	appLogs := &bytes.Buffer{}
	auditLogs := &bytes.Buffer{}
	loggers, err := logging.NewWithWriters("debug", "json", appLogs, auditLogs)
	if err != nil {
		t.Fatalf("NewWithWriters() error = %v", err)
	}

	aliceHash, err := auth.HashPassword(testPassword, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("HashPassword(alice) error = %v", err)
	}

	handler := NewHandler(Options{
		AppLogger:        loggers.App,
		AuditLogger:      loggers.Audit,
		AccessLogEnabled: true,
		AuthStore:        auth.NewMapStore(map[string]string{testUsername: aliceHash}),
		Limiter:          limiter.New(4),
		Policy: Policy{
			AllowedPorts: map[uint16]struct{}{port: {}},
			HostMatcher:  netutil.NewHostMatcher(map[string]struct{}{"denied.example": {}}, nil),
			Resolver:     staticResolver{testHostName: {addr}},
			AllowPrivate: true,
		},
		SourceIPs:                     netutil.NewPrefixMatcher(nil),
		Metrics:                       NewMetrics(),
		UpstreamDialTimeout:           2 * time.Second,
		UpstreamTLSHandshakeTimeout:   2 * time.Second,
		UpstreamResponseHeaderTimeout: 2 * time.Second,
		IdleTimeout:                   2 * time.Second,
		TunnelIdleTimeout:             2 * time.Second,
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	response := doRawHTTP(t, proxyAddress(server), rawForwardRequestWithAuth(port, testUsername, testPassword))
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status before reload = %d, want %d", response.StatusCode, http.StatusForbidden)
	}

	bobHash, err := auth.HashPassword("next-secret", bcrypt.MinCost)
	if err != nil {
		t.Fatalf("HashPassword(bob) error = %v", err)
	}

	handler.UpdateRuntime(RuntimeConfig{
		AuthStore: auth.NewMapStore(map[string]string{"bob": bobHash}),
		Policy: Policy{
			AllowedPorts: map[uint16]struct{}{port: {}},
			HostMatcher:  netutil.NewHostMatcher(map[string]struct{}{testHostName: {}}, nil),
			Resolver:     staticResolver{testHostName: {addr}},
			AllowPrivate: true,
		},
		SourceIPs: netutil.NewPrefixMatcher(nil),
	})

	response = doRawHTTP(t, proxyAddress(server), rawForwardRequestWithAuth(port, testUsername, testPassword))
	if response.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("status with stale credentials = %d, want %d", response.StatusCode, http.StatusProxyAuthRequired)
	}

	response = doRawHTTP(t, proxyAddress(server), rawForwardRequestWithAuth(port, "bob", "next-secret"))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status after reload = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if body := strings.TrimSpace(response.Body); body != "reload-ok" {
		t.Fatalf("body after reload = %q, want %q", body, "reload-ok")
	}
}

func startProxyServer(t *testing.T, options proxyTestOptions) (*httptest.Server, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	if options.maxConns == 0 {
		options.maxConns = 4
	}
	if !options.accessLogEnabled {
		options.accessLogEnabled = true
	}
	if len(options.allowedPorts) == 0 {
		options.allowedPorts = []uint16{443}
	}

	appLogs := &bytes.Buffer{}
	auditLogs := &bytes.Buffer{}
	loggers, err := logging.NewWithWriters("debug", "json", appLogs, auditLogs)
	if err != nil {
		t.Fatalf("NewWithWriters() error = %v", err)
	}

	hash, err := auth.HashPassword(testPassword, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	allowedPorts := make(map[uint16]struct{}, len(options.allowedPorts))
	for _, port := range options.allowedPorts {
		allowedPorts[port] = struct{}{}
	}

	handler := NewHandler(Options{
		AppLogger:        loggers.App,
		AuditLogger:      loggers.Audit,
		AccessLogEnabled: options.accessLogEnabled,
		AuthStore:        auth.NewMapStore(map[string]string{testUsername: hash}),
		Limiter:          limiter.New(options.maxConns),
		Policy: Policy{
			AllowedPorts: allowedPorts,
			HostMatcher:  netutil.NewHostMatcher(options.allowedHosts, options.allowedSuffixes),
			Resolver:     options.resolver,
			AllowPrivate: options.allowPrivate,
		},
		SourceIPs:                     netutil.NewPrefixMatcher(options.sourcePrefixes),
		Metrics:                       NewMetrics(),
		UpstreamDialTimeout:           2 * time.Second,
		UpstreamTLSHandshakeTimeout:   2 * time.Second,
		UpstreamResponseHeaderTimeout: 2 * time.Second,
		IdleTimeout:                   2 * time.Second,
		TunnelIdleTimeout:             2 * time.Second,
	})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, appLogs, auditLogs
}

func startTCPServer(t *testing.T, handler func(net.Conn)) (netip.Addr, uint16) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handler(conn)
		}
	}()

	return listenerAddrPort(t, listener.Addr())
}

func listenerAddrPort(t *testing.T, addr net.Addr) (netip.Addr, uint16) {
	t.Helper()

	host, rawPort, err := net.SplitHostPort(addr.String())
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", addr.String(), err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		t.Fatalf("ParseAddr(%q) error = %v", host, err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("Atoi(%q) error = %v", rawPort, err)
	}
	return ip.Unmap(), uint16(port)
}

func proxyAddress(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "http://")
}

func openRawConn(t *testing.T, addr string) (net.Conn, *bufio.Reader) {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial(%q) error = %v", addr, err)
	}
	return conn, bufio.NewReader(conn)
}

type rawHTTPResponse struct {
	StatusCode int
	Header     http.Header
	Body       string
}

func doRawHTTP(t *testing.T, addr, rawRequest string) rawHTTPResponse {
	t.Helper()

	conn, reader := openRawConn(t, addr)
	defer conn.Close()

	if _, err := io.WriteString(conn, rawRequest); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}

	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("ReadResponse() error = %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	return rawHTTPResponse{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       string(body),
	}
}

func readConnectResponse(reader *bufio.Reader) (int, http.Header, error) {
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return 0, nil, err
	}

	parts := strings.SplitN(strings.TrimSpace(statusLine), " ", 3)
	if len(parts) < 2 {
		return 0, nil, fmt.Errorf("invalid status line %q", statusLine)
	}

	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, nil, err
	}

	headers, err := textproto.NewReader(reader).ReadMIMEHeader()
	if err != nil {
		return 0, nil, err
	}

	return statusCode, http.Header(headers), nil
}

func rawForwardRequest(port uint16) string {
	return rawForwardRequestWithAuth(port, testUsername, testPassword)
}

func rawForwardRequestWithAuth(port uint16, username, password string) string {
	return fmt.Sprintf(
		"GET http://%s:%d/v1/messages HTTP/1.1\r\nHost: %s:%d\r\nAuthorization: Bearer upstream-token\r\nConnection: keep-alive, X-Proxy-Secret\r\nX-Proxy-Secret: drop-me\r\nProxy-Connection: keep-alive\r\nProxy-Authorization: %s\r\n\r\n",
		testHostName,
		port,
		testHostName,
		port,
		proxyAuthorizationHeaderFor(username, password),
	)
}

func rawConnectRequest(port uint16) string {
	return fmt.Sprintf(
		"CONNECT %s:%d HTTP/1.1\r\nHost: %s:%d\r\nProxy-Authorization: %s\r\n\r\n",
		testHostName,
		port,
		testHostName,
		port,
		proxyAuthorizationHeader(),
	)
}

func proxyAuthorizationHeader() string {
	return proxyAuthorizationHeaderFor(testUsername, testPassword)
}

func proxyAuthorizationHeaderFor(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}
