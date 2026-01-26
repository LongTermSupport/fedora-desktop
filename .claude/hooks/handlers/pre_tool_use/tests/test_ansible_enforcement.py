"""Comprehensive tests for AnsibleEnforcementHandler - blocks manual system commands."""

import sys
from pathlib import Path

import pytest

# Add handler directory to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent))

from ansible_enforcement import AnsibleEnforcementHandler


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

    # matches() - Package Management - Positive Cases (should block)
    def test_matches_dnf_install(self, handler):
        """Should match dnf install command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo dnf install vim"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_dnf_remove(self, handler):
        """Should match dnf remove command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "dnf remove package"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_dnf_update(self, handler):
        """Should match dnf update command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo dnf update"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_dnf_upgrade(self, handler):
        """Should match dnf upgrade command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "dnf upgrade"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_rpm_install(self, handler):
        """Should match rpm -i install command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo rpm -ivh package.rpm"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_rpm_erase(self, handler):
        """Should match rpm -e erase command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "rpm -e package"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_pip_install_global(self, handler):
        """Should match pip install without venv (system-wide)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo pip install requests"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_pip3_install_global(self, handler):
        """Should match pip3 install without venv."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "pip3 install --user package"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_npm_install_global(self, handler):
        """Should match npm install -g (global)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "npm install -g typescript"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_flatpak_install(self, handler):
        """Should match flatpak install command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "flatpak install flathub org.app"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_flatpak_remove(self, handler):
        """Should match flatpak remove/uninstall command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "flatpak uninstall org.app"},
        }
        assert handler.matches(hook_input) is True

    # matches() - System Configuration - Positive Cases
    def test_matches_systemctl_enable(self, handler):
        """Should match systemctl enable command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo systemctl enable docker"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_systemctl_start(self, handler):
        """Should match systemctl start command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "systemctl start nginx"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_systemctl_restart(self, handler):
        """Should match systemctl restart command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo systemctl restart sshd"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_systemctl_disable(self, handler):
        """Should match systemctl disable command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "systemctl disable firewalld"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_gsettings_set(self, handler):
        """Should match gsettings set command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "gsettings set org.gnome.desktop.interface theme 'Adwaita-dark'"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_dconf_write(self, handler):
        """Should match dconf write command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "dconf write /org/gnome/setting value"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_firewall_cmd_add(self, handler):
        """Should match firewall-cmd configuration commands."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo firewall-cmd --add-port=8080/tcp --permanent"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_useradd(self, handler):
        """Should match useradd command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo useradd newuser"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_usermod(self, handler):
        """Should match usermod command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo usermod -aG docker joseph"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_groupadd(self, handler):
        """Should match groupadd command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo groupadd developers"},
        }
        assert handler.matches(hook_input) is True

    # matches() - Negative Cases (should NOT block - read-only/query commands)
    def test_matches_dnf_info_returns_false(self, handler):
        """Should NOT match dnf info (read-only query)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "dnf info vim"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_dnf_list_returns_false(self, handler):
        """Should NOT match dnf list (read-only query)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "dnf list installed"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_dnf_search_returns_false(self, handler):
        """Should NOT match dnf search (read-only query)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "dnf search python"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_rpm_query_returns_false(self, handler):
        """Should NOT match rpm -q query (read-only)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "rpm -qa | grep vim"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_systemctl_status_returns_false(self, handler):
        """Should NOT match systemctl status (read-only)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "systemctl status docker"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_gsettings_get_returns_false(self, handler):
        """Should NOT match gsettings get (read-only)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "gsettings get org.gnome.desktop.interface theme"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_dconf_read_returns_false(self, handler):
        """Should NOT match dconf read (read-only)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "dconf read /org/gnome/setting"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_flatpak_list_returns_false(self, handler):
        """Should NOT match flatpak list (read-only)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "flatpak list"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_ansible_playbook_returns_false(self, handler):
        """Should NOT match ansible-playbook (correct deployment method)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "ansible-playbook playbooks/imports/play-basic-configs.yml"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_non_bash_tool_returns_false(self, handler):
        """Should NOT match non-Bash tools (Write, Edit, Read, etc.)."""
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/workspace/test.txt", "content": "dnf install vim"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_empty_command_returns_false(self, handler):
        """Should NOT match empty command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": ""},
        }
        assert handler.matches(hook_input) is False

    def test_matches_none_command_returns_false(self, handler):
        """Should NOT match None command."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": None},
        }
        assert handler.matches(hook_input) is False

    def test_matches_missing_tool_input_returns_false(self, handler):
        """Should NOT match missing tool_input."""
        hook_input = {"tool_name": "Bash"}
        assert handler.matches(hook_input) is False

    # matches() - Edge Cases
    def test_matches_dnf_in_comment_returns_false(self, handler):
        """Should NOT match dnf in comments (not actual command)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "echo 'Use dnf install to add packages'"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_dnf_in_string_returns_false(self, handler):
        """Should NOT match dnf in string literals."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "grep 'dnf install' logfile.txt"},
        }
        assert handler.matches(hook_input) is False

    def test_matches_case_insensitive_dnf(self, handler):
        """Should match DNF in any case (DNF, Dnf, dnf)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo DNF INSTALL vim"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_dnf_with_multiple_spaces(self, handler):
        """Should match dnf with multiple spaces."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo  dnf   install    vim"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_dnf_in_pipe_chain(self, handler):
        """Should match dnf install in pipe chain."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo dnf install -y vim && echo done"},
        }
        assert handler.matches(hook_input) is True

    def test_matches_systemctl_user_mode_returns_false(self, handler):
        """Should NOT match systemctl --user (user services, not system)."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "systemctl --user start myapp"},
        }
        assert handler.matches(hook_input) is False

    # handle() Tests - Decision and Reason
    def test_handle_dnf_install_returns_deny(self, handler):
        """handle() should return deny for dnf install."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "sudo dnf install vim"},
        }
        result = handler.handle(hook_input)
        assert result.decision == "deny"

    def test_handle_reason_contains_blocked_indicator(self, handler):
        """handle() reason should clearly indicate operation is blocked."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "dnf install package"},
        }
        result = handler.handle(hook_input)
        assert "BLOCKED" in result.reason

    def test_handle_reason_shows_command(self, handler):
        """handle() reason should show the blocked command."""
        command = "sudo dnf install vim"
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": command},
        }
        result = handler.handle(hook_input)
        assert command in result.reason or "dnf install" in result.reason

    def test_handle_reason_mentions_ansible(self, handler):
        """handle() reason should mention Ansible as correct approach."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "systemctl enable docker"},
        }
        result = handler.handle(hook_input)
        assert "Ansible" in result.reason or "ansible-playbook" in result.reason.lower()

    def test_handle_reason_references_playbooks(self, handler):
        """handle() reason should reference playbook usage."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "gsettings set org.gnome.desktop theme dark"},
        }
        result = handler.handle(hook_input)
        assert "playbook" in result.reason.lower()

    def test_handle_reason_explains_infrastructure_as_code(self, handler):
        """handle() reason should explain IaC principle."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "flatpak install app"},
        }
        result = handler.handle(hook_input)
        assert "version control" in result.reason.lower() or "reproducible" in result.reason.lower()

    def test_handle_suggests_query_commands_allowed(self, handler):
        """handle() reason should mention query commands are allowed."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "dnf remove package"},
        }
        result = handler.handle(hook_input)
        assert "info" in result.reason or "query" in result.reason.lower() or "search" in result.reason.lower()

    # handle() Tests - Specific Command Categories
    def test_handle_package_management_mentions_package_module(self, handler):
        """handle() for package commands should mention Ansible package module."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "dnf install vim"},
        }
        result = handler.handle(hook_input)
        assert "package" in result.reason.lower() or "dnf" in result.reason.lower()

    def test_handle_systemctl_mentions_service_module(self, handler):
        """handle() for systemctl should mention Ansible systemd module."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "systemctl enable nginx"},
        }
        result = handler.handle(hook_input)
        assert "systemd" in result.reason.lower() or "service" in result.reason.lower()

    def test_handle_gsettings_mentions_dconf_module(self, handler):
        """handle() for gsettings should mention GNOME/dconf configuration."""
        hook_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "gsettings set org.gnome.desktop theme dark"},
        }
        result = handler.handle(hook_input)
        assert "dconf" in result.reason.lower() or "gsettings" in result.reason.lower() or "GNOME" in result.reason

    # Integration Tests
    def test_blocks_destructive_package_operations(self, handler):
        """Should block all destructive package operations."""
        destructive_commands = [
            "dnf install vim",
            "dnf remove package",
            "dnf upgrade",
            "rpm -ivh package.rpm",
            "flatpak install app",
        ]
        for cmd in destructive_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            assert handler.matches(hook_input) is True
            result = handler.handle(hook_input)
            assert result.decision == "deny"

    def test_allows_query_operations(self, handler):
        """Should allow all read-only query operations."""
        query_commands = [
            "dnf info vim",
            "dnf list installed",
            "dnf search python",
            "rpm -qa",
            "systemctl status docker",
            "flatpak list",
        ]
        for cmd in query_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            assert handler.matches(hook_input) is False

    def test_comprehensive_system_management_enforcement(self, handler):
        """Should enforce complete Ansible-only system management."""
        # Block direct system changes
        bad_commands = [
            "dnf install package",
            "systemctl enable service",
            "gsettings set org.gnome key value",
            "useradd newuser",
        ]
        for cmd in bad_commands:
            hook_input = {"tool_name": "Bash", "tool_input": {"command": cmd}}
            assert handler.matches(hook_input) is True

        # Allow Ansible deployment
        good_input = {
            "tool_name": "Bash",
            "tool_input": {"command": "ansible-playbook playbooks/playbook-main.yml"},
        }
        assert handler.matches(good_input) is False
