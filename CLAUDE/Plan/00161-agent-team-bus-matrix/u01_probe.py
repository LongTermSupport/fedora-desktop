#!/usr/bin/env python3
"""Plan 00161 unit U01: the Claude Code probes behind triage.bash (DESIGN.md sections 6, 13).

Each `claude-env` and `session` run is one triage leg. It gathers facts, appends a Markdown
section to the report and renders no verdict (PlanScriptStandards R9). A leg exits non-zero
when it could not establish its facts. Progress goes to stderr; the report is the payload.

Subcommands:
  claude-env  the Claude Code version, the ccy token the child sessions use and the auth
              method Claude Code reports with it (no account details are recorded)
  session     one throwaway child session (`claude -p` with stream-json input, so it sits
              idle between turns), started with a throwaway plugin through --plugin-dir and
              a variant's --settings. The plugin's four hooks (SessionStart,
              UserPromptSubmit, Stop, SessionEnd) record that they fired; SessionStart also
              starts a detached writer, which inherits the session's
              CLAUDE_CODE_MESSAGING_SOCKET and _TOKEN exactly as `pingbus watch` will, and
              sends notices to the socket when this driver tells it to. Variants:
                main               bypass mode with crossSessionInbound accept (how ccy runs):
                                   a typed turn, a notice to the idle session, the same
                                   notice again, two notices back to back, the dedupe
                                   window, then the session's end
                bypass-no-accept   one notice, bypass mode, no setting
                default-accept     one notice, default permission mode, the setting
                default-no-accept  one notice, default permission mode, no setting
  hook        internal: run by the throwaway plugin's hooks
  writer      internal: the detached process the SessionStart hook starts

The child is isolated from everything live: its environment never carries a running
session's socket or token; it runs in an empty scratch directory with --setting-sources
project (so the user's own settings, hooks and plugins are not loaded), no tools, no MCP
servers and the haiku model. That directory is a fresh one in the system temp directory,
outside any checkout, and the session refuses to start if a CLAUDE.md or .claude/ sits
above it, so no project's instructions or hooks reach the child. The writer sends nothing
until the driver has checked that the writer's socket is the one the child itself logged,
and it stops by itself once that socket is gone, so the driver sends it an exit only if the
socket outlived the session.

The child authenticates as the owner's ccy sessions do: CLAUDE_CODE_OAUTH_TOKEN carries the
long-lived token that ccy last launched --checkout with (LAST_TOKEN in its
.claude/ccy/.last-launch.conf, the file ~/.claude-tokens/ccy/tokens/<name>.<expiry>.token),
refused when ccy would refuse it. The value only ever travels in the child's environment;
after each session every file in its run directory is checked for it, and any copy Claude
Code wrote is replaced with a placeholder and reported. The child still uses this user's
config directory, so afterwards `claude purge` removes the scratch project's transcript and
config entry, and any file named by the session's fresh UUID is removed.
"""

from __future__ import annotations

import argparse
import dataclasses
import datetime
import fnmatch
import json
import os
import pathlib
import re
import shlex
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import time
import uuid
from typing import Any

import triage_probe as tp

SCRIPT = pathlib.Path(__file__).resolve()
PLUGIN_NAME = "u01probe"
HOOK_EVENTS = ("SessionStart", "UserPromptSubmit", "Stop", "SessionEnd")
MODEL = "haiku"
SOCKET_ENV = "CLAUDE_CODE_MESSAGING_SOCKET"
TOKEN_ENV = "CLAUDE_CODE_MESSAGING_TOKEN"
# Variables that tie a process to a running session; none of them reaches the child.
LIVE_SESSION_ENV = frozenset(
    {
        SOCKET_ENV,
        TOKEN_ENV,
        "CLAUDECODE",
        "CLAUDE_CODE_SESSION_ID",
        "CLAUDE_CODE_ENTRYPOINT",
        "CLAUDE_CODE_CHILD_SESSION",
    }
)
OAUTH_ENV = "CLAUDE_CODE_OAUTH_TOKEN"
OAUTH_PLACEHOLDER = f"<{OAUTH_ENV}>"
# ccy's own rules (claude-yolo, lib/token-management.bash, lib/common-pure.bash).
_TOKEN_NAME_RE = re.compile(r"^[A-Za-z0-9_-]+$")
_TOKEN_EXPIRY_RE = re.compile(r"(\d{4}-\d{2}-\d{2})\.token$")
_LAST_TOKEN_RE = re.compile(r"""^LAST_TOKEN=(["']?)(.*)\1$""")
LAUNCH_HINT = "launch ccy in this checkout once (it records the token it used there)"
KEPT_HOOK_KEYS =("hook_event_name", "source", "session_id", "stop_hook_active", "reason")
INSTRUCTION_FILES = ("CLAUDE.md", "CLAUDE.local.md")
AUTH_KEYS =("loggedIn", "authMethod", "apiProvider", "projectsDirectory", "configDirectory")
FIRST_PROMPT = "Reply with the single word OK and nothing else."
PROBE_ASK = " (Claude Code probe: reply with the single word OK and nothing else.)"
START_TIMEOUT_S = 60
TURN_TIMEOUT_S = 120
QUIET_S = 15
EXIT_TIMEOUT_S = 60
SOCKET_GONE_TIMEOUT_S = 30
WRITER_DEADLINE_S = 900
REPLY_WAIT_S = 2.0
BATCH_GAP_S = 0.05
# An identical notice is resent this long after the first; Claude Code 2.1.291 ships a 30 s
# window, which its server-side flags can change, so it is measured, not assumed.
DEDUPE_OFFSETS_S = (20, 33)

_DEBUG_LINE_RE = re.compile(r"^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3})Z \[[A-Z]+\] (.*)$")
_ROUTED_RE = re.compile(r"\[uds-messaging\] Routed user message to queue \(priority=([\w-]+)\)")
_HELD_RE = re.compile(r"\[cross-session-inbound\] held inbound peer message \(\d+ held, cause=([\w-]+)\)")
_DROPPED_RE = re.compile(r"\[peer-guard\] drop ([\w-]+) from ")
_TURN_RE = re.compile(r"cc_turn_origin=([\w-]+)")
_LISTENING_RE = re.compile(r"\[uds-messaging\] Listening: (\S+)")
_OUTCOME_ORDER = ("dropped", "held", "routed")
_ANSI_RE = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")


@dataclasses.dataclass(frozen=True)
class Variant:
    bypass: bool
    accept: bool
    full: bool
    about: str


VARIANTS = {
    "main": Variant(
        True,
        True,
        True,
        "Bypass mode with `crossSessionInbound: accept`, as ccy runs: the hooks, a notice to the "
        "idle session, an identical repeat, two notices back to back, the dedupe window, the end.",
    ),
    "bypass-no-accept": Variant(True, False, False, "Bypass mode without the setting: one notice."),
    "default-accept": Variant(False, True, False, "Default permission mode with the setting: one notice."),
    "default-no-accept": Variant(False, False, False, "Default permission mode without the setting: one notice."),
}


# ── pure: frames, notices, the throwaway plugin, the child's argv and environment ─────────


def _line(obj: dict[str, Any]) -> bytes:
    return (json.dumps(obj, separators=(",", ":")) + "\n").encode("utf-8")


def auth_frame(token: str) -> bytes:
    if not token:
        raise ValueError("the auth frame needs a token")
    return _line({"type": "auth", "token": token})


def user_frame(text: str) -> bytes:
    if not text:
        raise ValueError("a user frame needs text")
    return _line({"type": "user", "message": {"role": "user", "content": text}})


def connection_payload(token: str, text: str) -> bytes:
    """What one connection to the inbox socket carries: the auth line first, then one message."""
    return auth_frame(token) + user_frame(text)


def wire_example(text: str) -> str:
    return (_line({"type": "auth", "token": "<CLAUDE_CODE_MESSAGING_TOKEN>"}) + user_frame(text)).decode("utf-8")


def notice_body(pending: int, humans: int, pings: int, seq: int) -> str:
    """DESIGN.md section 6's fixed notice template, plus a request for the cheapest reply."""
    return (
        f"agent-bus: {pending} pending ({humans} from humans, {pings} pings), notice {seq}. "
        f"Run `pingbus recv`.{PROBE_ASK}"
    )


def build_hooks(python: str, script: pathlib.Path, evidence: pathlib.Path) -> dict[str, Any]:
    def command(event: str) -> str:
        return " ".join([shlex.quote(python), shlex.quote(str(script)), "hook", shlex.quote(str(evidence)), event])

    return {"hooks": {event: [{"hooks": [{"type": "command", "command": command(event)}]}] for event in HOOK_EVENTS}}


def plugin_manifest() -> dict[str, str]:
    return {"name": PLUGIN_NAME, "version": "0.0.1", "description": "Throwaway Plan 00161 U01 probe"}


def build_settings(accept: bool) -> dict[str, str]:
    return {"crossSessionInbound": "accept"} if accept else {}


def build_argv(
    claude: str,
    session_id: str,
    settings: pathlib.Path,
    plugin_dir: pathlib.Path,
    debug_file: pathlib.Path,
    bypass: bool,
) -> list[str]:
    argv = [
        claude,
        "-p",
        "--input-format",
        "stream-json",
        "--output-format",
        "stream-json",
        "--verbose",
        "--model",
        MODEL,
        "--session-id",
        session_id,
        "--settings",
        str(settings),
        "--setting-sources",
        "project",
        "--plugin-dir",
        str(plugin_dir),
        "--strict-mcp-config",
        "--tools",
        "",
        "--debug-file",
        str(debug_file),
    ]
    if bypass:
        argv += ["--permission-mode", "bypassPermissions"]
    return argv


def child_env(environ: dict[str, str], oauth_token: str) -> dict[str, str]:
    """The child's environment: no live session's variables, and ccy's token as ccy passes it."""
    if not oauth_token:
        raise ValueError("the child needs the ccy token")
    env = {key: value for key, value in environ.items() if key not in LIVE_SESSION_ENV}
    env[OAUTH_ENV] = oauth_token
    return env


def instruction_ancestors(path: pathlib.Path) -> list[pathlib.Path]:
    """What Claude Code would load walking up from a working directory: memory files, .claude/."""
    found = []
    for directory in (path, *path.parents):
        found += [directory / name for name in INSTRUCTION_FILES if (directory / name).is_file()]
        if (directory / ".claude").is_dir():
            found.append(directory / ".claude")
    return found


def make_work_dir(variant: str) -> pathlib.Path:
    """The child's directory, in the system temp directory: never under a checkout's CLAUDE.md."""
    return pathlib.Path(tempfile.mkdtemp(prefix=f"u01-{variant}-")).resolve()


# ── pure: the ccy token the owner's sessions run on ───────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class CcyToken:
    name: str
    expires: str
    value: str = dataclasses.field(repr=False)

    def label(self) -> str:
        return f"ccy token {self.name} (expires {self.expires})"


def ccy_launch_conf(checkout: pathlib.Path) -> pathlib.Path:
    return checkout / ".claude" / "ccy" / ".last-launch.conf"


def ccy_tokens_dir(home: pathlib.Path) -> pathlib.Path:
    return home / ".claude-tokens" / "ccy" / "tokens"


def last_token_name(conf_text: str, conf: pathlib.Path) -> str:
    """LAST_TOKEN from ccy's launch record, parsed (never sourced); the last line wins, as in bash."""
    found = None
    for line in conf_text.splitlines():
        match = _LAST_TOKEN_RE.match(line.strip())
        if match is not None:
            found = match.group(2)
    if not found:
        raise tp.ProbeError(f"{conf} names no token (no LAST_TOKEN value): {LAUNCH_HINT}")
    if not _TOKEN_NAME_RE.match(found):
        raise tp.ProbeError(f"{conf} has a LAST_TOKEN that is not a plain token name: {LAUNCH_HINT}")
    return found


def pick_token_file(name: str, file_names: list[str]) -> str | None:
    """The file `ccy --token NAME` takes: the first match of <name>.*.token."""
    matches = sorted(n for n in file_names if fnmatch.fnmatchcase(n, f"{name}.*.token"))
    return matches[0] if matches else None


def token_expiry(file_name: str) -> str | None:
    match = _TOKEN_EXPIRY_RE.search(file_name)
    return None if match is None else match.group(1)


def token_usable(expiry: str, today: datetime.date) -> bool:
    """ccy's is_token_valid: a token expiring today is already expired."""
    return expiry > today.isoformat()


def token_value(content: str) -> str:
    """What ccy's `$(cat file)` yields: the content less its trailing newlines."""
    return content.rstrip("\n")


def redact_secret(root: pathlib.Path, secret: str) -> list[str]:
    """Every file under root that holds the secret, rewritten with a placeholder in its place."""
    needle, held = secret.encode("utf-8"), []
    for path in sorted(p for p in root.rglob("*") if p.is_file() and not p.is_symlink()):
        data = path.read_bytes()
        if needle in data:
            path.write_bytes(data.replace(needle, OAUTH_PLACEHOLDER.encode("utf-8")))
            held.append(str(path.relative_to(root)))
    return held


def load_ccy_token(checkout: pathlib.Path, home: pathlib.Path, today: datetime.date) -> CcyToken:
    """The token ccy last launched this checkout with, refused when ccy would refuse it."""
    conf = ccy_launch_conf(checkout)
    if not conf.is_file():
        raise tp.ProbeError(f"no ccy launch record at {conf}: {LAUNCH_HINT}")
    name = last_token_name(conf.read_text(encoding="utf-8"), conf)
    renew = f"run ccy --update-token={name}"
    tokens = ccy_tokens_dir(home)
    names = [p.name for p in tokens.iterdir()] if tokens.is_dir() else []
    file_name = pick_token_file(name, names)
    if file_name is None:
        raise tp.ProbeError(f"no file for ccy token {name} in {tokens}: {renew}")
    expiry = token_expiry(file_name)
    if expiry is None:
        raise tp.ProbeError(f"ccy token {name} ({file_name}) has no expiry date in its name, so ccy treats it as expired: {renew}")
    if not token_usable(expiry, today):
        raise tp.ProbeError(f"ccy token {name} expired {expiry} (ccy counts a token expiring today as expired): {renew}")
    value = token_value((tokens / file_name).read_text(encoding="utf-8"))
    if not value:
        raise tp.ProbeError(f"ccy token file {tokens / file_name} is empty: {renew}")
    return CcyToken(name, expiry, value)


# ── pure: reading the child's debug log, stream output and transcript ─────────────────────


def parse_debug_line(line: str) -> tuple[float, str] | None:
    match = _DEBUG_LINE_RE.match(line)
    if match is None:
        return None
    stamp = datetime.datetime.strptime(match.group(1), "%Y-%m-%dT%H:%M:%S.%f")
    return stamp.replace(tzinfo=datetime.UTC).timestamp(), match.group(2)


def classify_inbox(text: str) -> tuple[str, str] | None:
    for kind, pattern in (("routed", _ROUTED_RE), ("held", _HELD_RE), ("dropped", _DROPPED_RE), ("turn", _TURN_RE)):
        match = pattern.search(text)
        if match is not None:
            return kind, match.group(1)
    return None


def listening_socket(debug_text: str) -> str | None:
    match = _LISTENING_RE.search(debug_text)
    return None if match is None else match.group(1)


def inbox_events(debug_text: str) -> list[tuple[float, str, str]]:
    events = []
    for line in debug_text.splitlines():
        parsed = parse_debug_line(line)
        if parsed is None:
            continue
        found = classify_inbox(parsed[1])
        if found is not None:
            events.append((parsed[0], found[0], found[1]))
    return events


def pair_outcomes(sends: list[tuple[float, str]], events: list[tuple[float, str, str]]) -> list[dict[str, Any]]:
    """What the inbox did with each send: the events up to the next send belong to it."""
    rows = []
    for index, (start, label) in enumerate(sends):
        end = sends[index + 1][0] if index + 1 < len(sends) else float("inf")
        mine = [(kind, detail) for t, kind, detail in events if start <= t < end]
        outcome, detail = "no-inbox-event", ""
        for wanted in _OUTCOME_ORDER:
            hit = [d for kind, d in mine if kind == wanted]
            if hit:
                outcome, detail = wanted, hit[0]
                break
        rows.append(
            {"label": label, "outcome": outcome, "detail": detail, "peer_turn": ("turn", "peer") in mine}
        )
    return rows


def dedupe_window(resends: list[tuple[float, str]]) -> str:
    """Bounds on the window from identical resends at increasing offsets after an admitted send."""
    longer_than = None
    for offset, outcome in resends:
        if outcome == "routed":
            low = "" if longer_than is None else f"longer than {longer_than:g} s, "
            return f"{low}at most {offset:g} s"
        if outcome == "no-inbox-event":
            return "not measured (no inbox event for an identical resend)"
        if outcome != "dropped":
            return f"not measured (an identical resend was {outcome}, not judged)"
        longer_than = offset
    return "not measured (no identical resend)" if longer_than is None else f"longer than {longer_than:g} s"


def _json_or_none(text: str) -> Any:
    try:
        return json.loads(text)
    except ValueError:
        return None


def result_times(events: list[tuple[float, dict[str, Any]]]) -> list[float]:
    return [t for t, obj in events if obj.get("type") == "result"]


def hook_responses(events: list[tuple[float, dict[str, Any]]]) -> list[tuple[str, Any]]:
    return [
        (obj.get("hook_event", ""), obj.get("exit_code"))
        for _t, obj in events
        if obj.get("type") == "system" and obj.get("subtype") == "hook_response"
    ]


def _content_text(content: Any) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "\n".join(
            block.get("text", "") for block in content if isinstance(block, dict) and block.get("type") == "text"
        )
    return ""


def peer_entries(lines: list[str]) -> list[str]:
    """The text of every transcript entry that arrived from another session."""
    found = []
    for line in lines:
        obj = _json_or_none(line)
        if not isinstance(obj, dict) or obj.get("type") != "user":
            continue
        origin = obj.get("origin")
        if not isinstance(origin, dict) or origin.get("kind") != "peer":
            continue
        message = obj.get("message")
        found.append(_content_text(message.get("content") if isinstance(message, dict) else None))
    return found


def framing(content: str, body: str) -> tuple[str, str] | None:
    index = content.find(body)
    if index < 0:
        return None
    return content[:index], content[index + len(body) :]


def batching(entries: list[str], bodies: list[str]) -> str:
    holders = {body: [i for i, entry in enumerate(entries) if body in entry] for body in bodies}
    missing = [body for body in bodies if not holders[body]]
    if missing:
        return "missing:" + ",".join(missing)
    shared = set.intersection(*(set(found) for found in holders.values()))
    return "one-entry" if shared else "separate-entries"


# ── pure: hook marks, writer commands, login status ───────────────────────────────────────


def mark_record(event: str, raw_input: str, environ: dict[str, str], t: float) -> dict[str, Any]:
    parsed = _json_or_none(raw_input)
    if isinstance(parsed, dict):
        kept: dict[str, Any] = {key: parsed[key] for key in KEPT_HOOK_KEYS if key in parsed}
    else:
        kept = {"unparseable": True}
    return {
        "event": event,
        "t": t,
        "input": kept,
        "socket": environ.get(SOCKET_ENV, ""),
        "token_present": bool(environ.get(TOKEN_ENV)),
    }


def hook_counts(marks: pathlib.Path) -> dict[str, int]:
    counts = {}
    for event in HOOK_EVENTS:
        path = marks / f"{event}.jsonl"
        counts[event] = len(path.read_text(encoding="utf-8").splitlines()) if path.exists() else 0
    return counts


def _number(value: Any, name: str, minimum: float, strict: bool) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError(f"{name} must be a number")
    if value < minimum or (strict and value == minimum):
        raise ValueError(f"{name} is out of range: {value}")
    return value


def parse_command(obj: Any) -> dict[str, Any]:
    if not isinstance(obj, dict):
        raise ValueError("a writer command is a JSON object")
    op = obj.get("op")
    if op == "exit":
        return {"op": "exit"}
    if op == "send":
        bodies = obj.get("bodies")
        if not isinstance(bodies, list) or not bodies or not all(isinstance(b, str) and b for b in bodies):
            raise ValueError("send needs a non-empty list of non-empty bodies")
        return {"op": "send", "bodies": bodies, "gap_s": _number(obj.get("gap_s", BATCH_GAP_S), "gap_s", 0, False)}
    if op == "watch-socket":
        return {"op": "watch-socket", "timeout_s": _number(obj.get("timeout_s"), "timeout_s", 0, True)}
    raise ValueError(f"unknown writer command: {op!r}")


def parse_auth_status(text: str) -> dict[str, Any]:
    parsed = json.loads(text)
    if not isinstance(parsed, dict):
        raise ValueError("claude auth status did not print a JSON object")
    return {key: parsed[key] for key in AUTH_KEYS if key in parsed}


# ── side effects: the socket writer and the hooks ─────────────────────────────────────────


def send_notice(path: str, token: str, body: str, reply_wait_s: float) -> dict[str, Any]:
    """One connection, as `pingbus watch` will make it: auth, one message, half-close, read."""
    record: dict[str, Any] = {"body": body, "t": time.time(), "reply": "", "error": ""}
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as conn:
            conn.settimeout(10)
            conn.connect(path)
            conn.sendall(connection_payload(token, body))
            conn.shutdown(socket.SHUT_WR)
            conn.settimeout(reply_wait_s)
            reply = b""
            try:
                while chunk := conn.recv(4096):
                    reply += chunk
            except TimeoutError:
                record["reply_timed_out"] = True
            record["reply"] = reply.decode("utf-8", errors="replace")[:500]
    except OSError as error:
        record["error"] = f"{type(error).__name__}: {error}"
    return record


def write_json(path: pathlib.Path, obj: Any) -> None:
    """Atomic, so the other side of the command-file exchange never reads half a file."""
    partial = path.with_name(path.name + ".partial")
    partial.write_text(json.dumps(obj, indent=1) + "\n", encoding="utf-8")
    os.replace(partial, path)


def _read_json(path: pathlib.Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8")) if path.exists() else None


def writer_needs_exit(watch_result: dict[str, Any]) -> bool:
    """Whether the writer still waits for an exit: once its socket is gone it stops by itself."""
    return not watch_result["gone"]


def writer_main(evidence: pathlib.Path) -> int:
    """The hook-spawned stand-in for `pingbus watch`: sends only what the driver asks."""
    wdir = evidence / "writer"
    path = os.environ.get(SOCKET_ENV, "")
    token = os.environ.get(TOKEN_ENV, "")
    write_json(wdir / "ready.json", {"pid": os.getpid(), "socket": path, "token_present": bool(token)})
    deadline = time.monotonic() + WRITER_DEADLINE_S
    number = 1
    while time.monotonic() < deadline:
        cmd_path = wdir / f"cmd-{number}.json"
        if not cmd_path.exists():
            if not os.path.exists(path):
                # The session is gone (or a killed driver never said exit): stop, as
                # `pingbus watch` does (DESIGN.md section 6). The driver's watch-socket
                # command is written before the session closes, so it is seen first.
                write_json(wdir / "socket-gone.json", {"t": time.time()})
                return 0
            time.sleep(0.05)
            continue
        cmd = parse_command(_read_json(cmd_path))
        if cmd["op"] == "exit":
            write_json(wdir / f"done-{number}.json", {"exited": True})
            return 0
        if cmd["op"] == "send":
            sends = []
            for index, body in enumerate(cmd["bodies"]):
                if index:
                    time.sleep(cmd["gap_s"])
                sends.append(send_notice(path, token, body, REPLY_WAIT_S))
            result: dict[str, Any] = {"sends": sends}
        else:
            stop = time.monotonic() + cmd["timeout_s"]
            while os.path.exists(path) and time.monotonic() < stop:
                time.sleep(0.1)
            gone = not os.path.exists(path)
            result = {"gone": gone, "t": time.time() if gone else None}
        write_json(wdir / f"done-{number}.json", result)
        number += 1
    write_json(wdir / "deadline.json", {"t": time.time()})
    return 1


def hook_main(evidence: pathlib.Path, event: str) -> int:
    record = mark_record(event, sys.stdin.read(), dict(os.environ), time.time())
    with (evidence / "marks" / f"{event}.jsonl").open("a", encoding="utf-8") as marks:
        marks.write(json.dumps(record) + "\n")
    if event != "SessionStart":
        return 0
    try:
        (evidence / "writer").mkdir()
    except FileExistsError:
        return 0  # a second SessionStart (resume, clear) keeps the first writer
    with (evidence / "writer.stderr").open("ab") as err:
        subprocess.Popen(  # detached on purpose: it outlives this hook, as `pingbus watch` will
            [sys.executable, str(SCRIPT), "writer", str(evidence)],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=err,
            start_new_session=True,
            close_fds=True,
            cwd=str(evidence),
        )
    return 0


# ── side effects: the driver ──────────────────────────────────────────────────────────────


def _read_text(path: pathlib.Path) -> str:
    return path.read_text(encoding="utf-8", errors="replace") if path.exists() else ""


def claude_version(claude: str) -> str:
    return tp.run_ok([claude, "--version"], timeout=60).strip()


def claude_auth(claude: str, env: dict[str, str]) -> dict[str, Any]:
    """`claude auth status` as the child would see it: where its config lives, how it authenticates.

    It exits 1 when logged out; its JSON is the fact either way.
    """
    result = subprocess.run(
        [claude, "auth", "status", "--json"], check=False, capture_output=True, text=True, timeout=60, env=env
    )
    try:
        return parse_auth_status(result.stdout)
    except ValueError as error:
        raise tp.ProbeError(f"claude auth status (exit {result.returncode}) printed no JSON: {error}") from error


class Session:
    """One throwaway child session and its hook-spawned writer."""

    def __init__(
        self, claude: str, variant: Variant, evidence: pathlib.Path, work: pathlib.Path, oauth_token: str
    ) -> None:
        self.variant = variant
        self.oauth_token = oauth_token
        self.evidence = evidence
        self.work = work
        self.session_id = str(uuid.uuid4())
        self.cwd = work / "cwd"
        self.plugin = work / "plugin" / PLUGIN_NAME
        self.marks = evidence / "marks"
        self.writer_dir = evidence / "writer"
        self.debug_file = evidence / "debug.log"
        self.argv = build_argv(
            claude, self.session_id, evidence / "settings.json", self.plugin, self.debug_file, variant.bypass
        )
        self.events: list[tuple[float, dict[str, Any]]] = []
        self.sends: list[tuple[float, str]] = []
        self.lock = threading.Lock()
        self.proc: subprocess.Popen[str] | None = None
        self.stderr: Any = None
        self.reader: threading.Thread | None = None
        self.commands = 0
        self.exit_at: float | None = None

    def start(self) -> None:
        loaded = instruction_ancestors(self.cwd.parent)
        if loaded:
            raise tp.ProbeError(
                f"the child's directory {self.cwd} sits below {', '.join(map(str, loaded))}, which Claude Code "
                "would load into every turn; nothing was started"
            )
        self.evidence.mkdir(parents=True)
        self.marks.mkdir()
        self.cwd.mkdir(parents=True)
        (self.plugin / ".claude-plugin").mkdir(parents=True)
        (self.plugin / "hooks").mkdir()
        write_json(self.plugin / ".claude-plugin" / "plugin.json", plugin_manifest())
        write_json(self.plugin / "hooks" / "hooks.json", build_hooks(sys.executable, SCRIPT, self.evidence))
        write_json(self.evidence / "settings.json", build_settings(self.variant.accept))
        write_json(self.evidence / "argv.json", self.argv)
        self.stderr = (self.evidence / "child.stderr").open("w", encoding="utf-8")
        self.proc = subprocess.Popen(
            self.argv,
            cwd=str(self.cwd),
            env=child_env(dict(os.environ), self.oauth_token),
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=self.stderr,
            text=True,
        )
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()

    def _read(self) -> None:
        assert self.proc is not None and self.proc.stdout is not None
        with (self.evidence / "stream.jsonl").open("w", encoding="utf-8") as out:
            for line in self.proc.stdout:
                out.write(line)
                out.flush()
                obj = _json_or_none(line)
                if not isinstance(obj, dict):
                    obj = {"unparsed": line.strip()[:500]}
                with self.lock:
                    self.events.append((time.time(), obj))

    def snapshot(self) -> list[tuple[float, dict[str, Any]]]:
        with self.lock:
            return list(self.events)

    def results(self) -> list[float]:
        return result_times(self.snapshot())

    def alive(self, what: str) -> None:
        assert self.proc is not None
        if self.proc.poll() is not None:
            raise tp.ProbeError(f"the child claude exited ({self.proc.returncode}) during {what}")

    def wait_until(self, check: Any, timeout: float, what: str) -> Any:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            value = check()
            if value:
                return value
            self.alive(what)
            time.sleep(0.1)
        raise tp.ProbeError(f"timed out after {timeout} s waiting for {what}")

    def await_ready(self) -> dict[str, Any]:
        self.wait_until((self.marks / "SessionStart.jsonl").exists, START_TIMEOUT_S, "the SessionStart hook")
        ready = self.wait_until(lambda: _read_json(self.writer_dir / "ready.json"), START_TIMEOUT_S, "the writer")
        listening = self.wait_until(
            lambda: listening_socket(_read_text(self.debug_file)), START_TIMEOUT_S, "the inbox socket in the debug log"
        )
        if not ready["socket"]:
            raise tp.ProbeError(f"the hook-spawned writer saw no {SOCKET_ENV}; the inbox is not reachable from hooks")
        if ready["socket"] != listening:
            raise tp.ProbeError(
                f"the writer's socket {ready['socket']} is not the one the child logged ({listening}); nothing was sent"
            )
        return {"socket": ready["socket"], "token_present": ready["token_present"], "listening": listening}

    def issue(self, cmd: dict[str, Any]) -> int:
        parse_command(cmd)
        self.commands += 1
        write_json(self.writer_dir / f"cmd-{self.commands}.json", cmd)
        return self.commands

    def await_done(self, number: int, timeout: float) -> dict[str, Any]:
        path = self.writer_dir / f"done-{number}.json"
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            done = _read_json(path)
            if done is not None:
                return done
            time.sleep(0.05)
        raise tp.ProbeError(f"the writer did not finish command {number} within {timeout} s")

    def send(self, bodies: list[str], labels: list[str]) -> list[dict[str, Any]]:
        self.alive("a send")
        done = self.await_done(self.issue({"op": "send", "bodies": bodies, "gap_s": BATCH_GAP_S}), 30)
        for label, record in zip(labels, done["sends"], strict=True):
            self.sends.append((record["t"], label))
            if record["error"]:
                raise tp.ProbeError(f"the writer could not send ({label}): {record['error']}")
        return done["sends"]

    def prompt(self, text: str) -> float:
        assert self.proc is not None and self.proc.stdin is not None
        sent = time.time()
        self.proc.stdin.write(json.dumps({"type": "user", "message": {"role": "user", "content": text}}) + "\n")
        self.proc.stdin.flush()
        return sent

    def await_turn(self, before: int, timeout: float, what: str) -> float | None:
        """The time the next turn ended, or None if none ended within the timeout."""
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            times = self.results()
            if len(times) > before:
                return times[before]
            self.alive(what)
            time.sleep(0.1)
        return None

    def settle(self, quiet_s: float, limit_s: float, what: str) -> None:
        """Wait until no turn has ended for quiet_s (at most limit_s in all)."""
        start = time.monotonic()
        seen, since = len(self.results()), time.monotonic()
        while time.monotonic() - start < limit_s and time.monotonic() - since < quiet_s:
            self.alive(what)
            time.sleep(0.2)
            now = len(self.results())
            if now != seen:
                seen, since = now, time.monotonic()

    def stop(self, facts: dict[str, Any]) -> list[str]:
        errors = []
        watch = self.issue({"op": "watch-socket", "timeout_s": SOCKET_GONE_TIMEOUT_S}) if self.writer_ready() else None
        if self.proc is not None:
            if self.proc.poll() is None:
                assert self.proc.stdin is not None
                self.proc.stdin.close()
                try:
                    self.proc.wait(timeout=EXIT_TIMEOUT_S)
                except subprocess.TimeoutExpired:
                    errors.append(f"the child did not exit within {EXIT_TIMEOUT_S} s of its input closing; killed")
                    self.proc.kill()
                    self.proc.wait(timeout=10)
            self.exit_at = time.time()
            facts["exit_code"] = self.proc.returncode
            if self.reader is not None:
                self.reader.join(timeout=10)
        if watch is not None:
            try:
                facts["socket_after_exit"] = self.await_done(watch, SOCKET_GONE_TIMEOUT_S + 10)
                if writer_needs_exit(facts["socket_after_exit"]):
                    self.await_done(self.issue({"op": "exit"}), 10)
            except tp.ProbeError as error:
                errors.append(str(error))
        if self.stderr is not None:
            self.stderr.close()
        return errors

    def writer_ready(self) -> bool:
        """A writer that is still waiting for commands (it stops by itself once its socket is gone)."""
        ready = _read_json(self.writer_dir / "ready.json")
        return bool(ready and ready["socket"]) and not (self.writer_dir / "socket-gone.json").exists()


def remove_traces(claude: str, auth: dict[str, Any], session: Session, facts: dict[str, Any]) -> list[str]:
    """Keep the transcript as evidence, then remove every trace of the throwaway session."""
    errors = []
    projects = pathlib.Path(auth["projectsDirectory"])
    matches = sorted(projects.glob(f"*/{session.session_id}.jsonl"))
    if matches:
        shutil.copyfile(matches[0], session.evidence / "transcript.jsonl")
    facts["transcript_found"] = bool(matches)
    # A session that never took a turn leaves no project state, and purge exits 1 on that;
    # the dry run tells the two apart and its words are kept in the report either way.
    dry = tp.run([claude, "purge", "--dry-run", str(session.cwd)], timeout=120)
    if matches or dry.returncode == 0:
        purge = tp.run([claude, "purge", "-y", str(session.cwd)], timeout=120)
        (session.evidence / "purge.out").write_text(purge.stdout + purge.stderr, encoding="utf-8")
        facts["purge"] = f"exit {purge.returncode}"
        if purge.returncode != 0:
            errors.append(f"claude purge exited {purge.returncode} (see purge.out)")
    else:
        said = _ANSI_RE.sub("", dry.stdout + dry.stderr).strip().splitlines()
        facts["purge"] = "not needed, the dry run (exit {}) said: {}".format(
            dry.returncode, said[0] if said else "nothing"
        )
    config = pathlib.Path(auth["configDirectory"])
    removed = []
    for pattern in (f"*/{session.session_id}*", f"*/*/{session.session_id}*"):
        for path in sorted(config.glob(pattern)):
            if path.is_dir() and not path.is_symlink():
                shutil.rmtree(path)
            else:
                path.unlink()
            removed.append(path.relative_to(config).parts[0] + "/…/<session-id>")
    facts["removed_after_purge"] = removed
    if session.work.exists():
        shutil.rmtree(session.work)
    return errors


def run_main(session: Session, facts: dict[str, Any]) -> None:
    before = len(session.results())
    sent = session.prompt(FIRST_PROMPT)
    ended = session.await_turn(before, TURN_TIMEOUT_S, "the typed turn")
    if ended is None:
        raise tp.ProbeError(f"the typed turn did not end within {TURN_TIMEOUT_S} s")
    facts["typed_turn_s"] = round(ended - sent, 1)
    session.settle(2, 10, "the typed turn's end")

    notices = {seq: notice_body(seq, 0, seq, seq) for seq in (1, 2, 3, 4)}
    before = len(session.results())
    record = session.send([notices[1]], ["notice 1 to the idle session"])[0]
    ended = session.await_turn(before, TURN_TIMEOUT_S, "a turn after notice 1")
    facts["wake_turn_s"] = None if ended is None else round(ended - record["t"], 1)
    facts["wire_reply"] = record["reply"]
    session.settle(2, TURN_TIMEOUT_S, "the woken turn's end")

    before = len(session.results())
    session.send([notices[1]], ["notice 1 again, identical"])
    session.settle(QUIET_S, TURN_TIMEOUT_S, "after the identical repeat")
    facts["repeat_turns"] = len(session.results()) - before

    before = len(session.results())
    session.send([notices[2], notices[3]], ["notice 2, then", f"notice 3 {BATCH_GAP_S:g} s later"])
    session.settle(QUIET_S, TURN_TIMEOUT_S, "after two notices back to back")
    facts["batch_turns"] = len(session.results()) - before
    facts["batch_bodies"] = [notices[2], notices[3]]

    first = session.send([notices[4]], ["notice 4"])[0]
    labels = []
    for offset in DEDUPE_OFFSETS_S:
        delay = first["t"] + offset - time.time()
        if delay > 0:
            time.sleep(delay)
        label = f"notice 4 again, +{offset} s"
        session.send([notices[4]], [label])
        labels.append((offset, label))
    session.settle(QUIET_S, TURN_TIMEOUT_S, "after the dedupe-window resends")
    facts["window_labels"] = labels
    facts["framing_body"] = notices[1]


def run_single(session: Session, facts: dict[str, Any]) -> None:
    session.settle(2, 10, "startup")
    body = notice_body(1, 0, 1, 1)
    before = len(session.results())
    session.send([body], ["notice 1 to the idle session"])
    session.settle(QUIET_S + 5, TURN_TIMEOUT_S, "after one notice")
    facts["single_turns"] = len(session.results()) - before
    facts["framing_body"] = body


def _fence(text: str) -> str:
    return "```\n" + text.rstrip("\n") + "\n```"


def render_session(name: str, session: Session, facts: dict[str, Any], errors: list[str]) -> str:
    variant = session.variant
    out = [f"## U01 session: {name}", "", variant.about, ""]
    out.append(f"- Claude Code: {facts.get('version', 'unknown')}")
    out.append(f"- auth: {facts.get('auth', 'unknown')}, as {OAUTH_ENV}")
    out.append(f"- permission mode: {'bypassPermissions' if variant.bypass else 'default'}; settings: "
               f"`{json.dumps(build_settings(variant.accept))}` through --settings; plugin through --plugin-dir")
    ready = facts.get("ready")
    if ready:
        out.append(f"- the SessionStart hook's environment carried {SOCKET_ENV} (the socket the child logged) and "
                   f"{'a' if ready['token_present'] else 'no'} {TOKEN_ENV}; the writer it started kept running detached")
    out.append(f"- exit code after its input closed: {facts.get('exit_code', 'n/a')}")
    gone = facts.get("socket_after_exit")
    if gone is not None and session.exit_at is not None:
        out.append("- the socket file after the session ended: "
                   + (f"removed (seen by the writer {gone['t'] - session.exit_at:+.1f} s from the exit)"
                      if gone.get("gone") else f"still present after {SOCKET_GONE_TIMEOUT_S} s"))
    out += ["", "Wire format, one connection per notice (newline-delimited JSON, auth line first):", ""]
    out += [_fence(wire_example(notice_body(1, 0, 1, 1))), ""]
    if "wire_reply" in facts:
        out += [f"The socket's reply to a notice: {facts['wire_reply']!r} (it closes after the client half-closes).", ""]

    counts = hook_counts(session.marks)
    rows = []
    for event in HOOK_EVENTS:
        path = session.marks / f"{event}.jsonl"
        first = _json_or_none(path.read_text(encoding="utf-8").splitlines()[0]) if counts[event] else None
        rows.append([event, counts[event], json.dumps(first["input"]) if isinstance(first, dict) else ""])
    out += ["Hooks of the --plugin-dir plugin (times fired; first input, selected keys):", ""]
    out += [*tp.table_lines(["event", "fired", "first input"], rows), ""]

    debug = _read_text(session.debug_file)
    outcomes = pair_outcomes(session.sends, inbox_events(debug))
    out += ["What the inbox did with each send (from the child's debug log):", ""]
    out += tp.table_lines(
        ["send", "outcome", "detail", "peer turn logged before the next send"],
        [[o["label"], o["outcome"], o["detail"], "yes" if o["peer_turn"] else "no"] for o in outcomes],
    )
    out.append("")
    for key, text in (
        ("typed_turn_s", "typed turn ended after {} s"),
        ("wake_turn_s", "turn after notice 1 to the idle session ended after {} s (None: no turn)"),
        ("repeat_turns", "turns after the identical repeat: {}"),
        ("batch_turns", "turns after two notices back to back: {}"),
        ("single_turns", "turns after the notice: {}"),
    ):
        if key in facts:
            out.append("- " + text.format(facts[key]))
    if "window_labels" in facts:
        by_label = {o["label"]: o["outcome"] for o in outcomes}
        resends = [(offset, by_label.get(label, "no-inbox-event")) for offset, label in facts["window_labels"]]
        out.append(f"- dedupe window (identical body, same sender): {dedupe_window(resends)}")
    hooks_in_stream = hook_responses(session.snapshot())
    if hooks_in_stream:
        out.append("- hook responses in the stream: " + ", ".join(f"{e} exit {c}" for e, c in hooks_in_stream))
    out.append("")

    transcript = _read_text(session.evidence / "transcript.jsonl").splitlines()
    entries = peer_entries(transcript)
    out.append(f"Transcript: {'kept' if facts.get('transcript_found') else 'not found'}; "
               f"entries from another session: {len(entries)}; their first lines: "
               f"{sorted({entry.splitlines()[0] if entry else '' for entry in entries})}")
    body = facts.get("framing_body")
    split = next((framing(entry, body) for entry in entries if body and body in entry), None)
    if split is not None:
        out += ["", "How a notice is framed to the model (text before the notice):", "", _fence(split[0]),
                "", "and after it:", "", _fence(split[1])]
    if "batch_bodies" in facts:
        out += ["", f"Two notices back to back reached the model as: {batching(entries, facts['batch_bodies'])}"]
    out += ["", f"Cleanup: claude purge {facts.get('purge', 'not run')}; "
            f"removed after it: {facts.get('removed_after_purge', [])}"]
    if errors:
        out += ["", "**Facts not established:** " + "; ".join(errors)]
    return "\n".join(out)


def ccy_token_for(args: argparse.Namespace) -> CcyToken:
    return load_ccy_token(pathlib.Path(args.checkout), pathlib.Path.home(), datetime.date.today())


def leg_claude_env(args: argparse.Namespace, report: tp.Report) -> None:
    version = claude_version(args.claude)
    token = ccy_token_for(args)
    auth = claude_auth(args.claude, child_env(dict(os.environ), token.value))
    report.table(
        ["fact", "value"],
        [["Claude Code", version], ["auth the child sessions use", f"{token.label()}, as {OAUTH_ENV}"],
         ["auth method Claude Code reports with it", auth.get("authMethod")],
         ["API provider", auth.get("apiProvider")]],
    )


def leg_session(args: argparse.Namespace, report: tp.Report) -> None:
    variant = VARIANTS[args.variant]
    token = ccy_token_for(args)
    auth = claude_auth(args.claude, child_env(dict(os.environ), token.value))
    facts: dict[str, Any] = {"version": claude_version(args.claude), "auth": token.label()}
    evidence = pathlib.Path(args.evidence) / args.variant
    session = Session(args.claude, variant, evidence, make_work_dir(args.variant), token.value)
    errors: list[str] = []
    tp.say(f"[U01] session {args.variant}: {variant.about}")
    try:
        try:
            session.start()
            facts["ready"] = session.await_ready()
            (run_main if variant.full else run_single)(session, facts)
        except tp.ProbeError as error:
            errors.append(str(error))
    finally:
        errors += session.stop(facts)
        errors += remove_traces(args.claude, auth, session, facts)
        if evidence.is_dir():
            held = redact_secret(evidence, token.value)
            if held:
                errors.append(f"Claude Code wrote the ccy token's value into {', '.join(held)}; "
                              f"it was replaced there with {OAUTH_PLACEHOLDER}")
        report.write(render_session(args.variant, session, facts, errors))
    if errors:
        raise tp.ProbeError("; ".join(errors))


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="u01_probe.py")
    sub = parser.add_subparsers(dest="command", required=True)
    env = sub.add_parser("claude-env")
    session = sub.add_parser("session")
    for leg in (env, session):
        leg.add_argument("--report", required=True)
        leg.add_argument("--claude", default="claude")
        leg.add_argument("--checkout", required=True, help="the checkout whose ccy launch record names the token")
    session.add_argument("--variant", required=True, choices=sorted(VARIANTS))
    session.add_argument("--evidence", required=True)
    hook = sub.add_parser("hook")
    hook.add_argument("evidence")
    hook.add_argument("event", choices=HOOK_EVENTS)
    writer = sub.add_parser("writer")
    writer.add_argument("evidence")
    args = parser.parse_args(argv)
    if args.command == "hook":
        return hook_main(pathlib.Path(args.evidence), args.event)
    if args.command == "writer":
        return writer_main(pathlib.Path(args.evidence))
    if shutil.which(args.claude) is None:
        parser.error(f"{args.claude} is not on PATH")
    if getattr(args, "evidence", ""):
        args.evidence = str(pathlib.Path(args.evidence).resolve())
    report = tp.Report(pathlib.Path(args.report))
    legs = {"claude-env": leg_claude_env, "session": leg_session}
    try:
        legs[args.command](args, report)
    except (tp.ProbeError, ValueError, OSError, KeyError, subprocess.SubprocessError) as error:
        tp.say(f"[FAIL] U01 {args.command}: {type(error).__name__}: {error}")
        report.write(f"**U01 {args.command} could not establish its facts:** {type(error).__name__}: {error}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
