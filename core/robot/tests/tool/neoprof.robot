# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A test suite to verify the functionality of the 'neoprof' tool integration.

Resource            ../../resources/keywords/common.resource
Resource            ../../resources/keywords/process.resource
Resource            ../../resources/keywords/recipe.resource
Resource            ../../resources/keywords/run.resource
Resource            ../../resources/keywords/target.resource

Suite Setup         Neoprof Suite Setup
Suite Teardown      Neoprof Suite Teardown

Test Tags           neoprof


*** Variables ***
${TEMP_FILE_PATH_2}     ${EMPTY}
${TEMP_FILE_NAME_2}     tempFile2
${TARGET_HOME_DIR}      ${EMPTY}


*** Test Cases ***
The Neoprof Tool Integration Returns Workload Doesn't Exist Error
  [Documentation]  Tests the neoprof tool integration returns the expected error message
  ...  if the specified workload doesn't exist
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  When Run Code Hotspots Recipe  --workload my-made-up-workload --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed With Message Code  tool_integrations.common.WORKLOAD_NOT_EXIST_OR_NOT_EXECUTABLE
  And The Target Output Directory Is Empty

The Neoprof Tool Integration Returns Workload Isn't Executable Error
  [Documentation]  Tests the neoprof tool integration returns the expected error message
  ...  if the specified workload exists but isn't executable
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  And Ensure The File Is Created On The Target  ${TEMP_FILE_PATH}
  And The File Is Not Executable On The Target  ${TEMP_FILE_PATH}
  When Run Code Hotspots Recipe  --workload ${TEMP_FILE_PATH} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed With Message Code  tool_integrations.common.WORKLOAD_NOT_EXIST_OR_NOT_EXECUTABLE
  And The Target Output Directory Is Empty
  [Teardown]  The File Is Removed From The Target  ${TEMP_FILE_PATH}

The Neoprof Tool Integration Returns Workload File Not Found Error
  [Documentation]  Tests the neoprof tool integration returns the expected error message
  ...  if the workload string takes the form of "bash script_that_does_not_exist.sh"
  ...  causing the kernel to emit "No such file or directory" when attempting to exec it
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  When Run Code Hotspots Recipe
  ...  --workload "bash script_that_does_not_exist.sh" --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed With Message Code  tool_integrations.neoprof.WORKLOAD_FILE_NOT_FOUND
  And The Target Output Directory Is Empty

The Neoprof Tool Integration Returns PID Doesn't Exist Error
  [Documentation]  Tests the neoprof tool integration returns the expected error message
  ...  if the specified PID doesn't exist
  #  Disabled for now as we now perform system-wide captures when the --pid flag is supplied, but don't
  #  detect when the specified PID does not exist - see https://jira.arm.com/browse/APAP-842
  [Tags]  disabled
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  When Run Code Hotspots Recipe  --pid 999999 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed With Message Code  tool_integrations.neoprof.PID_NOT_EXIST
  And The Target Output Directory Is Empty

The Neoprof Tool Integration Returns Sl-Record Crashed Unknown Error
  [Documentation]  Tests the neoprof tool integration returns the expected error message
  ...  if sl-record returns a non-zero exit code, but no known message is found in the logs
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  When Run Code Hotspots Recipe And Kill Sl-Record  --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}  -9
  Then The Last Command Failed With Message Code  tool_integrations.neoprof.NEOPROF_FAILED
  And The Target Output Directory Is Empty

The Neoprof Tool Integration Returns No Samples Collected Error
  [Documentation]  Tests the neoprof tool integration returns the expected error message
  ...  if a very short workload with a low sampling rate yields no samples
  # This test is disabled because it's unreliable. Only re-enable when we have a more deterministic way to ensure no
  # samples are collected. See APAP-4648 for details.
  [Tags]  disabled
  [Setup]  Run Keyword And Ignore Error  The Output Directory Is Removed From The Target
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  And The Target Is Prepared
  When Run Code Hotspots Recipe  --workload "true" --param sampling_freq=low --target ${G_TARGET_NAME}
  Then The Last Command Failed With Message Code  tool_integrations.neoprof.NO_SAMPLES_COLLECTED
  And The Target Output Directory Is Empty
  [Teardown]  Run Keyword And Ignore Error  The Output Directory Is Removed From The Target

The Neoprof Tool Passes Environment Variables To The Inline Workload
  [Documentation]  Tests the neoprof tool integration passes the environment
  ...  variables provided by the user through to the inline workload
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  When Run Code Hotspots Recipe With Environment Variables In Workload
  And The Last Command Succeeded
  Then The Neoprof Capture Log Contains  this is bar

The Neoprof Tool Passes Environment Variables To The Workload Script
  [Documentation]  Tests the neoprof tool integration passes the environment
  ...  variables provided by the user through to the workload script
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  And The Script Is Created On The Target  ${TEMP_FILE_PATH}  echo "this is \$FOO"
  When Run Code Hotspots Recipe
  ...  --workload ${TEMP_FILE_PATH} --env FOO=bar --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  And The Last Command Succeeded
  Then The Neoprof Capture Log Contains  this is bar
  [Teardown]  The File Is Removed From The Target  ${TEMP_FILE_PATH}

The Neoprof Tool Uses The Specified Working Dir To Launch The Workload
  [Documentation]  Tests the neoprof tool integration uses the working dir
  ...  specified by the user to launch the workload
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  And The Script Is Created On The Target  ${TEMP_FILE_PATH}  echo "this is a test"
  When Run Code Hotspots Recipe
  ...  --workload ./${TEMP_FILE_NAME} --working-dir ${ATPERF_DIR} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  And The Last Command Succeeded
  Then The Neoprof Capture Log Contains  this is a test
  [Teardown]  The File Is Removed From The Target  ${TEMP_FILE_PATH}

The Neoprof Tool Uses The Specified Working Dir Within The Workload
  [Documentation]  Tests the neoprof tool integration uses the working dir
  ...  specified by the user within the workload
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  And The Script Is Created On The Target  ${TEMP_FILE_PATH}  echo "file 1"; ./${TEMP_FILE_NAME_2}
  And The Script Is Created On The Target  ${TEMP_FILE_PATH_2}  echo "file 2"
  When Run Code Hotspots Recipe
  ...  --workload ./${TEMP_FILE_NAME} --working-dir ${ATPERF_DIR} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  And The Last Command Succeeded
  Then The Neoprof Capture Log Contains  file 1
  And The Neoprof Capture Log Contains  file 2
  [Teardown]  Run Keywords  The File Is Removed From The Target  ${TEMP_FILE_PATH}
  ...  AND  The File Is Removed From The Target  ${TEMP_FILE_PATH_2}

The Neoprof Tool Uses The User's Home Dir If Working Dir Is Not Specified
  [Documentation]  Tests the neoprof tool integration uses the user's home dir
  ...  as the working dir for the workload if the user didn't specify one themselves.
  Given The Code Hotspots Recipe Is Listed
  And The Test Target Exists
  And The Target Home Dir Is Stored
  And The Script Is Created On The Target  ${TEMP_FILE_PATH}  pwd
  When Run Code Hotspots Recipe  --workload ${TEMP_FILE_PATH} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  And The Last Command Succeeded
  Then The Neoprof Capture Log Contains  ${TARGET_HOME_DIR}
  [Teardown]  The File Is Removed From The Target  ${TEMP_FILE_PATH}

The Neoprof Tool Reports As Not Ready When Workload Does Not Exist
  [Documentation]  Tests that the neoprof tool integration correctly reports as not ready
  ...  when the workload does not exist on the target.
  Given The Code Hotspots Recipe Is Listed
  When Check Recipe Is Ready  code_hotspots  --workload my-made-up-workload --target ${G_TARGET_NAME}
  Then The Recipe Is Not Ready
  And Check Advice Messages Contain
  ...  "The specified command does not exist or is not executable. Please verify this executable exists."

The Neoprof Tool Reports As Ready When Workload Exists Using Working Dir
  [Documentation]  Tests that the neoprof tool integration correctly reports as ready
  ...  when the workload exists on the target using the specified working directory.
  Given The Code Hotspots Recipe Is Listed
  When The Script Is Created On The Target  ${TEMP_FILE_PATH}  ls
  And Check Recipe Is Ready
  ...  code_hotspots
  ...  --workload "./${TEMP_FILE_NAME}" --working-dir ${ATPERF_DIR} --target ${G_TARGET_NAME}
  Then The Recipe Is Ready
  [Teardown]  The File Is Removed From The Target  ${TEMP_FILE_PATH}


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.

Neoprof Suite Setup
  Common Setup
  Set Suite Variables
  The Test Target Is Added Successfully
  The Test Target Is Prepared Successfully
  Deploy Tools For Recipe  code_hotspots

Neoprof Suite Teardown
  The Test Target Is Removed Successfully
  Common Teardown

Set Suite Variables
  VAR  ${TEMP_FILE_PATH} =  ${ATPERF_DIR}/${TEMP_FILE_NAME}  scope=SUITE
  VAR  ${TEMP_FILE_PATH_2} =  ${ATPERF_DIR}/${TEMP_FILE_NAME_2}  scope=SUITE

Run Code Hotspots Recipe And Kill Sl-Record
  [Documentation]  Starts running the code_hotspots recipe with the workload "sleep 15", then
  ...  kills the sl-record processes that were launched on the target. G_LAST_RESULT will contain
  ...  the result of the recipe run
  [Arguments]  ${args}  ${killArgs}
  ${timed_args} =  Apply Default Recipe Timeout  --workload "sleep 15" ${args}
  ${process} =  Start ATPerf CLI Command  recipe run code_hotspots ${timed_args}
  Wait For Matching Process And Kill With Timeout  [s]l-record.*sleep  20  ${killArgs}
  Wait For Process And Log  ${process}

Wait For Matching Process And Kill With Timeout
  [Documentation]  Continually checks for the existence of a process whose command matches
  ...  the provided regex, and kills it once it exists with the provided args. Exits if
  ...  the max iterations are reached
  [Arguments]  ${regex}  ${iters}  ${killArgs}
  Wait For Matching Process On Target  ${regex}  ${iters}
  Run Target Command  "sudo pkill -f ${killArgs} '${regex}'"

The Target Home Dir Is Stored
  [Documentation]  Helper keyword to record the user's home dir on the target.
  ${homeDir} =  Get Home Dir On Target
  ${sanitised} =  Strip String  ${homeDir.stdout}
  VAR  ${TARGET_HOME_DIR} =  ${sanitised}  scope=SUITE

Run Code Hotspots Recipe With Environment Variables In Workload
  ${code_hotspots_args} =  Catenate  --workload "bash -c \\"echo \\\\\\"this is \$FOO\\\\\\"\\""
  ...  --env FOO=bar --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  ${escaped} =  Escape Dollar If Needed  ${code_hotspots_args}
  Run Code Hotspots Recipe  ${escaped}
