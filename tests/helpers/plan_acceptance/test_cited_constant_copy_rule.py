"""The rule checker-copies-cited-constant (Defence Before Fix; CLAUDE/QA.md).

A plan checker (acceptance, triage, probe, or its test) judges a product. When it needs a value
the product owns (a command string, a content key, a message, a bound), it reads it from the
product's source. A literal copy drifts: Plan 00201's m7_check.py held `GUARD_TAIL = "exec
pingbus status-line"`, a copy of helpers/pingbus/seat.py STATUS_LINE_GUARD's tail, and when
Plan 00213 changed the guard to run `pingbus-status-line` the copy failed check M3 on a correct
session.

The rule reads code rather than running it. A finding is a constant of a checker in an active
plan folder, `tests/` or `scripts/` (Python: a module-level `NAME = <literal>`, or a literal
wrapped in one call such as `pathlib.Path("...")`; bash: a top-level `[readonly] NAME=`
with no expansion in its value) whose own comment (the comment lines directly above it, or a
trailing comment) names a product source file (under helpers/, src-go/internal/, files/ or
extensions/) together with a module-level constant that file defines: a token of the comment,
or the checker constant's own name. The comment is the author saying whose value it is, so the
value is the product's, copied.

The next wider rule, any checker literal equal to or inside a product constant's value, was
drafted and not built: it reports the Matrix spec's event types, GitHub's API address, Claude
Code's environment names and deployed install paths, which the product does not own either.

Its second clause needs no comment. OWNED names product constants a checker must never hold
as a literal: the session home, the membership mount, variable and file names, the status line's
inner-only variable, the checkout's seat directory, the team-name grammar, the agent-bus state
roots, the Tuwunel pin, ccy-teams' step marks and STATE words, and the message texts the product
names for its checkers. Each value is read from the product source when the rule runs. A finding
is a line of a checker in an active plan folder whose code (not a comment or docstring) holds the
value in the form its kind says: anywhere, as a file name after a `/`, as a directory between
two, as a mark a pattern anchors to a line start (`^✓`), or as a whole path (`/var/lib/agent-bus`
and below it, not `/var/lib/agent-bus-applied`). A text with placeholders is held when any fixed part of it, between
placeholders and long enough to be distinctive, is. The checkers' own unit tests (`test_*.py`) are outside this clause: their fixtures are
fake input, which cannot judge a product.

Fix a finding by reading the value from the product: import its module from this checkout (as
`m7_check.py` and `goodbye_check.py` do, lazily, by the repo root above the file), or, for bash,
ask a helper that does. A checker that runs where the checkout is not mounted is handed the
value by the script that starts it.
"""

from __future__ import annotations

import ast
import os
import pathlib
import re
import textwrap
import unittest

RULE_ID = "checker-copies-cited-constant"
REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
PLAN_DIR = "CLAUDE/Plan"
#: Active plan folders are the five-digit ones directly under CLAUDE/Plan: an archived plan
#: (Completed/, Cancelled/, Archive/) is not runnable through meta-deploy.
ACTIVE_PLAN = re.compile(r"^\d{5}-")
#: Where the product's source lives: the values a checker may cite as the product's.
SUBJECT_ROOTS = ("helpers", "src-go/internal", "files", "extensions")
SKIPPED_DIRS = frozenset({"__pycache__", ".venv", "venv", "vendor", "testdata", "subagent-reports", "JOURNAL",
                          "node_modules", ".git"})
SOURCE_FILE = re.compile(r"(?<![\w.-])((?:[\w.-]+/)*[\w.-]+\.(?:py|go))\b")
NAME_TOKEN = re.compile(r"\b[A-Za-z_]\w*\b")
BASH_CONSTANT = re.compile(r"^(?:readonly\s+|declare\s+-r\s+)?([A-Za-z_]\w*)=(\"[^\"$`]*\"|'[^']*'|[^\s\"'$`;()]+)"
                           r"\s*(?:#(.*))?$")
GO_BLOCK = re.compile(r"(?ms)^(?:const|var) \((.*?)^\)")
GO_BLOCK_NAME = re.compile(r"^\s*([A-Za-z_]\w*)\b(?:\s*,\s*[A-Za-z_]\w*)*(?:\s+[\w.*\[\]]+)?\s*=", re.M)
GO_SINGLE = re.compile(r"^(?:const|var) ([A-Za-z_]\w*)\b(?:\s+[\w.*\[\]]+)?\s*=", re.M)


def python_names(source: str) -> set[str]:
    """The module-level names a Python source assigns."""
    try:
        tree = ast.parse(source)
    except SyntaxError:
        return set()
    names: set[str] = set()
    for statement in tree.body:
        targets = (statement.targets if isinstance(statement, ast.Assign)
                   else [statement.target] if isinstance(statement, ast.AnnAssign) else [])
        names.update(target.id for target in targets if isinstance(target, ast.Name))
    return names


def go_names(source: str) -> set[str]:
    """The package-level constants and variables a Go source declares."""
    names = set(GO_SINGLE.findall(source))
    for block in GO_BLOCK.findall(source):
        names.update(GO_BLOCK_NAME.findall(block))
    return names


#: A product's sources: repo-relative path -> the constants it defines.
Subject = dict[str, set[str]]


def defined_in(subject: Subject, cited: str) -> set[str]:
    """The constants of every product file a comment's `cited` path names: the path itself, or
    any file it ends (`seat.py`, `pingbus/seat.py`)."""
    names: set[str] = set()
    for rel, defined in subject.items():
        if rel == cited or rel.endswith("/" + cited):
            names |= defined
    return names


def copies(constants: list[tuple[int, str, str]], subject: Subject) -> list[int]:
    """Of a checker's constants (line, name, its comment), the lines whose comment names a
    product file and a constant that file defines."""
    found = []
    for line, name, comment in constants:
        for cited in SOURCE_FILE.findall(comment):
            defined = defined_in(subject, cited)
            if defined and (name in defined or defined & set(NAME_TOKEN.findall(comment))):
                found.append(line)
                break
    return found


def _comment_above(lines: list[str], index: int) -> list[str]:
    above = []
    j = index - 1
    while j >= 0 and lines[j].lstrip().startswith("#"):
        above.insert(0, lines[j].lstrip().lstrip("#:").strip())
        j -= 1
    return above


def python_constants(source: str) -> list[tuple[int, str, str]]:
    """Module-level `NAME = <literal>` with its comment, as (line, name, comment)."""
    try:
        tree = ast.parse(source)
    except SyntaxError:
        return []
    lines = source.splitlines()
    out = []
    for statement in tree.body:
        if not isinstance(statement, (ast.Assign, ast.AnnAssign)) or statement.value is None:
            continue
        targets = statement.targets if isinstance(statement, ast.Assign) else [statement.target]
        if len(targets) != 1 or not isinstance(targets[0], ast.Name):
            continue
        value = statement.value
        if isinstance(value, ast.Call) and len(value.args) == 1 and not value.keywords:
            value = value.args[0]
        try:
            ast.literal_eval(value)
        except ValueError:
            continue
        index = statement.lineno - 1
        trailing = lines[statement.end_lineno - 1].split("#", 1)[1] if "#" in lines[statement.end_lineno - 1] else ""
        out.append((statement.lineno, targets[0].id, " ".join([*_comment_above(lines, index), trailing])))
    return out


def bash_constants(source: str) -> list[tuple[int, str, str]]:
    """Top-level `[readonly] NAME=<value with no expansion>` with its comment."""
    lines = source.splitlines()
    out = []
    for index, line in enumerate(lines):
        match = BASH_CONSTANT.match(line)
        if match and match.group(2) not in ('""', "''"):
            out.append((index + 1, match.group(1), " ".join([*_comment_above(lines, index), match.group(3) or ""])))
    return out


def _is_bash(path: str, source: str) -> bool:
    if path.endswith((".bash", ".sh")):
        return True
    first = source.split("\n", 1)[0]
    return first.startswith("#!") and re.search(r"\b(?:ba)?sh\b", first) is not None


def detect(path: str, source: str, subject: Subject) -> list[int]:
    if path.endswith(".py"):
        return copies(python_constants(source), subject)
    if _is_bash(path, source):
        return copies(bash_constants(source), subject)
    return []


def _walk(root: str) -> list[pathlib.Path]:
    files: list[pathlib.Path] = []
    for dirpath, dirnames, filenames in os.walk(REPO_ROOT / root):
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


def subject_sources() -> Subject:
    subject: Subject = {}
    for root in SUBJECT_ROOTS:
        for path in _walk(root):
            rel = path.relative_to(REPO_ROOT).as_posix()
            if rel.endswith("_test.go") or not rel.endswith((".py", ".go")):
                continue
            source = _text(path)
            if source is not None:
                subject[rel] = python_names(source) if rel.endswith(".py") else go_names(source)
    return subject


#: Where checkers live: the plan folders' acceptance, triage and probes, the test suites
#: (`tests/`) and the scripts' test suites (`scripts/test-*.bash` and their helpers).
CHECKER_ROOTS = (PLAN_DIR, "tests", "scripts")


def checker_files() -> dict[str, str]:
    out: dict[str, str] = {}
    for root in CHECKER_ROOTS:
        for path in _walk(root):
            rel = path.relative_to(REPO_ROOT).as_posix()
            if rel.startswith("tests/helpers/plan_acceptance/") or (root == PLAN_DIR and rel.count("/") <= 2):
                continue
            source = _text(path)
            if source is not None and (rel.endswith(".py") or _is_bash(rel, source)):
                out[rel] = source
    return out


#: How an owned value shows in a checker's code: anywhere; as a file name (after a `/`); as a
#: directory inside a path (between two); as a mark a pattern anchors to a line's start
#: (after a `^`); or as a whole path, not the head of a longer name (`/var/lib/agent-bus`, and
#: under it, but not `/var/lib/agent-bus-applied`).
ANYWHERE, FILE_NAME, DIRECTORY, LINE_START, PATH = "anywhere", "file name", "directory", "line start", "path"
#: The owned values, as (product file, constant, kind). The value is read from the file.
OWNED: tuple[tuple[str, str, str], ...] = (
    ("helpers/pingbus/seat.py", "SESSION_HOME", ANYWHERE),
    ("helpers/pingbus/membership.py", "CCY_MOUNT", ANYWHERE),
    ("helpers/pingbus/membership.py", "DESIRED_FILE", FILE_NAME),
    ("src-go/internal/teams/membership/desired.go", "RelaunchFile", FILE_NAME),
    ("src-go/internal/teams/membership/membership.go", "ccyRoot", DIRECTORY),
    ("src-go/internal/teams/membership/membership.go", "ccRoot", DIRECTORY),
    ("src-go/internal/pingbus/statusline/statusline.go", "innerVar", ANYWHERE),
    ("src-go/internal/teams/hot/hot.go", "markDone", LINE_START),
    ("src-go/internal/teams/hot/hot.go", "markFailed", LINE_START),
    ("helpers/pingbus/membership.py", "APPLIED_FILE", FILE_NAME),
    ("helpers/pingbus/membership.py", "MEMBERSHIP_VAR", ANYWHERE),
    ("helpers/pingbus/config.py", "ELEMENT_ACCEPTED_FILE", FILE_NAME),
    ("helpers/pingbus/statusline.py", "SESSIONS_DIR_TEMPLATE", ANYWHERE),
    ("helpers/pingbus/seat.py", "CHECKOUT_SEATS", ANYWHERE),
    ("helpers/pingbus/protocol.py", "TEAM_NAME_PATTERN", ANYWHERE),
    ("helpers/agent_bus/render.py", "STATE_ROOT", PATH),
    ("helpers/agent_bus/checkout.py", "APPLIED_DIR", PATH),
    ("helpers/agent_bus/health.py", "INSTALL_STATE_ROOT", PATH),
    ("helpers/agent_bus/declare.py", "PIN_FILE", ANYWHERE),
    ("src-go/internal/teams/hot/cc.go", "ccUnguarded", ANYWHERE),
    ("src-go/internal/teams/hot/remove.go", "removedHomeserverDown", ANYWHERE),
    ("helpers/pingbus/cli.py", "QUESTION_DUPLICATE", ANYWHERE),
)
#: A text constant with placeholders (`{name}` or a `%` verb) is matched by each fixed part
#: between them, its ends stripped; a part shorter than this is not distinctive enough.
MIN_TEXT_HEAD = 12
_PLACEHOLDER = re.compile(r"\{[a-z_]*\}|%[-+ #0-9.*]*[a-zA-Z]")


def python_constant_value(source: str, name: str) -> str | None:
    """The string a module-level `name = "..."` (or `name = pathlib.Path("...")`) assigns."""
    for statement in ast.parse(source).body:
        if (isinstance(statement, ast.Assign) and len(statement.targets) == 1
                and isinstance(statement.targets[0], ast.Name) and statement.targets[0].id == name):
            value = statement.value
            if isinstance(value, ast.Call) and len(value.args) == 1 and not value.keywords:
                value = value.args[0]
            if isinstance(value, ast.Constant) and isinstance(value.value, str):
                return value.value
    return None


def go_constant_value(source: str, name: str) -> str | None:
    """The string of the one `name = "..."` constant (single or in a block) a Go source has."""
    found = re.findall(rf'^(?:const\s+|\s+){re.escape(name)}(?:\s+string)?\s*=\s*"((?:[^"\\]|\\.)*)"', source, re.M)
    return found[0] if len(found) == 1 else None


def owned_needles(values: list[tuple[str, str]]) -> list[re.Pattern[str]]:
    """What a checker holding each (value, kind) would show: the text the clause looks for."""
    texts = []
    for value, kind in values:
        if kind == FILE_NAME:
            texts.append(re.escape("/" + value))
        elif kind == DIRECTORY:
            texts.append(re.escape(f"/{value}/"))
        elif kind == LINE_START:
            texts.append(re.escape("^" + value))
        elif kind == PATH:
            texts.append(re.escape(value) + r"(?![\w.-])")
        elif not _PLACEHOLDER.search(value):
            texts.append(re.escape(value))
        else:
            texts.extend(re.escape(part.strip()) for part in _PLACEHOLDER.split(value)
                         if len(part.strip()) >= MIN_TEXT_HEAD)
    return [re.compile(text) for text in texts]


def owned_values() -> list[tuple[str, str]]:
    """Each OWNED constant's value, read from this checkout; one that cannot be read fails."""
    out = []
    for rel, name, kind in OWNED:
        source = (REPO_ROOT / rel).read_text(encoding="utf-8")
        value = python_constant_value(source, name) if rel.endswith(".py") else go_constant_value(source, name)
        if not value:
            raise AssertionError(f"{rel} defines no string constant {name}: OWNED is stale")
        out.append((value, kind))
    return out


def _python_code_strings(source: str) -> list[tuple[int, str]]:
    """Every string literal of a Python source with its line, docstrings left out."""
    tree = ast.parse(source)
    docstrings = {id(node.body[0].value) for node in ast.walk(tree)
                  if isinstance(node, (ast.Module, ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef))
                  and node.body and isinstance(node.body[0], ast.Expr) and isinstance(node.body[0].value, ast.Constant)}
    return [(node.lineno, node.value) for node in ast.walk(tree)
            if isinstance(node, ast.Constant) and isinstance(node.value, str) and id(node) not in docstrings]


def detect_owned(path: str, source: str, needles: list[re.Pattern[str]]) -> list[int]:
    """The lines of a checker's code (no comment, no docstring) that hold an owned value."""
    if path.endswith(".py"):
        try:
            pieces = _python_code_strings(source)
        except SyntaxError:
            return []
    elif _is_bash(path, source):
        pieces = [(n, line) for n, line in enumerate(source.splitlines(), 1) if not line.lstrip().startswith("#")]
    else:
        return []
    return sorted({n for n, text in pieces if any(needle.search(text) for needle in needles)})


def _owned_scope(rel: str) -> bool:
    return rel.startswith(PLAN_DIR + "/") and not pathlib.PurePosixPath(rel).name.startswith("test_")


def sweep() -> list[str]:
    subject = subject_sources()
    needles = owned_needles(owned_values())
    found: list[str] = []
    for rel, source in sorted(checker_files().items()):
        lines = set(detect(rel, source, subject))
        if _owned_scope(rel):
            lines.update(detect_owned(rel, source, needles))
        found.extend(f"{rel}:{n}: {RULE_ID}: this value is the product's; read it from the product, never copy it"
                     for n in sorted(lines))
    return found


# ── the fixtures: `ruleid` marks each line the rule must report, `ok` a near miss ──────────

FIXTURE_SUBJECT: Subject = {
    "helpers/pingbus/seat.py": {"STATUS_LINE_GUARD", "ACTIVE_FLAG"},
    "helpers/pingbus/protocol.py": {"TEXT_KEY", "DESCRIPTION_MAX"},
    "helpers/pingbus/cli.py": {"DISCONNECT_TEXT"},
    "src-go/internal/fetchage/fetchage.go": {"FailedMark"},
    "helpers/agent_bus/declare.py": {"PIN_FILE"},
}

#: Plan 00201's m7_check.py as it failed check M3, and its neighbours.
FIXTURE_PYTHON = textwrap.dedent('''\
    #: How the shell guard a seated ccy session's settings name ends (helpers/pingbus/seat.py
    #: STATUS_LINE_GUARD, Plan 00203 H34): with the `active` flag, `pingbus status-line` runs.
    GUARD_TAIL = "exec pingbus status-line"  # ruleid
    #: helpers/pingbus/protocol.py DESCRIPTION_MAX; the installer applies the full rule again.
    DESCRIPTION_MAX = 120  # ruleid
    #: What the host says of a session that ended without one (helpers/pingbus/cli.py).
    DISCONNECT_TEXT = "{handle} disconnected: session ended without a goodbye"  # ruleid
    TEXT_KEY = "agent_bus.text"  # ruleid: protocol.py TEXT_KEY
    #: The pin agent-bus-install installs (declare.py's PIN_FILE).
    PIN = pathlib.PurePosixPath("files/usr/local/share/agent-bus/tuwunel.pin")  # ruleid
    #: Each status line command is given this long before the leg calls it hung.
    STATUS_LINE_TIMEOUT_S = 30  # ok: the checker's own
    #: The guard, read from helpers/pingbus/seat.py STATUS_LINE_GUARD.
    GUARD = seat.STATUS_LINE_GUARD  # ok: read, not copied
    #: A documentation network (CLAUDE/ExampleValues.md), as helpers/pingbus/seat.py never says.
    ALLOW_FROM = "192.0.2.0/24"  # ok: the cited file defines no name the comment gives
    #: The bus's content prefix (docs/agent-bus-protocol.md PREFIX).
    PREFIX = "agent_bus"  # ok: a document, not a product source
''')

FIXTURE_BASH = textwrap.dedent('''\
    # The mark the prompt prints for a failed fetch (fetchage.go FailedMark).
    FAILED_MARK=' [fetch failed or running]'  # ruleid
    readonly TEXT_KEY="agent_bus.text"  # ruleid: helpers/pingbus/protocol.py
    # The session home (helpers/pingbus/seat.py SESSION_HOME), from the launcher's own variable.
    HOME_DIR="${CCY_PINGBUS_HOME}"  # ok: an expansion, not a literal copy
        INDENTED_TEXT_KEY="agent_bus.text"  # ok: protocol.py TEXT_KEY, not top level
    # The guard, set by the reader from helpers/pingbus/seat.py STATUS_LINE_GUARD.
    GUARD=""  # ok: empty until read, not a copy
''')


#: The owned values the fixtures are judged by, in the product's own forms.
FIXTURE_OWNED = [("/tmp/pingbus-home", ANYWHERE), ("membership.json", FILE_NAME), ("✓", LINE_START),
                 ("pingbus/membership", DIRECTORY), ("/var/lib/agent-bus", PATH),
                 ("PINGBUS_STATUS_LINE_INNER", ANYWHERE),
                 ("several teams are active and --to/--re fit more than one", ANYWHERE),
                 ("{unit} is starting ({state})", ANYWHERE), ("{n} ok", ANYWHERE)]

#: Plan 00203's M4, m2-check.bash and _triage-hs.inc.bash as the Task 2.22 sweep found them.
FIXTURE_OWNED_BASH = textwrap.dedent('''\
    # The session home, /tmp/pingbus-home, is seat.py's.
    readonly HOME_IN=/tmp/pingbus-home  # ruleid
    local file="${XDG_RUNTIME_DIR:?}/ccy/membership/$1/membership.json"  # ruleid
    m4_said role '^✓ role: [^ ]+ set to coordinator$' || return 1  # ruleid
    line="$(podman exec -i -e PINGBUS_STATUS_LINE_INNER=1 ctr pingbus status-line)"  # ruleid
    grep -F 'is starting (' "${err}"  # ruleid: a fixed part between the placeholders
    grep -c ' ok$' "${err}"  # ok: the text's only fixed part is too short to be a copy
    grep -F 'several teams are active and --to/--re fit more than one' "${err}"  # ruleid
    printf '✓ %s\\n' "$1"  # ok: the checker's own mark, not anchored as a step's
    grep -oE '^[✓✗⚠] [a-z]+: ' "${capture}"  # ok: a character class, not the mark
    grep -qP "^\\\\Q${M3_MARK_DONE}\\\\E re-baseline" "${err}"  # ok: read through m7_check
    cp "${src}/membership.json.bak" .  # ruleid: still the file name after a slash
    echo "membership.json alone"  # ok: no slash, a word of a message
    m6_membership() { printf '%s/pingbus/membership/%s' "${XDG_RUNTIME_DIR:?}" "$1"; }  # ruleid
    echo "the pingbus/membership tree"  # ok: not inside a path
    readonly STATE_ROOT=/var/lib/agent-bus  # ruleid
    sudo ls "/var/lib/agent-bus/${team}"  # ruleid: a path under it
    sudo ls /var/lib/agent-bus-applied  # ok: another name, which has its own entry
''')

FIXTURE_OWNED_PYTHON = textwrap.dedent('''\
    """Reads /tmp/pingbus-home/watch.log, as the docstring may say."""
    HOME = "/tmp/pingbus-home"  # ruleid
    # "/tmp/pingbus-home" in a comment is not code.
    def f():
        """membership.json under /run/ccy/membership, in a docstring."""
        return pathlib.Path(seat.SESSION_HOME) / "watch.log"  # ok: read
    WANT = f"{team}/membership.json"  # ruleid
''')


def marked(source: str) -> list[int]:
    return [n for n, line in enumerate(source.splitlines(), 1) if re.search(r"# ruleid\b", line)]


class RuleProof(unittest.TestCase):
    """The rule fires on each fixture's `ruleid` lines and on nothing else."""

    def test_fires_on_the_originating_m7_check(self) -> None:
        self.assertEqual(detect("m7_check.py", FIXTURE_PYTHON, FIXTURE_SUBJECT), marked(FIXTURE_PYTHON))
        self.assertIn(3, marked(FIXTURE_PYTHON), "GUARD_TAIL, the copy M3 failed on")

    def test_fires_on_bash(self) -> None:
        self.assertEqual(detect("x.bash", FIXTURE_BASH, FIXTURE_SUBJECT), marked(FIXTURE_BASH))

    def test_reads_the_product_names(self) -> None:
        self.assertEqual(python_names("A = 1\nB: int = 2\ndef f():\n    C = 3\n"), {"A", "B"})
        self.assertEqual(go_names("package x\nconst One = \"a\"\nconst (\n\tTwo = \"b\"\n\tthree string = \"c\"\n)\n"),
                         {"One", "Two", "three"})

    def test_owned_values_fire_on_bash(self) -> None:
        needles = owned_needles(FIXTURE_OWNED)
        self.assertEqual(detect_owned("x.inc.bash", FIXTURE_OWNED_BASH, needles), marked(FIXTURE_OWNED_BASH))

    def test_owned_values_fire_on_python(self) -> None:
        needles = owned_needles(FIXTURE_OWNED)
        self.assertEqual(detect_owned("x_check.py", FIXTURE_OWNED_PYTHON, needles), marked(FIXTURE_OWNED_PYTHON))

    def test_owned_scope_leaves_out_unit_tests_and_other_trees(self) -> None:
        self.assertTrue(_owned_scope("CLAUDE/Plan/00203-x/_acceptance-m4.inc.bash"))
        self.assertFalse(_owned_scope("CLAUDE/Plan/00203-x/test_m2_procs.py"))
        self.assertFalse(_owned_scope("tests/helpers/pingbus/test_seat.py"))

    def test_owned_values_are_read_from_the_product(self) -> None:
        values = dict(owned_values())
        self.assertIn("/tmp/pingbus-home", values)
        self.assertEqual(values["✓"], LINE_START)
        self.assertEqual(python_constant_value('import pathlib\nH = pathlib.Path("/x/y")\n', "H"), "/x/y")
        self.assertEqual(go_constant_value('const (\n\tmarkDone   = "✓"\n)\n', "markDone"), "✓")
        self.assertIsNone(go_constant_value('const (\n\ta = "1"\n)\nconst a = "2"\n', "a"))

    def test_the_product_is_read(self) -> None:
        subject = subject_sources()
        self.assertIn("STATUS_LINE_GUARD", subject["helpers/pingbus/seat.py"])
        self.assertIn("FailedMark", subject["src-go/internal/fetchage/fetchage.go"])


class Sweep(unittest.TestCase):
    """No active plan checker holds a copy of a product constant it names."""

    def test_the_scan_finds_the_checkers(self) -> None:
        files = checker_files()
        for rel in ("CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py",
                    "CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage2.inc.bash"):
            self.assertIn(rel, files)

    def test_no_finding(self) -> None:
        found = [f"{line}; see CLAUDE/QA.md \"{RULE_ID}\"" for line in sweep()]
        self.assertEqual(found, [], "\n" + "\n".join(found))


if __name__ == "__main__":
    unittest.main()
