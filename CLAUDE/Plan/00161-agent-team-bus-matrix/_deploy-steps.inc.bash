# shellcheck shell=bash
# _deploy-steps.inc.bash — the leg logic of Plan 00161's deploy.bash (unit U16).
#
# A separate file because deploy.bash calls these only through `plan_deploy_leg "<name>"
# <function> …`, and ShellCheck cannot see through that indirection (SC2329); suppression
# is banned, so the leg logic lives in a sourced helper, as Plan 00073's acceptance cases
# do. Sourced, never executed: no shell options, no `exit`. Every function returns non-zero
# on the first failure, explicitly, since a leg runs in an `if` where errexit is off.
#
# Reads deploy.bash's globals: INSTALLER, BUS_ADDRESS, TEAM, TEAM_FILE, PLAY_VARS_PRESENT,
# PLAY_VARS_ABSENT, H2_DOCKER_IMAGE, and the plan library's PLAN_RUN_DIR, PLAN_REPO_ROOT and PLAN_SCRIPT_DIR.
# Sets TEAM_PRESENT.

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

# free_loopback_port — a TCP port free on loopback now, for a test team's homeserver.
free_loopback_port() {
    python3 -I -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

# The throwaway team: example values only (CLAUDE/ExampleValues.md), the bus address, and a
# port free on loopback now. The team file's shape is acceptance_check.py's team-file, the
# one the acceptance team is built from too.
write_team_file() {
    local port
    port="$(free_loopback_port)" || return 1
    python3 -I "${PLAN_SCRIPT_DIR}/acceptance_check.py" team-file "${TEAM}" "${port}" "${BUS_ADDRESS}" \
        owner example/project main CLAUDE/Plan/ https://api.example.com >"${TEAM_FILE}" || return 1
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

# ── U22: play-agent-bus.yml with the throwaway team ──────────────────────────────────────

# write_play_vars — two extra-vars files for the play from the team file: the team present,
# and the team absent with purge. Extra vars override the host_vars agent_bus_teams, so a
# run declares only the throwaway team; the play removes only teams declared absent, so an
# owner-declared team is left alone.
write_play_vars() {
    python3 -I -c '
import json, sys
team_file, bus_address, present_out, absent_out = sys.argv[1:5]
with open(team_file, encoding="utf-8") as handle:
    team = json.load(handle)
for path, teams in ((present_out, [team]), (absent_out, [{"team": team["team"], "state": "absent", "purge": True}])):
    with open(path, "w", encoding="utf-8") as handle:
        json.dump({"agent_bus_address": bus_address, "agent_bus_teams": teams}, handle, indent=1)
' "${TEAM_FILE}" "${BUS_ADDRESS}" "${PLAY_VARS_PRESENT}" "${PLAY_VARS_ABSENT}" || return 1
    printf '==> play vars %s and %s written\n' "${PLAY_VARS_PRESENT}" "${PLAY_VARS_ABSENT}"
}

# run_play <label> <vars-file> <expect> — the play with <vars-file>; its output is kept in
# <label>.out in the run directory. <expect> is "changed" (the recap must count at least
# one changed task) or "unchanged" (it must count none), which checks that the play
# reports changed exactly when the installer prints CHANGED lines.
run_play() {
    local label="$1" varsFile="$2" expect="$3" changed
    plan_ansible_playbook playbooks/imports/play-agent-bus.yml -e "@${varsFile}" \
        | tee "${PLAN_RUN_DIR}/${label}.out"
    if [[ "${PIPESTATUS[0]}" -ne 0 ]]; then
        printf '[FAIL] play-agent-bus.yml (%s) failed\n' "${label}" >&2
        return 1
    fi
    changed="$(awk 'match($0, /changed=[0-9]+/) { print substr($0, RSTART + 8, RLENGTH - 8) }' \
        "${PLAN_RUN_DIR}/${label}.out")" || return 1
    if [[ ! "${changed}" =~ ^[0-9]+$ ]]; then
        printf '[FAIL] no single changed= count in the play recap of %s: %s\n' "${label}" "${changed}" >&2
        return 1
    fi
    if [[ "${expect}" == "unchanged" && "${changed}" -ne 0 ]]; then
        printf '[FAIL] the play changed %s task(s) on a run that should change nothing\n' "${changed}" >&2
        return 1
    fi
    if [[ "${expect}" == "changed" && "${changed}" -eq 0 ]]; then
        printf '[FAIL] the play reported no change, but the installer had work to do\n' >&2
        return 1
    fi
    printf '==> play recap changed=%s, as expected (%s)\n' "${changed}" "${expect}"
}

play_team_present() {
    TEAM_PRESENT=1 # before the call: a half-installed team still needs removing
    run_play play-present "${PLAY_VARS_PRESENT}" changed
}

play_team_present_again() {
    run_play play-present-again "${PLAY_VARS_PRESENT}" unchanged
}

play_team_absent() {
    run_play play-absent "${PLAY_VARS_ABSENT}" changed || return 1
    TEAM_PRESENT=0
}

# pull_h2_docker_image — the image triage H2's docker leg runs in, which the triage never
# pulls itself. With no docker there is nothing to pull, and H2 records docker as not installed.
pull_h2_docker_image() {
    if ! command -v docker >/dev/null; then
        printf '==> docker is not installed: nothing to pull, H2 records that\n'
        return 0
    fi
    docker pull --quiet "${H2_DOCKER_IMAGE}" || return 1
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
