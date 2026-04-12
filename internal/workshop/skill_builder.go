// Package workshop provides the "织梦坊" (DreamWeaver Workshop) for creating
// skills, agents, and MCP tools through guided workflows.
package workshop

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SkillTemplate is the scaffold for a new skill.
type SkillTemplate struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	WhenToUse    string   `json:"when_to_use"`
	Instructions string   `json:"instructions"`
	Examples     []string `json:"examples,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

// SkillBuilder guides users through creating a new SKILL.md file.
type SkillBuilder struct {
	outputDir string
}

// NewSkillBuilder creates a skill builder.
func NewSkillBuilder(outputDir string) *SkillBuilder {
	return &SkillBuilder{outputDir: outputDir}
}

// Build generates a SKILL.md from a template.
func (b *SkillBuilder) Build(_ context.Context, tmpl SkillTemplate) (*BuildResult, error) {
	if tmpl.Name == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	if tmpl.Description == "" {
		return nil, fmt.Errorf("skill description is required")
	}

	content := b.renderSkillMD(tmpl)

	return &BuildResult{
		Name:      tmpl.Name,
		Type:      "skill",
		Content:   content,
		Path:      fmt.Sprintf("%s/%s/SKILL.md", b.outputDir, sanitizeName(tmpl.Name)),
		CreatedAt: time.Now(),
	}, nil
}

func (b *SkillBuilder) renderSkillMD(tmpl SkillTemplate) string {
	var sb strings.Builder

	sb.WriteString("# ")
	sb.WriteString(tmpl.Name)
	sb.WriteString("\n\n")

	sb.WriteString("## Description\n\n")
	sb.WriteString(tmpl.Description)
	sb.WriteString("\n\n")

	sb.WriteString("## When to Use\n\n")
	sb.WriteString(tmpl.WhenToUse)
	sb.WriteString("\n\n")

	sb.WriteString("## Instructions\n\n")
	sb.WriteString(tmpl.Instructions)
	sb.WriteString("\n")

	if len(tmpl.Examples) > 0 {
		sb.WriteString("\n## Examples\n\n")
		for _, ex := range tmpl.Examples {
			sb.WriteString("- ")
			sb.WriteString(ex)
			sb.WriteString("\n")
		}
	}

	if len(tmpl.Tags) > 0 {
		sb.WriteString("\n## Tags\n\n")
		sb.WriteString(strings.Join(tmpl.Tags, ", "))
		sb.WriteString("\n")
	}

	return sb.String()
}

// BuildResult is the output of a workshop build operation.
type BuildResult struct {
	Name      string    `json:"name"`
	Type      string    `json:"type"` // "skill", "agent", "mcp_tool"
	Content   string    `json:"content"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	var clean strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			clean.WriteRune(r)
		}
	}
	return clean.String()
}
