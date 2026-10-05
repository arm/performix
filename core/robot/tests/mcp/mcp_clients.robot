# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   Smoke tests for calling Performix MCP tools through coding agents.
Library         OperatingSystem
Library         ../../resources/libs/MCPClientSmoke.py
Library         ../../resources/libs/Terminology.py
Resource        ../../resources/keywords/common.resource
Suite Setup     MCP Client Smoke Suite Setup
Suite Teardown  Close MCP Client Smoke Tests
Test Tags       mcp  mcp-functional


*** Variables ***
${CLI_BIN_DIR}  ${CURDIR}${/}..${/}..${/}..${/}apap-cli


*** Test Cases ***
This Robot Test Suite Covers All Registered MCP Tools
  [Documentation]  Verify that every registered Performix MCP tool has smoke coverage.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Registered MCP Tools Have Smoke Coverage
  Then Should Be True  ${succeeded}

Codex Can Call Add Target
  [Documentation]  Verify that Codex can call add_target with explicit arguments.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Call Add Target
  Then Should Be True  ${succeeded}

Codex Can Call List Targets
  [Documentation]  Verify that Codex can call list_targets and consume its result.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Call List Targets
  Then Should Be True  ${succeeded}

Codex Can Search Target Processes
  [Documentation]  Verify that Codex can call search_target_processes with explicit arguments.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Search Target Processes
  Then Should Be True  ${succeeded}

Codex Can Call List Recipes
  [Documentation]  Verify that Codex can call list_recipes and consume its result.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Call List Recipes
  Then Should Be True  ${succeeded}

Codex Can Call Recipe Info
  [Documentation]  Verify that Codex can call recipe_info with explicit arguments.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Call Recipe Info
  Then Should Be True  ${succeeded}

Codex Can Call Run Recipe
  [Documentation]  Verify that Codex can call run_recipe with explicit arguments.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Call Run Recipe
  Then Should Be True  ${succeeded}

Codex Can Call List Runs
  [Documentation]  Verify that Codex can call list_runs and consume its result.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Call List Runs
  Then Should Be True  ${succeeded}

Codex Can Manage A Render Session
  [Documentation]  Verify the open, list, and close render-session lifecycle.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Manage A Render Session
  Then Should Be True  ${succeeded}

Codex Can Call Run Query
  [Documentation]  Verify that Codex can call run_query with explicit arguments.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Call Run Query
  Then Should Be True  ${succeeded}

Codex Can Generate Curated AI Insights
  [Documentation]  Verify curated insight generation and payload-details retrieval.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Generate Curated AI Insights
  Then Should Be True  ${succeeded}

Codex Can Generate Query-Based AI Insights
  [Documentation]  Verify that query-based insight generation returns run_query guidance.
  Given MCP Client Smoke Tests Are Prepared
  ${succeeded} =  When Codex Can Generate Query-Based AI Insights
  Then Should Be True  ${succeeded}


*** Keywords ***
MCP Client Smoke Suite Setup
  Populate Terms
  Determine Target For Test
  Codex Client Test Is Supported
  ${bin} =  Determine APX Binary Path
  Prepare MCP Client Smoke Tests
  ...  ${bin}
  ...  ${G_TARGET_NAME}
  ...  ${G_TARGET_HOST}
  ...  ${G_TARGET_PORT}
  ...  ${G_TARGET_USER}
  ...  ${G_TARGET_KEY}
  ...  ${OUTPUT DIR}

Codex Client Test Is Supported
  Skip If  '${G_TARGET_OS}' != '${OS_LINUX}'  The Codex client smoke tests currently require a Linux target.

Determine APX Binary Path
  ${host_os} =  Evaluate  platform.system()  platform
  ${suffix} =  Set Variable If  '${host_os}' == 'Windows'  .exe  ${EMPTY}
  ${path} =  Normalize Path  ${CLI_BIN_DIR}${/}${PRODUCT_BINARY_NAME}${suffix}
  File Should Exist  ${path}
  RETURN  ${path}
