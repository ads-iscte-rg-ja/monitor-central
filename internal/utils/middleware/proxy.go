package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedProxies replaces the peer address with the client address reported
// in X-Forwarded-For, but only when the peer is one of the trusted proxies
// (ADR-0007). Behind a load balancer every request comes from the balancer, so
// without this the per-IP login limit would become one global limit. Any
// other peer's header is ignored: a client cannot choose the address the login
// limiter sees. The proxy must overwrite the header, not append to it; the last
// value is the one used. With no trusted proxies the handler is unchanged.
func TrustedProxies(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(trusted) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if client, ok := forwardedClient(r, trusted); ok {
				r.RemoteAddr = net.JoinHostPort(client.String(), "0")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func forwardedClient(r *http.Request, trusted []netip.Prefix) (netip.Addr, bool) {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil || !contains(trusted, peer.Addr().Unmap()) {
		return netip.Addr{}, false
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return netip.Addr{}, false
	}
	parts := strings.Split(values[len(values)-1], ",")
	client, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1]))
	if err != nil {
		return netip.Addr{}, false
	}
	return client.Unmap(), true
}

func contains(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
