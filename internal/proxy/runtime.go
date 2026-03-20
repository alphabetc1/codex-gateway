package proxy

import (
	"codex-gateway/internal/auth"
	"codex-gateway/internal/netutil"
)

type RuntimeConfig struct {
	AuthStore auth.UserStore
	Policy    Policy
	SourceIPs netutil.PrefixMatcher
}

type runtimeState struct {
	authStore auth.UserStore
	policy    Policy
	sourceIPs netutil.PrefixMatcher
}

func newRuntimeState(config RuntimeConfig) *runtimeState {
	if config.AuthStore == nil {
		panic("proxy runtime auth store must not be nil")
	}
	return &runtimeState{
		authStore: config.AuthStore,
		policy:    config.Policy,
		sourceIPs: config.SourceIPs,
	}
}
