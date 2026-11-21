package server

import (
	"log"
	"log/slog"
	"net/http"
	"strings"

	"monitor-central/internal/handlers"
	"monitor-central/internal/templating/pages"
	"monitor-central/internal/utils/middleware"
	"monitor-central/internal/utils/session"

	"github.com/go-chi/chi/v5"
)

func setupRoutes(
	r chi.Router,
	logger *log.Logger,
	tb *pages.PageBox,
	authHandler *handlers.AuthHandler,
	userHandler *handlers.UserHandler,
	healthHandler *handlers.HealthHandler,
	legalHandler *handlers.LegalHandler,
	dashboardHandler *handlers.DashboardHandler,
	errorLogsHandler *handlers.ErrorLogsHandler,
	hostsHandler *handlers.HostsHandler,
	alertsHandler *handlers.AlertsHandler,
	sessionManager *session.SessionManager,
	mdw *middleware.Middleware,

) {
	// Global Middlewares
	r.Use(mdw.LoadUserContext)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		// Authenticated users are redirected to the dashboard.
		if sessionManager.Exists(r.Context(), "user_id") {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		tb.RenderPage("home", nil, w, r)
	})

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(mdw.Auth)
		r.Post("/logout", authHandler.Logout)
		r.Get("/profile", userHandler.GetProfile)
		r.Post(
			"/profile/notification-email",
			userHandler.UpdateNotificationEmail,
		)
		r.Get("/dashboard", dashboardHandler.GetDashboard)
		r.Get("/api/v1/dashboard/metrics", dashboardHandler.GetMetrics)
		r.Get("/api/v1/dashboard/summary", dashboardHandler.GetSummary)
		r.Get("/errors", errorLogsHandler.GetErrorLogs)
		r.Get("/alerts", alertsHandler.GetAlerts)
		r.Post("/alerts", alertsHandler.CreateAlert)
		r.Post(
			"/alerts/{alertID}/delete",
			alertsHandler.DeleteAlert,
		)
		r.Post(
			"/alerts/{alertID}/edit",
			alertsHandler.UpdateAlert,
		)
		r.Post(
			"/alerts/events/{eventID}/acknowledge",
			alertsHandler.AcknowledgeAlertEvent,
		)
		r.Get("/hosts", hostsHandler.GetHosts)
		r.Post("/hosts", hostsHandler.CreateHost)
		r.Post(
			"/hosts/{hostID}/delete",
			hostsHandler.DeleteHost,
		)
		r.Post(
			"/hosts/{hostID}/edit",
			hostsHandler.UpdateHost,
		)

		r.Post(
			"/hosts/{hostID}/regenerate-key",
			hostsHandler.RegenerateAPIKey,
		)
	})

	// Login
	r.Get("/login", func(w http.ResponseWriter, r *http.Request) {
		if sessionManager.Exists(r.Context(), "user_id") {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		tb.RenderPage("login", nil, w, r)
	})
	r.Post("/login", authHandler.Login)

	// Health checks (ADR-0010): /health is the same as /health/ready.
	r.Get("/health", healthHandler.Ready)
	r.Get("/health/ready", healthHandler.Ready)
	r.Get("/health/live", healthHandler.Live)

	// Legal
	r.Get("/privacy-policy", legalHandler.PrivacyPolicy)
	r.Get("/terms", legalHandler.Terms)
	r.Get("/offline", func(w http.ResponseWriter, r *http.Request) { tb.ServePage("offline", w, r) })

	printRoutes(r, logger)
}

// protectedPaths contains paths that require authentication
var protectedPaths = map[string]bool{
	"/logout":           true,
	"/profile":          true,
	"/dashboard":        true,
	"/errors":           true,
	"/alerts":           true,
	"/hosts":            true,
	"/api/v1/dashboard": true,
}

// printRoutes lists the registered routes at debug level (LOG_LEVEL=debug), as
// structured log lines rather than free text on stdout (ADR-0004).
func printRoutes(r chi.Router, logger *log.Logger) {
	walkFunc := func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// Skip middleware-only routes
		if route == "/*" {
			return nil
		}
		isProtected := false
		for path := range protectedPaths {
			if strings.HasPrefix(route, path) || route == path {
				isProtected = true
				break
			}
		}
		slog.Debug("route registered", "method", method, "route", route, "protected", isProtected)
		return nil
	}

	if err := chi.Walk(r, walkFunc); err != nil {
		logger.Printf("Error walking routes: %v", err)
	}
}
