"""Shared by the Plan 00148 triage probes: the report, the deployed helpers.

Diagnostics go to stderr; the findings are appended to the markdown report named on the
probe's command line. No probe loads a Whisper model: one does not fit on the GPU beside
the warm server's, so the speed of dictation is read from the server's own figures.
"""

from __future__ import annotations

import pathlib
import subprocess
import sys

RATE = 16000
BIN = pathlib.Path.home() / ".local" / "bin"


def say(message: str) -> None:
    print(message, file=sys.stderr, flush=True)


def append(report: str, text: str) -> None:
    with open(report, "a", encoding="utf-8") as f:
        f.write(text.rstrip("\n") + "\n\n")


def run_helper(*argv: str) -> str:
    """stdout of a deployed helper; raises with its stderr if it fails."""
    result = subprocess.run([str(BIN / argv[0]), *argv[1:]], capture_output=True,
                            text=True, timeout=60, check=False)
    if result.returncode != 0:
        raise RuntimeError(f"{argv[0]} {' '.join(argv[1:])} failed: {result.stderr.strip()}")
    if result.stderr.strip():
        say(result.stderr.strip())
    return result.stdout.strip()
