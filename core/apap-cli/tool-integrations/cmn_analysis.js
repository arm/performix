// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  probePythonVenv,
  ensureDeployed,
  probeWhl,
  buildToolBundlePath,
  getPerfSetting,
  normalizeRootOutputAccess,
} = require('./utils');
const { launchWorkloadIfNeeded } = require('./workload');

// Topdown tool bundle
const TOPDOWN_TOOL_NAME = 'topdown-tool';
let topdownWheelVersion = '0.1.0';
let topdownBundleVersion = `${topdownWheelVersion}-dev`;
let topdownToolFile = `topdown_tool-${topdownWheelVersion}-py3-none-any.whl`;

// cmn-tools tool bundle
const CMN_DISCOVER_NAME = 'cmn_discover';
let cmnToolsWheelVersion = '0.1.0';
let cmnToolsBundleVersion = `${cmnToolsWheelVersion}-dev`;
let cmnToolsFile = `cmn_tools-${cmnToolsWheelVersion}-py3-none-any.whl`;

// wperf-cmn-visualizer tool bundle
const CMN_VISUALIZER_NAME = 'wperf-cmn-visualizer';
let cmnVisualizerBundleVersion = '1.4.0';
let cmnVisualizerFile = `wperf_cmn_visualizer-${cmnVisualizerBundleVersion}-py3-none-any.whl`;

/**
 * Resolve deployment paths for required wheels.
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {{topdown: string, cmnTools: string, cmnVisualizer: string}}
 */
function getDeployPaths(ctx) {
  const toolsRoot = ctx.toolsRoot;
  return {
    topdown: buildToolBundlePath(
      toolsRoot,
      topdownToolFile,
      topdownBundleVersion,
    ),
    cmnTools: buildToolBundlePath(
      toolsRoot,
      cmnToolsFile,
      cmnToolsBundleVersion,
    ),
    cmnVisualizer: buildToolBundlePath(
      toolsRoot,
      cmnVisualizerFile,
      cmnVisualizerBundleVersion,
    ),
  };
}

const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';

const PYTHON_VER_MAJOR = 3;
const PYTHON_VER_MINOR = 9;

const DEFAULT_SAMPLE_DURATION_MS = 3000;
const FORCE_KILL_GRACE_MS = 1000;
const SAMPLING_INTERVAL_MS = {
  low: 10000,
  normal: 5000,
  high: 3500,
};

const RAW_CSV_PATTERN = /^cmn-(\d+)_([^.]+)\.csv$/;
const RENAMED_CSV_PATTERN = /^cmn\d+_[^_]+_\d+\.csv$/;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * @type {import("../recipes/docs/jsdocs").ToolIntegration}}
 */
let tool = {
  name: 'cmn_analysis',
  version: '0.1.0',
  supportsWorkloadLaunch: true,
  description: {
    short: 'CMN Analysis Tool for Coherent Mesh Network performance metrics.',
    long: 'The CMN Analysis Tool collects performance metrics from the Coherent Mesh Network (CMN) in ARM-based systems. It utilizes the Topdown Tool to probe CMN metrics and generate CSV reports for analysis. This tool is essential for understanding CMN behavior and optimizing system performance.',
  },
  parameters: [
    {
      id: 'samplingFreq',
      label: 'Sampling Frequency',
      description:
        "Controls the interval between CMN samples. Use 'normal' for typical workloads; 'low' reduces overhead, 'high' increases fidelity.",
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
    {
      id: 'toolDuration',
      label: 'Sample Duration (milliseconds)',
      description:
        'Duration of each topdown CMN sampling window. Must be shorter than the sampling interval and greater than 3000ms, shorter durations are unreliable.',
      config: {
        type: 'input',
        defaultValue: String(DEFAULT_SAMPLE_DURATION_MS),
        custom: {},
      },
    },
  ],
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Linux' }],
      dependencies: [
        {
          type: 'tool_bundle',
          name: topdownToolFile,
          version: topdownBundleVersion,
          requiredWhen: { type: 'always' },
        },
        {
          type: 'tool_bundle',
          name: cmnToolsFile,
          version: cmnToolsBundleVersion,
          requiredWhen: { type: 'always' },
        },
        {
          type: 'tool_bundle',
          name: cmnVisualizerFile,
          version: cmnVisualizerBundleVersion,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  probe: async (engine, ctx) => {
    /** @type {import("../recipes/docs/jsdocs").ProbeAdvice[]} */
    let advice = [];
    const deployPaths = getDeployPaths(ctx);

    let py = await probePythonVenv(
      engine,
      PYTHON_VER_MAJOR,
      PYTHON_VER_MINOR,
      tool.name,
    );
    if (py.level !== 'ready') advice.push(py);

    let topdownWhl = await probeWhl(
      engine,
      deployPaths.topdown,
      TOPDOWN_TOOL_NAME,
    );
    if (topdownWhl.level !== 'ready') advice.push(topdownWhl);

    let cmnToolsWhl = await probeWhl(
      engine,
      deployPaths.cmnTools,
      CMN_DISCOVER_NAME,
    );
    if (cmnToolsWhl.level !== 'ready') advice.push(cmnToolsWhl);

    let cmnVisualizerWhl = await probeWhl(
      engine,
      deployPaths.cmnVisualizer,
      CMN_VISUALIZER_NAME,
    );
    if (cmnVisualizerWhl.level !== 'ready') advice.push(cmnVisualizerWhl);

    let cmnDriver = await probeCmnPerfDriver(engine);
    if (cmnDriver.level !== 'ready') advice.push(cmnDriver);

    let perfEventParanoid = await probePerfEventParanoidSetting(engine);
    if (perfEventParanoid.level !== 'ready') advice.push(perfEventParanoid);

    return {
      available: advice.length === 0,
      capabilities: {},
      advice,
    };
  },

  run: async (engine, ctx) => {
    await validatePerfEventParanoidSetting(engine);

    let workload =
      typeof ctx.getWorkload === 'function' ? ctx.getWorkload() : ctx.workload;
    let timeout = ctx.timeout > 0 ? ctx.timeout * 1000 : Infinity;

    const deployPaths = getDeployPaths(ctx);
    let tempRoot = await engine.createTempDir();
    let paths = buildPaths(tempRoot);
    /** @type {{outputDir:string, csvDir:string, runHandle?:any}} */
    let metadata = { outputDir: tempRoot, csvDir: paths.csvDir };
    ctx.metadata = metadata;

    await validateEnvironment(engine, paths.venvRoot, deployPaths);
    await getCMNTopologyJSON(engine, paths.venvRoot, paths.topology);
    await exportCMNMeshVisualizations(
      engine,
      paths.venvRoot,
      paths.topology,
      paths.meshSvgDir,
    );

    const samplingIntervalMs = resolveSamplingInterval(
      ctx.params?.samplingFreq,
    );
    const sampleDurationMs = resolveSampleDuration(ctx.params?.toolDuration);

    const collectionStartMs = Date.now();
    const workloadState = await launchWorkloadIfNeeded(engine, workload, '.');
    const deadline = collectionStartMs + timeout;

    await runTopdownSamplingLoop(engine, {
      workloadState,
      deadline,
      samplingIntervalMs,
      sampleDurationMs,
      collectionStartMs,
      metadata,
      paths,
    });

    await emitCsvArtifacts(engine, paths.csvDir);
  },

  reformat: async (engine, ctx) => {},

  onCancel: async (engine, ctx) => {
    await ctx.metadata.runHandle.kill();
  },

  onStop: async (engine, ctx) => {},
};

function resolveSamplingInterval(freq) {
  if (typeof freq !== 'string') {
    return SAMPLING_INTERVAL_MS.normal;
  }
  return SAMPLING_INTERVAL_MS[freq] ?? SAMPLING_INTERVAL_MS.normal;
}

function resolveSampleDuration(durationValue) {
  const parsed = Number(durationValue);
  if (Number.isFinite(parsed) && parsed > 0) {
    return parsed;
  }
  return DEFAULT_SAMPLE_DURATION_MS;
}

/**
 * Ensure the workload is still healthy, throwing if not.
 * @param state
 */
function ensureWorkloadHealthy(state) {
  if (typeof state.assertHealthy === 'function') {
    state.assertHealthy();
  }
}

/**
 * Run the sampling loop until the workload finishes or timeout occurs.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {{
 *   workloadState: {completed: () => boolean, update: () => void, assertHealthy?: () => void, handle: import("../recipes/docs/jsdocs").ProcessHandle | null},
 *   deadline: number,
 *   samplingIntervalMs: number,
 *   sampleDurationMs: number,
 *   collectionStartMs: number,
 *   metadata: any,
 *   paths: {topology:string, csvDir:string, venvRoot:string},
 * }} config
 */
async function runTopdownSamplingLoop(engine, config) {
  let lastSampleEndedAt = 0;
  let topdownToolPath = `${config.paths.venvRoot}/bin/${TOPDOWN_TOOL_NAME}`;

  while (Date.now() < config.deadline && !config.workloadState.completed()) {
    if (lastSampleEndedAt) {
      const waitFor =
        lastSampleEndedAt + config.samplingIntervalMs - Date.now();
      if (waitFor > 0) {
        await sleep(waitFor);
      }
    }

    lastSampleEndedAt = await collectTopdownSample(engine, {
      topdownToolPath,
      topologyPath: config.paths.topology,
      csvDirectory: config.paths.csvDir,
      sampleDurationMs: config.sampleDurationMs,
      metadata: config.metadata,
      collectionStartMs: config.collectionStartMs,
    });

    // Check the workload is still running, and propagate status changes after each sample.
    ensureWorkloadHealthy(config.workloadState);
    config.workloadState.update();
  }

  // A final of the workload status.
  // This propagates the status of the workload if it died before this while loop started.
  ensureWorkloadHealthy(config.workloadState);
}

/**
 * Run a topdown-tool sample and handle timeout/renaming behavior.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {{topdownToolPath:string, topologyPath:string, csvDirectory:string, sampleDurationMs:number, metadata:Object, collectionStartMs:number}} opts
 * @returns {Promise<number>} timestamp when the sample finished
 */
async function collectTopdownSample(engine, opts) {
  //TODO: Do we need to distinguish HNS / HNF depending on if Graviton 4? It might be fine to just have both, like the below, as the topdown tool will ignore the ones missing anyways.
  const cmnMetrics = [
    // HNS Metrics
    'cmn_hns_pocq_retry_rate', // POCQ Retry Rate
    'cmn_hns_pocq_retry_ratio',
    'cmn_hns_sf_hit_rate', // Snoop Filter Hit Rate
    'cmn_hns_sf_hit_ratio',
    'cmn_hns_slc_miss_rate', // System Level Cache Miss Rate
    'cmn_hns_slc_miss_ratio',
    // HNF Metrics
    'cmn_hnf_pocq_retry_rate',
    'cmn_hnf_pocq_retry_ratio',
    'cmn_hnf_sf_hit_rate',
    'cmn_hnf_sf_hit_ratio',
    'cmn_hnf_slc_miss_rate',
    'cmn_hnf_slc_miss_ratio',
  ];

  // Merge metrics into one comma separated string
  let metricsString = cmnMetrics[0];
  for (let i = 1; i < cmnMetrics.length; i++) {
    metricsString += `,${cmnMetrics[i]}`;
  }

  const processArgs = [
    opts.topdownToolPath,
    '--probe',
    'CMN',
    '--cmn-mesh-layout-input',
    opts.topologyPath,
    '--cmn-collect-by',
    'metric',
    '--cmn-metrics',
    metricsString,
    '--cmn-capture-per-device-id',
    '--cmn-csv-output',
    opts.csvDirectory,
  ];

  engine.log(
    'info',
    `Starting ${TOPDOWN_TOOL_NAME} with args ${processArgs.join(' ')}`,
  );

  let topdownHandle = await engine.startProcess(processArgs, {
    asPrivileged: true,
  });
  opts.metadata.runHandle = topdownHandle;

  let captureTimedOut = false;
  let killTimer = null;
  const stopTimer = setTimeout(() => {
    captureTimedOut = true;
    engine.log(
      'info',
      `Stopping ${TOPDOWN_TOOL_NAME} after ${opts.sampleDurationMs / 1000}s sample window`,
    );
    void topdownHandle.interrupt().catch(() => {});
    killTimer = setTimeout(() => {
      engine.log(
        'info',
        `Force killing ${TOPDOWN_TOOL_NAME} after waiting ${FORCE_KILL_GRACE_MS / 1000}s for graceful shutdown`,
      );
      void topdownHandle.kill().catch(() => {});
    }, FORCE_KILL_GRACE_MS);
  }, opts.sampleDurationMs);

  let topdownRunResult;
  try {
    topdownRunResult = await topdownHandle.wait();
  } finally {
    clearTimeout(stopTimer);
    if (killTimer) clearTimeout(killTimer);
  }

  if (captureTimedOut) {
    engine.log(
      'info',
      `${TOPDOWN_TOOL_NAME} sample stopped after ${opts.sampleDurationMs / 1000}s`,
    );
  }

  if (topdownRunResult.exitCode === 0) {
    engine.log('info', `${TOPDOWN_TOOL_NAME} module executed successfully`);
  } else if (captureTimedOut) {
    engine.log(
      'info',
      `${TOPDOWN_TOOL_NAME} exited with code ${topdownRunResult.exitCode} after timeout`,
    );
  } else {
    engine.log(
      'error',
      `${TOPDOWN_TOOL_NAME} exited with code ${topdownRunResult.exitCode}`,
    );
    throw {
      code: 'tool_integrations.cmn_analysis.TOOL_FAILED',
      metadata: { exitCode: topdownRunResult.exitCode },
    };
  }

  const sampleEndMs = Date.now();

  await normalizeRootOutputAccess(engine, opts.csvDirectory, true);

  await renameRawCsvFiles(
    engine,
    opts.csvDirectory,
    opts.collectionStartMs,
    false,
    sampleEndMs,
  );
  return sampleEndMs;
}

/**
 * Rename any raw cmn-*.csv output into the timestamped format.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} csvDirectory
 * @param {number} collectionStartMs
 * @param {boolean} skipIfEmpty
 * @param {number|undefined} sampleEndMs
 */
async function renameRawCsvFiles(
  engine,
  csvDirectory,
  collectionStartMs,
  skipIfEmpty,
  sampleEndMs,
) {
  const rawFiles = await listFiles(engine, `${csvDirectory}/cmn-*.csv`);
  if (!rawFiles.length) {
    if (skipIfEmpty) {
      return;
    }
    engine.log('error', `No CMN CSV files found in ${csvDirectory}`);
    throw {
      code: 'tool_integrations.cmn_analysis.RENAME_CSV_FAILED',
      metadata: { reason: 'no_csv_found' },
    };
  }
  const targetTimestamp =
    typeof sampleEndMs === 'number' ? sampleEndMs : Date.now();
  const elapsedMs = Math.max(0, targetTimestamp - collectionStartMs);

  for (const filePath of rawFiles) {
    const baseName = filePath.replace(/^.*\//, '');
    if (RENAMED_CSV_PATTERN.test(baseName)) {
      continue;
    }
    const match = baseName.match(RAW_CSV_PATTERN);
    if (!match) {
      engine.log('error', `Unexpected CMN CSV filename: ${baseName}`);
      throw {
        code: 'tool_integrations.cmn_analysis.RENAME_CSV_FAILED',
        metadata: { reason: 'unexpected_name' },
      };
    }

    const [, versionPart, meshPart] = match;
    const destinationName = `cmn${versionPart}_${meshPart}_${elapsedMs}.csv`;
    const destinationPath = `${csvDirectory}/${destinationName}`;
    const renameResult = await engine.execCommand(
      ['mv', filePath, destinationPath],
      {},
    );
    if (renameResult.rc !== 0) {
      engine.log(
        'error',
        `Failed to rename CMN CSV '${filePath}' to '${destinationPath}': ${renameResult.stderr}`,
      );
      throw {
        code: 'tool_integrations.cmn_analysis.RENAME_CSV_FAILED',
        metadata: { reason: 'rename_failed' },
      };
    }
  }
}

/**
 * Emit all CSV artifacts collected during the run.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} csvDirectory
 */
async function emitCsvArtifacts(engine, csvDirectory) {
  const csvFiles = await listFiles(engine, `${csvDirectory}/cmn*.csv`);
  if (!csvFiles.length) {
    engine.log('warn', 'No CMN CSV files found to emit');
    return;
  }

  for (const csvPath of csvFiles) {
    const baseName = csvPath.replace(/^.*\//, '');
    engine.emitOutput(csvPath, `cmn-csv-data/${baseName}`, {
      name: 'cmn-csv-data',
      version: '1.0',
    });
  }
}

/**
 * Utility for listing files that match a glob. Returns an empty array if no files match.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} globPattern
 * @param {boolean} asPrivileged
 * @returns {Promise<string[]>}
 */
async function listFiles(engine, globPattern, asPrivileged = false) {
  const result = await engine.execCommand(
    ['sh', '-c', `ls -1 ${globPattern} 2>/dev/null`],
    { asPrivileged },
  );
  if (result.rc !== 0 && !result.stdout.trim()) {
    return [];
  }
  return result.stdout.trim().split('\n').filter(Boolean);
}

/**
 * Used in tools in the CMN Analysis Recipe.
 * Checks that the CMN perf driver is installed and loaded.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeCmnPerfDriver(engine) {
  // Check module metadata exists (installed)
  const modinfo = await engine.execCommand(['sh', '-c', 'modinfo arm_cmn'], {});
  if (modinfo.rc !== 0) {
    engine.log('error', `modinfo arm_cmn failed: ${modinfo.stderr}`);
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message:
          'CMN perf driver not installed. Install `linux-modules-extra-`*`{uname -r}` and ensure `arm_cmn` is available.',
      },
    };
  }

  // Check dry-run load path resolution without elevated privileges.
  const modprobeDry = await engine.execCommand(
    ['sh', '-c', 'modprobe -n -v arm_cmn'],
    {},
  );
  if (modprobeDry.rc !== 0) {
    engine.log('error', `modprobe -n -v arm_cmn failed: ${modprobeDry.stderr}`);
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message:
          'CMN perf driver cannot be resolved by modprobe. Verify modules path matches the running kernel.',
      },
    };
  }

  const modProbe = await engine.execCommand(
    ['sh', '-c', 'modprobe arm_cmn'],
    {},
  );

  if (modProbe.rc !== 0) {
    engine.log('error', `modprobe arm_cmn failed: ${modProbe.stderr}`);
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message:
          'CMN perf driver cannot be resolved by modprobe. Verify modules path matches the running kernel.',
      },
    };
  }

  return {
    level: 'ready',
    messageCode: '',
  };
}

/**
 * Validate the environment is suitable for running the tools.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} venvPath
 * @param {{topdown:string, cmnTools:string, cmnVisualizer:string}} deployPaths
 */
async function validateEnvironment(engine, venvPath, deployPaths) {
  let createVenvResult = await engine.execCommand(
    ['python3', '-m', 'venv', venvPath],
    {},
  );
  if (createVenvResult.rc !== 0) {
    engine.log(
      'error',
      `Python venv creation failed for ${venvPath}: ${createVenvResult.stderr}`,
    );
    throw {
      code: 'tool_integrations.common.CREATE_PYTHON_VENV',
      metadata: {
        tool: tool.name,
        pythonVersion: '3.9',
        exitCode: createVenvResult.rc,
      },
    };
  }

  await ensureDeployed(engine, deployPaths.topdown, TOPDOWN_TOOL_NAME);

  await ensureDeployed(engine, deployPaths.cmnTools, CMN_DISCOVER_NAME);

  await ensureDeployed(engine, deployPaths.cmnVisualizer, CMN_VISUALIZER_NAME);

  let topdownInstallResult = await engine.execCommand(
    [`${venvPath}/bin/pip`, 'install', deployPaths.topdown],
    {},
  );
  if (topdownInstallResult.rc !== 0) {
    engine.log(
      'error',
      `Topdown Tool installation failed: ${topdownInstallResult.stderr}`,
    );
    throw {
      code: 'tool_integrations.common.INSTALL_MODULE',
      metadata: {
        tool: TOPDOWN_TOOL_NAME,
        exitCode: topdownInstallResult.rc,
        deployPath: deployPaths.topdown,
      },
    };
  }

  let cmnToolsInstallResult = await engine.execCommand(
    [`${venvPath}/bin/pip`, 'install', deployPaths.cmnTools],
    {},
  );
  if (cmnToolsInstallResult.rc !== 0) {
    engine.log(
      'error',
      `CMN Discover installation failed: ${cmnToolsInstallResult.stdout}`,
    );
    throw {
      code: 'tool_integrations.common.INSTALL_MODULE',
      metadata: {
        tool: CMN_DISCOVER_NAME,
        exitCode: cmnToolsInstallResult.rc,
        deployPath: deployPaths.cmnTools,
      },
    };
  }

  let cmnVisualizerInstallResult = await engine.execCommand(
    [`${venvPath}/bin/pip`, 'install', deployPaths.cmnVisualizer],
    {},
  );
  if (cmnVisualizerInstallResult.rc !== 0) {
    engine.log(
      'error',
      `CMN mesh visualizer installation failed: ${cmnVisualizerInstallResult.stderr}`,
    );
    throw {
      code: 'tool_integrations.common.INSTALL_MODULE',
      metadata: {
        tool: CMN_VISUALIZER_NAME,
        exitCode: cmnVisualizerInstallResult.rc,
        deployPath: deployPaths.cmnVisualizer,
      },
    };
  }

  let cmnTopologyTestCmd = [
    `${venvPath}/bin/python3`,
    '-m',
    'cmn_discover',
    '--help',
  ];
  let cmnTopologyTestResult = await engine.execCommand(cmnTopologyTestCmd, {});
  if (cmnTopologyTestResult.rc !== 0) {
    engine.log(
      'error',
      `CMN Discover tool failed: ${cmnTopologyTestResult.stderr}`,
    );
    throw {
      code: 'tool_integrations.cmn_analysis.PROBE_TOOL_FAILED',
      metadata: { tool: CMN_DISCOVER_NAME, exitCode: cmnTopologyTestResult.rc },
    };
  }

  let topdownTestCmd = [
    `${venvPath}/bin/topdown-tool`,
    '--probe',
    'CMN',
    '--help',
  ];
  let topdownTestResult = await engine.execCommand(topdownTestCmd, {
    asPrivileged: true,
  });
  if (topdownTestResult.rc !== 0) {
    engine.log('error', `Topdown Tool failed: ${topdownTestResult.stderr}`);
    throw {
      code: 'tool_integrations.cmn_analysis.PROBE_TOOL_FAILED',
      metadata: { tool: TOPDOWN_TOOL_NAME, exitCode: topdownTestResult.rc },
    };
  }

  let cmnVisualizerTestCmd = [`${venvPath}/bin/wperf-cmn-visualizer`, '--help'];
  let cmnVisualizerTestResult = await engine.execCommand(
    cmnVisualizerTestCmd,
    {},
  );
  if (cmnVisualizerTestResult.rc !== 0) {
    engine.log(
      'error',
      `CMN mesh visualizer failed: ${cmnVisualizerTestResult.stderr}`,
    );
    throw {
      code: 'tool_integrations.cmn_analysis.PROBE_TOOL_FAILED',
      metadata: {
        tool: CMN_VISUALIZER_NAME,
        exitCode: cmnVisualizerTestResult.rc,
      },
    };
  }
}

/**
 * Evaluate whether the target perf_event_paranoid setting is compatible with
 * CMN discovery. Note that CMN discovery requires BOTH elevated privileges and
 * perf_event_paranoid <= 0.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<{compatible: boolean, value: string}>}
 */
async function evaluatePerfEventParanoidSetting(engine) {
  const perfSetting = await getPerfSetting(engine, tool.name);

  return {
    compatible: perfSetting !== null && perfSetting <= 0,
    value: perfSetting === null ? 'unknown' : String(perfSetting),
  };
}

/**
 * Report probe advice for the target perf_event_paranoid setting.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probePerfEventParanoidSetting(engine) {
  let compatible;
  let value;
  try {
    ({ compatible, value } = await evaluatePerfEventParanoidSetting(engine));
  } catch (err) {
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message:
          'CMN analysis requires perf_event_paranoid <= 0, but the perf_event_paranoid value could not be determined. Ensure perf_event_paranoid is set to 0 or lower on the target and that it is readable.',
      },
    };
  }
  if (compatible) {
    return {
      level: 'ready',
      messageCode: '',
    };
  }

  return {
    level: 'error',
    messageCode: readinessMessageCode,
    metadata: {
      message: `CMN analysis requires perf_event_paranoid <= 0, but the reported perf_event_paranoid value is ${value}. Ensure perf_event_paranoid is set to 0 or lower on the target.`,
    },
  };
}

/**
 * Validate the target perf_event_paranoid setting is compatible with CMN
 * discovery, throwing if not.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<void>}
 */
async function validatePerfEventParanoidSetting(engine) {
  const { compatible, value } = await evaluatePerfEventParanoidSetting(engine);
  if (compatible) {
    engine.log(
      'info',
      `CMN analysis target perf_event_paranoid setting is compatible. perf_event_paranoid=${value}`,
    );
    return;
  }

  engine.log(
    'error',
    `CMN analysis requires perf_event_paranoid <= 0, but found ${value}. Ensure perf_event_paranoid is set to 0 or lower on the target.`,
  );
  throw {
    code: 'tool_integrations.cmn_analysis.INVALID_PERF_EVENT_PARANOID',
    metadata: { value },
  };
}

/**
 * Run the cmn-tools cmn_discover script to get the topology of the CMN in JSON format. Emit that back as a run artifact.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} venvPath
 * @param {string} topologyPath
 * @returns {Promise<void>}
 */
async function getCMNTopologyJSON(engine, venvPath, topologyPath) {
  engine.log('info', `Getting CMN Topology JSON using ${CMN_DISCOVER_NAME}`);
  const cmnTopologyArgs = [
    `${venvPath}/bin/python3`,
    '-m',
    'cmn_discover',
    '--overwrite',
    '--output',
    topologyPath,
  ];

  let cmnTopologyResult = await engine.execCommand(cmnTopologyArgs, {
    asPrivileged: true,
  });
  if (cmnTopologyResult.rc !== 0) {
    engine.log('error', `CMN Discover failed: ${cmnTopologyResult.stderr}`);
    throw {
      code: 'tool_integrations.cmn_analysis.CMN_DISCOVER_FAILED',
      metadata: { exitCode: cmnTopologyResult.rc },
    };
  }

  let topologyExistsResult = await engine.execCommand(
    ['stat', topologyPath],
    {},
  );
  if (topologyExistsResult.rc !== 0) {
    engine.log(
      'error',
      `Expected CMN topology output was not found at ${topologyPath}`,
    );
    throw {
      code: 'tool_integrations.cmn_analysis.TOPOLOGY_NOT_FOUND',
      metadata: {},
    };
  }

  await normalizeRootOutputAccess(engine, topologyPath, false);

  engine.emitOutput(topologyPath, 'cmn-topology.json', {
    name: 'cmn-topology-json',
    version: '1.0',
  });
}

/**
 * Run the CMN mesh visualizer against the discovered topology and emit the
 * generated SVG artifacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} venvPath
 * @param {string} topologyPath
 * @param {string} outputDir
 * @returns {Promise<void>}
 */
async function exportCMNMeshVisualizations(
  engine,
  venvPath,
  topologyPath,
  outputDir,
) {
  await engine.mkDir(outputDir);

  const cmnVisualizerArgs = [
    `${venvPath}/bin/wperf-cmn-visualizer`,
    '--topology',
    topologyPath,
    '--export-svg',
    outputDir,
  ];

  let cmnVisualizerResult = await engine.execCommand(cmnVisualizerArgs, {});
  if (cmnVisualizerResult.rc !== 0) {
    engine.log(
      'error',
      `CMN mesh visualizer failed: ${cmnVisualizerResult.stderr}`,
    );
    throw {
      code: 'tool_integrations.cmn_analysis.CMN_VISUALIZER_FAILED',
      metadata: { exitCode: cmnVisualizerResult.rc },
    };
  }

  const svgFiles = await listFiles(engine, `${outputDir}/*.svg`);
  if (!svgFiles.length) {
    engine.log('error', `No CMN mesh SVG files found in ${outputDir}`);
    throw {
      code: 'tool_integrations.cmn_analysis.VISUALIZATION_NOT_FOUND',
      metadata: {},
    };
  }

  for (const svgPath of svgFiles) {
    const baseName = svgPath.replace(/^.*\//, '');
    engine.emitOutput(svgPath, `cmn-mesh-svg/${baseName}`, {
      name: 'cmn-mesh-svg',
      version: '1.0',
    });
  }
}

/**
 * Build commonly used paths under the integration temp root.
 * @param {string} root
 * @returns {{topology:string, csvDir:string, meshSvgDir:string, venvRoot:string}}
 */
function buildPaths(root) {
  const venvRoot = `${root}/td-venv`;
  return {
    topology: `${root}/cmn-topology.json`,
    csvDir: `${root}/cmn-csv`,
    meshSvgDir: `${root}/cmn-mesh-svg`,
    venvRoot: venvRoot,
  };
}
