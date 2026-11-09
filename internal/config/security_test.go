package config

import (
	"strings"
	"testing"
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
	t.Setenv("RETENTION_ERRORS_DAYS", "-1")
	if _, err := LoadRetentionConfig(); err == nil {
		t.Fatal("negative retention accepted")
	}
	t.Setenv("RETENTION_ERRORS_DAYS", "bad")
	if _, err := LoadRetentionConfig(); err == nil {
		t.Fatal("malformed retention accepted")
	}
}
