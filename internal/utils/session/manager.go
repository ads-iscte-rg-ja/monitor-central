package session

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/repositories"

	"github.com/alexedwards/scs/v2"
)

// SessionConfig holds session manager configuration
type SessionConfig struct {
	Lifetime       time.Duration
	IdleTimeout    time.Duration
	CookieName     string
	CookiePath     string
	CookieSecure   bool
	CookieSameSite http.SameSite
}

// DefaultSessionConfig returns default session configuration
func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		Lifetime:       24 * time.Hour,
		IdleTimeout:    20 * time.Minute,
		CookieName:     "session",
		CookiePath:     "/",
		CookieSecure:   config.GetEnvBool("COOKIE_SECURE", true),
		CookieSameSite: http.SameSiteStrictMode,
	}
}

// SessionManager wraps the SCS session manager with custom session store.
type SessionManager struct {
	*scs.SessionManager
	store *repositories.SessionStore // Keep reference for custom methods
}

// NewSessionManager creates a new session manager with the hybrid session store.
func NewSessionManager(store *repositories.SessionStore, cfg SessionConfig) *SessionManager {
	sm := scs.New()

	// Use the embedded PostgresStore from our SessionStore for standard SCS operations
	sm.Store = store.PostgresStore

	// Configure session behavior
	sm.Lifetime = cfg.Lifetime
	sm.IdleTimeout = cfg.IdleTimeout
	sm.Cookie.Name = cfg.CookieName
	sm.Cookie.Path = cfg.CookiePath
	sm.Cookie.Secure = cfg.CookieSecure
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = cfg.CookieSameSite

	return &SessionManager{
		SessionManager: sm,
		store:          store, // Keep reference for custom methods
	}
}

// LoginUser performs the full session login sequence:
// 1. Renews the session token (security against fixation)
// 2. Stores the user_id in the session
// 3. Commits the session to the DB
// 4. Links the session token to the user_id for tracking
func (sm *SessionManager) LoginUser(ctx context.Context, userID int64) error {
	// Renew session token to prevent session fixation
	if err := sm.RenewToken(ctx); err != nil {
		return fmt.Errorf("renew token: %w", err)
	}

	// Store user ID in session data
	sm.Put(ctx, "user_id", userID)

	// FORCE COMMIT: This writes the record to the Postgres 'sessions' table NOW
	token, _, err := sm.Commit(ctx)
	if err != nil {
		return fmt.Errorf("commit session: %w", err)
	}

	// Link session to user in DB
	if err := sm.UpdateUserID(ctx, token, userID); err != nil {
		return fmt.Errorf("link user session: %w", err)
	}

	return nil
}

// UpdateUserID links a session token to a user ID.
func (sm *SessionManager) UpdateUserID(ctx context.Context, token string, userID int64) error {
	return sm.store.UpdateUserID(ctx, token, userID)
}

// InvalidateAllUserSessions invalidates all sessions for a specific user.
func (sm *SessionManager) InvalidateAllUserSessions(ctx context.Context, userID int64) error {
	return sm.store.DeleteAllForUser(ctx, userID)
}

// NewSessionManagerWithDefaults creates a session manager with default configuration
func NewSessionManagerWithDefaults(store *repositories.SessionStore) *SessionManager {
	return NewSessionManager(store, DefaultSessionConfig())
}
