package config

import (
	"strings"
	"testing"
	"time"
)

func TestSecretAndRetentionValidation(t *testing.T) {
	for _, value := range []string{"", "short", strings.Repeat("z", 64)} {
		t.Setenv("HOST_API_KEY_SECRET", value)
		if _, err := LoadAPIKeySecret(); err == nil {
			t.Fatal("invalid secret accepted")
		}
	}
	t.Setenv("HOST_API_KEY_SECRET", strings.Repeat("ab", 32))
	if _, err := LoadAPIKeySecret(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"RETENTION_STATS_DAYS", "RETENTION_ERRORS_DAYS", "RETENTION_RESOLVED_EVENTS_DAYS", "RETENTION_SENT_EMAIL_DAYS", "RETENTION_AUDIT_DAYS"} {
		t.Setenv(name, "0")
	}
	t.Setenv("CLEANUP_INTERVAL", "1h")
	t.Setenv("CLEANUP_BATCH_SIZE", "1000")
	if _, err := LoadRetentionConfig(); err != nil {
		t.Fatal(err)
	}
	// Unset, retention is on with the measured defaults (ADR-0017).
	for _, name := range []string{"RETENTION_STATS_DAYS", "RETENTION_ERRORS_DAYS", "RETENTION_RESOLVED_EVENTS_DAYS", "RETENTION_SENT_EMAIL_DAYS", "RETENTION_AUDIT_DAYS", "CLEANUP_INTERVAL", "CLEANUP_BATCH_SIZE"} {
		t.Setenv(name, "")
	}
	if c, err := LoadRetentionConfig(); err != nil || c.StatsDays != 30 || c.ErrorDays != 30 || c.ResolvedEventDays != 180 ||
		c.SentEmailDays != 30 || c.AuditDays != 365 || c.Interval != time.Minute || c.BatchSize != 10000 {
		t.Fatalf("retention defaults: %+v %v", c, err)
	}
	t.Setenv("RETENTION_ERRORS_DAYS", "-1")
	if _, err := LoadRetentionConfig(); err == nil {
		t.Fatal("negative retention accepted")
	}
	t.Setenv("RETENTION_ERRORS_DAYS", "bad")
	if _, err := LoadRetentionConfig(); err == nil {
		t.Fatal("malformed retention accepted")
	}
}
