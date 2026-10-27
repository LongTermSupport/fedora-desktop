# shellcheck shell=bash
# _triage-hs.inc.bash — the read-only probes behind triage-hs.bash (Plan 00203), sourced by it
# after plan_start_log. They live in an include so the orchestrator can hand them to
# plan_gather_leg by name (PlanScriptStandards.md, "Legs are commands"). Every root read is
# `sudo -n`; nothing here changes the host.
#
# Expects from the orchestrator: TEAMS (array), STATE_ROOT, HEALTH_ROOT, TUWUNEL, RUNS_ROOT,
# FACTS, STOPS, SUMMARY, EXPORTS, PLAN_SCRIPT_DIR.

# Non-zero exit status is data, not failure (PlanTriage.md probe pattern); into the facts file.
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}" >>"${FACTS}"
    return 0
}

heading() {
    printf '\n================\n### READ THIS FOR: %s\n================\n\n' "$1" >>"${FACTS}"
}

# Every team with a homeserver unit systemd knows of: loaded (running or not) or enabled.
installed_teams() {
    local units files
    units="$(systemctl list-units --all --plain --no-legend --full "${HS_GLOB:?}")"
    files="$(systemctl list-unit-files --plain --no-legend --full "${HS_GLOB}")"
    # The team is what the glob's `*` stands for: between its head and its tail, with no dot.
    printf '%s\n%s\n' "${units}" "${files}" | awk '{print $1}' \
        | awk -v head="${HS_GLOB%%\**}" -v tail="${HS_GLOB#*\*}" '
            index($0, head) == 1 && length($0) > length(head) + length(tail) \
                && substr($0, length($0) - length(tail) + 1) == tail {
                team = substr($0, length(head) + 1, length($0) - length(head) - length(tail))
                if (team !~ /\./) print team
            }' | sort -u
}

# jexport <file> <journalctl args...> — a JSON export of the journal as root. journalctl exits
# non-zero with nothing on stderr when a --grep matches nothing; that is reported as "no
# entries", and any other non-zero exit fails the leg with what journalctl said.
jexport() {
    local file="$1"
    shift
    local err rc
    if err="$( { sudo -n journalctl --no-pager -o json "$@" | tee "${file}" >/dev/null; } 2>&1)"; then rc=0; else rc=$?; fi
    if [[ "${rc}" -ne 0 ]] && { [[ -n "${err}" ]] || [[ -s "${file}" ]]; }; then
        printf 'journalctl %s failed (rc=%d): %s\n' "$*" "${rc}" "${err}"
        return 1
    fi
    printf '%s: %s entries (journalctl %s, rc=%d)\n' "$(basename "${file}")" "$(awk 'END{print NR}' "${file}")" "$*" "${rc}"
}

export_journal() {
    local units=() glob
    for glob in "${UNIT_GLOBS[@]}"; do
        units+=(-u "${glob}")
    done
    jexport "${EXPORTS}/units.json" "${units[@]}"
    # Who can stop a homeserver: ansible's module records, sudo's command records, the installer.
    jexport "${EXPORTS}/mentions.json" --grep 'agent-bus'
    jexport "${EXPORTS}/kernel.json" -k --grep '(?i)out of memory|oom-kill|oom_reaper|tuwunel|segfault|hung_task|no space left'
    # systemd-oomd names a unit's cgroup with \x2d escapes, so its lines do not match "agent-bus".
    jexport "${EXPORTS}/oomd.json" -u systemd-oomd
    sudo -n journalctl --list-boots -o json | tee "${EXPORTS}/boots.json" >/dev/null
    if [[ ! -s "${EXPORTS}/units.json" ]] && [[ "${#TEAMS[@]}" -gt 0 ]]; then
        printf 'no journal entry for any agent-bus unit, yet %s teams are installed: the journal is not readable as expected\n' "${#TEAMS[@]}"
        return 1
    fi
}

analyse_stops() {
    local args=() team
    for team in "${TEAMS[@]}"; do
        args+=(--team "${team}")
    done
    python3 -I "${PLAN_SCRIPT_DIR}/hs_journal.py" \
        --journal "${EXPORTS}/units.json" --journal "${EXPORTS}/mentions.json" \
        --journal "${EXPORTS}/kernel.json" --journal "${EXPORTS}/oomd.json" \
        --boots "${EXPORTS}/boots.json" --runs-root "${RUNS_ROOT}" \
        --report "${STOPS}" "${args[@]}" >"${SUMMARY}"
}

readonly HS_PROPS=Id,ActiveState,SubState,Result,ExecMainCode,ExecMainStatus,ExecMainPID,ExecMainStartTimestamp,ExecMainExitTimestamp,NRestarts,InvocationID,ActiveEnterTimestamp,InactiveEnterTimestamp,MemoryCurrent,MemoryPeak,MemorySwapPeak,MemoryMax,MemoryHigh,MemorySwapMax,MemoryAccounting,TasksCurrent,TasksMax,CPUUsageNSec,CPUQuotaPerSecUSec,LimitNOFILE,WatchdogUSec,TimeoutAbortUSec,TimeoutStopUSec,Restart,RestartUSec,RestartSteps,RestartMaxDelayUSec,StartLimitIntervalUSec,StartLimitBurst,OOMPolicy,OOMScoreAdjust,ManagedOOMMemoryPressure,ManagedOOMSwap,IPIngressBytes,IPEgressBytes,Slice
readonly ONESHOT_PROPS=Id,ActiveState,SubState,Result,ExecMainCode,ExecMainStatus,ExecMainStartTimestamp,ExecMainExitTimestamp,NRestarts,MemoryPeak

# tuwunel.toml with the value of every key named like a secret substituted when it is a quoted
# string (a *_file key holds a path, kept). Then checked: a secret-named key still holding a
# quoted value that is not "<redacted>" fails the probe and prints nothing of the file.
redacted_config() {
    local team="$1" raw clean
    raw="$(sudo -n cat "${STATE_ROOT}/${team}/tuwunel.toml")"
    clean="$(awk '{
        if (match($0, /^[[:space:]]*[A-Za-z0-9_.]*(secret|token|key|password|pass)[A-Za-z0-9_.]*[[:space:]]*=[[:space:]]*["'"'"']/) \
            && $0 !~ /^[[:space:]]*[A-Za-z0-9_.]*_file[[:space:]]*=/) { sub(/=.*/, "= \"<redacted>\"") }
        print }' <<<"${raw}")"
    if awk '/^[[:space:]]*[A-Za-z0-9_.]*(secret|token|key|password|pass)[A-Za-z0-9_.]*[[:space:]]*=[[:space:]]*["'"'"']/ \
            && !/_file[[:space:]]*=/ && !/"<redacted>"/ {bad=1} END{exit !bad}' <<<"${clean}"; then
        printf 'redaction check FAILED for %s: a secret-named key kept its value; nothing printed\n' "${team}"
        return 1
    fi
    printf '%s\n' "${clean}"
}

# Tuwunel #653 (a start hangs after an unclean kill, seen on btrfs) and #649 (writes panic on a
# full disk): the filesystem under each team's database, its mount options, its free space.
db_and_filesystem() {
    local team="$1" dir="${STATE_ROOT}/$1"
    sudo -n du -sh "${dir}/db" "${dir}"
    echo "--- every entry of the team directory, by size:"
    sudo -n du -sh "${dir}"/*
    echo "--- filesystem under the database (FSTYPE btrfs is the #653 case):"
    sudo -n findmnt -T "${dir}/db" -o TARGET,SOURCE,FSTYPE,OPTIONS
    echo "--- free space and inodes there (#649: a full disk):"
    sudo -n df -hT "${dir}/db"
    sudo -n df -i "${dir}/db"
    echo "--- file attributes of the database directory (C = no copy-on-write, on btrfs):"
    sudo -n lsattr -d "${dir}/db"
}

# One line per team for the summary: filesystem type and free space under its database.
db_fs_line() {
    # Called where errexit is off (inside a gather leg), so each read says when it failed.
    local team="$1" db="${STATE_ROOT}/$1/db" fs df size
    fs="$(sudo -n findmnt -n -T "${db}" -o FSTYPE)" || return 1
    df="$(sudo -n df -h --output=avail,pcent "${db}")" || return 1
    size="$(sudo -n du -sh "${db}")" || return 1
    printf 'team %s database: %s on %s, %s\n' "${team}" "${size%%[[:space:]]*}" "${fs}" \
        "$(awk 'NR==2 {print $1 " free, " $2 " used"}' <<<"${df}")"
}

health_record() {
    sudo -n cat "${HEALTH_ROOT}/$1.json"
}

coredumps() {
    sudo -n coredumpctl list --no-pager tuwunel
}

latest_coredump() {
    sudo -n coredumpctl info --no-pager tuwunel
}

meta_deploy_runs() {
    local run
    for run in "${RUNS_ROOT}"/_meta-deploy/*/; do
        printf '%s: %s\n' "$(basename "${run}")" "$(find "${run}" -maxdepth 1 -name '*.log' -printf '%f ' | sort)"
    done
}

#: Plan 00201's reader of the names the product owns.
readonly TRIAGE_M7_CHECK="${PLAN_REPO_ROOT}/CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py"

host_memory() {
    free -h
    echo "--- memory pressure (PSI):"
    cat /proc/pressure/memory
    echo "--- systemd-oomd:"
    systemctl is-active systemd-oomd.service
}

# The session watchers are not host units: each runs in its ccy container and writes its
# stderr, and its supervisor's, to the watcher log in the session home there, which a
# container restart empties. Read (podman exec cat, rootless, as this user) from every running
# container that has one. Its path is this checkout's seat.py's (Plan 00201's m7_check
# session-paths and session-names), never a copy.
watcher_logs() {
    local names name log found=0 home entries path
    home="$(python3 -I "${TRIAGE_M7_CHECK}" session-paths)" || return 1
    entries="$(python3 -I "${TRIAGE_M7_CHECK}" session-names)" || return 1
    home="${home%%$'\n'*}"
    entries="${entries##*$'\n'}"
    if [[ -z "${home}" || -z "${entries}" ]]; then
        echo "m7_check named no session home or watcher log" >&2
        return 1
    fi
    path="${home}/${entries}"
    # The words each kind of line ends with are cli.py's own, read the same way.
    local exits ended refused restarted dropped silent
    exits="$(python3 -I "${TRIAGE_M7_CHECK}" product-value helpers/pingbus/cli.py WATCHER_EXITS)" || return 1
    ended="$(python3 -I "${TRIAGE_M7_CHECK}" product-value helpers/pingbus/cli.py WATCHER_ENDED)" || return 1
    refused="$(python3 -I "${TRIAGE_M7_CHECK}" product-value helpers/pingbus/cli.py WATCHER_REFUSED)" || return 1
    restarted="$(python3 -I "${TRIAGE_M7_CHECK}" product-value helpers/pingbus/cli.py WATCHER_RESTARTED)" || return 1
    dropped="$(python3 -I "${TRIAGE_M7_CHECK}" product-value helpers/pingbus/cli.py TEAM_NOT_WATCHED)" || return 1
    silent="$(python3 -I "${TRIAGE_M7_CHECK}" product-value helpers/pingbus/cli.py HOMESERVER_SILENT)" || return 1
    names="$(podman ps --format '{{.Names}}')"
    for name in ${names}; do
        if ! podman exec "${name}" test -f "${path}"; then
            continue
        fi
        found=1
        log="$(podman exec "${name}" cat "${path}")"
        printf '=== %s: %s modified %s\n' "${name}" "${path}" "$(podman exec "${name}" stat -c '%y' "${path}")"
        printf '    exits: %s; supervisor restarts: %s; teams dropped: %s; homeserver outages: %s\n' \
            "$(grep -cF -e "${exits}" -e "${ended}" -e "${refused}" <<<"${log}")" \
            "$(grep -cF -e "${restarted}" <<<"${log}")" \
            "$(grep -cF -e "${dropped}" <<<"${log}")" \
            "$(grep -cF -e "${silent}" <<<"${log}")"
        awk '{l[NR]=$0} END{for(i=NR-39;i<=NR;i++) if(i>0) print "    " l[i]}' <<<"${log}"
    done
    if [[ "${found}" -eq 0 ]]; then
        echo "no running container has a ${path}"
    fi
}

gather_facts() {
    local team hs health backup failed=0
    heading "the Tuwunel binary the units run"
    probe "tuwunel --version" "${TUWUNEL}" --version
    probe "tuwunel binary mtime and size" stat -c '%y %s bytes %n' "${TUWUNEL}"
    probe "deployed pin" cat /usr/local/share/agent-bus/tuwunel.pin
    heading "journal span and reboots (correlate stops with these)"
    probe "boots" sudo -n journalctl --list-boots --no-pager
    probe "journal disk usage" sudo -n journalctl --disk-usage
    probe "meta-deploy runs and their entries (local time stamps)" meta_deploy_runs
    heading "coredumps of tuwunel (none is a fact too)"
    probe "coredumpctl list tuwunel" coredumps
    probe "coredumpctl info tuwunel (the latest)" latest_coredump
    heading "host memory"
    probe "memory, pressure, oomd" host_memory
    heading "timers"
    probe "agent-bus timers" systemctl list-timers --all --no-pager 'agent-bus-*'
    heading "session watchers: each running container's watch.log"
    probe "watch.log per container" watcher_logs
    for team in "${TEAMS[@]}"; do
        hs="$(python3 -I "${TRIAGE_M7_CHECK}" product-value helpers/agent_bus/health.py hs_unit "${team}")" || return 1
        health="$(python3 -I "${TRIAGE_M7_CHECK}" product-value helpers/agent_bus/health.py health_service "${team}")" || return 1
        backup="$(python3 -I "${TRIAGE_M7_CHECK}" product-value helpers/agent_bus/health.py backup_service "${team}")" || return 1
        heading "team ${team}: limits, peaks, restart counters, database and its filesystem"
        probe "${hs} state, limits and counters" systemctl show "${hs}" -p "${HS_PROPS}"
        probe "${hs} unit and drop-ins" systemctl cat "${hs}"
        probe "${health}" systemctl show "${health}" -p "${ONESHOT_PROPS}"
        probe "${backup}" systemctl show "${backup}" -p "${ONESHOT_PROPS}"
        probe "health record ${team}.json" health_record "${team}"
        probe "tuwunel.toml (secret-named string values substituted)" redacted_config "${team}"
        probe "database size, filesystem and free space" db_and_filesystem "${team}"
        if ! db_fs_line "${team}" >>"${SUMMARY}"; then
            printf 'team %s database: filesystem or free space could not be read (see the facts report)\n' "${team}" >>"${SUMMARY}"
            failed=1
        fi
    done
    return "${failed}"
}
