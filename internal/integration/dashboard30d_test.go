//go:build integration

package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/database"
	"monitor-central/internal/models"
	"monitor-central/internal/repositories"
	"monitor-central/internal/seed"
	"monitor-central/internal/services"
)

// counterMismatches counts the (hour, host, level) groups where the hourly
// counters (ADR-0017) differ from a COUNT(*) over error_logs: must be 0.
func counterMismatches(t *testing.T, db *database.DB) int {
	t.Helper()
	return count(t, db, `SELECT COUNT(*) FROM (
	  SELECT date_bin('1 hour', created_at, TIMESTAMPTZ '2000-01-01 00:00:00+00') AS bucket, host_id, level, COUNT(*) AS n
	  FROM error_logs GROUP BY 1, 2, 3) e
	 FULL JOIN (SELECT * FROM error_counts_hourly WHERE count > 0) c USING (bucket, host_id, level)
	 WHERE e.n IS DISTINCT FROM c.count`)
}

func errorsAt(when time.Time, firstID int64, levels ...string) *models.ErrorLogsRequest {
	req := &models.ErrorLogsRequest{}
	for i, level := range levels {
		req.Errors = append(req.Errors, models.ErrorLogInput{ID: firstID + int64(i), AppName: "app", Environment: "test",
			Level: level, Message: "failure", CreatedAt: when})
	}
	return req
}

func TestErrorCountersFollowEveryWrite(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	a, b := host(t, db, "counters-a"), host(t, db, "counters-b")
	s := services.NewIngestionService(repositories.NewMonitoringRepository(db, testHasher(t)))
	now := time.Now().UTC()
	old := now.Add(-3 * 24 * time.Hour)
	for _, batch := range []struct {
		host int64
		req  *models.ErrorLogsRequest
	}{
		{a.ID, errorsAt(now, 1, models.LevelError, models.LevelCritical, models.LevelError)},
		{a.ID, errorsAt(old, 10, models.LevelWarn)},
		{b.ID, errorsAt(now, 1, models.LevelPanic)},
		// A resent batch is deduplicated by the receipts: no row, no count.
		{a.ID, errorsAt(now, 1, models.LevelError, models.LevelCritical, models.LevelError)},
	} {
		if _, _, err := s.IngestErrors(ctx, batch.host, batch.req); err != nil {
			t.Fatal(err)
		}
	}
	if counterMismatches(t, db) != 0 || count(t, db, "SELECT SUM(count) FROM error_counts_hourly") != 5 {
		t.Fatal("counters differ from the inserted errors")
	}

	// The demo seed upserts its error with ON CONFLICT DO UPDATE: the update
	// moves it between hours, and the counters follow.
	for i := 0; i < 2; i++ {
		if err := seed.Demo(ctx, db, testHasher(t)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE error_logs SET created_at = created_at - INTERVAL '5 hours' WHERE app_name = 'demo-app'"); err != nil {
			t.Fatal(err)
		}
	}
	if counterMismatches(t, db) != 0 {
		t.Fatal("counters differ after updates")
	}

	// Retention removes the old errors and then their expired counters.
	r := repositories.NewRetentionRepository(db)
	counts, err := r.Cleanup(ctx, config.RetentionConfig{ErrorDays: 1, BatchSize: 100}, now)
	if err != nil || counts["error_logs"] != 1 || counts["error_counts_hourly"] != 1 {
		t.Fatalf("retention: %v %v", counts, err)
	}
	if counterMismatches(t, db) != 0 || count(t, db, "SELECT COUNT(*) FROM error_counts_hourly WHERE bucket < $1", now.Add(-24*time.Hour)) != 0 {
		t.Fatal("expired counters kept or counters wrong after retention")
	}

	// Deleting a host deletes its errors and its counters.
	if _, err := db.ExecContext(ctx, "DELETE FROM hosts WHERE id = $1", b.ID); err != nil {
		t.Fatal(err)
	}
	if counterMismatches(t, db) != 0 || count(t, db, "SELECT COUNT(*) FROM error_counts_hourly WHERE host_id = $1", b.ID) != 0 {
		t.Fatal("counters of a deleted host kept")
	}
}

func TestDashboardSummaryMatchesTheFullQueries(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	s := services.NewIngestionService(repositories.NewMonitoringRepository(db, testHasher(t)))
	now := time.Now().UTC()
	for i, cpu := range []float64{10, 30, 80} {
		h := host(t, db, "summary-"+string(rune('a'+i)))
		for j, at := range []time.Time{now.Add(-2 * time.Hour), now.Add(-time.Minute)} {
			stat := &models.MachineStatsRequest{ID: int64(j + 1), CPUPercent: cpu + float64(j), RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2, CollectedAt: at}
			if _, err := s.IngestStats(ctx, h.ID, stat); err != nil {
				t.Fatal(err)
			}
		}
		levels := []string{models.LevelError, models.LevelCritical, models.LevelPanic, models.LevelInfo}[:i+2]
		if _, _, err := s.IngestErrors(ctx, h.ID, errorsAt(now.Add(-time.Minute), 1, levels...)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.IngestErrors(ctx, h.ID, errorsAt(now.Add(-48*time.Hour), 100, models.LevelPanic)); err != nil {
			t.Fatal(err)
		}
	}
	host(t, db, "no-samples") // left out of the average, as before

	repo := repositories.NewDashboardRepository(db)
	total, critical, err := repo.CountErrorsToday(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantTotal := count(t, db, "SELECT COUNT(*) FROM error_logs WHERE created_at >= CURRENT_DATE AND created_at < CURRENT_DATE + INTERVAL '1 day'")
	wantCritical := count(t, db, "SELECT COUNT(*) FROM error_logs WHERE level IN ('CRITICAL','PANIC') AND created_at >= CURRENT_DATE AND created_at < CURRENT_DATE + INTERVAL '1 day'")
	if total != wantTotal || critical != wantCritical {
		t.Fatalf("errors today %d/%d, the full count says %d/%d", total, critical, wantTotal, wantCritical)
	}
	average, err := repo.AverageLatestCPU(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var want float64
	if err := db.QueryRow(`SELECT AVG(cpu_percent) FROM (SELECT DISTINCT ON (host_id) cpu_percent FROM machine_stats ORDER BY host_id, collected_at DESC) s`).Scan(&want); err != nil {
		t.Fatal(err)
	}
	if average != want || average != (11+31+81)/3.0 {
		t.Fatalf("average latest CPU %v, the full query says %v", average, want)
	}
}

func TestErrorLogPageFiltersCapAndAppNames(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	a, b := host(t, db, "page-a"), host(t, db, "page-b")
	now := time.Now().UTC()
	// Inserted directly: the page only reads.
	if _, err := db.ExecContext(ctx, `INSERT INTO error_logs(host_id,app_name,environment,level,message,created_at)
	 SELECT $1, CASE WHEN n % 2 = 0 THEN 'billing' ELSE 'api' END, 'test', CASE WHEN n % 3 = 0 THEN 'CRITICAL' ELSE 'ERROR' END,
	        'timeout ' || n, $3::timestamptz - n * INTERVAL '1 second' FROM generate_series(1, $2) n`,
		a.ID, models.ErrorLogCountCap+50, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO error_logs(host_id,app_name,environment,level,message,created_at)
	 VALUES ($1,'worker','test','WARN','disk almost full',$2)`, b.ID, now); err != nil {
		t.Fatal(err)
	}
	repo := repositories.NewErrorLogsRepository(db)
	for _, tc := range []struct {
		name    string
		filters models.ErrorLogFilters
		want    int
	}{
		{"everything, above the cap", models.ErrorLogFilters{}, models.ErrorLogCountCap + 1},
		{"one host", models.ErrorLogFilters{HostID: b.ID}, 1},
		{"level", models.ErrorLogFilters{Level: "WARN"}, 1},
		{"application", models.ErrorLogFilters{AppName: "worker"}, 1},
		{"search", models.ErrorLogFilters{Search: "almost"}, 1},
		{"combined", models.ErrorLogFilters{HostID: a.ID, AppName: "billing", Search: "timeout 1"}, count(t, db,
			"SELECT COUNT(*) FROM error_logs WHERE host_id=$1 AND app_name='billing' AND message ILIKE '%timeout 1%'", a.ID)},
		{"nothing", models.ErrorLogFilters{HostID: b.ID, Level: "ERROR"}, 0},
	} {
		tc.filters.Page, tc.filters.PageSize = 1, 20
		list, total, err := repo.ListErrorLogs(ctx, tc.filters)
		if err != nil || total != tc.want || len(list) != min(tc.want, 20) {
			t.Fatalf("%s: total %d (want %d), %d rows, %v", tc.name, total, tc.want, len(list), err)
		}
	}
	// Newest first, and the second page continues where the first stopped.
	first, _, _ := repo.ListErrorLogs(ctx, models.ErrorLogFilters{HostID: a.ID, Page: 1, PageSize: 20})
	second, _, _ := repo.ListErrorLogs(ctx, models.ErrorLogFilters{HostID: a.ID, Page: 2, PageSize: 20})
	if len(second) != 20 || !first[0].CreatedAt.After(first[19].CreatedAt) || !first[19].CreatedAt.After(second[0].CreatedAt) {
		t.Fatal("pages out of order")
	}
	names, err := repo.ListErrorLogAppNames(ctx)
	if err != nil || len(names) != 3 || names[0] != "api" || names[1] != "billing" || names[2] != "worker" {
		t.Fatalf("application names %v %v", names, err)
	}
}

// The bulkhead's deadline (ADR-0017) relies on the driver cancelling a query
// when its context expires, instead of waiting for PostgreSQL to finish.
func TestAnExpiredContextCancelsTheQuery(t *testing.T) {
	db := isolatedDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := db.ExecContext(ctx, "SELECT pg_sleep(10)")
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("query ran for %v (err %v); the deadline must cancel it", time.Since(start), err)
	}
	if !errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		t.Fatalf("unexpected error %v", err)
	}
}
