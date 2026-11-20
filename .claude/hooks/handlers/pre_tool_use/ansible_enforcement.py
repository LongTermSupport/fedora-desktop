"""AnsibleEnforcementHandler - enforces Ansible-only system deployment.

Blocks direct package management, system configuration, and service management
commands. All system changes must go through Ansible playbooks.

The handler judges the commands a Bash line RUNS, one command-position segment at a
time (the daemon's `command_position_code_segments`). Text that only names a command
runs nothing and is not judged: an echo or printf argument, a grep or rg pattern, a
git or gh message, a heredoc body fed to a data sink, a comment. A `bash -c '...'`
body, a `$(...)` substitution, a pipeline stage and a script the line writes and then
runs are judged as commands, and anything the daemon's view cannot read stays as
written, so an unreadable shape is a false positive, never a bypass.
"""

import re
import sys
from pathlib import Path
from typing import Any, ClassVar

# Add daemon to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "hooks-daemon/src"))

from claude_code_hooks_daemon.core import Decision, GatingResult
from claude_code_hooks_daemon.core.acceptance_test import AcceptanceTest, TestType
from claude_code_hooks_daemon.core.handler_bases import PreToolUseHandlerBase
from claude_code_hooks_daemon.core.rule import Rule
from claude_code_hooks_daemon.core.utils import get_bash_command
from claude_code_hooks_daemon.utils.command_position import command_position_code_segments

RULE_ID = "R-ANSIBLE-DIRECT-SYSTEM-CHANGE"

_RULE = Rule(
    rule_id=RULE_ID,
    blocked="a command that changes the system directly: dnf/yum/rpm/pip/npm -g/flatpak/snap, systemctl state changes, gsettings/dconf/firewall-cmd writes, user and group management",
    why="The change lives outside version control: configuration drift that a fresh install does not reproduce",
    fix="Put the change in an Ansible playbook under playbooks/imports/ and run the playbook",
    verbose=(
        "WHY: every system change must go through an Ansible playbook, so it is version\n"
        "controlled, documented in playbook files, reproducible on a fresh install and\n"
        "consistent with the rest of the configuration.\n\n"
        "Only a command that RUNS is judged. A commit message, an echo, a grep pattern or a\n"
        "heredoc body that names `dnf install` is not blocked; `sudo dnf install x`,\n"
        "`a && systemctl restart y` and `bash -c 'dnf install x'` are.\n\n"
        "ALLOWED (read-only queries): dnf info/list/search, rpm -q, systemctl status,\n"
        "systemctl --user, gsettings get, dconf read, flatpak list."
    ),
)


class AnsibleEnforcementHandler(PreToolUseHandlerBase):
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
    - Text that only names a blocked command (a message, an echo, a search pattern)
    """

    # Forbidden command patterns (case-insensitive)
    FORBIDDEN_PATTERNS: ClassVar[list[str]] = [
        # Package management - install/remove operations
        r'\bdnf\s+(install|remove|erase|update|upgrade|downgrade|reinstall)',
        r'\byum\s+(install|remove|erase|update|upgrade)',
        r'\brpm\s+(-[iUeFh]*[iUeF][iUeFh]*|--install|--upgrade|--erase|--freshen)',
        r'\bpip3?\s+install\b',  # pip install / pip3 install (system-level)
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

    def _offending_segment(self, command: str) -> str | None:
        """The first command-position segment of ``command`` that changes the system."""
        for segment in command_position_code_segments(command):
            if any(pattern.search(segment) for pattern in self._compiled_patterns):
                return segment
        return None

    def matches(self, hook_input: dict[str, Any]) -> bool:
        """Check if the command line RUNS a direct system management command.

        Args:
            hook_input: Hook input dict with tool_name and tool_input

        Returns:
            True if a command that runs is a forbidden system command, False otherwise
        """
        command = get_bash_command(hook_input)
        if not command:
            return False
        return self._offending_segment(command) is not None

    def handle(self, hook_input: dict[str, Any]) -> GatingResult:
        """Block the operation with guidance on Ansible workflow.

        Args:
            hook_input: Hook input dict

        Returns:
            GatingResult denying under R-ANSIBLE-DIRECT-SYSTEM-CHANGE, or allow when
            no command that runs is a system change
        """
        command = get_bash_command(hook_input)
        segment = self._offending_segment(command) if command else None
        if command is None or segment is None:
            return GatingResult(decision=Decision.ALLOW)

        # Guidance follows the command that runs, not the text around it.
        category = self._categorize_command(segment)
        guidance = self._get_category_guidance(category)

        return GatingResult.deny(
            f"BLOCKED: Direct system management commands are not allowed.\n\n"
            f"Command: {command}\n\n"
            f"This command modifies system state outside of version control.\n"
            f"Manual system changes create configuration drift and are not reproducible.\n\n"
            f"CORRECT APPROACH:\n"
            f"1. Create or update an Ansible playbook in playbooks/imports/\n"
            f"2. {guidance}\n"
            f"3. Run the playbook:\n"
            f"   ansible-playbook playbooks/imports/[playbook-name].yml\n\n"
            f"This ensures:\n"
            f"  - Changes are version controlled in git\n"
            f"  - Changes are documented in playbook files\n"
            f"  - Changes are reproducible on fresh installs\n"
            f"  - Configuration remains consistent\n\n"
            f"ALLOWED COMMANDS (read-only queries):\n"
            f"  - dnf info/list/search, rpm -q\n"
            f"  - systemctl status, systemctl --user\n"
            f"  - gsettings get, dconf read\n"
            f"  - flatpak list\n\n"
            f"See CLAUDE.md 'INFRASTRUCTURE AS CODE - ANSIBLE-ONLY DEPLOYMENT' for details."
        ).under_rule(_RULE)

    def get_rules(self) -> list[Rule]:
        """Return the single Rule backing this handler's deny."""
        return [_RULE]

    def get_claude_md(self) -> str | None:
        """Return CLAUDE.md guidance for this handler."""
        return (
            "## ansible_enforcement — no direct system management commands\n\n"
            "Direct package management, service management, and system configuration "
            "commands are blocked ([R-ANSIBLE-DIRECT-SYSTEM-CHANGE]). All system changes "
            "must go through Ansible playbooks. Only a command that runs is judged: text "
            "that names one (a commit message, an echo, a grep pattern) is not blocked.\n\n"
            "**Blocked**: `dnf install`, `systemctl enable/start/stop`, `gsettings set`, "
            "`useradd`, `pip install` (system), `npm install -g`, `flatpak install`\n\n"
            "**Allowed** (read-only queries): `dnf info/list/search`, `systemctl status`, "
            "`gsettings get`, `flatpak list`\n\n"
            "**Use instead**: Create/update Ansible playbook in `playbooks/imports/` "
            "and deploy with `ansible-playbook`."
        )

    def get_acceptance_tests(self) -> list[AcceptanceTest]:
        """Return acceptance tests for AnsibleEnforcementHandler.

        A blocked command is wrapped in `bash -n -c '...'`, which only syntax-checks the
        body, so nothing changes the system even if the guard missed it. The allowed
        commands only print, or name a package that does not exist.
        """
        return [
            AcceptanceTest(
                title="Block dnf install command",
                command="bash -n -c 'sudo dnf install nonexistent-pkg-zz-safe-test'",
                dispatch_as_bash=True,
                description="Blocks direct package installation - must use Ansible",
                expected_decision=Decision.DENY,
                expected_message_patterns=[rf"BLOCKED \[{RULE_ID}\]", r"CORRECT APPROACH"],
                safety_notes="bash -n only parses its body: nothing runs even if the guard missed it",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Block systemctl restart command",
                command="bash -n -c 'systemctl restart nonexistent-unit-zz-safe-test.service'",
                dispatch_as_bash=True,
                description="Blocks direct service management - must use Ansible",
                expected_decision=Decision.DENY,
                expected_message_patterns=[rf"BLOCKED \[{RULE_ID}\]", r"systemd"],
                safety_notes="bash -n only parses its body: nothing runs even if the guard missed it",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Allow text that only names dnf install",
                command='echo "dnf install vim"',
                dispatch_as_bash=True,
                description="An echo argument is printed, not run",
                expected_decision=Decision.ALLOW,
                expected_message_patterns=[],
                safety_notes="echo prints its argument",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Allow dnf info query command",
                command="dnf info nonexistent-pkg-zz-safe-test",
                dispatch_as_bash=True,
                description="Permits read-only package queries",
                expected_decision=Decision.ALLOW,
                expected_message_patterns=[],
                safety_notes="Read-only query for a package that does not exist",
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
