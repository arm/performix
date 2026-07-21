// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  probeDeployment,
  ensureDeployed,
  isElevatePrivilegeError,
  normalizeRootOutputAccess,
} = require('./utils');

const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';

const performixGlobal =
  /** @type {import("../recipes/docs/jsdocs").PerformixGlobal} */ (
    globalThis['performix']
  );
const toolBundleName = 'syscall-trace';
const bundleVersion = performixGlobal.engineVersion;
const toolIntegrationVersion = '1.0.0';
const deployedBinaryName = 'syscall-trace';
const straceInterruptGraceMs = 5000;
const collectionProgressTracker = 'Collecting syscall trace data';
const parsingProgressTracker = 'Parsing syscall trace output';
const logTextComponent = { name: 'log-text', version: '1.0' };
const parquetComponent = { name: 'syscall-trace-parquet', version: '1.0' };
const straceAvailabilityCommand = [
  'sh',
  '-c',
  'command -v strace >/dev/null 2>&1',
];
const straceNotFoundError = {
  code: 'tool_integrations.syscall_trace.STRACE_NOT_FOUND',
  metadata: {},
};
const straceNotFoundAdvice = {
  level: 'error',
  messageCode: straceNotFoundError.code,
  metadata: {},
};

function getDeployRoot(ctx) {
  if (!ctx.toolsRoot) {
    throw new Error('toolsRoot missing from context');
  }
  return `${ctx.toolsRoot}/${toolBundleName}/${bundleVersion}`;
}

function getBinaryPath(ctx) {
  const deployRoot = getDeployRoot(ctx);
  return `${deployRoot}/${deployedBinaryName}`;
}

/**
 * @type {import("../recipes/docs/jsdocs").ToolIntegration}
 */
let tool = {
  name: 'syscall-trace',
  version: toolIntegrationVersion,
  supportsWorkloadLaunch: true,
  description: {
    short: 'Collect syscall trace events.',
    long: 'Runs strace for a launch or attach workload and emits structured syscall event data for recipes.',
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
    /** @type {import("../recipes/docs/jsdocs").ProbeAdvice[]} */
    const advice = [];

    const osCheck = await engine.execCommand(['uname', '-s'], {});
    if (osCheck.rc !== 0 || osCheck.stdout.trim() !== 'Linux') {
      advice.push({
        level: 'error',
        messageCode: readinessMessageCode,
        metadata: {
          message: 'syscall-trace requires a Linux target.',
        },
      });
      return { available: false, capabilities: {}, advice };
    }

    if (!(await isStraceAvailable(engine))) {
      advice.push(straceNotFoundAdvice);
    }

    const binaryPath = getBinaryPath(ctx);
    const deployAdvice = await probeDeployment(engine, binaryPath, tool.name);
    if (deployAdvice.level !== 'ready') {
      advice.push(deployAdvice);
    }

    return {
      available: advice.length === 0,
      capabilities: {},
      advice,
    };
  },

  run: async (engine, ctx) => {
    initialiseInterruptionState(ctx);
    throwIfUserInterrupted(engine, ctx, 'setup');

    const outputDir = await engine.createTempDir();
    throwIfUserInterrupted(engine, ctx, 'output setup');

    const binaryPath = getBinaryPath(ctx);

    const artifacts = buildArtifacts(outputDir);

    validateWorkload(ctx.workload);

    // Attaching strace to an existing process requires ptrace privileges on common Linux targets.
    const runAsPrivileged = ctx.workload.type === 'attach';

    await ensureDeployed(engine, binaryPath, tool.name);
    throwIfUserInterrupted(engine, ctx, 'tool deployment');

    const straceAvailable = await isStraceAvailable(engine);
    throwIfUserInterrupted(engine, ctx, 'strace availability check');
    if (!straceAvailable) {
      throw straceNotFoundError;
    }

    let runError = null;
    let parserCompleted = false;
    let activeProgressTracker = null;

    try {
      activeProgressTracker = collectionProgressTracker;
      engine.startProgressTracker(activeProgressTracker);
      // Before strace starts there is no trace data to preserve, so a user interruption should abort the run.
      throwIfUserInterrupted(engine, ctx, 'strace launch');
      const straceHandle = await engine.startProcess(
        buildStraceArgs(ctx.workload, artifacts.rawTrace.path),
        {
          asPrivileged: runAsPrivileged,
          stdout: { redirect: 'file', path: artifacts.straceStdout.path },
          stderr: { redirect: 'file', path: artifacts.straceStderr.path },
          workingDirectory: ctx.workload.workingDir || ctx.workingdir || '.',
          environment: {
            ...(ctx.env || {}),
            ...(ctx.workload.environment || {}),
          },
        },
      );
      ctx.metadata.stopInterruptsProcess = true;
      ctx.metadata.processHandle = straceHandle;
      await stopProcessIfInterrupted(engine, ctx, straceHandle, 'strace');

      const straceResult = await waitForCollection(straceHandle, ctx, engine);
      ctx.metadata.processHandle = null;
      if (
        straceResult.exitCode !== 0 &&
        !isExpectedCollectionExitCode(
          ctx.metadata.stopRequested,
          straceResult.timedOut,
          straceResult.exitCode,
        )
      ) {
        runError = {
          code: 'tool_integrations.syscall_trace.RUN_FAILED',
          metadata: { detail: `strace exit code ${straceResult.exitCode}` },
        };
      }

      // Once trace collection has started, Stop should preserve partial data; only Cancel should abort parsing.
      if (!runError && isUserCancellationRequested(ctx)) {
        runError = getUserInterruptionError(ctx);
      }

      if (!runError) {
        throwIfUserCancelled(engine, ctx, 'parser launch');
        engine.endProgress(activeProgressTracker);
        activeProgressTracker = parsingProgressTracker;
        engine.startProgressTracker(activeProgressTracker);
        const parserHandle = await engine.startProcess(
          [
            binaryPath,
            '--output',
            artifacts.parquet.path,
            '--raw-output',
            artifacts.rawTrace.path,
          ],
          {
            asPrivileged: runAsPrivileged,
            stdout: { redirect: 'file', path: artifacts.parserStdout.path },
            stderr: { redirect: 'file', path: artifacts.parserStderr.path },
          },
        );
        ctx.metadata.stopInterruptsProcess = false;
        ctx.metadata.processHandle = parserHandle;
        await stopProcessIfCancelled(engine, ctx, parserHandle, 'parser');

        const parserResult = await parserHandle.wait();
        if (parserResult.exitCode === 0) {
          parserCompleted = true;
        } else if (
          isExpectedStopExitCode(
            isUserCancellationRequested(ctx),
            parserResult.exitCode,
          )
        ) {
          runError = getUserInterruptionError(ctx);
        } else {
          runError = {
            code: 'tool_integrations.syscall_trace.RUN_FAILED',
            metadata: {
              detail: `parser exit code ${parserResult.exitCode}`,
            },
          };
        }
      }

      if (runAsPrivileged) {
        await normalizeRootOutputAccess(engine, outputDir, true);
      }
    } catch (err) {
      if (isElevatePrivilegeError(err)) {
        throw err;
      }
      if (isUserInterruptionError(err)) {
        runError = err;
      } else {
        runError = {
          code: 'tool_integrations.syscall_trace.RUN_FAILED',
          metadata: { detail: err?.message ?? 'run_failed' },
        };
      }
    } finally {
      if (ctx.metadata) {
        ctx.metadata.processHandle = null;
      }
      if (activeProgressTracker !== null) {
        engine.endProgress(activeProgressTracker);
      }
    }

    for (const artifact of Object.values(artifacts)) {
      if (artifact === artifacts.parquet && !parserCompleted) {
        continue;
      }
      await emitArtifactIfExists(engine, artifact);
    }

    if (runError) {
      throw runError;
    }
  },

  reformat: async () => {},
  onStop: async (engine, ctx) => {
    await requestInterruption(engine, ctx, false);
  },
  onCancel: async (engine, ctx) => {
    await requestInterruption(engine, ctx, true);
  },
};

function buildArtifacts(outputDir) {
  return {
    rawTrace: buildArtifact(outputDir, 'syscall_trace.log'),
    parquet: buildArtifact(outputDir, 'syscalls.parquet', parquetComponent),
    straceStdout: buildArtifact(outputDir, 'syscall_trace_strace_stdout.txt'),
    straceStderr: buildArtifact(outputDir, 'syscall_trace_strace_stderr.txt'),
    parserStdout: buildArtifact(outputDir, 'syscall_trace_parser_stdout.txt'),
    parserStderr: buildArtifact(outputDir, 'syscall_trace_parser_stderr.txt'),
  };
}

function buildArtifact(outputDir, name, component = logTextComponent) {
  return {
    name,
    path: `${outputDir}/${name}`,
    component,
  };
}

async function emitArtifactIfExists(engine, artifact) {
  const check = await engine.execCommand(['stat', artifact.path], {});
  if (check.rc === 0) {
    engine.emitOutput(artifact.path, artifact.name, artifact.component);
  }
}

async function isStraceAvailable(engine) {
  const check = await engine.execCommand(straceAvailabilityCommand, {});
  return check.rc === 0;
}

function buildStraceArgs(workload, rawPath) {
  const args = [
    'strace',
    '-ff',
    '--silence=attach,exit,path-resolution,personality,thread-execve,superseded',
    '--signal=none',
    '-ttt',
    '-T',
    '-o',
    rawPath,
    '-s',
    '128',
  ];
  if (workload.type === 'launch') {
    args.push(...workload.command);
  } else {
    args.push('-p', String(workload.pid));
  }
  return args;
}

function initialiseInterruptionState(ctx) {
  ctx.metadata = ctx.metadata || {};
  ctx.metadata.stopRequested = ctx.metadata.stopRequested === true;
  ctx.metadata.cancelRequested = ctx.metadata.cancelRequested === true;
  ctx.metadata.timedOut = false;
  ctx.metadata.processHandle = null;
  ctx.metadata.stopInterruptsProcess = true;
}

function isUserInterruptionRequested(ctx) {
  return (
    ctx.metadata?.stopRequested === true ||
    ctx.metadata?.cancelRequested === true
  );
}

function isUserCancellationRequested(ctx) {
  return ctx.metadata?.cancelRequested === true;
}

function getUserInterruptionError(ctx) {
  if (ctx.metadata?.cancelRequested === true) {
    return { code: 'engine.common.USER_CANCELLATION_ERROR' };
  }
  return { code: 'engine.common.USER_STOPPED_ERROR' };
}

function isUserInterruptionError(err) {
  return (
    !!err &&
    typeof err === 'object' &&
    'code' in err &&
    (err.code === 'engine.common.USER_CANCELLATION_ERROR' ||
      err.code === 'engine.common.USER_STOPPED_ERROR')
  );
}

function throwIfUserInterrupted(engine, ctx, stageName) {
  if (!isUserInterruptionRequested(ctx)) {
    return;
  }

  engine.log(
    'info',
    `syscall-trace stopping before ${stageName} because user interruption was requested`,
  );
  throw getUserInterruptionError(ctx);
}

function throwIfUserCancelled(engine, ctx, stageName) {
  if (!isUserCancellationRequested(ctx)) {
    return;
  }

  engine.log(
    'info',
    `syscall-trace stopping before ${stageName} because user cancellation was requested`,
  );
  throw getUserInterruptionError(ctx);
}

async function stopProcessIfInterrupted(engine, ctx, handle, processName) {
  if (!isUserInterruptionRequested(ctx)) {
    return;
  }

  const action = ctx.metadata.cancelRequested === true ? 'kill' : 'interrupt';
  await stopProcess(engine, handle, action, processName);
}

async function stopProcessIfCancelled(engine, ctx, handle, processName) {
  if (!isUserCancellationRequested(ctx)) {
    return;
  }

  await stopProcess(engine, handle, 'kill', processName);
}

async function requestInterruption(engine, ctx, cancelRequested) {
  ctx.metadata = ctx.metadata || {};
  ctx.metadata.stopRequested = true;
  ctx.metadata.cancelRequested =
    ctx.metadata.cancelRequested === true || cancelRequested;

  const shouldStopActiveProcess =
    cancelRequested || ctx.metadata.stopInterruptsProcess !== false;

  if (ctx.metadata.processHandle && shouldStopActiveProcess) {
    await stopProcess(
      engine,
      ctx.metadata.processHandle,
      cancelRequested ? 'kill' : 'interrupt',
      'active process',
    );
  }
}

async function stopProcess(engine, handle, action, processName) {
  try {
    await (action === 'kill' ? handle.kill() : handle.interrupt());
  } catch (err) {
    engine.log(
      'warn',
      `Failed to ${action} syscall-trace ${processName}: ${err?.message ?? err}`,
    );
  }
}

async function waitForCollection(handle, ctx, engine) {
  const waitPromise = handle.wait();
  const timeoutMs = ctx.timeout > 0 ? ctx.timeout * 1000 : 0;

  if (timeoutMs <= 0) {
    return { ...(await waitPromise), timedOut: false };
  }

  const firstResult = await Promise.race([
    waitPromise.then((result) => ({ state: 'exited', result })),
    new Promise((resolve) =>
      setTimeout(() => resolve({ state: 'timed_out' }), timeoutMs),
    ),
  ]);

  if (firstResult.state === 'exited') {
    return { ...firstResult.result, timedOut: false };
  }

  ctx.metadata.timedOut = true;

  try {
    await handle.interrupt();
  } catch (err) {
    engine.log(
      'warn',
      `Failed to interrupt syscall-trace after timeout: ${err?.message ?? err}`,
    );
    await handle.kill();
    return { ...(await waitPromise), timedOut: true };
  }

  const interruptedResult = await Promise.race([
    waitPromise.then((result) => ({ state: 'exited', result })),
    new Promise((resolve) =>
      setTimeout(() => resolve({ state: 'timed_out' }), straceInterruptGraceMs),
    ),
  ]);

  if (interruptedResult.state === 'exited') {
    return { ...interruptedResult.result, timedOut: true };
  }

  await handle.kill();
  return { ...(await waitPromise), timedOut: true };
}

function validateWorkload(workload) {
  if (!workload || !workload.type) {
    throw {
      code: 'tool_integrations.syscall_trace.MISSING_WORKLOAD',
      metadata: {},
    };
  }

  if (workload.type === 'systemWide') {
    throw {
      code: 'tool_integrations.syscall_trace.UNSUPPORTED_WORKLOAD',
      metadata: { workloadType: 'system-wide' },
    };
  }

  if (workload.type === 'launch') {
    if (!Array.isArray(workload.command) || workload.command.length === 0) {
      throw {
        code: 'tool_integrations.syscall_trace.INVALID_LAUNCH_WORKLOAD',
        metadata: {},
      };
    }
    return;
  }

  if (workload.type === 'attach') {
    if (!Number.isInteger(workload.pid) || workload.pid <= 0) {
      throw {
        code: 'tool_integrations.syscall_trace.INVALID_ATTACH_WORKLOAD',
        metadata: { pid: String(workload.pid) },
      };
    }
    return;
  }

  throw {
    code: 'tool_integrations.syscall_trace.UNSUPPORTED_WORKLOAD',
    metadata: { workloadType: String(workload.type) },
  };
}

function isExpectedStopExitCode(stopRequested, exitCode) {
  return stopRequested && (exitCode === 130 || exitCode === 137);
}

function isExpectedCollectionExitCode(stopRequested, timedOut, exitCode) {
  return (
    isExpectedStopExitCode(stopRequested || timedOut, exitCode) ||
    (timedOut && (exitCode === 1 || exitCode === -1))
  );
}
