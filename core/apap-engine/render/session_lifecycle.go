// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/Arm-Debug/apap-cli/apap-engine/cdf"
	"github.com/Arm-Debug/apap-cli/apap-engine/cdf/semver"
	"github.com/Arm-Debug/apap-cli/apap-engine/logging/logx"
	"github.com/Arm-Debug/apap-cli/apap-engine/run"
	"github.com/Arm-Debug/apap-cli/apap-engine/targetsession"
	"github.com/Arm-Debug/apap-cli/apap-engine/tool"
	"github.com/Arm-Debug/apap-cli/apap-engine/util"
)

// toolRegistryProvider is the minimal interface we need from packages.PackageManager
type ToolRegistryProvider interface {
	FindToolIntegrations() (*tool.Registry, error)
}

// RunRenderFSProvider provides rerender filesystem builders for runs.
type RunRenderFSProvider interface {
	NewRunRenderFS(runID run.RunID) (*run.RunRenderFS, error)
}

// LoadContent loads content for each run, discovers *all* recorded ToolsUsed,
// and reverses any tool‐name renames and folder‐moves that happened *after*
// that run’s recorded version. We chain name‐migrations first (From→To), then
// apply any suffix remaps for the final tool names. Finally we register exactly
// those migrations on the cdf.OnDiskModel so ResolveComponent works.
//
// When rerender support is enabled, loadContent also creates a per-run rerender
// OnDiskModel and wraps the base model in an OverlayModel (overlay wins) so all
// resolve/list operations see rerender outputs transparently. It returns the
// ContentMap plus a SessionRenderFS built from the per-run builders/targets.
//
//  1. If no ToolsUsed, inject streamline-cli@1.0.0.
//  2. Gather all declared migrations once.
//  3. For each run:
//     a) For *each* recorded ToolUsed, walk name‐migrations (Version>runVer),
//     chaining From to To.
//     b) Then for each of those final tool names, collect suffix‐migrations
//     (Version>runVer).
//     c) Register the union of all those PathMigrations on the OnDiskModel.
//     d) Create a rerender ID + manifest, build a rerender OnDiskModel rooted
//     under rerender/<id>, and overlay it.
//  4. Return a ContentMap so each model can rewrite “tool/X/…” back to the
//     original run‐time paths, plus a SessionRenderFS for emitting outputs.
func LoadContent(
	ctx context.Context,
	loader run.RunLoader,
	contentSelection []run.RunID,
	packageManager ToolRegistryProvider,
) (*ContentMap, SessionRenderFS, error) {
	if len(contentSelection) == 0 {
		return nil, nil, errors.New("empty content selection")
	}
	rerenderFSProvider, ok := loader.(RunRenderFSProvider)
	if !ok {
		return nil, nil, fmt.Errorf("starting session failed: run loader does not support rerender")
	}

	// 1) Gather every declared migration exactly once.
	var allMigs []cdf.PathMigration
	if packageManager != nil {
		if reg, err := packageManager.FindToolIntegrations(); err == nil {
			for _, fac := range reg.Factories() {
				for _, m := range fac.GetMigrations() {
					v, perr := semver.ParseSemVer(m.Version)
					if perr != nil {
						continue
					}
					switch m.Type {
					case "missingInvocation":
						allMigs = append(allMigs, &cdf.ToolInvocationMigration{
							Type: m.Type,
							From: m.From,
							Ver:  v,
						})
					case "suffixRewrite":
						allMigs = append(allMigs, &cdf.ToolPathSuffixMigration{
							Type:      m.Type,
							From:      m.From,
							Ver:       v,
							OldSuffix: m.OldSuffix,
							NewSuffix: m.NewSuffix,
						})

					case "renameTool":
						allMigs = append(allMigs, &cdf.ToolNameMigration{
							Type: m.Type,
							From: m.From,
							To:   m.To,
							Ver:  v,
						})

					default:
						// skip unknown types
					}
				}
			}
		} else {
			logx.FromContext(ctx).Warnf("skipping migrations: %v", err)
		}
	}

	cm := &ContentMap{Entries: make([]ContentMapEntry, len(contentSelection))}

	rerenderBuilders := make(map[run.RunID]*run.RunRenderFS, len(contentSelection))
	rerenderTargets := make(map[run.RunID]*RenderTarget, len(contentSelection))

	// Sort all migrations oldest to newest
	sorted := append([]cdf.PathMigration(nil), allMigs...)
	sort.Slice(sorted, func(i, j int) bool {
		return semver.Cmp(sorted[i].Version(), sorted[j].Version()) < 0
	})

	// 2) Process each selected run.
	for i, rid := range contentSelection {
		logx.FromContext(ctx).WithField("run", rid).Info("Loading run")
		if cm.Contains(rid) {
			logx.FromContext(ctx).Warnf("Content selection contains duplicate run %q", rid.Value)
		}

		model, err := loader.LoadRun(rid)
		if err != nil {
			return cm, nil, err
		}

		runRoot, err := filepath.Abs(model.BasePath())
		if err != nil {
			return cm, nil, fmt.Errorf("failed to resolve external access root for run %q: %w", rid.Value, err)
		}

		// Legacy default for ancient runs: inject every tool folder we find.
		if len(model.Manifest().ToolsUsed) == 0 {
			model.InjectLegacyToolOutputs("1.0.0")
		}

		var runMigs []cdf.PathMigration

		// 3) For each recorded tool, pick every migration Version > runVer,
		//    then sort them oldest to newest and register them in that order.
		for _, tu := range model.Manifest().ToolsUsed {
			runVer, _ := semver.ParseSemVer(tu.Version)

			// “cur” tracks the folder name as we apply renames
			cur := tu.Tool

			// Walk through every migration newer than this run’s version
			for _, pm := range sorted {
				if semver.Cmp(runVer, pm.Version()) >= 0 {
					continue
				}
				switch m := pm.(type) {
				case *cdf.ToolNameMigration:
					// If this rename applies to our current tool folder...
					if m.From == cur {
						runMigs = append(runMigs, m)
						cur = m.To
					}
				case *cdf.ToolInvocationMigration:
					// apply to whichever tool folder we currently have
					if m.From == cur {
						runMigs = append(runMigs, m)
					}
				case *cdf.ToolPathSuffixMigration:
					// If this layout change applies under our current tool folder...
					if m.From == cur {
						runMigs = append(runMigs, m)
					}
				}
			}
		}

		// 4) Register *all* discovered migrations on the model.

		model.AddPathMigrations(runMigs)

		var view cdf.ModelView
		// Build a per-run render model and overlay it on the base view.
		builder, err := rerenderFSProvider.NewRunRenderFS(rid)
		if err != nil {
			return cm, nil, err
		}

		rerenderID, err := builder.GenerateNewRenderID()
		if err != nil {
			return cm, nil, err
		}

		rerenderManifest := &cdf.Manifest{}
		rerenderPath := filepath.Join(model.BasePath(), run.RenderPath(rerenderID))
		rerenderModel := cdf.NewOnDiskModel(rerenderPath, rerenderManifest, model.Metadata())
		view, err = cdf.NewOverlayModel(model, rerenderModel)
		if err != nil {
			return cm, nil, err
		}

		rerenderBuilders[rid] = builder
		rerenderTargets[rid] = NewRenderTarget(rerenderID, rerenderModel)

		cm.Entries[i] = ContentMapEntry{
			ID:                  rid,
			Model:               view,
			ExternalAccessRoots: []string{runRoot},
		}
	}

	// Build a session-scoped rerender helper once all per-run builders/targets are collected.
	var rerender SessionRenderFS
	if rerenderFSProvider != nil {
		rerender = NewSessionRenderFS(context.Background(), rerenderBuilders, rerenderTargets)
	}

	return cm, rerender, nil
}

type RendererConfig struct {
	Name       string
	ID         *string
	ConfigJSON string
}

func (r *RendererConfig) GetDisplayID() string {
	if r.ID == nil {
		return "undefined"
	}
	return *r.ID
}

type WidgetConfig struct {
	ID         *string
	ConfigJSON string
}

type RendererConfigList = []RendererConfig
type WidgetConfigList = []WidgetConfig // JSON strings

var ValidIDRegex = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func validateRendererID(id *string) error {
	if id == nil {
		return nil
	}
	if *id == "" {
		return errors.New("renderer ID cannot be empty if provided")
	}
	if !ValidIDRegex.MatchString(*id) {
		return fmt.Errorf("invalid renderer ID '%q': must start with a letter and contain only letters, numbers, underscores, or hyphens", *id)
	}
	return nil
}

func validateRendererConfigList(configs RendererConfigList) error {
	seen := make(map[string]struct{})

	for _, config := range configs {
		if err := validateRendererID(config.ID); err != nil {
			return err
		}
		if config.ID != nil {
			if _, exists := seen[*config.ID]; exists {
				return fmt.Errorf("duplicate renderer ID '%q' found", *config.ID)
			}
			seen[*config.ID] = struct{}{}
		}
	}

	return nil
}

func createRenderer(ctx context.Context, factory RendererFactory, config RendererConfig, i int) (Renderer, error) {
	logger := logx.FromContext(ctx).WithFields(
		log.Fields{
			"renderer": config.Name,
		},
	)

	logger.Info("Creating renderer")
	renderer, err := factory.NewRenderer(config.Name)
	if err != nil {
		return renderer, fmt.Errorf("failed to construct renderer with name '%s': %w", config.Name, err)
	}

	logger.WithFields(log.Fields{"config": config.ConfigJSON}).Info("Configuring renderer")

	identity := RendererIdentity{Index: i, ID: config.ID, Name: config.Name}
	err = renderer.Configure(&Config{Identity: identity, JSON: config.ConfigJSON})
	if err != nil {
		return renderer, fmt.Errorf("failed to configure renderer '%s': %w", config.Name, err)
	}

	return renderer, nil
}

func parseDataSources(configs RendererConfigList) ([]map[string][]DataSource, []error) {
	dataSources := make([]map[string][]DataSource, len(configs))
	configErrs := make([]error, len(configs))
	for i, conf := range configs {
		ds, err := ParseDataSourcesFromConfig(conf.ConfigJSON)
		dataSources[i] = ds
		configErrs[i] = err
	}

	return dataSources, configErrs
}

func createDataSources(configs RendererConfigList) ([]map[string][]DataSource, []error, error) {
	if err := validateRendererConfigList(configs); err != nil {
		return nil, nil, err
	}

	// Parse data sources first - these are need to construct the renderer graph
	dataSources, configErrs := parseDataSources(configs)
	return dataSources, configErrs, nil
}

func createRenderers(ctx context.Context, factory RendererFactory, configs RendererConfigList, errs []error) (RendererList, []error) {
	renderers := make(RendererList, len(configs))
	creationErrs := make([]error, len(configs))

	for i, config := range configs {
		// Skip renderers which are already invalid
		if errs[i] != nil {
			continue
		}
		renderer, err := createRenderer(ctx, factory, config, i)
		creationErrs[i] = err
		renderers[i] = renderer
	}

	return renderers, creationErrs
}

func initializeRenderers(ctx context.Context, session Session, rendererSpec RendererSpec, renderers RendererList, errs []error) ([]error, error) {
	if len(renderers) == 0 {
		return nil, errors.New("no renderer specified")
	}

	defer func() {
		for _, renderer := range renderers {
			if renderer == nil {
				continue
			}
			if listener, ok := renderer.(InitializeCompletionListener); ok {
				listener.OnInitializeComplete(session)
			}
		}
	}()

	initializeErrs := make([]error, len(renderers))
	for i, renderer := range renderers {
		if renderer == nil {
			continue
		}

		if errs[i] != nil {
			logx.FromContext(ctx).WithFields(log.Fields{"renderer": rendererSpec.Configs[i].Name}).Info("Skipping renderer due to existing error")
			continue
		}

		var err error
		err = ValidatePortSpecs(renderer)
		if err != nil {
			initializeErrs[i] = err
			continue
		}

		logx.FromContext(ctx).WithFields(log.Fields{"renderer": rendererSpec.Configs[i].Name}).Info("Initializing renderer")

		var resolvedDataSources TableRefMap
		if ds := rendererSpec.DataSources[i]; ds != nil {
			resolvedDataSources, err = ResolveDataSources(session, ds, renderers)
			if err != nil {
				initializeErrs[i] = fmt.Errorf("failed to resolve data sources for renderer '%s': %w", rendererSpec.Configs[i].Name, err)
				continue
			}
			err = ValidateDataSources(session.Manifest(), resolvedDataSources, renderer.GetInputSpec())
			if err != nil {
				initializeErrs[i] = fmt.Errorf("data sources do not match input spec for renderer '%s': %w", rendererSpec.Configs[i].Name, err)
				continue
			}
			if resolvedDataSources.IsPending() {
				initializeErrs[i] = cdf.ErrComponentPending
				emitPendingRendererOutputSpec(session, renderer.GetOutputSpec(), RendererIdentity{
					Index: i,
					ID:    rendererSpec.Configs[i].ID,
					Name:  rendererSpec.Configs[i].Name,
				})
				logx.FromContext(ctx).WithFields(log.Fields{"renderer": rendererSpec.Configs[i].Name}).Info("Renderer input data source is pending; producing pending outputs and skipping")
				continue
			}
		}

		err = renderer.Initialize(session, resolvedDataSources)
		errRemoval := RemoveTempTables(session)
		if errors.Is(err, cdf.ErrComponentPending) {
			initializeErrs[i] = err
			emitPendingRendererOutputSpec(session, renderer.GetOutputSpec(), RendererIdentity{
				Index: i,
				ID:    rendererSpec.Configs[i].ID,
				Name:  rendererSpec.Configs[i].Name,
			})
			logx.FromContext(ctx).WithFields(log.Fields{"renderer": rendererSpec.Configs[i].Name}).Info("Renderer resolved pending component; producing pending outputs")
			continue
		} else if err != nil {
			initializeErrs[i] = fmt.Errorf("failed to initialize renderer '%s': %w", rendererSpec.Configs[i].Name, err)
			continue
		}

		if errRemoval != nil {
			initializeErrs[i] = fmt.Errorf("failed to cleanup temp tables for renderer '%s': %w", rendererSpec.Configs[i].Name, errRemoval)
			continue
		}

		identity := RendererIdentity{Index: i, ID: rendererSpec.Configs[i].ID, Name: rendererSpec.Configs[i].Name}
		err = ValidateRendererOutput(session, renderer, identity)
		if err != nil {
			initializeErrs[i] = fmt.Errorf("renderer '%s' output spec validation failed: %w", rendererSpec.Configs[i].Name, err)
		}
	}

	logx.FromContext(ctx).Debug("Checkpointing duckdb after render init")
	if err := CheckpointDatabase(ctx, session.Database()); err != nil {
		logx.FromContext(ctx).WithError(err).Warn("failed to checkpoint database after renderer initialization")
	}

	return initializeErrs, nil
}

// RemoveTempTables drops all temp tables currently tracked by the session manifest.
//
// Temp tables are treated as disposable staging tables during render initialization.
func RemoveTempTables(session Session) error {
	tempTableNames := session.Manifest().TempTableNames()
	for _, tableName := range tempTableNames {
		_, err := session.Database().Conn.ExecContext(
			context.Background(),
			fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, tableName),
		)
		if err != nil {
			return fmt.Errorf("failed to drop temp table '%s': %w", tableName, err)
		}

		if err := session.Manifest().RemoveEntry(tableName); err != nil {
			return fmt.Errorf("failed to remove temp table '%s' from manifest: %w", tableName, err)
		}
	}

	return nil
}

// ValidateRendererOutput checks that all manifest entries produced by the given renderer
// conform to the renderer's declared output specification.
// If the renderer does not declare any outputs, no validation is performed.
// The supplied identity must match the renderer's configured identity for its manifest entries.
func ValidateRendererOutput(session Session, renderer Renderer, identity RendererIdentity) error {
	manifest := session.Manifest()
	if manifest == nil {
		return errors.New("manifest is nil")
	}

	outSpec := renderer.GetOutputSpec()
	if len(outSpec.Ports) == 0 {
		// Nothing declared, assume legacy renderer and skip validation.
		return nil
	}

	// NOTE: Cardinality is currently ignored. We only ensure produced component types are declared.
	expected := make(map[cdf.ComponentType]struct{}, len(outSpec.Ports))
	for _, port := range outSpec.Ports {
		key := port.ComponentType
		if _, exists := expected[key]; exists {
			return fmt.Errorf("duplicate output component type '%s:%s' in specification", port.ComponentType.Name, port.ComponentType.SchemaVersion)
		}
		expected[key] = struct{}{}
	}

	var errs []string
	found := make(map[cdf.ComponentType]bool, len(expected))

	// Walk manifest entries to flag rogue outputs and track which ports were satisfied.
	for _, entry := range manifest.Entries() {
		if entry.IsHidden() {
			continue
		}

		info := entry.Info()
		component := info.ComponentType()

		if info.RendererIdentity().Equals(identity) {
			if _, ok := expected[component]; !ok {
				errs = append(errs,
					fmt.Sprintf("table '%s' not declared in output spec", entry.TableName()),
				)
				continue
			}

			found[component] = true
			continue
		}
	}

	// Ensure every declared port has a corresponding table from this renderer.
	for _, port := range outSpec.Ports {
		if found[port.ComponentType] {
			continue
		}
		msg := fmt.Sprintf("no table produced for declared output '%s' (%s:%s)",
			port.Name, port.ComponentType.Name, port.ComponentType.SchemaVersion)
		errs = append(errs, msg)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	// TODO Check cardinality and change check as appropriate.

	return nil
}

// StartRenderSession creates and initializes a new render session, loading the specified content and renderers.
// The session is registered in the provided session storage. Multiple sessions can exist concurrently, each with
// its own fixed content and set of renderers for its lifetime. This function does not automatically close or
// replace any existing sessions in storage.
// - sessionFactory constructs the Session object.
// - sessionStorage manages active sessions.
// - rendererFactory creates Renderer instances by name.
// - loader loads the CDF model content from disk.
// - contentSelection specifies which runs to load into the session.
// - rendererConfigs lists the renderers to instantiate.
// - visConfigs lists the visualizations (and other widgets) to configure.
// - databaseFactory provides a database connection for the session.
// Returns the new session (if at least one renderer succeeded), a list of per-renderer errors, or an error
func StartRenderSession(
	ctx context.Context,
	sessionFactory SessionFactory,
	sessionStorage *SessionStorage,
	rendererFactory RendererFactory,
	loader run.RunLoader,
	contentSelection []run.RunID,
	rendererConfigs RendererConfigList,
	visConfigs WidgetConfigList,
	databaseFactory DatabaseFactory,
	pkgProvider ToolRegistryProvider,
	targetSessions targetsession.TargetSessionProvider,
) (Session, []error, error) {

	if sessionStorage == nil {
		return nil, nil, fmt.Errorf("session storage missing")
	}

	logx.FromContext(ctx).Debug("Starting render session")

	sc := util.ScopeCleaner{}

	content, rerender, err := LoadContent(ctx, loader, contentSelection, pkgProvider)
	if err != nil {
		return nil, nil, fmt.Errorf("starting session failed: failed to load content for new render: %w", err)
	}

	dataSources, configErrs, err := createDataSources(rendererConfigs)
	if err != nil {
		return nil, nil, fmt.Errorf("creating data sources failed: %w", err)
	}
	rendererSpec, graphErrs, err := NewRendererSpec(rendererConfigs, dataSources)
	if err != nil {
		return nil, nil, fmt.Errorf("creating renderer graph failed: %w", err)
	}
	mergedErrs := merge(configErrs, graphErrs)

	renderers, creationErrs := createRenderers(ctx, rendererFactory, rendererSpec.Configs, mergedErrs)
	mergedErrs = merge(mergedErrs, creationErrs)

	session, err := sessionFactory.NewSession(content, renderers, databaseFactory, rerender, targetSessions)
	if err != nil {
		return nil, nil, fmt.Errorf("starting session failed: %w", err)
	}
	defer sc.MaybeCleanup(func() { session.Close() })

	initializeErrs, err := initializeRenderers(ctx, session, rendererSpec, renderers, mergedErrs)
	if err != nil {
		return nil, nil, fmt.Errorf("starting session failed: %w", err)
	}
	finalErrs := merge(mergedErrs, initializeErrs)

	visDataSourcesMap, err := ParseWidgetDataSources(visConfigs)
	if err != nil {
		return nil, nil, err
	}

	err = ResolveWidgetDataSources(session, renderers, visDataSourcesMap)
	if err != nil {
		return nil, nil, err
	}

	if addError := sessionStorage.AddRenderSession(session); addError != nil {
		return nil, nil, fmt.Errorf("render session add failed: %w", addError)
	}

	if util.Contains(finalErrs, nil) {
		// Retain the session only if one or more renderers succeeded
		sc.CancelCleanup()
		return session, finalErrs, nil
	} else {
		return nil, finalErrs, nil
	}
}
