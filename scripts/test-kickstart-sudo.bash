#!/usr/bin/env bash
# The kickstart's passwordless sudo lasts for first boot only, and the play's grant follows the
# host's declared choice, set last in a main run (Q15).
#
#   1. The first-boot wrapper (the AUTOSTART_SCRIPT heredoc of fedora-install/ks.cfg, read from
#      the file, never copied here) removes the kickstart's rule after run.bash whether it passed
#      or failed, after a failed clone too, and does nothing once setup is complete; a removal
#      that fails says so on stderr, naming the file. The path is read from the %post line that
#      writes the rule.
#   2. The localhost.yml the kickstart writes declares passwordless_sudo_for_unattended_deploys,
#      false.
#   3. Only the final play touches the grant or the kickstart's rule, so a play run on its own
#      (no final play) leaves the grant as it was and never half-revokes it; and the final play
#      is the last one playbook-main.yml imports, so no become task follows a revocation.
#   4. The final play's expressions, rendered by the real ansible over provisioning_profile x
#      install_audience x the switch (true, false, undeclared): a developer desktop must declare
#      the switch (the assertion fails otherwise); the grant is present exactly for a developer
#      desktop whose switch is true; the kickstart's rule is removed on every host, as nothing in
#      the run follows it (a reboot at the end of first boot can kill the wrapper before its own
#      removal runs). play-basic-configs.yml asserts the same declaration, early.
#
# Nothing here contacts a host or changes one: ansible renders against localhost with a
# throwaway config, and every write lands in a temp directory. `set -e` is not used: every case
# runs so the summary is complete.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KS="${KS_CFG:-$REPO_ROOT/fedora-install/ks.cfg}"
PLAYS="$REPO_ROOT/playbooks"
FINAL="$PLAYS/imports/play-ZZZ-passwordless-sudo.yml"
BASIC="$PLAYS/imports/play-basic-configs.yml"
ASSERT_TASKS="$REPO_ROOT/tasks/assert-passwordless-sudo-declared.yml"
SWITCH=passwordless_sudo_for_unattended_deploys
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

removal="sudo rm -f $RULE"
first_boot passed checkout
check "run.bash passed: the rule is removed after it" "$(paste -sd'|' "$LOG")" "run.bash|$removal"
FAKE_RUN_RC=1 first_boot failed checkout
check "run.bash failed: the rule is removed after it" "$(paste -sd'|' "$LOG")" "run.bash|$removal"
FAKE_CLONE_RC=128 first_boot noclone none
check "the clone failed: the rule is removed" "$(paste -sd'|' "$LOG")" "git clone|$removal"
first_boot cloned none
check "cloned on first login: the rule is removed after run.bash" "$(paste -sd'|' "$LOG")" "git clone|run.bash|$removal"
first_boot complete complete
check "setup already complete: nothing runs" "$(paste -sd'|' "$LOG")" ""
FAKE_SUDO_RC=1 first_boot nosudo checkout
check "a failed removal is reported" "$([[ "$ERR" == *ERROR*"$RULE"*"still grants passwordless sudo"* ]] && echo named)" named

# --- 2. the localhost.yml the kickstart writes -----------------------------------------------
heredoc LOCALHOST_SUDO "$KS" > "$work/localhost-sudo.yml"
check "the kickstart's localhost.yml declares the switch false" \
    "$(python3 -I -c 'import sys, yaml; print(yaml.safe_load(open(sys.argv[1])).get(sys.argv[2]))' "$work/localhost-sudo.yml" "$SWITCH")" False

# --- 3. only the final play touches the grant ------------------------------------------------
touching=$(cd "$REPO_ROOT" && grep -rlE 'passwordless sudo for|99-fedora-desktop-setup' playbooks tasks | sort | paste -sd' ')
check "only the final play touches the grant or the kickstart's rule" "$touching" "playbooks/imports/play-ZZZ-passwordless-sudo.yml"
check "the final play is the last one playbook-main.yml imports" \
    "$(awk '/import_playbook/ { last = $NF } END { print last }' "$PLAYS/playbook-main.yml")" "imports/play-ZZZ-passwordless-sudo.yml"
for play in "$BASIC" "$FINAL"; do
    check "$(basename "$play") asserts the declaration" \
        "$(grep -c 'import_tasks: .*/tasks/assert-passwordless-sudo-declared.yml' "$play")" 1
done

# --- 4. the final play's expressions, rendered by ansible ------------------------------------
printf '[defaults]\nretry_files_enabled = False\nstdout_callback = ansible.builtin.default\n' > "$work/ansible.cfg"
printf 'all:\n  hosts:\n    localhost:\n      ansible_connection: local\n      ansible_python_interpreter: "{{ ansible_playbook_python }}"\n' > "$work/inventory.yml"
mkdir -p "$work/tmp"
run_ansible() {
    env -i PATH="$PATH" HOME="$HOME" LANG=C.UTF-8 ANSIBLE_CONFIG="$work/ansible.cfg" \
        ANSIBLE_LOCAL_TEMP="$work/tmp" ANSIBLE_REMOTE_TEMP="$work/tmp" "$@" </dev/null 2>&1
}

# render PROFILE AUDIENCE SWITCH prints "assert=<ok|failed> grant=<state> rule=<removed|kept>",
# the final play's own vars and conditions evaluated by ansible.
python3 -I - "$FINAL" "$ASSERT_TASKS" "$work/render.yml" <<'EOF'
import sys, yaml
final, asserts, out = sys.argv[1:4]
play = yaml.safe_load(open(final))[0]
tasks = {t["name"]: t for t in play["tasks"] if "name" in t}
grant = next(t for t in play["tasks"] if "ansible.builtin.blockinfile" in t)
rule = next(t for t in play["tasks"] if "ansible.builtin.file" in t)
check = yaml.safe_load(open(asserts))[0]
render = [{
    "hosts": "localhost", "gather_facts": False, "vars": play["vars"],
    "tasks": [
        {"name": "assertion", "ansible.builtin.set_fact": {"held": "{{ " + " and ".join(
            "(" + c + ")" for c in check["ansible.builtin.assert"]["that"]) + " }}"},
         "when": check.get("when", True)},
        {"name": "print", "ansible.builtin.debug": {"msg": "RESULT assert={{ 'failed' if (held is defined and not held | bool) else 'ok' }} grant="
            + grant["ansible.builtin.blockinfile"]["state"] + " rule={{ 'removed' if (" + str(rule.get("when", True)) + ") else 'kept' }}"}},
    ],
}]
yaml.safe_dump(render, open(out, "w"))
EOF
render() {
    local extra=(-e "provisioning_profile=$1" -e "install_audience=$2" -e user_login=yourname)
    [[ "$3" == undeclared ]] || extra+=(-e "{\"$SWITCH\": $3}")
    run_ansible ansible-playbook -i "$work/inventory.yml" "$work/render.yml" "${extra[@]}" \
        | grep -m 1 -o 'RESULT [^"]*' | cut -d' ' -f2-
}

for profile in desktop server; do
    for audience in developer consumer; do
        for switch in true false undeclared; do
            developer_desktop=$([[ $profile == desktop && $audience == developer ]] && echo y)
            want_assert=ok
            [[ -n "$developer_desktop" && $switch == undeclared ]] && want_assert=failed
            want_grant=absent want_rule=removed
            if [[ -n "$developer_desktop" && $switch == true ]]; then want_grant=present; fi
            # an undeclared developer desktop never reaches the grant: only the assertion counts.
            got=$(render "$profile" "$audience" "$switch")
            if [[ $want_assert == failed ]]; then
                check "$profile/$audience/$switch: the declaration is required" "${got%% *}" "assert=failed"
            else
                check "$profile/$audience/$switch" "$got" "assert=ok grant=$want_grant rule=$want_rule"
            fi
        done
    done
done

printf '\npassed: %d   failed: %d\n' "$PASSED" "$FAILED"
[[ "$FAILED" == 0 ]]
