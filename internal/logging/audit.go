package logging

import (
	"context"
	"log/slog"
	"time"
)

type AuditEvent struct {
	RequestID     string
	StartedAt     time.Time
	SourceIP      string
	Username      string
	Method        string
	Destination   string
	ResolvedIP    string
	ProxyStatus   int
	UpstreamStatus int
	BytesUp       int64
	BytesDown     int64
	Duration      time.Duration
	ErrorCategory string
	CloseReason   string
}

func (e AuditEvent) Log(ctx context.Context, logger *slog.Logger) {
	attrs := []any{
		"request_id", e.RequestID,
		"started_at", e.StartedAt.UTC().Format(time.RFC3339Nano),
		"source_ip", e.SourceIP,
		"method", e.Method,
		"destination", e.Destination,
		"proxy_status", e.ProxyStatus,
		"bytes_up", e.BytesUp,
		"bytes_down", e.BytesDown,
		"duration_ms", e.Duration.Milliseconds(),
	}
	if e.Username != "" {
		attrs = append(attrs, "username", e.Username)
	}
	if e.ResolvedIP != "" {
		attrs = append(attrs, "resolved_ip", e.ResolvedIP)
	}
	if e.UpstreamStatus != 0 {
		attrs = append(attrs, "upstream_status", e.UpstreamStatus)
	}
	if e.ErrorCategory != "" {
		attrs = append(attrs, "error_category", e.ErrorCategory)
	}
	if e.CloseReason != "" {
		attrs = append(attrs, "close_reason", e.CloseReason)
	}
	logger.InfoContext(ctx, "proxy request", attrs...)
}
