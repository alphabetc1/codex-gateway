package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

func (h *Handler) handleConnect(w http.ResponseWriter, r *http.Request, state *requestState, runtime *runtimeState) {
	authority := r.Host
	if authority == "" {
		authority = r.RequestURI
	}
	state.audit.Destination = authority

	resolution, proxyErr := runtime.policy.ResolveCONNECT(r.Context(), authority)
	if proxyErr != nil {
		h.writeProxyError(w, state, proxyErr)
		return
	}

	state.audit.Destination = resolution.Destination.Authority()
	state.audit.ResolvedIP = resolution.Selected.String()

	upstreamConn, err := h.dialer.DialContext(r.Context(), "tcp", resolution.DialAddress())
	if err != nil {
		h.writeProxyError(w, state, classifyTransportError(err))
		return
	}
	defer upstreamConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		h.writeProxyError(w, state, &HandlerError{
			Status:   http.StatusInternalServerError,
			Category: CategoryInternal,
			Message:  "server does not support connection hijacking",
		})
		return
	}

	clientConn, readWriter, err := hijacker.Hijack()
	if err != nil {
		h.writeProxyError(w, state, &HandlerError{
			Status:   http.StatusInternalServerError,
			Category: CategoryInternal,
			Message:  "failed to hijack client connection",
			Err:      err,
		})
		return
	}
	defer clientConn.Close()

	if _, err := readWriter.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		state.audit.ProxyStatus = http.StatusBadGateway
		state.audit.ErrorCategory = CategoryTunnelIO
		state.audit.CloseReason = err.Error()
		return
	}
	if err := readWriter.Flush(); err != nil {
		state.audit.ProxyStatus = http.StatusBadGateway
		state.audit.ErrorCategory = CategoryTunnelIO
		state.audit.CloseReason = err.Error()
		return
	}

	state.audit.ProxyStatus = http.StatusOK
	if h.metrics != nil {
		h.metrics.activeTunnels.Add(1)
		defer h.metrics.activeTunnels.Add(-1)
	}

	clientTunnel := wrapIdleConn(clientConn, h.tunnelIdleTimeout)
	upstreamTunnel := wrapIdleConn(upstreamConn, h.tunnelIdleTimeout)
	uploadSource := io.Reader(clientTunnel)
	if buffered := readWriter.Reader.Buffered(); buffered > 0 {
		peeked, peekErr := readWriter.Reader.Peek(buffered)
		if peekErr == nil {
			_, _ = readWriter.Reader.Discard(buffered)
			uploadSource = io.MultiReader(bytes.NewReader(append([]byte(nil), peeked...)), clientTunnel)
		}
	}

	results := make(chan tunnelResult, 2)
	var wait sync.WaitGroup
	wait.Add(2)

	go func() {
		defer wait.Done()
		bytesUp, copyErr := tunnelCopy(upstreamTunnel, uploadSource)
		results <- tunnelResult{direction: "upstream", bytes: bytesUp, err: copyErr}
	}()

	go func() {
		defer wait.Done()
		bytesDown, copyErr := tunnelCopy(clientTunnel, upstreamTunnel)
		results <- tunnelResult{direction: "downstream", bytes: bytesDown, err: copyErr}
	}()

	wait.Wait()
	close(results)

	for result := range results {
		switch result.direction {
		case "upstream":
			state.audit.BytesUp = result.bytes
			recordTunnelOutcome(state, "client", result.err)
		case "downstream":
			state.audit.BytesDown = result.bytes
			recordTunnelOutcome(state, "upstream", result.err)
		}
	}
}

type tunnelResult struct {
	direction string
	bytes     int64
	err       error
}

type idleTimeoutConn struct {
	net.Conn
	idleTimeout time.Duration
}

func wrapIdleConn(conn net.Conn, idleTimeout time.Duration) net.Conn {
	if idleTimeout <= 0 {
		return conn
	}
	return &idleTimeoutConn{
		Conn:        conn,
		idleTimeout: idleTimeout,
	}
}

func (c *idleTimeoutConn) Read(p []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.idleTimeout))
	return c.Conn.Read(p)
}

func (c *idleTimeoutConn) Write(p []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(c.idleTimeout))
	return c.Conn.Write(p)
}

func (c *idleTimeoutConn) CloseWrite() error {
	if writer, ok := c.Conn.(closeWriter); ok {
		return writer.CloseWrite()
	}
	return c.Conn.Close()
}

type closeWriter interface {
	CloseWrite() error
}

func tunnelCopy(dst net.Conn, src io.Reader) (int64, error) {
	n, err := io.Copy(dst, src)
	if writer, ok := dst.(closeWriter); ok {
		_ = writer.CloseWrite()
	}
	return n, err
}

func recordTunnelOutcome(state *requestState, source string, err error) {
	if err == nil {
		if state.audit.CloseReason == "" {
			state.audit.CloseReason = source + "_eof"
		}
		return
	}
	if isBenignCopyError(err) {
		if state.audit.CloseReason == "" {
			state.audit.CloseReason = source + "_closed"
		}
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		state.audit.ErrorCategory = CategoryUpstreamTimeout
		state.audit.CloseReason = "idle_timeout"
		return
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		state.audit.ErrorCategory = CategoryUpstreamTimeout
		state.audit.CloseReason = "idle_timeout"
		return
	}
	state.audit.ErrorCategory = CategoryTunnelIO
	state.audit.CloseReason = fmt.Sprintf("%s_error: %v", source, err)
}
