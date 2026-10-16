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
from unittest import mock
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
    """M3: `pingbus status-line` ends the session's own status line with `bus: TEAMS`."""

    def test_expect_segment_wants_the_inner_line_then_the_teams(self) -> None:
        shown = self.write("seg", "model \x1b[2m|\x1b[0m dir | \x1b[36mbus: a,b\x1b[0m\n")
        alone = self.write("alone", "\x1b[36mbus: a,b\x1b[0m\n")
        broken = self.write("broken", "model | \x1b[31mbus: no record of this session's teams (pingbus status)\x1b[0m\n")
        self.assertEqual(run("expect-segment", shown, "a,b")[0], 0)
        self.assertEqual(run("expect-segment", shown, "b,a")[0], 1)
        self.assertEqual(run("expect-segment", alone, "a,b")[0], 1, "the session's own line is missing")
        self.assertEqual(run("expect-segment", broken, "a,b")[0], 1)

    def test_expect_session_settings(self) -> None:
        guard = '[ -e "$PINGBUS_HOME/active" ] || exec sh "$PINGBUS_HOME/statusline-inner"; exec pingbus status-line'
        seated = self.write("seated.json", json.dumps({
            "crossSessionInbound": "accept",
            "statusLine": {"type": "command", "command": guard, "refreshInterval": 1}}))
        kit = self.write("kit.json", json.dumps({
            "crossSessionInbound": "accept",
            "statusLine": {"type": "command", "command": "pingbus status-line"}}))
        plain = self.write("plain.json", json.dumps({"statusLine": {"type": "command", "command": guard}}))
        self.assertEqual(run("expect-session-settings", seated)[0], 0)
        self.assertEqual(run("expect-session-settings", kit)[0], 1, "not the shell guard")
        self.assertEqual(run("expect-session-settings", plain)[0], 1, "no inbound accept")


class SessionStatusLineTest(TempDir):
    """M3 runs the status line the session's claude was launched with, in claude's own
    environment, as Claude Code does: a `podman exec` carries the container's environment, not
    the one `pingbus seat exec` gave claude (PINGBUS_HOME), so running `pingbus status-line`
    from it renders a status line no session ever shows."""

    def proc(self, pid: str, argv: list[str], env: dict[str, str]) -> None:
        base = self.dir / "proc" / pid
        base.mkdir(parents=True)
        (base / "cmdline").write_bytes(b"\0".join(a.encode() for a in argv) + b"\0")
        (base / "environ").write_bytes(b"".join(f"{k}={v}\0".encode() for k, v in env.items()))
        (base / "cwd").symlink_to(self.dir)

    def settings(self, command: str) -> str:
        return self.write("settings.json", json.dumps({"statusLine": {"type": "command", "command": command}}))

    def render(self, payload: bytes = b'{"session_id": "s1"}') -> str:
        return mc.session_status_line(self.dir / "proc", payload)

    def test_runs_the_launch_settings_command_in_claudes_environment(self) -> None:
        path = self.settings('printf "%s|" "$PINGBUS_HOME"; cat')
        self.proc("1", ["catatonit", "--", "entrypoint"], {"PINGBUS_HOME": "/wrong"})
        self.proc("2", ["claude", "--dangerously-skip-permissions", "--settings", path, "-p", "x"],
                  {"PINGBUS_HOME": "/tmp/pingbus-home", "PATH": "/usr/bin:/bin"})
        self.proc("9", ["python3", "sup.py", "--", "claude", "--settings", "/elsewhere"], {})
        self.assertEqual(self.render(b'{"session_id": "s1"}'), '/tmp/pingbus-home|{"session_id": "s1"}')

    def test_no_claude_or_two_fail(self) -> None:
        path = self.settings("echo x")
        self.proc("1", ["bash"], {})
        with self.assertRaises(mc.Failed):
            self.render()
        self.proc("2", ["claude", "--settings", path], {"PATH": "/usr/bin:/bin"})
        self.proc("3", ["/usr/local/bin/claude", "--settings", path], {"PATH": "/usr/bin:/bin"})
        with self.assertRaises(mc.Failed):
            self.render()

    def test_a_failing_command_or_no_status_line_fails(self) -> None:
        path = self.settings("echo half; exit 4")
        self.proc("2", ["claude", "--settings", path], {"PATH": "/usr/bin:/bin"})
        with self.assertRaises(mc.Failed):
            self.render()
        pathlib.Path(path).write_text("{}", encoding="utf-8")
        with self.assertRaises(mc.Failed):
            self.render()

    def test_command_line(self) -> None:
        path = self.settings("cat")
        self.proc("2", ["claude", "--settings", path], {"PATH": "/usr/bin:/bin"})
        status, out, _ = run("session-status-line", str(self.dir / "proc"), stdin='{"a": 1}')
        self.assertEqual((status, out), (0, '{"a": 1}\n'))


class PlainTest(TempDir):
    def test_a_plain_session_carries_no_seats(self) -> None:
        labels = self.write("labels", json.dumps({"ccy": "true", "ccy-project": "x"}))
        env = self.write("env", json.dumps(["PATH=/usr/bin", "HOME=/root"]))
        self.assertEqual(run("expect-plain", labels, env)[0], 0)
        # Plan 00203 M2: a launch outside ccy's tmux with no --teams states PINGBUS_MEMBERSHIP=off.
        off = self.write("off", json.dumps(["PATH=/usr/bin", "PINGBUS_MEMBERSHIP=off"]))
        self.assertEqual(run("expect-plain", labels, off)[0], 0)

    def test_a_seat_label_or_bus_variable_fails(self) -> None:
        seated = self.write(
            "labels", json.dumps({"ccy": "true", "ccy-seats": f"1@{TEAM_A}"})
        )
        plain = self.write("plain", json.dumps({"ccy": "true"}))
        clean = self.write("env", json.dumps(["PATH=/usr/bin"]))
        bus = self.write("bus", json.dumps(["PINGBUS_SEATS=1@x"]))
        kit = self.write("kit", json.dumps(["PINGBUS_MEMBERSHIP=/run/ccy/membership"]))
        self.assertEqual(run("expect-plain", seated, clean)[0], 1)
        self.assertEqual(run("expect-plain", plain, bus)[0], 1)
        self.assertEqual(run("expect-plain", plain, kit)[0], 1)
        self.assertEqual(run("expect-plain", self.write("none", "{}"), clean)[0], 1)


class UsageTest(unittest.TestCase):
    def test_unknown_command_and_wrong_counts(self) -> None:
        self.assertEqual(run("nope")[0], 64)
        self.assertEqual(run("turns")[0], 64)
        self.assertEqual(run()[0], 64)


class ReviewFixesTest(TempDir):
    """Fable's M7 review, F1 to F8."""

    def conf(self, teams: str) -> str:
        return self.write(
            "last-launch.conf",
            "# CCY Launch Configuration\nSAVED_CONFIG_VERSION=3\n"
            f'LAST_TOKEN="tok-name"\nLAST_TEAMS="{teams}"\nLAST_LAUNCH_DATE="2026-10-15"\n',
        )

    def test_f1_a_conf_naming_an_acceptance_team_is_polluted(self) -> None:
        teams = f"{TEAM_A},{TEAM_B}"
        self.assertEqual(
            run("launch-conf-clean", self.conf("1@owner-team"), teams)[0], 0
        )
        self.assertEqual(run("launch-conf-clean", self.conf(""), teams)[0], 0)
        status, _, err = run(
            "launch-conf-clean", self.conf(f"1@{TEAM_A},1@{TEAM_B}"), teams
        )
        self.assertEqual(status, 1)
        self.assertIn(TEAM_A, err)
        self.assertEqual(
            run("launch-conf-clean", self.conf(f"2@owner,1@{TEAM_B}"), teams)[0], 1
        )

    def test_f1_a_conf_with_no_last_teams_line_is_clean(self) -> None:
        path = self.write("conf", 'LAST_TOKEN="x"\n')
        self.assertEqual(run("launch-conf-clean", path, TEAM_A)[0], 0)

    def test_f2_an_installed_team_is_ours_only_by_its_description(self) -> None:
        ours = self.write(
            "ours.json", json.dumps({"team": TEAM_A, "description": DESC_A})
        )
        other = self.write(
            "other.json", json.dumps({"team": TEAM_A, "description": "Real work"})
        )
        bare = self.write("bare.json", json.dumps({"team": TEAM_A}))
        prefix = "Plan 00201 acceptance"
        self.assertEqual(run("expect-own-team", ours, TEAM_A, prefix)[0], 0)
        self.assertEqual(run("expect-own-team", other, TEAM_A, prefix)[0], 1)
        self.assertEqual(run("expect-own-team", bare, TEAM_A, prefix)[0], 1)
        self.assertEqual(run("expect-own-team", ours, TEAM_B, prefix)[0], 1)

    def test_f4_ports_come_from_the_bus_range_avoiding_declared_and_listening(
        self,
    ) -> None:
        first = declare.AUTO_PORT_FIRST
        ports = mc.bus_ports(2, {first}, {first + 1}, declare.choose_port)
        self.assertEqual(ports, [first + 2, first + 3])

    def test_f4_bus_ports_reads_the_owners_host_vars(self) -> None:
        first = declare.AUTO_PORT_FIRST
        hv = self.write(
            "hv.yml", f"agent_bus_teams:\n  - team: owner\n    port: {first}\n"
        )
        listening = f"LISTEN 0 4096 127.0.0.1:{first + 1} 0.0.0.0:*\n"
        with mock.patch.object(declare, "_ss", return_value=listening):
            status, out, err = run("bus-ports", "2", hv)
        self.assertEqual(status, 0, err)
        self.assertEqual([int(p) for p in out.split()], [first + 2, first + 3])

    def test_f4_an_absent_owner_file_declares_nothing(self) -> None:
        with mock.patch.object(declare, "_ss", return_value=""):
            status, out, err = run("bus-ports", "1", str(self.dir / "absent.yml"))
        self.assertEqual(status, 0, err)
        self.assertEqual(out, f"{declare.AUTO_PORT_FIRST}\n")

    def test_f4_ports_unknown_when_ss_fails(self) -> None:
        failing = mock.Mock(side_effect=declare.DeclareError("ss failed"))
        with mock.patch.object(declare, "_ss", failing):
            status, out, _ = run("bus-ports", "1", str(self.dir / "absent.yml"))
        self.assertEqual((status, out), (1, ""))

    def test_f8_a_role_with_a_digit_or_hyphen_still_matches(self) -> None:
        err = self.write(
            "e",
            f"This session is on 1 team:\n  {TEAM_A}  seat 1 (lead-2 worker)  {DESC_A}\n",
        )
        self.assertEqual(run("expect-block", err, "1", TEAM_A, DESC_A)[0], 0)

    def test_f7_say_waiting_on_an_unverified_record_is_not_yet(self) -> None:
        unverified = self.write(
            "u.err",
            f"pingbus: {mc.CHOICES_TEXT}\n  {TEAM_A}  (no verified team record yet: run pingbus recv "
            "or name it with --team)\n",
        )
        other = self.write("o.err", "pingbus: refused: target\n")
        self.assertEqual(run("say-outcome", "64", unverified)[0], 3)
        self.assertEqual(run("say-outcome", "64", other)[0], 1)
        self.assertEqual(run("say-outcome", "0", other)[0], 0)


if __name__ == "__main__":
    unittest.main()
