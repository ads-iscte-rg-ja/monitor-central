package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"monitor-central/internal/models"
)

// SE3 (ADR-0016): the address ranges the probes may and may not reach.
func TestProbeGuardRanges(t *testing.T) {
	for addr, blocked := range map[string]bool{
		"127.0.0.1": true, "127.10.0.1": true, "::1": true, // loopback
		"169.254.169.254": true, "169.254.0.1": true, "fe80::1": true, // link-local, cloud metadata
		"0.0.0.0": true, "::": true, "224.0.0.1": true, "ff02::1": true, // unspecified, multicast
		"fd00:ec2::254": true, "100.100.100.200": true, // metadata outside those ranges
		"::ffff:127.0.0.1": true, "::ffff:169.254.169.254": true, // IPv4-mapped
		"10.0.0.10": false, "172.16.5.4": false, "192.168.1.20": false, "fd12:3456::1": false, // internal networks
		"203.0.113.7": false, "2001:db8::7": false, // public
	} {
		if got := blockedProbeAddress(netip.MustParseAddr(addr)); got != blocked {
			t.Errorf("%s: blocked=%v, want %v", addr, got, blocked)
		}
		err := guardProbeDial("tcp", netipAddrPort(addr), nil)
		if (err != nil) != blocked || (err != nil && !errors.Is(err, ErrBlockedProbeTarget)) {
			t.Errorf("dial %s: %v", addr, err)
		}
	}
	if guardProbeDial("tcp", "no-port", nil) == nil {
		t.Error("an address without a port must be refused")
	}
}

func netipAddrPort(addr string) string {
	return netip.AddrPortFrom(netip.MustParseAddr(addr), 80).String()
}

// The form refuses what is known without resolving; private networks pass.
func TestHealthEndpointFormRefusesBlockedTargets(t *testing.T) {
	s := NewHostsService(&fakeHostsRepository{})
	for _, endpoint := range []string{
		"http://127.0.0.1:8080/health", "http://localhost/health", "http://app.localhost/health", "http://LOCALHOST./h",
		"http://169.254.169.254/latest/meta-data/", "http://[::1]:9000/health", "http://[fe80::1]/health", "http://0.0.0.0/health",
	} {
		if _, err := s.CreateHost(context.Background(), models.CreateHostRequest{Name: "app", Hostname: "app.local", HealthEndpoint: endpoint}); !errors.Is(err, models.ErrHealthEndpoint) {
			t.Errorf("%s accepted: %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{"http://10.0.0.10:8080/health", "https://app.internal/health", "http://[fd12::1]/health"} {
		if _, err := s.CreateHost(context.Background(), models.CreateHostRequest{Name: "app", Hostname: "app.local", HealthEndpoint: endpoint}); err != nil {
			t.Errorf("%s refused: %v", endpoint, err)
		}
	}
}

// A real probe to loopback, by IP and by a name that resolves to it, is stopped
// at the dial: the host is recorded unhealthy and no request reaches the server.
func TestProbeToLoopbackNeverReachesIt(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	port := server.URL[strings.LastIndex(server.URL, ":"):]
	repo := &fakeHostHealthRepository{targets: []models.HostHealthTarget{
		{ID: 1, HealthEndpoint: "http://127.0.0.1" + port + "/health"},
		{ID: 2, HealthEndpoint: "http://localhost" + port + "/health"}, // the name is resolved, then refused
	}}
	if err := NewHostHealthService(repo, 0).Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 || len(repo.results) != 2 || repo.results[1] || repo.results[2] {
		t.Fatalf("requests=%d results=%v", requests.Load(), repo.results)
	}

	// Control: without the guard the same server answers, so the guard is what stopped it.
	repo = &fakeHostHealthRepository{targets: []models.HostHealthTarget{{ID: 1, HealthEndpoint: "http://127.0.0.1" + port + "/health"}}}
	open := NewHostHealthService(repo, 0)
	open.SetDialControl(nil)
	if err := open.Check(context.Background()); err != nil || requests.Load() != 1 || !repo.results[1] {
		t.Fatalf("control: err=%v requests=%d results=%v", err, requests.Load(), repo.results)
	}
}
