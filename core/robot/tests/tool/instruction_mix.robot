# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to verify the functionality of the 'instruction_mix' tool integration.

Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/recipe.resource
Resource        ../../resources/keywords/target.resource

Suite Setup     Instruction Mix Suite Setup
Suite Teardown  Instruction Mix Suite Teardown

Test Tags  instruction-mix


*** Test Cases ***
The Instruction Mix Recipe Is Not Ready When The Workload Type Is Not Launch
  [Documentation]  Tests that the instruction mix recipe is not ready with a non-launch workload
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  When Check Recipe Is Ready  instruction_mix  --param mode=static --system-wide --target ${G_TARGET_NAME}
  Then The Last Command Succeeded
  And The Recipe Is Not Ready
  And Check Advice Messages Contain  "You cannot use the `instruction_mix` tool with the workload type `systemWide`."

The Instruction Mix Recipe Is Not Ready When Shell Mode Is Used
  [Documentation]  Tests that the instruction mix recipe is not ready when the --use-shell flag is used
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  When Check Recipe Is Ready  instruction_mix  --param mode=static --workload ls --use-shell --target ${G_TARGET_NAME}
  Then The Last Command Succeeded
  And The Recipe Is Not Ready
  And Check Advice Messages Contain  "You cannot use the `instruction_mix` tool with the `--use-shell` flag."

The Instruction Mix Recipe Fails In Static Mode When The Workload Does Not Exist
  [Documentation]  Tests that the instruction mix recipe fails in static mode with the expected
  ...  message when the specified workload does not exist.
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  When Run Instruction Mix Recipe
  ...  --param mode=static --workload my-made-up-workload --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed With Message Code  tool_integrations.common.WORKLOAD_NOT_EXIST
  And The Target Output Directory Is Empty

The Instruction Mix Recipe Fails In Static Mode When The Workload Type Is Not Launch
  [Documentation]  Tests that the instruction mix recipe fails in static mode with the expected
  ...  message when a non-launch workload is used
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  When Run Instruction Mix Recipe  --param mode=static --system-wide --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed With Message Code  recipes.instruction_mix.NO_WORKLOAD_STATIC
  And The Target Output Directory Is Empty

The Instruction Mix Recipe Fails In Static Mode When Shell Mode Is Used
  [Documentation]  Tests that the instruction mix recipe fails in static mode with the expected
  ...  message when the --use-shell flag is used
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  When Run Instruction Mix Recipe
  ...  --param mode=static --workload ls --use-shell --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Then The Last Command Failed With Message Code  tool_integrations.instruction_mix.USE_SHELL
  And The Target Output Directory Is Empty

The Instruction Mix Recipe Uses The Specified Working Dir In Static Mode
  [Documentation]  Tests that the instruction mix recipe uses the specified working directory
  ...  in static mode. Ideally we would copy an actual disassemblable binary to a temp
  ...  location and verify that the recipe run succeeds, but checking that the error returned
  ...  is not related to a missing workload will do.
  Given The Instruction Mix Recipe Is Listed
  And The Target Exists  ${G_TARGET_NAME}
  And The Script Is Created On The Target  ${TEMP_FILE_PATH}  ls
  When Run Instruction Mix Recipe In Static Mode With Working Dir
  Then The Last Command Did Not Fail With Message Code  tool_integrations.common.WORKLOAD_NOT_EXIST
  And The Target Output Directory Is Empty
  [Teardown]  The File Is Removed From The Target  ${TEMP_FILE_PATH}


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.
Instruction Mix Suite Setup
  Common Setup
  Set Suite Variables
  The Test Target Is Added Successfully
  The Test Target Is Prepared Successfully

Instruction Mix Suite Teardown
  The Test Target Is Removed Successfully
  Common Teardown

Set Suite Variables
  VAR  ${TEMP_FILE_PATH} =  ${ATPERF_DIR}/${TEMP_FILE_NAME}  scope=SUITE

Run Instruction Mix Recipe In Static Mode With Working Dir
  ${instruction_mix_args} =  Catenate  --param mode=static --workload ./${TEMP_FILE_NAME}
  ...  --working-dir ${ATPERF_DIR} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  Run Instruction Mix Recipe  ${instruction_mix_args}
