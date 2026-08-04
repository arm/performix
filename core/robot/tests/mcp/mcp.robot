# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   A test suite for the MCP server and its engine lifecycle.
Library         Collections
Library         OperatingSystem
Library         ../../resources/libs/MCPClient.py
Library         ../../resources/libs/Terminology.py
Suite Setup     MCP Suite Setup
Test Tags       mcp


*** Variables ***
${CLI_BIN_DIR}  ${CURDIR}${/}..${/}..${/}..${/}apap-cli


*** Test Cases ***
The MCP Server Starts And Stops Its Engine
  [Documentation]  Verify that MCP starts an engine, serves an engine-backed tool, and stops the engine when stdin closes.
  ${bin} =  Given Determine APX Binary Path
  ${result} =  When Call MCP Tool And Verify Engine Lifecycle  ${bin}  list_recipes
  Then MCP Result Should Contain Recipe  ${result}  code_hotspots


*** Keywords ***
MCP Suite Setup
  Populate Terms
  ${bin} =  Determine APX Binary Path
  File Should Exist  ${bin}

Determine APX Binary Path
  ${host_os} =  Evaluate  platform.system()  platform
  ${suffix} =  Set Variable If  '${host_os}' == 'Windows'  .exe  ${EMPTY}
  ${path} =  Normalize Path  ${CLI_BIN_DIR}${/}${PRODUCT_BINARY_NAME}${suffix}
  RETURN  ${path}

MCP Result Should Contain Recipe
  [Arguments]  ${result}  ${recipe_name}
  ${recipes} =  Get From Dictionary  ${result}  recipes
  ${expected_recipe} =  Create Dictionary  name=${recipe_name}
  List Should Contain Value  ${recipes}  ${expected_recipe}
