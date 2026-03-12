package proxy

import (
	"net/http"
	"strings"
)

var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func stripHopHeaders(header http.Header) {
	if header == nil {
		return
	}

	for _, token := range connectionTokens(header.Values("Connection")) {
		header.Del(token)
	}
	for _, name := range hopByHopHeaders {
		header.Del(name)
	}
}

func copyHeaders(dst, src http.Header) {
	for name, values := range src {
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func connectionTokens(values []string) []string {
	var tokens []string
	for _, value := range values {
		parts := strings.Split(value, ",")
		for _, part := range parts {
			token := textprotoCanonical(strings.TrimSpace(part))
			if token == "" {
				continue
			}
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func textprotoCanonical(name string) string {
	if name == "" {
		return ""
	}
	return http.CanonicalHeaderKey(name)
}
