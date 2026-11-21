package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"monitor-central/internal/observability"
)

func metricsText(t *testing.T, m *observability.Metrics) string {
	t.Helper()
	srv := newMetricsServer("127.0.0.1:0", m)
	if srv == nil {
		t.Fatal("metrics server not created")
	}
	w := &recorder{header: http.Header{}}
	srv.Handler.ServeHTTP(w, mustRequest(t, "/metrics"))
	return w.body.String()
}

type recorder struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *recorder) WriteHeader(code int)        { r.code = code }

func mustRequest(t *testing.T, path string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestRunJobRecordsOutcomeButNotShutdown(t *testing.T) {
	m := observability.NewMetrics(nil, observability.Options{})
	s := &Server{metrics: m}

	_ = s.runJob(context.Background(), "ok_job", func(context.Context) error { return nil })
	_ = s.runJob(context.Background(), "bad_job", func(context.Context) error { return errors.New("db down") })
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.runJob(cancelled, "stopping_job", func(ctx context.Context) error { return ctx.Err() }); !errors.Is(err, context.Canceled) {
		t.Fatalf("runJob must return the job's error, got %v", err)
	}

	body := metricsText(t, m)
	for _, line := range []string{
		`monitor_job_last_success_timestamp_seconds{job="ok_job"}`,
		`monitor_job_failures_total{job="bad_job"} 1`,
	} {
		if !strings.Contains(body, line) {
			t.Errorf("missing %q", line)
		}
	}
	if strings.Contains(body, `job="stopping_job"`) {
		t.Error("a run interrupted by shutdown must not be recorded as a failure")
	}
}

func TestMetricsListenerIsOptionalAndServesMetrics(t *testing.T) {
	if newMetricsServer("", nil) != nil {
		t.Fatal("an empty METRICS_ADDR must disable the listener")
	}
	stop, err := (&Server{}).startMetrics()
	if err != nil {
		t.Fatal(err)
	}
	stop() // no listener configured: a no-op

	m := observability.NewMetrics(nil, observability.Options{})
	s := &Server{metricsSrv: newMetricsServer("127.0.0.1:0", m)}
	stop, err = s.startMetrics()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	// The listener is bound to an ephemeral port; serve one request through the
	// handler to confirm the route.
	if body := metricsText(t, m); !strings.Contains(body, "go_goroutines") {
		t.Fatal("/metrics did not serve the registry")
	}

	busy := &Server{metricsSrv: &http.Server{Addr: "256.0.0.1:1", ReadHeaderTimeout: time.Second}}
	if _, err := busy.startMetrics(); err == nil {
		t.Fatal("an invalid metrics address must fail at startup")
	}
}

func TestRoutesAreListedAsStructuredDebugLines(t *testing.T) {
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	r := chi.NewRouter()
	noop := func(http.ResponseWriter, *http.Request) {}
	r.Get("/dashboard", noop)
	r.Get("/health", noop)
	printRoutes(r, log.New(io.Discard, "", 0), []route{{http.MethodGet, "/dashboard", noop}})

	text := out.String()
	if !strings.Contains(text, `"route":"/dashboard","protected":true`) || !strings.Contains(text, `"route":"/health","protected":false`) {
		t.Fatalf("unexpected route log %q", text)
	}
}
