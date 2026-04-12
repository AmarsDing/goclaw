package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/marketplace"
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
	if spec.Name == "" || spec.Version == "" || spec.Type == "" || spec.Description == "" {
		return fmt.Errorf("name, version, type, and description are required")
	}
	switch spec.Type {
	case marketplace.TypeSkill, marketplace.TypeAgent, marketplace.TypeMCP, marketplace.TypePlugin:
	default:
		return fmt.Errorf("unsupported type %q", spec.Type)
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
			return fmt.Errorf("forbidden path traversal: %s", rel)
		}
		if strings.HasPrefix(rel, ".git/") {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not allowed: %s", rel)
		}
		if d.IsDir() {
			return nil
		}
		fileCount++
		if fileCount > 256 {
			return fmt.Errorf("too many files: %d", fileCount)
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		totalSize += info.Size()
		if info.Size() > 8<<20 {
			return fmt.Errorf("file too large: %s", rel)
		}
		if totalSize > 32<<20 {
			return fmt.Errorf("package too large: %d bytes", totalSize)
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
		return fmt.Errorf("required file missing: %s", required[spec.Type])
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
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", pkg.Name+"-"+pkg.Version+".zip")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(archive); err != nil {
		return nil, err
	}
	meta := map[string]any{
		"package": pkg,
		"force":   force,
	}
	metaRaw, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	if err := writer.WriteField("metadata", string(metaRaw)); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	base := resolveGatewayBaseURL()
	req, err := http.NewRequest(http.MethodPost, base+"/v1/marketplace/packages/upload", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token := resolveGatewayToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach gateway at %s: %w", base, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, parseHTTPError(raw, resp.StatusCode)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func marketplacePackageID(pkgType marketplace.PackageType, name, version string) string {
	cleanName := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	cleanVersion := strings.ReplaceAll(version, "/", "-")
	return string(pkgType) + "-" + cleanName + "-" + cleanVersion
}
