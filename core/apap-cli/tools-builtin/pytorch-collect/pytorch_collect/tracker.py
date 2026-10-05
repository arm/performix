# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import time
import logging
from .writer import ApiCall, OperatorCall, Writer
from typing import Any, Optional


def time_monotnonic_raw() -> int:
    return time.clock_gettime_ns(time.CLOCK_MONOTONIC_RAW)


class TraceTracker:
    def __init__(self, writer: Writer):
        self.writer = writer
        self.api_call: Optional[ApiCall] = None
        self.operator_call: Optional[OperatorCall] = None
        self.next_api_call_id = 0
        self.next_operator_call_id = 0

    def begin_api_call(self, name: str, args: list[Any], kwargs: dict[str, Any]) -> int:
        """
        Track the beginning of a PyTorch API call.
        """
        if self.api_call is not None:
            logging.warning("Resetting tracked API call!")

        call_id = self.next_api_call_id
        self.next_api_call_id += 1
        self.api_call = ApiCall(call_id, name)
        self.api_call.timestamp_begin = time_monotnonic_raw()
        self.api_call.args = args
        self.api_call.kwargs = kwargs
        return call_id

    def end_api_call(self, call_id: int, output: Any):
        """
        Track the end of a PyTorch API call.
        """
        if self.api_call is None:
            logging.warning("No tracked API call!")
            return

        if self.api_call.id != call_id:
            logging.warning(
                "API call ID mismatch: expected %d, got %d!",
                self.api_call.id,
                call_id,
            )
            return

        self.api_call.timestamp_end = time_monotnonic_raw()
        self.api_call.output = output
        self.writer.write_api_call(self.api_call)
        self.api_call = None

    def begin_operator_call(
        self,
        name: str,
        args: list[Any],
        kwargs: dict[str, Any],
    ) -> int:
        """
        Track the beginning of a PyTorch operator call.
        """
        if self.api_call is None:
            logging.warning("No tracked API call!")
            return -1

        if self.operator_call is not None:
            logging.warning("Resetting tracked operator call!")

        call_id = self.next_operator_call_id
        self.next_operator_call_id += 1
        self.operator_call = OperatorCall(call_id, name)
        self.operator_call.timestamp_begin = time_monotnonic_raw()
        self.operator_call.args = args
        self.operator_call.kwargs = kwargs
        return call_id

    def end_operator_call(self, call_id: int, output: Any):
        """
        Track the end of a PyTorch operator call.
        """
        if self.api_call is None:
            logging.warning("No tracked API call!")
            return

        if self.operator_call is None:
            logging.warning("No tracked operator call!")
            return

        if self.operator_call.id != call_id:
            logging.warning(
                "Operator call ID mismatch: expected %d, got %s!",
                self.operator_call.id,
                call_id,
            )
            return

        self.operator_call.timestamp_end = time_monotnonic_raw()
        self.operator_call.output = output
        self.api_call.operator_calls.append(self.operator_call)
        self.operator_call = None
