package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"monitor-central/internal/config"
	"monitor-central/internal/models"
	"net/http"
)

type HostAuthenticator interface {
	AuthenticateHost(context.Context, string) (*models.Host, error)
}

// RequireHost resolves the key before body decoding. Clients never select a host.
// Apply this middleware to the entire /api/v1/ingest group, outside browser sessions.
func RequireHost(auth HostAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, err := auth.AuthenticateHost(r.Context(), r.Header.Get("X-API-Key"))
			if err != nil || host == nil || host.ID <= 0 {
				status, message := http.StatusUnauthorized, "Invalid or missing API key"
				if err != nil && !errors.Is(err, models.ErrInvalidAPIKey) {
					status, message = http.StatusServiceUnavailable, "Host authentication unavailable"
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(models.ErrorResponse{Error: http.StatusText(status), Message: message})
				return
			}
			ctx := context.WithValue(r.Context(), config.HostIDContextKey, host.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
