"""The rule fixture-plain-member-bundle (Defence Before Fix; CLAUDE/QA.md).

A member bundle that the invoking user's pingbus loads is, in production, a SEAT: `agent-bus
seat take` places it in a directory of its own (the checkout's `seats/<team>/<seat>/`, 0700)
and the session home links `<home>/<team>` to it (helpers/pingbus/seat.py, `Keeper._link`).
pingbus judges the two shapes differently: helpers/pingbus/config.py `load_bundle` accepts a
`host` member beside the owner's Element profile only through that link. A checker that writes
the bundle with `agent-bus add-member --out=<home>/<team>` straight into the home it then runs
pingbus with (`PINGBUS_HOME=<home>`) tests a shape no session uses, and fails, or passes, for a
reason that is not the product's.

The rule reads bash rather than running it. A finding is an `add-member` whose `--out=` names
`<home>/<entry>` where `<home>` is a value the same program gives `PINGBUS_HOME` as a plain
assignment on the host (`env PINGBUS_HOME=...`, `PINGBUS_HOME=... cmd`). A program is a plan
folder (its scripts are sourced together) or one file elsewhere. `--setenv=PINGBUS_HOME=`
(systemd-run, a dedicated agent user's own home) and `-e PINGBUS_HOME=` (podman exec) are
not host runs by the invoking user and are not read. Names are followed through the program's
assignments (`X="${Y}/z"`), a few steps deep.

Fix a finding by placing the bundle in a directory of its own (add-member makes `--out` 0700,
owned by the user who ran sudo) and linking `<home>/<team>` to it with an absolute symlink, as
the keeper does.
"""

from __future__ import annotations

import os
import pathlib
import re
import textwrap
import unittest

RULE_ID = "fixture-plain-member-bundle"
REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
PLAN_DIR = "CLAUDE/Plan"
#: Active plan folders are the five-digit ones directly under CLAUDE/Plan: an archived plan
#: (Completed/, Cancelled/, Archive/) is not runnable through meta-deploy.
ACTIVE_PLAN = re.compile(r"^\d{5}-")
#: Where bash that adds a member and runs pingbus can live.
BASH_ROOTS = (PLAN_DIR, "scripts", "files", "tests")
SKIPPED_DIRS = frozenset({"__pycache__", ".venv", "venv", "vendor", "testdata", "subagent-reports", "JOURNAL",
                          "node_modules", ".git"})
#: How many assignments a name is followed through.
RESOLVE_DEPTH = 4

_VALUE = r"(\"[^\"]*\"|'[^']*'|[^\s;)|&]+)"
ADD_MEMBER = re.compile(r"(?<![\w-])add-member(?![\w-])")
OUT = re.compile(r"--out=" + _VALUE)
HOME = re.compile(r"(?<![\w=-])PINGBUS_HOME=" + _VALUE)
ASSIGN = re.compile(r"(?:^|[\s;(])(?:local\s+|readonly\s+|declare\s+(?:-\w+\s+)?)?([A-Za-z_]\w*)=" + _VALUE)
#: What, before `PINGBUS_HOME=`, makes it another process's environment, not the invoking
#: user's run on the host: `-e`/`--env` (podman, docker) and `--setenv=` (systemd-run; the
#: lookbehind already leaves `=PINGBUS_HOME` out).
OTHER_ENV = re.compile(r"(?:\s-e|\s--env)\s*[\"']?$")
_BRACED = re.compile(r"\$\{([A-Za-z_]\w*)\}")


def _is_bash(path: str, source: str) -> bool:
    if path.endswith((".bash", ".sh")):
        return True
    first = source.split("\n", 1)[0]
    return first.startswith("#!") and re.search(r"\b(?:ba)?sh\b", first) is not None


def logical_lines(source: str) -> list[tuple[int, str]]:
    """Code lines with their backslash continuations joined, each with its first line's number;
    whole-line comments are left out."""
    out: list[tuple[int, str]] = []
    pending: list[str] = []
    start = 0
    for number, line in enumerate(source.splitlines(), 1):
        if not pending:
            if line.lstrip().startswith("#"):
                continue
            start = number
        if line.endswith("\\"):
            pending.append(line[:-1])
            continue
        pending.append(line)
        out.append((start, " ".join(pending)))
        pending = []
    if pending:
        out.append((start, " ".join(pending)))
    return out


def normal(value: str) -> str:
    """A value as the rule compares it: unquoted, `${X}` written `$X`."""
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        value = value[1:-1]
    return _BRACED.sub(r"$\1", value).rstrip("/")


def assignments(sources: list[str]) -> dict[str, set[str]]:
    found: dict[str, set[str]] = {}
    for source in sources:
        for _, line in logical_lines(source):
            for match in ASSIGN.finditer(line):
                if match.group(1) != "PINGBUS_HOME":
                    found.setdefault(match.group(1), set()).add(normal(match.group(2)))
    return found


def forms(value: str, names: dict[str, set[str]]) -> set[str]:
    """The value and what it reads as through the program's assignments: a leading `$X` is
    replaced by each value X is given, a few steps deep."""
    seen = {value}
    frontier = {value}
    for _ in range(RESOLVE_DEPTH):
        nxt: set[str] = set()
        for item in frontier:
            match = re.match(r"\$([A-Za-z_]\w*)(.*)$", item)
            if match:
                for given in names.get(match.group(1), ()):
                    nxt.add(given + match.group(2))
        nxt -= seen
        if not nxt:
            break
        seen |= nxt
        frontier = nxt
    return seen


def homes(sources: list[str]) -> set[str]:
    found: set[str] = set()
    for source in sources:
        for _, line in logical_lines(source):
            for match in HOME.finditer(line):
                if not OTHER_ENV.search(line[:match.start()]):
                    found.add(normal(match.group(1)))
    return found


def detect(source: str, program: list[str]) -> list[int]:
    """The lines of `source` whose add-member writes its bundle into a pingbus home of
    `program` (every source of the program, `source` among them)."""
    names = assignments(program)
    home_forms = set().union(*(forms(h, names) for h in homes(program))) if homes(program) else set()
    found: list[int] = []
    for number, line in logical_lines(source):
        if not ADD_MEMBER.search(line):
            continue
        for match in OUT.finditer(line):
            out = normal(match.group(1))
            if out == "-":
                continue
            parents = {f.rsplit("/", 1)[0] for f in forms(out, names) if "/" in f}
            if any(forms(p, names) & home_forms for p in parents):
                found.append(number)
    return found


def _walk(root: str) -> list[pathlib.Path]:
    files: list[pathlib.Path] = []
    base = REPO_ROOT / root
    for dirpath, dirnames, filenames in os.walk(base):
        rel = pathlib.Path(dirpath).relative_to(REPO_ROOT).as_posix()
        if rel == PLAN_DIR:
            dirnames[:] = [d for d in dirnames if ACTIVE_PLAN.match(d)]
        dirnames[:] = [d for d in dirnames if d not in SKIPPED_DIRS]
        files.extend(p for p in (pathlib.Path(dirpath) / f for f in filenames) if not p.is_symlink())
    return files


def _text(path: pathlib.Path) -> str | None:
    try:
        return path.read_text(encoding="utf-8")
    except (UnicodeDecodeError, IsADirectoryError):
        return None


def plan_folder(rel: str) -> str | None:
    parts = rel.split("/")
    return "/".join(parts[:3]) if rel.startswith(PLAN_DIR + "/") and len(parts) > 3 else None


def bash_files() -> dict[str, str]:
    out: dict[str, str] = {}
    for root in BASH_ROOTS:
        for path in _walk(root):
            rel = path.relative_to(REPO_ROOT).as_posix()
            if rel.startswith("tests/helpers/plan_acceptance/"):
                continue
            source = _text(path)
            if source is not None and _is_bash(rel, source):
                out[rel] = source
    return out


def sweep() -> list[str]:
    files = bash_files()
    programs: dict[str, list[str]] = {}
    for rel, source in files.items():
        programs.setdefault(plan_folder(rel) or rel, []).append(source)
    found: list[str] = []
    for rel, source in sorted(files.items()):
        found.extend(f"{rel}:{n}: {RULE_ID}: a bundle pingbus loads from a home is a seat; give it a directory "
                     "of its own and link <home>/<team> to it"
                     for n in detect(source, programs[plan_folder(rel) or rel]))
    return found


# ── the fixtures: `ruleid` marks each line the rule must report, `ok` a near miss ──────────

#: Plan 00211's A3 as it failed on the host.
FIXTURE_A3 = textwrap.dedent('''\
    a3_take() {
        A3_HOME="${PLAN_RUN_DIR}/a3-pingbus"
        A3_DIR="${A3_HOME}/${QTEAM}"
        mkdir -m 0700 "${A3_HOME}" || return 1
        "${AGENT_BUS[@]}" add-member "${QTEAM}" --repo=fedora-desktop --seat="${RUN_TAG}" --host=acceptance \\
            --type=host --role=worker --address=127.0.0.1 --out="${A3_DIR}" || return 1  # ruleid
    }
    a3_check() {
        out="$(env -i PATH="${GUEST_PATH}" PINGBUS_HOME="${A3_HOME}" "${HOST_PINGBUS}" report)"
    }
''')

FIXTURE_DIRECT = textwrap.dedent('''\
    run_member() {
        sudo -n agent-bus add-member "${TEAM}" --type=podman --out="${HOME_DIR}/${TEAM}"  # ruleid
        PINGBUS_HOME="${HOME_DIR}" pingbus status
    }
''')

FIXTURE_NEAR_MISSES = textwrap.dedent('''\
    # the bundle in a seat directory of its own, linked into the home: the production shape
    a3_take() {
        A3_SEAT="${PLAN_RUN_DIR}/a3-seats/${QTEAM}/${RUN_TAG}"
        A3_HOME="${PLAN_RUN_DIR}/a3-pingbus"
        "${AGENT_BUS[@]}" add-member "${QTEAM}" --type=host --out="${A3_SEAT}" || return 1  # ok
        ln -s -- "${A3_SEAT}" "${A3_HOME}/${QTEAM}"
        PINGBUS_HOME="${A3_HOME}" pingbus report
    }
    # a dedicated agent user's own home, placed by that user: a plain bundle is production
    add_member() {
        sudo -n agent-bus add-member "${TEAM}" --type=host --out="${MEMBER_DIR}/a/${TEAM}"  # ok
        systemd-run --setenv=PINGBUS_HOME="${MEMBER_DIR}/a" pingbus recv
        podman exec -e PINGBUS_HOME="${MEMBER_DIR}/a" c1 pingbus recv
        podman exec -e "PINGBUS_HOME=${MEMBER_DIR}/a" c1 pingbus recv
    }
    take() {
        sudo -n agent-bus add-member "${TEAM}" --type=host --out=- | place  # ok
    }
''')

FIXTURE_ACROSS_FILES = (
    'A3_DIR="${A3_HOME}/${QTEAM}"\nagent-bus add-member t --out="${A3_DIR}"  # ruleid\n',
    'A3_HOME="${PLAN_RUN_DIR}/h"\nenv -i PINGBUS_HOME="${PLAN_RUN_DIR}/h" pingbus report\n',
)


def marked(source: str) -> list[int]:
    return [n for n, line in enumerate(source.splitlines(), 1) if re.search(r"# ruleid\b", line)]


class RuleProof(unittest.TestCase):
    """The rule fires on each fixture's `ruleid` lines and on nothing else."""

    def test_fires_on_the_originating_a3(self) -> None:
        self.assertEqual(detect(FIXTURE_A3, [FIXTURE_A3]), [5])
        self.assertEqual(marked(FIXTURE_A3), [6], "the mark sits on the continuation that carries --out")

    def test_fires_on_a_bundle_written_straight_into_a_home(self) -> None:
        self.assertEqual(detect(FIXTURE_DIRECT, [FIXTURE_DIRECT]), marked(FIXTURE_DIRECT))

    def test_passes_the_near_misses(self) -> None:
        self.assertEqual(detect(FIXTURE_NEAR_MISSES, [FIXTURE_NEAR_MISSES]), [])

    def test_a_program_is_its_plan_folder(self) -> None:
        first, second = FIXTURE_ACROSS_FILES
        self.assertEqual(detect(first, [first]), [])
        self.assertEqual(detect(first, [first, second]), marked(first))
        self.assertEqual(plan_folder("CLAUDE/Plan/00211-x/a.bash"), "CLAUDE/Plan/00211-x")
        self.assertIsNone(plan_folder("scripts/x.bash"))


class Sweep(unittest.TestCase):
    """No first-party bash writes a member bundle into the home it runs pingbus with."""

    def test_the_scan_finds_the_member_adding_bash(self) -> None:
        files = bash_files()
        for rel in ("CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage2.inc.bash",
                    "CLAUDE/Plan/00161-agent-team-bus-matrix/_acceptance-steps.inc.bash"):
            self.assertIn(rel, files)
            self.assertRegex(files[rel], ADD_MEMBER)

    def test_no_finding(self) -> None:
        found = [f"{line}; see CLAUDE/QA.md \"{RULE_ID}\"" for line in sweep()]
        self.assertEqual(found, [], "\n" + "\n".join(found))


if __name__ == "__main__":
    unittest.main()
