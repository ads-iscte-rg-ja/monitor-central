package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"monitor-central/internal/utils/middleware"
)

// Every route in the single table is behind the auth middleware (ADR-0012): without
// a session, pages redirect to /login and /api/ answers 401. Nil handlers are
// never called, which also proves that the middleware stops the request first.
func TestProtectedRoutesRequireASession(t *testing.T) {
	protected := protectedRoutes(nil, nil, nil, nil, nil, nil)
	r := chi.NewRouter()
	mdw := middleware.New(nil, nil, nil)
	r.Group(func(r chi.Router) {
		r.Use(mdw.Auth)
		for _, route := range protected {
			r.Method(route.method, route.pattern, route.handler)
		}
	})

	seen := map[string]bool{}
	for _, route := range protected {
		key := route.method + " " + route.pattern
		if seen[key] {
			t.Fatalf("route %s is listed twice", key)
		}
		seen[key] = true

		path := strings.NewReplacer("{alertID}", "1", "{eventID}", "1", "{hostID}", "1").Replace(route.pattern)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(route.method, path, nil))
		if strings.HasPrefix(path, "/api/") {
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s: status %d, want 401", key, w.Code)
			}
			continue
		}
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
			t.Errorf("%s: status %d location %q, want 303 to /login", key, w.Code, w.Header().Get("Location"))
		}
	}
}
