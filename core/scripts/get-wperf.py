#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import argparse
import platform
import subprocess
import shutil
import sys
import tarfile
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

BUNDLE_VERSION = "1.0.1"
WPERF_VERSION = "5.5.0"
WPERF_HELPER_VERSION = "0.3.0"

ZIP_NAME = f"windowsperf-bin-{WPERF_VERSION}.zip"
TAR_NAME = "wperf-Windows-aarch64.tar.gz"
SRC_ZIP_NAME = f"windowsperf-{WPERF_VERSION}.zip"

WPERF_BIN_DOWNLOAD_URL = (
    "https://gitlab.com/api/v4/projects/40381146/packages/generic/"
    f"windowsperf/{WPERF_VERSION}/{ZIP_NAME}"
)
WPERF_SRC_DOWNLOAD_URL = (f"https://gitlab.com/Linaro/WindowsPerf/windowsperf/-/archive/"
                    f"{WPERF_VERSION}/{SRC_ZIP_NAME}")

SCRIPT_DIR = Path(__file__).resolve().parent
DOWNLOAD_ASSET_SCRIPT = SCRIPT_DIR / "download_github_release_asset.py"

def log(message: str) -> None:
    print(f"[get-wperf] {message}")


def _default_tools_dir(script_dir: Path) -> Path:
    return script_dir.parent / "apap-cli" / "tools"


def _download_file(url: str, destination: Path) -> None:
    log(f"Downloading {destination.name} from {url}")
    try:
        with urllib.request.urlopen(url) as response, destination.open("wb") as out_file:
            shutil.copyfileobj(response, out_file)
    except urllib.error.URLError as exc:
        raise RuntimeError(f"Failed to download {destination.name} from {url}") from exc


def _download_and_extract_wperf_helper_release(destination: Path) -> None:
    archive_name = f"wperf-helper_{WPERF_HELPER_VERSION}_windows_arm64.zip"
    archive_repo = "Arm-Debug/wperf-helper"
    cmd = [sys.executable, DOWNLOAD_ASSET_SCRIPT, "--repo", archive_repo, "--tag", f"v{WPERF_HELPER_VERSION}",
           "--asset-name", archive_name, "--out", destination]
    subprocess.run(cmd, check=True, text=True)
    zip_path = destination / archive_name
    if not zip_path.is_file():
        raise RuntimeError(f"Workload asset not found at {zip_path}")
    _extract_zip(zip_path, destination)
    _clean(zip_path)

def _download_and_extract_wperf_license(tmp_dir: Path, target_path: Path) -> None:
    zip_path = tmp_dir / SRC_ZIP_NAME
    _download_file(WPERF_SRC_DOWNLOAD_URL, zip_path)
    _extract_zip(zip_path, tmp_dir)
    shutil.copyfile(tmp_dir / f"windowsperf-{WPERF_VERSION}" / "LICENSE", target_path)
    _clean(zip_path)

def _extract_zip(zip_path: Path, target_dir: Path) -> None:
    target_dir.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(zip_path, "r") as archive:
        archive.extractall(target_dir)


def _create_tarball(source_dir: Path, output_path: Path) -> None:
    if output_path.exists():
        output_path.unlink()

    is_windows = platform.system().lower().startswith("win")

    def tar_filter(tarinfo: tarfile.TarInfo) -> tarfile.TarInfo:
        if is_windows:
            tarinfo.uid = 0
            tarinfo.gid = 0
            tarinfo.uname = ""
            tarinfo.gname = ""
            tarinfo.mode = 0o755
        return tarinfo

    with tarfile.open(output_path, "w:gz") as archive:
        for entry in sorted(source_dir.iterdir()):
            archive.add(entry, arcname=entry.name, filter=tar_filter)


def _clean(path: Path) -> None:
    if not path.exists():
        return
    if path.is_dir():
        shutil.rmtree(path)
    else:
        path.unlink()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Download and package WPERF binaries.")
    parser.add_argument(
        "tools_dir",
        nargs="?",
        help="Destination tools directory (defaults to ../apap-cli/tools relative to the script).",
    )
    return parser.parse_args()


def main() -> None:
    script_dir = Path(__file__).resolve().parent
    args = parse_args()

    tool_dst_base = Path(args.tools_dir) if args.tools_dir else _default_tools_dir(script_dir)
    tool_dst_dir = tool_dst_base / "wperf" / BUNDLE_VERSION
    log(f"Using tools destination {tool_dst_dir}")

    zip_path = tool_dst_dir / ZIP_NAME
    tmp_dir = tool_dst_dir / "__tmp"
    src_tmp_dir = tmp_dir / "wperf_src"
    output_file = tool_dst_dir / TAR_NAME

    _clean(zip_path)
    _clean(tmp_dir)

    src_tmp_dir.mkdir(parents=True, exist_ok=True)

    _download_file(WPERF_BIN_DOWNLOAD_URL, zip_path)

    log(f"Extracting archive to {tmp_dir}")
    _extract_zip(zip_path, tmp_dir)
    _clean(zip_path)

    # Download wperf-helper
    log(f"Downloading wperf-helper version {WPERF_HELPER_VERSION}")
    _download_and_extract_wperf_helper_release(tmp_dir / "wperf")

    # Copy license from the source archive to wperf
    log("Downloading wperf license from source archive")
    _download_and_extract_wperf_license(src_tmp_dir, tmp_dir / "wperf" / "wperf_license.txt")

    # We got the license file from the source tree, we can remove everything else
    _clean(src_tmp_dir)


    log("Renaming and copying license files")
    # Copy the wperf license to the driver directory
    shutil.copyfile(tmp_dir / "wperf" / "wperf_license.txt", tmp_dir / "wperf-driver" / "license.txt")

    wperf_helper_license_dir = tmp_dir / "wperf" / "license_terms"
    # Copy the wperf-helper licenses from the license terms directory and rename them
    shutil.copyfile(wperf_helper_license_dir / "license_agreement.txt", tmp_dir / "wperf" / "wperf_helper_license.txt")
    shutil.copyfile(wperf_helper_license_dir / "third_party_licenses.txt", tmp_dir / "wperf" / "lib" / "third_party_licenses.txt")

    log("Removing redundant directories")
    _clean(wperf_helper_license_dir) # The licenses have already been copied and renamed accordingly, so remove this directory
    _clean(tmp_dir / "lib") # lib is not used by the wperf tool

    log(f"Creating tarball {output_file}")
    _create_tarball(tmp_dir, output_file)
    _clean(tmp_dir)

    log("Packaging complete")


if __name__ == "__main__":
    try:
        main()
        log("Success")
    except Exception as exc:  # pragma: no cover - defensive top-level guard
        print(f"Error: {exc}", file=sys.stderr)
        sys.exit(1)
