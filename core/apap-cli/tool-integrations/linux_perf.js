// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  ensureDeployed,
  buildToolBundlePath,
  probePythonVenv,
} = require('./utils');

const TOOL_NAME = 'linux_perf';
const VERSION = '1.0.0';
const POST_PROCESS_TOOL_NAME = 'cache_sharing';
const bundleVersion = '1.0.0';
const toolFile = `cache_sharing-${bundleVersion}-py3-none-any.whl`;
const readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';

const PYTHON_VER_MAJOR = 3;
const PYTHON_VER_MINOR = 5;

/**
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {string}
 */
function getDeployPath(ctx) {
  return buildToolBundlePath(ctx.toolsRoot, toolFile, bundleVersion);
}

/**
 * @type {import("../recipes/docs/jsdocs").ToolIntegration}
 */
const tool = {
  name: TOOL_NAME,
  version: VERSION,
  supportsWorkloadLaunch: true,

  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Linux' }],
      dependencies: [
        {
          type: 'tool_bundle',
          name: toolFile,
          version: bundleVersion,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],

  description: {
    short: 'Linux `perf` runner',
    long: 'Runs `perf` with whatever arguments are passed from the recipe.',
  },
  parameters: [
    {
      id: 'perfArgs',
      label: 'perf arguments',
      description:
        'Arguments passed verbatim to `perf` (e.g. "c2c record -u").',
      config: { type: 'input' },
    },
  ],

  probe: async (engine, ctx) => {
    const advice = [];

    const { rc } = await engine.execCommand(['perf', '--version'], {});
    if (rc !== 0) {
      advice.push({
        level: 'error',
        messageCode: readinessMessageCode,
        metadata: {
          message: '`perf` not found on PATH',
        },
      });
    }

    const py = await probePythonVenv(
      engine,
      PYTHON_VER_MAJOR,
      PYTHON_VER_MINOR,
      POST_PROCESS_TOOL_NAME,
    );
    if (py.level !== 'ready') {
      advice.push(py);
    }

    return {
      available: advice.length === 0,
      capabilities: {},
      advice,
    };
  },

  run: async (engine, ctx) => {
    engine.log(
      'info',
      'Running perf c2c with elevated privileges (physical address capture requires it).',
    );

    const outputDir = (await engine.createTempDir()) + '/';
    ctx.metadata.outputDirectory = outputDir;

    const perfArgValue = ctx.params.perfArgs;
    if (perfArgValue === undefined || perfArgValue === null) {
      throw new Error('perfArgs is required for linux_perf');
    }
    const recordArgs = String(perfArgValue).trim().split(/\s+/).filter(Boolean);
    const perfPath = `${ctx.metadata.outputDirectory}perf.data`;
    const errPath = `${ctx.metadata.outputDirectory}perf.err.log`;

    // Build perf command; append workload args if present.
    let cmd = ['perf', ...recordArgs, '-o', perfPath];
    const wl = ctx.workload;
    if (wl && wl.type === 'launch') {
      // Workload command is already normalized to an array per jsdocs; fall back to splitting string if needed.
      const wlArgs = Array.isArray(wl.command)
        ? wl.command
        : String(wl.command || wl.rawCommand || '')
            .trim()
            .split(/\s+/)
            .filter(Boolean);
      if (wlArgs.length > 0) {
        cmd = cmd.concat(['--', ...wlArgs]);
      }
    } else if (wl && wl.type === 'attach') {
      cmd = cmd.concat(['--', '-p', String(wl.pid)]);
    }
    engine.log('info', `Running: ${cmd.join(' ')}`);

    const handle = await engine.startProcess(cmd, {
      asPrivileged: true,
      stderr: { redirect: 'file', path: errPath },
    });
    ctx.metadata.recordHandle = handle;

    const result = await handle.wait();
    ctx.metadata.recordHandle = null;
    // always emit the perf stderr log to the run
    try {
      engine.emitOutput(errPath, 'perf.err.log', {
        name: 'log-text',
        version: '1.0',
      });
    } catch (e) {
      engine.log('warn', `Failed to emit perf.err.log: ${e}`);
    }
    if (result.exitCode === 0) {
      // relax permissions so the non-root retrieval path can read the data
      try {
        await engine.execCommand(['chmod', '644', perfPath], {
          asPrivileged: true,
        });
      } catch (e) {
        engine.log('warn', `Failed to chmod perf.data: ${e}`);
      }
      return;
    }

    let errText = '';
    try {
      const { stdout, stderr } = await engine.execCommand(['cat', errPath], {
        asPrivileged: true,
      });
      errText = (stdout || stderr || '').trim();
    } catch (e) {
      errText = '';
    }
    const cause = errText
      ? `perf exited ${result.exitCode}: ${errText}`
      : `perf exited ${result.exitCode}`;
    engine.log('error', cause);
    throw new Error(cause);
  },

  reformat: async (engine, ctx) => {
    const outputDir = ctx.metadata.outputDirectory;
    const perfPath = `${outputDir}perf.data`;
    const venvDir = `${outputDir}venv`;
    const deployPath = getDeployPath(ctx);
    const outPrefix = `${outputDir}perf_c2c`;
    const progressTrackerId = 'Analyzing collection';

    engine.startProgressTracker(progressTrackerId);

    let createVenvResult = await engine.execCommand(
      ['python3', '-m', 'venv', venvDir],
      { asPrivileged: true },
    );
    if (createVenvResult.rc !== 0) {
      throw {
        code: 'tool_integrations.common.CREATE_PYTHON_VENV',
        metadata: {
          tool: POST_PROCESS_TOOL_NAME,
          pythonVersion: `${PYTHON_VER_MAJOR}.${PYTHON_VER_MINOR}`,
          exitCode: createVenvResult.rc,
        },
      };
    }

    await ensureDeployed(engine, deployPath, POST_PROCESS_TOOL_NAME);

    let installResult = await engine.execCommand(
      [`${venvDir}/bin/pip`, 'install', deployPath],
      { asPrivileged: true },
    );
    if (installResult.rc !== 0) {
      throw {
        code: 'tool_integrations.common.INSTALL_MODULE',
        metadata: {
          tool: POST_PROCESS_TOOL_NAME,
          exitCode: installResult.rc,
          deployPath,
        },
      };
    }

    let testCmd = [`${venvDir}/bin/cache-sharing`, '--help'];
    let testResult = await engine.execCommand(testCmd, {
      asPrivileged: true,
    });
    if (testResult.rc !== 0) {
      throw `${POST_PROCESS_TOOL_NAME} installed successfully, but ${testCmd.join(' ')} exited with code ${testResult.rc}`;
    }

    let processArgs = [
      `${venvDir}/bin/cache-sharing`,
      '--perf-data',
      perfPath,
      '--out-prefix',
      outPrefix,
    ];
    engine.log(
      'info',
      `Starting cache-sharing with args: ${processArgs.join(' ')}`,
    );

    let result = await engine.execCommand(processArgs, {
      asPrivileged: true,
    });
    if (result.rc !== 0) {
      throw {
        code: 'tool_integrations.linux_perf.POST_PROCESS_FAILED',
        metadata: {
          exitCode: result.rc,
          stdout: result.stdout || '',
          stderr: result.stderr || '',
          script: processArgs[0],
        },
      };
    }

    const outputs = [
      'perf_c2c_output_parser_log.json',
      'perf_c2c_output_cachelines.csv',
      'perf_c2c_output_accesses.csv',
      'perf_c2c_output_symbols.json',
    ];
    for (const file of outputs) {
      const fullPath = `${outputDir}${file}`;
      const exists = await engine.execCommand(['stat', fullPath], {
        asPrivileged: true,
      });
      if (exists.rc !== 0) {
        throw {
          code: 'tool_integrations.linux_perf.MISSING_OUTPUT',
          metadata: { file },
        };
      }
      await engine.execCommand(['chmod', '644', fullPath], {
        asPrivileged: true,
      });
      const componentType = file.endsWith('symbols.json')
        ? { name: 'sl-collect-symbols', version: '1.0.0' }
        : { name: 'perf-c2c-output', version: '1.0.0' };
      engine.emitOutput(fullPath, `output/${file}`, componentType);
    }

    engine.endProgress(progressTrackerId);
  },

  onCancel: async (engine, ctx) => {
    if (ctx.metadata.recordHandle) ctx.metadata.recordHandle.kill();
  },

  onStop: async (engine, ctx) => {
    if (ctx.metadata.recordHandle) await ctx.metadata.recordHandle.interrupt();
  },
};
