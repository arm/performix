# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise .NET stack collection in Arm Total Performance.
Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/target.resource
Resource        ../../resources/keywords/render.resource
Suite Setup     Dotnet Recipe Suite Setup
Suite Teardown  Dotnet Recipe Suite Teardown
Test Tags       dotnet_workload  dotnet


*** Variables ***
${DOTNET_SYMBOL_QUERY}  "select * from symbols where name like '%Hanoi_Solve%'"
${DOTNET_WORKLOAD_NAME}  netbench
${DOTNET_WORKLOAD_PATH}  ${EMPTY}
${DOTNET_WORKLOAD_CMD}   ${EMPTY}
${DOTNET_WORKLOAD_ARGS}  12345 --scenario hanoi --marker-every 2048

${CPU_MICROARCH_DOTNET_ENABLED_RUN_ID}   ${EMPTY}
${CPU_MICROARCH_DOTNET_DISABLED_RUN_ID}  ${EMPTY}
${HOTSPOTS_DOTNET_ENABLED_RUN_ID}  ${EMPTY}
${HOTSPOTS_DOTNET_DISABLED_RUN_ID}  ${EMPTY}
${DOTNET_RENDER_SESSION}           ${EMPTY}


*** Test Cases ***
CPU Microarchitecture Recipe Produces Dotnet Symbols When Stack Collection Enabled
  [Documentation]  Render the .NET-enabled cpu_microarchitecture run and verify .NET symbols are produced.
  [Tags]  cpu_microarchitecture
  Skip Unless CPU Microarchitecture Is Supported On Target
  Given The Run Exists  ${CPU_MICROARCH_DOTNET_ENABLED_RUN_ID}
  When Render Dotnet Run  ${CPU_MICROARCH_DOTNET_ENABLED_RUN_ID}
  Then The Render Produced Dotnet Symbols

CPU Microarchitecture Recipe Does Not Produce Dotnet Symbols When Stack Collection Disabled
  [Documentation]  Render the .NET-disabled cpu_microarchitecture run and verify .NET symbols aren't produced.
  [Tags]  cpu_microarchitecture
  Skip Unless CPU Microarchitecture Is Supported On Target
  Given The Run Exists  ${CPU_MICROARCH_DOTNET_DISABLED_RUN_ID}
  When Render Dotnet Run  ${CPU_MICROARCH_DOTNET_DISABLED_RUN_ID}
  Then The Render Did Not Produce Dotnet Symbols

Code Hotspots Recipe Produces Dotnet Symbols When Stack Collection Enabled
  [Documentation]  Render the .NET-enabled code hotspots run and verify .NET symbols are produced.
  [Tags]  code_hotspots
  Given The Run Exists  ${HOTSPOTS_DOTNET_ENABLED_RUN_ID}
  When Render Dotnet Run  ${HOTSPOTS_DOTNET_ENABLED_RUN_ID}
  Then The Render Produced Dotnet Symbols

Code Hotspots Recipe Does Not Produce Dotnet Symbols When Stack Collection Disabled
  [Documentation]  Render the .NET-disabled code hotspots run and verify .NET symbols aren't produced.
  [Tags]  code_hotspots
  Given The Run Exists  ${HOTSPOTS_DOTNET_DISABLED_RUN_ID}
  When Render Dotnet Run  ${HOTSPOTS_DOTNET_DISABLED_RUN_ID}
  Then The Render Did Not Produce Dotnet Symbols


*** Keywords ***
Dotnet Recipe Suite Setup
  Common Setup
  Ensure Dotnet Workload Is Available
  Set Dotnet Workload Cmd
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  The Test Target Is Prepared Successfully
  Generate Dotnet Runs

Dotnet Recipe Suite Teardown
  Run Keyword And Ignore Error  The Target Is Unprepared Successfully
  Run Keyword And Ignore Error  The Test Target Is Removed Successfully
  Common Teardown

Ensure Dotnet Workload Is Available
  ${workload_path} =  Get Workload Path  ${DOTNET_WORKLOAD_NAME}
  IF  "${workload_path}" == ""
    Skip  Skipping test because workload ${DOTNET_WORKLOAD_NAME} was not provided.
  END
  VAR  ${DOTNET_WORKLOAD_PATH}  ${workload_path}  scope=SUITE

Set Dotnet Workload Cmd
  VAR  ${DOTNET_WORKLOAD_CMD}  "${DOTNET_WORKLOAD_PATH} ${DOTNET_WORKLOAD_ARGS}"  scope=SUITE

Generate Dotnet Runs
  Generate Dotnet CPU Microarchitecture Runs
  Generate Dotnet Code Hotspots Runs

Generate Dotnet CPU Microarchitecture Runs
  Skip Unless CPU Microarchitecture Is Supported On Target
  The CPU Microarchitecture Recipe Is Listed
  ${cpu_microarchitecture_enabled} =  Run Dotnet CPU Microarchitecture Recipe  collect=true
  VAR  ${CPU_MICROARCH_DOTNET_ENABLED_RUN_ID}  ${cpu_microarchitecture_enabled}  scope=SUITE
  ${cpu_microarchitecture_disabled} =  Run Dotnet CPU Microarchitecture Recipe  collect=false
  VAR  ${CPU_MICROARCH_DOTNET_DISABLED_RUN_ID}  ${cpu_microarchitecture_disabled}  scope=SUITE

Generate Dotnet Code Hotspots Runs
  The Code Hotspots Recipe Is Listed
  ${hotspots_enabled} =  Run Dotnet Hotspots Recipe  collect=true
  VAR  ${HOTSPOTS_DOTNET_ENABLED_RUN_ID}  ${hotspots_enabled}  scope=SUITE
  ${hotspots_disabled} =  Run Dotnet Hotspots Recipe  collect=false
  VAR  ${HOTSPOTS_DOTNET_DISABLED_RUN_ID}  ${hotspots_disabled}  scope=SUITE

Run Dotnet CPU Microarchitecture Recipe
  [Arguments]  ${collect}
  Run CPU Microarchitecture Recipe
  ...  --timeout 5 --workload ${DOTNET_WORKLOAD_CMD} --param collect_dotnet_stacks=${collect} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${run_id} =  Extract The Run ID
  The Run Exists  ${run_id}
  RETURN  ${run_id}

Run Dotnet Hotspots Recipe
  [Arguments]  ${collect}
  Run Code Hotspots Recipe
  ...  --timeout 5 --workload ${DOTNET_WORKLOAD_CMD} --param collect_dotnet_stacks=${collect} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${run_id} =  Extract The Run ID
  The Run Exists  ${run_id}
  RETURN  ${run_id}

Render Dotnet Run
  [Arguments]  ${run_id}
  Run Render  ${run_id}
  The Render Invocation Was Successful
  ${session} =  Extract The Render Session
  VAR  ${DOTNET_RENDER_SESSION}  ${session}  scope=SUITE

The Render Produced Dotnet Symbols
  ${session} =  Get Variable Value  ${DOTNET_RENDER_SESSION}  ${EMPTY}
  The Render Session ID Is Valid  ${session}
  ${row_count} =  Count Rows For Render Session Query  ${session}  ${DOTNET_SYMBOL_QUERY}
  Should Be True  ${row_count} > 0

The Render Did Not Produce Dotnet Symbols
  ${session} =  Get Variable Value  ${DOTNET_RENDER_SESSION}  ${EMPTY}
  The Render Session ID Is Valid  ${session}
  ${row_count} =  Count Rows For Render Session Query  ${session}  ${DOTNET_SYMBOL_QUERY}
  Should Be Equal As Integers  ${row_count}  0
