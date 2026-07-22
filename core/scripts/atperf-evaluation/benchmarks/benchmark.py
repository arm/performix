# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

import csv
import json
import pathlib
from dataclasses import dataclass
from enum import Enum
from typing import Any, Callable, Dict, List, Optional

from framework.metadata import BenchmarkMetadata


@dataclass
class BenchmarkResult:
    """
    Simple data structure for benchmark results that can be written to CSV or JSON.
    Reusable across all benchmark types.
    """
    headers: List[str]
    rows: List[List[Any]]

    def __len__(self):
        """Return number of rows"""
        return len(self.rows)

    def to_csv(self, filepath: pathlib.Path):
        """Write the result to a CSV file"""
        with filepath.open('w', newline='') as f:
            writer = csv.writer(f)
            writer.writerow(self.headers)
            writer.writerows(self.rows)

    def to_json(self, filepath: pathlib.Path):
        """Write the result to a JSON file with headers and rows"""
        data = {
            "headers": self.headers,
            "rows": self.rows
        }
        with filepath.open('w+') as f:
            json.dump(data, f, indent=2)

    def get_column(self, column_name: str) -> List[Any]:
        """Get all values from a specific column"""
        if column_name not in self.headers:
            raise ValueError(f"Column '{column_name}' not found in headers")

        col_index = self.headers.index(column_name)
        return [row[col_index] for row in self.rows]

    def add_row(self, row: List[Any]):
        """Add a single row to the result"""
        if len(row) != len(self.headers):
            raise ValueError(f"Row length {len(row)} doesn't match headers length {len(self.headers)}")
        self.rows.append(row)


class Benchmark:
    name = "base"
    description = "Base benchmark class"
    long_description = "Base class for all benchmarks. Inherit and override description and long_description."

    def __init__(self):
        self.metadata = BenchmarkMetadata()
        self.metadata.benchmark_name = self.name

    def get_metadata(self) -> BenchmarkMetadata:
        return self.metadata

    @staticmethod
    def add_arguments(parser):
        pass

    def run(self, args) -> bool:
        raise NotImplementedError

    def report(self, args) -> BenchmarkMetadata:
        raise NotImplementedError
