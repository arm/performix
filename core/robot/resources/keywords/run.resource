# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation  A resource file containing 'run' keywords for the Arm Total Performance CLI.

Resource       common.resource
Resource       recipe.resource


*** Variables ***
${G_NUM_RUNS}       ${EMPTY}  # Global variable to track last known number of runs
${EXPORT_RUNS}      False


*** Keywords ***
List Runs
  Run ATPerf CLI Command  run list

Count Runs
  [Documentation]  Counts the number of runs in the runs list.
  ${runs} =  Get Runs As List
  ${num_runs} =  Get Length  ${runs}
  VAR  ${G_NUM_RUNS} =  ${num_runs}  scope=GLOBAL

The Number Of Runs Is
  [Arguments]  ${expected}
  Count Runs
  Should Be Equal As Integers  ${expected}  ${G_NUM_RUNS}

The Number Of Runs Is At Least
  [Arguments]  ${expected}
  Count Runs
  ${is_at_least} =  Evaluate  ${expected} >= ${G_NUM_RUNS}
  Should Be True  ${is_at_least}

The Number Of Runs Has Increased By
  [Documentation]  Checks the number of runs has increased by the specified amount, by taking
  ...  note of the old count, re-counting the number of runs, and finding the difference.
  [Arguments]  ${expected}
  VAR  ${old_num} =  ${G_NUM_RUNS}  scope=LOCAL
  Count Runs
  VAR  ${new_num} =  ${G_NUM_RUNS}  scope=LOCAL
  ${increase} =  Evaluate  ${new_num} - ${old_num}
  Should Be Equal As Integers  ${increase}  ${expected}

The Number Of Runs Has Decreased By
  [Documentation]  Checks the number of runs has decreased by the specified amount, by taking
  ...  note of the old count, re-counting the number of runs, and finding the difference.
  [Arguments]  ${expected}
  VAR  ${old_num} =  ${G_NUM_RUNS}  scope=LOCAL
  Count Runs
  VAR  ${new_num} =  ${G_NUM_RUNS}  scope=LOCAL
  ${decrease} =  Evaluate  ${old_num} - ${new_num}
  Should Be Equal As Integers  ${decrease}  ${expected}

There Are No Runs
  List Runs
  The Number Of Runs Is  0

Rename Run
  [Arguments]  ${id}  ${new_name}
  Run ATPerf CLI Command  run rename ${id} ${new_name}

Delete Run
  [Arguments]  ${id}
  Run ATPerf CLI Command  run delete ${id}

The Run Is Deleted Successfully
  [Documentation]  Delete a run and then check it has been deleted successfully.
  [Arguments]  ${id}
  Count Runs
  Delete Run  ${id}
  The Last Command Succeeded
  The Number Of Runs Has Decreased By  1
  The Run Does Not Exist  ${id}

The Run Exists
  [Documentation]  Confirms a run exists by inspecting the run list and the on-disk contents.
  [Arguments]  ${id}
  VAR  ${found} =  False
  ${runs} =  Get Runs As List
  FOR  ${run}  IN  @{runs}
    IF  "${run['id']}" == "${id}"  VAR  ${found} =  True
  END
  Should Be True  ${found}
  Directory Should Not Be Empty  ${G_RUNS_DIR}${/}${id}

The Run Does Not Exist
  [Documentation]  Confirms a run doesn't exist by inspecting the run list and the on-disk contents.
  [Arguments]  ${id}
  VAR  ${found} =  False
  ${runs} =  Get Runs As List
  FOR  ${run}  IN  @{runs}
    IF  "${run['id']}" == "${id}"  VAR  ${found} =  True
  END
  Should Not Be True  ${found}
  Directory Should Not Exist  ${G_RUNS_DIR}${/}${id}

Delete All Runs
  [Documentation]  Deletes all runs using the CLI --all flag and auto-confirms the prompt.
  ${bin} =  Determine ATPerf CLI Bin Path
  ${cmd} =  Catenate  ${bin} run delete --all ${JSON_FLAG}
  ${full_cmd} =  Catenate  SEPARATOR=${EMPTY}  echo yes \| ${cmd}
  Run Host Command  ${full_cmd}

All Runs Are Deleted Successfully
  Delete All Runs
  The Last Command Succeeded
  Directory Should Not Be Empty  ${G_RUNS_DIR}

Update Run Host Source Code
  [Arguments]  ${id}
  Run ATPerf CLI Command  run update --source "/foo/bar:/baz" ${id}

Display Run Info
  [Arguments]  ${id}
  Run ATPerf CLI Command  run info ${id}

The Run Info Contains
  [Documentation]  Check that the run info contains the expected value for a field.
  [Arguments]  ${field}  ${expected}
  ${result} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${result}  data
  Dictionary Should Contain Item  ${data}  ${field}  ${expected}

Recommend Run
  [Arguments]  ${id}
  Run ATPerf CLI Command  run recommend ${id}

Export Run
  [Arguments]  ${id}  ${path}
  Run ATPerf CLI Command  run export ${id} ${path}

Export All Runs
  [Documentation]  Export all the runs in the ${G_RUNS_DIR} to individual zip
  ...  files so they can be collected by CI and imported later for debug.
  ...  Only exports runs if EXPORT_RUNS variable has been set on the CLI.
  IF  ${EXPORT_RUNS}
    Log  Exporting all runs to ${ROBOT_EXPORT_DIR}
    ${runs} =  Get Runs As List
    FOR  ${run}  IN  @{runs}
      ${id} =  Get From Dictionary  ${run}  id
      ${export_path} =  Normalize Path  ${ROBOT_EXPORT_DIR}
      Export Run  ${id}  ${export_path}
    END
  ELSE
    Log  Skipping export of runs as not running in CI environment
    RETURN
  END

The Run Is Exported Successfully
  [Arguments]  ${id}  ${path}
  File Should Exist  ${path}${/}${id}.zip

Import Run
  [Documentation]  Imports the run at the specified path into the run list.
  [Arguments]  ${path}
  Run ATPerf CLI Command  run import ${path}
  ${runs} =  Get Runs As List
  ${new_id} =  Get From Dictionary  ${runs}  new_id
  ${id} =  Get From Dictionary  ${new_id}  value
  RETURN  ${id}

The Run Is Imported Successfully
  [Arguments]  ${id}
  The Run Exists  ${id}

Get Runs As List
  [Documentation]  Lists all runs and returns the JSON data as a list.
  List Runs
  ${dict} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${dict}  data
  IF  not ${data}  RETURN  ${EMPTY}
  ${runs} =  Get From Dictionary  ${data}  runs
  RETURN  ${runs}

Get Run Details As Dictionary
  [Documentation]  Returns a specific run as a dictionary.
  [Arguments]  ${id}
  ${runs} =  Get Runs As List
  ${run} =  Get From Dictionary  ${runs}  ${id}
  RETURN  ${run}

The Run Details Contains
  [Documentation]  Check that the run details for a particular run contains the specified key/value pair.
  [Arguments]  ${id}  ${field}  ${expected_value}
  ${run} =  Get Run Details As Dictionary  ${id}
  Dictionary Should Contain Item  ${run}  ${field}  ${expected_value}

Extract The Run ID
  [Documentation]  Extract the run ID from last command's stdout, following a recipe run invocation.
  ${run_info} =  Parse Last JSON Stdout Line To Dictionary
  ${data} =  Get From Dictionary  ${run_info}  data
  ${run_id} =  Get From Dictionary  ${data}  run_id
  ${run_id_value} =  Get From Dictionary  ${run_id}  value
  RETURN  ${run_id_value}

Run Code Hotspots Recipe And Extract Run ID
  [Documentation]  Run code_hotspots recipe against the test target and return a run ID
  The Code Hotspots Recipe Is Listed
  Run Code Hotspots Recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${id} =  Extract The Run ID
  The Run Exists  ${id}
  RETURN  ${id}

Run CPU Microarchitecture Recipe And Extract Run ID
  [Documentation]  Run cpu_microarchitecture recipe against the test target and return a run ID
  Run CPU Microarchitecture Recipe  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${id} =  Extract The Run ID
  The Run Exists  ${id}
  RETURN  ${id}

Run Memory Access Recipe And Extract Run ID
  [Documentation]  Run memory access recipe against the test target and return a run ID
  Run Memory Access Recipe  --workload "ls -la" --timeout 1 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${id} =  Extract The Run ID
  The Run Exists  ${id}
  RETURN  ${id}

Run System Utilization Recipe And Extract Run ID
  [Documentation]  Run system utilization recipe against the test target and return a run ID
  Run System Utilization Recipe
  ...  --workload ${LAUNCH_WORKLOAD} --param=interval=0.1 --timeout 1 --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${id} =  Extract The Run ID
  The Run Exists  ${id}
  RETURN  ${id}

Run Instruction Mix Recipe In Dynamic Mode And Extract Run ID
  [Documentation]  Run instruction mix recipe in dynamic mode against the test target
  ...  and return a run ID
  Run Instruction Mix Recipe
  ...  --workload ${LAUNCH_WORKLOAD} --param mode=dynamic --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${id} =  Extract The Run ID
  The Run Exists  ${id}
  RETURN  ${id}

Run Instruction Mix Recipe In Static Mode And Extract Run ID
  [Documentation]  Run instruction mix recipe in static mode against the test target
  ...  and return a run ID
  Run Instruction Mix Recipe
  ...  --workload ${LAUNCH_WORKLOAD} --param mode=static --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${id} =  Extract The Run ID
  The Run Exists  ${id}
  RETURN  ${id}

Run Recipe And Extract Run ID
  [Documentation]  Run a recipe against the test target and return a run ID.
  [Arguments]  ${recipe}
  Run Recipe  ${recipe}  --workload ${LAUNCH_WORKLOAD} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${id} =  Extract The Run ID
  The Run Exists  ${id}
  RETURN  ${id}

Deploy Tools For Recipe
  [Documentation]  Deploy the required tools for a recipe by running it with --deploy-tools and removing the generated run.
  ...  TODO: Right now the only way to deploy recipe tools is to run it. Refactor after/with APAP-2506.
  [Arguments]  ${recipe_name}  ${args}=${EMPTY}
  ${timeout} =  Get Default Recipe Timeout  1
  Run Recipe
  ...  ${recipe_name}
  ...  ${args} --workload ${LAUNCH_WORKLOAD} --timeout ${timeout} --target ${G_TARGET_NAME} ${DEPLOY_TOOLS_FLAG}
  The Last Command Succeeded
  ${run_id} =  Extract The Run ID
  Delete Run  ${run_id}
  The Last Command Succeeded
