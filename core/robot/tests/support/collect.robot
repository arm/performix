# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'support collect' CLI of Arm Performix.

Resource        ../../resources/keywords/support.resource

Suite Setup     Support Package Suite Setup
Suite Teardown  Support Package Suite Teardown

Test Tags       support  package


*** Test Cases ***
A Support Package Is Created Successfully When Logs Are Present
  [Documentation]  Ensure a support package is collected and contains the right contents when logs are present.
  [Setup]  Generate Dummy Logs
  Given The ATPerf CLI Is Installed
  When A Support Package Is Collected
  Then The Support Package Is Created Successfully
  [Teardown]  Run Keywords  Remove Dummy Logs
  ...  AND  Remove Support Packages

A Support Package Is Created Successfully When Logs Are Missing
  [Documentation]  Ensure a support package is collected and contains the right contents even when logs are missing.
  ...  This validates that a useful support package is still generated successfully.
  Given The ATPerf CLI Is Installed
  When A Support Package Is Collected
  Then The Support Package Is Created Successfully Without Logs
  [Teardown]  Remove Support Packages

A Support Package Is Created Successfully When Runs Are Provided
  [Documentation]  Ensure a support package is collected and contains the specified runs. Delete the run afterwards
  ...  since we don't really need to keep these for debug.
  Given The ATPerf CLI Is Installed
  When A Support Package Is Collected  ${SUPPORT_PKG_RUN_1_ID}
  Then The Support Package Is Created Successfully  ${SUPPORT_PKG_RUN_1_ID}
  [Teardown]  Remove Support Packages
