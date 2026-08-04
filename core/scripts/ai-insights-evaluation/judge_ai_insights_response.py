#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Judge saved AI Insights responses without running the evaluation suite."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path

from test_ai_insights_evaluation import judge_response


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("rubric", type=Path, help="Rubric used by the judge")
    parser.add_argument(
        "responses",
        nargs="+",
        type=Path,
        help="One or more saved LLM response files to judge",
    )
    parser.add_argument(
        "--test-id",
        help="Testcase ID reported to the judge; defaults to the rubric filename",
    )
    parser.add_argument(
        "--judge-model",
        default=os.environ.get("AI_INSIGHTS_JUDGE_MODEL", "gpt-5-mini"),
        help="Judge model (default: %(default)s)",
    )
    args = parser.parse_args()

    api_key = os.environ.get("OPENAI_API_KEY", "").strip()
    if not api_key:
        parser.error("OPENAI_API_KEY is required")

    rubric = args.rubric.read_text(encoding="utf-8")
    test_id = args.test_id or args.rubric.stem
    results = []
    for response_path in args.responses:
        result = judge_response(
            test_id,
            rubric,
            response_path.read_text(encoding="utf-8"),
            {
                "judge_model": args.judge_model,
                "openai_api_key": api_key,
            },
        )
        results.append({"response": str(response_path), **result})

    print(json.dumps(results, indent=2))
    return int(any(result["label"] != "pass" for result in results))


if __name__ == "__main__":
    raise SystemExit(main())
