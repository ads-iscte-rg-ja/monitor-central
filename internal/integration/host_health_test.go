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

func TestHealthEndpointOverridesTelemetryContact(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	hosts := repositories.NewHostsRepository(db, testHasher(t))
	h, err := services.NewHostsService(hosts).CreateHost(ctx, models.CreateHostRequest{Name: "endpoint", Hostname: "app.local", HealthEndpoint: "https://app.example/health"})
	if err != nil {
		t.Fatal(err)
	}
	target := models.HostHealthTarget{ID: h.ID, HealthEndpoint: h.HealthEndpoint}
	if err := hosts.RecordHealthCheck(ctx, target, time.Now().Add(-time.Second), false); err != nil {
		t.Fatal(err)
	}
	ingest := services.NewIngestionService(repositories.NewMonitoringRepository(db, testHasher(t)))
	if _, err := ingest.IngestStats(ctx, h.ID, &models.MachineStatsRequest{ID: 1, CPUPercent: 25, RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2}); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "SELECT COUNT(*) FROM hosts WHERE status='offline' AND last_seen_at IS NOT NULL") != 1 {
		t.Fatal("telemetry receipt overrode a failed health endpoint")
	}
	if _, err := db.Exec("UPDATE hosts SET unavailable_since=NOW()-INTERVAL '6 minutes'"); err != nil {
		t.Fatal(err)
	}
	alerts := repositories.NewAlertsRepository(db)
	if err := alerts.EnsureOfflineAlertRules(ctx, 5); err != nil {
		t.Fatal(err)
	}
	evaluations, err := alerts.ListMetricEvaluations(ctx)
	if err != nil || len(evaluations) != 1 || evaluations[0].Metric != "host_offline" || evaluations[0].CurrentValue < 5 {
		t.Fatal("offline alert ignored endpoint failure", evaluations, err)
	}
	if err := hosts.RecordHealthCheck(ctx, target, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE hosts SET last_seen_at=NOW()-INTERVAL '1 day'"); err != nil {
		t.Fatal(err)
	}
	if err := alerts.MarkHostsOffline(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "SELECT COUNT(*) FROM hosts WHERE status='online' AND unavailable_since IS NULL") != 1 {
		t.Fatal("healthy endpoint depended on measurement cadence")
	}
	if _, err := db.Exec("UPDATE hosts SET health_checked_at=NOW()-INTERVAL '2 minutes'"); err != nil {
		t.Fatal(err)
	}
	if err := alerts.MarkHostsOffline(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "SELECT COUNT(*) FROM hosts WHERE status='unknown'") != 1 {
		t.Fatal("stalled central checks left stale health online")
	}
}

func TestPassiveHealthUsesPerHostTimeout(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	hosts := services.NewHostsService(repositories.NewHostsRepository(db, testHasher(t)))
	if _, err := hosts.CreateHost(ctx, models.CreateHostRequest{Name: "hourly", Hostname: "hourly.local", ContactTimeoutMinutes: 120}); err != nil {
		t.Fatal(err)
	}
	if _, err := hosts.CreateHost(ctx, models.CreateHostRequest{Name: "frequent", Hostname: "frequent.local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE hosts SET last_seen_at=NOW()-INTERVAL '1 hour'"); err != nil {
		t.Fatal(err)
	}
	alerts := repositories.NewAlertsRepository(db)
	if err := alerts.EnsureOfflineAlertRules(ctx, 5); err != nil {
		t.Fatal(err)
	}
	if err := alerts.MarkHostsOffline(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "SELECT COUNT(*) FROM hosts WHERE name='hourly' AND status='online'") != 1 || count(t, db, "SELECT COUNT(*) FROM hosts WHERE name='frequent' AND status='offline'") != 1 {
		t.Fatal("host contact timeout was ignored")
	}
	if count(t, db, "SELECT COUNT(*) FROM alerts a JOIN hosts h ON h.id=a.host_id WHERE h.name='hourly' AND a.threshold=120") != 1 {
		t.Fatal("automatic offline alert kept a fixed five-minute timeout")
	}
}

func TestEditedHealthEndpointRejectsInFlightOldResult(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	repo := repositories.NewHostsRepository(db, testHasher(t))
	s := services.NewHostsService(repo)
	h, err := s.CreateHost(ctx, models.CreateHostRequest{Name: "app", Hostname: "app.local", HealthEndpoint: "https://old.example/health"})
	if err != nil {
		t.Fatal(err)
	}
	old := models.HostHealthTarget{ID: h.ID, HealthEndpoint: h.HealthEndpoint}
	if err := s.UpdateHost(ctx, h.ID, models.UpdateHostRequest{Name: "app", Hostname: "app.local", HealthEndpoint: "https://new.example/health"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordHealthCheck(ctx, old, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "SELECT COUNT(*) FROM hosts WHERE status='unknown' AND health_checked_at IS NULL") != 1 {
		t.Fatal("old endpoint result replaced newly configured health")
	}
}

func TestVersionTwoHealthUpgradePreservesTelemetryAndKeys(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	h := host(t, db, "upgrade-health")
	s := services.NewIngestionService(repositories.NewMonitoringRepository(db, testHasher(t)))
	if _, err := s.IngestStats(ctx, h.ID, &models.MachineStatsRequest{ID: 1, CPUPercent: 25, RAMUsedMB: 1, RAMTotalMB: 2, DiskUsedGB: 1, DiskTotalGB: 2}); err != nil {
		t.Fatal(err)
	}
	// Recreate the version-2 shape only inside this isolated test schema.
	if _, err := db.Exec(`ALTER TABLE hosts DROP COLUMN health_endpoint,DROP COLUMN contact_timeout_seconds,DROP COLUMN health_checked_at,DROP COLUMN health_check_ok,DROP COLUMN unavailable_since;
 DELETE FROM schema_migrations WHERE version=3`); err != nil {
		t.Fatal(err)
	}
	migrate(t, db)
	migrate(t, db)
	if _, err := s.AuthenticateHost(ctx, h.APIKey); err != nil {
		t.Fatal("upgrade changed host credentials", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM hosts WHERE health_endpoint='' AND contact_timeout_seconds=300") != 1 || count(t, db, "SELECT COUNT(*) FROM machine_stats") != 1 || count(t, db, "SELECT COUNT(*) FROM telemetry_receipts") != 1 {
		t.Fatal("upgrade did not preserve host or telemetry state")
	}
}
