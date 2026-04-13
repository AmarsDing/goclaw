package workshop

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
)

// Publisher handles testing and publishing workshop creations to the marketplace.
type Publisher struct {
	catalog      *marketplace.Catalog
	baseURL      string
	authToken    string
	uploadClient *http.Client
	uploadFn     func(ctx context.Context, client *http.Client, baseURL, authBearer string, pkg marketplace.Package, force bool, archive []byte, extraMetadata map[string]any) (map[string]any, error)
}

// NewPublisher creates a publisher.
func NewPublisher(catalog *marketplace.Catalog) *Publisher {
	return &Publisher{
		catalog:      catalog,
		baseURL:      strings.TrimSuffix(envOr("GOCLAW_GATEWAY_BASE_URL", "http://127.0.0.1:18790"), "/"),
		authToken:    os.Getenv("GOCLAW_GATEWAY_TOKEN"),
		uploadClient: &http.Client{Timeout: 6 * time.Minute},
		uploadFn:     marketplace.UploadPackageHTTP,
	}
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

// Publish pushes the build result to the remote marketplace gateway first (when configured),
// then registers it in the local marketplace catalog snapshot.
// If GOCLAW_GATEWAY_TOKEN is not configured, Publish falls back to local-only registration.
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

	if strings.TrimSpace(p.authToken) != "" && p.uploadFn != nil {
		archive, err := zipWorkshopResult(result)
		if err != nil {
			return fmt.Errorf("build upload archive: %w", err)
		}
		resp, err := p.uploadFn(ctx, p.uploadClient, p.baseURL, p.authToken, pkg, false, archive, map[string]any{
			"source": "workshop_publish",
		})
		if err != nil {
			return fmt.Errorf("remote publish failed: %w", err)
		}
		slog.Info("workshop: remote publish completed",
			"name", result.Name, "type", result.Type, "id", pkg.ID, "artifact_path", resp["artifact_path"])
	} else {
		slog.Warn("workshop: gateway token missing, falling back to local-only publish",
			"name", result.Name, "type", result.Type)
	}

	if err := p.catalog.Register(ctx, pkg); err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	slog.Info("workshop: published to marketplace",
		"name", result.Name, "type", result.Type, "id", pkg.ID)
	return nil
}

func zipWorkshopResult(result *BuildResult) ([]byte, error) {
	fileName := workshopResultFileName(result.Type)
	if fileName == "" {
		return nil, fmt.Errorf("unsupported build result type %q", result.Type)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(fileName)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write([]byte(result.Content)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func workshopResultFileName(resultType string) string {
	switch resultType {
	case "skill":
		return "SKILL.md"
	case "agent":
		return "agent.json"
	case "plugin":
		return "plugin.json"
	case "mcp_tool":
		return "mcp.json"
	default:
		return ""
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
