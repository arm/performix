# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

*** Settings ***
Documentation       Additional target utility and recipe tool keywords for Robot.


*** Keywords ***
The Target PID Info Contains
  [Documentation]  Check that the target PID info for a particular target contains the expected value
  [Arguments]  ${field}  ${expected}
  ${result} =  Parse JSON To Dictionary  ${G_LAST_RESULT.stdout}
  ${data} =  Get From Dictionary  ${result}  data
  ${info} =  Get From Dictionary  ${data}  info
  ${pid_info} =  Get From Dictionary  ${info}  sl-collect-target-pids
  ${process_wrapper} =  Get From Dictionary  ${pid_info}  Info
  ${process_list} =  Get From Dictionary  ${process_wrapper}  Pids
  ${processes} =  Get From Dictionary  ${process_list}  process
  ${first_process} =  Get From List  ${processes}  0
  Dictionary Should Contain Item  ${first_process}  ${field}  ${expected}

The Target Output Directory Is Empty
  [Documentation]  Confirms that the remote machine does not contain any atperf output directories.
  Target Run Directories Exist
  VAR  ${stdout} =  ${G_LAST_RESULT.stdout.strip()}  scope=LOCAL
  Should Be Equal As Strings  ${stdout}  EMPTY  There are atperf output directories on the remote machine.

The Target Output Directory Is Not Empty
  [Documentation]  Confirms that the remote machine does contain atperf output directories.
  Target Run Directories Exist
  VAR  ${stdout} =  ${G_LAST_RESULT.stdout.strip()}  scope=LOCAL
  Should Be Equal As Strings  ${stdout}  NOT_EMPTY  There are no atperf output directories on the remote machine.

The Output Directory Is Removed From The Target
  [Documentation]  Removes the output directories from the target machine.
  Remove Target Run Directories
  The Last Command Succeeded
  The Target Output Directory Is Empty

Skip If Target Arch Is
  [Documentation]  Used to skip a test which uses a feature that is unsupported on the target architecture
  [Arguments]  ${arch}
  Skip If  '${G_TARGET_ARCH}' == '${arch}'  Test skipped. Incompatible with target architecture ${arch}.

Skip Unless Target Arch Is
  [Documentation]  Skips the current test unless the target architecture matches the supplied value.
  [Arguments]  ${arch}
  Skip If  '${G_TARGET_ARCH}' != '${arch}'  Test skipped. Requires target architecture ${arch}.

Skip If Target OS Is
  [Documentation]  Used to skip a test which uses a feature that is unsupported on the target operating system
  [Arguments]  ${os}
  Skip If  '${G_TARGET_OS}' == '${os}'  Test skipped. Incompatible with target operating system ${os}.

Skip Unless Target OS Is
  [Documentation]  Skips the current test unless the target operating system matches the supplied value.
  [Arguments]  ${os}
  Skip If  '${G_TARGET_OS}' != '${os}'  Test skipped. Requires target operating system ${os}.

CPU Microarchitecture Is Supported On Target
  [Documentation]  Return whether the CPU Microarchitecture recipe is supported on the configured target.
  ${is_supported} =  Evaluate  '${G_TARGET_OS}' == '${OS_LINUX}' and '${G_TARGET_ARCH}' == '${ARCH_AARCH64}'
  RETURN  ${is_supported}

Skip Unless CPU Microarchitecture Is Supported On Target
  [Documentation]  Skip unless the configured target supports the CPU Microarchitecture recipe.
  ${is_supported} =  CPU Microarchitecture Is Supported On Target
  Skip If  not ${is_supported}  Test skipped. CPU Microarchitecture requires a Linux aarch64 target.

Instruction Mix Is Supported On Target
  [Documentation]  Return whether the Instruction Mix recipe is supported on the configured target.
  ${is_supported} =  Evaluate  '${G_TARGET_OS}' == '${OS_LINUX}' and '${G_TARGET_ARCH}' == '${ARCH_AARCH64}'
  RETURN  ${is_supported}

Skip Unless Instruction Mix Is Supported On Target
  [Documentation]  Skip unless the configured target supports the Instruction Mix recipe.
  ${is_supported} =  Instruction Mix Is Supported On Target
  Skip If  not ${is_supported}  Test skipped. Instruction Mix requires a Linux aarch64 target.

System Utilization Is Supported On Target
  [Documentation]  Return whether the System Utilization recipe is supported on the configured target.
  ${is_supported} =  Evaluate  '${G_TARGET_OS}' == '${OS_LINUX}'
  RETURN  ${is_supported}

Skip Unless System Utilization Is Supported On Target
  [Documentation]  Skip unless the configured target supports the System Utilization recipe.
  ${is_supported} =  System Utilization Is Supported On Target
  Skip If  not ${is_supported}  Test skipped. System Utilization requires a Linux target.

### Recipe Tools ###

The Instruction Mix Tool Is Deployed
  [Documentation]  Check that the instruction mix tool has been deployed to the target
  Check Target Directory Exists  ${ATPERF_TOOLS_DIR}/instruction_mix
  The Last Command Succeeded

The Instruction Mix Tool Is Not Deployed
  [Documentation]  Check that the instruction mix tool has not been deployed to the target
  Check Target Directory Does Not Exist  ${ATPERF_TOOLS_DIR}/instruction_mix
  The Last Command Succeeded

The CPU Hotspots Tool Is Deployed
  [Documentation]  Check that the CPU hotspots tool has been deployed to the target
  Check Target Directory Exists  ${ATPERF_TOOLS_DIR}/cpu_hotspots
  The Last Command Succeeded

The CPU Hotspots Tool Is Not Deployed
  [Documentation]  Check that the CPU hotspots tool has not been deployed to the target
  Check Target Directory Does Not Exist  ${ATPERF_TOOLS_DIR}/cpu_hotspots
  The Last Command Succeeded

The CPU Hotspots Agent Is Deployed
  [Documentation]  Check that the CPU hotspots agent has been deployed to the target
  Check Target Directory Exists  ${ATPERF_TOOLS_DIR}/cpu_hotspots_agent
  The Last Command Succeeded

The CPU Hotspots Agent Is Not Deployed
  [Documentation]  Check that the CPU hotspots agent has not been deployed to the target
  Check Target Directory Does Not Exist  ${ATPERF_TOOLS_DIR}/cpu_hotspots_agent
  The Last Command Succeeded

The CPU Hotspots Agent Libraries Have Debug Symbols
  [Documentation]  Check that the CPU hotspots agent libraries contain debug symbols
  ${target_os} =  Get Variable Value  ${G_TARGET_OS}  ${EMPTY}
  ${is_windows} =  Run Keyword And Return Status  Should Be Equal  ${target_os}  ${OS_WINDOWS}
  IF  not ${is_windows}
    The Cpu Hotspots Agent Libraries Have Debug Symbols (non-windows)
  END

The Cpu Hotspots Agent Libraries Have Debug Symbols (non-windows)
  Run Target Command  nm ${ATPERF_TOOLS_DIR}/cpu_hotspots_agent/lib/atp-linux.so | grep -q ' T fsan_perror'
  The Last Command Succeeded
