# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   Reanalyse a saved mixed-core capture and compare named total/self metrics with reviewed golden data.

Resource        ../../resources/keywords/render.resource

Suite Setup     Import Mixed-Core Capture
Suite Teardown  Common Teardown
Test Teardown   Close Capture Render Session

Test Tags       render  core-filter


*** Variables ***
${FIXTURE_DIR}   ${CURDIR}${/}..${/}..${/}resources${/}files${/}mixed-core
${CAPTURE_ID}    NONE
${ACTIVE_SESSION}  ${NONE}
${CALLPATH_TABLE}  NONE


*** Test Cases ***
The A55 Core Filter Produces Expected Measurements
  [Documentation]  Compare A55 total/self measurements with golden analyzer values.
  Given The Run Exists  ${CAPTURE_ID}
  When Render Capture For Core  Cortex-A55
  Then Filtered Measurements Match Golden Data  Cortex-A55

The A76 Core Filter Produces Expected Measurements
  [Documentation]  Compare A76 total/self measurements with golden analyzer values.
  Given The Run Exists  ${CAPTURE_ID}
  When Render Capture For Core  Cortex-A76
  Then Filtered Measurements Match Golden Data  Cortex-A76

Switching Back To A55 Restores The Original Measurements
  [Documentation]  Check that an intervening A76 render does not change the A55 result.
  Given The Run Exists  ${CAPTURE_ID}
  And The A55 Measurements Are Recorded
  And Render Capture For Core  Cortex-A76
  When Render Capture For Core  Cortex-A55
  Then The Measurements Match The Recorded Values

The A55 Core Filter Produces No Duplicate Measurements
  [Documentation]  Check that each frame has at most one value for each registered measurement.
  Given The Run Exists  ${CAPTURE_ID}
  When Render Capture For Core  Cortex-A55
  Then The Filtered Measurements Have No Duplicates

The A76 Core Filter Produces No Duplicate Measurements
  [Documentation]  Check that each frame has at most one value for each registered measurement.
  Given The Run Exists  ${CAPTURE_ID}
  When Render Capture For Core  Cortex-A76
  Then The Filtered Measurements Have No Duplicates


*** Keywords ***
Import Mixed-Core Capture
  Prepare Common Test Environment
  Determine Runs Directory Path
  Run ATPerf CLI Command  run import "${FIXTURE_DIR}${/}capture.zip"
  The Last Command Succeeded
  ${response} =  Parse Last JSON Stdout Line To Dictionary
  VAR  ${CAPTURE_ID} =  ${response}[data][new_id][value]  scope=SUITE

Render Capture For Core
  [Arguments]  ${core}
  Close Capture Render Session
  Run Render With Flags  ${CAPTURE_ID}  flags=--recipe instruction_mix --param filter_core_type=${core}
  VAR  ${ACTIVE_SESSION} =  ${G_RENDER_SESSION_ID}  scope=TEST
  The Render Invocation Was Successful
  ${table} =  Get Callpath Table From Render Response
  VAR  ${CALLPATH_TABLE} =  ${table}  scope=TEST

Close Capture Render Session
  IF  $ACTIVE_SESSION is None  RETURN
  Run ATPerf CLI Command  render close ${ACTIVE_SESSION}
  The Last Command Succeeded
  VAR  ${ACTIVE_SESSION} =  ${NONE}  scope=TEST

Get Callpath Table From Render Response
  ${render} =  Parse Last JSON Stdout Line To Dictionary
  VAR  ${entries} =  ${render}[data][invocation][manifest][entry]
  ${tables} =  Evaluate
  ...  [e['table_name'] for e in $entries if e.get('renderer_id', {}).get('value') == 'drilldown' and e['component_type'] == 'drilldown']
  Length Should Be  ${tables}  1
  RETURN  ${tables}[0]

Query Golden Measurements
  [Arguments]  ${table}
  ${query} =  Catenate
  ...  "select d.call_tree_id as frame, m.name as metric, d.measurement_value as value
  ...  from ${table} d join ref_measurements m using (measurement_id)
  ...  where m.name in ('Sample Count (total)', 'Sample Count (self)',
  ...  'Integer Operations Percentage (total)', 'Integer Operations Percentage (self)',
  ...  'Branch Operations Percentage (total)', 'Branch Operations Percentage (self)',
  ...  'Load Operations Percentage (total)', 'Load Operations Percentage (self)',
  ...  'Store Operations Percentage (total)', 'Store Operations Percentage (self)')
  ...  order by frame, metric"
  Query The Render Session  ${G_RENDER_SESSION_ID}  ${query}
  The Last Command Succeeded
  ${response} =  Parse Last JSON Stdout Line To Dictionary
  RETURN  ${response}[data][rows]

Filtered Measurements Match Golden Data
  [Arguments]  ${core}
  ${rows} =  Query Golden Measurements  ${CALLPATH_TABLE}
  ${expected} =  Read JSON File Into Dictionary  ${FIXTURE_DIR}${/}${core}.json
  Lists Should Be Equal  ${rows}  ${expected}[rows]  msg=${core} metrics differ from golden data

The A55 Measurements Are Recorded
  Render Capture For Core  Cortex-A55
  ${rows} =  Query Golden Measurements  ${CALLPATH_TABLE}
  VAR  ${RECORDED_MEASUREMENTS} =  ${rows}  scope=TEST

The Measurements Match The Recorded Values
  ${rows} =  Query Golden Measurements  ${CALLPATH_TABLE}
  Lists Should Be Equal  ${rows}  ${RECORDED_MEASUREMENTS}

The Filtered Measurements Have No Duplicates
  ${duplicates} =  Query Single Value From Render Session
  ...  ${G_RENDER_SESSION_ID}
  ...  "select count(*) from (select call_tree_id, measurement_id from ${CALLPATH_TABLE} group by all having count(*) > 1)"
  Should Be Equal As Integers  ${duplicates}  0
