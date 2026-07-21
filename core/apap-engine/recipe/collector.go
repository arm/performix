// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/conductor"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

type CollectorOutput struct {
	Filename      string
	ComponentType cdf.ComponentType
}

type TargetInfoCollector struct {
	TargetCollectionPath     []string
	TargetCollectorOutput    util.Named[[]CollectorOutput]
	TargetPIDCollectionPath  string
	TargetPIDCollectorOutput util.Named[CollectorOutput]
}

// CollectionState is a mutable struct which stores information needed for run creation
// and file retrieval.
type CollectionState struct {
	RunBuilder          run.RunBuilder
	RunManifestUpdater  *run.RunManifestUpdater
	TargetInfoCollector TargetInfoCollector
}

// CreateRun creates a Run in the specified run directory.
// It constructs a RunBuilder to describe the Run contents.
// It also builds the initial Metadata for the Run.
// It leases the Run so that other processes know this Run is being worked on.
// The caller must ensure the Run is released when it is no longer being worked on.
func (r *CollectionState) CreateRun(ctx context.Context, c *run.RunCollection, rc *RecipeCtx) (run.RunID, func() error, error) {
	var err error

	r.RunBuilder, err = c.RunBuilder()
	if err != nil {
		return run.InvalidRunID, nil, fmt.Errorf("failed to create run builder, %w", err)
	}

	metadata, err := rc.CreateMetadata(&r.RunBuilder, rc.ParamValues)
	if err != nil {
		return run.InvalidRunID, nil, fmt.Errorf("failed to create metadata, %w", err)
	}

	categorizationPath := run.AddRunCategorizationComponent(&r.RunBuilder)

	err = r.ConfigureCollectorRunBuilder(c)
	if err != nil {
		return run.InvalidRunID, nil, err
	}

	release, err := c.LeaseRun(ctx, r.RunBuilder.RunID())
	if err != nil {
		return run.InvalidRunID, nil, fmt.Errorf("failed to lease run ID, %w", err)
	}

	newRunID, err := c.CreateRun(r.RunBuilder, metadata)
	if err != nil {
		_ = release()
		return run.InvalidRunID, nil, fmt.Errorf("failed to create new run, %w", err)
	}

	err = run.WriteRunCategorization(categorizationPath, nil)
	if err != nil {
		_ = release()
		return run.InvalidRunID, nil, fmt.Errorf("failed to write run categorization, %w", err)
	}

	err = preserveRecipeInRun(&r.RunBuilder, rc.RecipePath, &rc.RecipeMetadata)
	if err != nil {
		_ = release()
		return run.InvalidRunID, nil, err
	}

	err = run.CreateInitialHostSourceCodePath(&r.RunBuilder, &rc.SourceCodePaths)
	if err != nil {
		_ = release()
		return run.InvalidRunID, nil, fmt.Errorf("failed to write source code, %w", err)
	}

	return newRunID, release, nil
}

// ConfigureCollectorRunBuilder contains builder component updates to stash the target collection details.
func (r *CollectionState) ConfigureCollectorRunBuilder(c *run.RunCollection) error {
	for i := range r.TargetInfoCollector.TargetCollectorOutput.Value {
		collectorRelativePath := filepath.Join("collector", r.TargetInfoCollector.TargetCollectorOutput.Name, r.TargetInfoCollector.TargetCollectorOutput.Value[i].Filename)
		targetCollection := r.RunBuilder.AddComponent(r.TargetInfoCollector.TargetCollectorOutput.Value[i].ComponentType, collectorRelativePath)
		r.TargetInfoCollector.TargetCollectionPath = append(r.TargetInfoCollector.TargetCollectionPath, targetCollection)
	}

	collectorRelativePath := filepath.Join("collector", r.TargetInfoCollector.TargetPIDCollectorOutput.Name, r.TargetInfoCollector.TargetPIDCollectorOutput.Value.Filename)
	targetCollection := r.RunBuilder.AddComponent(r.TargetInfoCollector.TargetPIDCollectorOutput.Value.ComponentType, collectorRelativePath)
	r.TargetInfoCollector.TargetPIDCollectionPath = targetCollection

	return nil
}

type Collector struct {
	CollectionState *CollectionState
	FileRetriever   FileRetriever
}

func NewRetrieveAgentFilesCollector(collectionState *CollectionState) *Collector {
	return &Collector{CollectionState: collectionState, FileRetriever: &RetrieveAgentFilesStageRetriever{}}
}

func NewTransferManagerCollector(runState *CollectionState, tm *TransferManager) *Collector {
	return &Collector{CollectionState: runState, FileRetriever: &TransferManagerRetriever{TransferManager: tm}}
}

func (r *Collector) AddComponent(componentType cdf.ComponentType, relativePath string) string {
	return r.CollectionState.RunBuilder.AddComponent(componentType, relativePath)
}

// QueueFileRetrieval queues a file transfer from the target machine into the run directory and updates the run manifest.
func (r *Collector) QueueFileRetrieval(
	targetPlatform *conductor.TargetPlatform,
	agentSupplier AgentConnSupplier,
	workingDir string,
	targetPath string,
	destRelativePath string,
	componentType cdf.ComponentType,
	transferOptions tool.TransferOptions,
) error {
	platform := targetPlatform

	// If the targetPath is not absolute, then append the temporary working directory from the target.
	targetPath = platform.Path.GetFullPath(targetPath, workingDir)

	var componentAbsolutePath string
	manifestRelativePath := ""
	if _, ok := r.FileRetriever.(*TransferManagerRetriever); ok {
		// TransferManager-backed retrieval owns manifest updates.
		componentAbsolutePath = r.CollectionState.RunManifestUpdater.ComponentPath(destRelativePath)
		manifestRelativePath = destRelativePath
	} else {
		var err error
		componentAbsolutePath, err = r.StoreComponent(destRelativePath, componentType)
		if err != nil {
			return err
		}
	}

	r.FileRetriever.AddResolvedComponentTransfer(TransferRequest{
		FileTransfer: conductor.FileTransfer{
			RemotePath:    targetPath,
			LocalPath:     componentAbsolutePath,
			Exclude:       transferOptions.Exclude,
			ComponentType: componentType,
		},
		AgentSupplier:        agentSupplier,
		ManifestRelativePath: manifestRelativePath,
		ImmediateRetrieval:   transferOptions.ImmediateRetrieval,
		BackgroundTransfer:   transferOptions.BackgroundTransfer,
	})

	return nil
}

// StoreComponent adds a component and rewrites the manifest, returning the absolute path where the component should
// be written
func (r *Collector) StoreComponent(
	destRelativePath string,
	componentType cdf.ComponentType,
) (string, error) {
	manifestUpdater := r.CollectionState.RunManifestUpdater
	componentAbsolutePath := manifestUpdater.ComponentPath(destRelativePath)
	if err := manifestUpdater.AddComponent(destRelativePath, componentType); err != nil {
		return "", err
	}
	if err := manifestUpdater.WriteEntityDirs(); err != nil {
		return "", err
	}

	return componentAbsolutePath, nil
}

// FileRetriever is an interface for retrieving files, backed either by a RetrieveAgentFilesStage
// or a TransferManager
type FileRetriever interface {
	AddResolvedComponentTransfer(transfer TransferRequest)
}

type RetrieveAgentFilesStageRetriever struct {
	FileTransfers []TransferRequest
	LogTransfers  []TransferRequest
}

// AddResolvedFileTransfer explicitly adds a transfer operation to the list of transfers. Most callers should use
// QueueFileTransfer instead, as that performs path resolution and adds the file to the CDF model. However, for
// some use cases, direct control is useful.
func (r *RetrieveAgentFilesStageRetriever) AddResolvedFileTransfer(transfer TransferRequest) {
	r.FileTransfers = append(r.FileTransfers, transfer)
}

// AddResolvedLogFileTransfer explicitly adds a transfer operation to the list of log transfers. Most callers should use
// QueueFileTransfer instead, as that performs path resolution and adds the file to the CDF model. However, for
// some use cases, direct control is useful.
func (r *RetrieveAgentFilesStageRetriever) AddResolvedLogFileTransfer(transfer TransferRequest) {
	r.LogTransfers = append(r.LogTransfers, transfer)
}

// AddResolvedComponentTransfer adds either a log transfer or a regular file transfer depending on the component type
func (r *RetrieveAgentFilesStageRetriever) AddResolvedComponentTransfer(transfer TransferRequest) {
	if cdf.IsLogComponentType(transfer.ComponentType) {
		r.AddResolvedLogFileTransfer(transfer)
	} else {
		r.AddResolvedFileTransfer(transfer)
	}
}

// TransferManagerRetriever implements FileRetriever, backed by a TransferManager. Calls to
// AddResolvedComponentTransfer are forwarded to the TransferManager's AddTransfer method.
type TransferManagerRetriever struct {
	TransferManager *TransferManager
}

func (r *TransferManagerRetriever) AddResolvedComponentTransfer(transfer TransferRequest) {
	r.TransferManager.AddTransfer(transfer)
}

type RecipeFileCollector struct {
	Collector      *Collector
	TargetPlatform conductor.TargetPlatform
	AgentSupplier  AgentConnSupplier
}

func NewRecipeFileCollector(c *Collector, targetPlatform conductor.TargetPlatform, agentSupplier AgentConnSupplier) *RecipeFileCollector {
	return &RecipeFileCollector{
		Collector:      c,
		TargetPlatform: targetPlatform,
		AgentSupplier:  agentSupplier,
	}
}

func (r *RecipeFileCollector) QueueFileRetrieval(outputEntityDir string, remotePath string, destRelativePath string, componentType cdf.ComponentType, transferOptions tool.TransferOptions) error {
	toolRelativePath := filepath.Join(outputEntityDir, destRelativePath)

	return r.Collector.QueueFileRetrieval(
		&r.TargetPlatform,
		r.AgentSupplier,
		"",
		remotePath,
		toolRelativePath,
		componentType,
		transferOptions,
	)
}

func (r *RecipeFileCollector) AddComponent(outputEntityDir string, componentType cdf.ComponentType, relativePath string) (string, error) {
	relativePath = filepath.Join(outputEntityDir, relativePath)
	componentPath, err := r.Collector.StoreComponent(relativePath, componentType)
	if err != nil {
		return "", err
	}
	return componentPath, nil
}
