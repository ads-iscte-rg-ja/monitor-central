package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"monitor-central/internal/models"
	"strings"
	"time"
)

const (
	MaxErrorBatch = 100
	MaxStatsBatch = 100
)

type IngestionRepository interface {
	GetHostByAPIKey(context.Context, string) (*models.Host, error)
	CreateMachineStats(context.Context, int64, []*models.MachineStat) error
	CreateErrorLogs(context.Context, int64, []*models.ErrorLog) (int, error)
}
type IngestionService struct{ repo IngestionRepository }

func NewIngestionService(repo IngestionRepository) *IngestionService {
	return &IngestionService{repo: repo}
}

// AuthenticateHost is called by route middleware before the request body is read.
func (s *IngestionService) AuthenticateHost(ctx context.Context, key string) (*models.Host, error) {
	if strings.TrimSpace(key) == "" || len(key) > 256 {
		return nil, models.ErrInvalidAPIKey
	}
	host, err := s.repo.GetHostByAPIKey(ctx, key)
	if errors.Is(err, models.ErrHostNotFound) {
		return nil, models.ErrInvalidAPIKey
	}
	return host, err
}

func (s *IngestionService) IngestStats(ctx context.Context, hostID int64, request *models.MachineStatsRequest) (*models.MachineStat, error) {
	stat, err := prepareMachineStat(hostID, request, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateMachineStats(ctx, hostID, []*models.MachineStat{stat}); err != nil {
		return nil, fmt.Errorf("save statistics: %w", err)
	}
	return stat, nil
}

func (s *IngestionService) IngestStatsBatch(ctx context.Context, hostID int64, request *models.MachineStatsBatchRequest) ([]models.StatAcknowledgement, error) {
	if hostID <= 0 {
		return nil, models.ErrInvalidAPIKey
	}
	if request == nil || len(request.Stats) == 0 {
		return nil, models.ErrEmptyStatsBatch
	}
	if len(request.Stats) > MaxStatsBatch {
		return nil, models.ErrStatsBatchTooLarge
	}
	now := time.Now().UTC()
	seen := make(map[int64]bool, len(request.Stats))
	stats := make([]*models.MachineStat, 0, len(request.Stats))
	for i := range request.Stats {
		input := &request.Stats[i]
		if input.ID <= 0 {
			return nil, models.ErrMissingStatID
		}
		if seen[input.ID] {
			return nil, models.ErrDuplicateSourceID
		}
		seen[input.ID] = true
		stat, err := prepareMachineStat(hostID, input, now)
		if err != nil {
			return nil, &models.RecordValidationError{SourceID: input.ID, Err: err}
		}
		stats = append(stats, stat)
	}
	if err := s.repo.CreateMachineStats(ctx, hostID, stats); err != nil {
		return nil, fmt.Errorf("save statistics batch: %w", err)
	}
	ack := make([]models.StatAcknowledgement, 0, len(stats))
	for _, stat := range stats {
		ack = append(ack, models.StatAcknowledgement{ID: stat.ID, HostID: hostID, SourceID: *stat.SourceID})
	}
	return ack, nil
}

func prepareMachineStat(hostID int64, request *models.MachineStatsRequest, now time.Time) (*models.MachineStat, error) {
	if hostID <= 0 {
		return nil, models.ErrInvalidAPIKey
	}
	if request == nil || !finite(request.CPUPercent) || request.CPUPercent < 0 || request.CPUPercent > 100 {
		return nil, models.ErrInvalidCPUPercent
	}
	if !finite(request.RAMUsedMB) || !finite(request.RAMTotalMB) || request.RAMUsedMB < 0 || request.RAMTotalMB <= 0 || request.RAMUsedMB > request.RAMTotalMB {
		return nil, models.ErrInvalidRAMUsage
	}
	if !finite(request.DiskUsedGB) || !finite(request.DiskTotalGB) || request.DiskUsedGB < 0 || request.DiskTotalGB <= 0 || request.DiskUsedGB > request.DiskTotalGB {
		return nil, models.ErrInvalidDiskUsage
	}
	collected := request.CollectedAt
	if collected.IsZero() {
		collected = now
	}
	if collected.After(now) {
		return nil, models.ErrFutureTimestamp
	}
	if request.ID < 0 {
		return nil, models.ErrMissingStatID
	}
	stat := &models.MachineStat{HostID: hostID, CPUPercent: request.CPUPercent, RAMUsedMB: request.RAMUsedMB, RAMTotalMB: request.RAMTotalMB, DiskUsedGB: request.DiskUsedGB, DiskTotalGB: request.DiskTotalGB, CollectedAt: collected}
	if request.ID > 0 {
		source := request.ID
		stat.SourceID = &source
	}
	return stat, nil
}

func (s *IngestionService) IngestErrors(ctx context.Context, hostID int64, request *models.ErrorLogsRequest) (int, int, error) {
	if hostID <= 0 {
		return 0, 0, models.ErrInvalidAPIKey
	}
	if request == nil || len(request.Errors) == 0 {
		return 0, 0, models.ErrEmptyErrorBatch
	}
	if len(request.Errors) > MaxErrorBatch {
		return 0, 0, models.ErrBatchTooLarge
	}
	now := time.Now().UTC()
	logs := make([]*models.ErrorLog, 0, len(request.Errors))
	seen := make(map[int64]bool, len(request.Errors))
	for _, input := range request.Errors {
		if input.ID <= 0 {
			return 0, 0, models.ErrMissingErrorID
		}
		if seen[input.ID] {
			return 0, 0, models.ErrDuplicateSourceID
		}
		seen[input.ID] = true
		entry, err := prepareErrorLog(hostID, input, now)
		if err != nil {
			return 0, 0, &models.RecordValidationError{SourceID: input.ID, Err: err}
		}
		logs = append(logs, entry)
	}
	inserted, err := s.repo.CreateErrorLogs(ctx, hostID, logs)
	if err != nil {
		return 0, 0, fmt.Errorf("save error batch: %w", err)
	}
	return len(logs), inserted, nil
}

func prepareErrorLog(hostID int64, input models.ErrorLogInput, now time.Time) (*models.ErrorLog, error) {
	app, env := strings.TrimSpace(input.AppName), strings.TrimSpace(input.Environment)
	if app == "" {
		return nil, models.ErrMissingAppName
	}
	if env == "" {
		return nil, models.ErrMissingEnvironment
	}
	level := strings.ToUpper(strings.TrimSpace(input.Level))
	switch level {
	case "INFO", "WARN", "ERROR", "CRITICAL", "PANIC":
	default:
		return nil, models.ErrInvalidErrorLevel
	}
	if strings.TrimSpace(input.Message) == "" {
		return nil, models.ErrMissingErrorMessage
	}
	if len(app) > 200 || len(env) > 100 || len(input.Message) > 16384 || (input.StackTrace != nil && len(*input.StackTrace) > 65536) || len(input.Metadata) > 32768 {
		return nil, models.ErrFieldTooLarge
	}
	if len(input.Metadata) > 0 && !json.Valid(input.Metadata) {
		return nil, models.ErrInvalidMetadata
	}
	created := input.CreatedAt
	if created.IsZero() {
		created = now
	}
	if created.After(now) {
		return nil, models.ErrFutureTimestamp
	}
	source := input.ID
	return &models.ErrorLog{SourceID: &source, HostID: hostID, AppName: app, Environment: env, Level: level, Message: input.Message, StackTrace: input.StackTrace, Metadata: input.Metadata, CreatedAt: created}, nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
