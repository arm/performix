# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise re-rendering parameter propagation.

Resource        ../../resources/keywords/target.resource
Resource        ../../resources/keywords/run.resource
Resource        ../../resources/keywords/recipe.resource

Suite Setup     Re-Rendering Suite Setup
Suite Teardown  Re-Rendering Suite Teardown

Test Tags  render  re-rendering


*** Variables ***
${RERENDER_RUN_ID}  NONE


*** Test Cases ***
The Prepare Render Response Includes Render Parameters
  [Documentation]  Check that render parameters are passed into PrepareRender and reflected in renderer config.
  Given The Re-Rendering Run Exists
  When Prepare Render Is Run With Params  threshold=10
  Then The Render Parameter Is  threshold  10

The Prepare Render Response Uses Null for Unset Render Parameters
  [Documentation]  Check that omitted render parameter values are set to null.
  Given The Re-Rendering Run Exists
  When Prepare Render Is Run Without Params
  Then The Render Parameter Is  threshold  ${NONE}


*** Keywords ***
Generate Re-Rendering Run
  ${recipe_path} =  Get External Recipe Path  render_params_recipe.js
  ${run_id} =  Run Recipe And Extract Run ID  ${recipe_path}
  VAR  ${RERENDER_RUN_ID} =  ${run_id}  scope=SUITE

The Re-Rendering Run Exists
  The Run Exists  ${RERENDER_RUN_ID}

Prepare Render Is Run With Params
  [Arguments]  ${params}
  ${recipe_path} =  Get External Recipe Path  render_params_recipe.js
  Run ATPerf CLI Command  run prepare-render ${RERENDER_RUN_ID} --recipe ${recipe_path} --param ${params}
  The Last Command Succeeded

Prepare Render Is Run Without Params
  ${recipe_path} =  Get External Recipe Path  render_params_recipe.js
  Run ATPerf CLI Command  run prepare-render ${RERENDER_RUN_ID} --recipe ${recipe_path}
  The Last Command Succeeded

The Render Parameter Is
  [Arguments]  ${param_name}  ${expected}
  ${response} =  Parse Last JSON Stdout Line To Dictionary
  ${data} =  Get From Dictionary  ${response}  data
  ${renderers} =  Get From Dictionary  ${data}  renderers
  ${renderer} =  Get From List  ${renderers}  0
  ${config} =  Get From Dictionary  ${renderer}  config
  ${value} =  Get From Dictionary  ${config}  ${param_name}
  Should Be Equal As Strings  ${value}  ${expected}

Re-Rendering Suite Setup
  Common Setup
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  Generate Re-Rendering Run

Re-Rendering Suite Teardown
  The Test Target Is Removed Successfully
  Common Teardown
