"""Tests for AnsibleEnforcementHandler - blocks manual system commands.

The handler judges the commands a Bash line RUNS, not the words it contains (issue #294): a
commit message, an echo, a grep pattern or a heredoc body that names `dnf install` runs
nothing. Every blocking proof here goes through the matcher on a command string; no test hands a
real system-changing command to a shell.
"""

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).parent.parent))

import ansible_enforcement
from ansible_enforcement import AnsibleEnforcementHandler


def bash(command):
    """The hook input for one Bash command."""
    return {"tool_name": "Bash", "tool_input": {"command": command}}


# Every command here would change the system if it ran: the matcher must deny each one.
RUNS_A_SYSTEM_CHANGE = [
    # package management
    "sudo dnf install vim",
    "dnf remove package",
    "sudo dnf update",
    "dnf upgrade",
    "sudo rpm -ivh package.rpm",
    "rpm -e package",
    "sudo pip install requests",
    "pip3 install --user package",
    "npm install -g typescript",
    "flatpak install flathub org.app",
    "flatpak uninstall org.app",
    "flatpak update",
    "snap install thing",
    "yum install thing",
    # services
    "sudo systemctl enable docker",
    "systemctl start nginx",
    "sudo systemctl restart sshd",
    "systemctl disable firewalld",
    # configuration
    "gsettings set org.gnome.desktop.interface theme 'Adwaita-dark'",
    "dconf write /org/gnome/setting value",
    "sudo firewall-cmd --add-port=8080/tcp --permanent",
    # users
    "sudo useradd newuser",
    "sudo usermod -aG docker someone",
    "sudo groupadd developers",
    # spelling variants
    "sudo DNF INSTALL vim",
    "sudo  dnf   install    vim",
    # the shapes a careless agent writes
    "sudo dnf install -y vim && echo done",
    "cd /tmp && systemctl restart y",
    "echo hi; sudo dnf install x",
    "echo hi\nsudo dnf install x",
    "true || flatpak update",
    "(dnf install x)",
    "{ dnf install x; }",
    "if true; then dnf install x; fi",
    "for p in a b; do dnf install $p; done",
    "env X=1 pip install x",
    "X=1 pip install x",
    "sudo -n dnf install x",
    "sudo env A=1 dnf install x",
    "timeout 60 dnf install x",
    "nohup dnf install x &",
    "yes | dnf install x",
    "dnf install x | tee log.txt",
    "xargs dnf install < list.txt",
    "echo $(dnf install x)",
    'echo "$(systemctl restart y)"',
    "echo `dnf install x`",
    "bash -c 'dnf install vim'",
    'sh -c "systemctl restart y"',
    "bash -n -c 'sudo dnf install x'",
    "sudo bash -c 'cd /tmp; dnf install x'",
    "bash -c 'echo ok && dnf install x'",
    "bash -c \"bash -c 'dnf install x'\"",
    # text written and then run, or handed to a shell
    "echo 'dnf install vim' > s.sh && bash s.sh",
    "printf 'dnf install vim' | sh",
    "echo 'systemctl restart y' | bash",
    "bash <<EOF\ndnf install x\nEOF",
    "bash <<'EOF'\ndnf install x\nEOF",
    "sudo sh <<EOF\nsystemctl restart y\nEOF",
]

# Every command here only names a system change in text, or queries: nothing changes.
RUNS_NO_SYSTEM_CHANGE = [
    # read-only queries
    "dnf info vim",
    "dnf list installed",
    "dnf search python",
    "rpm -qa | grep vim",
    "systemctl status docker",
    "systemctl --user start myapp",
    "gsettings get org.gnome.desktop.interface theme",
    "dconf read /org/gnome/setting",
    "flatpak list",
    "ansible-playbook playbooks/imports/play-basic-configs.yml",
    "ansible-playbook playbooks/playbook-main.yml",
    # echo and printf print their arguments
    'echo "dnf install vim"',
    "echo 'Use dnf install to add packages'",
    'echo "systemctl enable nginx"',
    "echo systemctl restart y",
    "printf '%s\\n' \"... (flatpak update, changed ...\" >> CLAUDE/Plan/meta-deploy.list",
    "printf 'sudo dnf install x\\n'",
    # grep and rg search for their pattern
    "grep 'dnf install' logfile.txt",
    'grep -rn -i "flatpak update\\|flatpak-update" playbooks tasks',
    "rg 'systemctl restart' playbooks",
    "git grep -n 'dnf install' -- playbooks",
    "git log --grep='dnf install' --oneline",
    # a message, a title or a body is prose
    "git commit -m 'document why dnf install is blocked'",
    'git commit -m "systemctl restart is ansible only"',
    "git tag -a v1 -m 'flatpak update note'",
    "gh issue comment 294 --body 'sudo dnf install x is blocked'",
    "gh pr create --title 'pip install guard' --body 'pip install x'",
    # heredoc bodies written to a file or fed to cat
    "cat <<'EOF'\nsudo dnf install x\nEOF",
    "cat > notes.md <<'EOF'\nrun systemctl restart y\nEOF",
    "git commit -F - <<'EOF'\nmention dnf install here\nEOF",
    # a comment is not run
    "true # sudo dnf install x",
    "ls # systemctl restart y",
    # words that are not the command
    "ls dnf-install-notes",
    "cat docs/pip-install.md",
]


class TestAnsibleEnforcementHandler:
    """Test suite for AnsibleEnforcementHandler - enforces Ansible-only deployment."""

    @pytest.fixture
    def handler(self):
        """Create handler instance."""
        return AnsibleEnforcementHandler()

    # Initialization Tests
    def test_init_sets_correct_name(self, handler):
        """Handler name should be 'enforce-ansible-deployment'."""
        assert handler.name == "enforce-ansible-deployment"

    def test_init_sets_correct_priority(self, handler):
        """Handler priority should be 10 (safety handler)."""
        assert handler.priority == 10

    def test_init_sets_correct_terminal_flag(self, handler):
        """Handler should be terminal (blocks immediately)."""
        assert handler.terminal is True

    # matches() - the commands that run a system change
    @pytest.mark.parametrize("command", RUNS_A_SYSTEM_CHANGE)
    def test_matches_a_command_that_runs_a_system_change(self, handler, command):
        assert handler.matches(bash(command)) is True

    # matches() - nothing runs
    @pytest.mark.parametrize("command", RUNS_NO_SYSTEM_CHANGE)
    def test_does_not_match_text_or_queries(self, handler, command):
        assert handler.matches(bash(command)) is False

    def test_matches_non_bash_tool_returns_false(self, handler):
        """Should NOT match non-Bash tools (Write, Edit, Read, etc.)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/workspace/test.txt", "content": "dnf install vim"},
        }
        assert handler.matches(hook_input) is False

    @pytest.mark.parametrize("command", ["", None])
    def test_matches_empty_command_returns_false(self, handler, command):
        assert handler.matches(bash(command)) is False

    def test_matches_missing_tool_input_returns_false(self, handler):
        """Should NOT match missing tool_input."""
        assert handler.matches({"tool_name": "Bash"}) is False

    # handle() Tests - Decision and Reason
    def test_handle_dnf_install_returns_deny(self, handler):
        result = handler.handle(bash("sudo dnf install vim"))
        assert result.decision == "deny"

    def test_handle_reason_contains_blocked_indicator(self, handler):
        result = handler.handle(bash("dnf install package"))
        assert "BLOCKED" in result.reason

    def test_handle_reason_shows_command(self, handler):
        command = "sudo dnf install vim"
        result = handler.handle(bash(command))
        assert command in result.reason

    def test_handle_reason_mentions_ansible(self, handler):
        result = handler.handle(bash("systemctl enable docker"))
        assert "Ansible" in result.reason or "ansible-playbook" in result.reason.lower()

    def test_handle_reason_references_playbooks(self, handler):
        result = handler.handle(bash("gsettings set org.gnome.desktop theme dark"))
        assert "playbook" in result.reason.lower()

    def test_handle_reason_explains_infrastructure_as_code(self, handler):
        result = handler.handle(bash("flatpak install app"))
        assert "version control" in result.reason.lower() or "reproducible" in result.reason.lower()

    def test_handle_suggests_query_commands_allowed(self, handler):
        result = handler.handle(bash("dnf remove package"))
        assert "info" in result.reason or "query" in result.reason.lower()

    def test_handle_allows_a_command_that_runs_nothing(self, handler):
        """handle() on a text-only mention must not deny, whatever matches() said."""
        assert handler.handle(bash('echo "dnf install vim"')).decision == "allow"

    # handle() Tests - Specific Command Categories
    def test_handle_package_management_mentions_package_module(self, handler):
        result = handler.handle(bash("dnf install vim"))
        assert "'package' or 'dnf' module" in result.reason

    def test_handle_systemctl_mentions_service_module(self, handler):
        result = handler.handle(bash("systemctl enable nginx"))
        assert "'systemd' module" in result.reason

    def test_handle_gsettings_mentions_dconf_module(self, handler):
        result = handler.handle(bash("gsettings set org.gnome.desktop theme dark"))
        assert "dconf" in result.reason

    def test_handle_user_management_mentions_user_module(self, handler):
        result = handler.handle(bash("sudo useradd newuser"))
        assert "'user' or 'group' module" in result.reason

    def test_guidance_follows_the_command_that_runs_not_the_text_around_it(self, handler):
        """A service change after an echo that names dnf gets the service guidance."""
        result = handler.handle(bash("echo 'dnf is a package tool'; systemctl restart sshd"))
        assert "'systemd' module" in result.reason
        assert "'package' or 'dnf' module" not in result.reason

    # Rule id (issue #294: the deny had none, so explain-rule could not resolve it)
    def test_rule_id_is_stable(self):
        assert ansible_enforcement.RULE_ID == "R-ANSIBLE-DIRECT-SYSTEM-CHANGE"

    def test_get_rules_declares_the_rule(self, handler):
        rules = handler.get_rules()
        assert [rule.rule_id for rule in rules] == [ansible_enforcement.RULE_ID]

    def test_deny_reason_starts_with_the_rule_headline(self, handler):
        result = handler.handle(bash("sudo dnf install vim"))
        assert f"BLOCKED [{ansible_enforcement.RULE_ID}]" in result.reason
        assert result.reason.startswith(f"BLOCKED [{ansible_enforcement.RULE_ID}]")

    @pytest.mark.parametrize("command", RUNS_A_SYSTEM_CHANGE)
    def test_every_blocked_command_denies_under_the_rule(self, handler, command):
        result = handler.handle(bash(command))
        assert result.decision == "deny"
        assert f"BLOCKED [{ansible_enforcement.RULE_ID}]" in result.reason

    # Acceptance tests: the matcher proves each verdict, and none could run a real change
    def test_acceptance_tests_cover_a_block_and_an_allow(self, handler):
        decisions = {str(test.expected_decision) for test in handler.get_acceptance_tests()}
        assert {"deny", "allow"} <= decisions

    def test_acceptance_commands_get_the_verdict_they_expect(self, handler):
        for test in handler.get_acceptance_tests():
            expected = str(test.expected_decision) == "deny"
            assert handler.matches(bash(test.command)) is expected, test.title

    def test_denied_acceptance_commands_only_syntax_check(self, handler):
        """A denied acceptance command runs through `bash -n -c`, so it executes nothing
        even where the guard missed it."""
        for test in handler.get_acceptance_tests():
            if str(test.expected_decision) == "deny":
                assert test.command.startswith("bash -n -c "), test.title

    def test_allowed_acceptance_commands_are_harmless(self, handler):
        """An allowed acceptance command runs for real: it names no package or unit that
        exists, or only prints."""
        for test in handler.get_acceptance_tests():
            if str(test.expected_decision) == "allow":
                assert "nonexistent" in test.command or test.command.startswith("echo "), test.title

    def test_acceptance_tests_pin_the_rule_id(self, handler):
        for test in handler.get_acceptance_tests():
            if str(test.expected_decision) == "deny":
                assert any(ansible_enforcement.RULE_ID in pattern for pattern in test.expected_message_patterns)

    # Integration Tests
    def test_comprehensive_system_management_enforcement(self, handler):
        for cmd in ["dnf install package", "systemctl enable service", "gsettings set org.gnome key value", "useradd newuser"]:
            assert handler.matches(bash(cmd)) is True
        assert handler.matches(bash("ansible-playbook playbooks/playbook-main.yml")) is False
