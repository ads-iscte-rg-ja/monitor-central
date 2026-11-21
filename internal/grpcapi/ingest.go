// Package grpcapi is the gRPC transport layer for telemetry ingestion
// (contract: github.com/ads-iscte-rg-ja/monitor-api, ADR-0003). Like the HTTP
// handlers, it only translates between the wire contract and the services; it
// never uses repositories or the database directly.
package grpcapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"strings"
	"time"

	ingestv1 "github.com/ads-iscte-rg-ja/monitor-api/gen/go/monitor/ingest/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
	"google.golang.org/protobuf/types/known/timestamppb"

	"monitor-central/internal/models"
	"monitor-central/internal/observability"
	"monitor-central/internal/services"
)

// IngestServer implements ingestv1.IngestServiceServer on top of the ingestion service.
type IngestServer struct {
	ingestv1.UnimplementedIngestServiceServer
	service *services.IngestionService
	logger  *log.Logger
	metrics *observability.Metrics
}

// NewServer builds the gRPC server for ingestion: host authentication runs in an
// interceptor before any handler, and messages above the contract limit are
// rejected by gRPC itself with RESOURCE_EXHAUSTED. Interceptors, outermost
// first: metrics (so rejections are measured and logged too, ADR-0004), the
// admission limit (before authentication touches the database, ADR-0006), and
// authentication. metrics and slogger may be nil; maxInflight <= 0 means no limit.
func NewServer(service *services.IngestionService, logger *log.Logger, metrics *observability.Metrics, slogger *slog.Logger, maxInflight int) *grpc.Server {
	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(ingestv1.MaxMessageBytes),
		grpc.InTapHandle(requireKeyHeader),
		grpc.ChainUnaryInterceptor(metrics.GRPCInterceptor(slogger), admit(maxInflight, metrics), authenticateHost(service, logger)),
	)
	ingestv1.RegisterIngestServiceServer(server, &IngestServer{service: service, logger: logger, metrics: metrics})
	return server
}

type hostIDKey struct{}

// maxAPIKeyLength mirrors the bound enforced by IngestionService.AuthenticateHost.
const maxAPIKeyLength = 256

// requireKeyHeader runs on the transport, before the request message is read:
// a request without a plausible key is rejected without decoding its payload.
// It is deliberately cheap (no database access); the real check is
// authenticateHost.
func requireKeyHeader(ctx context.Context, info *tap.Info) (context.Context, error) {
	keys := info.Header.Get(ingestv1.APIKeyMetadata)
	if len(keys) != 1 || strings.TrimSpace(keys[0]) == "" || len(keys[0]) > maxAPIKeyLength {
		return nil, status.Error(codes.Unauthenticated, "invalid or missing API key")
	}
	return ctx, nil
}

// authenticateHost resolves the host from the x-api-key metadata. Clients never
// choose a host: the identity comes only from the key.
func authenticateHost(auth *services.IngestionService, logger *log.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		var key string
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if values := md.Get(ingestv1.APIKeyMetadata); len(values) == 1 {
				key = values[0]
			}
		}
		host, err := auth.AuthenticateHost(ctx, key)
		if err != nil || host == nil || host.ID <= 0 {
			if err != nil && !errors.Is(err, models.ErrInvalidAPIKey) {
				logger.Printf("grpc ingestion: host authentication unavailable: %v", err)
				return nil, status.Error(codes.Unavailable, "host authentication unavailable")
			}
			return nil, status.Error(codes.Unauthenticated, "invalid or missing API key")
		}
		observability.SetHostID(ctx, host.ID)
		return handler(context.WithValue(ctx, hostIDKey{}, host.ID), req)
	}
}

func hostID(ctx context.Context) (int64, error) {
	id, ok := ctx.Value(hostIDKey{}).(int64)
	if !ok || id <= 0 {
		return 0, status.Error(codes.Unauthenticated, "host authentication required")
	}
	return id, nil
}

// IngestStats stores a batch of machine statistics, all or nothing.
func (s *IngestServer) IngestStats(ctx context.Context, req *ingestv1.IngestStatsRequest) (*ingestv1.IngestStatsResponse, error) {
	host, err := hostID(ctx)
	if err != nil {
		return nil, err
	}
	batch := &models.MachineStatsBatchRequest{Stats: make([]models.MachineStatsRequest, 0, len(req.GetStats()))}
	for _, stat := range req.GetStats() {
		batch.Stats = append(batch.Stats, models.MachineStatsRequest{
			ID:          stat.GetSourceId(),
			CPUPercent:  stat.GetCpuPercent(),
			RAMUsedMB:   stat.GetRamUsedMb(),
			RAMTotalMB:  stat.GetRamTotalMb(),
			DiskUsedGB:  stat.GetDiskUsedGb(),
			DiskTotalGB: stat.GetDiskTotalGb(),
			CollectedAt: timestamp(stat.GetCollectedAt()),
		})
	}
	if buffer := req.GetBuffer(); buffer != nil {
		batch.Buffer = bufferStatus(buffer)
		if err := services.ValidateBufferStatus(batch.Buffer, time.Now()); err != nil {
			// Accessory data: log it and still accept the statistics (ADR-0010).
			s.logger.Printf("grpc ingestion: host %d sent a buffer status that was ignored: %v", host, err)
			batch.Buffer = nil
		}
	}
	acks, err := s.service.IngestStatsBatch(ctx, host, batch)
	if err != nil {
		s.recordRejection("stats", err)
		return nil, s.toStatus(err)
	}
	now := time.Now()
	lags := make([]time.Duration, 0, len(batch.Stats))
	for _, stat := range batch.Stats {
		if !stat.CollectedAt.IsZero() {
			lags = append(lags, now.Sub(stat.CollectedAt))
		}
	}
	// The stats repository is idempotent but does not report duplicates, so
	// every acknowledged record counts as inserted here.
	s.metrics.RecordIngest("stats", len(acks), 0, 0, lags)
	response := &ingestv1.IngestStatsResponse{Acknowledgements: make([]*ingestv1.StatAcknowledgement, 0, len(acks))}
	for _, ack := range acks {
		response.Acknowledgements = append(response.Acknowledgements, &ingestv1.StatAcknowledgement{Id: ack.ID, HostId: ack.HostID, SourceId: ack.SourceID})
	}
	return response, nil
}

// IngestErrors stores a batch of application errors, all or nothing.
func (s *IngestServer) IngestErrors(ctx context.Context, req *ingestv1.IngestErrorsRequest) (*ingestv1.IngestErrorsResponse, error) {
	host, err := hostID(ctx)
	if err != nil {
		return nil, err
	}
	batch := &models.ErrorLogsRequest{Errors: make([]models.ErrorLogInput, 0, len(req.GetErrors()))}
	for _, entry := range req.GetErrors() {
		input := models.ErrorLogInput{
			ID:          entry.GetSourceId(),
			AppName:     entry.GetAppName(),
			Environment: entry.GetEnvironment(),
			Level:       level(entry.GetLevel()),
			Message:     entry.GetMessage(),
			StackTrace:  entry.StackTrace,
			CreatedAt:   timestamp(entry.GetCreatedAt()),
		}
		if entry.GetMetadataJson() != "" {
			input.Metadata = json.RawMessage(entry.GetMetadataJson())
		}
		batch.Errors = append(batch.Errors, input)
	}
	accepted, inserted, err := s.service.IngestErrors(ctx, host, batch)
	if err != nil {
		s.recordRejection("errors", err)
		return nil, s.toStatus(err)
	}
	now := time.Now()
	lags := make([]time.Duration, 0, len(batch.Errors))
	for _, entry := range batch.Errors {
		if !entry.CreatedAt.IsZero() {
			lags = append(lags, now.Sub(entry.CreatedAt))
		}
	}
	s.metrics.RecordIngest("errors", inserted, accepted-inserted, 0, lags)
	ids := make([]int64, 0, len(batch.Errors))
	for _, entry := range batch.Errors {
		ids = append(ids, entry.ID)
	}
	return &ingestv1.IngestErrorsResponse{
		Accepted:   int32(accepted),            //nolint:gosec // G115: at most MaxBatchRecords (100)
		Inserted:   int32(inserted),            //nolint:gosec // G115: at most MaxBatchRecords (100)
		Duplicates: int32(accepted - inserted), //nolint:gosec // G115: at most MaxBatchRecords (100)
		SourceIds:  ids,
	}, nil
}

// bufferStatus translates the contract's buffer snapshot (monitor-api v1.1.0).
func bufferStatus(b *ingestv1.BufferStatus) *models.BufferStatus {
	return &models.BufferStatus{
		ObservedAt:              timestamp(b.GetObservedAt()),
		Pending:                 b.GetPending(),
		Quarantined:             b.GetQuarantined(),
		OldestPendingAgeSeconds: b.GetOldestPendingAgeSeconds(),
		DeliveryPaused:          b.GetDeliveryPaused(),
		SizeBytes:               b.GetSizeBytes(),
		DroppedByKind:           b.GetDroppedByKind(),
	}
}

// timestamp returns the zero time for an absent timestamp, which the service
// treats as "now", exactly like an omitted JSON field before.
func timestamp(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// level maps the contract enum to the stored level; UNSPECIFIED (or an unknown
// future value) becomes "" and is rejected by the service as an invalid level.
func level(l ingestv1.LogLevel) string {
	return contractLevels[l]
}

// contractLevels translates the contract to models.Levels (ADR-0015); a test
// checks that every level, and only those, has a translation.
var contractLevels = map[ingestv1.LogLevel]string{
	ingestv1.LogLevel_LOG_LEVEL_INFO:     models.LevelInfo,
	ingestv1.LogLevel_LOG_LEVEL_WARN:     models.LevelWarn,
	ingestv1.LogLevel_LOG_LEVEL_ERROR:    models.LevelError,
	ingestv1.LogLevel_LOG_LEVEL_CRITICAL: models.LevelCritical,
	ingestv1.LogLevel_LOG_LEVEL_PANIC:    models.LevelPanic,
}

// recordRejection counts a record-level rejection; batch-level and transport
// failures are already visible in the request metric by status code.
func (s *IngestServer) recordRejection(kind string, err error) {
	var record *models.RecordValidationError
	if errors.As(err, &record) {
		s.metrics.RecordIngest(kind, 0, 0, 1, nil)
	}
}
