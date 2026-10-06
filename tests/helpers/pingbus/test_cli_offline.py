"""Unit tests for the offline parts of helpers/pingbus/cli.py.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_cli_offline

Covers dispatch and the exit codes (spec §14), the line formatter (§15), `validate`,
`config check`, `suggest-handle` (§3) and `version` (§1). Spec:
docs/agent-bus-protocol.md. The network commands are U11/U12's and are not here.
Every bundle is built in a temporary directory; nothing reads the machine's own config.
"""

from __future__ import annotations

import io
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.pingbus import cli, config
from helpers.pingbus import protocol as p

DOC = REPO_ROOT / "docs" / "agent-bus-protocol.md"
SN = "team-a.agent-bus.internal"
TEAM = "team-a"
SELF_HANDLE = "myrepo.1+workstation.podman"
SELF = f"@{SELF_HANDLE}:{SN}"
ORCH = f"@orch.1+workstation.podman:{SN}"
WORKER = f"@other.3+workstation.host:{SN}"
HUMAN = f"@alice:{SN}"
ROOM = "!" + "A" * 43
EVENT = "$" + "B" * 43
TOKEN = "syt_ZXhhbXBsZQ_notarealtoken_0123"
SHA = "0123456789abcdef0123456789abcdef01234567"
REF_PATH = f"path:example-org/myrepo@{SHA}:CLAUDE/Plan/00001-x/PLAN.md"


def record(**overrides: object) -> dict[str, object]:
    data: dict[str, object] = {
        "v": 1,
        "team": TEAM,
        "humans": [HUMAN],
        "roles": {SELF: "worker", ORCH: "orchestrator", WORKER: "worker"},
        "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
        "path_prefixes": ["CLAUDE/Plan/", "docs/"],
        "forge_api": "https://api.github.com",
    }
    data.update(overrides)
    return data


def write_bundle(home: pathlib.Path, *, team_record: dict[str, object] | None = None) -> pathlib.Path:
    bundle = home / TEAM
    bundle.mkdir(parents=True)
    member = {
        "protocol": 1,
        "team": TEAM,
        "user_id": SELF,
        "server_name": SN,
        "base_url": "http://192.0.2.10:8448",
        "plain_http_hosts": ["192.0.2.10"],
        "token_file": "token",
        "admin": f"@admin:{SN}",
        "room": ROOM,
    }
    (bundle / "member.json").write_text(json.dumps(member), encoding="utf-8")
    token = bundle / "token"
    token.write_text(TOKEN, encoding="utf-8")
    token.chmod(0o600)
    if team_record is not None:
        state = bundle / "state"
        state.mkdir()
        (state / cli.TEAM_RECORD_CACHE).write_text(json.dumps(team_record), encoding="utf-8")
    return bundle


def run(argv: list[str], environ: dict[str, str]) -> tuple[int, str, str]:
    out, err = io.StringIO(), io.StringIO()
    code = cli.main(argv, environ=environ, stdout=out, stderr=err)
    return code, out.getvalue(), err.getvalue()


def base_env(tmp: pathlib.Path) -> dict[str, str]:
    return {
        "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
        "HOME": str(tmp / "home"),
        "GIT_CONFIG_GLOBAL": os.devnull,
        "GIT_CONFIG_NOSYSTEM": "1",
    }


def run_process(argv: list[str], env: dict[str, str], cwd: pathlib.Path = REPO_ROOT) -> subprocess.CompletedProcess:
    env = dict(env, PYTHONPATH=str(REPO_ROOT))
    return subprocess.run(
        [sys.executable, "-m", "helpers.pingbus.cli", *argv],
        cwd=cwd, env=env, capture_output=True, text=True, check=False, timeout=60,
    )


def doc_section(number: int) -> str:
    text = DOC.read_text(encoding="utf-8")
    match = re.search(rf"^## {number}\. .*?$(.*?)(?=^## |\Z)", text, re.M | re.S)
    if match is None:
        raise AssertionError(f"section {number} not found")
    return match.group(1)


def table_rows(body: str) -> list[list[str]]:
    rows = [
        [c.strip() for c in re.split(r"(?<!\\)\|", line)[1:-1]]
        for line in body.splitlines()
        if line.startswith("|")
    ]
    if len(rows) < 3:
        raise AssertionError("no table")
    return rows[2:]


class TempCase(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = pathlib.Path(self._tmp.name)
        (self.tmp / "home").mkdir()
        self.home = self.tmp / "pingbus"
        self.env = base_env(self.tmp)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def active(self, team_record: dict[str, object] | None = None) -> dict[str, str]:
        write_bundle(self.home, team_record=team_record)
        return dict(self.env, PINGBUS_HOME=str(self.home), PINGBUS_TEAMS=TEAM)


class TestExitCodeTable(unittest.TestCase):
    def test_table_equals_doc(self) -> None:
        rows = table_rows(doc_section(14))
        doc = {int(code): meaning for code, meaning in rows}
        self.assertEqual(doc, cli.EXIT_CODES)

    def test_config_exceptions_carry_the_table_codes(self) -> None:
        self.assertEqual(config.ConfigError.EXIT_CODE, cli.EXIT_CONFIG)
        self.assertEqual(config.UsageError.EXIT_CODE, cli.EXIT_USAGE)
        self.assertEqual(cli.EXIT_CONFIG, 78)
        self.assertEqual(cli.EXIT_USAGE, 64)
        self.assertEqual(cli.EXIT_REFUSED, 4)
        self.assertEqual(cli.EXIT_UNTRUSTED, 10)


class TestSubprocess(TempCase):
    """Real process runs: the exit status and which stream each output lands on."""

    def test_version_exit_0_stdout_only(self) -> None:
        proc = run_process(["version"], self.env)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout, f"pingbus {cli.TOOL_VERSION} protocol {p.PROTOCOL_VERSION}\n")
        self.assertEqual(proc.stderr, "")

    def test_validate_refusal_exit_4_reason_on_stdout(self) -> None:
        proc = run_process(["validate", "halt", REF_PATH], self.env)
        self.assertEqual(proc.returncode, 4)
        self.assertEqual(proc.stdout, "ref\n")

    def test_validate_ok_exit_0(self) -> None:
        proc = run_process(["validate", "review", REF_PATH], self.env)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout, "OK\n")
        self.assertIn("grammar only", proc.stderr)

    def test_usage_error_exit_64_stderr_only(self) -> None:
        for argv in (["no-such-command"], [], ["validate"], ["version", "--bogus"],
                     ["validate", "--event", "x.json", "review"]):
            with self.subTest(argv=argv):
                proc = run_process(argv, self.env)
                self.assertEqual(proc.returncode, 64, proc.stderr)
                self.assertEqual(proc.stdout, "")
                self.assertNotEqual(proc.stderr, "")

    def test_config_check_without_teams_exit_78_stderr_only(self) -> None:
        proc = run_process(["config", "check"], self.env)
        self.assertEqual(proc.returncode, 78)
        self.assertEqual(proc.stdout, "")
        self.assertIn("PINGBUS_TEAMS", proc.stderr)

    def test_validate_event_without_team_exit_78_stderr_only(self) -> None:
        path = self.tmp / "event.json"
        path.write_text(json.dumps(message_event(ORCH, {})), encoding="utf-8")
        proc = run_process(["validate", "--event", str(path)], self.env)
        self.assertEqual(proc.returncode, 78)
        self.assertEqual(proc.stdout, "")
        self.assertIn("--event", proc.stderr)

    def test_suggest_handle_without_role_exit_78(self) -> None:
        env = dict(self.env, CCY_HOST_HOSTNAME="realhostname", HOSTNAME="realhostname", container="docker")
        proc = run_process(["suggest-handle"], env, cwd=self.tmp)
        self.assertEqual(proc.returncode, 78)
        self.assertEqual(proc.stdout, "")
        self.assertIn("HOOKS_DAEMON_HOSTNAME", proc.stderr)
        self.assertNotIn("realhostname", proc.stderr)

    def test_suggest_handle_from_git_remote(self) -> None:
        checkout = self.tmp / "Some.Checkout"
        checkout.mkdir()
        git_env = dict(self.env)
        subprocess.run(["git", "init", "-q", str(checkout)], env=git_env, check=True)
        subprocess.run(
            ["git", "-C", str(checkout), "remote", "add", "origin",
             "git@github.com:Example-Org/My.Repo.git"],
            env=git_env, check=True,
        )
        env = dict(self.env, HOOKS_DAEMON_HOSTNAME="Work Station", container="docker")
        proc = run_process(["suggest-handle"], env, cwd=checkout)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout, "--repo=my-repo --host=work-station --type=docker\n")

    def test_bundle_token_never_on_either_stream(self) -> None:
        env = self.active(record())
        for argv in (["config", "check"], ["validate", "review", REF_PATH]):
            with self.subTest(argv=argv):
                proc = run_process(argv, env)
                self.assertEqual(proc.returncode, 0, proc.stderr)
                self.assertNotIn(TOKEN, proc.stdout + proc.stderr)


class TestDispatch(TempCase):
    def test_python_gate_is_78(self) -> None:
        with mock.patch.object(cli.sys, "version_info", (3, 10, 4)):
            code, out, err = run(["version"], self.env)
        self.assertEqual(code, 78)
        self.assertEqual(out, "")
        self.assertIn("3.11", err)

    def test_team_option_before_and_after_command(self) -> None:
        env = self.active(record())
        for argv in (["--team", TEAM, "config", "check"], ["config", "check", "--team", TEAM]):
            with self.subTest(argv=argv):
                code, _, err = run(argv, env)
                self.assertEqual(code, 0, err)

    def test_unknown_team_is_64(self) -> None:
        env = self.active(record())
        code, out, err = run(["--team", "team-b", "config", "check"], env)
        self.assertEqual(code, 64)
        self.assertEqual(out, "")

    def test_help_is_0_on_stdout(self) -> None:
        for argv in (["--help"], ["validate", "--help"]):
            with self.subTest(argv=argv):
                code, out, err = run(argv, self.env)
                self.assertEqual((code, err), (0, ""))
                self.assertTrue(out.startswith("usage: pingbus"), out)


class TestLineFormatter(unittest.TestCase):
    def test_lines_table_equals_doc(self) -> None:
        rows = table_rows(doc_section(15))
        doc = {}
        for kind, stream, fields in rows:
            labels = fields.split(", ")
            self.assertEqual(labels[:2], [f"`{code_span(kind)}`", "`1`"])
            # A label's parenthetical note and its code marks are prose, not the field.
            names = tuple(re.sub(r" \(.*\)$", "", label).replace("`", "") for label in labels[2:])
            doc[code_span(kind)] = (stream, names)
        self.assertEqual(doc, cli.LINES)

    def test_ping_line(self) -> None:
        ping = p.Ping("review", (SELF,), p.parse_ref(REF_PATH), None, sender=ORCH, event_id=EVENT,
                      origin_server_ts=1)
        self.assertEqual(
            cli.ping_line(TEAM, SN, ping),
            f"PING\t1\t{TEAM}\t{EVENT}\torch.1+workstation.podman\treview\t{REF_PATH}\t-",
        )

    def test_human_line_escapes(self) -> None:
        msg = p.HumanMessage(EVENT, HUMAN, 1791234567890, "halt\tnow\nplease é")
        line = cli.human_line(TEAM, SN, msg)
        self.assertEqual(
            line, f'HUMAN\t1\t{TEAM}\t{EVENT}\talice\t1791234567890\t"halt\\tnow\\nplease \\u00e9"'
        )
        self.assertEqual(line.count("\t"), 6)

    def test_timeout_and_sent_lines(self) -> None:
        self.assertEqual(
            cli.timeout_line(TEAM, SN, EVENT, WORKER, "halt", None),
            f"TIMEOUT\t1\t{TEAM}\t{EVENT}\tother.3+workstation.host\thalt\t-",
        )
        self.assertEqual(cli.sent_line(TEAM, EVENT), f"SENT\t1\t{TEAM}\t{EVENT}")

    def test_dropped_line_in_reason_order(self) -> None:
        self.assertEqual(
            cli.dropped_line({"sender": 2, "schema": 1, "rate": 0}),
            "DROPPED\t1\t3\tschema=1,sender=2",
        )

    def test_dropped_line_refuses_ignore_reasons_and_empty(self) -> None:
        for counts in ({p.IGNORE_AGENT_TEXT: 1}, {}, {"schema": 0}, {"secret": 1}):
            with self.subTest(counts=counts), self.assertRaises(ValueError):
                cli.dropped_line(counts)

    def test_fields_failing_grammar_are_refused(self) -> None:
        bad = [
            lambda: cli.sent_line("Team A", EVENT),
            lambda: cli.sent_line(TEAM, "$short"),
            lambda: cli.timeout_line(TEAM, SN, EVENT, f"@x:{SN}\n", "halt", None),
            lambda: cli.timeout_line(TEAM, SN, EVENT, "@alice:other.server", "halt", None),
            lambda: cli.timeout_line(TEAM, SN, EVENT, WORKER, "explode", None),
            lambda: cli.timeout_line(TEAM, SN, EVENT, WORKER, "review", "path:x\ty"),
        ]
        for i, call in enumerate(bad):
            with self.subTest(i=i), self.assertRaises(ValueError):
                call()

    def test_stream_of_each_kind(self) -> None:
        out, err = io.StringIO(), io.StringIO()
        cli.emit(cli.sent_line(TEAM, EVENT), out, err)
        cli.emit(cli.dropped_line({"schema": 1}), out, err)
        self.assertTrue(out.getvalue().startswith("SENT\t"))
        self.assertTrue(err.getvalue().startswith("DROPPED\t"))
        self.assertEqual(out.getvalue().count("\n") + err.getvalue().count("\n"), 2)


def code_span(cell: str) -> str:
    spans = re.findall(r"`([^`]*)`", cell)
    if len(spans) != 1:
        raise AssertionError(cell)
    return spans[0]


class TestValidateRequest(TempCase):
    def test_grammar_only_without_team(self) -> None:
        cases = [
            (["validate", "explode"], 4, "verb"),
            (["validate", "review"], 4, "ref"),
            (["validate", "halt", REF_PATH], 4, "ref"),
            (["validate", "fetch", f"pr:example-org/myrepo#1@{SHA}"], 4, "ref"),
            (["validate", "ack"], 4, "re"),
            (["validate", "ack", "--re", "$nope"], 4, "re"),
            (["validate", "review", REF_PATH, "--re", EVENT], 4, "re"),
            (["validate", "ack", "--re", EVENT], 0, "OK"),
            (["validate", "halt"], 0, "OK"),
            (["validate", "review", REF_PATH.replace("example-org", "Example-Org")], 0, "OK"),
            # Grammar only: a repository outside any allowlist still passes here.
            (["validate", "review", f"commit:someone/else@{SHA}"], 0, "OK"),
        ]
        for argv, code, word in cases:
            with self.subTest(argv=argv):
                got, out, err = run(argv, self.env)
                self.assertEqual((got, out), (code, f"{word}\n"), err)

    def test_with_team_checks_allowlist_and_role(self) -> None:
        env = self.active(record())
        cases = [
            (["validate", "review", REF_PATH], 0, "OK"),
            (["validate", "review", f"commit:someone/else@{SHA}"], 4, "allowlist"),
            (["validate", "review", f"path:example-org/myrepo@{SHA}:src/x.py"], 4, "allowlist"),
            (["validate", "halt"], 4, "role"),
            (["validate", "sync", REF_PATH], 4, "role"),
            (["validate", "done", "issue:example-org/myrepo#4"], 0, "OK"),
        ]
        for argv, code, word in cases:
            with self.subTest(argv=argv):
                got, out, err = run(argv, env)
                self.assertEqual((got, out), (code, f"{word}\n"), err)
                self.assertNotIn("grammar only", err)

    def test_with_team_but_no_cached_record_is_10(self) -> None:
        env = self.active(None)
        code, out, err = run(["validate", "halt"], env)
        self.assertEqual(code, 10)
        self.assertEqual(out, "")

    def test_cached_record_not_listing_self_is_10(self) -> None:
        env = self.active(record(roles={ORCH: "orchestrator"}))
        code, out, _ = run(["validate", "halt"], env)
        self.assertEqual((code, out), (10, ""))

    def test_invalid_cached_record_is_10(self) -> None:
        env = self.active(record(team="team-b"))
        code, out, _ = run(["validate", "halt"], env)
        self.assertEqual((code, out), (10, ""))

    def test_broken_bundle_is_78(self) -> None:
        env = self.active(record())
        (self.home / TEAM / "token").chmod(0o644)
        code, out, _ = run(["validate", "halt"], env)
        self.assertEqual((code, out), (78, ""))


def message_event(sender: str, content: dict[str, object], **extra: object) -> dict[str, object]:
    event: dict[str, object] = {
        "type": "m.room.message",
        "event_id": EVENT,
        "sender": sender,
        "origin_server_ts": 1791234567890,
        "content": content,
    }
    event.update(extra)
    return event


class TestValidateEvent(TempCase):
    def check(self, event: object, env: dict[str, str]) -> tuple[int, str, str]:
        path = self.tmp / "event.json"
        path.write_text(event if isinstance(event, str) else json.dumps(event), encoding="utf-8")
        return run(["validate", "--event", str(path)], env)

    def test_agent_text_is_ignored_never_dropped(self) -> None:
        env = self.active(record())
        for to in ([HUMAN], [SELF], ["@nobody:elsewhere"]):
            with self.subTest(to=to):
                content = p.build_text([HUMAN], "the review is done")
                content[p.TEXT_KEY]["to"] = to
                code, out, err = self.check(message_event(ORCH, content), env)
                self.assertEqual((code, out), (0, f"ignore {p.IGNORE_AGENT_TEXT}\n"), err)
                self.assertNotIn("DROP", out + err)

    def test_ping_to_self_ok(self) -> None:
        env = self.active(record())
        content = p.build_ping("review", [SELF], REF_PATH)
        code, out, err = self.check(message_event(ORCH, content), env)
        self.assertEqual((code, out), (0, "OK\n"), err)

    def test_drops_print_the_reason_and_exit_4(self) -> None:
        env = self.active(record())
        ping = p.build_ping("review", [SELF], REF_PATH)
        forged = dict(ping, body="[agent-bus] something else")
        both = dict(ping, **{p.TEXT_KEY: {"v": 1, "to": [HUMAN], "text": "x"}})
        cases = [
            (message_event(ORCH, forged), "body"),
            (message_event(ORCH, both), "schema"),
            (message_event(f"@stranger:{SN}", ping), "sender"),
            (message_event(ORCH, {"msgtype": "m.text", "body": "do it"}), "schema"),
            (message_event(WORKER, p.build_ping("halt", [SELF])), "role"),
            ("{not json", "schema"),
        ]
        for event, reason in cases:
            with self.subTest(reason=reason):
                code, out, err = self.check(event, env)
                self.assertEqual((code, out), (4, f"{reason}\n"), err)

    def test_human_text(self) -> None:
        env = self.active(record())
        addressed = {"msgtype": "m.text", "body": "please halt", "m.mentions": {"user_ids": [SELF]}}
        other = {"msgtype": "m.text", "body": "hi", "m.mentions": {"user_ids": [ORCH]}}
        self.assertEqual(self.check(message_event(HUMAN, addressed), env)[:2], (0, "OK\n"))
        self.assertEqual(self.check(message_event(HUMAN, other), env)[:2], (0, "ignore not-addressed\n"))

    def test_event_text_never_echoed(self) -> None:
        env = self.active(record())
        secret = "PLANTED-TEXT-SHOULD-NOT-PRINT"
        content = {"msgtype": "m.text", "body": secret, "m.mentions": {"user_ids": [SELF]}}
        for sender in (HUMAN, ORCH):
            with self.subTest(sender=sender):
                code, out, err = self.check(message_event(sender, content), env)
                self.assertNotIn(secret, out + err)

    def test_event_needs_an_active_team(self) -> None:
        code, out, err = self.check(message_event(ORCH, {}), self.env)
        self.assertEqual((code, out), (78, ""))
        self.assertIn("--event", err)

    def test_missing_event_file_is_64(self) -> None:
        env = self.active(record())
        code, out, _ = run(["validate", "--event", str(self.tmp / "absent.json")], env)
        self.assertEqual((code, out), (64, ""))


class TestConfigCheck(TempCase):
    def test_ok_report(self) -> None:
        env = self.active(None)
        code, out, err = run(["config", "check"], env)
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"OK\t{TEAM}\t{SELF_HANDLE}\thuman_text=true\nFORGE\tnone\n")

    def test_bad_bundle_is_78(self) -> None:
        env = self.active(None)
        (self.home / TEAM / "member.json").write_text("{}", encoding="utf-8")
        code, out, err = run(["config", "check"], env)
        self.assertEqual((code, out), (78, ""))
        self.assertIn(TEAM, err)

    def test_forge_sources_in_order(self) -> None:
        env = self.active(None)
        token_file = self.tmp / "forge-token"
        token_file.write_text("ghp_" + "x" * 36, encoding="utf-8")
        token_file.chmod(0o600)
        cases = [
            ({"PINGBUS_FORGE_TOKEN_FILE": str(token_file), "PINGBUS_FORGE_TOKEN": "a", "GH_TOKEN": "b"},
             "PINGBUS_FORGE_TOKEN_FILE"),
            ({"PINGBUS_FORGE_TOKEN": "a", "GH_TOKEN": "b"}, "PINGBUS_FORGE_TOKEN"),
            ({"GH_TOKEN": "b", "GITHUB_TOKEN": "c"}, "GH_TOKEN"),
            ({"GITHUB_TOKEN": "c"}, "GITHUB_TOKEN"),
            ({"GH_TOKEN": ""}, "none"),
        ]
        for extra, source in cases:
            with self.subTest(source=source):
                code, out, err = run(["config", "check"], dict(env, **extra))
                self.assertEqual(code, 0, err)
                self.assertTrue(out.endswith(f"FORGE\t{source}\n"), out)
                self.assertNotIn("x" * 36, out + err)

    def test_forge_token_file_refused(self) -> None:
        env = self.active(None)
        loose = self.tmp / "loose"
        loose.write_text("abc", encoding="utf-8")
        loose.chmod(0o644)
        link = self.tmp / "link"
        link.symlink_to(loose)
        for path in (loose, link, self.tmp / "absent", "relative/path"):
            with self.subTest(path=path):
                code, out, err = run(["config", "check"], dict(env, PINGBUS_FORGE_TOKEN_FILE=str(path)))
                self.assertEqual((code, out), (78, ""))
                self.assertIn("PINGBUS_FORGE_TOKEN_FILE", err)


class TestSuggestHandle(TempCase):
    def suggest(self, environ: dict[str, str], remote: str | None, top: str, type_: str) -> tuple[int, str, str]:
        with mock.patch.object(cli, "checkout_origin", return_value=(top, remote)), \
             mock.patch.object(cli, "detect_member_type", return_value=type_):
            return run(["suggest-handle"], environ)

    def test_role_only(self) -> None:
        env = dict(self.env, HOOKS_DAEMON_HOSTNAME="workstation")
        code, out, err = self.suggest(env, "https://github.com/example-org/myrepo.git", "/x/y", "podman")
        self.assertEqual((code, out), (0, "--repo=myrepo --host=workstation --type=podman\n"), err)

    def test_directory_name_without_remote(self) -> None:
        env = dict(self.env, HOOKS_DAEMON_HOSTNAME="laptop")
        code, out, _ = self.suggest(env, None, "/srv/_My_Project", "host")
        self.assertEqual((code, out), (0, "--repo=my_project --host=laptop --type=host\n"))

    def test_refuses_without_role_whatever_else_is_set(self) -> None:
        for extra in ({}, {"HOOKS_DAEMON_HOSTNAME": ""}, {"CCY_HOST_HOSTNAME": "realbox"},
                      {"HOSTNAME": "realbox"}):
            with self.subTest(extra=extra):
                code, out, err = self.suggest(dict(self.env, **extra), None, "/x/repo", "podman")
                self.assertEqual((code, out), (78, ""))
                self.assertNotIn("realbox", err)

    def test_refuses_role_that_cannot_be_a_host(self) -> None:
        env = dict(self.env, HOOKS_DAEMON_HOSTNAME="-bad-")
        code, out, _ = self.suggest(env, None, "/x/repo", "podman")
        self.assertEqual((code, out), (78, ""))

    def test_refuses_empty_repo(self) -> None:
        env = dict(self.env, HOOKS_DAEMON_HOSTNAME="box")
        code, out, _ = self.suggest(env, "https://example.com/___.git", "/x/y", "podman")
        self.assertEqual((code, out), (78, ""))


class TestDetectMemberType(unittest.TestCase):
    NONE = (1, "none\n")

    def detect(
        self, environ: dict[str, str], files: set[str], vm: int,
        container: tuple[int, str] = NONE,
    ) -> str:
        """`container` is `systemd-detect-virt --container`'s (status, stdout)."""

        def fake_run(argv: list[str], **kwargs: object) -> subprocess.CompletedProcess:
            self.assertEqual(argv[0], "systemd-detect-virt")
            self.assertIn("check", kwargs)
            if "--container" in argv:
                return subprocess.CompletedProcess(argv, container[0], container[1], "")
            self.assertIn("--vm", argv)
            return subprocess.CompletedProcess(argv, vm)

        with mock.patch.object(cli.os.path, "exists", side_effect=lambda f: f in files), \
             mock.patch.object(cli.subprocess, "run", side_effect=fake_run):
            return cli.detect_member_type(environ)

    def test_order(self) -> None:
        self.assertEqual(self.detect({"container": "lxc"}, {"/run/.containerenv"}, 0), "lxc")
        self.assertEqual(self.detect({}, {"/run/.containerenv"}, 0), "podman")
        self.assertEqual(self.detect({}, {"/.dockerenv"}, 0), "docker")
        self.assertEqual(self.detect({}, set(), 0), "vm")
        self.assertEqual(self.detect({}, set(), 1), "host")

    def test_lxc_from_the_detector_when_the_session_lacks_the_variable(self) -> None:
        # LXC sets `container` only in PID 1's environment and writes no marker file.
        for printed, want in (("lxc\n", "lxc"), ("lxc-libvirt\n", "lxc"),
                              ("podman\n", "podman"), ("docker\n", "docker")):
            with self.subTest(printed=printed):
                self.assertEqual(self.detect({}, set(), 0, (0, printed)), want)

    def test_container_detector_outranks_vm(self) -> None:
        self.assertEqual(self.detect({}, set(), 0, (0, "lxc\n")), "lxc")

    def test_unrecognised_container_is_78_not_host(self) -> None:
        for printed in ("systemd-nspawn\n", "wsl\n", "openvz\n"):
            with self.subTest(printed=printed), self.assertRaises(config.ConfigError) as caught:
                self.detect({}, set(), 1, (0, printed))
            self.assertIn("--type", str(caught.exception))

    def test_unknown_container_value_is_not_trusted_as_a_type(self) -> None:
        self.assertEqual(self.detect({"container": "systemd-nspawn"}, set(), 1), "host")

    def test_detector_failure_is_78(self) -> None:
        with self.assertRaises(config.ConfigError):
            self.detect({}, set(), 2)
        with self.assertRaises(config.ConfigError):
            self.detect({}, set(), 1, (2, ""))


class TestCheckoutOrigin(unittest.TestCase):
    def test_git_runs_in_the_c_locale(self) -> None:
        """"Not a repository" is read from git's message, so git must speak English."""
        calls: list[dict[str, str]] = []

        def fake_run(argv: list[str], **kwargs: object) -> subprocess.CompletedProcess:
            calls.append(kwargs["env"])
            if "rev-parse" in argv:
                return subprocess.CompletedProcess(argv, 0, "/x/repo\n", "")
            return subprocess.CompletedProcess(argv, 0, "https://example.com/o/r.git\n", "")

        with mock.patch.dict(cli.os.environ, {"LANG": "de_DE.UTF-8", "LC_ALL": "de_DE.UTF-8"}), \
             mock.patch.object(cli.subprocess, "run", side_effect=fake_run):
            self.assertEqual(cli.checkout_origin("/x/repo/sub"),
                             ("/x/repo", "https://example.com/o/r.git"))
        self.assertEqual(len(calls), 2)
        for env in calls:
            self.assertEqual(env["LC_ALL"], "C")

    def test_outside_a_checkout_falls_back_to_the_directory(self) -> None:
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.dict(cli.os.environ, {"LC_ALL": "de_DE.UTF-8", "GIT_CEILING_DIRECTORIES": tmp,
                                              "GIT_CONFIG_GLOBAL": os.devnull,
                                              "GIT_CONFIG_NOSYSTEM": "1"}):
            self.assertEqual(cli.checkout_origin(tmp), (tmp, None))


class TestDocMentionsOfflineCommands(unittest.TestCase):
    def test_commands_in_section_13(self) -> None:
        commands = {code_span_first(row[0]) for row in table_rows(doc_section(13))}
        for name in ("version", "config check", "suggest-handle", "validate"):
            self.assertTrue(any(c.startswith(name) for c in commands), name)


def code_span_first(cell: str) -> str:
    spans = re.findall(r"`([^`]*)`", cell)
    return spans[0] if spans else ""


if __name__ == "__main__":
    unittest.main()
