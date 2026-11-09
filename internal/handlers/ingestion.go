package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"monitor-central/internal/config"
	"monitor-central/internal/models"
	"monitor-central/internal/services"
	"net/http"
)

type IngestionHandler struct{ service *services.IngestionService }

func NewIngestionHandler(service *services.IngestionService) *IngestionHandler {
	return &IngestionHandler{service: service}
}
func (h *IngestionHandler) IngestStats(w http.ResponseWriter, r *http.Request) {
	hostID, ok := r.Context().Value(config.HostIDContextKey).(int64)
	if !ok || hostID <= 0 {
		writeIngestionError(w, 401, "unauthorized", "Host authentication required")
		return
	}
	var request models.MachineStatsBatchRequest
	if !decodeIngestion(w, r, &request) {
		return
	}
	ack, err := h.service.IngestStatsBatch(r.Context(), hostID, &request)
	if err != nil {
		ingestionServiceError(w, err)
		return
	}
	writeIngestionJSON(w, 201, map[string]any{"message": "machine statistics ingested", "data": ack})
}
func (h *IngestionHandler) IngestErrors(w http.ResponseWriter, r *http.Request) {
	hostID, ok := r.Context().Value(config.HostIDContextKey).(int64)
	if !ok || hostID <= 0 {
		writeIngestionError(w, 401, "unauthorized", "Host authentication required")
		return
	}
	var request models.ErrorLogsRequest
	if !decodeIngestion(w, r, &request) {
		return
	}
	accepted, inserted, err := h.service.IngestErrors(r.Context(), hostID, &request)
	if err != nil {
		ingestionServiceError(w, err)
		return
	}
	ids := make([]int64, 0, len(request.Errors))
	for _, entry := range request.Errors {
		ids = append(ids, entry.ID)
	}
	writeIngestionJSON(w, 201, map[string]any{"message": "error logs ingested", "accepted": accepted, "inserted": inserted, "duplicates": accepted - inserted, "source_ids": ids})
}
func decodeIngestion(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, config.MaxRequestBodySize)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	err := d.Decode(dest)
	if err == nil {
		var trailing any
		err = d.Decode(&trailing)
		if err == io.EOF {
			return true
		}
	}
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		writeIngestionError(w, 413, "body_too_large", "Maximum body size is 1 MiB")
	} else {
		writeIngestionError(w, 400, "invalid_request", "Expected one JSON object with documented fields")
	}
	return false
}
func ingestionServiceError(w http.ResponseWriter, err error) {
	var recordErr *models.RecordValidationError
	if errors.As(err, &recordErr) {
		writeIngestionJSON(w, 400, map[string]any{"error": "invalid_data", "message": recordErr.Error(), "source_id": recordErr.SourceID})
		return
	}
	for _, expected := range []error{models.ErrFutureTimestamp, models.ErrBatchTooLarge, models.ErrStatsBatchTooLarge, models.ErrEmptyStatsBatch, models.ErrMissingStatID, models.ErrDuplicateSourceID, models.ErrFieldTooLarge, models.ErrInvalidCPUPercent, models.ErrInvalidRAMUsage, models.ErrInvalidDiskUsage, models.ErrEmptyErrorBatch, models.ErrMissingErrorID, models.ErrMissingAppName, models.ErrMissingEnvironment, models.ErrInvalidErrorLevel, models.ErrMissingErrorMessage} {
		if errors.Is(err, expected) {
			writeIngestionError(w, 400, "invalid_data", expected.Error())
			return
		}
	}
	if errors.Is(err, models.ErrInvalidAPIKey) {
		writeIngestionError(w, 401, "unauthorized", "Host authentication required")
		return
	}
	writeIngestionError(w, 500, "internal_error", "Unable to save monitoring data")
}
func writeIngestionError(w http.ResponseWriter, status int, code, message string) {
	writeIngestionJSON(w, status, models.ErrorResponse{Error: code, Message: message})
}
func writeIngestionJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
