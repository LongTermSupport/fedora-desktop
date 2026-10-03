"""Shared stubs for the speech-to-text stop tests: script loader and a fake pw-record.

The fake pw-record writes 2048-byte chunks of 16 kHz s16 audio every 20 ms. On SIGTERM
it writes one extra 32 KiB burst - standing in for the audio a real recorder still had
in its pipe - records its stop time and total bytes written, and exits. A stop path
that stops reading before EOF loses that burst, so "bytes fed == bytes written" proves
the pipe was drained.
"""

import importlib.util
import json
import os
import pathlib
from importlib.machinery import SourceFileLoader

REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
# STT_TEST_BIN points the tests at another copy of the scripts, e.g. an older
# revision, to prove a test fails on the defect it guards (a control run)
BIN = pathlib.Path(os.environ.get("STT_TEST_BIN", REPO_ROOT / "files" / "home" / ".local" / "bin"))

FAKE_PW_RECORD = r'''#!/usr/bin/env python3
import json, os, signal, sys, time
events = os.environ["STUB_EVENTS"]
written = 0
def stopped(signum, frame):
    global written
    burst = b"\x01\x00" * 16384
    sys.stdout.buffer.write(burst)
    sys.stdout.buffer.flush()
    written += len(burst)
    with open(os.path.join(events, "pw-record.json"), "w") as f:
        json.dump({"stopped_at": time.monotonic(), "written": written}, f)
    sys.exit(0)
signal.signal(signal.SIGTERM, stopped)
signal.signal(signal.SIGINT, stopped)
chunk = b"\x02\x00" * 1024
while True:
    sys.stdout.buffer.write(chunk)
    sys.stdout.buffer.flush()
    written += len(chunk)
    time.sleep(0.02)
'''


def load_script(name, module_name):
    loader = SourceFileLoader(module_name, str(BIN / name))
    spec = importlib.util.spec_from_loader(module_name, loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


def install_fake_pw_record(stub_dir):
    path = pathlib.Path(stub_dir) / "pw-record"
    path.write_text(FAKE_PW_RECORD)
    path.chmod(0o755)
    return path


def pw_record_result(events_dir):
    """{'stopped_at': monotonic seconds, 'written': bytes} from the fake pw-record."""
    return json.loads((pathlib.Path(events_dir) / "pw-record.json").read_text())
