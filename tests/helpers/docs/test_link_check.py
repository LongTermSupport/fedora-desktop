"""Unit tests for helpers.docs.link_check.

Stdlib-only (helpers/CLAUDE.md rule — no pytest, no venv).

The slug cases are the important ones. This checker's first implementation was
WRONG in exactly the way the defects it hunts are wrong: it collapsed runs of
whitespace, but GitHub replaces each space individually, so a heading containing
" — " produces a DOUBLE hyphen. That under-reported by 32 findings. Every slug
rule below is pinned against an anchor observed working in a rendered document.
"""

import os
import subprocess
import tempfile
import unittest

from helpers.docs import link_check
from helpers.qa_environment import verdicts


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

    def test_python_module_gates_are_extracted_by_their_dotted_path(self):
        """Which is what the table's first cell writes. Reducing them to a last
        component matched the document by substring one way and disagreed with it the
        other, so the reverse check below could never have been clean."""
        self.assertIn("helpers.gnome.check_thing", link_check.qa_gates(self.QA_ALL))

    def test_the_same_gate_written_three_ways_is_one_gate(self):
        """`$SCRIPT_DIR/x`, `${SCRIPT_DIR}/x` and `$REPO_ROOT/scripts/x` name the same
        gate. Keying on one spelling exempts the others from the inventory silently,
        and the zero-discovery guard only fires if EVERY form fails."""
        for form in ('"$SCRIPT_DIR/test-x.bash"', '"${SCRIPT_DIR}/test-x.bash"',
                     '"$REPO_ROOT/scripts/test-x.bash"'):
            with self.subTest(form=form):
                self.assertEqual(link_check.qa_gates(f"bash {form}"), ["test-x.bash"])

    def test_a_sourced_library_is_not_a_gate(self):
        """`source`ing a library is not invoking a gate. A library emits no verdict line,
        so a row for it in the gate table would claim something the table cannot mean —
        and the alternative, documenting it to satisfy the checker, is how an inventory
        stops describing reality."""
        sourced = 'source "$SCRIPT_DIR/lib/qa-helper-summary.bash"\n'
        self.assertEqual(link_check.qa_gates(sourced), [])

    def test_the_dot_form_of_source_is_also_not_a_gate(self):
        self.assertEqual(link_check.qa_gates('. "$SCRIPT_DIR/lib/thing.bash"\n'), [])

    def test_a_gate_on_a_line_after_a_sourced_library_is_still_found(self):
        """The exclusion is per LINE, not a mode the file enters."""
        content = (
            'source "$SCRIPT_DIR/lib/qa-helper-summary.bash"\n'
            'if ! out="$(bash "$SCRIPT_DIR/test-thing.bash" 2>&1)"; then\n'
        )
        self.assertEqual(link_check.qa_gates(content), ["test-thing.bash"])

    def test_a_gate_with_no_row_is_reported(self):
        findings = link_check.check_qa_gate_inventory_in(
            qa_all=self.QA_ALL,
            qa_doc="| `qa-bash.bash` | x |\n| `helpers.gnome.check_thing` | y |\n")
        self.assertEqual([f["target"] for f in findings], ["test-thing.bash"])

    def test_prose_naming_a_gate_is_not_a_row_claiming_it(self):
        """A substring search over the whole document is satisfied by the paragraph
        below the table that names two gates while discussing them."""
        findings = link_check.check_qa_gate_inventory_in(
            qa_all='bash "$SCRIPT_DIR/test-thing.bash"',
            qa_doc="test-thing.bash was once documented and not run.")
        self.assertEqual([f["target"] for f in findings], ["test-thing.bash"])

    def test_a_documented_gate_that_is_not_run_is_reported(self):
        """The reverse direction. One-way, the row for a retired gate stands for ever —
        and a documented gate nobody executes is the failure this document narrates
        below its own table."""
        findings = link_check.check_qa_gate_inventory_in(
            qa_all='bash "$SCRIPT_DIR/test-thing.bash"',
            qa_doc="| `test-thing.bash` | x |\n| `test-retired.bash` | y |\n")
        self.assertEqual([f["target"] for f in findings], ["test-retired.bash"])

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


class _GitTree(unittest.TestCase):
    """A real git repository in a temp dir.

    The git index is the mechanism under test, so a fake would be testing a copy
    of it. `git ls-files` is what decides whether a target is this repository's,
    and the index ships in every clean checkout — which is why CI reaches the
    same verdict with no vendored tree present to probe.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = self.tmp.name
        self.addCleanup(self.tmp.cleanup)
        subprocess.run(["git", "init", "-q", self.root], check=True)
        self.write(".gitignore", ".claude/hooks-daemon/\nroles/vendor/*\nuntracked/\n")

    def write(self, rel, content):
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(content)
        return path

    def check(self, *rel_paths):
        return link_check.check_links(self.root, list(rel_paths))

    @staticmethod
    def vendored_total(vendored):
        """Every link that reached the vendored branch, whatever its outcome."""
        return (vendored["ok"] + vendored["unverifiable"]
                + len(vendored["broken"]))


class TestVendoredLinkTargets(_GitTree):
    """Three outcomes, not two — a link target is ours, vendored, or neither.

    `.claude/hooks-daemon/` is a clone of a different repository, and eight of
    the fifteen `.claude/rules/*.md` files are GENERATED by that repository's
    installer and link into it. This repo neither authors them nor could fix a
    broken link there, and the installer re-renders any edit on the next
    upgrade. Their targets exist on an installed machine and not in a clean
    checkout, which is why one commit produced a green docs gate here and a red
    one in CI for three weeks.

    The trap is treating "git ignores it" as sufficient. It is not: a link into
    `untracked/` is ignored too, and that one is a genuine defect — docs
    pointing at something no clean checkout has, and no other repository owns
    either. Collapsing the two would trade a false failure for a silent skip.

    A vendored target is still LOOKED AT, in the one case where looking means
    something. Three outcomes, and only the middle one is silent:

      - repo present, target there      -> ok
      - repo absent                     -> soft warning; nothing here could say
      - repo present, target NOT there  -> serious warning; the link really is
                                           broken, and on this machine we can
                                           prove it

    None of the three is a finding, because none of them is ours to fix — and
    that is the invariant the CI failure turned on: the gate's exit code must
    not depend on what is installed. What the gate SAYS may, and should, since a
    machine that can see more should report more.
    """

    def test_an_absent_vendored_repo_is_a_soft_warning(self):
        self.write(".claude/rules/agent-docs.md",
                   "See [roles](../hooks-daemon/CLAUDE/DirectoryRoles.md).\n")
        findings, vendored = self.check(".claude/rules/agent-docs.md")
        self.assertEqual(findings, [])
        self.assertEqual(vendored["unverifiable"], 1)
        self.assertEqual(vendored["ok"], 0)
        self.assertEqual(vendored["broken"], [])

    def test_a_present_vendored_repo_with_a_working_link_is_ok(self):
        self.write(".claude/hooks-daemon/CLAUDE/DirectoryRoles.md", "# Roles\n")
        self.write(".claude/rules/agent-docs.md",
                   "See [roles](../hooks-daemon/CLAUDE/DirectoryRoles.md).\n")
        findings, vendored = self.check(".claude/rules/agent-docs.md")
        self.assertEqual(findings, [])
        self.assertEqual(vendored["ok"], 1)
        self.assertEqual(vendored["unverifiable"], 0)
        self.assertEqual(vendored["broken"], [])

    def test_a_present_vendored_repo_with_a_broken_link_warns_seriously(self):
        """The case the flat exemption threw away.

        The repo IS here, so "cannot say" is false — the link is demonstrably
        broken, which usually means the vendored repo moved the file and the
        generated files pointing at it are stale. Worth saying loudly; still not
        ours to fix, so still not a finding.
        """
        os.makedirs(os.path.join(self.root, ".claude", "hooks-daemon", "CLAUDE"))
        self.write(".claude/rules/agent-docs.md",
                   "See [roles](../hooks-daemon/CLAUDE/DirectoryRoles.md).\n")
        findings, vendored = self.check(".claude/rules/agent-docs.md")
        self.assertEqual(findings, [])
        self.assertEqual(vendored["ok"], 0)
        self.assertEqual(vendored["unverifiable"], 0)
        self.assertEqual(len(vendored["broken"]), 1)
        self.assertEqual(vendored["broken"][0]["target"],
                         "../hooks-daemon/CLAUDE/DirectoryRoles.md")
        self.assertEqual(vendored["broken"][0]["file"],
                         ".claude/rules/agent-docs.md")

    def test_no_vendored_outcome_changes_the_gates_exit_code(self):
        """The invariant. All three outcomes leave `findings` empty.

        This is what fixes CI: the pass/fail verdict stops depending on what is
        installed. The warning text still varies by machine, deliberately — a
        machine that can see the repo can say more about it, and saying more
        never flips a verdict.
        """
        os.makedirs(os.path.join(self.root, ".claude", "hooks-daemon", "CLAUDE"))
        self.write(".claude/rules/agent-docs.md",
                   "[broken](../hooks-daemon/CLAUDE/Gone.md)\n")
        self.write(".claude/rules/plan-dir.md",
                   "[absent](../../roles/vendor/x/README.md)\n")
        findings, vendored = self.check(".claude/rules/agent-docs.md",
                                        ".claude/rules/plan-dir.md")
        self.assertEqual(findings, [])
        self.assertEqual(len(vendored["broken"]), 1)
        self.assertEqual(vendored["unverifiable"], 1)

    def test_a_link_to_the_vendored_root_ITSELF_is_covered(self):
        """The one path "parents, not leaves" did not cover — the parent.

        The roots carry a trailing slash and the test was `startswith`, so
        `[daemon](../hooks-daemon)` matched no root. It missed the ignore branch
        too: `.gitignore`'s rule is directory-only and git cannot tell that a
        non-existent path is a directory. Result was `target does not exist` —
        a hard CI failure, passing wherever the tree happens to be installed,
        which is precisely the divergence this classification replaced.
        """
        self.write(".claude/rules/agent-docs.md", "See [daemon](../hooks-daemon).\n")
        findings, vendored = self.check(".claude/rules/agent-docs.md")
        self.assertEqual(findings, [])
        self.assertEqual(self.vendored_total(vendored), 1)

    def test_a_sibling_of_a_vendored_root_is_not_exempted(self):
        """The trailing slash is what stops `roles/vendor-extra/` matching.

        Removing it to fix the case above would exempt every sibling whose name
        merely starts the same way, so the root is matched exactly rather than
        by a shortened prefix.
        """
        self.assertIsNone(link_check.vendored_root_for("roles/vendor-extra/x.md"))
        self.assertIsNone(link_check.vendored_root_for("roles/vendorx"))
        self.assertEqual(link_check.vendored_root_for("roles/vendor"), "roles/vendor/")
        self.assertEqual(link_check.vendored_root_for("roles/vendor/x"), "roles/vendor/")

    def test_every_vendored_root_is_also_excluded_from_the_scan(self):
        """The two tuples must agree, and nothing asserted it.

        Declaring a vendored root without excluding it from discovery would
        make this gate sweep another repository's markdown as if it were ours —
        checking their links, their anchors, and reporting their defects as
        ours.
        """
        for root in link_check._VENDORED_ROOTS:
            self.assertIn(root, link_check._EXCLUDE_PREFIX,
                          f"{root} is declared vendored but is still scanned")

    def test_an_anchor_into_a_vendored_repo_is_not_checked(self):
        """Existence only. Their headings are theirs to rename.

        Following an anchor into another repository would make this gate red
        every time that repo edited a title — a dependency on their churn, for
        a defect we could not fix.
        """
        self.write(".claude/hooks-daemon/CLAUDE/DirectoryRoles.md", "# Roles\n")
        self.write(".claude/rules/agent-docs.md",
                   "See [roles](../hooks-daemon/CLAUDE/DirectoryRoles.md#no-such-heading).\n")
        findings, vendored = self.check(".claude/rules/agent-docs.md")
        self.assertEqual(findings, [])
        self.assertEqual(vendored["ok"], 1)

    def test_a_vendored_role_is_covered_by_its_parent_declaration(self):
        """`roles/vendor/` is declared, not each role under it.

        A role vendored tomorrow is covered without touching this code, which
        is the half of "dynamic" that a declaration can actually deliver.
        """
        self.write("CLAUDE/QA.md",
                   "See [vault](../roles/vendor/some.role/README.md).\n")
        findings, vendored = self.check("CLAUDE/QA.md")
        self.assertEqual(findings, [])
        self.assertEqual(self.vendored_total(vendored), 1)

    def test_an_untracked_target_that_is_not_vendored_is_a_finding(self):
        """Not ours and nobody else's: a link to something no clean checkout has."""
        self.write("untracked/notes.md", "# Notes\n")
        self.write("CLAUDE/QA.md", "See [scratch](../untracked/notes.md).\n")
        findings, vendored = self.check("CLAUDE/QA.md")
        self.assertEqual(self.vendored_total(vendored), 0)
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["problem"],
                         "target is not tracked by this repository")

    def test_a_present_untracked_UNIGNORED_target_is_a_finding_here_too(self):
        """The divergence the ignore question could not see.

        A file sitting on this disk, never `git add`ed, matching no ignore
        rule: green here under an existence check, red in CI where it is simply
        absent. Cause A's exact shape, which is why the question asked is
        whether this repository TRACKS the target — the thing the finding has
        always claimed — rather than whether `.gitignore` happens to name it.
        """
        self.write("CLAUDE/Scratch.md", "# Scratch\n")
        self.write("CLAUDE/QA.md", "See [scratch](./Scratch.md).\n")
        findings, _ = self.check("CLAUDE/QA.md")
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["problem"],
                         "target is not tracked by this repository")

    def test_the_same_link_fails_in_CI_too_just_with_a_different_reason(self):
        """The invariant restated: same verdict, different detail.

        Here the file exists and is untracked; in a clean checkout it is absent.
        Both are findings, so the exit code agrees — which is all that must.
        """
        self.write("CLAUDE/QA.md", "See [scratch](./Scratch.md).\n")
        findings, _ = self.check("CLAUDE/QA.md")
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["problem"], "target does not exist")

    def test_a_tracked_directory_target_is_accepted(self):
        """`git ls-files` lists files, so a directory link needs its own answer."""
        self.write("docs/architecture.md", "# Arch\n")
        subprocess.run(["git", "-C", self.root, "add", "docs/architecture.md"], check=True)
        self.write("CLAUDE/QA.md", "See [docs](../docs).\n")
        findings, _ = self.check("CLAUDE/QA.md")
        self.assertEqual(findings, [])

    def test_a_link_to_the_repository_root_is_not_a_finding(self):
        """`git ls-files` lists no entry for the root, and the root is ours.

        `repo_relative` answers `.` for it, which is in neither the file set nor
        the derived-directory set, so it came out as "not tracked by this
        repository" — a false failure about the repository itself. No such link
        exists today; it is latent, and a reader meeting it would have nothing
        to act on.
        """
        self.write("README.md", "# R\n")
        subprocess.run(["git", "-C", self.root, "add", "README.md"], check=True)
        self.write("CLAUDE/QA.md", "See [the repo](..).\n")
        findings, _ = self.check("CLAUDE/QA.md")
        self.assertEqual(findings, [])

    def test_a_tracked_target_that_is_missing_from_disk_still_fails(self):
        """Trackedness does not excuse absence — a deleted tracked file is broken."""
        self.write("CLAUDE/Gone.md", "# Gone\n")
        subprocess.run(["git", "-C", self.root, "add", "CLAUDE/Gone.md"], check=True)
        os.remove(os.path.join(self.root, "CLAUDE", "Gone.md"))
        self.write("CLAUDE/QA.md", "See [gone](./Gone.md).\n")
        findings, _ = self.check("CLAUDE/QA.md")
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["problem"], "target does not exist")

    def test_that_finding_fires_even_when_the_file_is_sitting_there(self):
        """The case a plain existence check cannot see.

        The file is present locally and absent from every clean checkout, so an
        `os.path.exists` gate passes here and the link is broken everywhere
        else. That divergence is this plan's subject.
        """
        self.write("untracked/notes.md", "# Notes\n")
        self.write("CLAUDE/QA.md", "See [scratch](../untracked/notes.md).\n")
        findings, _ = self.check("CLAUDE/QA.md")
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["problem"],
                         "target is not tracked by this repository")

    def test_a_broken_link_in_our_own_tree_is_still_reported(self):
        """The control that makes every case above worth having."""
        self.write("CLAUDE/QA.md", "See [gone](./NoSuchFile.md).\n")
        findings, vendored = self.check("CLAUDE/QA.md")
        self.assertEqual(self.vendored_total(vendored), 0)
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["problem"], "target does not exist")

    def test_the_other_links_in_a_generated_file_are_still_checked(self):
        """Per LINK, not per file — the exemption is the target's, not the file's."""
        self.write(".claude/rules/agent-docs.md",
                   "See [roles](../hooks-daemon/CLAUDE/DirectoryRoles.md)\n"
                   "and [gone](../../CLAUDE/NoSuchFile.md).\n")
        findings, vendored = self.check(".claude/rules/agent-docs.md")
        self.assertEqual(self.vendored_total(vendored), 1)
        self.assertEqual([f["target"] for f in findings],
                         ["../../CLAUDE/NoSuchFile.md"])

    def test_naming_a_vendored_repo_in_the_link_text_exempts_nothing(self):
        """The resolved target decides, not the words around it."""
        self.write("CLAUDE/QA.md",
                   "See [the hooks-daemon docs](./NoSuchFile.md).\n")
        findings, vendored = self.check("CLAUDE/QA.md")
        self.assertEqual(self.vendored_total(vendored), 0)
        self.assertEqual(len(findings), 1)

    def test_vendored_skips_are_counted_so_the_exemption_is_visible(self):
        """An exemption nobody can count is a skip nobody can see.

        `version-pins` reports `COVERAGE: 9 of 9` for the same reason: a gate
        that quietly stopped checking something reads exactly like a gate that
        checked it and found nothing.
        """
        self.write(".claude/rules/agent-docs.md",
                   "[a](../hooks-daemon/A.md) [b](../hooks-daemon/B.md)\n")
        self.write(".claude/rules/plan-dir.md", "[c](../hooks-daemon/C.md)\n")
        _, vendored = self.check(".claude/rules/agent-docs.md",
                                 ".claude/rules/plan-dir.md")
        self.assertEqual(self.vendored_total(vendored), 3)

    def test_a_target_above_the_repo_root_is_not_read_as_inside_it(self):
        """`os.path.relpath` answers `../…` for an escape; it must not prefix-match."""
        self.write("CLAUDE/QA.md", "[up](../../elsewhere/README.md)\n")
        findings, vendored = self.check("CLAUDE/QA.md")
        self.assertEqual(self.vendored_total(vendored), 0)
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["problem"], "target does not exist")

    def test_an_escaping_target_that_EXISTS_is_still_a_finding(self):
        """The case the test above cannot reach, and the branch it distinguishes.

        Above, the escaping target is absent, so the existence branch answers
        first and `repo_relative`'s `None` is never consulted. A target that
        escapes the repository and IS on the disk takes the other branch — it
        exists, and `ls-files` can say nothing about a path outside the
        repository, so the honest verdict is untracked rather than a pass.
        Under `check-ignore` this link passed.
        """
        outer = tempfile.TemporaryDirectory()
        self.addCleanup(outer.cleanup)
        repo = os.path.join(outer.name, "repo")
        os.makedirs(os.path.join(outer.name, "elsewhere"))
        with open(os.path.join(outer.name, "elsewhere", "README.md"), "w",
                  encoding="utf-8") as handle:
            handle.write("# Outside\n")
        os.makedirs(os.path.join(repo, "CLAUDE"))
        subprocess.run(["git", "init", "-q", repo], check=True)
        with open(os.path.join(repo, "CLAUDE", "QA.md"), "w",
                  encoding="utf-8") as handle:
            handle.write("[out](../../elsewhere/README.md)\n")

        findings, vendored = link_check.check_links(repo, ["CLAUDE/QA.md"])
        self.assertEqual(self.vendored_total(vendored), 0)
        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0]["problem"],
                         "target is not tracked by this repository")

    def test_a_tree_git_cannot_answer_for_fails_loudly(self):
        """No git checkout means the TRACKEDNESS question has no answer.

        Returning an empty tracked set would say "this repository tracks
        nothing", turning every link into a finding — a confident verdict
        derived from a check that did not run, which is the shape this gate
        exists to remove. It raises instead.

        This asserts the RAISE only. What turns the raise into the gate's exit 2
        is two hops in `scripts/qa-docs.bash` — the interpreter exits 1, which
        that script treats as its findings status, and the payload validation
        then refuses a traceback and exits 2. Neither hop is covered here, and
        saying so is better than a docstring that claims the whole chain. Giving
        `qa-docs.bash` a root argument would let a test drive it end to end.
        """
        plain = tempfile.TemporaryDirectory()
        self.addCleanup(plain.cleanup)
        with open(os.path.join(plain.name, "README.md"), "w",
                  encoding="utf-8") as handle:
            handle.write("[x](./NoSuchFile.md)\n")
        with self.assertRaises(RuntimeError) as raised:
            link_check.check_links(plain.name, ["README.md"])
        self.assertIn("ls-files", str(raised.exception))


class TestTheUndeclaredRepositoryHint(_GitTree):
    """An untracked target sitting inside a repo nobody declared.

    This is a HINT attached to a finding, not a finding of its own. The version
    that swept the tree for undeclared repositories was written first and threw
    it away: it reported eleven, every one a gitignored acceptance-run fixture
    under a completed plan. Every vendored repo is gitignored, so a sweep either
    skips all of them or drags in every stray clone — the population it can see
    is not the population that matters.
    """

    def vendor(self, rel):
        os.makedirs(os.path.join(self.root, rel, ".git"))

    def test_an_untracked_target_inside_a_nested_repo_says_so(self):
        self.vendor("third_party/somelib")
        self.write("third_party/somelib/README.md", "# Lib\n")
        self.write("CLAUDE/QA.md", "[x](../third_party/somelib/README.md)\n")
        findings, _ = self.check("CLAUDE/QA.md")
        self.assertEqual(len(findings), 1)
        self.assertIn("a repository is nested at third_party/somelib",
                      findings[0]["problem"])
        self.assertIn("_VENDORED_ROOTS", findings[0]["problem"])

    def test_an_untracked_target_with_no_nested_repo_keeps_the_plain_wording(self):
        self.write("untracked/notes.md", "# Notes\n")
        self.write("CLAUDE/QA.md", "[x](../untracked/notes.md)\n")
        findings, _ = self.check("CLAUDE/QA.md")
        self.assertEqual(findings[0]["problem"],
                         "target is not tracked by this repository")

    def test_a_git_FILE_counts_as_a_repository(self):
        """A linked worktree leaves a `.git` FILE, not a directory."""
        os.makedirs(os.path.join(self.root, "third_party", "wt"))
        with open(os.path.join(self.root, "third_party", "wt", ".git"), "w",
                  encoding="utf-8") as handle:
            handle.write("gitdir: /elsewhere\n")
        self.write("third_party/wt/README.md", "# WT\n")
        self.write("CLAUDE/QA.md", "[x](../third_party/wt/README.md)\n")
        findings, _ = self.check("CLAUDE/QA.md")
        self.assertIn("a repository is nested at third_party/wt",
                      findings[0]["problem"])

    def test_the_hint_is_absent_rather_than_wrong_when_there_is_no_repo(self):
        """A hint that guessed would be worse than none.

        The file is present and untracked, so the finding stands; there is no
        nested repository to name, so nothing is named. On a clean checkout the
        target is absent instead and the finding becomes `target does not
        exist` — a different sentence, the same verdict.
        """
        self.write("third_party/somelib/README.md", "# Lib\n")
        self.write("CLAUDE/QA.md", "[x](../third_party/somelib/README.md)\n")
        findings, _ = self.check("CLAUDE/QA.md")
        self.assertEqual(findings[0]["problem"],
                         "target is not tracked by this repository")

    def test_the_repos_own_git_directory_is_not_a_nested_repository(self):
        self.assertIsNone(
            link_check.nested_repository_for(self.root, "CLAUDE/QA.md"))


class TestTheScanPopulationIsCounted(_GitTree):
    """The TARGETS ask trackedness; the DOCUMENTS are whatever `os.walk` finds.

    So an in-scope markdown file nobody committed is scanned here and absent in
    CI, and a broken link in it fails here and passes there — Cause A's
    direction reversed. It is latent (every in-scope document in this checkout
    is tracked) and it is not worth excluding untracked documents: a broken link
    in a file you have not committed yet is a true finding, and catching it
    before the commit is the point.

    What was missing is the denominator. `qa-helper-tests.bash` prints `65
    modules (65 tracked)` for exactly this reason and the lesson was not carried
    to the gate two rows below it.
    """

    def test_every_tracked_document_counts(self):
        self.write("docs/a.md", "# A\n")
        self.write("docs/b.md", "# B\n")
        subprocess.run(["git", "-C", self.root, "add", "docs"], check=True)
        self.assertEqual(
            link_check.tracked_scope_count(self.root, ["docs/a.md", "docs/b.md"]), 2)

    def test_an_uncommitted_document_is_scanned_but_not_counted_as_tracked(self):
        self.write("docs/a.md", "# A\n")
        subprocess.run(["git", "-C", self.root, "add", "docs/a.md"], check=True)
        self.write("docs/b.md", "# B\n")
        self.assertEqual(
            link_check.tracked_scope_count(self.root, ["docs/a.md", "docs/b.md"]), 1)

    def test_no_tracked_documents_counts_zero_rather_than_failing(self):
        self.write("docs/a.md", "# A\n")
        self.assertEqual(link_check.tracked_scope_count(self.root, ["docs/a.md"]), 0)


class TestTheVendoredWarningBlock(unittest.TestCase):
    """The ⚠ block, composed here so that composing it is not a rare event.

    The obvious home for it is `scripts/qa-docs.bash`, behind `if [[ "$V_BROKEN"
    -gt 0 ]]`. That branch is unreachable in practice: firing it needs a vendored
    repository present AND a stale pointer into it, which no invocation of that
    script has ever had. A formatter interpolating `.file`, `.line` and
    `.target` from entries nothing ever feeds it would run for the first time on
    the day it is most needed, watched by nobody.

    A list the caller prints unconditionally has no branch: `jq -r
    '.vendored_warning[]'` runs on every QA run and prints nothing when the list
    is empty, so a shape error surfaces at once rather than eventually.
    """

    @staticmethod
    def entry(file="CLAUDE/QA.md", line=12, target="../.claude/hooks-daemon/x.md"):
        return {"file": file, "line": line, "target": target, "problem": "gone"}

    def test_nothing_broken_produces_no_lines(self):
        lines = link_check.vendored_warning_lines(
            {"ok": 3, "unverifiable": 8, "broken": []})
        self.assertEqual(lines, [])

    def test_the_header_is_a_stage_line_the_verdict_parser_recognises(self):
        lines = link_check.vendored_warning_lines(
            {"ok": 0, "unverifiable": 0, "broken": [self.entry()]})
        self.assertRegex(lines[0], r"^⚠ docs: ")

    def test_the_header_counts_the_broken_links(self):
        broken = [self.entry(line=n) for n in (3, 9, 40)]
        lines = link_check.vendored_warning_lines(
            {"ok": 0, "unverifiable": 0, "broken": broken})
        self.assertIn("3 link(s)", lines[0])

    def test_each_broken_link_gets_a_line_naming_file_line_and_target(self):
        lines = link_check.vendored_warning_lines(
            {"ok": 0, "unverifiable": 0,
             "broken": [self.entry(file="docs/README.md", line=7,
                                   target="../.claude/hooks-daemon/gone.md")]})
        self.assertEqual(len(lines), 2)
        self.assertIn("docs/README.md:7", lines[1])
        self.assertIn("../.claude/hooks-daemon/gone.md", lines[1])

    def test_the_block_is_one_stage_to_the_real_verdict_parser(self):
        """Parsed by `verdicts.parse` itself, not by a copy of its regex.

        The first version of this test cited `verdicts.STAGE` in its docstring
        and then asserted a hand-written `r"^\\s*[✓✗⚠] "`. A pattern and the
        thing it describes that nothing compares will drift — which is the
        `nokill-containerwatch` argument, inside the test written to prevent it.
        Importing the parser also covers `SYMBOL_BEARING`, which the copy did
        not describe at all.

        The property: the whole block is ONE stage named `docs`, however many
        broken links it lists. A detail line read as a stage would invent a gate
        per broken link and inflate the census.

        TWO ASSERTIONS, because the parser one does not cover the indentation.
        Measured: stripping the indent leaves the parse unchanged, since a
        detail line carries no stage symbol either way — what the parser
        discriminates is a detail line that BEGINS with one, which gives
        `['⚠','⚠','⚠','✓']`. The indent is still load-bearing (`SYMBOL_BEARING`
        wants the symbol at line start), so it is asserted directly rather than
        left to a parser that would not notice it going.
        """
        broken = [self.entry(file=name, line=n)
                  for name, n in (("docs/a.md", 1), ("CLAUDE/b.md", 2))]
        lines = link_check.vendored_warning_lines(
            {"ok": 0, "unverifiable": 0, "broken": broken})
        for detail in lines[1:]:
            self.assertTrue(detail.startswith("    "), detail)

        text = "\n".join(lines + ["✓ docs: 71 files OK — VENDORED: 0, 0, 2"]) + "\n"
        parsed = verdicts.parse(text)
        self.assertEqual(sorted(parsed.stages), ["docs"])
        self.assertEqual([entry.symbol for entry in parsed.stages["docs"]],
                         ["⚠", "✓"])

    def test_the_entries_keep_their_order(self):
        broken = [self.entry(file=name) for name in ("a.md", "b.md", "c.md")]
        lines = link_check.vendored_warning_lines(
            {"ok": 0, "unverifiable": 0, "broken": broken})
        self.assertEqual([line.split(":")[0].strip() for line in lines[1:]],
                         ["a.md", "b.md", "c.md"])


if __name__ == "__main__":
    unittest.main()
