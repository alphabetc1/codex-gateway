package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestParseProxyAuthorization(t *testing.T) {
	header := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret"))

	credentials, err := ParseProxyAuthorization(header)
	if err != nil {
		t.Fatalf("ParseProxyAuthorization() error = %v", err)
	}
	if credentials.Username != "alice" {
		t.Fatalf("username = %q, want %q", credentials.Username, "alice")
	}
	if credentials.Password != "secret" {
		t.Fatalf("password = %q, want %q", credentials.Password, "secret")
	}
}

func TestParseProxyAuthorizationRejectsInvalidHeaders(t *testing.T) {
	cases := []string{
		"",
		"Bearer token",
		"Basic !!!",
		"Basic " + base64.StdEncoding.EncodeToString([]byte("missing-colon")),
	}

	for _, header := range cases {
		t.Run(strings.ReplaceAll(header, " ", "_"), func(t *testing.T) {
			if _, err := ParseProxyAuthorization(header); err == nil {
				t.Fatalf("ParseProxyAuthorization(%q) succeeded, want error", header)
			}
		})
	}
}

func TestMapStoreAuthenticateBcrypt(t *testing.T) {
	hash, err := HashPassword("secret", bcrypt.MinCost)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	store := NewMapStore(map[string]string{"alice": hash})

	if ok, err := store.Authenticate("alice", "secret"); err != nil || !ok {
		t.Fatalf("Authenticate(valid) = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := store.Authenticate("alice", "wrong"); err != nil || ok {
		t.Fatalf("Authenticate(invalid password) = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := store.Authenticate("missing", "secret"); err != nil || ok {
		t.Fatalf("Authenticate(missing user) = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestWriteProxyAuthRequired(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteProxyAuthRequired(recorder)

	response := recorder.Result()
	if response.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusProxyAuthRequired)
	}
	if value := response.Header.Get("Proxy-Authenticate"); value != ProxyAuthenticateValue {
		t.Fatalf("Proxy-Authenticate = %q, want %q", value, ProxyAuthenticateValue)
	}
}

func TestCachedStoreCachesSuccessfulAuth(t *testing.T) {
	backend := &countingStore{
		users: map[string]string{
			"alice": "secret",
		},
	}
	store := NewCachedStore(backend, time.Minute, 16)

	for i := 0; i < 2; i++ {
		ok, err := store.Authenticate("alice", "secret")
		if err != nil || !ok {
			t.Fatalf("Authenticate(valid) = (%v, %v), want (true, nil)", ok, err)
		}
	}

	if got := backend.calls("alice"); got != 1 {
		t.Fatalf("backend calls = %d, want %d", got, 1)
	}
}

func TestCachedStoreDoesNotCacheInvalidPassword(t *testing.T) {
	backend := &countingStore{
		users: map[string]string{
			"alice": "secret",
		},
	}
	store := NewCachedStore(backend, time.Minute, 16)

	for i := 0; i < 2; i++ {
		ok, err := store.Authenticate("alice", "wrong")
		if err != nil || ok {
			t.Fatalf("Authenticate(invalid) = (%v, %v), want (false, nil)", ok, err)
		}
	}

	if got := backend.calls("alice"); got != 2 {
		t.Fatalf("backend calls = %d, want %d", got, 2)
	}
}

type countingStore struct {
	mu        sync.Mutex
	users     map[string]string
	callCount map[string]int
}

func (s *countingStore) Authenticate(username, password string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.callCount == nil {
		s.callCount = make(map[string]int)
	}
	s.callCount[username]++
	expected, ok := s.users[username]
	return ok && expected == password, nil
}

func (s *countingStore) HasUser(username string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.users[username]
	return ok
}

func (s *countingStore) calls(username string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount[username]
}
