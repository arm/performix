# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   Keywords for JIT dump robot tests.

Resource        ./common.resource
Resource        ./target.resource
Resource        ./recipe.resource
Resource        ./run.resource
Resource        ./render.resource
Resource        ./workload.resource
Resource        ./process.resource


*** Variables ***
${DOTNET_SYMBOL_QUERY}   "select * from symbols where name like '%Hanoi_Solve%'"
${JAVA_SYMBOL_QUERY}     "select * from symbols where NAME='SimpleJavaWork::Work(I)V'"
${DOTNET_WORKLOAD_NAME}  netbench
${DOTNET_WORKLOAD_PATH}  ${EMPTY}
${DOTNET_WORKLOAD_CMD}   ${EMPTY}
${DOTNET_WORKLOAD_ARGS}  123456 --scenario hanoi --duration 100 --threads 8 --marker-every 2048
${DOTNET_WORKLOAD_ENV}   DOTNET_EnableDiagnostics=1 DOTNET_EnableWriteXorExecute=0 DOTNET_JitFramed=1
${JAVA_WORKLOAD_NAME}    simple-java-work
${JAVA_WORKLOAD_PATH}    ${EMPTY}
${JAVA_WORKLOAD_CMD}     ${EMPTY}
${JAVA_WORKLOAD_ARGS}    -DdurationSec=100
${PRESERVE_FRAME_POINTER_FLAG}  -XX:+PreserveFramePointer
${ENABLE_DYNAMIC_AGENT_FLAG}    -XX:+EnableDynamicAgentLoading
${JAVA_FLAGS}            ${PRESERVE_FRAME_POINTER_FLAG} ${ENABLE_DYNAMIC_AGENT_FLAG}


*** Keywords ***
JIT Dump Suite Setup
  Common Setup
  Target Supports JIT Dump Tests
  Setup JIT Dump Workloads
  Setup JIT Dump Target
  Verify JIT Dump Recipes Are Listed

JIT Dump Suite Teardown
  Run Keyword And Ignore Error  The Target Is Unprepared Successfully
  Run Keyword And Ignore Error  The Test Target Is Removed Successfully
  Common Teardown

Setup JIT Dump Workloads
  ${dotnet_path} =  Ensure Workload Is Available  ${DOTNET_WORKLOAD_NAME}
  ${java_path} =  Ensure Workload Is Available  ${JAVA_WORKLOAD_NAME}
  VAR  ${DOTNET_WORKLOAD_PATH} =  ${dotnet_path}  scope=SUITE
  VAR  ${JAVA_WORKLOAD_PATH} =  ${java_path}  scope=SUITE
  Set Dotnet Workload Cmd
  Set Java Workload Cmd

Setup JIT Dump Target
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  The Test Target Is Prepared Successfully

Verify JIT Dump Recipes Are Listed
  The CPU Microarchitecture Recipe Is Listed
  The Code Hotspots Recipe Is Listed

Set Dotnet Workload Cmd
  VAR  ${DOTNET_WORKLOAD_CMD} =
  ...  "env ${DOTNET_WORKLOAD_ENV} ${DOTNET_WORKLOAD_PATH} ${DOTNET_WORKLOAD_ARGS}"
  ...  scope=SUITE

Set Java Workload Cmd
  VAR  ${JAVA_WORKLOAD_CMD} =  "java ${JAVA_FLAGS} ${JAVA_WORKLOAD_ARGS} ${JAVA_WORKLOAD_PATH}"  scope=SUITE

Target Supports JIT Dump Tests
  Skip Unless Target Arch Is  ${ARCH_AARCH64}

Dotnet Workload Is Running
  Verify Dotnet Workload Is Present On Target
  ${pid} =  Start Process On Target And Capture PID  ${DOTNET_WORKLOAD_CMD}  ${DOTNET_WORKLOAD_PATH}
  VAR  ${DOTNET_PID} =  ${pid}  scope=TEST

Java Workload Is Running
  Verify Java Workload Is Present On Target
  ${pid} =  Start Process On Target And Capture PID  ${JAVA_WORKLOAD_CMD}  ${JAVA_WORKLOAD_PATH}
  VAR  ${JAVA_PID} =  ${pid}  scope=TEST

Dotnet And Java Workloads Are Running
  Dotnet Workload Is Running
  Java Workload Is Running

The Run Is Rendered Successfully
  Runs Are Rendered Successfully  ${RUN_ID}

The Render Produced Dotnet Symbols
  ${session} =  Get Variable Value  ${G_RENDER_SESSION_ID}  ${EMPTY}
  The Render Session ID Is Valid  ${session}
  ${row_count} =  Count Rows For Render Session Query  ${session}  ${DOTNET_SYMBOL_QUERY}
  Should Be True  ${row_count} > 0

The Render Produced Java Symbols
  ${session} =  Get Variable Value  ${G_RENDER_SESSION_ID}  ${EMPTY}
  The Render Session ID Is Valid  ${session}
  ${row_count} =  Count Rows For Render Session Query  ${session}  ${JAVA_SYMBOL_QUERY}
  Should Be True  ${row_count} > 0

Stop Dotnet Workload
  ${pid} =  Get Variable Value  ${DOTNET_PID}  ${EMPTY}
  IF  '${pid}' != '${EMPTY}'  Stop Process On Target  ${pid}

Stop Java Workload
  ${pid} =  Get Variable Value  ${JAVA_PID}  ${EMPTY}
  IF  '${pid}' != '${EMPTY}'  Stop Process On Target  ${pid}

Stop JIT Dump Workloads
  Stop Dotnet Workload
  Stop Java Workload

Verify Dotnet Workload Is Present On Target
  Verify Workload Path Exists  ${DOTNET_WORKLOAD_PATH}  test -x

Verify Java Workload Is Present On Target
  Verify Workload Path Exists  ${JAVA_WORKLOAD_PATH}  test -f

Build JIT Dump Params
  ${jit_args} =  Catenate  SEPARATOR=${SPACE}
  ...  --param collect_java_stacks=true
  ...  --param collect_dotnet_stacks=true
  RETURN  ${jit_args}

Run Code Hotspots Recipe With JIT Dump Collection
  [Documentation]  Run Code Hotspots with Java + .NET stack collection enabled.
  [Arguments]  @{extra_args}
  ${args} =  Build Code Hotspots Args  @{extra_args}
  ${jit_args} =  Build JIT Dump Params
  ${full_args} =  Catenate  SEPARATOR=${SPACE}  ${args}  ${jit_args}
  Run Code Hotspots Recipe  ${full_args}
  ${run_id} =  Verify Run Succeeded And Capture Run ID
  VAR  ${RUN_ID} =  ${run_id}  scope=TEST

Run CPU Microarchitecture Recipe With JIT Dump Collection
  [Documentation]  Run cpu_microarchitecture with Java + .NET stack collection enabled.
  [Arguments]  @{extra_args}
  ${args} =  Build CPU Microarchitecture Args  @{extra_args}
  ${jit_args} =  Build JIT Dump Params
  ${full_args} =  Catenate  SEPARATOR=${SPACE}  ${args}  ${jit_args}
  Run CPU Microarchitecture Recipe  ${full_args}
  ${run_id} =  Verify Run Succeeded And Capture Run ID
  VAR  ${RUN_ID} =  ${run_id}  scope=TEST

Verify Run Succeeded And Capture Run ID
  The Last Command Succeeded
  ${run_id} =  Extract The Run ID
  The Run Exists  ${run_id}
  RETURN  ${run_id}

Jitdump-JVM Is Deployed
  [Documentation]  Check that Jitdump-JVM has been deployed to the target (AArch64 only).
  ${target_arch} =  Get Variable Value  ${G_TARGET_ARCH}  ${EMPTY}
  IF  '${target_arch}' != '${ARCH_AARCH64}'
    Log  Skipping jitdump-jvm deployment check for non-AArch64 target.  DEBUG
    RETURN
  END
  Check Tool Deployed  ${JITDUMP_JVM_BINARY_FILE_NAME}  ${G_TEST_USERNAME}
  The Last Command Succeeded

Jitdump-JVM Is Not Deployed
  [Documentation]  Check that Jitdump-JVM has not been deployed to the target (AArch64 only).
  ${target_arch} =  Get Variable Value  ${G_TARGET_ARCH}  ${EMPTY}
  IF  '${target_arch}' != '${ARCH_AARCH64}'
    Log  Skipping jitdump-jvm deployment check for non-AArch64 target.  DEBUG
    RETURN
  END
  Check Tool Not Deployed  ${JITDUMP_JVM_BINARY_FILE_NAME}  ${G_TEST_USERNAME}
  The Last Command Succeeded

Tools Are Deployed For Recipe With Jitdump-JVM Tool Successfully
  [Documentation]  Verifies that tools are deployed successfully and the Jitdump-JVM tool exists on the target.
  [Arguments]  ${recipe}
  Prepare The Test Target
  The Target Is Prepared
  Deploy Tools For Recipe  ${recipe}  --param collect_java_stacks=true
  The Last Command Succeeded
  Jitdump-JVM Is Deployed

Tools Are Deployed For Recipe Without Jitdump-JVM Tool Successfully
  [Documentation]  Verifies that tools are deployed successfully and the Jitdump-JVM tool does not on the target.
  [Arguments]  ${recipe}
  Prepare The Test Target
  The Target Is Prepared
  Deploy Tools For Recipe  ${recipe}
  The Last Command Succeeded
  Jitdump-JVM Is Not Deployed
