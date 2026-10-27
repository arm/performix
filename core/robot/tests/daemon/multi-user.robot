# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       Linux remote-localhost tests for per-user engine daemon isolation.

Resource            ../../resources/keywords/agent.resource
Resource            ../../resources/keywords/remote_localhost.resource

Suite Setup         Multi-User Daemon Suite Setup
Suite Teardown      Multi-User Daemon Suite Teardown
Test Setup          Start Daemons For Both Users
Test Teardown       Stop Daemons For Both Users

Test Tags           daemon  multi-user  remote-localhost


*** Variables ***
${G_PRIMARY_USER_HOME}       ${EMPTY}
${G_SECONDARY_USER_HOME}     ${EMPTY}


*** Test Cases ***
Each User Gets A Separate Engine Daemon
  [Documentation]  Both users can run the CLI simultaneously and publish distinct daemon connection details.
  ${primary} =  Given Read Daemon Metadata For User  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  ${secondary} =  When Read Daemon Metadata For User  ${G_TARGET_USER_NON_ROOT}  ${G_SECONDARY_USER_HOME}
  Then Should Not Be Equal As Integers  ${primary}[server_port]  ${secondary}[server_port]

Each User Reuses Their Own Engine Daemon
  [Documentation]  Repeated CLI commands reuse the calling user's daemon rather than starting another.
  ${primary_before} =  Given Read Daemon Metadata For User  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  ${secondary_before} =  And Read Daemon Metadata For User  ${G_TARGET_USER_NON_ROOT}  ${G_SECONDARY_USER_HOME}
  When Run Remote CLI As Login User  recipe list  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  And The Last Command Succeeded
  And Run Remote CLI As Login User  recipe list  ${G_TARGET_USER_NON_ROOT}  ${G_SECONDARY_USER_HOME}
  And The Last Command Succeeded
  ${primary_after} =  Then Read Daemon Metadata For User  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  ${secondary_after} =  And Read Daemon Metadata For User  ${G_TARGET_USER_NON_ROOT}  ${G_SECONDARY_USER_HOME}
  And Daemon Ports Should Be Unchanged  ${primary_before}  ${primary_after}
  And Daemon Ports Should Be Unchanged  ${secondary_before}  ${secondary_after}

Daemon Metadata Is Private To Its Owner
  [Documentation]  The second user cannot read, overwrite, or delete the primary user's daemon metadata.
  ${daemon_path} =  Given Daemon Metadata Path  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  ${daemon_before} =  And Read Daemon Metadata For User  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  When Remote Command As Login User Should Fail
  ...  cat ${daemon_path}
  ...  ${G_TARGET_USER_NON_ROOT}
  ...  ${G_SECONDARY_USER_HOME}
  And Remote Command As Login User Should Fail
  ...  echo '{}' > ${daemon_path}
  ...  ${G_TARGET_USER_NON_ROOT}
  ...  ${G_SECONDARY_USER_HOME}
  And Remote Command As Login User Should Fail
  ...  rm ${daemon_path}
  ...  ${G_TARGET_USER_NON_ROOT}
  ...  ${G_SECONDARY_USER_HOME}
  ${daemon_after} =  Then Read Daemon Metadata For User  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  And Dictionaries Should Be Equal  ${daemon_before}  ${daemon_after}
  And Primary User Daemon Metadata Should Remain Usable And Private  ${daemon_path}

Stopping One User's Daemon Does Not Stop The Other
  [Documentation]  Stopping the engine daemon discovers and stops only the calling user's daemon.
  Given Daemon Metadata Should Exist For User  ${G_TARGET_USER_NON_ROOT}  ${G_SECONDARY_USER_HOME}
  When Run Remote CLI As Login User  daemon stop  ${G_TARGET_USER_NON_ROOT}  ${G_SECONDARY_USER_HOME}
  And The Last Command Succeeded
  Then Run Remote CLI As Login User  recipe list  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  And The Last Command Succeeded
  And Daemon Metadata Should Not Exist For User  ${G_TARGET_USER_NON_ROOT}  ${G_SECONDARY_USER_HOME}


*** Keywords ***
Multi-User Daemon Suite Setup
  Common Setup
  Skip Unless Target OS Is  ${OS_LINUX}
  Skip If Remote Localhost Is Not Set Up
  Configure Multi-User Test Accounts

Configure Multi-User Test Accounts
  Populate Test Target With Non-Root User Global Variables
  The Test Target Has Passwordless Sudo
  Ensure Non-Root User Exists On The Test Target
  Grant User Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}
  ${primary_home} =  Get User Home Directory  ${G_TARGET_USER}
  ${secondary_home} =  Get User Home Directory  ${G_TARGET_USER_NON_ROOT}
  VAR  ${G_PRIMARY_USER_HOME} =  ${primary_home}  scope=GLOBAL
  VAR  ${G_SECONDARY_USER_HOME} =  ${secondary_home}  scope=GLOBAL

Multi-User Daemon Suite Teardown
  Run Keyword And Ignore Error  Stop Daemons For Both Users
  Run Keyword And Ignore Error  Revoke User Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}
  Run Keyword And Ignore Error  The User Is Removed From The Test Target Successfully  ${G_TARGET_USER_NON_ROOT}
  Common Teardown

Start Daemons For Both Users
  Stop Daemons For Both Users
  Run Remote CLI As Login User  recipe list  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  The Last Command Succeeded
  Run Remote CLI As Login User  recipe list  ${G_TARGET_USER_NON_ROOT}  ${G_SECONDARY_USER_HOME}
  The Last Command Succeeded

Stop Daemons For Both Users
  Run Keyword And Ignore Error
  ...  Run Remote CLI As Login User  daemon stop  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  Run Keyword And Ignore Error
  ...  Run Remote CLI As Login User  daemon stop  ${G_TARGET_USER_NON_ROOT}  ${G_SECONDARY_USER_HOME}

Run Remote CLI As Login User
  [Arguments]  ${arguments}  ${user}  ${home}
  Run Remote Command As Login User
  ...  cd ${REMOTE_LOCALHOST_DIR} && ./apx ${arguments} --json
  ...  ${user}
  ...  ${home}

Run Remote Command As Login User
  [Arguments]  ${command}  ${user}  ${home}
  ${wrapped} =  Catenate  SEPARATOR=${SPACE}
  ...  sudo -u ${user}
  ...  env -i HOME=${home} USER=${user} LOGNAME=${user} PATH=/usr/local/bin:/usr/bin:/bin
  ...  sh -c 'cd ${home} && ${command}'
  Run Target Command  "${wrapped}"

Remote Command As Login User Should Fail
  [Arguments]  ${command}  ${user}  ${home}
  Run Remote Command As Login User  ${command}  ${user}  ${home}
  The Last Command Failed

Get User Home Directory
  [Arguments]  ${user}
  Run Target Command  getent passwd ${user} | cut -d: -f6
  The Last Command Succeeded
  VAR  ${home} =  ${G_LAST_RESULT.stdout.strip()}
  Should Not Be Empty  ${home}
  RETURN  ${home}

Daemon Metadata Path
  [Documentation]  Discovers the published daemon metadata without assuming the product's state-directory name.
  [Arguments]  ${user}  ${home}
  Run Remote Command As Login User
  ...  find ${home}/.local/state -mindepth 2 -maxdepth 2 -type f -name daemon.json
  ...  ${user}
  ...  ${home}
  The Last Command Succeeded
  ${paths} =  Split To Lines  ${G_LAST_RESULT.stdout}
  Length Should Be  ${paths}  1
  RETURN  ${paths}[0]

Read Daemon Metadata For User
  [Arguments]  ${user}  ${home}
  ${path} =  Daemon Metadata Path  ${user}  ${home}
  Run Remote Command As Login User  cat ${path}  ${user}  ${home}
  The Last Command Succeeded
  ${daemon} =  Evaluate  json.loads(r'''${G_LAST_RESULT.stdout}''')  modules=json
  RETURN  ${daemon}

Daemon Ports Should Be Unchanged
  [Arguments]  ${before}  ${after}
  Should Be Equal As Integers  ${before}[server_port]  ${after}[server_port]

Daemon Metadata Should Exist For User
  [Arguments]  ${user}  ${home}
  Daemon Metadata Path  ${user}  ${home}

Primary User Daemon Metadata Should Remain Usable And Private
  [Arguments]  ${daemon_path}
  Run Remote CLI As Login User  recipe list  ${G_TARGET_USER}  ${G_PRIMARY_USER_HOME}
  The Last Command Succeeded
  Run Target Command  stat -c '%a:%U' ${daemon_path}
  The Last Command Succeeded
  Should Be Equal As Strings  ${G_LAST_RESULT.stdout.strip()}  600:${G_TARGET_USER}

Daemon Metadata Should Not Exist For User
  [Arguments]  ${user}  ${home}
  Run Remote Command As Login User
  ...  find ${home}/.local/state -mindepth 2 -maxdepth 2 -type f -name daemon.json
  ...  ${user}
  ...  ${home}
  The Last Command Succeeded
  Should Be Empty  ${G_LAST_RESULT.stdout.strip()}
