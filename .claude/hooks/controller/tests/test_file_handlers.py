#!/usr/bin/env python3
"""Comprehensive unit tests for file_handlers module.

Tests ALL file-related handlers against legacy hook behavior to ensure 100% parity.
"""

import unittest
import sys
import os

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import Handler, HookResult
from handlers.pre_tool_use import (
    EslintDisableHandler,
    PlanTimeEstimatesHandler,
    MarkdownOrganizationHandler,
    ClaudeReadmeHandler,
    WebSearchYearHandler,
    OfficialPlanCommandHandler,
)


class TestEslintDisableHandler(unittest.TestCase):
    """Test ESLint disable comment blocking."""

    def setUp(self):
        self.handler = EslintDisableHandler()

    def test_matches_eslint_disable(self):
        """Should match eslint-disable comments."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/test.ts",
                "content": "// eslint-disable-next-line no-console\nconsole.log('test');"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_ts_ignore(self):
        """Should match @ts-ignore comments."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/test.ts",
                "content": "// @ts-ignore\nconst x: any = 'test';"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_ts_nocheck(self):
        """Should match @ts-nocheck comments."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/test.ts",
                "content": "// @ts-nocheck\nconst x = 'test';"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_block_comment_eslint_disable(self):
        """Should match block comment eslint-disable."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/test.ts",
                "content": "/* eslint-disable no-console */\nconsole.log('test');"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_no_match_clean_code(self):
        """Should not match clean code without suppressions."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/test.ts",
                "content": "const x = 'test';\nconsole.log(x);"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_non_code_files(self):
        """Should not match non-code file extensions."""
        non_code_files = [
            "/workspace/README.md",
            "/workspace/config.json",
            "/workspace/data.txt",
            "/workspace/style.css",
        ]
        for file_path in non_code_files:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": file_path,
                    "content": "// eslint-disable"
                }
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should not check {file_path}"
            )

    def test_no_match_node_modules(self):
        """Should skip node_modules directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/node_modules/package/index.ts",
                "content": "// eslint-disable"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_dist_directory(self):
        """Should skip dist/build artifacts."""
        paths = [
            "/workspace/dist/bundle.js",
            "/workspace/.build/output.js",
            "/workspace/coverage/report.js",
        ]
        for file_path in paths:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": file_path,
                    "content": "// eslint-disable"
                }
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should skip {file_path}"
            )

    def test_matches_edit_tool(self):
        """Should also check Edit tool operations."""
        hook_input = {
            "tool_name": "Edit",
            "tool_input": {
                "file_path": "/workspace/src/test.ts",
                "old_string": "const x = 1;",
                "new_string": "// eslint-disable-next-line\nconst x = 1;"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_handle_blocks_with_clear_message(self):
        """Should block with clear explanation."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/test.ts",
                "content": "// eslint-disable no-console\nconsole.log('test');"
            }
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("BLOCKED", result.reason)
        self.assertIn("eslint-disable", result.reason)
        self.assertIn("Fix the underlying issue", result.reason)

    def test_priority(self):
        """Handler should have priority 30."""
        self.assertEqual(self.handler.priority, 30)

    def test_name(self):
        """Handler should have correct name."""
        self.assertEqual(self.handler.name, "enforce-no-eslint-disable")


class TestPlanTimeEstimatesHandler(unittest.TestCase):
    """Test blocking time estimates in plan documents."""

    def setUp(self):
        self.handler = PlanTimeEstimatesHandler()

    def test_matches_hour_estimate(self):
        """Should match hour estimates."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/001-test/PLAN.md",
                "content": "**Estimated Effort**: 5 hours"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_day_estimate(self):
        """Should match day estimates."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/002-feature/PLAN.md",
                "content": "This will take 3 days to complete."
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_timeline_with_numbers(self):
        """Should match timeline fields with numbers."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/003-bug/PLAN.md",
                "content": "Timeline: 2 weeks"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_estimated_time(self):
        """Should match 'estimated time' phrases."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/004-refactor/PLAN.md",
                "content": "Estimated time: 10 hours"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_no_match_non_plan_files(self):
        """Should not check non-plan markdown files."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/README.md",
                "content": "This will take 5 hours."
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_plan_without_estimates(self):
        """Should not match plan without time estimates."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/005-clean/PLAN.md",
                "content": "## Tasks\n\n- [ ] Implement feature\n- [ ] Write tests"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_docs_directory(self):
        """Should not check docs directory."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/docs/guide.md",
                "content": "This guide takes 2 hours to read."
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handle_blocks_estimates(self):
        """Should block with explanation."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/006-test/PLAN.md",
                "content": "Estimated: 8 hours"
            }
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("BLOCKED", result.reason)
        self.assertIn("Time estimates not allowed", result.reason)

    def test_priority(self):
        """Handler should have priority 40."""
        self.assertEqual(self.handler.priority, 40)

    def test_name(self):
        """Handler should have correct name."""
        self.assertEqual(self.handler.name, "block-plan-time-estimates")


class TestMarkdownOrganizationHandler(unittest.TestCase):
    """Test markdown file organization rules.

    CRITICAL: This handler must allow CLAUDE.md and README.md in ANY directory.
    """

    def setUp(self):
        self.handler = MarkdownOrganizationHandler()

    def test_allows_plan_directory(self):
        """Should allow markdown in Plan directories."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/001-test/notes.md",
                "content": "Plan notes"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_claude_root(self):
        """Should allow markdown in CLAUDE root."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Guide.md",
                "content": "Guide content"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_docs_directory(self):
        """Should allow markdown in docs/."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/docs/api.md",
                "content": "API documentation"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_untracked_directory(self):
        """Should allow markdown in untracked/."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/untracked/scratch.md",
                "content": "Temporary notes"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_allows_claude_md_anywhere(self):
        """CRITICAL: Should allow CLAUDE.md in ANY directory."""
        claude_md_paths = [
            "/workspace/src/CLAUDE.md",
            "/workspace/.claude/hooks/CLAUDE.md",
            "/workspace/scripts/utils/CLAUDE.md",
            "/workspace/tests/CLAUDE.md",
        ]
        for file_path in claude_md_paths:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": file_path,
                    "content": "Context for this directory"
                }
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"CLAUDE.md should be allowed at {file_path}"
            )

    def test_allows_readme_md_anywhere(self):
        """CRITICAL: Should allow README.md in ANY directory."""
        readme_paths = [
            "/workspace/src/README.md",
            "/workspace/.claude/hooks/README.md",
            "/workspace/scripts/README.md",
            "/workspace/README.md",
        ]
        for file_path in readme_paths:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": file_path,
                    "content": "Documentation for this directory"
                }
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"README.md should be allowed at {file_path}"
            )

    def test_allows_page_research_files(self):
        """Should allow *-research.md files co-located with pages."""
        research_paths = [
            "/workspace/src/pages/technologies/languages/languages-research.md",
            "/workspace/src/pages/home-research.md",
            "/workspace/src/pages/services/consulting/consulting-research.md",
            "/workspace/src/pages/about/team-research.md",
        ]
        for file_path in research_paths:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": file_path,
                    "content": "Research data for page"
                }
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Page research file should be allowed at {file_path}"
            )

    def test_allows_page_rules_files(self):
        """Should allow *-rules.md files co-located with pages."""
        rules_paths = [
            "/workspace/src/pages/technologies/languages/languages-rules.md",
            "/workspace/src/pages/home-rules.md",
            "/workspace/src/pages/services/consulting/consulting-rules.md",
        ]
        for file_path in rules_paths:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": file_path,
                    "content": "Content rules for page"
                }
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Page rules file should be allowed at {file_path}"
            )

    def test_blocks_plan_root_files(self):
        """REGRESSION TEST: Should block files in CLAUDE/Plan/ without numbered subdirectory."""
        # Legacy hook requires: CLAUDE/Plan/001-name/file.md
        # Simple 'in' check would incorrectly allow: CLAUDE/Plan/file.md
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/notes.md",
                "content": "Random notes"
            }
        }
        self.assertTrue(
            self.handler.matches(hook_input),
            "Files in CLAUDE/Plan/ root should be blocked (must use numbered subdirectories)"
        )

    def test_allows_plan_numbered_subdirectory(self):
        """Should allow files in properly numbered plan subdirectories."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/Plan/001-test-plan/notes.md",
                "content": "Plan notes"
            }
        }
        self.assertFalse(
            self.handler.matches(hook_input),
            "Files in numbered plan subdirectories should be allowed"
        )

    def test_blocks_claude_arbitrary_subdirectories(self):
        """REGRESSION TEST: Should block files in arbitrary CLAUDE subdirectories."""
        # Legacy hook allows only: CLAUDE/ (root), CLAUDE/Plan/, CLAUDE/research/, CLAUDE/Sitemap/
        # Simple 'in' check would incorrectly allow: CLAUDE/anything/file.md
        disallowed_paths = [
            "/workspace/CLAUDE/subdir/notes.md",
            "/workspace/CLAUDE/random/documentation.md",
            "/workspace/CLAUDE/temp/analysis.md",
        ]
        for file_path in disallowed_paths:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": file_path,
                    "content": "Notes"
                }
            }
            self.assertTrue(
                self.handler.matches(hook_input),
                f"Arbitrary CLAUDE subdirectories should be blocked: {file_path}"
            )

    def test_allows_claude_root_files(self):
        """Should allow files directly in CLAUDE root (no subdirectory)."""
        root_files = [
            "/workspace/CLAUDE/Build.md",
            "/workspace/CLAUDE/Architecture.md",
            "/workspace/CLAUDE/PlanWorkflow.md",
        ]
        for file_path in root_files:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": file_path,
                    "content": "Documentation"
                }
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"CLAUDE root files should be allowed: {file_path}"
            )

    def test_blocks_wrong_location(self):
        """Should block markdown in disallowed locations."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/notes.md",
                "content": "Random notes"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_blocks_root_directory(self):
        """Should block random markdown in root (except README.md)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/NOTES.md",
                "content": "Notes"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_handle_suggests_alternatives(self):
        """Should suggest correct locations."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/notes.md",
                "content": "Notes"
            }
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("WRONG LOCATION", result.reason)
        self.assertIn("CLAUDE/Plan/", result.reason)
        self.assertIn("untracked/", result.reason)

    def test_priority(self):
        """Handler should have priority 35."""
        self.assertEqual(self.handler.priority, 35)

    def test_name(self):
        """Handler should have correct name."""
        self.assertEqual(self.handler.name, "enforce-markdown-organization")


class TestClaudeReadmeHandler(unittest.TestCase):
    """Test CLAUDE.md content validation."""

    def setUp(self):
        self.handler = ClaudeReadmeHandler()

    def test_allows_instructions(self):
        """Should allow pure instruction content."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/src/CLAUDE.md",
                "content": "# Instructions\n\nUse this pattern when implementing features."
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_blocks_code_blocks(self):
        """Should block code blocks in CLAUDE.md."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/CLAUDE.md",
                "content": "# Guide\n\n```python\nprint('hello')\n```"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_only_checks_claude_md(self):
        """Should only check files named CLAUDE.md."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/docs/guide.md",
                "content": "```python\ncode here\n```"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handle_explains_rules(self):
        """Should explain CLAUDE.md content rules."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/CLAUDE/CLAUDE.md",
                "content": "```bash\nls -la\n```"
            }
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("Invalid content", result.reason)
        self.assertIn("High-level context", result.reason)

    def test_priority(self):
        """Handler should have priority 45."""
        self.assertEqual(self.handler.priority, 45)

    def test_name(self):
        """Handler should have correct name."""
        self.assertEqual(self.handler.name, "validate-claude-readme-content")


class TestWebSearchYearHandler(unittest.TestCase):
    """Test WebSearch year validation."""

    def setUp(self):
        self.handler = WebSearchYearHandler()

    def test_matches_2024(self):
        """Should match searches with 2024."""
        hook_input = {
            "tool_name": "WebSearch",
            "tool_input": {
                "query": "best React practices 2024"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_2023(self):
        """Should match searches with 2023."""
        hook_input = {
            "tool_name": "WebSearch",
            "tool_input": {
                "query": "TypeScript 5.0 release 2023"
            }
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_no_match_current_year(self):
        """Should not match current year (2025)."""
        hook_input = {
            "tool_name": "WebSearch",
            "tool_input": {
                "query": "best practices 2025"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_no_year(self):
        """Should not match searches without year."""
        hook_input = {
            "tool_name": "WebSearch",
            "tool_input": {
                "query": "best React practices"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_non_websearch_tool(self):
        """Should only check WebSearch tool."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "echo 2024"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handle_suggests_current_year(self):
        """Should suggest using current year."""
        hook_input = {
            "tool_name": "WebSearch",
            "tool_input": {
                "query": "best practices 2024"
            }
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("outdated year", result.reason)
        self.assertIn("2025", result.reason)

    def test_priority(self):
        """Handler should have priority 55."""
        self.assertEqual(self.handler.priority, 55)

    def test_name(self):
        """Handler should have correct name."""
        self.assertEqual(self.handler.name, "validate-websearch-year")


class TestOfficialPlanCommandHandler(unittest.TestCase):
    """Test enforcement of /plan command for plan creation."""

    def setUp(self):
        self.handler = OfficialPlanCommandHandler()

    def test_matches_adhoc_plan_lookup(self):
        """Should match ad-hoc plan number lookup commands."""
        test_commands = [
            "ls CLAUDE/Plan/0*",
            "ls -d CLAUDE/Plan/[0-9]*",
            "ls CLAUDE/Plan | grep '^[0-9]'",
            "cd CLAUDE/Plan && ls",
            "find CLAUDE/Plan -maxdepth 1 -type d",  # Wrong maxdepth
        ]

        for command in test_commands:
            with self.subTest(command=command):
                hook_input = {
                    "tool_name": "Bash",
                    "tool_input": {"command": command}
                }
                self.assertTrue(
                    self.handler.matches(hook_input),
                    f"Should match ad-hoc command: {command}"
                )

    def test_no_match_official_command(self):
        """Should NOT match the official plan numbering command."""
        official_command = "find CLAUDE/Plan -maxdepth 2 -type d -name '[0-9]*' | sed 's|.*/\\([0-9]\\{3\\}\\).*|\\1|' | sort -n | tail -1"
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": official_command}
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_non_plan_commands(self):
        """Should NOT match commands that don't reference Plan directory."""
        test_commands = [
            "ls CLAUDE/Sitemap",
            "find src/ -name '*.tsx'",
            "git status",
        ]

        for command in test_commands:
            with self.subTest(command=command):
                hook_input = {
                    "tool_name": "Bash",
                    "tool_input": {"command": command}
                }
                self.assertFalse(self.handler.matches(hook_input))

    def test_handle_suggests_official_command(self):
        """Should suggest using the official plan numbering command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "ls CLAUDE/Plan/0*"}
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("OFFICIAL COMMAND", result.reason)
        self.assertIn("find CLAUDE/Plan -maxdepth 2", result.reason)
        self.assertIn("CLAUDE/Plan/CLAUDE.md", result.reason)

    def test_priority(self):
        """Handler should have priority 25."""
        self.assertEqual(self.handler.priority, 25)

    def test_name(self):
        """Handler should have correct name."""
        self.assertEqual(self.handler.name, "enforce-official-plan-command")


class TestMarkdownOrganizationAgentFiles(unittest.TestCase):
    """Test that agent definition files in .claude/agents/ are allowed.

    BUG FIX: The handler was incorrectly blocking writes to .claude/agents/*.md files.
    Agent files should be allowed per the is_adhoc_instruction_file() logic.
    """

    def setUp(self):
        self.handler = MarkdownOrganizationHandler()

    def test_allows_agent_files_absolute_path(self):
        """Should allow agent definition files with absolute path."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/workspace/.claude/agents/test-agent.md",
                "content": "Agent definition content"
            }
        }
        self.assertFalse(
            self.handler.matches(hook_input),
            "Agent files in .claude/agents/ should be allowed (absolute path)"
        )

    def test_allows_agent_files_relative_path(self):
        """Should allow agent definition files with relative path."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": ".claude/agents/test-agent.md",
                "content": "Agent definition content"
            }
        }
        self.assertFalse(
            self.handler.matches(hook_input),
            "Agent files in .claude/agents/ should be allowed (relative path)"
        )

    def test_allows_agent_files_workspace_normalized(self):
        """Should allow agent files when path is normalized with workspace prefix."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "workspace/.claude/agents/hooks-specialist.md",
                "content": "Agent definition content"
            }
        }
        self.assertFalse(
            self.handler.matches(hook_input),
            "Agent files should be allowed when workspace prefix is present"
        )

    def test_allows_various_agent_filenames(self):
        """Should allow various agent file names in .claude/agents/."""
        agent_paths = [
            "/workspace/.claude/agents/hooks-specialist.md",
            "/workspace/.claude/agents/typescript-refactor.md",
            "/workspace/.claude/agents/qa-fixer.md",
            ".claude/agents/content-editor.md",
            ".claude/agents/brand-strategist.md",
        ]
        for file_path in agent_paths:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": file_path,
                    "content": "Agent definition"
                }
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Agent file should be allowed: {file_path}"
            )


class TestHandlerEdgeCases(unittest.TestCase):
    """Test edge cases across all file handlers."""

    def test_empty_file_path(self):
        """All handlers should handle empty file paths gracefully."""
        handlers = [
            EslintDisableHandler(),
            PlanTimeEstimatesHandler(),
            MarkdownOrganizationHandler(),
            ClaudeReadmeHandler(),
        ]

        for handler in handlers:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": "",
                    "content": "test"
                }
            }
            # Should not crash - either match or not match
            try:
                result = handler.matches(hook_input)
                self.assertIsInstance(result, bool)
            except Exception as e:
                self.fail(f"{handler.name} crashed on empty file_path: {e}")

    def test_empty_content(self):
        """Handlers should handle empty content gracefully."""
        handlers = [
            (EslintDisableHandler(), "/workspace/src/test.ts"),
            (PlanTimeEstimatesHandler(), "/workspace/CLAUDE/Plan/001-test/PLAN.md"),
            (ClaudeReadmeHandler(), "/workspace/CLAUDE/CLAUDE.md"),
        ]

        for handler, file_path in handlers:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": file_path,
                    "content": ""
                }
            }
            # Should not crash
            try:
                result = handler.matches(hook_input)
                self.assertIsInstance(result, bool)
            except Exception as e:
                self.fail(f"{handler.name} crashed on empty content: {e}")

    def test_missing_tool_input(self):
        """Handlers should handle missing tool_input gracefully."""
        handlers = [
            EslintDisableHandler(),
            PlanTimeEstimatesHandler(),
            MarkdownOrganizationHandler(),
            WebSearchYearHandler(),
        ]

        for handler in handlers:
            hook_input = {
                "tool_name": "Write",
                # Missing tool_input
            }
            # Should not crash
            try:
                result = handler.matches(hook_input)
                self.assertIsInstance(result, bool)
            except Exception as e:
                self.fail(f"{handler.name} crashed on missing tool_input: {e}")

    def test_case_insensitive_file_extensions(self):
        """Handlers should handle case-insensitive extensions."""
        handler = EslintDisableHandler()

        extensions = [".ts", ".TS", ".Ts", ".tS"]
        for ext in extensions:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": f"/workspace/src/test{ext}",
                    "content": "// eslint-disable"
                }
            }
            self.assertTrue(
                handler.matches(hook_input),
                f"Should match {ext} extension"
            )


class TestLegacyParityChecks(unittest.TestCase):
    """Verify 100% parity with legacy hook behavior.

    These tests encode specific behaviors from legacy hooks that MUST be preserved.
    """

    def test_markdown_organization_claude_md_parity(self):
        """CRITICAL: Verify CLAUDE.md allowed anywhere (legacy line 149-151)."""
        handler = MarkdownOrganizationHandler()

        # Legacy hook explicitly allows CLAUDE.md in ANY directory
        test_paths = [
            "/workspace/.claude/hooks/CLAUDE.md",
            "/workspace/src/components/ui/CLAUDE.md",
            "/workspace/scripts/build/CLAUDE.md",
        ]

        for path in test_paths:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": path,
                    "content": "Instructions"
                }
            }
            self.assertFalse(
                handler.matches(hook_input),
                f"LEGACY PARITY BROKEN: CLAUDE.md should be allowed at {path}"
            )

    def test_markdown_organization_readme_md_parity(self):
        """CRITICAL: Verify README.md allowed anywhere (legacy line 149-151)."""
        handler = MarkdownOrganizationHandler()

        # Legacy hook explicitly allows README.md in ANY directory
        test_paths = [
            "/workspace/.claude/hooks/README.md",
            "/workspace/src/utils/README.md",
            "/workspace/tests/integration/README.md",
        ]

        for path in test_paths:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": path,
                    "content": "Documentation"
                }
            }
            self.assertFalse(
                handler.matches(hook_input),
                f"LEGACY PARITY BROKEN: README.md should be allowed at {path}"
            )

    def test_eslint_disable_all_patterns_from_legacy(self):
        """Verify all patterns from legacy hook (lines 14-20) are blocked."""
        handler = EslintDisableHandler()

        # All patterns from legacy SUPPRESSION_PATTERNS
        patterns = [
            "// eslint-disable",
            "// eslint-disable-line",
            "// eslint-disable-next-line",
            "// @ts-ignore",
            "// @ts-expect-error",
            "/* eslint-disable */",
        ]

        for pattern in patterns:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": "/workspace/src/test.ts",
                    "content": f"{pattern}\nconst x = 1;"
                }
            }
            self.assertTrue(
                handler.matches(hook_input),
                f"LEGACY PARITY BROKEN: Should block pattern '{pattern}'"
            )

    def test_plan_estimates_legacy_patterns(self):
        """Verify time estimate patterns from legacy hook (lines 42-72)."""
        handler = PlanTimeEstimatesHandler()

        # Patterns from legacy TIME_ESTIMATE_PATTERNS
        test_cases = [
            "**Estimated Effort**: 5 hours",
            "Estimated Effort: 3 days",
            "Time estimated: 2 weeks",
            "**Total Estimated Time**: 10 hours",
            "Target Completion: 2025-12-31",
            "Completion: 2025-01-15",
        ]

        for content in test_cases:
            hook_input = {
                "tool_name": "Write",
                "tool_input": {
                    "file_path": "/workspace/CLAUDE/Plan/001-test/PLAN.md",
                    "content": content
                }
            }
            self.assertTrue(
                handler.matches(hook_input),
                f"LEGACY PARITY BROKEN: Should block '{content}'"
            )

    def test_websearch_year_legacy_range(self):
        """Verify year range from legacy hook (line 58)."""
        handler = WebSearchYearHandler()

        # Legacy: r'\b(19\d{2}|20\d{2})\b' with check against current year
        outdated_years = ["2020", "2021", "2022", "2023", "2024"]

        for year in outdated_years:
            hook_input = {
                "tool_name": "WebSearch",
                "tool_input": {
                    "query": f"best practices {year}"
                }
            }
            self.assertTrue(
                handler.matches(hook_input),
                f"LEGACY PARITY BROKEN: Should block year {year}"
            )


if __name__ == '__main__':
    # Run tests with verbose output
    unittest.main(verbosity=2)
