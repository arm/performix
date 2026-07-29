# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to verify JIT dump collection with Java and .NET agents enabled.

Resource        ../../resources/keywords/jitdump.resource

Suite Setup     JIT Dump Suite Setup
Suite Teardown  JIT Dump Suite Teardown

Test Tags       jitdump


*** Variables ***
${JIT_DUMP_RECIPE_TIMEOUT}  5


*** Test Cases ***
Code Hotspots System-Wide Captures Java And Dotnet Symbols With Both Agents
  [Documentation]  Run both workloads in the background, collect system-wide with both agents enabled, then verify both symbol sets render.
  [Tags]  code-hotspots  system-wide  java  dotnet
  Given Target Supports JIT Dump Tests
  And Dotnet And Java Workloads Are Running
  When Run Code Hotspots Recipe With JIT Dump Collection  --system-wide  --timeout ${JIT_DUMP_RECIPE_TIMEOUT}
  Then The Run Is Rendered Successfully
  And The Render Produced Dotnet Symbols
  And The Render Produced Java Symbols
  [Teardown]  Stop JIT Dump Workloads

Code Hotspots Attaches To Dotnet PID With Both Agents Enabled
  [Documentation]  Start a .NET workload, then attach code_hotspots with both agents enabled and ensure the run succeeds.
  [Tags]  code-hotspots  attach  dotnet
  Given Target Supports JIT Dump Tests
  And Dotnet Workload Is Running
  When Run Code Hotspots Recipe With JIT Dump Collection  --pid ${DOTNET_PID}  --timeout ${JIT_DUMP_RECIPE_TIMEOUT}
  Then The Run Is Rendered Successfully
  And The Render Produced Dotnet Symbols
  [Teardown]  Stop Dotnet Workload

Code Hotspots Attaches To Java PID With Both Agents Enabled
  [Documentation]  Start a Java workload, then attach code_hotspots with both agents enabled and ensure the run succeeds.
  [Tags]  code-hotspots  attach  java
  Given Target Supports JIT Dump Tests
  And Java Workload Is Running
  When Run Code Hotspots Recipe With JIT Dump Collection  --pid ${JAVA_PID}  --timeout ${JIT_DUMP_RECIPE_TIMEOUT}
  Then The Run Is Rendered Successfully
  And The Render Produced Java Symbols
  [Teardown]  Stop Java Workload

CPU Microarchitecture System-Wide Captures Java And Dotnet Symbols With Both Agents
  [Documentation]  Run both workloads in the background, collect
  ...  system-wide cpu_microarchitecture with both agents enabled, then verify
  ...  both symbol sets render.
  [Tags]  cpu-microarchitecture  system-wide  java  dotnet
  Given Target Supports JIT Dump Tests
  And Dotnet And Java Workloads Are Running
  When Run CPU Microarchitecture Recipe With JIT Dump Collection  --system-wide  --timeout ${JIT_DUMP_RECIPE_TIMEOUT}
  Then The Run Is Rendered Successfully
  And The Render Produced Dotnet Symbols
  And The Render Produced Java Symbols
  [Teardown]  Stop JIT Dump Workloads

CPU Microarchitecture Captures Dotnet Symbols With Both Agents
  [Documentation]  Run cpu_microarchitecture with the .NET workload and verify .NET symbols render.
  [Tags]  cpu-microarchitecture  dotnet
  Given Target Supports JIT Dump Tests
  When Run CPU Microarchitecture Recipe With JIT Dump Collection
  ...  --workload ${DOTNET_WORKLOAD_CMD}  --timeout ${JIT_DUMP_RECIPE_TIMEOUT}
  Then The Run Is Rendered Successfully
  And The Render Produced Dotnet Symbols

CPU Microarchitecture Captures Java Symbols With Both Agents
  [Documentation]  Run cpu_microarchitecture with the Java workload and verify Java symbols render.
  [Tags]  cpu-microarchitecture  java
  Given Target Supports JIT Dump Tests
  When Run CPU Microarchitecture Recipe With JIT Dump Collection
  ...  --workload ${JAVA_WORKLOAD_CMD}
  ...  --timeout ${JIT_DUMP_RECIPE_TIMEOUT}
  Then The Run Is Rendered Successfully
  And The Render Produced Java Symbols
