// Package pages serves plain HTML and composes Go templates with shared partials.
package pages

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"monitor-central/internal/config"
)

type PageBox struct {
	Templates map[string]*template.Template
	pages     map[string][]byte
	root      string
	logger    *log.Logger
}

func NewPageBox(root string, logger *log.Logger) (*PageBox, error) {
	partials, err := filepath.Glob(filepath.Join(root, "partials", "*.html"))
	if err != nil || len(partials) == 0 {
		return nil, fmt.Errorf("no HTML partials in %s", root)
	}
	base, err := template.New("base").ParseFiles(partials...)
	if err != nil {
		return nil, fmt.Errorf("parse partials: %w", err)
	}
	if base.Lookup("base") == nil {
		return nil, fmt.Errorf("partials must define base")
	}
	files, err := filepath.Glob(filepath.Join(root, "templates", "*.html"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no HTML templates in %s", root)
	}
	pb := &PageBox{Templates: make(map[string]*template.Template), pages: make(map[string][]byte), root: root, logger: logger}
	for _, file := range files {
		t, err := base.Clone()
		if err != nil {
			return nil, err
		}
		if _, err = t.ParseFiles(file); err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
		if t.Lookup("main") == nil {
			return nil, fmt.Errorf("%s must define main", file)
		}
		pb.Templates[strings.TrimSuffix(filepath.Base(file), ".html")] = t
	}
	static, err := filepath.Glob(filepath.Join(root, "pages", "*.html"))
	if err != nil {
		return nil, err
	}
	for _, file := range static {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		pb.pages[strings.TrimSuffix(filepath.Base(file), ".html")] = content
	}
	return pb, nil
}

func (pb *PageBox) RenderPage(name string, data any, w http.ResponseWriter, r *http.Request) {
	pb.RenderPageStatus(name, data, http.StatusOK, w, r)
}

func (pb *PageBox) RenderPageStatus(name string, data any, status int, w http.ResponseWriter, r *http.Request) {
	t, ok := pb.Templates[name]
	if !ok {
		http.Error(w, "Page unavailable", http.StatusInternalServerError)
		return
	}
	d := map[string]any{"Year": time.Now().Year(), "CurrentPath": r.URL.Path, "isAuthenticated": false}
	if m, ok := data.(map[string]any); ok {
		for k, v := range m {
			d[k] = v
		}
	} else if data != nil {
		d["Data"] = data
	}
	d["isAuthenticated"], _ = r.Context().Value(config.IsAuthenticatedContextKey).(bool)
	var b bytes.Buffer
	if err := t.ExecuteTemplate(&b, "base", d); err != nil {
		pb.logger.Printf("render page %q: %v", name, err)
		http.Error(w, "Page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = b.WriteTo(w)
}

// ServePage serves a prewritten HTML document without executing template actions.
func (pb *PageBox) ServePage(name string, w http.ResponseWriter, r *http.Request) {
	content, ok := pb.pages[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}

func (pb *PageBox) MountStatic(r chi.Router) {
	for _, asset := range []string{"js", "public"} {
		prefix := "/" + asset
		root := filepath.Join(pb.root, asset)
		r.Handle(prefix+"/*", http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// Expose files, not directory listings, hidden files, or symlink targets.
			clean := filepath.Clean(req.URL.Path)
			if strings.Contains(strings.TrimPrefix(clean, "/"), "/") {
				http.NotFound(w, req)
				return
			}
			for _, part := range strings.Split(clean, "/") {
				if strings.HasPrefix(part, ".") {
					http.NotFound(w, req)
					return
				}
			}
			file := filepath.Join(root, clean)
			rel, err := filepath.Rel(root, file)
			if err != nil || strings.HasPrefix(rel, "..") {
				http.NotFound(w, req)
				return
			}
			info, err := os.Lstat(file)
			if err != nil || !info.Mode().IsRegular() {
				http.NotFound(w, req)
				return
			}
			http.ServeFile(w, req, file)
		})))
	}
}
