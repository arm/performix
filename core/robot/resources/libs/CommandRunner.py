# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from CommandService import CommandService
from robot.api.deco import keyword, library
from robot.libraries.BuiltIn import BuiltIn


@library(scope="TEST")
class CommandRunner:
    """
    CommandRunner provides Robot keywords for running commands on the host and target.
    """

    def __init__(self):
        self._built_in = BuiltIn()
        self._service = CommandService(self._built_in)

    @keyword("Run Host Command")
    def run_host_command(self, cmd):
        """
        Run a command on the host machine, i.e. the local machine.
        """
        return self._service.run_host_command(cmd)

    @keyword("Start Host Command")
    def start_host_command(self, cmd):
        """
        Start a command in the background on the host machine, i.e. the local machine.
        Returns a handle to the process that was started.
        """
        return self._service.start_host_command(cmd)

    @keyword("Run Target Command")
    def run_target_command(self, cmd):
        """
        Run a command on the test target machine (which could be
        remote or localhost).
        """
        return self._service.run_target_command(cmd)

    @keyword("Run Target Command In Background")
    def run_target_command_in_background(self, cmd):
        """
        Run a command on the test target machine detached from the SSH session and return its PID.
        """
        return self._service.run_target_command_in_background(cmd)
