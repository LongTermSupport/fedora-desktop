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
import http.client
import http.server
import json
import os
import sys
import threading
import unittest
import urllib.error
from unittest import mock

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

    def test_a_malformed_http_answer_is_a_failure_not_an_exception(self) -> None:
        """http.client raises these outside OSError. Escaping, one would leave the result
        recorded with an empty `alert`, which reads as delivered."""
        for error in (http.client.BadStatusLine("garbage"), http.client.LineTooLong("header line"),
                      http.client.IncompleteRead(b"partial")):
            with self.subTest(error=type(error).__name__):
                reason = alerts.post_slack(WEBHOOK, RECORD, opener=RecordingOpener(error=error))
                self.assertIsNotNone(reason)
                assert reason is not None
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
        error = urllib.error.HTTPError(WEBHOOK, 500, "Server Error", email.message.Message(), None)
        sinks = alerts.Sinks(slack_webhook=WEBHOOK, opener=RecordingOpener(error=error))
        self.assertEqual(sinks.deliver(RECORD), ["slack: HTTP 500"])


class StubSlack(http.server.BaseHTTPRequestHandler):
    """A loopback stand-in for Slack. Each path answers one way; every request is logged."""

    seen: list[tuple[str, str, bytes]] = []

    def _answer(self) -> None:
        length = int(self.headers.get("Content-Length") or 0)
        StubSlack.seen.append((self.command, self.path, self.rfile.read(length)))
        if self.path == "/ok":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")
        elif self.path == "/moved":
            self.send_response(302)
            self.send_header("Location", "/ok")
            self.end_headers()
        elif self.path == "/broken":
            self.send_response(500)
            self.end_headers()
        elif self.path == "/garbage":
            self.wfile.write(b"this is not an HTTP status line\r\n\r\n")
        self.close_connection = True

    do_GET = _answer
    do_POST = _answer

    def log_message(self, format: str, *args: object) -> None:
        return None


class TestTheRealOpener(unittest.TestCase):
    """Through `alerts`' own default opener, against a server on 127.0.0.1 only."""

    def setUp(self) -> None:
        StubSlack.seen = []
        self.server = http.server.HTTPServer(("127.0.0.1", 0), StubSlack)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.base = f"http://127.0.0.1:{self.server.server_address[1]}"
        # A proxy from the environment would take the request off the loopback.
        self.no_proxy = mock.patch.dict(os.environ, {"no_proxy": "*", "NO_PROXY": "*"})
        self.no_proxy.start()

    def tearDown(self) -> None:
        self.no_proxy.stop()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def post(self, path: str) -> str | None:
        return alerts.post_slack(self.base + path, RECORD, timeout=5)

    def test_an_accepted_post_is_delivered(self) -> None:
        self.assertIsNone(self.post("/ok"))
        self.assertEqual(StubSlack.seen, [("POST", "/ok", alerts.slack_payload(RECORD))])

    def test_a_redirect_is_a_failed_delivery_and_is_not_followed(self) -> None:
        """Followed, a 302 turns the POST into a GET with no body, and the 200 at the end
        of it would read as delivered."""
        self.assertEqual(self.post("/moved"), "HTTP 302")
        self.assertEqual([(method, path) for method, path, _ in StubSlack.seen], [("POST", "/moved")])

    def test_a_server_error_is_a_failed_delivery(self) -> None:
        self.assertEqual(self.post("/broken"), "HTTP 500")

    def test_an_answer_that_is_not_http_is_a_failed_delivery(self) -> None:
        reason = self.post("/garbage")
        self.assertIsNotNone(reason)
        assert reason is not None
        self.assertTrue(reason.startswith("not delivered ("), reason)


if __name__ == "__main__":
    unittest.main()
