# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A resource file containing common keywords for the Arm Total Performance CLI.

Library             Collections
Library             OperatingSystem
Library             Process
Library             String
Library             ../libs/CommandRunner.py
Library             ../libs/TargetPlatform.py
Library             ../libs/Terminology.py
Resource            environment.resource
Resource            run.resource


*** Variables ***
${G_TARGET_NAME}                ${EMPTY}  # Global to track test target name
${G_TARGET_HOST}                ${EMPTY}  # Global to track test target host
${G_TARGET_PORT}                ${EMPTY}  # Global to track test target port
${G_TARGET_USER}                ${EMPTY}  # Global to track test target user
${G_TARGET_KEY}                 ${EMPTY}  # Global to track test target SSH key path
${G_TARGET_ARCH}                ${EMPTY}  # Global to track test target architecture
${G_TARGET_OS}                  ${EMPTY}  # Global to track test target operating system
${G_RUNS_DIR}                   ${EMPTY}  # Global to track the runs directory path
${G_LAST_RESULT}                ${EMPTY}  # Global to track last command result (rc, stdout, stderr)
${G_LAST_PROCESS}               ${EMPTY}  # Global to track process handle of last launched process
${G_WORKLOADS}                  ${EMPTY}  # Global to track available workloads
${LAUNCH_WORKLOAD}              ${EMPTY}  # Global default workload command
${JSON_FLAG}                    --json
${CLI_BIN_DIR}                  ${CURDIR}${/}..${/}..${/}..${/}apap-cli
${ROBOT_FILES_DIR}              ${CURDIR}${/}..${/}files
${ROBOT_EXPORT_DIR}             ${ROBOT_FILES_DIR}${/}export  # If you change this, update robot-framework.yaml and robot-framework-x86.yaml too
${TARGET_CONFIG_DIR}            ${ROBOT_FILES_DIR}${/}targets
${WORKLOAD_CONFIG_DIR}          ${ROBOT_FILES_DIR}${/}workloads
${EXTERNAL_RECIPES_DIR}         ${ROBOT_FILES_DIR}${/}recipes
${ARCH_AARCH64}                 aarch64
${ARCH_ARM64}                   arm64  # Used by MacOS
${ARCH_X86_64}                  x86-64
${OS_LINUX}                     Linux
${OS_DARWIN}                    Darwin
${OS_WINDOWS}                   Windows
${PRODUCT_FULL_NAME}            ${EMPTY} # Populated in suite setup from `terminology.json`
${PRODUCT_BINARY_NAME}          ${EMPTY} # Populated in suite setup from `terminology.json`
${AGENT_BINARY_NAME}            ${EMPTY} # Populated in suite setup from `terminology.json`
${DAEMON_DIR_NAME}              ${EMPTY} # Populated in suite setup from `terminology.json`
${ENV_VAR_PREFIX}               ${EMPTY} # Populated in suite setup from `terminology.json`
${G_TEST_USERNAME}              ${EMPTY} # Username used by deployment verification keywords


*** Keywords ***
Configure Terminology
  [Documentation]  Configures terminology Robot variables
  Populate Terms

Determine Target For Test
  [Documentation]  This keyword consumes the 'TARGET' variable, if passed on the CLI, and uses the
  ...  value to lookup a target config JSON file with this name in the resources/targets directory.
  ...  If the config file is found, this keyword will load the target details into a dictionary and
  ...  stash it in a gobal variable that can be accessed in other tests (e.g. G_TARGET_NAME).
  ${target_var} =  Get Variable Value  ${TARGET}
  IF  "${target_var}" != "None"
    ${normalized_dir} =  Normalize Path  ${TARGET_CONFIG_DIR}
    VAR  ${target_json}  ${normalized_dir}${/}${target_var}.json  scope=LOCAL
    File Should Exist  ${target_json}
    ${target_dict} =  Read JSON File Into Dictionary  ${target_json}
    ${normalized_dict} =  Normalize Key Path In Target Dictionary  ${target_dict}
    Set Test Target As Global Variable  ${normalized_dict}
  ELSE
    Fatal Error  No target file specified on command-line. Use [-v TARGET:<name_of_target_json_file>].
  END

Set Test Target As Global Variable
  [Documentation]  Set the provided test target globally for use by all test suites.
  [Arguments]  ${target_dict}
  # TODO - temporary, should set the whole dictionary as global, but having trouble accessing keys
  VAR  ${G_TARGET_NAME}  ${target_dict}[name]  scope=GLOBAL
  VAR  ${G_TARGET_HOST}  ${target_dict}[host]  scope=GLOBAL
  VAR  ${G_TARGET_PORT}  ${target_dict}[port]  scope=GLOBAL
  VAR  ${G_TARGET_USER}  ${target_dict}[user]  scope=GLOBAL
  VAR  ${G_TARGET_KEY}  ${target_dict}[key]  scope=GLOBAL
  VAR  ${G_TARGET_ARCH}  ${target_dict}[arch]  scope=GLOBAL
  VAR  ${G_TARGET_OS}  ${target_dict}[os]  scope=GLOBAL

Configure Test Target Platform
  [Documentation]  Configures the platform for the test target
  Configure Target Platform  ${G_TARGET_OS}

Determine Workloads For Test
  [Documentation]  Load workload configuration specified on the Robot command line.
  ${workloads_var} =  Get Variable Value  ${WORKLOADS}  ${EMPTY}
  IF  "${workloads_var}" != "${EMPTY}" and "${workloads_var}" != "None"
    ${normalized_dir} =  Normalize Path  ${WORKLOAD_CONFIG_DIR}
    VAR  ${workload_file}  ${normalized_dir}${/}${workloads_var}.json  scope=LOCAL
    File Should Exist  ${workload_file}
    ${workloads_dict} =  Read JSON File Into Dictionary  ${workload_file}
    VAR  ${G_WORKLOADS}  ${workloads_dict}  scope=GLOBAL
  ELSE
    ${empty_workloads} =  Create Dictionary
    VAR  ${G_WORKLOADS}  ${empty_workloads}  scope=GLOBAL
    Log To Console  No workloads supplied on command-line. Workload-dependent tests may skip.
  END

Get Workload Path
  [Documentation]  Return the workload path for the specified workload name, or empty if not available.
  [Arguments]  ${workload_name}
  ${workloads} =  Get Variable Value  ${G_WORKLOADS}  ${EMPTY}
  ${has_workload} =  Run Keyword And Return Status  Dictionary Should Contain Key  ${workloads}  ${workload_name}
  IF  ${has_workload}
    ${workload_meta} =  Get From Dictionary  ${workloads}  ${workload_name}
    ${has_path} =  Run Keyword And Return Status  Dictionary Should Contain Key  ${workload_meta}  workload_path
    IF  ${has_path}
      ${path} =  Get From Dictionary  ${workload_meta}  workload_path
      RETURN  ${path}
    END
  END
  RETURN  ${EMPTY}

Determine ATPerf CLI Bin Path
  [Documentation]  Determine the full path to the atperf CLI binary depending on the operating system.
  ...  Also normalize the path (to reduce up-level references etc.) before returning it.
  ${os} =  Determine Operating System
  IF  '${os}' == 'Linux'
    VAR  ${path}  ${CURDIR}${/}..${/}..${/}..${/}apap-cli${/}${PRODUCT_BINARY_NAME}  scope=LOCAL
  ELSE IF  '${os}' == 'Darwin'
    VAR  ${path}  ${CURDIR}${/}..${/}..${/}..${/}apap-cli${/}${PRODUCT_BINARY_NAME}  scope=LOCAL
  ELSE IF  '${os}' == 'Windows'
    VAR  ${path}  ${CURDIR}${/}..${/}..${/}..${/}apap-cli${/}${PRODUCT_BINARY_NAME}.exe  scope=LOCAL
  END
  ${normalized_path} =  Normalize Path  ${path}
  RETURN  ${normalized_path}

Determine Runs Directory Path
  [Documentation]  Determine the full path to the runs directory based on the APXD_DATA_DIR env var.
  ...  Also normalize the path (to reduce up-level references etc.) before returning it.
  ${env_var} =  Get Performix Env Var  DATA_DIR
  VAR  ${path}  ${env_var}${/}runs  scope=LOCAL
  ${normalized_path} =  Normalize Path  ${path}
  VAR  ${G_RUNS_DIR}  ${normalized_path}  scope=GLOBAL

Determine Data Directory Path
  [Documentation]  Determine the full path to the atperf user data directory based on the APXD_DATA_DIR env var.
  ...  Also normalize the path (to reduce up-level references etc.) before returning it.
  ${env_var} =  Get Performix Env Var  DATA_DIR
  VAR  ${path}  ${env_var}  scope=LOCAL
  ${normalized_path} =  Normalize Path  ${path}
  RETURN  ${normalized_path}

Determine Config Directory Path
  [Documentation]  Determine the full path to the config directory based on the APXD_CONFIG_DIR env var.
  ...  Also normalize the path (to reduce up-level references etc.) before returning it.
  ${env_var} =  Get Performix Env Var  CONFIG_DIR
  VAR  ${path}  ${env_var}  scope=LOCAL
  ${normalized_path} =  Normalize Path  ${path}
  RETURN  ${normalized_path}

Determine Engine Log Directory Path
  [Documentation]  Determine the full path to the ATP engine log dir based on the APXD_LOG_FILE env var.
  ...  Also normalize the path (to reduce up-level references etc.) before returning it.
  ${env_var} =  Get Performix Env Var  LOG_FILE
  ${log_dir}  ${_} =  Split Path  ${env_var}
  VAR  ${path}  ${log_dir}  scope=LOCAL
  ${normalized_path} =  Normalize Path  ${path}
  RETURN  ${normalized_path}

Determine Engine Log File Path
  [Documentation]  Determine the full path to the ATP engine log file based on the APXD_LOG_FILE env var.
  ...  Also normalize the path (to reduce up-level references etc.) before returning it.
  ${env_var} =  Get Performix Env Var  LOG_FILE
  VAR  ${path}  ${env_var}  scope=LOCAL
  ${normalized_path} =  Normalize Path  ${path}
  RETURN  ${normalized_path}

Determine GUI Log Directory Path
  [Documentation]  Determine the full path to the GUI install directory based on the host OS.
  ...  TODO: this keyword is not finished yet.
  VAR  ${todo_fake_path}  /this/is/a/fake/path  scope=LOCAL
  RETURN  ${todo_fake_path}

The ATPerf CLI Is Installed
  [Documentation]  Keyword to check that the ATPerf CLI binary exists in the expected location.
  ${bin} =  Determine ATPerf CLI Bin Path
  File Should Exist  ${bin}

Display ATPerf CLI Version
  [Documentation]  Displays the version of the ATPerf CLI currently installed.
  Run ATPerf CLI Command  version
  The Last Command Succeeded
  Log  ${G_LAST_RESULT.stdout}

Display Robot Version
  [Documentation]  Displays the version of Robot currently installed.
  Run Host Command  robot --version
  Log  ${G_LAST_RESULT.stdout}

Display Versions
  [Documentation]  Displays the versions of common tools used by test suites.
  Display ATPerf CLI Version
  Display Robot Version

Run ATPerf CLI Command
  [Documentation]  Invoke the atperf CLI. Always issue the --json flag.
  [Arguments]  ${cmd_string}
  ${bin} =  Determine ATPerf CLI Bin Path
  ${cmd} =  Catenate  ${bin} ${cmd_string} ${JSON_FLAG}
  Run Host Command  ${cmd}

Start ATPerf CLI Command
  [Documentation]  Start the atperf CLI in the background. Always issue the --json flag.
  ...  Returns a handle to the process that was started.
  [Arguments]  ${cmd_string}
  ${bin} =  Determine ATPerf CLI Bin Path
  ${cmd} =  Catenate  ${bin} ${cmd_string} ${JSON_FLAG}
  Start Host Command  ${cmd}

Build Code Hotspots Args
  [Arguments]  @{extra_args}
  ${base_args} =  Catenate  SEPARATOR=${SPACE}
  ...  --target ${G_TARGET_NAME}
  ...  ${DEPLOY_TOOLS_FLAG}
  ${extra} =  Catenate  SEPARATOR=${SPACE}  @{extra_args}
  ${full_args} =  Catenate  SEPARATOR=${SPACE}  ${base_args}  ${extra}
  RETURN  ${full_args}

Build CPU Microarchitecture Args
  [Arguments]  @{extra_args}
  ${base_args} =  Catenate  SEPARATOR=${SPACE}
  ...  --target ${G_TARGET_NAME}
  ...  ${DEPLOY_TOOLS_FLAG}
  ${extra} =  Catenate  SEPARATOR=${SPACE}  @{extra_args}
  ${full_args} =  Catenate  SEPARATOR=${SPACE}  ${base_args}  ${extra}
  RETURN  ${full_args}

Get Default Launch Workload
  [Documentation]  Return a default workload command suitable for the configured target OS.
  IF  '${G_TARGET_OS}' == '${OS_WINDOWS}'  RETURN  "python -c 10**10**10"
  RETURN  "dd if=/dev/zero of=/dev/null bs=1M count=102400"

Set Default Launch Workload
  [Documentation]  Resolve and store the default workload command for the configured target.
  ${launch_workload} =  Get Default Launch Workload
  VAR  ${LAUNCH_WORKLOAD}  ${launch_workload}  scope=GLOBAL

Wait For Process And Log
  [Documentation]  Wait for a command in the background to finish
  [Arguments]  ${process}
  ${result} =  Wait For Process  ${process}
  VAR  ${G_LAST_RESULT}  ${result}  scope=GLOBAL
  Log Many  rc: ${G_LAST_RESULT.rc}  stdout: ${G_LAST_RESULT.stdout}  stderr: ${G_LAST_RESULT.stderr}

Ensure The File Is Created On The Target
  [Documentation]  Create a file on the target
  [Arguments]  ${path}
  Create Target File  ${path}
  The Last Command Succeeded

The File Is Not Executable On The Target
  [Documentation]  Verify that a file on the target is not executable
  [Arguments]  ${path}
  Check Target File Not Executable  ${path}
  The Last Command Succeeded

The File Is Removed From The Target
  [Documentation]  Remove a file from the target
  [Arguments]  ${path}
  Remove Target File  ${path}
  The Last Command Succeeded

Determine Operating System
  [Documentation]  Determine which operating system Robot is running on.
  ${current_os} =  Evaluate  platform.system()  modules=platform
  RETURN  ${current_os}

The Operating System Is Linux
  [Documentation]  Determine whether Robot is currently running on Linux.
  ${os} =  Determine Operating System
  Should Be Equal As Strings  ${os}  Linux

The Operating System Is Darwin
  [Documentation]  Determine whether Robot is currently running on MacOS.
  ${os} =  Determine Operating System
  Should Be Equal As Strings  ${os}  Darwin

The Operating System Is Windows
  [Documentation]  Determine whether Robot is currently running on Windows.
  ${os} =  Determine Operating System
  Should Be Equal As Strings  ${os}  Windows

Determine Architecture
  [Documentation]  Determine which architecture Robot is running on.
  ${arch} =  Evaluate  platform.machine().lower()  modules=platform
  RETURN  ${arch}

The Architecture Is Arm
  [Documentation]  Determine whether Robot is currently running on ARM.
  ${arch} =  Determine Architecture
  Should Contain Any  ${arch}  arm  aarch

The Architecture Is X86
  [Documentation]  Determine whether Robot is currently running on x86-64 (AMD64).
  ${arch} =  Determine Architecture
  Should Contain Any  ${arch}  x86_64  amd64

The Last Command Return Code Is
  [Documentation]  Confirms that the last command returned a specific return code.
  [Arguments]  ${rc}
  Should Be Equal As Integers  ${G_LAST_RESULT.rc}  ${rc}

The Last Command Return Code Is Not
  [Documentation]  Confirms that the last command did not return a specific return code.
  [Arguments]  ${rc}
  Should Not Be Equal As Integers  ${G_LAST_RESULT.rc}  ${rc}

The Last Command Failed
  [Documentation]  Confirm that the last command returned a non-zero return code.
  The Last Command Return Code Is Not  0

The Last Command Succeeded
  [Documentation]  Confirm that the last command returned a zero return code.
  The Last Command Return Code Is  0

The Last Command Failed With Message Code
  [Documentation]  Confirm that the last command returned an error with the specified message code.
  [Arguments]  ${expected_code}
  ${result} =  Parse Last JSON Stdout Line To Dictionary
  ${error} =  Get From Dictionary  ${result}  error
  ${actual_code} =  Get From Dictionary  ${error}  message_code
  Should Be Equal  ${actual_code}  ${expected_code}

The Last Command Did Not Fail With Message Code
  [Documentation]  Confirm that the last command did not return an error with the specified message code.
  ...  Note that this keyword does not require that the last command succeeded, nor that it failed; only
  ...  that if it did fail, it did not return an error with the specified message code.
  [Arguments]  ${unwanted_code}
  IF  not ${G_LAST_RESULT.rc}  RETURN
  ${result} =  Parse Last JSON Stdout Line To Dictionary
  ${error} =  Get From Dictionary  ${result}  error
  ${actual_code} =  Get From Dictionary  ${error}  message_code
  Should Not Be Equal  ${actual_code}  ${unwanted_code}

Parse Last JSON Stdout Line To Dictionary
  [Documentation]  Take the last non-empty line from stdout as json and parse it to a dictionary
  ${lines} =  Split String  ${G_LAST_RESULT.stdout}  \n
  Remove Values From List  ${lines}  ${EMPTY}
  ${num_lines} =  Get Length  ${lines}
  IF  ${num_lines} > 0
    ${last_line} =  Get From List  ${lines}  -1
    ${dict} =  Parse JSON To Dictionary  ${last_line}
    RETURN  ${dict}
  ELSE
    RETURN  ${EMPTY}
  END

Parse JSON To Dictionary
  [Arguments]  ${json}
  ${dict} =  Evaluate  json.loads(r'''${json}''')  json
  RETURN  ${dict}

Read JSON File Into Dictionary
  [Arguments]  ${file}
  ${json_file} =  Get File  ${file}
  ${dict} =  Parse JSON To Dictionary  ${json_file}
  RETURN  ${dict}

Normalize Key Path In Target Dictionary
  [Documentation]  Takes a target dictionary, extracts the key path, normalizes
  ...  the path, and then replaces it back in the dictionary and returns it.
  [Arguments]  ${dict}
  ${key_path} =  Get From Dictionary  ${dict}  key
  ${normalized_path} =  Normalize Path  ${key_path}
  Set To Dictionary  ${dict}  key  ${normalized_path}
  RETURN  ${dict}

Get External Recipe Path
  [Documentation]  Takes a recipe filename and returns its full path, in resources/files/recipes
  [Arguments]  ${name}
  RETURN  ${EXTERNAL_RECIPES_DIR}${/}${name}

Display Test Target Details
  [Documentation]  This keyword prints the test target details to the robot log.
  Log Many  Name: ${G_TARGET_NAME}  Host: ${G_TARGET_HOST}  Port: ${G_TARGET_PORT}
  ...  User: ${G_TARGET_USER}  Key: ${G_TARGET_KEY}  Arch: ${G_TARGET_ARCH}
  ...  OS: ${G_TARGET_OS}
  Log To Console  Using test target details from supplied config file:\n
  Log To Console  Name: ${G_TARGET_NAME}
  Log To Console  Host: ${G_TARGET_HOST}
  Log To Console  Port: ${G_TARGET_PORT}
  Log To Console  User: ${G_TARGET_USER}
  Log To Console  Key: ${G_TARGET_KEY}
  Log To Console  Arch: ${G_TARGET_ARCH}
  Log To Console  OS: ${G_TARGET_OS}\n

Set Test Username
  [Documentation]  Set the username used by deployment verification keywords.
  [Arguments]  ${username}=${G_TARGET_USER}
  Should Not Be Empty  ${username}
  ...  msg=The username used for deployment verification must not be empty.
  VAR  ${G_TEST_USERNAME}  ${username}  scope=SUITE

Get Localhost User
  [Documentation]  Resolve the current host username when the test target is localhost.
  ${localhost_user} =  Get Environment Variable  USER  ${EMPTY}
  IF  '${localhost_user}' == '${EMPTY}'
    ${localhost_user} =  Get Environment Variable  USERNAME  ${EMPTY}
  END
  Should Not Be Empty  ${localhost_user}
  ...  msg=Neither USER nor USERNAME is set; cannot determine the localhost user for robot tests.
  RETURN  ${localhost_user}

Prepare Common Test Environment
  [Documentation]  Configure shared test terminology and environment variables.
  Configure Terminology
  Set ATP Env Vars

Common Setup
  [Documentation]  Common setup steps for test suites. Must be called first in suite setup.
  Prepare Common Test Environment
  The ATPerf CLI Is Installed
  Display Versions
  Determine Workloads For Test
  Determine Target For Test
  Set Default Launch Workload
  Set Test Username
  Display Test Target Details
  Determine Runs Directory Path
  Configure Test Target Platform

Common Teardown
  [Documentation]  Common teardown steps for test suites. Must be called last in suite teardown.
  Set ATP Env Vars
  Export All Runs
  Delete All Runs
  Clear ATP Env Vars

Clear Test Target Output Directories
  [Documentation]  Removes any atperf output directories from the target and verifies they are gone.
  Remove Target Run Directories
  Remove Target Deployment Directories
  Target Run Directories Exist
  VAR  ${stdout} =  ${G_LAST_RESULT.stdout.strip()}  scope=LOCAL
  Should Be Equal As Strings  ${stdout}  EMPTY  There are atperf output directories on the remote machine.

Ensure Python3 Available On Target
  [Documentation]  Ensure python3 is available on the target.
  Run Target Command  python3 --help
  The Last Command Succeeded

Count Processes Running On Target
  [Documentation]  Return the number of processes running on the target that match the given command line or process name
  [Arguments]  ${match_string}  ${match_on_proc_name}=${False}
  Count Target Processes Matching Command  ${match_string}  ${match_on_proc_name}
  ${count} =  Evaluate  int(${G_LAST_RESULT.stdout.strip()})
  RETURN  ${count}

Processes Are Running On The Target
  [Documentation]  Check that a given number of processes matching the given command line or process name are running on the target
  [Arguments]  ${match_string}  ${expected_count}  ${match_on_proc_name}=${False}
  ${count} =  Count Processes Running On Target  ${match_string}  ${match_on_proc_name}
  Should Be Equal As Integers  ${count}  ${expected_count}

No Processes Are Running On The Target
  [Documentation]  Check that no processes matching the given command line or process name are running on the target
  [Arguments]  ${match_string}  ${match_on_proc_name}=${False}
  Processes Are Running On The Target  ${match_string}  0  ${match_on_proc_name}

Wait For Multiple Processes To Run On The Target
  [Documentation]  Wait until a number of processes are running on the target that match the given command line.
  ...  Timeout after the given number of iterations.
  [Arguments]  ${match_string}  ${proc_count}  ${iters}=20  ${match_on_proc_name}=${False}
  FOR  ${_}  IN RANGE  ${iters}
    ${count} =  Count Processes Running On Target  ${match_string}  ${match_on_proc_name}
    IF  ${count} == ${proc_count}  RETURN
    Sleep  100ms
  END
  Fail
  ...  Waiting for ${proc_count} processes with command line ${match_string} to run on target; condition not met after ${iters} iterations

Wait For Workload To Run On The Target
  [Documentation]  Wait for the workload process started via "Start ATPerf CLI Command" to run on the target by matching the given string to the
  ...  processes cmdline. Timeout after the given number of iterations and log the run details if the wait fails.
  [Arguments]  ${match_string}  ${iters}=40  ${match_on_proc_name}=${False}
  TRY
    Wait For Multiple Processes To Run On The Target  ${match_string}  1  ${iters}  ${match_on_proc_name}
  EXCEPT  AS  ${err}
    Wait For Process And Log  ${G_LAST_PROCESS}
    Fail  ${err}
  END

Write To Process Stdin
  [Documentation]  Write a string to a process's stdin
  [Arguments]  ${process}  ${string}
  ${bytes} =  Evaluate  ${string}.encode()
  Call Method  ${process}  communicate  input=${bytes}

Skip If Host Operating System Is Not
  [Arguments]  ${os}
  ${host_os} =  Determine Operating System
  Skip If  '${host_os}' != '${os}'  msg=Not supported on this operating system, skipping

Skip If Host Architecture Is Not
  [Arguments]  ${arch}
  ${host_arch} =  Determine Architecture
  Skip If  '${host_arch}' != '${arch}'  msg=Not supported on this architecture, skipping

Run Keyword Without Logging
  [Documentation]  Run a keyword without logging its execution in the log file. Useful for hiding sensitive information.
  [Arguments]  ${keyword}  @{args}
  ${prev_log_level} =  Set Log Level  NONE
  Run Keyword  ${keyword}  @{args}
  Set Log Level  ${prev_log_level}
