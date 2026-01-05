"""SystemPathsHandler - prevents direct editing of deployed system files."""

import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))

from front_controller import Handler, HookResult


class SystemPathsHandler(Handler):
    """Block Write/Edit operations on deployed system files.

    Enforces infrastructure-as-code principle: edit project files in files/
    directory and deploy via Ansible, never edit deployed files directly.

    Blocked paths: /etc/, /var/, /usr/, /opt/, /root/, /home/
    Allowed paths: files/*, /workspace/*, relative paths
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

    def __init__(self):
        super().__init__(name="validate-system-paths", priority=8)

    def matches(self, hook_input: dict) -> bool:
        """Check if operation targets a deployed system file."""
        tool_name = hook_input.get("tool_name")

        # Only check Write and Edit tools (Read is fine)
        if tool_name not in ["Write", "Edit"]:
            return False

        tool_input = hook_input.get("tool_input", {})
        file_path = tool_input.get("file_path", "")

        if not file_path:
            return False

        # Check if file_path starts with any blocked system path
        for blocked_path in self.BLOCKED_PATHS:
            if file_path.startswith(blocked_path):
                return True

        return False

    def handle(self, hook_input: dict) -> HookResult:
        """Block the operation with helpful message about infrastructure-as-code."""
        tool_input = hook_input.get("tool_input", {})
        file_path = tool_input.get("file_path", "")

        # Determine the project equivalent path
        project_path = self._get_project_path(file_path)

        return HookResult(
            decision="deny",
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
            )
        )

    def _get_project_path(self, file_path: str) -> str:
        """Determine the correct project path for a system file."""
        if file_path.startswith("/var/local/"):
            return f"files/var/local/..."
        elif file_path.startswith("/etc/"):
            return f"files/etc/..."
        elif file_path.startswith("/usr/local/"):
            return f"files/usr/local/..."
        elif file_path.startswith("/usr/"):
            return f"files/usr/..."
        elif file_path.startswith("/opt/"):
            return f"files/opt/..."
        elif file_path.startswith("/root/"):
            return "Use Ansible to configure root user files"
        elif file_path.startswith("/home/"):
            return "Use Ansible to configure user home directory files"
        else:
            return "files/[appropriate-subdirectory]/"
