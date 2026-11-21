package server

import (
	"log"
	"log/slog"
	"net/http"

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

	// Authenticated routes: one table, so the auth group and the route log cannot diverge.
	protected := protectedRoutes(authHandler, userHandler, dashboardHandler, errorLogsHandler, hostsHandler, alertsHandler)
	r.Group(func(r chi.Router) {
		r.Use(mdw.Auth)
		for _, route := range protected {
			r.Method(route.method, route.pattern, route.handler)
		}
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

	printRoutes(r, logger, protected)
}

// route is one authenticated route: the single source for registration and for
// the route log (ADR-0012).
type route struct {
	method  string
	pattern string
	handler http.HandlerFunc
}

func protectedRoutes(
	authHandler *handlers.AuthHandler,
	userHandler *handlers.UserHandler,
	dashboardHandler *handlers.DashboardHandler,
	errorLogsHandler *handlers.ErrorLogsHandler,
	hostsHandler *handlers.HostsHandler,
	alertsHandler *handlers.AlertsHandler,
) []route {
	return []route{
		{http.MethodPost, "/logout", authHandler.Logout},
		{http.MethodGet, "/profile", userHandler.GetProfile},
		{http.MethodPost, "/profile/notification-email", userHandler.UpdateNotificationEmail},
		{http.MethodGet, "/dashboard", dashboardHandler.GetDashboard},
		{http.MethodGet, "/api/v1/dashboard/metrics", dashboardHandler.GetMetrics},
		{http.MethodGet, "/api/v1/dashboard/summary", dashboardHandler.GetSummary},
		{http.MethodGet, "/errors", errorLogsHandler.GetErrorLogs},
		{http.MethodGet, "/alerts", alertsHandler.GetAlerts},
		{http.MethodPost, "/alerts", alertsHandler.CreateAlert},
		{http.MethodPost, "/alerts/{alertID}/delete", alertsHandler.DeleteAlert},
		{http.MethodPost, "/alerts/{alertID}/edit", alertsHandler.UpdateAlert},
		{http.MethodPost, "/alerts/events/{eventID}/acknowledge", alertsHandler.AcknowledgeAlertEvent},
		{http.MethodGet, "/hosts", hostsHandler.GetHosts},
		{http.MethodPost, "/hosts", hostsHandler.CreateHost},
		{http.MethodPost, "/hosts/{hostID}/delete", hostsHandler.DeleteHost},
		{http.MethodPost, "/hosts/{hostID}/edit", hostsHandler.UpdateHost},
		{http.MethodPost, "/hosts/{hostID}/regenerate-key", hostsHandler.RegenerateAPIKey},
	}
}

// printRoutes lists the registered routes at debug level (LOG_LEVEL=debug), as
// structured log lines rather than free text on stdout (ADR-0004).
func printRoutes(r chi.Router, logger *log.Logger, protected []route) {
	isProtected := make(map[string]bool, len(protected))
	for _, route := range protected {
		isProtected[route.method+" "+route.pattern] = true
	}
	walkFunc := func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// Skip middleware-only routes
		if route == "/*" {
			return nil
		}
		slog.Debug("route registered", "method", method, "route", route, "protected", isProtected[method+" "+route])
		return nil
	}

	if err := chi.Walk(r, walkFunc); err != nil {
		logger.Printf("Error walking routes: %v", err)
	}
}
