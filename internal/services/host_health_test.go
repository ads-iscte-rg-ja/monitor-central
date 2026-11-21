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
	claimed map[int64]bool
	claims  []int // limit of each claim
	err     error
}

// ClaimDueHealthTargets hands out each target once, as the booking of the next
// check does in PostgreSQL.
func (f *fakeHostHealthRepository) ClaimDueHealthTargets(_ context.Context, _ time.Time, _ time.Duration, limit int) ([]models.HostHealthTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims = append(f.claims, limit)
	if f.err != nil {
		return nil, f.err
	}
	if f.claimed == nil {
		f.claimed = make(map[int64]bool)
	}
	var result []models.HostHealthTarget
	for _, target := range f.targets {
		if !f.claimed[target.ID] && len(result) < limit {
			f.claimed[target.ID] = true
			result = append(result, target)
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
			s := NewHostHealthService(repo, 0)
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
	s := NewHostHealthService(repo, 0)
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
	s := NewHostHealthService(repo, 0)
	s.client.Transport = hostHealthTransportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if err := s.Check(context.Background()); err != nil || len(repo.results) != 205 {
		t.Fatal("health sweep stopped at the first page", len(repo.results), err)
	}
}

func okResponse() *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}
}

// ADR-0013: never more probes in flight than the configured concurrency, and
// never a claim for more hosts than there are free slots.
func TestHealthChecksStayWithinTheConcurrency(t *testing.T) {
	repo := &fakeHostHealthRepository{}
	for id := int64(1); id <= 40; id++ {
		repo.targets = append(repo.targets, models.HostHealthTarget{ID: id, HealthEndpoint: "https://app.example/health"})
	}
	s := NewHostHealthService(repo, 4)
	var mu sync.Mutex
	inFlight, peak := 0, 0
	release := make(chan struct{})
	var once sync.Once
	s.client.Transport = hostHealthTransportFunc(func(*http.Request) (*http.Response, error) {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		full := inFlight == 4
		mu.Unlock()
		if full { // the first wave fills every slot; then let them all go
			once.Do(func() { close(release) })
		}
		<-release
		mu.Lock()
		inFlight--
		mu.Unlock()
		return okResponse(), nil
	})
	if err := s.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.results) != 40 || peak != 4 {
		t.Fatalf("results=%d peak=%d, want 40 and 4", len(repo.results), peak)
	}
	for _, limit := range repo.claims {
		if limit < 1 || limit > 4 {
			t.Fatalf("claimed %d hosts with 4 slots: %v", limit, repo.claims)
		}
	}
}

// The iteration stops claiming at its deadline; the hosts left over stay due.
func TestHealthIterationStopsClaimingAtTheDeadline(t *testing.T) {
	repo := &fakeHostHealthRepository{}
	for id := int64(1); id <= 10; id++ {
		repo.targets = append(repo.targets, models.HostHealthTarget{ID: id, HealthEndpoint: "https://app.example/health"})
	}
	s := NewHostHealthService(repo, 1)
	clock := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	s.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	s.client.Transport = hostHealthTransportFunc(func(*http.Request) (*http.Response, error) {
		mu.Lock()
		clock = clock.Add(10 * time.Second) // each probe "takes" 10 s of the 30 s interval
		mu.Unlock()
		return okResponse(), nil
	})
	if err := s.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(repo.results); n < 3 || n > 4 {
		t.Fatalf("%d hosts probed in a 30 s iteration of 10 s probes", n)
	}
}

func TestHealthClaimFailureIsReported(t *testing.T) {
	repo := &fakeHostHealthRepository{err: errors.New("database down")}
	if err := NewHostHealthService(repo, 0).Check(context.Background()); err == nil || !strings.Contains(err.Error(), "database down") {
		t.Fatalf("claim failure hidden: %v", err)
	}
}
