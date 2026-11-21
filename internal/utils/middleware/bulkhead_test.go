package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"monitor-central/internal/config"
)

func TestBulkheadServesWithinTheLimitAndSetsTheDeadline(t *testing.T) {
	var deadline time.Time
	var ok bool
	h := Bulkhead(config.DashboardConfig{MaxInflight: 1, QueryTimeout: 3 * time.Second}, time.Second)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deadline, ok = r.Context().Deadline()
			w.WriteHeader(http.StatusNoContent)
		}))
	start := time.Now()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if !ok || deadline.Before(start.Add(3*time.Second)) || deadline.After(time.Now().Add(3*time.Second)) {
		t.Fatalf("deadline %v (set: %v), want about 3 s from the request", deadline, ok)
	}
	// The slot is released: a second request is served too.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("second request: status %d", rec.Code)
	}
}

func TestBulkheadAnswers503WhenFull(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	h := Bulkhead(config.DashboardConfig{MaxInflight: 1, QueryTimeout: time.Minute}, 50*time.Millisecond)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(entered)
			<-release
		}))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/errors", nil))
	}()
	<-entered

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/errors", nil))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	close(release)
	wg.Wait()
}

func TestBulkheadStopsWaitingWhenTheClientLeaves(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	called := 0
	h := Bulkhead(config.DashboardConfig{MaxInflight: 1, QueryTimeout: time.Minute}, time.Minute)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called++
			if called == 1 {
				close(entered)
				<-release
			}
		}))
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hosts", nil))
		close(done)
	}()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hosts", nil).WithContext(ctx))
	if called != 1 {
		t.Fatalf("handler called %d times; the cancelled request must not be served", called)
	}
	close(release)
	<-done
}
