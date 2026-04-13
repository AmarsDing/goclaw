package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *MarketplaceHandler) installEntitled(r *http.Request, pkg *marketplace.Package) bool {
	if requestHasMarketplaceEntitlement(r) {
		return true
	}
	if !packageRequiresEntitlement(pkg) {
		return true
	}
	if h.db == nil {
		return false
	}
	tid := store.TenantIDFromContext(r.Context()).String()
	uid := store.UserIDFromContext(r.Context())
	if uid == "" {
		uid = "anonymous"
	}
	ok, err := marketplace.HasEntitlement(r.Context(), h.db, tid, uid, pkg.ID)
	if err != nil {
		slog.Warn("marketplace: entitlement check failed", "error", err)
		return false
	}
	return ok
}

func (h *MarketplaceHandler) handleCatalogExport(w http.ResponseWriter, r *http.Request) {
	snap := h.catalog.Snapshot()
	writeJSON(w, http.StatusOK, snap)
}

func (h *MarketplaceHandler) handleMarketplaceAudit(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "audit log requires PostgreSQL"})
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, package_id, tenant_id, actor_user_id, action, old_state, new_state, note, detail, created_at
		FROM marketplace_audit_events
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var pkgID, tenantID, actor, action string
		var oldS, newS, note sql.NullString
		var detail []byte
		var created time.Time
		if err := rows.Scan(&id, &pkgID, &tenantID, &actor, &action, &oldS, &newS, &note, &detail, &created); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		ev := map[string]any{
			"id":         id,
			"package_id": pkgID,
			"tenant_id":  tenantID,
			"actor":      actor,
			"action":     action,
			"created_at": created.UTC().Format(time.RFC3339),
		}
		if oldS.Valid {
			ev["old_state"] = oldS.String
		}
		if newS.Valid {
			ev["new_state"] = newS.String
		}
		if note.Valid {
			ev["note"] = note.String
		}
		if len(detail) > 0 {
			var m map[string]any
			if json.Unmarshal(detail, &m) == nil {
				ev["detail"] = m
			}
		}
		out = append(out, ev)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func (h *MarketplaceHandler) handleTrial(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "trial requires PostgreSQL-backed entitlements"})
		return
	}
	id := r.PathValue("id")
	pkg, ok := h.catalog.Get(id)
	if !ok || !packageIsPublished(pkg) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
		return
	}
	if pkg.Pricing.TrialDays <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package does not offer a trial"})
		return
	}
	tid := store.TenantIDFromContext(r.Context()).String()
	uid := store.UserIDFromContext(r.Context())
	if uid == "" {
		uid = "anonymous"
	}
	until := time.Now().UTC().AddDate(0, 0, pkg.Pricing.TrialDays)
	if err := marketplace.GrantEntitlement(r.Context(), h.db, tid, uid, id, "trial", &until); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entitled":    true,
		"source":      "trial",
		"active_until": until.UTC().Format(time.RFC3339),
	})
}

func (h *MarketplaceHandler) handlePurchase(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "purchase recording requires PostgreSQL"})
		return
	}
	id := r.PathValue("id")
	pkg, ok := h.catalog.Get(id)
	if !ok || !packageIsPublished(pkg) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
		return
	}
	if !packageRequiresEntitlement(pkg) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package does not require purchase"})
		return
	}
	// Manual entitlement: shared secret header, or dev self-grant when GOCLAW_MARKETPLACE_DEV_GRANT_PURCHASE=true.
	secret := os.Getenv("GOCLAW_MARKETPLACE_MANUAL_PURCHASE_SECRET")
	if secret != "" {
		if r.Header.Get("X-Marketplace-Purchase-Secret") != secret {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid purchase secret"})
			return
		}
	} else if os.Getenv("GOCLAW_MARKETPLACE_DEV_GRANT_PURCHASE") != "true" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "purchase endpoint disabled (set GOCLAW_MARKETPLACE_DEV_GRANT_PURCHASE or MANUAL_PURCHASE_SECRET)"})
		return
	}
	tid := store.TenantIDFromContext(r.Context()).String()
	uid := store.UserIDFromContext(r.Context())
	if uid == "" {
		uid = "anonymous"
	}
	if err := marketplace.GrantEntitlement(r.Context(), h.db, tid, uid, id, "manual", nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entitled": true, "source": "manual"})
}

func (h *MarketplaceHandler) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("GOCLAW_MARKETPLACE_WEBHOOK_STRIPE_SECRET")
	if secret == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "stripe webhook not configured"})
		return
	}
	if r.Header.Get("X-Goclaw-Webhook-Secret") != secret {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid webhook secret"})
		return
	}
	if h.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database required"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body"})
		return
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	typ, _ := raw["type"].(string)
	if typ != "checkout.session.completed" {
		writeJSON(w, http.StatusOK, map[string]any{"received": true, "ignored": typ})
		return
	}
	data, _ := raw["data"].(map[string]any)
	obj, _ := data["object"].(map[string]any)
	meta, _ := obj["metadata"].(map[string]any)
	pkgID, _ := meta["package_id"].(string)
	tenantID, _ := meta["tenant_id"].(string)
	userID, _ := meta["user_id"].(string)
	if pkgID == "" || tenantID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing metadata.package_id or tenant_id"})
		return
	}
	if userID == "" {
		userID = "stripe"
	}
	if err := marketplace.GrantEntitlement(r.Context(), h.db, tenantID, userID, pkgID, "stripe", nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *MarketplaceHandler) notifyReviewWebhook(ctx context.Context, pkg *marketplace.Package, oldState, newState, note string) {
	url := strings.TrimSpace(os.Getenv("GOCLAW_MARKETPLACE_REVIEW_WEBHOOK_URL"))
	if url == "" || pkg == nil {
		return
	}
	payload := map[string]any{
		"package_id": pkg.ID,
		"name":       pkg.Name,
		"version":    pkg.Version,
		"tenant_id":  pkg.TenantID,
		"old_state":  oldState,
		"new_state":  newState,
		"note":       note,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("marketplace: webhook marshal failed", "error", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(b)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if sig := os.Getenv("GOCLAW_MARKETPLACE_REVIEW_WEBHOOK_SECRET"); sig != "" {
		req.Header.Set("X-Goclaw-Signature", sig)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("marketplace: review webhook request failed", "error", err)
		return
	}
	_ = resp.Body.Close()
}
