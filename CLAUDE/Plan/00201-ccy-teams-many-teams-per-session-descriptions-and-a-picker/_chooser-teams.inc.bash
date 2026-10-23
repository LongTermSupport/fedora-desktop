# shellcheck shell=bash
# _chooser-teams.inc.bash — the legs of chooser-teams-up.bash and chooser-teams-down.bash, the
# seat-chooser teams of Plan 00201. A separate file for the reason _acceptance-m7.inc.bash is
# one: legs are called through plan_deploy_leg, which ShellCheck cannot see through (SC2329), and
# suppression is banned. Sourced after _acceptance-m7.inc.bash, never executed: no shell options,
# no `exit`. Every function returns non-zero on its first failure, explicitly.
#
# Reads the scripts' CURRENT (the file naming the up run's directory) and the M7 globals.

# up_not_already — one set of these teams at a time: a second would purge the first's.
up_not_already() {
    if [[ -e "${CURRENT}" ]]; then
        printf '[FAIL] the seat-chooser teams are already up (%s names %s): run chooser-teams-down.bash first\n' \
            "${CURRENT}" "$(cat -- "${CURRENT}")" >&2
        return 1
    fi
}

# up_record — the run directory, for the teardown: the team files, the firewalld rules recorded,
# the host_vars and the saved Quick Launch record are all in it.
up_record() {
    printf '%s\n' "${PLAN_RUN_DIR}" >"${CURRENT}" || return 1
    printf '==> %s names this run directory for chooser-teams-down.bash\n' "${CURRENT}"
}

# down_prerequisites — what the teardown needs, and no more: a missing hooks daemon or Quick
# Launch record (the launches' needs) must not leave the teams up.
down_prerequisites() {
    local tool
    m7_session_paths || return 1
    for tool in "${AGENT_BUS}" "${INSTALLER}"; do
        if [[ ! -x "${tool}" ]]; then
            printf '[FAIL] %s is not installed: run play-agent-bus.yml\n' "${tool}" >&2
            return 1
        fi
    done
    for tool in podman firewall-cmd jq; do
        if ! command -v "${tool}" >/dev/null; then
            printf '[FAIL] %s is not on PATH\n' "${tool}" >&2
            return 1
        fi
    done
    m7_owner_declares_neither
}

# down_adopt — the up run's records, copied into this run's directory so the M7 legs read them:
# each team's file and recorded firewalld rules, the host_vars, and the saved Quick Launch record.
down_adopt() {
    local up name team
    if [[ ! -f "${CURRENT}" ]]; then
        printf '[FAIL] %s is missing: no seat-chooser teams were left up by chooser-teams-up.bash\n' "${CURRENT}" >&2
        return 1
    fi
    up="$(cat -- "${CURRENT}")" || return 1
    if [[ ! -d "${up}" ]]; then
        printf '[FAIL] %s names %s, which is not a directory\n' "${CURRENT}" "${up}" >&2
        return 1
    fi
    for team in "${M7_TEAMS[@]}"; do
        for name in "${team}.team.json" "${team}.firewalld.rules"; do
            cp -p -- "${up}/${name}" "${PLAN_RUN_DIR}/${name}" || return 1
        done
    done
    cp -p -- "${up}/host_vars.yml" "${HOST_VARS_FILE}" || return 1
    cp -p -- "${up}/last-launch.conf.before" "${M7_LAST_LAUNCH_SAVED}" || return 1
    m7_adopted
    DOWN_FROM="${up}"
    printf '==> the records of %s adopted\n' "${up}"
}

# down_sessions — every session the chooser launches left, ended; the seats parked, then deleted.
down_sessions() {
    m7_remove_containers || return 1
    m7_sudo_alive || return 1
    m7_park_seats || return 1
    m7_delete_seat_dirs
}

# down_forget — the teams are gone and checked: the record that they were up goes too.
down_forget() {
    rm -f -- "${CURRENT}" || return 1
    DOWN_DONE=1
    printf '==> %s removed\n' "${CURRENT}"
}

# down_after_stop — on the way out of a teardown that stopped early (plan_on_cleanup): the rest
# of it tried, then the check that the teams' firewalld rules are gone. The record of the up run
# stays, so running chooser-teams-down.bash again finishes the job.
down_after_stop() {
    local failed=0
    if [[ "${DOWN_DONE}" -eq 1 || -z "${DOWN_FROM}" ]]; then
        return 0
    fi
    m7_teardown_after_stop || failed=1
    if ! m7_check_gone; then
        printf '[FAIL] the seat-chooser teams are not wholly gone (above): run chooser-teams-down.bash again\n' >&2
        failed=1
    fi
    return "${failed}"
}
