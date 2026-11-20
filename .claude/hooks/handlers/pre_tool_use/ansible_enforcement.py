"""AnsibleEnforcementHandler - enforces Ansible-only system deployment.

Blocks direct package management, system configuration, and service management
commands. All system changes must go through Ansible playbooks.

The line is judged as WRITTEN, exactly as the original handler did: any forbidden pattern
anywhere in the Bash text denies it. That default cannot regress, because it is the original
reading, and a line that runs a script it wrote, hands text to a shell, or wraps a change in
anything (`sudo`, `script -c`, `watch`, `find -exec`, `perl -e system(...)`) names the change
in its text.

One exception answers issue #294 (a commit message, an echo or a grep pattern that merely NAMES
`dnf install` was denied). It lives in its own module, `_raw_command_judge`, the one sanctioned
judge of a raw line (the Semgrep rule hook-regex-over-raw-command excludes that module by path):
a line that is ALL INERT is judged by the commands it runs instead of its raw text. All inert
means the line writes no file (any redirect but /dev/null and the standard streams), holds no
`$(...)`, backtick or process substitution, runs only `echo`, `printf`, `grep`, `rg`, `cat`, `jq`,
`wc`, `cd`, `true`, `false`, a few git subcommands (commit, log, show, diff, status, add) and
`gh issue|pr comment|create` with a body, each with an EXACT option allow-list (an option not on
it, an abbreviated long option, or any git option before the subcommand sends the raw text to the
judge), and no word of those commands can expand to an option. Over-blocking is accepted: a line
that writes a file, substitutes, or runs a wrapper, shell, interpreter, `find`, `xargs`, `make`,
`watch`, `git push` or anything unknown is judged as the original judged it.
"""

import re
import sys
from pathlib import Path
from typing import Any, ClassVar

# Add daemon to path for imports, and this directory for the sanctioned raw judge
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "hooks-daemon/src"))
sys.path.insert(0, str(Path(__file__).parent))

from claude_code_hooks_daemon.core import Decision, GatingResult
from claude_code_hooks_daemon.core.acceptance_test import AcceptanceTest, TestType
from claude_code_hooks_daemon.core.handler_bases import PreToolUseHandlerBase
from claude_code_hooks_daemon.core.rule import Rule
from claude_code_hooks_daemon.core.utils import get_bash_command

from _raw_command_judge import INERT_COMMANDS, every_command_is_inert, judge_raw_command_line

RULE_ID = "R-ANSIBLE-DIRECT-SYSTEM-CHANGE"

__all__ = ["INERT_COMMANDS", "RULE_ID", "AnsibleEnforcementHandler", "every_command_is_inert", "judge_raw_command_line"]

_RULE = Rule(
    rule_id=RULE_ID,
    blocked="a command that changes the system directly: dnf/yum/rpm/pip/npm -g/flatpak/snap, systemctl state changes, gsettings/dconf/firewall-cmd writes, user and group management",
    why="The change lives outside version control: configuration drift that a fresh install does not reproduce",
    fix="Put the change in an Ansible playbook under playbooks/imports/ and run the playbook",
    verbose=(
        "WHY: every system change must go through an Ansible playbook, so it is version\n"
        "controlled, documented in playbook files, reproducible on a fresh install and\n"
        "consistent with the rest of the configuration.\n\n"
        "The line is judged as written: `sudo dnf install x`, `a && systemctl restart y`,\n"
        "`bash -c 'dnf install x'`, a script the line writes and then runs, and text piped into\n"
        "a shell are blocked, whatever options sit before the verb (`dnf -y install`) and\n"
        "however the tool is spelled (`dnf5`, `/usr/bin/dnf`, `d''nf`, behind `sudo`, `env`).\n\n"
        "Only a line that is ALL INERT is judged by what it runs, so a commit message or an echo\n"
        "that merely names `dnf install` is not blocked. All inert: no file written (any redirect\n"
        "but /dev/null), no `$(...)` or backtick, only echo, printf, grep, rg, cat, jq, wc, cd, a\n"
        "git commit/log/show/diff/status/add or a gh comment with a body, each with listed options\n"
        "only. A shell, wrapper, interpreter, `find`, `xargs`, `make`, a write or a substitution\n"
        "on the line puts the whole line back to its raw text.\n\n"
        "ALLOWED (read-only queries): dnf info/list/search, rpm -q, systemctl status,\n"
        "systemctl --user, gsettings get, dconf read, flatpak list."
    ),
)


def _options(value_options: tuple[str, ...] = (), not_before_option: str = "") -> str:
    """A pattern for any number of options: the words that start with `-`.

    ``value_options`` name the ones that take the next word as their value
    (`dnf --releasever 40 install`). ``not_before_option`` is a lookahead an option must not
    start with: the `--user` of `systemctl --user enable`.
    """
    guard = f"(?!{not_before_option})" if not_before_option else ""
    valued = "|".join(re.escape(option) for option in value_options)
    return (rf"(?:{guard}(?:{valued})\s+\S+\s+|" if valued else "(?:") + rf"{guard}-\S*\s+)*"


def _tool(names: str, verbs: str, value_options: tuple[str, ...] = (), not_before_option: str = "") -> str:
    """A pattern for ``names`` followed by any options and then one of ``verbs``."""
    return rf"\b(?:{names})\s+{_options(value_options, not_before_option)}(?:{verbs})\b"


_RPM_VALUE_OPTIONS = r"--root|--dbpath|--prefix|--relocate|--define|-D|--macros"
_NPM_GLOBAL = r"(?:-g|--global)\b"
_NPM_VERB = r"(?:install|i|add)\b"
_DNF_VALUE_OPTIONS = (
    "-c", "--config", "--releasever", "--enablerepo", "--disablerepo", "--repo", "--repoid", "--installroot", "-x", "--exclude",
    "--forcearch", "--setopt", "-d", "--debuglevel", "-e", "--errorlevel", "--rpmverbosity", "--color",
)  # fmt: skip
_PACKAGE_VERBS = (
    "install|remove|erase|update|upgrade|downgrade|reinstall|autoremove|distro-sync|distrosync|swap|mark"
    r"|groupinstall|groupremove|groupupdate|group\s+(?:install|remove|erase|upgrade|update)"
    r"|module\s+(?:install|remove|enable|disable|reset|switch-to|update)|history\s+(?:undo|redo|rollback)"
)
_SYSTEMCTL_VALUE_OPTIONS = ("-H", "--host", "-M", "--machine", "--root", "-t", "--type", "-p", "--property")
_SYSTEMCTL_VERBS = (
    "enable|disable|start|stop|restart|reload|mask|unmask|try-restart|reload-or-restart|try-reload-or-restart"
    "|daemon-reload|daemon-reexec|set-default|isolate|preset|preset-all|link|edit|revert|set-property"
    "|reset-failed|kill|add-wants|add-requires|import-environment|set-environment|unset-environment"
    "|reboot|poweroff|halt|suspend|hibernate"
)

# Forbidden command patterns, matched case-insensitively against the text of the line.
_FORBIDDEN_PATTERNS: tuple[str, ...] = (
    # Package management - install/remove operations
    _tool("dnf5?", _PACKAGE_VERBS + r"|system-upgrade|builddep|copr\s+(?:enable|disable|remove)", _DNF_VALUE_OPTIONS),
    _tool("yum", _PACKAGE_VERBS, _DNF_VALUE_OPTIONS),
    rf"\brpm\s+(?:(?:{_RPM_VALUE_OPTIONS})\s+\S+\s+|--(?!install\b|upgrade\b|erase\b|freshen\b|query\b|verify\b)\S+\s+|-v+\s+)*"
    r"(?:-[iUeFhv]*[iUeF][iUeFhv]*|--install|--upgrade|--erase|--freshen)",
    _tool("pip[0-9.]*", "install|uninstall", ("--proxy", "--cache-dir", "--log", "--timeout", "--retries", "--cert", "--client-cert", "--trusted-host", "--python", "--exists-action")),
    # npm install -g, with the global flag after the verb or before it
    rf"\bnpm\s+(?:-\S*\s+)*{_NPM_VERB}[^;&|\n]*?\s+{_NPM_GLOBAL}",
    rf"\bnpm\s+(?:-\S*\s+)*?{_NPM_GLOBAL}\s+(?:-\S*\s+)*{_NPM_VERB}",
    _tool("flatpak", "install|remove|uninstall|update"),
    _tool("snap", "install|remove|refresh"),
    # System services - state changes (not queries). A --user unit is the user's own, but the last
    # scope flag wins, so any --system on the line makes it a system change.
    _tool("systemctl", _SYSTEMCTL_VERBS, _SYSTEMCTL_VALUE_OPTIONS, not_before_option=r"--user\b"),
    rf"\bsystemctl\s+{_options(_SYSTEMCTL_VALUE_OPTIONS)}--system\s+{_options(_SYSTEMCTL_VALUE_OPTIONS)}(?:{_SYSTEMCTL_VERBS})\b",
    # System configuration
    _tool("gsettings", "set|reset|reset-recursively", ("--schemadir",)),
    _tool("dconf", "write|reset|load"),
    r"\bgconftool(-2)?\s+--set",
    r"\bfirewall-cmd\s+.*--(add|remove|zone)",
    # User/group management
    r"\buseradd\b",
    r"\busermod\b",
    r"\buserdel\b",
    r"\bgroupadd\b",
    r"\bgroupmod\b",
    r"\bgroupdel\b",
)

#: A quoted word that is one plain name or path: `'dnf'`, `"/usr/bin/systemctl"`.
_QUOTED_NAME = re.compile(r"""(?<![\w'"])(['"])([\w./+-]+)\1(?![\w'"])""")

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
    - Text that only names a blocked command, when every command on the line is inert
    """

    # Forbidden command patterns (case-insensitive)
    FORBIDDEN_PATTERNS: ClassVar[tuple[str, ...]] = _FORBIDDEN_PATTERNS

    def __init__(self) -> None:
        super().__init__(name="enforce-ansible-deployment", priority=10, terminal=True)

        # Compile patterns for performance
        self._compiled_patterns = [re.compile(pattern, re.IGNORECASE) for pattern in self.FORBIDDEN_PATTERNS]

    def _search(self, text: str) -> str | None:
        """The matched text of the first forbidden pattern in ``text`` (a quoted plain name read unquoted)."""
        text = _QUOTED_NAME.sub(r"\2", text)
        for pattern in self._compiled_patterns:
            match = pattern.search(text)
            if match:
                return match.group(0)
        return None

    def matches(self, hook_input: dict[str, Any]) -> bool:
        """Check if the Bash line names a direct system management command that runs.

        Args:
            hook_input: Hook input dict with tool_name and tool_input

        Returns:
            True if the line is a forbidden system command, False otherwise
        """
        command = get_bash_command(hook_input)
        if not command:
            return False
        return judge_raw_command_line(command, self._search) is not None

    def handle(self, hook_input: dict[str, Any]) -> GatingResult:
        """Block the operation with guidance on Ansible workflow.

        Args:
            hook_input: Hook input dict

        Returns:
            GatingResult denying under R-ANSIBLE-DIRECT-SYSTEM-CHANGE, or allow when
            the line is not a system change
        """
        command = get_bash_command(hook_input)
        offending = judge_raw_command_line(command, self._search) if command else None
        if command is None or offending is None:
            return GatingResult(decision=Decision.ALLOW)

        # Guidance follows the change the line names, not the text around it.
        category = self._categorize_command(offending)
        guidance = self._get_category_guidance(category)

        return GatingResult.deny(
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
            "must go through Ansible playbooks. The line is judged as written; only a line "
            "that writes no file, substitutes nothing and runs only inert commands with listed "
            "options (echo, grep, cat, a git commit) is judged by what it runs, so a message "
            "that merely names one passes.\n\n"
            "**Blocked**: `dnf install`, `systemctl enable/start/stop`, `gsettings set`, "
            "`useradd`, `pip install` (system), `npm install -g`, `flatpak install`\n\n"
            "**Allowed** (read-only queries): `dnf info/list/search`, `systemctl status`, "
            "`gsettings get`, `flatpak list`\n\n"
            "**Use instead**: Create/update Ansible playbook in `playbooks/imports/` "
            "and deploy with `ansible-playbook`."
        )

    def get_acceptance_tests(self) -> list[AcceptanceTest]:
        """Return acceptance tests for AnsibleEnforcementHandler.

        Each denied probe is judged on its raw text. It names a package or unit that does not
        exist and answers no to every prompt (`--assumeno`), or is a shell that only parses its
        body (`bash -n`), so it changes nothing even if the guard missed it. The allowed probes
        only print or query.
        """
        return [
            AcceptanceTest(
                title="Block dnf install command",
                command="dnf install --assumeno nonexistent-pkg-zz-safe-test",
                dispatch_as_bash=True,
                description="Blocks direct package installation - must use Ansible",
                expected_decision=Decision.DENY,
                expected_message_patterns=[rf"BLOCKED \[{RULE_ID}\]", r"CORRECT APPROACH"],
                safety_notes="--assumeno answers no to the prompt and the package does not exist, so nothing installs even if the guard missed it",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Block systemctl restart command",
                command="systemctl restart nonexistent-unit-zz-safe-test.service",
                dispatch_as_bash=True,
                description="Blocks direct service management - must use Ansible",
                expected_decision=Decision.DENY,
                expected_message_patterns=[rf"BLOCKED \[{RULE_ID}\]", r"systemd"],
                safety_notes="The unit does not exist, so a restart that got through fails with unit not found",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Block a dnf install behind a shell, judged as written",
                command="bash -n -c 'dnf install --assumeno nonexistent-pkg-zz-safe-test'",
                dispatch_as_bash=True,
                description="A shell is not inert, so the line is judged on its raw text",
                expected_decision=Decision.DENY,
                expected_message_patterns=[rf"BLOCKED \[{RULE_ID}\]", r"CORRECT APPROACH"],
                safety_notes="bash -n only parses its body and runs nothing; the package does not exist either",
                test_type=TestType.BLOCKING,
            ),
            AcceptanceTest(
                title="Allow text that only names dnf install",
                command='echo "dnf install vim"',
                dispatch_as_bash=True,
                description="Every command on the line is inert: an echo argument is printed, not run",
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
