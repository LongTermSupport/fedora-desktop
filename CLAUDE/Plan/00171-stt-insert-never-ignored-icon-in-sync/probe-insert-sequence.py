"""The owner's Insert rules replayed, judged from the panel's debug log: <debug.log> <offset> <report.md>.

Plan 00171 acceptance (Task 5.11). The owner dictates once (A) and presses Insert to stop
it, presses Insert again during the stop grace, and again while A is transcribing or the
instant its result lands. Once the icon is ready the owner presses Insert to start a new
dictation (B), speaks, and presses Insert to stop it. The panel's own [EXT] lines, written
from byte <offset> of <debug.log> on, are read for the latest such replay, and say whether:

  1. A was stopped once: no press after the first sent A a second stop;
  2. a press during A's grace did nothing, and the icon flashed BLOCKED;
  3. a press while A was transcribing or pasting did nothing, and the icon flashed BLOCKED;
  4. nothing started by itself: B was started by a press made after the last blocked one;
  5. B reached RECORDING and the next press stopped it (TERM to B's PID);
  6. nothing showed the ready icon while B recorded: no IDLE taken, no `Display: IDLE`
     (the icon itself), none of B's own signals dropped.

Exit 0 when all hold; 1 when one fails; 2 when nothing failed but part of the sequence was
not played, so there is no evidence either way (the report says what to press).
"""

from __future__ import annotations

import datetime
import pathlib
import re
import sys

EXT_LINE = re.compile(r"^\[(\S+)\] \[EXT\] (.*)$")
STARTED = re.compile(r"^Started (\w+) recorder (\d+) \(session (\S+)\)$")
STOPPED = re.compile(r"^Insert while RECORDING: stop after the grace \(TERM to (\d+)\)$")
# panelSession.js _block: "<state>: <pid> is stopping…" or "<state> and <pid> has exited…"
BLOCKED = re.compile(r"^Insert does nothing while (\w+)(?::| and) (\d+) ")
ANY_BLOCKED = "Insert does nothing while "
DROPPED = re.compile(r"^Dropped (.+?) from session (\S+):")
STATE = re.compile(r"^State: (\w+)$")
DISPLAY = re.compile(r"^Display: (\w+)$")  # what the icon showed (panelSession.js)
PRESS = "INSERT KEY PRESSED"
# A press before the recorder has said STOPPING is a press in the grace too
GRACE_STATES = ("RECORDING", "STOPPING")
# A press's own icon change is logged in the same call; the flash lasts 0.7 s
PRESS_RENDER_SECONDS = 0.2


def events(log: pathlib.Path, offset: int) -> list[tuple[datetime.datetime, str]]:
    """The panel's lines written since byte `offset`, with their times."""
    with log.open("rb") as f:
        f.seek(offset)
        text = f.read().decode("utf-8", errors="replace")
    found = []
    for line in text.splitlines():
        match = EXT_LINE.match(line)
        if match:
            when = datetime.datetime.fromisoformat(match.group(1).replace("Z", "+00:00"))
            found.append((when, match.group(2)))
    return found


def verdict(ok: bool | None) -> str:
    return {True: "PASS", False: "FAIL", None: "NOT ESTABLISHED"}[ok]


def judge(lines: list[tuple[datetime.datetime, str]]) -> tuple[list[str], int]:
    """(report lines, exit status: 0 all held, 1 one failed, 2 part not played)."""
    messages = [m for _, m in lines]
    # The latest replay: the latest stopped dictation a press was blocked on afterwards
    replay = None
    for i in range(len(messages) - 1, -1, -1):
        stopped = STOPPED.match(messages[i])
        if not stopped:
            continue
        pid_a = stopped.group(1)
        blocks = [(j, BLOCKED.match(messages[j]).group(1)) for j in range(i + 1, len(messages))
                  if BLOCKED.match(messages[j]) and BLOCKED.match(messages[j]).group(2) == pid_a]
        if blocks:
            replay = (i, pid_a, blocks)
            break
    if replay is None:
        return ["NOT PLAYED: no Insert was blocked behind a dictation it had stopped. Dictate, "
                "press Insert, press it again during the grace and again after it, until the text "
                "is pasted (the grace shows the orange dots too), then run this again."], 2
    stop_at, pid_a, blocks = replay
    report: list[str] = []
    results: list[bool | None] = []

    def record(ok: bool | None, text: str) -> None:
        results.append(ok)
        report.append(f"{verdict(ok)}: {text}")

    stops_of_a = [m for m in messages[stop_at:] if STOPPED.match(m) and STOPPED.match(m).group(1) == pid_a]
    record(len(stops_of_a) == 1, f"A ({pid_a}) was asked to stop once (stops sent: {len(stops_of_a)})")

    def flashed(index: int) -> bool:
        """The icon showed BLOCKED just after the press. The press's own Display line comes
        at once; a press during the flash keeps it on and logs none, and the flash's end
        comes BLOCKED_FLASH_MS later, so a later Display line is not the press's."""
        after = index + 1
        if (after < len(lines) and DISPLAY.match(messages[after])
                and (lines[after][0] - lines[index][0]).total_seconds() < PRESS_RENDER_SECONDS):
            return DISPLAY.match(messages[after]).group(1) == "BLOCKED"
        shown = [DISPLAY.match(m).group(1) for m in messages[:index] if DISPLAY.match(m)]
        return bool(shown) and shown[-1] == "BLOCKED"

    grace = [j for j, state in blocks if state in GRACE_STATES]
    rendering = [j for j, state in blocks if state not in GRACE_STATES]
    # The icon shows the same orange dots in STOPPING and TRANSCRIBING (panelLook.js)
    missing_hint = {
        "during A's grace": "press Insert again within the stop grace",
        "while A was transcribing or pasting": "the grace shows the orange dots too; after "
                                               "the grace keep pressing Insert until the text is pasted",
    }
    for label, found in (("during A's grace", grace), ("while A was transcribing or pasting", rendering)):
        if not found:
            record(None, f"a press {label} did nothing (no such press: {missing_hint[label]})")
        else:
            shown = all(flashed(j) for j in found)
            record(shown, f"a press {label} did nothing ({len(found)} press(es); "
                          f"BLOCKED shown for each: {shown})")
    report.append("Presses that did nothing behind A (time UTC, state): "
                  + ", ".join(f"{lines[j][0]:%H:%M:%S.%f}"[:-3] + f" {state}" for j, state in blocks))

    last_block = blocks[-1][0]
    b_index = next((j for j in range(last_block + 1, len(messages)) if STARTED.match(messages[j])), None)
    if b_index is None:
        record(None, "B was started by a fresh press (no recorder started after the blocked "
                     "presses: press Insert once the icon is ready)")
        return report, 1 if False in results else 2
    press_at = next((j for j in range(b_index - 1, last_block, -1) if PRESS in messages[j]), None)
    fresh = press_at is not None and not any(ANY_BLOCKED in m for m in messages[press_at:b_index])
    record(fresh, "nothing started by itself: B was started by a press made after the last blocked one")

    started = STARTED.match(messages[b_index])
    pid_b, token_b = started.group(2), started.group(3)
    recording_at = stop_b = None
    for j in range(b_index + 1, len(messages)):
        state = STATE.match(messages[j])
        if recording_at is None and state and state.group(1) == "RECORDING":
            recording_at = j
        stopped = STOPPED.match(messages[j])
        if recording_at is not None and stopped and stopped.group(1) == pid_b:
            stop_b = j
            break
    if recording_at is None or stop_b is None:
        record(None if recording_at is not None else False,
               f"B ({pid_b}) reached RECORDING and the next press stopped it "
               f"(RECORDING seen: {recording_at is not None}; stop of {pid_b} seen: {stop_b is not None})")
    else:
        record(True, f"B ({pid_b}) reached RECORDING and the next press stopped it")

    window = messages[b_index:stop_b if stop_b is not None else len(messages)]
    idle_taken = [m for m in window if STATE.match(m) and STATE.match(m).group(1) == "IDLE"]
    shown = [DISPLAY.match(m).group(1) for m in window if DISPLAY.match(m)]
    own_dropped = [m for m in window if DROPPED.match(m) and DROPPED.match(m).group(2) == token_b]
    ok = not idle_taken and "IDLE" not in shown and not own_dropped
    record(ok if "RECORDING" in shown else None,
           f"nothing showed the ready icon while B recorded (IDLE taken: {len(idle_taken)}; "
           f"Display: IDLE: {shown.count('IDLE')}; B's own signals dropped: {len(own_dropped)})"
           + ("" if "RECORDING" in shown else "; no 'Display: RECORDING' line, so the running "
              "panel predates Task 5.8 (log out and back in)"))

    stale = [m for m in messages[b_index:] if DROPPED.match(m) and DROPPED.match(m).group(2) != token_b]
    report.append(f"Signals from an earlier recorder dropped after B started: {len(stale)}"
                  + (f" ({'; '.join(stale[:3])})" if stale else ""))
    if False in results:
        return report, 1
    return report, 2 if None in results else 0


def main(argv: list[str]) -> int:
    if len(argv) != 4 or not argv[2].isdigit():
        print("usage: probe-insert-sequence.py <debug.log> <offset> <report.md>", file=sys.stderr)
        return 64
    log, offset, report_path = pathlib.Path(argv[1]), int(argv[2]), pathlib.Path(argv[3])
    if not log.is_file():
        print(f"probe-insert-sequence: no debug log at {log} (is Debug Logging on?)", file=sys.stderr)
        return 1
    report, status = judge(events(log, offset))
    with report_path.open("a", encoding="utf-8") as f:
        f.write("## The Insert rules replayed\n\n" + "".join(f"- {line}\n" for line in report) + "\n")
    for line in report:
        print(f"  {line}")
    return status


if __name__ == "__main__":
    sys.exit(main(sys.argv))
