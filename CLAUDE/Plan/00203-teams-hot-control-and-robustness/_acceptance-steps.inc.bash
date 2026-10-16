# shellcheck shell=bash
# _acceptance-steps.inc.bash — the legs and checks of Plan 00203's acceptance.bash. A separate
# file because legs are called through plan_deploy_leg and check_leg, which ShellCheck cannot
# see through (SC2329), and suppression is banned. Sourced, never executed: no shell options,
# no `exit`. A leg runs where errexit is off, so every command that can fail returns non-zero
# from its function explicitly. The variables are acceptance.bash's.

prop() {
    systemctl show -P "$1" "${HS}"
}

api_answers() {
    curl --fail --silent --show-error --noproxy '*' --max-time 3 -o /dev/null \
        "http://127.0.0.1:${PORT}/_matrix/client/versions"
}

note() {
    printf '%s\n' "$*" | tee -a "${REPORT}"
}

# health_status <expected exit> <file> — agent-bus health status for the team, as the desktop
# user, its output kept in <file>; refused unless it exits as expected.
health_status() {
    local want="$1" out="$2" status=0
    "${AGENT_BUS}" health status "${TEAM}" >"${out}" || status=$?
    cat -- "${out}" || return 1
    if [[ "${status}" -ne "${want}" ]]; then
        printf '[FAIL] agent-bus health status exited %d, not %d\n' "${status}" "${want}" >&2
        return 1
    fi
}

run_health_check() {
    sudo -n systemctl start "${HEALTH_SVC}"
}

# wait_back <old main pid> <seconds> — the homeserver active again under a new process and
# answering its client API, within the bound; says how long it took.
wait_back() {
    local old="$1" limit="$2" waited pid
    for ((waited = 0; waited <= limit; waited++)); do
        pid="$(prop MainPID)" || return 1
        if [[ "$(prop ActiveState)" == active && "${pid}" != 0 && "${pid}" != "${old}" ]] && api_answers; then
            note "back after ${waited} s (main PID ${old} -> ${pid}, NRestarts $(prop NRestarts))" || return 1
            return 0
        fi
        sleep 1 || return 1
    done
    printf '[FAIL] %s not back within %d s: %s (%s), main PID %s\n' "${HS}" "${limit}" \
        "$(prop ActiveState)" "$(prop SubState)" "$(prop MainPID)" >&2
    return 1
}

remove_delay() {
    if [[ -e "${DELAY_DROPIN}" ]]; then
        sudo -n rm -f -- "${DELAY_DROPIN}" || return 1
        sudo -n systemctl daemon-reload || return 1
    fi
}

teardown() {
    local enabled
    remove_delay || return 1
    enabled="$(systemctl show -P UnitFileState "${HS}")" || return 1
    if [[ "${enabled}" == enabled ]] || sudo -n test -e "/var/lib/agent-bus/${TEAM}"; then
        sudo -n "${INSTALLER}" remove --team "${TEAM}" --purge || return 1
    fi
}

teardown_on_exit() {
    teardown
}

install_team() {
    PORT="$(python3 -I -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')" || return 1
    printf '{"team": "%s", "port": %s, "listen": [], "allow_from": [], "humans": ["tester"], "org": "example", "forge_api": "https://api.github.com"}\n' \
        "${TEAM}" "${PORT}" >"${TEAM_FILE}" || return 1
    cat -- "${TEAM_FILE}" || return 1
    sudo -n "${INSTALLER}" team --team-file "${TEAM_FILE}"
}

check_notify_and_quiet_watchdog() {
    local type watchdog restart pid
    type="$(prop Type)" || return 1
    watchdog="$(prop WatchdogUSec)" || return 1
    restart="$(prop Restart)" || return 1
    note "Type=${type} WatchdogUSec=${watchdog} Restart=${restart}" || return 1
    [[ "${type}" == notify && "${watchdog}" == 30s && "${restart}" == always ]] || return 1
    pid="$(prop MainPID)" || return 1
    note "waiting 75 s (two and a half watchdog periods) for a restart that must not come" || return 1
    sleep 75 || return 1
    if [[ "$(prop MainPID)" != "${pid}" || "$(prop NRestarts)" != 0 || "$(prop ActiveState)" != active ]]; then
        printf '[FAIL] the homeserver restarted while healthy: main PID %s -> %s, NRestarts %s\n' \
            "${pid}" "$(prop MainPID)" "$(prop NRestarts)" >&2
        return 1
    fi
    api_answers
}

check_healthy_status() {
    run_health_check || return 1
    health_status 0 "${PLAN_RUN_DIR}/health-new.txt"
}

check_signal() {
    local signal="$1" limit="$2" pid before
    pid="$(prop MainPID)" || return 1
    before="$(prop NRestarts)" || return 1
    sudo -n systemctl kill --kill-whom=main "--signal=${signal}" "${HS}" || return 1
    wait_back "${pid}" "${limit}" || return 1
    [[ "$(prop NRestarts)" -gt "${before}" ]]
}

check_watchdog_hang() {
    local since journal
    since="$(date +%s)" || return 1
    check_signal SIGSTOP 150 || return 1
    journal="$(sudo -n journalctl -u "${HS}" --since "@${since}" --no-pager -o cat)" || return 1
    printf '%s\n' "${journal}" >"${PLAN_RUN_DIR}/watchdog-journal.txt" || return 1
    grep -i 'watchdog timeout' <<<"${journal}"
}

check_restarts_noted() {
    run_health_check || return 1
    health_status 1 "${PLAN_RUN_DIR}/health-restarts.txt" || return 1
    grep -F 'PROBLEM systemd restarted the homeserver' "${PLAN_RUN_DIR}/health-restarts.txt"
}

check_stale_backup() {
    local backups newest list rows listed=0
    sudo -n "${INSTALLER}" backup-now --team "${TEAM}" || return 1
    backups="$(sudo -n "${INSTALLER}" backups --team "${TEAM}")" || return 1
    newest="$(awk -F'\t' '$1 == "BACKUP" { id = $2 } END { print id }' <<<"${backups}")" || return 1
    [[ -n "${newest}" ]] || return 1
    sudo -n touch -d '3 days ago' "/var/lib/agent-bus-install/${TEAM}/backups/agent-bus-state-${newest}.tar" || return 1
    run_health_check || return 1
    health_status 1 "${PLAN_RUN_DIR}/health-stale.txt" || return 1
    grep -F 'PROBLEM the newest backup is 3 d' "${PLAN_RUN_DIR}/health-stale.txt" || return 1
    # --list exits 1 when any team's members cannot be read; only this team's row is judged.
    list="$(ccy-teams --list)" || listed=$?
    note "ccy-teams --list exited ${listed}" || return 1
    printf '%s\n' "${list}" >"${PLAN_RUN_DIR}/ccy-teams-list.txt" || return 1
    # Only the team's own row carries health words; the restarts R6 made come first in them.
    rows="$(grep -E "^${TEAM} " <<<"${list}" | grep -F 'health: ')" || return 1
    printf '%s\n' "${rows}" || return 1
    grep -F 'the newest backup is 3 d' <<<"${rows}"
}

check_seat_check_waits() {
    local start elapsed waited state err="${PLAN_RUN_DIR}/seat-check.stderr"
    sudo -n install -d -m 0755 -- "${DELAY_DIR}" || return 1
    printf '[Service]\nExecStartPre=/usr/bin/sleep %d\n' "${DELAY_S}" | sudo -n tee -- "${DELAY_DROPIN}" >/dev/null || return 1
    sudo -n systemctl daemon-reload || return 1
    sudo -n systemctl restart --no-block "${HS}" || return 1
    for ((waited = 0; waited < 10; waited++)); do
        state="$(prop ActiveState)" || return 1
        [[ "${state}" != activating ]] || break
        sleep 1 || return 1
    done
    if [[ "${state}" != activating ]]; then
        printf '[FAIL] %s did not start activating: %s\n' "${HS}" "${state}" >&2
        return 1
    fi
    start="$(date +%s)" || return 1
    "${AGENT_BUS}" seat check "1@${TEAM}" 2>"${err}" || return 1
    elapsed="$(($(date +%s) - start))"
    cat -- "${err}" || return 1
    note "seat check returned after ${elapsed} s" || return 1
    remove_delay || return 1
    [[ "${elapsed}" -ge $((DELAY_S - 10)) ]] || return 1
    grep -F 'is starting' "${err}" || return 1
    grep -F 'is up after' "${err}"
}

# check_lxc_members — each running container with systemd that holds an agent-bus-claude unit:
# the unit enabled, active and Restart=always, and the container started at boot.
check_lxc_members() {
    local containers container unit units state autostart found=0
    local -a names
    containers="$(sudo -n lxc-ls --running -1)" || return 1
    for container in ${containers}; do
        if ! sudo -n lxc-attach -n "${container}" --clear-env -- test -d /run/systemd/system; then
            note "container ${container}: no systemd, so no member unit" || return 1
            continue
        fi
        units="$(sudo -n lxc-attach -n "${container}" --clear-env -- systemctl list-units --all --plain \
            --no-legend 'agent-bus-claude-*.service')" || return 1
        mapfile -t names < <(awk 'NF { print $1 }' <<<"${units}")
        for unit in "${names[@]+"${names[@]}"}"; do
            found=1
            state="$(sudo -n lxc-attach -n "${container}" --clear-env -- systemctl show \
                --property=ActiveState --property=UnitFileState --property=Restart "${unit}")" || return 1
            state="$(sort <<<"${state}" | tr '\n' ' ')" || return 1
            autostart="$(sudo -n lxc-info -n "${container}" -c lxc.start.auto)" || return 1
            note "container ${container}: ${unit}: ${state}; ${autostart}" || return 1
            [[ "${state}" == "ActiveState=active Restart=always UnitFileState=enabled " ]] || return 1
            [[ "${autostart}" == *"= 1" ]] || return 1
        done
    done
    if [[ "${found}" -eq 0 ]]; then
        note "R9: no agent_bus_lxc_members entry declares service: true on this host, so there is no member unit to prove" || return 1
    fi
}
