#!/usr/bin/env python3
"""The homeserver stop analysis behind triage-hs.bash (Plan 00203). RUN ON THE HOST, by
triage-hs.bash, on journal exports it has already written (`journalctl -o json`).

For every team homeserver unit (agent-bus-hs@<team>.service) it lists each stop over the whole
exported journal, with what systemd recorded (exit code or signal, Result=, the restart counter,
the watchdog, the OOM killer, a core dump) and who asked for it, judged from what the journal
holds around it:

  health-restart                   the team's health check logged "restarting" just before
  reboot/shutdown                  a requested stop in the last minutes of an earlier boot
  acceptance                       a requested stop inside an acceptance run's time window
  deploy/play                      a requested stop near an ansible or agent-bus-install line,
                                   or inside any other plan run's window
  sudo command                     a requested stop near a sudo line naming agent-bus
  watchdog, oom-kill, core-dump,   an unrequested stop: the process ended without a stop job
  signal, exit-code, exited on
  its own
  boot ended, unit running         the boot's journal ends with the unit running, no stop logged
  requested, requester not found   a stop job, with nothing in the journal saying whose

Fact-finding only: it renders no verdict on the homeserver. Writes the full report to --report
and prints the per-team summary on stdout; a failure on stderr, exit 1; usage 2 (argparse).
"""

from __future__ import annotations

import argparse
import dataclasses
import json
import os
import pathlib
import re
import sys
import time
from collections import Counter
from collections.abc import Iterable, Sequence

HS_RE = re.compile(r"^agent-bus-hs@(?P<team>[^.]+)\.service$")
STAMP_RE = re.compile(r"^\d{8}-\d{6}$")
STAMP_FMT = "%Y%m%d-%H%M%S"

#: How far around a stop the journal is searched for its cause.
ACTOR_BEFORE = 900.0
HEALTH_BEFORE = 120.0
BOOT_END_MARGIN = 300.0
CONTEXT_BEFORE = 300.0
CONTEXT_LINES = 12
KERNEL_BEFORE = 120.0
BACKUP_BEFORE = 900.0

DELIBERATE, NOT_DELIBERATE, UNKNOWN = "deliberate", "not deliberate", "unknown"
CATEGORY_KIND = {
    "deploy/play": DELIBERATE,
    "acceptance": DELIBERATE,
    "reboot/shutdown": DELIBERATE,
    "sudo command": DELIBERATE,
    "health-restart": NOT_DELIBERATE,
    "watchdog": NOT_DELIBERATE,
    "oom-kill": NOT_DELIBERATE,
    "core-dump": NOT_DELIBERATE,
    "signal": NOT_DELIBERATE,
    "exit-code": NOT_DELIBERATE,
    "exited on its own": NOT_DELIBERATE,
    "boot ended, unit running": UNKNOWN,
    "requested, requester not found": UNKNOWN,
}

_REDACTIONS = (
    (re.compile(r"(access_token=)[^&\s\"']+"), r"\1<redacted>"),
    (re.compile(r"(?i)(bearer\s+)\S+"), r"\1<redacted>"),
    (re.compile(r"\bsyt_[A-Za-z0-9_]+"), "<redacted-token>"),
    (re.compile(r"(?i)\b(password|secret|token)(\"?\s*[:=]\s*)\S+"), r"\1\2<redacted>"),
)
_LEAK_RE = re.compile(r"syt_[A-Za-z0-9]{4}|access_token=(?!<redacted>)")


class Failed(Exception):
    """The analysis cannot run; the message says why."""


@dataclasses.dataclass(frozen=True)
class Entry:
    ts: float
    boot: str
    #: The unit the line is about: UNIT= for systemd's own lines, COREDUMP_UNIT=, else the
    #: unit whose process wrote it (_SYSTEMD_UNIT=); empty for a line from outside any of them.
    unit: str
    #: "systemd" for PID 1, else the syslog identifier (sudo, kernel, ansible-..., tuwunel).
    origin: str
    msg: str


@dataclasses.dataclass(frozen=True)
class Boot:
    boot_id: str
    first: float
    last: float


@dataclasses.dataclass(frozen=True)
class Window:
    label: str
    start: float
    end: float


@dataclasses.dataclass
class Stop:
    unit: str
    at: float
    boot: str
    requested: bool
    boot_end: bool = False
    during_start: bool = False
    code: str = ""
    status: str = ""
    result: str = ""
    watchdog: bool = False
    oom: bool = False
    coredump: bool = False
    nrestarts: int | None = None
    consumed: str = ""
    actor: str = ""
    window: str = ""
    backup_near: str = ""
    category: str = ""
    context: list[str] = dataclasses.field(default_factory=list)


def redact(line: str) -> str:
    for pattern, repl in _REDACTIONS:
        line = pattern.sub(repl, line)
    return line


def _text(value: object) -> str:
    """journalctl's JSON gives a field as a string, or as a byte list when it is not UTF-8."""
    if isinstance(value, list):
        return bytes(int(b) for b in value).decode("utf-8", "replace")
    return "" if value is None else str(value)


def load_entries(paths: Iterable[pathlib.Path]) -> list[Entry]:
    seen: set[str] = set()
    out: list[Entry] = []
    for path in paths:
        with path.open(encoding="utf-8") as fh:
            for n, line in enumerate(fh, 1):
                if not line.strip():
                    continue
                try:
                    rec = json.loads(line)
                except json.JSONDecodeError as exc:
                    raise Failed(f"{path}:{n} is not journal JSON: {exc}") from exc
                cursor = _text(rec.get("__CURSOR"))
                if cursor in seen:
                    continue
                seen.add(cursor)
                pid1 = _text(rec.get("_PID")) == "1"
                if pid1 and rec.get("UNIT"):
                    unit = _text(rec.get("UNIT"))
                elif rec.get("COREDUMP_UNIT"):
                    unit = _text(rec.get("COREDUMP_UNIT"))
                else:
                    unit = _text(rec.get("_SYSTEMD_UNIT"))
                origin = (
                    "systemd"
                    if pid1
                    else (
                        _text(rec.get("SYSLOG_IDENTIFIER")) or _text(rec.get("_COMM"))
                    )
                )
                out.append(
                    Entry(
                        ts=int(_text(rec["__REALTIME_TIMESTAMP"])) / 1_000_000,
                        boot=_text(rec.get("_BOOT_ID")),
                        unit=unit,
                        origin=origin,
                        msg=_text(rec.get("MESSAGE")),
                    )
                )
    out.sort(key=lambda e: e.ts)
    return out


def load_boots(path: pathlib.Path) -> list[Boot]:
    """`journalctl --list-boots -o json`: oldest first, the current boot last."""
    try:
        recs = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise Failed(f"{path} is not --list-boots JSON: {exc}") from exc
    boots = [
        Boot(r["boot_id"], r["first_entry"] / 1_000_000, r["last_entry"] / 1_000_000)
        for r in recs
    ]
    if not boots:
        raise Failed(f"{path} lists no boot")
    return sorted(boots, key=lambda b: b.first)


def _stamp(name: str) -> float:
    return time.mktime(time.strptime(name, STAMP_FMT))


def plan_run_windows(root: pathlib.Path) -> list[Window]:
    """Each plan run's time window: untracked/plan-runs/<plan>/<script>/<stamp>/ from its stamp
    to its newest file, and each meta-deploy entry's log from the previous log's last write
    (the run's stamp for the first) to its own."""
    windows: list[Window] = []
    if not root.is_dir():
        return windows
    for run in sorted(
        p for p in root.glob("*/*") if p.is_dir() and STAMP_RE.match(p.name)
    ):
        logs = sorted(
            (f for f in run.iterdir() if f.is_file() and f.suffix == ".log"),
            key=lambda f: f.stat().st_mtime,
        )
        start = _stamp(run.name)
        for log in logs:
            end = log.stat().st_mtime
            windows.append(Window(f"{run.relative_to(root)}/{log.name}", start, end))
            start = end
    for run in sorted(
        p for p in root.glob("*/*/*") if p.is_dir() and STAMP_RE.match(p.name)
    ):
        files = [f.stat().st_mtime for f in run.rglob("*") if f.is_file()]
        start = _stamp(run.name)
        windows.append(
            Window(str(run.relative_to(root)), start, max(files, default=start))
        )
    return windows


def size_bytes(text: str) -> int:
    m = re.fullmatch(r"([\d.]+)([BKMGT]?)", text)
    if not m:
        raise Failed(f"not a size: {text}")
    return int(float(m.group(1)) * 1024 ** "BKMGT".index(m.group(2) or "B"))


def team_of(unit: str) -> str:
    m = HS_RE.match(unit)
    return m.group("team") if m else ""


def _body(unit: str, msg: str) -> str:
    return msg[len(unit) + 2 :] if msg.startswith(unit + ": ") else msg


def _lifecycle(
    unit: str, entries: Sequence[Entry], boots: Sequence[Boot]
) -> list[Stop]:
    """The stops systemd recorded for <unit>, in order, before any judgement of who."""
    last_of = {b.boot_id: b.last for b in boots}
    current = boots[-1].boot_id
    stops: list[Stop] = []
    stop: Stop | None = None
    running = False
    run_boot = ""

    def opened(e: Entry, requested: bool) -> Stop:
        nonlocal stop, running
        if stop is None:
            stop = Stop(
                unit=unit,
                at=e.ts,
                boot=e.boot,
                requested=requested,
                during_start=not running,
            )
            stops.append(stop)
        running = False
        return stop

    for e in entries:
        if e.unit != unit or e.origin not in ("systemd", "systemd-coredump"):
            continue
        if running and e.boot != run_boot:
            stops.append(
                Stop(
                    unit=unit,
                    at=last_of.get(run_boot, e.ts),
                    boot=run_boot,
                    requested=False,
                    boot_end=True,
                )
            )
            running = False
        body = _body(unit, e.msg)
        if e.origin == "systemd-coredump":
            if "dumped core" in body:
                opened(e, False).coredump = True
        elif body.startswith("Started "):
            stop, running, run_boot = None, True, e.boot
        elif body.startswith("Starting "):
            # A new start attempt: a failure from here on is its own stop, not the last one's.
            stop = None
        elif body.startswith("Stopping "):
            stop = None
            opened(e, True)
        elif m := re.search(r"Main process exited, code=(\w+), status=(\S+)", body):
            cur = opened(e, False)
            cur.code, cur.status = m.group(1), m.group(2)
        elif "Watchdog timeout" in body:
            opened(e, False).watchdog = True
        elif "killed by the OOM killer" in body or "systemd-oomd killed" in body:
            opened(e, False).oom = True
        elif m := re.search(r"Failed with result '([\w-]+)'", body):
            opened(e, False).result = m.group(1)
        elif (
            m := re.search(r"restart counter is at (\d+)", body)
        ) and stop is not None:
            stop.nrestarts = int(m.group(1))
        elif body.startswith("Consumed ") and stop is not None:
            stop.consumed = body
    if running and run_boot != current:
        stops.append(
            Stop(
                unit=unit,
                at=last_of.get(run_boot, 0.0),
                boot=run_boot,
                requested=False,
                boot_end=True,
            )
        )
    return stops


def _between(entries: Sequence[Entry], start: float, end: float) -> list[Entry]:
    return [e for e in entries if start <= e.ts <= end]


def _categorise(stop: Stop, near: Sequence[Entry], boots: Sequence[Boot]) -> str:
    team = team_of(stop.unit)
    if stop.boot_end:
        return "boot ended, unit running"
    if not stop.requested:
        if stop.oom or stop.result == "oom-kill":
            return "oom-kill"
        if stop.watchdog or stop.result == "watchdog":
            return "watchdog"
        if stop.coredump or stop.code == "dumped" or stop.result == "core-dump":
            return "core-dump"
        if stop.code == "killed" or stop.result == "signal":
            return "signal"
        if stop.result == "exit-code" or (
            stop.code == "exited" and not stop.status.startswith("0/")
        ):
            return "exit-code"
        return "exited on its own"
    health = f"agent-bus-hs-health@{team}.service"
    if any(
        e.unit == health and "restarting" in e.msg
        for e in _between(near, stop.at - HEALTH_BEFORE, stop.at)
    ):
        return "health-restart"
    boot = next((b for b in boots if b.boot_id == stop.boot), None)
    if (
        boot is not None
        and boot is not boots[-1]
        and stop.at >= boot.last - BOOT_END_MARGIN
    ):
        return "reboot/shutdown"
    if "acceptance" in stop.window:
        return "acceptance"
    if stop.actor.startswith(("ansible", "agent-bus-install")) or (
        stop.window and not stop.actor
    ):
        return "deploy/play"
    if stop.actor:
        return "sudo command"
    return "requested, requester not found"


def _is_actor(e: Entry) -> bool:
    """A line that can stop a homeserver: an ansible module run naming agent-bus, or a command
    (sudo's record of it) running agent-bus-install or systemctl on an agent-bus unit. A sudo
    journalctl naming the units, as this triage runs, is not one."""
    if e.origin.startswith("ansible"):
        return "agent-bus" in e.msg
    if e.origin in ("sudo", "agent-bus-install"):
        return "agent-bus-install" in e.msg or (
            "systemctl" in e.msg and "agent-bus" in e.msg
        )
    return False


def _actor(stop: Stop, entries: Sequence[Entry]) -> str:
    team = team_of(stop.unit)
    hits = [
        e
        for e in _between(entries, stop.at - ACTOR_BEFORE, stop.at + 2)
        if _is_actor(e)
    ]
    if not hits:
        return ""
    best = next((e for e in reversed(hits) if team in e.msg), hits[-1])
    return f"{best.origin} at {_when(best.ts)}: {redact(best.msg)[:300]}"


def _context(stop: Stop, entries: Sequence[Entry]) -> list[str]:
    own = [
        e
        for e in _between(entries, stop.at - CONTEXT_BEFORE, stop.at + 1)
        if e.unit == stop.unit and e.origin != "systemd" and e.boot == stop.boot
    ]
    kernel = [
        e
        for e in _between(entries, stop.at - KERNEL_BEFORE, stop.at + 5)
        if e.origin == "kernel"
    ]
    lines = own[-CONTEXT_LINES:] + kernel
    lines.sort(key=lambda e: e.ts)
    return [f"{_when(e.ts)} {e.origin}: {redact(e.msg)[:400]}" for e in lines]


def _backup_near(stop: Stop, entries: Sequence[Entry]) -> str:
    unit = f"agent-bus-backup@{team_of(stop.unit)}.service"
    runs = [
        e
        for e in _between(entries, stop.at - BACKUP_BEFORE, stop.at)
        if e.unit == unit
        and e.origin == "systemd"
        and _body(unit, e.msg).startswith("Starting ")
    ]
    return f"a backup started {stop.at - runs[-1].ts:.0f} s before" if runs else ""


def stops_of(
    unit: str,
    entries: Sequence[Entry],
    boots: Sequence[Boot],
    windows: Sequence[Window],
) -> list[Stop]:
    stops = _lifecycle(unit, entries, boots)
    for stop in stops:
        if stop.requested:
            stop.actor = _actor(stop, entries)
            inside = [w for w in windows if w.start - 5 <= stop.at <= w.end + 5]
            stop.window = ", ".join(w.label for w in inside)
        stop.backup_near = _backup_near(stop, entries)
        stop.context = _context(stop, entries)
        stop.category = _categorise(stop, entries, boots)
    return stops


def _when(ts: float) -> str:
    return time.strftime("%Y-%m-%d %H:%M:%S %Z", time.localtime(ts))


def _peaks(unit: str, entries: Sequence[Entry]) -> list[tuple[int, str, float]]:
    out = []
    for e in entries:
        if e.unit == unit and e.origin == "systemd":
            m = re.search(r"([\d.]+[BKMGT]) memory peak", e.msg)
            if m:
                out.append((size_bytes(m.group(1)), _body(unit, e.msg), e.ts))
    return out


def _unit_lines(unit: str, entries: Sequence[Entry], needle: str) -> list[str]:
    return [
        f"{_when(e.ts)} {e.origin}: {redact(e.msg)[:400]}"
        for e in entries
        if e.unit == unit and needle in e.msg
    ]


def _team_report(
    team: str,
    entries: Sequence[Entry],
    boots: Sequence[Boot],
    windows: Sequence[Window],
    out: list[str],
) -> tuple[list[Stop], int]:
    unit = f"agent-bus-hs@{team}.service"
    health = f"agent-bus-hs-health@{team}.service"
    backup = f"agent-bus-backup@{team}.service"
    boot_index = {b.boot_id: i - len(boots) + 1 for i, b in enumerate(boots)}
    stops = stops_of(unit, entries, boots, windows)
    starts = _unit_lines(unit, entries, "Started ")
    out += [
        "",
        "=" * 100,
        f"TEAM {team}: {unit}",
        "=" * 100,
        f"starts recorded: {len([s for s in starts if 'systemd:' in s])}; stops recorded: {len(stops)}",
        "",
    ]
    out.append(
        "### READ THIS FOR: every stop, its cause as systemd recorded it, and who asked for it"
    )
    for n, s in enumerate(stops, 1):
        out.append(
            f"--- stop {n}: {_when(s.at)}  boot {boot_index.get(s.boot, '?')}  [{CATEGORY_KIND[s.category]}] "
            f"{s.category}"
        )
        out.append(
            f"    requested={'yes (a stop job)' if s.requested else 'no'}"
            f"{'  during start' if s.during_start and not s.boot_end else ''}  code={s.code or '-'}  "
            f"status={s.status or '-'}  Result={s.result or '-'}  watchdog={s.watchdog}  oom={s.oom}  "
            f"coredump={s.coredump}  restart-counter={s.nrestarts if s.nrestarts is not None else '-'}"
        )
        for label, value in (
            ("actor", s.actor),
            ("plan run", s.window),
            ("backup", s.backup_near),
            ("resources", s.consumed),
        ):
            if value:
                out.append(f"    {label}: {value}")
        out.append(
            "    journal lines before it (the unit's own log, and kernel lines):"
            if s.context
            else "    journal lines before it: none in the window"
        )
        out += [f"      {line}" for line in s.context]
    peaks = _peaks(unit, entries)
    out += ["", "### memory peaks systemd recorded at each stop (largest first)"]
    out += [
        f"  {_when(ts)} {body}" for _, body, ts in sorted(peaks, reverse=True)[:10]
    ] or ["  none recorded"]
    restarts = _unit_lines(health, entries, "restarting")
    out += [
        "",
        f"### health check restarts ({len(restarts)}): its own line, with the probe failure",
    ]
    out += [f"  {line}" for line in restarts] or ["  none"]
    failed = _unit_lines(health, entries, "Failed with result")
    out += [
        "",
        f"### health check runs that failed ({len(failed)}); the unit's own lines just before each",
    ]
    for line in failed[-10:]:
        out.append(f"  {line}")
    if len(failed) > 10:
        out.append(f"  ... {len(failed) - 10} earlier failures not listed")
    healthsays = [
        ln
        for ln in _unit_lines(health, entries, "agent-bus health:")
        if "restarting" not in ln
    ]
    out += ["", f"### other health check lines ({len(healthsays)}; last 20)"]
    out += [f"  {line}" for line in healthsays[-20:]] or ["  none"]
    b_runs = _unit_lines(backup, entries, "Starting ")
    b_fail = _unit_lines(backup, entries, "Failed with result")
    out += ["", f"### backups: {len(b_runs)} runs started, {len(b_fail)} failed"]
    out += [f"  {line}" for line in b_fail] or ["  no failed backup run"]
    return stops, len(restarts)


def analyse(
    entries: Sequence[Entry],
    boots: Sequence[Boot],
    windows: Sequence[Window],
    teams: Sequence[str],
) -> tuple[list[str], list[str]]:
    """The report's lines and the summary's."""
    found = sorted({team_of(e.unit) for e in entries if team_of(e.unit)} | set(teams))
    if not found:
        raise Failed(
            "no agent-bus-hs@ unit in the journal export and none installed: nothing to analyse"
        )
    report = [
        "Plan 00203 homeserver stop analysis (triage-hs.bash, hs_journal.py). Facts only; no verdict.",
        f"journal span: {_when(boots[0].first)} to {_when(boots[-1].last)}, {len(boots)} boots",
        f"plan-run windows considered: {len(windows)}",
        "categories: " + "; ".join(f"{k} = {v}" for k, v in CATEGORY_KIND.items()),
    ]
    summary = [
        f"journal span {_when(boots[0].first)} .. {_when(boots[-1].last)} ({len(boots)} boots)"
    ]
    for team in found:
        stops, restarts = _team_report(team, entries, boots, windows, report)
        kinds: dict[str, Counter[str]] = {
            k: Counter() for k in (DELIBERATE, NOT_DELIBERATE, UNKNOWN)
        }
        for s in stops:
            kinds[CATEGORY_KIND[s.category]][s.category] += 1
        peak = max(_peaks(f"agent-bus-hs@{team}.service", entries), default=None)
        summary.append(
            f"team {team}: {len(stops)} stops; "
            + "; ".join(
                f"{k} {sum(c.values())}"
                + (
                    f" ({', '.join(f'{n} {cat}' for cat, n in c.most_common())})"
                    if c
                    else ""
                )
                for k, c in kinds.items()
            )
            + f"; health restarts {restarts}; largest memory peak {peak[1].split(', ')[1] if peak else 'none recorded'}"
        )
    leaks = [ln for ln in report if _LEAK_RE.search(ln)]
    if leaks:
        raise Failed(
            f"redaction check FAILED on {len(leaks)} report lines; nothing was written"
        )
    return report, summary


def main(argv: Sequence[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument(
        "--journal",
        action="append",
        type=pathlib.Path,
        required=True,
        help="journalctl -o json export",
    )
    ap.add_argument(
        "--boots",
        type=pathlib.Path,
        required=True,
        help="journalctl --list-boots -o json",
    )
    ap.add_argument(
        "--runs-root", type=pathlib.Path, required=True, help="untracked/plan-runs"
    )
    ap.add_argument("--report", type=pathlib.Path, required=True)
    ap.add_argument(
        "--team", action="append", default=[], help="an installed team (repeatable)"
    )
    args = ap.parse_args(argv)
    try:
        entries = load_entries(args.journal)
        boots = load_boots(args.boots)
        report, summary = analyse(
            entries, boots, plan_run_windows(args.runs_root), args.team
        )
    except (Failed, OSError, KeyError, ValueError) as exc:
        print(f"hs_journal: {exc}", file=sys.stderr)
        return 1
    fd = os.open(args.report, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as fh:
        fh.write("\n".join(report) + "\n")
    print("\n".join(summary))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
