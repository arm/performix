#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Resolve AI Insights testcase ids for GitHub Actions matrices."""

import argparse
import json
import sys
from pathlib import Path


MANIFEST = Path(__file__).with_name("ai_insights_evaluation.json")


parser = argparse.ArgumentParser()
parser.add_argument("--act", default="")
parser.add_argument(
    "--testcase",
    default="",
    help="Optional testcase id. If set, this overrides --act.",
)
args = parser.parse_args()

manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
tests = manifest["tests"]

if args.testcase:
    selected = [test for test in tests if test["id"] == args.testcase]
else:
    if not args.act:
        print("error: --act is required when --testcase is not set", file=sys.stderr)
        sys.exit(1)
    selected = [test for test in tests if args.act in test.get("acts", [])]

if not selected:
    print("error: no matching AI Insights workloads", file=sys.stderr)
    sys.exit(1)
if args.testcase and len(selected) != 1:
    print(f"error: testcase id is not unique: {args.testcase}", file=sys.stderr)
    sys.exit(1)

print(json.dumps([test["id"] for test in selected], separators=(",", ":")))
