// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Memory Access Recipe Definition

// @ts-check
const { render: renderMemoryAccess } = require('./analyses/memory_access');

let tool_name = 'neoprof';

let tool_version = '1.1.0';
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'memory_access',
  title: 'Memory Access',
  version: '1.0',
  api_version: '1.0.0',
  status: 'stable',
  description:
    'Find functions with slow memory accesses and see whether data comes from caches or main memory.',
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Linux' }],
      dependencies: [
        {
          type: 'tool',
          name: tool_name,
          version: tool_version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  parameters: [
    {
      id: 'min_latency_cycles',
      required: false,
      label: 'Minimum Latency Cycles',
      description:
        'Set the minimum latency cycles. This parameter allows you to filter out memory accesses that are below a certain latency threshold, focusing on more significant memory operations.',
      config: {
        type: 'input',
        defaultValue: '',
      },
    },
    {
      id: 'spe_sample_rate',
      required: false,
      label: 'SPE Sampling Rate',
      description:
        'Set the Arm SPE periodic sampling rate (operations between each sample). Must be a non-zero positive integer; hardware minimums apply.',
      config: {
        type: 'input',
        defaultValue: '',
      },
    },
    {
      id: 'collect_java_stacks',
      required: false,
      label: 'Collect Java stacks',
      description:
        'Enable collection of Java stack traces when profiling JVM workloads.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
    {
      id: 'collect_dotnet_stacks',
      required: false,
      label: 'Collect .NET stacks',
      description:
        'Enable collection of .NET stack traces when profiling .NET workloads.',
      config: {
        type: 'checkbox',
        defaultValue: false,
      },
    },
  ],
  readyStages: [
    {
      name: 'Checking Neoprof is ready',
      description: 'Check that the target can run the Neoprof collector',
      exec: readyNeoprof,
    },
    {
      name: 'Checking SPE is ready',
      description: 'Check that the target is configured for SPE profiling',
      exec: readySPE,
    },
  ],
  runStages: [
    {
      name: 'Collecting SPE samples',
      description:
        'Collect SPE samples on the target and processes the captured data',
      exec: runMemoryAccess,
    },
  ],
  renderStages: [
    {
      name: 'Creating render',
      description:
        'Create the renderer specs that are used to produce visualizations',
      exec: renderMemoryAccess,
    },
  ],
};

/**
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {string} workflow
 */
function getToolsArg(workload, workflow) {
  let toolsArg = {
    tools: [
      {
        name: tool_name,
        args: ['-X', workflow],
      },
    ],
    workload: workload,
  };
  return toolsArg;
}

/**
 * generateNeoprofConfig generates a ToolConfigurationsArg for the
 * neoprof tool integration.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {Object.<string, any>} params
 * @return {import("./docs/jsdocs").ToolConfigurationsArg}
 */
function generateNeoprofConfig(workload, params) {
  return {
    toolConfigs: [
      {
        name: tool_name,
        params: params,
        workload: workload,
        env: {},
      },
    ],
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext | import("./docs/jsdocs").RunExecutionContext} context
 */
function computeSpeWorkflow(context) {
  let workflow = 'workflow_spe';
  let minLatency = context.getParameter('min_latency_cycles');
  if (minLatency) {
    workflow = `workflow_spe:min_latency=${minLatency}`;
  }
  return workflow;
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readyNeoprof(context) {
  let workload = context.getWorkload();
  const speSampleRate = context.getParameter('spe_sample_rate');

  let params = {
    mode: 'spe',
    spe_sample_rate: speSampleRate,
    collect_java_stacks: context.getParameter('collect_java_stacks'),
    collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
  };
  let tools = generateNeoprofConfig(workload, params);
  let toolResponses = context.probeTools(tools);

  let allAdvice = collectToolAdvice(tools, toolResponses);

  return {
    status: toolStatusToRecipeStatus(allAdvice),
    advice: allAdvice,
  };
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readySPE(context) {
  /* This is a temporary check until the profiler supports a recipe readiness
   * probe for this. It would be better to create a trial SPE event but that
   * requires perf to be installed, so just check sysfs instead for now. */
  let readySPE = context.runCommand({
    type: 'exec',
    cmd: 'ls -d /sys/bus/event_source/devices/arm_spe_* 2>/dev/null',
  });
  if (readySPE.ReturnCode != 0) {
    return {
      status: 'error',
      advice: [
        {
          ToolName: tool_name,
          AdviceSeverity: 'error',
          MessageCode: 'tool_integrations.common.SPE_NOT_CONFIGURED',
          Metadata: {},
          Cause: '',
        },
      ],
    };
  }

  return { status: 'ready', advice: [] };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runMemoryAccess(context) {
  let workload = context.getWorkload();
  let workflow = computeSpeWorkflow(context);
  const speSampleRate = context.getParameter('spe_sample_rate');
  let params = {
    mode: 'spe',
    spe_workflow: workflow,
    spe_sample_rate: speSampleRate,
    collect_java_stacks: context.getParameter('collect_java_stacks'),
    collect_dotnet_stacks: context.getParameter('collect_dotnet_stacks'),
  };
  context.runTools(generateNeoprofConfig(workload, params));
}
