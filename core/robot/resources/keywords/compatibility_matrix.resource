# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation  A resource file containing keywords for compatibility matrix tests
Library        Process
Resource       common.resource


*** Variables ***
${UPDATE_SCRIPT}    ${CURDIR}${/}..${/}..${/}..${/}scripts${/}update_matrix.py
${VALIDATE_SCRIPT}  ${CURDIR}${/}..${/}..${/}..${/}scripts${/}validate_matrix.py
${SCHEMA_FILE}      ${CURDIR}${/}..${/}..${/}..${/}compatibility${/}schema.json


*** Keywords ***
Update Compatibility Matrix
  [Documentation]  Runs the compatibility matrix update script.
  [Arguments]  ${matrix_file}  ${new_version}
  Run Host Command  python3 ${UPDATE_SCRIPT} ${matrix_file} ${new_version}

Compatibility Matrix Is Valid
  [Documentation]  Runs the compatibility matrix verification script.
  [Arguments]  ${matrix_file}
  Run Host Command  python3 ${VALIDATE_SCRIPT} ${SCHEMA_FILE} ${matrix_file}

The Update Matrix Command Output Should Contain
  [Arguments]  ${expected}
  Should Contain  ${G_LAST_RESULT.stdout}  ${expected}

Restore Matrix File To Head
  [Arguments]  ${matrix_file}
  Run Host Command  git restore --source HEAD --worktree -- ${matrix_file}
  Should Be Equal As Integers  ${G_LAST_RESULT.rc}  0  msg=git restore failed: ${G_LAST_RESULT.stderr}
