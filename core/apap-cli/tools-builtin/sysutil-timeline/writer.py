# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import csv
from typing import Iterable


class CsvWriter:
    def __init__(self, path: str, header: Iterable[str], flush: bool = False) -> None:
        """Create a CSV writer with a fixed header row."""
        self._path = path
        self._flush = flush
        self._file = open(path, "w", newline="", encoding="utf-8")
        self._writer = csv.writer(self._file)
        self._writer.writerow(list(header))
        if self._flush:
            self._file.flush()

    def write_row(self, values: Iterable[object]) -> None:
        """Write a row to the CSV, optionally flushing."""
        self._writer.writerow(list(values))
        if self._flush:
            self._file.flush()

    def close(self) -> None:
        """Close the underlying file handle."""
        if self._file:
            self._file.close()
            self._file = None
