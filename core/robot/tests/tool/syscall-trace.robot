# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A test suite to verify the functionality of the 'syscall-trace' tool integration.

Resource            ../../resources/keywords/common.resource
Resource            ../../resources/keywords/recipe.resource
Resource            ../../resources/keywords/run.resource
Resource            ../../resources/keywords/target.resource

Suite Setup         Syscall Trace Suite Setup
Suite Teardown      Syscall Trace Suite Teardown

Test Tags           syscall-trace


*** Test Cases ***
The Syscall Trace Summary Recipe Is Not Ready For System-Wide Workloads
  [Documentation]  Tests that the syscall-trace tool integration reports as not ready
  ...  when the recipe is configured with a system-wide workload.
  [Setup]  Skip Unless Target OS Is  ${OS_LINUX}
  Given The Syscall Trace Summary Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  When Check Recipe Is Ready  syscall_trace_summary  --system-wide --target ${G_TARGET_NAME}
  Then The Last Command Succeeded
  And The Recipe Is Not Ready
  And Check Advice Messages Contain  "Syscall Trace Summary supports launch and attach workloads. System-wide tracing is not supported."

The Syscall Trace Summary Recipe Fails When Workload Type Is System-Wide
  [Documentation]  Tests that the syscall-trace tool integration returns the expected error
  ...  when the recipe is run with a system-wide workload.
  [Setup]  Skip Unless Target OS Is  ${OS_LINUX}
  Given The Syscall Trace Summary Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  When Run Syscall Trace Summary Recipe  --system-wide --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed With Message Code  tool_integrations.syscall_trace.UNSUPPORTED_WORKLOAD
  And The Target Output Directory Is Empty

The Syscall Trace Tool Passes Environment Variables To The Workload Script
  [Documentation]  Tests that the syscall-trace tool integration passes environment variables
  ...  provided by the user through to the workload script.
  [Setup]  Skip Unless Syscall Trace Can Run On Target
  Given The Syscall Trace Summary Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Script Is Created On The Target  ${TEMP_FILE_PATH}  echo "this is \$FOO"
  When Run Syscall Trace Summary Recipe  --workload ${TEMP_FILE_PATH} --env FOO=bar --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Syscall Trace Parquet Artifact Exists
  And The Syscall Trace Strace Stdout Contains  this is bar
  And The Target Output Directory Is Empty
  [Teardown]  The File Is Removed From The Target  ${TEMP_FILE_PATH}

The Syscall Trace Tool Uses The Specified Working Dir To Launch The Workload
  [Documentation]  Tests that the syscall-trace tool integration uses the working directory
  ...  specified by the user to launch the workload.
  [Setup]  Skip Unless Syscall Trace Can Run On Target
  Given The Syscall Trace Summary Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Script Is Created On The Target  ${TEMP_FILE_PATH}  pwd
  When Run Syscall Trace Summary Recipe  --workload ./${TEMP_FILE_NAME} --working-dir ${ATPERF_DIR} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Syscall Trace Parquet Artifact Exists
  And The Syscall Trace Strace Stdout Contains  ${ATPERF_DIR}
  And The Target Output Directory Is Empty
  [Teardown]  The File Is Removed From The Target  ${TEMP_FILE_PATH}


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.

Syscall Trace Suite Setup
  Common Setup
  Set Enable Experimental Recipes Env Var
  Set Suite Variables
  The Test Target Is Added Successfully
  The Test Target Is Prepared Successfully

Syscall Trace Suite Teardown
  The Test Target Is Removed Successfully
  Clear Enable Experimental Recipes Env Var
  Common Teardown

Set Suite Variables
  VAR  ${TEMP_FILE_PATH}  ${ATPERF_DIR}/${TEMP_FILE_NAME}  scope=SUITE

Skip Unless Syscall Trace Can Run On Target
  [Documentation]  Skips tests that require a Linux target with strace installed.
  Skip Unless Target OS Is  ${OS_LINUX}
  Run Target Command  command -v strace >/dev/null 2>&1
  Skip If  ${G_LAST_RESULT.rc} != 0  Test skipped. syscall-trace requires strace on the target.

The Syscall Trace Parquet Artifact Exists
  [Documentation]  Verifies that the syscall-trace parquet output was saved in the run.
  ${run_id} =  Extract The Run ID
  File Should Exist  ${G_RUNS_DIR}${/}${run_id}${/}tool${/}syscall-trace${/}0${/}syscalls.parquet

The Syscall Trace Strace Stdout Contains
  [Documentation]  Checks that the strace stdout artifact contains the expected text.
  [Arguments]  ${expected}
  ${run_id} =  Extract The Run ID
  VAR  ${stdout_path}  ${G_RUNS_DIR}${/}${run_id}${/}tool${/}syscall-trace${/}0${/}syscall_trace_strace_stdout.txt
  ${stdout_text} =  Get File  ${stdout_path}
  Should Contain  ${stdout_text}  ${expected}
