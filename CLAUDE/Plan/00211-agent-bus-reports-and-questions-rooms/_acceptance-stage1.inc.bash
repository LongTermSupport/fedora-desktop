# shellcheck shell=bash
# _acceptance-stage1.inc.bash — the legs and checks of Plan 00211's acceptance.bash, stage 1
# (S1, S2) and its --fake fixtures. A separate file because the legs are called through
# plan_deploy_leg and check_leg, which ShellCheck cannot see through (SC2329), and suppression
# is banned. Sourced, never executed: no shell options, no `exit`. Every function returns
# non-zero with a message on stderr when it does not pass. The caller sets HOST_PINGBUS,
# STAGED_PINGBUS, LXC_INFO, LXC_ATTACH, MEMBERS_JSON and REPORT.

WANT=""
FIX=""

# want_version — the `pingbus version` line this checkout's code prints: the new one.
want_version() {
    WANT="$(cd "${PLAN_REPO_ROOT}" && python3 -B -s -c 'import sys; from helpers.pingbus import cli; sys.exit(cli.main(["version"]))')" || return 1
    if [[ "${WANT}" != "pingbus "*" protocol "* ]]; then
        printf 'this checkout printed %q for pingbus version\n' "${WANT}" >&2
        return 1
    fi
    printf 'want: %s\n' "${WANT}" | tee -a "${REPORT}"
}

# same_version <label> <got> — the line is WANT, or the label is named with both lines.
same_version() {
    printf -- '- %s: %s\n' "$1" "$2" >>"${REPORT}"
    if [[ "$2" != "${WANT}" ]]; then
        printf '%s prints "%s", not "%s": it is older than this checkout (re-run the deploy)\n' "$1" "$2" "${WANT}" >&2
        return 1
    fi
    printf '%s: %s\n' "$1" "$2"
}

# s1_check — the host's pingbus and the ccy build context's.
s1_check() {
    local path got
    for path in "${HOST_PINGBUS}" "${STAGED_PINGBUS}"; do
        if [[ ! -f "${path}" ]]; then
            printf '%s is missing: its play has not run\n' "${path}" >&2
            return 1
        fi
        got="$(python3 -B -s "${path}" version)" || return 1
        same_version "${path}" "${got}" || return 1
    done
}

# s2_check — every declared LXC container is RUNNING and its pingbus is WANT. Every container
# is judged before the verdict, so one run names all that are down or old.
s2_check() {
    local containers name state got bad=0
    containers="$(jq -r '[.[] | select((.state // "present") == "present") | .container] | unique | .[]' "${MEMBERS_JSON}")" || return 1
    if [[ -z "${containers}" ]]; then
        printf 'no present agent_bus_lxc_members entry: nothing to check\n' | tee -a "${REPORT}"
        return 0
    fi
    while IFS= read -r name; do
        state="$("${LXC_INFO[@]}" -n "${name}" -sH)" || {
            printf 'lxc-info failed for container %s: is it the name of an LXC container here?\n' "${name}" >&2
            bad=1
            continue
        }
        if [[ "${state}" != "RUNNING" ]]; then
            printf 'container %s is %s, not RUNNING: start it, then run meta-deploy again (deploy 1 copies its kit only while it runs)\n' "${name}" "${state}" >&2
            printf -- '- %s: %s\n' "${name}" "${state}" >>"${REPORT}"
            bad=1
            continue
        fi
        got="$("${LXC_ATTACH[@]}" -n "${name}" --clear-env -- env -i PATH=/usr/local/bin:/usr/bin:/bin pingbus version)" || {
            printf 'pingbus version failed in container %s: its kit is missing or broken\n' "${name}" >&2
            bad=1
            continue
        }
        same_version "container ${name}" "${got}" || bad=1
    done <<<"${containers}"
    return "${bad}"
}

# read_members — agent_bus_lxc_members as the plays see it, through an ad-hoc Ansible copy into
# the run directory (0600): the inventory and vault are Ansible's, so nothing here parses YAML.
read_members() {
    local args
    args="$(jq -cn --arg dest "${MEMBERS_JSON}" \
        '{content: "{{ agent_bus_lxc_members | default([]) | to_json }}\n", dest: $dest, mode: "0600"}')" || return 1
    plan_ansible_adhoc localhost -m ansible.builtin.copy -a "${args}" || return 1
    jq -e 'type == "array"' "${MEMBERS_JSON}" >/dev/null
}

# ── --fake ───────────────────────────────────────────────────────────────────────────────
# A fake pingbus is a Python file printing one line; the fake lxc-info and lxc-attach print
# <name>.state and <name>.version from their own directory, <name> being their second argument.

fake_setup() {
    FIX="${PLAN_RUN_DIR}/fixtures"
    mkdir -p "${FIX}" || return 1
    cat >"${FIX}/lxc-info" <<'EOF' || return 1
#!/usr/bin/env bash
set -euo pipefail
cat -- "$(dirname "$0")/$2.state"
EOF
    cat >"${FIX}/lxc-attach" <<'EOF' || return 1
#!/usr/bin/env bash
set -euo pipefail
cat -- "$(dirname "$0")/$2.version"
EOF
    chmod 0755 "${FIX}/lxc-info" "${FIX}/lxc-attach" || return 1
    LXC_INFO=("${FIX}/lxc-info")
    LXC_ATTACH=("${FIX}/lxc-attach")
    printf 'print("pingbus 0.1.0 protocol 1")\n' >"${FIX}/old-pingbus"
    printf 'print("%s")\n' "${WANT}" >"${FIX}/new-pingbus"
}

# fake_case <host> <staged> <members json> [<name>=<state>:<old|new>...] — one fixture.
fake_case() {
    HOST_PINGBUS="${FIX}/$1-pingbus"
    STAGED_PINGBUS="${FIX}/$2-pingbus"
    MEMBERS_JSON="${FIX}/members.json"
    printf '%s\n' "$3" >"${MEMBERS_JSON}" || return 1
    shift 3
    local spec name state which line
    for spec in "$@"; do
        name="${spec%%=*}"
        state="${spec#*=}"
        which="${state#*:}"
        state="${state%%:*}"
        printf '%s\n' "${state}" >"${FIX}/${name}.state"
        line="pingbus 0.1.0 protocol 1"
        if [[ "${which}" == "new" ]]; then
            line="${WANT}"
        fi
        printf '%s\n' "${line}" >"${FIX}/${name}.version"
    done
}

# fake_s1 <host old|new|missing> <staged old|new|missing>
fake_s1() { fake_case "$1" "$2" '[]' && s1_check; }

# fake_s2 <members json> [<name>=<state>:<old|new>...]
fake_s2() {
    local members="$1"
    shift
    fake_case new new "${members}" "$@" && s2_check
}
