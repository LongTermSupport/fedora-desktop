#!/usr/bin/env bash
# Plan 00144 — acceptance.bash: the pass/fail gate for the single panel icon's HOST state.
#
# Run on the HOST, in the graphical session, AFTER deploy.bash and a logout/login. Read-only.
#
# It checks what a script can establish: the retired extension is gone from enabled-extensions
# and from disk, the panel is enabled and deployed with the container section's files, and the
# watchdog timer is still active. It prints COVERAGE: n of m and names what it CANNOT establish
# (the logout having happened, and the visual result), so a green run is never read as more
# than it proves.
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

Checks, on the HOST after deploy.bash and a logout/login: the retired container-watch
extension is absent from enabled-extensions and from disk; the fedora-desktop panel is
enabled and deployed with containerReport.js and sections/containers.js; the container-watch
timer is active. Prints COVERAGE: n of m and what it cannot establish."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads the host session's dconf, extensions directory and systemd --user manager"

plan_start_log auto

panelUuid="fedora-desktop@fedora-desktop"
oldUuid="container-watch@fedora-desktop"
extensionsDir="${HOME}/.local/share/gnome-shell/extensions"

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

enabledRaw="$(gsettings get org.gnome.shell enabled-extensions)"

case "${enabledRaw}" in
    *"'${oldUuid}'"*) check "${oldUuid} is absent from enabled-extensions" no ;;
    *) check "${oldUuid} is absent from enabled-extensions" yes ;;
esac

if [[ -e "${extensionsDir}/${oldUuid}" ]]; then
    check "${oldUuid} is absent from ${extensionsDir}" no
else
    check "${oldUuid} is absent from ${extensionsDir}" yes
fi

case "${enabledRaw}" in
    *"'${panelUuid}'"*) check "${panelUuid} is in enabled-extensions" yes ;;
    *) check "${panelUuid} is in enabled-extensions" no ;;
esac

for file in metadata.json extension.js containerReport.js sections/containers.js; do
    if [[ -f "${extensionsDir}/${panelUuid}/${file}" ]]; then
        check "${panelUuid} is deployed with ${file}" yes
    else
        check "${panelUuid} is deployed with ${file}" no
    fi
done

# A non-zero `is-active` is the answer being checked, so the exit status is inspected.
if timerState="$(systemctl --user is-active container-watch.timer 2>&1)"; then
    check "container-watch.timer is active (${timerState})" yes
else
    check "container-watch.timer is active (got: ${timerState})" no
fi

printf '\nCOVERAGE: %d of %d established checks passed\n' "${passed}" "${total}"
printf '\nNOT ESTABLISHABLE by this script (a human must confirm, Task 7.3):\n'
printf '  - that you logged out and back in after deploy.bash (the new panel code loads only then)\n'
printf '  - that exactly one icon shows, and that it turns amber for an injected finding\n'

if [[ "${failed}" -gt 0 ]]; then
    printf '\n==> %d check(s) FAILED\n' "${failed}" >&2
    exit 1
fi
printf '\n==> all established checks passed\n'
