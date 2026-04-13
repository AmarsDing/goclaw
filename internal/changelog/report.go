package changelog

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed subsystems.yaml
var subsystemsYAML []byte

type subsystemFile struct {
	Mappings []struct {
		Prefix string `yaml:"prefix"`
		Name   string `yaml:"name"`
	} `yaml:"mappings"`
}

// ReportOptions configures git range and repo root.
type ReportOptions struct {
	RepoRoot string // default: current dir
	FromRef  string // e.g. v1.0.0 or commit
	ToRef    string // e.g. HEAD
}

// FileChange is one path from git name-status.
type FileChange struct {
	Status string
	Path   string
}

// Report contains parsed git output and subsystem grouping.
type Report struct {
	FromRef     string
	ToRef       string
	Commits     []string
	Files       []FileChange
	BySubsystem map[string][]FileChange
	// DiffStat is raw output of `git diff --stat` for the range (may be empty on error).
	DiffStat string
	// RiskTags are heuristic labels (e.g. migration, security-sensitive-path); not authoritative.
	RiskTags []string
}

// Generate runs git log / diff --name-status and builds a Markdown report.
func Generate(opts ReportOptions) (*Report, string, error) {
	root := opts.RepoRoot
	if root == "" {
		root = "."
	}
	from := opts.FromRef
	to := opts.ToRef
	if to == "" {
		to = "HEAD"
	}
	if from == "" {
		from = to + "~1"
	}

	rng := from + ".." + to

	outLog, err := git(root, "log", "--oneline", "--no-decorate", rng)
	if err != nil {
		return nil, "", fmt.Errorf("git log: %w", err)
	}
	outNames, err := git(root, "diff", "--name-status", rng)
	if err != nil {
		return nil, "", fmt.Errorf("git diff: %w", err)
	}

	var statOut string
	if s, err := git(root, "diff", "--stat", rng); err == nil {
		statOut = strings.TrimSpace(s)
	}

	var commits []string
	for _, line := range strings.Split(strings.TrimSpace(outLog), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			commits = append(commits, line)
		}
	}

	var files []FileChange
	for _, line := range strings.Split(strings.TrimSpace(outNames), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		path := parts[len(parts)-1]
		if len(parts) == 3 && (status == "R100" || strings.HasPrefix(status, "R")) {
			path = parts[2]
		}
		files = append(files, FileChange{Status: status, Path: path})
	}

	by := groupBySubsystem(files)
	tags := heuristicRiskTags(files)
	sort.Strings(tags)

	rep := &Report{
		FromRef:     from,
		ToRef:       to,
		Commits:     commits,
		Files:       files,
		BySubsystem: by,
		DiffStat:    statOut,
		RiskTags:    tags,
	}
	md := renderMarkdown(rep)
	return rep, md, nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func groupBySubsystem(files []FileChange) map[string][]FileChange {
	var sf subsystemFile
	if err := yaml.Unmarshal(subsystemsYAML, &sf); err != nil {
		// Embedded file must parse; fail closed to "Other" only if empty mappings
		_ = err
	}
	out := make(map[string][]FileChange)
	for _, f := range files {
		label := "Other"
		p := filepath.ToSlash(f.Path)
		for _, m := range sf.Mappings {
			prefix := strings.TrimSuffix(m.Prefix, "/") + "/"
			if strings.HasPrefix(p, prefix) || p == strings.TrimSuffix(m.Prefix, "/") {
				label = m.Name
				break
			}
		}
		out[label] = append(out[label], f)
	}
	return out
}

func renderMarkdown(rep *Report) string {
	var b strings.Builder
	b.WriteString("# goclaw changelog\n\n")
	b.WriteString(fmt.Sprintf("**Range:** `%s` → `%s`\n\n", rep.FromRef, rep.ToRef))

	if migrationTouched(rep.Files) {
		b.WriteString("> **Migration:** `migrations/` changed — review SQL and run migrations as needed.\n\n")
	}

	if len(rep.RiskTags) > 0 {
		b.WriteString("## Risk flags (heuristic)\n\n")
		b.WriteString("_Automated path-based tags; verify manually._\n\n")
		for _, t := range rep.RiskTags {
			b.WriteString(fmt.Sprintf("- `%s`\n", t))
		}
		b.WriteString("\n")
	}

	if rep.DiffStat != "" {
		b.WriteString("## Diff stat\n\n")
		b.WriteString("```\n")
		b.WriteString(rep.DiffStat)
		b.WriteString("\n```\n\n")
	}

	b.WriteString("## Commits\n\n")
	if len(rep.Commits) == 0 {
		b.WriteString("_No commits in range._\n\n")
	} else {
		for _, c := range rep.Commits {
			b.WriteString("- ")
			b.WriteString(c)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Files by subsystem\n\n")
	keys := make([]string, 0, len(rep.BySubsystem))
	for k := range rep.BySubsystem {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("### ")
		b.WriteString(k)
		b.WriteString("\n\n")
		for _, f := range rep.BySubsystem[k] {
			b.WriteString(fmt.Sprintf("- `%s` %s\n", f.Status, f.Path))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func migrationTouched(files []FileChange) bool {
	for _, f := range files {
		if strings.HasPrefix(filepath.ToSlash(f.Path), "migrations/") {
			return true
		}
	}
	return false
}

// heuristicRiskTags assigns coarse labels from paths (FR-C2b-style); may false-positive.
func heuristicRiskTags(files []FileChange) []string {
	if len(files) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var tags []string
	add := func(s string) {
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		tags = append(tags, s)
	}

	docsOnly := len(files) > 0
	var configTouched bool
	for _, f := range files {
		p := filepath.ToSlash(f.Path)
		if !strings.HasPrefix(p, "docs/") {
			docsOnly = false
		}
		if strings.HasPrefix(p, "internal/config/") || strings.HasPrefix(p, "migrations/") {
			configTouched = true
		}
		if strings.HasPrefix(p, "migrations/") {
			add("migration")
		}
		if strings.HasPrefix(p, "pkg/") {
			add("public-api-surface")
		}
		if strings.HasPrefix(p, "internal/permissions/") || strings.HasPrefix(p, "internal/tools/") ||
			strings.Contains(p, "/permissions/") {
			add("security-sensitive-path")
		}
		if strings.HasPrefix(p, "internal/gateway/") || strings.HasPrefix(p, "internal/http/") {
			add("gateway-http")
		}
		if strings.HasPrefix(p, "ui/web/") {
			add("frontend")
		}
	}
	if docsOnly {
		add("docs-only")
	}
	if configTouched {
		add("config-or-schema")
	}
	return tags
}

// ReportJSON returns machine-readable JSON for CI.
func ReportJSON(rep *Report) ([]byte, error) {
	return json.MarshalIndent(rep, "", "  ")
}
