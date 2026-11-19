//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"testing"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/database"
	"monitor-central/internal/models"
	"monitor-central/internal/repositories"
	"monitor-central/internal/services"
)

func lead(t *testing.T, e *database.Elector, job string) bool {
	t.Helper()
	ok, err := e.Lead(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// Advisory locks are database-wide, not per schema: unique names keep tests apart.
func jobName(t *testing.T) string { return fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano()) }

func TestEachJobHasOneLeaderAndAStandbyTakesOver(t *testing.T) {
	db := isolatedDB(t)
	first, second := database.NewElector(db), database.NewElector(db)
	t.Cleanup(func() { first.Close(); second.Close() })
	alerts, health := jobName(t)+"-alerts", jobName(t)+"-health"

	if !lead(t, first, alerts) || lead(t, second, alerts) {
		t.Fatal("exactly one instance must lead a job")
	}
	if !lead(t, second, health) {
		t.Fatal("another job must be free for the other instance")
	}
	if !lead(t, first, alerts) || lead(t, second, alerts) {
		t.Fatal("the leader must keep the job on the next cycle")
	}

	// Orderly shutdown: the standby takes over at its next attempt.
	first.Close()
	if !lead(t, second, alerts) {
		t.Fatal("the standby must take over after the leader closes")
	}

	// Crash: PostgreSQL ends the leader's session; the lock goes with it.
	var pid int
	if err := db.QueryRow(`SELECT pid FROM pg_locks WHERE locktype='advisory' AND classid=743819115 AND objid=hashtext($1)::oid AND granted`, alerts).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Lead(context.Background(), alerts); err == nil {
		t.Fatal("the instance that lost its session must notice it")
	}
	third := database.NewElector(db)
	t.Cleanup(third.Close)
	if !lead(t, third, alerts) || !lead(t, third, health) {
		t.Fatal("after the crash both jobs must be free for another instance")
	}
}

func TestLoginLimitIsSharedByEveryInstance(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	limits := config.LoginConfig{PerIP: 100, PerAccount: 2, MaxConcurrent: 4}
	instance := func() *services.AuthService {
		return services.NewAuthService(repositories.NewUserRepository(db), repositories.NewAuditRepository(db, log.New(io.Discard, "", 0)),
			log.New(io.Discard, "", 0), limits, repositories.NewLoginAttemptsRepository(db))
	}
	a, b := instance(), instance()
	attempt := func(s *services.AuthService) error {
		_, err := s.Login(ctx, &models.LoginRequest{Email: "victim@example.com", Password: "guess"}, "198.51.100.7")
		return err
	}
	if err := attempt(a); !errors.Is(err, models.ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if err := attempt(b); !errors.Is(err, models.ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if err := attempt(a); !errors.Is(err, services.ErrLoginRateLimited) {
		t.Fatalf("the third attempt must be limited whatever the instance, got %v", err)
	}
	if count(t, db, "SELECT COUNT(*) FROM login_attempts") != 2 {
		t.Fatal("one window per IP and per account, keyed by hash")
	}

	if _, err := db.ExecContext(context.Background(), "UPDATE login_attempts SET window_ends = CURRENT_TIMESTAMP - INTERVAL '1 second'"); err != nil {
		t.Fatal(err)
	}
	if err := attempt(b); !errors.Is(err, models.ErrInvalidCredentials) {
		t.Fatalf("a new window must start after the old one ends, got %v", err)
	}

	if _, err := db.ExecContext(context.Background(), "UPDATE login_attempts SET window_ends = CURRENT_TIMESTAMP - INTERVAL '1 minute'"); err != nil {
		t.Fatal(err)
	}
	counts, err := repositories.NewRetentionRepository(db).Cleanup(ctx, config.RetentionConfig{BatchSize: 100}, time.Now().UTC())
	if err != nil || counts["login_attempts"] != 2 || count(t, db, "SELECT COUNT(*) FROM login_attempts") != 0 {
		t.Fatalf("retention must delete ended windows: %v %v", counts, err)
	}
}

func TestScalingStoresReportTheirFailures(t *testing.T) {
	db := isolatedDB(t)
	migrate(t, db)
	ctx := context.Background()
	key := make([]byte, 32)
	if _, err := repositories.NewLoginAttemptsRepository(db).CountAttempts(ctx, [][]byte{key, key}, time.Minute); err == nil {
		t.Fatal("the same key twice in one statement must be rejected, not miscounted")
	}
	if _, err := repositories.NewRetentionRepository(db).Cleanup(ctx, config.RetentionConfig{StatsDays: -1, BatchSize: 10}, time.Now()); err == nil {
		t.Fatal("negative retention must be rejected")
	}
	elector := database.NewElector(db)
	_ = db.DB.Close()
	if _, err := elector.Lead(ctx, jobName(t)); err == nil {
		t.Fatal("an elector without a database must not claim to lead")
	}
	elector.Close()
}
