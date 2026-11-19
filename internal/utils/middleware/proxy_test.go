package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func clientSeenBehind(trusted []netip.Prefix, peer string, forwarded ...string) string {
	var seen string
	h := TrustedProxies(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r.RemoteAddr }))
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.RemoteAddr = peer
	for _, v := range forwarded {
		r.Header.Add("X-Forwarded-For", v)
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
	return seen
}

func TestForwardedClientIsBelievedOnlyFromATrustedProxy(t *testing.T) {
	proxy := []netip.Prefix{netip.MustParsePrefix("172.28.0.10/32")}
	cases := []struct {
		name, peer string
		forwarded  []string
		want       string
	}{
		{"trusted proxy", "172.28.0.10:5000", []string{"198.51.100.7"}, "198.51.100.7:0"},
		{"last value wins", "172.28.0.10:5000", []string{"203.0.113.1, 198.51.100.7"}, "198.51.100.7:0"},
		{"last header wins", "172.28.0.10:5000", []string{"203.0.113.1", "2001:db8::1"}, "[2001:db8::1]:0"},
		{"untrusted client spoofing", "198.51.100.9:5000", []string{"203.0.113.1"}, "198.51.100.9:5000"},
		{"trusted proxy without header", "172.28.0.10:5000", nil, "172.28.0.10:5000"},
		{"garbage header", "172.28.0.10:5000", []string{"not-an-ip"}, "172.28.0.10:5000"},
		{"unparsable peer", "pipe", []string{"198.51.100.7"}, "pipe"},
	}
	for _, c := range cases {
		if got := clientSeenBehind(proxy, c.peer, c.forwarded...); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if got := clientSeenBehind(nil, "172.28.0.10:5000", "198.51.100.7"); got != "172.28.0.10:5000" {
		t.Errorf("with no trusted proxies the header must be ignored, got %q", got)
	}
}
