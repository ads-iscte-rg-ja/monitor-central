//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/repositories"
	"monitor-central/internal/services"
)

// TestAV2MeasureProbeCycle measures the health probes with 1,000 hosts, half of
// them hanging past the probe timeout (plan §3, AV2; ADR-0013). It runs the job
// as production does (one iteration per tick, real 5 s timeout) for
// MONITOR_MEDIR_AV2_DURACAO (default 3m), so it only runs with MONITOR_MEDIR_AV2=1.
// The endpoints record when each host is probed. The baseline before ADR-0013
// (one sweep of every host) is in docs/metrics/iter-6/av2-antes.txt.
//
// AV2: a host that stops answering raises the host_offline alert within its
// threshold + 60 s. unavailable_since is the start of the first failed probe, and
// the alert job evaluates every 15 s, so the 60 s must hold the longest gap
// between two probes of the same host plus one evaluation.
func TestAV2MeasureProbeCycle(t *testing.T) {
	if os.Getenv("MONITOR_MEDIR_AV2") != "1" {
		t.Skip("MONITOR_MEDIR_AV2=1 to measure AV2 (several minutes)")
	}
	duration := 3 * time.Minute
	if value := os.Getenv("MONITOR_MEDIR_AV2_DURACAO"); value != "" {
		var err error
		if duration, err = time.ParseDuration(value); err != nil {
			t.Fatal(err)
		}
	}
	const hosts, evaluation, concurrency = 1000, 15 * time.Second, config.DefaultHostHealthConcurrency
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()

	var mu sync.Mutex
	probes := map[string][]time.Time{}
	inFlight, peak := 0, 0
	endpoints := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		probes[r.URL.Path] = append(probes[r.URL.Path], time.Now())
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		defer func() { mu.Lock(); inFlight--; mu.Unlock() }()
		if strings.HasPrefix(r.URL.Path, "/lento/") {
			<-r.Context().Done() // never answers: the probe times out
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer endpoints.Close()
	// Odd hosts hang.
	if _, err := db.ExecContext(ctx, `INSERT INTO hosts(name,hostname,api_key_digest,api_key_masked,status,health_endpoint)
 SELECT 'av2-'||i, 'av2-'||i||'.local', md5('av2-'||i), 'av2', 'online',
  $1 || CASE WHEN i % 2 = 1 THEN '/lento/' ELSE '/ok/' END || i
 FROM generate_series(1, $2) AS i`, endpoints.URL, hosts); err != nil {
		t.Fatal(err)
	}

	// The job loop of internal/server/jobs.go: one iteration, then the next tick.
	service := services.NewHostHealthService(repositories.NewHostsRepository(db, testHasher(t)), concurrency)
	service.SetDialControl(nil) // the endpoints listen on loopback, which the SSRF guard refuses (ADR-0016)
	run, stop := context.WithTimeout(ctx, duration)
	defer stop()
	ticker := time.NewTicker(config.HostHealthTick)
	defer ticker.Stop()
	var iterations []time.Duration
	for run.Err() == nil {
		started := time.Now()
		if err := service.Check(run); err != nil && run.Err() == nil {
			t.Fatal(err)
		}
		iterations = append(iterations, time.Since(started))
		select {
		case <-run.Done():
		case <-ticker.C:
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(probes) != hosts {
		t.Fatalf("%d of %d hosts probed", len(probes), hosts)
	}
	var gaps []float64
	total := 0
	for _, times := range probes {
		total += len(times)
		for i := 1; i < len(times); i++ {
			gaps = append(gaps, times[i].Sub(times[i-1]).Seconds())
		}
	}
	if len(gaps) == 0 {
		t.Fatal("no host was probed twice; run for longer")
	}
	sort.Float64s(gaps)
	sort.Slice(iterations, func(i, j int) bool { return iterations[i] < iterations[j] })
	worstGap := time.Duration(gaps[len(gaps)-1] * float64(time.Second))
	worst := worstGap + evaluation
	verdict := "cumpre"
	if worst > 60*time.Second {
		verdict = "FALHA"
	}
	t.Logf(`AV2 (%d hosts, metade em timeout; timeout %s, concorrência %d, intervalo %s ± 10 %%, job a cada %s; %s)
  probes: %d (%.1f/s), no máximo %d em simultâneo
  intervalo entre probes do mesmo host: p50 %.1f s, p99 %.1f s, máx %.1f s
  pior atraso até ao alerta, além do limiar: máx + avaliação (%s) = %s (alvo ≤ 60 s): %s
  hosts em "unknown" (intervalo > %s): %s
  iterações do job: %d, a mais longa %s (o heartbeat marca parado acima de %s)`,
		hosts, config.HostHealthTimeout, concurrency, config.HostHealthInterval, config.HostHealthTick, duration,
		total, float64(total)/duration.Seconds(), peak,
		gaps[len(gaps)/2], gaps[len(gaps)*99/100], gaps[len(gaps)-1],
		evaluation, worst.Round(100*time.Millisecond), verdict,
		config.HostHealthFreshness, map[bool]string{true: "nenhum", false: "sim"}[worstGap <= config.HostHealthFreshness],
		len(iterations), iterations[len(iterations)-1].Round(100*time.Millisecond), 2*config.HostHealthInterval)
	if worst > 60*time.Second {
		t.Errorf("AV2: %s > 60 s", worst)
	}
}
