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
	TargetCollectionPath  []string
	TargetCollectorOutput util.Named[[]CollectorOutput]
}

// CollectionState is a mutable struct which stores information needed for run creation
// and file retrieval.
type CollectionState struct {
	RunBuilder          run.RunBuilder
	RunManifestUpdater  *run.RunManifestUpdater
	RunMetadataUpdater  *run.RunMetadataUpdater
	TargetInfoCollector TargetInfoCollector
}

// CreateRun creates a Run in the specified run directory.
// It constructs a RunBuilder to describe the Run contents.
// It also builds the initial Metadata for the Run.
// It leases the Run so that other processes know this Run is being worked on.
// The caller must ensure the Run is released when it is no longer being worked on.
func (r *CollectionState) CreateRun(ctx context.Context, c *run.RunCollection, rc *RecipeCtx, notifier run.MetadataUpdateNotifier) (run.RunID, func() error, error) {
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
	r.RunMetadataUpdater = run.NewRunMetadataUpdater(newRunID, c, notifier)

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

	return nil
}

type Collector struct {
	CollectionState *CollectionState
	TransferManager *TransferManager
}

func NewCollector(collectionState *CollectionState, transferManager *TransferManager) *Collector {
	return &Collector{CollectionState: collectionState, TransferManager: transferManager}
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

	// TransferManager owns manifest updates. Keep transfer paths logical;
	// compression is applied after glob expansion.
	componentAbsolutePath := r.CollectionState.RunManifestUpdater.ComponentPath(destRelativePath)

	r.TransferManager.AddTransfer(TransferRequest{
		FileTransfer: conductor.FileTransfer{
			RemotePath:    targetPath,
			LocalPath:     componentAbsolutePath,
			Exclude:       transferOptions.Exclude,
			ComponentType: componentType,
		},
		AgentSupplier:        agentSupplier,
		ManifestRelativePath: destRelativePath,
		ImmediateRetrieval:   transferOptions.ImmediateRetrieval,
		BackgroundTransfer:   transferOptions.BackgroundTransfer,
		Compressed:           transferOptions.Compressed,
	})

	return nil
}

// StoreComponent adds a component and rewrites the manifest, returning the absolute path where the component should
// be written
func (r *Collector) StoreComponent(
	destRelativePath string,
	componentType cdf.ComponentType,
) (string, error) {
	return r.storeComponent(destRelativePath, componentType, false)
}

func (r *Collector) storeComponent(destRelativePath string, componentType cdf.ComponentType, compressed bool) (string, error) {
	manifestUpdater := r.CollectionState.RunManifestUpdater
	componentAbsolutePath := manifestUpdater.ComponentPath(cdf.ManifestEntry{
		Path:       destRelativePath,
		Compressed: compressed,
	}.StoragePath())
	err := manifestUpdater.AddComponentWithFlags(destRelativePath, componentType, run.ComponentFlags{Compressed: compressed})
	if err != nil {
		return "", err
	}
	if err := manifestUpdater.WriteEntityDirs(); err != nil {
		return "", err
	}

	return componentAbsolutePath, nil
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
