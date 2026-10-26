"""The rule chooser-skip-condition-text (Defence Before Fix; CLAUDE/QA.md).

`ccy --teams` with no value (and `cc --teams`) asks `ccy-teams choose-teams`, which skips its
team list only when the machine has exactly one team, it covers the checkout and it is up
(Plan 00201 SPEC-many-teams.md Q5 and S-B2, src-go/internal/teams/chooser/list.go `take`).
With two or more teams the list is always shown, covering teams first.

Plan 00177 Task 7.1, Plan 00201's ACCEPTANCE-owner.md step 5 and docs/agent-bus.md said a
team is taken without a list when it is the one team that covers the checkout. The host run
of 26-10-26 read the correct list as a failure (Plan 00177 Task 11.1).

The rule reads text rather than running anything. A finding is a sentence of a text a person
or agent follows (docs/, an active plan's markdown outside JOURNAL/ and subagent-reports/, the
launchers and the Go usage texts) that says a team is taken or chosen without a list (or
alone), names covering or matching the checkout, and does not say the machine has one team.
docs/ccy-changelog.md is history, as JOURNAL/ is, and is not judged.

The next wider rule, any sentence about the chooser that disagrees with S-B2 in any respect
(the order of the rows, what Esc does), was not built: those are other facts, and the prose
for them has no common shape a reader of text can judge.

Fix a finding by stating the real condition in the sentence: the machine's only team, when it
covers the checkout, is taken without a list.
"""

from __future__ import annotations

import pathlib
import re
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]

RULE_ID = "chooser-skip-condition-text"

#: The texts judged: globs under the repo root.
SCOPE = (
    "docs/**/*.md",
    "CLAUDE/Plan/[0-9]*/**/*.md",
    "src-go/**/usage*.txt",
    "files/var/local/claude-yolo/claude-yolo",
    "files/var/local/claude-code/*.bash",
)

#: Path parts that are history or point-in-time reports, not texts a reader follows.
HISTORY = ("JOURNAL", "subagent-reports")
HISTORY_FILES = ("docs/ccy-changelog.md",)

_SKIPS_LIST = re.compile(
    r"without (?:a|the|any) (?:team )?list"
    r"|no list is shown"
    r"|skips? the (?:team )?list"
    r"|(?:chos(?:en|e)|choos(?:ing|es)|taken|tak(?:es|ing) it) alone",
    re.IGNORECASE,
)
_COVERS = re.compile(r"\bcover(?:s|ing)?\b|\bmatch(?:es|ing)?\b", re.IGNORECASE)
_TEAM = re.compile(r"\bteams?\b", re.IGNORECASE)
_MACHINE_HAS_ONE = re.compile(
    r"machine has (?:exactly |only )?one (?:present )?team"
    r"|(?:only|one) team on (?:the|this) machine"
    r"|machine's (?:only|one) team",
    re.IGNORECASE,
)
_SENTENCE_END = re.compile(r"(?<=[.!?])\s+(?=[A-Z`*\"(])")


def sentences(text: str) -> list[str]:
    """The text's sentences, each paragraph's lines joined first so a wrapped sentence is whole."""
    out: list[str] = []
    for paragraph in re.split(r"\n\s*\n", text):
        joined = " ".join(line.strip().lstrip("#>-*| ").strip() for line in paragraph.splitlines())
        out.extend(s for s in _SENTENCE_END.split(joined) if s)
    return out


def findings_in(text: str) -> list[str]:
    """The sentences of text that state the chooser's skip condition without the machine's one team."""
    return [
        s
        for s in sentences(text)
        if _SKIPS_LIST.search(s) and _COVERS.search(s) and _TEAM.search(s) and not _MACHINE_HAS_ONE.search(s)
    ]


def judged_files(root: pathlib.Path) -> list[pathlib.Path]:
    files: set[pathlib.Path] = set()
    for pattern in SCOPE:
        for path in root.glob(pattern):
            rel = path.relative_to(root)
            if not path.is_file() or any(part in HISTORY for part in rel.parts):
                continue
            if rel.as_posix() in HISTORY_FILES or rel.parts[:3] == ("CLAUDE", "Plan", "Completed"):
                continue
            files.add(path)
    return sorted(files)


def findings(root: pathlib.Path) -> list[str]:
    out = []
    for path in judged_files(root):
        for s in findings_in(path.read_text(encoding="utf-8")):
            out.append(f"{path.relative_to(root)}: {RULE_ID}: {s}")
    return out


class RuleFixtures(unittest.TestCase):
    """The rule is proved against fixtures first, both ways."""

    def test_the_three_wordings_the_host_run_tripped_on_are_findings(self) -> None:
        for text in (
            "`ccy --teams` offers the teams whose scope covers this checkout,\n"
            "choosing alone when exactly one matches and saying so when none does.",
            'If only one team covers the\ncheckout, it says "Team ... works on ..." and takes it without a list.',
            "Covering teams first, and a row to create a team. A single covering team is\ntaken without a list.",
        ):
            with self.subTest(text=text):
                self.assertEqual(len(findings_in(text)), 1, text)

    def test_a_sentence_that_names_the_machines_one_team_is_not_a_finding(self) -> None:
        for text in (
            "When the machine has exactly one team and it covers the checkout, it is taken without a list.",
            "The only team on this machine, when it covers the checkout, is taken without a list.",
        ):
            with self.subTest(text=text):
                self.assertEqual(findings_in(text), [])

    def test_other_sentences_about_lists_are_not_findings(self) -> None:
        for text in (
            "A team alone in the given list goes straight to the seat chooser.",
            "ccy-teams for-checkout prints the covering teams for scripts.",
            "Tab marks several teams without a list of seats.",
        ):
            with self.subTest(text=text):
                self.assertEqual(findings_in(text), [])

    def test_history_is_not_judged(self) -> None:
        judged = {p.relative_to(REPO_ROOT).as_posix() for p in judged_files(REPO_ROOT)}
        self.assertIn("docs/agent-bus.md", judged)
        self.assertFalse(any("/JOURNAL/" in p or "/subagent-reports/" in p for p in judged))
        self.assertNotIn("docs/ccy-changelog.md", judged)


class Repository(unittest.TestCase):
    def test_no_text_states_the_skip_condition_without_the_machines_one_team(self) -> None:
        found = findings(REPO_ROOT)
        self.assertEqual(found, [], "\n".join(found) + "\nSee CLAUDE/QA.md#" + RULE_ID)


if __name__ == "__main__":
    unittest.main()
