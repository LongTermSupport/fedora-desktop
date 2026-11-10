#!/usr/bin/env python3
"""ssh-keygen with a passphrase that never reaches a command line.

ssh-keygen takes a passphrase only as an argument (-P, -N) or from a prompt. An argument is
readable by every local user while the command runs (/proc/<pid>/cmdline), so this helper takes
the passphrase on its own stdin and answers ssh-keygen's prompts as its SSH_ASKPASS program,
with SSH_ASKPASS_REQUIRE=force. The askpass program is a two-line script in a private temporary
directory; it prints the passphrase from its environment and holds no secret itself
(CLAUDE/QA.md "secret-in-argv", issue #220).

    python3 -m helpers.ssh_passphrase.cli generate KEY COMMENT   # new ed25519 key
    python3 -m helpers.ssh_passphrase.cli unlocks KEY            # exit 0 opens, 1 does not
    python3 -m helpers.ssh_passphrase.cli strip KEY              # remove the passphrase in place

The passphrase is stdin less one trailing newline (Ansible's stdin: adds one). Exit 2 is a run
that could not judge: no passphrase, a missing key, a key that would be overwritten.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile

PASSPHRASE_VAR = "SSH_PASSPHRASE_HELPER_VALUE"
ASKPASS_SCRIPT = '#!/bin/sh\nprintf \'%s\\n\' "$' + PASSPHRASE_VAR + '"\n'


def keygen_argv(mode: str, key: str, comment: str) -> list[str]:
    """The ssh-keygen command for a mode; it holds no passphrase."""
    if mode == "generate":
        return ["ssh-keygen", "-q", "-t", "ed25519", "-C", comment, "-f", key]
    if mode == "unlocks":
        return ["ssh-keygen", "-y", "-f", key]
    return ["ssh-keygen", "-p", "-N", "", "-f", key]


def run_keygen(argv: list[str], passphrase: str) -> subprocess.CompletedProcess:
    """Run ssh-keygen with this process as the only route the passphrase takes."""
    askpass_dir = tempfile.mkdtemp(prefix="ssh-askpass-")
    try:
        askpass = os.path.join(askpass_dir, "askpass")
        fd = os.open(askpass, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o700)
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            fh.write(ASKPASS_SCRIPT)
        env = dict(os.environ)
        env.update({"SSH_ASKPASS": askpass, "SSH_ASKPASS_REQUIRE": "force", PASSPHRASE_VAR: passphrase})
        return subprocess.run(argv, env=env, stdin=subprocess.DEVNULL, capture_output=True, check=False)
    finally:
        shutil.rmtree(askpass_dir)


def read_passphrase(text: str) -> str:
    return text[:-1] if text.endswith("\n") else text


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="mode", required=True)
    gen = sub.add_parser("generate", help="make a new ed25519 key protected by the passphrase")
    gen.add_argument("key")
    gen.add_argument("comment")
    sub.add_parser("unlocks", help="exit 0 if the passphrase opens the key, 1 if not").add_argument("key")
    sub.add_parser("strip", help="remove the passphrase from the key, in place").add_argument("key")
    args = parser.parse_args(argv)

    passphrase = read_passphrase(sys.stdin.read())
    if passphrase == "":
        print("ssh_passphrase: no passphrase on stdin; nothing was run", file=sys.stderr)
        return 2
    exists = os.path.exists(args.key)
    if args.mode == "generate" and exists:
        print(f"ssh_passphrase: {args.key} already exists; it is not overwritten", file=sys.stderr)
        return 2
    if args.mode != "generate" and not exists:
        print(f"ssh_passphrase: no key at {args.key}", file=sys.stderr)
        return 2

    res = run_keygen(keygen_argv(args.mode, args.key, getattr(args, "comment", "")), passphrase)
    if res.returncode != 0:
        detail = res.stderr.decode("utf-8", "replace").strip()
        print(f"ssh_passphrase: ssh-keygen {args.mode} failed for {args.key} (exit {res.returncode}): {detail}",
              file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
