#!/usr/bin/env bash
# Plan 00161 — triage-ssh-agent-probe.bash: the probes behind triage-ssh-agent.bash.
#
# Usage: triage-ssh-agent-probe.bash <probe> <repo-root> <work-dir>
#   agent   which agent SSH_AUTH_SOCK is, the OpenSSH version, how many keys it lists
#   keys    each key this checkout's Quick Launch saved: passphrase, and whether the agent lists it
#   sign    for each saved key the agent lists, four timed attempts (exit 124 is the limit):
#           sign a scratch file directly through the agent and through the one-key agent, and
#           ssh -v -T to github.com both ways, then the one-key agent's log
#   network the host's addresses, routes and rules; timed TCP connects to GitHub's SSH ports
#           and API from the host and from a rootless container; ssh -v over port 443
#
# Read-only. Nothing is added to or removed from the agent; the one-key agent lives in an
# owner-only directory under XDG_RUNTIME_DIR for the length of the probe. A probe that cannot
# establish its fact exits non-zero, so triage-ssh-agent.bash names the leg.
set -euo pipefail

probe="${1:?usage: triage-ssh-agent-probe.bash <probe> <repo-root> <work-dir>}"
repoRoot="${2:?usage: triage-ssh-agent-probe.bash <probe> <repo-root> <work-dir>}"
work="${3:?usage: triage-ssh-agent-probe.bash <probe> <repo-root> <work-dir>}"

readonly CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly FILTER="/var/local/claude-yolo/lib/ssh_agent_filter.py"
readonly ATTEMPT_SECONDS=40
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
readonly U20=(python3 -I "${scriptDir}/u20_check.py")
SAVED_KEYS=()

# timed <label> <command...> — run it under a time limit; print its exit status, the seconds
# it took and its combined output. Exit 124 is the limit. The status is the fact, so it
# returns 0 whatever the command did.
timed() {
    local label="$1" start rc=0 out
    shift
    start=${SECONDS}
    out="$(timeout --kill-after=5 "${ATTEMPT_SECONDS}" "$@" 2>&1)" || rc=$?
    printf -- '--- %s: exit %s after %ss\n' "${label}" "${rc}" "$((SECONDS - start))"
    printf '%s\n' "${out:-(no output)}"
}

# needs_passphrase <key> — "yes" when ssh-keygen refuses the empty passphrase as a wrong one.
needs_passphrase() {
    local out
    if out="$(ssh-keygen -y -P '' -f "$1" 2>&1)"; then
        printf 'no'
    elif [[ "${out}" == *passphrase* ]]; then
        printf 'yes'
    else
        printf 'unknown (%s)' "${out}"
    fi
}

probe_agent() {
    printf 'SSH_AUTH_SOCK=%s\n' "${SSH_AUTH_SOCK:-unset}"
    [[ -n "${SSH_AUTH_SOCK:-}" ]] || return 1
    ls -l -- "${SSH_AUTH_SOCK}"
    printf 'listening process: '
    ss -xlpn | awk -v sock="${SSH_AUTH_SOCK}" '$5 == sock { print $NF; found = 1 } END { if (!found) print "(not shown)" }'
    printf 'user units: '
    systemctl --user list-units --all --no-pager --no-legend 'gcr-ssh-agent*' 'ssh-agent*' \
        | awk '{ printf "%s=%s/%s ", $1, $3, $4 } END { print "" }'
    ssh -V 2>&1
    local listed rc=0
    listed="$(ssh-add -l -E sha256 2>&1)" || rc=$?
    printf 'ssh-add -l: exit %s, %s line(s)\n' "${rc}" "$(grep -c . <<<"${listed}")"
}

# collect_keys — print each saved key's facts; SAVED_KEYS gets "file|fingerprint" for each the
# agent lists. Fails when there is none.
collect_keys() {
    local config_version keys key fp listed
    config_version="$(awk -F= '/^CONFIG_VERSION=/ { print $2; exit }' "${CCY_LAUNCHER}")"
    printf 'ccy: %s\n' "$(grep -m1 '^CCY_VERSION=' "${CCY_LAUNCHER}")"
    keys="$("${U20[@]}" launch-keys "${repoRoot}" "${config_version}")"
    if [[ -z "${keys}" ]]; then
        printf 'Quick Launch saved no SSH key for this checkout\n'
        return 1
    fi
    listed="$(ssh-add -l -E sha256 2>&1)"
    while IFS= read -r key; do
        if [[ "${key}" == ssh-agent ]]; then
            printf '%s: the whole agent, not a key file; nothing to forward\n' "${key}"
            continue
        fi
        fp="$(ssh-keygen -E sha256 -lf "${key}" | awk '{ print $2 }')"
        if awk -v fp="${fp}" '$2 == fp { found = 1 } END { exit !found }' <<<"${listed}"; then
            printf '%s: %s, needs a passphrase: %s, the agent lists it: yes\n' "${key}" "${fp}" "$(needs_passphrase "${key}")"
            SAVED_KEYS+=("${key}|${fp}")
        else
            printf '%s: %s, needs a passphrase: %s, the agent lists it: no\n' "${key}" "${fp}" "$(needs_passphrase "${key}")"
        fi
    done <<<"${keys}"
    if [[ "${#SAVED_KEYS[@]}" -eq 0 ]]; then
        printf 'no saved key is one the agent lists\n'
        return 1
    fi
}

# attempts <key> <sock> <how> — the offline signature and the GitHub probe, signing through <sock>.
attempts() {
    local key="$1" sock="$2" how="$3"
    rm -f -- "${work}/payload.sig"
    timed "sign offline, ${how}" \
        env SSH_AUTH_SOCK="${sock}" ssh-keygen -Y sign -f "${key}.pub" -n file "${work}/payload"
    rm -f -- "${work}/payload.sig"
    # The same options as ccy's own probe (_github_probe_identity), with -v.
    timed "ssh -T github.com, ${how}" \
        ssh -v -T -i "${key}" -o IdentitiesOnly=yes -o IdentityAgent="${sock}" -o BatchMode=yes \
        -F /dev/null -o StrictHostKeyChecking=no -o ConnectTimeout=10 -p 22 git@github.com
}

probe_sign() {
    local entry key fp filter_dir filter_pid tries rc
    collect_keys
    printf 'scratch\n' >"${work}/payload"
    for entry in "${SAVED_KEYS[@]}"; do
        key="${entry%%|*}"
        fp="${entry#*|}"
        printf '=== %s\n' "${key}"
        attempts "${key}" "${SSH_AUTH_SOCK}" "directly through the agent"

        # A socket path is limited to 108 bytes, which a run directory under the checkout
        # exceeds; ccy keeps its own one-key agent under XDG_RUNTIME_DIR for the same reason.
        filter_dir="$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/ccy-triage.XXXXXX")"
        python3 -I "${FILTER}" --listen "${filter_dir}/agent.sock" --upstream "${SSH_AUTH_SOCK}" \
            --allow "${fp}" --parent-pid "$$" </dev/null 2>"${filter_dir}/log" &
        filter_pid=$!
        tries=0
        while [[ ! -S "${filter_dir}/agent.sock" ]] && [[ "${tries}" -lt 50 ]]; do
            sleep 0.1
            tries=$((tries + 1))
        done
        timed "ssh-add -l through the one-key agent" env SSH_AUTH_SOCK="${filter_dir}/agent.sock" ssh-add -l -E sha256
        attempts "${key}" "${filter_dir}/agent.sock" "through the one-key agent"
        if ! kill "${filter_pid}" 2>/dev/null; then
            printf -- '--- the one-key agent had already exited\n'
        fi
        rc=0
        wait "${filter_pid}" || rc=$?
        printf -- '--- the one-key agent exited with status %s; its log:\n' "${rc}"
        cat -- "${filter_dir}/log"
        rm -rf -- "${filter_dir}"
    done
}

# tcp_try <host> <port> — three timed TCP connects from the host; prints each result.
tcp_try() {
    local host="$1" port="$2" n start rc
    for n in 1 2 3; do
        start=${SECONDS}
        rc=0
        timeout 10 bash -c "exec 3<>/dev/tcp/${host}/${port}" 2>&1 || rc=$?
        printf -- '--- TCP %s:%s try %s: exit %s after %ss\n' "${host}" "${port}" "${n}" "${rc}" "$((SECONDS - start))"
    done
}

# probe_network — why the host's own connections to GitHub's SSH ports time out while a
# container on the same host gets through: addresses, routes, rules, active connections, the
# same TCP connects from the host and from a rootless container, and ssh -v over 443.
probe_network() {
    local host addr
    ip -br addr
    ip route
    ip rule
    nmcli -t -f NAME,TYPE,DEVICE connection show --active
    for host in github.com ssh.github.com api.github.com; do
        addr="$(getent ahostsv4 "${host}" | awk 'NR == 1 { print $1 }')"
        printf '%s -> %s; ' "${host}" "${addr:-(no address)}"
        if [[ -n "${addr}" ]]; then ip route get "${addr}"; else printf '\n'; fi
    done
    tcp_try github.com 22
    tcp_try ssh.github.com 443
    tcp_try api.github.com 443
    timed "TCP github.com:22 from a rootless container" \
        podman run --rm --network podman docker.io/library/alpine nc -z -w 10 github.com 22
    timed "TCP ssh.github.com:443 from a rootless container" \
        podman run --rm --network podman docker.io/library/alpine nc -z -w 10 ssh.github.com 443
    timed "ssh -T ssh.github.com:443 through the agent" \
        ssh -v -T -o IdentityAgent="${SSH_AUTH_SOCK:-none}" -o BatchMode=yes \
        -F /dev/null -o StrictHostKeyChecking=no -o ConnectTimeout=10 -p 443 git@ssh.github.com
}

case "${probe}" in
    agent) probe_agent ;;
    keys) collect_keys ;;
    sign) probe_sign ;;
    network) probe_network ;;
    *)
        printf '[FATAL] unknown probe: %s\n' "${probe}" >&2
        exit 64
        ;;
esac
