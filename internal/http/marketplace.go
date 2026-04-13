package http

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MarketplaceHandler exposes marketplace catalog APIs.
type MarketplaceHandler struct {
	catalog         *marketplace.Catalog
	catalogPath     string
	dataDir         string
	installer       *marketplace.Installer
	artifactStore   marketplaceArtifactStore
	db              *sql.DB
}

// MarketplaceHandlerOptions configures catalog persistence and integrations.
type MarketplaceHandlerOptions struct {
	DataDir string
	DB      *sql.DB
}

// NewMarketplaceHandler creates a catalog-backed marketplace handler (JSON snapshot under dataDir).
func NewMarketplaceHandler(dataDir string) *MarketplaceHandler {
	return NewMarketplaceHandlerWithOptions(MarketplaceHandlerOptions{DataDir: dataDir})
}

// NewMarketplaceHandlerWithOptions loads catalog from PostgreSQL when DB is set (falls back to file / migrates file → PG).
func NewMarketplaceHandlerWithOptions(opts MarketplaceHandlerOptions) *MarketplaceHandler {
	h := &MarketplaceHandler{
		catalog:     marketplace.NewCatalog(),
		catalogPath: filepath.Join(opts.DataDir, "marketplace", "catalog.json"),
		dataDir:     opts.DataDir,
		db:          opts.DB,
	}
	h.loadCatalogAtStartup()
	h.installer = marketplace.NewInstaller(opts.DataDir, h.catalog, nil)
	if store, err := newMarketplaceArtifactStoreFromEnv(); err != nil {
		slog.Warn("marketplace: object store disabled", "error", err)
	} else {
		h.artifactStore = store
	}
	return h
}

func (h *MarketplaceHandler) loadCatalogAtStartup() {
	if h.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := marketplace.LoadCatalogFromDB(ctx, h.db, h.catalog); err != nil {
			slog.Warn("marketplace: PG catalog load failed, trying file snapshot", "error", err)
		}
		if h.catalog.PackageCount() == 0 {
			if err := h.catalog.LoadFromFile(h.catalogPath); err == nil && h.catalog.PackageCount() > 0 {
				if err := marketplace.SaveCatalogSnapshotToDB(ctx, h.db, h.catalog); err != nil {
					slog.Warn("marketplace: could not migrate file catalog to PG", "error", err)
				}
			}
		}
		return
	}
	if err := h.catalog.LoadFromFile(h.catalogPath); err != nil {
		slog.Warn("marketplace: failed to load catalog from file", "path", h.catalogPath, "error", err)
	}
}

func (h *MarketplaceHandler) persistCatalog(ctx context.Context) error {
	if h.db != nil {
		if err := marketplace.SaveCatalogSnapshotToDB(ctx, h.db, h.catalog); err != nil {
			return err
		}
	}
	return h.catalog.SaveToFile(h.catalogPath)
}

// RegisterRoutes registers marketplace endpoints.
func (h *MarketplaceHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/marketplace/packages", h.readAuth(h.handleList))
	mux.HandleFunc("POST /v1/marketplace/packages", h.adminAuth(h.handleUpsert))
	mux.HandleFunc("POST /v1/marketplace/packages/upload", h.uploadAuth(h.handleUpload))
	mux.HandleFunc("POST /v1/marketplace/packages/upload/chunk", h.uploadAuth(h.handleUploadChunk))
	mux.HandleFunc("POST /v1/marketplace/packages/upload/complete", h.uploadAuth(h.handleUploadComplete))
	mux.HandleFunc("GET /v1/marketplace/catalog-export", h.readAuth(h.handleCatalogExport))
	mux.HandleFunc("GET /v1/marketplace/audit", h.adminAuth(h.handleMarketplaceAudit))
	mux.HandleFunc("GET /v1/marketplace/packages/{id}", h.readAuth(h.handleGet))
	mux.HandleFunc("GET /v1/marketplace/packages/{id}/reviews", h.readAuth(h.handleListReviews))
	mux.HandleFunc("GET /v1/marketplace/installed", h.readAuth(h.handleInstalled))
	mux.HandleFunc("PATCH /v1/marketplace/packages/{id}/review", h.adminAuth(h.handleModeration))
	mux.HandleFunc("POST /v1/marketplace/packages/{id}/review", h.readAuth(h.handleAddReview))
	mux.HandleFunc("POST /v1/marketplace/packages/{id}/like", h.readAuth(h.handleLike))
	mux.HandleFunc("POST /v1/marketplace/packages/{id}/install", h.readAuth(h.handleInstall))
	mux.HandleFunc("POST /v1/marketplace/packages/{id}/trial", h.readAuth(h.handleTrial))
	mux.HandleFunc("POST /v1/marketplace/packages/{id}/purchase", h.readAuth(h.handlePurchase))
	mux.HandleFunc("POST /v1/marketplace/webhooks/stripe", h.handleStripeWebhook)
}

func (h *MarketplaceHandler) readAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *MarketplaceHandler) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

// uploadAuth requires admin unless GOCLAW_MARKETPLACE_ALLOW_TENANT_PUBLISH=true (then Operator+).
func (h *MarketplaceHandler) uploadAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(MarketplacePublishMinRole(), next)
}

func (h *MarketplaceHandler) handleList(w http.ResponseWriter, r *http.Request) {
	q := parseSearchQuery(r)
	if !store.IsMasterScope(r.Context()) {
		if tid := store.TenantIDFromContext(r.Context()); tid != store.MasterTenantID {
			q.TenantID = tid.String()
		}
	}
	if q.ReviewState == "" && !requestCanViewUnpublished(r) {
		q.ReviewState = "published"
	}
	writeJSON(w, http.StatusOK, h.catalog.Search(q))
}

func (h *MarketplaceHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	pkg, ok := h.catalog.Get(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
		return
	}
	if !packageVisibleToTenant(r, pkg) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
		return
	}
	if !packageIsPublished(pkg) && !requestCanViewUnpublished(r) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
		return
	}
	writeJSON(w, http.StatusOK, pkg)
}

func (h *MarketplaceHandler) handleUpsert(w http.ResponseWriter, r *http.Request) {
	var pkg marketplace.Package
	if !bindJSON(w, r, extractLocale(r), &pkg) {
		return
	}
	if tid := store.TenantIDFromContext(r.Context()); tid != store.MasterTenantID {
		pkg.TenantID = tid.String()
	}
	if err := h.catalog.Register(r.Context(), pkg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.persistCatalog(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, pkg)
}

const maxMarketplaceUploadSize = 32 << 20

// MarketplacePublishMinRole returns the minimum role required to publish packages (upload / agent publish).
// It mirrors uploadAuth: Admin unless GOCLAW_MARKETPLACE_ALLOW_TENANT_PUBLISH=true (then Operator+).
func MarketplacePublishMinRole() permissions.Role {
	min := permissions.RoleAdmin
	if strings.EqualFold(os.Getenv("GOCLAW_MARKETPLACE_ALLOW_TENANT_PUBLISH"), "true") {
		min = permissions.RoleOperator
	}
	return min
}

// IngestZipArtifact mirrors POST /v1/marketplace/packages/upload: writes the zip to staging,
// registers the package, and persists the catalog. reviewState is set on the package
// (e.g. "pending_review" for moderated uploads, "published" for self-service agent publish).
func (h *MarketplaceHandler) IngestZipArtifact(ctx context.Context, pkg marketplace.Package, src io.Reader, filename string, force bool, expectedSize int64, expectedSHA256 string, reviewState string) (map[string]any, error) {
	if pkg.ID == "" || pkg.Name == "" || pkg.Version == "" || pkg.Type == "" {
		return nil, fmt.Errorf("package id, name, version, and type are required")
	}
	if tid := store.TenantIDFromContext(ctx); tid != store.MasterTenantID {
		pkg.TenantID = tid.String()
	}
	if _, ok := h.catalog.Get(pkg.ID); ok && !force {
		return nil, fmt.Errorf("package already exists; use force to overwrite")
	}

	stagingDir := filepath.Join(h.dataDir, "marketplace", "staging", pkg.ID)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, err
	}
	fileName := sanitizeUploadName(filename)
	if fileName == "" {
		fileName = "artifact.zip"
	}
	artifactPath := filepath.Join(stagingDir, fileName)
	out, err := os.Create(artifactPath)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	limited := io.LimitReader(src, maxMarketplaceUploadSize+1)
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(out, hasher), limited)
	if err != nil {
		return nil, err
	}
	if size > maxMarketplaceUploadSize {
		return nil, fmt.Errorf("artifact exceeds size limit")
	}
	computedSHA := hex.EncodeToString(hasher.Sum(nil))
	if expectedSize > 0 && expectedSize != size {
		return nil, fmt.Errorf("artifact size mismatch")
	}
	if expectedSHA256 != "" && !strings.EqualFold(strings.TrimSpace(expectedSHA256), computedSHA) {
		return nil, fmt.Errorf("artifact sha256 mismatch")
	}

	pkg.ArtifactSHA256 = computedSHA
	pkg.ArtifactSize = size
	if h.artifactStore != nil {
		key := filepath.ToSlash(filepath.Join("marketplace", "staging", pkg.ID, fileName))
		uri, err := h.artifactStore.UploadFile(ctx, key, artifactPath, size)
		if err != nil {
			return nil, fmt.Errorf("object storage upload failed: %w", err)
		}
		pkg.ArtifactURI = uri
	}

	pkg.ReviewState = reviewState
	if err := h.catalog.Register(ctx, pkg); err != nil {
		return nil, err
	}
	if err := h.persistCatalog(ctx); err != nil {
		return nil, err
	}
	return map[string]any{
		"package":       pkg,
		"artifact_path": artifactPath,
		"size":          size,
		"sha256":        computedSHA,
	}, nil
}

func (h *MarketplaceHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMarketplaceUploadSize)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file is required"})
		return
	}
	defer file.Close()

	metaRaw := r.FormValue("metadata")
	if metaRaw == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "metadata is required"})
		return
	}
	var req struct {
		Package        marketplace.Package `json:"package"`
		Force          bool                `json:"force,omitempty"`
		ArtifactSHA256 string              `json:"artifact_sha256,omitempty"`
		ArtifactSize   int64               `json:"artifact_size,omitempty"`
	}
	if err := json.Unmarshal([]byte(metaRaw), &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid metadata"})
		return
	}
	if req.Package.ID == "" || req.Package.Name == "" || req.Package.Version == "" || req.Package.Type == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package id, name, version, and type are required"})
		return
	}

	result, err := h.IngestZipArtifact(r.Context(), req.Package, file, header.Filename, req.Force, req.ArtifactSize, req.ArtifactSHA256, "pending_review")
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "package version already uploaded; use --force to overwrite review staging"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *MarketplaceHandler) handleModeration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		State string `json:"state"`
		Note  string `json:"note"`
	}
	if !bindJSON(w, r, extractLocale(r), &body) {
		return
	}
	prev, _ := h.catalog.Get(id)
	oldState := ""
	if prev != nil {
		oldState = prev.ReviewState
	}
	if err := h.catalog.SetReviewState(id, body.State, body.Note); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.persistCatalog(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pkg, _ := h.catalog.Get(id)
	if h.db != nil && pkg != nil {
		tid := ""
		if pkg.TenantID != "" {
			tid = pkg.TenantID
		}
		_ = marketplace.InsertAuditEvent(r.Context(), h.db, id, tid, store.UserIDFromContext(r.Context()),
			"review_state_change", oldState, body.State, body.Note, map[string]any{
				"package_name": pkg.Name,
				"version":      pkg.Version,
			})
	}
	h.notifyReviewWebhook(r.Context(), pkg, oldState, body.State, body.Note)
	writeJSON(w, http.StatusOK, pkg)
}

func (h *MarketplaceHandler) handleAddReview(w http.ResponseWriter, r *http.Request) {
	packageID := r.PathValue("id")
	var body struct {
		UserID  string `json:"user_id"`
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if !bindJSON(w, r, extractLocale(r), &body) {
		return
	}
	userID := body.UserID
	if userID == "" {
		userID = store.UserIDFromContext(r.Context())
	}
	if userID == "" {
		userID = "anonymous"
	}
	err := h.catalog.AddReview(r.Context(), marketplace.Review{
		PackageID: packageID,
		UserID:    userID,
		Rating:    body.Rating,
		Comment:   body.Comment,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.persistCatalog(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pkg, _ := h.catalog.Get(packageID)
	writeJSON(w, http.StatusCreated, pkg)
}

func (h *MarketplaceHandler) handleListReviews(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if pkg, ok := h.catalog.Get(id); ok {
		if !packageVisibleToTenant(r, pkg) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
			return
		}
		if !packageIsPublished(pkg) && !requestCanViewUnpublished(r) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
			return
		}
	}
	reviews, err := h.catalog.ListReviews(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": reviews})
}

func (h *MarketplaceHandler) handleLike(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.catalog.IncrementLikes(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.persistCatalog(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pkg, _ := h.catalog.Get(id)
	writeJSON(w, http.StatusCreated, pkg)
}

func (h *MarketplaceHandler) handleInstalled(w http.ResponseWriter, r *http.Request) {
	results, err := h.installer.ListInstalled()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"packages": results})
}

func (h *MarketplaceHandler) handleInstall(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pkg, ok := h.catalog.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
		return
	}
	if err := marketplace.ValidatePricingModel(pkg.Pricing); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !packageVisibleToTenant(r, pkg) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "package not found"})
		return
	}
	if !packageIsPublished(pkg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "package is not published"})
		return
	}
	if packageRequiresEntitlement(pkg) && !h.installEntitled(r, pkg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "package requires entitlement", "code": "MARKETPLACE_ENTITLEMENT_REQUIRED"})
		return
	}
	result, err := h.installer.Install(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func parseSearchQuery(r *http.Request) marketplace.SearchQuery {
	query := r.URL.Query()
	q := marketplace.SearchQuery{
		Query:  query.Get("query"),
		Author: query.Get("author"),
		SortBy: query.Get("sort_by"),
	}
	if pt := query.Get("type"); pt != "" {
		q.Type = marketplace.PackageType(pt)
	}
	if reviewState := query.Get("review_state"); reviewState != "" {
		q.ReviewState = reviewState
	}
	if minRating, err := strconv.ParseFloat(query.Get("min_rating"), 64); err == nil {
		q.MinRating = minRating
	}
	if free, err := strconv.ParseBool(query.Get("free")); err == nil {
		q.Free = free
	}
	if limit, err := strconv.Atoi(query.Get("limit")); err == nil {
		q.Limit = limit
	}
	if offset, err := strconv.Atoi(query.Get("offset")); err == nil {
		q.Offset = offset
	}
	return q
}

func sanitizeUploadName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" {
		return ""
	}
	return strings.ReplaceAll(name, "..", "")
}

func requestHasMarketplaceEntitlement(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("X-Marketplace-Entitled"), "true")
}

func packageRequiresEntitlement(pkg *marketplace.Package) bool {
	return marketplace.PackageRequiresEntitlement(pkg)
}

func packageIsPublished(pkg *marketplace.Package) bool {
	return pkg.ReviewState == "published"
}

func requestCanViewUnpublished(r *http.Request) bool {
	role := permissions.Role(store.RoleFromContext(r.Context()))
	return permissions.HasMinRole(role, permissions.RoleAdmin) || store.IsOwnerRole(r.Context())
}

func packageVisibleToTenant(r *http.Request, pkg *marketplace.Package) bool {
	if pkg == nil {
		return false
	}
	if store.IsMasterScope(r.Context()) {
		return true
	}
	if pkg.TenantID == "" {
		return true
	}
	tid := store.TenantIDFromContext(r.Context())
	if tid == store.MasterTenantID || tid == uuid.Nil {
		return true
	}
	return pkg.TenantID == tid.String()
}
