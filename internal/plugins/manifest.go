// Package plugins provides a standardised plugin protocol for extending goclaw.
//
// Plugins are self-contained extensions that can contribute tools, hooks, MCP servers,
// skills, and custom UI to the platform. Each plugin declares its capabilities via
// a manifest and follows a well-defined lifecycle (install → configure → activate →
// deactivate → uninstall).
package plugins

import "time"

// Manifest describes a plugin's identity, capabilities, and requirements.
type Manifest struct {
	// Identity
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	License     string `json:"license,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	Repository  string `json:"repository,omitempty"`

	// Requirements
	MinGoclawVersion string   `json:"min_goclaw_version,omitempty"`
	Dependencies     []string `json:"dependencies,omitempty"` // other plugin names

	// Capabilities
	Capabilities Capabilities `json:"capabilities"`

	// Permissions the plugin needs
	Permissions PluginPermissions `json:"permissions"`

	// Runtime
	Entrypoint string `json:"entrypoint"` // command or binary to launch
	Transport  string `json:"transport"`  // "stdio", "http", "mcp"
}

// Capabilities declares what the plugin contributes to the platform.
type Capabilities struct {
	Tools      []ToolDecl      `json:"tools,omitempty"`
	Hooks      []HookDecl      `json:"hooks,omitempty"`
	MCPServers []MCPServerDecl `json:"mcp_servers,omitempty"`
	Skills     []SkillDecl     `json:"skills,omitempty"`
}

// ToolDecl declares a tool the plugin provides.
type ToolDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	ReadOnly    bool           `json:"read_only"`
}

// HookDecl declares a hook the plugin wants to register.
type HookDecl struct {
	Event    string `json:"event"`
	Mode     string `json:"mode"` // "sync" / "async"
	Priority int    `json:"priority"`
}

// MCPServerDecl declares an MCP server the plugin provides.
type MCPServerDecl struct {
	Name      string `json:"name"`
	Transport string `json:"transport"` // "stdio", "sse", "streamable-http"
	Command   string `json:"command,omitempty"`
	URL       string `json:"url,omitempty"`
}

// SkillDecl declares a skill the plugin provides.
type SkillDecl struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"` // path to SKILL.md within plugin
}

// PluginPermissions declares what the plugin needs access to.
type PluginPermissions struct {
	FileSystem  bool     `json:"file_system"`
	Network     bool     `json:"network"`
	Shell       bool     `json:"shell"`
	Environment []string `json:"environment,omitempty"` // env vars needed
}

// State represents the current lifecycle state of a plugin.
type State string

const (
	StateInstalled   State = "installed"
	StateConfigured  State = "configured"
	StateActive      State = "active"
	StateInactive    State = "inactive"
	StateError       State = "error"
	StateUninstalled State = "uninstalled"
)

// Instance is a runtime plugin instance with its manifest and state.
type Instance struct {
	Manifest    Manifest  `json:"manifest"`
	State       State     `json:"state"`
	InstalledAt time.Time `json:"installed_at"`
	ActivatedAt time.Time `json:"activated_at,omitempty"`
	Error       string    `json:"error,omitempty"`
	Config      map[string]any `json:"config,omitempty"` // user-supplied config
	RuntimeDir  string    `json:"runtime_dir,omitempty"`
}
