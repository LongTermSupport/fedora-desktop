#!/usr/bin/env bash
# Plan 00195 — acceptance.bash
#
# PURPOSE: render the VERDICT that deploy.bash landed the fix (CLAUDE/PlanScriptStandards.md
# R9). Checks the override is recorded AND that every symlink in the home directory that
# points into /var/lib/lxc resolves inside a fresh Slack sandbox, which is the exact
# operation a drag from such a folder needs. HOST ONLY, read-only.
#
# The drag itself cannot be automated from here; the closing banner tells the operator
# what to do.
#
# Usage: ./acceptance.bash [-h|--help]
# Exit 0 = ACCEPTED, non-zero = REJECTED (the failed leg names itself).
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
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

Checks, on the HOST and without changing anything:
  1. the system Flatpak override for com.slack.Slack records /var/lib/lxc:ro
  2. the home directory holds at least one symlink into /var/lib/lxc to test with
  3. every such symlink resolves inside a fresh Slack sandbox

Exit 0 = ACCEPTED, non-zero = REJECTED."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads the host Flatpak overrides and runs a command inside the Slack sandbox"
plan_start_log auto

readonly APP_ID="com.slack.Slack"
readonly LXC_DIR="/var/lib/lxc"

# Symlinks up to two levels below home (a project folder, or one inside a group folder)
# whose target lies under the LXC directory. Hidden trees are skipped: nothing is
# dragged from them.
lxcLinks=()
while IFS= read -r -d '' link; do
    if [[ "$(readlink -f "${link}")" == "${LXC_DIR}/"* ]]; then
        lxcLinks+=("${link}")
    fi
done < <(find "${HOME}" -mindepth 1 -maxdepth 2 -not -path '*/.*' -type l -print0)

plan_gather_leg "override records ${LXC_DIR}:ro" \
    grep -q "${LXC_DIR}:ro" <<<"$(flatpak override --show "${APP_ID}")"
plan_gather_leg "home holds a symlink into ${LXC_DIR} (found ${#lxcLinks[@]})" \
    test "${#lxcLinks[@]}" -gt 0

# `test -e` follows the symlink, so it fails when the target is outside what the sandbox
# was granted, exactly as the drop does. Each run is a fresh sandbox with the CURRENT
# overrides, which is what a relaunched Slack gets.
for link in "${lxcLinks[@]}"; do
    plan_gather_leg "sandbox resolves ${link}" \
        flatpak run --command=test "${APP_ID}" -e "${link}/"
done

printf '\nNow the real test: with Slack relaunched, drag a file from one of the folders above\n'
printf 'in Nautilus into a message. It must upload. If it does not, the plan is not done.\n\n'

plan_finish
