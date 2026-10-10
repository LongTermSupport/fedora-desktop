#!/usr/bin/env bash
# unifi-api.bash — read-only client for the local UniFi Network controller's API.
#
# Logs in as unifi_admin_name with unifi_admin_password, read from the vault by
# vault.bash, so the password never appears on screen or in argv. stdout is the JSON
# response (the payload); diagnostics go to stderr. Every x_* field (passphrases, keys)
# is removed before printing.
#
# WHERE TO RUN: on the HOST, with the controller running (`unifi-controller start`).
#
# Usage: unifi-api.bash <path>          GET /api/s/<site>/<path>, e.g. stat/device
#        unifi-api.bash --site S <path>  another site than "default"
#
# Paths worth knowing: stat/device (APs, switches, firmware, radios, ports), stat/sta
# (clients), rest/wlanconf (WiFi networks), rest/setting (site settings). The old event
# and alarm lists (stat/event, stat/alarm) return 404 on Network 10.x.
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

CONTROLLER_URL="https://localhost:8443"
ADMIN_NAME="unifi-admin"
site="default"

if [[ "${1:-}" == "--site" ]]; then
    [[ $# -ge 2 ]] || { printf 'usage: unifi-api.bash [--site S] <path>\n' >&2; exit 64; }
    site="$2"
    shift 2
fi
[[ $# -eq 1 ]] || { printf 'usage: unifi-api.bash [--site S] <path>\n' >&2; exit 64; }
apiPath="${1#/}"

for tool in curl jq; do
    command -v "${tool}" >/dev/null || { printf '[FATAL] %s not found — an IaC gap, not something to skip\n' "${tool}" >&2; exit 1; }
done

cookieDir="$(mktemp -d)"
trap 'rm -rf "${cookieDir}"' EXIT
cookieJar="${cookieDir}/cookies"

# The password goes vault -> jq -> curl's stdin, and the response body is kept for the
# error message only: the login reply carries no secret.
loginBody="${cookieDir}/login.json"
loginStatus="$("${repoRoot}/vault.bash" get unifi_admin_password \
    | jq -Rs --arg user "${ADMIN_NAME}" '{username: $user, password: (. | rtrimstr("\n"))}' \
    | curl -sk -c "${cookieJar}" -o "${loginBody}" -w '%{http_code}' \
        -H 'Content-Type: application/json' --data-binary @- "${CONTROLLER_URL}/api/login")"
if [[ "${loginStatus}" != "200" ]]; then
    printf '[FATAL] login to %s as %s returned HTTP %s: %s\n' "${CONTROLLER_URL}" "${ADMIN_NAME}" \
        "${loginStatus}" "$(cat "${loginBody}")" >&2
    printf '        Is the controller running (unifi-controller status), and has play-unifi-controller.yml applied the login?\n' >&2
    exit 1
fi

response="${cookieDir}/response.json"
status="$(curl -sk -b "${cookieJar}" -o "${response}" -w '%{http_code}' "${CONTROLLER_URL}/api/s/${site}/${apiPath}")"
if [[ "${status}" != "200" ]]; then
    printf '[FATAL] GET /api/s/%s/%s returned HTTP %s\n' "${site}" "${apiPath}" "${status}" >&2
    exit 1
fi

jq 'walk(if type == "object" then with_entries(select(.key | startswith("x_") | not)) else . end)' "${response}"
