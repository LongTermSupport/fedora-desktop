"""The rule offline-member-blocks (Defence Before Fix; CLAUDE/QA.md).

A declared team member whose container or session is not running is OFFLINE, and offline is
normal lifecycle: members go offline and come back. Every play, deploy, acceptance and tool
names such a member as offline and carries on with the rest; none fails, refuses or asks the
owner to start it (CLAUDE/AgentNotes.md, "An offline team member never blocks anything"). The
rule reads code rather than running it, with three detectors:

A. Bash that reads the declared LXC members (it names `agent_bus_lxc_members` or MEMBERS_JSON),
   in an active plan folder, `files/` or `scripts/`: an LXC state read of its own (`lxc-info -s`,
   or a `${LXC_INFO[@]}` call) is a finding, because the state must come from `plan_lxc_online`
   (CLAUDE/Plan/_members.inc.bash), which names an offline container and leaves it out but never
   fails on one; and a file that enters a container (`lxc-attach -n`, or a `${LXC_ATTACH[@]}`
   call) is a finding at its first entry unless its program calls `plan_lxc_online`: the file
   itself, or another file of its plan folder (a plan's scripts are sourced together).
B. In team-member code (the agent-bus and pingbus helpers, ccy-teams' packages, the agent-bus
   sbin tools, and the member-reading bash of A), a message that makes starting a member the way
   on: "start it, then ...", "restart that session, then ...", "Start container ...", "... with
   the container running".
C. Go under src-go/internal/teams: a flow that collects its LXC member rows into a list
   (`case row.Kind == kindLxc:` then `p.<list> = append(p.<list>, ...)`) and returns
   `term.ExitFailure` on that list being non-empty, so a stopped container fails the run.

Fix a finding by asking `plan_lxc_online` (bash) or the container list or member record (Go,
Python) which members are online, naming each offline one as "<member> is offline (container
stopped)" and judging only the online ones; a check that needs an online member and has none
says NOT JUDGED and does not fail. A running member's correctness is still checked in full.
"""

from __future__ import annotations

import os
import pathlib
import re
import textwrap
import unittest

RULE_ID = "offline-member-blocks"
REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
PLAN_DIR = "CLAUDE/Plan"
#: Active plan folders are the five-digit ones directly under CLAUDE/Plan: an archived plan
#: (Completed/, Cancelled/, Archive/) is not runnable through meta-deploy.
ACTIVE_PLAN = re.compile(r"^\d{5}-")
#: Where member-reading bash can live (detectors A and B).
BASH_ROOTS = (PLAN_DIR, "files", "scripts")
#: Team-member code in every language (detector B).
MEMBER_CODE_ROOTS = ("helpers/agent_bus", "helpers/agent_bus_lxc", "helpers/pingbus", "src-go/internal/teams",
                     "src-go/internal/declare", "src-go/internal/agentbus", "src-go/internal/pingbus")
MEMBER_SBIN = "files/usr/local/sbin"
#: Detector C's scope.
TEAMS_GO = "src-go/internal/teams"
SKIPPED_DIRS = frozenset({"__pycache__", ".venv", "venv", "vendor", "testdata", "subagent-reports", "JOURNAL",
                          "node_modules"})

MEMBER_LIST = re.compile(r"agent_bus_lxc_members|MEMBERS_JSON")
STATE_READ = re.compile(r"\blxc-info\b[^\n#]*\s-s|\$\{LXC_INFO\[@\]\}")
#: A container entered: an lxc-attach run, not the word in a message or an array being set.
CONTAINER_ENTRY = re.compile(r"\blxc-attach -n\b|\$\{LXC_ATTACH\[@\]\}")
HELPER_CALL = re.compile(r"\bplan_lxc_online\b")
#: The owner told to start a member, or restart its session, to go on ("restart it" alone is
#: also what a supervisor does, so only a session object counts for restart).
START_TO_GO_ON = re.compile(
    r"\bstart (?:it|them|the container|its container|its session|the session)(?:,| \(| then\b)"
    r"|\brestart (?:that session|its session|that agent's session|the session)(?:,| \(| then\b)"
    r"|\bstart container\b"
    r"|\b(?:with|while) (?:it|the container|its container) (?:is )?running\b",
    re.IGNORECASE)
LXC_CASE = re.compile(r"case row\.Kind == kindLxc:")
APPEND_TO = re.compile(r"\bp\.(\w+) = append\(p\.\1\b")
FAILING_ON = re.compile(r"\bif\b[^\n{]*\blen\(p\.(\w+)\) > 0[^\n{]*\{")


def _is_comment(line: str) -> bool:
    stripped = line.lstrip()
    return stripped.startswith("#") or stripped.startswith("//")


def _is_bash(path: str, source: str) -> bool:
    if path.endswith((".bash", ".sh")):
        return True
    first = source.split("\n", 1)[0]
    return first.startswith("#!") and re.search(r"\b(?:ba)?sh\b", first) is not None


def reads_members(path: str, source: str) -> bool:
    return _is_bash(path, source) and MEMBER_LIST.search(source) is not None


def detector_a(source: str, program_asks: bool = False) -> list[int]:
    """`program_asks`: another file of the same program (a plan folder's scripts are sourced
    together) calls plan_lxc_online, so this file's entries are of containers it listed."""
    found: list[int] = []
    first_entry = None
    for number, line in enumerate(source.splitlines(), 1):
        if _is_comment(line):
            continue
        if STATE_READ.search(line):
            found.append(number)
        if first_entry is None and CONTAINER_ENTRY.search(line):
            first_entry = number
    if first_entry is not None and not program_asks and HELPER_CALL.search(source) is None:
        found.append(first_entry)
    return sorted(set(found))


def detector_b(source: str) -> list[int]:
    return [number for number, line in enumerate(source.splitlines(), 1)
            if not _is_comment(line) and START_TO_GO_ON.search(line)]


def detector_c(sources: dict[str, str]) -> dict[str, list[int]]:
    """Over one Go package (file name -> source): the lines returning ExitFailure on a list
    the package fills with its LXC member rows."""
    lxc_lists: set[str] = set()
    for source in sources.values():
        lines = source.splitlines()
        for i, line in enumerate(lines):
            if LXC_CASE.search(line) and i + 1 < len(lines):
                match = APPEND_TO.search(lines[i + 1])
                if match:
                    lxc_lists.add(match.group(1))
    found: dict[str, list[int]] = {}
    for name, source in sources.items():
        lines = source.splitlines()
        for i, line in enumerate(lines):
            match = FAILING_ON.search(line)
            if (match and i + 1 < len(lines) and "return term.ExitFailure" in lines[i + 1]
                    and any(f"len(p.{x}) > 0" in line for x in lxc_lists)):
                found.setdefault(name, []).append(i + 1)
    return found


def _walk(root: str) -> list[pathlib.Path]:
    files: list[pathlib.Path] = []
    base = REPO_ROOT / root
    if base.is_file():
        return [base]
    for dirpath, dirnames, filenames in os.walk(base):
        rel = pathlib.Path(dirpath).relative_to(REPO_ROOT).as_posix()
        if rel == PLAN_DIR:
            dirnames[:] = [d for d in dirnames if ACTIVE_PLAN.match(d)]
        dirnames[:] = [d for d in dirnames if d not in SKIPPED_DIRS]
        # A symbolic link is the file it names, scanned where that lives (or nowhere, dangling).
        files.extend(p for p in (pathlib.Path(dirpath) / f for f in filenames) if not p.is_symlink())
    return files


def _text(path: pathlib.Path) -> str | None:
    try:
        return path.read_text(encoding="utf-8")
    except (UnicodeDecodeError, IsADirectoryError):
        return None


def _is_test(rel: str) -> bool:
    return rel.endswith("_test.go") or rel.startswith("tests/")


def member_bash_files() -> dict[str, str]:
    out: dict[str, str] = {}
    for root in BASH_ROOTS:
        for path in _walk(root):
            rel = path.relative_to(REPO_ROOT).as_posix()
            source = _text(path)
            if source is not None and not _is_test(rel) and reads_members(rel, source):
                out[rel] = source
    return out


def member_code_files() -> dict[str, str]:
    out: dict[str, str] = {}
    for root in MEMBER_CODE_ROOTS:
        for path in _walk(root):
            rel = path.relative_to(REPO_ROOT).as_posix()
            source = _text(path)
            if source is not None and rel.endswith((".py", ".go")) and not _is_test(rel):
                out[rel] = source
    for path in _walk(MEMBER_SBIN):
        rel = path.relative_to(REPO_ROOT).as_posix()
        source = _text(path)
        if source is not None and path.name.startswith("agent-bus-"):
            out[rel] = source
    return out


def teams_packages() -> dict[str, dict[str, str]]:
    packages: dict[str, dict[str, str]] = {}
    for path in _walk(TEAMS_GO):
        rel = path.relative_to(REPO_ROOT).as_posix()
        source = _text(path)
        if source is not None and rel.endswith(".go") and not _is_test(rel):
            packages.setdefault(path.parent.relative_to(REPO_ROOT).as_posix(), {})[rel] = source
    return packages


def plan_folder(rel: str) -> str | None:
    parts = rel.split("/")
    return "/".join(parts[:3]) if rel.startswith(PLAN_DIR + "/") and len(parts) > 3 else None


def folders_asking() -> set[str]:
    """The active plan folders one of whose files calls plan_lxc_online."""
    asking: set[str] = set()
    for path in _walk(PLAN_DIR):
        rel = path.relative_to(REPO_ROOT).as_posix()
        folder = plan_folder(rel)
        source = _text(path)
        if folder is not None and source is not None and HELPER_CALL.search(source):
            asking.add(folder)
    return asking


def sweep() -> list[str]:
    found: list[str] = []
    bash = member_bash_files()
    asking = folders_asking()
    for rel, source in sorted(bash.items()):
        found.extend(f"{rel}:{n}: {RULE_ID} (A): read the declared LXC members' state through plan_lxc_online"
                     for n in detector_a(source, plan_folder(rel) in asking))
    for rel, source in sorted({**member_code_files(), **bash}.items()):
        found.extend(f"{rel}:{n}: {RULE_ID} (B): an offline member is named and passed over, never a reason "
                     "to start it" for n in detector_b(source))
    for package in sorted(teams_packages().values(), key=lambda p: sorted(p)[0]):
        for rel, lines in sorted(detector_c(package).items()):
            found.extend(f"{rel}:{n}: {RULE_ID} (C): an offline LXC member must not fail the run" for n in lines)
    return found


# ── the fixtures: `ruleid` marks each line the rule must report, `ok` a near miss ──────────

FIXTURE_BASH = textwrap.dedent('''\
    # reads agent_bus_lxc_members into MEMBERS_JSON
    s2_check() {
        while IFS= read -r name; do
            state="$("${LXC_INFO[@]}" -n "${name}" -sH)" || return 1  # ruleid
            state="$(sudo -n lxc-info -n "${name}" -sH)"  # ruleid
            printf 'container %s is down: start it, then run meta-deploy again\\n' "${name}" >&2  # ruleid
            printf 'run the play with the container running\\n' >&2  # ruleid
            printf 'team %s: restart that session, then run meta-deploy again\\n' "$1" >&2  # ruleid
            # a comment may say: start it, then run meta-deploy again
            printf '%s is offline (container stopped): not judged\\n' "${name}"  # ok
            got="$("${LXC_ATTACH[@]}" -n "${name}" -- pingbus version)"  # ruleid
            got="$(sudo -n lxc-attach -n "${name}" -- pingbus version)"  # ok: the first entry is reported
        done
    }
''')

FIXTURE_BASH_WITH_HELPER = textwrap.dedent('''\
    # reads MEMBERS_JSON
    online="$(plan_lxc_online "${containers}")" || return 1
    got="$("${LXC_ATTACH[@]}" -n "${name}" -- pingbus version)"  # ok: the file asks plan_lxc_online
''')

FIXTURE_PYTHON = textwrap.dedent('''\
    def deferred_why(member, state):
        return (f"LXC container {member.container} is {state}: start it, then run the play again")  # ruleid
    def offline_why(member):
        return f"{member.label} is offline (container stopped): it joins on the first run with it online"  # ok
    RESTART = ("restart that agent's session (`invite`: it is not running), then run the play")  # ruleid
''')

FIXTURE_GO = {
    "a/plan.go": textwrap.dedent('''\
        package a
        func planFor() {
            switch {
            case row.Kind == kindLxc:
                p.unreached = append(p.unreached, unreached{row.Handle, whyLxc})
            }
        }
        const advice = "Start container " + name // ruleid
    '''),
    "a/report.go": textwrap.dedent('''\
        package a
        func report(p plan) term.ExitCode {
            if failedN > 0 || len(p.unreached) > 0 { // ruleid
                return term.ExitFailure
            }
            if len(p.leftOut) > 0 { // ok: not the LXC rows' list
                return term.ExitFailure
            }
            return term.ExitOK
        }
    '''),
}

FIXTURE_GO_LEFT_OUT = {
    "b/plan.go": textwrap.dedent('''\
        package b
        func planFor() {
            switch {
            case row.Kind == kindLxc:
                p.leftOut = append(p.leftOut, leftOut{row.Handle, whyLxc})
            }
            if failedN > 0 { // ok: the LXC rows are left out, never a failure
                return term.ExitFailure
            }
        }
    '''),
}


def marked(source: str) -> list[int]:
    return [n for n, line in enumerate(source.splitlines(), 1) if re.search(r"(#|//) ruleid\b", line)]


class RuleProof(unittest.TestCase):
    """Each detector fires on its fixture's `ruleid` lines and on nothing else."""

    def test_a_and_b_fire_on_the_bash_fixture(self) -> None:
        self.assertTrue(reads_members("x.bash", FIXTURE_BASH))
        self.assertEqual(sorted(set(detector_a(FIXTURE_BASH)) | set(detector_b(FIXTURE_BASH))),
                         marked(FIXTURE_BASH))
        self.assertEqual(len(marked(FIXTURE_BASH)), 6)

    def test_a_is_satisfied_by_the_shared_helper(self) -> None:
        self.assertEqual(detector_a(FIXTURE_BASH_WITH_HELPER), [])
        # Asked elsewhere in its plan folder: the entry is of a listed container; the state reads
        # are still findings.
        self.assertEqual(detector_a(FIXTURE_BASH, program_asks=True), [4, 5])
        self.assertEqual(plan_folder("CLAUDE/Plan/00211-x/sub/a.bash"), "CLAUDE/Plan/00211-x")
        self.assertIsNone(plan_folder("files/usr/local/bin/x.bash"))

    def test_b_fires_on_the_python_fixture(self) -> None:
        self.assertEqual(detector_b(FIXTURE_PYTHON), marked(FIXTURE_PYTHON))
        self.assertEqual(len(marked(FIXTURE_PYTHON)), 2)

    def test_b_and_c_fire_on_the_go_fixture(self) -> None:
        got = {name: sorted(set(detector_b(src)) | set(detector_c(FIXTURE_GO).get(name, [])))
               for name, src in FIXTURE_GO.items()}
        self.assertEqual(got, {name: marked(src) for name, src in FIXTURE_GO.items()})
        self.assertEqual(sum(len(v) for v in got.values()), 2)

    def test_c_passes_lxc_rows_that_are_only_left_out(self) -> None:
        self.assertEqual(detector_c(FIXTURE_GO_LEFT_OUT), {})


class Sweep(unittest.TestCase):
    """No first-party code treats a declared but offline team member as a failure."""

    def test_the_scan_finds_the_member_code(self) -> None:
        bash = member_bash_files()
        code = member_code_files()
        for rel in ("CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage1.inc.bash",
                    "CLAUDE/Plan/00213-agent-bus-performance-go-carve-out/_acceptance.inc.bash"):
            self.assertIn(rel, bash)
        for rel in ("helpers/agent_bus_lxc/members.py", "helpers/agent_bus/admin.py",
                    "src-go/internal/teams/winddown/winddown.go", "files/usr/local/sbin/agent-bus-install"):
            self.assertIn(rel, code)
        self.assertIn(f"{TEAMS_GO}/shutdown", teams_packages())

    def test_no_finding(self) -> None:
        found = [f"{line}; see CLAUDE/QA.md \"{RULE_ID}\"" for line in sweep()]
        self.assertEqual(found, [], "\n" + "\n".join(found))


if __name__ == "__main__":
    unittest.main()
