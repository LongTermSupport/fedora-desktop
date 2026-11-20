"""The shapes of the round 3 review of issue #294 and the invariants round 4 pins.

Not a test module: test_ansible_enforcement.py imports the lists. Every template holds `{C}`,
the text of a system change; `fill` puts a real one in. Each list is a class of line:

- DENIED_NEVER_INERT: the line must be denied, and no reading may call it all-inert (the review's
  14 shapes that write text for git to run, the startup-file writes, the abbreviated options that
  run a program, git's global options, every substitution, every write, expanded option words).
- INERT_ALLOWED: the line holds `{C}` only as text and uses only listed options, so it passes.
- NOT_LISTED_OPTIONS: an option no allow-list names, so the raw text decides.
"""

from review_probe_shapes import SHAPES as _PROBE_SHAPES

CHANGES = [
    "dnf install -y foo",
    "sudo dnf install foo",
    "systemctl restart sshd",
    "useradd bob",
    "pip install requests",
    "flatpak install x",
    "npm install -g x",
]


def fill(templates, changes=CHANGES):
    return [template.replace("{C}", change) for template in templates for change in changes]


def probe_shapes():
    """The reviewer's 121 shapes, a heredoc block (and a git commit after it) kept as one shape."""
    lines = _PROBE_SHAPES.strip("\n").split("\n")
    shapes = []
    index = 0
    while index < len(lines):
        line = lines[index]
        if "<<" in line and "<<<" not in line:
            block = [line]
            index += 1
            while lines[index] != "EOF":
                block.append(lines[index])
                index += 1
            block.append("EOF")
            index += 1
            if index < len(lines) and lines[index].startswith("git commit"):
                block.append(lines[index])
                index += 1
            shapes.append("\n".join(block))
        else:
            shapes.append(line)
            index += 1
    return shapes


# Item 1: an inert command writes text, an inert git subcommand on the same line runs it.
WRITE_THEN_GIT_RUNS_IT = [
    "echo '{C}' >> scripts/git-hooks/pre-commit && git commit -m x",
    "printf '%s\\n' '{C}' >> scripts/git-hooks/pre-commit; git commit -qm x",
    "echo '{C}' >> .git/hooks/pre-commit && git commit -m x",
    "echo '{C}' >> scripts/git-hooks/pre-push && git add .",
    "echo '{C}' >> scripts/git-hooks/commit-msg; git commit -m y",
    "cat >> scripts/git-hooks/pre-commit <<'EOF'\n{C}\nEOF\ngit commit -m x",
    "echo '[core] sshCommand = {C}' >> .git/config && git status",
    "echo '[core] fsmonitor = {C}' >> .git/config; git status",
    "echo '[filter \"x\"] clean = {C}' >> .git/config && echo '* filter=x' > .gitattributes && git add .",
    "echo '[diff] external = {C}' >> .git/config && git diff",
    "echo '[core] pager = {C}' >> .git/config && git log",
    "echo '[alias] lg = !{C}' >> .git/config && git log",
    "echo '[core] hooksPath = x' >> .git/config; git commit -m '{C}'",
    "printf '[core]\\n\\tfsmonitor = {C}\\n' >> .git/config && git status",
    "git log && echo '{C}' >> scripts/git-hooks/pre-commit",
]

# Item 3: text planted in a startup file runs later.
STARTUP_FILE_WRITES = [
    "echo 'sudo {C}' >> ~/.bashrc",
    "echo '{C}' >> ~/.bash_profile",
    "echo '{C}' > ~/.config/autostart/x.desktop",
    "echo '{C}' > /etc/profile.d/x.sh",
    "echo '{C}' >> .envrc && cd .",
    "echo '{C}' > Makefile",
    "echo \"alias ls='{C}'\" >> ~/.bashrc",
    "echo '{C}'>~/.bashrc",
]

# Item 2: an abbreviated or glued option that makes the tool run a program.
RUNS_A_PROGRAM_OPTIONS = [
    "git fetch --upload='{C};' .",
    "git fetch --upload-pa='{C};' .",
    "git fetch --upload-pack='{C};' .",
    "git push --rec='{C};' .",
    "git push --exec='{C};' .",
    "git push --receive-pack='{C};' .",
    "git grep --open-files='{C}' x",
    "git grep -O'{C}' x",
    "git grep -O '{C}' x",
    "git grep --open-files-in-pager='{C}' x",
    "git log --output=/tmp/x '{C}'",
    "git diff --output=/tmp/x '{C}'",
    "git show --output=/tmp/x '{C}'",
    "git diff --ext-diff '{C}'",
    "git log -p --ext-diff '{C}'",
    "sort --compress='{C}' x",
    "sort --compress-program='{C}' x",
    "rg --pr='{C}' x",
    "rg --pre='{C}' x",
    "rg --pre '{C}' x",
    "rg --pre-glob='{C}' x",
    "rg --hostname-bin='{C}' x",
    "git commit --message='{C}'",
    "git log --oneli '{C}'",
]

# git's global options run programs or move the repository: none may precede the subcommand.
GIT_GLOBAL_OPTIONS = [
    "git -c core.pager='{C}' log",
    "git -c alias.x='!{C}' x",
    "git -C . -c core.sshCommand='{C}' fetch",
    "git -C sub commit -m '{C}'",
    "git -C sub log -1 '{C}'",
    "git --exec-path=/x log '{C}'",
    "git --git-dir=.git commit -m '{C}'",
    "git --work-tree=. commit -m '{C}'",
    "git --config-env=core.pager=V log '{C}'",
    "git -c core.fsmonitor='{C}' status",
    "git -P log '{C}'",
]

# A substitution executes, in double quotes too.
SUBSTITUTIONS = [
    "echo x $({C})",
    'echo "$({C})"',
    'echo "x $(date)" \'{C}\'',
    'grep "$(sudo {C})" f',
    "echo `{C}`",
    'echo "`{C}`"',
    "cat <({C})",
    "echo hi > >({C})",
    "git commit -m \"$(cat <<'EOF'\n{C}\nEOF\n)\"",
    'git commit -m "$(date) {C}"',
    "cat <<EOF\n$({C})\nEOF",
    "git log --grep=\"$(echo x)\" '{C}'",
    "printf '%s' \"$(echo '{C}')\"",
]

# Every redirect that creates or truncates a file, however it is spelled.
FILE_WRITES = [
    "echo '{C}' > notes.md",
    "echo '{C}' >> notes.md",
    "echo '{C}' &> notes.md",
    "echo '{C}' &>> notes.md",
    "echo '{C}' >| notes.md",
    "echo '{C}' 2> notes.md",
    "echo '{C}' 3> notes.md",
    "echo '{C}' 1>notes.md",
    "echo '{C}'>notes.md",
    "echo '{C}' >&notes.md",
    "echo '{C}' <> notes.md",
    "cat > notes.md <<'EOF'\n{C}\nEOF",
    "cat <<'EOF' > notes.md\n{C}\nEOF",
    "git commit -m '{C}' > /tmp/c.log",
    "git log --oneline '{C}' > log.txt",
    "grep -rn '{C}' . > hits.txt",
    "jq -n '\"{C}\"' > out.json",
    "printf '{C}' >> \"$HOME/f\"",
    "echo '{C}' > /dev/null.bak",
    "echo '{C}' > /dev/nullx",
    "echo '{C}' > /dev/null/../x",
    "echo '{C}' | tee notes.md",
    "echo '{C}' | tee -a notes.md",
    "git commit -m '{C}' && git log > log.txt",
]

# A separator glued to an inert command: the line is split the way the shell splits it.
SEPARATOR_SHAPES = [
    "echo x & {C}",
    "echo x &{C}",
    "echo x|{C}",
    "echo x;{C}",
    "echo x&&{C}",
    "echo x ||{C}",
    "echo x |& {C}",
    "echo x\n{C}",
    "echo x\\\n; {C}",
    "git status; ( {C} )",
    "git status && { {C}; }",
    "echo x && ! {C}",
    "if true; then {C}; fi",
    "echo a && time {C}",
]

# A variable or a brace expansion can expand to an option: the words are not literal.
EXPANDED_WORDS = [
    "for x in --output=/tmp/o; do git log $x '{C}'; done",
    "for x in --exec-path=/x; do git status $x; done && echo '{C}'",
    "X=--output=/tmp/o; git log $X '{C}'",
    "git log {-1,--output=/tmp/o} '{C}'",
    "git log \"$OPT\" '{C}'",
    "git commit -m '{C}' $OPT",
    "git add $FILES '{C}'",
    "grep -rn '{C}' $DIR",
    "git log -{1,2} '{C}'",
]

# Not on any exact list: head, subcommand or option.
NOT_LISTED = [
    "git push origin HEAD '{C}'",
    "git fetch origin '{C}'",
    "git tag -a v1 -m '{C}'",
    "git grep -n '{C}' -- playbooks",
    "git stash push -m '{C}'",
    "git commit -C HEAD -m '{C}'",
    "git commit --amend -m '{C}'",
    "git commit -e -m '{C}'",
    "git commit --no-verify -m '{C}'",
    "git commit --template=t -m '{C}'",
    "git add -p '{C}'",
    "git status --porcelain=v2 '{C}'",
    "git diff -O orderfile '{C}'",
    "grep -f pats '{C}' f",
    "grep -P '{C}' f",
    "grep -H '{C}' f",
    "grep --include='*.md' '{C}' f",
    "grep --exclude-from=x '{C}' f",
    "grep --color=always '{C}' f",
    "egrep '{C}' f",
    "fgrep '{C}' f",
    "rg -z '{C}'",
    "rg -j2 '{C}'",
    "jq -f prog.jq '{C}'",
    "jq --from-file prog.jq '{C}'",
    "jq -L dir '{C}'",
    "jq --rawfile a f '{C}'",
    "printf -v x '{C}'",
    "echo --help '{C}'",
    "cat -x '{C}'",
    "wc --files0-from=x '{C}'",
    "wc -L '{C}'",
    "cd -P x && echo '{C}'",
    "gh issue comment 1 -e --body '{C}'",
    "gh issue comment 1 --editor --body '{C}'",
    "gh issue comment 1 --web --body '{C}'",
    "gh pr create --fill --body '{C}'",
    "gh issue comment 1; echo '{C}'",
    "gh api repos/x/y -f body='{C}'",
    "gh pr comment 1 --body-file; echo '{C}'",
    "sort '{C}' f",
    "head '{C}' f",
    "tail '{C}' f",
    "ls '{C}'",
    "pwd '{C}'",
    "uniq '{C}'",
    "test -n '{C}'",
    "[ -n '{C}' ]",
    "echo() { :; }; echo '{C}'",
]

# What passes: only text, only listed options, no write, no substitution, no expansion.
INERT_ALLOWED = [
    "echo '{C}'",
    "echo -n '{C}'",
    "echo -ne '{C}'",
    "printf '%s' '{C}'",
    "printf '%s\\n' '{C}' 'more'",
    "grep -rn '{C}' .",
    "grep -rnE -e '{C}' f",
    "grep -c -w -v -F -l -i '{C}' f",
    "grep -nie '{C}' f",
    "rg -n -i --color=never '{C}'",
    "rg -e '{C}' -l",
    "rg -nw '{C}' playbooks",
    "cat -n notes.md | grep '{C}'",
    "jq -r --arg a b '.x | \"{C}\"' f.json",
    "jq -n '\"{C}\"'",
    "git commit -S -F notes.md",
    "git commit -am '{C}'",
    "git commit -qm '{C}'",
    "git commit -m'{C}'",
    "git commit -m 'x' -m '{C}'",
    "git commit -F - <<'EOF'\n{C}\nEOF",
    "git log --oneline -5 --grep='{C}'",
    "git log -n 3 --grep '{C}'",
    "git log -1 --format=%s | cat",
    "git show --stat HEAD",
    "git diff --cached --stat -- '{C}'",
    "git status -sb",
    "git add -A && git commit -m '{C}'",
    "git add -- '{C}'",
    "gh issue comment 1 --body-file x.md",
    "gh pr create -t '{C}' -b 'x'",
    "gh issue create --title t --body '{C}'",
    "cd sub && git commit -m '{C}'",
    "grep -n x f | wc -l && echo '{C}'",
    "git commit -m '{C}' 2>&1",
    "git status > /dev/null && echo '{C}'",
    "echo '{C}' 2> /dev/null",
    "echo '{C}' >/dev/null 2>&1",
    "echo '{C}' &>/dev/null",
    "echo '{C}' >&2",
    "echo '{C}' 1>&2",
    "echo '{C}' > /dev/stderr",
    "echo '{C}' > /dev/stdout",
    "echo 'a > b {C}'",
    "grep -n '>>' f '{C}'",
    'git commit -m "x > y {C}"',
    "cat <<'EOF'\na > b {C}\nEOF",
    "for f in a b; do echo $f; done && echo '{C}'",
    "true && echo '{C}'",
]

DENIED_NEVER_INERT = fill(
    WRITE_THEN_GIT_RUNS_IT
    + STARTUP_FILE_WRITES
    + RUNS_A_PROGRAM_OPTIONS
    + GIT_GLOBAL_OPTIONS
    + SUBSTITUTIONS
    + FILE_WRITES
    + EXPANDED_WORDS
    + NOT_LISTED
    + SEPARATOR_SHAPES
)
INERT_ALLOWED_FILLED = fill(INERT_ALLOWED)
ALL_DENIED = DENIED_NEVER_INERT
ALL_ALLOWED = INERT_ALLOWED_FILLED

# Every probe shape of the review times every change: the original denied nearly all of them, so
# the invariant (denied by the original means denied now, unless every command is inert) bites.
PROBE_CORPUS = fill(probe_shapes())
