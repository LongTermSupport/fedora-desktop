#!/usr/bin/env bash
# Plan 00151 — acceptance.bash: the pass/fail gate for ccy's clipboard image paste on the HOST.
#
# Run on the HOST, as the desktop user, AFTER deploy.bash. Read-only apart from throwaway
# containers (--rm) started from the deployed image.
#
# It checks what a script can establish: the launcher, its lib/ and the Dockerfile are deployed
# and identical to the checkout's; the claude-yolo:latest image carries the Dockerfile's
# version label; and inside a throwaway container from that image the 5 s wl-paste guard,
# the real wl-paste and wl-copy exist and wl-paste answers over the read-only Wayland socket.
# It prints COVERAGE: n of m and names what it CANNOT establish.
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

Checks, on the HOST after deploy.bash: launcher, lib and Dockerfile deployed and identical to
this checkout's; the claude-yolo:latest image has the Dockerfile's version label; in a
throwaway container the wl-paste guard, wl-paste and wl-copy exist and wl-paste answers over
the Wayland socket."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads the deployed launcher and starts a container from the ccy image"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user: ccy containers are rootless and per user\n' >&2
    exit 1
fi

plan_start_log auto

launcherSource="${repoRoot}/files/var/local/claude-yolo"
launcherDeployed="/var/local/claude-yolo"
dockerfileDeployed="/opt/claude-yolo/Dockerfile"
imageName="claude-yolo:latest"

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

compare() {
    local label="${1}" source="${2}" deployed="${3}"
    if [[ ! -f "${deployed}" ]]; then
        check "${label} is deployed" no
    elif cmp -s "${source}" "${deployed}"; then
        check "${label} is deployed and identical to the checkout" yes
    else
        check "${label} is deployed but DIFFERS from the checkout (re-run deploy.bash)" no
    fi
}

printf '=== launcher, lib and Dockerfile deployed and identical ===\n'
compare "claude-yolo launcher" "${launcherSource}/claude-yolo" "${launcherDeployed}/claude-yolo"
compare "Dockerfile" "${launcherSource}/Dockerfile" "${dockerfileDeployed}"
for lib in "${launcherSource}"/lib/*.bash; do
    compare "lib/$(basename "${lib}")" "${lib}" "${launcherDeployed}/lib/$(basename "${lib}")"
done

printf '=== the image ===\n'
if ! command -v podman > /dev/null; then
    check "podman is installed" no
else
    wantVersion="$(awk -F'"' '/^LABEL claude-yolo-version=/ {print $2}' "${launcherSource}/Dockerfile")"
    requiredVersion="$(awk -F'"' '/^REQUIRED_CONTAINER_VERSION=/ {print $2}' "${launcherSource}/claude-yolo")"
    if [[ -z "${wantVersion}" ]]; then
        check "the Dockerfile has a claude-yolo-version label" no
    else
        if [[ "${wantVersion}" == "${requiredVersion}" ]]; then
            check "the launcher's REQUIRED_CONTAINER_VERSION matches the Dockerfile label (${wantVersion})" yes
        else
            check "the launcher requires '${requiredVersion}' but the Dockerfile label is '${wantVersion}'" no
        fi
        haveVersion=""
        if podman image exists "${imageName}"; then
            haveVersion="$(podman image inspect "${imageName}" \
                --format '{{index .Config.Labels "claude-yolo-version"}}')"
        fi
        if [[ "${haveVersion}" == "${wantVersion}" ]]; then
            check "${imageName} carries claude-yolo-version ${wantVersion}" yes
        else
            check "${imageName} carries claude-yolo-version ${wantVersion} (has: ${haveVersion:-absent}; deploy.bash builds it)" no
        fi
    fi
fi

if [[ "${failed}" -eq 0 ]]; then
    printf '=== inside a throwaway container ===\n'
    # The script runs inside the container, so its expansions are single-quoted on purpose.
    inside="$(podman run --rm --entrypoint /bin/bash "${imageName}" -c \
        'for f in /usr/local/bin/wl-paste /usr/bin/wl-paste /usr/bin/wl-copy; do
             if [[ -x $f ]]; then echo "ok $f"; else echo "missing $f"; fi
         done
         echo "guard $(grep -c "timeout 5" /usr/local/bin/wl-paste)"' 2>&1)" || inside="${inside:-podman run failed}"
    for f in /usr/local/bin/wl-paste /usr/bin/wl-paste /usr/bin/wl-copy; do
        if [[ "${inside}" == *"ok ${f}"* ]]; then
            check "${f} exists and is executable in the image" yes
        else
            check "${f} exists and is executable in the image (got: ${inside//$'\n'/; })" no
        fi
    done
    if [[ "${inside}" == *"guard 1"* ]]; then
        check "the /usr/local/bin/wl-paste wrapper carries the 5 s guard" yes
    else
        check "the /usr/local/bin/wl-paste wrapper carries the 5 s guard" no
    fi

    printf '=== wl-paste answers over the Wayland socket ===\n'
    socket="${XDG_RUNTIME_DIR:-}/${WAYLAND_DISPLAY:-}"
    if [[ -z "${XDG_RUNTIME_DIR:-}" || -z "${WAYLAND_DISPLAY:-}" || ! -S "${socket}" ]]; then
        check "a Wayland socket is available to this session (run from the desktop session)" no
    else
        pasteStatus=0
        pasteOut="$(podman run --rm --entrypoint /bin/bash \
            -v "${socket}:${socket}:ro" -e WAYLAND_DISPLAY -e XDG_RUNTIME_DIR \
            "${imageName}" -c 'wl-paste -l' 2>&1)" || pasteStatus=$?
        # 0 = a clipboard with content; 1 = an empty clipboard, but also what a failed
        # connection returns, so rc 1 counts only without that message. 124 is the guard's
        # timeout: GNOME withheld focus (a locked screen does the same).
        if [[ "${pasteStatus}" -eq 0 ]] \
            || [[ "${pasteStatus}" -eq 1 && "${pasteOut}" != *"Failed to connect"* ]]; then
            check "wl-paste -l is answered by the compositor (rc ${pasteStatus})" yes
        else
            check "wl-paste -l is answered by the compositor (rc ${pasteStatus}; 124 = no answer in 5 s): ${pasteOut}" no
        fi
    fi
fi

printf '\nCOVERAGE: %d of %d established checks passed\n' "${passed}" "${total}"
printf '\nNOT ESTABLISHABLE by this script (a person must confirm):\n'
printf '  - Ctrl+V in a running ccy session attaching a copied image (confirmed once by the owner)\n'
printf '  - project images built from a custom Dockerfile (claude-yolo:<project>): only :latest is checked\n'
printf '  - containers other than ccy (LXC stays a manual recipe, Task 2.2)\n'
printf '  - wl-paste needs the screen unlocked: a locked screen makes check above fail with rc 124\n'

if [[ "${failed}" -gt 0 ]]; then
    printf '\n==> %d check(s) FAILED\n' "${failed}" >&2
    exit 1
fi
printf '\n==> all established checks passed\n'
