// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

const NO_OP_WORKLOAD_STATE = {
  completed: () => false,
  update: () => {},
  assertHealthy: () => {},
  handle: null,
};

const MAX_UNTERMINATED_BYTES = 1024 * 1024;
const WORKLOAD_LOG_FILENAME = 'workload.log.json';

/**
 * Launch a workload if provided, returning an object that can be polled via completed().
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {Workload} workload
 * @param {string} outputDirectory
 * @returns {Promise<{completed: () => boolean, update: () => void, assertHealthy: () => void, handle: import("../recipes/docs/jsdocs").ProcessHandle | null}>}
 */
async function launchWorkloadIfNeeded(engine, workload, outputDirectory) {
  const workloadInfo = normalizeWorkloadDescriptor(workload);
  if (workloadInfo.type === 'none') {
    return NO_OP_WORKLOAD_STATE;
  }

  if (workloadInfo.type === 'invalid') {
    throw {
      code: 'tool_integrations.common.WORKLOAD_START_FAILED',
      metadata: {
        workload: stringifyWorkload(workload),
        reason: workloadInfo.reason,
      },
    };
  }

  let finished = false;
  let workloadFailure;
  const command = workloadInfo.command;
  const stringCommand = workloadInfo.rawCommand;
  const logPath = joinOutputPath(outputDirectory, WORKLOAD_LOG_FILENAME);

  engine.log('info', `Launching workload command: ${stringCommand}`);
  let handle;
  let logHandle;
  try {
    logHandle = await engine.createRunFile(logPath, {
      name: 'log-json',
      version: '0.2',
    });
  } catch (err) {
    engine.log(
      'error',
      `Failed to create workload log file '${logPath}': ${err?.message ?? err}`,
    );
    throw {
      code: 'tool_integrations.common.WORKLOAD_LOG_FILE_CREATE_FAILED',
      metadata: {
        workload: stringCommand,
        reason: err?.message ?? 'log_file_create_failed',
      },
    };
  }
  try {
    handle = await engine.startProcess(['sh', '-c', stringCommand], {
      stdout: { redirect: 'stream' },
      stderr: { redirect: 'stream' },
      environment: workloadInfo.environment,
      workingDirectory: workloadInfo.workingDir,
    });
  } catch (err) {
    engine.log(
      'error',
      `Failed to start workload ${stringCommand}: ${err?.message ?? err}`,
    );
    await logHandle.close();
    throw {
      code: 'tool_integrations.common.WORKLOAD_START_FAILED',
      metadata: {
        workload: stringCommand,
        reason: err?.message ?? 'start_failed',
      },
    };
  }

  engine.log('info', `Workload '${stringCommand}' started successfully`);
  void captureWorkloadLogs(engine, logHandle, handle, stringCommand).catch(
    (err) => {
      engine.log(
        'warn',
        `Failed to capture workload logs for '${stringCommand}': ${err?.message ?? err}`,
      );
    },
  );
  void handle
    .wait()
    .then((result) => {
      finished = true;
      if (result.exitCode === 0) {
        engine.log(
          'info',
          `Workload '${stringCommand}' completed with exit code 0`,
        );
      } else {
        engine.log(
          'error',
          `Workload '${stringCommand}' exited with code ${result.exitCode}`,
        );
        workloadFailure = {
          code: 'tool_integrations.common.WORKLOAD_RUNTIME_FAILED',
          metadata: {
            workload: stringCommand,
            exitCode: String(result.exitCode),
            reason: 'non_zero_exit',
          },
        };
      }
    })
    .catch((err) => {
      finished = true;
      engine.log(
        'error',
        `Workload '${stringCommand}' failed: ${err?.message ?? err}`,
      );
      workloadFailure = {
        code: 'tool_integrations.common.WORKLOAD_WAIT_FAILED',
        metadata: {
          workload: stringCommand,
          reason: err?.message ?? 'wait_failed',
        },
      };
    });

  return {
    completed: () => finished,
    update: () => {},
    assertHealthy: () => {
      if (workloadFailure) {
        throw workloadFailure;
      }
    },
    handle: handle,
  };
}

function joinOutputPath(outputDirectory, fileName) {
  if (!outputDirectory || outputDirectory === '.') {
    return fileName;
  }
  const trimmed = outputDirectory.replace(/[\\/]+$/, '').replace(/\\/g, '/');
  return `${trimmed}/${fileName}`;
}

function formatTimestamp(date) {
  return date.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

async function captureWorkloadLogs(engine, logHandle, processHandle, command) {
  let writeChain = Promise.resolve();
  const enqueueLine = (severity, message, streamName) => {
    const entry = {
      timestamp: formatTimestamp(new Date()),
      severity,
      message,
      context: { stream: streamName },
    };
    writeChain = writeChain.then(() =>
      logHandle.append(`${JSON.stringify(entry)}\n`),
    );
  };

  const drainStream = async (stream, streamName, severity) => {
    if (!stream) {
      return;
    }
    let buffer = '';
    const iterator = stream[Symbol.asyncIterator]();
    while (true) {
      const { value: chunk, done } = await iterator.next();
      if (done) {
        break;
      }
      const chunkText = String(chunk);
      buffer += chunkText;
      let newlineIndex = buffer.indexOf('\n');
      while (newlineIndex !== -1) {
        let line = buffer.slice(0, newlineIndex);
        if (line.endsWith('\r')) {
          line = line.slice(0, -1);
        }
        enqueueLine(severity, line, streamName);
        buffer = buffer.slice(newlineIndex + 1);
        newlineIndex = buffer.indexOf('\n');
      }
      if (buffer.length >= MAX_UNTERMINATED_BYTES) {
        enqueueLine(severity, buffer, streamName);
        buffer = '';
      }
    }
    if (buffer.length > 0) {
      enqueueLine(severity, buffer, streamName);
    }
  };

  try {
    await Promise.all([
      drainStream(processHandle.stdout, 'stdout', 'info'),
      drainStream(processHandle.stderr, 'stderr', 'error'),
    ]);
    await writeChain;
  } finally {
    await logHandle.close();
    engine.log(
      'debug',
      `Workload log capture complete for '${command}' -> ${logHandle.path}`,
    );
  }
}

function normalizeWorkloadDescriptor(workload) {
  if (!workload) {
    return { type: 'none' };
  }

  if (typeof workload === 'object') {
    const rawType = workload.type;
    const type = typeof rawType === 'string' ? rawType.toLowerCase() : '';
    if (type === 'launch') {
      const command = workload.command ?? [];
      const rawCommand = workload.rawCommand ?? '';
      if (!Array.isArray(command) || command.length === 0) {
        return { type: 'invalid', reason: 'missing_command' };
      }
      if (typeof rawCommand !== 'string' || rawCommand.length === 0) {
        return { type: 'invalid', reason: 'missing_command' };
      }
      return {
        type: 'launch',
        command: command,
        rawCommand: rawCommand,
        environment: workload.environment,
        workingDir: workload.workingDir,
      };
    }
  }
  return { type: 'none' };
}

function stringifyWorkload(workload) {
  if (typeof workload === 'string') {
    return workload;
  }
  try {
    return JSON.stringify(workload);
  } catch {
    return String(workload);
  }
}

/**
 * Gets the executable from the workload slice
 * @param {string[]} wl
 * @returns {string}
 */
function getExecutableFromWorkload(wl) {
  return wl && wl.length > 0 ? wl[0] : '';
}

module.exports = {
  launchWorkloadIfNeeded,
  getExecutableFromWorkload,
};
