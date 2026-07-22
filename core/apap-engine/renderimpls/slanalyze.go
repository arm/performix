// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package renderimpls

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/render"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const slAnalyzeRendererName = "SlAnalyzeRenderer"
const slAnalyzeRendererVersion = "1.0"

var ErrSlAnalyzeBinaryNotFound = errors.New("failed to resolve sl-analyze binary path for current platform")

// SlAnalyzeRendererConfigJSON holds configuration for the SlAnalyzeRenderer.
type SlAnalyzeRendererConfigJSON struct {
	FilterPid         *int     `json:"filter_pid,omitempty"`
	FilterTid         *int     `json:"filter_tid,omitempty"`
	FilterStartTimeNs *int64   `json:"filter_start_time_ns,omitempty"`
	FilterEndTimeNs   *int64   `json:"filter_end_time_ns,omitempty"`
	Grouping          []string `json:"grouping,omitempty"`
	Entity            string   `json:"entity"`
}

// SlAnalyzeRenderer runs host sl-analyze and emits capture-derived artifacts.
type SlAnalyzeRenderer struct {
	config         *render.Config
	specificConfig *SlAnalyzeRendererConfigJSON
}

// Name returns the renderer identifier.
func (renderer *SlAnalyzeRenderer) Name() string {
	return slAnalyzeRendererName
}

// Version returns the renderer schema version.
func (renderer *SlAnalyzeRenderer) Version() string {
	return slAnalyzeRendererVersion
}

// Configure parses and stores renderer configuration.
func (renderer *SlAnalyzeRenderer) Configure(config *render.Config) error {
	renderer.config = config

	parsed, err := util.DecodeJSON[SlAnalyzeRendererConfigJSON]([]byte(config.JSON))
	if err != nil {
		return fmt.Errorf("failed to parse config '%s': %w", config.JSON, err)
	}
	renderer.specificConfig = parsed
	return nil
}

func (renderer *SlAnalyzeRenderer) DoRenderParamsExist() bool {
	if renderer.specificConfig.FilterPid != nil || renderer.specificConfig.FilterTid != nil || renderer.specificConfig.FilterStartTimeNs != nil || renderer.specificConfig.FilterEndTimeNs != nil || len(renderer.specificConfig.Grouping) != 0 {
		return true
	}
	return false
}

// GetInputSpec returns an empty input specification.
func (renderer *SlAnalyzeRenderer) GetInputSpec() render.InputSpec {
	return render.InputSpec{}
}

// GetOutputSpec declares the status table produced by this renderer.
func (renderer *SlAnalyzeRenderer) GetOutputSpec() render.OutputSpec {
	return render.OutputSpec{}
}

// Initialize executes sl-analyze (when enabled) and emits output files.
func (renderer *SlAnalyzeRenderer) Initialize(session render.Session, _ map[string][]render.TableRef) error {
	if !renderer.DoRenderParamsExist() {
		return nil
	}

	// If capture.apc directory is not present, skip initialization as well since there's nothing to analyze.
	hasCapture := false
	for _, entry := range session.Content().Entries {
		_, err := renderer.resolveCaptureRoot(entry.Model)
		if err == nil || errors.Is(err, cdf.ErrComponentPending) {
			hasCapture = true
			break
		}
	}
	if !hasCapture {
		return fmt.Errorf("capture.apc directory not found in session content")
	}

	// Prepare common configuration and session helpers.
	rerender := session.Rerender()
	if rerender == nil {
		return fmt.Errorf("rerender filesystem is not available")
	}

	pending := false
	for _, entry := range session.Content().Entries {
		// Resolve capture.apc directory for the run.
		captureDir, err := renderer.resolveCaptureRoot(entry.Model)
		if errors.Is(err, cdf.ErrComponentPending) {
			pending = true
			// Add pending entries to the render manifest
			if emitErr := renderer.emitOutputs(rerender, entry.ID, true); emitErr != nil {
				return emitErr
			}
			// continue to allow outputs for other runs to be emitted; we will still return errcomponentpending in the end
			continue
		} else if err != nil {
			return err
		}

		// Create the temp render directory for sl-analyze output.
		tempDir, err := rerender.CreateTempDirForRun(entry.ID)
		if err != nil {
			return err
		}

		// Resolve host sl-analyze binary.
		slAnalyzePath, err := resolveSlAnalyzeBinaryPath()
		if err != nil {
			return err
		}

		// Run sl-analyze and place outputs in the temp render directory.
		args := renderer.buildSlAnalyzeArgs(slAnalyzePath, tempDir, captureDir)
		if err := runSlAnalyze(args); err != nil {
			return err
		}

		// Move outputs from temp render directory into the session render overlay.
		if err := renderer.emitOutputs(rerender, entry.ID, false); err != nil {
			return err
		}
	}

	if pending {
		return cdf.ErrComponentPending
	}
	return nil
}

// getEntity returns the entity root to use for resolving and emitting components.
func (renderer *SlAnalyzeRenderer) getEntity() string {
	entity := renderer.specificConfig.Entity
	if entity == "" {
		return "tool/neoprof/0/"
	}
	return entity
}

func (renderer *SlAnalyzeRenderer) resolveCaptureRoot(model cdf.ModelView) (string, error) {
	component, err := model.ResolveComponent(path.Join(renderer.getEntity(), "capture.apc", "**", "*"))
	if err != nil {
		return "", err
	}

	absolutePath := filepath.ToSlash(filepath.Clean(component.AbsolutePath))
	return filepath.FromSlash(strings.TrimSuffix(absolutePath, "/**/*")), nil
}

// buildSlAnalyzeArgs builds the sl-analyze command line arguments.
func (renderer *SlAnalyzeRenderer) buildSlAnalyzeArgs(slAnalyzePath, tempDir, captureDir string) []string {
	args := []string{
		slAnalyzePath,
		"-o",
		tempDir,
		"--collect-images",
		"--all-images",
		"--apap-export",
		"--include-empty-columns",
		"--annotate-source",
		"--disassemble",
	}

	if renderer.specificConfig.FilterPid != nil {
		args = append(args, "--pid", strconv.Itoa(*renderer.specificConfig.FilterPid))
	}
	if renderer.specificConfig.FilterTid != nil {
		args = append(args, "--tid", strconv.Itoa(*renderer.specificConfig.FilterTid))
	}
	if renderer.specificConfig.FilterStartTimeNs != nil || renderer.specificConfig.FilterEndTimeNs != nil {
		startTime := ""
		endTime := ""
		if renderer.specificConfig.FilterStartTimeNs != nil {
			startTime = strconv.FormatInt(*renderer.specificConfig.FilterStartTimeNs, 10)
		}
		if renderer.specificConfig.FilterEndTimeNs != nil {
			endTime = strconv.FormatInt(*renderer.specificConfig.FilterEndTimeNs, 10)
		}
		args = append(args, "--between", startTime+"-"+endTime)
	}

	groupingStr := "none"
	if len(renderer.specificConfig.Grouping) > 0 {
		parts := make([]string, 0, len(renderer.specificConfig.Grouping))
		for _, grouping := range renderer.specificConfig.Grouping {
			switch grouping {
			case "process", "thread", "core", "none":
				parts = append(parts, grouping)
			}
		}
		groupingStr = strings.Join(parts, ",")
	}
	args = append(args, "--group-by", groupingStr)

	args = append(args, captureDir)
	return args
}

// emitOutputs moves sl-analyze outputs into the render overlay.
func (renderer *SlAnalyzeRenderer) emitOutputs(rerender render.SessionRenderFS, runID run.RunID, pending bool) error {
	entity := renderer.getEntity()
	outputs := []struct {
		sourceRel      string
		destRel        string
		meta           render.OutputMetadata
		allowNoMatches bool
	}{
		{
			sourceRel: "symbols.json",
			destRel:   path.Join(entity, "output", "symbols.json"),
			meta:      render.OutputMetadata{ComponentType: "sl-collect-symbols", Version: "1.1"},
		},
		{
			sourceRel:      "sources-capture-periodic_sampling*",
			destRel:        path.Join(entity, "output", "sources-capture-periodic_sampling*"),
			meta:           render.OutputMetadata{ComponentType: "sl-collect-source-line-attribution", Version: "1.0"},
			allowNoMatches: true,
		},
		{
			sourceRel:      "call_tree.json",
			destRel:        path.Join(entity, "output", "call_tree.json"),
			meta:           render.OutputMetadata{ComponentType: "sl-collect-call-tree", Version: "1.0"},
			allowNoMatches: true,
		},
		{
			sourceRel: "call_tree_samples.json",
			destRel:   path.Join(entity, "output", "call_tree_samples.json"),
			meta:      render.OutputMetadata{ComponentType: "sl-collect-call-tree", Version: "1.0"},
		},
		{
			sourceRel: "callpath_self_samples.json",
			destRel:   path.Join(entity, "output", "callpath_self_samples.json"),
			meta:      render.OutputMetadata{ComponentType: "sl-collect-metrics", Version: "1.0"},
		},
		{
			sourceRel:      "callpath_self_metrics.json",
			destRel:        path.Join(entity, "output", "callpath_self_metrics.json"),
			meta:           render.OutputMetadata{ComponentType: "sl-collect-metrics", Version: "1.0"},
			allowNoMatches: true,
		},
		{
			sourceRel: "callpath_total_samples.json",
			destRel:   path.Join(entity, "output", "callpath_total_samples.json"),
			meta:      render.OutputMetadata{ComponentType: "sl-collect-metrics", Version: "1.0"},
		},
		{
			sourceRel:      "callpath_total_metrics.json",
			destRel:        path.Join(entity, "output", "callpath_total_metrics.json"),
			meta:           render.OutputMetadata{ComponentType: "sl-collect-metrics", Version: "1.0"},
			allowNoMatches: true,
		},
		{
			sourceRel: "callpaths-capture-periodic_sampling.csv",
			destRel:   path.Join(entity, "output", "callpaths-capture-periodic_sampling.csv"),
			meta:      render.OutputMetadata{ComponentType: "sl-collect", Version: "1.0"},
		},
		{
			sourceRel:      "functions-capture-periodic_sampling.csv",
			destRel:        path.Join(entity, "output", "functions-capture-periodic_sampling.csv"),
			meta:           render.OutputMetadata{ComponentType: "sl-collect-flat-functions-csv", Version: "1.1"},
			allowNoMatches: true,
		},
		{
			sourceRel:      "functions-capture-metrics.csv",
			destRel:        path.Join(entity, "output", "functions-capture-metrics.csv"),
			meta:           render.OutputMetadata{ComponentType: "sl-collect-flat-functions-csv", Version: "1.1"},
			allowNoMatches: true,
		},
		{
			sourceRel:      "disassembly-capture-periodic_sampling*",
			destRel:        path.Join(entity, "output", "disassembly-capture-periodic_sampling*"),
			meta:           render.OutputMetadata{ComponentType: "disassembly_capture_samples", Version: "1.1"},
			allowNoMatches: true,
		},
	}

	// Emit each output, allowing optional globbed files to be absent.
	for _, output := range outputs {
		if pending {
			err := rerender.EmitPendingOutputForRun(runID, output.destRel, output.meta)
			if err != nil {
				return err
			}
		} else {
			err := rerender.EmitOutputForRun(runID, output.sourceRel, output.destRel, output.meta)
			if output.allowNoMatches && (errors.Is(err, render.ErrRenderNoMatches) || errors.Is(err, run.ErrRenderTempFileNotFound)) {
				continue
			}
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// resolveSlAnalyzeBinaryPath returns the host sl-analyze binary path for the current platform.
func resolveSlAnalyzeBinaryPath() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("%w: failed to resolve executable path: %w", ErrSlAnalyzeBinaryNotFound, err)
	}
	return resolveSlAnalyzeBinaryPathWithFS(afero.NewOsFs(), exePath, runtime.GOOS, runtime.GOARCH)
}

// resolveSlAnalyzeBinaryPathWithFS resolves sl-analyze using the provided filesystem and platform inputs.
func resolveSlAnalyzeBinaryPathWithFS(fs afero.Fs, exePath, goos, goarch string) (string, error) {
	exeDir := filepath.Dir(exePath)

	subPath, err := slAnalyzeBinarySubPathProd(goos, goarch)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSlAnalyzeBinaryNotFound, err)
	}
	binaryPath := filepath.Join(exeDir, subPath)

	// Check prod path first. If we get a non-NotExist error, surface it immediately.
	if _, err = fs.Stat(binaryPath); err == nil {
		return binaryPath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("%w: stat failed for %s: %v", ErrSlAnalyzeBinaryNotFound, binaryPath, err)
	}

	// Prod missing; try dev path next.
	subPath, err = slAnalyzeBinarySubPathDev(goos, goarch)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSlAnalyzeBinaryNotFound, err)
	}
	binaryPath = filepath.Join(exeDir, subPath)
	if _, err = fs.Stat(binaryPath); err == nil {
		return binaryPath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("%w: stat failed for %s: %v", ErrSlAnalyzeBinaryNotFound, binaryPath, err)
	}
	return "", ErrSlAnalyzeBinaryNotFound
}

// slAnalyzeBinarySubPathProd returns the packaged path for the sl-analyze host binary.
func slAnalyzeBinarySubPathProd(goos, goarch string) (string, error) {
	binaryName, err := slAnalyzeBinaryNameForPlatform(goos, goarch)
	if err != nil {
		return "", err
	}
	return filepath.Join("tools", "sl-analyze-host", binaryName), nil
}

// slAnalyzeBinarySubPathDev returns the dev-tree path for the sl-analyze host binary.
func slAnalyzeBinarySubPathDev(goos, goarch string) (string, error) {
	binaryName, err := slAnalyzeBinaryNameForPlatform(goos, goarch)
	if err != nil {
		return "", err
	}
	subDir, err := slAnalyzeHostSubdirFor(goos, goarch)
	if err != nil {
		return "", err
	}
	return filepath.Join("sl-analyze-host-tools", subDir, binaryName), nil
}

// slAnalyzeBinaryNameForPlatform returns the platform-specific sl-analyze binary name.
func slAnalyzeBinaryNameForPlatform(goos, goarch string) (string, error) {
	switch goos {
	case "darwin", "linux":
		return "sl-analyze", nil

	case "windows":
		return "sl-analyze.exe", nil
	}

	return "", fmt.Errorf("unsupported host platform %s/%s for sl-analyze", goos, goarch)
}

// slAnalyzeHostSubdirFor maps OS/arch values to the host tools subdirectory and binary name.
func slAnalyzeHostSubdirFor(goos string, goarch string) (string, error) {
	switch goos {
	case "darwin":
		if goarch == "arm64" {
			return "darwin-arm64", nil
		}
	case "linux":
		switch goarch {
		case "arm64":
			return "linux-arm64", nil
		case "amd64":
			return "linux-x64", nil
		}
	case "windows":
		switch goarch {
		case "arm64":
			return "windows-arm64", nil
		case "amd64":
			return "windows-x64", nil
		}
	}

	return "", fmt.Errorf("unsupported host platform %s/%s for sl-analyze", goos, goarch)
}

// runSlAnalyze executes sl-analyze with the provided arguments.
func runSlAnalyze(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("sl-analyze command is empty")
	}
	log.Infof("Running sl-analyze with args: %s", strings.Join(args, " "))

	// #nosec G204 -- temporary mechanism to run sl-analyze until we'll have a robust tool integration strategy host-side.
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sl-analyze failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
