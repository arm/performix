-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

WITH ids(id) AS (VALUES __PLACEHOLDERS__),
tags AS (
  SELECT
    measurement_id,
    COALESCE(FILTER(LIST(tag ORDER BY tag), x -> x <> ''), []) AS tags
  FROM ref_measurement_tags
  GROUP BY measurement_id
),
aliases AS (
  SELECT
    measurement_id,
    COALESCE(LIST({
           'source': source,
           'alias':  alias,
        }), []) as aliases
  FROM ref_measurement_aliases
  GROUP BY measurement_id
),
colrefs AS (
  SELECT
    measurement_id,
    COALESCE(LIST({
           'table':      table_name,
           'column':     column_name,
           'rendererID': NULLIF(TRIM(renderer), '')
        } ORDER BY table_name, column_name, renderer), []) AS colrefs
  FROM ref_measurement_column_refs
  GROUP BY measurement_id
),
group_ids AS (
  SELECT
    measurement_id,
    COALESCE(LIST(group_id ORDER BY group_id), []) AS group_ids
  FROM ref_measurement_group_links
  GROUP BY measurement_id
)
SELECT
  m.measurement_id,
  m.identifier,
  m.name,
  m.description,
  m.short_description,
  m.units,
  CAST(to_json(t.tags) AS VARCHAR)         as tags_json,
  CAST(to_json(map_from_entries(a.aliases)) AS VARCHAR) as aliases_json,
  CAST(to_json(cr.colrefs) AS VARCHAR)     as colrefs_json,
  CAST(to_json(gi.group_ids) AS VARCHAR)   as groups_ids_json
FROM ids
JOIN ref_measurements m ON m.measurement_id = ids.id
LEFT JOIN tags t         ON t.measurement_id = m.measurement_id
LEFT JOIN aliases a      ON a.measurement_id = m.measurement_id
LEFT JOIN colrefs cr     ON cr.measurement_id = m.measurement_id
LEFT JOIN group_ids gi   ON gi.measurement_id = m.measurement_id