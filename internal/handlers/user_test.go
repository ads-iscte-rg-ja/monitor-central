package handlers

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"monitor-central/internal/models"
)

type fakeUsers struct {
	err    error
	userID int64
	email  string
}

func (f *fakeUsers) GetProfile(_ context.Context, id int64) (*models.User, error) {
	f.userID = id
	if f.err != nil {
		return nil, f.err
	}
	return &models.User{ID: id, Name: "Ana", Email: "ana@example.com"}, nil
}
func (f *fakeUsers) UpdateNotificationEmail(_ context.Context, id int64, email string) error {
	f.userID, f.email = id, email
	return f.err
}

func TestProfileUsesTheSignedInUser(t *testing.T) {
	svc := &fakeUsers{}
	h := NewUserHandler(svc, nil, realPages(t), discard())
	wantStatus(t, call(t, "GET", "/profile", "/profile", nil, h.GetProfile), http.StatusOK, "Ana")
	if svc.userID != 7 {
		t.Fatalf("user %d", svc.userID)
	}
	form := url.Values{"notification_email": {"alerts@example.com"}}
	wantRedirect(t, call(t, "POST", "/profile/notification-email", "/profile/notification-email", form, h.UpdateNotificationEmail), "/profile")
	if svc.email != "alerts@example.com" {
		t.Fatalf("email %q", svc.email)
	}
	svc.err = models.ErrInvalidEmail
	wantStatus(t, call(t, "POST", "/profile/notification-email", "/profile/notification-email", form, h.UpdateNotificationEmail), http.StatusBadRequest, models.ErrInvalidEmail.Error())
	svc.err = errStore
	wantStatus(t, call(t, "POST", "/profile/notification-email", "/profile/notification-email", form, h.UpdateNotificationEmail), http.StatusInternalServerError, "Error updating")
	wantStatus(t, call(t, "GET", "/profile", "/profile", nil, h.GetProfile), http.StatusInternalServerError, "Error fetching profile")
}
