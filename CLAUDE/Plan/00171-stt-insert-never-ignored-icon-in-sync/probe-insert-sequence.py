"""The incident replayed, judged from the panel's debug log: <debug.log> <offset> <report.md>.

Plan 00171 acceptance. The owner dictates once (A), presses Insert while A is still
finishing (transcribing, or the instant its result lands), and presses Insert again once
the new dictation (B) is recording. The panel's own [EXT] lines, written from byte
<offset> of <debug.log> on, are read for the latest such replay, and say whether:

  1. the press while A was finishing was queued, and B started only after A had exited;
  2. B reached RECORDING and the second press stopped B (TERM to B's PID);
  3. nothing showed the ready icon while B recorded: no IDLE taken, no `Display: IDLE`
     (the icon itself), none of B's own signals dropped.

Signals from A that arrived after B started are counted: each is one the old panel would
have taken as B's. Exit 0 when all three hold; 1 when one fails; 2 when the sequence was
not played at all, so there is no evidence either way (the report says what to press).
"""

from __future__ import annotations

import datetime
import pathlib
import re
import sys

EXT_LINE = re.compile(r"^\[(\S+)\] \[EXT\] (.*)$")
STARTED = re.compile(r"^Started (\w+) recorder (\d+) \(session (\S+)\)$")
QUEUED = re.compile(r"^Insert while (\w+) and (\d+) is still finishing")
RAN_QUEUED = "The recorder has finished: running the queued start"
STOPPED = re.compile(r"^Insert while (RECORDING|STOPPING): stop \(TERM to (\d+)\)$")
DROPPED = re.compile(r"^Dropped (.+?) from session (\S+):")
STATE = re.compile(r"^State: (\w+)$")
DISPLAY = re.compile(r"^Display: (\w+)$")  # what the icon showed (panelSession.js)
PRESS = "INSERT KEY PRESSED"
INCIDENT_SECONDS = 0.5  # the incident's press came 376 ms after SUCCESS


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


def judge(lines: list[tuple[datetime.datetime, str]]) -> tuple[list[str], int]:
    """(report lines, exit status: 0 all three held, 1 one failed, 2 not played)."""
    report: list[str] = []
    # The latest replay: an earlier one may predate a fix being judged
    queued_at = next((i for i in range(len(lines) - 1, -1, -1) if QUEUED.match(lines[i][1])), None)
    if queued_at is None:
        report.append("NOT PLAYED: no Insert was queued behind a finishing dictation. Press "
                      "Insert while the icon shows the orange dots (transcribing) or the "
                      "instant it turns green, then run this again.")
        return report, 2
    queued = QUEUED.match(lines[queued_at][1])
    pid_a = queued.group(2)
    last_end = next((t for t, m in reversed(lines[:queued_at])
                     if STATE.match(m) and STATE.match(m).group(1) in ("SUCCESS", "ERROR")), None)
    press_at = next((t for t, m in reversed(lines[:queued_at]) if PRESS in m), None)
    if last_end and press_at and press_at >= last_end:
        delay = (press_at - last_end).total_seconds()
        timing = "the incident's timing" if delay <= INCIDENT_SECONDS else "later than the incident"
        report.append(f"The press came {delay:.3f} s after A's result ({timing}; the incident: 0.376 s)")
    else:
        report.append(f"The press came while A was {queued.group(1)} (before its result)")

    b_start = None
    ran = False
    for i in range(queued_at + 1, len(lines)):
        if lines[i][1] == RAN_QUEUED:
            ran = True
        started = STARTED.match(lines[i][1])
        if started:
            b_start = (i, started.group(2), started.group(3))
            break
    ok1 = b_start is not None and ran
    report.append(f"{'PASS' if ok1 else 'FAIL'}: the press while A ({pid_a}) was finishing was "
                  "queued, and B started only once A had exited"
                  + ("" if ok1 else " (no queued start ran)"))
    if b_start is None:
        return report, 1
    b_index, pid_b, token_b = b_start

    recording_at = stop_at = None
    for i in range(b_index + 1, len(lines)):
        message = lines[i][1]
        state = STATE.match(message)
        if recording_at is None and state and state.group(1) == "RECORDING":
            recording_at = i
        stopped = STOPPED.match(message)
        if recording_at is not None and stopped and stopped.group(2) == pid_b:
            stop_at = i
            break
    ok2 = recording_at is not None and stop_at is not None
    report.append(f"{'PASS' if ok2 else 'FAIL'}: B ({pid_b}) reached RECORDING and the next "
                  "press stopped it"
                  + ("" if ok2 else f" (RECORDING seen: {recording_at is not None}; "
                                    f"stop of {pid_b} seen: {stop_at is not None})"))

    window = lines[b_index:stop_at if stop_at is not None else len(lines)]
    idle_taken = [m for _, m in window if STATE.match(m) and STATE.match(m).group(1) == "IDLE"]
    shown = [DISPLAY.match(m).group(1) for _, m in window if DISPLAY.match(m)]
    own_dropped = [m for _, m in window if DROPPED.match(m) and DROPPED.match(m).group(2) == token_b]
    # No Display: line at all is a panel too old to log it, not proof the icon was right
    sees_icon = "RECORDING" in shown
    ok3 = sees_icon and not idle_taken and "IDLE" not in shown and not own_dropped
    report.append(f"{'PASS' if ok3 else 'FAIL'}: nothing showed the ready icon while B recorded "
                  f"(IDLE taken: {len(idle_taken)}; Display: IDLE: {shown.count('IDLE')}; "
                  f"B's own signals dropped: {len(own_dropped)})"
                  + ("" if sees_icon else "; NOT ESTABLISHED: no 'Display: RECORDING' line, "
                     "so the running panel predates Task 5.8 (log out and back in)"))

    stale = [m for _, m in lines[b_index:] if DROPPED.match(m) and DROPPED.match(m).group(2) != token_b]
    report.append(f"Signals from an earlier recorder dropped after B started: {len(stale)}"
                  + (f" ({'; '.join(stale[:3])})" if stale else ""))
    return report, 0 if ok1 and ok2 and ok3 else 1


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
        f.write("## The incident replayed\n\n" + "".join(f"- {line}\n" for line in report) + "\n")
    for line in report:
        print(f"  {line}")
    return status


if __name__ == "__main__":
    sys.exit(main(sys.argv))
