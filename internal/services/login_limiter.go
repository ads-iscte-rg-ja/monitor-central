package services

import (
	"context"
	"crypto/sha256"
	"time"

	"monitor-central/internal/config"
)

// loginWindow is the fixed window in which attempts are counted.
const loginWindow = time.Minute

// LoginAttemptStore counts login attempts per key in fixed windows that every
// instance of the central shares (ADR-0007).
type LoginAttemptStore interface {
	// CountAttempts records one attempt for each (distinct) key and returns the
	// attempts in each key's current window, in the order of keys.
	CountAttempts(ctx context.Context, keys [][]byte, window time.Duration) ([]int, error)
}

type loginLimiter struct {
	store  LoginAttemptStore
	config config.LoginConfig
	// work bounds concurrent bcrypt comparisons. It stays per instance: it
	// protects this instance's CPU, not the accounts.
	work chan struct{}
}

func newLoginLimiter(c config.LoginConfig, store LoginAttemptStore) *loginLimiter {
	return &loginLimiter{store: store, config: c, work: make(chan struct{}, c.MaxConcurrent)}
}

// allow counts the attempt against the client address and the account, and
// reports whether both are within their limits. Keys are hashed, so the store
// never holds e-mail addresses. Peer addresses are used, never untrusted proxy
// headers (see middleware.TrustedProxies).
func (l *loginLimiter) allow(ctx context.Context, ip, account string) (bool, error) {
	ipKey := sha256.Sum256([]byte("ip:" + ip))
	accountKey := sha256.Sum256([]byte("account:" + account))
	counts, err := l.store.CountAttempts(ctx, [][]byte{ipKey[:], accountKey[:]}, loginWindow)
	if err != nil {
		return false, err
	}
	return counts[0] <= l.config.PerIP && counts[1] <= l.config.PerAccount, nil
}
