// Package observability holds the central's runtime metrics and structured
// logging (ADR-0004). It is cross-cutting: the layers use it, it uses none of
// them. All methods are safe on a nil *Metrics, so code and tests that do not
// care about metrics need no setup.
package observability

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "monitor"

// Latency buckets (seconds) around the scenario targets: ingestion p95 < 300 ms
// (SC1) and dashboard p95 < 1 s (SC2, SC4).
var latencyBuckets = []float64{.005, .01, .025, .05, .1, .2, .3, .5, 1, 2, 5, 10}

// Metrics is the central's metric registry.
type Metrics struct {
	registry *prometheus.Registry
	logger   *slog.Logger

	ingestDuration *prometheus.HistogramVec
	ingestRecords  *prometheus.CounterVec
	ingestBatch    *prometheus.HistogramVec
	ingestLag      *prometheus.HistogramVec
	ingestInflight prometheus.Gauge
	httpDuration   *prometheus.HistogramVec
	jobDuration    *prometheus.HistogramVec
	jobFailures    *prometheus.CounterVec
	jobLastSuccess *prometheus.GaugeVec
}

// Options are the optional data sources read at scrape time.
type Options struct {
	// DB adds the connection-pool statistics (go_sql_*).
	DB *sql.DB
	// OutboxPending returns the number of unsent notifications.
	OutboxPending func(context.Context) (int, error)
}

// NewMetrics registers every metric on a private registry.
func NewMetrics(logger *slog.Logger, opts Options) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		logger:   logger,
		ingestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "ingest_request_duration_seconds",
			Help:    "Duration of gRPC ingestion requests, by method and status code.",
			Buckets: latencyBuckets,
		}, []string{"method", "code"}),
		ingestRecords: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "ingest_records_total",
			Help: "Telemetry records by kind (stats, errors) and outcome (inserted, duplicate, rejected). For stats, inserted also counts re-sent duplicates.",
		}, []string{"kind", "outcome"}),
		ingestBatch: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "ingest_batch_records",
			Help:    "Records per accepted ingestion request.",
			Buckets: []float64{1, 2, 5, 10, 25, 50, 75, 100},
		}, []string{"kind"}),
		ingestLag: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "ingest_lag_seconds",
			Help:    "Delay between a record's collection on the host and its storage in central.",
			Buckets: []float64{.5, 1, 2, 5, 10, 30, 60, 300, 900, 3600, 4 * 3600},
		}, []string{"kind"}),
		ingestInflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace, Name: "ingest_inflight",
			Help: "Ingestion requests being served now (bounded by INGEST_MAX_INFLIGHT, ADR-0006).",
		}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "http_request_duration_seconds",
			Help:    "Duration of HTTP requests, by route pattern, method and status.",
			Buckets: latencyBuckets,
		}, []string{"route", "method", "status"}),
		jobDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "job_duration_seconds",
			Help:    "Duration of each background job run.",
			Buckets: []float64{.01, .05, .1, .5, 1, 2, 5, 10, 30, 60, 300, 625},
		}, []string{"job"}),
		jobFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "job_failures_total",
			Help: "Background job runs that returned an error.",
		}, []string{"job"}),
		jobLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace, Name: "job_last_success_timestamp_seconds",
			Help: "Unix time of each background job's last successful run.",
		}, []string{"job"}),
	}
	m.registry.MustRegister(
		m.ingestDuration, m.ingestRecords, m.ingestBatch, m.ingestLag, m.ingestInflight,
		m.httpDuration, m.jobDuration, m.jobFailures, m.jobLastSuccess,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	if opts.DB != nil {
		m.registry.MustRegister(collectors.NewDBStatsCollector(opts.DB, "monitor"))
	}
	if opts.OutboxPending != nil {
		m.registry.MustRegister(&outboxCollector{count: opts.OutboxPending, logger: logger})
	}
	return m
}

// Handler serves the metrics in the Prometheus exposition format.
func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Registry: m.registry})
}

// RecordIngest counts the outcome of one ingestion request. lags are the delays
// between collection and storage of the inserted records.
func (m *Metrics) RecordIngest(kind string, inserted, duplicates, rejected int, lags []time.Duration) {
	if m == nil {
		return
	}
	m.ingestRecords.WithLabelValues(kind, "inserted").Add(float64(inserted))
	m.ingestRecords.WithLabelValues(kind, "duplicate").Add(float64(duplicates))
	m.ingestRecords.WithLabelValues(kind, "rejected").Add(float64(rejected))
	if accepted := inserted + duplicates; accepted > 0 {
		m.ingestBatch.WithLabelValues(kind).Observe(float64(accepted))
	}
	lag := m.ingestLag.WithLabelValues(kind)
	for _, d := range lags {
		lag.Observe(d.Seconds())
	}
}

// AddInflight tracks the ingestion requests being served (+1 on admission, -1 when done).
func (m *Metrics) AddInflight(delta int) {
	if m == nil {
		return
	}
	m.ingestInflight.Add(float64(delta))
}

// ObserveJob records one run of a background job.
func (m *Metrics) ObserveJob(job string, started time.Time, err error) {
	if m == nil {
		return
	}
	m.jobDuration.WithLabelValues(job).Observe(time.Since(started).Seconds())
	if err != nil {
		m.jobFailures.WithLabelValues(job).Inc()
		return
	}
	m.jobLastSuccess.WithLabelValues(job).SetToCurrentTime()
}

// outboxCollector reads the outbox size at scrape time, with a short timeout so
// a slow database never blocks a scrape.
type outboxCollector struct {
	count  func(context.Context) (int, error)
	logger *slog.Logger
}

var outboxDesc = prometheus.NewDesc(namespace+"_email_outbox_pending", "Notifications not yet sent.", nil, nil)

func (c *outboxCollector) Describe(ch chan<- *prometheus.Desc) { ch <- outboxDesc }

func (c *outboxCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n, err := c.count(ctx)
	if err != nil {
		// A missing sample is visible in Prometheus (absent series); the
		// reason goes to the log.
		if c.logger != nil {
			c.logger.Warn("metrics: count pending notifications", "error", err)
		}
		return
	}
	ch <- prometheus.MustNewConstMetric(outboxDesc, prometheus.GaugeValue, float64(n))
}
