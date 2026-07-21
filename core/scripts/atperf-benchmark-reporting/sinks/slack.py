# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import json
from typing import List, Dict
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

from parse_results import eprintln


def _post_webhook(webhook_url: str, payload: Dict[str, object], timeout: int = 10) -> bool:
    data = json.dumps(payload).encode("utf-8")
    req = Request(
        webhook_url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urlopen(req, timeout=timeout) as resp:
            status = getattr(resp, "status", 200)
            if 200 <= status < 300:
                return True
            eprintln(f"[WARN] Slack webhook non-2xx status: {status}")
            return False
    except HTTPError as e:
        eprintln(f"[ERROR] Slack webhook HTTPError: {e.code} {e.reason}")
        return False
    except URLError as e:
        eprintln(f"[ERROR] Slack webhook URLError: {e.reason}")
        return False
    except Exception as e:
        eprintln(f"[ERROR] Slack webhook unexpected error: {e}")
        return False


def send_text(webhook_url: str, text: str) -> bool:
    return _post_webhook(webhook_url, {"text": text})


def send_blocks(webhook_url: str, blocks: List[Dict[str, object]]) -> bool:
    return _post_webhook(webhook_url, {"blocks": blocks})
