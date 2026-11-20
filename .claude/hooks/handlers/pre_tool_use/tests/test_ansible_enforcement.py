"""Tests for AnsibleEnforcementHandler - blocks manual system commands.

The handler judges the RAW command text, as the original did (issue #294, frozen form): a command
that only mentions a blocked tool in text is denied too, a known limit pinned below in
KNOWN_LIMIT_TEXT_MENTIONS. The additions over the original only add denials, and the oracle
invariant proves it: for every line in the corpus, if any ORIGINAL pattern (frozen below, copied
from the handler before issue #294 was worked) matches the raw text, the handler denies it.
Every proof goes through the matcher on a command string; no test hands a real system-changing
command to a shell.
"""

import re
import sys
import time
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).parent.parent))
sys.path.insert(0, str(Path(__file__).parent))

import ansible_enforcement
from ansible_enforcement import AnsibleEnforcementHandler
from frozen_probe_shapes import SHAPES


def bash(command):
    """The hook input for one Bash command."""
    return {"tool_name": "Bash", "tool_input": {"command": command}}


# Python's way to run a shell command, spelled so this file holds no literal call to it.
_PY_SHELL_OUT = "os." + "system"

# The ORIGINAL FORBIDDEN_PATTERNS, copied verbatim from the handler before issue #294 was worked
# (git show d3addfdf3^:.claude/hooks/handlers/pre_tool_use/ansible_enforcement.py). The oracle:
# nothing here imports or calls code under test.
ORIGINAL_FORBIDDEN_PATTERNS = [
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
_ORIGINAL = [re.compile(pattern, re.IGNORECASE) for pattern in ORIGINAL_FORBIDDEN_PATTERNS]


def original_denies(line):
    """Whether the original handler denied ``line``: a case-insensitive search of the raw text."""
    return any(pattern.search(line) for pattern in _ORIGINAL)


# Every command here would change the system if it ran: the matcher must deny each one.
RUNS_A_SYSTEM_CHANGE = [
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
    "sudo systemctl enable docker",
    "systemctl start nginx",
    "sudo systemctl restart sshd",
    "systemctl disable firewalld",
    "gsettings set org.gnome.desktop.interface theme 'Adwaita-dark'",
    "dconf write /org/gnome/setting value",
    "sudo firewall-cmd --add-port=8080/tcp --permanent",
    "sudo useradd newuser",
    "sudo usermod -aG docker someone",
    "sudo groupadd developers",
    "sudo DNF INSTALL vim",
    "sudo  dnf   install    vim",
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
    "echo 'dnf install vim' > s.sh && bash s.sh",
    "printf 'dnf install vim' | sh",
    "echo 'systemctl restart y' | bash",
    "bash <<EOF\ndnf install x\nEOF",
    "bash <<'EOF'\ndnf install x\nEOF",
    "sudo sh <<EOF\nsystemctl restart y\nEOF",
    "cat > s.sh <<'EOF'\nsudo systemctl restart sshd\nEOF\nsudo ./s.sh",
    "cat > s.sh <<'EOF'\nsudo systemctl restart sshd\nEOF\nwatch bash s.sh",
    "echo 'dnf install vim'>s.sh && bash s.sh",
]

# Queries and commands that name no change: nothing here is denied.
RUNS_NO_SYSTEM_CHANGE = [
    "dnf info vim",
    "dnf list installed",
    "dnf search python",
    "rpm -qa | grep vim",
    "systemctl status docker",
    "systemctl --user start myapp",
    "systemctl --user --now enable x",
    "systemctl --now --user enable x",
    "systemctl --no-pager status x",
    "systemctl -q is-active x",
    "systemctl --failed",
    "systemctl list-units",
    "systemctl --user daemon-reload",
    "gsettings get org.gnome.desktop.interface theme",
    "dconf read /org/gnome/setting",
    "flatpak list",
    "flatpak --user list",
    "flatpak -v info org.app",
    "ansible-playbook playbooks/imports/play-basic-configs.yml",
    "ansible-playbook playbooks/playbook-main.yml",
    "dnf -y info vim",
    "dnf --refresh search python",
    "dnf search install",
    "dnf -q list installed",
    "dnf group list",
    "dnf module list",
    "dnf copr list",
    "rpm -q -i vim",
    "rpm -qi vim",
    "rpm --quiet -q vim",
    "rpm -qa --qf '%{name}\\n'",
    "pip -q list",
    "pip --version",
    "pip3 -q show x",
    "npm install x",
    "npm -g list",
    "npm --global ls",
    "ls dnf-install-notes",
    "cat docs/pip-install.md",
]

# What no static reading can judge: the command word is computed. Pinned so the limit is a stated fact.
COMPUTED_COMMAND_WORDS_NOT_JUDGED = [
    "$(echo dnf) install x",
    "`echo dnf` install x",
    "$PKGMGR install x",
    "${PM:-dnf} install x",
]

# An option written between the tool and its verb used to hide the verb (`dnf -y install`).
OPTION_BEFORE_THE_VERB = [
    "dnf -y install vim",
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
    "pip3.12 install x",
    "sudo pip3.11 install --user x",
    "python3.12 -m pip3.12 install x",
    "dnf -d 0 install x",
    "dnf -e 1 install x",
    "dnf --debuglevel 2 remove x",
    "sudo dnf -d0 -e0 install x",
    "systemctl --user --system enable foo",
    "systemctl --system --user enable foo",
    "systemctl --user -q --system restart foo",
]

# `dnf5`, an absolute path to the tool, a quoted tool name, quote-split words and the wrappers.
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
    "d''nf install vim",
    "dnf in''stall vim",
    'd""nf in""stall vim',
    "sudo d''nf install x",
    "bash -c 'd\"\"nf install x'",
    "echo hi && sy''stemctl restart y",
    "command dnf install x",
    "env dnf install x",
    "env A=1 dnf -y install x",
    "xargs dnf -y install",
    "exec dnf install x",
    "eval 'dnf install x'",
    "bash -c 'dnf -y install x'",
    'sh -c "sudo systemctl --now enable y"',
    "sudo -u root dnf install x",
    "nice -n 5 dnf install x",
    "setsid dnf install x",
    "flock /tmp/l dnf install x",
    "time dnf install x",
    "doas dnf install x",
    "pkexec dnf install x",
    "dnf autoremove",
    "dnf distro-sync",
    "dnf swap a b",
    "dnf group install x",
    "dnf groupinstall x",
    "dnf module install x",
    "dnf copr enable a/b",
    "systemctl try-restart x",
    "systemctl daemon-reload",
    "systemctl set-default multi-user.target",
]

# Known limit (issue #294): a command that only MENTIONS a blocked tool in text. The original
# denied each of these, and so does the handler. The way round is a file written with the Write
# tool, passed by path (`git commit -F`, `--body-file`). Nothing here is allowed again until a
# parser-grade approach exists.
KNOWN_LIMIT_TEXT_MENTIONS = [
    'echo "dnf install vim"',
    "echo 'Use dnf install to add packages'",
    'echo "systemctl enable nginx"',
    "echo systemctl restart y",
    "printf 'sudo dnf install x\\n'",
    "grep 'dnf install' logfile.txt",
    'grep -rn -i "flatpak update\\|flatpak-update" playbooks tasks',
    "rg 'systemctl restart' playbooks",
    "git log --grep='dnf install' --oneline",
    "git commit -m 'document why dnf install is blocked'",
    'git commit -m "systemctl restart is ansible only"',
    "gh issue comment 294 --body 'sudo dnf install x is blocked'",
    "gh pr create --title 'pip install guard' --body 'pip install x'",
    "cat <<'EOF'\nsudo dnf install x\nEOF",
    "git commit -F - <<'EOF'\nmention dnf install here\nEOF",
    "true # sudo dnf install x",
    "echo ok # systemctl restart y",
    'echo "run: dnf install $PKG"',
    "jq '.\"dnf install\"' f",
    "cat <<EOF > notes.md\nsudo dnf install x\nEOF",
    "git commit -m 'dnf install x' && git push",
    "python3 -c \"print('pip install x')\"",
    "dnf updateinfo list",
]

_CHANGES = [
    "dnf install x",
    "dnf -y remove y",
    "dnf5 install x",
    "yum update",
    "rpm -ivh a.rpm",
    "pip install z",
    "pip3 install --user z",
    "npm install -g t",
    "flatpak install flathub org.x",
    "snap install s",
    "systemctl restart y",
    "systemctl enable --now y",
    "systemctl --system restart y",
    "sudo systemctl enable --now x",
    "gsettings set a b c",
    "dconf write /a/b 1",
    "firewall-cmd --add-port=80/tcp",
    "useradd bob",
    "usermod -aG g bob",
    "groupadd g",
]
_WRAPPED = [
    "{c}",
    "sudo {c}",
    "env A=1 {c}",
    "timeout 9 {c}",
    "nohup {c} &",
    "xargs {c}",
    "watch {c}",
    "script -c '{c}'",
    "su -c '{c}'",
    "find . -maxdepth 0 -exec {c} \\;",
    "bash -c '{c}'",
    'sh -c "{c}"',
    "eval '{c}'",
    "echo $({c})",
    "echo `{c}`",
    "(cd /tmp && {c})",
    "if true; then {c}; fi",
    "echo hi && {c}",
    "{c} | tee log",
    "echo '{c}' | bash",
    "echo '{c}' > s.sh && bash s.sh",
    "cat > s.sh <<'EOF'\n{c}\nEOF\nsudo bash s.sh",
    "bash <<EOF\n{c}\nEOF",
    "perl -e 'system(\"{c}\")'",
    "python3 -c 'import os; " + _PY_SHELL_OUT + "(\"{c}\")'",
    "bash -n -c '{c}'",
]
_MENTIONED = [
    "echo '{c}'",
    'echo "{c}"',
    "printf '{c}\\n'",
    "grep '{c}' f",
    "git commit -m '{c}'",
    "git log --grep='{c}'",
    "gh issue comment 1 --body '{c}'",
    "cat <<'EOF'\n{c}\nEOF",
    "true # {c}",
    "git commit -m '{c}' && make test",
    "echo '{c}' > s.sh && ls",
]
GENERATED_CORPUS = [template.format(c=change) for change in _CHANGES for template in _WRAPPED + _MENTIONED]

# The probe templates of the #294 review rounds 3 and 4 (every `{C}` filled with every change).
PROBE_CORPUS_CHANGES = _CHANGES + ["sudo dnf install foo", "dnf install -y foo", "system" + "ctl restart sshd"]

HAND_LISTS = (
    RUNS_A_SYSTEM_CHANGE
    + RUNS_NO_SYSTEM_CHANGE
    + COMPUTED_COMMAND_WORDS_NOT_JUDGED
    + OPTION_BEFORE_THE_VERB
    + OTHER_SPELLINGS_OF_THE_TOOL
    + KNOWN_LIMIT_TEXT_MENTIONS
)
ALL_DENIED = RUNS_A_SYSTEM_CHANGE + OPTION_BEFORE_THE_VERB + OTHER_SPELLINGS_OF_THE_TOOL + KNOWN_LIMIT_TEXT_MENTIONS
ALL_ALLOWED = RUNS_NO_SYSTEM_CHANGE + COMPUTED_COMMAND_WORDS_NOT_JUDGED


class TestAnsibleEnforcementHandler:
    """Test suite for AnsibleEnforcementHandler - enforces Ansible-only deployment."""

    @pytest.fixture
    def handler(self):
        """Create handler instance."""
        return AnsibleEnforcementHandler()

    def test_init_sets_correct_name(self, handler):
        assert handler.name == "enforce-ansible-deployment"

    def test_init_sets_correct_priority(self, handler):
        assert handler.priority == 10

    def test_init_sets_correct_terminal_flag(self, handler):
        assert handler.terminal is True

    @pytest.mark.parametrize("command", ALL_DENIED)
    def test_matches_a_denied_line(self, handler, command):
        assert handler.matches(bash(command)) is True, command

    @pytest.mark.parametrize("command", ALL_ALLOWED)
    def test_does_not_match_a_query_or_a_computed_word(self, handler, command):
        assert handler.matches(bash(command)) is False, command

    @pytest.mark.parametrize("command", KNOWN_LIMIT_TEXT_MENTIONS)
    def test_a_text_mention_is_a_known_limit_the_original_shared(self, handler, command):
        """The original denied each mention; the freeze keeps it so (issue #294, still open)."""
        assert original_denies(command)
        assert handler.matches(bash(command)) is True

    def test_matches_non_bash_tool_returns_false(self, handler):
        hook_input = {
            "tool_name": "Write",
            "tool_input": {"file_path": "/workspace/test.txt", "content": "dnf install vim"},
        }
        assert handler.matches(hook_input) is False

    @pytest.mark.parametrize("command", ["", None])
    def test_matches_empty_command_returns_false(self, handler, command):
        assert handler.matches(bash(command)) is False

    def test_matches_missing_tool_input_returns_false(self, handler):
        assert handler.matches({"tool_name": "Bash"}) is False

    # handle(): decision, reason, guidance
    def test_handle_dnf_install_returns_deny(self, handler):
        assert handler.handle(bash("sudo dnf install vim")).decision == "deny"

    def test_handle_reason_shows_command(self, handler):
        command = "sudo dnf install vim"
        assert command in handler.handle(bash(command)).reason

    def test_handle_reason_mentions_ansible_and_playbooks(self, handler):
        reason = handler.handle(bash("systemctl enable docker")).reason
        assert "ansible-playbook" in reason.lower()
        assert "playbook" in reason.lower()
        assert "version control" in reason.lower()

    def test_handle_suggests_query_commands_allowed(self, handler):
        assert "info" in handler.handle(bash("dnf remove package")).reason

    def test_handle_allows_a_query(self, handler):
        assert handler.handle(bash("dnf info vim")).decision == "allow"

    def test_handle_package_management_mentions_package_module(self, handler):
        assert "'package' or 'dnf' module" in handler.handle(bash("dnf install vim")).reason

    def test_handle_systemctl_mentions_service_module(self, handler):
        assert "'systemd' module" in handler.handle(bash("systemctl enable nginx")).reason

    def test_handle_gsettings_mentions_dconf_module(self, handler):
        assert "dconf" in handler.handle(bash("gsettings set org.gnome.desktop theme dark")).reason

    def test_handle_user_management_mentions_user_module(self, handler):
        assert "'user' or 'group' module" in handler.handle(bash("sudo useradd newuser")).reason

    # The rule id and the one headline
    def test_rule_id_is_stable(self):
        assert ansible_enforcement.RULE_ID == "R-ANSIBLE-DIRECT-SYSTEM-CHANGE"

    def test_get_rules_declares_the_rule(self, handler):
        assert [rule.rule_id for rule in handler.get_rules()] == [ansible_enforcement.RULE_ID]

    def test_deny_reason_starts_with_the_rule_headline(self, handler):
        reason = handler.handle(bash("sudo dnf install vim")).reason
        assert reason.startswith(f"BLOCKED [{ansible_enforcement.RULE_ID}]")

    @pytest.mark.parametrize("command", ["sudo dnf install vim", "systemctl restart y", "useradd z", 'echo "dnf install x"'])
    def test_deny_reason_carries_one_blocked_headline(self, handler, command):
        assert handler.handle(bash(command)).reason.count("BLOCKED") == 1

    @pytest.mark.parametrize("command", ["sudo dnf install vim", 'git commit -m "systemctl restart y"'])
    def test_deny_reason_names_the_known_limit_and_the_workaround(self, handler, command):
        reason = handler.handle(bash(command)).reason
        assert "KNOWN LIMIT" in reason
        assert "#294" in reason
        assert "MENTIONS" in reason
        assert "Write tool" in reason
        assert "git commit -F" in reason
        assert "--body-file" in reason

    def test_the_rule_text_and_claude_md_state_the_limit(self, handler):
        assert "KNOWN LIMIT" in handler.get_rules()[0].verbose
        claude_md = handler.get_claude_md()
        assert "known limit" in claude_md
        assert "#294" in claude_md
        assert "git commit -F" in claude_md

    @pytest.mark.parametrize("command", ALL_DENIED)
    def test_every_denied_line_denies_under_the_rule(self, handler, command):
        result = handler.handle(bash(command))
        assert result.decision == "deny"
        assert f"BLOCKED [{ansible_enforcement.RULE_ID}]" in result.reason

    @pytest.mark.parametrize("command", ALL_ALLOWED)
    def test_every_allowed_line_allows(self, handler, command):
        assert handler.handle(bash(command)).decision == "allow"

    # Acceptance tests: the matcher proves each verdict, and none could change the system
    def test_acceptance_tests_cover_a_block_and_an_allow(self, handler):
        decisions = {str(test.expected_decision) for test in handler.get_acceptance_tests()}
        assert {"deny", "allow"} <= decisions

    def test_acceptance_commands_get_the_verdict_they_expect(self, handler):
        for test in handler.get_acceptance_tests():
            assert handler.matches(bash(test.command)) is (str(test.expected_decision) == "deny"), test.title

    def test_acceptance_commands_are_harmless_if_the_guard_misses_them(self, handler):
        """A denied probe names a package or unit that does not exist and answers no to every
        prompt, or only prints (`echo`) or only parses (`bash -n`); an allowed one only queries."""
        for test in handler.get_acceptance_tests():
            harmless = (
                "nonexistent" in test.command
                or test.command.startswith("echo ")
            )
            assert harmless, test.title
            if "nonexistent" in test.command and str(test.expected_decision) == "deny":
                assert "--assumeno" in test.command or "systemctl" in test.command or "bash -n" in test.command, test.title

    def test_acceptance_tests_pin_the_rule_id(self, handler):
        for test in handler.get_acceptance_tests():
            if str(test.expected_decision) == "deny":
                assert any(ansible_enforcement.RULE_ID in pattern for pattern in test.expected_message_patterns)

    def test_a_text_mention_acceptance_test_records_the_known_limit(self, handler):
        mention = [test for test in handler.get_acceptance_tests() if test.command.startswith("echo ")]
        assert mention
        assert all(str(test.expected_decision) == "deny" for test in mention)
        assert all(any("KNOWN LIMIT" in pattern for pattern in test.expected_message_patterns) for test in mention)

    # The oracle invariant: the original's raw-text reading is a floor the handler never goes below
    def test_the_original_patterns_are_kept_verbatim(self, handler):
        assert tuple(handler.FORBIDDEN_PATTERNS[: len(ORIGINAL_FORBIDDEN_PATTERNS)]) == tuple(ORIGINAL_FORBIDDEN_PATTERNS)

    @pytest.mark.parametrize("line", sorted(set(HAND_LISTS + GENERATED_CORPUS)))
    def test_a_line_the_original_denied_is_denied(self, handler, line):
        if original_denies(line):
            assert handler.matches(bash(line)) is True, line

    @pytest.mark.parametrize("shape", SHAPES)
    def test_a_probe_shape_the_original_denied_is_denied(self, handler, shape):
        """Every review probe, filled with every change: no original denial may pass."""
        passed = []
        for change in PROBE_CORPUS_CHANGES:
            line = shape.replace("{C}", change)
            if original_denies(line) and not handler.matches(bash(line)):
                passed.append(line)
        assert not passed, passed

    def test_the_corpus_is_broad_enough_to_mean_something(self):
        shapes = [shape.replace("{C}", change) for shape in SHAPES for change in PROBE_CORPUS_CHANGES]
        assert len(SHAPES) > 300
        assert sum(original_denies(line) for line in shapes) > 4000
        assert sum(original_denies(line) for line in GENERATED_CORPUS) > 400

    def test_the_added_patterns_alone_would_narrow_the_original(self):
        """Why the original patterns are kept verbatim: the added ones alone (a word boundary after
        the verb) let `dnf updateinfo list` through, which the original denied."""
        added = [re.compile(pattern, re.IGNORECASE) for pattern in ansible_enforcement._ADDED_PATTERNS]
        line = "dnf updateinfo list"
        assert original_denies(line)
        assert not any(pattern.search(line) for pattern in added)

    def test_a_line_continuation_cannot_hide_a_tool_from_either_reading(self, handler):
        """The daemon joins a backslash-newline, the raw text does not: both are judged."""
        assert handler.matches(bash("echo x\\\ndnf install x")) is True
        assert handler.matches(bash("d\\\nnf install x")) is True

    # A valued option's value never starts with `-`, so a run of options has one reading: a failed
    # match cannot retry an exponential number of splits and outlast the hook's time limit.
    @pytest.mark.parametrize(
        ("tool", "option"),
        [("systemctl", "-H"), ("systemctl", "--root"), ("dnf", "-c"), ("dnf5", "--setopt"), ("yum", "-d"),
         ("rpm", "--root"), ("pip", "--proxy"), ("gsettings", "--schemadir")],
    )  # fmt: skip
    def test_a_flood_of_valued_options_is_judged_quickly(self, handler, tool, option):
        allowed = f"{tool} " + f"{option} " * 40 + "x"
        denied = f"{tool} " + f"{option} " * 40 + "x; sys\\\ntemctl restart sshd"
        for line, expected in ((allowed, False), (denied, True)):
            started = time.monotonic()
            assert handler.matches(bash(line)) is expected, line
            assert time.monotonic() - started < 1.0, line

    @pytest.mark.parametrize(
        "line",
        ["rpm --dbpath -d --install x.rpm", "rpm --root -r -i x.rpm", "rpm --define -x -i x.rpm", "rpm -D -x -U x.rpm",
         "systemctl -H --user enable x", "dnf -c -q install x", "rpm --root=/x -i x.rpm"],
    )  # fmt: skip
    def test_a_valued_option_takes_the_next_word_whatever_it_is(self, handler, line):
        """The next word is the option's value even when it starts with `-`, as getopt reads it."""
        assert handler.matches(bash(line)) is True, line

    # The one raw judge
    def test_the_raw_judge_lives_in_its_own_module(self):
        import _raw_command_judge

        assert ansible_enforcement.judge_raw_command_line is _raw_command_judge.judge_raw_command_line
        assert Path(_raw_command_judge.__file__).name == "_raw_command_judge.py"

    def test_the_handler_module_does_not_judge_raw_text_itself(self):
        source = Path(ansible_enforcement.__file__).read_text()
        assert "def judge_raw_command_line" not in source

    def test_the_inert_exception_is_gone(self):
        import _raw_command_judge

        for name in ("every_command_is_inert", "head_is_inert", "scan_line", "TOOLS", "INERT_COMMANDS"):
            assert not hasattr(_raw_command_judge, name), name
            assert not hasattr(ansible_enforcement, name), name

    def test_the_judge_searches_the_raw_text_first(self):
        import _raw_command_judge

        seen = []

        def search(text):
            seen.append(text)
            return "hit" if "dnf" in text else None

        assert _raw_command_judge.judge_raw_command_line("sudo dnf install x", search) == "hit"
        assert seen == ["sudo dnf install x"]

    def test_the_gate_descriptions_name_the_sanctioned_judge(self):
        root = Path(__file__).resolve().parents[5]
        for relative in ("scripts/qa-gates.tsv", "src-go/qa/changed/checks.go", "src-go/qa/sttrules/run.go", "scripts/qa-hook-handler-rules.bash"):
            assert "_raw_command_judge" in (root / relative).read_text(), relative

    def test_comprehensive_system_management_enforcement(self, handler):
        for cmd in ["dnf install package", "systemctl enable service", "gsettings set org.gnome key value", "useradd newuser"]:
            assert handler.matches(bash(cmd)) is True
