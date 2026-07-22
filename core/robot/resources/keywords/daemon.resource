# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation  A resource file containing daemon keywords for the Arm Performance Analysis Platform CLI.
Library        OperatingSystem
Library        String
Resource       common.resource


*** Keywords ***
Start Daemon
  [Arguments]  ${args}=${EMPTY}
  Run ATPerf CLI Command  daemon start ${args}

Stop Daemon
  [Documentation]  Stops the daemon process if it's running. This also causes the target agent to be stopped.
  [Arguments]  ${args}=${EMPTY}
  Run ATPerf CLI Command  daemon stop ${args}

Kill Daemon
  Log  Keyword not finished yet, doing nothing for now
  # Get Daemon Process ID
  # Terminate Process  ${pid}  kill=True

Get Daemon Process ID
  [Documentation]  Checks whether a process is running for the apap-cli daemon. Only expects a
  ...  maximum of one PID to be reported in the output.
  # TODO - this keyword is untested
  ${os} =  Determine Operating System
  IF  '${os}' == 'Linux'
    VAR  ${cmd}  TODO  scope=LOCAL
  ELSE IF  '${os}' == 'Darwin'
    VAR  ${cmd}  TODO  scope=LOCAL
  ELSE IF  '${os}' == 'Windows'
    VAR  ${cmd}  TODO  scope=LOCAL
  END
  Run Host Command  ${cmd}
  The Last Command Succeeded
  ${num_pids} =  Get Line Count  ${G_LAST_RESULT.stdout}
  Should Be Equal As Integers  ${num_pids}  1
  ${pid} =  Get Variable Value  ${G_LAST_RESULT.stdout}
  RETURN  ${pid}

Check Process List For Daemon
  [Documentation]  Lists running processes on the host and checks whether the daemon is present.
  ${os} =  Determine Operating System
  IF  '${os}' == 'Linux'
    VAR  ${cmd}  pgrep -f "${PRODUCT_BINARY_NAME} daemon start"  scope=LOCAL
  ELSE IF  '${os}' == 'Darwin'
    VAR  ${cmd}  pgrep -f "${PRODUCT_BINARY_NAME} daemon start"  scope=LOCAL
  ELSE IF  '${os}' == 'Windows'
    VAR  ${cmd}  "tasklist /fi "Imagename eq ${PRODUCT_BINARY_NAME}.exe"  scope=LOCAL
  END
  Run Host Command  ${cmd}

The Daemon Is Running
  [Documentation]  Confirms there is a process running for the apap-cli daemon.
  Check Process List For Daemon
  Wait Until Keyword Succeeds  30 sec  5 sec  The Last Command Succeeded
  Should Not Be Empty  ${G_LAST_RESULT.stdout}

The Daemon Is Not Running
  [Documentation]  Confirms there isn't a process running for the apap-cli daemon.
  Check Process List For Daemon
  Wait Until Keyword Succeeds  30 sec  5 sec  The Last Command Failed

Only One Instance Of The Daemon Is Running
  [Documentation]  Confirms that precisely one instance of the daemon is running.
  Check Process List For Daemon
  Wait Until Keyword Succeeds  30 sec  5 sec  The Last Command Succeeded
  ${num_daemons} =  Evaluate  ${G_LAST_RESULT.stdout}.count('\\n')
  Should Be Equal As Integers  ${num_daemons}  1
