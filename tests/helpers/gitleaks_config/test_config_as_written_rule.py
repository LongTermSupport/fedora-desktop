"""The rule gitleaks-config-not-as-written (Defence Before Fix; CLAUDE/QA.md).

`.gitleaks.toml` must do what it says, because an allowlist wider than written lets a real
secret through a scan that reports nothing. gitleaks 8.30.1, which CI runs as `gitleaks dir`,
departs from what is written in two ways, and the rule reports each:

- A key it does not read. It reads the file through viper into fixed structs (matching a key
  to a field without regard to case) and drops any other key without a word. An allowlist
  written with `matchCondition = "AND"` (the key is `condition`) is an OR: its paths allow every
  finding in those files, its regexes those values in every file.
- A global allowlist (no `targetRules`) that pairs `paths` with `regexes` or `stopwords` under
  `condition = "AND"`. `gitleaks dir` drops every file such an allowlist's paths match before
  any rule runs (`sources/common.go` `shouldSkipPath`), so the AND never applies and the whole
  file goes unscanned. Naming the rule in `targetRules` attaches the allowlist to that rule,
  where the condition is applied to each finding.

The schema below is gitleaks 8.30.1's (`config/config.go`: `ViperConfig`, `Extend`,
`viperRuleAllowlist`, `viperGlobalAllowlist`, `viperRequired`). The version is the one the CI
workflow installs; when the workflow moves to another, the version test fails until both
clauses are checked against that release and the pin here moved with it.
"""

from __future__ import annotations

import pathlib
import re
import textwrap
import tomllib
import unittest

RULE_ID = "gitleaks-config-not-as-written"
REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
CONFIG = ".gitleaks.toml"
WORKFLOW = ".github/workflows/qa.yml"
SCHEMA_VERSION = "8.30.1"

TOP_KEYS = frozenset({"title", "description", "extend", "rules", "allowlist", "allowlists", "minversion"})
EXTEND_KEYS = frozenset({"path", "url", "usedefault", "disabledrules"})
RULE_KEYS = frozenset({"id", "description", "path", "regex", "secretgroup", "entropy", "keywords", "tags",
                       "allowlist", "allowlists", "required", "skipreport"})
RULE_ALLOWLIST_KEYS = frozenset({"description", "condition", "commits", "paths", "regextarget", "regexes",
                                 "stopwords"})
GLOBAL_ALLOWLIST_KEYS = RULE_ALLOWLIST_KEYS | {"targetrules"}
REQUIRED_KEYS = frozenset({"id", "withinlines", "withincolumns"})


def _tables(value: object) -> list[dict]:
    """A table, or each table of an array of tables; anything else holds no keys to judge."""
    if isinstance(value, dict):
        return [value]
    if isinstance(value, list):
        return [item for item in value if isinstance(item, dict)]
    return []


def _unread(table: dict, known: frozenset[str], where: str) -> list[str]:
    return [f"{where}.{key}: a key gitleaks does not read" for key in table if key.lower() not in known]


def _value(table: dict, key: str) -> object:
    """A key's value as viper finds it: the key matched without regard to case."""
    return next((value for name, value in table.items() if name.lower() == key), None)


def _whole_file_skip(allowlist: dict, where: str) -> list[str]:
    anded = str(_value(allowlist, "condition") or "").upper() in ("AND", "&&")
    if anded and _value(allowlist, "paths") and not _value(allowlist, "targetrules") and (
            _value(allowlist, "regexes") or _value(allowlist, "stopwords")):
        return [f"{where}: paths under condition AND with no targetRules: gitleaks dir skips each whole file"]
    return []


def detect(text: str) -> list[str]:
    """Each place in a gitleaks configuration that gitleaks does not apply as written."""
    config = tomllib.loads(text)
    found = _unread(config, TOP_KEYS, "")
    for extend in _tables(config.get("extend")):
        found += _unread(extend, EXTEND_KEYS, ".extend")
    for key in ("allowlist", "allowlists"):
        for n, allowlist in enumerate(_tables(config.get(key))):
            found += _unread(allowlist, GLOBAL_ALLOWLIST_KEYS, f".{key}[{n}]")
            found += _whole_file_skip(allowlist, f".{key}[{n}]")
    for r, rule in enumerate(_tables(config.get("rules"))):
        found += _unread(rule, RULE_KEYS, f".rules[{r}]")
        for key in ("allowlist", "allowlists"):
            for n, allowlist in enumerate(_tables(rule.get(key))):
                found += _unread(allowlist, RULE_ALLOWLIST_KEYS, f".rules[{r}].{key}[{n}]")
        for n, required in enumerate(_tables(rule.get("required"))):
            found += _unread(required, REQUIRED_KEYS, f".rules[{r}].required[{n}]")
    return [place.lstrip(".") for place in found]


FIXTURE = textwrap.dedent('''\
    [extend]
    useDefault = true
    disabled_rules = ["x"]

    [[allowlists]]
    description = "the condition key gitleaks drops"
    matchCondition = "AND"
    paths = ['a\\.py']
    regexes = ['^v$']
    regexTarget = "secret"

    [[allowlists]]
    description = "every key read, in viper's own mixed case, attached to its rule"
    Condition = "AND"
    targetRules = ["generic-api-key"]
    stopWords = ["w"]
    paths = ['b\\.py']

    [[allowlists]]
    description = "global, so gitleaks dir skips c.py whole"
    condition = "and"
    paths = ['c\\.py']
    regexes = ['^v$']

    [[allowlists]]
    description = "a whole-file skip, written as one"
    paths = ['d\\.py']

    [[rules]]
    id = "r"
    regex = "x"
    keyword = ["x"]
    [[rules.allowlists]]
    condition = "AND"
    targetRules = ["r"]
    paths = ['e\\.py']
    regexes = ['^y$']
    [[rules.required]]
    id = "s"
    withinLines = 1
''')


class RuleProof(unittest.TestCase):
    def test_reports_each_place_not_applied_as_written_and_nothing_else(self) -> None:
        self.assertEqual(detect(FIXTURE), [
            "extend.disabled_rules: a key gitleaks does not read",
            "allowlists[0].matchCondition: a key gitleaks does not read",
            "allowlists[2]: paths under condition AND with no targetRules: gitleaks dir skips each whole file",
            "rules[0].keyword: a key gitleaks does not read",
            "rules[0].allowlists[0].targetRules: a key gitleaks does not read",
        ])


class Sweep(unittest.TestCase):
    def test_the_schema_is_the_version_ci_installs(self) -> None:
        workflow = (REPO_ROOT / WORKFLOW).read_text(encoding="utf-8")
        pins = re.findall(r"^\s*VER=(\S+)\s*$", workflow, re.M)
        self.assertEqual(pins, [SCHEMA_VERSION], f"{WORKFLOW} gitleaks VER= lines; check the rule for a new release")

    def test_no_finding(self) -> None:
        found = [f"{CONFIG}: {place} ({RULE_ID})"
                 for place in detect((REPO_ROOT / CONFIG).read_text(encoding="utf-8"))]
        self.assertEqual(found, [], "\n" + "\n".join(found))


if __name__ == "__main__":
    unittest.main()
