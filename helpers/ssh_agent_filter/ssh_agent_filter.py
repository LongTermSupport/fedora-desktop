#!/usr/bin/env python3
"""ccy's one-key agent: an ssh-agent socket that offers only the allowed key (Plan 00163).

It listens on a socket of its own and relays to the owner's agent (`--upstream`, else
SSH_AUTH_SOCK), answering by the SSH agent protocol (draft-miller-ssh-agent):

  REQUEST_IDENTITIES (11)  the upstream's answer, keeping only the allowed key(s)
  SIGN_REQUEST (13)        relayed when it names an allowed key, else FAILURE
  anything else            FAILURE: add, remove, remove-all, lock, unlock, smartcard,
                           and every extension (27), session-bind included

A key is allowed by its SHA256 fingerprint, as `ssh-keygen -l` and `ssh-add -l` print it.
Each client gets its own upstream connection, opened when it first needs one. A message is
at most MAX_MESSAGE bytes; anything longer, or malformed framing, closes that client only.
No key, signature or data is logged: the log names message types and errors.

The launcher (files/var/local/claude-yolo/lib/ssh-handling.bash) runs it in the background
for one session, inside an owner-only directory on XDG_RUNTIME_DIR, and stops it with
SIGTERM; `--parent-pid` names the launcher, and the filter also stops when that is gone.
Deployed beside the launcher's libraries by playbooks/imports/play-claude-yolo.yml, and run
there by path, so it imports nothing but the standard library.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import os
import re
import signal
import socket
import struct
import sys
import threading

SSH_AGENT_FAILURE = 5
SSH_AGENTC_REQUEST_IDENTITIES = 11
SSH_AGENT_IDENTITIES_ANSWER = 12
SSH_AGENTC_SIGN_REQUEST = 13
SSH_AGENTC_EXTENSION = 27

# OpenSSH's own agent refuses messages over 256 KiB (AGENT_MAX_LEN in authfd.h).
MAX_MESSAGE = 256 * 1024
PARENT_POLL_SECONDS = 1.0
FAILURE_MESSAGE = bytes([SSH_AGENT_FAILURE])

_FINGERPRINT = re.compile(r"SHA256:[A-Za-z0-9+/]{43}")


class ProtocolError(Exception):
    """A message that does not follow the agent protocol's framing or layout."""


def log(text: str) -> None:
    print(f"ccy ssh-agent filter: {text}", file=sys.stderr, flush=True)


def fingerprint(blob: bytes) -> str:
    digest = base64.b64encode(hashlib.sha256(blob).digest()).decode().rstrip("=")
    return f"SHA256:{digest}"


def parse_fingerprint(text: str) -> str:
    if not _FINGERPRINT.fullmatch(text):
        raise ValueError(f"not a SHA256 key fingerprint: {text!r}")
    return text


def _read_string(payload: bytes, offset: int) -> tuple[bytes, int]:
    if offset + 4 > len(payload):
        raise ProtocolError("string length runs past the message")
    (length,) = struct.unpack_from(">I", payload, offset)
    start = offset + 4
    end = start + length
    if end > len(payload):
        raise ProtocolError("string runs past the message")
    return payload[start:end], end


def _string(data: bytes) -> bytes:
    return struct.pack(">I", len(data)) + data


def classify(request: bytes) -> str:
    if request[:1] == bytes([SSH_AGENTC_REQUEST_IDENTITIES]):
        return "identities"
    if request[:1] == bytes([SSH_AGENTC_SIGN_REQUEST]):
        return "sign"
    return "refuse"


def filter_identities(answer: bytes, allowed: set[str]) -> bytes:
    if answer[:1] != bytes([SSH_AGENT_IDENTITIES_ANSWER]) or len(answer) < 5:
        raise ProtocolError("not an identities answer")
    (count,) = struct.unpack_from(">I", answer, 1)
    offset = 5
    kept = []
    for _ in range(count):
        blob, offset = _read_string(answer, offset)
        comment, offset = _read_string(answer, offset)
        if fingerprint(blob) in allowed:
            kept.append(_string(blob) + _string(comment))
    if offset != len(answer):
        raise ProtocolError("identities answer has trailing bytes")
    return bytes([SSH_AGENT_IDENTITIES_ANSWER]) + struct.pack(">I", len(kept)) + b"".join(kept)


def sign_request_key_blob(request: bytes) -> bytes:
    blob, offset = _read_string(request, 1)
    _data, offset = _read_string(request, offset)
    if offset + 4 != len(request):
        raise ProtocolError("sign request is not key, data and flags")
    return blob


def _receive_exactly(sock: socket.socket, length: int) -> bytes:
    chunks = []
    remaining = length
    while remaining:
        chunk = sock.recv(min(remaining, 65536))
        if not chunk:
            raise ProtocolError("connection closed inside a message")
        chunks.append(chunk)
        remaining -= len(chunk)
    return b"".join(chunks)


def read_message(sock: socket.socket) -> bytes | None:
    """One message's payload, or None when the peer closed between messages."""
    first = sock.recv(4)
    if not first:
        return None
    header = first + (_receive_exactly(sock, 4 - len(first)) if len(first) < 4 else b"")
    (length,) = struct.unpack(">I", header)
    if length == 0 or length > MAX_MESSAGE:
        raise ProtocolError(f"message length {length} is outside 1..{MAX_MESSAGE}")
    return _receive_exactly(sock, length)


def write_message(sock: socket.socket, payload: bytes) -> None:
    sock.sendall(struct.pack(">I", len(payload)) + payload)


class _Client:
    """One client connection, with its own upstream connection opened when first needed."""

    def __init__(self, client: socket.socket, upstream_path: str, allowed: set[str]) -> None:
        self.client = client
        self.upstream_path = upstream_path
        self.allowed = allowed
        self.upstream: socket.socket | None = None

    def _relay(self, request: bytes) -> bytes:
        if self.upstream is None:
            upstream = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                upstream.connect(self.upstream_path)
            except OSError:
                upstream.close()
                raise
            self.upstream = upstream
        write_message(self.upstream, request)
        reply = read_message(self.upstream)
        if reply is None:
            raise ProtocolError("upstream agent closed the connection")
        return reply

    def answer(self, request: bytes) -> bytes:
        kind = classify(request)
        if kind == "refuse":
            log(f"refused message type {request[0]}")
            return FAILURE_MESSAGE
        if kind == "sign":
            try:
                blob = sign_request_key_blob(request)
            except ProtocolError as error:
                log(f"refused a malformed sign request: {error}")
                return FAILURE_MESSAGE
            if fingerprint(blob) not in self.allowed:
                log("refused a sign request for a key that is not allowed")
                return FAILURE_MESSAGE
        try:
            reply = self._relay(request)
        except (OSError, ProtocolError) as error:
            log(f"upstream agent {self.upstream_path}: {error}")
            self._close_upstream()
            return FAILURE_MESSAGE
        if kind == "identities":
            try:
                return filter_identities(reply, self.allowed)
            except ProtocolError as error:
                log(f"upstream identities answer unusable: {error}")
                return FAILURE_MESSAGE
        return reply

    def _close_upstream(self) -> None:
        if self.upstream is not None:
            self.upstream.close()
            self.upstream = None

    def serve(self) -> None:
        try:
            while True:
                request = read_message(self.client)
                if request is None:
                    return
                write_message(self.client, self.answer(request))
        except ProtocolError as error:
            log(f"closed a client: {error}")
        except OSError as error:
            log(f"client connection: {error}")
        finally:
            self._close_upstream()
            self.client.close()


def _parent_alive(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def _bind(listen_path: str) -> socket.socket:
    """Listen on listen_path, owner-only, appearing there only once it accepts connections."""
    if os.path.lexists(listen_path):
        raise FileExistsError(f"{listen_path} exists; the filter never replaces a path")
    staging = f"{listen_path}.{os.getpid()}.binding"
    server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    previous_umask = os.umask(0o177)
    try:
        server.bind(staging)
    finally:
        os.umask(previous_umask)
    try:
        os.chmod(staging, 0o600)
        server.listen(16)
        os.link(staging, listen_path)
    except BaseException:
        server.close()
        os.unlink(staging)
        raise
    os.unlink(staging)
    return server


class _Stop(Exception):
    pass


def _raise_stop(_signum, _frame) -> None:
    raise _Stop


def serve(listen_path: str, upstream_path: str, allowed: set[str], parent_pid: int | None) -> None:
    server = _bind(listen_path)
    signal.signal(signal.SIGTERM, _raise_stop)
    signal.signal(signal.SIGINT, _raise_stop)
    signal.signal(signal.SIGHUP, _raise_stop)
    server.settimeout(PARENT_POLL_SECONDS)
    log(f"serving {len(allowed)} key(s) on {listen_path}")
    try:
        while parent_pid is None or _parent_alive(parent_pid):
            try:
                client, _address = server.accept()
            except socket.timeout:
                continue
            client.settimeout(None)
            threading.Thread(target=_Client(client, upstream_path, allowed).serve, daemon=True).start()
        log(f"process {parent_pid} is gone; stopping")
    except _Stop:
        log("stopping on a signal")
    finally:
        server.close()
        os.unlink(listen_path)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Serve only the allowed key(s) of an ssh-agent.")
    parser.add_argument("--listen", required=True, help="socket path to create (must not exist)")
    parser.add_argument("--upstream", default=os.environ.get("SSH_AUTH_SOCK", ""), help="agent socket to relay to")
    parser.add_argument(
        "--allow", action="append", default=[], required=True, type=parse_fingerprint, help="SHA256 fingerprint"
    )
    parser.add_argument("--parent-pid", type=int, help="stop when this process is gone")
    args = parser.parse_args(argv)
    if not args.upstream:
        parser.error("no upstream agent: pass --upstream or set SSH_AUTH_SOCK")
    try:
        serve(args.listen, args.upstream, set(args.allow), args.parent_pid)
    except OSError as error:
        log(str(error))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
