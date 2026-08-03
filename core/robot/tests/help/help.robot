# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'help' CLI of Arm Total Performance.

Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/help.resource

Suite Setup     Help Suite Setup
Suite Teardown  Help Suite Teardown

Test Tags       help


*** Test Cases ***
The CLI Help Is Displayed Correctly
  [Documentation]  Test that the CLI displays help instructions correctly.
  [Tags]  disabled
  Given The ATPerf CLI Is Installed
  When Display Help
  Then Help Is Displayed


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.
Help Suite Setup
  Common Setup

Help Suite Teardown
  Common Teardown
