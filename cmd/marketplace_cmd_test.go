package cmd

import (
	"os"
	"path/filepath"
	"strings"
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
		Author:      "author",
	})
	if err == nil {
		t.Fatal("expected missing SKILL.md error")
	}
}

func TestValidateMarketplacePush_PluginPassesWithPluginJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pluginManifest := `{
  "name": "demo",
  "version": "1.2.3",
  "description": "demo plugin",
  "entrypoint": "demo.exe",
  "transport": "stdio",
  "capabilities": {
    "tools": [
      {"name":"echo","description":"echo input","parameters":{"type":"object"}}
    ]
  },
  "permissions": {"file_system": false, "network": false, "shell": false}
}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(pluginManifest), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := validateMarketplacePush(dir, &marketplace.Package{
		Name:        "demo-plugin",
		Version:     "1.0.0",
		Type:        marketplace.TypePlugin,
		Description: "desc",
		Author:      "author",
	}); err != nil {
		t.Fatalf("validateMarketplacePush: %v", err)
	}
}

func TestValidateMarketplacePush_AgentRequiresAgentJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := validateMarketplacePush(dir, &marketplace.Package{
		Name:        "demo-agent",
		Version:     "1.0.0",
		Type:        marketplace.TypeAgent,
		Description: "desc",
		Author:      "author",
	})
	if err == nil || !strings.Contains(err.Error(), "required file missing: agent.json") {
		t.Fatalf("expected missing agent.json error, got %v", err)
	}
}

func TestValidateMarketplacePush_MCPRequiresConfigFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := validateMarketplacePush(dir, &marketplace.Package{
		Name:        "demo-mcp",
		Version:     "1.0.0",
		Type:        marketplace.TypeMCP,
		Description: "desc",
		Author:      "author",
	})
	if err == nil || !strings.Contains(err.Error(), "mcp_server package requires .mcp.json or mcp.json") {
		t.Fatalf("expected missing mcp config error, got %v", err)
	}
}

func TestValidateMarketplacePush_CollectsMetadataErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Skill"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	err := validateMarketplacePush(dir, &marketplace.Package{
		Name:        "demo",
		Version:     "not-semver",
		Type:        marketplace.TypeSkill,
		Description: "desc",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "metadata.author is required") {
		t.Fatalf("expected author error, got %v", err)
	}
	if !strings.Contains(err.Error(), "metadata.version should be semver-like") {
		t.Fatalf("expected semver error, got %v", err)
	}
}
