#!/usr/bin/bash
# guest-acceptance-server.bash — the assertion set for a SERVER-profile guest,
# run INSIDE the guest after run.bash has finished (Plan 00110, DESIGN.md §9 T3.3).
#
# Shared by both server bases (server-fast and server-full); which one ran is
# recorded by the host from base.json, not decided here, so this script cannot
# conflate them.
#
# Contract (helpers/vmtest/transcript.py parses exactly these lines):
#   VMTEST-CHECK-PLANNED <n>                declared BEFORE the first check
#   VMTEST-CHECK pass|fail|skip <name> [detail]
#   VMTEST-EVIDENCE <key>=<value>
#   VMTEST-CHECKS-DONE total=N passed=N failed=N skipped=N
#
# `planned` is fixed by the list of checks below and must equal the scenario's
# `planned` in vars/vm-test-scenarios.yml; a run whose count differs is an
# error, which is how a stale count is caught rather than papered over. Every
# check runs even after a failure — the transcript should show the whole
# picture, not the first crack. Nothing here changes the guest. A probe's
# output is captured (2>&1) and judged, never discarded: a failing probe's
# message IS the detail the check reports.
#
# Inputs (environment, set by the host over SSH):
#   VMTEST_COMMIT       the 40-hex commit the guest was told to provision from
#   VMTEST_USER_EMAIL   the git identity run.bash was given
set -uo pipefail

PLANNED=13
readonly PLANNED
REPO="${HOME}/Projects/fedora-desktop"
readonly REPO

total=0
passed=0
failed=0
skipped=0

check() {
    # check <status> <name> [detail]
    local status="${1:?}" name="${2:?}" detail="${3:-}"
    total=$((total + 1))
    case "${status}" in
        pass) passed=$((passed + 1)) ;;
        fail) failed=$((failed + 1)) ;;
        skip) skipped=$((skipped + 1)) ;;
        *)
            echo "ERROR: check status must be pass|fail|skip, got ${status}" >&2
            exit 70
            ;;
    esac
    detail="$(printf '%s' "${detail}" | tr '\n' ' ')"
    if [[ -n "${detail}" ]]; then
        printf 'VMTEST-CHECK %s %s %s\n' "${status}" "${name}" "${detail}"
    else
        printf 'VMTEST-CHECK %s %s\n' "${status}" "${name}"
    fi
}

evidence() {
    printf 'VMTEST-EVIDENCE %s=%s\n' "${1:?}" "$(printf '%s' "${2-}" | tr '\n' ' ')"
}

printf 'VMTEST-CHECK-PLANNED %d\n' "${PLANNED}"

# ── 1. the repo is at the pinned commit ───────────────────────────────────────────────
head=""
if head="$(git -C "${REPO}" rev-parse HEAD 2>&1)"; then
    if [[ "${head}" == "${VMTEST_COMMIT:-}" ]]; then
        check pass repo-cloned-at-pinned-commit "${head:0:12}"
    else
        check fail repo-cloned-at-pinned-commit "HEAD ${head:0:12} != requested ${VMTEST_COMMIT:-unset}"
    fi
else
    check fail repo-cloned-at-pinned-commit "no git checkout at ${REPO}: ${head}"
    head=""
fi

# ── 2. the branch's Fedora version is the guest's ───────────────────────────────────
repo_version=""
if version_line="$(grep -E '^fedora_version:' "${REPO}/vars/fedora-version.yml" 2>&1)"; then
    repo_version="${version_line#fedora_version:}"
    repo_version="${repo_version// /}"
fi
guest_version="$(. /etc/os-release && printf '%s' "${VERSION_ID}")"
if [[ -n "${repo_version}" && "${repo_version}" == "${guest_version}" ]]; then
    check pass fedora-version-matches-guest "${guest_version}"
else
    check fail fedora-version-matches-guest "repo '${repo_version}' vs guest '${guest_version}'"
fi

# ── 3. the no-identity config path was taken ────────────────────────────────────────
localhost_yml="${REPO}/environment/localhost/host_vars/localhost.yml"
if grep_out="$(grep -E '^github_accounts: \{\}$' "${localhost_yml}" 2>&1)"; then
    check pass localhost-yml-no-identity
else
    check fail localhost-yml-no-identity "github_accounts: {} not found in localhost.yml: ${grep_out}"
fi

# ── 4. ansible is on the box, since run.bash's whole job is to run it ──────────────
if ansible_version="$(ansible --version 2>&1)"; then
    check pass ansible-available "$(printf '%s\n' "${ansible_version}" | grep -m1 .)"
else
    check fail ansible-available "${ansible_version}"
fi

# ── 5. no transient secret file survived run.bash ───────────────────────────────────
if [[ ! -e /tmp/.github_ssh_pp ]]; then
    check pass no-ssh-passphrase-tmp-left
else
    check fail no-ssh-passphrase-tmp-left "/tmp/.github_ssh_pp still exists"
fi

# ── 6. the server profile was detected: no graphical target, no display manager ─────
default_target="$(systemctl get-default 2>&1)"
if gdm_query="$(rpm -q gdm 2>&1)"; then
    gdm_state="installed (${gdm_query})"
else
    gdm_state="absent"
fi
if [[ "${default_target}" == "multi-user.target" && "${gdm_state}" == "absent" ]]; then
    check pass provisioning-profile-server "${default_target}"
else
    check fail provisioning-profile-server "default ${default_target}, gdm ${gdm_state}"
fi

# ── 7. rootless podman works for the user (play-podman) ────────────────────────────
rootless="$(podman info --format '{{.Host.Security.Rootless}}' 2>&1)"
if [[ "${rootless}" == "true" ]]; then
    check pass podman-rootless-works
else
    check fail podman-rootless-works "${rootless}"
fi

# ── 8. git identity configured (play-git-configure-and-tools) ───────────────────────
git_email="$(git config --global user.email 2>&1)"
if [[ -n "${VMTEST_USER_EMAIL:-}" && "${git_email}" == "${VMTEST_USER_EMAIL}" ]]; then
    check pass git-identity-configured
else
    check fail git-identity-configured "user.email is '${git_email}', expected '${VMTEST_USER_EMAIL:-unset}'"
fi

# ── 9. tmux configuration deployed (play-tmux-sessions) ────────────────────────────
if [[ -r /etc/tmux.conf ]] && tmux_path="$(command -v tmux)"; then
    check pass tmux-deployed "${tmux_path}"
else
    check fail tmux-deployed "/etc/tmux.conf or tmux missing"
fi

# ── 10. the CCY launcher landed (play-claude-yolo) ─────────────────────────────────
if [[ -x /var/local/claude-yolo/claude-yolo ]]; then
    check pass ccy-launcher-deployed
else
    check fail ccy-launcher-deployed "/var/local/claude-yolo/claude-yolo not executable"
fi

# ── 11. the GitHub CLI is installed even with no account (play-github-cli-multi) ────
if gh_version="$(gh --version 2>&1)"; then
    check pass gh-cli-installed "$(printf '%s\n' "${gh_version}" | grep -m1 .)"
else
    check fail gh-cli-installed "${gh_version}"
fi

# ── 12. pyenv installed for the user (play-python) ─────────────────────────────────
if [[ -x "${HOME}/.pyenv/bin/pyenv" ]]; then
    check pass pyenv-installed
else
    check fail pyenv-installed "${HOME}/.pyenv/bin/pyenv missing"
fi

# ── 13. sshd is what let us in, and it is still up ──────────────────────────────────
if sshd_state="$(systemctl is-active sshd 2>&1)"; then
    check pass sshd-active "${sshd_state}"
else
    check fail sshd-active "${sshd_state}"
fi

# ── evidence (never a check; §6.6 rule 04 and the §4.4a guest-seen revision) ──────────
evidence boot_id "$(cat /proc/sys/kernel/random/boot_id 2>&1)"
evidence machine_id "$(cat /etc/machine-id 2>&1)"
evidence os_release "$(. /etc/os-release && printf '%s' "${PRETTY_NAME}")"
evidence kernel "$(uname -r)"
evidence repo_commit "${head}"
evidence default_target "${default_target}"
if repoinfo="$(sudo -n dnf --quiet repoinfo updates 2>&1)"; then
    evidence updates_revision "$(printf '%s\n' "${repoinfo}" | grep -E '^\s*Revision\s*:' | sed -E 's/^[^:]*:\s*//')"
    evidence updates_mirror "$(printf '%s\n' "${repoinfo}" | grep -E '^\s*Base URL\s*:' | sed -E 's/^[^:]*:\s*//' | awk '{print $1}')"
else
    evidence updates_revision ""
    evidence updates_mirror ""
    echo "WARNING: dnf repoinfo updates failed: ${repoinfo}" >&2
fi

printf 'VMTEST-CHECKS-DONE total=%d passed=%d failed=%d skipped=%d\n' "${total}" "${passed}" "${failed}" "${skipped}"
if [[ "${total}" -ne "${PLANNED}" ]]; then
    echo "ERROR: ${total} checks ran but ${PLANNED} were declared; this script is inconsistent" >&2
    exit 70
fi
exit 0
