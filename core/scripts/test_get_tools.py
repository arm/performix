#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import os
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT_DIR = Path(__file__).resolve().parent


def load_script(module_name: str, file_name: str):
    spec = importlib.util.spec_from_file_location(module_name, SCRIPT_DIR / file_name)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


get_tools = load_script("test_open_source_get_tools", "get-tools.py")


def create_sysutil_source(root: Path) -> Path:
    source_dir = root / "sysutil-timeline"
    (source_dir / "nested").mkdir(parents=True)
    (source_dir / "tests").mkdir()
    (source_dir / "__pycache__").mkdir()
    (source_dir / ".pytest_cache").mkdir()
    (source_dir / "env").mkdir()

    (source_dir / "sysutil-timeline.py").write_text("#!/usr/bin/env python3\n")
    (source_dir / "collector.py").write_text("COLLECTOR = True\n")
    (source_dir / "nested" / "config.json").write_text("{}\n")
    (source_dir / "tests" / "test_collector.py").write_text("assert True\n")
    (source_dir / "__pycache__" / "collector.pyc").write_bytes(b"cache")
    (source_dir / ".pytest_cache" / "README.md").write_text("cache\n")
    (source_dir / "env" / "python").write_text("environment\n")
    (source_dir / "loose.pyc").write_bytes(b"cache")
    return source_dir


class VersionTests(unittest.TestCase):
    def test_release_version_uses_engine_version(self):
        environment = {"PERFORMIX_ENGINE_VERSION": "1.2.3"}
        with patch.dict(os.environ, environment, clear=True):
            self.assertEqual(get_tools.get_engine_version(), "1.2.3")

    def test_snapshot_version_appends_dev(self):
        environment = {
            "PERFORMIX_ENGINE_VERSION": "1.2.3",
            "PERFORMIX_SNAPSHOT_BUILD": "true",
        }
        with patch.dict(os.environ, environment, clear=True):
            self.assertEqual(get_tools.get_engine_version(), "1.2.3-dev")


class SysutilPackagingTests(unittest.TestCase):
    def test_open_source_script_packages_sysutil_without_credentials(self):
        with tempfile.TemporaryDirectory() as temporary_dir:
            tools_dir = Path(temporary_dir) / "tools"
            environment = {"PERFORMIX_ENGINE_VERSION": "4.0.0"}
            with (
                patch.dict(os.environ, environment, clear=True),
                patch.object(
                    get_tools,
                    "_get_builtin_tool_source",
                    return_value=create_sysutil_source(Path(temporary_dir)),
                ),
            ):
                get_tools.main(
                    ["--dest", str(tools_dir), "sysutil-timeline"]
                )

            bundle_dir = tools_dir / "sysutil-timeline" / "4.0.0"
            self.assertTrue(
                (bundle_dir / "sysutil-timeline-Linux-aarch64.tar.gz").is_file()
            )
            self.assertTrue(
                (bundle_dir / "sysutil-timeline-Linux-x86_64.tar.gz").is_file()
            )

    def test_archive_contents_exclude_tests_and_caches(self):
        with tempfile.TemporaryDirectory() as temporary_dir:
            temporary_path = Path(temporary_dir)
            with (
                patch.object(
                    get_tools,
                    "_get_builtin_tool_source",
                    return_value=create_sysutil_source(temporary_path),
                ),
                patch.object(
                    get_tools,
                    "get_engine_version",
                    return_value="2.0.0",
                ),
            ):
                archives = get_tools.package_sysutil_timeline(
                    temporary_path / "tools"
                )

            self.assertEqual(
                {
                    "sysutil-timeline-Linux-aarch64.tar.gz",
                    "sysutil-timeline-Linux-x86_64.tar.gz",
                },
                {archive.name for archive in archives},
            )
            for archive_path in archives:
                with tarfile.open(archive_path, "r:gz") as archive:
                    names = set(archive.getnames())
                self.assertIn("sysutil-timeline.py", names)
                self.assertIn("collector.py", names)
                self.assertIn("nested/config.json", names)
                self.assertFalse(
                    any(
                        part in {"__pycache__", ".pytest_cache", "env", "tests"}
                        or part.endswith(".pyc")
                        for name in names
                        for part in Path(name).parts
                    )
                )


class ParquetToJsonPackagingTests(unittest.TestCase):
    def test_package_parquet_to_json_builds_all_host_variants(self):
        with tempfile.TemporaryDirectory() as temporary_dir:
            tools_dir = Path(temporary_dir) / "tools"
            with patch.object(get_tools, "package_builtin_go_tool") as package_builtin:
                get_tools.package_parquet_to_json(tools_dir)

            package_builtin.assert_called_once_with(
                Path("parquet-to-json"),
                "parquet-to-json",
                [
                    ("Linux", "aarch64"),
                    ("Linux", "x86_64"),
                    ("Windows", "aarch64"),
                    ("Windows", "x86_64"),
                    ("Darwin", "aarch64"),
                    ("Darwin", "x86_64"),
                ],
                tools_dir,
            )

class StagingTests(unittest.TestCase):
    def test_release_staging_keeps_linux_target_bundles_for_every_host(self):
        with tempfile.TemporaryDirectory() as temporary_dir:
            temporary_path = Path(temporary_dir)
            tools_dir = temporary_path / "tools"
            with (
                patch.object(
                    get_tools,
                    "_get_builtin_tool_source",
                    return_value=create_sysutil_source(temporary_path),
                ),
                patch.object(
                    get_tools,
                    "get_engine_version",
                    return_value="3.0.0",
                ),
            ):
                get_tools.package_sysutil_timeline(tools_dir)
            get_tools.prepare_release_tool_dirs(tools_dir)

            for goos, goarch in get_tools.RELEASE_TARGETS:
                bundle_dir = (
                    temporary_path
                    / f"tools-{goos}-{goarch}"
                    / "sysutil-timeline"
                    / "3.0.0"
                )
                self.assertTrue(
                    (bundle_dir / "sysutil-timeline-Linux-aarch64.tar.gz").is_file()
                )
                self.assertTrue(
                    (bundle_dir / "sysutil-timeline-Linux-x86_64.tar.gz").is_file()
                )


if __name__ == "__main__":
    unittest.main()
