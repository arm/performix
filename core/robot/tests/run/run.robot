# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'run' CLI of Arm Total Performance.

Resource        ../../resources/keywords/recipe.resource
Resource        ../../resources/keywords/run.resource
Resource        ../../resources/keywords/target.resource

Suite Setup     Runs Suite Setup
Suite Teardown  Runs Suite Teardown

Test Tags  run


*** Test Cases ***
### List ###

All Runs Can Be Listed
  [Documentation]  Tests that all runs can be listed.
  [Tags]  list  disabled
  # TODO - test finished but hangs on windows, investigate
  Given The ATPerf CLI Is Installed
  When List Runs
  Then The Last Command Succeeded

### Info ###

Run Info Can Be Display For A Run That Exists
  [Documentation]  Tests that an existing run's full details can be displayed.
  [Tags]  info
  Given The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  When Display Run Info  ${CODE_HOTSPOTS_RUN_ID_1}
  Then The Last Command Succeeded
  And The Run Info Contains  target_name  ${G_TARGET_NAME}

Run Info Cannot Be Displayed For A Run That Does Not Exist
  [Documentation]  Tests that a non-existing run's full details cannot be displayed.
  [Tags]  info
  Given The Run Does Not Exist  this_run_doesnt_exist
  When Display Run Info  this_run_doesnt_exist
  Then The Last Command Failed

### Update ###

Run Update Can Update The Host Source Code Path Updated For A Run That Exists
  [Documentation]  Tests that an existing run can have its host source code path updated.
  [Tags]  update
  Given The Run Exists  ${CODE_HOTSPOTS_RUN_ID_1}
  When Update Run Host Source Code  ${CODE_HOTSPOTS_RUN_ID_1}
  Then The Last Command Succeeded

Run Update Cannot Update The Host Source Code Path For A Run That Does Not Exists
  [Documentation]  Tests that a non-existing run cannot have its host source code path updated.
  [Tags]  update
  Given The Run Does Not Exist  this_run_doesnt_exist
  When Update Run Host Source Code  this_run_doesnt_exist
  Then The Last Command Failed

### Rename ###

A Run Can Be Renamed If It Exists
  [Documentation]  Tests that an existing run can be renamed.
  [Tags]  rename  disabled
  Given The ATPerf CLI Is Installed
  When The ATPerf CLI Is Installed
  Then The ATPerf CLI Is Installed

A Run Cannot Be Renamed If It Does Not Exist
  [Documentation]  Tests that a non-existing run cannot be renamed.
  [Tags]  rename  disabled
  Given The ATPerf CLI Is Installed
  When The ATPerf CLI Is Installed
  Then The ATPerf CLI Is Installed

### Delete ###

A Run Can Be Deleted If It Exists
  [Documentation]  Tests that an existing run can be deleted.
  [Tags]  delete  disabled
  Given The ATPerf CLI Is Installed
  When The ATPerf CLI Is Installed
  Then The ATPerf CLI Is Installed

A Run Cannot Be Deleted If It Does Not Exist
  [Documentation]  Tests that a non-existing run cannot be deleted.
  [Tags]  delete  disabled
  Given The ATPerf CLI Is Installed
  When The ATPerf CLI Is Installed
  Then The ATPerf CLI Is Installed

### Render ###

A Valid Run Can Be Rendered
  [Documentation]  Tests that an existing run can be rendered.
  [Tags]  render  disabled
  Given The ATPerf CLI Is Installed
  When The ATPerf CLI Is Installed
  Then The ATPerf CLI Is Installed

An Invalid Run Cannot Be Rendered
  [Documentation]  Tests that a non-existing run cannot be rendered.
  [Tags]  render  disabled
  Given The ATPerf CLI Is Installed
  When The ATPerf CLI Is Installed
  Then The ATPerf CLI Is Installed

The Last Run Can Be Rendered
  [Documentation]  Tests that the most recent run can be rendered.
  [Tags]  render  disabled
  Given The ATPerf CLI Is Installed
  When The ATPerf CLI Is Installed
  Then The ATPerf CLI Is Installed

### Recommend ###

# TODO

### Export ###

A Valid Run Can Be Exported
  [Documentation]  Tests that an existing run can be exported.
  [Tags]  export  disabled
  Given The ATPerf CLI Is Installed
  When The ATPerf CLI Is Installed
  Then The ATPerf CLI Is Installed

An Invalid Run Cannot Be Exported
  [Documentation]  Tests that a non-existing run cannot be exported.
  [Tags]  export  disabled
  Given The ATPerf CLI Is Installed
  When The ATPerf CLI Is Installed
  Then The ATPerf CLI Is Installed

### Import ###

A Run Can Be Imported
  [Documentation]  Tests that a run can be imported from another location.
  [Tags]  import  disabled
  Given The ATPerf CLI Is Installed
  When The ATPerf CLI Is Installed
  Then The ATPerf CLI Is Installed


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.

Runs Suite Setup
  Common Setup
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  ${run_id} =  Run Recipe And Extract Run ID  code_hotspots
  VAR  ${CODE_HOTSPOTS_RUN_ID_1} =  ${run_id}  scope=SUITE
  The Test Target Is Removed Successfully

Runs Suite Teardown
  Common Teardown
