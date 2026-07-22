# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A test suite to exercise the target Agent's Group Controller functionality.

Resource            ../../resources/keywords/agent.resource

Suite Setup         Group Controller Suite Setup
Suite Teardown      Group Controller Suite Teardown

Test Tags           agent  group-controller


*** Test Cases ***
The Target Agent Group Controller Dies When Stdin Is EOF
  [Documentation]  Verifies that the target agent group controller dies when its stdin is closed (EOF).
  [Tags]  eof
  Start Group Controller With Command  sleep 90
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${True}
  Stop Group Controller  timeout=10
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${False}

The Target Agent Group Controller Captures Stdout
  [Documentation]  Verifies that the target agent group controller can capture stdout output from commands run on the target.
  [Tags]  stdout
  Start Group Controller With Command  echo hello-world
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${True}
  Stop Group Controller  timeout=10
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${False}
  Group Controller Stdout Should Contain  hello-world

The Target Agent Group Controller Captures Stderr
  [Documentation]  Verifies that the target agent group controller can capture stderr output from commands run on the target.
  [Tags]  stderr
  Start Group Controller With Command  logger -s hello-world-stderr
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${True}
  Stop Group Controller  timeout=10
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${False}
  Group Controller Stderr Should Contain  hello-world-stderr

The Target Agent Group Controller Kills The Whole Tree
  [Documentation]  Verifies that the target agent group controller kills the whole process tree when it is stopped.
  [Tags]  tree
  Start Group Controller With Command  bash -lc 'for i in {1..3};do(for j in {1..3};do sleep 99& done)& done'
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${True}
  ${count} =  Group Controller Get Process Count
  Should Be Equal  ${count}  ${9}
  Stop Group Controller  timeout=10
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${False}
  ${count} =  Group Controller Get Process Count
  Should Be Equal  ${count}  ${0}

The Target Agent Group Controller Kills Double Fork Daemons
  [Documentation]  Verifies that the target agent group controller kills double-forked daemon processes when it is stopped.
  [Tags]  double-fork
  Skip If Cgroupv2 Not Available

  Start Group Controller With Command  bash -lc '(setsid sleep 60 & disown) &'  sudo=True
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${True}

  ${pids} =  Group Controller Cgroupv2 Process List
  Length Should Be  ${pids}  1
  Stop Group Controller  timeout=10
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${False}
  PIDs Are Unalive On Target  @{pids}

The Target Agent Group Controller Waits For Child
  [Documentation]  Verifies that the target agent group controller returns the child process exit code when the wait-for-child flag is used
  [Tags]  wait-for-child
  ${exit_code} =  Start Group Controller With Command And Wait For Exit  bash -c 'sleep 1; exit 123'
  Should Be Equal As Integers  ${exit_code}  ${123}
  ${result} =  Group Controller Running
  Should Be Equal  ${result}  ${False}


*** Keywords ***
Skip Suite If Target Agent Not Deployed
  [Documentation]  (DEPRECATED) Skip whole suite if target agent is not deployed.
  ${deployed} =  Run Keyword And Return Status  The Target Agent Is Deployed
  IF  not ${deployed}  Skip  Skipping suite as target agent is not deployed.

Skip If Cgroupv2 Not Available
  [Documentation]  Skip the test if cgroupv2 is not available on the target.
  ${cgroupv2} =  Group Controller Cgroupv2 Available
  IF  '${cgroupv2}' == '${False}'
    Skip  Cgroupv2 is not available on this system.
  END

PIDs Are Unalive On Target
  [Documentation]  Verifies that the given PIDs are not alive on the target.
  [Arguments]  @{pids}
  FOR  ${pid}  IN  @{pids}
    Run Target Command  kill -0 ${pid}
    The Last Command Return Code Is Not  0
  END
