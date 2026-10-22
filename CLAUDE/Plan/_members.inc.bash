# shellcheck shell=bash
#
# _members.inc.bash — sourced by plan scripts that judge the declared team members.
#
# A declared team member whose container is not running is OFFLINE, which is normal lifecycle
# and never a failure (CLAUDE/AgentNotes.md, "An offline team member never blocks anything";
# the rule offline-member-blocks in CLAUDE/QA.md holds it). Sourced, never executed: no shell
# options, no `exit`. Tests: scripts/test-planlib.bash.

# The lxc-info command plan_lxc_online asks, as an array, so a test or a --fake run can point it
# at a stub.
PLAN_LXC_INFO=(sudo -n lxc-info)

# The prefix of each offline line plan_lxc_online writes on stderr, so a caller tells them from
# any other stderr (a sudo or lxc warning).
PLAN_LXC_OFFLINE_PREFIX="offline: "

# plan_lxc_online <container>... — the running ones among the named LXC containers, one per
# line on stdout, in the order given. Each other one is left out and named on stderr as
# `offline: <container> (container <state>)`, its lxc-info state as given. Only a container
# lxc-info cannot read (one that does not exist, or no sudo), or one that is ABORTING (lxc
# failed to start or stop it, which is broken, not offline), fails, naming it, after every
# container has been asked.
plan_lxc_online() {
    local name state bad=0
    for name in "$@"; do
        state="$("${PLAN_LXC_INFO[@]+"${PLAN_LXC_INFO[@]}"}" -n "${name}" -sH)" || {
            printf 'lxc-info could not read container %s: is it the name of an LXC container here?\n' "${name}" >&2
            bad=1
            continue
        }
        case "${state}" in
            RUNNING) printf '%s\n' "${name}" ;;
            ABORTING)
                printf 'container %s is ABORTING: lxc failed to start or stop it; read lxc-info -n %s and its log\n' "${name}" "${name}" >&2
                bad=1
                ;;
            *) printf '%s%s (container %s)\n' "${PLAN_LXC_OFFLINE_PREFIX}" "${name}" "${state,,}" >&2 ;;
        esac
    done
    return "${bad}"
}

# plan_lxc_online_named <report> <container>... — plan_lxc_online, with each offline container
# said on stderr and appended to <report> as `- <container> (container <state>): offline, not
# judged`; any other stderr line is passed through to stderr unchanged. Needs PLAN_RUN_DIR.
plan_lxc_online_named() {
    local report="$1" err="${PLAN_RUN_DIR}/lxc-online.err" online line rc=0
    shift
    online="$(plan_lxc_online "$@" 2>"${err}")" || rc=$?
    while IFS= read -r line; do
        if [[ "${line}" == "${PLAN_LXC_OFFLINE_PREFIX}"* ]]; then
            line="${line#"${PLAN_LXC_OFFLINE_PREFIX}"}: offline, not judged"
            printf '%s\n' "${line}" >&2
            printf -- '- %s\n' "${line}" >>"${report}" || return 1
        else
            printf '%s\n' "${line}" >&2
        fi
    done <"${err}"
    if [[ "${rc}" -ne 0 ]]; then
        return "${rc}"
    fi
    if [[ -n "${online}" ]]; then
        printf '%s\n' "${online}"
    fi
}
