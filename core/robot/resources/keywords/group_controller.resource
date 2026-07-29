# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation  A resource file containing process group controller keywords for the Performix CLI.

Library        ../libs/AgentHelper.py

Resource       agent.resource
Resource       common.resource
Resource       target.resource


*** Keywords ***
Group Controller Suite Setup
  [Documentation]  Prepares the test target for group controller suite tests.
  Common Setup
  Skip Unless Target OS Is  ${OS_LINUX}
  Clear Test Target Output Directories
  Ensure Target Does Not Exist  ${G_TARGET_NAME}
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  The Test Target Is Prepared Successfully
  Populate Group Controller Test Target Global Variables

Group Controller Suite Teardown
  [Documentation]  Cleans up after the group controller suite tests.
  Skip Unless Target OS Is  ${OS_LINUX}
  The Test Target Is Removed Successfully
  Clear Test Target Output Directories
  Common Teardown

Skip If Cgroupv2 Not Available
  [Documentation]  Skip the test if cgroupv2 is not available on the target.
  ${cgroupv2} =  Group Controller Cgroupv2 Available
  IF  '${cgroupv2}' == '${False}'
    Skip  Cgroupv2 is not available on this system.
  END

Populate Group Controller Test Target Global Variables
  [Documentation]  Populates global variables required for the group controller suite tests.
  Should Not Be Empty  ${AGENT_BINARY_FILE_NAME}  msg=AGENT_BINARY_FILE_NAME must be set for group controller tests.
  VAR  ${G_TARGET_AGENT_GROUP_CONTROLLER_CMD} =  ${AGENT_BINARY_FILE_NAME} start-group-controller  scope=GLOBAL

Group Controller Process Count Is
  [Documentation]  Verifies that the target agent group controller has the expected number of processes running.
  [Arguments]  ${expected_count}
  ${actual_count} =  Group Controller Get Process Count
  Should Be Equal As Integers  ${actual_count}  ${expected_count}

Group Controller CGroupV2 Process Count Is
  [Documentation]  Verifies that the target agent group controller has the expected number of processes running in
  ...  cgroup v2 and stores the list of PIDs in a test variable for access later.
  [Arguments]  ${expected_count}
  ${pids} =  Group Controller Cgroupv2 Process List
  Length Should Be  ${pids}  ${expected_count}
  VAR  ${GROUP_CONTROLLER_PIDS} =  ${pids}  scope=TEST

Group Controller PIDs Are Not Alive
  [Documentation]  Verifies that the target agent group controller has no alive processes running in cgroup v2.
  PIDs Are Not Alive On Target  @{GROUP_CONTROLLER_PIDS}

PIDs Are Not Alive On Target
  [Documentation]  Verifies that the given PIDs are not alive on the target.
  [Arguments]  @{pids}
  FOR  ${pid}  IN  @{pids}
    Run Target Command  sudo -n test ! -d /proc/${pid}
    The Last Command Succeeded
  END

Group Controller Is Started With Command And Exits With Code
  [Documentation]  Starts the group controller with a command and waits for it to exit with the specified code.
  [Arguments]  ${code}
  ${exit_code} =  Start Group Controller With Command And Wait For Exit  bash -c 'sleep 1; exit ${code}'
  VAR  ${GROUP_CONTROLLER_EXIT_CODE} =  ${exit_code}  scope=TEST

Group Controller Exit Code Is
  [Arguments]  ${expected_exit_code}
  Should Be Equal As Integers  ${GROUP_CONTROLLER_EXIT_CODE}  ${expected_exit_code}

The Group Controller Is Running
  ${running} =  Group Controller Is Running
  Should Be True  ${running}

The Group Controller Is Not Running
  ${running} =  Group Controller Is Running
  Should Not Be True  ${running}
