package middleware

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"monitor-central/internal/config"
)

// Bulkhead bounds the authenticated browser routes (ADR-0017): at most
// MaxInflight requests are served at once, a request that finds no free slot
// within wait is answered 503 with Retry-After, and each served request's
// context gets the QueryTimeout deadline, so the driver cancels a database
// query that runs past it. Together with the ingestion limit (ADR-0006), a slow
// page can no longer take the whole connection pool.
func Bulkhead(c config.DashboardConfig, wait time.Duration) func(http.Handler) http.Handler {
	slots := make(chan struct{}, c.MaxInflight)
	retryAfter := strconv.Itoa(max(1, int(wait.Seconds())))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case slots <- struct{}{}:
			case <-timer.C:
				w.Header().Set("Retry-After", retryAfter)
				http.Error(w, "The server is busy. Please try again in a moment.", http.StatusServiceUnavailable)
				return
			case <-r.Context().Done():
				return // the browser gave up while waiting
			}
			defer func() { <-slots }()
			ctx, cancel := context.WithTimeout(r.Context(), c.QueryTimeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
