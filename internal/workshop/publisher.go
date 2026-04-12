package workshop

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
)

// Publisher handles testing and publishing workshop creations to the marketplace.
type Publisher struct {
	catalog *marketplace.Catalog
}

// NewPublisher creates a publisher.
func NewPublisher(catalog *marketplace.Catalog) *Publisher {
	return &Publisher{catalog: catalog}
}

// TestResult describes the outcome of a sandbox test.
type TestResult struct {
	Passed  bool     `json:"passed"`
	Errors  []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Duration time.Duration `json:"duration"`
}

// Validate checks a build result for basic correctness before publishing.
func (p *Publisher) Validate(result *BuildResult) *TestResult {
	start := time.Now()
	tr := &TestResult{Passed: true}

	if result.Name == "" {
		tr.Errors = append(tr.Errors, "name is required")
		tr.Passed = false
	}
	if result.Content == "" {
		tr.Errors = append(tr.Errors, "content is empty")
		tr.Passed = false
	}
	if result.Type == "" {
		tr.Errors = append(tr.Errors, "type is required")
		tr.Passed = false
	}

	if len(result.Content) < 50 {
		tr.Warnings = append(tr.Warnings, "content is very short — consider adding more detail")
	}

	tr.Duration = time.Since(start)
	return tr
}

// Publish registers a build result in the marketplace catalog.
func (p *Publisher) Publish(ctx context.Context, result *BuildResult, author string) error {
	validation := p.Validate(result)
	if !validation.Passed {
		return fmt.Errorf("validation failed: %v", validation.Errors)
	}

	var pkgType marketplace.PackageType
	switch result.Type {
	case "skill":
		pkgType = marketplace.TypeSkill
	case "agent":
		pkgType = marketplace.TypeAgent
	case "mcp_tool":
		pkgType = marketplace.TypeMCP
	case "plugin":
		pkgType = marketplace.TypePlugin
	default:
		return fmt.Errorf("unknown package type: %s", result.Type)
	}

	pkg := marketplace.Package{
		ID:          fmt.Sprintf("%s-%s-%d", result.Type, sanitizeName(result.Name), time.Now().UnixNano()),
		Name:        result.Name,
		Type:        pkgType,
		Description: "Created in DreamWeaver Workshop",
		Author:      author,
		Version:     "1.0.0",
		License:     "MIT",
		Pricing: marketplace.Pricing{
			Model: marketplace.PricingFree,
		},
	}

	if err := p.catalog.Register(ctx, pkg); err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	slog.Info("workshop: published to marketplace",
		"name", result.Name, "type", result.Type, "id", pkg.ID)
	return nil
}
