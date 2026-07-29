# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite to exercise the 'recipe list' CLI of Arm Total Performance.

Resource        ../../resources/keywords/common.resource
Resource        ../../resources/keywords/recipe.resource
Resource        ../../resources/keywords/target.resource

Suite Setup     Recipe List Suite Setup
Suite Teardown  Recipe List Suite Teardown

Test Tags       recipe  list


*** Test Cases ***
All Default Recipes Are Listed Correctly
  [Documentation]  Tests that all built-in recipes are listed.
  Given The ATPerf CLI Is Installed
  When List All Recipes
  Then The Last Command Succeeded
  And The Default Recipes Are Listed

Recipes In The User Recipe Directory Are Listed Correctly
  [Documentation]  Tests that all external recipes are listed.
  [Setup]  Copy The External Test Recipe Into The User Recipe Folder
  Given The ATPerf CLI Is Installed
  When List All Recipes
  Then The Last Command Succeeded
  And The External Test Recipe Is Listed
  [Teardown]  Remove The External Test Recipe From The User Recipe Folder


*** Keywords ***
# This section is for throwaway keywords that only exist to this test suite.
Recipe List Suite Setup
  Common Setup

Recipe List Suite Teardown
  Common Teardown
