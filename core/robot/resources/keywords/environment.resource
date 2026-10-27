# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A resource file containing environment keywords for the Arm Performix CLI.

Library             OperatingSystem
Resource            daemon.resource


*** Variables ***
${ENV_VAR_UNSET}     __PERFORMIX_ROBOT_ENV_VAR_UNSET__


*** Keywords ***
Set Data Directory Env Var
  [Documentation]  Set the data directory which controls where runs live. The
  ...  apx engine daemon must be restarted for this to take effect.
  [Arguments]  ${stop_daemon}=${True}
  ${os} =  Determine Operating System
  IF  '${os}' == '${OS_LINUX}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}share${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_DARWIN}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}share${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_WINDOWS}'
    VAR  ${dir}  %{USERPROFILE}${/}AppData${/}Local${/}Robot${/}${DAEMON_DIR_NAME}  scope=LOCAL
  END
  Create Directory  ${dir}
  ${changed} =  Set Performix Env Var  DATA_DIR  ${dir}  stop_daemon=${stop_daemon}
  RETURN  ${changed}

Set Config Directory Env Var
  [Documentation]  Set the config directory which controls where target configs live. The
  ...  apx engine daemon must be restarted for this to take effect.
  [Arguments]  ${stop_daemon}=${True}
  ${os} =  Determine Operating System
  IF  '${os}' == '${OS_LINUX}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}config${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_DARWIN}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}config${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_WINDOWS}'
    VAR  ${dir}  %{USERPROFILE}${/}AppData${/}Local${/}Robot${/}${DAEMON_DIR_NAME}  scope=LOCAL
  END
  Create Directory  ${dir}
  ${changed} =  Set Performix Env Var  CONFIG_DIR  ${dir}  stop_daemon=${stop_daemon}
  RETURN  ${changed}

Set Log File Env Var
  [Documentation]  Set the log file location. The apx engine daemon must be
  ...  restarted for this to take effect.
  [Arguments]  ${stop_daemon}=${True}
  ${os} =  Determine Operating System
  IF  '${os}' == '${OS_LINUX}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}state${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_DARWIN}'
    VAR  ${dir}  %{HOME}${/}.local${/}robot${/}state${/}${DAEMON_DIR_NAME}  scope=LOCAL
  ELSE IF  '${os}' == '${OS_WINDOWS}'
    VAR  ${dir}  %{USERPROFILE}${/}AppData${/}Local${/}Robot${/}${DAEMON_DIR_NAME}  scope=LOCAL
  END
  Create Directory  ${dir}
  ${changed} =  Set Performix Env Var  LOG_FILE  ${dir}/${DAEMON_DIR_NAME}_log_file  stop_daemon=${stop_daemon}
  RETURN  ${changed}

Set Log Level Env Var
  [Documentation]  Set the log level. The apx engine daemon must be
  ...  restarted for this to take effect.
  [Arguments]  ${level}  ${stop_daemon}=${True}
  ${changed} =  Set Performix Env Var  LOG_LEVEL  ${level}  stop_daemon=${stop_daemon}
  RETURN  ${changed}

Set Enable Agent Env Var
  [Documentation]  Enable or disable agent mode. The apx engine daemon must be
  ...  restarted for this to take effect. This can be removed once we have fully
  ...  transitioned to using the agent.
  [Arguments]  ${value}
  Set Performix Env Var  ENABLE_AGENT  ${value}

Set Enable Experimental Recipes Env Var
  [Documentation]  Enable experimental recipes. The apx engine daemon must be
  ...  restarted for this to take effect.
  Set Performix Env Var  ENABLE_EXPERIMENTAL_RECIPES  1

Set ATP Env Vars
  [Documentation]  Set environment variables to ensure the Robot tests run in a
  ...  sandboxed area. This prevents local data (such as runs and targets) from
  ...  being destroyed by the Robot run. Restart the daemon after applying all changes.
  ${data_changed} =  Set Data Directory Env Var  stop_daemon=${False}
  ${config_changed} =  Set Config Directory Env Var  stop_daemon=${False}
  ${log_file_changed} =  Set Log File Env Var  stop_daemon=${False}
  ${log_level_changed} =  Set Log Level Env Var  debug  stop_daemon=${False}
  IF  ${data_changed} or ${config_changed} or ${log_file_changed} or ${log_level_changed}
    Run Keyword And Ignore Error  Stop Daemon
  END

Clear Data Directory Env Var
  [Documentation]  Clear the data directory environment variable. The apx
  ...  engine daemon must be restarted for this to take effect.
  [Arguments]  ${stop_daemon}=${True}
  ${changed} =  Clear Performix Env Var  DATA_DIR  stop_daemon=${stop_daemon}
  RETURN  ${changed}

Clear Config Directory Env Var
  [Documentation]  Clear the config directory environment variable. The apx
  ...  engine daemon must be restarted for this to take effect.
  [Arguments]  ${stop_daemon}=${True}
  ${changed} =  Clear Performix Env Var  CONFIG_DIR  stop_daemon=${stop_daemon}
  RETURN  ${changed}

Clear Log File Env Var
  [Documentation]  Clear the log file location. The apx engine daemon must be
  ...  restarted for this to take effect.
  [Arguments]  ${stop_daemon}=${True}
  ${changed} =  Clear Performix Env Var  LOG_FILE  stop_daemon=${stop_daemon}
  RETURN  ${changed}

Clear Log Level Env Var
  [Documentation]  Clear the log level environment variable. The apx engine
  ...  daemon must be restarted for this to take effect.
  [Arguments]  ${stop_daemon}=${True}
  ${changed} =  Clear Performix Env Var  LOG_LEVEL  stop_daemon=${stop_daemon}
  RETURN  ${changed}

Clear Enable Agent Env Var
  [Documentation]  Clear the enable agent environment variable. The apx engine
  ...  daemon must be restarted for this to take effect. This can be removed once
  ...  we have fully transitioned to using the agent.
  Clear Performix Env Var  ENABLE_AGENT

Clear Enable Experimental Recipes Env Var
  [Documentation]  Clear the experimental recipes env var. The apx engine daemon must be
  ...  restarted for this to take effect.
  Clear Performix Env Var  ENABLE_EXPERIMENTAL_RECIPES

Clear ATP Env Vars
  [Documentation]  Clear any environment variables set by Robot. Restart the
  ...  daemon after applying all changes.
  ${data_changed} =  Clear Data Directory Env Var  stop_daemon=${False}
  ${config_changed} =  Clear Config Directory Env Var  stop_daemon=${False}
  ${log_file_changed} =  Clear Log File Env Var  stop_daemon=${False}
  ${log_level_changed} =  Clear Log Level Env Var  stop_daemon=${False}
  IF  ${data_changed} or ${config_changed} or ${log_file_changed} or ${log_level_changed}
    Run Keyword And Ignore Error  Stop Daemon
  END

Set Performix Env Var
  [Documentation]  Helper keyword which prefixes the provided env var name with
  ...  the env var prefix as defined in terminology.json, and sets it to the
  ...  provided value. Returns whether the value changed.
  [Arguments]  ${env_var_name}  ${env_var_value}  ${stop_daemon}=${True}
  ${qualified_name} =  Catenate  SEPARATOR=_  ${ENV_VAR_PREFIX}  ${env_var_name}
  ${current_value} =  Get Environment Variable  ${qualified_name}  ${ENV_VAR_UNSET}
  IF  $current_value == $env_var_value  RETURN  ${False}
  Set Environment Variable  ${qualified_name}  ${env_var_value}
  IF  ${stop_daemon}  Run Keyword And Ignore Error  Stop Daemon
  RETURN  ${True}

Get Performix Env Var
  [Documentation]  Helper keyword which prefixes the provided env var name with
  ...  the env var prefix as defined in terminology.json, and gets its value
  [Arguments]  ${env_var_name}
  ${value} =  Get Environment Variable  ${ENV_VAR_PREFIX}_${env_var_name}
  RETURN  ${value}

Clear Performix Env Var
  [Documentation]  Helper keyword which prefixes the provided env var name with
  ...  the env var prefix as defined in terminology.json, and clears its value.
  ...  Returns whether the value was set.
  [Arguments]  ${env_var_name}  ${stop_daemon}=${True}
  ${qualified_name} =  Catenate  SEPARATOR=_  ${ENV_VAR_PREFIX}  ${env_var_name}
  ${current_value} =  Get Environment Variable  ${qualified_name}  ${ENV_VAR_UNSET}
  IF  $current_value == $ENV_VAR_UNSET  RETURN  ${False}
  Remove Environment Variable  ${qualified_name}
  IF  ${stop_daemon}  Run Keyword And Ignore Error  Stop Daemon
  RETURN  ${True}
