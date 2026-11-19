#!/usr/bin/env bash
# The kickstart's passwordless sudo lasts for first boot only, and the play's grant follows the
# host's declared choice, set last in a main run (Q15).
#
#   1. The first-boot wrapper (the AUTOSTART_SCRIPT heredoc of fedora-install/ks.cfg, read from
#      the file, never copied here) removes the kickstart's rule with `sudo -n` (never a prompt)
#      after run.bash whether it passed or failed, after a failed clone too, and does nothing once
#      setup is complete; a refused removal says why on stderr, naming the file. The path is read
#      from the %post line that writes the rule.
#   2. Only the final play touches the grant or the kickstart's rule, so a play run on its own
#      (no final play) leaves the grant as it was and never half-revokes it; and the final play
#      is the last one playbook-main.yml imports, so no later play follows a revocation.
#   3. The final play's conditions, rendered by the real ansible over provisioning_profile x
#      install_audience x the switch (true, false, undeclared) x whether the /etc/sudoers block
#      and the kickstart's rule exist when it starts. Its tasks are then walked in the play's
#      order, as a run without the sudo password meets them: every privileged task must find a
#      grant still in place (none left stranded by an earlier revocation), and the play must end
#      with the block present exactly for a developer desktop whose switch is true (undeclared is
#      false) and the kickstart's rule gone on every host.
#
# Nothing here contacts a host or changes one: ansible renders against localhost with a
# throwaway config, and every write lands in a temp directory. `set -e` is not used: every case
# runs so the summary is complete.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KS="${KS_CFG:-$REPO_ROOT/fedora-install/ks.cfg}"
PLAYS="$REPO_ROOT/playbooks"
FINAL="${FINAL_PLAY:-$PLAYS/imports/play-ZZZ-passwordless-sudo.yml}"
PASSED=0
FAILED=0

for tool in ansible-playbook python3; do
    if ! command -v "$tool" >/dev/null; then
        echo "ERROR: $tool not found: this suite renders the real play with it" >&2
        exit 2
    fi
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

check() {
    if [[ "$2" == "$3" ]]; then
        printf 'PASS: %s\n' "$1"
        PASSED=$((PASSED + 1))
    else
        printf 'FAIL: %s\n  expected: %s\n  got:      %s\n' "$1" "$3" "$2"
        FAILED=$((FAILED + 1))
    fi
}

# heredoc NAME FILE prints the body of the quoted heredoc NAME in FILE.
heredoc() {
    awk -v tag="$1" '$0 == tag { on = 0 } on { print } index($0, "<< '\''" tag "'\''") { on = 1 }' "$2"
}

# --- 1. the first-boot wrapper -------------------------------------------------------------
RULE=$(awk '/NOPASSWD: ALL" > \/etc\/sudoers.d\// { sub(/.*> /, ""); print; exit }' "$KS")
check "ks.cfg names the kickstart's rule file" "${RULE:+found}" found
heredoc AUTOSTART_SCRIPT "$KS" > "$work/autostart.bash"
check "ks.cfg carries the autostart wrapper" "$(grep -c '^launch_terminal() {' "$work/autostart.bash")" 1

# fakes: the terminal runs its command with no stdin (so the closing `exec bash` ends), sudo and
# run.bash log their argv, git clone makes a checkout or fails.
mkdir -p "$work/bin"
cat > "$work/bin/ptyxis" <<'EOF'
#!/usr/bin/env bash
exec bash -c "${@: -1}" </dev/null
EOF
cat > "$work/bin/sudo" <<'EOF'
#!/usr/bin/env bash
echo "sudo $*" >> "$FAKE_LOG"
exit "${FAKE_SUDO_RC:-0}"
EOF
cat > "$work/bin/git" <<'EOF'
#!/usr/bin/env bash
echo "git $1" >> "$FAKE_LOG"
[[ "${FAKE_CLONE_RC:-0}" == 0 ]] || exit "$FAKE_CLONE_RC"
mkdir -p "${@: -1}"
printf '#!/usr/bin/env bash\necho run.bash >> "$FAKE_LOG"\nexit "${FAKE_RUN_RC:-0}"\n' > "${@: -1}/run.bash"
chmod +x "${@: -1}/run.bash"
EOF
chmod +x "$work/bin/"*

# first_boot CASE [checkout|none|complete] runs the wrapper in a fresh HOME; $LOG holds the
# calls in order and $ERR the wrapper's stderr.
first_boot() {
    local home="$work/home-$1"
    mkdir -p "$home"
    case "$2" in
        checkout) FAKE_LOG=/dev/null PATH="$work/bin:$PATH" "$work/bin/git" clone "$home/Projects/fedora-desktop" ;;
        complete) mkdir -p "$home/.local/state" && touch "$home/.local/state/fedora-desktop-setup-complete" ;;
    esac
    LOG="$work/$1.log"
    : > "$LOG"
    HOME="$home" FAKE_LOG="$LOG" PATH="$work/bin:/usr/bin:/bin" bash "$work/autostart.bash" 2> "$work/$1.err" > /dev/null
    ERR=$(cat "$work/$1.err")
}

removal="sudo -n rm -f $RULE"
first_boot passed checkout
check "run.bash passed: the rule is removed after it, never by a prompt" "$(paste -sd'|' "$LOG")" "run.bash|$removal"
FAKE_RUN_RC=1 first_boot failed checkout
check "run.bash failed: the rule is removed after it" "$(paste -sd'|' "$LOG")" "run.bash|$removal"
FAKE_CLONE_RC=128 first_boot noclone none
check "the clone failed: the rule is removed" "$(paste -sd'|' "$LOG")" "git clone|$removal"
first_boot cloned none
check "cloned on first login: the rule is removed after run.bash" "$(paste -sd'|' "$LOG")" "git clone|run.bash|$removal"
first_boot complete complete
check "setup already complete: nothing runs" "$(paste -sd'|' "$LOG")" ""
FAKE_SUDO_RC=1 first_boot nosudo checkout
check "a refused removal says no passwordless rule is in effect, naming the file" \
    "$([[ "$ERR" == *"no passwordless rule is in effect"*"$RULE"* ]] && echo named)" named

# --- 2. only the final play touches the grant ------------------------------------------------
touching=$(cd "$REPO_ROOT" && grep -rlE 'passwordless sudo for|99-fedora-desktop-setup' playbooks tasks | sort | paste -sd' ')
check "only the final play touches the grant or the kickstart's rule" "$touching" "playbooks/imports/play-ZZZ-passwordless-sudo.yml"
check "the final play is the last one playbook-main.yml imports" \
    "$(awk '/import_playbook/ { last = $NF } END { print last }' "$PLAYS/playbook-main.yml")" "imports/play-ZZZ-passwordless-sudo.yml"

# --- 3. the final play, rendered by ansible and walked in order ------------------------------
printf '[defaults]\nretry_files_enabled = False\nstdout_callback = ansible.builtin.default\n' > "$work/ansible.cfg"
printf 'all:\n  hosts:\n    localhost:\n      ansible_connection: local\n      ansible_python_interpreter: "{{ ansible_playbook_python }}"\n' > "$work/inventory.yml"
mkdir -p "$work/tmp"

# One play per case, each the final play's own vars with the case's facts, printing
# "RESULT <case> <task>,<task>,..." where a task is slurp|stat (a probe), block:<state> or
# rule:absent when it runs, and skip when it does not. The probes' results are the case's facts.
python3 -I - "$FINAL" "$work/render.yml" <<'EOF'
import base64, itertools, json, sys, yaml
final, out = sys.argv[1:3]
play = yaml.safe_load(open(final))[0]

def runs(task):
    when = task.get("when", True)
    return "{{ (" + (when if isinstance(when, str) else str(when)) + ") | bool }}"

steps = []
for t in play["tasks"]:
    if "ansible.builtin.slurp" in t or "ansible.builtin.stat" in t:
        steps.append("probe")
    elif "ansible.builtin.blockinfile" in t:
        steps.append("{{ 'block:' ~ (" + t["ansible.builtin.blockinfile"]["state"].strip("{} ") + ") if " + runs(t)[3:-3] + " else 'skip' }}")
    elif "ansible.builtin.file" in t:
        steps.append("{{ 'rule:' ~ '" + t["ansible.builtin.file"]["state"] + "' if " + runs(t)[3:-3] + " else 'skip' }}")
    else:
        steps.append("unknown:" + next(k for k in t if k != "name"))

plays = []
for profile, audience, switch, block, rule in itertools.product(
        ("desktop", "server"), ("developer", "consumer"), ("true", "false", "undeclared"), (0, 1), (0, 1)):
    case = f"{profile}/{audience}/{switch}/block={block}/rule={rule}"
    sudoers = "root ALL=(ALL) ALL\n" + ("## BEGIN passwordless sudo for yourname\n" if block else "")
    facts = dict(play["vars"], provisioning_profile=profile, install_audience=audience, user_login="yourname",
                 sudoers_file={"content": base64.b64encode(sudoers.encode()).decode()},
                 first_boot_rule_stat={"stat": {"exists": bool(rule)}})
    if switch != "undeclared":
        facts["passwordless_sudo_for_unattended_deploys"] = switch == "true"
    plays.append({"name": case, "hosts": "localhost", "gather_facts": False, "vars": facts,
                  "tasks": [{"ansible.builtin.debug": {"msg": "RESULT " + case + " " + ",".join(steps)}}]})
yaml.safe_dump(plays, open(out, "w"))
EOF

env -i PATH="$PATH" HOME="$HOME" LANG=C.UTF-8 ANSIBLE_CONFIG="$work/ansible.cfg" \
    ANSIBLE_LOCAL_TEMP="$work/tmp" ANSIBLE_REMOTE_TEMP="$work/tmp" \
    ansible-playbook -i "$work/inventory.yml" "$work/render.yml" </dev/null > "$work/render.out" 2>&1
grep -o 'RESULT [^"]*' "$work/render.out" > "$work/results"
check "ansible rendered every case" "$(wc -l < "$work/results")" 48

# walk CASE STEPS: replay the tasks against the case's grants. A run without the sudo password
# has only the block and the kickstart's rule; with neither at the start, run.bash asked for the
# password and nothing can strand. Prints "<block>,<rule>" at the end, or "stranded at task N".
walk() {
    local block rule password=no i=0 step
    block=$(case_field "$1" block)
    rule=$(case_field "$1" rule)
    [[ $block == 1 || $rule == 1 ]] || password=yes
    IFS=, read -ra steps <<< "$2"
    for step in "${steps[@]}"; do
        i=$((i + 1))
        [[ $step == skip ]] && continue
        if [[ $password == no && $block == 0 && $rule == 0 ]]; then
            echo "stranded at task $i ($step)"
            return
        fi
        case "$step" in
            block:present) block=1 ;;
            block:absent) block=0 ;;
            rule:absent) rule=0 ;;
            probe) ;;
            *) echo "unknown task $step"; return ;;
        esac
    done
    echo "block=$block,rule=$rule"
}

# case_field CASE NAME prints the 0|1 after NAME= in CASE.
case_field() {
    local rest="${1#*"$2"=}"
    echo "${rest%%/*}"
}

while read -r _ case steps; do
    IFS=/ read -r profile audience switch _ <<< "$case"
    want_block=0
    [[ $profile == desktop && $audience == developer && $switch == true ]] && want_block=1
    check "$case" "$(walk "$case" "$steps")" "block=$want_block,rule=0"
done < "$work/results"

printf '\npassed: %d   failed: %d\n' "$PASSED" "$FAILED"
[[ "$FAILED" == 0 ]]
