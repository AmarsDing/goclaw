package http

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MarketplaceHandler exposes marketplace catalog APIs.
type MarketplaceHandler struct {
	catalog     *marketplace.Catalog
	catalogPath string
	dataDir     string
	installer   *marketplace.Installer
}

// NewMarketplaceHandler creates a catalog-backed marketplace handler.
func NewMarketplaceHandler(dataDir string) *MarketplaceHandler {
	h := &MarketplaceHandler{
		catalog:     marketplace.NewCatalog(),
		catalogPath: filepath.Join(dataDir, "marketplace", "catalog.json"),
		dataDir:     dataDir,
	}
	_ = h.catalog.LoadFromFile(h.catalogPath) // start empty if no snapshot exists
	h.installer = marketplace.NewInstaller(dataDir, h.catalog, nil)
	return h
}

// RegisterRoutes registers marketplace endpoints.
func (h *MarketplaceHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/marketplace/packages", h.readAuth(h.handleList))
	mux.HandleFunc("POST /v1/marketplace/packages", h.adminAuth(h.handleUpsert))
	mux.HandleFunc("POST /v1/marketplace/packages/upload", h.adminAuth(h.handleUpload))
	mux.HandleFunc("GET /v1/marketplace/packages/{id}", h.readAuth(h.handleGet))
	mux.HandleFunc("GET /v1/marketplace/packages/{id}/reviews", h.readAuth(h.handleListReviews))
	mux.HandleFunc("GET /v1/marketplace/installed", h.readAuth(h.handleInstalled))
	mux.HandleFunc("PATCH /v1/marketplace/packages/{id}/review", h.adminAuth(h.handleModeration))
	mux.HandleFunc("POST /v1/marketplace/packages/{id}/review", h.readAuth(h.handleAddReview))
	mux.HandleFunc("POST /v1/marketplace/packages/{id}/like", h.readAuth(h.handleLike))
	mux.HandleFunc("POST /v1/marketplace/packages/{id}/install", h.readAuth(h.handleInstall))
}

func (h *MarketplaceHandler) readAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *MarketplaceHandler) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

func (h *MarketplaceHandler) handleList(w http.ResponseWriter, r *http.Request) {
	q := parseSearchQuery(r)
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
	if err := h.catalog.Register(r.Context(), pkg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.catalog.SaveToFile(h.catalogPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, pkg)
}

func (h *MarketplaceHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 32 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
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
		Package marketplace.Package `json:"package"`
		Force   bool                `json:"force,omitempty"`
	}
	if err := json.Unmarshal([]byte(metaRaw), &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid metadata"})
		return
	}
	if req.Package.ID == "" || req.Package.Name == "" || req.Package.Version == "" || req.Package.Type == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "package id, name, version, and type are required"})
		return
	}
	if _, ok := h.catalog.Get(req.Package.ID); ok && !req.Force {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "package version already uploaded; use --force to overwrite review staging"})
		return
	}

	stagingDir := filepath.Join(h.dataDir, "marketplace", "staging", req.Package.ID)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	fileName := sanitizeUploadName(header.Filename)
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
	size, err := io.Copy(out, file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	req.Package.ReviewState = "pending_review"
	if err := h.catalog.Register(r.Context(), req.Package); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.catalog.SaveToFile(h.catalogPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"package":       req.Package,
		"artifact_path": artifactPath,
		"size":          size,
	})
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
	if err := h.catalog.SetReviewState(id, body.State, body.Note); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.catalog.SaveToFile(h.catalogPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pkg, _ := h.catalog.Get(id)
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
	if err := h.catalog.SaveToFile(h.catalogPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pkg, _ := h.catalog.Get(packageID)
	writeJSON(w, http.StatusCreated, pkg)
}

func (h *MarketplaceHandler) handleListReviews(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if pkg, ok := h.catalog.Get(id); ok {
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
	if err := h.catalog.SaveToFile(h.catalogPath); err != nil {
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
	if !packageIsPublished(pkg) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "package is not published"})
		return
	}
	if packageRequiresEntitlement(pkg) && !requestHasMarketplaceEntitlement(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "package requires entitlement"})
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
	switch pkg.Pricing.Model {
	case "", marketplace.PricingFree:
		return false
	default:
		return true
	}
}

func packageIsPublished(pkg *marketplace.Package) bool {
	return pkg.ReviewState == "published"
}

func requestCanViewUnpublished(r *http.Request) bool {
	role := permissions.Role(store.RoleFromContext(r.Context()))
	return permissions.HasMinRole(role, permissions.RoleAdmin) || store.IsOwnerRole(r.Context())
}
