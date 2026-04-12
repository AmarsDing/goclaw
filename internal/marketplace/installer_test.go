package marketplace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerInstall_WritesInstalledManifest(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	catalog := NewCatalog()
	pkg := Package{
		ID:      "pkg-1",
		Name:    "hello-skill",
		Type:    TypeSkill,
		Version: "1.2.3",
		Ref:     "v1.2.3",
	}
	if err := catalog.Register(context.Background(), pkg); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	installer := NewInstaller(dataDir, catalog, nil)
	result, err := installer.Install(context.Background(), pkg.ID)
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}
	if result.Ref != pkg.Ref {
		t.Fatalf("result.Ref = %q, want %q", result.Ref, pkg.Ref)
	}

	manifestPath := filepath.Join(dataDir, "packages", "installed.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", manifestPath, err)
	}
	if !strings.Contains(string(data), "\"package_id\": \"pkg-1\"") {
		t.Fatalf("installed manifest missing package id: %s", string(data))
	}
	if !strings.Contains(string(data), "\"ref\": \"v1.2.3\"") {
		t.Fatalf("installed manifest missing ref: %s", string(data))
	}

	listed, err := installer.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled() error: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("len(ListInstalled()) = %d, want 1", len(listed))
	}
	if listed[0].PackageID != pkg.ID || listed[0].Version != pkg.Version {
		t.Fatalf("listed[0] = %#v", listed[0])
	}
}

func TestInstallerInstall_ChecksOutRequestedRef(t *testing.T) {
	t.Parallel()

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	writeFile(t, filepath.Join(repoDir, "skill.txt"), "v1")
	runGitWithCommitEnv(t, repoDir, "add", ".")
	runGitWithCommitEnv(t, repoDir, "commit", "-m", "v1")
	runGit(t, repoDir, "tag", "v1.0.0")

	writeFile(t, filepath.Join(repoDir, "skill.txt"), "v2")
	runGitWithCommitEnv(t, repoDir, "add", ".")
	runGitWithCommitEnv(t, repoDir, "commit", "-m", "v2")

	dataDir := t.TempDir()
	catalog := NewCatalog()
	pkg := Package{
		ID:         "pkg-ref",
		Name:       "ref-skill",
		Type:       TypeSkill,
		Version:    "1.0.0",
		Repository: repoDir,
		Ref:        "v1.0.0",
	}
	if err := catalog.Register(context.Background(), pkg); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	installer := NewInstaller(dataDir, catalog, nil)
	result, err := installer.Install(context.Background(), pkg.ID)
	if err != nil {
		t.Fatalf("Install() error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(result.Path, "skill.txt"))
	if err != nil {
		t.Fatalf("ReadFile(skill.txt) error: %v", err)
	}
	if strings.TrimSpace(string(got)) != "v1" {
		t.Fatalf("installed content = %q, want v1", string(got))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func runGitWithCommitEnv(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=GoClaw Test",
		"GIT_AUTHOR_EMAIL=test@goclaw.local",
		"GIT_COMMITTER_NAME=GoClaw Test",
		"GIT_COMMITTER_EMAIL=test@goclaw.local",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
