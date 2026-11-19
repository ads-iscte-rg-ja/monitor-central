package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("scrape status %d", w.Code)
	}
	return w.Body.String()
}

func requireLines(t *testing.T, body string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(body, line) {
			t.Errorf("metrics output is missing %q", line)
		}
	}
}

func TestIngestAndJobMetricsAreExposed(t *testing.T) {
	m := NewMetrics(nil, Options{OutboxPending: func(context.Context) (int, error) { return 3, nil }})
	m.RecordIngest("errors", 4, 2, 0, []time.Duration{2 * time.Second, 40 * time.Second})
	m.RecordIngest("stats", 0, 0, 1, nil)
	m.ObserveJob("host_health", time.Now(), nil)
	m.ObserveJob("metric_alerts", time.Now(), errors.New("db down"))

	body := scrape(t, m)
	requireLines(t, body,
		`monitor_ingest_records_total{kind="errors",outcome="inserted"} 4`,
		`monitor_ingest_records_total{kind="errors",outcome="duplicate"} 2`,
		`monitor_ingest_records_total{kind="stats",outcome="rejected"} 1`,
		`monitor_ingest_batch_records_count{kind="errors"} 1`,
		`monitor_ingest_lag_seconds_count{kind="errors"} 2`,
		`monitor_ingest_lag_seconds_bucket{kind="errors",le="5"} 1`,
		`monitor_job_failures_total{job="metric_alerts"} 1`,
		`monitor_job_last_success_timestamp_seconds{job="host_health"}`,
		`monitor_job_duration_seconds_count{job="host_health"} 1`,
		`monitor_email_outbox_pending 3`,
		`go_goroutines`,
	)
	if strings.Contains(body, `monitor_job_last_success_timestamp_seconds{job="metric_alerts"}`) {
		t.Error("a failed run must not update the last-success time")
	}
}

func TestOutboxFailureOmitsTheSampleInsteadOfFailingTheScrape(t *testing.T) {
	var logs bytes.Buffer
	m := NewMetrics(slog.New(slog.NewJSONHandler(&logs, nil)), Options{OutboxPending: func(context.Context) (int, error) { return 0, errors.New("timeout") }})
	if body := scrape(t, m); strings.Contains(body, "monitor_email_outbox_pending ") {
		t.Fatal("a failed count must not be reported as a value")
	}
	if !strings.Contains(logs.String(), "count pending notifications") {
		t.Fatal("the failure must be logged")
	}
}

func TestHTTPMiddlewareLabelsByRoutePatternAndLogsTheRequestID(t *testing.T) {
	var logs bytes.Buffer
	m := NewMetrics(nil, Options{})
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(m.HTTPMiddleware(slog.New(slog.NewJSONHandler(&logs, nil))))
	r.Get("/hosts/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hosts/42", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hosts/43", nil))

	requireLines(t, scrape(t, m), `monitor_http_request_duration_seconds_count{method="GET",route="/hosts/{id}",status="404"} 2`)
	var entry map[string]any
	if err := json.Unmarshal(bytes.SplitN(logs.Bytes(), []byte("\n"), 2)[0], &entry); err != nil {
		t.Fatal(err)
	}
	if entry["route"] != "/hosts/{id}" || entry["status"] != float64(404) || entry["request_id"] == "" || entry["level"] != "WARN" {
		t.Fatalf("unexpected log entry %v", entry)
	}
}

func TestGRPCInterceptorCountsByCodeAndLogsTheHost(t *testing.T) {
	var logs bytes.Buffer
	m := NewMetrics(nil, Options{})
	intercept := m.GRPCInterceptor(slog.New(slog.NewJSONHandler(&logs, nil)))
	info := &grpc.UnaryServerInfo{FullMethod: "/monitor.ingest.v1.IngestService/IngestStats"}

	_, _ = intercept(context.Background(), nil, info, func(ctx context.Context, _ any) (any, error) {
		SetHostID(ctx, 7)
		return nil, status.Error(codes.InvalidArgument, "invalid record")
	})
	_, _ = intercept(context.Background(), nil, info, func(context.Context, any) (any, error) { return "ok", nil })

	requireLines(t, scrape(t, m),
		`monitor_ingest_request_duration_seconds_count{code="InvalidArgument",method="/monitor.ingest.v1.IngestService/IngestStats"} 1`,
		`monitor_ingest_request_duration_seconds_count{code="OK",method="/monitor.ingest.v1.IngestService/IngestStats"} 1`,
	)
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("only the failed call is logged at info level, got %d lines", len(lines))
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["host_id"] != float64(7) || entry["code"] != "InvalidArgument" || entry["level"] != "WARN" {
		t.Fatalf("unexpected log entry %v", entry)
	}
}

func TestInflightGaugeFollowsAdmissions(t *testing.T) {
	m := NewMetrics(nil, Options{})
	m.AddInflight(1)
	m.AddInflight(1)
	m.AddInflight(-1)
	requireLines(t, scrape(t, m), "monitor_ingest_inflight 1")
}

func TestNilMetricsAreANoOp(t *testing.T) {
	var m *Metrics
	m.AddInflight(1)
	m.RecordIngest("stats", 1, 0, 0, nil)
	m.ObserveJob("x", time.Now(), nil)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusNotFound {
		t.Fatal("disabled metrics must not serve data")
	}
}

func TestNewLoggerValidatesFormatAndLevel(t *testing.T) {
	var out bytes.Buffer
	_, legacy, err := NewLogger(&out, "json", "warn")
	if err != nil {
		t.Fatal(err)
	}
	legacy.Printf("legacy %s", "line") // info: below warn, dropped
	slog.Warn("structured", "host_id", 7)
	if strings.Contains(out.String(), "legacy line") || !strings.Contains(out.String(), `"host_id":7`) {
		t.Fatalf("unexpected output %q", out.String())
	}
	for _, bad := range [][2]string{{"xml", "info"}, {"json", "loud"}} {
		if _, _, err := NewLogger(io.Discard, bad[0], bad[1]); err == nil {
			t.Fatalf("format %q level %q accepted", bad[0], bad[1])
		}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}
