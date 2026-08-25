"""SystemPathsHandler - prevents direct editing of deployed system files.

Enforces infrastructure-as-code principle: edit project files in files/
directory and deploy via Ansible, never edit deployed files directly.
"""

import os
import sys
from pathlib import Path
from typing import Any, ClassVar

# Add daemon to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "hooks-daemon/src"))

from claude_code_hooks_daemon.core import Decision, Handler, HookResult
from claude_code_hooks_daemon.core.acceptance_test import AcceptanceTest, TestType
from claude_code_hooks_daemon.core.utils import get_file_path

# Sentinel files whose presence at a directory confirms it is the project root.
# Used by _resolve_project_root() to guard against a wrong parents[N] depth.
_ROOT_SENTINELS = (".claude/settings.json", "CLAUDE.md")


def _resolve_project_root() -> str:
    """Return the project root directory (with trailing slash).

    Derives the root from this file's own location (five parents up), then
    verifies the result by checking for a sentinel file.  If the depth
    assumption is wrong, falls back to the CLAUDE_PROJECT_DIR env var.
    Raises RuntimeError if neither source yields a verifiable root so that
    a misconfiguration fails loudly rather than silently mis-exempting paths.
    """
    # This file lives at:
    #   <project_root>/.claude/hooks/handlers/pre_tool_use/system_paths.py
    # so project_root is five parents up.
    derived = Path(__file__).resolve().parents[4]
    if any((derived / sentinel).exists() for sentinel in _ROOT_SENTINELS):
        return str(derived) + "/"

    # Depth assumption appears wrong — fall back to the env var if available.
    env_root = os.environ.get("CLAUDE_PROJECT_DIR", "").strip()
    if env_root:
        env_path = Path(env_root).resolve()
        if any((env_path / sentinel).exists() for sentinel in _ROOT_SENTINELS):
            return str(env_path) + "/"
        raise RuntimeError(
            f"system_paths handler: CLAUDE_PROJECT_DIR={env_root!r} does not "
            f"contain a sentinel ({_ROOT_SENTINELS}).  Cannot determine project "
            "root — refusing to run to avoid mis-exempting system paths."
        )

    raise RuntimeError(
        f"system_paths handler: derived project root {derived!r} (parents[4] of "
        f"{__file__!r}) does not contain a sentinel ({_ROOT_SENTINELS}) and "
        "CLAUDE_PROJECT_DIR is not set.  Cannot determine project root — refusing "
        "to run to avoid mis-exempting system paths."
    )


def _is_claude_projects_path(resolved: str) -> bool:
    """Return True if *resolved* is under a Claude Code agent runtime directory.

    Matches /root/.claude/projects/... and /home/<user>/.claude/projects/...
    using a resolved-prefix check, not a substring match, so a crafted path
    like /var/evil/.claude/projects/../../etc/passwd cannot bypass the block.
    """
    if resolved.startswith("/root/.claude/projects/"):
        return True
    # /home/<user>/.claude/projects/  — match exactly three leading components
    # before .claude so we don't accidentally exempt /home/user/work/.claude/...
    if resolved.startswith("/home/"):
        parts = Path(resolved).parts  # ('/', 'home', '<user>', '.claude', ...)
        # parts[0]='/', parts[1]='home', parts[2]=<user>, parts[3]='.claude',
        # parts[4]='projects'
        if (
            len(parts) >= 5
            and parts[3] == ".claude"
            and parts[4] == "projects"
        ):
            return True
    return False


class SystemPathsHandler(Handler):
    """Block Write/Edit operations on deployed system files.

    Enforces infrastructure-as-code principle: edit project files in files/
    directory and deploy via Ansible, never edit deployed files directly.

    Blocked paths: /etc/, /var/, /usr/, /opt/, /root/, /home/
    Allowed paths: /workspace/*, relative paths
    """

    # System paths that should NEVER be edited directly
    BLOCKED_PATHS: ClassVar[list[str]] = [
        "/etc/",
        "/var/",
        "/usr/",
        "/opt/",
        "/root/",
        "/home/",
    ]

    def __init__(self) -> None:
        super().__init__(name="prevent-system-file-edits", priority=8, terminal=True)

    def matches(self, hook_input: dict[str, Any]) -> bool:
        """Check if operation targets a deployed system file.

        Args:
            hook_input: Hook input dict with tool_name and tool_input

        Returns:
            True if attempting to Write/Edit a system file, False otherwise
        """
        tool_name = hook_input.get("tool_name")

        # Only check Write and Edit tools (Read is fine, Bash handled by Ansible)
        if tool_name not in ["Write", "Edit"]:
            return False

        file_path = get_file_path(hook_input)
        if not file_path:
            return False

        # Exempt the active Claude Code project directory. When Claude runs on
        # the HOST (not inside a /workspace CCY container), the project itself
        # lives under /home/<user>/..., and legitimately editing its own files
        # must not be confused with editing deployed system files.
        # _resolve_project_root() verifies the derived path against a sentinel
        # so a wrong parents[N] depth fails loudly instead of silently
        # over-exempting an unrelated directory.
        project_dir = _resolve_project_root()
        if file_path.startswith(project_dir):
            return False

        # Exempt the Claude Code agent's own runtime directory. This holds
        # the agent's memory, session state, todos and plans — it is not
        # deployed system config.
        # Use a resolved-prefix check (not a substring match) so a crafted
        # path containing /.claude/projects/ mid-string cannot bypass the block.
        resolved = str(Path(file_path).resolve())
        if _is_claude_projects_path(resolved):
            return False

        # Check if file_path starts with any blocked system path
        for blocked_path in self.BLOCKED_PATHS:
            if file_path.startswith(blocked_path):
                return True

        return False

    def handle(self, hook_input: dict[str, Any]) -> HookResult:
        """Block the operation with helpful message about infrastructure-as-code.

        Args:
            hook_input: Hook input dict

        Returns:
            HookResult with deny decision and guidance on proper workflow
        """
        file_path = get_file_path(hook_input)
        if not file_path:
            return HookResult(decision=Decision.ALLOW)

        # Determine the project equivalent path
        project_path = self._get_project_path(file_path)

        return HookResult(
            decision=Decision.DENY,
            reason=(
                f"❌ BLOCKED: Direct editing of deployed system files is not allowed.\n\n"
                f"Target: {file_path}\n\n"
                f"This is a deployed file on the actual system filesystem. Directly editing\n"
                f"deployed files bypasses version control and creates configuration drift.\n\n"
                f"✓ CORRECT APPROACH:\n"
                f"1. Edit the project file in: {project_path}\n"
                f"2. Deploy using Ansible:\n"
                f"   ansible-playbook playbooks/imports/[appropriate-playbook].yml\n\n"
                f"This ensures:\n"
                f"  - Changes are version controlled\n"
                f"  - Changes can be reviewed and tested\n"
                f"  - Changes are reproducible across environments\n"
                f"  - Configuration remains consistent\n\n"
                f"See CLAUDE.md 'INFRASTRUCTURE AS CODE - ANSIBLE-ONLY DEPLOYMENT' for details."
            ),
        )

    def get_claude_md(self) -> str | None:
        """Return CLAUDE.md guidance for this handler."""
        blocked = ", ".join(self.BLOCKED_PATHS)
        return (
            "## system_paths — do not edit deployed system files directly\n\n"
            f"Writing or editing files under system paths ({blocked}) is blocked.\n"
            "These are deployed files managed by Ansible.\n\n"
            "**Edit the project source instead**:\n"
            "- `/etc/foo` → `files/etc/foo`\n"
            "- `/var/local/foo` → `files/var/local/foo`\n"
            "- `/usr/bin/foo` → `files/usr/bin/foo`\n\n"
            "Then deploy via Ansible playbook."
        )

    def get_acceptance_tests(self) -> list[AcceptanceTest]:
        """Return acceptance tests for SystemPathsHandler."""
        return [
            AcceptanceTest(
                title="Block Write to /etc/ system path",
                command='echo "Write to /etc/hosts"',
                description="Blocks direct editing of deployed system config files",
                expected_decision=Decision.DENY,
                expected_message_patterns=[r"BLOCKED.*Direct editing", r"CORRECT APPROACH"],
                safety_notes="Uses echo - safe to execute",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Allow Write to /workspace/ project path",
                command='echo "Write to /workspace/files/etc/hosts"',
                description="Permits editing project source files in /workspace/",
                expected_decision=Decision.ALLOW,
                expected_message_patterns=[],
                safety_notes="Uses echo - safe to execute",
                test_type=TestType.BLOCKING,
            ),
        ]

    def _get_project_path(self, file_path: str) -> str:
        """Determine the correct project path for a system file.

        Args:
            file_path: System file path

        Returns:
            Suggested project path for editing
        """
        if file_path.startswith("/var/local/"):
            return f"files/var/local/{file_path.split('/var/local/', 1)[1]}"
        if file_path.startswith("/etc/"):
            return f"files/etc/{file_path.split('/etc/', 1)[1]}"
        if file_path.startswith("/usr/local/"):
            return f"files/usr/local/{file_path.split('/usr/local/', 1)[1]}"
        if file_path.startswith("/usr/"):
            return f"files/usr/{file_path.split('/usr/', 1)[1]}"
        if file_path.startswith("/opt/"):
            return f"files/opt/{file_path.split('/opt/', 1)[1]}"
        if file_path.startswith("/root/"):
            return "Use Ansible to configure root user files (playbooks/imports/play-*.yml)"
        if file_path.startswith("/home/"):
            return "Use Ansible to configure user home directory files (playbooks/imports/play-*.yml)"
        return "files/[appropriate-subdirectory]/"
