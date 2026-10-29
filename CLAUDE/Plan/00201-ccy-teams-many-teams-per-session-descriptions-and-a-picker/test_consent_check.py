"""Tests for consent_check.py, the judgements behind acceptance-consent.bash (Plan 00201,
ACCEPTANCE-owner.md step 08: cc --teams's Element consent, No then Yes).

Nothing here starts tmux, cc, sudo or a team: every command takes text or a temporary file. The
product's texts are read from this checkout's own consent.go and token-management.bash, so a
change of wording there is caught here before a host run waits for words that no longer appear.

Run from anywhere:
    python3 CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/test_consent_check.py
"""

from __future__ import annotations

import importlib.util
import io
import os
import pathlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

_HERE = pathlib.Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location("consent_check", _HERE / "consent_check.py")
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_HERE / 'consent_check.py'}")
cc = importlib.util.module_from_spec(_SPEC)
sys.modules["consent_check"] = cc
_SPEC.loader.exec_module(cc)

H1 = "scratch201.1+desk.host"
H2 = "scratch201.2+desk.host"


def texts() -> dict[str, str]:
    """The product's texts as the module reads them from this checkout."""
    return cc.product_texts()


def consent_screen(handles: list[str]) -> str:
    """What the pane shows while the consent is asked: the words, then gum's question."""
    t = texts()
    joined = ", ".join(handles)
    return (
        "Team consent201a: seat 1\n"
        f"  {joined} {t['opening']}, and runs as you.\n\n"
        "  That means it can reach what you can, including the Element\n"
        f"Launch › Element consent\nhost seats: {joined}\n"
        f"{t['question']}{joined}?\n\n  Yes     No  \n"
    )


class ProductTexts(unittest.TestCase):
    def test_every_text_is_read_from_this_checkout(self) -> None:
        t = texts()
        self.assertEqual(t["question"], "Accept, and start ")
        self.assertTrue(t["declined"].startswith("Not started. Nothing was recorded"))
        self.assertTrue(t["opening"].startswith("join their teams"))
        self.assertIn("Token Selection for cc", t["tokens"])

    def test_a_text_the_source_no_longer_has_fails_rather_than_guessing(self) -> None:
        with self.assertRaises(cc.Failed):
            cc.product_texts(consent_go="package chooser\n", tokens_bash='echo "Claude Code Token Selection for cc"\n')
        with self.assertRaises(cc.Failed):
            cc.product_texts(tokens_bash="echo nothing\n")


class Screen(unittest.TestCase):
    def test_nothing_known_yet(self) -> None:
        self.assertEqual(cc.screen("$ \n"), "none")

    def test_the_consent_question(self) -> None:
        self.assertEqual(cc.screen(consent_screen([H1, H2])), "consent")

    def test_the_decline_after_the_question_wins(self) -> None:
        pane = consent_screen([H1, H2]) + texts()["declined"] + "\n"
        self.assertEqual(cc.screen(pane), "declined")

    def test_the_token_menu_after_the_question(self) -> None:
        pane = consent_screen([H1, H2]) + "\n" + texts()["tokens"] + "\n  1) work\n  d) Desktop\n"
        self.assertEqual(cc.screen(pane), "tokens")

    def test_a_sudo_prompt_on_the_last_line(self) -> None:
        pane = consent_screen([H1, H2]) + "creating seat 1@consent201a\n[sudo] password for someone: \n\n"
        self.assertEqual(cc.screen(pane), "sudo")

    def test_sudo_rs_prompt_too(self) -> None:
        pane = consent_screen([H1, H2]) + "[sudo: authenticate] Password: \n"
        self.assertEqual(cc.screen(pane), "sudo")

    def test_an_answered_sudo_prompt_is_not_the_screen(self) -> None:
        pane = consent_screen([H1, H2]) + "[sudo] password for someone: \n" + texts()["tokens"] + "\n"
        self.assertEqual(cc.screen(pane), "tokens")


class ExpectConsent(unittest.TestCase):
    def test_both_handles_named_in_order(self) -> None:
        cc.expect_consent(consent_screen([H1, H2]), [H1, H2])

    def test_a_missing_handle_fails(self) -> None:
        with self.assertRaises(cc.Failed):
            cc.expect_consent(consent_screen([H1]), [H1, H2])

    def test_another_handle_fails(self) -> None:
        with self.assertRaises(cc.Failed):
            cc.expect_consent(consent_screen([H1, "other.1+desk.host"]), [H1, H2])

    def test_no_question_fails(self) -> None:
        with self.assertRaises(cc.Failed):
            cc.expect_consent("$ cc --teams\n", [H1, H2])

    def test_the_words_must_name_them_too(self) -> None:
        pane = f"{texts()['question']}{H1}, {H2}?\n"
        with self.assertRaises(cc.Failed):
            cc.expect_consent(pane, [H1, H2])


class DefaultSeat(unittest.TestCase):
    def test_the_new_default_and_its_handle(self) -> None:
        out = f"CHOICE\t1\tnew\t{H1}\ta host session\nDEFAULT\t1\n"
        self.assertEqual(cc.default_seat(out), ("1", H1))

    def test_a_default_that_already_exists_is_a_leftover(self) -> None:
        out = f"CHOICE\t1\tfree\t{H1}\ta host session\nCHOICE\t2\tnew\t{H2}\ta host session\nDEFAULT\t1\n"
        with self.assertRaises(cc.Failed):
            cc.default_seat(out)

    def test_no_default_line_fails(self) -> None:
        with self.assertRaises(cc.Failed):
            cc.default_seat(f"CHOICE\t1\tnew\t{H1}\ta host session\n")

    def test_a_default_with_no_row_fails(self) -> None:
        with self.assertRaises(cc.Failed):
            cc.default_seat(f"CHOICE\t1\tnew\t{H1}\ta host session\nDEFAULT\t2\n")


class NotMembers(unittest.TestCase):
    def test_absent_handle_passes(self) -> None:
        cc.expect_not_member("MEMBER\tother.1+desk.host\thost\tworker\tactive\n", H1)

    def test_present_handle_fails_whatever_its_state(self) -> None:
        for state in ("active", "parked"):
            with self.subTest(state=state), self.assertRaises(cc.Failed):
                cc.expect_not_member(f"MEMBER\t{H1}\thost\tworker\t{state}\n", H1)


class SeatsHeld(unittest.TestCase):
    WANT = [("1@consent201a", H1), ("1@consent201b", H2)]

    def test_both_held_with_their_handles(self) -> None:
        listing = f"SEAT\tconsent201a\t1\theld\t-\t{H1}\nSEAT\tconsent201b\t1\theld\t-\t{H2}\n"
        self.assertEqual(cc.seats_held(listing, self.WANT), cc.EXIT_OK)

    def test_one_not_there_yet(self) -> None:
        listing = f"SEAT\tconsent201a\t1\theld\t-\t{H1}\n"
        self.assertEqual(cc.seats_held(listing, self.WANT), cc.EXIT_NOT_YET)

    def test_one_free_yet(self) -> None:
        listing = f"SEAT\tconsent201a\t1\theld\t-\t{H1}\nSEAT\tconsent201b\t1\tfree\t-\t{H2}\n"
        self.assertEqual(cc.seats_held(listing, self.WANT), cc.EXIT_NOT_YET)

    def test_another_handle_in_the_seat_fails(self) -> None:
        listing = f"SEAT\tconsent201a\t1\theld\t-\t{H1}\nSEAT\tconsent201b\t1\theld\t-\tother.1+desk.host\n"
        with self.assertRaises(cc.Failed):
            cc.seats_held(listing, self.WANT)

    def test_a_want_that_is_not_seat_at_team_equals_handle_is_usage(self) -> None:
        with self.assertRaises(cc.Failed):
            cc.parse_wants(["1@consent201a"])
        self.assertEqual(cc.parse_wants([f"1@consent201a={H1}"]), [("1@consent201a", H1)])


class ExpectRecord(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = pathlib.Path(self.tmp.name) / "element-accepted"

    def write(self, mode: int = 0o600) -> None:
        self.path.write_text("accepted 2026-10-28T10:00:00Z\n", encoding="ascii")
        self.path.chmod(mode)

    def test_a_new_owner_only_file_passes(self) -> None:
        self.write()
        cc.expect_record(str(self.path), os.getuid(), self.path.stat().st_mtime - 5)

    def test_missing_fails(self) -> None:
        with self.assertRaises(cc.Failed):
            cc.expect_record(str(self.path), os.getuid(), 0)

    def test_open_to_others_fails(self) -> None:
        self.write(0o644)
        with self.assertRaises(cc.Failed):
            cc.expect_record(str(self.path), os.getuid(), 0)

    def test_older_than_the_run_fails(self) -> None:
        self.write()
        with self.assertRaises(cc.Failed):
            cc.expect_record(str(self.path), os.getuid(), self.path.stat().st_mtime + 60)

    def test_another_owner_fails(self) -> None:
        self.write()
        with self.assertRaises(cc.Failed):
            cc.expect_record(str(self.path), os.getuid() + 1, 0)

    def test_a_symlink_fails(self) -> None:
        self.write()
        link = pathlib.Path(self.tmp.name) / "link"
        link.symlink_to(self.path)
        with self.assertRaises(cc.Failed):
            cc.expect_record(str(link), os.getuid(), 0)


class Main(unittest.TestCase):
    def run_main(self, *argv: str) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = cc.main(list(argv))
        return code, out.getvalue(), err.getvalue()

    def test_screen_prints_the_state(self) -> None:
        with tempfile.NamedTemporaryFile("w", suffix=".pane", delete=False) as handle:
            handle.write(consent_screen([H1, H2]))
        self.addCleanup(os.unlink, handle.name)
        self.assertEqual(self.run_main("screen", handle.name), (0, "consent\n", ""))

    def test_seats_held_not_yet_is_three(self) -> None:
        with tempfile.NamedTemporaryFile("w", suffix=".list", delete=False) as handle:
            handle.write("")
        self.addCleanup(os.unlink, handle.name)
        code, _, _ = self.run_main("seats-held", handle.name, f"1@consent201a={H1}")
        self.assertEqual(code, cc.EXIT_NOT_YET)

    def test_a_failed_expectation_is_one_with_the_reason_on_stderr(self) -> None:
        with tempfile.NamedTemporaryFile("w", suffix=".list", delete=False) as handle:
            handle.write(f"MEMBER\t{H1}\thost\tworker\tactive\n")
        self.addCleanup(os.unlink, handle.name)
        code, out, err = self.run_main("expect-not-member", handle.name, H1)
        self.assertEqual((code, out), (cc.EXIT_FAIL, ""))
        self.assertIn(H1, err)

    def test_unknown_command_and_wrong_count_are_usage(self) -> None:
        self.assertEqual(self.run_main("nope")[0], cc.EXIT_USAGE)
        self.assertEqual(self.run_main("screen")[0], cc.EXIT_USAGE)
        self.assertEqual(self.run_main()[0], cc.EXIT_USAGE)


if __name__ == "__main__":
    unittest.main()
