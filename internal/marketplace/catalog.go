// Package marketplace provides a catalog and distribution system for
// skills, agents, MCP servers, and plugins.
//
// The marketplace indexes packages hosted on GitHub, supports versioned
// installation, trial/billing integration, and user ratings.
package marketplace

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// PackageType classifies marketplace packages.
type PackageType string

const (
	TypeSkill  PackageType = "skill"
	TypeAgent  PackageType = "agent"
	TypeTeam   PackageType = "team"
	TypeMCP    PackageType = "mcp_server"
	TypePlugin PackageType = "plugin"
)

// Package is a marketplace listing.
type Package struct {
	ID          string      `json:"id"`
	TenantID    string      `json:"tenant_id,omitempty"`
	Name        string      `json:"name"`
	Type        PackageType `json:"type"`
	Description string      `json:"description"`
	Author      string      `json:"author"`
	Repository  string      `json:"repository"` // GitHub URL
	Version     string      `json:"version"`
	Ref         string      `json:"ref,omitempty"` // tag, branch, or commit to install
	License     string      `json:"license"`
	Tags        []string    `json:"tags"`
	Downloads   int         `json:"downloads"`
	Likes       int         `json:"likes"`
	Rating      float64     `json:"rating"`
	RatingCount int         `json:"rating_count"`
	ReviewState string      `json:"review_state,omitempty"` // pending_review, approved, rejected, published
	ReviewNote  string      `json:"review_note,omitempty"`  // reject/approval note
	ArtifactURI string      `json:"artifact_uri,omitempty"` // remote object storage URI for staged/published artifact
	ArtifactSHA256 string   `json:"artifact_sha256,omitempty"`
	ArtifactSize int64      `json:"artifact_size,omitempty"`
	Pricing     Pricing     `json:"pricing"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	Verified       bool   `json:"verified"` // reviewed by platform team
	ReadmeMarkdown string `json:"readme_markdown,omitempty"`
}

// Pricing describes the cost model.
type Pricing struct {
	Model     PricingModel `json:"model"`
	Price     float64      `json:"price,omitempty"`    // for paid/subscription
	Currency  string       `json:"currency,omitempty"` // "USD", "CNY"
	TrialDays int          `json:"trial_days,omitempty"`
}

// PricingModel identifies how the package is monetised.
type PricingModel string

const (
	PricingFree         PricingModel = "free"
	PricingPaid         PricingModel = "paid"
	PricingSubscription PricingModel = "subscription"
	PricingFreemium     PricingModel = "freemium"
)

// Review is a user-submitted rating and comment.
type Review struct {
	ID        string    `json:"id"`
	PackageID string    `json:"package_id"`
	UserID    string    `json:"user_id"`
	Rating    int       `json:"rating"` // 1–5
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

// SearchQuery filters marketplace packages.
type SearchQuery struct {
	Query       string      `json:"query,omitempty"`
	Type        PackageType `json:"type,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	Author      string      `json:"author,omitempty"`
	ReviewState string      `json:"review_state,omitempty"`
	MinRating   float64     `json:"min_rating,omitempty"`
	Free        bool        `json:"free,omitempty"`
	SortBy      string      `json:"sort_by,omitempty"` // "downloads", "rating", "updated", "name"
	Limit       int         `json:"limit,omitempty"`
	Offset      int         `json:"offset,omitempty"`
	TenantID    string      `json:"tenant_id,omitempty"`
}

// SearchResult is a paginated search response.
type SearchResult struct {
	Packages []Package `json:"packages"`
	Total    int       `json:"total"`
	Limit    int       `json:"limit"`
	Offset   int       `json:"offset"`
}

// Catalog manages the package index and search.
type Catalog struct {
	mu       sync.RWMutex
	packages map[string]*Package
	reviews  map[string][]Review // packageID → reviews
}

// NewCatalog creates an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{
		packages: make(map[string]*Package),
		reviews:  make(map[string][]Review),
	}
}

// PackageCount returns the number of indexed packages.
func (c *Catalog) PackageCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.packages)
}

// Register adds or updates a package in the catalog.
func (c *Catalog) Register(_ context.Context, pkg Package) error {
	if pkg.ID == "" || pkg.Name == "" {
		return fmt.Errorf("package ID and name are required")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	existing, ok := c.packages[pkg.ID]
	if ok {
		pkg.Downloads = existing.Downloads
		pkg.Likes = existing.Likes
		pkg.Rating = existing.Rating
		pkg.RatingCount = existing.RatingCount
		pkg.CreatedAt = existing.CreatedAt
		if pkg.ReviewState == "" {
			pkg.ReviewState = existing.ReviewState
		}
		if pkg.ReviewNote == "" {
			pkg.ReviewNote = existing.ReviewNote
		}
	} else {
		pkg.CreatedAt = time.Now()
		if pkg.ReviewState == "" {
			pkg.ReviewState = "pending_review"
		}
	}
	pkg.UpdatedAt = time.Now()

	c.packages[pkg.ID] = &pkg
	slog.Debug("marketplace: package registered", "id", pkg.ID, "name", pkg.Name)
	return nil
}

// Get returns a package by ID.
func (c *Catalog) Get(id string) (*Package, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	pkg, ok := c.packages[id]
	if !ok {
		return nil, false
	}
	cp := *pkg
	return &cp, true
}

// Search finds packages matching the query.
func (c *Catalog) Search(query SearchQuery) SearchResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var matched []*Package
	for _, pkg := range c.packages {
		if !matchesQuery(pkg, query) {
			continue
		}
		matched = append(matched, pkg)
	}

	sortPackages(matched, query.SortBy)

	total := len(matched)
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := query.Offset
	if offset >= total {
		return SearchResult{Total: total, Limit: limit, Offset: offset}
	}

	end := offset + limit
	if end > total {
		end = total
	}

	result := make([]Package, end-offset)
	for i, pkg := range matched[offset:end] {
		result[i] = *pkg
	}

	return SearchResult{
		Packages: result,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	}
}

// AddReview adds a user review and updates the package rating.
func (c *Catalog) AddReview(_ context.Context, review Review) error {
	if review.Rating < 1 || review.Rating > 5 {
		return fmt.Errorf("rating must be 1-5")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	pkg, ok := c.packages[review.PackageID]
	if !ok {
		return fmt.Errorf("package %q not found", review.PackageID)
	}

	review.CreatedAt = time.Now()
	if review.ID == "" {
		review.ID = fmt.Sprintf("review-%d", time.Now().UnixNano())
	}

	c.reviews[review.PackageID] = append(c.reviews[review.PackageID], review)

	// Recalculate average rating.
	reviews := c.reviews[review.PackageID]
	total := 0.0
	for _, r := range reviews {
		total += float64(r.Rating)
	}
	pkg.Rating = total / float64(len(reviews))
	pkg.RatingCount = len(reviews)
	pkg.UpdatedAt = time.Now()

	return nil
}

// IncrementDownloads bumps the download count.
func (c *Catalog) IncrementDownloads(packageID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pkg, ok := c.packages[packageID]; ok {
		pkg.Downloads++
		pkg.UpdatedAt = time.Now()
	}
}

// IncrementLikes bumps the likes count for a package.
func (c *Catalog) IncrementLikes(packageID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	pkg, ok := c.packages[packageID]
	if !ok {
		return fmt.Errorf("package %q not found", packageID)
	}
	pkg.Likes++
	pkg.UpdatedAt = time.Now()
	return nil
}

// SetReviewState updates marketplace moderation status for a package.
func (c *Catalog) SetReviewState(packageID, state, note string) error {
	switch state {
	case "pending_review", "approved", "rejected", "published":
	default:
		return fmt.Errorf("invalid review state %q", state)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	pkg, ok := c.packages[packageID]
	if !ok {
		return fmt.Errorf("package %q not found", packageID)
	}
	pkg.ReviewState = state
	pkg.ReviewNote = note
	pkg.UpdatedAt = time.Now()
	return nil
}

// ListReviews returns a stable copy of reviews for a package, newest first.
func (c *Catalog) ListReviews(packageID string) ([]Review, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if _, ok := c.packages[packageID]; !ok {
		return nil, fmt.Errorf("package %q not found", packageID)
	}
	reviews := append([]Review(nil), c.reviews[packageID]...)
	sort.Slice(reviews, func(i, j int) bool {
		return reviews[i].CreatedAt.After(reviews[j].CreatedAt)
	})
	return reviews, nil
}

func matchesQuery(pkg *Package, q SearchQuery) bool {
	if q.TenantID != "" && pkg.TenantID != "" && pkg.TenantID != q.TenantID {
		return false
	}
	if q.Type != "" && pkg.Type != q.Type {
		return false
	}
	if q.Author != "" && pkg.Author != q.Author {
		return false
	}
	if q.ReviewState != "" && pkg.ReviewState != q.ReviewState {
		return false
	}
	if q.MinRating > 0 && pkg.Rating < q.MinRating {
		return false
	}
	if q.Free && pkg.Pricing.Model != PricingFree {
		return false
	}
	if q.Query != "" {
		lowerQuery := strings.ToLower(q.Query)
		if !strings.Contains(strings.ToLower(pkg.Name), lowerQuery) &&
			!strings.Contains(strings.ToLower(pkg.Description), lowerQuery) {
			return false
		}
	}
	if len(q.Tags) > 0 {
		tagSet := make(map[string]bool, len(pkg.Tags))
		for _, t := range pkg.Tags {
			tagSet[t] = true
		}
		for _, qt := range q.Tags {
			if !tagSet[qt] {
				return false
			}
		}
	}
	return true
}

func sortPackages(pkgs []*Package, sortBy string) {
	switch sortBy {
	case "downloads":
		sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Downloads > pkgs[j].Downloads })
	case "rating":
		sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Rating > pkgs[j].Rating })
	case "updated":
		sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].UpdatedAt.After(pkgs[j].UpdatedAt) })
	case "name":
		sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	default:
		sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Downloads > pkgs[j].Downloads })
	}
}
