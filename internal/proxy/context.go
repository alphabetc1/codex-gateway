package proxy

import (
	"fmt"
	"sync/atomic"
	"time"

	"claude-gateway/internal/logging"
)

var requestCounter atomic.Uint64

type requestState struct {
	audit logging.AuditEvent
}

func newRequestState(method string) *requestState {
	startedAt := time.Now().UTC()
	return &requestState{
		audit: logging.AuditEvent{
			RequestID: fmt.Sprintf("req-%012d", requestCounter.Add(1)),
			StartedAt: startedAt,
			Method:    method,
		},
	}
}
