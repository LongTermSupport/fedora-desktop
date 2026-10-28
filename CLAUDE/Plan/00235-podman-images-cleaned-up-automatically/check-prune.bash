#!/usr/bin/env bash
# Plan 00235 — check-prune.bash <check>: one acceptance check of the daily prune, run by
# acceptance.bash as a gather leg. Read-only: it asks systemd and podman, and removes nothing.
# Exit 0 the check passed, 1 it failed (the reason on stderr), 64 usage.
#
# The image checks ask podman directly, never container-images itself, so the command under
# test does not judge its own work.
set -euo pipefail

unit_prop() {
    systemctl --user show "$1" --property "$2" --value
}

case "${1:-}" in
    timer)
        state="$(systemctl --user is-enabled container-images-prune.timer)" || {
            printf 'container-images-prune.timer is not enabled (%s)\n' "${state}" >&2
            exit 1
        }
        next="$(unit_prop container-images-prune.timer NextElapseUSecRealtime)"
        printf 'timer %s, next run: %s\n' "${state}" "${next:-none}"
        [[ -n "${next}" ]] || {
            printf 'the timer has no next run\n' >&2
            exit 1
        }
        ;;
    last-run)
        started="$(unit_prop container-images-prune.service ExecMainStartTimestamp)"
        result="$(unit_prop container-images-prune.service Result)"
        status="$(unit_prop container-images-prune.service ExecMainStatus)"
        printf 'last run started: %s, result: %s, exit status: %s\n' "${started:-never}" "${result}" "${status}"
        [[ -n "${started}" && "${result}" == success && "${status}" == 0 ]] || {
            printf 'the prune service has not run, or its last run failed: journalctl --user -u container-images-prune.service --no-pager\n' >&2
            exit 1
        }
        log="${HOME}/.local/state/fedora-desktop/container-images.log"
        [[ -f "${log}" ]] || {
            printf 'no removal log at %s: a --yes run opens it before anything else\n' "${log}" >&2
            exit 1
        }
        printf 'removal log: %s lines\n' "$(wc -l <"${log}")"
        ;;
    no-old-unused-dangling)
        used="$(podman ps --all --external --no-trunc --format '{{.ImageID}}')"
        old="$(podman images --no-trunc --filter dangling=true --filter until=24h --format '{{.ID}}')"
        left=0
        while read -r id; do
            [[ -n "${id}" ]] || continue
            id="${id#sha256:}"
            if ! grep -qxF "${id}" <<<"${used}"; then
                printf 'dangling, over a day old, used by no container: %s\n' "${id}"
                left=$((left + 1))
            fi
        done <<<"${old}"
        printf 'unused dangling images over a day old: %d\n' "${left}"
        [[ "${left}" -eq 0 ]]
        ;;
    containers-keep-images)
        missing=0
        containers="$(podman ps --all --no-trunc --format '{{.Names}} {{.ImageID}}')"
        while read -r name id; do
            [[ -n "${id}" ]] || continue
            if ! podman image exists "${id}"; then
                printf 'container %s has lost its image %s\n' "${name}" "${id}" >&2
                missing=$((missing + 1))
            fi
        done <<<"${containers}"
        printf 'containers without their image: %d\n' "${missing}"
        [[ "${missing}" -eq 0 ]]
        ;;
    ccy-images)
        for image in claude-yolo:base claude-yolo:latest; do
            podman image exists "${image}" || {
                printf '%s is gone\n' "${image}" >&2
                exit 1
            }
            printf '%s exists\n' "${image}"
        done
        ;;
    free-space)
        df -h / /home
        ;;
    *)
        printf 'usage: check-prune.bash timer|last-run|no-old-unused-dangling|containers-keep-images|ccy-images|free-space\n' >&2
        exit 64
        ;;
esac
