package http

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestMarketplaceHandler_UpsertThenList(t *testing.T) {
	t.Parallel()
	h := NewMarketplaceHandler(t.TempDir())

	body := map[string]any{
		"id":           "pkg-1",
		"name":         "demo-skill",
		"type":         "skill",
		"description":  "demo package",
		"author":       "alice",
		"version":      "1.0.0",
		"ref":          "v1.0.0",
		"review_state": "published",
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/marketplace/packages", bytes.NewReader(raw)).WithContext(context.Background())
	rec := httptest.NewRecorder()
	h.handleUpsert(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/marketplace/packages?query=demo&type=skill", nil)
	listRec := httptest.NewRecorder()
	h.handleList(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRec.Code, http.StatusOK)
	}
	var result marketplace.SearchResult
	if err := json.NewDecoder(listRec.Body).Decode(&result); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(result.Packages) != 1 {
		t.Fatalf("packages len = %d, want 1", len(result.Packages))
	}
	if result.Packages[0].Ref != "v1.0.0" {
		t.Fatalf("package ref = %q, want v1.0.0", result.Packages[0].Ref)
	}
}

func TestMarketplaceHandler_LoadsSnapshotOnStartup(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "marketplace", "catalog.json")

	c := marketplace.NewCatalog()
	if err := c.Register(context.Background(), marketplace.Package{
		ID:          "pkg-2",
		Name:        "persisted",
		Type:        marketplace.TypePlugin,
		Version:     "2.0.0",
		ReviewState: "published",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := c.SaveToFile(path); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	h := NewMarketplaceHandler(dataDir)
	req := httptest.NewRequest(http.MethodGet, "/v1/marketplace/packages/pkg-2", nil)
	req.SetPathValue("id", "pkg-2")
	rec := httptest.NewRecorder()
	h.handleGet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMarketplaceHandler_ModerationReviewAndLike(t *testing.T) {
	t.Parallel()
	h := NewMarketplaceHandler(t.TempDir())

	upsert := map[string]any{
		"id":      "pkg-3",
		"name":    "review-target",
		"type":    "plugin",
		"version": "0.1.0",
	}
	rawUpsert, _ := json.Marshal(upsert)
	upsertReq := httptest.NewRequest(http.MethodPost, "/v1/marketplace/packages", bytes.NewReader(rawUpsert))
	upsertRec := httptest.NewRecorder()
	h.handleUpsert(upsertRec, upsertReq)
	if upsertRec.Code != http.StatusCreated {
		t.Fatalf("upsert status = %d, want %d", upsertRec.Code, http.StatusCreated)
	}

	modBody, _ := json.Marshal(map[string]any{"state": "published", "note": "approved"})
	modReq := httptest.NewRequest(http.MethodPatch, "/v1/marketplace/packages/pkg-3/review", bytes.NewReader(modBody))
	modReq.SetPathValue("id", "pkg-3")
	modRec := httptest.NewRecorder()
	h.handleModeration(modRec, modReq)
	if modRec.Code != http.StatusOK {
		t.Fatalf("moderation status = %d, want %d", modRec.Code, http.StatusOK)
	}

	reviewBody, _ := json.Marshal(map[string]any{"user_id": "u1", "rating": 5, "comment": "great"})
	reviewReq := httptest.NewRequest(http.MethodPost, "/v1/marketplace/packages/pkg-3/review", bytes.NewReader(reviewBody))
	reviewReq.SetPathValue("id", "pkg-3")
	reviewRec := httptest.NewRecorder()
	h.handleAddReview(reviewRec, reviewReq)
	if reviewRec.Code != http.StatusCreated {
		t.Fatalf("review status = %d, want %d", reviewRec.Code, http.StatusCreated)
	}

	likeReq := httptest.NewRequest(http.MethodPost, "/v1/marketplace/packages/pkg-3/like", nil)
	likeReq.SetPathValue("id", "pkg-3")
	likeRec := httptest.NewRecorder()
	h.handleLike(likeRec, likeReq)
	if likeRec.Code != http.StatusCreated {
		t.Fatalf("like status = %d, want %d", likeRec.Code, http.StatusCreated)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/marketplace/packages/pkg-3", nil)
	getReq.SetPathValue("id", "pkg-3")
	getRec := httptest.NewRecorder()
	h.handleGet(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", getRec.Code, http.StatusOK)
	}
	var pkg marketplace.Package
	if err := json.NewDecoder(getRec.Body).Decode(&pkg); err != nil {
		t.Fatalf("decode package: %v", err)
	}
	if pkg.ReviewState != "published" {
		t.Fatalf("review state = %q, want published", pkg.ReviewState)
	}
	if pkg.Likes != 1 {
		t.Fatalf("likes = %d, want 1", pkg.Likes)
	}
	if pkg.RatingCount != 1 || pkg.Rating != 5 {
		t.Fatalf("rating = %v count = %d, want 5/1", pkg.Rating, pkg.RatingCount)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/marketplace/packages/pkg-3/reviews", nil)
	listReq.SetPathValue("id", "pkg-3")
	listRec := httptest.NewRecorder()
	h.handleListReviews(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("review list status = %d, want %d", listRec.Code, http.StatusOK)
	}
}

func TestMarketplaceHandler_UploadStagesArtifact(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	h := NewMarketplaceHandler(dataDir)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "pkg.zip")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("zip-bytes")); err != nil {
		t.Fatalf("Write file: %v", err)
	}
	meta := `{"package":{"id":"pkg-upload","name":"upload-demo","type":"plugin","version":"1.0.0","description":"demo"},"force":false}`
	if err := mw.WriteField("metadata", meta); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("Close multipart: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/marketplace/packages/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.handleUpload(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	artifactPath := filepath.Join(dataDir, "marketplace", "staging", "pkg-upload", "pkg.zip")
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("expected artifact at %s: %v", artifactPath, err)
	}
	pkg, ok := h.catalog.Get("pkg-upload")
	if !ok {
		t.Fatal("expected uploaded package in catalog")
	}
	if pkg.ReviewState != "pending_review" {
		t.Fatalf("review state = %q, want pending_review", pkg.ReviewState)
	}
}

func TestMarketplaceHandler_ListHidesUnpublishedForNonAdmin(t *testing.T) {
	t.Parallel()
	h := NewMarketplaceHandler(t.TempDir())
	_ = h.catalog.Register(context.Background(), marketplace.Package{
		ID:          "pkg-public",
		Name:        "public",
		Type:        marketplace.TypeSkill,
		Version:     "1.0.0",
		Description: "public",
		ReviewState: "published",
	})
	_ = h.catalog.Register(context.Background(), marketplace.Package{
		ID:          "pkg-draft",
		Name:        "draft",
		Type:        marketplace.TypeSkill,
		Version:     "1.0.0",
		Description: "draft",
		ReviewState: "pending_review",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/marketplace/packages", nil)
	rec := httptest.NewRecorder()
	h.handleList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var result marketplace.SearchResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Packages) != 1 || result.Packages[0].ID != "pkg-public" {
		t.Fatalf("packages = %#v", result.Packages)
	}
}

func TestMarketplaceHandler_ListShowsRequestedStateForAdmin(t *testing.T) {
	t.Parallel()
	h := NewMarketplaceHandler(t.TempDir())
	_ = h.catalog.Register(context.Background(), marketplace.Package{
		ID:          "pkg-draft",
		Name:        "draft",
		Type:        marketplace.TypeSkill,
		Version:     "1.0.0",
		Description: "draft",
		ReviewState: "pending_review",
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/marketplace/packages?review_state=pending_review", nil)
	req = req.WithContext(store.WithRole(req.Context(), "admin"))
	rec := httptest.NewRecorder()
	h.handleList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var result marketplace.SearchResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Packages) != 1 || result.Packages[0].ID != "pkg-draft" {
		t.Fatalf("packages = %#v", result.Packages)
	}
}

func TestMarketplaceHandler_InstallRequiresEntitlementForPaidPackage(t *testing.T) {
	t.Parallel()
	h := NewMarketplaceHandler(t.TempDir())
	if err := h.catalog.Register(context.Background(), marketplace.Package{
		ID:          "pkg-paid",
		Name:        "paid-plugin",
		Type:        marketplace.TypePlugin,
		Version:     "1.0.0",
		Description: "paid",
		ReviewState: "published",
		Pricing: marketplace.Pricing{
			Model: marketplace.PricingPaid,
			Price: 9.9,
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/marketplace/packages/pkg-paid/install", nil)
	req.SetPathValue("id", "pkg-paid")
	rec := httptest.NewRecorder()
	h.handleInstall(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestMarketplaceHandler_InstallRequiresPublishedState(t *testing.T) {
	t.Parallel()
	h := NewMarketplaceHandler(t.TempDir())
	if err := h.catalog.Register(context.Background(), marketplace.Package{
		ID:          "pkg-draft",
		Name:        "draft-plugin",
		Type:        marketplace.TypePlugin,
		Version:     "1.0.0",
		Description: "draft",
		ReviewState: "pending_review",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/marketplace/packages/pkg-draft/install", nil)
	req.SetPathValue("id", "pkg-draft")
	rec := httptest.NewRecorder()
	h.handleInstall(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestMarketplaceHandler_InstallAndListInstalled(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	h := NewMarketplaceHandler(dataDir)
	if err := h.catalog.Register(context.Background(), marketplace.Package{
		ID:          "pkg-local",
		Name:        "local-skill",
		Type:        marketplace.TypeSkill,
		Version:     "1.0.0",
		Description: "free local package",
		ReviewState: "published",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	installReq := httptest.NewRequest(http.MethodPost, "/v1/marketplace/packages/pkg-local/install", nil)
	installReq.SetPathValue("id", "pkg-local")
	installRec := httptest.NewRecorder()
	h.handleInstall(installRec, installReq)
	if installRec.Code != http.StatusCreated {
		t.Fatalf("install status = %d, want %d, body=%s", installRec.Code, http.StatusCreated, installRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/marketplace/installed", nil)
	listRec := httptest.NewRecorder()
	h.handleInstalled(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("installed status = %d, want %d", listRec.Code, http.StatusOK)
	}
	var resp struct {
		Packages []marketplace.InstallResult `json:"packages"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode installed: %v", err)
	}
	if len(resp.Packages) != 1 || resp.Packages[0].PackageID != "pkg-local" {
		t.Fatalf("installed packages = %#v", resp.Packages)
	}
}
