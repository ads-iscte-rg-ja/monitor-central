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
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/models"
)

func validateHostHealth(endpoint string, timeoutMinutes int) (string, int, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || len(endpoint) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.HasSuffix(u.Host, ":") {
			return "", 0, models.ErrHealthEndpoint
		}
		if u.Port() != "" {
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return "", 0, models.ErrHealthEndpoint
			}
		}
		endpoint = u.String()
	}
	if timeoutMinutes == 0 {
		timeoutMinutes = 5
	}
	if timeoutMinutes < 1 || timeoutMinutes > 1440 {
		return "", 0, models.ErrContactTimeout
	}
	return endpoint, timeoutMinutes * 60, nil
}

type HostHealthRepository interface {
	ListHealthTargets(context.Context, int64, int) ([]models.HostHealthTarget, error)
	RecordHealthCheck(context.Context, models.HostHealthTarget, time.Time, bool) error
}

type HostHealthService struct {
	repo   HostHealthRepository
	client *http.Client
}

func NewHostHealthService(repo HostHealthRepository) *HostHealthService {
	return &HostHealthService{repo: repo, client: &http.Client{
		Timeout:       config.HostHealthTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			DialContext:            (&net.Dialer{Timeout: config.HostHealthTimeout}).DialContext,
			TLSHandshakeTimeout:    config.HostHealthTimeout,
			ResponseHeaderTimeout:  config.HostHealthTimeout,
			MaxResponseHeaderBytes: 32 * 1024,
			DisableKeepAlives:      true,
		},
	}}
}

// Check probes pages of targets with bounded concurrency. No telemetry credentials
// or response bodies are sent to/persisted from application health endpoints.
func (s *HostHealthService) Check(ctx context.Context) error {
	var afterID int64
	for ctx.Err() == nil {
		targets, err := s.repo.ListHealthTargets(ctx, afterID, 100)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return nil
		}
		jobs := make(chan models.HostHealthTarget)
		failures := make(chan error, len(targets))
		var workers sync.WaitGroup
		for i := 0; i < min(config.HostHealthConcurrency, len(targets)); i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for target := range jobs {
					if err := s.checkTarget(ctx, target); err != nil {
						failures <- err
					}
				}
			}()
		}
	queue:
		for _, target := range targets {
			select {
			case jobs <- target:
			case <-ctx.Done():
				break queue
			}
		}
		close(jobs)
		workers.Wait()
		close(failures)
		var errs []error
		for err := range failures {
			errs = append(errs, err)
		}
		if err := errors.Join(errs...); err != nil {
			return err
		}
		afterID = targets[len(targets)-1].ID
	}
	return ctx.Err()
}

func (s *HostHealthService) checkTarget(ctx context.Context, target models.HostHealthTarget) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	endpoint, _, err := validateHostHealth(target.HealthEndpoint, 5)
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
