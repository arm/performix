# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'render' and 'run render' CLI of Arm Total Performance.

Resource        ../../resources/keywords/target.resource
Resource        ../../resources/keywords/render.resource
Resource        ../../resources/keywords/process.resource
Resource        ../../resources/keywords/environment.resource

Suite Setup     Render Suite Setup
Suite Teardown  Render Suite Teardown

Test Tags  render


*** Variables ***
${CPU_MICROARCH_RUN_ID_1}  NONE
${CPU_MICROARCH_RUN_ID_2}  NONE
${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}  NONE
${INSTRUCTION_MIX_DYNAMIC_RUN_ID_2}  NONE
${INSTRUCTION_MIX_STATIC_RUN_ID_1}  NONE
${INSTRUCTION_MIX_STATIC_RUN_ID_2}  NONE
${CODE_HOTSPOTS_RUN_ID_1}  NONE
${CODE_HOTSPOTS_RUN_ID_2}  NONE
${MEMORY_ACCESS_RUN_ID}  NONE
${SYSTEM_UTILIZATION_RUN_ID}  NONE
${FLAT_COMPARISON_RENDER_CONFIG}  NONE
${FLAT_RENDER_CONFIG}  NONE
${FLAT_PERIODIC_RENDER_CONFIG}  NONE
${SLANALYZE_RENDER_CONFIG_TEMPLATE}  NONE
${PYTHON_PID}  ${EMPTY}
${YES_PID}  ${EMPTY}
${RUN_ID}  ${EMPTY}
${PROC_NAME}  ${EMPTY}


*** Test Cases ***
### StreamlineAnalyzeFlatFunctions2 ###

The Run Can Be Invoke-Rendered With StreamlineAnalyzeFlatFunctions2
  [Documentation]  Check that a run can be invoke-rendered with StreamlineAnalyzeFlatFunctions2
  [Tags]  cpu-microarchitecture
  Given The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  When Run Invoke Render  ${CPU_MICROARCH_RUN_ID_1}  renderer_configs=${FLAT_RENDER_CONFIG}
  Then The Render Invocation Was Successful
  And The Render Session ID Is Valid  ${G_RENDER_SESSION_ID}

The StreamlineAnalyzeFlatFunctions2 Invoke-Render Session Tables Are Queried Successfully
  [Documentation]  Checks that all tables for StreamlineAnalyzeFlatFunctions2 render session can be queried succssfully.
  [Tags]  cpu-microarchitecture
  [Setup]  Run Keywords  The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  ...  AND  The Runs Are Invoke-Rendered Successfully  ${CPU_MICROARCH_RUN_ID_1}  renderer_configs=${FLAT_RENDER_CONFIG}
  Given The Render Session ID Is Valid  ${G_RENDER_SESSION_ID}
  When All Render Session Tables Are Queried  renderer=StreamlineAnalyzeFlatFunctions2
  Then All Render Session Query Results Were Successful

### StreamlineAnalyzeFunctionProfileRenderer2 tests ###

The Run Can Be Invoke-Rendered With StreamlineAnalyzeFunctionProfileRenderer2
  [Documentation]  Check that a run can be rendererd with StreamlineAnalyzeFunctionProfileRenderer2
  [Tags]  cpu-microarchitecture
  Given The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  When Run Invoke Render  ${CPU_MICROARCH_RUN_ID_1}  renderer_configs=${FLAT_RENDER_CONFIG}
  Then The Render Invocation Was Successful
  And The Render Session ID Is Valid  ${G_RENDER_SESSION_ID}

The StreamlineAnalyzeFunctionProfileRenderer2 Invoke-Render Session Tables Are Queried Successfully
  [Documentation]  Checks that all tables for StreamlineAnalyzeFunctionProfileRenderer2 render session can be queried succssfully.
  [Tags]  cpu-microarchitecture
  [Setup]  Run Keywords  The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  ...  AND  The Runs Are Invoke-Rendered Successfully  ${CPU_MICROARCH_RUN_ID_1}  renderer_configs=${FLAT_RENDER_CONFIG}
  ...  AND  Sleep  1s
  Given The Render Session ID Is Valid  ${G_RENDER_SESSION_ID}
  When All Render Session Tables Are Queried  renderer=StreamlineAnalyzeFunctionProfileRenderer2
  Then All Render Session Query Results Were Successful

### TargetInfoRenderer tests ###

The Run Can Be Invoke-Rendered With TargetInfoRenderer
  [Documentation]  Check that a run can be rendererd with TargetInfoRenderer
  [Tags]  code-hotspots
  Given The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  When Run Invoke Render  ${CODE_HOTSPOTS_RUN_ID_1}  renderer_configs=--renderer=TargetInfoRenderer={}
  Then The Render Invocation Was Successful
  And The Render Session ID Is Valid  ${G_RENDER_SESSION_ID}

The TargetInfoRenderer Invoke-Render Session Tables Are Queried Successfully
  [Documentation]  Checks that all tables for TargetInfoRenderer render session can be queried succssfully.
  [Tags]  code-hotspots
  [Setup]  Run Keywords  The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  ...  AND  The Runs Are Invoke-Rendered Successfully  ${CODE_HOTSPOTS_RUN_ID_1}  renderer_configs=--renderer=TargetInfoRenderer={}
  Given The Render Session ID Is Valid  ${G_RENDER_SESSION_ID}
  When All Render Session Tables Are Queried  renderer=TargetInfoRenderer
  Then All Render Session Query Results Were Successful

The SlAnalyze Renderer Successfully Filters A CPU Microarchitecture Run by PID
  [Documentation]  Invoke-render SlAnalyze and flat functions using a PID filter from a running process.
  [Tags]  cpu-microarchitecture
  [Setup]  Run Keywords  Skip Unless Target OS Is  ${OS_LINUX}
  ...  AND  Run Busy Loop Process On Target And Capture PID
  ...  AND  Run CPU Microarchitecture With System-Wide And Capture Run ID
  Given The Run Exists  ${RUN_ID}
  When Invoke-Render Is Run With SlAnalyze Filtered By PID
  Then The Render Invocation Was Successful
  And All Image Names Match The Process Name
  [Teardown]  Run Keywords  Stop Process On Target  ${PYTHON_PID}
  ...  AND  Stop Process On Target  ${YES_PID}

The SlAnalyze Renderer Successfully Filters A Code Hotspots Run by PID
  [Documentation]  Invoke-render SlAnalyze and flat functions using a PID filter from a running process.
  [Tags]  code-hotspots
  [Setup]  Run Keywords  Skip Unless Target OS Is  ${OS_LINUX}
  ...  AND  Run Busy Loop Process On Target And Capture PID
  ...  AND  Run Code Hotspots With System-Wide And Capture Run ID
  Given The Run Exists  ${RUN_ID}
  When Invoke-Render Is Run With SlAnalyze Filtered By PID  flat_renderer_config=${FLAT_PERIODIC_RENDER_CONFIG}
  Then The Render Invocation Was Successful
  And All Image Names Match The Process Name
  [Teardown]  Run Keywords  Stop Process On Target  ${PYTHON_PID}
  ...  AND  Stop Process On Target  ${YES_PID}

Run Render Successfully Filters a Code Hotspots Run by PID with SlAnalyze Renderer
  [Documentation]  Run render a Code Hotspots Run with PID filter and confirm it's successful.
  [Tags]  code-hotspots
  [Setup]  Run Keywords  Skip Unless Target OS Is  ${OS_LINUX}
  ...  AND  Run Busy Loop Process On Target And Capture PID
  ...  AND  Run Code Hotspots With System-Wide And Capture Run ID
  ...  AND  Set Enable Rerendering Env Var
  Given The Run Exists  ${RUN_ID}
  When Run Render With Flags  ${RUN_ID}  flags="--param=filter_pid=${PYTHON_PID}"
  Then The Render Invocation Was Successful
  And All Image Names Match The Process Name
  [Teardown]  Run Keywords  Stop Process On Target  ${PYTHON_PID}
  ...  AND  Stop Process On Target  ${YES_PID}
  ...  AND  Clear Enable Rerendering Env Var

### CompareDrilldownFlat tests ###

The Runs Can Be Invoke-Rendered With CompareDrilldownFlat
  [Documentation]  Check that a run can be rendererd with CompareDrilldownFlat
  [Tags]  cpu-microarchitecture
  Given The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  And The Run Exists  ${CPU_MICROARCH_RUN_ID_2}
  When Run Invoke Render
  ...  ${CPU_MICROARCH_RUN_ID_1}
  ...  ${CPU_MICROARCH_RUN_ID_2}
  ...  renderer_configs=${FLAT_COMPARISON_RENDER_CONFIG}
  Then The Render Invocation Was Successful
  And The Render Session ID Is Valid  ${G_RENDER_SESSION_ID}

The CompareDrilldownFlat Invoke-Render Session Tables Are Queried Successfully
  [Documentation]  Checks that all tables for CompareDrilldownFlat render session can be queried succssfully.
  [Tags]  cpu-microarchitecture
  [Setup]  Run Keywords  The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  ...  AND  The Run Exists  ${CPU_MICROARCH_RUN_ID_2}
  ...  AND  The Runs Are Invoke-Rendered Successfully
  ...  ${CPU_MICROARCH_RUN_ID_1}  ${CPU_MICROARCH_RUN_ID_2}
  ...  renderer_configs=${FLAT_COMPARISON_RENDER_CONFIG}
  Given The Render Session ID Is Valid  ${G_RENDER_SESSION_ID}
  When All Render Session Tables Are Queried  renderer=CompareDrilldownFlat
  Then All Render Session Query Results Were Successful

### DisassemblyRenderer tests ###

The Runs Can Be Invoke-Rendered With DisassemblyRenderer
  [Documentation]  Check that two runs can be rendered with the DisassemblyRenderer
  [Tags]  code-hotspots  disassembly
  Given The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  And The Run Exists  ${CODE_HOTSPOTS_RUN_ID_2}
  When Run Render  ${CODE_HOTSPOTS_RUN_ID_1}  ${CODE_HOTSPOTS_RUN_ID_2}
  Then The Render Invocation Was Successful
  And The Render Session ID Is Valid  ${G_RENDER_SESSION_ID}
  And The Render Session Table Exists  ${G_RENDER_SESSION_ID}  disassembly
  And The Render Session Table Exists  ${G_RENDER_SESSION_ID}  disassembly_1

### Log tests ###

The Run Can Be Invoke-Rendered With A Renderer ID
  [Documentation]  Check that a run can be invoke-rendered with an explicit renderer ID supplied.
  [Tags]  code-hotspots
  Given The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  When Run Invoke Render  ${CODE_HOTSPOTS_RUN_ID_1}  renderer_configs=--renderer=Log:foo={}
  Then The Render Invocation Was Successful
  And The Render Session Manifest Includes Renderer ID  foo

### Render tests ###

The CPU Microarchitecture Run Renders Successfully
  [Documentation]  Check that CPU Microarchitecture run can be rendered.
  [Tags]  cpu-microarchitecture
  Given The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  When Run Render  ${CPU_MICROARCH_RUN_ID_1}
  Then The Render Invocation Was Successful

The Instruction Mix Dynamic Run Renders Successfully
  [Documentation]  Check that Instruction Mix Dynamic run can be rendered.
  [Tags]  instruction-mix
  Given The Run Exists  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  When Run Render  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  Then The Render Invocation Was Successful

The Instruction Mix Static Run Renders Successfully
  [Documentation]  Check that Instruction Mix Static run can be rendered.
  [Tags]  instruction-mix
  Given The Run Exists  ${INSTRUCTION_MIX_STATIC_RUN_ID_1}
  When Run Render  ${INSTRUCTION_MIX_STATIC_RUN_ID_1}
  Then The Render Invocation Was Successful

The Code Hotspots Run Renders Successfully
  [Documentation]  Check that Code Hotspots run can be rendered.
  [Tags]  code-hotspots
  Given The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  When Run Render  ${CODE_HOTSPOTS_RUN_ID_1}
  Then The Render Invocation Was Successful

The Memory Access Run Renders Successfully
  [Documentation]  Check that Memory Access run can be rendered.
  ...  Disabled as there's no SPE data with the 1s timeout
  [Tags]  disabled  memory-access
  Given The Run Exists  ${MEMORY_ACCESS_RUN_ID}
  When Run Render  ${MEMORY_ACCESS_RUN_ID}
  Then The Render Invocation Was Successful

The System Utilization Run Renders Successfully
  [Documentation]  Check that a System Utilization run can be rendered.
  [Tags]  system-utilization
  [Setup]  Skip Unless System Utilization Is Supported On Target
  Given The Run Exists  ${SYSTEM_UTILIZATION_RUN_ID}
  When Run Render  ${SYSTEM_UTILIZATION_RUN_ID}
  Then The Render Invocation Was Successful

A Localhost Code Hotspots Run Can Be Rendered
  [Documentation]  Tests that a code_hotspots run created on localhost can be rendered. Localhost runs are generated
  ...  inline instead of during suite setup, so that only the test gets skipped (rather than the whole suite) if
  ...  localhost isn't supported.
  [Tags]  code-hotspots  localhost
  [Setup]  Run Keywords  Skip If Localhost Is Not Supported
  ...  AND  Generate Local Code Hotspots Runs
  Given The Run Exists  ${CODE_HOTSPOTS_LOCAL_RUN_ID}
  When Run Render  ${CODE_HOTSPOTS_LOCAL_RUN_ID}
  Then The Render Invocation Was Successful
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  Restore Remote Test Target

### Render tests for multiple runs ###
# These are validating that comparison renderers work without errors. We currently use the same
# run ID for both runs, since we're not validating the comparison results here, just that the
# rendering is successful. This essentially validates the renderers as well as the JS recipes,
# without the overhead of creating multiple runs.

The Two CPU Microarchitecture Runs Render Successfully
  [Documentation]  Check that two CPU Microarchitecture runs can be rendered.
  [Tags]  cpu-microarchitecture
  Given The Run Exists  ${CPU_MICROARCH_RUN_ID_1}
  When Run Render  ${CPU_MICROARCH_RUN_ID_1}  ${CPU_MICROARCH_RUN_ID_2}
  Then The Render Invocation Was Successful

The Two Instruction Mix Dynamic Runs Render Successfully
  [Documentation]  Check that two Instruction Mix Dynamic runs can be rendered.
  [Tags]  instruction-mix
  Given The Run Exists  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}
  When Run Render  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1}  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_2}
  Then The Render Invocation Was Successful

The Two Instruction Mix Static Runs Render Successfully
  [Documentation]  Check that two Instruction Mix Static runs can be rendered.
  [Tags]  instruction-mix
  Given The Run Exists  ${INSTRUCTION_MIX_STATIC_RUN_ID_1}
  And The Run Exists  ${INSTRUCTION_MIX_STATIC_RUN_ID_2}
  When Run Render  ${INSTRUCTION_MIX_STATIC_RUN_ID_1}  ${INSTRUCTION_MIX_STATIC_RUN_ID_2}
  Then The Render Invocation Was Successful

The Two Code Hotspots Runs Render Successfully
  [Documentation]  Check that two Code Hotspots runs can be rendered.
  [Tags]  code-hotspots
  Given The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  When Run Render  ${CODE_HOTSPOTS_RUN_ID_1}  ${CODE_HOTSPOTS_RUN_ID_2}
  Then The Render Invocation Was Successful

The Two Memory Access Runs Render Successfully
  [Documentation]  Check that two Memory Access runs can be rendered.
  ...  Disabled as there's no SPE data with the 1s timeout
  [Tags]  disabled  memory-access
  Given The Run Exists  ${MEMORY_ACCESS_RUN_ID}
  When Run Render  ${MEMORY_ACCESS_RUN_ID}  ${MEMORY_ACCESS_RUN_ID}
  Then The Render Invocation Was Successful


*** Keywords ***
Run Recipes For Suite
  Generate Code Hotspots Runs
  ${cpu_microarchitecture_supported} =  CPU Microarchitecture Is Supported On Target
  IF  ${cpu_microarchitecture_supported}  Generate CPU Microarchitecture Runs
  ${instruction_mix_supported} =  Instruction Mix Is Supported On Target
  IF  ${instruction_mix_supported}  Generate Instruction Mix Runs
  # Generate Memory Access Run // temporary disabled to unblock integration (also disabled deletion in Render Suite Teardown)
  ${system_utilization_supported} =  System Utilization Is Supported On Target
  IF  ${system_utilization_supported}  Generate System Utilization Run

Generate CPU Microarchitecture Runs
  ${run_id} =  Run CPU Microarchitecture Recipe And Extract Run ID
  VAR  ${CPU_MICROARCH_RUN_ID_1} =  ${run_id}  scope=SUITE
  ${run_id} =  Run CPU Microarchitecture Recipe And Extract Run ID
  VAR  ${CPU_MICROARCH_RUN_ID_2} =  ${run_id}  scope=SUITE

Generate Instruction Mix Runs
  ${run_id} =  Run Instruction Mix Recipe In Dynamic Mode And Extract Run ID
  VAR  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_1} =  ${run_id}  scope=SUITE
  ${run_id} =  Run Instruction Mix Recipe In Dynamic Mode And Extract Run ID
  VAR  ${INSTRUCTION_MIX_DYNAMIC_RUN_ID_2} =  ${run_id}  scope=SUITE
  ${run_id} =  Run Instruction Mix Recipe In Static Mode And Extract Run ID
  VAR  ${INSTRUCTION_MIX_STATIC_RUN_ID_1} =  ${run_id}  scope=SUITE
  ${run_id} =  Run Instruction Mix Recipe In Static Mode And Extract Run ID
  VAR  ${INSTRUCTION_MIX_STATIC_RUN_ID_2} =  ${run_id}  scope=SUITE

Generate Code Hotspots Runs
  ${run_id} =  Run Recipe And Extract Run ID  code_hotspots
  VAR  ${CODE_HOTSPOTS_RUN_ID_1} =  ${run_id}  scope=SUITE
  ${run_id} =  Run Recipe And Extract Run ID  code_hotspots
  VAR  ${CODE_HOTSPOTS_RUN_ID_2} =  ${run_id}  scope=SUITE

Generate Local Code Hotspots Runs
  Set Localhost As Test Target
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  The Test Target Is Prepared Successfully
  The Target Exists  ${G_TARGET_NAME}
  ${run_id} =  Run Code Hotspots Recipe And Extract Run ID
  VAR  ${CODE_HOTSPOTS_LOCAL_RUN_ID} =  ${run_id}  scope=SUITE

Generate Memory Access Run
  ${run_id} =  Run Memory Access Recipe And Extract Run ID
  VAR  ${MEMORY_ACCESS_RUN_ID} =  ${run_id}  scope=SUITE

Generate System Utilization Run
  ${run_id} =  Run System Utilization Recipe And Extract Run ID
  VAR  ${SYSTEM_UTILIZATION_RUN_ID} =  ${run_id}  scope=SUITE

Render Suite Setup
  Common Setup
  Set Enable Full Capture Support Env Var
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  Run Recipes For Suite
  Populate Render Session Tables
  Set Long Comparison Renderer Configs
  Set Long Source Code Renderer Configs

Render Suite Teardown
  The Test Target Is Removed Successfully
  Clear Enable Full Capture Support Env Var
  Common Teardown

Set Long Comparison Renderer Configs
  ${comparison_render_config} =  Catenate
  ...  --renderer=StreamlineAnalyzeSymbols={} \
  ...  --renderer=TargetInfoRenderer={} \
  ...  --renderer=StreamlineAnalyzeFlatFunctions2="{\\"data_source\\":\
  ...  {\\"tables\\": {\\"symbols\\": [{\\"name\\": \\"symbols\\"}, {\\"name\\": \\"symbols_1\\"}], \
  ...  \\"images\\": [{\\"name\\": \\"images\\"},{\\"name\\": \\"images_1\\"}],
  ...  \\"target_info_cpus\\": [{\\"name\\": \\"target_info_cpus\\"}, {\\"name\\": \\"target_info_cpus_1\\"}]}}}"  \
  ...  --renderer=CompareDrilldownFlat="{\\"data_source\\": {\\"tables\\": \
  ...  {\\"drilldown\\": [{\\"name\\": \\"drilldown\\"}, {\\"name\\": \\"drilldown_1\\"}],
  ...  \\"symbols\\": [{\\"name\\": \\"symbols\\"}, {\\"name\\": \\"symbols_1\\"}], \
  ...  \\"images\\": [{\\"name\\": \\"images\\"},{\\"name\\": \\"images_1\\"}]}}}"
  VAR  ${FLAT_COMPARISON_RENDER_CONFIG} =  ${comparison_render_config}  scope=SUITE

Set Long Source Code Renderer Configs
  ${render_config} =  Catenate
  ...  --renderer=StreamlineAnalyzeSymbols={} \
  ...  --renderer=TargetInfoRenderer={} \
  ...  --renderer=StreamlineAnalyzeFlatFunctions2="{\\"data_source\\":\
  ...  {\\"tables\\": {\\"symbols\\": [{\\"name\\": \\"symbols\\"}], \
  ...  \\"images\\": [{\\"name\\": \\"images\\"}], \
  ...  \\"target_info_cpus\\": [{\\"name\\": \\"target_info_cpus\\"}]}}}"
  VAR  ${FLAT_RENDER_CONFIG} =  ${render_config}  scope=SUITE
  ${periodic_render_config} =  Catenate
  ...  --renderer=StreamlineAnalyzeSymbols={} \
  ...  --renderer=TargetInfoRenderer={} \
  ...  --renderer=StreamlineAnalyzeFlatFunctions2="{\\"component\\":\\"functions-capture-periodic_sampling.csv\\",\\"data_source\\":\
  ...  {\\"tables\\": {\\"symbols\\": [{\\"name\\": \\"symbols\\"}], \
  ...  \\"images\\": [{\\"name\\": \\"images\\"}], \
  ...  \\"target_info_cpus\\": [{\\"name\\": \\"target_info_cpus\\"}]}}}"
  VAR  ${FLAT_PERIODIC_RENDER_CONFIG} =  ${periodic_render_config}  scope=SUITE
  VAR  ${slanalyze_render_config} =  --renderer=SlAnalyzeRenderer="{\\"filter_pid\\": __PID__}"
  VAR  ${SLANALYZE_RENDER_CONFIG_TEMPLATE} =  ${slanalyze_render_config}  scope=SUITE

Run Busy Loop Process On Target And Capture PID
  [Documentation]  Start a busy-loop process and a yes process on the target, capture PIDs and process name.
  ${python_pid} =  Start Process On Target And Capture PID  python3 -c "for i in range(10**9): i*i"  python3 -c
  VAR  ${PYTHON_PID} =  ${python_pid}  scope=TEST
  ${proc_name} =  Get Process Command Name On Target  ${PYTHON_PID}
  VAR  ${PROC_NAME} =  ${proc_name}  scope=TEST
  ${yes_pid} =  Start Process On Target And Capture PID  yes > /dev/null  yes > /dev/null
  VAR  ${YES_PID} =  ${yes_pid}  scope=TEST

Run CPU Microarchitecture With System-Wide And Capture Run ID
  [Documentation]  Run cpu_microarchitecture system-wide and capture the run ID in a test variable
  Run CPU Microarchitecture Recipe  --system-wide --timeout 3 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${run_id} =  Extract The Run ID
  The Run Exists  ${run_id}
  VAR  ${RUN_ID} =  ${run_id}  scope=TEST

Run Code Hotspots With System-Wide And Capture Run ID
  [Documentation]  Run code_hotspots system-wide and capture the run ID in a test variable
  Run Code Hotspots Recipe  --system-wide --timeout 3 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${run_id} =  Extract The Run ID
  The Run Exists  ${run_id}
  VAR  ${RUN_ID} =  ${run_id}  scope=TEST

Invoke-Render Is Run With SlAnalyze Filtered By PID
  [Documentation]  Invoke-render slanalyze, symbols, and flat functions with PID filtering.
  [Arguments]  ${flat_renderer_config}=${FLAT_RENDER_CONFIG}
  ${slanalyze_renderer} =  Replace String  ${SLANALYZE_RENDER_CONFIG_TEMPLATE}  __PID__  ${PYTHON_PID}
  ${renderer_configs} =  Catenate  ${slanalyze_renderer}  ${flat_renderer_config}
  Run Invoke Render  ${RUN_ID}  renderer_configs=${renderer_configs}

All Image Names Match The Process Name
  [Documentation]  Validate that at least one image name includes the process name.
  ${query} =  Catenate
  ...  "select i.image_name
  ...  from drilldown as d
  ...  left join symbols as s on s.symbol_id = d.symbol_id
  ...  left join images as i on s.image_id = i.image_id
  ...  where lower(i.image_name) like '%${PROC_NAME}%'"
  ${row_count} =  Count Rows For Render Session Query  ${G_RENDER_SESSION_ID}  ${query}
  ${has_match} =  Evaluate  ${row_count} > 0
  Should Be True  ${has_match}
  ${query} =  Catenate
  ...  "select i.image_name
  ...  from drilldown as d
  ...  left join symbols as s on s.symbol_id = d.symbol_id
  ...  left join images as i on s.image_id = i.image_id
  ...  where lower(i.image_name) = 'yes'"
  ${row_count} =  Count Rows For Render Session Query  ${G_RENDER_SESSION_ID}  ${query}
  ${has_match} =  Evaluate  ${row_count} == 0
  Should Be True  ${has_match}

Get Process Command Name On Target
  [Documentation]  Return the process command name for a PID on the target (lowercased).
  [Arguments]  ${pid}
  Run Target Command  "ps -p ${pid} -o comm="
  The Last Command Succeeded
  ${name} =  Strip String  ${G_LAST_RESULT.stdout}
  ${lower} =  Convert To Lower Case  ${name}
  RETURN  ${lower}
