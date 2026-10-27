# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Verify disposable-target transport without contacting a real machine."""

import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from pathways_target_access import TargetAccess, TargetAccessError


class TargetAccessTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def test_connection_exports_real_target_without_a_jump(self):
        key = self.root / "input_key"
        key.write_text("target credential")
        target = TargetAccess({"host": "target", "port": 2222, "user": "ubuntu", "key": str(key)})
        with patch("pathways_target_access.subprocess.run", return_value=Mock(stdout=b"trusted key")):
            route = target.export_connection(self.root / "agent/.ssh")
        self.assertEqual("target", route["host"])
        self.assertEqual(2222, route["port"])
        self.assertNotIn("jumps", route)
        self.assertEqual("target credential", (self.root / "agent/.ssh/target_key").read_text())

    def test_ssh_failures_report_stderr_without_dumping_commands_or_resetting_target(self):
        target = TargetAccess({"host": "target", "port": 22, "user": "ubuntu", "key": "/key"})
        for stderr in (b"Pathways requires a clean target: /workspace already exists\n", b"Permission denied\n", b""):
            with self.subTest(stderr=stderr):
                failed = subprocess.CompletedProcess("ssh", 1, stdout=b"", stderr=stderr)
                with patch("pathways_target_access.subprocess.run", return_value=failed) as run:
                    with self.assertRaises(TargetAccessError) as raised:
                        target.ssh("test ! -e /workspace")
                self.assertEqual(f"SSH command failed on ubuntu@target:22 (exit 1): "
                                 f"{stderr.decode().strip() or 'no stderr output'}", str(raised.exception))
                run.assert_called_once()
