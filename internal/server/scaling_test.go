package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"monitor-central/internal/config"
	"monitor-central/internal/observability"
)

type fakeLeader struct {
	leads  map[string]bool
	err    error
	closed bool
}

func (f *fakeLeader) Lead(_ context.Context, job string) (bool, error) { return f.leads[job], f.err }
func (f *fakeLeader) Close()                                           { f.closed = true }

func TestJobsRunOnlyWhereTheInstanceLeads(t *testing.T) {
	leader := &fakeLeader{leads: map[string]bool{"led": true}}
	s := &Server{elector: leader, metrics: observability.NewMetrics(nil, observability.Options{}), logger: log.New(io.Discard, "", 0)}
	runs := map[string]int{}
	for _, job := range []string{"led", "other"} {
		if err := s.runJob(context.Background(), job, func(context.Context) error { runs[job]++; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if runs["led"] != 1 || runs["other"] != 0 {
		t.Fatalf("only the led job must run here: %v", runs)
	}
	w := httptest.NewRecorder()
	s.metrics.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, want := range []string{`monitor_job_leader{job="led"} 1`, `monitor_job_leader{job="other"} 0`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("metrics must show %s", want)
		}
	}

	leader.err = errors.New("connection reset")
	ran := false
	if err := s.runJob(context.Background(), "led", func(context.Context) error { ran = true; return nil }); err == nil || ran {
		t.Fatal("a failed election must be reported and the job skipped")
	}
}

func TestWorkerAnswersOnlyTheHealthCheck(t *testing.T) {
	app := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	health := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	status := func(h http.Handler, path string) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w.Code
	}
	if h := httpHandler(config.RoleWeb, app, health); status(h, "/dashboard") != http.StatusTeapot {
		t.Fatal("web must serve the application")
	}
	worker := httpHandler(config.RoleWorker, app, health)
	if status(worker, "/health") != http.StatusOK || status(worker, "/dashboard") != http.StatusNotFound {
		t.Fatal("a worker must answer the health check and nothing else")
	}
}

func TestOnlyServingInstancesListenForIngestion(t *testing.T) {
	if ln, err := (&Server{role: config.RoleWorker, grpcAddr: "127.0.0.1:0"}).listenGRPC(); err != nil || ln != nil {
		t.Fatalf("a worker must not listen for ingestion: %v %v", ln, err)
	}
	ln, err := (&Server{role: config.RoleAll, grpcAddr: "127.0.0.1:0"}).listenGRPC()
	if err != nil || ln == nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	if _, err := (&Server{role: config.RoleWeb, grpcAddr: "256.0.0.1:1"}).listenGRPC(); err == nil {
		t.Fatal("an invalid address must be reported")
	}
}
