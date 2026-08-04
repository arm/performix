// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package deployer

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/packages"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
)

type ReconcileResult string

const (
	Deployed ReconcileResult = "Deployed" // Wasn't deployed, has now just been deployed
	NoAction ReconcileResult = "NoAction" // Was already deployed
	Deploy   ReconcileResult = "Deploy"   // Needs deploying
)

type ToolDeploymentMode int32

const (
	ToolDeployNONE      ToolDeploymentMode = 0
	ToolDeployAUTO      ToolDeploymentMode = 1
	ToolDeployAUTOFORCE ToolDeploymentMode = 2
	ToolDeployCHECK     ToolDeploymentMode = 3
)

type ToolPaths struct {
	DstDir       string
	TargetBundle string
	SrcBundle    string
}

// Marker filename created after a successful deployment. Presence is used to
// prove extraction happened without hard-coding tool contents.
const deployedMarker = ".extracted"

// Paths required to deploy the target daemon
type BaseToolDeploymentPaths struct {
	DeployedToolsDirectory string
}

func CreateToolDeploymentPaths(targetType conductor.PlatformConfiguration, toolInfo tool.ToolInfo, pm *packages.PackageManager, dstToolDir string) (ToolPaths, error) {
	bundleName := getToolDeployFileName(targetType, toolInfo)

	tp := ToolPaths{
		DstDir:       filepath.Join(dstToolDir, toolInfo.Name, toolInfo.Version),
		TargetBundle: filepath.Join(dstToolDir, toolInfo.Name, toolInfo.Version, bundleName),
	}

	{
		toolId := filepath.Join(toolInfo.Name, toolInfo.Version, bundleName)

		srcBundle, err := pm.GetToolBundle(toolId)
		if err != nil {
			return ToolPaths{}, err
		}

		tp.SrcBundle = srcBundle.Path()
	}

	return tp, nil
}

func GetToolBundleFileInfo(targetType conductor.PlatformConfiguration, toolInfo tool.ToolInfo, pm *packages.PackageManager) (os.FileInfo, error) {
	bundleName := getToolDeployFileName(targetType, toolInfo)
	toolID := filepath.Join(toolInfo.Name, toolInfo.Version, bundleName)
	pack, err := pm.GetToolBundle(toolID)
	if err != nil {
		return nil, err
	}
	return os.Stat(pack.Path())
}

func getToolTargetPath(targetType conductor.PlatformConfiguration, toolInfo tool.ToolInfo, dstToolDir string) ToolPaths {
	bundleName := getToolDeployFileName(targetType, toolInfo)

	return ToolPaths{
		DstDir:       filepath.Join(dstToolDir, toolInfo.Name, toolInfo.Version),
		TargetBundle: filepath.Join(dstToolDir, toolInfo.Name, toolInfo.Version, bundleName),
	}
}

func ReconcileTool(targetType conductor.PlatformConfiguration, toolInfo tool.ToolInfo, deployMode ToolDeploymentMode, cmdRunner conductor.CommandRunner, remoteFS conductor.TargetFilesystem, toolStore ToolPaths, progUpdate []conductor.ReportProgressRequest) (ReconcileResult, error) {
	normalisedDstDir := filepath.ToSlash(toolStore.DstDir)

	log.Infof("Checking tool deployment %s %s", toolInfo.Name, toolInfo.Version)
	toolDeployed := isToolDeployed(toolStore, toolInfo, targetType, remoteFS)

	// Handle check deployment type first
	if deployMode == ToolDeployCHECK {
		log.Infof("Checking tool deployment %s %s", toolInfo.Name, toolInfo.Version)
		if toolDeployed {
			// Tool already deployed, no action is needed
			log.Infof("Tool already deployed, no deployment required")
			return NoAction, nil
		} else {
			// Tool not deployed, deploy action is needed
			log.Infof("Tool not deployed, deployment needed")
			return Deploy, nil
		}
	}

	// Handle auto (force) deployment
	if deployMode == ToolDeployAUTOFORCE {
		log.Infof("Force deploying tool %s %s", toolInfo.Name, toolInfo.Version)

		log.Infof("Removing existing tool deployment %s", normalisedDstDir)
		if err := remoteFS.RemoveDirTree(normalisedDstDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			metadata := map[string]string{
				"toolName": toolInfo.Name,
				"version":  toolInfo.Version,
				"toolDir":  normalisedDstDir,
			}
			return NoAction, message.New(message.EngineToolDeployerRemoveExistingTool).WithCause(err).WithMetadata(metadata)
		}
	} else {
		if toolDeployed {
			log.Infof("Tool %s %s already deployed, no deployment required", toolInfo.Name, toolInfo.Version)
			return NoAction, nil
		}
	}

	log.Infof("Tool missing %s %s, deploying", toolInfo.Version, toolInfo.Name)
	err := deployTool(targetType, toolInfo, cmdRunner, remoteFS, toolStore, progUpdate, deployMode == ToolDeployAUTOFORCE)
	if err != nil {
		return NoAction, err
	}
	return Deployed, nil
}

// isToolDeployed checks that the expected bundle exists (and is non-empty)
// and that a deployment marker created after extraction is present. It does
// not currently validate the contents of the marker against the requested
// tool or platform.
func isToolDeployed(toolPaths ToolPaths, toolInfo tool.ToolInfo, targetType conductor.PlatformConfiguration, rfs conductor.TargetFilesystem) bool {
	normalisedDstDir := filepath.ToSlash(toolPaths.DstDir)
	normalisedBundle := filepath.ToSlash(toolPaths.TargetBundle)
	markerPath := filepath.ToSlash(filepath.Join(normalisedDstDir, deployedMarker))

	baseFields := log.Fields{
		"tool":    toolInfo.Name,
		"version": toolInfo.Version,
	}

	// Bundle must exist and be non-empty.
	log.WithFields(baseFields).Debug("checking tool deployment: verifying bundle exists")
	info, err := rfs.FileStat(normalisedBundle)
	if err != nil || info.Size() == 0 {
		log.WithFields(log.Fields{
			"tool":    toolInfo.Name,
			"version": toolInfo.Version,
			"bundle":  filepath.Base(normalisedBundle),
			"error":   err,
		}).Info("tool deployment invalid: bundle missing or empty")
		return false
	}

	// Marker must exist.
	log.WithFields(baseFields).Debug("checking tool deployment: verifying deployment marker exists")
	if _, err := rfs.FileStat(markerPath); err != nil {
		log.WithFields(log.Fields{
			"tool":    toolInfo.Name,
			"version": toolInfo.Version,
			"marker":  markerPath,
			"error":   err,
		}).Info("tool deployment invalid: deployment marker missing")
		return false
	}

	log.WithFields(baseFields).Info("tool deployment valid")
	return true
}

func getToolDeployFileName(targetType conductor.PlatformConfiguration, toolInfo tool.ToolInfo) string {
	return fmt.Sprintf("%s-%s-%s.tar.gz", toolInfo.Name, string(targetType.OS), string(targetType.Architecture))
}

// Copy the tool bundle to the target
// 1. create target directory
// 2. copy tool bundle
// 3. unzip
// If any step fails, remove the target directory
func deployTool(targetType conductor.PlatformConfiguration, toolInfo tool.ToolInfo, cmdRunner conductor.CommandRunner, rfs conductor.TargetFilesystem, toolPaths ToolPaths, progUpdate []conductor.ReportProgressRequest, forceDeploy bool) error {
	normalisedDstDir := filepath.ToSlash(toolPaths.DstDir)
	normalisedTargetBundle := filepath.ToSlash(toolPaths.TargetBundle)
	toolMetadata := map[string]string{
		"toolName": toolInfo.Name,
		"version":  toolInfo.Version,
	}

	tidyUp := func(err error) error {
		removeError := rfs.RemoveDirTree(normalisedDstDir)
		if removeError != nil {
			toolMetadata["toolDir"] = normalisedDstDir
			return message.New(message.EngineToolDeployerRemoveFailedDeployment).WithCause(errors.Join(removeError, err)).WithMetadata(toolMetadata)
		}
		return err
	}

	log.Info("generating deployment directory for tool deployment ", normalisedDstDir)
	// Ensure the tools directory is read/write/executable only by the owner.
	err := rfs.CreateDirTree(normalisedDstDir, perms.TargetToolDeploymentPerm)
	if err != nil {
		toolMetadata["toolDir"] = normalisedDstDir

		msgCode := message.EngineToolDeployerCreateToolDeploymentDir
		if errors.Is(err, iofs.ErrPermission) {
			msgCode = message.EngineToolDeployerCreateToolDeploymentDirPermissionDenied
		}

		return message.New(msgCode).WithCause(err).WithMetadata(toolMetadata)
	}

	normalisedSrcBundle := filepath.ToSlash(toolPaths.SrcBundle)

	log.Infof("sending tool bundle %s to %s", normalisedSrcBundle, normalisedTargetBundle)
	err = rfs.CopyFromHost(normalisedSrcBundle, normalisedTargetBundle, progUpdate)
	if err != nil {
		toolMetadata["toolPath"] = normalisedTargetBundle
		toolMetadata["srcPath"] = normalisedSrcBundle
		return tidyUp(message.New(message.EngineToolDeployerCopyTool).WithCause(err).WithMetadata(toolMetadata))
	}
	log.Infof("Successfully moved file: %s to %s", normalisedSrcBundle, normalisedTargetBundle)

	inflateCommand := fmt.Sprintf("tar zxf %s -C %s", normalisedTargetBundle, normalisedDstDir)
	if targetType.OS == conductor.Win {
		inflateCommand = fmt.Sprintf(
			`powershell -NoProfile -NoLogo -WindowStyle Hidden -Command "$bundle = %s; Set-Location -LiteralPath ([System.IO.Path]::GetDirectoryName($bundle)); tar -zxf ([System.IO.Path]::GetFileName($bundle)) -C %s"`,
			conductor.QuotePowershellString(normalisedTargetBundle),
			conductor.QuotePowershellString(normalisedDstDir),
		)
	}
	log.Infof(`inflating tool bundle "%s"`, inflateCommand)
	stdout, stderr, err := cmdRunner.RunCommand(inflateCommand)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"command": inflateCommand,
			"stdout":  stdout,
			"stderr":  stderr,
		}).Error("failed to inflate tool bundle")
		toolMetadata["toolPath"] = normalisedTargetBundle
		toolMetadata["toolDir"] = normalisedDstDir
		return tidyUp(message.New(message.EngineToolDeployerInflateToolBundle).WithCause(err).WithMetadata(toolMetadata))
	}

	// Record a deployment marker so subsequent runs can verify deployment state.
	markerPath := filepath.ToSlash(filepath.Join(normalisedDstDir, deployedMarker))
	if err := rfs.CreateEmptyFile(markerPath, perms.LocalFilePerm); err != nil {
		toolMetadata["markerPath"] = markerPath
		log.WithFields(log.Fields{
			"tool":       toolInfo.Name,
			"version":    toolInfo.Version,
			"markerPath": markerPath,
		}).Warnf("failed to write deployment marker; cleaning up deployment: %v", err)
		return tidyUp(message.New(message.EngineToolDeployerWriteMarker).WithCause(err).WithMetadata(toolMetadata))
	}

	return nil
}

func GetToolPath(toolInfo tool.ToolInfo, machineType conductor.PlatformConfiguration, toolBasePaths BaseToolDeploymentPaths, remoteFs conductor.TargetFilesystem) string {
	toolPaths := getToolTargetPath(machineType, toolInfo, toolBasePaths.DeployedToolsDirectory)
	if isToolDeployed(toolPaths, toolInfo, machineType, remoteFs) {
		return toolPaths.TargetBundle
	}
	return ""
}
