# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import base64
import os
import shlex

from robot.api import logger
from robot.api.deco import keyword, library
from robot.libraries.BuiltIn import BuiltIn

from CommandService import CommandResult, CommandService
from deployment_paths import (resolve_deployment_dir_for_user_posix,
                              resolve_deployment_dir_for_user_windows)


@library(scope="SUITE")
class TargetPlatform:
    """
    TargetPlatform provides keywords for managing and verifying the state of test targets.
    It supports implementations for different target platforms.
    """

    def __init__(self) -> None:
        self._built_in = BuiltIn()
        self._target = None

    def assert_initialised(self):
        if self._target is None or self._target.target_config is None:
            raise AssertionError("The target platform is not initialised. Call 'Configure Target Platform' to initialise the target platform.")

    def _update_robot_variables(self) -> None:
        self.assert_initialised()

        variables = {
            "ATPERF_DIR": "deployment_dir",
            "ATPERF_TOOLS_DIR": "tools_dir",
            "COLLECTOR_BINARY_FILE_NAME": "collector_binary",
            "AGENT_BINARY_FILE_NAME": "agent_binary",
            "JITDUMP_JVM_BINARY_FILE_NAME": "jitdump_jvm_binary",
            "DOTNET_AGENT_BINARY_FILE_NAME": "dotnet_agent_binary",
            "EXPECTED_PID": "expected_first_pid",
            "EXPECTED_PROCESS_NAME": "expected_first_process",
        }

        for var, config in variables.items():
            value = self._target.target_config.get(config, "")
            if value is None:
                raise KeyError(f"Config '{config}' not found in target configuration.")
            self._built_in.set_global_variable(f"${{{var}}}", value)

    @keyword("Configure Target Platform")
    def configure_target_platform(self, target_os: str) -> None:
        target_os = target_os.lower()
        service = CommandService(self._built_in)
        platforms = {
            "linux": LinuxTarget,
            "windows": WindowsTarget,
        }
        platform = platforms.get(target_os)
        if platform is None:
            raise ValueError(f"operating system '{target_os}' is not supported.")
        product_bin_name = self._built_in.get_variable_value(f'${{PRODUCT_BINARY_NAME}}', "")
        if not product_bin_name:
            raise ValueError("PRODUCT_BINARY_NAME variable is undefined")
        agent_bin_name = self._built_in.get_variable_value(f'${{AGENT_BINARY_NAME}}', "")
        if not agent_bin_name:
            raise ValueError("AGENT_BINARY_NAME variable is undefined")
        self._target = platform(service, product_bin_name, agent_bin_name)
        self._update_robot_variables()

    @keyword("Check Tool Deployed")
    def check_deployed(self, filename: str, username: str = "") -> CommandResult:
        self.assert_initialised()
        return self._target.check_tool_deployed(filename, username)

    @keyword("Check Tool Not Deployed")
    def check_tool_not_deployed(self, filename: str, username: str = "") -> CommandResult:
        self.assert_initialised()
        return self._target.check_tool_not_deployed(filename, username)

    @keyword("Kill Target Process")
    def kill_target_process(self, process_name: str) -> CommandResult:
        self.assert_initialised()
        return self._target.kill_target_process(process_name)

    @keyword("Target Process Is Running")
    def target_process_is_running(self, process_name: str) -> CommandResult:
        self.assert_initialised()
        return self._target.target_process_is_running(process_name)
    
    @keyword("Target Process Command Is Running As User")
    def target_process_command_is_running_as_user(
        self,
        match_string: str,
        user_name: str,
    ) -> CommandResult:
        self.assert_initialised()
        return self._target.target_process_command_is_running_as_user(
            match_string,
            user_name,
        )

    @keyword("Target Process Is Not Running")
    def target_process_is_not_running(self, process_name: str) -> CommandResult:
        self.assert_initialised()
        return self._target.target_process_is_not_running(process_name)

    @keyword("Set Process Capability")
    def set_process_capability(self, binary_path: str, capabilities: str) -> CommandResult:
        self.assert_initialised()
        return self._target.set_process_capability(binary_path, capabilities)

    @keyword("Get Process Capability")
    def get_process_capability(self, binary_path: str) -> CommandResult:
        self.assert_initialised()
        return self._target.get_process_capability(binary_path)

    @keyword("Clear Process Capabilities")
    def clear_process_capabilities(self, binary_path: str) -> CommandResult:
        self.assert_initialised()
        return self._target.remove_process_capability(binary_path)

    @keyword("Set Target Platform Perf Event Paranoid")
    def set_target_platform_perf_event_paranoid(self, value: str) -> CommandResult:
        self.assert_initialised()
        return self._target.set_perf_event_paranoid(value)

    @keyword("Get Target Platform Perf Event Paranoid")
    def get_target_platform_perf_event_paranoid(self) -> CommandResult:
        self.assert_initialised()
        return self._target.get_perf_event_paranoid()

    @keyword("Target Platform Perf Event Paranoid Should Be")
    def target_platform_perf_event_paranoid_should_be(self, value: str) -> None:
        self.assert_initialised()
        set_result = self._target.set_perf_event_paranoid(value)
        if set_result.rc != 0:
            details = set_result.stderr or set_result.stdout
            raise AssertionError(f"Setting perf_event_paranoid failed with rc={set_result.rc}: {details}")
        get_result = self._target.get_perf_event_paranoid()
        if get_result.rc != 0:
            details = get_result.stderr or get_result.stdout
            raise AssertionError(f"Getting perf_event_paranoid failed with rc={get_result.rc}: {details}")
        actual = (get_result.stdout or "").strip()
        self._built_in.should_be_equal_as_integers(actual, value)

    @keyword("Create Target File")
    def create_target_file(self, path: str) -> CommandResult:
        self.assert_initialised()
        return self._target.create_file(path)

    @keyword("Write Target Temp File")
    def write_target_temp_file(self, filename_suffix: str, content: str) -> CommandResult:
        self.assert_initialised()
        return self._target.write_temp_file(filename_suffix, content)

    @keyword("Check Target File Not Executable")
    def check_target_file_not_executable(self, path: str) -> CommandResult:
        self.assert_initialised()
        return self._target.check_file_not_executable(path)

    @keyword("Remove Target File")
    def remove_target_file(self, path: str) -> CommandResult:
        self.assert_initialised()
        return self._target.remove_file(path)

    @keyword("Remove Target Directory")
    def remove_target_directory(self, path: str) -> CommandResult:
        self.assert_initialised()
        return self._target.remove_target_directory(path)

    @keyword("Remove Target Deployment Directories")
    def remove_target_deployment_directories(self) -> CommandResult:
        self.assert_initialised()
        return self._target.remove_target_deployment_directories()

    @keyword("Check Target Directory Exists")
    def check_target_directory_exists(self, path: str) -> CommandResult:
        self.assert_initialised()
        return self._target.check_target_directory_exists(path)

    @keyword("Check Target Directory Does Not Exist")
    def check_target_directory_does_not_exist(self, path: str) -> CommandResult:
        self.assert_initialised()
        return self._target.check_target_directory_does_not_exist(path)

    @keyword("Target Run Directories Exist")
    def target_run_directories_exist(self) -> CommandResult:
        self.assert_initialised()
        return self._target.target_run_directories_exist()

    @keyword("Remove Target Run Directories")
    def remove_target_run_directories(self) -> CommandResult:
        self.assert_initialised()
        return self._target.remove_target_run_directories()

    @keyword("Count Target Processes Matching Command")
    def count_target_processes_matching_command(self, match_string: str, match_on_proc_name: bool) -> CommandResult:
        self.assert_initialised()
        return self._target.count_target_processes_matching_command(match_string, match_on_proc_name)

    @keyword("Get Home Dir On Target")
    def get_home_dir_on_target(self) -> CommandResult:
        self.assert_initialised()
        return self._target.get_home_dir_on_target()

class LinuxTarget:
    """
    LinuxTarget is the Linux implementation of target operations.
    """

    def __init__(self, service: CommandService, product_binary_name: str, agent_binary_name: str) -> None:
        self._service = service
        self._product_bin_name = product_binary_name
        deployment_dir = self._resolve_deployment_dir()
        self.target_config = {
            "deployment_dir": deployment_dir,
            "tools_dir": f"{deployment_dir}/tools",
            "run_dirs_glob": f"/tmp/{agent_binary_name}-*",
            "collector_binary": "sl-collect",
            "agent_binary": agent_binary_name,
            "jitdump_jvm_binary": "jitdump-jvm",
            "dotnet_agent_binary": "jitdump-dotnet",
            "expected_first_pid": 1,
            "expected_first_process": "systemd",
        }

    def _run(self, command: str) -> CommandResult:
        return self._service.run_target_command(command)

    def _resolve_deployment_dir(self) -> str:
        """Resolve the apx deployment directory for the current SSH user.

        Mirrors ResolveToolsBaseDir in apap-engine/conductor/target_path_resolver.go:
        - Uses HOME/.local/share/apx when HOME is writable.
        - Falls back to /tmp/apx/<username> otherwise.
        """
        home_result = self._run("printenv HOME")
        if home_result.rc == 0:
            home = home_result.stdout.strip()
            if home:
                writable_result = self._run(f"test -w {shlex.quote(home)}")
                if writable_result.rc == 0:
                    return f"{home}/.local/share/{self._product_bin_name}"

        whoami_result = self._run("whoami")
        if whoami_result.rc == 0 and whoami_result.stdout.strip():
            return f"/tmp/{self._product_bin_name}/{whoami_result.stdout.strip()}"

        return f"/tmp/{self._product_bin_name}"

    def _resolve_deployment_dir_for_user(self, username: str) -> str:
        """Resolve the apx deployment directory for a specific user.

        Uses getent passwd to find the user's home directory and checks
        writability via sudo -u so that permission errors are avoided.
        Mirrors ResolveToolsBaseDir in apap-engine/conductor/target_path_resolver.go.
        """
        def runner(cmd: str):
            res = self._run(cmd)
            return res.rc, res.stdout.strip()

        return resolve_deployment_dir_for_user_posix(runner, username, self._product_bin_name)

    def check_tool_deployed(self, tool_name: str, username: str = "") -> CommandResult:
        if username:
            tools_dir = f"{self._resolve_deployment_dir_for_user(username)}/tools"
            command = f"sudo find {shlex.quote(tools_dir)} -name {shlex.quote(tool_name)} | grep ."
        else:
            tools_dir = f"{self._resolve_deployment_dir()}/tools"
            command = f"find {shlex.quote(tools_dir)} -name {shlex.quote(tool_name)} | grep ."
        return self._run(command)

    def check_tool_not_deployed(self, tool_name: str, username: str = "") -> CommandResult:
        if username:
            tools_dir = f"{self._resolve_deployment_dir_for_user(username)}/tools"
            command = f"! sudo find {shlex.quote(tools_dir)} -name {shlex.quote(tool_name)} | grep ."
        else:
            tools_dir = f"{self._resolve_deployment_dir()}/tools"
            command = f"! find {shlex.quote(tools_dir)} -name {shlex.quote(tool_name)} | grep ."
        return self._run(command)

    def kill_target_process(self, process_name: str) -> CommandResult:
        command = f"sudo pkill -x {shlex.quote(process_name)}"
        return self._run(command)

    def target_process_is_running(self, process_name: str) -> CommandResult:
        command = f"ps aux | grep {shlex.quote(process_name)} | grep -v grep"
        return self._run(command)

    def target_process_command_is_running_as_user(self, match_string: str, user_name: str) -> CommandResult:
        if not match_string:
            raise ValueError("match_string must not be empty.")
        command = f"pgrep -u {shlex.quote(user_name)} -f -- {shlex.quote(match_string)}"
        return self._run(command)

    def target_process_is_not_running(self, process_name: str) -> CommandResult:
        command = f"! ps aux | grep {shlex.quote(process_name)} | grep -v grep"
        return self._run(command)

    def set_process_capability(self, binary_path: str, capabilities: str) -> CommandResult:
        path = "" if binary_path is None else str(binary_path).strip()
        if not path:
            raise ValueError("binary_path must not be empty.")
        if not capabilities:
            raise ValueError("capabilities must not be empty.")
        command = f"sudo setcap {shlex.quote(capabilities)} {shlex.quote(path)}"
        return self._run(command)

    def get_process_capability(self, binary_path: str) -> CommandResult:
        path = "" if binary_path is None else str(binary_path).strip()
        if not path:
            raise ValueError("binary_path must not be empty.")
        command = f"sudo getcap {shlex.quote(path)}"
        return self._run(command)

    def remove_process_capability(self, binary_path: str) -> CommandResult:
        path = "" if binary_path is None else str(binary_path).strip()
        if not path:
            raise ValueError("binary_path must not be empty.")
        command = f"sudo setcap -r {shlex.quote(path)}"
        return self._run(command)

    def set_perf_event_paranoid(self, value: str) -> CommandResult:
        command_value = "" if value is None else str(value).strip()
        if command_value == "":
            raise ValueError("value must not be empty.")
        write_cmd = f"echo {command_value} > /proc/sys/kernel/perf_event_paranoid"
        command = f"sudo sh -c {shlex.quote(write_cmd)}"
        return self._run(command)

    def get_perf_event_paranoid(self) -> CommandResult:
        command = "cat /proc/sys/kernel/perf_event_paranoid"
        return self._run(command)

    def create_file(self, path: str) -> CommandResult:
        command = f"touch {shlex.quote(path)}"
        return self._run(command)

    def write_temp_file(self, filename_suffix, content: str) -> CommandResult:
        encoded = base64.b64encode(content.encode()).decode()
        filename_pattern = f"atp-XXXXXX-{filename_suffix}"
        command = (
            f"temp_path=$(mktemp /tmp/{shlex.quote(filename_pattern)}) && "
            f"printf '%s' {shlex.quote(encoded)} | base64 -d > $temp_path && "
            "echo $temp_path"
        )
        return self._run(command)

    def check_file_not_executable(self, path: str) -> CommandResult:
        quoted_path = shlex.quote(path)
        command = f"test ! -x {quoted_path} -o -d {quoted_path}"
        return self._run(command)

    def remove_file(self, path: str) -> CommandResult:
        command = f"rm -f {shlex.quote(path)}"
        return self._run(command)

    def remove_target_directory(self, path: str) -> CommandResult:
        command = f"sudo rm -rf {shlex.quote(path)}"
        return self._run(command)

    def remove_target_deployment_directories(self) -> CommandResult:
        base_dir = self.target_config["deployment_dir"].rstrip("/")
        command = f"sudo rm -rf {shlex.quote(base_dir)}*"
        return self._run(command)

    def check_target_directory_exists(self, path: str) -> CommandResult:
        command = f"test -d {shlex.quote(path)}"
        return self._run(command)

    def check_target_directory_does_not_exist(self, path: str) -> CommandResult:
        command = f"test ! -d {shlex.quote(path)}"
        return self._run(command)

    def target_run_directories_exist(self) -> CommandResult:
        command = f"sudo ls {self.target_config['run_dirs_glob']} >/dev/null 2>&1 && echo NOT_EMPTY || echo EMPTY"
        return self._run(command)

    def remove_target_run_directories(self) -> CommandResult:
        command = f"sudo rm -rf {self.target_config['run_dirs_glob']}"
        return self._run(command)

    def count_target_processes_matching_command(self, match_string: str, match_on_proc_name: bool) -> CommandResult:
        flags = "-xc" if match_on_proc_name else "-fc"
        command = f"pgrep {flags} {shlex.quote(match_string)}"
        return self._run(command)

    def get_home_dir_on_target(self) -> CommandResult:
        command = "printenv HOME"
        return self._run(command)


class WindowsTarget:
    """
    WindowsTarget is the Windows implementation of target operations.
    """

    def __init__(self, service: CommandService, product_binary_name: str, agent_binary_name: str) -> None:
        self._service = service
        self._temp_dir = self.get_temp_directory()
        self._product_bin_name = product_binary_name
        deployment_dir = self._resolve_deployment_dir(self._temp_dir)
        self.target_config = {
            "deployment_dir": deployment_dir,
            "tools_dir": f"{deployment_dir}/tools",
            "run_dirs_glob": f"{self._temp_dir}/{agent_binary_name}-*",
            "collector_binary": "sl-collect.exe",
            "agent_binary": f"{agent_binary_name}.exe",
            "jitdump_jvm_binary": "jitdump-jvm.exe",
            "dotnet_agent_binary": "jitdump-dotnet.exe",
            "expected_first_pid": 0,
            "expected_first_process": "[System Process]",
        }

    @staticmethod
    def _normalize_path(path: str) -> str:
        if not path:
            return ""
        return str(path).strip().replace("/", "\\")

    @staticmethod
    def _ps_quote(value: str) -> str:
        escaped = value.replace("'", "''")
        return f"'{escaped}'"

    def _quoted_path(self, path: str) -> str:
        return self._ps_quote(self._normalize_path(path))

    def _quoted_config(self, key: str) -> str:
        return self._ps_quote(self._config_path(key))

    @staticmethod
    def _check_for_output(expression: str, expect_output: bool = True) -> str:
        return (
            f"$result = {expression};"
            f"if ($result) {{ Write-Output $result; exit {0 if expect_output else 1} }} "
            f"else {{ Write-Output 'No matches found'; exit {1 if expect_output else 0} }}"
        )

    def _resolve_deployment_dir(self, temp_dir: str) -> str:
        """
        Resolve the apx deployment directory for the current user on Windows.

        Mirrors ResolveToolsBaseDir in apap-engine/conductor/target_path_resolver.go:
        - Use LOCALAPPDATA/apx if writable.
        - Fallback to TEMP/apx/<username>.
        """
        local_appdata = self._get_local_appdata()
        if local_appdata and self._is_path_writable(local_appdata):
            return f"{local_appdata}/{self._product_bin_name}"

        username = self._get_username()
        if username:
            return f"{temp_dir}/{self._product_bin_name}/{username}"

        return f"{temp_dir}/{self._product_bin_name}"

    def _resolve_deployment_dir_for_user(self, username: str, temp_dir: str) -> str:
        """
        Resolve the apx deployment directory for a specific user on Windows.
        """
        return resolve_deployment_dir_for_user_windows(
            self._get_user_profile_path, self._is_path_writable, temp_dir, username, self._product_bin_name
        )

    def _get_username(self) -> str:
        result = self._run_powershell("$env:USERNAME")
        if result.rc == 0:
            return result.stdout.strip()
        return ""

    def _get_local_appdata(self) -> str:
        result = self._run_powershell("$env:LOCALAPPDATA")
        if result.rc == 0:
            return result.stdout.strip().replace("\\", "/").rstrip("/")
        return ""

    def _get_user_profile_path(self, username: str) -> str:
        script = (
            "$u = " + self._ps_quote(username) + ";"
            "$profile = Get-CimInstance -ClassName Win32_UserProfile | "
            "Where-Object { $_.LocalPath -and $_.LocalPath.Split('\\\\')[-1] -ieq $u } | "
            "Select-Object -First 1 -ExpandProperty LocalPath;"
            "if ($profile) { Write-Output $profile }"
        )
        result = self._run_powershell(script)
        if result.rc == 0 and result.stdout.strip():
            return result.stdout.strip().replace("\\", "/").rstrip("/")
        return ""

    def _is_path_writable(self, path: str) -> bool:
        script = (
            f"$p = {self._ps_quote(self._normalize_path(path))}; "
            f"$tmp = Join-Path $p '{self._product_bin_name}-writecheck.tmp'; "
            "try { Set-Content -Path $tmp -Value '' -ErrorAction Stop; "
            "Remove-Item $tmp -Force; exit 0 } catch { exit 1 }"
        )
        result = self._run_powershell(script)
        return result.rc == 0

    def _run_powershell(self, script: str) -> CommandResult:
        logger.info(f"PowerShell script:\n{script}")
        script = (
            "$ProgressPreference = 'SilentlyContinue';"
            "$PSDefaultParameterValues['*:Encoding'] = 'utf8';"
            "$OutputEncoding = [Console]::OutputEncoding = "
            "[System.Text.UTF8Encoding]::new($false);"
            + script
        )
        # Use encoded command to avoid quoting issues over SSH
        encoded = base64.b64encode(script.encode("utf-16le")).decode("ascii")
        command = f"powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand {encoded}"
        return self._service.run_target_command(command)

    def get_temp_directory(self) -> str:
        script = (
            "$temp = $env:TEMP;"
            "if ([string]::IsNullOrWhiteSpace($temp)) { "
            "Write-Error 'TEMP environment variable is not set'; exit 1 "
            "};"
            "Write-Output $temp"
        )
        result = self._run_powershell(script)
        if result.rc != 0:
            raise RuntimeError(f"Failed to read TEMP directory: {result.stderr or result.stdout}")
        return result.stdout.strip().replace("\\", "/").rstrip('/')

    @staticmethod
    def _normalize_process_name(name: str) -> str:
        lowered = name.lower()
        if lowered.endswith(".exe"):
            return name[:-4]
        return name

    def _config_path(self, key: str) -> str:
        return self._normalize_path(self.target_config[key])

    def check_tool_deployed(self, tool_name: str, username: str = "") -> CommandResult:
        if username:
            tools_dir = f"{self._resolve_deployment_dir_for_user(username, self._temp_dir)}/tools"
            quoted_dir = self._ps_quote(self._normalize_path(tools_dir))
        else:
            quoted_dir = self._quoted_config('tools_dir')
        expression = (
            f"Get-ChildItem -Path {quoted_dir} "
            f"-Filter {self._ps_quote(tool_name)} -Recurse -ErrorAction SilentlyContinue"
        )
        script = self._check_for_output(expression)
        return self._run_powershell(script)

    def check_tool_not_deployed(self, tool_name: str, username: str = "") -> CommandResult:
        if username:
            tools_dir = f"{self._resolve_deployment_dir_for_user(username, self._temp_dir)}/tools"
            quoted_dir = self._ps_quote(self._normalize_path(tools_dir))
        else:
            quoted_dir = self._quoted_config('tools_dir')
        expression = (
            f"Get-ChildItem -Path {quoted_dir} "
            f"-Filter {self._ps_quote(tool_name)} -Recurse -ErrorAction SilentlyContinue"
        )
        script = self._check_for_output(expression, expect_output=False)
        return self._run_powershell(script)

    def kill_target_process(self, process_name: str) -> CommandResult:
        proc_name = self._ps_quote(self._normalize_process_name(process_name))
        script = (
            f"$proc = Get-Process -Name {proc_name} -ErrorAction SilentlyContinue;"
            "if (-not $proc) { Write-Output 'No matches found'; exit 1 };"
            "try { Write-Output $proc; $proc | Stop-Process -Force -ErrorAction Stop; exit 0 }"
            " catch { Write-Output $_; exit 1 }"
        )
        return self._run_powershell(script)

    def target_process_is_running(self, process_name: str) -> CommandResult:
        proc_name = self._ps_quote(self._normalize_process_name(process_name))
        expression = f"Get-Process -Name {proc_name} -ErrorAction SilentlyContinue"
        script = self._check_for_output(expression)
        return self._run_powershell(script)

    def target_process_command_is_running_as_user(
        self,
        match_string: str,
        user_name: str,
    ) -> CommandResult:
        raise NotImplementedError("Process command matching with user is not supported on Windows targets.")

    def target_process_is_not_running(self, process_name: str) -> CommandResult:
        proc_name = self._ps_quote(self._normalize_process_name(process_name))
        expression = f"Get-Process -Name {proc_name} -ErrorAction SilentlyContinue"
        script = self._check_for_output(expression, expect_output=False)
        return self._run_powershell(script)

    def set_process_capability(self, binary_path: str, capabilities: str) -> CommandResult:
        raise NotImplementedError("Setting process capabilities is not supported on Windows targets.")

    def get_process_capability(self, binary_path: str) -> CommandResult:
        raise NotImplementedError("Getting process capabilities is not supported on Windows targets.")

    def remove_process_capability(self, binary_path: str) -> CommandResult:
        raise NotImplementedError("Removing process capabilities is not supported on Windows targets.")

    def set_perf_event_paranoid(self, value: str) -> CommandResult:
        raise NotImplementedError("perf_event_paranoid is not supported on Windows targets.")

    def get_perf_event_paranoid(self) -> CommandResult:
        raise NotImplementedError("perf_event_paranoid is not supported on Windows targets.")

    def create_file(self, path: str) -> CommandResult:
        script = (
            f"New-Item -ItemType File -Path {self._quoted_path(path)} "
            "-Force -ErrorAction Stop | Out-Null"
        )
        return self._run_powershell(script)

    def write_temp_file(self, filename_suffix, content: str) -> CommandResult:
        raise NotImplementedError("write_temp_file is not yet implemented for Windows targets.")

    def check_file_not_executable(self, path: str) -> CommandResult:
        # Check if the path is a directory. If so, return success. If not,
        # check for common exe file extensions.
        script = (
            f"$path = {self._quoted_path(path)};"
            "if (Test-Path -Path $path -PathType Container) { exit 0 };"
            "if (-not (Test-Path -Path $path -PathType Leaf)) { exit 0 };"
            "$ext = [System.IO.Path]::GetExtension($path).ToLowerInvariant();"
            "$executableExts = @('.exe', '.com', '.bat', '.cmd', '.ps1');"
            "if ($executableExts -contains $ext) { exit 1 } else { exit 0 }"
        )
        return self._run_powershell(script)

    def remove_file(self, path: str) -> CommandResult:
        script = (
            f"Remove-Item -Path {self._quoted_path(path)} "
            "-Force -ErrorAction SilentlyContinue; exit 0"
        )
        return self._run_powershell(script)

    def remove_target_directory(self, path: str) -> CommandResult:
        script = (
            f"Remove-Item -Path {self._quoted_path(path)} "
            "-Recurse -Force -ErrorAction SilentlyContinue; exit 0"
        )
        return self._run_powershell(script)

    def remove_target_deployment_directories(self) -> CommandResult:
        base_dir = self._config_path("deployment_dir").rstrip("\\/")
        glob_path = f"{base_dir}*"
        script = (
            f"Remove-Item -Path {self._quoted_path(glob_path)} "
            "-Recurse -Force -ErrorAction SilentlyContinue; exit 0"
        )
        return self._run_powershell(script)

    def check_target_directory_exists(self, path: str) -> CommandResult:
        expression = f"Resolve-Path -Path {self._quoted_path(path)} -ErrorAction SilentlyContinue"
        script = self._check_for_output(expression)
        return self._run_powershell(script)

    def check_target_directory_does_not_exist(self, path: str) -> CommandResult:
        expression = f"Resolve-Path -Path {self._quoted_path(path)} -ErrorAction SilentlyContinue"
        script = self._check_for_output(expression, expect_output=False)
        return self._run_powershell(script)

    def target_run_directories_exist(self) -> CommandResult:
        run_glob = self._quoted_config('run_dirs_glob')
        script = (
            f"$dirs = Get-ChildItem -Path {run_glob} "
            "-Directory -ErrorAction SilentlyContinue;"
            "if ($dirs) { Write-Output 'NOT_EMPTY'; exit 0 } else { Write-Output 'EMPTY'; exit 0 }"
        )
        return self._run_powershell(script)

    def remove_target_run_directories(self) -> CommandResult:
        run_glob = self._quoted_config('run_dirs_glob')
        script = (
            f"$dirs = Get-ChildItem -Path {run_glob} "
            "-Directory -ErrorAction SilentlyContinue;"
            "if ($dirs) { $dirs | Remove-Item -Recurse -Force };"
            "exit 0"
        )
        return self._run_powershell(script)

    def count_target_processes_matching_command(self, match_string: str, match_on_proc_name: bool) -> CommandResult:
        if match_on_proc_name:
           raise NotImplementedError("Matching on process name is not yet implemented for Windows targets.")
        # Substring match against the full command line (similar to pgrep -fc on Linux)
        script = (
            f"$pattern = {self._ps_quote(match_string)}; "
            "$processes = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | "
            "Where-Object { "
            " $cmdLine = $_.CommandLine;"
            " if (-not $cmdLine) { return $false } "
            " return $cmdLine -cmatch $pattern "
            "}; "
            "($processes | Measure-Object).Count | Write-Output"
        )
        return self._run_powershell(script)

    def get_home_dir_on_target(self) -> CommandResult:
        command = "$env:USERPROFILE"
        return self._run_powershell(command)
