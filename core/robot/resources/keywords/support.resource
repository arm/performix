# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A resource file containing support keywords for the Arm Performix CLI.
Library         Collections
Library         OperatingSystem
Library         String
Resource        common.resource
Resource        run.resource
Resource        target.resource


*** Variables ***
${SUPPORT_PKG_PATH}    ${EMPTY}
${EXTRACTED_PKG_DIR}   ${TEMPDIR}/apx/support
${EXTRACTED_PKG_PATH}  ${EMPTY}


*** Keywords ***
Support Package Suite Setup
  Common Setup
  Generate Dummy Runs

Support Package Suite Teardown
  Remove Dummy Runs
  Common Teardown

A Support Package Is Collected
  [Arguments]  ${args}=${EMPTY}
  ${pkg_path} =  Collect Support Package  ${args}
  VAR  ${SUPPORT_PKG_PATH}  ${pkg_path}  scope=TEST
  # Determine the file name from the package path (everything after the last path separator and.zip extension removed)
  ${_}  ${zip_name} =  Split Path  ${SUPPORT_PKG_PATH}
  ${zip_name_no_ext} =  Remove String  ${zip_name}  .zip
  VAR  ${EXTRACTED_PKG_PATH}  ${EXTRACTED_PKG_DIR}${/}${zip_name_no_ext}  scope=TEST

The Support Package Is Created Successfully
  [Documentation]  Validate the contents of the extracted support package. Expects logs by default unless overridden.
  [Arguments]  @{runs}  ${expect_logs}=True
  Extract Support Package  ${SUPPORT_PKG_PATH}
  Support Package Contains Expected Contents  @{runs}  expect_logs=${expect_logs}

The Support Package Is Created Successfully Without Logs
  The Support Package Is Created Successfully  expect_logs=False

Collect Support Package
  [Documentation]  Generate a support package and return the archive path.
  [Arguments]  ${args}=${EMPTY}
  Run ATPerf CLI Command  support collect ${args}
  The Last Command Succeeded
  ${output} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${output}  data
  ${pkg_path} =  Get From Dictionary  ${data}  package_path
  Should Not Be Empty  ${pkg_path}
  File Should Exist  ${pkg_path}
  RETURN  ${pkg_path}

Extract Support Package
  [Documentation]  Unzips the specified support package into the extracted package path.
  [Arguments]  ${pkg}
  File Should Exist  ${pkg}
  Create Directory  ${EXTRACTED_PKG_DIR}
  Run Host Command  unzip ${pkg} -d ${EXTRACTED_PKG_DIR}
  The Last Command Succeeded

Remove Support Packages
  [Documentation]  Removes the collected support package zip file and the extracted support package directory.
  Remove Collected Support Package
  Remove Extracted Support Package

Remove Collected Support Package
  [Documentation]  Removes the collected support package zip file.
  Remove File  ${SUPPORT_PKG_PATH}

Remove Extracted Support Package
  [Documentation]  Removes the extracted support package directory.
  Remove Directory  ${EXTRACTED_PKG_DIR}  recursive=True

Support Package Contains Expected Contents
  [Documentation]  Ensure the support package contains expected directories and files.
  [Arguments]  @{runs}  ${expect_logs}=True
  Support Package Contains Metadata
  Support Package Contains Versions
  IF  $expect_logs  Support Package Contains Logs
  IF  $runs  Support Package Contains Runs  @{runs}

Support Package Contains Metadata
  [Documentation]  Validate that the support package contains a metadata.json file.
  File Should Exist  ${EXTRACTED_PKG_PATH}${/}metadata.json
  Support Package Metadata Exists

Support Package Contains Versions
  [Documentation]  Validate that the support package contains a versions.json file.
  File Should Exist  ${EXTRACTED_PKG_PATH}${/}versions.json
  Support Package Versions Exist

Support Package Contains Logs
  [Documentation]  Validate that the support package contains logs for the engine.
  Directory Should Exist  ${EXTRACTED_PKG_PATH}${/}logs
  Directory Should Exist  ${EXTRACTED_PKG_PATH}${/}logs${/}engine
  Directory Should Not Be Empty  ${EXTRACTED_PKG_PATH}${/}logs${/}engine

Support Package Contains Runs
  [Documentation]  Validate that the support package contains the specified runs.
  [Arguments]  @{runs}
  Directory Should Exist  ${EXTRACTED_PKG_PATH}${/}runs
  FOR  ${run_id}  IN  @{runs}
    File Should Exist  ${EXTRACTED_PKG_PATH}${/}runs${/}${run_id}.zip
  END

Support Package Metadata Exists
  [Documentation]  Validate the metadata.json file contains required keys.
  ${metadata} =  Get File  ${EXTRACTED_PKG_PATH}${/}metadata.json
  ${metadata_dict} =  Parse JSON To Dictionary  ${metadata}
  Dictionary Should Contain Key  ${metadata_dict}  generated_at

Support Package Versions Exist
  [Documentation]  Validate the versions.json file contains required keys.
  ${versions} =  Get File  ${EXTRACTED_PKG_PATH}${/}versions.json
  ${versions_dict} =  Parse JSON To Dictionary  ${versions}
  Dictionary Should Contain Key  ${versions_dict}  cli_version
  Dictionary Should Contain Key  ${versions_dict}  engine_version
  Dictionary Should Contain Key  ${versions_dict}  gui_version

Generate Dummy Logs
  [Documentation]  Create dummy log files to ensure the support package has content to collect.
  Generate Dummy Engine Log

Generate Dummy Engine Log
  [Documentation]  Create a dummy engine log file in the log directory. Create the dummy file in the Data Dir as that's
  ...  where APX looks for engine logs to include in the support package.
  ${engine_log_dir} =  Determine Engine Log Directory Path
  VAR  ${DUMMY_ENGINE_LOG}  ${engine_log_dir}${/}2026-Mar-17_09-31-57.log  scope=TEST
  Create File  ${DUMMY_ENGINE_LOG}  This is a dummy engine log.

Remove Dummy Logs
  [Documentation]  Remove the dummy log files.
  Remove File  ${DUMMY_ENGINE_LOG}

Generate Dummy Runs
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  ${run_id} =  Run Recipe And Extract Run ID  code_hotspots
  VAR  ${SUPPORT_PKG_RUN_1_ID}  ${run_id}  scope=SUITE
  The Test Target Is Removed Successfully

Remove Dummy Runs
  Delete Run  ${SUPPORT_PKG_RUN_1_ID}
