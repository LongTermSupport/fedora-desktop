"""Unit tests for helpers.docs.link_check.

Stdlib-only (helpers/CLAUDE.md rule — no pytest, no venv).

The slug cases are the important ones. This checker's first implementation was
WRONG in exactly the way the defects it hunts are wrong: it collapsed runs of
whitespace, but GitHub replaces each space individually, so a heading containing
" — " produces a DOUBLE hyphen. That under-reported by 32 findings. Every slug
rule below is pinned against an anchor observed working in a rendered document.
"""

import os
import unittest

from helpers.docs import link_check


class TestSlug(unittest.TestCase):
    """GitHub heading-anchor slug rules."""

    def test_lowercases_and_hyphenates_spaces(self):
        self.assertEqual(link_check.slug("Quick Install"), "quick-install")

    def test_each_space_becomes_its_own_hyphen(self):
        # "Fail Fast — HARD RULE": the em-dash is stripped, leaving TWO spaces,
        # which become TWO hyphens. Collapsing here is the bug that shipped.
        self.assertEqual(
            link_check.slug("Fail Fast — HARD RULE"), "fail-fast--hard-rule")

    def test_plus_sign_also_yields_a_double_hyphen(self):
        # Pinned against an author-written anchor that resolves in the rendered
        # document: #3-kickstart-luks--btrfs-partitioning
        self.assertEqual(
            link_check.slug("3. Kickstart LUKS + Btrfs Partitioning"),
            "3-kickstart-luks--btrfs-partitioning")

    def test_slash_is_deleted_not_hyphenated(self):
        # "pass/fail" -> "passfail", NOT "pass-fail".
        self.assertEqual(link_check.slug("the pass/fail gate"),
                         "the-passfail-gate")

    def test_semicolon_removed_without_adding_a_hyphen(self):
        self.assertEqual(
            link_check.slug("Triage is fact-finding; verify/acceptance"),
            "triage-is-fact-finding-verifyacceptance")

    def test_existing_hyphens_are_preserved(self):
        # "config-manager --enable" -> literal "--" plus the space-hyphen.
        self.assertEqual(link_check.slug("dnf5 rejects config-manager --enable"),
                         "dnf5-rejects-config-manager---enable")

    def test_strips_inline_code_backticks(self):
        self.assertEqual(link_check.slug("The `qa-js.bash` gate"),
                         "the-qa-jsbash-gate")

    def test_strips_bold_markers(self):
        self.assertEqual(link_check.slug("A **bold** heading"),
                         "a-bold-heading")

    def test_trailing_and_leading_whitespace_ignored(self):
        self.assertEqual(link_check.slug("  Spaced  "), "spaced")

    def test_underscores_survive(self):
        self.assertEqual(link_check.slug("provisioning_profile detection"),
                         "provisioning_profile-detection")


class TestHeadings(unittest.TestCase):
    def test_collects_all_levels(self):
        content = "# One\n\n## Two\n\n###### Six\n"
        self.assertEqual(link_check.headings(content), {"one", "two", "six"})

    def test_ignores_headings_inside_fenced_code(self):
        content = "# Real\n\n```bash\n# Not A Heading\n```\n\n## Also Real\n"
        self.assertEqual(link_check.headings(content), {"real", "also-real"})

    def test_ignores_tilde_fenced_code(self):
        content = "# Real\n\n~~~\n# Fake\n~~~\n"
        self.assertEqual(link_check.headings(content), {"real"})

    def test_duplicate_headings_get_numeric_suffixes(self):
        content = "# Dup\n\n# Dup\n\n# Dup\n"
        self.assertEqual(link_check.headings(content),
                         {"dup", "dup-1", "dup-2"})

    def test_requires_space_after_hashes(self):
        # "#NotAHeading" is not a heading in GFM.
        self.assertEqual(link_check.headings("#NoSpace\n"), set())

    def test_strips_trailing_closing_hashes(self):
        self.assertEqual(link_check.headings("## Closed ##\n"), {"closed"})


class TestLinks(unittest.TestCase):
    def test_finds_a_relative_link(self):
        self.assertEqual(link_check.links("see [x](docs/a.md)"),
                         [(1, "docs/a.md")])

    def test_reports_the_line_number(self):
        self.assertEqual(link_check.links("a\nb\n[x](y.md)"), [(3, "y.md")])

    def test_skips_external_schemes(self):
        content = "[a](https://example.com) [b](mailto:x@example.com)"
        self.assertEqual(link_check.links(content), [])

    def test_skips_protocol_relative_urls(self):
        self.assertEqual(link_check.links("[a](//example.com/x)"), [])

    def test_skips_images(self):
        self.assertEqual(link_check.links("![alt](pic.png)"), [])

    def test_finds_a_bare_fragment(self):
        self.assertEqual(link_check.links("[a](#section)"), [(1, "#section")])

    def test_ignores_links_inside_fenced_code(self):
        content = "```\n[a](nope.md)\n```\n[b](yes.md)\n"
        self.assertEqual(link_check.links(content), [(4, "yes.md")])

    def test_handles_two_links_on_one_line(self):
        self.assertEqual(
            link_check.links("[a](one.md) and [b](two.md)"),
            [(1, "one.md"), (1, "two.md")])


class TestCatalogChecks(unittest.TestCase):
    def test_imported_playbooks_are_extracted(self):
        content = (
            "- import_playbook: imports/play-a.yml\n"
            "# - import_playbook: imports/play-commented.yml\n"
            "- import_playbook: imports/play-b.yml\n"
        )
        self.assertEqual(link_check.imported_playbooks(content),
                         ["play-a.yml", "play-b.yml"])

    def test_missing_playbook_is_reported(self):
        missing = link_check.missing_mentions(["play-a.yml", "play-b.yml"],
                                              "we document play-a.yml only")
        self.assertEqual(missing, ["play-b.yml"])

    def test_nothing_missing_returns_empty(self):
        missing = link_check.missing_mentions(["play-a.yml"],
                                              "play-a.yml is here")
        self.assertEqual(missing, [])


class TestQaGateInventory(unittest.TestCase):
    """The gate inventory in CLAUDE/QA.md is DERIVED, not re-enumerated.

    That table had drifted by roughly five entries and stated two counts that were both
    wrong, and every previous fix replaced one stale enumeration with a fresher one — so
    it went stale again. `CLAUDE/AgentNotes.md` names the answer: derive the set. The
    names now come out of `qa-all.bash` itself, so a gate added without a row fails the
    docs gate on the same commit.
    """

    QA_ALL = (
        'QA_JSON_OUT="$TMP_BASH" "$SCRIPT_DIR/qa-bash.bash" || rc=$?\n'
        'if ! out="$(bash "$SCRIPT_DIR/test-thing.bash" 2>&1)"; then\n'
        'if ! c="$(cd "$SCRIPT_DIR/.." && python3 -m helpers.gnome.check_thing 2>&1)"; then\n'
    )

    def test_bash_gates_are_extracted(self):
        self.assertIn("qa-bash.bash", link_check.qa_gates(self.QA_ALL))
        self.assertIn("test-thing.bash", link_check.qa_gates(self.QA_ALL))

    def test_python_module_gates_are_extracted_by_their_last_component(self):
        """The table names `check_extension_compat`, not the dotted path, so that is
        the token to require — requiring the dotted form would fail the document for
        writing the gate's name the way a human would."""
        self.assertIn("check_thing", link_check.qa_gates(self.QA_ALL))

    def test_a_gate_with_no_row_is_reported(self):
        missing = link_check.missing_mentions(
            link_check.qa_gates(self.QA_ALL), "we document qa-bash.bash and check_thing")
        self.assertEqual(missing, ["test-thing.bash"])

    def test_finding_no_gates_is_itself_a_finding(self):
        """A discovery that matches nothing would report a clean inventory over a
        document listing none of them — the shape this repo keeps rediscovering."""
        findings = link_check.check_qa_gate_inventory_in(
            qa_all="nothing that looks like a gate here", qa_doc="")
        self.assertEqual(len(findings), 1)
        self.assertIn("discovery", findings[0]["problem"])

    def test_the_shipped_inventory_is_complete(self):
        """The control that makes the rest of this class worth having: run against the
        real files, not a fixture."""
        root = os.path.join(os.path.dirname(__file__), "..", "..", "..")
        self.assertEqual(link_check.check_qa_gate_inventory(root), [])


class TestScope(unittest.TestCase):
    """Which markdown this gate owns.

    The plan tree is excluded on principle, not for convenience: a core gate
    that sweeps plan content is a core->plan dependency, so archiving a plan
    could flip core CI's verdict without a core file changing.
    """

    def test_includes_docs(self):
        self.assertTrue(link_check.in_scope("docs/architecture.md"))

    def test_includes_nested_docs(self):
        self.assertTrue(link_check.in_scope("docs/features/speech-to-text.md"))

    def test_includes_root_readme(self):
        self.assertTrue(link_check.in_scope("README.md"))

    def test_includes_root_claude_md(self):
        self.assertTrue(link_check.in_scope("CLAUDE.md"))

    def test_includes_top_level_topic_files(self):
        self.assertTrue(link_check.in_scope("CLAUDE/QA.md"))

    def test_includes_nested_claude_md(self):
        self.assertTrue(link_check.in_scope("helpers/CLAUDE.md"))

    def test_includes_path_triggered_rules(self):
        self.assertTrue(link_check.in_scope(".claude/rules/qa-gates.md"))

    def test_excludes_the_plan_tree(self):
        self.assertFalse(link_check.in_scope("CLAUDE/Plan/00070-x/PLAN.md"))

    def test_excludes_archived_plans(self):
        self.assertFalse(
            link_check.in_scope("CLAUDE/Plan/Completed/00067-x/PLAN.md"))

    def test_excludes_plan_system_docs(self):
        self.assertFalse(link_check.in_scope("CLAUDE/Plan/CLAUDE.md"))

    def test_excludes_vendored_checkouts(self):
        self.assertFalse(
            link_check.in_scope("untracked/repos/fedora-desktop/README.md"))

    def test_excludes_node_modules_at_any_depth(self):
        self.assertFalse(
            link_check.in_scope("extensions/node_modules/pkg/README.md"))

    def test_excludes_the_hooks_daemon(self):
        self.assertFalse(link_check.in_scope(".claude/hooks-daemon/X.md"))

    def test_excludes_agent_definitions(self):
        # .claude/agents/ carries template placeholders like [text](URL).
        self.assertFalse(link_check.in_scope(".claude/agents/creator.md"))

    def test_excludes_arbitrary_other_markdown(self):
        self.assertFalse(link_check.in_scope("extensions/SOMENOTES.md"))


if __name__ == "__main__":
    unittest.main()
