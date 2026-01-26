#!/usr/bin/env python3
"""Unit tests for bash_handlers module."""

import os
import sys
import unittest

# Add parent directory to path
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from front_controller import Handler
from handlers.pre_tool_use import (
    DestructiveGitHandler,
    GitStashHandler,
    NpmCommandHandler,
)


class TestDestructiveGitHandler(unittest.TestCase):
    """Test suite for DestructiveGitHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = DestructiveGitHandler()

    def test_matches_git_reset_hard(self):
        """Should match 'git reset --hard' commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git reset --hard HEAD~1"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_git_clean_f(self):
        """Should match 'git clean -f' commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git clean -fd"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_git_stash_drop(self):
        """Should match 'git stash drop' commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git stash drop stash@{0}"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_git_stash_clear(self):
        """Should match 'git stash clear' commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git stash clear"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_no_match_safe_git(self):
        """Should not match safe git commands."""
        safe_commands = [
            "git status",
            "git commit -m 'test'",
            "git push",
            "git pull",
            "git diff",
            "git log",
        ]

        for command in safe_commands:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command},
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should not match safe command: {command}",
            )

    def test_no_match_non_bash(self):
        """Should not match non-Bash tools."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/test.txt", "content": "git reset --hard"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_no_match_no_git(self):
        """Should not match commands without git."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "rm -rf /"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_handle_blocks_destructive_command(self):
        """Should return deny decision for destructive commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git reset --hard"},
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("BLOCKED", result.reason)
        self.assertIn("git reset --hard", result.reason)
        self.assertIn("destroys all uncommitted changes permanently", result.reason)

    def test_handle_provides_specific_reasons(self):
        """Should provide specific reasons for different destructive commands."""
        test_cases = [
            ("git reset --hard", "destroys all uncommitted changes permanently"),
            ("git clean -f", "permanently deletes untracked files"),
            ("git stash drop", "permanently destroys stashed changes"),
            ("git stash clear", "permanently destroys all stashed changes"),
        ]

        for command, expected_reason in test_cases:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command},
            }
            result = self.handler.handle(hook_input)
            self.assertIn(expected_reason, result.reason, f"Failed for command: {command}")

    def test_priority(self):
        """Handler should have priority 10 (runs first)."""
        self.assertEqual(self.handler.priority, 10)

    def test_name(self):
        """Handler should have correct name."""
        self.assertEqual(self.handler.name, "prevent-destructive-git")


class TestGitStashHandler(unittest.TestCase):
    """Test suite for GitStashHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = GitStashHandler()

    def test_matches_git_stash_push(self):
        """Should match 'git stash push' commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git stash push -m 'WIP'"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_git_stash_save(self):
        """Should match 'git stash save' commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git stash save 'work in progress'"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_no_match_git_stash_list(self):
        """Should not match safe stash operations like list."""
        safe_stash_commands = [
            "git stash list",
            "git stash show",
            "git stash apply",
            "git stash branch my-branch",
        ]

        for command in safe_stash_commands:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command},
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should not match safe command: {command}",
            )

    def test_no_match_non_bash(self):
        """Should not match non-Bash tools."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/test.txt", "content": "git stash"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_escape_hatch_allows(self):
        """Should allow when escape hatch phrase present."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "git stash  # I HAVE ABSOLUTELY CONFIRMED THAT STASH IS THE ONLY OPTION",
            },
        }
        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "allow")

    def test_blocks_without_escape_hatch(self):
        """Should block when escape hatch phrase absent."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git stash"},
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("BLOCKED", result.reason)
        self.assertIn("git stash is dangerous", result.reason)
        self.assertIn("ESCAPE HATCH", result.reason)

    def test_priority(self):
        """Handler should have priority 20."""
        self.assertEqual(self.handler.priority, 20)

    def test_name(self):
        """Handler should have correct name."""
        self.assertEqual(self.handler.name, "discourage-git-stash")


class TestNpmCommandHandler(unittest.TestCase):
    """Test suite for NpmCommandHandler."""

    def setUp(self):
        """Set up test handler."""
        self.handler = NpmCommandHandler()

    def test_matches_npm_run_build(self):
        """Should match 'npm run build' - must use llm:build instead."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm run build"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_npm_run_test(self):
        """Should match 'npm run test' command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm run test"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_no_match_llm_commands(self):
        """Should not match llm: prefixed commands."""
        llm_commands = [
            "npm run llm:build",
            "npm run llm:lint",
            "npm run llm:test",
            "npm run llm:qa",
        ]

        for command in llm_commands:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command},
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should not match llm command: {command}",
            )

    def test_no_match_allowed_commands(self):
        """Should not match whitelisted commands."""
        allowed_commands = [
            "npm run clean",
            "npm run dev:permissive",
        ]

        for command in allowed_commands:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command},
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should not match allowed command: {command}",
            )

    def test_no_match_non_npm_run(self):
        """Should not match non-npm-run commands."""
        non_npm_commands = [
            "npm install",
            "npm test",  # Direct npm test, not npm run test
            "yarn build",
            "pnpm run build",
        ]

        for command in non_npm_commands:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command},
            }
            self.assertFalse(
                self.handler.matches(hook_input),
                f"Should not match non-npm-run command: {command}",
            )

    def test_matches_npx_tsc(self):
        """Should match 'npx tsc' commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npx tsc --noEmit"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_npx_eslint(self):
        """Should match 'npx eslint' commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npx eslint src/"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_npx_prettier(self):
        """Should match 'npx prettier' commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npx prettier --check ."},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_npx_tsx_script(self):
        """Should match 'npx tsx' script execution."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npx tsx scripts/llm-lint.ts"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_llm_command_piped_to_grep(self):
        """Should match llm: command piped to grep (pointless - uses cache files)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm run llm:lint | grep error"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_llm_command_piped_to_tee(self):
        """Should match llm: command piped to tee (pointless - already logs)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm run llm:build | tee output.log"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_matches_npm_run_piped(self):
        """Should match any npm run command piped to grep/awk/sed."""
        test_cases = [
            "npm run lint | grep error",
            "npm run test | awk '{print $1}'",
            "npm run build | sed 's/foo/bar/'",
        ]
        for command in test_cases:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command},
            }
            self.assertTrue(self.handler.matches(hook_input), f"Should block: {command}")

    def test_handle_provides_suggestion(self):
        """Should provide suggested llm: command."""
        # build is in ALLOWED_COMMANDS, so test with non-allowed commands
        test_cases = [
            ("npm run lint", "llm:lint"),
            ("npm run test", "llm:test"),
            ("npm run type-check", "llm:type-check"),
            ("npx tsc", "llm:type-check"),
            ("npx eslint", "llm:lint"),
            ("npx prettier", "llm:format:check"),
        ]

        for command, expected_suggestion in test_cases:
            hook_input = {
                "tool_name": "Bash",
                "tool_input": {"command": command},
            }
            result = self.handler.handle(hook_input)
            self.assertEqual(result.decision, "deny")
            self.assertIn(expected_suggestion, result.reason, f"Failed for command: {command}")

    def test_handle_blocks_command(self):
        """Should return deny decision for non-llm, non-allowed commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm run lint"},
        }
        result = self.handler.handle(hook_input)

        self.assertEqual(result.decision, "deny")
        self.assertIn("BLOCKED", result.reason)
        self.assertIn("npm run lint", result.reason)

    def test_priority(self):
        """Handler should have priority 50."""
        self.assertEqual(self.handler.priority, 50)

    def test_name(self):
        """Handler should have correct name."""
        self.assertEqual(self.handler.name, "enforce-npm-commands")


class TestDestructiveGitHandlerEdgeCases(unittest.TestCase):
    """Test edge cases and boundary conditions for DestructiveGitHandler."""

    def setUp(self):
        self.handler = DestructiveGitHandler()

    def test_case_insensitive_matching(self):
        """Should match commands regardless of case."""
        commands = [
            "GIT RESET --HARD",
            "Git Reset --Hard",
            "git RESET --HARD",
        ]
        for command in commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": command}}
            self.assertTrue(self.handler.matches(hook_input), f"Failed for: {command}")

    def test_git_in_path_not_command(self):
        """Should not match when 'git' is just in a path."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "cd /tmp/project && ls"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_command_chains(self):
        """Should match destructive git in command chains."""
        commands = [
            "git status && git reset --hard",
            "git reset --hard; git push",
            "git reset --hard | grep output",
        ]
        for command in commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": command}}
            self.assertTrue(self.handler.matches(hook_input), f"Failed for: {command}")

    def test_git_with_extra_flags(self):
        """Should match git commands with various flag combinations."""
        commands = [
            "git reset --hard --quiet",
            "git clean -fdx",
            "git clean -f -d -x",
            "git clean -fqd",
        ]
        for command in commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": command}}
            self.assertTrue(self.handler.matches(hook_input), f"Failed for: {command}")

    def test_empty_command(self):
        """Should handle empty command gracefully."""
        hook_input = {"tool_name": "Bash", "tool_input": {"command": ""}}
        self.assertFalse(self.handler.matches(hook_input))

    def test_whitespace_only_command(self):
        """Should handle whitespace-only commands."""
        hook_input = {"tool_name": "Bash", "tool_input": {"command": "   \n\t  "}}
        self.assertFalse(self.handler.matches(hook_input))

    def test_git_reset_soft(self):
        """Should not match safe git reset --soft."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git reset --soft HEAD~1"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_git_reset_mixed(self):
        """Should not match git reset --mixed (default)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git reset --mixed HEAD~1"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_git_checkout_branch(self):
        """Should not match safe branch checkout."""
        safe_checkouts = [
            "git checkout main",
            "git checkout feature-branch",
            "git checkout -b new-branch",
        ]
        for command in safe_checkouts:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": command}}
            self.assertFalse(self.handler.matches(hook_input), f"Failed for: {command}")

    def test_blocks_checkout_head_with_file(self):
        """SECURITY: Must block git checkout HEAD -- file (discards changes permanently)."""
        commands = [
            "git checkout HEAD -- src/foo.ts",
            "git checkout HEAD -- /tmp/file.tsx",
            "git checkout HEAD -- .",
            "git checkout main -- src/file.ts",  # Any ref before --
            "git checkout HEAD~1 -- file.ts",
            "git checkout origin/master -- .",
            "git checkout @{upstream} -- src/",
            "git checkout -- file.ts",  # Original pattern should still work
        ]
        for cmd in commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            self.assertTrue(
                self.handler.matches(hook_input),
                f"SECURITY: MUST block destructive checkout: {cmd}",
            )

    def test_reason_includes_command(self):
        """Blocked reason should include the actual command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git clean -fd /tmp/test"},
        }
        result = self.handler.handle(hook_input)
        self.assertIn("git clean -fd /tmp/test", result.reason)

    def test_reason_includes_safe_alternatives(self):
        """Blocked reason should suggest safe alternatives."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git reset --hard"},
        }
        result = self.handler.handle(hook_input)
        self.assertIn("SAFE alternatives", result.reason)
        self.assertIn("git stash", result.reason)


class TestGitStashHandlerEdgeCases(unittest.TestCase):
    """Test edge cases for GitStashHandler."""

    def setUp(self):
        self.handler = GitStashHandler()

    def test_stash_with_flags(self):
        """Should match stash with various flags."""
        commands = [
            "git stash push --keep-index",
            "git stash push -u",
            "git stash push --include-untracked",
            "git stash save --patch",
        ]
        for command in commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": command}}
            self.assertTrue(self.handler.matches(hook_input), f"Failed for: {command}")

    def test_escape_hatch_case_sensitive(self):
        """Escape hatch should be case-sensitive."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "git stash  # i have absolutely confirmed that stash is the only option",
            },
        }
        result = self.handler.handle(hook_input)
        # Should still block because case doesn't match
        self.assertEqual(result.decision, "deny")

    def test_escape_hatch_in_middle_of_command(self):
        """Escape hatch should work anywhere in command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {
                "command": "# I HAVE ABSOLUTELY CONFIRMED THAT STASH IS THE ONLY OPTION\ngit stash push",
            },
        }
        result = self.handler.handle(hook_input)
        self.assertEqual(result.decision, "allow")

    def test_git_stash_pop_allowed(self):
        """Git stash pop should be allowed (recovery operation)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git stash pop"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_stash_in_command_chain(self):
        """Should match stash even in command chains."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "git add . && git stash push && git pull"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_empty_command(self):
        """Should handle empty command."""
        hook_input = {"tool_name": "Bash", "tool_input": {"command": ""}}
        self.assertFalse(self.handler.matches(hook_input))


class TestNpmCommandHandlerEdgeCases(unittest.TestCase):
    """Test edge cases for NpmCommandHandler."""

    def setUp(self):
        self.handler = NpmCommandHandler()

    def test_npm_install_not_matched(self):
        """Should not match npm install (not npm run)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm install package-name"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_npm_test_direct_not_matched(self):
        """Should not match direct 'npm test' (not npm run test)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm test"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_npm_command_with_args(self):
        """Should match npm run build even with arguments."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm run build -- --watch"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_npm_in_path(self):
        """Should not match npm in path/variable name."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "cd npm-project && ls"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_command_with_env_vars(self):
        """Should match npm run build even with env vars."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "NODE_ENV=production npm run build"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_llm_command_with_colon_args(self):
        """Should not match llm commands with additional segments."""
        commands = [
            "npm run llm:test:unit",
            "npm run llm:lint:fix",
            "npm run llm:build:prod",
        ]
        for command in commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": command}}
            self.assertFalse(self.handler.matches(hook_input), f"Should allow: {command}")

    def test_unknown_command_gets_generic_suggestion(self):
        """Unknown npm run commands should get generic llm:qa suggestion."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm run unknown-command"},
        }
        result = self.handler.handle(hook_input)
        self.assertIn("llm:qa", result.reason)

    def test_whitelist_command_not_matched(self):
        """Whitelisted commands should not match."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm run clean"},
        }
        self.assertFalse(self.handler.matches(hook_input))

    def test_command_in_subshell(self):
        """Should match npm run build in subshells."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "$(npm run build)"},
        }
        self.assertTrue(self.handler.matches(hook_input))

    def test_command_with_pipes(self):
        """Should match npm run build when piped."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm run build | tee output.log"},
        }
        self.assertTrue(self.handler.matches(hook_input))


class TestHandlerBaseClassBehavior(unittest.TestCase):
    """Test Handler base class requires implementation."""

    def test_handler_matches_not_implemented(self):
        """Handler.matches() should raise NotImplementedError."""
        handler = Handler("test-handler")
        with self.assertRaises(NotImplementedError):
            handler.matches({})

    def test_handler_handle_not_implemented(self):
        """Handler.handle() should raise NotImplementedError."""
        handler = Handler("test-handler")
        with self.assertRaises(NotImplementedError):
            handler.handle({})

    def test_handler_name_stored(self):
        """Handler should store name."""
        handler = Handler("my-handler", priority=50)
        self.assertEqual(handler.name, "my-handler")
        self.assertEqual(handler.priority, 50)


if __name__ == "__main__":
    unittest.main()
