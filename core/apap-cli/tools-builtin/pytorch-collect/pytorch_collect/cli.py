# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import argparse
import runpy
import sys
from pathlib import Path
from .tracing import ApiCallTrace, OperatorCallTrace
from .tracker import TraceTracker
from .writer import ParquetWriter, PrintWriter, Writer


def parse_args():
    parser = argparse.ArgumentParser(
        prog="pytorch-collect",
        description="Collect PyTorch API and operator call trace data.",
    )
    parser.add_argument(
        "-o",
        "--output",
        default=".",
        help="Directory for collected trace output (for parquet trace writer only).",
    )
    parser.add_argument(
        "-b",
        "--batch",
        default="1024",
        help="Batch size of parquet file writer (for parquet trace writer only).",
    )
    parser.add_argument(
        "--writer",
        choices=("parquet", "print"),
        default="parquet",
        help="Trace writer backend to use.",
    )
    parser.add_argument(
        "--completion-marker",
        help="Create this file after output has been finalized.",
    )
    parser.add_argument(
        "module",
        nargs=argparse.REMAINDER,
        help="Python module and optional arguments to run under PyTorch tracing",
    )

    return parser.parse_args()


def trace_module(writer: Writer, module: Path, args: list[str]):
    tracker = TraceTracker(writer)

    with ApiCallTrace(tracker), OperatorCallTrace(tracker):
        module_dir = str(module.parent)

        sys.path = [module_dir, *sys.path]
        sys.argv = [str(module), *args]
        runpy.run_path(str(module), run_name='__main__')


def main() -> int:
    args = parse_args()

    if not args.module:
        raise ValueError('module must be specified')

    if args.module[0] == '--':
        args.module.pop(0)

    module = Path(args.module[0])

    if not module.is_file():
        raise ValueError('module must be a file')

    if args.writer == 'parquet':
        writer = ParquetWriter(args.output, int(args.batch))
    else:
        writer = PrintWriter()

    interrupted = False
    try:
        with writer:
            try:
                trace_module(writer, module, args.module[1:])
            except KeyboardInterrupt:
                # If we're interrupted whilst running the subject module, we'll
                # want the writer to finish writing...
                interrupted = True
    except KeyboardInterrupt:
        # ... but if we're interrupted whilst that's happening then we can't
        # be sure of the state of the output.
        return 130
    if args.completion_marker:
        Path(args.completion_marker).touch()
    if interrupted:
        return 130

    return 0


if __name__ == "__main__":
    sys.exit(main())
