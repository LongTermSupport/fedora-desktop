"""Tests for AnsibleEnforcementHandler - blocks manual system commands.

The handler judges the line as written, exactly as the original did, with one exception
(issue #294): when EVERY command the line runs is inert (echo, printf, grep, cat, a git commit...),
nothing on it can run a system change, so a text-only mention passes. Any other command on the
line (a wrapper, a shell, an interpreter, find, xargs, make, watch, anything unknown) puts the
whole line back to the raw text. No regression is possible by construction, and the invariant
tests below prove it over a broad corpus. Every proof here goes through the matcher on a command
string; no test hands a real system-changing command to a shell.
"""

import re
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
    'eval "$(cat s.sh)"',
    "exec ./s.sh",
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
    "(jq '.\"dnf install\"' f)",
    "if true; then jq '.\"dnf install\"' f; fi",
]

# Accepted over-blocking: a shell, an interpreter or a wrapper is not inert, so the line is judged
# on its raw text, exactly as the original handler did. `bash -n` would not run its body, but the
# handler does not read option letters any more.
ACCEPTED_OVERBLOCKING = [
    "python3 -c \"print('pip install x')\"",
    "bash -n -c 'sudo dnf install x'",
    "bash -nc 'dnf install x'",
    "cat > notes.md <<'EOF'\nsudo dnf install x\nEOF\nbash -n notes.md",
    # an inert write and an unrelated shell run on one line (review of round 2, item 3)
    "echo 'dnf install vim' > notes.txt && bash other.sh",
    "git commit -m 'playbook: dnf install foo' > /tmp/c.log && bash scripts/qa-changed.bash",
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
    "bash +n -c 'dnf install x'",
    "bash -n +n -c 'dnf install x'",
    "bash -o noexec +o noexec -c 'dnf install x'",
    "sudo bash +n -c 'dnf install x'",
    "sudo bash -c 'dnf -y install x'",
]

# The review of round 2 (blocking 2): each of these runs a written script, and the original denied
# it. No runner is named by a list: a line with any non-inert command is judged on its raw text.
_RUNNERS = [
    "script -c 'bash s.sh'",
    "script -qc ./s.sh /dev/null",
    "watch bash s.sh",
    "find . -maxdepth 0 -exec bash s.sh \\;",
    "systemd-run bash s.sh",
    "su -c 'bash s.sh'",
    "runuser -u root -- bash s.sh",
    "flatpak-spawn --host bash s.sh",
    "busybox sh s.sh",
    "perl -e 'system(\"bash s.sh\")'",
    f"python3 -c 'import os; {_PY_SHELL_OUT}(open(\"s.sh\").read())'",
    "make -E 'x:;bash s.sh'",
    "bash s.sh",
    "sudo bash s.sh",
    "xargs -a s.sh bash -c",
    "ssh localhost bash < s.sh",
    "toolbox run bash s.sh",
    "bash +n s.sh",
]
GLUED_REDIRECT_THEN_RUN = [
    "echo 'dnf install vim'>s.sh && bash s.sh",
    "echo 'dnf install vim'>>s.sh && sudo bash s.sh",
    "printf 'systemctl restart y'>s.sh;sh s.sh",
    "echo 'dnf install vim'>s.sh && ./s.sh",
]
WRITTEN_THEN_RUN_BY_ANY_RUNNER = [f"echo 'dnf install vim' > s.sh && {runner}" for runner in _RUNNERS] + [
    f"cat > s.sh <<'EOF'\ndnf install vim\nEOF\n{runner}" for runner in _RUNNERS
]

# Item 5 of the review of round 2: gaps the original also had.
VERSIONED_AND_VALUED_OPTIONS = [
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
QUOTE_SPLIT_WORDS = [
    "d''nf install vim",
    "dnf in''stall vim",
    'd""nf in""stall vim',
    "sudo d''nf install x",
    "bash -c 'd\"\"nf install x'",
    "echo hi && sy''stemctl restart y",
]
# The verbs the round 2 report listed as found and not fixed.
MORE_VERBS = [
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
MORE_VERBS_ALLOWED = [
    "dnf group list",
    "dnf module list",
    "dnf copr list",
    "systemctl list-units",
    "systemctl --user daemon-reload",
]

# Lines whose every command is inert: text only, nothing can run. The original denied each.
ALL_INERT_MENTIONS = [
    'echo "dnf install vim"',
    "git commit -m 'document why dnf install is blocked' && git push",
    "git -C sub commit -m 'systemctl restart y' && git log -1 --oneline",
    "gh issue comment 294 -b 'sudo dnf install x'",
    "gh pr create --title t --body-file notes.md",
    "gh issue comment 1 --body-file notes.md",
    "grep -rn 'dnf install' playbooks | wc -l",
    "cat notes.md | grep 'pip install' | sort | uniq",
    "git commit -F - <<'EOF'\nmention dnf install here\nEOF",
    "cat > notes.md <<'EOF'\nsudo dnf install x\nEOF\nls",
    "for f in a b; do echo $f; done",
    "git add notes.md && git commit -m 'flatpak update'",
    "git commit -m \"$(cat <<'EOF'\nfoo dnf install bar\nEOF\n)\"",
    "ls && echo 'systemctl restart y' && true",
]
# The same inert text with one command that is not inert: judged as written, so denied.
ONE_NON_INERT_COMMAND_MAKES_IT_RAW = [
    "echo 'dnf install x' && sudo true",
    "git commit -m 'dnf install x' && make test",
    "git commit -m 'dnf install x' && ./script.sh",
    "echo 'dnf install x' | xargs echo",
    "git commit -m 'dnf install x' && env true",
    "echo 'dnf install x' && timeout 5 true",
    "echo 'dnf install x' && python3 -c 'pass'",
    "git -c core.pager='dnf install x' log",
    "git log --exec='dnf install x'",
    "rg --pre 'dnf install x' pattern .",
    "sort --compress-program='dnf install x' f",
    "echo 'dnf install x' > s.sh && X=1 echo y",
]

ALL_DENIED = (
    WRITTEN_THEN_RUN_BY_ANY_RUNNER
    + GLUED_REDIRECT_THEN_RUN
    + VERSIONED_AND_VALUED_OPTIONS
    + QUOTE_SPLIT_WORDS
    + MORE_VERBS
    + ONE_NON_INERT_COMMAND_MAKES_IT_RAW
    + ACCEPTED_OVERBLOCKING
    + 
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
    + MORE_VERBS_ALLOWED
    + ALL_INERT_MENTIONS
)


# The patterns of the original handler (d3addfdf3^), frozen: the baseline the invariant compares to.
ORIGINAL_PATTERNS = [
    re.compile(pattern, re.IGNORECASE)
    for pattern in [
        r'\bdnf\s+(install|remove|erase|update|upgrade|downgrade|reinstall)',
        r'\byum\s+(install|remove|erase|update|upgrade)',
        r'\brpm\s+(-[iUeFh]*[iUeF][iUeFh]*|--install|--upgrade|--erase|--freshen)',
        r'\bpip3?\s+install\b',
        r'\bnpm\s+install\s+-g',
        r'\bflatpak\s+(install|remove|uninstall|update)',
        r'\bsnap\s+(install|remove)',
        r'\bsystemctl\s+(?!--user)(enable|disable|start|stop|restart|reload|mask|unmask)',
        r'\bgsettings\s+set',
        r'\bdconf\s+write',
        r'\bgconftool(-2)?\s+--set',
        r'\bfirewall-cmd\s+.*--(add|remove|zone)',
        r'\buseradd\b',
        r'\busermod\b',
        r'\buserdel\b',
        r'\bgroupadd\b',
        r'\bgroupmod\b',
        r'\bgroupdel\b',
    ]
]


def original_denies(line):
    """Whether the original handler denied ``line``: a search of the raw text."""
    return any(pattern.search(line) for pattern in ORIGINAL_PATTERNS)


# Lines the original denied that the raw judge now passes for a reason other than inertness: the
# round 2 word boundary after a verb, which frees a query that only starts with a verb's letters.
DOCUMENTED_NARROWING = {"dnf updateinfo list"}

_INERT_WORDS = {"echo", "printf", "grep", "egrep", "fgrep", "rg", "cat", "jq", "git", "gh", "true", "false", "test", "[", "wc", "sort", "head", "tail", "ls", "pwd", "cd", "uniq", "for", "done", "fi"}


def only_inert_words(line):
    """An independent check: the first word of every `;`, `&&`, `||`, `|` or newline piece is inert.

    Not used for lines with a heredoc body or a substitution: the body is data, not pieces.
    """
    if "<<" in line or "$(" in line or "`" in line:
        return True
    unquoted = re.sub(r"'[^']*'|\"[^\"]*\"", "_", line)
    pieces = re.split(r"&&|\|\||;|\||\n", unquoted)
    for piece in pieces:
        words = piece.replace("(", " ").split()
        while words and words[0] in {"do", "then", "else", "if", "!", "{", "("}:
            words = words[1:]
        if words and words[0] not in _INERT_WORDS:
            return False
    return True


_CHANGES = [
    "dnf install x",
    "dnf -y remove y",
    "yum update",
    "rpm -ivh a.rpm",
    "pip install z",
    "pip3 install --user z",
    "npm install -g t",
    "flatpak install flathub org.x",
    "snap install s",
    "systemctl restart y",
    "systemctl enable --now y",
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
    "sudo -n {c}",
    "env A=1 {c}",
    "timeout 9 {c}",
    "nice -n 5 {c}",
    "nohup {c} &",
    "setsid {c}",
    "xargs {c}",
    "command {c}",
    "exec {c}",
    "time {c}",
    "stdbuf -oL {c}",
    "ionice -c3 {c}",
    "flock /tmp/l {c}",
    "watch {c}",
    "script -c '{c}'",
    "su -c '{c}'",
    "runuser -u root -- {c}",
    "systemd-run {c}",
    "find . -maxdepth 0 -exec {c} \\;",
    "busybox {c}",
    "bash -c '{c}'",
    'sh -c "{c}"',
    "eval '{c}'",
    "$({c})",
    "echo $({c})",
    "echo `{c}`",
    "(cd /tmp && {c})",
    "{{ {c}; }}",
    "if true; then {c}; fi",
    "for i in 1 2; do {c}; done",
    "echo hi && {c}",
    "echo hi; {c}",
    "true || {c}",
    "{c} | tee log",
    "yes | {c}",
    "echo hi\n{c}",
    "echo '{c}' | bash",
    "echo '{c}' | sudo sh -s",
    "printf '{c}' | sh",
    "echo '{c}' > s.sh && bash s.sh",
    "echo '{c}'>s.sh && bash s.sh",
    "echo '{c}' > s.sh && sudo ./s.sh",
    "echo '{c}' | tee s.sh && watch bash s.sh",
    "cat > s.sh <<'EOF'\n{c}\nEOF\nbash s.sh",
    "cat > s.sh <<'EOF'\n{c}\nEOF\nsudo bash s.sh",
    "bash <<EOF\n{c}\nEOF",
    "bash <<'EOF'\n{c}\nEOF",
    "perl -e 'system(\"{c}\")'",
    "python3 -c 'import os; " + _PY_SHELL_OUT + "(\"{c}\")'",
    "make -E 'x:;{c}'",
    "bash -n -c '{c}'",
    "bash +n -c '{c}'",
]
# the same text only mentioned: echo, grep, a message, a heredoc to cat
_MENTIONED = [
    "echo '{c}'",
    'echo "{c}"',
    "printf '{c}\\n'",
    "grep '{c}' f",
    "rg '{c}' playbooks",
    "git commit -m '{c}'",
    "git log --grep='{c}'",
    "gh issue comment 1 --body '{c}'",
    "cat <<'EOF'\n{c}\nEOF",
    "cat > notes.md <<'EOF'\n{c}\nEOF",
    "true # {c}",
    "echo '{c}' && git commit -m 'x'",
    "git commit -m '{c}' && make test",
    "echo '{c}' | xargs echo",
    "echo '{c}' > s.sh && ls",
]
GENERATED_CORPUS = [template.format(c=change) for change in _CHANGES for template in _WRAPPED + _MENTIONED]
INVARIANT_CORPUS = (
    GENERATED_CORPUS
    + RUNS_A_SYSTEM_CHANGE
    + RUNS_NO_SYSTEM_CHANGE
    + WRITTEN_SCRIPT_LINES
    + PIPED_INTO_A_SHELL
    + WRITES_BUT_RUNS_NOTHING
    + OPTION_BEFORE_THE_VERB
    + OPTION_BEFORE_THE_VERB_ALLOWED
    + OTHER_SPELLINGS_OF_THE_TOOL
    + COMPUTED_COMMAND_WORDS_NOT_JUDGED
    + INERT_TEXT_THE_DAEMON_KEEPS
    + LIVE_LOOKALIKES_OF_INERT_TEXT
    + ACCEPTED_OVERBLOCKING
    + WRITTEN_THEN_RUN_BY_ANY_RUNNER
    + GLUED_REDIRECT_THEN_RUN
    + VERSIONED_AND_VALUED_OPTIONS
    + QUOTE_SPLIT_WORDS
    + MORE_VERBS
    + MORE_VERBS_ALLOWED
    + ALL_INERT_MENTIONS
    + ONE_NON_INERT_COMMAND_MAKES_IT_RAW
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
                assert not test.command.startswith("echo "), test.title
                assert "--assumeno" in test.command or "systemctl" in test.command or "bash -n" in test.command, test.title

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

    def test_a_shell_probe_is_denied_on_its_raw_text_and_cannot_run_its_body(self, handler):
        """`bash -n -c '...'` parses and runs nothing, and a shell is not inert, so the line is
        judged as written: a harmless real-run shape that proves the raw default."""
        probes = [test for test in handler.get_acceptance_tests() if test.command.startswith("bash -n -c")]
        assert probes
        assert all(str(test.expected_decision) == "deny" for test in probes)

    # The invariant: the raw line is the judge, an all-inert line the only exception
    @pytest.mark.parametrize("line", sorted(set(INVARIANT_CORPUS)))
    def test_a_line_the_original_denied_is_denied_unless_every_command_is_inert(self, handler, line):
        if not original_denies(line) or handler.matches(bash(line)):
            return
        assert line in DOCUMENTED_NARROWING or ansible_enforcement.every_command_is_inert(line), line
        assert line in DOCUMENTED_NARROWING or only_inert_words(line), line

    def test_the_corpus_is_broad_enough_to_mean_something(self):
        denied = [line for line in set(INVARIANT_CORPUS) if original_denies(line)]
        assert len(denied) > 600

    def test_the_exception_exists(self, handler):
        passed = [line for line in set(INVARIANT_CORPUS) if original_denies(line) and not handler.matches(bash(line))]
        assert len(passed) > 50

    @pytest.mark.parametrize("line", ALL_INERT_MENTIONS)
    def test_an_all_inert_line_is_judged_by_what_it_runs(self, handler, line):
        assert ansible_enforcement.every_command_is_inert(line)
        assert only_inert_words(line)
        assert handler.matches(bash(line)) is False

    @pytest.mark.parametrize("line", ONE_NON_INERT_COMMAND_MAKES_IT_RAW)
    def test_one_command_that_is_not_inert_puts_the_whole_line_back_to_raw_text(self, handler, line):
        assert not ansible_enforcement.every_command_is_inert(line)
        assert handler.matches(bash(line)) is True

    @pytest.mark.parametrize(
        "word",
        ["sudo", "env", "timeout", "nice", "nohup", "bash", "sh", "xargs", "find", "make", "watch", "python3", "perl", "script", "su", "busybox", "eval", "exec", "ssh", "unknowntool"],
    )
    def test_wrappers_shells_and_unknown_commands_are_not_inert(self, word):
        assert not ansible_enforcement.every_command_is_inert(f"{word} true")

    @pytest.mark.parametrize("word", ["echo", "printf", "grep", "rg", "cat", "jq", "true", "wc", "ls"])
    def test_the_inert_commands_are_inert(self, word):
        assert ansible_enforcement.every_command_is_inert(f"{word} x")

    def test_the_closed_list_machinery_is_gone(self):
        """The written-script tracking, the `-n` reader and the wrapper and shell lists were each
        a closed list that a review found a way round; the raw line replaces them."""
        for name in (
            "_runs_what_the_daemons_view_cannot_follow",
            "_is_syntax_check_only",
            "_reads_stdin_as_shell",
            "_runs_shell_or_script",
            "_written_names",
            "_WRAPPERS",
            "_PLAIN_WRAPPERS",
            "_WRITERS",
            "_REDIRECT_TO_FILE",
        ):
            assert not hasattr(ansible_enforcement, name), name

    def test_the_inert_list_is_documented_and_holds_no_wrapper(self):
        assert "inert" in ansible_enforcement.__doc__
        assert ansible_enforcement.INERT_COMMANDS >= {"echo", "printf", "grep", "cat", "git", "gh", "true"}
        hostile = {"sudo", "env", "bash", "sh", "xargs", "find", "make", "watch", "timeout", "nice", "nohup", "python3"}
        assert not ansible_enforcement.INERT_COMMANDS & hostile

    def test_the_one_raw_judge_is_a_named_visible_function(self):
        assert callable(ansible_enforcement.judge_raw_command_line)

    # Integration Tests
    def test_comprehensive_system_management_enforcement(self, handler):
        for cmd in ["dnf install package", "systemctl enable service", "gsettings set org.gnome key value", "useradd newuser"]:
            assert handler.matches(bash(cmd)) is True
        assert handler.matches(bash("ansible-playbook playbooks/playbook-main.yml")) is False
