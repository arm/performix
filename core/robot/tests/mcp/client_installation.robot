# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation   End-to-end MCP client installation tests using mock or real client fixtures.
Resource        ../../resources/keywords/mcp/client_installation.resource
Suite Setup     MCP Client Installation Suite Setup
Test Template   MCP Client Registration Lifecycle Should Succeed
Test Tags       mcp  mcp-client-installation


*** Test Cases ***
Antigravity  antigravity  Antigravity
Claude Code  claude-code  Claude Code
Claude Desktop  claude-desktop  Claude Desktop
Codex  codex  Codex
Cursor  cursor  Cursor
VS Code  vscode  VS Code

Install And Uninstall All Supported Clients
  [Template]  NONE
  ${context} =  Given Create All MCP Client Test Context
  ${results} =  When Exercise All MCP Client Operations  ${context}
  Then All MCP Client Operations Should Succeed  ${results}
  [Teardown]  Remove MCP Registration Fixture

Codex Conflict Is Diagnosed
  [Template]  MCP Diagnostic Problem Should Be Reported
  codex  Codex  conflict  conflict  ${CONFIG_CONFLICT}  /unexpected/apx  /unexpected/apx  ${True}

Unreadable Antigravity Configuration Is Diagnosed
  [Template]  MCP Diagnostic Problem Should Be Reported
  antigravity  Antigravity  unreadable  unreadable  ${CONFIG_READ_FAILED}  Could not parse JSON  unexpected end of JSON input  ${True}

Undetected Codex Is Diagnosed
  [Template]  MCP Diagnostic Problem Should Be Reported
  codex  Codex  undetected  not configured  ${CLIENT_UNAVAILABLE}  was not detected  was not detected  ${False}
