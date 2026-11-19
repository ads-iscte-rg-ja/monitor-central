//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"monitor-central/internal/models"
	"monitor-central/internal/repositories"
	"monitor-central/internal/services"
)

func TestStatisticsBatchDeduplicatesEverySourcePerHost(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	a, b := host(t, db, "batch-first"), host(t, db, "batch-second")
	s := services.NewIngestionService(repositories.NewMonitoringRepository(db, testHasher(t)))
	request := &models.MachineStatsBatchRequest{}
	for id := int64(100); id > 0; id-- {
		request.Stats = append(request.Stats, models.MachineStatsRequest{ID: id, CPUPercent: 25, RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2})
	}
	for _, h := range []*models.Host{a, b} {
		var original []models.StatAcknowledgement
		for attempt := 0; attempt < 2; attempt++ {
			ack, err := s.IngestStatsBatch(ctx, h.ID, request)
			if err != nil || len(ack) != 100 {
				t.Fatal("batch was not accepted", err)
			}
			for i, entry := range ack {
				if entry.ID <= 0 || entry.HostID != h.ID || entry.SourceID != int64(100-i) {
					t.Fatal("batch identity changed", entry)
				}
				if attempt == 1 && entry.ID != original[i].ID {
					t.Fatal("replay allocated another central ID")
				}
			}
			original = ack
		}
	}
	if count(t, db, "SELECT COUNT(*) FROM machine_stats") != 200 || count(t, db, "SELECT COUNT(*) FROM telemetry_receipts WHERE kind='stat'") != 200 {
		t.Fatal("batch replay duplicated data or source IDs collided across hosts")
	}
}

func TestStatisticsBatchPayloadAndReceiptsRollBackTogether(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	h := host(t, db, "batch-rollback")
	if _, err := db.ExecContext(context.Background(), `ALTER TABLE machine_stats ADD CONSTRAINT injected_batch_failure CHECK (source_id<>1)`); err != nil {
		t.Fatal(err)
	}
	s := services.NewIngestionService(repositories.NewMonitoringRepository(db, testHasher(t)))
	request := &models.MachineStatsBatchRequest{Stats: []models.MachineStatsRequest{
		{ID: 2, CPUPercent: 25, RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2},
		{ID: 1, CPUPercent: 25, RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2},
	}}
	if _, err := s.IngestStatsBatch(context.Background(), h.ID, request); err == nil {
		t.Fatal("expected storage failure")
	}
	if count(t, db, "SELECT COUNT(*) FROM machine_stats") != 0 || count(t, db, "SELECT COUNT(*) FROM telemetry_receipts") != 0 || count(t, db, "SELECT COUNT(*) FROM hosts WHERE last_seen_at IS NOT NULL") != 0 {
		t.Fatal("failed batch partially persisted payload, receipt, or heartbeat")
	}
}

func TestErrorBatchMixesNewAndResentRecordsInOrder(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	h := host(t, db, "batch-mixed")
	repo := repositories.NewMonitoringRepository(db, testHasher(t))
	stack := "goroutine 1"
	entry := func(source int64, withExtras bool) *models.ErrorLog {
		e := &models.ErrorLog{SourceID: &source, HostID: h.ID, AppName: "app", Environment: "test", Level: "ERROR", Message: "boom", CreatedAt: time.Now().UTC().Add(-time.Minute)}
		if withExtras {
			e.StackTrace, e.Metadata = &stack, []byte(`{"rota":"/x"}`)
		}
		return e
	}

	first := []*models.ErrorLog{entry(1, true), entry(2, false), entry(3, false)}
	if inserted, err := repo.CreateErrorLogs(ctx, h.ID, first); err != nil || inserted != 3 {
		t.Fatalf("first batch: inserted=%d err=%v", inserted, err)
	}
	// 2 and 3 were already received; 4 and 5 are new.
	if inserted, err := repo.CreateErrorLogs(ctx, h.ID, []*models.ErrorLog{entry(5, false), entry(2, false), entry(4, true), entry(3, false)}); err != nil || inserted != 2 {
		t.Fatalf("mixed batch: inserted=%d err=%v", inserted, err)
	}
	if count(t, db, "SELECT COUNT(*) FROM error_logs WHERE host_id=$1", h.ID) != 5 ||
		count(t, db, "SELECT COUNT(*) FROM telemetry_receipts WHERE host_id=$1 AND kind='error'", h.ID) != 5 {
		t.Fatal("re-sent records were stored twice")
	}
	if count(t, db, "SELECT COUNT(*) FROM error_logs WHERE source_id IN (1,4) AND stack_trace='goroutine 1' AND metadata->>'rota'='/x'") != 2 ||
		count(t, db, "SELECT COUNT(*) FROM error_logs WHERE source_id IN (2,3,5) AND stack_trace IS NULL AND metadata IS NULL") != 3 {
		t.Fatal("optional fields were not stored as given")
	}
}

func TestStatisticsAcknowledgementsKeepTheBatchOrder(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	h := host(t, db, "batch-order")
	repo := repositories.NewMonitoringRepository(db, testHasher(t))
	stat := func(source int64) *models.MachineStat {
		return &models.MachineStat{SourceID: &source, HostID: h.ID, CPUPercent: 25, RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2, CollectedAt: time.Now().UTC().Add(-time.Minute)}
	}
	original := []*models.MachineStat{stat(7), stat(8)}
	if err := repo.CreateMachineStats(ctx, h.ID, original); err != nil {
		t.Fatal(err)
	}
	resent := []*models.MachineStat{stat(9), stat(8), stat(7)}
	if err := repo.CreateMachineStats(ctx, h.ID, resent); err != nil {
		t.Fatal(err)
	}
	if resent[1].ID != original[1].ID || resent[2].ID != original[0].ID || resent[0].ID <= 0 || resent[0].ID == original[0].ID {
		t.Fatalf("IDs not matched to their records: original %d,%d resent %d,%d,%d",
			original[0].ID, original[1].ID, resent[0].ID, resent[1].ID, resent[2].ID)
	}
}

func TestAlertEventsAreResolvedTogether(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	a, b := host(t, db, "resolve-a"), host(t, db, "resolve-b")
	var rule int64
	if err := db.QueryRowContext(ctx, `INSERT INTO alerts(name,metric,condition,threshold) VALUES('cpu','cpu','gt',80) RETURNING id`).Scan(&rule); err != nil {
		t.Fatal(err)
	}
	for _, h := range []*models.Host{a, b} {
		if _, err := db.ExecContext(ctx, `INSERT INTO alert_events(alert_id,host_id,triggered_value) VALUES($1,$2,95)`, rule, h.ID); err != nil {
			t.Fatal(err)
		}
	}
	repo := repositories.NewAlertsRepository(db)
	if err := repo.ResolveOpenAlertEvents(ctx, []models.AlertTarget{{AlertID: rule, HostID: a.ID}}); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "SELECT COUNT(*) FROM alert_events WHERE host_id=$1 AND status='resolved' AND resolved_at IS NOT NULL", a.ID) != 1 ||
		count(t, db, "SELECT COUNT(*) FROM alert_events WHERE host_id=$1 AND status='fired'", b.ID) != 1 {
		t.Fatal("only the listed (alert, host) pairs may be resolved")
	}
	if err := repo.ResolveOpenAlertEvents(ctx, nil); err != nil {
		t.Fatal("an empty list must be a no-op", err)
	}
}
