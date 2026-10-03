#!/usr/bin/env bash
# Plan 00151 — prototype-ccy-wl-paste.bash: does wl-paste inside a ccy container read an
# image from the host clipboard on GNOME? (RESEARCH-survey.md section 7, option A.)
#
# Run on the HOST, in a Wayland session, once. It builds a throwaway image FROM the local
# claude-yolo:latest with wl-clipboard added, runs wl-paste in it over the same read-only
# socket mount the ccy launcher uses, prints what came back, and removes the image again.
# It changes nothing else.
#
# Watch the screen while it runs: whether a small window flashes, and whether focus returns
# to this terminal, is the answer the script cannot see. Exit code 124 from wl-paste means
# GNOME never gave its window focus (the hang case).
#
# Usage: ./prototype-ccy-wl-paste.bash [-h|--help]
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

PLAN_USAGE="usage: prototype-ccy-wl-paste.bash [-h|--help]

On the HOST in a Wayland session: builds a throwaway image from claude-yolo:latest plus
wl-clipboard, asks you to copy an image, runs wl-paste in a container over the Wayland
socket, prints the result, then removes the image."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it talks to the host's Wayland compositor and builds a local image"

PROTO_IMAGE="ccy-wlpaste-proto"
BASE_IMAGE="claude-yolo:latest"
socket="${XDG_RUNTIME_DIR:-}/${WAYLAND_DISPLAY:-}"

if [[ -z "${WAYLAND_DISPLAY:-}" ]] || [[ ! -S "${socket}" ]]; then
    printf '[FATAL] no Wayland socket at %s: run this from a terminal in your GNOME Wayland session\n' "${socket}" >&2
    exit 1
fi
if ! podman image exists "${BASE_IMAGE}"; then
    printf '[FATAL] %s is not built on this host: start ccy once so it builds, then re-run\n' "${BASE_IMAGE}" >&2
    exit 1
fi

plan_start_log auto

trap 'if podman image exists "${PROTO_IMAGE}"; then podman rmi "${PROTO_IMAGE}" > /dev/null && printf "==> removed the throwaway image %s\n" "${PROTO_IMAGE}"; fi' EXIT

if command -v getenforce > /dev/null; then
    printf '==> SELinux mode: %s\n' "$(getenforce)"
else
    printf '==> SELinux mode: getenforce not found\n'
fi

containerfile="${PLAN_RUN_DIR}/Containerfile"
printf 'FROM %s\nUSER root\nRUN apt-get update && apt-get install -y --no-install-recommends wl-clipboard\n' \
    "${BASE_IMAGE}" > "${containerfile}"
plan_deploy_leg "build ${PROTO_IMAGE} (wl-clipboard on top of ${BASE_IMAGE})" \
    podman build -t "${PROTO_IMAGE}" -f "${containerfile}" "${PLAN_RUN_DIR}"

plan_confirm "Copy an IMAGE to the clipboard now (e.g. a screenshot: Print, then copy). Then watch the screen while the next step runs." "ready"

# The same socket flags the ccy launcher passes: the socket alone, read-only. The ccy
# entrypoint is bypassed: it sets up a full agent session and needs GH_TOKEN, none of which
# this probe uses.
plan_deploy_leg "wl-paste inside the container" \
    podman run --rm \
    -v "${socket}:${socket}:ro" \
    -e "WAYLAND_DISPLAY=${WAYLAND_DISPLAY}" \
    -e "XDG_RUNTIME_DIR=${XDG_RUNTIME_DIR}" \
    --entrypoint /bin/bash \
    "${PROTO_IMAGE}" -c "
        set -uo pipefail
        echo '--- wl-paste -l (types offered):'
        timeout 10 wl-paste -l
        echo \"list rc=\$?\"
        echo '--- wl-paste --type image/png:'
        timeout 10 wl-paste --type image/png > /tmp/clip.png
        rc=\$?
        echo \"png rc=\${rc} bytes=\$(wc -c < /tmp/clip.png)\"
    "

printf '\nREAD THE RESULT:\n'
printf '  png rc=0 and bytes > 0       the container read the image: option A works\n'
printf '  rc=124                        GNOME withheld focus: option A hangs; fall back to D\n'
printf '  any other rc                  read the error above\n'
printf 'And tell the agent: did a small window flash, and did focus come back to this terminal?\n'

plan_finish
