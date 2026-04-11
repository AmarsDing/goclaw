package http

import (
	"archive/zip"
	"path"
	"sort"
	"strings"
)

// skillZipRoot identifies one skill inside a ZIP: stripPrefix is removed from each entry
// before writing (e.g. "foo/" or "skills/foo/"). Empty means SKILL.md at archive root.
type skillZipRoot struct {
	stripPrefix string
	skillMD     *zip.File
}

func normZipEntryName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	return strings.TrimPrefix(strings.TrimSpace(name), "./")
}

func zipSkillByteSize(files []*zip.File, stripPrefix string) int64 {
	var total int64
	for _, f := range files {
		if f.FileInfo().IsDir() {
			continue
		}
		n := normZipEntryName(f.Name)
		if stripPrefix != "" {
			if !strings.HasPrefix(n, stripPrefix) {
				continue
			}
		}
		total += int64(f.UncompressedSize64)
	}
	return total
}

// discoverSkillZipRoots finds one or more skills in a ZIP:
//   - SKILL.md at root (single)
//   - foo/SKILL.md for one or more top-level dirs (single or multi)
//   - wrapper/foo/SKILL.md for one wrapper dir and one or more skill dirs (single or multi)
func discoverSkillZipRoots(files []*zip.File) ([]skillZipRoot, string) {
	type skillEntry struct {
		path  string
		file  *zip.File
		parts []string
	}
	var entries []skillEntry
	for _, f := range files {
		if f.FileInfo().IsDir() {
			continue
		}
		n := normZipEntryName(f.Name)
		if path.Base(n) != "SKILL.md" {
			continue
		}
		parts := strings.Split(n, "/")
		if len(parts) > 3 {
			return nil, "SKILL.md path is too deep; use ZIP root, foo/SKILL.md, or wrapper/foo/SKILL.md"
		}
		entries = append(entries, skillEntry{path: n, file: f, parts: parts})
	}
	if len(entries) == 0 {
		return nil, "ZIP must contain SKILL.md at root (or inside subdirectories)"
	}

	var rootSkill *zip.File
	for _, e := range entries {
		if len(e.parts) == 1 && e.parts[0] == "SKILL.md" {
			rootSkill = e.file
			break
		}
	}
	if rootSkill != nil {
		if len(entries) > 1 {
			return nil, "ZIP must not mix SKILL.md at root with SKILL.md in subdirectories"
		}
		return []skillZipRoot{{stripPrefix: "", skillMD: rootSkill}}, ""
	}

	var depth2 []skillEntry
	var depth3 []skillEntry
	for _, e := range entries {
		switch len(e.parts) {
		case 2:
			if e.parts[1] == "SKILL.md" {
				depth2 = append(depth2, e)
			}
		case 3:
			if e.parts[2] == "SKILL.md" {
				depth3 = append(depth3, e)
			}
		}
	}
	if len(depth2) > 0 && len(depth3) > 0 {
		return nil, "ZIP must not mix one-level (foo/SKILL.md) and two-level (wrapper/foo/SKILL.md) skill layouts"
	}

	var roots []skillZipRoot
	if len(depth2) > 0 {
		dirSet := map[string]struct{}{}
		for _, e := range depth2 {
			dirSet[e.parts[0]] = struct{}{}
		}
		dirs := make([]string, 0, len(dirSet))
		for d := range dirSet {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		for _, d := range dirs {
			var skill *zip.File
			for _, e := range depth2 {
				if e.parts[0] == d {
					skill = e.file
					break
				}
			}
			if skill == nil {
				continue
			}
			roots = append(roots, skillZipRoot{stripPrefix: d + "/", skillMD: skill})
		}
		return roots, ""
	}

	if len(depth3) == 0 {
		return nil, "ZIP must contain SKILL.md at root (or inside subdirectories)"
	}

	wrapSet := map[string]struct{}{}
	for _, e := range depth3 {
		wrapSet[e.parts[0]] = struct{}{}
	}
	if len(wrapSet) > 1 {
		return nil, "ZIP must use a single top-level wrapper directory for skills under wrapper/foo/SKILL.md"
	}
	var w string
	for k := range wrapSet {
		w = k
		break
	}
	childSet := map[string]struct{}{}
	for _, e := range depth3 {
		if e.parts[0] == w {
			childSet[e.parts[1]] = struct{}{}
		}
	}
	children := make([]string, 0, len(childSet))
	for c := range childSet {
		children = append(children, c)
	}
	sort.Strings(children)
	for _, child := range children {
		var skill *zip.File
		for _, e := range depth3 {
			if e.parts[0] == w && e.parts[1] == child {
				skill = e.file
				break
			}
		}
		if skill == nil {
			continue
		}
		roots = append(roots, skillZipRoot{stripPrefix: w + "/" + child + "/", skillMD: skill})
	}
	return roots, ""
}
