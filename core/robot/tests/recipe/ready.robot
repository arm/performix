# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'recipe ready' CLI of Arm Total Performance.

Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/jitdump.resource
Resource        ../../resources/keywords/recipe.resource
Resource        ../../resources/keywords/run.resource
Resource        ../../resources/keywords/target.resource

Suite Setup     Recipe Ready Suite Setup
Suite Teardown  Recipe Ready Suite Teardown

Test Tags       recipe  ready


*** Test Cases ***
The Code Hotspots Readiness Check Installs Required Tools
  [Documentation]  Tests that a recipe correctly reports as ready when the target is prepared
  ...  and readiness installs its required tools before the first recipe run.
  [Tags]  code-hotspots
  Given The Target Is Unprepared
  When Prepare The Test Target
  Then The Target Is Prepared
  And Check Recipe Is Ready  code_hotspots  --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  And The Recipe Is Ready
  And The Code Hotspots Tool Is Deployed
  And Jitdump-JVM Is Not Deployed
  And Check Recipe Is Ready  code_hotspots  --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  And The Recipe Is Ready
  [Teardown]  Set Ready Suite Default State

The Code Hotspots Recipe Reports As Not Ready When Target Is Not Prepared
  [Documentation]  Tests that a recipe correctly reports as not ready when the target is not prepared.
  [Tags]  code-hotspots
  Given The Code Hotspots Recipe Is Listed
  And The Target Is Unprepared
  When Check Recipe Is Ready  code_hotspots  --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Not Ready
  And Check Advice Messages Contain
  ...  "Arm Performix could not find the launch script for the agent server on the target."
  [Teardown]  Set Ready Suite Default State

The CPU Microarchitecture Recipe Reports As Ready For Java Collection When Jitdump-JVM Is Deployed
  [Documentation]  CPU Microarchitecture reports ready when the Jitdump-JVM is deployed and Java stack collection is enabled.
  [Tags]  cpu-microarchitecture
  [Setup]  Skip Unless CPU Microarchitecture Is Supported On Target
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Is Unprepared
  When Tools Are Deployed For Recipe With Jitdump-JVM Tool Successfully  cpu_microarchitecture
  And Check Recipe Is Ready
  ...  cpu_microarchitecture
  ...  --param collect_java_stacks=true --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Ready
  [Teardown]  Set Ready Suite Default State

The CPU Microarchitecture Recipe Reports As Ready For Dotnet Collection When Dotnet Agent Is Deployed
  [Documentation]  CPU Microarchitecture reports ready when the .NET agent is deployed and .NET stack collection is enabled.
  ...  This is a contract/readiness test that can be enabled early; it does not require .NET symbolization to be working,
  ...  only the readiness gating and deployment checks.
  [Tags]  cpu-microarchitecture  dotnet
  [Setup]  Skip Unless CPU Microarchitecture Is Supported On Target
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Is Unprepared
  When Prepare The Test Target
  And The Target Is Prepared
  And Deploy Tools For Recipe  cpu_microarchitecture  --param collect_dotnet_stacks=true
  And The Output Directory Is Removed From The Target
  And The Dotnet Agent Is Deployed
  And Check Recipe Is Ready
  ...  cpu_microarchitecture
  ...  --param collect_dotnet_stacks=true --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Ready
  [Teardown]  Set Ready Suite Default State

The CPU Microarchitecture Readiness Check Installs Jitdump-JVM When Java Collection Is Enabled
  [Documentation]  CPU Microarchitecture installs Jitdump-JVM when Java stack collection is enabled.
  [Tags]  cpu-microarchitecture
  [Setup]  Run Keywords  Skip Unless CPU Microarchitecture Is Supported On Target
  ...  AND  Skip Unless Target Arch Is  ${ARCH_AARCH64}
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Is Unprepared
  When Tools Are Deployed For Recipe Without Jitdump-JVM Tool Successfully  cpu_microarchitecture
  And Check Recipe Is Ready
  ...  cpu_microarchitecture
  ...  --param collect_java_stacks=true --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Ready
  And Jitdump-JVM Is Deployed
  [Teardown]  Set Ready Suite Default State

The CPU Microarchitecture Readiness Check Installs Dotnet Agent When Dotnet Collection Is Enabled
  [Documentation]  CPU Microarchitecture installs the .NET agent when .NET stack
  ...  collection is enabled. This test does not require .NET
  ...  symbolization to be working, only the readiness gating and deployment checks.
  [Tags]  cpu-microarchitecture  dotnet
  [Setup]  Run Keywords  Skip Unless CPU Microarchitecture Is Supported On Target
  ...  AND  Skip Unless Target Arch Is  ${ARCH_AARCH64}
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Is Unprepared
  When Prepare The Test Target
  And The Target Is Prepared
  And The Output Directory Is Removed From The Target
  And The Dotnet Agent Is Not Deployed
  And Check Recipe Is Ready
  ...  cpu_microarchitecture
  ...  --param collect_dotnet_stacks=true --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Ready
  And The Dotnet Agent Is Deployed
  [Teardown]  Set Ready Suite Default State

The CPU Microarchitecture Recipe Reports An Error For Java Collection On Unsupported Architectures
  [Documentation]  CPU Microarchitecture reports as not ready when Java stack collection is requested on unsupported targets.
  [Tags]  cpu-microarchitecture
  [Setup]  Skip Unless Target Arch Is  ${ARCH_X86_64}
  Given The CPU Microarchitecture Recipe Is Listed
  And The Target Is Unprepared
  When Tools Are Deployed For Recipe Without Jitdump-JVM Tool Successfully  cpu_microarchitecture
  And Check Recipe Is Ready
  ...  cpu_microarchitecture
  ...  --param collect_java_stacks=true --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Not Ready
  And Check Advice Messages Contain  "Java stack collection is only supported on aarch64 targets."
  [Teardown]  Set Ready Suite Default State

The External Code Hotspots Recipe Reports As Ready When Tools Are Deployed
  [Documentation]  Tests that an external recipe correctly reports as ready when the target is prepared
  ...  and its required tools have been deployed.
  [Tags]  external-code-hotspots
  Given The External Code Hotspots Recipe Info Is Listed
  And The Target Is Unprepared
  When Prepare The Test Target
  Then The Target Is Prepared
  And Deploy Tools For Recipe  code_hotspots
  And Jitdump-JVM Is Not Deployed
  And Check External Recipe Is Ready
  ...  code_hotspots_external.js
  ...  --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  And The Recipe Is Ready
  [Teardown]  Set Ready Suite Default State

The External Code Hotspots Recipe Reports As Not Ready When Target Is Not Prepared
  [Documentation]  Tests that an external recipe correctly reports as not ready when the target is not prepared.
  [Tags]  external-code-hotspots
  Given The External Code Hotspots Recipe Info Is Listed
  And The Target Is Unprepared
  When Check External Recipe Is Ready
  ...  code_hotspots_external.js
  ...  --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Not Ready
  And Check Advice Messages Contain
  ...  "Arm Performix could not find the launch script for the agent server on the target."
  [Teardown]  Set Ready Suite Default State

The Instruction Mix Readiness Check Installs Tools In Static Mode
  [Documentation]  Tests that the instruction mix recipe installs the required tool in static mode.
  [Tags]  instruction-mix
  [Setup]  Run Keywords  The Test Target Is Prepared Successfully
  ...  AND  Remove The Instruction Mix Tool Deployment Directory
  Given The Instruction Mix Recipe Is Listed
  And The Target Is Prepared
  And The Instruction Mix Tool Is Not Deployed
  When Check Recipe Is Ready
  ...  instruction_mix
  ...  --param mode=static --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Ready
  [Teardown]  Set Ready Suite Default State

The Instruction Mix Readiness Check Installs Jitdump-JVM In Dynamic Mode
  [Documentation]  Tests that the instruction mix recipe installs Jitdump-JVM in dynamic mode
  ...  and Java stack collection is enabled.
  [Tags]  instruction-mix
  [Setup]  Run Keywords  Skip Unless Target Arch Is  ${ARCH_AARCH64}
  ...  AND  The Test Target Is Prepared Successfully
  ...  AND  Jitdump-JVM Is Not Deployed
  Given The Instruction Mix Recipe Is Listed
  And The Target Is Prepared
  When Check Recipe Is Ready
  ...  instruction_mix
  ...  --param collect_java_stacks=true --param mode=dynamic --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Ready
  And Jitdump-JVM Is Deployed
  [Teardown]  Set Ready Suite Default State

The System Utilization Recipe Reports As Ready When Tools Are Deployed
  [Documentation]  Tests that the System Utilization recipe correctly reports as ready when
  ...  the target is prepared and its required tools have been deployed.
  [Tags]  system-utilization
  [Setup]  Skip Unless System Utilization Is Supported On Target
  Given The System Utilization Recipe Is Listed
  And The Target Is Unprepared
  And Prepare The Test Target
  And The Target Is Prepared
  And Deploy Tools For Recipe  system_utilization  --param interval=0.1
  When Check Recipe Is Ready  system_utilization  --target ${G_TARGET_NAME} --system-wide
  Then The Recipe Is Ready
  [Teardown]  Set Ready Suite Default State

The System Utilization Readiness Check Installs Required Tools
  [Documentation]  Tests that the System Utilization recipe installs required tools on a prepared target.
  [Tags]  system-utilization
  [Setup]  Skip Unless System Utilization Is Supported On Target
  Given The System Utilization Recipe Is Listed
  And The Target Is Unprepared
  And Prepare The Test Target
  And The Target Is Prepared
  When Check Recipe Is Ready  system_utilization  --target ${G_TARGET_NAME} --system-wide
  Then The Recipe Is Ready
  [Teardown]  Set Ready Suite Default State

The System Utilization Recipe Reports As Not Ready On Unsupported OSes
  [Documentation]  Tests that the System Utilization recipe correctly reports as not ready when
  ...  the OS is unsupported (non-Linux)
  [Tags]  system-utilization
  [Setup]  Skip If Target OS Is  ${OS_LINUX}
  Given The System Utilization Recipe Is Listed
  When Check Recipe Is Ready  system_utilization  --target ${G_TARGET_NAME} --system-wide
  Then The Recipe Is Not Ready
  And Check Advice Messages Contain  "Target platform is not supported by the `system_utilization` recipe."
  [Teardown]  Set Ready Suite Default State

Recipe Ready Uses The Correct Target When Multiple Targets Exist
  [Documentation]  Tests that recipe ready uses the correct target when multiple targets exist.
  [Tags]  code-hotspots
  [Setup]  Run Keywords  Remove All Targets
  ...  AND  Create Default Dummy Target  dummy
  ...  AND  The Test Target Is Added Successfully
  ...  AND  Prepare The Test Target If Needed
  ...  AND  Deploy Tools For Recipe  code_hotspots
  ...  AND  Jitdump-JVM Is Not Deployed
  Given The Code Hotspots Recipe Is Listed
  And The Target Is Prepared
  And The Target Is The Default  dummy
  And The Target Is Not The Default  ${G_TARGET_NAME}
  When Check Recipe Is Ready  code_hotspots  --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Ready
  [Teardown]  Run Keywords  Remove All Targets
  ...  AND  Set Ready Suite Default State

The Code Hotspots Recipe Reports As Ready On Localhost
  [Documentation]  Tests that code_hotspots reports as ready on localhost once the target is
  ...  prepared and the required tools have been deployed.
  [Tags]  code-hotspots  localhost
  [Setup]  Run Keywords  Set Localhost As Test Target
  Given The Code Hotspots Recipe Is Listed
  And The Target Is Prepared Successfully  ${G_TARGET_NAME}
  And Deploy Tools For Recipe  code_hotspots
  When Check Recipe Is Ready  code_hotspots  --target ${G_TARGET_NAME} --workload ${LAUNCH_WORKLOAD}
  Then The Recipe Is Ready
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  Restore Remote Test Target
  ...  AND  Set Ready Suite Default State

Recipe Ready Respects Shell Mode
  [Documentation]  Tests that recipe ready doesn't report errors on a bash syntax workload
  [Tags]  code-hotspots
  [Setup]  Run Keywords  Skip Unless Target OS Is  ${OS_LINUX}
  ...  AND  The Test Target Is Prepared Successfully
  ...  AND  Deploy Tools For Recipe  code_hotspots
  Given The Code Hotspots Recipe Is Listed
  And The Target Is Prepared
  When Check Recipe Is Ready With Use Shell
  Then The Recipe Is Ready
  [Teardown]  Set Ready Suite Default State


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.
The Code Hotspots Tool Is Deployed
  [Documentation]  Check that the Code Hotspots profiling tool for the target OS has been deployed.
  IF  '${G_TARGET_OS}' == '${OS_WINDOWS}'
    Check Tool Deployed  wperf.exe  ${G_TEST_USERNAME}
  ELSE
    Check Tool Deployed  sl-record  ${G_TEST_USERNAME}
  END
  The Last Command Succeeded

Recipe Ready Suite Setup
  Common Setup
  Set Ready Suite Default State

Recipe Ready Suite Teardown
  Run Keyword And Ignore Error  The Output Directory Is Removed From The Target
  Run Keyword And Ignore Error  The Target Is Unprepared Successfully
  Run Keyword And Ignore Error  The Test Target Is Removed Successfully
  Common Teardown

Set Ready Suite Default State
  [Documentation]  Set the default state for the `ready` suite; ensure the target exists and is unprepared
  Ensure Target Does Not Exist  ${G_TARGET_NAME}
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  The Test Target Exists
  The Output Directory Is Removed From The Target
  Unprepare The Target
  The Target Is Unprepared

Check Recipe Is Ready With Use Shell
  VAR  ${recipe_args}  --target ${G_TARGET_NAME} --workload "FOO=bar; echo \$FOO" --use-shell  scope=LOCAL
  ${escaped} =  Escape Dollar If Needed  ${recipe_args}
  Check Recipe Is Ready  code_hotspots  ${escaped}
