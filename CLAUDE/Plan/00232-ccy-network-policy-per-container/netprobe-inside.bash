#!/usr/bin/env bash
# Plan 00232 — netprobe-inside.bash: runs INSIDE a container (fed on stdin to `podman exec -i
# <c> bash -s -- <target>...`) and reports, for each target, whether a connection was made, how
# long the attempt took and what the client printed. Reports only: it renders no verdict, and
# the caller (triage or acceptance) reads the RESULT lines.
#
# Needs only what the ccy image has: bash, curl, ssh, nc (netcat-openbsd), getent, date.
#
# Targets, one argument each, fields split by '|':
#   facts                          the container's resolv.conf, hosts, routes and capabilities
#   http|<label>|<host>|<port>     curl http://<host>:<port>/  (an IPv6 host is bracketed)
#   url|<label>|<url>              curl <url>
#   ssh|<label>|<host>|<port>      ssh in batch mode (no key is offered; only the connect matters)
#   nc|<label>|<host>|<port>       nc -zv (verbose, so a refusal prints its reason)
#   dns|<label>|<name>             getent ahosts <name>
#
# Output: one line per attempt,
#   RESULT <label> kind=<k> rc=<n> ms=<n> http=<code|-> err=<first line of the client's stderr>
set -euo pipefail

CONNECT_TIMEOUT=5

now_ms() {
    local ns
    ns="$(date +%s%N)"
    printf '%s' "$((ns / 1000000))"
}

first_line() {
    local text="$1"
    text="${text%%$'\n'*}"
    printf '%s' "${text:--}"
}

result() {
    local label="$1" kind="$2" rc="$3" ms="$4" http="$5" err="$6"
    printf 'RESULT %s kind=%s rc=%s ms=%s http=%s err=%s\n' "${label}" "${kind}" "${rc}" "${ms}" "${http}" "$(first_line "${err}")"
}

show_facts() {
    printf '### /etc/resolv.conf\n'
    cat /etc/resolv.conf
    printf '### /etc/hosts\n'
    cat /etc/hosts
    printf '### /proc/net/route (hex, little-endian)\n'
    cat /proc/net/route
    printf '### /proc/net/if_inet6\n'
    cat /proc/net/if_inet6
    printf '### capabilities of this shell (CapEff bit 12 = NET_ADMIN, bit 21 = SYS_ADMIN)\n'
    awk '/^Cap(Eff|Bnd):/' /proc/self/status
    printf '### nft and ip in the image\n'
    local tool
    for tool in nft ip iptables; do
        if command -v "${tool}" >/dev/null; then printf '%s: present\n' "${tool}"; else printf '%s: absent\n' "${tool}"; fi
    done
}

bracket() {
    local host="$1"
    if [[ "${host}" == *:* ]]; then printf '[%s]' "${host}"; else printf '%s' "${host}"; fi
}

probe_curl() {
    local label="$1" kind="$2" url="$3" start rc http errfile err
    errfile="$(mktemp)"
    start="$(now_ms)"
    if http="$(curl -sS -o /dev/null -w '%{http_code}' --connect-timeout "${CONNECT_TIMEOUT}" --max-time 15 "${url}" 2>"${errfile}")"; then rc=0; else rc=$?; fi
    err="$(cat "${errfile}")"
    rm -f "${errfile}"
    result "${label}" "${kind}" "${rc}" "$(($(now_ms) - start))" "${http:--}" "${err}"
}

probe_ssh() {
    local label="$1" host="$2" port="$3" start rc out
    start="$(now_ms)"
    if out="$(ssh -o BatchMode=yes -o ConnectTimeout="${CONNECT_TIMEOUT}" -o StrictHostKeyChecking=no \
        -o UserKnownHostsFile=/dev/null -o PubkeyAuthentication=no -o PasswordAuthentication=no \
        -p "${port}" "netprobe@${host}" true 2>&1)"; then rc=0; else rc=$?; fi
    result "${label}" ssh "${rc}" "$(($(now_ms) - start))" - "$(awk '!/^Warning: Permanently added/' <<<"${out}")"
}

probe_nc() {
    local label="$1" host="$2" port="$3" start rc out
    start="$(now_ms)"
    if out="$(nc -zv -w "${CONNECT_TIMEOUT}" "${host}" "${port}" 2>&1)"; then rc=0; else rc=$?; fi
    result "${label}" nc "${rc}" "$(($(now_ms) - start))" - "${out}"
}

probe_dns() {
    local label="$1" name="$2" start rc out
    start="$(now_ms)"
    if out="$(getent ahosts "${name}" 2>&1)"; then rc=0; else rc=$?; fi
    result "${label}" dns "${rc}" "$(($(now_ms) - start))" - "${out}"
}

status=0
for target in "$@"; do
    IFS='|' read -r kind label a b <<<"${target}"
    case "${kind}" in
        facts) show_facts ;;
        http) probe_curl "${label}" http "http://$(bracket "${a}"):${b}/" ;;
        url) probe_curl "${label}" url "${a}" ;;
        ssh) probe_ssh "${label}" "${a}" "${b}" ;;
        nc) probe_nc "${label}" "${a}" "${b}" ;;
        dns) probe_dns "${label}" "${a}" ;;
        *)
            printf 'netprobe-inside: unknown target kind in %s\n' "${target}" >&2
            status=64
            ;;
    esac
done
exit "${status}"
