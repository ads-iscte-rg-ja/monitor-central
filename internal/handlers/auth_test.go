package handlers

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/models"
	"monitor-central/internal/services"
)

func TestLoginReturns429AndDoesNotTrustForwardingHeaders(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	s := services.NewAuthService(nil, nil, logger, config.LoginConfig{PerIP: 1, PerAccount: 5, MaxConcurrent: 1}, &countingAttempts{})
	// Invalid input still consumes the peer's attempt without querying storage.
	_, _ = s.Login(context.Background(), &models.LoginRequest{}, "127.0.0.1")
	h := NewAuthHandler(s, nil, nil, logger)
	r := httptest.NewRequest("POST", "https://monitor.example/login", strings.NewReader("email=user%40example.com&password=wrong"))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-For", "192.0.2.123")
	w := httptest.NewRecorder()
	h.Login(w, r)
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("status=%d retry=%s", w.Code, w.Header().Get("Retry-After"))
	}
}

// countingAttempts counts attempts in memory (the real store is PostgreSQL).
type countingAttempts struct{ counts map[string]int }

func (c *countingAttempts) CountAttempts(_ context.Context, keys [][]byte, _ time.Duration) ([]int, error) {
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	out := make([]int, len(keys))
	for i, k := range keys {
		c.counts[string(k)]++
		out[i] = c.counts[string(k)]
	}
	return out, nil
}
