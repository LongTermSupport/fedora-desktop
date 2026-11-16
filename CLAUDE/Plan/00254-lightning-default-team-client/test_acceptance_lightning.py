"""Tests for acceptance_lightning.py, the checks behind this plan's acceptance.bash (Task 5.5).

Nothing here starts Flatpak or reads the real host: every check runs over a temporary home, a
temporary approved-state root and host vars text written here.

Run from anywhere:
    python3 CLAUDE/Plan/00254-lightning-default-team-client/test_acceptance_lightning.py
"""

from __future__ import annotations

import importlib.util
import io
import json
import os
import pathlib
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

_HERE = pathlib.Path(__file__).resolve().parent
_SPEC = importlib.util.spec_from_file_location("acceptance_lightning", _HERE / "acceptance_lightning.py")
if _SPEC is None or _SPEC.loader is None:
    raise ImportError(f"cannot load {_HERE / 'acceptance_lightning.py'}")
al = importlib.util.module_from_spec(_SPEC)
sys.modules["acceptance_lightning"] = al
_SPEC.loader.exec_module(al)

APP = "org.lightning_matrix.Lightning"
PEM = "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"


def host_vars(*entries: str, default: str | None = None, absent: tuple[str, ...] = ()) -> str:
    """Host vars text in the shape localhost.yml.dist shows."""
    lines = []
    if default is not None:
        lines.append(f"agent_bus_matrix_client: {default}\n")
    if absent:
        lines.append("agent_bus_teams:\n")
        for team in absent:
            lines += [f"  - team: {team}\n", "    port: 6170\n", "    state: absent\n"]
    lines.append("agent_bus_element_teams:\n")
    lines += entries
    return "".join(lines)


def entry(team: str, base_url: str, client: str | None = None, ca_file: str | None = None) -> str:
    text = f"  - team: {team}\n    base_url: {base_url}\n"
    if client is not None:
        text += f"    client: {client}\n"
    if ca_file is not None:
        text += f"    ca_file: {ca_file}\n"
    return text


class Fixture(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        base = pathlib.Path(self._tmp.name)
        self.home = base / "home"
        self.home.mkdir()
        self.approved = base / "approved"
        self.approved.mkdir()
        self.root = self.home / ".var/app" / APP

    def approve(self, team: str, url: str) -> None:
        folder = self.approved / team
        folder.mkdir()
        (folder / "approved.json").write_text(json.dumps({"approved": {"url": url}}))

    def write_team(self, team: str, url: str, settings_mode: int = 0o600) -> None:
        """What agent-bus-lightning apply writes for one team."""
        for path in (self.root / "teams", self.root / "teams" / team):
            path.mkdir(parents=True, exist_ok=True)
            path.chmod(0o700)
        for name in ("data", "config", "cache"):
            (self.root / "teams" / team / name).mkdir(mode=0o700)
        settings = self.root / "teams" / team / "config/MatrixClient/matrix-client.conf"
        settings.parent.mkdir(mode=0o700)
        settings.write_text(f"[homeserver]\nurl={url}\n")
        settings.chmod(settings_mode)
        apps = self.home / ".local/share/applications"
        apps.mkdir(parents=True, exist_ok=True)
        launcher = apps / f"agent-bus-{team}-lightning.desktop"
        launcher.write_text("[Desktop Entry]\nType=Application\n" + al.exec_line(self.home, team) + "\n")
        launcher.chmod(0o644)

    def place_ca(self, team: str, text: str = PEM) -> None:
        folder = self.root / "teamchat-ca"
        folder.mkdir(parents=True, exist_ok=True)
        (folder / f"{team}.pem").write_text(text)
        (folder / f"{team}.pem").chmod(0o644)

    def place_check(self) -> None:
        binary = self.home / ".local/libexec/tls-trust-check/tls-trust-check"
        binary.parent.mkdir(parents=True, exist_ok=True)
        binary.write_text("#!/bin/false\n")
        binary.chmod(0o755)

    # The refusal of root is its own test; a container's tests may run as root, so the other
    # runs are given a desktop user's euid (file ownership is still judged against the real one).
    def run_check(self, text: str) -> tuple[int, list[list[str]], str]:
        hv = pathlib.Path(self._tmp.name) / "localhost.yml"
        hv.write_text(text)
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = al.main(["check", "--file", str(hv), "--approved-root", str(self.approved),
                            "--home", str(self.home)], euid=os.geteuid() or 1000)
        rows =[line.split("\t") for line in out.getvalue().splitlines()]
        return code, rows, err.getvalue()

    def fails(self, rows: list[list[str]]) -> list[list[str]]:
        return [r for r in rows if r[0] == "FAIL"]


CLIENT_CASES = al._repo_root(_HERE) / "src-go/internal/bus/declare/testdata/client-cases.json"


class SharedCasesTest(Fixture):
    """The resolution table the Go (bus/declare clients_test.go) is tested on too."""

    def setUp(self) -> None:
        super().setUp()
        self.cases = json.loads(CLIENT_CASES.read_text(encoding="utf-8"))
        self.assertTrue(self.cases["resolves"] and self.cases["refuses"], CLIENT_CASES)

    def test_each_case_resolves_as_the_table_says(self) -> None:
        for case in self.cases["resolves"]:
            with self.subTest(case["name"]):
                teams = al.resolve(case["hostVars"], self.approved)
                self.assertEqual({t.team: t.client for t in teams}, case["clients"])
                self.assertEqual(al.home_default(case["hostVars"]), case["default"])

    def test_each_refusal_is_refused(self) -> None:
        for case in self.cases["refuses"]:
            with self.subTest(case["name"]), self.assertRaises((al.Refused, al.CouldNotEstablish)):
                al.resolve(case["hostVars"], self.approved)


class ResolveTest(Fixture):
    def test_the_team_client_wins_over_the_home_setting_which_wins_over_lightning(self) -> None:
        text = host_vars(entry("alpha", "http://192.0.2.1:6167"),
                         entry("beta", "http://192.0.2.1:6168", client="lightning"), default="element")
        teams = al.resolve(text, self.approved)
        self.assertEqual([(t.team, t.client) for t in teams], [("alpha", "element"), ("beta", "lightning")])
        teams = al.resolve(host_vars(entry("alpha", "http://192.0.2.1:6167")), self.approved)
        self.assertEqual(teams[0].client, "lightning")

    def test_a_team_declared_absent_is_left_out(self) -> None:
        text = host_vars(entry("alpha", "http://192.0.2.1:6167", client="lightning"), absent=("alpha",))
        self.assertEqual(al.resolve(text, self.approved), [])

    def test_https_comes_from_the_approved_url_or_a_given_ca_file(self) -> None:
        self.approve("alpha", "https://alpha.example.com:8448")
        self.approve("gamma", "http://192.0.2.1:6169")
        text = host_vars(entry("alpha", "http://192.0.2.1:6167", client="lightning"),
                         entry("beta", "https://beta.example.com", client="lightning", ca_file="/etc/beta.pem"),
                         entry("gamma", "http://192.0.2.1:6169", client="lightning"))
        teams = {t.team: t.https_url for t in al.resolve(text, self.approved)}
        self.assertEqual(teams, {"alpha": "https://alpha.example.com:8448",
                                 "beta": "https://beta.example.com", "gamma": None})

    def test_an_unreadable_approval_could_not_be_established(self) -> None:
        self.approve("alpha", "https://alpha.example.com")
        (self.approved / "alpha" / "approved.json").write_text("{not json")
        with self.assertRaises(al.CouldNotEstablish):
            al.resolve(host_vars(entry("alpha", "http://192.0.2.1:6167", client="lightning")), self.approved)

    def test_a_vaulted_value_could_not_be_established(self) -> None:
        text = host_vars("  - team: alpha\n    base_url: !vault |\n      abc\n")
        with self.assertRaises(al.CouldNotEstablish):
            al.resolve(text, self.approved)

    def test_an_unknown_client_is_refused(self) -> None:
        with self.assertRaises(al.Refused):
            al.resolve(host_vars(entry("alpha", "http://192.0.2.1:6167"), default="nheko"), self.approved)
        with self.assertRaises(al.Refused):
            al.resolve(host_vars(entry("alpha", "http://192.0.2.1:6167", client="nheko")), self.approved)

    def test_the_home_setting_set_twice_could_not_be_established(self) -> None:
        text = "agent_bus_matrix_client: lightning\n" + host_vars(entry("a", "http://192.0.2.1:1"), default="element")
        with self.assertRaises(al.CouldNotEstablish):
            al.resolve(text, self.approved)


class CheckTest(Fixture):
    def test_exec_line_is_the_one_the_go_command_writes(self) -> None:
        root = f"{self.home}/.var/app/{APP}"
        self.assertEqual(
            al.exec_line(self.home, "alpha"),
            f"Exec=flatpak run --command=env {APP} XDG_DATA_HOME={root}/teams/alpha/data "
            f"XDG_CONFIG_HOME={root}/teams/alpha/config XDG_CACHE_HOME={root}/teams/alpha/cache "
            f"SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt SSL_CERT_DIR={root}/teamchat-ca lightning-matrix")

    def test_a_written_http_team_passes(self) -> None:
        self.write_team("alpha", "http://192.0.2.1:6167")
        code, rows, err = self.run_check(host_vars(entry("alpha", "http://192.0.2.1:6167"), default="lightning"))
        self.assertEqual(code, al.EXIT_OK, (rows, err))
        self.assertEqual(self.fails(rows), [])
        self.assertFalse([r for r in rows if r[0] == "HANDSHAKE"])

    def test_no_lightning_team_could_not_be_established(self) -> None:
        code, rows, err = self.run_check(host_vars(entry("alpha", "http://192.0.2.1:6167"), default="element"))
        self.assertEqual(code, al.EXIT_NOT_ESTABLISHED)
        self.assertIn("no declared team opens in Lightning", err)

    def test_a_wrong_exec_line_fails_naming_team_and_file(self) -> None:
        self.write_team("alpha", "http://192.0.2.1:6167")
        launcher = self.home / ".local/share/applications/agent-bus-alpha-lightning.desktop"
        launcher.write_text("[Desktop Entry]\nExec=flatpak run " + APP + "\n")
        code, rows, _ = self.run_check(host_vars(entry("alpha", "http://192.0.2.1:6167", client="lightning")))
        self.assertEqual(code, al.EXIT_FAIL)
        self.assertEqual([(r[1], r[2]) for r in self.fails(rows)], [("alpha", str(launcher))])

    def test_a_wide_directory_and_a_wide_settings_file_fail(self) -> None:
        self.write_team("alpha", "http://192.0.2.1:6167", settings_mode=0o644)
        (self.root / "teams/alpha/cache").chmod(0o755)
        code, rows, _ = self.run_check(host_vars(entry("alpha", "http://192.0.2.1:6167", client="lightning")))
        self.assertEqual(code, al.EXIT_FAIL)
        failed = {r[2] for r in self.fails(rows)}
        self.assertEqual(failed, {str(self.root / "teams/alpha/cache"),
                                  str(self.root / "teams/alpha/config/MatrixClient/matrix-client.conf")})

    def test_a_missing_instance_fails(self) -> None:
        code, rows, _ = self.run_check(host_vars(entry("alpha", "http://192.0.2.1:6167", client="lightning")))
        self.assertEqual(code, al.EXIT_FAIL)
        self.assertIn(str(self.root / "teams/alpha/data"), {r[2] for r in self.fails(rows)})

    def test_the_settings_url_may_be_lightnings_canonical_form(self) -> None:
        self.write_team("alpha", "https://Alpha.example.com:443/")
        self.place_ca("alpha")
        self.place_check()
        code, rows, _ = self.run_check(host_vars(
            entry("alpha", "https://alpha.example.com", client="lightning", ca_file="/etc/a.pem")))
        self.assertEqual(code, al.EXIT_OK, rows)
        settings = [r[3] for r in rows if r[2].endswith("matrix-client.conf")]
        self.assertEqual(settings, ["file, mode 0600",
                                    "names https://Alpha.example.com:443/, Lightning's canonical form of the base_url"])

    def test_a_different_settings_url_fails(self) -> None:
        self.write_team("alpha", "http://192.0.2.9:6167")
        code, rows, _ = self.run_check(host_vars(entry("alpha", "http://192.0.2.1:6167", client="lightning")))
        self.assertEqual(code, al.EXIT_FAIL)
        self.assertTrue(self.fails(rows)[0][2].endswith("matrix-client.conf"))

    def test_an_https_team_needs_its_ca_and_the_check_and_asks_for_a_handshake(self) -> None:
        self.approve("alpha", "https://alpha.example.com:8448")
        self.write_team("alpha", "http://192.0.2.1:6167")
        text = host_vars(entry("alpha", "http://192.0.2.1:6167", client="lightning"))
        code, rows, _ = self.run_check(text)
        self.assertEqual(code, al.EXIT_FAIL)
        self.assertEqual({r[2] for r in self.fails(rows)},
                         {str(self.root / "teamchat-ca/alpha.pem"),
                          str(self.home / ".local/libexec/tls-trust-check/tls-trust-check")})
        self.place_ca("alpha")
        self.place_check()
        code, rows, _ = self.run_check(text)
        self.assertEqual(code, al.EXIT_OK, rows)
        handshake = [r for r in rows if r[0] == "HANDSHAKE"]
        self.assertEqual(handshake, [["HANDSHAKE", "alpha", "https://alpha.example.com:8448",
                                      str(self.root / "teamchat-ca"),
                                      str(self.home / ".local/libexec/tls-trust-check"), APP]])

    def test_a_ca_file_that_is_not_pem_fails(self) -> None:
        self.approve("alpha", "https://alpha.example.com")
        self.write_team("alpha", "http://192.0.2.1:6167")
        self.place_ca("alpha", "not a certificate\n")
        self.place_check()
        code, rows, _ = self.run_check(host_vars(entry("alpha", "http://192.0.2.1:6167", client="lightning")))
        self.assertEqual(code, al.EXIT_FAIL)
        self.assertEqual([r[2] for r in self.fails(rows)], [str(self.root / "teamchat-ca/alpha.pem")])

    def test_each_team_has_only_its_own_clients_launcher(self) -> None:
        self.write_team("alpha", "http://192.0.2.1:6167")
        apps = self.home / ".local/share/applications"
        (apps / "agent-bus-alpha-element.desktop").write_text("[Desktop Entry]\n")
        (apps / "agent-bus-beta-lightning.desktop").write_text("[Desktop Entry]\n")
        code, rows, _ = self.run_check(host_vars(entry("alpha", "http://192.0.2.1:6167"),
                                                 entry("beta", "http://192.0.2.1:6168", client="element")))
        self.assertEqual(code, al.EXIT_FAIL)
        self.assertEqual({(r[1], r[2]) for r in self.fails(rows)},
                         {("alpha", str(apps / "agent-bus-alpha-element.desktop")),
                          ("beta", str(apps / "agent-bus-beta-lightning.desktop"))})

    def test_a_user_holding_pingbus_bundles_is_refused(self) -> None:
        (self.home / ".config/pingbus").mkdir(parents=True)
        (self.home / ".config/pingbus/bundle").write_text("x")
        code, _, err = self.run_check(host_vars(entry("alpha", "http://192.0.2.1:6167", client="lightning")))
        self.assertEqual(code, al.EXIT_FAIL)
        self.assertIn("pingbus bundles", err)

    def test_root_is_refused(self) -> None:
        err = io.StringIO()
        with redirect_stderr(err):
            code = al.main(["check", "--file", "/nonexistent", "--approved-root", "/nonexistent"], euid=0)
        self.assertEqual(code, al.EXIT_FAIL)
        self.assertIn("not root", err.getvalue())

    def test_an_unreadable_host_vars_file_could_not_be_established(self) -> None:
        err = io.StringIO()
        with redirect_stderr(err):
            code = al.main(["check", "--file", os.devnull + "-missing", "--approved-root", str(self.approved),
                            "--home", str(self.home)], euid=os.geteuid() or 1000)
        self.assertEqual(code, al.EXIT_NOT_ESTABLISHED)


class ProductValuesTest(unittest.TestCase):
    """The layout the checks judge is read from src-go/internal/lightning, never copied
    (QA.md checker-copies-cited-constant)."""

    GO = """const (
\tteamsDir   = "teams"
\tcaDir      = "teamchat-ca" // a trailing comment
\tdirMode      fs.FileMode = 0o700
)

var instanceDirs = []string{"data", "config", "cache"}
"""

    def test_the_readers_take_each_kind_of_constant(self) -> None:
        self.assertEqual(al.go_text(self.GO, "caDir"), "teamchat-ca")
        self.assertEqual(al.go_mode(self.GO, "dirMode"), 0o700)
        self.assertEqual(al.go_strings(self.GO, "instanceDirs"), ("data", "config", "cache"))

    def test_a_missing_constant_is_refused(self) -> None:
        for read in (al.go_text, al.go_mode, al.go_strings):
            with self.subTest(read=read.__name__), self.assertRaises(ValueError):
                read(self.GO, "absent")

    def test_a_list_with_no_plain_entries_is_refused(self) -> None:
        # An empty or unread list would make every instance-folder check pass with nothing checked.
        for body in ("", '"a\\"b"'):
            with self.subTest(body=body), self.assertRaises(ValueError):
                al.go_strings(f"var instanceDirs = []string{{{body}}}\n", "instanceDirs")

    def test_the_checker_holds_the_products_values(self) -> None:
        source = (al.REPO_ROOT / al.LIGHTNING_GO).read_text(encoding="utf-8")
        self.assertEqual(al.TEAMS_DIR, al.go_text(source, "teamsDir"))
        self.assertEqual(al.CA_DIR, al.go_text(source, "caDir"))
        self.assertEqual(al.INSTANCE_DIRS, al.go_strings(source, "instanceDirs"))
        self.assertEqual(al.SETTINGS_MODE, al.go_mode(source, "settingsMode"))
        self.assertEqual(al.LAUNCHER_MODE, al.go_mode(source, "launcherMode"))


if __name__ == "__main__":
    unittest.main()
