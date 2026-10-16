"""A team homeserver's health: the root check behind `agent-bus-hs-health@<team>.timer` and
`agent-bus health status`.

Plan 00203 gaps 4 and 10. systemd's watchdog (`WatchdogSec=` in agent-bus-hs@.service) restarts
a Tuwunel that stops pinging it; it cannot see one whose runtime still ticks while its client API
has stopped answering. So every minute `health check --team <team>`, as root:

  1. probes `http://127.0.0.1:<port>/_matrix/client/versions` of a homeserver that is active and
     has been up for START_GRACE, PROBE_TRIES times PROBE_GAP apart; when every probe of the run
     misses and the unit is still the same invocation, it restarts the unit and logs why;
  2. notes the restarts systemd made itself (NRestarts rose: a watchdog abort, a crash, an exit);
  3. judges the backups: the backup service failed, the newest usable backup (a database backup
     with its state tar, what `agent-bus-install backups` lists) older than BACKUP_MAX_AGE, none
     at all once the daily timer has run, or the timer itself stopped;
  4. writes what it found to `/var/lib/agent-bus-applied/health/<team>.json` (0640 in a 0750
     directory, root and the stamps' group, like the applied stamps: no secret), with the time
     it stops being current, so a check that stops running is itself seen.

`ccy-teams` shows the record's problems on the team's row; `agent-bus health status [TEAM...]`,
run as yourself, prints them with the homeserver's live state and the sign-in limits.

A per-account sign-in lockout cannot be read: Tuwunel 1.9.4 keeps its sign-in buckets in memory
(src/service/login_ratelimit) and logs a refusal only at debug level, which the release build
compiles out. It offers no reset; a restart empties every account's buckets, which would weaken
the limit for all of them, so none is offered here. The status says the limits and the wait.

Streams: `check` logs to stderr (the journal); `status` prints its report on stdout (its
payload) and exits EXIT_PROBLEM when a team has a problem.
"""

from __future__ import annotations

import dataclasses
import grp
import json
import os
import pathlib
import subprocess
import sys
import time
import urllib.error
import urllib.request
from collections.abc import Callable, Iterable, Mapping, Sequence

from helpers.agent_bus import checkout, render, teamfile
from helpers.pingbus import protocol

HEALTH_DIR = checkout.APPLIED_DIR / "health"
STAMP_GROUP_FILE = pathlib.Path("/etc/agent-bus/stamp-group")
INSTALL_STATE_ROOT = pathlib.Path("/var/lib/agent-bus-install")
STATE_ROOT = pathlib.Path(render.STATE_ROOT)
RECORD_VERSION = 1
DIR_MODE = 0o750
FILE_MODE = 0o640

#: The probe: three misses, ten seconds apart, each allowed five seconds, before a restart.
PROBE_TRIES = 3
PROBE_GAP = 10.0
PROBE_TIMEOUT = 5.0
#: A homeserver is probed once it has been active this long (it is ready when active, but a
#: restart loop must not be fed by the check itself).
START_GRACE = 60.0
#: The daily backup timer fires within an hour of midnight; a day and a half is late.
BACKUP_MAX_AGE = 36 * 3600.0
#: How long a restart stays on the board.
EVENT_KEEP = 24 * 3600.0
#: The check runs every minute; a record older than this says the check has stopped.
RECORD_LIFETIME = 300
EXIT_PROBLEM = 1

#: What `systemctl show` is asked of each unit: only what that unit's type has, since systemctl
#: leaves out any other property and parse_show refuses a missing one.
_HS_SHOW = ("ActiveState", "SubState", "Result", "InvocationID", "ActiveEnterTimestampMonotonic", "NRestarts")
_BACKUP_SHOW = ("ActiveState", "Result")
_TIMER_SHOW = ("ActiveState", "LastTriggerUSec", "NextElapseUSecRealtime")


class HealthError(Exception):
    """The check could not do its job, or a record cannot be read (exit 78)."""


@dataclasses.dataclass(frozen=True)
class Problem:
    what: str
    fix: str


@dataclasses.dataclass(frozen=True)
class Backup:
    id: int
    time: float


def hs_unit(team: str) -> str:
    return checkout.hs_unit(team)


def backup_service(team: str) -> str:
    return f"agent-bus-backup@{team}.service"


def backup_timer(team: str) -> str:
    return f"agent-bus-backup@{team}.timer"


def health_timer(team: str) -> str:
    return f"agent-bus-hs-health@{team}.timer"


def health_service(team: str) -> str:
    return f"agent-bus-hs-health@{team}.service"


# ── words (pure) ─────────────────────────────────────────────────────────────────────────


def age_words(seconds: float) -> str:
    seconds = max(0, int(seconds))
    if seconds < 120:
        return f"{seconds} s"
    if seconds < 3600:
        return f"{round(seconds / 60)} min"
    if seconds < 86400:
        return f"{seconds // 3600} h"
    return f"{seconds // 86400} d {seconds % 86400 // 3600} h"


def when_words(at: float) -> str:
    return time.strftime("%Y-%m-%d %H:%M:%S UTC", time.gmtime(at))


def login_limits_text() -> str:
    """The per-account sign-in limits (render.LOGIN_LIMITS), what a refusal looks like and what
    to do about it."""
    failed, account = render.LOGIN_LIMITS["failed"], render.LOGIN_LIMITS["account"]

    def every(limit: Mapping[str, float]) -> str:
        return f"one every {round(1 / limit['per_second'] / 60)} min"

    return (f"sign-in limits per person: {failed['burst_count']} wrong passwords, then {every(failed)}; "
            f"{account['burst_count']} sign-ins, then {every(account)}. Element then says \"Too many login "
            "attempts for this account\"; it clears by itself (Element shows the wait), retrying does not "
            "lengthen it, and the limit is never lifted early")


# ── what the facts mean (pure) ───────────────────────────────────────────────────────────


def unix_time(value: str) -> float | None:
    """`systemctl show --timestamp=unix`'s `@<seconds>`; None for `n/a` or empty (never)."""
    if value.startswith("@") and value[1:].isdigit():
        return float(value[1:])
    return None


def newest_backup(meta_names: Iterable[str], tar_times: Mapping[int, float]) -> Backup | None:
    """The highest numbered database backup that has its state tar: what restore can use."""
    ids = [int(name) for name in meta_names if name.isdigit() and not name.startswith("0")]
    usable = [number for number in ids if number in tar_times]
    if not usable:
        return None
    best = max(usable)
    return Backup(best, tar_times[best])


def backup_problems(team: str, newest: Backup | None, service: Mapping[str, str],
                    timer: Mapping[str, str], now: float) -> list[Problem]:
    svc = backup_service(team)
    retry = f"sudo systemctl start {svc}, then journalctl -u {svc} -n 40 --no-pager if it fails"
    problems = []
    if timer["ActiveState"] != "active":
        problems.append(Problem(f"the daily backup timer {backup_timer(team)} is {timer['ActiveState']}",
                                f"ccy-teams apply {team} enables and starts it"))
    if service["ActiveState"] == "failed" or service["Result"] not in ("success", ""):
        problems.append(Problem(f"the last backup failed ({service['Result']})",
                                f"journalctl -u {svc} -n 40 --no-pager says why; then sudo systemctl start {svc}"))
    if newest is None:
        if unix_time(timer["LastTriggerUSec"]) is not None:
            problems.append(Problem("no backup exists, though the daily backup has run", retry))
    elif now - newest.time > BACKUP_MAX_AGE:
        problems.append(Problem(
            f"the newest backup is {age_words(now - newest.time)} old (#{newest.id}, {when_words(newest.time)})",
            retry))
    return problems


def restart_events(previous: Mapping | None, hs: Mapping[str, str], now: float) -> tuple[list[dict], int]:
    """The restarts to keep on the board, with systemd's own (NRestarts rose since the last
    record) added, and the count to remember. A lower count is a restart by hand."""
    events = [dict(event) for event in (previous or {}).get("events", ()) if now - event["at"] < EVENT_KEEP]
    count = int(hs["NRestarts"] or 0)
    before = (previous or {}).get("nrestarts", 0)
    if count > before:
        times = "once" if count == 1 else f"{count} times"
        events.append({"at": int(now), "what": f"systemd restarted the homeserver {times} since it was last "
                                              f"started by hand (last cause: {hs['Result']})"})
    return events, count


def event_problems(team: str, events: Sequence[Mapping]) -> list[Problem]:
    unit = hs_unit(team)
    return [Problem(f"{event['what']}, at {when_words(event['at'])}",
                    f"journalctl -u {unit} --since '{when_words(event['at'] - 300)}' --no-pager says why")
            for event in events]


def build_record(team: str, now: float, problems: Sequence[Problem], newest: Backup | None,
                 due: float | None, events: Sequence[Mapping], nrestarts: int) -> dict:
    return {
        "v": RECORD_VERSION, "team": team, "checked": int(now), "expires": int(now) + RECORD_LIFETIME,
        "problems": [dataclasses.asdict(problem) for problem in problems],
        "backup": None if newest is None else {"id": newest.id, "time": int(newest.time)},
        "backup_due": None if due is None else int(due),
        "events": list(events), "nrestarts": nrestarts,
    }


def parse_record(text: str, team: str) -> dict:
    try:
        data = json.loads(text)
    except ValueError as exc:
        raise HealthError(f"the health record of team {team} is not JSON: {exc}") from None
    keys = {"v", "team", "checked", "expires", "problems", "backup", "backup_due", "events", "nrestarts"}
    if not isinstance(data, dict) or set(data) != keys or data["v"] != RECORD_VERSION or data["team"] != team:
        raise HealthError(f"the health record of team {team} is not one this agent-bus writes")
    return data


def status_report(team: str, hs: Mapping[str, str], record: Mapping | None, now: float) -> tuple[list[str], int]:
    """`agent-bus health status` lines for one team and how many problems it has."""
    state, sub = hs["ActiveState"], hs["SubState"]
    lines = [f"{team}: homeserver {state} ({sub})"]
    problems: list[Problem] = []
    if state != "active":
        problems.append(Problem(f"the homeserver is {state} ({sub})",
                                f"journalctl -u {hs_unit(team)} -n 40 --no-pager says why; "
                                f"ccy-teams apply {team} starts it"))
    if record is None:
        problems.append(Problem("the health check has never run",
                                f"ccy-teams apply {team} installs and starts {health_timer(team)}"))
    else:
        if now > record["expires"]:
            problems.append(Problem(f"the health check last ran {age_words(now - record['checked'])} ago",
                                    f"systemctl status {health_timer(team)} {health_service(team)} --no-pager"))
        problems += [Problem(item["what"], item["fix"]) for item in record["problems"]]
        backup = record["backup"]
        if backup is not None:
            lines.append(f"{team}: newest backup #{backup['id']}, {when_words(backup['time'])} "
                         f"({age_words(now - backup['time'])} ago)")
        elif record["backup_due"] is not None:
            lines.append(f"{team}: no backup yet; the first is due {when_words(record['backup_due'])}")
    for problem in problems:
        lines += [f"{team}: PROBLEM {problem.what}", f"{team}:   do: {problem.fix}"]
    lines.append(f"{team}: {login_limits_text()}")
    return lines, len(problems)


# ── the executor ─────────────────────────────────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class HealthSystem:
    """The host's side: systemd, the client API, the clocks, the log and the paths."""

    #: `systemctl show --timestamp=unix` of these properties of a unit.
    show: Callable[[str, Sequence[str]], dict[str, str]]
    #: Whether a GET of the URL answered 2xx within the timeout.
    get: Callable[[str, float], bool]
    restart: Callable[[str], None]
    sleep: Callable[[float], None]
    now: Callable[[], float]
    monotonic: Callable[[], float]
    say: Callable[[str], None]
    state_root: pathlib.Path
    install_root: pathlib.Path
    health_dir: pathlib.Path
    #: The stamps' group (play-agent-bus.yml records it), the record's group.
    group_id: Callable[[], int]
    #: The teams built here (their applied stamps).
    teams_here: Callable[[], Sequence[str]]


def _port(team: str, system: HealthSystem) -> int:
    path = system.state_root / team / "team.json"
    try:
        port = json.loads(path.read_text(encoding="utf-8"))["port"]
    except (OSError, ValueError, KeyError, TypeError) as exc:
        raise HealthError(f"team {team}: {path} gives no port: {exc}") from None
    if type(port) is not int:
        raise HealthError(f"team {team}: {path} gives no port")
    return port


def _answers(url: str, system: HealthSystem) -> bool:
    for attempt in range(PROBE_TRIES):
        if attempt:
            system.sleep(PROBE_GAP)
        if system.get(url, PROBE_TIMEOUT):
            return True
    return False


def _liveness(team: str, system: HealthSystem, hs: Mapping[str, str], now: float) -> list[dict]:
    """A restart event when the client API missed every probe of this run."""
    if (hs["ActiveState"], hs["SubState"]) != ("active", "running"):
        return []
    up_for = system.monotonic() - int(hs["ActiveEnterTimestampMonotonic"] or 0) / 1e6
    if up_for < START_GRACE:
        return []
    url = f"http://127.0.0.1:{_port(team, system)}/_matrix/client/versions"
    if _answers(url, system):
        return []
    unit = hs_unit(team)
    again = system.show(unit, _HS_SHOW)
    if again["InvocationID"] != hs["InvocationID"] or again["ActiveState"] != "active":
        system.say(f"{unit}: {url} did not answer, but the unit restarted meanwhile; leaving it")
        return []
    system.say(f"{unit}: {url} missed {PROBE_TRIES} probes {PROBE_GAP:.0f} s apart; restarting {unit}")
    system.restart(unit)
    return [{"at": int(now), "what": f"restarted by the health check: the client API missed {PROBE_TRIES} "
                                    f"probes in {PROBE_GAP * (PROBE_TRIES - 1):.0f} s"}]


def _tar_times(team: str, system: HealthSystem) -> dict[int, float]:
    tars = system.install_root / team / "backups"
    found = {}
    if tars.is_dir():
        for path in tars.glob("agent-bus-state-*.tar"):
            number = path.name.removeprefix("agent-bus-state-").removesuffix(".tar")
            if number.isdigit():
                found[int(number)] = path.stat().st_mtime
    return found


def _meta_names(team: str, system: HealthSystem) -> list[str]:
    meta = system.state_root / team / "backups" / "meta"
    return [path.name for path in meta.iterdir()] if meta.is_dir() else []


def _read_record(team: str, system: HealthSystem) -> dict | None:
    path = system.health_dir / f"{team}.json"
    try:
        text = path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return None
    except PermissionError:
        raise HealthError(f"{path} cannot be read: run agent-bus health as the desktop user, a member of "
                          f"the group named in {STAMP_GROUP_FILE}") from None
    return parse_record(text, team)


def _write_record(team: str, record: Mapping, system: HealthSystem) -> None:
    """The record replaced atomically: the directory 0750 and the file 0640, root's and the
    stamps' group's."""
    gid = system.group_id()
    directory = system.health_dir
    if not directory.parent.is_dir():
        raise HealthError(f"{directory.parent} is missing: agent-bus-install team makes it (ccy-teams apply)")
    directory.mkdir(mode=DIR_MODE, exist_ok=True)
    if directory.is_symlink() or not directory.is_dir():
        raise HealthError(f"{directory} is not a directory")
    os.chown(directory, -1, gid)
    os.chmod(directory, DIR_MODE)
    final = directory / f"{team}.json"
    tmp = directory / f".{team}.json.new"
    tmp.unlink(missing_ok=True)
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, FILE_MODE)
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        os.fchown(handle.fileno(), -1, gid)
        os.fchmod(handle.fileno(), FILE_MODE)
        handle.write(json.dumps(record, sort_keys=True) + "\n")
    os.replace(tmp, final)


def check(team: str, system: HealthSystem) -> None:
    """One run of the health check for `team` (as root, from its timer)."""
    now = system.now()
    previous = None
    try:
        previous = _read_record(team, system)
    except HealthError as exc:
        system.say(f"the last record is unreadable and is replaced: {exc}")
    hs = system.show(hs_unit(team), _HS_SHOW)
    events, nrestarts = restart_events(previous, hs, now)
    events += _liveness(team, system, hs, now)
    timer = system.show(backup_timer(team), _TIMER_SHOW)
    newest = newest_backup(_meta_names(team, system), _tar_times(team, system))
    problems = event_problems(team, events)
    problems += backup_problems(team, newest, system.show(backup_service(team), _BACKUP_SHOW), timer, now)
    for problem in problems:
        system.say(f"team {team}: {problem.what} (do: {problem.fix})")
    record = build_record(team, now, problems, newest, unix_time(timer["NextElapseUSecRealtime"]), events,
                          nrestarts)
    _write_record(team, record, system)


def status(teams: Sequence[str], system: HealthSystem) -> tuple[list[str], int]:
    """`agent-bus health status`: every named team, or every team built here."""
    here = list(system.teams_here())
    for team in teams:
        if not protocol.is_team_name(team) or team not in here:
            raise HealthError(f"team {team} is not built on this host; teams here: {', '.join(here) or 'none'}")
    lines: list[str] = []
    problems = 0
    for team in teams or here:
        hs = system.show(hs_unit(team), ("ActiveState", "SubState"))
        team_lines, count = status_report(team, hs, _read_record(team, system), system.now())
        lines += team_lines
        problems += count
    return lines, problems


# ── the real host ────────────────────────────────────────────────────────────────────────


def _systemctl(*args: str) -> str:
    try:
        run = subprocess.run(["systemctl", *args], capture_output=True, text=True, check=False)
    except FileNotFoundError:
        raise HealthError("systemctl is not installed") from None
    if run.returncode != 0:
        raise HealthError(f"systemctl {' '.join(args)} failed (exit {run.returncode}): {run.stderr.strip()}")
    return run.stdout


def _show(unit: str, props: Sequence[str]) -> dict[str, str]:
    return parse_show(unit, props, _systemctl("show", "--timestamp=unix",
                                              *(f"--property={prop}" for prop in props), unit))


def parse_show(unit: str, props: Sequence[str], out: str) -> dict[str, str]:
    """`systemctl show`'s answer; systemctl leaves out, without an error, a property the unit's
    type does not have, so a missing one is refused here by name."""
    found = dict(line.split("=", 1) for line in out.splitlines() if "=" in line)
    missing = [prop for prop in props if prop not in found]
    if missing:
        raise HealthError(f"systemctl show {unit} gave no {', '.join(missing)}")
    return found


def _get(url: str, timeout: float) -> bool:
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(url, timeout=timeout) as response:
            return 200 <= response.status < 300
    except (urllib.error.URLError, OSError, ValueError):
        return False


def _group_id() -> int:
    try:
        return grp.getgrnam(teamfile.read_stamp_group(STAMP_GROUP_FILE, "root")).gr_gid
    except teamfile.StampGroupError as exc:
        raise HealthError(str(exc)) from None


def _say(text: str) -> None:
    print(f"agent-bus health: {text}", file=sys.stderr, flush=True)


def _restart(unit: str) -> None:
    _systemctl("restart", unit)


def real_system() -> HealthSystem:
    return HealthSystem(show=_show, get=_get, restart=_restart,
                        sleep=time.sleep, now=time.time, monotonic=time.monotonic, say=_say,
                        state_root=STATE_ROOT, install_root=INSTALL_STATE_ROOT, health_dir=HEALTH_DIR,
                        group_id=_group_id, teams_here=checkout._stamped_teams)
