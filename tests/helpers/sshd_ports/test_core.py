"""Unit tests for helpers/sshd_ports/core.py — pure `sshd -T` port extraction.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.sshd_ports.test_core

sshd_ports is a namespace package (no __init__.py); we put the repo root on
sys.path so `from helpers.sshd_ports import core` resolves. The sys.path edit
before the import is why ruff E402 is ignored for tests/** in ruff.toml.

The case that motivates the helper: a port configured ONLY via `ListenAddress
host:port` never appears on a `port` line, so a naive `port `-only filter opens
the default 22 and leaves the real port shut — locking the operator out of a
remote machine on the very run that first starts firewalld.
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.sshd_ports import core


class TestParsePorts(unittest.TestCase):
    def test_stock_config_yields_22(self):
        self.assertEqual(core.parse_ports("port 22\n"), ["22"])

    def test_non_standard_port(self):
        self.assertEqual(core.parse_ports("port 2222\n"), ["2222"])

    def test_multiple_port_lines_all_returned(self):
        text = "port 22\nport 2222\nport 8022\n"
        self.assertEqual(core.parse_ports(text), ["22", "2222", "8022"])

    def test_ignores_unrelated_directives(self):
        text = "addressfamily any\nport 22\npermitrootlogin no\n"
        self.assertEqual(core.parse_ports(text), ["22"])

    def test_empty_input_yields_no_ports(self):
        self.assertEqual(core.parse_ports(""), [])

    # ── ListenAddress: the lockout case ────────────────────────────────────────
    def test_ipv4_listenaddress_port_is_collected(self):
        text = "port 22\nlistenaddress 0.0.0.0:2222\n"
        self.assertEqual(core.parse_ports(text), ["22", "2222"])

    def test_bracketed_ipv6_listenaddress_port_is_collected(self):
        text = "port 22\nlistenaddress [::]:2222\n"
        self.assertEqual(core.parse_ports(text), ["22", "2222"])

    def test_listenaddress_without_port_is_ignored(self):
        text = "port 22\nlistenaddress 0.0.0.0\n"
        self.assertEqual(core.parse_ports(text), ["22"])

    def test_bare_ipv6_listenaddress_is_not_mistaken_for_a_port(self):
        """`2001:db8::1` ends in `:1` but that is an address, not a port."""
        text = "port 22\nlistenaddress 2001:db8::1\n"
        self.assertEqual(core.parse_ports(text), ["22"])

    def test_bare_ipv6_wildcard_is_ignored(self):
        text = "port 22\nlistenaddress ::\n"
        self.assertEqual(core.parse_ports(text), ["22"])

    def test_hostname_listenaddress_with_port(self):
        text = "listenaddress localhost:2222\n"
        self.assertEqual(core.parse_ports(text), ["2222"])

    def test_duplicates_collapse_preserving_first_occurrence(self):
        text = "port 22\nlistenaddress 0.0.0.0:22\nlistenaddress [::]:22\n"
        self.assertEqual(core.parse_ports(text), ["22"])

    def test_port_only_in_listenaddress_is_still_found(self):
        """The true lockout shape: sshd reports the DEFAULT port plus a
        ListenAddress carrying the real one. Both must be permitted."""
        text = "port 22\nlistenaddress 0.0.0.0:2222\n"
        self.assertIn("2222", core.parse_ports(text))

    def test_real_sshd_t_sample(self):
        """Trimmed from an actual `sshd -T` run on a machine moved off port 22."""
        text = (
            "port 22022\n"
            "addressfamily any\n"
            "listenaddress 0.0.0.0:2222\n"
            "permitrootlogin no\n"
            "passwordauthentication no\n"
        )
        self.assertEqual(core.parse_ports(text), ["22022", "2222"])

    def test_case_insensitive_directive_names(self):
        """sshd -T lower-cases its output, but do not depend on that."""
        text = "Port 22\nListenAddress 0.0.0.0:2222\n"
        self.assertEqual(core.parse_ports(text), ["22", "2222"])

    def test_rejects_non_numeric_port(self):
        text = "port ssh\nport 22\n"
        self.assertEqual(core.parse_ports(text), ["22"])


if __name__ == "__main__":
    unittest.main()
