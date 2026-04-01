package proxy

import (
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

func (h *Handler) handleForwardHTTP(w http.ResponseWriter, r *http.Request, state *requestState, runtime *runtimeState) {
	if r.URL != nil {
		state.audit.Destination = r.URL.Host
	}

	resolution, proxyErr := runtime.policy.ResolveHTTP(r.Context(), r)
	if proxyErr != nil {
		h.writeProxyError(w, state, proxyErr)
		return
	}

	state.audit.Destination = resolution.Destination.Authority()
	setupStartedAt := time.Now()

	outboundURL := cloneURL(r.URL)
	outboundURL.Host = resolution.Destination.Authority()

	trace := &dialTrace{}
	outboundRequest := r.Clone(withResolvedDialTargets(r.Context(), resolution.DialAddresses(), trace))
	outboundRequest.RequestURI = ""
	outboundRequest.URL = outboundURL
	outboundRequest.Host = r.Host
	if outboundRequest.Host == "" {
		outboundRequest.Host = r.URL.Host
	}
	if outboundRequest.Host == "" {
		outboundRequest.Host = resolution.Destination.Authority()
	}
	outboundRequest.Header = r.Header.Clone()
	stripHopHeaders(outboundRequest.Header)

	var upstreamBytes atomic.Int64
	if outboundRequest.Body != nil {
		outboundRequest.Body = &countingReadCloser{
			ReadCloser: outboundRequest.Body,
			count:      &upstreamBytes,
		}
	}

	response, err := h.transport.RoundTrip(outboundRequest)
	if err != nil {
		h.writeProxyError(w, state, classifyTransportError(err))
		return
	}
	defer response.Body.Close()
	state.audit.UpstreamSetupDuration = time.Since(setupStartedAt)
	state.audit.DialAttempts = trace.attempts
	if selectedIP := dialAddressIP(trace.selectedAddress); selectedIP != "" {
		state.audit.ResolvedIP = selectedIP
	} else {
		state.audit.ResolvedIP = resolution.Selected.String()
	}

	stripHopHeaders(response.Header)
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)

	state.audit.ProxyStatus = response.StatusCode
	state.audit.UpstreamStatus = response.StatusCode
	state.audit.BytesUp = upstreamBytes.Load()

	bytesDown, copyErr := io.Copy(w, response.Body)
	state.audit.BytesDown = bytesDown
	if copyErr != nil && !isBenignCopyError(copyErr) {
		state.audit.ErrorCategory = CategoryTunnelIO
		state.audit.CloseReason = copyErr.Error()
		if h.appLogger != nil {
			h.appLogger.Warn("response streaming failed",
				"request_id", state.audit.RequestID,
				"destination", state.audit.Destination,
				"error", copyErr.Error(),
			)
		}
	}
}

func cloneURL(input *url.URL) *url.URL {
	if input == nil {
		return &url.URL{}
	}
	cloned := *input
	return &cloned
}
