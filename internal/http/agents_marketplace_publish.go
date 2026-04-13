package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SetMarketplaceHandler wires marketplace catalog upload for POST /v1/agents/{id}/marketplace/publish.
func (h *AgentsHandler) SetMarketplaceHandler(mp *MarketplaceHandler) {
	h.marketplace = mp
}

func (h *AgentsHandler) marketplacePublishMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(MarketplacePublishMinRole(), next)
}

type marketplacePublishBody struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Version      string   `json:"version"`
	License      string   `json:"license"`
	Author       string   `json:"author,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	PackageID    string   `json:"package_id,omitempty"`
	Force        bool     `json:"force,omitempty"`
	PricingModel string   `json:"pricing_model,omitempty"` // "free" (default)
}

func (h *AgentsHandler) handleAgentMarketplacePublish(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())

	if h.marketplace == nil {
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "marketplace not configured"))
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agent"))
		return
	}

	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "agent", id.String()))
		return
	}
	if !h.canExport(ag, userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "agent"))
		return
	}

	var body marketplacePublishBody
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name"))
		return
	}
	if strings.TrimSpace(body.Version) == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "version"))
		return
	}
	if body.License == "" {
		body.License = "MIT"
	}
	pm := marketplace.PricingModel(marketplace.PricingFree)
	if strings.EqualFold(body.PricingModel, string(marketplace.PricingPaid)) {
		pm = marketplace.PricingPaid
	} else if strings.EqualFold(body.PricingModel, string(marketplace.PricingSubscription)) {
		pm = marketplace.PricingSubscription
	}

	pkgID := strings.TrimSpace(body.PackageID)
	if pkgID == "" {
		// One marketplace row per agent; use force to overwrite when publishing a new version.
		pkgID = "agent-" + ag.ID.String()
	}

	pkg := marketplace.Package{
		ID:          pkgID,
		Name:        strings.TrimSpace(body.Name),
		Type:        marketplace.TypeAgent,
		Description: strings.TrimSpace(body.Description),
		Version:     strings.TrimSpace(body.Version),
		License:     body.License,
		Tags:        body.Tags,
		Pricing: marketplace.Pricing{
			Model: pm,
		},
	}
	if a := strings.TrimSpace(body.Author); a != "" {
		pkg.Author = a
	} else if ag.DisplayName != "" {
		pkg.Author = ag.DisplayName
	} else {
		pkg.Author = ag.AgentKey
	}

	zipData, err := h.buildAgentMarketplaceZip(r.Context(), ag)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}

	result, err := h.marketplace.IngestZipArtifact(
		r.Context(),
		pkg,
		bytes.NewReader(zipData),
		fmt.Sprintf("%s-%s.zip", sanitizeMarketplaceSlug(pkg.Name), pkg.Version),
		body.Force,
		0,
		"",
		"published",
	)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, http.StatusConflict, protocol.ErrAlreadyExists, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}

	// Mark agent as published to marketplace in other_config (merge).
	var oc map[string]any
	if len(ag.OtherConfig) > 0 {
		_ = json.Unmarshal(ag.OtherConfig, &oc)
	}
	if oc == nil {
		oc = map[string]any{}
	}
	oc["marketplace"] = map[string]any{
		"published":    true,
		"package_id":   pkgID,
		"version":      pkg.Version,
		"published_at": time.Now().UTC().Format(time.RFC3339),
	}
	if err := store.ValidateV3Flags(oc); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}
	rawOC, err := marshalJSONRaw(oc)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	updates := map[string]any{"other_config": rawOC}
	if err := h.agents.Update(r.Context(), ag.ID, updates); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "agent", err.Error()))
		return
	}

	h.emitCacheInvalidate(bus.CacheKindAgent, ag.AgentKey)
	h.emitCacheInvalidate(bus.CacheKindBootstrap, ag.ID.String())

	emitAudit(h.msgBus, r, "agent.marketplace_published", "agent", ag.ID.String())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"package": result["package"],
	})
}

func sanitizeMarketplaceSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r == ' ' || r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			if r == ' ' {
				b.WriteRune('-')
			} else {
				b.WriteRune(r)
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "agent"
	}
	return strings.ToLower(out)
}
