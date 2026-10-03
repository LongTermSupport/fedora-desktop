"""Record the owner reading aloud: <out.raw> <seconds>. Raw 16 kHz mono s16, as the server reads it.

Plan 00148 triage. Fails if pw-record exits early or captures much less than asked for.
"""

from __future__ import annotations

import signal
import subprocess
import sys
import threading
import time

from probe_common import RATE, say


def main(out: str, seconds: str) -> int:
    duration = int(seconds)
    proc = subprocess.Popen(["pw-record", "--rate", str(RATE), "--channels", "1",
                             "--format", "s16", "-"], stdout=subprocess.PIPE)
    captured = bytearray()

    def read():
        while chunk := proc.stdout.read(4096):
            captured.extend(chunk)

    reader = threading.Thread(target=read)
    reader.start()
    say(f"RECORDING for {duration} s - start reading aloud now")
    for left in range(duration, 0, -10):
        if proc.poll() is not None:
            raise RuntimeError(f"pw-record exited early with {proc.returncode}")
        say(f"  {left} s left")
        time.sleep(min(10, left))
    proc.send_signal(signal.SIGINT)
    proc.wait(timeout=5)
    reader.join(timeout=5)
    say("Recording finished - you can stop reading")

    got = len(captured) / 2 / RATE
    if got < duration * 0.9:
        raise RuntimeError(f"pw-record captured {got:.1f} s of the {duration} s asked for")
    with open(out, "wb") as f:
        f.write(captured)
    say(f"{got:.1f} s recorded to {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main(*sys.argv[1:]))
