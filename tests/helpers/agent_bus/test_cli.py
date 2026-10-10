"""Unit tests for helpers/agent_bus/cli.py: the `agent-bus` admin command line.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_cli

DESIGN.md section 4 lists the commands; sections 3.4 and 3.6 the renders the installer
uses. Streams (CLAUDE/StderrHygiene.md): stdout is each command's payload and nothing
else; exactly two payloads carry a secret, the `add-member` bundle (and the token
`rotate-token` writes into one) and the password `human password` prints once. Every
other stream of every command is checked for every secret the run created.
"""

from __future__ import annotations

import io
import json
import os
import pathlib
import secrets
import sys
import tarfile
import tempfile
import tomllib
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import admin, cli, teamfile
from tests.helpers.agent_bus.test_admin import Stubbed, team_data
from tests.helpers.pingbus import fake_admin_api

HANDLE = "myrepo.1+workstation.podman"
ADD = ["add-member", "team-a", "--repo=myrepo", "--host=workstation", "--type=podman",
       "--role=worker", "--address=192.0.2.10"]


class TtyBytes(io.BytesIO):
    def isatty(self) -> bool:
        return True


class Run:
    def __init__(self, code: int, out: bytes, err: str) -> None:
        self.code, self.out, self.err = code, out, err

    @property
    def text(self) -> str:
        return self.out.decode()


class CliTestCase(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = pathlib.Path(tmp.name)
        self.team_dir = self.root / "team-a"
        self.team_dir.mkdir(mode=0o700)
        (self.team_dir / "secrets").mkdir(mode=0o700)
        self.shared_secret = secrets.token_hex(64)
        admin.write_private(self.team_dir / "secrets" / "registration_shared_secret", self.shared_secret)
        tf = teamfile.parse_team_file(team_data())
        (self.team_dir / "team.json").write_text(teamfile.dump_team_file(tf), encoding="utf-8")
        self.fake = fake_admin_api.FakeAdminHomeserver(shared_secret=self.shared_secret)
        self.transport = Stubbed(self.fake)
        self.urls: list[str] = []
        self.runs: list[tuple[list[str], Run]] = []

    def factory(self, url: str) -> Stubbed:
        self.urls.append(url)
        return self.transport

    def run_cli(self, argv: list[str], stdin: str = "",
                stdout: io.BytesIO | None = None) -> Run:
        out = io.BytesIO() if stdout is None else stdout
        err = io.StringIO()
        code = cli.main(argv, root=self.root, transport_factory=self.factory,
                        stdin=io.StringIO(stdin), stdout=out, stderr=err)
        run = Run(code, out.getvalue(), err.getvalue())
        self.runs.append((argv, run))
        return run

    def ok(self, argv: list[str], **kwargs: object) -> Run:
        run = self.run_cli(argv, **kwargs)
        self.assertEqual(run.code, 0, run.err)
        return run


class BasicsTest(CliTestCase):
    def test_version(self) -> None:
        self.assertEqual(self.ok(["version"]).text, f"agent-bus {cli.TOOL_VERSION} protocol 1\n")

    def test_usage_errors_exit_64(self) -> None:
        for argv in ([], ["nope"], ["add-member", "team-a"], ADD[:-1] + ["--type=boat", "--address=192.0.2.10"],
                     ["human", "shout", "team-a", "alice"], ["set-role", "team-a", HANDLE]):
            with self.subTest(argv=argv):
                run = self.run_cli(argv)
                self.assertEqual(run.code, 64)
                self.assertEqual(run.out, b"")

    def test_talks_to_loopback_on_the_team_port(self) -> None:
        self.ok(["bootstrap", "team-a"])
        self.assertEqual(self.urls, ["http://127.0.0.1:8448"])

    def test_bootstrap_markers(self) -> None:
        first = self.ok(["bootstrap", "team-a"])
        self.assertTrue(first.text)
        self.assertTrue(all(line.startswith("CHANGED\t") for line in first.text.splitlines()))
        self.assertEqual(self.ok(["bootstrap", "team-a"]).out, b"")

    def test_missing_team_is_config_error(self) -> None:
        run = self.run_cli(["bootstrap", "team-b"])
        self.assertEqual(run.code, 78)
        self.assertIn("team-b", run.err)

    @unittest.skipUnless(os.geteuid() == 0, "chown to another user needs root")
    def test_team_dir_owned_by_someone_else_is_refused(self) -> None:
        os.chown(self.team_dir, 65534, 65534)
        run = self.run_cli(["bootstrap", "team-a"])
        self.assertEqual(run.code, 78)
        self.assertIn("owned", run.err)

    def test_homeserver_refusal_exits_70(self) -> None:
        self.ok(["bootstrap", "team-a"])
        self.fake.add_user("intruder", admin=True)
        run = self.run_cli(["bootstrap", "team-a"])
        self.assertEqual(run.code, 70)
        self.assertIn("server admin", run.err)

    def test_unreachable_exits_69(self) -> None:
        def unreachable(url: str) -> object:
            return admin.http_transport("http://127.0.0.1:9")

        out, err = io.BytesIO(), io.StringIO()
        code = cli.main(["bootstrap", "team-a"], root=self.root, transport_factory=unreachable,
                        stdin=io.StringIO(), stdout=out, stderr=err)
        self.assertEqual(code, 69)


class MemberCommandsTest(CliTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.ok(["bootstrap", "team-a"])

    def test_add_member_writes_a_tar_to_stdout(self) -> None:
        run = self.ok(ADD)
        tar = tarfile.open(fileobj=io.BytesIO(run.out))
        self.assertEqual(sorted(tar.getnames()), ["README", "member.json", "token"])
        member = json.loads(tar.extractfile("member.json").read())
        self.assertIs(member["human_text"], True)
        self.assertIn(HANDLE, run.err)

    def test_no_human_text(self) -> None:
        run = self.ok([*ADD, "--no-human-text"])
        member = json.loads(tarfile.open(fileobj=io.BytesIO(run.out)).extractfile("member.json").read())
        self.assertIs(member["human_text"], False)

    def test_refuses_a_terminal_for_the_bundle(self) -> None:
        run = self.run_cli(ADD, stdout=TtyBytes())
        self.assertEqual(run.code, 64)
        self.assertEqual(run.out, b"")
        self.assertEqual(self.fake.users.get(f"@{HANDLE}:{self.fake.server_name}"), None)

    def test_host_is_required_and_normalised(self) -> None:
        # sudo's env_reset drops any role variable, so no default could ever apply.
        argv = [a for a in ADD if not a.startswith("--host=")]
        run = self.run_cli(argv)
        self.assertEqual(run.code, 64)
        self.assertIn("--host", run.err)
        self.assertEqual(run.out, b"")
        run = self.ok([*argv, "--host=Lab_Box"])
        member = json.loads(tarfile.open(fileobj=io.BytesIO(run.out)).extractfile("member.json").read())
        self.assertTrue(member["user_id"].startswith("@myrepo.1+lab-box.podman:"))

    def test_set_role_list_remove(self) -> None:
        self.ok(ADD)
        self.ok(["set-role", "team-a", HANDLE, "--role=orchestrator"])
        listed = self.ok(["list", "team-a"]).text
        self.assertIn(f"MEMBER\t{HANDLE}\torchestrator\tinvite\tactive\n", listed)
        self.ok(["remove-member", "team-a", HANDLE])
        self.assertNotIn(HANDLE, self.ok(["list", "team-a"]).text)

    def test_add_member_with_a_seat_then_park_and_return(self) -> None:
        seat_handle = "myrepo.dev1+local.podman"
        argv = [a for a in ADD if not a.startswith("--host=")] + ["--host=local", "--seat=dev1"]
        first = self.ok(argv)
        member = json.loads(tarfile.open(fileobj=io.BytesIO(first.out)).extractfile("member.json").read())
        self.assertTrue(member["user_id"].startswith(f"@{seat_handle}:"))
        self.assertIn(f"added {seat_handle}", first.err)
        refused = self.run_cli(argv)
        self.assertEqual((refused.code, refused.out), (70, b""))
        self.assertIn("current member", refused.err)
        parked = self.ok(["park-member", "team-a", seat_handle])
        self.assertEqual(parked.text, f"CHANGED\trevoked the token of {seat_handle}\nCHANGED\tparked {seat_handle}\n")
        self.assertIn(f"MEMBER\t{seat_handle}\tworker\tinvite\tparked\n", self.ok(["list", "team-a"]).text)
        returned = self.ok(argv)
        self.assertEqual(sorted(tarfile.open(fileobj=io.BytesIO(returned.out)).getnames()),
                         ["README", "member.json", "token"])
        self.assertIn(f"returned {seat_handle}", returned.err)
        self.assertIn(f"MEMBER\t{seat_handle}\tworker\tinvite\tactive\n", self.ok(["list", "team-a"]).text)

    def test_seat_usage_errors_exit_64(self) -> None:
        for seat in ("--seat=dev-1", "--seat=0", "--seat=Dev", "--seat="):
            with self.subTest(seat=seat):
                run = self.run_cli([*ADD, seat])
                self.assertEqual((run.code, run.out), (64, b""))
        self.assertEqual(self.run_cli(["park-member", "team-a"]).code, 64)

    def test_park_member_of_an_unknown_handle_exits_70(self) -> None:
        run = self.run_cli(["park-member", "team-a", HANDLE])
        self.assertEqual((run.code, run.out), (70, b""))

    def test_rotate_token_writes_a_token_tar(self) -> None:
        self.ok(ADD)
        run = self.ok(["rotate-token", "team-a", HANDLE])
        self.assertEqual(tarfile.open(fileobj=io.BytesIO(run.out)).getnames(), ["token"])
        self.assertEqual(self.run_cli(["rotate-token", "team-a", HANDLE], stdout=TtyBytes()).code, 64)


class HumanCommandsTest(CliTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.ok(["bootstrap", "team-a"])

    def test_password_is_the_payload(self) -> None:
        run = self.ok(["human", "password", "team-a", "alice"])
        password = run.text.rstrip("\n")
        self.assertEqual(run.text, password + "\n")
        self.assertEqual(self.fake.users[f"@alice:{self.fake.server_name}"].password, password)
        self.assertNotIn(password, run.err)

    def test_other_human_commands(self) -> None:
        self.assertEqual(self.ok(["human", "lock", "team-a", "alice"]).out, b"")
        self.assertEqual(self.ok(["human", "unlock", "team-a", "alice"]).out, b"")
        self.assertEqual(self.ok(["human", "logout-all", "team-a", "alice"]).out, b"")
        self.assertIn("DEVICE\tPHONE1\t", self.ok(["human", "devices", "team-a", "alice"]).text)

    def test_unknown_human_exits_70(self) -> None:
        self.assertEqual(self.run_cli(["human", "password", "team-a", "bob"]).code, 70)


class SecretHygieneTest(CliTestCase):
    """No secret on any stream but the three payloads (DESIGN.md section 12, U15)."""

    PAYLOAD_COMMANDS = {("add-member",), ("rotate-token",), ("human", "password")}

    def test_every_stream(self) -> None:
        self.ok(["bootstrap", "team-a"])
        bundle = tarfile.open(fileobj=io.BytesIO(self.ok(ADD).out))
        member_token = bundle.extractfile("token").read().decode()
        self.ok(["set-role", "team-a", HANDLE, "--role=orchestrator"])
        self.ok(["list", "team-a"])
        seat_add = [*ADD, "--seat=dev"]
        seat_token = tarfile.open(fileobj=io.BytesIO(self.ok(seat_add).out)).extractfile("token").read().decode()
        self.ok(["park-member", "team-a", "myrepo.dev+workstation.podman"])
        returned_token = tarfile.open(fileobj=io.BytesIO(self.ok(seat_add).out)).extractfile("token").read().decode()
        rotated = tarfile.open(fileobj=io.BytesIO(self.ok(["rotate-token", "team-a", HANDLE]).out))
        rotated_token = rotated.extractfile("token").read().decode()
        human_password = self.ok(["human", "password", "team-a", "alice"]).text.strip()
        self.ok(["human", "devices", "team-a", "alice"])
        self.ok(["human", "lock", "team-a", "alice"])
        self.ok(["human", "logout-all", "team-a", "alice"])
        self.ok(["remove-member", "team-a", HANDLE])
        self.run_cli(["human", "password", "team-a", "bob"])
        self.ok(["bootstrap", "team-a"])
        secrets_dir = self.team_dir / "secrets"
        found = [self.shared_secret, member_token, seat_token, returned_token, rotated_token, human_password,
                 (secrets_dir / "admin.token").read_text(), (secrets_dir / "admin.password").read_text()]
        found += [u.password for u in self.fake.users.values() if u.password]
        for argv, run in self.runs:
            payload = any(tuple(argv[:len(p)]) == p for p in self.PAYLOAD_COMMANDS)
            for secret in found:
                with self.subTest(argv=argv):
                    self.assertNotIn(secret, run.err)
                    if not payload:
                        self.assertNotIn(secret.encode(), run.out)


class RenderCommandsTest(CliTestCase):
    def team_json(self, **overrides: object) -> str:
        return json.dumps(team_data(**overrides))

    def test_check_prints_the_canonical_team_json(self) -> None:
        run = self.ok(["render", "check"], stdin=self.team_json())
        self.assertEqual(run.text, teamfile.dump_team_file(teamfile.parse_team_file(team_data())))

    def test_check_refuses_with_nothing_on_stdout(self) -> None:
        run = self.run_cli(["render", "check"], stdin=self.team_json(listen=["0.0.0.0"]))
        self.assertEqual(run.code, 78)
        self.assertEqual(run.out, b"")
        self.assertIn("listen", run.err)
        self.assertEqual(self.run_cli(["render", "check"], stdin="{").code, 78)

    def test_check_against_the_previous_team_json(self) -> None:
        previous = self.team_dir / "team.json"
        self.ok(["render", "check", f"--previous={previous}"], stdin=self.team_json())
        absent = self.run_cli(["render", "check", f"--previous={self.root / 'absent.json'}"],
                              stdin=self.team_json())
        self.assertEqual(absent.code, 78)
        self.assertEqual(absent.out, b"")
        self.assertIn("absent.json", absent.err)
        run = self.run_cli(["render", "check", f"--previous={previous}"],
                           stdin=self.team_json(server_name="other.internal"))
        self.assertEqual(run.code, 78)
        self.assertIn("server_name", run.err)

    def test_toml(self) -> None:
        parsed = tomllib.loads(self.ok(["render", "toml"], stdin=self.team_json()).text)
        self.assertEqual(parsed["port"], 8448)

    def test_dropin(self) -> None:
        text = self.ok(["render", "dropin", "--interface=192.0.2.10=agentbus0"], stdin=self.team_json()).text
        self.assertIn("IPAddressAllow=127.0.0.1/32 ::1/128 192.0.2.10/32 192.0.2.0/24\n", text)
        run = self.run_cli(["render", "dropin"], stdin=self.team_json())
        self.assertEqual(run.code, 78)
        self.assertEqual(run.out, b"")

    def test_renders_need_no_team_dir_or_homeserver(self) -> None:
        self.ok(["render", "toml"], stdin=self.team_json(team="other"))
        self.assertEqual(self.urls, [])


if __name__ == "__main__":
    unittest.main()
