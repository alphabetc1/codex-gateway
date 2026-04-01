package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"codex-gateway/internal/auth"
	"codex-gateway/internal/limiter"
	"codex-gateway/internal/logging"
	"codex-gateway/internal/netutil"
)

type ctxKey int

const (
	resolvedDialTargetsKey ctxKey = iota + 1
	dialTraceKey
)

const metricOtherHost = "__other__"

var metricDurationBuckets = []time.Duration{
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

type Metrics struct {
	startedAt time.Time

	totalRequests     atomic.Uint64
	activeTunnels     atomic.Int64
	authFailures      atomic.Uint64
	sourceDenied      atomic.Uint64
	concurrencyDenied atomic.Uint64
	destinationDenied atomic.Uint64
	upstreamFailures  atomic.Uint64
	badRequests       atomic.Uint64

	mu                    sync.Mutex
	requestDurationCounts []uint64
	setupDurationCounts   []uint64
	requestDurationSum    float64
	setupDurationSum      float64
	setupDurationTotal    uint64
	hostStats             map[string]*HostStats
}

type Snapshot struct {
	StartedAt         time.Time
	TotalRequests     uint64
	ActiveTunnels     int64
	AuthFailures      uint64
	SourceDenied      uint64
	ConcurrencyDenied uint64
	DestinationDenied uint64
	UpstreamFailures  uint64
	BadRequests       uint64
	RequestDuration   HistogramSnapshot
	SetupDuration     HistogramSnapshot
	Hosts             []HostSnapshot
}

type HistogramBucket struct {
	UpperBoundSeconds float64
	Count             uint64
}

type HistogramSnapshot struct {
	Buckets []HistogramBucket
	Sum     float64
	Count   uint64
}

type HostSnapshot struct {
	Host               string
	Requests           uint64
	DialAttempts       uint64
	RequestDurationSum float64
	SetupDurationSum   float64
}

type HostStats struct {
	Requests           uint64
	DialAttempts       uint64
	RequestDurationSum float64
	SetupDurationSum   float64
}

type dialTrace struct {
	selectedAddress string
	attempts        int
}

func NewMetrics() *Metrics {
	return &Metrics{
		startedAt:             time.Now().UTC(),
		requestDurationCounts: make([]uint64, len(metricDurationBuckets)+1),
		setupDurationCounts:   make([]uint64, len(metricDurationBuckets)+1),
		hostStats:             make(map[string]*HostStats),
	}
}

func (m *Metrics) Record(event logging.AuditEvent) {
	m.totalRequests.Add(1)
	switch event.ErrorCategory {
	case CategoryAuthFailed:
		m.authFailures.Add(1)
	case CategorySourceDenied:
		m.sourceDenied.Add(1)
	case CategoryConcurrencyLimit:
		m.concurrencyDenied.Add(1)
	case CategoryDestinationDenied:
		m.destinationDenied.Add(1)
	case CategoryUpstreamDialFailed, CategoryUpstreamTimeout, CategoryTunnelIO:
		m.upstreamFailures.Add(1)
	case CategoryBadRequest:
		m.badRequests.Add(1)
	}

	host := metricHost(event.Destination)
	requestDurationSeconds := event.Duration.Seconds()
	setupDurationSeconds := event.UpstreamSetupDuration.Seconds()

	m.mu.Lock()
	defer m.mu.Unlock()
	recordDurationBucket(m.requestDurationCounts, event.Duration)
	m.requestDurationSum += requestDurationSeconds
	if event.UpstreamSetupDuration > 0 {
		recordDurationBucket(m.setupDurationCounts, event.UpstreamSetupDuration)
		m.setupDurationSum += setupDurationSeconds
		m.setupDurationTotal++
	}

	if host != "" {
		stats := m.hostStats[host]
		if stats == nil {
			if len(m.hostStats) >= 64 {
				host = metricOtherHost
				stats = m.hostStats[host]
			}
			if stats == nil {
				stats = &HostStats{}
				m.hostStats[host] = stats
			}
		}
		stats.Requests++
		stats.DialAttempts += uint64(event.DialAttempts)
		stats.RequestDurationSum += requestDurationSeconds
		stats.SetupDurationSum += setupDurationSeconds
	}
}

func (m *Metrics) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	hosts := make([]HostSnapshot, 0, len(m.hostStats))
	for host, stats := range m.hostStats {
		hosts = append(hosts, HostSnapshot{
			Host:               host,
			Requests:           stats.Requests,
			DialAttempts:       stats.DialAttempts,
			RequestDurationSum: stats.RequestDurationSum,
			SetupDurationSum:   stats.SetupDurationSum,
		})
	}
	sort.Slice(hosts, func(i, j int) bool {
		if hosts[i].Requests == hosts[j].Requests {
			return hosts[i].Host < hosts[j].Host
		}
		return hosts[i].Requests > hosts[j].Requests
	})

	return Snapshot{
		StartedAt:         m.startedAt,
		TotalRequests:     m.totalRequests.Load(),
		ActiveTunnels:     m.activeTunnels.Load(),
		AuthFailures:      m.authFailures.Load(),
		SourceDenied:      m.sourceDenied.Load(),
		ConcurrencyDenied: m.concurrencyDenied.Load(),
		DestinationDenied: m.destinationDenied.Load(),
		UpstreamFailures:  m.upstreamFailures.Load(),
		BadRequests:       m.badRequests.Load(),
		RequestDuration:   buildHistogramSnapshot(m.requestDurationCounts, m.requestDurationSum, m.totalRequests.Load()),
		SetupDuration:     buildHistogramSnapshot(m.setupDurationCounts, m.setupDurationSum, m.setupDurationTotal),
		Hosts:             hosts,
	}
}

type Options struct {
	AppLogger        *slog.Logger
	AuditLogger      *slog.Logger
	AccessLogEnabled bool

	AuthStore auth.UserStore
	Limiter   *limiter.ConcurrencyLimiter
	Policy    Policy
	SourceIPs netutil.PrefixMatcher
	Metrics   *Metrics

	UpstreamDialTimeout           time.Duration
	UpstreamTLSHandshakeTimeout   time.Duration
	UpstreamResponseHeaderTimeout time.Duration
	IdleTimeout                   time.Duration
	TunnelIdleTimeout             time.Duration
}

type Handler struct {
	appLogger        *slog.Logger
	auditLogger      *slog.Logger
	accessLogEnabled bool

	runtime atomic.Value
	limiter *limiter.ConcurrencyLimiter
	metrics *Metrics

	dialer            net.Dialer
	transport         *http.Transport
	tunnelIdleTimeout time.Duration
}

func NewHandler(options Options) *Handler {
	handler := &Handler{
		appLogger:        options.AppLogger,
		auditLogger:      options.AuditLogger,
		accessLogEnabled: options.AccessLogEnabled,
		limiter:          options.Limiter,
		metrics:          options.Metrics,
		dialer: net.Dialer{
			Timeout:   options.UpstreamDialTimeout,
			KeepAlive: 30 * time.Second,
		},
		tunnelIdleTimeout: options.TunnelIdleTimeout,
	}

	handler.transport = &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		IdleConnTimeout:       options.IdleTimeout,
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   16,
		ResponseHeaderTimeout: options.UpstreamResponseHeaderTimeout,
		TLSHandshakeTimeout:   options.UpstreamTLSHandshakeTimeout,
		DialContext:           handler.dialContext,
	}

	handler.runtime.Store(newRuntimeState(RuntimeConfig{
		AuthStore: options.AuthStore,
		Policy:    options.Policy,
		SourceIPs: options.SourceIPs,
	}))

	return handler
}

func (h *Handler) UpdateRuntime(config RuntimeConfig) {
	h.runtime.Store(newRuntimeState(config))
	if h.transport != nil {
		h.transport.CloseIdleConnections()
	}
}

func (h *Handler) currentRuntime() *runtimeState {
	return h.runtime.Load().(*runtimeState)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	state := newRequestState(r.Method)
	state.audit.Duration = time.Since(state.audit.StartedAt)
	defer h.finishAudit(r.Context(), state)
	runtime := h.currentRuntime()

	sourceIP, err := netutil.ParseRemoteIP(r.RemoteAddr)
	if err != nil {
		h.writeProxyError(w, state, badRequest("remote address is invalid", err))
		return
	}
	state.audit.SourceIP = sourceIP.String()

	if !runtime.sourceIPs.Allow(sourceIP) {
		h.writeProxyError(w, state, &HandlerError{
			Status:   http.StatusForbidden,
			Category: CategorySourceDenied,
			Message:  "source IP is not allowed",
		})
		return
	}

	if !h.limiter.Acquire(state.audit.SourceIP) {
		h.writeProxyError(w, state, &HandlerError{
			Status:   http.StatusTooManyRequests,
			Category: CategoryConcurrencyLimit,
			Message:  "source IP concurrency limit exceeded",
		})
		return
	}
	defer h.limiter.Release(state.audit.SourceIP)

	credentials, authErr := auth.ParseProxyAuthorization(r.Header.Get("Proxy-Authorization"))
	if authErr != nil {
		h.writeProxyError(w, state, &HandlerError{
			Status:   http.StatusProxyAuthRequired,
			Category: CategoryAuthFailed,
			Message:  "proxy authentication required",
			Err:      authErr,
		})
		return
	}

	ok, authStoreErr := runtime.authStore.Authenticate(credentials.Username, credentials.Password)
	if authStoreErr != nil {
		h.writeProxyError(w, state, &HandlerError{
			Status:   http.StatusInternalServerError,
			Category: CategoryInternal,
			Message:  "proxy authentication backend failed",
			Err:      authStoreErr,
		})
		return
	}
	if !ok {
		h.writeProxyError(w, state, &HandlerError{
			Status:   http.StatusProxyAuthRequired,
			Category: CategoryAuthFailed,
			Message:  "proxy authentication required",
		})
		return
	}
	state.audit.Username = credentials.Username

	if r.Method == http.MethodConnect {
		h.handleConnect(w, r, state, runtime)
		return
	}
	h.handleForwardHTTP(w, r, state, runtime)
}

func (h *Handler) finishAudit(ctx context.Context, state *requestState) {
	state.audit.Duration = time.Since(state.audit.StartedAt)
	if h.metrics != nil {
		h.metrics.Record(state.audit)
	}
	if h.accessLogEnabled && h.auditLogger != nil {
		state.audit.Log(ctx, h.auditLogger)
	}
}

func (h *Handler) writeProxyError(w http.ResponseWriter, state *requestState, err *HandlerError) {
	if err == nil {
		return
	}

	state.audit.ProxyStatus = err.Status
	if state.audit.ErrorCategory == "" {
		state.audit.ErrorCategory = err.Category
	}
	if state.audit.CloseReason == "" && err.Err != nil {
		state.audit.CloseReason = err.Err.Error()
	}

	switch err.Status {
	case http.StatusProxyAuthRequired:
		auth.WriteProxyAuthRequired(w)
	default:
		http.Error(w, err.Message, err.Status)
	}

	if h.appLogger != nil {
		attrs := []any{
			"request_id", state.audit.RequestID,
			"source_ip", state.audit.SourceIP,
			"category", err.Category,
			"status", err.Status,
		}
		if err.Err != nil {
			attrs = append(attrs, "error", err.Err.Error())
		}
		h.appLogger.Warn("proxy request failed", attrs...)
	}
}

func (h *Handler) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if targets, ok := ctx.Value(resolvedDialTargetsKey).([]string); ok && len(targets) > 0 {
		conn, trace, err := h.dialResolvedTargets(ctx, network, targets)
		if err != nil {
			return nil, err
		}
		recordDialTrace(ctx, trace)
		return conn, nil
	}
	return h.dialer.DialContext(ctx, network, address)
}

func (h *Handler) dialResolvedTargets(ctx context.Context, network string, addresses []string) (net.Conn, dialTrace, error) {
	if len(addresses) == 0 {
		return nil, dialTrace{}, errors.New("no dial targets")
	}

	var lastErr error
	for index, address := range addresses {
		conn, err := h.dialer.DialContext(ctx, network, address)
		if err == nil {
			return conn, dialTrace{selectedAddress: address, attempts: index + 1}, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, dialTrace{}, ctx.Err()
		}
	}
	return nil, dialTrace{}, lastErr
}

func withResolvedDialTargets(ctx context.Context, addresses []string, trace *dialTrace) context.Context {
	ctx = context.WithValue(ctx, resolvedDialTargetsKey, append([]string(nil), addresses...))
	return context.WithValue(ctx, dialTraceKey, trace)
}

func recordDialTrace(ctx context.Context, trace dialTrace) {
	recorded, ok := ctx.Value(dialTraceKey).(*dialTrace)
	if !ok || recorded == nil {
		return
	}
	*recorded = trace
}

func dialAddressIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	return strings.Trim(host, "[]")
}

func recordDurationBucket(counts []uint64, duration time.Duration) {
	index := len(metricDurationBuckets)
	for i, bound := range metricDurationBuckets {
		if duration <= bound {
			index = i
			break
		}
	}
	counts[index]++
}

func buildHistogramSnapshot(counts []uint64, sum float64, total uint64) HistogramSnapshot {
	buckets := make([]HistogramBucket, 0, len(counts))
	cumulative := uint64(0)
	for i, count := range counts {
		cumulative += count
		upperBound := "+Inf"
		if i < len(metricDurationBuckets) {
			upperBound = formatMetricFloat(metricDurationBuckets[i].Seconds())
		}
		value := 0.0
		if upperBound != "+Inf" {
			value = metricDurationBuckets[i].Seconds()
		}
		buckets = append(buckets, HistogramBucket{
			UpperBoundSeconds: value,
			Count:             cumulative,
		})
	}
	return HistogramSnapshot{
		Buckets: buckets,
		Sum:     sum,
		Count:   total,
	}
}

func metricHost(destination string) string {
	if destination == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(destination)
	if err == nil {
		return netutil.NormalizeHost(host)
	}
	return netutil.NormalizeHost(destination)
}

func formatMetricFloat(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", value), "0"), ".")
}

func classifyTransportError(err error) *HandlerError {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return upstreamTimeout("upstream timeout", err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return upstreamTimeout("upstream timeout", err)
	}
	return upstreamDialFailed("upstream request failed", err)
}

func isBenignCopyError(err error) bool {
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && !netErr.Timeout()
}
