// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const { probePython, probeWhl, normalizeRootOutputAccess } = require('./utils');

const TOOL_NAME = 'asct';
const TOOL_DISPLAY_NAME = 'ASCT';
const BUNDLE_VERSION = '0.6.1';
const BUNDLE_HASH = 'd2f4360';
const BUNDLE_DIR = `${TOOL_NAME}/${BUNDLE_VERSION}`;
const BUNDLE_ARCHIVE_NAME = `${TOOL_NAME}-${BUNDLE_VERSION}+${BUNDLE_HASH}.tar.gz`;
const INSTALL_DIR_NAME = `install-${BUNDLE_VERSION}+${BUNDLE_HASH}`;
const TOOL_LOG_FILENAME = `${TOOL_NAME}.log`;
const SETUP_LOG_DIRNAME = 'setup-logs';
const RUN_DATA_DIRNAME = 'data';
const CANCEL_INTERRUPT_GRACE_MS = 3000;
const CANCEL_KILL_WAIT_GRACE_MS = 1500;
const INTERRUPT_PROPAGATION_GRACE_MS = 500;
// Handles CSI/private-mode escapes such as ESC[?25l and cursor/color codes.
const ANSI_ESCAPE_REGEX =
  /\x1B(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1B\\)|[()][A-Za-z])/g;
// Input: "00:01:23 [02/06] bandwidth-sweep > (21/65) for size 79.48KiB"
// Matches: ["00:01:23", "02", "06", "bandwidth-sweep", "21", "65", "for size 79.48KiB"]
const ASCT_PROGRESS_LINE_REGEX =
  /^(\d{2}:\d{2}:\d{2}) \[([0-9?]+)\/([0-9?]+)\] (.+?)(?: > \(([0-9?]+)\/([0-9?]+)\) (.+))?$/;
const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';

/**
 * @typedef {import("../recipes/docs/jsdocs").Engine} Engine
 * @typedef {import("../recipes/docs/jsdocs").ToolContext} ToolContext
 * @typedef {import("../recipes/docs/jsdocs").ProcessHandle} ProcessHandle
 * @typedef {import("../recipes/docs/jsdocs").ProcessOptions} ProcessOptions
 * @typedef {{message: string, percent: number}} ProgressUpdate
 * @typedef {{seen: Set<string>}} ProgressState
 *
 * @typedef {Object} ProgressHooks
 * @property {ProgressUpdate=} initialProgress
 * @property {((line: string) => Promise<void>|void)=} stdoutLineHandler
 * @property {((line: string) => Promise<void>|void)=} stderrLineHandler
 *
 * @typedef {Object} SetupStep
 * @property {string} logDir
 * @property {string} stageName
 * @property {string} trackerId
 * @property {string} label
 * @property {string} logBasename
 * @property {string[]} cmd
 * @property {string} errorCode
 * @property {(exitCode: number) => Object.<string, any>} getErrorMetadata
 */

/**
 * Build the deployed ASCT bundle path.
 *
 * @param {string} toolsRoot
 * @returns {string}
 */
function buildBundlePath(toolsRoot) {
  return `${toolsRoot}/${BUNDLE_DIR}/${BUNDLE_ARCHIVE_NAME}`;
}

/**
 * Build the persistent target-side install paths for the current ASCT bundle.
 *
 * @param {string} installRoot
 * @returns {{installRoot: string, venvDir: string, binaryPath: string, pipPath: string}}
 */
function buildPersistentInstallPaths(installRoot) {
  const venvDir = `${installRoot}/venv`;
  return {
    installRoot,
    venvDir,
    binaryPath: `${venvDir}/bin/${TOOL_NAME}`,
    pipPath: `${venvDir}/bin/pip`,
  };
}

/**
 * Check whether a target path currently exists.
 * TODO: reuse the shared helper from `./utils` once it is exported there.
 *
 * @param {Engine} engine
 * @param {string} path
 * @returns {Promise<boolean>}
 */
async function pathExists(engine, path) {
  const statResult = await engine.execCommand(['stat', path], {});
  return statResult.rc === 0;
}

/**
 * Create a directory path recursively on the target.
 *
 * @param {Engine} engine
 * @param {string} path
 * @returns {Promise<void>}
 */
async function ensureDirectory(engine, path) {
  const cmd = ['mkdir', '-p', path];
  const mkdirResult = await engine.execCommand(cmd, {});
  if (mkdirResult.rc !== 0) {
    throw {
      code: 'tool_integrations.common.MKDIR_FAILED',
      metadata: {
        tool: TOOL_NAME,
        path,
        cmd: cmd.join(' '),
        exitCode: mkdirResult.rc,
        stderr: mkdirResult.stderr || '',
      },
    };
  }
}

/**
 * Resolve a persistent target-side install root for cached ASCT environments.
 * Keep the cache near the deployed ASCT bundle by rooting it under `toolsRoot`.
 *
 * @param {string} toolsRoot
 * @returns {string}
 */
function resolvePersistentInstallRoot(toolsRoot) {
  return `${toolsRoot}/${BUNDLE_DIR}/${INSTALL_DIR_NAME}`;
}

/**
 * Check whether the cached ASCT install looks reusable for this run.
 *
 * @param {Engine} engine
 * @param {{installRoot: string, venvDir: string, binaryPath: string, pipPath: string}} installPaths
 * @returns {Promise<boolean>}
 */
async function hasReusableInstall(engine, installPaths) {
  if (
    !(await pathExists(engine, installPaths.venvDir)) ||
    !(await pathExists(engine, installPaths.binaryPath))
  ) {
    return false;
  }

  const healthCheck = await engine.execCommand(
    [installPaths.binaryPath, '--help'],
    {},
  );
  return healthCheck.rc === 0;
}

/**
 * Register the standard stdout/stderr log files for a setup step.
 *
 * @param {Engine} engine
 * @param {string} logDir
 * @param {string} basename
 * @returns {ProcessOptions}
 */
function emitSetupStepLogs(engine, logDir, basename) {
  const stdoutPath = `${logDir}/${basename}.stdout.log`;
  const stderrPath = `${logDir}/${basename}.stderr.log`;
  engine.emitOutput(
    stdoutPath,
    `${SETUP_LOG_DIRNAME}/${basename}.stdout.log`,
    /** @type {any} */ ({ name: 'log-text', version: '1.0' }),
  );
  engine.emitOutput(
    stderrPath,
    `${SETUP_LOG_DIRNAME}/${basename}.stderr.log`,
    /** @type {any} */ ({ name: 'log-text', version: '1.0' }),
  );
  return {
    stdout: { redirect: 'file', path: stdoutPath },
    stderr: { redirect: 'file', path: stderrPath },
  };
}

/**
 * Parse a numeric progress token, returning null when it is unknown.
 *
 * @param {string|undefined} token
 * @returns {number|null}
 */
function parseProgressCount(token) {
  if (!token || token.includes('?')) {
    return null;
  }

  const parsed = Number.parseInt(token, 10);
  return Number.isNaN(parsed) ? null : parsed;
}

/**
 * Convert ASCT progress output into a tracker message and percent.
 *
 * @param {string} rawLine
 * @returns {ProgressUpdate|null}
 */
function parseAsctProgressUpdate(rawLine) {
  const cleanLine = rawLine.replace(ANSI_ESCAPE_REGEX, '').trim();
  if (cleanLine === '') {
    return null;
  }

  // ASCT emits progress through the logger, so discard the log prefix and
  // parse only the final progress payload:
  // "INFO|...| 00:01:23 [02/06] bandwidth-sweep > (21/65) for size 79.48KiB"
  // becomes benchmark 2/6, "bandwidth-sweep", step 21/65, "for size 79.48KiB".
  const messageStart = cleanLine.lastIndexOf('| ');
  const progressLine =
    messageStart >= 0 ? cleanLine.slice(messageStart + 2).trim() : cleanLine;
  const match = progressLine.match(ASCT_PROGRESS_LINE_REGEX);
  if (!match) {
    return null;
  }

  const benchmarkCurrent = parseProgressCount(match[2]);
  const benchmarkTotal = parseProgressCount(match[3]);
  const benchmarkName = match[4].trim();
  const stepCurrent = parseProgressCount(match[5]);
  const stepTotal = parseProgressCount(match[6]);
  const stepDescription = match[7]?.trim();

  let percent = 0;
  if (benchmarkCurrent && benchmarkTotal && benchmarkTotal > 0) {
    const completedBenchmarks = Math.max(benchmarkCurrent - 1, 0);
    let withinBenchmarkProgress = 0;
    if (stepCurrent && stepTotal && stepTotal > 0) {
      withinBenchmarkProgress = Math.min(stepCurrent / stepTotal, 1);
    }
    percent = Math.min(
      ((completedBenchmarks + withinBenchmarkProgress) / benchmarkTotal) * 100,
      99,
    );
  }

  const message = stepDescription
    ? `${benchmarkName}: ${stepDescription}`
    : benchmarkName;
  return { message, percent };
}

/**
 * Drain a process stream line-by-line without failing if parsing breaks.
 *
 * @param {Engine} engine
 * @param {AsyncIterable<unknown>|null|undefined} stream
 * @param {string} streamName
 * @param {(line: string) => Promise<void>|void} onLine
 * @returns {Promise<void>}
 */
async function drainProcessStreamByLine(engine, stream, streamName, onLine) {
  if (!stream) {
    return;
  }

  let buffer = '';
  try {
    await forAwait(stream, async (chunk) => {
      buffer += String(chunk);

      let newlineIndex = buffer.indexOf('\n');
      while (newlineIndex !== -1) {
        let line = buffer.slice(0, newlineIndex);
        if (line.endsWith('\r')) {
          line = line.slice(0, -1);
        }
        await onLine(line);
        buffer = buffer.slice(newlineIndex + 1);
        newlineIndex = buffer.indexOf('\n');
      }
    });

    if (buffer.length > 0) {
      const finalLine = buffer.endsWith('\r') ? buffer.slice(0, -1) : buffer;
      await onLine(finalLine);
    }
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err);
    engine.log(
      'warn',
      `Failed to read ${TOOL_DISPLAY_NAME} ${streamName} stream for progress updates: ${reason}`,
    );
  }
}

/**
 * Log ASCT stderr lines after stripping terminal control sequences.
 *
 * @param {Engine} engine
 * @param {string} line
 * @returns {void}
 */
function logAsctStderrLine(engine, line) {
  const cleanLine = line.replace(ANSI_ESCAPE_REGEX, '').trim();
  if (cleanLine !== '') {
    engine.log('info', `${TOOL_DISPLAY_NAME} stderr: ${cleanLine}`);
  }
}

/**
 * Parse ASCT stderr for progress updates and log any other lines.
 *
 * @param {Engine} engine
 * @param {string} trackerId
 * @param {string} line
 * @param {ProgressState} state
 * @returns {Promise<void>}
 */
async function handleAsctStderrLine(engine, trackerId, line, state) {
  const progress = parseAsctProgressUpdate(line);
  if (progress) {
    const dedupeKey = `${progress.message}|${progress.percent.toFixed(1)}`;
    if (state.seen.has(dedupeKey)) {
      return;
    }

    state.seen.add(dedupeKey);
    await updateProgressSafely(
      engine,
      trackerId,
      progress.message,
      progress.percent,
    );
    return;
  }

  logAsctStderrLine(engine, line);
}

/**
 * Update a progress tracker and downgrade failures to warnings.
 *
 * @param {Engine} engine
 * @param {string} trackerId
 * @param {string} message
 * @param {number} percent
 * @returns {Promise<void>}
 */
async function updateProgressSafely(engine, trackerId, message, percent) {
  try {
    await engine.updateProgress(trackerId, message, percent);
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err);
    engine.log(
      'warn',
      `Failed to update ${TOOL_DISPLAY_NAME} progress tracker '${trackerId}': ${reason}`,
    );
  }
}

/**
 * Record the active process so stop/cancel can find it.
 *
 * @param {ToolContext} ctx
 * @param {ProcessHandle} handle
 * @param {string} trackerId
 * @param {string} label
 * @returns {void}
 */
function registerActiveProcess(ctx, handle, trackerId, label) {
  // The stop/cancel callbacks only act on one canonical "active" child at a time.
  ctx.metadata.activeProcessHandle = handle;
  ctx.metadata.activeProcessTrackerId = trackerId;
  ctx.metadata.activeProcessLabel = label;
}

/**
 * Clear the active process record when the handle is no longer running.
 *
 * @param {ToolContext} ctx
 * @param {ProcessHandle} handle
 * @returns {void}
 */
function clearActiveProcess(ctx, handle) {
  // Only clear the slot if it still points at the handle this caller launched.
  if (ctx.metadata.activeProcessHandle !== handle) {
    return;
  }

  ctx.metadata.activeProcessHandle = null;
  ctx.metadata.activeProcessTrackerId = null;
  ctx.metadata.activeProcessLabel = null;
}

/**
 * End the matching progress tracker if it is still active.
 *
 * @param {Engine} engine
 * @param {ToolContext} ctx
 * @param {string} trackerId
 * @returns {void}
 */
function endTrackedProgress(engine, ctx, trackerId) {
  // Guard against ending a tracker that has already been replaced by a newer process.
  if (ctx.metadata.activeProcessTrackerId !== trackerId) {
    return;
  }

  engine.endProgress(trackerId);
  ctx.metadata.activeProcessTrackerId = null;
}

/**
 * Stop the active ASCT process with interrupt-first behavior.
 *
 * @param {Engine} engine
 * @param {ToolContext} ctx
 * @param {string} triggerName
 * @returns {Promise<void>}
 */
async function stopActiveAsctProcess(engine, ctx, triggerName) {
  const activeHandle = ctx.metadata.activeProcessHandle;
  const activeLabel = ctx.metadata.activeProcessLabel ?? TOOL_DISPLAY_NAME;
  const activeTrackerId = ctx.metadata.activeProcessTrackerId;
  if (!activeHandle) {
    // A stop can arrive after the child already exited and cleaned up its bookkeeping.
    engine.log(
      'info',
      `${triggerName} requested for ${TOOL_DISPLAY_NAME}, but no active process handle was found`,
    );
    return;
  }
  const activePid = String(activeHandle.pid);

  /** @param {string} reason */
  const killAndWait = async (reason) => {
    engine.log('warn', reason);
    // This is the hard-stop path used only after the interrupt path failed.
    await activeHandle.kill();
    engine.log('info', `${activeLabel} kill signal sent to PID ${activePid}`);

    const killWaitResult = await Promise.race([
      activeHandle
        .wait()
        .then(() => 'exited')
        .catch(() => 'exited'),
      new Promise((resolve) =>
        setTimeout(() => resolve('timed_out'), CANCEL_KILL_WAIT_GRACE_MS),
      ),
    ]);

    if (killWaitResult === 'timed_out') {
      engine.log(
        'warn',
        `${activeLabel} did not exit within ${CANCEL_KILL_WAIT_GRACE_MS}ms after kill. The process may need to be manually killed on the target side (PID ${activePid}).`,
      );
    }
  };

  try {
    // Let ASCT handle SIGINT cleanly first, then escalate only if it hangs.
    engine.log(
      'info',
      `${triggerName} requested for ${activeLabel}; sending interrupt signal to PID ${activePid}`,
    );
    await activeHandle.interrupt();
    engine.log(
      'info',
      `${activeLabel} interrupt signal sent to PID ${activePid}`,
    );

    const waitResult = await Promise.race([
      activeHandle.wait().then(() => 'exited'),
      new Promise((resolve) =>
        setTimeout(() => resolve('timed_out'), CANCEL_INTERRUPT_GRACE_MS),
      ),
    ]);

    if (waitResult === 'exited') {
      engine.log(
        'info',
        `${activeLabel} exited after interrupt for PID ${activePid}`,
      );
    } else {
      await killAndWait(
        `${activeLabel} did not exit within ${CANCEL_INTERRUPT_GRACE_MS}ms after interrupt; falling back to kill for PID ${activePid}`,
      );
    }
  } catch {
    await killAndWait(
      `Interrupt failed for ${activeLabel} PID ${activePid}; falling back to kill`,
    );
  } finally {
    if (activeTrackerId) {
      endTrackedProgress(engine, ctx, activeTrackerId);
    }
    clearActiveProcess(ctx, activeHandle);
    // The same child may still be referenced by its stage-specific slot.
    if (ctx.metadata.runHandle === activeHandle) {
      ctx.metadata.runHandle = null;
    }
    if (ctx.metadata.setupHandle === activeHandle) {
      ctx.metadata.setupHandle = null;
    }
  }
}

/**
 * Report whether a cancel or stop request has been recorded.
 *
 * @param {ToolContext} ctx
 * @returns {boolean}
 */
function isCancellationRequested(ctx) {
  return ctx.metadata.cancelRequested === true || !!ctx.metadata.interruptType;
}

/**
 * Resolve the interruption label used in logs and shutdown.
 *
 * @param {ToolContext} ctx
 * @returns {string}
 */
function getInterruptionTriggerName(ctx) {
  return ctx.metadata.interruptType === 'stop' ? 'Stop' : 'Cancellation';
}

/**
 * Resolve the catalog code for the current interruption.
 *
 * @param {ToolContext} ctx
 * @returns {string|null}
 */
function getInterruptionMessageCode(ctx) {
  if (ctx.metadata.interruptType === 'stop') {
    return 'engine.common.USER_STOPPED';
  }
  if (ctx.metadata.interruptType === 'cancel') {
    return 'engine.common.USER_CANCELED';
  }
  return null;
}

/**
 * Check whether the thrown error matches the expected interruption code.
 *
 * @param {ToolContext} ctx
 * @param {unknown} err
 * @returns {boolean}
 */
function isInterruptionError(ctx, err) {
  const messageCode = getInterruptionMessageCode(ctx);
  return (
    !!messageCode &&
    !!err &&
    typeof err === 'object' &&
    'code' in err &&
    err.code === messageCode
  );
}

/**
 * Throw a user-facing interruption when cancel/stop arrived between stages.
 *
 * @param {Engine} engine
 * @param {ToolContext} ctx
 * @param {string} stageName
 * @returns {void}
 */
function throwIfInterrupted(engine, ctx, stageName) {
  const messageCode = getInterruptionMessageCode(ctx);
  if (!messageCode) {
    return;
  }

  engine.log(
    'info',
    `${TOOL_DISPLAY_NAME} stopping before ${stageName} because ${ctx.metadata.interruptType} was requested`,
  );
  throw { code: messageCode };
}

/**
 * Give stop/cancel callbacks a brief chance to update the tool context.
 *
 * @param {Engine} engine
 * @param {ToolContext} ctx
 * @param {string} stageName
 * @returns {Promise<void>}
 */
async function waitForPendingInterruption(engine, ctx, stageName) {
  if (isCancellationRequested(ctx)) {
    // Fast path when the interrupt was already recorded before this stage started.
    throwIfInterrupted(engine, ctx, stageName);
    return;
  }

  await new Promise((resolve) =>
    setTimeout(resolve, INTERRUPT_PROPAGATION_GRACE_MS),
  );
  throwIfInterrupted(engine, ctx, stageName);
}

/**
 * Start a tracked process and keep progress and cancellation state aligned.
 *
 * @param {Engine} engine
 * @param {ToolContext} ctx
 * @param {string} label
 * @param {string} trackerId
 * @param {string[]} cmd
 * @param {ProcessOptions} opts
 * @param {'setupHandle'|'runHandle'} handleKey
 * @param {ProgressHooks} progressHooks
 * @returns {Promise<{exitCode: number}>}
 */
async function runTrackedProcess(
  engine,
  ctx,
  label,
  trackerId,
  cmd,
  opts,
  handleKey,
  progressHooks = {},
) {
  engine.log('info', `Starting ${label}: ${cmd.join(' ')}`);
  if (opts.stdout?.redirect === 'file' && opts.stdout.path) {
    engine.log(
      'info',
      `${label} stdout will be written to ${opts.stdout.path}`,
    );
  }
  if (opts.stderr?.redirect === 'file' && opts.stderr.path) {
    engine.log(
      'info',
      `${label} stderr will be written to ${opts.stderr.path}`,
    );
  }
  engine.startProgressTracker(trackerId);
  if (progressHooks.initialProgress) {
    await updateProgressSafely(
      engine,
      trackerId,
      progressHooks.initialProgress.message,
      progressHooks.initialProgress.percent,
    );
  }

  let handle;
  let progressEnded = false;
  try {
    handle = await engine.startProcess(cmd, opts);
    // Keep both the stage-specific handle slot and the canonical active handle in sync.
    ctx.metadata[handleKey] = handle;
    registerActiveProcess(ctx, handle, trackerId, label);

    if (isCancellationRequested(ctx)) {
      // Cover the race where stop lands after startProcess returns but before wait begins.
      await stopActiveAsctProcess(engine, ctx, getInterruptionTriggerName(ctx));
    }

    const streamDrains = [];
    if (progressHooks.stdoutLineHandler && handle.stdout) {
      streamDrains.push(
        drainProcessStreamByLine(
          engine,
          handle.stdout,
          'stdout',
          progressHooks.stdoutLineHandler,
        ),
      );
    }
    if (progressHooks.stderrLineHandler && handle.stderr) {
      streamDrains.push(
        drainProcessStreamByLine(
          engine,
          handle.stderr,
          'stderr',
          progressHooks.stderrLineHandler,
        ),
      );
    }

    const [result] = await Promise.all([handle.wait(), ...streamDrains]);
    engine.log('info', `${label} exited with code ${result.exitCode}`);
    if (isCancellationRequested(ctx)) {
      throw { code: getInterruptionMessageCode(ctx) };
    }
    if (result.exitCode === 0) {
      endTrackedProgress(engine, ctx, trackerId);
      progressEnded = true;
    }
    return result;
  } catch (err) {
    if (isInterruptionError(ctx, err)) {
      engine.log(
        'info',
        `${label} stopped because ${ctx.metadata.interruptType} was requested`,
      );
      throw err;
    }
    engine.log('error', `${label} failed before completion: ${String(err)}`);
    throw err;
  } finally {
    if (
      !progressEnded &&
      (!handle || ctx.metadata.activeProcessTrackerId === trackerId)
    ) {
      // Launch failures, interruptions, and non-zero exits all land here.
      engine.endProgress(trackerId);
    }
    if (handle) {
      clearActiveProcess(ctx, handle);
    }
    if (handle && ctx.metadata[handleKey] === handle) {
      ctx.metadata[handleKey] = null;
    }
  }
}

/**
 * Run a setup step with standard logging and error mapping.
 *
 * @param {Engine} engine
 * @param {ToolContext} ctx
 * @param {SetupStep} step
 * @returns {Promise<void>}
 */
async function runSetupStep(engine, ctx, step) {
  const processOptions = emitSetupStepLogs(
    engine,
    step.logDir,
    step.logBasename,
  );
  const result = await runTrackedProcess(
    engine,
    ctx,
    step.label,
    step.trackerId,
    step.cmd,
    processOptions,
    'setupHandle',
  );
  await waitForPendingInterruption(engine, ctx, step.stageName);
  if (result.exitCode !== 0) {
    throw {
      code: step.errorCode,
      metadata: step.getErrorMetadata(result.exitCode),
    };
  }
}

/**
 * Parse the recipe-provided benchmark selection payload.
 *
 * @param {unknown} benchmarks
 * @returns {string[]}
 */
function parseBenchmarksParam(benchmarks) {
  if (Array.isArray(benchmarks)) {
    return benchmarks.map(String).filter(Boolean);
  }

  if (typeof benchmarks !== 'string') {
    return [];
  }

  return benchmarks
    .split(',')
    .map((benchmark) => benchmark.trim())
    .filter(Boolean);
}

const PYTHON_VER_MAJOR = 3;
const PYTHON_VER_MINOR = 10;
/**
 * Check whether `numactl` is available on the target.
 *
 * @param {Engine} engine
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeNumactl(engine) {
  const check = await engine.execCommand(
    ['bash', '-c', 'command -v numactl'],
    {},
  );
  if (check.rc !== 0) {
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message:
          'numactl is not available on the target machine. Install the numactl package on the target machine.',
      },
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
  name: TOOL_NAME,
  version: BUNDLE_VERSION,
  supportsWorkloadLaunch: true,
  description: {
    short: 'ASCT runs system characterization benchmarks and diagnostics.',
    long: 'ASCT (Arm System Characterization Tool) is a standalone command-line utility for running low-level benchmarks, diagnostic scripts, and system tests on Arm-based platforms. It provides a standardized environment for measuring hardware characteristics such as memory latency/bandwidth and storage performance, supporting platform bring-up, tuning, and architectural comparisons.',
  },
  parameters: [
    {
      id: 'systemInfoOnly',
      label: 'System info only',
      description: 'Run only system-info (no benchmarks).',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
    {
      id: 'defaultBenchmarks',
      label: 'Default benchmarks',
      description:
        'Run the default latency, bandwidth, NUMA, and core-to-core benchmarks. Loaded latency is not included and can extend the run.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
    {
      id: 'benchmarks',
      label: 'Benchmarks',
      description:
        'Comma-separated benchmark names selected by the invoking recipe.',
      config: {
        type: 'input',
        defaultValue: '',
      },
    },
  ],
  deployments: [
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool_bundle',
          name: TOOL_NAME,
          version: BUNDLE_VERSION,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  /**
   * Validate ASCT's non-privileged prerequisites.
   */
  probe: async (engine, ctx) => {
    /** @type {import("../recipes/docs/jsdocs").ProbeAdvice[]} */
    let advice = [];

    let py = await probePython(
      engine,
      PYTHON_VER_MAJOR,
      PYTHON_VER_MINOR,
      tool.name,
    );
    if (py.level !== 'ready') advice.push(py);

    let whlAdvice = await probeWhl(
      engine,
      buildBundlePath(ctx.toolsRoot),
      tool.name,
    );
    if (whlAdvice.level !== 'ready') {
      advice.push(whlAdvice);
    }

    let numactlAdvice = await probeNumactl(engine);
    if (numactlAdvice.level !== 'ready') {
      advice.push(numactlAdvice);
    }

    return {
      available: advice.length === 0,
      capabilities: {},
      advice,
    };
  },

  /**
   * Reformat is currently a no-op because ASCT outputs are already emitted as-is.
   */
  reformat: async (engine, ctx) => {},

  /**
   * Set up the ASCT runtime environment, launch the requested command, and collect its output.
   */
  run: async (engine, ctx) => {
    if (typeof ctx.metadata.cancelRequested !== 'boolean') {
      ctx.metadata.cancelRequested = false;
    }
    if (ctx.metadata.interruptType === undefined) {
      ctx.metadata.interruptType = null;
    }
    // Catch a stop/cancel that was already recorded before setup begins.
    throwIfInterrupted(engine, ctx, 'ASCT setup');

    await waitForPendingInterruption(engine, ctx, 'ASCT setup');
    let deployPath = buildBundlePath(ctx.toolsRoot);

    // Create a scratch area for per-run logs and collected ASCT artifacts.
    let outputDir = await engine.createTempDir();
    const setupLogsDir = outputDir + `/${SETUP_LOG_DIRNAME}`;
    const runDataDir = outputDir + `/${RUN_DATA_DIRNAME}`;
    await engine.mkDir(setupLogsDir);
    await engine.mkDir(runDataDir);
    const installRoot = await resolvePersistentInstallRoot(ctx.toolsRoot);
    const installPaths = buildPersistentInstallPaths(installRoot);
    engine.log(
      'info',
      `${TOOL_DISPLAY_NAME} install root for this bundle: ${installRoot}`,
    );
    throwIfInterrupted(engine, ctx, 'cached install lookup');

    // Reuse a healthy cached install when available; otherwise rebuild it.
    const reusedCachedInstall = await hasReusableInstall(engine, installPaths);
    if (reusedCachedInstall) {
      engine.log(
        'info',
        `Reusing cached ${TOOL_DISPLAY_NAME} install from ${installRoot}`,
      );
    } else {
      engine.log(
        'info',
        `No reusable cached ${TOOL_DISPLAY_NAME} install found; rebuilding at ${installRoot}`,
      );
      if (await pathExists(engine, installRoot)) {
        engine.log(
          'warn',
          `Removing incomplete or unhealthy cached ${TOOL_DISPLAY_NAME} install at ${installRoot}`,
        );
        await engine.rm(installRoot, true, true);
      }

      await ensureDirectory(engine, installRoot);
      throwIfInterrupted(engine, ctx, 'bundle verification');

      let wheelExistsResult = await engine.execCommand(
        ['stat', deployPath],
        {},
      );
      if (wheelExistsResult.rc !== 0) {
        throw {
          code: 'tool_integrations.common.TOOL_NOT_DEPLOYED',
          metadata: {
            tool: tool.name,
            deployPath: deployPath,
            locality: engine.getLocality(),
          },
        };
      }

      // Setup logs and collection outputs are split so failed setup still yields useful logs.
      // Build the venv first so later commands run against the bundled install.
      await runSetupStep(engine, ctx, {
        logDir: setupLogsDir,
        stageName: 'Python virtual environment setup',
        trackerId: `Setting up ${TOOL_DISPLAY_NAME} Python virtual environment`,
        label: 'ASCT Python virtual environment setup',
        logBasename: 'setup_venv',
        cmd: ['python3', '-m', 'venv', installPaths.venvDir],
        errorCode: 'tool_integrations.common.CREATE_PYTHON_VENV',
        getErrorMetadata: (exitCode) => ({
          tool: tool.name,
          pythonVersion: '3.10',
          exitCode,
        }),
      });

      // Keep an interruption window immediately before the first long setup step.
      await waitForPendingInterruption(engine, ctx, 'pip install setup');
      await waitForPendingInterruption(engine, ctx, 'pip install launch');
      await runSetupStep(engine, ctx, {
        logDir: setupLogsDir,
        stageName: 'package installation',
        trackerId: `Installing ${TOOL_DISPLAY_NAME} into the virtual environment`,
        label: 'ASCT package installation',
        logBasename: 'pip_install',
        cmd: [installPaths.pipPath, 'install', deployPath],
        errorCode: 'tool_integrations.common.INSTALL_MODULE',
        getErrorMetadata: (exitCode) => ({
          tool: tool.name,
          deployPath: deployPath,
          exitCode,
        }),
      });
      throwIfInterrupted(engine, ctx, 'installation smoke test');

      // Smoke-test the entrypoint before launching the real collection run.
      let testCmd = [installPaths.binaryPath, '--help'];
      let testResult = await engine.execCommand(testCmd, {});
      if (testResult.rc !== 0) {
        throw `${TOOL_DISPLAY_NAME} module installed successfully, but ${testCmd.join(' ')} exited with code ${testResult.rc}`;
      }
    }
    throwIfInterrupted(engine, ctx, 'ASCT command preparation');

    const selectedBenchmarks = parseBenchmarksParam(ctx.params?.benchmarks);
    const systemInfoOnly = ctx.params?.systemInfoOnly === true;
    const defaultBenchmarks = ctx.params?.defaultBenchmarks === true;

    let processArgs = [installPaths.binaryPath];
    if (systemInfoOnly) {
      processArgs.push('report', 'system');
    } else {
      processArgs.push('run');
      processArgs.push('--no-progress-bar');
      if (selectedBenchmarks.length > 0) {
        processArgs.push(...selectedBenchmarks);
      }
    }

    processArgs.push(
      '--format',
      'csv',
      '--output-dir',
      runDataDir,
      '--force',
      '--log-file',
      `${runDataDir}/${TOOL_LOG_FILENAME}`,
    );

    // Stage 6: rebuild the runtime environment so the cached venv tools take precedence.
    const currentPathResult = await engine.execCommand(
      ['bash', '-c', 'echo $PATH'],
      {
        asPrivileged: true,
      },
    );
    const currentPythonPathResult = await engine.execCommand(
      ['bash', '-c', 'echo ${PYTHONPATH:-}'],
      {
        asPrivileged: true,
      },
    );
    const venvPath = installPaths.venvDir;
    const existingPath = currentPathResult.stdout.trim();
    const existingPythonPath = currentPythonPathResult.stdout.trim();
    const updatePath = [`${venvPath}/bin`, ...existingPath.split(':')]
      .filter(Boolean)
      .join(':');
    const updatePythonPath = [
      `${venvPath}/lib/python${PYTHON_VER_MAJOR}.${PYTHON_VER_MINOR}/site-packages`,
      ...existingPythonPath.split(':'),
    ]
      .filter(Boolean)
      .join(':');
    throwIfInterrupted(engine, ctx, 'output registration');

    engine.log('info', `PATH for ${TOOL_DISPLAY_NAME} run: ${updatePath}`);
    engine.log(
      'info',
      `PYTHONPATH for ${TOOL_DISPLAY_NAME} run: ${updatePythonPath}`,
    );
    engine.log(
      'info',
      `Starting ${TOOL_DISPLAY_NAME} with args: ${processArgs.join(' ')}`,
    );

    // Register the output files once setup is complete and the run is ready to launch.
    engine.emitOutput(
      `${runDataDir}/**/*`,
      'output/**/*',
      /** @type {any} */ ({ name: 'asct-data', version: '1.0' }),
    );
    engine.emitOutput(
      `${runDataDir}/${TOOL_LOG_FILENAME}`,
      TOOL_LOG_FILENAME,
      /** @type {any} */ ({ name: 'log-text', version: '1.0' }),
    );

    // One last stop check before the long-running ASCT child becomes the active process.
    // Stop early if cancel/stop arrived while setup was still underway, before
    // the main ASCT process was launched and could register itself as active.
    throwIfInterrupted(engine, ctx, 'ASCT launch');

    const benchmarkSelectionSummary =
      defaultBenchmarks || selectedBenchmarks.length === 0
        ? 'default benchmarks'
        : `${selectedBenchmarks.length} selected`;
    const progressTrackerId = systemInfoOnly
      ? `Collecting ${TOOL_DISPLAY_NAME} system information`
      : `Collecting ${TOOL_DISPLAY_NAME} benchmarks (${benchmarkSelectionSummary})`;
    const progressState = { seen: new Set() };

    // Stream stderr so benchmark status lines can become APX progress updates.
    let runResult;
    try {
      runResult = await runTrackedProcess(
        engine,
        ctx,
        progressTrackerId,
        progressTrackerId,
        processArgs,
        {
          asPrivileged: true,
          stderr: {
            redirect: 'stream',
          },
          environment: {
            PATH: updatePath,
            PYTHONPATH: updatePythonPath,
            VIRTUAL_ENV: venvPath,
          },
        },
        'runHandle',
        {
          initialProgress: systemInfoOnly
            ? {
                message: `Gathering ${TOOL_DISPLAY_NAME} system information`,
                percent: 0,
              }
            : {
                message: `Waiting for ${TOOL_DISPLAY_NAME} benchmark progress`,
                percent: 0,
              },
          stderrLineHandler: systemInfoOnly
            ? async (line) => logAsctStderrLine(engine, line)
            : async (line) =>
                handleAsctStderrLine(
                  engine,
                  progressTrackerId,
                  line,
                  progressState,
                ),
        },
      );
    } finally {
      try {
        await normalizeRootOutputAccess(engine, outputDir, true);
      } catch (err) {
        engine.log(
          'warn',
          `Failed to fix ownership/permissions for ${TOOL_DISPLAY_NAME} output dir: ${String(err)}`,
        );
      }
    }

    throwIfInterrupted(engine, ctx, 'ASCT run completion');
    if (runResult.exitCode === 0) {
      engine.log('info', `${TOOL_DISPLAY_NAME} module executed successfully`);
    } else {
      engine.log(
        'error',
        `${TOOL_DISPLAY_NAME} module failed with exit code ${runResult.exitCode}`,
      );
      throw {
        code: 'tool_integrations.asct.RUN_FAILED',
        metadata: { exitCode: runResult.exitCode, log: TOOL_LOG_FILENAME },
      };
    }
  },

  /**
   * Handle user cancellation by marking the run as cancelled and stopping the active process.
   * Cancellation shares the same shutdown path as stop, but preserves the
   * distinct interruption type for later messaging and error codes.
   */
  onCancel: async (engine, ctx) => {
    ctx.metadata.cancelRequested = true;
    ctx.metadata.interruptType = 'cancel';
    await stopActiveAsctProcess(engine, ctx, 'Cancellation');
  },

  /**
   * Handle an explicit stop request by marking the run as cancelled and stopping the active process.
   * We intentionally do not walk a list of remembered handles here; the active
   * process helper centralizes graceful interrupt, kill fallback, tracker
   * cleanup, and clearing whichever stage-specific slot still references it.
   */
  onStop: async (engine, ctx) => {
    ctx.metadata.cancelRequested = true;
    ctx.metadata.interruptType = 'stop';
    await stopActiveAsctProcess(engine, ctx, 'Stop');
  },
};

// ASCT does not require objdump; no additional probe checks are needed.
