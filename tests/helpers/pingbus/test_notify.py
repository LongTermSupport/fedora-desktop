"""Unit tests for helpers/pingbus/notify.py (the inbox socket client) and `pingbus watch`.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_notify

Spec: docs/agent-bus-protocol.md §12 (the lock and its kinds), §13 (`watch`), §15 (the
socket notification template). Plan 00161's DESIGN.md section 6 (the watcher, the notice
number, liveness by `flock`, exit on socket loss) and section 12 row U12. The wire format
is the one the U01 probe measured (plan journal, 26-10-06 16:12): newline-delimited JSON,
one connection per notice, an `auth` line then one `user` line, then a half-close.

The socket is a real AF_UNIX listener in a temporary directory that records every
connection's bytes; `watch` runs against the U08 fake homeserver as in test_cli.
"""

from __future__ import annotations

import json
import os
import pathlib
import re
import socket
import sys
import tempfile
import threading
import time
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, inbox, notify, protocol
from tests.helpers.pingbus import test_cli as bus

SESSION_TOKEN = "session-token-0123456789abcdef"
NOTICE_RE = re.compile(
    r"agent-bus: (\d+) pending \((\d+) from humans, (\d+) pings\), notice (\d+)\. "
    r"Run `pingbus recv`\."
)


class FakeInbox:
    """Claude Code's inbox socket as U01 measured it: reads each connection to its
    half-close, answers nothing, closes."""

    def __init__(self, path: pathlib.Path) -> None:
        self.path = path
        self.payloads: list[bytes] = []
        self.got = threading.Condition()
        self.server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.server.bind(str(path))
        self.server.listen(8)
        self.server.settimeout(0.05)
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self._serve, daemon=True)
        self.thread.start()

    def _serve(self) -> None:
        while not self.stop.is_set():
            try:
                conn, _ = self.server.accept()
            except TimeoutError:
                continue
            with conn:
                conn.settimeout(5)
                chunks = []
                while chunk := conn.recv(4096):
                    chunks.append(chunk)
            with self.got:
                self.payloads.append(b"".join(chunks))
                self.got.notify_all()

    def close(self) -> None:
        """The session went away: the socket file goes with it."""
        self.stop.set()
        self.thread.join(5)
        self.server.close()
        self.path.unlink(missing_ok=True)

    def frames(self, index: int) -> list[dict]:
        return [json.loads(line) for line in self.payloads[index].decode("utf-8").splitlines()]

    def texts(self) -> list[str]:
        return [self.frames(i)[1]["message"]["content"] for i in range(len(self.payloads))]

    def wait_for(self, count: int, timeout: float = 10) -> list[str]:
        with self.got:
            self.got.wait_for(lambda: len(self.payloads) >= count, timeout)
        return self.texts()


class SocketCase(unittest.TestCase):
    def setUp(self) -> None:
        self.dir = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory(dir="/tmp")))
        self.path = self.dir / "inbox.sock"
        self.inbox = FakeInbox(self.path)
        self.addCleanup(self.inbox.close)


# ── the template ───────────────────────────────────────────────────────────────────────


class TemplateTest(unittest.TestCase):
    def test_the_template_is_the_spec_text(self):
        self.assertEqual(
            notify.notice_text(3, 1, 2, 7),
            "agent-bus: 3 pending (1 from humans, 2 pings), notice 7. Run `pingbus recv`.")

    def test_the_spec_carries_the_same_template(self):
        doc = (pathlib.Path(__file__).resolve().parents[3] / "docs" / "agent-bus-protocol.md").read_text(
            encoding="utf-8")
        self.assertIn(notify.NOTICE_TEMPLATE.format(total="<N>", humans="<H>", pings="<P>", number="<S>"),
                      doc)

    def test_counts_only(self):
        for bad in ((1, 1, 1, 1), (-1, 0, -1, 1), (1, 0, 1, 0), ("1", 0, 1, 1), (True, 0, 1, 1)):
            with self.assertRaises(ValueError, msg=bad):
                notify.notice_text(*bad)


# ── the wire ───────────────────────────────────────────────────────────────────────────


class WireTest(SocketCase):
    def test_one_connection_carries_auth_then_one_user_line(self):
        notify.send_notice(str(self.path), SESSION_TOKEN, "hello")
        self.inbox.wait_for(1)
        self.assertEqual(self.inbox.frames(0), [
            {"type": "auth", "token": SESSION_TOKEN},
            {"type": "user", "message": {"role": "user", "content": "hello"}},
        ])
        self.assertTrue(self.inbox.payloads[0].endswith(b"\n"))
        self.assertEqual(self.inbox.payloads[0].count(b"\n"), 2)

    def test_each_notice_is_its_own_connection(self):
        notify.send_notice(str(self.path), SESSION_TOKEN, "one")
        notify.send_notice(str(self.path), SESSION_TOKEN, "two")
        self.assertEqual(self.inbox.wait_for(2), ["one", "two"])

    def test_a_token_is_required(self):
        with self.assertRaises(ValueError):
            notify.send_notice(str(self.path), "", "hello")

    def test_a_missing_socket_is_socket_gone(self):
        self.inbox.close()
        with self.assertRaises(notify.SocketGone):
            notify.send_notice(str(self.path), SESSION_TOKEN, "hello")

    def test_a_socket_nobody_listens_on_is_socket_gone(self):
        dead = self.dir / "dead.sock"
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.bind(str(dead))
        sock.close()
        with self.assertRaises(notify.SocketGone):
            notify.send_notice(str(dead), SESSION_TOKEN, "hello")

    def test_socket_present(self):
        self.assertTrue(notify.socket_present(str(self.path)))
        plain = self.dir / "plain"
        plain.write_text("x", encoding="ascii")
        self.assertFalse(notify.socket_present(str(plain)), "a regular file is not the socket")
        self.inbox.close()
        self.assertFalse(notify.socket_present(str(self.path)))

    def test_session_socket_from_the_environment(self):
        env = {notify.SOCKET_ENV: str(self.path), notify.TOKEN_ENV: SESSION_TOKEN}
        self.assertEqual(notify.session_socket(env), (str(self.path), SESSION_TOKEN))
        self.assertIsNone(notify.session_socket({}))
        self.assertIsNone(notify.session_socket({notify.SOCKET_ENV: ""}))
        with self.assertRaises(ValueError, msg="a socket with no token cannot authenticate"):
            notify.session_socket({notify.SOCKET_ENV: str(self.path)})


# ── when to notify ─────────────────────────────────────────────────────────────────────


class NotifierTest(unittest.TestCase):
    def setUp(self) -> None:
        self.sent: list[str] = []
        self.clock = [100.0]
        self.n = notify.Notifier(self.sent.append, first_number=41, clock=lambda: self.clock[0],
                                 min_interval_s=2.0)

    def numbers(self) -> list[int]:
        return [int(NOTICE_RE.fullmatch(text)[4]) for text in self.sent]

    def test_only_a_new_item_notifies(self):
        self.n.observe((1, 0, 1), {"$a"})
        self.clock[0] += 5
        self.n.observe((1, 0, 1), {"$a"})
        self.n.observe((1, 0, 1), {"$a"})
        self.assertEqual(len(self.sent), 1)
        self.n.observe((0, 0, 0), set())
        self.clock[0] += 5
        self.n.observe((1, 1, 0), {"$b"})
        self.assertEqual(len(self.sent), 2, "1 pending, recv, 1 pending again: a second notice")
        self.clock[0] += 5
        self.n.observe((3, 1, 2), {"$b", "$c", "$d"})
        self.assertEqual(len(self.sent), 3)

    def test_nothing_pending_never_notifies(self):
        self.n.observe((0, 0, 0), set())
        self.assertEqual(self.sent, [])

    def test_the_notice_number_rises_with_every_notice(self):
        for n, total in enumerate((1, 0, 1, 0, 1)):
            self.clock[0] += 5
            self.n.observe((total, 0, total), {f"${n}"} if total else set())
        self.assertEqual(self.numbers(), [41, 42, 43])
        self.assertEqual(len(set(self.sent)), 3, "no two notices are identical")

    def test_a_new_item_within_the_interval_is_sent_once_the_interval_passes(self):
        self.n.observe((1, 0, 1), {"$a"})
        self.clock[0] += 0.5
        self.n.observe((2, 0, 2), {"$a", "$b"})
        self.assertEqual(len(self.sent), 1)
        self.clock[0] += 1.0
        self.n.observe((3, 0, 3), {"$a", "$b", "$c"})
        self.assertEqual(len(self.sent), 1)
        self.clock[0] += 1.0
        self.assertTrue(self.n.flush())
        self.assertEqual(self.sent[-1], notify.notice_text(3, 0, 3, 42), "the latest counts")

    def test_an_owed_notice_is_forgotten_once_nothing_is_pending(self):
        self.n.observe((1, 0, 1), {"$a"})
        self.n.observe((2, 0, 2), {"$a", "$b"})
        self.n.observe((0, 0, 0), set())
        self.clock[0] += 5
        self.assertFalse(self.n.flush())
        self.assertEqual(len(self.sent), 1)

    def test_an_owed_notice_carries_the_counts_of_its_send(self):
        self.n.observe((1, 0, 1), {"$a"})
        self.n.observe((3, 1, 2), {"$a", "$b", "$c"})
        self.n.observe((2, 1, 1), {"$b", "$c"})
        self.clock[0] += 5
        self.n.flush()
        self.assertEqual(self.sent[-1], notify.notice_text(2, 1, 1, 42))

    def test_a_new_item_at_the_same_count_notifies(self):
        """recv empties the inbox (1 to 0) and a new item lands before the next look: the
        total reads 1 then 1, but the item is new, so it gets a notice."""
        self.n.observe((1, 0, 1), {"$a"})
        self.clock[0] += 5
        self.n.observe((1, 0, 1), {"$b"})
        self.assertEqual(self.numbers(), [41, 42])

    def test_an_item_already_seen_never_notifies_again(self):
        self.n.observe((2, 0, 2), {"$a", "$b"})
        self.clock[0] += 5
        self.n.observe((1, 0, 1), {"$b"})
        self.assertEqual(len(self.sent), 1, "a falling set is no news")

    def test_first_number_must_be_positive(self):
        with self.assertRaises(ValueError):
            notify.Notifier(self.sent.append, first_number=0, clock=time.monotonic)


# ── `pingbus watch` ────────────────────────────────────────────────────────────────────


class WatchCase(bus.BusCase):
    def setUp(self) -> None:
        super().setUp()
        sock_dir = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory(dir="/tmp")))
        self.sock = FakeInbox(sock_dir / "inbox.sock")
        self.addCleanup(self.sock.close)
        self.env[notify.SOCKET_ENV] = str(self.sock.path)
        self.env[notify.TOKEN_ENV] = SESSION_TOKEN
        self.result: list[tuple[int, str, str]] = []
        self.watcher: threading.Thread | None = None

    def tearDown(self) -> None:
        self.end_session()
        super().tearDown()
        for text in self.outputs:
            self.assertNotIn(SESSION_TOKEN, text)

    def start_watch(self, *, ready: bool = True) -> None:
        self.watcher = threading.Thread(target=lambda: self.result.append(self.run_cli("watch")),
                                        daemon=True)
        self.watcher.start()
        if ready:
            self.until(lambda: inbox.probe_lock(self.a.state) == "watch"
                       and self.status_event() is not None, "the watcher never came up")

    def end_session(self) -> tuple[int, str, str] | None:
        self.sock.close()
        if self.watcher is not None:
            self.watcher.join(10)
            self.assertFalse(self.watcher.is_alive(), "watch did not exit on socket loss")
            self.watcher = None
        self.settle()
        return self.result[0] if self.result else None

    def until(self, check, message: str, timeout: float = 10) -> None:
        give_up = time.monotonic() + timeout
        while not check():
            if time.monotonic() > give_up or (self.watcher is not None and not self.watcher.is_alive()):
                self.fail(f"{message}: {self.result}")
            time.sleep(0.02)

    def status_event(self):
        return self.a.fake.rooms[self.a.room].state.get((protocol.EVENT_STATUS, self.a.me))

    def runtime(self, *args, **kwargs) -> cli.Runtime:
        rt = super().runtime(*args, **kwargs)
        rt.notice_interval_s = 0.0
        # The test polls the lock with `probe_lock`, whose brief shared hold a starting
        # watcher rides out with real sleeps between its tries (inbox.LOCK_ATTEMPTS).
        rt.sleep = time.sleep
        return rt


class WatchTest(WatchCase):
    def test_a_ping_wakes_the_session_with_counts_only(self):
        self.joined()
        self.start_watch()
        self.a.ping(self.a.orch, "review", ref=bus.PATH_REF)
        (text,) = self.sock.wait_for(1)
        m = NOTICE_RE.fullmatch(text)
        self.assertIsNotNone(m, text)
        self.assertEqual(m.groups()[:3], ("1", "0", "1"))
        self.assertEqual(self.sock.frames(0)[0], {"type": "auth", "token": SESSION_TOKEN})

    def test_human_text_never_reaches_the_socket(self):
        self.joined()
        self.start_watch()
        self.a.human("MARKER-human-words-never-forwarded")
        (text,) = self.sock.wait_for(1)
        self.assertEqual(NOTICE_RE.fullmatch(text).groups()[:3], ("1", "1", "0"))
        self.assertNotIn(b"MARKER", b"".join(self.sock.payloads))

    def test_same_count_after_a_recv_gets_a_new_notice_number(self):
        self.joined()
        self.start_watch()
        self.a.ping(self.a.orch, "review", ref=bus.PATH_REF)
        self.sock.wait_for(1)
        code, out, _ = self.run_cli("recv")
        self.assertEqual(code, cli.EXIT_OK)
        self.until(lambda: self.a.state.pending(
            protocol.parse_team_record(self.a.record, bus.SN_A, bus.TEAM_A).context(bus.SN_A),
            self.a.me, human_text=True).items == (), "recv did not consume")
        time.sleep(0.2)  # the watcher sees the count fall
        self.a.ping(self.a.orch, "review", ref=bus.PATH_REF)
        first, second = self.sock.wait_for(2)
        one, two = NOTICE_RE.fullmatch(first), NOTICE_RE.fullmatch(second)
        self.assertEqual(one.groups()[:3], two.groups()[:3])
        self.assertEqual(int(two[4]), int(one[4]) + 1)
        self.assertNotEqual(first, second)

    def test_drops_alone_never_notify(self):
        self.joined()
        self.start_watch()
        self.a.send(self.a.peer, {"msgtype": "m.text", "body": "agent free text"})
        self.until(lambda: self.a.drop_log() != [], "the drop was never logged")
        time.sleep(0.3)
        self.assertEqual(self.sock.payloads, [])

    def test_items_already_pending_are_notified_at_start(self):
        self.joined()
        self.a.human()
        self.start_watch()
        (text,) = self.sock.wait_for(1)
        self.assertEqual(NOTICE_RE.fullmatch(text).groups()[:3], ("1", "1", "0"))

    def test_watch_exits_on_socket_loss_and_lets_go_of_the_lock(self):
        self.joined()
        self.start_watch()
        code, out, err = self.end_session()
        self.assertEqual((code, out), (cli.EXIT_OK, ""))
        self.assertIn("socket", err)
        self.assertIsNone(inbox.probe_lock(self.a.state))

    def test_watch_publishes_listening_under_its_own_user_id(self):
        self.joined()
        self.start_watch()
        event = self.status_event()
        self.assertEqual(event.content["state"], protocol.STATUS_LISTENING)
        self.assertEqual(event.content["until"], int(self.now * 1000) + cli.WATCH_STATUS_TTL_MS)

    def test_watch_refreshes_its_status(self):
        self.joined()
        self.start_watch()
        first = self.status_event().content["until"]
        self.now += cli.WATCH_STATUS_REFRESH_MS / 1000 + 1
        self.until(lambda: self.status_event().content["until"] > first, "the status was never refreshed")

    def test_watch_takes_the_first_sync_itself(self):
        self.start_watch()
        self.a.human()
        (text,) = self.sock.wait_for(1)
        self.assertTrue(NOTICE_RE.fullmatch(text))

    def test_watch_covers_every_active_team(self):
        b = self.add_team()
        self.joined()
        self.start_watch()
        self.until(lambda: inbox.probe_lock(b.state) == "watch", "team B never held")
        b.human("for b")
        (text,) = self.sock.wait_for(1)
        self.assertEqual(NOTICE_RE.fullmatch(text).groups()[:3], ("1", "1", "0"))


class WatchRefusalTest(WatchCase):
    def test_a_held_seat_is_busy(self):
        self.joined()
        with inbox.acquire_lock(self.a.state, "wait"):
            code, out, err = self.run_cli("watch")
        self.assertEqual((code, out), (cli.EXIT_BUSY, ""))
        self.assertIn("wait", err)
        self.assertEqual(self.sock.payloads, [])

    def test_a_second_watcher_is_busy(self):
        self.joined()
        self.start_watch()
        code, _, err = self.run_cli("watch")
        self.assertEqual(code, cli.EXIT_BUSY)
        self.assertIn("watch", err)

    def test_one_busy_team_does_not_stop_the_others(self):
        b = self.add_team()
        self.joined()
        with inbox.acquire_lock(self.a.state, "wait"):
            self.start_watch(ready=False)
            self.until(lambda: inbox.probe_lock(b.state) == "watch", "team B never held")
            b.human("for b")
            (text,) = self.sock.wait_for(1)
        self.assertTrue(NOTICE_RE.fullmatch(text))

    def test_without_a_session_socket_watch_is_refused(self):
        env = {k: v for k, v in self.env.items() if k not in (notify.SOCKET_ENV, notify.TOKEN_ENV)}
        code, out, err = self.run_cli("watch", env=env)
        self.assertEqual((code, out), (cli.EXIT_CONFIG, ""))
        self.assertIn(notify.SOCKET_ENV, err)
        self.assertIsNone(inbox.probe_lock(self.a.state))

    def test_a_socket_already_gone_ends_watch_at_once(self):
        self.sock.close()
        code, out, err = self.run_cli("watch")
        self.assertEqual((code, out), (cli.EXIT_OK, ""))
        self.assertIn("socket", err)

    def test_losing_trust_ends_watch_with_exit_10(self):
        self.joined()
        self.start_watch()
        self.a.set_record(dict(self.a.record, roles={self.a.orch: "orchestrator"}))
        self.watcher.join(10)
        self.assertEqual(self.result[0][0], cli.EXIT_UNTRUSTED)

    def test_one_failing_team_does_not_stop_the_others(self):
        """Team A loses trust: it is reported and dropped, and team B still wakes the
        session. The watcher exits with A's code only once no team is left."""
        b = self.add_team()
        self.joined()
        self.start_watch()
        self.until(lambda: inbox.probe_lock(b.state) == "watch", "team B never held")
        self.a.set_record(dict(self.a.record, roles={self.a.orch: "orchestrator"}))
        self.until(lambda: inbox.probe_lock(self.a.state) is None, "team A was never let go")
        self.assertTrue(self.watcher.is_alive(), f"one team's failure ended the watcher: {self.result}")
        b.human("for b")
        (text,) = self.sock.wait_for(1)
        self.assertEqual(NOTICE_RE.fullmatch(text).groups()[:3], ("1", "1", "0"))
        self.assertEqual(inbox.probe_lock(b.state), "watch")
        b.set_record(dict(b.record, roles={b.orch: "orchestrator"}))
        self.watcher.join(10)
        self.assertFalse(self.watcher.is_alive(), "the watcher outlived its last team")
        code, _, err = self.result[0]
        self.assertEqual(code, cli.EXIT_UNTRUSTED)
        self.assertIn(f"team {bus.TEAM_A}", err)

    def test_a_stale_lock_file_is_not_a_watcher(self):
        """Liveness is the lock, never what a file says: `watch` written into an unheld lock
        file, and a PID file beside it, mean nothing."""
        self.joined()
        (self.a.state.path / inbox.LOCK_FILE).write_text("watch", encoding="ascii")
        (self.a.state.path / "watch.pid").write_text(str(os.getpid()), encoding="ascii")
        self.assertIsNone(inbox.probe_lock(self.a.state))
        self.start_watch()
        self.assertEqual(inbox.probe_lock(self.a.state), "watch")


if __name__ == "__main__":
    unittest.main()
