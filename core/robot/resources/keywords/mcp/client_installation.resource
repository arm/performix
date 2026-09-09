# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   Keywords and fixtures for MCP client installation tests.
Library         Collections
Library         OperatingSystem
Library         Process
Library         ../../libs/MCPClient.py
Library         ../../libs/Terminology.py
Resource        ../common.resource


*** Variables ***
${MCP_CLIENT_TEST_MODE}  %{MCP_CLIENT_TEST_MODE=mock}
${CONFIG_CONFLICT}       engine.mcp_registration.CONFIG_CONFLICT
${CONFIG_READ_FAILED}    engine.mcp_registration.CONFIG_READ_FAILED
${CLIENT_UNAVAILABLE}    engine.mcp_registration.CLIENT_UNAVAILABLE


*** Keywords ***
MCP Client Installation Suite Setup
  Populate Terms
  ${bin} =  Determine ATPerf CLI Bin Path
  File Should Exist  ${bin}

MCP Client Registration Lifecycle Should Succeed
  [Arguments]  ${client}  ${display_name}
  ${bin} =  Determine ATPerf CLI Bin Path
  ${fixture} =  Create MCP Registration Fixture  ${client}  ${MCP_CLIENT_TEST_MODE}
  TRY
    Exercise MCP Registration Lifecycle  ${bin}  ${fixture}  ${client}  ${display_name}
  FINALLY
    Remove MCP Registration Fixture
  END

Exercise MCP Registration Lifecycle
  [Arguments]  ${bin}  ${fixture}  ${client}  ${display_name}
  ${connection_args} =  Create List
  ...  --server-port  ${fixture}[server_port]
  ...  --auth-port  ${fixture}[auth_port]
  TRY
    ${start} =  Run Process  ${bin}  @{connection_args}  daemon  start  --http-port  0
    ...  env=${fixture}[environment]
    MCP Process Should Succeed  ${start}
    MCP Registration Should Be Absent  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}
    Install MCP Registration  ${bin}  ${connection_args}  ${fixture}  ${client}
    MCP Registration Should Be Ready  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}
    Install MCP Registration  ${bin}  ${connection_args}  ${fixture}  ${client}
    Uninstall MCP Registration  ${bin}  ${connection_args}  ${fixture}  ${client}
    MCP Client Status Should Be  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}  not configured
  FINALLY
    Run Process  ${bin}  @{connection_args}  daemon  stop  --json
    ...  env=${fixture}[environment]
  END

MCP Registration Should Be Absent
  [Arguments]  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}
  MCP Client Status Should Be  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}  not configured
  MCP Doctor Should Report Readiness  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}  ${False}

MCP Registration Should Be Ready
  [Arguments]  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}
  MCP Client Status Should Be  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}  configured
  MCP Doctor Should Report Readiness  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}  ${True}

MCP Client Status Should Be
  [Arguments]  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}  ${expected_state}
  ${status} =  Run Process  ${bin}  @{connection_args}  mcp  status  ${client}
  ...  env=${fixture}[environment]
  MCP Process Should Succeed  ${status}
  MCP Plain Client Status Should Be  ${status.stdout}  ${client}  ${display_name}  ${expected_state}
  ${json_status} =  Run Process  ${bin}  @{connection_args}  --json  mcp  status  ${client}
  ...  env=${fixture}[environment]
  MCP Process Should Succeed  ${json_status}
  MCP JSON Client Status Should Be  ${json_status.stdout}  ${client}  ${expected_state}  ${True}

MCP Plain Client Status Should Be
  [Arguments]  ${output}  ${client}  ${display_name}  ${expected_state}
  Should Contain  ${output}  Client:
  Should Contain  ${output}  ${display_name}
  Should Contain  ${output}  Client ID:
  Should Contain  ${output}  ${client}
  Should Contain  ${output}  Detected:
  Should Contain  ${output}  true
  Should Contain  ${output}  ${PRODUCT_FULL_NAME} MCP status:
  Should Contain  ${output}  ${expected_state}

Install MCP Registration
  [Arguments]  ${bin}  ${connection_args}  ${fixture}  ${client}
  ${result} =  Run Process  ${bin}  @{connection_args}  mcp  install  ${client}
  ...  env=${fixture}[environment]
  MCP Process Should Succeed  ${result}
  Should Contain  ${result.stderr}  Adding ${PRODUCT_FULL_NAME} MCP to ${client}
  Should Contain  ${result.stderr}  done
  IF  $fixture["configuration_path"]
    MCP Configuration Should Contain Server
    ...  ${fixture}[configuration_path]
    ...  ${fixture}[configuration_root]
    ...  ${bin}
  END

MCP Doctor Should Report Readiness
  [Arguments]  ${bin}  ${connection_args}  ${fixture}  ${client}  ${display_name}  ${ready}
  ${doctor} =  Run Process  ${bin}  @{connection_args}  mcp  doctor  ${client}
  ...  env=${fixture}[environment]
  ${json_doctor} =  Run Process  ${bin}  @{connection_args}  --json  mcp  doctor  ${client}
  ...  env=${fixture}[environment]
  MCP Doctor Plain Output Should Match Readiness  ${doctor}  ${client}  ${display_name}  ${ready}
  MCP Doctor JSON Should Match Readiness  ${json_doctor}  ${client}  ${ready}

MCP Doctor Plain Output Should Match Readiness
  [Arguments]  ${doctor}  ${client}  ${display_name}  ${ready}
  IF  ${ready}
    MCP Process Should Succeed  ${doctor}
    Should Contain  ${doctor.stdout}  ✓ ${PRODUCT_FULL_NAME} MCP: configured.
    Should Contain  ${doctor.stdout}  ${display_name} is ready to use ${PRODUCT_FULL_NAME} MCP.
  ELSE
    MCP Process Should Fail  ${doctor}
    Should Contain  ${doctor.stdout}  ✗ ${PRODUCT_FULL_NAME} MCP: not configured.
    Should Contain  ${doctor.stdout}  apx mcp install ${client}
    Should Contain  ${doctor.stdout}  ${display_name} is not ready to use ${PRODUCT_FULL_NAME} MCP.
  END
  Should Contain  ${doctor.stdout}  ✓ MCP client: ${display_name} was detected.

MCP Doctor JSON Should Match Readiness
  [Arguments]  ${json_doctor}  ${client}  ${ready}
  IF  ${ready}
    MCP Process Should Succeed  ${json_doctor}
    VAR  ${expected_code}  0
  ELSE
    MCP Process Should Fail  ${json_doctor}
    VAR  ${expected_code}  -1
  END
  ${response} =  Evaluate  json.loads($json_doctor.stdout)  json
  Should Be Equal  ${response}[code]  ${expected_code}
  Should Be Equal  ${response}[data][client_id]  ${client}
  Should Be Equal  ${response}[data][ready]  ${ready}

Uninstall MCP Registration
  [Arguments]  ${bin}  ${connection_args}  ${fixture}  ${client}
  ${result} =  Run Process  ${bin}  @{connection_args}  mcp  uninstall  ${client}
  ...  env=${fixture}[environment]
  MCP Process Should Succeed  ${result}
  Should Contain  ${result.stderr}  Removing ${PRODUCT_FULL_NAME} MCP from ${client}
  Should Contain  ${result.stderr}  done
  ${status} =  Run Process  ${bin}  @{connection_args}  mcp  status  ${client}
  ...  env=${fixture}[environment]
  MCP Process Should Succeed  ${status}
  IF  $fixture["configuration_path"]
    MCP Configuration Should Not Contain Server
    ...  ${fixture}[configuration_path]
    ...  ${fixture}[configuration_root]
  END

Create All MCP Client Test Context
  Skip If  '${MCP_CLIENT_TEST_MODE}' != 'mock'  The --all fixture requires mock mode.
  ${bin} =  Determine ATPerf CLI Bin Path
  ${fixture} =  Create MCP Registration Fixture  all  mock
  ${context} =  Create Dictionary  bin=${bin}  fixture=${fixture}
  RETURN  ${context}

Exercise All MCP Client Operations
  [Arguments]  ${context}
  ${connection_args} =  Start MCP Registration Daemon  ${context}[bin]  ${context}[fixture]
  TRY
    ${before} =  Collect MCP Status Results  ${context}[bin]  ${connection_args}  ${context}[fixture]
    ${install} =  Run Process  ${context}[bin]  @{connection_args}  mcp  install  --all
    ...  env=${context}[fixture][environment]
    ${installed} =  Collect MCP Status Results  ${context}[bin]  ${connection_args}  ${context}[fixture]
    ${uninstall} =  Run Process  ${context}[bin]  @{connection_args}  mcp  uninstall  --all
    ...  env=${context}[fixture][environment]
    ${removed} =  Collect MCP Status Results  ${context}[bin]  ${connection_args}  ${context}[fixture]
  FINALLY
    Run Process  ${context}[bin]  @{connection_args}  daemon  stop  --json
    ...  env=${context}[fixture][environment]
  END
  ${results} =  Create Dictionary
  ...  before=${before}
  ...  install=${install}
  ...  installed=${installed}
  ...  uninstall=${uninstall}
  ...  removed=${removed}
  RETURN  ${results}

Start MCP Registration Daemon
  [Arguments]  ${bin}  ${fixture}
  ${connection_args} =  Create List
  ...  --server-port  ${fixture}[server_port]
  ...  --auth-port  ${fixture}[auth_port]
  ${start} =  Run Process  ${bin}  @{connection_args}  daemon  start  --http-port  0
  ...  env=${fixture}[environment]
  MCP Process Should Succeed  ${start}
  RETURN  ${connection_args}

Collect MCP Status Results
  [Arguments]  ${bin}  ${connection_args}  ${fixture}
  ${plain} =  Run Process  ${bin}  @{connection_args}  mcp  status
  ...  env=${fixture}[environment]
  ${json} =  Run Process  ${bin}  @{connection_args}  --json  mcp  status
  ...  env=${fixture}[environment]
  ${results} =  Create Dictionary  plain=${plain}  json=${json}
  RETURN  ${results}

All MCP Client Operations Should Succeed
  [Arguments]  ${results}
  MCP Status Summary Should Report State  ${results}[before]  not configured
  MCP Process Should Succeed  ${results}[install]
  MCP Operation Should List Every Client  ${results}[install]  Adding  to
  MCP Status Summary Should Report State  ${results}[installed]  configured
  MCP Process Should Succeed  ${results}[uninstall]
  MCP Operation Should List Every Client  ${results}[uninstall]  Removing  from
  MCP Status Summary Should Report State  ${results}[removed]  not configured

MCP Status Summary Should Report State
  [Arguments]  ${results}  ${expected_state}
  VAR  ${plain_result}  ${results}[plain]
  VAR  ${json_result}  ${results}[json]
  MCP Process Should Succeed  ${plain_result}
  MCP Process Should Succeed  ${json_result}
  MCP Plain Status Summary Should Report State  ${plain_result}  ${expected_state}
  MCP JSON Status Summary Should Report State  ${json_result.stdout}  ${expected_state}

MCP Plain Status Summary Should Report State
  [Arguments]  ${result}  ${expected_state}
  Should Contain  ${result.stderr}  Detected MCP clients:
  FOR  ${client}  IN  antigravity  claude-code  claude-desktop  codex  cursor  vscode
    Should Contain  ${result.stdout}  ${client}
    Should Contain  ${result.stdout}  ${expected_state}
  END

MCP JSON Status Summary Should Report State
  [Arguments]  ${output}  ${expected_state}
  ${response} =  Evaluate  json.loads($output)  json
  ${expected_json_state} =  Evaluate  $expected_state.replace(" ", "_")
  Should Be Equal  ${response}[code]  0
  VAR  ${clients}  ${response}[data][clients]
  ${clients_by_id} =  Evaluate  {item["id"]: item for item in $clients}
  Length Should Be  ${clients}  6
  FOR  ${client}  IN  antigravity  claude-code  claude-desktop  codex  cursor  vscode
    ${client_status} =  Get From Dictionary  ${clients_by_id}  ${client}
    Should Be Equal  ${client_status}[registration_state]  ${expected_json_state}
    Should Be Equal  ${client_status}[detected]  ${True}
  END

MCP Operation Should List Every Client
  [Arguments]  ${result}  ${verb}  ${preposition}
  FOR  ${client}  IN  antigravity  claude-code  claude-desktop  codex  cursor  vscode
    Should Contain  ${result.stderr}  ${verb} ${PRODUCT_FULL_NAME} MCP ${preposition} ${client} - done
  END

MCP Diagnostic Problem Should Be Reported
  [Arguments]  ${client}  ${display_name}  ${scenario}  ${state}  ${error_code}  ${status_detail}  ${doctor_detail}  ${detected}
  Skip If  '${MCP_CLIENT_TEST_MODE}' != 'mock'  Diagnostic problem fixtures require mock mode.
  ${bin} =  Determine ATPerf CLI Bin Path
  ${fixture} =  Create MCP Registration Fixture  ${client}  mock  ${scenario}
  TRY
    ${results} =  Exercise MCP Diagnostic Commands  ${bin}  ${fixture}  ${client}
    MCP Diagnostic Commands Should Report Problem
    ...  ${results}  ${client}  ${display_name}  ${state}  ${error_code}
    ...  ${status_detail}  ${doctor_detail}  ${detected}
  FINALLY
    Remove MCP Registration Fixture
  END

Exercise MCP Diagnostic Commands
  [Arguments]  ${bin}  ${fixture}  ${client}
  ${connection_args} =  Create List
  ...  --server-port  ${fixture}[server_port]
  ...  --auth-port  ${fixture}[auth_port]
  TRY
    ${start} =  Run Process  ${bin}  @{connection_args}  daemon  start  --http-port  0
    ...  env=${fixture}[environment]
    MCP Process Should Succeed  ${start}
    ${status} =  Run Process  ${bin}  @{connection_args}  mcp  status  ${client}
    ...  env=${fixture}[environment]
    ${status_json} =  Run Process  ${bin}  @{connection_args}  --json  mcp  status  ${client}
    ...  env=${fixture}[environment]
    ${doctor} =  Run Process  ${bin}  @{connection_args}  mcp  doctor  ${client}
    ...  env=${fixture}[environment]
    ${doctor_json} =  Run Process  ${bin}  @{connection_args}  --json  mcp  doctor  ${client}
    ...  env=${fixture}[environment]
  FINALLY
    Run Process  ${bin}  @{connection_args}  daemon  stop  --json
    ...  env=${fixture}[environment]
  END
  ${results} =  Create Dictionary
  ...  status=${status}
  ...  status_json=${status_json}
  ...  doctor=${doctor}
  ...  doctor_json=${doctor_json}
  RETURN  ${results}

MCP Diagnostic Commands Should Report Problem
  [Arguments]  ${results}  ${client}  ${display_name}  ${state}  ${error_code}  ${status_detail}  ${doctor_detail}  ${detected}
  MCP Diagnostic Processes Should Have Expected Results  ${results}
  MCP Problem Status Output Should Match
  ...  ${results}[status]  ${state}  ${error_code}  ${status_detail}  ${detected}
  MCP Problem Doctor Output Should Match
  ...  ${results}[doctor]  ${display_name}  ${doctor_detail}
  MCP Problem JSON Outputs Should Match
  ...  ${results}[status_json]  ${results}[doctor_json]  ${client}  ${state}  ${error_code}  ${detected}

MCP Diagnostic Processes Should Have Expected Results
  [Arguments]  ${results}
  MCP Process Should Succeed  ${results}[status]
  MCP Process Should Succeed  ${results}[status_json]
  MCP Process Should Fail  ${results}[doctor]
  MCP Process Should Fail  ${results}[doctor_json]

MCP Problem Status Output Should Match
  [Arguments]  ${result}  ${state}  ${error_code}  ${detail}  ${detected}
  Should Contain  ${result.stdout}  Detected:
  IF  ${detected}
    Should Contain  ${result.stdout}  true
  ELSE
    Should Contain  ${result.stdout}  false
  END
  Should Contain  ${result.stdout}  ${PRODUCT_FULL_NAME} MCP status:
  Should Contain  ${result.stdout}  ${state}
  Should Contain  ${result.stdout}  ${error_code}
  Should Contain  ${result.stdout}  ${detail}

MCP Problem Doctor Output Should Match
  [Arguments]  ${result}  ${display_name}  ${detail}
  Should Contain  ${result.stdout}  ${detail}
  Should Contain  ${result.stdout}  ${display_name} is not ready to use ${PRODUCT_FULL_NAME} MCP.

MCP Problem JSON Outputs Should Match
  [Arguments]  ${status_result}  ${doctor_result}  ${client}  ${state}  ${error_code}  ${detected}
  MCP JSON Client Status Should Be  ${status_result.stdout}  ${client}  ${state}  ${detected}
  Should Contain  ${status_result.stdout}  ${error_code}
  Should Contain  ${doctor_result.stdout}  ${error_code}
  ${doctor_response} =  Evaluate  json.loads($doctor_result.stdout)  json
  Should Be Equal  ${doctor_response}[code]  -1
  Should Be Equal  ${doctor_response}[data][ready]  ${False}

MCP JSON Client Status Should Be
  [Arguments]  ${output}  ${client}  ${expected_state}  ${detected}
  ${response} =  Evaluate  json.loads($output)  json
  ${expected_json_state} =  Evaluate  $expected_state.replace(" ", "_")
  Should Be Equal  ${response}[code]  0
  Length Should Be  ${response}[data][clients]  1
  VAR  ${client_status}  ${response}[data][clients][0]
  Should Be Equal  ${client_status}[id]  ${client}
  Should Be Equal  ${client_status}[detected]  ${detected}
  Should Be Equal  ${client_status}[registration_state]  ${expected_json_state}

MCP Process Should Succeed
  [Arguments]  ${result}
  Should Be Equal As Integers  ${result.rc}  0
  ...  msg=stdout: ${result.stdout}${\n}stderr: ${result.stderr}

MCP Process Should Fail
  [Arguments]  ${result}
  Should Not Be Equal As Integers  ${result.rc}  0
  ...  msg=stdout: ${result.stdout}${\n}stderr: ${result.stderr}

MCP Configuration Should Contain Server
  [Arguments]  ${path}  ${root}  ${expected_command}
  ${text} =  Get File  ${path}
  ${configuration} =  Evaluate  json.loads($text)  json
  ${servers} =  Get From Dictionary  ${configuration}  ${root}
  ${server} =  Get From Dictionary  ${servers}  arm-performix
  Should Be Equal  ${server}[command]  ${expected_command}
  ${expected_args} =  Create List  mcp  start
  Lists Should Be Equal  ${server}[args]  ${expected_args}

MCP Configuration Should Not Contain Server
  [Arguments]  ${path}  ${root}
  ${text} =  Get File  ${path}
  ${configuration} =  Evaluate  json.loads($text)  json
  ${servers} =  Get From Dictionary  ${configuration}  ${root}
  Dictionary Should Not Contain Key  ${servers}  arm-performix
