package limiter

import "sync"

type ConcurrencyLimiter struct {
	max int

	mu     sync.Mutex
	counts map[string]int
}

func New(max int) *ConcurrencyLimiter {
	return &ConcurrencyLimiter{
		max:    max,
		counts: make(map[string]int),
	}
}

func (l *ConcurrencyLimiter) Acquire(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	count := l.counts[key]
	if count >= l.max {
		return false
	}
	l.counts[key] = count + 1
	return true
}

func (l *ConcurrencyLimiter) Release(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	count := l.counts[key]
	if count <= 1 {
		delete(l.counts, key)
		return
	}
	l.counts[key] = count - 1
}

func (l *ConcurrencyLimiter) Current(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[key]
}
