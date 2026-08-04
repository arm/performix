// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  probePython,
  probeDeployment,
  ensureDeployed,
  delay,
} = require('./utils');
const {
  createInsufficientSamplesError,
  getEligibleSampleCount,
  resolveSampleCollectionError,
  validateSampleWindow,
} = require('./sampling');
const { launchWorkloadIfNeeded } = require('./workload');

const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';
const invalidIntervalMessageCode =
  'tool_integrations.sysutil_timeline.INVALID_INTERVAL';
const numastatNotFoundMessageCode =
  'tool_integrations.sysutil_timeline.NUMASTAT_NOT_FOUND';

const PYTHON_VER_MAJOR = 3;
const PYTHON_VER_MINOR = 9;
const DEFAULT_INTERVAL_SECONDS = 1.0;
const MIN_INTERVAL_SECONDS = 0.01;
const MAX_INTERVAL_SECONDS = 60;
const INSUFFICIENT_SAMPLES_EXIT_CODE = 3;
const COLLECTOR_STOP_GRACE_PERIOD_MS = 2000;
const COLLECTOR_SAMPLE_COMPLETION_GRACE_PERIOD_MS = 10000;
const sampleRequirements = {
  tool: 'System Utilization',
  minimumSamples: 2,
};

const performixGlobal =
  /** @type {import("../recipes/docs/jsdocs").PerformixGlobal} */ (
    globalThis['performix']
  );
const toolBundleName = 'sysutil-timeline';
const bundleVersion = performixGlobal.engineVersion;
const toolIntegrationVersion = '1.0.0';
const deployedScriptName = 'sysutil-timeline.py';

function getDeployRoot(ctx) {
  if (!ctx.toolsRoot) {
    throw new Error('toolsRoot missing from context');
  }
  return `${ctx.toolsRoot}/${toolBundleName}/${bundleVersion}`;
}

function getScriptPath(ctx) {
  const deployRoot = getDeployRoot(ctx);
  return `${deployRoot}/${deployedScriptName}`;
}

/**
 * @param {import("../recipes/docs/jsdocs").ProbeAdvice[]} advice
 * @returns {boolean}
 */
function hasErrorAdvice(advice) {
  return advice.some((item) => item.level === 'error');
}

/**
 * @param {unknown} intervalRaw
 * @returns {{value: string, min: string, max: string} | null}
 */
function getIntervalValidationMetadata(intervalRaw) {
  const interval = Number(intervalRaw);
  if (
    !Number.isFinite(interval) ||
    interval < MIN_INTERVAL_SECONDS ||
    interval > MAX_INTERVAL_SECONDS
  ) {
    return {
      value: String(intervalRaw),
      min: String(MIN_INTERVAL_SECONDS),
      max: String(MAX_INTERVAL_SECONDS),
    };
  }
  return null;
}

/**
 * @param {unknown} intervalRaw
 * @returns {number}
 */
function parseInterval(intervalRaw) {
  const validationMetadata = getIntervalValidationMetadata(intervalRaw);
  if (validationMetadata) {
    throw {
      code: invalidIntervalMessageCode,
      metadata: validationMetadata,
    };
  }
  return Number(intervalRaw);
}

/**
 * Launch the collector and record the closest host-observed start time exposed
 * by the process API. Recording after launch excludes remote launch latency
 * from the eligible sampling window.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string[]} args
 * @returns {Promise<{
 *   handle: import("../recipes/docs/jsdocs").ProcessHandle,
 *   startedAtMonotonicMs: number
 * }>}
 */
async function startCollectorProcess(engine, args) {
  const handle = await engine.startProcess(args, {});
  return { handle, startedAtMonotonicMs: engine.monotonicNow() };
}

/**
 * @param {string} stdout
 * @param {number} interval
 * @param {number} workloadDuration
 * @returns {number | null}
 */
function parseEligibleSampleCount(stdout, interval, workloadDuration) {
  const lineCount = Number.parseInt(stdout.trim().split(/\s+/)[0], 10);
  if (!Number.isInteger(lineCount) || lineCount < 1) {
    return null;
  }

  const total = lineCount - 1;
  return getEligibleSampleCount({
    totalSamples: total,
    interval,
    duration: workloadDuration,
  });
}

/**
 * Count complete CSV rows and constrain them to the number of scheduled sample
 * slots within the workload window. A boundary sample may finish during
 * cleanup, but a later scheduled sample must not extend the workload timeline.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} outputPath
 * @param {number} interval
 * @param {number} workloadDuration
 * @returns {Promise<number | null>}
 */
async function inspectEligibleSampleCount(
  engine,
  outputPath,
  interval,
  workloadDuration,
) {
  const result = await engine.execCommand(['wc', '-l', outputPath], {});
  if (result.rc !== 0) {
    engine.log(
      'warn',
      `Failed to inspect completed collector samples: ${result.stderr}`,
    );
    return null;
  }

  const eligibleSamples = parseEligibleSampleCount(
    result.stdout,
    interval,
    workloadDuration,
  );
  if (eligibleSamples === null) {
    engine.log(
      'warn',
      `Failed to parse collector sample inspection: ${result.stdout}`,
    );
  }
  return eligibleSamples;
}

/**
 * @param {number} exitCode
 * @param {number} interval
 * @param {number} duration
 * @returns {{code: string, metadata: Record<string, string | number>}}
 */
function getCollectorError(exitCode, interval, duration) {
  if (exitCode === INSUFFICIENT_SAMPLES_EXIT_CODE) {
    return createInsufficientSamplesError({
      ...sampleRequirements,
      interval,
      duration,
    });
  }

  return {
    code: 'tool_integrations.sysutil_timeline.RUN_FAILED',
    metadata: { exitCode },
  };
}

/**
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {number} gracePeriodMs
 * @returns {Promise<boolean>}
 */
async function stopCollectorWithFallback(
  engine,
  ctx,
  gracePeriodMs = COLLECTOR_STOP_GRACE_PERIOD_MS,
) {
  const metadata = ctx.metadata;
  const collectorHandle = metadata && metadata.collectorHandle;
  if (!metadata || !collectorHandle || metadata.collectorFinished) {
    return false;
  }

  if (metadata.collectorStopPromise) {
    return await metadata.collectorStopPromise;
  }

  metadata.collectorStopPromise = (async () => {
    let killedByFallback = false;
    metadata.collectorStopRequested = true;
    try {
      await collectorHandle.interrupt();
    } catch (err) {
      engine.log(
        'warn',
        `Failed to interrupt collector: ${err?.message ?? err}`,
      );
    }

    if (metadata.collectorCompletion) {
      await Promise.race([metadata.collectorCompletion, delay(gracePeriodMs)]);
    } else {
      await delay(gracePeriodMs);
    }

    if (!metadata.collectorFinished) {
      killedByFallback = true;
      engine.log('warn', 'Collector did not stop after interrupt; killing it');
      try {
        await collectorHandle.kill();
        if (metadata.collectorCompletion) {
          await metadata.collectorCompletion;
        }
      } catch (err) {
        engine.log('warn', `Failed to kill collector: ${err?.message ?? err}`);
      }
    }
    return killedByFallback;
  })();

  return await metadata.collectorStopPromise;
}

/**
 * Finalize collection after a launched workload exits. Preserve eligible
 * samples observed before cleanup, allow an in-progress boundary sample to
 * finish, then reconcile collector failures with the sampling requirements.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @param {{
 *   interval: number,
 *   outputPath: string,
 *   workloadDuration: number
 * }} input
 * @returns {Promise<ReturnType<typeof resolveSampleCollectionError>>}
 */
async function finalizeCollectorAfterWorkload(engine, ctx, input) {
  const sampleWindow = {
    ...sampleRequirements,
    interval: input.interval,
    duration: input.workloadDuration,
  };
  const windowError = validateSampleWindow(sampleWindow);
  const samplesBeforeCleanup = await inspectEligibleSampleCount(
    engine,
    input.outputPath,
    input.interval,
    input.workloadDuration,
  );
  const killedByFallback = await stopCollectorWithFallback(
    engine,
    ctx,
    windowError
      ? COLLECTOR_STOP_GRACE_PERIOD_MS
      : COLLECTOR_SAMPLE_COMPLETION_GRACE_PERIOD_MS,
  );
  const eligibleSamples =
    samplesBeforeCleanup !== null &&
    samplesBeforeCleanup >= sampleRequirements.minimumSamples
      ? samplesBeforeCleanup
      : await inspectEligibleSampleCount(
          engine,
          input.outputPath,
          input.interval,
          input.workloadDuration,
        );
  const collectorError = resolveSampleCollectionError({
    ...sampleWindow,
    eligibleSamples,
    collectorError: ctx.metadata.collectorError,
    cleanupCausedCollectorError: killedByFallback,
    inspectionError: {
      code: 'tool_integrations.sysutil_timeline.RUN_FAILED',
      metadata: { reason: 'sample_inspection_failed' },
    },
  });

  if (collectorError === null && killedByFallback) {
    engine.log(
      'warn',
      `Collector required fallback cleanup after producing ${eligibleSamples ?? sampleRequirements.minimumSamples} eligible samples`,
    );
  }
  return collectorError;
}

async function interruptWorkload(engine, ctx) {
  const metadata = ctx.metadata;
  const workloadHandle = metadata && metadata.workloadHandle;
  if (!metadata || !workloadHandle) {
    return;
  }

  try {
    metadata.workloadExplicitlyInterrupted = true;
    await workloadHandle.interrupt();
  } catch (err) {
    engine.log('warn', `Failed to interrupt workload: ${err?.message ?? err}`);
  }
}

/**
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeNumastat(engine) {
  const check = await engine.execCommand(
    ['bash', '-c', 'command -v numastat'],
    {},
  );
  if (check.rc !== 0) {
    return {
      level: 'warning',
      messageCode: numastatNotFoundMessageCode,
    };
  }

  return {
    level: 'ready',
    messageCode: '',
  };
}

/**
 * @type {import("../recipes/docs/jsdocs").ToolIntegration}}
 */
let tool = {
  name: 'sysutil-timeline',
  version: toolIntegrationVersion,
  supportsWorkloadLaunch: true,
  description: {
    short: 'Collect system utilization into a timeline CSV.',
    long: 'Runs a lightweight Python collector to sample CPU, memory, disk, network, load, and thread stats from /proc, writing a timeline CSV artifact.',
  },
  parameters: [
    {
      id: 'interval',
      label: 'Interval',
      description: 'Sampling interval in seconds (0.01 to 60).',
      config: {
        type: 'input',
        defaultValue: '1.0',
      },
    },
    {
      id: 'thread_scan_interval',
      label: 'Thread Scan Interval',
      description: 'Thread scan interval in seconds (defaults to interval).',
      config: {
        type: 'input',
      },
    },
  ],
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Linux' }],
      dependencies: [
        {
          type: 'tool_bundle',
          name: toolBundleName,
          version: bundleVersion,
          requiredWhen: { type: 'always' },
        },
      ],
    },
    {
      appliesTo: [{ architecture: 'x86_64', os: 'Linux' }],
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
    /** @type {import("../recipes/docs/jsdocs").ProbeAdvice[]} */
    const advice = [];

    const intervalRaw =
      ctx.params && ctx.params.interval !== undefined
        ? ctx.params.interval
        : DEFAULT_INTERVAL_SECONDS;
    const intervalValidationMetadata =
      getIntervalValidationMetadata(intervalRaw);
    if (intervalValidationMetadata) {
      advice.push({
        level: 'error',
        messageCode: invalidIntervalMessageCode,
        metadata: intervalValidationMetadata,
      });
    }

    const osCheck = await engine.execCommand(['uname', '-s'], {});
    if (osCheck.rc !== 0 || osCheck.stdout.trim() !== 'Linux') {
      advice.push({
        level: 'error',
        messageCode: readinessMessageCode,
        metadata: {
          message:
            'sysutil-timeline requires a Linux target with /proc access.',
        },
      });
      return { available: false, capabilities: {}, advice };
    }

    const pythonAdvice = await probePython(
      engine,
      PYTHON_VER_MAJOR,
      PYTHON_VER_MINOR,
      'sysutil-timeline',
    );
    if (pythonAdvice.level !== 'ready') {
      advice.push(pythonAdvice);
    }

    const procCheck = await engine.execCommand(
      [
        'bash',
        '-c',
        'test -r /proc/stat -a -r /proc/meminfo -a -r /proc/loadavg -a -r /proc/vmstat -a -r /proc/net/dev',
      ],
      {},
    );
    if (procCheck.rc !== 0) {
      advice.push({
        level: 'error',
        messageCode: readinessMessageCode,
        metadata: {
          message:
            'sysutil-timeline requires /proc/stat, /proc/meminfo, /proc/loadavg, /proc/vmstat, and /proc/net/dev.',
        },
      });
    }

    const numastatAdvice = await probeNumastat(engine);
    if (numastatAdvice.level !== 'ready') {
      advice.push(numastatAdvice);
    }

    const scriptPath = getScriptPath(ctx);
    const deployAdvice = await probeDeployment(engine, scriptPath, tool.name);
    if (deployAdvice.level !== 'ready') {
      advice.push(deployAdvice);
    }

    return {
      available: !hasErrorAdvice(advice),
      capabilities: {},
      advice,
    };
  },

  run: async (engine, ctx) => {
    const outputDir = await engine.createTempDir();
    const scriptPath = getScriptPath(ctx);

    const outputFileName = 'timeline.csv';
    const outputPath = `${outputDir}/${outputFileName}`;

    const intervalRaw =
      ctx.params && ctx.params.interval !== undefined
        ? ctx.params.interval
        : DEFAULT_INTERVAL_SECONDS;
    const threadScanRaw =
      ctx.params && ctx.params.thread_scan_interval !== undefined
        ? ctx.params.thread_scan_interval
        : undefined;
    const durationRaw = ctx.timeout > 0 ? String(ctx.timeout) : '0.0';

    const interval = parseInterval(intervalRaw);
    const duration = Number(durationRaw);
    const sampleWindowError = validateSampleWindow({
      ...sampleRequirements,
      interval,
      duration,
    });
    if (sampleWindowError) {
      throw sampleWindowError;
    }

    await ensureDeployed(engine, scriptPath, tool.name);

    const args = [
      'python3',
      scriptPath,
      '--interval',
      String(interval),
      '--duration',
      String(duration),
      '--output',
      outputPath,
      '--flush',
    ];
    if (threadScanRaw !== undefined && threadScanRaw !== '') {
      const threadScan = Number(threadScanRaw);
      if (!Number.isFinite(threadScan) || threadScan <= 0) {
        throw {
          code: 'tool_integrations.sysutil_timeline.INVALID_THREAD_SCAN_INTERVAL',
          metadata: { value: String(threadScanRaw) },
        };
      }
      args.push('--thread-scan-interval', String(threadScan));
    }

    engine.startProgressTracker('Collecting sysutil timeline data');
    try {
      ctx.metadata = ctx.metadata || {};
      ctx.metadata.userStopped = false;
      ctx.metadata.timedOut = false;

      // Launch workload if there is one
      const workloadState = await launchWorkloadIfNeeded(
        engine,
        ctx.workload,
        '.',
      );
      ctx.metadata.workloadHandle = workloadState.handle;
      ctx.metadata.workloadExplicitlyInterrupted = false;

      // Launch collector
      ctx.metadata.collectorFinished = false;
      ctx.metadata.collectorError = null;
      engine.log('info', `Launching collector command: ${args.join(' ')}`);
      let collectorHandle;
      try {
        const collectorProcess = await startCollectorProcess(engine, args);
        collectorHandle = collectorProcess.handle;
        ctx.metadata.collectorStartedAtMonotonicMs =
          collectorProcess.startedAtMonotonicMs;
        engine.log('info', `Collector started successfully`);
      } catch (err) {
        engine.log(
          'error',
          `Failed to start collector: ${err?.message ?? err}`,
        );
        collectorHandle = null;
        ctx.metadata.collectorFinished = true;
        ctx.metadata.collectorError = {
          code: 'tool_integrations.sysutil_timeline.RUN_FAILED',
          metadata: {
            reason: err?.message ?? 'start_failed',
          },
        };
      }

      ctx.metadata.collectorHandle = collectorHandle;

      // Wait for collector to end in background and handle result
      if (collectorHandle) {
        const collectorCompletion = collectorHandle
          .wait()
          .then((result) => {
            ctx.metadata.collectorFinished = true;
            if (result.exitCode === 0) {
              engine.log('info', 'Collector completed with exit code 0');
            } else {
              engine.log(
                ctx.metadata.collectorStopRequested ? 'warn' : 'error',
                `Collector exited with code ${result.exitCode}`,
              );
              ctx.metadata.collectorError = getCollectorError(
                result.exitCode,
                interval,
                duration,
              );
            }
          })
          .catch((err) => {
            ctx.metadata.collectorFinished = true;
            engine.log('error', `Collector failed: ${err?.message ?? err}`);
            ctx.metadata.collectorError = {
              code: 'tool_integrations.sysutil_timeline.RUN_FAILED',
              metadata: {
                reason: err?.message ?? 'wait_failed',
              },
            };
          });
        ctx.metadata.collectorCompletion = collectorCompletion;
        void collectorCompletion;
      }

      // Wait for workload to complete if one was provided
      if (ctx.workload && ctx.workload.type === 'launch') {
        // A zero duration has no timeout; otherwise use a monotonic deadline.
        const timeoutDeadlineMonotonicMs =
          duration > 0 ? engine.monotonicNow() + duration * 1000 : null;

        // Poll for workload completion with health checks while it is still running
        while (!workloadState.completed()) {
          // If user requested stop/cancel then break. Workload and collector get stopped in onCancel/onStop handlers
          if (ctx.metadata.userStopped) {
            break;
          }

          // If we have a timeout and reached it, stop the workload, collector stops itself on timeout
          if (
            timeoutDeadlineMonotonicMs &&
            engine.monotonicNow() >= timeoutDeadlineMonotonicMs
          ) {
            engine.log('info', 'Timeout reached, stopping workload');
            if (ctx.metadata.workloadHandle) {
              try {
                ctx.metadata.workloadExplicitlyInterrupted = true;
                await ctx.metadata.workloadHandle.interrupt();
              } catch (err) {
                engine.log(
                  'warn',
                  `Failed to interrupt workload: ${err?.message ?? err}`,
                );
              }
            }
            ctx.metadata.timedOut = true;
            break;
          }

          // If collector finishes stop the workload
          if (ctx.metadata.collectorFinished) {
            engine.log('info', 'Collector finished, stopping workload');
            if (ctx.metadata.workloadHandle) {
              try {
                ctx.metadata.workloadExplicitlyInterrupted = true;
                await ctx.metadata.workloadHandle.interrupt();
              } catch (err) {
                engine.log(
                  'warn',
                  `Failed to interrupt workload: ${err?.message ?? err}`,
                );
              }
            }
            break;
          }

          await delay(100);
        }

        // If the workload completed itself (not cancel/timeout), stop collector if still running
        if (
          !ctx.metadata.userStopped &&
          !ctx.metadata.timedOut &&
          !ctx.metadata.collectorFinished
        ) {
          const workloadCompletedAtMonotonicMs =
            workloadState.completedAtMonotonicMs();
          const workloadDuration = Math.max(
            0,
            (workloadCompletedAtMonotonicMs -
              ctx.metadata.collectorStartedAtMonotonicMs) /
              1000,
          );
          ctx.metadata.collectorError = await finalizeCollectorAfterWorkload(
            engine,
            ctx,
            {
              interval,
              outputPath,
              workloadDuration,
            },
          );
        }

        // Throw any workload failures after workload should have stopped
        try {
          workloadState.assertHealthy();
        } catch (err) {
          // If error is non-zero exit code due to an interrupt we did, just log, else throw error
          if (
            err?.metadata?.reason === 'non_zero_exit' &&
            ctx.metadata.workloadExplicitlyInterrupted === true
          ) {
            engine.log(
              'warn',
              'Workload exited with non-zero code due to expected interrupt',
            );
          } else {
            throw err;
          }
        }
      }

      // Wait for collector to finish
      while (!ctx.metadata.collectorFinished) {
        await delay(100);
      }

      if (ctx.metadata.collectorError) {
        throw ctx.metadata.collectorError;
      }
    } finally {
      engine.endProgress('Collecting sysutil timeline data');
    }

    engine.emitOutput(outputPath, outputFileName, {
      name: 'sysutil-timeline-csv',
      version: '3.0',
    });
  },

  reformat: async () => {},

  onStop: async (engine, ctx) => {
    if (ctx.metadata) {
      ctx.metadata.userStopped = true;
    }
    await Promise.all([
      stopCollectorWithFallback(engine, ctx, COLLECTOR_STOP_GRACE_PERIOD_MS),
      interruptWorkload(engine, ctx),
    ]);
  },

  onCancel: async (engine, ctx) => {
    if (ctx.metadata) {
      ctx.metadata.userStopped = true;
    }
    const collectorHandle = ctx.metadata && ctx.metadata.collectorHandle;
    if (collectorHandle) {
      try {
        await collectorHandle.kill();
      } catch (err) {
        engine.log('warn', `Failed to kill collector: ${err?.message ?? err}`);
      }
    }
    const workloadHandle = ctx.metadata && ctx.metadata.workloadHandle;
    if (workloadHandle) {
      try {
        ctx.metadata.workloadExplicitlyInterrupted = true;
        await workloadHandle.kill();
      } catch (err) {
        engine.log('warn', `Failed to kill workload: ${err?.message ?? err}`);
      }
    }
  },
};
