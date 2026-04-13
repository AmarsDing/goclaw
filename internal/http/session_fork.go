package http

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SessionForkHandler creates a new session key that copies message history from an existing session.
type SessionForkHandler struct {
	sessions  store.SessionStore
	domainBus eventbus.DomainEventBus
	hookFire  hooks.HookFire // optional; fires hooks.EventSessionFork after a successful fork
}

// NewSessionForkHandler returns a handler. sessions may be nil (routes return 503).
// domainBus is optional; when set, a successful fork publishes EventSessionForked.
// hookFire is optional; when set, a successful fork fires hooks.EventSessionFork.
func NewSessionForkHandler(sessions store.SessionStore, domainBus eventbus.DomainEventBus, hookFire ...hooks.HookFire) *SessionForkHandler {
	h := &SessionForkHandler{sessions: sessions, domainBus: domainBus}
	if len(hookFire) > 0 {
		h.hookFire = hookFire[0]
	}
	return h
}

// RegisterRoutes registers POST /v1/sessions/fork.
func (h *SessionForkHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/sessions/fork", requireAuth("", h.handlePost))
}

type sessionForkRequest struct {
	SourceSessionKey string `json:"source_session_key"`
	ForkLabel        string `json:"fork_label,omitempty"`
}

func (h *SessionForkHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	if h.sessions == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session store not configured"})
		return
	}
	var req sessionForkRequest
	if !bindJSON(w, r, extractLocale(r), &req) {
		return
	}
	req.SourceSessionKey = strings.TrimSpace(req.SourceSessionKey)
	if req.SourceSessionKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source_session_key is required"})
		return
	}

	ctx := r.Context()
	src := h.sessions.Get(ctx, req.SourceSessionKey)
	if src == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source session not found"})
		return
	}

	label := sanitizeForkLabel(req.ForkLabel)
	forkKey := req.SourceSessionKey + ":fork:" + label

	// Copy history and summary into the forked session.
	history := h.sessions.GetHistory(ctx, req.SourceSessionKey)
	forked := make([]providers.Message, len(history))
	copy(forked, history)

	h.sessions.GetOrCreate(ctx, forkKey)
	h.sessions.SetHistory(ctx, forkKey, forked)
	if sum := h.sessions.GetSummary(ctx, req.SourceSessionKey); sum != "" {
		h.sessions.SetSummary(ctx, forkKey, sum)
	}
	if err := h.sessions.Save(ctx, forkKey); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.domainBus != nil {
		tenantID := store.TenantIDFromContext(ctx)
		h.domainBus.Publish(eventbus.DomainEvent{
			ID:        uuid.Must(uuid.NewV7()).String(),
			Type:      eventbus.EventSessionForked,
			SourceID:  forkKey,
			TenantID:  tenantID.String(),
			UserID:    store.UserIDFromContext(ctx),
			Timestamp: time.Now(),
			Payload: eventbus.SessionForkedPayload{
				SourceSessionKey: req.SourceSessionKey,
				ForkSessionKey:   forkKey,
				MessageCount:     len(forked),
			},
		})
	}

	if h.hookFire != nil {
		forkPayload := hooks.SessionForkPayload{
			SourceSessionKey: req.SourceSessionKey,
			ForkSessionKey:   forkKey,
			MessageCount:     len(forked),
		}
		// Fire async (system-level, not tied to an agent run: RunID / AgentID are empty).
		_, _ = h.hookFire(context.WithoutCancel(ctx), hooks.NewPayload(
			hooks.EventSessionFork, "", "", forkKey, forkPayload,
		))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                 true,
		"fork_session_key":   forkKey,
		"source_session_key": req.SourceSessionKey,
		"message_count":      len(forked),
	})
}

var forkLabelSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeForkLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return uuid.NewString()[:8]
	}
	s = forkLabelSanitizer.ReplaceAllString(s, "-")
	if len(s) > 48 {
		s = s[:48]
	}
	if s == "" {
		return uuid.NewString()[:8]
	}
	return s
}
