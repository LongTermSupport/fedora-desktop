"""Tests for helpers/ssh_agent_filter/ssh_agent_filter.py — ccy's one-key agent (Plan 00163).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.ssh_agent_filter.test_ssh_agent_filter

The filter stands between a ccy container and the owner's ssh-agent: it lists and signs
with only the allowed key and refuses every other request. The protocol helpers are tested
on their own, and the filter itself is run as the launcher runs it (a separate process, by
path) in front of a REAL `ssh-agent` holding two generated keys, and driven with the real
`ssh-add` and `ssh-keygen`. A stub agent would only prove the filter agrees with the stub.

The OpenSSH client tools are required, not optional: a machine without them fails here
loudly (they are installed by the playbooks; a missing one is an IaC gap, never a skip).
"""

from __future__ import annotations

import os
import pathlib
import shutil
import signal
import socket
import stat
import struct
import subprocess
import sys
import tempfile
import time
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.ssh_agent_filter import ssh_agent_filter as agent_filter

HELPER = REPO_ROOT / "helpers/ssh_agent_filter/ssh_agent_filter.py"
REQUIRED_TOOLS = ("ssh-agent", "ssh-add", "ssh-keygen")
WAIT_SECONDS = 10.0


def _require_tools() -> None:
    missing = [tool for tool in REQUIRED_TOOLS if shutil.which(tool) is None]
    if missing:
        raise RuntimeError(
            f"{', '.join(missing)} not found on PATH: these tests drive a real ssh-agent. "
            "Install the OpenSSH clients through the playbooks; this is an IaC gap, not a skip."
        )


def _run(argv: list[str], env: dict[str, str], **kwargs) -> subprocess.CompletedProcess:
    return subprocess.run(argv, env=env, capture_output=True, text=True, check=False, timeout=30, **kwargs)


def _wait_for_socket(path: pathlib.Path, process: subprocess.Popen) -> None:
    deadline = time.monotonic() + WAIT_SECONDS
    while time.monotonic() < deadline:
        if path.is_socket():
            return
        if process.poll() is not None:
            raise RuntimeError(f"{process.args!r} exited with {process.returncode} before {path} appeared")
        time.sleep(0.05)
    raise RuntimeError(f"{path} did not appear within {WAIT_SECONDS}s")


def _wait_for_exit(process: subprocess.Popen) -> int:
    return process.wait(timeout=WAIT_SECONDS)


def _string(data: bytes) -> bytes:
    return struct.pack(">I", len(data)) + data


def _message(payload: bytes) -> bytes:
    return struct.pack(">I", len(payload)) + payload


def _key_blob(public_line: str) -> bytes:
    import base64

    return base64.b64decode(public_line.split()[1])


class _Keys:
    """Two generated keys in a fresh directory: `a` is the allowed one, `b` is not."""

    def __init__(self, directory: pathlib.Path) -> None:
        self.directory = directory
        env = {"PATH": os.environ["PATH"], "HOME": str(directory)}
        for name in ("a", "b"):
            result = _run(
                ["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", f"test-{name}", "-f", str(directory / name)],
                env,
            )
            if result.returncode != 0:
                raise RuntimeError(f"ssh-keygen failed: {result.stderr}")
        self.a = directory / "a"
        self.b = directory / "b"
        self.a_public = (directory / "a.pub").read_text().strip()
        self.b_public = (directory / "b.pub").read_text().strip()
        self.a_fingerprint = self._fingerprint(self.a, env)
        self.b_fingerprint = self._fingerprint(self.b, env)

    @staticmethod
    def _fingerprint(key: pathlib.Path, env: dict[str, str]) -> str:
        result = _run(["ssh-keygen", "-E", "sha256", "-lf", str(key)], env)
        if result.returncode != 0:
            raise RuntimeError(f"ssh-keygen -l failed: {result.stderr}")
        return result.stdout.split()[1]


class ProtocolTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        _require_tools()
        cls._tmp = tempfile.TemporaryDirectory(prefix="ccy-agent-filter-unit.")
        cls.keys = _Keys(pathlib.Path(cls._tmp.name))

    @classmethod
    def tearDownClass(cls) -> None:
        cls._tmp.cleanup()

    def test_fingerprint_matches_ssh_keygen(self):
        self.assertEqual(agent_filter.fingerprint(_key_blob(self.keys.a_public)), self.keys.a_fingerprint)

    def test_identities_answer_keeps_only_allowed_keys_with_their_comments(self):
        payload = (
            bytes([agent_filter.SSH_AGENT_IDENTITIES_ANSWER])
            + struct.pack(">I", 2)
            + _string(_key_blob(self.keys.a_public))
            + _string(b"comment-a")
            + _string(_key_blob(self.keys.b_public))
            + _string(b"comment-b")
        )
        filtered = agent_filter.filter_identities(payload, {self.keys.a_fingerprint})
        expected = (
            bytes([agent_filter.SSH_AGENT_IDENTITIES_ANSWER])
            + struct.pack(">I", 1)
            + _string(_key_blob(self.keys.a_public))
            + _string(b"comment-a")
        )
        self.assertEqual(filtered, expected)

    def test_identities_answer_with_no_allowed_key_is_empty(self):
        payload = (
            bytes([agent_filter.SSH_AGENT_IDENTITIES_ANSWER])
            + struct.pack(">I", 1)
            + _string(_key_blob(self.keys.b_public))
            + _string(b"comment-b")
        )
        filtered = agent_filter.filter_identities(payload, {self.keys.a_fingerprint})
        self.assertEqual(filtered, bytes([agent_filter.SSH_AGENT_IDENTITIES_ANSWER]) + struct.pack(">I", 0))

    def test_truncated_identities_answer_is_a_protocol_error(self):
        payload = bytes([agent_filter.SSH_AGENT_IDENTITIES_ANSWER]) + struct.pack(">I", 2) + _string(b"x")
        with self.assertRaises(agent_filter.ProtocolError):
            agent_filter.filter_identities(payload, {self.keys.a_fingerprint})

    def test_identities_answer_with_trailing_bytes_is_a_protocol_error(self):
        payload = bytes([agent_filter.SSH_AGENT_IDENTITIES_ANSWER]) + struct.pack(">I", 0) + b"extra"
        with self.assertRaises(agent_filter.ProtocolError):
            agent_filter.filter_identities(payload, set())

    def test_sign_request_key_blob_is_read(self):
        blob = _key_blob(self.keys.a_public)
        payload = bytes([agent_filter.SSH_AGENTC_SIGN_REQUEST]) + _string(blob) + _string(b"data") + struct.pack(">I", 0)
        self.assertEqual(agent_filter.sign_request_key_blob(payload), blob)

    def test_truncated_sign_request_is_a_protocol_error(self):
        payload = bytes([agent_filter.SSH_AGENTC_SIGN_REQUEST]) + struct.pack(">I", 99) + b"short"
        with self.assertRaises(agent_filter.ProtocolError):
            agent_filter.sign_request_key_blob(payload)

    def test_every_other_request_type_is_refused(self):
        listed = {agent_filter.SSH_AGENTC_REQUEST_IDENTITIES, agent_filter.SSH_AGENTC_SIGN_REQUEST}
        for message_type in range(256):
            if message_type in listed:
                continue
            with self.subTest(message_type=message_type):
                self.assertEqual(agent_filter.classify(bytes([message_type])), "refuse")
        self.assertEqual(agent_filter.classify(b""), "refuse")
        self.assertEqual(agent_filter.classify(bytes([agent_filter.SSH_AGENTC_REQUEST_IDENTITIES])), "identities")
        self.assertEqual(agent_filter.classify(bytes([agent_filter.SSH_AGENTC_SIGN_REQUEST])), "sign")

    def test_an_identities_request_with_trailing_bytes_is_refused(self):
        request = bytes([agent_filter.SSH_AGENTC_REQUEST_IDENTITIES]) + b"extra"
        self.assertEqual(agent_filter.classify(request), "refuse")

    def test_read_message_gives_up_on_a_message_left_unfinished(self):
        left, right = socket.socketpair()
        previous = agent_filter.MESSAGE_SECONDS
        agent_filter.MESSAGE_SECONDS = 0.2
        try:
            with left, right:
                left.sendall(b"\x00\x00")
                started = time.monotonic()
                with self.assertRaises(agent_filter.ProtocolError):
                    agent_filter.read_message(right)
                self.assertLess(time.monotonic() - started, 5)
        finally:
            agent_filter.MESSAGE_SECONDS = previous

    def test_a_process_is_recognised_by_its_start_time_not_its_pid_alone(self):
        started = agent_filter.process_start_time(os.getpid())
        self.assertIsNotNone(started)
        self.assertTrue(agent_filter.process_alive(os.getpid(), started))
        # The same pid with another start time is another process: the one watched is gone.
        self.assertFalse(agent_filter.process_alive(os.getpid(), started + "0"))
        child = subprocess.Popen(["true"])
        child.wait(timeout=WAIT_SECONDS)
        self.assertIsNone(agent_filter.process_start_time(child.pid))

    def test_parse_fingerprint_accepts_only_sha256(self):
        self.assertEqual(agent_filter.parse_fingerprint(self.keys.a_fingerprint), self.keys.a_fingerprint)
        for bad in ("", "MD5:aa:bb", "SHA256:", "SHA256:not base64!", self.keys.a_fingerprint + "="):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                agent_filter.parse_fingerprint(bad)

    def test_read_message_refuses_an_oversized_length(self):
        left, right = socket.socketpair()
        with left, right:
            left.sendall(struct.pack(">I", agent_filter.MAX_MESSAGE + 1))
            with self.assertRaises(agent_filter.ProtocolError):
                agent_filter.read_message(right)

    def test_read_message_returns_none_at_a_clean_end(self):
        left, right = socket.socketpair()
        with right:
            left.close()
            self.assertIsNone(agent_filter.read_message(right))

    def test_read_message_refuses_a_truncated_message(self):
        left, right = socket.socketpair()
        with right:
            left.sendall(struct.pack(">I", 10) + b"abc")
            left.close()
            with self.assertRaises(agent_filter.ProtocolError):
                agent_filter.read_message(right)


class _RealAgentCase(unittest.TestCase):
    """A real ssh-agent holding keys a and b, and the filter in front of it allowing a."""

    @classmethod
    def setUpClass(cls) -> None:
        _require_tools()
        cls._tmp = tempfile.TemporaryDirectory(prefix="ccy-agent-filter.")
        cls.tmp = pathlib.Path(cls._tmp.name)
        cls.keys = _Keys(cls.tmp)
        cls.upstream = cls.tmp / "upstream.sock"
        cls.filtered = cls.tmp / "filtered.sock"
        cls.base_env = {"PATH": os.environ["PATH"], "HOME": str(cls.tmp)}
        cls.agent = subprocess.Popen(
            ["ssh-agent", "-D", "-a", str(cls.upstream)],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            env=cls.base_env,
        )
        cls.filter_process = None
        try:
            _wait_for_socket(cls.upstream, cls.agent)
            for key in (cls.keys.a, cls.keys.b):
                result = _run(["ssh-add", str(key)], cls.upstream_env())
                if result.returncode != 0:
                    raise RuntimeError(f"ssh-add {key} into the test agent failed: {result.stderr}")
            cls.filter_process = cls.start_filter(cls.filtered, [cls.keys.a_fingerprint])
        except BaseException:
            cls.tearDownClass()
            raise

    @classmethod
    def tearDownClass(cls) -> None:
        for process in (cls.filter_process, cls.agent):
            if process is not None and process.poll() is None:
                process.terminate()
                process.wait(timeout=WAIT_SECONDS)
            if process is not None and process.stderr is not None:
                process.stderr.close()
        cls._tmp.cleanup()

    @classmethod
    def upstream_env(cls) -> dict[str, str]:
        return {**cls.base_env, "SSH_AUTH_SOCK": str(cls.upstream)}

    @classmethod
    def filtered_env(cls) -> dict[str, str]:
        return {**cls.base_env, "SSH_AUTH_SOCK": str(cls.filtered)}

    @classmethod
    def start_filter(cls, listen: pathlib.Path, allowed: list[str], *extra: str) -> subprocess.Popen:
        argv = [sys.executable, "-I", str(HELPER), "--listen", str(listen), "--upstream", str(cls.upstream)]
        for fingerprint in allowed:
            argv += ["--allow", fingerprint]
        process = subprocess.Popen([*argv, *extra], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, env=cls.base_env)
        _wait_for_socket(listen, process)
        return process

    def assert_upstream_unchanged(self):
        listed = _run(["ssh-add", "-l"], self.upstream_env())
        self.assertEqual(listed.returncode, 0, listed.stderr)
        self.assertIn(self.keys.a_fingerprint, listed.stdout)
        self.assertIn(self.keys.b_fingerprint, listed.stdout)


class FilterAgainstRealAgentTests(_RealAgentCase):
    def test_only_the_allowed_key_is_listed(self):
        listed = _run(["ssh-add", "-l"], self.filtered_env())
        self.assertEqual(listed.returncode, 0, listed.stderr)
        self.assertEqual(len(listed.stdout.splitlines()), 1, listed.stdout)
        self.assertIn(self.keys.a_fingerprint, listed.stdout)
        self.assertNotIn(self.keys.b_fingerprint, listed.stdout)

    def test_only_the_allowed_public_key_is_given_out(self):
        listed = _run(["ssh-add", "-L"], self.filtered_env())
        self.assertEqual(listed.returncode, 0, listed.stderr)
        self.assertEqual([line.split()[:2] for line in listed.stdout.splitlines()], [self.keys.a_public.split()[:2]])

    def test_the_allowed_key_signs(self):
        tested = _run(["ssh-add", "-T", str(self.keys.a) + ".pub"], self.filtered_env())
        self.assertEqual(tested.returncode, 0, tested.stderr)

    def test_the_allowed_key_signs_a_file_that_verifies(self):
        data = self.tmp / "signed-data"
        data.write_text("ccy one-key agent\n")
        signature = pathlib.Path(str(data) + ".sig")
        signature.unlink(missing_ok=True)
        # The public half alone, so the signature can only have come through the agent.
        public_only = self.tmp / "public-only-a"
        public_only.mkdir(exist_ok=True)
        shutil.copy(str(self.keys.a) + ".pub", public_only / "a.pub")
        signed = _run(
            ["ssh-keygen", "-Y", "sign", "-f", str(public_only / "a.pub"), "-n", "file", str(data)], self.filtered_env()
        )
        self.assertEqual(signed.returncode, 0, signed.stderr)
        signers = self.tmp / "allowed_signers"
        signers.write_text(f"test-a {self.keys.a_public}\n")
        with data.open("rb") as message:
            verified = subprocess.run(
                ["ssh-keygen", "-Y", "verify", "-f", str(signers), "-I", "test-a", "-n", "file", "-s", str(signature)],
                stdin=message,
                capture_output=True,
                env=self.base_env,
                check=False,
                timeout=30,
            )
        self.assertEqual(verified.returncode, 0, verified.stderr)

    def test_the_other_key_cannot_sign(self):
        tested = _run(["ssh-add", "-T", str(self.keys.b) + ".pub"], self.filtered_env())
        self.assertNotEqual(tested.returncode, 0)
        data = self.tmp / "unsigned-data"
        data.write_text("must not be signed\n")
        # Only the public half, alone: beside its private key, ssh-keygen would sign with
        # the file itself and never ask the agent.
        public_only = self.tmp / "public-only"
        public_only.mkdir(exist_ok=True)
        shutil.copy(str(self.keys.b) + ".pub", public_only / "b.pub")
        signed = _run(
            ["ssh-keygen", "-Y", "sign", "-f", str(public_only / "b.pub"), "-n", "file", str(data)], self.filtered_env()
        )
        self.assertNotEqual(signed.returncode, 0)
        self.assertFalse(pathlib.Path(str(data) + ".sig").exists())

    def test_adding_a_key_fails(self):
        added = _run(["ssh-add", str(self.keys.b)], self.filtered_env())
        self.assertNotEqual(added.returncode, 0)
        self.assert_upstream_unchanged()

    def test_removing_a_key_fails(self):
        removed = _run(["ssh-add", "-d", str(self.keys.a) + ".pub"], self.filtered_env())
        self.assertNotEqual(removed.returncode, 0)
        self.assert_upstream_unchanged()

    def test_removing_every_key_fails(self):
        removed = _run(["ssh-add", "-D"], self.filtered_env())
        self.assertNotEqual(removed.returncode, 0)
        self.assert_upstream_unchanged()

    def test_locking_the_agent_fails(self):
        askpass = self.tmp / "askpass"
        askpass.write_text("#!/bin/sh\necho lock-password\n")
        askpass.chmod(0o700)
        env = {**self.filtered_env(), "SSH_ASKPASS": str(askpass), "SSH_ASKPASS_REQUIRE": "force", "DISPLAY": ":0"}
        locked = _run(["ssh-add", "-x"], env, stdin=subprocess.DEVNULL)
        self.assertNotEqual(locked.returncode, 0)
        self.assert_upstream_unchanged()

    def _exchange(self, payload: bytes) -> bytes:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
            client.settimeout(WAIT_SECONDS)
            client.connect(str(self.filtered))
            client.sendall(_message(payload))
            reply = agent_filter.read_message(client)
        self.assertIsNotNone(reply)
        return reply

    def test_extensions_including_session_bind_are_refused(self):
        # OpenSSH's extension name, assembled so it is not mistaken for an email address.
        session_bind = b"session-bind" + b"@" + b"openssh.com"
        for name in (session_bind, b"query"):
            with self.subTest(extension=name):
                reply = self._exchange(bytes([agent_filter.SSH_AGENTC_EXTENSION]) + _string(name) + b"\x00" * 8)
                self.assertEqual(reply, bytes([agent_filter.SSH_AGENT_FAILURE]))

    def test_an_unknown_message_type_is_refused(self):
        self.assertEqual(self._exchange(bytes([200])), bytes([agent_filter.SSH_AGENT_FAILURE]))

    def test_a_malformed_sign_request_is_refused(self):
        reply = self._exchange(bytes([agent_filter.SSH_AGENTC_SIGN_REQUEST]) + struct.pack(">I", 500) + b"x")
        self.assertEqual(reply, bytes([agent_filter.SSH_AGENT_FAILURE]))

    def test_an_oversized_message_closes_that_connection_only(self):
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
            client.settimeout(WAIT_SECONDS)
            client.connect(str(self.filtered))
            client.sendall(struct.pack(">I", agent_filter.MAX_MESSAGE + 1))
            self.assertEqual(client.recv(1), b"")
        self.test_only_the_allowed_key_is_listed()

    def test_concurrent_clients_are_each_answered(self):
        clients = []
        try:
            for _ in range(3):
                client = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                client.settimeout(WAIT_SECONDS)
                client.connect(str(self.filtered))
                clients.append(client)
            for client in reversed(clients):
                client.sendall(_message(bytes([agent_filter.SSH_AGENTC_REQUEST_IDENTITIES])))
                reply = agent_filter.read_message(client)
                self.assertEqual(reply[0], agent_filter.SSH_AGENT_IDENTITIES_ANSWER)
                self.assertEqual(struct.unpack(">I", reply[1:5])[0], 1)
        finally:
            for client in clients:
                client.close()

    def test_connections_past_the_cap_are_closed_and_the_cap_frees_up(self):
        clients = []
        try:
            for _ in range(agent_filter.MAX_CLIENTS):
                client = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                client.settimeout(WAIT_SECONDS)
                client.connect(str(self.filtered))
                client.sendall(_message(bytes([agent_filter.SSH_AGENTC_REQUEST_IDENTITIES])))
                self.assertIsNotNone(agent_filter.read_message(client))
                clients.append(client)
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as extra:
                extra.settimeout(WAIT_SECONDS)
                extra.connect(str(self.filtered))
                self.assertEqual(extra.recv(1), b"")
        finally:
            for client in clients:
                client.close()
        deadline = time.monotonic() + WAIT_SECONDS
        while True:
            listed = _run(["ssh-add", "-l"], self.filtered_env())
            if listed.returncode == 0 or time.monotonic() > deadline:
                break
            time.sleep(0.1)
        self.assertEqual(listed.returncode, 0, listed.stderr)

    def test_the_socket_is_owner_only(self):
        mode = stat.S_IMODE(self.filtered.stat().st_mode)
        self.assertEqual(mode & 0o077, 0, oct(mode))

    def test_the_filter_writes_no_key_material_to_its_log(self):
        # A filter of its own, so its log can be read once it has stopped while the shared
        # one keeps serving: whatever it logs of a listing, a signature and a refusal must
        # never carry a key blob.
        listen = self.tmp / "log-probe.sock"
        env = {**self.base_env, "SSH_AUTH_SOCK": str(listen)}
        process = self.start_filter(listen, [self.keys.a_fingerprint])
        _run(["ssh-add", "-L"], env)
        _run(["ssh-add", "-T", str(self.keys.a) + ".pub"], env)
        _run(["ssh-add", "-D"], env)
        process.send_signal(signal.SIGTERM)
        _wait_for_exit(process)
        log = process.stderr.read().decode()
        process.stderr.close()
        for public in (self.keys.a_public, self.keys.b_public):
            self.assertNotIn(public.split()[1], log)


class LifetimeTests(_RealAgentCase):
    def test_sigterm_stops_the_filter_and_removes_its_socket(self):
        listen = self.tmp / "term.sock"
        process = self.start_filter(listen, [self.keys.a_fingerprint])
        process.send_signal(signal.SIGTERM)
        self.assertEqual(_wait_for_exit(process), 0)
        process.stderr.close()
        self.assertFalse(listen.exists())

    def test_the_filter_stops_when_the_process_it_watches_is_gone(self):
        parent = subprocess.Popen(["sleep", "60"])
        listen = self.tmp / "parent.sock"
        try:
            process = self.start_filter(listen, [self.keys.a_fingerprint], "--parent-pid", str(parent.pid))
        finally:
            parent.kill()
            parent.wait(timeout=WAIT_SECONDS)
        self.assertEqual(_wait_for_exit(process), 0)
        process.stderr.close()
        self.assertFalse(listen.exists())

    def test_an_existing_listen_path_is_never_replaced(self):
        listen = self.tmp / "taken.sock"
        listen.write_text("someone else's\n")
        result = _run(
            [sys.executable, "-I", str(HELPER), "--listen", str(listen), "--upstream", str(self.upstream),
             "--allow", self.keys.a_fingerprint],
            self.base_env,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("exists", result.stderr)
        self.assertEqual(listen.read_text(), "someone else's\n")

    def test_no_allowed_key_is_a_usage_error(self):
        result = _run(
            [sys.executable, "-I", str(HELPER), "--listen", str(self.tmp / "none.sock"), "--upstream", str(self.upstream)],
            self.base_env,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.tmp / "none.sock").exists())

    def test_a_bad_fingerprint_is_a_usage_error(self):
        result = _run(
            [sys.executable, "-I", str(HELPER), "--listen", str(self.tmp / "bad.sock"), "--upstream", str(self.upstream),
             "--allow", "MD5:00:11"],
            self.base_env,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.tmp / "bad.sock").exists())

    def test_an_unreachable_upstream_answers_failure(self):
        listen = self.tmp / "dead-upstream.sock"
        argv = [sys.executable, "-I", str(HELPER), "--listen", str(listen), "--upstream", str(self.tmp / "no-agent"),
                "--allow", self.keys.a_fingerprint]
        process = subprocess.Popen(argv, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, env=self.base_env)
        try:
            _wait_for_socket(listen, process)
            listed = _run(["ssh-add", "-l"], {**self.base_env, "SSH_AUTH_SOCK": str(listen)})
            self.assertNotEqual(listed.returncode, 0)
        finally:
            process.terminate()
            _wait_for_exit(process)
            log = process.stderr.read().decode()
            process.stderr.close()
        self.assertIn("upstream", log)


if __name__ == "__main__":
    unittest.main()
