package pages

import (
	"context"
	"github.com/go-chi/chi/v5"
	"io"
	"log"
	"monitor-central/internal/config"
	"monitor-central/internal/models"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestEveryPageAndStaticAssets(t *testing.T) {
	pb, err := NewPageBox("../../../frontend", log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	id := int64(1)
	form := map[string]any{"Hosts": []models.AlertHostOption{{ID: 1, Name: "Host"}}, "SelectedHost": id, "Metrics": []string{"cpu"}, "Metric": "cpu", "Condition": "gt", "Threshold": 80, "Severity": "critical"}
	data := map[string]any{"User": &models.User{Name: "<script>alert(1)</script>", Email: "admin@example.com"}, "Hosts": []models.HostListItem{{ID: 1, Name: "Host"}}, "Filters": models.ErrorLogFilters{}, "Alerts": []models.AlertRuleListItem{{ID: 1, HostID: &id}}, "Errors": []models.ErrorLogListItem{{ID: 1, Message: "<script>alert(1)</script>"}}, "NewAlertForm": form, "EditAlertForms": []map[string]any{form}}
	for name := range pb.Templates {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/"+name, nil)
			r = r.WithContext(context.WithValue(r.Context(), config.IsAuthenticatedContextKey, true))
			w := httptest.NewRecorder()
			pb.RenderPage(name, data, w, r)
			if w.Code != 200 {
				t.Fatalf("render failed %d %s", w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), "<script>alert(1)</script>") {
				t.Fatal("user data not escaped")
			}
			body := w.Body.String()
			if !strings.Contains(body, `<script src="/public/tailwindcss-browser-4.1.18.js">`) {
				t.Fatal("local Tailwind missing")
			}
			// ADR-0016: the CSP allows scripts from this origin only, never inline.
			if strings.Contains(body, "://cdn.") || inlineScript.MatchString(body) || inlineHandler.MatchString(body) {
				t.Fatalf("page %s has an external or inline script, which the CSP blocks", name)
			}
		})
	}
	router := chi.NewRouter()
	pb.MountStatic(router)
	for _, test := range []struct {
		path   string
		status int
	}{{"/js/app.js", 200}, {"/public/app.css", 200}, {"/public/tailwindcss-browser-4.1.18.js", 200}, {"/js/", 404}, {"/js/../templates/login.html", 404}, {"/js/.env", 404}} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", test.path, nil))
		if w.Code != test.status {
			t.Fatalf("%s: %d", test.path, w.Code)
		}
	}
}

var (
	inlineScript  = regexp.MustCompile(`<script(\s[^>]*)?>\s*[^<\s]`) // a <script> with a body
	inlineHandler = regexp.MustCompile(`\son[a-z]+\s*=`)              // onclick=, onload=, …
)
