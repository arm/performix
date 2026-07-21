#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import argparse
import json
import sys
from pathlib import Path

'''
This is a simple script which reads the terminology definition file at
`apap-engine/terminology/terminology.json` and provides getter methods to allow
other python scripts to reference these terms. The `main()` function just prints
out all terms and their values, unless a particular term is requested.
'''

SCRIPT_DIR = Path(__file__).resolve().parent.parent
JSON_PATH = SCRIPT_DIR.parent / "apap-engine" / "terminology" / "terminology.json"

def _get_terminology() -> dict[str, str]:
    try:
        text = JSON_PATH.read_text()
    except Exception as e:
        print(f"Could not read terminology file {JSON_PATH}: {e}")
        sys.exit(1)
    try:
        data = json.loads(text)
        if not isinstance(data, dict):
            print(f"Expected terms JSON object to be a key:value map: {data}")
            sys.exit(1)
        return data
    except (json.JSONDecodeError, KeyError) as e:
        print(f"Invalid terminology file format in {JSON_PATH}: {e}")
        sys.exit(1)

names = _get_terminology()

def get_product_full_name() -> str:
    return names["PRODUCT_FULL_NAME"]

def get_product_binary_name() -> str:
    return names["PRODUCT_BINARY_NAME"]

def get_agent_binary_name() -> str:
    return names["AGENT_BINARY_NAME"]

def get_daemon_dir_name() -> str:
    return names["DAEMON_DIR_NAME"]

def get_env_var_prefix() -> str:
    return names["ENV_VAR_PREFIX"]

def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Print terminology values from terminology.json"
    )
    subparsers = parser.add_subparsers(dest="command")

    subparsers.add_parser("get_product_full_name", help="Print the product full name")
    subparsers.add_parser("get_product_binary_name", help="Print the product binary name")
    subparsers.add_parser("get_agent_binary_name", help="Print the agent binary name")
    subparsers.add_parser("get_daemon_dir_name", help="Print the daemon dir name")
    subparsers.add_parser("get_env_var_prefix", help="Print the env var prefix")
    return parser


def main():
    if not isinstance(names, dict):
        print(f"Expected terms JSON object to be a key:value map: {names}")
        sys.exit(1)

    parser = _build_parser()
    args = parser.parse_args()

    if args.command is None:
        for key, value in names.items():
            print(f"{key}: {value}")
        return

    if args.command == "get_product_full_name":
        print(get_product_full_name())
    elif args.command == "get_product_binary_name":
        print(get_product_binary_name())
    elif args.command == "get_agent_binary_name":
        print(get_agent_binary_name())
    elif args.command == "get_daemon_dir_name":
        print(get_daemon_dir_name())
    elif args.command == "get_env_var_prefix":
        print(get_env_var_prefix())
    else:
        parser.error(f"Unknown command: {args.command}")

if __name__ == "__main__":
    main()
