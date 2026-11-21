package services

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"syscall"
)

// ErrBlockedProbeTarget is a health probe refused by the SSRF guard (ADR-0016).
var ErrBlockedProbeTarget = errors.New("health probe target not allowed")

// metadataAddresses are cloud metadata services outside the loopback and
// link-local ranges (AWS over IPv6, Alibaba Cloud). 169.254.169.254 (AWS, GCP,
// Azure, …) is link-local and already blocked by the range checks.
var metadataAddresses = map[netip.Addr]bool{
	netip.MustParseAddr("fd00:ec2::254"):   true,
	netip.MustParseAddr("100.100.100.200"): true,
}

// blockedProbeAddress reports whether central must never probe ip: loopback,
// link-local (cloud metadata), unspecified, multicast, or a known metadata
// service. Private ranges (RFC 1918, ULA) are allowed: monitored hosts usually
// live in internal networks.
func blockedProbeAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsValid() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || metadataAddresses[ip]
}

// guardProbeDial is the net.Dialer Control of the probes. It runs after DNS
// resolution, on the address actually dialled, so a name that resolves (or
// later re-resolves) to a blocked address is refused too.
func guardProbeDial(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBlockedProbeTarget, address)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || blockedProbeAddress(ip) {
		return fmt.Errorf("%w: %s", ErrBlockedProbeTarget, host)
	}
	return nil
}

// blockedProbeHost rejects, when the endpoint is saved, the targets that are
// known without resolving: a blocked literal IP or a localhost name. The dial
// guard stays the control that matters.
func blockedProbeHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && blockedProbeAddress(ip)
}
