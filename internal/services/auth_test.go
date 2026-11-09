package services

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"monitor-central/internal/config"
	"monitor-central/internal/models"
)

type authUsers struct {
	hash             string
	entered, release chan struct{}
}

func (u *authUsers) GetByEmail(context.Context, string) (*models.User, error) {
	if u.entered != nil {
		u.entered <- struct{}{}
		<-u.release
	}
	if u.hash == "" {
		return nil, models.ErrUserNotFound
	}
	return &models.User{ID: 1, PasswordHash: u.hash}, nil
}
func (*authUsers) UpdateLastLogin(context.Context, int64) error { return nil }
func (*authUsers) Exists(context.Context, int64) (bool, error)  { return true, nil }

type authAudit struct{}

func (authAudit) Create(context.Context, int64, string, string) error { return nil }

func TestLoginLimiterAccountIPAndExpiry(t *testing.T) {
	limits := config.LoginConfig{PerIP: 2, PerAccount: 2, MaxConcurrent: 1}
	l := newLoginLimiter(limits)
	now := time.Now()
	if !l.allow("ip1", "account1", now) || !l.allow("ip2", "account1", now) || l.allow("ip3", "account1", now) {
		t.Fatal("account limit bypassed across IPs")
	}
	l = newLoginLimiter(limits)
	if !l.allow("ip1", "account1", now) || !l.allow("ip1", "account2", now) || l.allow("ip1", "account3", now) {
		t.Fatal("IP limit bypassed across accounts")
	}
	if !l.allow("ip1", "account3", now.Add(time.Minute)) {
		t.Fatal("expired window did not reset")
	}
}

func TestMissingUserUsesConfiguredBcryptCost(t *testing.T) {
	s := NewAuthService(&authUsers{}, authAudit{}, log.New(io.Discard, "", 0), config.LoginConfig{PerIP: 20, PerAccount: 5, MaxConcurrent: 1})
	cost, err := bcrypt.Cost(s.dummyHash)
	if err != nil || cost != config.BcryptCost {
		t.Fatal("dummy hash cost differs", err)
	}
	id, err := s.Login(context.Background(), &models.LoginRequest{Email: "missing@example.com", Password: "wrong-password"}, "127.0.0.1")
	if id != 0 || !errors.Is(err, models.ErrInvalidCredentials) {
		t.Fatal(id, err)
	}
	s.limiter.config.PerAccount = 1
	if _, err := s.Login(context.Background(), &models.LoginRequest{Email: "  MISSING@EXAMPLE.COM ", Password: "wrong-password"}, "another-IP"); !errors.Is(err, ErrLoginRateLimited) {
		t.Fatal("normalization bypassed account limit", err)
	}
}

func TestLoginWorkIsBounded(t *testing.T) {
	u := &authUsers{entered: make(chan struct{}, 1), release: make(chan struct{})}
	s := NewAuthService(u, authAudit{}, log.New(io.Discard, "", 0), config.LoginConfig{PerIP: 20, PerAccount: 5, MaxConcurrent: 1})
	done := make(chan error, 1)
	go func() {
		_, err := s.Login(context.Background(), &models.LoginRequest{Email: "one@example.com", Password: "wrong"}, "ip1")
		done <- err
	}()
	<-u.entered
	_, err := s.Login(context.Background(), &models.LoginRequest{Email: "two@example.com", Password: "wrong"}, "ip2")
	close(u.release)
	if !errors.Is(err, ErrLoginRateLimited) {
		t.Fatal("concurrent bcrypt work was not bounded", err)
	}
	if err := <-done; !errors.Is(err, models.ErrInvalidCredentials) {
		t.Fatal(err)
	}
}
