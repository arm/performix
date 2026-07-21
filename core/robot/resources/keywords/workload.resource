# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A resource file containing workload-related keywords for the Arm Total Performance CLI.

Resource            common.resource


*** Keywords ***
Ensure Workload Is Available
  [Documentation]  Ensure the workload is available or skip the test.
  [Arguments]  ${workload_name}
  ${workload_path} =  Get Workload Path  ${workload_name}
  IF  "${workload_path}" == ""
    Skip  Skipping test because workload ${workload_name} was not provided.
  END
  RETURN  ${workload_path}

Verify Workload Path Exists
  [Documentation]  Ensure the workload path exists and matches the expected type.
  [Arguments]  ${workload_path}  ${test_cmd}
  Run Target Command  "ls -l ${workload_path}"
  The Last Command Succeeded
  Run Target Command  "${test_cmd} ${workload_path}"
  The Last Command Succeeded
