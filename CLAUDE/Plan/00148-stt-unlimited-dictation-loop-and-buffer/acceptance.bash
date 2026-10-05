#!/usr/bin/env bash
# Plan 00148 — acceptance.bash: the pass/fail gate for the speech-to-text HOST state.
#
# Run on the HOST, as the desktop user, AFTER deploy.bash and a logout/login. Read-only.
#
# It checks what a script can establish: every script and extension file this plan changed is
# deployed and identical to the checkout's; the settings the recorders read are readable and
# well-formed; the model resolver answers; the start-at-login unit is enabled; and the deployed
# wsi-article loads the deployed wsi-stream. It prints COVERAGE: n of m and names what it
# CANNOT establish (the logout, and anything that needs a person at the microphone), so a
# green run is never read as more than it proves.
#
# Usage: ./acceptance.bash [-h|--help]
#
# EXIT CODES: 0 every established check passed; 1 at least one failed; 64 usage error.
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

PLAN_USAGE="usage: acceptance.bash [-h|--help]

Checks, on the HOST after deploy.bash and a logout/login: the recorders, helpers and
extension files are deployed and identical to this checkout's; the settings the recorders
read are readable and well-formed; the model resolver answers; the start-at-login unit is
enabled; the deployed wsi-article loads the deployed wsi-stream. Prints COVERAGE: n of m and
what it cannot establish."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads the host's deployed scripts, GSettings and systemd --user manager"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: the settings and units are per user\n' >&2
    exit 1
fi

plan_start_log auto

binDir="${HOME}/.local/bin"
extUuid="speech-to-text@fedora-desktop"
extDeployed="${HOME}/.local/share/gnome-shell/extensions/${extUuid}"
extSource="${repoRoot}/extensions/${extUuid}"

total=0
passed=0
failed=0

check() {
    local label="${1}" ok="${2}"
    total=$((total + 1))
    if [[ "${ok}" == "yes" ]]; then
        passed=$((passed + 1))
        printf '  PASS: %s\n' "${label}"
    else
        failed=$((failed + 1))
        printf '  FAIL: %s\n' "${label}"
    fi
}

printf '=== scripts deployed and identical to the checkout ===\n'
for script in wsi wsi-stream wsi-stream-server wsi-article wsi-article-window \
    wsi-stop-grace wsi-setting wsi-resolve-model; do
    deployed="${binDir}/${script}"
    checkoutCopy="${repoRoot}/files/home/.local/bin/${script}"
    if [[ ! -x "${deployed}" ]]; then
        check "${script} is deployed and executable" no
    elif cmp -s "${checkoutCopy}" "${deployed}"; then
        check "${script} is deployed and identical to the checkout" yes
    else
        check "${script} is deployed but DIFFERS from the checkout (re-run deploy.bash)" no
    fi
done

printf '=== extension files deployed and identical ===\n'
for file in extension.js focusOutline.js prefs.js metadata.json \
    schemas/org.gnome.shell.extensions.speech-to-text.gschema.xml; do
    if [[ ! -f "${extDeployed}/${file}" ]]; then
        check "${file} is deployed" no
    elif cmp -s "${extSource}/${file}" "${extDeployed}/${file}"; then
        check "${file} is deployed and identical" yes
    else
        check "${file} is deployed but DIFFERS from the checkout" no
    fi
done

printf '=== settings the recorders read ===\n'
# wsi-setting is the reader the recorders use; a non-zero exit is the answer being checked.
for key in stop-grace-seconds server-idle-timeout-minutes max-recording-minutes \
    silence-autostop-seconds dictation-paste-interval-seconds; do
    if value="$("${binDir}/wsi-setting" "${key}" 2>&1)" && [[ "${value}" =~ ^[0-9]+$ ]]; then
        check "${key} reads as a whole number (${value})" yes
    else
        check "${key} reads as a whole number (got: ${value:-nothing})" no
    fi
done
for key in continuous-dictation server-start-at-login; do
    if value="$("${binDir}/wsi-setting" "${key}" 2>&1)" && [[ "${value}" == "true" || "${value}" == "false" ]]; then
        check "${key} reads as true or false (${value})" yes
    else
        check "${key} reads as true or false (got: ${value:-nothing})" no
    fi
done

printf '=== model resolver and unit ===\n'
if value="$("${binDir}/wsi-resolve-model" --gpu-count 2>&1)" && [[ "${value}" =~ ^[0-9]+$ ]]; then
    check "wsi-resolve-model --gpu-count answers (${value})" yes
else
    check "wsi-resolve-model --gpu-count answers (got: ${value:-nothing})" no
fi
if value="$("${binDir}/wsi-resolve-model" --mode streaming --language en 2>&1)" && [[ -n "${value}" ]]; then
    check "wsi-resolve-model picks a streaming model (${value})" yes
else
    check "wsi-resolve-model picks a streaming model (got: ${value:-nothing})" no
fi
if unitState="$(systemctl --user is-enabled wsi-stream-server-at-login.service 2>&1)"; then
    check "wsi-stream-server-at-login.service is enabled (${unitState})" yes
else
    check "wsi-stream-server-at-login.service is enabled (got: ${unitState})" no
fi

printf '=== the deployed pair works together ===\n'
# --help runs after wsi-article has loaded the deployed wsi-stream, so a mismatch between the
# two (a helper one has and the other lacks) fails here and not at the microphone.
if helpOut="$("${binDir}/wsi-article" --help 2>&1)" && [[ "${helpOut}" == *"usage:"* ]]; then
    check "the deployed wsi-article loads the deployed wsi-stream" yes
else
    check "the deployed wsi-article loads the deployed wsi-stream (got: ${helpOut:-nothing})" no
fi

printf '\nCOVERAGE: %d of %d established checks passed\n' "${passed}" "${total}"
printf '\nNOT ESTABLISHABLE by this script (a person must confirm, Task 6.2):\n'
printf '  - that you logged out and back in after deploy.bash (the new extension code loads only then)\n'
printf '  - a dictation past five minutes with Continuous Dictation on, and its text arriving whole\n'
printf '  - a forced segment failure: the text so far on the clipboard and nothing pasted\n'
printf '  - Insert stopping after the grace, the STOPPING icon, Escape discarding\n'
printf '  - article mode in its window: text appearing, Stop draining the last words\n'

if [[ "${failed}" -gt 0 ]]; then
    printf '\n==> %d check(s) FAILED\n' "${failed}" >&2
    exit 1
fi
printf '\n==> all established checks passed\n'
