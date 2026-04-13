package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/plugins"
)

// Installer handles downloading and installing marketplace packages.
type Installer struct {
	dataDir        string
	catalog        *Catalog
	pluginRegistry *plugins.Registry
}

// NewInstaller creates an installer.
func NewInstaller(dataDir string, catalog *Catalog, pluginRegistry *plugins.Registry) *Installer {
	return &Installer{
		dataDir:        dataDir,
		catalog:        catalog,
		pluginRegistry: pluginRegistry,
	}
}

// InstallResult describes the outcome of an installation.
type InstallResult struct {
	PackageID   string      `json:"package_id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Ref         string      `json:"ref,omitempty"`
	Type        PackageType `json:"type"`
	InstalledAt time.Time   `json:"installed_at"`
	Path        string      `json:"path"` // local installation path
}

// Install downloads and installs a package from the catalog.
func (i *Installer) Install(ctx context.Context, packageID string) (*InstallResult, error) {
	pkg, ok := i.catalog.Get(packageID)
	if !ok {
		return nil, fmt.Errorf("package %q not found in catalog", packageID)
	}

	installDir := filepath.Join(i.dataDir, "packages", string(pkg.Type), pkg.Name)
	if err := os.MkdirAll(filepath.Dir(installDir), 0o755); err != nil {
		return nil, fmt.Errorf("create install dir: %w", err)
	}

	stagingDir, err := os.MkdirTemp(filepath.Dir(installDir), pkg.Name+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(stagingDir)

	// Clone from repository.
	if pkg.Repository != "" {
		if err := i.cloneRepo(ctx, pkg.Repository, pkg.Ref, stagingDir); err != nil {
			return nil, fmt.Errorf("clone: %w", err)
		}
	} else if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, fmt.Errorf("prepare staging dir: %w", err)
	}

	if err := os.RemoveAll(installDir); err != nil {
		return nil, fmt.Errorf("remove existing install dir: %w", err)
	}
	if err := os.Rename(stagingDir, installDir); err != nil {
		return nil, fmt.Errorf("activate install dir: %w", err)
	}

	i.catalog.IncrementDownloads(packageID)

	// For plugins, register with the plugin registry.
	if pkg.Type == TypePlugin && i.pluginRegistry != nil {
		manifest, err := i.loadPluginManifest(installDir)
		if err != nil {
			slog.Warn("marketplace: plugin manifest not found", "name", pkg.Name, "err", err)
		} else {
			if err := i.pluginRegistry.Install(ctx, *manifest); err != nil {
				slog.Warn("marketplace: plugin install failed", "name", pkg.Name, "err", err)
			}
		}
	}

	slog.Info("marketplace: package installed",
		"id", packageID, "name", pkg.Name, "type", pkg.Type)

	result := InstallResult{
		PackageID:   packageID,
		Name:        pkg.Name,
		Version:     pkg.Version,
		Ref:         pkg.Ref,
		Type:        pkg.Type,
		InstalledAt: time.Now(),
		Path:        installDir,
	}
	if err := i.recordInstall(result, pkg.Repository); err != nil {
		return nil, fmt.Errorf("record install: %w", err)
	}
	return &result, nil
}

// Uninstall removes an installed package.
func (i *Installer) Uninstall(ctx context.Context, packageID string) error {
	pkg, ok := i.catalog.Get(packageID)
	if !ok {
		return fmt.Errorf("package %q not found", packageID)
	}

	installDir := filepath.Join(i.dataDir, "packages", string(pkg.Type), pkg.Name)

	// For plugins, unregister from the plugin registry.
	if pkg.Type == TypePlugin && i.pluginRegistry != nil {
		_ = i.pluginRegistry.Uninstall(ctx, pkg.Name)
	}

	if err := os.RemoveAll(installDir); err != nil {
		return fmt.Errorf("remove install dir: %w", err)
	}
	if err := i.removeInstallRecord(packageID); err != nil {
		return fmt.Errorf("update install manifest: %w", err)
	}

	slog.Info("marketplace: package uninstalled", "id", packageID, "name", pkg.Name)
	return nil
}

// Update re-installs a package with the latest version.
func (i *Installer) Update(ctx context.Context, packageID string) (*InstallResult, error) {
	if err := i.Uninstall(ctx, packageID); err != nil {
		slog.Warn("marketplace: uninstall during update", "err", err)
	}
	return i.Install(ctx, packageID)
}

// ListInstalled returns locally installed packages by scanning the data directory.
func (i *Installer) ListInstalled() ([]InstallResult, error) {
	if results, err := i.readInstalledManifest(); err == nil && len(results) > 0 {
		sort.Slice(results, func(a, b int) bool {
			return results[a].InstalledAt.After(results[b].InstalledAt)
		})
		return results, nil
	}

	var results []InstallResult

	typeDirs := []PackageType{TypeSkill, TypeAgent, TypeTeam, TypeMCP, TypePlugin}
	for _, pt := range typeDirs {
		dir := filepath.Join(i.dataDir, "packages", string(pt))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			info, _ := entry.Info()
			results = append(results, InstallResult{
				Name:        entry.Name(),
				Type:        pt,
				Path:        filepath.Join(dir, entry.Name()),
				InstalledAt: info.ModTime(),
			})
		}
	}

	return results, nil
}

func (i *Installer) cloneRepo(ctx context.Context, repoURL, ref, targetDir string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", repoURL, targetDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	if ref == "" {
		return nil
	}
	fetchCmd := exec.CommandContext(ctx, "git", "-C", targetDir, "fetch", "--depth=1", "origin", ref)
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		return err
	}
	checkoutCmd := exec.CommandContext(ctx, "git", "-C", targetDir, "checkout", "FETCH_HEAD")
	checkoutCmd.Stdout = os.Stdout
	checkoutCmd.Stderr = os.Stderr
	return checkoutCmd.Run()
}

func (i *Installer) loadPluginManifest(dir string) (*plugins.Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, err
	}
	var m plugins.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

type installedManifest struct {
	Packages []installedPackage `json:"packages"`
}

type installedPackage struct {
	InstallResult
	Repository string `json:"repository,omitempty"`
}

func (i *Installer) installedManifestPath() string {
	return filepath.Join(i.dataDir, "packages", "installed.json")
}

func (i *Installer) readInstalledManifest() ([]InstallResult, error) {
	data, err := os.ReadFile(i.installedManifestPath())
	if err != nil {
		return nil, err
	}
	var manifest installedManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	results := make([]InstallResult, 0, len(manifest.Packages))
	for _, pkg := range manifest.Packages {
		results = append(results, pkg.InstallResult)
	}
	return results, nil
}

func (i *Installer) recordInstall(result InstallResult, repository string) error {
	manifest, err := i.loadInstallManifest()
	if err != nil {
		return err
	}
	entry := installedPackage{
		InstallResult: result,
		Repository:    repository,
	}
	replaced := false
	for idx := range manifest.Packages {
		if manifest.Packages[idx].PackageID == result.PackageID {
			manifest.Packages[idx] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		manifest.Packages = append(manifest.Packages, entry)
	}
	return i.writeInstallManifest(manifest)
}

func (i *Installer) removeInstallRecord(packageID string) error {
	manifest, err := i.loadInstallManifest()
	if err != nil {
		return err
	}
	filtered := manifest.Packages[:0]
	for _, pkg := range manifest.Packages {
		if pkg.PackageID == packageID {
			continue
		}
		filtered = append(filtered, pkg)
	}
	manifest.Packages = filtered
	return i.writeInstallManifest(manifest)
}

func (i *Installer) loadInstallManifest() (*installedManifest, error) {
	path := i.installedManifestPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &installedManifest{}, nil
		}
		return nil, err
	}
	var manifest installedManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (i *Installer) writeInstallManifest(manifest *installedManifest) error {
	path := i.installedManifestPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
