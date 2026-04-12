package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	shellwords "github.com/mattn/go-shellwords"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// Registry manages plugin lifecycle: install, configure, activate, deactivate, uninstall.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]*Instance // name → instance
	dataDir string               // directory where plugins are stored

	// Callbacks invoked during lifecycle transitions.
	onActivate   func(inst *Instance) error
	onDeactivate func(inst *Instance) error
}

// NewRegistry creates a plugin registry.
func NewRegistry(dataDir string) *Registry {
	return &Registry{
		plugins: make(map[string]*Instance),
		dataDir: dataDir,
	}
}

// OnActivate registers a callback invoked when a plugin is activated.
func (r *Registry) OnActivate(fn func(inst *Instance) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onActivate = fn
}

// OnDeactivate registers a callback invoked when a plugin is deactivated.
func (r *Registry) OnDeactivate(fn func(inst *Instance) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onDeactivate = fn
}

// Install installs a plugin from its manifest. The plugin files should already
// exist in the data directory.
func (r *Registry) Install(_ context.Context, manifest Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if manifest.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if _, exists := r.plugins[manifest.Name]; exists {
		return fmt.Errorf("plugin %q already installed", manifest.Name)
	}

	// Check dependencies.
	for _, dep := range manifest.Dependencies {
		if _, ok := r.plugins[dep]; !ok {
			return fmt.Errorf("missing dependency: %s", dep)
		}
	}

	inst := &Instance{
		Manifest:    manifest,
		State:       StateInstalled,
		InstalledAt: time.Now(),
		RuntimeDir:  filepath.Join(r.dataDir, manifest.Name),
	}

	r.plugins[manifest.Name] = inst
	slog.Info("plugin installed", "name", manifest.Name, "version", manifest.Version)
	return nil
}

// Configure sets user-supplied configuration for a plugin.
func (r *Registry) Configure(_ context.Context, name string, config map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	inst, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	if inst.State != StateInstalled && inst.State != StateConfigured && inst.State != StateInactive {
		return fmt.Errorf("plugin %q cannot be configured in state %s", name, inst.State)
	}

	inst.Config = config
	inst.State = StateConfigured
	return nil
}

// Activate starts a plugin. It must be installed (and optionally configured) first.
func (r *Registry) Activate(_ context.Context, name string) error {
	r.mu.Lock()
	inst, ok := r.plugins[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("plugin %q not found", name)
	}
	if inst.State == StateActive {
		r.mu.Unlock()
		return nil
	}
	if inst.State != StateInstalled && inst.State != StateConfigured && inst.State != StateInactive {
		r.mu.Unlock()
		return fmt.Errorf("plugin %q cannot be activated from state %s", name, inst.State)
	}

	onActivate := r.onActivate
	r.mu.Unlock()

	if onActivate != nil {
		if err := onActivate(inst); err != nil {
			r.mu.Lock()
			inst.State = StateError
			inst.Error = err.Error()
			r.mu.Unlock()
			return fmt.Errorf("activate %q: %w", name, err)
		}
	}

	r.mu.Lock()
	inst.State = StateActive
	inst.ActivatedAt = time.Now()
	inst.Error = ""
	r.mu.Unlock()

	slog.Info("plugin activated", "name", name)
	return nil
}

// Deactivate stops a plugin without uninstalling it.
func (r *Registry) Deactivate(_ context.Context, name string) error {
	r.mu.Lock()
	inst, ok := r.plugins[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("plugin %q not found", name)
	}
	if inst.State != StateActive {
		r.mu.Unlock()
		return nil
	}

	onDeactivate := r.onDeactivate
	r.mu.Unlock()

	if onDeactivate != nil {
		if err := onDeactivate(inst); err != nil {
			slog.Warn("deactivate error", "plugin", name, "err", err)
		}
	}

	r.mu.Lock()
	inst.State = StateInactive
	r.mu.Unlock()

	slog.Info("plugin deactivated", "name", name)
	return nil
}

// Uninstall removes a plugin completely.
func (r *Registry) Uninstall(_ context.Context, name string) error {
	r.mu.Lock()
	inst, ok := r.plugins[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("plugin %q not found", name)
	}

	if inst.State == StateActive {
		r.mu.Unlock()
		if err := r.Deactivate(context.Background(), name); err != nil {
			return err
		}
		r.mu.Lock()
	}

	// Check reverse dependencies.
	for n, p := range r.plugins {
		if n == name {
			continue
		}
		for _, dep := range p.Manifest.Dependencies {
			if dep == name && p.State != StateUninstalled {
				r.mu.Unlock()
				return fmt.Errorf("cannot uninstall %q: required by %q", name, n)
			}
		}
	}

	delete(r.plugins, name)
	r.mu.Unlock()

	slog.Info("plugin uninstalled", "name", name)
	return nil
}

// List returns all installed plugins.
func (r *Registry) List() []Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Instance, 0, len(r.plugins))
	for _, inst := range r.plugins {
		out = append(out, *inst)
	}
	return out
}

// Get returns a single plugin by name.
func (r *Registry) Get(name string) (*Instance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inst, ok := r.plugins[name]
	if !ok {
		return nil, false
	}
	cp := *inst
	return &cp, true
}

// Active returns only active plugins.
func (r *Registry) Active() []Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []Instance
	for _, inst := range r.plugins {
		if inst.State == StateActive {
			out = append(out, *inst)
		}
	}
	return out
}

// LoadFromDir scans a directory for plugin manifests (plugin.json files)
// and installs them.
func (r *Registry) LoadFromDir(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read plugin dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(dir, entry.Name(), "plugin.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // skip directories without manifest
		}

		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			slog.Warn("invalid plugin manifest", "path", manifestPath, "err", err)
			continue
		}

		if err := r.Install(ctx, manifest); err != nil {
			slog.Warn("plugin install failed", "name", manifest.Name, "err", err)
			continue
		}
		r.mu.Lock()
		if inst, ok := r.plugins[manifest.Name]; ok {
			inst.RuntimeDir = filepath.Join(dir, entry.Name())
		}
		r.mu.Unlock()
	}

	return nil
}

// ExecuteTool runs a tool exposed by an active plugin via its entrypoint.
// The current contract is a lightweight stdio request/response where the plugin
// reads one JSON line from stdin and emits either a tools.Result-like JSON
// object, a JSON-RPC result, or plain stdout text.
func (r *Registry) ExecuteTool(ctx context.Context, pluginName, toolName string, args map[string]any) (*tools.Result, error) {
	r.mu.RLock()
	inst, ok := r.plugins[pluginName]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("plugin %q not found", pluginName)
	}
	if inst.State != StateActive {
		return nil, fmt.Errorf("plugin %q is not active", pluginName)
	}
	if inst.Manifest.Entrypoint == "" {
		return nil, fmt.Errorf("plugin %q has no entrypoint", pluginName)
	}

	parser := shellwords.NewParser()
	fields, err := parser.Parse(inst.Manifest.Entrypoint)
	if err != nil {
		return nil, fmt.Errorf("parse entrypoint: %w", err)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("plugin %q entrypoint is empty", pluginName)
	}

	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
	cmd.Dir = pluginWorkingDir(inst)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	request := map[string]any{
		"tool":      toolName,
		"arguments": args,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start entrypoint: %w", err)
	}

	go func() {
		defer stdin.Close()
		_, _ = stdin.Write(append(payload, '\n'))
	}()

	stdoutText := readAllText(stdout)
	stderrText := readAllText(stderr)
	if err := cmd.Wait(); err != nil {
		if stderrText != "" {
			return nil, fmt.Errorf("plugin process failed: %w: %s", err, stderrText)
		}
		return nil, fmt.Errorf("plugin process failed: %w", err)
	}

	if result := parsePluginResult(stdoutText); result != nil {
		return result, nil
	}
	if trimmed := strings.TrimSpace(stdoutText); trimmed != "" {
		return tools.NewResult(trimmed), nil
	}
	if stderrText != "" {
		return tools.NewResult(stderrText), nil
	}
	return tools.NewResult("plugin completed with empty output"), nil
}

func pluginWorkingDir(inst *Instance) string {
	if inst == nil {
		return ""
	}
	runtimeDir := inst.RuntimeDir
	if runtimeDir == "" {
		return ""
	}
	nameRef := inst.Manifest.Name + string(filepath.Separator)
	if strings.Contains(inst.Manifest.Entrypoint, inst.Manifest.Name+"/") ||
		strings.Contains(inst.Manifest.Entrypoint, inst.Manifest.Name+"\\") ||
		strings.Contains(inst.Manifest.Entrypoint, nameRef) {
		return filepath.Dir(runtimeDir)
	}
	return runtimeDir
}

func readAllText(pipe any) string {
	switch r := pipe.(type) {
	case interface{ Read([]byte) (int, error) }:
		var b strings.Builder
		scanner := bufio.NewScanner(r)
		first := true
		for scanner.Scan() {
			if !first {
				b.WriteByte('\n')
			}
			b.WriteString(scanner.Text())
			first = false
		}
		return b.String()
	default:
		return ""
	}
}

func parsePluginResult(stdout string) *tools.Result {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if result := parsePluginResultLine(line); result != nil {
			return result
		}
	}
	return nil
}

func parsePluginResultLine(line string) *tools.Result {
	var direct struct {
		ForLLM  string `json:"for_llm"`
		ForUser string `json:"for_user"`
		Silent  bool   `json:"silent"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal([]byte(line), &direct); err == nil && (direct.ForLLM != "" || direct.ForUser != "" || direct.IsError || direct.Silent) {
		return &tools.Result{
			ForLLM:  direct.ForLLM,
			ForUser: direct.ForUser,
			Silent:  direct.Silent,
			IsError: direct.IsError,
		}
	}

	var rpc struct {
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &rpc); err == nil {
		if rpc.Error != nil {
			return tools.ErrorResult(fmt.Sprintf("%v", rpc.Error))
		}
		if len(rpc.Result) == 0 {
			return nil
		}
		if len(rpc.Result) > 0 && rpc.Result[0] == '"' {
			var text string
			if err := json.Unmarshal(rpc.Result, &text); err == nil {
				return tools.NewResult(text)
			}
		}
		var nested struct {
			ForLLM  string `json:"for_llm"`
			ForUser string `json:"for_user"`
			Silent  bool   `json:"silent"`
			IsError bool   `json:"is_error"`
			Status  string `json:"status"`
		}
		if err := json.Unmarshal(rpc.Result, &nested); err == nil {
			if nested.ForLLM != "" || nested.ForUser != "" || nested.Status != "" || nested.IsError || nested.Silent {
				result := nested.ForLLM
				if result == "" {
					result = nested.Status
				}
				return &tools.Result{
					ForLLM:  result,
					ForUser: nested.ForUser,
					Silent:  nested.Silent,
					IsError: nested.IsError,
				}
			}
		}
	}
	return nil
}
