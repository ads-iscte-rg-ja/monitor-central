package alertmetric

import (
	"fmt"
	"math"

	"monitor-central/internal/models"
)

const (
	// errorRateQuery counts ERROR, CRITICAL and PANIC logs in the last minute.
	errorRateQuery = rulesSelect + `SELECT COUNT(*) FROM error_logs el WHERE el.host_id = h.id
  AND el.level IN ('ERROR','CRITICAL','PANIC')
  AND el.created_at <= CURRENT_TIMESTAMP AND el.created_at >= CURRENT_TIMESTAMP - INTERVAL '1 minute'` +
		rulesFrom + enabledRulesOf + `'error_rate'`
	// errorLevelQuery is the highest level logged in the last 5 minutes (1 INFO … 5 PANIC, 0 if none).
	errorLevelQuery = rulesSelect + `SELECT COALESCE(MAX(CASE el.level WHEN 'INFO' THEN 1 WHEN 'WARN' THEN 2
  WHEN 'ERROR' THEN 3 WHEN 'CRITICAL' THEN 4 WHEN 'PANIC' THEN 5 ELSE 0 END), 0)
  FROM error_logs el WHERE el.host_id = h.id
  AND el.created_at <= CURRENT_TIMESTAMP AND el.created_at >= CURRENT_TIMESTAMP - INTERVAL '5 minutes'` +
		rulesFrom + enabledRulesOf + `'error_level'`
)

func init() {
	register(errorRate{})
	register(errorLevel{})
}

// errorRate is the number of errors per minute.
type errorRate struct{}

func (errorRate) Name() string                { return "error_rate" }
func (errorRate) Help() string                { return "error_rate: errors per minute" }
func (errorRate) ValidThreshold(float64) bool { return true }
func (errorRate) Format(value float64) string { return fmt.Sprintf("%.0f/min", value) }
func (errorRate) Query() (string, []any)      { return errorRateQuery, nil }
func (errorRate) Breached(condition string, value, threshold float64) bool {
	return compare(condition, value, threshold)
}
func (e errorRate) FormatThreshold(condition string, threshold float64) string {
	return operator(condition) + " " + e.Format(threshold)
}

// errorLevel is the highest severity logged recently. It matches at or above
// the threshold, whatever the rule's condition: "alert on ERROR" means ERROR,
// CRITICAL or PANIC.
type errorLevel struct{}

func (errorLevel) Name() string { return "error_level" }
func (errorLevel) Help() string {
	return "error_level: 1 INFO, 2 WARN, 3 ERROR, 4 CRITICAL, 5 PANIC (matches at or above)"
}
func (errorLevel) ValidThreshold(threshold float64) bool {
	return threshold <= float64(len(models.Levels)) && math.Trunc(threshold) == threshold
}
func (errorLevel) Breached(_ string, value, threshold float64) bool { return value >= threshold }
func (errorLevel) Format(value float64) string {
	if i := int(value); i >= 1 && i <= len(models.Levels) {
		return models.Levels[i-1]
	}
	return "UNKNOWN"
}
func (e errorLevel) FormatThreshold(_ string, threshold float64) string {
	return "≥ " + e.Format(threshold)
}
func (errorLevel) Query() (string, []any) { return errorLevelQuery, nil }
