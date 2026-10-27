// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

// @ts-check

const traceSchemaVersion = '0.1';

/**
 * Construct an SQL renderer that converts one pytorch-collect Parquet table
 * into trace span fragments. SQL expressions are evaluated against each row of
 * the Parquet input.
 *
 * @param {object} args
 * @param {string} args.id - Unique ID for the generated SQL renderer.
 * @param {string} args.path - Manifest path of the pytorch-collect Parquet
 * file. It is resolved through an SQL renderer `path` placeholder.
 * @param {object} args.timeOrigin - Query and dependencies used to establish
 * the trace's time origin. Its `sql` will be embedded as a subquery for finding the
 * time origin. Its `inputs` and `data_source` are copied to the generated SQL
 * renderer so that any renderer inputs or table references used by the query
 * can be resolved.
 * @param {string} args.timeOrigin.sql - SQL query that must return one row with
 * a `zero_time_ns` column.
 * @param {object[]} [args.timeOrigin.inputs] - SQL renderer input-port
 * declarations required by the time-origin query.
 * @param {object} [args.timeOrigin.data_source] - Tables connected to those
 * input ports.
 * @param {string} args.eventID - SQL expression producing the source event ID
 * as a `VARCHAR`. IDs must be unique within this input.
 * @param {string} args.eventName - SQL expression producing the displayed span
 * name; it is converted to `VARCHAR`.
 * @param {string} args.parent - SQL expression producing the corresponding
 * parent source event ID (or `NULL`) as a `VARCHAR`.
 * @returns {object} An SQL renderer configuration producing one
 * `trace_span_fragment` table.
 */
function makePytorchTraceSQLRenderer({
  id,
  path,
  timeOrigin,
  eventID,
  eventName,
  parent,
}) {
  if (!timeOrigin || typeof timeOrigin.sql !== 'string') {
    throw new Error('A time-origin SQL query is required');
  }

  return {
    type: 'SQL',
    id,
    config: {
      sql: `
        WITH trace_time_origin AS (
          ${timeOrigin.sql}
        )
        SELECT
          ${eventID} AS source_event_id,
          CAST(ts_begin_ns - origin.zero_time_ns AS BIGINT) AS start_ns,
          CAST(ts_end_ns - origin.zero_time_ns AS BIGINT) AS end_ns,
          0::UINTEGER AS row,
          CAST(${eventName} AS VARCHAR) AS name,
          ${parent} AS parent_source_event_id,
          CASE
            WHEN len(json_keys(CAST(kwargs_json AS JSON))) = 0 THEN
              json_object(
                'arguments', CAST(args_json AS JSON),
                'output', CAST(output_json AS JSON)
              )
            ELSE
              json_object(
                'arguments', CAST(args_json AS JSON),
                'keyword_arguments', CAST(kwargs_json AS JSON),
                'output', CAST(output_json AS JSON)
              )
          END AS metadata
        FROM read_parquet({{path:${path}}})
        CROSS JOIN trace_time_origin AS origin
        WHERE origin.zero_time_ns IS NOT NULL
      `,
      inputs: timeOrigin.inputs ?? [],
      data_source: timeOrigin.data_source ?? { tables: {} },
      output: {
        name: 'spans',
        cardinality: 'one',
        component_type: {
          name: 'trace_span_fragment',
          schema_version: traceSchemaVersion,
        },
      },
    },
  };
}

module.exports = {
  makePytorchTraceSQLRenderer,
};
