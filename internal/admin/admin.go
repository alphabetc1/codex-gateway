package admin

import (
	"codex-gateway/internal/auth"
	"codex-gateway/internal/claudeoauth"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"codex-gateway/internal/proxy"
)

type Options struct {
	MetricsEnabled bool
	Metrics        *proxy.Metrics
	Version        string
	Runtime        *Runtime
}

type Runtime struct {
	authStore atomic.Value
	broker    *claudeoauth.Broker
}

func NewRuntime(store auth.UserStore, broker *claudeoauth.Broker) *Runtime {
	runtime := &Runtime{broker: broker}
	if store != nil {
		runtime.authStore.Store(store)
	}
	return runtime
}

func (r *Runtime) UpdateAuthStore(store auth.UserStore) {
	if r == nil || store == nil {
		return
	}
	r.authStore.Store(store)
}

func (r *Runtime) currentAuthStore() auth.UserStore {
	if r == nil {
		return nil
	}
	value := r.authStore.Load()
	if value == nil {
		return nil
	}
	store, _ := value.(auth.UserStore)
	return store
}

func NewHandler(options Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	if options.MetricsEnabled && options.Metrics != nil {
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			snapshot := options.Metrics.Snapshot()
			uptime := time.Since(snapshot.StartedAt).Seconds()
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = fmt.Fprintf(w, "claude_gateway_info{version=%q} 1\n", options.Version)
			_, _ = fmt.Fprintf(w, "claude_gateway_uptime_seconds %.0f\n", uptime)
			_, _ = fmt.Fprintf(w, "claude_gateway_requests_total %d\n", snapshot.TotalRequests)
			_, _ = fmt.Fprintf(w, "claude_gateway_active_tunnels %d\n", snapshot.ActiveTunnels)
			_, _ = fmt.Fprintf(w, "claude_gateway_auth_failures_total %d\n", snapshot.AuthFailures)
			_, _ = fmt.Fprintf(w, "claude_gateway_source_denied_total %d\n", snapshot.SourceDenied)
			_, _ = fmt.Fprintf(w, "claude_gateway_concurrency_denied_total %d\n", snapshot.ConcurrencyDenied)
			_, _ = fmt.Fprintf(w, "claude_gateway_destination_denied_total %d\n", snapshot.DestinationDenied)
			_, _ = fmt.Fprintf(w, "claude_gateway_upstream_failures_total %d\n", snapshot.UpstreamFailures)
			_, _ = fmt.Fprintf(w, "claude_gateway_bad_requests_total %d\n", snapshot.BadRequests)
			writeHistogram(w, "claude_gateway_request_duration_seconds", snapshot.RequestDuration)
			writeHistogram(w, "claude_gateway_upstream_setup_duration_seconds", snapshot.SetupDuration)
			for _, host := range snapshot.Hosts {
				_, _ = fmt.Fprintf(w, "claude_gateway_host_requests_total{host=%q} %d\n", host.Host, host.Requests)
				_, _ = fmt.Fprintf(w, "claude_gateway_host_dial_attempts_total{host=%q} %d\n", host.Host, host.DialAttempts)
				_, _ = fmt.Fprintf(w, "claude_gateway_host_request_duration_seconds_sum{host=%q} %.6f\n", host.Host, host.RequestDurationSum)
				_, _ = fmt.Fprintf(w, "claude_gateway_host_request_duration_seconds_count{host=%q} %d\n", host.Host, host.Requests)
				_, _ = fmt.Fprintf(w, "claude_gateway_host_upstream_setup_duration_seconds_sum{host=%q} %.6f\n", host.Host, host.SetupDurationSum)
			}
		})
	}

	if options.Runtime != nil && options.Runtime.broker != nil {
		mux.HandleFunc("/claude/oauth/health", func(w http.ResponseWriter, r *http.Request) {
			if !requireAdminAuth(w, r, options.Runtime.currentAuthStore()) {
				return
			}
			status := options.Runtime.broker.Status()
			httpStatus := http.StatusOK
			if status.Enabled && !status.Ready {
				httpStatus = http.StatusServiceUnavailable
			}
			writeJSON(w, httpStatus, status)
		})
		mux.HandleFunc("/claude/oauth/token", func(w http.ResponseWriter, r *http.Request) {
			if !requireAdminAuth(w, r, options.Runtime.currentAuthStore()) {
				return
			}
			token, err := options.Runtime.broker.GetToken(r.Context())
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{
					"error": err.Error(),
				})
				return
			}
			writeJSON(w, http.StatusOK, token)
		})
	}

	return mux
}

func writeHistogram(w http.ResponseWriter, name string, snapshot proxy.HistogramSnapshot) {
	for index, bucket := range snapshot.Buckets {
		le := "+Inf"
		if index < len(snapshot.Buckets)-1 {
			le = fmt.Sprintf("%g", bucket.UpperBoundSeconds)
		}
		_, _ = fmt.Fprintf(w, "%s_bucket{le=%q} %d\n", name, le, bucket.Count)
	}
	_, _ = fmt.Fprintf(w, "%s_sum %.6f\n", name, snapshot.Sum)
	_, _ = fmt.Fprintf(w, "%s_count %d\n", name, snapshot.Count)
}

func requireAdminAuth(w http.ResponseWriter, r *http.Request, store auth.UserStore) bool {
	if store == nil {
		http.Error(w, "admin auth store unavailable", http.StatusServiceUnavailable)
		return false
	}

	headerValue := r.Header.Get("Authorization")
	if headerValue == "" {
		headerValue = r.Header.Get("Proxy-Authorization")
	}
	credentials, err := auth.ParseProxyAuthorization(headerValue)
	if err != nil {
		writeAdminAuthRequired(w)
		return false
	}

	ok, err := store.Authenticate(credentials.Username, credentials.Password)
	if err != nil {
		http.Error(w, "admin authentication failed", http.StatusInternalServerError)
		return false
	}
	if !ok {
		writeAdminAuthRequired(w)
		return false
	}
	return true
}

func writeAdminAuthRequired(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="codex-gateway-admin"`)
	http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
