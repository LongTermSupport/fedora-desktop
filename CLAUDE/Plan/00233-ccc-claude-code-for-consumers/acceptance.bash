#!/usr/bin/env bash
# Plan 00233 — acceptance.bash
#
# PURPOSE: judge the deployed ccc on the HOST, after deploy.bash. Read-only.
#
# NOT ESTABLISHABLE by a script (a person at a terminal with a Claude login is needed); the
# host agent or the owner does these by hand, and PLAN.md lists them:
#   - `ccc` from any directory opens Claude Code in ~/CCC, made on first use, after a sign-in
#   - a second `ccc` starts with no sign-in
#   - while it runs, `podman ps --filter label=ccc=true --format '{{.Mounts}}'` shows only
#     ~/CCC and ~/.local/share/ccc/claude, and the container has no GH_TOKEN
#   - ctrl+z in the session does nothing; resizing the window redraws it
#   - after /exit, `git -C ~/CCC log --oneline` shows the session's snapshot and
#     `git -C ~/CCC remote` prints nothing
#   - "Claude" in the app grid opens the same in a terminal window
#
# Usage: ./acceptance.bash [-h|--help]
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

The Plan 00233 acceptance gate. Run deploy.bash first (meta-deploy.bash does). It checks the
installed ccc, the image it runs, its private directories and its app grid entry.

EXIT STATUS
  0  ACCEPTED — every check ran and passed
  1  REJECTED — a check failed, or did not run
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n%s\n' "${PLAN_REMAINING_ARGS[*]}" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it checks the ccc the host's play installed"
plan_start_log auto

checker="${PLAN_SCRIPT_DIR}/check-ccc.bash"
plan_gather_leg "ccc --help prints its usage" bash "${checker}" "${PLAN_REPO_ROOT}" help
plan_gather_leg "ccc was built with the image version and refuses no terminal" bash "${checker}" "${PLAN_REPO_ROOT}" built-with-version
plan_gather_leg "claude-yolo:latest is at least REQUIRED_CONTAINER_VERSION" bash "${checker}" "${PLAN_REPO_ROOT}" image
plan_gather_leg "the token pool and ccc's state are 0700" bash "${checker}" "${PLAN_REPO_ROOT}" private-dirs
plan_gather_leg "the Claude app grid entry runs ccc in a terminal and validates" bash "${checker}" "${PLAN_REPO_ROOT}" desktop-entry
plan_gather_leg "podman is rootless" bash "${checker}" "${PLAN_REPO_ROOT}" podman-for-everyone
plan_finish
