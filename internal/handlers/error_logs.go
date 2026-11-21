package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"monitor-central/internal/models"
	"monitor-central/internal/templating/pages"
)

// errorLogsService is what the errors page needs (ADR-0014).
type errorLogsService interface {
	GetPage(ctx context.Context, filters models.ErrorLogFilters) (*models.ErrorLogsPage, error)
}

type ErrorLogsHandler struct {
	service errorLogsService
	tb      *pages.PageBox
	logger  *log.Logger
}

func NewErrorLogsHandler(
	service errorLogsService,
	tb *pages.PageBox,
	logger *log.Logger,
) *ErrorLogsHandler {
	return &ErrorLogsHandler{
		service: service,
		tb:      tb,
		logger:  logger,
	}
}

func (h *ErrorLogsHandler) GetErrorLogs(
	w http.ResponseWriter,
	r *http.Request,
) {
	pageNumber := 1

	if pageValue := r.URL.Query().Get("page"); pageValue != "" {
		parsedPage, err := strconv.Atoi(pageValue)
		if err == nil && parsedPage > 0 {
			pageNumber = parsedPage
		}
	}

	hostID, err := optionalID(r.URL.Query().Get("host_id"))
	if err != nil {
		http.Error(w, "Invalid host ID", http.StatusBadRequest)
		return
	}
	filters := models.ErrorLogFilters{
		HostID: hostID,
		Level: strings.ToUpper(strings.TrimSpace(
			r.URL.Query().Get("level"),
		)),
		AppName: strings.TrimSpace(
			r.URL.Query().Get("app_name"),
		),
		Search: strings.TrimSpace(
			r.URL.Query().Get("search"),
		),
		Page:     pageNumber,
		PageSize: 20,
	}

	page, err := h.service.GetPage(r.Context(), filters)
	if err != nil {
		if errors.Is(err, models.ErrInvalidErrorLevel) {
			http.Error(
				w,
				"Invalid error level",
				http.StatusBadRequest,
			)
			return
		}

		h.logger.Printf(
			"Failed to load error logs page: %v",
			err,
		)

		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}
	for i := range page.Errors {
		page.Errors[i].MetadataText =
			string(page.Errors[i].Metadata)
	}

	var previousPageURL string
	var nextPageURL string

	if page.Page > 1 {
		previousPageURL = buildErrorLogsPageURL(
			page.Filters,
			page.Page-1,
		)
	}

	if page.Page < page.TotalPages {
		nextPageURL = buildErrorLogsPageURL(
			page.Filters,
			page.Page+1,
		)
	}

	h.tb.RenderPage(
		"errors",
		map[string]interface{}{
			"Errors":          page.Errors,
			"Levels":          models.LevelsMostSevereFirst(),
			"Hosts":           page.Hosts,
			"AppNames":        page.AppNames,
			"TotalErrors":     page.TotalErrors,
			"Page":            page.Page,
			"PageSize":        page.PageSize,
			"TotalPages":      page.TotalPages,
			"Filters":         page.Filters,
			"PreviousPageURL": previousPageURL,
			"NextPageURL":     nextPageURL,
		},
		w,
		r,
	)
}

func buildErrorLogsPageURL(
	filters models.ErrorLogFilters,
	page int,
) string {
	values := url.Values{}

	if filters.HostID > 0 {
		values.Set("host_id", strconv.FormatInt(filters.HostID, 10))
	}

	if filters.Level != "" {
		values.Set("level", filters.Level)
	}

	if filters.AppName != "" {
		values.Set("app_name", filters.AppName)
	}

	if filters.Search != "" {
		values.Set("search", filters.Search)
	}

	values.Set("page", strconv.Itoa(page))

	return "/errors?" + values.Encode()
}
