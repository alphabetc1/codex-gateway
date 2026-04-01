package auth

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	defaultAuthCacheTTL     = 5 * time.Minute
	defaultAuthCacheMaxSize = 256
)

type UserStore interface {
	Authenticate(username, password string) (bool, error)
	HasUser(username string) bool
}

type MapStore struct {
	users map[string]string
}

func NewMapStore(users map[string]string) *MapStore {
	cloned := make(map[string]string, len(users))
	for username, hash := range users {
		cloned[username] = hash
	}
	return &MapStore{users: cloned}
}

func LoadUserStore(filePath, inline string) (UserStore, error) {
	users := make(map[string]string)

	if filePath != "" {
		fromFile, err := loadUsersFromFile(filePath)
		if err != nil {
			return nil, err
		}
		for username, hash := range fromFile {
			users[username] = hash
		}
	}

	if inline != "" {
		fromInline, err := ParseInlineUsers(inline)
		if err != nil {
			return nil, err
		}
		for username, hash := range fromInline {
			users[username] = hash
		}
	}

	if len(users) == 0 {
		return nil, errors.New("no proxy auth users configured")
	}

	return NewCachedStore(NewMapStore(users), defaultAuthCacheTTL, defaultAuthCacheMaxSize), nil
}

func ParseInlineUsers(raw string) (map[string]string, error) {
	users := make(map[string]string)
	for _, entry := range splitUsers(raw) {
		username, hash, err := parseUserLine(entry)
		if err != nil {
			return nil, err
		}
		users[username] = hash
	}
	return users, nil
}

func HashPassword(password string, cost int) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func (s *MapStore) Authenticate(username, password string) (bool, error) {
	hash, ok := s.users[username]
	if !ok {
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	return false, err
}

func (s *MapStore) HasUser(username string) bool {
	_, ok := s.users[username]
	return ok
}

type CachedStore struct {
	base authBackend
	ttl  time.Duration
	max  int

	mu      sync.Mutex
	entries map[[32]byte]time.Time
}

type authBackend interface {
	Authenticate(username, password string) (bool, error)
	HasUser(username string) bool
}

func NewCachedStore(base authBackend, ttl time.Duration, maxEntries int) *CachedStore {
	if ttl <= 0 {
		ttl = defaultAuthCacheTTL
	}
	if maxEntries <= 0 {
		maxEntries = defaultAuthCacheMaxSize
	}
	return &CachedStore{
		base:    base,
		ttl:     ttl,
		max:     maxEntries,
		entries: make(map[[32]byte]time.Time),
	}
}

func (s *CachedStore) Authenticate(username, password string) (bool, error) {
	if !s.base.HasUser(username) {
		return false, nil
	}

	key := authCacheKey(username, password)
	now := time.Now()

	s.mu.Lock()
	if expiresAt, ok := s.entries[key]; ok {
		if now.Before(expiresAt) {
			s.mu.Unlock()
			return true, nil
		}
		delete(s.entries, key)
	}
	s.mu.Unlock()

	ok, err := s.base.Authenticate(username, password)
	if err != nil || !ok {
		return ok, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= s.max {
		s.evictExpired(now)
		if len(s.entries) >= s.max {
			s.entries = make(map[[32]byte]time.Time, s.max)
		}
	}
	s.entries[key] = now.Add(s.ttl)
	return true, nil
}

func (s *CachedStore) HasUser(username string) bool {
	return s.base.HasUser(username)
}

func (s *CachedStore) evictExpired(now time.Time) {
	for key, expiresAt := range s.entries {
		if !now.Before(expiresAt) {
			delete(s.entries, key)
		}
	}
}

func authCacheKey(username, password string) [32]byte {
	return sha256.Sum256([]byte(username + "\x00" + password))
}

func loadUsersFromFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open users file: %w", err)
	}
	defer file.Close()

	users := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		username, hash, err := parseUserLine(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		users[username] = hash
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read users file: %w", err)
	}
	return users, nil
}

func parseUserLine(line string) (string, string, error) {
	username, hash, ok := strings.Cut(strings.TrimSpace(line), ":")
	if !ok || strings.TrimSpace(username) == "" || strings.TrimSpace(hash) == "" {
		return "", "", errors.New("invalid user entry; expected username:bcrypt_hash")
	}
	return strings.TrimSpace(username), strings.TrimSpace(hash), nil
}

func splitUsers(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}
