package services

import (
	"fmt"
	"time"

	"monitor-central/internal/models"
)

// bufferDropKinds are the drop counter keys of the agent (ADR-0009).
var bufferDropKinds = map[string]bool{
	"INFO": true, "WARN": true, "ERROR": true, "CRITICAL": true, "PANIC": true, "machine_stat": true,
}

// ValidateBufferStatus checks an agent's buffer snapshot (ADR-0010). An invalid
// snapshot is ignored by ingestion, never a reason to reject statistics.
func ValidateBufferStatus(status *models.BufferStatus, now time.Time) error {
	switch {
	case status.ObservedAt.IsZero():
		return fmt.Errorf("%w: observation time is missing", models.ErrInvalidBufferStatus)
	case status.ObservedAt.After(now):
		return fmt.Errorf("%w: observation time is in the future", models.ErrInvalidBufferStatus)
	case status.Pending < 0 || status.Quarantined < 0 || status.OldestPendingAgeSeconds < 0 || status.SizeBytes < 0:
		return fmt.Errorf("%w: negative count", models.ErrInvalidBufferStatus)
	}
	for kind, dropped := range status.DroppedByKind {
		if !bufferDropKinds[kind] || dropped < 0 {
			return fmt.Errorf("%w: invalid drop counter", models.ErrInvalidBufferStatus)
		}
	}
	return nil
}
