// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  ensureDeployed,
  probeDeployment,
  probePython,
  probePythonModule,
} = require('./utils');

const performixGlobal =
  /** @type {import("../recipes/docs/jsdocs").PerformixGlobal} */ (
    globalThis['performix']
  );

const toolBundleName = 'pytorch-collect';
const parquetWriterBundleName = 'parquet-writer';
const bundleVersion = performixGlobal.engineVersion;
const toolIntegrationVersion = '1.0.0';
const minimumPythonMajor = 3;
const minimumPythonMinor = 8;
const apiCallsComponent = {
  name: 'pytorch-api-calls-parquet',
  version: '1.0',
};
const operatorCallsComponent = {
  name: 'pytorch-operator-calls-parquet',
  version: '1.0',
};
const logComponent = { name: 'log-text', version: '1.0' };

function getDeployRoot(ctx) {
  if (!ctx.toolsRoot) {
    throw new Error('toolsRoot missing from context');
  }
  return `${ctx.toolsRoot}/${toolBundleName}/${bundleVersion}`;
}

function getPackagePath(ctx) {
  return `${getDeployRoot(ctx)}/pytorch_collect`;
}

function getParquetWriterRoot(ctx) {
  if (!ctx.toolsRoot) {
    throw new Error('toolsRoot missing from context');
  }
  return `${ctx.toolsRoot}/${parquetWriterBundleName}/${bundleVersion}`;
}

function getParquetWriterModulePath(ctx) {
  return `${getParquetWriterRoot(ctx)}/apx_parquet_writer.py`;
}

function getParquetWriterBinaryPath(ctx) {
  return `${getParquetWriterRoot(ctx)}/parquet-writer`;
}

/**
 * Check both required files while returning at most one deployment advice for
 * the shared bundle.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeParquetWriterDeployment(engine, ctx) {
  const check = await engine.execCommand(
    ['stat', getParquetWriterModulePath(ctx), getParquetWriterBinaryPath(ctx)],
    {},
  );
  if (check.rc !== 0) {
    return {
      level: 'error',
      messageCode: 'tool_integrations.common.TOOL_NOT_DEPLOYED',
      metadata: {
        tool: parquetWriterBundleName,
        deployPath: getParquetWriterRoot(ctx),
        locality: engine.getLocality(),
      },
    };
  }
  return { level: 'ready', messageCode: '' };
}

/**
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {string}
 */
function getPythonExecutable(ctx) {
  const python = ctx.params?.python;
  if (typeof python !== 'string' || python.trim() === '') {
    return 'python3';
  }
  return python.trim();
}

/**
 * Build the environment shared by probe and run, preserving the target's
 * ambient PYTHONPATH before prepending the deployed modules.
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<{options: import("../recipes/docs/jsdocs").ExecOptions, pythonPathRead: boolean}>}
 */
async function getExecutionOptions(engine, ctx) {
  const environment = {
    ...(ctx.env || {}),
    ...(ctx.workload?.type === 'launch' ? ctx.workload.environment || {} : {}),
  };
  const baseOptions = {
    workingDirectory: ctx.workload?.workingDir || ctx.workingdir || '.',
    environment,
  };
  const pythonPathResult = await engine.execCommand(
    [
      getPythonExecutable(ctx),
      '-c',
      'import os, sys; sys.stdout.write(os.environ.get("PYTHONPATH", ""))',
    ],
    baseOptions,
  );
  if (pythonPathResult.rc !== 0) {
    return { options: baseOptions, pythonPathRead: false };
  }

  const deployedPythonPath = `${getDeployRoot(ctx)}:${getParquetWriterRoot(ctx)}`;
  environment.PYTHONPATH = pythonPathResult.stdout
    ? `${deployedPythonPath}:${pythonPathResult.stdout}`
    : deployedPythonPath;
  return { options: baseOptions, pythonPathRead: true };
}

/**
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").Workload | undefined} workload
 * @param {string | undefined} pythonExecutable Python interpreter used to parse the workload script
 * @param {import("../recipes/docs/jsdocs").ExecOptions} execOptions
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeWorkload(engine, workload, pythonExecutable, execOptions) {
  if (!workload || workload.type !== 'launch') {
    return {
      level: 'error',
      messageCode: 'tool_integrations.pytorch_collect.UNSUPPORTED_WORKLOAD',
      metadata: { workloadType: workload?.type ?? 'missing' },
    };
  }
  if (workload.useShell) {
    return {
      level: 'error',
      messageCode: 'tool_integrations.pytorch_collect.USE_SHELL',
      metadata: {},
    };
  }
  if (!workload.command || workload.command.length === 0) {
    return {
      level: 'error',
      messageCode: 'tool_integrations.common.INVALID_LAUNCH_WORKLOAD',
      metadata: {},
    };
  }

  const modulePath = workload.command[0];
  const fileCheck = await engine.execCommand(
    ['test', '-f', modulePath],
    execOptions,
  );
  if (fileCheck.rc !== 0) {
    return {
      level: 'error',
      messageCode: 'tool_integrations.pytorch_collect.MODULE_FILE_NOT_FOUND',
      metadata: { module: modulePath },
    };
  }

  // Parse rather than execute the file so that an executable or another
  // inappropriate file is rejected without running any workload code.
  if (pythonExecutable) {
    const parseCheck = await engine.execCommand(
      [
        pythonExecutable,
        '-c',
        'import ast, pathlib, sys; ast.parse(pathlib.Path(sys.argv[1]).read_bytes(), filename=sys.argv[1])',
        modulePath,
      ],
      execOptions,
    );
    if (parseCheck.rc !== 0) {
      return {
        level: 'error',
        messageCode: 'tool_integrations.pytorch_collect.MODULE_PARSE_FAILED',
        metadata: { module: modulePath },
      };
    }
  }

  return { level: 'ready', messageCode: '' };
}

/**
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {string} path
 * @param {string} name
 * @param {{name: string, version: string}} component
 * @returns {Promise<boolean>}
 */
async function emitIfPresent(engine, path, name, component) {
  const check = await engine.execCommand(['stat', path], {});
  if (check.rc !== 0) {
    return false;
  }
  engine.emitOutput(path, name, component);
  return true;
}

async function interruptCollector(engine, ctx) {
  if (!ctx.metadata.processHandle) {
    return;
  }
  try {
    await ctx.metadata.processHandle.interrupt();
  } catch (error) {
    engine.log(
      'warn',
      `Failed to interrupt PyTorch collector: ${error?.message ?? error}`,
    );
  }
}

/** @type {import("../recipes/docs/jsdocs").ToolIntegration} */
const tool = {
  name: 'pytorch-collect',
  version: toolIntegrationVersion,
  supportsWorkloadLaunch: true,
  description: {
    short: 'Collect PyTorch API and operator calls.',
    long: 'Runs a Python module while tracing PyTorch API and operator calls, and emits Parquet data for analysis.',
  },
  parameters: [
    {
      id: 'python',
      label: 'Python interpreter',
      description:
        'Optional interpreter executable in a Python environment containing PyTorch.',
      config: {
        type: 'input',
        defaultValue: 'python3',
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
          name: toolBundleName,
          version: bundleVersion,
          requiredWhen: { type: 'always' },
        },
        {
          type: 'tool_bundle',
          name: parquetWriterBundleName,
          version: bundleVersion,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],

  probe: async (engine, ctx) => {
    /** @type {import("../recipes/docs/jsdocs").ProbeAdvice[]} */
    const advice = [];

    const pythonExecutable = getPythonExecutable(ctx);
    const { options: execOptions } = await getExecutionOptions(engine, ctx);
    const pythonAdvice = await probePython(
      engine,
      minimumPythonMajor,
      minimumPythonMinor,
      tool.name,
      pythonExecutable,
      execOptions,
    );
    if (pythonAdvice.level !== 'ready') {
      advice.push(pythonAdvice);
    } else {
      const torchAdvice = await probePythonModule(
        engine,
        'torch',
        tool.name,
        pythonExecutable,
        execOptions,
      );
      if (torchAdvice.level !== 'ready') {
        advice.push(torchAdvice);
      }
    }

    const deploymentAdvice = await probeDeployment(
      engine,
      getPackagePath(ctx),
      tool.name,
    );
    if (deploymentAdvice.level !== 'ready') {
      advice.push(deploymentAdvice);
    }
    const writerDeploymentAdvice = await probeParquetWriterDeployment(
      engine,
      ctx,
    );
    if (writerDeploymentAdvice.level !== 'ready') {
      advice.push(writerDeploymentAdvice);
    }

    const workloadAdvice = await probeWorkload(
      engine,
      ctx.workload,
      pythonAdvice.level === 'ready' ? pythonExecutable : undefined,
      execOptions,
    );
    if (workloadAdvice.level !== 'ready') {
      advice.push(workloadAdvice);
    }

    return {
      available: !advice.some((item) => item.level === 'error'),
      capabilities: {},
      advice,
    };
  },

  run: async (engine, ctx) => {
    ctx.metadata = ctx.metadata || {};
    const { options: execOptions, pythonPathRead } = await getExecutionOptions(
      engine,
      ctx,
    );
    if (!pythonPathRead) {
      throw {
        code: 'tool_integrations.pytorch_collect.RUN_FAILED',
        metadata: { reason: 'collector_process_failed' },
      };
    }
    const workloadAdvice = await probeWorkload(
      engine,
      ctx.workload,
      getPythonExecutable(ctx),
      execOptions,
    );
    if (workloadAdvice.level !== 'ready') {
      throw {
        code: workloadAdvice.messageCode,
        metadata: workloadAdvice.metadata,
      };
    }

    const packagePath = getPackagePath(ctx);
    await ensureDeployed(engine, packagePath, tool.name);
    await ensureDeployed(
      engine,
      getParquetWriterModulePath(ctx),
      parquetWriterBundleName,
    );
    await ensureDeployed(
      engine,
      getParquetWriterBinaryPath(ctx),
      parquetWriterBundleName,
    );

    const outputDir = await engine.createTempDir();
    const apiCallsPath = `${outputDir}/api_calls.parquet`;
    const operatorCallsPath = `${outputDir}/operator_calls.parquet`;
    const stdoutPath = `${outputDir}/pytorch_collect_stdout.txt`;
    const stderrPath = `${outputDir}/pytorch_collect_stderr.txt`;
    const completionMarkerPath = `${outputDir}/pytorch_collect_complete`;
    const args = [
      getPythonExecutable(ctx),
      '-m',
      'pytorch_collect.cli',
      '--output',
      outputDir,
      '--writer',
      'parquet',
      '--completion-marker',
      completionMarkerPath,
      '--',
      ...ctx.workload.command,
    ];

    engine.startProgressTracker('Collecting PyTorch call data');
    try {
      const handle = await engine.startProcess(args, {
        ...execOptions,
        stdout: { redirect: 'file', path: stdoutPath },
        stderr: { redirect: 'file', path: stderrPath },
      });
      ctx.metadata.processHandle = handle;
      if (ctx.metadata.requestStop) {
        await interruptCollector(engine, ctx);
      }
      if (ctx.metadata.requestCancel) {
        await ctx.metadata.processHandle.kill();
      }
      const result = await handle.wait();
      ctx.metadata.processHandle = null;

      await emitIfPresent(
        engine,
        stdoutPath,
        'pytorch_collect_stdout.txt',
        logComponent,
      );
      await emitIfPresent(
        engine,
        stderrPath,
        'pytorch_collect_stderr.txt',
        logComponent,
      );

      // Sending SIGINT to the collector will result in different exit codes
      // depending on whether Python had a chance to install its signal handlers
      const stoppedByRequest =
        ctx.metadata.requestStop === true &&
        (result.exitCode === 130 || result.exitCode === -1);
      if (result.exitCode !== 0 && !stoppedByRequest) {
        throw {
          code: 'tool_integrations.pytorch_collect.RUN_FAILED',
          metadata: { reason: `exit_code_${result.exitCode}` },
        };
      }

      const completionMarker = await engine.execCommand(
        ['stat', completionMarkerPath],
        {},
      );
      if (completionMarker.rc !== 0 && stoppedByRequest) {
        return;
      }
      if (completionMarker.rc !== 0) {
        throw {
          code: 'tool_integrations.pytorch_collect.RUN_FAILED',
          metadata: { reason: 'expected_output_missing' },
        };
      }

      const apiCallsPresent = await emitIfPresent(
        engine,
        apiCallsPath,
        'api_calls.parquet',
        apiCallsComponent,
      );
      const operatorCallsPresent = await emitIfPresent(
        engine,
        operatorCallsPath,
        'operator_calls.parquet',
        operatorCallsComponent,
      );
      if (!apiCallsPresent || !operatorCallsPresent) {
        throw {
          code: 'tool_integrations.pytorch_collect.RUN_FAILED',
          metadata: { reason: 'expected_output_missing' },
        };
      }
    } catch (error) {
      if (error?.code) {
        throw error;
      }
      engine.log(
        'error',
        `PyTorch collector failed: ${error?.message ?? error}`,
      );
      throw {
        code: 'tool_integrations.pytorch_collect.RUN_FAILED',
        metadata: { reason: 'collector_process_failed' },
      };
    } finally {
      ctx.metadata.processHandle = null;
      engine.endProgress('Collecting PyTorch call data');
    }
  },

  reformat: async () => {},
  onStop: async (engine, ctx) => {
    ctx.metadata = ctx.metadata || {};
    ctx.metadata.requestStop = true;
    await interruptCollector(engine, ctx);
  },
  onCancel: async (_engine, ctx) => {
    ctx.metadata = ctx.metadata || {};
    ctx.metadata.requestCancel = true;
    if (ctx.metadata.processHandle) {
      await ctx.metadata.processHandle.kill();
    }
  },
};
