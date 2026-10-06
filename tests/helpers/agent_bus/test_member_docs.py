"""The member documentation's contract: docs/agent-bus.md, the kit's README per member
type and the README every bundle carries.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_member_docs

Plan 00161 DESIGN.md section 12, row U21: a kit README per member type, a bundle README per
type, and docs/agent-bus.md. Every command those texts name must exist: each `pingbus`,
`agent-bus` and `agent-bus-install` invocation in their code is parsed by the real command
line (the installer's by its own `--help`), so a made-up command or option fails here.
"""

from __future__ import annotations

import io
import pathlib
import re
import shlex
import subprocess
import sys
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.agent_bus import admin
from helpers.agent_bus import cli as admin_cli
from helpers.pingbus import cli as pingbus_cli
from helpers.pingbus import config, protocol

KIT = REPO_ROOT / "files" / "opt" / "claude-yolo" / "optional" / "agent-bus"
DOC = REPO_ROOT / "docs" / "agent-bus.md"
INSTALLER = REPO_ROOT / "files" / "usr" / "local" / "sbin" / "agent-bus-install"
DOCS_INDEX = REPO_ROOT / "docs" / "README.md"

#: The topics DESIGN.md's U21 row gives docs/agent-bus.md, each as the heading it is under.
DOC_SECTIONS = (
    "Teams",
    "Where a team's homeserver runs",
    "Installing a homeserver host",
    "Joining a team",
    "The dedicated agent user",
    "Joining a team hosted elsewhere",
    "Backups are the root of trust",
    "The human's account",
    "Limits",
)

#: The rest of an invocation in a code line: up to a shell operator. `<...>` is a
#: placeholder here, never a redirection.
_STOP = r"[^|;&`)]*"


def kit_readme(member_type: str) -> pathlib.Path:
    return KIT / f"README.{member_type}"


def code_lines(text: str) -> list[str]:
    """Every inline code span and every line of a fenced block, a block indented under a
    list item included."""
    block = r"^[ \t]*```[a-z]*\n(.*?)^[ \t]*```"
    fenced = re.findall(block, text, flags=re.M | re.S)
    outside = re.sub(block, "", text, flags=re.M | re.S)
    spans = re.findall(r"`([^`\n]+)`", outside)
    return spans + [line for block in fenced for line in block.splitlines()]


def invocations(text: str, command: str) -> list[list[str]]:
    """The arguments of every `command` in the code, up to a shell operator, with the
    synopsis brackets (`[--opt]`) read as the arguments they hold. A name inside a path
    (`~/.config/pingbus/env`, `.claude/ccy/pingbus/`) is not an invocation, but the
    installer is run by its path (`<clone>/files/usr/local/sbin/agent-bus-install`)."""
    found = []
    before = r"[\w.~$-]" if command == "agent-bus-install" else r"[\w/.~$-]"
    pattern = rf"(?<!{before}){re.escape(command)}(?![\w/.-])[ \t]+(\S{_STOP})"
    for line in code_lines(text):
        for match in re.finditer(pattern, line):
            words = match.group(1).replace("[", "").replace("]", "")
            found.append(shlex.split(words, comments=True))
    return found


def pingbus_parses(argv: list[str]) -> bool:
    try:
        pingbus_cli.build_parser(io.StringIO()).parse_args(argv)
    except config.UsageError:
        return False
    return True


def agent_bus_parses(argv: list[str]) -> bool:
    """The wrapper takes `--out=DIR` for itself (add-member, rotate-token) and hands the
    rest to the admin tool."""
    if argv and argv[0] in ("add-member", "rotate-token"):
        outs = [arg for arg in argv if arg.startswith("--out=")]
        if len(outs) != 1:
            return False
        argv = [arg for arg in argv if not arg.startswith("--out=")]
    try:
        admin_cli.build_parser(io.BytesIO()).parse_args(argv)
    except admin_cli.UsageError:
        return False
    return True


def installer_synopsis() -> dict[str, set[str]]:
    """Each installer command and the options its `--help` line names."""
    usage = subprocess.run(["bash", str(INSTALLER), "--help"], capture_output=True, text=True,
                           check=True).stdout
    commands: dict[str, set[str]] = {}
    for line in usage.splitlines():
        match = re.match(r"^  ([a-z][a-z-]*)( .*)?$", line)
        if match:
            commands[match.group(1)] = set(re.findall(r"--[a-z][a-z-]*", match.group(2) or ""))
    return commands


def installer_parses(argv: list[str], synopsis: dict[str, set[str]]) -> bool:
    if not argv or argv[0] not in synopsis:
        return False
    options = {arg.split("=", 1)[0] for arg in argv[1:] if arg.startswith("--")}
    return options <= synopsis[argv[0]]


def documents() -> dict[str, str]:
    texts = {str(DOC.relative_to(REPO_ROOT)): DOC.read_text(encoding="utf-8")}
    for member_type in protocol.HANDLE_TYPES:
        path = kit_readme(member_type)
        texts[str(path.relative_to(REPO_ROOT))] = path.read_text(encoding="utf-8")
        texts[f"bundle README ({member_type})"] = admin.bundle_readme(
            f"myrepo.1+workstation.{member_type}", "team-a", member_type)
    return texts


class KitReadmeTest(unittest.TestCase):
    def test_one_readme_per_member_type(self) -> None:
        found = sorted(path.name for path in KIT.glob("README*"))
        self.assertEqual(found, sorted(f"README.{t}" for t in protocol.HANDLE_TYPES))

    def test_each_readme_names_its_type_and_the_guide(self) -> None:
        for member_type in protocol.HANDLE_TYPES:
            text = kit_readme(member_type).read_text(encoding="utf-8")
            self.assertIn(f"--type={member_type}", text, member_type)
            self.assertIn("docs/agent-bus.md", text, member_type)

    def test_the_installer_ships_every_readme(self) -> None:
        source = INSTALLER.read_text(encoding="utf-8")
        match = re.search(r"^readonly KIT_FILES=\((.*?)\)", source, flags=re.M | re.S)
        self.assertIsNotNone(match)
        kit_files = match.group(1).replace("\\\n", " ").split()
        for member_type in protocol.HANDLE_TYPES:
            self.assertIn(f"README.{member_type}", kit_files)


class BundleReadmeTest(unittest.TestCase):
    def test_every_type_has_next_steps(self) -> None:
        self.assertEqual(set(admin.NEXT_STEPS), set(protocol.HANDLE_TYPES))

    def test_each_bundle_readme_points_at_its_kit_readme(self) -> None:
        for member_type in protocol.HANDLE_TYPES:
            handle = f"myrepo.1+workstation.{member_type}"
            text = admin.bundle_readme(handle, "team-a", member_type)
            self.assertIn(f"member {handle} of team team-a", text)
            self.assertIn(f"/usr/local/share/agent-bus/kit/README.{member_type}", text)
            self.assertIn("pingbus config check", text)
            self.assertNotIn("{team}", text)

    def test_a_bundle_carries_the_readme(self) -> None:
        # The tar add-member writes holds exactly what bundle_readme renders.
        self.assertIn(admin.README_FILE, admin.BUNDLE_FILES)


class CommandsExistTest(unittest.TestCase):
    def test_every_pingbus_invocation_parses(self) -> None:
        seen = 0
        for name, text in documents().items():
            for argv in invocations(text, "pingbus"):
                seen += 1
                self.assertTrue(pingbus_parses(argv), f"{name}: pingbus {shlex.join(argv)}")
        self.assertGreater(seen, 10)

    def test_every_agent_bus_invocation_parses(self) -> None:
        seen = 0
        for name, text in documents().items():
            for argv in invocations(text, "agent-bus"):
                seen += 1
                self.assertTrue(agent_bus_parses(argv), f"{name}: agent-bus {shlex.join(argv)}")
        self.assertGreater(seen, 10)

    def test_every_installer_invocation_is_in_its_help(self) -> None:
        synopsis = installer_synopsis()
        self.assertIn("software", synopsis)
        seen = 0
        for name, text in documents().items():
            for argv in invocations(text, "agent-bus-install"):
                seen += 1
                self.assertTrue(installer_parses(argv, synopsis),
                                f"{name}: agent-bus-install {shlex.join(argv)}")
        self.assertGreater(seen, 5)

    def test_the_checks_catch_made_up_commands(self) -> None:
        self.assertFalse(pingbus_parses(["peers"]))
        self.assertFalse(agent_bus_parses(["add-member", "t", "--repo=r", "--host=h",
                                           "--type=host", "--role=worker", "--address=192.0.2.1"]))
        self.assertFalse(agent_bus_parses(["human", "reset", "t", "n"]))
        synopsis = installer_synopsis()
        self.assertFalse(installer_parses(["team", "--team=x"], synopsis))
        self.assertFalse(installer_parses(["upgrade"], synopsis))

    def test_paths_are_not_invocations(self) -> None:
        text = "`~/.config/pingbus/env` and `/usr/local/bin/pingbus` and `sudo agent-bus list t`"
        self.assertEqual(invocations(text, "pingbus"), [])
        self.assertEqual(invocations(text, "agent-bus"), [["list", "t"]])
        by_path = "`sudo <clone>/files/usr/local/sbin/agent-bus-install software --source <clone>`"
        self.assertEqual(invocations(by_path, "agent-bus-install"),
                         [["software", "--source", "<clone>"]])
        self.assertEqual(invocations(by_path, "agent-bus"), [])


class GuideTest(unittest.TestCase):
    def setUp(self) -> None:
        self.text = DOC.read_text(encoding="utf-8")
        self.flat = " ".join(self.text.split())

    def test_every_topic_has_a_section(self) -> None:
        headings = re.findall(r"^#{2,3} (.+)$", self.text, flags=re.M)
        for section in DOC_SECTIONS:
            self.assertIn(section, headings)

    def test_every_member_type_is_covered(self) -> None:
        for member_type in protocol.HANDLE_TYPES:
            self.assertIn(f"README.{member_type}", self.text)

    def test_the_trust_statements(self) -> None:
        for statement in (
            "--no-human-text",
            "whoever holds a backup can instruct every agent in the team",
            "Root on the homeserver host can instruct every agent in the team",
            "travel in clear",
        ):
            self.assertIn(statement, self.flat)

    def test_every_human_action_is_named(self) -> None:
        for action in admin_cli.HUMAN_ACTIONS:
            self.assertIn(f"agent-bus human {action} ", self.text)

    def test_the_index_links_the_guide(self) -> None:
        self.assertIn("(agent-bus.md)", DOCS_INDEX.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
