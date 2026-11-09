package handlers

import (
	"net/http"

	"monitor-central/internal/templating/pages"
	"monitor-central/internal/utils/session"
)

// LegalHandler handles legal pages requests
type LegalHandler struct {
	tb             *pages.PageBox
	sessionManager *session.SessionManager
}

// NewLegalHandler creates a new LegalHandler
func NewLegalHandler(tb *pages.PageBox, sessionManager *session.SessionManager) *LegalHandler {
	return &LegalHandler{
		tb:             tb,
		sessionManager: sessionManager,
	}
}

// PrivacyPolicy renders the privacy policy page
func (h *LegalHandler) PrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	h.tb.RenderPage("privacy", nil, w, r)
}

// Terms renders the terms and conditions page
func (h *LegalHandler) Terms(w http.ResponseWriter, r *http.Request) {
	h.tb.RenderPage("terms", nil, w, r)
}
