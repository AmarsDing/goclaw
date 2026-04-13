package http

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	skillUploadErrRead      = "read_skill_md"
	skillUploadErrEmpty     = "empty_skill_md"
	skillUploadErrNoName    = "no_name"
	skillUploadErrBadSlug   = "bad_slug"
	skillUploadErrSystem    = "system_slug"
	skillUploadErrDupBatch  = "dup_batch"
	skillUploadErrMkdir     = "mkdir"
	skillUploadErrCreate    = "create"
)

// uploadSkillError describes a failed single-skill upload (single ZIP or one entry in a bundle).
type uploadSkillError struct {
	Kind       string
	Slug       string
	Detail     string
	HTTPStatus int
}

func (e *uploadSkillError) Error() string {
	if e.Detail != "" {
		return e.Kind + ": " + e.Detail
	}
	return e.Kind
}

// uploadOneSkillFromZip extracts and registers one skill. batchSeen prevents duplicate slugs in a multi-skill ZIP (non-nil).
// Returns HTTP status: 200 for unchanged (identical SKILL.md hash), 201 for created/updated.
func (h *SkillsHandler) uploadOneSkillFromZip(
	ctx context.Context,
	r *http.Request,
	zr *zip.ReadCloser,
	root skillZipRoot,
	zipTotalBytes int64,
	batchSeen map[string]struct{},
) (map[string]any, int, *uploadSkillError) {
	userID := store.UserIDFromContext(r.Context())
	stripPrefix := root.stripPrefix

	skillContent, err := readZipFile(root.skillMD)
	if err != nil {
		return nil, 0, &uploadSkillError{Kind: skillUploadErrRead, HTTPStatus: http.StatusBadRequest}
	}
	if strings.TrimSpace(skillContent) == "" {
		return nil, 0, &uploadSkillError{Kind: skillUploadErrEmpty, HTTPStatus: http.StatusBadRequest}
	}

	skillHash := fmt.Sprintf("%x", sha256.Sum256([]byte(skillContent)))

	name, description, slug, frontmatter := skills.ParseSkillFrontmatter(skillContent)
	if name == "" {
		return nil, 0, &uploadSkillError{Kind: skillUploadErrNoName, HTTPStatus: http.StatusBadRequest}
	}
	if slug == "" {
		slug = skills.Slugify(name)
	}
	if !skills.SlugRegexp.MatchString(slug) {
		return nil, 0, &uploadSkillError{Kind: skillUploadErrBadSlug, Slug: slug, HTTPStatus: http.StatusBadRequest}
	}

	if batchSeen != nil {
		if _, dup := batchSeen[slug]; dup {
			return nil, 0, &uploadSkillError{Kind: skillUploadErrDupBatch, Detail: slug, HTTPStatus: http.StatusBadRequest}
		}
		batchSeen[slug] = struct{}{}
	}

	if h.skills.IsSystemSkill(slug) {
		return nil, 0, &uploadSkillError{Kind: skillUploadErrSystem, Slug: slug, HTTPStatus: http.StatusConflict}
	}

	tenantSkillsBase := h.tenantSkillsDir(r)
	uploadLock := h.skillUploadLock(filepath.Join(tenantSkillsBase, slug))
	uploadLock.Lock()
	defer uploadLock.Unlock()

	// Idempotency: identical SKILL.md content → same hash as stored (not ZIP bytes).
	existingHash, existingVer, skillExists := h.skills.GetSkillHashBySlug(r.Context(), slug)
	if skillExists && existingHash != "" && existingHash == skillHash {
		return map[string]any{
			"slug":    slug,
			"version": existingVer,
			"name":    name,
			"status":  "unchanged",
		}, http.StatusOK, nil
	}

	isNew := !skillExists

	version := h.skills.GetNextVersion(r.Context(), slug)

	destDir := filepath.Join(tenantSkillsBase, slug, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, 0, &uploadSkillError{Kind: skillUploadErrMkdir, Detail: err.Error(), HTTPStatus: http.StatusInternalServerError}
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		entryName := normZipEntryName(f.Name)
		if stripPrefix != "" {
			if !strings.HasPrefix(entryName, stripPrefix) {
				continue
			}
			entryName = strings.TrimPrefix(entryName, stripPrefix)
			if entryName == "" {
				continue
			}
		}
		if skills.IsSystemArtifact(entryName) {
			continue
		}
		cleanName := filepath.Clean(entryName)
		if strings.Contains(cleanName, "..") {
			continue
		}
		destPath := filepath.Join(destDir, cleanName)
		if !strings.HasPrefix(destPath, destDir+string(filepath.Separator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			continue
		}
		data, err := readZipFile(f)
		if err != nil {
			continue
		}
		_ = os.WriteFile(destPath, []byte(data), 0644)
	}

	skillSize := zipSkillByteSize(zr.File, stripPrefix)
	if skillSize == 0 {
		skillSize = zipTotalBytes
	}

	desc := description
	skill := store.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     userID,
		Visibility:  "internal",
		Version:     version,
		FilePath:    destDir,
		FileSize:    skillSize,
		FileHash:    &skillHash,
		Frontmatter: frontmatter,
	}

	response := map[string]any{"slug": slug, "version": version, "name": name, "status": "active", "is_new": isNew}
	depState := uploadSkillDepState{}
	depsCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), uploadDepsInstallTimeout)
	defer cancel()

	manifest := skills.ScanSkillDeps(destDir)
	if manifest != nil && !manifest.IsEmpty() {
		if ok, missing := checkUploadedSkillDeps(manifest); !ok {
			depState = h.reconcileUploadedSkillDeps(
				depsCtx,
				slug,
				manifest,
				missing,
				canAutoInstallUploadedSkillDeps(ctx),
			)
			skill.Status = depState.status
			skill.MissingDeps = depState.missing
			for key, value := range depState.response {
				response[key] = value
			}
		}
	}

	id, err := h.skills.CreateSkillManaged(depsCtx, skill)
	if err != nil {
		return nil, 0, &uploadSkillError{Kind: skillUploadErrCreate, Detail: err.Error(), HTTPStatus: http.StatusInternalServerError}
	}
	response["id"] = id.String()

	h.skills.BumpVersion()
	emitAudit(h.msgBus, r, "skill.uploaded", "skill", slug)
	slog.Info("skill uploaded", "id", id, "slug", slug, "version", version, "size", skillSize, "status", skill.Status)
	depState.emit(h, slug)

	return response, http.StatusCreated, nil
}

func skillUploadErrMsg(locale string, e *uploadSkillError) string {
	switch e.Kind {
	case skillUploadErrRead:
		return i18n.T(locale, i18n.MsgInvalidRequest, "failed to read SKILL.md")
	case skillUploadErrEmpty:
		return i18n.T(locale, i18n.MsgInvalidRequest, "SKILL.md is empty")
	case skillUploadErrNoName:
		return i18n.T(locale, i18n.MsgRequired, "name in SKILL.md frontmatter")
	case skillUploadErrBadSlug:
		return i18n.T(locale, i18n.MsgInvalidSlug, "slug")
	case skillUploadErrSystem:
		return i18n.T(locale, i18n.MsgInvalidRequest, "slug conflicts with a system skill")
	case skillUploadErrDupBatch:
		return i18n.T(locale, i18n.MsgInvalidRequest, "duplicate slug in ZIP bundle: "+e.Detail)
	case skillUploadErrMkdir:
		return i18n.T(locale, i18n.MsgInternalError, "failed to create skill directory")
	case skillUploadErrCreate:
		return i18n.T(locale, i18n.MsgFailedToCreate, "skill", e.Detail)
	default:
		return i18n.T(locale, i18n.MsgInvalidRequest, e.Detail)
	}
}
