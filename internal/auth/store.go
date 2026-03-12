package auth

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
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

	return NewMapStore(users), nil
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
