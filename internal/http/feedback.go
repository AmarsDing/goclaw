package http

import (
	"database/sql"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/spirit"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	pgstore "github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// FeedbackHandler records DreamWeaver Spirit user feedback into profile affinity and optional topic rows.
type FeedbackHandler struct {
	loop *spirit.LearningLoop
}

// NewFeedbackHandler builds a handler backed by Postgres. If db is nil, routes return 503.
func NewFeedbackHandler(db *sql.DB) *FeedbackHandler {
	if db == nil {
		return &FeedbackHandler{}
	}
	prof := pgstore.NewPGSpiritProfileStore(db)
	mgr := spirit.NewProfileManager(prof)
	loop := spirit.NewLearningLoop(mgr)
	loop.BindTopicStore(pgstore.NewPGTopicStore(db), "")
	return &FeedbackHandler{loop: loop}
}

// RegisterRoutes registers POST /v1/feedback.
func (h *FeedbackHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/feedback", h.readAuth(h.handlePost))
}

func (h *FeedbackHandler) readAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *FeedbackHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	if h.loop == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "feedback storage not configured"})
		return
	}
	var fb spirit.Feedback
	if !bindJSON(w, r, extractLocale(r), &fb) {
		return
	}
	if fb.UserID == "" {
		fb.UserID = store.UserIDFromContext(r.Context())
	}
	if fb.TenantID == "" {
		fb.TenantID = store.TenantIDFromContext(r.Context()).String()
	}
	if fb.UserID == "" || fb.TenantID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id and tenant_id are required"})
		return
	}
	if fb.RunID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "run_id is required"})
		return
	}
	ctx := r.Context()
	if err := h.loop.RecordFeedback(ctx, fb); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
