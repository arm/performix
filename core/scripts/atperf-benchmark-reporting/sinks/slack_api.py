# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import json
from typing import Optional, Tuple
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

from parse_results import eprintln


SLACK_API_BASE = "https://slack.com/api"


def _http_post_json(url: str, token: str, payload: dict, timeout: int = 15) -> Tuple[bool, dict]:
    data = json.dumps(payload).encode("utf-8")
    req = Request(
        url,
        data=data,
        headers={
            "Content-Type": "application/json; charset=utf-8",
            "Authorization": f"Bearer {token}",
        },
        method="POST",
    )
    try:
        with urlopen(req, timeout=timeout) as resp:
            status = getattr(resp, "status", 200)
            body_raw = resp.read().decode("utf-8", errors="replace")
            try:
                body = json.loads(body_raw)
            except Exception:
                body = {"raw": body_raw}
            if 200 <= status < 300:
                return True, body
            eprintln(f"[WARN] Slack API non-2xx status: {status} body={body}")
            return False, body
    except HTTPError as e:
        eprintln(f"[ERROR] Slack API HTTPError: {e.code} {e.reason}")
        return False, {}
    except URLError as e:
        eprintln(f"[ERROR] Slack API URLError: {e.reason}")
        return False, {}
    except Exception as e:
        eprintln(f"[ERROR] Slack API unexpected error: {e}")
        return False, {}


def post_message(token: str, channel: str, text: str, thread_timestamp: Optional[str] = None) -> Tuple[bool, Optional[str]]:
    """Post a message using chat.postMessage. Returns (ok, timestamp).

    - channel: channel ID (recommended) or name that the bot has access to
    - thread_timestamp: if provided, posts as a reply in that thread
    """
    url = f"{SLACK_API_BASE}/chat.postMessage"
    payload = {"channel": channel, "text": text}
    if thread_timestamp:
        payload["thread_ts"] = thread_timestamp
    ok, body = _http_post_json(url, token, payload)
    if not ok:
        return False, None
    if not body.get("ok"):
        eprintln(f"[ERROR] Slack API chat.postMessage returned ok=false: {body}")
        return False, None
    timestamp = body.get("ts")
    if not timestamp:
        eprintln("[WARN] Slack API response missing timestamp")
    return True, timestamp