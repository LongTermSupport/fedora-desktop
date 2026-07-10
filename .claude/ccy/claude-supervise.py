#!/usr/bin/env python3
"""claude-supervise: a standalone, stdlib-only PTY supervisor for wrapping `claude`.

This file is intentionally standalone: it imports nothing from
`claude_code_hooks_daemon` (no pydantic, no daemon venv). It runs under the
container's system `python3` so that upgrading (or breaking) the hooks-daemon
venv can never take down every `ccy` launch.

It spawns the wrapped process on a pseudo-terminal and forwards
stdin/stdout/window-resize faithfully. On each idle poll tick it reads the
daemon-written context sidecar and runs the Decision H state machine; when the
context goes red (+ idle + cooldown/cap) it INJECTS a compact trigger into the
session. In DRY-RUN (default) that injection is a harmless VISIBLE MARKER,
proving the mechanism end-to-end without a real compaction; with ``--arm`` it
injects the real ``/compact`` (and ``continue``).

Usage:
    claude-supervise.py [--dry-run | --arm] [--log PATH] -- <child argv...>
"""

from __future__ import annotations

import argparse
import enum
import errno
import fcntl
import json
import os
import pty
import select
import signal
import struct
import sys
import termios
import time
import tty
from collections.abc import Callable
from dataclasses import dataclass, replace
from datetime import UTC, datetime
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from types import FrameType

_READ_CHUNK_SIZE = 4096
_FALLBACK_WINSIZE = struct.pack("HHHH", 24, 80, 0, 0)
_LOG_SUBDIRECTORY = "supervise"
_LOG_FILENAME = "decision.log"

_USAGE = "Usage: claude-supervise.py [--dry-run | --arm] [--log PATH] -- <child argv...>\n"


@dataclass
class InputActivity:
    """Tracks observed stdin activity forwarded to the supervised child."""

    bytes_seen: int = 0
    last_input_monotonic: float | None = None

    def record(self, data: bytes) -> None:
        """Record a chunk of stdin data that was forwarded to the child."""
        self.bytes_seen += len(data)
        self.last_input_monotonic = os.times().elapsed


class DecisionLog:
    """Append-only, timestamped log file for supervisor decisions/observations.

    Every line is timestamped and written immediately (no buffering) so a
    crash or forced kill of the supervised session never loses prior
    observations. Write failures are never swallowed -- FAIL FAST so a broken
    log path is surfaced immediately rather than silently discarding
    supervisor context.
    """

    def __init__(self, path: Path | None = None) -> None:
        """Create (or attach to) a decision log file.

        Args:
            path: Explicit log file path. When omitted, defaults to
                ``$CLAUDE_PROJECT_DIR/untracked/supervise/decision.log``
                (falling back to the current working directory when the
                environment variable is unset).

        Raises:
            OSError: If the parent directory cannot be created.
        """
        self._path = path if path is not None else self._default_path()
        self._path.parent.mkdir(parents=True, exist_ok=True)

    @staticmethod
    def _default_path() -> Path:
        project_dir = Path(os.environ.get("CLAUDE_PROJECT_DIR") or Path.cwd())
        return project_dir / "untracked" / _LOG_SUBDIRECTORY / _LOG_FILENAME

    @property
    def path(self) -> Path:
        """Absolute path to the underlying log file."""
        return self._path

    def write(self, message: str) -> None:
        """Append a single timestamped line to the log.

        Args:
            message: The message to record.

        Raises:
            OSError: If the file cannot be written (never swallowed).
        """
        timestamp = datetime.now(UTC).isoformat()
        with self._path.open("a", encoding="utf-8") as handle:
            handle.write(f"{timestamp} {message}\n")


# ---------------------------------------------------------------------------
# Compact decision logic (Plan 00135 Slice 2)
#
# The daemon writes an observe-only "context sidecar" JSON per session
# (handlers/status_line/context_sidecar.py). This supervisor READS the freshest
# sidecar and runs the Decision H state machine to decide when to inject
# `/compact` (and, once compaction is under way, `continue`). The decision here
# is mode-agnostic; the injection layer below turns a decision into either a
# harmless dry-run marker or the real command.
# ---------------------------------------------------------------------------

_SIDECAR_SUBDIR = "context-sidecar"

# State-machine policy defaults (seconds / counts). Conservative on purpose.
_DEFAULT_FRESHNESS_SECONDS = 30.0
_DEFAULT_COOLDOWN_SECONDS = 300.0
_DEFAULT_AWAIT_TIMEOUT_SECONDS = 120.0
_DEFAULT_MAX_INJECTIONS = 20
# How long a compaction-signal file is treated as "compaction under way".
# Much longer than the sidecar freshness because a large-context compaction can
# keep the session busy (streaming output, no status render, no select-timeout
# poll) for minutes before it goes idle. The signal is written at the START of
# compaction; if the TTL expires before the first post-compaction idle poll, the
# resume `continue` is never injected and the session is left idle. 120s was too
# short and dropped the resume on big conversations (observed live: signal 485s
# old by the first idle poll). The file is consumed on a successful resume and a
# fresh compaction overwrites it, so a generous TTL cannot re-fire or wedge a
# later compaction -- it only bounds how long an unconsumed signal lingers.
_DEFAULT_COMPACTION_SIGNAL_TTL_SECONDS = 600.0

# Compaction-signal files are written by the daemon's PreCompact handler as
# ``<session>.compacting`` -- deliberately NOT ``*.json`` so they are never
# mistaken for a context sidecar by ``load_freshest_sidecar``.
_COMPACTION_SIGNAL_GLOB = "*.compacting"


class Decision(enum.Enum):
    """What the supervisor WOULD do this evaluation (dry-run logs it)."""

    NOOP = "noop"
    WOULD_COMPACT = "would-compact"
    WOULD_CONTINUE = "would-continue"


class SupervisorState(enum.Enum):
    """Two-state compact-and-resume machine (Decision H)."""

    MONITOR = "monitor"
    AWAIT_COMPACTING = "await-compacting"


@dataclass(frozen=True)
class SidecarReading:
    """A parsed snapshot of the daemon-written context sidecar."""

    red: bool
    tier: str
    pct: float
    session_id: str
    ts: float
    seq: int
    writer_pid: int
    compacting: bool
    stale: bool


@dataclass(frozen=True)
class CompactPolicy:
    """Tunable guards for the compact state machine."""

    freshness_seconds: float = _DEFAULT_FRESHNESS_SECONDS
    cooldown_seconds: float = _DEFAULT_COOLDOWN_SECONDS
    await_timeout_seconds: float = _DEFAULT_AWAIT_TIMEOUT_SECONDS
    max_injections: int = _DEFAULT_MAX_INJECTIONS
    compaction_signal_ttl_seconds: float = _DEFAULT_COMPACTION_SIGNAL_TTL_SECONDS


@dataclass(frozen=True)
class Evaluation:
    """The outcome of one state-machine evaluation."""

    decision: Decision
    reason: str


def _coerce_float(value: object) -> float:
    """Best-effort float coercion; non-numeric values become 0.0."""
    return float(value) if isinstance(value, (int, float)) else 0.0


def _coerce_int(value: object) -> int:
    """Best-effort int coercion; non-numeric values become 0."""
    return int(value) if isinstance(value, (int, float)) else 0


def _default_sidecar_dir() -> Path:
    """Resolve the daemon's context-sidecar directory from the environment.

    Mirrors ``DecisionLog._default_path``: uses ``$CLAUDE_PROJECT_DIR`` (the
    project root, exported by ccy in-container), falling back to the current
    working directory when the variable is unset.
    """
    project_dir = Path(os.environ.get("CLAUDE_PROJECT_DIR") or Path.cwd())
    return project_dir / "untracked" / _SIDECAR_SUBDIR


def load_freshest_sidecar(
    directory: Path, *, now: float, freshness_seconds: float
) -> SidecarReading | None:
    """Load the freshest (max-``ts``) sidecar JSON in ``directory``.

    Returns None when the directory is absent or contains no parseable
    sidecar. Malformed files are skipped (they may be from an older schema or
    a foreign writer) rather than aborting the scan -- the freshest VALID
    reading wins. ``stale`` is set when ``now - ts`` exceeds
    ``freshness_seconds`` (the daemon has not rendered a status line recently,
    so the session is idle or gone and must not be acted on).

    Args:
        directory: The ``context-sidecar`` directory to scan.
        now: Current epoch time (injected for deterministic tests).
        freshness_seconds: Age beyond which a reading is marked ``stale``.

    Returns:
        The freshest valid ``SidecarReading``, or None if none is available.
    """
    if not directory.is_dir():
        return None

    freshest_data = None
    freshest_ts = float("-inf")
    for path in directory.glob("*.json"):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            # Unreadable or malformed sidecar -- skip and keep scanning.
            continue
        if not isinstance(data, dict):
            continue
        ts = _coerce_float(data.get("ts"))
        if ts > freshest_ts:
            freshest_ts = ts
            freshest_data = data

    if freshest_data is None:
        return None

    return SidecarReading(
        red=bool(freshest_data.get("red", False)),
        tier=str(freshest_data.get("tier", "")),
        pct=_coerce_float(freshest_data.get("pct")),
        session_id=str(freshest_data.get("session_id", "")),
        ts=freshest_ts,
        seq=_coerce_int(freshest_data.get("seq")),
        writer_pid=_coerce_int(freshest_data.get("writer_pid")),
        compacting=bool(freshest_data.get("compacting", False)),
        stale=(now - freshest_ts) > freshness_seconds,
    )


def load_compaction_signal(directory: Path, *, now: float, ttl_seconds: float) -> Path | None:
    """Return the path of a fresh compaction-signal file, or None.

    The daemon's PreCompact handler drops a ``<session>.compacting`` file (JSON
    ``{"ts": ...}``) when a compaction starts -- whether the supervisor
    triggered it or the human typed ``/compact``. A signal is "fresh" while
    ``now - ts <= ttl_seconds``; older files are treated as a finished
    compaction and ignored. The path (not a bool) is returned so the caller can
    CONSUME the file (unlink it) once it has acted on it, guaranteeing the
    resume fires exactly once and cannot wedge a later compaction.
    """
    if not directory.is_dir():
        return None
    for path in directory.glob(_COMPACTION_SIGNAL_GLOB):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            continue
        if not isinstance(data, dict):
            continue
        ts = _coerce_float(data.get("ts"))
        if (now - ts) <= ttl_seconds:
            return path
    return None


class CompactStateMachine:
    """Decision H compact-and-resume machine (pure; injects nothing).

    Compaction detection (any state): when a compaction is under way
    (``reading.compacting`` -- set from the daemon's PreCompact signal, whether
    the supervisor triggered it OR the human typed ``/compact`` manually),
    decide WOULD_CONTINUE exactly once per episode to resume the post-compact
    session. A latch prevents re-firing while the compaction persists and
    resets when it ends. ``continue`` is harmless, so this fires even in
    dry-run.

    MONITOR: when the sidecar is fresh + red AND the session is idle AND the
    cooldown/cap allow it, decide WOULD_COMPACT and move to AWAIT_COMPACTING.

    AWAIT_COMPACTING: wait for a compaction to start (handled above). If none
    starts within ``await_timeout_seconds``, give up and return to MONITOR so a
    missed transition cannot wedge the machine forever.
    """

    def __init__(self, policy: CompactPolicy) -> None:
        self._policy = policy
        self.state = SupervisorState.MONITOR
        self._injections = 0
        self._last_action_ts: float | None = None
        self._compaction_handled = False

    def evaluate(self, reading: SidecarReading | None, *, idle: bool, now: float) -> Evaluation:
        """Advance the machine one step and return what it WOULD do."""
        compacting = reading is not None and reading.compacting
        if compacting:
            if self._compaction_handled:
                # Already resumed this episode; sit tight until compaction ends.
                return Evaluation(Decision.NOOP, "compaction in progress (already resumed)")
            if not idle:
                # Never type `continue` into a busy TUI -- it would be lost or
                # corrupt in-flight input. Do NOT latch: retry on the next idle
                # poll so the resume still fires once the session settles.
                return Evaluation(
                    Decision.NOOP,
                    "compaction detected but session busy -> awaiting idle to resume",
                )
            self._compaction_handled = True
            self._last_action_ts = now
            self.state = SupervisorState.MONITOR
            return Evaluation(
                Decision.WOULD_CONTINUE,
                "compaction detected -> would inject continue",
            )

        # No compaction under way: reset the latch and run normal logic.
        self._compaction_handled = False
        if self.state is SupervisorState.AWAIT_COMPACTING:
            return self._evaluate_await(now=now)
        return self._evaluate_monitor(reading, idle=idle, now=now)

    def _evaluate_monitor(
        self, reading: SidecarReading | None, *, idle: bool, now: float
    ) -> Evaluation:
        if reading is None:
            return Evaluation(Decision.NOOP, "no sidecar reading")
        if reading.stale:
            return Evaluation(Decision.NOOP, "sidecar stale")
        if not reading.red:
            return Evaluation(Decision.NOOP, f"not red (tier={reading.tier})")
        if not idle:
            return Evaluation(Decision.NOOP, "session busy (composing)")
        if self._injections >= self._policy.max_injections:
            return Evaluation(Decision.NOOP, "injection cap reached")
        if not self._cooldown_elapsed(now):
            return Evaluation(Decision.NOOP, "cooldown active")

        self._injections += 1
        self._last_action_ts = now
        self.state = SupervisorState.AWAIT_COMPACTING
        return Evaluation(
            Decision.WOULD_COMPACT,
            f"red at {reading.pct:.0f}% + idle -> would inject /compact",
        )

    def _evaluate_await(self, *, now: float) -> Evaluation:
        if self._await_timed_out(now):
            self.state = SupervisorState.MONITOR
            return Evaluation(Decision.NOOP, "await-compacting timed out -> back to monitor")
        return Evaluation(Decision.NOOP, "awaiting compaction start")

    def _cooldown_elapsed(self, now: float) -> bool:
        if self._last_action_ts is None:
            return True
        return (now - self._last_action_ts) >= self._policy.cooldown_seconds

    def _await_timed_out(self, now: float) -> bool:
        if self._last_action_ts is None:
            return False
        return (now - self._last_action_ts) > self._policy.await_timeout_seconds


# ---------------------------------------------------------------------------
# Keystroke injection (Plan 00135 Slice 2)
#
# In DRY-RUN (default) the supervisor types a harmless, visible MARKER string
# into the live session when the compact trigger fires -- proving the
# end-to-end injection path without triggering a real compaction. When ARMED
# it types the real `/compact` (and, later, `continue`). Both go through the
# same code path; only the payload text differs.
# ---------------------------------------------------------------------------

# All supervisor-injected PROMPTS carry this bot prefix (with an emoji) so they
# are visibly machine-generated in the transcript and never mistaken for
# something the human typed.
_BOT_PREFIX = "🤖 [ccy-supervisor]"
_DRY_RUN_COMPACT_MARKER = (
    f"{_BOT_PREFIX} compact suggestion fired (dry-run — not a real /compact, not human input)"
)
# The armed compact is a real `/compact`, but `/compact` accepts freeform custom
# instructions as its argument -- so the bot chrome rides along AS those
# instructions. The slash command is still the first token (recognised
# normally), the compaction summary is now visibly bot-initiated (never mistaken
# for a human `/compact`), and the instruction text tells the post-compact
# session it was an AUTOMATED compaction that must resume -- reinforcing the
# `continue` keystroke the supervisor injects once compaction ends.
_ARMED_COMPACT_PAYLOAD = (
    f"/compact {_BOT_PREFIX} automated compaction — NOT human-initiated. "
    "After compacting, immediately resume and continue the work that was in progress."
)
# `continue` is harmless -- it only nudges the agent to resume -- so it is
# injected FOR REAL in both dry-run and armed modes. Detecting a compaction and
# not resuming would defeat the purpose, and (unlike /compact) a stray
# `continue` cannot destroy context. It keeps the bot prefix so a post-compact
# resume is clearly the supervisor's doing, not a human message.
_CONTINUE_PAYLOAD = f"{_BOT_PREFIX} continue"
_INJECT_SUBMIT = "\r"
# The submit (Enter) is written SEPARATELY from the payload, after this pause.
# Injecting `payload + \r` in a single burst leaves the trailing carriage
# return absorbed into the (multi-line-capable) input box -- the text sits
# unsubmitted -- which was observed live on a long `/compact <instructions>`
# line (a short bare `/compact\r` had submitted fine, so length/burst is the
# trigger). A brief pause then a standalone `\r` is registered by the TUI as a
# real Enter and submits regardless of payload length. This mirrors how
# tmux/expect/pexpect drive a TUI: send text, then send Enter as its own key.
_SUBMIT_DELAY_SECONDS = 0.2

_DEFAULT_POLL_SECONDS = 2.0
_DEFAULT_IDLE_FLOOR_SECONDS = 2.0


def _resolve_payload(decision: Decision, *, dry_run: bool) -> str | None:
    """Return the keystroke payload for a decision, or None for NOOP.

    In dry-run mode the payload is a harmless, visible MARKER string so the
    injection path can be exercised end-to-end without triggering a real
    compaction. Armed mode injects the real slash-command / prompt.
    """
    if decision is Decision.WOULD_COMPACT:
        return _DRY_RUN_COMPACT_MARKER if dry_run else _ARMED_COMPACT_PAYLOAD
    if decision is Decision.WOULD_CONTINUE:
        return _CONTINUE_PAYLOAD
    return None


def _perform_injection(
    master_writer: Callable[[bytes], None],
    payload: str,
    *,
    sleep: Callable[[float], None] = time.sleep,
) -> None:
    """Type ``payload`` into the child PTY, then submit it as a SEPARATE keypress.

    The submit (carriage return) is a distinct write, delayed by
    ``_SUBMIT_DELAY_SECONDS`` from the payload, so the TUI registers a real Enter
    instead of absorbing a trailing CR into its multi-line input box (which left
    a long ``/compact`` line sitting unsubmitted). ``sleep`` is injectable so
    tests do not actually pause.
    """
    master_writer(payload.encode("utf-8"))
    sleep(_SUBMIT_DELAY_SECONDS)
    master_writer(_INJECT_SUBMIT.encode("utf-8"))


def _consume_signal(path: Path, log: DecisionLog | None) -> None:
    """Delete a handled compaction-signal file so the resume fires exactly once.

    Best-effort: a missing or undeletable file is harmless because the TTL will
    eventually age it out and the ``_compaction_handled`` latch already blocks a
    same-episode repeat. A delete failure is LOGGED (never silently swallowed)
    so a broken untracked dir is visible.
    """
    try:
        path.unlink(missing_ok=True)
    except OSError as exc:
        if log is not None:
            log.write(f"warning: could not consume compaction signal {path}: {exc}")


def _is_idle(activity: InputActivity, *, now_monotonic: float, idle_floor_seconds: float) -> bool:
    """True when no stdin byte has been forwarded within ``idle_floor_seconds``.

    Never inject while the human is composing: an injection mid-keystroke would
    corrupt their input. A session with no observed input yet counts as idle.
    """
    if activity.last_input_monotonic is None:
        return True
    return (now_monotonic - activity.last_input_monotonic) >= idle_floor_seconds


def _poll_once(
    machine: CompactStateMachine,
    *,
    sidecar_dir: Path,
    now_wall: float,
    idle: bool,
    dry_run: bool,
    master_writer: Callable[[bytes], None],
    log: DecisionLog | None,
    freshness_seconds: float,
    compaction_signal_ttl_seconds: float = _DEFAULT_COMPACTION_SIGNAL_TTL_SECONDS,
) -> Evaluation:
    """One supervisor tick: read the sidecar, decide, and inject if warranted.

    Reads the freshest sidecar AND the compaction signal, advances the state
    machine, and -- for a non-NOOP decision -- injects the resolved payload
    (a marker for a dry-run compact; the real command otherwise) and logs it.
    Returns the Evaluation so callers/tests can observe the decision.
    """
    reading = load_freshest_sidecar(sidecar_dir, now=now_wall, freshness_seconds=freshness_seconds)
    # A compaction stops status renders, so the context sidecar goes
    # stale/absent during one -- the compaction signal is an independent input.
    signal_path = load_compaction_signal(
        sidecar_dir, now=now_wall, ttl_seconds=compaction_signal_ttl_seconds
    )
    if signal_path is not None:
        reading = (
            replace(reading, compacting=True)
            if reading is not None
            else SidecarReading(
                red=False,
                tier="",
                pct=0.0,
                session_id="",
                ts=now_wall,
                seq=0,
                writer_pid=0,
                compacting=True,
                stale=False,
            )
        )
    evaluation = machine.evaluate(reading, idle=idle, now=now_wall)
    payload = _resolve_payload(evaluation.decision, dry_run=dry_run)
    if payload is not None:
        _perform_injection(master_writer, payload)
        if log is not None:
            log.write(f"{evaluation.decision.value}: {evaluation.reason}; injected {payload!r}")
        # Consume the signal ONLY after a resume actually fired, so a busy-gated
        # NOOP leaves it in place to retry on the next idle poll.
        if evaluation.decision is Decision.WOULD_CONTINUE and signal_path is not None:
            _consume_signal(signal_path, log)
    return evaluation


def _get_winsize(stdin_fd: int) -> bytes:
    """Read the controlling terminal's window size, falling back if unavailable."""
    try:
        return fcntl.ioctl(stdin_fd, termios.TIOCGWINSZ, b"\x00" * 8)
    except OSError:
        return _FALLBACK_WINSIZE


def _set_winsize(master_fd: int, stdin_fd: int) -> None:
    """Copy the controlling terminal's window size onto the pty master fd."""
    fcntl.ioctl(master_fd, termios.TIOCSWINSZ, _get_winsize(stdin_fd))


def _exit_code_from_status(status: int) -> int:
    """Translate a `waitpid` status into a shell-style exit code."""
    if os.WIFEXITED(status):
        return os.WEXITSTATUS(status)
    if os.WIFSIGNALED(status):
        return 128 + os.WTERMSIG(status)
    return 1


def _forward_io(
    stdin_fd: int,
    master_fd: int,
    activity: InputActivity,
    *,
    poll_seconds: float | None = None,
    on_poll: Callable[[], None] | None = None,
) -> None:
    """Select loop: forward stdin -> master, master -> stdout.

    When ``poll_seconds`` is set, ``select`` wakes on that interval even with no
    I/O, and ``on_poll`` (if given) runs one supervisor tick before looping.
    A tick never touches the forwarded I/O -- it only reads the sidecar and may
    inject -- so transparent passthrough is unchanged when polling is disabled.

    Once stdin reaches EOF it is dropped from the watch set: an EOF fd is always
    "readable", so continuing to select on it would spin the loop and starve the
    poll timeout. Dropping it lets timeouts (and therefore polling) resume.
    """
    stdin_open = True
    while True:
        watch = [master_fd, stdin_fd] if stdin_open else [master_fd]
        try:
            readable, _, _ = select.select(watch, [], [], poll_seconds)
        except OSError as exc:
            if exc.errno == errno.EINTR:
                continue
            raise

        if not readable:
            # select timed out with no I/O ready -> run one supervisor tick.
            if on_poll is not None:
                on_poll()
            continue

        if stdin_open and stdin_fd in readable:
            data = os.read(stdin_fd, _READ_CHUNK_SIZE)
            if data:
                activity.record(data)
                os.write(master_fd, data)
            else:
                # stdin EOF: stop watching it so poll timeouts can fire.
                stdin_open = False

        if master_fd in readable:
            try:
                output = os.read(master_fd, _READ_CHUNK_SIZE)
            except OSError:
                output = b""
            if not output:
                return
            os.write(sys.stdout.fileno(), output)


def supervise(
    argv: list[str],
    *,
    dry_run: bool = True,
    log: DecisionLog | None = None,
    activity: InputActivity | None = None,
    stdin_fd: int | None = None,
    sidecar_dir: Path | None = None,
    policy: CompactPolicy | None = None,
    poll_seconds: float = _DEFAULT_POLL_SECONDS,
    idle_floor_seconds: float = _DEFAULT_IDLE_FLOOR_SECONDS,
) -> int:
    """Run `argv` under a PTY, forwarding I/O and polling the context sidecar.

    On each idle poll tick the supervisor reads the daemon-written context
    sidecar and runs the Decision H state machine. When the compact trigger
    fires (red + idle + cooldown/cap) it INJECTS a payload into the child PTY:
    a harmless visible MARKER in dry-run (default), or the real `/compact` when
    armed. Forwarded I/O is never altered by a tick.

    Args:
        argv: The child command and its arguments (argv[0] is the executable).
        dry_run: When True (default) inject the harmless marker; when False
            (armed) inject the real `/compact` / `continue`.
        log: Optional decision log for startup, injection, and exit lines.
        activity: Optional `InputActivity` to record stdin byte counts into.
            A fresh one is created internally if not supplied.
        stdin_fd: File descriptor to read supervisor input from. Defaults to
            `sys.stdin.fileno()`. Overridable so callers (and tests) can pass
            a real fd directly, bypassing wrappers that don't expose one.
        sidecar_dir: Directory holding the daemon's context sidecars. Defaults
            to the environment-derived `context-sidecar` dir.
        policy: Compact state-machine policy (cooldown/cap/freshness/timeout).
        poll_seconds: Idle poll interval for the sidecar tick.
        idle_floor_seconds: Minimum quiet time before an injection is allowed.

    Returns:
        The child's exit code (or 128+signal if it died from a signal).

    Raises:
        ValueError: If `argv` is empty.
    """
    if not argv:
        raise ValueError("supervise() requires a non-empty argv")

    activity = activity if activity is not None else InputActivity()
    stdin_fd = stdin_fd if stdin_fd is not None else sys.stdin.fileno()
    sidecar_dir = sidecar_dir if sidecar_dir is not None else _default_sidecar_dir()
    policy = policy if policy is not None else CompactPolicy()
    machine = CompactStateMachine(policy)
    mode = "dry-run (injects marker)" if dry_run else "ARMED (injects /compact)"

    if log is not None:
        log.write(
            f"supervisor active ({mode}); polling {sidecar_dir} every "
            f"{poll_seconds}s; wrapping: {argv}"
        )

    pid, master_fd = pty.fork()
    if pid == 0:  # pragma: no cover - runs in the forked child process
        # SECURITY: no shell involved -- argv is passed directly to execvp as
        # a list (never a shell string), so there is no command-injection
        # surface here. This IS the supervisor's job: exec the wrapped
        # process (e.g. `claude`) on the child side of the PTY.
        os.execvp(argv[0], argv)  # nosec B606
        os._exit(127)  # unreachable on success

    _set_winsize(master_fd, stdin_fd)

    old_termios: list[int | list[bytes | int]] | None = None
    stdin_is_tty = os.isatty(stdin_fd)
    if stdin_is_tty:
        old_termios = termios.tcgetattr(stdin_fd)
        tty.setraw(stdin_fd)

    def _on_winch(_signum: int, _frame: FrameType | None) -> None:
        _set_winsize(master_fd, stdin_fd)

    def _write_master(data: bytes) -> None:
        os.write(master_fd, data)

    def _on_poll() -> None:
        idle = _is_idle(
            activity,
            now_monotonic=os.times().elapsed,
            idle_floor_seconds=idle_floor_seconds,
        )
        _poll_once(
            machine,
            sidecar_dir=sidecar_dir,
            now_wall=time.time(),
            idle=idle,
            dry_run=dry_run,
            master_writer=_write_master,
            log=log,
            freshness_seconds=policy.freshness_seconds,
            compaction_signal_ttl_seconds=policy.compaction_signal_ttl_seconds,
        )

    previous_handler = signal.signal(signal.SIGWINCH, _on_winch)

    try:
        _forward_io(stdin_fd, master_fd, activity, poll_seconds=poll_seconds, on_poll=_on_poll)
    finally:
        signal.signal(signal.SIGWINCH, previous_handler)
        if old_termios is not None:
            termios.tcsetattr(stdin_fd, termios.TCSAFLUSH, old_termios)

    _pid, status = os.waitpid(pid, 0)
    exit_code = _exit_code_from_status(status)

    if log is not None:
        log.write(
            f"supervisor exiting ({mode}); transparent passthrough; "
            f"{activity.bytes_seen} input bytes observed"
        )

    return exit_code


def _split_child_argv(argv: list[str]) -> list[str] | None:
    """Return everything after the first `--` separator, or None if absent/empty."""
    if "--" not in argv:
        return None
    child = argv[argv.index("--") + 1 :]
    return child if child else None


def _parse_supervisor_flags(argv: list[str]) -> argparse.Namespace:
    """Parse only the flags that appear before `--` (argparse never sees the child argv)."""
    supervisor_argv = argv[: argv.index("--")] if "--" in argv else argv

    parser = argparse.ArgumentParser(
        prog="claude-supervise",
        description="PTY supervisor for `claude` that watches the context sidecar "
        "and injects a compact trigger when the context goes red.",
        add_help=False,
    )
    mode_group = parser.add_mutually_exclusive_group()
    mode_group.add_argument(
        "--dry-run",
        dest="dry_run",
        action="store_true",
        default=True,
        help="Default. When the trigger fires, inject a harmless VISIBLE MARKER "
        "into the session (proving the mechanism) instead of a real /compact.",
    )
    mode_group.add_argument(
        "--arm",
        dest="dry_run",
        action="store_false",
        help="Inject the REAL /compact (and continue) instead of the marker. "
        "Use only when you want the supervisor to actually compact the session.",
    )
    parser.add_argument(
        "--log",
        dest="log_path",
        type=Path,
        default=None,
        help="Decision log file path (default: $CLAUDE_PROJECT_DIR/untracked/supervise/).",
    )
    return parser.parse_args(supervisor_argv)


def _resolve_decision_log(explicit_path: Path | None) -> DecisionLog:
    """Build the DecisionLog from an explicit path, or the environment-derived default."""
    return DecisionLog(explicit_path)


def main(argv: list[str] | None = None) -> int:
    """Entry point: parse args, run the PTY supervisor, return the exit code.

    Args:
        argv: Command-line arguments (excluding program name). Defaults to
            `sys.argv[1:]` when None.

    Returns:
        2 if no child argv is supplied after `--`; otherwise the supervised
        child's exit code.
    """
    argv = argv if argv is not None else sys.argv[1:]

    child_argv = _split_child_argv(argv)
    if child_argv is None:
        sys.stderr.write(_USAGE)
        return 2

    flags = _parse_supervisor_flags(argv)
    log = _resolve_decision_log(flags.log_path)

    return supervise(child_argv, dry_run=flags.dry_run, log=log)


if __name__ == "__main__":
    raise SystemExit(main())
