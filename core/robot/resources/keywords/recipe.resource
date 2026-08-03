# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A resource file containing 'recipe' keywords for the Arm Total Performance CLI.

Resource            common.resource
Library             String


*** Variables ***
${DEPLOY_TOOLS_FLAG}                --deploy-tools
${DEFAULT_LINUX_RECIPE_TIMEOUT}     3
${DEFAULT_WOA_RECIPE_TIMEOUT}       6  # WOA targets need a longer timeout as wperf can fail for short runs due to no samples.
${RECIPE_READY_STATUS_UNKNOWN}      0
${RECIPE_READY_STATUS_READY}        1
${RECIPE_READY_STATUS_WARNING}      2
${RECIPE_READY_STATUS_ERROR}        3
${CUSTOM_TOOL_RECIPE}               custom_tool_recipe.js
${RUN_COMMAND_RECIPE}               run_command_recipe.js
${RUN_COMMAND_TOOL_INTEGRATION}     run_command_tool_integration.js
${G_LAST_RUN_COMMAND_STDOUT_PATH}   ${EMPTY}
${G_LAST_RUN_COMMAND_STDERR_PATH}   ${EMPTY}
${TEMP_FILE_PATH}       ${EMPTY}
${TEMP_FILE_NAME}       tempFile


*** Keywords ***
### List ###

List All Recipes
  Run ATPerf CLI Command  recipe list

The Recipe Is Listed
  [Arguments]  ${name}
  List All Recipes
  ${dict} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${dict}  data
  ${recipes} =  Get From Dictionary  ${data}  recipes
  ${val} =  Create Dictionary  name=${name}  path=${None}  load_error=${None}
  List Should Contain Value  ${recipes}  ${val}

The Default Recipes Are Listed
  The CPU Microarchitecture Recipe Is Listed
  The Instruction Mix Recipe Is Listed
  The Code Hotspots Recipe Is Listed
  The Memory Access Recipe Is Listed
  The System Utilization Recipe Is Listed

The CPU Microarchitecture Recipe Is Listed
  The Recipe Is Listed  cpu_microarchitecture

The Code Hotspots Recipe Is Listed
  The Recipe Is Listed  code_hotspots

The Instruction Mix Recipe Is Listed
  The Recipe Is Listed  instruction_mix

The Memory Access Recipe Is Listed
  The Recipe Is Listed  memory_access

The System Utilization Recipe Is Listed
  The Recipe Is Listed  system_utilization

The Syscall Trace Summary Recipe Is Listed
  The Recipe Is Listed  syscall_trace_summary

Copy The External Test Recipe Into The User Recipe Folder
  ${atperf_user_data_dir} =  Determine Data Directory Path
  ${src_test_recipe_path} =  Normalize Path  ${ROBOT_FILES_DIR}${/}recipes${/}${CUSTOM_TOOL_RECIPE}
  ${dst_test_recipe_path} =  Normalize Path
  ...  ${atperf_user_data_dir}${/}extensions${/}cpuE${/}recipes${/}${CUSTOM_TOOL_RECIPE}
  Copy File  ${src_test_recipe_path}  ${dst_test_recipe_path}
  File Should Exist  ${dst_test_recipe_path}

Remove The External Test Recipe From The User Recipe Folder
  ${atperf_user_data_dir} =  Determine Data Directory Path
  ${dst_test_recipe_path} =  Normalize Path
  ...  ${atperf_user_data_dir}${/}extensions${/}cpuE${/}recipes${/}${CUSTOM_TOOL_RECIPE}
  Remove File  ${dst_test_recipe_path}
  File Should Not Exist  ${dst_test_recipe_path}

The External Test Recipe Is Listed
  The Recipe Is Listed  custom_tool_recipe

### Info ###

List Recipe Info
  [Arguments]  ${name}
  Run ATPerf CLI Command  recipe info ${name}

List Recipe Info With Target
  [Arguments]  ${name}  ${target}
  Run ATPerf CLI Command  recipe info ${name} --target ${target}

List CPU Microarchitecture Recipe Info
  List Recipe Info  cpu_microarchitecture

List Code Hotspots Recipe Info
  List Recipe Info  code_hotspots

List Instruction Mix Recipe Info
  List Recipe Info  instruction_mix

List External Recipe Info
  [Arguments]  ${file}
  ${path} =  Get External Recipe Path  ${file}
  List Recipe Info  ${path}

The External Recipe Info Is Listed
  [Arguments]  ${file}
  List External Recipe Info  ${file}
  The Last Command Succeeded

The External Code Hotspots Recipe Info Is Listed
  The External Recipe Info Is Listed  code_hotspots_external.js

Get Recipe Info As Dictionary
  [Documentation]  Returns the JSON data from recipe info as a dictionary
  ${info} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${info}  data
  RETURN  ${data}

The Recipe Info Contains
  [Documentation]  Check that the recipe info details contains the specified key/value pair.
  [Arguments]  ${field}  ${expected_value}
  ${details} =  Get Recipe Info As Dictionary
  ${value} =  Get From Dictionary  ${details}  ${field}
  Should Be Equal As Strings  ${value}  ${expected_value}

The Recipe Info Parameter Options Contains
  [Documentation]  Check that the recipe info details contains a parameter with the expected options.
  [Arguments]  ${parameter_name}  ${parameter_type}  ${expected_options}
  ${details} =  Get Recipe Info As Dictionary
  ${parameter_to_check} =  Evaluate
  ...  next((param for param in $details["parameters"] if param["config"]["type"] == ${parameter_type} and param["id"] == ${parameter_name}), None)
  Should Not Be Empty  ${parameter_to_check}  ${parameter_name} parameter not found
  ${has_options} =  Run Keyword And Return Status
  ...  Dictionary Should Contain Key  ${parameter_to_check["config"]}  options
  IF  ${has_options}
    ${options} =  Get From Dictionary  ${parameter_to_check["config"]}  options
  ELSE
    ${option_items} =  Get From Dictionary  ${parameter_to_check["config"]}  optionItems
    ${options} =  Evaluate  [item["value"] for item in $option_items]
  END
  List Should Contain Sub List  ${options}  ${expected_options}

The Recipe Info Parameter Options Contain The Default Static Options
  ${expected_options} =  Create List  normal  high
  The Recipe Info Parameter Options Contains  'sampling_freq'  'single_select'  ${expected_options}

The Recipe Info Plain Text Parameter Options Contain The Default Static Options
  ${expected_options} =  Create List  low (Low)  normal (Normal)  high (High)
  VAR  ${stdout} =  ${G_LAST_RESULT.stdout}
  Should Contain  ${stdout}  sampling_freq - single_select
  Should Contain  ${stdout}  - Options:
  FOR  ${expected_option}  IN  @{expected_options}
    Should Contain  ${stdout}  - ${expected_option}
  END

The Recipe Info Parameter Options Contains Basic Metric Groups
  ${expected_options} =  Create List  operation_mix  branch_effectiveness
  The Recipe Info Parameter Options Contains  'metrics_group'  'multi_select'  ${expected_options}

### Ready ###

Check Recipe Is Ready
  [Arguments]  ${name}  ${args}
  Run ATPerf CLI Command  recipe ready ${name} ${args}

Check External Recipe Is Ready
  [Arguments]  ${file}  ${args}
  ${path} =  Get External Recipe Path  ${file}
  Run ATPerf CLI Command  recipe ready ${path} ${args}

The Recipe Is Ready
  The Last Command Succeeded
  The Recipe Ready Status Is Not  ${RECIPE_READY_STATUS_ERROR}

The Recipe Is Not Ready
  The Last Command Succeeded
  The Recipe Ready Status Is  ${RECIPE_READY_STATUS_ERROR}

The Recipe Ready Status Is
  [Documentation]  Checks that the supplied expected status matches the recipe ready status
  [Arguments]  ${expected}
  ${stdout} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${stdout}  data
  ${ready_status} =  Get From Dictionary  ${data}  ready_status
  Should Be Equal As Integers  ${ready_status}  ${expected}

The Recipe Ready Status Is Not
  [Documentation]  Checks that the supplied expected status does not match the recipe ready status
  [Arguments]  ${expected}
  ${stdout} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${stdout}  data
  ${ready_status} =  Get From Dictionary  ${data}  ready_status
  Should Not Be Equal As Integers  ${ready_status}  ${expected}

Check Advice Messages Contain
  [Arguments]  ${expected_string}
  ${advice_messages} =  Get Advice Messages
  VAR  ${found} =  ${False}  scope=LOCAL
  ${expected_string} =  Evaluate  ${expected_string}.strip('""')

  FOR  ${message}  IN  @{advice_messages}
    ${advice} =  Get From Dictionary  ${message}  advice_message
    ${advice_text} =  Get From Dictionary  ${advice}  message
    ${contains} =  Run Keyword And Return Status  Should Contain  ${advice_text}  ${expected_string}
    IF  ${contains}
      VAR  ${found} =  ${True}  scope=LOCAL
      BREAK
    END
  END
  Should Be True  ${found}  Expected message containing '${expected_string}' not found in advice messages.

Get Advice Messages
  ${stdout} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${stdout}  data
  ${advice_messages} =  Get From Dictionary  ${data}  advice_messages
  RETURN  ${advice_messages}

Check Advice Messages Contain When Target Is Aarch64
  [Arguments]  ${expected_string}
  IF  '${G_TARGET_ARCH}' == '${ARCH_AARCH64}'
    Check Advice Messages Contain  ${expected_string}
  END

### Run ###

Apply Default Recipe Timeout
  [Documentation]  Append the default recipe timeout unless the supplied args already specify one.
  [Arguments]  ${args}
  ${has_timeout} =  Evaluate  '--timeout' in '''${args}'''
  IF  ${has_timeout}  RETURN  ${args}
  ${default_timeout} =  Get Default Recipe Timeout
  ${normalized_args} =  Strip String  ${args}
  IF  '${normalized_args}' == ''  RETURN  --timeout ${default_timeout}
  ${args_with_timeout} =  Catenate  SEPARATOR=${SPACE}  ${normalized_args}  --timeout ${default_timeout}
  RETURN  ${args_with_timeout}

Get Default Recipe Timeout
  [Documentation]  Return the default recipe timeout for the configured target.
  [Arguments]  ${default_linux_timeout}=${DEFAULT_LINUX_RECIPE_TIMEOUT}
  IF  '${G_TARGET_OS}' == '${OS_WINDOWS}'  RETURN  ${DEFAULT_WOA_RECIPE_TIMEOUT}
  RETURN  ${default_linux_timeout}

Run CPU Microarchitecture Recipe
  [Arguments]  ${args}
  ${timed_args} =  Apply Default Recipe Timeout  ${args}
  Run ATPerf CLI Command  recipe run cpu_microarchitecture ${timed_args}

Run Instruction Mix Recipe
  [Arguments]  ${args}
  ${timed_args} =  Apply Default Recipe Timeout  ${args}
  Run ATPerf CLI Command  recipe run instruction_mix ${timed_args}

Run Code Hotspots Recipe
  [Arguments]  ${args}
  ${timed_args} =  Apply Default Recipe Timeout  ${args}
  Run ATPerf CLI Command  recipe run code_hotspots ${timed_args}

Run Memory Access Recipe
  [Arguments]  ${args}
  ${timed_args} =  Apply Default Recipe Timeout  ${args}
  Run ATPerf CLI Command  recipe run memory_access ${timed_args}

Run System Utilization Recipe
  [Arguments]  ${args}
  ${timed_args} =  Apply Default Recipe Timeout  ${args}
  Run ATPerf CLI Command  recipe run system_utilization ${timed_args}

Run Syscall Trace Summary Recipe
  [Arguments]  ${args}
  ${timed_args} =  Apply Default Recipe Timeout  ${args}
  Run ATPerf CLI Command  recipe run syscall_trace_summary ${timed_args}

Run Recipe
  [Arguments]  ${recipe}  ${args}
  ${timed_args} =  Apply Default Recipe Timeout  ${args}
  Run ATPerf CLI Command  recipe run ${recipe} ${timed_args}

Start Code Hotspots Recipe
  [Arguments]  ${args}
  ${timed_args} =  Apply Default Recipe Timeout  ${args}
  Start ATPerf CLI Command  recipe run code_hotspots ${timed_args}

Stop Run
  [Documentation]  Stop a run by writing "s" to the given process's stdin
  [Arguments]  ${process}
  Write To Process Stdin  ${process}  "s"
  Wait For Process And Log  ${process}
  The Last Command Succeeded

Cancel Run
  [Documentation]  Cancel a run by writing "q" to the given process's stdin
  [Arguments]  ${process}
  Write To Process Stdin  ${process}  "q"
  Wait For Process And Log  ${process}
  The Last Command Failed With Message Code  engine.common.USER_CANCELLATION_ERROR

Last Run Command Via Recipe Stdout Contains
  [Documentation]  Checks that the stdout of the last run command contains the specified substring.
  [Arguments]  ${substr}
  ${path} =  Get Variable Value  ${G_LAST_RUN_COMMAND_STDOUT_PATH}
  ${os} =  Get Variable Value  ${G_TARGET_OS}
  IF  '${os}' == 'Windows'
    Run Target Command  type ${path}
  ELSE
    Run Target Command  cat ${path}
  END
  The Last Command Succeeded
  Should Contain  ${G_LAST_RESULT.stdout}  ${substr}

Last Run Command Via Recipe Stderr Contains
  [Documentation]  Checks that the stderr of the last run command contains the specified substring.
  [Arguments]  ${substr}
  ${path} =  Get Variable Value  ${G_LAST_RUN_COMMAND_STDERR_PATH}
  ${os} =  Get Variable Value  ${G_TARGET_OS}
  IF  '${os}' == 'Windows'
    Run Target Command  type ${path}
  ELSE
    Run Target Command  cat ${path}
  END
  The Last Command Succeeded
  Should Contain  ${G_LAST_RESULT.stdout}  ${substr}

The Script Is Created On The Target
  [Documentation]  Helper keyword to create a script with the specified content on the target.
  [Arguments]  ${path}  ${contents}
  ${sanitised} =  Replace String  ${contents}  \$  \\$
  ${sanitised} =  Replace String  ${sanitised}  \"  \\"
  Run Target Command  "printf \"%s\n\" \"#!/bin/bash\" \"${sanitised}\" > ${path} && chmod 0700 ${path}"
  The Last Command Succeeded

The Neoprof Capture Log Contains
  [Documentation]  Helper keyword to check for the presence of a string in the neoprof capture log.
  [Arguments]  ${expected}
  ${run_id} =  Extract The Run ID
  VAR  ${log_path} =  ${G_RUNS_DIR}${/}${run_id}${/}tool${/}neoprof${/}0${/}capture_log.txt
  ${log_text} =  Get File  ${log_path}
  Should Contain  ${log_text}  ${expected}

Escape Dollar If Needed
  [Documentation]  Escapes any dollar signs in the provided string if this is a special character on the
  ...  host machine (i.e. if host is UNIX). No-op on Windows.
  [Arguments]  ${str}
  ${os} =  Determine Operating System
  IF  '${os}' == 'Linux' or '${os}' == 'Darwin'
    ${escaped} =  Replace String  ${str}  \$  \\\$
    RETURN  ${escaped}
  ELSE IF  '${os}' == 'Windows'
    RETURN  ${str}
  END

### Validate ###

Validate Recipe Parameters
  [Arguments]  ${recipe}  ${args}
  Run ATPerf CLI Command  recipe validate-parameters ${recipe} ${args}

The Recipe Parameter Validate Response Contains
  [Documentation]  Checks that the recipe parameter validate response contains a specific parameter ID and message code.
  [Arguments]  ${parameter_id}  ${expected_message_code}
  ${dict} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${dict}  data
  ${messages} =  Get From Dictionary  ${data}  messages
  ${expected_message_code} =  Evaluate  ${expected_message_code}.strip('""')
  FOR  ${item}  IN  @{messages}
    ${param} =  Get From Dictionary  ${item}  parameter_id
    ${msg} =  Get From Dictionary  ${item}  message
    ${msg_code} =  Get From Dictionary  ${msg}  message_code
    IF  '${param}' == '${parameter_id}' and '${msg_code}' == '${expected_message_code}'
      VAR  ${found} =  ${True}  scope=LOCAL
      BREAK
    END
  END
  Should Be True
  ...  ${found}
  ...  Parameter ${parameter_id} with message code ${expected_message_code} was not found in the response.

The Recipe Parameter Validate Response Has No Messages
  [Documentation]  Checks that the recipe parameter validate response contains no validation messages.
  ${dict} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${dict}  data
  Dictionary Should Not Contain Key  ${data}  messages
