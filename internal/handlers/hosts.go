package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"monitor-central/internal/models"
	"monitor-central/internal/services"
	"monitor-central/internal/templating/pages"

	"github.com/go-chi/chi/v5"
)

type HostsHandler struct {
	service *services.HostsService
	tb      *pages.PageBox
	logger  *log.Logger
}

func NewHostsHandler(
	service *services.HostsService,
	tb *pages.PageBox,
	logger *log.Logger,
) *HostsHandler {
	return &HostsHandler{
		service: service,
		tb:      tb,
		logger:  logger,
	}
}

func (h *HostsHandler) GetHosts(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.renderHosts(w, r, "", "", "")
}

func (h *HostsHandler) renderHosts(w http.ResponseWriter, r *http.Request, newAPIKey, apiKeyNoticeTitle, apiKeyNoticeMessage string) {
	page, err := h.service.GetPage(r.Context())
	if err != nil {
		h.logger.Printf(
			"Failed to load hosts page: %v",
			err,
		)

		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}

	h.tb.RenderPage(
		"hosts",
		map[string]interface{}{
			"Hosts":               page.Hosts,
			"NewAPIKey":           newAPIKey,
			"APIKeyNoticeTitle":   apiKeyNoticeTitle,
			"APIKeyNoticeMessage": apiKeyNoticeMessage,
		},
		w,
		r,
	)
}

func (h *HostsHandler) CreateHost(
	w http.ResponseWriter,
	r *http.Request,
) {
	if err := r.ParseForm(); err != nil {
		http.Error(
			w,
			"Invalid form",
			http.StatusBadRequest,
		)
		return
	}

	timeoutMinutes, err := hostContactTimeout(r.FormValue("contact_timeout_minutes"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	host, err := h.service.CreateHost(
		r.Context(),
		models.CreateHostRequest{
			Name:                  r.FormValue("name"),
			Hostname:              r.FormValue("hostname"),
			HealthEndpoint:        r.FormValue("health_endpoint"),
			ContactTimeoutMinutes: timeoutMinutes,
		},
	)
	if err != nil {
		if errors.Is(err, models.ErrHostNameRequired) ||
			errors.Is(err, models.ErrHostnameRequired) || errors.Is(err, models.ErrFieldTooLarge) || errors.Is(err, models.ErrHealthEndpoint) || errors.Is(err, models.ErrContactTimeout) {
			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)
			return
		}

		h.logger.Printf("Failed to create host: %v", err)

		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}

	// Render once directly: never persist the raw key in the session database.
	h.renderHosts(w, r, host.APIKey, "Host registered successfully", host.Name+" is ready to receive monitoring data.")
}

func (h *HostsHandler) DeleteHost(
	w http.ResponseWriter,
	r *http.Request,
) {
	hostID, err := requiredID(chi.URLParam(r, "hostID"))
	if err != nil {
		http.Error(w, "Invalid host ID", http.StatusBadRequest)
		return
	}

	err = h.service.DeleteHost(
		r.Context(),
		hostID,
	)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrHostIDRequired):
			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)

		case errors.Is(err, models.ErrHostNotFound):
			http.Error(
				w,
				err.Error(),
				http.StatusNotFound,
			)

		default:
			h.logger.Printf(
				"Failed to delete host: %v",
				err,
			)

			http.Error(
				w,
				http.StatusText(
					http.StatusInternalServerError,
				),
				http.StatusInternalServerError,
			)
		}

		return
	}

	http.Redirect(
		w,
		r,
		"/hosts",
		http.StatusSeeOther,
	)
}

func (h *HostsHandler) UpdateHost(
	w http.ResponseWriter,
	r *http.Request,
) {
	if err := r.ParseForm(); err != nil {
		http.Error(
			w,
			"Invalid form",
			http.StatusBadRequest,
		)
		return
	}

	hostID, err := requiredID(chi.URLParam(r, "hostID"))
	if err != nil {
		http.Error(w, "Invalid host ID", http.StatusBadRequest)
		return
	}
	timeoutMinutes, err := hostContactTimeout(r.FormValue("contact_timeout_minutes"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = h.service.UpdateHost(
		r.Context(),
		hostID,
		models.UpdateHostRequest{
			Name:                  r.FormValue("name"),
			Hostname:              r.FormValue("hostname"),
			HealthEndpoint:        r.FormValue("health_endpoint"),
			ContactTimeoutMinutes: timeoutMinutes,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrHostIDRequired),
			errors.Is(err, models.ErrHostNameRequired),
			errors.Is(err, models.ErrHostnameRequired), errors.Is(err, models.ErrFieldTooLarge), errors.Is(err, models.ErrHealthEndpoint), errors.Is(err, models.ErrContactTimeout):
			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)

		case errors.Is(err, models.ErrHostNotFound):
			http.Error(
				w,
				err.Error(),
				http.StatusNotFound,
			)

		default:
			h.logger.Printf(
				"Failed to update host: %v",
				err,
			)

			http.Error(
				w,
				http.StatusText(
					http.StatusInternalServerError,
				),
				http.StatusInternalServerError,
			)
		}

		return
	}

	http.Redirect(
		w,
		r,
		"/hosts",
		http.StatusSeeOther,
	)
}

func hostContactTimeout(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 5, nil
	}
	minutes, err := strconv.Atoi(value)
	if err != nil || minutes < 1 || minutes > 1440 {
		return 0, models.ErrContactTimeout
	}
	return minutes, nil
}

func (h *HostsHandler) RegenerateAPIKey(
	w http.ResponseWriter,
	r *http.Request,
) {
	hostID, err := requiredID(chi.URLParam(r, "hostID"))
	if err != nil {
		http.Error(w, "Invalid host ID", http.StatusBadRequest)
		return
	}
	apiKey, err := h.service.RegenerateAPIKey(
		r.Context(),
		hostID,
	)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrHostIDRequired):
			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)

		case errors.Is(err, models.ErrHostNotFound):
			http.Error(
				w,
				err.Error(),
				http.StatusNotFound,
			)

		default:
			h.logger.Printf(
				"Failed to regenerate API key: %v",
				err,
			)

			http.Error(
				w,
				http.StatusText(
					http.StatusInternalServerError,
				),
				http.StatusInternalServerError,
			)
		}

		return
	}

	h.renderHosts(w, r, apiKey, "API key regenerated", "The previous API key is no longer valid.")
}
