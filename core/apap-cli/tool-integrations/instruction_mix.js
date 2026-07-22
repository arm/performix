// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  probePythonVenv,
  ensureDeployed,
  probeWhl,
  posixTestWorkload,
  buildToolBundlePath,
} = require('./utils');
const { getExecutableFromWorkload } = require('./workload');

let bundleVersion = '0.4.5';
let toolFile = `instruction_mix-${bundleVersion}-py3-none-any.whl`;
/**
 * Resolve the deployment path for the instruction_mix wheel.
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {string}
 */
function getDeployPath(ctx) {
  const toolsRoot = ctx.toolsRoot;
  return buildToolBundlePath(toolsRoot, toolFile, bundleVersion);
}
let readinessMessageCode =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';

const PYTHON_VER_MAJOR = 3;
const PYTHON_VER_MINOR = 6;

/**
 * @type {import("../recipes/docs/jsdocs").ToolIntegration}}
 */
let tool = {
  name: 'instruction_mix',
  version: '1.1.0',
  supportsWorkloadLaunch: true,
  description: {
    short: 'Instruction mix tool for analysis of instruction mixes.',
    long: 'The instruction mix tool performs analysis of binaries to provide insights into the instruction mix executed by a workload. It helps identify performance bottlenecks and optimization opportunities by analyzing the distribution of different instruction types within the binary.',
  },
  parameters: [
    {
      id: 'customRecipe',
      label: 'Custom recipe invocation',
      description:
        'Indicates that the recipe invoking this tool integration is third-party.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
  ],
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
  migrations: [
    {
      type: 'missingInvocation',
      from: 'instruction_mix',
      version: '1.1.0',
    },
  ],
  probe: async (engine, ctx) => {
    /** @type {import("../recipes/docs/jsdocs").ProbeAdvice[]} */
    let advice = [];
    const deployPath = getDeployPath(ctx);

    let py = await probePythonVenv(
      engine,
      PYTHON_VER_MAJOR,
      PYTHON_VER_MINOR,
      tool.name,
    );
    if (py.level !== 'ready') advice.push(py);

    let od = await probeObjdump(engine);
    if (od.level !== 'ready') advice.push(od);

    let whl = await probeWhl(engine, deployPath, tool.name);
    if (whl.level !== 'ready') advice.push(whl);

    if (ctx.workload) {
      if (ctx.workload.type !== 'launch') {
        advice.push({
          level: 'error',
          messageCode: 'tool_integrations.instruction_mix.NON_LAUNCH_WORKLOAD',
          metadata: {
            workloadType: ctx.workload.type,
          },
        });
      } else if (ctx.workload.useShell) {
        advice.push({
          level: 'error',
          messageCode: 'tool_integrations.instruction_mix.USE_SHELL',
          metadata: {},
        });
      }
    }

    return {
      available: advice.length === 0,
      capabilities: {},
      advice,
    };
  },

  run: async (engine, ctx) => {},

  reformat: async (engine, ctx) => {
    if (ctx.workload.type !== 'launch') {
      throw {
        code: 'tool_integrations.instruction_mix.NON_LAUNCH_WORKLOAD',
        metadata: { workloadType: ctx.workload.type },
      };
    } else if (ctx.workload.useShell) {
      throw {
        code: 'tool_integrations.instruction_mix.USE_SHELL',
        metadata: {},
      };
    }

    const deployPath = await getDeployPath(ctx);
    // Use raw workload as static instruction mix tool expects a single value for the `--workload` flag
    let rawWorkload = ctx.workload.rawCommand;
    let outputDir = await engine.createTempDir();

    engine.emitOutput(
      outputDir + '/data/static_instruction_mix.csv',
      'static_instruction_mix.csv',
      { name: 'static_instruction_mix', version: '1.0' },
    );
    engine.emitOutput(
      outputDir + '/data/instruction_mix_log.txt',
      'instruction_mix_log.txt',
      { name: 'log-text', version: '1.0' },
    );

    let createVenvResult = await engine.execCommand(
      ['python3', '-m', 'venv', outputDir + '/venv'],
      {},
    );
    if (createVenvResult.rc !== 0) {
      throw {
        code: 'tool_integrations.common.CREATE_PYTHON_VENV',
        metadata: {
          tool: tool.name,
          pythonVersion: '3.6',
          exitCode: createVenvResult.rc,
        },
      };
    }

    await ensureDeployed(engine, deployPath, tool.name);

    let installResult = await engine.execCommand(
      [outputDir + '/venv/bin/pip', 'install', deployPath],
      {},
    );
    if (installResult.rc !== 0) {
      throw {
        code: 'tool_integrations.common.INSTALL_MODULE',
        metadata: {
          tool: tool.name,
          exitCode: installResult.rc,
          deployPath: deployPath,
        },
      };
    }

    let testCmd = [outputDir + '/venv/bin/instruction-mix', '--help'];
    let testResult = await engine.execCommand(testCmd, {});
    if (testResult.rc !== 0) {
      // This throw is fine as it will be scooped up into a SCRIPTED_STAGE_ERROR later (and there's no advice here anyway)
      throw `instruction_mix tool installed successfully, but ${testCmd.join(' ')} exited with code ${testResult.rc}`;
    }

    const runAsPrivileged = await isPrivilegeRequired(engine, ctx);

    let processArgs = [
      outputDir + '/venv/bin/instruction-mix',
      '--static',
      '--workload',
      rawWorkload,
      '--outdir',
      outputDir + '/data',
    ];

    let opts = {
      asPrivileged: runAsPrivileged,
      workingDirectory: ctx.workload.workingDir,
    };

    engine.log(
      'info',
      `Starting ${tool.name} with args: ${processArgs.join(' ')}; options: ${JSON.stringify(opts)}`,
    );
    engine.startProgressTracker(`Collecting ${tool.name} data`);

    let runHandle = await engine.startProcess(processArgs, opts);
    ctx.metadata.runHandle = runHandle;

    let runResult = await runHandle.wait();
    // Switch on exit code to provide more specific advice if possible
    switch (runResult.exitCode) {
      case 0:
        break;
      case 1:
        throw {
          code: 'tool_integrations.common.WORKLOAD_NOT_EXECUTABLE',
          metadata: {
            workload: ctx.workload.rawCommand,
            executable: getExecutableFromWorkload(ctx.workload.command),
          },
        };
      case 2:
        throw {
          code: 'tool_integrations.instruction_mix.DISASSEMBLER_NOT_FOUND',
          metadata: { disassembler: 'objdump' },
        };
      case 3:
        throw {
          code: 'tool_integrations.instruction_mix.DISASSEMBLER_INVOCATION_FAILED',
          metadata: { disassembler: 'objdump' },
        };
      case 4:
        throw { code: 'tool_integrations.instruction_mix.MRS_LOAD_FAILED' };
      case 5:
        throw {
          code: 'tool_integrations.instruction_mix.DECODE_ERROR',
          metadata: { disassembler: 'objdump', log: 'instruction_mix_log.txt' },
        };
      case 6:
        throw { code: 'tool_integrations.instruction_mix.OUTPUT_WRITE_FAILED' };
      case 7:
        if (ctx.params['customRecipe'] === true) {
          throw {
            code: 'tool_integrations.common.INVALID_ARGUMENTS_THIRD_PARTY',
            metadata: { tool: tool.name, cmd: 'instruction-mix --help' },
          };
        } else {
          throw {
            code: 'tool_integrations.common.INVALID_ARGUMENTS',
            metadata: { tool: tool.name, cmd: 'instruction-mix --help' },
          };
        }
      case 8:
        throw {
          code: 'tool_integrations.instruction_mix.DYNAMIC_ANALYSIS_ERROR',
          metadata: { log: 'instruction_mix_log.txt' },
        };
      case 12:
        throw {
          code: 'tool_integrations.common.WORKLOAD_NOT_EXIST',
          metadata: {
            workload: ctx.workload.rawCommand,
            executable: getExecutableFromWorkload(ctx.workload.command),
          },
        };
      case 99:
      default:
        engine.log(
          'error',
          `${tool.name} tool failed with unknown exit code ${runResult.exitCode}`,
        );
        throw { code: 'tool_integrations.instruction_mix.UNEXPECTED_ERROR' };
    }

    engine.endProgress(`Collecting ${tool.name} data`);
  },

  onCancel: async (engine, ctx) => {
    await ctx.metadata.runHandle.kill();
  },

  onStop: async (engine, ctx) => {},
};

/**
 * Checks that `objdump` exists (binutils).
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @returns {Promise<import("../recipes/docs/jsdocs").ProbeAdvice>}
 */
async function probeObjdump(engine) {
  // Note we have to use "bash -c" instead of just running "objdump --version"
  // as execCommand will error if the command is not found. Should the agent have a "command exists" method?
  let odCheck = await engine.execCommand(
    ['bash', '-c', 'objdump --version'],
    {},
  );
  if (odCheck.rc !== 0) {
    return {
      level: 'error',
      messageCode: readinessMessageCode,
      metadata: {
        message:
          'objdump is not available on the target machine. Install the binutils system package in order to run static instruction mix.',
      },
    };
  }

  return {
    level: 'ready',
    messageCode: '',
  };
}

/**
 * Determines if instruction_mix requires privileged access on the target.
 *
 * @param {import("../recipes/docs/jsdocs").Engine} engine
 * @param {import("../recipes/docs/jsdocs").ToolContext} ctx
 * @returns {Promise<boolean>}
 */
async function isPrivilegeRequired(engine, ctx) {
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

  return false;
}
