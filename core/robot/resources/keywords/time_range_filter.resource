# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A resource file containing time range filter keywords.

Resource            render.resource


*** Keywords ***
Query Periodic Sample Count
  [Arguments]  ${session_id}  ${table}=drilldown  ${measurements_table}=drilldown_measurements
  ${query} =  Catenate
  ...  "select coalesce(sum(d.measurement_value), 0) as sample_count
  ...  from ${table} d
  ...  join ${measurements_table} m on d.measurement_id = m.measurement_id
  ...  where m.name in ('Periodic Samples (self)', 'Sample Count (self)')"
  ${drilldown_count} =  Query Single Value From Render Session
  ...  ${session_id}
  ...  ${query}
  RETURN  ${drilldown_count}

Query Time Limits
  [Arguments]  ${session_id}
  ${start_time} =  Query Single Value From Render Session
  ...  ${session_id}
  ...  "select min(start_time_ns) as start_time_ns from time_limits"
  ${end_time} =  Query Single Value From Render Session
  ...  ${session_id}
  ...  "select max(end_time_ns) as end_time_ns from time_limits"
  RETURN  ${start_time}  ${end_time}

Calculate Middle Tenth Time Filter
  [Arguments]  ${start_time}  ${end_time}
  ${duration} =  Evaluate  ${end_time} - ${start_time}
  ${partial_start_time} =  Evaluate  ${start_time} + ${duration} // 2
  ${partial_end_time} =  Evaluate  ${partial_start_time} + ${duration} // 10
  RETURN  ${partial_start_time}  ${partial_end_time}

Time Limits Should Match
  [Arguments]  ${original_start_time}  ${original_end_time}  ${filtered_start_time}  ${filtered_end_time}
  Should Be Equal As Numbers  ${original_start_time}  ${filtered_start_time}
  Should Be Equal As Numbers  ${original_end_time}  ${filtered_end_time}

The Unfiltered Run Contains Periodic Samples
  [Arguments]  ${run_id}  ${table}=drilldown  ${measurements_table}=drilldown_measurements
  Run Render  ${run_id}
  ${initial_sample_count} =  Query Periodic Sample Count
  ...  ${G_RENDER_SESSION_ID}
  ...  ${table}
  ...  ${measurements_table}
  VAR  ${UNFILTERED_COUNT} =  ${initial_sample_count}  scope=TEST
  Should Be True  ${UNFILTERED_COUNT} > 0

The Run Is Filtered With Full Time Range
  [Arguments]  ${run_id}  ${table}=drilldown  ${measurements_table}=drilldown_measurements
  ${end_time_ns} =  Query Single Value From Render Session  ${G_RENDER_SESSION_ID}  "select max(end_time_ns) as end_time_ns from time_limits"
  Run Render With Flags
  ...  ${run_id}
  ...  flags=--param=filter_start_time_ns=0 --param=filter_end_time_ns=${end_time_ns}
  ${new_sample_count} =  Query Periodic Sample Count
  ...  ${G_RENDER_SESSION_ID}
  ...  ${table}
  ...  ${measurements_table}
  VAR  ${FILTERED_COUNT} =  ${new_sample_count}  scope=TEST

The Run Is Filtered Over A Partial Time Range
  [Arguments]  ${run_id}  ${table}=drilldown  ${measurements_table}=drilldown_measurements
  ${start_time_ns}  ${end_time_ns} =  Query Time Limits  ${G_RENDER_SESSION_ID}
  ${partial_start_time_ns}  ${partial_end_time_ns} =  Calculate Middle Tenth Time Filter
  ...  ${start_time_ns}
  ...  ${end_time_ns}
  Run Render With Flags
  ...  ${run_id}
  ...  flags=--param=filter_start_time_ns=${partial_start_time_ns} --param=filter_end_time_ns=${partial_end_time_ns}
  ${new_sample_count} =  Query Periodic Sample Count
  ...  ${G_RENDER_SESSION_ID}
  ...  ${table}
  ...  ${measurements_table}
  VAR  ${FILTERED_COUNT} =  ${new_sample_count}  scope=TEST
  VAR  ${ORIGINAL_START_TIME} =  ${start_time_ns}  scope=TEST
  VAR  ${ORIGINAL_END_TIME} =  ${end_time_ns}  scope=TEST

The Run Data Is Unchanged
  Should Be True  ${FILTERED_COUNT} > 0
  Should Be Equal As Numbers  ${FILTERED_COUNT}  ${UNFILTERED_COUNT}

The Run Data Contains Fewer Samples
  Should Be True  ${FILTERED_COUNT} < ${UNFILTERED_COUNT}

The Run Data Keeps Original Time Limits
  ${filtered_start_time}  ${filtered_end_time} =  Query Time Limits  ${G_RENDER_SESSION_ID}
  Time Limits Should Match
  ...  ${ORIGINAL_START_TIME}
  ...  ${ORIGINAL_END_TIME}
  ...  ${filtered_start_time}
  ...  ${filtered_end_time}
