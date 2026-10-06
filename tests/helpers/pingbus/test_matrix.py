"""Unit tests for helpers/pingbus/matrix.py: the Matrix client-server API client.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_matrix

Most tests talk real HTTP on loopback to the U08 fake homeserver (`fake_client_api.serve`);
the few that need a response the fake cannot give (a `Retry-After` header, a dropped
connection) inject an opener. Sleeps are injected, so nothing here waits on a clock.
"""

from __future__ import annotations

import contextlib
import email.message
import http.server
import io
import json
import os
import pathlib
import sys
import tempfile
import threading
import traceback
import unittest
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Iterator
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import config, limits, matrix, protocol
from tests.helpers.pingbus import fake_client_api

SN = "team-a.agent-bus.internal"
AGENT = "repo.1+ws.podman"
OTHER = "repo.2+ws.podman"
PING = protocol.build_ping("review", [f"@{OTHER}:{SN}"])
LOOPBACK = "127.0.0.1"


def loopback_client(base_url: str, token: str, **kwargs: object) -> matrix.Client:
    """A client allowed plain HTTP to the loopback fakes, as a bundle listing it would be."""
    return matrix.Client(base_url, token, plain_http_hosts=(LOOPBACK,), **kwargs)


class RecordingServer:
    """A loopback HTTP server that answers every request with one canned response and
    records what it was sent: the target of a redirect, or a proxy that must stay unused."""

    def __init__(self, status: int = 200, headers: dict[str, str] | None = None, body: bytes = b"{}") -> None:
        self.requests: list[tuple[str, str, dict[str, str]]] = []
        outer = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def _handle(self) -> None:
                outer.requests.append((self.command, self.path, dict(self.headers.items())))
                length = int(self.headers.get("Content-Length") or 0)
                if length:
                    self.rfile.read(length)
                self.send_response(status)
                for name, value in (headers or {}).items():
                    self.send_header(name, value)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            do_GET = do_PUT = do_POST = do_CONNECT = _handle

            def log_message(self, format: str, *args: object) -> None:
                """Requests are kept in `requests`; nothing is printed."""

        self._server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self._server.daemon_threads = True
        self.url = f"http://127.0.0.1:{self._server.server_address[1]}"

    @contextlib.contextmanager
    def running(self) -> Iterator[str]:
        thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        thread.start()
        try:
            yield self.url
        finally:
            self._server.shutdown()
            self._server.server_close()
            thread.join(5)


def http_error(status: int, headers: dict[str, str] | None = None, body: dict | None = None) -> urllib.error.HTTPError:
    hdrs = email.message.Message()
    for name, value in (headers or {}).items():
        hdrs[name] = value
    raw = json.dumps(body if body is not None else {}).encode()
    return urllib.error.HTTPError("http://127.0.0.1/x", status, "error", hdrs, io.BytesIO(raw))


class FakeResponse(io.BytesIO):
    def __init__(self, status: int, payload: bytes) -> None:
        super().__init__(payload)
        self.status = status


class ScriptedOpener:
    """An opener answering from a script: an exception to raise, or (status, JSON)."""

    def __init__(self, *steps: object) -> None:
        self.steps = list(steps)
        self.calls: list[tuple[urllib.request.Request, float]] = []

    def open(self, request: urllib.request.Request, timeout: float) -> FakeResponse:
        self.calls.append((request, timeout))
        step = self.steps.pop(0)
        if isinstance(step, BaseException):
            raise step
        status, body = step
        return FakeResponse(status, json.dumps(body).encode())


class LosesFirstResponse:
    """Delivers every request to the real server, but loses the first response, as a
    timeout after the server has acted does."""

    def __init__(self, real: urllib.request.OpenerDirector) -> None:
        self.real = real
        self.paths: list[str] = []
        self.lost = False

    def open(self, request: urllib.request.Request, timeout: float) -> object:
        self.paths.append(urllib.parse.urlsplit(request.full_url).path)
        response = self.real.open(request, timeout=timeout)
        if not self.lost:
            self.lost = True
            response.close()
            raise TimeoutError("timed out")
        return response


class TeamRoom(unittest.TestCase):
    """A fake homeserver over loopback HTTP: `admin`'s team room with two agents joined
    and a third user invited but not joined."""

    def setUp(self) -> None:
        self.fake = fake_client_api.FakeHomeserver(server_name=SN)
        self.admin = self.fake.add_user("admin", admin=True)
        self.agent = self.fake.add_user(AGENT)
        self.other = self.fake.add_user(OTHER)
        self.invited = self.fake.add_user("repo.3+ws.podman")
        self.tokens = {uid: self.fake.mint_token(uid)
                       for uid in (self.admin, self.agent, self.other, self.invited)}
        status, body = self.fake.request("POST", "/_matrix/client/v3/createRoom", body={
            "preset": "private_chat", "room_version": "12", "name": "team-a",
            "power_level_content_override": protocol.expected_power_levels([]),
            "invite": [self.agent, self.other, self.invited]}, token=self.tokens[self.admin])
        self.assertEqual(status, 200, body)
        self.room = body["room_id"]
        for uid in (self.agent, self.other):
            path = f"/_matrix/client/v3/rooms/{urllib.parse.quote(self.room, safe='')}/join"
            self.assertEqual(self.fake.request("POST", path, body={}, token=self.tokens[uid])[0], 200)
        self.sleeps: list[float] = []
        self.base = self.enterContext(fake_client_api.serve(self.fake))

    def client(self, uid: str | None = None, **kwargs: object) -> matrix.Client:
        kwargs.setdefault("sleep", self.sleeps.append)
        return loopback_client(self.base, self.tokens[uid or self.agent], **kwargs)

    def events_of(self, event_type: str) -> list[fake_client_api.Event]:
        return [e for e in self.fake.rooms[self.room].events if e.type == event_type]


class BearerHeaderTest(TeamRoom):
    def test_token_travels_only_in_the_authorization_header(self) -> None:
        self.assertEqual(self.client().whoami()["user_id"], self.agent)
        record = self.fake.http_log[-1]
        self.assertEqual(record.headers["Authorization"], f"Bearer {self.tokens[self.agent]}")
        self.assertNotIn(self.tokens[self.agent], record.path)
        self.assertNotIn(self.tokens[self.agent], json.dumps(record.query))

    def test_authorization_is_an_unredirected_header(self) -> None:
        opener = ScriptedOpener((200, {"user_id": self.agent}))
        loopback_client(self.base, self.tokens[self.agent], opener=opener).whoami()
        request = opener.calls[0][0]
        self.assertEqual(request.unredirected_hdrs.get("Authorization"), f"Bearer {self.tokens[self.agent]}")
        self.assertNotIn("Authorization", request.headers)

    def test_unauthenticated_call_sends_no_authorization(self) -> None:
        self.client().call("GET", "/_matrix/client/versions", auth=False)
        self.assertNotIn("Authorization", self.fake.http_log[-1].headers)


class NoRedirectTest(TeamRoom):
    def test_a_redirect_is_refused_and_never_followed(self) -> None:
        target = RecordingServer()
        with target.running() as target_url:
            redirector = RecordingServer(302, {"Location": target_url + "/_matrix/client/v3/account/whoami"})
            with redirector.running() as redirect_url:
                client = loopback_client(redirect_url, self.tokens[self.agent], sleep=self.sleeps.append)
                with self.assertRaises(matrix.MatrixError) as caught:
                    client.whoami()
        self.assertEqual(len(redirector.requests), 1)
        self.assertEqual(target.requests, [])
        self.assertEqual(caught.exception.status, 302)
        self.assertEqual(caught.exception.exit_code, 7)

    def test_a_same_origin_redirect_is_refused_too(self) -> None:
        redirector = RecordingServer(301, {"Location": "/_matrix/client/v3/account/whoami"})
        with redirector.running() as url:
            with self.assertRaises(matrix.MatrixError):
                loopback_client(url, self.tokens[self.agent]).whoami()
        self.assertEqual(len(redirector.requests), 1)


class NoProxyTest(TeamRoom):
    def test_proxy_environment_is_ignored(self) -> None:
        proxy = RecordingServer(200, body=b'{"user_id": "@nobody:example.invalid"}')
        with proxy.running() as proxy_url:
            env = {name: proxy_url for name in ("http_proxy", "HTTP_PROXY", "https_proxy",
                                                "HTTPS_PROXY", "all_proxy", "ALL_PROXY")}
            with mock.patch.dict(os.environ, env):
                os.environ.pop("no_proxy", None)
                os.environ.pop("NO_PROXY", None)
                answer = self.client().whoami()
        self.assertEqual(answer["user_id"], self.agent)
        self.assertEqual(proxy.requests, [])

    def test_default_opener_has_no_proxy_handler(self) -> None:
        with mock.patch.dict(os.environ, {"http_proxy": "http://127.0.0.1:9"}):
            opener = matrix.default_opener()
        self.assertEqual([h for h in opener.handlers if isinstance(h, urllib.request.ProxyHandler)], [])
        self.assertEqual([h for h in opener.handlers if type(h) is urllib.request.HTTPRedirectHandler], [])


class TransactionIdTest(TeamRoom):
    def test_send_after_a_lost_response_reuses_the_txn_id(self) -> None:
        opener = LosesFirstResponse(matrix.default_opener())
        event_id = self.client(opener=opener).send_message(self.room, PING)
        sends = [p for p in opener.paths if "/send/" in p]
        self.assertEqual(len(sends), 2)
        self.assertEqual(sends[0], sends[1])
        events = self.events_of(protocol.EVENT_MESSAGE)
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0].event_id, event_id)
        self.assertEqual(self.sleeps, [matrix.SEND_RETRY_WAIT_S])

    def test_each_send_has_its_own_txn_id(self) -> None:
        client = self.client()
        first = client.send_message(self.room, PING)
        second = client.send_message(self.room, PING)
        self.assertNotEqual(first, second)
        self.assertEqual(len(self.events_of(protocol.EVENT_MESSAGE)), 2)

    def test_a_given_txn_id_is_used_and_reuse_returns_the_first_event(self) -> None:
        client = self.client()
        first = client.send_message(self.room, PING, txn_id="pb-fixed.1")
        again = client.send_message(self.room, PING, txn_id="pb-fixed.1")
        self.assertEqual(first, again)
        self.assertTrue(self.fake.http_log[-1].path.endswith("/send/m.room.message/pb-fixed.1"))

    def test_txn_id_grammar_is_checked_before_any_request(self) -> None:
        for bad in ("", "a/b", "a b", "x" * 65, "é"):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                self.client().send_message(self.room, PING, txn_id=bad)
        self.assertEqual(self.fake.http_log, [])

    def test_send_gives_up_after_the_retries_as_unreachable(self) -> None:
        opener = ScriptedOpener(*[TimeoutError("timed out")] * matrix.SEND_TRIES)
        client = loopback_client(self.base, self.tokens[self.agent], opener=opener, sleep=self.sleeps.append)
        with self.assertRaises(matrix.Unreachable) as caught:
            client.send_message(self.room, PING)
        self.assertEqual(caught.exception.exit_code, 7)
        paths = {c[0].full_url for c in opener.calls}
        self.assertEqual(len(opener.calls), matrix.SEND_TRIES)
        self.assertEqual(len(paths), 1)

    def test_a_non_send_request_is_not_retried_on_a_transport_error(self) -> None:
        opener = ScriptedOpener(TimeoutError("timed out"), (200, {}))
        client = loopback_client(self.base, self.tokens[self.agent], opener=opener, sleep=self.sleeps.append)
        with self.assertRaises(matrix.Unreachable):
            client.whoami()
        self.assertEqual(len(opener.calls), 1)


class StatusMappingTest(TeamRoom):
    def test_401_is_authentication_refused(self) -> None:
        client = loopback_client(self.base, "not-a-token", sleep=self.sleeps.append)
        with self.assertRaises(matrix.AuthRefused) as caught:
            client.whoami()
        self.assertEqual((caught.exception.status, caught.exception.errcode), (401, "M_UNKNOWN_TOKEN"))
        self.assertEqual(caught.exception.exit_code, 8)

    def test_a_logged_out_token_is_authentication_refused(self) -> None:
        self.fake.logout_all(self.agent)
        with self.assertRaises(matrix.AuthRefused):
            self.client().send_message(self.room, PING)

    def test_403_is_forbidden_and_means_not_trusted_or_not_joined(self) -> None:
        with self.assertRaises(matrix.Forbidden) as caught:
            self.client(self.invited).send_message(self.room, PING)
        self.assertEqual((caught.exception.status, caught.exception.errcode), (403, "M_FORBIDDEN"))
        self.assertEqual(caught.exception.exit_code, 10)

    def test_404_is_not_found(self) -> None:
        with self.assertRaises(matrix.NotFound) as caught:
            self.client().get_event(self.room, "$" + "A" * 43)
        self.assertEqual(caught.exception.status, 404)
        self.assertEqual(caught.exception.exit_code, 7)

    def test_429_honours_retry_after_ms_then_succeeds(self) -> None:
        self.fake.inject("PUT", r"/send/", 429, {"errcode": "M_LIMIT_EXCEEDED", "error": "Too many requests",
                                                 "retry_after_ms": 1500}, times=2)
        event_id = self.client().send_message(self.room, PING)
        self.assertEqual(self.sleeps, [1.5, 1.5])
        self.assertEqual([e.event_id for e in self.events_of(protocol.EVENT_MESSAGE)], [event_id])

    def test_429_after_the_last_try_is_rate_limited(self) -> None:
        self.fake.inject("GET", r"/whoami$", 429, {"errcode": "M_LIMIT_EXCEEDED", "error": "slow down"},
                         times=limits.SERVER_429_MAX_TRIES)
        with self.assertRaises(matrix.RateLimited) as caught:
            self.client().whoami()
        self.assertEqual(caught.exception.exit_code, 9)
        self.assertEqual(self.sleeps, [limits.SERVER_429_DEFAULT_WAIT_S] * (limits.SERVER_429_MAX_TRIES - 1))

    def test_retry_after_header_outranks_the_body(self) -> None:
        opener = ScriptedOpener(http_error(429, {"Retry-After": "7"}, {"retry_after_ms": 1000}),
                                (200, {"user_id": self.agent}))
        loopback_client(self.base, "t0ken", opener=opener, sleep=self.sleeps.append).whoami()
        self.assertEqual(self.sleeps, [7])

    def test_unusable_retry_hints_fall_back_to_the_default(self) -> None:
        opener = ScriptedOpener(http_error(429, {"Retry-After": "soon"}, {"retry_after_ms": "1000"}),
                                (200, {"user_id": self.agent}))
        loopback_client(self.base, "t0ken", opener=opener, sleep=self.sleeps.append).whoami()
        self.assertEqual(self.sleeps, [limits.SERVER_429_DEFAULT_WAIT_S])

    def test_a_wait_beyond_the_cap_is_not_slept(self) -> None:
        over_s = limits.SERVER_429_WAIT_MAX_S + 1
        for hint in (http_error(429, {"Retry-After": str(over_s)}),
                     http_error(429, body={"retry_after_ms": over_s * 1000})):
            with self.subTest(hint=hint.headers.items()):
                self.sleeps.clear()
                opener = ScriptedOpener(hint, (200, {"user_id": self.agent}))
                with self.assertRaises(matrix.RateLimited) as caught:
                    loopback_client(self.base, "t0ken", opener=opener, sleep=self.sleeps.append).whoami()
                self.assertEqual(caught.exception.exit_code, 9)
                self.assertEqual(len(opener.calls), 1)
                self.assertEqual(self.sleeps, [])

    def test_a_wait_at_the_cap_is_slept(self) -> None:
        opener = ScriptedOpener(http_error(429, {"Retry-After": str(limits.SERVER_429_WAIT_MAX_S)}),
                                (200, {"user_id": self.agent}))
        loopback_client(self.base, "t0ken", opener=opener, sleep=self.sleeps.append).whoami()
        self.assertEqual(self.sleeps, [limits.SERVER_429_WAIT_MAX_S])

    def test_5xx_is_a_matrix_error_with_exit_7(self) -> None:
        self.fake.inject("GET", r"/whoami$", 500, {"errcode": "M_UNKNOWN", "error": "boom"})
        with self.assertRaises(matrix.MatrixError) as caught:
            self.client().whoami()
        self.assertEqual((caught.exception.status, caught.exception.exit_code), (500, 7))

    def test_a_malformed_errcode_is_not_kept(self) -> None:
        self.fake.inject("GET", r"/whoami$", 400, {"errcode": "not\nan errcode", "error": "x"})
        with self.assertRaises(matrix.MatrixError) as caught:
            self.client().whoami()
        self.assertIsNone(caught.exception.errcode)

    def test_connection_refused_is_unreachable(self) -> None:
        dead = RecordingServer()
        url = dead.url
        dead._server.server_close()
        with self.assertRaises(matrix.Unreachable) as caught:
            loopback_client(url, "t0ken").whoami()
        self.assertEqual(caught.exception.exit_code, 7)

    def test_a_non_json_or_non_object_answer_is_unreachable(self) -> None:
        for body in (b"<html>", b"[1, 2]"):
            server = RecordingServer(200, body=body)
            with self.subTest(body=body), server.running() as url, self.assertRaises(matrix.Unreachable):
                loopback_client(url, "t0ken").whoami()

    def test_an_oversized_answer_is_unreachable(self) -> None:
        server = RecordingServer(200, body=b'{"a": "' + b"x" * 64 + b'"}')
        with server.running() as url, mock.patch.object(matrix, "MAX_RESPONSE_BYTES", 32):
            with self.assertRaises(matrix.Unreachable):
                loopback_client(url, "t0ken").whoami()

    def test_exit_codes_match_the_cli_table(self) -> None:
        from helpers.pingbus import cli
        self.assertEqual(matrix.Unreachable.exit_code, cli.EXIT_UNREACHABLE)
        self.assertEqual(matrix.AuthRefused.exit_code, cli.EXIT_AUTH)
        self.assertEqual(matrix.RateLimited.exit_code, cli.EXIT_RATE)
        self.assertEqual(matrix.Forbidden.exit_code, cli.EXIT_UNTRUSTED)


class TokenNeverInErrorsTest(TeamRoom):
    """Every error a client can raise, rendered every way it could be shown."""

    def assert_clean(self, exc: BaseException, token: str) -> None:
        shown = [str(exc), repr(exc), "".join(traceback.format_exception(exc)), repr(exc.args),
                 repr(vars(exc))]
        for text in shown:
            self.assertNotIn(token, text)

    def raised(self, client: matrix.Client, call: str = "whoami") -> matrix.MatrixError:
        with self.assertRaises(matrix.MatrixError) as caught:
            getattr(client, call)() if call == "whoami" else client.send_message(self.room, PING)
        return caught.exception

    def test_no_error_carries_the_token(self) -> None:
        token = self.tokens[self.agent]
        echo = {"errcode": "M_UNKNOWN", "error": f"bad header Bearer {token}"}
        cases = {
            "401": lambda: self.raised(loopback_client(self.base, token + "x")),
            "403": lambda: self.raised(loopback_client(self.base, self.tokens[self.invited]), "send"),
            "server echoes the token": lambda: (
                self.fake.inject("GET", r"/whoami$", 400, echo),
                self.raised(self.client()))[1],
            "429 exhausted": lambda: (
                self.fake.inject("GET", r"/whoami$", 429, echo, times=3),
                self.raised(self.client()))[1],
            "transport": lambda: self.raised(loopback_client(
                self.base, token, opener=ScriptedOpener(OSError(f"reset {token}")))),
            "send retries exhausted": lambda: self.raised(loopback_client(
                self.base, token, opener=ScriptedOpener(*[TimeoutError(token)] * matrix.SEND_TRIES),
                sleep=self.sleeps.append), "send"),
        }
        for name, make in cases.items():
            with self.subTest(case=name):
                self.assert_clean(make(), token)
                self.assert_clean(make(), token + "x")

    def test_redirect_error_carries_no_token(self) -> None:
        token = self.tokens[self.agent]
        redirector = RecordingServer(302, {"Location": f"http://127.0.0.1:1/?t={token}"})
        with redirector.running() as url:
            with self.assertRaises(matrix.MatrixError) as caught:
                loopback_client(url, token).whoami()
        self.assert_clean(caught.exception, token)

    def test_client_repr_hides_the_token(self) -> None:
        client = self.client()
        self.assertNotIn(self.tokens[self.agent], repr(client))
        self.assertNotIn(self.tokens[self.agent], str(client))


class EndpointsTest(TeamRoom):
    def test_join_accepts_an_invite(self) -> None:
        self.client(self.invited).join(self.room)
        self.assertEqual(self.fake.rooms[self.room].membership(self.invited), "join")

    def test_leave_rejects_an_invite(self) -> None:
        self.client(self.invited).leave(self.room)
        self.assertEqual(self.fake.rooms[self.room].membership(self.invited), "leave")
        self.assertEqual(self.fake.http_log[-1].method, "POST")
        self.assertTrue(self.fake.http_log[-1].path.endswith("/leave"))
        with self.assertRaises(ValueError):
            self.client(self.invited).leave("!short")

    def test_sync_passes_since_filter_and_timeout(self) -> None:
        client = self.client()
        first = client.sync(timeout_ms=0, filter={"room": {"rooms": [self.room]}})
        self.assertIn("next_batch", first)
        record = self.fake.http_log[-1]
        self.assertEqual(record.query["timeout"], "0")
        self.assertEqual(json.loads(record.query["filter"]), {"room": {"rooms": [self.room]}})
        self.assertNotIn("since", record.query)
        client.sync(since=first["next_batch"], timeout_ms=0)
        self.assertEqual(self.fake.http_log[-1].query["since"], first["next_batch"])

    def test_sync_socket_timeout_covers_the_long_poll(self) -> None:
        opener = ScriptedOpener((200, {"next_batch": "1"}))
        loopback_client(self.base, "t0ken", opener=opener).sync(since="0", timeout_ms=30_000)
        self.assertEqual(opener.calls[0][1], 30 + matrix.REQUEST_TIMEOUT_S)

    def test_sync_timeout_must_be_bounded(self) -> None:
        for bad in (-1, limits.SYNC_LONG_POLL_S * 1000 + 1, True, 1.5):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                self.client().sync(timeout_ms=bad)

    def test_get_event_and_state(self) -> None:
        client = self.client()
        event_id = client.send_message(self.room, PING)
        event = client.get_event(self.room, event_id)
        self.assertEqual((event["event_id"], event["content"]), (event_id, PING))
        create = client.get_state(self.room, "m.room.create", as_event=True)
        self.assertEqual(create["sender"], self.admin)
        levels = client.get_state(self.room, "m.room.power_levels")
        self.assertNotIn("sender", levels)

    def test_put_state_under_the_own_user_id(self) -> None:
        status = {"v": 1, "state": protocol.STATUS_LISTENING, "until": 1}
        event_id = self.client().put_state(self.room, protocol.EVENT_STATUS, self.agent, status)
        self.assertTrue(protocol.is_event_id(event_id))
        self.assertIn(urllib.parse.quote(self.agent, safe=""), self.fake.http_log[-1].path)

    def test_messages_fills_forward(self) -> None:
        client = self.client()
        since = client.sync(timeout_ms=0)["next_batch"]
        sent = client.send_message(self.room, PING)
        page = client.messages(self.room, from_token=since, limit=5,
                               filter={"types": [protocol.EVENT_MESSAGE]})
        self.assertEqual([e["event_id"] for e in page["chunk"]], [sent])
        self.assertEqual(self.fake.http_log[-1].query["dir"], "f")

    def test_ids_are_checked_before_any_request(self) -> None:
        client = self.client()
        calls = [lambda: client.join("!short"), lambda: client.get_event(self.room, "$bad"),
                 lambda: client.get_event("room", "$" + "A" * 43),
                 lambda: client.send_message("!x/../y", PING)]
        for call in calls:
            with self.assertRaises(ValueError):
                call()
        self.assertEqual(self.fake.http_log, [])

    def test_path_segments_are_quoted(self) -> None:
        self.assertEqual(matrix.path("rooms", "!a:b", "state", "agent_bus.status", "@x+y:z"),
                         "/_matrix/client/v3/rooms/%21a%3Ab/state/agent_bus.status/%40x%2By%3Az")

    def test_base_url_must_be_a_bare_http_origin(self) -> None:
        for bad in ("ftp://127.0.0.1", "http://127.0.0.1/sub", "http://127.0.0.1?x=1", "127.0.0.1:80",
                    "http://u:p@127.0.0.1"):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                loopback_client(bad, "t0ken")

    def test_plain_http_only_to_a_listed_ip_literal(self) -> None:
        """The §12 rule `config` holds a bundle to holds a client built from a bare URL."""
        for bad, hosts in (("http://example.invalid", (LOOPBACK,)), ("http://localhost:8448", (LOOPBACK,)),
                           ("http://127.0.0.1:8448", ()), ("http://192.0.2.10", (LOOPBACK,))):
            with self.subTest(bad=bad, hosts=hosts), self.assertRaisesRegex(ValueError, "plain_http_hosts"):
                matrix.Client(bad, "t0ken", plain_http_hosts=hosts)
        with self.assertRaisesRegex(ValueError, "plain_http_hosts"):
            matrix.Client("http://127.0.0.1:8448", "t0ken")
        self.assertEqual(matrix.Client("https://hs.example.com:8448", "t0ken").base_url,
                         "https://hs.example.com:8448")

    def test_token_must_be_printable(self) -> None:
        for bad in ("", "a b", "a\nb", None):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                loopback_client(self.base, bad)


class ForMemberTest(TeamRoom):
    def member(self, token_mode: int = 0o600) -> config.Member:
        bundle = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        token_path = bundle / "token"
        token_path.write_text(self.tokens[self.agent], encoding="ascii")
        token_path.chmod(token_mode)
        return config.Member(
            team="team-a", user_id=self.agent, handle=AGENT, member_type="podman", server_name=SN,
            base_url=self.base, plain_http_hosts=("127.0.0.1",), admin=self.admin, room=self.room,
            human_text=True, limits=limits.Limits(), bundle_dir=bundle, token_path=token_path)

    def test_client_for_a_member_uses_its_bundle_token(self) -> None:
        client = matrix.Client.for_member(self.member())
        self.assertEqual(client.whoami()["user_id"], self.agent)

    def test_a_loose_token_file_is_a_config_error(self) -> None:
        with self.assertRaises(config.ConfigError):
            matrix.Client.for_member(self.member(0o644))
        self.assertEqual(self.fake.http_log, [])


if __name__ == "__main__":
    unittest.main()
