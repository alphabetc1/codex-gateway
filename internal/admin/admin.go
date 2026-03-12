package admin

import (
	"fmt"
	"net/http"
	"time"

	"claude-gateway/internal/proxy"
)

type Options struct {
	MetricsEnabled bool
	Metrics        *proxy.Metrics
	Version        string
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
		})
	}

	return mux
}
