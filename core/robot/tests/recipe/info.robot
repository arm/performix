# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'recipe info' CLI of Arm Total Performance.
Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/recipe.resource
Resource        ../../resources/keywords/target.resource
Suite Setup     Recipe Info Suite Setup
Suite Teardown  Recipe Info Suite Teardown
Test Tags       recipe  info


*** Test Cases ***
The CPU Microarchitecture Recipe Info Can Be Listed
  [Documentation]  Tests that the cpu_microarchitecture recipe full definition can be displayed.
  [Tags]  cpu_microarchitecture
  Given The ATPerf CLI Is Installed
  When List Recipe Info  cpu_microarchitecture
  Then The Last Command Succeeded
  And The Recipe Info Contains  title  CPU Microarchitecture

The CPU Microarchitecture Recipe Info Shows Correct Static Options
  [Documentation]  Tests that the cpu_microarchitecture recipe shows static parameter options.
  [Tags]  cpu_microarchitecture
  Given The CPU Microarchitecture Recipe Is Listed
  When List Recipe Info  cpu_microarchitecture
  Then The Last Command Succeeded
  The Recipe Info Parameter Options Contain The Default Static Options

The CPU Microarchitecture Recipe Info Shows Correct Computed Options
  [Documentation]  Tests that the cpu_microarchitecture recipe shows computed parameter options requiring a target.
  [Tags]   cpu_microarchitecture
  [Setup]  Run Keywords  The Test Target Is Added Successfully
  ...  AND  Prepare The Test Target
  Given The ATPerf CLI Is Installed
  When List Recipe Info With Target  cpu_microarchitecture  ${G_TARGET_NAME}
  Then The Last Command Succeeded
  The Recipe Info Parameter Options Contains Basic Metric Groups
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  The Test Target Is Removed Successfully

Recipe Info Uses Correct Target When Multiple Targets Exist
  [Documentation]  Tests that recipe info uses the correct target when multiple targets exist.
  [Setup]  Run Keywords  Remove All Targets
  ...  AND  Create Default Dummy Target  dummy
  ...  AND  The Test Target Is Added Successfully
  ...  AND  Prepare The Test Target
  Given The Target Is The Default  dummy
  And The Target Is Not The Default  ${G_TARGET_NAME}
  When List Recipe Info With Target  cpu_microarchitecture  ${G_TARGET_NAME}
  Then The Last Command Succeeded
  The Recipe Info Parameter Options Contains Basic Metric Groups
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  Remove All Targets

The Code Hotspots Recipe Info Can Be Listed
  [Documentation]  Tests that the code hotspots recipe full definition can be displayed.
  [Tags]  code_hotspots
  Given The ATPerf CLI Is Installed
  When List Recipe Info  code_hotspots
  Then The Last Command Succeeded
  And The Recipe Info Contains  title  Code Hotspots

The Instruction Mix Recipe Info Can Be Listed
  [Documentation]  Tests that the instruction mix recipe full definition can be displayed.
  [Tags]  instruction_mix
  Given The ATPerf CLI Is Installed
  When List Recipe Info  instruction_mix
  Then The Last Command Succeeded
  And The Recipe Info Contains  title  Instruction Mix

The Memory Access Recipe Info Can Be Listed
  [Documentation]  Tests that the memory access recipe full definition can be displayed.
  [Tags]  memory_access
  Given The ATPerf CLI Is Installed
  When List Recipe Info  memory_access
  Then The Last Command Succeeded
  And The Recipe Info Contains  title  Memory Access

The System Utilization Recipe Info Can Be Listed
  [Documentation]  Tests that the system utilization recipe full definition can be displayed.
  [Tags]  system_utilization
  Given The ATPerf CLI Is Installed
  When List Recipe Info  system_utilization
  Then The Last Command Succeeded
  And The Recipe Info Contains  title  System Utilization


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.
Recipe Info Suite Setup
  Common Setup

Recipe Info Suite Teardown
  Common Teardown
