package auth

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
)

const ProxyAuthenticateValue = `Basic realm="claude-gateway"`

var (
	ErrMissingProxyAuthorization = errors.New("missing proxy authorization")
	ErrInvalidProxyAuthorization = errors.New("invalid proxy authorization")
)

type Credentials struct {
	Username string
	Password string
}

func ParseProxyAuthorization(headerValue string) (Credentials, error) {
	if strings.TrimSpace(headerValue) == "" {
		return Credentials{}, ErrMissingProxyAuthorization
	}

	scheme, payload, ok := strings.Cut(headerValue, " ")
	if !ok || !strings.EqualFold(strings.TrimSpace(scheme), "basic") {
		return Credentials{}, ErrInvalidProxyAuthorization
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
	if err != nil {
		return Credentials{}, ErrInvalidProxyAuthorization
	}

	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok || username == "" {
		return Credentials{}, ErrInvalidProxyAuthorization
	}

	return Credentials{
		Username: username,
		Password: password,
	}, nil
}

func WriteProxyAuthRequired(w http.ResponseWriter) {
	w.Header().Set("Proxy-Authenticate", ProxyAuthenticateValue)
	http.Error(w, http.StatusText(http.StatusProxyAuthRequired), http.StatusProxyAuthRequired)
}

func IsSensitiveHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key":
		return true
	default:
		return false
	}
}
