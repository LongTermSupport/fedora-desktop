#!/usr/bin/env bash
# probe-host.bash <H1|H2|H3|H4> — one grounded fact about the HOST side of Plan 00092.
#
# Fact-finding only: renders no verdict, changes nothing, safe to re-run. Called as a leg by
# triage.bash, which owns the host guard; also runnable alone for debugging.
#
# Nothing here reads or prints a credential. The container-side confidentiality invariants
# are probe-invariant.bash's job and run inside the container.
set -euo pipefail

CHECK="${1:?usage: probe-host.bash <H1|H2|H3|H4>}"

BUILD_CTX="/opt/claude-yolo"
DEPLOYED_LAUNCHER="/var/local/claude-yolo/claude-yolo"
WRAPPER_REL="optional/child-claude/bin/ccy-claude"
SKILL_REL="optional/child-claude/skills/child-claude/SKILL.md"

# repo_root — the checkout this script lives in, resolved script-relative and bounded at the
# repository boundary. Never `git rev-parse`, which answers about the cwd: Plan 00068's
# triage did exactly that, was run by path from another repo, and compared against a path
# that does not exist there. See CLAUDE/PlanScriptStandards.md R1.
repo_root() {
    local d
    d="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
    while [[ "${d}" != "/" ]] && [[ ! -e "${d}/ansible.cfg" ]]; do
        if [[ -e "${d}/.git" ]]; then
            printf '[FAIL] walked to the repository boundary without finding ansible.cfg\n' >&2
            return 1
        fi
        d="$(dirname "${d}")"
    done
    if [[ ! -e "${d}/ansible.cfg" ]]; then
        printf '[FAIL] no ansible.cfg above this script; not inside a fedora-desktop checkout\n' >&2
        return 1
    fi
    printf '%s' "${d}"
}

# read_pinned <file> <variable-or-label> — pull a version out of the launcher or Dockerfile
# without sourcing either. `|| value=""` is the explicit-fallback form, not error hiding:
# grep exits 1 when the line is absent, and under pipefail that would kill the script at the
# assignment, making the "not found" report below unreachable.
read_pinned() {
    local file="$1" pattern="$2" value=""
    if [[ ! -r "${file}" ]]; then
        printf 'UNREADABLE'
        return 0
    fi
    value="$(grep -m1 -oE "${pattern}" "${file}" | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?')" || value=""
    if [[ -z "${value}" ]]; then
        printf 'NOT-FOUND'
        return 0
    fi
    printf '%s' "${value}"
}

# ── H1: is the opt-in tree staged in the image build context? ─────────────────────────────
probe_H1() {
    local missing=0 p
    printf '== H1: the opt-in tree is present in the Docker build context\n'
    printf '   build context: %s\n' "${BUILD_CTX}"
    if [[ ! -d "${BUILD_CTX}" ]]; then
        printf '[FAIL] %s does not exist, so play-claude-yolo.yml has never run here.\n' \
            "${BUILD_CTX}" >&2
        return 1
    fi
    for p in "${WRAPPER_REL}" "${SKILL_REL}"; do
        if [[ -e "${BUILD_CTX}/${p}" ]]; then
            printf '   present: %s\n' "${p}"
        else
            printf '[FAIL] missing from the build context: %s\n' "${BUILD_CTX}/${p}" >&2
            missing=1
        fi
    done
    if [[ -e "${BUILD_CTX}/${WRAPPER_REL}" ]] && [[ ! -x "${BUILD_CTX}/${WRAPPER_REL}" ]]; then
        printf '[FAIL] the wrapper is present but not executable in the build context\n' >&2
        missing=1
    fi
    [[ "${missing}" -eq 0 ]] || return 1
    printf 'the build context carries both artefacts, wrapper executable\n'
}

# ── H2: do the two coupled version values agree, in the checkout? ─────────────────────────
#
# They live in different files and MUST match, or users get an infinite rebuild loop. That is
# stated in both files and enforced by nothing, so it is checked here.
probe_H2() {
    local root dockerfileVer launcherVer
    printf '== H2: Dockerfile label and REQUIRED_CONTAINER_VERSION agree in the checkout\n'
    root="$(repo_root)"
    dockerfileVer="$(read_pinned "${root}/files/var/local/claude-yolo/Dockerfile" \
        'LABEL claude-yolo-version="[0-9.]+"')"
    launcherVer="$(read_pinned "${root}/files/var/local/claude-yolo/claude-yolo" \
        'REQUIRED_CONTAINER_VERSION="[0-9.]+"')"
    printf '   Dockerfile label:            %s\n' "${dockerfileVer}"
    printf '   REQUIRED_CONTAINER_VERSION:  %s\n' "${launcherVer}"
    case "${dockerfileVer}" in
        UNREADABLE | NOT-FOUND)
            printf '[FAIL] could not read the Dockerfile label, so nothing was proved.\n' >&2
            return 1
            ;;
    esac
    case "${launcherVer}" in
        UNREADABLE | NOT-FOUND)
            printf '[FAIL] could not read REQUIRED_CONTAINER_VERSION, so nothing was proved.\n' >&2
            return 1
            ;;
    esac
    if [[ "${dockerfileVer}" != "${launcherVer}" ]]; then
        printf '[FAIL] they disagree. Users get an infinite rebuild loop until they match.\n' >&2
        return 1
    fi
    printf 'both at %s\n' "${dockerfileVer}"
}

# ── H3: is the deployed launcher the one in the checkout? ─────────────────────────────────
probe_H3() {
    local root deployedVer checkoutVer
    printf '== H3: the deployed launcher matches the checkout\n'
    root="$(repo_root)"
    checkoutVer="$(read_pinned "${root}/files/var/local/claude-yolo/claude-yolo" \
        'CCY_VERSION="[0-9.]+"')"
    deployedVer="$(read_pinned "${DEPLOYED_LAUNCHER}" 'CCY_VERSION="[0-9.]+"')"
    printf '   checkout CCY_VERSION:  %s\n' "${checkoutVer}"
    printf '   deployed CCY_VERSION:  %s  (%s)\n' "${deployedVer}" "${DEPLOYED_LAUNCHER}"
    if [[ "${deployedVer}" == "UNREADABLE" ]]; then
        printf '[FAIL] the deployed launcher is absent or unreadable — run the playbook.\n' >&2
        return 1
    fi
    if [[ "${checkoutVer}" != "${deployedVer}" ]]; then
        printf '[FAIL] deployed launcher is stale. Run deploy.bash in this plan folder.\n' >&2
        return 1
    fi
    printf 'deployed launcher is current at %s\n' "${checkoutVer}"
}

# ── H4: does the built image carry the version the launcher now requires? ─────────────────
probe_H4() {
    local root required label engine="" rc=0
    printf '== H4: the built image carries the required container version\n'
    root="$(repo_root)"
    required="$(read_pinned "${root}/files/var/local/claude-yolo/claude-yolo" \
        'REQUIRED_CONTAINER_VERSION="[0-9.]+"')"
    printf '   required by the launcher: %s\n' "${required}"

    # Podman first, per CLAUDE/ContainerEngines.md. A missing engine FAILS the leg rather
    # than skipping: absence of a check is not a passing check.
    if command -v podman >/dev/null; then
        engine=podman
    elif command -v docker >/dev/null; then
        engine=docker
    else
        printf '[FAIL] neither podman nor docker is on PATH, so the image cannot be inspected.\n' >&2
        return 1
    fi
    printf '   engine: %s\n' "${engine}"

    label="$("${engine}" image inspect claude-yolo \
        --format '{{index .Config.Labels "claude-yolo-version"}}')" || rc=$?
    if [[ "${rc}" -ne 0 ]] || [[ -z "${label}" ]]; then
        printf '   no claude-yolo image built yet, or it carries no version label.\n'
        printf '[FAIL] nothing to compare against — build it with: ccy --rebuild\n' >&2
        return 1
    fi
    printf '   image label:              %s\n' "${label}"
    if [[ "${label}" != "${required}" ]]; then
        printf '[FAIL] the image predates the launcher requirement. Run: ccy --rebuild\n' >&2
        return 1
    fi
    printf 'image is current at %s, no rebuild pending\n' "${label}"
}

case "${CHECK}" in
    H1) probe_H1 ;;
    H2) probe_H2 ;;
    H3) probe_H3 ;;
    H4) probe_H4 ;;
    *)
        printf '[FATAL] unknown check %s (expected H1..H4)\n' "${CHECK}" >&2
        exit 1
        ;;
esac
