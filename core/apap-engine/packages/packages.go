// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package packages

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	tool_goja "github.com/Arm-Debug/apap-cli/apap-engine/tool/goja"
	"github.com/Arm-Debug/apap-cli/apap-engine/userdirs"
)

func GetExtensionsDir() (string, error) {
	dataDir, err := userdirs.DataDir()
	if err != nil {
		return "", fmt.Errorf("failed to get data directory: %w", err)
	}
	return filepath.Join(dataDir, "extensions"), nil
}

// GetPackageDirs returns all directories which contain a Performix package. This is the root installation directory
// and the extension directory within the user data directory. execPath is the path to the binary (atperf) this process was launched from.
func GetPackageDirs(execPath string) []string {

	// This will work no matter where we're calling the executable from
	execDir := filepath.Dir(execPath)
	recipeDirs := []string{execDir}
	// errors from this point will only produce log warnings. recipe access should still be possible
	// without access to the user directory
	extensionsDir, err := GetExtensionsDir()
	if err != nil {
		log.WithError(err).Warnf("failed to get extensions directory")
		return recipeDirs
	}

	if err := os.MkdirAll(extensionsDir, perms.LocalDirPerm); err != nil {
		log.WithError(err).Warn("failed to ensure extensions directory exists")
		return recipeDirs
	}
	extensionDirectories, err := os.ReadDir(extensionsDir)
	if err != nil {
		log.WithError(err).Warnf("failed to read extensions dir")
		return recipeDirs
	}

	// Each directory in the extensions directory. There may be N directories, each of which contains its own recipes directory.
	for eds := range extensionDirectories {
		if extensionDirectories[eds].IsDir() {
			recipeDirs = append(recipeDirs, filepath.Join(extensionsDir, extensionDirectories[eds].Name()))
		}
	}
	return recipeDirs
}

type PackageManager struct {
	executableDir string // Directory where the executable is installed ("core" package files are located here)
	extensionsDir string // Directory where extension packages are installed (optional)
}

func NewPackageManager(executableDir string, extensionsDir string) *PackageManager {
	pm := PackageManager{}
	pm.executableDir = executableDir
	pm.extensionsDir = extensionsDir
	return &pm
}

type packageFiles struct {
	toolBundleMap      map[string]int // Map from tool bundle ID to index into toolBundles
	toolIntegrationMap map[string]int // Map from tool integration ID to index into toolIntegrations
	toolBundles        []PackageFile  // Info about each tool bundle
	toolIntegrations   []PackageFile  // Info about each tool integration
}

func newPackageFiles() *packageFiles {
	p := packageFiles{}
	p.toolBundleMap = make(map[string]int)
	p.toolIntegrationMap = make(map[string]int)
	p.toolBundles = make([]PackageFile, 0)
	p.toolIntegrations = make([]PackageFile, 0)
	return &p
}

type PackageFile struct {
	kind         packageFileKind // The type of file
	packageDir   string          // Absolute path of package directory
	relativePath string          // Relative path of the file in the package subdirectory
}

type packageFileKind int

const (
	recipe packageFileKind = iota
	toolBundle
	toolIntegration
)

var subDir = []string{
	"recipes",
	"tools",
	"tool-integrations",
}

func findPackages(executableDir string, extensionsDir string) (*packageFiles, error) {
	packages := newPackageFiles()

	err := findPackageFiles(executableDir, toolBundle, packages)
	if err != nil {
		metadata := map[string]string{
			"executableToolsDir": filepath.Join(executableDir, subDir[toolBundle]),
		}
		return nil, message.New(message.EnginePackagesLoadCoreToolBundles).WithCause(err).WithMetadata(metadata)
	}

	err = findPackageFiles(executableDir, toolIntegration, packages)
	if err != nil {
		// TODO: make this an error?
		log.WithError(err).Warnf("no tool integrations found in core package")
	}

	if extensionsDir == "" {
		return packages, nil
	}
	dirEntries, err := os.ReadDir(extensionsDir)
	if err != nil {
		log.WithError(err).Warnf("failed to read extensions directory")
		return packages, nil
	}

	for _, entry := range dirEntries {
		if entry.IsDir() {
			packageName := entry.Name()
			packageDir := filepath.Join(extensionsDir, packageName)

			err = findPackageFiles(packageDir, toolBundle, packages)
			if err != nil {
				log.WithError(err).Warnf("no tool bundles found in extension package %q", packageName)
			}

			err = findPackageFiles(packageDir, toolIntegration, packages)
			if err != nil {
				log.WithError(err).Warnf("no tool integrations found in extension package %q", packageName)
			}
		}
	}

	return packages, nil
}

func findPackageFiles(packageDir string, fileKind packageFileKind, packages *packageFiles) error {
	var relativePaths []string
	dir := filepath.Join(packageDir, subDir[fileKind])
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			var relativePath string
			relativePath, err = filepath.Rel(dir, path)
			if err == nil {
				relativePaths = append(relativePaths, relativePath)
			}
		}
		return err
	})
	if err != nil {
		return err
	}

	if len(relativePaths) > 0 {
		for _, relativePath := range relativePaths {
			switch fileKind {
			case toolBundle:
				// Check the path matches the expected format name/version/bundle.ext
				relDir, bundle := filepath.Split(relativePath)
				dirs := strings.Split(relDir, string(filepath.Separator))
				ext := filepath.Ext(bundle)
				if bundle == "" || ext == "" || len(dirs) != 3 || dirs[2] != "" {
					log.Warnf("file %q in package %q does not match the expected path format: %s", relativePath, packageDir, filepath.Join("name", "version", "bundle.ext"))
					break
				}
				addPackageFile(packages.toolBundleMap, &packages.toolBundles, fileKind, relativePath, packageDir)
			case toolIntegration:
				addPackageFile(packages.toolIntegrationMap, &packages.toolIntegrations, fileKind, relativePath, packageDir)
			}
		}
	}

	return nil
}

func addPackageFile(fileMap map[string]int, files *[]PackageFile, fileKind packageFileKind, relativePath, packageDir string) {
	existingFile, exists := fileMap[relativePath]
	if exists {
		log.Warnf("file %q in package %q also exists in package %q", relativePath, packageDir, (*files)[existingFile].packageDir)
		return
	}
	fileMap[relativePath] = len(*files)
	*files = append(*files, PackageFile{kind: fileKind, packageDir: packageDir, relativePath: relativePath})
}

// ID returns the unique identifier of a package file. (The ID is the relative filepath within a package directory.)
func (f PackageFile) ID() string {
	return f.relativePath
}

// Path returns the absolute path of a package file
func (f PackageFile) Path() string {
	return filepath.Join(f.packageDir, subDir[f.kind], f.relativePath)
}

// GetToolBundle returns the tool bundle for a given ID
func (pm *PackageManager) GetToolBundle(id string) (PackageFile, error) {
	// Rescan files on disk - todo: this could be done only when changes are detected
	packages, err := findPackages(pm.executableDir, pm.extensionsDir)
	if err != nil {
		return PackageFile{}, err
	}

	fileIndex, ok := packages.toolBundleMap[id]
	if !ok {
		metadata := map[string]string{
			"toolName":           id,
			"executableToolsDir": filepath.Join(pm.executableDir, subDir[toolBundle]),
			"extensionsToolsDir": filepath.Join(pm.extensionsDir, subDir[toolBundle]),
		}
		return PackageFile{}, message.New(message.EnginePackagesLoadToolBundle).WithMetadata(metadata)
	}
	return packages.toolBundles[fileIndex], nil
}

// ListToolBundles returns a list of each tool bundle in each package
func (pm *PackageManager) ListToolBundles() ([]PackageFile, error) {
	// Rescan files on disk - todo: this could be done only when changes are detected
	packages, err := findPackages(pm.executableDir, pm.extensionsDir)
	if err != nil {
		return nil, err
	}

	return packages.toolBundles, nil
}

// ListToolIntegrations returns a list of each tool integration in each package
func (pm *PackageManager) ListToolIntegrations() ([]PackageFile, error) {
	// Rescan files on disk - todo: this could be done only when changes are detected
	packages, err := findPackages(pm.executableDir, pm.extensionsDir)
	if err != nil {
		return nil, err
	}

	return packages.toolIntegrations, nil
}

// FindToolIntegrations returns a registry of tool integrations found in all packages
func (pm *PackageManager) FindToolIntegrations() (*tool.Registry, error) {
	// Rescan files on disk - todo: this could be done only when changes are detected
	packages, err := findPackages(pm.executableDir, pm.extensionsDir)
	if err != nil {
		return nil, err
	}

	registry := tool.NewToolRegistry()

	for _, file := range packages.toolIntegrations {
		path := file.Path()
		data, err := os.ReadFile(path)
		if err != nil {
			log.WithError(err).Warnf("failed to read tool integration file (path: %q)", path)
			continue
		}
		script, err := tool_goja.LoadFromSource(string(data), path)
		if err != nil {
			log.WithError(err).Warnf("failed to load tool integration from source (path: %q)", path)
			continue
		}
		log.Debugf("loaded tool integration from file %q", path)
		registry.RegisterTool(script)
	}

	return registry, nil
}
