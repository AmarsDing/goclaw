package migratefile

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFileURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "migrations")
	u := FileURL(sub)
	if !strings.HasPrefix(u, "file://") {
		t.Fatalf("expected file:// prefix, got %q", u)
	}
	if !strings.Contains(u, "migrations") {
		t.Fatalf("expected path segment in URL: %q", u)
	}
}
