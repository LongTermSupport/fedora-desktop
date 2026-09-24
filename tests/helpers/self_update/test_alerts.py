"""Tests for helpers.self_update.alerts — the optional Slack sink (Plan 00137 T0.4, T4.5).

Nothing here touches the network: every post goes through an injected opener that
records the request it was handed. Pinned:

1. **The webhook is validated, and never echoed.** A malformed one is refused with a
   message that does not contain it, because the URL is the secret.
2. **The message is the result record and nothing else**: no hostname, username or path
   beyond the repo-relative plays the record already carries.
3. **A delivery that fails says so**, as a short reason that does not contain the URL.
4. **No sink configured sends nothing and reports nothing.**
"""

from __future__ import annotations

import email.message
import json
import os
import sys
import unittest
import urllib.error

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import alerts

# Short on purpose: a real webhook's secret path is far longer, and this must never look
# like one to a secret scanner.
WEBHOOK = "https://hooks.slack.com/services/EXAMPLE/EXAMPLE/EXAMPLE"

RECORD = {
    "at": "2026-09-23T03:30:00Z", "phase": "play", "outcome": "play-failed",
    "old": "a" * 40, "new": "b" * 40, "plays": "playbooks/imports/play-claude-yolo.yml",
    "detail": "playbooks/imports/play-claude-yolo.yml exited 2; no reboot, retried next cycle",
}


class FakeResponse:
    def __init__(self, status: int) -> None:
        self.status = status

    def __enter__(self) -> FakeResponse:
        return self

    def __exit__(self, *exc: object) -> None:
        return None


class RecordingOpener:
    def __init__(self, *, status: int = 200, error: BaseException | None = None) -> None:
        self.requests: list[tuple[object, float]] = []
        self.status = status
        self.error = error

    def __call__(self, request: object, *, timeout: float) -> FakeResponse:
        self.requests.append((request, timeout))
        if self.error is not None:
            raise self.error
        return FakeResponse(self.status)


class TestWebhookValidation(unittest.TestCase):
    def test_a_slack_webhook_is_accepted_and_its_newline_dropped(self) -> None:
        self.assertEqual(alerts.parse_slack_webhook(WEBHOOK + "\n"), WEBHOOK)

    def test_anything_that_is_not_a_slack_webhook_is_refused_without_echoing_it(self) -> None:
        for bad in ("", "http://hooks.slack.com/services/X", "https://example.com/services/X",
                    "https://hooks.slack.com.example.com/services/X", WEBHOOK + "?x=1",
                    WEBHOOK + "\n" + WEBHOOK, "https://hooks.slack.com/"):
            with self.subTest(bad=bad), self.assertRaises(ValueError) as caught:
                alerts.parse_slack_webhook(bad)
            if bad:
                self.assertNotIn(bad.strip().splitlines()[0], str(caught.exception))


class TestMessage(unittest.TestCase):
    def test_the_message_names_the_outcome_phase_time_and_detail(self) -> None:
        text = alerts.slack_message(RECORD)
        for part in ("play-failed", "play", "2026-09-23T03:30:00Z", "exited 2", "bbbbbbbbbbbb"):
            self.assertIn(part, text)

    def test_the_message_carries_nothing_the_record_does_not(self) -> None:
        text = alerts.slack_message({key: "" for key in RECORD})
        self.assertNotIn("/home", text)
        self.assertNotIn(os.uname().nodename, text)

    def test_the_payload_is_slack_json_with_the_message_as_text(self) -> None:
        payload = json.loads(alerts.slack_payload(RECORD).decode("utf-8"))
        self.assertEqual(payload, {"text": alerts.slack_message(RECORD)})


class TestPost(unittest.TestCase):
    def test_a_2xx_answer_is_delivered(self) -> None:
        opener = RecordingOpener(status=200)
        self.assertIsNone(alerts.post_slack(WEBHOOK, RECORD, opener=opener))
        request, timeout = opener.requests[0]
        self.assertEqual(request.full_url, WEBHOOK)
        self.assertEqual(request.get_method(), "POST")
        self.assertEqual(request.get_header("Content-type"), "application/json")
        self.assertEqual(request.data, alerts.slack_payload(RECORD))
        self.assertEqual(timeout, alerts.TIMEOUT_SECONDS)

    def test_a_non_2xx_answer_is_a_failure(self) -> None:
        self.assertEqual(alerts.post_slack(WEBHOOK, RECORD, opener=RecordingOpener(status=302)), "HTTP 302")

    def test_an_http_error_is_a_failure_naming_the_status_not_the_url(self) -> None:
        error = urllib.error.HTTPError(WEBHOOK, 404, "Not Found", email.message.Message(), None)
        reason = alerts.post_slack(WEBHOOK, RECORD, opener=RecordingOpener(error=error))
        self.assertEqual(reason, "HTTP 404")

    def test_an_unreachable_host_or_a_timeout_is_a_failure_without_the_url(self) -> None:
        for error in (urllib.error.URLError("Name or service not known"), TimeoutError("timed out"),
                      OSError("Network is unreachable")):
            with self.subTest(error=type(error).__name__):
                reason = alerts.post_slack(WEBHOOK, RECORD, opener=RecordingOpener(error=error))
                self.assertIsNotNone(reason)
                assert reason is not None
                self.assertNotIn("EXAMPLE", reason)
                self.assertIn("not delivered", reason)


class TestSinks(unittest.TestCase):
    def test_no_sink_sends_nothing_and_reports_nothing(self) -> None:
        opener = RecordingOpener()
        self.assertEqual(alerts.Sinks(slack_webhook=None, opener=opener).deliver(RECORD), [])
        self.assertEqual(opener.requests, [])
        self.assertFalse(alerts.Sinks(slack_webhook=None).configured)

    def test_a_configured_sink_is_posted_to(self) -> None:
        opener = RecordingOpener()
        sinks = alerts.Sinks(slack_webhook=WEBHOOK, opener=opener)
        self.assertTrue(sinks.configured)
        self.assertEqual(sinks.deliver(RECORD), [])
        self.assertEqual(len(opener.requests), 1)

    def test_a_failed_delivery_is_named_by_its_sink(self) -> None:
        sinks = alerts.Sinks(slack_webhook=WEBHOOK, opener=RecordingOpener(status=500))
        self.assertEqual(sinks.deliver(RECORD), ["slack: HTTP 500"])


if __name__ == "__main__":
    unittest.main()
