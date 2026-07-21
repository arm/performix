# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A resource file containing 'target' keywords for the Arm Total Performance CLI.

Resource            common.resource
Resource            ssh.resource
Resource            target_extra.resource
Library             String


*** Variables ***
${LOCALHOST_TARGET_NAME}          localhost
${G_NUM_TARGETS}                  ${EMPTY}  # Global variable to track last known number of targets

# The following variables are set when configuring the target platform
${COLLECTOR_BINARY_FILE_NAME}     ${EMPTY}
${AGENT_BINARY_FILE_NAME}         ${EMPTY}
${JITDUMP_JVM_BINARY_FILE_NAME}   ${EMPTY}
${DOTNET_AGENT_BINARY_FILE_NAME}  ${EMPTY}
${ATPERF_DIR}                     ${EMPTY}
${ATPERF_TOOLS_DIR}               ${EMPTY}
${EXPECTED_PID}                   ${EMPTY}
${EXPECTED_PROCESS_NAME}          ${EMPTY}


*** Keywords ***
List Targets
  Run ATPerf CLI Command  target list

Run Target Info
  [Arguments]  ${target}  ${pid}=${EMPTY}
  Run ATPerf CLI Command  target info ${target} ${pid}

Count Targets
  [Documentation]  Counts the number of targets in the targets dictionary.
  List Targets
  ${targets} =  Get Targets As Dictionary
  ${num_targets} =  Get Length  ${targets}
  VAR  ${G_NUM_TARGETS}  ${num_targets}  scope=GLOBAL

The Number Of Targets Is
  [Arguments]  ${expected}
  Count Targets
  Should Be Equal As Integers  ${expected}  ${G_NUM_TARGETS}

The Number Of Targets Is At Least
  [Arguments]  ${expected}
  Count Targets
  ${is_at_least} =  Evaluate  ${expected} >= ${G_NUM_TARGETS}
  Should Be True  ${is_at_least}

The Number Of Targets Has Increased By
  [Documentation]  Checks the number of targets has increased by the specified amount, by
  ...  taking note of the old count, re-counting the number of targets, and finding the difference.
  [Arguments]  ${expected}
  VAR  ${old_num}  ${G_NUM_TARGETS}  scope=LOCAL
  Count Targets
  VAR  ${new_num}  ${G_NUM_TARGETS}  scope=LOCAL
  ${increase} =  Evaluate  ${new_num} - ${old_num}
  Should Be Equal As Integers  ${increase}  ${expected}

The Number Of Targets Has Decreased By
  [Documentation]  Checks the number of targets has decreased by the specified amount, by
  ...  taking note of the old count, re-counting the number of targets, and finding the difference.
  [Arguments]  ${expected}
  VAR  ${old_num}  ${G_NUM_TARGETS}  scope=LOCAL
  Count Targets
  VAR  ${new_num}  ${G_NUM_TARGETS}  scope=LOCAL
  ${decrease} =  Evaluate  ${old_num} - ${new_num}
  Should Be Equal As Integers  ${decrease}  ${expected}

There Are No Targets
  The Number Of Targets Is  0

There Are No SSH Targets
  ${no_targets} =  Run Keyword And Return Status  There Are No Targets
  ${localhost_is_only_target} =  Run Keyword And Return Status  The Only Target Is Localhost
  Should Be True  ${no_targets} or ${localhost_is_only_target}

There Is At Least One Target
  The Number Of Targets Is At Least  1

The Only Target Is Localhost
  Localhost Is In Target List
  The Number Of Targets Is  1

Localhost Is In Target List
  The Target Exists  ${LOCALHOST_TARGET_NAME}

Localhost Is Not In Target List
  The Target Does Not Exist  ${LOCALHOST_TARGET_NAME}

Add Test Target
  [Documentation]  Adds the test target specified by the user on the robot command line (-v TARGET:<path>)
  Add Target  ${G_TARGET_USER}@${G_TARGET_HOST}:${G_TARGET_PORT}:${G_TARGET_KEY} --name ${G_TARGET_NAME}

Ensure The Test Target Exists
  [Documentation]  Adds the configured test target if it is not already present.
  ${exists} =  Run Keyword And Return Status  The Test Target Exists
  IF  not ${exists}  The Test Target Is Added Successfully

Add Target
  [Documentation]  Adds a new target with the details specified in the supplied args string.
  [Arguments]  ${args}
  Run ATPerf CLI Command  target add ${args}

The Test Target Is Added Successfully
  [Documentation]  Add the test target and check the result is successful.
  Count Targets
  Add Test Target
  The Last Command Succeeded
  The Number Of Targets Has Increased By  1

The Target Is Added Successfully
  [Documentation]  Add the specified target and check the result is successful.
  [Arguments]  ${args}
  Count Targets
  Add Target  ${args}
  The Last Command Succeeded
  The Number Of Targets Has Increased By  1

The Target Is Not Added Successfully
  [Documentation]  Add the specified target and check the result is not successful.
  [Arguments]  ${args}
  Count Targets
  Add Target  ${args}
  The Last Command Failed
  The Number Of Targets Is  ${G_NUM_TARGETS}

The Target Is Displayed Correctly
  [Arguments]  ${host}  ${user}  ${key}  ${port}  ${name}
  The Target Is Single SSH With Details Containing  ${name}  host  ${host}
  The Target Is Single SSH With Details Containing  ${name}  username  ${user}
  The Target Is Single SSH With Details Containing  ${name}  private_key_filename  ${key}
  The Target Is Single SSH With Details Containing  ${name}  port  ${port}

Remove Target
  [Arguments]  ${name}
  Run ATPerf CLI Command  target remove ${name}

Remove Test Target
  [Documentation]  Removes the test target specified by the user on the robot command line (-v TARGET:<path>)
  Remove Target  ${G_TARGET_NAME}

Ensure Target Does Not Exist
  [Arguments]  ${name}
  Remove Target  ${name}
  The Target Does Not Exist  ${name}

The Test Target Is Removed Successfully
  [Documentation]  Remove the test target and check the result is successful.
  The Target Is Removed Successfully  ${G_TARGET_NAME}

The Target Is Removed Successfully
  [Documentation]  Remove a target and then check it has been removed successfully.
  [Arguments]  ${name}
  Count Targets
  Remove Target  ${name}
  The Last Command Succeeded
  The Number Of Targets Has Decreased By  1
  The Target Does Not Exist  ${name}

Remove All Targets
  Run ATPerf CLI Command  target remove --all

The Test Target Exists
  ${targets} =  Get Targets As Dictionary
  Dictionary Should Contain Key  ${targets}  ${G_TARGET_NAME}

The Target Exists
  [Arguments]  ${name}
  ${targets} =  Get Targets As Dictionary
  Dictionary Should Contain Key  ${targets}  ${name}

The Target Does Not Exist
  [Arguments]  ${name}
  ${targets} =  Get Targets As Dictionary
  Dictionary Should Not Contain Key  ${targets}  ${name}

Set Default Target
  [Arguments]  ${name}
  Run ATPerf CLI Command  target default ${name}

The Target Is The Default
  [Arguments]  ${name}
  ${targets} =  Get Targets As Dictionary
  ${target} =  Get From Dictionary  ${targets}  ${name}
  Dictionary Should Contain Item  ${target}  default  ${true}

The Target Is Not The Default
  [Arguments]  ${name}
  ${targets} =  Get Targets As Dictionary
  ${target} =  Get From Dictionary  ${targets}  ${name}
  Dictionary Should Contain Item  ${target}  default  ${false}

The Target Is Set To Default Successfully
  [Documentation]  Set a target to default and check the result is successful.
  [Arguments]  ${name}
  Set Default Target  ${name}
  The Last Command Succeeded
  The Target Is The Default  ${name}

Test Target
  [Documentation]  Test a target without checking the result.
  [Arguments]  ${name}
  Run ATPerf CLI Command  target test --target ${name}

The Target Test Is Successful
  [Documentation]  Test a target and check the result is successful.
  [Arguments]  ${name}
  Test Target  ${name}
  The Last Command Succeeded

The Target Test Is Not Successful
  [Arguments]  ${name}
  Test Target  ${name}
  The Last Command Failed

Update Target
  [Documentation]  Update a single field of a target.
  [Arguments]  ${name}  ${field}  ${new_val}
  Run ATPerf CLI Command  target update ${name} --${field} ${new_val}

Get Targets As Dictionary
  [Documentation]  Lists all targets and returns the JSON data as a dictionary.
  List Targets
  ${targets} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${targets}  data
  RETURN  ${data}

Get Target Details As Dictionary
  [Documentation]  Returns a specific target as a dictionary.
  [Arguments]  ${name}
  ${targets} =  Get Targets As Dictionary
  ${target} =  Get From Dictionary  ${targets}  ${name}
  RETURN  ${target}

The Target Is Single SSH With Details Containing
  [Documentation]  Check that the target details for a particular target indicate it is an SSH target
  ...  with 1 jump, and that jump contains the specified key/value pair.
  [Arguments]  ${name}  ${field}  ${expected_value}
  ${details} =  Get Target Details As Dictionary  ${name}
  ${type} =  Get From Dictionary  ${details}  type
  ${value} =  Get From Dictionary  ${details}  value
  Should Be Equal As Strings  ${type}  ssh
  ${jumps} =  Get From Dictionary  ${value}  jumps
  ${jump} =  Get From List  ${jumps}  0
  ${field_value} =  Get From Dictionary  ${jump}  ${field}
  Should Be Equal As Strings  ${field_value}  ${expected_value}

The Collector Daemon Is Deployed
  [Documentation]  Check that the collector daemon has been deployed to the target.
  ${target_os} =  Get Variable Value  ${G_TARGET_OS}  ${EMPTY}
  IF  '${target_os}' == '${OS_WINDOWS}'
    Log  Skipping Collector deployment check for Windows target.  DEBUG
    RETURN
  END
  Check Tool Deployed  ${COLLECTOR_BINARY_FILE_NAME}  ${G_TEST_USERNAME}
  The Last Command Succeeded

The Collector Daemon Is Not Deployed
  [Documentation]  Check that the collector daemon has not been deployed to the target.
  ${target_os} =  Get Variable Value  ${G_TARGET_OS}  ${EMPTY}
  IF  '${target_os}' == '${OS_WINDOWS}'
    Log  Skipping Collector deployment check for Windows target.  DEBUG
    RETURN
  END
  Check Tool Not Deployed  ${COLLECTOR_BINARY_FILE_NAME}  ${G_TEST_USERNAME}
  The Last Command Succeeded

The Target Agent Is Deployed
  [Documentation]  Check that the target agent has been deployed to the target.
  Check Tool Deployed  ${AGENT_BINARY_FILE_NAME}  ${G_TEST_USERNAME}
  The Last Command Succeeded

The Target Agent Is Not Deployed
  [Documentation]  Check that the target agent has not been deployed to the target.
  Check Tool Not Deployed  ${AGENT_BINARY_FILE_NAME}  ${G_TEST_USERNAME}
  The Last Command Succeeded

Jitdump-JVM Is Deployed
  [Documentation]  Check that Jitdump-JVM has been deployed to the target (AArch64 only).
  ${target_arch} =  Get Variable Value  ${G_TARGET_ARCH}  ${EMPTY}
  IF  '${target_arch}' != '${ARCH_AARCH64}'
    Log  Skipping jitdump-jvm deployment check for non-AArch64 target.  DEBUG
    RETURN
  END
  Check Tool Deployed  ${JITDUMP_JVM_BINARY_FILE_NAME}  ${G_TEST_USERNAME}
  The Last Command Succeeded

Jitdump-JVM Is Not Deployed
  [Documentation]  Check that Jitdump-JVM has not been deployed to the target (AArch64 only).
  ${target_arch} =  Get Variable Value  ${G_TARGET_ARCH}  ${EMPTY}
  IF  '${target_arch}' != '${ARCH_AARCH64}'
    Log  Skipping jitdump-jvm deployment check for non-AArch64 target.  DEBUG
    RETURN
  END
  Check Tool Not Deployed  ${JITDUMP_JVM_BINARY_FILE_NAME}  ${G_TEST_USERNAME}
  The Last Command Succeeded

The Dotnet Agent Is Deployed
  [Documentation]  Check that the .NET agent has been deployed to the target (Linux only).
  ${target_os} =  Get Variable Value  ${G_TARGET_OS}  ${EMPTY}
  IF  '${target_os}' == '${OS_WINDOWS}'
    Log  Skipping .NET agent deployment check for Windows target.  DEBUG
    RETURN
  END
  Check Tool Deployed  ${DOTNET_AGENT_BINARY_FILE_NAME}  ${G_TEST_USERNAME}
  The Last Command Succeeded

The Dotnet Agent Is Not Deployed
  [Documentation]  Check that the .NET agent has not been deployed to the target (Linux only).
  ${target_os} =  Get Variable Value  ${G_TARGET_OS}  ${EMPTY}
  IF  '${target_os}' == '${OS_WINDOWS}'
    Log  Skipping .NET agent deployment check for Windows target.  DEBUG
    RETURN
  END
  Check Tool Not Deployed  ${DOTNET_AGENT_BINARY_FILE_NAME}  ${G_TEST_USERNAME}
  The Last Command Succeeded

Kill The Target Agent
  [Documentation]  Kill the agent process running on the target.
  Kill Target Process  ${AGENT_BINARY_FILE_NAME}

Remove The Target Deployment Directory
  [Documentation]  Remove the atperf deployment directory on the target.
  Remove Target Directory  ${ATPERF_DIR}

Remove The Target Tools Deployment Directory
  [Documentation]  Remove the atperf tools deployment subdirectory on the target.
  Remove Target Directory  ${ATPERF_TOOLS_DIR}

Remove The Instruction Mix Tool Deployment Directory
  [Documentation]  Remove only the instruction mix tool deployment from the target.
  Remove Target Directory  ${ATPERF_TOOLS_DIR}/instruction_mix

The Collector Daemon Is Not Running
  [Documentation]  Check that the collector daemon is not running on the target.
  Target Process Is Not Running  ${COLLECTOR_BINARY_FILE_NAME}
  The Last Command Succeeded

The Collector Daemon Is Running
  [Documentation]  Check that the collector daemon is running on the target.
  Target Process Is Running  ${COLLECTOR_BINARY_FILE_NAME}
  The Last Command Succeeded

Prepare Target
  [Documentation]  Prepare the target
  [Arguments]  ${target_name}
  Run ATPerf CLI Command  target prepare --target ${target_name}

Prepare The Test Target
  [Documentation]  Prepare the test target
  Prepare Target  ${G_TARGET_NAME}

Prepare The Target If Needed
  [Documentation]  Prepare the specified target if it is not already prepared.
  ...  Safe to use in Given/setup flows because it does not unprepare first.
  [Arguments]  ${target_name}
  ${is_prepared} =  Run Keyword And Return Status  The Target Is Prepared
  IF  not ${is_prepared}
    Prepare Target  ${target_name}
    The Last Command Succeeded
    The Target Is Prepared
  END

Prepare The Test Target If Needed
  [Documentation]  Prepare the test target if it is not already prepared.
  ...  Safe to use in Given/setup flows because it does not unprepare first.
  Prepare The Target If Needed  ${G_TARGET_NAME}

The Target Is Prepared
  [Documentation]  Check that the target is prepared, by checking if the
  ...  target agent is deployed. This is a pure state check and does not mutate target state.
  The Target Agent Is Deployed

Freshly Prepare Target
  [Documentation]  Force a clean prepare of the specified target by unpreparing first.
  [Arguments]  ${target_name}
  Unprepare The Target
  Prepare Target  ${target_name}
  The Last Command Succeeded
  The Target Is Prepared

The Target Is Prepared Successfully
  [Documentation]  Prepare the specified target from a clean state and check that the result was successful.
  ...  Prefer `Prepare The Target If Needed` when a fresh install is not required.
  [Arguments]  ${target_name}
  Freshly Prepare Target  ${target_name}

The Test Target Is Prepared Successfully
  [Documentation]  Prepare the test target from a clean state and check that the result was successful.
  ...  Prefer `Prepare The Test Target If Needed` when a fresh install is not required.
  The Target Is Prepared Successfully  ${G_TARGET_NAME}

Unprepare The Target
  [Documentation]  Unprepare the target by killing the collector daemon, killing the agent, and removing the deployment directory
  Kill The Target Agent
  Remove The Target Deployment Directory

The Target Is Unprepared
  [Documentation]  Check that the target is unprepared.
  The Target Agent Is Not Deployed

The Target Is Unprepared Successfully
  [Documentation]  Unprepare the target and check that the result was successful.
  Unprepare The Target
  The Target Is Unprepared

The Target Info Contains
  [Documentation]  Check that the target info for a particular target contains the expected value
  [Arguments]  ${os_field}  ${expected_os}  ${arch_field}  ${expected_arch}
  ${result} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  VAR  ${system_info}  ${result}[data][info][sl-collect-target-info][Info][System]
  ${os_map} =  Create Dictionary  ${OS_LINUX}=${1}  ${OS_DARWIN}=${2}  ${OS_WINDOWS}=${3}
  ${arch_map} =  Create Dictionary  ${ARCH_AARCH64}=${1}  ${ARCH_X86_64}=${2}
  ${os_value} =  Get From Dictionary  ${os_map}  ${expected_os}
  Dictionary Should Contain Item  ${system_info}[os]  ${os_field}  ${os_value}
  ${arch_value} =  Get From Dictionary  ${arch_map}  ${expected_arch}
  Dictionary Should Contain Item  ${system_info}  ${arch_field}  ${arch_value}

Localhost Is Not Supported
  [Documentation]  Confirms that localhost is not supported on the machine Robot is currently running on
  ${arch_is_arm} =  Run Keyword And Return Status  The Architecture Is Arm
  ${arch_is_x86} =  Run Keyword And Return Status  The Architecture Is X86
  ${os_is_linux} =  Run Keyword And Return Status  The Operating System Is Linux
  ${is_unsupported} =  Evaluate  not ((${arch_is_arm} or ${arch_is_x86}) and ${os_is_linux})
  Should Be True  ${is_unsupported}

Skip If Localhost Is Not Supported
  [Documentation]  Skips the test if localhost is not supported on the machine Robot is currently running on
  ${is_unsupported} =  Run Keyword And Return Status  Localhost Is Not Supported
  Skip If  ${is_unsupported}  "Test skipped because localhost is not supported on this machine."

Set Localhost Target Architecture
  [Documentation]  Detect the localhost architecture and store it in the global test target state.
  ${arch_is_arm} =  Run Keyword And Return Status  The Architecture Is Arm
  ${arch_is_x86} =  Run Keyword And Return Status  The Architecture Is X86
  IF  ${arch_is_arm}
    VAR  ${G_TARGET_ARCH}  ${ARCH_AARCH64}  scope=GLOBAL
  ELSE IF  ${arch_is_x86}
    VAR  ${G_TARGET_ARCH}  ${ARCH_X86_64}  scope=GLOBAL
  ELSE
    Fail  Unable to determine host architecture
  END

Set Localhost As Test Target
  [Documentation]  Sets up localhost as the test target
  Skip If Localhost Is Not Supported
  VAR  ${G_TARGET_NAME}  ${LOCALHOST_TARGET_NAME}  scope=GLOBAL
  Set Localhost Target Architecture
  ${host_os} =  Determine Operating System
  VAR  ${G_TARGET_OS}  ${host_os}  scope=GLOBAL
  Set Default Launch Workload
  ${localhost_user} =  Get Localhost User
  Set Test Username  ${localhost_user}
  Configure Test Target Platform

Restore Remote Test Target
  [Documentation]  Restores the default test target
  Determine Target For Test
  Set Default Launch Workload
  Set Test Username
  Configure Test Target Platform

Create Default Dummy Target
  [Documentation]  Creates a dummy target and sets it as Default
  [Arguments]  ${name}
  Run ATPerf CLI Command  target add 111.111.111.111 --name ${name} --default
