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

The persisted state lives in the plugin's private state directory, which survives a
container restart (hence the launch id) and a worker hot reload (hence persisting at all).

Standard library only, as the supervisor's plugin contract requires.
"""

from __future__ import annotations

import json
import math
import os
import re
import time
from pathlib import Path

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

STATE_FILE = "lifecycle.json"
STATE_FILE_MODE = 0o600

_DIGITS = re.compile(r"[0-9]+")
_LAUNCH_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{7,63}")


def _optional_int(environ, name: str) -> int | None:
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
    """The validated configuration; `enabled` is False when no feature is configured."""

    def __init__(self, environ) -> None:
        self.max_age = _optional_int(environ, ENV_MAX_AGE)
        self.deadline = _optional_int(environ, ENV_DEADLINE)
        warn = _optional_int(environ, ENV_WARN_MINUTES)
        self.warn_minutes = WARN_MINUTES_DEFAULT if warn is None else warn

        if self.max_age is not None and not (
            MAX_AGE_MIN_SECONDS <= self.max_age <= MAX_AGE_MAX_SECONDS
        ):
            raise ValueError(
                f"{ENV_MAX_AGE} must be between {MAX_AGE_MIN_SECONDS} and "
                f"{MAX_AGE_MAX_SECONDS} seconds, got {self.max_age}"
            )
        if self.deadline is not None and self.deadline <= 0:
            raise ValueError(f"{ENV_DEADLINE} must be a positive epoch time, got {self.deadline}")
        if not (WARN_MINUTES_MIN <= self.warn_minutes <= WARN_MINUTES_MAX):
            raise ValueError(
                f"{ENV_WARN_MINUTES} must be between {WARN_MINUTES_MIN} and "
                f"{WARN_MINUTES_MAX}, got {self.warn_minutes}"
            )
        if self.max_age is not None and self.warn_minutes * 60 >= self.max_age:
            raise ValueError(
                f"{ENV_WARN_MINUTES} ({self.warn_minutes}) must be shorter than the maximum age "
                f"({_describe_duration(self.max_age)})"
            )

        self.launch_id = environ.get(ENV_LAUNCH_ID, "")
        if self.enabled and not _LAUNCH_ID.fullmatch(self.launch_id):
            raise ValueError(
                f"{ENV_LAUNCH_ID} must be set by the launcher to an id of 8 to 64 safe "
                f"characters, got {self.launch_id!r}"
            )

    @property
    def enabled(self) -> bool:
        return self.max_age is not None or self.deadline is not None


class LifecycleHalf:
    name = PLUGIN_NAME
    version = PLUGIN_VERSION

    def __init__(self, api, environ=None, clock=None) -> None:
        self._api = api
        self._environ = os.environ if environ is None else environ
        self._clock = time.time if clock is None else clock
        self._config: Config | None = None
        self._state: dict | None = None
        self._restart_audited = False

    # -- supervisor hooks ---------------------------------------------------------------

    def on_start(self) -> None:
        """Validate the configuration and load or create the persisted state.

        Runs on every worker start, including a hot reload, so it must be idempotent: the
        same launch id keeps the stored start time and flags.
        """
        self._config = Config(self._environ)
        if not self._config.enabled:
            self._state = None
            return
        self._state = self._load_or_create_state(self._config)
        started = self._state["started_at"]
        self._api.audit(
            f"started: launch {self._config.launch_id}, session clock began at {started:.0f}, "
            f"max age {self._describe_max_age()}, deadline {self._describe_deadline()}"
        )

    def on_idle(self, tick):
        if self._config is None or self._state is None:
            return None
        now = tick.now
        deadline_result = self._deadline_result(now)
        if deadline_result is not None:
            return deadline_result
        return self._max_age_result(now)

    # -- features -----------------------------------------------------------------------

    def _deadline_result(self, now: float):
        config = self._config
        if config.deadline is None or now < config.deadline:
            return None
        if self._state["deadline_notified"] == config.deadline:
            return None
        self._state["deadline_notified"] = config.deadline
        self._save_state()
        self._api.audit(f"deadline {config.deadline} reached: asking for the deadline notice")
        return self._api.Notify(self._api.DEADLINE_REACHED)

    def _max_age_result(self, now: float):
        config = self._config
        if config.max_age is None:
            return None
        if config.deadline is not None and self._state["deadline_notified"] == config.deadline:
            return None
        restart_at = self._state["started_at"] + config.max_age
        if now >= restart_at:
            if not self._restart_audited:
                self._restart_audited = True
                self._api.audit(
                    f"restart requested: session is {_describe_duration(int(now - self._state['started_at']))} "
                    f"old, maximum is {_describe_duration(config.max_age)}"
                )
            return self._api.ExitForRestart(
                f"session reached its maximum age of {_describe_duration(config.max_age)}"
            )
        warn_at = restart_at - config.warn_minutes * 60
        if now >= warn_at and not self._state["restart_warned"]:
            self._state["restart_warned"] = True
            self._save_state()
            remaining_minutes = max(1, math.ceil((restart_at - now) / 60))
            minutes = min(config.warn_minutes, remaining_minutes)
            self._api.audit(f"restart warning: {minutes} minute(s) to the maximum age")
            return self._api.Notify(self._api.RESTART_SOON, minutes)
        return None

    # -- state --------------------------------------------------------------------------

    def _state_path(self) -> Path:
        return Path(self._api.state_dir) / STATE_FILE

    def _load_or_create_state(self, config: Config) -> dict:
        path = self._state_path()
        previous = self._read_state(path)
        if previous is not None and previous["launch_id"] == config.launch_id:
            state = previous
        else:
            # A new container: the age clock and the restart warning start over. The
            # deadline notice is keyed by the deadline itself and carries across, so a
            # restart after the deadline does not announce it a second time.
            state = {
                "launch_id": config.launch_id,
                "started_at": float(self._clock()),
                "restart_warned": False,
                "deadline_notified": None if previous is None else previous["deadline_notified"],
            }
        self._state = state
        self._save_state()
        return state

    @staticmethod
    def _read_state(path: Path) -> dict | None:
        try:
            raw = path.read_text(encoding="utf-8")
        except FileNotFoundError:
            return None
        except OSError as exc:
            raise ValueError(f"cannot read the lifecycle state at {path}: {exc}") from exc
        try:
            data = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise ValueError(f"the lifecycle state at {path} is not valid JSON: {exc}") from exc
        started = data.get("started_at") if isinstance(data, dict) else None
        launch = data.get("launch_id") if isinstance(data, dict) else None
        warned = data.get("restart_warned", False) if isinstance(data, dict) else None
        notified = data.get("deadline_notified") if isinstance(data, dict) else None
        valid = (
            isinstance(launch, str)
            and isinstance(started, (int, float))
            and not isinstance(started, bool)
            and isinstance(warned, bool)
            and (notified is None or (isinstance(notified, int) and not isinstance(notified, bool)))
        )
        if not valid:
            raise ValueError(f"the lifecycle state at {path} has unexpected contents")
        return {
            "launch_id": launch,
            "started_at": float(started),
            "restart_warned": warned,
            "deadline_notified": notified,
        }

    def _save_state(self) -> None:
        path = self._state_path()
        tmp = path.with_name(path.name + f".tmp.{os.getpid()}")
        payload = json.dumps(self._state, sort_keys=True)
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, STATE_FILE_MODE)
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as handle:
                handle.write(payload)
            os.replace(tmp, path)
        except OSError:
            tmp.unlink(missing_ok=True)
            raise

    # -- audit text ---------------------------------------------------------------------

    def _describe_max_age(self) -> str:
        if self._config.max_age is None:
            return "off"
        return _describe_duration(self._config.max_age)

    def _describe_deadline(self) -> str:
        if self._config.deadline is None:
            return "none"
        return str(self._config.deadline)


def create_worker_half(api, environ=None, clock=None):
    """The supervisor's factory; `environ` and `clock` exist for tests only."""
    return LifecycleHalf(api, environ=environ, clock=clock)
