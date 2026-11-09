package session

import (
	"github.com/alexedwards/scs/v2/memstore"
	"monitor-central/internal/repositories"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecureCookieConfiguration(t *testing.T) {
	for _, value := range []string{"true", "false"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("COOKIE_SECURE", value)
			sm := NewSessionManagerWithDefaults(&repositories.SessionStore{})
			sm.Store = memstore.NewWithCleanupInterval(0)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "https://monitor.example/", nil)
			sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sm.Put(r.Context(), "user_id", int64(1))
				w.WriteHeader(200)
			})).ServeHTTP(w, r)
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Secure != (value == "true") || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
				t.Fatalf("unexpected cookie %+v", cookies)
			}
		})
	}
}
