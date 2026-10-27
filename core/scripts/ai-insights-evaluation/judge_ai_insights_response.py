#!/usr/bin/env python3

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Judge saved AI Insights responses without running the evaluation suite."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path

from test_ai_insights_evaluation import judge_response, mcp_policy_passes
from pathways_artifacts import load_pathways_judge_evidence


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
        default=os.environ.get("AI_INSIGHTS_JUDGE_MODEL", "gpt-5.6-luna"),
        help="Judge model (default: %(default)s)",
    )
    parser.add_argument("--judge-reasoning-effort",
                        default=os.environ.get("AI_INSIGHTS_JUDGE_REASONING_EFFORT", "medium"),
                        help="Judge reasoning effort (default: %(default)s)")
    parser.add_argument("--pathways-session", type=Path,
                        help="Saved pathways JSONL to assess alongside a single final response")
    parser.add_argument("--supplied-run-id", help="Initial fixture run ID, if the pathways attempt supplied one")
    args = parser.parse_args()
    if args.supplied_run_id and not args.pathways_session:
        parser.error("--supplied-run-id requires --pathways-session")
    if args.pathways_session and len(args.responses) != 1:
        parser.error("a pathways session requires exactly one final response")

    api_key = os.environ.get("OPENAI_API_KEY", "").strip()
    if not api_key:
        parser.error("OPENAI_API_KEY is required")

    rubric = args.rubric.read_text(encoding="utf-8")
    test_id = args.test_id or args.rubric.stem
    pathways_judge_options = {}
    if args.pathways_session:
        evidence, transcript_error = load_pathways_judge_evidence(args.pathways_session, args.supplied_run_id)
        if transcript_error:
            parser.error(transcript_error)
        pathways_judge_options["pathways_evidence"] = evidence
    results = []
    for response_path in args.responses:
        result = judge_response(
            test_id,
            rubric,
            response_path.read_text(encoding="utf-8"),
            {
                "judge_model": args.judge_model,
                "judge_reasoning_effort": args.judge_reasoning_effort,
                "openai_api_key": api_key,
            },
            **pathways_judge_options,
        )
        results.append({"response": str(response_path), **result})

    print(json.dumps(results, indent=2))
    return int(any(
        result["label"] != "pass"
        or (args.pathways_session and not mcp_policy_passes(result.get("mcp_policy")))
        for result in results
    ))


if __name__ == "__main__":
    raise SystemExit(main())
