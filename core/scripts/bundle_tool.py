#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""
This script is used to bundle ATP tools by normalising their package name and placing them under the tools directory.

The normalised package name looks like:
{tool-name}-{os}-{arch}.tar.gz
"""
import argparse
import os
import shutil
import sys

def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Bundle tool tar.gz files into the ATP tools directory.")
    parser.add_argument("--tool-name", required=True, help="Tool name.")
    parser.add_argument("--version", required=True, help="Tool version.")
    parser.add_argument("--tools-dir", required=True, help="Path to the ATP tools directory.")
    parser.add_argument("--source", required=True, help="Path to the source .tar.gz file.")
    parser.add_argument("--os", required=True, help="OS label used for the bundled tar file name (formatted as {tool-name}-{os}-{arch}.tar.gz).")
    parser.add_argument("--arch", required=True, help="Arch label used for the bundled tar file name (formatted as {tool-name}-{os}-{arch}.tar.gz).")
    return parser.parse_args()


def destination_dir(tools_dir: str, tool_name: str, version: str) -> str:
    dest = os.path.join(tools_dir, tool_name, version)
    os.makedirs(dest, exist_ok=True)
    return dest


def destination_name(tool_name: str, os_name: str, arch: str) -> str:
    return f"{tool_name}-{os_name}-{arch}.tar.gz"


def stage_tar(source: str, dest_path: str):
    print(f"Staging {source} to {dest_path}")
    shutil.copy(source, dest_path)


def main():
    args = parse_args()
    if not os.path.isdir(args.tools_dir):
        sys.exit(f"Supplied tools directory not found: {args.tools_dir}")
    if not os.path.isfile(args.source):
        sys.exit(f"Supplied source file not found: {args.source}")
    if not args.source.endswith(".tar.gz"):
        sys.exit(f"Only .tar.gz sources are supported (supplied --source is {args.source})")

    dest_dir = destination_dir(args.tools_dir, args.tool_name, args.version)
    dest_name = destination_name(args.tool_name, args.os, args.arch)
    dest_path = os.path.join(dest_dir, dest_name)
    stage_tar(args.source, dest_path)


if __name__ == "__main__":
    main()
