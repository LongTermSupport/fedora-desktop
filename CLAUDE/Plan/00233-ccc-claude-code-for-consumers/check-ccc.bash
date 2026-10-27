#!/usr/bin/env bash
# Plan 00233 — check-ccc.bash: one acceptance check of the deployed ccc, named by its first
# argument. Read-only; acceptance.bash runs each as a gather leg. Exit 0 pass, 1 fail.
set -euo pipefail

repoRoot="$1"
check="$2"
ccc="${HOME}/.local/bin/ccc"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

case "${check}" in
help)
    out="$("${ccc}" --help)" || fail "ccc --help exited non-zero"
    [[ "${out}" == *"usage: ccc"* ]] || fail "ccc --help printed no usage: ${out}"
    ;;
built-with-version)
    # The version check comes before the terminal check, so a build without the image version
    # names the play instead of the terminal.
    rc=0
    err="$("${ccc}" </dev/null 2>&1 >/dev/null)" || rc=$?
    [[ "${rc}" -eq 1 ]] || fail "ccc with no terminal exited ${rc}, not 1"
    [[ "${err}" == *"needs a terminal"* ]] || fail "ccc with no terminal said: ${err}"
    ;;
image)
    want="$(grep -oP '^REQUIRED_CONTAINER_VERSION="\K[0-9.]+' "${repoRoot}/files/var/local/claude-yolo/claude-yolo")"
    have="$(podman image inspect --format '{{ index .Config.Labels "claude-yolo-version" }}' claude-yolo:latest)" ||
        fail "podman has no claude-yolo:latest"
    [[ "$(printf '%s\n%s\n' "${want}" "${have}" | sort -V | head -n1)" == "${want}" ]] ||
        fail "the image is ${have}, older than the required ${want}"
    ;;
private-dirs)
    for dir in "${HOME}/.claude-tokens/ccy/tokens" "${HOME}/.local/share/ccc/claude"; do
        [[ "$(stat -c %a "${dir}")" == 700 ]] || fail "${dir} is not 0700"
    done
    ;;
desktop-entry)
    entry="${HOME}/.local/share/applications/ccc.desktop"
    grep -qx "Exec=${ccc}" "${entry}" || fail "${entry} does not run ${ccc}"
    grep -qx 'Terminal=true' "${entry}" || fail "${entry} does not open a terminal"
    desktop-file-validate "${entry}" || fail "${entry} does not validate"
    ;;
podman-for-everyone)
    podman info --format '{{ .Host.Security.Rootless }}' | grep -qx true || fail "podman is not rootless here"
    ;;
*)
    fail "unknown check ${check}"
    ;;
esac
printf 'PASS: %s\n' "${check}"
