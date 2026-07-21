#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import argparse
import os
import sys
from pathlib import Path

from download_github_release_asset import download_asset, fetch_release, pick_asset
from tool_configs import GITHUB_TOOL_REGISTRY
from utils.archive import create_tarball, extract_tarball, extract_zip
from utils.fs import clean, ensure_executable
from utils.paths import default_tools_dir
from utils.tool_naming import generate_tool_tarball_name


def log(tool, message: str) -> None:
    """
    Log a message prefixed with the tool identifier.

    Output format:
        [tool-name@version] message
    """
    print(f"[{tool.tool_name}@{tool.version}] {message}")


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Download and package tools from GitHub releases."
    )
    parser.add_argument(
        "tool_name",
        help=f"Tool name to download. Available: {', '.join(sorted(GITHUB_TOOL_REGISTRY.keys()))}",
    )
    parser.add_argument(
        "--tools-dir",
        nargs="?",
        required=False,
        help="Destination tools directory (defaults to ../apap-cli/tools relative to the script).",
    )
    parser.add_argument(
        "--os",
        type=str,
        required=False,
        help="Target operating system (e.g. linux, windows). It must match the github asset naming.",
        default="linux",
    )
    parser.add_argument(
        "--arch",
        type=str,
        required=False,
        help="Target architecture (e.g. x86, arm64). It must match the github asset naming.",
        default="arm64",
    )
    parser.add_argument(
        "--third-party-licenses",
        type=str,
        required=False,
        help=(
            "Path to third_party_licenses.txt inside the downloaded tool archive "
            "(e.g. license_terms/third_party_licenses.txt). When provided, the file is "
            "extracted and placed at <tool-version-dir>/license_terms/third_party_licenses.txt."
        ),
    )
    return parser.parse_args()


def download_github_asset(
    *,
    repo: str,
    tag: str,
    asset_name: str,
    destination: Path,
    token: str,
) -> None:
    release = fetch_release(repo, tag, token)
    asset = pick_asset(release.get("assets", []), asset_name)

    destination.parent.mkdir(parents=True, exist_ok=True)

    downloaded_path = Path(
        download_asset(asset, token, str(destination.parent))
    ).resolve()
    expected_path = destination.resolve()

    if downloaded_path != expected_path:
        downloaded_path.replace(expected_path)

def place_tpip(source: Path, dest_dir: Path, tool) -> Path:
    """
    Copy third-party license file into dest_dir/license_terms/third_party_licenses.txt.

    Returns the path to the created file.
    """
    license_dir = dest_dir / "license_terms"
    license_dir.mkdir(parents=True, exist_ok=True)
    
    dest_path = license_dir / "third_party_licenses.txt"
    dest_path.write_bytes(source.read_bytes())
    
    log(tool, f"Placed third-party license asset at {dest_path}")
    return dest_path

def main() -> None:
    args = parse_arguments()
    gh_os = str(args.os)
    gh_arch = str(args.arch)

    token = os.environ.get("GITHUB_TOKEN")
    if not token:
        raise RuntimeError(
            "GITHUB_TOKEN env var is not set. Set this to a GitHub personal access token "
            "with permissions for the Arm-Debug organisation."
        )

    tool_base_dir = Path(args.tools_dir) if args.tools_dir else default_tools_dir()

    tool_name = str(args.tool_name)
    tool = GITHUB_TOOL_REGISTRY.get(tool_name)
    if tool is None:
        available = ", ".join(sorted(GITHUB_TOOL_REGISTRY.keys()))
        raise ValueError(f"Unknown tool '{tool_name}'. Available: {available}")

    if not tool.supports(os=gh_os, arch=gh_arch):
        raise RuntimeError(
            f"{tool.tool_name}@{tool.version} is not available for {gh_os}/{gh_arch}"
        )

    tag = tool.github_conf.tag_for(version=tool.version)
    asset_name = tool.github_conf.asset_for(
        version=tool.version,
        os=gh_os,
        arch=gh_arch,
    )

    tool_dst_dir = tool_base_dir / tool.tool_name / tool.version
    tool_dst_dir.mkdir(parents=True, exist_ok=True)

    asset_archive_path = tool_dst_dir / asset_name
    archive_path = tool_dst_dir / generate_tool_tarball_name(
        tool.tool_name, gh_os, gh_arch
    )
    temp_dir = tool_dst_dir / f"__tmp-{gh_os}-{gh_arch}"
    # Clean up any previous temp dir artifact
    try:
        clean(temp_dir)
    except Exception:
        pass

    download_github_asset(
        repo=tool.github_conf.repo,
        tag=tag,
        asset_name=asset_name,
        destination=asset_archive_path,
        token=token,
    )
    log(tool, f"Downloaded {asset_name} from {tool.github_conf.repo}@{tag}")

    # Extract the downloaded asset. Note: Path.suffix only returns the last suffix
    # (e.g. ".gz"), so use the full filename to detect ".tar.gz".
    asset_filename = asset_archive_path.name
    if asset_filename.endswith(".zip"):
        extract_zip(asset_archive_path, temp_dir)
    elif asset_filename.endswith(".tar.gz") or asset_filename.endswith(".tgz"):
        extract_tarball(asset_archive_path, temp_dir)
    else:
        raise RuntimeError(
            f"Unsupported asset archive type: {asset_filename}. Expected .zip or .tar.gz"
        )

    log(tool, f"Extracted {asset_name} to {temp_dir}")

    # Ensure binary is executable before packaging
    binary_path = temp_dir / tool.binary_name
    if not binary_path.exists():
        matches = [p for p in temp_dir.rglob(tool.binary_name) if p.is_file()]
        if len(matches) == 1:
            binary_path = matches[0]
        elif not matches:
            raise FileNotFoundError(
                f"Could not find binary '{tool.binary_name}' in {temp_dir}"
            )
        else:
            raise RuntimeError(
                f"Multiple binaries named '{tool.binary_name}' found in {temp_dir}: {matches}"
            )

    ensure_executable(binary_path)
    log(tool, f"Set executable bit on {binary_path}")

    # create third_party_licenses.txt
    if args.third_party_licenses:
        tpip_src = temp_dir / args.third_party_licenses
        if not tpip_src.exists():
            raise FileNotFoundError(
                f"third-party license file '{args.third_party_licenses}' not found in {temp_dir}"
            )
        place_tpip(tpip_src, tool_dst_dir, tool)

    create_tarball(temp_dir, archive_path)
    log(tool, f"Created archive {archive_path}")

    clean(temp_dir)
    clean(asset_archive_path)
    log(tool, "Cleaned up temporary files")

    log(tool, "Packaging complete.")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print(f"[Error] {e}")
        sys.exit(1)
