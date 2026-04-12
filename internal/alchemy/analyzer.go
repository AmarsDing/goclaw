// Package alchemy provides the "炼化" (Alchemy) system that transforms
// GitHub repositories into goclaw plugins.
//
// Given a GitHub URL, the alchemy pipeline:
//  1. Clones and analyses the repository
//  2. Extracts capabilities (APIs, CLIs, libraries)
//  3. Generates a plugin wrapper (manifest + bridge code)
//  4. Tests the plugin in a sandbox
//  5. Publishes to the marketplace
package alchemy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RepoAnalysis is the result of analysing a GitHub repository.
type RepoAnalysis struct {
	URL          string       `json:"url"`
	Name         string       `json:"name"`
	Language     string       `json:"language"`     // primary language
	Languages    []string     `json:"languages"`    // all detected languages
	Description  string       `json:"description"`
	Structure    RepoStructure `json:"structure"`
	APIs         []APIEndpoint `json:"apis,omitempty"`
	CLIs         []CLICommand  `json:"clis,omitempty"`
	Dependencies []string     `json:"dependencies"`
	AnalysedAt   time.Time    `json:"analysed_at"`
}

// RepoStructure describes the repository's file organisation.
type RepoStructure struct {
	TotalFiles   int      `json:"total_files"`
	Directories  []string `json:"directories"`
	EntryPoints  []string `json:"entry_points"`  // main files, index files, etc.
	ConfigFiles  []string `json:"config_files"`  // package.json, go.mod, etc.
	HasTests     bool     `json:"has_tests"`
	HasDocs      bool     `json:"has_docs"`
	HasDockerfile bool    `json:"has_dockerfile"`
}

// APIEndpoint describes a discovered API endpoint.
type APIEndpoint struct {
	Method      string         `json:"method"` // GET, POST, etc.
	Path        string         `json:"path"`
	Description string         `json:"description"`
	Parameters  []APIParameter `json:"parameters,omitempty"`
}

// APIParameter describes an API endpoint parameter.
type APIParameter struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	In       string `json:"in"` // "path", "query", "body"
}

// CLICommand describes a discovered CLI command.
type CLICommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Usage       string `json:"usage"`
}

// Analyzer clones and analyses GitHub repositories.
type Analyzer struct {
	workDir string // temporary directory for cloning
}

// NewAnalyzer creates a repository analyser.
func NewAnalyzer(workDir string) *Analyzer {
	return &Analyzer{workDir: workDir}
}

// Analyze clones a GitHub repository and analyses its structure.
func (a *Analyzer) Analyze(ctx context.Context, repoURL string) (*RepoAnalysis, error) {
	name := extractRepoName(repoURL)
	cloneDir := filepath.Join(a.workDir, name)

	// Clean up any previous clone.
	_ = os.RemoveAll(cloneDir)

	slog.Info("alchemy: cloning repository", "url", repoURL)
	if err := a.clone(ctx, repoURL, cloneDir); err != nil {
		return nil, fmt.Errorf("clone %s: %w", repoURL, err)
	}

	analysis := &RepoAnalysis{
		URL:        repoURL,
		Name:       name,
		AnalysedAt: time.Now(),
	}

	// Analyse structure.
	analysis.Structure = a.analyseStructure(cloneDir)
	analysis.Language, analysis.Languages = a.detectLanguages(cloneDir)
	analysis.Dependencies = a.detectDependencies(cloneDir)

	slog.Info("alchemy: analysis complete",
		"name", name,
		"language", analysis.Language,
		"files", analysis.Structure.TotalFiles)

	return analysis, nil
}

func (a *Analyzer) clone(ctx context.Context, url, target string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", url, target)
	return cmd.Run()
}

func (a *Analyzer) analyseStructure(dir string) RepoStructure {
	s := RepoStructure{}
	dirSet := make(map[string]bool)

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if strings.HasPrefix(rel, ".git") {
			return filepath.SkipDir
		}

		if info.IsDir() {
			dirSet[rel] = true
			return nil
		}

		s.TotalFiles++

		base := filepath.Base(path)
		lower := strings.ToLower(base)

		// Detect entry points.
		entryNames := []string{"main.go", "main.py", "index.js", "index.ts", "app.py", "cli.py", "cmd"}
		for _, e := range entryNames {
			if lower == e || strings.HasPrefix(lower, e+".") {
				s.EntryPoints = append(s.EntryPoints, rel)
			}
		}

		// Detect config files.
		configNames := []string{"package.json", "go.mod", "cargo.toml", "pyproject.toml",
			"requirements.txt", "dockerfile", "docker-compose.yml", "makefile"}
		for _, c := range configNames {
			if lower == c {
				s.ConfigFiles = append(s.ConfigFiles, rel)
			}
		}

		if lower == "dockerfile" {
			s.HasDockerfile = true
		}

		// Detect tests and docs.
		if strings.Contains(lower, "test") || strings.Contains(lower, "_test.") {
			s.HasTests = true
		}
		if strings.HasPrefix(rel, "docs") || lower == "readme.md" {
			s.HasDocs = true
		}

		return nil
	})

	for d := range dirSet {
		s.Directories = append(s.Directories, d)
	}

	return s
}

func (a *Analyzer) detectLanguages(dir string) (string, []string) {
	langCount := make(map[string]int)

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if strings.HasPrefix(rel, ".git") {
			return filepath.SkipDir
		}

		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".go":
			langCount["Go"]++
		case ".py":
			langCount["Python"]++
		case ".js":
			langCount["JavaScript"]++
		case ".ts", ".tsx":
			langCount["TypeScript"]++
		case ".rs":
			langCount["Rust"]++
		case ".java":
			langCount["Java"]++
		case ".rb":
			langCount["Ruby"]++
		case ".php":
			langCount["PHP"]++
		case ".sh", ".bash":
			langCount["Shell"]++
		}
		return nil
	})

	var primary string
	maxCount := 0
	var languages []string

	for lang, count := range langCount {
		languages = append(languages, lang)
		if count > maxCount {
			maxCount = count
			primary = lang
		}
	}

	return primary, languages
}

func (a *Analyzer) detectDependencies(dir string) []string {
	var deps []string

	// Check for go.mod.
	if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "require") || strings.Contains(line, "/") {
				if parts := strings.Fields(line); len(parts) >= 1 && strings.Contains(parts[0], "/") {
					deps = append(deps, parts[0])
				}
			}
		}
	}

	// Check for package.json.
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err == nil {
		deps = append(deps, "npm (package.json found)")
	}

	// Check for requirements.txt.
	if data, err := os.ReadFile(filepath.Join(dir, "requirements.txt")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				deps = append(deps, line)
			}
		}
	}

	return deps
}

func extractRepoName(url string) string {
	url = strings.TrimSuffix(url, ".git")
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "unknown"
}
