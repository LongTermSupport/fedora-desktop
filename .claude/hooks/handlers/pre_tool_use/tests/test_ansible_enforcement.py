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


# Python's way to run a shell command, spelled so this file holds no literal call to it.
_PY_SHELL_OUT = "os." + "system"

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

# A script the line writes and then runs behind a wrapper. The daemon's command view only
# recognises the bare `bash s.sh`, so the handler judges such a line as written (the review of
# issue #294, blocking finding 1).
_WRITE_SCRIPT = "cat > s.sh <<'EOF'\nsudo systemctl restart sshd\nEOF\n"
WRITTEN_SCRIPT_RUN = [
    "sudo ./s.sh",
    "./s.sh",
    "sudo bash s.sh",
    "sudo sh s.sh",
    "sudo -E bash s.sh",
    "sudo -u root bash s.sh",
    "sudo source s.sh",
    "sudo . ./s.sh",
    "source s.sh",
    "timeout 9 bash s.sh",
    "nohup bash s.sh &",
    "nice bash s.sh",
    "nice -n 5 bash s.sh",
    "(bash s.sh)",
    "{ bash s.sh; }",
    "bash $PWD/s.sh",
    "bash ./s.sh",
    "sudo bash /var/tmp/s.sh",
    "env A=1 bash s.sh",
    "setsid bash s.sh",
    "cat s.sh | sudo bash",
    "chmod +x s.sh && sudo ./s.sh",
]
WRITTEN_SCRIPT_LINES = [_WRITE_SCRIPT + run for run in WRITTEN_SCRIPT_RUN] + [
    "echo 'dnf install x' | sudo tee s.sh; sudo bash s.sh",
    "echo 'dnf install x' > a.sh\ncp a.sh /tmp/b.sh && sudo bash /tmp/b.sh",
    "printf 'dnf install x' >> s.sh && timeout 5 sh s.sh",
    "cat > install.sh <<'EOF'\nsudo systemctl restart sshd\nEOF\nchmod +x install.sh\nsudo ./install.sh",
]

# Text that names a system change goes into a pipeline whose last stage is a shell: whatever the
# earlier stages print, the shell runs, so the line is judged as written.
PIPED_INTO_A_SHELL = [
    "git commit -m 'dnf install x' && git log -1 --format=%s | bash",
    "gh issue comment 1 --body 'sudo dnf install x' && gh issue view 1 -q .body | sudo bash",
    "git log -1 --format=%s -- 'dnf install x' | sh",
    "git commit -m 'systemctl restart y' && git log -1 --format=%B | sudo bash -s",
]

# A line that writes a file and runs no script and no shell: it only names the change in text.
WRITES_BUT_RUNS_NOTHING = [
    "cat > notes.md <<'EOF'\nsudo dnf install x\nEOF\nls",
    "cat > notes.md <<'EOF'\nsudo dnf install x\nEOF\nbash -n notes.md",
    "echo 'dnf install x' > notes.md && grep -c bash notes.md",
    "echo 'dnf install x' > notes.md && git add notes.md",
    "cat > notes.md <<'EOF'\nsudo dnf install x\nEOF\nls . && cat notes.md",
    "git commit -m 'dnf install x' && git log -1 --format=%s | cat",
]

# An option written between the tool and its verb used to hide the verb (`dnf -y install`).
OPTION_BEFORE_THE_VERB = [
    "dnf -y install vim",
    "sudo dnf -y install vim",
    "sudo dnf --assumeyes install vim",
    "dnf --setopt=install_weak_deps=False remove y",
    "dnf -c /etc/dnf/dnf.conf install x",
    "dnf --releasever 40 upgrade",
    "dnf -y --refresh update",
    "yum -y install x",
    "yum --nogpgcheck update",
    "rpm --quiet -i x.rpm",
    "rpm --nodeps -e pkg",
    "rpm -vi x.rpm",
    "rpm --root /mnt/sys -Uvh a.rpm",
    "pip --quiet install x",
    "pip3 -v install x",
    "python3 -m pip -q install x",
    "pip --proxy http://proxy.example.com:3128 install x",
    "flatpak --user install x",
    "flatpak -y install flathub org.app",
    "flatpak --assumeyes update",
    "snap --classic install thing",
    "systemctl --now enable foo",
    "sudo systemctl -q restart foo",
    "systemctl --system restart x",
    "systemctl --no-block start x",
    "systemctl --no-ask-password --now disable x",
    "systemctl -H host.example.com restart x",
    "gsettings --schemadir /usr/share/glib-2.0/schemas set org.x y z",
    "npm install typescript -g",
    "npm i -g x",
    "npm -g install x",
    "npm install --global x",
]
OPTION_BEFORE_THE_VERB_ALLOWED = [
    "systemctl --user enable x",
    "systemctl --user --now enable x",
    "systemctl --now --user enable x",
    "systemctl --user -q restart x",
    "systemctl --no-pager status x",
    "systemctl -q is-active x",
    "systemctl --failed",
    "dnf -y info vim",
    "dnf --refresh search python",
    "dnf updateinfo list",
    "dnf search install",
    "dnf -q list installed",
    "rpm -q -i vim",
    "rpm -qi vim",
    "rpm --quiet -q vim",
    "rpm -qa --qf '%{name}\\n'",
    "pip -q list",
    "pip --version",
    "pip3 -q show x",
    "flatpak --user list",
    "flatpak -v info org.app",
    "npm install x",
    "npm -g list",
    "npm --global ls",
]

# `dnf5`, an absolute path to the tool, a quoted tool name, and the wrappers that run what follows.
OTHER_SPELLINGS_OF_THE_TOOL = [
    "dnf5 install vim",
    "sudo dnf5 -y install x",
    "dnf5 remove x",
    "/usr/bin/dnf install vim",
    "sudo /usr/bin/dnf -y install vim",
    "/usr/bin/systemctl restart sshd",
    "sudo /usr/bin/pip3 install x",
    "/usr/bin/flatpak install x",
    "'dnf' install vim",
    '"dnf" install vim',
    "sudo 'systemctl' restart x",
    "command dnf install x",
    "env dnf install x",
    "env A=1 dnf -y install x",
    "xargs dnf -y install",
    "exec dnf install x",
    "eval 'dnf install x'",
    'eval "sudo dnf -y install x"',
    "bash -c 'dnf -y install x'",
    'sh -c "sudo systemctl --now enable y"',
    "sudo -u root dnf install x",
    "ionice -c3 dnf install x",
    "nice -n 5 dnf install x",
    "setsid dnf install x",
    "flock /tmp/l dnf install x",
    "time dnf install x",
    "stdbuf -oL dnf install x",
    "doas dnf install x",
    "pkexec dnf install x",
]

# What no static reading can judge: the command word is computed. These are pinned so the limit
# is a stated fact, not a surprise.
COMPUTED_COMMAND_WORDS_NOT_JUDGED = [
    "$(echo dnf) install x",
    "`echo dnf` install x",
    "$PKGMGR install x",
    "${PM:-dnf} install x",
]

# Text the daemon's own view keeps (a `$` expansion, an unquoted heredoc, a jq program, a print of
# a literal, a shell that only checks syntax) but that cannot run what it names.
INERT_TEXT_THE_DAEMON_KEEPS = [
    'echo "run: dnf install $PKG"',
    'echo "${PKG}: sudo dnf install"',
    'grep -n "dnf install $X" f',
    "cat <<EOF > notes.md\nsudo dnf install x\nEOF",
    "cat <<EOF\nsudo dnf install $X\nEOF",
    "cat <<-EOF > notes.md\n\tsudo dnf install x\nEOF",
    "jq '.\"dnf install\"' f",
    "jq -r '.[] | select(.n == \"pip install x\")' f",
    "python3 -c \"print('pip install x')\"",
    "python3 -c 'print(\"systemctl restart y\")'",
    "bash -n -c 'sudo dnf install x'",
    "sudo bash -n -c 'systemctl restart y'",
    "bash -nc 'dnf install x'",
    "bash -n script-that-holds-dnf-install.sh",
]

# The same shapes with a way to run: each must stay denied.
LIVE_LOOKALIKES_OF_INERT_TEXT = [
    'echo "$(dnf install x)"',
    'echo "run `dnf install x`"',
    'echo "dnf install $PKG" | bash',
    'echo "dnf install $PKG" | sudo sh -s',
    "cat <<EOF | bash\nsudo dnf install $X\nEOF",
    "cat <<EOF\n$(dnf install x)\nEOF",
    "cat <<EOF\n`dnf install x`\nEOF",
    "bash <<EOF\ndnf install $X\nEOF",
    f"python3 -c \"import os; {_PY_SHELL_OUT}('pip install x')\"",
    f"python3 -c \"print('a'); import os; {_PY_SHELL_OUT}('pip install x')\"",
    "jq '.\"dnf install\"' f | bash",
    "bash -c -n 'dnf install x'",
    "bash -n -c 'true'; dnf install x",
    "sudo bash -c 'dnf -y install x'",
]

ALL_DENIED = (
    RUNS_A_SYSTEM_CHANGE
    + WRITTEN_SCRIPT_LINES
    + PIPED_INTO_A_SHELL
    + OPTION_BEFORE_THE_VERB
    + OTHER_SPELLINGS_OF_THE_TOOL
    + LIVE_LOOKALIKES_OF_INERT_TEXT
)
ALL_ALLOWED = (
    RUNS_NO_SYSTEM_CHANGE
    + WRITES_BUT_RUNS_NOTHING
    + OPTION_BEFORE_THE_VERB_ALLOWED
    + COMPUTED_COMMAND_WORDS_NOT_JUDGED
    + INERT_TEXT_THE_DAEMON_KEEPS
)


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

    # matches() - a script written by the line, then run behind a wrapper (review finding 1)
    @pytest.mark.parametrize("command", WRITTEN_SCRIPT_LINES)
    def test_a_written_script_run_behind_a_wrapper_is_denied(self, handler, command):
        assert handler.matches(bash(command)) is True

    @pytest.mark.parametrize("command", PIPED_INTO_A_SHELL)
    def test_text_piped_into_a_shell_is_judged_as_written(self, handler, command):
        assert handler.matches(bash(command)) is True

    @pytest.mark.parametrize("command", WRITES_BUT_RUNS_NOTHING)
    def test_a_line_that_writes_a_file_but_runs_no_shell_is_not_judged_as_written(self, handler, command):
        assert handler.matches(bash(command)) is False

    # matches() - an option before the verb, another spelling of the tool
    @pytest.mark.parametrize("command", OPTION_BEFORE_THE_VERB)
    def test_an_option_before_the_verb_does_not_hide_it(self, handler, command):
        assert handler.matches(bash(command)) is True

    @pytest.mark.parametrize("command", OPTION_BEFORE_THE_VERB_ALLOWED)
    def test_queries_and_user_services_stay_allowed_with_options(self, handler, command):
        assert handler.matches(bash(command)) is False

    @pytest.mark.parametrize("command", OTHER_SPELLINGS_OF_THE_TOOL)
    def test_another_spelling_of_the_tool_is_judged(self, handler, command):
        assert handler.matches(bash(command)) is True

    @pytest.mark.parametrize("command", COMPUTED_COMMAND_WORDS_NOT_JUDGED)
    def test_a_computed_command_word_is_a_documented_limit(self, handler, command):
        """No static reading can know what `$(echo dnf)` expands to: the limit is stated."""
        assert handler.matches(bash(command)) is False

    # matches() - inert text the daemon's own view keeps
    @pytest.mark.parametrize("command", INERT_TEXT_THE_DAEMON_KEEPS)
    def test_inert_text_the_daemon_keeps_is_not_judged(self, handler, command):
        assert handler.matches(bash(command)) is False

    @pytest.mark.parametrize("command", LIVE_LOOKALIKES_OF_INERT_TEXT)
    def test_the_live_lookalikes_of_inert_text_stay_denied(self, handler, command):
        assert handler.matches(bash(command)) is True

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

    def test_guidance_for_a_written_script_follows_the_change_it_holds(self, handler):
        result = handler.handle(bash(_WRITE_SCRIPT + "sudo bash s.sh"))
        assert "'systemd' module" in result.reason

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

    @pytest.mark.parametrize("command", ["sudo dnf install vim", "systemctl restart y", "useradd z"])
    def test_deny_reason_carries_one_blocked_headline(self, handler, command):
        """The rule's own headline is the only one: no second `BLOCKED:` line below it."""
        assert handler.handle(bash(command)).reason.count("BLOCKED") == 1

    @pytest.mark.parametrize("command", ALL_DENIED)
    def test_every_blocked_command_denies_under_the_rule(self, handler, command):
        result = handler.handle(bash(command))
        assert result.decision == "deny"
        assert f"BLOCKED [{ansible_enforcement.RULE_ID}]" in result.reason

    @pytest.mark.parametrize("command", ALL_ALLOWED)
    def test_every_allowed_command_allows(self, handler, command):
        assert handler.handle(bash(command)).decision == "allow"

    # Acceptance tests: the matcher proves each verdict, and none could change the system
    def test_acceptance_tests_cover_a_block_and_an_allow(self, handler):
        decisions = {str(test.expected_decision) for test in handler.get_acceptance_tests()}
        assert {"deny", "allow"} <= decisions

    def test_acceptance_commands_get_the_verdict_they_expect(self, handler):
        for test in handler.get_acceptance_tests():
            expected = str(test.expected_decision) == "deny"
            assert handler.matches(bash(test.command)) is expected, test.title

    def test_denied_acceptance_commands_are_real_runs_that_are_harmless(self, handler):
        """A denied probe is judged for what it RUNS, never for a syntax check or an echo: it
        names a package or unit that does not exist and answers no to every prompt, so it
        changes nothing even where the guard missed it."""
        for test in handler.get_acceptance_tests():
            if str(test.expected_decision) == "deny":
                assert "nonexistent" in test.command, test.title
                assert not test.command.startswith(("bash -n", "echo ")), test.title
                assert "--assumeno" in test.command or "systemctl" in test.command, test.title

    def test_allowed_acceptance_commands_are_harmless(self, handler):
        """An allowed acceptance command runs for real: it names no package or unit that
        exists, only prints, or only checks syntax."""
        for test in handler.get_acceptance_tests():
            if str(test.expected_decision) == "allow":
                assert "nonexistent" in test.command or test.command.startswith("echo "), test.title

    def test_acceptance_tests_pin_the_rule_id(self, handler):
        for test in handler.get_acceptance_tests():
            if str(test.expected_decision) == "deny":
                assert any(ansible_enforcement.RULE_ID in pattern for pattern in test.expected_message_patterns)

    def test_a_syntax_check_acceptance_probe_is_allowed(self, handler):
        titles = [test.title for test in handler.get_acceptance_tests() if "bash -n" in test.command]
        assert titles, "the false-positive removal for a syntax-only shell has an acceptance probe"

    # Integration Tests
    def test_comprehensive_system_management_enforcement(self, handler):
        for cmd in ["dnf install package", "systemctl enable service", "gsettings set org.gnome key value", "useradd newuser"]:
            assert handler.matches(bash(cmd)) is True
        assert handler.matches(bash("ansible-playbook playbooks/playbook-main.yml")) is False
