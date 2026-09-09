# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import json
import tempfile
import unittest
from pathlib import Path

from merge_istanbul_coverage import (
    merge_coverage_directories,
    merge_file_coverage,
    write_merged_coverage,
)


def file_coverage(path: str, hits: int, *, all_files: bool = False) -> dict:
    coverage = {
        "path": path,
        "statementMap": {
            "0": {
                "start": {"line": 1, "column": 0},
                "end": {"line": 1, "column": 1},
            }
        },
        "fnMap": {},
        "branchMap": {},
        "s": {"0": hits},
        "f": {},
        "b": {},
    }
    if all_files:
        coverage["all"] = True
    return coverage


class MergeIstanbulCoverageTests(unittest.TestCase):
    def test_preserves_uncovered_files_and_adds_measured_coverage(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            baseline_dir = root / "baseline"
            measured_dir = root / "measured"
            baseline_dir.mkdir()
            measured_dir.mkdir()
            touched_path = "/source/touched.js"
            untouched_path = "/source/untouched.js"

            (baseline_dir / "baseline.json").write_text(
                json.dumps(
                    {
                        touched_path: file_coverage(
                            touched_path, 0, all_files=True
                        ),
                        untouched_path: file_coverage(
                            untouched_path, 0, all_files=True
                        ),
                    }
                ),
                encoding="utf-8",
            )
            (measured_dir / "first.json").write_text(
                json.dumps({touched_path: file_coverage(touched_path, 1)}),
                encoding="utf-8",
            )
            (measured_dir / "second.json").write_text(
                json.dumps({touched_path: file_coverage(touched_path, 2)}),
                encoding="utf-8",
            )

            coverage = merge_coverage_directories([baseline_dir, measured_dir])

            self.assertEqual(3, coverage[touched_path]["s"]["0"])
            self.assertEqual(0, coverage[untouched_path]["s"]["0"])

    def test_rejects_different_location_maps(self):
        current = file_coverage("/source/example.js", 1)
        incoming = file_coverage("/source/example.js", 1)
        incoming["statementMap"]["0"]["end"]["column"] = 2

        with self.assertRaisesRegex(ValueError, "statementMap values do not match"):
            merge_file_coverage(current, incoming)

    def test_writes_the_merged_coverage_map(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            input_dir = root / "input"
            input_dir.mkdir()
            source_path = "/source/example.js"
            output_file = root / "output" / "coverage.json"
            (input_dir / "coverage.json").write_text(
                json.dumps({source_path: file_coverage(source_path, 1)}),
                encoding="utf-8",
            )

            write_merged_coverage([input_dir], output_file)

            written_coverage = json.loads(output_file.read_text(encoding="utf-8"))
            self.assertEqual(1, written_coverage[source_path]["s"]["0"])


if __name__ == "__main__":
    unittest.main()
