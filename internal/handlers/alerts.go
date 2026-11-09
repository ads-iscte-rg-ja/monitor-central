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

type AlertsHandler struct {
	service *services.AlertsService
	tb      *pages.PageBox
	logger  *log.Logger
}

func NewAlertsHandler(
	service *services.AlertsService,
	tb *pages.PageBox,
	logger *log.Logger,
) *AlertsHandler {
	return &AlertsHandler{
		service: service,
		tb:      tb,
		logger:  logger,
	}
}

func (h *AlertsHandler) GetAlerts(
	w http.ResponseWriter,
	r *http.Request,
) {
	page, err := h.service.GetPage(r.Context())
	if err != nil {
		h.logger.Printf(
			"Failed to load alerts page: %v",
			err,
		)

		http.Error(
			w,
			http.StatusText(
				http.StatusInternalServerError,
			),
			http.StatusInternalServerError,
		)
		return
	}

	forms := make([]map[string]any, 0, len(page.Alerts))
	for _, alert := range page.Alerts {
		forms = append(forms, alertForm(page.Hosts, alert))
	}
	h.tb.RenderPage(
		"alerts",
		map[string]interface{}{
			"Alerts":         page.Alerts,
			"AlertEvents":    page.AlertEvents,
			"Hosts":          page.Hosts,
			"NewAlertForm":   alertForm(page.Hosts, models.AlertRuleListItem{Metric: "cpu", Condition: "gt", Threshold: 80, Severity: "warning", Enabled: true, ChannelsCSV: "email"}),
			"EditAlertForms": forms,
		},
		w,
		r,
	)
}

func (h *AlertsHandler) CreateAlert(
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

	threshold, err := strconv.ParseFloat(
		r.FormValue("threshold"),
		64,
	)
	if err != nil {
		http.Error(
			w,
			models.ErrInvalidAlertThreshold.Error(),
			http.StatusBadRequest,
		)
		return
	}

	hostID, err := requiredID(r.FormValue("host_id"))
	if err != nil {
		http.Error(w, "Invalid host ID", http.StatusBadRequest)
		return
	}
	_, err = h.service.CreateAlert(
		r.Context(),
		models.CreateAlertRequest{
			Name:      r.FormValue("name"),
			HostID:    hostID,
			Metric:    r.FormValue("metric"),
			Condition: r.FormValue("condition"),
			Threshold: threshold,
			Severity:  r.FormValue("severity"),
			Channels:  r.Form["channels"],
			Enabled:   r.FormValue("enabled") != "",
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrAlertNameRequired),
			errors.Is(err, models.ErrFieldTooLarge),
			errors.Is(err, models.ErrAlertHostRequired),
			errors.Is(err, models.ErrInvalidAlertMetric),
			errors.Is(err, models.ErrInvalidAlertCondition),
			errors.Is(err, models.ErrInvalidAlertThreshold),
			errors.Is(err, models.ErrInvalidAlertSeverity),
			errors.Is(err, models.ErrAlertChannelRequired):
			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)

		default:
			h.logger.Printf(
				"Failed to create alert: %v",
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
		"/alerts",
		http.StatusSeeOther,
	)
}

func (h *AlertsHandler) DeleteAlert(
	w http.ResponseWriter,
	r *http.Request,
) {
	alertID, err := requiredID(chi.URLParam(r, "alertID"))
	if err != nil {
		http.Error(w, "Invalid alert ID", http.StatusBadRequest)
		return
	}
	err = h.service.DeleteAlert(
		r.Context(),
		alertID,
	)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrAlertIDRequired):
			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)

		case errors.Is(err, models.ErrAlertNotFound):
			http.Error(
				w,
				err.Error(),
				http.StatusNotFound,
			)

		default:
			h.logger.Printf(
				"Failed to delete alert: %v",
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
		"/alerts",
		http.StatusSeeOther,
	)
}

func (h *AlertsHandler) UpdateAlert(
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

	threshold, err := strconv.ParseFloat(
		r.FormValue("threshold"),
		64,
	)
	if err != nil {
		http.Error(
			w,
			models.ErrInvalidAlertThreshold.Error(),
			http.StatusBadRequest,
		)
		return
	}

	alertID, err := requiredID(chi.URLParam(r, "alertID"))
	if err != nil {
		http.Error(w, "Invalid alert ID", http.StatusBadRequest)
		return
	}
	hostID, err := requiredID(r.FormValue("host_id"))
	if err != nil {
		http.Error(w, "Invalid host ID", http.StatusBadRequest)
		return
	}
	err = h.service.UpdateAlert(
		r.Context(),
		alertID,
		models.UpdateAlertRequest{
			Name:      r.FormValue("name"),
			HostID:    hostID,
			Metric:    r.FormValue("metric"),
			Condition: r.FormValue("condition"),
			Threshold: threshold,
			Severity:  r.FormValue("severity"),
			Channels:  r.Form["channels"],
			Enabled:   r.FormValue("enabled") != "",
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrAlertIDRequired),
			errors.Is(err, models.ErrFieldTooLarge),
			errors.Is(err, models.ErrAlertNameRequired),
			errors.Is(err, models.ErrAlertHostRequired),
			errors.Is(err, models.ErrInvalidAlertMetric),
			errors.Is(err, models.ErrInvalidAlertCondition),
			errors.Is(err, models.ErrInvalidAlertThreshold),
			errors.Is(err, models.ErrInvalidAlertSeverity),
			errors.Is(err, models.ErrAlertChannelRequired):
			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)

		case errors.Is(err, models.ErrAlertNotFound):
			http.Error(
				w,
				err.Error(),
				http.StatusNotFound,
			)

		default:
			h.logger.Printf(
				"Failed to update alert: %v",
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
		"/alerts",
		http.StatusSeeOther,
	)
}

func (h *AlertsHandler) AcknowledgeAlertEvent(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := requiredID(chi.URLParam(r, "eventID"))
	if err != nil {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}
	err = h.service.AcknowledgeAlertEvent(
		r.Context(),
		eventID,
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			models.ErrAlertEventIDRequired,
		):
			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)

		case errors.Is(
			err,
			models.ErrAlertEventNotFound,
		):
			http.Error(
				w,
				err.Error(),
				http.StatusNotFound,
			)

		default:
			h.logger.Printf(
				"Failed to acknowledge alert event: %v",
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
		"/alerts",
		http.StatusSeeOther,
	)
}

func alertForm(hosts []models.AlertHostOption, alert models.AlertRuleListItem) map[string]any {
	var hostID int64
	if alert.HostID != nil {
		hostID = *alert.HostID
	}
	return map[string]any{"ID": alert.ID, "Name": alert.Name, "Hosts": hosts, "SelectedHost": hostID, "Metrics": []string{"cpu", "ram", "disk", "error_rate", "error_level", "host_offline"}, "Metric": alert.Metric, "Condition": alert.Condition, "Threshold": alert.Threshold, "Severity": alert.Severity, "Enabled": alert.Enabled, "Email": strings.Contains(","+alert.ChannelsCSV+",", ",email,")}
}
