#!/usr/bin/env python3
"""Plan 00161 unit U00: the probes behind triage.bash (DESIGN.md section 13, H1-H6).

Each subcommand is one triage leg. It gathers facts, appends a Markdown section to the
report, and renders no verdict (PlanScriptStandards R9). A leg exits non-zero when it could
not establish its facts; that means the fact-finding is incomplete, not that the system is
broken. Progress goes to stderr; the report file is the payload.

Subcommands:
  env        tools, OS variant and architecture
  h3-asset   download the pinned Tuwunel release asset, verify and decompress it
  h4         a throwaway Tuwunel (plain process, loopback only): every call the design
             makes, recorded and scrubbed as fixtures for U08; header logging; the
             unknown-key warning
  h5         backup by SIGUSR2, restore onto a copy, the restored server answering
  h3-unit    Tuwunel under a transient system unit with DESIGN.md section 3.5's sandbox
             and the resolver stub (needs sudo)
  h1         reachability of host addresses from ccy-image containers (rootless podman)
  h2         the same from docker, LXC and libvirt guests, where installed
  h6         Element Desktop under pasta with a packet capture
  owner      the steps only the owner can do (H6 login, H7 phone), written to the report

Nothing here changes the system beyond a scratch directory the caller removes, a transient
unit and its runtime directory (removed when it stops), a rootless podman network created
and removed by h1, and an Element profile directory created and removed by h6.
"""

from __future__ import annotations

import argparse
import base64
import dataclasses
import hashlib
import hmac
import ipaddress
import json
import os
import pathlib
import re
import secrets
import shutil
import signal
import socket
import struct
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import Counter
from typing import Any

PREFIX = "agent_bus"
PROBE_SERVER_NAME = "probe.agent-bus.internal"
TUWUNEL_VERSION = "v1.9.3"
RELEASE_API = "https://api.github.com/repos/matrix-construct/tuwunel/releases/tags/{version}"
UNKNOWN_KEY = "agent_bus_probe_unknown_key"
# Tuwunel runs these admin commands on SIGUSR2 and nothing by default (example config,
# `admin_signal_execute`), so a SIGUSR2 backup needs this key, which DESIGN.md 3.6 lacks.
SIGNAL_BACKUP: dict[str, object] = {"admin_signal_execute": ["server backup-database"]}
CCY_IMAGE = "claude-yolo:latest"
# Inside the transient unit, a tmpfs over an existing empty directory carries the read-only
# inputs, so nothing is created on the host's own filesystem.
UNIT_INPUT_DIR = "/mnt"
UNIT_RUNTIME_DIR = "agent-bus-probe"
PROBE_HUMAN = "probehuman"
PROBE_REF = "commit:example-org/myrepo@" + "0" * 40
HTTP_TIMEOUT_S = 40

_SERVER_NAME_RE = re.compile(r"[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?")
_DATA_DIR_RE = re.compile(r"/[A-Za-z0-9._/-]*")
_TOML_KEY_RE = re.compile(r"[a-z][a-z0-9_]*")
_TAG_RE = re.compile(r"[a-z0-9-]{1,48}")
_ID_RE = re.compile(r"(?<![A-Za-z0-9_-])([!$]|%21|%24)([A-Za-z0-9_-]{43})(?![A-Za-z0-9_-])")
_HEADER_RE = re.compile(r"(?i)authorization|bearer|access_token")


class ProbeError(Exception):
    """A fact could not be established; the leg fails with this message."""


def say(message: str) -> None:
    print(message, file=sys.stderr, flush=True)


# ── pure: config, identifiers, events ─────────────────────────────────────────────────────


def _toml_value(value: object) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        return str(value)
    if isinstance(value, str):
        if any(ord(ch) < 0x20 or ord(ch) > 0x7E for ch in value):
            raise ValueError(f"refusing a non-printable TOML string: {value!r}")
        return json.dumps(value)
    if isinstance(value, list):
        return "[" + ", ".join(_toml_value(item) for item in value) + "]"
    raise ValueError(f"no TOML rendering for {type(value).__name__}")


def render_tuwunel_toml(
    server_name: str,
    addresses: list[str],
    port: int,
    data_dir: str,
    extra: dict[str, object] | None = None,
    secret_file: str | None = None,
) -> str:
    """DESIGN.md section 3.6, every key, for a probe instance rooted at data_dir."""
    if not _SERVER_NAME_RE.fullmatch(server_name):
        raise ValueError(f"bad server_name: {server_name!r}")
    if not _DATA_DIR_RE.fullmatch(data_dir) or ".." in data_dir.split("/"):
        raise ValueError(f"bad data_dir: {data_dir!r}")
    if not addresses or addresses[0] != "127.0.0.1":
        raise ValueError("the first listen address must be 127.0.0.1 (DESIGN.md section 3.3)")
    for address in addresses:
        if ipaddress.ip_address(address).is_unspecified:
            raise ValueError(f"wildcard listen address refused: {address}")
    if not 1 <= port <= 65535:
        raise ValueError(f"bad port: {port}")
    root = data_dir.rstrip("/")
    keys: list[tuple[str, object]] = [
        ("server_name", server_name),
        ("address", list(addresses)),
        ("port", port),
        ("database_path", f"{root}/db"),
        ("database_backup_path", f"{root}/backups"),
        ("database_backups_to_keep", 7),
        ("allow_registration", False),
        ("allow_guest_registration", False),
        (
            "registration_shared_secret_file",
            secret_file or f"{root}/secrets/registration_shared_secret",
        ),
        ("grant_admin_to_first_user", False),
        ("login_via_token", False),
        ("allow_federation", False),
        ("trusted_servers", []),
        ("federate_admin_room", False),
        ("admin_escape_commands", False),
        ("allow_encryption", False),
        ("auto_accept_invites", False),
        ("new_user_displayname_suffix", ""),
        ("client_sync_timeout_min", 0),
        ("default_room_version", "12"),
        ("sentry", False),
        ("log", "warn"),
    ]
    for key, value in (extra or {}).items():
        if not _TOML_KEY_RE.fullmatch(key):
            raise ValueError(f"bad extra key: {key!r}")
        keys.append((key, value))
    return "".join(f"{key} = {_toml_value(value)}\n" for key, value in keys)


def registration_mac(secret: str, nonce: str, user: str, password: str, admin: bool) -> str:
    """Synapse's shared-secret registration MAC, which Tuwunel implements."""
    message = b"\x00".join(
        [nonce.encode(), user.encode(), password.encode(), b"admin" if admin else b"notadmin"]
    )
    return hmac.new(secret.encode(), message, hashlib.sha1).hexdigest()


def team_power_levels(humans: list[str]) -> dict[str, Any]:
    """PROTOCOL.md section 8, exactly."""
    return {
        "users": {human: 50 for human in humans},
        "users_default": 0,
        "events_default": 0,
        "state_default": 100,
        "invite": 100,
        "kick": 100,
        "ban": 100,
        "redact": 100,
        "notifications": {"room": 50},
        "events": {
            "m.room.power_levels": 100,
            "m.room.tombstone": 150,
            "m.room.redaction": 100,
            "m.reaction": 50,
            "m.sticker": 100,
            f"{PREFIX}.status": 0,
        },
    }


def probe_handle(n: int, sep: str) -> str:
    """A handle in PROTOCOL.md section 3's grammar, with either candidate separator."""
    if sep not in ("+", "="):
        raise ValueError(f"bad separator: {sep!r}")
    return f"probe.{n}{sep}triage.podman"


def ping_content(verb: str, ref: str | None, to: list[str]) -> dict[str, Any]:
    """PROTOCOL.md section 4's content for a ping, without validation."""
    targets = sorted(to)
    ping: dict[str, Any] = {"v": 1, "verb": verb, "to": targets}
    if ref is not None:
        ping["ref"] = ref
    body = f"[agent-bus] {verb} {ref or '-'} -> {' '.join(targets)}"
    return {
        "msgtype": "m.notice",
        "body": body,
        "m.mentions": {"user_ids": targets},
        f"{PREFIX}.ping": ping,
    }


def team_record(team: str, humans: list[str], roles: dict[str, str]) -> dict[str, Any]:
    return {
        "v": 1,
        "team": team,
        "humans": humans,
        "roles": roles,
        "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
        "path_prefixes": ["CLAUDE/Plan/"],
        "forge_api": "https://api.github.com",
    }


def fake_id(sigil: str, label: str) -> str:
    """A deterministic stand-in ID in the room/event ID grammar."""
    digest = hashlib.sha256(f"agent-bus-probe:{label}".encode()).digest()
    return sigil + base64.urlsafe_b64encode(digest).decode().rstrip("=")


def parse_tag(data: bytes) -> str:
    text = data.decode("ascii", "replace").strip()
    if not text:
        return "<none>"
    return text if _TAG_RE.fullmatch(text) else "<invalid>"


def primary_address(route: list[dict[str, Any]]) -> str:
    """The source address of the default route, from `ip -j route get`."""
    if not route or "prefsrc" not in route[0]:
        raise ValueError(f"no prefsrc in route output: {route!r}")
    return str(ipaddress.ip_address(route[0]["prefsrc"]))


_ASSET_ARCH = {"x86_64": "x86_64-v1", "aarch64": "aarch64-v8"}


def pick_asset(assets: list[dict[str, Any]], machine: str, version: str) -> dict[str, Any]:
    """The static zstd binary of the "all" feature set (x86_64: the x86-64-v1 build)."""
    if machine not in _ASSET_ARCH:
        raise ValueError(f"no known release asset for architecture {machine}")
    wanted = f"{version}-release-all-{_ASSET_ARCH[machine]}-linux-gnu-tuwunel.zst"
    found = [asset for asset in assets if asset["name"] == wanted]
    if len(found) != 1:
        names = ", ".join(asset["name"] for asset in assets)
        raise ValueError(f"expected one asset named {wanted}, found {len(found)} among: {names}")
    return found[0]


_ANSI_RE = re.compile(r"\x1b\[[0-9;]*m")
_LEVEL_RE = re.compile(r"\b(WARN|ERROR)\b\s*(.*)")


def log_findings(text: str) -> list[str]:
    """Distinct WARN and ERROR messages of a Tuwunel log, timestamps and colour removed."""
    found: list[str] = []
    for line in text.splitlines():
        match = _LEVEL_RE.search(_ANSI_RE.sub("", line))
        if match:
            entry = f"{match.group(1)} {match.group(2).strip()}"
            if entry not in found:
                found.append(entry)
    return found


def unit_properties(
    port: int,
    addresses: list[str],
    binary: str,
    toml: str,
    secret: str,
    stub: str,
    notify: bool,
) -> list[str]:
    """DESIGN.md section 3.5's sandbox for a transient probe unit (DynamicUser stands in for
    the agent-bus user, which U16 creates)."""
    allow = ["127.0.0.1/32", "::1/128"]
    for address in addresses:
        parsed = ipaddress.ip_address(address)
        if parsed.is_loopback:
            continue
        allow.append(f"{parsed}/{parsed.max_prefixlen}")
    return [
        f"Type={'notify' if notify else 'simple'}",
        "DynamicUser=yes",
        f"RuntimeDirectory={UNIT_RUNTIME_DIR}",
        f"Environment=TUWUNEL_CONFIG={UNIT_INPUT_DIR}/tuwunel.toml",
        f"TemporaryFileSystem={UNIT_INPUT_DIR}:ro",
        f"BindReadOnlyPaths={binary}:{UNIT_INPUT_DIR}/tuwunel",
        f"BindReadOnlyPaths={toml}:{UNIT_INPUT_DIR}/tuwunel.toml",
        f"BindReadOnlyPaths={secret}:{UNIT_INPUT_DIR}/registration_shared_secret",
        "TimeoutStartSec=90",
        "TimeoutStopSec=330",
        "NoNewPrivileges=yes",
        "ProtectSystem=strict",
        "ProtectHome=yes",
        "PrivateTmp=yes",
        "PrivateDevices=yes",
        "ProtectKernelTunables=yes",
        "ProtectKernelModules=yes",
        "ProtectControlGroups=yes",
        "RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX",
        "SocketBindDeny=any",
        f"SocketBindAllow=tcp:{port}",
        "IPAccounting=yes",
        "IPAddressDeny=any",
        "IPAddressAllow=" + " ".join(allow),
        "TemporaryFileSystem=/run/systemd/resolve:ro",
        f"BindReadOnlyPaths={stub}:/run/systemd/resolve/stub-resolv.conf",
        f"BindReadOnlyPaths={stub}:/run/systemd/resolve/resolv.conf",
        "InaccessiblePaths=-/run/dbus",
    ]


# ── pure: scrubbing and logs ──────────────────────────────────────────────────────────────


class Scrubber:
    """Replaces secrets with labels and room/event IDs with stable fakes."""

    def __init__(self) -> None:
        self._secrets: dict[str, str] = {}
        self._ids: dict[str, str] = {}
        self._counts: Counter[str] = Counter()

    def add_secret(self, value: str, label: str) -> None:
        if not value:
            raise ValueError(f"empty secret for {label}")
        self._secrets[value] = label

    def discover_ids(self, texts: list[str]) -> None:
        for text in texts:
            for match in _ID_RE.finditer(text):
                body = match.group(2)
                if body in self._ids:
                    continue
                kind = "room" if match.group(1) in ("!", "%21") else "event"
                self._counts[kind] += 1
                self._ids[body] = f"{kind}-{self._counts[kind]}"

    def scrub(self, text: str) -> str:
        for value in sorted(self._secrets, key=len, reverse=True):
            replacement = f"<{self._secrets[value]}>"
            text = text.replace(value, replacement)
            text = text.replace(urllib.parse.quote(value, safe=""), replacement)
        for body, label in self._ids.items():
            sigil = "!" if label.startswith("room") else "$"
            text = text.replace(body, fake_id(sigil, label)[1:])
        return text

    def leaks(self, text: str) -> list[str]:
        found = []
        for value, label in self._secrets.items():
            if value in text or urllib.parse.quote(value, safe="") in text:
                found.append(label)
        for body, label in self._ids.items():
            if body in text:
                found.append(label)
        return found

    def secret_values(self) -> list[str]:
        return list(self._secrets)


def scrub_json(obj: Any, scrubber: Scrubber) -> Any:
    return json.loads(scrubber.scrub(json.dumps(obj)))


@dataclasses.dataclass(frozen=True)
class LogScan:
    secret_hits: int
    header_lines: int
    total_lines: int
    scrubbed: str


def scan_log(text: str, secret_values: list[str]) -> LogScan:
    values = sorted((value for value in secret_values if value), key=len, reverse=True)
    hits = sum(text.count(value) for value in values)
    scrubbed = text
    for value in values:
        scrubbed = scrubbed.replace(value, "<secret>")
    lines = text.splitlines()
    headers = sum(1 for line in lines if _HEADER_RE.search(line))
    return LogScan(hits, headers, len(lines), scrubbed)


def lines_naming(text: str, key: str) -> list[str]:
    pattern = re.compile(rf"(?<![A-Za-z0-9_]){re.escape(key)}(?![A-Za-z0-9_])")
    return [line for line in text.splitlines() if pattern.search(line)]


# ── pure: pcap ────────────────────────────────────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class Packet:
    proto: str
    src: str
    dst: str
    sport: int | None
    dport: int | None


def _parse_ip(data: bytes) -> Packet:
    version = data[0] >> 4 if data else 0
    if version == 4 and len(data) >= 20:
        ihl = (data[0] & 0x0F) * 4
        number = data[9]
        src = str(ipaddress.IPv4Address(data[12:16]))
        dst = str(ipaddress.IPv4Address(data[16:20]))
        payload = data[ihl:]
    elif version == 6 and len(data) >= 40:
        number = data[6]
        src = str(ipaddress.IPv6Address(data[8:24]))
        dst = str(ipaddress.IPv6Address(data[24:40]))
        payload = data[40:]
    else:
        return Packet(f"ip-version-{version}", "", "", None, None)
    names = {6: "tcp", 17: "udp", 1: "icmp", 58: "icmpv6"}
    proto = names.get(number, f"ipproto-{number}")
    if proto in ("tcp", "udp") and len(payload) >= 4:
        sport, dport = struct.unpack("!HH", payload[:4])
        return Packet(proto, src, dst, sport, dport)
    return Packet(proto, src, dst, None, None)


def parse_pcap(data: bytes) -> list[Packet]:
    """Classic pcap (as pasta --pcap writes), Ethernet, raw IP or Linux cooked frames."""
    if len(data) < 24:
        raise ValueError("pcap too short")
    magic = struct.unpack("<I", data[:4])[0]
    if magic in (0xA1B2C3D4, 0xA1B23C4D):
        endian = "<"
    elif magic in (0xD4C3B2A1, 0x4D3CB2A1):
        endian = ">"
    else:
        raise ValueError(f"not a pcap file (magic 0x{magic:08x})")
    linktype = struct.unpack(endian + "I", data[20:24])[0]
    packets: list[Packet] = []
    offset = 24
    while offset + 16 <= len(data):
        incl = struct.unpack(endian + "IIII", data[offset : offset + 16])[2]
        frame = data[offset + 16 : offset + 16 + incl]
        offset += 16 + incl
        if linktype == 1:
            ethertype = struct.unpack("!H", frame[12:14])[0] if len(frame) >= 14 else 0
            payload = frame[14:]
        elif linktype == 113:
            ethertype = struct.unpack("!H", frame[14:16])[0] if len(frame) >= 16 else 0
            payload = frame[16:]
        elif linktype in (12, 101):
            ethertype = 0x0800 if frame[:1] and frame[0] >> 4 == 4 else 0x86DD
            payload = frame
        else:
            raise ValueError(f"unsupported pcap link type {linktype}")
        if ethertype in (0x0800, 0x86DD):
            packets.append(_parse_ip(payload))
        else:
            packets.append(Packet(f"ethertype-0x{ethertype:04x}", "", "", None, None))
    return packets


def summarise_packets(packets: list[Packet]) -> Counter[tuple[str, str, int | None]]:
    return Counter((packet.proto, packet.dst, packet.dport) for packet in packets)


# ── side effects: report, subprocess, HTTP, the Tuwunel process ───────────────────────────


class Report:
    def __init__(self, path: pathlib.Path) -> None:
        self.path = path

    def write(self, text: str) -> None:
        with self.path.open("a", encoding="utf-8") as handle:
            handle.write(text.rstrip("\n") + "\n\n")

    def table(self, header: list[str], rows: list[list[object]]) -> None:
        out = ["| " + " | ".join(header) + " |", "|" + "---|" * len(header)]
        for row in rows:
            cells = [str(cell).replace("|", "\\|").replace("\n", " ") for cell in row]
            out.append("| " + " | ".join(cells) + " |")
        self.write("\n".join(out))


def run(argv: list[str], timeout: float = 120) -> subprocess.CompletedProcess[str]:
    """Run a command whose exit status is a recorded result: every caller inspects it."""
    return subprocess.run(argv, check=False, capture_output=True, text=True, timeout=timeout)


def run_ok(argv: list[str], timeout: float = 120) -> str:
    result = run(argv, timeout=timeout)
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip()
        raise ProbeError(f"{' '.join(argv)} exited {result.returncode}: {detail}")
    return result.stdout


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(
        self,
        req: urllib.request.Request,
        fp: Any,
        code: int,
        msg: str,
        headers: Any,
        newurl: str,
    ) -> urllib.request.Request | None:
        raise urllib.error.HTTPError(req.full_url, code, f"redirect refused: {msg}", headers, fp)


_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}), _NoRedirect())


@dataclasses.dataclass
class Exchange:
    name: str
    method: str
    path: str
    query: dict[str, str]
    auth: str | None
    body: Any
    status: int
    response: Any
    elapsed_ms: int


class Http:
    """Records every exchange; the bearer token is sent unredirected and recorded by label."""

    def __init__(self, base_url: str) -> None:
        self.base_url = base_url
        self.exchanges: list[Exchange] = []
        self._lock = threading.Lock()

    def call(
        self,
        name: str,
        method: str,
        path: str,
        body: Any = None,
        token: tuple[str, str] | None = None,
        query: dict[str, str] | None = None,
        expect: int | None = None,
    ) -> tuple[int, Any]:
        url = self.base_url + path + ("?" + urllib.parse.urlencode(query) if query else "")
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(url, data=data, method=method)
        if data is not None:
            request.add_header("Content-Type", "application/json")
        if token is not None:
            request.add_unredirected_header("Authorization", f"Bearer {token[1]}")
        started = time.monotonic()
        try:
            with _OPENER.open(request, timeout=HTTP_TIMEOUT_S) as response:
                status, raw = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, raw = error.code, error.read()
        elapsed = int((time.monotonic() - started) * 1000)
        text = raw.decode("utf-8", "replace")
        try:
            parsed: Any = json.loads(text) if text else None
        except json.JSONDecodeError:
            parsed = {"non_json_body": text[:2000]}
        auth = f"<{token[0]}>" if token else None
        exchange = Exchange(
            name, method, path, dict(query or {}), auth, body, status, parsed, elapsed
        )
        with self._lock:
            self.exchanges.append(exchange)
        say(f"  {name}: {method} {path} -> {status} ({elapsed} ms)")
        if expect is not None and status != expect:
            errcode = parsed.get("errcode") if isinstance(parsed, dict) else None
            raise ProbeError(f"{name}: expected {expect}, got {status} ({errcode})")
        return status, parsed


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def _server_answers(url: str) -> bool:
    try:
        with _OPENER.open(url, timeout=2) as response:
            return bool(response.status == 200)
    except (urllib.error.URLError, ConnectionError, TimeoutError):
        return False


class Tuwunel:
    """A Tuwunel run as a plain process of the invoking user, in a scratch directory."""

    def __init__(
        self,
        binary: pathlib.Path,
        data_dir: pathlib.Path,
        port: int,
        extra: dict[str, object] | None = None,
    ) -> None:
        self.binary = binary
        self.data_dir = data_dir
        self.port = port
        self.base_url = f"http://127.0.0.1:{port}"
        self.log_path = data_dir / "tuwunel.log"
        self.secret_path = data_dir / "secrets" / "registration_shared_secret"
        (data_dir / "secrets").mkdir(parents=True, exist_ok=True, mode=0o700)
        (data_dir / "backups").mkdir(exist_ok=True, mode=0o700)
        if not self.secret_path.exists():
            self.secret_path.write_text(secrets.token_hex(64), encoding="ascii")
            self.secret_path.chmod(0o600)
        self.secret = self.secret_path.read_text(encoding="ascii").strip()
        self.toml_path = data_dir / "tuwunel.toml"
        self.toml_path.write_text(
            render_tuwunel_toml(PROBE_SERVER_NAME, ["127.0.0.1"], port, str(data_dir), extra),
            encoding="ascii",
        )
        self.proc: subprocess.Popen[bytes] | None = None
        self._log_handle: Any = None

    def start(self, extra_args: list[str] | None = None) -> None:
        env = {
            "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
            "TUWUNEL_CONFIG": str(self.toml_path),
        }
        self._log_handle = self.log_path.open("ab")
        self.proc = subprocess.Popen(
            [str(self.binary), *(extra_args or [])],
            env=env,
            stdout=self._log_handle,
            stderr=subprocess.STDOUT,
            stdin=subprocess.DEVNULL,
            cwd=self.data_dir,
        )
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            if self.proc.poll() is not None:
                raise ProbeError(
                    f"Tuwunel exited {self.proc.returncode} before it was ready; "
                    f"log: {self.log_tail()}"
                )
            if _server_answers(self.base_url + "/_tuwunel/server_version"):
                return
            time.sleep(0.5)
        raise ProbeError(f"Tuwunel not ready after 120 s; log: {self.log_tail()}")

    def signal(self, signum: int) -> None:
        if self.proc is None or self.proc.poll() is not None:
            raise ProbeError("Tuwunel is not running")
        self.proc.send_signal(signum)

    def stop(self) -> int | None:
        if self.proc is None:
            return None
        if self.proc.poll() is None:
            self.proc.send_signal(signal.SIGTERM)
            try:
                self.proc.wait(timeout=60)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait(timeout=30)
                self._close()
                raise ProbeError("Tuwunel did not stop within 60 s of SIGTERM; killed") from None
        self._close()
        return self.proc.returncode

    def _close(self) -> None:
        if self._log_handle is not None:
            self._log_handle.close()
            self._log_handle = None

    def log_text(self) -> str:
        if not self.log_path.exists():
            return ""
        return self.log_path.read_text(encoding="utf-8", errors="replace")

    def log_tail(self, lines: int = 15) -> str:
        return " / ".join(self.log_text().splitlines()[-lines:])


def binary_path(scratch: pathlib.Path) -> pathlib.Path:
    path = scratch / "tuwunel"
    if not path.is_file():
        raise ProbeError(f"no Tuwunel binary at {path}: the H3 asset leg did not complete")
    return path


def user_id(localpart: str) -> str:
    return f"@{localpart}:{PROBE_SERVER_NAME}"


def q(value: str) -> str:
    return urllib.parse.quote(value, safe="")


def shared_secret_register(
    http: Http,
    server: Tuwunel,
    scrubber: Scrubber,
    name: str,
    username: str,
    admin: bool,
    label: str,
) -> tuple[int, Any]:
    _, nonce_body = http.call(f"{name}-nonce", "GET", "/_synapse/admin/v1/register", expect=200)
    nonce = nonce_body["nonce"]
    password = secrets.token_urlsafe(24)
    scrubber.add_secret(password, f"password:{label}")
    mac = registration_mac(server.secret, nonce, username, password, admin)
    scrubber.add_secret(mac, f"mac:{label}")
    status, body = http.call(
        name,
        "POST",
        "/_synapse/admin/v1/register",
        {"nonce": nonce, "username": username, "password": password, "admin": admin, "mac": mac},
    )
    if status == 200:
        scrubber.add_secret(body["access_token"], f"token:{label}")
    return status, body


# ── legs ──────────────────────────────────────────────────────────────────────────────────

_TOOLS = (
    "podman", "docker", "lxc-ls", "lxc-attach", "virsh", "flatpak", "pasta", "zstd", "curl",
    "ss", "ip", "firewall-cmd", "systemd-run", "tcpdump", "sudo", "nmcli",
)


def leg_env(args: argparse.Namespace, report: Report) -> None:
    os_release: dict[str, str] = {}
    for line in pathlib.Path("/etc/os-release").read_text(encoding="utf-8").splitlines():
        key, sep, value = line.partition("=")
        if sep:
            os_release[key] = value.strip('"')
    systemd = run_ok(["systemctl", "--version"]).splitlines()[0]
    if shutil.which("podman"):
        exists = run(["podman", "image", "exists", CCY_IMAGE]).returncode == 0
        image = "present" if exists else "absent"
    else:
        image = "absent (no podman)"
    release = " / ".join(
        os_release.get(key, "-") for key in ("ID", "VERSION_ID", "VARIANT_ID")
    )
    report.write("## Environment")
    report.table(
        ["Fact", "Value"],
        [
            ["os ID / VERSION_ID / VARIANT_ID", release],
            ["machine", os.uname().machine],
            ["kernel", os.uname().release],
            ["systemd", systemd],
            ["python", sys.version.split()[0]],
            [f"podman image {CCY_IMAGE}", image],
        ],
    )
    report.table(["Tool", "Path"], [[tool, shutil.which(tool) or "absent"] for tool in _TOOLS])


def leg_h3_asset(args: argparse.Namespace, report: Report) -> None:
    scratch = pathlib.Path(args.scratch)
    scratch.mkdir(parents=True, exist_ok=True)
    headers = {"Accept": "application/vnd.github+json", "User-Agent": "agent-bus-probe"}
    url = RELEASE_API.format(version=args.tuwunel_version)
    with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=60) as response:
        release = json.loads(response.read())
    assets = release["assets"]
    machine = os.uname().machine
    asset = pick_asset(assets, machine, args.tuwunel_version)
    say(f"  downloading {asset['name']}")
    compressed = scratch / asset["name"]
    digest = hashlib.sha256()
    download = urllib.request.Request(
        asset["browser_download_url"], headers={"User-Agent": "agent-bus-probe"}
    )
    with urllib.request.urlopen(download, timeout=300) as response, compressed.open("wb") as out:
        while chunk := response.read(1 << 20):
            digest.update(chunk)
            out.write(chunk)
    sha = digest.hexdigest()
    api_digest = asset.get("digest") or "-"
    if api_digest != "-" and api_digest != f"sha256:{sha}":
        raise ProbeError(f"sha256 {sha} differs from the release API's {api_digest}")
    binary = scratch / "tuwunel"
    run_ok(["zstd", "-q", "-d", "-f", "-o", str(binary), str(compressed)], timeout=300)
    binary.chmod(0o755)
    binary_sha = hashlib.sha256(binary.read_bytes()).hexdigest()
    version = run(["/usr/bin/env", "-i", str(binary), "--version"], timeout=30)
    version_text = (version.stdout or version.stderr).strip()[:200]
    report.write(f"## H3 Tuwunel release asset ({args.tuwunel_version}, {machine})")
    report.table(
        ["Fact", "Value"],
        [
            ["asset", asset["name"]],
            ["asset sha256 (computed)", sha],
            ["asset digest (release API)", api_digest],
            ["asset size", asset.get("size", "-")],
            ["decompressed sha256", binary_sha],
            ["`--version` exit / output", f"{version.returncode} / {version_text}"],
        ],
    )
    listing = "\n".join(f"- `{a['name']}` {a.get('digest') or ''}" for a in assets)
    report.write("All release assets:\n\n" + listing)


def _sync_filter(room: str, limit: int) -> str:
    types = [f"{PREFIX}.team", f"{PREFIX}.status"]
    return json.dumps(
        {
            "presence": {"types": []},
            "account_data": {"types": []},
            "room": {
                "rooms": [room],
                "ephemeral": {"types": []},
                "account_data": {"types": []},
                "state": {
                    "types": ["m.room.create", "m.room.power_levels", "m.room.member", *types],
                    "lazy_load_members": True,
                },
                "timeline": {
                    "types": ["m.room.message", "m.room.member", "m.room.power_levels", *types],
                    "limit": limit,
                },
            },
        },
        separators=(",", ":"),
    )


class _Flows:
    """H4: every call the design makes, in order, against one throwaway server."""

    def __init__(self, http: Http, server: Tuwunel, scrubber: Scrubber) -> None:
        self.http = http
        self.server = server
        self.scrubber = scrubber
        self.facts: list[list[object]] = []
        self.tokens: dict[str, tuple[str, str]] = {}

    def fact(self, name: str, value: object) -> None:
        self.facts.append([name, value])

    def token(self, label: str, value: str) -> tuple[str, str]:
        self.scrubber.add_secret(value, f"token:{label}")
        self.tokens[label] = (f"token:{label}", value)
        return self.tokens[label]

    def password(self, label: str) -> str:
        value = secrets.token_urlsafe(24)
        self.scrubber.add_secret(value, f"password:{label}")
        return value

    def login(self, name: str, localpart: str, password: str) -> dict[str, Any]:
        _, body = self.http.call(
            name,
            "POST",
            "/_matrix/client/v3/login",
            {
                "type": "m.login.password",
                "identifier": {"type": "m.id.user", "user": localpart},
                "password": password,
                "initial_device_display_name": "probe",
            },
            expect=200,
        )
        return dict(body)

    def run(self) -> None:
        self.unauthenticated()
        admin = self.registration()
        sep = self.separator(admin)
        sender, receiver, human = self.accounts(admin, sep)
        self.queries(admin, sender)
        self.password_resets(admin, human)
        room = self.room(admin, sender, receiver, human)
        self.messages(room, sender, receiver)

    def unauthenticated(self) -> None:
        call = self.http.call
        call("client-versions", "GET", "/_matrix/client/versions", expect=200)
        call("server-version", "GET", "/_tuwunel/server_version", expect=200)
        _, flows = call("login-flows", "GET", "/_matrix/client/v3/login", expect=200)
        types = ", ".join(flow.get("type", "?") for flow in flows.get("flows", []))
        self.fact("GET /login flows", types)

    def registration(self) -> tuple[str, str]:
        call = self.http.call
        _, nonce = call(
            "register-wrong-mac-nonce", "GET", "/_synapse/admin/v1/register", expect=200
        )
        status, body = call(
            "register-wrong-mac",
            "POST",
            "/_synapse/admin/v1/register",
            {"nonce": nonce["nonce"], "username": "wrongmac", "password": "x" * 24,
             "admin": False, "mac": "0" * 40},
        )
        errcode = body.get("errcode") if isinstance(body, dict) else ""
        self.fact("shared-secret register, wrong MAC", f"{status} {errcode}")
        status, body = shared_secret_register(
            self.http, self.server, self.scrubber, "register-admin", "admin", True, "admin"
        )
        if status != 200:
            raise ProbeError(f"shared-secret admin registration failed: {status} {body}")
        self.fact("shared-secret register admin", status)
        return self.token("admin", body["access_token"])

    def separator(self, admin: tuple[str, str]) -> str:
        explicit = user_id("explicitnonadmin")
        status, body = self.http.call(
            "put-user-admin-false", "PUT", f"/_synapse/admin/v2/users/{q(explicit)}",
            {"password": self.password("explicit"), "admin": False}, token=admin,
        )
        error = body.get("error") if isinstance(body, dict) else ""
        self.fact("`PUT v2/users` creating with `\"admin\": false`", f"{status} {error}")
        results: dict[str, dict[str, int]] = {}
        for n, sep in ((1, "+"), (2, "=")):
            status, _ = shared_secret_register(
                self.http, self.server, self.scrubber, f"register-handle-{n}",
                probe_handle(n, sep), False, f"handle-{n}",
            )
            other = probe_handle(n + 10, sep)
            put, _ = self.http.call(
                f"put-user-handle-{n}", "PUT", f"/_synapse/admin/v2/users/{q(user_id(other))}",
                {"password": self.password(f"put-{n}"), "displayname": other},
                token=admin,
            )
            results[sep] = {"v1/register": status, "PUT v2/users": put}
            summary = ", ".join(f"{k} {v}" for k, v in results[sep].items())
            self.fact(f"handle with `{sep}`", summary)
        for sep in ("+", "="):
            if all(code in (200, 201) for code in results[sep].values()):
                self.fact("separator the rest of this run uses", sep)
                return sep
        raise ProbeError(f"neither separator creates accounts: {results}")

    def accounts(self, admin: tuple[str, str], sep: str) -> tuple[str, str, str]:
        sender, receiver = user_id(probe_handle(20, sep)), user_id(probe_handle(21, sep))
        human = user_id(PROBE_HUMAN)
        for label, uid in (("sender", sender), ("receiver", receiver), ("human", human)):
            password = self.password(label)
            status, _ = self.http.call(
                f"put-user-{label}", "PUT", f"/_synapse/admin/v2/users/{q(uid)}",
                {"password": password, "displayname": uid[1:].split(":")[0]},
                token=admin,
            )
            if status not in (200, 201):
                raise ProbeError(f"PUT v2/users for the {label} account answered {status}")
            if label == "human":
                self.token("human", self.login("human-password-login", PROBE_HUMAN, password)["access_token"])
                continue
            _, minted = self.http.call(
                f"login-mint-{label}", "POST", f"/_synapse/admin/v1/users/{q(uid)}/login", {},
                token=admin, expect=200,
            )
            self.token(label, minted["access_token"])
        self.http.call(
            "whoami-sender", "GET", "/_matrix/client/v3/account/whoami",
            token=self.tokens["sender"], expect=200,
        )
        return sender, receiver, human

    def queries(self, admin: tuple[str, str], sender: str) -> None:
        status, _ = self.http.call(
            "admin-query-admins", "GET", "/_synapse/admin/v2/users", token=admin,
            query={"admins": "true"},
        )
        self.fact("`GET v2/users?admins=true`", status)
        for label, uid in (("admin", user_id("admin")), ("sender", sender)):
            status, body = self.http.call(
                f"admin-query-{label}", "GET", f"/_synapse/admin/v2/users/{q(uid)}", token=admin
            )
            value = body.get("admin") if isinstance(body, dict) else None
            self.fact(f"`GET v2/users/{label}` status / admin field", f"{status} / {value}")
        status, _ = self.http.call(
            "member-token-on-admin-api", "GET", f"/_synapse/admin/v2/users/{q(sender)}",
            token=self.tokens["sender"],
        )
        self.fact("member token on an admin endpoint", status)

    def password_resets(self, admin: tuple[str, str], human: str) -> None:
        call = self.http.call
        whoami = "/_matrix/client/v3/account/whoami"
        first = self.password("human-reset-1")
        status, _ = call(
            "reset-password-v1", "POST", f"/_synapse/admin/v1/reset_password/{q(human)}",
            {"new_password": first, "logout_devices": True}, token=admin,
        )
        self.fact("`POST v1/reset_password` + logout_devices", status)
        old, _ = call("whoami-human-after-v1-reset", "GET", whoami, token=self.tokens["human"])
        self.fact("old human token after the v1 reset", old)
        if status != 200:
            first = self.password("human-reset-fallback")
            call("put-user-human-password", "PUT", f"/_synapse/admin/v2/users/{q(human)}",
                 {"password": first}, token=admin, expect=200)
        before = self.token("human-2", self.login("human-password-login-2", PROBE_HUMAN, first)["access_token"])
        second = self.password("human-reset-2")
        status, _ = call(
            "put-user-password-logout", "PUT", f"/_synapse/admin/v2/users/{q(human)}",
            {"password": second, "logout_devices": True}, token=admin,
        )
        self.fact("`PUT v2/users` password + logout_devices", status)
        old, _ = call("whoami-human-after-put-reset", "GET", whoami, token=before)
        self.fact("old human token after the PUT reset", old)
        self.token("human", self.login("human-password-login-3", PROBE_HUMAN, second)["access_token"])
        status, body = call("login-get-token", "POST", "/_matrix/client/v1/login/get_token", {},
                            token=self.tokens["human"])
        detail = ""
        if isinstance(body, dict):
            flows = body.get("flows") or []
            detail = body.get("errcode") or " ".join("+".join(f.get("stages", [])) for f in flows)
        self.fact("a session asks to mint a login token (`login_via_existing_session`)",
                  f"{status} {detail}")

    def room(self, admin: tuple[str, str], sender: str, receiver: str, human: str) -> str:
        call = self.http.call
        record = team_record("probe", [human], {sender: "orchestrator", receiver: "worker"})
        _, created = call(
            "create-room", "POST", "/_matrix/client/v3/createRoom",
            {
                "preset": "private_chat",
                "visibility": "private",
                "name": "probe",
                "topic": "agent bus probe",
                "room_version": "12",
                "power_level_content_override": team_power_levels([human]),
                "initial_state": [{"type": f"{PREFIX}.team", "state_key": "", "content": record}],
            },
            token=admin, expect=200,
        )
        room = str(created["room_id"])
        rq = q(room)
        _, create = call(
            "state-create-format-event", "GET", f"/_matrix/client/v3/rooms/{rq}/state/m.room.create/",
            token=admin, query={"format": "event"}, expect=200,
        )
        version = create.get("content", {}).get("room_version")
        self.fact("`m.room.create?format=event` sender / room_version", f"{create.get('sender')} / {version}")
        _, levels = call(
            "state-power-levels", "GET", f"/_matrix/client/v3/rooms/{rq}/state/m.room.power_levels/",
            token=admin, expect=200,
        )
        self.fact("power levels read back equal PROTOCOL §8", levels == team_power_levels([human]))
        call("state-team-record", "GET", f"/_matrix/client/v3/rooms/{rq}/state/{PREFIX}.team/",
             token=admin, expect=200)
        for label, uid in (("sender", sender), ("receiver", receiver), ("human", human)):
            call(f"invite-{label}", "POST", f"/_matrix/client/v3/rooms/{rq}/invite",
                 {"user_id": uid}, token=admin, expect=200)
        return room

    def sync(self, name: str, room: str, since: str | None, limit: int, timeout: str) -> tuple[dict[str, Any], int]:
        query = {"filter": _sync_filter(room, limit), "timeout": timeout}
        if since is not None:
            query["since"] = since
        started = time.monotonic()
        _, body = self.http.call(
            name, "GET", "/_matrix/client/v3/sync", token=self.tokens["receiver"], query=query,
            expect=200,
        )
        return dict(body), int((time.monotonic() - started) * 1000)

    def send(self, name: str, room: str, txn: str, content: dict[str, Any], who: str) -> str:
        _, sent = self.http.call(
            name, "PUT", f"/_matrix/client/v3/rooms/{q(room)}/send/m.room.message/{txn}",
            content, token=self.tokens[who], expect=200,
        )
        return str(sent["event_id"])

    def messages(self, room: str, sender: str, receiver: str) -> None:
        call = self.http.call
        rq = q(room)
        first, ms = self.sync("sync-initial-invite", room, None, 0, "0")
        self.fact("initial `/sync?timeout=0` ms", ms)
        invite = first.get("rooms", {}).get("invite", {}).get(room, {})
        types = sorted({e.get("type", "?") for e in invite.get("invite_state", {}).get("events", [])})
        self.fact("stripped invite state types", ", ".join(types) or "none")
        for label in ("sender", "receiver", "human"):
            call(f"join-{label}", "POST", f"/_matrix/client/v3/rooms/{rq}/join", {},
                 token=self.tokens[label], expect=200)
        joined, _ = self.sync("sync-after-join", room, first["next_batch"], 50, "0")
        since = joined["next_batch"]

        content = ping_content("review", PROBE_REF, [receiver])
        ping = self.send("send-ping", room, "probe-1", content, "sender")
        _, again = call("send-ping-txn-reuse", "PUT",
                        f"/_matrix/client/v3/rooms/{rq}/send/m.room.message/probe-1",
                        content, token=self.tokens["sender"])
        self.fact("txnId reuse returns the same event ID", again.get("event_id") == ping)
        self.send("send-human-text", room, "probe-h1",
                  {"msgtype": "m.text", "body": "please review",
                   "m.mentions": {"user_ids": [receiver]}}, "human")
        status_content = {"v": 1, "state": "listening", "until": int(time.time() * 1000) + 600_000}
        for name, key in (("status-own-key", sender), ("status-other-at-key", receiver),
                          ("status-non-at-key", "probe")):
            status, _ = call(name, "PUT", f"/_matrix/client/v3/rooms/{rq}/state/{PREFIX}.status/{q(key)}",
                             status_content, token=self.tokens["sender"])
            self.fact(f"`{PREFIX}.status` {name}", status)
        for name, who, method, path, body in (
            ("agent-sets-room-name", "sender", "PUT", f"/rooms/{rq}/state/m.room.name/", {"name": "x"}),
            ("human-sets-topic", "human", "PUT", f"/rooms/{rq}/state/m.room.topic/", {"topic": "x"}),
            ("human-invites", "human", "POST", f"/rooms/{rq}/invite", {"user_id": user_id("admin")}),
            ("human-redacts", "human", "PUT", f"/rooms/{rq}/redact/{q(ping)}/probe-r1", {}),
        ):
            status, _ = call(name, method, "/_matrix/client/v3" + path, body, token=self.tokens[who])
            self.fact(name, status)

        synced, ms = self.sync("sync-timeout-0", room, since, 50, "0")
        self.fact("`/sync?timeout=0` with events, ms", ms)
        empty, ms = self.sync("sync-timeout-0-empty", room, synced["next_batch"], 50, "0")
        self.fact("`/sync?timeout=0` with nothing pending, ms", ms)
        since = empty["next_batch"]

        held: dict[str, Any] = {}

        def held_sync() -> None:
            held["body"], _ = self.sync("sync-held", room, since, 50, "20000")
            held["returned"] = time.monotonic()

        thread = threading.Thread(target=held_sync)
        thread.start()
        time.sleep(2)
        sent_at = time.monotonic()
        fetch_ref = "path:example-org/myrepo@" + "1" * 40 + ":CLAUDE/Plan/x.md"
        self.send("send-ping-during-held-sync", room, "probe-2",
                  ping_content("fetch", fetch_ref, [receiver]), "sender")
        thread.join(timeout=40)
        if "returned" not in held:
            raise ProbeError("the held /sync did not return within 40 s of a send")
        self.fact("held `/sync` returned after the send, ms", int((held["returned"] - sent_at) * 1000))
        since = held["body"]["next_batch"]

        for n in range(12):
            ref = "commit:example-org/myrepo@" + f"{n:040x}"
            self.send(f"send-burst-{n}", room, f"probe-b{n}", ping_content("run-qa", ref, [receiver]), "sender")
        limited, _ = self.sync("sync-limited", room, since, 5, "0")
        timeline = limited.get("rooms", {}).get("join", {}).get(room, {}).get("timeline", {})
        self.fact("timeline limit 5 after 12 sends: limited / events",
                  f"{timeline.get('limited')} / {len(timeline.get('events', []))}")
        if timeline.get("limited") and timeline.get("prev_batch"):
            _, gap = call(
                "messages-gap-fill", "GET", f"/_matrix/client/v3/rooms/{rq}/messages",
                token=self.tokens["receiver"],
                query={"from": since, "to": timeline["prev_batch"], "dir": "f", "limit": "100",
                       "filter": json.dumps({"types": ["m.room.message"]})},
                expect=200,
            )
            self.fact("`/messages` gap fill: chunk / end present",
                      f"{len(gap.get('chunk', []))} / {'end' in gap}")
        call("get-event", "GET", f"/_matrix/client/v3/rooms/{rq}/event/{q(ping)}",
             token=self.tokens["receiver"], expect=200)


def _write_fixtures(http: Http, scrubber: Scrubber, out_dir: pathlib.Path) -> int:
    records = [dataclasses.asdict(exchange) for exchange in http.exchanges]
    scrubber.discover_ids([json.dumps(record) for record in records])
    out_dir.mkdir(parents=True, exist_ok=True)
    names = []
    for index, record in enumerate(records, start=1):
        text = json.dumps(scrub_json(record, scrubber), indent=2, sort_keys=True) + "\n"
        leaked = scrubber.leaks(text)
        if leaked:
            raise ProbeError(f"fixture {record['name']} still holds {leaked} after scrubbing")
        name = f"{index:03d}-{record['name']}.json"
        (out_dir / name).write_text(text, encoding="utf-8")
        names.append(name)
    (out_dir / "index.json").write_text(json.dumps(names, indent=2) + "\n", encoding="utf-8")
    return len(names)


def leg_h4(args: argparse.Namespace, report: Report) -> None:
    scratch = pathlib.Path(args.scratch)
    binary = binary_path(scratch)
    server = Tuwunel(binary, scratch / "h4", free_port())
    scrubber = Scrubber()
    scrubber.add_secret(server.secret, "shared-secret")
    http = Http(server.base_url)
    flows = _Flows(http, server, scrubber)
    server.start()
    try:
        flows.run()
    finally:
        flows.fact("exit code after SIGTERM", server.stop())
    log = server.log_text()
    scan = scan_log(log, scrubber.secret_values())
    flows.fact("log at `warn`: lines / secret occurrences / header-like lines",
               f"{scan.total_lines} / {scan.secret_hits} / {scan.header_lines}")
    for key in ("login_via_token", "database_backup_path", "database_backups_to_keep",
                "allow_guest_registration"):
        flows.fact(f"log lines naming `{key}`", len(lines_naming(log, key)))

    unknown = Tuwunel(binary, scratch / "h4-unknown", free_port(), extra={UNKNOWN_KEY: True})
    started = True
    try:
        unknown.start()
    except ProbeError as error:
        started = False
        say(f"  the unknown-key start failed: {error}")
    finally:
        unknown.stop()
    unknown_log = _ANSI_RE.sub("", scan_log(unknown.log_text(), [unknown.secret]).scrubbed)
    warning = lines_naming(unknown_log, UNKNOWN_KEY)
    flows.fact("starts with an unknown key", started)
    strict = Tuwunel(binary, scratch / "h4-strict", free_port(),
                     extra={UNKNOWN_KEY: True, "error_on_unknown_config_opts": True})
    strict_started = True
    try:
        strict.start()
    except ProbeError:
        strict_started = False
    finally:
        flows.fact("starts with an unknown key and `error_on_unknown_config_opts = true`",
                   f"{strict_started} (exit {strict.stop()})")
    strict_log = _ANSI_RE.sub("", scan_log(strict.log_text(), [strict.secret]).scrubbed)
    warning += lines_naming(strict_log, UNKNOWN_KEY)
    count = _write_fixtures(http, scrubber, pathlib.Path(args.fixtures))
    report.write("## H4 admin and client API (throwaway Tuwunel, loopback)")
    report.table(["Fact", "Value"], flows.facts)
    report.write("Unknown-key warning lines (verbatim, scrubbed):\n\n```\n"
                 + ("\n".join(warning) or "(none)") + "\n```")
    report.write("Distinct WARN/ERROR messages of the main run (scrubbed):\n\n```\n"
                 + ("\n".join(log_findings(scan.scrubbed)) or "(none)") + "\n```")
    report.table(["Call", "Status", "ms"], [[e.name, e.status, e.elapsed_ms] for e in http.exchanges])
    report.write(f"{count} scrubbed fixtures in `{args.fixtures}`; U08 copies them into "
                 "`tests/helpers/pingbus/fixtures/tuwunel/`.")


def _latest_meta(backups: pathlib.Path) -> pathlib.Path | None:
    meta = backups / "meta"
    if not meta.is_dir():
        return None
    numbered = sorted((p for p in meta.iterdir() if p.name.isdigit()), key=lambda p: int(p.name))
    return numbered[-1] if numbered else None


def leg_h5(args: argparse.Namespace, report: Report) -> None:
    scratch = pathlib.Path(args.scratch)
    binary = binary_path(scratch)
    data = scratch / "h5"
    server = Tuwunel(binary, data, free_port(), extra=SIGNAL_BACKUP)
    http = Http(server.base_url)
    facts: list[list[object]] = []
    server.start()
    try:
        status, body = shared_secret_register(
            http, server, Scrubber(), "register-admin", "admin", True, "admin"
        )
        if status != 200:
            raise ProbeError(f"admin registration failed: {status}")
        admin = ("token:admin", str(body["access_token"]))
        _, created = http.call("create-room", "POST", "/_matrix/client/v3/createRoom",
                               {"preset": "private_chat", "room_version": "12"}, token=admin,
                               expect=200)
        room = str(created["room_id"])
        send = f"/_matrix/client/v3/rooms/{q(room)}/send/m.room.message"
        _, sent = http.call("send-before", "PUT", f"{send}/h5-1",
                            {"msgtype": "m.notice", "body": "before backup"}, token=admin, expect=200)
        event_before = str(sent["event_id"])
        before = _latest_meta(data / "backups")
        log_mark = len(server.log_text())
        signalled = time.monotonic()
        server.signal(signal.SIGUSR2)
        meta = None
        while time.monotonic() - signalled < 120:
            meta = _latest_meta(data / "backups")
            if meta is not None and meta != before:
                break
            time.sleep(0.5)
        if meta is None or meta == before:
            since = _ANSI_RE.sub("", server.log_text()[log_mark:])[-1500:]
            raise ProbeError(f"no new backup meta file within 120 s of SIGUSR2; log since: {since}")
        facts.append(["backup meta visible after SIGUSR2, ms",
                      int((time.monotonic() - signalled) * 1000)])
        time.sleep(1)
        _, sent = http.call("send-after", "PUT", f"{send}/h5-2",
                            {"msgtype": "m.notice", "body": "after backup"}, token=admin, expect=200)
        event_after = str(sent["event_id"])
        findings = log_findings(server.log_text()[log_mark:])
        facts.append(["WARN/ERROR after SIGUSR2", " / ".join(findings) or "(none)"])
    finally:
        server.stop()
    backups = data / "backups"
    layout = Counter(p.relative_to(backups).parts[0] for p in backups.rglob("*") if p.is_file())
    copy = scratch / "h5-copy"
    shutil.copytree(data, copy)
    restored = Tuwunel(binary, copy, free_port(), extra=SIGNAL_BACKUP)
    http2 = Http(restored.base_url)
    log_mark = len(restored.log_text())
    restored.start(["--restore-backup"])
    try:
        status, _ = http2.call("whoami-admin-restored", "GET", "/_matrix/client/v3/account/whoami",
                               token=admin)
        facts.append(["restored copy: the admin token, whoami", status])
        event_path = f"/_matrix/client/v3/rooms/{q(room)}/event"
        status, got = http2.call("get-event-before", "GET", f"{event_path}/{q(event_before)}",
                                 token=admin)
        same = isinstance(got, dict) and got.get("content", {}).get("body") == "before backup"
        facts.append(["restored copy: the event sent before the backup, status / body matches",
                      f"{status} / {same}"])
        status, _ = http2.call("get-event-after", "GET", f"{event_path}/{q(event_after)}",
                               token=admin)
        facts.append(["restored copy: the event sent after the backup (gone if restored)", status])
    finally:
        facts.append(["restored copy: exit code after SIGTERM", restored.stop()])
    findings = log_findings(restored.log_text()[log_mark:])
    facts.append(["restored copy: WARN/ERROR", " / ".join(findings) or "(none)"])
    report.write("## H5 backup by SIGUSR2 and restore onto a copy")
    report.table(["Fact", "Value"], facts)
    report.write(
        "Backup trigger: `admin_signal_execute = [\"server backup-database\"]` in the config, then "
        "SIGUSR2. Restore: the stopped data directory copied whole, then started with "
        "`--restore-backup` (latest backup).\n\nBackup directory, files per top-level entry: "
        + ", ".join(f"`{name}` {count}" for name, count in sorted(layout.items()))
    )


def leg_h3_unit(args: argparse.Namespace, report: Report) -> None:
    scratch = pathlib.Path(args.scratch)
    binary = binary_path(scratch)
    unit_dir = scratch / "h3-unit"
    unit_dir.mkdir(parents=True, exist_ok=True)
    port = free_port()
    addresses = ["127.0.0.1"] + ([args.bus_address] if args.bus_address else [])
    toml = unit_dir / "tuwunel.toml"
    toml.write_text(
        render_tuwunel_toml(PROBE_SERVER_NAME, addresses, port, f"/run/{UNIT_RUNTIME_DIR}",
                            secret_file=f"{UNIT_INPUT_DIR}/registration_shared_secret"),
        encoding="ascii",
    )
    secret = unit_dir / "registration_shared_secret"
    secret.write_text(secrets.token_hex(64), encoding="ascii")
    stub = unit_dir / "resolv.conf"
    stub.write_text("nameserver 127.0.0.1\n", encoding="ascii")
    for path in (toml, secret, stub):
        path.chmod(0o644)  # throwaway inputs the unit's dynamic user must read
    paths = (str(binary), str(toml), str(secret), str(stub))
    unit = f"agent-bus-probe-{secrets.token_hex(4)}"
    facts: list[list[object]] = []

    def props(notify: bool, skip_type: bool = False) -> list[str]:
        out: list[str] = []
        for prop in unit_properties(port, addresses, *paths, notify=notify):
            if not (skip_type and prop.startswith("Type=")):
                out += ["-p", prop]
        return out

    def start(notify: bool) -> subprocess.CompletedProcess[str]:
        return run(["sudo", "-n", "systemd-run", f"--unit={unit}", "--collect", "--quiet",
                    *props(notify), "--", f"{UNIT_INPUT_DIR}/tuwunel"], timeout=150)

    def stop() -> None:
        result = run(["sudo", "-n", "systemctl", "stop", unit], timeout=360)
        if result.returncode not in (0, 5):  # 5: not loaded, already gone
            raise ProbeError(f"could not stop {unit}: {result.stderr.strip()}")

    try:
        started = start(True)
        facts.append(["starts as `Type=notify` (sends READY=1)",
                      f"{started.returncode == 0} {started.stderr.strip()[:300]}"])
        if started.returncode != 0:
            stop()
            simple = start(False)
            if simple.returncode != 0:
                raise ProbeError(f"the unit did not start as Type=simple either: {simple.stderr.strip()}")
        deadline = time.monotonic() + 90
        ready = False
        while not ready and time.monotonic() < deadline:
            ready = _server_answers(f"http://127.0.0.1:{port}/_tuwunel/server_version")
            if not ready:
                time.sleep(1)
        facts.append(["answers on 127.0.0.1 under the sandbox", ready])
        show = run_ok(["systemctl", "show", unit, "-p",
                       "ActiveState,SubState,Type,NRestarts,IPAddressDeny,IPAddressAllow"])
        facts.append(["`systemctl show`", show.strip().replace("\n", "; ")])
        listening = run_ok(["ss", "-ltnH", f"sport = :{port}"])
        facts.append(["`ss -ltnH` on the port", listening.strip().replace("\n", "; ") or "(nothing)"])
        journal = run(["sudo", "-n", "journalctl", "-u", unit, "--no-pager", "-o", "cat"], timeout=60)
        facts.append(["unit journal (last lines)", " / ".join(journal.stdout.splitlines()[-6:])])
    finally:
        stop()
    resolver = run(
        ["sudo", "-n", "systemd-run", "--wait", "--pipe", "--collect", "--quiet",
         *props(False, skip_type=True), "-p", "Type=exec", "--", "/bin/sh", "-c",
         "cat /etc/resolv.conf; getent ahosts example.org; echo getent-exit=$?"],
        timeout=120,
    )
    if resolver.returncode != 0:
        raise ProbeError(f"the resolver probe unit failed: {resolver.stderr.strip()}")
    facts.append(["inside the sandbox: /etc/resolv.conf, then a lookup",
                  resolver.stdout.strip().replace("\n", " / ")])
    where = "with" if args.bus_address else "without"
    report.write(f"## H3 Tuwunel under the section 3.5 sandbox (transient unit, {where} a bus address)")
    report.table(["Fact", "Value"], facts)


class Listener:
    """TCP listeners on several addresses, one port, recording who connected with which tag."""

    def __init__(self, addresses: list[str]) -> None:
        self.port = free_port()
        self.records: list[tuple[str, str, str]] = []
        self._sockets: list[socket.socket] = []
        self._stop = threading.Event()
        for address in addresses:
            family = socket.AF_INET6 if ":" in address else socket.AF_INET
            sock = socket.socket(family, socket.SOCK_STREAM)
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            sock.bind((address, self.port))
            sock.listen(16)
            sock.settimeout(0.5)
            self._sockets.append(sock)
            threading.Thread(target=self._serve, args=(address, sock), daemon=True).start()

    def _serve(self, address: str, sock: socket.socket) -> None:
        while not self._stop.is_set():
            try:
                conn, peer = sock.accept()
            except TimeoutError:
                continue
            except OSError:
                return  # the socket was closed by close()
            with conn:
                conn.settimeout(3)
                try:
                    data = conn.recv(64)
                except OSError as error:
                    data = f"recv-{type(error).__name__}".lower().encode()
            self.records.append((address, peer[0], parse_tag(data)))

    def close(self) -> None:
        self._stop.set()
        for sock in self._sockets:
            sock.close()


def _host_addresses(bus_address: str) -> dict[str, str]:
    route = json.loads(run_ok(["ip", "-j", "route", "get", "192.0.2.1"]))
    targets = {"primary": primary_address(route)}
    if bus_address:
        links = json.loads(run_ok(["ip", "-j", "addr"]))
        local = {info["local"] for link in links for info in link.get("addr_info", [])}
        if bus_address not in local:
            raise ProbeError(f"--bus-address {bus_address} is not assigned on this host")
        targets["bus"] = bus_address
    return targets


def _client_script(port: int, tag_prefix: str, targets: dict[str, str]) -> str:
    lines = []
    for name, host in targets.items():
        tag = f"{tag_prefix}-{name}"
        lines.append(
            f"if timeout 5 bash -c 'printf \"%s\\n\" {tag} > /dev/tcp/{host}/{port}'; "
            f"then echo '{name} connected'; else echo '{name} not connected'; fi"
        )
    lines.append("getent hosts host.containers.internal || echo 'host.containers.internal unresolved'")
    return "; ".join(lines)


def leg_h1(args: argparse.Namespace, report: Report) -> None:
    if run(["podman", "image", "exists", CCY_IMAGE]).returncode != 0:
        raise ProbeError(f"{CCY_IMAGE} is not present: launch ccy once to build it (this triage pulls nothing)")
    hosts = _host_addresses(args.bus_address)
    listener = Listener(["127.0.0.1", *hosts.values()])
    targets = dict(hosts)
    targets["containers-internal"] = "host.containers.internal"
    network = f"agent-bus-probe-{secrets.token_hex(3)}"
    rows: list[list[object]] = []
    try:
        run_ok(["podman", "network", "create", network])
        for label, net_args in (("default", []), ("named", ["--network", network]),
                                ("pasta", ["--network", "pasta"])):
            result = run(["podman", "run", "--rm", *net_args, "--entrypoint", "bash", CCY_IMAGE, "-c",
                          _client_script(listener.port, f"h1-{label}", targets)], timeout=180)
            rows.append([label, result.returncode, (result.stdout + result.stderr).strip()])
        time.sleep(1)
    finally:
        listener.close()
        removed = run(["podman", "network", "rm", network])
    report.write("## H1 host addresses from ccy-image containers (rootless podman)")
    report.write("Targets: " + ", ".join(f"{k} = `{v}`" for k, v in targets.items())
                 + f"; the listener bound 127.0.0.1 and the host addresses on port {listener.port}.")
    report.table(["Network", "Exit", "Client saw"], rows)
    report.table(["Listener address", "Source seen", "Tag"], [list(r) for r in listener.records])
    if removed.returncode != 0:
        raise ProbeError(f"podman network {network} was left behind: {removed.stderr.strip()}")


def _bridges() -> list[list[object]]:
    rows: list[list[object]] = []
    for bridge in sorted(pathlib.Path("/sys/class/net").glob("*/bridge")):
        name = bridge.parent.name
        ports = sorted(p.name for p in (bridge.parent / "brif").iterdir())
        links = json.loads(run_ok(["ip", "-j", "addr", "show", "dev", name]))
        cidrs = [f"{i['local']}/{i['prefixlen']}" for link in links for i in link.get("addr_info", [])]
        zone = run(["sudo", "-n", "firewall-cmd", f"--get-zone-of-interface={name}"])
        rows.append([name, ", ".join(cidrs) or "-", ", ".join(ports) or "-",
                     zone.stdout.strip() or zone.stderr.strip() or f"exit {zone.returncode}"])
    return rows


def leg_h2(args: argparse.Namespace, report: Report) -> None:
    hosts = _host_addresses(args.bus_address)
    listener = Listener(list(hosts.values()))
    rows: list[list[object]] = []
    owner_needed: list[str] = []
    try:
        if not shutil.which("docker"):
            rows.append(["docker", "-", "-", "not installed"])
        elif run(["docker", "image", "inspect", args.docker_image]).returncode != 0:
            owner_needed.append(f"docker: image {args.docker_image} is not present locally (this triage pulls nothing)")
        else:
            for name, host in hosts.items():
                result = run(["docker", "run", "--rm", args.docker_image, "sh", "-c",
                              f"echo h2-docker-{name} | nc -w 3 {host} {listener.port} && echo connected"],
                             timeout=120)
                rows.append(["docker", name, result.returncode, (result.stdout + result.stderr).strip()])
        if not shutil.which("lxc-ls"):
            rows.append(["lxc", "-", "-", "not installed"])
        else:
            running = run_ok(["sudo", "-n", "lxc-ls", "--running", "-1"]).split()
            if not running:
                owner_needed.append("lxc: no running container; start one and re-run")
            for name, host in (hosts.items() if running else ()):
                result = run(["sudo", "-n", "lxc-attach", "-n", running[0], "--", "bash", "-c",
                              f"printf '%s\\n' h2-lxc-{name} > /dev/tcp/{host}/{listener.port} && echo connected"],
                             timeout=60)
                rows.append(["lxc", name, result.returncode, (result.stdout + result.stderr).strip()])
        if not shutil.which("virsh"):
            rows.append(["libvirt", "-", "-", "not installed"])
        else:
            nets = run(["sudo", "-n", "virsh", "-c", "qemu:///system", "net-list", "--all"])
            rows.append(["libvirt", "networks", nets.returncode, nets.stdout.strip().replace("\n", "; ")])
            owner_needed.append("libvirt: reaching the host from a guest needs a guest shell (owner section)")
        time.sleep(1)
    finally:
        listener.close()
    report.write("## H2 host addresses from docker, LXC and libvirt guests")
    report.table(["Engine", "Target", "Exit", "Result"], rows)
    report.table(["Listener address", "Source seen", "Tag"], [list(r) for r in listener.records])
    report.table(["Bridge", "Addresses", "Ports", "firewalld zone"], _bridges())
    if owner_needed:
        report.write("Not established here:\n\n" + "\n".join(f"- {item}" for item in owner_needed))
        raise ProbeError("; ".join(owner_needed))


def leg_h6(args: argparse.Namespace, report: Report) -> None:
    for tool in ("flatpak", "pasta"):
        if not shutil.which(tool):
            raise ProbeError(f"{tool} is not installed")
    if run(["flatpak", "info", "im.riot.Riot"]).returncode != 0:
        raise ProbeError("Element Desktop (flatpak im.riot.Riot) is not installed")
    if not (os.environ.get("WAYLAND_DISPLAY") or os.environ.get("DISPLAY")):
        raise ProbeError("no graphical session here (WAYLAND_DISPLAY and DISPLAY are unset)")
    profile = f"agent-bus-probe-{secrets.token_hex(3)}"
    profile_dir = pathlib.Path.home() / ".var/app/im.riot.Riot/config" / f"Element-{profile}"
    if profile_dir.exists():
        raise ProbeError(f"{profile_dir} already exists")
    pcap = pathlib.Path(args.scratch) / "h6.pcap"
    port = free_port()
    proc = subprocess.Popen(
        ["pasta", "--pcap", str(pcap), "-T", str(port), "--",
         "flatpak", "run", "im.riot.Riot", "--profile", profile],
        stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, stdin=subprocess.DEVNULL,
        start_new_session=True,
    )
    try:
        time.sleep(args.element_seconds)
        exited_early = proc.poll() is not None
    finally:
        if proc.poll() is None:
            os.killpg(proc.pid, signal.SIGTERM)
        try:
            proc.wait(timeout=30)
        except subprocess.TimeoutExpired:
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait(timeout=30)
    stderr = proc.stderr.read().decode("utf-8", "replace") if proc.stderr else ""
    profile_resolved = profile_dir.is_dir()
    if profile_resolved:
        shutil.rmtree(profile_dir)
    if not pcap.exists():
        raise ProbeError(f"pasta wrote no capture; exit {proc.returncode}: {stderr[-800:]}")
    summary = summarise_packets(parse_pcap(pcap.read_bytes()))
    report.write(f"## H6 Element Desktop under pasta ({args.element_seconds} s, nothing behind -T {port})")
    report.table(["Fact", "Value"], [
        ["exited before the window ended", exited_early],
        ["profile directory created as `Element-<profile>`", profile_resolved],
        ["pasta / Element stderr (tail)", stderr.strip()[-400:] or "(none)"],
    ])
    report.table(["Proto", "Destination", "Port", "Packets"],
                 [[p, d, dport, n] for (p, d, dport), n in summary.most_common()])


def leg_owner(args: argparse.Namespace, report: Report) -> None:
    report.write(
        "## Owner steps (not automatable here)\n\n"
        "- **H6 login over plain HTTP:** once a team is installed (U16), log in from the team's "
        "Element profile at `http://<bus_ip>:<port>` and confirm the room opens; this run proved "
        "only the capture and the profile path.\n"
        "- **H7 phone:** on WireGuard, Element and Element X at `http://<wg_ip>:<port>`: which log "
        "in and sync, whether ping notices show, whether a mention pill sets `m.mentions` (view "
        "the event source), and whether the app trusts a user-installed CA.\n"
        "- **H2 libvirt guest:** from a guest on the libvirt network, connect to the host's "
        "address and note the source address the host sees.\n"
        "- **H3 on Fedora Server:** run this triage inside a Fedora Server test VM for the "
        "resolver leg there."
    )


LEGS = {
    "env": leg_env,
    "h3-asset": leg_h3_asset,
    "h4": leg_h4,
    "h5": leg_h5,
    "h3-unit": leg_h3_unit,
    "h1": leg_h1,
    "h2": leg_h2,
    "h6": leg_h6,
    "owner": leg_owner,
}


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="triage_probe.py")
    parser.add_argument("leg", choices=sorted(LEGS))
    parser.add_argument("--report", required=True)
    parser.add_argument("--scratch", default="")
    parser.add_argument("--fixtures", default="")
    parser.add_argument("--bus-address", default="")
    parser.add_argument("--docker-image", default="docker.io/library/busybox:latest")
    parser.add_argument("--element-seconds", type=int, default=60)
    parser.add_argument("--tuwunel-version", default=TUWUNEL_VERSION)
    args = parser.parse_args(argv)
    for name in ("scratch", "fixtures"):
        if getattr(args, name):
            setattr(args, name, str(pathlib.Path(getattr(args, name)).resolve()))
    if args.bus_address:
        args.bus_address = str(ipaddress.ip_address(args.bus_address))
    if args.leg not in ("env", "owner") and not args.scratch:
        parser.error(f"{args.leg} needs --scratch")
    if args.leg == "h4" and not args.fixtures:
        parser.error("h4 needs --fixtures")
    report = Report(pathlib.Path(args.report))
    try:
        LEGS[args.leg](args, report)
    except (ProbeError, ValueError, OSError, KeyError, subprocess.SubprocessError) as error:
        say(f"[FAIL] {args.leg}: {type(error).__name__}: {error}")
        report.write(f"**{args.leg} could not establish its facts:** {type(error).__name__}: {error}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
