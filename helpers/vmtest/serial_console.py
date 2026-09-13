"""Read a guest's serial console from a unix socket, answer the LUKS prompt, name a stall (Plan 00110, §5.3a, T5.1b).

    python3 -m helpers.vmtest.serial_console --socket PATH --log FILE --until REGEX \\
        [--unlock-with FILE] [--timeout SECONDS]

Tees every byte from the socket into --log from the moment it connects. With
--unlock-with, waits for the `cryptsetup`/`systemd-ask-password` passphrase
prompt, sends the file's first line ONCE (never logged), and expects the boot
to continue. Exits when --until matches the accumulated console text:

    VMTEST-CONSOLE unlocked                        the prompt was answered
    VMTEST-CONSOLE reached pattern=<regex>         --until matched; exit 0
    VMTEST-CONSOLE stalled stage=boot reason=<..>  exit 1, excerpt on stderr

A guest that never prints the prompt, one that refuses the answer, and one
that never reaches --until are three different stalls and each is named; none
is reported as a bare timeout. `stage=boot` is the response's failure stage.
"""

from __future__ import annotations

import argparse
import os
import pathlib
import re
import select
import socket
import sys
import time

# systemd-ask-password on a serial console prints, without a trailing newline,
# "Please enter passphrase for disk luks-…: (press TAB for no echo) ", so the prompt is
# matched by its phrase, not by a colon at end of line.
PROMPT_RE = re.compile(r"(Please enter passphrase for disk|Enter passphrase for)[^\n]*:")
REFUSED_RE = re.compile(r"(Sorry, try again|No key available with this passphrase|Failed to activate with specified passphrase)")
EXCERPT_LINES = 25


def excerpt(text: str) -> str:
    lines = [line for line in text.replace("\r", "").splitlines() if line.strip()]
    return "\n".join(lines[-EXCERPT_LINES:])


def stalled(reason: str, text: str) -> int:
    print(f"VMTEST-CONSOLE stalled stage=boot reason={reason}")
    print(f"ERROR: {reason}; last console lines:", file=sys.stderr)
    print(excerpt(text) or "(nothing was written to the serial console)", file=sys.stderr)
    return 1


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--socket", required=True, type=pathlib.Path)
    parser.add_argument("--log", required=True, type=pathlib.Path)
    parser.add_argument("--until", required=True, help="regex on the accumulated console text that means 'done'")
    parser.add_argument("--unlock-with", type=pathlib.Path, default=None, help="file whose first line answers the LUKS prompt")
    parser.add_argument("--follow", action="store_true", help="log until the port closes; a closed port is then the normal end (exit 0)")
    parser.add_argument("--timeout", type=float, default=600.0)
    parser.add_argument("--connect-wait", type=float, default=30.0, help="seconds to wait for the socket to appear")
    args = parser.parse_args(argv)

    passphrase: str | None = None
    if args.unlock_with is not None:
        try:
            passphrase = args.unlock_with.read_text(encoding="utf-8").splitlines()[0]
        except (OSError, IndexError) as exc:
            print(f"ERROR: --unlock-with {args.unlock_with}: {exc}", file=sys.stderr)
            return 2
    until = re.compile(args.until, re.MULTILINE)

    deadline = time.monotonic() + args.connect_wait
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    while True:
        try:
            sock.connect(str(args.socket))
            break
        except OSError as exc:
            if time.monotonic() > deadline:
                print(f"ERROR: could not connect to {args.socket}: {exc}", file=sys.stderr)
                return 2
            time.sleep(0.2)

    text = ""
    unlocked = False
    start = time.monotonic()
    with args.log.open("ab") as log:
        while True:
            remaining = args.timeout - (time.monotonic() - start)
            if remaining <= 0:
                if passphrase is not None and not unlocked:
                    return stalled("no passphrase prompt reached the serial console (is plymouth.enable=0 on the kernel line?)", text)
                if passphrase is not None and REFUSED_RE.search(text[-4000:]):
                    return stalled("wedged at the LUKS passphrase prompt: the passphrase was refused", text)
                return stalled(f"console never matched {args.until!r} within {int(args.timeout)}s", text)
            ready, _, _ = select.select([sock], [], [], min(remaining, 1.0))
            if not ready:
                continue
            data = sock.recv(4096)
            if not data:
                if args.follow:
                    print("VMTEST-CONSOLE closed")
                    return 0
                if passphrase is not None and not unlocked:
                    return stalled("the serial port closed with no passphrase prompt seen", text)
                return stalled("the serial port closed before the console matched the expected pattern", text)
            log.write(data)
            log.flush()
            os.fsync(log.fileno())
            text += data.decode("utf-8", errors="replace")
            if len(text) > 1_000_000:
                text = text[-500_000:]
            if passphrase is not None and not unlocked and PROMPT_RE.search(text.replace("\r", "")):
                sock.sendall((passphrase + "\n").encode("utf-8"))
                unlocked = True
                print("VMTEST-CONSOLE unlocked")
                sys.stdout.flush()
            if passphrase is not None and unlocked and REFUSED_RE.search(text[-4000:]):
                return stalled("wedged at the LUKS passphrase prompt: the passphrase was refused", text)
            if until.search(text.replace("\r", "")):
                print(f"VMTEST-CONSOLE reached pattern={args.until}")
                return 0


if __name__ == "__main__":
    sys.exit(main())
