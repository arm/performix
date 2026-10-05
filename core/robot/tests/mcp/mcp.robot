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
  ${result} =  When Run MCP And Verify Engine Lifecycle  ${bin}  tool_name=list_recipes
  Then MCP Result Should Contain Recipe  ${result}  code_hotspots

The MCP Server Stops Its Engine When Client Pipes Close
  [Documentation]  Verify that a broken stderr pipe cannot stop MCP before stdin EOF triggers engine shutdown.
  ${bin} =  Given Determine Unix APX Binary Path
  ${result} =  When Run MCP And Verify Engine Lifecycle  ${bin}  tool_name=list_recipes  broken_stderr_cleanup=${TRUE}
  Then MCP Result Should Contain Recipe  ${result}  code_hotspots

The MCP Engine Stops When MCP Cannot Run Deferred Cleanup
  [Documentation]  Verify that the engine stops when the client signals MCP's process group and MCP cannot clean it up.
  ${bin} =  Given Determine Unix APX Binary Path
  ${stopped} =  When Run MCP And Verify Engine Lifecycle  ${bin}  interrupt_cleanup=${TRUE}
  Then Should Be True  ${stopped}


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

Determine Unix APX Binary Path
  ${host_os} =  Evaluate  platform.system()  platform
  Skip If  '${host_os}' == 'Windows'  Process-group signalling is Unix-specific.
  ${bin} =  Determine APX Binary Path
  RETURN  ${bin}

MCP Result Should Contain Recipe
  [Arguments]  ${result}  ${recipe_name}
  ${recipes} =  Get From Dictionary  ${result}  recipes
  ${expected_recipe} =  Create Dictionary  name=${recipe_name}
  List Should Contain Value  ${recipes}  ${expected_recipe}
