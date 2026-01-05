#!/usr/bin/env python3
"""Unit tests for AdHocScriptHandler."""

import unittest
import sys
import os

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import Handler
from handlers.pre_tool_use import AdHocScriptHandler


class TestAdHocScriptHandler(unittest.TestCase):
    """Test suite for AdHocScriptHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = AdHocScriptHandler()

    # Test BLOCKED patterns (scripts with npm commands)

    def test_matches_npx_tsx_protected_script(self):
        """Should match npx tsx scripts/... for protected scripts."""
        protected_scripts = [
            "npx tsx scripts/llm-lint.ts",
            "npx tsx scripts/llm-type-check.ts",
            # extract-seo-metadata.ts is in ALLOWED_ADHOC (build pipeline script)
            "npx tsx scripts/generate-screenshots.ts",
            "npx tsx scripts/qa.ts",
            "npx tsx scripts/find-unused-exports.ts",
        ]

        for command in protected_scripts:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command}
            }
            self.assertTrue(
                self.handler.matches(hook_input),
                f"Should match protected script: {command}"
            )

    def test_matches_node_scripts_protected_script(self):
        """Should match node scripts/... for protected scripts."""
        command = "node scripts/llm-lint.ts"
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": command}
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_tsx_scripts_protected_script(self):
        """Should match tsx scripts/... for protected scripts."""
        command = "tsx scripts/llm-lint.ts"  # Changed from verify-build.ts (which is in ALLOWED_ADHOC)
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": command}
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_with_path_prefix(self):
        """Should match ./scripts/ and /workspace/scripts/ patterns."""
        test_cases = [
            "npx tsx ./scripts/llm-lint.ts",
            "npx tsx /workspace/scripts/llm-type-check.ts",
            "tsx ./scripts/qa.ts",
        ]

        for command in test_cases:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command}
            }
            self.assertTrue(
                self.handler.matches(hook_input),
                f"Should match with path prefix: {command}"
            )

    def test_matches_with_arguments(self):
        """Should match scripts with command-line arguments."""
        test_cases = [
            "npx tsx scripts/llm-page-errors.ts github-copilot",
            "tsx scripts/screenshot-page.ts /about mobile",
            "npx tsx scripts/smoke-test-pages-vite.ts src/pages/home.tsx",
        ]

        for command in test_cases:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command}
            }
            self.assertTrue(
                self.handler.matches(hook_input),
                f"Should match with arguments: {command}"
            )

    # Test ALLOWED patterns (scripts without npm commands)

    def test_no_match_allowed_adhoc_scripts(self):
        """Should NOT match scripts that have no npm command wrapper."""
        allowed_scripts = [
            "npx tsx scripts/debug-page.ts",
            "tsx scripts/eslint-wrapper.ts",
            "npx tsx scripts/generate-page-folders.ts",
            "tsx scripts/test-playwright-403.ts",
        ]

        for command in allowed_scripts:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command}
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should NOT match allowed ad-hoc script: {command}"
            )

    def test_no_match_library_files(self):
        """Should NOT match library/support files."""
        library_files = [
            "npx tsx scripts/lib/cache.ts",
            "tsx scripts/parsers/eslint-parse-summary.ts",
            "npx tsx scripts/types/cache.ts",
        ]

        for command in library_files:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command}
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should NOT match library file: {command}"
            )

    # Test npm run commands (should NOT match - handled by NpmCommandHandler)

    def test_no_match_npm_run_commands(self):
        """Should NOT match npm run commands - different handler handles these."""
        npm_commands = [
            "npm run llm:lint",
            "npm run build",
            "npm run qa",
            "npm run llm:type-check",
        ]

        for command in npm_commands:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command}
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should NOT match npm command: {command}"
            )

    # Test non-Bash tools

    def test_no_match_non_bash(self):
        """Should NOT match non-Bash tools."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {
                "file_path": "/test.txt",
                "content": "npx tsx scripts/llm-lint.ts"
            }
        }
        self.assertFalse(self.handler.matches(hook_input))

    # Test non-script commands

    def test_no_match_other_commands(self):
        """Should NOT match commands that don't invoke scripts."""
        other_commands = [
            "ls scripts/",
            "cat scripts/llm-lint.ts",
            "grep 'export' scripts/*.ts",
            "git status",
            "npm install",
        ]

        for command in other_commands:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command}
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should NOT match non-script command: {command}"
            )

    # Test edge cases

    def test_no_match_empty_command(self):
        """Should NOT match empty commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": ""}
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_missing_command(self):
        """Should NOT match when command key is missing."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {}
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_matches_case_insensitive(self):
        """Should match scripts regardless of case in npx/tsx."""
        test_cases = [
            "NPX tsx scripts/llm-lint.ts",
            "npx TSX scripts/llm-lint.ts",
            "NPX TSX scripts/llm-lint.ts",
        ]

        for command in test_cases:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command}
            }
            self.assertTrue(
                self.handler.matches(hook_input),
                f"Should match case-insensitive: {command}"
            )

    # Test handle() method

    def test_handle_blocks_with_npm_suggestion(self):
        """Should block and suggest npm command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npx tsx scripts/llm-lint.ts"}
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("BLOCKED", result.reason)
        self.assertIn("llm-lint.ts", result.reason)
        self.assertIn("npm run llm:lint", result.reason)

    def test_handle_suggests_correct_npm_command(self):
        """Should suggest the correct npm command for each script."""
        test_cases = [
            ("npx tsx scripts/llm-type-check.ts", "npm run llm:type-check"),
            ("tsx scripts/qa.ts", "npm run qa"),
            ("npx tsx scripts/generate-screenshots.ts", "npm run screenshots"),
            ("npx tsx scripts/find-unused-exports.ts", "npm run llm:unused-exports"),
            ("tsx scripts/llm-lint.ts", "npm run llm:lint"),  # Replaced extract-seo-metadata (in ALLOWED_ADHOC)
        ]

        for command, expected_suggestion in test_cases:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command}
            }
            result = self.handler.handle(hook_input)

            self.assertEqual(result.decision, "deny")
            self.assertIn(expected_suggestion, result.reason,
                         f"Should suggest {expected_suggestion} for {command}")

    def test_handle_includes_blocked_command(self):
        """Should include the blocked command in the reason."""
        command = "npx tsx scripts/llm-lint.ts"  # Changed from verify-build.ts (which is in ALLOWED_ADHOC)
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": command}
        }
        result = self.handler.handle(hook_input)

        self.assertIn(command, result.reason)

    def test_handle_explains_philosophy(self):
        """Should explain why ad-hoc script execution is blocked."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npx tsx scripts/llm-lint.ts"}
        }
        result = self.handler.handle(hook_input)

        self.assertIn("PHILOSOPHY", result.reason)
        self.assertIn("npm run", result.reason)
        self.assertIn("standardised", result.reason.lower())

    # Test priority and name

    def test_priority(self):
        """Handler should have priority 51 (after npm handler at 50)."""
        self.assertEqual(self.handler.priority, 51)

    def test_name(self):
        """Handler should have correct name."""
        self.assertEqual(self.handler.name, "prevent-adhoc-scripts")


if __name__ == '__main__':
    unittest.main()
