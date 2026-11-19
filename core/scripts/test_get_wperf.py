# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import argparse
import importlib.util
import subprocess
import tarfile
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location(
    "test_get_wperf", Path(__file__).with_name("get-wperf.py")
)
get_wperf = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(get_wperf)


class WperfSigningTests(unittest.TestCase):
    def package(self, root, signing, include_record_dll, sign):
        def download(url, destination):
            with zipfile.ZipFile(destination, "w") as archive:
                archive.writestr("wperf/wperf.exe", b"signed WindowsPerf")
                archive.writestr("wperf/lib/msdia140.dll", b"signed runtime")
                archive.writestr("wperf-driver/wperf-driver.sys", b"signed driver")

        def download_helper(destination):
            files = {
                "wperf-helper.exe": b"helper",
                "license_terms/license_agreement.txt": b"helper license",
                "license_terms/third_party_licenses.txt": b"third party license",
            }
            if include_record_dll:
                files["libwperf_record.dll"] = b"record library"
            for name, contents in files.items():
                file = destination / name
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_bytes(contents)

        with patch.dict(get_wperf.os.environ, {"SIGN_REQUIRED": signing}), patch.object(
            get_wperf, "parse_args", return_value=argparse.Namespace(tools_dir=str(root))
        ), patch.object(get_wperf, "_download_file", side_effect=download), patch.object(
            get_wperf, "_download_and_extract_wperf_helper_release", side_effect=download_helper
        ), patch.object(
            get_wperf, "_download_and_extract_wperf_license",
            side_effect=lambda source, target: target.write_bytes(b"WindowsPerf license"),
        ), patch.object(get_wperf.subprocess, "run", side_effect=sign) as signer:
            get_wperf.main()
        return signer

    def test_packages_expected_helper_signatures_and_preserves_vendor_binaries(self):
        cases = [
            (
                "release with record DLL", "true", True, 2,
                {
                    "wperf-helper.exe": b"signed helper",
                    "libwperf_record.dll": b"signed record library",
                },
            ),
            (
                "release without record DLL", "true", False, 1,
                {"wperf-helper.exe": b"signed helper"},
            ),
            (
                "developer build", "false", True, 0,
                {
                    "wperf-helper.exe": b"helper",
                    "libwperf_record.dll": b"record library",
                },
            ),
        ]
        for case_name, signing, include_record_dll, expected_calls, expected_helpers in cases:
            with (
                self.subTest(case=case_name),
                tempfile.TemporaryDirectory() as temporary,
            ):
                root = Path(temporary)

                def sign(command, **kwargs):
                    binary = Path(command[4])
                    binary.write_bytes(b"signed " + binary.read_bytes())

                signer = self.package(root, signing, include_record_dll, sign)
                self.assertEqual(signer.call_count, expected_calls)
                archive = root / "wperf" / get_wperf.BUNDLE_VERSION / get_wperf.TAR_NAME
                with tarfile.open(archive) as bundle:
                    expected = {
                        "wperf/wperf.exe": b"signed WindowsPerf",
                        "wperf/lib/msdia140.dll": b"signed runtime",
                        "wperf-driver/wperf-driver.sys": b"signed driver",
                        "wperf/wperf_helper_license.txt": b"helper license",
                        "wperf/lib/third_party_licenses.txt": b"third party license",
                    }
                    expected.update({
                        f"wperf/{name}": contents
                        for name, contents in expected_helpers.items()
                    })
                    for name, contents in expected.items():
                        self.assertEqual(bundle.extractfile(name).read(), contents)

    def test_signing_failure_stops_archive_creation(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with self.assertRaises(subprocess.CalledProcessError):
                self.package(
                    root, "true", False, subprocess.CalledProcessError(1, "sign.sh")
                )
            archive = root / "wperf" / get_wperf.BUNDLE_VERSION / get_wperf.TAR_NAME
            self.assertFalse(archive.exists())


if __name__ == "__main__":
    unittest.main()
