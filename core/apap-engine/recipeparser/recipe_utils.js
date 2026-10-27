// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

var recipeUtils = Object.freeze({
  toolStatusToRecipeStatus(advice) {
    const rank = { ready: 0, unknown: 1, warning: 2, error: 3 };
    return advice.reduce(
      (worst, { AdviceSeverity }) =>
        (rank[AdviceSeverity] ?? 0) > (rank[worst] ?? 0)
          ? AdviceSeverity
          : worst,
      'ready',
    );
  },

  getPartialCPUSupportAdvice(context, messageCode) {
    const cpus = [...context.targetInfo().CPUs].sort(
      (left, right) => left.CoreNumber - right.CoreNumber,
    );
    const supportByName = new Map();
    for (const cpu of cpus) {
      if (!supportByName.has(cpu.Name)) {
        supportByName.set(
          cpu.Name,
          Boolean(context.getTelemetrySpecification(cpu.Name)),
        );
      }
    }
    const unsupportedCPUs = cpus.filter((cpu) => !supportByName.get(cpu.Name));
    if (
      unsupportedCPUs.length === 0 ||
      unsupportedCPUs.length === cpus.length
    ) {
      return undefined;
    }

    return {
      ToolName: '',
      AdviceSeverity: 'warning',
      MessageCode: messageCode,
      Metadata: {
        cpuNames: [...new Set(unsupportedCPUs.map((cpu) => cpu.Name))].join(
          ', ',
        ),
        coreNumbers: unsupportedCPUs.map((cpu) => cpu.CoreNumber).join(', '),
      },
      Cause: '',
    };
  },

  collectToolAdvice(tools, toolResponses) {
    return toolResponses.flatMap((tr, i) =>
      (tr.advice ?? []).map((a) => ({
        ToolName: tools.toolConfigs[i].name,
        AdviceSeverity: a.level,
        MessageCode: a.messageCode ?? '',
        Metadata: a.metadata ?? {},
        Cause: a.cause ?? '',
      })),
    );
  },
});
