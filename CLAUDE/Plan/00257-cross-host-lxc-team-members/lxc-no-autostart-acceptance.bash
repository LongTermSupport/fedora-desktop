#!/usr/bin/env bash
# Plan 00257 Task B.12 acceptance: no LXC member's session starts with its container.
#
# For every declared LXC member container (v1 agent_bus_lxc_members, v2 teamchat_lxc_members,
# read from the inventory by Ansible as the plays read them), inside the container:
#   - each member session unit file is present (v1 agent-bus-claude-<user>.service for a
#     service: true entry; v2 every teamchat-session-*.service);
#   - `systemctl is-enabled` of each is not "enabled";
#   - no link to it is in multi-user.target.wants.
# Then it asks, for each running container, whether to reboot it (lxc-stop, lxc-start), default
# no (askReboot). Only on yes is it rebooted; once it has booted, `pgrep -u <user> -x claude` must find nothing for
# each of its members' users.
#
# Exit 0: proven. Exit 1: a member session would start, or did start, at boot. Exit 2: COULD NOT
# ESTABLISH (nothing declared, a container not running, a probe that did not answer).
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: $(basename "$0") [-y|--yes] [-h|--help]
Read-only check that no LXC member session is enabled or wanted at boot, then an optional
reboot of each member container, asked per container at the terminal, default no (-y does
not answer it: a container is never rebooted unasked)."
plan_parse_common_flags "$@"
plan_mode gather
plan_require_host "it inspects this host's LXC containers"
plan_prime_sudo
plan_start_log auto

wantsDir=/etc/systemd/system/multi-user.target.wants
unitDir=/etc/systemd/system
bootWait=120
failed=0
unknown=0

evidence() { printf 'EVIDENCE %s\n' "$*"; }
bad() {
    printf 'FAIL %s\n' "$*" >&2
    failed=1
}
unsure() {
    printf 'COULD NOT ESTABLISH %s\n' "$*" >&2
    unknown=1
}

# members prints "<generation> <container> <user> <service>" for each declared LXC member,
# as Ansible reads the inventory (the plays' own source of the declaration).
members() {
    local tpl args
    tpl='{% for m in agent_bus_lxc_members | default([]) %}v1 {{ m.container }} {{ m.user }} {{ m.service | default(false) | bool }}
{% endfor %}{% for m in teamchat_lxc_members | default([]) %}v2 {{ m.container }} {{ m.user }} {{ m.service | default(false) | bool }}
{% endfor %}'
    # The module arguments as JSON, which ansible takes whole: no k=v splitting of the template.
    args="$(jq -cn --arg t "${tpl}" '{msg: $t}')"
    ANSIBLE_STDOUT_CALLBACK=json ANSIBLE_LOAD_CALLBACK_PLUGINS=1 \
        plan_ansible_adhoc localhost -m ansible.builtin.debug -a "${args}" |
        jq -r '.plays[0].tasks[0].hosts.localhost.msg'
}

# inside runs a command in a container as root, its stdin closed.
inside() {
    local container="$1"
    shift
    sudo -n lxc-attach -n "${container}" --clear-env -- env -i PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin "$@" </dev/null
}

running() { [[ "$(sudo -n lxc-info -n "$1" -s -H </dev/null)" == "RUNNING" ]]; }

# checkUnit judges one unit in a container: present, not enabled, no wants link.
checkUnit() {
    local container="$1" unit="$2" state="" rc=0
    if ! inside "${container}" test -f "${unitDir}/${unit}"; then
        bad "${container}: ${unit} is not present in ${unitDir}"
        return 0
    fi
    evidence "${container}: ${unit} present"
    # is-enabled exits non-zero for every state but enabled; its word is the answer.
    state="$(inside "${container}" systemctl is-enabled "${unit}")" || rc=$?
    if [[ -z "${state}" ]]; then
        unsure "${container}: systemctl is-enabled ${unit} said nothing (exit ${rc})"
    elif [[ "${state}" == enabled* ]]; then
        bad "${container}: ${unit} is ${state}"
    else
        evidence "${container}: systemctl is-enabled ${unit} = ${state}"
    fi
    if inside "${container}" test -L "${wantsDir}/${unit}"; then
        bad "${container}: ${wantsDir}/${unit} exists (a wants link)"
    else
        evidence "${container}: no wants link for ${unit}"
    fi
}

# booted is whether container's systemd has finished starting (running or degraded).
booted() {
    local said="" rc=0
    said="$(inside "$1" systemctl is-system-running)" || rc=$?
    [[ "${said}" == running || "${said}" == degraded ]]
}

# askReboot asks the owner whether to reboot container, default no. It departs from R5 on
# purpose: plan_confirm takes PLAN_ASSUME_YES (which meta-deploy.bash exports) as yes, and a
# member's container is never rebooted unasked. So the question is always put at the terminal,
# the prompt on stdout (through the log) and the reply from /dev/tty; Enter, "n", end of input
# or no terminal at all is no. Bounded: three unreadable answers are no.
askReboot() {
    local container="$1" reply="" attempt
    if ! _plan_tty_openable; then
        evidence "${container}: no terminal to ask on, so it is not rebooted"
        return 1
    fi
    for attempt in 1 2 3; do
        printf '\nReboot LXC container %s (lxc-stop, then lxc-start) to prove no member session starts at boot? [y/N]\n' "${container}"
        if ! IFS= read -r reply </dev/tty; then
            return 1
        fi
        case "${reply,,}" in
            y | yes) return 0 ;;
            "" | n | no) return 1 ;;
        esac
        printf '==> answer y or n (got "%s") [attempt %d of 3]\n' "${reply}" "${attempt}" >&2
    done
    return 1
}

# rebootProof reboots container (after asking) and checks no claude runs as any of users.
rebootProof() {
    local container="$1" users="$2" user="" waited=0 found="" rc=0
    if ! askReboot "${container}"; then
        evidence "${container}: reboot not chosen; the boot proof was not run"
        return 0
    fi
    sudo -n lxc-stop -n "${container}" </dev/null
    sudo -n lxc-start -n "${container}" </dev/null
    until booted "${container}"; do
        if ! running "${container}"; then
            unsure "${container}: stopped again after lxc-start, before it booted"
            return 0
        fi
        if ((waited >= bootWait)); then
            unsure "${container}: not booted ${bootWait}s after lxc-start"
            return 0
        fi
        sleep 2
        waited=$((waited + 2))
    done
    evidence "${container}: booted after ${waited}s"
    for user in ${users}; do
        rc=0
        found="$(inside "${container}" pgrep -u "${user}" -x claude)" || rc=$?
        if ((rc == 0)); then
            bad "${container}: claude runs as ${user} after boot (pids ${found//$'\n'/ })"
        elif ((rc == 1)); then
            evidence "${container}: pgrep -u ${user} -x claude finds nothing after boot"
        else
            unsure "${container}: pgrep -u ${user} -x claude failed (exit ${rc})"
        fi
    done
}

declared="$(members)"
if [[ -z "${declared//[[:space:]]/}" ]]; then
    unsure "no LXC member is declared (agent_bus_lxc_members, teamchat_lxc_members)"
    exit 2
fi
containers=()
while read -r gen container user service; do
    [[ -n "${gen}" ]] || continue
    evidence "declared: ${gen} ${user}@${container} service=${service}"
    if ! running "${container}"; then
        unsure "${container}: not running, so its units cannot be read"
        continue
    fi
    [[ " ${containers[*]} " == *" ${container} "* ]] || containers+=("${container}")
    if [[ "${gen}" == v1 && "${service}" == True ]]; then
        checkUnit "${container}" "agent-bus-claude-${user}.service"
    fi
done <<<"${declared}"

for container in "${containers[@]}"; do
    units="$(inside "${container}" find "${unitDir}" -maxdepth 1 -name 'teamchat-session-*.service' -printf '%f\n')"
    for unit in ${units}; do
        checkUnit "${container}" "${unit}"
    done
done

for container in "${containers[@]}"; do
    users="$(awk -v c="${container}" '$2 == c { print $3 }' <<<"${declared}" | sort -u | tr '\n' ' ')"
    rebootProof "${container}" "${users}"
done

if ((failed)); then
    printf '==> FAIL: a member session is enabled, wanted at boot, or ran after boot\n' >&2
    exit 1
fi
if ((unknown)); then
    printf '==> COULD NOT ESTABLISH: see the lines above\n' >&2
    exit 2
fi
printf '==> PASS: no LXC member session is enabled or wanted at boot\n'
exit 0
