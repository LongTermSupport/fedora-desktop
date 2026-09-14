#!/usr/bin/bash
# guest-acceptance-desktop.bash — the assertion set for a DESKTOP-profile guest, run
# INSIDE the guest after run.bash has finished in the autologin session (Plan 00110,
# DESIGN.md §5.4). Same contract as guest-acceptance-server.bash:
#
#   VMTEST-CHECK-PLANNED <n>                declared BEFORE the first check
#   VMTEST-CHECK pass|fail|skip <name> [detail]
#   VMTEST-EVIDENCE <key>=<value>
#   VMTEST-CHECKS-DONE total=N passed=N failed=N skipped=N
#
# Runs over SSH as the lab user with the session's bus reachable (the host sets
# XDG_RUNTIME_DIR and DBUS_SESSION_BUS_ADDRESS from the session.env the runner wrote), so
# `gnome-extensions info` and `busctl --user` speak to the real GNOME Shell. What §5.4
# calls evidence-only (screenshot, journal) is collected by the host, not asserted here.
#
# Inputs (environment, set by the host over SSH):
#   VMTEST_COMMIT       the 40-hex commit the guest was told to provision from
#   VMTEST_USER_EMAIL   the git identity run.bash was given
set -uo pipefail

PLANNED=16
readonly PLANNED
REPO="${HOME}/Projects/fedora-desktop"
readonly REPO

total=0
passed=0
failed=0
skipped=0

check() {
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

# ── the shared product checks (as the server script) ─────────────────────────────────
head=""
if head="$(git -C "${REPO}" rev-parse HEAD 2>&1)"; then
    if [[ "${head}" == "${VMTEST_COMMIT:-}" ]]; then
        check pass repo-cloned-at-pinned-commit "${head:0:12}"
    else
        check fail repo-cloned-at-pinned-commit "HEAD ${head:0:12} is not the pinned ${VMTEST_COMMIT:-unset}"
    fi
else
    check fail repo-cloned-at-pinned-commit "no git checkout at ${REPO}: ${head}"
fi

repo_version="$(grep -E '^fedora_version:' "${REPO}/vars/fedora-version.yml" 2>&1 | awk '{print $2}')"
guest_version="$(. /etc/os-release && printf '%s' "${VERSION_ID}")"
if [[ -n "${repo_version}" && "${repo_version}" == "${guest_version}" ]]; then
    check pass fedora-version-matches-guest "${guest_version}"
else
    check fail fedora-version-matches-guest "repo '${repo_version}' vs guest '${guest_version}'"
fi

grep_out=""
if grep_out="$(grep -E '^github_accounts: \{\}' "${REPO}/environment/localhost/host_vars/localhost.yml" 2>&1)"; then
    check pass localhost-yml-no-identity
else
    check fail localhost-yml-no-identity "github_accounts: {} not found in localhost.yml: ${grep_out}"
fi

ansible_version=""
if ansible_version="$(ansible --version 2>&1)"; then
    check pass ansible-available "$(printf '%s\n' "${ansible_version}" | grep -m1 .)"
else
    check fail ansible-available "${ansible_version}"
fi

git_email="$(git config --global user.email 2>&1)"
if [[ -n "${VMTEST_USER_EMAIL:-}" && "${git_email}" == "${VMTEST_USER_EMAIL}" ]]; then
    check pass git-identity-configured
else
    check fail git-identity-configured "user.email is '${git_email}', expected '${VMTEST_USER_EMAIL:-unset}'"
fi

tmux_path="$(command -v tmux 2>&1)"
if [[ -r /etc/tmux.conf && -n "${tmux_path}" ]]; then
    check pass tmux-deployed "${tmux_path}"
else
    check fail tmux-deployed "/etc/tmux.conf or tmux missing"
fi

if [[ -x /var/local/claude-yolo/claude-yolo ]]; then
    check pass ccy-launcher-deployed
else
    check fail ccy-launcher-deployed "/var/local/claude-yolo/claude-yolo not executable"
fi

gh_version=""
if gh_version="$(gh --version 2>&1)"; then
    check pass gh-cli-installed "$(printf '%s\n' "${gh_version}" | grep -m1 .)"
else
    check fail gh-cli-installed "${gh_version}"
fi

if [[ -x "${HOME}/.pyenv/bin/pyenv" ]]; then
    check pass pyenv-installed
else
    check fail pyenv-installed "${HOME}/.pyenv/bin/pyenv missing"
fi

sshd_state="$(systemctl is-active sshd 2>&1)"
if [[ "${sshd_state}" == "active" ]]; then
    check pass sshd-active "${sshd_state}"
else
    check fail sshd-active "${sshd_state}"
fi

rootless=""
if rootless="$(podman run --rm registry.fedoraproject.org/fedora-minimal:latest true 2>&1)"; then
    check pass podman-rootless-works
else
    check fail podman-rootless-works "${rootless}"
fi

# ── the desktop session (§5.4 asserted) ──────────────────────────────────────────────
default_target="$(systemctl get-default 2>&1)"
if [[ "${default_target}" == "graphical.target" ]]; then
    check pass default-target-graphical "${default_target}"
else
    check fail default-target-graphical "${default_target}"
fi

session_id="$(loginctl list-sessions --no-legend 2>&1 | awk -v u="${USER}" '$3 == u && $0 ~ /seat0/ {print $1; exit}')"
session_facts="$(loginctl show-session "${session_id:-none}" -p Type -p Active -p Remote 2>&1 | tr '\n' ' ')"
if [[ "${session_facts}" == *"Type=wayland"* && "${session_facts}" == *"Active=yes"* && "${session_facts}" == *"Remote=no"* ]]; then
    check pass session-wayland-active-local "${session_facts}"
else
    check fail session-wayland-active-local "session ${session_id:-none}: ${session_facts}"
fi

bus_status=""
if bus_status="$(busctl --user status 2>&1)"; then
    check pass session-bus-answers
else
    check fail session-bus-answers "${bus_status}"
fi

shell_version="$(gnome-shell --version 2>&1)"
if pgrep -x gnome-shell >/dev/null && [[ "${shell_version}" == "GNOME Shell "* ]]; then
    check pass gnome-shell-running "${shell_version}"
else
    check fail gnome-shell-running "${shell_version}"
fi

# Every extension the REPO deploys must report ACTIVE in the live session, not merely be
# enabled in a settings list: this is the check verify_extension.py cannot make without a
# session (§5.4).
#
# The population is DECLARED by vars/gnome-shell-extensions.yml, not discovered by listing
# ~/.local/share/gnome-shell/extensions. That directory is also where a user's own
# extensions live, so listing it judged extensions the repo never installed while still
# missing a partial install. Declaring it also brings in dash-to-dock, which is a DNF
# SYSTEM extension living in a different directory and was silently excluded before —
# a run of 2026-09-14 found it installed and never enabled.
extensions_vars="${REPO}/vars/gnome-shell-extensions.yml"
declared="$(python3 -c '
import sys

import yaml

with open(sys.argv[1]) as handle:
    groups = (yaml.safe_load(handle) or {}).get("gnome_shell_extensions")
# Every group, not three names spelled out: a fourth group must not be silently
# uncounted here while the play enables it. A missing or renamed top-level key is
# an ERROR, not an empty population — "0 of 0 declared ACTIVE" would otherwise be
# a clean pass over a check that examined nothing.
if not isinstance(groups, dict) or not groups:
    raise SystemExit("gnome_shell_extensions is missing or not a mapping of groups")
uuids = [entry["uuid"] for entries in groups.values() for entry in entries or []]
if not uuids:
    raise SystemExit("gnome_shell_extensions declares no extensions at all")
for uuid in uuids:
    print(uuid)
' "${extensions_vars}" 2>&1)" || declared="READ-FAILED ${declared}"

user_extensions="${HOME}/.local/share/gnome-shell/extensions"
on_disk=""
if [[ -d "${user_extensions}" ]]; then
    on_disk="$(find "${user_extensions}" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)"
fi
enabled_live="$(gnome-extensions list --enabled 2>&1 | sort | tr '\n' ',')"
enabled_settings="$(gsettings get org.gnome.shell enabled-extensions 2>&1)"

if [[ "${declared}" == READ-FAILED* ]]; then
    # A check whose population could not be read has not passed, it has not run.
    check fail deployed-extensions-active "cannot read ${extensions_vars}: ${declared#READ-FAILED }"
else
    expected="$(printf '%s' "${declared}" | grep -c .)"
    inactive=""
    active_count=0
    for uuid in ${declared}; do
        state="$(gnome-extensions info "${uuid}" 2>&1 | grep -E '^\s*State:' | awk '{print $2}')"
        if [[ "${state}" == "ACTIVE" ]]; then
            active_count=$((active_count + 1))
        else
            inactive="${inactive} ${uuid}=${state:-unknown}"
        fi
    done
    coverage="COVERAGE: ${active_count} of ${expected} declared ACTIVE"
    if [[ "${expected}" -eq 0 ]]; then
        # The population and the expectation are now the same list, so nothing
        # else guards zero. "0 of 0 ACTIVE" is the repo's highest-recurrence
        # defect shape and must never be a pass.
        check fail deployed-extensions-active "${extensions_vars} parsed to no extensions at all"
    elif [[ -z "${inactive}" ]]; then
        check pass deployed-extensions-active "${coverage}"
    else
        check fail deployed-extensions-active "${coverage}; not ACTIVE:${inactive}"
    fi
fi

# ── evidence (never a check; §6.6 rule 04, §5.3b) ─────────────────────────────────────
evidence boot_id "$(cat /proc/sys/kernel/random/boot_id)"
evidence machine_id "$(cat /etc/machine-id)"
evidence os_release "$(. /etc/os-release && printf '%s' "${PRETTY_NAME}")"
evidence kernel "$(uname -r)"
evidence repo_commit "${head}"
evidence default_target "${default_target}"
evidence session_type "$(printf '%s' "${session_facts}" | grep -oE 'Type=[a-z0-9]+' | cut -d= -f2)"
evidence gnome_shell_version "${shell_version#GNOME Shell }"
evidence declared_extensions "$(printf '%s' "${declared}" | tr '\n' ',')"
evidence user_extensions_on_disk "$(printf '%s' "${on_disk}" | tr '\n' ',')"
evidence enabled_extensions_live "${enabled_live}"
evidence enabled_extensions_settings "${enabled_settings}"
evidence disable_user_extensions "$(gsettings get org.gnome.shell disable-user-extensions 2>&1)"
evidence system_extensions "$(find /usr/share/gnome-shell/extensions -mindepth 1 -maxdepth 1 -type d -printf '%f,' 2>&1)"
session_env_count=0
if [[ -r /var/lib/vmtest/session.env ]]; then
    session_env_count="$(wc -l </var/lib/vmtest/session.env)"
fi
evidence session_env_vars "${session_env_count}"
repoinfo=""
if repoinfo="$(sudo -n dnf --quiet repoinfo updates 2>&1)"; then
    evidence updates_revision "$(printf '%s\n' "${repoinfo}" | grep -E '^\s*Revision\s*:' | awk -F: '{gsub(/ /, "", $2); print $2}')"
    evidence updates_mirror "$(printf '%s\n' "${repoinfo}" | grep -E '^\s*Base URL\s*:' | awk '{print $4}')"
else
    evidence updates_revision ""
    evidence updates_mirror ""
    echo "WARNING: dnf repoinfo updates failed: ${repoinfo}" >&2
fi

printf 'VMTEST-CHECKS-DONE total=%d passed=%d failed=%d skipped=%d\n' "${total}" "${passed}" "${failed}" "${skipped}"
