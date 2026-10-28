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
import subprocess
import sys
import tempfile
import types
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
#: A team's colour span on the status line (a 24-bit foreground escape, then the reset).
COLOUR, RESET = "\x1b[38;2;232;121;249m", "\x1b[0m"


def spans(*pieces: str) -> str:
    """Team pieces as the drawer joins them: each its own colour span, plain spaces between."""
    return " ".join(f"{COLOUR}{piece}{RESET}" for piece in pieces)


def drawer(letters: dict[str, str], draw: object) -> types.SimpleNamespace:
    """A stand-in statusline.py: its ROLE_LETTERS and a `_team_piece` drawing (team, member,
    role) by `draw`."""
    return types.SimpleNamespace(
        ROLE_LETTERS=letters, _team_piece=lambda _env, team, found, _unknown: draw(team, *found)
    )


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

    def rendered(self, path: str, cidr: str | None) -> dict:
        bridge = mc._checkout_module("helpers.agent_bus.bridge")
        with mock.patch.object(bridge, "live_subnet", return_value=cidr):
            status, out, err = run("rendered-team", path)
        self.assertEqual(status, 0, err)
        return json.loads(out)

    def test_the_installed_team_is_the_team_the_play_renders_bridge_subnet_included(self) -> None:
        # The play adds the LXC bridge's subnet to every team's allow_from before it installs
        # it; a fixture team installed without it is "out of date: config" and its first play
        # changes the unit drop-in, restarting the homeserver (host run at fe894347c).
        path = self.team_file(TEAM_A, "6201", DESC_A)
        status, hv_text, err = run("host-vars", path)
        self.assertEqual(status, 0, err)
        declared = declare.parse(hv_text).teams[0]
        cidr = "198.51.100.0/24"
        rendered = self.rendered(path, cidr)
        self.assertEqual(rendered["allow_from"], [mc.ALLOW_FROM_EXAMPLE, cidr])
        self.assertEqual(
            teamfile.config_digest(teamfile.parse_team_file(rendered)),
            declare.declared_config(declared, cidr),
        )

    def test_with_no_bridge_the_rendered_team_admits_only_its_declared_list(self) -> None:
        path = self.team_file(TEAM_A, "6201", DESC_A)
        self.assertEqual(self.rendered(path, None)["allow_from"], [mc.ALLOW_FROM_EXAMPLE])


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
    HEADING = f"{'TEAM':<24}  {'MEMBER':<80}  {'KIND':<6}  {'STATE':<13}  APPLIED"

    def row(self, team: str, member: str, kind: str = "team") -> str:
        return f"{team:<24}  {member:<80}  {kind:<6}  {'up':<13}  {'up to date':<18}  {'-':<26}  {'-':<7}  -"

    def test_the_description_is_whole_on_the_team_row(self) -> None:
        text = (
            "\n".join(
                [
                    self.HEADING,
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
            self.HEADING + "\n\n" + self.row(TEAM_A, cut) + "\n" + self.row(TEAM_B, "(no description)") + "\n",
        )
        self.assertEqual(run("expect-list-row", path, TEAM_A, DESC_A)[0], 1)
        self.assertEqual(run("expect-list-row", path, TEAM_B, DESC_B)[0], 1)

    #: `ccy-teams --list` piped, as src-go/internal/teams/board's Layout(rows, 0) renders these
    #: rows (recorded from the Go table code): each column as wide as its longest value, so
    #: TEAM is 10 wide here, not 24.
    MEASURED = [
        "TEAM        MEMBER                                                                     KIND  STATE  APPLIED     SESSION  RESTORE  WHERE",
        "",
        "accept201a  Plan 00201 acceptance team A: a throwaway team, removed when the run ends  team  up     up to date  -        -        -",
        "accept201b  Plan 00201 acceptance team B                                               team  up     up to date  -        -        -",
        "accept201b  1                                                                          ccy   free   -           -        -        /home/u/proj",
    ]

    def test_the_go_tables_measured_columns_pass(self) -> None:
        path = self.write("list", "\n".join(self.MEASURED) + "\n")
        self.assertEqual(run("expect-list-row", path, TEAM_A, DESC_A)[0], 0)
        self.assertEqual(run("expect-list-row", path, TEAM_B, DESC_B)[0], 0)

    def test_a_row_off_the_headings_columns_fails(self) -> None:
        """One space more or less before MEMBER or KIND is misaligned output, not a pass."""
        shifted = list(self.MEASURED)
        shifted[2] = shifted[2].replace("accept201a  Plan", "accept201a   Plan").replace("ends  team", "ends team")
        shifted[3] = shifted[3].replace("B   ", "B    ", 1)
        path = self.write("list", "\n".join(shifted) + "\n")
        self.assertEqual(run("expect-list-row", path, TEAM_A, DESC_A)[0], 1)
        self.assertEqual(run("expect-list-row", path, TEAM_B, DESC_B)[0], 1)

    def test_no_heading_fails(self) -> None:
        path = self.write("list", "\n".join(self.MEASURED[2:]) + "\n")
        self.assertEqual(run("expect-list-row", path, TEAM_A, DESC_A)[0], 1)

    def test_the_headings_and_team_kind_are_read_from_the_go_table(self) -> None:
        self.assertEqual(mc.board_table(), (("TEAM", "MEMBER", "KIND", "STATE"), "team"))
        moved = 'var headings = [columns]string{"T", "WHO", "K", "S", "A"}\n\tKindTeam  Kind = "group"\n'
        self.assertEqual(mc.board_table(moved), (("T", "WHO", "K", "S"), "group"))
        width = len(DESC_A)
        heading = f"{'T':<10}  {'WHO':<{width}}  {'K':<5}  {'S':<4}  A"
        row = f"{TEAM_A:<10}  {DESC_A:<{width}}  {'group':<5}  {'up':<4}  -"
        mc.expect_list_row(f"{heading}\n\n{row}\n", TEAM_A, DESC_A, mc.board_table(moved))
        with self.assertRaises(mc.Failed):
            mc.expect_list_row(f"{heading}\n\n{row}\n", TEAM_A, DESC_A)
        for broken in (
            'KindTeam Kind = "team"\n',
            'var headings = [columns]string{"A", "B", "C"}\nKindTeam Kind = "team"\n',
            'var headings = [columns]string{"A", "B", "C", "D"}\n'
            'var headings = [columns]string{"A", "B", "C", "D"}\nKindTeam Kind = "team"\n',
            'var headings = [columns]string{"A", "B", "C", "D"}\n',
        ):
            with self.subTest(broken=broken), self.assertRaises(mc.Failed):
                mc.board_table(broken)


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

    def test_the_head_and_seat_line_are_read_from_choose_go(self) -> None:
        """The block's head, nouns and seat line are choose.go's named texts: a changed one is
        followed, and a source that names none fails."""
        choose = ('const (\n\tblockHead     = "On %d %s:"\n\tblockNounOne  = "team"\n\tblockNounMany = "teams"\n'
                  '\tblockLine     = "- %s seat %s [%s] %s"\n)\n')
        stderr = f"On 2 teams:\n- {TEAM_A} seat 1 [worker] {DESC_A}\n- {TEAM_B} seat 1 [coordinator] {DESC_B}\n"
        seats = [("1", TEAM_A, DESC_A), ("1", TEAM_B, DESC_B)]
        mc.expect_block(stderr, seats, choose)
        with self.assertRaises(mc.Failed):
            mc.expect_block(stderr.replace("On 2", "This session is on 2"), seats, choose)
        with self.assertRaises(mc.Failed):
            mc.expect_block(stderr, seats, choose.replace("blockLine", "line"))
        mc.expect_block(pathlib.Path(self.stderr()).read_text(), seats)


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
    """M3: `pingbus status-line` prints the session's own status line, then `👥 TEAMS` on rows of
    its own (wrapped after a comma at COLUMNS, later rows indented under the first), each team
    `team: member (letter)` in its own colour with this session's role in it (Plan 00203 Task
    3.8, Plan 00217 C10 and C15); a role or a member that could not be read fails."""

    def test_expect_segment_wants_the_inner_line_then_the_teams(self) -> None:
        shown = self.write("seg", f"model \x1b[2m|\x1b[0m dir\n👥 {spans('a: repo.1 (w),', 'b: repo.1 (c)')}\n")
        wrapped = self.write("wrapped", f"model | dir\nmore\n👥 {spans('a: repo.1 (w),')}\n"
                                        f"   {spans('b: repo.1 (w) (down)')}\n")
        refused = {
            "alone": (f"👥 {spans('a: repo.1 (w),', 'b: repo.1 (w)')}\n", "the session's own line is missing"),
            "old": ("model | dir | \x1b[36mbus: a,b\x1b[0m\n", "the bus: segment"),
            "pre-00217": ("model\n\x1b[36m👥 a (worker), b (coordinator)\x1b[0m\n", "the `team (role)` form"),
            "no-role": (f"model | dir\n👥 {spans('a: repo.1,', 'b: repo.1')}\n", "each team shows its role"),
            "role-unknown": (f"model\n👥 {spans('a: repo.1 (w),', 'b: repo.1 (role?)')}\n", "a role not read"),
            "member-unknown": (f"model\n👥 {spans('a: member? (w),', 'b: repo.1 (c)')}\n", "a member not read"),
            "colour-unknown": (f"model\n👥 {spans('a: repo.1 (w) (colour?),', 'b: repo.1 (c)')}\n", "colour?"),
            "uncoloured": ("model\n👥 a: repo.1 (w), b: repo.1 (c)\n", "each team in its own colour"),
            "one-span": (f"model\n👥 {COLOUR}a: repo.1 (w), b: repo.1 (c){RESET}\n", "one span for two teams"),
            "appended": (f"model | dir | 👥 {spans('a: repo.1 (w),', 'b: repo.1 (w)')}\n", "must start a row"),
            "unindented": (f"model\n👥 {spans('a: repo.1 (w),')}\n{spans('b: repo.1 (w)')}\n", "not indented"),
            "broken": ("model\n\x1b[31m👥 no record of this session's teams (pingbus status)\x1b[0m\n", "a note"),
        }
        self.assertEqual(run("expect-segment", shown, "a,b")[0], 0)
        self.assertEqual(run("expect-segment", wrapped, "a,b")[0], 0)
        self.assertEqual(run("expect-segment", shown, "b,a")[0], 1)
        for name, (text, why) in refused.items():
            with self.subTest(name):
                self.assertEqual(run("expect-segment", self.write(name, text), "a,b")[0], 1, why)

    def test_expect_segment_rows_wants_the_teams_alone(self) -> None:
        """Plan 00203 M3 draws the segment alone (the inner-only variable set): the same rows,
        with no line before them."""
        alone = self.write("alone", f"👥 {spans('a: repo.1 (w),', 'b: repo.1 (c)')}\n")
        wrapped = self.write("wrapped", f"👥 {spans('a: repo.1 (w),')}\n   {spans('b: repo.1 (w) (down)')}\n")
        refused = {
            "inner": (f"model | dir\n👥 {spans('a: repo.1 (w),', 'b: repo.1 (c)')}\n", "a line before the rows"),
            "pre-00217": ("\x1b[36m👥 a (worker), b (coordinator)\x1b[0m\n", "the `team (role)` form"),
            "role-unknown": (f"👥 {spans('a: repo.1 (w),', 'b: repo.1 (role?)')}\n", "a role not read"),
            "uncoloured": ("👥 a: repo.1 (w), b: repo.1 (c)\n", "each team in its own colour"),
            "empty": ("", "no rows"),
        }
        self.assertEqual(run("expect-segment-rows", alone, "a,b")[0], 0)
        self.assertEqual(run("expect-segment-rows", wrapped, "a,b")[0], 0)
        self.assertEqual(run("expect-segment-rows", alone, "a")[0], 1)
        for name, (text, why) in refused.items():
            with self.subTest(name):
                self.assertEqual(run("expect-segment-rows", self.write(name, text), "a,b")[0], 1, why)
        golden = (_repo_root() / "src-go/internal/pingbus/statusline/golden/testdata/golden"
                  / "bus-two-teams-with-roles/1.stdout").read_text(encoding="utf-8")
        mc.expect_segment(golden, "alpha,beta", inner=False)

    def test_the_drawers_golden_two_teams_pass(self) -> None:
        """The golden the Go drawer and statusline.py are both held to: two teams with roles."""
        golden = (_repo_root() / "src-go/internal/pingbus/statusline/golden/testdata/golden"
                  / "bus-two-teams-with-roles/1.stdout").read_text(encoding="utf-8")
        mc.expect_segment(f"model | dir\n{golden}", "alpha,beta")
        with self.assertRaises(mc.Failed):
            mc.expect_segment(golden, "alpha,beta")

    def test_the_piece_is_drawn_by_statusline_py(self) -> None:
        """The piece's separators and letters are statusline.py's `_team_piece` and ROLE_LETTERS,
        never a copy: a drawer that changes them is followed; one whose roles differ in shape, or
        that leaves out a part, fails."""
        statusline = importlib.import_module("helpers.pingbus.statusline")
        self.assertEqual(mc.segment_piece(statusline).replace(mc._PIECE_TEAM, "t")
                         .replace(mc._PIECE_MEMBER, "m").replace(mc._PIECE_LETTER, "x"), "t: m (x)")
        letters = {"coordinator": "K", "worker": "W"}
        other = mc.segment_piece_re(drawer(letters, lambda team, member, role: f"{team} @ {member} [{letters[role]}]"))
        self.assertIsNotNone(other.fullmatch("abc @ repo.1 [K] (down)"))
        self.assertIsNone(other.fullmatch("abc: repo.1 (c)"))
        self.assertIsNone(other.fullmatch("abc @ repo.1 [role?]"))
        shapes = drawer({"coordinator": "c", "worker": "w"},
                        lambda team, member, role: f"{team}{':' if role == 'worker' else ''} {member} ({role[0]})")
        with self.assertRaises(mc.Failed):
            mc.segment_piece(shapes)
        with self.assertRaises(mc.Failed):
            mc.segment_piece(drawer({"coordinator": "c"}, lambda team, _member, _role: f"{team} (c)"))

    def test_the_session_paths_are_seat_and_membership_s(self) -> None:
        seat = mc._checkout_module("helpers.pingbus.seat")
        membership = mc._checkout_module("helpers.pingbus.membership")
        home = str(seat.SESSION_HOME)
        self.assertEqual(
            mc.session_paths(),
            [home, f"{home}/{seat.SESSION_SETTINGS}", membership.CCY_MOUNT, membership.DESIRED_FILE],
        )
        status, out, _ = run("session-paths")
        self.assertEqual((status, out.splitlines()), (0, mc.session_paths()))

    def test_the_session_names_are_seat_s(self) -> None:
        seat = mc._checkout_module("helpers.pingbus.seat")
        self.assertEqual(mc.session_names(), [seat.ACTIVE_FLAG, seat.STATUS_LINE_INNER, seat.WATCH_LOG])
        status, out, _ = run("session-names")
        self.assertEqual((status, out.splitlines()), (0, mc.session_names()))

    def test_the_membership_dirs_are_read_from_the_go_package(self) -> None:
        """This checkout's, and a changed one followed; a missing or doubled one fails."""
        dirs = mc.membership_dirs()
        self.assertEqual(len(dirs), 3)
        membership_go = (mc._checkout_root() / mc.MEMBERSHIP_GO).read_text(encoding="utf-8")
        self.assertIn(f'ccyRoot = "{dirs[0]}"', membership_go)
        status, out, _ = run("membership-dirs")
        self.assertEqual((status, out.splitlines()), (0, dirs))
        source = 'const (\n\tccyRoot = "a/b"\n\tccRoot  = "c/d"\n)\n'
        self.assertEqual(mc.membership_dirs(source, 'const RelaunchFile = "r.json"\n'), ["a/b", "c/d", "r.json"])
        for bad_dirs, bad_relaunch in ((source.replace("ccRoot", "other"), 'const RelaunchFile = "r"\n'),
                                       (source, 'const Relaunch = "r"\n')):
            with self.assertRaises(mc.Failed):
                mc.membership_dirs(bad_dirs, bad_relaunch)

    def test_the_say_refusal_texts_are_the_products(self) -> None:
        """F7 and the choices read pingbus's own words: cli.py CHOICES_HEAD, inbox.py NO_RECORD_YET."""
        cli = mc._checkout_module("helpers.pingbus.cli")
        inbox = mc._checkout_module("helpers.pingbus.inbox")
        self.assertEqual(mc.choices_head(), cli.CHOICES_HEAD)
        self.assertEqual(mc.no_record_yet(), inbox.NO_RECORD_YET)

    def test_a_product_value_is_read_by_name(self) -> None:
        """Any named string of this checkout's helpers/ or Go package, its placeholders filled;
        a missing, doubled, non-text or badly filled one fails."""
        membership = mc._checkout_module("helpers.pingbus.membership")
        seat = mc._checkout_module("helpers.pingbus.seat")
        self.assertEqual(mc.product_value("helpers/pingbus/membership.py", "APPLIED_FILE"), membership.APPLIED_FILE)
        self.assertEqual(mc.product_value("helpers/pingbus/seat.py", "CHECKOUT_SEATS"), str(seat.CHECKOUT_SEATS))
        self.assertEqual(mc.product_value("helpers/pingbus/statusline.py", "SESSIONS_DIR_TEMPLATE", ["uid=7"]),
                         mc._checkout_module("helpers.pingbus.statusline").SESSIONS_DIR_TEMPLATE.format(uid=7))
        self.assertEqual(mc.product_value("helpers/pingbus/hooks.py", "GOODBYE_REASONS[prompt_input_exit]"),
                         mc._checkout_module("helpers.pingbus.hooks").GOODBYE_REASONS["prompt_input_exit"])
        status, out, _ = run("product-value", "helpers/pingbus/membership.py", "APPLIED_FILE")
        self.assertEqual((status, out), (0, membership.APPLIED_FILE + "\n"))
        go = 'package x\nconst One = "a %s b %d"\nconst (\n\ttwo   = "t"\n\tthree string = "c" // why\n)\n'
        self.assertEqual(mc.product_value("src-go/x.go", "One", ["p", "9"], go), "a p b 9")
        self.assertEqual(mc.product_value("src-go/x.go", "two", source=go), "t")
        self.assertEqual(mc.product_value("src-go/x.go", "three", source=go), "c")
        unguarded = mc.product_value("src-go/internal/teams/hot/cc.go", "ccUnguarded")
        self.assertIn(f'ccUnguarded = "{unguarded}"', (mc._checkout_root() / "src-go/internal/teams/hot/cc.go").read_text())
        for rel, name, fills, source in (("src-go/x.go", "four", [], go), ("src-go/x.go", "One", ["p"], go),
                                         ("src-go/x.go", "One", [], 'const One = "a"\nconst One = "b"\n'),
                                         ("helpers/pingbus/membership.py", "NOPE", [], None),
                                         ("helpers/pingbus/hooks.py", "GOODBYE_REASONS", [], None),
                                         ("helpers/pingbus/statusline.py", "SESSIONS_DIR_TEMPLATE", ["x=1"], None),
                                         ("helpers/pingbus/statusline.py", "SESSIONS_DIR_TEMPLATE[uid]", [], None),
                                         ("helpers/pingbus/hooks.py", "GOODBYE_REASONS[nope]", [], None),
                                         ("docs/x.md", "A", [], None)):
            with self.subTest(rel=rel, name=name), self.assertRaises(mc.Failed):
                mc.product_value(rel, name, fills, source)

    def test_a_product_function_names_the_text_of_its_arguments(self) -> None:
        """A helpers/ function that names a text (checkout.py's hs_unit, health.py's timers) is
        called with the fills in order, as product-value and product-pattern; a wrong count of
        arguments, a class or a function that names no text fails."""
        checkout = mc._checkout_module("helpers.agent_bus.checkout")
        health = mc._checkout_module("helpers.agent_bus.health")
        self.assertEqual(mc.product_value("helpers/agent_bus/checkout.py", "hs_unit", ["t1"]), checkout.hs_unit("t1"))
        self.assertEqual(mc.product_value("helpers/agent_bus/health.py", "backup_timer", ["t1"]), health.backup_timer("t1"))
        status, out, _ = run("product-value", "helpers/agent_bus/health.py", "health_service", "t2")
        self.assertEqual((status, out), (0, health.health_service("t2") + "\n"))
        pattern = mc.product_pattern("helpers/agent_bus/checkout.py", "hs_unit", ["[a-z0-9-]+"])
        for text, matches in ((checkout.hs_unit("team-a"), 0), (checkout.hs_unit("team-a") + "x", 1),
                              (health.health_service("team-a"), 1)):
            grep = subprocess.run(["grep", "-qxP", pattern], input=text, text=True, check=False)
            self.assertEqual(grep.returncode, matches, text)
        for name, fills in (("hs_unit", []), ("hs_unit", ["a", "b"]), ("Choice", ["a"]), ("handle_type", ["?"])):
            with self.subTest(name=name, fills=fills), self.assertRaises(mc.Failed):
                mc.product_value("helpers/agent_bus/checkout.py", name, fills)

    def test_a_product_pattern_quotes_the_text_and_fills_each_placeholder_with_a_pattern(self) -> None:
        """product-pattern: the named text as a Perl pattern (grep -P), its fixed parts quoted and
        each placeholder the pattern given; a wrong count of patterns fails as product-value does."""
        go = 'package x\nconst One = "a.b %s (c) %d"\nconst two = "x+y"\n'
        pattern = mc.product_pattern("src-go/x.go", "One", ["[a-z]+", "[0-9]+"], go)
        self.assertEqual(pattern, r"\Qa.b \E[a-z]+\Q (c) \E[0-9]+")
        for text, matches in (("a.b qq (c) 12", 0), ("axb qq (c) 12", 1), ("a.b qq c 12", 1)):
            grep = subprocess.run(["grep", "-qxP", pattern], input=text, text=True, check=False)
            self.assertEqual(grep.returncode, matches, text)
        self.assertEqual(mc.product_pattern("src-go/x.go", "two", source=go), r"\Qx+y\E")
        status, out, _ = run("product-pattern", "src-go/internal/teams/hot/texts.go", "stepLine", "a", "b")
        self.assertEqual((status, out), (0, "a\\Q: \\Eb\n"))
        status, out, _ = run("product-pattern", "helpers/pingbus/statusline.py", "SESSIONS_DIR_TEMPLATE", "uid=[0-9]+")
        self.assertEqual(status, 0)
        self.assertTrue(out.endswith(r"\E[0-9]+" + "\n"), out)
        for fills, source in ((["p"], go), (["p", "q", "r"], go), ([], 'const One = "a \\E b"\n')):
            with self.subTest(fills=fills), self.assertRaises(mc.Failed):
                mc.product_pattern("src-go/x.go", "One", fills, source)

    def test_the_label_is_read_from_the_go_drawer(self) -> None:
        """The label is the Go statusline's `label` constant and the indent its display width:
        this checkout's, and a changed one followed rather than failed."""
        self.assertEqual(mc.segment_label(), ("👥 ", "   "))
        self.assertEqual(mc.segment_label('const (\n\tlabel       = "T"\n\tother = "x"\n)\n'), ("T ", "  "))
        mc.expect_segment(f"model\nT {spans('a: repo.1 (w),')}\n  {spans('b: repo.1 (c)')}\n", "a,b",
                          label=("T ", "  "))
        with self.assertRaises(mc.Failed):
            mc.segment_label('const (\n\tnolabel = "T"\n)\n')
        with self.assertRaises(mc.Failed):
            mc.segment_label('label = "A"\nlabel = "B"\n')

    def test_expect_session_settings(self) -> None:
        """What a seated session's settings must hold is what this checkout's seat.py writes,
        never a copy: the copy said `exec pingbus status-line` after Plan 00213 made the kit
        run `pingbus-status-line`, and failed M3 on a correct session."""
        seat = importlib.import_module("helpers.pingbus.seat")
        written = seat.session_settings(None, seated=True)
        seated = self.write("seated.json", json.dumps(written))
        padded = dict(written, statusLine=dict(written["statusLine"], padding=1))
        python_era = dict(written, statusLine=dict(
            written["statusLine"], command=written["statusLine"]["command"].replace(
                "exec pingbus-status-line", "exec pingbus status-line")))
        kit = dict(written, statusLine={"type": "command", "command": "pingbus-status-line"})
        slow = dict(written, statusLine=dict(written["statusLine"], refreshInterval=1))
        plain = dict(written, crossSessionInbound="refuse")
        self.assertEqual(run("expect-session-settings", seated)[0], 0)
        self.assertEqual(run("expect-session-settings", self.write("padded.json", json.dumps(padded)))[0], 0,
                         "Claude Code's own padding may be added")
        self.assertEqual(run("expect-session-settings", self.write("old.json", json.dumps(python_era)))[0], 1,
                         "the guard of the Python status line is not what the kit runs now")
        self.assertEqual(run("expect-session-settings", self.write("kit.json", json.dumps(kit)))[0], 1,
                         "not the shell guard")
        self.assertEqual(run("expect-session-settings", self.write("slow.json", json.dumps(slow)))[0], 1,
                         "not the seated refresh interval")
        self.assertEqual(run("expect-session-settings", self.write("plain.json", json.dumps(plain)))[0], 1,
                         "no inbound accept")

    def test_the_seated_guard_runs_the_kit_command(self) -> None:
        """The checkout's seat.py guard and its kit settings agree, and the check says so when
        they do not."""
        kit = json.loads((_repo_root() / mc.KIT_SETTINGS).read_text(encoding="utf-8"))
        self.assertEqual(mc.kit_command(), kit["statusLine"]["command"])
        self.assertTrue(mc.seated_status_line()["command"].endswith("exec " + mc.kit_command()))
        settings = {"crossSessionInbound": "accept", "statusLine": mc.seated_status_line()}
        with self.assertRaises(mc.Failed):
            mc.expect_session_settings(settings, kit="pingbus status-line")


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
            f"pingbus: {mc.choices_head()}\n  {TEAM_A}  (no verified team record yet: run pingbus recv "
            "or name it with --team)\n",
        )
        other = self.write("o.err", "pingbus: refused: target\n")
        self.assertEqual(run("say-outcome", "64", unverified)[0], 3)
        self.assertEqual(run("say-outcome", "64", other)[0], 1)
        self.assertEqual(run("say-outcome", "0", other)[0], 0)


#: Sources CLAUDE/Plan/_image-build.inc.bash and _acceptance-m7.inc.bash, as acceptance.bash
#: does, and runs one wait ($3: grace, timeout or launch). $4 is the build: live (the launcher's
#: own ccy_image_build_locked, lib/image-build-lock.bash, building for 5 s), record (a record
#: that looks live, but nothing holds its lock) or none. $5: the session ends (exit 1) after
#: 1 s, or lives. The check passes once the build has ended.
WAIT_DRIVER = r"""
set -u
here="$1" tmp="$2" mode="$3" kind="$4" session="$5" repo="$6"
PLAN_RUN_DIR="${tmp}/run" PLAN_REPO_ROOT="${tmp}" PLAN_SCRIPT_DIR="${here}" TEAM_A=a TEAM_B=b
REPORT="${tmp}/report.md"
export XDG_RUNTIME_DIR="${tmp}/rt"
mkdir -p -- "${PLAN_RUN_DIR}/s" "${XDG_RUNTIME_DIR}/ccy/image-builds"
: >"${PLAN_RUN_DIR}/s/session.err"
source "${repo}/CLAUDE/Plan/_image-build.inc.bash"
source "${here}/_acceptance-m7.inc.bash"
m7_sudo_alive() { :; }
lock="${XDG_RUNTIME_DIR}/ccy/image-builds/claude-yolo_latest.lock"
case "${kind}" in
    live)
        # Its self-prune (lib/image-build-prune.bash) is `true`: it records and removes nothing.
        CCY_CONTAINER_IMAGES=true bash -c 'source "$1" && ccy_image_build_locked claude-yolo:latest false -- sleep 5 && : >"$2"' \
            _ "${repo}/files/var/local/claude-yolo/lib/image-build-lock.bash" "${tmp}/built" &
        for _ in 1 2 3 4 5 6 7 8 9 10; do
            [[ -s "${lock}" ]] && break
            sleep 0.2
        done ;;
    record) printf '%s %s claude-yolo:latest\n' "$$" "${EPOCHSECONDS}" >"${lock}" ;;
    none) ;;
esac
SESSION_NAME=s SESSION_PID=""
if [[ "${session}" == ends ]]; then
    bash -c 'sleep 1; exit 1' &
    SESSION_PID=$!
fi
built() { [[ -e "${tmp}/built" ]] || return 3; }
case "${mode}" in
    grace) M7_BUILD_GRACE=1 m7_poll 2 "the fixture wait" built ;;
    timeout) m7_poll 2 "the fixture wait" built ;;
    launch) declare -f m7_start ;;
esac
"""

class LaunchWaitTest(TempDir):
    """The launch's waits allow for an image build (the host run that failed at 600 s had
    rebuilt the project image first) and still fail fast when the launch ends."""

    def wait(self, mode: str, kind: str, session: str = "lives") -> tuple[int, str, str]:
        done = subprocess.run(["bash", "-c", WAIT_DRIVER, "driver", str(_HERE), str(self.dir), mode, kind, session,
                               str(_repo_root())],
                              capture_output=True, text=True, timeout=60, check=False)
        return done.returncode, done.stdout, done.stderr

    def test_a_live_build_restarts_the_wait_until_it_ends(self) -> None:
        status, _out, err = self.wait("grace", "live")
        self.assertEqual(status, 0, err)
        self.assertIn("waiting for the image build of claude-yolo:latest", err)
        self.assertIn("waited for an image build first", (self.dir / "report.md").read_text(encoding="utf-8"))

    def test_a_launch_that_ends_during_the_build_fails_at_once(self) -> None:
        status, _out, err = self.wait("grace", "live", "ends")
        self.assertEqual(status, 1)
        self.assertIn("the ccy session s ended", err)

    def test_no_build_or_a_record_whose_lock_nothing_holds_keeps_the_timeout(self) -> None:
        for kind in ("none", "record"):
            with self.subTest(kind=kind):
                status, _out, err = self.wait("grace", kind)
                self.assertEqual(status, 1)
                self.assertIn("not within 2 s", err)

    def test_without_the_grace_a_build_does_not_extend_the_wait(self) -> None:
        status, _out, err = self.wait("timeout", "live")
        self.assertEqual(status, 1)
        self.assertIn("not within 2 s", err)

    def test_both_start_waits_of_a_launch_allow_for_a_build(self) -> None:
        status, out, err = self.wait("launch", "none")
        self.assertEqual(status, 0, err)
        self.assertIn('m7_poll_launch "session $1\'s container running" m7_container_up', out)
        self.assertIn('m7_poll_launch "seats ${SEAT_LIST} held" m7_seats_held', out)
        self.assertNotIn("M7_START_S", out)


if __name__ == "__main__":
    unittest.main()
