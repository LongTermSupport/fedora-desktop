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

It also GUARDS the session against accidental terminal control keys that would
otherwise freeze or kill it: Ctrl+Z (SUSP) is stripped from the forwarded input
(``strip_suspend``) AND, belt-and-braces, the stop/quit SIGNALS are swallowed if
ever delivered (``install_input_signal_guards``: SIGTSTP + SIGQUIT, plus ignored
SIGTTIN/SIGTTOU). Ctrl+C (SIGINT) is deliberately left working. Each swallow
surfaces a transient status-line notice via the message channel below.

Usage:
    claude-supervise.py [--dry-run | --arm] [--log PATH] -- <child argv...>

THREAD / PROCESS SAFETY (FIRST-CLASS CONCERN — read before touching shared
state or any file under the ``supervise/`` runtime dir).

The supervisor does NOT run in isolation. Every file it writes under the daemon
untracked dir (``supervise/``: the status file, the message file, sidecars,
signal files) is concurrently accessed by OTHER processes and threads:

  * the supervisor HOST process (the select loop),
  * the ``--worker`` decision SUBPROCESS (a separate pid),
  * the DAEMON (a different process entirely) reading these files on every
    status-line render,
  * MULTIPLE Claude sessions that may share one daemon (Plan 00127).

Non-negotiable rules for anything in this area:

  1. WRITES ARE ATOMIC-REPLACE ONLY. Write to a PRIVATE temp file, then
     ``os.replace`` (atomic rename on POSIX) — never write a shared file in
     place. A reader therefore always sees either the old or the new COMPLETE
     file, never a partial one. Temp names carry the pid (and, where more than
     one thread may write, the thread id) so concurrent writers never share a
     temp path. See ``write_supervisor_status`` and ``write_status_message``
     for the canonical form; last writer wins.
  2. READS ARE FAIL-SILENT AND DEFENSIVE. A missing / malformed / partial /
     foreign-schema file yields "no result", never an exception — a bad file
     must never wedge the supervisor or break the status line.
  3. IN-PROCESS SHARED MUTABLE STATE IS LOCK-GUARDED. Any state touched from
     more than one thread (e.g. a poster's rate-limit counter) uses a
     ``threading.Lock`` around the check-and-update. See ``StatusMessagePoster``.

The paired daemon-side reader guidance lives in
``src/claude_code_hooks_daemon/handlers/status_line/CLAUDE.md`` and
``CLAUDE/Architecture/StatusLine.md``.
"""

from __future__ import annotations

import argparse
import enum
import errno
import fcntl
import hashlib
import json
import os
import pty
import select
import signal
import struct
import subprocess  # nosec B404 - spawns ONLY `python3 <self> --worker`, a fixed argv, never a shell
import sys
import termios
import threading
import time
import traceback
import tty
from collections.abc import Callable
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from types import FrameType
    from typing import TextIO

# Supervisor version. Kept in lockstep with the daemon version at release time
# (see CLAUDE/development/RELEASING.md). Display-only for the banner and the
# runtime status file; staleness detection (Plan 00164 Phase 3) uses a content
# hash of THIS file so it is correct even between version bumps.
__version__ = "3.46.0"

# Absolute path to THIS running script — hashed for staleness detection so the
# daemon can tell when the on-disk supervisor differs from the running one.
_SELF_PATH = Path(__file__).resolve()

_READ_CHUNK_SIZE = 4096
_FALLBACK_WINSIZE = struct.pack("HHHH", 24, 80, 0, 0)
_LOG_SUBDIRECTORY = "supervise"
_LOG_FILENAME = "decision.log"
# Runtime identity file the running supervisor writes for staleness detection
# (Plan 00164 Phase 3). Lives in the same 'supervise' subdir as the decision log.
_SUPERVISOR_STATUS_FILENAME = "supervisor-status.json"

_USAGE = "Usage: claude-supervise.py [--dry-run | --arm] [--log PATH] -- <child argv...>\n"

# Opt-out env var for the startup banner + spinner (any non-empty value silences
# them). The banner is also skipped whenever stderr is not a TTY (piped output,
# the test suite, non-interactive launches).
_NO_BANNER_ENV = "CLAUDE_SUPERVISE_NO_BANNER"

# Policy-worker split (Plan 00164 Phase 4). The host spawns `python3 <self>
# --worker` and streams TickFacts to it; the worker runs the decision logic and
# streams TickOutcomes back. A host-side restart of the worker hot-reloads the
# decision code without touching the PTY/child. Set the opt-out env to force the
# in-process path (the same code runs either way — the worker just isolates it).
_WORKER_FLAG = "--worker"
_NO_WORKER_ENV = "CLAUDE_SUPERVISE_NO_WORKER"
# A hung worker must never stall the PTY host: its reply is awaited at most this
# long, after which the host falls back to an in-process decision for that tick.
_WORKER_READ_TIMEOUT_SECONDS = 2.0
# How often the host re-checks the on-disk supervisor fingerprint to hot-reload
# the worker (cheap mtime pre-check gates the hash).
_WORKER_RELOAD_CHECK_SECONDS = 5.0

# Braille spinner frames for the brief pre-fork "starting up" flourish.
_SPINNER_FRAMES = ("⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏")
_SPINNER_INTERVAL_SECONDS = 0.08
_BANNER_RULE = "━" * 46


def render_startup_banner(*, version: str, armed: bool) -> str:
    """Return the multi-line ECHD ccy supervisor startup banner.

    Pure and deterministic: it takes the version + mode and returns text. The
    caller decides whether and where to print it (see ``_should_show_banner``),
    so this stays trivially testable. Left-aligned content under a top/bottom
    rule avoids fragile right-border alignment across variable version/mode
    widths.

    Args:
        version: Supervisor version string (e.g. ``"3.41.0"``).
        armed: True when the supervisor injects a real ``/compact`` (armed);
            False for the harmless dry-run marker.

    Returns:
        The banner as a multi-line string (no trailing newline).
    """
    mode = (
        "ARMED — injects a real /compact when the context goes red"
        if armed
        else "dry-run — injects a harmless visible marker only"
    )
    return "\n".join(
        (
            f"┏━ ECHD ⟐ ccy Supervisor {_BANNER_RULE[24:]}",
            f"┃  v{version}  ·  wrapping claude on a PTY",
            f"┃  {mode}",
            "┗━ starting up ⏳",
        )
    )


def _should_show_banner(stream: object, env: dict[str, str] | None = None) -> bool:
    """Return True iff the startup banner/spinner should be shown on ``stream``.

    Shown only for an interactive launch: ``stream`` must be a TTY and the
    opt-out env var must be unset. A non-tty stream (piped output, the test
    harness, a non-interactive launch) is always silent.
    """
    resolved_env = env if env is not None else dict(os.environ)
    if resolved_env.get(_NO_BANNER_ENV):
        return False
    isatty = getattr(stream, "isatty", None)
    return bool(callable(isatty) and isatty())


class _StartupSpinner:
    """A brief 'starting up' spinner on a background thread.

    It runs ONLY between the banner and the PTY fork and is always stopped —
    with its line cleared — before the wrapped child takes over the terminal, so
    it never coexists with the child's output. Any write error (closed/redirected
    stream) silently ends the animation; the spinner is cosmetic and must never
    affect the supervised session.
    """

    def __init__(self, stream: object, *, label: str = "starting claude") -> None:
        self._stream = stream
        self._label = label
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None

    def start(self) -> None:
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    def _run(self) -> None:
        index = 0
        while not self._stop.is_set():
            frame = _SPINNER_FRAMES[index % len(_SPINNER_FRAMES)]
            if not self._write(f"\r  {frame} {self._label}… "):
                return
            index += 1
            self._stop.wait(_SPINNER_INTERVAL_SECONDS)

    def stop(self) -> None:
        self._stop.set()
        if self._thread is not None:
            self._thread.join(timeout=1.0)
            self._thread = None
        # Clear the spinner line so the child starts on a clean row.
        self._write("\r" + " " * (len(self._label) + 12) + "\r")

    def _write(self, text: str) -> bool:
        """Best-effort write+flush; return False if the stream is unusable."""
        write = getattr(self._stream, "write", None)
        flush = getattr(self._stream, "flush", None)
        if not callable(write):
            return False
        try:
            write(text)
            if callable(flush):
                flush()
        except (OSError, ValueError):
            return False
        return True


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

# Ctrl+Z (SUSP, 0x1a): universal "undo" muscle memory, but a terminal turns it
# into SIGTSTP (suspend). Under the supervisor the outer terminal is raw so it
# arrives as this byte rather than a signal (see `_forward_io`); the input guard
# strips it from the forwarded stream so Ctrl+Z can never reach the child PTY or
# suspend the session — it becomes an inert, ignored keystroke (upstream
# anthropics/claude-code#43596). 0x1a has no legitimate meaning in the input box.
_SUSPEND_BYTE = 0x1A


def strip_suspend(data: bytes) -> bytes:
    """Return ``data`` with every Ctrl+Z (SUSP, ``0x1a``) byte removed.

    The supervisor forwards operator stdin to the child byte-for-byte; this
    filter drops the suspend byte so Ctrl+Z can never reach the child PTY or
    suspend the session. ``0x1a`` has no legitimate meaning in Claude's input
    line, so removing it is safe. Returns ``data`` unchanged (the same object)
    when no suspend byte is present — the overwhelming common case on the hot
    input-forwarding path, so the filter allocates nothing in the fast path.
    """
    if _SUSPEND_BYTE not in data:
        return data
    return bytes(byte for byte in data if byte != _SUSPEND_BYTE)


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
        # Dedup state for `write_noop` (Plan 00168 Phase 1): the last NOOP-reason
        # message written, so a gate held unchanged for minutes logs ONCE rather
        # than flooding the log every ~1-2s idle tick. Any real `write` (an
        # injection / deferral / reap / lifecycle line) resets it so the next
        # NOOP re-logs and the file stays a faithful transition record.
        self._last_noop_message: str | None = None

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
        # A real action line breaks the NOOP-dedup run: the next identical NOOP
        # reason must re-log (it describes a fresh post-action observation).
        self._last_noop_message = None

    def write_noop(self, message: str) -> None:
        """Append a NOOP-reason line, suppressing a CONSECUTIVE identical repeat.

        Plan 00168 Phase 1 observability: the supervisor records WHY each idle
        tick did nothing (the gate that blocked -- ``sidecar stale`` /
        ``cooldown active`` / ``not idle`` / ``no sidecar reading`` / ...), so a
        red-but-not-compacting session is diagnosable from the log alone. The
        message is low-cardinality (the reason plus a coarse context band, never
        a per-tick pct), so deduping on the exact message keeps a steady gate to
        a single line while still logging every gate TRANSITION.

        Args:
            message: The NOOP-reason line to record if it differs from the last.

        Raises:
            OSError: If the file cannot be written (never swallowed).
        """
        if message == self._last_noop_message:
            return
        timestamp = datetime.now(UTC).isoformat()
        with self._path.open("a", encoding="utf-8") as handle:
            handle.write(f"{timestamp} {message}\n")
        self._last_noop_message = message


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
# Plan 00160: the supervisor drives ONE PTY and injects into whatever Agent-View
# thread is FOREGROUND. Only the foreground thread renders its statusLine, so the
# freshest sidecar is normally the foreground -- EXCEPT in the brief window right
# after a thread switch, when the just-backgrounded thread's sidecar is still
# fresh and could momentarily be the freshest. When a SECOND still-fresh sidecar's
# ts is within this margin of the freshest, the foreground is AMBIGUOUS and a
# compaction is deferred (it would risk compacting the wrong thread). Sized at
# roughly one status refresh interval: a live foreground re-renders every interval
# so it reliably pulls this far ahead of a non-rendering backgrounded thread,
# self-resolving the ambiguity within a tick or two.
_DEFAULT_FOREGROUND_MARGIN_SECONDS = 10.0
_DEFAULT_COOLDOWN_SECONDS = 300.0
_DEFAULT_AWAIT_TIMEOUT_SECONDS = 120.0
_DEFAULT_MAX_INJECTIONS = 20
# After injecting `/compact`, Claude Code may QUEUE it behind the in-flight turn
# and not run it until the turn is interrupted (the human normally presses
# [esc]). If no compaction starts within this window, the supervisor injects an
# [esc] to flush the queued command — and RE-FIRES every window until the
# compaction starts or `max_escapes` is reached (Plan 00164 dogfooding fix).
_DEFAULT_ESCAPE_AFTER_SECONDS = 60.0
# Cap on the repeated [esc] flushes for one queued /compact. Plan 00164: the
# supervisor keeps pressing [esc] ("until it does") but must eventually give up
# so a genuinely-wedged session returns to MONITOR rather than escaping forever.
_DEFAULT_MAX_ESCAPES = 5
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

# Nothing else deletes per-session sidecars/signals: every session writes its own
# ``{session}.json`` (and, on compaction, ``{session}.compacting``) into the ONE
# shared context-sidecar dir, and a closed/backgrounded session's file lingers
# forever (a live dir was observed holding 17 files, most dead for days). The
# supervisor reaps files whose mtime is older than this TTL each tick. It is set
# WELL beyond both the sidecar freshness window and the compaction-signal TTL, so
# a file this old is definitively from a closed session and can never still be
# actionable -- reaping it cannot race a live decision.
_DEFAULT_REAP_TTL_SECONDS = 1800.0
_CONTEXT_SIDECAR_GLOB = "*.json"


# ---------------------------------------------------------------------------
# Own-session identity (Plan 00166): namespace-broad session-id filtering.
#
# Two `ccy` terminals in the SAME repo share ONE context-sidecar dir (a
# bind-mounted `untracked/`), but each runs in its OWN container / PID
# namespace. Without a filter, this supervisor reads the freshest sidecar and
# the first `*.compacting` signal in the shared dir REGARDLESS of which session
# wrote them -- so a compaction in terminal B makes terminal A's supervisor
# inject `continue` into A's PTY (the reported cross-injection bug).
#
# Fix (Decision 1, option B): a session id reachable in the supervisor's OWN
# PID namespace belongs to the supervisor's own Claude instance -- a foreign
# terminal is a separate container and never appears here. So we learn the
# own-session-id SET by scanning the container's process environs for
# `CLAUDE_CODE_SESSION_ID`, then act ONLY on sidecars/signals in that set. Fail
# safe: an empty (not-yet-learned / non-Linux) set means act on NOTHING.
#
# Caveat: under a shared PID namespace (e.g. `podman run --pid=host`) the scan
# could see a sibling terminal's ids. For that deployment give the session its
# own runtime via CLAUDE_HOOKS_SOCKET_PATH / _PID_PATH / _LOG_PATH.
# ---------------------------------------------------------------------------

_PROC_ROOT = "/proc"
_SESSION_ENV_PREFIX = b"CLAUDE_CODE_SESSION_ID="

# Accumulated own-session ids. Union-only: ids are stable per Claude process
# and only ever come from this process's own namespace, so growth never admits
# a foreign terminal. Keeps ids learned during active work available on later
# idle ticks (when no descendant currently exposes the env var).
_own_session_ids_cache: set[str] = set()


def _session_ids_from_environ(environ: bytes) -> set[str]:
    """Extract ``CLAUDE_CODE_SESSION_ID`` value(s) from a NUL-delimited environ."""
    found: set[str] = set()
    for entry in environ.split(b"\x00"):
        if entry.startswith(_SESSION_ENV_PREFIX):
            value = entry[len(_SESSION_ENV_PREFIX) :].decode("utf-8", "replace").strip()
            if value:
                found.add(value)
    return found


def _read_proc_environ(environ_path: Path) -> bytes:
    """Read a ``/proc/<pid>/environ`` blob; empty bytes if the process vanished."""
    try:
        return environ_path.read_bytes()
    except OSError:
        # The process exited between listdir and read, or its environ is
        # unreadable -- it simply contributes no session id. Not an error.
        return b""


def resolve_own_session_ids(proc_root: Path | None = None) -> frozenset[str]:
    """Scan this container's process environs for ``CLAUDE_CODE_SESSION_ID``.

    Returns every session id found in the supervisor's OWN PID namespace
    (namespace-broad identity, Plan 00166). Empty on a host without ``/proc``
    so the caller fails safe.
    """
    root = proc_root if proc_root is not None else Path(_PROC_ROOT)
    if not root.is_dir():
        return frozenset()
    found: set[str] = set()
    for entry in root.iterdir():
        if entry.name.isdigit():
            found |= _session_ids_from_environ(_read_proc_environ(entry / "environ"))
    return frozenset(found)


def cached_own_session_ids(proc_root: Path | None = None) -> frozenset[str]:
    """Union-accumulate and return the supervisor's own-session-id set."""
    _own_session_ids_cache.update(resolve_own_session_ids(proc_root))
    return frozenset(_own_session_ids_cache)


def _session_in_scope(session_id: object, own_sessions: frozenset[str] | None) -> bool:
    """True if ``session_id`` should be acted on given the own-session filter.

    ``own_sessions is None`` disables filtering (legacy / unit-test callers).
    A provided set (even empty) filters: only ids in the set are in scope, so an
    empty set puts NOTHING in scope -- the fail-safe when identity is unknown.
    """
    if own_sessions is None:
        return True
    return isinstance(session_id, str) and session_id in own_sessions


# NOOP reasons that mean "an injection is pending but gated on the session
# being safe to type into". `_poll_once` uses these to log a deferral when
# the gate was the non-empty input box rather than keystroke activity.
_REASON_BUSY_COMPOSING = "session busy (composing)"
_REASON_BUSY_AWAIT_RESUME = "compaction detected but session busy -> awaiting idle to resume"
_INJECTION_GATED_REASONS = frozenset({_REASON_BUSY_COMPOSING, _REASON_BUSY_AWAIT_RESUME})
# Plan 00160: a would-be compaction is deferred because the foreground thread
# cannot be told apart from a just-backgrounded one this tick (two still-fresh
# sidecars within the margin). Self-resolves as the backgrounded sidecar ages out.
_REASON_FOREGROUND_AMBIGUOUS = "foreground ambiguous (recent thread switch) -> deferring compact"
# Plan 00152: in the LOWER red band (red but below the compact-urgency midpoint)
# the supervisor is PATIENT -- it defers /compact until the child output has
# settled (work_idle), so an in-progress turn is never interrupted. This
# restores the pre-Plan-00151 "blocked whilst things were happening" behaviour
# for the red band only; the elevated + critical bands still act promptly.
_REASON_RED_WORK_IN_PROGRESS = "red (patient band) but work in progress -> deferring until settled"
_DEFERRED_LOG_PREFIX = "injection deferred: input box not empty"
# Plan 00168 Phase 1: prefix for the deduped NOOP-reason diagnostic. Every idle
# tick that decides NOOP records `<prefix>: <reason>[ band]` so a
# red-but-not-compacting session names its blocking gate in `decision.log`.
_NOOP_LOG_PREFIX = "noop"


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
    max_escapes: int = _DEFAULT_MAX_ESCAPES
    compaction_signal_ttl_seconds: float = _DEFAULT_COMPACTION_SIGNAL_TTL_SECONDS
    escape_after_seconds: float = _DEFAULT_ESCAPE_AFTER_SECONDS
    reap_ttl_seconds: float = _DEFAULT_REAP_TTL_SECONDS
    foreground_margin_seconds: float = _DEFAULT_FOREGROUND_MARGIN_SECONDS


@dataclass(frozen=True)
class Evaluation:
    """The outcome of one state-machine evaluation."""

    decision: Decision
    reason: str


@dataclass(frozen=True)
class TickFacts:
    """The per-tick inputs only the PTY HOST knows (Plan 00164 Phase 4).

    The host tracks these from the forwarded I/O and clock; the policy worker
    (which owns the state machine and reads the sidecars) is told them each tick.
    Serialised host→worker as JSON.
    """

    now_wall: float
    idle: bool
    input_line_empty: bool
    human_compact_submitted: bool
    work_idle: bool
    # The host's authoritative CompactStateMachine state for this tick (Plan
    # 00164 Phase 4 fix). The worker loads it before deciding so it never runs on
    # divergent state; None on the in-process path (the machine is already live).
    machine_state: dict[str, object] | None = None


@dataclass(frozen=True)
class TickOutcome:
    """The decision produced by one tick — what the HOST should DO (Phase 4).

    Pure data: the worker (or the in-process fallback) decides; the host injects.
    ``payload is None`` means NOOP. ``consume_signal_path`` is the compaction
    signal to delete AFTER a successful resume injection (kept out of the worker
    so a failed PTY write never loses the resume). Serialised worker→host as JSON.
    """

    decision_value: str
    reason: str
    payload: str | None
    submit: bool
    consume_signal_path: str | None
    deferred_log: str | None
    # The machine state AFTER this tick (Plan 00164 Phase 4 fix). The host adopts
    # it as the new authoritative state so the next fallback tick cannot diverge.
    machine_state: dict[str, object] | None = None
    # Plan 00168 Phase 1: a deduped NOOP-reason diagnostic (`noop: <reason>[
    # band]`) the host writes to `decision.log` via `DecisionLog.write_noop`.
    # Set only for NOOP ticks NOT already covered by `deferred_log`. None on
    # action ticks. Kept out of the state machine (host-side dedup owns volume).
    noop_reason_log: str | None = None


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
    return _daemon_untracked_dir() / _SIDECAR_SUBDIR


def _daemon_untracked_dir() -> Path:
    """Resolve the daemon's untracked runtime dir (install-mode-aware).

    Mirrors ``ProjectContext.daemon_untracked_dir()`` without importing the
    daemon: self-install iff the daemon SOURCE tree is present at the project
    root. Used for both the context-sidecar dir and the supervisor status file,
    so the daemon (which writes/reads via ProjectContext) and the standalone
    supervisor always agree on one location.
    """
    project_dir = Path(os.environ.get("CLAUDE_PROJECT_DIR") or Path.cwd())
    self_install = (project_dir / "src" / "claude_code_hooks_daemon").exists()
    if self_install:
        return project_dir / "untracked"
    return project_dir / ".claude" / "hooks-daemon" / "untracked"


_WORKER_ERROR_LOG_NAME = "claude-supervise-worker.err.log"


def worker_error_log_path() -> Path:
    """Absolute path of the supervisor worker's error log (never the PTY).

    The policy worker runs as a subprocess whose stderr MUST NOT reach the
    inherited terminal — a per-tick exception would otherwise flood the live
    Claude session with tracebacks. All worker diagnostics land in this file.
    """
    return _daemon_untracked_dir() / _WORKER_ERROR_LOG_NAME


def open_worker_error_log() -> TextIO | None:
    """Open the worker error log for appending; None if it cannot be opened.

    Used as the worker subprocess's ``stderr`` so nothing it emits — including
    an uncaught interpreter traceback — can reach the inherited PTY. Callers
    fall back to ``os.devnull`` when this returns None; the worker's stderr is
    NEVER left inheriting the terminal.
    """
    try:
        path = worker_error_log_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        return path.open("a", encoding="utf-8", buffering=1)
    except OSError:
        # Cannot open the log — the caller redirects to os.devnull instead. We
        # return a sentinel (None), never the terminal. This is not swallowing:
        # the failure changes the caller's redirect target, it does not hide it.
        return None


def append_worker_error(message: str) -> None:
    """Append a timestamped diagnostic to the worker error log (last resort).

    Best-effort file logging that must itself NEVER raise or write to the PTY —
    it is the safety net's own logger, so a failure here has nowhere left to go
    and is intentionally dropped (see error_hiding exclusion).
    """
    stamp = datetime.now(tz=UTC).strftime("%Y-%m-%d %H:%M:%S")
    try:
        with worker_error_log_path().open("a", encoding="utf-8") as handle:
            handle.write(f"[{stamp}] {message}\n")
    except OSError:
        # Deliberate last-resort drop: the error logger cannot log its own
        # failure anywhere safe (writing to stderr would flood the PTY, which
        # is the very bug this safety net exists to prevent).
        return


def _redirect_worker_stderr_to_log() -> None:
    """Point the worker process's stderr (fd 2 + ``sys.stderr``) at the log file.

    Guarantees the worker can never write to an inherited terminal even if it is
    launched directly. Best-effort: if the log cannot be opened, stderr is left
    as the host already configured it (the Popen redirect), never forced onto a
    tty by this function.
    """
    stream = open_worker_error_log()
    if stream is None:
        return
    try:
        os.dup2(stream.fileno(), sys.stderr.fileno())
    except (OSError, ValueError) as exc:
        # fileno() unavailable or dup2 failed: record it, then fall through to
        # swap the Python-level handle only -- still never a terminal.
        append_worker_error(f"stderr dup2 failed, using handle swap: {exc}")
    sys.stderr = stream


def compute_source_hash(path: Path) -> str:
    """Return a short sha256 hex digest of ``path``'s bytes (staleness key).

    Detects when the on-disk supervisor differs from the running one,
    independent of whether ``__version__`` was bumped. Not security-sensitive —
    a content fingerprint only (hence ``usedforsecurity=False``).
    """
    digest = hashlib.sha256(path.read_bytes(), usedforsecurity=False)
    return digest.hexdigest()[:12]


def _supervisor_status_path(untracked_dir: Path) -> Path:
    """Path to the supervisor status file under the shared 'supervise' subdir."""
    return untracked_dir / _LOG_SUBDIRECTORY / _SUPERVISOR_STATUS_FILENAME


def write_supervisor_status(
    untracked_dir: Path,
    *,
    version: str,
    source_hash: str,
    pid: int,
    started_at: float,
) -> Path | None:
    """Atomically write the running supervisor's identity for staleness checks.

    Records ``version`` + ``source_hash`` (of the running script) + ``pid`` +
    ``started_at`` so a SessionStart advisory can compare the on-disk supervisor
    against the running one. Best-effort: a write failure is reported to stderr
    and returns None rather than disturbing the supervised session.

    Returns:
        The status file path on success, or None on failure.
    """
    status_path = _supervisor_status_path(untracked_dir)
    payload = {
        "version": version,
        "source_hash": source_hash,
        "pid": pid,
        "started_at": started_at,
    }
    try:
        status_path.parent.mkdir(parents=True, exist_ok=True)
        tmp_path = status_path.parent / f".{_SUPERVISOR_STATUS_FILENAME}.{pid}.tmp"
        tmp_path.write_text(json.dumps(payload), encoding="utf-8")
        tmp_path.replace(status_path)
    except OSError as exc:
        sys.stderr.write(f"claude-supervise: could not write status file: {exc}\n")
        return None
    return status_path


def remove_supervisor_status(untracked_dir: Path) -> None:
    """Remove the supervisor status file if present (idempotent, best-effort).

    Called on exit so a clean shutdown leaves no stale identity behind. Any
    OSError is reported to stderr, never silently swallowed.
    """
    try:
        _supervisor_status_path(untracked_dir).unlink(missing_ok=True)
    except OSError as exc:
        sys.stderr.write(f"claude-supervise: could not remove status file: {exc}\n")


# ---------------------------------------------------------------------------
# Supervisor -> status-line transient message channel (GENERAL, reusable).
#
# The supervisor writes a small TTL-bounded JSON message file that the daemon's
# status-line handler reads and renders, auto-omitting it once expired. The
# Ctrl+Z "ignored" notice is merely the FIRST consumer; future supervisor
# events (compact fired, worker restart, arm/disarm, ...) can post through the
# same channel.
#
# THREAD/PROCESS SAFETY (first-class concern — see the top-of-file note): this
# file is read by the daemon (a SEPARATE process) on every status render and
# may be written by more than one supervisor thread/process. Every writer here
# obeys the same rules as `write_supervisor_status`: write to a PRIVATE temp
# file (named with BOTH pid and thread id so concurrent writers never share a
# temp path), then `os.replace` (atomic on POSIX) swaps it in — a reader always
# sees either the old or the new COMPLETE file, never a partial. Last writer
# wins. In-process rate-limit state is guarded by a `threading.Lock`.
# ---------------------------------------------------------------------------

_STATUS_MESSAGE_FILENAME = "status-message.json"
# How long a posted supervisor message stays live. The status line re-renders on
# Claude Code Status events (~per turn / periodic), NOT on keypress, so the TTL
# must outlast the gap to the next render for the message to be seen, while
# staying short enough that a stale notice clears promptly. A few status renders'
# worth of seconds is the pragmatic middle ground.
_STATUS_MESSAGE_TTL_SECONDS = 10.0
# Minimum monotonic gap between writes from one poster, so a key-mash (a burst of
# Ctrl+Z) rewrites the file at most once per interval instead of thrashing it.
_STATUS_MESSAGE_MIN_INTERVAL_SECONDS = 1.0
# Severity levels a posted message can carry. The status-line reader maps
# "warning" to an orange background (attached to the supervisor's top hat) and
# treats anything else (including a missing level) as plain info. Kept as named
# constants — the string values are the on-disk contract shared with the daemon
# handler (``status_message.py``), so they MUST stay in lockstep.
_STATUS_LEVEL_INFO = "info"
_STATUS_LEVEL_WARNING = "warning"
# Text shown when a Ctrl+Z (SUSP) keystroke/signal is swallowed by the guard.
_CTRL_Z_NOTICE_TEXT = "⛔ Ctrl+Z ignored — use /exit to quit"
# Text shown when a Ctrl+\ (QUIT) SIGNAL is swallowed by the guard. Ctrl+\ almost
# never intentional — a fat-finger next to Enter that would otherwise SIGQUIT
# (and possibly core-dump) the session.
_CTRL_BACKSLASH_NOTICE_TEXT = "⛔ Ctrl+\\ ignored — use /exit to quit"
# The signals install_input_signal_guards manages — SIGINT (Ctrl+C) is
# deliberately EXCLUDED. Kept as one tuple so supervise() can save+restore
# exactly this set around the forwarding loop (must stay in sync with the
# signals install_input_signal_guards touches).
_INPUT_GUARD_SIGNALS = (signal.SIGTSTP, signal.SIGQUIT, signal.SIGTTIN, signal.SIGTTOU)


def _status_message_path(untracked_dir: Path) -> Path:
    """Path to the supervisor->status-line message file (shared 'supervise' dir)."""
    return untracked_dir / _LOG_SUBDIRECTORY / _STATUS_MESSAGE_FILENAME


def write_status_message(
    untracked_dir: Path,
    *,
    text: str,
    expires_at: float,
    level: str = _STATUS_LEVEL_INFO,
) -> Path | None:
    """Atomically write a transient supervisor message for the status line.

    THREAD/PROCESS SAFETY: the message file is read by the daemon (a separate
    process) on every status render and may be written by more than one
    supervisor thread/process. The write goes to a PRIVATE temp file named with
    BOTH pid and thread id (``.{name}.{pid}.{tid}.tmp``) so concurrent writers
    never share a temp path, then ``os.replace`` (atomic on POSIX) swaps it in —
    a reader therefore always sees either the old or the new COMPLETE file, never
    a partial one. Last writer wins.

    ``level`` (``_STATUS_LEVEL_INFO`` / ``_STATUS_LEVEL_WARNING``) rides along in
    the payload so the reader can colour warning-level notices (orange
    background on the supervisor's top hat) distinctly from plain info.

    Best-effort: a write failure is reported to stderr and returns None rather
    than disturbing the supervised session.

    Returns:
        The message file path on success, or None on failure.
    """
    message_path = _status_message_path(untracked_dir)
    payload = {"text": text, "expires_at": expires_at, "level": level}
    try:
        message_path.parent.mkdir(parents=True, exist_ok=True)
        tmp_path = (
            message_path.parent
            / f".{_STATUS_MESSAGE_FILENAME}.{os.getpid()}.{threading.get_ident()}.tmp"
        )
        tmp_path.write_text(json.dumps(payload), encoding="utf-8")
        tmp_path.replace(message_path)
    except OSError as exc:
        sys.stderr.write(f"claude-supervise: could not write status message: {exc}\n")
        return None
    return message_path


class StatusMessagePoster:
    """Thread-safe, rate-limited writer for transient status-line messages.

    A GENERAL supervisor->status-line channel; the Ctrl+Z guard is merely its
    first consumer. The rate-limit state (``_last_monotonic``) is shared mutable
    state that concurrent supervisor threads may touch, so the check-and-update
    is done under a ``threading.Lock`` — two threads posting at the same instant
    can never both slip past the interval. The file write itself is atomic (see
    ``write_status_message``) and is performed OUTSIDE the lock so I/O never
    serialises other posters' rate-limit checks.
    """

    def __init__(
        self,
        untracked_dir: Path,
        *,
        ttl_seconds: float = _STATUS_MESSAGE_TTL_SECONDS,
        min_interval_seconds: float = _STATUS_MESSAGE_MIN_INTERVAL_SECONDS,
        wall_clock: Callable[[], float] = time.time,
        monotonic: Callable[[], float] = time.monotonic,
    ) -> None:
        self._untracked_dir = untracked_dir
        self._ttl_seconds = ttl_seconds
        self._min_interval_seconds = min_interval_seconds
        self._wall_clock = wall_clock
        self._monotonic = monotonic
        self._lock = threading.Lock()
        self._last_monotonic: float | None = None

    def post(self, text: str, *, level: str = _STATUS_LEVEL_INFO) -> Path | None:
        """Write ``text`` as the current status message, honouring the rate limit.

        ``level`` selects the severity (``_STATUS_LEVEL_WARNING`` renders on an
        orange background attached to the supervisor's top hat; the default is
        plain info). Returns the written path, or None when the post is
        suppressed by the rate limit or the write fails. Thread-safe: the
        rate-limit check-and-update runs under the lock so concurrent posters
        cannot both pass within one interval.
        """
        now_mono = self._monotonic()
        with self._lock:
            if (
                self._last_monotonic is not None
                and now_mono - self._last_monotonic < self._min_interval_seconds
            ):
                return None
            self._last_monotonic = now_mono
            expires_at = self._wall_clock() + self._ttl_seconds
        return write_status_message(
            self._untracked_dir, text=text, expires_at=expires_at, level=level
        )


def install_input_signal_guards(post_notice: Callable[[str], object]) -> None:
    """Make the supervisor un-suspendable / un-quittable by accidental keys.

    Belt-and-braces to the byte-level ``strip_suspend``: the byte strip only
    covers Ctrl+Z while the outer terminal is in RAW mode. If a stop/quit SIGNAL
    is actually delivered — a race in the window before ``tty.setraw`` runs, a
    non-tty stdin, ``kill -TSTP``/``-QUIT``, or shell job control — the process
    would still freeze or core-dump. These handlers swallow those signals so the
    session survives, surfacing a transient notice via ``post_notice`` instead:

    - ``SIGTSTP`` (Ctrl+Z / VSUSP): suspend — swallowed, notice posted.
    - ``SIGQUIT`` (Ctrl+\\ / VQUIT): quit + possible core dump — swallowed,
      notice posted. Almost never an intentional keystroke.
    - ``SIGTTIN`` / ``SIGTTOU``: a backgrounded process touching the tty is
      stopped by these — ignored so terminal ops never wedge the supervisor.

    ``SIGINT`` (Ctrl+C / VINTR) is DELIBERATELY LEFT ALONE — it is the
    legitimate, expected interrupt in Claude's TUI; swallowing it would break
    real usage.

    THREAD SAFETY: ``post_notice`` runs inside a signal handler, which CPython
    dispatches on the main thread between bytecodes. Pass a lock-free writer
    (e.g. ``write_status_message`` directly, NOT a ``StatusMessagePoster`` whose
    ``threading.Lock`` the interrupted main thread might already hold) to avoid
    a self-deadlock.

    Must be called from the PARENT after ``pty.fork`` so the child ``claude``
    (already forked with default dispositions) never inherits these handlers.
    """

    def _swallow_stop(_signum: int, _frame: FrameType | None) -> None:
        # Suspend swallowed: post the notice and return WITHOUT stopping.
        post_notice(_CTRL_Z_NOTICE_TEXT)

    def _swallow_quit(_signum: int, _frame: FrameType | None) -> None:
        # Quit swallowed: post the notice and return WITHOUT exiting/core-dumping.
        post_notice(_CTRL_BACKSLASH_NOTICE_TEXT)

    signal.signal(signal.SIGTSTP, _swallow_stop)
    signal.signal(signal.SIGQUIT, _swallow_quit)
    signal.signal(signal.SIGTTIN, signal.SIG_IGN)
    signal.signal(signal.SIGTTOU, signal.SIG_IGN)


def load_freshest_sidecar(
    directory: Path,
    *,
    now: float,
    freshness_seconds: float,
    own_sessions: frozenset[str] | None = None,
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
    scanned = _scan_sidecars(directory, own_sessions=own_sessions)
    if not scanned:
        return None
    data, ts = max(scanned, key=lambda pair: pair[1])
    return _build_sidecar_reading(data, ts, now=now, freshness_seconds=freshness_seconds)


def load_foreground_sidecar(
    directory: Path,
    *,
    now: float,
    freshness_seconds: float,
    margin_seconds: float = _DEFAULT_FOREGROUND_MARGIN_SECONDS,
    own_sessions: frozenset[str] | None = None,
) -> tuple[SidecarReading | None, bool]:
    """Return ``(freshest_reading, foreground_ambiguous)``.

    ``freshest_reading`` is identical to what ``load_freshest_sidecar`` returns.
    ``foreground_ambiguous`` is True when a SECOND, still-fresh sidecar's ``ts``
    is within ``margin_seconds`` of the freshest -- a thread-switch window where
    the just-backgrounded thread cannot be told apart from the new foreground, so
    the caller MUST defer a compaction (it would risk compacting the wrong
    thread). Only NON-stale runner-ups count: under the verified
    only-the-foreground-renders model a backgrounded thread stops rendering and
    ages to stale within one freshness window, dropping out of contention -- so
    ambiguity is transient and self-resolves as the foreground pulls ahead.

    Ambiguity is always False when the freshest reading is stale or absent (there
    is no live foreground to be ambiguous about; the caller NOOPs on staleness
    anyway).
    """
    scanned = _scan_sidecars(directory, own_sessions=own_sessions)
    if not scanned:
        return None, False
    scanned.sort(key=lambda pair: pair[1], reverse=True)
    data, ts = scanned[0]
    reading = _build_sidecar_reading(data, ts, now=now, freshness_seconds=freshness_seconds)

    ambiguous = False
    if not reading.stale and len(scanned) > 1:
        runner_ts = scanned[1][1]
        runner_fresh = (now - runner_ts) <= freshness_seconds
        if runner_fresh and (ts - runner_ts) < margin_seconds:
            ambiguous = True
    return reading, ambiguous


def _scan_sidecars(
    directory: Path, own_sessions: frozenset[str] | None = None
) -> list[tuple[dict[str, object], float]]:
    """Parse every ``*.json`` sidecar in ``directory`` into ``(data, ts)`` pairs.

    Single source of truth for the sidecar scan shared by ``load_freshest_sidecar``
    and ``load_foreground_sidecar``. Unreadable, malformed, or non-object files are
    skipped (older schema or a foreign writer) rather than aborting the scan.

    ``own_sessions`` (Plan 00166): when provided, sidecars whose ``session_id`` is
    NOT in the set are skipped, so a foreign terminal's sidecar in the shared dir
    can never be read as this supervisor's foreground. ``None`` disables the
    filter (legacy / unit-test callers).
    """
    if not directory.is_dir():
        return []
    results: list[tuple[dict[str, object], float]] = []
    for path in directory.glob(_CONTEXT_SIDECAR_GLOB):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            continue
        if not isinstance(data, dict):
            continue
        if not _session_in_scope(data.get("session_id"), own_sessions):
            continue
        results.append((data, _coerce_float(data.get("ts"))))
    return results


def _build_sidecar_reading(
    data: dict[str, object], ts: float, *, now: float, freshness_seconds: float
) -> SidecarReading:
    """Build a ``SidecarReading`` from parsed sidecar ``data`` and its ``ts``."""
    return SidecarReading(
        red=bool(data.get("red", False)),
        critical=bool(data.get("critical", False)),
        compact_urgent=bool(data.get("compact_urgent", False)),
        tier=str(data.get("tier", "")),
        pct=_coerce_float(data.get("pct")),
        session_id=str(data.get("session_id", "")),
        ts=ts,
        seq=_coerce_int(data.get("seq")),
        writer_pid=_coerce_int(data.get("writer_pid")),
        compacting=bool(data.get("compacting", False)),
        stale=(now - ts) > freshness_seconds,
    )


def load_compaction_signal(
    directory: Path,
    *,
    now: float,
    ttl_seconds: float,
    own_sessions: frozenset[str] | None = None,
) -> Path | None:
    """Return the path of a fresh compaction-signal file, or None.

    The daemon's PreCompact handler drops a ``<session>.compacting`` file (JSON
    ``{"ts": ..., "session_id": ...}``) when a compaction starts -- whether the
    supervisor triggered it or the human typed ``/compact``. A signal is "fresh"
    while ``now - ts <= ttl_seconds``; older files are treated as a finished
    compaction and ignored. The path (not a bool) is returned so the caller can
    CONSUME the file (unlink it) once it has acted on it, guaranteeing the
    resume fires exactly once and cannot wedge a later compaction.

    ``own_sessions`` (Plan 00166): when provided, a signal whose ``session_id``
    is NOT in the set is skipped -- this is what stops terminal A's supervisor
    resuming off terminal B's compaction in the shared dir. ``None`` disables
    the filter (legacy / unit-test callers).
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
        if not _session_in_scope(data.get("session_id"), own_sessions):
            continue
        ts = _coerce_float(data.get("ts"))
        if (now - ts) <= ttl_seconds:
            return path
    return None


def reap_stale_sidecars(
    directory: Path,
    *,
    now: float,
    ttl_seconds: float = _DEFAULT_REAP_TTL_SECONDS,
    log: DecisionLog | None = None,
) -> list[Path]:
    """Delete dead context-sidecar / compaction-signal files older than the TTL.

    Reaps both ``*.json`` sidecars and ``*.compacting`` signals whose FILE MTIME
    is older than ``ttl_seconds``. Mtime (not the JSON ``ts``) is used so a
    malformed, truncated, or foreign file is reaped uniformly without a parse --
    a dead file is a dead file. The single newest-mtime ``*.json`` is ALWAYS
    spared, so the supervisor's current reading source is never removed even when
    every session is dead (a bounded residue of one file). Because ``ttl_seconds``
    is far larger than the freshness and compaction-signal windows, a file this
    old can never still be actionable, so reaping cannot race a live decision.

    Atomic-write temp files (``.{stem}.{pid}.tmp``) and any non-sidecar file (the
    supervisor's own ``decision.log``) are never matched by the globs and so are
    left untouched. Unlink races (the file vanished since it was stat'd) and stat
    failures are tolerated -- reaping is best-effort hygiene, never fatal; a real
    unlink error is logged, never silently swallowed.
    """
    if not directory.is_dir():
        return []

    entries: list[tuple[Path, float]] = []
    for path in list(directory.glob(_CONTEXT_SIDECAR_GLOB)) + list(
        directory.glob(_COMPACTION_SIGNAL_GLOB)
    ):
        try:
            mtime = path.stat().st_mtime
        except OSError:
            # Vanished or unreadable between glob and stat -- nothing to reap.
            continue
        entries.append((path, mtime))

    # Spare the freshest sidecar (the supervisor's current reading source) so it
    # is never reaped, even if every session is dead and it too is past the TTL.
    newest_json: Path | None = None
    newest_mtime = float("-inf")
    for path, mtime in entries:
        if path.suffix == ".json" and mtime > newest_mtime:
            newest_mtime = mtime
            newest_json = path

    reaped: list[Path] = []
    for path, mtime in entries:
        if path == newest_json:
            continue
        if (now - mtime) <= ttl_seconds:
            continue
        try:
            path.unlink(missing_ok=True)
        except OSError as exc:
            if log is not None:
                log.write(f"warning: could not reap stale file {path}: {exc}")
            continue
        reaped.append(path)

    if reaped and log is not None:
        log.write(f"reaped {len(reaped)} stale sidecar/signal file(s)")
    return reaped


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

    AWAIT_COMPACTING: wait for a compaction to start (handled above). The machine
    NEVER re-injects a second ``/compact`` here -- a queued one only needs
    flushing. If none starts within ``escape_after_seconds``, decide WOULD_ESCAPE
    so the supervisor injects ``[esc]`` to flush the ``/compact`` that Claude Code
    queued behind the in-flight turn, and it REPEATS that at each
    ``escape_after_seconds`` interval ("fire escape until it does") for ANY band
    -- superseding Plan 00152's critical-only gate, because a stalled queued
    ``/compact`` must be flushable or the monitor loops re-injecting it (the 4x
    dogfooding bug, Plan 00164). ESC is gated on ``idle`` and never fires for a
    human-originated await (the human owns their own flush). After ``max_escapes``
    ESCs with no compaction, OR after the longer ``await_timeout_seconds``, give
    up and return to MONITOR so a missed transition cannot wedge the machine.
    """

    def __init__(self, policy: CompactPolicy) -> None:
        self._policy = policy
        self.state = SupervisorState.MONITOR
        self._injections = 0
        self._last_action_ts: float | None = None
        self._compaction_handled = False
        # ESC-flush bookkeeping for the current AWAIT_COMPACTING episode: how many
        # [esc] we have already injected to flush the queued /compact. Capped at
        # policy.max_escapes, then we give up and return to MONITOR (Plan 00164).
        self._escapes_sent = 0
        # True when the current AWAIT was entered by a HUMAN /compact (not the
        # supervisor's own inject) -- suppresses the supervisor ESC flush.
        self._await_is_human = False

    def export_state(self) -> dict[str, object]:
        """Serialise the mutable per-episode state (Plan 00164 Phase 4 fix).

        The HOST holds the single authoritative machine; it ships this state to
        the policy worker each tick and adopts the worker's returned state, so the
        worker and the in-process fallback can never diverge (which previously let
        a worker stall inject a duplicate ``/compact``). Excludes ``_policy``,
        which is fixed configuration reconstructed on both sides.
        """
        return {
            "state": self.state.value,
            "injections": self._injections,
            "last_action_ts": self._last_action_ts,
            "compaction_handled": self._compaction_handled,
            "escapes_sent": self._escapes_sent,
            "await_is_human": self._await_is_human,
        }

    def import_state(self, state: dict[str, object]) -> None:
        """Overwrite the mutable state from an :meth:`export_state` payload.

        Missing keys keep the current value so an older/newer peer stays safe.
        """
        if "state" in state:
            self.state = SupervisorState(str(state["state"]))
        if "injections" in state:
            self._injections = _coerce_int(state["injections"])
        if "last_action_ts" in state:
            raw = state["last_action_ts"]
            self._last_action_ts = None if raw is None else _coerce_float(raw)
        if "compaction_handled" in state:
            self._compaction_handled = bool(state["compaction_handled"])
        if "escapes_sent" in state:
            self._escapes_sent = _coerce_int(state["escapes_sent"])
        if "await_is_human" in state:
            self._await_is_human = bool(state["await_is_human"])

    def evaluate(
        self,
        reading: SidecarReading | None,
        *,
        idle: bool,
        now: float,
        human_compact_submitted: bool = False,
        work_idle: bool = True,
        foreground_ambiguous: bool = False,
    ) -> Evaluation:
        """Advance the machine one step and return what it WOULD do.

        ``foreground_ambiguous`` (Plan 00160) is True when the freshest sidecar
        cannot be confidently attributed to the FOREGROUND thread this tick (a
        recent Agent-View thread switch left two still-fresh sidecars). It gates
        ONLY the compact path -- a would-be `/compact` is deferred so it never
        targets the wrong thread -- and never affects resume/AWAIT.

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
            if not idle or not work_idle:
                # Never type `continue` into a busy TUI -- it would be lost or
                # corrupt in-flight input. TWO ways the TUI is busy:
                #   * not idle      -- a human keystroke / non-empty input box.
                #   * not work_idle -- the child is still STREAMING (Plan 00152).
                # The second is the critical one for AUTOMATED compaction: the
                # `.compacting` signal is written at compaction START and the
                # human types nothing, so `idle` stays True the whole time. Only
                # `work_idle` tells us the compaction summary has stopped
                # streaming and we are back at the real post-compaction prompt.
                # Gating on it stops the resume from firing mid-compaction and
                # being dropped (the "compact injected, continue lost" bug). Do
                # NOT latch: retry each poll so the resume still fires once the
                # session settles.
                return Evaluation(Decision.NOOP, _REASON_BUSY_AWAIT_RESUME)
            self._compaction_handled = True
            self._last_action_ts = now
            # Plan 00180: a compaction actually happened, so refresh the injection
            # budget. `max_injections` is a runaway-loop fuse on CONSECUTIVE FAILED
            # injections (compact injected but nothing compacts), NOT a lifetime
            # cap -- without this reset a long-lived session hits 20 legitimate
            # compactions and is then permanently muzzled even at CRITICAL context.
            # Reset ONLY here (the confirmed-compaction success path), never in
            # `_enter_monitor` (the AWAIT-timeout give-up path also calls it, and a
            # wedged session's failed injections MUST keep accumulating).
            self._injections = 0
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
            return self._evaluate_await(idle=idle, now=now)
        return self._evaluate_monitor(
            reading,
            idle=idle,
            work_idle=work_idle,
            now=now,
            foreground_ambiguous=foreground_ambiguous,
        )

    def _evaluate_monitor(
        self,
        reading: SidecarReading | None,
        *,
        idle: bool,
        work_idle: bool,
        now: float,
        foreground_ambiguous: bool = False,
    ) -> Evaluation:
        if reading is None:
            return Evaluation(Decision.NOOP, "no sidecar reading")
        if reading.stale:
            return Evaluation(Decision.NOOP, "sidecar stale")
        if not reading.red:
            return Evaluation(Decision.NOOP, f"not red (tier={reading.tier})")
        # Plan 00160: defer a would-be compaction while the foreground thread is
        # ambiguous (recent thread switch) -- injecting now could compact the
        # wrong Agent-View thread. Self-resolves as the backgrounded sidecar ages.
        if foreground_ambiguous:
            return Evaluation(Decision.NOOP, _REASON_FOREGROUND_AMBIGUOUS)
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
        # Enter AWAIT: from here the queued /compact is flushed with [esc] if it
        # stalls -- for ANY band, not just critical (Plan 00164 supersedes the
        # Plan 00152 critical-only gate: a stalled queued /compact MUST be
        # flushable or the machine loops re-injecting it).
        self._enter_await(is_human=False)
        urgency = "CRITICAL" if reading.critical else ("urgent" if urgent else "red")
        return Evaluation(
            Decision.WOULD_COMPACT,
            f"{urgency} at {reading.pct:.0f}% + idle -> would inject /compact",
        )

    def _evaluate_await(self, *, idle: bool, now: float) -> Evaluation:
        if self._await_timed_out(now):
            self._enter_monitor()
            return Evaluation(Decision.NOOP, "await-compacting timed out -> back to monitor")
        # A human-originated AWAIT never gets a supervisor ESC -- the human owns
        # flushing their own queued /compact.
        if self._await_is_human:
            return Evaluation(Decision.NOOP, "awaiting human compaction start")
        # ESC-flush (Plan 00164): a supervisor /compact that Claude Code queued
        # behind the in-flight turn needs an [esc] to run, so we NEVER re-inject a
        # second /compact -- we fire [esc] REPEATEDLY at escape_after intervals
        # ("fire escape until it does") for ANY band, re-arming the interval each
        # time, until compaction starts or max_escapes is reached. After that we
        # give up and return to MONITOR so a missed transition cannot wedge us.
        if idle and self._escape_due(now):
            if self._escapes_sent >= self._policy.max_escapes:
                self._enter_monitor()
                return Evaluation(
                    Decision.NOOP,
                    "max escapes reached, compaction never started -> back to monitor",
                )
            self._escapes_sent += 1
            self._last_action_ts = now
            return Evaluation(
                Decision.WOULD_ESCAPE,
                f"queued /compact stalled -> would inject [esc] to flush "
                f"({self._escapes_sent}/{self._policy.max_escapes})",
            )
        return Evaluation(Decision.NOOP, "awaiting compaction start")

    def _enter_await(self, *, is_human: bool) -> None:
        """Enter AWAIT_COMPACTING, resetting the per-episode ESC-flush counter."""
        self.state = SupervisorState.AWAIT_COMPACTING
        self._escapes_sent = 0
        self._await_is_human = is_human

    def _enter_monitor(self) -> None:
        """Return to MONITOR, clearing per-episode AWAIT bookkeeping."""
        self.state = SupervisorState.MONITOR
        self._escapes_sent = 0
        self._await_is_human = False

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
# something the human typed. The full prefix appends the local wall-clock time
# the message was injected (see `_format_bot_prefix`) so a human scrolling back
# through a long session can see WHEN each supervisor action happened -- the
# `]` closes the bracket AFTER the timestamp: `🤖 [ccy-supervisor 2026-07-16 14:30:05]`.
_BOT_PREFIX = "🤖 [ccy-supervisor"
# Local time (not UTC): the marker is read by a human scrolling their own
# terminal history, for whom local time is the natural "when" reference.
_BOT_PREFIX_TIME_FORMAT = "%Y-%m-%d %H:%M:%S"
# Message BODIES (without the timestamped prefix, which is prepended at inject
# time by `_resolve_payload`). Kept as the single source of truth for the text.
_DRY_RUN_COMPACT_BODY = "compact suggestion fired (dry-run — not a real /compact, not human input)"
# The armed compact is a real `/compact`, but `/compact` accepts freeform custom
# instructions as its argument -- so the bot chrome rides along AS those
# instructions. The slash command is still the first token (recognised
# normally) and the timestamped `🤖 [ccy-supervisor ...]` prefix marks it for a
# human reading scrollback. The instruction itself is purely ACTIONABLE -- just
# "resume and continue" -- with no provenance framing: the agent should act on
# what it is told regardless of who initiated it (reinforced by the `continue`
# keystroke the supervisor injects once compaction ends).
_ARMED_COMPACT_BODY = (
    "After compacting, immediately resume and continue the work that was in progress."
)
# `continue` is harmless -- it only nudges the agent to resume -- so it is
# injected FOR REAL in both dry-run and armed modes. Detecting a compaction and
# not resuming would defeat the purpose, and (unlike /compact) a stray
# `continue` cannot destroy context. It keeps the bot prefix so a post-compact
# resume is clearly the supervisor's doing, not a human message.
_CONTINUE_BODY = "continue"
# The real ESC byte injected (armed) to interrupt the current turn so a QUEUED
# `/compact` runs. It is an interrupt KEY, not a line: injected raw with NO
# trailing Enter. ESC is harmless (it only interrupts), but it DOES affect the
# session, so in dry-run a visible marker is shown instead of a real ESC.
_ESC_PAYLOAD = "\x1b"
_DRY_RUN_ESCAPE_BODY = "would send [esc] to flush a queued /compact (dry-run — no real ESC sent)"


def _format_bot_prefix(now_wall: float | None = None) -> str:
    """Return the bot prefix stamped with the tick's local wall-clock time.

    ``now_wall`` is the tick's epoch-seconds wall clock (``time.time()``), so the
    stamp is deterministic and matches the decision that triggered the injection.
    When omitted the current local time is used. Local time (not UTC) is used
    deliberately: the marker is read by a human scrolling their own terminal
    history, for whom local time is the natural "when did this happen" reference.
    """
    moment = datetime.now() if now_wall is None else datetime.fromtimestamp(now_wall)
    return f"{_BOT_PREFIX} {moment.strftime(_BOT_PREFIX_TIME_FORMAT)}]"


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


def _resolve_payload(
    decision: Decision, *, dry_run: bool, now_wall: float | None = None
) -> str | None:
    """Return the keystroke payload for a decision, or None for NOOP.

    In dry-run mode the payload is a harmless, visible MARKER string so the
    injection path can be exercised end-to-end without triggering a real
    compaction. Armed mode injects the real slash-command / prompt.

    Every visible payload embeds the tick's local wall-clock time (via
    ``_format_bot_prefix(now_wall)``) so the human can see WHEN the supervisor
    acted when scrolling back. The raw ESC (armed) is exempt -- it is an
    interrupt key, not a human-readable line.
    """
    prefix = _format_bot_prefix(now_wall)
    if decision is Decision.WOULD_COMPACT:
        if dry_run:
            return f"{prefix} {_DRY_RUN_COMPACT_BODY}"
        # `/compact` MUST stay the FIRST token so it is recognised as the slash
        # command; the timestamped bot chrome rides along as its instruction arg.
        return f"/compact {prefix} {_ARMED_COMPACT_BODY}"
    if decision is Decision.WOULD_CONTINUE:
        return f"{prefix} {_CONTINUE_BODY}"
    if decision is Decision.WOULD_ESCAPE:
        return f"{prefix} {_DRY_RUN_ESCAPE_BODY}" if dry_run else _ESC_PAYLOAD
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


def _is_benign_not_red(reading: SidecarReading | None) -> bool:
    """True when the supervisor POSITIVELY sees a not-red, non-stale context.

    This is the overwhelmingly common idle tick -- the context is genuinely
    fine and there is nothing to do. Its NOOP carries no diagnostic value, so
    Plan 00168 Phase 1 stays SILENT on it (keeping a green idle session's
    decision.log empty). Every OTHER NOOP gate IS logged -- crucially the
    ``reading is None`` (no sidecar / filtered out) and ``reading.stale`` cases,
    which are the blind-spots (H3 / H1) a red-but-not-compacting report needs.
    """
    return reading is not None and not reading.stale and not reading.red


def _noop_band_suffix(reading: SidecarReading | None) -> str:
    """A coarse ' [band]' suffix for a NOOP-reason line (Plan 00168 Phase 1).

    Annotates the observed context severity when it is known and not already
    stated by the reason itself. Deliberately low-cardinality (band, never a
    per-tick pct) so ``DecisionLog.write_noop`` dedup keeps a steady gate to one
    line. Empty when there is no reading, the reading is stale, or the context
    is not red (those cases the reason string already describes fully).
    """
    if reading is None or reading.stale or not reading.red:
        return ""
    if reading.critical:
        return " [critical]"
    if reading.compact_urgent:
        return " [urgent]"
    return " [red]"


def decide_once(
    machine: CompactStateMachine,
    *,
    sidecar_dir: Path,
    facts: TickFacts,
    dry_run: bool,
    freshness_seconds: float,
    log: DecisionLog | None = None,
    compaction_signal_ttl_seconds: float = _DEFAULT_COMPACTION_SIGNAL_TTL_SECONDS,
    reap_ttl_seconds: float = _DEFAULT_REAP_TTL_SECONDS,
    foreground_margin_seconds: float = _DEFAULT_FOREGROUND_MARGIN_SECONDS,
    own_sessions: frozenset[str] | None = None,
) -> TickOutcome:
    """Decide what to inject this tick WITHOUT touching the PTY (Plan 00164 P4).

    This is the whole 'brain': reap dead files, read the foreground sidecar and
    compaction signal, advance the state machine, and resolve the payload. It
    performs NO injection — the host (or the in-process fallback) applies the
    returned :class:`TickOutcome`. Because it is pure w.r.t. the PTY, it runs
    IDENTICALLY in the policy-worker subprocess and in-process, so a worker
    restart cannot change behaviour. ``log`` is used only for reap diagnostics.

    ``own_sessions`` (Plan 00166): the set of session ids belonging to THIS
    supervisor's own Claude instance. Sidecars and compaction signals from any
    other session in the shared dir are ignored, so a compaction in one terminal
    never drives an injection into another. ``None`` disables the filter (the
    legacy behaviour, used by unit tests); the production callers resolve the
    set via :func:`cached_own_session_ids` and pass it. The empty set fails safe
    (act on nothing).

    See ``_poll_once`` for the semantics of the individual :class:`TickFacts`
    (empty-input-box guard, human-compact edge, work-idle band gating, reaping).
    """
    reap_stale_sidecars(sidecar_dir, now=facts.now_wall, ttl_seconds=reap_ttl_seconds, log=log)
    # Plan 00160: resolve the FOREGROUND sidecar and whether it is ambiguous (a
    # recent Agent-View thread switch left two still-fresh sidecars). Ambiguity
    # gates only the compact path in the machine below. Plan 00166: scoped to
    # this instance's own sessions so a foreign terminal's sidecar is invisible.
    reading, foreground_ambiguous = load_foreground_sidecar(
        sidecar_dir,
        now=facts.now_wall,
        freshness_seconds=freshness_seconds,
        margin_seconds=foreground_margin_seconds,
        own_sessions=own_sessions,
    )
    # A compaction stops status renders, so the context sidecar goes
    # stale/absent during one -- the compaction signal is an independent input.
    # Plan 00166: only this instance's own compaction signal counts.
    signal_path = load_compaction_signal(
        sidecar_dir,
        now=facts.now_wall,
        ttl_seconds=compaction_signal_ttl_seconds,
        own_sessions=own_sessions,
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
                ts=facts.now_wall,
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
    can_inject = facts.idle and facts.input_line_empty
    # Plan 00164 Phase 4 fix: adopt the host's authoritative machine state before
    # deciding so the worker never runs on divergent state. None on the in-process
    # path (the passed machine is already the live authoritative one).
    if facts.machine_state is not None:
        machine.import_state(facts.machine_state)
    evaluation = machine.evaluate(
        reading,
        idle=can_inject,
        now=facts.now_wall,
        human_compact_submitted=facts.human_compact_submitted,
        work_idle=facts.work_idle,
        foreground_ambiguous=foreground_ambiguous,
    )
    payload = _resolve_payload(evaluation.decision, dry_run=dry_run, now_wall=facts.now_wall)
    # The raw ESC is an interrupt key, not a line -- inject it WITHOUT a trailing
    # Enter. Every other payload (compact / continue / markers) is a line.
    submit = not (evaluation.decision is Decision.WOULD_ESCAPE and not dry_run)
    # Consume the signal ONLY after a resume actually fired (the host does the
    # consuming, so a failed PTY write never loses the resume).
    consume_signal_path = (
        str(signal_path)
        if evaluation.decision is Decision.WOULD_CONTINUE and signal_path is not None
        else None
    )
    deferred_log = None
    if (
        payload is None
        and facts.idle
        and not facts.input_line_empty
        and evaluation.reason in _INJECTION_GATED_REASONS
    ):
        deferred_log = f"{_DEFERRED_LOG_PREFIX} ({evaluation.reason})"
    # Plan 00168 Phase 1: for every OTHER NOOP tick (not the input-box deferral
    # above, which already logs), emit a deduped NOOP-reason diagnostic naming
    # the gate + observed band. This makes a red-but-not-compacting session
    # self-explaining in decision.log. Decision-preserving: pure logging.
    noop_reason_log = None
    if (
        payload is None
        and deferred_log is None
        and evaluation.decision is Decision.NOOP
        and not _is_benign_not_red(reading)
    ):
        noop_reason_log = f"{_NOOP_LOG_PREFIX}: {evaluation.reason}{_noop_band_suffix(reading)}"
    return TickOutcome(
        decision_value=evaluation.decision.value,
        reason=evaluation.reason,
        payload=payload,
        submit=submit,
        consume_signal_path=consume_signal_path,
        deferred_log=deferred_log,
        machine_state=machine.export_state(),
        noop_reason_log=noop_reason_log,
    )


def _apply_decision(
    outcome: TickOutcome,
    *,
    master_writer: Callable[[bytes], None],
    log: DecisionLog | None,
) -> None:
    """Perform a :class:`TickOutcome` on the PTY (host side, Plan 00164 P4).

    Injects the payload (if any), logs it, and consumes the compaction signal
    only AFTER a successful resume injection — mirroring the original inline
    behaviour of ``_poll_once`` exactly.
    """
    if outcome.payload is not None:
        _perform_injection(master_writer, outcome.payload, submit=outcome.submit)
        if log is not None:
            log.write(f"{outcome.decision_value}: {outcome.reason}; injected {outcome.payload!r}")
        if outcome.consume_signal_path is not None:
            _consume_signal(Path(outcome.consume_signal_path), log)
    elif log is not None and outcome.deferred_log is not None:
        # An injection was pending and the NON-EMPTY INPUT BOX was the sole gate.
        log.write(outcome.deferred_log)
    elif log is not None and outcome.noop_reason_log is not None:
        # Plan 00168 Phase 1: record WHY this idle tick did nothing (deduped, so
        # an unchanged gate never floods). Makes red-but-not-compacting visible.
        log.write_noop(outcome.noop_reason_log)


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
    reap_ttl_seconds: float = _DEFAULT_REAP_TTL_SECONDS,
    foreground_margin_seconds: float = _DEFAULT_FOREGROUND_MARGIN_SECONDS,
    own_sessions: frozenset[str] | None = None,
) -> Evaluation:
    """One in-process supervisor tick: decide (``decide_once``) then inject.

    Retained as the in-process fast path AND the fallback used when the policy
    worker cannot run (Plan 00164 Phase 4). Behaviour is unchanged: it delegates
    the decision to ``decide_once`` and the injection to ``_apply_decision``.
    ``own_sessions`` (Plan 00166) is passed straight through to ``decide_once``
    (``None`` = no own-session filter; the production loop resolves and passes it).
    """
    facts = TickFacts(
        now_wall=now_wall,
        idle=idle,
        input_line_empty=input_line_empty,
        human_compact_submitted=human_compact_submitted,
        work_idle=work_idle,
    )
    outcome = decide_once(
        machine,
        sidecar_dir=sidecar_dir,
        facts=facts,
        dry_run=dry_run,
        log=log,
        freshness_seconds=freshness_seconds,
        compaction_signal_ttl_seconds=compaction_signal_ttl_seconds,
        reap_ttl_seconds=reap_ttl_seconds,
        foreground_margin_seconds=foreground_margin_seconds,
        own_sessions=own_sessions,
    )
    _apply_decision(outcome, master_writer=master_writer, log=log)
    return Evaluation(decision=Decision(outcome.decision_value), reason=outcome.reason)


# ---------------------------------------------------------------------------
# Policy-worker split (Plan 00164 Phase 4)
#
# The decision logic (decide_once + the state machine) runs in a restartable
# `--worker` subprocess so it can be hot-reloaded from a freshly-deployed
# claude-supervise.py WITHOUT restarting the PTY host that owns `claude`. Host
# and worker exchange line-delimited JSON: host -> worker TickFacts, worker ->
# host TickOutcome. Anything that goes wrong with the worker falls back to an
# identical in-process decision, so the supervised session is never at risk.
# ---------------------------------------------------------------------------


def _facts_to_json(facts: TickFacts) -> str:
    return json.dumps(
        {
            "now_wall": facts.now_wall,
            "idle": facts.idle,
            "input_line_empty": facts.input_line_empty,
            "human_compact_submitted": facts.human_compact_submitted,
            "work_idle": facts.work_idle,
            "machine_state": facts.machine_state,
        }
    )


def _facts_from_json(line: str) -> TickFacts:
    data = json.loads(line)
    return TickFacts(
        now_wall=float(data["now_wall"]),
        idle=bool(data["idle"]),
        input_line_empty=bool(data["input_line_empty"]),
        human_compact_submitted=bool(data["human_compact_submitted"]),
        work_idle=bool(data["work_idle"]),
        machine_state=data.get("machine_state"),
    )


def _outcome_to_json(outcome: TickOutcome) -> str:
    return json.dumps(
        {
            "decision_value": outcome.decision_value,
            "reason": outcome.reason,
            "payload": outcome.payload,
            "submit": outcome.submit,
            "consume_signal_path": outcome.consume_signal_path,
            "deferred_log": outcome.deferred_log,
            "machine_state": outcome.machine_state,
            "noop_reason_log": outcome.noop_reason_log,
        }
    )


def _outcome_from_json(line: str) -> TickOutcome:
    data = json.loads(line)
    return TickOutcome(
        decision_value=str(data["decision_value"]),
        reason=str(data["reason"]),
        payload=data["payload"],
        submit=bool(data["submit"]),
        consume_signal_path=data["consume_signal_path"],
        deferred_log=data["deferred_log"],
        machine_state=data.get("machine_state"),
        noop_reason_log=data.get("noop_reason_log"),
    )


def run_worker(
    in_stream: TextIO,
    out_stream: TextIO,
    *,
    dry_run: bool,
    sidecar_dir: Path,
    policy: CompactPolicy,
) -> int:
    """Policy-worker loop: read TickFacts lines, emit TickOutcome lines.

    Owns the ``CompactStateMachine`` so a host-side restart of this subprocess
    reloads the decision code (this whole 'brain') without disturbing the PTY
    host. Blocks on ``in_stream``; a closed pipe (host gone / EOF) ends the loop
    and returns 0. A malformed line is skipped (logged to stderr), never fatal.
    """
    machine = CompactStateMachine(policy)
    for raw in in_stream:
        line = raw.strip()
        if not line:
            continue
        try:
            facts = _facts_from_json(line)
        except (ValueError, KeyError) as exc:
            append_worker_error(f"bad tick line: {exc}")
            continue
        try:
            outcome = decide_once(
                machine,
                sidecar_dir=sidecar_dir,
                facts=facts,
                dry_run=dry_run,
                freshness_seconds=policy.freshness_seconds,
                compaction_signal_ttl_seconds=policy.compaction_signal_ttl_seconds,
                reap_ttl_seconds=policy.reap_ttl_seconds,
                foreground_margin_seconds=policy.foreground_margin_seconds,
                own_sessions=cached_own_session_ids(),  # Plan 00166: only our own sessions
            )
        except Exception:
            # SAFETY NET: a single tick's exception must not kill the worker
            # (the host would respawn it and the crash would repeat every tick,
            # flooding the PTY with tracebacks). Log the full traceback to the
            # error FILE, emit a safe NOOP so the host still gets a reply, and
            # carry on with the next tick. This is a deliberate broad catch --
            # the whole purpose is to contain ANY unexpected decision failure.
            append_worker_error("decide_once failed:\n" + traceback.format_exc())
            outcome = _worker_error_noop()
        out_stream.write(_outcome_to_json(outcome) + "\n")
        out_stream.flush()
    return 0


def _worker_error_noop() -> TickOutcome:
    """A do-nothing TickOutcome emitted when a worker tick raised.

    ``machine_state=None`` leaves the host's authoritative state untouched, so a
    transient tick error never advances or corrupts the compaction state.
    """
    return TickOutcome(
        decision_value=Decision.NOOP.value,
        reason="worker tick error (see worker error log)",
        payload=None,
        submit=True,
        consume_signal_path=None,
        deferred_log=None,
        machine_state=None,
    )


class PolicyWorker:
    """Host-side client for the restartable policy-worker subprocess (Phase 4).

    ``decide(facts)`` returns the worker's TickOutcome, or ``None`` on ANY
    problem (worker not started, dead, slow, or a malformed reply) so the host
    can fall back to an in-process decision. ``reload_if_stale()`` respawns the
    worker from the current on-disk code when the supervisor file changes.
    """

    def __init__(
        self,
        self_path: Path,
        *,
        dry_run: bool,
        read_timeout: float = _WORKER_READ_TIMEOUT_SECONDS,
    ) -> None:
        self._self_path = self_path
        self._dry_run = dry_run
        self._read_timeout = read_timeout
        self._proc: subprocess.Popen[str] | None = None
        self._err_stream: TextIO | None = None
        self._source_fingerprint = self._current_fingerprint()

    def _current_fingerprint(self) -> str | None:
        """Best-effort source hash of the on-disk supervisor (None on error)."""
        try:
            return compute_source_hash(self._self_path)
        except OSError:
            return None

    def start(self) -> bool:
        """Spawn the worker subprocess. Returns True on success."""
        argv = [sys.executable, str(self._self_path), _WORKER_FLAG]
        if not self._dry_run:
            argv.append("--arm")
        # The worker's stderr MUST go to a file (or /dev/null), NEVER the PTY the
        # host inherited: an uncaught per-tick traceback would otherwise flood
        # the live Claude session. Open the error log; fall back to devnull.
        self._err_stream = open_worker_error_log()
        err_target: TextIO | int = (
            self._err_stream if self._err_stream is not None else subprocess.DEVNULL
        )
        try:
            # SECURITY: fixed argv (python + this script + flags), never a shell.
            self._proc = subprocess.Popen(  # nosec B603 - trusted fixed argv, no shell
                argv,
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=err_target,
                text=True,
                bufsize=1,
            )
        except OSError as exc:
            append_worker_error(f"could not start policy worker: {exc}")
            self._close_err_stream()
            self._proc = None
            return False
        self._source_fingerprint = self._current_fingerprint()
        return self._proc.stdin is not None and self._proc.stdout is not None

    def alive(self) -> bool:
        return self._proc is not None and self._proc.poll() is None

    def decide(self, facts: TickFacts) -> TickOutcome | None:
        """Ask the worker for a decision; None on any failure (host falls back)."""
        proc = self._proc
        if proc is None or proc.stdin is None or proc.stdout is None or proc.poll() is not None:
            return None
        try:
            proc.stdin.write(_facts_to_json(facts) + "\n")
            proc.stdin.flush()
        except (OSError, ValueError):
            return None
        # Bounded wait: a hung worker must not stall the PTY host for a whole tick.
        ready, _, _ = select.select([proc.stdout], [], [], self._read_timeout)
        if not ready:
            return None
        try:
            line = proc.stdout.readline()
        except (OSError, ValueError):
            return None
        if not line:
            return None
        try:
            return _outcome_from_json(line)
        except (ValueError, KeyError):
            return None

    def reload_if_stale(self) -> bool:
        """Respawn the worker if the on-disk supervisor code has changed.

        Returns True when a reload happened. The PTY host is untouched — only the
        decision subprocess is swapped for one running the new code.
        """
        current = self._current_fingerprint()
        if current is not None and current != self._source_fingerprint:
            return self.restart()
        return False

    def restart(self) -> bool:
        self.close()
        return self.start()

    def _close_err_stream(self) -> None:
        """Close the worker's error-log stream handle if the host opened one."""
        stream = self._err_stream
        self._err_stream = None
        if stream is None:
            return
        try:
            stream.close()
        except OSError as exc:
            append_worker_error(f"worker error-log close failed: {exc}")

    def close(self) -> None:
        proc = self._proc
        self._proc = None
        if proc is None:
            self._close_err_stream()
            return
        for stream in (proc.stdin, proc.stdout):
            if stream is None:
                continue
            try:
                stream.close()
            except OSError as exc:
                append_worker_error(f"worker stream close failed: {exc}")
        try:
            proc.terminate()
            proc.wait(timeout=1.0)
        except subprocess.TimeoutExpired:
            proc.kill()
        except OSError as exc:
            append_worker_error(f"worker terminate failed: {exc}")
        self._close_err_stream()


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
    on_suspend: Callable[[], object] | None = None,
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
                # Drop Ctrl+Z (SUSP) before it reaches the child so it can never
                # suspend the session. A chunk that was ONLY suspend bytes
                # forwards nothing, but must NOT be mistaken for EOF (that is the
                # empty-read branch below) — so this stays inside `if data:`.
                forwarded = strip_suspend(data)
                if forwarded != data and on_suspend is not None:
                    # At least one suspend byte was swallowed — surface a
                    # transient status-line notice so the user learns why their
                    # Ctrl+Z did nothing. Best-effort: never let it break I/O.
                    on_suspend()
                if forwarded:
                    activity.record(forwarded)
                    os.write(master_fd, forwarded)
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
    decider: Callable[[TickFacts], TickOutcome | None] | None = None,
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
    # Transient supervisor->status-line message channel (GENERAL; the Ctrl+Z
    # input guard is its first consumer). Co-located with the sidecar dir's
    # PARENT — the daemon untracked dir — so the daemon's status handler reads
    # the very same file. Thread-safe + rate-limited (see StatusMessagePoster).
    status_message_poster = StatusMessagePoster(sidecar_dir.parent)

    if log is not None:
        log.write(
            f"supervisor active ({mode}); polling {sidecar_dir} every "
            f"{poll_seconds}s; wrapping: {argv}"
        )

    # Startup banner + spinner (Plan 00164 Phase 2): give the launching ccy
    # session immediate, informative feedback during the perceptible start-up
    # lull. Both go to stderr and are gated on an interactive TTY, so piped/
    # non-interactive launches and the test suite stay silent. The spinner is
    # stopped (line cleared) right after the fork, BEFORE the child paints, so
    # it never coexists with the wrapped process's output.
    spinner: _StartupSpinner | None = None
    if _should_show_banner(sys.stderr):
        # Blank lines above and below set the banner apart from whatever the
        # terminal was showing, so the startup notice reads as a distinct block.
        banner = render_startup_banner(version=__version__, armed=not dry_run)
        sys.stderr.write("\n\n" + banner + "\n\n")
        sys.stderr.flush()
        spinner = _StartupSpinner(sys.stderr)
        spinner.start()

    pid, master_fd = pty.fork()
    if pid == 0:  # pragma: no cover - runs in the forked child process
        # SECURITY: no shell involved -- argv is passed directly to execvp as
        # a list (never a shell string), so there is no command-injection
        # surface here. This IS the supervisor's job: exec the wrapped
        # process (e.g. `claude`) on the child side of the PTY.
        os.execvp(argv[0], argv)  # nosec B606
        os._exit(127)  # unreachable on success

    if spinner is not None:
        spinner.stop()

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
        # Plan 00164 Phase 4: prefer the restartable policy worker; on ANY worker
        # failure (decider returns None) fall back to the identical in-process
        # path so a tick is never dropped. The host always performs the injection.
        now_wall = time.time()
        outcome = None
        if decider is not None:
            outcome = decider(
                TickFacts(
                    now_wall=now_wall,
                    idle=idle,
                    input_line_empty=activity.line.is_empty,
                    human_compact_submitted=human_compact,
                    work_idle=work_idle,
                    # Ship the host's authoritative machine state so the worker
                    # decides on it -- never on divergent worker-local state.
                    machine_state=machine.export_state(),
                )
            )
        if outcome is not None:
            _apply_decision(outcome, master_writer=_write_master, log=log)
            # Adopt the worker's post-tick state so `machine` remains the single
            # source of truth; a later in-process fallback tick then cannot
            # diverge and inject a duplicate /compact (Plan 00164 Phase 4 fix).
            if outcome.machine_state is not None:
                machine.import_state(outcome.machine_state)
        else:
            _poll_once(
                machine,
                sidecar_dir=sidecar_dir,
                now_wall=now_wall,
                idle=idle,
                dry_run=dry_run,
                master_writer=_write_master,
                log=log,
                freshness_seconds=policy.freshness_seconds,
                compaction_signal_ttl_seconds=policy.compaction_signal_ttl_seconds,
                input_line_empty=activity.line.is_empty,
                human_compact_submitted=human_compact,
                work_idle=work_idle,
                reap_ttl_seconds=policy.reap_ttl_seconds,
                foreground_margin_seconds=policy.foreground_margin_seconds,
                own_sessions=cached_own_session_ids(),  # Plan 00166: only our own sessions
            )

    previous_handler = signal.signal(signal.SIGWINCH, _on_winch)

    # Belt-and-braces to the byte-level Ctrl+Z strip: swallow stop/quit SIGNALS
    # if they ever reach the supervisor (race before setraw, non-tty stdin, `kill
    # -TSTP`, job control) so the session can never freeze or core-dump. Uses a
    # LOCK-FREE writer (not the poster) because it runs inside a signal handler
    # (see install_input_signal_guards). Installed here in the PARENT, after the
    # fork, so the child never inherits these dispositions.
    def _post_signal_notice(notice: str) -> None:
        write_status_message(
            sidecar_dir.parent,
            text=notice,
            expires_at=time.time() + _STATUS_MESSAGE_TTL_SECONDS,
            level=_STATUS_LEVEL_WARNING,
        )

    # Save prior dispositions so they are restored on exit (supervise() is called
    # in-process by tests; leaked stop/quit handlers would break their job
    # control). getsignal returns the typeshed _HANDLER union — inferred locally.
    prev_signal_guards = {sig: signal.getsignal(sig) for sig in _INPUT_GUARD_SIGNALS}
    install_input_signal_guards(_post_signal_notice)

    try:
        _forward_io(
            stdin_fd,
            master_fd,
            activity,
            poll_seconds=poll_seconds,
            on_poll=_on_poll,
            output_activity=output_activity,
            on_suspend=lambda: status_message_poster.post(
                _CTRL_Z_NOTICE_TEXT, level=_STATUS_LEVEL_WARNING
            ),
        )
    finally:
        for guarded_signal, prior_handler in prev_signal_guards.items():
            signal.signal(guarded_signal, prior_handler)
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

    # Policy-worker mode (Plan 00164 Phase 4): no child argv — read TickFacts
    # from stdin, write TickOutcomes to stdout. The host spawns this.
    if _WORKER_FLAG in argv:
        # Defence in depth: guarantee the worker's stderr is a FILE, never a
        # terminal, regardless of how it was launched (the host already sets
        # this via Popen, but a direct/manual `--worker` run must not flood a
        # tty either). See Plan 00166.
        _redirect_worker_stderr_to_log()
        return run_worker(
            sys.stdin,
            sys.stdout,
            dry_run="--arm" not in argv,
            sidecar_dir=_default_sidecar_dir(),
            policy=CompactPolicy(),
        )

    child_argv = _split_child_argv(argv)
    if child_argv is None:
        sys.stderr.write(_USAGE)
        return 2

    flags = _parse_supervisor_flags(argv)
    log = _resolve_decision_log(flags.log_path)

    # Advertise this running supervisor's identity (version + source hash) so a
    # SessionStart advisory can detect when an upgrade has left a NEWER
    # supervisor on disk than the one still running (Plan 00164 Phase 3). The
    # status is removed on exit so a clean shutdown leaves nothing stale behind.
    untracked_dir = _daemon_untracked_dir()
    write_supervisor_status(
        untracked_dir,
        version=__version__,
        source_hash=compute_source_hash(_SELF_PATH),
        pid=os.getpid(),
        started_at=time.time(),
    )

    # Run the decision logic in a restartable worker subprocess (Plan 00164
    # Phase 4) so it can hot-reload from a freshly-deployed supervisor without
    # disturbing this PTY host. Worker failure is invisible — the host falls back
    # to an identical in-process decision — so the session is never at risk.
    worker = _make_policy_worker(flags.dry_run)
    decider = _make_worker_decider(worker) if worker is not None else None
    try:
        return supervise(child_argv, dry_run=flags.dry_run, log=log, decider=decider)
    finally:
        if worker is not None:
            worker.close()
        remove_supervisor_status(untracked_dir)


def _make_policy_worker(dry_run: bool) -> PolicyWorker | None:
    """Create + start the policy worker, or None (in-process) when opted out /
    unstartable. Never raises — a worker problem must not break a launch."""
    if os.environ.get(_NO_WORKER_ENV):
        return None
    worker = PolicyWorker(_SELF_PATH, dry_run=dry_run)
    if not worker.start():
        return None
    return worker


def _make_worker_decider(worker: PolicyWorker) -> Callable[[TickFacts], TickOutcome | None]:
    """Return a per-tick decider that hot-reloads the worker on code change and
    asks it to decide, returning None on any failure so the host falls back."""
    last_reload_check = [0.0]

    def _decide(facts: TickFacts) -> TickOutcome | None:
        if facts.now_wall - last_reload_check[0] >= _WORKER_RELOAD_CHECK_SECONDS:
            last_reload_check[0] = facts.now_wall
            worker.reload_if_stale()
        if not worker.alive():
            worker.restart()
        return worker.decide(facts)

    return _decide


if __name__ == "__main__":
    raise SystemExit(main())
