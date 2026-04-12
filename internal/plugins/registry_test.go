package plugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryExecuteTool_StdioPlugin(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "echoer")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}

	manifest := `{
  "name": "echoer",
  "version": "0.1.0",
  "description": "echo plugin",
  "author": "test",
  "entrypoint": "go run main.go",
  "transport": "stdio",
  "capabilities": {
    "tools": [
      {
        "name": "echo",
        "description": "echo tool",
        "parameters": {
          "message": { "type": "string" }
        }
      }
    ]
  },
  "permissions": {}
}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	mainGo := `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			Tool      string         ` + "`json:\"tool\"`" + `
			Arguments map[string]any ` + "`json:\"arguments\"`" + `
		}
		_ = json.Unmarshal(scanner.Bytes(), &req)
		msg, _ := req.Arguments["message"].(string)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"for_llm": fmt.Sprintf("%s:%s", req.Tool, msg),
		})
		return
	}
}`
	if err := os.WriteFile(filepath.Join(pluginDir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	reg := NewRegistry(root)
	if err := reg.LoadFromDir(context.Background(), root); err != nil {
		t.Fatalf("load plugins: %v", err)
	}
	if err := reg.Activate(context.Background(), "echoer"); err != nil {
		t.Fatalf("activate plugin: %v", err)
	}

	result, err := reg.ExecuteTool(context.Background(), "echoer", "echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("execute plugin tool: %v", err)
	}
	if result == nil {
		t.Fatal("expected plugin result")
	}
	if got, want := result.ForLLM, "echo:hello"; got != want {
		t.Fatalf("ForLLM = %q, want %q", got, want)
	}
}
