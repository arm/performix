# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'recipe validate-parameters' CLI of Arm Total Performance.
Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/recipe.resource
Resource        ../../resources/keywords/target.resource
Suite Setup     Recipe Validate Suite Setup
Suite Teardown  Recipe Validate Suite Teardown
Test Tags       recipe  validate


*** Test Cases ***
The Code Hotspots Recipe Successfully Validates Parameters
  [Documentation]  Tests that the code_hotspots recipe validates valid parameter values when a target is provided.
  [Tags]  code_hotspots
  [Setup]  Run Keywords  The Test Target Is Added Successfully
  ...  AND  The Test Target Is Prepared Successfully
  Given The Recipe Is Listed  code_hotspots
  And The Target Exists  ${G_TARGET_NAME}
  When Validate Recipe Parameters  code_hotspots  --target ${G_TARGET_NAME} --param=sampling_freq=high
  And The Last Command Succeeded
  Then The Recipe Parameter Validate Response Has No Messages
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  The Test Target Is Removed Successfully

The Custom Tool Recipe Successfully Validates Parameters
  [Documentation]  Tests that the custom recipe successfully validates parameter values
  [Tags]  custom_tool
  [Setup]  Run Keywords  The Test Target Is Added Successfully
  ...  AND  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  ...  AND  The Test Target Is Prepared Successfully
  ...  AND  Copy The External Test Recipe Into The User Recipe Folder
  Given The Recipe Is Listed  custom_tool_recipe
  And The Target Exists  ${G_TARGET_NAME}
  When Validate Recipe Parameters  custom_tool_recipe  --target ${G_TARGET_NAME} --param=sampling_freq=normal
  And The Last Command Succeeded
  Then The Recipe Parameter Validate Response Has No Messages
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  The Test Target Is Removed Successfully
  ...  AND  Remove The External Test Recipe From The User Recipe Folder

The Custom Tool Recipe Fails To Validate Parameters With Invalid Values
  [Documentation]  Tests that the custom recipe fails when invalid parameter values are used
  [Tags]  custom_tool
  [Setup]  Run Keywords  The Test Target Is Added Successfully
  ...  AND  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  ...  AND  The Test Target Is Prepared Successfully
  ...  AND  Copy The External Test Recipe Into The User Recipe Folder
  Given The Recipe Is Listed  custom_tool_recipe
  And The Target Exists  ${G_TARGET_NAME}
  When Validate Recipe Parameters  custom_tool_recipe  --target ${G_TARGET_NAME} --param=sampling_freq=invalidFreq
  And The Last Command Succeeded
  Then The Recipe Parameter Validate Response Contains  sampling_freq  "tool_integrations.neoprof.PID_NOT_EXIST"
  [Teardown]  Run Keywords  The Target Is Unprepared Successfully
  ...  AND  The Test Target Is Removed Successfully
  ...  AND  Remove The External Test Recipe From The User Recipe Folder

The CPU Microarchitecture Recipe Fails To Validate Parameters Without Target
  [Documentation]  Ensures cpu_microarchitecture validation fails without a target (as valid options cannot be computed)
  [Tags]  cpu_microarchitecture
  Given The Recipe Is Listed  cpu_microarchitecture
  When Validate Recipe Parameters  cpu_microarchitecture  ${EMPTY}
  And The Last Command Failed
  Then The Last Command Failed With Message Code  engine.grpcserver.api_apap.TARGET_REQUIRED


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.
Recipe Validate Suite Setup
  Common Setup

Recipe Validate Suite Teardown
  Common Teardown
