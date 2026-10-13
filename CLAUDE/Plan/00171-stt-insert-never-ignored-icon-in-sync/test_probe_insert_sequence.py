"""Tests for probe-insert-sequence.py, the replay judge of this plan's acceptance.bash.

The panel's [EXT] lines are built here as panelSession.js and extension.js write them, and
judge() is asked which presses fell in A's grace and which while A was transcribing or
pasting. The icon shows the same orange dots in STOPPING (the grace) and TRANSCRIBING
(panelLook.js), so a press "on the orange dots" may be a grace press: the report must say
the state of every blocked press, and what to press when the second kind is missing.

Run from anywhere:
    python3 CLAUDE/Plan/00171-stt-insert-never-ignored-icon-in-sync/test_probe_insert_sequence.py
"""

from __future__ import annotations

import datetime
import importlib.util
import pathlib
import sys
import unittest

_MODULE_PATH = pathlib.Path(__file__).resolve().parent / "probe-insert-sequence.py"
_SPEC = importlib.util.spec_from_file_location("probe_insert_sequence", _MODULE_PATH)
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_MODULE_PATH}")
probe = importlib.util.module_from_spec(_SPEC)
sys.modules["probe_insert_sequence"] = probe
_SPEC.loader.exec_module(probe)

START = datetime.datetime(2026, 1, 1, 12, 0, 0, tzinfo=datetime.UTC)
A, B = "1001", "2002"


def timeline(*steps: tuple[float, str]) -> list[tuple[datetime.datetime, str]]:
    return [(START + datetime.timedelta(seconds=at), message) for at, message in steps]


def press(at: float) -> tuple[float, str]:
    return at, "INSERT KEY PRESSED - callback fired!"


def blocked(at: float, why: str) -> list[tuple[float, str]]:
    return [press(at), (at, f"Insert does nothing while {why}"), (at + 0.01, "Display: BLOCKED")]


def replay(*after_stop: tuple[float, str]) -> list[tuple[datetime.datetime, str]]:
    """A recorded and stopped, `after_stop`, then B started, recorded and stopped."""
    return timeline(
        press(0.0), (0.0, f"Started dictation recorder {A} (session tok-a)"),
        (0.5, "State: RECORDING"), (0.5, "Display: RECORDING"),
        press(5.0), (5.0, f"Insert while RECORDING: stop after the grace (TERM to {A})"),
        (5.1, "State: STOPPING"), (5.1, "Display: STOPPING"),
        *after_stop,
        (20.0, "State: SUCCESS"), (20.0, "Display: SUCCESS"),
        press(25.0), (25.0, f"Started dictation recorder {B} (session tok-b)"),
        (25.5, "State: RECORDING"), (25.5, "Display: RECORDING"),
        press(30.0), (30.0, f"Insert while RECORDING: stop after the grace (TERM to {B})"),
    )


class TestPressStates(unittest.TestCase):
    def test_presses_on_the_orange_dots_of_the_grace_alone_say_the_grace_shows_them_too(self) -> None:
        report, status = probe.judge(replay(*blocked(6.0, f"STOPPING: {A} is stopping or finishing the dictation"),
                                            *blocked(7.0, f"STOPPING: {A} is stopping or finishing the dictation")))
        self.assertEqual(status, 2)
        missing = next(line for line in report if "transcribing or pasting" in line)
        self.assertTrue(missing.startswith("NOT ESTABLISHED"))
        self.assertIn("the grace shows the orange dots too", missing)
        self.assertIn("until the text is pasted", missing)

    def test_every_blocked_press_is_listed_with_its_time_and_state(self) -> None:
        report, _ = probe.judge(replay(*blocked(6.0, f"STOPPING: {A} is stopping or finishing the dictation"),
                                       (9.0, "State: TRANSCRIBING"), (9.0, "Display: TRANSCRIBING"),
                                       *blocked(10.0, f"TRANSCRIBING: {A} is stopping or finishing the dictation")))
        listing = next(line for line in report if line.startswith("Presses that did nothing behind A"))
        self.assertIn("12:00:06.000 STOPPING", listing)
        self.assertIn("12:00:10.000 TRANSCRIBING", listing)

    def test_a_press_while_transcribing_counts_as_one_while_transcribing(self) -> None:
        report, status = probe.judge(replay(*blocked(6.0, f"STOPPING: {A} is stopping or finishing the dictation"),
                                            (9.0, "State: TRANSCRIBING"), (9.0, "Display: TRANSCRIBING"),
                                            *blocked(10.0, f"TRANSCRIBING: {A} is stopping or finishing the dictation")))
        self.assertIn("PASS: a press while A was transcribing or pasting did nothing (1 press(es); "
                      "BLOCKED shown for each: True)", report)
        self.assertEqual(status, 0)

    def test_a_press_after_the_result_before_the_exit_counts_as_one_while_pasting(self) -> None:
        report, status = probe.judge(replay(*blocked(6.0, f"STOPPING: {A} is stopping or finishing the dictation"),
                                            *blocked(19.0, f"SUCCESS and {A} has exited (its last signal is not in yet)")))
        self.assertTrue(any(line.startswith("PASS: a press while A was transcribing or pasting") for line in report))
        self.assertEqual(status, 0)


class TestPressAfterThePaste(unittest.TestCase):
    """Issue #96: a press within 1 s of the paste once started a recording a late IDLE orphaned."""

    def test_the_gap_from_a_result_to_the_press_that_started_b_is_reported(self) -> None:
        report, _ = probe.judge(replay(*blocked(6.0, f"STOPPING: {A} is stopping or finishing the dictation")))
        self.assertIn("The press that started B came 5.000 s after A's SUCCESS (within 1 s of the paste: no)", report)

    def test_a_press_within_a_second_of_the_result_is_named(self) -> None:
        lines = replay(*blocked(6.0, f"STOPPING: {A} is stopping or finishing the dictation"))
        moved = [(when - datetime.timedelta(seconds=4.4), m) if when >= START + datetime.timedelta(seconds=25) else (when, m)
                 for when, m in lines]
        report, _ = probe.judge(moved)
        self.assertIn("The press that started B came 0.600 s after A's SUCCESS (within 1 s of the paste: yes)", report)


if __name__ == "__main__":
    unittest.main()
