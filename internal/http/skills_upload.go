package http

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const uploadDepsInstallTimeout = 5 * time.Minute

var (
	installUploadedSkillDeps = skills.InstallDeps
	checkUploadedSkillDeps   = skills.CheckSkillDeps
)

// handleUpload processes a ZIP file upload containing one or more skills (SKILL.md at root,
// foo/SKILL.md, or wrapper/foo/SKILL.md for multiple skills).
func (h *SkillsHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDHeader)})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSkillUploadSize)

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "file is required: "+err.Error())})
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "skill-upload-*.zip")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create temp file")})
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to save upload")})
		return
	}
	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))

	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "invalid ZIP file")})
		return
	}
	defer zr.Close()

	roots, layoutErr := discoverSkillZipRoots(zr.File)
	if layoutErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, layoutErr)})
		return
	}

	ctx := r.Context()
	depsCtx := context.WithoutCancel(ctx)

	if len(roots) == 1 {
		resp, upErr := h.uploadOneSkillFromZip(depsCtx, r, zr, roots[0], fileHash, size, nil)
		if upErr != nil {
			writeJSON(w, upErr.HTTPStatus, map[string]string{"error": skillUploadErrMsg(locale, upErr)})
			return
		}
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	batchSeen := make(map[string]struct{})
	var okSkills []map[string]any
	var failed []map[string]string
	for _, root := range roots {
		resp, upErr := h.uploadOneSkillFromZip(depsCtx, r, zr, root, fileHash, size, batchSeen)
		if upErr != nil {
			slug := upErr.Slug
			if slug == "" {
				slug = strings.TrimSuffix(strings.TrimSuffix(root.stripPrefix, "/"), "/")
				if i := strings.LastIndex(slug, "/"); i >= 0 {
					slug = slug[i+1:]
				}
			}
			failed = append(failed, map[string]string{
				"slug":  slug,
				"error": skillUploadErrMsg(locale, upErr),
			})
			continue
		}
		okSkills = append(okSkills, resp)
	}

	if len(okSkills) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  i18n.T(locale, i18n.MsgInvalidRequest, "no skills could be imported from the ZIP"),
			"multi":  true,
			"failed": failed,
		})
		return
	}

	out := map[string]any{
		"multi":  true,
		"skills": okSkills,
	}
	if len(failed) > 0 {
		out["failed"] = failed
	}
	slog.Info("multi skill upload", "imported", len(okSkills), "failed", len(failed), "zip_bytes", header.Size)
	writeJSON(w, http.StatusCreated, out)
}

func canAutoInstallUploadedSkillDeps(ctx context.Context) bool {
	return store.IsOwnerRole(ctx) || store.TenantIDFromContext(ctx) == store.MasterTenantID
}

func uploadDepErrors(result *skills.InstallResult, installErr error) []string {
	var errors []string
	if installErr != nil {
		errors = append(errors, installErr.Error())
	}
	if result != nil && len(result.Errors) > 0 {
		errors = append(errors, result.Errors...)
	}
	return errors
}

func (h *SkillsHandler) emitUploadDepInstalling(slug string, count int) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventSkillDepsInstalling,
		Payload: map[string]any{"skill": slug, "count": count},
	})
}

func (h *SkillsHandler) emitUploadDepChecked(slug, status string, missing []string) {
	if h.msgBus == nil {
		return
	}
	payload := map[string]any{
		"slug":   slug,
		"status": status,
	}
	if len(missing) > 0 {
		payload["missing"] = missing
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventSkillDepsChecked,
		Payload: payload,
	})
}

func (h *SkillsHandler) emitUploadDepInstalled(slug string, result *skills.InstallResult) {
	if h.msgBus == nil {
		return
	}
	payload := map[string]any{"skill": slug}
	if result != nil {
		payload["result"] = result
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventSkillDepsInstalled,
		Payload: payload,
	})
}

func (h *SkillsHandler) reconcileUploadedSkillDeps(
	ctx context.Context,
	slug string,
	manifest *skills.SkillManifest,
	missing []string,
	allowAutoInstall bool,
) uploadSkillDepState {
	response := map[string]any{}
	finalStatus := "archived"
	finalMissing := append([]string(nil), missing...)
	state := uploadSkillDepState{installCount: len(missing), checked: true}
	var installResult *skills.InstallResult
	var installErr error

	if allowAutoInstall {
		installResult, installErr = installUploadedSkillDeps(ctx, manifest, missing)
		if ok, checkedMissing := checkUploadedSkillDeps(manifest); ok {
			finalStatus = "active"
			finalMissing = nil
			response["deps_installed"] = true
			slog.Info("skill deps auto-installed", "skill", slug, "installed", missing)
		} else {
			finalMissing = checkedMissing
			slog.Warn("skill deps auto-install failed", "skill", slug, "missing", finalMissing, "errors", uploadDepErrors(installResult, installErr))
		}
		state.installResult = installResult
	} else {
		response["deps_warning"] = "missing dependencies: " + skills.FormatMissing(finalMissing)
		state.installCount = 0
	}

	if finalStatus == "archived" {
		if _, exists := response["deps_warning"]; !exists {
			response["deps_warning"] = "auto-install failed for: " + skills.FormatMissing(finalMissing)
		}
		response["missing_deps"] = finalMissing
		if errors := uploadDepErrors(installResult, installErr); len(errors) > 0 {
			response["deps_errors"] = errors
		}
	}
	response["status"] = finalStatus
	state.status = finalStatus
	state.missing = finalMissing
	state.response = response
	return state
}

type uploadSkillDepState struct {
	status        string
	missing       []string
	response      map[string]any
	installCount  int
	installResult *skills.InstallResult
	checked       bool
}

func (s uploadSkillDepState) emit(h *SkillsHandler, slug string) {
	if !s.checked {
		return
	}
	if s.installCount > 0 {
		h.emitUploadDepInstalling(slug, s.installCount)
		h.emitUploadDepInstalled(slug, s.installResult)
	}
	h.emitUploadDepChecked(slug, s.status, s.missing)
}
