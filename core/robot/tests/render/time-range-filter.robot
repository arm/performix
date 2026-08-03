# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A test suite to exercise time range filtering for render and run render.

Resource            ../../resources/keywords/target.resource
Resource            ../../resources/keywords/render.resource
Resource            ../../resources/keywords/environment.resource
Resource            ../../resources/keywords/time_range_filter.resource

Suite Setup         Time Range Filter Suite Setup
Suite Teardown      Time Range Filter Suite Teardown

Test Tags           render  time-filter


*** Variables ***
${CPU_MICROARCH_RUN_ID_1}               NONE
${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}     NONE
${CODE_HOTSPOTS_RUN_ID_1}               NONE
${TIME_RANGE_FILTER_SAMPLING_FREQ}      high


*** Test Cases ***
The Code Hotspots Run Rendered With Full Time Filter Matches Unfiltered Samples
  [Documentation]  Check that filtering a run over full time range produces unchanged results.
  [Tags]  code-hotspots
  Given The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  And The Unfiltered Run Contains Periodic Samples  ${CODE_HOTSPOTS_RUN_ID_1}
  When The Run Is Filtered With Full Time Range  ${CODE_HOTSPOTS_RUN_ID_1}
  Then The Run Data Is Unchanged

The Code Hotspots Run Rendered With Partial Time Filter Contains Fewer Samples
  [Documentation]  Check that filtering a run over partial time range produces fewer samples.
  [Tags]  code-hotspots
  Given The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  And The Unfiltered Run Contains Periodic Samples  ${CODE_HOTSPOTS_RUN_ID_1}
  When The Run Is Filtered Over A Partial Time Range  ${CODE_HOTSPOTS_RUN_ID_1}
  Then The Run Data Contains Fewer Samples

The Code Hotspots Run Rendered With Partial Time Filter Keeps Original Time Limits
  [Documentation]  Check that filtering a run over partial time range keeps the original time limits.
  [Tags]  code-hotspots
  Given The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  And The Unfiltered Run Contains Periodic Samples  ${CODE_HOTSPOTS_RUN_ID_1}
  When The Run Is Filtered Over A Partial Time Range  ${CODE_HOTSPOTS_RUN_ID_1}
  Then The Run Data Keeps Original Time Limits

The Instruction Mix Run Rendered With Full Time Filter Matches Unfiltered Samples
  [Documentation]  Check that filtering a run over full time range produces unchanged results.
  [Tags]  instruction-mix
  [Setup]  Skip Unless Instruction Mix Is Supported On Target
  Given The Run Exists  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  And The Unfiltered Run Contains Periodic Samples
  ...  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  ...  drilldown_1
  ...  drilldown_measurements_1
  When The Run Is Filtered With Full Time Range
  ...  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  ...  drilldown_1
  ...  drilldown_measurements_1
  Then The Run Data Is Unchanged

The Instruction Mix Run Rendered With Partial Time Filter Contains Fewer Samples
  [Documentation]  Check that filtering a run over partial time range produces fewer samples.
  [Tags]  instruction-mix
  [Setup]  Skip Unless Instruction Mix Is Supported On Target
  Given The Run Exists  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  And The Unfiltered Run Contains Periodic Samples
  ...  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  ...  drilldown_1
  ...  drilldown_measurements_1
  When The Run Is Filtered Over A Partial Time Range
  ...  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  ...  drilldown_1
  ...  drilldown_measurements_1
  Then The Run Data Contains Fewer Samples

The Instruction Mix Run Rendered With Partial Time Filter Keeps Original Time Limits
  [Documentation]  Check that filtering a run over partial time range keeps the original time limits.
  [Tags]  instruction-mix
  [Setup]  Skip Unless Instruction Mix Is Supported On Target
  Given The Run Exists  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  And The Unfiltered Run Contains Periodic Samples
  ...  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  ...  drilldown_1
  ...  drilldown_measurements_1
  When The Run Is Filtered Over A Partial Time Range
  ...  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  ...  drilldown_1
  ...  drilldown_measurements_1
  Then The Run Data Keeps Original Time Limits

The CPU Microarchitecture Run Rendered With Full Time Filter Matches Unfiltered Samples
  [Documentation]  Check that filtering a run over full time range produces unchanged results.
  [Tags]  cpu-microarchitecture
  [Setup]  Skip Unless CPU Microarchitecture Is Supported On Target
  Given The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  And The Unfiltered Run Contains Periodic Samples  ${CPU_MICROARCH_RUN_ID_1}
  When The Run Is Filtered With Full Time Range  ${CPU_MICROARCH_RUN_ID_1}
  Then The Run Data Is Unchanged

The CPU Microarchitecture Run Rendered With Partial Time Filter Contains Fewer Samples
  [Documentation]  Check that filtering a run over partial time range produces fewer samples.
  [Tags]  cpu-microarchitecture
  [Setup]  Skip Unless CPU Microarchitecture Is Supported On Target
  Given The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  And The Unfiltered Run Contains Periodic Samples  ${CPU_MICROARCH_RUN_ID_1}
  When The Run Is Filtered Over A Partial Time Range  ${CPU_MICROARCH_RUN_ID_1}
  Then The Run Data Contains Fewer Samples

The CPU Microarchitecture Run Rendered With Partial Time Filter Keeps Original Time Limits
  [Documentation]  Check that filtering a run over partial time range keeps the original time limits.
  [Tags]  cpu-microarchitecture
  [Setup]  Skip Unless CPU Microarchitecture Is Supported On Target
  Given The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  And The Unfiltered Run Contains Periodic Samples  ${CPU_MICROARCH_RUN_ID_1}
  When The Run Is Filtered Over A Partial Time Range  ${CPU_MICROARCH_RUN_ID_1}
  Then The Run Data Keeps Original Time Limits


*** Keywords ***
Time Range Filter Suite Setup
  Common Setup
  Skip Unless Target OS Is  ${OS_LINUX}
  Set Enable Rerendering Env Var
  Set Enable Full Capture Support Env Var
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  Generate Time Range Filter Runs

Time Range Filter Suite Teardown
  The Test Target Is Removed Successfully
  Clear Enable Full Capture Support Env Var
  Clear Enable Rerendering Env Var
  Common Teardown

Generate Time Range Filter Runs
  Generate Code Hotspots Run
  ${cpu_microarchitecture_supported} =  CPU Microarchitecture Is Supported On Target
  IF  ${cpu_microarchitecture_supported}  Generate CPU Microarchitecture Run
  ${instruction_mix_supported} =  Instruction Mix Is Supported On Target
  IF  ${instruction_mix_supported}  Generate Instruction Mix Dynamic Run

Generate Code Hotspots Run
  ${run_id} =  Run Time Range Filter Recipe And Extract Run ID
  ...  code_hotspots
  ...  --param sampling_freq=${TIME_RANGE_FILTER_SAMPLING_FREQ}
  VAR  ${CODE_HOTSPOTS_RUN_ID_1} =  ${run_id}  scope=SUITE

Generate CPU Microarchitecture Run
  ${run_id} =  Run Time Range Filter Recipe And Extract Run ID
  ...  cpu_microarchitecture
  ...  --param sampling_freq=${TIME_RANGE_FILTER_SAMPLING_FREQ}
  VAR  ${CPU_MICROARCH_RUN_ID_1} =  ${run_id}  scope=SUITE

Generate Instruction Mix Dynamic Run
  ${run_id} =  Run Time Range Filter Recipe And Extract Run ID
  ...  instruction_mix
  ...  --param mode=dynamic --param sampling_freq=${TIME_RANGE_FILTER_SAMPLING_FREQ}
  VAR  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1} =  ${run_id}  scope=SUITE

Run Time Range Filter Recipe And Extract Run ID
  [Documentation]  Run a recipe with time-filter test parameters and return a run ID.
  [Arguments]  ${recipe}  ${extra_args}=${EMPTY}
  Run Recipe
  ...  ${recipe}
  ...  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG} ${extra_args}
  The Last Command Succeeded
  ${id} =  Extract The Run ID
  The Run Exists  ${id}
  RETURN  ${id}
