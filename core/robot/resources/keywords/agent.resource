# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       A resource file containg keywords for the Target Agent robot tests.

Resource            target.resource
Library             ../libs/AgentHelper.py


*** Variables ***
# They are populated during runtime
${G_TARGET_NAME_NON_ROOT}               ${EMPTY}
${G_TARGET_USER_NON_ROOT}               ${EMPTY}
${G_TARGET_AGENT_CONTROLLER_CMD}        ${EMPTY}
${G_TARGET_AGENT_ROOTWORKER_CMD}        ${EMPTY}
${G_TARGET_AGENT_GROUP_CONTROLLER_CMD}  ${EMPTY}


*** Keywords ***
Group Controller Suite Setup
  [Documentation]  Prepares the test target for group controller suite tests.
  Common Setup
  Skip Unless Target OS Is  ${OS_LINUX}
  Clear Test Target Output Directories
  Ensure Target Does Not Exist  ${G_TARGET_NAME}
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  The Test Target Is Prepared Successfully
  Populate Test Target Global Variables

Group Controller Suite Teardown
  [Documentation]  Cleans up after the group controller suite tests.
  Skip Unless Target OS Is  ${OS_LINUX}
  The Test Target Is Removed Successfully
  Clear Test Target Output Directories
  Common Teardown

Root Worker Suite Setup
  [Documentation]  Prepares the test target for root worker suite tests.
  Common Setup
  Skip Unless Target OS Is  ${OS_LINUX}
  Clear Test Target Output Directories
  The Test Target Is Added Successfully
  The Target Is Set To Default Successfully  ${G_TARGET_NAME}
  The Test Target Is Prepared Successfully
  Populate Test Target Global Variables
  Configure Non-Root User On Target
  The Test Target With Non-Root User Is Added Successfully

Root Worker Suite Teardown
  [Documentation]  Cleans up after the root worker suite tests.
  Skip Unless Target OS Is  ${OS_LINUX}
  Remove Non-Root User From Target
  The Test Target With Non-Root User Is Removed Successfully
  The Test Target Is Removed Successfully
  Clear Test Target Output Directories
  Common Teardown

Root Worker Test Teardown
  [Documentation]  Cleans up after each root worker test.
  Stop Daemon

Configure Non-Root User On Target
  [Documentation]  (Linux-only) Adds the non-root user required for agent tests on the target OS.
  The Test Target Has Passwordless Sudo
  Populate Test Target With Non-Root User Global Variables
  Ensure Non-Root User Exists On The Test Target
  Grant User Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}

Remove Non-Root User From Target
  [Documentation]  (Linux-only) Removes the non-root user required for agent tests from the target OS.
  Revoke User Passwordless Sudo On The Test Target  ${G_TARGET_USER_NON_ROOT}
  The User Is Removed From The Test Target Successfully  ${G_TARGET_USER_NON_ROOT}

Populate Test Target Global Variables
  [Documentation]  Populates global variables required for the agent suite tests.
  Should Not Be Empty  ${AGENT_BINARY_FILE_NAME}  msg=AGENT_BINARY_FILE_NAME must be set for agent tests.
  VAR  ${G_TARGET_AGENT_CONTROLLER_CMD} =  ${AGENT_BINARY_FILE_NAME} start  scope=GLOBAL
  VAR  ${G_TARGET_AGENT_ROOTWORKER_CMD} =  ${AGENT_BINARY_FILE_NAME} start-root-worker  scope=GLOBAL
  VAR  ${G_TARGET_AGENT_GROUP_CONTROLLER_CMD} =  ${AGENT_BINARY_FILE_NAME} start-group-controller  scope=GLOBAL

Populate Test Target With Non-Root User Global Variables
  [Documentation]  Populates global variables required for the agent suite tests.
  Should Not Be Empty  ${G_TARGET_NAME}  msg=G_TARGET_NAME must be set for agent tests.
  Should Not Be Empty  ${G_TARGET_USER}  msg=G_TARGET_USER must be set for agent tests.
  VAR  ${G_TARGET_NAME_NON_ROOT} =  ${G_TARGET_NAME}-non-root  scope=GLOBAL
  VAR  ${G_TARGET_USER_NON_ROOT} =  ${G_TARGET_USER}-non-root  scope=GLOBAL

The Target Agent Started Successfully
  [Documentation]  Triggers the start of the target agent by running a simple command on the target.
  Run Target Info  ${G_TARGET_NAME_NON_ROOT}
  The Last Command Succeeded

The Target Agent Stopped Successfully
  [Documentation]  Stops the target agent by ensuring the agent process is not running on the target. Note that
  ...  stopping the engine daemon also causes the target agent to be stopped automatically.
  Stop Daemon
  Wait Until Keyword Succeeds  10 sec  2 sec  The Target Agent Is Not Running

The Target Agent Is Not Running
  [Documentation]  Verifies that the target agent process is not running on the target.
  Target Process Is Not Running  ${AGENT_BINARY_FILE_NAME}
  The Last Command Succeeded

The Target Agent Process Has Capability
  [Documentation]  (Linux-only) Sets the specified Linux capability on the target agent process.
  ...  Optionally resolves the agent binary for a specific username (required when the agent is
  ...  deployed under a non-default user's home directory).
  [Arguments]  ${linux_capability_name}  ${username}=${EMPTY}
  ${agent_binary_path} =  Resolve Target Agent Binary Path  ${username}
  Set Process Capability  ${agent_binary_path}  ${linux_capability_name}
  The Last Command Succeeded
  Get Process Capability  ${agent_binary_path}
  The Last Command Succeeded
  Should Contain  ${G_LAST_RESULT.stdout}  ${linux_capability_name}

The Target Agent Process Capabilities Are Cleared
  [Documentation]  (Linux-only) Clears all Linux capabilities on the target agent process.
  ...  Optionally resolves the agent binary for a specific username (required when the agent is
  ...  deployed under a non-default user's home directory).
  [Arguments]  ${username}=${EMPTY}
  ${agent_binary_path} =  Resolve Target Agent Binary Path  ${username}
  Get Process Capability  ${agent_binary_path}
  ${has_caps} =  Run Keyword And Return Status  The Last Command Succeeded
  IF  ${has_caps}
    ${current_spec} =  Strip String  ${G_LAST_RESULT.stdout}
    IF  '${current_spec}' == ''  RETURN
  END
  Clear Process Capabilities  ${agent_binary_path}
  The Last Command Succeeded

The Test Target Has Passwordless Sudo
  [Documentation]  Verifies that the current test target user has passwordless sudo.
  Check User Has Passwordless Sudo On The Test Target  ${G_TARGET_USER}

The Test Target With Non-Root User Is Added Successfully
  [Documentation]  Adds the test target with non-root user credentials.
  The Target Is Added Successfully
  ...  ${G_TARGET_USER_NON_ROOT}@${G_TARGET_HOST}:${G_TARGET_PORT}:${G_TARGET_KEY} --name ${G_TARGET_NAME_NON_ROOT}

The Test Target With Non-Root User Is Removed Successfully
  [Documentation]  Removes the test target with non-root user credentials.
  The Target Is Removed Successfully  ${G_TARGET_NAME_NON_ROOT}

The Test Target With Non-Root User Is The Default Target
  [Documentation]  Sets the test target with non-root user credentials as the default target.
  The Target Is Set To Default Successfully  ${G_TARGET_NAME_NON_ROOT}

The Test Target With Non-Root User Is Prepared Successfully
  [Documentation]  Prepare the non-root target from a clean state and verify deployment under the non-root user.
  Unprepare The Target
  Prepare Target  ${G_TARGET_NAME_NON_ROOT}
  The Last Command Succeeded
  Set Test Username  ${G_TARGET_USER_NON_ROOT}
  The Target Agent Is Deployed

The User Has Passwordless Sudo On The Test Target
  [Documentation]  Grants passwordless sudo privileges to the user, if not already granted.
  [Arguments]  ${user_name}
  ${has_passwordless} =  Run Keyword And Return Status
  ...  Check User Has Passwordless Sudo On The Test Target  ${user_name}
  IF  not ${has_passwordless}
    Grant User Passwordless Sudo On The Test Target  ${user_name}
  END

The User Lacks Passwordless Sudo On The Test Target
  [Documentation]  Revokes passwordless sudo privileges from the user, if currently granted.
  [Arguments]  ${user_name}
  Revoke User Passwordless Sudo On The Test Target  ${user_name}

Ensure Non-Root User Exists On The Test Target
  [Documentation]  (Linux-only) Ensures that the non-root user required for agent tests exists on the target, creating it if necessary.
  ${exists} =  Run Keyword And Return Status  The User Exists On The Test Target  ${G_TARGET_USER_NON_ROOT}
  IF  not ${exists}
    The User Is Added On The Test Target Successfully  ${G_TARGET_USER_NON_ROOT}
  END
  The User Exists On The Test Target  ${G_TARGET_USER_NON_ROOT}

The User Is Added On The Test Target Successfully
  [Documentation]  (Linux-only) Creates a new user on the target with the specified name.
  [Arguments]  ${new_user_name}
  # 1. Create the user
  Run Target Command  sudo useradd -m ${new_user_name}
  The Last Command Succeeded

  # 2. Inherit current user's authorized_keys
  Run Target Command
  ...  sudo install -m 700 -o ${new_user_name} -g ${new_user_name} -d /home/${new_user_name}/.ssh
  The Last Command Succeeded
  Run Target Command
  ...  sudo cp -p /home/${G_TARGET_USER}/.ssh/authorized_keys /home/${new_user_name}/.ssh/authorized_keys
  The Last Command Succeeded

  # 3. Permission
  Run Target Command
  ...  sudo chown -R ${new_user_name}:${new_user_name} /home/${new_user_name}/.ssh
  The Last Command Succeeded

The User Is Removed From The Test Target Successfully
  [Documentation]  (Linux-only) Removes the specified user from the target.
  [Arguments]  ${user_name}
  Run Target Command  sudo userdel -rf ${user_name}
  The Last Command Succeeded
  The User Does Not Exist On The Test Target  ${user_name}

The User Exists On The Test Target
  [Documentation]  Verifies that the non-root user required for agent tests exists on the target.
  [Arguments]  ${user_name}
  Run Target Command  id -u ${user_name}
  The Last Command Succeeded

The User Does Not Exist On The Test Target
  [Documentation]  Verifies that the non-root user required for agent tests does not exist on the target.
  [Arguments]  ${user_name}
  Run Target Command  id -u ${user_name}
  The Last Command Failed

Grant User Passwordless Sudo On The Test Target
  [Documentation]  Grants passwordless sudo privileges to the specified user on the test target.
  ...  This is done by creating an ephemeral sudoers file under /run/sudoers.d.
  ...  Doing so ensures the user does not retaing passwordless sudo privileges after a reboot.
  [Arguments]  ${user_name}
  # 1. Register /run/sudoers.d as a sudoers include directory
  Run Target Command  echo '#includedir /run/sudoers.d' | sudo tee /etc/sudoers.d/robot-ephemeral-sudoers
  The Last Command Succeeded

  # 2. Create the sudoers.d directory if it does not exist
  # Note: This directory is owned by the root and is not writeable by others
  # And SSH expects it to be so. See sudoers(5) for details.
  Run Target Command  sudo mkdir -p /run/sudoers.d
  The Last Command Succeeded

  # 3. Add passwordless sudo rule for the user
  Run Target Command  echo '${user_name} ALL=(ALL) NOPASSWD:ALL' | sudo tee /run/sudoers.d/99-${user_name}
  The Last Command Succeeded

Revoke User Passwordless Sudo On The Test Target
  [Documentation]  Revokes passwordless sudo privileges from the specified user on the test target.
  ...  This is done by removing the ephemeral sudoers file under /run/sudoers.d.
  [Arguments]  ${user_name}
  Run Target Command  sudo rm -f /run/sudoers.d/99-${user_name}
  The Last Command Succeeded

Check User Has Passwordless Sudo On The Test Target
  [Documentation]  Verifies that the specified user has passwordless sudo privileges on the test target.
  [Arguments]  ${user_name}
  Run Target Command  sudo -u ${user_name} sudo -n true
  The Last Command Succeeded

Check User Lacks Passwordless Sudo On The Test Target
  [Documentation]  Verifies that the specified user lacks passwordless sudo privileges on the test target.
  [Arguments]  ${user_name}
  Run Target Command  sudo -u ${user_name} sudo -n true
  The Last Command Failed

The Target Agent Is Running As User
  [Documentation]  Verifies that the target agent process is running as the specified user on the test target.
  [Arguments]  ${mode}  ${user_name}
  Target Process Command Is Running As User  ${mode}  ${user_name}
  The Last Command Succeeded

The Root Worker Is Triggered
  [Documentation]  Runs a deployed tool integration with an asPrivileged operation.
  Run Code Hotspots Recipe  --target ${G_TARGET_NAME_NON_ROOT} --system-wide ${DEPLOY_TOOLS_FLAG}

The Root Worker Is Not Triggered
  [Documentation]  Runs a tool integration that does not require root privileges.
  ${perf_event_paranoid_result} =  Get Target Platform Perf Event Paranoid
  The Last Command Succeeded
  ${perf_event_paranoid} =  Strip String  ${perf_event_paranoid_result.stdout}
  IF  "${perf_event_paranoid}" != "0"
    Target Platform Perf Event Paranoid Should Be  0
  END
  TRY
    Run Code Hotspots Recipe  --target ${G_TARGET_NAME_NON_ROOT} --workload ${LAUNCH_WORKLOAD} ${DEPLOY_TOOLS_FLAG}
    The Last Command Succeeded
  FINALLY
    IF  "${perf_event_paranoid}" != "0"
      Target Platform Perf Event Paranoid Should Be  ${perf_event_paranoid}
    END
  END

The Root Worker Is Running On The Target
  [Documentation]  Verifies that the root worker process is running on the test target.
  Target Process Is Running  ${G_TARGET_AGENT_ROOTWORKER_CMD}
  The Last Command Succeeded

The Root Worker Is Not Running On The Target
  [Documentation]  Verifies that the root worker process is not running on the test target.
  Target Process Is Not Running  ${G_TARGET_AGENT_ROOTWORKER_CMD}
  The Last Command Succeeded
