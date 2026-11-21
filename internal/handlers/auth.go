package handlers

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"

	"monitor-central/internal/models"
	"monitor-central/internal/services"
	"monitor-central/internal/templating/pages"
)

// authService is what login and logout need (ADR-0014).
type authService interface {
	Login(ctx context.Context, req *models.LoginRequest, ipAddress string) (int64, error)
	Logout(ctx context.Context, userID int64, ipAddress string)
}

// loginSession is the part of the session manager login and logout use.
type loginSession interface {
	LoginUser(ctx context.Context, userID int64) error
	GetInt64(ctx context.Context, key string) int64
	Destroy(ctx context.Context) error
}

// AuthHandler handles authentication HTTP requests
type AuthHandler struct {
	logger         *log.Logger
	authService    authService
	sessionManager loginSession
	tb             *pages.PageBox
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(authService authService, sessionManager loginSession, tb *pages.PageBox, logger *log.Logger) *AuthHandler {
	return &AuthHandler{
		logger:         logger,
		authService:    authService,
		sessionManager: sessionManager,
		tb:             tb,
	}
}

// Login handles user login via Form POST
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")

	req := models.LoginRequest{
		Email:    email,
		Password: password,
	}

	userID, err := h.authService.Login(r.Context(), &req, GetClientIP(r))
	if errors.Is(err, services.ErrLoginRateLimited) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many login attempts. Try again later.", http.StatusTooManyRequests)
		return
	}
	if err != nil {
		// Map domain errors to user-friendly messages
		errorMsg := "Error while logging in."
		if errors.Is(err, models.ErrInvalidCredentials) {
			errorMsg = "Incorrect email or password."
		} else {
			// Log internal errors that are not credential issues
			h.logger.Printf("Login error: %v", err)
		}

		h.tb.RenderPage("login", map[string]any{
			"Error": errorMsg,
			"Form": map[string]string{
				"Email": email,
			},
		}, w, r)
		return
	}

	// Perform session login sequence
	// This logic handles the HTTP/Session state, while AuthService handles the Domain/DB state.
	if err := h.sessionManager.LoginUser(r.Context(), userID); err != nil {
		h.logger.Printf("Session login error: %v", err)
		http.Error(w, "Login error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout handles user logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	userID := h.sessionManager.GetInt64(r.Context(), "user_id")

	// Call service logout (audit, etc)
	if userID > 0 {
		h.authService.Logout(r.Context(), userID, GetClientIP(r))
	}

	// Destroy the session
	if err := h.sessionManager.Destroy(r.Context()); err != nil {
		h.logger.Printf("Session destroy error: %v", err)
	}
	// Ask the browser to drop what it kept from the session (ASVS V14.3.1).
	w.Header().Set("Clear-Site-Data", `"cache", "storage"`)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// GetClientIP helper function
func GetClientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
