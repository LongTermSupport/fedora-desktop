#!/usr/bin/env bash
# Plan 00102 — acceptance.bash
#
# PURPOSE: render the VERDICT that deploy.bash landed the fix (CLAUDE/PlanScriptStandards.md
# R9). Reads the effective Dash to Dock setting through gsettings, exactly as the extension
# itself does, so it exercises the deployed state and not the repo. HOST ONLY, read-only.
#
# The visual half of the check — an un-maximised Ptyxis window overlapping the dock makes
# it hide — cannot be automated from here (GNOME 50 denies window introspection over
# D-Bus); the closing banner tells the operator what to look at.
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
  1. the Dash to Dock extension is enabled and active
  2. intellihide is on and the dock is not fixed
  3. intellihide-mode reads back as ALL_WINDOWS

Exit 0 = ACCEPTED, non-zero = REJECTED."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it reads the live GNOME session's dconf and extension state"
plan_start_log auto

readonly SCHEMA="org.gnome.shell.extensions.dash-to-dock"

# Every check is a plain command, so a failed leg is a failed check and plan_finish exits
# non-zero. gsettings prints GVariant, hence the quoted 'ALL_WINDOWS'.
# "enabled" is only the gsettings list; ACTIVE is the Shell's runtime verdict (EXT-05 in the
# play). The UUID is discovered because it has the shape of an email address, which the
# pre-commit scanner rejects.
plan_gather_leg "dash-to-dock is ACTIVE in the running Shell" \
    grep -q 'State: ACTIVE' <<<"$(gnome-extensions info "$(gnome-extensions list | grep 'dash-to-dock')")"
plan_gather_leg "intellihide is on" \
    test "$(gsettings get "${SCHEMA}" intellihide)" = "true"
plan_gather_leg "dock is not fixed" \
    test "$(gsettings get "${SCHEMA}" dock-fixed)" = "false"
plan_gather_leg "intellihide-mode is ALL_WINDOWS" \
    test "$(gsettings get "${SCHEMA}" intellihide-mode)" = "'ALL_WINDOWS'"

printf '\nNow the eyeball check: un-maximise a Ptyxis window, drag it over the dock.\n'
printf 'The dock must slide away. If it does not, the plan is not done.\n\n'

plan_finish
