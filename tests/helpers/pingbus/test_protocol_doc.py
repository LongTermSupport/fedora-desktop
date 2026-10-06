"""Contract test: docs/agent-team-bus-protocol.md and helpers.pingbus.protocol agree.

The spec is the single source of truth a human reads; the constants are what every
pingbus and the warden enforce. Nothing at runtime notices when the two drift, so each
table the validator implements is parsed out of the document and compared with the code,
in both directions. A table this test cannot find is a failure, never agreement.
"""

from __future__ import annotations

import os
import re
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.pingbus import protocol as p

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
DOC = os.path.join(REPO_ROOT, "docs", "agent-team-bus-protocol.md")


def read_doc() -> str:
    with open(DOC, encoding="utf-8") as fh:
        return fh.read()


def section(text: str, number: int) -> str:
    """The body of `## <number>. ...` up to the next `## ` heading."""
    match = re.search(rf"^## {number}\. .*?$(.*?)(?=^## |\Z)", text, re.M | re.S)
    if match is None:
        raise AssertionError(f"section {number} not found in {DOC}")
    return match.group(0)


def table_rows(body: str) -> list[list[str]]:
    """Rows of the first markdown table in `body`, header and rule removed, cells
    stripped, `\\|` unescaped."""
    rows = []
    for line in body.splitlines():
        if not line.startswith("|"):
            if rows:
                break
            continue
        cells = [c.strip().replace("\\|", "|") for c in re.split(r"(?<!\\)\|", line)[1:-1]]
        rows.append(cells)
    if len(rows) < 3:
        raise AssertionError("no markdown table found")
    return rows[2:]


def code(cell: str) -> str:
    """The text of the single backtick span in a cell."""
    spans = re.findall(r"`([^`]*)`", cell)
    if len(spans) != 1:
        raise AssertionError(f"expected one code span in {cell!r}")
    return spans[0]


class TestProtocolDoc(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.text = read_doc()

    def test_version(self) -> None:
        self.assertIn(f"`PROTOCOL_VERSION = {p.PROTOCOL_VERSION}`", section(self.text, 1))
        self.assertTrue(self.text.startswith(f"# Agent team bus protocol, version {p.PROTOCOL_VERSION}\n"))

    def test_namespace_and_event_types(self) -> None:
        body = section(self.text, 2)
        self.assertIn(f"(namespace `{p.NAMESPACE}`)", body.splitlines()[0])
        types = [code(row[0]) for row in table_rows(body)]
        self.assertEqual(types, [f"<ns>.{s}" for s in p.EVENT_TYPE_SUFFIXES])

    def test_identifier_patterns(self) -> None:
        rows = {row[0]: row[1] for row in table_rows(section(self.text, 3))}
        expected = {
            "agent handle": p.HANDLE_PATTERN,
            "human localpart": p.HUMAN_LOCALPART_PATTERN,
            "room ID": p.ROOM_ID_PATTERN,
            "event ID": p.EVENT_ID_PATTERN,
            "room pair name": p.ROOM_PAIR_NAME_PATTERN,
        }
        for name, pattern in expected.items():
            with self.subTest(identifier=name):
                self.assertEqual(re.findall(r"`([^`]*)`", rows[name])[0], pattern)
        reserved = re.findall(r"`([^`]*)`", rows["reserved localpart"])
        self.assertEqual(tuple(reserved), p.RESERVED_LOCALPARTS)

    def test_ping_keys_and_limits(self) -> None:
        body = section(self.text, 4)
        keys = [code(row[0]) for row in table_rows(body)]
        self.assertEqual(tuple(keys), p.PING_KEYS)
        self.assertIn(f"at most {p.MAX_CONTENT_BYTES} bytes", body)
        rows = {code(row[0]): row for row in table_rows(body)}
        self.assertIn(f"1 to {p.MAX_TO} distinct", rows["to"][3])
        self.assertIn(f"≤ {p.MAX_REF_LEN} characters", rows["ref"][3])

    def test_verbs_table(self) -> None:
        rows = table_rows(section(self.text, 5))
        self.assertEqual([code(r[0]) for r in rows], list(p.VERBS))
        for row in rows:
            verb = code(row[0])
            rule = p.VERBS[verb]
            with self.subTest(verb=verb):
                presence, _, forms_text = row[2].partition(":")
                self.assertEqual(presence.strip(), rule.ref)
                if forms_text.strip() == "any form":
                    forms = frozenset(p.REF_FORMS)
                else:
                    forms = frozenset(re.findall(r"`([^`]*)`", forms_text))
                self.assertEqual(forms, rule.ref_forms)
                self.assertEqual(row[3], rule.re)
                self.assertEqual(row[4], "yes" if rule.ack_expected else "no")
                if row[5] == "anyone addressed by `re`":
                    senders = frozenset({p.SENDER_ADDRESSED})
                else:
                    senders = frozenset(s.strip() for s in row[5].split(","))
                self.assertEqual(senders, rule.senders)

    def test_reference_grammar(self) -> None:
        body = section(self.text, 6)
        block = re.search(r"```\n(.*?)```", body, re.S)
        self.assertIsNotNone(block)
        lines = block.group(1).splitlines()
        defs = {}
        forms = {}
        for line in lines:
            m = re.match(r"^([A-Z]+)\s+=\s+(\S+)", line)
            if m:
                defs[m.group(1)] = m.group(2)
            m = re.match(r"^([a-z]+):\s+(\S+)", line)
            if m:
                forms[m.group(1)] = m.group(2)
        self.assertEqual(
            defs,
            {
                "OWNER": p.OWNER_PATTERN,
                "REPO": p.REPO_PATTERN,
                "SHA": p.SHA_PATTERN,
                "NUM": p.NUM_PATTERN,
                "SEG": p.SEG_PATTERN,
                "PATH": f"SEG(/SEG){{0,{p.PATH_MAX_SEGMENTS - 1}}}",
            },
        )
        self.assertEqual(forms, p.REF_TEMPLATES)
        self.assertEqual(tuple(forms), p.REF_FORMS)

    def test_note(self) -> None:
        body = section(self.text, 7)
        self.assertIn(f"at most {p.NOTE_MAX} characters, pattern `{p.NOTE_PATTERN}`", body)

    def test_drop_reason_codes(self) -> None:
        body = section(self.text, 8)
        m = re.search(r"\*\*Drop reason codes\*\* \(closed set\):(.*?)\n\n", body, re.S)
        self.assertIsNotNone(m)
        self.assertEqual(tuple(re.findall(r"`([^`]*)`", m.group(1))), p.DROP_REASONS)

    def test_branch_pattern(self) -> None:
        self.assertIn(f"`{p.BRANCH_PATTERN}`", section(self.text, 10))

    def test_roles_bounds(self) -> None:
        self.assertIn(f"`roles` holds 1 to {p.ROLES_MAX} entries, exactly one", section(self.text, 2))


if __name__ == "__main__":
    unittest.main()
