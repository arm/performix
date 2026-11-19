// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  render: renderSyscallTraceSummary,
} = require('./analyses/syscall_trace_summary');
const TOOL_SYSCALL_TRACE = {
  name: 'syscall-trace',
  version: '1.0.0',
};

const READINESS_MESSAGE_CODE =
  'engine.recipeparser.js_recipe_stage.READINESS_MESSAGE';
const UNSUPPORTED_WORKLOAD_CODE =
  'tool_integrations.syscall_trace.UNSUPPORTED_WORKLOAD';
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
const recipe = {
  name: 'syscall_trace_summary',
  title: 'Syscall Trace Summary',
  version: '1.0.0',
  api_version: '1.0.0',
  status: 'stable',
  description:
    'Trace Linux system calls to identify frequent calls, slow operations, and repeated failures.',
  mcp_guidance:
    'This recipe supports launch and attach workloads only. Do not run it with the system workload because system-wide syscall tracing is not supported.',
  deployments: [
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_SYSCALL_TRACE.name,
          version: TOOL_SYSCALL_TRACE.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  parameters: [],
  readyStages: [
    {
      name: 'Check Syscall Trace Summary readiness',
      description: 'Check that syscall tracing can run on the target.',
      exec: readySyscallTrace,
    },
  ],
  runStages: [
    {
      name: 'Validate workload type',
      description:
        'Ensure syscall tracing is run for launch or attach workloads.',
      exec: validateWorkloadType,
    },
    {
      name: 'Collect syscall trace data',
      description: 'Collect syscall trace output for the selected workload.',
      exec: runSyscallTrace,
    },
  ],
  renderStages: [
    {
      name: 'Render syscall trace summary',
      description: 'Create syscall trace summary tables from Parquet output.',
      exec: renderSyscallTraceSummary,
    },
  ],
};

/**
 * @param {import("./docs/jsdocs").Workload} workload
 * @returns {boolean}
 */
function isSystemWideWorkload(workload) {
  return workload?.Type === 'systemWide';
}

/**
 * @returns {import("./docs/jsdocs").ToolConfigurationsArg}
 * @param {import("./docs/jsdocs").Workload} workload
 */
function buildSyscallTraceTools(workload) {
  return {
    toolConfigs: [
      {
        name: TOOL_SYSCALL_TRACE.name,
        params: {},
        workload,
        env: {},
      },
    ],
  };
}

/**
 * @param {import("./docs/jsdocs").Workload} workload
 * @returns {import("./docs/jsdocs").RecipeReadyAdvice | undefined}
 */
function unsupportedWorkloadAdvice(workload) {
  if (!isSystemWideWorkload(workload)) {
    return undefined;
  }
  return {
    ToolName: TOOL_SYSCALL_TRACE.name,
    AdviceSeverity: 'error',
    MessageCode: READINESS_MESSAGE_CODE,
    Metadata: {
      message:
        'Syscall Trace Summary supports launch and attach workloads. System-wide tracing is not supported.',
    },
    Cause: '',
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readySyscallTrace(context) {
  const workload = context.getWorkload();
  const workloadAdvice = unsupportedWorkloadAdvice(workload);
  if (workloadAdvice) {
    return {
      status: 'error',
      advice: [workloadAdvice],
    };
  }

  const tools = buildSyscallTraceTools(workload);
  const toolResponses = context.probeTools(tools);
  const allAdvice = collectToolAdvice(tools, toolResponses);
  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function validateWorkloadType(context) {
  if (isSystemWideWorkload(context.getWorkload())) {
    throw {
      code: UNSUPPORTED_WORKLOAD_CODE,
      metadata: { workloadType: 'system-wide' },
    };
  }
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runSyscallTrace(context) {
  context.runTools(buildSyscallTraceTools(context.getWorkload()));
}
