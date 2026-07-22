// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import "github.com/dop251/goja"

const recipeUtilsScript = `
var recipeUtils = Object.freeze({
  toolStatusToRecipeStatus(advice) {
    const rank = { ready: 0, unknown: 1, warning: 2, error: 3 };
    return advice.reduce(
      (worst, { AdviceSeverity }) =>
        (rank[AdviceSeverity] ?? 0) > (rank[worst] ?? 0) ? AdviceSeverity : worst,
      'ready',
    );
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
`

func setRecipeUtilsGlobal(vm *goja.Runtime) error {
	_, err := vm.RunString(recipeUtilsScript)
	return err
}
