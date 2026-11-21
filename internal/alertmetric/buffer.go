package alertmetric

import (
	"fmt"
	"math"
)

// bufferQuarantinedQuery is the number of records in quarantine in the host's
// buffer, from its agent's last report (ADR-0010). Only reports received in the
// last 5 minutes count: a silent agent is host_offline's business.
const bufferQuarantinedQuery = rulesSelect + `hbs.quarantined` + rulesFrom + `
 JOIN host_buffer_status hbs ON hbs.host_id = h.id` + enabledRulesOf + `'buffer_quarantined'
 AND hbs.received_at >= CURRENT_TIMESTAMP - INTERVAL '5 minutes'`

func init() { register(bufferQuarantined{}) }

// bufferQuarantined is how many records the host's agent holds in quarantine:
// data that will never reach central without someone's intervention. It
// matches at or above the threshold, so "any quarantine" is a threshold of 1.
type bufferQuarantined struct{}

func (bufferQuarantined) Name() string { return "buffer_quarantined" }
func (bufferQuarantined) Help() string {
	return "buffer_quarantined: records in quarantine on the host (matches at or above)"
}
func (bufferQuarantined) ValidThreshold(threshold float64) bool {
	return math.Trunc(threshold) == threshold
}
func (bufferQuarantined) Breached(_ string, value, threshold float64) bool { return value >= threshold }
func (bufferQuarantined) Format(value float64) string {
	return fmt.Sprintf("%.0f quarantined", value)
}
func (b bufferQuarantined) FormatThreshold(_ string, threshold float64) string {
	return "≥ " + b.Format(threshold)
}
func (bufferQuarantined) Query() (string, []any) { return bufferQuarantinedQuery, nil }
