// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// Render Parameters Recipe Definition
// @ts-check

/**
 * @type {import("./docs/jsdocs").Recipe}
 */
var recipe = {
  name: 'render_params_recipe',
  title: 'Render Params Recipe',
  version: '1.0',
  api_version: '1.0.0',
  description: 'Recipe for validating render parameter propagation.',
  parameters: [],
  readyStages: [],
  renderParameters: [
    {
      id: 'threshold',
      config: {
        type: 'number',
      },
    },
  ],
  runStages: [
    {
      name: 'Run the recipe',
      description: '',
      exec: runRecipe,
    },
  ],
  renderStages: [
    {
      name: 'Create Render',
      description: 'Create renderer specs using render parameters.',
      exec: renderRecipe,
    },
  ],
  parameterValidation: validateParameters,
};

/**
 * @param {import("./docs/jsdocs").RenderExecutionContext} context
 * @returns {import("./docs/jsdocs").RecipeRenderOutput}
 */
function renderRecipe(context) {
  return {
    renderers: [
      {
        type: 'Log',
        id: 'log',
        config: { threshold: context.getRenderParameter('threshold') },
      },
    ],
    visualizations: [],
  };
}

/**
 * @param {import("./docs/jsdocs").RunExecutionContext} context
 */
function runRecipe(context) {}

/**
 * @param {import("./docs/jsdocs").ReadyExecutionContext} context
 * @returns {import("./docs/jsdocs").ValidationResult}
 */
function validateParameters(context) {
  return { errors: [] };
}
