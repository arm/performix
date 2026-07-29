# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       Resource file containing keywords for running the profiler as localhost
...                 on a remote target. The CLI binary and tool bundles are deployed to the
...                 remote target by run-robot.py before the test suite is invoked.

Resource            common.resource
Resource            target.resource
Library             OperatingSystem


*** Variables ***
${REMOTE_LOCALHOST_DIR}     /tmp/apx-remote-localhost/repo/core/apap-cli


*** Keywords ***
Run Remote ATPerf CLI Command
  [Documentation]  Invokes the apx CLI on the remote target. The command output is captured
  ...  in G_LAST_RESULT via Run Target Command, so all standard assertion keywords work.
  [Arguments]  ${cmd_string}
  ${cmd} =  Catenate  SEPARATOR=${SPACE}
  ...  ${REMOTE_LOCALHOST_DIR}/apx ${cmd_string} ${JSON_FLAG}
  Run Target Command  "${cmd}"

Run Remote ATPerf CLI Command As User
  [Documentation]  Invokes the apx CLI on the remote target as the specified user via sudo -u.
  ...  Otherwise identical to Run Remote ATPerf CLI Command.
  [Arguments]  ${cmd_string}  ${user}
  ${cmd} =  Catenate  SEPARATOR=${SPACE}
  ...  sudo -u ${user}
  ...  ${REMOTE_LOCALHOST_DIR}/apx ${cmd_string} ${JSON_FLAG}
  Run Target Command  "${cmd}"

Clean Up Remote Localhost
  [Documentation]  Stops the remote localhost daemon (if running) and removes per-test runtime
  ...  artifacts (config, log) from the remote target. The apx binary and repo checkout
  ...  are left in place so that subsequent tests can reuse them.
  ...  When ${user} is provided, the daemon is stopped as that user (required when a
  ...  non-privileged user started the daemon).
  [Arguments]  ${user}=${EMPTY}
  IF  '${user}' != '${EMPTY}'
    Run Remote ATPerf CLI Command As User  daemon stop  ${user}
    Run Target Command  "sudo -u ${user} rm -rf ${REMOTE_LOCALHOST_DIR}/config ${REMOTE_LOCALHOST_DIR}/log"
  ELSE
    Run Remote ATPerf CLI Command  daemon stop
    Run Target Command  "rm -rf ${REMOTE_LOCALHOST_DIR}/config ${REMOTE_LOCALHOST_DIR}/log"
  END

The Remote Localhost CPU Microarchitecture Recipe Is Listed
  [Documentation]  Verifies the cpu_microarchitecture recipe is available on the remote localhost CLI.
  Run Remote ATPerf CLI Command  recipe list
  ${dict} =  Parse Last JSON Stdout Line To Dictionary
  ${data} =  Get From Dictionary  ${dict}  data
  ${recipes} =  Get From Dictionary  ${data}  recipes
  ${val} =  Create Dictionary  name=cpu_microarchitecture  path=${None}  load_error=${None}
  List Should Contain Value  ${recipes}  ${val}

The Remote Localhost CPU Microarchitecture Recipe Is Listed As Non-Root User
  [Documentation]  Verifies the cpu_microarchitecture recipe is available on the remote localhost CLI.
  ...  Runs recipe list command as non-root user
  Run Remote ATPerf CLI Command As User  recipe list  ${G_TARGET_USER_NON_ROOT}
  The Last Command Succeeded
  ${dict} =  Parse Last JSON Stdout Line To Dictionary
  ${data} =  Get From Dictionary  ${dict}  data
  ${recipes} =  Get From Dictionary  ${data}  recipes
  ${val} =  Create Dictionary  name=cpu_microarchitecture  path=${None}  load_error=${None}
  List Should Contain Value  ${recipes}  ${val}

Run Remote Localhost CPU Microarchitecture Recipe
  [Documentation]  Runs the cpu_microarchitecture recipe via the CLI deployed on the remote target,
  ...  profiling localhost. The --target localhost flag is added automatically.
  [Arguments]  ${args}
  Run Remote ATPerf CLI Command  recipe run cpu_microarchitecture --target localhost ${args}

Run Remote Localhost CPU Microarchitecture Recipe As Non-Root User
  [Documentation]  Runs the cpu_microarchitecture recipe via the CLI deployed on the remote target
  ...  as the non-root user, profiling localhost.
  [Arguments]  ${args}
  Run Remote ATPerf CLI Command As User
  ...  recipe run cpu_microarchitecture --target localhost ${args}
  ...  ${G_TARGET_USER_NON_ROOT}

Skip If Remote Localhost Is Not Set Up
  [Documentation]  Skips the current test if the remote-localhost apx binary is not present on the
  ...  target. This indicates that run-robot.py's remote-localhost setup has not been run.
  ...  Use this keyword as a [Setup] step in remote_localhost-tagged tests so that they are
  ...  gracefully skipped when invoking robot directly without prior setup.
  Run Target Command  "test -x ${REMOTE_LOCALHOST_DIR}/apx"
  ${is_set_up} =  Run Keyword And Return Status  The Last Command Succeeded
  ${msg} =  Catenate
  ...  Remote localhost not set up (${REMOTE_LOCALHOST_DIR}/apx not found).
  ...  Use run-robot.py with flag --run-remote-localhost to set up remote localhost and run this test.
  Skip If  not ${is_set_up}  msg=${msg}

Run Remote Localhost Code Hotspots Recipe
  [Documentation]  Runs the code_hotspots recipe via the CLI deployed on the remote target,
  ...  profiling localhost. The --target localhost flag is added automatically.
  [Arguments]  ${args}
  Run Remote ATPerf CLI Command  recipe run code_hotspots --target localhost ${args}

The Remote Localhost Instruction Mix Recipe Is Listed
  [Documentation]  Verifies the instruction_mix recipe is available on the remote localhost CLI.
  Run Remote ATPerf CLI Command  recipe list
  ${dict} =  Parse Last JSON Stdout Line To Dictionary
  ${data} =  Get From Dictionary  ${dict}  data
  ${recipes} =  Get From Dictionary  ${data}  recipes
  ${val} =  Create Dictionary  name=instruction_mix  path=${None}  load_error=${None}
  List Should Contain Value  ${recipes}  ${val}

Run Remote Localhost Instruction Mix Recipe
  [Documentation]  Runs the instruction_mix recipe via the CLI deployed on the remote target,
  ...  profiling localhost. The --target localhost flag is added automatically.
  [Arguments]  ${args}
  Run Remote ATPerf CLI Command  recipe run instruction_mix --target localhost ${args}
