"""AnsibleEnforcementHandler - enforces Ansible-only system deployment.

Blocks direct package management, system configuration, and service management
commands. All system changes must go through Ansible playbooks.
"""

import re
import sys
from pathlib import Path
from typing import Any

# Add daemon to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "hooks-daemon/src"))

from claude_code_hooks_daemon.core import Decision, Handler, HookResult
from claude_code_hooks_daemon.core.acceptance_test import AcceptanceTest, TestType
from claude_code_hooks_daemon.core.utils import get_bash_command


class AnsibleEnforcementHandler(Handler):
    """Block direct system management commands - enforce Ansible deployment.

    Blocks:
    - Package management: dnf install/remove/upgrade, rpm -i/-e, pip install, npm -g, flatpak install
    - System services: systemctl enable/start/stop/restart/disable
    - System config: gsettings set, dconf write, firewall-cmd modifications
    - User management: useradd, usermod, groupadd

    Allows:
    - Query commands: dnf info/list/search, systemctl status, gsettings get, etc.
    - Ansible playbook execution
    - Container operations (podman, docker)
    """

    # Forbidden command patterns (case-insensitive)
    FORBIDDEN_PATTERNS = [
        # Package management - install/remove operations
        r'\bdnf\s+(install|remove|erase|update|upgrade|downgrade|reinstall)',
        r'\byum\s+(install|remove|erase|update|upgrade)',
        r'\brpm\s+(-[iUeFh]*[iUeF][iUeFh]*|--install|--upgrade|--erase|--freshen)',
        r'\bpip3?\s+install(?!\s)',  # pip install (but not in venv context)
        r'\bnpm\s+install\s+-g',  # npm install -g (global)
        r'\bflatpak\s+(install|remove|uninstall|update)',
        r'\bsnap\s+(install|remove)',

        # System services - state changes (not queries)
        r'\bsystemctl\s+(?!--user)(enable|disable|start|stop|restart|reload|mask|unmask)',

        # System configuration
        r'\bgsettings\s+set',
        r'\bdconf\s+write',
        r'\bgconftool(-2)?\s+--set',
        r'\bfirewall-cmd\s+.*--(add|remove|zone)',

        # User/group management
        r'\buseradd\b',
        r'\busermod\b',
        r'\buserdel\b',
        r'\bgroupadd\b',
        r'\bgroupmod\b',
        r'\bgroupdel\b',
    ]

    def __init__(self) -> None:
        super().__init__(name="enforce-ansible-deployment", priority=10, terminal=True)

        # Compile patterns for performance
        self._compiled_patterns = [
            re.compile(pattern, re.IGNORECASE) for pattern in self.FORBIDDEN_PATTERNS
        ]

    def matches(self, hook_input: dict[str, Any]) -> bool:
        """Check if command attempts direct system management.

        Args:
            hook_input: Hook input dict with tool_name and tool_input

        Returns:
            True if attempting forbidden system command, False otherwise
        """
        # Only check Bash commands
        command = get_bash_command(hook_input)
        if not command:
            return False

        # Check against all forbidden patterns
        for pattern in self._compiled_patterns:
            if pattern.search(command):
                return True

        return False

    def handle(self, hook_input: dict[str, Any]) -> HookResult:
        """Block the operation with guidance on Ansible workflow.

        Args:
            hook_input: Hook input dict

        Returns:
            HookResult with deny decision and Ansible workflow guidance
        """
        command = get_bash_command(hook_input)
        if not command:
            return HookResult(decision=Decision.ALLOW)

        # Determine command category for specific guidance
        category = self._categorize_command(command)
        guidance = self._get_category_guidance(category)

        return HookResult(
            decision=Decision.DENY,
            reason=(
                f"❌ BLOCKED: Direct system management commands are not allowed.\n\n"
                f"Command: {command}\n\n"
                f"This command modifies system state outside of version control.\n"
                f"Manual system changes create configuration drift and are not reproducible.\n\n"
                f"✓ CORRECT APPROACH:\n"
                f"1. Create or update an Ansible playbook in playbooks/imports/\n"
                f"2. {guidance}\n"
                f"3. Run the playbook:\n"
                f"   ansible-playbook playbooks/imports/[playbook-name].yml\n\n"
                f"This ensures:\n"
                f"  - Changes are version controlled in git\n"
                f"  - Changes are documented in playbook files\n"
                f"  - Changes are reproducible on fresh installs\n"
                f"  - Configuration remains consistent\n\n"
                f"✓ ALLOWED COMMANDS (read-only queries):\n"
                f"  - dnf info/list/search, rpm -q\n"
                f"  - systemctl status, systemctl --user\n"
                f"  - gsettings get, dconf read\n"
                f"  - flatpak list\n\n"
                f"See CLAUDE.md 'INFRASTRUCTURE AS CODE - ANSIBLE-ONLY DEPLOYMENT' for details."
            ),
        )

    def get_acceptance_tests(self) -> list[AcceptanceTest]:
        """Return acceptance tests for AnsibleEnforcementHandler."""
        return [
            AcceptanceTest(
                title="Block dnf install command",
                command='echo "dnf install vim"',
                description="Blocks direct package installation - must use Ansible",
                expected_decision=Decision.DENY,
                expected_message_patterns=[r"BLOCKED.*Direct system management", r"CORRECT APPROACH"],
                safety_notes="Uses echo - safe to execute",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Block systemctl enable command",
                command='echo "systemctl enable nginx"',
                description="Blocks direct service management - must use Ansible",
                expected_decision=Decision.DENY,
                expected_message_patterns=[r"BLOCKED.*Direct system management"],
                safety_notes="Uses echo - safe to execute",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Allow dnf info query command",
                command="dnf info vim",
                description="Permits read-only package queries",
                expected_decision=Decision.ALLOW,
                expected_message_patterns=[],
                safety_notes="Read-only query, safe to execute",
                test_type=TestType.BLOCKING,
            ),
        ]

    def _categorize_command(self, command: str) -> str:
        """Determine the category of system command.

        Args:
            command: The bash command string

        Returns:
            Category string: 'package', 'service', 'config', 'user', or 'other'
        """
        command_lower = command.lower()

        if any(pkg in command_lower for pkg in ['dnf', 'yum', 'rpm', 'flatpak', 'snap', 'pip', 'npm']):
            return 'package'
        if 'systemctl' in command_lower:
            return 'service'
        if any(cfg in command_lower for cfg in ['gsettings', 'dconf', 'gconftool', 'firewall-cmd']):
            return 'config'
        if any(usr in command_lower for usr in ['useradd', 'usermod', 'userdel', 'groupadd', 'groupmod']):
            return 'user'

        return 'other'

    def _get_category_guidance(self, category: str) -> str:
        """Get Ansible-specific guidance for command category.

        Args:
            category: Command category from _categorize_command()

        Returns:
            Ansible module/approach guidance string
        """
        guidance_map = {
            'package': (
                "Use Ansible 'package' or 'dnf' module:\n"
                "     - name: Install package\n"
                "       package:\n"
                "         name: package-name\n"
                "         state: present"
            ),
            'service': (
                "Use Ansible 'systemd' module:\n"
                "     - name: Enable and start service\n"
                "       systemd:\n"
                "         name: service-name\n"
                "         state: started\n"
                "         enabled: yes"
            ),
            'config': (
                "Use Ansible 'dconf', 'command', or 'blockinfile' module:\n"
                "     - name: Configure GNOME setting\n"
                "       dconf:\n"
                "         key: /org/gnome/desktop/path\n"
                "         value: \"'value'\""
            ),
            'user': (
                "Use Ansible 'user' or 'group' module:\n"
                "     - name: Create user\n"
                "       user:\n"
                "         name: username\n"
                "         state: present"
            ),
            'other': (
                "Use appropriate Ansible module or 'command' module with proper handlers"
            ),
        }

        return guidance_map.get(category, guidance_map['other'])
