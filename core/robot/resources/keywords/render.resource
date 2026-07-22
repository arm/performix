# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A resource file containing 'render' keywords for the Arm Total Performance CLI.

Resource            common.resource
Resource            run.resource
Resource            recipe.resource


*** Variables ***
${G_RENDER_SESSION_ID}      NONE
${RENDER_SESSION_TABLES}    NONE
${QUERY_RESULTS}            NONE


*** Keywords ***
Run Invoke Render
  [Documentation]  Invoke-Render one or multiple runs using a given renderer, optionally specifying a renderer ID.
  [Arguments]  @{run_ids}  ${renderer_configs}
  ${concatenated_runs} =  Catenate  @{run_ids}
  Run ATPerf CLI Command  run invoke-render ${concatenated_runs} ${renderer_configs}
  ${session} =  Extract The Render Session
  VAR  ${G_RENDER_SESSION_ID} =  ${session}  scope=GLOBAL

The Runs Are Invoke-Rendered Successfully
  [Documentation]  Invoke-Render one or multiple runs using a given renderer and check for success
  [Arguments]  @{run_ids}  ${renderer_configs}
  Run Invoke Render  @{run_ids}  renderer_configs=${renderer_configs}
  The Render Invocation Was Successful

Extract The Render Session
  [Documentation]  Extract the render session (i.e s0, s1) from stdout, following
  ...    a run render invocation
  ${render_info} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${render_info}  data
  # 'run render' has session_id under data.invocation.session_id, whereas
  # 'run invoke-render' has it under data.session_id
  ${has_invocation} =  Run Keyword And Return Status  Dictionary Should Contain Key  ${data}  invocation
  IF  ${has_invocation}
    ${invocation} =  Get From Dictionary  ${data}  invocation
    ${session} =  Get From Dictionary  ${invocation}  session_id
  ELSE
    ${session} =  Get From Dictionary  ${data}  session_id
  END
  RETURN  ${session}

The Render Session ID Is Valid
  [Documentation]  Check if a given session ID is valid by invoking "show tables"
  [Arguments]  ${session_id}
  VAR  ${query} =  "show tables"  scope=LOCAL
  Run ATPerf CLI Command  render query ${session_id} ${query}
  The Last Command Succeeded

The Render Session Manifest Includes Renderer ID
  [Documentation]  Verify that the render session manifest contains the given renderer ID
  [Arguments]  ${renderer_id}
  ${render_info} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${render_info}  data
  ${manifest} =  Get From Dictionary  ${data}  manifest
  ${entries} =  Get From Dictionary  ${manifest}  entry

  VAR  ${found} =  ${False}  scope=LOCAL
  FOR  ${e}  IN  @{entries}
    ${status}  ${rid_obj} =  Run Keyword And Ignore Error  Get From Dictionary  ${e}  renderer_id
    IF  '${status}' == 'PASS'
      ${rid} =  Get From Dictionary  ${rid_obj}  value
      IF  '${rid}' == '${renderer_id}'
        VAR  ${found} =  ${True}
        BREAK
      END
    END
  END

  Should Be True  ${found}
  ...  Renderer ID '${renderer_id}' not found in manifest entries.

Count Rows For Render Session Query
  [Documentation]  Given a session ID and a query, count the number of rows the query returns
  [Arguments]  ${session_id}  ${query}
  Query The Render Session  ${session_id}  ${query}
  ${query_info} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${query_info}  data
  ${count} =  Get From Dictionary  ${data}  rowCount
  RETURN  ${count}

Query Single Value From Render Session
  [Documentation]  Query a render session and return a single scalar value.
  [Arguments]  ${session_id}  ${query}
  Query The Render Session  ${session_id}  ${query}
  The Last Command Succeeded
  ${row} =  Get Single Render Query Result Row
  ${column_count} =  Get Length  ${row}
  Should Be Equal As Integers  ${column_count}  1
  ${keys} =  Get Dictionary Keys  ${row}
  ${column_name} =  Get From List  ${keys}  0
  ${value} =  Get From Dictionary  ${row}  ${column_name}
  RETURN  ${value}

Get Single Render Query Result Row
  [Documentation]  Return the only row from the last render query result.
  ${query_info} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${query_info}  data
  ${row_count} =  Get From Dictionary  ${data}  rowCount
  Should Be Equal As Integers  ${row_count}  1
  ${rows} =  Get From Dictionary  ${data}  rows
  ${row} =  Get From List  ${rows}  0
  RETURN  ${row}

Query The Render Session
  [Documentation]  Given a session ID and a query, execute the query and return the result
  [Arguments]  ${session_id}  ${query}
  Run ATPerf CLI Command  render query ${session_id} ${query}

Query The Render Session Table
  [Documentation]  Given a table name and a session ID, query the table and return the query result
  [Arguments]  ${session_id}  ${table}
  Query The Render Session  ${session_id}  "select * from ${table} LIMIT 10"
  RETURN  ${G_LAST_RESULT}

The Render Session Table Exists
  [Documentation]  Given a table name and a session ID, assert that this table exists
  [Arguments]  ${session_id}  ${table}
  Query The Render Session  ${session_id}  "select * from ${table} LIMIT 1"
  The Last Command Succeeded

The Render Session Table Does Not Exist
  [Documentation]  Given a table name and a session ID, assert that this table does not exist
  [Arguments]  ${session_id}  ${table}
  Query The Render Session  ${session_id}  "select * from ${table} LIMIT 1"
  The Last Command Failed
  ${result} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${error} =  Get From Dictionary  ${result}  error
  ${msg} =  Get From Dictionary  ${error}  message
  Should Contain  ${msg}  Table with name ${table} does not exist

The Render Session Table Is Empty
  [Documentation]  Given a table name and a session ID, assert that this table exists and is empty
  [Arguments]  ${session_id}  ${table}
  The Render Session Table Exists  ${session_id}  ${table}
  ${result} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${result}  data
  ${count} =  Get From Dictionary  ${data}  rowCount
  Should Be Equal As Integers  ${count}  0

The Render Session Table Is Not Empty
  [Documentation]  Given a table name and a session ID, assert that this table exists and is not empty
  [Arguments]  ${session_id}  ${table}
  The Render Session Table Exists  ${session_id}  ${table}
  ${result} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${result}  data
  ${count} =  Get From Dictionary  ${data}  rowCount
  Should Be Equal As Integers  ${count}  1

Query The Render Session Tables
  [Documentation]  Iterates over a list of given table names and query each of them, returning
  ...  the results in a list.
  [Arguments]  ${session_id}  ${tables}
  ${query_results} =  Create List
  FOR  ${table}  IN  @{tables}
    ${result} =  Query The Render Session Table  session_id=${session_id}  table=${table}
    Append To List  ${query_results}  ${result}
  END
  RETURN  ${query_results}

All Render Session Tables Are Queried
  [Documentation]  For a given renderer, lookup the list of tables associated to it
  ...  in the RENDER_SESSION_TABLES suite variable, then query each of them. Results
  ...  are stored in a QUERY_RESULTS test variable
  [Arguments]  ${renderer}
  ${tables_data} =  Get From Dictionary  ${RENDER_SESSION_TABLES}  ${renderer}
  ${tables} =  Evaluate  list(${tables_data})
  ${results} =  Query The Render Session Tables  session_id=${G_RENDER_SESSION_ID}  tables=${tables}
  VAR  ${QUERY_RESULTS} =  ${results}  scope=TEST

All Render Session Query Results Were Successful
  [Documentation]  Go through the QUERY_RESULTS and check if they were successful
  FOR  ${query_result}  IN  @{QUERY_RESULTS}
    Should Be Equal As Integers  ${query_result.rc}  0
  END

Populate Render Session Tables
  [Documentation]  Creates a dictionary of available session tables, keyed by the renderer
  ${tables} =  Create Dictionary
  ...  StreamlineAnalyzeFlatFunctions=["drilldown", "drilldown_measurements"],
  ...  StreamlineAnalyzeFlatFunctions2=["drilldown", "drilldown_measurements"],
  ...  StreamlineAnalyzeFunctionProfileRenderer=["drilldown", "drilldown_measurements"],
  ...  StreamlineAnalyzeFunctionProfileRenderer2=["drilldown", "drilldown_measurements"],
  ...  StreamlineAnalyzeFunctionProfileRenderer2=["drilldown", "drilldown_measurements"],
  ...  TargetInfoRenderer=["target_info_os", "target_info_cluster", "target_info_cpus"],
  ...  CompareDrilldownFlat=["drilldown", "drilldown_1", "drilldown_measurements", "drilldown_delta"],
  VAR  ${RENDER_SESSION_TABLES} =  ${tables}  scope=SUITE

Run Render
  [Documentation]  Creates a render session for one or multiple runs
  [Arguments]  @{run_ids}
  ${concatenated_runs} =  Catenate  @{run_ids}
  Run ATPerf CLI Command  run render ${concatenated_runs}
  ${session} =  Extract The Render Session
  VAR  ${G_RENDER_SESSION_ID} =  ${session}  scope=GLOBAL

Run Render With Flags
  [Documentation]  Creates a render session for one or multiple runs, using additional flags for the render invocation
  [Arguments]  @{run_ids}  ${flags}
  ${concatenated_runs} =  Catenate  @{run_ids}
  Run ATPerf CLI Command  run render ${concatenated_runs} ${flags}
  ${session} =  Extract The Render Session
  VAR  ${G_RENDER_SESSION_ID} =  ${session}  scope=GLOBAL

Runs Are Rendered Successfully
  [Documentation]  Render one or multiple runs and check for success
  [Arguments]  @{run_ids}
  Run Render  @{run_ids}
  The Render Invocation Was Successful

The Render Invocation Was Successful
  [Documentation]  Check if the last render invocation was successful by looking at the "code"
  The Last Command Succeeded
  ${render_result} =  Parse Last JSON Stdout Line To Dictionary
  ${code} =  Get From Dictionary  ${render_result}  code
  Should Be Equal As Strings  ${code}  0

Render User Messages
  [Documentation]  Invoke-render a run with the log renderer restricted to user messages.
  [Arguments]  ${run_id}
  The Runs Are Invoke-Rendered Successfully  ${run_id}
  ...  renderer_configs=--renderer=Log="{\\"entity_filter\\":\\"user_messages\\"}"
