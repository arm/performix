// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"context"

	"github.com/Arm-Debug/apap-cli/apap-engine/recipe"
)

// ParameterOptionsEvaluator computes dynamic parameter options for a recipe.
// Implementations control how and when recipe option callbacks are executed.
type ParameterOptionsEvaluator interface {
	Evaluate(ctx context.Context, sc *StageConfiguration, stageContext *recipe.StageContext) (recipe.ParameterOptions, error)
}
