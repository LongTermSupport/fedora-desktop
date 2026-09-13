"""Tests for helpers/vmtest/serial_console.py — the serial console reader that can answer a prompt (T5.1b).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_serial_console

The guest's serial port is a unix socket on the host. The reader tees every
byte to the console log from the first instant, and when told to, waits for a
prompt (the LUKS passphrase prompt) and answers it — once — then keeps logging
until the port closes or it is told to stop. A guest that never reaches the
prompt, or never gets past it, is reported by name with the console excerpt,
never as a bare timeout. Driven against a fake guest on a real unix socket.
"""

from __future__ import annotations

import os
import pathlib
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

# Verbatim from a Fedora 44 guest's serial console: no trailing newline, and a hint after the colon.
PROMPT = "\x1b[0;1;39mPlease enter passphrase for disk luks-49e43c29-ae1a-4d97-bb83-23a1b4d25ba1: (press TAB for no echo) \x1b[0m"


class FakeGuest:
    """Listens on a unix socket, streams `before`, waits for a line, streams `after`."""

    def __init__(self, path, *, before: str, after: str, expect_line: str | None, delay: float = 0.05, hold: bool = True):
        self.path = path
        self.before = before
        self.after = after
        self.expect_line = expect_line
        self.delay = delay
        self.hold = hold
        self.received = b""
        self.server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.server.bind(str(path))
        self.server.listen(1)
        self.thread = threading.Thread(target=self.body, daemon=True)
        self.thread.start()

    def body(self):
        conn, _ = self.server.accept()
        with conn:
            for chunk in self.before.splitlines(keepends=True):
                conn.sendall(chunk.encode())
                time.sleep(self.delay)
            if self.expect_line is not None:
                conn.settimeout(5)
                buffer = b""
                while not buffer.endswith(b"\n"):
                    data = conn.recv(1024)
                    if not data:
                        break
                    buffer += data
                self.received = buffer
                if buffer.strip() != self.expect_line.encode():
                    conn.sendall(b"Sorry, try again.\n")
                    return
            for chunk in self.after.splitlines(keepends=True):
                conn.sendall(chunk.encode())
                time.sleep(self.delay)
            # A real serial port stays open however quiet the guest is; hold the
            # connection until the reader hangs up (or a generous fixture limit).
            if not self.hold:
                return
            conn.settimeout(10)
            try:
                while conn.recv(1024):
                    pass
            except TimeoutError:
                return


class SerialCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        self.sock = self.root / "serial.sock"
        self.log = self.root / "console.log"
        self.passphrase_file = self.root / "passphrase"
        self.passphrase_file.write_text("correct horse\n")

    def run_reader(self, *extra, timeout="8"):
        return subprocess.run(
            [sys.executable, "-m", "helpers.vmtest.serial_console", "--socket", str(self.sock), "--log", str(self.log), "--timeout", timeout, *extra],
            cwd=REPO_ROOT, capture_output=True, text=True, check=False,
        )


class TestUnlock(SerialCase):
    def test_answers_the_prompt_once_and_reports_userspace_reached(self):
        guest = FakeGuest(self.sock, before=f"[    0.000000] Linux version 7.2\n{PROMPT}", after="\n[  OK  ] Reached target Basic System.\nfedora login: ", expect_line="correct horse")
        result = self.run_reader("--unlock-with", str(self.passphrase_file), "--until", r"login: $")
        guest.thread.join(timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(guest.received.strip(), b"correct horse")
        self.assertIn("VMTEST-CONSOLE unlocked", result.stdout)
        self.assertIn("VMTEST-CONSOLE reached", result.stdout)
        log = self.log.read_text()
        self.assertIn("Linux version 7.2", log)
        self.assertIn("login:", log)
        self.assertNotIn("correct horse", log)

    def test_a_guest_that_never_prompts_is_named_not_timed_out_into_silence(self):
        guest = FakeGuest(self.sock, before="[    0.000000] Linux version 7.2\n[ boot hangs here ]\n", after="", expect_line=None)
        result = self.run_reader("--unlock-with", str(self.passphrase_file), "--until", r"login: $", timeout="2")
        guest.thread.join(timeout=5)
        self.assertEqual(result.returncode, 1)
        self.assertIn("VMTEST-CONSOLE stalled stage=boot", result.stdout)
        self.assertIn("no passphrase prompt", result.stderr)
        self.assertIn("boot hangs here", result.stderr)

    def test_a_guest_wedged_at_the_prompt_is_named_with_the_excerpt(self):
        # The prompt appears and the answer is refused: the run must say "wedged
        # at the LUKS passphrase prompt", never "boot timeout".
        guest = FakeGuest(self.sock, before=f"boot\n{PROMPT}", after="", expect_line="something else")
        result = self.run_reader("--unlock-with", str(self.passphrase_file), "--until", r"login: $", timeout="2")
        guest.thread.join(timeout=5)
        self.assertEqual(result.returncode, 1)
        self.assertIn("VMTEST-CONSOLE stalled stage=boot", result.stdout)
        self.assertIn("wedged at the LUKS passphrase prompt", result.stderr)
        self.assertIn("Sorry, try again", result.stderr)

    def test_a_plymouth_owned_prompt_that_never_reaches_serial_is_the_no_prompt_case(self):
        # T5.1b: with Plymouth enabled nothing is written to the serial line at
        # all; that must surface as the named stall, which is what proves the
        # matcher is not a no-op.
        guest = FakeGuest(self.sock, before="", after="", expect_line=None)
        result = self.run_reader("--unlock-with", str(self.passphrase_file), "--until", r"login: $", timeout="2")
        guest.thread.join(timeout=5)
        self.assertEqual(result.returncode, 1)
        self.assertIn("no passphrase prompt", result.stderr)


class TestLogOnly(SerialCase):
    def test_logs_until_the_pattern_without_unlocking(self):
        guest = FakeGuest(self.sock, before="Anaconda starting\nInstallation complete\n", after="", expect_line=None)
        result = self.run_reader("--until", r"Installation complete")
        guest.thread.join(timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("VMTEST-CONSOLE reached", result.stdout)
        self.assertIn("Anaconda starting", self.log.read_text())

    def test_a_closed_port_before_the_pattern_is_reported(self):
        guest = FakeGuest(self.sock, before="short\n", after="", expect_line=None)
        result = self.run_reader("--until", r"never appears", timeout="4")
        guest.thread.join(timeout=5)
        self.assertEqual(result.returncode, 1)
        self.assertIn("VMTEST-CONSOLE stalled", result.stdout)

    def test_follow_mode_logs_until_the_port_closes_and_that_is_success(self):
        # The guest powering off closes the port; for a follower that is the end, not a stall.
        guest = FakeGuest(self.sock, before="Installation complete\nPowering off.\n", after="", expect_line=None, hold=False)
        result = self.run_reader("--until", r"never appears", "--follow", timeout="6")
        guest.thread.join(timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("VMTEST-CONSOLE closed", result.stdout)
        self.assertIn("Powering off", self.log.read_text())

    def test_missing_socket_is_a_usage_error(self):
        result = self.run_reader("--until", "x", "--connect-wait", "1", timeout="1")
        self.assertEqual(result.returncode, 2)
        self.assertFalse(os.path.exists(self.log) and self.log.read_text())


if __name__ == "__main__":
    unittest.main()
