package services

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/models"
)

// validateHostHealth checks a host's health settings when they are saved: the
// endpoint's syntax, its target (ADR-0016) and the contact timeout.
func validateHostHealth(endpoint string, timeoutMinutes int) (string, int, error) {
	endpoint, err := parseHealthEndpoint(endpoint)
	if err != nil {
		return "", 0, err
	}
	if endpoint != "" {
		if u, _ := url.Parse(endpoint); blockedProbeHost(u.Hostname()) { // loopback, link-local, metadata
			return "", 0, models.ErrHealthEndpoint
		}
	}
	if timeoutMinutes == 0 {
		timeoutMinutes = 5
	}
	if timeoutMinutes < 1 || timeoutMinutes > 1440 {
		return "", 0, models.ErrContactTimeout
	}
	return endpoint, timeoutMinutes * 60, nil
}

// parseHealthEndpoint checks only the syntax: http(s), a host, no credentials,
// query or fragment, a valid port. "" means no endpoint.
func parseHealthEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.HasSuffix(u.Host, ":") {
		return "", models.ErrHealthEndpoint
	}
	if !validPort(u.Port()) {
		return "", models.ErrHealthEndpoint
	}
	return u.String(), nil
}

// validPort accepts no port (the scheme's default) or 1–65535.
func validPort(port string) bool {
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

type HostHealthRepository interface {
	ClaimDueHealthTargets(ctx context.Context, now time.Time, interval time.Duration, limit int) ([]models.HostHealthTarget, error)
	RecordHealthCheck(context.Context, models.HostHealthTarget, time.Time, bool) error
}

// maxHealthClaim bounds one claim statement; free slots are usually fewer.
const maxHealthClaim = 100

type HostHealthService struct {
	repo        HostHealthRepository
	client      *http.Client
	dialer      *net.Dialer
	concurrency int
	interval    time.Duration
	now         func() time.Time
}

// NewHostHealthService probes with at most concurrency requests in flight
// (HEALTH_CONCURRENCY; below 1 means the default).
func NewHostHealthService(repo HostHealthRepository, concurrency int) *HostHealthService {
	if concurrency < 1 {
		concurrency = config.DefaultHostHealthConcurrency
	}
	dialer := &net.Dialer{Timeout: config.HostHealthTimeout, Control: guardProbeDial}
	return &HostHealthService{repo: repo, concurrency: concurrency, interval: config.HostHealthInterval, now: time.Now, dialer: dialer, client: &http.Client{
		Timeout:       config.HostHealthTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		// No Proxy: a proxy from the environment would dial on the probes' behalf, past the guard.
		Transport: &http.Transport{
			DialContext:            dialer.DialContext,
			TLSHandshakeTimeout:    config.HostHealthTimeout,
			ResponseHeaderTimeout:  config.HostHealthTimeout,
			MaxResponseHeaderBytes: 32 * 1024,
			DisableKeepAlives:      true,
		},
	}}
}

// SetDialControl replaces the SSRF guard of the probes (ADR-0016). Only tests
// whose endpoints listen on loopback call it, with nil.
func (s *HostHealthService) SetDialControl(control func(network, address string, c syscall.RawConn) error) {
	s.dialer.Control = control
}

// Check is one iteration of the health job (ADR-0013): it probes the hosts that
// are due, claiming only as many as there are free slots, so each probe starts as
// soon as it is claimed and a slow host holds one slot for at most the probe
// timeout. It stops claiming at the interval deadline and waits for the probes in
// flight; hosts left unclaimed stay due for the next iteration. No telemetry
// credentials or response bodies are sent to/persisted from health endpoints.
func (s *HostHealthService) Check(ctx context.Context) error {
	deadline := s.now().Add(s.interval)
	run := &probeRun{slots: make(chan struct{}, s.concurrency)}
	for s.now().Before(deadline) && run.acquire(ctx) {
		free := cap(run.slots) - len(run.slots) + 1
		targets, err := s.repo.ClaimDueHealthTargets(ctx, s.now(), s.interval, min(free, maxHealthClaim))
		if err != nil || len(targets) == 0 {
			<-run.slots
			run.fail(err)
			break
		}
		for i, target := range targets {
			if i > 0 {
				run.slots <- struct{}{} // free was counted above; releases only add room
			}
			run.probes.Add(1)
			go func() {
				defer run.release()
				run.fail(s.checkTarget(ctx, target))
			}()
		}
	}
	run.probes.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.Join(run.errs...)
}

// probeRun is the state of one iteration: the probe slots and their failures.
type probeRun struct {
	slots  chan struct{}
	probes sync.WaitGroup
	mu     sync.Mutex
	errs   []error
}

// acquire waits for one free slot; false when the iteration is cancelled.
func (r *probeRun) acquire(ctx context.Context) bool {
	select {
	case r.slots <- struct{}{}:
		return ctx.Err() == nil || r.giveBack()
	case <-ctx.Done():
		return false
	}
}

func (r *probeRun) giveBack() bool {
	<-r.slots
	return false
}

func (r *probeRun) release() {
	<-r.slots
	r.probes.Done()
}

func (r *probeRun) fail(err error) {
	if err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
}

func (s *HostHealthService) checkTarget(ctx context.Context, target models.HostHealthTarget) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Syntax only: a target refused by the SSRF guard (for example one saved
	// before ADR-0016) is probed, refused at the dial and recorded as unhealthy.
	endpoint, err := parseHealthEndpoint(target.HealthEndpoint)
	if err != nil || endpoint == "" {
		return fmt.Errorf("invalid health endpoint for host %d", target.ID)
	}
	started := time.Now().UTC()
	probeCtx, cancel := context.WithTimeout(ctx, config.HostHealthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create health request for host %d", target.ID)
	}
	response, requestErr := s.client.Do(req)
	healthy := false
	if response != nil {
		healthy = requestErr == nil && response.StatusCode >= 200 && response.StatusCode < 300
		_ = response.Body.Close()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := s.repo.RecordHealthCheck(ctx, target, started, healthy); err != nil {
		return fmt.Errorf("record health check for host %d: %w", target.ID, err)
	}
	return nil
}
