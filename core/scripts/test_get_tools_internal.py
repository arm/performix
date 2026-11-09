# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import importlib.util
import tarfile
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest.mock import MagicMock, patch

import requests


SPEC = importlib.util.spec_from_file_location(
    "test_internal_get_tools", Path(__file__).with_name("get-tools-internal.py")
)
get_tools = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(get_tools)


class DownloadTests(unittest.TestCase):
    def test_streams_authenticated_download(self):
        response = MagicMock()
        response.__enter__.return_value = response
        response.iter_content.return_value = [b"first", b"second"]
        with tempfile.TemporaryDirectory() as temporary, patch.object(
            get_tools.requests, "get", return_value=response
        ) as get:
            destination = Path(temporary) / "nested" / "asset.tar.gz"
            get_tools._download("https://example.invalid/asset", destination, token="test-token")
            self.assertEqual(destination.read_bytes(), b"firstsecond")
        get.assert_called_once_with(
            "https://example.invalid/asset", headers={"X-JFrog-Art-Api": "test-token"},
            stream=True, timeout=120,
        )
        response.raise_for_status.assert_called_once()

    def test_http_failure_does_not_write_error_body_as_archive(self):
        response = MagicMock()
        response.__enter__.return_value = response
        response.raise_for_status.side_effect = requests.HTTPError("403 Forbidden")
        with tempfile.TemporaryDirectory() as temporary, patch.object(
            get_tools.requests, "get", return_value=response
        ):
            destination = Path(temporary) / "asset.tar.gz"
            with self.assertRaisesRegex(RuntimeError, "Download failed.*403 Forbidden"):
                get_tools._download("https://example.invalid/asset", destination)
            self.assertFalse(destination.exists())

    def test_connection_failure_reports_download_url(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(
            get_tools.requests, "get", side_effect=requests.ConnectionError("connection failed")
        ):
            with self.assertRaisesRegex(RuntimeError, "https://example.invalid/asset"):
                get_tools._download("https://example.invalid/asset", Path(temporary) / "asset")


class AndroidToolPackagingTests(unittest.TestCase):
    def test_packages_selected_android_arm64_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root / "source.zip"
            with zipfile.ZipFile(archive, "w") as zip_file:
                zip_file.writestr(
                    "bin/android/arm64/libVkLayerGPUTimeline.so", b"layer"
                )
                zip_file.writestr(
                    "bin/android/arm/libVkLayerGPUTimeline.so", b"wrong arch"
                )

            get_tools._package_android_arm64_zip_tool(
                root / "tools",
                "gpulayers",
                "9.8.0",
                archive,
                ("bin/android/arm64/libVkLayerGPUTimeline.so",),
            )

            bundle = root / "tools/gpulayers/9.8.0/gpulayers-Android-aarch64.tar.gz"
            with tarfile.open(bundle, "r:gz") as tar:
                self.assertEqual(tar.getnames(), ["bin/libVkLayerGPUTimeline.so"])
                extracted = tar.extractfile("bin/libVkLayerGPUTimeline.so")
                self.assertIsNotNone(extracted)
                self.assertEqual(extracted.read(), b"layer")

    def test_gpu_layers_download_uses_streamline_maven_repository(self):
        with tempfile.TemporaryDirectory() as temporary, patch.dict(
            get_tools.os.environ, {"ARTIFACTORY_API_TOKEN": "test-token"}
        ), patch.object(get_tools, "_download") as download, patch.object(
            get_tools, "_package_android_arm64_zip_tool"
        ) as package:
            get_tools.get_gpulayers(Path(temporary))

        download.assert_called_once()
        self.assertEqual(
            download.call_args.args[0],
            "https://artifactory.arm.com/artifactory/"
            "mobile-studio.streamline-maven-virtual/com/arm/streamline/"
            "lib-gpu-layers-build/9.8.0.ngp_b7193aa2_3/"
            "lib-gpu-layers-build-9.8.0.ngp_b7193aa2_3-target-binary.zip",
        )
        package.assert_called_once()


if __name__ == "__main__":
    unittest.main()
