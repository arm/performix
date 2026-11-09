-- SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
-- SPDX-License-Identifier: Apache-2.0

CREATE OR REPLACE MACRO trace_viewport(viewport_dataset, viewport_start, viewport_end, viewport_resolution, viewport_tracks) AS TABLE (
  SELECT spans.event_id, spans.track_id, spans.start_ns, spans.end_ns,
         spans.row, spans.name, spans.parent_event_id, spans.color_key,
         spans.color, spans.flags, selected.represented_count
  FROM (
    {{range $index, $source := .Sources}}
    {{if $index}}UNION ALL{{end}}
    SELECT s.event_id, s.track_id, s.start_ns, s.end_ns, s.row, s.name,
           s.parent_event_id, s.color_key, s.color, s.flags
    FROM {{$source.Table}} AS s
    WHERE viewport_dataset = {{sqlString $source.Dataset}}
      AND list_contains(viewport_tracks::BIGINT[], s.track_id)
      AND s.start_ns < viewport_end AND s.end_ns > viewport_start
    {{end}}
  ) AS spans
  JOIN trace_viewport_native(
    {{sqlString .SessionID}},
    to_json(struct_pack(
      dataset := viewport_dataset::VARCHAR,
      start := viewport_start::BIGINT,
      finish := viewport_end::BIGINT,
      resolution := viewport_resolution::BIGINT,
      tracks := viewport_tracks::BIGINT[]
    ))::VARCHAR
  ) AS selected USING (event_id, track_id)
  ORDER BY spans.track_id, spans.row, spans.start_ns, spans.end_ns, spans.event_id
);
