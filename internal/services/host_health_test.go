package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"monitor-central/internal/models"
)

type fakeHostHealthRepository struct {
	targets []models.HostHealthTarget
	mu      sync.Mutex
	results map[int64]bool
}

func (f *fakeHostHealthRepository) ListHealthTargets(_ context.Context, after int64, limit int) ([]models.HostHealthTarget, error) {
	var result []models.HostHealthTarget
	for _, target := range f.targets {
		if target.ID > after {
			result = append(result, target)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (f *fakeHostHealthRepository) RecordHealthCheck(_ context.Context, target models.HostHealthTarget, _ time.Time, healthy bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.results == nil {
		f.results = make(map[int64]bool)
	}
	f.results[target.ID] = healthy
	return nil
}

type hostHealthTransportFunc func(*http.Request) (*http.Response, error)

func (f hostHealthTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHostHealthConfigurationAndUpdates(t *testing.T) {
	repo := &fakeHostsRepository{}
	s := NewHostsService(repo)
	h, err := s.CreateHost(context.Background(), models.CreateHostRequest{Name: "app", Hostname: "app.local", HealthEndpoint: " http://10.0.0.10:8080/health ", ContactTimeoutMinutes: 120})
	if err != nil || h.HealthEndpoint != "http://10.0.0.10:8080/health" || h.ContactTimeoutSeconds != 7200 {
		t.Fatal("health configuration not preserved", h, err)
	}
	if err := s.UpdateHost(context.Background(), h.ID, models.UpdateHostRequest{Name: "app", Hostname: "app.local", ContactTimeoutMinutes: 60}); err != nil || repo.host.HealthEndpoint != "" || repo.host.ContactTimeoutSeconds != 3600 {
		t.Fatal("health source cannot be edited", repo.host, err)
	}
	for _, endpoint := range []string{"/health", "file:///etc/passwd", "ftp://host/health", "https://user:secret@host/health", "https://host/health?token=secret", "https://host/health#fragment", "http://host:0/health", "http://host:65536/health", strings.Repeat("x", 2049)} {
		if _, err := s.CreateHost(context.Background(), models.CreateHostRequest{Name: "app", Hostname: "app.local", HealthEndpoint: endpoint}); !errors.Is(err, models.ErrHealthEndpoint) {
			t.Fatal("invalid endpoint accepted", err)
		}
	}
	for _, minutes := range []int{-1, 1441} {
		if _, err := s.CreateHost(context.Background(), models.CreateHostRequest{Name: "app", Hostname: "app.local", ContactTimeoutMinutes: minutes}); !errors.Is(err, models.ErrContactTimeout) {
			t.Fatal("invalid timeout accepted", minutes, err)
		}
	}
}

func TestHealthChecksUseStatusWithoutCredentialsOrRedirects(t *testing.T) {
	for _, status := range []int{200, 204, 301, 503, 0} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			repo := &fakeHostHealthRepository{targets: []models.HostHealthTarget{{ID: 1, HealthEndpoint: "https://app.example/health"}}}
			s := NewHostHealthService(repo)
			calls := 0
			s.client.Transport = hostHealthTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Host != "app.example" || r.Header.Get("X-API-Key") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("health request leaked credentials or followed a redirect")
				}
				if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > 6*time.Second {
					t.Error("probe is not time bounded")
				}
				if status == 0 {
					return nil, errors.New("network failure")
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://other.example/health"}}, Body: io.NopCloser(strings.NewReader("ignored response body"))}, nil
			})
			if err := s.Check(context.Background()); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(repo.results) != 1 || repo.results[1] != (status >= 200 && status < 300) {
				t.Fatal("incorrect health result", calls, repo.results)
			}
		})
	}
}

func TestHealthCancellationDoesNotRecordHostFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &fakeHostHealthRepository{targets: []models.HostHealthTarget{{ID: 1, HealthEndpoint: "https://app.example/health"}}}
	s := NewHostHealthService(repo)
	s.client.Transport = hostHealthTransportFunc(func(*http.Request) (*http.Response, error) { cancel(); return nil, context.Canceled })
	if err := s.Check(ctx); !errors.Is(err, context.Canceled) || len(repo.results) != 0 {
		t.Fatal("shutdown was reported as host failure", repo.results, err)
	}
}

func TestHealthChecksReachTargetsBeyondFirstPage(t *testing.T) {
	repo := &fakeHostHealthRepository{}
	for id := int64(1); id <= 205; id++ {
		repo.targets = append(repo.targets, models.HostHealthTarget{ID: id, HealthEndpoint: "https://app.example/health"})
	}
	s := NewHostHealthService(repo)
	s.client.Transport = hostHealthTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if err := s.Check(context.Background()); err != nil || len(repo.results) != 205 {
		t.Fatal("health sweep stopped at the first page", len(repo.results), err)
	}
}
