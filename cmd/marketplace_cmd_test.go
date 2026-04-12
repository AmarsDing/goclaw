package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
)

func TestValidateMarketplacePush_SkillRequiresSkillMarkdown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("demo"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := validateMarketplacePush(dir, &marketplace.Package{
		Name:        "demo",
		Version:     "1.0.0",
		Type:        marketplace.TypeSkill,
		Description: "desc",
	})
	if err == nil {
		t.Fatal("expected missing SKILL.md error")
	}
}

func TestValidateMarketplacePush_PluginPassesWithPluginJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := validateMarketplacePush(dir, &marketplace.Package{
		Name:        "demo-plugin",
		Version:     "1.0.0",
		Type:        marketplace.TypePlugin,
		Description: "desc",
	}); err != nil {
		t.Fatalf("validateMarketplacePush: %v", err)
	}
}
