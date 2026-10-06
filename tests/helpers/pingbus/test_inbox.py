"""Unit tests for helpers/pingbus/inbox.py: a team's local state (inbox, sync token,
outbox and send gate, the sync lock).

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_inbox

Spec: docs/agent-bus-protocol.md §9 (the inbox is a cache, re-validated on read; the sync
token is saved after the batch's inbox writes are durable), §10 (TIMEOUT, the send limits
across `send` processes), §12 (the state layout and the lock), §14 (exit 75). Every state
directory here is a temporary one; the clock is always injected.
"""

from __future__ import annotations

import json
import os
import pathlib
import stat
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest import mock

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.pingbus import inbox, limits, protocol

SN = "team-a.agent-bus.internal"
ME = f"@myrepo.1+workstation.podman:{SN}"
ORCH = f"@orch.1+workstation.podman:{SN}"
PEER = f"@other.1+workstation.podman:{SN}"
HUMAN = f"@alice:{SN}"
REF = "commit:example-org/myrepo@" + "0" * 40
T0 = 1_791_234_567_890
LIM = limits.Limits()

CTX = protocol.Context(
    server_name=SN,
    humans=frozenset({HUMAN}),
    roles={ME: "worker", ORCH: "orchestrator", PEER: "worker"},
    repos={"example-org/myrepo": ("main",)},
    path_prefixes=("docs/",),
)


def event_id(n: int) -> str:
    return "$" + f"{n:043d}"


def ping_event(n: int, verb: str = "review", sender: str = ORCH, ts: int = T0, **kw) -> dict:
    ref = kw.pop("ref", REF)
    return {
        "type": "m.room.message",
        "event_id": event_id(n),
        "sender": sender,
        "origin_server_ts": ts,
        "content": protocol.build_ping(verb, kw.pop("to", [ME]), ref=ref, re=kw.pop("re", None)),
    }


def human_event(n: int, text: str = "please look", ts: int = T0) -> dict:
    return {
        "type": "m.room.message",
        "event_id": event_id(n),
        "sender": HUMAN,
        "origin_server_ts": ts,
        "content": {"msgtype": "m.text", "body": text, "m.mentions": {"user_ids": [ME]}},
    }


class StateCase(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        (self.root / "team-a").mkdir()
        (self.root / "team-b").mkdir()
        self.state = inbox.TeamState(self.root / "team-a" / "state")

    def pending(self, state: inbox.TeamState | None = None, human_text: bool = True) -> inbox.Pending:
        return (state or self.state).pending(CTX, ME, human_text=human_text)


class LayoutTest(StateCase):
    def test_names_match_spec_section_12(self):
        self.assertEqual(inbox.INBOX_DIR, "inbox")
        self.assertEqual(inbox.CONSUMED_DIR, "consumed")
        self.assertEqual(inbox.OUTBOX_FILE, "outbox.json")
        self.assertEqual(inbox.SYNC_FILE, "sync.json")
        self.assertEqual(inbox.LOCK_FILE, "lock")
        self.assertEqual(inbox.LOCK_KINDS, ("watch", "wait", "recv"))

    def test_for_member_uses_the_bundles_state_dir(self):
        member = mock.Mock(state_dir=self.root / "team-b" / "state")
        self.assertEqual(inbox.TeamState.for_member(member).path, self.root / "team-b" / "state")

    def test_a_missing_bundle_dir_is_not_created(self):
        state = inbox.TeamState(self.root / "no-such-team" / "state")
        with self.assertRaises(FileNotFoundError):
            state.commit_batch([], "s1")

    def test_reads_create_nothing(self):
        self.assertEqual(self.pending().items, ())
        self.assertIsNone(self.state.sync_token())
        self.assertIsNone(inbox.probe_lock(self.state))
        self.assertFalse(self.state.path.exists())

    def test_writes_create_private_dirs(self):
        self.state.commit_batch([ping_event(1)], "s1")
        for path in (self.state.path, self.state.path / "inbox", self.state.path / "consumed"):
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o700, path)
        for path in (self.state.path / "sync.json", self.state.path / "inbox" / f"{event_id(1)}.json"):
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600, path)

    def test_symlinked_state_dir_refused(self):
        real = self.root / "elsewhere"
        real.mkdir()
        os.symlink(real, self.state.path)
        with self.assertRaises(inbox.StateError):
            self.state.commit_batch([], "s1")

    def test_per_team_dirs_are_independent(self):
        other = inbox.TeamState(self.root / "team-b" / "state")
        self.state.commit_batch([ping_event(1)], "a1")
        other.commit_batch([ping_event(2)], "b1")
        self.assertEqual([i.event_id for i in self.pending().items], [event_id(1)])
        self.assertEqual([i.event_id for i in self.pending(other).items], [event_id(2)])
        self.assertEqual(self.state.sync_token(), "a1")
        self.assertEqual(other.sync_token(), "b1")


class InboxWriteTest(StateCase):
    def test_event_id_validated_before_it_names_a_file(self):
        for bad in ("../../escape", "$short", "$" + "A" * 42 + "/", None, 7, "$" + "A" * 43 + ".."):
            event = ping_event(1)
            event["event_id"] = bad
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                self.state.commit_batch([event], "s1")
        self.assertFalse((self.root / "escape").exists())
        self.assertIsNone(self.state.sync_token())

    def test_a_bad_event_in_a_batch_writes_nothing(self):
        bad = ping_event(2)
        bad["event_id"] = "nope"
        with self.assertRaises(ValueError):
            self.state.commit_batch([ping_event(1), bad], "s1")
        self.assertEqual(self.pending().items, ())

    def test_dedupe_against_inbox_and_consumed(self):
        self.assertEqual(self.state.commit_batch([ping_event(1), ping_event(1)], "s1"), 1)
        self.assertEqual(self.state.commit_batch([ping_event(1)], "s2"), 0)
        self.assertTrue(self.state.consume(event_id(1)))
        self.assertEqual(self.state.commit_batch([ping_event(1)], "s3"), 0)
        self.assertEqual(self.pending().items, ())
        self.assertEqual(self.state.known_event_ids(), frozenset({event_id(1)}))

    def test_known_event_ids_feed_the_validators_seen(self):
        self.state.commit_batch([ping_event(1)], "s1")
        outcome = protocol.validate_event(ping_event(1), CTX, ME, seen=self.state.known_event_ids())
        self.assertEqual((outcome.kind, outcome.reason), (protocol.IGNORE, "seen"))

    def test_stored_copy_is_a_projection_of_the_event(self):
        event = ping_event(1)
        event["unsigned"] = {"age": 5, "big": "x" * 100}
        self.state.commit_batch([event], "s1")
        stored = json.loads((self.state.path / "inbox" / f"{event_id(1)}.json").read_text())
        self.assertEqual(set(stored), {"type", "event_id", "sender", "origin_server_ts", "content"})

    def test_write_is_atomic_no_temporary_left_behind(self):
        self.state.commit_batch([ping_event(1), human_event(2)], "s1")
        self.assertEqual(
            sorted(p.name for p in (self.state.path / "inbox").iterdir()),
            [f"{event_id(1)}.json", f"{event_id(2)}.json"],
        )
        self.assertEqual(sorted(p.name for p in self.state.path.iterdir()),
                         ["consumed", "inbox", "sync.json"])

    def test_failed_write_removes_its_temporary_and_keeps_the_old_file(self):
        self.state.commit_batch([], "s1")
        with mock.patch.object(inbox.os, "fsync", side_effect=OSError(5, "EIO")), \
                self.assertRaises(OSError):
            self.state.commit_batch([], "s2")
        self.assertEqual(self.state.sync_token(), "s1")
        self.assertEqual(sorted(p.name for p in self.state.path.iterdir()),
                         ["consumed", "inbox", "sync.json"])


class SyncTokenTest(StateCase):
    def test_saved_after_the_inbox_is_durable(self):
        calls: list[tuple[str, str]] = []
        real_write, real_fsync_dir = inbox._write_atomic, inbox._fsync_dir

        def write(path, data):
            calls.append(("write", pathlib.Path(path).parent.name + "/" + pathlib.Path(path).name))
            real_write(path, data)

        def fsync_dir(path):
            calls.append(("fsync_dir", pathlib.Path(path).name))
            real_fsync_dir(path)

        with mock.patch.object(inbox, "_write_atomic", write), \
                mock.patch.object(inbox, "_fsync_dir", fsync_dir):
            self.state.commit_batch([ping_event(1), ping_event(2)], "s1")
        self.assertEqual(calls, [
            ("write", f"inbox/{event_id(1)}.json"),
            ("write", f"inbox/{event_id(2)}.json"),
            ("fsync_dir", "inbox"),
            ("write", "state/sync.json"),
            ("fsync_dir", "state"),
        ])

    def test_not_saved_when_an_inbox_write_fails(self):
        self.state.commit_batch([], "s1")
        real_write = inbox._write_atomic

        def write(path, data):
            if pathlib.Path(path).parent.name == "inbox":
                raise OSError(28, "ENOSPC")
            real_write(path, data)

        with mock.patch.object(inbox, "_write_atomic", write), self.assertRaises(OSError):
            self.state.commit_batch([ping_event(1)], "s2")
        self.assertEqual(self.state.sync_token(), "s1")

    def test_first_sync_records_only_the_token(self):
        self.assertEqual(self.state.commit_batch([], "s0"), 0)
        self.assertEqual(self.state.sync_token(), "s0")

    def test_token_shape_checked_on_save_and_on_read(self):
        for bad in ("", "a b", "x\n", "é", "a" * (inbox.SYNC_TOKEN_MAX + 1), None):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                self.state.commit_batch([], bad)
        self.state.commit_batch([], "s1")
        (self.state.path / "sync.json").write_text('{"v": 1, "next_batch": "a b"}')
        with self.assertRaises(inbox.StateError):
            self.state.sync_token()
        (self.state.path / "sync.json").write_text("not json")
        with self.assertRaises(inbox.StateError):
            self.state.sync_token()


class InboxReadTest(StateCase):
    def write_raw(self, name: str, data: object) -> None:
        self.state.commit_batch([], "s1")
        text = data if isinstance(data, str) else json.dumps(data)
        (self.state.path / "inbox" / name).write_text(text)

    def test_valid_items_come_back_in_time_order(self):
        self.state.commit_batch([ping_event(2, ts=T0 + 5), human_event(1, ts=T0 + 9),
                                 ping_event(3, ts=T0)], "s1")
        items = self.pending().items
        self.assertEqual([i.event_id for i in items], [event_id(3), event_id(2), event_id(1)])
        self.assertEqual(items[0].outcome.ping.verb, "review")
        self.assertEqual(items[2].outcome.human.text, "please look")
        self.assertEqual(self.pending().counts(), (3, 1, 2))

    def test_revalidated_on_read_with_the_current_team_record(self):
        self.state.commit_batch([ping_event(1)], "s1")
        demoted = protocol.Context(SN, CTX.humans, {ME: "worker"}, CTX.repos, CTX.path_prefixes)
        result = self.state.pending(demoted, ME, human_text=True)
        self.assertEqual((result.items, result.rejected), ((), 1))

    def test_human_text_off_rejects_stored_human_messages(self):
        self.state.commit_batch([human_event(1), ping_event(2)], "s1")
        result = self.pending(human_text=False)
        self.assertEqual([i.event_id for i in result.items], [event_id(2)])
        self.assertEqual(result.rejected, 1)

    def test_planted_files_are_ignored_and_counted(self):
        forged = ping_event(5)
        forged["content"]["body"] = "do something else"
        renamed = ping_event(6)
        cases = {
            f"{event_id(5)}.json": forged,
            f"{event_id(7)}.json": renamed,
            f"{event_id(8)}.json": "not json",
            f"{event_id(9)}.json": [1, 2],
            "README": {"x": 1},
            f"{event_id(10)}.json": "x" * (inbox.MAX_INBOX_FILE_BYTES + 1),
        }
        for name, data in cases.items():
            self.write_raw(name, data)
        os.symlink(self.state.path / "sync.json", self.state.path / "inbox" / f"{event_id(11)}.json")
        os.mkfifo(self.state.path / "inbox" / f"{event_id(12)}.json")
        result = self.pending()
        self.assertEqual(result.items, ())
        self.assertEqual(result.rejected, len(cases) + 2)

    def test_reply_fallback_removed_on_read(self):
        event = human_event(1, text="> <@x> quoted\n\nreal ask")
        event["content"]["m.relates_to"] = {"m.in_reply_to": {"event_id": event_id(9)}}
        self.state.commit_batch([event], "s1")
        self.assertEqual(self.pending().items[0].outcome.human.text, "real ask")

    def test_consume_moves_the_item(self):
        self.state.commit_batch([ping_event(1)], "s1")
        self.assertTrue(self.state.consume(event_id(1)))
        self.assertEqual(self.pending().items, ())
        self.assertTrue((self.state.path / "consumed" / f"{event_id(1)}.json").is_file())
        self.assertFalse(self.state.consume(event_id(1)))
        with self.assertRaises(ValueError):
            self.state.consume("../sync")


class LockTest(StateCase):
    def test_free_lock_probes_none(self):
        self.state.commit_batch([], "s1")
        self.assertIsNone(inbox.probe_lock(self.state))

    def test_holder_kind_written_and_probed(self):
        with inbox.acquire_lock(self.state, "watch"):
            self.assertEqual((self.state.path / "lock").read_text(), "watch")
            self.assertEqual(inbox.probe_lock(self.state), "watch")
        self.assertIsNone(inbox.probe_lock(self.state))

    def test_second_locker_is_busy(self):
        sleeps: list[float] = []
        with inbox.acquire_lock(self.state, "wait"):
            with self.assertRaises(inbox.Busy) as caught:
                inbox.acquire_lock(self.state, "watch", sleep=sleeps.append)
        self.assertEqual(caught.exception.EXIT_CODE, 75)
        self.assertEqual(caught.exception.holder, "wait")
        self.assertEqual(len(sleeps), inbox.LOCK_ATTEMPTS - 1)
        with inbox.acquire_lock(self.state, "watch"):
            pass

    def test_unknown_kind_refused(self):
        with self.assertRaises(ValueError):
            inbox.acquire_lock(self.state, "pid 1234")

    def test_leftover_text_without_a_holder_means_free(self):
        self.state.commit_batch([], "s1")
        (self.state.path / "lock").write_text("watch")
        self.assertIsNone(inbox.probe_lock(self.state))

    def test_held_lock_with_unexpected_text_probes_unknown(self):
        with inbox.acquire_lock(self.state, "wait"):
            (self.state.path / "lock").write_text("garbage")
            self.assertEqual(inbox.probe_lock(self.state), inbox.KIND_UNKNOWN)

    def test_symlinked_lock_refused(self):
        self.state.commit_batch([], "s1")
        os.symlink(self.state.path / "sync.json", self.state.path / "lock")
        with self.assertRaises(inbox.StateError):
            inbox.acquire_lock(self.state, "wait")
        with self.assertRaises(inbox.StateError):
            inbox.probe_lock(self.state)

    def test_another_process_holds_then_dies(self):
        self.state.commit_batch([], "s1")
        child = subprocess.Popen(
            [sys.executable, "-c",
             "import pathlib, sys\n"
             "from helpers.pingbus import inbox\n"
             "lock = inbox.acquire_lock(inbox.TeamState(pathlib.Path(sys.argv[1])), 'wait')\n"
             "print('held', flush=True)\n"
             "sys.stdin.read()\n",
             str(self.state.path)],
            cwd=REPO_ROOT, stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True,
        )
        self.addCleanup(child.wait)
        self.assertEqual(child.stdout.readline().strip(), "held")
        self.assertEqual(inbox.probe_lock(self.state), "wait")
        with self.assertRaises(inbox.Busy):
            inbox.acquire_lock(self.state, "watch", sleep=lambda _s: None)
        child.kill()
        child.wait()
        child.stdout.close()
        child.stdin.close()
        self.assertIsNone(inbox.probe_lock(self.state))
        with inbox.acquire_lock(self.state, "watch"):
            self.assertEqual(inbox.probe_lock(self.state), "watch")


class OutboxTest(StateCase):
    def send(self, n: int, verb: str = "review", to=(PEER,), at: int = T0) -> None:
        with self.state.outbox() as box:
            box.record_sent(event_id(n), verb, None if verb == "halt" else REF, list(to), at)

    def test_only_ack_expected_pings_are_tracked(self):
        with self.state.outbox() as box:
            box.record_sent(event_id(1), "done", REF, [PEER], T0)
            self.assertEqual(box.due_timeouts(T0 + 10**9, LIM), ())

    def test_timeout_per_silent_target_after_the_deadline(self):
        self.send(1, to=(PEER, ORCH))
        deadline = T0 + LIM.ack_timeout_s * 1000
        with self.state.outbox() as box:
            self.assertEqual(box.due_timeouts(deadline, LIM), ())
            due = box.due_timeouts(deadline + 1, LIM)
        self.assertEqual(due, (
            inbox.Timeout(event_id(1), ORCH, "review", REF),
            inbox.Timeout(event_id(1), PEER, "review", REF),
        ))

    def test_halt_uses_its_own_timeout(self):
        self.send(1, verb="halt")
        with self.state.outbox() as box:
            due = box.due_timeouts(T0 + LIM.halt_ack_timeout_s * 1000 + 1, LIM)
        self.assertEqual(due, (inbox.Timeout(event_id(1), PEER, "halt", None),))

    def test_ack_or_nack_from_a_target_answers(self):
        self.send(1, to=(PEER, ORCH))
        with self.state.outbox() as box:
            self.assertTrue(box.record_answer(event_id(1), PEER))
            self.assertFalse(box.record_answer(event_id(1), HUMAN))
            self.assertFalse(box.record_answer(event_id(2), ORCH))
            due = box.due_timeouts(T0 + 10**9, LIM)
        self.assertEqual(due, (inbox.Timeout(event_id(1), ORCH, "review", REF),))

    def test_each_timeout_reported_once_then_the_entry_settles(self):
        self.send(1)
        late = T0 + 10**9
        with self.state.outbox() as box:
            due = box.due_timeouts(late, LIM)
            box.mark_reported(due)
        with self.state.outbox() as box:
            self.assertEqual(box.due_timeouts(late, LIM), ())
            self.assertEqual(box.tracked(), ())

    def test_fully_answered_entry_settles(self):
        self.send(1)
        with self.state.outbox() as box:
            box.record_answer(event_id(1), PEER)
        with self.state.outbox() as box:
            self.assertEqual(box.tracked(), ())

    def test_persisted_between_processes(self):
        self.send(1)
        again = inbox.TeamState(self.state.path)
        with again.outbox() as box:
            self.assertEqual(box.tracked(), (event_id(1),))

    def test_exception_in_a_transaction_saves_nothing(self):
        with self.assertRaises(RuntimeError), self.state.outbox() as box:
            box.record_sent(event_id(1), "review", REF, [PEER], T0)
            raise RuntimeError("boom")
        with self.state.outbox() as box:
            self.assertEqual(box.tracked(), ())

    def test_record_sent_checks_its_input(self):
        with self.state.outbox() as box:
            for args in (("nope", "review", REF, [PEER], T0),
                         (event_id(1), "shout", REF, [PEER], T0),
                         (event_id(1), "review", REF, [], T0),
                         (event_id(1), "review", REF, [PEER], True)):
                with self.subTest(args=args), self.assertRaises(ValueError):
                    box.record_sent(*args)

    def test_corrupt_outbox_fails_fast(self):
        self.send(1)
        for bad in ("not json", '{"v": 1}', '{"v": 2, "pings": [], "gate": null}',
                    '{"v": 1, "pings": [{"event_id": "x"}], "gate": null}'):
            (self.state.path / "outbox.json").write_text(bad)
            with self.subTest(bad=bad), self.assertRaises(inbox.StateError), self.state.outbox():
                pass

    def test_transactions_are_serialised(self):
        entered = threading.Event()
        released = threading.Event()

        def other() -> None:
            with inbox.TeamState(self.state.path).outbox() as box:
                entered.set()
                box.record_sent(event_id(2), "review", REF, [PEER], T0)

        with self.state.outbox() as box:
            box.record_sent(event_id(1), "review", REF, [PEER], T0)
            thread = threading.Thread(target=other)
            thread.start()
            self.assertFalse(entered.wait(0.2))
            released.set()
        thread.join(5)
        self.assertTrue(entered.is_set())
        with self.state.outbox() as box:
            self.assertEqual(box.tracked(), (event_id(1), event_id(2)))


class SendGateTest(StateCase):
    """U03's token bucket and duplicate window, kept between `send` processes."""

    def admit(self, n: int, at: int, state: inbox.TeamState | None = None) -> None:
        with (state or inbox.TeamState(self.state.path)).outbox() as box:
            box.send_gate(LIM, at).admit("review", REF, None, [f"@r.{n}+h.podman:{SN}"], at)

    def test_bucket_drains_across_processes(self):
        for n in range(1, LIM.send_burst + 1):
            self.admit(n, T0)
        with self.assertRaises(limits.RateLimited) as caught:
            self.admit(99, T0)
        self.assertEqual(caught.exception.reason, "rate")
        self.admit(100, T0 + 60_000 // LIM.send_per_minute)

    def test_duplicate_window_across_processes(self):
        with inbox.TeamState(self.state.path).outbox() as box:
            box.send_gate(LIM, T0).admit("review", REF, None, [PEER], T0)
        with self.assertRaises(limits.RateLimited) as caught, \
                inbox.TeamState(self.state.path).outbox() as box:
            box.send_gate(LIM, T0 + 1000).admit("review", REF, None, [PEER], T0 + 1000)
        self.assertEqual(caught.exception.reason, "duplicate")
        with inbox.TeamState(self.state.path).outbox() as box:
            box.send_gate(LIM, T0 + 61_000).admit("review", REF, None, [PEER], T0 + 61_000)

    def test_a_refused_admit_spends_nothing(self):
        for n in range(1, LIM.send_burst + 1):
            self.admit(n, T0)
        state = json.loads((self.state.path / "outbox.json").read_text())["gate"]
        with self.assertRaises(limits.RateLimited):
            self.admit(99, T0)
        self.assertEqual(json.loads((self.state.path / "outbox.json").read_text())["gate"], state)

    def test_saved_gate_is_validated(self):
        self.admit(1, T0)
        data = json.loads((self.state.path / "outbox.json").read_text())
        data["gate"]["bucket"]["credit"] = -1
        (self.state.path / "outbox.json").write_text(json.dumps(data))
        with self.assertRaises(inbox.StateError), self.state.outbox() as box:
            box.send_gate(LIM, T0)


if __name__ == "__main__":
    unittest.main()
