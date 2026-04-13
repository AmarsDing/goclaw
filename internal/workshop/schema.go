package workshop

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValidateAgentJSON checks workshop-style agent JSON (subset of runtime agent fields).
func ValidateAgentJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("agent JSON: %w", err)
	}
	if _, ok := raw["name"]; !ok {
		return fmt.Errorf("agent JSON: missing required field \"name\"")
	}
	if p, ok := raw["provider"].(string); ok && p == "" {
		return fmt.Errorf("agent JSON: \"provider\" must not be empty when set")
	}
	return nil
}

// SkillFrontmatter is the optional YAML block at the top of SKILL.md.
type SkillFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Version     string   `yaml:"version"`
	Tags        []string `yaml:"tags"`
}

// ValidateSkillMarkdown parses leading YAML frontmatter (--- ... ---) when present and validates fields.
func ValidateSkillMarkdown(content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("SKILL.md: empty file")
	}
	if !strings.HasPrefix(content, "---") {
		// Allow skills without frontmatter (body-only).
		return nil
	}
	rest := strings.TrimPrefix(content, "---")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return fmt.Errorf("SKILL.md: unclosed frontmatter (expected closing ---)")
	}
	block := strings.TrimSpace(rest[:end])
	var fm SkillFrontmatter
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return fmt.Errorf("SKILL.md frontmatter: %w", err)
	}
	if fm.Name == "" && fm.Description == "" {
		return fmt.Errorf("SKILL.md frontmatter: set at least name or description")
	}
	return nil
}
