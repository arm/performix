// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

let tool_cmn_name = 'cmn_analysis';

/**
 * @param {import("../docs/jsdocs").RenderExecutionContext} context
 */
function renderCMNAnalysis(context) {
  const entity = `tool/${tool_cmn_name}/0/`;
  const isComparison = context.getRunDescriptions().length === 2;
  const renderers = [];
  const visualizations = [];

  renderers.push({
    type: 'CmnCsvAverage',
    id: 'cmn_csv_average',
    config: {
      entity,
    },
  });

  if (isComparison) {
    renderers.push({
      type: 'CompareFlatTable',
      id: 'cmn_csv_compare',
      config: {
        data_source: {
          tables: {
            flat_tables: [
              {
                renderer_id: 'cmn_csv_average',
                output: 'cmn_average_table',
                content_index: 0,
              },
              {
                renderer_id: 'cmn_csv_average',
                output: 'cmn_average_table',
                content_index: 1,
              },
            ],
          },
        },
        join_columns: ['mesh_id', 'group', 'metric', 'units'],
        fixed_columns: [],
        compare_columns: ['*'],
        input_component_type: 'cmn_metrics',
        output_component_type: 'cmn_metrics_delta',
      },
    });
  }

  visualizations.push(
    isComparison
      ? {
          type: 'generic_grid',
          id: 'cmn_average_table_comparison',
          rendererId: 'cmn_csv_compare',
          title: 'CMN Per-Device Averages (Comparison)',
          description: '',
          config: {
            autoSizeColumns: false,
            data_source: {
              tables: {
                table: [
                  {
                    renderer_id: 'cmn_csv_compare',
                    output: 'delta_flat_table',
                  },
                ],
              },
            },
          },
        }
      : {
          type: 'generic_grid',
          id: 'cmn_average_table',
          rendererId: 'cmn_csv_average',
          title: 'CMN Per-Device Averages',
          description: '',
          config: {
            autoSizeColumns: false,
            data_source: {
              tables: {
                table: [
                  {
                    renderer_id: 'cmn_csv_average',
                    output: 'cmn_average_table',
                  },
                ],
              },
            },
          },
        },
  );

  const meshContentIndex = isComparison ? 1 : 0;
  const meshSvgComponents = context
    .listRunComponents(meshContentIndex, `${entity}cmn-mesh-svg/**`)
    .filter((component) => component.componentType.name === 'cmn-mesh-svg');

  for (const component of meshSvgComponents) {
    // Mesh images are expected to be named like "mesh0.svg"
    if (
      !component.fileName.startsWith('mesh') ||
      !component.fileName.endsWith('.svg')
    ) {
      throw new Error(`unexpected CMN mesh filename: ${component.fileName}`);
    }
    const meshIndex = component.fileName.slice('mesh'.length, -'.svg'.length);
    const titleIndex = meshSvgComponents.length > 1 ? ` ${meshIndex}` : '';
    const titleSuffix = isComparison ? ' (Current Run)' : '';

    visualizations.push({
      type: 'image',
      id: `cmn_mesh${meshIndex}`,
      rendererId: 'cmn_csv_average', // This visualization doesn't use a renderer, but a rendererId is required by the engine. Pass cmn_csv_average as a placeholder.
      title: `CMN Mesh Topology${titleIndex}${titleSuffix}`,
      description: '',
      config: {
        data_source: {
          file: {
            relative_path: component.relativePath,
          },
        },
      },
    });
  }

  return {
    renderers,
    visualizations,
  };
}

module.exports = {
  id: 'cmn_analysis',
  renderParameters: [],
  render: renderCMNAnalysis,
};
