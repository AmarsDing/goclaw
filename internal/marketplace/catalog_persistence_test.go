package marketplace

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCatalogSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	catalog := NewCatalog()
	if err := catalog.Register(context.Background(), Package{
		ID:          "pkg-1",
		Name:        "test-skill",
		Type:        TypeSkill,
		Description: "demo",
		Version:     "1.0.0",
		Ref:         "v1.0.0",
	}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	if err := catalog.AddReview(context.Background(), Review{
		ID:        "review-1",
		PackageID: "pkg-1",
		UserID:    "user-1",
		Rating:    5,
		Comment:   "great",
	}); err != nil {
		t.Fatalf("AddReview() error: %v", err)
	}

	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := catalog.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile() error: %v", err)
	}

	loaded := NewCatalog()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile() error: %v", err)
	}

	pkg, ok := loaded.Get("pkg-1")
	if !ok {
		t.Fatal("expected package after load")
	}
	if pkg.Ref != "v1.0.0" {
		t.Fatalf("pkg.Ref = %q, want v1.0.0", pkg.Ref)
	}
	search := loaded.Search(SearchQuery{Query: "test"})
	if len(search.Packages) != 1 {
		t.Fatalf("search packages len = %d, want 1", len(search.Packages))
	}
	if search.Packages[0].Rating != 5 {
		t.Fatalf("search rating = %v, want 5", search.Packages[0].Rating)
	}
}
