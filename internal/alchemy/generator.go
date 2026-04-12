package alchemy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/plugins"
)

// Generator creates plugin manifests and wrapper code from repository analysis.
type Generator struct {
	outputDir string
}

// NewGenerator creates a plugin generator.
func NewGenerator(outputDir string) *Generator {
	return &Generator{outputDir: outputDir}
}

// GenerateResult is the output of plugin generation.
type GenerateResult struct {
	PluginName   string           `json:"plugin_name"`
	Manifest     plugins.Manifest `json:"manifest"`
	ManifestPath string           `json:"manifest_path"`
	WrapperPath  string           `json:"wrapper_path,omitempty"`
	GeneratedAt  time.Time        `json:"generated_at"`
}

// Generate creates a plugin from a repository analysis.
func (g *Generator) Generate(ctx context.Context, analysis *RepoAnalysis) (*GenerateResult, error) {
	pluginName := sanitizePluginName(analysis.Name)
	pluginDir := filepath.Join(g.outputDir, pluginName)

	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return nil, fmt.Errorf("create plugin dir: %w", err)
	}

	manifest := g.buildManifest(analysis, pluginName)

	// Write manifest.
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	// Generate wrapper based on language.
	wrapperPath := ""
	switch analysis.Language {
	case "Go":
		wrapperPath, err = g.generateGoWrapper(pluginDir, analysis, pluginName)
	case "Python":
		wrapperPath, err = g.generatePythonWrapper(pluginDir, analysis, pluginName)
	case "JavaScript", "TypeScript":
		wrapperPath, err = g.generateNodeWrapper(pluginDir, analysis, pluginName)
	default:
		wrapperPath, err = g.generateShellWrapper(pluginDir, analysis, pluginName)
	}
	if err != nil {
		slog.Warn("alchemy: wrapper generation failed", "err", err)
	}

	slog.Info("alchemy: plugin generated",
		"name", pluginName, "language", analysis.Language)

	return &GenerateResult{
		PluginName:   pluginName,
		Manifest:     manifest,
		ManifestPath: manifestPath,
		WrapperPath:  wrapperPath,
		GeneratedAt:  time.Now(),
	}, nil
}

func (g *Generator) buildManifest(analysis *RepoAnalysis, name string) plugins.Manifest {
	m := plugins.Manifest{
		Name:        name,
		Version:     "0.1.0",
		Description: fmt.Sprintf("Auto-generated plugin from %s", analysis.URL),
		Author:      "alchemy",
		Repository:  analysis.URL,
		Transport:   "stdio",
	}

	// Map APIs to tools.
	for _, api := range analysis.APIs {
		toolName := sanitizePluginName(api.Path)
		params := make(map[string]any)
		for _, p := range api.Parameters {
			params[p.Name] = map[string]any{
				"type":        p.Type,
				"description": p.Name,
			}
		}
		m.Capabilities.Tools = append(m.Capabilities.Tools, plugins.ToolDecl{
			Name:        fmt.Sprintf("%s_%s", name, toolName),
			Description: api.Description,
			Parameters:  params,
			ReadOnly:    api.Method == "GET",
		})
	}

	// Map CLIs to tools.
	for _, cli := range analysis.CLIs {
		m.Capabilities.Tools = append(m.Capabilities.Tools, plugins.ToolDecl{
			Name:        fmt.Sprintf("%s_%s", name, sanitizePluginName(cli.Name)),
			Description: cli.Description,
			Parameters: map[string]any{
				"args": map[string]any{
					"type":        "string",
					"description": "command arguments",
				},
			},
		})
	}

	// Default tool if none extracted.
	if len(m.Capabilities.Tools) == 0 {
		m.Capabilities.Tools = append(m.Capabilities.Tools, plugins.ToolDecl{
			Name:        name + "_run",
			Description: fmt.Sprintf("Run %s", analysis.Name),
			Parameters: map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "input for the tool",
				},
			},
		})
	}

	// Set permissions based on analysis.
	m.Permissions.FileSystem = true
	m.Permissions.Shell = true
	if containsNetworkDep(analysis.Dependencies) {
		m.Permissions.Network = true
	}

	// Set entrypoint based on language.
	switch analysis.Language {
	case "Go":
		m.Entrypoint = fmt.Sprintf("go run %s/.", name)
	case "Python":
		m.Entrypoint = fmt.Sprintf("python %s/wrapper.py", name)
	case "JavaScript", "TypeScript":
		m.Entrypoint = fmt.Sprintf("node %s/wrapper.js", name)
	default:
		m.Entrypoint = fmt.Sprintf("sh %s/wrapper.sh", name)
	}

	return m
}

func (g *Generator) generateGoWrapper(dir string, analysis *RepoAnalysis, name string) (string, error) {
	path := filepath.Join(dir, "wrapper.go")
	content := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
)

// Auto-generated wrapper for %s
// Source: %s

func main() {
	fmt.Fprintf(os.Stderr, "%s plugin started\n")
	// TODO: Implement MCP stdio bridge for %s
	// Read JSON-RPC from stdin, dispatch to wrapped functionality, write results to stdout.
	select {}
}
`, analysis.Name, analysis.URL, name, analysis.Name)

	return path, os.WriteFile(path, []byte(content), 0o644)
}

func (g *Generator) generatePythonWrapper(dir string, analysis *RepoAnalysis, name string) (string, error) {
	path := filepath.Join(dir, "wrapper.py")
	content := fmt.Sprintf(`#!/usr/bin/env python3
"""Auto-generated wrapper for %s.

Source: %s
"""
import sys
import json

def main():
    print(f"%s plugin started", file=sys.stderr)
    # TODO: Implement MCP stdio bridge
    # Read JSON-RPC from stdin, dispatch to wrapped functionality, write results to stdout.
    for line in sys.stdin:
        request = json.loads(line)
        response = {"jsonrpc": "2.0", "id": request.get("id"), "result": {"status": "not_implemented"}}
        print(json.dumps(response), flush=True)

if __name__ == "__main__":
    main()
`, analysis.Name, analysis.URL, name)

	return path, os.WriteFile(path, []byte(content), 0o644)
}

func (g *Generator) generateNodeWrapper(dir string, analysis *RepoAnalysis, name string) (string, error) {
	path := filepath.Join(dir, "wrapper.js")
	content := fmt.Sprintf(`#!/usr/bin/env node
// Auto-generated wrapper for %s
// Source: %s

const readline = require('readline');

const rl = readline.createInterface({ input: process.stdin });
console.error('%s plugin started');

rl.on('line', (line) => {
  try {
    const request = JSON.parse(line);
    const response = { jsonrpc: '2.0', id: request.id, result: { status: 'not_implemented' } };
    console.log(JSON.stringify(response));
  } catch (e) {
    console.error('Parse error:', e.message);
  }
});
`, analysis.Name, analysis.URL, name)

	return path, os.WriteFile(path, []byte(content), 0o644)
}

func (g *Generator) generateShellWrapper(dir string, analysis *RepoAnalysis, name string) (string, error) {
	path := filepath.Join(dir, "wrapper.sh")
	content := fmt.Sprintf(`#!/bin/bash
# Auto-generated wrapper for %s
# Source: %s

echo "%s plugin started" >&2

# TODO: Implement JSON-RPC stdio bridge
while IFS= read -r line; do
  echo "{\"jsonrpc\":\"2.0\",\"result\":{\"status\":\"not_implemented\"}}"
done
`, analysis.Name, analysis.URL, name)

	return path, os.WriteFile(path, []byte(content), 0o644)
}

func sanitizePluginName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, "/", "_")
	var clean strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			clean.WriteRune(r)
		}
	}
	result := clean.String()
	if result == "" {
		return "plugin"
	}
	return result
}

func containsNetworkDep(deps []string) bool {
	networkIndicators := []string{"http", "net", "request", "axios", "fetch", "curl", "grpc", "websocket"}
	for _, dep := range deps {
		lower := strings.ToLower(dep)
		for _, indicator := range networkIndicators {
			if strings.Contains(lower, indicator) {
				return true
			}
		}
	}
	return false
}
