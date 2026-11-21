package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"monitor-central/internal/config"
)

// SE2 (ADR-0016): every response, whatever its kind or status, carries the CSP,
// the anti-framing headers and nosniff; HSTS only over HTTPS.
func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/dashboard", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) })
	r.Get("/api/v1/dashboard/summary", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	})
	r.Get("/broken", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) })

	for _, tc := range []struct {
		name   string
		config config.WebSecurityConfig
		csp    string
		hsts   bool
	}{
		{"enforce over HTTPS", config.WebSecurityConfig{CSPMode: config.CSPEnforce, HSTS: true}, "Content-Security-Policy", true},
		{"report-only over HTTP", config.WebSecurityConfig{CSPMode: config.CSPReportOnly}, "Content-Security-Policy-Report-Only", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := SecurityHeaders(tc.config)(r)
			for _, path := range []string{"/dashboard", "/api/v1/dashboard/summary", "/broken", "/missing"} {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				got := w.Header()
				csp := got.Get(tc.csp)
				if !strings.Contains(csp, "script-src 'self';") || !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "object-src 'none'") {
					t.Fatalf("%s: %s = %q", path, tc.csp, csp)
				}
				other := "Content-Security-Policy-Report-Only"
				if tc.csp == other {
					other = "Content-Security-Policy"
				}
				if got.Get(other) != "" {
					t.Fatalf("%s: both CSP headers sent", path)
				}
				if got.Get("X-Frame-Options") != "DENY" || got.Get("X-Content-Type-Options") != "nosniff" ||
					got.Get("Referrer-Policy") != "strict-origin-when-cross-origin" || got.Get("Cross-Origin-Opener-Policy") != "same-origin" {
					t.Fatalf("%s: headers %v", path, got)
				}
				if hsts := got.Get("Strict-Transport-Security"); (hsts == "max-age=31536000") != tc.hsts || (!tc.hsts && hsts != "") {
					t.Fatalf("%s: HSTS %q", path, hsts)
				}
			}
		})
	}
}

func TestCSPReportIsAcceptedAndBounded(t *testing.T) {
	w := httptest.NewRecorder()
	CSPReport(w, httptest.NewRequest("POST", "/csp-report", strings.NewReader(`{"csp-report":{"violated-directive":"script-src"}}`)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d", w.Code)
	}
	w = httptest.NewRecorder()
	CSPReport(w, httptest.NewRequest("POST", "/csp-report", strings.NewReader(strings.Repeat("x", maxCSPReport+1))))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized report: status %d", w.Code)
	}
}
