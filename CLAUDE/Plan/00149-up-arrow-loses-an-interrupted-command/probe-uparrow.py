"""Drive the owner's real interactive bash in a pty and report what up-arrow brings back.

Plan 00149 triage. The shell is `bash -i` with the owner's own HOME and startup files, so
every hook that runs in a terminal runs here too. Three cases, each in a fresh shell:

  A: a command that finishes, then up-arrow;
  B: a running command interrupted with Ctrl+C, then up-arrow;
  C: a typed line abandoned with Ctrl+C before Enter, then up-arrow.

After the up-arrow the line is moved to the start and `echo UPLINE=` is typed in front of it,
so Enter prints what up-arrow recalled without running it. Each case's commands carry the
marker `uparrow-probe`, and they do reach the owner's history file, as any command would.

The shell's own history settings are printed first, from inside it.

argv: <report file> [<rcfile>]. The report is appended to (markdown); a line per case also
goes to stdout. With an rcfile the shell is `bash --rcfile <rcfile> -i` instead, so a run
with only this repo's history files loaded can be told apart from the full startup.
"""

from __future__ import annotations

import os
import pty
import re
import select
import sys
import time

SETTLE = 2.0
STATE = (
    "printf 'STATE-BEGIN\\n'; echo \"BASH_VERSION=$BASH_VERSION\"; "
    "declare -p HISTSIZE HISTFILESIZE HISTFILE HISTCONTROL HISTIGNORE PROMPT_COMMAND 2>&1; "
    "declare -p __history_shared_file 2>&1; shopt -p histappend cmdhist lithist histverify; "
    "trap -p EXIT; bind -q previous-history; bind -p | grep -F -e previous-history -e 'history-search'; "
    "echo \"in-memory history entries: $(HISTTIMEFORMAT= builtin history | wc -l)\"; "
    "printf 'STATE-END\\n'"
)
# An OSC sequence ends with BEL or with ESC \ (systemd's OSC 3008 context uses the second),
# and must never run past either: a BEL-only pattern swallowed every command's output from
# systemd's sequence to the next window-title one.
ANSI = re.compile(r"\x1b\[[0-9;?]*[A-Za-z@]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\r")


def read_for(fd: int, seconds: float) -> bytes:
    out = b""
    end = time.time() + seconds
    while time.time() < end:
        ready, _, _ = select.select([fd], [], [], 0.1)
        if not ready:
            continue
        try:
            chunk = os.read(fd, 65536)
        except OSError:
            break
        if not chunk:
            break
        out += chunk
    return out


BASH_ARGV = ["bash", "-i"]


def spawn() -> tuple[int, int]:
    pid, fd = pty.fork()
    if pid == 0:
        os.execvp("bash", BASH_ARGV)
    read_for(fd, SETTLE)
    return pid, fd


def finish(pid: int, fd: int) -> None:
    os.write(fd, b"\x15exit\r")
    read_for(fd, 1.0)
    os.close(fd)
    os.waitpid(pid, 0)


def recall(fd: int) -> str:
    """Press up-arrow, then print the recalled line without running it."""
    os.write(fd, b"\x1b[A")
    read_for(fd, 1.0)
    os.write(fd, b"\x01echo UPLINE=\r")
    text = ANSI.sub("", read_for(fd, SETTLE).decode(errors="replace"))
    found = re.findall(r"^UPLINE=(.*)$", text, re.MULTILINE)
    return found[-1] if found else f"(no UPLINE line came back; the shell printed: {text!r})"


def state() -> str:
    pid, fd = spawn()
    os.write(fd, STATE.encode() + b"\r")
    text = ANSI.sub("", read_for(fd, SETTLE).decode(errors="replace"))
    finish(pid, fd)
    match = re.search(r"^STATE-BEGIN$(.*?)^STATE-END$", text, re.MULTILINE | re.DOTALL)
    return match.group(1).strip() if match else f"(no state block came back: {text!r})"


def case_a() -> str:
    pid, fd = spawn()
    os.write(fd, b"true uparrow-probe-A\r")
    read_for(fd, SETTLE)
    line = recall(fd)
    finish(pid, fd)
    return line


def case_b() -> str:
    pid, fd = spawn()
    os.write(fd, b"sleep 30 # uparrow-probe-B\r")
    read_for(fd, 1.5)
    os.write(fd, b"\x03")
    read_for(fd, SETTLE)
    line = recall(fd)
    finish(pid, fd)
    return line


def case_c() -> str:
    pid, fd = spawn()
    os.write(fd, b"true uparrow-probe-C-first\r")
    read_for(fd, SETTLE)
    os.write(fd, b"echo uparrow-probe-C-abandoned")
    read_for(fd, 0.5)
    os.write(fd, b"\x03")
    read_for(fd, SETTLE)
    line = recall(fd)
    finish(pid, fd)
    return line


def main() -> int:
    if len(sys.argv) not in (2, 3):
        print("usage: probe-uparrow.py <report file> [<rcfile>]", file=sys.stderr)
        return 2
    title = "the full interactive startup (bash -i)"
    if len(sys.argv) == 3:
        BASH_ARGV[:] = ["bash", "--rcfile", sys.argv[2], "-i"]
        title = f"only this repo's history files (bash --rcfile {sys.argv[2]} -i)"
    rows = [
        ("A: a command that finished", "true uparrow-probe-A", case_a()),
        ("B: a running command, Ctrl+C", "sleep 30", case_b()),
        ("C: a typed line, Ctrl+C before Enter", "true uparrow-probe-C-first", case_c()),
    ]
    with open(sys.argv[1], "a", encoding="utf-8") as report:
        report.write(f"## With {title}\n\n### The live shell's history settings\n\n```text\n")
        report.write(state() + "\n```\n\n### What up-arrow brings back\n\n")
        report.write("| Case | Expected | Up-arrow brought back |\n| --- | --- | --- |\n")
        for case, expected, got in rows:
            report.write(f"| {case} | `{expected}` | `{got}` |\n")
            print(f"{title}: {case}: expected {expected!r}, got {got!r}")
        report.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
