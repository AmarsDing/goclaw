package tools

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// SandboxCwd maps the current effective workspace (from context) to its
// corresponding path inside the sandbox container. The sandbox mounts the
// global workspace root at containerBase (usually "/workspace"). This function
// computes the relative path from globalWorkspace to the context workspace
// and joins it with containerBase.
//
// Example: globalWorkspace="/app/workspace", ctx workspace="/app/workspace/agent-a/user-123"
// returns "/workspace/agent-a/user-123".
func SandboxCwd(ctx context.Context, globalWorkspace, containerBase string) (string, error) {
	ws := ToolWorkspaceFromCtx(ctx)
	if ws == "" {
		return containerPath(containerBase), nil
	}

	rel, err := filepath.Rel(globalWorkspace, ws)
	if err != nil || strings.HasPrefix(filepath.Clean(rel), "..") {
		return "", fmt.Errorf("workspace %q is outside global mount %q", ws, globalWorkspace)
	}

	if rel == "." {
		return containerPath(containerBase), nil
	}
	return path.Join(containerPath(containerBase), filepath.ToSlash(rel)), nil
}

// ResolveSandboxPath resolves a tool-provided path (relative or absolute)
// against the sandbox container CWD. Container paths are always POSIX paths
// even when the host runs on Windows.
func ResolveSandboxPath(p, containerCwd string) string {
	p = filepath.ToSlash(p)
	containerCwd = containerPath(containerCwd)
	if strings.HasPrefix(p, "/") {
		return path.Clean(p)
	}
	return path.Clean(path.Join(containerCwd, p))
}

func containerPath(p string) string {
	if p == "" {
		return "/"
	}
	return path.Clean(filepath.ToSlash(p))
}
