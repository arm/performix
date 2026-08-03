// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  reformatJitdumps,
  immediateEmitJitdumpLogs,
  filterJitdumpAgentsForPid,
} = require('./jitdump.js');
const {
  ensureDeployed,
  getPerfSetting,
  isPerfCapable,
  probeDeployment,
  posixTestWorkload,
} = require('./utils.js');
const { getExecutableFromWorkload } = require('./workload');

let bundleVersion = '2.1.0';
let rc = 'build-1';
let readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';

const slAnalyzeToolName = 'sl-analyze';
const slRecordToolName = 'sl-record';

const jitdumpJvmVersion = '0.9.0';
const jitdumpJvmToolName = 'jitdump-jvm';

const dotnetAgentVersion = '0.9.0';
const dotnetAgentToolName = 'dotnet-agent';

/**
 * Resolve deployment paths for the current engine locality.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {{slAnalyzeDeployPath: string, slRecordDeployPath: string, jitdumpJvmDeployPath: string, dotnetAgentDeployPath: string}}
 */
function getNeoprofPaths(engine) {
  const toolsRoot = engine.toolsRoot();
  const paths = {
    slAnalyzeDeployPath: `${toolsRoot}/${slAnalyzeToolName}/${bundleVersion}-${rc}/bin/`,
    slRecordDeployPath: `${toolsRoot}/${slRecordToolName}/${bundleVersion}-${rc}/bin/`,
    jitdumpJvmDeployPath: `${toolsRoot}/${jitdumpJvmToolName}/${jitdumpJvmVersion}/`,
    dotnetAgentDeployPath: `${toolsRoot}/${dotnetAgentToolName}/${dotnetAgentVersion}/`,
  };

  return paths;
}

/**
 * Creates a file as the non-root user and makes it readable by all users.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path
 * @returns {Promise<void>}
 */
async function createNonRootReadableFile(engine, path) {
  const touchResult = await engine.execCommand(['touch', path], {
    asPrivileged: false,
  });
  if (touchResult.rc === 0) {
    await engine.execCommand(['chmod', '644', path], {
      asPrivileged: false,
    });
  }
}

/**
 * @type {import("../recipes/docs/jsdocs").ToolIntegration}
 */
let tool = {
  name: 'neoprof',
  version: '1.1.0',
  supportsWorkloadLaunch: true,
  deployments: [
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool_bundle',
          name: slRecordToolName,
          version: `${bundleVersion}-${rc}`,
          requiredWhen: { type: 'always' },
        },
        {
          type: 'tool_bundle',
          name: slAnalyzeToolName,
          version: `${bundleVersion}-${rc}`,
          requiredWhen: { type: 'always' },
        },
        {
          type: 'tool_bundle',
          name: slAnalyzeToolName,
          version: `${bundleVersion}-${rc}`,
          requiredWhen: {
            type: 'param_is_set',
            parameters: [{ reformat_on_host: true }],
          },
          locality: 'host',
        },
        {
          type: 'tool_bundle',
          name: jitdumpJvmToolName,
          version: jitdumpJvmVersion,
          requiredWhen: {
            type: 'param_is_set',
            parameters: [{ collect_java_stacks: true }],
          },
        },
        {
          type: 'tool_bundle',
          name: dotnetAgentToolName,
          version: dotnetAgentVersion,
          requiredWhen: {
            type: 'param_is_set',
            parameters: [{ collect_dotnet_stacks: true }],
          },
        },
      ],
    },
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Android' }],
      dependencies: [
        {
          type: 'tool_bundle',
          name: slRecordToolName,
          version: `${bundleVersion}-${rc}`,
          requiredWhen: { type: 'always' },
        },
        {
          type: 'tool_bundle',
          name: slAnalyzeToolName,
          version: `${bundleVersion}-${rc}`,
          requiredWhen: { type: 'always' },
          locality: 'host',
        },
      ],
    },
  ],
  migrations: [
    // v1.1.0: Tool‐name rename from "streamline-cli" to "neoprof"
    {
      type: 'renameTool',
      from: 'streamline-cli',
      to: 'neoprof',
      version: '1.1.0',
    },
  ],
  description: {
    short:
      'Neoprof tool for performance analysis and profiling of applications.',
    long: 'The Neoprof tool is a command-line utility for performance analysis and profiling of applications. It provides insights into application behavior, resource utilization, and performance bottlenecks by collecting and analyzing various performance metrics.',
  },
  parameters: [
    {
      id: 'mode',
      label: 'Profiling mode',
      description:
        'Select the profiling mode used when invoking `sl-record`. Choose `samples` to collect CPU samples, `metrics` to gather counter metrics, or `spe` for Arm Statistical Profiling Extension data.',
      config: {
        type: 'radio',
        defaultValue: 'samples',
        options: [
          { value: 'samples', label: 'Samples' },
          { value: 'spe', label: 'SPE' },
          { value: 'metrics', label: 'Metrics' },
        ],
      },
    },
    {
      id: 'metrics_group',
      label: 'Metrics group',
      description:
        'Select the metrics groups to collect when running in metrics mode.',
      config: {
        type: 'input',
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
    {
      id: 'spe_workflow',
      label: 'SPE workflow',
      description:
        'Provide the SPE workflow string used when running in `spe` mode.',
      config: {
        type: 'input',
      },
    },
    {
      id: 'spe_sample_rate',
      label: 'SPE sample rate',
      description:
        'Specify the SPE periodic sampling rate (operations between each sample). Must be a non-zero positive integer.',
      config: {
        type: 'input',
      },
    },
    {
      id: 'collect_java_stacks',
      label: 'Collect Java stacks',
      description:
        'Enable collection of Java stack traces when profiling JVM workloads.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
    {
      id: 'collect_dotnet_stacks',
      label: 'Collect .NET stacks',
      description:
        'Enable collection of .NET stack traces when profiling .NET workloads.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
    {
      id: 'get_ipc_metric_name',
      label: 'IPC metric name',
      description: 'The name of the IPC metric.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
    {
      id: 'reformat_on_host',
      label: 'Reformat on host',
      description: 'Run analysis on the host instead of the target.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
  ],

  probe: async (engine, ctx) => {
    const neoprofAsPrivileged = await isPrivilegeRequired(engine, ctx);
    engine.log('info', `Neoprof privilege requirement: ${neoprofAsPrivileged}`);
    ctx.metadata.neoprofAsPrivileged = neoprofAsPrivileged;

    const result = {
      available: false,
      capabilities: {},
      advice: [],
    };

    const paths = getNeoprofPaths(engine);
    const recordDeploymentProbe = await probeDeployment(
      engine,
      paths.slRecordDeployPath,
      slRecordToolName,
    );
    const recordDeployed = recordDeploymentProbe.level === 'ready';
    if (!recordDeployed) {
      result.advice.push(recordDeploymentProbe);
    }

    const localisedEngine = ctx.params.reformat_on_host
      ? engine.withLocality('host')
      : engine;
    const localisedPaths = getNeoprofPaths(localisedEngine);
    const analyzeDeploymentProbe = await probeDeployment(
      localisedEngine,
      localisedPaths.slAnalyzeDeployPath,
      slAnalyzeToolName,
    );
    const analyzeDeployed = analyzeDeploymentProbe.level === 'ready';
    if (!analyzeDeployed) {
      result.advice.push(analyzeDeploymentProbe);
    }

    result.available = recordDeployed && analyzeDeployed;

    if (recordDeployed) {
      // Generate the initial probe report using sl-record
      const slRecordProbe = await probeSlRecord(engine, ctx);
      const probeResponse = JSON.parse(slRecordProbe.stdout);

      // Probe the IPC metric name
      let ipcMetricProbe = await probeIpcMetric(engine, ctx);
      if (ipcMetricProbe.level !== 'ready') {
        probeResponse.advice.push({
          message: ipcMetricProbe.message,
          severity: ipcMetricProbe.level,
        });
      }

      // Probe sl-analyze
      if (analyzeDeployed) {
        let slAnalyzeProbe = await probeSlAnalyze(localisedEngine, ctx);
        if (slAnalyzeProbe.level !== 'ready') {
          probeResponse.advice.push({
            message: slAnalyzeProbe.message,
            severity: slAnalyzeProbe.level,
          });
        }
        result.capabilities = {
          supports_strobing: probeResponse.supports_strobing,
          supports_event_inherit: probeResponse.supports_event_inherit,
        };
      }

      result.advice.push(
        ...probeResponse.advice.map((a) => {
          return {
            level: a.severity,
            messageCode: readinessMessageCode,
            metadata: { message: a.message },
          };
        }),
      );
    }

    // Probe jitdump-jvm
    if (ctx.params['collect_java_stacks']) {
      let jitdumpJvmProbe = await probeJitdumpJvm(engine, ctx);
      if (jitdumpJvmProbe.level !== 'ready') {
        result.advice.push(jitdumpJvmProbe);
      }
    }

    if (ctx.params['collect_dotnet_stacks']) {
      let dotnetAgentProbe = await probeDotnetAgent(engine, ctx);
      if (dotnetAgentProbe.level !== 'ready') {
        result.advice.push(dotnetAgentProbe);
      }
    }

    return result;
  },

  run: async (engine, ctx) => {
    const paths = getNeoprofPaths(engine);
    const jitdumpJvmDeployPath = paths.jitdumpJvmDeployPath;
    const dotnetAgentDeployPath = paths.dotnetAgentDeployPath;

    await ensureDeployed(engine, paths.slRecordDeployPath, slRecordToolName);

    const neoprofAsPrivileged = await isPrivilegeRequired(engine, ctx);
    engine.log('info', `Neoprof privilege requirement: ${neoprofAsPrivileged}`);
    ctx.metadata.neoprofAsPrivileged = neoprofAsPrivileged;

    // This is a workaround for the fact that we cannot control where the sl-record log file is written to.
    // The file is always written to the bin directory. If we could choose to write it to the run working
    // directory there wouldn't be a problem.
    // We create the file as a non-root user before running sl-record. If we didn't do this and neoprofAsPrivileged is true
    // then the file would get created with the owner as root. Then a subsequent run with asPrivileged false would not have
    // permission to write the file.
    // We also set the permissions to 644 to ensure the file is readable by ALL users, otherwise the copy created
    // in the capture.apc will be root owned when sl-record is ran as root and not readable by the (non-root) agent.
    await createNonRootReadableFile(
      engine,
      `${paths.slRecordDeployPath}/gator-log.txt`,
    );

    let outputDirectory = await engine.createTempDir();
    ctx.metadata.outputDirectory = outputDirectory;
    let captureDirectory = outputDirectory + '/capture.apc';
    ctx.metadata.captureDirectory = captureDirectory;

    let slRecordPath = paths.slRecordDeployPath + slRecordToolName;
    let ipcMetricName = null;

    if (ctx.params['get_ipc_metric_name']) {
      let ipcCommand = `${slRecordPath} --print counters`;
      let ipcResult = await engine.execCommand(
        [slRecordPath, '--print', 'counters'],
        { asPrivileged: ctx.metadata.neoprofAsPrivileged },
      );
      ipcMetricName =
        ipcResult.stdout.match(/\b\w*metric_ipc(?:_\w+)*\b/)?.[0] || null;

      if (!ipcMetricName) {
        engine.log(
          'warn',
          `Could not determine IPC metric name: rc=${ipcResult.rc}, stdout=${ipcResult.stdout.trim()}, stderr=${ipcResult.stderr.trim()}`,
        );
      }
    }

    let processArgs = [
      slRecordPath,
      '-o',
      ctx.metadata.captureDirectory,
      '-t',
      ctx.timeout.toString(),
      ...(ipcMetricName ? ['-C', ipcMetricName] : []),
      '--capture-log',
    ];

    let recordArgs = buildRecordArgs(ctx);
    processArgs.push(...recordArgs);
    engine.log(
      'info',
      `Launch env vars: ${JSON.stringify(ctx.workload.environment)}`,
    );

    if (!ctx.params.reformat_on_host) {
      emitAnalysisFiles(engine, ctx, captureDirectory);

      if (engine.isFullCaptureSupportEnabled()) {
        emitCaptureDir(engine, captureDirectory);
      }
    }

    // Agent support (.NET + JVM)
    //
    // Both agents generate jitdump files as the target process user (not necessarily root),
    // so the output directories must be writable by the session owner.

    let collectJavaStacks = ctx.params['collect_java_stacks'];
    let collectDotnetStacks = ctx.params['collect_dotnet_stacks'];

    if (ctx.workload.type === 'attach' && ctx.workload.pid != null) {
      const pid = ctx.workload.pid;
      const filterResult = await filterJitdumpAgentsForPid(
        engine,
        pid,
        ctx.metadata.neoprofAsPrivileged,
      );

      if (collectJavaStacks && !filterResult.isJvmPid) {
        engine.log(
          'warn',
          `Attach pid ${pid} does not appear to be a JVM process; skipping ${jitdumpJvmToolName}.`,
        );
      }
      if (collectDotnetStacks && !filterResult.isDotnetPid) {
        engine.log(
          'warn',
          `Attach pid ${pid} does not appear to be a .NET process; skipping ${dotnetAgentToolName}.`,
        );
      }

      collectJavaStacks = collectJavaStacks && filterResult.isJvmPid;
      collectDotnetStacks = collectDotnetStacks && filterResult.isDotnetPid;
    }

    const requireJitdumps = collectJavaStacks || collectDotnetStacks;

    /** @type {string | null} */
    let currUser = null;

    // Default .NET jitdump dir (used for `-o`). Some runtimes may emit jitdumps elsewhere
    // (e.g. via DOTNET_PerfMapJitDumpPath); staging will consolidate from all reported directories.
    let dotnetJitdumpDir = `${ctx.metadata.outputDirectory}/dotnet-jitdumps`;
    const dotnetActionsFile = `${ctx.metadata.outputDirectory}/dotnet-user-actions`;
    const jvmActionsFile = `${ctx.metadata.outputDirectory}/jvm-user-actions`;
    const jvmJitdumpDir = `${ctx.metadata.outputDirectory}/jvm-jitdumps`;

    if (requireJitdumps) {
      const results = await Promise.allSettled([
        resolveSessionOwner(engine),
        engine.makeWritable(outputDirectory, true),
      ]);

      if (results[0].status === 'fulfilled') {
        currUser = results[0].value;
        engine.log('info', `Current user (session owner) is ${currUser}`);
      } else {
        // If we can't determine the session owner, let later agent setup fail with a clearer error.
        engine.log(
          'warn',
          `Failed to determine session owner for agent output permissions: ${String(
            results[0].reason,
          )}`,
        );
      }

      const setupTasks = [];
      const chownTasks = [];

      if (collectJavaStacks) {
        setupTasks.push(
          () => assertJitdumpJvm(engine, ctx),
          () => engine.mkDir(jvmJitdumpDir),
        );
        if (currUser) {
          const user = currUser;
          chownTasks.push(() => engine.chown(jvmJitdumpDir, user, true));
        }
      }

      if (collectDotnetStacks) {
        setupTasks.push(
          () => assertDotnetAgent(engine, ctx),
          () => engine.mkDir(dotnetJitdumpDir),
        );
        if (currUser) {
          const user = currUser;
          chownTasks.push(() => engine.chown(dotnetJitdumpDir, user, true));
        }
      }

      await Promise.all(setupTasks.map((t) => t()));
      await Promise.all(chownTasks.map((t) => t()));
    }

    // .NET support
    if (collectDotnetStacks) {
      ctx.metadata.dotnetAgentAvailable = true;
      ctx.metadata.dotnetAgentDeployPath = dotnetAgentDeployPath;
      ctx.metadata.dotnetJitdumpDir = dotnetJitdumpDir;
      ctx.metadata.dotnetActionsFile = dotnetActionsFile;

      let dotnetAgentProcessArgs = [
        `${dotnetAgentDeployPath}/jitdump-dotnet`,
        '-o',
        dotnetJitdumpDir,
        `--user-actions-file`,
        dotnetActionsFile,
      ];
      dotnetAgentProcessArgs.push(...workloadToAgentHelperArgs(ctx.workload));

      engine.log(
        'info',
        `Starting ${dotnetAgentToolName} with args: ${dotnetAgentProcessArgs.join(' ')}`,
      );

      // Start capturing on .NET processes
      let dotnetAgentProcHandle = await engine.startProcess(
        dotnetAgentProcessArgs,
        {
          asPrivileged: ctx.metadata.neoprofAsPrivileged,
          environment: { DOTNET_EnableDiagnostics: '0' },
          stdout: {
            redirect: 'file',
            path: `${ctx.metadata.outputDirectory}/jitdumpdotnet.log`,
          },
          stderr: {
            redirect: 'file',
            path: `${ctx.metadata.outputDirectory}/jitdumpdotnet_stderr.txt`,
          },
        },
      );
      ctx.metadata.dotnetAgentProcHandle = dotnetAgentProcHandle;

      if (ctx.timeout > 0) {
        const dotnetAgentPid = dotnetAgentProcHandle.pid();
        if (!Number.isInteger(dotnetAgentPid) || dotnetAgentPid <= 0) {
          throw {
            code: 'common.UNKNOWN_ERROR',
            cause: `invalid dotnet-agent pid: ${String(dotnetAgentPid)}`,
          };
        }

        processArgs.push('--timeout-signal-pid', String(dotnetAgentPid));
        engine.log(
          'info',
          `sl-record timeout will signal dotnet-agent pid ${dotnetAgentPid}`,
        );
      }
    }

    // Java support
    if (collectJavaStacks) {
      // Ensure jitdump-jvm has a writable output directory
      //
      // Context:
      // The jitdump-jvm injects libjitdump_jvm_agent.so into the running JVM process,
      // inheriting the JVM's effective user ID (EUID) and file permissions.
      // The library generates a jitdump file under that EUID, so we must ensure
      // the session owner (the user launching the JVM) owns a writable directory
      // to allow successful file creation.
      //
      ctx.metadata.jitdumpJvmAvailable = true;
      ctx.metadata.jvmJitdumpDir = jvmJitdumpDir;
      ctx.metadata.jvmActionsFile = jvmActionsFile;

      // jitdump-jvm output directory setup is already performed under the `requireJitdumps` block above.
      // We rely on `resolveSessionOwner()` + `engine.makeWritable()` early in the run to ensure the
      // session owner can write jitdump files into `jvmJitdumpDir`.

      let jitdumpJvmProcessArgs = [
        `${jitdumpJvmDeployPath}/jitdump-jvm`,
        '--agent-path',
        `${jitdumpJvmDeployPath}/libjitdump_jvm_agent.so`,
        '-o',
        jvmJitdumpDir,
        `--user-actions-file`,
        jvmActionsFile,
      ];
      jitdumpJvmProcessArgs.push(...workloadToAgentHelperArgs(ctx.workload));

      engine.log(
        'info',
        `Starting ${jitdumpJvmToolName} with args: ${jitdumpJvmProcessArgs.join(' ')}`,
      );

      // Start capturing on JVM processes
      // It's important that this is started BEFORE neoprof so that
      // any JVM processes is captured from the very beginning
      let jitdumpJvmProcHandle = await engine.startProcess(
        jitdumpJvmProcessArgs,
        {
          asPrivileged: ctx.metadata.neoprofAsPrivileged,
          stdout: {
            redirect: 'file',
            path: `${ctx.metadata.outputDirectory}/jitdumpjvm.log`,
          },
          stderr: {
            redirect: 'file',
            path: `${ctx.metadata.outputDirectory}/jitdumpjvm_stderr.txt`,
          },
        },
      );
      ctx.metadata.jitdumpJvmProcHandle = jitdumpJvmProcHandle;
    }

    // Set the workload last so that workload command arguments are correctly passed down
    processArgs.push(...workloadToSLArgs(ctx.workload));

    let runSlRecordErr = false;
    try {
      await runSlRecord(engine, ctx, processArgs);
    } catch (err) {
      runSlRecordErr = true;
      throw err;
    } finally {
      try {
        await stopJitdumpAgents(engine, ctx, runSlRecordErr);
      } finally {
        immediateEmitSlRecordFiles(engine, ctx, captureDirectory);
        immediateEmitJitdumpLogs(engine, ctx);
      }
    }
  },

  reformat: async (engine, ctx) => {
    if (ctx.params.reformat_on_host) {
      return reformatOnHost(engine.withLocality('host'), ctx);
    }

    return reformatOnTarget(engine, ctx);
  },

  onCancel: async (engine, ctx) => {
    ctx.metadata.requestCancel = true;
    await stopDotnetAgent(engine, ctx);
    if (ctx.metadata.jitdumpJvmProcHandle) {
      await ctx.metadata.jitdumpJvmProcHandle.kill();
    }
    if (ctx.metadata.recordHandle) {
      ctx.metadata.recordHandle.kill();
    }
    if (ctx.metadata.analyzeHandle) {
      ctx.metadata.analyzeHandle.kill();
    }
  },

  onStop: async (engine, ctx) => {
    ctx.metadata.requestStop = true;
    await stopDotnetAgent(engine, ctx);
    if (ctx.metadata.recordHandle && ctx.metadata.allowStop) {
      await ctx.metadata.recordHandle.interrupt();
    }
    if (ctx.metadata.procHandle) {
      await ctx.metadata.procHandle.interrupt();
    }
  },
};

async function reformatOnHost(engine, ctx) {
  const paths = getNeoprofPaths(engine);
  const progressTrackerId = 'Analyzing collection';
  engine.startProgressTracker(progressTrackerId);

  //
  // Fetch capture.apc from target
  //

  const hostTempDirectory = await engine.createTempDir();
  const hostCaptureDirectory = hostTempDirectory + '/capture.apc';

  emitAnalysisFiles(engine, ctx, hostCaptureDirectory);

  if (engine.isFullCaptureSupportEnabled()) {
    emitCaptureDir(engine, hostCaptureDirectory);
  }

  await engine.mkDir(hostCaptureDirectory);
  await engine.copyFrom(
    'target',
    ctx.metadata.captureDirectory + '/**/*',
    hostCaptureDirectory + '/**/*',
  );

  //
  // Analysis phase 1 - database generation produces executable_paths.xml
  //

  let slAnalyzePath = paths.slAnalyzeDeployPath + slAnalyzeToolName;
  let args = [
    slAnalyzePath,
    '-o',
    hostCaptureDirectory,
    '--verbose',
    hostCaptureDirectory,
  ];

  await runSlAnalyze(engine, ctx, args, {
    stdoutLog: 'host_analysis_phase1.log',
    stderrLog: 'host_analysis_phase1_stderr.txt',
    progressTrackerId,
    asPrivileged: false,
  });

  //
  // Fetch images from target
  //

  const imagePaths = await parseExecutablePaths(engine, hostCaptureDirectory);
  await Promise.all(
    imagePaths.map((image) =>
      engine.copyFrom('target', image.sourcePath, image.destinationPath),
    ),
  );

  //
  // Analysis phase 2 - full analysis with images
  //

  args = buildAnalyzeArgs(engine, ctx, {
    slAnalyzePath,
    outputDirectory: hostCaptureDirectory,
    captureDirectory: hostCaptureDirectory,
    collectImages: false,
  });

  await runSlAnalyze(engine, ctx, args, {
    stdoutLog: 'host_analysis_phase2.log',
    stderrLog: 'host_analysis_phase2_stderr.txt',
    progressTrackerId,
    asPrivileged: false,
  });

  engine.endProgress(progressTrackerId);
}

async function reformatOnTarget(engine, ctx) {
  const paths = getNeoprofPaths(engine);
  const progressTrackerId = 'Analyzing collection';
  engine.startProgressTracker(progressTrackerId);

  await ensureDeployed(engine, paths.slAnalyzeDeployPath, slAnalyzeToolName);

  ctx.metadata.jitdumpsAvailable =
    ctx.metadata.jitdumpJvmAvailable || ctx.metadata.dotnetAgentAvailable;

  // Reformat any jitdump files generated during capture.
  // Do this before starting sl-analyze so that jitdumps are correctly placed in the APC directory.
  await reformatJitdumps(engine, ctx);

  if (ctx.metadata.jitdumpsAvailable && engine.isFullCaptureSupportEnabled()) {
    immediateEmitEnrichedJitdumps(
      engine,
      ctx.metadata.outputDirectory + '/capture.apc',
    );
  }

  let slAnalyzePath = paths.slAnalyzeDeployPath + slAnalyzeToolName;
  let args = buildAnalyzeArgs(engine, ctx, {
    slAnalyzePath,
    outputDirectory: ctx.metadata.captureDirectory,
    captureDirectory: ctx.metadata.captureDirectory,
    collectImages: true,
  });

  await runSlAnalyze(engine, ctx, args, {
    stdoutLog: 'analysis.log',
    stderrLog: 'analysis_stderr.txt',
    progressTrackerId,
    asPrivileged: ctx.metadata.neoprofAsPrivileged,
  });

  engine.endProgress(progressTrackerId);
}

function buildAnalyzeArgs(engine, ctx, options) {
  const args = [options.slAnalyzePath, '-o', options.outputDirectory];

  args.push(
    '--all-images',
    '--apap-export',
    '--group-by',
    'none',
    '--include-empty-columns',
    '--annotate-source',
    '--disassemble',
    '--all-jitdumps',
    '--verbose', // verbose gives us progress messages for updating the progress tracker, but we don't write these to the log file because they're noisy
  );

  if (options.collectImages) {
    args.push('--collect-images', '--collect-jitdumps');
  }

  if (engine.isNeoprofTimelineEnabled()) {
    args.push('--bin-durations', '1000000000');
  }

  if (ctx.workload.type === 'attach') {
    args.push(
      '--pid',
      ctx.workload.pid.toString(),
      '--include-child-processes',
    );
  }

  args.push(options.captureDirectory);
  return args;
}

async function runSlAnalyze(engine, ctx, args, options) {
  engine.log('info', `Starting sl-analyze with args: ${args.join(' ')}`);

  const outFileHandle = await createRunFile(engine, options.stdoutLog, {
    name: 'log-text',
    version: '1.0',
  });
  const errFileHandle = await createRunFile(engine, options.stderrLog, {
    name: 'log-text',
    version: '1.0',
  });
  const analyzeHandle = await engine.startProcess(args, {
    stdout: { redirect: 'stream' },
    stderr: { redirect: 'stream' },
    asPrivileged: options.asPrivileged,
  });
  ctx.metadata.analyzeHandle = analyzeHandle;

  if (ctx.metadata.requestCancel) {
    ctx.metadata.analyzeHandle.kill();
  }

  const stderrDrain = drainStreamToFileAndClose(
    errFileHandle,
    ctx.metadata.analyzeHandle.stderr,
  );
  const stdoutDrain = drainStreamToFileAndTrackProgress(
    outFileHandle,
    ctx.metadata.analyzeHandle.stdout,
    engine,
    options.progressTrackerId,
  );
  const [, result] = await Promise.all([
    Promise.all([stderrDrain, stdoutDrain]),
    ctx.metadata.analyzeHandle.wait(),
  ]);
  ctx.metadata.analyzeHandle = null;

  await checkToolFailureFromRun(
    engine,
    ctx,
    errFileHandle.path,
    null,
    result.exitCode,
    slAnalyzeToolName,
  );
}

/**
 * Checks that `sl-record` exists and runs on the target.
 * Invokes `sl-record --probe-report` to get probe advice.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<import("../recipes/docs/jsdocs").CommandResult>}
 */
async function probeSlRecord(engine, ctx) {
  const paths = getNeoprofPaths(engine);
  let slRecordPath = paths.slRecordDeployPath + slRecordToolName;
  const probeReportPath = paths.slRecordDeployPath + 'probe_report.json';
  let args = [slRecordPath, '--probe-report', '-o', 'platform_probe'];

  let recordArgs = buildRecordArgs(ctx);
  args.push(...recordArgs);

  // Set the workload last so that workload command arguments are correctly passed down
  args.push(...workloadToSLArgs(ctx.workload));

  // sl-record writes probe_report.json to the bin directory. Create it as the
  // non-root user first so a root probe cannot leave a stale root-owned report
  // that a later non-root probe cannot overwrite.
  await createNonRootReadableFile(engine, probeReportPath);

  let commandResult = await engine.execCommand(args, {
    asPrivileged: ctx.metadata.neoprofAsPrivileged,
  });

  await checkAndThrowNeoprofError(
    engine,
    ctx,
    commandResult.rc,
    commandResult.stderr,
    slRecordToolName,
  );

  let probeResult = await engine.execCommand(['cat', probeReportPath], {
    asPrivileged: ctx.metadata.neoprofAsPrivileged,
  });
  if (probeResult.rc !== 0) {
    throw {
      code: 'tool_integrations.neoprof.NEOPROF_FAILED',
      metadata: { tool: slRecordToolName, code: probeResult.rc },
      cause: `failed to fetch probe report`,
    };
  }

  return probeResult;
}

/**
 * Checks that IPC metric name can be gathered from the target.
 * This metric gets passed to sl-record later during the run.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeIpcMetric(engine, ctx) {
  const paths = getNeoprofPaths(engine);
  let slRecordPath = paths.slRecordDeployPath + slRecordToolName;
  let ipcMetricName = null;

  if (ctx.params['get_ipc_metric_name']) {
    let args = [slRecordPath, '--print', 'counters'];

    let ipcResult = await engine.execCommand(args, {
      asPrivileged: ctx.metadata.neoprofAsPrivileged,
    });
    ipcMetricName =
      ipcResult.stdout.match(/\b\w*metric_ipc(?:_\w+)*\b/)?.[0] || null;

    if (!ipcMetricName) {
      engine.log(
        'warn',
        `Could not determine IPC metric name: rc=${ipcResult.rc}, stdout=${ipcResult.stdout.trim()}, stderr=${ipcResult.stderr.trim()}`,
      );
      return {
        level: 'warning',
        message: `The name of the IPC metric for the target could not be determined. This means that the IPC metric will not be included in the profiling data collected.`,
      };
    }
  }

  return {
    level: 'ready',
    message: '',
  };
}

/**
 * Checks that `sl-analyze` exists and runs. The command may execute on the target or host, depending on the engine locality.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeSlAnalyze(engine, ctx) {
  const paths = getNeoprofPaths(engine);
  let slAnalyzePath = paths.slAnalyzeDeployPath + slAnalyzeToolName;
  let args = [slAnalyzePath, '--help'];

  let slAnalyzeCheck = await engine.execCommand(args, {});
  const glibcVersionNotFoundStr = /version `GLIBC_(\d+\.\d+)' not found/;
  let glibcMatch = slAnalyzeCheck.stderr.match(glibcVersionNotFoundStr);
  if (slAnalyzeCheck.rc !== 0 && glibcMatch) {
    const locality = engine.getLocality();
    engine.log(
      'error',
      `Incompatible version of glibc on ${locality} (${glibcMatch[1]}).`,
    );
    return {
      level: 'error',
      message: `The ${locality} is using a version of the GNU C Library which is incompatible with the '${tool.name}' tool. Upgrade the GNU C Library on the ${locality} machine to at least version GLIBC_${glibcMatch[1]}, or use a different ${locality} machine with a newer operating system.`,
    };
  }
  await checkAndThrowNeoprofError(
    engine,
    ctx,
    slAnalyzeCheck.rc,
    slAnalyzeCheck.stderr,
    slAnalyzeToolName,
  );

  return {
    level: 'ready',
    message: '',
  };
}

/**
 *  Runs sl-record with the given arguments.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {string[]} processArgs
 * @returns {Promise<void>}
 */
async function runSlRecord(engine, ctx, processArgs) {
  let errFilehandle = await createRunFile(engine, 'capture_log_err.txt', {
    name: 'log-text',
    version: '1.0',
  });
  /** @type {import("../recipes/docs/jsdocs").ProcessOptions} */
  let recordProcessOptions = {
    stdout: {
      redirect: 'file',
      path: `${ctx.metadata.outputDirectory}/capture_log.txt`,
    },
    stderr: { redirect: 'stream' },
    asPrivileged: ctx.metadata.neoprofAsPrivileged,
    environment: {
      ...(ctx.env || {}), // General tool environment
      // Workload-specified environment - for sl-record, these are just provided to the tool in the same way as the
      // env vars above, as there's currently no way to provide env vars specifically to the workload
      ...(ctx.workload.environment || {}),
    },
  };

  // These .NET runtime settings only affect processes launched with this
  // environment. In attach mode, the target process is already running and
  // cannot inherit them.
  if (ctx.params['collect_dotnet_stacks'] && ctx.workload.type === 'launch') {
    const dotnetEnvironment = {
      DOTNET_EnableWriteXorExecute: '0',
      DOTNET_JitFramed: '1',
      DOTNET_PerfMapJitDumpPath: ctx.metadata.dotnetJitdumpDir,
      // Required for runtimes that emit stub jitdump records in a shape
      // sl-analyze cannot consume unless stub granularity is explicit.
      DOTNET_PerfMapStubGranularity: '2',
    };

    recordProcessOptions.environment = {
      ...(recordProcessOptions.environment || {}),
      ...dotnetEnvironment,
    };
  }

  engine.log(
    'info',
    `Starting sl-record with args: ${processArgs.join(' ')}; options: ${JSON.stringify(recordProcessOptions)}`,
  );
  engine.startProgressTracker('Collecting data');

  let recordHandle = await engine.startProcess(
    processArgs,
    recordProcessOptions,
  );
  ctx.metadata.recordHandle = recordHandle;

  if (ctx.metadata.requestCancel) {
    ctx.metadata.recordHandle.kill();
  }

  // Wait for a minimum period before allowing the capture to be stopped.
  // If sl-record receives SIGINT immediately it fails to produce valid output and sl-analyze will fail.
  // This is a hacky workaround. There's no guarantee that sl-record will
  // have been running for sufficient time when this timeout expires.
  setTimeout(() => {
    ctx.metadata.allowStop = true;
    if (ctx.metadata.requestStop && ctx.metadata.recordHandle) {
      ctx.metadata.recordHandle.interrupt();
    }
  }, 2000);

  await drainStreamToFileAndClose(
    errFilehandle,
    ctx.metadata.recordHandle.stderr,
  );
  let result = await ctx.metadata.recordHandle.wait();
  ctx.metadata.recordHandle = null;

  await checkToolFailureFromRun(
    engine,
    ctx,
    errFilehandle.path,
    `${ctx.metadata.outputDirectory}/capture_log.txt`,
    result.exitCode,
    slRecordToolName,
  );
  engine.endProgress('Collecting data');
}

/**
 * Stops any running jitdump agents at the end of an sl-record run.
 * Errors encountered during stopping are not thrown if the sl-record run failed.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {boolean} runErr
 */
async function stopJitdumpAgents(engine, ctx, runErr) {
  // Attempt to stop both agents in parallel, even if one fails.
  const [jvmRes, dotnetRes] = await Promise.allSettled([
    stopJvmAgent(engine, ctx),
    stopDotnetAgent(engine, ctx),
  ]);

  const stopJvmAgentErr =
    jvmRes.status === 'rejected' ? jvmRes.reason : jvmRes.value;
  const stopDotnetAgentErr =
    dotnetRes.status === 'rejected' ? dotnetRes.reason : dotnetRes.value;

  if (!runErr) {
    if (stopJvmAgentErr) {
      throw stopJvmAgentErr;
    }
    if (stopDotnetAgentErr) {
      throw stopDotnetAgentErr;
    }
  }
}

/**
 * Converts workload configuration to streamline-cli arguments.
 * @param {import("../recipes/docs/jsdocs").WorkloadLaunch |
 * import("../recipes/docs/jsdocs").WorkloadAndroidLaunch |
 * import("../recipes/docs/jsdocs").WorkloadAttach |
 * import("../recipes/docs/jsdocs").WorkloadSystemWide} workload
 * @returns {string[]}
 */
function workloadToSLArgs(workload) {
  switch (workload.type) {
    case 'launch': {
      let args = [];
      if (workload.workingDir) {
        args.push('-w', workload.workingDir);
      }
      args.push('-A', ...workload.command);
      return args;
    }
    case 'attach':
      // sl-record uses system-wide capture during attach mode, to avoid file descriptor issues,
      // so we need to enable system-wide here as well. Filtering to just the target PID is done during
      // reformat by sl-analyze.
      return ['-S', 'yes'];
    case 'systemWide':
      return ['-S', 'yes'];
    case 'androidLaunch':
      return [
        '--android-pkg',
        workload.packageName,
        '--android-activity',
        workload.activityName,
      ];
    default:
      // This throw is fine as it will be scooped up into a SCRIPTED_STAGE_ERROR later (and there's no advice here anyway)
      throw new Error(
        `Invalid workload type '${/** @type {any} */ (workload).type}'`,
      );
  }
}

/**
 * Converts workload configuration to jitdump-jvm / dotnet-agent CLI arguments.
 * @param {import("../recipes/docs/jsdocs").WorkloadLaunch |
 * import("../recipes/docs/jsdocs").WorkloadAndroidLaunch |
 * import("../recipes/docs/jsdocs").WorkloadAttach |
 * import("../recipes/docs/jsdocs").WorkloadSystemWide} workload
 * @returns {string[]}
 */
function workloadToAgentHelperArgs(workload) {
  if (workload.type === 'attach') {
    return ['--pid', workload.pid.toString()];
  } else if (workload.type === 'systemWide') {
    return ['--attach-all'];
  }

  return [];
}

/**
 * Builds the arguments for sl-record based on the given parameters.
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {string[]}
 */
function buildRecordArgs(ctx) {
  let args = [];

  const mode = ctx.params['mode'];
  const samplingFrequency = ctx.params['sampling_frequency'];
  const metricsGroup = ctx.params['metrics_group'];
  const speWorkflow = ctx.params['spe_workflow'];
  const speSampleRate = ctx.params['spe_sample_rate'];

  if (mode === 'metrics') {
    if (!metricsGroup) {
      throw {
        code: 'tool_integrations.neoprof.MISSING_REQUIRED_PARAM',
        metadata: { param: 'metrics_group', mode: 'metrics' },
      };
    }
    if (!samplingFrequency) {
      throw {
        code: 'tool_integrations.neoprof.MISSING_REQUIRED_PARAM',
        metadata: { param: 'sampling_frequency', mode: 'metrics' },
      };
    }
    args.push('-M', metricsGroup);
    args.push('-r', samplingFrequency);
  } else if (mode === 'samples') {
    if (!samplingFrequency) {
      throw {
        code: 'tool_integrations.neoprof.MISSING_REQUIRED_PARAM',
        metadata: { param: 'sampling_frequency', mode: 'samples' },
      };
    }
    args.push('-r', samplingFrequency);
  } else if (mode === 'spe') {
    const speSampleRateStr =
      speSampleRate === undefined || speSampleRate === null
        ? ''
        : speSampleRate.toString().trim();

    if (speSampleRateStr.length > 0) {
      const parsedSpeSampleRate = Number(speSampleRateStr);
      if (
        !Number.isInteger(parsedSpeSampleRate) ||
        parsedSpeSampleRate <= 0 ||
        parsedSpeSampleRate > 16777215
      ) {
        throw {
          code: 'tool_integrations.neoprof.INVALID_SPE_SAMPLE_RATE',
          metadata: { value: speSampleRate },
        };
      }
      args.push('-F', speSampleRateStr);
    }

    if (speWorkflow) {
      args.push('-X', speWorkflow);
    }
  } else {
    throw {
      code: 'tool_integrations.neoprof.INVALID_MODE_PARAM',
      metadata: { value: mode },
    };
  }

  return args;
}

/**
 * Checks the stderr output of the neoprof tools for known error patterns and throws the relevant catalog error if matched.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {string} stdErr - the neoprof stderr
 * @param {number} exitCode - the exit code of the process
 */
async function checkAndThrowNeoprofError(engine, ctx, exitCode, stdErr, tool) {
  // ERR_MAP maps stderr patterns to catalog errors.
  // throwOnZeroExit: true => throw even when exitCode == 0 (defaults to false)
  const ERR_MAP = new Map([
    // m flag means ^ and $ match start and end of each line, rather than whole string
    [
      /^ERROR: The specified command does not exist or is not executable\. Please verify this executable exists\./m,
      {
        msgCode:
          'tool_integrations.common.WORKLOAD_NOT_EXIST_OR_NOT_EXECUTABLE',
        metadataProvider: (match) => ({
          workload: ctx.workload.rawCommand,
          executable: getExecutableFromWorkload(ctx.workload.command),
        }),
      },
    ],
    [
      /^ERROR: Failed to run command .*: Permission denied or is a directory$/m,
      {
        msgCode: 'tool_integrations.common.WORKLOAD_NOT_EXECUTABLE',
        metadataProvider: (match) => ({
          workload: ctx.workload.rawCommand,
          executable: getExecutableFromWorkload(ctx.workload.command),
        }),
      },
    ],
    [
      /^WARN: Nonexistent process, pid: .*\. Ensure process will exist on capture\.$/m,
      {
        msgCode: 'tool_integrations.neoprof.PID_NOT_EXIST',
        metadataProvider: (match) => ({
          pid: ctx.workload.pid.toString(),
        }),
      },
    ],
    [
      /^.*No file descriptors available$/m,
      {
        msgCode: 'tool_integrations.neoprof.FILE_DESCRIPTORS',
        metadataProvider: (match) => ({}),
      },
    ],
    [
      /^ERROR: Could not mmap perf buffer on cpu (?<cpuNum>\d+), 'Out of memory' \(errno: 12\) returned\.$/m,
      {
        msgCode: 'tool_integrations.neoprof.OUT_OF_MEMORY',
        throwOnZeroExit: true,
        metadataProvider: (match) => ({
          cpuNum: match[1],
        }),
      },
    ],
    [
      /No space left on device/m,
      {
        msgCode: 'tool_integrations.neoprof.INSUFFICIENT_DISK_SPACE',
        metadataProvider: (match) => ({
          outPath: ctx.metadata.outputDirectory,
        }),
      },
    ],
    [
      /^ERROR: Failed writing binary file\s+\S+/m,
      {
        msgCode: 'tool_integrations.neoprof.WRITE_FAILURE',
        metadataProvider: (match) => ({
          outPath: ctx.metadata.outputDirectory,
        }),
      },
    ],
    [
      /^ERROR: Unable to create directory\s+\S+/m,
      {
        msgCode: 'tool_integrations.neoprof.WRITE_FAILURE',
        metadataProvider: (match) => ({
          outPath: ctx.metadata.outputDirectory,
        }),
      },
    ],
    [
      /^ERROR: Error writing\s+\S+/m,
      {
        msgCode: 'tool_integrations.neoprof.WRITE_FAILURE',
        metadataProvider: (match) => ({
          outPath: ctx.metadata.outputDirectory,
        }),
      },
    ],
    // TODO: edit this catalog message to reference the user guide once more information on PMU counters and this issue exists
    //  see https://jira.arm.com/browse/APAP-2323
    [
      /^ERROR: Insufficient counters to collect metrics. Minimum of (?<minCounters>\d+?) counters required, found (?<actualCounters>\d+?) for cpu (?<cpuNum>\d+?)$/m,
      {
        msgCode: 'tool_integrations.neoprof.INSUFFICIENT_PMU_COUNTERS',
        metadataProvider: (match) => ({
          minCounters: match[1],
          actualCounters: match[2],
          cpuNum: match[3],
        }),
      },
    ],
    [
      /^WARN: Invalid value for --metric-group \((?<value>.+?)\):$/m,
      {
        msgCode: 'tool_integrations.neoprof.INVALID_METRICS_GROUP',
        metadataProvider: (match) => ({
          value: match[1],
        }),
      },
    ],
    [
      /^ERROR: SPE requested but the Arm SPE driver was not detected on this machine.$/m,
      {
        msgCode: 'tool_integrations.common.SPE_NOT_CONFIGURED',
        metadataProvider: (match) => ({}),
      },
    ],
    [
      /^ERROR: Failed to bind socket due to Address in use.*$/m,
      {
        msgCode: 'tool_integrations.neoprof.TARGET_IN_USE',
        metadataProvider: (match) => ({}),
      },
    ],
    [
      /^Exception -- Error, invalid jitdump record, required data is not present.$/m,
      {
        msgCode: 'tool_integrations.neoprof.JITDUMP_INVALID_RECORD',
        metadataProvider: (/** @type {any} */ match) => ({}),
      },
    ],
    [
      /^Skipping symbols.json export because backtrace report is empty$/m,
      {
        msgCode: 'tool_integrations.neoprof.NO_SAMPLES_COLLECTED',
        throwOnZeroExit: true,
        metadataProvider: (/** @type {any} */ match) => ({}),
      },
    ],
    [
      /No such file or directory$/m,
      {
        msgCode: 'tool_integrations.neoprof.WORKLOAD_FILE_NOT_FOUND',
        metadataProvider: (/** @type {any} */ match) => ({}),
      },
    ],
  ]);

  ERR_MAP.forEach((msgInfo, regex) => {
    if (exitCode === 0 && msgInfo.throwOnZeroExit !== true) {
      return;
    }
    let match = stdErr.match(regex);
    if (!match) {
      return;
    }
    // Known error message was found in stdErr output, throw error with corresponding code
    let metadata = msgInfo.metadataProvider(match);
    throw { code: msgInfo.msgCode, metadata: metadata };
  });

  if (exitCode !== 0) {
    // If no known error was found, return a generic error if still didn't complete successfully
    throw {
      code: 'tool_integrations.neoprof.NEOPROF_FAILED',
      metadata: { tool: tool, code: exitCode },
    };
  }
}

/**
 * Parses stderr and stdout for workload exit code/signal and surfaces it with context as user messages.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} stdErr
 * @param {string} stdOut
 * @returns {Promise<void>}
 */
async function emitNeoprofWorkloadExitCode(engine, stdErr, stdOut) {
  const exitCodeRegex = /^(?:ERROR: )?Command exited with code (\d+).*$/m;
  const stdErrExitCodeMatch = stdErr.match(exitCodeRegex);

  // Always surface the exit code if present (prefer stderr to avoid parsing stdOut).
  const exitCodeMatch = stdErrExitCodeMatch || stdOut.match(exitCodeRegex);
  if (exitCodeMatch && exitCodeMatch[1]) {
    const code = exitCodeMatch[1];
    const level = code === '0' ? 'info' : 'warn';
    engine.writeUserMessage(level, `Workload exit code: ${code}`);

    // Surface the nearest context line when the workload exits non-zero.
    if (stdErrExitCodeMatch && stdErrExitCodeMatch[1] !== '0') {
      const lines = stdErr.split('\n');
      const exitIdx = lines.findIndex(
        (line) => line === stdErrExitCodeMatch[0],
      );
      let contextLine = '';
      if (exitIdx > 0) {
        for (let i = exitIdx - 1; i >= 0; i -= 1) {
          if (lines[i].trim()) {
            contextLine = lines[i].trim();
            break;
          }
        }
      }
      if (contextLine) {
        engine.writeUserMessage(
          'warn',
          `Workload exit context: ${contextLine}`,
        );
      }
    }
    return;
  }

  // Signal termination is mutually exclusive with exit-code termination.
  const exitSignalMatch = stdErr.match(
    /^ERROR: Command exited with signal (\d+).*$/m,
  );
  if (exitSignalMatch && exitSignalMatch[1]) {
    engine.writeUserMessage(
      'warn',
      `Workload exit signal: ${exitSignalMatch[1]}`,
    );
  }
}

/**
 * Reads tool stderr/stdout, emits workload exit messages, and throws the relevant catalog error for tool failures.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {string} stdErrPath
 * @param {string|null} stdOutPath
 * @param {number} exitCode
 * @param {string} tool
 * @returns {Promise<void>}
 */
async function checkToolFailureFromRun(
  engine,
  ctx,
  stdErrPath,
  stdOutPath,
  exitCode,
  tool,
) {
  let stdErr = await readHostFile(engine, stdErrPath);
  let stdOut = stdOutPath ? await readHostFile(engine, stdOutPath) : '';
  await emitNeoprofWorkloadExitCode(engine, stdErr, stdOut);
  await checkAndThrowNeoprofError(engine, ctx, exitCode, stdErr, tool);
}

/**
 * Create a file in the run directory.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path
 * @returns {Promise<import("../recipes/docs/jsdocs").FileHandle>}
 */
async function createRunFile(engine, path, meta) {
  try {
    return await engine.createRunFile(path, meta);
  } catch {
    throw {
      code: 'tool_integrations.neoprof.HOST_FILE_CREATE',
      metadata: { file: path },
    };
  }
}

/**
 * Read the contents of a file on the host.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path
 * @returns {Promise<string>}
 */
async function readHostFile(engine, path) {
  try {
    return await engine.readHostFile(path);
  } catch {
    engine.log(
      'warn',
      `Failed to read host file '${path}', falling back to generic error`,
    );
    return '';
  }
}

/**
 * Drains a stream to a file and closes the file when complete.
 * @param {import("../recipes/docs/jsdocs").FileHandle} handle
 * @param {import("../recipes/docs/jsdocs").StreamRedirect} stream
 * @returns {Promise<void>}
 */
async function drainStreamToFileAndClose(handle, stream) {
  try {
    await forAwait(stream, (chunk) => handle.append(chunk));
  } catch {
    throw {
      code: 'tool_integrations.neoprof.WRITE_STREAM',
      metadata: { file: handle.path },
    };
  } finally {
    await handle.close().catch(() => {});
  }
}

/**
 * Drains a stream to a file and closes the file when complete. Parses "Progress" lines and updates the progress tracker.
 * @param {import("../recipes/docs/jsdocs").FileHandle} handle
 * @param {import("../recipes/docs/jsdocs").StreamRedirect} stream
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} trackerId
 * @returns {Promise<void>}
 */
async function drainStreamToFileAndTrackProgress(
  handle,
  stream,
  engine,
  trackerId,
) {
  if (!stream) {
    await handle.close().catch(() => {});
    return;
  }

  let buffer = '';
  let lastProgress = null;
  let lastMessage = null;

  const processLine = async (rawLine, suffix = '') => {
    let line = rawLine;
    if (line.endsWith('\r')) {
      line = line.slice(0, -1);
    }

    const progressRegex = /^Progress: (.+): (\d+)% /;
    const match = line.match(progressRegex);
    if (!match) {
      await handle.append(rawLine + suffix);
      return;
    }

    const message = match[1].trim();
    const percent = Number.parseFloat(match[2]);
    if (Number.isNaN(percent)) {
      await handle.append(rawLine + suffix);
      return;
    }

    if (message === lastMessage && percent === lastProgress) {
      return;
    }

    lastMessage = message;
    lastProgress = percent;
    try {
      await engine.updateProgress(trackerId, message, percent);
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      engine.log(
        'warn',
        `Failed to update analysis progress tracker '${trackerId}': ${reason}`,
      );
    }
  };

  try {
    await forAwait(stream, async (chunk) => {
      buffer += String(chunk);

      let newlineIndex = buffer.indexOf('\n');
      while (newlineIndex !== -1) {
        await processLine(buffer.slice(0, newlineIndex), '\n');
        buffer = buffer.slice(newlineIndex + 1);
        newlineIndex = buffer.indexOf('\n');
      }
    });

    if (buffer.length > 0) {
      await processLine(buffer);
    }
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    throw {
      code: 'tool_integrations.neoprof.WRITE_STREAM',
      metadata: { file: handle.path, reason: message },
    };
  } finally {
    await handle.close().catch(() => {});
  }
}

/**
 * Registers the capture directory artifacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function emitCaptureDir(engine, outputDir) {
  // Exclude files already emitted
  const exclude = [
    outputDir + '/0000000000',
    outputDir + '/captured.xml',
    outputDir + '/counters.xml',
    outputDir + '/events.xml',
    outputDir + '/gator-log.txt',
    outputDir + '/jitdumps/*',
  ];
  engine.emitOutput(
    outputDir + '/**/*',
    'capture.apc/**/*',
    {
      name: 'capture_apc',
      version: '1.0',
    },
    {
      exclude: exclude,
      backgroundTransfer: true,
    },
  );
}

function emitAnalysisFiles(engine, ctx, outputDir) {
  emitCommonFiles(engine, outputDir);
  emitDisassemblyFiles(engine, outputDir);

  let mode = ctx.params['mode'];
  if (mode === 'samples') {
    emitHotspotFiles(engine, outputDir);
  } else if (mode === 'spe') {
    emitSPEFiles(engine, outputDir);
  } else if (mode === 'metrics') {
    emitMetricsFiles(engine, outputDir);
  } else {
    throw {
      code: 'tool_integrations.neoprof.INVALID_MODE_PARAM',
      metadata: { value: mode },
    };
  }

  if (engine.isNeoprofTimelineEnabled()) {
    emitNeoprofTimelineFiles(engine, outputDir);
  }
}

/**
 * Registers artefacts which are safe to start transferring as soon as sl-record completes.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function immediateEmitSlRecordFiles(engine, ctx, outputDir) {
  engine.emitOutput(
    outputDir + '/gator-log.txt',
    'gator-log.txt',
    {
      name: 'log-text-debug',
      version: '1.0',
    },
    {
      immediateRetrieval: true,
    },
  );
  engine.emitOutput(
    outputDir + '/../capture_log.txt',
    'capture_log.txt',
    {
      name: 'log-text',
      version: '1.0',
    },
    {
      immediateRetrieval: true,
    },
  );
  if (engine.isFullCaptureSupportEnabled()) {
    // Emit capture.apc outputs that are available as soon as sl-record runs. Remaining capture.apc files
    // will be output once sl-analyze finishes
    engine.emitOutput(
      outputDir + '/0000000000',
      'capture.apc/0000000000',
      {
        name: 'capture_apc',
        version: '1.0',
      },
      {
        immediateRetrieval: true,
        backgroundTransfer: true,
      },
    );
    engine.emitOutput(
      outputDir + '/captured.xml',
      'capture.apc/captured.xml',
      {
        name: 'capture_apc',
        version: '1.0',
      },
      {
        immediateRetrieval: true,
        backgroundTransfer: true,
      },
    );
    engine.emitOutput(
      outputDir + '/counters.xml',
      'capture.apc/counters.xml',
      {
        name: 'capture_apc',
        version: '1.0',
      },
      {
        immediateRetrieval: true,
        backgroundTransfer: true,
      },
    );
    engine.emitOutput(
      outputDir + '/events.xml',
      'capture.apc/events.xml',
      {
        name: 'capture_apc',
        version: '1.0',
      },
      {
        immediateRetrieval: true,
        backgroundTransfer: true,
      },
    );
    engine.emitOutput(
      outputDir + '/gator-log.txt',
      'capture.apc/gator-log.txt',
      {
        name: 'capture_apc',
        version: '1.0',
      },
      {
        immediateRetrieval: true,
        backgroundTransfer: true,
      },
    );
  }
}

/**
 * Registers capture.apc jitdump artefacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function immediateEmitEnrichedJitdumps(engine, outputDir) {
  engine.emitOutput(
    outputDir + '/jitdumps/*',
    'capture.apc/jitdumps/*',
    {
      name: 'capture_apc',
      version: '1.0',
    },
    {
      immediateRetrieval: true,
      backgroundTransfer: true,
    },
  );
}

/**
 * Registers the common artifacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function emitCommonFiles(engine, outputDir) {
  engine.emitOutput(outputDir + '/symbols.json', 'output/symbols.json', {
    name: 'sl-collect-symbols',
    version: '1.1',
  });
  engine.emitOutput(outputDir + '/db/state.xml', 'state.xml', {
    name: 'state',
    version: '1.1',
  });
  engine.emitOutput(
    outputDir + '/sources-capture-periodic_sampling*',
    'output/sources-capture-periodic_sampling*',
    { name: 'sl-collect-source-line-attribution', version: '1.0' },
  );
  // applications.xml contains processes and threads info
  engine.emitOutput(outputDir + '/db/applications.xml', 'applications.xml', {
    name: 'applications',
    version: '1.0',
  });
}

/**
 * Registers SPE-specific artifacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function emitSPEFiles(engine, outputDir) {
  // This file is not always available (see https://jira.arm.com/browse/NEOPROF-321) - if it's not produced at recipe run time, then let's
  // ensure that recipe run will fail
  engine.emitOutput(
    outputDir + '/functions-capture-spe.csv',
    'output/functions-capture-spe.csv',
    { name: 'sl-collect-functions-spe-csv', version: '1.1' },
  );
  engine.emitOutput(
    outputDir + '/symbols-spe.json',
    'output/symbols-spe.json',
    {
      name: 'sl-collect-symbols',
      version: '1.1',
    },
  );
}

/**
 * Registers hotspots-specific artifacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function emitHotspotFiles(engine, outputDir) {
  engine.emitOutput(
    outputDir + '/call_tree_samples.json',
    'output/call_tree_samples.json',
    { name: 'sl-collect-call-tree', version: '1.0' },
  );
  engine.emitOutput(
    outputDir + '/callpath_self_samples.json',
    'output/callpath_self_samples.json',
    { name: 'sl-collect-metrics', version: '1.0' },
  );
  engine.emitOutput(
    outputDir + '/callpath_total_samples.json',
    'output/callpath_total_samples.json',
    { name: 'sl-collect-metrics', version: '1.0' },
  );
  engine.emitOutput(
    outputDir + '/callpaths-capture-periodic_sampling.csv',
    'output/callpaths-capture-periodic_sampling.csv',
    { name: 'sl-collect', version: '1.0' },
  );
  engine.emitOutput(
    outputDir + '/functions-capture-periodic_sampling.csv',
    'output/functions-capture-periodic_sampling.csv',
    { name: 'sl-collect-flat-functions-csv', version: '1.1' },
  );
}

/**
 * Registers metrics-specific artifacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function emitMetricsFiles(engine, outputDir) {
  engine.emitOutput(outputDir + '/call_tree.json', 'output/call_tree.json', {
    name: 'sl-collect-call-tree',
    version: '1.0',
  });
  engine.emitOutput(
    outputDir + '/callpath_self_metrics.json',
    'output/callpath_self_metrics.json',
    { name: 'sl-collect-metrics', version: '1.0' },
  );
  engine.emitOutput(
    outputDir + '/callpath_total_metrics.json',
    'output/callpath_total_metrics.json',
    { name: 'sl-collect-metrics', version: '1.0' },
  );
  engine.emitOutput(
    outputDir + '/callpaths-capture-metrics.csv',
    'output/callpaths-capture-metrics.csv',
    { name: 'sl-collect', version: '1.0' },
  );
  engine.emitOutput(
    outputDir + '/functions-capture-metrics.csv',
    'output/functions-capture-metrics.csv',
    { name: 'sl-collect-flat-functions-csv', version: '1.1' },
  );
}

/**
 * Registers disassembly artifacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function emitDisassemblyFiles(engine, outputDir) {
  engine.emitOutput(
    outputDir + '/disassembly-capture-periodic_sampling*',
    'output/disassembly-capture-periodic_sampling*',
    { name: 'disassembly_capture_samples', version: '1.1' },
  );
  engine.emitOutput(
    outputDir + '/disassembly-capture-metrics*',
    'output/disassembly-capture-metrics*',
    { name: 'disassembly_capture_metrics', version: '1.1' },
  );
}

/**
 * Registers neoprof timeline artifacts.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputDir
 * @returns {void}
 */
function emitNeoprofTimelineFiles(engine, outputDir) {
  engine.emitOutput(
    outputDir + '/report-new/apx/metadata/capture_metadata.parquet',
    'output/parquet/metadata/capture_metadata.parquet',
    {
      name: 'timeline-capture-metadata',
      version: '1.0',
    },
  );
  engine.emitOutput(
    outputDir + '/report-new/apx/metadata/counter_series_metadata.parquet',
    'output/parquet/metadata/counter_series_metadata.parquet',
    {
      name: 'timeline-counter-series-metadata',
      version: '1.0',
    },
  );
  engine.emitOutput(
    outputDir + '/report-new/apx/metadata/devices.parquet',
    'output/parquet/metadata/devices.parquet',
    {
      name: 'timeline-devices-metadata',
      version: '1.0',
    },
  );
  engine.emitOutput(
    outputDir + '/report-new/apx/metadata/processes.parquet',
    'output/parquet/metadata/processes.parquet',
    {
      name: 'timeline-processes-metadata',
      version: '1.0',
    },
  );
  engine.emitOutput(
    outputDir + '/report-new/apx/metadata/threads.parquet',
    'output/parquet/metadata/threads.parquet',
    {
      name: 'timeline-threads-metadata',
      version: '1.0',
    },
  );
  engine.emitOutput(
    outputDir + '/report-new/apx/timeline/counter_series_files.parquet',
    'output/parquet/timeline/counter_series_files.parquet',
    {
      name: 'timeline-counter-series-files-metadata',
      version: '1.0',
    },
  );
  engine.emitOutput(
    outputDir +
      '/report-new/apx/timeline/series_id=*/bin_duration=*/counter.parquet',
    'output/parquet/timeline/series_id=*/bin_duration=*/counter.parquet',
    {
      name: 'timeline-counter-series-binned-deltas',
      version: '1.0',
    },
  );
}

/**
 * Checks jitdump-jvm is available on the target machine,
 * either returning advice or throwing an error based on throwOnError.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {boolean} throwOnError
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function checkJvmAgent(engine, ctx, throwOnError) {
  // Ideally we'd get this info via the exposed APIs on engine/ctxs
  // For now, this is fine, but refactor when those APIs are available
  // TODO: See APAP-2528
  const paths = getNeoprofPaths(engine);
  const jitdumpJvmFile = paths.jitdumpJvmDeployPath + jitdumpJvmToolName;
  if (throwOnError) {
    await ensureDeployed(engine, jitdumpJvmFile, jitdumpJvmToolName);
    return { level: 'ready', messageCode: '' };
  }

  return await probeDeployment(engine, jitdumpJvmFile, jitdumpJvmToolName);
}

/**
 * Probes jitdump-jvm on the target machine.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeJitdumpJvm(engine, ctx) {
  return await checkJvmAgent(engine, ctx, false);
}

/**
 * Asserts jitdump-jvm is available on the target machine.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<void>}
 */
async function assertJitdumpJvm(engine, ctx) {
  await checkJvmAgent(engine, ctx, true);
}

/**
 * Checks the dotnet agent is available on the target machine,
 * either returning advice or throwing an error based on throwOnError.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {boolean} throwOnError
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function checkDotnetAgent(engine, ctx, throwOnError) {
  const paths = getNeoprofPaths(engine);
  const dotnetAgentFile = paths.dotnetAgentDeployPath + 'jitdump-dotnet';
  if (throwOnError) {
    await ensureDeployed(engine, dotnetAgentFile, dotnetAgentToolName);
    return { level: 'ready', messageCode: '' };
  }

  return await probeDeployment(engine, dotnetAgentFile, dotnetAgentToolName);
}

/**
 * Probes the dotnet agent on the target machine.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeDotnetAgent(engine, ctx) {
  return await checkDotnetAgent(engine, ctx, false);
}

/**
 * Asserts the dotnet agent is available on the target machine.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<void>}
 */
async function assertDotnetAgent(engine, ctx) {
  await checkDotnetAgent(engine, ctx, true);
}

/**
 * Tries to determine the session owner (current user) on the target machine.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<string>} The username of the session owner.
 */
async function resolveSessionOwner(engine) {
  const lognameCmd = await engine.execCommand(['logname'], {});
  if (lognameCmd.rc === 0) {
    return lognameCmd.stdout.trim();
  }

  // Fallback: SUDO_USER or USER (in that order)
  const envCmd = await engine.execCommand(['env'], {});
  if (envCmd.rc === 0) {
    const envLines = envCmd.stdout.split('\n');
    for (const key of ['SUDO_USER=', 'USER=']) {
      const line = envLines.find((l) => l.startsWith(key));
      if (line) {
        return line.substring(key.length).trim();
      }
    }
  }

  throw {
    code: 'tool_integrations.neoprof.SESSION_OWNER_NOT_FOUND',
    metadata: { lognameRc: lognameCmd.rc, envRc: envCmd.rc },
  };
}

/**
 * Interrupts an agent helper process and waits for it to exit.
 * @param {string} agentName - Used only for error messages (e.g. "jitdump-jvm", "dotnet-agent")
 * @param {import("../recipes/docs/jsdocs").ProcessHandle} procHandle
 * @returns {Promise<Error | null>}
 */
async function interruptAgent(agentName, procHandle) {
  if (!procHandle) {
    return null;
  }

  /** @type {unknown} */
  let interruptErr = null;
  try {
    await procHandle.interrupt();
  } catch (err) {
    interruptErr = err;
  }

  let result;
  try {
    result = await procHandle.wait();
  } catch (err) {
    const waitErr = err instanceof Error ? err : new Error(String(err));
    if (interruptErr) {
      const interruptMessage =
        interruptErr instanceof Error
          ? interruptErr.message
          : String(interruptErr);
      return new Error(
        `${agentName} interrupt failed: ${interruptMessage}; wait failed: ${waitErr.message}`,
      );
    }
    return waitErr;
  }

  // -1 indicates the process already stopped - safe to ignore
  if (result.exitCode != -1 && result.exitCode != 0) {
    return new Error(`${agentName} exited with code ${result.exitCode}`);
  }

  return null;
}

/**
 * Stops the jitdump-jvm process via interrupt and resets its process handle.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<import("../recipes/docs/jsdocs").CatalogMessage|null>}
 */
async function stopJvmAgent(engine, ctx) {
  if (!ctx.metadata.jitdumpJvmProcHandle) {
    return null;
  }

  let err = await interruptAgent(
    'jitdump-jvm',
    ctx.metadata.jitdumpJvmProcHandle,
  );

  if (err) {
    engine.log('warn', `jitdump-jvm stop reported: ${err.message}`);
    return {
      code: 'tool_integrations.neoprof.JITDUMP_JVM_STOP_FAILED',
      cause: `failed to stop jitdump-jvm: ${err.message}`,
    };
  }

  ctx.metadata.jitdumpJvmProcHandle = null;
  return null;
}

/**
 * Stops the dotnet-agent process via interrupt and resets its process handle.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<import("../recipes/docs/jsdocs").CatalogMessage|null>}
 */
async function stopDotnetAgent(engine, ctx) {
  if (ctx.metadata.dotnetAgentStopPromise) {
    return await ctx.metadata.dotnetAgentStopPromise;
  }

  if (!ctx.metadata.dotnetAgentProcHandle) {
    return null;
  }

  const dotnetAgentProcHandle = ctx.metadata.dotnetAgentProcHandle;
  ctx.metadata.dotnetAgentStopPromise = (async () => {
    let err = await interruptAgent('dotnet-agent', dotnetAgentProcHandle);

    if (err) {
      engine.log('warn', `dotnet-agent stop reported: ${err.message}`);
      return {
        code: 'tool_integrations.neoprof.DOTNET_AGENT_STOP_FAILED',
        cause: `failed to stop dotnet-agent: ${err.message}`,
      };
    }

    ctx.metadata.dotnetAgentProcHandle = null;
    return null;
  })();

  return await ctx.metadata.dotnetAgentStopPromise;
}

/**
 * Determines if neoprof requires privileged access to run sl-record, sl-analyze,
 * jitdump-jvm, and dotnet-agent on the target.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<boolean>}
 */
async function isPrivilegeRequired(engine, ctx) {
  if (ctx.workload.type === 'systemWide' || ctx.workload.type === 'attach') {
    engine.log(
      'info',
      `Workload type is ${ctx.workload.type}, privileged access is required.`,
    );
    return true;
  }

  // SPE physical-address capture currently requires privileged access.
  // TODO: Revisit this when adding PA support for the memory sharing recipe.
  if (ctx.params['mode'] === 'spe') {
    engine.log(
      'info',
      'SPE workflow requires physical address capture; privileged access is required.',
    );
    return true;
  }

  if (!(await posixTestWorkload(engine, ctx.workload, ['-r']))) {
    engine.log(
      'info',
      'The current user cannot read the workload; privileged access is required.',
    );
    return true;
  }

  if (!(await posixTestWorkload(engine, ctx.workload, ['-x']))) {
    engine.log(
      'info',
      'The current user cannot execute the workload; privileged access is required.',
    );
    return true;
  }

  // CAP_PERFMON (Linux 5.8+) and CAP_SYS_ADMIN (pre-5.8 fallback) allow
  // performance monitoring without routing the operation through the root worker.
  if (await isPerfCapable(engine)) {
    engine.log(
      'info',
      'Target platform grants performance monitoring capabilities, privileged access is not required.',
    );
    return false;
  }

  const perfSetting = await getPerfSetting(engine, tool.name);
  if (perfSetting === null) {
    engine.log(
      'info',
      'Target platform is not Linux or the perf_event_paranoid setting could not be determined. Assuming privileged access is required.',
    );
    return true;
  }

  if (perfSetting === -1 || perfSetting === 0) {
    engine.log(
      'info',
      `Target platform permits the required performance monitoring without privileged access. perf_event_paranoid=${perfSetting}`,
    );
    return false;
  }

  engine.log(
    'info',
    `Target platform performance monitoring restrictions require privileged access. perf_event_paranoid=${perfSetting}`,
  );
  return true;
}

async function parseExecutablePaths(engine, captureDirectory) {
  const xml = await readHostFile(
    engine,
    captureDirectory + '/db/executable_paths.xml',
  );
  const paths = new Map();
  const regex =
    /<path id="\d+" path="([^"]+)"(?: build_id="([^"]+)")? referenced="yes"\/>/g;
  let match;
  while ((match = regex.exec(xml)) !== null) {
    const pathAttribute = match[1];
    const buildId = match[2];
    if (!pathAttribute.startsWith('/')) {
      continue;
    }
    const sourcePath = pathAttribute
      .replace(/&quot;/g, '"')
      .replace(/&apos;/g, "'")
      .replace(/&lt;/g, '<')
      .replace(/&gt;/g, '>')
      .replace(/&amp;/g, '&');
    const imageName = sourcePath.split('/').pop();
    const existing = paths.get(imageName);
    if (existing) {
      if (existing.buildId !== buildId) {
        engine.log(
          'warn',
          `Skipping duplicate image '${sourcePath}' because '${existing.sourcePath}' already maps to '${imageName}'`,
        );
      }
      continue;
    }
    paths.set(imageName, {
      imageName,
      sourcePath,
      destinationPath: captureDirectory + '/images/' + imageName,
    });
  }
  return Array.from(paths.values());
}
