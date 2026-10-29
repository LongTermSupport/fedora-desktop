#!/usr/bin/env python3
"""Plan 00201, ACCEPTANCE-owner.md step 08: the judgements behind acceptance-consent.bash.

The script drives `cc --teams` in a private tmux, answers its Element consent No and then Yes,
and reads the pane (`tmux capture-pane -p -J`), `agent-bus seat choose` and `seat list`,
`agent-bus list` and the consent record; this module judges what it read. The words it waits
for are read from this checkout's product (the chooser's consent.go, the token chooser in
token-management.bash), never copied, so a change of wording fails these tests rather than
leaving a host run waiting for a screen that never comes. sudo's prompt is sudo's own.
Standard library only. Commands (stdout is the payload, reasons on stderr):

  screen PANE                       what the pane shows last: consent, declined, tokens, sudo or none
  expect-consent PANE HANDLE...     the consent question names exactly these handles, in order,
                                    and its words before it name them too
  default-seat CHOOSE_OUT           `agent-bus seat choose --type=host`'s default seat and its
                                    handle, tab-separated; it must be a new one (a fresh checkout)
  expect-not-member LIST_OUT HANDLE   `agent-bus list TEAM` has no member of that handle
  seats-held SEAT_LIST SEAT@TEAM=HANDLE...   each seat held in `agent-bus seat list`, with that
                                    handle; 3 while one is absent or free
  expect-record PATH UID SINCE      the consent record: a plain file of UID's, owner-only,
                                    written at or after SINCE (epoch seconds)

Exit codes: 0 ok; 1 an expectation failed or the input was malformed; 3 not there yet;
64 usage.
"""

from __future__ import annotations

import os
import pathlib
import re
import stat
import sys
from collections.abc import Callable, Sequence

EXIT_OK = 0
EXIT_FAIL = 1
EXIT_NOT_YET = 3
EXIT_USAGE = 64

CONSENT_GO = "src-go/internal/teams/chooser/consent.go"
TOKENS_BASH = "files/var/local/claude-yolo/lib/token-management.bash"
#: sudo's own prompt, which no file here owns: sudo's default `[sudo] password for %p: `, and
#: sudo-rs's `[sudo: authenticate] Password: `.
SUDO_PROMPT_RE = re.compile(r"\[sudo\] password for [^\n:]*:|\[sudo: [^\]\n]*\] Password:")

#: Each product text: the file it is in and the pattern whose group is the text.
_TEXTS: dict[str, tuple[str, re.Pattern[str]]] = {
    "question": (CONSENT_GO, re.compile(r'"(Accept, and start )"\s*\+\s*who')),
    "declined": (CONSENT_GO, re.compile(r'c\.say\("(Not started\. Nothing was recorded;[^"]*)"\)')),
    "opening": (CONSENT_GO, re.compile(r'"\s(join their teams[^",]*)')),
    "tokens": (TOKENS_BASH, re.compile(r'echo "(Claude Code Token Selection for cc)"')),
}


class Failed(Exception):
    """An expectation did not hold; the message says what was seen."""


def _checkout_root() -> pathlib.Path:
    """This checkout's top: the first directory above this file holding ansible.cfg."""
    here = pathlib.Path(__file__).resolve().parent
    for candidate in (here, *here.parents):
        if (candidate / "ansible.cfg").exists():
            return candidate
    raise Failed(f"no ansible.cfg above {here}: not inside a fedora-desktop checkout")


def product_texts(consent_go: str | None = None, tokens_bash: str | None = None) -> dict[str, str]:
    """The texts the pane is read for, each found exactly once in its source file."""
    sources: dict[str, str] = {}
    if consent_go is not None:
        sources[CONSENT_GO] = consent_go
    if tokens_bash is not None:
        sources[TOKENS_BASH] = tokens_bash
    found: dict[str, str] = {}
    for name, (rel, pattern) in _TEXTS.items():
        if rel not in sources:
            sources[rel] = (_checkout_root() / rel).read_text(encoding="utf-8")
        hits = pattern.findall(sources[rel])
        if len(set(hits)) != 1:
            raise Failed(f"want exactly one {name} text in {rel} (pattern {pattern.pattern}), found {hits!r}")
        found[name] = hits[0]
    return found


def _flat(text: str) -> str:
    return re.sub(r"[ \t]+", " ", text)


def screen(pane: str, texts: dict[str, str] | None = None) -> str:
    """Which known screen appears last in the pane's text (its history included): the consent
    question, the decline, the token chooser, a sudo password prompt; none of them is none."""
    t = product_texts() if texts is None else texts
    at = {
        "consent": pane.rfind(t["question"]),
        "declined": pane.rfind(t["declined"]),
        "tokens": pane.rfind(t["tokens"]),
        "sudo": max((m.start() for m in SUDO_PROMPT_RE.finditer(pane)), default=-1),
    }
    last = max(at, key=lambda name: at[name])
    return last if at[last] >= 0 else "none"


def expect_consent(pane: str, handles: Sequence[str], texts: dict[str, str] | None = None) -> None:
    """The last consent question names exactly `handles`, in order, and the words printed
    before it say that they join their teams."""
    t = product_texts() if texts is None else texts
    flat = _flat(pane)
    asked = re.findall(re.escape(t["question"]) + r"(.+?)\?", flat)
    if not asked:
        raise Failed(f"no consent question ({t['question']!r}) on the screen")
    named = [name.strip() for name in asked[-1].split(",")]
    if named != list(handles):
        raise Failed(f"the consent question names {named!r}, want {list(handles)!r}")
    words = ", ".join(handles) + " " + t["opening"]
    if words not in flat:
        raise Failed(f"the consent's words do not say {words!r}")


def default_seat(choose_out: str) -> tuple[str, str]:
    """The DEFAULT seat of `agent-bus seat choose` and its handle; a fresh checkout has no
    seats, so the default must be a new one."""
    rows: dict[str, tuple[str, str]] = {}
    default = None
    for line in choose_out.splitlines():
        fields = line.split("\t")
        if fields[0] == "CHOICE" and len(fields) == 5:
            rows[fields[1]] = (fields[2], fields[3])
        elif fields[0] == "DEFAULT" and len(fields) == 2:
            default = fields[1]
    if default is None:
        raise Failed(f"no DEFAULT line in agent-bus seat choose's output: {choose_out!r}")
    if default not in rows:
        raise Failed(f"the default seat {default} has no CHOICE row: {choose_out!r}")
    state, handle = rows[default]
    if state != "new":
        raise Failed(f"the default seat {default} is {state}, not new: this checkout should have no seats")
    return default, handle


def expect_not_member(list_out: str, handle: str) -> None:
    """`agent-bus list TEAM`'s MEMBER lines (handle, ..., state) name no member `handle`."""
    for line in list_out.splitlines():
        fields = line.split("\t")
        if fields[0] == "MEMBER" and len(fields) == 5 and fields[1] == handle:
            raise Failed(f"{handle} is a member ({fields[4]}): a seat was made")


def parse_wants(items: Sequence[str]) -> list[tuple[str, str]]:
    wants = []
    for item in items:
        ref, sep, handle = item.partition("=")
        if not sep or "@" not in ref or not handle:
            raise Failed(f"{item!r} is not SEAT@TEAM=HANDLE")
        wants.append((ref, handle))
    return wants


def seats_held(seat_list: str, wants: Sequence[tuple[str, str]]) -> int:
    """`agent-bus seat list`'s SEAT lines (team, seat, held|free, self|-, handle): 0 when every
    wanted seat is held with its handle, 3 while one is absent or free."""
    seen: dict[str, tuple[str, str]] = {}
    for line in seat_list.splitlines():
        fields = line.split("\t")
        if fields[0] == "SEAT" and len(fields) == 6:
            seen[f"{fields[2]}@{fields[1]}"] = (fields[3], fields[5])
    result = EXIT_OK
    for ref, handle in wants:
        if ref not in seen:
            result = EXIT_NOT_YET
            continue
        state, held_by = seen[ref]
        if held_by != handle:
            raise Failed(f"seat {ref} is {held_by}, want {handle}")
        if state != "held":
            result = EXIT_NOT_YET
    return result


def expect_record(path: str, uid: int, since: float) -> None:
    """The consent record as pingbus counts it (config.element_accepted): a plain file of
    `uid`'s, open to no one else; and this run's, written at or after `since`."""
    try:
        info = os.lstat(path)
    except FileNotFoundError:
        raise Failed(f"no consent record at {path}") from None
    if not stat.S_ISREG(info.st_mode):
        raise Failed(f"{path} is not a plain file")
    if info.st_uid != uid:
        raise Failed(f"{path} belongs to uid {info.st_uid}, want {uid}")
    if info.st_mode & 0o077:
        raise Failed(f"{path} is mode {stat.S_IMODE(info.st_mode):o}: open to other users")
    if info.st_mtime + 1 < since:
        raise Failed(f"{path} was written before this run (mtime {info.st_mtime:.0f}, run {since:.0f})")


def _read(path: str) -> str:
    return pathlib.Path(path).read_text(encoding="utf-8", errors="replace")


def _none(_: object) -> int:
    return EXIT_OK


def _print(value: str) -> int:
    print(value)
    return EXIT_OK


#: name: (least argument count, exact?, run) — run returns the exit code.
COMMANDS: dict[str, tuple[int, bool, Callable[[Sequence[str]], int]]] = {
    "screen": (1, True, lambda a: _print(screen(_read(a[0])))),
    "expect-consent": (2, False, lambda a: _none(expect_consent(_read(a[0]), a[1:]))),
    "default-seat": (1, True, lambda a: _print("\t".join(default_seat(_read(a[0]))))),
    "expect-not-member": (2, True, lambda a: _none(expect_not_member(_read(a[0]), a[1]))),
    "seats-held": (2, False, lambda a: seats_held(_read(a[0]), parse_wants(a[1:]))),
    "expect-record": (3, True, lambda a: _none(expect_record(a[0], int(a[1]), float(a[2])))),
}


def main(argv: Sequence[str]) -> int:
    if not argv or argv[0] not in COMMANDS:
        print(f"usage: consent_check.py {{{','.join(COMMANDS)}}} ARGS...", file=sys.stderr)
        return EXIT_USAGE
    least, exact, run = COMMANDS[argv[0]]
    args = argv[1:]
    if len(args) < least or (exact and len(args) != least):
        print(f"usage: consent_check.py {argv[0]} takes {'' if exact else 'at least '}{least} argument(s)",
              file=sys.stderr)
        return EXIT_USAGE
    try:
        return run(args)
    except (Failed, OSError, ValueError) as exc:
        print(f"[FAIL] {exc}", file=sys.stderr)
        return EXIT_FAIL


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
