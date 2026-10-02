"""Tests for helpers.self_update.signers — the server's allowed-signers list (Plan 00137 Task 4.8).

A self-update server trusts a small LIST of the owner's keys, because the owner's commits
are signed by more than one: the desktop's `~/.ssh/id` on the host, and a GitHub account's
key in a ccy session (Plan 00139 D5). The list is `self_update_signing_public_keys`, plus
the older single `self_update_signing_public_key` when a server still declares it.
"""

from __future__ import annotations

import io
import json
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import signers

PRINCIPAL = "owner@example.com"
# Placeholders in the shape of a .pub line, short enough that nobody mistakes them for keys.
MACHINE = "ssh-ed25519 AAAAmachinePlaceholder owner@example.com"
ACCOUNT = "ssh-ed25519 AAAAaccountPlaceholder account"
RSA = "ssh-rsa AAAArsaPlaceholder="


class TestEffectiveKeys(unittest.TestCase):
    def test_the_list_alone(self) -> None:
        self.assertEqual(signers.effective_keys([MACHINE, ACCOUNT], None), [MACHINE, ACCOUNT])

    def test_the_single_key_alone_still_works(self) -> None:
        self.assertEqual(signers.effective_keys(None, MACHINE), [MACHINE])

    def test_the_single_key_is_added_after_the_list(self) -> None:
        self.assertEqual(signers.effective_keys([ACCOUNT], MACHINE), [ACCOUNT, MACHINE])

    def test_a_key_in_both_appears_once(self) -> None:
        self.assertEqual(signers.effective_keys([MACHINE, ACCOUNT], MACHINE), [MACHINE, ACCOUNT])

    def test_the_same_key_with_another_comment_is_the_same_key(self) -> None:
        relabelled = MACHINE.rsplit(" ", 1)[0] + " relabelled"
        self.assertEqual(signers.effective_keys([MACHINE, relabelled], None), [MACHINE])

    def test_surrounding_whitespace_is_dropped(self) -> None:
        self.assertEqual(signers.effective_keys([f"  {MACHINE}\n"], None), [MACHINE])

    def test_a_key_without_a_comment_is_accepted(self) -> None:
        self.assertEqual(signers.effective_keys([RSA], None), [RSA])

    def test_no_key_at_all_is_refused(self) -> None:
        with self.assertRaisesRegex(signers.InvalidSigners, "self_update_signing_public_keys"):
            signers.effective_keys(None, None)

    def test_an_empty_list_and_no_single_key_is_refused(self) -> None:
        with self.assertRaisesRegex(signers.InvalidSigners, "at least one"):
            signers.effective_keys([], None)

    def test_an_empty_single_key_counts_as_none(self) -> None:
        with self.assertRaisesRegex(signers.InvalidSigners, "at least one"):
            signers.effective_keys([], "")

    def test_a_string_where_the_list_belongs_is_refused(self) -> None:
        with self.assertRaisesRegex(signers.InvalidSigners, "must be a list"):
            signers.effective_keys(MACHINE, None)

    def test_a_malformed_list_entry_is_refused_by_position(self) -> None:
        with self.assertRaisesRegex(signers.InvalidSigners, r"self_update_signing_public_keys\[1\]"):
            signers.effective_keys([MACHINE, "not a key"], None)

    def test_a_bare_base64_line_with_no_key_type_is_refused(self) -> None:
        with self.assertRaises(signers.InvalidSigners):
            signers.effective_keys(["b3BlbnNzaC1rZXktdjEAAAAA"], None)

    def test_a_multi_line_entry_is_refused(self) -> None:
        with self.assertRaises(signers.InvalidSigners):
            signers.effective_keys([MACHINE + "\n" + ACCOUNT], None)

    def test_a_malformed_single_key_is_refused_by_name(self) -> None:
        with self.assertRaisesRegex(signers.InvalidSigners, "self_update_signing_public_key "):
            signers.effective_keys([MACHINE], "ssh-ed25519")

    def test_a_non_string_entry_is_refused(self) -> None:
        with self.assertRaises(signers.InvalidSigners):
            signers.effective_keys([42], None)


class TestRender(unittest.TestCase):
    def test_one_line_per_key_for_the_principal(self) -> None:
        self.assertEqual(
            signers.render(PRINCIPAL, [MACHINE, ACCOUNT]),
            f"{PRINCIPAL} {MACHINE}\n{PRINCIPAL} {ACCOUNT}\n",
        )

    def test_a_principal_with_whitespace_is_refused(self) -> None:
        with self.assertRaisesRegex(signers.InvalidSigners, "self_update_signing_principal"):
            signers.render("two words", [MACHINE])

    def test_an_empty_principal_is_refused(self) -> None:
        with self.assertRaises(signers.InvalidSigners):
            signers.render("", [MACHINE])


class TestMain(unittest.TestCase):
    def _main(self, payload: object) -> tuple[int, str, str]:
        stdin = io.StringIO(payload if isinstance(payload, str) else json.dumps(payload))
        out, err = io.StringIO(), io.StringIO()
        code = signers.main(stdin=stdin, stdout=out, stderr=err)
        return code, out.getvalue(), err.getvalue()

    def test_the_file_is_printed_and_nothing_else(self) -> None:
        code, out, err = self._main({"principal": PRINCIPAL, "keys": [ACCOUNT], "key": MACHINE})
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"{PRINCIPAL} {ACCOUNT}\n{PRINCIPAL} {MACHINE}\n")
        self.assertEqual(err, "")

    def test_undeclared_inputs_arrive_as_null(self) -> None:
        code, out, _ = self._main({"principal": PRINCIPAL, "keys": None, "key": MACHINE})
        self.assertEqual(code, 0)
        self.assertEqual(out, f"{PRINCIPAL} {MACHINE}\n")

    def test_an_invalid_input_exits_2_with_the_reason_on_stderr_and_no_file(self) -> None:
        code, out, err = self._main({"principal": PRINCIPAL, "keys": [], "key": None})
        self.assertEqual(code, signers.EXIT_INVALID)
        self.assertEqual(out, "")
        self.assertIn("at least one", err)

    def test_input_that_is_not_json_exits_2(self) -> None:
        code, out, err = self._main("not json")
        self.assertEqual(code, signers.EXIT_INVALID)
        self.assertEqual(out, "")
        self.assertIn("JSON", err)

    def test_json_missing_a_field_exits_2(self) -> None:
        code, _, err = self._main({"principal": PRINCIPAL, "keys": [MACHINE]})
        self.assertEqual(code, signers.EXIT_INVALID)
        self.assertIn("key", err)


if __name__ == "__main__":
    unittest.main()
