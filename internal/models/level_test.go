package models_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	databasesql "monitor-central/deployments/database"
	"monitor-central/internal/models"
)

// The CHECK on error_logs.level accepts exactly models.Levels (ADR-0015).
func TestLevelsMatchTheSchema(t *testing.T) {
	m := regexp.MustCompile(`level TEXT NOT NULL CHECK\(level IN \(([^)]*)\)\)`).FindStringSubmatch(databasesql.InitSQL)
	if m == nil {
		t.Fatal("level CHECK not found in init.sql")
	}
	var checked []string
	for _, value := range strings.Split(m[1], ",") {
		checked = append(checked, strings.Trim(strings.TrimSpace(value), "'"))
	}
	if got, want := slices.Sorted(slices.Values(models.Levels)), slices.Sorted(slices.Values(checked)); !slices.Equal(got, want) {
		t.Fatalf("Go %v, init.sql %v", got, want)
	}
}

func TestSeverity(t *testing.T) {
	if models.Severity(models.LevelInfo) != 1 || models.Severity(models.LevelPanic) != 5 || models.Severity("DEBUG") != 0 || models.ValidLevel("error") {
		t.Fatal("severity scale")
	}
	if got := models.LevelsMostSevereFirst(); got[0] != models.LevelPanic || got[len(got)-1] != models.LevelInfo {
		t.Fatalf("order %v", got)
	}
}
