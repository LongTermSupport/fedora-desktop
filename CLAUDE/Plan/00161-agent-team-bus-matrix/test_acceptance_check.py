"""Tests for acceptance_check.py, the pure logic behind this plan's acceptance.bash (U17).

Nothing here touches the network, a homeserver or the host: every function takes text or
decoded JSON and returns a value or a list of problems.

Run from anywhere:
    python3 CLAUDE/Plan/00161-agent-team-bus-matrix/test_acceptance_check.py
"""

from __future__ import annotations

import importlib.util
import io
import json
import pathlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

_MODULE_PATH = pathlib.Path(__file__).resolve().parent / "acceptance_check.py"
_SPEC = importlib.util.spec_from_file_location("acceptance_check", _MODULE_PATH)
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_MODULE_PATH}")
ac = importlib.util.module_from_spec(_SPEC)
sys.modules["acceptance_check"] = ac
_SPEC.loader.exec_module(ac)

TEAM = "acceptance"
SN = "acceptance.agent-bus.internal"
A = "acceptance.1+acceptance.host"
B = "acceptance.2+acceptance.host"
EV1 = "$" + "a" * 43
EV2 = "$" + "b" * 43
SHA = "0123456789abcdef0123456789abcdef01234567"
REF = f"path:example-org/project@{SHA}:CLAUDE/Plan/x/DESIGN.md"


def line(*fields: str) -> str:
    return "\t".join(fields)


def run_main(*argv: str, stdin: str = "") -> tuple[int, str, str]:
    out, err = io.StringIO(), io.StringIO()
    old_stdin = sys.stdin
    sys.stdin = io.StringIO(stdin)
    try:
        with redirect_stdout(out), redirect_stderr(err):
            status = ac.main(list(argv))
    finally:
        sys.stdin = old_stdin
    return status, out.getvalue(), err.getvalue()


class GithubRepoTest(unittest.TestCase):
    def test_every_github_remote_form_gives_lowercase_owner_and_repo(self) -> None:
        for url in ("https://github.com/Example-Org/Project.git", "https://github.com/example-org/project",
                    "https://github.com/example-org/project/", "git@github.com:Example-Org/project.git",
                    "ssh://git@github.com/example-org/Project.git"):
            with self.subTest(url=url):
                self.assertEqual(ac.github_repo(url), "example-org/project")

    def test_a_remote_elsewhere_or_malformed_is_refused(self) -> None:
        for url in ("https://example.com/example-org/project.git", "git@example.com:a/b.git",
                    "https://github.com/example-org", "https://github.com/a/b/c", "",
                    "https://user:pw" + "@github.com/a/b.git"):  # credentials in the URL
            with self.subTest(url=url), self.assertRaises(ValueError):
                ac.github_repo(url)


class TeamFileTest(unittest.TestCase):
    def test_the_team_file_names_the_bus_address_the_repo_and_the_human(self) -> None:
        data = ac.team_file(TEAM, 45001, "203.0.113.9", "tester", "example-org/project", "main",
                            "CLAUDE/Plan/", "https://api.github.com")
        self.assertEqual(data, {
            "team": TEAM, "port": 45001, "listen": ["203.0.113.9"], "allow_from": ["192.0.2.0/24"],
            "humans": ["tester"], "repos": [{"repo": "example-org/project", "branches": ["main"]}],
            "path_prefixes": ["CLAUDE/Plan/"], "forge_api": "https://api.github.com",
        })

    def test_it_passes_the_admin_tool_validator(self) -> None:
        from helpers.agent_bus import teamfile

        for forge_api in ("https://api.github.com", "https://api.example.com"):
            with self.subTest(forge_api=forge_api):
                data = ac.team_file(TEAM, 45001, "203.0.113.9", "tester", "example-org/project", "F44",
                                    "CLAUDE/Plan/", forge_api)
                self.assertEqual(teamfile.parse_team_file(data).team, TEAM)

    def test_the_command_takes_dash_as_pingbus_github_api(self) -> None:
        args = ("team-file", TEAM, "45001", "203.0.113.9", "owner", "example/project", "main", "CLAUDE/Plan/")
        status, out, _ = run_main(*args, "-")
        self.assertEqual((status, json.loads(out)["forge_api"]), (0, ac.forge.GITHUB_API))
        status, out, _ = run_main(*args, "https://api.example.com")
        self.assertEqual((status, json.loads(out)["forge_api"]), (0, "https://api.example.com"))


class MemberTest(unittest.TestCase):
    def test_a_limit_is_added_without_touching_the_rest(self) -> None:
        member = {"team": TEAM, "user_id": f"@{A}:{SN}"}
        self.assertEqual(ac.with_limit(member, "ack_timeout_s", 60),
                         {"team": TEAM, "user_id": f"@{A}:{SN}", "limits": {"ack_timeout_s": 60}})
        self.assertNotIn("limits", member)

    def test_a_limit_pingbus_does_not_know_or_out_of_bounds_is_refused(self) -> None:
        for key, value in (("no_such_limit", 60), ("ack_timeout_s", 59)):
            with self.subTest(key=key), self.assertRaises(ValueError):
                ac.with_limit({}, key, value)

    def test_the_handle_is_the_localpart_of_an_agent_user_id(self) -> None:
        self.assertEqual(ac.handle_of(f"@{A}:{SN}"), A)
        for bad in (f"@tester:{SN}", "acceptance.1+acceptance.host", f"@{A}"):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                ac.handle_of(bad)


class HumanApiTest(unittest.TestCase):
    def test_the_login_body_is_a_password_login(self) -> None:
        self.assertEqual(ac.login_body("tester", "pw"), {
            "type": "m.login.password", "identifier": {"type": "m.id.user", "user": "tester"},
            "password": "pw", "initial_device_display_name": "agent-bus acceptance",
        })

    def test_the_access_token_is_read_from_the_login_response(self) -> None:
        self.assertEqual(ac.access_token({"access_token": "syt_abc", "device_id": "D"}), "syt_abc")
        for bad in ({}, {"access_token": ""}, {"access_token": "a b"}, {"access_token": 5}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                ac.access_token(bad)

    def test_the_human_message_mentions_only_the_addressed_member(self) -> None:
        self.assertEqual(ac.human_message("hello", [f"@{A}:{SN}"]), {
            "msgtype": "m.text", "body": "hello", "m.mentions": {"user_ids": [f"@{A}:{SN}"]},
        })

    def test_the_event_id_is_read_from_a_send_response(self) -> None:
        self.assertEqual(ac.event_id_of({"event_id": EV1}), EV1)
        for bad in ({}, {"event_id": "$short"}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                ac.event_id_of(bad)

    def test_a_room_id_is_quoted_for_a_url_path(self) -> None:
        room = "!" + "R" * 43
        self.assertEqual(ac.room_path(room), "%21" + "R" * 43)
        with self.assertRaises(ValueError):
            ac.room_path("!short")


class LinesTest(unittest.TestCase):
    def test_sent_gives_the_one_event_id(self) -> None:
        self.assertEqual(ac.sent_event(line("SENT", "1", TEAM, EV1) + "\n", TEAM), EV1)

    def test_sent_refuses_anything_but_exactly_one_sent_line(self) -> None:
        for text in ("", line("SENT", "1", "other", EV1) + "\n",
                     line("SENT", "1", TEAM, EV1) + "\n" + line("SENT", "1", TEAM, EV2) + "\n"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                ac.sent_event(text, TEAM)

    def test_a_malformed_line_is_refused(self) -> None:
        for text in ("NOPE\t1\tx\n", line("SENT", "1", TEAM) + "\n", line("SENT", "2", TEAM, EV1) + "\n"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                ac.parse_lines(text)

    def test_the_expected_ping_alone_passes(self) -> None:
        text = line("PING", "1", TEAM, EV1, A, "review", REF, "-") + "\n"
        self.assertEqual(ac.expect_ping(text, TEAM, EV1, A, "review", REF, None), [])

    def test_a_ping_with_any_field_different_fails(self) -> None:
        good = ("PING", "1", TEAM, EV1, A, "review", REF, "-")
        for index, value in ((2, "other"), (3, EV2), (4, B), (5, "fetch"), (7, EV2)):
            fields = list(good)
            fields[index] = value
            with self.subTest(index=index):
                self.assertTrue(ac.expect_ping(line(*fields) + "\n", TEAM, EV1, A, "review", REF, None))

    def test_an_extra_line_or_none_fails(self) -> None:
        ping = line("PING", "1", TEAM, EV1, A, "review", REF, "-")
        timeout = line("TIMEOUT", "1", TEAM, EV2, B, "review", REF)
        self.assertTrue(ac.expect_ping(f"{ping}\n{timeout}\n", TEAM, EV1, A, "review", REF, None))
        self.assertTrue(ac.expect_ping("", TEAM, EV1, A, "review", REF, None))

    def test_an_ack_names_its_re_and_no_ref(self) -> None:
        text = line("PING", "1", TEAM, EV2, B, "ack", "-", EV1) + "\n"
        self.assertEqual(ac.expect_ping(text, TEAM, EV2, B, "ack", None, EV1), [])
        self.assertTrue(ac.expect_ping(text, TEAM, EV2, B, "ack", None, EV2))

    def test_the_expected_human_line_passes_with_its_text_decoded(self) -> None:
        text = line("HUMAN", "1", TEAM, EV1, "tester", "1791234567890", json.dumps("hi\tthere")) + "\n"
        self.assertEqual(ac.expect_human(text, TEAM, EV1, "tester", "hi\tthere"), [])
        self.assertTrue(ac.expect_human(text, TEAM, EV1, "tester", "hi there"))
        self.assertTrue(ac.expect_human(text, TEAM, EV1, "other", "hi\tthere"))

    def test_a_human_line_with_a_bad_timestamp_or_text_fails(self) -> None:
        for ts, body in (("-1", json.dumps("hi")), ("x", json.dumps("hi")), ("1", "hi")):
            text = line("HUMAN", "1", TEAM, EV1, "tester", ts, body) + "\n"
            with self.subTest(ts=ts, body=body):
                self.assertTrue(ac.expect_human(text, TEAM, EV1, "tester", "hi"))

    def test_the_expected_timeout_alone_passes(self) -> None:
        text = line("TIMEOUT", "1", TEAM, EV2, B, "review", REF) + "\n"
        self.assertEqual(ac.expect_timeout(text, TEAM, EV2, B, "review", REF), [])
        both = text + line("TIMEOUT", "1", TEAM, EV1, B, "review", REF) + "\n"
        self.assertTrue(ac.expect_timeout(both, TEAM, EV2, B, "review", REF))

    def test_absent_passes_when_no_line_names_the_event_and_no_human_line(self) -> None:
        self.assertEqual(ac.expect_absent("", EV1), [])
        other = line("PING", "1", TEAM, EV2, A, "review", REF, "-") + "\n"
        self.assertEqual(ac.expect_absent(other, EV1), [])
        self.assertTrue(ac.expect_absent(line("PING", "1", TEAM, EV2, A, "ack", "-", EV1) + "\n", EV1))
        human = line("HUMAN", "1", TEAM, EV2, "tester", "1", json.dumps("x")) + "\n"
        self.assertTrue(ac.expect_absent(human, EV1))


class SendOutcomeTest(unittest.TestCase):
    def test_success_is_zero(self) -> None:
        self.assertEqual(ac.send_outcome(0, ""), 0)

    def test_an_unreachable_or_rate_limited_forge_could_not_be_established(self) -> None:
        for code, err in ((5, "pingbus: forge check refused: forge-unreachable: compare: URLError\n"),
                          (9, "pingbus: forge check refused: forge-rate: compare: rate limited by the forge\n")):
            with self.subTest(err=err):
                self.assertEqual(ac.send_outcome(code, err), 2)

    def test_pingbus_own_message_for_an_unanswered_forge_is_recognised(self) -> None:
        for code in (ac.forge.UNREACHABLE_REFUSAL, ac.forge.RATE_REFUSAL):
            with self.subTest(code=code):
                status, message = ac.cli.failure(ac.forge.ForgeError(code, "compare: x"))
                self.assertEqual(ac.send_outcome(status, f"pingbus: {message}\n"), 2)
        status, message = ac.cli.failure(ac.forge.ForgeError("provenance", "x"))
        self.assertEqual(ac.send_outcome(status, f"pingbus: {message}\n"), 1)

    def test_every_other_refusal_fails(self) -> None:
        for code, err in ((5, "pingbus: forge check refused: provenance: x\n"), (9, "pingbus: rate limited: duplicate\n"),
                          (10, "pingbus: the team room is not trusted: x\n"), (5, "")):
            with self.subTest(err=err):
                self.assertEqual(ac.send_outcome(code, err), 1)


class MainTest(unittest.TestCase):
    def test_login_body_reads_the_password_from_stdin_only(self) -> None:
        status, out, err = run_main("login-body", "tester", stdin="pw-from-stdin\n")
        self.assertEqual(status, 0)
        self.assertEqual(json.loads(out)["password"], "pw-from-stdin")
        self.assertNotIn("pw-from-stdin", err)

    def test_set_limit_rewrites_the_member_file_in_place(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "member.json"
            path.write_text(json.dumps({"team": TEAM}), encoding="utf-8")
            path.chmod(0o600)
            self.assertEqual(run_main("set-limit", str(path), "ack_timeout_s", "60")[0], 0)
            self.assertEqual(json.loads(path.read_text(encoding="utf-8"))["limits"], {"ack_timeout_s": 60})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_an_expectation_that_fails_exits_1_and_says_why_on_stderr(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "out"
            path.write_text("", encoding="utf-8")
            status, out, err = run_main("expect-ping", str(path), TEAM, EV1, A, "review", REF, "-")
        self.assertEqual((status, out), (1, ""))
        self.assertIn("PING", err)

    def test_send_outcome_exits_with_the_verdict(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "err"
            path.write_text("pingbus: forge check refused: forge-rate: x\n", encoding="utf-8")
            self.assertEqual(run_main("send-outcome", "9", str(path))[0], 2)

    def test_a_bad_command_is_usage(self) -> None:
        self.assertEqual(run_main("no-such-command")[0], 64)


if __name__ == "__main__":
    unittest.main()
