"""Tests for u20_check.py, the pure logic behind acceptance.bash's M2 slice (unit U20).

Nothing here touches the network, a homeserver, podman or a real session: every function
takes text, decoded JSON or a temporary directory and returns a value or a list of problems.

Run from anywhere:
    python3 CLAUDE/Plan/00161-agent-team-bus-matrix/test_u20_check.py
"""

from __future__ import annotations

import datetime
import importlib.util
import io
import json
import pathlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

_HERE = pathlib.Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location("u20_check", _HERE / "u20_check.py")
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_HERE / 'u20_check.py'}")
uc = importlib.util.module_from_spec(_SPEC)
sys.modules["u20_check"] = uc
_SPEC.loader.exec_module(uc)

TEAM = "acceptance"
SN = "acceptance.agent-bus.internal"
A_UID = f"@ccy-a.3+acceptance.podman:{SN}"
B_UID = f"@ccy-b.4+acceptance.podman:{SN}"
H_UID = f"@tester:{SN}"
EV1 = "$" + "a" * 43
EV2 = "$" + "b" * 43
EV3 = "$" + "c" * 43
SHA = "0123456789abcdef0123456789abcdef01234567"
REF = f"path:example-org/project@{SHA}:CLAUDE/Plan/x/DESIGN.md"
# Shaped like a ccy token; never a real one.
FAKE_TOKEN = "sk-ant-oat01-" + "x" * 90


def run_main(*argv: str) -> tuple[int, str, str]:
    out, err = io.StringIO(), io.StringIO()
    with redirect_stdout(out), redirect_stderr(err):
        status = uc.main(list(argv))
    return status, out.getvalue(), err.getvalue()


def notice(total: int, humans: int, pings: int, number: int) -> str:
    return (f"agent-bus: {total} pending ({humans} from humans, {pings} pings), notice {number}. "
            "Run `pingbus recv`.")


def peer_line(content: str) -> str:
    return json.dumps({"type": "user", "message": {"role": "user", "content": content},
                       "origin": {"kind": "peer", "from": "unknown"}})


def typed_line(content: str) -> str:
    return json.dumps({"type": "user", "message": {"role": "user", "content": content}})


def ping_event(event_id: str, sender: str, verb: str, to: list[str], ts: int,
               ref: str | None = None, re_: str | None = None) -> dict:
    ping: dict = {"v": 1, "verb": verb, "to": sorted(to)}
    if ref is not None:
        ping["ref"] = ref
    if re_ is not None:
        ping["re"] = re_
    return {"type": "m.room.message", "event_id": event_id, "sender": sender, "origin_server_ts": ts,
            "content": {"msgtype": "m.notice", "body": "[agent-bus] ...", "m.mentions": {"user_ids": sorted(to)},
                        "agent_bus.ping": ping}}


def messages(*events: dict) -> dict:
    """A /messages response, newest first, as dir=b returns it."""
    return {"chunk": list(reversed(events)), "start": "s1", "end": "s0"}


class FrameAndTextTest(unittest.TestCase):
    def test_frame_is_one_stream_json_user_line(self) -> None:
        line = uc.frame("hello")
        self.assertTrue(line.endswith("\n"))
        self.assertEqual(line.count("\n"), 1)
        self.assertEqual(json.loads(line), {"type": "user", "message": {"role": "user", "content": "hello"}})

    def test_frame_refuses_empty_text(self) -> None:
        with self.assertRaises(ValueError):
            uc.frame("")

    def test_socket_orders_name_recv_and_ack_and_no_wait(self) -> None:
        text = uc.orders("socket")
        self.assertIn("pingbus recv", text)
        self.assertIn("pingbus send ack --re EVENT_ID --to SENDER", text)
        self.assertNotIn("pingbus wait", text)
        self.assertIn("READY", text)

    def test_wait_orders_start_the_waiter_in_the_background(self) -> None:
        text = uc.orders("wait")
        self.assertIn("pingbus wait", text)
        self.assertIn("run_in_background", text)
        self.assertIn("pingbus send ack --re EVENT_ID --to SENDER", text)

    def test_orders_refuse_an_unknown_kind(self) -> None:
        with self.assertRaises(ValueError):
            uc.orders("other")

    def test_send_order_is_exactly_one_pingbus_command(self) -> None:
        text = uc.send_order("review", REF, "ccy-b.4+acceptance.podman")
        self.assertIn(f"`pingbus send review {REF} --to ccy-b.4+acceptance.podman`", text)

    def test_send_order_refuses_a_bad_handle_or_ref(self) -> None:
        with self.assertRaises(ValueError):
            uc.send_order("review", REF, "not a handle")
        with self.assertRaises(ValueError):
            uc.send_order("review", "path:nope", "ccy-b.4+acceptance.podman")

    def test_human_request_asks_for_an_ack_to_the_human(self) -> None:
        self.assertIn("pingbus send ack --re EVENT_ID --to tester", uc.human_request("tester"))

    def test_cli_frames(self) -> None:
        status, out, _ = run_main("frame-orders", "wait")
        self.assertEqual(status, 0)
        self.assertIn("pingbus wait", json.loads(out)["message"]["content"])
        status, out, _ = run_main("frame-send", "review", REF, "ccy-b.4+acceptance.podman")
        self.assertEqual(status, 0)
        self.assertIn(REF, json.loads(out)["message"]["content"])
        status, out, _ = run_main("human-request", "tester")
        self.assertEqual((status, out), (0, uc.human_request("tester") + "\n"))


class TurnsTest(unittest.TestCase):
    def test_counts_result_lines_and_skips_ccy_banners(self) -> None:
        stream = "\n".join([
            "════ Executing Claude Code with arguments:",
            "\x1b]0;CCY: project\x07",
            json.dumps({"type": "system", "subtype": "init"}),
            json.dumps({"type": "result", "subtype": "success"}),
            "{not json",
            json.dumps({"type": "assistant"}),
            json.dumps({"type": "result", "subtype": "success"}),
        ])
        self.assertEqual(uc.turns(stream), 2)

    def test_cli(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "session.out"
            path.write_text(json.dumps({"type": "result"}) + "\n", encoding="utf-8")
            self.assertEqual(run_main("turns", str(path))[:2], (0, "1\n"))


class StatusTest(unittest.TestCase):
    STATUS = "\t".join(["TEAM", TEAM, "ccy-a.3+acceptance.podman", "trust=trusted", "wake=watcher",
                        "pending=1", "humans=0", "pings=1", "overdue=0", "dropped=0", "unexpected=0"]) + "\n" + \
        "\t".join(["MEMBER", TEAM, "tester", "human", "-"]) + "\n"

    def test_reads_one_field_of_the_team_line(self) -> None:
        self.assertEqual(uc.status_fields(self.STATUS, TEAM)["wake"], "watcher")
        self.assertEqual(uc.status_fields(self.STATUS, TEAM)["pending"], "1")

    def test_refuses_a_missing_team(self) -> None:
        with self.assertRaises(ValueError):
            uc.status_fields(self.STATUS, "other")

    def test_cli(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "status.out"
            path.write_text(self.STATUS, encoding="utf-8")
            self.assertEqual(run_main("status-field", str(path), TEAM, "wake")[:2], (0, "watcher\n"))
            self.assertEqual(run_main("status-field", str(path), TEAM, "absent")[0], 1)


class NoticesTest(unittest.TestCase):
    def state_dir(self, tmp: str, lines: list[str]) -> pathlib.Path:
        state = pathlib.Path(tmp) / ".claude" / "ccy"
        project = state / "projects" / "-workspace"
        project.mkdir(parents=True)
        (project / "0000.jsonl").write_text("\n".join(lines) + "\n", encoding="utf-8")
        return state

    def test_only_peer_entries_count(self) -> None:
        lines = [typed_line(notice(1, 0, 1, 5)),
                 peer_line("Another Claude session sent:\n" + notice(1, 0, 1, 7) + "\nEnd."),
                 peer_line(notice(2, 1, 1, 8))]
        self.assertEqual(uc.notices(lines), [(1, 0, 1, 7), (2, 1, 1, 8)])

    def test_same_count_needs_two_distinct_numbers(self) -> None:
        two = [(1, 0, 1, 7), (1, 0, 1, 8)]
        self.assertEqual(uc.expect_same_count(two, 1, 0, 1), [])
        self.assertTrue(uc.expect_same_count([(1, 0, 1, 7)], 1, 0, 1))
        self.assertTrue(uc.expect_same_count([(1, 0, 1, 7), (1, 0, 1, 7)], 1, 0, 1))
        self.assertTrue(uc.expect_same_count([(1, 0, 1, 7), (2, 0, 2, 8)], 1, 0, 1))

    def test_no_notices_and_no_human_notices(self) -> None:
        self.assertEqual(uc.expect_no_notices([], humans_only=False), [])
        self.assertTrue(uc.expect_no_notices([(1, 0, 1, 7)], humans_only=False))
        self.assertEqual(uc.expect_no_notices([(1, 0, 1, 7)], humans_only=True), [])
        self.assertTrue(uc.expect_no_notices([(1, 1, 0, 9)], humans_only=True))

    def test_a_human_notice(self) -> None:
        self.assertEqual(uc.expect_human_notice([(1, 0, 1, 7), (1, 1, 0, 9)]), [])
        self.assertTrue(uc.expect_human_notice([(1, 0, 1, 7)]))

    def test_cli_reads_every_transcript_of_the_checkout(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            state = self.state_dir(tmp, [peer_line(notice(1, 0, 1, 7)), peer_line(notice(1, 0, 1, 8))])
            self.assertEqual(run_main("notices", str(state))[:2], (0, "1 0 1 7\n1 0 1 8\n"))
            self.assertEqual(run_main("expect-same-count", str(state), "1", "0", "1")[0], 0)
            self.assertEqual(run_main("expect-no-notices", str(state), "any")[0], 1)
            self.assertEqual(run_main("expect-no-notices", str(state), "humans")[0], 0)
            self.assertEqual(run_main("expect-human-notice", str(state))[0], 1)

    def test_cli_a_missing_transcript_reads_as_no_notices_yet(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(run_main("notices", tmp)[:2], (0, ""))


class RoomTest(unittest.TestCase):
    REVIEW = ping_event(EV1, A_UID, "review", [B_UID], 1000, ref=REF)
    ACK = ping_event(EV2, B_UID, "ack", [A_UID], 9000, re_=EV1)

    def test_room_pings_skip_events_without_a_ping(self) -> None:
        human = {"type": "m.room.message", "event_id": EV3, "sender": H_UID, "origin_server_ts": 5,
                 "content": {"msgtype": "m.text", "body": "hi"}}
        state = {"type": "agent_bus.status", "event_id": EV3, "sender": A_UID, "content": {}}
        pings = uc.room_pings(messages(self.REVIEW, human, state, self.ACK))
        self.assertEqual([p["event_id"] for p in pings], [EV1, EV2])

    def test_find_ping_matches_every_field(self) -> None:
        pings = uc.room_pings(messages(self.REVIEW, self.ACK))
        self.assertEqual(uc.find_ping(pings, A_UID, "review", B_UID, REF, None)["event_id"], EV1)
        self.assertEqual(uc.find_ping(pings, B_UID, "ack", A_UID, None, EV1)["event_id"], EV2)
        self.assertIsNone(uc.find_ping(pings, B_UID, "ack", A_UID, None, EV3))
        self.assertIsNone(uc.find_ping(pings, A_UID, "review", B_UID, None, None))
        self.assertIsNone(uc.find_ping(pings, B_UID, "review", B_UID, REF, None))

    def test_find_ping_takes_the_earliest_match(self) -> None:
        again = ping_event(EV3, A_UID, "review", [B_UID], 2000, ref=REF)
        pings = uc.room_pings(messages(self.REVIEW, again))
        self.assertEqual(uc.find_ping(pings, A_UID, "review", B_UID, REF, None)["event_id"], EV1)

    def test_replies_only_from_the_expected_sender(self) -> None:
        pings = uc.room_pings(messages(self.REVIEW, self.ACK))
        self.assertEqual(uc.expect_replies(pings, EV1, B_UID), [])
        self.assertTrue(uc.expect_replies(pings, EV1, A_UID))
        self.assertTrue(uc.expect_replies(pings, EV3, B_UID))
        other = ping_event(EV3, A_UID, "ack", [B_UID], 9500, re_=EV1)
        self.assertTrue(uc.expect_replies(uc.room_pings(messages(self.REVIEW, self.ACK, other)), EV1, B_UID))

    def test_room_pings_refuse_a_response_without_a_chunk(self) -> None:
        with self.assertRaises(ValueError):
            uc.room_pings({"errcode": "M_FORBIDDEN"})

    def test_cli_find_ping_found_and_not_yet(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "messages.json"
            path.write_text(json.dumps(messages(self.REVIEW, self.ACK)), encoding="utf-8")
            self.assertEqual(run_main("find-ping", str(path), B_UID, "ack", A_UID, "-", EV1)[:2],
                             (0, f"{EV2}\t9000\n"))
            self.assertEqual(run_main("find-ping", str(path), B_UID, "ack", A_UID, "-", EV3)[0], uc.EXIT_NOT_YET)
            self.assertEqual(run_main("expect-replies", str(path), EV1, B_UID)[0], 0)


class WindowTest(unittest.TestCase):
    def test_inside_the_lower_bound_passes(self) -> None:
        self.assertEqual(uc.expect_within_window(1000, 1000 + uc.DEDUPE_LOWER_MS), [])

    def test_outside_fails_with_the_gap(self) -> None:
        problems = uc.expect_within_window(1000, 1001 + uc.DEDUPE_LOWER_MS)
        self.assertEqual(len(problems), 1)
        self.assertIn("20.0 s", problems[0])

    def test_out_of_order_is_malformed(self) -> None:
        self.assertTrue(uc.expect_within_window(2000, 1000))


class CcyTokenTest(unittest.TestCase):
    def tree(self, tmp: str, expiry: str, value: str = FAKE_TOKEN) -> tuple[pathlib.Path, pathlib.Path]:
        root = pathlib.Path(tmp)
        checkout, home = root / "checkout", root / "home"
        (checkout / ".claude" / "ccy").mkdir(parents=True)
        (checkout / ".claude" / "ccy" / ".last-launch.conf").write_text('LAST_TOKEN="team1"\n', encoding="utf-8")
        tokens = home / ".claude-tokens" / "ccy" / "tokens"
        tokens.mkdir(parents=True)
        (tokens / f"team1.{expiry}.token").write_text(value + "\n", encoding="utf-8")
        return checkout, home

    def test_name_of_a_usable_token_never_its_value(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            checkout, home = self.tree(tmp, "2099-01-01")
            token = uc.ccy_token(checkout, home, datetime.date(2026, 10, 7))
            self.assertEqual(token.name, "team1")
            self.assertNotIn(FAKE_TOKEN, repr(token))

    def test_expired_token_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            checkout, home = self.tree(tmp, "2026-10-07")
            with self.assertRaises(uc.up.tp.ProbeError):
                uc.ccy_token(checkout, home, datetime.date(2026, 10, 7))

    def test_scrub_replaces_and_names_only_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            evidence = pathlib.Path(tmp) / "u20"
            (evidence / "a").mkdir(parents=True)
            (evidence / "a" / "transcript.jsonl").write_text(f'{{"x":"{FAKE_TOKEN}"}}\n', encoding="utf-8")
            (evidence / "a" / "session.out").write_text("clean\n", encoding="utf-8")
            held = uc.scrub(evidence, FAKE_TOKEN)
            self.assertEqual(held, ["a/transcript.jsonl"])
            self.assertNotIn(FAKE_TOKEN, (evidence / "a" / "transcript.jsonl").read_text(encoding="utf-8"))


class UsageTest(unittest.TestCase):
    def test_unknown_command_and_wrong_arity_are_usage(self) -> None:
        self.assertEqual(run_main("nope")[0], 64)
        self.assertEqual(run_main("turns")[0], 64)


if __name__ == "__main__":
    unittest.main()
