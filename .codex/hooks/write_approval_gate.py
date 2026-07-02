#!/usr/bin/env python3
"""Block Codex write tools unless the latest user prompt approves writes."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any


APPROVAL_PATTERNS = [
    r"\bapproved\b",
    r"\bapprove\b",
    r"\bgo ahead\b",
    r"\bdo it\b",
    r"\bok(?:ay)?[, ]+(?:do it|go ahead)\b",
    r"\blam di\b",
    r"\blam theo plan\b",
    r"\bok[, ]+lam\b",
    r"\btrien khai theo plan\b",
    r"\btrien khai di\b",
    r"\btrien khai luon\b",
    r"\bcho phep ghi file\b",
    r"\bcho phep sua\b",
    r"\bsua cac file nay\b",
    r"\bsua di\b",
    r"làm đi",
    r"làm theo plan",
    r"ok[, ]+làm",
    r"triển khai theo plan",
    r"triển khai đi",
    r"triển khai luôn",
    r"cho phép ghi file",
    r"cho phép sửa",
    r"sửa các file này",
    r"sửa đi",
]

READ_ONLY_MARKERS = [
    "why",
    "what happened",
    "explain",
    "review",
    "check",
    "status",
    "kiem tra",
    "kiểm tra",
    "tai sao",
    "tại sao",
    "vi sao",
    "vì sao",
    "ua",
    "ủa",
    "haizz",
]


def deny(reason: str) -> None:
    print(
        json.dumps(
            {
                "hookSpecificOutput": {
                    "hookEventName": "PreToolUse",
                    "permissionDecision": "deny",
                    "permissionDecisionReason": reason,
                }
            }
        )
    )


def allow() -> None:
    return


def text_from_content(value: Any) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        return "\n".join(text_from_content(item) for item in value)
    if isinstance(value, dict):
        for key in ("text", "content", "message", "prompt"):
            if key in value:
                text = text_from_content(value[key])
                if text:
                    return text
        return "\n".join(text_from_content(v) for v in value.values())
    return ""


def collect_user_messages(value: Any) -> list[str]:
    messages: list[str] = []
    if isinstance(value, dict):
        role = str(value.get("role") or value.get("author") or "").lower()
        msg_type = str(value.get("type") or value.get("kind") or "").lower()
        if role == "user" or msg_type in {"user_message", "user"}:
            text = text_from_content(value.get("content") or value.get("message") or value.get("prompt"))
            if text.strip():
                messages.append(text.strip())
        for nested_key in ("item", "payload", "message", "event", "data", "content"):
            nested = value.get(nested_key)
            if isinstance(nested, (dict, list)):
                messages.extend(collect_user_messages(nested))
    elif isinstance(value, list):
        for item in value:
            messages.extend(collect_user_messages(item))
    return messages


def latest_user_prompt(transcript_path: str | None) -> str:
    if not transcript_path:
        return ""
    path = Path(transcript_path)
    if not path.exists():
        return ""

    latest = ""
    try:
        for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue
            for message in collect_user_messages(event):
                latest = message
    except OSError:
        return ""
    return latest


def has_explicit_approval(prompt: str) -> bool:
    lowered = prompt.lower()
    return any(re.search(pattern, lowered, re.IGNORECASE) for pattern in APPROVAL_PATTERNS)


def looks_read_only(prompt: str) -> bool:
    lowered = prompt.lower()
    if "?" in prompt:
        return True
    return any(marker in lowered for marker in READ_ONLY_MARKERS)


def main() -> int:
    try:
        hook_input = json.load(sys.stdin)
    except json.JSONDecodeError:
        deny("Write blocked: hook could not parse Codex input, so approval could not be verified.")
        return 0

    tool_name = str(hook_input.get("tool_name") or "")
    if tool_name != "apply_patch":
        allow()
        return 0

    prompt = latest_user_prompt(hook_input.get("transcript_path"))
    if not prompt:
        deny("Write blocked: no latest user prompt was available to verify explicit approval.")
        return 0

    if has_explicit_approval(prompt):
        allow()
        return 0

    if looks_read_only(prompt):
        deny("Write blocked: latest user message looks like a question, complaint, screenshot/status check, or review request, not explicit approval.")
        return 0

    deny("Write blocked: latest user message does not contain explicit approval for file changes.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
