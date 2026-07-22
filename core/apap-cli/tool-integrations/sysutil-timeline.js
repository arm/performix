// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const { probePython, probeDeployment, ensureDeployed } = require('./utils');
const { launchWorkloadIfNeeded } = require('./workload');

const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';
const numastatNotFoundMessageCode =
  'tool_integrations.sysutil_timeline.NUMASTAT_NOT_FOUND';

const PYTHON_VER_MAJOR = 3;
const PYTHON_VER_MINOR = 9;

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
      description: 'Sampling interval in seconds.',
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
        : '1.0';
    const threadScanRaw =
      ctx.params && ctx.params.thread_scan_interval !== undefined
        ? ctx.params.thread_scan_interval
        : undefined;
    const durationRaw = ctx.timeout > 0 ? String(ctx.timeout) : '0.0';

    const interval = Number(intervalRaw);
    const duration = Number(durationRaw);
    if (!Number.isFinite(interval) || interval <= 0) {
      throw {
        code: 'tool_integrations.sysutil_timeline.INVALID_INTERVAL',
        metadata: { value: String(intervalRaw) },
      };
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
        collectorHandle = await engine.startProcess(args, {});
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

      // Wait for collector to end in background and handle result
      if (collectorHandle) {
        void collectorHandle
          .wait()
          .then((result) => {
            ctx.metadata.collectorFinished = true;
            if (result.exitCode === 0) {
              engine.log('info', 'Collector completed with exit code 0');
            } else {
              engine.log(
                'error',
                `Collector exited with code ${result.exitCode}`,
              );
              ctx.metadata.collectorError = {
                code: 'tool_integrations.sysutil_timeline.RUN_FAILED',
                metadata: { exitCode: result.exitCode },
              };
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
      }
      ctx.metadata.collectorHandle = collectorHandle;

      // Wait for workload to complete if one was provided
      if (ctx.workload && ctx.workload.type === 'launch') {
        // If duration is 0 timeout is null, otherwise calculate the timeout timestamp
        const timeoutTimestampMs =
          duration > 0 ? Date.now() + duration * 1000 : null;

        // Poll for workload completion with health checks while it is still running
        while (!workloadState.completed()) {
          // If user requested stop/cancel then break. Workload and collector get stopped in onCancel/onStop handlers
          if (ctx.metadata.userStopped) {
            break;
          }

          // If we have a timeout and reached it, stop the workload, collector stops itself on timeout
          if (timeoutTimestampMs && Date.now() >= timeoutTimestampMs) {
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

          await new Promise((resolve) => setTimeout(resolve, 100));
        }

        // If the workload completed itself (not cancel/timeout), stop collector if still running
        if (
          !ctx.metadata.userStopped &&
          !ctx.metadata.timedOut &&
          !ctx.metadata.collectorFinished
        ) {
          try {
            await ctx.metadata.collectorHandle.interrupt();
          } catch (err) {
            engine.log(
              'warn',
              `Failed to interrupt collector: ${err?.message ?? err}`,
            );
          }
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
        await new Promise((resolve) => setTimeout(resolve, 100));
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
    const collectorHandle = ctx.metadata && ctx.metadata.collectorHandle;
    if (collectorHandle) {
      try {
        await collectorHandle.interrupt();
      } catch (err) {
        engine.log(
          'warn',
          `Failed to interrupt collector: ${err?.message ?? err}`,
        );
      }
    }
    const workloadHandle = ctx.metadata && ctx.metadata.workloadHandle;
    if (workloadHandle) {
      try {
        ctx.metadata.workloadExplicitlyInterrupted = true;
        await workloadHandle.interrupt();
      } catch (err) {
        engine.log(
          'warn',
          `Failed to interrupt workload: ${err?.message ?? err}`,
        );
      }
    }
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
