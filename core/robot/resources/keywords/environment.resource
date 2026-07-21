# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A resource file containing environment keywords for the Arm Total Performance CLI.

Library             OperatingSystem
Resource            daemon.resource


*** Keywords ***
Set Data Directory Env Var
  [Documentation]  Set the data directory which controls where runs live. The
  ...  atperf engine daemon must be restarted for this to take effect.
  ${os} =  Determine Operating System
  IF  '${os}' == '${OS_LINUX}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}share${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_DARWIN}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}share${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_WINDOWS}'
    VAR  ${dir}  %{USERPROFILE}${/}AppData${/}Local${/}Robot${/}${DAEMON_DIR_NAME}  scope=LOCAL
  END
  Create Directory  ${dir}
  Set Performix Env Var  DATA_DIR  ${dir}

Set Config Directory Env Var
  [Documentation]  Set the config directory which controls where target configs live. The
  ...  atperf engine daemon must be restarted for this to take effect.
  ${os} =  Determine Operating System
  IF  '${os}' == '${OS_LINUX}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}config${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_DARWIN}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}config${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_WINDOWS}'
    VAR  ${dir}  %{USERPROFILE}${/}AppData${/}Local${/}Robot${/}${DAEMON_DIR_NAME}  scope=LOCAL
  END
  Create Directory  ${dir}
  Set Performix Env Var  CONFIG_DIR  ${dir}

Set Log File Env Var
  [Documentation]  Set the log file location. The atperf engine daemon must be
  ...  restarted for this to take effect.
  ${os} =  Determine Operating System
  IF  '${os}' == '${OS_LINUX}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}state${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_DARWIN}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}state${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_WINDOWS}'
    VAR  ${dir}  %{USERPROFILE}${/}AppData${/}Local${/}Robot${/}${DAEMON_DIR_NAME}  scope=LOCAL
  END
  Create Directory  ${dir}
  Set Performix Env Var  LOG_FILE  ${dir}/${DAEMON_DIR_NAME}_log_file

Set Log Level Env Var
  [Documentation]  Set the log level. The atperf engine daemon must be
  ...  restarted for this to take effect.
  [Arguments]  ${level}
  Set Performix Env Var  LOG_LEVEL  ${level}

Set Enable Agent Env Var
  [Documentation]  Enable or disable agent mode. The atperf engine daemon must be
  ...  restarted for this to take effect. This can be removed once we have fully
  ...  transitioned to using the agent.
  [Arguments]  ${value}
  Set Performix Env Var  ENABLE_AGENT  ${value}

Set Enable On Demand Privilege Env Var
  [Documentation]  Enable or disable on-demand privilege mode. The atperf engine daemon must be
  ...  restarted for this to take effect.
  [Arguments]  ${value}
  Set Performix Env Var  ENABLE_ON_DEMAND_PRIVILEGE  ${value}

Set Enable Full Capture Support Env Var
  [Documentation]  Enable full capture support. The atperf engine daemon must be
  ...  restarted for this to take effect.
  Set Performix Env Var  ENABLE_FULL_CAPTURE_SUPPORT  1

Set Enable Rerendering Env Var
  [Documentation]  Enable rerendering feature. The atperf engine daemon must be
  ...  restarted for this to take effect.
  Set Performix Env Var  ENABLE_RERENDERING  1

Set Enable Experimental Recipes Env Var
  [Documentation]  Enable experimental recipes. The atperf engine daemon must be
  ...  restarted for this to take effect.
  Set Performix Env Var  ENABLE_EXPERIMENTAL_RECIPES  1

Set ATP Env Vars
  [Documentation]  Set environment variables to ensure the Robot tests run in a
  ...  sandboxed area. This prevents local data (such as runs and targets) from
  ...  being destroyed by the Robot run.
  Set Data Directory Env Var
  Set Config Directory Env Var
  Set Log File Env Var
  Set Log Level Env Var  debug

Clear Data Directory Env Var
  [Documentation]  Clear the data directory environment variable. The atperf
  ...  engine daemon must be restarted for this to take effect.
  Clear Performix Env Var  DATA_DIR

Clear Config Directory Env Var
  [Documentation]  Clear the config directory environment variable. The atperf
  ...  engine daemon must be restarted for this to take effect.
  Clear Performix Env Var  CONFIG_DIR

Clear Log File Env Var
  [Documentation]  Clear the log file location. The atperf engine daemon must be
  ...  restarted for this to take effect.
  Clear Performix Env Var  LOG_FILE

Clear Log Level Env Var
  [Documentation]  Clear the log level environment variable. The atperf engine
  ...  daemon must be restarted for this to take effect.
  Clear Performix Env Var  LOG_LEVEL

Clear Enable Agent Env Var
  [Documentation]  Clear the enable agent environment variable. The atperf engine
  ...  daemon must be restarted for this to take effect. This can be removed once
  ...  we have fully transitioned to using the agent.
  Clear Performix Env Var  ENABLE_AGENT

Clear Enable Full Capture Support Env Var
  [Documentation]  Clear the full capture support environment variable. The atperf engine daemon must be
  ...  restarted for this to take effect.
  Clear Performix Env Var  ENABLE_FULL_CAPTURE_SUPPORT

Clear Enable Rerendering Env Var
  [Documentation]  Clear the rerendering env var. The atperf engine daemon must be
  ...  restarted for this to take effect.
  Clear Performix Env Var  ENABLE_RERENDERING

Clear Enable Experimental Recipes Env Var
  [Documentation]  Clear the experimental recipes env var. The atperf engine daemon must be
  ...  restarted for this to take effect.
  Clear Performix Env Var  ENABLE_EXPERIMENTAL_RECIPES

Clear ATP Env Vars
  [Documentation]  Clear any environment variables set by Robot.
  Clear Data Directory Env Var
  Clear Config Directory Env Var
  Clear Log File Env Var
  Clear Log Level Env Var

Set Performix Env Var
  [Documentation]  Helper keyword which prefixes the provided env var name with
  ...  the env var prefix as defined in terminology.json, and sets it to the
  ...  provided value
  [Arguments]  ${env_var_name}  ${env_var_value}
  Set Environment Variable  ${ENV_VAR_PREFIX}_${env_var_name}  ${env_var_value}
  Run Keyword And Ignore Error  Stop Daemon

Get Performix Env Var
  [Documentation]  Helper keyword which prefixes the provided env var name with
  ...  the env var prefix as defined in terminology.json, and gets its value
  [Arguments]  ${env_var_name}
  ${value} =  Get Environment Variable  ${ENV_VAR_PREFIX}_${env_var_name}
  RETURN  ${value}

Clear Performix Env Var
  [Documentation]  Helper keyword which prefixes the provided env var name with
  ...  the env var prefix as defined in terminology.json, and clears its value
  [Arguments]  ${env_var_name}
  Remove Environment Variable  ${ENV_VAR_PREFIX}_${env_var_name}
  Run Keyword And Ignore Error  Stop Daemon
