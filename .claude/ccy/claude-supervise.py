#!/usr/bin/env python3
#
# DAEMON-OWNED FILE - do not edit. Deployed into your project by the
# claude-code-hooks-daemon installer and refreshed on every upgrade, so local
# changes are discarded. See CLAUDE/LLM-INSTALL.md, "Which Files Under
# .claude/ Are Yours?", for the full list and the linter exclusions.
#
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

Two further injection families share the same choke point (idle + empty input
box, subordinate to compact/continue): a ``/goal`` typed from a daemon-written
goal-intent signal (Plan 00269), and an ``/effort`` raise (Plan 00278) when
the sidecar's live effort sits BELOW its model family's configured floor
(defaults fable=low, opus=high, sonnet=high; override via
``CCY_MIN_EFFORT_LEVELS``) — with the floor raised to xhigh for a ranked
model-family DOWNGRADE episode, e.g. a security-triggered fable → opus
switch falls through to opus at xhigh, not opus at fable's low. This family
only ever RAISES effort — with one sanctioned exception: the supervisor also
types ``/model <original family>`` to flip a session-sticky downgrade back
(capped, flip-flop backoff). The restore is TURN-GATED, not time-gated: the
classifier flags turns, so recovery is safe the moment the flagged turn ends,
and the injection choke point (idle + empty input box) is exactly that
boundary — the restore fires on the first injectable tick after the
downgrade. ``CCY_MODEL_RESTORE_SECONDS`` adds an optional EXTRA quiet delay
(default 0; "off" or negative disables auto-restore).

On a REPEATED downgrade — a flip-flop, where a prior auto-restore was undone
because the saturated context re-tripped the classifier on the next flagged
turn — restoring the model alone cannot win. So, opt-in via ``CCY_FLAG_COMPACT``
(default OFF; ``1``/``true``/``yes``/``on`` enables), the supervisor fires ONE
armed ``/compact`` that asks the agent to summarise the sensitive material at a
HIGH LEVEL, omitting the low-level specifics — so the compacted context stops
re-triggering the classifier and the subsequent restore sticks. It is capped
per process (``_MAX_FLAG_COMPACTIONS``) with a flip-flop backoff, audit-trailed
with a 🧽 glyph, and fires only when idle (no ESC needed; the resume rides the
normal compaction-signal path). It is checked just BEFORE the model restore, so
on a qualifying flip-flop the compact fires instead of a re-restore.

Every ``/model <family>`` injection -- this auto-restore AND the manual
override below alike -- GUARANTEES a coupled ``/effort`` correction on the
very next injectable tick, unconditionally: switching TO the TOP-ranked
family (fable) drives effort DOWN to its configured floor (the one
sanctioned lowering, so fable never idles at xhigh burning account
allowance); switching to anything else drives effort UP to
``_DOWNGRADE_TARGET_EFFORT`` (xhigh), so a still-degraded fallback model
gets maximum compensating effort. This is unconditional — it never waits
for a downgrade episode to be open, nor for a later sidecar reading to
confirm the switch landed, which is exactly what a purely reading-driven
reset previously missed for a manual override.

Every ``/model <family>`` injection (the auto-restore above, and the manual
override below) sends a SECOND, confirming Enter after the normal submit:
Claude Code's model switch shows a confirmation dialog that the ordinary
single-Enter submit does not clear. The count is configurable via
``CCY_MODEL_CONFIRM_ENTERS`` (default 1). Every ``/effort <level>``
injection sends its own confirming Enter the same way — the effort selector
also needs one — configurable via ``CCY_EFFORT_CONFIRM_ENTERS`` (default 1).
A session can also be switched
on demand — for end-to-end testing, or a genuine manual override — by
writing a ``<session>.model-switch-intent`` signal (mirroring the
goal-intent signal) and consuming it at the same idle choke point, ahead of
goal/effort/auto-model-restore. The CLI helper ``--emit-model-switch
<family>`` writes one for whichever session owns the newest context
sidecar; it does not start a supervisor.

It also GUARDS the session against accidental terminal control keys that would
otherwise freeze or kill it: Ctrl+Z (SUSP) is stripped from the forwarded input
(``strip_suspend``) AND, belt-and-braces, the stop/quit SIGNALS are swallowed if
ever delivered (``install_input_signal_guards``: SIGTSTP + SIGQUIT, plus ignored
SIGTTIN/SIGTTOU). Ctrl+C (SIGINT) is deliberately left working. Each swallow
surfaces a transient status-line notice via the message channel below.

Usage:
    claude-supervise.py [--dry-run | --arm] [--log PATH] -- <child argv...>

HOST-TIER SURFACE (Plan 00317). The PTY host (`supervise()`/`_forward_io()`)
never reloads -- it owns the live child. Everything it does beyond raw byte
forwarding and child/worker lifecycle is intentionally minimal, audited in
`CLAUDE/Plan/00317-supervisor-host-thin-shim/AUDIT.md`:

  * the Ctrl+C double-press byte-level swallow (`CtrlCGate.filter`) and
    Ctrl+Z byte strip (`strip_suspend`) -- must act on a byte BEFORE it is
    forwarded to the child, so a worker round-trip is not viable here;
  * the fixed per-process signal guards (`install_input_signal_guards`);
  * injecting a `TickOutcome` the worker already decided on
    (`_apply_decision`), and adopting the worker's post-tick state.

Typed-command recognition (`/compact`, `/model <x>`, `/effort <x>`) runs
WORKER-SIDE: the host only forwards raw stdin bytes into a bounded
`RawInputTap`, drained each tick into `TickFacts.human_raw_input`; the
`--worker` subprocess owns the `HumanInputLine` parser that recognises
commands from it, so a code change to recognition hot-reloads with the rest
of the worker (`run_worker`'s own restart-scoped instance). The host's own
`HumanInputLine` (on `InputActivity.line`) is kept ONLY to feed the
in-process fallback path (`_poll_once`, used when the worker is
unavailable) -- an intentional, already-accepted non-hot-reloading
degradation, not the live path.

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
import base64
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
from collections.abc import Callable, Mapping
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
__version__ = "3.60.0"

# Absolute path to THIS running script — hashed for staleness detection so the
# daemon can tell when the on-disk supervisor differs from the running one.
_SELF_PATH = Path(__file__).resolve()

_READ_CHUNK_SIZE = 4096
_FALLBACK_WINSIZE = struct.pack("HHHH", 24, 80, 0, 0)
_LOG_SUBDIRECTORY = "supervise"
_LOG_FILENAME = "decision.log"
# Plan 00181: a red-but-idle session ticks every ~1-2s indefinitely, each tick
# potentially appending a NOOP-reason line, so decision.log is an unbounded
# disk time-bomb. Front-cap it: when it exceeds _DECISION_LOG_MAX_BYTES after a
# write, drop the oldest bytes so only the newest _DECISION_LOG_RETAIN_BYTES
# (whole lines) survive. RETAIN < MAX gives hysteresis so a log sitting at the
# ceiling is not rewritten on every single append. This mirrors the daemon's
# utils.retention.cap_log_file, reimplemented inline because the standalone
# supervisor cannot import daemon modules.
_DECISION_LOG_MAX_BYTES = 4 * 1024 * 1024
_DECISION_LOG_RETAIN_BYTES = 2 * 1024 * 1024
# Runtime identity file the running supervisor writes for staleness detection
# (Plan 00164 Phase 3). Lives in the same 'supervise' subdir as the decision log.
_SUPERVISOR_STATUS_FILENAME = "supervisor-status.json"

_USAGE = "Usage: claude-supervise.py [--dry-run | --arm] [--log PATH] -- <child argv...>\n"

# CLI test-trigger mode: writes a manual model-switch signal and exits --
# never starts a supervisor. See `_run_emit_model_switch`.
_EMIT_MODEL_SWITCH_FLAG = "--emit-model-switch"

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

# ─── DOGFOODING: EDITING THIS FILE DOES NOT TAKE EFFECT UNTIL THE WORKER RELOADS ───
# Every injection decision runs in the `--worker` SUBPROCESS, not the running
# PTY host. So a fresh edit to this file is INERT in the live session until that
# subprocess is respawned from the new code. The host auto-respawns it within
# ~_WORKER_RELOAD_CHECK_SECONDS of a *content* change (reload_if_stale compares a
# content hash, and an mtime pre-check gates it), on the next tick — the PTY/child
# is never touched, so no full Claude Code session restart is needed.
#
# Two traps that make an edit look shipped while the worker is still stale — both
# have bitten this project, hence this note:
#   1. A bare `touch` (mtime bumps, content unchanged) triggers NOTHING — the
#      content hash is identical. Only a real content change reloads the worker.
#   2. A redeploy that PRESERVES mtime (`cp -p`, `rsync -a`, some installers) can
#      change the content WITHOUT advancing mtime, so the mtime pre-check skips
#      the hash and the reload never fires.
#
# To dogfood a change to this file IMMEDIATELY AND CORRECTLY, do not assume — VERIFY
# the worker actually reloaded before testing behaviour:
#     ps -eo pid,lstart,args | grep 'claude-supervise.py --worker' | grep -v grep
# A NEW pid / start-time means the new code is live. If it has not changed, force
# it: `kill <worker-pid>` — the host's `if not worker.alive(): worker.restart()`
# path respawns a fresh worker from current on-disk code on its next tick. Never
# restart the whole ccy session just to reload the worker.
# See also .claude/rules/ccy-supervisor-dogfooding.md.

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
# Slash-line observability bounds: submitted '/'-prefixed lines are recorded
# for the worker's diagnostic log, truncated and capped so a paste storm
# cannot bloat memory or the log.
_SLASH_BYTE = 0x2F
_SLASH_OBSERVED_MAX_CHARS = 80
_SLASH_OBSERVED_MAX_LINES = 8
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


# Ctrl+C double-press guard (Plan 00312). In current Claude Code a single ^C
# kills background agents, so one accidental press can destroy hours of
# delegated work. The outer terminal is raw, so Ctrl+C arrives as a lone 0x03
# byte on stdin — the gate swallows the first lone press, arms a confirm
# window, and forwards a second press inside the window (spamming always
# wins). Only a chunk that is EXACTLY one 0x03 byte counts as a press: a 0x03
# inside a larger chunk is a paste burst or a coalesced spam and passes
# through untouched, so pasted content is never corrupted and the escape
# hatch can never be delayed by read coalescing.
_INTERRUPT_BYTE = 0x03
_LONE_INTERRUPT_CHUNK = bytes([_INTERRUPT_BYTE])
_CTRL_C_CONFIRM_WINDOW_SECONDS = 2.0
_CTRL_C_GUARD_ENV_VAR = "CCY_CTRL_C_GUARD"
_CTRL_C_WINDOW_ENV_VAR = "CCY_CTRL_C_WINDOW_SECONDS"
_CTRL_C_EVENT_SWALLOWED = "ctrl-c-swallowed"
_CTRL_C_EVENT_FORWARDED = "ctrl-c-forwarded"
_CTRL_C_FALSE_WORDS = frozenset({"0", "false", "no", "off"})


def _resolve_ctrl_c_guard_enabled(env: Mapping[str, str] | None = None) -> bool:
    """Resolve the Ctrl+C guard enabled flag (default ON; CCY_CTRL_C_GUARD=0 off)."""
    resolved_env = env if env is not None else os.environ
    raw = resolved_env.get(_CTRL_C_GUARD_ENV_VAR, "").strip().lower()
    return raw not in _CTRL_C_FALSE_WORDS


def _resolve_ctrl_c_window_seconds(env: Mapping[str, str] | None = None) -> float:
    """Resolve the confirm window seconds (CCY_CTRL_C_WINDOW_SECONDS; default 2.0).

    Garbage or non-positive values fall back to the default rather than
    disabling interruption semantics in a surprising way.
    """
    resolved_env = env if env is not None else os.environ
    raw = resolved_env.get(_CTRL_C_WINDOW_ENV_VAR, "").strip()
    if not raw:
        return _CTRL_C_CONFIRM_WINDOW_SECONDS
    try:
        value = float(raw)
    except ValueError:
        return _CTRL_C_CONFIRM_WINDOW_SECONDS
    if value <= 0:
        return _CTRL_C_CONFIRM_WINDOW_SECONDS
    return value


class CtrlCGate:
    """Double-press gate for Ctrl+C on the supervisor's stdin path.

    ``filter(data)`` returns ``(forwarded_bytes, event)`` where ``event`` is
    ``_CTRL_C_EVENT_SWALLOWED`` when a lone first press was withheld,
    ``_CTRL_C_EVENT_FORWARDED`` when a confirmed second press goes through,
    or ``None`` when the gate did not intervene. Non-press chunks (anything
    other than exactly ``b"\\x03"``) pass through untouched and never disturb
    an armed window — typing between the two presses must not disarm it.
    """

    def __init__(
        self,
        *,
        window_seconds: float = _CTRL_C_CONFIRM_WINDOW_SECONDS,
        enabled: bool = True,
        clock: Callable[[], float] = time.monotonic,
    ) -> None:
        self._window_seconds = window_seconds
        self._enabled = enabled
        self._clock = clock
        self._armed_at: float | None = None

    @property
    def window_seconds(self) -> float:
        return self._window_seconds

    def filter(self, data: bytes) -> tuple[bytes, str | None]:
        """Gate one stdin chunk; see class docstring for the contract."""
        if not self._enabled or data != _LONE_INTERRUPT_CHUNK:
            return data, None
        now = self._clock()
        if self._armed_at is not None and now - self._armed_at <= self._window_seconds:
            self._armed_at = None
            return data, _CTRL_C_EVENT_FORWARDED
        self._armed_at = now
        return b"", _CTRL_C_EVENT_SWALLOWED


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
        # Plan 00316: the raw argument text of a submitted human `/model <x>`
        # or `/effort <x>` line -- cleared by take_model_submitted()/
        # take_effort_submitted() so each typed command is consumed exactly
        # once. A submission with no argument (bare `/model`) is not tracked
        # -- it opens Claude Code's own selector rather than naming a target.
        self._model_submitted: str | None = None
        self._effort_submitted: str | None = None
        # Observability: every submitted line starting with '/' is recorded
        # verbatim (bounded), matched or not, so a recognition MISS (e.g.
        # autocomplete swallowing the argument bytes) is diagnosable from the
        # worker's diagnostic log instead of failing invisibly.
        self._slash_submitted: list[str] = []

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
            if byte in _LINE_SUBMIT_BYTES:
                if self._buffer_is_compact():
                    self._compact_submitted = True
                model_arg = self._buffer_command_arg(_MODEL_COMMAND)
                if model_arg:
                    self._model_submitted = model_arg
                effort_arg = self._buffer_command_arg(_EFFORT_COMMAND)
                if effort_arg:
                    self._effort_submitted = effort_arg
                if self._buffer and self._buffer[0] == _SLASH_BYTE:
                    text = bytes(self._buffer).decode("utf-8", errors="replace")
                    self._slash_submitted.append(text[:_SLASH_OBSERVED_MAX_CHARS])
                    del self._slash_submitted[:-_SLASH_OBSERVED_MAX_LINES]
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

    def _buffer_command_arg(self, command: str) -> str | None:
        """Return the trimmed argument of a submitted ``command <arg>`` line.

        ``None`` when the line does not start with ``command`` followed by
        whitespace and a non-empty argument -- a bare ``/model`` with no
        target opens Claude Code's own selector and is not a command this
        class can classify.
        """
        text = bytes(self._buffer).decode("utf-8", errors="ignore").strip()
        prefix = f"{command} "
        if not text.startswith(prefix):
            return None
        arg = text[len(prefix) :].strip()
        return arg or None

    def take_compact_submitted(self) -> bool:
        """Return True once if a human `/compact` was submitted, then clear it.

        Edge-triggered and consume-once so a single human `/compact` defers a
        single supervisor evaluation, never a permanent suppression.
        """
        if self._compact_submitted:
            self._compact_submitted = False
            return True
        return False

    def take_model_submitted(self) -> str | None:
        """Return the argument of a submitted human `/model <x>`, then clear it.

        Edge-triggered and consume-once, mirroring ``take_compact_submitted``
        (Plan 00316) -- so a manual model command is recorded exactly once
        per submission, however many ticks pass before it is consumed.
        """
        arg = self._model_submitted
        self._model_submitted = None
        return arg

    def take_effort_submitted(self) -> str | None:
        """Return the argument of a submitted human `/effort <x>`, then clear it."""
        arg = self._effort_submitted
        self._effort_submitted = None
        return arg

    def take_slash_submitted(self) -> list[str]:
        """Return submitted '/'-prefixed lines observed since last checked."""
        lines = self._slash_submitted
        self._slash_submitted = []
        return lines

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

    def take_model_submitted(self) -> str | None:
        """Return the argument of a human `/model <x>` submitted since last checked."""
        return self.line.take_model_submitted()

    def take_effort_submitted(self) -> str | None:
        """Return the argument of a human `/effort <x>` submitted since last checked."""
        return self.line.take_effort_submitted()


_DEFAULT_RAW_INPUT_TAP_MAX_BYTES = 4096


class RawInputTap:
    """Bounded, fail-open buffer of raw stdin bytes forwarded to the child.

    Plan 00317: the host-side ``HumanInputLine`` in ``InputActivity`` remains
    for the in-process fallback path, but the LIVE typed-command recognition
    now runs inside the hot-reloadable ``--worker`` subprocess, fed by this
    tap (drained into ``TickFacts.human_raw_input`` once per tick). The tap
    itself does no parsing -- it only accumulates bytes -- so it carries no
    recognition logic to keep host-side.

    Bounded so a slow/dead worker (drain not happening) can never grow this
    buffer without limit: appending past ``max_bytes`` drops the OLDEST bytes,
    never raises, and never blocks the forwarding call site.
    """

    def __init__(self, max_bytes: int = _DEFAULT_RAW_INPUT_TAP_MAX_BYTES) -> None:
        self._buffer = bytearray()
        self._max_bytes = max_bytes

    def append(self, data: bytes) -> None:
        """Append forwarded bytes, dropping the oldest on overflow."""
        self._buffer.extend(data)
        overflow = len(self._buffer) - self._max_bytes
        if overflow > 0:
            del self._buffer[:overflow]

    def drain(self) -> bytes:
        """Return and clear everything buffered since the last drain."""
        data = bytes(self._buffer)
        self._buffer.clear()
        return data


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
        self._cap_if_needed()
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
        self._cap_if_needed()
        self._last_noop_message = message

    def _cap_if_needed(self) -> None:
        """Front-truncate the log to below the byte ceiling, keeping newest lines.

        Best-effort disk-bomb guard (Plan 00181): a capping failure must NEVER
        crash a supervisor tick or lose the line just written, so IO errors are
        reported to stderr and swallowed rather than propagated. The successful
        path drops the oldest bytes and the (now partial) leading line so only
        the newest ``_DECISION_LOG_RETAIN_BYTES`` of WHOLE lines remain.
        """
        try:
            size = self._path.stat().st_size
        except OSError:
            return
        if size <= _DECISION_LOG_MAX_BYTES:
            return
        try:
            with self._path.open("rb") as handle:
                handle.seek(max(0, size - _DECISION_LOG_RETAIN_BYTES))
                tail = handle.read()
            # Drop the partial first line so the file starts on a line boundary.
            newline = tail.find(b"\n")
            kept = tail[newline + 1 :] if newline != -1 else tail
            tmp = self._path.with_name(self._path.name + ".retain.tmp")
            tmp.write_bytes(kept)
            tmp.replace(self._path)
        except OSError as exc:
            # FAIL LOUD but not FATAL: surface the cap failure without aborting
            # the supervision loop (the appended line is already safely on disk).
            sys.stderr.write(f"[claude-supervise] decision.log cap failed: {exc}\n")


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

# Goal-intent signal files (Plan 00269) are written by the daemon's
# goal_injection PostToolUse handler (or `hooks-daemon inject-goal`) as
# ``<session>.goal-intent`` -- again deliberately NOT ``*.json``. Same TTL
# reasoning as the compaction signal: plan-execution start is usually an idle
# moment, but the input-box gate can defer, so the window is generous. The
# signal is consumed (unlinked) on injection, so a generous TTL cannot
# re-fire it.
_GOAL_SIGNAL_GLOB = "*.goal-intent"
_DEFAULT_GOAL_SIGNAL_TTL_SECONDS = 600.0

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
# Plan 00182: throttle the expensive full-/proc environ scan. Once our session
# ids are learned they persist in the accumulate-set above, so re-scanning every
# worker tick (poll interval 2s) was the latency source that could push a tick
# past the 2s worker read timeout and trigger the stale-reply desync. Re-scan at
# most once per TTL; an EMPTY cache always forces a scan so discovery is never
# starved (fail-safe: a supervisor that has not yet found its session must look).
_OWN_SESSION_SCAN_TTL_SECONDS = 30.0
_own_session_ids_last_scan: float | None = None


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


def cached_own_session_ids(
    proc_root: Path | None = None, *, now: float | None = None
) -> frozenset[str]:
    """Union-accumulate and return the supervisor's own-session-id set.

    Plan 00182: the full /proc environ scan is throttled to
    ``_OWN_SESSION_SCAN_TTL_SECONDS`` once at least one id is known -- most ticks
    then return the accumulated set without touching /proc, keeping the worker
    tick well under its read timeout. An EMPTY cache always re-scans so a
    supervisor that has not yet discovered its own session is never starved.
    ``now`` (monotonic seconds) is injectable for tests; production uses the
    monotonic clock.
    """
    global _own_session_ids_last_scan
    current = time.monotonic() if now is None else now
    due = (
        not _own_session_ids_cache
        or _own_session_ids_last_scan is None
        or (current - _own_session_ids_last_scan) >= _OWN_SESSION_SCAN_TTL_SECONDS
    )
    if due:
        _own_session_ids_cache.update(resolve_own_session_ids(proc_root))
        _own_session_ids_last_scan = current
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


# ── Effort restore on model downgrade (Plan 00278) ──────────────────────────
# A session downgraded from a higher-ranked model family (e.g. a
# security-triggered fable → opus switch) inherits its previous effort
# setting; "fable low" must fall through to "opus xhigh", not "opus low".
# The supervisor tracks the foreground sidecar's model family per session,
# enforces per-model effort FLOORS, and raises the floor to xhigh for a
# downgrade episode.
#
# INVARIANT: this family only ever RAISES effort. An injection fires only
# when the live effort ranks strictly BELOW the target, so a session already
# at or above its floor (or at max) is never touched.
#
# Ascending capability rank; "mythos" shares fable's rank (same underlying
# model). Matched by token containment on the sidecar's model_id; unknown
# ids have no rank and never trigger.
_MODEL_FAMILY_RANKS: dict[str, int] = {
    "haiku": 0,
    "sonnet": 1,
    "opus": 2,
    "fable": 3,
    "mythos": 3,
}
# "mythos" is an alias: _model_family canonicalises it to "fable".
_MODEL_FAMILY_CANONICAL: dict[str, str] = {"mythos": "fable"}
# The single highest capability rank (fable/mythos). Used by the coupled-
# effort mechanism to distinguish "switching TO the best model" (a sanctioned
# effort LOWERING to its floor) from every other destination (a downgrade or
# a partial restore, which targets _DOWNGRADE_TARGET_EFFORT instead).
_TOP_FAMILY_RANK = max(_MODEL_FAMILY_RANKS.values())
_EFFORT_COMMAND = "/effort"
# Ascending effort ranks (Claude Code's own low→max ordering).
_EFFORT_RANKS: dict[str, int] = {"low": 0, "medium": 1, "high": 2, "xhigh": 3, "max": 4}
# The effort a downgraded session is restored to — the whole point of the
# family: "fable low" falls through to the fallback model at XHIGH, so the
# downgrade target outranks any configured per-model minimum.
_DOWNGRADE_TARGET_EFFORT = "xhigh"
# Per-model minimum effort levels (Plan 00278 Task 2b.2). No official
# per-model effort mechanism exists in Claude Code (effort is one global
# setting that survives a safety fallback unchanged), so the supervisor
# enforces these floors. Override via the env var below, e.g.
# CCY_MIN_EFFORT_LEVELS="fable=low,opus=xhigh".
_DEFAULT_MIN_EFFORT_LEVELS: dict[str, str] = {
    "fable": "low",
    "opus": "high",
    "sonnet": "high",
    "haiku": "low",
}
_MIN_EFFORT_ENV_VAR = "CCY_MIN_EFFORT_LEVELS"
# After a successful /effort injection the sidecar keeps reporting the OLD
# effort until the next status render; without a cooldown the stale reading
# would re-open the episode and burn the cap on duplicates.
_EFFORT_REINJECT_COOLDOWN_SECONDS = 180.0
# Family-specific per-process cap: a flapping model_id cannot type forever.
_MAX_EFFORT_INJECTIONS = 3
_DRY_RUN_EFFORT_BODY_PREFIX = "would inject /effort (dry-run — no real /effort sent):"
# ── Model restore after a downgrade (Plan 00278 Task 2b.3) ──────────────────
# The safety fallback is session-sticky, but flipping back manually works
# once the flaggable TURN has passed — the classifier flags turns, not
# sessions' worth of wall-clock. The real gate is therefore the injection
# choke point itself (idle + empty input box = the flagged turn is over), so
# the default extra delay is ZERO: restore fires on the first injectable
# tick after the downgrade (joseph: turn-gated, not time-gated). A positive
# CCY_MODEL_RESTORE_SECONDS adds an optional extra quiet delay on top;
# "off" (or any negative value) disables auto-restore entirely. A successful
# flip-back then RESETS effort down to the restored family's floor (the one
# sanctioned lowering: fable at xhigh eats account allowance).
_MODEL_COMMAND = "/model"
# Plan 00316: BACKSTOP expiry on a user-typed `/model <family>` command. The
# manual note is a latch consumed by the first sidecar reading that shows the
# family -- however late that reading arrives, because a BUSY session can defer
# the first observation for many minutes (the sidecar only refreshes on status
# renders). This window exists only so a typed command whose switch never
# landed at all cannot re-classify a much-later unrelated silent drop to the
# same family; it must dwarf any plausible busy spell.
_MANUAL_MODEL_WINDOW_SECONDS = 3600.0
_DEFAULT_MODEL_RESTORE_DELAY_SECONDS = 0.0
_MODEL_RESTORE_DISABLED_SENTINEL = -1.0
_MODEL_RESTORE_ENV_VAR = "CCY_MODEL_RESTORE_SECONDS"
# Flip-flop guard: a re-downgrade soon after a restore means the classifier
# still fires — do not bounce the session between models.
_MODEL_RESTORE_BACKOFF_SECONDS = 3600.0
# Per-process lifetime cap on auto-restores.
_MAX_MODEL_RESTORES = 2
_DRY_RUN_MODEL_BODY_PREFIX = "would inject /model (dry-run — no real /model sent):"

# ── DROP ANCHOR: fable-above-low invariant (Plan 00297) ─────────────────────
# On 2026-08-31 the effort floor injected `/effort xhigh` on Opus; a model
# flip to Fable carried the xhigh over; the follow-up `/effort low`
# injection was SWALLOWED by a busy session while the audit recorded it
# done -- Fable ran at XHIGH for roughly an hour. The floor/coupled-effort
# machinery above cannot by construction catch this: the floor family only
# ever RAISES effort (see its INVARIANT docstring), and the one-shot
# post-/model-switch correction is marked done on a successful PTY WRITE,
# never on a verified read-back of the session's actual state. DROP ANCHOR
# is a SEPARATE, continuously re-evaluated invariant -- `model == fable`
# implies `effort == low` -- checked on every fresh, non-stale sidecar
# reading, with its own retry cooldown and escalation bound, independent of
# the floor mechanism's cap/cooldown so an unverified violation always keeps
# retrying.
_ANCHOR_TARGET_EFFORT = "low"
# Own, tighter cooldown than `_EFFORT_REINJECT_COOLDOWN_SECONDS` -- an
# active anchor must retry aggressively, but still not spam every tick.
_ANCHOR_RETRY_COOLDOWN_SECONDS = 25.0
# After this many injection attempts with no verified read-back, OR after
# this much wall-clock since the violation was first observed (whichever
# comes first), the anchor is considered ESCALATED: the host posts a loud
# owner-facing alert (see `supervise()`) while continuing to retry.
_ANCHOR_MAX_ATTEMPTS = 3
_ANCHOR_ESCALATION_BOUND_SECONDS = 300.0
_ANCHOR_ALERT_TEXT = (
    "🚨 DROP ANCHOR: fable stuck above low effort after repeated /effort low "
    "attempts — owner attention needed (see decision.log)"
)
# Plan 00297 follow-up (owner-approved, ruling: "Compaction is uninterruptible
# AFAIK... Esc can be disruptive but its basically OK - its MUCH MUCH better
# than leaving fable running at xhigh"): once ESCALATED, the anchor also
# sends a raw ESC to interrupt an in-flight turn that is swallowing the
# `/effort low` injection -- the same WOULD_ESCAPE keystroke path the
# AWAIT_COMPACTING flush already uses. Its OWN cooldown, separate from
# `_ANCHOR_RETRY_COOLDOWN_SECONDS`, so ESC does not fire every retry tick.
_ANCHOR_ESC_COOLDOWN_SECONDS = 60.0
_DRY_RUN_ANCHOR_ESCAPE_BODY = (
    "would send [esc] to interrupt the in-flight turn (dry-run — no real ESC sent)"
)


def _parse_model_restore_delay(raw: str) -> float:
    """Parse the restore-delay override.

    "off" (or any negative number) disables auto-restore; 0 means restore on
    the first injectable tick after the downgrade (the turn gate alone);
    a positive value adds that many seconds of extra quiet delay; junk keeps
    the default.
    """
    value = raw.strip().lower()
    if value == "off":
        return _MODEL_RESTORE_DISABLED_SENTINEL
    try:
        return float(value)
    except ValueError:
        return _DEFAULT_MODEL_RESTORE_DELAY_SECONDS


def _model_restore_delay_from_env() -> float:
    """Resolve the effective restore delay (env override or default)."""
    raw = os.environ.get(_MODEL_RESTORE_ENV_VAR)
    if raw is None:
        return _DEFAULT_MODEL_RESTORE_DELAY_SECONDS
    return _parse_model_restore_delay(raw)


# Every ``/model <family>`` injection -- the auto-restore above AND the
# manual override signal below -- sends this many ADDITIONAL standalone
# Enter keystrokes after the normal submit. Claude Code's model switch shows
# a confirmation dialog that the ordinary single-Enter submit does not
# clear, so without this the switch never completes.
_DEFAULT_MODEL_CONFIRM_ENTERS = 1
_MODEL_CONFIRM_ENTERS_ENV_VAR = "CCY_MODEL_CONFIRM_ENTERS"


def _parse_model_confirm_enters(raw: str) -> int:
    """Parse the confirm-Enter count override; junk or negative keeps default."""
    try:
        value = int(raw.strip())
    except ValueError:
        return _DEFAULT_MODEL_CONFIRM_ENTERS
    return value if value >= 0 else _DEFAULT_MODEL_CONFIRM_ENTERS


def _model_confirm_enters_from_env() -> int:
    """Resolve the effective confirm-Enter count (env override or default)."""
    raw = os.environ.get(_MODEL_CONFIRM_ENTERS_ENV_VAR)
    if raw is None:
        return _DEFAULT_MODEL_CONFIRM_ENTERS
    return _parse_model_confirm_enters(raw)


# Every ``/effort <level>`` injection needs the same treatment: Claude Code's
# effort selector shows a confirmation UI that the ordinary single-Enter
# submit does not clear, so without a confirming Enter the level change sits
# unconfirmed forever (a human had to press Enter for the first live coupled
# correction).
_DEFAULT_EFFORT_CONFIRM_ENTERS = 1
_EFFORT_CONFIRM_ENTERS_ENV_VAR = "CCY_EFFORT_CONFIRM_ENTERS"


def _parse_effort_confirm_enters(raw: str) -> int:
    """Parse the effort confirm-Enter override; junk or negative keeps default."""
    try:
        value = int(raw.strip())
    except ValueError:
        return _DEFAULT_EFFORT_CONFIRM_ENTERS
    return value if value >= 0 else _DEFAULT_EFFORT_CONFIRM_ENTERS


def _effort_confirm_enters_from_env() -> int:
    """Resolve the effective effort confirm-Enter count (env override or default)."""
    raw = os.environ.get(_EFFORT_CONFIRM_ENTERS_ENV_VAR)
    if raw is None:
        return _DEFAULT_EFFORT_CONFIRM_ENTERS
    return _parse_effort_confirm_enters(raw)


# /model and /effort injections leave NO trace in the chat (unlike /compact
# and /goal, whose payloads carry visible text), so after a successful silent
# injection the supervisor flushes a transient status-line banner naming what
# it did (Plan 00318). Pending audit items are bounded so a stuck flush can
# never grow the machine state without limit.
_MAX_AUDIT_ITEMS = 8


# ── Flag-cleaning compaction on repeated downgrade (Plan 00281) ──────────────
# When a downgrade RECURS after a prior auto-restore (a flip-flop the model
# restore cannot win because the CONTEXT keeps re-tripping the classifier), the
# supervisor can fire ONE armed /compact instructing Claude to summarise the
# flaggable material at a HIGH LEVEL, cleaning the context so the next restore
# sticks. OFF by default: /compact rewrites context, so a project not doing
# flaggable work never wants it fired automatically.
_DEFAULT_FLAG_COMPACT_ENABLED = False
_FLAG_COMPACT_ENV_VAR = "CCY_FLAG_COMPACT"
_FLAG_COMPACT_TRUE_VALUES = ("1", "true", "yes", "on")
# At most this many flag-cleaning compactions per worker process, with a
# backoff between them: /compact is heavy and must never storm.
_MAX_FLAG_COMPACTIONS = 1
_FLAG_COMPACT_BACKOFF_SECONDS = 1800.0


def _parse_flag_compact_enabled(raw: str) -> bool:
    """Parse the CCY_FLAG_COMPACT toggle; anything not clearly true is False."""
    return raw.strip().lower() in _FLAG_COMPACT_TRUE_VALUES


def _flag_compact_enabled_from_env() -> bool:
    """Resolve the effective flag-compact toggle (env override or default off)."""
    raw = os.environ.get(_FLAG_COMPACT_ENV_VAR)
    if raw is None:
        return _DEFAULT_FLAG_COMPACT_ENABLED
    return _parse_flag_compact_enabled(raw)


def _parse_min_effort_levels(raw: str) -> dict[str, str]:
    """Parse ``family=level,...`` overrides onto the default minimum map.

    Unknown families and unknown levels are ignored (the defaults stand) —
    a typo in the env var must degrade to defaults, never crash the launch.
    """
    result = dict(_DEFAULT_MIN_EFFORT_LEVELS)
    for part in raw.split(","):
        family, sep, level = part.partition("=")
        family = family.strip().lower()
        level = level.strip().lower()
        if sep and family in _MODEL_FAMILY_RANKS and level in _EFFORT_RANKS:
            result[_MODEL_FAMILY_CANONICAL.get(family, family)] = level
    return result


def _min_effort_levels_from_env() -> dict[str, str]:
    """Resolve the effective per-model minimum map (defaults + env overrides).

    Read via the environment so the HOST and the policy WORKER subprocess
    (which reconstructs its own CompactPolicy) resolve identical maps.
    """
    return _parse_min_effort_levels(os.environ.get(_MIN_EFFORT_ENV_VAR, ""))


def _model_family(model_id: str) -> str | None:
    """Return the canonical model family for ``model_id``, or None if unknown."""
    lowered = model_id.lower()
    for token in _MODEL_FAMILY_RANKS:
        if token in lowered:
            return _MODEL_FAMILY_CANONICAL.get(token, token)
    return None


def _family_rank(family: str) -> int:
    """Return the capability rank of a known family (KeyError on unknown)."""
    return _MODEL_FAMILY_RANKS[family]


class Decision(enum.Enum):
    """What the supervisor WOULD do this evaluation (dry-run logs it)."""

    NOOP = "noop"
    WOULD_COMPACT = "would-compact"
    WOULD_CONTINUE = "would-continue"
    WOULD_ESCAPE = "would-escape"
    WOULD_GOAL = "would-goal"
    WOULD_STANDING_AUTH = "would-standing-auth"
    WOULD_EFFORT = "would-effort"
    WOULD_MODEL = "would-model"
    WOULD_AUDIT = "would-audit"


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
    # Plan 00278: model identity and live effort level, for the model-downgrade
    # effort-restore family. Defaults cover sidecars predating the fields.
    model_id: str = ""
    effort: str | None = None


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
    goal_signal_ttl_seconds: float = _DEFAULT_GOAL_SIGNAL_TTL_SECONDS
    # Plan 00278: per-model effort floors, resolved from the environment so
    # the host and the policy worker (which rebuilds its own policy) agree.
    min_effort_levels: dict[str, str] = field(default_factory=_min_effort_levels_from_env)
    # Plan 00278 Task 2b.3: EXTRA quiet delay before the /model flip-back on
    # top of the turn gate (default 0 — the idle/empty-input injection gate
    # already means the flagged turn is over); negative ("off") disables
    # auto-restore. Env-resolved for the same host/worker agreement.
    model_restore_delay_seconds: float = field(default_factory=_model_restore_delay_from_env)
    # Additional confirming Enters sent after every /model injection (both
    # auto-restore and the manual switch signal). Env-resolved for the same
    # host/worker agreement.
    model_confirm_enters: int = field(default_factory=_model_confirm_enters_from_env)
    # Additional confirming Enters sent after every /effort injection (both
    # the floor-based restore and the coupled post-/model correction) — the
    # effort selector needs one just like the model switch. Env-resolved for
    # the same host/worker agreement.
    effort_confirm_enters: int = field(default_factory=_effort_confirm_enters_from_env)
    # Plan 00281: opt-in flag-cleaning /compact on a repeated (flip-flop)
    # downgrade. OFF by default — auto-compaction rewrites context. Env-resolved
    # for host/worker agreement, like the other Plan 00278/00281 toggles.
    flag_compact_enabled: bool = field(default_factory=_flag_compact_enabled_from_env)


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
    # Plan 00316: the raw argument of a human-typed `/model <x>` / `/effort
    # <x>` line submitted since the last tick, or None. Consumed by
    # `CompactStateMachine.note_manual_model_command`/
    # `note_manual_effort_command` so a manual choice is recognised and never
    # fought by the auto-restore or the coupled-effort default.
    human_model_command: str | None = None
    human_effort_command: str | None = None
    # Plan 00317: raw stdin bytes forwarded since the last tick, base64-encoded
    # (JSON has no byte-string type). Drained from the host's ``RawInputTap``.
    # The worker feeds this into its OWN persistent ``HumanInputLine`` and
    # RECOMPUTES ``human_compact_submitted``/``human_model_command``/
    # ``human_effort_command``/``input_line_empty`` from it, overriding
    # whatever the host sent above -- that override is what makes typed-
    # command recognition hot-reloadable (a worker restart alone picks up a
    # code change). Empty string when nothing was forwarded since last tick,
    # and on the in-process fallback path (no worker, host's own
    # ``HumanInputLine`` is authoritative there).
    human_raw_input: str = ""
    # The host's authoritative CompactStateMachine state for this tick (Plan
    # 00164 Phase 4 fix). The worker loads it before deciding so it never runs on
    # divergent state; None on the in-process path (the machine is already live).
    machine_state: dict[str, object] | None = None
    # Per-request correlation id (Plan 00182). The host stamps a monotonically
    # increasing id on each worker request; the worker echoes it back on the
    # matching TickOutcome. The host drops any reply whose id does not match the
    # current request, so a reply left buffered by a timed-out (slow) tick can
    # never be consumed on a LATER tick and injected out of turn. 0 on the
    # in-process path (no worker, no correlation needed).
    tick_id: int = 0


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
    # Correlation id echoed from the originating TickFacts (Plan 00182). The
    # worker copies the request's ``tick_id`` onto its reply so the host can
    # discard a stale reply from a previously timed-out tick. 0 on the
    # in-process path and on legacy replies without the field.
    tick_id: int = 0
    # Additional confirming Enters the host must send after ``payload`` (only
    # ever non-zero for a ``/model`` injection -- see ``_perform_injection``).
    # 0 for every other decision and for legacy replies without the field.
    confirm_enters: int = 0
    # Plan 00278 continuation: populated only when ``decision_value`` is
    # WOULD_MODEL, so the HOST can arm the mandatory coupled-effort
    # correction after a successful injection (``arm_coupled_effort``) --
    # for BOTH the manual test-trigger switch and the auto-restore
    # flip-back. None/False for every other decision and for legacy replies
    # without the fields.
    model_switch_family: str | None = None
    model_switch_session: str | None = None
    model_switch_is_auto_restore: bool = False
    # Plan 00281: True only for the flip-flop flag-cleaning /compact, so the
    # HOST counts it against the flag-compaction cap/backoff
    # (``mark_flag_compaction``) on a successful injection. The capacity-based
    # /compact (Plan 00152) leaves this False — it has its own AWAIT_COMPACTING
    # lifecycle and must not eat into the flag-compaction budget. False for
    # every other decision and for legacy replies without the field.
    is_flag_compact: bool = False
    # Plan 00297: True only for the DROP ANCHOR emergency `/effort low`
    # correction, so the HOST records the attempt against the anchor's own
    # bookkeeping (`mark_anchor_injection`) rather than the floor/coupled
    # mechanisms' -- the anchor is verified by read-back only, never by a
    # successful PTY write. False for every other decision and for legacy
    # replies without the field.
    is_anchor_injection: bool = False
    # Plan 00297 follow-up: True only for the DROP ANCHOR escalation ESC (a
    # WOULD_ESCAPE decision fired to interrupt an in-flight turn that may be
    # swallowing the emergency `/effort low` correction), so the HOST records
    # the send against the anchor's own ESC cooldown (`mark_anchor_esc`)
    # rather than the AWAIT_COMPACTING flush's escape-count bookkeeping.
    # False for every other decision and for legacy replies without the field.
    is_anchor_escape: bool = False
    # Plan 00299: the raw combined /goal text THIS tick decided to inject
    # (None for every other decision), so the HOST can record it as the
    # multi-plan thrash guard (`mark_goal_injection`) on a successful
    # injection only -- a failed PTY write must not update the guard while
    # the signal survives for retry.
    goal_line: str | None = None


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


def _ctrl_c_notice_text(window_seconds: float) -> str:
    """Status-line hint shown when a lone Ctrl+C is swallowed (Plan 00312)."""
    return f"⚠ Ctrl+C intercepted — press Ctrl+C again within {window_seconds:g}s to interrupt"


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
    countdown: bool = False,
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

    ``countdown`` asks the reader to render the seconds remaining until
    ``expires_at`` alongside the text, so a longer-lived notice visibly
    announces that it is transient. Opt-in per message: a keystroke hint whose
    own text already names a window (Ctrl+C's confirm period) would only be
    muddled by a second number.

    Best-effort: a write failure is reported to stderr and returns None rather
    than disturbing the supervised session.

    Returns:
        The message file path on success, or None on failure.
    """
    message_path = _status_message_path(untracked_dir)
    payload: dict[str, object] = {"text": text, "expires_at": expires_at, "level": level}
    if countdown:
        # OMITTED when false, never written as `false`: absent is the reader's
        # default, so an older daemon (no countdown support) sees exactly the
        # payload it always saw, and a keystroke hint stays a bare notice.
        payload["countdown"] = True
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

    def post(
        self,
        text: str,
        *,
        level: str = _STATUS_LEVEL_INFO,
        ttl_seconds: float | None = None,
        countdown: bool = False,
    ) -> Path | None:
        """Write ``text`` as the current status message, honouring the rate limit.

        ``level`` selects the severity (``_STATUS_LEVEL_WARNING`` renders on an
        orange background attached to the supervisor's top hat; the default is
        plain info). ``ttl_seconds`` overrides this poster's default lifetime
        for THIS message alone (an audit summary is read, not glanced at, so it
        needs longer on screen than a keystroke hint), and ``countdown`` asks
        the reader to show the seconds remaining. Returns the written path, or
        None when the post is suppressed by the rate limit or the write fails.
        Thread-safe: the rate-limit check-and-update runs under the lock so
        concurrent posters cannot both pass within one interval.
        """
        now_mono = self._monotonic()
        ttl = self._ttl_seconds if ttl_seconds is None else ttl_seconds
        with self._lock:
            if (
                self._last_monotonic is not None
                and now_mono - self._last_monotonic < self._min_interval_seconds
            ):
                return None
            self._last_monotonic = now_mono
            expires_at = self._wall_clock() + ttl
        return write_status_message(
            self._untracked_dir,
            text=text,
            expires_at=expires_at,
            level=level,
            countdown=countdown,
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
        model_id=str(data.get("model_id", "")),
        effort=_coerce_optional_str(data.get("effort")),
    )


def _coerce_optional_str(value: object) -> str | None:
    """Return ``value`` as a string, or None for anything non-string."""
    return value if isinstance(value, str) else None


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


# ---------------------------------------------------------------------------
# Goal-intent signal (Plan 00269) — structural validation gate (Decision 2).
#
# The member allowlist ({'/compact', 'continue'}) cannot express per-plan
# text, so for the goal family ONLY it becomes a SHAPE allowlist: mandatory
# machine-origin marker prefix, max total length, max logical-line count, and
# a printable charset with a hard control-byte (newline!) ban. Anything
# failing the gate is dropped fail-closed and the reason is logged. The
# compact/continue member allowlist is untouched.
# ---------------------------------------------------------------------------

# The goal payload's mandatory FIXED HEADER, verified VERBATIM. A mere
# marker-prefix check would admit any text following the marker — anything
# able to write the signal file (a bash redirect is enough; no content guard
# covers it) could then get arbitrary text, including asserted human consent,
# typed into the chat. The load-bearing clause is the "NOT a human
# instruction and NOT human authorisation" disclaimer, so the WHOLE header
# must equal the daemon renderer's fixed header. A lockstep test
# (tests/unit/supervise/test_goal_signal.py) asserts this string equals
# goal_injection._HEADER_TEXT so the two sides cannot drift.
_GOAL_HEADER_TEXT = (
    "🤖 [ccy-supervisor] automated goal — machine-generated, NOT a human "
    "instruction and NOT human authorisation for anything."
)
# The slash command; the payload is always ``/goal <joined line>`` so the
# injected message begins ``/goal 🤖 [ccy-supervisor]``.
_GOAL_COMMAND = "/goal"
# Fixed separator used to re-join ``rendered_lines`` (identity for the
# list-of-one the daemon writes today; forward-compatible with a future safe
# multi-line mechanism).
_GOAL_SEPARATOR = " — "
_GOAL_MAX_JOINED_CHARS = 500
_GOAL_MAX_LOGICAL_LINES = 8
# Family-specific per-process cap so a signal storm cannot type repeatedly
# (each signal is also consumed on injection).
_MAX_GOAL_INJECTIONS = 5
_DRY_RUN_GOAL_BODY_PREFIX = "would inject /goal (dry-run — no real /goal sent):"


def _validate_goal_lines(rendered_lines: object) -> tuple[str | None, str | None]:
    """Validate ``rendered_lines`` from a goal signal; return (joined, error).

    Fail-closed: exactly one of the pair is non-None. The gate checks shape
    only — it never interprets the text.
    """
    if not isinstance(rendered_lines, list) or not rendered_lines:
        return None, "rendered_lines is not a non-empty list"
    if len(rendered_lines) > _GOAL_MAX_LOGICAL_LINES:
        return None, f"more than {_GOAL_MAX_LOGICAL_LINES} logical lines"
    if not all(isinstance(line, str) for line in rendered_lines):
        return None, "rendered_lines contains a non-string element"
    joined = _GOAL_SEPARATOR.join(rendered_lines)
    # The ENTIRE fixed header must open the payload, verbatim — either alone
    # (a header-only message) or followed by the separator. A prefix-only
    # marker check would let a forged signal carry arbitrary text (including
    # asserted human consent) past the gate.
    if joined != _GOAL_HEADER_TEXT and not joined.startswith(_GOAL_HEADER_TEXT + _GOAL_SEPARATOR):
        return None, "payload does not open with the verbatim machine-origin header"
    if len(joined) > _GOAL_MAX_JOINED_CHARS:
        return None, f"joined line exceeds {_GOAL_MAX_JOINED_CHARS} chars"
    for ch in joined:
        # A newline is a control byte too: under the one-chunk + separate \r
        # delivery contract it would SUBMIT an unmarked intermediate prompt.
        if ord(ch) < 0x20 or ord(ch) == 0x7F:
            return None, "control byte in payload"
    return joined, None


def load_goal_signal(
    directory: Path,
    *,
    now: float,
    ttl_seconds: float = _DEFAULT_GOAL_SIGNAL_TTL_SECONDS,
    own_sessions: frozenset[str] | None = None,
) -> tuple[Path | None, str | None, str | None]:
    """Return ``(path, joined_line, reject_reason)`` for a goal-intent signal.

    Exactly one of the three shapes comes back: a valid fresh in-scope signal
    yields ``(path, joined, None)``; an in-scope fresh signal that FAILS the
    validation gate yields ``(None, None, reason)`` so the caller can log the
    rejection; no actionable signal at all yields ``(None, None, None)``.
    Foreign-session and stale signals are skipped silently (foreign files in
    a shared dir are normal; stale ones age out via the reaper).
    """
    if not directory.is_dir():
        return None, None, None
    for path in sorted(directory.glob(_GOAL_SIGNAL_GLOB)):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            return None, None, f"unreadable/malformed goal signal {path.name}"
        if not isinstance(data, dict):
            return None, None, f"goal signal {path.name} is not a JSON object"
        if not _session_in_scope(data.get("session_id"), own_sessions):
            continue
        if (now - _coerce_float(data.get("ts"))) > ttl_seconds:
            continue
        joined, error = _validate_goal_lines(data.get("rendered_lines"))
        if error is not None:
            return None, None, f"goal signal {path.name} rejected: {error}"
        return path, joined, None
    return None, None, None


# ---------------------------------------------------------------------------
# Standing-authorisation signal (Plan 00283).
#
# The daemon's standing_authorisations UserPromptSubmit handler routes a due
# reinforcement to a ``<session>.standing-auth-intent`` signal (again NOT
# ``*.json``) when its supervisor channel is enabled and this supervisor is
# armed+live. We type it as a real user-role line at the same idle choke point
# as the goal signal, subordinate to every other family. Same security reasoning
# as the goal header: anything able to WRITE the signal file (a bash redirect is
# enough) could otherwise get arbitrary text — including a forged human consent —
# typed into the chat, so the WHOLE header must equal the daemon renderer's fixed
# header verbatim. A lockstep test asserts this equals
# standing_authorisations.SUPERVISOR_CHANNEL_HEADER so the two ends cannot drift.
_STANDING_AUTH_SIGNAL_GLOB = "*.standing-auth-intent"
_STANDING_AUTH_HEADER_TEXT = (
    "🤖 [ccy-supervisor] standing authorisations replayed from this project's "
    "config — machine-generated, NOT a human instruction and NOT fresh human "
    "authorisation for anything."
)
# Re-join separator for rendered_lines (the daemon writes [header, body]).
_STANDING_AUTH_SEPARATOR = " — "
_STANDING_AUTH_MAX_JOINED_CHARS = 500
_STANDING_AUTH_MAX_LOGICAL_LINES = 8
# Runaway backstop only — the daemon cadence (≤1 signal per reinforcement window)
# plus consume-on-inject is the real rate limit, so this only ever catches a
# pathological consume-delete loop. Set high so a long session is never silenced.
_MAX_STANDING_AUTH_INJECTIONS = 100
_DRY_RUN_STANDING_AUTH_BODY_PREFIX = (
    "would inject standing-auth reminder (dry-run — no real message sent):"
)


def _validate_standing_auth_lines(rendered_lines: object) -> tuple[str | None, str | None]:
    """Validate ``rendered_lines`` from a standing-auth signal; return (joined, error).

    Fail-closed and shape-only, mirroring ``_validate_goal_lines``: the ENTIRE
    fixed machine-origin header must open the payload verbatim (alone, or followed
    by the separator), so a forged signal cannot smuggle arbitrary text past the
    gate. Never interprets the body text.
    """
    if not isinstance(rendered_lines, list) or not rendered_lines:
        return None, "rendered_lines is not a non-empty list"
    if len(rendered_lines) > _STANDING_AUTH_MAX_LOGICAL_LINES:
        return None, f"more than {_STANDING_AUTH_MAX_LOGICAL_LINES} logical lines"
    if not all(isinstance(line, str) for line in rendered_lines):
        return None, "rendered_lines contains a non-string element"
    joined = _STANDING_AUTH_SEPARATOR.join(rendered_lines)
    if joined != _STANDING_AUTH_HEADER_TEXT and not joined.startswith(
        _STANDING_AUTH_HEADER_TEXT + _STANDING_AUTH_SEPARATOR
    ):
        return None, "payload does not open with the verbatim machine-origin header"
    if len(joined) > _STANDING_AUTH_MAX_JOINED_CHARS:
        return None, f"joined line exceeds {_STANDING_AUTH_MAX_JOINED_CHARS} chars"
    for ch in joined:
        if ord(ch) < 0x20 or ord(ch) == 0x7F:
            return None, "control byte in payload"
    return joined, None


def load_standing_auth_signal(
    directory: Path,
    *,
    now: float,
    ttl_seconds: float = _DEFAULT_GOAL_SIGNAL_TTL_SECONDS,
    own_sessions: frozenset[str] | None = None,
) -> tuple[Path | None, str | None, str | None]:
    """Return ``(path, joined_line, reject_reason)`` for a standing-auth signal.

    Same three-shape contract as :func:`load_goal_signal`: a valid fresh in-scope
    signal yields ``(path, joined, None)``; an in-scope fresh signal that FAILS
    the gate yields ``(None, None, reason)``; nothing actionable yields
    ``(None, None, None)``. Foreign-session and stale signals are skipped
    silently. Reuses the goal TTL default — both are idle-consumed signals with
    the same generous window.
    """
    if not directory.is_dir():
        return None, None, None
    for path in sorted(directory.glob(_STANDING_AUTH_SIGNAL_GLOB)):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            return None, None, f"unreadable/malformed standing-auth signal {path.name}"
        if not isinstance(data, dict):
            return None, None, f"standing-auth signal {path.name} is not a JSON object"
        if not _session_in_scope(data.get("session_id"), own_sessions):
            continue
        if (now - _coerce_float(data.get("ts"))) > ttl_seconds:
            continue
        joined, error = _validate_standing_auth_lines(data.get("rendered_lines"))
        if error is not None:
            return None, None, f"standing-auth signal {path.name} rejected: {error}"
        return path, joined, None
    return None, None, None


# ---------------------------------------------------------------------------
# Manual model-switch signal (test trigger / deliberate override).
#
# Mirrors the goal-intent signal: a session-keyed file dropped into the
# shared context-sidecar dir, consumed at the same idle choke point. Unlike
# the daemon-written goal signal, this one is written by the supervisor's OWN
# ``--emit-model-switch`` CLI helper -- there was previously no way to
# trigger a ``/model`` switch on demand to verify the confirm-Enter fix
# end-to-end.
# ---------------------------------------------------------------------------

_MODEL_SWITCH_SIGNAL_SUFFIX = ".model-switch-intent"
_MODEL_SWITCH_SIGNAL_GLOB = f"*{_MODEL_SWITCH_SIGNAL_SUFFIX}"
# Same idle-gate reasoning as the goal signal: plan-execution/test-trigger
# start is usually an idle moment, but the input-box gate can defer, so the
# window is generous. The signal is consumed on injection, so a generous TTL
# cannot re-fire it.
_DEFAULT_MODEL_SWITCH_SIGNAL_TTL_SECONDS = _DEFAULT_GOAL_SIGNAL_TTL_SECONDS


def _canonical_model_family(raw: object) -> str | None:
    """Canonicalise a bare model-family token (e.g. ``mythos`` -> ``fable``).

    Returns None when ``raw`` is not a string or names no known family. Exact
    membership only -- unlike ``_model_family``'s token-containment scan over
    a full model id, the model-switch signal always carries a bare family
    token, never a model id string.
    """
    if not isinstance(raw, str):
        return None
    lowered = raw.strip().lower()
    canonical = _MODEL_FAMILY_CANONICAL.get(lowered, lowered)
    return canonical if canonical in _MODEL_FAMILY_RANKS else None


def load_model_switch_signal(
    directory: Path,
    *,
    now: float,
    ttl_seconds: float = _DEFAULT_MODEL_SWITCH_SIGNAL_TTL_SECONDS,
    own_sessions: frozenset[str] | None = None,
) -> tuple[Path | None, str | None, str | None]:
    """Return ``(path, canonical_family, reject_reason)`` for a switch signal.

    Exactly one of the three shapes comes back: a valid fresh in-scope signal
    yields ``(path, family, None)``; an in-scope fresh signal that FAILS
    validation (unknown family, malformed JSON) yields ``(None, None,
    reason)`` so the caller can log the rejection; no actionable signal at
    all yields ``(None, None, None)``. Foreign-session and stale signals are
    skipped silently, mirroring ``load_goal_signal``.
    """
    if not directory.is_dir():
        return None, None, None
    for path in sorted(directory.glob(_MODEL_SWITCH_SIGNAL_GLOB)):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            return None, None, f"unreadable/malformed model-switch signal {path.name}"
        if not isinstance(data, dict):
            return None, None, f"model-switch signal {path.name} is not a JSON object"
        if not _session_in_scope(data.get("session_id"), own_sessions):
            continue
        if (now - _coerce_float(data.get("ts"))) > ttl_seconds:
            continue
        raw_family = data.get("family")
        family = _canonical_model_family(raw_family)
        if family is None:
            return (
                None,
                None,
                f"model-switch signal {path.name} names unknown family {raw_family!r}",
            )
        return path, family, None
    return None, None, None


def write_model_switch_signal(
    directory: Path,
    *,
    session_id: str,
    family: str,
    now: float,
) -> Path:
    """Atomically write a manual model-switch signal (the CLI test trigger).

    ``family`` is validated and canonicalised (e.g. ``mythos`` -> ``fable``)
    the SAME way :func:`load_model_switch_signal` reads it back, so a round
    trip through this writer always yields a family the reader accepts.
    Raises ``ValueError`` for an unrecognised family -- fail fast on bad CLI
    input rather than writing a signal the reader would only reject later.

    Mirrors ``write_status_message``'s atomic-replace pattern: write to a
    PRIVATE pid-qualified temp file, then ``os.replace`` (``Path.replace``)
    swaps it in, so a concurrent reader (the supervisor's own tick) always
    sees either no file or a COMPLETE one. This is a one-shot CLI helper, not
    a hot-loop writer, so an ``OSError`` propagates to the caller rather than
    being swallowed here.
    """
    canonical = _canonical_model_family(family)
    if canonical is None:
        raise ValueError(
            f"unknown model family {family!r}; expected one of {sorted(_MODEL_FAMILY_RANKS)}"
        )
    directory.mkdir(parents=True, exist_ok=True)
    signal_path = directory / f"{session_id}{_MODEL_SWITCH_SIGNAL_SUFFIX}"
    payload = {"session_id": session_id, "ts": now, "family": canonical}
    tmp_path = directory / f".{signal_path.name}.{os.getpid()}.tmp"
    tmp_path.write_text(json.dumps(payload), encoding="utf-8")
    tmp_path.replace(signal_path)
    return signal_path


# Plan 00316 Task 1.3: subdirectory (under the daemon's untracked dir, the
# same shared root ``sidecar_dir``'s parent resolves to) holding one small
# marker file per session -- the last user-typed /model family + when. Read
# by the daemon's ``downgrade_indicator`` status-line handler so a manual
# choice never shows as a silent downgrade there either.
_MANUAL_MODEL_MARKER_SUBDIR = "manual-model-changes"


def write_manual_model_marker(
    daemon_untracked_dir: Path, *, session_id: str, family: str, now: float
) -> Path:
    """Atomically record this session's last user-typed ``/model <family>``.

    Mirrors ``write_model_switch_signal``'s atomic-replace pattern. Written
    unconditionally on every typed command (no family validation) -- an
    unrecognised family simply never matches any later observed reading, so
    there is nothing to fail closed on here.
    """
    directory = daemon_untracked_dir / _MANUAL_MODEL_MARKER_SUBDIR
    directory.mkdir(parents=True, exist_ok=True)
    marker_path = directory / f"{session_id}.json"
    payload = {"session_id": session_id, "family": family, "ts": now}
    tmp_path = directory / f".{marker_path.name}.{os.getpid()}.tmp"
    tmp_path.write_text(json.dumps(payload), encoding="utf-8")
    tmp_path.replace(marker_path)
    return marker_path


def reap_stale_sidecars(
    directory: Path,
    *,
    now: float,
    ttl_seconds: float = _DEFAULT_REAP_TTL_SECONDS,
    log: DecisionLog | None = None,
) -> list[Path]:
    """Delete dead context-sidecar / compaction-signal files older than the TTL.

    Reaps ``*.json`` sidecars, ``*.compacting`` signals, ``*.goal-intent``
    signals, ``*.standing-auth-intent`` signals and ``*.model-switch-intent``
    signals whose FILE MTIME is older than ``ttl_seconds``. Mtime (not the JSON ``ts``) is used so a
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
    for path in (
        list(directory.glob(_CONTEXT_SIDECAR_GLOB))
        + list(directory.glob(_COMPACTION_SIGNAL_GLOB))
        + list(directory.glob(_GOAL_SIGNAL_GLOB))
        + list(directory.glob(_STANDING_AUTH_SIGNAL_GLOB))
        + list(directory.glob(_MODEL_SWITCH_SIGNAL_GLOB))
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
        # Plan 00183: dry-run fires ONCE per session, once only. A dry-run marker
        # is a no-op on the environment (no real /compact), so context stays red
        # and the machine would re-decide to act every episode -- flooding the
        # session with fake prompts. This latch is set on the first dry-run
        # injection and suppresses every one thereafter for the process lifetime.
        self._dry_run_fired = False
        # Plan 00269: family-specific cap counter for goal injections. Goal
        # signals are consumed on injection, so this only ever matters under a
        # signal storm; per-process lifetime, never reset.
        self._goal_injections = 0
        # Plan 00299: the exact combined /goal text last SUCCESSFULLY
        # injected (None until the first injection). Compared verbatim (not
        # hashed -- the text is already length-capped) against the next
        # tick's candidate to suppress a re-type of an identical combined
        # goal caused by an unrelated ledgered plan's status flip.
        self._last_goal_text: str | None = None
        # Plan 00283: standing-authorisation reinforcement injections this process
        # has fired. Runaway backstop only (see _MAX_STANDING_AUTH_INJECTIONS).
        self._standing_auth_injections = 0
        # Plan 00278: effort-floor tracking. The last observed (session,
        # family) pair, an open downgrade episode ("session:family"), the
        # pending injection key ("session:family:target" — recomputed from
        # every fresh reading), the family cap, and the last-fired key/ts
        # pair backing the stale-reading re-inject cooldown.
        self._last_model_session: str | None = None
        self._last_model_family: str | None = None
        self._downgrade_episode: str | None = None
        self._effort_pending: str | None = None
        self._effort_injections = 0
        self._effort_last_fired_key: str | None = None
        self._effort_last_fired_ts: float | None = None
        # Plan 00278 Task 2b.3: model-restore bookkeeping — the family the
        # downgrade came FROM, when the episode opened, the per-process
        # restore count, and the last restore ts (flip-flop backoff).
        self._downgrade_from_family: str | None = None
        self._downgrade_started_ts: float | None = None
        self._model_restores = 0
        self._model_restore_last_ts: float | None = None
        # Plan 00281: flag-cleaning /compact bookkeeping — per-process count
        # (round-tripped) and last-fire ts (process-local backoff).
        self._flag_compactions = 0
        self._flag_compact_last_ts: float | None = None
        # Plan 00278 continuation: the COUPLED effort correction a /model
        # switch always owes on the next injectable tick (format
        # "session:family:target"). Armed by the HOST after ANY successful
        # /model injection -- manual test-trigger AND auto-restore alike --
        # so the correction never depends on a downgrade episode being open
        # or a later sidecar reading confirming the switch landed.
        self._coupled_effort_pending: str | None = None
        # Audit items owed to the chat: one entry per silent injection
        # (/model, /effort) since the last flush. Flushed as ONE visible
        # bot-prefixed message on the next injectable tick; bounded FIFO.
        self._audit_pending: list[str] = []
        # Plan 00297: DROP ANCHOR invariant state -- whether it is currently
        # active (a fresh, non-stale reading last showed fable above low
        # effort), when it was first observed, the last injection attempt
        # timestamp (its own retry cooldown, separate from the floor
        # mechanism's), and how many attempts have been made without a
        # verified read-back correcting it.
        self._anchor_active: bool = False
        self._anchor_started_ts: float | None = None
        self._anchor_last_injected_ts: float | None = None
        self._anchor_attempts: int = 0
        # Plan 00297 follow-up: last wall-clock time the escalation ESC fired
        # (its own cooldown, separate from `_anchor_last_injected_ts`). Reset
        # to None whenever the anchor clears, so a fresh violation episode is
        # never throttled by a previous episode's ESC cooldown.
        self._anchor_esc_last_sent_ts: float | None = None
        # Plan 00316: the last user-TYPED `/model <family>` command (canonical
        # family + when it was typed) -- an observed change matching this
        # within `_MANUAL_MODEL_WINDOW_SECONDS` is classified MANUAL, not a
        # silent downgrade, so it is never fought by the auto-restore. A
        # fresh manual command always overwrites the previous one (rapid
        # successive manual changes each count on their own).
        self._manual_model_family: str | None = None
        self._manual_model_ts: float | None = None
        # Shared-marker debt: a typed /model whose daemon-facing marker file
        # has not been written yet because no tick so far could name the
        # session (no fresh reading, no tracked session). decide_once retries
        # every tick until a session id exists, then clears it.
        self._manual_marker_pending: str | None = None
        # Consume-once note set by note_model_reading() when a manual match
        # suppressed what would otherwise have opened a downgrade episode --
        # surfaced to decision.log by decide_once via take_manual_model_note().
        self._manual_model_note: str | None = None
        # Plan 00316 Task 2.1: the last user-TYPED `/effort <level>` -- a
        # latch (not time-windowed): it wins over the per-model default/
        # coupled effort until the user manually changes model again or
        # manually re-sets effort.
        self._manual_effort_active: str | None = None

    @property
    def effort_pending(self) -> str | None:
        """Pending effort-restore episode key, or None (Plan 00278)."""
        return self._effort_pending

    @property
    def effort_injections(self) -> int:
        """How many effort injections this process has fired (Plan 00278)."""
        return self._effort_injections

    def mark_effort_injection(self, now_wall: float | None = None) -> None:
        """Count a fired effort restore and close the pending episode.

        Called by the HOST only after a SUCCESSFUL injection — a failed PTY
        write keeps the pending episode so the next tick retries. The fired
        key + timestamp start the stale-reading re-inject cooldown
        (Plan 00278).
        """
        self._effort_injections += 1
        self._effort_last_fired_key = self._effort_pending
        self._effort_last_fired_ts = time.time() if now_wall is None else now_wall
        self._effort_pending = None

    def mark_model_restore(self, now_wall: float | None = None) -> None:
        """Count a fired auto-restore /model flip-back for cap/backoff purposes.

        Called by the HOST only after a SUCCESSFUL AUTO-RESTORE injection
        (Plan 00278 Task 2b.3) -- never for the manual test-trigger switch,
        which has its own signal-consumption lifecycle and must not eat into
        this cap/backoff budget. The timestamp starts the flip-flop backoff
        (see ``model_restore_due``). The post-flip effort correction is
        handled unconditionally by the coupled-effort mechanism, not by this
        method -- see ``arm_coupled_effort``.
        """
        self._model_restores += 1
        self._model_restore_last_ts = time.time() if now_wall is None else now_wall

    def model_restore_due(self, now_wall: float) -> str | None:
        """Return the family to restore to when the flip-back is due, else None.

        Due means: auto-restore enabled (delay >= 0; negative/"off"
        disables), a downgrade episode is open, any EXTRA quiet delay has
        elapsed since it opened (default 0 — the injection choke point's
        idle + empty-input gate already guarantees the flagged TURN is over,
        which is the real recovery condition), the lifetime cap is not
        reached, and no restore fired within the flip-flop backoff window.
        """
        delay = self._policy.model_restore_delay_seconds
        if (
            delay < 0
            or self._downgrade_episode is None
            or self._downgrade_from_family is None
            or self._downgrade_started_ts is None
        ):
            return None
        if now_wall - self._downgrade_started_ts < delay:
            return None
        if self._model_restores >= _MAX_MODEL_RESTORES:
            return None
        if (
            self._model_restore_last_ts is not None
            and now_wall - self._model_restore_last_ts < _MODEL_RESTORE_BACKOFF_SECONDS
        ):
            return None
        return self._downgrade_from_family

    def mark_flag_compaction(self, now_wall: float | None = None) -> None:
        """Count a fired flag-cleaning /compact for cap/backoff purposes.

        Called by the HOST only after a SUCCESSFUL injection (Plan 00281); a
        failed PTY write keeps the budget so a later tick can retry. The
        timestamp starts the flip-flop backoff (see ``flag_compact_due``).
        """
        self._flag_compactions += 1
        self._flag_compact_last_ts = time.time() if now_wall is None else now_wall

    def flag_compact_due(self, now_wall: float) -> bool:
        """True when a flag-cleaning /compact should fire on this tick (Plan 00281).

        Fires only on a FLIP-FLOP: the feature is enabled, a downgrade episode
        is open, AND at least one model auto-restore has already happened this
        process — so a prior restore was undone by the classifier re-firing and
        restoring the model alone cannot win. Capped per process and backed
        off, because /compact rewrites context and must never storm.
        """
        if not self._policy.flag_compact_enabled:
            return False
        if self._downgrade_episode is None or self._model_restores < 1:
            return False
        if self._flag_compactions >= _MAX_FLAG_COMPACTIONS:
            return False
        if (
            self._flag_compact_last_ts is not None
            and now_wall - self._flag_compact_last_ts < _FLAG_COMPACT_BACKOFF_SECONDS
        ):
            return False
        return True

    def _coupled_effort_target(self, family: str) -> str:
        """Return the mandatory post-/model-switch effort target for ``family``.

        Every ``/model <family>`` injection is followed, unconditionally, by
        an ``/effort`` injection to this target on the next injectable tick
        (the "effort switch MUST follow model switch" invariant) -- entirely
        independent of whether a downgrade episode happens to be open or a
        later sidecar reading ever confirms the switch landed. The
        TOP-ranked family (fable) is a SANCTIONED LOWERING to its configured
        floor -- the fix for the live defect where an opus->fable switch
        left effort at xhigh and burned account allowance. Any other
        destination -- a downgrade, or a partial restore that has not yet
        reached the top family -- targets ``_DOWNGRADE_TARGET_EFFORT``
        (xhigh) so a still-degraded model gets maximum compensating effort.
        This BYPASSES the raise-only invariant that governs the floor-based
        ``_effort_pending`` family: lowering fable to its floor is the
        entire point.

        DROP ANCHOR clamp (Plan 00297): the top-ranked family's configured
        floor is clamped to ``_ANCHOR_TARGET_EFFORT`` when it would resolve
        ABOVE that ceiling -- e.g. a ``CCY_MIN_EFFORT_LEVELS`` misconfigured
        with an Opus-era ``fable=xhigh`` override. Fable-above-low is banned
        unconditionally by the anchor invariant, not merely by the default
        floor value, so this path must never hand out anything higher.
        """
        if _family_rank(family) == _TOP_FAMILY_RANK:
            configured = self._policy.min_effort_levels.get(
                family, _DEFAULT_MIN_EFFORT_LEVELS.get(family, _DOWNGRADE_TARGET_EFFORT)
            )
            if (
                configured not in _EFFORT_RANKS
                or _EFFORT_RANKS[configured] > _EFFORT_RANKS[_ANCHOR_TARGET_EFFORT]
            ):
                return _ANCHOR_TARGET_EFFORT
            return configured
        return _DOWNGRADE_TARGET_EFFORT

    @property
    def coupled_effort_pending(self) -> str | None:
        """Pending coupled effort-injection key, or None (Plan 00278 cont.)."""
        return self._coupled_effort_pending

    def arm_coupled_effort(self, *, session: str, family: str) -> None:
        """Arm the mandatory post-/model-switch effort correction.

        Called by the HOST after ANY successful ``/model <family>``
        injection -- both the manual test-trigger signal and the
        auto-restore flip-back -- so the very next injectable tick fires
        ``/effort <target>`` unconditionally, guaranteeing the invariant
        regardless of downgrade-episode state or sidecar timing. A blank
        ``session`` or ``family`` is a no-op (defensive: decide_once only
        ever calls this with values it has just resolved for a real switch).

        Plan 00316 Task 2.1 (owner clarification): precedence is
        TIME-ORDERED, not absolute -- EVERY model change (manual or
        auto-restore) starts a fresh "model spell" and re-applies ITS
        default effort, even over a manual /effort set under the PREVIOUS
        spell. A manual /effort only stays sticky for the CURRENT spell,
        beating the coupling until the NEXT model change. So every call
        here (which only ever happens right after a real /model switch)
        clears any earlier manual-effort latch before arming the new
        default -- it never skips arming because one was active.
        """
        if not session or not family:
            return
        self._manual_effort_active = None
        target = self._coupled_effort_target(family)
        self._coupled_effort_pending = f"{session}:{family}:{target}"

    def mark_coupled_effort_injection(self) -> None:
        """Close the pending coupled-effort episode after a SUCCESSFUL injection.

        Called by the HOST only after a successful PTY write -- a failed
        write keeps the pending episode so the next tick retries.
        """
        self._coupled_effort_pending = None

    @property
    def audit_pending(self) -> tuple[str, ...]:
        """Audit items owed to the chat since the last flush (may be empty)."""
        return tuple(self._audit_pending)

    def arm_audit(self, item: str) -> None:
        """Queue one silent-injection audit item for the next banner flush.

        Called at DECISION time by the armed (non-dry-run) ``/model`` and
        ``/effort`` branches in ``decide_once`` — worker-side, so the whole
        audit loop deploys via worker hot-reload with no host restart (an
        older host's merge-by-key ``import_state`` never clobbers the
        worker's backlog; it merely doesn't carry it). Bounded FIFO: the
        oldest item is dropped once ``_MAX_AUDIT_ITEMS`` is reached, because
        an unflushable audit backlog must never grow the machine state
        without limit.
        """
        if not item:
            return
        self._audit_pending.append(item)
        if len(self._audit_pending) > _MAX_AUDIT_ITEMS:
            del self._audit_pending[0]

    def mark_audit_injection(self) -> None:
        """Clear the audit backlog once the banner for it has been posted.

        Called by ``decide_once`` at DECISION time (Plan 00318): the flush is
        a file write, not a PTY write, so there is no host-side success to
        wait for. A failed banner write therefore drops that notice rather
        than retrying it -- decision.log keeps the durable record either way.
        """
        self._audit_pending = []

    # ── DROP ANCHOR (Plan 00297) ─────────────────────────────────────────

    @property
    def anchor_active(self) -> bool:
        """True while the DROP ANCHOR invariant is observed violated.

        Cleared ONLY by a later fresh reading showing verified read-back
        (fable at low effort, or the family moving off fable entirely) --
        never by an injection attempt succeeding on the wire, which is
        exactly the inject-and-assume defect this closes.
        """
        return self._anchor_active

    @property
    def anchor_attempts(self) -> int:
        """How many `/effort low` attempts the anchor has fired this episode."""
        return self._anchor_attempts

    @property
    def anchor_started_ts(self) -> float | None:
        """Wall-clock time the current anchor episode was first observed."""
        return self._anchor_started_ts

    def _evaluate_anchor(self, *, family: str, effort: str | None, now_wall: float) -> None:
        """Continuously re-check `model == fable implies effort == low`.

        An UNKNOWN effort reading (older sidecar, or a race before the
        first render) never counts as evidence either way -- it neither
        engages nor clears an anchor, because a guess must never stand in
        for a verified read-back in either direction.
        """
        if effort is None or effort not in _EFFORT_RANKS:
            return
        violated = (
            family == "fable" and _EFFORT_RANKS[effort] > _EFFORT_RANKS[_ANCHOR_TARGET_EFFORT]
        )
        if violated:
            if not self._anchor_active:
                self._anchor_active = True
                self._anchor_started_ts = now_wall
                self._anchor_attempts = 0
            return
        if self._anchor_active:
            # Invariant holds again -- either fable is verified at low, or
            # the model has moved off fable entirely (the invariant no
            # longer applies to it).
            self._anchor_active = False
            self._anchor_started_ts = None
            self._anchor_last_injected_ts = None
            self._anchor_attempts = 0
            self._anchor_esc_last_sent_ts = None

    def anchor_injection_due(self, now_wall: float) -> bool:
        """True when the anchor should (re)inject `/effort low` this tick.

        Its OWN cooldown (`_ANCHOR_RETRY_COOLDOWN_SECONDS`) -- deliberately
        NOT the floor mechanism's `_EFFORT_REINJECT_COOLDOWN_SECONDS` or
        `_MAX_EFFORT_INJECTIONS` cap, both of which an unverified anchor
        violation must bypass so it keeps retrying rather than going quiet
        because an unrelated budget was spent.
        """
        if not self._anchor_active:
            return False
        if self._anchor_last_injected_ts is None:
            return True
        return now_wall - self._anchor_last_injected_ts >= _ANCHOR_RETRY_COOLDOWN_SECONDS

    def mark_anchor_injection(self, now_wall: float) -> None:
        """Record an anchor `/effort low` attempt (HOST, successful PTY write only).

        Deliberately does NOT clear `anchor_active` -- a PTY write
        succeeding proves nothing about whether the session actually
        applied it (the live incident: swallowed by a busy session). Only
        `_evaluate_anchor` observing a later verified read-back clears it.
        """
        self._anchor_last_injected_ts = now_wall
        self._anchor_attempts += 1

    def anchor_escalated_at(self, now_wall: float) -> bool:
        """True once the anchor has exceeded its attempt or time bound.

        Escalation does not change injection behaviour (the anchor keeps
        retrying either way) -- it is purely the signal the HOST uses to
        post a loud, rate-limited owner-facing alert (Plan 00297 Task 2.2).
        """
        if not self._anchor_active:
            return False
        if self._anchor_attempts >= _ANCHOR_MAX_ATTEMPTS:
            return True
        return (
            self._anchor_started_ts is not None
            and now_wall - self._anchor_started_ts >= _ANCHOR_ESCALATION_BOUND_SECONDS
        )

    def anchor_esc_due(self, now_wall: float) -> bool:
        """True when an ESCALATED anchor should send an ESC to flush the turn.

        Requires the anchor to be both active AND escalated -- ESC is a
        stronger interrupt than the plain `/effort low` retry, reserved for
        the case that retry alone has not been enough. Gated by its OWN
        cooldown (`_ANCHOR_ESC_COOLDOWN_SECONDS`), independent of
        `anchor_injection_due`'s cooldown, so ESC never fires every retry
        tick. Callers must ALSO check that no compaction is in flight --
        compaction is uninterruptible, and this method has no visibility
        into the sidecar reading needed to know that.
        """
        if not self._anchor_active or not self.anchor_escalated_at(now_wall):
            return False
        if self._anchor_esc_last_sent_ts is None:
            return True
        return now_wall - self._anchor_esc_last_sent_ts >= _ANCHOR_ESC_COOLDOWN_SECONDS

    def mark_anchor_esc(self, now_wall: float) -> None:
        """Record that the anchor's escalation ESC fired this tick (HOST-side)."""
        self._anchor_esc_last_sent_ts = now_wall

    @property
    def last_model_session(self) -> str | None:
        """The most recently observed foreground session id, or None (Plan 00278)."""
        return self._last_model_session

    def note_manual_model_command(self, family: str, *, now_wall: float) -> None:
        """Record a user-TYPED ``/model <family>`` command (Plan 00316).

        Called by decide_once for every tick that observed one (via
        ``TickFacts.human_model_command``, sourced from the PTY host's input
        path). A fresh command always overwrites the previous one -- rapid
        successive manual changes each count on their own, never merged.
        Also clears any manual effort latch: a deliberate model change is a
        fresh context the old manual effort no longer speaks to (Task 2.1).
        """
        # CANONICALISE on the way in. `family` here is the RAW argument the
        # human typed -- "Opus", "opusplan", "claude-opus-4-8", "mythos" -- and
        # every later comparison is against a canonical family. Storing it raw
        # made `/model Opus` silently fail to latch, so the auto-restore
        # overrode the human's own choice. Falls back to the lowered raw string
        # for an unrecognised family: it simply never matches a reading, which
        # is the same harmless outcome as before.
        canonical = _model_family(family) or family.strip().lower()
        self._manual_model_family = canonical
        self._manual_model_ts = now_wall
        self._manual_marker_pending = canonical
        self._manual_effort_active = None

    @property
    def manual_marker_pending(self) -> str | None:
        """Family of a typed /model whose shared marker is not yet on disk."""
        return self._manual_marker_pending

    def clear_manual_marker_pending(self) -> None:
        """The shared marker for the pending typed /model has been written."""
        self._manual_marker_pending = None

    def note_manual_effort_command(self, level: str, *, now_wall: float) -> None:
        """Record a user-TYPED ``/effort <level>`` command (Plan 00316 Task 2.1).

        A latch, not time-windowed: it wins over the per-model default and
        the post-switch coupled effort for the REST OF THE CURRENT model
        spell -- until the user manually changes model again
        (``note_manual_model_command``, which starts a fresh spell and
        re-applies its own default via ``arm_coupled_effort``) or manually
        re-sets effort. Also cancels any coupled-effort correction already
        armed for THIS spell (from an earlier ``arm_coupled_effort`` call)
        that has not yet been injected -- the human's choice, typed after
        that default was queued, overrides the queued default outright.
        """
        del now_wall  # kept for signature symmetry with the model counterpart
        self._manual_effort_active = level
        self._coupled_effort_pending = None

    def _manual_model_matches(self, family: str, now_wall: float) -> bool:
        """True when ``family`` matches a recent user-typed ``/model`` command."""
        return (
            self._manual_model_family == family
            and self._manual_model_ts is not None
            and now_wall - self._manual_model_ts <= _MANUAL_MODEL_WINDOW_SECONDS
        )

    def take_manual_model_note(self) -> str | None:
        """Return, once, the reason a manual match suppressed a downgrade episode.

        Edge-triggered and consume-once, mirroring ``take_compact_submitted``,
        so decide_once logs it exactly once per manual match.
        """
        note = self._manual_model_note
        self._manual_model_note = None
        return note

    def note_model_reading(self, reading: SidecarReading, *, now_wall: float) -> None:
        """Track the foreground model; recompute the floor-based effort episode.

        Two triggers share one pending episode (Plan 00278):

        - a ranked DOWNGRADE for the same session raises the target to
          ``_DOWNGRADE_TARGET_EFFORT`` until the family recovers;
        - otherwise the live effort sitting BELOW the configured per-model
          minimum targets that minimum.

        The pending key is recomputed from every fresh reading, so it clears
        itself when the effort rises, the family recovers, or the session
        changes. A just-fired episode is suppressed for the re-inject
        cooldown, because the sidecar reports the old effort until the next
        status render. Unknown families and session-less synthetic readings
        are ignored entirely.

        The post-/model-switch effort correction is NOT decided here any
        more (Plan 00278 continuation): it used to fire only once THIS
        method observed the switch land, which never happened for a manual
        override with no downgrade episode open. That correction is now the
        unconditional ``_coupled_effort_pending`` mechanism, armed by the
        HOST straight off a successful injection -- see
        ``arm_coupled_effort``. This method still closes a recovered
        downgrade episode (so ``model_restore_due`` stops considering a
        restore "due" once the family is back); it just no longer tries to
        infer an effort reset from that recovery.
        """
        family = _model_family(reading.model_id)
        session = reading.session_id
        if not session or family is None:
            return
        # Plan 00297: DROP ANCHOR is evaluated on every fresh, non-stale
        # reading, INDEPENDENT of the raise-only floor/downgrade logic below
        # -- that logic cannot detect "fable already running above its
        # floor" by construction. Runs before the floor bookkeeping so an
        # anchor engagement/clear is never skipped by an early return below.
        self._evaluate_anchor(family=family, effort=reading.effort, now_wall=now_wall)
        prev_session = self._last_model_session
        prev_family = self._last_model_family
        self._last_model_session = session
        self._last_model_family = family
        if self._downgrade_episode is not None:
            ep_session, _, ep_family = self._downgrade_episode.partition(":")
            if session != ep_session or _family_rank(family) > _family_rank(ep_family):
                self._downgrade_episode = None
                self._downgrade_from_family = None
                self._downgrade_started_ts = None
        manual_match = self._manual_model_matches(family, now_wall)
        if (
            prev_session == session
            and prev_family is not None
            and _family_rank(family) < _family_rank(prev_family)
        ):
            if manual_match:
                # Plan 00316: a rank drop matching a recently-typed /model
                # command is the human's OWN deliberate choice, not a silent
                # substitution -- never open a downgrade episode for it (no
                # auto-restore, no forced xhigh floor). A stale episode left
                # open from an earlier silent drop on this session is cleared
                # too, since the manual choice must win outright.
                if self._downgrade_episode is not None:
                    ep_session, _, _ = self._downgrade_episode.partition(":")
                    if ep_session == session:
                        self._downgrade_episode = None
                        self._downgrade_from_family = None
                        self._downgrade_started_ts = None
                self._manual_model_note = f"manual change ({family}) — no restore"
            else:
                if self._downgrade_episode is None:
                    # A fresh episode: remember where we fell FROM and when, for
                    # the delayed /model flip-back (Task 2b.3). A further drop
                    # inside an open episode keeps the original from/started.
                    self._downgrade_from_family = prev_family
                    self._downgrade_started_ts = now_wall
                self._downgrade_episode = f"{session}:{family}"
        if manual_match:
            # Latch consumed: the typed choice has been observed landing. A
            # LATER family change with nothing newly typed is a silent
            # substitution again -- the spent latch must not re-classify it.
            self._manual_model_family = None
            self._manual_model_ts = None
        if self._manual_effort_active is not None:
            # Plan 00316 Task 2.1: a manual /effort always wins -- neither the
            # downgrade-episode xhigh floor nor the per-model default fires
            # while it is active.
            target: str | None = None
        elif self._downgrade_episode is not None:
            target = _DOWNGRADE_TARGET_EFFORT
        else:
            target = self._policy.min_effort_levels.get(family)
        current = reading.effort
        below = target is not None and (
            # An unknown effort is assumed low ONLY inside a downgrade
            # episode (the restore is the point); outside one it means an
            # older Claude Code without the live field — do nothing.
            (current is None and self._downgrade_episode is not None)
            or (
                current is not None
                and current in _EFFORT_RANKS
                and _EFFORT_RANKS[current] < _EFFORT_RANKS[target]
            )
        )
        if not below or target is None:
            self._effort_pending = None
            return
        key = f"{session}:{family}:{target}"
        if (
            self._effort_last_fired_key == key
            and self._effort_last_fired_ts is not None
            and now_wall - self._effort_last_fired_ts < _EFFORT_REINJECT_COOLDOWN_SECONDS
        ):
            self._effort_pending = None
            return
        self._effort_pending = key

    @property
    def goal_injections(self) -> int:
        """How many goal injections this process has fired (Plan 00269)."""
        return self._goal_injections

    def mark_goal_injection(self, goal_line: str | None = None) -> None:
        """Count one goal injection against the family cap (Plan 00269).

        ``goal_line`` (Plan 00299) records the injected text as the thrash
        guard for the NEXT tick's candidate; omitted/None leaves the guard
        unchanged (legacy callers, and tests exercising the cap alone).
        """
        self._goal_injections += 1
        if goal_line is not None:
            self._last_goal_text = goal_line

    @property
    def last_goal_text(self) -> str | None:
        """The combined /goal text last successfully injected (Plan 00299)."""
        return self._last_goal_text

    @property
    def standing_auth_injections(self) -> int:
        """How many standing-auth reinforcements this process has fired (Plan 00283)."""
        return self._standing_auth_injections

    def mark_standing_auth_injection(self) -> None:
        """Count one standing-auth reinforcement against the runaway backstop (Plan 00283)."""
        self._standing_auth_injections += 1

    @property
    def dry_run_fired(self) -> bool:
        """True once a dry-run marker has been injected this session (Plan 00183)."""
        return self._dry_run_fired

    def mark_dry_run_fired(self) -> None:
        """Latch the dry-run once-only fuse for the process lifetime (Plan 00183)."""
        self._dry_run_fired = True

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
            "dry_run_fired": self._dry_run_fired,
            "goal_injections": self._goal_injections,
            "last_goal_text": self._last_goal_text,
            "standing_auth_injections": self._standing_auth_injections,
            "last_model_session": self._last_model_session,
            "last_model_family": self._last_model_family,
            "downgrade_episode": self._downgrade_episode,
            "effort_pending": self._effort_pending,
            "effort_injections": self._effort_injections,
            "effort_last_fired_key": self._effort_last_fired_key,
            "effort_last_fired_ts": self._effort_last_fired_ts,
            "downgrade_from_family": self._downgrade_from_family,
            "downgrade_started_ts": self._downgrade_started_ts,
            "model_restores": self._model_restores,
            "model_restore_last_ts": self._model_restore_last_ts,
            "flag_compactions": self._flag_compactions,
            "coupled_effort_pending": self._coupled_effort_pending,
            "audit_pending": list(self._audit_pending),
            "anchor_active": self._anchor_active,
            "anchor_started_ts": self._anchor_started_ts,
            "anchor_last_injected_ts": self._anchor_last_injected_ts,
            "anchor_attempts": self._anchor_attempts,
            "anchor_esc_last_sent_ts": self._anchor_esc_last_sent_ts,
            "manual_model_family": self._manual_model_family,
            "manual_model_ts": self._manual_model_ts,
            "manual_model_note": self._manual_model_note,
            "manual_marker_pending": self._manual_marker_pending,
            "manual_effort_active": self._manual_effort_active,
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
        if "dry_run_fired" in state:
            self._dry_run_fired = bool(state["dry_run_fired"])
        if "goal_injections" in state:
            self._goal_injections = _coerce_int(state["goal_injections"])
        if "last_goal_text" in state:
            raw = state["last_goal_text"]
            self._last_goal_text = None if raw is None else str(raw)
        if "standing_auth_injections" in state:
            self._standing_auth_injections = _coerce_int(state["standing_auth_injections"])
        if "last_model_session" in state:
            raw = state["last_model_session"]
            self._last_model_session = None if raw is None else str(raw)
        if "last_model_family" in state:
            raw = state["last_model_family"]
            self._last_model_family = None if raw is None else str(raw)
        if "downgrade_episode" in state:
            raw = state["downgrade_episode"]
            self._downgrade_episode = None if raw is None else str(raw)
        if "effort_pending" in state:
            raw = state["effort_pending"]
            self._effort_pending = None if raw is None else str(raw)
        if "effort_injections" in state:
            self._effort_injections = _coerce_int(state["effort_injections"])
        if "effort_last_fired_key" in state:
            raw = state["effort_last_fired_key"]
            self._effort_last_fired_key = None if raw is None else str(raw)
        if "effort_last_fired_ts" in state:
            raw = state["effort_last_fired_ts"]
            self._effort_last_fired_ts = None if raw is None else _coerce_float(raw)
        if "downgrade_from_family" in state:
            raw = state["downgrade_from_family"]
            self._downgrade_from_family = None if raw is None else str(raw)
        if "downgrade_started_ts" in state:
            raw = state["downgrade_started_ts"]
            self._downgrade_started_ts = None if raw is None else _coerce_float(raw)
        if "model_restores" in state:
            self._model_restores = _coerce_int(state["model_restores"])
        if "model_restore_last_ts" in state:
            raw = state["model_restore_last_ts"]
            self._model_restore_last_ts = None if raw is None else _coerce_float(raw)
        if "flag_compactions" in state:
            self._flag_compactions = _coerce_int(state["flag_compactions"])
        if "coupled_effort_pending" in state:
            raw = state["coupled_effort_pending"]
            self._coupled_effort_pending = None if raw is None else str(raw)
        if "audit_pending" in state:
            raw_items = state["audit_pending"]
            if isinstance(raw_items, list):
                self._audit_pending = [str(item) for item in raw_items[:_MAX_AUDIT_ITEMS]]
        if "anchor_active" in state:
            self._anchor_active = bool(state["anchor_active"])
        if "anchor_started_ts" in state:
            raw = state["anchor_started_ts"]
            self._anchor_started_ts = None if raw is None else _coerce_float(raw)
        if "anchor_last_injected_ts" in state:
            raw = state["anchor_last_injected_ts"]
            self._anchor_last_injected_ts = None if raw is None else _coerce_float(raw)
        if "anchor_attempts" in state:
            self._anchor_attempts = _coerce_int(state["anchor_attempts"])
        if "anchor_esc_last_sent_ts" in state:
            raw = state["anchor_esc_last_sent_ts"]
            self._anchor_esc_last_sent_ts = None if raw is None else _coerce_float(raw)
        if "manual_model_family" in state:
            raw = state["manual_model_family"]
            self._manual_model_family = None if raw is None else str(raw)
        if "manual_model_ts" in state:
            raw = state["manual_model_ts"]
            self._manual_model_ts = None if raw is None else _coerce_float(raw)
        if "manual_model_note" in state:
            raw = state["manual_model_note"]
            self._manual_model_note = None if raw is None else str(raw)
        if "manual_marker_pending" in state:
            raw = state["manual_marker_pending"]
            self._manual_marker_pending = None if raw is None else str(raw)
        if "manual_effort_active" in state:
            raw = state["manual_effort_active"]
            self._manual_effort_active = None if raw is None else str(raw)

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
# Plan 00281: the instruction body for a flag-cleaning /compact. Phrased WITHOUT
# the trigger vocabulary itself (naming those categories would re-seed the very
# terms the compaction exists to clear) — it asks for a high-level summary that
# omits low-level technical specifics, so the compacted context stops
# re-triggering the content classifier and the model-restore can stick.
_FLAG_COMPACT_BODY = (
    "Summarise the work so far at a HIGH LEVEL. Where the conversation touched "
    "sensitive or security-adjacent material, keep only what was done and the "
    "outcome — leave out the low-level technical specifics, sample text, and "
    "sensitive strings, which are preserved in git and the plan docs. This keeps "
    "the continuing context from re-triggering content classifiers. Then resume "
    "and continue the work in progress."
)
_DRY_RUN_FLAG_COMPACT_BODY = (
    "flag-cleaning compact fired (dry-run — not a real /compact, not human input)"
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
    # Built tz-AWARE and then converted to local with `.astimezone()`, rather
    # than via the naive `datetime.now()` / `fromtimestamp(now_wall)`. The
    # rendered string is identical -- `.astimezone()` with no argument converts
    # to the system local zone, which is what the naive calls already returned
    # -- so this states the "local, deliberately" intent above in code instead
    # of only in prose. It also keeps the file clean under ruff's DEFAULT rule
    # set (DTZ005/DTZ006 joined those defaults in ruff 0.16), which the client
    # boundary document promises of every deployed asset.
    moment = (
        datetime.now(UTC).astimezone()
        if now_wall is None
        else datetime.fromtimestamp(now_wall, UTC).astimezone()
    )
    return f"{_BOT_PREFIX} {moment.strftime(_BOT_PREFIX_TIME_FORMAT)}]"


# ── Injection iconography (RULESET — single source of truth) ─────────────────
# Every supervisor chat injection carries the invariant provenance marker
# `🤖 [ccy-supervisor <local-time>]` (machine origin + WHEN). That marker is
# NEVER split or removed: skill-scan (`EXCLUDE_CONTENT_MARKERS`) and the
# guardrail tests recognise supervisor traffic by the `🤖 [ccy-supervisor`
# substring, so any classifying glyph is added AROUND it, never inside it.
#
# A leading CATEGORY banner lets a human classify an injection at a glance when
# scrolling back through a long session — this is what makes an audit comment
# easy to SPOT versus a goal or a resume nudge:
#   🧾  audit  — a record of silent actions ALREADY TAKEN on your behalf
# Within an audit line each injected command is prefixed with a per-ACTION
# glyph, so the specific silent actions are scannable without reading the whole
# sentence:
#   ⚙️  /effort …   (effort level changed)
#   ♻️  /model …    (model restored / switched)
#   🧽  /compact …  (flag-cleaning compaction, Plan 00281)
# New action families extend `_AUDIT_ACTION_GLYPHS`; unknown items fall back to
# the neutral bullet. Keep this block and the table in sync.
_AUDIT_BANNER_GLYPH = "🧾"
_AUDIT_ACTION_EFFORT_GLYPH = "⚙️"
_AUDIT_ACTION_MODEL_GLYPH = "♻️"
_AUDIT_ACTION_COMPACT_GLYPH = "🧽"
_AUDIT_ACTION_DEFAULT_GLYPH = "•"
# (command-prefix, glyph) pairs, longest-prefix-first is unnecessary here since
# the commands share no prefix; a plain first-match scan suffices.
_AUDIT_ACTION_GLYPHS: tuple[tuple[str, str], ...] = (
    ("/effort", _AUDIT_ACTION_EFFORT_GLYPH),
    ("/model", _AUDIT_ACTION_MODEL_GLYPH),
    ("/compact", _AUDIT_ACTION_COMPACT_GLYPH),
)
# Plan 00318: the audit trail is a transient STATUS-LINE banner, not a chat
# injection. A banner is read at a glance mid-render, so it lives longer than a
# keystroke hint (_STATUS_MESSAGE_TTL_SECONDS) and carries a countdown, and it
# shows only WHAT was done — the per-item reason and the full record stay in
# decision.log, which the status line has no room for and no need to repeat.
_AUDIT_BANNER_TTL_SECONDS = 30.0
_AUDIT_BANNER_MAX_ITEMS = 3
# Separator between an audit item's command and its parenthesised reason; the
# banner keeps only the part before it.
_AUDIT_ITEM_REASON_SEPARATOR = " ("


def _audit_action_glyph(item: str) -> str:
    """Return the per-action glyph for one pending audit item.

    Keys on the leading slash-command token so the caller does not have to
    tag items at arm time — the glyph is derived from the command text itself.
    """
    stripped = item.lstrip()
    for prefix, glyph in _AUDIT_ACTION_GLYPHS:
        if stripped.startswith(prefix):
            return glyph
    return _AUDIT_ACTION_DEFAULT_GLYPH


def _format_audit_banner(items: tuple[str, ...]) -> str:
    """Compose the transient STATUS-LINE form of the audit trail (Plan 00318).

    Same iconography as the chat form, stripped to what a status line can hold:
    the banner glyph and each action's glyph + command, with the parenthesised
    reason dropped (it is in decision.log, and repeating it here would push the
    interesting part off the end of the line). A backlog longer than
    ``_AUDIT_BANNER_MAX_ITEMS`` is truncated with a remainder count rather than
    silently losing the tail.
    """
    shown = items[:_AUDIT_BANNER_MAX_ITEMS]
    labelled = "; ".join(
        f"{_audit_action_glyph(item)} {item.split(_AUDIT_ITEM_REASON_SEPARATOR)[0].strip()}"
        for item in shown
    )
    remainder = len(items) - len(shown)
    if remainder > 0:
        labelled = f"{labelled}; +{remainder} more"
    return f"{_AUDIT_BANNER_GLYPH} {labelled}"


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
# A `/model <family>` switch shows a CONFIRMATION dialog after the command
# line is submitted; a second, standalone Enter is needed to complete it, and
# the dialog needs a moment to render before that keystroke lands. Kept
# separate from _SUBMIT_DELAY_SECONDS (a different UI element, a different
# render latency) rather than reusing the same constant for two purposes.
_MODEL_CONFIRM_DELAY_SECONDS = 0.4

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
    confirm_enters: int = 0,
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

    ``confirm_enters`` sends that many ADDITIONAL standalone Enter keystrokes
    after the normal submit, each preceded by ``_MODEL_CONFIRM_DELAY_SECONDS``
    -- a ``/model <family>`` switch shows a confirmation dialog that needs a
    SECOND Enter to complete. Every other decision passes 0, making this loop
    a no-op for them; it is also skipped entirely when ``submit=False`` (an
    interrupt keypress is never followed by a confirmation dialog).
    """
    master_writer(payload.encode("utf-8"))
    if not submit:
        return
    sleep(_SUBMIT_DELAY_SECONDS)
    master_writer(_INJECT_SUBMIT.encode("utf-8"))
    for _ in range(confirm_enters):
        sleep(_MODEL_CONFIRM_DELAY_SECONDS)
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
    goal_signal_ttl_seconds: float = _DEFAULT_GOAL_SIGNAL_TTL_SECONDS,
    model_confirm_enters: int = _DEFAULT_MODEL_CONFIRM_ENTERS,
    effort_confirm_enters: int = _DEFAULT_EFFORT_CONFIRM_ENTERS,
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
    # Plan 00316: record a user-typed /model or /effort command BEFORE
    # tracking this tick's reading, so a manual match can be recognised on
    # the same tick the change is first observed. Also drop a shared marker
    # (untracked, keyed by session) so the daemon's status-line downgrade
    # indicator can suppress itself for the very same manual choice.
    if facts.human_model_command:
        machine.note_manual_model_command(facts.human_model_command, now_wall=facts.now_wall)
    pending_marker_family = machine.manual_marker_pending
    if pending_marker_family:
        # Written on the FIRST tick that can name the session (not necessarily
        # the typing tick: right after a worker restart or during a compaction
        # the reading can be absent or synthetic with an empty session id).
        # Retried every tick until then so the marker is never silently lost.
        marker_session = (reading.session_id if reading is not None else None) or (
            machine.last_model_session
        )
        if marker_session:
            write_manual_model_marker(
                sidecar_dir.parent,
                session_id=marker_session,
                family=pending_marker_family,
                now=facts.now_wall,
            )
            machine.clear_manual_marker_pending()
            if log is not None:
                # Via the INJECTED log, never the global worker error log: this
                # function runs under unit test too, and a global-path write
                # would pollute the live session's log from a test run.
                log.write(
                    f"manual /model marker written: family={pending_marker_family!r} "
                    f"session={marker_session!r}"
                )
    if facts.human_effort_command:
        machine.note_manual_effort_command(facts.human_effort_command, now_wall=facts.now_wall)
    # Plan 00278: track the foreground model family so a ranked downgrade
    # opens an effort-restore episode (fired further below, subordinate to
    # every other family). Synthetic/stale readings are ignored.
    if reading is not None and not reading.stale:
        machine.note_model_reading(reading, now_wall=facts.now_wall)
    manual_model_note = machine.take_manual_model_note()
    evaluation = machine.evaluate(
        reading,
        idle=can_inject,
        now=facts.now_wall,
        human_compact_submitted=facts.human_compact_submitted,
        work_idle=facts.work_idle,
        foreground_ambiguous=foreground_ambiguous,
    )
    payload = _resolve_payload(evaluation.decision, dry_run=dry_run, now_wall=facts.now_wall)
    # Additional confirming Enters for a /model injection (set by the manual
    # switch branch and the auto-restore branch below); every other decision
    # leaves this at 0, which is a no-op in _perform_injection.
    confirm_enters = 0
    # Plan 00183: dry-run fires ONCE per session, once only. A dry-run marker is a
    # no-op on the environment (no real /compact), so context stays red and the
    # machine re-decides to act every episode -- and each marker is Enter-submitted,
    # so it lands as a real prompt that wakes the agent. Left unlatched this floods
    # the session (MONITOR -> WOULD_COMPACT -> AWAIT -> WOULD_ESCAPE x N -> ...).
    # After the FIRST would-be injection we latch OFF for the process lifetime: one
    # visible demonstration, then silence. Armed mode never latches -- real
    # compaction feedback resolves each episode there. The latch rides in the
    # machine state so it round-trips through the policy worker (Plan 00164 P4).
    dry_run_latched_log: str | None = None
    if dry_run and payload is not None:
        if machine.dry_run_fired:
            payload = None
            dry_run_latched_log = f"{_NOOP_LOG_PREFIX}: dry-run already fired once this session"
        else:
            machine.mark_dry_run_fired()
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
    # A dry-run suppression is not a NOOP decision (the machine still WOULD act),
    # so the block above skips it -- surface it explicitly for decision.log.
    if dry_run_latched_log is not None:
        noop_reason_log = dry_run_latched_log
    # Plan 00316: a manual /model match suppressed what would otherwise have
    # opened a downgrade episode this tick -- surface WHY nothing was done,
    # overriding whatever generic NOOP reason was derived above.
    if manual_model_note is not None:
        noop_reason_log = f"{_NOOP_LOG_PREFIX}: {manual_model_note}"
    # ── Goal injection (Plan 00269) ─────────────────────────────────────────
    # Strictly SUBORDINATE to compact/continue: the goal branch runs only when
    # this tick decided NOOP with no payload, no compaction signal is pending,
    # and the machine is in MONITOR — so a pending goal can never starve or
    # reorder a compact/continue decision. The idle + empty-input-box gate
    # applies unchanged (a deferred goal keeps its signal and retries).
    decision_value = evaluation.decision.value
    reason = evaluation.reason
    # Populated below whenever THIS tick's decision is WOULD_MODEL, so the
    # HOST can arm the mandatory coupled-effort correction after a
    # successful injection (Plan 00278 continuation) -- for BOTH the manual
    # test-trigger switch and the auto-restore flip-back.
    model_switch_family: str | None = None
    model_switch_session: str | None = None
    model_switch_is_auto_restore = False
    # Plan 00281: set True by the flip-flop flag-compact branch below, so the
    # HOST counts the successful injection against the flag-compaction budget.
    is_flag_compact = False
    # Plan 00297: set True only by the DROP ANCHOR branch immediately below,
    # so the HOST records the attempt against the anchor's OWN bookkeeping.
    is_anchor_injection = False
    # Plan 00297 follow-up: set True only by the DROP ANCHOR ESCALATION ESC
    # branch immediately below, so the HOST records the send against the
    # anchor's own ESC cooldown (`mark_anchor_esc`) rather than treating it
    # like a normal `/effort` injection.
    is_anchor_escape = False
    # Plan 00299: populated only by the goal branch below when this tick
    # decides WOULD_GOAL, so the HOST can record the injected text's hash
    # (`mark_goal_injection`) as the multi-plan thrash guard -- an identical
    # combined /goal never re-types on a later tick.
    goal_line_for_hash: str | None = None
    # ── DROP ANCHOR: fable-above-low invariant (Plan 00297) ─────────────────
    # HIGHEST subordinate priority -- checked even ahead of the coupled-effort
    # correction below -- because fable observed running above low effort is
    # the most expensive misconfiguration the product allows (owner ruling,
    # incident 2026-08-31), treated as an emergency rather than a preference.
    # Uniquely allowed to preempt a WOULD_CONTINUE nudge -- never
    # WOULD_COMPACT/WOULD_ESCAPE, which manage an in-flight compaction rather
    # than invite more work -- so a "stop everything" episode never resumes
    # the session onto another turn at the wrong effort. Gated on an empty
    # input box ONLY (never the full idle floor), matching the coupled-effort
    # and manual-model-switch precedent: an emergency correction cannot wait
    # for the session to go idle, and is NOT permanently deferred by a busy
    # box -- the anchor stays active and retries on the next unobstructed
    # tick. Uses its OWN cooldown (`anchor_injection_due`), bypassing the
    # floor mechanism's `_EFFORT_REINJECT_COOLDOWN_SECONDS`/
    # `_MAX_EFFORT_INJECTIONS` entirely. A successful PTY write here does NOT
    # verify anything -- only a later sidecar reading showing effort == low
    # (`_evaluate_anchor`, run from `note_model_reading` above) clears
    # `anchor_active`, so a swallowed injection (the live incident) keeps
    # retrying instead of masquerading as fixed.
    if machine.state is SupervisorState.MONITOR and machine.anchor_active:
        anchor_preempts_continue = evaluation.decision is Decision.WOULD_CONTINUE
        if payload is None or anchor_preempts_continue:
            if anchor_preempts_continue:
                payload = None
                consume_signal_path = None
            observed = (
                f"model={reading.model_id!r} effort={reading.effort!r}"
                if reading is not None
                else "model=? effort=?"
            )
            # Compaction is uninterruptible (owner ruling, Plan 00297
            # follow-up) -- an ESC must never fire while one is in flight,
            # even though `machine.state` staying MONITOR on the FIRST tick
            # that observes `reading.compacting` (before `_enter_await` can
            # run) would otherwise let it slip through.
            compacting_in_flight = reading is not None and reading.compacting
            if not facts.input_line_empty:
                deferred_log = f"{_DEFERRED_LOG_PREFIX} (DROP ANCHOR: fable above low effort)"
                noop_reason_log = None
            elif not compacting_in_flight and machine.anchor_esc_due(facts.now_wall):
                # Escalation ESC (Plan 00297 follow-up): retries alone have
                # not verified within the escalation bound, so interrupt the
                # in-flight turn that may be swallowing the `/effort low`
                # injection -- reusing the WOULD_ESCAPE keystroke path
                # AWAIT_COMPACTING already uses (raw ESC, no Enter).
                decision_value = Decision.WOULD_ESCAPE.value
                reason = (
                    f"DROP ANCHOR ESCALATED: fable observed above low effort ({observed}) "
                    f"persists after {machine.anchor_attempts} attempt(s) -> would send "
                    f"[esc] to interrupt the in-flight turn"
                )
                if dry_run:
                    payload = f"{_format_bot_prefix(facts.now_wall)} {_DRY_RUN_ANCHOR_ESCAPE_BODY}"
                else:
                    payload = _ESC_PAYLOAD
                submit = False
                is_anchor_escape = True
                deferred_log = None
                noop_reason_log = None
            elif machine.anchor_injection_due(facts.now_wall):
                decision_value = Decision.WOULD_EFFORT.value
                confirm_enters = effort_confirm_enters
                effort_command = f"{_EFFORT_COMMAND} {_ANCHOR_TARGET_EFFORT}"
                escalated = " [ESCALATED]" if machine.anchor_escalated_at(facts.now_wall) else ""
                reason = (
                    f"DROP ANCHOR: fable observed above low effort ({observed}) -> "
                    f"would inject {effort_command} "
                    f"(attempt {machine.anchor_attempts + 1}){escalated}"
                )
                if dry_run:
                    payload = (
                        f"{_format_bot_prefix(facts.now_wall)} "
                        f"{_DRY_RUN_EFFORT_BODY_PREFIX} {effort_command} [DROP ANCHOR]"
                    )
                else:
                    payload = effort_command
                    machine.arm_audit(f"{effort_command} (DROP ANCHOR emergency correction)")
                submit = True
                is_anchor_injection = True
                deferred_log = None
                noop_reason_log = None
            else:
                escalated = " [ESCALATED]" if machine.anchor_escalated_at(facts.now_wall) else ""
                noop_reason_log = (
                    f"{_NOOP_LOG_PREFIX}: DROP ANCHOR active, retry cooldown "
                    f"({observed}, attempt {machine.anchor_attempts}){escalated}"
                )
                deferred_log = None
                if anchor_preempts_continue:
                    decision_value = Decision.NOOP.value
                    reason = "DROP ANCHOR active -> continue nudge suppressed"
    # ── Coupled effort: "/model switch MUST be followed by /effort" ─────────
    # HIGH PRIORITY: checked BEFORE every other subordinate family (manual
    # model switch, goal, floor-based effort, auto-model-restore) so a
    # correction owed from a PRIOR /model switch is always delivered before
    # any new intent is considered. Still strictly subordinate to
    # compact/continue/escape above. Unconditional and UNGATED on any
    # downgrade episode or sidecar-observed recovery -- the manual-switch
    # path has no episode to gate on, and the auto-restore path must not
    # depend on a race-prone later reading confirming the switch landed.
    # This is the fix for the live defect where opus->fable left effort at
    # xhigh and burned account allowance.
    if (
        payload is None
        and evaluation.decision is Decision.NOOP
        and signal_path is None
        and machine.state is SupervisorState.MONITOR
        and machine.coupled_effort_pending is not None
    ):
        # Gate on an empty input box ONLY, not the full `can_inject` (which
        # also requires idle): the correction must land on the first tick
        # after the /model injection, or turns run the forced model at the
        # pre-switch effort and burn allowance. Same rationale and rail as
        # the manual model-switch branch below — never type into a box
        # mid-composition, but never wait for idle either.
        if not facts.input_line_empty:
            deferred_log = f"{_DEFERRED_LOG_PREFIX} (coupled effort pending)"
        else:
            decision_value = Decision.WOULD_EFFORT.value
            confirm_enters = effort_confirm_enters
            coupled_target = machine.coupled_effort_pending.rsplit(":", 1)[-1]
            effort_command = f"{_EFFORT_COMMAND} {coupled_target}"
            reason = (
                f"model switch requires coupled effort "
                f"({machine.coupled_effort_pending}) -> would inject {effort_command}"
            )
            if dry_run:
                payload = (
                    f"{_format_bot_prefix(facts.now_wall)} "
                    f"{_DRY_RUN_EFFORT_BODY_PREFIX} {effort_command}"
                )
            else:
                payload = effort_command
                # Decision-time arming (worker-side, hot-reloadable): in
                # production a decided payload is injected this same tick;
                # if the PTY write fails, the flush cannot print through
                # that same broken PTY either, so no false claim surfaces.
                machine.arm_audit(f"{effort_command} (coupled to model switch)")
            submit = True
            deferred_log = None
            noop_reason_log = None
    # ── Manual model-switch override (test trigger / deliberate override) ────
    # Checked ahead of goal, effort and auto-model-restore so a deliberate
    # switch is never starved by them -- but still strictly after
    # compact/continue/escape (and the coupled-effort correction) above, so
    # it can never disrupt an in-flight compaction or skip a correction
    # owed from a PRIOR switch. Unlike every other family, the injection
    # gate here is `facts.input_line_empty` ONLY, not the full `can_inject`
    # (which also requires the keystroke-idle floor): the whole point of a
    # manual trigger is to fire promptly, and an empty input box is the one
    # hard safety rail (never type into a box mid-composition).
    if (
        payload is None
        and evaluation.decision is Decision.NOOP
        and signal_path is None
        and machine.state is SupervisorState.MONITOR
    ):
        switch_path, switch_family, switch_reject = load_model_switch_signal(
            sidecar_dir,
            now=facts.now_wall,
            own_sessions=own_sessions,
        )
        if switch_reject is not None:
            # Fail-closed: an in-scope signal that failed validation (unknown
            # family, malformed JSON) is dropped and the reason is logged.
            noop_reason_log = f"{_NOOP_LOG_PREFIX}: {switch_reject}"
        elif switch_path is not None and switch_family is not None:
            if not facts.input_line_empty:
                if facts.idle:
                    deferred_log = f"{_DEFERRED_LOG_PREFIX} (model-switch signal pending)"
                else:
                    noop_reason_log = (
                        f"{_NOOP_LOG_PREFIX}: model-switch signal pending but session busy"
                    )
            else:
                decision_value = Decision.WOULD_MODEL.value
                model_command = f"{_MODEL_COMMAND} {switch_family}"
                reason = f"model-switch signal -> would inject {model_command}"
                model_switch_family = switch_family
                # The signal file is named "<session_id>.model-switch-intent"
                # (write_model_switch_signal) -- derive the session the same
                # way rather than re-parsing the JSON body a second time.
                model_switch_session = switch_path.name.removesuffix(_MODEL_SWITCH_SIGNAL_SUFFIX)
                if dry_run:
                    payload = (
                        f"{_format_bot_prefix(facts.now_wall)} "
                        f"{_DRY_RUN_MODEL_BODY_PREFIX} {model_command}"
                    )
                else:
                    payload = model_command
                    machine.arm_audit(f"{model_command} (manual switch signal)")
                submit = True
                confirm_enters = model_confirm_enters
                # Consumed in dry-run too — the demonstration episode is
                # spent either way, mirroring the goal signal's rule.
                consume_signal_path = str(switch_path)
                deferred_log = None
                noop_reason_log = None
    if (
        payload is None
        and evaluation.decision is Decision.NOOP
        and signal_path is None
        and machine.state is SupervisorState.MONITOR
    ):
        goal_path, goal_line, goal_reject = load_goal_signal(
            sidecar_dir,
            now=facts.now_wall,
            ttl_seconds=goal_signal_ttl_seconds,
            own_sessions=own_sessions,
        )
        if goal_reject is not None:
            # Fail-closed: an in-scope signal that failed the validation gate
            # is dropped and the reason is logged (deduped by write_noop).
            noop_reason_log = f"{_NOOP_LOG_PREFIX}: {goal_reject}"
        elif goal_path is not None and goal_line is not None:
            if not can_inject:
                if facts.idle and not facts.input_line_empty:
                    deferred_log = f"{_DEFERRED_LOG_PREFIX} (goal injection pending)"
                else:
                    noop_reason_log = f"{_NOOP_LOG_PREFIX}: goal signal pending but session busy"
            elif machine.goal_injections >= _MAX_GOAL_INJECTIONS:
                noop_reason_log = f"{_NOOP_LOG_PREFIX}: goal injection cap reached"
            elif goal_line == machine.last_goal_text:
                # Plan 00299 thrash guard: the daemon re-renders the combined
                # multi-plan /goal on every ledgered status flip (not just
                # this session's own), so an unrelated plan's edit can
                # rewrite an IDENTICAL signal file. Re-typing the same
                # /goal condition teaches nothing new -- skip it. The stale
                # duplicate file is left for the TTL reaper, mirroring how a
                # completed plan's now-stale signal already ages out today.
                noop_reason_log = f"{_NOOP_LOG_PREFIX}: goal signal identical to last injection"
            else:
                # The cap is counted by the HOST only after a SUCCESSFUL
                # injection (see the callers of _apply_decision) — a PTY
                # write failure must not burn the cap while the signal
                # survives for retry. decide_once stays pure.
                decision_value = Decision.WOULD_GOAL.value
                reason = "goal signal -> would inject /goal"
                if dry_run:
                    payload = (
                        f"{_format_bot_prefix(facts.now_wall)} "
                        f"{_DRY_RUN_GOAL_BODY_PREFIX} {goal_line}"
                    )
                else:
                    payload = f"{_GOAL_COMMAND} {goal_line}"
                submit = True
                goal_line_for_hash = goal_line
                # Consumed in dry-run too — the demonstration episode is spent
                # either way, so a goal can never flood the session.
                consume_signal_path = str(goal_path)
                deferred_log = None
                noop_reason_log = None
    # ── Effort restore on model downgrade (Plan 00278) ──────────────────────
    # Subordinate to every other family: fires only on a tick that would
    # otherwise NOOP in MONITOR with no compaction signal and no goal payload.
    # The pending episode persists across deferred ticks; the HOST closes it
    # (mark_effort_injection) only after a successful PTY write, so a failed
    # write retries. No signal file exists to consume — the episode key in the
    # machine state is the whole lifecycle.
    if (
        payload is None
        and evaluation.decision is Decision.NOOP
        and signal_path is None
        and machine.state is SupervisorState.MONITOR
        and machine.effort_pending is not None
    ):
        if not can_inject:
            if facts.idle and not facts.input_line_empty:
                deferred_log = f"{_DEFERRED_LOG_PREFIX} (effort restore pending)"
            else:
                noop_reason_log = f"{_NOOP_LOG_PREFIX}: effort restore pending but session busy"
        elif machine.effort_injections >= _MAX_EFFORT_INJECTIONS:
            noop_reason_log = f"{_NOOP_LOG_PREFIX}: effort injection cap reached"
        else:
            decision_value = Decision.WOULD_EFFORT.value
            confirm_enters = effort_confirm_enters
            target = machine.effort_pending.rsplit(":", 1)[-1]
            effort_command = f"{_EFFORT_COMMAND} {target}"
            reason = (
                f"effort below floor ({machine.effort_pending}) -> "
                f"would inject {effort_command}"
            )
            if dry_run:
                payload = (
                    f"{_format_bot_prefix(facts.now_wall)} "
                    f"{_DRY_RUN_EFFORT_BODY_PREFIX} {effort_command}"
                )
            else:
                payload = effort_command
                machine.arm_audit(f"{effort_command} (effort-floor restore)")
            submit = True
            deferred_log = None
            noop_reason_log = None
    # ── Flag-cleaning compaction on a REPEATED downgrade (Plan 00281) ───────
    # Checked JUST BEFORE the model restore: on a flip-flop (an open episode
    # AND a prior auto-restore that the classifier already undid) a re-restore
    # cannot win while the context keeps re-tripping, so instead fire ONE armed
    # `/compact` that asks the agent to summarise the sensitive material at a
    # high level. Opt-in (default off), capped and backed off (all inside
    # flag_compact_due). Fires only when idle (can_inject) so no ESC interrupt
    # is needed; the resume is driven by the compaction signal like every other
    # `/compact`. The HOST counts a successful injection (mark_flag_compaction).
    if (
        payload is None
        and evaluation.decision is Decision.NOOP
        and signal_path is None
        and machine.state is SupervisorState.MONITOR
        and machine.flag_compact_due(facts.now_wall)
    ):
        if not can_inject:
            if facts.idle and not facts.input_line_empty:
                deferred_log = f"{_DEFERRED_LOG_PREFIX} (flag-cleaning compact pending)"
            else:
                noop_reason_log = (
                    f"{_NOOP_LOG_PREFIX}: flag-cleaning compact pending but session busy"
                )
        else:
            decision_value = Decision.WOULD_COMPACT.value
            reason = "repeated downgrade (flip-flop) -> would inject flag-cleaning /compact"
            is_flag_compact = True
            if dry_run:
                payload = f"{_format_bot_prefix(facts.now_wall)} {_DRY_RUN_FLAG_COMPACT_BODY}"
            else:
                # `/compact` MUST stay the FIRST token so it is recognised as the
                # slash command; the bot chrome rides along as its instruction.
                payload = f"/compact {_format_bot_prefix(facts.now_wall)} {_FLAG_COMPACT_BODY}"
                machine.arm_audit("/compact (flag-cleaning after repeated downgrade)")
            submit = True
            deferred_log = None
            noop_reason_log = None
    # ── Model restore after a downgrade (Plan 00278 Task 2b.3) ──────────────
    # Fires only on an otherwise-NOOP MONITOR tick, after the quiet delay,
    # under the lifetime cap and the flip-flop backoff (all inside
    # model_restore_due). The HOST marks success (mark_model_restore) for
    # the cap/backoff, and ALSO arms the coupled effort correction
    # (arm_coupled_effort) -- see `model_switch_is_auto_restore` below.
    if (
        payload is None
        and evaluation.decision is Decision.NOOP
        and signal_path is None
        and machine.state is SupervisorState.MONITOR
    ):
        restore_family = machine.model_restore_due(facts.now_wall)
        if restore_family is not None:
            if not can_inject:
                if facts.idle and not facts.input_line_empty:
                    deferred_log = f"{_DEFERRED_LOG_PREFIX} (model restore pending)"
                else:
                    noop_reason_log = f"{_NOOP_LOG_PREFIX}: model restore pending but session busy"
            else:
                decision_value = Decision.WOULD_MODEL.value
                model_command = f"{_MODEL_COMMAND} {restore_family}"
                reason = f"downgrade quiet delay elapsed -> would inject {model_command}"
                model_switch_family = restore_family
                model_switch_session = machine.last_model_session
                model_switch_is_auto_restore = True
                if dry_run:
                    payload = (
                        f"{_format_bot_prefix(facts.now_wall)} "
                        f"{_DRY_RUN_MODEL_BODY_PREFIX} {model_command}"
                    )
                else:
                    payload = model_command
                    machine.arm_audit(f"{model_command} (auto-restore after downgrade)")
                submit = True
                confirm_enters = model_confirm_enters
                deferred_log = None
                noop_reason_log = None
    # ── Audit-trail flush: transient status-line banner (Plan 00318) ────────
    # LOWEST priority of all families: a pending /model, /effort, goal or
    # restore always lands first, so a switch sequence flushes as ONE banner
    # once the sequence itself is complete. /model and /effort leave no trace
    # in the chat (unlike /compact and /goal, whose payloads carry visible
    # text) — without this flush, decision.log is the only audit trail and
    # nobody watching the session can tell anything happened.
    #
    # This posts a TTL-bounded, self-counting-down BANNER rather than
    # injecting a chat line. The chat form cost a whole model turn plus a
    # permanent transcript entry to tell the HUMAN something the session
    # itself did not need to know; a banner costs neither. It also needs
    # neither an idle session nor an empty input box (`can_inject`) — writing
    # a file cannot disturb what the user is typing — so the notice surfaces
    # at once instead of waiting for a quiet moment.
    if (
        payload is None
        and evaluation.decision is Decision.NOOP
        and signal_path is None
        and machine.state is SupervisorState.MONITOR
        and machine.audit_pending
    ):
        decision_value = Decision.WOULD_AUDIT.value
        pending_items = machine.audit_pending
        reason = f"audit trail flush ({len(pending_items)} item(s))"
        write_status_message(
            sidecar_dir.parent,
            text=_format_audit_banner(pending_items),
            expires_at=facts.now_wall + _AUDIT_BANNER_TTL_SECONDS,
            countdown=True,
        )
        # Cleared at DECISION time (worker-side, hot-reloadable): a failed
        # banner write then LOSES this audit instead of retrying it —
        # acceptable, because the notice is a convenience surface and
        # decision.log (below) keeps the durable record either way.
        machine.mark_audit_injection()
        # The log line carries the FULL items (reasons included) — the banner
        # showed only the short form. A deferral log already in hand wins,
        # since it describes an injection this tick actually held back.
        if deferred_log is None:
            noop_reason_log = f"{reason}: {'; '.join(pending_items)}"
    # ── Standing-authorisation reinforcement (Plan 00283) ───────────────────
    # LEAST urgent of every injectable family: a reminder, not an action, so it
    # fires only on a tick that would otherwise NOOP in MONITOR with nothing else
    # pending — a real /compact, /model, /effort, goal, restore or audit flush
    # always lands first. The daemon's standing_authorisations handler writes the
    # signal on a due reinforcement when its supervisor channel is enabled; here
    # we type it as one real user-role line. The HOST counts a successful
    # injection (mark_standing_auth_injection); the signal is consumed on
    # injection so it cannot re-fire. Reuses the goal signal TTL.
    if (
        payload is None
        and evaluation.decision is Decision.NOOP
        and signal_path is None
        and machine.state is SupervisorState.MONITOR
    ):
        sa_path, sa_line, sa_reject = load_standing_auth_signal(
            sidecar_dir,
            now=facts.now_wall,
            ttl_seconds=goal_signal_ttl_seconds,
            own_sessions=own_sessions,
        )
        if sa_reject is not None:
            # Fail-closed: an in-scope signal that failed the gate is dropped and
            # the reason logged (deduped by write_noop).
            noop_reason_log = f"{_NOOP_LOG_PREFIX}: {sa_reject}"
        elif sa_path is not None and sa_line is not None:
            if not can_inject:
                if facts.idle and not facts.input_line_empty:
                    deferred_log = f"{_DEFERRED_LOG_PREFIX} (standing-auth reminder pending)"
                else:
                    noop_reason_log = (
                        f"{_NOOP_LOG_PREFIX}: standing-auth signal pending but session busy"
                    )
            elif machine.standing_auth_injections >= _MAX_STANDING_AUTH_INJECTIONS:
                noop_reason_log = f"{_NOOP_LOG_PREFIX}: standing-auth injection cap reached"
            else:
                # Counted by the HOST only after a SUCCESSFUL injection, so a PTY
                # write failure does not burn the backstop while the signal
                # survives for retry. decide_once stays pure.
                decision_value = Decision.WOULD_STANDING_AUTH.value
                reason = "standing-auth signal -> would inject reminder"
                if dry_run:
                    payload = (
                        f"{_format_bot_prefix(facts.now_wall)} "
                        f"{_DRY_RUN_STANDING_AUTH_BODY_PREFIX} {sa_line}"
                    )
                else:
                    # The joined line ALREADY opens with the bot-prefixed
                    # machine-origin header, so it is typed verbatim as one real
                    # user-role line — no slash command, no extra chrome.
                    payload = sa_line
                submit = True
                # Consumed on injection (dry-run too) so it cannot re-fire.
                consume_signal_path = str(sa_path)
                deferred_log = None
                noop_reason_log = None
    return TickOutcome(
        decision_value=decision_value,
        reason=reason,
        payload=payload,
        submit=submit,
        consume_signal_path=consume_signal_path,
        deferred_log=deferred_log,
        machine_state=machine.export_state(),
        noop_reason_log=noop_reason_log,
        confirm_enters=confirm_enters,
        model_switch_family=model_switch_family,
        model_switch_session=model_switch_session,
        model_switch_is_auto_restore=model_switch_is_auto_restore,
        is_flag_compact=is_flag_compact,
        is_anchor_injection=is_anchor_injection,
        is_anchor_escape=is_anchor_escape,
        goal_line=goal_line_for_hash,
    )


def _apply_decision(
    outcome: TickOutcome,
    *,
    master_writer: Callable[[bytes], None],
    log: DecisionLog | None,
    host_state: str | None = None,
) -> bool:
    """Perform a :class:`TickOutcome` on the PTY (host side, Plan 00164 P4).

    Injects the payload (if any), logs it, and consumes the compaction signal
    only AFTER a successful resume injection — mirroring the original inline
    behaviour of ``_poll_once`` exactly.

    Returns True when a payload was actually injected (the PTY write
    succeeded); False on a NOOP/suppressed tick. Callers use this to count a
    ``would-goal`` injection against the goal cap only on SUCCESS (Plan
    00269 review fix): a PTY write failure raises out of this function
    before the return, so the cap is not burned and the un-consumed signal
    survives for retry.

    Plan 00182 defence-in-depth: ``host_state`` is the host's authoritative
    ``SupervisorState`` value BEFORE this outcome is adopted. If the host is
    already ``AWAIT_COMPACTING`` a ``WOULD_COMPACT`` outcome is stale/impossible
    (a compaction is already in flight) -- injecting it would stack a second
    ``/compact``, so it is suppressed and logged rather than performed. The
    in-process path passes ``None`` (single live machine -- no desync possible).
    """
    if (
        host_state == SupervisorState.AWAIT_COMPACTING.value
        and outcome.decision_value == Decision.WOULD_COMPACT.value
    ):
        if log is not None:
            log.write_noop("noop: stale /compact suppressed (host already awaiting compaction)")
        return False
    if outcome.payload is not None:
        _perform_injection(
            master_writer,
            outcome.payload,
            submit=outcome.submit,
            confirm_enters=outcome.confirm_enters,
        )
        if log is not None:
            log.write(f"{outcome.decision_value}: {outcome.reason}; injected {outcome.payload!r}")
        if outcome.consume_signal_path is not None:
            _consume_signal(Path(outcome.consume_signal_path), log)
        return True
    if log is not None and outcome.deferred_log is not None:
        # An injection was pending and the NON-EMPTY INPUT BOX was the sole gate.
        log.write(outcome.deferred_log)
    elif log is not None and outcome.noop_reason_log is not None:
        # Plan 00168 Phase 1: record WHY this idle tick did nothing (deduped, so
        # an unchanged gate never floods). Makes red-but-not-compacting visible.
        log.write_noop(outcome.noop_reason_log)
    return False


def _apply_post_injection_bookkeeping(
    machine: CompactStateMachine,
    outcome: TickOutcome,
    *,
    injected: bool,
    now_wall: float | None = None,
) -> None:
    """Update per-family cap/pending bookkeeping after ``_apply_decision`` runs.

    Shared by both host call sites (``_poll_once``'s in-process path and
    ``supervise()``'s ``_on_poll``) so the success-only counting/clearing
    rules for every injectable family live in exactly one place. Every
    mark_*/arm_* call below fires ONLY on a successful injection
    (``injected``) -- a failed PTY write keeps every pending episode/signal
    intact so the next tick retries. ``now_wall`` feeds
    ``mark_anchor_injection`` (Plan 00297), whose own retry cooldown is
    timestamp-based; it is only ever consulted for an anchor injection, so
    the default (resolved lazily via ``time.time()``) never affects any
    other family or any existing caller that omits it.
    """
    if not injected:
        return
    # NOTE: audit-trail arming and clearing deliberately do NOT live here.
    # They happen at DECISION time inside decide_once, i.e. in the WORKER --
    # so the whole audit loop is deployable by worker hot-reload without a
    # host restart (import_state merges by present key, so an older host
    # never clobbers the worker's audit backlog; it merely doesn't carry it).
    if outcome.decision_value == Decision.WOULD_GOAL.value:
        machine.mark_goal_injection(outcome.goal_line)
    elif outcome.decision_value == Decision.WOULD_STANDING_AUTH.value:
        machine.mark_standing_auth_injection()
    elif outcome.decision_value == Decision.WOULD_EFFORT.value:
        # The DROP ANCHOR branch is checked FIRST of all, then the coupled
        # branch, in decide_once -- so `is_anchor_injection` (Plan 00297)
        # disambiguates from the coupled correction (Plan 00278
        # continuation), which in turn disambiguates from the plain floor
        # restore.
        if outcome.is_anchor_injection:
            machine.mark_anchor_injection(now_wall if now_wall is not None else time.time())
        elif machine.coupled_effort_pending is not None:
            machine.mark_coupled_effort_injection()
        else:
            machine.mark_effort_injection()
    elif outcome.decision_value == Decision.WOULD_MODEL.value:
        # Cap/backoff bookkeeping is reserved for the AUTO-restore path --
        # the manual test-trigger switch has its own signal-consumption
        # lifecycle and must not eat into that budget.
        if outcome.model_switch_is_auto_restore:
            machine.mark_model_restore()
        # BOTH paths owe the coupled effort correction, unconditionally.
        if outcome.model_switch_family is not None:
            machine.arm_coupled_effort(
                session=outcome.model_switch_session or "",
                family=outcome.model_switch_family,
            )
    elif outcome.decision_value == Decision.WOULD_COMPACT.value and outcome.is_flag_compact:
        # Plan 00281: only the flip-flop flag-compact counts against the
        # flag-compaction cap/backoff. The capacity-based /compact leaves
        # is_flag_compact False and is governed by its AWAIT_COMPACTING
        # lifecycle instead.
        machine.mark_flag_compaction()
    elif outcome.decision_value == Decision.WOULD_ESCAPE.value and outcome.is_anchor_escape:
        # Plan 00297 follow-up: the escalation ESC has its OWN cooldown,
        # separate from the AWAIT_COMPACTING flush's `_escapes_sent` counter
        # (which `evaluate()` already manages internally) -- `is_anchor_escape`
        # disambiguates the two so a compaction flush never throttles, or is
        # throttled by, the anchor's interrupt.
        machine.mark_anchor_esc(now_wall if now_wall is not None else time.time())


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
    human_model_command: str | None = None,
    human_effort_command: str | None = None,
    work_idle: bool = True,
    reap_ttl_seconds: float = _DEFAULT_REAP_TTL_SECONDS,
    foreground_margin_seconds: float = _DEFAULT_FOREGROUND_MARGIN_SECONDS,
    own_sessions: frozenset[str] | None = None,
    goal_signal_ttl_seconds: float = _DEFAULT_GOAL_SIGNAL_TTL_SECONDS,
    model_confirm_enters: int = _DEFAULT_MODEL_CONFIRM_ENTERS,
    effort_confirm_enters: int = _DEFAULT_EFFORT_CONFIRM_ENTERS,
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
        human_model_command=human_model_command,
        human_effort_command=human_effort_command,
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
        goal_signal_ttl_seconds=goal_signal_ttl_seconds,
        model_confirm_enters=model_confirm_enters,
        effort_confirm_enters=effort_confirm_enters,
    )
    injected = _apply_decision(outcome, master_writer=master_writer, log=log)
    _apply_post_injection_bookkeeping(machine, outcome, injected=injected, now_wall=now_wall)
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
            "human_model_command": facts.human_model_command,
            "human_effort_command": facts.human_effort_command,
            "human_raw_input": facts.human_raw_input,
            "machine_state": facts.machine_state,
            "tick_id": facts.tick_id,
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
        human_model_command=data.get("human_model_command"),
        human_effort_command=data.get("human_effort_command"),
        human_raw_input=str(data.get("human_raw_input", "")),
        machine_state=data.get("machine_state"),
        tick_id=int(data.get("tick_id", 0)),
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
            "tick_id": outcome.tick_id,
            "confirm_enters": outcome.confirm_enters,
            "model_switch_family": outcome.model_switch_family,
            "model_switch_session": outcome.model_switch_session,
            "model_switch_is_auto_restore": outcome.model_switch_is_auto_restore,
            "is_flag_compact": outcome.is_flag_compact,
            "is_anchor_injection": outcome.is_anchor_injection,
            "is_anchor_escape": outcome.is_anchor_escape,
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
        tick_id=int(data.get("tick_id", 0)),
        confirm_enters=int(data.get("confirm_enters", 0)),
        model_switch_family=data.get("model_switch_family"),
        model_switch_session=data.get("model_switch_session"),
        model_switch_is_auto_restore=bool(data.get("model_switch_is_auto_restore", False)),
        is_flag_compact=bool(data.get("is_flag_compact", False)),
        is_anchor_injection=bool(data.get("is_anchor_injection", False)),
        is_anchor_escape=bool(data.get("is_anchor_escape", False)),
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

    Also owns a persistent ``HumanInputLine`` (Plan 00317): the SAME class the
    host's in-process fallback uses, but here it is fed each tick's
    ``human_raw_input`` and its recognition -- ``human_compact_submitted`` /
    ``human_model_command`` / ``human_effort_command`` / ``input_line_empty``
    -- overrides whatever the host sent, before ``decide_once`` runs. This is
    the piece that now hot-reloads with the rest of the worker: a code change
    to typed-command recognition takes effect on the next worker restart, with
    no host-side change and no session restart. Reset (buffer cleared) on
    every restart -- the same risk profile ``machine``'s in-flight state
    already carries across a restart.
    """
    machine = CompactStateMachine(policy)
    line_recognizer = HumanInputLine()
    for raw in in_stream:
        line = raw.strip()
        if not line:
            continue
        try:
            facts = _facts_from_json(line)
        except (ValueError, KeyError) as exc:
            append_worker_error(f"bad tick line: {exc}")
            continue
        if facts.human_raw_input:
            try:
                line_recognizer.feed(base64.b64decode(facts.human_raw_input))
            except (ValueError, TypeError) as exc:
                # Fail-open: a malformed raw-input chunk must never stall or
                # crash the worker -- just skip recognition for this tick.
                append_worker_error(f"bad raw-input chunk: {exc}")
        facts = replace(
            facts,
            human_compact_submitted=line_recognizer.take_compact_submitted(),
            human_model_command=line_recognizer.take_model_submitted(),
            human_effort_command=line_recognizer.take_effort_submitted(),
            # AND, never override: a worker restart resets this recognizer, so
            # its buffer reads EMPTY while the human still has unsubmitted text
            # in the box. Overriding the host's own observation there would let
            # `can_inject` go True and the supervisor type into a non-empty
            # input box -- the exact invariant the empty-box guard exists for.
            # Either side seeing text is enough to hold the injection.
            input_line_empty=facts.input_line_empty and line_recognizer.is_empty,
        )
        for typed_slash in line_recognizer.take_slash_submitted():
            # Recognition-miss observability: what the human's submitted
            # slash line actually contained at the byte level, so a MISS
            # (e.g. autocomplete inserting text the PTY never carries) is
            # diagnosable from the field.
            append_worker_error(
                f"diagnostic typed-slash observed: {typed_slash!r} "
                f"(recognised model={facts.human_model_command!r} "
                f"effort={facts.human_effort_command!r})"
            )
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
                goal_signal_ttl_seconds=policy.goal_signal_ttl_seconds,
                model_confirm_enters=policy.model_confirm_enters,
                effort_confirm_enters=policy.effort_confirm_enters,
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
        # Plan 00182: echo the request's correlation id so the host can match
        # this reply to the tick that produced it and drop any stale reply left
        # buffered by an earlier timed-out tick.
        out_stream.write(_outcome_to_json(replace(outcome, tick_id=facts.tick_id)) + "\n")
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
        # Plan 00182: monotonically increasing per-request correlation id. The
        # worker echoes it back; `decide` drops any reply whose id does not match
        # the current request, so a stale reply buffered by a timed-out tick is
        # never injected out of turn.
        self._tick_id = 0

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
        """Ask the worker for a decision; None on any failure (host falls back).

        Plan 00182: each request carries a unique ``tick_id`` the worker echoes
        back. A reply whose id does not match the current request is a stale
        reply left buffered by an earlier timed-out (slow) tick -- it is drained
        and discarded, NEVER returned. Without this, a slow tick's late
        ``WOULD_COMPACT`` reply would be read on the NEXT tick and injected on
        top of a still-queued ``/compact``, stacking two compactions.
        """
        proc = self._proc
        if proc is None or proc.stdin is None or proc.stdout is None or proc.poll() is not None:
            return None
        self._tick_id += 1
        tick_id = self._tick_id
        try:
            proc.stdin.write(_facts_to_json(replace(facts, tick_id=tick_id)) + "\n")
            proc.stdin.flush()
        except (OSError, ValueError):
            return None
        # Bounded wait: a hung worker must not stall the PTY host for a whole
        # tick. Drain stale replies (from earlier timed-out ticks) until the
        # reply matching THIS request arrives or the deadline passes.
        deadline = time.monotonic() + self._read_timeout
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return None
            ready, _, _ = select.select([proc.stdout], [], [], remaining)
            if not ready:
                return None
            try:
                line = proc.stdout.readline()
            except (OSError, ValueError):
                return None
            if not line:
                return None
            try:
                outcome = _outcome_from_json(line)
            except (ValueError, KeyError):
                return None
            if outcome.tick_id == tick_id:
                return outcome
            # Stale reply from a previously timed-out tick -- discard and keep
            # reading for the reply that matches this request.

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
    ctrl_c_gate: CtrlCGate | None = None,
    on_ctrl_c_event: Callable[[str], object] | None = None,
    raw_tap: RawInputTap | None = None,
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
                # Ctrl+C double-press gate (Plan 00312): a lone 0x03 chunk is
                # withheld until confirmed by a second press inside the window.
                # Runs BEFORE strip_suspend so the exact-chunk press test sees
                # the raw read. A fully-swallowed chunk forwards nothing but
                # must NOT be mistaken for EOF — so this stays inside `if data:`.
                if ctrl_c_gate is not None:
                    data, ctrl_c_event = ctrl_c_gate.filter(data)
                    if ctrl_c_event is not None and on_ctrl_c_event is not None:
                        # Surface the swallow/forward (status-line hint +
                        # decision log). Best-effort: never let it break I/O.
                        on_ctrl_c_event(ctrl_c_event)
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
                    if raw_tap is not None:
                        # Plan 00317: same bytes, second sink -- feeds the
                        # worker's hot-reloadable recognizer. Never blocks or
                        # alters what is written to the child below.
                        raw_tap.append(forwarded)
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
    # Plan 00317: fed the same forwarded bytes as `activity`, drained once per
    # tick into TickFacts.human_raw_input for the worker's own recognizer.
    raw_tap = RawInputTap()
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
        # Plan 00316: consume a human-typed /model or /effort command the same
        # way -- edge-triggered, once per tick, so a manual choice is recorded
        # exactly once however many ticks pass before the worker consumes it.
        human_model_command = activity.take_model_submitted()
        human_effort_command = activity.take_effort_submitted()
        # Plan 00317: drain the raw tap for the worker's OWN recognizer. Sent
        # alongside the host-computed fields above (unchanged, still used by
        # the in-process fallback below) so a worker restart alone can change
        # recognition behaviour without any host-side code change.
        human_raw_input = base64.b64encode(raw_tap.drain()).decode("ascii")
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
                    human_model_command=human_model_command,
                    human_effort_command=human_effort_command,
                    human_raw_input=human_raw_input,
                    # Ship the host's authoritative machine state so the worker
                    # decides on it -- never on divergent worker-local state.
                    machine_state=machine.export_state(),
                )
            )
        if outcome is not None:
            # Plan 00182: pass the host's authoritative PRE-tick state so a stale
            # WOULD_COMPACT reply (worker still MONITOR, host already awaiting)
            # is suppressed instead of stacking a second /compact.
            injected = _apply_decision(
                outcome,
                master_writer=_write_master,
                log=log,
                host_state=machine.state.value,
            )
            # Adopt the worker's post-tick state so `machine` remains the single
            # source of truth; a later in-process fallback tick then cannot
            # diverge and inject a duplicate /compact (Plan 00164 Phase 4 fix).
            if outcome.machine_state is not None:
                machine.import_state(outcome.machine_state)
            # Success-only bookkeeping, run AFTER adopting the worker state
            # (which carries the pre-injection counts) so no increment is
            # ever overwritten. A PTY write failure raises above, so no cap
            # is burned and no pending episode/signal is lost (Plan 00269
            # review fix; Plan 00278 continuation).
            _apply_post_injection_bookkeeping(
                machine, outcome, injected=injected, now_wall=now_wall
            )
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
                human_model_command=human_model_command,
                human_effort_command=human_effort_command,
                work_idle=work_idle,
                reap_ttl_seconds=policy.reap_ttl_seconds,
                foreground_margin_seconds=policy.foreground_margin_seconds,
                own_sessions=cached_own_session_ids(),  # Plan 00166: only our own sessions
                goal_signal_ttl_seconds=policy.goal_signal_ttl_seconds,
            )
        # Plan 00297: loud, rate-limited owner-facing alert once the anchor
        # has exceeded its attempt/time bound -- purely a notification, the
        # anchor keeps retrying either way (see `anchor_escalated_at`).
        if machine.anchor_active and machine.anchor_escalated_at(now_wall):
            status_message_poster.post(_ANCHOR_ALERT_TEXT, level=_STATUS_LEVEL_WARNING)

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

    # Ctrl+C double-press guard (Plan 00312): on by default, disable with
    # CCY_CTRL_C_GUARD=0; window via CCY_CTRL_C_WINDOW_SECONDS. A None gate is
    # full passthrough — Ctrl+C behaves exactly as before the guard existed.
    ctrl_c_gate = (
        CtrlCGate(window_seconds=_resolve_ctrl_c_window_seconds())
        if _resolve_ctrl_c_guard_enabled()
        else None
    )

    def _on_ctrl_c_event(event: str) -> None:
        if event == _CTRL_C_EVENT_SWALLOWED and ctrl_c_gate is not None:
            status_message_poster.post(
                _ctrl_c_notice_text(ctrl_c_gate.window_seconds),
                level=_STATUS_LEVEL_WARNING,
            )
        if log is not None:
            log.write(f"ctrl-c guard: {event}")

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
            ctrl_c_gate=ctrl_c_gate,
            on_ctrl_c_event=_on_ctrl_c_event,
            raw_tap=raw_tap,
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


def _newest_sidecar_session_id(directory: Path) -> str | None:
    """Return the ``session_id`` of the newest (max-``ts``) context sidecar.

    Mirrors the scan ``load_freshest_sidecar`` uses for the supervisor's own
    current reading source, without the freshness/staleness filtering -- the
    CLI trigger wants whichever session is CURRENTLY under supervision,
    live or not, so it can target it explicitly.
    """
    scanned = _scan_sidecars(directory)
    if not scanned:
        return None
    data, _ts = max(scanned, key=lambda pair: pair[1])
    session_id = data.get("session_id")
    return session_id if isinstance(session_id, str) and session_id else None


def _run_emit_model_switch(argv: list[str]) -> int:
    """CLI test-trigger: write a manual model-switch signal and exit.

    Does NOT start a supervisor. Resolves the sidecar/signal directory the
    SAME way the running supervisor does (`_default_sidecar_dir`) and targets
    whichever session currently owns the newest context sidecar there.

    Returns:
        0 on success; 2 on a usage error (missing ``<family>`` argument);
        1 when no sidecar can be found or the signal cannot be written.
    """
    index = argv.index(_EMIT_MODEL_SWITCH_FLAG)
    if index + 1 >= len(argv):
        sys.stderr.write(f"Usage: claude-supervise.py {_EMIT_MODEL_SWITCH_FLAG} <family>\n")
        return 2
    family_arg = argv[index + 1]
    sidecar_dir = _default_sidecar_dir()
    session_id = _newest_sidecar_session_id(sidecar_dir)
    if session_id is None:
        sys.stderr.write(
            f"claude-supervise: no context sidecar found under {sidecar_dir} -- "
            "is a supervised session running?\n"
        )
        return 1
    try:
        path = write_model_switch_signal(
            sidecar_dir, session_id=session_id, family=family_arg, now=time.time()
        )
    except ValueError as exc:
        sys.stderr.write(f"claude-supervise: {exc}\n")
        return 1
    except OSError as exc:
        sys.stderr.write(f"claude-supervise: could not write model-switch signal: {exc}\n")
        return 1
    sys.stdout.write(
        f"claude-supervise: wrote model-switch signal for session {session_id} "
        f"-> {family_arg} ({path})\n"
    )
    return 0


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

    # CLI test-trigger mode: writes a manual model-switch signal and exits --
    # never starts a supervisor, so it is checked before the worker/child-argv
    # modes below.
    if _EMIT_MODEL_SWITCH_FLAG in argv:
        return _run_emit_model_switch(argv)

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
