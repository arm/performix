# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'daemon' CLI of Arm Total Performance.
Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/daemon.resource
Suite Setup     Daemon Suite Setup
Suite Teardown  Daemon Suite Teardown
Test Tags       daemon  disabled


*** Test Cases ***
### Start ###
The Daemon Can Be Started Manually
  [Documentation]  Tests that the daemon can be started manually.
  [Tags]  start
  [Setup]  Stop Daemon
  Given The Daemon Is Not Running
  When Start Daemon
  Then The Last Command Succeeded
  And The Daemon Is Running
  [Teardown]  Stop Daemon

The Daemon Is Automatically Started When The ATPerf CLI Launches
  [Documentation]  Test that the daemon is automatically started if not already running when an
  ...  ATPerf CLI command is run.
  [Tags]  start
  [Setup]  Stop Daemon
  Given The Daemon Is Not Running
  When Run Arbitrary ATPerf CLI Command
  Then The Daemon Is Running
  [Teardown]  Stop Daemon

The Daemon Is Not Started Twice If Already Running
  [Documentation]  Test that the daemon is not started for a second time if it's already running.
  [Tags]  start
  [Setup]  Run Keywords  Stop Daemon
  ...  AND  Start Daemon
  Given Only One Instance Of The Daemon Is Running
  When Run Arbitrary ATPerf CLI Command
  Then Only One Instance Of The Daemon Is Running
  [Teardown]  Stop Daemon

### Stop ###

The Daemon Can Be Stopped Manually
  [Documentation]  Tests that the daemon can be stopped manually.
  [Tags]  stop
  [Setup]  Run Keywords  Stop Daemon
  ...  AND  Start Daemon
  Given The Daemon Is Running
  When Stop Daemon
  Then The Last Command Succeeded
  And The Daemon Is Not Running
  [Teardown]  Stop Daemon


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.

Run Arbitrary ATPerf CLI Command
  [Documentation]  This keyword just runs any old ATPerf CLI command to cause the
  ...  daemon to launch.
  Display ATPerf CLI Version

Daemon Suite Setup
  Common Setup
  Check Process List For Daemon
  Run Host Command  ps aux | grep atperf
  Log  ${G_LAST_RESULT.stdout}
  Stop Daemon
  The Daemon Is Not Running

Daemon Suite Teardown
  Stop Daemon
  The Daemon Is Not Running
  Common Teardown
