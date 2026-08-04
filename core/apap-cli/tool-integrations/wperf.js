// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

// The bundle version should be updated manually when either wperf or the wperf helper is updated.
const wperfBundleVersion = '1.0.1';
const wperfExecutableName = 'wperf.exe';
const wperfHelperExecutableName = 'wperf-helper.exe';
const wperfDriverLauncherExecutableName = 'wperf-devgen.exe';
const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';
const toolNameWperf = 'wperf';

/**
 * @typedef {object} WperfError
 * @property {string} code
 * @property {object} metadata
 */

/**
 * @typedef {object} WperfDeployment
 * @property {string} deployRoot
 * @property {string} binaryDir
 * @property {string} driverDir
 * @property {string} wperfExecutablePath
 * @property {string} wperfHelperExecutablePath
 * @property {string} wperfDriverLauncherPath
 */

/**
 * @type {import("../recipes/docs/jsdocs").ToolIntegration}
 */
let tool = {
  name: toolNameWperf,
  version: '1.0.1',
  supportsWorkloadLaunch: true,
  description: {
    short: `Collect performance data using the Windows ${toolNameWperf} tooling.`,
    long: `Invokes the ${toolNameWperf} command line utility to collect performance data on Windows targets. The integration handles process lifecycle and log capture so that recipes can launch workloads and retrieve the results.`,
  },
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Windows' }],
      dependencies: [
        {
          type: 'tool_bundle',
          name: toolNameWperf,
          version: wperfBundleVersion,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  parameters: [
    {
      id: 'mode',
      label: 'Profiling mode',
      description: `Select the profiling mode used when invoking \`${toolNameWperf}\`. Currently, only \`samples\` is supported - collect CPU samples.`,
      config: {
        type: 'radio',
        defaultValue: 'samples',
        options: [{ value: 'samples', label: 'Samples' }],
      },
    },
    {
      id: 'sampling_frequency',
      label: 'Sampling Frequency',
      description:
        "Select the sampling frequency for the CPU microarchitecture analysis. The 'normal' frequency is suitable for most workloads, while 'high' provides more detailed information at the cost of increased overhead.",
      config: {
        type: 'radio',
        options: [
          { value: 'low', label: 'Low' },
          { value: 'normal', label: 'Normal' },
          { value: 'high', label: 'High' },
        ],
        defaultValue: 'normal',
      },
    },
  ],

  probe: async (engine, ctx) => {
    const advice = [];
    let available = true;
    let deployment = await setupEnv(engine, ctx);

    if (ctx.workload.type === 'systemWide') {
      advice.push({
        level: 'error',
        messageCode: readinessMessageCode,
        metadata: {
          message: `The \`${toolNameWperf}\` tool cannot be used with system-wide capture. Run the recipe again with either the launch or attach configuration.`,
        },
      });
    }

    const deploymentDirExists = await checkDirExists(
      engine,
      deployment.deployRoot,
    );
    const missingBinaries = deploymentDirExists
      ? await checkDeploymentBinaries(engine, deployment)
      : [];
    if (!deploymentDirExists || missingBinaries.length > 0) {
      available = false;
      advice.push({
        level: 'error',
        messageCode: 'tool_integrations.common.TOOL_NOT_DEPLOYED',
        metadata: {
          tool: tool.name,
          deployPath: deployment.deployRoot,
          locality: engine.getLocality(),
        },
      });
    }
    return { available: available, advice: advice, capabilities: {} };
  },

  run: async (engine, ctx) => {
    const configInvalidMessage = checkRunRequestConfiguration(ctx);

    if (configInvalidMessage) {
      throw {
        code: 'tool_integrations.wperf.UNSUPPORTED_CONFIGURATION',
        metadata: { unsupportedConfiguration: configInvalidMessage },
      };
    }

    const setupResult = await setup(engine, ctx);
    if (setupResult.error) {
      throw setupResult.error;
    }

    const deployment = setupResult.deployment;

    let outputDirectory = await engine.createTempDir();
    outputDirectory = normalizeWindowsPath(outputDirectory);

    let captureDirectory = outputDirectory;

    ctx.metadata.outputDirectory = outputDirectory;
    ctx.metadata.captureDirectory = captureDirectory;

    engine.log(
      'info',
      `Launch env vars: ${JSON.stringify(ctx.workload.environment)}`,
    );

    const processArgs = buildWperfHelperRecordArgs(ctx, deployment);
    emitAllFiles(ctx, engine);

    let opts = {
      stdout: {
        redirect: 'file',
        path: pathJoin(captureDirectory, 'helper_log.txt'),
      },
      stderr: {
        redirect: 'file',
        path: pathJoin(captureDirectory, 'helper_err.txt'),
      },
      environment: {
        ...(ctx.env || {}), // General tool environment
        // Workload-specified environment - for wperf, these are just provided to the tool in the same way as the
        // env vars above, as there's currently no way to provide env vars specifically to the workload
        ...(ctx.workload.environment || {}),
      },
    };
    if (ctx.workload.type === 'launch') {
      opts.workingDirectory = ctx.workload.workingDir;
    }

    engine.log(
      'info',
      `Starting ${toolNameWperf} collection with arguments: ${processArgs.join(';')}; options: ${JSON.stringify(opts)}`,
    );

    engine.startProgressTracker('Collecting data');
    let recordHandle = await engine.startProcess(processArgs, opts);

    ctx.metadata.recordHandle = recordHandle;

    if (ctx.metadata.requestCancel) {
      ctx.metadata.recordHandle.kill();
    }

    // Same hack as neoprof to allow graceful stop after 2 seconds
    setTimeout(() => {
      ctx.metadata.allowStop = true;
      if (ctx.metadata.requestStop) {
        ctx.metadata.recordHandle.interrupt();
      }
    }, 2000);

    let result = await ctx.metadata.recordHandle.wait();
    ctx.metadata.recordHandle = null;

    if (result.exitCode !== 0) {
      throw wperfHelperRetCodeToError(result.exitCode, '');
    }
    engine.endProgress('Collecting data');
  },

  reformat: async (engine, ctx) => {
    const deployment = await setupEnv(engine, ctx);
    const processArgs = buildWperfHelperReformatArgs(ctx, deployment);

    engine.startProgressTracker('Analyzing collection');

    let analyzeHandle = await engine.startProcess(processArgs, {
      stdout: {
        redirect: 'file',
        path: pathJoin(ctx.metadata.captureDirectory, 'analysis_log.txt'),
      },
      stderr: {
        redirect: 'file',
        path: pathJoin(ctx.metadata.captureDirectory, 'analysis_err.txt'),
      },
    });
    ctx.metadata.analyzeHandle = analyzeHandle;

    if (ctx.metadata.requestCancel) {
      ctx.metadata.analyzeHandle.kill();
    }

    let result = await ctx.metadata.analyzeHandle.wait();
    ctx.metadata.analyzeHandle = null;
    if (result.exitCode !== 0) {
      throw wperfHelperRetCodeToError(result.exitCode, '');
    }
    engine.endProgress('Analyzing collection');
  },

  onCancel: async (engine, ctx) => {
    ctx.metadata.requestCancel = true;
    if (ctx.metadata.recordHandle) {
      ctx.metadata.recordHandle.kill();
    }
    if (ctx.metadata.analyzeHandle) {
      ctx.metadata.analyzeHandle.kill();
    }
  },

  onStop: async (engine, ctx) => {
    ctx.metadata.requestStop = true;
    if (ctx.metadata.recordHandle && ctx.metadata.allowStop) {
      await ctx.metadata.recordHandle.interrupt();
    }
  },
};

/**
 * Escape single quotes for use in a PowerShell single-quoted literal.
 * @param {string} path
 * @returns {string}
 */
function escapeForPowerShellLiteral(path) {
  return path.replace(/'/g, "''");
}

/**
 * Shared setup logic for probe and other operations.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<{error: WperfError | null, deployment: WperfDeployment}>}
 */
async function setup(engine, ctx) {
  const deployment = await setupEnv(engine, ctx);
  const progressStage = `Getting ready for ${toolNameWperf} collection`;

  engine.startProgressTracker(progressStage);
  engine.updateProgress(progressStage, 'Checking deployment', 0.0);
  try {
    const deploymentDirExists = await checkDirExists(
      engine,
      deployment.deployRoot,
    );
    if (!deploymentDirExists) {
      return {
        error: {
          code: 'tool_integrations.common.TOOL_NOT_DEPLOYED',
          metadata: {
            tool: tool.name,
            deployPath: deployment.deployRoot,
            locality: engine.getLocality(),
          },
        },
        deployment,
      };
    }
    const missingBinaries = await checkDeploymentBinaries(engine, deployment);
    if (missingBinaries.length > 0) {
      return {
        error: {
          code: 'tool_integrations.common.TOOL_NOT_DEPLOYED',
          metadata: {
            tool: tool.name,
            deployPath: deployment.deployRoot,
            locality: engine.getLocality(),
          },
        },
        deployment,
      };
    }

    engine.updateProgress(progressStage, `Setting up ${toolNameWperf}`, 20.0);
    const setupResult = await runWperfHelperSetup(engine, deployment);
    if (setupResult.rc !== 0) {
      throw wperfHelperRetCodeToError(setupResult.rc, setupResult.stderr);
    }

    for (let attempt = 0; attempt < 2; attempt++) {
      engine.updateProgress(
        progressStage,
        `Checking ${toolNameWperf} driver`,
        40.0 + attempt * 40.0,
      );

      const wperfCheckError = await runWperfSanityCheck(engine, deployment);
      if (!wperfCheckError) {
        return { error: null, deployment };
      }

      if (
        wperfCheckError.code === 'tool_integrations.wperf.DRIVER_NOT_STARTED'
      ) {
        if (attempt == 1) {
          // If we've already attempted to launch the driver, return the error instead of trying again
          return { error: wperfCheckError, deployment };
        }
        engine.updateProgress(
          progressStage,
          `Starting ${toolNameWperf} driver`,
          60.0,
        );

        const result = await launchWperfDriver(engine, deployment);
        if (result.rc !== 0) {
          return {
            error: {
              code: 'tool_integrations.wperf.DRIVER_LAUNCH_FAILED',
              metadata: { exitCode: result.rc, output: result.stderr },
            },
            deployment,
          };
        }
      } else {
        return { error: wperfCheckError, deployment };
      }
    }
  } catch (err) {
    return {
      error: {
        code: 'tool_integrations.wperf.GENERIC_SETUP_FAILURE',
        metadata: {
          error: String(err),
        },
      },
      deployment,
    };
  } finally {
    engine.endProgress(progressStage);
  }

  return { error: null, deployment };
}

/**
 * Maps a wperf-helper return code to an error object.
 * @param {number} returnCode
 * @param {string} stderr
 * @returns {WperfError | null}
 */
function wperfHelperRetCodeToError(returnCode, stderr) {
  const EXIT_SUCCESS = 0;
  const EXIT_GENERAL_ERROR = 1; // Handled by the WPERF_FAILED case

  // Argument and environment errors
  const EXIT_INVALID_ARGUMENTS = 2; // Handled by the WPERF_FAILED case
  const EXIT_INVALID_ENVIRONMENT = 3; // Handled by the WPERF_FAILED case
  const EXIT_WPERF_NOT_FOUND = 4; // Handled by the WPERF_FAILED case
  const EXIT_TARGET_BINARY_NOT_FOUND = 5;
  const EXIT_TARGET_PDB_NOT_FOUND = 6;
  const EXIT_LLVM_OBJDUMP_NOT_FOUND = 7;

  // Capture errors
  const EXIT_RECORD_GENERIC_FAILURE = 10; // Handled by the WPERF_FAILED case

  // Post processing errors
  const EXIT_REFORMAT_GENERIC_FAILURE = 20;
  const EXIT_REFORMAT_FILE_MISSING = 21;
  const EXIT_REFORMAT_NO_WPERF_SAMPLES = 22;
  const EXIT_REFORMAT_INVALID_FORMAT = 23;

  // Setup errors
  const EXIT_SETUP_GENERIC_FAILURE = 30;
  const EXIT_SETUP_MSDIA_DLL_NOT_FOUND = 31;

  switch (returnCode) {
    case EXIT_SUCCESS:
      return null;
    case EXIT_TARGET_BINARY_NOT_FOUND:
      return {
        code: 'tool_integrations.wperf.TARGET_BINARY_NOT_FOUND',
        metadata: {},
      };
    case EXIT_TARGET_PDB_NOT_FOUND:
      return {
        code: 'tool_integrations.wperf.TARGET_PDB_NOT_FOUND',
        metadata: {},
      };
    case EXIT_LLVM_OBJDUMP_NOT_FOUND:
      return {
        code: 'tool_integrations.wperf.LLVM_OBJDUMP_NOT_FOUND',
        metadata: {},
      };
    case EXIT_REFORMAT_NO_WPERF_SAMPLES:
      return {
        code: 'tool_integrations.wperf.NO_SAMPLES_COLLECTED',
        metadata: {},
      };
    case EXIT_REFORMAT_GENERIC_FAILURE:
    case EXIT_REFORMAT_FILE_MISSING:
    case EXIT_REFORMAT_INVALID_FORMAT:
      return {
        code: 'tool_integrations.wperf.POST_PROCESS_FAILED',
        metadata: { exitCode: returnCode },
      };
    case EXIT_SETUP_MSDIA_DLL_NOT_FOUND:
      return {
        code: 'tool_integrations.wperf.MSDIA_DLL_NOT_FOUND',
        metadata: {},
      };
    case EXIT_SETUP_GENERIC_FAILURE:
      return {
        code: 'tool_integrations.wperf.WPERF_SETUP_FAILED',
        metadata: { exitCode: returnCode, output: stderr },
      };
  }
  if (stderr) {
    return {
      code: 'tool_integrations.wperf.WPERF_FAILED_WITH_OUTPUT',
      metadata: { exitCode: returnCode, output: stderr },
    };
  }
  return {
    code: 'tool_integrations.wperf.WPERF_FAILED',
    metadata: { exitCode: returnCode },
  };
}

/**
 * Prepare the deployment metadata for wperf execution.
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<WperfDeployment>}
 */
async function setupEnv(engine, ctx) {
  ctx.metadata = ctx.metadata ?? {};

  if (!ctx.metadata.wperfDeployment) {
    const toolsRoot = normalizeWindowsPath(ctx.toolsRoot);
    const deployRoot = `${toolsRoot}\\${toolNameWperf}\\${wperfBundleVersion}`;
    const binaryDir = `${deployRoot}\\${toolNameWperf}`;
    const driverDir = `${deployRoot}\\wperf-driver`;
    const wperfExecutablePath = `${binaryDir}\\${wperfExecutableName}`;
    const wperfHelperExecutablePath = `${binaryDir}\\${wperfHelperExecutableName}`;
    const wperfDriverLauncherPath = `${driverDir}\\${wperfDriverLauncherExecutableName}`;

    ctx.metadata.wperfDeployment = {
      deployRoot: deployRoot,
      binaryDir: binaryDir,
      driverDir,
      wperfExecutablePath,
      wperfHelperExecutablePath,
      wperfDriverLauncherPath,
    };
  }

  return ctx.metadata.wperfDeployment;
}

/**
 * Check whether all required deployment binaries exist on the target.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {WperfDeployment} deployment
 * @returns {Promise<string[]>} List of missing executable names
 */
async function checkDeploymentBinaries(engine, deployment) {
  const [wperfExists, helperExists, launcherExists] = await Promise.all([
    checkFileExists(engine, deployment.wperfExecutablePath),
    checkFileExists(engine, deployment.wperfHelperExecutablePath),
    checkFileExists(engine, deployment.wperfDriverLauncherPath),
  ]);
  return [
    wperfExists ? null : wperfExecutableName,
    helperExists ? null : wperfHelperExecutableName,
    launcherExists ? null : wperfDriverLauncherExecutableName,
  ].filter((x) => x !== null);
}

/**
 * Check whether a file exists on the target
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} filePath
 * @returns {Promise<boolean>}
 */
async function checkFileExists(engine, filePath) {
  return checkPathExists(engine, filePath, 'file');
}

/**
 * Check whether a directory exists on the target
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} dirPath
 * @returns {Promise<boolean>}
 */
async function checkDirExists(engine, dirPath) {
  return checkPathExists(engine, dirPath, 'dir');
}

/**
 * Check whether a path exists on the target and is of the expected type (file or directory).
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path
 * @param {'file' | 'dir'} type
 * @returns {Promise<boolean>}
 */
async function checkPathExists(engine, path, type) {
  let pathTypeFlag;
  if (type === 'file') {
    pathTypeFlag = '-PathType Leaf';
  } else if (type === 'dir') {
    pathTypeFlag = '-PathType Container';
  } else {
    throw new Error(`Invalid type: ${type}. Must be 'file' or 'dir'.`);
  }
  const result = await engine.execCommand(
    [
      'powershell',
      '-NoProfile',
      '-Command',
      `if (Test-Path -LiteralPath '${path}' ${pathTypeFlag}) { exit 0 } else { exit 1 }`,
    ],
    {},
  );
  return result.rc === 0;
}

/**
 * Check whether the wperf driver is installed on the target.
 * Returns one of DRIVER_CHECK_SUCCESS, DRIVER_CHECK_FAILURE_MISSING_DRIVER, DRIVER_CHECK_GENERIC_FAILURE.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {WperfDeployment} deployment
 * @returns {Promise<WperfError | null>}
 */
async function runWperfSanityCheck(engine, deployment) {
  const result = await engine.execCommand(
    [deployment.wperfExecutablePath, 'test'],
    {},
  );
  const stderr = result.stderr || '';
  if (result.rc === 0) {
    return null;
  }
  if (stderr.includes('No active device interfaces found.')) {
    return { code: 'tool_integrations.wperf.DRIVER_NOT_STARTED', metadata: {} };
  }
  return {
    code: 'tool_integrations.wperf.SANITY_CHECK_FAILED',
    metadata: { output: stderr },
  };
}

/**
 * Check whether the wperf driver is installed on the target and attempts to launch it if not.
 * Returns a ProveAdvice object indicating success or failure.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {WperfDeployment} deployment
 * @returns {Promise<import("../recipes/docs/jsdocs").CommandResult>}
 */
async function launchWperfDriver(engine, deployment) {
  const localLauncherPath = pathJoin('.', wperfDriverLauncherExecutableName);
  return await engine.execCommand(
    [
      'powershell',
      '-NoProfile',
      '-Command',
      `Set-Location ${deployment.driverDir}; & ${localLauncherPath} install; & ${localLauncherPath} enable`,
    ],
    {},
  );
}

/**
 * Sets up wperf by calling wperf-helper setup which registers the DIA DLL
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {WperfDeployment} deployment
 * @returns {Promise<import("../recipes/docs/jsdocs").CommandResult>}
 */
async function runWperfHelperSetup(engine, deployment) {
  return await engine.execCommand(
    [deployment.wperfHelperExecutablePath, 'setup'],
    {},
  );
}

/**
 * Checks if the current run config is supported. If not, returns a message indicating why.
 * @param {import("./docs/jsdocs").ToolContext} ctx
 * @returns {string}
 */
function checkRunRequestConfiguration(ctx) {
  const mode = ctx.params['mode'];
  const samplingFrequency = ctx.params['sampling_frequency'];
  const workloadType = ctx.workload.type;

  if (mode !== 'samples') {
    return "Only 'samples' collection  is currently supported.";
  }

  if (
    samplingFrequency !== 'low' &&
    samplingFrequency !== 'normal' &&
    samplingFrequency !== 'high'
  ) {
    return `Invalid sampling frequency selected: ${samplingFrequency}.`;
  }

  switch (ctx.workload.type) {
    case 'launch':
      if (!ctx.workload.command || ctx.workload.command.length === 0) {
        return "Workload command must be specified for 'launch' workload type.";
      }
      break;
    case 'attach':
      if (!ctx.workload.pid) {
        return "Workload PID must be specified for 'attach' workload type.";
      }
      break;
    default:
      return `Unsupported workload type: ${workloadType}. Only 'launch' and 'attach' are supported.`;
  }

  return '';
}

/**
 * Builds the arguments for wperf-helper's 'record' command based on the given parameters.
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {WperfDeployment} deployment
 * @returns {string[]}
 */
function buildWperfHelperRecordArgs(ctx, deployment) {
  const mode = ctx.params['mode'];
  const samplingFrequency = ctx.params['sampling_frequency'];
  const timeout = ctx.timeout ? ctx.timeout.toString() : '0';

  let args = [
    deployment.wperfHelperExecutablePath,
    'record',
    mode,
    '--output-dir',
    ctx.metadata.captureDirectory,
    '--frequency',
    samplingFrequency,
    '--duration',
    timeout,
    '--callstacks',
  ];

  if (ctx.workload.type === 'launch') {
    args.push('--cmd');
    args.push(normalizeWindowsPath(ctx.workload.rawCommand)); // Make sure paths are Windows-style
  } else if (ctx.workload.type === 'attach') {
    args.push('--pid');
    args.push(ctx.workload.pid.toString());
  }

  return args;
}

/**
 * Builds the arguments for wperf-helper based on the given parameters.
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {WperfDeployment} deployment
 * @returns {string[]}
 */
function buildWperfHelperReformatArgs(ctx, deployment) {
  let args = [
    deployment.wperfHelperExecutablePath,
    'reformat',
    ctx.params['mode'],
    '--input-dir',
    ctx.metadata.captureDirectory,
    '--output-dir',
    ctx.metadata.captureDirectory,
  ];
  return args;
}

/**
 * Registers the common artifacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function emitCommonFiles(engine, outputDir) {
  engine.emitOutput(
    pathJoin(outputDir, 'symbols.json'),
    pathJoin('output', 'symbols.json'),
    { name: 'sl-collect-symbols', version: '1.1' },
  );
  engine.emitOutput(
    pathJoin(outputDir, 'sources-capture-periodic_sampling*'),
    pathJoin('output', 'sources-capture-periodic_sampling*'),
    { name: 'sl-collect-source-line-attribution', version: '1.0' },
  );
  engine.emitOutput(pathJoin(outputDir, 'stdout.txt'), 'capture_log.txt', {
    name: 'log-text',
    version: '1.0',
  });
  engine.emitOutput(pathJoin(outputDir, 'stderr.txt'), 'capture_err.txt', {
    name: 'log-text',
    version: '1.0',
  });
  for (let log_type of ['helper', 'analysis']) {
    engine.emitOutput(
      pathJoin(outputDir, `${log_type}_log.txt`),
      `${log_type}_log.txt`,
      { name: 'log-text', version: '1.0' },
    );
    engine.emitOutput(
      pathJoin(outputDir, `${log_type}_err.txt`),
      `${log_type}_err.txt`,
      { name: 'log-text', version: '1.0' },
    );
  }
}

/**
 * Registers hotspots-specific artifacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function emitHotspotFiles(engine, outputDir) {
  engine.emitOutput(
    pathJoin(outputDir, 'call_tree_samples.json'),
    pathJoin('output', 'call_tree_samples.json'),
    { name: 'sl-collect-call-tree', version: '1.0' },
  );
  engine.emitOutput(
    pathJoin(outputDir, 'callpath_self_samples.json'),
    pathJoin('output', 'callpath_self_samples.json'),
    { name: 'sl-collect-metrics', version: '1.0' },
  );
  engine.emitOutput(
    pathJoin(outputDir, 'callpath_total_samples.json'),
    pathJoin('output', 'callpath_total_samples.json'),
    { name: 'sl-collect-metrics', version: '1.0' },
  );
  engine.emitOutput(
    pathJoin(outputDir, 'functions-capture-periodic_sampling.csv'),
    pathJoin('output', 'functions-capture-periodic_sampling.csv'),
    { name: 'sl-collect-flat-functions-csv', version: '1.1' },
  );
}

/**
 * Registers hotspots-specific artifacts.
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {void}
 */
function emitAllFiles(ctx, engine) {
  emitCommonFiles(engine, ctx.metadata.captureDirectory);
  if (ctx.params['mode'] === 'samples') {
    emitHotspotFiles(engine, ctx.metadata.captureDirectory);
  }
}

/**
 * Join Windows path segments using backslash separators making sure the parts don't start/end with extra slashes.
 * @param {...string} parts
 * @returns {string}
 */
function pathJoin(...parts) {
  const sep = '\\';
  const leadingRe = /^\\+/;
  const trailingRe = /\\+$/;

  const cleaned = [];
  for (let i = 0; i < parts.length; i++) {
    let p = parts[i];
    if (p == null) {
      continue;
    }
    p = String(p);

    if (!p) continue;

    // Trim leading separators
    p = p.replace(leadingRe, '');

    // Trim trailing separators
    p = p.replace(trailingRe, '');
    cleaned.push(p);
  }
  return cleaned.join(sep);
}

/**
 * Ensure a path uses Windows backslash separators.
 * @param {string} path
 * @returns {string}
 */
function normalizeWindowsPath(path) {
  return path.replace(/\//g, '\\').replace(/\\\\+/g, '\\');
}
