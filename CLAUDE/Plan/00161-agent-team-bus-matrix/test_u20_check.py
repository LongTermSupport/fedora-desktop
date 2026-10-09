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
import os
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
A_HANDLE = "example-repo.acca+local.podman"
B_HANDLE = "example-repo.accb+local.podman"
C_HANDLE = "example-repo.accc+local.podman"
A_UID = f"@{A_HANDLE}:{SN}"
B_UID = f"@{B_HANDLE}:{SN}"
H_UID = f"@tester:{SN}"
EV1 = "$" + "a" * 43
EV2 = "$" + "b" * 43
EV3 = "$" + "c" * 43
SHA = "0123456789abcdef0123456789abcdef01234567"
REF = f"path:example-org/project@{SHA}:CLAUDE/Plan/x/DESIGN.md"
SESSION = "0b6f3c2e-1d2a-4f00-9a7e-123456789abc"
# Shaped like a ccy token; never a real one.
FAKE_TOKEN = "sk-ant-oat01-" + "x" * 90


def run_main(*argv: str, stdin: str = "") -> tuple[int, str, str]:
    out, err = io.StringIO(), io.StringIO()
    saved = sys.stdin
    sys.stdin = io.StringIO(stdin)
    try:
        with redirect_stdout(out), redirect_stderr(err):
            status = uc.main(list(argv))
    finally:
        sys.stdin = saved
    return status, out.getvalue(), err.getvalue()


def notice(total: int, humans: int, pings: int, number: int) -> str:
    return (f"agent-bus: {total} pending ({humans} from humans, {pings} pings), notice {number}. "
            "Run `pingbus recv`.")


def peer_line(content: str) -> str:
    return json.dumps({"type": "user", "message": {"role": "user", "content": content},
                       "origin": {"kind": "peer", "from": "unknown"}})


def absorbed_line(prompt: str, *, kind: str = "peer") -> str:
    """A message queued while the session was mid-turn, folded into that turn."""
    return json.dumps({"type": "attachment", "attachment": {
        "type": "queued_command", "prompt": prompt, "commandMode": "prompt",
        "origin": {"kind": kind, "from": "unknown"}, "isMeta": True}})


def typed_line(content: str) -> str:
    return json.dumps({"type": "user", "message": {"role": "user", "content": content}})


def bash_use(command: str) -> str:
    return json.dumps({"type": "assistant", "message": {"role": "assistant", "content": [
        {"type": "text", "text": "running it"},
        {"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": {"command": command}}]}})


def tool_result(text: str, *, as_list: bool = False, is_error: bool = False) -> str:
    content: object = [{"type": "text", "text": text}] if as_list else text
    return json.dumps({"type": "user", "message": {"role": "user", "content": [
        {"type": "tool_result", "tool_use_id": "toolu_1", "content": content, "is_error": is_error}]}})


def init_line(plugins: list, skills: list | None = None) -> str:
    return json.dumps({"type": "system", "subtype": "init", "session_id": SESSION, "cwd": "/workspace",
                       "plugins": plugins, "skills": skills or [], "slash_commands": ["compact"]})


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


def write(tmp: str, name: str, text: str) -> pathlib.Path:
    path = pathlib.Path(tmp) / name
    path.write_text(text, encoding="utf-8")
    return path


class OrdersTest(unittest.TestCase):
    def test_frame_is_one_stream_json_user_line(self) -> None:
        line = uc.frame("hello")
        self.assertTrue(line.endswith("\n"))
        self.assertEqual(line.count("\n"), 1)
        self.assertEqual(json.loads(line), {"type": "user", "message": {"role": "user", "content": "hello"}})

    def test_frame_refuses_empty_text(self) -> None:
        with self.assertRaises(ValueError):
            uc.frame("")

    def test_every_order_forbids_edits_and_names_the_stop_line(self) -> None:
        for kind in uc.ORDER_KINDS:
            with self.subTest(kind=kind):
                text = uc.orders(kind)
                self.assertIn("never edit", text)
                self.assertIn("commit", text)
                self.assertIn(uc.STOP_LINE, text)
                self.assertEqual(uc.STOP_LINE, "STOPPING BECAUSE: waiting on the agent team bus")

    def test_bus_orders_name_recv_and_a_quoted_ack(self) -> None:
        text = uc.orders("bus")
        self.assertIn("pingbus recv", text)
        self.assertIn("pingbus send ack --re 'EVENT_ID' --to SENDER", text)
        self.assertIn("pingbus wait", text)
        self.assertIn("run_in_background", text)
        self.assertIn("READY", text)

    def test_idle_orders_give_nothing_to_do(self) -> None:
        text = uc.orders("idle")
        self.assertIn("nothing to do", text)
        self.assertNotIn("READY", text)

    def test_history_and_plain_orders_run_one_command(self) -> None:
        self.assertIn("`pingbus history`", uc.orders("history"))
        self.assertIn("`pingbus status`", uc.orders("plain"))

    def test_orders_refuse_an_unknown_kind(self) -> None:
        with self.assertRaises(ValueError):
            uc.orders("socket")

    def test_send_order_is_exactly_one_pingbus_command(self) -> None:
        text = uc.send_order("review", REF, B_HANDLE)
        self.assertIn(f"`pingbus send review {REF} --to {B_HANDLE}`", text)
        self.assertIn(uc.STOP_LINE, text)

    def test_send_order_refuses_a_bad_handle_or_ref(self) -> None:
        with self.assertRaises(ValueError):
            uc.send_order("review", REF, "not a handle")
        with self.assertRaises(ValueError):
            uc.send_order("review", "path:nope", B_HANDLE)

    def test_human_request_asks_for_a_quoted_ack_to_the_human(self) -> None:
        self.assertIn("pingbus send ack --re 'EVENT_ID' --to tester", uc.human_request("tester"))

    def test_cli_frames(self) -> None:
        status, out, _ = run_main("frame-orders", "idle")
        self.assertEqual(status, 0)
        self.assertIn("nothing to do", json.loads(out)["message"]["content"])
        status, out, _ = run_main("frame-send", "review", REF, B_HANDLE)
        self.assertEqual(status, 0)
        self.assertIn(REF, json.loads(out)["message"]["content"])
        status, out, _ = run_main("human-request", "tester")
        self.assertEqual((status, out), (0, uc.human_request("tester") + "\n"))


class StreamTest(unittest.TestCase):
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

    def test_session_id_from_the_init_line(self) -> None:
        stream = "banner\n" + init_line([{"name": "pingbus", "path": "/opt/x"}]) + "\n"
        self.assertEqual(uc.session_id(stream), SESSION)
        self.assertIsNone(uc.session_id("banner only\n"))

    def test_session_id_refuses_a_malformed_id(self) -> None:
        with self.assertRaises(ValueError):
            uc.session_id(json.dumps({"type": "system", "subtype": "init", "session_id": "../x"}))

    def test_plugin_present_and_absent(self) -> None:
        seated = init_line([{"name": "pingbus", "path": "/opt/claude-yolo/optional/agent-bus/plugin/pingbus"}],
                           ["pingbus:pingbus"])
        plain = init_line([], ["other"])
        self.assertEqual(uc.expect_plugin(seated, present=True), [])
        self.assertTrue(uc.expect_plugin(seated, present=False))
        self.assertEqual(uc.expect_plugin(plain, present=False), [])
        self.assertTrue(uc.expect_plugin(plain, present=True))
        self.assertTrue(uc.expect_plugin("no init line\n", present=False))

    def test_cli(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = write(tmp, "session.out", init_line([]) + "\n" + json.dumps({"type": "result"}) + "\n")
            self.assertEqual(run_main("turns", str(path))[:2], (0, "1\n"))
            self.assertEqual(run_main("session-id", str(path))[:2], (0, SESSION + "\n"))
            self.assertEqual(run_main("expect-plugin", str(path), "absent")[0], 0)
            self.assertEqual(run_main("expect-plugin", str(path), "present")[0], 1)
            empty = write(tmp, "empty.out", "banner\n")
            self.assertEqual(run_main("session-id", str(empty))[0], uc.EXIT_NOT_YET)


class StatusTest(unittest.TestCase):
    STATUS = "\t".join(["TEAM", TEAM, A_HANDLE, "trust=trusted", "wake=watcher",
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
            path = write(tmp, "status.out", self.STATUS)
            self.assertEqual(run_main("status-field", str(path), TEAM, "wake")[:2], (0, "watcher\n"))
            self.assertEqual(run_main("status-field", str(path), TEAM, "absent")[0], 1)


class TranscriptTest(unittest.TestCase):
    def test_finds_the_session_transcript_and_only_it(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            ccy = pathlib.Path(tmp)
            self.assertIsNone(uc.find_transcript(ccy, SESSION))
            project = ccy / "projects" / "-workspace"
            project.mkdir(parents=True)
            (project / "11111111-2222-4333-8444-555555555555.jsonl").write_text("{}\n", encoding="utf-8")
            self.assertIsNone(uc.find_transcript(ccy, SESSION))
            (project / f"{SESSION}.jsonl").write_text("{}\n", encoding="utf-8")
            self.assertEqual(uc.find_transcript(ccy, SESSION), project / f"{SESSION}.jsonl")
            other = ccy / "projects" / "-elsewhere"
            other.mkdir()
            (other / f"{SESSION}.jsonl").write_text("{}\n", encoding="utf-8")
            with self.assertRaises(ValueError):
                uc.find_transcript(ccy, SESSION)

    def test_refuses_a_session_id_that_is_not_one(self) -> None:
        with self.assertRaises(ValueError):
            uc.find_transcript(pathlib.Path("/nonexistent"), "../../x")

    def test_cli_not_yet_then_found(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(run_main("transcript", tmp, SESSION)[0], uc.EXIT_NOT_YET)
            project = pathlib.Path(tmp) / "projects" / "-workspace"
            project.mkdir(parents=True)
            (project / f"{SESSION}.jsonl").write_text("{}\n", encoding="utf-8")
            self.assertEqual(run_main("transcript", tmp, SESSION)[:2], (0, f"{project / SESSION}.jsonl\n"))


class NoticesTest(unittest.TestCase):
    def test_only_peer_entries_count(self) -> None:
        lines = [typed_line(notice(1, 0, 1, 5)),
                 peer_line("Another Claude session sent:\n" + notice(1, 0, 1, 7) + "\nEnd."),
                 peer_line(notice(2, 1, 1, 8))]
        self.assertEqual(uc.notices(lines), [(1, 0, 1, 7), (2, 1, 1, 8)])

    def test_a_notice_absorbed_mid_turn_counts(self) -> None:
        lines = [peer_line(notice(1, 0, 1, 7)), bash_use("pingbus recv"),
                 absorbed_line(notice(1, 0, 1, 8)), absorbed_line(notice(1, 0, 1, 9), kind="user"),
                 json.dumps({"type": "attachment", "attachment": {"type": "queued_command"}})]
        self.assertEqual(uc.notices(lines), [(1, 0, 1, 7), (1, 0, 1, 8)])
        self.assertEqual(uc.expect_same_count(uc.notices(lines), 1, 0, 1), [])

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

    def test_the_notice_precedes_the_turn_that_ran_recv(self) -> None:
        woken = [typed_line("orders"), bash_use("echo READY"), peer_line(notice(1, 0, 1, 1)),
                 bash_use("pingbus recv"), tool_result("PING\t1\t...")]
        self.assertEqual(uc.expect_notice_before_recv(woken), [])
        unprompted = [typed_line("orders"), bash_use("pingbus recv"), peer_line(notice(1, 0, 1, 1))]
        self.assertTrue(uc.expect_notice_before_recv(unprompted))
        self.assertTrue(uc.expect_notice_before_recv([peer_line(notice(1, 0, 1, 1)), bash_use("pingbus status")]))
        self.assertTrue(uc.expect_notice_before_recv([bash_use("pingbus recv")]))

    def test_the_no_waker_block_is_found_in_any_entry(self) -> None:
        block = ("Stop hook feedback:\nagent-bus: 1 of 1 active teams have no watcher or waiter, so "
                 "nothing will wake this session. Run `pingbus wait` with run_in_background.")
        self.assertEqual(uc.expect_no_waker_block([typed_line("x"), typed_line(block)]), [])
        self.assertEqual(uc.expect_no_waker_block([json.dumps({"type": "system", "content": block})]), [])
        self.assertTrue(uc.expect_no_waker_block([typed_line(notice(1, 0, 1, 1))]))

    def test_cli_reads_one_transcript(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = write(tmp, "t.jsonl", "\n".join([peer_line(notice(1, 0, 1, 7)), bash_use("pingbus recv"),
                                                    peer_line(notice(1, 0, 1, 8))]) + "\n")
            self.assertEqual(run_main("notices", str(path))[:2], (0, "1 0 1 7\n1 0 1 8\n"))
            self.assertEqual(run_main("expect-same-count", str(path), "1", "0", "1")[0], 0)
            self.assertEqual(run_main("expect-no-notices", str(path), "any")[0], 1)
            self.assertEqual(run_main("expect-no-notices", str(path), "humans")[0], 0)
            self.assertEqual(run_main("expect-human-notice", str(path))[0], 1)
            self.assertEqual(run_main("expect-notice-before-recv", str(path))[0], 0)
            self.assertEqual(run_main("expect-no-waker-block", str(path))[0], 1)


class ToolResultsTest(unittest.TestCase):
    HISTORY_IN = "\t".join(["HISTORY", "1", "in", "1000", "PING", TEAM, EV1, A_HANDLE, "review", REF, "-"])
    HISTORY_OUT = "\t".join(["HISTORY", "1", "out", "2000", "PING", TEAM, EV2, B_HANDLE, "ack", "-", EV1])

    def test_history_lines_from_string_and_list_results(self) -> None:
        lines = [bash_use("pingbus history"), tool_result(self.HISTORY_OUT + "\n" + self.HISTORY_IN),
                 tool_result("noise\n" + self.HISTORY_IN, as_list=True)]
        self.assertEqual(uc.expect_history(lines, "in", EV1), [])
        self.assertEqual(uc.expect_history(lines, "out", EV2), [])
        self.assertTrue(uc.expect_history(lines, "in", EV2))
        self.assertTrue(uc.expect_history(lines, "out", EV1))

    def test_a_history_line_in_prose_is_not_a_record(self) -> None:
        prose = json.dumps({"type": "assistant", "message": {"content": [{"type": "text", "text": self.HISTORY_IN}]}})
        self.assertTrue(uc.expect_history([prose], "in", EV1))

    def test_history_refuses_a_direction(self) -> None:
        with self.assertRaises(ValueError):
            uc.expect_history([], "sideways", EV1)

    def test_pingbus_not_found(self) -> None:
        missing = tool_result("Exit code 127\n/bin/bash: line 1: pingbus: command not found", is_error=True)
        self.assertEqual(uc.expect_not_found([bash_use("pingbus status"), missing]), [])
        self.assertTrue(uc.expect_not_found([tool_result("TEAM\tacceptance\t...")]))
        self.assertTrue(uc.expect_not_found([tool_result("git: command not found", is_error=True)]))

    def test_cli(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = write(tmp, "t.jsonl", tool_result(self.HISTORY_IN) + "\n")
            self.assertEqual(run_main("expect-history", str(path), "in", EV1)[0], 0)
            self.assertEqual(run_main("expect-history", str(path), "out", EV1)[0], 1)
            self.assertEqual(run_main("expect-not-found", str(path))[0], 1)


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
            path = write(tmp, "messages.json", json.dumps(messages(self.REVIEW, self.ACK)))
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


class SeatsTest(unittest.TestCase):
    SEATS = "\n".join([
        "\t".join(["SEAT", TEAM, "acca", "held", "-", A_HANDLE]),
        "\t".join(["SEAT", TEAM, "accb", "held", "-", B_HANDLE]),
        "\t".join(["SEAT", TEAM, "accc", "free", "-", C_HANDLE]),
        "\t".join(["SEAT", "other-team", "acca", "free", "-", "example-repo.acca+local.podman"]),
    ]) + "\n"

    def test_a_seat_handle(self) -> None:
        self.assertEqual(uc.seat_handle(self.SEATS, TEAM, "accb"), B_HANDLE)
        with self.assertRaises(ValueError):
            uc.seat_handle(self.SEATS, TEAM, "accd")

    def test_seat_states(self) -> None:
        self.assertEqual(uc.expect_seats(self.SEATS, TEAM, "held", ["acca", "accb"]), [])
        self.assertTrue(uc.expect_seats(self.SEATS, TEAM, "held", ["accc"]))
        self.assertTrue(uc.expect_seats(self.SEATS, TEAM, "free", ["accd"]))
        with self.assertRaises(ValueError):
            uc.expect_seats(self.SEATS, TEAM, "busy", ["acca"])

    def test_a_handle_carries_the_seat_the_host_and_podman(self) -> None:
        self.assertEqual(uc.expect_handle(A_HANDLE, "acca", "local"), [])
        self.assertTrue(uc.expect_handle(A_HANDLE, "accb", "local"))
        self.assertTrue(uc.expect_handle(A_HANDLE, "acca", "workstation"))
        self.assertTrue(uc.expect_handle("example-repo.acca+local.lxc", "acca", "local"))
        self.assertTrue(uc.expect_handle("not a handle", "acca", "local"))

    def test_the_host_rule_is_the_seat_commands_own(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            top = pathlib.Path(tmp)
            (top / ".claude" / "ccy").mkdir(parents=True)
            self.assertEqual(uc.expected_host(top), "local")
            (top / ".claude" / "ccy" / "ccy.env.local").write_text(
                "export HOOKS_DAEMON_HOSTNAME=role-a\n", encoding="utf-8")
            self.assertEqual(uc.expected_host(top), "role-a")

    def test_seat_permissions(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            seat = pathlib.Path(tmp) / "acca"
            (seat / "state").mkdir(parents=True)
            for path in (seat, seat / "state"):
                path.chmod(0o700)
            for name in ("member.json", "token", "state/sync.lock"):
                (seat / name).write_text("x", encoding="utf-8")
                (seat / name).chmod(0o600)
            self.assertEqual(uc.seat_permissions(seat, os.getuid()), [])
            self.assertTrue(uc.seat_permissions(seat, os.getuid() + 1))
            (seat / "token").chmod(0o640)
            self.assertTrue(uc.seat_permissions(seat, os.getuid()))
            (seat / "token").chmod(0o600)
            (seat / "state").chmod(0o750)
            self.assertTrue(uc.seat_permissions(seat, os.getuid()))
            (seat / "state").chmod(0o700)
            (seat / "link").symlink_to(seat / "token")
            self.assertTrue(uc.seat_permissions(seat, os.getuid()))

    def test_cli(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = write(tmp, "seats.txt", self.SEATS)
            self.assertEqual(run_main("seat-handle", str(path), TEAM, "acca")[:2], (0, A_HANDLE + "\n"))
            self.assertEqual(run_main("expect-seats", str(path), TEAM, "held", "acca,accb")[0], 0)
            self.assertEqual(run_main("expect-seats", str(path), TEAM, "free", "acca,accc")[0], 1)
            self.assertEqual(run_main("expect-handle", C_HANDLE, "accc", "local")[0], 0)
            self.assertEqual(run_main("expected-host", tmp)[:2], (0, "local\n"))


class MembersTest(unittest.TestCase):
    BEFORE = "\n".join([
        "HUMAN\ttester\tjoin",
        "MEMBER\tacceptance.1+acceptance.host\torchestrator\tjoin\tactive",
        "MEMBER\tacceptance.2+acceptance.host\tworker\tjoin\tactive",
    ]) + "\n"
    AFTER = BEFORE + "\n".join([
        f"MEMBER\t{A_HANDLE}\torchestrator\tjoin\tactive",
        f"MEMBER\t{B_HANDLE}\tworker\tjoin\tactive",
        f"MEMBER\t{C_HANDLE}\tworker\tjoin\tactive",
    ]) + "\n"

    def test_members_are_parsed(self) -> None:
        self.assertEqual(uc.members(self.AFTER)[A_HANDLE], ("orchestrator", "join", "active"))
        with self.assertRaises(ValueError):
            uc.members("MEMBER\tonly-two\tfields\n")

    def test_exactly_the_new_members_with_their_roles(self) -> None:
        want = {A_HANDLE: "orchestrator", B_HANDLE: "worker", C_HANDLE: "worker"}
        self.assertEqual(uc.expect_new_members(self.BEFORE, self.AFTER, want), [])
        self.assertTrue(uc.expect_new_members(self.BEFORE, self.AFTER, {**want, A_HANDLE: "worker"}))
        self.assertTrue(uc.expect_new_members(self.BEFORE, self.AFTER, {A_HANDLE: "orchestrator"}))
        extra = self.AFTER + "MEMBER\texample-repo.accd+local.podman\tworker\tjoin\tactive\n"
        self.assertTrue(uc.expect_new_members(self.BEFORE, extra, want))
        parked = self.AFTER.replace(f"{C_HANDLE}\tworker\tjoin\tactive", f"{C_HANDLE}\tworker\tjoin\tparked")
        self.assertTrue(uc.expect_new_members(self.BEFORE, parked, want))

    def test_one_member_and_the_same_handles(self) -> None:
        parked = self.AFTER.replace(f"{C_HANDLE}\tworker\tjoin\tactive", f"{C_HANDLE}\tworker\tjoin\tparked")
        self.assertEqual(uc.expect_member(parked, C_HANDLE, "worker", "parked"), [])
        self.assertTrue(uc.expect_member(parked, C_HANDLE, "worker", "active"))
        self.assertTrue(uc.expect_member(self.BEFORE, C_HANDLE, "worker", "active"))
        self.assertEqual(uc.expect_same_handles(self.AFTER, parked), [])
        self.assertTrue(uc.expect_same_handles(self.BEFORE, self.AFTER))

    def test_cli(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            before, after = write(tmp, "before", self.BEFORE), write(tmp, "after", self.AFTER)
            want = f"{A_HANDLE}=orchestrator,{B_HANDLE}=worker,{C_HANDLE}=worker"
            self.assertEqual(run_main("expect-new-members", str(before), str(after), want)[0], 0)
            self.assertEqual(run_main("expect-member", str(after), A_HANDLE, "orchestrator", "active")[0], 0)
            self.assertEqual(run_main("expect-same-handles", str(before), str(after))[0], 1)
            self.assertEqual(run_main("expect-new-members", str(before), str(after), "nonsense")[0], 1)


class ContainerTest(unittest.TestCase):
    def test_the_watcher_by_its_argv(self) -> None:
        listing = "\n".join([
            "1\t/usr/bin/tini -- /usr/local/bin/entrypoint.sh claude",
            "57\tpython3 /usr/local/bin/pingbus watch ",
            "58\t/usr/local/bin/pingbus watch",
            "60\tpython3 /usr/local/bin/pingbus wait --timeout 1500",
            "61\tbash -c grep pingbus watch",
            "62\t",
        ])
        self.assertEqual(uc.watcher_pids(listing), ["57", "58"])

    def test_containers_holding_a_seat_of_the_team(self) -> None:
        listing = "\n".join([
            "aaa\tacca@acceptance",
            "bbb\tdev1@other-team,accb@acceptance",
            "ccc\tdev1@other-team",
            "ddd\t",
            "eee\tacceptance@other",
        ])
        self.assertEqual(uc.seat_containers(listing, TEAM), ["aaa", "bbb"])

    def test_a_plain_session_carries_no_seats_label(self) -> None:
        self.assertEqual(uc.expect_no_seats_label({"ccy": "true", "ccy-project": "p"}), [])
        self.assertTrue(uc.expect_no_seats_label({"ccy": "true", "ccy-seats": "acca@acceptance"}))
        self.assertTrue(uc.expect_no_seats_label({"ccy": "true", "ccy-seats": ""}))
        self.assertTrue(uc.expect_no_seats_label({"ccy-project": "p"}))
        self.assertTrue(uc.expect_no_seats_label(None))

    def test_cli_reads_stdin(self) -> None:
        self.assertEqual(run_main("expect-no-seats-label", stdin='{"ccy": "true"}\n')[0], 0)
        self.assertEqual(run_main("expect-no-seats-label", stdin='{"ccy": "true", "ccy-seats": "x@y"}\n')[0], 1)
        self.assertEqual(run_main("watcher-pids", stdin="9\tpython3 /usr/local/bin/pingbus watch\n")[:2], (0, "9\n"))
        self.assertEqual(run_main("seat-containers", TEAM, stdin="x\taccb@acceptance\n")[:2], (0, "x\n"))


class LaunchChoicesTest(unittest.TestCase):
    def conf(self, tmp: str, version: str = "3.86.1", keys: str = "/home/u/.ssh/id_ed25519 ssh-agent",
             config: str | None = "1", omit: str = "") -> pathlib.Path:
        checkout = pathlib.Path(tmp)
        (checkout / ".claude" / "ccy").mkdir(parents=True, exist_ok=True)
        lines = ["# CCY Launch Configuration"]
        if config is not None:
            lines.append(f"SAVED_CONFIG_VERSION={config}")
        lines += [f'SAVED_CCY_VERSION="{version}"', 'LAST_TOKEN="team1"', f'LAST_SSH_KEYS="{keys}"', 'LAST_NETWORK=""']
        lines = [line for line in lines if not (omit and line.startswith(f"{omit}="))]
        (checkout / ".claude" / "ccy" / ".last-launch.conf").write_text("\n".join(lines) + "\n", encoding="utf-8")
        return checkout

    def test_the_saved_keys(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            checkout = self.conf(tmp)
            self.assertEqual(uc.launch_keys(checkout, "1"), ["/home/u/.ssh/id_ed25519", "ssh-agent"])

    def test_another_ccy_version_of_the_same_format_is_taken(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(uc.launch_keys(self.conf(tmp, version="3.80.0"), "1"),
                             ["/home/u/.ssh/id_ed25519", "ssh-agent"])

    def test_no_keys_is_no_ssh(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(uc.launch_keys(self.conf(tmp, keys=""), "1"), [])

    def test_another_or_no_format_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):
                uc.launch_keys(self.conf(tmp, config="2"), "1")
            with self.assertRaises(ValueError):
                uc.launch_keys(self.conf(tmp, config=None), "1")

    def test_a_missing_key_is_refused(self) -> None:
        for key in ("LAST_TOKEN", "LAST_SSH_KEYS", "LAST_NETWORK"):
            with tempfile.TemporaryDirectory() as tmp:
                with self.assertRaises(ValueError, msg=key):
                    uc.launch_keys(self.conf(tmp, omit=key), "1")

    def test_no_record_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(ValueError):
                uc.launch_keys(pathlib.Path(tmp), "1")

    def test_cli(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            checkout = self.conf(tmp)
            self.assertEqual(run_main("launch-keys", str(checkout), "1")[:2],
                             (0, "/home/u/.ssh/id_ed25519\nssh-agent\n"))
            status, _, err = run_main("launch-keys", str(checkout), "2")
            self.assertEqual(status, 1)
            self.assertIn("launch ccy", err)


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
