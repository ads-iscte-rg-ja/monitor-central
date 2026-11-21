package alertmetric

import (
	"fmt"
	"strings"
	"testing"

	"monitor-central/internal/models"
)

// The SQL of the error metrics repeats the levels (the database computes them);
// it must agree with models.Levels and models.Severity (ADR-0015).
func TestErrorMetricsSQLMatchesTheLevels(t *testing.T) {
	for _, level := range models.Levels {
		if when := fmt.Sprintf("WHEN '%s' THEN %d", level, models.Severity(level)); !strings.Contains(errorLevelQuery, when) {
			t.Errorf("error_level SQL lacks %q", when)
		}
		inRate := strings.Contains(errorRateQuery, "'"+level+"'")
		if want := models.Severity(level) >= models.Severity(models.LevelError); inRate != want {
			t.Errorf("error_rate counts %s: %v, want %v", level, inRate, want)
		}
		if got := (errorLevel{}).Format(float64(models.Severity(level))); got != level {
			t.Errorf("Format(%d) = %s", models.Severity(level), got)
		}
	}
	if (errorLevel{}).Format(0) != "UNKNOWN" || (errorLevel{}).ValidThreshold(6) || !(errorLevel{}).ValidThreshold(5) {
		t.Error("error_level bounds")
	}
}
