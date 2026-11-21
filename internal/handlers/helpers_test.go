package handlers

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"monitor-central/internal/config"
	"monitor-central/internal/templating/pages"
)

// errStore is an unexpected failure: the handler logs it and answers 500.
var errStore = errors.New("store unavailable")

func discard() *log.Logger { return log.New(io.Discard, "", 0) }

// realPages renders the real templates (frontend/), so a field a template uses
// and a handler does not pass fails the test.
func realPages(t *testing.T) *pages.PageBox {
	t.Helper()
	tb, err := pages.NewPageBox("../../frontend", discard())
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

// call routes one request through chi (for URL parameters), as a signed-in user.
func call(t *testing.T, method, pattern, target string, form url.Values, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Method(method, pattern, h)
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	ctx := context.WithValue(req.Context(), config.UserIDContextKey, int64(7))
	ctx = context.WithValue(ctx, config.IsAuthenticatedContextKey, true)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req.WithContext(ctx))
	return w
}

func wantStatus(t *testing.T, w *httptest.ResponseRecorder, status int, contains string) {
	t.Helper()
	if w.Code != status || !strings.Contains(w.Body.String(), contains) {
		t.Fatalf("status %d (want %d), body %q (want it to contain %q)", w.Code, status, w.Body.String(), contains)
	}
}

func wantRedirect(t *testing.T, w *httptest.ResponseRecorder, location string) {
	t.Helper()
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != location {
		t.Fatalf("status %d location %q, want 303 to %s", w.Code, w.Header().Get("Location"), location)
	}
}
