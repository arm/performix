# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise Java stack collection in Arm Total Performance.
Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/target.resource
Resource        ../../resources/keywords/render.resource
Suite Setup     Java Recipe Suite Setup
Suite Teardown  Java Recipe Suite Teardown
Test Tags       java_workload


*** Variables ***
${JAVA_SYMBOL_QUERY}  "select * from symbols where NAME='SimpleJavaWork::Work(I)V'"
${PRESERVE_FRAME_POINTER_FLAG}  -XX:+PreserveFramePointer
${ENABLE_DYNAMIC_AGENT_FLAG}  -XX:+EnableDynamicAgentLoading
${JAVA_FLAGS}  ${PRESERVE_FRAME_POINTER_FLAG} ${ENABLE_DYNAMIC_AGENT_FLAG}
${JITDUMP_JVM_PROCESS_NAME}  jitdump-jvm
${JAVA_WORKLOAD_NAME}  simple-java-work
${JAVA_WORKLOAD_PATH}  ${EMPTY}
${JAVA_WORKLOAD_CMD}  ${EMPTY}
${CPU_MICROARCH_JAVA_ENABLED_RUN_ID}  ${EMPTY}
${CPU_MICROARCH_JAVA_DISABLED_RUN_ID}  ${EMPTY}
${HOTSPOTS_JAVA_ENABLED_RUN_ID}  ${EMPTY}
${HOTSPOTS_JAVA_DISABLED_RUN_ID}  ${EMPTY}
${HOTSPOTS_JAVA_MISSING_FLAGS_RUN_ID}  ${EMPTY}


*** Test Cases ***
CPU Microarchitecture Recipe Produces Java Symbols When Stack Collection Enabled
  [Documentation]  Render the Java-enabled cpu_microarchitecture run and verify Java symbols are produced.
  [Tags]  cpu_microarchitecture
  Given The Run Exists  ${CPU_MICROARCH_JAVA_ENABLED_RUN_ID}
  When Runs Are Rendered Successfully  ${CPU_MICROARCH_JAVA_ENABLED_RUN_ID}
  Then The Render Produced Java Symbols

CPU Microarchitecture Recipe Does Not Produce Java Symbols When Stack Collection Disabled
  [Documentation]  Render the Java-disabled cpu_microarchitecture run and verify Java symbols aren't produced.
  [Tags]  cpu_microarchitecture
  Given The Run Exists  ${CPU_MICROARCH_JAVA_DISABLED_RUN_ID}
  When Runs Are Rendered Successfully  ${CPU_MICROARCH_JAVA_DISABLED_RUN_ID}
  Then The Render Did Not Produce Java Symbols

Code Hotspots Recipe Produces Java Symbols When Stack Collection Enabled
  [Documentation]  Render the Java-enabled Code Hotspots run and verify Java symbols are produced.
  [Tags]  code_hotspots
  Given The Run Exists  ${HOTSPOTS_JAVA_ENABLED_RUN_ID}
  When Runs Are Rendered Successfully  ${HOTSPOTS_JAVA_ENABLED_RUN_ID}
  Then The Render Produced Java Symbols

Code Hotspots Recipe Does Not Produce Java Symbols When Stack Collection Disabled
  [Documentation]  Render the Java-disabled Code Hotspots run and verify Java symbols aren't produced.
  [Tags]  code_hotspots
  Given The Run Exists  ${HOTSPOTS_JAVA_DISABLED_RUN_ID}
  When Runs Are Rendered Successfully  ${HOTSPOTS_JAVA_DISABLED_RUN_ID}
  Then The Render Did Not Produce Java Symbols

Java Workload Missing Flags Emit User Messages
  [Documentation]  Verify that omitting recommended JVM flags emits user messages.
  [Tags]  code_hotspots  user_messages
  Given The Run Exists  ${HOTSPOTS_JAVA_MISSING_FLAGS_RUN_ID}
  When Render User Messages  ${HOTSPOTS_JAVA_MISSING_FLAGS_RUN_ID}
  Then The Render Produced JVM User Messages

Java Workload With Required Flags Emits No User Message
  [Documentation]  Verify that no user messages are emitted when the recommended flags are used.
  [Tags]  code_hotspots  user_messages
  Given The Run Exists  ${HOTSPOTS_JAVA_ENABLED_RUN_ID}
  When Render User Messages  ${HOTSPOTS_JAVA_ENABLED_RUN_ID}
  Then The Render Produced No JVM User Messages

Jitdump-JVM Is Killed For A Successful Run
  [Documentation]  For a successful Java-enabled Code Hotspots run, verify Jitdump-JVM is killed.
  [Tags]  code_hotspots
  Given The Code Hotspots Recipe Is Listed
  When Run Code Hotspots Recipe
  ...  --timeout 5 --workload ${JAVA_WORKLOAD_CMD} --param collect_java_stacks=true --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  And The Last Command Succeeded
  Then Target Process Is Not Running  ${JITDUMP_JVM_PROCESS_NAME}

Jitdump-JVM Is Killed For A Failed Run
  [Documentation]  For a failed Java-enabled Code Hotspots run, verify Jitdump-JVM is killed.
  [Tags]  code_hotspots
  Given The Code Hotspots Recipe Is Listed
  When Run Code Hotspots Recipe
  ...  --timeout 5 --workload non_existent_workload --param collect_java_stacks=true --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  And The Last Command Failed With Message Code  tool_integrations.common.WORKLOAD_NOT_EXIST_OR_NOT_EXECUTABLE
  Then Target Process Is Not Running  ${JITDUMP_JVM_PROCESS_NAME}

Jitdump-JVM Is Killed For A Stopped Run
  [Documentation]  For a stopped Java-enabled Code Hotspots run, verify the Jitdump-JVM is killed.
  [Tags]  code_hotspots  stop
  Given No Processes Are Running On The Target  ${JITDUMP_JVM_PROCESS_NAME}  match_on_proc_name=${True}
  And Start Code Hotspots Recipe
  ...  --timeout 5 --workload ${JAVA_WORKLOAD_CMD} --param collect_java_stacks=true --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  And Wait For Workload To Run On The Target  ${JITDUMP_JVM_PROCESS_NAME}  match_on_proc_name=${True}
  When Stop Run  ${G_LAST_PROCESS}
  Then No Processes Are Running On The Target  ${JITDUMP_JVM_PROCESS_NAME}  match_on_proc_name=${True}

Jitdump-JVM Is Killed For A Cancelled Run
  [Documentation]  For a cancelled Java-enabled Code Hotspots run, verify the Jitdump-JVM is killed.
  [Tags]  code_hotspots  cancel
  Given No Processes Are Running On The Target  ${JITDUMP_JVM_PROCESS_NAME}  match_on_proc_name=${True}
  And Start Code Hotspots Recipe
  ...  --timeout 5 --workload ${JAVA_WORKLOAD_CMD} --param collect_java_stacks=true --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  And Wait For Workload To Run On The Target  ${JITDUMP_JVM_PROCESS_NAME}  match_on_proc_name=${True}
  When Cancel Run  ${G_LAST_PROCESS}
  Then No Processes Are Running On The Target  ${JITDUMP_JVM_PROCESS_NAME}  match_on_proc_name=${True}


*** Keywords ***
Java Recipe Suite Setup
  Common Setup
  Ensure Java Workload Is Available
  Set Java Workload Cmd
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  The Test Target Is Prepared Successfully
  The CPU Microarchitecture Recipe Is Listed
  The Code Hotspots Recipe Is Listed
  Generate Java Runs

Java Recipe Suite Teardown
  Run Keyword And Ignore Error  The Target Is Unprepared Successfully
  Run Keyword And Ignore Error  The Test Target Is Removed Successfully
  Common Teardown

Set Java Workload Cmd
  [Documentation]  Sets the Java workload command variable.
  VAR  ${JAVA_WORKLOAD_CMD}  "java ${JAVA_FLAGS} ${JAVA_WORKLOAD_PATH}"  scope=SUITE

Ensure Java Workload Is Available
  [Documentation]  Ensure the Java workload is prepared on the target or skip the suite.
  ${workload_path} =  Get Workload Path  ${JAVA_WORKLOAD_NAME}
  IF  "${workload_path}" == ""
    Skip  Skipping test because workload ${JAVA_WORKLOAD_NAME} was not provided.
  END
  VAR  ${JAVA_WORKLOAD_PATH}  ${workload_path}  scope=SUITE

Generate Java Runs
  [Documentation]  Generate Java workload runs.
  Generate CPU Microarchitecture Java Runs
  Generate Code Hotspots Java Runs

Generate CPU Microarchitecture Java Runs
  [Documentation]  Generate CPU Microarchitecture Java workload runs when the target supports the recipe.
  ${is_supported} =  CPU Microarchitecture Is Supported On Target
  IF  not ${is_supported}  RETURN
  ${enabled} =  Run Java CPU Microarchitecture Recipe  collect=true
  VAR  ${CPU_MICROARCH_JAVA_ENABLED_RUN_ID}  ${enabled}  scope=SUITE
  ${disabled} =  Run Java CPU Microarchitecture Recipe  collect=false
  VAR  ${CPU_MICROARCH_JAVA_DISABLED_RUN_ID}  ${disabled}  scope=SUITE

Generate Code Hotspots Java Runs
  [Documentation]  Generate Code Hotspots Java workload runs.
  ${hotspots_enabled} =  Run Java Hotspots Recipe  collect=true
  VAR  ${HOTSPOTS_JAVA_ENABLED_RUN_ID}  ${hotspots_enabled}  scope=SUITE
  ${hotspots_disabled} =  Run Java Hotspots Recipe  collect=false
  VAR  ${HOTSPOTS_JAVA_DISABLED_RUN_ID}  ${hotspots_disabled}  scope=SUITE
  ${hotspots_missing_flags} =  Run Java Hotspots Recipe With Flags  collect=true  flags=${EMPTY}
  VAR  ${HOTSPOTS_JAVA_MISSING_FLAGS_RUN_ID}  ${hotspots_missing_flags}  scope=SUITE

Run Java CPU Microarchitecture Recipe
  [Documentation]  Run the cpu_microarchitecture recipe with the Java workload, specifying whether to collect Java stacks.
  [Arguments]  ${collect}
  Run CPU Microarchitecture Recipe
  ...  --timeout 5 --workload ${JAVA_WORKLOAD_CMD} --param collect_java_stacks=${collect} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${run_id} =  Extract The Run ID
  The Run Exists  ${run_id}
  RETURN  ${run_id}

Run Java Hotspots Recipe
  [Documentation]  Run the Code Hotspots recipe with the Java workload, specifying whether to collect Java stacks.
  [Arguments]  ${collect}
  ${run_id} =  Run Java Hotspots Recipe With Flags  ${collect}  ${JAVA_FLAGS}
  RETURN  ${run_id}

Run Java Hotspots Recipe With Flags
  [Documentation]  Run the Code Hotspots recipe with the Java workload, overriding the JVM flags if desired.
  [Arguments]  ${collect}=${True}  ${flags}=${JAVA_FLAGS}
  VAR  ${workload_cmd}  "java ${flags} ${JAVA_WORKLOAD_PATH}"  scope=LOCAL
  Run Code Hotspots Recipe
  ...  --timeout 5 --workload ${workload_cmd} --param collect_java_stacks=${collect} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${run_id} =  Extract The Run ID
  The Run Exists  ${run_id}
  RETURN  ${run_id}

The Render Produced Java Symbols
  [Documentation]  Verify that the most recently rendered Java workload produced Java symbols.
  ${session} =  Get Variable Value  ${G_RENDER_SESSION_ID}  ${EMPTY}
  The Render Session ID Is Valid  ${session}
  ${row_count} =  Count Rows For Render Session Query  ${session}  ${JAVA_SYMBOL_QUERY}
  Should Be True  ${row_count} > 0

The Render Did Not Produce Java Symbols
  [Documentation]  Verify that the most recently rendered Java workload did not produce Java symbols.
  ${session} =  Get Variable Value  ${G_RENDER_SESSION_ID}  ${EMPTY}
  The Render Session ID Is Valid  ${session}
  ${row_count} =  Count Rows For Render Session Query  ${session}  ${JAVA_SYMBOL_QUERY}
  Should Be Equal As Integers  ${row_count}  0

The Render Produced JVM User Messages
  [Documentation]  Verify that the most recently rendered session produced JVM user messages.
  ${session} =  Get Variable Value  ${G_RENDER_SESSION_ID}  ${EMPTY}
  The Render Session ID Is Valid  ${session}
  ${row_count} =  Count Rows For Render Session Query  ${session}  "select * from log WHERE message LIKE '%${PRESERVE_FRAME_POINTER_FLAG}%'"
  Should Be Equal As Integers  ${row_count}  1
  ${row_count} =  Count Rows For Render Session Query  ${session}  "select * from log WHERE message LIKE '%${ENABLE_DYNAMIC_AGENT_FLAG}%'"
  Should Be Equal As Integers  ${row_count}  1

The Render Produced No JVM User Messages
  [Documentation]  Verify that the most recently rendered session did not produce JVM user messages.
  ${session} =  Get Variable Value  ${G_RENDER_SESSION_ID}  ${EMPTY}
  The Render Session ID Is Valid  ${session}
  ${row_count} =  Count Rows For Render Session Query  ${session}  "select * from log WHERE message LIKE '%${PRESERVE_FRAME_POINTER_FLAG}%'"
  Should Be Equal As Integers  ${row_count}  0
  ${row_count} =  Count Rows For Render Session Query  ${session}  "select * from log WHERE message LIKE '%${ENABLE_DYNAMIC_AGENT_FLAG}%'"
  Should Be Equal As Integers  ${row_count}  0
