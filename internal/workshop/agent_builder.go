package workshop

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// AgentTemplate is the scaffold for a new agent configuration.
type AgentTemplate struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	SystemPrompt string            `json:"system_prompt"`
	Provider     string            `json:"provider"`
	Model        string            `json:"model"`
	Tools        AgentToolConfig   `json:"tools"`
	Memory       AgentMemoryConfig `json:"memory"`
	Mode         string            `json:"mode"` // "safe", "standard", "autonomous"
	Tags         []string          `json:"tags,omitempty"`
}

// AgentToolConfig configures which tools the agent can use.
type AgentToolConfig struct {
	Allow []string `json:"allow,omitempty"` // empty = all
	Deny  []string `json:"deny,omitempty"`
}

// AgentMemoryConfig configures agent memory behavior.
type AgentMemoryConfig struct {
	AutoInject         bool    `json:"auto_inject"`
	ConsolidationEnabled bool  `json:"consolidation_enabled"`
	EpisodicTTLDays    int    `json:"episodic_ttl_days"`
	Threshold          float64 `json:"threshold"`
}

// AgentBuilder guides users through creating an agent configuration.
type AgentBuilder struct {
	outputDir string
}

// NewAgentBuilder creates an agent builder.
func NewAgentBuilder(outputDir string) *AgentBuilder {
	return &AgentBuilder{outputDir: outputDir}
}

// Build generates an agent configuration JSON.
func (b *AgentBuilder) Build(_ context.Context, tmpl AgentTemplate) (*BuildResult, error) {
	if tmpl.Name == "" {
		return nil, fmt.Errorf("agent name is required")
	}

	// Apply defaults.
	if tmpl.Provider == "" {
		tmpl.Provider = "openai"
	}
	if tmpl.Model == "" {
		tmpl.Model = "gpt-4o"
	}
	if tmpl.Mode == "" {
		tmpl.Mode = "standard"
	}
	if tmpl.Memory.EpisodicTTLDays == 0 {
		tmpl.Memory.EpisodicTTLDays = 90
	}
	if tmpl.Memory.Threshold == 0 {
		tmpl.Memory.Threshold = 0.3
	}

	data, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal agent config: %w", err)
	}

	return &BuildResult{
		Name:      tmpl.Name,
		Type:      "agent",
		Content:   string(data),
		Path:      fmt.Sprintf("%s/%s/agent.json", b.outputDir, sanitizeName(tmpl.Name)),
		CreatedAt: time.Now(),
	}, nil
}
