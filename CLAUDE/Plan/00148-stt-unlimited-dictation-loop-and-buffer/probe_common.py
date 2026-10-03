"""Shared by the Plan 00148 triage probes: the recording, the settings, the model.

Every probe reads the same raw recording (16 kHz mono s16) and asks the DEPLOYED
helpers, wsi-setting and wsi-resolve-model, which model the recorders would load, so the
measurements are of what the owner's Insert actually runs. Diagnostics go to stderr; the
findings are appended to the markdown report named on the probe's command line.
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


def setting(key: str) -> str:
    return run_helper("wsi-setting", key)


def language() -> str:
    """The language code the recorders use, as wsi-stream's session_language() computes it."""
    import os
    value = setting("language")
    if value == "system":
        return (os.environ.get("LANG") or "en_GB.UTF-8").split("_")[0]
    return value


def streaming_model(lang: str) -> str:
    return run_helper("wsi-resolve-model", "--mode", "streaming", "--language", lang,
                      setting("whisper-model"))


def device() -> tuple[str, str]:
    """(device, compute_type) for faster-whisper: CUDA when CTranslate2 counts a GPU."""
    count = int(run_helper("wsi-resolve-model", "--gpu-count"))
    return ("cuda", "float16") if count > 0 else ("cpu", "int8")


def load_audio(path: str):
    """The recording as float32 samples in [-1, 1]. Raises if it is too short to use."""
    import numpy as np
    raw = pathlib.Path(path).read_bytes()
    samples = np.frombuffer(raw[: len(raw) - len(raw) % 2], dtype=np.int16)
    if len(samples) < RATE * 20:
        raise RuntimeError(f"{path} holds {len(samples) / RATE:.1f} s of audio; at least 20 s is needed")
    return samples.astype(np.float32) / 32768.0


def words(text: str) -> list[str]:
    """Lower-case words without punctuation, for comparing two transcripts."""
    import re
    return re.findall(r"[a-z0-9']+", text.lower())
