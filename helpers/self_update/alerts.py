"""Where the self-update cycle's alerts go (Plan 00137 D8, Tasks 0.4 and 4.5).

The one sink is a Slack incoming webhook, and it is optional: an install that declares
none gets the journal and the host-health report, which every result reaches anyway.
The webhook URL is the secret. It is read from a root-only file the play provisions from
vault, and no message this module produces, error or otherwise, ever contains it.

A message is built from the result record only, so it carries what the record carries:
no hostname, no username, and no path but the repo-relative plays. One webhook per
install is how an owner with several servers tells them apart.
"""

from __future__ import annotations

import http.client
import json
import re
import urllib.error
import urllib.request
from collections.abc import Callable
from typing import Any

#: Slack's incoming-webhook and workflow-trigger hosts share this origin. The path is the
#: secret, so it is checked for shape only.
_SLACK_WEBHOOK = re.compile(r"^https://hooks\.slack\.com/[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)+$")
#: The cycle runs at night with nobody waiting on it, but a hung post would hold the play
#: lock and delay the countdown, so a slow Slack is a failed delivery, not a wait.
TIMEOUT_SECONDS = 10.0

Opener = Callable[..., Any]


class _RefuseRedirects(urllib.request.HTTPRedirectHandler):
    """A followed 302 or 303 turns the POST into a GET with no body, and a 200 at the end
    of it would read as delivered. Refused here, a 3xx reaches the caller as an HTTPError."""

    def redirect_request(self, req: object, fp: object, code: int, msg: str, headers: object,
                         newurl: str) -> None:
        return None


def _open(request: urllib.request.Request, *, timeout: float) -> Any:
    return urllib.request.build_opener(_RefuseRedirects()).open(request, timeout=timeout)


def parse_slack_webhook(text: str) -> str:
    """The webhook from the provisioned file's text. Raises ValueError, never naming it."""
    lines = text.splitlines()
    url = lines[0].strip() if len(lines) == 1 else ""
    if not _SLACK_WEBHOOK.match(url):
        raise ValueError(
            "the Slack webhook file does not hold exactly one Slack webhook URL "
            "(an https URL on hooks.slack.com with a path)"
        )
    return url


def slack_message(record: dict[str, str]) -> str:
    """The alert's text, from the result record's own fields and nothing else."""
    lines = [
        f"fedora-desktop self-update: {record.get('outcome', '')} at {record.get('phase', '')}"
        f" ({record.get('at', '')})",
    ]
    if record.get("detail"):
        lines.append(record["detail"])
    if record.get("plays"):
        lines.append(f"plays: {record['plays']}")
    if record.get("new"):
        lines.append(f"commit: {record['new'][:12]}")
    return "\n".join(lines)


def slack_payload(record: dict[str, str]) -> bytes:
    return json.dumps({"text": slack_message(record)}).encode("utf-8")


def post_slack(
    url: str, record: dict[str, str], *, opener: Opener = _open, timeout: float = TIMEOUT_SECONDS,
) -> str | None:
    """Post one alert. None when Slack accepted it, else a reason that omits the URL."""
    request = urllib.request.Request(
        url, data=slack_payload(record), headers={"Content-Type": "application/json"}, method="POST",
    )
    try:
        with opener(request, timeout=timeout) as response:
            status = response.status
    except urllib.error.HTTPError as error:
        error.close()
        return f"HTTP {error.code}"
    except urllib.error.URLError as error:
        return f"not delivered ({error.reason})"
    except OSError as error:
        return f"not delivered ({type(error).__name__}: {error})"
    except http.client.HTTPException as error:
        # Not an OSError: a malformed status line, an over-long header or a cut-off body.
        return f"not delivered ({type(error).__name__})"
    if not 200 <= status < 300:
        return f"HTTP {status}"
    return None


class Sinks:
    """The configured sinks. `deliver` returns one failure per sink that did not accept."""

    def __init__(self, *, slack_webhook: str | None, opener: Opener = _open) -> None:
        self._slack = slack_webhook
        self._opener = opener

    @property
    def configured(self) -> bool:
        return self._slack is not None

    def deliver(self, record: dict[str, str]) -> list[str]:
        if self._slack is None:
            return []
        reason = post_slack(self._slack, record, opener=self._opener)
        return [] if reason is None else [f"slack: {reason}"]
