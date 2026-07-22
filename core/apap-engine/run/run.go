// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/message"
	"github.com/Arm-Debug/apap-cli/apap-engine/perms"
	"github.com/Arm-Debug/apap-cli/apap-engine/run/recipemigration"
	"github.com/Arm-Debug/apap-cli/apap-engine/target"
	"github.com/Arm-Debug/apap-cli/apap-engine/terminology"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

const RunDirName = "runs"
const lockDirName = "locks"
const manifestFileName = "manifest.json"
const metadataFileName = "metadata.json"
const runIDLength = 12

var RecipeSourceRelativePath = filepath.Join("recipe", "source", "recipe.js")
var RecipeVersionRelativePath = filepath.Join("recipe", "version.json")
var InvalidRunID = RunID{Value: strings.Repeat("*", runIDLength)}

// RunID represents the ID of a run.
type RunID struct {
	Value string `json:"value"`
}

// RunCollection represents an entire collection of runs.
type RunCollection struct {
	primaryPath string
	allPaths    []string // Combination of primary & secondary paths to avoid re-computation

	// importDeps allows tests to override filesystem/zip operations used by ImportRun.
	importDeps importDeps
}

// RunRecipeLookup is a minimal interface needed for selecting a recipe.
type RunRecipeLookup interface {
	CheckRunsUseSameRecipe([]RunID) error
	CheckRunsUseSameRecipeNameOnly([]RunID) error
	GetNewestRun([]RunID) (RunID, error)
	GetRecipeComponentPath(RunID) (string, error)
	GetRecipeName(RunID) (string, error)
}

type RunWriter interface {
	WriteManifest(builder RunBuilder) error
	WriteEntityDirs(builder RunBuilder) error
}

type RunDescriptionProvider interface {
	RunDescription(context.Context, RunID) (*RunDescription, error)
}

type ConcreteRunWriter struct {
	RunCollection *RunCollection
}

// FileLockOptions represents the timing options for file locking.
type FileLockOptions struct {
	Deadline time.Duration
	Interval time.Duration
}

func defaultFileLockOptions() FileLockOptions {
	return FileLockOptions{
		Deadline: 10 * time.Second,
		Interval: 250 * time.Millisecond,
	}
}

func defaultLeaseOptions() FileLockOptions {
	return FileLockOptions{
		Deadline: 200 * time.Millisecond,
		Interval: 50 * time.Millisecond,
	}
}

func (r *ConcreteRunWriter) WriteManifest(builder RunBuilder) error {
	manifest := r.RunCollection.buildManifest(builder)
	return r.RunCollection.writeManifest(builder.RunID(), manifest)
}

func (r *ConcreteRunWriter) WriteEntityDirs(builder RunBuilder) error {
	return r.RunCollection.CreateEntityDirs(builder)
}

type RunResult string

const (
	RecipeSuccess                         RunResult = "success"
	RecipeInProgress                      RunResult = "in_progress"
	RecipeInProgressPhase1Complete        RunResult = "in_progress_phase1_complete"
	RecipeFailureConnectSSH               RunResult = "failure_connect_ssh"
	RecipeFailureConnectAgent             RunResult = "failure_connect_to_agent"
	RecipeFailureCollect                  RunResult = "failure_collect_target_info"
	RecipeFailureWorkloadOptions          RunResult = "failure_evaluate_workload_options"
	RecipeFailureNoShell                  RunResult = "failure_no_shell_available"
	RecipeFailureProfiling                RunResult = "failure_profiling"
	RecipeFailureIdentify                 RunResult = "failure_identify_target_architecture"
	RecipeFailureUnsupportedPlatform      RunResult = "failure_target_platform_unsupported"
	RecipeFailureCheckPlatformSupport     RunResult = "failure_check_platform_support"
	RecipeFailureRetrieve                 RunResult = "failure_retrieve_output_files"
	RecipeFailureRetrievePhase1Complete   RunResult = "failure_retrieve_output_files_phase1_complete"
	RecipeFailureDeploy                   RunResult = "failure_tool_deployment"
	RecipeFailureStage                    RunResult = "failure_recipe_stage"
	RecipeFailureTargetLock               RunResult = "failure_target_lock"
	RecipeFailureIncomplete               RunResult = "failure_incomplete_run"
	RecipeFailureIncompletePhase1Complete RunResult = "failure_incomplete_run_phase1_complete"
)

// NewRunCollectionWithSecondaryPaths constructs a new collection of runs at the specified primary path
// utilizing the secondary paths for lookup. This is used for backwards compatibility with previous run collection locations,
// allowing the engine to find runs that were created before the introduction of the new primary path.
func NewRunCollectionWithSecondaryPaths(primaryPath string, secondaryPaths []string) (*RunCollection, error) {
	abspath, err := filepath.Abs(primaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create runs directory: %w", err)
	}

	allPaths := make([]string, len(secondaryPaths)+1)
	allPaths[0] = abspath
	for i, p := range secondaryPaths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve secondary runs directory: %w", err)
		}
		allPaths[i+1] = absPath
	}

	return &RunCollection{primaryPath: abspath, allPaths: allPaths}, nil
}

// NewRunCollection constructs a new collection of runs at the specified primary path.
func NewRunCollection(primaryPath string) (*RunCollection, error) {
	return NewRunCollectionWithSecondaryPaths(primaryPath, nil)
}

func (c *RunCollection) runBasePath(entry RunID) (string, bool) {
	for _, basePath := range c.allPaths {
		exists, err := util.PathExists(filepath.Join(basePath, entry.Value))
		if err != nil {
			continue
		}
		if exists {
			return basePath, true
		}
	}
	return c.primaryPath, false
}

// RunBuilder constructs a new run builder.
func (c *RunCollection) RunBuilder() (RunBuilder, error) {
	newRunID, err := c.getNextRunID()
	if err != nil {
		return RunBuilder{}, fmt.Errorf("failed to get next run ID, %w", err)
	}
	return RunBuilder{runID: newRunID, basePath: c.primaryPath, runPath: c.GetRunPath(newRunID), entities: []cdf.Entity{}, components: []builderComponent{}}, nil
}

// NewRunRenderFS constructs a RunRenderFS for the specified run.
func (c *RunCollection) NewRunRenderFS(runID RunID) (*RunRenderFS, error) {
	if !c.runExists(runID) {
		return nil, message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runID.Value})
	}
	return &RunRenderFS{
		runID:         runID,
		runCollection: c,
	}, nil
}

// ListRuns returns a slice of RunIDs that exist in the run collection on-disk.
// Checks that directories resemble a valid-looking run before adding to slice.
func (c *RunCollection) ListRuns(ctx context.Context) ([]RunID, error) {
	runs := make([]RunID, 0)

	// Instantiate the run dir if it doesn't exist at this point. It is not
	// expected for the run collection to be missing, but we try to be
	// resilient of cases where the run collection might be broken instead of
	// just failing.
	if !c.exists() {
		err := c.create()
		if err != nil {
			return nil, fmt.Errorf("failed to list runs: %w", err)
		}
	}

	for i, basePath := range c.allPaths {
		dirs, err := os.ReadDir(basePath)
		if err != nil && i == 0 { // Only produce errors when listing from the primary path
			return nil, fmt.Errorf("failed to list runs from %s", basePath)
		}
		for d := range dirs {
			runID := RunID{Value: dirs[d].Name()}
			if dirs[d].IsDir() && c.entryLooksValid(ctx, runID) {
				runs = append(runs, runID)
			}
		}
	}

	return runs, nil
}

// exists determines whether the run collection exists on disk.
func (c *RunCollection) exists() bool {
	if _, err := os.Stat(c.primaryPath); err == nil {
		return true
	} else {
		return false
	}
}

// runExists determines whether the specified run exists on disk.
func (c *RunCollection) runExists(entry RunID) bool {
	if entry.Value == "" {
		return false
	}
	for _, basePath := range c.allPaths {
		runPath := filepath.Join(basePath, entry.Value)
		if !util.IsChildPath(basePath, runPath) {
			return false
		}
		exists, err := util.PathExists(runPath)
		if err != nil {
			continue
		}
		if exists {
			return true
		}
	}
	return false
}

// metadataExists determines whether the metadata.json file exists on disk for
// the specified run. Returns false if file doesn't exist or we fail to check.
func (c *RunCollection) metadataExists(entry RunID) bool {
	basePath, ok := c.runBasePath(entry)
	if !ok {
		return false
	}
	exists, err := util.PathExists(filepath.Join(basePath, entry.Value, metadataFileName))
	if err != nil {
		return false
	}
	return exists
}

// manifestExists determines whether the manifest.json file exists on disk for
// the specified run. Returns false if file doesn't exist or we fail to check.
func (c *RunCollection) manifestExists(entry RunID) bool {
	basePath, ok := c.runBasePath(entry)
	if !ok {
		return false
	}
	exists, err := util.PathExists(filepath.Join(basePath, entry.Value, manifestFileName))
	if err != nil {
		return false
	}
	return exists
}

// entryLooksValid determines whether the specified entry looks like a valid run:
//   - Is there a metadata file on disk with valid contents?
//   - Is there a manifest file on disk with valid contents?
func (c *RunCollection) entryLooksValid(ctx context.Context, entry RunID) bool {
	unlock, err := c.RLockRun(ctx, entry)
	if err != nil {
		return false
	}
	defer func() { _ = unlock() }()

	isValid := true

	if c.metadataExists(entry) {
		_, err := c.readMetadata(entry)
		if err != nil {
			isValid = false
		}
	} else {
		isValid = false
	}

	if c.manifestExists(entry) {
		_, err := c.readManifest(entry)
		if err != nil {
			isValid = false
		}
	} else {
		isValid = false
	}

	return isValid
}

// CreateRun creates a new run in the run collection. First, it creates the
// directory structure for the run by creating all entities. Then it creates
// the manifest using the builder provided by the recipe parser. Finally, it
// creates the initial metadata based on what we know so far (some fields get
// updated later on). If successful, it returns the RunID of the new run.
func (c *RunCollection) CreateRun(builder RunBuilder, metadata *cdf.Metadata) (RunID, error) {
	var err error

	// Instantiate the run dir if it doesn't exist at this point. It is not
	// expected for the run collection to be missing, but we try to be
	// resilient of cases where the run collection might be broken instead of
	// just failing.
	if !c.exists() {
		err = c.create()
		if err != nil {
			return InvalidRunID, fmt.Errorf("failed to create run: %w", err)
		}
	}

	if c.runExists(builder.runID) {
		return InvalidRunID, fmt.Errorf("failed to create run, already exists")
	}

	// Create run root dir
	err = os.Mkdir(c.GetRunPath(builder.runID), perms.LocalDirPerm)
	if err != nil {
		return InvalidRunID, fmt.Errorf("failed to create runID directory")
	}
	err = c.CreateEntityDirs(builder)
	if err != nil {
		return InvalidRunID, err
	}

	err = c.UpdateManifest(builder)
	if err != nil {
		return InvalidRunID, err
	}
	err = c.writeMetadata(builder.runID, metadata)
	if err != nil {
		return InvalidRunID, fmt.Errorf("failed to create metadata for run: %w", err)
	}

	return builder.runID, nil
}

// DeleteRun deletes an entry from the run collection.
// First, it acquires a lease to ensure no one else is running a recipe on it.
// Second, it acquires an exclusive lock to ensure no one else is modifying the run files.
// Finally, it deletes the run directory from disk.
// As a best-effort, it also attempts to clean up the lock directory.
// The acquisition order (lease before lock) is critical to prevent lock-inversion
// deadlocks with the recovery manager, which follows the same order.
func (c *RunCollection) DeleteRun(ctx context.Context, entry RunID) error {
	release, err := c.LeaseRun(ctx, entry)
	if err != nil {
		return err
	}
	defer func() { _ = release() }()

	unlock, err := c.LockRun(ctx, entry)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	// Check if run still exists (avoids TOCTOU)
	if !c.runExists(entry) {
		return message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": entry.Value})
	}

	runPath := c.GetRunPath(entry)
	err = os.RemoveAll(runPath)
	if err != nil {
		return message.New(message.EngineRunDeleteFailure).WithCause(err)
	}

	// Best effort to clean up the lock directory
	runLockDir := c.getLockDir(entry)
	_ = unlock()
	_ = release()
	_ = os.RemoveAll(runLockDir)

	return nil
}

// DeleteRuns is a convenience function for deleting multiple runs. Calls `DeleteRun` on each provided runID. Returns
// a slice of errors where each error corresponds to the attempt to delete the run at the same index in the provided
// slice of ids.
func (c *RunCollection) DeleteRuns(ctx context.Context, entries []RunID) []error {
	var errs = make([]error, len(entries))
	for i, entry := range entries {
		err := c.DeleteRun(ctx, entry)
		if err != nil {
			errs[i] = err
		}
	}
	return errs
}

// DeleteAllRuns deletes every run in the collection, returning the runs that were attempted and their corresponding errors.
func (c *RunCollection) DeleteAllRuns(ctx context.Context) ([]RunID, []error, error) {
	ids, err := c.ListRuns(ctx)
	if err != nil {
		return nil, nil, err
	}
	return ids, c.DeleteRuns(ctx, ids), nil
}

type RunLoader interface {
	LoadRun(id RunID) (*cdf.OnDiskModel, error)
}

// LoadRun returns the OnDiskModel of the Run with the specified RunID.
// Called by renderers to access the data OnDiskModel of a particular Run.
func (c *RunCollection) LoadRun(id RunID) (*cdf.OnDiskModel, error) {
	if !c.runExists(id) {
		return nil, message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": id.Value})
	}
	basePathDir, ok := c.runBasePath(id)
	if !ok {
		return nil, message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": id.Value})
	}
	runRerender, err := c.NewRunRenderFS(id)
	if err != nil {
		return nil, err
	}
	if err := runRerender.CleanupStaleRenders(context.Background()); err != nil {
		log.WithError(err).WithField("runID", id.Value).Warn("failed to cleanup stale rerenders")
	}
	basePath := path.Join(basePathDir, id.Value)
	manifest, err := c.readManifest(id)
	if err != nil {
		return nil, err
	}
	metadata, err := c.readMetadata(id)
	if err != nil {
		return nil, err
	}

	return cdf.NewOnDiskModel(basePath, &manifest, metadata), nil
}

// GetRunPath returns the root directory of a Run on disk.
// Used when constructing a new Run.
func (c *RunCollection) GetRunPath(e RunID) string {
	basePath, _ := c.runBasePath(e)
	return filepath.Join(basePath, e.Value)
}

// Create will create the run collection's root directory on disk. We only expect
// the runs to be missing on new installations, or in scenarios where the
// filesystem has been corrupted or unintentionally modified.
func (c *RunCollection) create() error {
	if c.exists() {
		return fmt.Errorf("failed to create run directory, already exists")
	} else {
		err := os.MkdirAll(c.primaryPath, perms.LocalDirPerm)
		if err != nil {
			return fmt.Errorf("failed to create run directory: %w", err)
		}
	}

	return nil
}

// getNextRunID() generates the next available RunID in the run collection.
// It first generates a 36 char UUID in RFC4122 format, then takes the last
// 12 characters and checks an entry doesn't already exist in the collection
// with this ID. Tries up to three times before returning an error.
func (c *RunCollection) getNextRunID() (RunID, error) {
	for range 3 {
		uuid := uuid.New().String()
		if len(uuid) != 36 {
			continue
		}
		nextRunID := RunID{Value: string(uuid[len(uuid)-runIDLength:])}
		if !c.runExists(nextRunID) && len(nextRunID.Value) == runIDLength {
			return nextRunID, nil
		}
	}

	return InvalidRunID, fmt.Errorf("failed to get next run ID")
}

// readManifest returns the existing Manifest for a Run.
func (c *RunCollection) readManifest(entry RunID) (cdf.Manifest, error) {
	manifestPath := c.getManifestPath(entry)
	manifest, err := util.ReadJSONFile[cdf.Manifest](manifestPath)
	if err != nil {
		metadata := map[string]string{
			"runID": entry.Value,
			"path":  manifestPath,
		}
		return cdf.Manifest{}, message.New(message.EngineRunReadManifest).WithCause(err).WithMetadata(metadata)
	}
	return *manifest, nil
}

func (c *RunCollection) getManifestPath(entry RunID) string {
	basePath, _ := c.runBasePath(entry)
	return filepath.Join(basePath, entry.Value, manifestFileName)
}

// UpdateManifest builds a manifest and commits it to the disk
func (c *RunCollection) UpdateManifest(builder RunBuilder) error {
	manifest := c.buildManifest(builder)
	err := c.writeManifest(builder.runID, manifest)
	if err != nil {
		return fmt.Errorf("failed to create manifest for run: %w", err)
	}
	return nil
}

// CreateEntityDirs creates a directory for each RunBuilder entity
func (c *RunCollection) CreateEntityDirs(builder RunBuilder) error {
	newEntryDir := c.GetRunPath(builder.runID)
	for i := range builder.entities {
		entryPath := filepath.Join(newEntryDir, builder.entities[i].RelativePath)
		err := os.MkdirAll(entryPath, perms.LocalDirPerm)
		if err != nil {
			return fmt.Errorf("failed to create entity dir for run: %w", err)
		}
	}
	return nil
}

// buildManifest builds the Manifest for a Run based on the provided RunBuilder.
func (c *RunCollection) buildManifest(builder RunBuilder) *cdf.Manifest {
	return builder.buildManifest()
}

// writeManifest writes the Manifest file for a Run (e.g. after a recipe run or run render).
func (c *RunCollection) writeManifest(entry RunID, manifest *cdf.Manifest) error {
	basePath, _ := c.runBasePath(entry)

	// File transfers can result in multiple manifest updates while renderers,
	// recovery, or another engine process read it, so replace the file atomically.
	return util.WriteJSONFileAtomic[cdf.Manifest](filepath.Join(basePath, entry.Value, manifestFileName), manifest, perms.LocalFilePerm)
}

// RemovePendingManifestEntries removes stale pending transfer entries from an existing manifest.
func (c *RunCollection) RemovePendingManifestEntries(entry RunID) (bool, error) {
	manifest, err := c.readManifest(entry)
	if err != nil {
		return false, err
	}

	entries := manifest.Entries[:0]
	removed := false
	for _, manifestEntry := range manifest.Entries {
		if manifestEntry.Pending {
			removed = true
			continue
		}
		entries = append(entries, manifestEntry)
	}
	if !removed {
		return false, nil
	}

	manifest.Entries = entries
	return true, c.writeManifest(entry, &manifest)
}

// readMetadata returns the existing Metadata for a Run.
func (c *RunCollection) readMetadata(entry RunID) (cdf.Metadata, error) {
	metadataPath := c.getMetadataPath(entry)
	value, err := util.ReadJSONFile[cdf.Metadata](metadataPath)
	if err != nil {
		metadata := map[string]string{
			"runID": entry.Value,
			"path":  metadataPath,
		}
		return cdf.Metadata{}, message.New(message.EngineRunReadMetadata).WithCause(err).WithMetadata(metadata)
	}
	return *value, nil
}

func (c *RunCollection) getMetadataPath(entry RunID) string {
	basePath, _ := c.runBasePath(entry)
	return filepath.Join(basePath, entry.Value, metadataFileName)
}

// readCategorization returns the categorization data for a Run.
// Missing categorization files are treated as legacy runs with empty categorization data.
func (c *RunCollection) readCategorization(entry RunID) (RunCategorization, error) {
	categorizationPath := c.getCategorizationPath(entry)
	value, err := ReadRunCategorization(categorizationPath)
	if err != nil {
		metadata := map[string]string{
			"runID": entry.Value,
			"path":  categorizationPath,
		}
		return RunCategorization{}, message.New(message.EngineRunReadCategorization).WithCause(err).WithMetadata(metadata)
	}
	return *value, nil
}

func (c *RunCollection) getCategorizationPath(entry RunID) string {
	basePath, _ := c.runBasePath(entry)
	return filepath.Join(basePath, entry.Value, CategorizationFilename)
}

// writeMetadata writes the Metadata file for a Run.
func (c *RunCollection) writeMetadata(entry RunID, metadata *cdf.Metadata) error {
	return util.WriteJSONFile(c.getMetadataPath(entry), metadata, perms.LocalFilePerm)
}

// UpdateRunResult is used to update the recipe result field in the metadata of an existing run.
// Used in the event of a recipe run success/failure
func (c *RunCollection) UpdateRunResult(ctx context.Context, entry RunID, recipeResult RunResult, e error) error {
	unlock, err := c.LockRun(ctx, entry)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	// Check if run still exists (avoids TOCTOU)
	if !c.runExists(entry) {
		return message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": entry.Value})
	}

	metadata, err := c.readMetadata(entry)
	if err != nil {
		return err
	}

	metadata.RunResult = string(recipeResult)
	if e != nil {
		// Human readable error message
		human := e.Error()
		if m := message.IsMessage(e); m != nil {
			if catalogMsg, err := message.LookupMessage(m); err == nil {
				human = catalogMsg.Text()
			}
		}
		metadata.RunError = human
	}

	err = c.writeMetadata(entry, &metadata)
	if err != nil {
		return err
	}
	return nil
}

// SetRunEndTime sets the end time in the metadata of an existing run. It is a no-op if the end
// time for this run is already set.
func (c *RunCollection) SetRunEndTime(ctx context.Context, entry RunID) error {
	unlock, err := c.LockRun(ctx, entry)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	// Check if run still exists (avoids TOCTOU)
	if !c.runExists(entry) {
		return message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": entry.Value})
	}

	metadata, err := c.readMetadata(entry)
	if err != nil {
		return err
	}

	if metadata.EndTime == util.InvalidTime() {
		metadata.EndTime = util.CurrentTime()
	}

	err = c.writeMetadata(entry, &metadata)
	if err != nil {
		return err
	}
	return nil
}

// UpdateWorkingDir is used to set the working directory for a given run. It is a no-op if the
// working directory for this run is already set (or if the provided working directory is empty).
func (c *RunCollection) UpdateWorkingDir(ctx context.Context, entry RunID, workingDir string) error {
	unlock, err := c.LockRun(ctx, entry)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	// Check if run still exists (avoids TOCTOU)
	if !c.runExists(entry) {
		return message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": entry.Value})
	}

	metadata, err := c.readMetadata(entry)
	if err != nil {
		return err
	}

	if metadata.WorkloadType != "Launch" || metadata.WorkingDir != "" || workingDir == "" {
		return nil
	}

	metadata.WorkingDir = workingDir
	err = c.writeMetadata(entry, &metadata)
	if err != nil {
		msgMetadata := map[string]string{
			"runID": entry.Value,
			"path":  c.getMetadataPath(entry),
		}
		return message.New(message.EngineRunUpdateMetadata).WithCause(err).WithMetadata(msgMetadata)
	}
	return nil
}

// RenameRun is used to update the name field for a given run
func (c *RunCollection) RenameRun(ctx context.Context, entry RunID, newName string) error {
	unlock, err := c.LockRun(ctx, entry)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	// Check if run still exists (avoids TOCTOU)
	if !c.runExists(entry) {
		return message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": entry.Value})
	}

	metadata, err := c.readMetadata(entry)
	if err != nil {
		return err
	}
	metadata.Name = newName
	err = c.writeMetadata(entry, &metadata)
	if err != nil {
		msgMetadata := map[string]string{
			"runID": entry.Value,
			"path":  c.getMetadataPath(entry),
		}
		return message.New(message.EngineRunUpdateMetadata).WithCause(err).WithMetadata(msgMetadata)
	}
	return nil
}

// ExportRun is used to export a given run to a .zip file at a specified directory
func (c *RunCollection) ExportRun(ctx context.Context, runID RunID, dstDir string) error {
	unlock, err := c.RLockRun(ctx, runID)
	if err != nil {
		return err
	}
	defer func() { _ = unlock() }()

	// Check if run still exists (avoids TOCTOU)
	if !c.runExists(runID) {
		return message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runID.Value})
	}

	// Get paths
	srcDir := c.GetRunPath(runID)
	dstZip := filepath.Join(dstDir, fmt.Sprintf("%s.zip", runID.Value))

	// Zip run to destination
	return zipDirectory(ctx, srcDir, dstZip)
}

// importDeps holds the filesystem/zip operations ImportRun depends on.
// Tests can override one or more of these per RunCollection instance.
type importDeps struct {
	getRunIDFromZip    func(zipPath string) (string, error)
	estimatedUnzipSize func(zipPath string) (uint64, error)
	mkdirTemp          func(dir, pattern string) (string, error)
	unzip              func(zipPath, dstDir string) error
	rename             func(oldPath, newPath string) error
}

// Estimates the size of a zip archive once unzipped, before actually unzipping it
// Used as a best-effort predictor of whether enough disk space is available to unzip
func estimatedUnzipSizeFn(zipPath string) (uint64, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, err
	}
	defer r.Close()

	var total uint64
	for _, file := range r.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > math.MaxUint64-total {
			return 0, fmt.Errorf("zip contents too large to sum")
		}
		total += file.UncompressedSize64
	}

	return total, nil
}

// resolveImportDeps returns a fully-populated dependency set.
// Defaults are overridden by any non-nil function fields in c.importDeps.
func (c *RunCollection) resolveImportDeps() importDeps {
	d := importDeps{
		getRunIDFromZip:    getRunIDFromZipFn,
		estimatedUnzipSize: estimatedUnzipSizeFn,
		mkdirTemp:          os.MkdirTemp,
		unzip:              unzipFn,
		rename:             os.Rename,
	}

	// Allow partial overriding (tests can set just one func).
	if c.importDeps.getRunIDFromZip != nil {
		d.getRunIDFromZip = c.importDeps.getRunIDFromZip
	}
	if c.importDeps.estimatedUnzipSize != nil {
		d.estimatedUnzipSize = c.importDeps.estimatedUnzipSize
	}
	if c.importDeps.mkdirTemp != nil {
		d.mkdirTemp = c.importDeps.mkdirTemp
	}
	if c.importDeps.unzip != nil {
		d.unzip = c.importDeps.unzip
	}
	if c.importDeps.rename != nil {
		d.rename = c.importDeps.rename
	}

	return d
}

func (c *RunCollection) ImportRun(runPath string) (RunID, error) {
	deps := c.resolveImportDeps()

	// Attempt to read oldID
	oldID, err := deps.getRunIDFromZip(runPath)
	if err != nil {
		return RunID{}, err
	}

	// Generate new ID if run exists
	runID := RunID{Value: oldID}
	if runID.Value == "" || strings.ContainsAny(runID.Value, `./\`) {
		err := fmt.Errorf("run ID %q is not a valid archive root", oldID)
		return RunID{}, message.New(message.EngineRunZipFileInvalid).WithCause(err).WithMetadata(map[string]string{"zipPath": runPath})
	}
	if c.runExists(runID) {
		runID, err = c.getNextRunID()
		if err != nil {
			metadata := map[string]string{
				"zipPath": runPath,
				"runID":   oldID,
			}
			return runID, message.New(message.EngineRunGenerateRunIdFailed).WithCause(err).WithMetadata(metadata)
		}
	}

	// Prepare low disk space error message metadata with defaults
	requiredDiskSpace := "unknown"
	if requiredBytes, err := deps.estimatedUnzipSize(runPath); err == nil {
		requiredDiskSpace = util.FormatBytesIEC(requiredBytes)
	}

	// Unzip run to a temp directory under the runs dir - this is done to ensure that the runs directory never
	// contains an invalid run which we failed to clean up. Runs are only moved to the runs directory once we've
	// successfully unzipped them.
	tempDir, err := deps.mkdirTemp(c.primaryPath, fmt.Sprintf("%v-run-%v-import-*", terminology.GetProductBinaryName(), runID.Value))
	if err != nil {
		if util.IsLowDiskSpace(err) {
			return RunID{}, message.New(message.EngineRunLowDiskSpaceTempDir).
				WithMetadata(map[string]string{"requiredDiskSpace": requiredDiskSpace}).
				WithCause(err)
		}
		return RunID{}, message.New(message.CommonUnknownError).WithCause(err)
	}
	// Clean up temp dir
	defer func() {
		cleanupErr := os.RemoveAll(tempDir)
		if cleanupErr != nil {
			log.Warnf("ImportRun: failed to clean up temporary dir `%v`: %v", tempDir, cleanupErr)
		}
	}()

	err = deps.unzip(runPath, tempDir)
	if err != nil {
		if util.IsLowDiskSpace(err) {
			return RunID{}, message.New(message.EngineRunLowDiskSpaceTempDir).
				WithMetadata(map[string]string{"requiredDiskSpace": requiredDiskSpace}).
				WithCause(err)
		}
		return RunID{}, message.New(message.EngineRunZipFileInvalid).WithCause(err).WithMetadata(map[string]string{"zipPath": runPath})
	}

	// Move unzipped run to runs directory
	err = deps.rename(filepath.Join(tempDir, oldID), filepath.Join(c.primaryPath, runID.Value))
	if err != nil {
		if util.IsLowDiskSpace(err) {
			return RunID{}, message.New(message.EngineRunLowDiskSpaceRunDir).
				WithMetadata(map[string]string{"requiredDiskSpace": requiredDiskSpace, "runsDir": filepath.Clean(c.primaryPath)}).
				WithCause(err)
		}
		return RunID{}, message.New(message.EngineRunZipFileInvalid).WithCause(err).WithMetadata(map[string]string{"zipPath": runPath})
	}

	return runID, nil
}

// LockRun attempts to acquire an exclusive file lock for the specified run.
// Used for API-level run operations.
// Tries to obtain the lock within a default deadline, retrying at regular intervals.
// On success, returns an unlock() that should be called to release the lock when done.
// On failure (i.e. deadline exceeded or run does not exist), returns an error.
func (c *RunCollection) LockRun(ctx context.Context, runID RunID) (unlock func() error, err error) {
	return c.lockRun(ctx, runID, false)
}

// RLockRun attempts to acquire a shared file lock for the specified run.
// Used for API-level run operations.
// Tries to obtain the lock within a default deadline, retrying at regular intervals.
// On success, returns an unlock() that should be called to release the lock when done.
// On failure (i.e. deadline exceeded or run does not exist), returns an error.
func (c *RunCollection) RLockRun(ctx context.Context, runID RunID) (unlock func() error, err error) {
	return c.lockRun(ctx, runID, true)
}

// lockRun is a helper to acquire either an exclusive or shared file lock for the specified run.
func (c *RunCollection) lockRun(ctx context.Context, runID RunID, shared bool) (unlock func() error, err error) {
	if !c.runExists(runID) {
		return nil, message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runID.Value})
	}

	fileLock, err := c.newFileLock(runID)
	if err != nil {
		return nil, err
	}

	// Cancelation & timeout
	opts := defaultFileLockOptions()
	tightCtx, cancel := context.WithTimeout(ctx, opts.Deadline)
	defer cancel()

	var locked bool
	if shared {
		locked, err = fileLock.TryRLockContext(tightCtx, opts.Interval)
	} else {
		locked, err = fileLock.TryLockContext(tightCtx, opts.Interval)
	}
	if err != nil || !locked {
		return nil, message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runID.Value})
	}

	unlock = func() error {
		err := fileLock.Unlock()
		if err != nil {
			return err
		}
		return nil
	}

	return unlock, nil
}

func (c *RunCollection) newFileLock(runID RunID) (*flock.Flock, error) {
	runLockDir := c.getLockDir(runID)

	err := os.MkdirAll(runLockDir, perms.LocalDirPerm)
	if err != nil {
		metadata := map[string]string{
			"runID":   runID.Value,
			"lockDir": runLockDir,
		}
		return nil, message.New(message.EngineRunCreateLock).WithMetadata(metadata)
	}

	lockFilePath := filepath.Join(runLockDir, "lockfile")
	return flock.New(lockFilePath), nil
}

func (c *RunCollection) getLockDir(runID RunID) string {
	parentDir := filepath.Dir(c.primaryPath)
	locksDir := filepath.Join(parentDir, lockDirName)
	runLockDir := filepath.Join(locksDir, runID.Value)

	return runLockDir
}

// LeaseRun attempts to create and acquire an exclusive lease (in the form of a file lock) for the specified run.
// Used to indicate whether a run is currently in use by a recipe execution.
// On success, returns a release() that should be called to release the lease when done.
// On failure (i.e. run ID looks like a path or the run is already leased), returns an error.
func (c *RunCollection) LeaseRun(ctx context.Context, runID RunID) (release func() error, err error) {
	if runID.Value == "" || strings.ContainsAny(runID.Value, `./\`) {
		return nil, message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runID.Value})
	}

	fileLease, err := c.newLease(runID)
	if err != nil {
		return nil, err
	}

	// Cancellation & timeout
	opts := defaultLeaseOptions()
	tightCtx, cancel := context.WithTimeout(ctx, opts.Deadline)
	defer cancel()

	locked, err := fileLease.TryLockContext(tightCtx, opts.Interval)
	if err != nil || !locked {
		return nil, message.New(message.EngineRunBusy).WithMetadata(map[string]string{"runID": runID.Value})
	}

	release = func() error {
		err := fileLease.Unlock()
		if err != nil {
			return err
		}
		return nil
	}

	return release, nil
}
func (c *RunCollection) newLease(runID RunID) (*flock.Flock, error) {
	runLockDir := c.getLockDir(runID)

	err := os.MkdirAll(runLockDir, perms.LocalDirPerm)
	if err != nil {
		metadata := map[string]string{
			"runID":   runID.Value,
			"lockDir": runLockDir,
		}
		return nil, message.New(message.EngineRunCreateLock).WithMetadata(metadata)
	}

	lockFilePath := filepath.Join(runLockDir, "leasefile")
	return flock.New(lockFilePath), nil
}

func getRunIDFromZipFn(zipPath string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	metadata := map[string]string{"zipPath": zipPath}
	if err != nil {
		if _, statErr := os.Stat(zipPath); statErr != nil {
			return "", message.New(message.EngineRunZipFileNotExist).WithCause(statErr).WithMetadata(metadata)
		}
		return "", message.New(message.EngineRunOpenZipFile).WithCause(err).WithMetadata(metadata)
	}
	defer r.Close()

	if len(r.File) == 0 {
		err = errors.New("imported zip is empty")
		return "", message.New(message.EngineRunZipFileInvalid).WithCause(err).WithMetadata(metadata)
	}

	err = validateZipArchive(r, zipPath)
	if err != nil {
		return "", err
	}

	return filepath.Base(r.File[0].Name), nil
}

// validateZipArchive attempts to verify that the zip archive to import does actually contain a run.
// This verification is not exhaustive and can definitely be spoofed, but is likely to catch unintentional
// attempts to import non-run zip archives. Checks:
//
// - that the zip archive contains exactly 1 top-level directory
// - that this top-level directory contains a `metadata.json` file and a `manifest.json` file
func validateZipArchive(r *zip.ReadCloser, zipPath string) message.Message {
	topLevels := make(map[string]struct{})
	hasManifest := false
	hasMetadata := false

	metadata := map[string]string{"zipPath": zipPath}

	for _, file := range r.File {
		normalized := strings.TrimPrefix(file.Name, "/")
		if normalized == "" || normalized == "." {
			continue
		}

		parts := strings.Split(normalized, "/")
		if len(parts) == 0 {
			continue
		}

		topLevel := parts[0]
		if topLevel == "" {
			continue
		}

		topLevels[topLevel] = struct{}{}

		if len(parts) == 1 {
			if !file.FileInfo().IsDir() {
				err := fmt.Errorf("imported zip contains top-level file %q; expected a single directory", file.Name)
				return message.New(message.EngineRunZipFileInvalid).WithCause(err).WithMetadata(metadata)
			}
			continue
		}

		if len(parts) == 2 && !file.FileInfo().IsDir() {
			switch parts[1] {
			case manifestFileName:
				hasManifest = true
			case metadataFileName:
				hasMetadata = true
			}
		}
	}

	if len(topLevels) != 1 {
		err := fmt.Errorf("imported zip must contain exactly one top-level directory, found %d", len(topLevels))
		return message.New(message.EngineRunZipFileInvalid).WithCause(err).WithMetadata(metadata)
	}

	missing := make([]string, 0, 2)
	if !hasMetadata {
		missing = append(missing, metadataFileName)
	}
	if !hasManifest {
		missing = append(missing, manifestFileName)
	}
	if len(missing) > 0 {
		err := fmt.Errorf("imported zip is missing required file(s): %s", strings.Join(missing, ", "))
		return message.New(message.EngineRunZipFileInvalid).WithCause(err).WithMetadata(metadata)
	}

	return nil
}

// sanitizeArchiveEntry checks that the given entryPath from an abitrary zip archive is a valid path to extract to within dstDir, and returns the absolute path to extract to if it is valid. Checks:
// - that the entryPath is not an absolute path (e.g. /etc/passwd)
// - that the entryPath does not overwrite existing files on disk at the destination
// - that the entryPath is a child of the destination directory (e.g. dstDir/entryPath), to prevent zip slip vulnerabilities
func sanitizeArchiveEntry(entryPath string, dstDir string) (string, error) {
	dstDirAbs, err := filepath.Abs(dstDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path of destination directory: %w", err)
	}

	// Ensure entry is not an absolute path (e.g. /etc/passwd)
	if filepath.IsAbs(entryPath) {
		return "", fmt.Errorf("illegal archive entry: `%s`, must be a relative path", entryPath)
	}

	entryPathAbs, err := filepath.Abs(filepath.Join(dstDir, entryPath))
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path of archive entry: %w", err)
	}

	// Ensure entry path does not overwrite existing files
	if _, err := os.Stat(entryPathAbs); err == nil {
		return "", fmt.Errorf("illegal archive entry: `%s`, file already exists", entryPathAbs)
	}

	// Reject archive entries that resolve outside the extraction directory.
	if !util.IsChildPath(dstDirAbs, entryPathAbs) {
		return "", fmt.Errorf("illegal archive entry: `%s`, must be a child of `%s`", entryPath, dstDir)
	}

	return entryPathAbs, nil
}

func unzipFn(srcPath string, dstDir string) error {
	r, err := zip.OpenReader(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file '%v': %w", srcPath, err)
	}
	defer r.Close()

	// Create destination directory if it does not exist
	if err := os.MkdirAll(dstDir, perms.LocalDirPerm); err != nil {
		return fmt.Errorf("failed to create import dstDir '%v': %w", dstDir, err)
	}

	for _, file := range r.File {
		// Check for path traversal vulnerabilities before extracting a sanitized path.
		fpath, err := sanitizeArchiveEntry(file.Name, dstDir)
		if err != nil {
			return err
		}

		if file.FileInfo().IsDir() {
			// Make Folder
			err := os.MkdirAll(fpath, perms.LocalDirPerm)
			if err != nil {
				return err
			}
			continue
		}

		// Ensure parent dir exists with directory permissions before creating file
		if err := os.MkdirAll(filepath.Dir(fpath), perms.LocalDirPerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE, perms.LocalFilePerm)
		if err != nil {
			return err
		}
		defer outFile.Close()

		rc, err := file.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		// Copy file incrementally to avoid compression bomb vulnerability
		for {
			_, err := io.CopyN(outFile, rc, 1024)
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
		}

		// Close the file without defer to close before next iteration of loop
		// Surface file close errors (ENOSPC low disk space errors can originate here)
		if err := rc.Close(); err != nil {
			_ = outFile.Close()
			return err
		}
		if err := outFile.Close(); err != nil {
			return err
		}
	}

	return nil
}

// zipDirectory archives a directory to dstPath, excluding rerender output data.
func zipDirectory(ctx context.Context, srcDir string, dstPath string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Setup destination directory
	if err := os.MkdirAll(filepath.Dir(dstPath), perms.LocalDirPerm); err != nil {
		return message.New(message.EngineRunCreateZipDirectory).WithCause(err).WithMetadata(map[string]string{"dstDir": filepath.Dir(dstPath)})
	}

	// Create zip file at destination
	zipFile, err := os.Create(dstPath)
	if err != nil {
		return message.New(message.EngineRunCreateZipFile).WithCause(err).WithMetadata(map[string]string{"dstPath": dstPath})
	}
	defer func() {
		zipFile.Close()
		if err != nil {
			_ = os.Remove(dstPath)
		}
	}()

	zipWriter := zip.NewWriter(zipFile)

	// Extract base name of directory to use as the top-level directory in zip
	baseDir := filepath.Base(srcDir)

	err = filepath.Walk(srcDir, func(path string, info fs.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return message.New(message.CommonUnknownError).WithCause(err)
		}

		// Get relative path for zip file header
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("error getting relative path: %w", err))
		}
		// Exclude rerender output directory
		if relPath == renderDirName || strings.HasPrefix(relPath, renderDirName+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relPath = filepath.Join(baseDir, relPath)
		normalisedRelPath := filepath.ToSlash(relPath)

		// If directory, add it as a header
		if info.IsDir() {
			if relPath != "." {
				header := newZipHeader(info, normalisedRelPath+"/", zip.Store)

				_, err = zipWriter.CreateHeader(header)
				if err != nil {
					return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("failed to create directory in zip: %w", err))
				}
			}
			return nil
		}

		// Create a zip header for the file
		header := newZipHeader(info, normalisedRelPath, zip.Deflate)

		zipFileHeader, err := zipWriter.CreateHeader(header)
		if err != nil {
			return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("failed to create file in zip: %w", err))
		}

		// Open file
		file, err := os.Open(path) //nolint:gosec // path comes from filepath.Walk over the selected run directory.
		if err != nil {
			return message.New(message.EngineRunReadRunFile).WithCause(err).WithMetadata(map[string]string{"filePath": path})
		}

		// Copy contents
		_, err = util.CopyWithContext(ctx, zipFileHeader, file)
		if err != nil {
			_ = file.Close()
			return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("failed to write file to zip: %w", err))
		}

		return file.Close()
	})

	if err != nil {
		return err
	}
	if err = zipWriter.Close(); err != nil {
		return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("failed to close zip archive: %w", err))
	}
	if err = zipFile.Close(); err != nil {
		return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("failed to close zip file: %w", err))
	}
	return nil
}

func newZipHeader(info fs.FileInfo, name string, method uint16) *zip.FileHeader {
	header := &zip.FileHeader{
		Name:     name,
		Method:   method,
		Modified: info.ModTime(),
	}
	return header
}

// RunDescription represents a run description.
type RunDescription struct {
	ID                  string
	EngineVersion       string
	Name                string
	StartTime           string
	EndTime             string
	RecipeName          string
	Parameters          map[string]any
	WorkloadType        string
	Cmdline             string
	WorkingDir          string
	Env                 map[string]string
	UseShell            bool
	AndroidPackageName  string
	AndroidActivityName string
	Group               string
	Tags                []string
	Pid                 int64
	Target              target.Target
	TargetName          string
	Timeout             uint32
	RunResult           string
	RunError            string
	ToolsUsed           []cdf.ToolUsed
}

// RunDescription produces a run description in preparation for listing on the
// CLI.
func (c *RunCollection) RunDescription(ctx context.Context, run RunID) (*RunDescription, error) {
	unlock, err := c.RLockRun(ctx, run)
	if err != nil {
		return &RunDescription{}, err
	}
	defer func() { _ = unlock() }()

	// Check if run still exists (avoids TOCTOU)
	if !c.runExists(run) {
		return &RunDescription{}, message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": run.Value})
	}

	metadata, err := c.readMetadata(run)
	if err != nil {
		return &RunDescription{}, err
	}

	manifest, err := c.readManifest(run)
	if err != nil {
		return &RunDescription{}, err
	}

	categorization, err := c.readCategorization(run)
	if err != nil {
		return &RunDescription{}, err
	}

	tgt, err := target.EngineTargetFromJSON(metadata.TargetConfig)
	if err != nil {
		errMetadata := map[string]string{
			"runID": run.Value,
			"path":  c.getMetadataPath(run),
		}
		return nil, message.New(message.EngineRunParseTarget).WithMetadata(errMetadata).WithCause(err)
	}

	return &RunDescription{
		ID:                  run.Value,
		EngineVersion:       metadata.EngineVersion,
		Name:                metadata.Name,
		StartTime:           metadata.StartTime.ToFormattedString(),
		EndTime:             metadata.EndTime.ToFormattedString(),
		RecipeName:          recipemigration.GetMigratedName(run.Value, metadata.RecipeName, metadata.EngineVersion),
		Parameters:          metadata.Parameters,
		WorkloadType:        metadata.WorkloadType,
		Cmdline:             metadata.Cmdline,
		WorkingDir:          metadata.WorkingDir,
		Env:                 metadata.Env,
		UseShell:            metadata.UseShell,
		AndroidPackageName:  metadata.AndroidPackageName,
		AndroidActivityName: metadata.AndroidActivityName,
		Group:               categorization.Group,
		Tags:                categorization.Tags,
		Pid:                 metadata.Pid,
		Target:              tgt,
		TargetName:          metadata.TargetName,
		Timeout:             metadata.Timeout,
		RunResult:           metadata.RunResult,
		RunError:            metadata.RunError,
		ToolsUsed:           manifest.ToolsUsed,
	}, nil
}

// RunDescriptionsForExport returns sanitized run metadata suitable for inclusion in things like support packages.
func (c *RunCollection) RunDescriptionsForExport(ctx context.Context) ([]map[string]any, error) {
	ids, err := c.ListRuns(ctx)
	if err != nil {
		return nil, err
	}

	type exportRun struct {
		startTime string
		id        string
		payload   map[string]any
	}

	summaries := make([]exportRun, 0, len(ids))
	for _, id := range ids {
		desc, err := c.RunDescription(ctx, id)
		if err != nil {
			if msg := message.IsMessage(err); msg != nil && msg.Code() == message.EngineRunDoesNotExist {
				continue
			}
			return nil, err
		}

		params := make(map[string]any, len(desc.Parameters))
		for k, v := range desc.Parameters {
			params[k] = v
		}

		var targetJSON any
		if desc.Target != nil {
			jsonTarget, convErr := target.JSONTargetFromEngine(desc.Target)
			if convErr != nil {
				return nil, convErr
			}
			targetJSON = jsonTarget
		}

		env := make(map[string]string, len(desc.Env))
		for k, v := range desc.Env {
			env[k] = v
		}

		toolsUsed := make([]cdf.ToolUsed, len(desc.ToolsUsed))
		copy(toolsUsed, desc.ToolsUsed)

		tags := make([]string, len(desc.Tags))
		copy(tags, desc.Tags)

		payload := map[string]any{
			"id":             desc.ID,
			"engine_version": desc.EngineVersion,
			"name":           desc.Name,
			"start_time":     desc.StartTime,
			"end_time":       desc.EndTime,
			"recipe_name":    desc.RecipeName,
			"parameters":     params,
			"workload_type":  desc.WorkloadType,
			"cmdline":        desc.Cmdline,
			"pid":            desc.Pid,
			"target":         targetJSON,
			"target_name":    desc.TargetName,
			"timeout":        desc.Timeout,
			"run_result":     desc.RunResult,
			"run_error":      desc.RunError,
			"group":          desc.Group,
			"tags":           tags,
			"tools_used":     toolsUsed,
		}
		if desc.WorkloadType == "Android Launch" {
			payload["android_launch_workload"] = map[string]string{
				"package_name":  desc.AndroidPackageName,
				"activity_name": desc.AndroidActivityName,
			}
		}

		if desc.WorkingDir != "" {
			payload["working_dir"] = desc.WorkingDir
		}
		if len(env) > 0 {
			payload["env"] = env
		}

		summaries = append(summaries, exportRun{
			startTime: desc.StartTime,
			id:        desc.ID,
			payload:   payload,
		})
	}

	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].startTime == summaries[j].startTime {
			return summaries[i].id > summaries[j].id
		}
		return summaries[i].startTime > summaries[j].startTime
	})

	result := make([]map[string]any, 0, len(summaries))
	for _, summary := range summaries {
		result = append(result, summary.payload)
	}

	return result, nil
}

// CheckRunsUseSameRecipe takes a list of run IDs and checks whether all runs are using the same recipe.
func (c *RunCollection) CheckRunsUseSameRecipe(runIDs []RunID) error {
	if len(runIDs) == 0 {
		return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("no runs provided"))
	}

	var referenceName string
	var referenceNameID string

	var referenceSchema string
	var referenceSchemaID string

	for _, runID := range runIDs {
		recipeName, err := c.GetRecipeName(runID)
		if err != nil {
			return err
		}
		if referenceNameID == "" {
			referenceName = recipeName
			referenceNameID = runID.Value
		} else if recipeName != referenceName {
			metadata := map[string]string{
				"runID1":  referenceNameID,
				"runID2":  runID.Value,
				"recipe1": referenceName,
				"recipe2": recipeName,
			}
			return message.New(message.EngineRunDifferentRecipes).WithMetadata(metadata)
		}

		schema, err := c.GetRecipeSchema(runID)
		if err != nil {
			// If the schema is missing (e.g., old run), skip comparison
			continue
		}

		if referenceSchemaID == "" {
			referenceSchema = schema
			referenceSchemaID = runID.Value
		} else if schema != referenceSchema {
			metadata := map[string]string{
				"runID1":  referenceSchemaID,
				"runID2":  runID.Value,
				"recipe":  referenceName,
				"schema1": referenceSchema,
				"schema2": schema,
			}
			return message.New(message.EngineRunDifferentRecipeSchemas).WithMetadata(metadata)
		}
	}

	return nil
}

// CheckRunsUseSameRecipeNameOnly takes a list of run IDs and checks whether all runs are using the same recipe using
// recipe name only.
func (c *RunCollection) CheckRunsUseSameRecipeNameOnly(runIDs []RunID) error {
	if len(runIDs) == 0 {
		return message.New(message.CommonUnknownError).WithCause(fmt.Errorf("no runs provided"))
	}

	var referenceName string
	var referenceID string

	for _, runID := range runIDs {
		recipeName, err := c.GetRecipeName(runID)
		if err != nil {
			return err
		}
		if referenceName == "" {
			referenceName = recipeName
			referenceID = runID.Value
		} else if recipeName != referenceName {
			metadata := map[string]string{
				"runID1":  referenceID,
				"runID2":  runID.Value,
				"recipe1": referenceName,
				"recipe2": recipeName,
			}
			return message.New(message.EngineRunDifferentRecipes).WithMetadata(metadata)
		}
	}

	return nil
}

// GetRecipeName takes a runID and reads the recipe name from its description.
func (c *RunCollection) GetRecipeName(runID RunID) (string, error) {
	desc, err := c.RunDescription(context.Background(), runID)
	if err != nil {
		return "", err
	}
	return desc.RecipeName, nil
}

// GetRecipeComponentPath takes a runID and returns the absolute component path
func (c *RunCollection) GetRecipeComponentPath(runID RunID) (string, error) {
	component, err := c.LoadRecipeComponent(runID)
	if err != nil {
		return "", err
	}
	return component.AbsolutePath, nil
}

// GetNewestRun takes a list of run IDs and returns the newest one, based on the start time
// recorded in the run description.
func (c *RunCollection) GetNewestRun(runIDs []RunID) (RunID, error) {
	if len(runIDs) == 0 {
		return RunID{}, message.New(message.CommonUnknownError).WithCause(fmt.Errorf("no runs provided"))
	}

	var latestStartTime string
	var newestRunID = runIDs[0]

	for _, runID := range runIDs {
		desc, err := c.RunDescription(context.Background(), runID)
		if err != nil {
			return RunID{}, err
		}
		if desc.StartTime >= latestStartTime {
			latestStartTime = desc.StartTime
			newestRunID = runID
		}
	}
	return newestRunID, nil
}

// GetRecipeSchema takes a runID and returns the schema version for the recipe component
func (c *RunCollection) GetRecipeSchema(runID RunID) (string, error) {
	component, err := c.LoadRecipeComponent(runID)
	if err != nil {
		return "", err
	}
	return component.Type.SchemaVersion, nil
}

// LoadRecipeComponent takes a runID and returns the recipe cdf component
func (c *RunCollection) LoadRecipeComponent(runID RunID) (*cdf.Component, error) {
	model, err := c.LoadRun(runID)
	if err != nil {
		return nil, err
	}
	component, err := model.ResolveComponent(RecipeSourceRelativePath)
	if err != nil {
		return nil, message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runID.Value})
	}
	return &component, nil
}

// EnsureRunsExist is a helper function to check that the supplied run IDs exist in the specified run collection.
// Returns an error if any of the runs do not exist.
func (c *RunCollection) EnsureRunsExist(runIDs []RunID) error {
	for _, runID := range runIDs {
		if exists := c.runExists(runID); !exists {
			return message.New(message.EngineRunDoesNotExist).WithMetadata(map[string]string{"runID": runID.Value})
		}
	}
	return nil
}
