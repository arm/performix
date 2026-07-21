# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A test suite to exercise the target Agent's Root Worker functionality.
...                 The target Agent has two modes of operation:
...                 1) Agent (Controller) - The Agent that talks to the Engine and runs as non-root user
...                 2) Agent (Root Worker) - The Agent that talks to the Agent (Controller) and runs as root user

Resource            ../../resources/keywords/agent.resource

Suite Setup         Root Worker Suite Setup
Suite Teardown      Root Worker Suite Teardown
Test Teardown       Root Worker Test Teardown

Test Tags           agent  root-worker


*** Test Cases ***
Agent (Controller) Should Fail To Launch Agent (Root Worker)
  [Documentation]  Verify that the Agent (Controller) fails to start the Agent (Root Worker) if the user lacks passwordless `sudo`.
  [Setup]  Run Keywords  Skip Unless Target OS Is  ${OS_LINUX}
  ...  AND  The Test Target With Non-Root User Is Prepared Successfully
  Given The User Lacks Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}
  And The Root Worker Is Not Running On The Target
  And The Test Target With Non-Root User Is The Default Target
  When The Root Worker Is Triggered
  Then The Last Command Failed With Message Code  engine.tool.service.ELEVATE_PRIVILEGES_FAILED
  [Teardown]  Grant User Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}

Agent (Controller) Should Launch As Non-Root
  [Documentation]  Verify that the Agent (Controller) launches as a non-root user.
  [Setup]  Run Keywords  Skip Unless Target OS Is  ${OS_LINUX}
  ...  AND  The Test Target With Non-Root User Is Prepared Successfully
  Given The User Has Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}
  And The Root Worker Is Not Running On The Target
  And The Test Target With Non-Root User Is The Default Target
  When The Target Agent Started Successfully
  Then The Target Agent Is Running As User
  ...  ${G_TARGET_AGENT_CONTROLLER_CMD}
  ...  ${G_TARGET_USER_NON_ROOT}

Agent (Controller) Should Launch Agent (Root Worker) As Root
  [Documentation]  Verify that the Agent (Controller) can start the Agent (Root Worker) as root using passwordless `sudo`.
  [Setup]  Run Keywords  Skip Unless Target OS Is  ${OS_LINUX}
  ...  AND  The Test Target With Non-Root User Is Prepared Successfully
  Given The User Has Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}
  And The Root Worker Is Not Running On The Target
  And The Test Target With Non-Root User Is The Default Target
  When The Root Worker Is Triggered
  Then The Target Agent Is Running As User
  ...  ${G_TARGET_AGENT_ROOTWORKER_CMD}
  ...  root

Agent (Controller) Should Execute Processes Under Itself If AsPrivileged Is Not Requested
  [Documentation]  Verify that the Agent (Controller) executes processes under itself if `AsPrivileged` is not specified in the process configuration.
  [Setup]  Run Keywords  Skip Unless Target OS Is  ${OS_LINUX}
  ...  AND  The Test Target With Non-Root User Is Prepared Successfully
  Given The User Has Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}
  And The Root Worker Is Not Running On The Target
  And The Test Target With Non-Root User Is The Default Target
  When The Root Worker Is Not Triggered
  And The Last Command Succeeded
  Then The Root Worker Is Not Running On The Target

Agent (Controller) Should Execute Processes Under Agent (Root Worker) If AsPrivileged Is Requested
  [Documentation]  Verify that the Agent (Controller) executes processes under root if `AsPrivileged` is specified in the process configuration.
  [Setup]  Run Keywords  Skip Unless Target OS Is  ${OS_LINUX}
  ...  AND  The Test Target With Non-Root User Is Prepared Successfully
  Given The User Has Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}
  And The Root Worker Is Not Running On The Target
  And The Test Target With Non-Root User Is The Default Target
  When The Root Worker Is Triggered
  And The Last Command Succeeded
  Then The Root Worker Is Running On The Target

Agent (Root Worker) Should Be Cleaned Up On Agent (Controller) Shutdown
  [Documentation]  Verify that the Agent (Controller) cleans up all child processes when it is shut down
  ...  Including Agent (Root Worker) and all its child processes.
  [Setup]  Run Keywords  Skip Unless Target OS Is  ${OS_LINUX}
  ...  AND  The Test Target With Non-Root User Is Prepared Successfully
  Given The User Has Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}
  And The Root Worker Is Not Running On The Target
  And The Test Target With Non-Root User Is The Default Target
  And The Target Agent Started Successfully
  When The Root Worker Is Triggered
  And The Last Command Succeeded
  And The Target Agent Stopped Successfully
  Then The Root Worker Is Not Running On The Target
