// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Comparisons use stored results, so expose their core types without binding
// either run's value to the shared, editable render parameters.
function getComparisonFilters(context) {
  const coreTypesByRun = context.getRunDescriptions().map((_, runIndex) => {
    const cpus = JSON.parse(
      context.readRunComponent(
        runIndex,
        'collector/sl-collect-target-info/sl-collect-target-info-cpus.json',
      ),
    );
    return [...new Set(cpus.map((cpu) => cpu.name))];
  });
  if (!coreTypesByRun.some((coreTypes) => coreTypes.length > 1)) return [];

  return [
    {
      id: 'core_type',
      title: 'Core type',
      type: 'comparison_filter',
      rendererId: 'target_info',
      description: 'Include data from a selected CPU core type.',
      disabled: { reason: 'Filters cannot be changed while comparing runs.' },
      parameterBindings: {},
      config: {
        warning:
          'Core type filtering still applies to the runs being compared. Filters cannot be changed while comparing runs.',
        values: [
          {
            label: 'Current run',
            value: context.getFirstSupportedCpuName(1) ?? 'Unknown',
            options: coreTypesByRun[1],
          },
          {
            label: 'Baseline run',
            value: context.getFirstSupportedCpuName(0) ?? 'Unknown',
            options: coreTypesByRun[0],
          },
        ],
      },
    },
  ];
}

module.exports = { getComparisonFilters };
