# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A test suite to exercise the compatibility matrix scripts
Resource            ../../resources/keywords/compatibility_matrix.resource
Library             OperatingSystem
Library             Process
Suite Setup         Compatibility Matrix Suite Setup
Suite Teardown      Compatibility Matrix Suite Teardown
Test Tags           compatibility


*** Variables ***
${SAMPLE_MATRICES_DIR}      ${CURDIR}${/}..${/}..${/}resources${/}files${/}compatibility_matrices
${GOOD_MATRIX_FILE}         ${SAMPLE_MATRICES_DIR}${/}good_matrix.json
${BAD_KEY_MATRIX_FILE}      ${SAMPLE_MATRICES_DIR}${/}bad_key_matrix.json
${MISSING_KEY_MATRIX_FILE}  ${SAMPLE_MATRICES_DIR}${/}missing_key_matrix.json
${BREAKING_MATRIX_FILE}     ${SAMPLE_MATRICES_DIR}${/}breaking_matrix.json
${NEW_VERSION}              0.28.0
${NEW_VERSION_1}            0.29.0
${EXISTING_VERSION}         0.27.0
${INVALID_VERSION}          1.0


*** Test Cases ***
The Compatibility Matrix Can Be Updated With A New Version
  [Documentation]  Test updating the compatibility matrix with a new version
  Given Compatibility Matrix Is Valid  ${GOOD_MATRIX_FILE}
  When Update Compatibility Matrix  ${GOOD_MATRIX_FILE}  ${NEW_VERSION}
  Then The Last Command Succeeded
  And Compatibility Matrix Is Valid  ${GOOD_MATRIX_FILE}
  [Teardown]  Restore Matrix File To Head  ${GOOD_MATRIX_FILE}

The Compatibility Matrix Can Be Updated With Multiple New Versions
  [Documentation]  Test updating the compatibility matrix with a new version
  Given Compatibility Matrix Is Valid  ${GOOD_MATRIX_FILE}
  When Update Compatibility Matrix  ${GOOD_MATRIX_FILE}  ${NEW_VERSION}
  And The Last Command Succeeded
  And Update Compatibility Matrix  ${GOOD_MATRIX_FILE}  ${NEW_VERSION_1}
  And The Last Command Succeeded
  Then Compatibility Matrix Is Valid  ${GOOD_MATRIX_FILE}
  [Teardown]  Restore Matrix File To Head  ${GOOD_MATRIX_FILE}

The Compatibility Matrix Can Be Updated When A Recipe Introduced A Breaking Change
  [Documentation]  Test updating the matrix when a recipe has NEXT_VERSION set to indicate breaking
  Given Compatibility Matrix Is Valid  ${BREAKING_MATRIX_FILE}
  When Update Compatibility Matrix  ${BREAKING_MATRIX_FILE}  ${NEW_VERSION}
  Then The Last Command Succeeded
  And Compatibility Matrix Is Valid  ${BREAKING_MATRIX_FILE}
  [Teardown]  Restore Matrix File To Head  ${BREAKING_MATRIX_FILE}

Update Compatibility Matrix With Existing Version
  [Documentation]  Test updating the compatibility matrix with an existing version
  Given Compatibility Matrix Is Valid  ${GOOD_MATRIX_FILE}
  When Update Compatibility Matrix  ${GOOD_MATRIX_FILE}  ${EXISTING_VERSION}
  Then The Last Command Failed
  And The Update Matrix Command Output Should Contain  already exists
  And Compatibility Matrix Is Valid  ${GOOD_MATRIX_FILE}

Update Compatibility Matrix With Invalid Version Format
  [Documentation]  Test updating the compatibility matrix with an invalid version format
  Given Compatibility Matrix Is Valid  ${GOOD_MATRIX_FILE}
  When Update Compatibility Matrix  ${GOOD_MATRIX_FILE}  ${INVALID_VERSION}
  Then The Last Command Failed
  And The Update Matrix Command Output Should Contain  not a valid semantic version
  And Compatibility Matrix Is Valid  ${GOOD_MATRIX_FILE}

Update Compatibility Matrix With Bad Key Names
  [Documentation]  Test updating the compatibility matrix with bad/invalid key names
  Given Compatibility Matrix Is Valid  ${BAD_KEY_MATRIX_FILE}
  When Update Compatibility Matrix  ${BAD_KEY_MATRIX_FILE}  ${NEW_VERSION}
  Then The Last Command Failed
  And The Update Matrix Command Output Should Contain  must contain both 'compatibility' and 'version' keys
  And Compatibility Matrix Is Valid  ${BAD_KEY_MATRIX_FILE}

Update Compatibility Matrix With Missing Key
  [Documentation]  Test updating the compatibility matrix with missing NEXT_VERSION entry/key
  Given Compatibility Matrix Is Valid  ${MISSING_KEY_MATRIX_FILE}
  When Update Compatibility Matrix  ${MISSING_KEY_MATRIX_FILE}  ${NEW_VERSION}
  Then The Last Command Failed
  And The Update Matrix Command Output Should Contain  missing a "NEXT_VERSION" entry
  And Compatibility Matrix Is Valid  ${MISSING_KEY_MATRIX_FILE}


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.
Compatibility Matrix Suite Setup
  Common Setup

Compatibility Matrix Suite Teardown
  Common Teardown
