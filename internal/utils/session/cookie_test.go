package session

import "testing"

// ASVS V3.3.1 (ADR-0016): the __Host- prefix over HTTPS, a plain name over HTTP.
func TestCookieName(t *testing.T) {
	t.Setenv("COOKIE_SECURE", "true")
	if c := DefaultSessionConfig(); c.CookieName != "__Host-session" || !c.CookieSecure || c.CookiePath != "/" {
		t.Fatalf("HTTPS: %+v", c)
	}
	t.Setenv("COOKIE_SECURE", "false")
	if c := DefaultSessionConfig(); c.CookieName != "session" || c.CookieSecure {
		t.Fatalf("HTTP: %+v", c)
	}
}
