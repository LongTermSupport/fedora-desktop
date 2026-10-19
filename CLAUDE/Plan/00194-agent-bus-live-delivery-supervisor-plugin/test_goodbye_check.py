"""Tests for goodbye_check.py, the judgements behind acceptance.bash (Plan 00194 Task 2.0b).

Nothing here touches a homeserver, podman, tmux or a session: every command takes text or
decoded JSON (a room's /messages pages as the team's human reads them) and prints a value or
a verdict. The last test runs `acceptance.bash --fake`, which drives the same bash judgement
functions the host run uses over fixture rooms, so it runs in the CCY container.

Run from anywhere:
    python3 CLAUDE/Plan/00194-agent-bus-live-delivery-supervisor-plugin/test_goodbye_check.py
"""

from __future__ import annotations

import importlib.util
import io
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

_HERE = pathlib.Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location("goodbye_check", _HERE / "goodbye_check.py")
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_HERE / 'goodbye_check.py'}")
gc = importlib.util.module_from_spec(_SPEC)
sys.modules["goodbye_check"] = gc
_SPEC.loader.exec_module(gc)

SEAT = "@fedora-desktop.1:accept194a.bus"
HANDLE = "fedora-desktop.1"
OTHER = "@coordinator.1:accept194a.bus"
SINCE = 1_700_000_000_000


def event(sender: str, text: str, ts: int = SINCE + 1000) -> dict:
    return {
        "type": "m.room.message",
        "sender": sender,
        "origin_server_ts": ts,
        "event_id": f"$e{ts}",
        "content": {
            "msgtype": "m.notice",
            "body": f"[agent-bus] text -> @tester:accept194a.bus\n{text}",
            gc.TEXT_KEY: {"v": 1, "to": ["@tester:accept194a.bus"], "text": text},
        },
    }


def goodbye(reason: str = "exit") -> str:
    return gc.GOODBYE_TEXT.format(handle=HANDLE, reason=reason)


def disconnected() -> str:
    return gc.DISCONNECT_TEXT.format(handle=HANDLE)


def run_main(*argv: str) -> tuple[int, str, str]:
    out, err = io.StringIO(), io.StringIO()
    with redirect_stdout(out), redirect_stderr(err):
        code = gc.main(list(argv))
    return code, out.getvalue(), err.getvalue()


class HandleTest(unittest.TestCase):
    def test_the_handle_is_the_user_id_localpart(self) -> None:
        self.assertEqual(gc.handle_of(SEAT), HANDLE)

    def test_a_string_that_is_no_user_id_is_refused(self) -> None:
        for bad in ("fedora-desktop.1", "@:server", "@handle", "@handle:", ""):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                gc.handle_of(bad)


class SeatTextsTest(unittest.TestCase):
    def test_only_the_seats_own_agent_texts_since_the_start_in_time_order(self) -> None:
        newest_first = {
            "chunk": [
                event(SEAT, "second", SINCE + 2000),
                event(OTHER, "not the seat's", SINCE + 1500),
                event(SEAT, "first", SINCE + 1000),
                event(SEAT, "before the scenario", SINCE - 1),
            ]
        }
        self.assertEqual(gc.seat_texts([newest_first], SEAT, SINCE), ["first", "second"])

    def test_pages_are_read_newest_first_across_pages(self) -> None:
        pages = [{"chunk": [event(SEAT, "b", SINCE + 20)], "end": "t1"}, {"chunk": [event(SEAT, "a", SINCE + 10)]}]
        self.assertEqual(gc.seat_texts(pages, SEAT, SINCE), ["a", "b"])

    def test_a_message_that_is_no_agent_text_is_not_counted(self) -> None:
        plain = event(SEAT, "x")
        del plain["content"][gc.TEXT_KEY]
        state = {"type": "m.room.member", "sender": SEAT, "origin_server_ts": SINCE + 5, "content": {}}
        self.assertEqual(gc.seat_texts([{"chunk": [plain, state]}], SEAT, SINCE), [])

    def test_a_page_with_no_chunk_list_is_refused(self) -> None:
        with self.assertRaises(ValueError):
            gc.seat_texts([{"end": "t"}], SEAT, SINCE)


class TallyTest(unittest.TestCase):
    def test_goodbyes_disconnects_and_the_rest_are_sorted_apart(self) -> None:
        tally = gc.tally([goodbye(), disconnected(), "something else"], HANDLE)
        self.assertEqual(tally, {"goodbye": [goodbye()], "disconnected": [disconnected()], "other": ["something else"]})

    def test_another_handles_goodbye_is_other(self) -> None:
        tally = gc.tally(["fedora-desktop.2 left the team: session ended (exit)"], HANDLE)
        self.assertEqual(tally["goodbye"], [])
        self.assertEqual(len(tally["other"]), 1)


class ExpectTest(unittest.TestCase):
    def check(self, texts: list[str], goodbyes: int, disconnects: int) -> None:
        gc.expect_tally(gc.tally(texts, HANDLE), HANDLE, goodbyes, disconnects, "exit")

    def test_g1_exactly_one_goodbye_passes(self) -> None:
        self.check([goodbye()], 1, 0)

    def test_g2_g3_exactly_one_disconnect_passes(self) -> None:
        self.check([disconnected()], 0, 1)

    def test_g4_both_messages_fail_whatever_was_wanted(self) -> None:
        for wanted in ((1, 0), (0, 1)):
            with self.subTest(wanted=wanted), self.assertRaisesRegex(gc.Failed, "both a goodbye and a disconnected"):
                self.check([goodbye(), disconnected()], *wanted)

    def test_two_goodbyes_fail(self) -> None:
        with self.assertRaisesRegex(gc.Failed, "2 goodbye"):
            self.check([goodbye(), goodbye()], 1, 0)

    def test_two_disconnects_fail(self) -> None:
        with self.assertRaisesRegex(gc.Failed, "2 disconnected"):
            self.check([disconnected(), disconnected()], 0, 1)

    def test_nothing_posted_fails(self) -> None:
        with self.assertRaisesRegex(gc.Failed, "0 goodbye"):
            self.check([], 1, 0)
        with self.assertRaisesRegex(gc.Failed, "0 disconnected"):
            self.check([], 0, 1)

    def test_a_goodbye_with_another_reason_fails(self) -> None:
        with self.assertRaisesRegex(gc.Failed, "other"):
            self.check([goodbye("other")], 1, 0)

    def test_a_disconnect_in_another_wording_fails(self) -> None:
        with self.assertRaisesRegex(gc.Failed, "wording"):
            self.check([f"{HANDLE} disconnected: something new"], 0, 1)


class NextFromTest(unittest.TestCase):
    def test_the_end_token_of_a_page_with_events(self) -> None:
        self.assertEqual(gc.next_from({"chunk": [event(SEAT, "x")], "start": "s", "end": "t1"}), "t1")

    def test_no_more_pages(self) -> None:
        self.assertIsNone(gc.next_from({"chunk": [event(SEAT, "x")], "start": "s"}))
        self.assertIsNone(gc.next_from({"chunk": [], "start": "s", "end": "t1"}))
        self.assertIsNone(gc.next_from({"chunk": [event(SEAT, "x")], "start": "t1", "end": "t1"}))


class LastTokenTest(unittest.TestCase):
    def conf(self, token_line: str) -> str:
        return f'# CCY Launch Configuration\nSAVED_CONFIG_VERSION=3\n{token_line}\nLAST_SSH_KEYS=""\n'

    def test_the_saved_token_name(self) -> None:
        self.assertEqual(gc.last_token(self.conf('LAST_TOKEN="personal"')), "personal")
        self.assertEqual(gc.last_token(self.conf("LAST_TOKEN=work-2")), "work-2")

    def test_no_or_an_empty_or_an_odd_token_is_refused(self) -> None:
        for line in ("", 'LAST_TOKEN=""', 'LAST_TOKEN="a b"', "LAST_TOKEN=$(id)"):
            with self.subTest(line=line), self.assertRaises(gc.Failed):
                gc.last_token(self.conf(line))


class PaneCleanTest(unittest.TestCase):
    def test_a_quiet_exit_passes(self) -> None:
        gc.pane_clean("Supervisor: on by default\n✓ seat 1@accept194a: ...\n")

    def test_a_traceback_or_a_failed_disconnect_fails(self) -> None:
        for text in (
            "Traceback (most recent call last):\n  File ...",
            "✗ pingbus seat disconnected failed (the reason is above)",
            "pingbus seat disconnected was stopped after 60 s",
        ):
            with self.subTest(text=text), self.assertRaises(gc.Failed):
                gc.pane_clean(text)


class PaneErrorsTest(unittest.TestCase):
    """A launcher that ended before its container started: its ERROR lines are the reason."""

    REFUSAL = ("Commit signing is on in ~/.gitconfig, but the session has no SSH identity to sign with.")

    def test_the_error_line_without_its_colour_codes(self) -> None:
        pane = (
            "✓ Using token: personal (expires in 200 days)\r\n"
            f"\x1b[0;31mERROR:\x1b[0m {self.REFUSAL}\r\n"
            "  Every commit in the container would fail. Choose an SSH key at launch.\r\n"
        )
        self.assertEqual(gc.pane_errors(pane), [f"ERROR: {self.REFUSAL}"])

    def test_only_lines_starting_with_error_never_the_rest(self) -> None:
        pane = (
            "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-secret\n"
            "the launch said ERROR: in the middle of a line\n"
            "   ERROR: indented, still the launcher's\n"
            "\x1b]0;CCY: checkout\x07ERROR: after a title sequence\n"
            "ERRORS are not errors\n"
        )
        self.assertEqual(gc.pane_errors(pane), ["ERROR: indented, still the launcher's",
                                                "ERROR: after a title sequence"])

    def test_the_last_few_only(self) -> None:
        pane = "".join(f"ERROR: number {n}\n" for n in range(10))
        self.assertEqual(gc.pane_errors(pane), [f"ERROR: number {n}" for n in range(10 - gc.PANE_ERRORS_MAX, 10)])

    def test_a_pane_with_no_error_line_gives_nothing(self) -> None:
        self.assertEqual(gc.pane_errors("Validating token...\n✓ Token validated successfully\n"), [])

    def test_the_command_prints_one_per_line(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            log = pathlib.Path(tmp, "pane.log")
            log.write_text(f"x\n\x1b[0;31mERROR:\x1b[0m {self.REFUSAL}\n", encoding="utf-8")
            self.assertEqual(run_main("pane-errors", str(log)), (0, f"ERROR: {self.REFUSAL}\n", ""))


class CommandLineTest(unittest.TestCase):
    def test_tally_then_expect_through_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            page = pathlib.Path(tmp, "page.json")
            page.write_text(json.dumps({"chunk": [event(SEAT, goodbye())]}), encoding="utf-8")
            code, out, err = run_main("tally", SEAT, HANDLE, str(SINCE), str(page))
            self.assertEqual((code, err), (0, ""))
            tally = pathlib.Path(tmp, "tally.json")
            tally.write_text(out, encoding="utf-8")
            self.assertEqual(run_main("expect", str(tally), HANDLE, "1", "0", "exit")[0], 0)
            code, _, err = run_main("expect", str(tally), HANDLE, "0", "1", "exit")
            self.assertEqual(code, 1)
            self.assertIn("posted 1 goodbye(s), want 0", err)

    def test_fake_page_builds_what_tally_reads(self) -> None:
        code, out, _ = run_main("fake-page", "t1", str(SINCE + 5), SEAT, goodbye())
        self.assertEqual(code, 0)
        page = json.loads(out)
        self.assertEqual(gc.next_from(page), "t1")
        self.assertEqual(gc.seat_texts([page], SEAT, SINCE), [goodbye()])

    def test_usage(self) -> None:
        self.assertEqual(run_main()[0], 64)
        self.assertEqual(run_main("tally", SEAT)[0], 64)
        self.assertEqual(run_main("expect", "f", HANDLE, "one", "0", "exit")[0], 64)


class FakeRunTest(unittest.TestCase):
    """acceptance.bash --fake: the bash judgement legs over fixture rooms, no host needed."""

    def test_fake_mode_accepts_the_good_rooms_and_rejects_the_bad(self) -> None:
        env = {key: value for key, value in os.environ.items() if not key.startswith(("PLAN_", "TMUX"))}
        run = subprocess.run(
            ["bash", str(_HERE / "acceptance.bash"), "--fake"],
            check=False, capture_output=True, text=True, env=env, stdin=subprocess.DEVNULL, timeout=300,
        )
        self.assertEqual(run.returncode, 0, run.stdout + run.stderr)
        self.assertIn("FAKE ACCEPTED", run.stdout)


if __name__ == "__main__":
    unittest.main()
