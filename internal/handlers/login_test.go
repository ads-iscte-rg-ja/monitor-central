package handlers

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"monitor-central/internal/models"
)

type fakeAuth struct {
	userID  int64
	err     error
	logouts []int64
}

func (f *fakeAuth) Login(context.Context, *models.LoginRequest, string) (int64, error) {
	return f.userID, f.err
}
func (f *fakeAuth) Logout(_ context.Context, id int64, _ string) { f.logouts = append(f.logouts, id) }

type fakeSession struct {
	loggedIn  int64
	current   int64
	destroyed bool
	err       error
}

func (f *fakeSession) LoginUser(_ context.Context, id int64) error { f.loggedIn = id; return f.err }
func (f *fakeSession) GetInt64(context.Context, string) int64      { return f.current }
func (f *fakeSession) Destroy(context.Context) error               { f.destroyed = true; return f.err }

func TestLoginOutcomes(t *testing.T) {
	form := url.Values{"email": {"ana@example.com"}, "password": {"secret"}}
	auth, sessions := &fakeAuth{userID: 7}, &fakeSession{}
	h := NewAuthHandler(auth, sessions, realPages(t), discard())

	wantRedirect(t, call(t, "POST", "/login", "/login", form, h.Login), "/")
	if sessions.loggedIn != 7 {
		t.Fatalf("session for %d", sessions.loggedIn)
	}
	// Wrong password: the form again, with the email kept and a generic message.
	auth.err = models.ErrInvalidCredentials
	wantStatus(t, call(t, "POST", "/login", "/login", form, h.Login), http.StatusOK, "Incorrect email or password.")
	auth.err = errStore
	wantStatus(t, call(t, "POST", "/login", "/login", form, h.Login), http.StatusOK, "Error while logging in.")
	auth.err, sessions.err = nil, errStore
	wantStatus(t, call(t, "POST", "/login", "/login", form, h.Login), http.StatusInternalServerError, "Login error")
}

func TestLogoutAuditsTheUserAndEndsTheSession(t *testing.T) {
	auth, sessions := &fakeAuth{}, &fakeSession{current: 7}
	h := NewAuthHandler(auth, sessions, nil, discard())
	wantRedirect(t, call(t, "POST", "/logout", "/logout", nil, h.Logout), "/")
	if len(auth.logouts) != 1 || auth.logouts[0] != 7 || !sessions.destroyed {
		t.Fatalf("logouts %v destroyed %v", auth.logouts, sessions.destroyed)
	}
	// No user in the session: nothing to audit, the session still ends.
	sessions.current, sessions.destroyed, sessions.err = 0, false, errStore
	wantRedirect(t, call(t, "POST", "/logout", "/logout", nil, h.Logout), "/")
	if len(auth.logouts) != 1 || !sessions.destroyed {
		t.Fatalf("logouts %v destroyed %v", auth.logouts, sessions.destroyed)
	}
}

func TestLegalPages(t *testing.T) {
	h := NewLegalHandler(realPages(t), nil)
	wantStatus(t, call(t, "GET", "/privacy-policy", "/privacy-policy", nil, h.PrivacyPolicy), http.StatusOK, "<html")
	wantStatus(t, call(t, "GET", "/terms", "/terms", nil, h.Terms), http.StatusOK, "<html")
}
