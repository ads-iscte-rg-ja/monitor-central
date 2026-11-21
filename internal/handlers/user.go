package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"

	"monitor-central/internal/config"
	"monitor-central/internal/models"
	"monitor-central/internal/templating/pages"
	"monitor-central/internal/utils/session"
)

// userService is what the profile page needs (ADR-0014).
type userService interface {
	GetProfile(ctx context.Context, userID int64) (*models.User, error)
	UpdateNotificationEmail(ctx context.Context, userID int64, notificationEmail string) error
}

type UserHandler struct {
	logger         *log.Logger
	userService    userService
	sessionManager *session.SessionManager
	tb             *pages.PageBox
}

func NewUserHandler(userService userService, sessionManager *session.SessionManager, tb *pages.PageBox, logger *log.Logger) *UserHandler {
	return &UserHandler{
		logger:         logger,
		userService:    userService,
		sessionManager: sessionManager,
		tb:             tb,
	}
}

func (h *UserHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(config.UserIDContextKey).(int64)

	user, err := h.userService.GetProfile(r.Context(), userID)
	if err != nil {
		h.logger.Printf("Error fetching profile: %v", err)
		http.Error(w, "Error fetching profile", http.StatusInternalServerError)
		return
	}

	h.tb.RenderPage("profile", map[string]any{
		"User":     user,
		"UserName": user.Name,
	}, w, r)
}

func (h *UserHandler) UpdateNotificationEmail(
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

	userID := r.Context().
		Value(config.UserIDContextKey).(int64)

	err := h.userService.UpdateNotificationEmail(
		r.Context(),
		userID,
		r.FormValue("notification_email"),
	)
	if err != nil {
		if errors.Is(err, models.ErrInvalidEmail) {
			http.Error(
				w,
				err.Error(),
				http.StatusBadRequest,
			)
			return
		}

		h.logger.Printf(
			"Error updating notification email: %v",
			err,
		)
		http.Error(
			w,
			"Error updating notification email",
			http.StatusInternalServerError,
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/profile",
		http.StatusSeeOther,
	)
}
