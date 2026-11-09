package services

import (
	"crypto/sha256"
	"sync"
	"time"

	"monitor-central/internal/config"
)

type loginWindow struct {
	count   int
	expires time.Time
}
type loginLimiter struct {
	mu        sync.Mutex
	windows   map[[32]byte]loginWindow
	config    config.LoginConfig
	work      chan struct{}
	lastSweep time.Time
}

func newLoginLimiter(c config.LoginConfig) *loginLimiter {
	return &loginLimiter{windows: make(map[[32]byte]loginWindow), config: c, work: make(chan struct{}, c.MaxConcurrent)}
}

// Limits are process-local. Peer addresses are used, never untrusted proxy headers.
// The map is bounded and fails closed under a flood of distinct identities.
func (l *loginLimiter) allow(ip, account string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) >= time.Minute {
		for key, window := range l.windows {
			if !now.Before(window.expires) {
				delete(l.windows, key)
			}
		}
		l.lastSweep = now
	}
	for _, item := range []struct {
		key   string
		limit int
	}{{"ip:" + ip, l.config.PerIP}, {"account:" + account, l.config.PerAccount}} {
		key := sha256.Sum256([]byte(item.key))
		window := l.windows[key]
		if !now.Before(window.expires) {
			window = loginWindow{expires: now.Add(time.Minute)}
		}
		if window.count >= item.limit {
			return false
		}
		if _, exists := l.windows[key]; !exists && len(l.windows) >= 10000 {
			return false
		}
		window.count++
		l.windows[key] = window
	}
	return true
}
