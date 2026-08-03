# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A test suite to exercise the target Agent's Group Controller functionality.

Resource            ../../resources/keywords/agent.resource
Resource            ../../resources/keywords/group_controller.resource

Suite Setup         Group Controller Suite Setup
Suite Teardown      Group Controller Suite Teardown

Test Tags           agent  group-controller


*** Test Cases ***
The Target Agent Group Controller Dies When Stdin Is Closed
  [Documentation]  Verifies that the target agent group controller dies when its stdin is closed (EOF).
  [Tags]  eof
  [Setup]  Run Keywords  The Group Controller Is Not Running
  ...  AND  Start Group Controller With Command  sleep 90
  Given The Group Controller Is Running
  When Stop Group Controller  timeout=10
  Then The Group Controller Is Not Running

The Target Agent Group Controller Captures Stdout
  [Documentation]  Verifies that the target agent group controller can capture stdout output from commands run on the target.
  [Tags]  stdout
  [Setup]  Run Keywords  The Group Controller Is Not Running
  ...  AND  Start Group Controller With Command  echo hello-world
  Given The Group Controller Is Running
  When Stop Group Controller  timeout=10
  Then The Group Controller Is Not Running
  And Group Controller Stdout Should Contain  hello-world

The Target Agent Group Controller Captures Stderr
  [Documentation]  Verifies that the target agent group controller can capture stderr output from commands run on the target.
  [Tags]  stderr
  [Setup]  Run Keywords  The Group Controller Is Not Running
  ...  AND  Start Group Controller With Command  logger -s hello-world-stderr
  Given The Group Controller Is Running
  When Stop Group Controller  timeout=10
  Then The Group Controller Is Not Running
  And Group Controller Stderr Should Contain  hello-world-stderr

The Target Agent Group Controller Kills The Whole Tree
  [Documentation]  Verifies that the target agent group controller kills the whole process tree when it is stopped.
  [Tags]  tree
  [Setup]  Run Keywords  The Group Controller Is Not Running
  ...  AND  Start Group Controller With Command  bash -lc 'for i in {1..3};do(for j in {1..3};do sleep 99& done)& done'
  Given The Group Controller Is Running
  And Group Controller Process Count Is  9
  When Stop Group Controller  timeout=10
  Then The Group Controller Is Not Running
  And Group Controller Process Count Is  0

The Target Agent Group Controller Kills Double Fork Daemons
  [Documentation]  Verifies that the target agent group controller kills double-forked daemon processes when it is stopped.
  [Tags]  double-fork
  [Setup]  Run Keywords  Skip If Cgroupv2 Not Available
  ...  AND  The Group Controller Is Not Running
  ...  AND  Start Group Controller With Command  bash -lc '(setsid sleep 60 & disown) &'  sudo=True
  Given The Group Controller Is Running
  And Group Controller CGroupV2 Process Count Is  1
  When Stop Group Controller  timeout=10
  Then The Group Controller Is Not Running
  And Group Controller PIDs Are Not Alive

The Target Agent Group Controller Waits For Child
  [Documentation]  Verifies that the target agent group controller returns the child process exit code when the
  ...  wait-for-child flag is used.
  [Tags]  wait-for-child
  Given The Group Controller Is Not Running
  When Group Controller Is Started With Command And Exits With Code  123
  Then The Group Controller Is Not Running
  And Group Controller Exit Code Is  123
