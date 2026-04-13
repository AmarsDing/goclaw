package changelog

import (
	"slices"
	"testing"
)

func TestHeuristicRiskTags(t *testing.T) {
	cases := []struct {
		name string
		files []FileChange
		want []string
	}{
		{
			name:  "migration",
			files: []FileChange{{Status: "M", Path: "migrations/000050_foo.sql"}},
			want:  []string{"config-or-schema", "migration"},
		},
		{
			name: "docs only",
			files: []FileChange{
				{Status: "M", Path: "docs/README.md"},
			},
			want: []string{"docs-only"},
		},
		{
			name: "sensitive surfaces",
			files: []FileChange{
				{Status: "M", Path: "internal/permissions/governor.go"},
				{Status: "M", Path: "internal/http/server.go"},
				{Status: "M", Path: "ui/web/src/App.tsx"},
				{Status: "M", Path: "pkg/sdk/bridge.go"},
			},
			want: []string{"frontend", "gateway-http", "public-api-surface", "security-sensitive-path"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := heuristicRiskTags(tc.files)
			slices.Sort(got)
			for _, w := range tc.want {
				if !slices.Contains(got, w) {
					t.Fatalf("missing tag %q in %v", w, got)
				}
			}
		})
	}
}

func TestHeuristicRiskTagsMixedDocsNotDocsOnly(t *testing.T) {
	files := []FileChange{{Status: "M", Path: "docs/a.md"}, {Status: "M", Path: "internal/foo.go"}}
	got := heuristicRiskTags(files)
	for _, s := range got {
		if s == "docs-only" {
			t.Fatal("expected not docs-only")
		}
	}
}
