# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

from typing import List, Dict

import requests

from parse_results import eprintln


def _post_webhook(webhook_url: str, payload: Dict[str, object], timeout: int = 10) -> bool:
    try:
        response = requests.post(webhook_url, json=payload, timeout=timeout)
        response.raise_for_status()
        return True
    except requests.HTTPError as exc:
        eprintln(
            f"[ERROR] Slack webhook HTTPError: "
            f"{exc.response.status_code} {exc.response.reason}"
        )
        return False
    except requests.RequestException as exc:
        eprintln(f"[ERROR] Slack webhook request failed: {type(exc).__name__}")
        return False


def send_text(webhook_url: str, text: str) -> bool:
    return _post_webhook(webhook_url, {"text": text})


def send_blocks(webhook_url: str, blocks: List[Dict[str, object]]) -> bool:
    return _post_webhook(webhook_url, {"blocks": blocks})
