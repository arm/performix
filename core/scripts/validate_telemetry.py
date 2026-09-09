# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

"""Validate CPU telemetry against the schemas stored in the repository."""

import json
import os
from pathlib import Path

from jsonschema import Draft202012Validator


DATA_DIR = (
    Path(__file__).resolve().parents[1] / "apap-engine" / "telemetry" / "data"
)


def run(data_dir: Path = DATA_DIR) -> int:
    schemas = {}
    for path in (data_dir / "schemas").glob("*.schema.json"):
        schema = json.loads(path.read_text(encoding="utf-8"))
        Draft202012Validator.check_schema(schema)
        schemas[path.name] = Draft202012Validator(schema)

    counts = {"error": 0, "warning": 0}
    for visibility, level in (("public", "error"), ("private", "warning")):
        for path in sorted((data_dir / visibility).glob("*.json")):
            try:
                document = json.loads(path.read_text(encoding="utf-8"))
                name = document.get("$schema") if isinstance(document, dict) else None
                validator = schemas.get(name) if isinstance(name, str) else None
                messages = (
                    [f"{error.json_path}: {error.message}" for error in validator.iter_errors(document)]
                    if validator
                    else [f"$.$schema: unknown or missing schema {name!r}"]
                )
            except (OSError, json.JSONDecodeError) as error:
                messages = [f"malformed JSON: {error}"]

            for message in messages:
                counts[level] += 1
                prefix = f"::{level} file={path}::" if os.getenv("GITHUB_ACTIONS") else f"{level}: {path}:"
                print(f"{prefix}{message}")

    print(f"Telemetry validation: {counts['error']} error(s), {counts['warning']} warning(s).")
    return int(counts["error"] > 0)


if __name__ == "__main__":
    raise SystemExit(run())
