"""ccy-lifecycle: the session-lifetime plugin for the hooks-daemon ccy supervisor.

Shipped in the ccy image at /opt/claude-yolo/supervisor-plugins/ccy_lifecycle.py, outside
the project bind mount, so a project can neither carry nor tamper with it. The entrypoint
adds it to the supervisor's wrapper line (`--plugin ccy-lifecycle=<this file>`) only when
at least one feature below is configured; otherwise the wrapper line is untouched.

Two independent features, both off unless configured:

  maximum session age  The session is restarted on a fresh container (and so on whatever
                       Claude Code version the image now holds) once it has run for the
                       configured time. A RESTART_SOON notice goes out `warn minutes`
                       before; at the age the plugin asks the supervisor to exit for a
                       restart. The supervisor calls on_idle only at an idle point with an
                       empty input box, so the restart never lands mid-turn. There is no
                       forced restart: a session that never idles restarts at its first
                       idle after the age.

  deadline             A DEADLINE_REACHED notice goes out once, at the absolute deadline.
                       The plugin never ends the session for a deadline. Once the notice
                       has gone out, the maximum-age restart stands down: a session that
                       has been told to wrap up is not restarted into a fresh container.

Configuration arrives in the environment (the ccy launcher passes it through):

  CCY_LIFECYCLE_LAUNCH_ID          one value per container launch; required when a feature
                                   is on. A change means a new container: the age clock
                                   restarts. The same value across a worker hot reload
                                   keeps it.
  CCY_LIFECYCLE_MAX_AGE_SECONDS    1800 to 2592000. Absent or empty: no maximum age.
  CCY_LIFECYCLE_DEADLINE_EPOCH     absolute deadline, epoch seconds. Absent: no deadline.
  CCY_LIFECYCLE_WARN_MINUTES       1 to 240, default 10, and shorter than the maximum age.

Anything invalid raises ValueError from on_start. The supervisor disables a plugin that
raises and tells the session so, which is the loud failure wanted here: a typo must not
quietly turn the feature off.

The persisted state lives in the plugin's private state directory. That directory belongs to
the PROJECT, not the container: it survives a container restart and a worker hot reload, and
every concurrent ccy container of the project shares it. So nothing in it is keyed by the
project alone:

  lifecycle-<launch id>.json        this container's age clock, warning and deadline flags.
                                    A hot reload finds it again; a new container (a new
                                    launch id) never sees it, so the age clock restarts.
  deadline-<epoch>-<session>.json   "this deadline was announced to this Claude session".
                                    The relaunch after a restart resumes the same session
                                    id, so it is not told twice; a concurrent session with
                                    the same deadline has a different id and is told too.
                                    <session> is a hash, so no session id is written down.

Each start removes files of both shapes, and leftovers of an interrupted save, that have not
been written for PRUNE_AFTER_SECONDS. That is longer than any session the plugin restarts
can live, and a live container rewrites its own file on every worker start.

Standard library only, as the supervisor's plugin contract requires.
"""

from __future__ import annotations

import hashlib
import json
import math
import os
import re
import stat
import time
from collections.abc import Callable, Mapping
from pathlib import Path
from typing import Any

PLUGIN_API = 1

PLUGIN_NAME = "ccy-lifecycle"
PLUGIN_VERSION = "1.0.0"

ENV_LAUNCH_ID = "CCY_LIFECYCLE_LAUNCH_ID"
ENV_MAX_AGE = "CCY_LIFECYCLE_MAX_AGE_SECONDS"
ENV_DEADLINE = "CCY_LIFECYCLE_DEADLINE_EPOCH"
ENV_WARN_MINUTES = "CCY_LIFECYCLE_WARN_MINUTES"

MAX_AGE_MIN_SECONDS = 30 * 60
MAX_AGE_MAX_SECONDS = 30 * 24 * 3600
WARN_MINUTES_DEFAULT = 10
WARN_MINUTES_MIN = 1
WARN_MINUTES_MAX = 240

STATE_FILE_PREFIX = "lifecycle-"
DEADLINE_MARKER_PREFIX = "deadline-"
STATE_SUFFIX = ".json"
STATE_FILE_MODE = 0o600
# Twice the longest session the plugin restarts, so a live container's file is never pruned.
PRUNE_AFTER_SECONDS = 2 * MAX_AGE_MAX_SECONDS
SESSION_KEY_HEX_CHARS = 32

_DIGITS = re.compile(r"[0-9]+")
_LAUNCH_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{7,63}")
# Every name this plugin writes, including the temporary file of an interrupted save. Pruning
# matches against this and nothing else, so a file another writer put here is never removed.
_OWN_FILE = re.compile(
    r"(?:lifecycle-[A-Za-z0-9][A-Za-z0-9._-]{7,63}|deadline-[0-9]+-[0-9a-f]{32})"
    r"\.json(?:\.tmp\.[0-9]+)?"
)


def _optional_int(environ: Mapping[str, str], name: str) -> int | None:
    raw = environ.get(name, "")
    if raw == "":
        return None
    if not _DIGITS.fullmatch(raw):
        raise ValueError(f"{name} must be a whole number, got {raw!r}")
    return int(raw)


def _describe_duration(seconds: int) -> str:
    days, rest = divmod(seconds, 86400)
    hours, rest = divmod(rest, 3600)
    minutes = rest // 60
    parts = []
    if days:
        parts.append(f"{days}d")
    if hours:
        parts.append(f"{hours}h")
    if minutes or not parts:
        parts.append(f"{minutes}m")
    return "".join(parts)


class Config:
    """The validated configuration of an enabled plugin (at least one feature is on).

    Plain classes, not dataclasses. The supervisor's loader registers this module in
    sys.modules before executing it, so dataclasses would work there; but the unit tests
    (tests/helpers/ccy_lifecycle/test_plugin.py, load_plugin_module) import a fresh copy
    per test WITHOUT registering it, and a dataclass under postponed annotations fails to
    build when its module is missing from sys.modules.
    """

    def __init__(self, launch_id: str, max_age: int | None, deadline: int | None, warn_minutes: int) -> None:
        self.launch_id = launch_id
        self.max_age = max_age
        self.deadline = deadline
        self.warn_minutes = warn_minutes


def parse_config(environ: Mapping[str, str]) -> Config | None:
    """Validate the environment. None means no feature is configured; ValueError means a bad value."""
    max_age = _optional_int(environ, ENV_MAX_AGE)
    deadline = _optional_int(environ, ENV_DEADLINE)
    warn = _optional_int(environ, ENV_WARN_MINUTES)
    warn_minutes = WARN_MINUTES_DEFAULT if warn is None else warn

    if max_age is not None and not (MAX_AGE_MIN_SECONDS <= max_age <= MAX_AGE_MAX_SECONDS):
        raise ValueError(
            f"{ENV_MAX_AGE} must be between {MAX_AGE_MIN_SECONDS} and "
            f"{MAX_AGE_MAX_SECONDS} seconds, got {max_age}"
        )
    if deadline is not None and deadline <= 0:
        raise ValueError(f"{ENV_DEADLINE} must be a positive epoch time, got {deadline}")
    if not (WARN_MINUTES_MIN <= warn_minutes <= WARN_MINUTES_MAX):
        raise ValueError(
            f"{ENV_WARN_MINUTES} must be between {WARN_MINUTES_MIN} and "
            f"{WARN_MINUTES_MAX}, got {warn_minutes}"
        )
    if max_age is not None and warn_minutes * 60 >= max_age:
        raise ValueError(
            f"{ENV_WARN_MINUTES} ({warn_minutes}) must be shorter than the maximum age "
            f"({_describe_duration(max_age)})"
        )

    if max_age is None and deadline is None:
        return None
    launch_id = environ.get(ENV_LAUNCH_ID, "")
    if not _LAUNCH_ID.fullmatch(launch_id):
        raise ValueError(
            f"{ENV_LAUNCH_ID} must be set by the launcher to an id of 8 to 64 safe "
            f"characters, got {launch_id!r}"
        )
    return Config(launch_id=launch_id, max_age=max_age, deadline=deadline, warn_minutes=warn_minutes)


class State:
    """One container's state (lifecycle-<launch id>.json): it survives a worker hot reload."""

    def __init__(
        self,
        launch_id: str,
        started_at: float,
        restart_warned: bool = False,
        deadline_notified: int | None = None,
    ) -> None:
        self.launch_id = launch_id
        self.started_at = started_at
        self.restart_warned = restart_warned
        self.deadline_notified = deadline_notified

    def to_json(self) -> str:
        return json.dumps(
            {
                "launch_id": self.launch_id,
                "started_at": self.started_at,
                "restart_warned": self.restart_warned,
                "deadline_notified": self.deadline_notified,
            },
            sort_keys=True,
        )

    @classmethod
    def from_json(cls, raw: str, path: Path) -> State:
        try:
            data: object = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise ValueError(f"the lifecycle state at {path} is not valid JSON: {exc}") from exc
        if not isinstance(data, dict):
            raise ValueError(f"the lifecycle state at {path} has unexpected contents")
        launch = data.get("launch_id")
        started = data.get("started_at")
        warned = data.get("restart_warned", False)
        notified = data.get("deadline_notified")
        if (
            not isinstance(launch, str)
            or isinstance(started, bool)
            or not isinstance(started, (int, float))
            or not isinstance(warned, bool)
            or isinstance(notified, bool)
            or not (notified is None or isinstance(notified, int))
        ):
            raise ValueError(f"the lifecycle state at {path} has unexpected contents")
        return cls(
            launch_id=launch,
            started_at=float(started),
            restart_warned=warned,
            deadline_notified=notified,
        )


class _Active:
    """A configured, started plugin: both parts exist together or not at all."""

    def __init__(self, config: Config, state: State) -> None:
        self.config = config
        self.state = state


class LifecycleHalf:
    name = PLUGIN_NAME
    version = PLUGIN_VERSION

    def __init__(
        self,
        api: Any,
        environ: Mapping[str, str] | None = None,
        clock: Callable[[], float] | None = None,
    ) -> None:
        self._api = api
        self._environ: Mapping[str, str] = os.environ if environ is None else environ
        self._clock: Callable[[], float] = time.time if clock is None else clock
        # None until on_start has run and found a feature configured. on_idle then does
        # nothing, which is the whole of "off".
        self._active: _Active | None = None
        self._restart_audited = False

    # -- supervisor hooks ---------------------------------------------------------------

    def on_start(self) -> None:
        """Validate the configuration and load or create the persisted state.

        Runs on every worker start, including a hot reload, so it must be idempotent: the
        same launch id keeps the stored start time and flags.
        """
        self._active = None
        config = parse_config(self._environ)
        if config is None:
            return
        state = self._load_or_create_state(config)
        self._active = _Active(config=config, state=state)
        self._api.audit(
            f"started: launch {config.launch_id}, session clock began at {state.started_at:.0f}, "
            f"max age {'off' if config.max_age is None else _describe_duration(config.max_age)}, "
            f"deadline {'none' if config.deadline is None else config.deadline}"
        )

    def on_idle(self, tick: Any) -> Any:
        active = self._active
        if active is None:
            return None
        now = float(tick.now)
        session_id: object = tick.session_id
        if session_id is not None and not isinstance(session_id, str):
            raise TypeError(f"tick.session_id must be a str or None, got {type(session_id)}")
        deadline_result = self._deadline_result(active, now, session_id or None)
        if deadline_result is not None:
            return deadline_result
        return self._max_age_result(active, now)

    # -- features -----------------------------------------------------------------------

    def _deadline_result(self, active: _Active, now: float, session_id: str | None) -> Any:
        deadline = active.config.deadline
        if deadline is None or now < deadline:
            return None
        if active.state.deadline_notified == deadline:
            return None
        # With no single session id (none, or several) only this container's own record
        # applies, so a relaunch in that case could announce the deadline once more.
        marker = None if session_id is None else self._deadline_marker_path(deadline, session_id)
        active.state.deadline_notified = deadline
        self._save_state(active.state)
        if marker is not None and marker.is_file():
            # This session was told in an earlier container. Recording it here also stands
            # the maximum-age restart down, as it did there.
            self._api.audit(f"deadline {deadline} was announced before a restart: not repeating")
            return None
        if marker is not None:
            self._write_private(marker, json.dumps({"deadline": deadline, "announced_at": now}))
        self._api.audit(f"deadline {deadline} reached: asking for the deadline notice")
        return self._api.Notify(self._api.DEADLINE_REACHED)

    def _max_age_result(self, active: _Active, now: float) -> Any:
        config = active.config
        state = active.state
        max_age = config.max_age
        if max_age is None:
            return None
        if config.deadline is not None and state.deadline_notified == config.deadline:
            return None
        restart_at = state.started_at + max_age
        if now >= restart_at:
            if not self._restart_audited:
                self._restart_audited = True
                age = _describe_duration(int(now - state.started_at))
                self._api.audit(
                    f"restart requested: session is {age} old, "
                    f"maximum is {_describe_duration(max_age)}"
                )
            return self._api.ExitForRestart(
                f"session reached its maximum age of {_describe_duration(max_age)}"
            )
        warn_at = restart_at - config.warn_minutes * 60
        if now >= warn_at and not state.restart_warned:
            state.restart_warned = True
            self._save_state(state)
            remaining_minutes = max(1, math.ceil((restart_at - now) / 60))
            minutes = min(config.warn_minutes, remaining_minutes)
            self._api.audit(f"restart warning: {minutes} minute(s) to the maximum age")
            return self._api.Notify(self._api.RESTART_SOON, minutes)
        return None

    # -- state --------------------------------------------------------------------------

    def _state_dir(self) -> Path:
        return Path(self._api.state_dir)

    def _state_path(self, launch_id: str) -> Path:
        # Safe as a file name: parse_config has matched launch_id against _LAUNCH_ID.
        return self._state_dir() / f"{STATE_FILE_PREFIX}{launch_id}{STATE_SUFFIX}"

    def _deadline_marker_path(self, deadline: int, session_id: str) -> Path:
        key = hashlib.sha256(session_id.encode("utf-8")).hexdigest()[:SESSION_KEY_HEX_CHARS]
        return self._state_dir() / f"{DEADLINE_MARKER_PREFIX}{deadline}-{key}{STATE_SUFFIX}"

    def _load_or_create_state(self, config: Config) -> State:
        path = self._state_path(config.launch_id)
        self._prune_stale_files(keep=path.name, now=float(self._clock()))
        state = self._read_state(path)
        if state is None:
            # A new container: the age clock and the restart warning start over. Whether a
            # deadline was already announced is per session, found in on_idle.
            state = State(launch_id=config.launch_id, started_at=float(self._clock()))
        elif state.launch_id != config.launch_id:
            raise ValueError(f"the lifecycle state at {path} names launch {state.launch_id!r}")
        self._save_state(state)
        return state

    def _prune_stale_files(self, keep: str, now: float) -> None:
        directory = self._state_dir()
        try:
            names = [entry.name for entry in directory.iterdir()]
        except OSError as exc:
            raise ValueError(f"cannot list the lifecycle state in {directory}: {exc}") from exc
        for name in names:
            if name == keep or not _OWN_FILE.fullmatch(name):
                continue
            path = directory / name
            try:
                info = path.lstat()
            except FileNotFoundError:
                # A concurrent container's start pruned it first, or its save renamed it.
                continue
            except OSError as exc:
                raise ValueError(f"cannot inspect the lifecycle state at {path}: {exc}") from exc
            if not stat.S_ISREG(info.st_mode) or now - info.st_mtime <= PRUNE_AFTER_SECONDS:
                continue
            try:
                path.unlink(missing_ok=True)
            except OSError as exc:
                raise ValueError(f"cannot remove stale lifecycle state {path}: {exc}") from exc

    @staticmethod
    def _read_state(path: Path) -> State | None:
        try:
            raw = path.read_text(encoding="utf-8")
        except FileNotFoundError:
            return None
        except OSError as exc:
            raise ValueError(f"cannot read the lifecycle state at {path}: {exc}") from exc
        return State.from_json(raw, path)

    def _save_state(self, state: State) -> None:
        self._write_private(self._state_path(state.launch_id), state.to_json())

    @staticmethod
    def _write_private(path: Path, text: str) -> None:
        # The temporary name carries the target's own unique name, so two containers (whose
        # pids can coincide across pid namespaces) never write the same temporary file.
        tmp = path.with_name(path.name + f".tmp.{os.getpid()}")
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, STATE_FILE_MODE)
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as handle:
                handle.write(text)
            os.replace(tmp, path)
        except OSError:
            tmp.unlink(missing_ok=True)
            raise


def create_worker_half(
    api: Any,
    environ: Mapping[str, str] | None = None,
    clock: Callable[[], float] | None = None,
) -> LifecycleHalf:
    """The supervisor's factory; `environ` and `clock` exist for tests only."""
    return LifecycleHalf(api, environ=environ, clock=clock)
