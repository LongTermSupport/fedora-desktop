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
from dataclasses import dataclass, field, replace
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

# ---------------------------------------------------------------------------
# Human input-line tracking (empty-input-box injection guard)
#
# Observed live failure: the supervisor pasted its injection into a NON-EMPTY
# input box the human had partially typed into, then submitted the corrupted
# mix. The fix: model the human's input line from the stdin bytes the
# supervisor already forwards, and refuse to inject while that line holds any
# non-whitespace human input. Supervisor-injected keystrokes are written
# straight to the PTY master (never through `InputActivity.record`), so they
# can never mark the box non-empty.
# ---------------------------------------------------------------------------

# Keys that clear the input box: Enter (CR / LF) submits it, Ctrl-U kills the
# line, Ctrl-C discards the in-progress message in the Claude Code TUI.
_LINE_CLEAR_BYTES = frozenset({0x0D, 0x0A, 0x15, 0x03})
# The SUBSET of clear bytes that SUBMIT the line (Enter). Ctrl-U/Ctrl-C discard
# without submitting, so they must NOT count as a human `/compact` request.
_LINE_SUBMIT_BYTES = frozenset({0x0D, 0x0A})
# Keys that delete one character: Ctrl-H and DEL.
_LINE_BACKSPACE_BYTES = frozenset({0x08, 0x7F})
# Bytes that never make the box "non-empty" on their own: space and tab.
_LINE_WHITESPACE_BYTES = frozenset({0x20, 0x09})
# A submitted line whose (whitespace-stripped) text starts with this is a human
# `/compact` — the supervisor detects it to avoid stacking its own /compact on
# top (Claude Code aborts the duplicate). Covers `/compact` and `/compact <args>`.
_COMPACT_COMMAND_PREFIX = "/compact"


# ANSI control-sequence parser states. Terminal-GENERATED sequences (focus
# events ESC[I / ESC[O, cursor-position and device-attribute reports, mouse
# tracking, SS3 function keys) arrive on stdin WITHOUT the human adding content
# and must never mark the box non-empty. Counting their raw bytes as content
# permanently wedged the guard (v3.34.1 field report: a single window-focus
# switch left the box "non-empty" for the whole session -> every injection tick
# deferred forever). The parser consumes whole control sequences as non-content,
# with two deliberate exceptions that DO represent real box content: bracketed-
# paste payload (between ESC[200~ and ESC[201~) and up/down history-recall arrows.
_ST_GROUND = 0
_ST_ESC = 1
_ST_CSI = 2
_ST_SS3 = 3
_ST_STR = 4
_ST_STR_ESC = 5

_ESC = 0x1B
_CSI_INTRODUCER = 0x5B  # '['
_SS3_INTRODUCER = 0x4F  # 'O'
# ESC followed by one of these starts a string sequence terminated by ST/BEL:
# OSC ']' , DCS 'P', SOS 'X', PM '^', APC '_'.
_STRING_INTRODUCERS = frozenset({0x5D, 0x50, 0x58, 0x5E, 0x5F})
_BEL = 0x07
_ST_TERMINATOR = 0x5C  # '\' completing ESC \ (ST)
# CSI byte ranges (ECMA-48): parameter/intermediate 0x20-0x3F, final 0x40-0x7E.
_CSI_PARAM_MIN, _CSI_PARAM_MAX = 0x20, 0x3F
_CSI_FINAL_MIN, _CSI_FINAL_MAX = 0x40, 0x7E
_CSI_FINAL_TILDE = 0x7E  # '~' terminates edit/function keys and paste markers
_CSI_ARROW_UP = 0x41  # 'A'
_CSI_ARROW_DOWN = 0x42  # 'B'
_PASTE_START_PARAMS = (0x32, 0x30, 0x30)  # "200"
_PASTE_END_PARAMS = (0x32, 0x30, 0x31)  # "201"


class HumanInputLine:
    """ANSI-aware model of the human's input-box line, fed from forwarded stdin.

    Only genuine box content marks the line non-empty: printable keystrokes,
    bracketed-paste payload, and up/down history-recall arrows. Whole terminal
    control sequences -- focus events, cursor/device reports, mouse tracking,
    SS3 function keys -- are parsed and consumed WITHOUT counting their bytes,
    because they arrive without the human adding content. Counting them was a
    permanent poison: a single focus switch left the box "non-empty" for the
    rest of the session and every injection tick was deferred forever.

    Still conservative where it matters: any printable byte, unrecognised
    control byte, or UTF-8 content counts, and a malformed/aborted escape
    sequence falls back to ground without silently swallowing later content.
    The parser is streaming -- a sequence split across read() chunks is carried
    across feed() calls via the persisted state.
    """

    def __init__(self) -> None:
        self._buffer: list[int] = []
        self._state: int = _ST_GROUND
        self._csi_params: list[int] = []
        self._in_paste: bool = False
        # Edge flag: set when a submitted line was a human `/compact`, cleared by
        # take_compact_submitted() so the supervisor acts on it exactly once.
        self._compact_submitted: bool = False

    def feed(self, data: bytes) -> None:
        """Advance the line model with a chunk of forwarded human stdin bytes."""
        for byte in data:
            self._consume(byte)

    def _consume(self, byte: int) -> None:
        state = self._state
        if state == _ST_GROUND:
            self._consume_ground(byte)
        elif state == _ST_ESC:
            self._consume_esc(byte)
        elif state == _ST_CSI:
            self._consume_csi(byte)
        elif state == _ST_SS3:
            # SS3 is ESC O <one final byte> (function/keypad key): non-content.
            self._state = _ST_GROUND
        elif state == _ST_STR:
            self._consume_str(byte)
        else:  # _ST_STR_ESC: saw ESC inside a string sequence, want ST ('\').
            self._state = _ST_GROUND if byte == _ST_TERMINATOR else _ST_STR

    def _consume_ground(self, byte: int) -> None:
        if byte == _ESC:
            self._state = _ST_ESC
            return
        if self._in_paste:
            # Inside bracketed paste every non-ESC byte is literal paste
            # payload -- real box content, including embedded CR/LF.
            self._buffer.append(byte)
            return
        if byte in _LINE_CLEAR_BYTES:
            # Only Enter SUBMITS the line; Ctrl-U/Ctrl-C discard it. A submitted
            # `/compact` sets the edge flag so the supervisor can defer to it.
            if byte in _LINE_SUBMIT_BYTES and self._buffer_is_compact():
                self._compact_submitted = True
            self._buffer.clear()
        elif byte in _LINE_BACKSPACE_BYTES:
            if self._buffer:
                self._buffer.pop()
        else:
            self._buffer.append(byte)

    def _consume_esc(self, byte: int) -> None:
        if byte == _CSI_INTRODUCER:
            self._state = _ST_CSI
            self._csi_params = []
        elif byte == _SS3_INTRODUCER:
            self._state = _ST_SS3
        elif byte in _STRING_INTRODUCERS:
            self._state = _ST_STR
        else:
            # Two-char escape (e.g. ESC M) or a bare ESC: consumed, non-content.
            self._state = _ST_GROUND

    def _consume_csi(self, byte: int) -> None:
        if _CSI_PARAM_MIN <= byte <= _CSI_PARAM_MAX:
            self._csi_params.append(byte)
        elif _CSI_FINAL_MIN <= byte <= _CSI_FINAL_MAX:
            self._finish_csi(byte)
            self._state = _ST_GROUND
        else:
            # Control byte mid-sequence: abort the sequence, count nothing.
            self._state = _ST_GROUND

    def _finish_csi(self, final: int) -> None:
        params = tuple(self._csi_params)
        if final == _CSI_FINAL_TILDE and params == _PASTE_START_PARAMS:
            self._in_paste = True
        elif final == _CSI_FINAL_TILDE and params == _PASTE_END_PARAMS:
            self._in_paste = False
        elif not params and final in (_CSI_ARROW_UP, _CSI_ARROW_DOWN):
            # Up/Down recall history into an empty box -- conservatively content.
            self._buffer.append(final)
        # else: focus (I/O), cursor/device reports (R/c/n), mouse (M/m),
        # left/right arrows (C/D) etc. -- terminal noise, never box content.

    def _consume_str(self, byte: int) -> None:
        if byte == _BEL:
            self._state = _ST_GROUND
        elif byte == _ESC:
            self._state = _ST_STR_ESC

    def _buffer_is_compact(self) -> bool:
        """True when the current line (stripped) begins with the /compact command."""
        text = bytes(self._buffer).decode("utf-8", errors="ignore")
        return text.strip().startswith(_COMPACT_COMMAND_PREFIX)

    def take_compact_submitted(self) -> bool:
        """Return True once if a human `/compact` was submitted, then clear it.

        Edge-triggered and consume-once so a single human `/compact` defers a
        single supervisor evaluation, never a permanent suppression.
        """
        if self._compact_submitted:
            self._compact_submitted = False
            return True
        return False

    @property
    def is_empty(self) -> bool:
        """True when the box holds no non-whitespace human input."""
        return all(byte in _LINE_WHITESPACE_BYTES for byte in self._buffer)


@dataclass
class InputActivity:
    """Tracks observed stdin activity forwarded to the supervised child."""

    bytes_seen: int = 0
    last_input_monotonic: float | None = None
    line: HumanInputLine = field(default_factory=HumanInputLine)

    def record(self, data: bytes) -> None:
        """Record a chunk of stdin data that was forwarded to the child."""
        self.bytes_seen += len(data)
        self.last_input_monotonic = os.times().elapsed
        self.line.feed(data)

    def take_compact_submitted(self) -> bool:
        """Return True once if the human submitted a `/compact` since last checked."""
        return self.line.take_compact_submitted()


@dataclass
class OutputActivity:
    """Tracks child->stdout output timing to tell 'work in progress' from 'settled'.

    Plan 00152: while a turn is generating, the child streams output almost
    continuously; between turns the output falls quiet. Recording the last
    output timestamp lets the supervisor derive a ``work_idle`` signal so the
    LOWER red band can wait for the turn to settle before compacting (restoring
    the pre-Plan-00151 "blocked whilst things were happening" behaviour).
    Supervisor-injected keystrokes go to the PTY master, never here, so an
    injection never marks the child "busy".
    """

    bytes_seen: int = 0
    last_output_monotonic: float | None = None

    def record(self, data: bytes) -> None:
        """Record a chunk of child output that was forwarded to stdout."""
        self.bytes_seen += len(data)
        self.last_output_monotonic = os.times().elapsed


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
# After injecting `/compact`, Claude Code may QUEUE it behind the in-flight turn
# and not run it until the turn is interrupted (the human normally presses
# [esc]). If no compaction starts within this window, the supervisor injects a
# single [esc] to flush the queued command. Must be < await_timeout so the ESC
# fires before the machine gives up and returns to MONITOR (Plan 00151).
_DEFAULT_ESCAPE_AFTER_SECONDS = 60.0
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


# NOOP reasons that mean "an injection is pending but gated on the session
# being safe to type into". `_poll_once` uses these to log a deferral when
# the gate was the non-empty input box rather than keystroke activity.
_REASON_BUSY_COMPOSING = "session busy (composing)"
_REASON_BUSY_AWAIT_RESUME = "compaction detected but session busy -> awaiting idle to resume"
_INJECTION_GATED_REASONS = frozenset({_REASON_BUSY_COMPOSING, _REASON_BUSY_AWAIT_RESUME})
# Plan 00152: in the LOWER red band (red but below the compact-urgency midpoint)
# the supervisor is PATIENT -- it defers /compact until the child output has
# settled (work_idle), so an in-progress turn is never interrupted. This
# restores the pre-Plan-00151 "blocked whilst things were happening" behaviour
# for the red band only; the elevated + critical bands still act promptly.
_REASON_RED_WORK_IN_PROGRESS = "red (patient band) but work in progress -> deferring until settled"
_DEFERRED_LOG_PREFIX = "injection deferred: input box not empty"


class Decision(enum.Enum):
    """What the supervisor WOULD do this evaluation (dry-run logs it)."""

    NOOP = "noop"
    WOULD_COMPACT = "would-compact"
    WOULD_CONTINUE = "would-continue"
    WOULD_ESCAPE = "would-escape"


class SupervisorState(enum.Enum):
    """Two-state compact-and-resume machine (Decision H)."""

    MONITOR = "monitor"
    AWAIT_COMPACTING = "await-compacting"


@dataclass(frozen=True)
class SidecarReading:
    """A parsed snapshot of the daemon-written context sidecar.

    ``compact_urgent`` (Plan 00152) is the midpoint band between red and
    critical: the supervisor stays PATIENT in the lower red band (waits for the
    child to settle before compacting) and acts PROMPTLY once the context is
    compact-urgent (or critical) even while the child is streaming. Defaults
    False for older sidecars that predate the field.
    """

    red: bool
    critical: bool
    compact_urgent: bool
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
    escape_after_seconds: float = _DEFAULT_ESCAPE_AFTER_SECONDS


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

    The daemon writes the sidecar to ``daemon_untracked_dir()/context-sidecar``,
    whose location is INSTALL-MODE-AWARE (see ``ProjectContext``):

    - normal client install: ``{project}/.claude/hooks-daemon/untracked``
    - self-install (the daemon's own repo): ``{project}/untracked``

    We must resolve the SAME directory or we poll a path the daemon never writes
    (the v3.34.0 bug: the compact trigger was permanently inert in every normal
    client install because this hardcoded the self-install layout). Install mode
    is detected exactly as the daemon does: self-install iff the daemon SOURCE
    tree ``{project}/src/claude_code_hooks_daemon`` is present at the project
    root. This is stdlib-only (no daemon import) and stable at startup regardless
    of whether the sidecar directory exists yet.

    Uses ``$CLAUDE_PROJECT_DIR`` (the project root, exported by ccy in-container),
    falling back to the current working directory when the variable is unset.
    """
    project_dir = Path(os.environ.get("CLAUDE_PROJECT_DIR") or Path.cwd())
    self_install = (project_dir / "src" / "claude_code_hooks_daemon").exists()
    if self_install:
        daemon_untracked = project_dir / "untracked"
    else:
        daemon_untracked = project_dir / ".claude" / "hooks-daemon" / "untracked"
    return daemon_untracked / _SIDECAR_SUBDIR


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
        critical=bool(freshest_data.get("critical", False)),
        compact_urgent=bool(freshest_data.get("compact_urgent", False)),
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
    cap allows it, decide WOULD_COMPACT and move to AWAIT_COMPACTING. Plan 00152
    splits "red or worse" into graduated bands: in the LOWER red band
    (``reading.red`` but NOT ``reading.compact_urgent``) the machine is PATIENT
    -- it defers the compaction while ``work_idle`` is False so an in-progress
    turn is never interrupted (restoring the pre-Plan-00151 behaviour). In the
    ELEVATED band (``reading.compact_urgent``) and at CRITICAL it compacts
    PROMPTLY even while the child streams. The post-compact cooldown gates a
    non-critical repeat, but CRITICAL context (``reading.critical``) bypasses it
    so a dangerously-high context compacts on the very next idle tick (Plan
    00151). A detected HUMAN ``/compact`` (``human_compact_submitted``) instead
    moves to AWAIT_COMPACTING WITHOUT injecting, so the supervisor never stacks a
    duplicate compaction.

    AWAIT_COMPACTING: wait for a compaction to start (handled above). If none
    starts within ``escape_after_seconds``, decide WOULD_ESCAPE ONCE so the
    supervisor can inject ``[esc]`` to flush a ``/compact`` that Claude Code
    queued behind the in-flight turn. Per Plan 00152 the ESC flush is reserved
    for CRITICAL: it fires only when the compaction was CRITICAL-driven (latched
    at inject time) OR the live reading is currently critical -- never for an
    elevated-band compact and never for a human-originated await. If none starts
    within the longer ``await_timeout_seconds``, give up and return to MONITOR so
    a missed transition cannot wedge the machine forever.
    """

    def __init__(self, policy: CompactPolicy) -> None:
        self._policy = policy
        self.state = SupervisorState.MONITOR
        self._injections = 0
        self._last_action_ts: float | None = None
        self._compaction_handled = False
        # ESC-flush bookkeeping for the current AWAIT_COMPACTING episode.
        self._escape_sent = False
        # True when the current AWAIT was entered by a HUMAN /compact (not the
        # supervisor's own inject) -- suppresses the supervisor ESC flush.
        self._await_is_human = False
        # True when the compaction that opened the current AWAIT was
        # CRITICAL-driven -- gates the ESC flush to critical only (Plan 00152).
        self._await_escalate = False

    def evaluate(
        self,
        reading: SidecarReading | None,
        *,
        idle: bool,
        now: float,
        human_compact_submitted: bool = False,
        work_idle: bool = True,
    ) -> Evaluation:
        """Advance the machine one step and return what it WOULD do.

        ``human_compact_submitted`` is an edge signal: the human just submitted a
        ``/compact``. The machine then enters AWAIT_COMPACTING WITHOUT injecting,
        so the supervisor never stacks a second ``/compact`` on top of the
        human's (Claude Code aborts the duplicate). It still resumes afterwards
        via the normal compaction-signal path.

        ``work_idle`` (Plan 00152) is True when the child has produced no output
        recently (the turn has settled). It only gates the LOWER red band: an
        elevated-band or critical reading compacts regardless of ``work_idle``.
        """
        compacting = reading is not None and reading.compacting
        if compacting:
            if self._compaction_handled:
                # Already resumed this episode; sit tight until compaction ends.
                return Evaluation(Decision.NOOP, "compaction in progress (already resumed)")
            if not idle:
                # Never type `continue` into a busy TUI -- it would be lost or
                # corrupt in-flight input. Do NOT latch: retry on the next idle
                # poll so the resume still fires once the session settles.
                return Evaluation(Decision.NOOP, _REASON_BUSY_AWAIT_RESUME)
            self._compaction_handled = True
            self._last_action_ts = now
            self._enter_monitor()
            return Evaluation(
                Decision.WOULD_CONTINUE,
                "compaction detected -> would inject continue",
            )

        # No compaction under way: reset the latch and run normal logic.
        self._compaction_handled = False

        # A human /compact wins over any supervisor action: enter AWAIT without
        # injecting so we don't double-compact. Marked human so ESC-flush stays
        # off (the human owns flushing their own queued command).
        if human_compact_submitted and self.state is SupervisorState.MONITOR:
            self._last_action_ts = now
            self._enter_await(is_human=True)
            return Evaluation(
                Decision.NOOP, "human /compact detected -> awaiting compaction (no inject)"
            )

        if self.state is SupervisorState.AWAIT_COMPACTING:
            return self._evaluate_await(reading, idle=idle, now=now)
        return self._evaluate_monitor(reading, idle=idle, work_idle=work_idle, now=now)

    def _evaluate_monitor(
        self, reading: SidecarReading | None, *, idle: bool, work_idle: bool, now: float
    ) -> Evaluation:
        if reading is None:
            return Evaluation(Decision.NOOP, "no sidecar reading")
        if reading.stale:
            return Evaluation(Decision.NOOP, "sidecar stale")
        if not reading.red:
            return Evaluation(Decision.NOOP, f"not red (tier={reading.tier})")
        if not idle:
            return Evaluation(Decision.NOOP, _REASON_BUSY_COMPOSING)
        # Plan 00152 graduated bands: critical is always urgent. In the LOWER red
        # band (red but not urgent) stay PATIENT -- defer until the child output
        # has settled -- so an in-progress turn is never interrupted. The
        # elevated + critical bands act promptly even while the child streams.
        urgent = reading.compact_urgent or reading.critical
        if not urgent and not work_idle:
            return Evaluation(Decision.NOOP, _REASON_RED_WORK_IN_PROGRESS)
        if self._injections >= self._policy.max_injections:
            return Evaluation(Decision.NOOP, "injection cap reached")
        # CRITICAL bypasses the cooldown so a dangerously-high context compacts
        # on the very next idle tick instead of waiting the cooldown out.
        if not self._cooldown_elapsed(now, critical=reading.critical):
            return Evaluation(Decision.NOOP, "cooldown active")

        self._injections += 1
        self._last_action_ts = now
        # Latch the ESC-flush escalation to critical: only a critical-driven
        # compaction is allowed to interrupt the turn with [esc] (Plan 00152).
        self._enter_await(is_human=False, escalate=reading.critical)
        urgency = "CRITICAL" if reading.critical else ("urgent" if urgent else "red")
        return Evaluation(
            Decision.WOULD_COMPACT,
            f"{urgency} at {reading.pct:.0f}% + idle -> would inject /compact",
        )

    def _evaluate_await(
        self, reading: SidecarReading | None, *, idle: bool, now: float
    ) -> Evaluation:
        if self._await_timed_out(now):
            self._enter_monitor()
            return Evaluation(Decision.NOOP, "await-compacting timed out -> back to monitor")
        # ESC-flush: a supervisor /compact that Claude Code queued behind the
        # in-flight turn needs an [esc] to run. Reserved for CRITICAL (Plan
        # 00152): fire only when the compaction was critical-driven OR the live
        # reading is currently critical, once, when idle, never for a human await.
        live_critical = reading is not None and not reading.stale and reading.critical
        escalate = self._await_escalate or live_critical
        if (
            not self._await_is_human
            and escalate
            and not self._escape_sent
            and idle
            and self._escape_due(now)
        ):
            self._escape_sent = True
            return Evaluation(
                Decision.WOULD_ESCAPE,
                "critical compaction not started -> would inject [esc] to flush queued /compact",
            )
        return Evaluation(Decision.NOOP, "awaiting compaction start")

    def _enter_await(self, *, is_human: bool, escalate: bool = False) -> None:
        """Enter AWAIT_COMPACTING, resetting the per-episode ESC-flush latch.

        ``escalate`` records whether this compaction was CRITICAL-driven, which
        gates the supervisor ESC flush to critical only (Plan 00152).
        """
        self.state = SupervisorState.AWAIT_COMPACTING
        self._escape_sent = False
        self._await_is_human = is_human
        self._await_escalate = escalate

    def _enter_monitor(self) -> None:
        """Return to MONITOR, clearing per-episode AWAIT bookkeeping."""
        self.state = SupervisorState.MONITOR
        self._escape_sent = False
        self._await_is_human = False
        self._await_escalate = False

    def _cooldown_elapsed(self, now: float, *, critical: bool = False) -> bool:
        if critical:
            return True
        if self._last_action_ts is None:
            return True
        return (now - self._last_action_ts) >= self._policy.cooldown_seconds

    def _escape_due(self, now: float) -> bool:
        if self._last_action_ts is None:
            return False
        return (now - self._last_action_ts) >= self._policy.escape_after_seconds

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
# The real ESC byte injected (armed) to interrupt the current turn so a QUEUED
# `/compact` runs. It is an interrupt KEY, not a line: injected raw with NO
# trailing Enter. ESC is harmless (it only interrupts), but it DOES affect the
# session, so in dry-run a visible marker is shown instead of a real ESC.
_ESC_PAYLOAD = "\x1b"
_DRY_RUN_ESCAPE_MARKER = (
    f"{_BOT_PREFIX} would send [esc] to flush a queued /compact " "(dry-run — no real ESC sent)"
)
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
# Plan 00152: the LOWER red band waits for the child to be quiet this long before
# compacting. It must exceed the poll interval so a full quiet tick has to pass
# with no child output -- faithfully mirroring the pre-Plan-00151 behaviour where
# the tick only ran on a clear `select` timeout (a genuine lull). Between-turn
# lulls easily exceed this; an actively streaming turn never does.
_DEFAULT_WORK_SETTLE_SECONDS = 3.0


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
    if decision is Decision.WOULD_ESCAPE:
        return _DRY_RUN_ESCAPE_MARKER if dry_run else _ESC_PAYLOAD
    return None


def _perform_injection(
    master_writer: Callable[[bytes], None],
    payload: str,
    *,
    submit: bool = True,
    sleep: Callable[[float], None] = time.sleep,
) -> None:
    """Type ``payload`` into the child PTY, optionally submitting it with Enter.

    The submit (carriage return) is a distinct write, delayed by
    ``_SUBMIT_DELAY_SECONDS`` from the payload, so the TUI registers a real Enter
    instead of absorbing a trailing CR into its multi-line input box (which left
    a long ``/compact`` line sitting unsubmitted). ``sleep`` is injectable so
    tests do not actually pause.

    ``submit=False`` writes the payload with NO trailing Enter -- used for the
    raw ESC interrupt, which is a keypress, not a line to submit.
    """
    master_writer(payload.encode("utf-8"))
    if not submit:
        return
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


def _is_work_idle(
    output_activity: OutputActivity, *, now_monotonic: float, work_settle_seconds: float
) -> bool:
    """True when the child has produced no output within ``work_settle_seconds``.

    Plan 00152: distinguishes an actively-streaming turn (recent output -> NOT
    work-idle) from a settled session (a lull -> work-idle). A child that has
    produced no output at all yet counts as settled.
    """
    if output_activity.last_output_monotonic is None:
        return True
    return (now_monotonic - output_activity.last_output_monotonic) >= work_settle_seconds


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
    input_line_empty: bool = True,
    human_compact_submitted: bool = False,
    work_idle: bool = True,
) -> Evaluation:
    """One supervisor tick: read the sidecar, decide, and inject if warranted.

    Reads the freshest sidecar AND the compaction signal, advances the state
    machine, and -- for a non-NOOP decision -- injects the resolved payload
    (a marker for a dry-run compact; the real command otherwise) and logs it.
    Returns the Evaluation so callers/tests can observe the decision.

    ``input_line_empty`` is the empty-input-box guard: when False (the human
    has non-whitespace text sitting in the input box), NO injection may fire
    on this tick -- pasting into and submitting a half-typed human message is
    data corruption. The gate is applied to the machine's ``idle`` input, so
    every injection path (armed ``/compact``, ``continue``, dry-run marker,
    ESC flush) defers via the machine's existing busy semantics: no latch, no
    cooldown, the compaction signal stays in place, and the injection retries
    on the next tick with an empty box.

    ``human_compact_submitted`` is the edge signal that the human just submitted
    a ``/compact``; it makes the machine await the compaction instead of
    injecting its own, so the supervisor never stacks a duplicate compaction.

    ``work_idle`` (Plan 00152) is True when the child output has settled; it
    only gates the LOWER red band (elevated/critical readings compact promptly).
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
                critical=False,
                compact_urgent=False,
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
    # Empty-input-box guard: the machine only ever decides to inject when its
    # `idle` input is True, so AND-ing the box state into `idle` guards every
    # injection path without new machine states -- the busy branches already
    # defer-and-retry correctly (no latch, no cooldown, signal kept).
    can_inject = idle and input_line_empty
    evaluation = machine.evaluate(
        reading,
        idle=can_inject,
        now=now_wall,
        human_compact_submitted=human_compact_submitted,
        work_idle=work_idle,
    )
    payload = _resolve_payload(evaluation.decision, dry_run=dry_run)
    if payload is not None:
        # The raw ESC is an interrupt key, not a line -- inject it WITHOUT a
        # trailing Enter. Every other payload (compact / continue / markers) is
        # a line and submits normally.
        submit = not (evaluation.decision is Decision.WOULD_ESCAPE and not dry_run)
        _perform_injection(master_writer, payload, submit=submit)
        if log is not None:
            log.write(f"{evaluation.decision.value}: {evaluation.reason}; injected {payload!r}")
        # Consume the signal ONLY after a resume actually fired, so a busy-gated
        # NOOP leaves it in place to retry on the next idle poll.
        if evaluation.decision is Decision.WOULD_CONTINUE and signal_path is not None:
            _consume_signal(signal_path, log)
    elif (
        log is not None
        and idle
        and not input_line_empty
        and evaluation.reason in _INJECTION_GATED_REASONS
    ):
        # An injection was pending and the NON-EMPTY INPUT BOX was the sole
        # gate (keystrokes were idle) -- record the skip.
        log.write(f"{_DEFERRED_LOG_PREFIX} ({evaluation.reason})")
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
    output_activity: OutputActivity | None = None,
) -> None:
    """Select loop: forward stdin -> master, master -> stdout.

    When ``poll_seconds`` is set, ``select`` wakes on that interval even with no
    I/O, and ``on_poll`` (if given) runs one supervisor tick before looping.
    A tick never touches the forwarded I/O -- it only reads the sidecar and may
    inject -- so transparent passthrough is unchanged when polling is disabled.

    Once stdin reaches EOF it is dropped from the watch set: an EOF fd is always
    "readable", so continuing to select on it would spin the loop and starve the
    poll timeout. Dropping it lets timeouts (and therefore polling) resume.

    The supervisor tick fires on a MONOTONIC interval, not on the ``select``
    timeout branch. A busy child streams output continuously, so ``master_fd``
    is readable on almost every ``select`` call and the timeout branch would
    never run -- starving the compact decision for the whole busy burst (Plan
    00151: context climbed past red before any evaluation). Tracking the next
    tick deadline and running ``on_poll`` whenever it is due -- readable or not
    -- evaluates the decision every ``poll_seconds`` regardless of how busy the
    thread is; resetting the deadline after each run throttles it to one tick
    per interval.
    """
    stdin_open = True
    # tick_interval is the monotonic period between supervisor ticks; it is
    # None (polling disabled) only when a caller omits poll_seconds/on_poll.
    tick_interval = poll_seconds if (poll_seconds is not None and on_poll is not None) else None
    next_tick = time.monotonic() + tick_interval if tick_interval is not None else None
    while True:
        watch = [master_fd, stdin_fd] if stdin_open else [master_fd]
        if tick_interval is not None and next_tick is not None:
            timeout: float | None = max(0.0, next_tick - time.monotonic())
        else:
            timeout = poll_seconds
        try:
            readable, _, _ = select.select(watch, [], [], timeout)
        except OSError as exc:
            if exc.errno == errno.EINTR:
                continue
            raise

        # Run the supervisor tick whenever its interval has elapsed -- even if
        # the child is mid-stream and select keeps returning readable.
        if (
            tick_interval is not None
            and next_tick is not None
            and on_poll is not None
            and time.monotonic() >= next_tick
        ):
            on_poll()
            next_tick = time.monotonic() + tick_interval

        if not readable:
            # select timed out with no I/O ready -> nothing to forward.
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
            if output_activity is not None:
                # Record child output timing so the supervisor can tell an
                # actively-streaming turn from a settled lull (Plan 00152).
                output_activity.record(output)
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
    work_settle_seconds: float = _DEFAULT_WORK_SETTLE_SECONDS,
) -> int:
    """Run `argv` under a PTY, forwarding I/O and polling the context sidecar.

    On each idle poll tick the supervisor reads the daemon-written context
    sidecar and runs the Decision H state machine. When the compact trigger
    fires (red + idle + cooldown/cap) it INJECTS a payload into the child PTY:
    a harmless visible MARKER in dry-run (default), or the real `/compact` when
    armed. Injection only ever targets an EMPTY input box: the human input
    line is tracked from the forwarded stdin bytes (`activity.line`) and any
    tick that finds non-whitespace human text pending is deferred and logged
    instead. Forwarded I/O is never altered by a tick.

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
        work_settle_seconds: Minimum quiet time on the CHILD output before the
            lower red band is allowed to compact (Plan 00152). The elevated and
            critical bands ignore it and compact promptly.

    Returns:
        The child's exit code (or 128+signal if it died from a signal).

    Raises:
        ValueError: If `argv` is empty.
    """
    if not argv:
        raise ValueError("supervise() requires a non-empty argv")

    activity = activity if activity is not None else InputActivity()
    output_activity = OutputActivity()
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
        now_monotonic = os.times().elapsed
        idle = _is_idle(
            activity,
            now_monotonic=now_monotonic,
            idle_floor_seconds=idle_floor_seconds,
        )
        # Child-output lull: gates only the LOWER red band (Plan 00152).
        work_idle = _is_work_idle(
            output_activity,
            now_monotonic=now_monotonic,
            work_settle_seconds=work_settle_seconds,
        )
        # Consume the human-/compact edge exactly once per tick so a human
        # compaction defers the supervisor's own, never suppresses it forever.
        human_compact = activity.take_compact_submitted()
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
            input_line_empty=activity.line.is_empty,
            human_compact_submitted=human_compact,
            work_idle=work_idle,
        )

    previous_handler = signal.signal(signal.SIGWINCH, _on_winch)

    try:
        _forward_io(
            stdin_fd,
            master_fd,
            activity,
            poll_seconds=poll_seconds,
            on_poll=_on_poll,
            output_activity=output_activity,
        )
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
