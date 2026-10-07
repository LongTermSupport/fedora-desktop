"""Tests for triage_probe.py, the probing half of this plan's triage.bash (unit U00).

Only the pure parts are tested here: the Tuwunel config render (DESIGN.md section 3.6), the
team room's power levels (PROTOCOL.md section 8), the shared-secret MAC, the scrubber that
keeps tokens and IDs out of the recorded fixtures, the log scan, the pcap reader, the release
asset picker, the transient unit's properties, the backup-meta reader, the fixture writer's
token-field check, the stop-on-failure guard, and the H1/H2 legs' gap reporting (with the
host calls replaced). The network flows run only on the host, against a real Tuwunel.

Run from anywhere:
    python3 CLAUDE/Plan/00161-agent-team-bus-matrix/test_triage_probe.py
"""

from __future__ import annotations

import argparse
import importlib.util
import ipaddress
import json
import pathlib
import re
import struct
import subprocess
import sys
import tempfile
import tomllib
import unittest
from unittest import mock

_MODULE_PATH = pathlib.Path(__file__).resolve().parent / "triage_probe.py"
_SPEC = importlib.util.spec_from_file_location("triage_probe", _MODULE_PATH)
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_MODULE_PATH}")
tp = importlib.util.module_from_spec(_SPEC)
sys.modules["triage_probe"] = tp
_SPEC.loader.exec_module(tp)


ROOM_RE = re.compile(r"![A-Za-z0-9_-]{43}")
EVENT_RE = re.compile(r"\$[A-Za-z0-9_-]{43}")
# PROTOCOL.md section 3, the agent handle grammar.
HANDLE_RE = re.compile(
    r"(?P<repo>[a-z0-9][a-z0-9_-]{0,47})\.(?P<n>[1-9][0-9]{0,5})(?P<sep>[+=])"
    r"(?P<host>[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)\.(?P<type>podman|lxc|docker|vm|host)"
)


class PrefixTest(unittest.TestCase):
    def test_prefix_is_the_protocol_one(self) -> None:
        self.assertEqual(tp.PREFIX, "agent_bus")

    def test_server_name_is_reserved_internal(self) -> None:
        self.assertTrue(tp.PROBE_SERVER_NAME.endswith(".agent-bus.internal"))


class TuwunelTomlTest(unittest.TestCase):
    def render(self, **kwargs: object) -> dict:
        args: dict = {
            "server_name": "probe.agent-bus.internal",
            "addresses": ["127.0.0.1"],
            "port": 18008,
            "data_dir": "/srv/probe",
        }
        args.update(kwargs)
        return tomllib.loads(tp.render_tuwunel_toml(**args))

    def test_key_set_is_exactly_design_section_3_6(self) -> None:
        self.assertEqual(
            set(self.render()),
            {
                "server_name",
                "address",
                "port",
                "database_path",
                "database_backup_path",
                "database_backups_to_keep",
                "allow_registration",
                "allow_guest_registration",
                "registration_shared_secret_file",
                "grant_admin_to_first_user",
                "login_via_token",
                "login_via_existing_session",
                "allow_federation",
                "trusted_servers",
                "federate_admin_room",
                "admin_escape_commands",
                "allow_encryption",
                "auto_accept_invites",
                "new_user_displayname_suffix",
                "client_sync_timeout_min",
                "default_room_version",
                "sentry",
                "log",
                "admin_signal_execute",
                "error_on_unknown_config_opts",
                "rocksdb_allow_fallocate",
            },
        )

    def test_values(self) -> None:
        parsed = self.render(addresses=["127.0.0.1", "192.0.2.7"])
        self.assertEqual(parsed["server_name"], "probe.agent-bus.internal")
        self.assertEqual(parsed["address"], ["127.0.0.1", "192.0.2.7"])
        self.assertEqual(parsed["port"], 18008)
        self.assertEqual(parsed["database_path"], "/srv/probe/db")
        self.assertEqual(parsed["database_backup_path"], "/srv/probe/backups")
        self.assertEqual(parsed["database_backups_to_keep"], 7)
        self.assertEqual(
            parsed["registration_shared_secret_file"],
            "/srv/probe/secrets/registration_shared_secret",
        )
        for key in (
            "allow_registration",
            "allow_guest_registration",
            "grant_admin_to_first_user",
            "login_via_token",
            "login_via_existing_session",
            "allow_federation",
            "federate_admin_room",
            "rocksdb_allow_fallocate",
            "admin_escape_commands",
            "allow_encryption",
            "auto_accept_invites",
            "sentry",
        ):
            self.assertIs(parsed[key], False, key)
        self.assertEqual(parsed["trusted_servers"], [])
        self.assertEqual(parsed["new_user_displayname_suffix"], "")
        self.assertEqual(parsed["client_sync_timeout_min"], 0)
        self.assertEqual(parsed["default_room_version"], "12")
        self.assertEqual(parsed["log"], "warn")
        self.assertEqual(parsed["admin_signal_execute"], ["server backup-database"])
        self.assertIs(parsed["error_on_unknown_config_opts"], True)

    def test_extra_key_appended(self) -> None:
        parsed = self.render(extra={"agent_bus_probe_unknown_key": True})
        self.assertIs(parsed["agent_bus_probe_unknown_key"], True)

    def test_extra_overrides_a_section_3_6_key_in_place(self) -> None:
        text = tp.render_tuwunel_toml(
            "probe.agent-bus.internal", ["127.0.0.1"], 18008, "/srv/probe",
            extra={"error_on_unknown_config_opts": False},
        )
        self.assertEqual(text.count("error_on_unknown_config_opts"), 1)
        self.assertIs(tomllib.loads(text)["error_on_unknown_config_opts"], False)

    def test_loopback_must_come_first(self) -> None:
        with self.assertRaises(ValueError):
            self.render(addresses=["192.0.2.7"])

    def test_wildcard_refused(self) -> None:
        with self.assertRaises(ValueError):
            self.render(addresses=["127.0.0.1", "0.0.0.0"])

    def test_server_name_is_validated(self) -> None:
        with self.assertRaises(ValueError):
            self.render(server_name='evil"\nallow_registration = true')

    def test_data_dir_is_validated(self) -> None:
        with self.assertRaises(ValueError):
            self.render(data_dir='/x"\nallow_registration = true')
        with self.assertRaises(ValueError):
            self.render(data_dir="relative/dir")


class RegistrationMacTest(unittest.TestCase):
    # Fixed vectors: HMAC-SHA1(secret, nonce NUL user NUL password NUL admin|notadmin), the
    # construction Synapse documents for shared-secret registration.
    def test_admin(self) -> None:
        mac = tp.registration_mac("shared-secret-example", "abcd", "admin", "pw-example", True)
        self.assertEqual(mac, "9bc11cc0fb33e0cd8e165abc10cf355e5a87ee11")

    def test_notadmin(self) -> None:
        mac = tp.registration_mac("shared-secret-example", "abcd", "steward", "pw-example", False)
        self.assertEqual(mac, "93da3293f0396d224090e809d4d0c483bd0bbda5")


class PowerLevelsTest(unittest.TestCase):
    def test_team_room_levels_are_exactly_protocol_section_8(self) -> None:
        human = "@probehuman:probe.agent-bus.internal"
        self.assertEqual(
            tp.team_power_levels([human]),
            {
                "users": {human: 50},
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
                    "agent_bus.status": 0,
                },
            },
        )


class HandleTest(unittest.TestCase):
    def test_probe_handle_fits_protocol_grammar(self) -> None:
        for sep in ("+", "="):
            handle = tp.probe_handle(1, sep)
            match = HANDLE_RE.fullmatch(handle)
            self.assertIsNotNone(match, handle)
            assert match is not None
            self.assertEqual(match["sep"], sep)
            self.assertEqual(match["type"], "podman")

    def test_bad_separator_refused(self) -> None:
        with self.assertRaises(ValueError):
            tp.probe_handle(1, "-")


class PingTest(unittest.TestCase):
    def test_ping_content_shape_and_body(self) -> None:
        to = ["@b.1+probe.podman:sn.internal", "@a.1+probe.podman:sn.internal"]
        ref = "commit:example-org/myrepo@" + "0" * 40
        content = tp.ping_content("review", ref, to)
        self.assertEqual(set(content), {"msgtype", "body", "m.mentions", "agent_bus.ping"})
        self.assertEqual(content["msgtype"], "m.notice")
        self.assertEqual(content["m.mentions"], {"user_ids": sorted(to)})
        self.assertEqual(
            content["agent_bus.ping"], {"v": 1, "verb": "review", "to": sorted(to), "ref": ref}
        )
        self.assertEqual(
            content["body"],
            f"[agent-bus] review {ref} -> @a.1+probe.podman:sn.internal "
            "@b.1+probe.podman:sn.internal",
        )


class FakeIdTest(unittest.TestCase):
    def test_grammar_and_determinism(self) -> None:
        room = tp.fake_id("!", "room-1")
        event = tp.fake_id("$", "event-1")
        self.assertRegex(room, r"^![A-Za-z0-9_-]{43}$")
        self.assertRegex(event, r"^\$[A-Za-z0-9_-]{43}$")
        self.assertEqual(room, tp.fake_id("!", "room-1"))
        self.assertNotEqual(room, tp.fake_id("!", "room-2"))


class ScrubberTest(unittest.TestCase):
    REAL_ROOM = "!" + "r" * 43
    REAL_EVENT = "$" + "E" * 43
    TOKEN = "tokenvalue-AAAA1111"

    def make(self) -> tp.Scrubber:
        scrubber = tp.Scrubber()
        scrubber.add_secret(self.TOKEN, "token:agent1")
        return scrubber

    def test_secret_replaced_raw_and_url_encoded(self) -> None:
        scrubber = self.make()
        scrubber.add_secret("a/b+c", "password:alice")
        text = f'{{"t": "{self.TOKEN}", "p": "a/b+c"}} /x/a%2Fb%2Bc'
        out = scrubber.scrub(text)
        self.assertNotIn(self.TOKEN, out)
        self.assertNotIn("a/b+c", out)
        self.assertNotIn("a%2Fb%2Bc", out)
        self.assertIn("<token:agent1>", out)
        self.assertIn("<password:alice>", out)

    def test_ids_discovered_and_replaced_consistently(self) -> None:
        scrubber = self.make()
        texts = [
            f'{{"room_id": "{self.REAL_ROOM}"}}',
            f'{{"event_id": "{self.REAL_EVENT}", "room": "{self.REAL_ROOM}"}}',
            "/rooms/%21" + "r" * 43 + "/event/%24" + "E" * 43,
        ]
        scrubber.discover_ids(texts)
        outs = [scrubber.scrub(t) for t in texts]
        for out in outs:
            self.assertNotIn("r" * 43, out)
            self.assertNotIn("E" * 43, out)
        fake_room = tp.fake_id("!", "room-1")
        fake_event = tp.fake_id("$", "event-1")
        self.assertIn(fake_room, outs[0])
        self.assertIn(fake_room, outs[1])
        self.assertIn(fake_event, outs[1])
        self.assertIn("/rooms/%21" + fake_room[1:], outs[2])
        self.assertRegex(outs[1], ROOM_RE)
        self.assertRegex(outs[1], EVENT_RE)

    def test_longer_tokens_are_not_partly_matched_as_ids(self) -> None:
        scrubber = tp.Scrubber()
        longer = "!" + "x" * 50
        scrubber.discover_ids([longer])
        self.assertEqual(scrubber.scrub(longer), longer)

    def test_leaks(self) -> None:
        scrubber = self.make()
        scrubber.discover_ids([self.REAL_ROOM])
        self.assertEqual(scrubber.leaks("clean"), [])
        self.assertEqual(scrubber.leaks(f"x {self.TOKEN} y"), ["token:agent1"])
        self.assertEqual(scrubber.leaks(self.REAL_ROOM), ["room-1"])

    def test_empty_secret_refused(self) -> None:
        with self.assertRaises(ValueError):
            tp.Scrubber().add_secret("", "nothing")

    def test_scrub_json_round_trip(self) -> None:
        scrubber = self.make()
        record = {"response": {"access_token": self.TOKEN, "room_id": self.REAL_ROOM}}
        scrubber.discover_ids([json.dumps(record)])
        out = tp.scrub_json(record, scrubber)
        self.assertEqual(out["response"]["access_token"], "<token:agent1>")
        self.assertEqual(out["response"]["room_id"], tp.fake_id("!", "room-1"))
        self.assertEqual(scrubber.leaks(json.dumps(out)), [])


class ScanLogTest(unittest.TestCase):
    def test_counts_and_scrubs(self) -> None:
        log = "\n".join(
            [
                "INFO started",
                "WARN request Authorization: Bearer sekrit-token-1",
                "WARN another sekrit-token-1 here",
                "nothing",
            ]
        )
        result = tp.scan_log(log, ["sekrit-token-1", "unused-secret"])
        self.assertEqual(result.secret_hits, 2)
        self.assertEqual(result.header_lines, 1)
        self.assertEqual(result.total_lines, 4)
        self.assertNotIn("sekrit-token-1", result.scrubbed)
        self.assertIn("<secret>", result.scrubbed)

    def test_lines_naming_a_key(self) -> None:
        log = "a\nWARN unknown config key agent_bus_probe_unknown_key\nb agent_bus_probe_unknown_keyx\n"
        self.assertEqual(
            tp.lines_naming(log, "agent_bus_probe_unknown_key"),
            ["WARN unknown config key agent_bus_probe_unknown_key"],
        )


def _pcap(frames: list[bytes]) -> bytes:
    out = struct.pack("<IHHiIII", 0xA1B2C3D4, 2, 4, 0, 0, 65535, 1)
    for frame in frames:
        out += struct.pack("<IIII", 1, 0, len(frame), len(frame)) + frame
    return out


def _ipv4(proto: int, src: str, dst: str, payload: bytes) -> bytes:
    header = struct.pack(
        "!BBHHHBBH4s4s",
        0x45,
        0,
        20 + len(payload),
        0,
        0,
        64,
        proto,
        0,
        ipaddress.ip_address(src).packed,
        ipaddress.ip_address(dst).packed,
    )
    return b"\x00" * 12 + b"\x08\x00" + header + payload


class PcapTest(unittest.TestCase):
    def test_parse_and_summarise(self) -> None:
        udp = struct.pack("!HHHH", 40000, 53, 8, 0)
        tcp = struct.pack("!HHIIBBHHH", 50000, 8008, 0, 0, 0x50, 0x02, 0, 0, 0)
        data = _pcap(
            [
                _ipv4(17, "192.0.2.10", "192.0.2.53", udp),
                _ipv4(6, "127.0.0.1", "127.0.0.1", tcp),
                _ipv4(6, "127.0.0.1", "127.0.0.1", tcp),
            ]
        )
        packets = tp.parse_pcap(data)
        self.assertEqual(len(packets), 3)
        self.assertEqual(packets[0].proto, "udp")
        self.assertEqual(packets[0].dst, "192.0.2.53")
        self.assertEqual(packets[0].dport, 53)
        summary = tp.summarise_packets(packets)
        self.assertEqual(summary[("tcp", "127.0.0.1", 8008)], 2)
        self.assertEqual(summary[("udp", "192.0.2.53", 53)], 1)

    def test_non_ip_frame_counted_as_other(self) -> None:
        frame = b"\x00" * 12 + b"\x08\x06" + b"\x00" * 28
        packets = tp.parse_pcap(_pcap([frame]))
        self.assertEqual(packets[0].proto, "ethertype-0x0806")

    def test_bad_magic_refused(self) -> None:
        with self.assertRaises(ValueError):
            tp.parse_pcap(b"\x00" * 24)


class PickAssetTest(unittest.TestCase):
    # A subset of the real v1.9.3 release listing: the "all" feature set is the one with
    # every feature, "default"/"logging"/"debuginfo" are variants, and only the bare
    # "-tuwunel.zst" is the static binary.
    ASSETS = [
        {"name": f"v1.9.3-release-{variant}-{arch}-linux-gnu-tuwunel{suffix}"}
        for variant in ("all", "default", "logging", "debuginfo-all")
        for arch in ("x86_64-v1", "x86_64-v2", "x86_64-v3", "aarch64-v8")
        for suffix in (".zst", "-oci.tar.zst", ".nix.tar.zst", ".deb", ".rpm", "-docker.tar.gz")
    ]

    def test_x86_64_takes_the_v1_build_of_the_all_set(self) -> None:
        self.assertEqual(
            tp.pick_asset(self.ASSETS, "x86_64", "v1.9.3")["name"],
            "v1.9.3-release-all-x86_64-v1-linux-gnu-tuwunel.zst",
        )

    def test_aarch64(self) -> None:
        self.assertEqual(
            tp.pick_asset(self.ASSETS, "aarch64", "v1.9.3")["name"],
            "v1.9.3-release-all-aarch64-v8-linux-gnu-tuwunel.zst",
        )

    def test_none_refused(self) -> None:
        with self.assertRaises(ValueError):
            tp.pick_asset(self.ASSETS, "riscv64", "v1.9.3")
        with self.assertRaises(ValueError):
            tp.pick_asset(self.ASSETS, "x86_64", "v1.9.4")


class LogFindingsTest(unittest.TestCase):
    def test_warn_and_error_lines_deduplicated_without_ansi(self) -> None:
        log = "\n".join(
            [
                "\x1b[2m2026-10-06T14:41:11Z\x1b[0m \x1b[33m WARN\x1b[0m mod: disk is CoW",
                "2026-10-06T14:41:12Z  INFO mod: started",
                "\x1b[2m2026-10-06T14:41:13Z\x1b[0m \x1b[33m WARN\x1b[0m mod: disk is CoW",
                "2026-10-06T14:41:14Z ERROR other: failed",
                "   at frame 3",
            ]
        )
        self.assertEqual(
            tp.log_findings(log), ["WARN mod: disk is CoW", "ERROR other: failed"]
        )


class PrimaryAddressTest(unittest.TestCase):
    def test_prefsrc_taken(self) -> None:
        route = [{"dst": "192.0.2.1", "gateway": "198.51.100.1", "dev": "eth0", "prefsrc": "198.51.100.20"}]
        self.assertEqual(tp.primary_address(route), "198.51.100.20")

    def test_missing_refused(self) -> None:
        with self.assertRaises(ValueError):
            tp.primary_address([{"dst": "192.0.2.1", "dev": "eth0"}])
        with self.assertRaises(ValueError):
            tp.primary_address([])


class UnitPropertiesTest(unittest.TestCase):
    def props(self, notify: bool = True) -> list[str]:
        return tp.unit_properties(
            port=18009,
            addresses=["127.0.0.1", "192.0.2.7"],
            binary="/srv/scratch/tuwunel",
            toml="/srv/scratch/tuwunel.toml",
            secret="/srv/scratch/registration_shared_secret",
            stub="/srv/scratch/resolv.conf",
            notify=notify,
        )

    def test_section_3_5_hardening_present(self) -> None:
        props = self.props()
        for expected in (
            "Type=notify",
            "DynamicUser=yes",
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
            "SocketBindAllow=tcp:18009",
            "IPAccounting=yes",
            "IPAddressDeny=any",
            "IPAddressAllow=127.0.0.1/32 ::1/128 192.0.2.7/32",
            "TemporaryFileSystem=/run/systemd/resolve:ro",
            "BindReadOnlyPaths=/srv/scratch/resolv.conf:/run/systemd/resolve/stub-resolv.conf",
            "BindReadOnlyPaths=/srv/scratch/resolv.conf:/run/systemd/resolve/resolv.conf",
            "InaccessiblePaths=-/run/dbus",
            "Environment=TUWUNEL_CONFIG=/mnt/tuwunel.toml",
            "TemporaryFileSystem=/mnt:ro",
            "BindReadOnlyPaths=/srv/scratch/tuwunel:/mnt/tuwunel",
            "BindReadOnlyPaths=/srv/scratch/tuwunel.toml:/mnt/tuwunel.toml",
            "BindReadOnlyPaths=/srv/scratch/registration_shared_secret:/mnt/registration_shared_secret",
            "RuntimeDirectory=agent-bus-probe",
        ):
            self.assertIn(expected, props)

    def test_simple_when_not_notify(self) -> None:
        self.assertIn("Type=simple", self.props(notify=False))
        self.assertNotIn("Type=notify", self.props(notify=False))

    def test_every_entry_is_key_equals_value(self) -> None:
        for prop in self.props():
            self.assertRegex(prop, r"^[A-Za-z]+=\S")

    def test_exec_argv_names_a_host_path_first(self) -> None:
        """systemd-run looks argv[0] up on the HOST, before the unit's mounts exist, so the
        in-namespace /mnt/tuwunel cannot be argv[0] (host run 20261006-162757 failed so).
        A host shell execs it inside the namespace, keeping the PID for Type=notify."""
        argv = tp.unit_exec_argv()
        self.assertEqual(argv, ["/bin/sh", "-c", "exec /mnt/tuwunel"])
        self.assertFalse(argv[0].startswith(tp.UNIT_INPUT_DIR))


class LatestMetaTest(unittest.TestCase):
    def test_none_without_a_meta_directory(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            self.assertIsNone(tp._latest_meta(pathlib.Path(tmp)))

    def test_none_when_only_non_numeric_names(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            meta = pathlib.Path(tmp) / "meta"
            meta.mkdir()
            (meta / "LOCK").write_text("")
            (meta / "1a").write_text("")
            self.assertIsNone(tp._latest_meta(pathlib.Path(tmp)))

    def test_highest_number_not_highest_string(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            meta = pathlib.Path(tmp) / "meta"
            meta.mkdir()
            for name in ("2", "10", "9", "LOCK"):
                (meta / name).write_text("")
            self.assertEqual(tp._latest_meta(pathlib.Path(tmp)), meta / "10")


class UnregisteredTokensTest(unittest.TestCase):
    def test_every_token_field_found_at_any_depth(self) -> None:
        record = {
            "response": {
                "access_token": "known-1",
                "nested": [{"refresh_token": "unknown-2"}, {"login_token": "unknown-3"}],
            },
            "body": {"login_token": "known-1"},
        }
        self.assertEqual(
            tp.unregistered_tokens(record, ["known-1"]),
            ["response.nested[0].refresh_token", "response.nested[1].login_token"],
        )

    def test_clean_record(self) -> None:
        self.assertEqual(tp.unregistered_tokens({"access_token": "a", "x": "b"}, ["a"]), [])


class WriteFixturesTest(unittest.TestCase):
    def exchange(self, response: object) -> tp.Exchange:
        return tp.Exchange(
            name="login-get-token", method="POST", path="/_matrix/client/v1/login/get_token",
            query={}, auth="token:human", body={}, status=200, response=response, elapsed_ms=3,
        )

    def write(self, response: object) -> int:
        http = tp.Http("http://127.0.0.1:1")
        http.exchanges.append(self.exchange(response))
        scrubber = tp.Scrubber()
        scrubber.add_secret("registered-token", "token:human")
        with tempfile.TemporaryDirectory() as tmp:
            return tp._write_fixtures(http, scrubber, pathlib.Path(tmp) / "out")

    def test_unregistered_login_token_refused(self) -> None:
        with self.assertRaisesRegex(tp.ProbeError, "login_token"):
            self.write({"login_token": "never-registered", "expires_in_ms": 120000})

    def test_registered_token_written(self) -> None:
        self.assertEqual(self.write({"access_token": "registered-token"}), 1)


class StoppingTest(unittest.TestCase):
    def test_exit_status_kept_on_success(self) -> None:
        with tp.stopping(lambda: 0) as stopped:
            pass
        self.assertEqual(stopped["exit"], 0)

    def test_first_error_survives_a_failing_stop(self) -> None:
        def failing_stop() -> int:
            raise tp.ProbeError("stop failed")

        with self.assertRaisesRegex(RuntimeError, "flow failed"), tp.stopping(failing_stop):
            raise RuntimeError("flow failed")

    def test_stop_runs_when_the_body_fails(self) -> None:
        calls: list[str] = []
        with self.assertRaises(RuntimeError), tp.stopping(lambda: calls.append("stop")):
            raise RuntimeError("flow failed")
        self.assertEqual(calls, ["stop"])


def _done(stdout: str = "", returncode: int = 0, stderr: str = "") -> subprocess.CompletedProcess[str]:
    return subprocess.CompletedProcess([], returncode, stdout, stderr)


class _FakeListener:
    port = 40000

    def __init__(self, addresses: list[str]) -> None:
        self.records: list[tuple[str, str, str]] = []

    def close(self) -> None:
        pass


class AddressLegsTest(unittest.TestCase):
    """H1 and H2 with every host call replaced: only their gap reporting is under test."""

    def leg(self, name: str, bus_address: str, which: dict[str, str],
            run_result: subprocess.CompletedProcess[str] | None = None) -> str:
        hosts = {"primary": "198.51.100.20"}
        if bus_address:
            hosts["bus"] = bus_address
        with tempfile.TemporaryDirectory() as tmp:
            report_path = pathlib.Path(tmp) / "report.md"
            args = argparse.Namespace(bus_address=bus_address, docker_image="busybox")
            with (
                mock.patch.object(tp, "_host_addresses", return_value=hosts),
                mock.patch.object(tp, "Listener", _FakeListener),
                mock.patch.object(tp, "_bridges", return_value=[]),
                mock.patch.object(tp, "run", return_value=run_result or _done("net-a active")),
                mock.patch.object(tp, "run_ok", return_value=""),
                mock.patch.object(tp.shutil, "which", side_effect=which.get),
                mock.patch.object(tp.time, "sleep"),
            ):
                try:
                    tp.LEGS[name](args, tp.Report(report_path))
                finally:
                    self.text = report_path.read_text()
        return self.text

    def test_h1_without_a_bus_address_says_so_and_fails(self) -> None:
        with self.assertRaisesRegex(tp.ProbeError, "not tested"):
            self.leg("h1", "", {})
        self.assertIn("dummy/bus address not tested", self.text)

    def test_h1_with_a_bus_address_passes(self) -> None:
        self.assertNotIn("not tested", self.leg("h1", "192.0.2.7", {}))

    def test_h2_without_a_bus_address_says_so_and_fails(self) -> None:
        with self.assertRaisesRegex(tp.ProbeError, "not tested"):
            self.leg("h2", "", {})
        self.assertIn("dummy/bus address not tested", self.text)

    def test_h2_on_a_libvirt_host_points_at_the_owner_step_and_passes(self) -> None:
        text = self.leg("h2", "192.0.2.7", {"virsh": "/usr/bin/virsh"})
        self.assertIn("owner step", text)
        self.assertNotIn("Not established here", text)

    def test_h2_with_the_docker_image_missing_names_the_deploy_leg_that_pulls_it(self) -> None:
        missing = _done(returncode=1, stderr="Error: No such image: busybox")
        with self.assertRaisesRegex(tp.ProbeError, "not present locally"):
            self.leg("h2", "192.0.2.7", {"docker": "/usr/bin/docker"}, missing)
        self.assertIn("deploy.bash pulls it", self.text)

    def test_h2_with_docker_failing_otherwise_reports_dockers_error(self) -> None:
        denied = _done(returncode=1, stderr="permission denied while trying to connect to the Docker daemon socket")
        with self.assertRaisesRegex(tp.ProbeError, "permission denied") as caught:
            self.leg("h2", "192.0.2.7", {"docker": "/usr/bin/docker"}, denied)
        self.assertNotIn("not present", str(caught.exception))


class ListenerTagTest(unittest.TestCase):
    def test_tag_grammar(self) -> None:
        self.assertEqual(tp.parse_tag(b"h1-default\n"), "h1-default")
        self.assertEqual(tp.parse_tag(b"h1-net-x"), "h1-net-x")
        self.assertEqual(tp.parse_tag(b"bad tag\n"), "<invalid>")
        self.assertEqual(tp.parse_tag(b""), "<none>")


class TableLinesTest(unittest.TestCase):
    def test_header_rule_and_escaped_cells(self) -> None:
        self.assertEqual(
            tp.table_lines(["a", "b"], [["x|y", "one\ntwo"], [1, None]]),
            ["| a | b |", "|---|---|", "| x\\|y | one two |", "| 1 | None |"],
        )

    def test_report_table_writes_the_same_lines(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "report.md"
            tp.Report(path).table(["a"], [["x"]])
            self.assertIn("\n".join(tp.table_lines(["a"], [["x"]])), path.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
