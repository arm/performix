# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'recipe run' CLI of Arm Total Performance.

Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/jitdump.resource
Resource        ../../resources/keywords/recipe.resource
Resource        ../../resources/keywords/remote_localhost.resource
Resource        ../../resources/keywords/render.resource
Resource        ../../resources/keywords/run.resource
Resource        ../../resources/keywords/target.resource

Suite Setup     Recipe Run Suite Setup
Suite Teardown  Recipe Run Suite Teardown

Test Tags       recipe  run


*** Variables ***
${L2_MULTIPLE_METRIC_GROUP}  --param=metrics_group=branch_effectiveness
...                          --param=metrics_group=itlb_effectiveness
...                          --param=metrics_group=l1i_cache_effectiveness
...                          --param=metrics_group=l2_cache_effectiveness
...                          --param=metrics_group=ll_cache_effectiveness
${SLEEP_SCRIPT}  import subprocess, sys, time;subprocess.Popen(["python3", "-c", "import time; time.sleep(60)"]);time.sleep(60)
${SLEEP_PROCESS_MATCH}  ^python3 -c import time; time.sleep
${SLEEP_SCRIPT_PATH}  ${EMPTY}


*** Test Cases ***
The Instruction Mix Recipe Fails When Invalid Mode Specified
  [Documentation]  Tests that the instruction mix recipe fails if an invalid / unknown mode
  ...  is specified.
  [Tags]  instruction-mix
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run Instruction Mix Recipe
  ...  --param mode=my_made_up_mode --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed
  And The Last Command Failed With Message Code  engine.recipeparser.js_recipe_stage.INVALID_PARAM_VALUE_SUMMARY
  And The Target Output Directory Is Empty

The Instruction Mix Recipe Fails When Java Collection Requested In Static Mode
  [Documentation]  Tests that the instruction mix recipe fails in static mode if collect_java_stacks
  ...  is enabled.
  [Tags]  instruction-mix
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run Instruction Mix Recipe
  ...  --param mode=static --param collect_java_stacks=true --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed
  And The Last Command Failed With Message Code  recipes.instruction_mix.JAVA_COLLECTION_STATIC_MODE
  And The Target Output Directory Is Empty

The CPU Microarchitecture Recipe Fails When An Invalid Workload Is Specified
  [Documentation]  Tests that the cpu_microarchitecture recipe fails if an invalid / unknown workload
  ...  is specified.
  [Tags]  cpu-microarchitecture
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run CPU Microarchitecture Recipe  --workload my_made_up_workload --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed
  And The Last Command Failed With Message Code  tool_integrations.common.WORKLOAD_NOT_EXIST_OR_NOT_EXECUTABLE
  And The Target Output Directory Is Empty

The CPU Microarchitecture Recipe Fails When An Invalid Parameter Is Specified
  [Documentation]  Tests that the cpu_microarchitecture recipe fails if an invalid parameter is specified.
  [Tags]  cpu-microarchitecture
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run CPU Microarchitecture Recipe
  ...  --param=foo=bar --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed
  And The Last Command Failed With Message Code  engine.parameters.INVALID_PARAM
  And The Target Output Directory Is Empty

The CPU Microarchitecture Recipe Fails For Collect Java Stacks Parameter On X86
  [Documentation]  Tests that the cpu_microarchitecture recipe fails on x86 targets when collect_java_stacks is enabled.
  [Tags]  cpu-microarchitecture
  Given Skip Unless Target Arch Is  ${ARCH_X86_64}
  And The CPU Microarchitecture Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run CPU Microarchitecture Recipe
  ...  --param collect_java_stacks=true --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed
  And The Last Command Failed With Message Code  tool_integrations.neoprof.JITDUMP_JVM_UNSUPPORTED_ARCH
  And The Target Output Directory Is Empty

The CPU Microarchitecture Recipe Fails When Dotnet Agent Is Not Deployed
  [Documentation]  Verifies cpu_microarchitecture run fails on supported targets if collect_dotnet_stacks is enabled but the .NET agent is missing.
  [Tags]  cpu-microarchitecture  dotnet
  Given Skip Unless CPU Microarchitecture Is Supported On Target
  And The CPU Microarchitecture Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  And The Dotnet Agent Is Not Deployed
  When Run CPU Microarchitecture Recipe
  ...  --param collect_dotnet_stacks=true --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME}
  Then The Last Command Failed
  And The Last Command Failed With Message Code  tool_integrations.common.TOOL_NOT_DEPLOYED
  And The Target Output Directory Is Empty

The Code Hotspots Recipe Fails When Dotnet Agent Is Not Deployed
  [Documentation]  Verifies code_hotspots run fails on supported targets if collect_dotnet_stacks is enabled but the .NET agent is missing.
  ...  This is intended to be executed on x86 as well (code_hotspots is the only recipe supported on x86),
  ...  ensuring the .NET gating and error propagation path is exercised.
  [Tags]  code-hotspots  dotnet
  Given The Code Hotspots Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  And Deploy Tools For Recipe  code_hotspots
  And The Dotnet Agent Is Not Deployed
  When Run Code Hotspots Recipe
  ...  --param collect_dotnet_stacks=true --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME}
  Then The Last Command Failed
  And The Last Command Failed With Message Code  tool_integrations.common.TOOL_NOT_DEPLOYED
  And The Target Output Directory Is Empty

The CPU Microarchitecture Recipe Fails When Jitdump-JVM Is Not Deployed
  [Documentation]  Verifies cpu_microarchitecture run fails on aarch64 if collect_java_stacks is enabled but Jitdump-JVM is missing.
  [Tags]  cpu-microarchitecture
  Given Skip Unless CPU Microarchitecture Is Supported On Target
  And The CPU Microarchitecture Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  And Deploy Tools For Recipe  cpu_microarchitecture
  And Jitdump-JVM Is Not Deployed
  When Run CPU Microarchitecture Recipe
  ...  --param collect_java_stacks=true --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME}
  Then The Last Command Failed
  And The Last Command Failed With Message Code  tool_integrations.common.TOOL_NOT_DEPLOYED
  And The Target Output Directory Is Empty

The CPU Microarchitecture Recipe Can Be Run Successfully To Profile A Workload
  [Documentation]  Tests that the cpu_microarchitecture recipe can be run successfully on a specific workload
  ...  with tool deployment.
  [Tags]  cpu-microarchitecture
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run CPU Microarchitecture Recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty

The CPU Microarchitecture Recipe Can Be Run Successfully To Profile A Workload On Localhost
  [Documentation]  Tests that the cpu_microarchitecture recipe can be run when launching a workload on localhost.
  ...  Uses a remote localhost setup to ensure >2 PMU counters are available.
  [Tags]  cpu-microarchitecture  remote-localhost
  [Setup]  Skip If Remote Localhost Is Not Set Up
  Given The Remote Localhost CPU Microarchitecture Recipe Is Listed
  When Run Remote Localhost CPU Microarchitecture Recipe  --workload ${LAUNCH_WORKLOAD} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty
  [Teardown]  Clean Up Remote Localhost

The CPU Microarchitecture Recipe Can Be Run Successfully With Multiple L2 Metric Groups
  [Documentation]  Tests that the cpu_microarchitecture recipe can be run successfully with multiple L2 metric groups.
  [Tags]  cpu-microarchitecture
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run CPU Microarchitecture Recipe
  ...  ${L2_MULTIPLE_METRIC_GROUP} --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty

The CPU Microarchitecture Recipe Can Be Run Successfully When Doing System Wide Profile
  [Documentation]  Tests that the cpu_microarchitecture recipe can do a successful system wide profile.
  ...  This test uses the --timeout argument to specify the number of seconds
  ...  to run for.
  [Tags]  cpu-microarchitecture
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run CPU Microarchitecture Recipe  --system-wide --timeout 1 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty

The Instruction Mix Recipe Can Be Run Successfully In Both Mode To Profile A Workload
  [Documentation]  Tests that the instruction mix recipe can be run successfully in
  ...  both mode on a specific workload with tool deployment.
  [Tags]  instruction-mix
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run Instruction Mix Recipe
  ...  --param mode=both --workload ${LAUNCH_WORKLOAD} --timeout 1 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty

The Instruction Mix Recipe Can Be Run Successfully In Dynamic Mode To Profile A Workload
  [Documentation]  Tests that the instruction mix recipe can be run successfully in
  ...  dynamic mode on a specific workload with tool deployment.
  [Tags]  instruction-mix
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run Instruction Mix Recipe
  ...  --param mode=dynamic --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty

The Instruction Mix Recipe Can Be Run Successfully In Dynamic Mode With Launch Workload On Localhost
  [Documentation]  Tests that the instruction mix recipe can be run successfully in
  ...  dynamic mode when launching a new workload on localhost.
  ...  Uses a remote localhost setup to ensure >2 PMU counters are available.
  [Tags]  instruction-mix  remote-localhost
  [Setup]  Skip If Remote Localhost Is Not Set Up
  Given The Remote Localhost Instruction Mix Recipe Is Listed
  When Run Remote Localhost Instruction Mix Recipe
  ...  --param mode=dynamic --workload ${LAUNCH_WORKLOAD} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty
  [Teardown]  Clean Up Remote Localhost

The Instruction Mix Recipe Can Be Run Successfully In Static Mode To Profile A Workload
  [Documentation]  Tests that the instruction mix recipe can be run successfully in
  ...  static mode on a specific workload with tool deployment.
  [Tags]  instruction-mix
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run Instruction Mix Recipe
  ...  --param mode=static --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty

The Code Hotspots Recipe Can Be Run Successfully To Profile A Workload
  [Documentation]  Tests that the Code Hotspots recipe can be run successfully on a specific
  ...  workload with tool deployment.
  [Tags]  code-hotspots
  Given The Code Hotspots Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run Code Hotspots Recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty

The Code Hotspots Recipe Can Be Run Successfully To Profile A Workload On Localhost
  [Documentation]  Tests that the Code Hotspots recipe can be run successfully on a specific workload with tool deployment on localhost.
  [Tags]  code-hotspots  localhost
  [Setup]  Run Keywords  Set Localhost As Test Target
  ...  AND  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  ...  AND  Prepare The Test Target If Needed
  Given The Code Hotspots Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  When Run Code Hotspots Recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  Restore Remote Test Target
  ...  AND  Ensure Recipe Run Suite Target Is Ready

The Memory Access Recipe Can Be Run Successfully To Profile A Workload
  [Documentation]  Tests that the memory access recipe can be run successfully on a specific
  ...  workload with tool deployment.
  # This test is disabled until we configure SPE on the CI test targets
  [Tags]  memory-access  disabled
  Given The Memory Access Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run Memory Access Recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded

The System Utilization Recipe Can Be Run Successfully To Profile A Workload
  [Documentation]  Tests that the System Utilization recipe can be run successfully on a specific
  ...  workload with tool deployment.
  [Tags]  system-utilization
  [Setup]  Skip Unless System Utilization Is Supported On Target
  Given The System Utilization Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run System Utilization Recipe
  ...  --workload ${LAUNCH_WORKLOAD} --param=interval=0.1 --timeout 1 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty

The External Test Recipe Can Be Run Successfully To Profile A Workload
  [Documentation]  Tests that the custom recipe can be run
  ...  workload with tool deployment.
  [Tags]  custom-tool
  [Setup]  Copy The External Test Recipe Into The User Recipe Folder
  Given The Recipe Is Listed  custom_tool_recipe
  And The Target Exists  ${G_TARGET_NAME}
  When Run Recipe  custom_tool_recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty
  [Teardown]  Remove The External Test Recipe From The User Recipe Folder

The Custom Tool Recipe Run Fails With Invalid Parameter Values
  [Documentation]  Tests invalid parameter values interrupt recipe execution and produce an error
  ...  workload with tool deployment.
  [Tags]  custom-tool
  [Setup]  Copy The External Test Recipe Into The User Recipe Folder
  Given The Recipe Is Listed  custom_tool_recipe
  And The Target Exists  ${G_TARGET_NAME}
  When Run Recipe  custom_tool_recipe
  ...  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG} --param=sampling_freq=invalidFreq
  Then The Last Command Failed
  And The Target Output Directory Is Empty
  [Teardown]  Remove The External Test Recipe From The User Recipe Folder

The Working Directory Remains In Place On The Target If No Cleanup Is Set
  [Documentation]  Tests that the working directory on the target remains in place if the
  ...  --no-cleanup parameter is set.
  [Tags]  code-hotspots
  Given The Code Hotspots Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run Code Hotspots Recipe
  ...  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG} --no-cleanup
  Then The Last Command Succeeded
  And The Target Output Directory Is Not Empty
  [Teardown]  The Output Directory Is Removed From The Target

Recipe Run Uses The Correct Target When Multiple Targets Exist
  [Documentation]  Tests that recipe run uses the correct target when multiple targets exist.
  [Tags]  code-hotspots
  [Setup]  Run Keywords  Remove All Targets
  ...  AND  Create Default Dummy Target  dummy
  ...  AND  The Test Target Is Added Successfully
  ...  AND  Prepare The Test Target If Needed
  Given The Code Hotspots Recipe Is Listed
  And The Target Is The Default  dummy
  And The Target Is Not The Default  ${G_TARGET_NAME}
  When Run Code Hotspots Recipe
  ...  --workload ${LAUNCH_WORKLOAD} --timeout 1 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty
  [Teardown]  Run Keywords  Remove All Targets
  ...  AND  Ensure Recipe Run Suite Target Is Ready

Code Hotspots Succeeds When Capturing Is Cut Off By Timeout
  [Documentation]  Tests that the Code Hotspots recipe doesn't return an error if profiling is cut
  ...  short due to the timeout being hit when the timeout cuts off the run
  # Disabled by defect https://jira.arm.com/browse/NEOPROF-411
  [Tags]  code-hotspots  disabled
  Given The Code Hotspots Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run Code Hotspots Recipe  --workload "sleep 5" --timeout 1 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty

CPU Microarchitecture Succeeds When Capturing Is Cut Off By Timeout
  [Documentation]  Tests that the cpu_microarchitecture recipe doesn't return an error if profiling is cut short
  ...  due to the timeout being hit
  # Disabled by defect https://jira.arm.com/browse/NEOPROF-411
  [Tags]  cpu-microarchitecture  disabled
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Target Is Prepared
  When Run CPU Microarchitecture Recipe
  ...  --workload "sleep 5" --timeout 1 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Succeeded
  And The Target Output Directory Is Empty

Recipe Workload Is Killed When A Run Is Stopped
  [Documentation]  Tests that processes started by a recipe workload are killed when a run is stopped
  [Tags]  code-hotspots  stop
  [Setup]  Set Up Sleep Script On Target
  Given No Processes Are Running On The Target  ${SLEEP_PROCESS_MATCH}
  And The Target Is Prepared
  And Start Code Hotspots Recipe  --workload "python3 ${SLEEP_SCRIPT_PATH}" --timeout 30 --target ${G_TARGET_NAME}
  And Wait For Workload To Run On The Target  ${SLEEP_PROCESS_MATCH}
  When Stop Run  ${G_LAST_PROCESS}
  Then No Processes Are Running On The Target  ${SLEEP_PROCESS_MATCH}
  And The Target Output Directory Is Empty
  [Teardown]  Run Keywords  Remove Sleep Script From Target
  ...  AND  The Output Directory Is Removed From The Target

Recipe Workload Is Killed When A Run Is Cancelled
  [Documentation]  Tests that processes started by a recipe workload are killed when a run is cancelled
  [Tags]  code-hotspots  cancel
  [Setup]  Set Up Sleep Script On Target
  Given No Processes Are Running On The Target  ${SLEEP_PROCESS_MATCH}
  And The Target Is Prepared
  And Start Code Hotspots Recipe  --workload "python3 ${SLEEP_SCRIPT_PATH}" --timeout 30 --target ${G_TARGET_NAME}
  And Wait For Workload To Run On The Target  ${SLEEP_PROCESS_MATCH}
  When Cancel Run  ${G_LAST_PROCESS}
  Then No Processes Are Running On The Target  ${SLEEP_PROCESS_MATCH}
  And The Target Output Directory Is Empty
  [Teardown]  Run Keywords  Remove Sleep Script From Target
  ...  AND  The Output Directory Is Removed From The Target

Recipe Run Uses Shell Mode If Requested
  [Documentation]  Tests that recipe run uses shell mode if the --use-shell flag was set
  [Tags]  code-hotspots
  [Setup]  Skip Unless Target OS Is  ${OS_LINUX}
  Given The Code Hotspots Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  When Run Code Hotspots Recipe With Use Shell
  Then The Last Command Succeeded
  And The Neoprof Capture Log Contains  this is bar
  And The Target Output Directory Is Empty


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.
Recipe Run Suite Setup
  Common Setup
  Ensure Recipe Run Suite Target Is Ready

Recipe Run Suite Teardown
  Run Keyword And Ignore Error  The Output Directory Is Removed From The Target
  Run Keyword And Ignore Error  The Target Is Unprepared Successfully
  Run Keyword And Ignore Error  The Test Target Is Removed Successfully
  Common Teardown

Ensure Recipe Run Suite Target Is Ready
  Ensure Target Does Not Exist  ${G_TARGET_NAME}
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  Prepare The Test Target If Needed
  The Output Directory Is Removed From The Target

Set Up Sleep Script On Target
  Ensure Python3 Available On Target
  Write Target Temp File  sleep.py  ${SLEEP_SCRIPT}
  The Last Command Succeeded
  ${path} =  Strip String  ${G_LAST_RESULT.stdout}
  VAR  ${SLEEP_SCRIPT_PATH} =  ${path}  scope=SUITE

Remove Sleep Script From Target
  The File Is Removed From The Target  ${SLEEP_SCRIPT_PATH}

Run Code Hotspots Recipe With Use Shell
  ${recipe_args} =  Catenate  --use-shell --workload
  ...  "FOO=bar; sleep 1; echo \\"this is \$FOO\\"" --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  ${escaped} =  Escape Dollar If Needed  ${recipe_args}
  Run Code Hotspots Recipe  ${escaped}
