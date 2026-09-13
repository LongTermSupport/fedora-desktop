"""Unit tests for helpers/vmtest/spool.py — symlink-safe, read-once spool I/O (DESIGN.md §6.3).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_spool

The spool lives inside the bind-mounted checkout, so the sandbox can restructure
it at will and the host writes into it as the host user. These tests include
the attacks, not only the happy path: a symlinked `untracked/` (the
component-walk case), a symlinked `responses/`, `diagnostics/` and
`archive/<run_id>/`, a `requests/` entry that is a symlink, a FIFO and an
oversized request, a body swapped between read and dispatch, and a filename
verb that disagrees with the body verb.

Every attack asserts on the REFUSAL, never on a particular errno:
`O_PATH|O_NOFOLLOW` on a symlink succeeds, so a test expecting ELOOP would pass
against a symlink while proving nothing.
"""

from __future__ import annotations

import json
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.vmtest import spool

GOOD_NAME = "20260913T114500Z-run-scenario-0123456789abcdef.json"


def _body(verb="run-scenario", argument="server-fast-provision", nonce="0123456789abcdef", **extra):
    return json.dumps({"verb": verb, "argument": argument, "nonce": nonce, **extra}).encode()


class SpoolCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.checkout = pathlib.Path(self._tmp.name) / "checkout"
        self.bridge = self.checkout / "untracked" / "vmtest-bridge"
        for name in spool.SPOOL_DIRS:
            (self.bridge / name).mkdir(parents=True)
        self.outside = pathlib.Path(self._tmp.name) / "outside"
        self.outside.mkdir()
        self.fds: list[int] = []

    def tearDown(self):
        for fd in self.fds:
            os.close(fd)

    def root(self):
        fd = spool.open_root(str(self.checkout))
        self.fds.append(fd)
        return fd

    def subdir(self, root, name):
        fd = spool.open_subdir(root, name)
        self.fds.append(fd)
        return fd

    def put_request(self, name=GOOD_NAME, body=None):
        (self.bridge / "requests" / name).write_bytes(body if body is not None else _body())


class TestOpenRoot(SpoolCase):
    def test_walks_to_the_bridge_root(self):
        fd = self.root()
        self.assertTrue(os.path.samefile(f"/proc/self/fd/{fd}", self.bridge))

    def test_symlinked_untracked_is_refused(self):
        # The component-walk case: O_NOFOLLOW on the final component alone
        # would still traverse a symlinked untracked/.
        real = pathlib.Path(self._tmp.name) / "real-untracked"
        (self.bridge.parent).rename(real)
        (self.checkout / "untracked").symlink_to(real)
        with self.assertRaises(spool.SpoolRefusal):
            spool.open_root(str(self.checkout))

    def test_symlinked_bridge_dir_is_refused(self):
        real = pathlib.Path(self._tmp.name) / "real-bridge"
        self.bridge.rename(real)
        self.bridge.symlink_to(real)
        with self.assertRaises(spool.SpoolRefusal):
            spool.open_root(str(self.checkout))

    def test_missing_bridge_is_refused_not_created(self):
        for name in spool.SPOOL_DIRS:
            (self.bridge / name).rmdir()
        self.bridge.rmdir()
        with self.assertRaises(spool.SpoolRefusal):
            spool.open_root(str(self.checkout))
        self.assertFalse(self.bridge.exists())

    def test_relative_checkout_is_refused(self):
        with self.assertRaises(spool.SpoolRefusal):
            spool.open_root("checkout")


class TestOpenSubdir(SpoolCase):
    def test_opens_each_host_facing_directory(self):
        root = self.root()
        for name in spool.HOST_DIRS:
            with self.subTest(name=name):
                fd = self.subdir(root, name)
                self.assertTrue(os.path.samefile(f"/proc/self/fd/{fd}", self.bridge / name))

    def test_tmp_is_never_opened_by_the_host(self):
        with self.assertRaises(spool.SpoolRefusal):
            spool.open_subdir(self.root(), "tmp")

    def test_symlinked_responses_diagnostics_and_quarantine_are_refused(self):
        root = self.root()
        for name in ("responses", "diagnostics", "quarantine", "requests", "processing", "archive"):
            with self.subTest(name=name):
                (self.bridge / name).rmdir()
                (self.bridge / name).symlink_to(self.outside)
                with self.assertRaises(spool.SpoolRefusal):
                    spool.open_subdir(root, name)
                (self.bridge / name).unlink()
                (self.bridge / name).mkdir()

    def test_a_regular_file_in_place_of_a_directory_is_refused(self):
        root = self.root()
        (self.bridge / "responses").rmdir()
        (self.bridge / "responses").write_text("not a dir")
        with self.assertRaises(spool.SpoolRefusal):
            spool.open_subdir(root, "responses")

    def test_unknown_directory_names_are_refused(self):
        with self.assertRaises(spool.SpoolRefusal):
            spool.open_subdir(self.root(), "../../etc")


class TestRequests(SpoolCase):
    def test_lists_well_formed_names_and_flags_the_rest(self):
        self.put_request()
        self.put_request("evil.json")
        self.put_request("20260913T114500Z-run-scenario-0123456789abcdef.json.bak")
        requests = self.subdir(self.root(), "requests")
        listing = spool.list_requests(requests)
        self.assertEqual(listing.valid, [GOOD_NAME])
        self.assertEqual(sorted(listing.malformed), ["20260913T114500Z-run-scenario-0123456789abcdef.json.bak", "evil.json"])

    def test_read_returns_the_bytes_exactly_once(self):
        self.put_request()
        requests = self.subdir(self.root(), "requests")
        self.assertEqual(spool.read_request(requests, GOOD_NAME), _body())

    def test_symlinked_request_entry_is_malformed(self):
        (self.outside / "payload.json").write_bytes(_body())
        (self.bridge / "requests" / GOOD_NAME).symlink_to(self.outside / "payload.json")
        requests = self.subdir(self.root(), "requests")
        with self.assertRaises(spool.SpoolMalformed):
            spool.read_request(requests, GOOD_NAME)

    def test_fifo_request_is_malformed_not_a_hang(self):
        os.mkfifo(self.bridge / "requests" / GOOD_NAME)
        requests = self.subdir(self.root(), "requests")
        with self.assertRaises(spool.SpoolMalformed):
            spool.read_request(requests, GOOD_NAME)

    def test_oversized_request_is_malformed(self):
        self.put_request(body=b"x" * (spool.REQUEST_SIZE_CAP + 1))
        requests = self.subdir(self.root(), "requests")
        with self.assertRaises(spool.SpoolMalformed):
            spool.read_request(requests, GOOD_NAME)

    def test_size_cap_is_small(self):
        # A request is a verb, an argument and a nonce. Anything approaching a
        # megabyte is not one.
        self.assertLessEqual(spool.REQUEST_SIZE_CAP, 64 * 1024)

    def test_claim_moves_by_rename_into_processing(self):
        self.put_request()
        root = self.root()
        requests = self.subdir(root, "requests")
        processing = self.subdir(root, "processing")
        spool.claim(requests, GOOD_NAME, processing)
        self.assertFalse((self.bridge / "requests" / GOOD_NAME).exists())
        self.assertTrue((self.bridge / "processing" / GOOD_NAME).exists())

    def test_quarantine_moves_malformed_input_aside(self):
        self.put_request("evil.json", b"junk")
        root = self.root()
        requests = self.subdir(root, "requests")
        quarantine = self.subdir(root, "quarantine")
        spool.quarantine(requests, "evil.json", quarantine)
        self.assertTrue((self.bridge / "quarantine" / "evil.json").exists())


class TestParseRequest(SpoolCase):
    def test_parses_verb_argument_and_nonce(self):
        request = spool.parse_request(GOOD_NAME, _body())
        self.assertEqual((request.verb, request.argument, request.nonce), ("run-scenario", "server-fast-provision", "0123456789abcdef"))
        self.assertEqual(request.name, GOOD_NAME)

    def test_body_is_judged_not_the_file(self):
        # D2: read once, judge the buffer. Swapping the file after the read must
        # not change what is dispatched.
        self.put_request()
        requests = self.subdir(self.root(), "requests")
        body = spool.read_request(requests, GOOD_NAME)
        (self.bridge / "requests" / GOOD_NAME).write_bytes(_body(argument="rm-rf-everything"))
        self.assertEqual(spool.parse_request(GOOD_NAME, body).argument, "server-fast-provision")

    def test_denylisted_verb_is_rejected_before_anything_else(self):
        for verb in ("exec", "shell", "sh", "bash", "run", "eval", "system", "ansible", "ansible-playbook"):
            with self.subTest(verb=verb):
                name = f"20260913T114500Z-{verb}-0123456789abcdef.json"
                with self.assertRaises(spool.RequestRejected) as caught:
                    spool.parse_request(name, _body(verb=verb))
                self.assertEqual(caught.exception.code, "denylisted-verb")

    def test_denylist_is_checked_on_the_filename_before_the_body_is_trusted(self):
        # §6.4 step 4 runs before step 5. A denylisted filename verb with a
        # valid body verb must be reported as denylisted, not as a mismatch —
        # the deny list is the first thing consulted, from the cheapest signal.
        name = "20260913T114500Z-exec-0123456789abcdef.json"
        with self.assertRaises(spool.RequestRejected) as caught:
            spool.parse_request(name, _body(verb="run-scenario"))
        self.assertEqual(caught.exception.code, "denylisted-verb")
        with self.assertRaises(spool.RequestRejected) as caught:
            spool.parse_request(GOOD_NAME, _body(verb="exec"))
        self.assertEqual(caught.exception.code, "denylisted-verb")

    def test_verb_outside_the_set_is_rejected(self):
        name = "20260913T114500Z-list-everything-0123456789abcdef.json"
        with self.assertRaises(spool.RequestRejected) as caught:
            spool.parse_request(name, _body(verb="list-everything"))
        self.assertEqual(caught.exception.code, "unknown-verb")

    def test_filename_verb_must_equal_body_verb(self):
        with self.assertRaises(spool.RequestRejected) as caught:
            spool.parse_request(GOOD_NAME, _body(verb="lab-status"))
        self.assertEqual(caught.exception.code, "verb-mismatch")

    def test_filename_nonce_must_equal_body_nonce(self):
        with self.assertRaises(spool.RequestRejected) as caught:
            spool.parse_request(GOOD_NAME, _body(nonce="fedcba9876543210"))
        self.assertEqual(caught.exception.code, "nonce-mismatch")

    def test_argument_grammar_is_enforced_before_the_enumeration(self):
        for bad in ("Server-Fast", "a b", "x;y", "../x", ""):
            with self.subTest(argument=bad):
                with self.assertRaises(spool.RequestRejected) as caught:
                    spool.parse_request(GOOD_NAME, _body(argument=bad))
                self.assertEqual(caught.exception.code, "bad-argument")

    def test_verbs_without_an_argument_take_none(self):
        name = "20260913T114500Z-lab-status-0123456789abcdef.json"
        request = spool.parse_request(name, _body(verb="lab-status", argument=None))
        self.assertIsNone(request.argument)
        with self.assertRaises(spool.RequestRejected) as caught:
            spool.parse_request(name, _body(verb="lab-status", argument="x"))
        self.assertEqual(caught.exception.code, "bad-argument")

    def test_malformed_json_and_wrong_shapes_are_rejected(self):
        for body in (b"{not json", b"[]", b'{"verb": "run-scenario"}', _body(extra="field")):
            with self.subTest(body=body):
                with self.assertRaises(spool.RequestRejected) as caught:
                    spool.parse_request(GOOD_NAME, body)
                self.assertEqual(caught.exception.code, "bad-body")

    def test_verb_set_is_exactly_the_five(self):
        self.assertEqual(spool.VERBS, frozenset({"list-scenarios", "lab-status", "run-scenario", "refresh-base", "abort-run"}))


class TestWrites(SpoolCase):
    def test_write_response_lands_the_file_atomically(self):
        root = self.root()
        responses = self.subdir(root, "responses")
        spool.write_file(responses, f"{GOOD_NAME}.response.json", b'{"state": "accepted"}\n')
        self.assertEqual((self.bridge / "responses" / f"{GOOD_NAME}.response.json").read_bytes(), b'{"state": "accepted"}\n')
        self.assertEqual(sorted(p.name for p in (self.bridge / "responses").iterdir()), [f"{GOOD_NAME}.response.json"])

    def test_write_replaces_an_existing_file_and_a_symlink_in_place(self):
        # The heartbeat rewrites a fixed name every interval; a symlink planted
        # there must be replaced, never followed.
        root = self.root()
        diagnostics = self.subdir(root, "diagnostics")
        target = self.outside / "victim"
        target.write_text("precious")
        (self.bridge / "diagnostics" / "bridge-heartbeat.json").symlink_to(target)
        spool.write_file(diagnostics, "bridge-heartbeat.json", b"{}")
        self.assertEqual(target.read_text(), "precious")
        self.assertFalse((self.bridge / "diagnostics" / "bridge-heartbeat.json").is_symlink())
        self.assertEqual((self.bridge / "diagnostics" / "bridge-heartbeat.json").read_bytes(), b"{}")

    def test_write_refuses_names_with_separators(self):
        responses = self.subdir(self.root(), "responses")
        for name in ("../x", "a/b", "", ".", ".."):
            with self.subTest(name=name):
                with self.assertRaises(spool.SpoolRefusal):
                    spool.write_file(responses, name, b"x")

    def test_archive_dir_is_created_fresh_and_a_symlink_there_is_refused(self):
        root = self.root()
        archive = self.subdir(root, "archive")
        fd = spool.create_archive_dir(archive, "20260913T114501Z-server-fast-provision")
        self.fds.append(fd)
        self.assertTrue((self.bridge / "archive" / "20260913T114501Z-server-fast-provision").is_dir())
        with self.assertRaises(spool.SpoolRefusal):
            spool.create_archive_dir(archive, "20260913T114501Z-server-fast-provision")
        (self.bridge / "archive" / "planted").symlink_to(self.outside)
        with self.assertRaises(spool.SpoolRefusal):
            spool.create_archive_dir(archive, "planted")
        self.assertEqual(sorted(p.name for p in self.outside.iterdir()), [])

    def test_transcript_written_through_the_held_archive_fd(self):
        root = self.root()
        archive = self.subdir(root, "archive")
        run_fd = spool.create_archive_dir(archive, "run-1")
        self.fds.append(run_fd)
        spool.write_file(run_fd, "transcript.log", b"hello\n")
        self.assertEqual((self.bridge / "archive" / "run-1" / "transcript.log").read_bytes(), b"hello\n")

    def test_run_id_grammar(self):
        archive = self.subdir(self.root(), "archive")
        for bad in ("../x", "a/b", "", "Run 1"):
            with self.subTest(run_id=bad):
                with self.assertRaises(spool.SpoolRefusal):
                    spool.create_archive_dir(archive, bad)


if __name__ == "__main__":
    unittest.main()
