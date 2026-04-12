package marketplace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// CatalogSnapshot is a portable representation of a catalog state.
type CatalogSnapshot struct {
	Packages []Package `json:"packages"`
	Reviews  []Review  `json:"reviews,omitempty"`
}

// Snapshot returns a stable copy of the catalog for persistence.
func (c *Catalog) Snapshot() CatalogSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	packages := make([]Package, 0, len(c.packages))
	for _, pkg := range c.packages {
		packages = append(packages, *pkg)
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].ID < packages[j].ID
	})

	var reviews []Review
	for _, pkgReviews := range c.reviews {
		reviews = append(reviews, pkgReviews...)
	}
	sort.Slice(reviews, func(i, j int) bool {
		if reviews[i].PackageID == reviews[j].PackageID {
			return reviews[i].ID < reviews[j].ID
		}
		return reviews[i].PackageID < reviews[j].PackageID
	})

	return CatalogSnapshot{
		Packages: packages,
		Reviews:  reviews,
	}
}

// Restore replaces the catalog contents with a persisted snapshot.
func (c *Catalog) Restore(snapshot CatalogSnapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.packages = make(map[string]*Package, len(snapshot.Packages))
	c.reviews = make(map[string][]Review)
	for _, pkg := range snapshot.Packages {
		copyPkg := pkg
		c.packages[pkg.ID] = &copyPkg
	}
	for _, review := range snapshot.Reviews {
		c.reviews[review.PackageID] = append(c.reviews[review.PackageID], review)
	}
}

// SaveToFile writes the current catalog snapshot to disk atomically.
func (c *Catalog) SaveToFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.Snapshot(), "", "  ")
	if err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

// LoadFromFile restores a catalog snapshot from disk.
func (c *Catalog) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var snapshot CatalogSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	c.Restore(snapshot)
	return nil
}
