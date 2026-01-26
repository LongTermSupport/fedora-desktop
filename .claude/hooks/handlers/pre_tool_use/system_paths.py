"""SystemPathsHandler - prevents direct editing of deployed system files.

Enforces infrastructure-as-code principle: edit project files in files/
directory and deploy via Ansible, never edit deployed files directly.
"""

import sys
from pathlib import Path
from typing import Any

# Add daemon to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "hooks-daemon/src"))

from claude_code_hooks_daemon.core import Decision, Handler, HookResult
from claude_code_hooks_daemon.core.utils import get_file_path


class SystemPathsHandler(Handler):
    """Block Write/Edit operations on deployed system files.

    Enforces infrastructure-as-code principle: edit project files in files/
    directory and deploy via Ansible, never edit deployed files directly.

    Blocked paths: /etc/, /var/, /usr/, /opt/, /root/, /home/
    Allowed paths: /workspace/*, relative paths
    """

    # System paths that should NEVER be edited directly
    BLOCKED_PATHS = [
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
