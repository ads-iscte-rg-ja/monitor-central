//go:build integration

package integration_test

import (
	"context"
	"testing"

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
