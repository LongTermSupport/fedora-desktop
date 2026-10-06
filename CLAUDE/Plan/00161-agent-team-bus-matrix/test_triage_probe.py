"""Tests for triage_probe.py, the parsing half of this plan's triage.bash (unit U00).

Only the pure parts are tested here: the Tuwunel config render, the shared-secret MAC, the
scrubber that keeps tokens and IDs out of the recorded fixtures, the log scan, the subnet
picker and the pcap reader. The network flows run only on the host, against a real Tuwunel.

Run from anywhere:
    python3 CLAUDE/Plan/00161-agent-team-bus-matrix/test_triage_probe.py
"""

from __future__ import annotations

import importlib.util
import ipaddress
import pathlib
import re
import struct
import sys
import tomllib
import unittest

_MODULE_PATH = pathlib.Path(__file__).resolve().parent / "triage_probe.py"
_SPEC = importlib.util.spec_from_file_location("triage_probe", _MODULE_PATH)
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_MODULE_PATH}")
tp = importlib.util.module_from_spec(_SPEC)
sys.modules["triage_probe"] = tp
_SPEC.loader.exec_module(tp)


ROOM_RE = re.compile(r"![A-Za-z0-9_-]{43}")
EVENT_RE = re.compile(r"\$[A-Za-z0-9_-]{43}")
# A non-overlapping network in the benchmarking range, standing in for the podman bridge.
OTHER_NET = "198.18.0.0/15"


class NamespaceTest(unittest.TestCase):
    def test_namespace_is_the_protocol_one(self) -> None:
        self.assertEqual(tp.NAMESPACE, "io.github.longtermsupport.agentbus")


class TuwunelTomlTest(unittest.TestCase):
    def test_key_set_is_exactly_design_section_2a(self) -> None:
        parsed = tomllib.loads(tp.render_tuwunel_toml("server.test"))
        self.assertEqual(
            set(parsed),
            {
                "server_name",
                "address",
                "port",
                "database_path",
                "allow_registration",
                "registration_shared_secret_file",
                "grant_admin_to_first_user",
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
            },
        )

    def test_values(self) -> None:
        parsed = tomllib.loads(tp.render_tuwunel_toml("server.test"))
        self.assertEqual(parsed["server_name"], "server.test")
        self.assertEqual(parsed["address"], ["0.0.0.0"])
        self.assertEqual(parsed["port"], 8008)
        self.assertIs(parsed["allow_registration"], False)
        self.assertIs(parsed["allow_federation"], False)
        self.assertEqual(parsed["trusted_servers"], [])
        self.assertEqual(parsed["new_user_displayname_suffix"], "")
        self.assertEqual(parsed["client_sync_timeout_min"], 0)
        self.assertEqual(parsed["default_room_version"], "12")
        self.assertEqual(parsed["log"], "warn")
        self.assertEqual(
            parsed["registration_shared_secret_file"],
            "/run/secrets/registration_shared_secret",
        )

    def test_server_name_is_validated(self) -> None:
        with self.assertRaises(ValueError):
            tp.render_tuwunel_toml('evil"\nallow_registration = true')


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
    def test_bus_room_levels_match_design_section_7(self) -> None:
        levels = tp.bus_power_levels()
        self.assertEqual(levels["users_default"], 0)
        self.assertEqual(levels["events_default"], 100)
        self.assertEqual(levels["state_default"], 100)
        self.assertEqual(
            levels["events"],
            {f"{tp.NAMESPACE}.ping": 0, f"{tp.NAMESPACE}.status": 0},
        )
        for key in ("invite", "kick", "ban", "redact"):
            self.assertEqual(levels[key], 100)
        self.assertNotIn("users", levels)

    def test_control_room_levels_match_design_section_7(self) -> None:
        levels = tp.control_power_levels(["@alice:server.test"], "@warden:server.test")
        self.assertEqual(levels["users"], {"@alice:server.test": 50, "@warden:server.test": 50})
        self.assertEqual(levels["events_default"], 0)
        self.assertEqual(levels["state_default"], 100)
        for key in ("invite", "kick", "ban", "redact"):
            self.assertEqual(levels[key], 100)


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


class PickSubnetTest(unittest.TestCase):
    def test_first_free_candidate(self) -> None:
        self.assertEqual(tp.pick_subnet([OTHER_NET]), ipaddress.ip_network(tp.SUBNET_CANDIDATES[0]))

    def test_overlap_skipped(self) -> None:
        taken = [tp.SUBNET_CANDIDATES[0].replace("/24", "/25")]
        self.assertEqual(tp.pick_subnet(taken), ipaddress.ip_network(tp.SUBNET_CANDIDATES[1]))

    def test_none_free(self) -> None:
        self.assertIsNone(tp.pick_subnet(["0.0.0.0/0"]))

    def test_ipv6_taken_ignored(self) -> None:
        self.assertEqual(
            tp.pick_subnet(["fd00::/64"]), ipaddress.ip_network(tp.SUBNET_CANDIDATES[0])
        )

    def test_host_address_with_prefix_accepted(self) -> None:
        first = ipaddress.ip_network(tp.SUBNET_CANDIDATES[0])
        taken = [f"{first.network_address + 5}/24"]
        self.assertEqual(tp.pick_subnet(taken), ipaddress.ip_network(tp.SUBNET_CANDIDATES[1]))


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


if __name__ == "__main__":
    unittest.main()
