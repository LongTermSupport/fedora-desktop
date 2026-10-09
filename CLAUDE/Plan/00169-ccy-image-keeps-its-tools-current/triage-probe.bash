#!/usr/bin/env bash
# Plan 00169 — triage-probe.bash: the probes behind triage.bash, one fact group per call.
#
# Usage: triage-probe.bash <probe> <repo-root> <report-file>
#   host-git      the host's git version
#   extensions    the repository's core.repositoryformatversion and every extensions.* key
#   images        each local claude-yolo image: its git version and claude-yolo-version label
#   base-digest   the local node:lts-slim against the registry's current digest for that tag,
#                 and whether each claude-yolo image sits on the local node:lts-slim's layers
#   build-pull    whether the deployed launcher and the play pass --pull to their builds
#
# Read-only: it inspects images and runs `git --version` in throwaway --rm containers. It
# pulls nothing (the registry is asked for a digest with a HEAD request, which is not a pull).
# Each fact goes to stdout and is appended to the report. A probe that cannot establish its
# fact exits non-zero, so triage.bash names the leg.
set -euo pipefail

probe="${1:?usage: triage-probe.bash <probe> <repo-root> <report-file>}"
repoRoot="${2:?usage: triage-probe.bash <probe> <repo-root> <report-file>}"
report="${3:?usage: triage-probe.bash <probe> <repo-root> <report-file>}"

readonly NODE_TAG="node:lts-slim"
readonly NODE_REPO="library/node"
readonly NODE_REF="lts-slim"
readonly DEPLOYED_LIB="/var/local/claude-yolo/lib/common.bash"
readonly DEPLOYED_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly PLAY="playbooks/imports/play-claude-yolo.yml"

# fact <text> — one line of the report, on stdout too.
fact() {
    printf '%s\n' "$1" | tee -a "${report}"
}

# ccy_images — every local claude-yolo image as repository:tag, one per line.
ccy_images() {
    podman images --filter reference='claude-yolo' --format '{{.Repository}}:{{.Tag}}'
}

probe_host_git() {
    local version
    version="$(git --version)"
    fact "- host git: ${version}"
}

probe_extensions() {
    local format keys status
    format="$(git -C "${repoRoot}" config --get core.repositoryformatversion)"
    fact "- core.repositoryformatversion: ${format}"
    # `git config --get-regexp` exits 1 when no key matches, which is a fact here, not a fault.
    if keys="$(git -C "${repoRoot}" config --get-regexp '^extensions\.')"; then
        fact "- extensions.* keys:"
        while IFS= read -r line; do
            fact "  - \`${line}\`"
        done <<<"${keys}"
        return 0
    else
        status=$?
    fi
    if [[ "${status}" -eq 1 ]]; then
        fact "- extensions.* keys: none"
        return 0
    fi
    printf '[FAIL] git config --get-regexp exited %s on %s\n' "${status}" "${repoRoot}" >&2
    return 1
}

probe_images() {
    local images image label git_out failed=0
    images="$(ccy_images)"
    if [[ -z "${images}" ]]; then
        fact "- claude-yolo images: none"
        return 0
    fi
    while IFS= read -r image; do
        label="$(podman image inspect --format '{{index .Labels "claude-yolo-version"}}' "${image}")"
        if git_out="$(podman run --rm --network none --entrypoint git "${image}" --version 2>&1)"; then
            fact "- ${image}: claude-yolo-version ${label:-(none)}, ${git_out}"
        else
            fact "- ${image}: claude-yolo-version ${label:-(none)}, git --version FAILED: ${git_out}"
            failed=1
        fi
    done <<<"${images}"
    return "${failed}"
}

probe_base_digest() {
    local local_digests token registry_digest node_layers images image image_layers
    if ! local_digests="$(podman image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "${NODE_TAG}" 2>&1)"; then
        fact "- local ${NODE_TAG}: not present (${local_digests})"
        local_digests=""
    else
        fact "- local ${NODE_TAG} repo digests:"
        while IFS= read -r line; do
            if [[ -n "${line}" ]]; then
                fact "  - ${line}"
            fi
        done <<<"${local_digests}"
        fact "- local ${NODE_TAG} created: $(podman image inspect --format '{{.Created}}' "${NODE_TAG}")"
    fi

    token="$(curl -fsS --max-time 20 \
        "https://auth.docker.io/token?service=registry.docker.io&scope=repository:${NODE_REPO}:pull" \
        | jq -er '.token')"
    registry_digest="$(curl -fsS --max-time 20 --head \
        -H "Authorization: Bearer ${token}" \
        -H 'Accept: application/vnd.oci.image.index.v1+json' \
        -H 'Accept: application/vnd.docker.distribution.manifest.list.v2+json' \
        "https://registry-1.docker.io/v2/${NODE_REPO}/manifests/${NODE_REF}" \
        | awk 'tolower($1) == "docker-content-digest:" { gsub(/\r/, "", $2); print $2 }')"
    if [[ -z "${registry_digest}" ]]; then
        printf '[FAIL] the registry answered without a Docker-Content-Digest for %s\n' "${NODE_TAG}" >&2
        return 1
    fi
    fact "- registry ${NODE_TAG} digest now: ${registry_digest}"
    if [[ -n "${local_digests}" ]] && grep -qF "@${registry_digest}" <<<"${local_digests}"; then
        fact "- local ${NODE_TAG} is the registry's current one: yes"
    else
        fact "- local ${NODE_TAG} is the registry's current one: no"
    fi

    [[ -n "${local_digests}" ]] || return 0
    node_layers="$(podman image inspect --format '{{range .RootFS.Layers}}{{println .}}{{end}}' "${NODE_TAG}")"
    images="$(ccy_images)"
    [[ -n "${images}" ]] || return 0
    while IFS= read -r image; do
        image_layers="$(podman image inspect --format '{{range .RootFS.Layers}}{{println .}}{{end}}' "${image}")"
        if [[ "${image_layers}" == "${node_layers}"* ]]; then
            fact "- ${image} is built on the local ${NODE_TAG}'s layers: yes"
        else
            fact "- ${image} is built on the local ${NODE_TAG}'s layers: no"
        fi
    done <<<"${images}"
}

probe_build_pull() {
    local file count status
    for file in "${DEPLOYED_LIB}" "${DEPLOYED_LAUNCHER}" "${repoRoot}/${PLAY}"; do
        # grep -c exits 1 for no match (a count of 0, a fact) and 2 for an error (a fault).
        if count="$(grep -cE -- '--pull\b' "${file}")"; then
            status=0
        else
            status=$?
        fi
        if [[ "${status}" -gt 1 ]]; then
            printf '[FAIL] could not read %s (grep exited %s)\n' "${file}" "${status}" >&2
            return 1
        fi
        fact "- \`--pull\` in ${file}: ${count} line(s)"
    done
}

case "${probe}" in
    host-git) probe_host_git ;;
    extensions) probe_extensions ;;
    images) probe_images ;;
    base-digest) probe_base_digest ;;
    build-pull) probe_build_pull ;;
    *)
        printf '[FATAL] unknown probe: %s\n' "${probe}" >&2
        exit 64
        ;;
esac
