package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
	"github.com/nextlevelbuilder/goclaw/internal/plugins"
	"github.com/nextlevelbuilder/goclaw/internal/workshop"
)

func marketplaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "marketplace",
		Short: "Import and inspect marketplace catalog snapshots",
	}
	cmd.AddCommand(marketplaceImportCmd())
	cmd.AddCommand(marketplacePushCmd())
	return cmd
}

func marketplaceImportCmd() *cobra.Command {
	var inputPath string
	var catalogPath string

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a marketplace registry JSON file",
		Run: func(cmd *cobra.Command, args []string) {
			if inputPath == "" {
				fmt.Fprintln(os.Stderr, "--file is required")
				os.Exit(1)
			}
			if catalogPath == "" {
				catalogPath = filepath.Join(resolveMarketplaceDataDir(), "marketplace", "catalog.json")
			}

			packages, err := loadMarketplacePackages(inputPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "load registry: %v\n", err)
				os.Exit(1)
			}

			catalog := marketplace.NewCatalog()
			if _, err := os.Stat(catalogPath); err == nil {
				if err := catalog.LoadFromFile(catalogPath); err != nil {
					fmt.Fprintf(os.Stderr, "load existing catalog: %v\n", err)
					os.Exit(1)
				}
			}
			for _, pkg := range packages {
				if err := catalog.Register(context.Background(), pkg); err != nil {
					fmt.Fprintf(os.Stderr, "register package %q: %v\n", pkg.ID, err)
					os.Exit(1)
				}
			}
			if err := catalog.SaveToFile(catalogPath); err != nil {
				fmt.Fprintf(os.Stderr, "save catalog: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Imported %d package(s) into %s\n", len(packages), catalogPath)
		},
	}
	cmd.Flags().StringVar(&inputPath, "file", "", "path to registry JSON file")
	cmd.Flags().StringVar(&catalogPath, "catalog", "", "path to local catalog snapshot (default: $GOCLAW_DATA_DIR/marketplace/catalog.json)")
	return cmd
}

func marketplacePushCmd() *cobra.Command {
	var dir string
	var name string
	var version string
	var pkgType string
	var description string
	var author string
	var repository string
	var ref string
	var tags []string
	var force bool

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Validate and upload a marketplace package directory",
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			spec := marketplace.Package{
				Name:        name,
				Version:     version,
				Type:        marketplace.PackageType(pkgType),
				Description: description,
				Author:      author,
				Repository:  repository,
				Ref:         ref,
				Tags:        tags,
			}
			if err := validateMarketplacePush(dir, &spec); err != nil {
				fmt.Fprintf(os.Stderr, "validate package: %v\n", err)
				os.Exit(1)
			}
			spec.ID = marketplacePackageID(spec.Type, spec.Name, spec.Version)

			archiveBytes, err := zipMarketplaceDir(dir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "archive package: %v\n", err)
				os.Exit(1)
			}
			resp, err := gatewayMarketplaceUpload(spec, force, archiveBytes)
			if err != nil {
				fmt.Fprintf(os.Stderr, "upload package: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Uploaded %s %s as %s\n", spec.Name, spec.Version, resp["artifact_path"])
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "package directory to validate and upload")
	cmd.Flags().StringVar(&name, "name", "", "package name")
	cmd.Flags().StringVar(&version, "version", "", "package version")
	cmd.Flags().StringVar(&pkgType, "type", "", "package type: skill, agent, mcp_server, plugin")
	cmd.Flags().StringVar(&description, "description", "", "package description")
	cmd.Flags().StringVar(&author, "author", "", "package author")
	cmd.Flags().StringVar(&repository, "repository", "", "source repository URL")
	cmd.Flags().StringVar(&ref, "ref", "", "source git ref (tag, branch, or commit)")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "comma-separated package tags")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing staged upload for the same name+version")
	return cmd
}

func resolveMarketplaceDataDir() string {
	cfg, err := config.Load(resolveConfigPath())
	if err == nil {
		return cfg.ResolvedDataDir()
	}
	return config.ResolvedDataDirFromEnv()
}

func loadMarketplacePackages(path string) ([]marketplace.Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var snapshot struct {
		Packages []marketplace.Package `json:"packages"`
	}
	if err := json.Unmarshal(data, &snapshot); err == nil && snapshot.Packages != nil {
		return snapshot.Packages, nil
	}

	var packages []marketplace.Package
	if err := json.Unmarshal(data, &packages); err != nil {
		return nil, err
	}
	return packages, nil
}

func validateMarketplacePush(dir string, spec *marketplace.Package) error {
	validation := &pushValidation{}
	if dir == "" {
		return fmt.Errorf("--dir is required")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if spec == nil {
		return fmt.Errorf("package spec is required")
	}
	if strings.TrimSpace(spec.Name) == "" {
		validation.add("metadata.name is required")
	}
	if strings.TrimSpace(spec.Version) == "" {
		validation.add("metadata.version is required")
	}
	if strings.TrimSpace(spec.Description) == "" {
		validation.add("metadata.description is required")
	}
	if strings.TrimSpace(spec.Author) == "" {
		validation.add("metadata.author is required")
	}

	switch spec.Type {
	case marketplace.TypeSkill, marketplace.TypeAgent, marketplace.TypeTeam, marketplace.TypeMCP, marketplace.TypePlugin:
	default:
		validation.add(fmt.Sprintf("metadata.type %q is unsupported", spec.Type))
	}
	if spec.Version != "" && !isSemverLike(spec.Version) {
		validation.add("metadata.version should be semver-like (e.g. 1.2.3)")
	}
	if spec.Repository != "" {
		if _, err := parseHTTPURL(spec.Repository); err != nil {
			validation.add(fmt.Sprintf("metadata.repository: %v", err))
		}
	}
	if spec.Ref != "" && strings.TrimSpace(spec.Repository) == "" {
		validation.add("metadata.ref requires metadata.repository")
	}
	if len(spec.Tags) > 16 {
		validation.add("metadata.tags supports at most 16 tags")
	}
	for _, tag := range spec.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			validation.add("metadata.tags must not contain empty tag")
			break
		}
		if len(tag) > 32 {
			validation.add(fmt.Sprintf("metadata.tags contains oversized tag %q (max 32 chars)", tag))
			break
		}
	}

	var fileCount int
	var totalSize int64
	required := map[marketplace.PackageType]string{
		marketplace.TypeSkill:  "SKILL.md",
		marketplace.TypePlugin: "plugin.json",
	}
	seenRequired := required[spec.Type] == ""

	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if strings.Contains(rel, "..") {
			validation.add(fmt.Sprintf("forbidden path traversal: %s", rel))
			return nil
		}
		if strings.HasPrefix(rel, ".git/") {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			validation.add(fmt.Sprintf("symlink not allowed: %s", rel))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		fileCount++
		if fileCount > 256 {
			validation.add(fmt.Sprintf("too many files: %d (max 256)", fileCount))
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		totalSize += info.Size()
		if info.Size() > 8<<20 {
			validation.add(fmt.Sprintf("file too large: %s (max 8 MiB per file)", rel))
		}
		if totalSize > 32<<20 {
			validation.add(fmt.Sprintf("package too large: %d bytes (max 32 MiB)", totalSize))
			return nil
		}
		if requiredPath, ok := required[spec.Type]; ok && rel == requiredPath {
			seenRequired = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !seenRequired {
		validation.add(fmt.Sprintf("required file missing: %s", required[spec.Type]))
	}

	if spec.Type == marketplace.TypeSkill {
		b, rerr := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if rerr != nil {
			validation.add(fmt.Sprintf("SKILL.md: %v", rerr))
		} else if err := workshop.ValidateSkillMarkdown(string(b)); err != nil {
			validation.add(fmt.Sprintf("SKILL.md: %v", err))
		}
	}
	if spec.Type == marketplace.TypeAgent {
		p := filepath.Join(dir, "agent.json")
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			validation.add("required file missing: agent.json")
		} else if err := workshop.ValidateAgentJSON(b); err != nil {
			validation.add(fmt.Sprintf("agent.json: %v", err))
		} else if err := validateAgentJSONSchema(b); err != nil {
			validation.add(fmt.Sprintf("agent.json schema: %v", err))
		}
	}
	if spec.Type == marketplace.TypePlugin {
		b, rerr := os.ReadFile(filepath.Join(dir, "plugin.json"))
		if rerr != nil {
			validation.add(fmt.Sprintf("plugin.json: %v", rerr))
		} else {
			for _, issue := range validatePluginManifestJSON(b) {
				validation.add(issue)
			}
		}
	}
	if spec.Type == marketplace.TypeMCP {
		var mcpPath string
		for _, candidate := range []string{".mcp.json", "mcp.json"} {
			p := filepath.Join(dir, candidate)
			if _, err := os.Stat(p); err == nil {
				mcpPath = p
				break
			}
		}
		if mcpPath == "" {
			validation.add("mcp_server package requires .mcp.json or mcp.json")
		} else {
			b, rerr := os.ReadFile(mcpPath)
			if rerr != nil {
				validation.add(fmt.Sprintf("%s: %v", filepath.Base(mcpPath), rerr))
			} else if err := validateMCPConfigJSON(b); err != nil {
				validation.add(fmt.Sprintf("%s: %v", filepath.Base(mcpPath), err))
			}
		}
	}

	if err := validation.err(); err != nil {
		return err
	}

	return nil
}

func zipMarketplaceDir(dir string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if strings.Contains(rel, "..") || strings.HasPrefix(rel, ".git/") {
			return nil
		}
		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(w, file)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gatewayMarketplaceUpload(pkg marketplace.Package, force bool, archive []byte) (map[string]any, error) {
	base := strings.TrimSuffix(resolveGatewayBaseURL(), "/")
	token := resolveGatewayToken()
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	return marketplace.UploadPackageHTTP(
		ctx, marketplaceUploadClient, base, token, pkg, force, archive,
		map[string]any{
			"artifact_sha256": sha256Hex(archive),
			"artifact_size":   len(archive),
		},
	)
}

func marketplacePackageID(pkgType marketplace.PackageType, name, version string) string {
	cleanName := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	cleanVersion := strings.ReplaceAll(version, "/", "-")
	return string(pkgType) + "-" + cleanName + "-" + cleanVersion
}

type pushValidation struct {
	issues []string
}

func (v *pushValidation) add(msg string) {
	msg = strings.TrimSpace(msg)
	if msg != "" {
		v.issues = append(v.issues, msg)
	}
}

func (v *pushValidation) err() error {
	if len(v.issues) == 0 {
		return nil
	}
	sort.Strings(v.issues)
	return fmt.Errorf("%d validation issue(s):\n - %s", len(v.issues), strings.Join(v.issues, "\n - "))
}

var semverLikeRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)

func isSemverLike(v string) bool {
	return semverLikeRE.MatchString(strings.TrimSpace(v))
}

func parseHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("URL host is required")
	}
	return u, nil
}

func validateAgentJSONSchema(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var payload struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Provider    string `json:"provider"`
		Model       string `json:"model"`
	}
	if err := dec.Decode(&payload); err != nil {
		return err
	}
	if strings.TrimSpace(payload.Name) == "" {
		return fmt.Errorf("field \"name\" must not be empty")
	}
	if strings.TrimSpace(payload.Description) == "" {
		return fmt.Errorf("field \"description\" must not be empty")
	}
	if strings.TrimSpace(payload.Provider) == "" {
		return fmt.Errorf("field \"provider\" must not be empty")
	}
	if strings.TrimSpace(payload.Model) == "" {
		return fmt.Errorf("field \"model\" must not be empty")
	}
	return nil
}

func validatePluginManifestJSON(data []byte) []string {
	var manifest plugins.Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return []string{fmt.Sprintf("plugin.json schema: %v", err)}
	}

	var issues []string
	if strings.TrimSpace(manifest.Name) == "" {
		issues = append(issues, "plugin.json: field \"name\" is required")
	}
	if strings.TrimSpace(manifest.Version) == "" {
		issues = append(issues, "plugin.json: field \"version\" is required")
	} else if !isSemverLike(manifest.Version) {
		issues = append(issues, "plugin.json: field \"version\" must be semver-like")
	}
	if strings.TrimSpace(manifest.Description) == "" {
		issues = append(issues, "plugin.json: field \"description\" is required")
	}
	if strings.TrimSpace(manifest.Entrypoint) == "" {
		issues = append(issues, "plugin.json: field \"entrypoint\" is required")
	}
	switch strings.ToLower(strings.TrimSpace(manifest.Transport)) {
	case "stdio", "http", "mcp":
	default:
		issues = append(issues, "plugin.json: field \"transport\" must be one of stdio/http/mcp")
	}

	toolNames := map[string]bool{}
	for i, decl := range manifest.Capabilities.Tools {
		prefix := fmt.Sprintf("plugin.json capabilities.tools[%d]", i)
		if strings.TrimSpace(decl.Name) == "" {
			issues = append(issues, prefix+": name is required")
		} else if toolNames[decl.Name] {
			issues = append(issues, prefix+": duplicate tool name "+decl.Name)
		} else {
			toolNames[decl.Name] = true
		}
		if strings.TrimSpace(decl.Description) == "" {
			issues = append(issues, prefix+": description is required")
		}
		if decl.Parameters == nil {
			issues = append(issues, prefix+": parameters is required")
		}
	}
	return issues
}

func validateMCPConfigJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if len(raw) == 0 {
		return fmt.Errorf("empty config")
	}
	if srv, ok := raw["mcpServers"]; ok {
		if m, ok := srv.(map[string]any); ok && len(m) > 0 {
			return nil
		}
		return fmt.Errorf(`"mcpServers" must be a non-empty object`)
	}
	if srv, ok := raw["servers"]; ok {
		if m, ok := srv.(map[string]any); ok && len(m) > 0 {
			return nil
		}
		return fmt.Errorf(`"servers" must be a non-empty object`)
	}
	return fmt.Errorf(`expected top-level "mcpServers" (or "servers") object`)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
