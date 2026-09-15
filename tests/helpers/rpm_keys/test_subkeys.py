"""Tests for helpers.rpm_keys.subkeys — is an imported signing key still current?

Issue #45. Google's Linux signing key is ONE primary with a growing set of signing
subkeys, and the current Chrome package is signed by a subkey added long after the
key was first published. `rpm` and `dnf` both decide a key is "present" by its
PRIMARY id, so on a host that imported it years ago — an F41 install upgraded to
F44 is exactly that — the import is skipped for ever and the new subkey never
arrives. dnf5 then says both halves of a contradiction in one breath:

    Public key "…linux_signing_key.pub" is already present, not importing.
    OpenPGP check … has failed: Public key is not installed.

Both are true. The decision below is what tells them apart, so the play can replace
a stale key instead of re-importing one that is already there.

The play acts on this verdict with `rpm -e`, so the cases that matter most are the
ones where the answer is "do not remove anything": a key that is merely old but
sufficient, and a key at this id that turns out not to be ours at all.

`gpg` is shelled out to rather than reimplemented: parsing OpenPGP packets in the
standard library to answer one question would be a great deal of code with its own
bugs. The RUNNER is injected, so every case here is driven without a real keyring.
"""

from __future__ import annotations

import io
import os
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.rpm_keys import subkeys

PRIMARY = "7721F63BD38B4796"
SHORT = "d38b4796"
SIGNER = "FD533C07C264648F"
OLD_SUBKEY = "1397BC53640DB551"

#: What `rpm -qa gpg-pubkey --qf '%{name}-%{version}-%{release}\n'` prints. Unrelated
#: keys sit either side of the one being matched, so a case that passes has actually
#: selected rather than simply taken whatever came first.
LISTING = (
    "gpg-pubkey-a1b2c3d4-5e6f7081\n"
    f"gpg-pubkey-{SHORT}-5713b0fd\n"
    "gpg-pubkey-99887766-55443322\n"
)
GOOGLE_ENVELOPE = f"gpg-pubkey-{SHORT}-5713b0fd"
ARMOUR = "-----BEGIN PGP PUBLIC KEY BLOCK-----\nmQENBFcA\n-----END PGP PUBLIC KEY BLOCK-----\n"


def _colons(primary: str | None, subs: list[str]) -> str:
    """`gpg --show-keys --with-colons` output, reduced to the fields read."""
    if primary is None:
        return ""
    lines = [f"pub:-:4096:1:{primary}:1460440275:::-:::scSC::::::23::0:"]
    lines += [f"sub:-:4096:1:{sub}:1460440275::::::s::::::23:" for sub in subs]
    return "\n".join(lines) + "\n"


class FakeRunner:
    """A stand-in for `subprocess.run`, answering the queries the helper makes.

    The queries are recognised by what they ask for, not by argv position, so each
    case reads as "rpm listed these keys, gpg saw this" rather than as a lookup
    table. Anything the helper asks that is not one of them raises: a fake that
    quietly returns "" for an unrecognised command turns a wrong invocation into a
    passing test, which is the failure mode these tests exist to catch elsewhere.
    """

    def __init__(
        self,
        *,
        listing: str = "",
        armour: str = "",
        colons: str = "",
        fail: set[str] | None = None,
    ) -> None:
        self.listing = listing
        self.armour = armour
        self.colons = colons
        self.fail = fail or set()
        self.calls: list[list[str]] = []

    def __call__(self, argv: list[str], **kwargs: object) -> subprocess.CompletedProcess:
        self.calls.append(list(argv))
        if argv[0] in self.fail:
            raise subprocess.CalledProcessError(2, argv, output="", stderr="boom")
        if argv[0] == "gpg":
            return subprocess.CompletedProcess(argv, 0, self.colons, "")
        if "%{description}" in argv:
            return subprocess.CompletedProcess(argv, 0, self.armour, "")
        if "gpg-pubkey" in argv:
            return subprocess.CompletedProcess(argv, 0, self.listing, "")
        raise AssertionError(f"FakeRunner has no answer for: {argv}")


class StagedRunner(FakeRunner):
    """A FakeRunner whose `gpg` answers differ per call, in order.

    The published key and the installed key are both read by `gpg`, and the whole
    decision is about them DIFFERING — a fake returning one canned answer to both
    could only ever exercise the equal case.
    """

    def __init__(
        self,
        *,
        answers: list[str],
        listing: str = "",
        armour: str = "",
    ) -> None:
        super().__init__(listing=listing, armour=armour)
        self.answers = list(answers)

    def __call__(self, argv: list[str], **kwargs: object) -> subprocess.CompletedProcess:
        if argv[0] == "gpg":
            self.calls.append(list(argv))
            if not self.answers:
                raise AssertionError(f"gpg was called more times than staged: {argv}")
            return subprocess.CompletedProcess(argv, 0, self.answers.pop(0), "")
        return super().__call__(argv, **kwargs)


class TestKeyIds(unittest.TestCase):
    def test_reads_the_primary_and_every_subkey(self) -> None:
        text = _colons(PRIMARY, [OLD_SUBKEY, SIGNER])
        self.assertEqual(
            subkeys.key_ids(text), {"primary": PRIMARY, "subkeys": {OLD_SUBKEY, SIGNER}}
        )

    def test_a_key_with_no_subkeys(self) -> None:
        self.assertEqual(
            subkeys.key_ids(_colons(PRIMARY, [])), {"primary": PRIMARY, "subkeys": set()}
        )

    def test_empty_output_has_no_primary(self) -> None:
        """gpg printing nothing is not a key with no subkeys. Treating it as one would
        make an unreadable key look like a key that merely needs refreshing."""
        self.assertIsNone(subkeys.key_ids("")["primary"])

    def test_other_record_types_are_ignored(self) -> None:
        text = _colons(PRIMARY, [SIGNER]) + "uid:-::::::::Google Inc.:\nfpr:::::::::ABC:\n"
        self.assertEqual(subkeys.key_ids(text)["subkeys"], {SIGNER})

    def test_a_second_pub_record_does_not_displace_the_first(self) -> None:
        """A keyring export can hold several keys. The first `pub` is the one being
        asked about, and a later one must not silently become the answer."""
        text = _colons(PRIMARY, [SIGNER]) + _colons("DEADBEEFDEADBEEF", ["AAAABBBBCCCCDDDD"])
        self.assertEqual(subkeys.key_ids(text)["primary"], PRIMARY)


class TestShortId(unittest.TestCase):
    """rpm names a `gpg-pubkey` package after the SHORT id — the last 8 hex digits of
    gpg's long id, lowercased. Deriving it is what lets the removal be aimed at one
    key by identity rather than at anything whose description mentions a vendor."""

    def test_takes_the_last_eight_hex_digits_lowercased(self) -> None:
        self.assertEqual(subkeys.short_id(PRIMARY), SHORT)

    def test_an_already_short_id_is_returned_as_is(self) -> None:
        self.assertEqual(subkeys.short_id(SHORT.upper()), SHORT)

    def test_an_unusable_primary_raises(self) -> None:
        """An id too short to name a package is not something to guess at: it would
        build a removal target that matches packages nobody meant to name."""
        with self.assertRaises(ValueError):
            subkeys.short_id("ABC")


class TestNeedsRefresh(unittest.TestCase):
    """The decision itself, over the states an installed key can be in."""

    def test_a_subkey_present_in_published_but_not_installed_needs_refresh(self) -> None:
        """THE case from issue #45: same primary, the signing subkey missing."""
        verdict = subkeys.needs_refresh(
            installed={"primary": PRIMARY, "subkeys": {OLD_SUBKEY}},
            published={"primary": PRIMARY, "subkeys": {OLD_SUBKEY, SIGNER}},
        )
        self.assertTrue(verdict.refresh)
        self.assertEqual(verdict.action, "refresh")
        self.assertIn(SIGNER, verdict.reason)

    def test_an_identical_key_does_not_need_refresh(self) -> None:
        both = {"primary": PRIMARY, "subkeys": {OLD_SUBKEY, SIGNER}}
        verdict = subkeys.needs_refresh(installed=dict(both), published=dict(both))
        self.assertFalse(verdict.refresh)
        self.assertFalse(verdict.import_missing)
        self.assertEqual(verdict.action, "none")

    def test_an_absent_installed_key_is_an_import_not_a_refresh(self) -> None:
        """Nothing installed is an import, not a refresh — and the play must not try to
        remove a key that is not there. Distinct answers, because the remedies differ."""
        verdict = subkeys.needs_refresh(
            installed={"primary": None, "subkeys": set()},
            published={"primary": PRIMARY, "subkeys": {SIGNER}},
        )
        self.assertFalse(verdict.refresh)
        self.assertTrue(verdict.import_missing)
        self.assertEqual(verdict.action, "import")

    def test_a_key_at_this_id_that_is_not_ours_is_imported_not_removed(self) -> None:
        """Short ids are 8 hex digits and are not unique. A key sitting at the same id
        with a different primary is somebody else's, so our key is simply absent —
        and `rpm -e` must not be pointed at a stranger's key to make room."""
        verdict = subkeys.needs_refresh(
            installed={"primary": "DEADBEEFD38B4796", "subkeys": {OLD_SUBKEY}},
            published={"primary": PRIMARY, "subkeys": {SIGNER}},
        )
        self.assertFalse(verdict.refresh)
        self.assertTrue(verdict.import_missing)
        self.assertIn("DEADBEEFD38B4796", verdict.reason)

    def test_an_unreadable_published_key_refuses_rather_than_deciding(self) -> None:
        """A published key that could not be read is not evidence the installed one is
        fine. Answering 'no refresh needed' there would be a confident claim from a
        probe that failed — and would leave the host unable to install anything."""
        with self.assertRaises(ValueError):
            subkeys.needs_refresh(
                installed={"primary": PRIMARY, "subkeys": {SIGNER}},
                published={"primary": None, "subkeys": set()},
            )

    def test_an_installed_extra_subkey_is_not_a_refresh(self) -> None:
        """A key holding MORE than the published file is old-but-sufficient, or a
        published file mid-rotation. Nothing needed for verification is missing."""
        verdict = subkeys.needs_refresh(
            installed={"primary": PRIMARY, "subkeys": {OLD_SUBKEY, SIGNER, "AAAABBBBCCCCDDDD"}},
            published={"primary": PRIMARY, "subkeys": {OLD_SUBKEY, SIGNER}},
        )
        self.assertFalse(verdict.refresh)


class TestInstalledEnvelopes(unittest.TestCase):
    def test_returns_only_the_package_named_for_this_key_id(self) -> None:
        runner = FakeRunner(listing=LISTING)
        self.assertEqual(subkeys.installed_envelopes(SHORT, run=runner), [GOOGLE_ENVELOPE])

    def test_the_id_is_matched_case_insensitively(self) -> None:
        """gpg prints the long id in upper case and rpm names the package in lower."""
        runner = FakeRunner(listing=LISTING)
        self.assertEqual(subkeys.installed_envelopes(SHORT.upper(), run=runner), [GOOGLE_ENVELOPE])

    def test_a_longer_id_starting_with_this_one_is_not_a_match(self) -> None:
        """`gpg-pubkey-d38b4796a-…` is a different key. Anchoring on the separator is
        what keeps the removal aimed at one package."""
        runner = FakeRunner(listing=f"gpg-pubkey-{SHORT}ab-5713b0fd\n")
        self.assertEqual(subkeys.installed_envelopes(SHORT, run=runner), [])

    def test_every_match_is_returned_not_just_the_first(self) -> None:
        """A stale duplicate is what an upgraded host accumulates, and leaving one
        behind would leave the same unusable key in the keyring after the refresh."""
        runner = FakeRunner(listing=LISTING + f"gpg-pubkey-{SHORT}-4615767f\n")
        self.assertEqual(len(subkeys.installed_envelopes(SHORT, run=runner)), 2)

    def test_no_matching_key_is_an_empty_list_not_an_error(self) -> None:
        """A host that never imported the key is a normal state, and the play handles
        it by importing. It must not look like a failed probe."""
        runner = FakeRunner(listing=LISTING)
        self.assertEqual(subkeys.installed_envelopes("aaaaaaaa", run=runner), [])

    def test_a_failing_rpm_raises_rather_than_reporting_no_key(self) -> None:
        """'rpm could not tell me' and 'there is no key' are different facts. Reporting
        the first as the second would re-import a key on every run, for ever."""
        runner = FakeRunner(listing=LISTING, fail={"rpm"})
        with self.assertRaises(subprocess.CalledProcessError):
            subkeys.installed_envelopes(SHORT, run=runner)


class TestKeyArmour(unittest.TestCase):
    def test_asks_rpm_for_the_named_envelope(self) -> None:
        runner = FakeRunner(armour=ARMOUR)
        self.assertIn("-----BEGIN", subkeys.key_armour(GOOGLE_ENVELOPE, run=runner))
        self.assertIn(GOOGLE_ENVELOPE, runner.calls[-1])


class TestReadKey(unittest.TestCase):
    def test_reads_a_key_file_by_path(self) -> None:
        runner = FakeRunner(colons=_colons(PRIMARY, [SIGNER]))
        self.assertEqual(subkeys.read_key("/etc/pki/rpm-gpg/KEY", run=runner)["primary"], PRIMARY)
        self.assertIn("/etc/pki/rpm-gpg/KEY", runner.calls[0])

    def test_reads_armour_from_stdin(self) -> None:
        """The installed key only exists as rpm's `%{description}`, never as a file."""
        runner = FakeRunner(colons=_colons(PRIMARY, [OLD_SUBKEY]))
        self.assertEqual(subkeys.read_armour(ARMOUR, run=runner)["subkeys"], {OLD_SUBKEY})

    def test_an_unreadable_key_file_raises(self) -> None:
        runner = FakeRunner(colons="", fail={"gpg"})
        with self.assertRaises(subprocess.CalledProcessError):
            subkeys.read_key("/etc/pki/rpm-gpg/KEY", run=runner)


class TestReport(unittest.TestCase):
    """The marker lines the play parses. The play keys `changed_when` and an `rpm -e`
    loop off these, so their shape is a contract, not formatting."""

    def _report(self, verdict: subkeys.Verdict, envelopes: list[str]) -> list[str]:
        out = io.StringIO()
        subkeys.report(verdict=verdict, envelopes=envelopes, stdout=out)
        return out.getvalue().splitlines()

    def test_a_refresh_names_every_envelope_to_remove(self) -> None:
        lines = self._report(
            subkeys.Verdict(refresh=True, import_missing=False, reason="stale"),
            [GOOGLE_ENVELOPE, f"gpg-pubkey-{SHORT}-4615767f"],
        )
        self.assertIn(f"{subkeys.ACTION_MARKER} refresh", lines)
        self.assertEqual(
            [line for line in lines if line.startswith(subkeys.ENVELOPE_MARKER)],
            [
                f"{subkeys.ENVELOPE_MARKER} {GOOGLE_ENVELOPE}",
                f"{subkeys.ENVELOPE_MARKER} gpg-pubkey-{SHORT}-4615767f",
            ],
        )

    def test_an_action_of_import_names_no_envelope_even_when_one_is_installed(self) -> None:
        """`import` covers the stranger-at-the-same-id case, where a key IS installed
        and must be left alone. Printing it would hand the play a removal target for
        somebody else's key."""
        lines = self._report(
            subkeys.Verdict(refresh=False, import_missing=True, reason="not ours"),
            [GOOGLE_ENVELOPE],
        )
        self.assertIn(f"{subkeys.ACTION_MARKER} import", lines)
        self.assertFalse([line for line in lines if line.startswith(subkeys.ENVELOPE_MARKER)])

    def test_no_action_names_no_envelope(self) -> None:
        lines = self._report(
            subkeys.Verdict(refresh=False, import_missing=False, reason="current"),
            [GOOGLE_ENVELOPE],
        )
        self.assertIn(f"{subkeys.ACTION_MARKER} none", lines)
        self.assertFalse([line for line in lines if line.startswith(subkeys.ENVELOPE_MARKER)])

    def test_the_reason_is_carried_on_its_own_line(self) -> None:
        lines = self._report(
            subkeys.Verdict(refresh=False, import_missing=False, reason="the key is fine"), []
        )
        self.assertIn(f"{subkeys.REASON_MARKER} the key is fine", lines)


class TestMain(unittest.TestCase):
    """End to end over the injected runner: the argument in, the marker lines out."""

    def _run(self, runner: FakeRunner) -> tuple[int, list[str]]:
        out = io.StringIO()
        code = subkeys.main(["--published", "/etc/pki/rpm-gpg/KEY"], run=runner, stdout=out)
        return code, out.getvalue().splitlines()

    def test_a_stale_installed_key_reports_refresh(self) -> None:
        """The published key carries the signer; the installed one does not. This is
        the host in issue #45, driven all the way through."""
        runner = StagedRunner(
            listing=LISTING,
            armour=ARMOUR,
            answers=[_colons(PRIMARY, [OLD_SUBKEY, SIGNER]), _colons(PRIMARY, [OLD_SUBKEY])],
        )
        code, lines = self._run(runner)
        self.assertEqual(code, 0)
        self.assertIn(f"{subkeys.ACTION_MARKER} refresh", lines)
        self.assertIn(f"{subkeys.ENVELOPE_MARKER} {GOOGLE_ENVELOPE}", lines)

    def test_a_current_installed_key_reports_none(self) -> None:
        runner = StagedRunner(
            listing=LISTING,
            armour=ARMOUR,
            answers=[_colons(PRIMARY, [SIGNER]), _colons(PRIMARY, [SIGNER])],
        )
        code, lines = self._run(runner)
        self.assertEqual(code, 0)
        self.assertIn(f"{subkeys.ACTION_MARKER} none", lines)

    def test_a_host_with_no_such_key_reports_import(self) -> None:
        """gpg is asked ONCE — there is no installed armour to read — so a second
        staged answer would mean the helper had invented a key to inspect."""
        runner = StagedRunner(
            listing="gpg-pubkey-a1b2c3d4-5e6f7081\n",
            armour=ARMOUR,
            answers=[_colons(PRIMARY, [SIGNER])],
        )
        code, lines = self._run(runner)
        self.assertEqual(code, 0)
        self.assertIn(f"{subkeys.ACTION_MARKER} import", lines)
        self.assertFalse([line for line in lines if line.startswith(subkeys.ENVELOPE_MARKER)])

    def test_the_listing_is_matched_against_the_published_key_id(self) -> None:
        """Nothing in the invocation names Google. The id comes out of the published
        key itself, so the removal cannot drift onto a key the play never read."""
        runner = StagedRunner(
            listing=LISTING,
            armour=ARMOUR,
            answers=[_colons(PRIMARY, [SIGNER]), _colons(PRIMARY, [SIGNER])],
        )
        self._run(runner)
        self.assertIn(["rpm", "-q", GOOGLE_ENVELOPE, "--qf", "%{description}"], runner.calls)

    def test_an_unreadable_published_key_fails_rather_than_printing_none(self) -> None:
        """The play must stop here. Printing `none` would leave the host with a key
        nothing verified, and a green run saying so."""
        runner = StagedRunner(listing=LISTING, armour=ARMOUR, answers=["", ""])
        with self.assertRaises(ValueError):
            self._run(runner)


if __name__ == "__main__":
    unittest.main()
