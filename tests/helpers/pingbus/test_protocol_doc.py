"""Contract test: docs/agent-bus-protocol.md and helpers.pingbus.protocol agree.

The spec is the single source of truth a human reads; the constants are what every
pingbus enforces. Nothing at runtime notices when the two drift, so each table the
validator implements is parsed out of the document and compared with the code, in both
directions. A table this test cannot find is a failure, never agreement.
"""

from __future__ import annotations

import json
import os
import re
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.pingbus import protocol as p

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
DOC = os.path.join(REPO_ROOT, "docs", "agent-bus-protocol.md")
HUMAN_PLACEHOLDER = "<each human user ID>"


def read_doc() -> str:
    with open(DOC, encoding="utf-8") as fh:
        return fh.read()


def section(text: str, number: int) -> str:
    """The body of `## <number>. ...` up to the next `## ` heading."""
    match = re.search(rf"^## {number}\. .*?$(.*?)(?=^## |\Z)", text, re.M | re.S)
    if match is None:
        raise AssertionError(f"section {number} not found in {DOC}")
    return match.group(0)


def tables(body: str) -> list[list[list[str]]]:
    """Every markdown table in `body`: rows with header and rule removed, cells stripped,
    `\\|` unescaped."""
    found: list[list[list[str]]] = []
    current: list[list[str]] = []
    for line in body.splitlines() + [""]:
        if line.startswith("|"):
            cells = [c.strip().replace("\\|", "|") for c in re.split(r"(?<!\\)\|", line)[1:-1]]
            current.append(cells)
        elif current:
            if len(current) < 3:
                raise AssertionError("a markdown table with no body rows")
            found.append(current[2:])
            current = []
    if not found:
        raise AssertionError("no markdown table found")
    return found


def code(cell: str) -> str:
    """The text of the single backtick span in a cell."""
    spans = re.findall(r"`([^`]*)`", cell)
    if len(spans) != 1:
        raise AssertionError(f"expected one code span in {cell!r}")
    return spans[0]


def fenced(body: str, lang: str = "") -> list[str]:
    return re.findall(rf"```{lang}\n(.*?)```", body, re.S)


class TestProtocolDoc(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.text = read_doc()

    def test_version(self) -> None:
        self.assertTrue(self.text.startswith(f"# Agent team bus protocol, version {p.PROTOCOL_VERSION}\n"))
        self.assertIn(f"`PROTOCOL_VERSION = {p.PROTOCOL_VERSION}`", section(self.text, 1))

    def test_prefix_and_event_types(self) -> None:
        body = section(self.text, 2)
        self.assertIn(f'`PREFIX = "{p.PREFIX}"`', body)
        self.assertIn(f"content key `{p.PING_KEY}`", body)
        rows = {r[0]: r for r in tables(body)[0]}
        self.assertEqual(set(rows), {"ping", "human message", "team record", "status"})
        self.assertEqual(code(rows["ping"][1]), p.EVENT_MESSAGE)
        self.assertEqual(code(rows["human message"][1]), p.EVENT_MESSAGE)
        self.assertEqual(code(rows["team record"][1]), p.EVENT_TEAM)
        self.assertEqual(code(rows["status"][1]), p.EVENT_STATUS)
        self.assertIn(f"`{p.MSGTYPE_PING}`", rows["ping"][2])
        self.assertIn(f"`{p.MSGTYPE_HUMAN}`", rows["human message"][2])

    def test_identifier_patterns(self) -> None:
        body = section(self.text, 3)
        rows = {row[0]: row[1] for row in tables(body)[0]}
        expected = {
            "team name": p.TEAM_NAME_PATTERN,
            "agent handle": p.HANDLE_PATTERN,
            "human localpart": p.HUMAN_LOCALPART_PATTERN,
            "room ID": p.ROOM_ID_PATTERN,
            "event ID": p.EVENT_ID_PATTERN,
        }
        for name, pattern in expected.items():
            with self.subTest(identifier=name):
                self.assertEqual(re.findall(r"`([^`]*)`", rows[name])[0], pattern)
        self.assertEqual(tuple(re.findall(r"`([^`]*)`", rows["reserved localpart"])), p.RESERVED_LOCALPARTS)
        self.assertIn(f"`{p.HANDLE_SEP}` between `<n>` and `<host>` is one constant (`HANDLE_SEP`)", body)

    def test_ping_envelope(self) -> None:
        body = section(self.text, 4)
        envelope, obj = tables(body)[:2]
        self.assertEqual(tuple(code(r[0]) for r in envelope), p.CONTENT_KEYS)
        self.assertEqual(code({code(r[0]): r for r in envelope}["msgtype"][1]), f'"{p.MSGTYPE_PING}"')
        self.assertIn(f"at most {p.MAX_CONTENT_BYTES} bytes", body)
        self.assertEqual(tuple(code(r[0]) for r in obj), p.PING_KEYS)
        rows = {code(r[0]): r for r in obj}
        self.assertEqual(
            [k for k, r in rows.items() if r[2] == "always"], list(p.PING_REQUIRED_KEYS)
        )
        self.assertIn(f"1 to {p.MAX_TO} distinct", rows["to"][3])
        humans_clause = re.search(r"for (.*?) only, a listed human", rows["to"][3])
        self.assertIsNotNone(humans_clause)
        self.assertEqual(set(re.findall(r"`([^`]*)`", humans_clause.group(1))), set(p.VERBS_TO_HUMANS))
        self.assertIn(f"at most {p.MAX_REF_LEN} characters", rows["ref"][3])

    def test_rendering_template_and_example(self) -> None:
        body = section(self.text, 4)
        template = fenced(body)[0].strip()
        self.assertEqual(template, f'{p.RENDER_TAG} <verb> <ref or "-"> -> <to[0]> <to[1]> ...[ re <re>]')
        example = re.search(r"Example: `([^`]*)`", body).group(1)
        to = "@myrepo.2+workstation.podman:<sn>"
        ref = "pr:example-org/myrepo#12@0123456789abcdef0123456789abcdef01234567"
        self.assertEqual(example, p.render({"verb": "review", "ref": ref, "to": [to]}))

    def test_verbs_table(self) -> None:
        rows = tables(section(self.text, 5))[0]
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
        lines = fenced(body)[0].splitlines()
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

    def test_human_message_limit(self) -> None:
        self.assertIn(f"at most {p.MAX_HUMAN_BODY_BYTES} UTF-8 bytes", section(self.text, 7))
        self.assertIn(f"`msgtype` `{p.MSGTYPE_HUMAN}`", section(self.text, 7))

    def test_team_record_status_and_power_levels(self) -> None:
        body = section(self.text, 8)
        rows = tables(body)[0]
        self.assertEqual(tuple(code(r[0]) for r in rows), p.TEAM_KEYS)
        by_key = {code(r[0]): r[2] for r in rows}
        self.assertIn(f"1 to {p.HUMANS_MAX} user IDs", by_key["humans"])
        self.assertIn(f"0 to {p.ROLES_MAX} entries", by_key["roles"])
        self.assertIn(f"serialised at most {p.MAX_STATUS_BYTES} bytes", body)
        self.assertIn(
            f'`{{"v": {p.PROTOCOL_VERSION}, "state": "{p.STATUS_LISTENING}", "until": <int ms>}}`', body
        )
        levels = json.loads(fenced(body, "json")[0])
        self.assertEqual(levels, p.expected_power_levels([HUMAN_PLACEHOLDER]))

    def test_drop_reason_codes(self) -> None:
        body = section(self.text, 9)
        m = re.search(r"\*\*Drop reason codes\*\* \(closed set\):(.*?)\n\n", body, re.S)
        self.assertIsNotNone(m)
        self.assertEqual(tuple(re.findall(r"`([^`]*)`", m.group(1))), p.DROP_REASONS)

    def test_fixed_sizes_in_limits(self) -> None:
        body = " ".join(section(self.text, 10).split())
        self.assertIn(f"a ping's content at most {p.MAX_CONTENT_BYTES} bytes", body)
        self.assertIn(f"a human message's body at most {p.MAX_HUMAN_BODY_BYTES} bytes", body)

    def test_allowlists(self) -> None:
        body = section(self.text, 11)
        self.assertIn(f"`{p.BRANCH_PATTERN}`", body)
        self.assertIn(f"1 to {p.REPOS_MAX} objects", body)
        self.assertIn(f"1 to {p.BRANCHES_MAX} names", body)
        self.assertIn(f"1 to {p.PATH_PREFIXES_MAX} entries", body)


if __name__ == "__main__":
    unittest.main()
