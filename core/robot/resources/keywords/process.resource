# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A resource file containing process-related keywords for the Arm Total Performance CLI.

Resource            common.resource
Resource            target.resource


*** Keywords ***
Start Process On Target And Capture PID
  [Documentation]  Start a process on the target in the background and capture its PID.
  [Arguments]  ${command}  ${process_regex}
  Run Target Command In Background  ${command}
  The Last Command Succeeded
  ${pid} =  Convert To String  ${G_LAST_RESULT.stdout.strip()}
  Should Match Regexp  ${pid}  ^[0-9]+$
  Wait For Matching Process On Target  ${process_regex}  40
  RETURN  ${pid}

Stop Process On Target
  [Documentation]  Stop a process started for attach tests.
  [Arguments]  ${pid}
  IF  "${pid}" != "${EMPTY}"  Run Target Command  "sudo kill -9 ${pid}"

Wait For Matching Process On Target
  [Documentation]  Continually checks for the existence of a process whose command matches
  ...  the provided regex, and returns once one does. Exits if the max iterations are reached
  [Arguments]  ${regex}  ${iters}
  FOR  ${_}  IN RANGE  ${iters}
    Run Target Command  "pgrep -f '${regex}'"
    IF  not ${G_LAST_RESULT.rc}  RETURN
    Sleep  100ms
  END
  Fail  Process matching pattern '${regex}' not found within ${iters} iterations
