# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A test suite to verify the privilege escalation functionality of the 'neoprof' tool integration.

Resource            ../../resources/keywords/agent.resource
Resource            ../../resources/keywords/common.resource
Resource            ../../resources/keywords/recipe.resource
Resource            ../../resources/keywords/target.resource
Resource            ../../resources/keywords/run.resource
Resource            ../../resources/keywords/remote_localhost.resource

Suite Setup         Neoprof-Privilege Suite Setup
Suite Teardown      Neoprof-Privilege Suite Teardown

Test Tags           neoprof-privilege


*** Test Cases ***
The Neoprof Tool Requires Elevated Privileges On System-Wide Workloads
  [Documentation]  Tests the neoprof tool integration wants elevated privileges
  ...  when performing system-wide workloads
  [Setup]  The Test Target With Non-Root User Is Prepared Successfully
  Given The Code Hotspots Recipe Is Listed
  When Run Code Hotspots Recipe  --system-wide --target ${G_TARGET_NAME_NON_ROOT} ${DEPLOY_TOOLS_FLAG} --timeout 1
  Then Neoprof Requires Root

The Neoprof Tool Requires Elevated Privileges On Attach Workloads
  [Documentation]  Tests the neoprof tool integration wants elevated privileges
  ...  when performing attach workloads
  Given The Code Hotspots Recipe Is Listed
  When Run Code Hotspots Recipe  --pid 1 --target ${G_TARGET_NAME_NON_ROOT} ${DEPLOY_TOOLS_FLAG} --timeout 1
  Then Neoprof Requires Root

The Neoprof Tool Does Not Require Elevated Privileges If CAP_SYS_ADMIN Is Present
  [Documentation]  Tests the neoprof tool integration does not want elevated privileges
  ...  when CAP_SYS_ADMIN is present on the target process
  [Setup]  The Target Agent Stopped Successfully
  Given The Target Agent Process Has Capability  cap_sys_admin=ep  ${G_TARGET_USER_NON_ROOT}
  And Target Platform Perf Event Paranoid Should Be  1
  And The Code Hotspots Recipe Is Listed
  When Run Code Hotspots Recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME_NON_ROOT} ${DEPLOY_TOOLS_FLAG}
  And The Last Command Succeeded
  Then Neoprof Does Not Require Root
  [Teardown]  Run Keywords  The Target Agent Process Capabilities Are Cleared  ${G_TARGET_USER_NON_ROOT}
  ...  AND  The Target Agent Stopped Successfully

The Neoprof Tool Does Not Require Elevated Privileges If CAP_PERFMON Is Present
  [Documentation]  Tests the neoprof tool integration wants elevated privileges
  ...  when CAP_PERFMON is present on the target process
  [Setup]  The Target Agent Stopped Successfully
  Given The Target Agent Process Has Capability  cap_perfmon=ep  ${G_TARGET_USER_NON_ROOT}
  And Target Platform Perf Event Paranoid Should Be  1
  And The Code Hotspots Recipe Is Listed
  When Run Code Hotspots Recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME_NON_ROOT} ${DEPLOY_TOOLS_FLAG}
  And The Last Command Succeeded
  Then Neoprof Does Not Require Root
  [Teardown]  Run Keywords  The Target Agent Process Capabilities Are Cleared  ${G_TARGET_USER_NON_ROOT}
  ...  AND  The Target Agent Stopped Successfully

The Neoprof Tool Does Not Require Elevated Privileges If Perf Event Paranoid Is Less Than 1
  [Documentation]  Tests the neoprof tool integration does not want elevated privileges
  ...  when perf_event_paranoid is less than 1 on the target
  Given The Code Hotspots Recipe Is Listed
  And Target Platform Perf Event Paranoid Should Be  0
  When Run Code Hotspots Recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME_NON_ROOT} ${DEPLOY_TOOLS_FLAG}
  And The Last Command Succeeded
  Then Neoprof Does Not Require Root
  [Teardown]  Target Platform Perf Event Paranoid Should Be  1

The Neoprof Tool Requires Elevated Privileges If Perf Event Paranoid Is Greater Than Or Equal To 1
  [Documentation]  Tests the neoprof tool integration wants elevated privileges
  ...  when perf_event_paranoid is 1 or higher on the target
  Given The Code Hotspots Recipe Is Listed
  And Target Platform Perf Event Paranoid Should Be  1
  When Run Code Hotspots Recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME_NON_ROOT} ${DEPLOY_TOOLS_FLAG}
  And The Last Command Succeeded
  Then Neoprof Requires Root
  [Teardown]  Target Platform Perf Event Paranoid Should Be  1

The CPU Microarchitecture Recipe Runs On Localhost with A Privileged User, Launch New Process and Perf Event Paranoid Is -1
  [Documentation]  Tests that the CPU Microarchitecture recipe runs successfully on localhost as a privileged user,
  ...  launching a new process workload when perf_event_paranoid is set to -1.
  [Tags]  cpu-microarchitecture  remote-localhost
  [Setup]  Skip If Remote Localhost Is Not Set Up
  Given The Remote Localhost CPU Microarchitecture Recipe Is Listed
  And Target Platform Perf Event Paranoid Should Be  -1
  When Run Remote Localhost CPU Microarchitecture Recipe  --workload ${LAUNCH_WORKLOAD} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  [Teardown]  Run Keywords  Clean Up Remote Localhost  AND  Target Platform Perf Event Paranoid Should Be  -1

The CPU Microarchitecture Recipe Runs On Localhost with A Privileged User, Systemwide Profiling and Perf Event Paranoid Is -1
  [Documentation]  Tests that the CPU Microarchitecture recipe runs successfully on localhost as a privileged user,
  ...  with systemwide profiling when perf_event_paranoid is set to -1.
  [Tags]  cpu-microarchitecture  remote-localhost
  [Setup]  Skip If Remote Localhost Is Not Set Up
  Given The Remote Localhost CPU Microarchitecture Recipe Is Listed
  And Target Platform Perf Event Paranoid Should Be  -1
  When Run Remote Localhost CPU Microarchitecture Recipe  --system-wide ${DEPLOY_TOOLS_FLAG} --timeout 2
  Then The Last Command Succeeded
  [Teardown]  Run Keywords  Clean Up Remote Localhost  AND  Target Platform Perf Event Paranoid Should Be  -1

The CPU Microarchitecture Recipe Runs On Localhost with A Non-Privileged User, Launch New Process and Perf Event Paranoid Is -1
  [Documentation]  Tests that the CPU Microarchitecture recipe runs successfully on localhost as a non-privileged user,
  ...  launching a new process workload when perf_event_paranoid is set to -1.
  [Tags]  cpu-microarchitecture  remote-localhost
  [Setup]  Skip If Remote Localhost Is Not Set Up
  Given The Remote Localhost CPU Microarchitecture Recipe Is Listed As Non-Root User
  And Target Platform Perf Event Paranoid Should Be  -1
  When Run Remote Localhost CPU Microarchitecture Recipe As Non-Root User
  ...  --workload ${LAUNCH_WORKLOAD} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  [Teardown]  Run Keywords  Clean Up Remote Localhost  ${G_TARGET_USER_NON_ROOT}  AND  Target Platform Perf Event Paranoid Should Be  -1

The CPU Microarchitecture Recipe Runs On Localhost with A Non-Privileged User, Launch New Process and Perf Event Paranoid Is 0
  [Documentation]  Tests that the CPU Microarchitecture recipe runs successfully on localhost as a non-privileged user,
  ...  launching a new process workload when perf_event_paranoid is set to 0.
  [Tags]  cpu-microarchitecture  remote-localhost
  [Setup]  Skip If Remote Localhost Is Not Set Up
  Given The Remote Localhost CPU Microarchitecture Recipe Is Listed As Non-Root User
  And Target Platform Perf Event Paranoid Should Be  0
  When Run Remote Localhost CPU Microarchitecture Recipe As Non-Root User
  ...  --workload ${LAUNCH_WORKLOAD} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  [Teardown]  Run Keywords  Clean Up Remote Localhost  ${G_TARGET_USER_NON_ROOT}  AND  Target Platform Perf Event Paranoid Should Be  -1


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.

Neoprof-Privilege Suite Setup
  Common Setup
  The Test Target Is Added Successfully
  The Test Target Is Prepared Successfully
  Configure Non-Root User On Target
  The Test Target With Non-Root User Is Added Successfully

Neoprof-Privilege Suite Teardown
  Remove Non-Root User From Target
  The Test Target With Non-Root User Is Removed Successfully
  The Test Target Is Removed Successfully
  Common Teardown

Neoprof Requires Root
  [Documentation]  Verifies the run log reports that neoprof needs root privileges.
  Verify Neoprof Privilege Requirement  ${True}

Neoprof Does Not Require Root
  [Documentation]  Verifies the run log reports that neoprof does not need root privileges.
  Verify Neoprof Privilege Requirement  ${False}

Verify Neoprof Privilege Requirement
  [Documentation]  Helper keyword to check neoprof privilege requirement in run log.
  [Arguments]  ${expected}
  ${run_id} =  Extract The Run ID
  VAR  ${log_path} =  ${G_RUNS_DIR}${/}${run_id}${/}log.json
  ${log_text} =  Get File  ${log_path}
  ${expected_value} =  Evaluate  str(${expected}).lower()
  Should Contain  ${log_text}  Neoprof privilege requirement: ${expected_value}
