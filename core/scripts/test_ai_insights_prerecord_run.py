# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import io
import json
import tempfile
import unittest
from argparse import Namespace
import zipfile
from contextlib import redirect_stdout
from pathlib import Path
from subprocess import CompletedProcess
from unittest.mock import call, patch

MODULE_DIR = Path(__file__).resolve().parent
PRERECORD_SPEC = importlib.util.spec_from_file_location(
    "ai_insights_prerecord_run",
    MODULE_DIR / "ai-insights-evaluation" / "prerecord-run.py",
)
prerecord = importlib.util.module_from_spec(PRERECORD_SPEC)
PRERECORD_SPEC.loader.exec_module(prerecord)


class QueryRenderRowsTests(unittest.TestCase):
    def test_null_rows_returns_empty_list(self):
        fake_process = CompletedProcess(
            args=["apx"],
            returncode=0,
            stdout='{"data":{"rows":null}}',
            stderr="",
        )

        with patch.object(prerecord, "run_cli", return_value=fake_process):
            rows = prerecord.query_render_rows(Path("/tmp/apx"), "s1", "SELECT 1")

        self.assertEqual(rows, [])

    def test_non_list_rows_error_includes_value(self):
        fake_process = CompletedProcess(
            args=["apx"],
            returncode=0,
            stdout='{"data":{"rows":{}}}',
            stderr="",
        )

        with patch.object(prerecord, "run_cli", return_value=fake_process):
            with self.assertRaises(ValueError) as ctx:
                prerecord.query_render_rows(Path("/tmp/apx"), "s1", "SELECT 1")

        self.assertIn("render query rows are not a list", str(ctx.exception))
        self.assertIn("rows={}", str(ctx.exception))


class PrintRunInfoTests(unittest.TestCase):
    def test_print_run_info_reports_json_output(self):
        fake_process = CompletedProcess(
            args=["apx"],
            returncode=0,
            stdout='{"data":{"id":"run-1","run_result":"success"}}\n',
            stderr="",
        )

        with patch.object(prerecord, "run_cli", return_value=fake_process) as mock_run:
            output = io.StringIO()
            with redirect_stdout(output):
                prerecord.print_run_info(Path("/tmp/apx"), "run-1")

        mock_run.assert_called_once_with(
            ["/tmp/apx", "run", "info", "run-1", "--json"],
            Path("/tmp"),
        )
        self.assertIn("===== apx run info run-1 --json =====", output.getvalue())
        self.assertIn('"run_result":"success"', output.getvalue())
        self.assertIn("===== end apx run info =====", output.getvalue())


class MainTests(unittest.TestCase):
    def _write_exported_zip(self, path: Path) -> Path:
        path.write_bytes(b"zip-bytes")
        return path

    def _base_args(
        self,
        cli_bin: Path,
        output_dir: Path,
        *,
        params: list[str] | None = None,
        artifactory_run_base: str | None = "its.apx-prerecorded-runs/manual",
    ) -> Namespace:
        return Namespace(
            case="case-1",
            cli_bin=cli_bin,
            target="remote_target",
            recipe="instruction_mix",
            workload_cmd="openssl speed",
            pid=None,
            timeout=None,
            prerecord='{"mode":"launch"}',
            params_json="[]",
            ssh_target=None,
            ssh_key=None,
            target_workload_root=None,
            output_dir=output_dir,
            artifactory_run_base=artifactory_run_base,
            param=params or [],
        )

    def test_main_without_optional_inputs_omits_related_metadata(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            cli_bin = tmp_path / "apx"
            cli_bin.write_text("", encoding="utf-8")
            exported = self._write_exported_zip(tmp_path / "exported.zip")
            args = self._base_args(cli_bin, tmp_path, artifactory_run_base=None)
            args.recipe = "system_utilization"
            resolved_cli_bin = cli_bin.resolve()
            fake_process = CompletedProcess(args=["apx"], returncode=0, stdout="", stderr="")

            with (
                patch.object(prerecord, "parse_args", return_value=args),
                patch.object(prerecord, "run_cli", return_value=fake_process) as mock_run_cli,
                patch.object(prerecord, "parse_recipe_run_id", return_value="run-1"),
                patch.object(prerecord, "print_run_info"),
                patch.object(prerecord, "export_run", return_value=exported),
                patch.object(prerecord, "get_cli_version", return_value="1.2.3"),
                patch.object(prerecord, "sha256_file", return_value="fake-sha"),
                patch.object(prerecord, "collect_sampled_sources") as mock_collect_sources,
            ):
                self.assertEqual(prerecord.main(), 0)

            mock_collect_sources.assert_not_called()
            mock_run_cli.assert_has_calls(
                [
                    call(
                        [
                            str(resolved_cli_bin),
                            "recipe",
                            "ready",
                            "system_utilization",
                            "--workload",
                            "openssl speed",
                            "--target",
                            "remote_target",
                        ],
                        resolved_cli_bin.parent,
                    ),
                    call(
                        [
                            str(resolved_cli_bin),
                            "recipe",
                            "run",
                            "system_utilization",
                            "--workload",
                            "openssl speed",
                            "--target",
                            "remote_target",
                            "--deploy-tools",
                        ],
                        resolved_cli_bin.parent,
                    ),
                ]
            )

            metadata = json.loads(
                (tmp_path.resolve() / "case-1" / "metadata.json").read_text(encoding="utf-8")
            )
            self.assertEqual(metadata["recipe_params"], [])
            self.assertNotIn("manifest_path", metadata)
            self.assertNotIn("source_archive_path", metadata)
            self.assertFalse(any(key.startswith("artifactory_") for key in metadata))
            self.assertEqual(
                metadata["archive_path"],
                str(tmp_path.resolve() / "case-1" / "latest.zip"),
            )

    def test_main_adds_resolved_recipe_params_before_cli_params(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            cli_bin = tmp_path / "apx"
            cli_bin.write_text("", encoding="utf-8")
            exported = self._write_exported_zip(tmp_path / "exported.zip")
            args = self._base_args(cli_bin, tmp_path, params=["mode=dynamic"])
            args.recipe = "system_utilization"
            args.params_json = '["collector=enabled"]'
            resolved_cli_bin = cli_bin.resolve()
            fake_process = CompletedProcess(args=["apx"], returncode=0, stdout="", stderr="")

            with (
                patch.object(prerecord, "parse_args", return_value=args),
                patch.object(prerecord, "run_cli", return_value=fake_process) as mock_run_cli,
                patch.object(prerecord, "parse_recipe_run_id", return_value="run-2"),
                patch.object(prerecord, "print_run_info"),
                patch.object(prerecord, "export_run", return_value=exported),
                patch.object(prerecord, "get_cli_version", return_value="1.2.3"),
                patch.object(prerecord, "sha256_file", return_value="fake-sha"),
            ):
                self.assertEqual(prerecord.main(), 0)

            mock_run_cli.assert_has_calls(
                [
                    call(
                        [
                            str(resolved_cli_bin),
                            "recipe",
                            "ready",
                            "system_utilization",
                            "--workload",
                            "openssl speed",
                            "--target",
                            "remote_target",
                            "--param",
                            "collector=enabled",
                            "--param",
                            "mode=dynamic",
                        ],
                        resolved_cli_bin.parent,
                    ),
                    call(
                        [
                            str(resolved_cli_bin),
                            "recipe",
                            "run",
                            "system_utilization",
                            "--workload",
                            "openssl speed",
                            "--target",
                            "remote_target",
                            "--deploy-tools",
                            "--param",
                            "collector=enabled",
                            "--param",
                            "mode=dynamic",
                        ],
                        resolved_cli_bin.parent,
                    ),
                ]
            )

            metadata = json.loads(
                (tmp_path.resolve() / "case-1" / "metadata.json").read_text(encoding="utf-8")
            )
            self.assertEqual(metadata["recipe_params"], ["collector=enabled", "mode=dynamic"])

    def test_main_collects_sources_without_passing_source_root(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            cli_bin = tmp_path / "apx"
            cli_bin.write_text("", encoding="utf-8")
            exported = self._write_exported_zip(tmp_path / "exported.zip")
            source_bundle = self._write_exported_zip(tmp_path / "test_src.zip")
            args = self._base_args(cli_bin, tmp_path)
            resolved_cli_bin = cli_bin.resolve()
            resolved_case_dir = tmp_path.resolve() / "case-1"
            fake_process = CompletedProcess(args=["apx"], returncode=0, stdout="", stderr="")

            with (
                patch.object(prerecord, "parse_args", return_value=args),
                patch.object(prerecord, "run_cli", return_value=fake_process) as mock_run_cli,
                patch.object(prerecord, "parse_recipe_run_id", return_value="run-3"),
                patch.object(prerecord, "print_run_info"),
                patch.object(prerecord, "export_run", return_value=exported),
                patch.object(prerecord, "get_cli_version", return_value="1.2.3"),
                patch.object(prerecord, "sha256_file", return_value="fake-sha"),
                patch.object(
                    prerecord,
                    "collect_sampled_sources",
                    return_value=source_bundle,
                ) as mock_collect_sources,
            ):
                self.assertEqual(prerecord.main(), 0)

            mock_collect_sources.assert_called_once_with(
                resolved_cli_bin,
                "run-3",
                resolved_case_dir,
                "instruction_mix",
                prerecord.source_files_query("instruction_mix"),
            )
            mock_run_cli.assert_has_calls(
                [
                    call(
                        [
                            str(resolved_cli_bin),
                            "recipe",
                            "ready",
                            "instruction_mix",
                            "--workload",
                            "openssl speed",
                            "--target",
                            "remote_target",
                        ],
                        resolved_cli_bin.parent,
                    ),
                    call(
                        [
                            str(resolved_cli_bin),
                            "recipe",
                            "run",
                            "instruction_mix",
                            "--workload",
                            "openssl speed",
                            "--target",
                            "remote_target",
                            "--deploy-tools",
                        ],
                        resolved_cli_bin.parent,
                    ),
                ]
            )

            metadata = json.loads(
                (resolved_case_dir / "metadata.json").read_text(encoding="utf-8")
            )
            self.assertEqual(metadata["source_archive_path"], str(source_bundle))
            self.assertEqual(
                metadata["artifactory_source_archive_path"],
                "its.apx-prerecorded-runs/manual/case-1/test_src.zip",
            )

    def test_attach_cleanup_runs_when_profiling_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            cli_bin = tmp_path / "apx"
            cli_bin.write_text("", encoding="utf-8")
            ssh_key = tmp_path / "id.pem"
            ssh_key.write_text("", encoding="utf-8")
            args = self._base_args(cli_bin, tmp_path)
            args.recipe = "code_hotspots"
            args.workload_cmd = None
            args.prerecord = json.dumps(
                {
                    "mode": "attach",
                    "timeout_seconds": 25,
                    "setup": ["bash", "sidecar.sh", "setup"],
                    "cleanup": ["bash", "sidecar.sh", "cleanup"],
                }
            )
            args.ssh_target = "ubuntu@target"
            args.ssh_key = ssh_key
            args.target_workload_root = Path("/remote/workloads")
            setup_process = CompletedProcess(
                args=["ssh"], returncode=0, stdout="PID:1234\n", stderr=""
            )
            cleanup_process = CompletedProcess(
                args=["ssh"], returncode=0, stdout="", stderr=""
            )
            ready_process = CompletedProcess(
                args=["apx"], returncode=0, stdout="", stderr=""
            )

            with (
                patch.object(prerecord, "parse_args", return_value=args),
                patch.object(
                    prerecord,
                    "run_remote_command",
                    side_effect=[setup_process, cleanup_process],
                ) as mock_remote,
                patch.object(
                    prerecord,
                    "run_cli",
                    side_effect=[ready_process, RuntimeError("profile failed")],
                ) as mock_run_cli,
                self.assertRaisesRegex(RuntimeError, "profile failed"),
            ):
                prerecord.main()

            mock_run_cli.assert_has_calls(
                [
                    call(
                        [
                            str(cli_bin.resolve()),
                            "recipe",
                            "ready",
                            "code_hotspots",
                            "--pid",
                            "1234",
                            "--target",
                            "remote_target",
                        ],
                        cli_bin.resolve().parent,
                    ),
                    call(
                        [
                            str(cli_bin.resolve()),
                            "recipe",
                            "run",
                            "code_hotspots",
                            "--pid",
                            "1234",
                            "--target",
                            "remote_target",
                            "--deploy-tools",
                            "--timeout",
                            "25",
                        ],
                        cli_bin.resolve().parent,
                    ),
                ]
            )
            self.assertEqual(mock_remote.call_count, 2)
            self.assertEqual(
                mock_remote.call_args_list[-1].args[0],
                ["bash", "sidecar.sh", "cleanup"],
            )


class CollectSampledSourcesTests(unittest.TestCase):
    def test_writes_manifest_when_no_sampled_sources_exist(self):
        with tempfile.TemporaryDirectory() as tmp:
            case_dir = Path(tmp)
            with (
                patch.object(prerecord, "render_run", return_value="s1"),
                patch.object(prerecord, "query_render_rows", return_value=[]),
                patch.object(prerecord, "close_render_session") as mock_close,
            ):
                archive = prerecord.collect_sampled_sources(
                    Path("/tmp/apx"),
                    "run-1",
                    case_dir,
                    "code_hotspots",
                    prerecord.source_files_query("code_hotspots"),
                )

            with zipfile.ZipFile(archive) as source_zip:
                manifest = json.loads(source_zip.read("test_src/manifest.json"))

        mock_close.assert_called_once_with(Path("/tmp/apx"), "s1")
        self.assertEqual(manifest["sampled_source_count"], 0)
        self.assertEqual(manifest["fetched_source_count"], 0)
        self.assertEqual(manifest["failed_source_count"], 0)

    def test_writes_manifest_when_sampled_sources_cannot_be_fetched(self):
        sampled = [{"source_file_id": 1, "periodic_samples": 10}]
        with tempfile.TemporaryDirectory() as tmp:
            with (
                patch.object(prerecord, "render_run", return_value="s1"),
                patch.object(
                    prerecord, "query_render_rows", side_effect=[sampled, []]
                ),
                patch.object(prerecord, "close_render_session"),
            ):
                archive = prerecord.collect_sampled_sources(
                    Path("/tmp/apx"),
                    "run-1",
                    Path(tmp),
                    "code_hotspots",
                    prerecord.source_files_query("code_hotspots"),
                )

            with zipfile.ZipFile(archive) as source_zip:
                manifest = json.loads(source_zip.read("test_src/manifest.json"))

        self.assertEqual(manifest["sampled_source_count"], 1)
        self.assertEqual(manifest["fetched_source_count"], 0)
        self.assertEqual(manifest["failed_source_count"], 1)
        self.assertEqual(manifest["failed"][0]["source_file_id"], 1)


class BuildRecipeRunCommandTests(unittest.TestCase):
    def test_builds_launch_command(self):
        command = prerecord.build_recipe_run_command(
            Path("/tmp/apx"),
            "code_hotspots",
            "remote_target",
            prerecord.Launch("/tmp/workload"),
            None,
            [],
        )

        self.assertEqual(
            command,
            [
                "/tmp/apx",
                "recipe",
                "run",
                "code_hotspots",
                "--workload",
                "/tmp/workload",
                "--target",
                "remote_target",
                "--deploy-tools",
            ],
        )

    def test_builds_attach_command_with_metadata(self):
        command = prerecord.build_recipe_run_command(
            Path("/tmp/apx"),
            "code_hotspots",
            "remote_target",
            prerecord.AttachToPID(1234),
            "25",
            ["collect_dotnet_stacks=true"],
        )

        self.assertEqual(
            command,
            [
                "/tmp/apx",
                "recipe",
                "run",
                "code_hotspots",
                "--pid",
                "1234",
                "--target",
                "remote_target",
                "--deploy-tools",
                "--timeout",
                "25",
                "--param",
                "collect_dotnet_stacks=true",
            ],
        )

    def test_builds_attach_command_without_source(self):
        command = prerecord.build_recipe_run_command(
            Path("/tmp/apx"),
            "code_hotspots",
            "remote_target",
            prerecord.AttachToPID(1234),
            "25",
            [],
        )

        self.assertEqual(
            command,
            [
                "/tmp/apx",
                "recipe",
                "run",
                "code_hotspots",
                "--pid",
                "1234",
                "--target",
                "remote_target",
                "--deploy-tools",
                "--timeout",
                "25",
            ],
        )

    def test_builds_system_wide_command(self):
        command = prerecord.build_recipe_run_command(
            Path("/tmp/apx"),
            "system_utilization",
            "remote_target",
            prerecord.SystemWide(),
            "25",
            [],
        )

        self.assertEqual(
            command,
            [
                "/tmp/apx",
                "recipe",
                "run",
                "system_utilization",
                "--system-wide",
                "--target",
                "remote_target",
                "--deploy-tools",
                "--timeout",
                "25",
            ],
        )


if __name__ == "__main__":
    unittest.main()
