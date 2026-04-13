package http

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const maxTeamMarketplaceInnerBytes = 32 << 20

// buildTeamMarketplaceZip builds a zip containing team-export.tar.gz (same payload as GET /v1/teams/{id}/export).
func (h *AgentsHandler) buildTeamMarketplaceZip(ctx context.Context, teamID uuid.UUID, teamMeta *pg.TeamExport) ([]byte, error) {
	var tgBuf bytes.Buffer
	lw := &limitedWriter{w: &tgBuf, limit: maxTeamMarketplaceInnerBytes}
	if err := h.writeTeamExportArchive(ctx, lw, teamID, teamMeta, nil, maxTeamMarketplaceInnerBytes); err != nil {
		return nil, err
	}
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	fw, err := zw.Create("team-export.tar.gz")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, bytes.NewReader(tgBuf.Bytes())); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if zipBuf.Len() > maxMarketplaceUploadSize {
		return nil, fmt.Errorf("team export exceeds marketplace size limit")
	}
	return zipBuf.Bytes(), nil
}

func (h *AgentsHandler) handleTeamMarketplacePublish(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())

	if h.marketplace == nil {
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "marketplace not configured"))
		return
	}
	if h.teamStore == nil {
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "team store not configured"))
		return
	}

	teamIDStr := r.PathValue("id")
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "team"))
		return
	}

	teamMeta, err := pg.ExportTeamByID(r.Context(), h.db, teamID)
	if err != nil || teamMeta == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "team", teamIDStr))
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
		pkgID = "team-" + teamID.String()
	}

	pkg := marketplace.Package{
		ID:          pkgID,
		Name:        strings.TrimSpace(body.Name),
		Type:        marketplace.TypeTeam,
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
	} else {
		pkg.Author = teamMeta.Name
	}

	zipData, err := h.buildTeamMarketplaceZip(r.Context(), teamID, teamMeta)
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

	var st map[string]any
	if len(teamMeta.Settings) > 0 {
		_ = json.Unmarshal(teamMeta.Settings, &st)
	}
	if st == nil {
		st = map[string]any{}
	}
	st["marketplace"] = map[string]any{
		"published":    true,
		"package_id":     pkgID,
		"version":        pkg.Version,
		"published_at":   time.Now().UTC().Format(time.RFC3339),
	}
	rawSettings, err := json.Marshal(st)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	if err := h.teamStore.UpdateTeam(r.Context(), teamID, map[string]any{"settings": json.RawMessage(rawSettings)}); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "team", err.Error()))
		return
	}

	h.emitCacheInvalidate(bus.CacheKindTeam, teamID.String())

	emitAudit(h.msgBus, r, "team.marketplace_published", "team", teamID.String())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"package": result["package"],
	})
}
