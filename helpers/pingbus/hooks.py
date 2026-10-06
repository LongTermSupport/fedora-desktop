"""The Claude Code hooks: `pingbus hook session-start|prompt|stop|session-end`.

Spec: docs/agent-bus-protocol.md §12 (the lock: a waker is a `watch` or `wait` holder,
found by a non-blocking `flock`, never by a PID), §13 (stdin hook JSON, stdout hook JSON,
always exit 0, fixed templates only), §15 (counts only). Plan 00161's DESIGN.md section 6.

Every hook is offline: it reads the bundle, the last verified team record, the inbox
(re-validated, and only ever counted) and probes each active team's lock. Nothing an
event, a stored file or an exception carried reaches the output: every text is one of
`TEMPLATES`, filled with counts.

- `session-start` starts `pingbus watch` detached when the session has an inbox socket and
  some active team has no waker; it reports the wake path, the pending count and any
  failure class.
- `prompt` (UserPromptSubmit) reports the pending count and any failure class.
- `stop` is the guard. It never blocks when `stop_hook_active` is true. It blocks once for
  a set of pending items (a new item blocks again), and otherwise, at most once per
  window, for a failure class or for an active team with no waker. The windows are kept in
  `PINGBUS_HOME/stop-guard.json`, so a guard that keeps blocking cannot loop against the
  hooks daemon's own Stop handlers.
- `session-end` does nothing: the watcher exits when the session's socket goes, which also
  keeps it running across a `/clear`, where SessionEnd fires and the session goes on.
"""

from __future__ import annotations

import dataclasses
import hashlib
import json
import os
import pathlib
import shutil
import subprocess
from collections.abc import Callable, Mapping
from typing import TextIO

from helpers.pingbus import config, inbox, notify, protocol

EVENTS = ("session-start", "prompt", "stop", "session-end")
HOOK_EVENT_NAMES = {
    "session-start": "SessionStart",
    "prompt": "UserPromptSubmit",
    "stop": "Stop",
    "session-end": "SessionEnd",
}
GUARD_FILE = "stop-guard.json"
WATCH_LOG = "watch.log"
NO_WAKER_EVERY_S = 600
FAILURE_EVERY_S = 600
INPUT_MAX_BYTES = 1 << 20

#: The only fields a template may carry: counts.
COUNT_FIELDS = ("total", "humans", "pings", "held", "teams", "free")
TEMPLATES = {
    "pending": "agent-bus: {total} pending ({humans} from humans, {pings} pings). Run `pingbus recv`.",
    "stop_pending": ("agent-bus: {total} pending ({humans} from humans, {pings} pings). "
                     "Run `pingbus recv` before stopping."),
    "no_waker": ("agent-bus: {free} of {teams} active teams have no watcher or waiter, so nothing "
                 "will wake this session. Run `pingbus wait` with run_in_background."),
    "watcher_started": "agent-bus: the wake path is the inbox socket; the watcher was started.",
    "seat_held": ("agent-bus: {held} of {teams} active teams already have a watcher or waiter "
                  "(this session's own, or another session holds the seat: run `pingbus status`)."),
    "no_socket": ("agent-bus: this session has no inbox socket. To be woken, run `pingbus wait` "
                  "with run_in_background."),
    "watcher_failed": ("agent-bus: the watcher could not be started. Run `pingbus status`; "
                       "meanwhile run `pingbus wait` with run_in_background."),
    "config": "agent-bus: the pingbus configuration was refused. Run `pingbus config check`.",
    "state": "agent-bus: a pingbus state file is unreadable. Run `pingbus status`.",
    "untrusted": "agent-bus: a team room is not trusted, or not verified yet. Run `pingbus status`.",
    "internal": "agent-bus: pingbus failed inside a hook. Run `pingbus status`.",
}
#: Failure classes, worst first: the one a hook names when several apply.
FAILURE_CLASSES = ("internal", "config", "state", "untrusted")


class _Failure(Exception):
    """A failure that ends a hook's survey, by class."""

    def __init__(self, cls: str) -> None:
        super().__init__(cls)
        self.cls = cls


def pending_text(total: int, humans: int, pings: int) -> str:
    return TEMPLATES["pending"].format(total=total, humans=humans, pings=pings)


# ── what the hooks look at ───────────────────────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class TeamView:
    """One active team as a hook sees it: the lock holder's kind, the valid pending items
    (None when the room is not trusted or the state is unreadable), a failure class."""

    team: str
    holder: str | None
    pending: inbox.Pending | None
    failure: str | None

    @property
    def waker(self) -> bool:
        return inbox.is_waker(self.holder)


@dataclasses.dataclass(frozen=True)
class Survey:
    teams: tuple[TeamView, ...]

    def counts(self) -> tuple[int, int, int]:
        total = humans = pings = 0
        for view in self.teams:
            if view.pending is not None:
                t, h, p = view.pending.counts()
                total, humans, pings = total + t, humans + h, pings + p
        return total, humans, pings

    def failure(self) -> str | None:
        found = {view.failure for view in self.teams if view.failure is not None}
        return next((cls for cls in FAILURE_CLASSES if cls in found), None)

    def digest(self) -> str:
        """Names the set of pending items, so the guard blocks once for each set."""
        ids = sorted(f"{view.team}:{item.event_id}" for view in self.teams if view.pending
                     for item in view.pending.items)
        return hashlib.sha256("\n".join(ids).encode("ascii")).hexdigest()

    def without_waker(self) -> int:
        return sum(1 for view in self.teams if not view.waker)


def _team_view(member: config.Member) -> TeamView:
    state = inbox.TeamState.for_member(member)
    try:
        holder = inbox.probe_lock(state)
    except inbox.StateError:
        return TeamView(member.team, None, None, "state")
    try:
        record = inbox.load_cached_record(member)
        pending = state.pending(record.context(member.server_name), member.user_id,
                                human_text=member.human_text)
    except protocol.Untrusted:
        return TeamView(member.team, holder, None, "untrusted")
    except inbox.StateError:
        return TeamView(member.team, holder, None, "state")
    return TeamView(member.team, holder, pending, None)


def survey(environ: Mapping[str, str]) -> Survey:
    """Every active team, offline. `_Failure` when the bundles themselves are refused."""
    try:
        members = config.load_active(environ)
    except (config.ConfigError, config.UsageError):
        raise _Failure("config") from None
    return Survey(tuple(_team_view(member) for member in members))


# ── starting the watcher ─────────────────────────────────────────────────────────────────


def watch_argv(environ: Mapping[str, str]) -> list[str]:
    """`pingbus watch`, by the `pingbus` on the session's PATH (the hooks run it so too)."""
    exe = shutil.which("pingbus", path=environ.get("PATH", ""))
    if exe is None:
        raise FileNotFoundError("pingbus is not on PATH")
    return [exe, "watch"]


def spawn_watcher(environ: Mapping[str, str], log_path: pathlib.Path) -> None:
    """Start `pingbus watch` detached, in its own session, with the hook's environment
    (which carries the inbox socket's variables); its stderr is appended to `log_path`."""
    argv = watch_argv(environ)
    fd = os.open(log_path, os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        subprocess.Popen(  # detached on purpose: it outlives the hook, until the socket goes
            argv, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=fd,
            env=dict(environ), start_new_session=True, close_fds=True,
        )
    finally:
        os.close(fd)


# ── the Stop guard's memory ──────────────────────────────────────────────────────────────


class _Guard:
    """`stop-guard.json` in PINGBUS_HOME: the pending set last blocked for, and when the
    guard last blocked for a failure and for no waker. A throttle, never an authority: a
    malformed file is reported and replaced. Without a home, nothing is remembered."""

    KEYS = ("pending", "no_waker_ms", "failure_ms")

    def __init__(self, path: pathlib.Path | None, values: dict[str, object]) -> None:
        self.path = path
        self.values = values

    @classmethod
    def load(cls, environ: Mapping[str, str], err: TextIO) -> _Guard:
        try:
            home = config.resolve_home(environ)
        except config.ConfigError:
            return cls(None, dict.fromkeys(cls.KEYS))
        path = home / GUARD_FILE
        try:
            data = inbox.read_json_file(path)
        except inbox.StateError as exc:
            err.write(f"pingbus: hook stop: replacing the guard file: {exc}\n")
            data = None
        if data is not None and not cls._valid(data):
            err.write(f"pingbus: hook stop: replacing the guard file: {path} is malformed\n")
            data = None
        values = dict.fromkeys(cls.KEYS) if data is None else {k: data[k] for k in cls.KEYS}
        return cls(path, values)

    @classmethod
    def _valid(cls, data: object) -> bool:
        if not isinstance(data, dict) or set(data) != {"v", *cls.KEYS} or data["v"] != 1:
            return False
        pending = data["pending"]
        if pending is not None and not (isinstance(pending, str) and len(pending) == 64):
            return False
        return all(data[k] is None or (type(data[k]) is int and data[k] >= 0)
                   for k in ("no_waker_ms", "failure_ms"))

    def save(self) -> None:
        if self.path is None or not self.path.parent.is_dir():
            return
        inbox.write_json_file(self.path, {"v": 1, **self.values})

    def allow(self, key: str, now_ms: int, every_s: int) -> bool:
        """Whether the window for `key` has passed; if so, it starts again now."""
        last = self.values[key]
        if last is not None and 0 <= now_ms - last < every_s * 1000:
            return False
        self.values[key] = now_ms
        self.save()
        return True


# ── the hooks ────────────────────────────────────────────────────────────────────────────


def _context(event: str, lines: list[str]) -> dict:
    if not lines:
        return {}
    return {"hookSpecificOutput": {"hookEventName": HOOK_EVENT_NAMES[event],
                                   "additionalContext": "\n".join(lines)}}


def _block(text: str) -> dict:
    return {"decision": "block", "reason": text}


def _parse_input(raw: bytes) -> dict | None:
    if len(raw) > INPUT_MAX_BYTES:
        return None
    try:
        data = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None
    return data if isinstance(data, dict) else None


@dataclasses.dataclass
class _Hook:
    event: str
    payload: dict | None
    environ: Mapping[str, str]
    now_ms: int
    spawn: Callable[[Mapping[str, str], pathlib.Path], object]
    err: TextIO

    def session_start(self) -> dict:
        surveyed = survey(self.environ)
        lines: list[str] = []
        try:
            session = notify.session_socket(self.environ)
        except ValueError as exc:
            self.err.write(f"pingbus: hook session-start: {exc}\n")
            session = None
        teams = len(surveyed.teams)
        held = teams - surveyed.without_waker()
        if session is None:
            lines.append(TEMPLATES["no_socket"])
        elif held < teams:
            try:
                self.spawn(self.environ, config.resolve_home(self.environ) / WATCH_LOG)
            except OSError as exc:
                self.err.write(f"pingbus: hook session-start: the watcher did not start: {exc.strerror or exc}\n")
                lines.append(TEMPLATES["watcher_failed"])
            else:
                lines.append(TEMPLATES["watcher_started"])
        if held:
            lines.append(TEMPLATES["seat_held"].format(held=held, teams=teams))
        lines += self._pending_and_failure(surveyed)
        return _context(self.event, lines)

    def prompt(self) -> dict:
        return _context(self.event, self._pending_and_failure(survey(self.environ)))

    def _pending_and_failure(self, surveyed: Survey) -> list[str]:
        lines = []
        total, humans, pings = surveyed.counts()
        if total:
            lines.append(pending_text(total, humans, pings))
        failure = surveyed.failure()
        if failure is not None:
            lines.append(TEMPLATES[failure])
        return lines

    def stop(self) -> dict:
        if self.payload is None or self.payload.get("stop_hook_active") is not False:
            return {}
        guard = _Guard.load(self.environ, self.err)
        try:
            surveyed = survey(self.environ)
        except _Failure as failure:
            return self.stop_failure(failure.cls, guard)
        total, humans, pings = surveyed.counts()
        if total:
            digest = surveyed.digest()
            if guard.values["pending"] != digest:
                guard.values["pending"] = digest
                guard.save()
                return _block(TEMPLATES["stop_pending"].format(total=total, humans=humans, pings=pings))
        elif guard.values["pending"] is not None:
            guard.values["pending"] = None
            guard.save()
        failure = surveyed.failure()
        if failure is not None:
            return self.stop_failure(failure, guard)
        free = surveyed.without_waker()
        if free and guard.allow("no_waker_ms", self.now_ms, NO_WAKER_EVERY_S):
            return _block(TEMPLATES["no_waker"].format(free=free, teams=len(surveyed.teams)))
        return {}

    def stop_failure(self, cls: str, guard: _Guard | None = None) -> dict:
        if self.payload is None or self.payload.get("stop_hook_active") is not False:
            return {}
        guard = guard or _Guard.load(self.environ, self.err)
        if guard.allow("failure_ms", self.now_ms, FAILURE_EVERY_S):
            return _block(TEMPLATES[cls])
        return {}

    def failed(self, cls: str) -> dict:
        if self.event == "stop":
            return self.stop_failure(cls)
        if self.event == "session-end":
            return {}
        return _context(self.event, [TEMPLATES[cls]])


def run(event: str, raw: bytes, environ: Mapping[str, str], *, now_ms: int,
        spawn: Callable[[Mapping[str, str], pathlib.Path], object], err: TextIO) -> dict:
    """One hook's output object. Never raises: a failure becomes its class's template."""
    if event not in EVENTS:
        raise ValueError(f"not a hook: {event!r}")
    hook = _Hook(event, _parse_input(raw), environ, now_ms, spawn, err)
    try:
        if event == "session-end":
            return {}
        if event == "session-start":
            return hook.session_start()
        if event == "prompt":
            return hook.prompt()
        return hook.stop()
    except _Failure as failure:
        return hook.failed(failure.cls)
    except Exception as exc:  # every hook must answer: an unexpected error is the `internal` template
        err.write(f"pingbus: hook {event}: internal error ({type(exc).__name__})\n")
        return hook.failed("internal")
