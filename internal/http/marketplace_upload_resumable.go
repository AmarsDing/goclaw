package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var uploadIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{8,128}$`)

type uploadMetadata struct {
	Package        marketplace.Package `json:"package"`
	Force          bool                `json:"force,omitempty"`
	ArtifactSHA256 string              `json:"artifact_sha256,omitempty"`
	ArtifactSize   int64               `json:"artifact_size,omitempty"`
	FileName       string              `json:"file_name,omitempty"`
}

type resumableMeta struct {
	UploadID   string         `json:"upload_id"`
	TotalParts int            `json:"total_parts"`
	Metadata   uploadMetadata `json:"metadata"`
}

func (h *MarketplaceHandler) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	const maxChunkSize = 8 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxChunkSize+(1<<20))
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file chunk is required"})
		return
	}
	defer file.Close()

	uploadID := strings.TrimSpace(r.FormValue("upload_id"))
	if !uploadIDRe.MatchString(uploadID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload_id"})
		return
	}
	partNumber, err := strconv.Atoi(r.FormValue("part_number"))
	if err != nil || partNumber < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid part_number"})
		return
	}
	totalParts, err := strconv.Atoi(r.FormValue("total_parts"))
	if err != nil || totalParts < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid total_parts"})
		return
	}

	metaPath := h.resumableMetaPath(uploadID)
	meta, hasMeta, err := h.loadResumableMeta(metaPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !hasMeta {
		metaRaw := strings.TrimSpace(r.FormValue("metadata"))
		if metaRaw == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "metadata is required for first chunk"})
			return
		}
		var parsed uploadMetadata
		if err := json.Unmarshal([]byte(metaRaw), &parsed); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid metadata"})
			return
		}
		if tid := store.TenantIDFromContext(r.Context()); tid != store.MasterTenantID {
			parsed.Package.TenantID = tid.String()
		}
		if parsed.Package.ID == "" || parsed.Package.Name == "" || parsed.Package.Version == "" || parsed.Package.Type == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package id, name, version, and type are required"})
			return
		}
		meta = resumableMeta{
			UploadID:   uploadID,
			TotalParts: totalParts,
			Metadata:   parsed,
		}
		if meta.Metadata.FileName == "" {
			meta.Metadata.FileName = "artifact.zip"
		}
		if err := h.saveResumableMeta(metaPath, meta); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else if meta.TotalParts != totalParts {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "total_parts mismatch"})
		return
	}

	partDir := h.resumablePartsDir(uploadID)
	if err := os.MkdirAll(partDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	partPath := filepath.Join(partDir, fmt.Sprintf("%06d.part", partNumber))
	out, err := os.Create(partPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer out.Close()
	n, err := io.Copy(out, file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"upload_id":    uploadID,
		"part_number":  partNumber,
		"total_parts":  totalParts,
		"received":     n,
		"parts_dir":    partDir,
		"metadata_set": true,
	})
}

func (h *MarketplaceHandler) handleUploadComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UploadID string `json:"upload_id"`
	}
	if !bindJSON(w, r, extractLocale(r), &req) {
		return
	}
	uploadID := strings.TrimSpace(req.UploadID)
	if !uploadIDRe.MatchString(uploadID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload_id"})
		return
	}

	metaPath := h.resumableMetaPath(uploadID)
	meta, hasMeta, err := h.loadResumableMeta(metaPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !hasMeta {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "upload session not found"})
		return
	}
	if meta.TotalParts < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload session has invalid total_parts"})
		return
	}
	if _, ok := h.catalog.Get(meta.Metadata.Package.ID); ok && !meta.Metadata.Force {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "package version already uploaded; use --force to overwrite review staging"})
		return
	}

	partDir := h.resumablePartsDir(uploadID)
	parts, err := os.ReadDir(partDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(parts) != meta.TotalParts {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("incomplete upload: have %d/%d parts", len(parts), meta.TotalParts)})
		return
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Name() < parts[j].Name() })

	stagingDir := filepath.Join(h.dataDir, "marketplace", "staging", meta.Metadata.Package.ID)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	fileName := sanitizeUploadName(meta.Metadata.FileName)
	if fileName == "" {
		fileName = "artifact.zip"
	}
	artifactPath := filepath.Join(stagingDir, fileName)
	out, err := os.Create(artifactPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer out.Close()

	hasher := sha256.New()
	written, err := h.mergeParts(io.MultiWriter(out, hasher), partDir, parts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	computedSHA := hex.EncodeToString(hasher.Sum(nil))
	if meta.Metadata.ArtifactSize > 0 && meta.Metadata.ArtifactSize != written {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifact size mismatch"})
		return
	}
	if meta.Metadata.ArtifactSHA256 != "" && !strings.EqualFold(strings.TrimSpace(meta.Metadata.ArtifactSHA256), computedSHA) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifact sha256 mismatch"})
		return
	}

	pkg := meta.Metadata.Package
	pkg.ArtifactSHA256 = computedSHA
	pkg.ArtifactSize = written
	if h.artifactStore != nil {
		key := filepath.ToSlash(filepath.Join("marketplace", "staging", pkg.ID, fileName))
		uri, err := h.artifactStore.UploadFile(r.Context(), key, artifactPath, written)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "object storage upload failed: " + err.Error()})
			return
		}
		pkg.ArtifactURI = uri
	}
	pkg.ReviewState = "pending_review"
	if err := h.catalog.Register(r.Context(), pkg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.persistCatalog(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = os.RemoveAll(h.resumableSessionDir(uploadID))

	writeJSON(w, http.StatusCreated, map[string]any{
		"upload_id":     uploadID,
		"package":       pkg,
		"artifact_path": artifactPath,
		"size":          written,
		"sha256":        computedSHA,
	})
}

func (h *MarketplaceHandler) resumableRootDir() string {
	return filepath.Join(h.dataDir, "marketplace", "staging", "_resumable")
}

func (h *MarketplaceHandler) resumableSessionDir(uploadID string) string {
	return filepath.Join(h.resumableRootDir(), uploadID)
}

func (h *MarketplaceHandler) resumableMetaPath(uploadID string) string {
	return filepath.Join(h.resumableSessionDir(uploadID), "metadata.json")
}

func (h *MarketplaceHandler) resumablePartsDir(uploadID string) string {
	return filepath.Join(h.resumableSessionDir(uploadID), "parts")
}

func (h *MarketplaceHandler) saveResumableMeta(path string, meta resumableMeta) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func (h *MarketplaceHandler) loadResumableMeta(path string) (resumableMeta, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return resumableMeta{}, false, nil
		}
		return resumableMeta{}, false, err
	}
	var meta resumableMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return resumableMeta{}, false, err
	}
	return meta, true, nil
}

func (h *MarketplaceHandler) mergeParts(dst io.Writer, partDir string, parts []os.DirEntry) (int64, error) {
	var written int64
	for _, part := range parts {
		if part.IsDir() || !strings.HasSuffix(part.Name(), ".part") {
			continue
		}
		f, err := os.Open(filepath.Join(partDir, part.Name()))
		if err != nil {
			return written, err
		}
		n, err := io.Copy(dst, f)
		_ = f.Close()
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}
