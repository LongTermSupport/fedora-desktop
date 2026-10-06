# shellcheck shell=bash
# _deploy-steps.inc.bash — the leg logic of Plan 00161's deploy.bash (unit U16).
#
# A separate file because deploy.bash calls these only through `plan_deploy_leg "<name>"
# <function> …`, and ShellCheck cannot see through that indirection (SC2329); suppression
# is banned, so the leg logic lives in a sourced helper, as Plan 00073's acceptance cases
# do. Sourced, never executed: no shell options, no `exit`. Every function returns non-zero
# on the first failure, explicitly, since a leg runs in an `if` where errexit is off.
#
# Reads deploy.bash's globals: INSTALLER, BUS_ADDRESS, TEAM, TEAM_FILE, and the plan
# library's PLAN_RUN_DIR and PLAN_REPO_ROOT. Sets TEAM_PRESENT.

# run_installer <label> <args...> — agent-bus-install as root. Its stdout (the CHANGED and
# CHECK marker lines) is kept in <label>.out in the run directory and shown; its stderr goes
# straight to the log. Fails when the installer does.
run_installer() {
    local label="$1" status=0 output
    shift
    output="$(sudo -n "${INSTALLER}" "$@")" || status=$?
    printf '%s\n' "${output}" >"${PLAN_RUN_DIR}/${label}.out" || return 1
    if [[ -n "${output}" ]]; then
        printf '%s\n' "${output}"
    fi
    if [[ "${status}" -ne 0 ]]; then
        printf '[FAIL] agent-bus-install %s exited %d (sudo saying a password is required means its timestamp lapsed: run again)\n' \
            "$1" "${status}" >&2
        return 1
    fi
}

# unchanged <label> — fail when the run kept in <label>.out printed any CHANGED line.
unchanged() {
    local changes
    changes="$(awk -F'\t' '$1 == "CHANGED"' "${PLAN_RUN_DIR}/$1.out")" || return 1
    if [[ -n "${changes}" ]]; then
        printf '[FAIL] the second run changed something, so the first did not converge:\n%s\n' "${changes}" >&2
        return 1
    fi
    printf '==> nothing changed\n'
}

install_software() {
    run_installer "$1" software --source "${PLAN_REPO_ROOT}" --bus-address "${BUS_ADDRESS}"
}

# The throwaway team: example values only (CLAUDE/ExampleValues.md), the bus address, and a
# port free on loopback now.
write_team_file() {
    local port
    port="$(python3 -I -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')" || return 1
    cat >"${TEAM_FILE}" <<EOF || return 1
{"team": "${TEAM}", "port": ${port},
 "listen": ["${BUS_ADDRESS}"], "allow_from": ["192.0.2.0/24"], "humans": ["owner"],
 "repos": [{"repo": "example/project", "branches": ["main"]}],
 "path_prefixes": ["CLAUDE/Plan/"], "forge_api": "https://api.example.com"}
EOF
    printf '==> team file %s:\n' "${TEAM_FILE}"
    cat -- "${TEAM_FILE}"
}

install_team() {
    TEAM_PRESENT=1 # before the call: a half-installed team still needs removing
    run_installer "$1" team --team-file "${TEAM_FILE}"
}

second_run() {
    install_software software-again || return 1
    unchanged software-again || return 1
    install_team team-again || return 1
    unchanged team-again
}

check_team() {
    local status=0 failed
    run_installer check check --team "${TEAM}" || status=$?
    failed="$(awk -F'\t' '$1 == "CHECK" && $3 == "FAIL"' "${PLAN_RUN_DIR}/check.out")" || return 1
    if [[ -n "${failed}" ]]; then
        printf '[FAIL] check reported:\n%s\n' "${failed}" >&2
        return 1
    fi
    if [[ "${status}" -ne 0 ]]; then
        return 1
    fi
    if ! grep -q '^CHECK' "${PLAN_RUN_DIR}/check.out"; then
        printf '[FAIL] check printed no CHECK line\n' >&2
        return 1
    fi
}

remove_team() {
    run_installer "$1" remove --team "${TEAM}" --purge || return 1
    TEAM_PRESENT=0
}

# On the way out of a failed run (plan_on_cleanup): the throwaway team must not outlive it.
remove_team_after_failure() {
    if [[ "${TEAM_PRESENT}" -ne 1 ]]; then
        return 0
    fi
    printf '==> the run stopped with %s installed: removing it\n' "${TEAM}"
    if ! remove_team remove-after-failure; then
        printf '[FAIL] %s could not be removed; run deploy.bash again, which removes a leftover team first\n' "${TEAM}" >&2
        return 1
    fi
}
