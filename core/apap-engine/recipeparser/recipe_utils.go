// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipeparser

import (
	_ "embed"

	"github.com/dop251/goja"
)

//go:embed recipe_utils.js
var recipeUtilsScript string

func setRecipeUtilsGlobal(vm *goja.Runtime) error {
	_, err := vm.RunString(recipeUtilsScript)
	return err
}
