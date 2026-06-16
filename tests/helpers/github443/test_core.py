"""Unit tests for helpers/github443/core.py — pure SSH-config / known_hosts logic.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.github443.test_core

github443 is a namespace package (no __init__.py); we put the repo root on
sys.path so `from helpers.github443 import core` resolves. The sys.path edit
before the import is why ruff E402 is ignored for tests/** in ruff.toml.
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.github443 import core


class TestNormalizeAliases(unittest.TestCase):
    def test_base_aliases_present_by_default(self):
        self.assertEqual(
            core.normalize_aliases([]),
            ["github.com", "ssh.github.com", "github.com-*"],
        )

    def test_extra_aliases_appended_in_order(self):
        self.assertEqual(
            core.normalize_aliases(["deploy_*", "myorg_*"]),
            ["github.com", "ssh.github.com", "github.com-*", "deploy_*", "myorg_*"],
        )

    def test_duplicates_and_blanks_dropped_order_preserved(self):
        self.assertEqual(
            core.normalize_aliases(["github.com", "", "deploy_*", "deploy_*", "  "]),
            ["github.com", "ssh.github.com", "github.com-*", "deploy_*"],
        )

    def test_whitespace_trimmed(self):
        self.assertEqual(
            core.normalize_aliases(["  deploy_*  "]),
            ["github.com", "ssh.github.com", "github.com-*", "deploy_*"],
        )


class TestRenderOverride(unittest.TestCase):
    def test_host_line_lists_all_aliases(self):
        block = core.render_ssh_override(["github.com", "ssh.github.com", "deploy_*"])
        self.assertEqual(
            block.splitlines()[0],
            "Host github.com ssh.github.com deploy_*",
        )

    def test_routes_to_443_endpoint(self):
        block = core.render_ssh_override(["github.com"])
        self.assertIn("HostName ssh.github.com", block)
        self.assertIn("Port 443", block)
        self.assertIn("User git", block)
        self.assertIn("ConnectTimeout 10", block)

    def test_directives_are_indented(self):
        block = core.render_ssh_override(["github.com"])
        for line in block.splitlines()[1:]:
            self.assertTrue(line.startswith("    "), f"not indented: {line!r}")


class TestRenderKnownHosts(unittest.TestCase):
    def test_each_key_prefixed_with_443_endpoint(self):
        out = core.render_known_hosts(["ssh-ed25519 AAAA", "ssh-rsa BBBB"])
        self.assertEqual(
            out.splitlines(),
            ["[ssh.github.com]:443 ssh-ed25519 AAAA", "[ssh.github.com]:443 ssh-rsa BBBB"],
        )


class TestUpsertBlockBehaviour(unittest.TestCase):
    BEGIN = "# >>> test BEGIN >>>"
    END = "# <<< test END <<<"

    def _upsert(self, text, body, present, at_bof=False):
        return core.upsert_block(text, self.BEGIN, self.END, body, present, at_bof=at_bof)

    def test_insert_into_empty_text(self):
        new, changed = self._upsert("", "BODY", present=True, at_bof=True)
        self.assertTrue(changed)
        self.assertIn(self.BEGIN, new)
        self.assertIn("BODY", new)
        self.assertIn(self.END, new)
        self.assertTrue(new.endswith("\n"))

    def test_insert_is_idempotent(self):
        once, _ = self._upsert("", "BODY", present=True, at_bof=True)
        twice, changed = self._upsert(once, "BODY", present=True, at_bof=True)
        self.assertFalse(changed)
        self.assertEqual(once, twice)

    def test_bof_insert_goes_before_existing_content(self):
        existing = "Host github.com-work\n    HostName github.com\n"
        new, changed = self._upsert(existing, "BODY", present=True, at_bof=True)
        self.assertTrue(changed)
        self.assertLess(new.index(self.BEGIN), new.index("Host github.com-work"))
        # user content is preserved verbatim
        self.assertIn("Host github.com-work", new)
        self.assertIn("    HostName github.com", new)

    def test_present_with_changed_body_replaces_in_place(self):
        once, _ = self._upsert("", "OLD", present=True, at_bof=True)
        new, changed = self._upsert(once, "NEW", present=True, at_bof=True)
        self.assertTrue(changed)
        self.assertIn("NEW", new)
        self.assertNotIn("OLD", new)
        # exactly one managed block
        self.assertEqual(new.count(self.BEGIN), 1)

    def test_remove_existing_block(self):
        existing = "Host github.com-work\n    HostName github.com\n"
        added, _ = self._upsert(existing, "BODY", present=True, at_bof=True)
        removed, changed = self._upsert(added, "BODY", present=False)
        self.assertTrue(changed)
        self.assertNotIn(self.BEGIN, removed)
        self.assertNotIn("BODY", removed)
        # user content survives the removal
        self.assertIn("Host github.com-work", removed)

    def test_remove_when_absent_is_noop(self):
        text = "Host github.com-work\n    HostName github.com\n"
        new, changed = self._upsert(text, "BODY", present=False)
        self.assertFalse(changed)
        self.assertEqual(text, new)

    def test_remove_then_reinsert_round_trips_to_clean_content(self):
        original = "Host x\n    HostName y\n"
        added, _ = self._upsert(original, "BODY", present=True, at_bof=True)
        removed, _ = self._upsert(added, "BODY", present=False)
        self.assertEqual(removed, original)


class TestParseMetaSshKeys(unittest.TestCase):
    def test_extracts_ssh_keys_array(self):
        meta = '{"ssh_keys": ["ssh-ed25519 AAAA", "ssh-rsa BBBB"], "hooks": ["x"]}'
        self.assertEqual(core.parse_meta_ssh_keys(meta), ["ssh-ed25519 AAAA", "ssh-rsa BBBB"])

    def test_missing_key_returns_empty(self):
        self.assertEqual(core.parse_meta_ssh_keys('{"hooks": []}'), [])

    def test_entries_are_normalised_to_type_and_blob(self):
        # comments / trailing fields are stripped to the canonical 2-field form
        meta = '{"ssh_keys": ["ssh-ed25519 AAAA comment-here"]}'
        self.assertEqual(core.parse_meta_ssh_keys(meta), ["ssh-ed25519 AAAA"])


class TestParseKeyscan(unittest.TestCase):
    def test_strips_host_prefix_to_canonical_form(self):
        out = (
            "# ssh.github.com:443 SSH-2.0-babeld\n"
            "[ssh.github.com]:443 ssh-ed25519 AAAA\n"
            "[ssh.github.com]:443 ssh-rsa BBBB\n"
        )
        self.assertEqual(core.parse_keyscan(out), ["ssh-ed25519 AAAA", "ssh-rsa BBBB"])

    def test_ignores_comments_and_blanks(self):
        self.assertEqual(core.parse_keyscan("# c\n\n"), [])


class TestDecideAuto(unittest.TestCase):
    def test_22_open_means_off(self):
        self.assertEqual(core.decide_auto(port22_ok=True, port443_ok=True), "off")
        self.assertEqual(core.decide_auto(port22_ok=True, port443_ok=False), "off")

    def test_22_blocked_443_open_means_on(self):
        self.assertEqual(core.decide_auto(port22_ok=False, port443_ok=True), "on")

    def test_both_blocked_means_noop(self):
        self.assertEqual(core.decide_auto(port22_ok=False, port443_ok=False), "noop")


class TestEnvLine(unittest.TestCase):
    def test_present_exports_one(self):
        self.assertEqual(core.env_line(True), "export GITHUB_SSH_443=1")

    def test_absent_unsets(self):
        self.assertEqual(core.env_line(False), "unset GITHUB_SSH_443")


if __name__ == "__main__":
    unittest.main()
