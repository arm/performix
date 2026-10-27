# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation  Keywords for importing pre-recorded runs that match the test's target platform, workload and profiling settings.

Library        ../libs/PrerecordedRuns.py


*** Keywords ***
Resolve Prerecorded Run ID
  [Arguments]  ${logical_run_id}
  ${run_id} =  Resolve Robot Prerecorded Run  ${logical_run_id}  ${G_TARGET_OS}  ${G_TARGET_ARCH}
  RETURN  ${run_id}

Validate Prerecorded Run Metadata
  [Documentation]  Check the pre-recorded run's metadata against the test settings and return the metadata file path.
  [Arguments]  ${logical_run_id}
  ${run_id} =  Resolve Prerecorded Run ID  ${logical_run_id}
  ${metadata_path} =  Join Path  ${PRERECORDED_RUNS_DIR}  ${run_id}  metadata.json
  File Should Exist  ${metadata_path}  msg=Missing metadata for pre-recorded run ${run_id}: ${metadata_path}
  Validate Robot Prerecorded Metadata  ${metadata_path}  ${run_id}  ${LAUNCH_WORKLOAD}
  RETURN  ${metadata_path}

Import Prerecorded Run
  [Documentation]  Import a pre-recorded run by its ID. Fail if it is missing or its metadata does not match the test settings.
  ...  This does not start a live capture.
  [Arguments]  ${logical_run_id}
  ${run_id} =  Resolve Prerecorded Run ID  ${logical_run_id}
  Validate Prerecorded Run Metadata  ${logical_run_id}
  ${archive_path} =  Join Path  ${PRERECORDED_RUNS_DIR}  ${run_id}  latest.zip
  File Should Exist  ${archive_path}  msg=Missing archive for pre-recorded run ${run_id}: ${archive_path}
  Log To Console  Using pre-recorded run: ${run_id}
  Import Run  ${archive_path}
  ${imported_id} =  The Run Is Imported Successfully
  RETURN  ${imported_id}

The Run Info Contains Target
  [Arguments]  ${target_name}  ${logical_run_id}
  IF  '${USE_PRERECORDED_RUNS}' == 'True'
    ${metadata_path} =  Validate Prerecorded Run Metadata  ${logical_run_id}
    ${metadata} =  Read JSON File Into Dictionary  ${metadata_path}
    ${target_name} =  Get From Dictionary  ${metadata}  target
  END
  The Run Info Contains  target_name  ${target_name}
