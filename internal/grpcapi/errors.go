package grpcapi

import (
	"errors"

	ingestv1 "github.com/ads-iscte-rg-ja/monitor-api/gen/go/monitor/ingest/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"monitor-central/internal/models"
)

// rejectionReasons maps each record-level validation error to its stable
// contract code. The client decides only by this code, never by message text.
var rejectionReasons = []struct {
	err    error
	reason ingestv1.RejectionReason
}{
	{models.ErrFutureTimestamp, ingestv1.RejectionReason_REJECTION_REASON_FUTURE_TIMESTAMP},
	{models.ErrFieldTooLarge, ingestv1.RejectionReason_REJECTION_REASON_FIELD_TOO_LARGE},
	{models.ErrInvalidCPUPercent, ingestv1.RejectionReason_REJECTION_REASON_INVALID_CPU_PERCENT},
	{models.ErrInvalidRAMUsage, ingestv1.RejectionReason_REJECTION_REASON_INVALID_RAM_USAGE},
	{models.ErrInvalidDiskUsage, ingestv1.RejectionReason_REJECTION_REASON_INVALID_DISK_USAGE},
	{models.ErrMissingAppName, ingestv1.RejectionReason_REJECTION_REASON_MISSING_APP_NAME},
	{models.ErrMissingEnvironment, ingestv1.RejectionReason_REJECTION_REASON_MISSING_ENVIRONMENT},
	{models.ErrInvalidErrorLevel, ingestv1.RejectionReason_REJECTION_REASON_INVALID_LOG_LEVEL},
	{models.ErrMissingErrorMessage, ingestv1.RejectionReason_REJECTION_REASON_MISSING_MESSAGE},
	{models.ErrInvalidMetadata, ingestv1.RejectionReason_REJECTION_REASON_INVALID_METADATA},
}

// batchErrors make the whole request malformed (a contract violation by the client).
var batchErrors = []error{
	models.ErrEmptyStatsBatch, models.ErrStatsBatchTooLarge, models.ErrMissingStatID,
	models.ErrEmptyErrorBatch, models.ErrBatchTooLarge, models.ErrMissingErrorID,
	models.ErrDuplicateSourceID,
}

// toStatus translates a service error into the gRPC status defined by the contract.
func (s *IngestServer) toStatus(err error) error {
	var record *models.RecordValidationError
	if errors.As(err, &record) {
		reason := ingestv1.RejectionReason_REJECTION_REASON_UNSPECIFIED
		for _, candidate := range rejectionReasons {
			if errors.Is(err, candidate.err) {
				reason = candidate.reason
				break
			}
		}
		st, detailErr := status.New(codes.InvalidArgument, "invalid record").
			WithDetails(&ingestv1.RecordRejection{SourceId: record.SourceID, Reason: reason})
		if detailErr != nil {
			s.logger.Printf("grpc ingestion: attach rejection detail: %v", detailErr)
			return status.Error(codes.Internal, "unable to describe the rejected record")
		}
		return st.Err()
	}
	for _, expected := range batchErrors {
		if errors.Is(err, expected) {
			return status.Error(codes.InvalidArgument, expected.Error())
		}
	}
	if errors.Is(err, models.ErrInvalidAPIKey) {
		return status.Error(codes.Unauthenticated, "host authentication required")
	}
	// Never echo internal details (SQL, connection strings) to the agent.
	s.logger.Printf("grpc ingestion: %v", err)
	return status.Error(codes.Internal, "unable to save monitoring data")
}
