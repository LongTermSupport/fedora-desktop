#!/usr/bin/env python3
"""Plan 00254 Task 5.5: the file checks behind acceptance.bash, read-only.

Which teams open in Lightning is read from the host vars the way play-agent-bus-element.yml
reads them, through the existing readers rather than a new YAML parser:

- the agent_bus_element_teams entries and agent_bus_teams' states through
  helpers.agent_bus.declare (`parse`, `entry_of`), the line reader ccy-teams uses;
- each entry validated, its client resolved, through helpers.agent_bus.element
  (`parse_profiles`), the play's own check step; the user guard is element's `check_user`;
- the home-level agent_bus_matrix_client, a top-level scalar, read here line by line in
  declare's subset (one plain or quoted value; a tag is not read);
- Lightning's app id and data folder from the one Matrix client list (helpers.pingbus.config).

The resolution is the play's: a team's client:, else agent_bus_matrix_client, else element; a
team agent_bus_teams declares absent is left out. A lightning team is an https team when its
entry gives a ca_file (its base_url is then the url) or its approved.json under the approved
root names an https url, as tasks/https-team-ca.yml decides.

    acceptance_lightning.py check --file <host vars> --approved-root <dir> [--home <dir>]

stdout, one tab-separated line per finding (the payload; reasons on stderr):
    TEAM<TAB><team><TAB><client>
    PASS<TAB><team><TAB><path><TAB><what was found>
    FAIL<TAB><team><TAB><path><TAB><the cause>
    HANDSHAKE<TAB><team><TAB><url><TAB><CA dir><TAB><check dir><TAB><app id>   (for acceptance.bash)

Exit codes: 0 every check passed; 1 a check failed, or the run was refused (root, a user
holding pingbus bundles, an entry the play would refuse); 2 a fact could not be established
(the host vars or an approval unreadable, or no team opens in Lightning); 64 usage.
"""

from __future__ import annotations

import argparse
import configparser
import importlib
import json
import os
import pathlib
import pwd
import re
import stat
import sys
import urllib.parse
from collections.abc import Sequence
from dataclasses import dataclass

EXIT_OK = 0
EXIT_FAIL = 1
EXIT_NOT_ESTABLISHED = 2
EXIT_USAGE = 64


def _repo_root(start: pathlib.Path) -> pathlib.Path:
    """The checkout this file is in: the first folder up holding ansible.cfg, never past .git."""
    for folder in start.parents:
        if (folder / "ansible.cfg").exists():
            return folder
        if (folder / ".git").exists():
            break
    raise ImportError(f"no ansible.cfg above {start}")


REPO_ROOT = _repo_root(pathlib.Path(__file__).resolve())
sys.path.insert(0, str(REPO_ROOT))
declare = importlib.import_module("helpers.agent_bus.declare")
element = importlib.import_module("helpers.agent_bus.element")
pingbus_config = importlib.import_module("helpers.pingbus.config")


def _one(source: str, pattern: str, name: str) -> str:
    """The one match of pattern in a Go source; none or several is refused, never guessed."""
    found = re.findall(pattern, source, re.MULTILINE)
    if len(found) != 1:
        raise ValueError(f"want exactly one Go constant {name}, found {len(found)}")
    return found[0]


def go_text(source: str, name: str) -> str:
    """A Go string constant `name = "..."`, as a const block or a const line holds it."""
    return _one(source, rf'^(?:const\s+|\s+){re.escape(name)}\s*=\s*"([^"\\]+)"\s*(?://.*)?$', name)


def go_mode(source: str, name: str) -> int:
    """A Go `name fs.FileMode = 0o...` constant."""
    return int(_one(source, rf"^\s+{re.escape(name)}\s+fs\.FileMode\s*=\s*0o([0-7]+)\s*(?://.*)?$", name), 8)


def go_strings(source: str, name: str) -> tuple[str, ...]:
    """A Go `var name = []string{"a", "b"}` list."""
    body = _one(source, rf"^var\s+{re.escape(name)}\s*=\s*\[\]string\{{([^}}]*)\}}\s*$", name)
    return tuple(re.findall(r'"([^"\\]+)"', body))


LIGHTNING = pingbus_config.MATRIX_CLIENTS.client("Lightning")
CLIENT_LIGHTNING = "lightning"
DEFAULT_KEY = "agent_bus_matrix_client"
# What src-go/internal/lightning writes, read from its source in this checkout.
LIGHTNING_GO = "src-go/internal/lightning/lightning.go"
_GO = (REPO_ROOT / LIGHTNING_GO).read_text(encoding="utf-8")
TEAMS_DIR = go_text(_GO, "teamsDir")
CA_DIR = go_text(_GO, "caDir")
INSTANCE_DIRS = go_strings(_GO, "instanceDirs")
RUNTIME_CA = go_text(_GO, "runtimeCA")
COMMAND = go_text(_GO, "command")
APPS_DIR = go_text(_GO, "appsDir")
SETTINGS_IN = go_text(_GO, "settingsIn")
DIR_MODE = go_mode(_GO, "dirMode")
SETTINGS_MODE = go_mode(_GO, "settingsMode")
LAUNCHER_MODE = go_mode(_GO, "launcherMode")
# Where tasks/lightning-team-ca.yml installs tls-trust-check, in a folder of its own.
CHECK_DIR = ".local/libexec/tls-trust-check"
PEM_BEGIN = "-----BEGIN CERTIFICATE-----"

_DEFAULT_RE = re.compile(rf"{DEFAULT_KEY}\s*:(?P<rest>.*)")


class CouldNotEstablish(Exception):
    """A fact the checks need could not be read."""


class Refused(Exception):
    """The run is refused: the play would refuse it too, or it is the wrong user."""


@dataclass(frozen=True)
class Team:
    team: str
    client: str
    base_url: str
    https_url: str | None


def _scalar(rest: str) -> str:
    value = re.sub(r"(^|\s)#.*$", "", rest).strip()
    if value.startswith("!"):
        raise CouldNotEstablish(f"{DEFAULT_KEY} is tagged (vault-encrypted?); only a plain value is read")
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        value = value[1:-1]
    return value


def home_default(text: str) -> str:
    """agent_bus_matrix_client as the play reads it, element when it is not set."""
    found = [m for m in (_DEFAULT_RE.fullmatch(line.rstrip("\r\n")) for line in text.splitlines()) if m]
    if len(found) > 1:
        raise CouldNotEstablish(f"{DEFAULT_KEY} is set more than once")
    return _scalar(found[0]["rest"]) if found else element.CLIENT_ELEMENT


def _approved_url(approved_root: pathlib.Path, team: str) -> str | None:
    path = approved_root / team / "approved.json"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return None
    except (OSError, ValueError) as exc:
        raise CouldNotEstablish(f"{path} cannot be read ({exc})") from None
    try:
        url = data["approved"]["url"]
    except (KeyError, TypeError):
        raise CouldNotEstablish(f"{path} names no approved url") from None
    if not isinstance(url, str):
        raise CouldNotEstablish(f"{path} names no approved url")
    return url


def resolve(text: str, approved_root: pathlib.Path) -> list[Team]:
    """Every present team of agent_bus_element_teams with its client resolved as the play does."""
    try:
        default = home_default(text)
        absent = {t.team for t in declare.parse(text).teams if t.state == "absent"}
        entries = [declare.entry_of(p) for p in declare.parse(text, declare.ELEMENT_KEY).teams]
    except declare.DeclareError as exc:
        raise CouldNotEstablish(f"the host vars: {exc}") from None
    if default not in element.CLIENTS:
        raise Refused(f"{DEFAULT_KEY} must be one of {', '.join(element.CLIENTS)}")
    present = [{"client": default, **e} for e in entries if e.get("team") not in absent]
    try:
        profiles = element.parse_profiles(present)
    except element.ElementError as exc:
        raise Refused(str(exc)) from None
    teams = []
    for p in profiles:
        https = None
        if p.client == CLIENT_LIGHTNING:
            https = p.base_url if p.ca_file is not None else _approved_url(approved_root, p.team)
            if https is not None and not https.startswith("https://"):
                https = None
        teams.append(Team(p.team, p.client, p.base_url, https))
    return teams


def app_root(home: pathlib.Path) -> pathlib.Path:
    return home / LIGHTNING.flatpak_dir


def launcher(home: pathlib.Path, team: str, client: str) -> pathlib.Path:
    return home / APPS_DIR / f"agent-bus-{team}-{client}.desktop"


def exec_line(home: pathlib.Path, team: str) -> str:
    """lightning.go execLine, without its newline."""
    root = app_root(home)
    words = ["Exec=flatpak", "run", "--command=env", LIGHTNING.app_id]
    words += [f"XDG_{name.upper()}_HOME={root / TEAMS_DIR / team / name}" for name in INSTANCE_DIRS]
    words += [f"SSL_CERT_FILE={RUNTIME_CA}", f"SSL_CERT_DIR={root / CA_DIR}", COMMAND]
    return " ".join(words)


class Report:
    def __init__(self) -> None:
        self.failed = False

    def ok(self, team: str, path: pathlib.Path, what: str) -> None:
        print(f"PASS\t{team}\t{path}\t{what}")

    def fail(self, team: str, path: pathlib.Path, cause: str) -> None:
        self.failed = True
        print(f"FAIL\t{team}\t{path}\t{cause}")


def _mode_of(path: pathlib.Path, want_dir: bool) -> tuple[int | None, str]:
    """The path's permission bits, or None and why it is not what is wanted."""
    try:
        info = os.lstat(path)
    except FileNotFoundError:
        return None, "is missing"
    except OSError as exc:
        return None, f"cannot be looked at ({exc.strerror})"
    kind_ok = stat.S_ISDIR(info.st_mode) if want_dir else stat.S_ISREG(info.st_mode)
    if not kind_ok:
        return None, "is not a directory" if want_dir else "is not a regular file"
    if info.st_uid != os.geteuid():
        return None, "is not owned by this user"
    return stat.S_IMODE(info.st_mode), ""


def _check_mode(report: Report, team: str, path: pathlib.Path, want_dir: bool, mode: int) -> bool:
    found, why = _mode_of(path, want_dir)
    if found is None:
        report.fail(team, path, why)
        return False
    if found != mode:
        report.fail(team, path, f"has mode {found:04o}, not {mode:04o}")
        return False
    report.ok(team, path, f"{'directory' if want_dir else 'file'}, mode {mode:04o}")
    return True


def _normal(url: str) -> tuple[str, str, int | None, str]:
    parts = urllib.parse.urlsplit(url.strip())
    port = parts.port
    if (parts.scheme.lower(), port) in (("https", 443), ("http", 80)):
        port = None
    return parts.scheme.lower(), (parts.hostname or "").lower(), port, parts.path.rstrip("/")


def _check_settings(report: Report, t: Team, path: pathlib.Path) -> None:
    if not _check_mode(report, t.team, path, False, SETTINGS_MODE):
        return
    parser = configparser.RawConfigParser(strict=False)
    try:
        parser.read_string(path.read_text(encoding="utf-8"))
        url = parser.get("homeserver", "url")
    except (OSError, UnicodeDecodeError, configparser.Error) as exc:
        report.fail(t.team, path, f"names no [homeserver] url ({type(exc).__name__})")
        return
    if len(url) >= 2 and url[0] == url[-1] == '"':
        url = url[1:-1]
    if url == t.base_url:
        report.ok(t.team, path, "names the team's base_url, as agent-bus-lightning wrote it")
    elif _normal(url) == _normal(t.base_url):
        report.ok(t.team, path, f"names {url}, Lightning's canonical form of the base_url")
    else:
        report.fail(t.team, path, f"names {url}, not the team's base_url {t.base_url}")


def _check_launcher(report: Report, home: pathlib.Path, team: str) -> None:
    path = launcher(home, team, CLIENT_LIGHTNING)
    if not _check_mode(report, team, path, False, LAUNCHER_MODE):
        return
    lines = [line for line in path.read_text(encoding="utf-8").splitlines() if line.startswith("Exec=")]
    if lines == [exec_line(home, team)]:
        report.ok(team, path, "its Exec line is the one agent-bus-lightning writes")
    else:
        report.fail(team, path, f"its Exec lines {lines} are not the one agent-bus-lightning writes")


def _check_no_launcher(report: Report, home: pathlib.Path, team: str, client: str) -> None:
    path = launcher(home, team, client)
    if os.path.lexists(path):
        report.fail(team, path, f"is there, but the team opens in the other client, not {client}")
    else:
        report.ok(team, path, f"absent: the team has no {client} launcher")


def _check_https(report: Report, home: pathlib.Path, t: Team) -> None:
    ca_dir = app_root(home) / CA_DIR
    ca = ca_dir / f"{t.team}.pem"
    check = home / CHECK_DIR / "tls-trust-check"
    ready = True
    if _check_mode(report, t.team, ca, False, 0o644):
        if PEM_BEGIN not in ca.read_text(encoding="utf-8", errors="replace"):
            report.fail(t.team, ca, "holds no PEM certificate")
            ready = False
    else:
        ready = False
    found, why = _mode_of(check, False)
    if found is None or not found & stat.S_IXUSR:
        report.fail(t.team, check, why or "is not executable")
        ready = False
    else:
        report.ok(t.team, check, "the TLS trust check the play built")
    if ready:
        print(f"HANDSHAKE\t{t.team}\t{t.https_url}\t{ca_dir}\t{check.parent}\t{LIGHTNING.app_id}")


def check_team(report: Report, home: pathlib.Path, t: Team) -> None:
    if t.client != CLIENT_LIGHTNING:
        _check_no_launcher(report, home, t.team, CLIENT_LIGHTNING)
        return
    teams = app_root(home) / TEAMS_DIR
    for folder in (teams, teams / t.team, *(teams / t.team / name for name in INSTANCE_DIRS)):
        _check_mode(report, t.team, folder, True, DIR_MODE)
    _check_settings(report, t, teams / t.team / SETTINGS_IN)
    _check_launcher(report, home, t.team)
    _check_no_launcher(report, home, t.team, element.CLIENT_ELEMENT)
    if t.https_url is not None:
        _check_https(report, home, t)


def check(args: argparse.Namespace) -> int:
    home = pathlib.Path(args.home) if args.home else pathlib.Path(pwd.getpwuid(os.geteuid()).pw_dir)
    try:
        element.check_user(home)
    except element.ElementError as exc:
        print(f"refused: {exc}", file=sys.stderr)
        return EXIT_FAIL
    try:
        text = pathlib.Path(args.file).read_text(encoding="utf-8")
        teams = resolve(text, pathlib.Path(args.approved_root))
    except OSError as exc:
        print(f"COULD NOT ESTABLISH: the host vars {args.file} cannot be read ({exc.strerror})", file=sys.stderr)
        return EXIT_NOT_ESTABLISHED
    except CouldNotEstablish as exc:
        print(f"COULD NOT ESTABLISH: {exc}", file=sys.stderr)
        return EXIT_NOT_ESTABLISHED
    except Refused as exc:
        print(f"refused, as the play would refuse it: {exc}", file=sys.stderr)
        return EXIT_FAIL
    if not any(t.client == CLIENT_LIGHTNING for t in teams):
        print("COULD NOT ESTABLISH: no declared team opens in Lightning, so there is nothing to check",
              file=sys.stderr)
        return EXIT_NOT_ESTABLISHED
    report = Report()
    for t in teams:
        print(f"TEAM\t{t.team}\t{t.client}")
        check_team(report, home, t)
    return EXIT_FAIL if report.failed else EXIT_OK


def main(argv: Sequence[str] | None = None, *, euid: int | None = None) -> int:
    parser = argparse.ArgumentParser(prog="acceptance_lightning.py")
    commands = parser.add_subparsers(dest="command", required=True)
    sub = commands.add_parser("check", help="check every lightning team's instance and launcher")
    sub.add_argument("--file", required=True)
    sub.add_argument("--approved-root", required=True)
    sub.add_argument("--home")
    try:
        args = parser.parse_args(argv)
    except SystemExit as exc:
        return EXIT_OK if exc.code == 0 else EXIT_USAGE
    if (os.geteuid() if euid is None else euid) == 0:
        print("refused: run this as the desktop user, not root", file=sys.stderr)
        return EXIT_FAIL
    return check(args)


if __name__ == "__main__":
    sys.exit(main())
