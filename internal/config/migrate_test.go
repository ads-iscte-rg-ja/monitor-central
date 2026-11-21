package config

import (
	"testing"
	"time"
)

func TestLoadMigrateTimeout(t *testing.T) {
	for value, want := range map[string]time.Duration{"": DefaultMigrateTimeout, "1m": time.Minute, "2h": 2 * time.Hour} {
		t.Setenv("MIGRATE_TIMEOUT", value)
		if d, err := LoadMigrateTimeout(); err != nil || d != want {
			t.Fatalf("%q: %v %v", value, d, err)
		}
	}
	for _, value := range []string{"30s", "25h", "later"} {
		t.Setenv("MIGRATE_TIMEOUT", value)
		if _, err := LoadMigrateTimeout(); err == nil {
			t.Fatalf("%q accepted", value)
		}
	}
}
