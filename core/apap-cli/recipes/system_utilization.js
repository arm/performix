// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const {
  render: renderSystemUtilization,
  getNeoprofTimelineSchema,
  buildNeoprofTimelineRenderer,
} = require('./analyses/system_utilization');

const TOOL_SYSUTIL_TIMELINE = {
  name: 'sysutil-timeline',
  version: '1.0.0',
};
const TOOL_NEOPROF = { name: 'neoprof', version: '1.1.0' };

const GPU_ACTIVITY_METRICS = [
  'GPUFront-end.GPUCycles.GPUActive',
  'GPUFront-end.GPUUtilization',
];

const NEOPROF_SYSUTIL_WORKFLOW = 'sys_util';
const { collectToolAdvice, toolStatusToRecipeStatus } = recipeUtils;

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
const recipe = {
  name: 'system_utilization',
  title: 'System Utilization',
  version: '1.0',
  api_version: '1.0.0',
  status: 'preview',
  description:
    'Analyze CPU, GPU, memory, disk, and network activity over time to identify system resource bottlenecks.',
  deployments: [
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Android' }],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_NEOPROF.name,
          version: TOOL_NEOPROF.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: [
        {
          type: 'tool',
          name: TOOL_NEOPROF.name,
          version: TOOL_NEOPROF.version,
          requiredWhen: { type: 'always' },
        },
        {
          type: 'tool',
          name: TOOL_SYSUTIL_TIMELINE.name,
          version: TOOL_SYSUTIL_TIMELINE.version,
          requiredWhen: { type: 'always' },
        },
      ],
    },
  ],
  parameters: [
    {
      id: 'interval',
      required: false,
      label: 'Interval',
      description: 'Sampling interval in seconds (0.01 to 60).',
      config: {
        type: 'input',
        defaultValue: '1.0',
      },
    },
    {
      id: 'thread_scan_interval',
      required: false,
      label: 'Thread Scan Interval',
      description: 'Thread scan interval in seconds (defaults to interval).',
      config: {
        type: 'input',
        defaultValue: '1',
      },
    },
  ],
  parameterValidation: (context) => {
    const value = context.getParameter('interval');
    const interval = Number(value);
    if (!Number.isFinite(interval) || interval < 0.01 || interval > 60) {
      return {
        errors: [
          {
            parameterId: 'interval',
            value,
            messageCode: 'tool_integrations.sysutil_timeline.INVALID_INTERVAL',
            metadata: { value: String(value), min: '0.01', max: '60' },
          },
        ],
      };
    }
    return { errors: [] };
  },
  readyStages: [
    {
      name: 'Checking recipe is ready',
      description:
        'Check dependencies and parameters specified for the System Utilization recipe.',
      exec: readySystemUtilization,
    },
  ],
  runStages: [
    {
      name: 'Collecting utilization data',
      description:
        'Run the System Utilization recipe to capture utilization data.',
      exec: runSystemUtilization,
    },
  ],
  renderStages: [
    {
      name: 'Creating render',
      description:
        'Create the renderer specs that are used to produce visualizations',
      exec: renderSystemUtilization,
    },
  ],
};

/**
 * generateSysutilTimelineConfig generates a ToolConfigurationsArg for the
 * sysutil-timeline tool integration.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {Object.<string, any>} params
 * @return {import("./docs/jsdocs").ToolConfigurationsArg}
 */
function generateSysutilTimelineConfig(workload, params) {
  return {
    toolConfigs: [
      {
        name: TOOL_SYSUTIL_TIMELINE.name,
        params: params,
        workload: workload,
        env: {},
      },
    ],
  };
}

/**
 * Generates the NeoProf configuration used to collect system-utilization
 * counters, analyzing on the host for Android and on the target for Linux.
 * @param {import("./docs/jsdocs").Workload} workload
 * @param {import("./docs/jsdocs").TargetInfoDescription} targetInfo
 * @param {string} interval Sampling interval in seconds.
 * @return {import("./docs/jsdocs").ToolConfigurationsArg}
 */
function generateNeoprofSysutilConfig(workload, targetInfo, interval) {
  const timelineDeviceNumbers = (targetInfo?.CPUs ?? [])
    .filter(
      ({ CoreNumber }) => Number.isSafeInteger(CoreNumber) && CoreNumber >= 0,
    )
    .sort((left, right) => left.CoreNumber - right.CoreNumber)
    .map(({ CoreNumber }) => CoreNumber);

  return {
    toolConfigs: [
      {
        name: TOOL_NEOPROF.name,
        params: {
          mode: 'samples',
          sampling_frequency: 'normal',
          workflow: NEOPROF_SYSUTIL_WORKFLOW,
          requested_gpu_metrics: JSON.stringify(GPU_ACTIVITY_METRICS),
          poll_interval: String(Math.round(Number(interval) * 1000)),
          reformat_on_host: isAndroidTarget(targetInfo),
          timeline_device_numbers: JSON.stringify(timelineDeviceNumbers),
          rich_data_capture: false,
        },
        workload: workload,
        env: {},
      },
    ],
  };
}

/**
 * @param {import("./docs/jsdocs").TargetInfoDescription} targetInfo
 * @returns {boolean}
 */
function isAndroidTarget(targetInfo) {
  const family = targetInfo?.Os?.OSFamily ?? '';
  return family.toLowerCase() === 'android';
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext | import("./docs/jsdocs").RunExecutionContext} context
 * @param {import("./docs/jsdocs").TargetInfoDescription} targetInfo
 * @returns {boolean}
 */
function usesNeoprofSystemUtilization(context, targetInfo) {
  return (
    isAndroidTarget(targetInfo) ||
    (targetInfo?.Os?.OSFamily?.toLowerCase() === 'linux' &&
      context.isNeoprofSystemUtilizationEnabled())
  );
}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 */
function readySystemUtilization(context) {
  const workload = context.getWorkload();
  const targetInfo = context.targetInfo();
  if (usesNeoprofSystemUtilization(context, targetInfo)) {
    const tools = generateNeoprofSysutilConfig(
      workload,
      targetInfo,
      context.getParameter('interval'),
    );
    const toolResponses = context.probeTools(tools);
    const allAdvice = collectToolAdvice(tools, toolResponses);

    return {
      status: toolStatusToRecipeStatus(allAdvice),
      advice: allAdvice,
    };
  }

  const params = {
    interval: context.getParameter('interval'),
    thread_scan_interval: context.getParameter('thread_scan_interval'),
  };

  const tools = generateSysutilTimelineConfig(workload, params);
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
function runSystemUtilization(context) {
  const workload = context.getWorkload();
  const targetInfo = context.targetInfo();
  if (usesNeoprofSystemUtilization(context, targetInfo)) {
    context.runTools(
      generateNeoprofSysutilConfig(
        workload,
        targetInfo,
        context.getParameter('interval'),
      ),
    );
    return;
  }

  const params = {
    interval: context.getParameter('interval'),
    thread_scan_interval: context.getParameter('thread_scan_interval'),
  };

  context.runTools(generateSysutilTimelineConfig(workload, params));
}
