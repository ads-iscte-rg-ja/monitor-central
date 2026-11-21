package middleware

import (
	"io"
	"log/slog"
	"net/http"

	"monitor-central/internal/config"
)

// ContentSecurityPolicy allows scripts only from this origin and no inline
// scripts. Styles allow 'unsafe-inline' because the Tailwind browser build
// writes the generated CSS into a <style> element (ADR-0016).
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; " +
	"frame-ancestors 'none'; report-uri /csp-report"

// SecurityHeaders sets the security headers on every response (ADR-0016).
// X-Frame-Options is always sent: browsers ignore frame-ancestors in a
// Report-Only policy, and clickjacking must stay blocked in both modes.
func SecurityHeaders(c config.WebSecurityConfig) func(http.Handler) http.Handler {
	cspHeader := "Content-Security-Policy"
	if c.CSPMode == config.CSPReportOnly {
		cspHeader = "Content-Security-Policy-Report-Only"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set(cspHeader, ContentSecurityPolicy)
			h.Set("X-Frame-Options", "DENY")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if c.HSTS {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// maxCSPReport bounds a violation report; real ones are well under 2 KiB.
const maxCSPReport = 16 << 10

// CSPReport logs the browser's violation reports (report-uri) as warnings and
// stores nothing: a violation means a page needs fixing before production.
func CSPReport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCSPReport))
	if err != nil {
		http.Error(w, "Report too large", http.StatusRequestEntityTooLarge)
		return
	}
	slog.Warn("content security policy violation", "report", string(body), "user_agent", r.UserAgent())
	w.WriteHeader(http.StatusNoContent)
}
