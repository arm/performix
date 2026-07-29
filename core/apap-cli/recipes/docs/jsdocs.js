// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// This file provides type definitions for the available APIs exposed to recipes.

/**
 * Metadata exposed as `globalThis['performix']` to top-level recipe declarations
 * and tool integrations.
 * @typedef {Object} PerformixGlobal
 * @property {string} engineVersion The current Performix engine version.
 */

/**
 * Recipe declarations and tool integrations can read this runtime-provided
 * object as `globalThis['performix']`. In `@ts-check` files, cast it to
 * `PerformixGlobal` before reading its fields.
 */

/**
 * Shared utilities exposed to top-level recipe declarations.
 * @typedef {Object} RecipeUtils
 * @property {function(RecipeReadyAdvice[]): string} toolStatusToRecipeStatus
 * Converts recipe ready advice to a single recipe ready status.
 * @property {function(ToolConfigurationsArg, ProbeResult[]): RecipeReadyAdvice[]} collectToolAdvice
 * Normalizes advice returned by tool integration probes.
 */

/**
 * @typedef {Object} Workload
 * @property {string} Type
 * @property {any} Data
 */

/**
 * @typedef {Object} Tool
 * @property {string} name
 * @property {string[]} args
 */

/**
 * @typedef {Object} ToolConfiguration
 * @property {string} name                      The name of the tool to invoke. This should match the name defined in the tool integration.
 * @property {string} version                   The version of the tool to invoke. This should match the version defined in the tool integration.
 * @property {Object.<string, any>} params      A key-value mapping of parameters to pass to the tool when it's invoked.
 * @property {Workload} workload                The workload to invoke the tool on.
 * @property {Object.<string, string>} env      Environment variables for the tool invocation.
 * @description - Defines the instance specific data for a tool run
 */

/**
 * @typedef {Object} ToolConfigurationsArg
 * @property {ToolConfiguration[]} toolConfigs  A list of tool configurations to run.
 * @description - Defines configuration data for a list of tools to run
 */

/**
 * @typedef {Object} ToolProperties
 * @property {ToolDeployment} Deployment - An object describing the deployment of the tool on the target
 * @description An object containing properties describing a tool
 */

/**
 * @typedef {Object} ToolDeployment
 * @property {string} Path - The path to the tool on the target if deployed, or "" otherwise
 * @description An object describing the deployment of the tool on the target
 */

/**
 * @typedef {Object} OsInfo
 * @property {string} OSFamily
 * @property {string} OSDescription
 * @property {string} KernelVersion
 */

/**
 * @typedef {Object} ClusterDescription
 * @property {number} ClusterID
 * @property {string} Name
 */

/**
 * @typedef {Object} CPUDescription
 * @property {number} CoreNumber
 * @property {number} ClusterID
 * @property {string} Midr
 * @property {string} Name
 */

/**
 * @typedef {Object} TargetInfoDescription
 * @property {OsInfo} Os
 * @property {string} PrimaryCPUName
 * @property {CPUDescription[]} CPUs
 * @property {ClusterDescription[]} ClusterInfo
 */

/**
 * @typedef {Object} RunDescription
 * @property {Object.<string, string>} Parameters
 * @property {string[]} ToolsUsed
 * @property {bool} IsRunInProgress
 * @property {bool} IsRunPhaseTwoComplete
 * Describes general metadata associated with a run.
 *
 * **Properties**
 *   - parameters: A key-value mapping of parameters used in the run.
 */

/**
 * @typedef {Object} ComponentType
 * @property {string} name
 * @property {string} version
 * @description
 * Describes the classification of a file being transferred. After the files are transfered into the
 * run directory, they are referenced exclusively through their component types for further processing.
 *
 * **Properties**
 *   - name: Defines the processing rules for the file. It can either be an existing type
 *           (e.g. "log-text"), or a custom-defined one.

 *   - version: Specifies the format version of the component type. If you're using an existing component
 *           type but the file format has changed, increment the version to reflect the difference.
 */

/**
 * @typedef {Object} FileArg
 * @property {string} targetPath
 * @property {string} destRelativePath
 * @property {ComponentType} componentType
 * @property {TransferOptions} [transferOptions]
 * @description
 * Defines the parameters for transferring a file artifact from the target machine to the host.
 *
 * **Properties**
 *   - targetPath: Specifies the location of the source file on the target machine. This property accepts
 *            either an absolute path or a relative path. If a relative path is provided, it is interpreted
 *            relative to the target's temporary working directory.
 *
 *   - destRelativePath: The relative destination path on the host machine, under the run directory.
 *            Can include nested folders, which will be created during transfer.
 *
 *   - componentType: An object that classifies the file for later processing.
 *
 *   - transferOptions: Configuration options for how this transfer should be executed
 */

/**
 * @typedef {Object} PythonExec
 * @property {string} type - Must be "python" to indicate the argument set is intended for running a Python script.
 * @property {string} [venv] - Optional path to the virtual environment, if applicable.
 *     The virtual environment is used to set up the PATH, ensuring that the appropriate Python interpreter
 *     and dependencies are used. If the venv does not exist, it is automatically created. The venv is destroyed after
 *     the recipe execution completes.
 *
 *     The venv path can be provided as relative path (relative to the temporary working directory on the target machine),
 *     or as an absolute path).
 * @property {boolean} [runAsAdmin] - Optionally specify whether the command should be run as Administrator. For this to work,
 *      the target user account must be able to elevate to Administrator priviliges without password prompt.
 * @property {string} cmd - The command to execute on the target machine.
 * @description Defines the parameters required to run a python script on the target machine.
 */

/**
 * @typedef {Object} Exec
 * @property {string} type - Must be "exec" to indicate that the argument set is intended for executing a command.
 * @property {boolean} [runAsAdmin] - Optionally specify whether the command should be run as Administrator. For this to work,
 *      the target user account must be able to elevate to Administrator priviliges without password prompt.
 * @property {string} cmd - The command to execute on the target machine.
 * @description - Defines the parameters required to execute a command on the target machine.
 */

/**
 * @typedef {Object} RunCommandOutput
 * @property {number} ReturnCode
 * @property {string} Stdout
 * @property {string} Stderr
 * @description - Defines the output structure for the runCommand API.
 */

/**
 * @typedef {Object} RunExecutionContext
 * @property {string} engineVersion
 * @property {function(): Workload} getWorkload
 * @property {function(string): string | string[]} getParameter
 * @property {function(string, string): ToolProperties} getTool
 * @property {function(ToolConfigurationsArg): void} runTools
 * If a ToolConfigurationsArg is provided, the tools will be run concurrently. After the tools finish,
 * their output files will be reformatted concurrently. The running and reformating is defined by the tool integration.
 * Each supplied tool must have a corresponding tool integration. The tool must be deployed on the target before it can be run.
 * @property {function(): TargetInfoDescription} targetInfo
 * @property {function(string): void} logWarn
 * @property {function(string): void} logInfo
 * @property {function(("info"|"warn"|"error"), string): void} writeUserMessage
 * Records a message to display to the user.
 * @property {function(string): string} readHostFile
 * @property {function(string): (string|undefined)} getTelemetrySpecification
 * Returns the telemetry specification JSON for a supported CPU model name or undefined when unsupported.
 * @property {function(FileArg): void} retrieveFile
 * @property {function(PythonExec|Exec): RunCommandOutput} runCommand
 * @property {function(): boolean} isFullCaptureSupportEnabled
 * Returns whether full capture support is enabled for the current run.
 * @description - The run execution context provides access to APIs available to the run stages.
 */

/**
 * @typedef {Object} ReadyExecutionContext
 * @property {string} engineVersion
 * @property {function(ToolConfigurationsArg): (RecipeReadyOutput|ProbeResult[])} probeTools
 * The response is either a single RecipeReadyOutput (sl-collect probe) or an array of ProbeResult (tool integration probe).
 * This function verifies that the recipe is ready to run on the target machine.
 * Issues that may limit effectiveness of the recipe or prevent it completely are returned as advice entries
 * @property {function(): Workload} getWorkload
 * @property {function(string): string} getParameter
 * @property {function(string, string): ToolProperties} getTool
 * @property {function(string): void} logWarn
 * @property {function(string): void} logInfo
 * @property {function(string): string} readHostFile
 * @property {function(string): (string|undefined)} getTelemetrySpecification
 * Returns the telemetry specification JSON for a supported CPU model name or undefined when unsupported.
 * @property {function(): TargetInfoDescription} targetInfo
 * @property {function(PythonExec|Exec): RunCommandOutput} runCommand
 * @property {function(): boolean} isFullCaptureSupportEnabled
 * Returns whether full capture support is enabled for the current run.
 * @description - The ready execution context provides access to APIs available to the ready stages.
 */

/**
 * @typedef {Object} RenderExecutionContext
 * @property {string} engineVersion
 * @property {function(): RunDescription[]} getRunDescriptions - Provides general metadata associated with the runs to render.
 * @property {function(number, string): RunComponentDescription[]} listRunComponents
 * Lists manifest components for an indexed run matching the given component glob.
 * @property {function(string): any} getRenderParameter - Retrieves a render parameter by ID.
 * @property {function(): Object.<string, any>} getRenderParameters - Retrieves all render parameters by ID.
 * @property {function(string): void} logWarn
 * @property {function(string): void} logInfo
 * @property {function(): boolean} isRerenderingEnabled
 * Returns whether re-rendering feature flag is enabled.
 * @property {function(): boolean} isNeoprofTimelineEnabled
 * Returns whether the neoprof timeline feature flag is enabled
 * @description - The render execution context provides access to APIs available to the render stages.
 */

/**
 * @typedef {Object} RunComponentDescription
 * @property {string} relativePath - Relative path to the component within the run directory.
 * @property {string} fileName - Base file name for the component.
 * @property {{name: string, version: string}} componentType - Component type metadata.
 */

/**
 * @typedef {Object} Parameter
 * @property {string} id - Unique identifier for the parameter.
 * @property {boolean} [required] - Whether the parameter is mandatory.
 * @property {string} label - Human-friendly label used in UIs.
 * @property {string} description - Explains what the parameter controls.
 * @property {SingleSelectConfig|MultiSelectConfig|RadioConfig|InputConfig|CheckboxConfig} config - Input config (tagged by its `type`).
 */

/**
 * @typedef {Object} RenderParameter
 * @property {string} id - Unique identifier for the render parameter.
 * @property {RenderParameterConfig} config - Render parameter config.
 */

/**
 * @typedef {Object} RenderParameterConfig
 * @property {"number"|"string"|["number"]|["string"]} type - The value type accepted by the render parameter.
 */

/**
 * @typedef {Object} BaseConfig
 * @property {"single_select"|"multi_select"|"radio"|"input"|"checkbox"} type - Discriminator for the config union.
 */

/**
 * @typedef {Object} SingleSelectConfig
 * @property {"single_select"} type
 * @property {ParameterOptions[]|function(ReadyExecutionContext): (ParameterOptions[])} options - List of detailed options with value, label, and optional description, either static or computed at runtime. Legacy recipes with `api_version: "1.0.0"` may also use string arrays.
 * @property {string} defaultValue - Default selected option.
 * @augments BaseConfig
 */

/**
 * @typedef {Object} MultiSelectConfig
 * @property {"multi_select"} type
 * @property {ParameterOptions[]|function(ReadyExecutionContext): (ParameterOptions[])} options - List of detailed options with value, label, and optional description, either static or computed at runtime. Legacy recipes with `api_version: "1.0.0"` may also use string arrays.
 * @property {string[]} [defaultValue] - Default selected options.
 * @augments BaseConfig
 */

/**
 * @typedef {Object} RadioConfig
 * @property {"radio"} type
 * @property {ParameterOptions[]|function(ReadyExecutionContext): (ParameterOptions[])} options - List of detailed options with value, label, and optional description, either static or computed at runtime. Legacy recipes with `api_version: "1.0.0"` may also use string arrays.
 * @property {string} defaultValue - Default selected option.
 * @augments BaseConfig
 */

/**
 * @typedef {Object} ParameterOptions
 * @property {string} value - The actual value of the option which will be passed to the recipe when set.
 * @property {string} label - Human-friendly label for the option, used in UIs.
 * @property {string} [description] - Optional description for the option, used in UIs.
 */

/**
 * @typedef {Object} InputConfig
 * @property {"input"} type
 * @property {string} [defaultValue] - Default input value.
 * @property {Object.<string, string>} [custom] - Custom key-value pairs.
 * @augments BaseConfig
 */

/**
 * @typedef {Object} CheckboxConfig
 * @property {"checkbox"} type
 * @property {boolean} defaultValue - Default checked state.
 * @augments BaseConfig
 */

/**
 * @typedef {Object} RunStage
 * @property {string} name
 * @property {string} description
 * @property {function(RunExecutionContext): void} exec
 */

/**
 * @typedef {Object} RecipeReadyAdvice
 * @property {string} ToolName
 * @property {string} AdviceSeverity
 * @property {string} MessageCode - the code of the catalog message to look up
 * @property {Object.<string, string>} Metadata - (optional) any metadata to attach to the Message
 * @property {string} Cause - (optional) a string to attach as a cause
 */

/**
 * @typedef {Object} RecipeReadyOutput
 * @property {string} Status
 * @property {RecipeReadyAdvice[]} Advice
 */

/**
 * @typedef {Object} Renderer
 * @property {string} type - a renderer type that is supported
 * @property {string} id - a unique ID that is used to reference the renderer
 * @property {string} [config] - optional config for the renderer
 * @description - Declares a renderer, with a type and unique ID.
 * A renderer is post-processing the output data of a recipe run, after it has been retrieved on the host
 * machine. The output of renderers may include tables, which are then referenced by a visualiser to format
 * the data in CLI or GUI.
 */

/**
 * @typedef {Object} Widget
 * @property {string} type - a visualization (or other widget) type that is supported
 * @property {string} id - a unique ID that is used to reference the widget
 * @property {string} rendererId - references an existing renderer ID
 * @property {string} title
 * @property {string} description
 * @property {string} [config] - optional config for the widget
 * @property {Object.<string,string>} [parameterBindings] - maps widget parameter IDs to recipe render parameter IDs
 * @property {WidgetDisabledState} [disabled] - optional object disabling this widget and providing a user-facing reason
 * @description - Declares a widget, typically a visualization, which references a renderer ID.
 * - The widget must support the renderer referenced.
 * - A renderer can support one or multiple widgets.
 */

/**
 * @typedef {Object} WidgetDisabledState
 * @property {string} reason - User-facing explanation for why the widget is disabled
 */

/**
 * @typedef {Object} RecipeRenderOutput
 * @property {Renderer[]} renderers
 * @property {Object.<string, Widget[]>} [ui]
 * @property {Widget[]} [visualizations]
 * @description - The output of a RenderStage, which includes a list of renderers and a
 * collection of recipe-controlled widgets (typically visualizations)
 * - the keys in `ui` represent placements in the application UI.
 * - Use either `ui` or `visualizations`, but not both.
 * - The `visualizations` field is treated as `ui.visualizations`.
 */

/**
 * @typedef {Object} RenderStage
 * @property {string} name
 * @property {string} description
 * @property {function(RenderExecutionContext): RecipeRenderOutput} exec
 * @description RenderStage returns the specifications for Renderers and Visualizations.
 * Multiple render stages are allowed, in which case the list of renderers and visualizations is
 * aggregated into one.
 */

/**
 * @typedef {Object} ReadyStage
 * @property {string} name
 * @property {string} description
 * @property {function(ReadyExecutionContext): RecipeReadyOutput} exec
 */

/**
 * @typedef {Object} ValidationError
 * @property {string} parameterId - The ID of the parameter that caused the validation error.
 * @property {string} value - The value provided for this parameter.
 * @property {string} messageCode - The code of the catalog message to look up.
 * @property {Object.<string, string>} metadata - (optional) Any metadata to attach to the Message.
 * @property {string} cause - (optional) A string to attach as a cause.
 */

/**
 * @typedef {Object} ValidationResult
 * @property {ValidationError[]} errors - A list of validation errors found during processing.
 * @description Defines the structure of a validation result returned by the validation function.
 */

/**
 * @typedef {"always"|"param_is_set"|"param_is_not_set"} RequiredWhen
 * @description
 * Indicates when the dependency is required.
 * - "always": Always required
 * - "param_is_set": Required when a specific parameter is set to a value. The parameter name and value(s) must be provided in an additional `parameters` field.
 * - "param_is_not_set": Required when a specific parameter is not set to a value. The parameter name and value(s) must be provided in an additional `parameters` field.
 */

/**
 * @typedef {Object} RequirementSpec
 * @property {RequiredWhen} type // When the dependency is required
 * @property {Object.<string, string>} [parameters] // Parameter values applicable to "param_is_set" and "param_is_not_set".
 * @description
 * Specifies when a dependency is required.
 */

/**
 * @typedef {Object} AppliesTo
 * @property {"aarch64"|"x86_64"} [architecture] // can be "aarch64", "x86_64"
 * @property {"Linux"|"Windows"|"Darwin"} [os] // can be "Linux", "Windows", "Darwin"
 * @description
 * Defines a platform filter for deployments. If multiple filters are provided, the deployment applies if any filter matches.
 */

/** @typedef {Object} ToolBundle
 * @property {string} name
 * @property {string} version
 * @description
 * Defines the tool bundle name and version for a dependency. This is used to identify tar balls that must be deployed in the required locality.
 * If --deploy-tools is specified when running a recipe, all supported and required tool bundle dependencies will be pushed to their required locality, ready to use in the recipe.
 * The version number is used to isolate tool installations in each deployment location.
 */

/**
 * @typedef {Object} Dependency
 * @property {"tool"|"tool_bundle"} type
 * @property {string} name
 * @property {string} version
 * @property {"target"|"host"} [locality="target"] // locality defines where a tool_bundle dependency is deployed. It is only valid for tool_bundle dependencies.
 * @property {RequirementSpec} requiredWhen
 * @description
 * Defines a dependency for a deployment. The intended use is that
 * recipes depend on tools and then tools depend on tool bundles.
 * This is what the current dependency hierachy looks like:
 * Recipe -> Tool -> Tool Bundle
 */

/**
 * @typedef {Object} Deployment
 * @property {AppliesTo[]} [appliesTo] - List of supported target platforms (e.g., {architecture: string, os: string})
 * @property {Dependency[]} [dependencies] - List of dependencies for this deployment
 * @description
 * Defines a deployment for a recipe, including supported target platform filters and dependencies.
 *
 */

/**
 * @typedef {Object} Recipe
 * @property {string} name - the unique name of the recipe
 * @property {string} title - a human-readable title for the recipe
 * @property {string} version - the version of the recipe
 * @property {string} api_version - the version of the recipe API that this recipe conforms to
 * @property {string} description - a human-readable description of the recipe
 * @property {string} [mcp_guidance] - guidance for MCP clients and coding agents. Keep this separate from
 * description when advice is useful for automation but not for GUI or CLI recipe summaries.
 * @property {"stable"|"preview"|"experimental"} [status] - indicates the recipe's user-facing maturity; defaults to "preview" when omitted.
 * Stable recipes are fully fledged recipes. Preview recipes are user-facing by default and should work end-to-end,
 * but may have significant updates in the near future. Experimental recipes are gated behind a feature flag, are not
 * user-facing by default, and may not be fully functional end-to-end in their current form.
 * @property {Deployment[]} [deployments] - defines deployments required by the recipe
 * @property {Parameter[]} parameters - defines the parameters that can be passed to the recipe
 * @property {RenderParameter[]} [renderParameters] - defines parameters used during rendering and re-rendering
 * @property {RunStage[]} runStages - defines the stages that are executed when the recipe is run
 * @property {ReadyStage[]} readyStages - defines the stages that are executed to verify that the recipe can run on the target
 * @property {RenderStage[]} renderStages - defines the renderers and visualizations available to the recipe
 * @property {undefined|function(ReadyExecutionContext): ValidationResult} [validationFunction] - optional function to validate recipe parameters.
 * This can be called explicitly using the validate API. It will also be called as part of a recipe run.

 */

/**
 * CPU affinity labels (agent-specific). Semantics are engine-defined.
 * @typedef {string} AffinityLabel
 */

/**
 * Redirect mode for process stdio.
 * - "none": discard
 * - "file": write to file only (`path` is required)
 * - "stream": expose as async iterator only
 * - "both": write to file **and** expose as async iterator
 * @typedef {"none"|"file"|"stream"|"both"} RedirectMode
 */

/**
 * Redirect configuration for a single stream.
 * @typedef {Object} StreamRedirect
 * @property {RedirectMode} redirect
 * @property {string} [path]             Path to file if mode is "file" or "both".
 */

/**
 * Options for command launch
 * @typedef {Object} ExecOptions
 * @property {boolean} [asPrivileged=false]           Run with elevated privileges. The engine performs privilege elevation on demand.
 * @property {AffinityLabel[]} [affinity]       CPU affinity for the workload.
 * @property {string} [workingDirectory]        Process working directory.
 * @property {Object.<string,string>} [environment] Environment variables (key/value).
 */

/**
 * Options for a long-running process (engine.startProcess).
 * @typedef {Object} ProcessOptions
 * @property {boolean} [asPrivileged=false]           Run with elevated privileges. The engine performs privilege elevation on demand.
 * @property {AffinityLabel[]} [affinity]
 * @property {string} [workingDirectory]
 * @property {Object.<string,string>} [environment]
 * @property {boolean} [stdinOpen=false]        If true, stdin is writable via handle.writeStdin().
 * @property {StreamRedirect} [stdout]          Redirect policy for stdout.
 * @property {StreamRedirect} [stderr]          Redirect policy for stderr.
 */

/**
 * Result of a completed command.
 * @typedef {Object} CommandResult
 * @property {number} rc         Exit code (0 = success).
 * @property {string} stdout     Entire stdout as a string.
 * @property {string} stderr     Entire stderr as a string.
 */

/**
 * Metadata when emitting an output artifact from a tool.
 * @typedef {Object} OutputMetadata
 * @property {string} componentType
 * @property {string} version
 */

/**
 * Transfer options when emitting an output artifact from a tool.
 * @typedef {Object} TransferOptions
 * @property {boolean} [immediateRetrieval=false]    Indicates whether the transfer should begin immediately, or be deferred until the stage completes.
 * @property {string[]} [exclude=[]]                 An optional list of globbed paths on the target to exclude from this transfer. Escaping of metachars (*) is not supported.
 * @property {boolean} [backgroundTransfer=false]    Set this if the file is not required for phase 1. Background artifacts may transfer immediately if capacity is available, but
 *         waiting background transfers are deprioritized while phase 1 transfers are flushed.
 */

/**
 * JS engine binding exposed to tools (argument 1 to stage functions).
 * Unless otherwise noted, methods reject their Promise on failure.
 * @typedef {Object} Engine
 *
 * @property {(cmd:(string|string[]), opts?:ExecOptions) => Promise<CommandResult>} execCommand
 * Run a command and resolve with its full output.
 * - `cmd` defines the program to launch along with its arguments.
 * - If the program cannot be found through `PATH`, resolves with exit code 127.
 *
 * @property {(cmd:(string|string[]), opts:ProcessOptions) => Promise<ProcessHandle>} startProcess
 * Launch a process and resolve to a `ProcessHandle` allowing interaction of the process
 * within the runtime before it ends, e.g. kill or process stdout/stderr
 *
 * @property {() => Promise<string>} createTempDir
 * Create a temporary directory, returning its absolute path.
 * On Linux, it will be owned by current user with file mode 0o700 permissions.
 * On Windows, it will be owned by current user with default ACL permissions.
 *
 * @property {(path:string) => Promise<void>} mkDir
 * Make a directory
 *
 * @property {(path:string, recursive:boolean, force:boolean) => Promise<void>} rm
 * Remove a file or directory. Use `recursive`/`force` with care.
 *
 * @property {(path:string, recursive:boolean) => Promise<void>} makeWritable
 * Make file writable / dir writable and traversable; recursive if requested.
 *
 * @property {(path:string, owner:string, recursive:boolean) => Promise<void>} chown
 * Change ownership (format of `owner` is engine/OS-specific).
 *
 * @property {(path:string, runRelativePath:string, meta:OutputMetadata, transferOptions?:TransferOptions) => void} emitOutput
 * Register an artifact produced by the tool for collection.
 * `path` locates the artifact on the locality's filesystem. `runRelativePath` specifies the destination path inside
 * the run directory on the host.
 * For globbed outputs, `runRelativePath` must use the same wildcard-containing suffix as `path`,
 * starting at the first path segment containing `*`, after path normalization. Only the concrete
 * prefix before that segment may differ.
 * Globs may not rename or reshape wildcarded path segments.
 * `transferOptions` defines configuration options for how the transfer should be executed.
 * Artifacts will only be transferred on successful tool integration, whereas log files will always be collected.
 * @property {() => boolean} isFullCaptureSupportEnabled
 * Returns true when the EnableFullCaptureSupport feature flag is enabled
 * @property {() => boolean} isNeoprofTimelineEnabled
 * Returns true when the EnableNeoprofTimeline feature flag is enabled
 * @property {(relativePath:string, meta:OutputMetadata) => Promise<FileHandle>} createRunFile
 * Create a file in the run directory and return a handle for writing.
 * @property {(absolutePath:string) => Promise<string>} readHostFile
 * Read the contents of a host file.
 * @property {(level:("debug"|"info"|"warn"|"error"), message:string) => void} log
 * Write to the run log file
 * @property {(level:("info"|"warn"|"error"), message:string) => void} writeUserMessage
 * Records a message to display to the user.
 * @property {(id:string) => void} startProgressTracker
 * Begin a progress tracker identified by `id`.
 * @property {(id:string, message:string, percent:number) => void} updateProgress
 * Update progress for `id`. `percent` is a float in the range [0, 100].
 * @property {(id:string) => void} endProgress
 * End the tracker identified by `id`.
 * @property {(name:string) => Engine} withLocality
 * Return an engine instance that routes operations through the specified locality.
 * Supported values are currently `"target"` and `"host"`.
 * @property {() => string} getLocality
 * Return the current engine locality name.
 * @property {() => string} toolsRoot
 * Return the root tool deployment directory for this engine locality.
 *
 * @property {(sourceLocality:("target"), sourcePath:string, destinationPath:string) => Promise<void>} copyFrom
 * Copy a file from another locality to the current engine locality and resolve when the copy has completed.
 */

/**
 * Handle for writing files.
 * @typedef {Object} FileHandle
 * @property {(chunk:string) => Promise<void>} append
 * Append a chunk of data to the file.
 * @property {() => Promise<void>} close
 * Manually close the underlying writer for the file. Any open handles will be closed automatically when the tool integration completes.
 * @property {string} path
 * Absolute path to the file.
 */

/**
 * Async stream chunk data.
 * @typedef {string} StreamChunk
 */

/**
 * Handle for a running process returned by `engine.startProcess`.
 * @typedef {Object} ProcessHandle
 * @property {number} pid
 * @property {() => Promise<void>} kill
 * Send SIGKILL (or platform equivalent).
 * @property {() => Promise<void>} interrupt
 * Send SIGINT (or platform equivalent).
 * @property {() => Promise<{exitCode:number}>} wait
 * Resolve when the process exits, returning `{ exitCode }`.
 * @property {AsyncIterable<StreamChunk>} [stdout]
 * Present only if `stdout.redirect` is "stream" or "both".
 * @property {AsyncIterable<StreamChunk>} [stderr]
 * Present only if `stderr.redirect` is "stream" or "both".
 * @property {(data:string) => Promise<void>} writeStdin
 * Write to stdin. Rejects if `stdinOpen` was false in `ProcessOptions`.
 */

/**
 * Workload: launched by command.
 * @typedef {Object} WorkloadLaunch
 * @property {"launch"} type
 * @property {string} rawCommand  The raw workload string provided in the RPC call, before parsing. This should be used by tools which expect a single workload arg
 * @property {string[]} command  The parsed workload, stored as a slice of strings. This should be used by tools which expect the workload as multiple args
 * @property {Object.<string,string>} [environment]  Environment variables to use for the workload (key/value)
 * @property {string} workingDir  Working directory in which the workload should be executed.
 * @property {boolean} useShell  Whether the user requested that this workload be run through a shell
 *   Note that rawCommand and command will already be updated to reflect this - this field is only included so that tool ints can validate its value
 */

/**
 * Workload: launched Android package activity.
 * @typedef {Object} WorkloadAndroidLaunch
 * @property {"androidLaunch"} type
 * @property {string} packageName
 * @property {string} activityName
 */

/**
 * Workload: attach to existing PID.
 * @typedef {Object} WorkloadAttach
 * @property {"attach"} type
 * @property {number} pid
 */

/**
 * Workload: system-wide mode.
 * @typedef {Object} WorkloadSystemWide
 * @property {"systemWide"} type
 */

/** Generic Workload Type
 * @typedef {WorkloadLaunch | WorkloadAndroidLaunch | WorkloadAttach | WorkloadSystemWide} Workload
 */

/**
 * Context passed to your tool
 * @typedef {Object} ToolContext
 * @property {Object.<string, any>} params                               Parameters from the recipe/invocation.
 * @property {Workload} workload Launch/Android launch/attach/system-wide info (if any).
 * @property {string} workingdir                                         Working directory chosen by the engine/recipe.
 * @property {Object.<string,string>} env                                Environment variables for this invocation.
 * @property {number} timeout                                            Run timeout in seconds.
 * @property {string} toolsRoot                                          Deprecated: legacy target tools root. Use `engine.toolsRoot()` for locality-aware tool resolution.
 * @property {string} engineVersion                                      The current Performix engine version.
 * @property {any} metadata                                              Shared object across run/cancel/stop/reformat
 */

/**
 * Human-readable description for UIs.
 * @typedef {Object} ToolDescription
 * @property {string} short
 * @property {string} long
 */

/**
 * Shape of the global `tool` object your script must define.
 * Field names are lower-camelcase in JS.
 * @typedef {Object} ToolIntegration
 * @property {string} name
 * @property {string} version
 * @property {Deployment[]} [deployments] // defines deployments required by the recipe
 * @property {ToolDescription} description
 * @property {MigrationEntry[]} [migrations]
 * @property {boolean} [supportsWorkloadLaunch=false]
 * @property {Parameter[]} [parameters] // parameter definitions accepted by the tool integration
 * @property {(engine:Engine, ctx:ToolContext) => (void|Promise<void>)} run // Entry point for normal execution. May be async.
 * @property {(engine:Engine, ctx:ToolContext) => (ProbeResult | Promise<ProbeResult>)} [probe]
 * @property {(engine:Engine, ctx:ToolContext) => (void|Promise<void>)} reformat // Transform outpule files after run, before they're transferred to the host
 * @property {(engine:Engine, ctx:ToolContext) => (void|Promise<void>)} onCancel // Respond to cancellation.
 * @property {(engine:Engine, ctx:ToolContext) => (void|Promise<void>)} onStop // Force stop
 */

/**
 * @typedef {Object} BaseMigration
 * @property {string} version   - semver string at which the change occurred
 */

/**
 * @typedef {Object} ToolNameMigration
 * @property {"name"} kind
 * @property {string} from      - old folder name, e.g. "streamline-cli"
 * @property {string} to        - new folder name, e.g. "neoprof"
 * @property {string} version
 */

/**
 * @typedef {Object} ToolPathSuffixMigration
 * @property {"pathSuffix"} kind
 * @property {string} tool      - tool name (unchanged), e.g. "neoprof"
 * @property {string} oldSuffix - old sub-path, e.g. "foo/bar"
 * @property {string} newSuffix - new sub-path, e.g. "baz/waz"
 * @property {string} version
 */

/**
 * @typedef {ToolNameMigration | ToolPathSuffixMigration} Migration
 */

/**
 * @typedef {Object} ProbeAdvice
 * A single element of the probe result advice, describing an issue or recommendation.
 * @property {string} level // ready, warning, error, unknown
 * @property {string} [message] // The advice message
 * @property {string} [messageCode] // The advice message code
 * @property {Object.<string, string>} [metadata] // (optional) any metadata to attach to the message
 * @property {string} [cause] // (optional) any cause to attach to the message
 */

/**
 * Result of a tool probe
 * @typedef {Object} ProbeResult
 * @property {boolean} available // The tool is available to use
 * @property {Record<string, any>} [capabilities] // A map of tool capabilities
 * @property {ProbeAdvice[]} [advice] // Advice messages to report back to the user
 */

/**
 * @typedef {Object} CatalogMessage
 * @property {string} code // The code of the catalog message
 * @property {Object.<string, string>} [metadata] // The metadata to attach to the message
 * @property {string} [cause] // The cause to attach to the message
 */

export {};
