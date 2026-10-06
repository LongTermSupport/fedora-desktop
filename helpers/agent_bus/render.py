"""What `agent-bus render` writes for the installer: `tuwunel.toml` and the unit drop-in.

Plan 00161's DESIGN.md section 3.6 is the `tuwunel.toml` key table, every key and value;
`TOML_KEYS` is that table and test_render holds the two equal. Section 3.3 and 3.5 give
the drop-in `/etc/systemd/system/agent-bus-hs@<team>.service.d/network.conf`: the unit's
IP filter, the socket-bind limit, the restart policy and a dependency on the device of
every interface carrying a listen address. Which interface carries an address is a fact
of the live host, so the installer passes it in (`ADDR=IFACE`); this module checks that
every listen address has exactly one and renders nothing it was not given.

Pure functions: no file or network I/O.
"""

from __future__ import annotations

import ipaddress
import json
import pathlib
import re
from collections.abc import Mapping, Sequence

from helpers.agent_bus import teamfile

STATE_ROOT = pathlib.PurePosixPath("/var/lib/agent-bus")
BACKUPS_TO_KEEP = 7
ROOM_VERSION = "12"
LOOPBACK_ALLOW = ("127.0.0.1/32", "::1/128")
RESTART_SEC = "5s"

#: DESIGN.md section 3.6's key column; `toml_values` must produce exactly these.
TOML_KEYS = (
    "server_name", "address", "port", "database_path", "database_backup_path",
    "database_backups_to_keep", "allow_registration", "allow_guest_registration",
    "registration_shared_secret_file", "grant_admin_to_first_user", "login_via_token",
    "login_via_existing_session", "allow_federation", "trusted_servers", "federate_admin_room",
    "admin_escape_commands", "allow_encryption", "auto_accept_invites",
    "new_user_displayname_suffix", "client_sync_timeout_min", "default_room_version", "sentry",
    "log", "admin_signal_execute", "error_on_unknown_config_opts", "rocksdb_allow_fallocate",
)

#: Linux interface names: at most 15 bytes, no `/`, whitespace or `:` (an alias).
_IFACE_RE = re.compile(r"[A-Za-z0-9_.-]{1,15}")


class RenderError(ValueError):
    """The input cannot be rendered; the message names what is wrong."""


def team_dir(team: str) -> pathlib.PurePosixPath:
    return STATE_ROOT / team


def toml_values(tf: teamfile.TeamFile) -> dict[str, object]:
    """DESIGN.md section 3.6, in its order."""
    base = team_dir(tf.team)
    return {
        "server_name": tf.server_name,
        "address": list(tf.listen_addresses()),
        "port": tf.port,
        "database_path": str(base / "db"),
        "database_backup_path": str(base / "backups"),
        "database_backups_to_keep": BACKUPS_TO_KEEP,
        "allow_registration": False,
        "allow_guest_registration": False,
        "registration_shared_secret_file": str(base / "secrets" / "registration_shared_secret"),
        "grant_admin_to_first_user": False,
        "login_via_token": False,
        "login_via_existing_session": False,
        "allow_federation": False,
        "trusted_servers": [],
        "federate_admin_room": False,
        "admin_escape_commands": False,
        "allow_encryption": False,
        "auto_accept_invites": False,
        "new_user_displayname_suffix": "",
        "client_sync_timeout_min": 0,
        "default_room_version": ROOM_VERSION,
        "sentry": False,
        "log": "warn",
        "admin_signal_execute": ["server backup-database"],
        "error_on_unknown_config_opts": True,
        "rocksdb_allow_fallocate": False,
    }


def _toml_value(value: object) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        return str(value)
    if isinstance(value, str):
        if not value.isascii() or not value.isprintable():
            raise RenderError("a tuwunel.toml string must be printable ASCII")
        return json.dumps(value)
    if isinstance(value, list):
        return "[" + ", ".join(_toml_value(v) for v in value) + "]"
    raise RenderError(f"no TOML form for {type(value).__name__}")


def render_toml(tf: teamfile.TeamFile) -> str:
    values = toml_values(tf)
    if tuple(values) != TOML_KEYS:
        raise RenderError("tuwunel.toml keys differ from TOML_KEYS")
    # Top-level keys, no `[global]` table: the form probe H4 started Tuwunel 1.9.3 with.
    lines = [f"# Rendered by agent-bus render toml for team {tf.team}; do not edit."]
    lines += [f"{key} = {_toml_value(value)}" for key, value in values.items()]
    return "\n".join(lines) + "\n"


def device_unit(iface: str) -> str:
    """systemd's device unit for a network interface, `-` escaped as `\\x2d`."""
    if not _IFACE_RE.fullmatch(iface) or iface in (".", ".."):
        raise RenderError(f"{iface!r} is not a network interface name")
    escaped = iface.replace("-", "\\x2d")
    if escaped.startswith("."):
        escaped = "\\x2e" + escaped[1:]
    return f"sys-subsystem-net-devices-{escaped}.device"


def parse_interfaces(pairs: Sequence[str]) -> dict[str, str]:
    """`ADDR=IFACE` arguments as a mapping; each address once, canonical, with a valid name."""
    out: dict[str, str] = {}
    for pair in pairs:
        address, sep, iface = pair.partition("=")
        try:
            canonical = str(ipaddress.ip_address(address))
        except ValueError:
            raise RenderError(f"--interface {pair!r}: expected ADDR=IFACE with an IP literal") from None
        if not sep or canonical != address:
            raise RenderError(f"--interface {pair!r}: expected ADDR=IFACE with a canonical IP literal")
        if address in out:
            raise RenderError(f"--interface: {address} is given twice")
        device_unit(iface)
        out[address] = iface
    return out


def _host_prefix(address: str) -> str:
    return f"{address}/{ipaddress.ip_address(address).max_prefixlen}"


def render_dropin(tf: teamfile.TeamFile, interfaces: Mapping[str, str]) -> str:
    missing = [a for a in tf.listen if a not in interfaces]
    if missing:
        raise RenderError(f"no --interface given for listen address {missing[0]}")
    extra = sorted(set(interfaces) - set(tf.listen))
    if extra:
        raise RenderError(f"--interface names {extra[0]}, which is not a listen address")
    devices = list(dict.fromkeys(device_unit(interfaces[a]) for a in tf.listen))
    allow = list(dict.fromkeys([*LOOPBACK_ALLOW, *(_host_prefix(a) for a in tf.listen), *tf.allow_from]))
    unit = ["[Unit]", "StartLimitIntervalSec=0"]
    if devices:
        unit += [f"After={' '.join(devices)}", f"Wants={' '.join(devices)}"]
    service = [
        "[Service]",
        "Restart=on-failure",
        f"RestartSec={RESTART_SEC}",
        "IPAddressDeny=any",
        f"IPAddressAllow={' '.join(allow)}",
        "SocketBindDeny=any",
        f"SocketBindAllow=tcp:{tf.port}",
    ]
    header = f"# Rendered by agent-bus render dropin for team {tf.team}; do not edit."
    return "\n".join([header, *unit, "", *service]) + "\n"


def check_unchanged(tf: teamfile.TeamFile, previous: teamfile.TeamFile | None) -> None:
    """A team's name and `server_name` never change once installed (DESIGN.md section 3.4:
    Tuwunel cannot change its server name without wiping the database)."""
    if previous is None:
        return
    if tf.team != previous.team:
        raise RenderError(f"team file: team {tf.team!r} differs from the installed {previous.team!r}")
    if tf.server_name != previous.server_name:
        raise RenderError(
            f"team file: server_name {tf.server_name!r} differs from the installed "
            f"{previous.server_name!r}; it can never change for a team"
        )
