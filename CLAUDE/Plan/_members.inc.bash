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

# plan_lxc_online <container>... — the running ones among the named LXC containers, one per
# line on stdout, in the order given. Each other one is named on stderr as `<container> is
# offline (container <state>)` and left out. Only a container lxc-info cannot read (one that
# does not exist, or no sudo) fails, naming it, after every container has been asked.
plan_lxc_online() {
    local name state bad=0
    for name in "$@"; do
        state="$("${PLAN_LXC_INFO[@]+"${PLAN_LXC_INFO[@]}"}" -n "${name}" -sH)" || {
            printf 'lxc-info could not read container %s: is it the name of an LXC container here?\n' "${name}" >&2
            bad=1
            continue
        }
        if [[ "${state}" == "RUNNING" ]]; then
            printf '%s\n' "${name}"
        else
            printf '%s is offline (container %s)\n' "${name}" "${state,,}" >&2
        fi
    done
    return "${bad}"
}
