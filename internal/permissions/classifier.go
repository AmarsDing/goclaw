package permissions

import (
	"fmt"
	"strings"
)

// Classifier determines the security class of a tool call based on its name
// and arguments. This enables mode-based permission defaults without
// hard-coding tool knowledge into the governor.
type Classifier struct {
	overrides map[string]SecurityClass // tool name → forced class
}

// NewClassifier creates a classifier with built-in heuristics.
func NewClassifier() *Classifier {
	return &Classifier{
		overrides: make(map[string]SecurityClass),
	}
}

// SetOverride forces a specific security class for a tool, bypassing heuristics.
func (c *Classifier) SetOverride(toolName string, class SecurityClass) {
	c.overrides[toolName] = class
}

// Classify determines the security classification for a tool call.
func (c *Classifier) Classify(toolName string, args map[string]any) *SecurityClassification {
	if class, ok := c.overrides[toolName]; ok {
		return &SecurityClassification{
			Class:       class,
			Description: "override: " + toolName,
			Risk:        riskLevel(class),
		}
	}

	class := classifyByName(toolName, args)
	return &SecurityClassification{
		Class:       class,
		Description: toolName,
		Risk:        riskLevel(class),
	}
}

func classifyByName(name string, args map[string]any) SecurityClass {
	lower := strings.ToLower(name)
	argText := strings.ToLower(flattenArgs(args))

	// Dangerous operations
	dangerousPatterns := []string{"delete", "remove", "drop", "destroy", "reset", "format"}
	for _, p := range dangerousPatterns {
		if strings.Contains(lower, p) {
			return ClassDangerous
		}
		if strings.Contains(argText, p) {
			return ClassDangerous
		}
	}

	// Path- and command-based escalation.
	dangerousArgs := []string{
		"rm -rf", "shutdown", "reboot", "mkfs", "del /f", "/etc/", "/root/", "system32",
	}
	for _, p := range dangerousArgs {
		if strings.Contains(argText, p) {
			return ClassDangerous
		}
	}

	// Execute operations
	executePatterns := []string{"exec", "shell", "bash", "command", "run", "terminal"}
	for _, p := range executePatterns {
		if strings.Contains(lower, p) {
			return ClassExecute
		}
	}

	// Network operations
	networkPatterns := []string{"fetch", "http", "web", "api", "request", "download", "upload", "curl"}
	for _, p := range networkPatterns {
		if strings.Contains(lower, p) {
			return ClassNetwork
		}
	}

	// Write operations
	writePatterns := []string{"write", "create", "update", "edit", "modify", "append", "save", "patch"}
	for _, p := range writePatterns {
		if strings.Contains(lower, p) {
			return ClassWriteFS
		}
	}

	// Read operations (default for anything else known-safe)
	readPatterns := []string{"read", "get", "list", "search", "find", "show", "view", "stat", "glob", "memory_search"}
	for _, p := range readPatterns {
		if strings.Contains(lower, p) {
			return ClassReadOnly
		}
	}

	// Unknown tools default to write (cautious default).
	return ClassWriteFS
}

func flattenArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for key, value := range args {
		parts = append(parts, key+"="+fmt.Sprint(value))
	}
	return strings.Join(parts, " ")
}

func riskLevel(class SecurityClass) string {
	switch class {
	case ClassReadOnly:
		return "low"
	case ClassWriteFS:
		return "medium"
	case ClassNetwork:
		return "medium"
	case ClassExecute:
		return "high"
	case ClassDangerous:
		return "high"
	default:
		return "medium"
	}
}
