package alchemy

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGenerateGoWrapper_Compiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	g := NewGenerator(dir)
	analysis := &RepoAnalysis{
		Name:     "demo",
		URL:      "https://example.com/demo",
		Language: "Go",
	}
	res, err := g.Generate(t.Context(), analysis)
	if err != nil {
		t.Fatal(err)
	}
	if res.WrapperPath == "" {
		t.Fatal("expected wrapper path")
	}
	outBin := filepath.Join(dir, "wrap")
	if runtime.GOOS == "windows" {
		outBin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", outBin, res.WrapperPath)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Dir = filepath.Dir(res.WrapperPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
}
