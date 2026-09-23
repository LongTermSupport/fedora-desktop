"""Tests for helpers.self_update.gate — the pure decisions of the signed-tip trust gate.

Plan 00137 D3: the cycle deploys only a commit signed by the owner's pinned key, and that
signature vouches for everything between the deployed commit and it. These pin the
decisions without any git: which signature states are trusted, skipped or refused, how
the newest trusted candidate is chosen, and how the Fedora pin is read.
"""

from __future__ import annotations

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import gate

SSH_SIG = (
    "tree 1111111111111111111111111111111111111111\n"
    "author a <a@example.com> 1 +0000\n"
    "committer a <a@example.com> 1 +0000\n"
    "gpgsig -----BEGIN SSH SIGNATURE-----\n"
    " U1NIU0lH\n"
    " -----END SSH SIGNATURE-----\n"
    "\n"
    "message\n"
)
PGP_SIG = SSH_SIG.replace("SSH SIGNATURE", "PGP SIGNATURE")
UNSIGNED = (
    "tree 1111111111111111111111111111111111111111\n"
    "author a <a@example.com> 1 +0000\n"
    "committer a <a@example.com> 1 +0000\n"
    "\n"
    "message mentioning gpgsig -----BEGIN SSH SIGNATURE----- in the body\n"
)


class TestSignatureKind(unittest.TestCase):
    def test_an_unsigned_commit_has_no_signature(self) -> None:
        self.assertEqual(gate.signature_kind(UNSIGNED), gate.KIND_NONE)

    def test_an_ssh_signature_is_recognised_from_the_header(self) -> None:
        self.assertEqual(gate.signature_kind(SSH_SIG), gate.KIND_SSH)

    def test_a_pgp_signature_is_other(self) -> None:
        """Never verified: doing so would run the gpg program the repo's config names."""
        self.assertEqual(gate.signature_kind(PGP_SIG), gate.KIND_OTHER)

    def test_a_signature_line_in_the_message_body_is_not_a_header(self) -> None:
        """Headers end at the first blank line; text after it is the message."""
        self.assertEqual(gate.signature_kind(UNSIGNED), gate.KIND_NONE)


class TestJudge(unittest.TestCase):
    def test_good_and_the_pinned_principal_is_trusted(self) -> None:
        self.assertEqual(gate.judge("G", "owner@example.com", "owner@example.com"), gate.TRUSTED)

    def test_good_but_another_principal_is_only_untrusted(self) -> None:
        self.assertEqual(gate.judge("G", "someone@example.com", "owner@example.com"), gate.UNTRUSTED)

    def test_a_good_signature_from_an_unpinned_key_is_untrusted_not_hostile(self) -> None:
        """`U`: an agent signing with its own key. Not the owner, and not an attack."""
        self.assertEqual(gate.judge("U", "", "owner@example.com"), gate.UNTRUSTED)

    def test_expired_keys_are_untrusted(self) -> None:
        for status in ("X", "Y"):
            with self.subTest(status=status):
                self.assertEqual(gate.judge(status, "owner@example.com", "owner@example.com"), gate.UNTRUSTED)

    def test_a_bad_signature_refuses(self) -> None:
        """`B`: the bytes do not match the signature. Someone altered a signed commit."""
        self.assertEqual(gate.judge("B", "", "owner@example.com"), gate.REFUSE)

    def test_a_revoked_key_refuses(self) -> None:
        self.assertEqual(gate.judge("R", "owner@example.com", "owner@example.com"), gate.REFUSE)

    def test_a_signature_that_cannot_be_checked_refuses(self) -> None:
        """`E`: the check itself did not run. Unknown is not untrusted."""
        self.assertEqual(gate.judge("E", "", "owner@example.com"), gate.REFUSE)

    def test_an_unknown_status_letter_refuses(self) -> None:
        self.assertEqual(gate.judge("Q", "", "owner@example.com"), gate.REFUSE)

    def test_an_empty_principal_never_matches(self) -> None:
        self.assertEqual(gate.judge("G", "", ""), gate.UNTRUSTED)


class TestChooseTarget(unittest.TestCase):
    """Candidates arrive newest first: the first-parent commits above the deployed one."""

    def test_the_newest_trusted_commit_is_chosen(self) -> None:
        choice = gate.choose_target([("c3", gate.UNTRUSTED), ("c2", gate.TRUSTED), ("c1", gate.TRUSTED)])
        self.assertEqual(choice, gate.Choice(target="c2", refused=None))

    def test_unsigned_commits_above_it_wait(self) -> None:
        choice = gate.choose_target([("c2", gate.UNTRUSTED), ("c1", gate.UNTRUSTED)])
        self.assertEqual(choice, gate.Choice(target=None, refused=None))

    def test_no_candidates_is_nothing_to_do(self) -> None:
        self.assertEqual(gate.choose_target([]), gate.Choice(target=None, refused=None))

    def test_a_refusal_above_the_trusted_commit_refuses_the_cycle(self) -> None:
        choice = gate.choose_target([("c3", gate.REFUSE), ("c2", gate.TRUSTED)])
        self.assertEqual(choice, gate.Choice(target=None, refused="c3"))

    def test_a_refusal_below_the_trusted_commit_is_vouched_for(self) -> None:
        """The owner signed a descendant of it; that signature covers the range."""
        choice = gate.choose_target([("c3", gate.TRUSTED), ("c2", gate.REFUSE)])
        self.assertEqual(choice, gate.Choice(target="c3", refused=None))

    def test_it_accepts_a_lazy_iterable_and_stops_at_the_first_trusted(self) -> None:
        seen: list[str] = []

        def candidates():
            for sha, verdict in (("c3", gate.UNTRUSTED), ("c2", gate.TRUSTED), ("c1", gate.REFUSE)):
                seen.append(sha)
                yield sha, verdict

        self.assertEqual(gate.choose_target(candidates()).target, "c2")
        self.assertEqual(seen, ["c3", "c2"])


class TestFedoraPin(unittest.TestCase):
    def test_the_pin_is_read_from_the_vars_file(self) -> None:
        self.assertEqual(gate.pinned_fedora_version("---\n# comment\nfedora_version: 44\n"), 44)

    def test_the_pin_file_without_a_trailing_newline(self) -> None:
        self.assertEqual(gate.pinned_fedora_version("---\nfedora_version: 45"), 45)

    def test_a_quoted_pin_is_accepted(self) -> None:
        self.assertEqual(gate.pinned_fedora_version('fedora_version: "44"\n'), 44)

    def test_a_missing_pin_raises(self) -> None:
        with self.assertRaises(ValueError):
            gate.pinned_fedora_version("---\nsomething_else: 1\n")

    def test_a_commented_out_pin_is_not_a_pin(self) -> None:
        with self.assertRaises(ValueError):
            gate.pinned_fedora_version("# fedora_version: 44\n")

    def test_the_running_version_is_read_from_os_release(self) -> None:
        text = 'NAME="Fedora Linux"\nVERSION_ID=44\nID=fedora\n'
        self.assertEqual(gate.running_fedora_version(text), 44)

    def test_a_quoted_version_id(self) -> None:
        self.assertEqual(gate.running_fedora_version('VERSION_ID="44"\n'), 44)

    def test_os_release_without_version_id_raises(self) -> None:
        with self.assertRaises(ValueError):
            gate.running_fedora_version('NAME="Fedora Linux"\n')


if __name__ == "__main__":
    unittest.main()
