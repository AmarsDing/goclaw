package workshop

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
)

func TestPublisherPublish_RemoteFirstThenLocalRegister(t *testing.T) {
	t.Parallel()
	catalog := marketplace.NewCatalog()
	p := NewPublisher(catalog)
	p.authToken = "token"
	remoteCalled := false
	p.uploadFn = func(ctx context.Context, client *http.Client, baseURL, authBearer string, pkg marketplace.Package, force bool, archive []byte, extraMetadata map[string]any) (map[string]any, error) {
		remoteCalled = true
		if len(archive) == 0 {
			t.Fatal("expected non-empty archive")
		}
		if extraMetadata["source"] != "workshop_publish" {
			t.Fatalf("unexpected metadata: %#v", extraMetadata)
		}
		return map[string]any{"artifact_path": "/tmp/pkg.zip"}, nil
	}

	err := p.Publish(context.Background(), &BuildResult{
		Name:      "Demo Skill",
		Type:      "skill",
		Content:   "# demo skill content with enough length for validation pass",
		CreatedAt: time.Now(),
	}, "alice")
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if !remoteCalled {
		t.Fatal("expected remote upload to be called")
	}
	result := catalog.Search(marketplace.SearchQuery{Limit: 10})
	if len(result.Packages) != 1 {
		t.Fatalf("expected local catalog register, got %d", len(result.Packages))
	}
}

func TestPublisherPublish_RemoteFailureReturnsError(t *testing.T) {
	t.Parallel()
	catalog := marketplace.NewCatalog()
	p := NewPublisher(catalog)
	p.authToken = "token"
	p.uploadFn = func(ctx context.Context, client *http.Client, baseURL, authBearer string, pkg marketplace.Package, force bool, archive []byte, extraMetadata map[string]any) (map[string]any, error) {
		return nil, errors.New("gateway down")
	}

	err := p.Publish(context.Background(), &BuildResult{
		Name:      "Demo Skill",
		Type:      "skill",
		Content:   "# demo skill content with enough length for validation pass",
		CreatedAt: time.Now(),
	}, "alice")
	if err == nil {
		t.Fatal("expected remote publish error")
	}
	result := catalog.Search(marketplace.SearchQuery{Limit: 10})
	if len(result.Packages) != 0 {
		t.Fatalf("expected no local register on remote failure, got %d", len(result.Packages))
	}
}

func TestPublisherPublish_LocalFallbackWithoutToken(t *testing.T) {
	t.Parallel()
	catalog := marketplace.NewCatalog()
	p := NewPublisher(catalog)
	p.authToken = ""
	remoteCalled := false
	p.uploadFn = func(ctx context.Context, client *http.Client, baseURL, authBearer string, pkg marketplace.Package, force bool, archive []byte, extraMetadata map[string]any) (map[string]any, error) {
		remoteCalled = true
		return nil, nil
	}

	err := p.Publish(context.Background(), &BuildResult{
		Name:      "Demo Skill",
		Type:      "skill",
		Content:   "# demo skill content with enough length for validation pass",
		CreatedAt: time.Now(),
	}, "alice")
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if remoteCalled {
		t.Fatal("expected remote upload to be skipped without token")
	}
	result := catalog.Search(marketplace.SearchQuery{Limit: 10})
	if len(result.Packages) != 1 {
		t.Fatalf("expected local register, got %d", len(result.Packages))
	}
}
