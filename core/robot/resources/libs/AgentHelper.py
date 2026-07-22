# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import time
import shlex
import paramiko
from robot.libraries.BuiltIn import BuiltIn
from robot.api.deco import keyword, library
from robot.api import logger

from deployment_paths import resolve_deployment_dir_for_user_posix

@library(scope='TEST')
class AgentHelper:
    """
    Robot Framework library for managing the atperf-agent group controller process on a remote target via SSH.
    """

    def __init__(self):
        self._gc_pid = None
        self._writer_pid = None
        self._fifo_path = None
        self._stdout = []
        self._stderr = []
        self._tmpdir = None
        self._built_in = BuiltIn()
        
        # SSH connection
        self._ssh_host = None
        self._ssh_user = None
        self._ssh_key = None
        self._ssh_client = paramiko.SSHClient()

    def __del__(self):
        self._cleanup()
        
        if self._ssh_client:
            self._ssh_client.close()

    def _set_global(self, name, value):
        self._built_in.set_global_variable(f'${{{name}}}', value)

    def _get_var(self, name, default=None):
        return self._built_in.get_variable_value(f'${{{name}}}', default)

    def _run_remote(self, cmd: str):
        _, stdout, _ = self._ssh_client.exec_command(cmd, timeout=3)
        rc = stdout.channel.recv_exit_status()
        return rc, stdout.read().decode().strip()

    def _resolve_deployment_dir_for_user(self, username: str) -> str:
        """
        Resolve the atperf deployment directory for a specific user.
        Mirrors _resolve_deployment_dir_for_user in TargetPlatform.py.
        """
        product_bin_name = self._get_var('PRODUCT_BINARY_NAME')
        return resolve_deployment_dir_for_user_posix(self._run_remote, username, product_bin_name)

    def _resolve_agent_binary(self, username: str = ""):
        agent_name = self._get_var('AGENT_BINARY_FILE_NAME', 'atperf-agent').strip()
        if not agent_name:
            raise AssertionError("Variable ${AGENT_BINARY_FILE_NAME} not set.")
        found = None

        if username:
            atperf_dir = self._resolve_deployment_dir_for_user(username)
            cmd = f'sudo -u {username} find {atperf_dir} -name {agent_name} -type f'
        else:
            atperf_dir = self._get_var('ATPERF_DIR', '').strip()
            if not atperf_dir:
                raise AssertionError("Variable ${ATPERF_DIR} not set.")
            cmd = f'find {atperf_dir} -name {agent_name} -type f'

        _, stdout, _ = self._ssh_client.exec_command(cmd, timeout=3)
        found = stdout.read().decode().strip()
        if not found:
            raise AssertionError(f"Could not find '{agent_name}' in '{atperf_dir}' on target.")
        return found.splitlines()[0]  # Take the first match

    def _connect_ssh(self):
        tp = self._ssh_client.get_transport()
        if tp and tp.is_active():
            return
        
        self._ssh_host = self._get_var('G_TARGET_HOST', '').strip()
        self._ssh_user = self._get_var('G_TARGET_USER', '').strip()
        self._ssh_key = self._get_var('G_TARGET_KEY', '').strip()

        if not self._ssh_host or not self._ssh_user or not self._ssh_key:
            raise AssertionError("SSH connection details are not fully set in variables.")

        self._ssh_client = paramiko.SSHClient()
        self._ssh_client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        self._ssh_client.connect(self._ssh_host, username=self._ssh_user, key_filename=self._ssh_key)

        _, stdout, _ = self._ssh_client.exec_command('echo connected')
        if stdout.read().decode().strip() != 'connected':
            raise AssertionError("Failed to verify SSH connection to target.")
        else:
            print(f"Connected to target {self._ssh_host} as {self._ssh_user}")
    
    def _cleanup(self):
        """
        Cleans up any leftover temp directories on the target.
        """
        if not self._tmpdir:
            return

        self._connect_ssh()

        # Ignore if directory does not exist
        _, stdout, _ = self._ssh_client.exec_command(f'find {self._tmpdir} -type d -maxdepth 0', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            return

        _, stdout, stderr = self._ssh_client.exec_command(f'rm -rf {self._tmpdir}', timeout=3)
        exit_status = stdout.channel.recv_exit_status()
        if exit_status != 0:
            err_msg = stderr.read().decode().strip()
            raise AssertionError(f"Failed to remove temp directory {self._tmpdir} on target: {err_msg}")

    @keyword('Resolve Target Agent Binary Path')
    def resolve_target_agent_binary_path(self, username: str = ""):
        """
        Resolves and returns the absolute path to the target agent binary on the target.
        If username is provided, resolves the path within that user's deployment directory.
        """
        self._connect_ssh()
        agent_binary_path = self._resolve_agent_binary(username)
        if not agent_binary_path:
            raise AssertionError("Target agent binary path must not be empty.")
        return agent_binary_path

    @keyword('Group Controller Running')
    def group_controller_running(self):
        """
        Returns True if the group controller process is still running.
        """
        if not self._gc_pid:
            return False
        _, stdout, _ = self._ssh_client.exec_command(f'ps -p {self._gc_pid}', timeout=3)
        output = stdout.read().decode().strip()
        return self._gc_pid in output

    @keyword('Start Group Controller With Command')
    def start_group_controller_with_command(self, *cmd, sudo=False):
        """
        Creates a FIFO, launches the group controller process with that FIFO as stdin.
        Keeps the write end open so the process runs until Stop Group Controller is called.
        """
        self._connect_ssh()

        if self._gc_pid:
            _, stdout, _ = self._ssh_client.exec_command(f'ps -p {self._gc_pid}', timeout=3)
            output = stdout.read().decode().strip()
            if self._gc_pid in output:
                raise AssertionError("Group controller is already running.")
            else:
                self._gc_pid = None

        binary_path = self._resolve_agent_binary()
        _, stdout, _ = self._ssh_client.exec_command(f'test -x {binary_path}', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            raise AssertionError(f"Agent binary '{binary_path}' is not executable on target.")
    
        # Create tempdir, log file and FIFO on target
        _, stdout, _ = self._ssh_client.exec_command('mktemp -d', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            raise AssertionError("Failed to create temporary directory on target.")
        self._tmpdir = stdout.read().decode().strip()

        _, stdout, _ = self._ssh_client.exec_command(f'touch {self._tmpdir}/gc_stdout.log {self._tmpdir}/gc_stderr.log', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            raise AssertionError("Failed to create log files on target.")
        
        _, stdout, _ = self._ssh_client.exec_command(f'mkfifo {self._tmpdir}/gc_stdin_fifo', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            raise AssertionError("Failed to create FIFO on target.")
        self._fifo_path = f"{self._tmpdir}/gc_stdin_fifo"

        # Open the FIFO write end
        _, stdout, _ = self._ssh_client.exec_command(f'tail -f /dev/null > {self._fifo_path} 2>&1 &', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            raise AssertionError("Failed to start FIFO keeper process on target.")
        
        _, stdout, _ = self._ssh_client.exec_command(f'pgrep -f "tail -f /dev/null > {self._fifo_path}"', timeout=3)
        pids = stdout.read().decode().strip().splitlines()
        if not pids:
            raise AssertionError("Failed to retrieve PID of the FIFO keeper process.")
        self._writer_pid = pids[0]

        # Start the group controller process
        cmd = ' '.join(cmd)
        full_cmd = f'bash -lc "{binary_path} start-group-controller -- {cmd} < {self._fifo_path} 1>> {self._tmpdir}/gc_stdout.log 2>> {self._tmpdir}/gc_stderr.log" &'
        if sudo:
            full_cmd = f'sudo -n {full_cmd}'
        logger.info(f'Full command: {full_cmd}')
        _, stdout, _ = self._ssh_client.exec_command(full_cmd, timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            raise AssertionError(f"Failed to {full_cmd}. Check {self._tmpdir}/gc_stderr.log for details.")
        time.sleep(1)

        # Get its PID
        _, stdout, _ = self._ssh_client.exec_command(f'pgrep -f "{binary_path} start-group-controller"', timeout=3)
        pids = stdout.read().decode().strip().splitlines()
        if not pids:
            raise AssertionError(f"Failed to retrieve PID of the group controller process. Check {self._tmpdir}/gc_stderr.log for details. Or {full_cmd}")
        
        # Pick the PID with its /proc/[pid]/cmdline starting exactly with the binary path and start-group-controller
        valid_pid = None
        for pid in pids:
            _, stdout, _ = self._ssh_client.exec_command(f'cat /proc/{pid}/cmdline', timeout=3)
            cmdline = stdout.read().decode().strip().replace('\x00', ' ')
            if cmdline.startswith(f"{binary_path} start-group-controller"):
                valid_pid = pid
                break

        if not valid_pid:
            raise AssertionError(f"Failed to find valid PID for group controller process. Check {self._tmpdir}/gc_stderr.log for details.")

        self._gc_pid = valid_pid

    @keyword('Start Group Controller With Command And Wait For Exit')
    def start_group_controller_with_command_and_wait_for_exit(self, *cmd, sudo=False):
        """
        Starts group controller with the wait-for-child flag and waits for the process exit code
        """
        self._connect_ssh()

        if self._gc_pid:
            _, stdout, _ = self._ssh_client.exec_command(f'ps -p {self._gc_pid}', timeout=3)
            output = stdout.read().decode().strip()
            if self._gc_pid in output:
                raise AssertionError("Group controller is already running.")
            else:
                self._gc_pid = None

        binary_path = self._resolve_agent_binary()
        _, stdout, _ = self._ssh_client.exec_command(f'test -x {binary_path}', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            raise AssertionError(f"Agent binary '{binary_path}' is not executable on target.")

        # Create tempdir, log file
        _, stdout, _ = self._ssh_client.exec_command('mktemp -d', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            raise AssertionError("Failed to create temporary directory on target.")
        self._tmpdir = stdout.read().decode().strip()

        _, stdout, _ = self._ssh_client.exec_command(f'touch {self._tmpdir}/gc_stdout.log {self._tmpdir}/gc_stderr.log', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            raise AssertionError("Failed to create log files on target.")

        # Start the group controller process
        cmd = ' '.join(cmd)
        full_cmd = f'bash -lc "exec {binary_path} start-group-controller --wait-for-child -- {cmd} 1>> {self._tmpdir}/gc_stdout.log 2>> {self._tmpdir}/gc_stderr.log"'
        if sudo:
            full_cmd = f'sudo -n {full_cmd}'
        logger.info(f'Full command: {full_cmd}')
        _, stdout, _ = self._ssh_client.exec_command(full_cmd, timeout=10)
        exit_code = stdout.channel.recv_exit_status()

        return exit_code

    @keyword('Stop Group Controller')
    def stop_group_controller(self, timeout=5):
        """
        Stops the group controller process by closing the FIFO write end.
        """
        if not self._gc_pid:
            print("Group controller is not running.")
            return
        
        if self._writer_pid:
            _, stdout, _ = self._ssh_client.exec_command(f'kill {self._writer_pid}', timeout=3)
            if stdout.channel.recv_exit_status() != 0:
                raise AssertionError("Failed to kill FIFO writer process on target.")

        end_time = time.time() + timeout
        while time.time() < end_time:
            _, stdout, _ = self._ssh_client.exec_command(f'ps -p {self._gc_pid}', timeout=3)
            output = stdout.read().decode().strip()
            if self._gc_pid not in output:
                break
            time.sleep(1)
        else:
            raise AssertionError("Group controller did not exit within the timeout period.")

    @keyword('Group Controller Stdout Should Contain')
    def group_controller_stdout_should_contain(self, text, timeout=5):
        """
        Asserts that the group controller's stdout log contains the specified text within the timeout.
        """
        if not self._tmpdir:
            raise AssertionError("Group controller is not running or temp directory is not set.")
        
        end_time = time.time() + timeout
        while time.time() < end_time:
            _, stdout, _ = self._ssh_client.exec_command(f'grep -q "{text}" {self._tmpdir}/gc_stdout.log && echo found', timeout=3)
            if 'found' in stdout.read().decode():
                return
            time.sleep(1)
        
        raise AssertionError(f"Text '{text}' not found in group controller stdout within {timeout} seconds.")  

    @keyword('Group Controller Stderr Should Contain')
    def group_controller_stderr_should_contain(self, text, timeout=5):
        """
        Asserts that the group controller's stderr log contains the specified text within the timeout.
        """
        if not self._tmpdir:
            raise AssertionError("Group controller is not running or temp directory is not set.")
        
        end_time = time.time() + timeout
        while time.time() < end_time:
            _, stdout, _ = self._ssh_client.exec_command(f'grep -q "{text}" {self._tmpdir}/gc_stderr.log && echo found', timeout=3)
            if 'found' in stdout.read().decode():
                return
            time.sleep(1)
        
        raise AssertionError(f"Text '{text}' not found in group controller stderr within {timeout} seconds.")
    
    @keyword('Group Controller Get Process Count')
    def group_controller_get_process_count(self):
        """
        Returns the number of processes started by the group controller.
        """
        if not self._gc_pid:
            return 0

        _, stdout, _ = self._ssh_client.exec_command(f"ps --ppid {self._gc_pid} --no-headers | wc -l", timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            return 0

        output = stdout.read().decode().strip()
        try:
            return int(output)
        except ValueError:
            return 0
    
    @keyword('Group Controller Cgroupv2 Available')
    def group_controller_cgroupv2_available(self):
        """
        Returns True if cgroupv2 is available on the target system.
        """
        self._connect_ssh()
        _, stdout, _ = self._ssh_client.exec_command('stat -fc %T /sys/fs/cgroup', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            return False
        output = stdout.read().decode().strip()
        return output == 'cgroup2fs'

    @keyword('Group Controller Cgroupv2 Process List')
    def group_controller_cgroupv2_process_list(self):
        """
        Returns the list of processes in the cgroupv2 created by the group controller.
        """
        if not self._gc_pid:
            return []

        agent_binary_name = self._get_var('AGENT_BINARY_NAME')

        # The path of cgroupv2 depends on target platform and configuration.
        # Systemd-based distros, by default, mounts it on /sys/fs/cgroup.
        # Since our targets are mostly systemd-based, we assume that here.
        # See https://github.com/systemd/systemd/blob/main/docs/CGROUP_DELEGATION.md
        cgroup_path = f"/sys/fs/cgroup/{agent_binary_name}-gc-{self._gc_pid}"
        _, stdout, _ = self._ssh_client.exec_command(f"test -d {cgroup_path} && echo {cgroup_path}", timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            return []
        cgroup_path = stdout.read().decode().strip()

        _, stdout, _ = self._ssh_client.exec_command(f'cat {cgroup_path}/cgroup.procs', timeout=3)
        if stdout.channel.recv_exit_status() != 0:
            return []
        output = stdout.read().decode().strip()
        return output.splitlines() if output else []
