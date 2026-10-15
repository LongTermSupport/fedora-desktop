"""Tests for m7_check.py, the pure logic behind acceptance.bash (Plan 00201 M7).

Nothing here touches the network, a homeserver, podman or a real session: every command takes
text, decoded JSON or a temporary file and prints a value or a verdict. The team file and the
throwaway host_vars it writes are read back by the real teamfile and declare parsers, so the
acceptance run cannot hand the installer or ccy-teams something they would refuse.

Run from anywhere:
    python3 CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/test_m7_check.py
"""

from __future__ import annotations

import importlib
import importlib.util
import io
import json
import pathlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

_HERE = pathlib.Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location("m7_check", _HERE / "m7_check.py")
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_HERE / 'm7_check.py'}")
mc = importlib.util.module_from_spec(_SPEC)
sys.modules["m7_check"] = mc
_SPEC.loader.exec_module(mc)


def _repo_root() -> pathlib.Path:
    for candidate in (_HERE, *_HERE.parents):
        if (candidate / "ansible.cfg").exists():
            return candidate
    raise ImportError("not inside a fedora-desktop checkout")


if str(_repo_root()) not in sys.path:
    sys.path.insert(0, str(_repo_root()))
declare = importlib.import_module("helpers.agent_bus.declare")
teamfile = importlib.import_module("helpers.agent_bus.teamfile")

TEAM_A = "accept201a"
TEAM_B = "accept201b"
DESC_A = "Plan 00201 acceptance team A: a throwaway team, removed when the run ends"
DESC_B = "Plan 00201 acceptance team B"
ADDRESS = "192.0.2.10"
SID = "0123abcd-4567-89ab-cdef-0123456789ab"
EV = "$" + "e" * 43


def run(*argv: str, stdin: str = "") -> tuple[int, str, str]:
    out, err = io.StringIO(), io.StringIO()
    old = sys.stdin
    sys.stdin = io.StringIO(stdin)
    try:
        with redirect_stdout(out), redirect_stderr(err):
            status = mc.main(list(argv))
    finally:
        sys.stdin = old
    return status, out.getvalue(), err.getvalue()


class TempDir(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = pathlib.Path(self._tmp.name)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def write(self, name: str, text: str) -> str:
        path = self.dir / name
        path.write_text(text, encoding="utf-8")
        return str(path)


class TeamFileTest(TempDir):
    def team_file(self, team: str, port: str, desc: str) -> str:
        status, out, err = run(
            "team-file", team, port, ADDRESS, "tester", "example-org", desc
        )
        self.assertEqual(status, 0, err)
        return self.write(f"{team}.json", out)

    def test_the_team_file_passes_the_installers_own_validator(self) -> None:
        path = self.team_file(TEAM_A, "6201", DESC_A)
        parsed = teamfile.parse_team_file(json.loads(pathlib.Path(path).read_text()))
        self.assertEqual(parsed.team, TEAM_A)
        self.assertEqual(parsed.description, DESC_A)
        self.assertEqual(parsed.org, "example-org")
        self.assertEqual(parsed.listen, (ADDRESS,))

    def test_a_description_the_grammar_refuses_is_refused_here(self) -> None:
        status, out, err = run(
            "team-file", TEAM_A, "6201", ADDRESS, "tester", "example-org", " padded "
        )
        self.assertEqual((status, out), (1, ""))
        self.assertIn("description", err)

    def test_the_host_vars_read_back_as_the_same_entries_and_descriptions(self) -> None:
        files = [
            self.team_file(TEAM_A, "6201", DESC_A),
            self.team_file(TEAM_B, "6202", DESC_B),
        ]
        status, out, err = run("host-vars", *files)
        self.assertEqual(status, 0, err)
        hv = declare.parse(out)
        self.assertEqual([t.team for t in hv.teams], [TEAM_A, TEAM_B])
        for team, path in zip(hv.teams, files, strict=True):
            self.assertEqual(
                declare.entry_of(team), json.loads(pathlib.Path(path).read_text())
            )
        self.assertEqual(hv.address, ADDRESS)

    def test_host_vars_needs_a_team_file(self) -> None:
        status, _, _ = run("host-vars")
        self.assertEqual(status, 64)


class SmallValuesTest(TempDir):
    def test_free_ports_are_distinct_numbers(self) -> None:
        status, out, _ = run("free-ports", "2")
        ports = out.split()
        self.assertEqual(status, 0)
        self.assertEqual(len(set(ports)), 2)
        self.assertTrue(all(p.isdigit() for p in ports))

    def test_github_owner_of_each_remote_form(self) -> None:
        for url in (
            "https://github.com/Example-Org/proj.git",
            "git@github.com:Example-Org/proj.git",
            "ssh://git@github.com/Example-Org/proj",
        ):
            self.assertEqual(run("github-owner", url)[:2], (0, "example-org\n"), url)

    def test_github_owner_refuses_another_forge(self) -> None:
        status, out, _ = run("github-owner", "https://gitlab.example.com/a/b.git")
        self.assertEqual((status, out), (1, ""))

    def test_member_field(self) -> None:
        path = self.write(
            "member.json",
            json.dumps({"room": "!r:x", "base_url": "http://192.0.2.10:6201"}),
        )
        self.assertEqual(
            run("member-field", path, "base_url")[:2], (0, "http://192.0.2.10:6201\n")
        )
        self.assertEqual(run("member-field", path, "missing")[0], 1)

    def test_room_path_quotes_every_reserved_character(self) -> None:
        self.assertEqual(
            run("room-path", "!abc:accept201a.agent-bus.internal")[1],
            "%21abc%3Aaccept201a.agent-bus.internal\n",
        )

    def test_login_body_takes_the_password_on_stdin(self) -> None:
        status, out, _ = run("login-body", "tester", stdin="s3cret\n")
        body = json.loads(out)
        self.assertEqual(status, 0)
        self.assertEqual(body["password"], "s3cret")
        self.assertEqual(body["identifier"], {"type": "m.id.user", "user": "tester"})

    def test_access_token(self) -> None:
        self.assertEqual(
            run("access-token", stdin='{"access_token": "tok"}')[:2], (0, "tok\n")
        )
        self.assertEqual(run("access-token", stdin="{}")[0], 1)


class TopicTest(TempDir):
    def state(self, topic: str | None) -> str:
        events = [{"type": "m.room.name", "state_key": "", "content": {"name": "x"}}]
        if topic is not None:
            events.append(
                {
                    "type": "m.room.topic",
                    "state_key": "",
                    "sender": "@admin:s",
                    "content": {"topic": topic},
                }
            )
        return self.write("state.json", json.dumps(events))

    def test_the_topic_is_the_description(self) -> None:
        self.assertEqual(run("expect-topic", self.state(DESC_A), DESC_A)[0], 0)

    def test_another_topic_or_none_fails(self) -> None:
        status, _, err = run("expect-topic", self.state("something else"), DESC_A)
        self.assertEqual(status, 1)
        self.assertIn("something else", err)
        self.assertEqual(run("expect-topic", self.state(None), DESC_A)[0], 1)


class StreamTest(TempDir):
    def test_user_line_is_one_stream_json_user_message(self) -> None:
        status, out, _ = run("user-line", "Reply READY")
        self.assertEqual(status, 0)
        self.assertTrue(out.endswith("\n") and out.count("\n") == 1)
        self.assertEqual(
            json.loads(out),
            {"type": "user", "message": {"role": "user", "content": "Reply READY"}},
        )

    def test_turns_and_session_id(self) -> None:
        stream = "\n".join(
            [
                "Quick Launch banner",
                json.dumps({"type": "system", "subtype": "init", "session_id": SID}),
                json.dumps({"type": "result", "subtype": "success"}),
            ]
        )
        path = self.write("session.out", stream)
        self.assertEqual(run("turns", path)[1], "1\n")
        self.assertEqual(run("session-id", path)[:2], (0, SID + "\n"))

    def test_no_init_line_yet_is_not_yet(self) -> None:
        path = self.write("session.out", "banner only\n")
        self.assertEqual(run("session-id", path)[:2], (3, ""))
        self.assertEqual(run("turns", path)[1], "0\n")


class SeatsTest(TempDir):
    LIST = (
        "\n".join(
            [
                f"SEAT\t{TEAM_A}\t1\theld\t-\tfedora-desktop.1+host-x.podman",
                f"SEAT\t{TEAM_B}\t1\tfree\t-\tfedora-desktop.1+host-x.podman",
                "SEAT\towner-team\t1\theld\t-\tfedora-desktop.1+host-x.podman",
            ]
        )
        + "\n"
    )

    def test_held_only_when_every_seat_is(self) -> None:
        path = self.write("seats", self.LIST)
        self.assertEqual(run("expect-held", path, f"1@{TEAM_A}")[0], 0)
        self.assertEqual(run("expect-held", path, f"1@{TEAM_A},1@{TEAM_B}")[0], 3)
        self.assertEqual(run("expect-held", path, f"2@{TEAM_A}")[0], 3)

    def test_seat_containers_names_only_these_teams(self) -> None:
        listing = f"c1\t1@{TEAM_A},1@{TEAM_B}\nc2\t1@owner-team\nc3\t2@{TEAM_B}\nc4\t\n"
        status, out, _ = run("seat-containers", f"{TEAM_A},{TEAM_B}", stdin=listing)
        self.assertEqual((status, out), (0, "c1\nc3\n"))


class ListRowTest(TempDir):
    def row(self, team: str, member: str, kind: str = "team") -> str:
        return f"{team:<24}  {member:<36}  {kind:<6}  {'up':<13}  {'up to date':<18}  {'-':<26}  {'-':<7}  -"

    def test_the_description_is_whole_on_the_team_row(self) -> None:
        text = (
            "\n".join(
                [
                    "TEAM  MEMBER ...",
                    "",
                    self.row(TEAM_A, DESC_A),
                    self.row(TEAM_B, DESC_B),
                    self.row(TEAM_B, "1", "ccy"),
                ]
            )
            + "\n"
        )
        path = self.write("list", text)
        self.assertEqual(run("expect-list-row", path, TEAM_A, DESC_A)[0], 0)
        self.assertEqual(run("expect-list-row", path, TEAM_B, DESC_B)[0], 0)

    def test_a_cut_or_missing_description_fails(self) -> None:
        cut = DESC_A[:35] + "…"
        path = self.write(
            "list",
            self.row(TEAM_A, cut) + "\n" + self.row(TEAM_B, "(no description)") + "\n",
        )
        self.assertEqual(run("expect-list-row", path, TEAM_A, DESC_A)[0], 1)
        self.assertEqual(run("expect-list-row", path, TEAM_B, DESC_B)[0], 1)


class BlockTest(TempDir):
    def stderr(self) -> str:
        return self.write(
            "session.err",
            "\n".join(
                [
                    "Quick Launch Available",
                    "This session is on 2 teams:",
                    f"  {TEAM_A}  seat 1 (worker)  {DESC_A}",
                    f"  {TEAM_B}  seat 1 (coordinator)  {DESC_B}",
                    "Container name: x",
                ]
            )
            + "\n",
        )

    def test_the_block_names_both_teams_seats_and_descriptions(self) -> None:
        status, _, err = run(
            "expect-block", self.stderr(), "1", TEAM_A, DESC_A, "1", TEAM_B, DESC_B
        )
        self.assertEqual(status, 0, err)

    def test_a_wrong_description_or_count_fails(self) -> None:
        self.assertEqual(
            run(
                "expect-block", self.stderr(), "1", TEAM_A, DESC_B, "1", TEAM_B, DESC_B
            )[0],
            1,
        )
        self.assertEqual(run("expect-block", self.stderr(), "1", TEAM_A, DESC_A)[0], 1)

    def test_arguments_come_in_threes(self) -> None:
        self.assertEqual(run("expect-block", self.stderr(), "1", TEAM_A)[0], 64)


class StatusTest(TempDir):
    def status(self, *, b_trusted: bool = True, b_about: bool = True) -> str:
        lines = [
            f"TEAM\t{TEAM_A}\tfedora-desktop.1+h.podman\ttrust=trusted\twake=watcher\tpending=0",
            f"ABOUT\t1\t{TEAM_A}\t{json.dumps(DESC_A)}",
            f"TEAM\t{TEAM_B}\tfedora-desktop.1+h.podman\ttrust={'trusted' if b_trusted else 'untrusted'}\twake=watcher",
        ]
        if b_about:
            lines.append(f"ABOUT\t1\t{TEAM_B}\t{json.dumps(DESC_B)}")
        return self.write("status", "\n".join(lines) + "\n")

    def test_ready_once_both_teams_are_trusted_and_described(self) -> None:
        self.assertEqual(run("status-ready", self.status(), TEAM_A, TEAM_B)[0], 0)
        self.assertEqual(
            run("status-ready", self.status(b_trusted=False), TEAM_A, TEAM_B)[0], 3
        )
        self.assertEqual(
            run("status-ready", self.status(b_about=False), TEAM_A, TEAM_B)[0], 3
        )

    def test_about_is_the_description(self) -> None:
        self.assertEqual(run("expect-about", self.status(), TEAM_B, DESC_B)[0], 0)
        self.assertEqual(run("expect-about", self.status(), TEAM_B, DESC_A)[0], 1)


class RoutingTest(TempDir):
    def test_sent_names_the_inferred_team(self) -> None:
        path = self.write("say.out", f"SENT\t1\t{TEAM_A}\t{EV}\n")
        self.assertEqual(run("expect-sent", path, TEAM_A)[:2], (0, EV + "\n"))
        self.assertEqual(run("expect-sent", path, TEAM_B)[0], 1)

    def test_the_refusal_lists_every_team_with_its_description(self) -> None:
        err = self.write(
            "say.err",
            "\n".join(
                [
                    "pingbus: several teams are active and --to/--re fit more than one (or none): name one with --team:",
                    f"  {TEAM_A}  {DESC_A}",
                    f"  {TEAM_B}  {DESC_B}",
                ]
            )
            + "\n",
        )
        self.assertEqual(
            run("expect-choices", err, TEAM_A, DESC_A, TEAM_B, DESC_B)[0], 0
        )
        self.assertEqual(
            run("expect-choices", err, TEAM_A, DESC_B, TEAM_B, DESC_B)[0], 1
        )

    def test_another_refusal_is_not_the_choices(self) -> None:
        err = self.write("say.err", "pingbus: refused: target\n")
        self.assertEqual(
            run("expect-choices", err, TEAM_A, DESC_A, TEAM_B, DESC_B)[0], 1
        )


class SegmentTest(TempDir):
    HANDLER = """
import types
class BusTeamsHandler:
    def handle(self, hook_input):
        sid = hook_input.get("session_id")
        teams = {"%s": "a,b"}.get(sid)
        return types.SimpleNamespace(context=[f"| \\033[36mbus: {teams}\\033[0m"] if teams else [])
"""

    def test_segment_calls_the_handler_with_the_session_id(self) -> None:
        handler = self.write("bus_teams.py", self.HANDLER % SID)
        status, out, err = run("segment", handler, SID)
        self.assertEqual(status, 0, err)
        self.assertEqual(out, "| \x1b[36mbus: a,b\x1b[0m\n")
        self.assertEqual(run("segment", handler, "other")[:2], (0, ""))

    def test_expect_segment(self) -> None:
        shown = self.write("seg", "| \x1b[36mbus: a,b\x1b[0m\n")
        empty = self.write("empty", "")
        self.assertEqual(run("expect-segment", shown, "a,b")[0], 0)
        self.assertEqual(run("expect-segment", shown, "b,a")[0], 1)
        self.assertEqual(run("expect-segment", empty, "-")[0], 0)
        self.assertEqual(run("expect-segment", shown, "-")[0], 1)
        self.assertEqual(run("expect-segment", empty, "a,b")[0], 1)


class PlainTest(TempDir):
    def test_a_plain_session_carries_no_seats(self) -> None:
        labels = self.write("labels", json.dumps({"ccy": "true", "ccy-project": "x"}))
        env = self.write("env", json.dumps(["PATH=/usr/bin", "HOME=/root"]))
        self.assertEqual(run("expect-plain", labels, env)[0], 0)

    def test_a_seat_label_or_bus_variable_fails(self) -> None:
        seated = self.write(
            "labels", json.dumps({"ccy": "true", "ccy-seats": f"1@{TEAM_A}"})
        )
        plain = self.write("plain", json.dumps({"ccy": "true"}))
        clean = self.write("env", json.dumps(["PATH=/usr/bin"]))
        bus = self.write("bus", json.dumps(["PINGBUS_SEATS=1@x"]))
        self.assertEqual(run("expect-plain", seated, clean)[0], 1)
        self.assertEqual(run("expect-plain", plain, bus)[0], 1)
        self.assertEqual(run("expect-plain", self.write("none", "{}"), clean)[0], 1)


class UsageTest(unittest.TestCase):
    def test_unknown_command_and_wrong_counts(self) -> None:
        self.assertEqual(run("nope")[0], 64)
        self.assertEqual(run("turns")[0], 64)
        self.assertEqual(run()[0], 64)


if __name__ == "__main__":
    unittest.main()
