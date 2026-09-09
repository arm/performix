// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  probeDeployment,
  ensureDeployed,
  buildToolBundlePath,
  delay,
} = require('./utils');
const { launchWorkloadIfNeeded, validateWorkload } = require('./workload');
const { endProcess, promiseProcessExit } = require('./process-lifecycle');
const { isJvmProcessPid } = require('./jitdump');

const toolBundleName = 'jitdump-jvm';
const toolIntegrationVersion = '1.0.0';
const bundleVersion = '1.0.0';
const collectionProgressTracker = 'Collecting Java runtime data';
const defaultJfrSettings = 'profile';

const jvmAgentStartFailedCode =
  'tool_integrations.jitdump_jvm.JVM_AGENT_START_FAILED';
const jvmAgentWaitFailedCode =
  'tool_integrations.jitdump_jvm.JVM_AGENT_WAIT_FAILED';
const jvmAgentExitedCode = 'tool_integrations.jitdump_jvm.JVM_AGENT_EXITED';
const attachPidNotJvmCode = 'tool_integrations.jitdump_jvm.ATTACH_PID_NOT_JVM';

/**
 * @param {string} outputDir
 * @returns {string}
 * @description
 * Generates a recording name based on the output directory. The recording name
 * is derived from the last segment of the output directory path.
 */
function generateRecordingName(outputDir) {
  return outputDir.slice(outputDir.lastIndexOf('/') + 1);
}

/**
 * @param {string} outputDir
 * @returns {{
 *   outputDir: string,
 *   jfrOutputDir: string,
 *   recordingName: string,
 *   jvmAgentStdout: string,
 *   jvmAgentStderr: string,
 *   workloadStdout: string,
 *   workloadStderr: string,
 * }}
 */
function createRunArtifacts(outputDir) {
  return {
    outputDir,
    jfrOutputDir: `${outputDir}/jfr`,
    recordingName: generateRecordingName(outputDir),
    jvmAgentStdout: `${outputDir}/jitdump-jvm.log`,
    jvmAgentStderr: `${outputDir}/jitdump-jvm_stderr.txt`,
    workloadStdout: `${outputDir}/workload.log`,
    workloadStderr: `${outputDir}/workload_stderr.txt`,
  };
}

/**
 * Registers JVM agent and workload logs as run artifacts after the managed
 * processes have stopped, following the existing jitdump log publication
 * pattern used by the neoprof integration.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 */
function emitLogs(engine, ctx) {
  const logs = [];
  if (ctx.metadata.jvmAgentStarted) {
    logs.push(
      [ctx.metadata.jvmAgentStdout, 'jitdump-jvm.log'],
      [ctx.metadata.jvmAgentStderr, 'jitdump-jvm_stderr.txt'],
    );
  }
  if (ctx.metadata.workloadStarted) {
    logs.push(
      [ctx.metadata.workloadStdout, 'workload.log'],
      [ctx.metadata.workloadStderr, 'workload_stderr.txt'],
    );
  }

  for (const [source, destination] of logs) {
    engine.emitOutput(
      source,
      destination,
      { name: 'log-text', version: '1.0' },
      { immediateRetrieval: true },
    );
  }
}

/**
 * @param {string} recordingName
 * @param {string} jfrOutputDir
 * @param {string} settings
 * @returns {string}
 */
function buildWorkloadJFRArgs(recordingName, jfrOutputDir, settings) {
  return `-XX:StartFlightRecording=name=${recordingName},settings=${settings},filename=${jfrOutputDir},dumponexit=true`;
}

/**
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {string} jfrStartOption
 * @returns {Object.<string, string>}
 */
function buildLaunchEnvironment(ctx, jfrStartOption) {
  const environment = {
    ...(ctx.env || {}),
    ...(ctx.workload.type === 'launch' ? ctx.workload.environment || {} : {}),
  };
  environment.JDK_JAVA_OPTIONS = environment.JDK_JAVA_OPTIONS
    ? `${environment.JDK_JAVA_OPTIONS} ${jfrStartOption}`
    : jfrStartOption;
  return environment;
}

/**
 * @param {string} binaryPath
 * @param {import("../recipes/docs/jsdocs").Workload} workload
 * @param {string} jfrOutputDir
 * @param {string} recordingName
 * @returns {string[]}
 */
function buildJvmAgentArgs(binaryPath, workload, jfrOutputDir, recordingName) {
  validateWorkload(workload);
  const args = [binaryPath];
  if (workload.type === 'attach') {
    args.push('--pid', String(workload.pid));
  } else if (workload.type === 'systemWide') {
    args.push('--attach-all');
  }
  args.push(
    '--jfr-output-dir',
    jfrOutputDir,
    '--jfr-name',
    recordingName,
    '--jfr-settings',
    defaultJfrSettings,
  );
  return args;
}

/**
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 */
function initialiseState(ctx) {
  // Keep nested process states shared by reference across lifecycle hooks.
  ctx.metadata = { ...(ctx.metadata || {}) };
  ctx.metadata.requestStop = ctx.metadata.requestStop === true;
  ctx.metadata.requestCancel = ctx.metadata.requestCancel === true;
  ctx.metadata.jvmAgentProcess = null;
  ctx.metadata.workloadProcess = null;
  ctx.metadata.jvmAgentStarted = false;
  ctx.metadata.workloadStarted = false;
}

/**
 * @param {unknown} error
 * @returns {string}
 */
function errorMessage(error) {
  if (error instanceof Error) {
    return error.message;
  }
  if (error && typeof error === 'object' && 'cause' in error) {
    return String(error.cause);
  }
  return String(error);
}

function buildReformatArgs(binaryPath, jfrOutputDir, parquetOutputDir) {
  return [
    binaryPath,
    '--jfr-input-dir',
    jfrOutputDir,
    '--jfr-parquet-output-dir',
    parquetOutputDir,
  ];
}

/**
 * @type {import("../recipes/docs/jsdocs").ToolIntegration}
 */
const tool = {
  name: 'jitdump-jvm',
  version: toolIntegrationVersion,
  supportsWorkloadLaunch: true,
  description: {
    short: 'Collect Java Flight Recorder data from JVM workloads.',
    long: 'Starts and manages the jitdump-jvm JVM agent to capture Java Flight Recorder (JFR) data from Java workloads. Supports launching a new Java workload, attaching to an existing JVM process, or capturing system-wide data from all JVM processes.',
  },
  deployments: [
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool_bundle',
          name: toolBundleName,
          version: bundleVersion,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],

  probe: async (engine, ctx) => {
    const binaryPath = buildToolBundlePath(
      engine.toolsRoot(),
      toolBundleName,
      bundleVersion,
    );
    // Check that the tool bundle is deployed
    const deployAdvice = await probeDeployment(engine, binaryPath, tool.name);
    if (deployAdvice.level !== 'ready') {
      return { available: false, capabilities: {}, advice: [deployAdvice] };
    }

    if (ctx.workload?.type === 'attach') {
      const isJvm = await isJvmProcessPid(engine, ctx.workload.pid, false);
      if (!isJvm) {
        return {
          available: false,
          capabilities: {},
          advice: [
            {
              level: 'error',
              messageCode: attachPidNotJvmCode,
              metadata: { pid: String(ctx.workload.pid) },
            },
          ],
        };
      }
    }

    return { available: true, capabilities: {}, advice: [] };
  },

  run: async (engine, ctx) => {
    // Initialise
    initialiseState(ctx);
    validateWorkload(ctx.workload);
    const binaryPath = buildToolBundlePath(
      engine.toolsRoot(),
      toolBundleName,
      bundleVersion,
    );
    await ensureDeployed(engine, binaryPath, tool.name);
    if (ctx.metadata.requestStop || ctx.metadata.requestCancel) {
      return;
    }

    // Create output directories & metadata
    const outputDir = await engine.createTempDir();
    const artifacts = createRunArtifacts(outputDir);
    await engine.mkDir(artifacts.jfrOutputDir);
    await engine.makeWritable(outputDir, true);
    Object.assign(ctx.metadata, artifacts);

    engine.startProgressTracker(collectionProgressTracker);
    /** @type {Promise<any> | null} */
    let jvmAgentExit = null;
    /** @type {Promise<any> | null} */
    let workloadExit = null;
    try {
      const jvmAgentArgs = buildJvmAgentArgs(
        binaryPath,
        ctx.workload,
        artifacts.jfrOutputDir,
        artifacts.recordingName,
      );
      const jvmAgentProcessOptions = {
        stdout: {
          redirect: 'file',
          path: ctx.metadata.jvmAgentStdout,
        },
        stderr: {
          redirect: 'file',
          path: ctx.metadata.jvmAgentStderr,
        },
      };

      try {
        const handle = await engine.startProcess(
          jvmAgentArgs,
          jvmAgentProcessOptions,
        );
        const process = promiseProcessExit({
          handle,
          exitPromise: handle.wait(),
        });
        ctx.metadata.jvmAgentProcess = process;
        ctx.metadata.jvmAgentStarted = true;
        jvmAgentExit = process.exitPromise.then((outcome) => ({
          state: 'jvm_agent_exited',
          ...outcome,
        }));
      } catch (error) {
        throw {
          code: jvmAgentStartFailedCode,
          metadata: {},
          cause: errorMessage(error),
        };
      }

      if (ctx.metadata.requestCancel && ctx.metadata.jvmAgentProcess.handle) {
        await ctx.metadata.jvmAgentProcess.handle.kill();
      }

      if (!ctx.metadata.requestStop && !ctx.metadata.requestCancel) {
        if (ctx.workload.type === 'launch') {
          const jfrStartOption = buildWorkloadJFRArgs(
            ctx.metadata.recordingName,
            ctx.metadata.jfrOutputDir,
            defaultJfrSettings,
          );
          const workloadState = await launchWorkloadIfNeeded(
            engine,
            ctx.workload,
            artifacts.outputDir,
            {
              command: [...ctx.workload.command],
              captureLogs: false,
              processOptions: {
                stdout: {
                  redirect: 'file',
                  path: ctx.metadata.workloadStdout,
                },
                stderr: {
                  redirect: 'file',
                  path: ctx.metadata.workloadStderr,
                },
                workingDirectory: ctx.workload.workingDir,
                environment: buildLaunchEnvironment(ctx, jfrStartOption),
              },
            },
          );
          const process = promiseProcessExit({
            handle: workloadState.handle,
            exitPromise: workloadState.exitPromise,
          });
          ctx.metadata.workloadProcess = process;
          ctx.metadata.workloadStarted = true;
          workloadExit = process.exitPromise.then((outcome) => ({
            state: 'workload_exited',
            ...outcome,
          }));

          if (ctx.metadata.requestCancel && process.handle) {
            await process.handle.kill();
          }
        }

        if (!ctx.metadata.requestStop && !ctx.metadata.requestCancel) {
          const exits = [jvmAgentExit];
          if (workloadExit) {
            exits.push(workloadExit);
          }
          const timeoutWaiter =
            ctx.timeout > 0
              ? delay(ctx.timeout * 1000).then(() => ({ state: 'timed_out' }))
              : null;
          if (timeoutWaiter) {
            exits.push(timeoutWaiter);
          }
          const outcome = await Promise.race(exits);

          if (outcome.state === 'timed_out') {
            ctx.metadata.requestStop = true;
          } else if (outcome.state === 'workload_exited') {
            if ('error' in outcome) {
              throw {
                code: 'tool_integrations.common.WORKLOAD_WAIT_FAILED',
                metadata: {
                  workload: ctx.workload.rawCommand,
                  reason: errorMessage(outcome.error),
                },
                cause: errorMessage(outcome.error),
              };
            }
            if (
              outcome.result.exitCode !== 0 &&
              !ctx.metadata.requestStop &&
              !ctx.metadata.requestCancel
            ) {
              throw {
                code: 'tool_integrations.common.WORKLOAD_RUNTIME_FAILED',
                metadata: {
                  workload: ctx.workload.rawCommand,
                  exitCode: String(outcome.result.exitCode),
                  reason: 'non_zero_exit',
                },
              };
            }
          } else if (outcome.state === 'jvm_agent_exited') {
            if ('error' in outcome) {
              throw {
                code: jvmAgentWaitFailedCode,
                metadata: {},
                cause: errorMessage(outcome.error),
              };
            }
            if (!ctx.metadata.requestStop && !ctx.metadata.requestCancel) {
              throw {
                code: jvmAgentExitedCode,
                metadata: { exitCode: String(outcome.result.exitCode) },
              };
            }
          }
        }
      }
    } finally {
      try {
        if (ctx.metadata.requestCancel) {
          await Promise.all(
            [workloadExit, jvmAgentExit].filter((exit) => exit !== null),
          );
        } else {
          if (ctx.metadata.workloadProcess?.handle && workloadExit) {
            try {
              await endProcess(engine, ctx.metadata.workloadProcess);
            } catch (error) {
              engine.log(
                'warn',
                `Failed while stopping workload process ${ctx.metadata.workloadProcess.handle.pid()}: ${errorMessage(error)}`,
              );
            }
          }
          if (ctx.metadata.jvmAgentProcess?.handle && jvmAgentExit) {
            try {
              await endProcess(engine, ctx.metadata.jvmAgentProcess);
            } catch (error) {
              engine.log(
                'warn',
                `Failed to stop jitdump-jvm JVM agent process ${ctx.metadata.jvmAgentProcess.handle.pid()}: ${errorMessage(error)}`,
              );
            }
          }
        }
      } finally {
        // Logs are complete after the active processes have been signalled and
        // waited for, so publish them before reformatting begins.
        try {
          emitLogs(engine, ctx);
        } finally {
          engine.endProgress(collectionProgressTracker);
        }
      }
    }
  },

  reformat: async (engine, ctx) => {
    const binaryPath = buildToolBundlePath(
      engine.toolsRoot(),
      toolBundleName,
      bundleVersion,
    );

    const parquetOutputDir = `${ctx.metadata.outputDir}/parquet`;
    const reformatStdoutFile = `jitdump-jvm-reformat.log`;
    const reformatStdoutPath = `${ctx.metadata.outputDir}/${reformatStdoutFile}`;
    const reformatStderrFile = `jitdump-jvm-reformat_stderr.txt`;
    const reformatStderrPath = `${ctx.metadata.outputDir}/${reformatStderrFile}`;
    await ensureDeployed(engine, binaryPath, tool.name);
    await engine.mkDir(parquetOutputDir);

    const progressTrackerID = 'Converting Java Flight Recorder data';
    engine.startProgressTracker(progressTrackerID);

    let result;
    let handle;
    try {
      handle = await engine.startProcess(
        buildReformatArgs(
          binaryPath,
          ctx.metadata.jfrOutputDir,
          parquetOutputDir,
        ),
        {
          stdout: { redirect: 'file', path: reformatStdoutPath },
          stderr: { redirect: 'file', path: reformatStderrPath },
        },
      );
      result = await handle.wait();
    } catch (error) {
      throw {
        code: 'tool_integrations.jitdump_jvm.JFR_REFORMAT_FAILED',
        metadata: {},
        cause: errorMessage(error),
      };
    } finally {
      try {
        if (handle) {
          engine.emitOutput(
            reformatStdoutPath,
            reformatStdoutFile,
            { name: 'log-text', version: '1.0' },
            { immediateRetrieval: true },
          );
          engine.emitOutput(
            reformatStderrPath,
            reformatStderrFile,
            { name: 'log-text', version: '1.0' },
            { immediateRetrieval: true },
          );
        }
      } finally {
        engine.endProgress(progressTrackerID);
      }
    }

    if (result.exitCode !== 0) {
      throw {
        code: 'tool_integrations.jitdump_jvm.JFR_REFORMAT_FAILED',
        metadata: { exitCode: String(result.exitCode) },
      };
    }

    const requiredParquetFiles = [
      'metadata/jfr_recordings.parquet',
      'events/jfr_jvm_information.parquet',
      'events/jfr_initial_system_property.parquet',
      'events/jfr_gc_heap_summary.parquet',
      'events/jfr_garbage_collection.parquet',
    ];

    const missingComponents = [];
    for (const relativePath of requiredParquetFiles) {
      const outputPath = `${parquetOutputDir}/${relativePath}`;
      const check = await engine.execCommand(['stat', outputPath], {});
      if (check.rc !== 0) {
        missingComponents.push(relativePath);
      }
    }

    if (missingComponents.length > 0) {
      throw {
        code: 'tool_integrations.jitdump_jvm.JFR_REFORMAT_COMPONENTS_MISSING',
        metadata: { missingComponents: missingComponents.join(', ') },
      };
    }

    const jfrParquetComponent = { name: 'jfr-parquet', version: '1.0' };
    engine.emitOutput(
      `${parquetOutputDir}/**/*`,
      'parquet/**/*',
      jfrParquetComponent,
      { immediateRetrieval: true },
    );
  },

  onStop: async (engine, ctx) => {
    ctx.metadata = ctx.metadata || {};
    ctx.metadata.requestStop = true;
    if (ctx.metadata.workloadProcess?.handle) {
      try {
        await endProcess(engine, ctx.metadata.workloadProcess);
      } catch (error) {
        if (ctx.metadata.jvmAgentProcess?.handle) {
          await endProcess(engine, ctx.metadata.jvmAgentProcess);
        }
        throw error;
      }
    } else if (ctx.metadata.jvmAgentProcess?.handle) {
      await endProcess(engine, ctx.metadata.jvmAgentProcess);
    }
  },

  onCancel: async (engine, ctx) => {
    ctx.metadata = ctx.metadata || {};
    ctx.metadata.requestCancel = true;
    try {
      if (ctx.metadata.jvmAgentProcess?.handle) {
        await ctx.metadata.jvmAgentProcess.handle.kill();
      }
    } finally {
      if (ctx.metadata.workloadProcess?.handle) {
        await ctx.metadata.workloadProcess.handle.kill();
      }
    }
  },
};
