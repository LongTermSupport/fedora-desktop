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
# A container that is not running is offline: named in the report and not judged.
#
# Exit 0: proven. Exit 1: a member session would start, or did start, at boot. Exit 2: COULD NOT
# ESTABLISH (nothing declared; nothing judged, as when every container is offline; fewer units
# found than online service members declared, per generation; a probe that did not answer; an
# lxc-stop or lxc-start that failed during the reboot).
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

# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_members.inc.bash
source "${repoRoot}/CLAUDE/Plan/_members.inc.bash"

PLAN_USAGE="usage: $(basename "$0") [-y|--yes] [-h|--help]
Check that no LXC member session is enabled or wanted at boot, then an optional reboot of each
member container, asked per container at the terminal, default no (-y does not answer it: a
container is never rebooted unasked)."
plan_parse_common_flags "$@"
# deploy, not gather: the run may reboot a container, which is a change (R7).
plan_mode deploy
plan_require_host "it inspects this host's LXC containers"
plan_prime_sudo
plan_start_log auto

wantsDir=/etc/systemd/system/multi-user.target.wants
unitDir=/etc/systemd/system
bootWait=120
failed=0
unknown=0
offlineReport="${PLAN_RUN_DIR}/offline-members-report.md"
declare -A unitsOf=()
v1Wanted=0 v1Found=0 v2Wanted=0 v2Found=0 unitFound=0 bootsProven=0

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
    # Not plan_ansible_adhoc: it passes -v, and at -v ansible prints its config file line on
    # stdout before the json document, which jq cannot read (plan-ansible-adhoc-stdout-parsed).
    # The config is named, so the working directory does not matter.
    ANSIBLE_CONFIG="${PLAN_REPO_ROOT}/ansible.cfg" ANSIBLE_STDOUT_CALLBACK=json ANSIBLE_LOAD_CALLBACK_PLUGINS=1 \
        ansible localhost -m ansible.builtin.debug -a "${args}" </dev/null |
        jq -r '.plays[0].tasks[0].hosts.localhost.msg'
}

# ask runs a shell probe in a container as root, its stdin closed, and prints the one word the
# probe printed. An lxc-attach that fails, or a probe that prints no word, is exit 1: the
# question was not answered, which is never read as an answer.
ask() {
    local container="$1" probe="$2" said=""
    said="$(sudo -n lxc-attach -n "${container}" --clear-env -- env -i PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin sh -c "${probe}" </dev/null)" || return 1
    [[ -n "${said}" ]] || return 1
    printf '%s\n' "${said}"
}

# running is whether container runs now, asked of lxc-ls itself so the boot wait below sees a
# container that stopped again (ready-wait-ignores-child-exit).
running() {
    local name
    while IFS= read -r name; do
        [[ "${name}" != "$1" ]] || return 0
    done < <(sudo -n lxc-ls -1 --running </dev/null)
    return 1
}

# checkUnit judges one unit in a container: present, not enabled, no wants link. unitFound
# says whether the unit was found (so judged), for the coverage count.
checkUnit() {
    local container="$1" unit="$2" said=""
    unitFound=0
    if ! said="$(ask "${container}" "if test -f '${unitDir}/${unit}'; then echo present; else echo absent; fi")"; then
        unsure "${container}: could not look for ${unit} (lxc-attach failed)"
        return 0
    fi
    if [[ "${said}" != present ]]; then
        bad "${container}: ${unit} is not present in ${unitDir}"
        return 0
    fi
    evidence "${container}: ${unit} present"
    unitFound=1
    # is-enabled exits non-zero for every state but enabled; its word is the answer.
    if ! said="$(ask "${container}" "systemctl is-enabled '${unit}' || true")"; then
        unsure "${container}: systemctl is-enabled ${unit} did not answer"
    elif [[ "${said}" == enabled* ]]; then
        bad "${container}: ${unit} is ${said}"
    else
        evidence "${container}: systemctl is-enabled ${unit} = ${said}"
    fi
    if ! said="$(ask "${container}" "if test -L '${wantsDir}/${unit}'; then echo linked; else echo none; fi")"; then
        unsure "${container}: could not look for a wants link for ${unit} (lxc-attach failed)"
    elif [[ "${said}" == linked ]]; then
        bad "${container}: ${wantsDir}/${unit} exists (a wants link)"
    else
        evidence "${container}: no wants link for ${unit}"
    fi
    unitsOf["${container}"]+="${unit} "
}

# booted is whether container's systemd has finished booting (running or degraded).
booted() {
    local said=""
    said="$(ask "$1" "systemctl is-system-running || true")" || return 1
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

# afterBoot is the boot proof for one container: each of its checked units is inactive (which
# also proves the probe could see the unit), and no claude runs as any of its users.
afterBoot() {
    local container="$1" users="$2" user="" unit="" said="" found="" rc=0
    for unit in ${unitsOf["${container}"]:-}; do
        if ! said="$(ask "${container}" "systemctl is-active '${unit}' || true")"; then
            unsure "${container}: systemctl is-active ${unit} did not answer after boot"
        elif [[ "${said}" == inactive ]]; then
            evidence "${container}: ${unit} is inactive after boot"
        else
            bad "${container}: ${unit} is ${said} after boot"
        fi
    done
    for user in ${users}; do
        rc=0
        found="$(sudo -n lxc-attach -n "${container}" --clear-env -- pgrep -u "${user}" -x claude </dev/null)" || rc=$?
        if ((rc == 0)); then
            bad "${container}: claude runs as ${user} after boot (pids ${found//$'\n'/ })"
        elif ((rc == 1)); then
            evidence "${container}: pgrep -u ${user} -x claude finds nothing after boot"
        else
            unsure "${container}: pgrep -u ${user} -x claude failed (exit ${rc})"
        fi
    done
}

# rebootProof reboots container (after asking) and runs afterBoot once it has booted.
rebootProof() {
    local container="$1" users="$2" waited=0
    if ! askReboot "${container}"; then
        evidence "${container}: reboot not chosen; the boot proof was not run"
        return 0
    fi
    # A failed lxc-stop or lxc-start is COULD NOT ESTABLISH, never its own exit code (which
    # could read as the exit-1 verdict).
    if ! sudo -n lxc-stop -n "${container}" </dev/null; then
        unsure "${container}: lxc-stop failed, so the boot proof was not run"
        return 0
    fi
    if ! sudo -n lxc-start -n "${container}" </dev/null; then
        unsure "${container}: lxc-start failed after lxc-stop, so the boot proof was not run"
        return 0
    fi
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
    afterBoot "${container}" "${users}"
    bootsProven=$((bootsProven + 1))
}

declared="$(members)"
if [[ -z "${declared//[[:space:]]/}" ]]; then
    unsure "no LXC member is declared (agent_bus_lxc_members, teamchat_lxc_members)"
    exit 2
fi
mapfile -t names < <(awk 'NF { print $2 }' <<<"${declared}" | sort -u)
# A container that is not running is an offline member: named in the report, never judged.
if ! running="$(plan_lxc_online_named "${offlineReport}" "${names[@]}")"; then
    unsure "lxc-info could not read a declared container (see above)"
    exit 2
fi
containers=()
mapfile -t containers <<<"${running}"
isOnline() { [[ " ${containers[*]} " == *" $1 "* ]]; }

while read -r gen container user service; do
    [[ -n "${gen}" ]] || continue
    evidence "declared: ${gen} ${user}@${container} service=${service}"
    isOnline "${container}" || continue
    if [[ "${service}" == True && "${gen}" == v1 ]]; then
        v1Wanted=$((v1Wanted + 1))
        checkUnit "${container}" "agent-bus-claude-${user}.service"
        v1Found=$((v1Found + unitFound))
    elif [[ "${service}" == True ]]; then
        v2Wanted=$((v2Wanted + 1))
    fi
done <<<"${declared}"

for container in "${containers[@]}"; do
    [[ -n "${container}" ]] || continue
    # The listing ends in a line "end" only when find succeeded, so a failed find, or one that
    # failed part way, is never read as fewer units.
    if ! units="$(ask "${container}" "find '${unitDir}' -maxdepth 1 -name 'teamchat-session-*.service' -printf '%f\\n' && echo end")" ||
        [[ "${units##*$'\n'}" != end ]]; then
        unsure "${container}: could not list ${unitDir} for teamchat-session units (lxc-attach or find failed)"
        continue
    fi
    for unit in ${units}; do
        [[ "${unit}" != end ]] || continue
        checkUnit "${container}" "${unit}"
        v2Found=$((v2Found + unitFound))
    done
done

# Coverage is judged per generation against the online service members declared: fewer units
# found than members is a proof not made, never a pass.
evidence "COVERAGE v1: ${v1Found} unit(s) checked for ${v1Wanted} online service member(s)"
evidence "COVERAGE v2: ${v2Found} unit(s) checked for ${v2Wanted} online service member(s)"
if ((v1Found < v1Wanted)); then
    unsure "v1 declares ${v1Wanted} online service member(s), but only ${v1Found} agent-bus-claude unit(s) were found"
fi
if ((v2Found < v2Wanted)); then
    unsure "v2 declares ${v2Wanted} online service member(s), but only ${v2Found} teamchat-session unit(s) were found"
fi

for container in "${containers[@]}"; do
    [[ -n "${container}" ]] || continue
    users="$(awk -v c="${container}" '$2 == c { print $3 }' <<<"${declared}" | sort -u | tr '\n' ' ')"
    rebootProof "${container}" "${users}"
done

# A run that judged no unit and proved no boot made no proof, whatever else it saw.
if ((v1Found + v2Found + bootsProven == 0)); then
    if [[ -z "${running}" ]]; then
        unsure "every declared container is offline (named above), so nothing was judged"
    else
        unsure "no member unit was found and no reboot was proven, so nothing was judged"
    fi
fi

if ((failed)); then
    printf '==> FAIL: a member session is enabled, wanted at boot, or ran after boot\n' >&2
    PLAN_FAILED_LEGS="no-autostart"
    plan_finish
fi
if ((unknown)); then
    # plan_finish has no third status: COULD NOT ESTABLISH is exit 2, said here.
    plan_list_reports
    printf '==> COULD NOT ESTABLISH: see the lines above\n' >&2
    exit 2
fi
printf '==> PASS: no LXC member session is enabled or wanted at boot\n'
plan_finish
