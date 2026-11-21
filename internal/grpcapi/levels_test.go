package grpcapi

import (
	"testing"

	ingestv1 "github.com/ads-iscte-rg-ja/monitor-api/gen/go/monitor/ingest/v1"

	"monitor-central/internal/models"
)

// Every contract level maps to one of models.Levels, and every level is reachable
// (ADR-0015); UNSPECIFIED and unknown values map to "" (rejected).
func TestContractLevelsMatchTheModel(t *testing.T) {
	seen := map[string]bool{}
	for value := range ingestv1.LogLevel_name {
		l := ingestv1.LogLevel(value)
		got := level(l)
		if l == ingestv1.LogLevel_LOG_LEVEL_UNSPECIFIED {
			if got != "" {
				t.Fatalf("UNSPECIFIED maps to %q", got)
			}
			continue
		}
		if !models.ValidLevel(got) || seen[got] {
			t.Fatalf("%s maps to %q", l, got)
		}
		seen[got] = true
	}
	if len(seen) != len(models.Levels) || level(ingestv1.LogLevel(99)) != "" {
		t.Fatalf("%d of %d levels reachable from the contract", len(seen), len(models.Levels))
	}
}
