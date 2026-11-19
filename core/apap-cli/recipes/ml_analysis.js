// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

const pytorch = require('./lib/ml-analysis/pytorch');
const vulkanml = require('./lib/ml-analysis/vulkanml');
const flavors = { pytorch, vulkanml };

function readyMLAnalysis(context) {
  return flavors[context.getParameter('flavor')].ready(context);
}

function runMLAnalysis(context) {
  return flavors[context.getParameter('flavor')].run(context);
}

function renderMLAnalysis(context) {
  const runs = context.getRunDescriptions();
  const implementations = runs.map(
    (run) => flavors[run.Parameters?.flavor ?? 'pytorch'],
  );
  if (
    implementations.some(
      (implementation) => implementation !== implementations[0],
    )
  ) {
    throw { code: 'recipes.ml_analysis.COMPARE_MISMATCHED_FLAVORS' };
  }
  return implementations[0].render(context, runs);
}

const recipe = {
  name: 'ml_analysis',
  title: 'ML Analysis',
  version: '1.0.0',
  api_version: '1.0.0',
  status: 'experimental',
  description:
    'Profile PyTorch workloads with call tracing or VulkanML workloads with NeoProf.',
  mcp_guidance:
    'Select `flavor=pytorch` for a non-shell Python module launch; the Python environment must provide torch and `python` selects its interpreter. Select `flavor=vulkanml` for an Android package/activity launch under NeoProf. Other workload types are not currently supported by the VulkanML flavor. VulkanML results show the available Graphics GPU timeline. Run Details and Logs use the shared views.',
  deployments: [
    {
      appliesTo: [
        { architecture: 'aarch64', os: 'Linux' },
        { architecture: 'x86_64', os: 'Linux' },
      ],
      dependencies: Object.values(flavors).flatMap((flavor) =>
        flavor.getDeploymentDependencies('Linux'),
      ),
    },
    {
      appliesTo: [{ architecture: 'aarch64', os: 'Android' }],
      dependencies: Object.values(flavors).flatMap((flavor) =>
        flavor.getDeploymentDependencies('Android'),
      ),
    },
  ],
  parameters: [
    {
      id: 'flavor',
      required: false,
      label: 'ML Analysis flavor',
      description: 'Select the ML Analysis flavor for the workload.',
      config: {
        type: 'radio',
        defaultValue: 'pytorch',
        options: [
          { value: 'pytorch', label: 'PyTorch' },
          { value: 'vulkanml', label: 'VulkanML' },
        ],
      },
    },
    ...pytorch.parameters,
    ...vulkanml.parameters,
  ],
  readyStages: [
    {
      name: 'Check ML Analysis readiness',
      description: 'Check the selected collector and workload.',
      exec: readyMLAnalysis,
    },
  ],
  runStages: [
    {
      name: 'Collect ML data',
      description:
        'Profile the workload with the selected ML flavor collector.',
      exec: runMLAnalysis,
    },
  ],
  renderStages: [
    {
      name: 'Render ML data',
      description:
        'Create the available visualizations for the recorded ML flavor.',
      exec: renderMLAnalysis,
    },
  ],
};
