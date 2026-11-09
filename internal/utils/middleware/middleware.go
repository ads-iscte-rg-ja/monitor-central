package middleware

import (
	"context"
	"log"
	"net/http"

	"monitor-central/internal/config"
	"monitor-central/internal/services"
	"monitor-central/internal/utils/session"
)

// Middleware holds middleware dependencies
type Middleware struct {
	logger         *log.Logger
	sessionManager *session.SessionManager
	authService    *services.AuthService
}

// New creates a new Middleware instance
func New(logger *log.Logger, sessionManager *session.SessionManager, authService *services.AuthService) *Middleware {
	return &Middleware{
		logger:         logger,
		sessionManager: sessionManager,
		authService:    authService,
	}
}

// LoadUserContext adds authentication status to the request context.
// This middleware runs on all routes to ensure identity is available everywhere.
func (m *Middleware) LoadUserContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := m.sessionManager.GetInt64(r.Context(), "user_id")

		var isAuthenticated bool
		if userID > 0 {
			// Verify user still exists in DB
			exists, err := m.authService.UserExists(r.Context(), userID)
			if err == nil && exists {
				isAuthenticated = true
			} else {
				// If user was deleted or error occurred, clear session
				m.sessionManager.Remove(r.Context(), "user_id")
				userID = 0
			}
		}

		// Inject identity into context
		ctx := context.WithValue(r.Context(), config.IsAuthenticatedContextKey, isAuthenticated)
		if isAuthenticated {
			ctx = context.WithValue(ctx, config.UserIDContextKey, userID)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Auth enforces authentication by checking the context.
// It assumes LoadUserContext has already run.
func (m *Middleware) Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isAuthenticated, _ := r.Context().Value(config.IsAuthenticatedContextKey).(bool)
		if !isAuthenticated {
			if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// CSRF returns modern CSRF protection middleware using Fetch Metadata headers.
// This is a stateless approach that doesn't require tokens, cookies, or secrets.
//
// Check: https://www.alexedwards.net/blog/preventing-csrf-in-go
//
// How it works:
// - Modern browsers (2020+) send Sec-Fetch-Site header automatically
// - Middleware checks if request is same-origin or cross-site
// - Cross-site POST/PUT/DELETE requests are blocked (403 Forbidden)
// - Falls back to Origin header comparison for older browsers
//
// Requirements:
// - HTTPS in production (localhost works over HTTP for development)
// - Modern browsers (Chrome 76+, Firefox 90+, Safari 15.6+, Edge 79+)
//
// Benefits:
// - No frontend changes required - works automatically
// - No secrets or keys to manage
// - Stateless - perfect for distributed systems
// - Standard library approach (Go 1.25+)
func (m *Middleware) CSRF() func(http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()

	return func(next http.Handler) http.Handler {
		return protection.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Log rejected requests for monitoring
			if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
				// If we reach here, the request passed CSRF validation
				m.logger.Printf("CSRF check passed for %s %s", r.Method, r.URL.Path)
			}
			next.ServeHTTP(w, r)
		}))
	}
}
