package models

// Log levels, the only list in the central (ADR-0015). The CHECK in init.sql, the
// alert SQL and the contract mapping are compared with it by tests.
const (
	LevelInfo     = "INFO"
	LevelWarn     = "WARN"
	LevelError    = "ERROR"
	LevelCritical = "CRITICAL"
	LevelPanic    = "PANIC"
)

// Levels lists every level, from the least to the most severe.
var Levels = []string{LevelInfo, LevelWarn, LevelError, LevelCritical, LevelPanic}

// Severity is 1 (INFO) to 5 (PANIC), the scale of the error_level alert metric;
// 0 for anything else.
func Severity(level string) int {
	for i, known := range Levels {
		if level == known {
			return i + 1
		}
	}
	return 0
}

// ValidLevel reports whether level is one of Levels (exact, upper case).
func ValidLevel(level string) bool { return Severity(level) > 0 }

// LevelsMostSevereFirst is Levels in reverse, the order of the error filters.
func LevelsMostSevereFirst() []string {
	out := make([]string, len(Levels))
	for i, level := range Levels {
		out[len(Levels)-1-i] = level
	}
	return out
}
