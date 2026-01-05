"""PostToolUse file validation handlers - runs AFTER file operations."""

import sys
import os
import subprocess

# Add parent directories to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult, get_file_path


class ValidateEslintOnWriteHandler(Handler):
    """Run ESLint validation on TypeScript/TSX files after write."""

    VALIDATE_EXTENSIONS = ['.ts', '.tsx']
    SKIP_PATHS = ['node_modules', 'dist', '.build', 'coverage', 'test-results']

    def __init__(self):
        super().__init__(name="validate-eslint-on-write", priority=10)

    def matches(self, hook_input: dict) -> bool:
        """Check if writing TypeScript/TSX file that needs validation."""
        tool_name = hook_input.get("tool_name")
        if tool_name not in ["Write", "Edit"]:
            return False

        file_path = get_file_path(hook_input)
        if not file_path:
            return False

        # Only check TypeScript/TSX files
        if not any(file_path.endswith(ext) for ext in self.VALIDATE_EXTENSIONS):
            return False

        # Skip build artifacts
        if any(skip in file_path for skip in self.SKIP_PATHS):
            return False

        # File must exist (PostToolUse runs after write)
        if not os.path.exists(file_path):
            return False

        return True

    def handle(self, hook_input: dict) -> HookResult:
        """Run ESLint on the file and block if errors found."""
        file_path = get_file_path(hook_input)

        print(f"\n🔍 Running ESLint validation on {os.path.basename(file_path)}...")

        # Check if this is a worktree file
        is_worktree = 'untracked/worktrees/' in file_path

        # Run ESLint using wrapper script
        try:
            command = ['tsx', 'scripts/eslint-wrapper.ts', file_path, '--max-warnings', '0', '--human']
            cwd = '/workspace'

            if is_worktree:
                print("  [Detected worktree file - using ESLint wrapper for consistent config]")

            result = subprocess.run(
                command,
                cwd=cwd,
                capture_output=True,
                text=True,
                timeout=30
            )

            if result.returncode != 0:
                error_message = (
                    f"ESLint validation FAILED for {file_path}\n\n"
                    + "=" * 80 + "\n"
                    + result.stdout + "\n"
                )

                if result.stderr:
                    error_message += result.stderr + "\n"

                error_message += (
                    "=" * 80 + "\n\n"
                    "🚫 FILE WAS WRITTEN BUT HAS ESLINT ERRORS!\n"
                    "   You MUST fix these errors before continuing.\n\n"
                    f"   Run: npx eslint {file_path} --fix\n"
                    "   Or:  npm run lint -- --fix\n"
                )

                return HookResult(decision="deny", reason=error_message)

            print(f"✅ ESLint validation passed for {os.path.basename(file_path)}\n")
            return HookResult(decision="allow")

        except subprocess.TimeoutExpired:
            return HookResult(
                decision="deny",
                reason="ESLint timed out after 30 seconds"
            )
        except Exception as e:
            return HookResult(
                decision="deny",
                reason=f"Failed to run ESLint: {str(e)}"
            )


class ValidateSitemapHandler(Handler):
    """Remind to validate sitemap files after editing."""

    def __init__(self):
        super().__init__(name="validate-sitemap-on-edit", priority=20)

    def matches(self, hook_input: dict) -> bool:
        """Check if editing sitemap markdown file."""
        tool_name = hook_input.get("tool_name")
        if tool_name not in ["Write", "Edit"]:
            return False

        file_path = get_file_path(hook_input)
        if not file_path:
            return False

        # Check if file is in CLAUDE/Sitemap/ directory
        if "CLAUDE/Sitemap" not in file_path:
            return False

        # Ignore CLAUDE/Sitemap/CLAUDE.md itself (documentation file)
        if file_path.endswith("CLAUDE/Sitemap/CLAUDE.md"):
            return False

        # Must be a markdown file
        if not file_path.endswith(".md"):
            return False

        return True

    def handle(self, hook_input: dict) -> HookResult:
        """Add reminder to validate sitemap after editing."""
        file_path = get_file_path(hook_input)

        reminder = f"""
⚠️ REMINDER: Sitemap file modified: {file_path}

After completing your edits, you SHOULD validate the sitemap:

Run sitemap-validator agent:
  Task tool:
    subagent_type: sitemap-validator
    prompt: Validate sitemap file: {file_path}
    model: haiku

The validator checks:
  ✓ No content (statistics, prose, descriptions)
  ✓ No hallucinated components (must exist in src/components/CLAUDE.md)
  ✓ No implementation details (props, code, styling)
  ✓ Correct notation (CSI enums, arrow syntax)

Result: ✅ PASS or ❌ FAIL with violation details

If using the sitemap skill, validation runs automatically in the modify→validate→fix loop.
"""

        return HookResult(decision="allow", context=reminder)


# NOTE: ValidatePlanNumberHandler was moved to PreToolUse to fix timing bug.
# See: .claude/hooks/controller/handlers/pre_tool_use/validate_plan_number_handler.py
#
# The PostToolUse timing bug:
# - PostToolUse runs AFTER directory creation
# - Handler saw just-created directory as "existing"
# - Creating 061 when highest was 060 incorrectly warned "use 062"
#
# Fix: Move to PreToolUse (validate BEFORE creation)
