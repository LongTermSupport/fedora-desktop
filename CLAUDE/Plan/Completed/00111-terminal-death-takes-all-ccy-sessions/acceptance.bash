#!/usr/bin/env bash
# Plan 00111 — acceptance.bash
#
# PURPOSE: render the VERDICT that a CCY session survives the death of its terminal
# (CLAUDE/PlanScriptStandards.md R9). It drives the DEPLOYED library at
# /var/local/claude-yolo/lib through the same entry point the launcher uses, with a stand-in
# for the launcher and a throwaway pty standing in for the terminal emulator, then kills that
# pty owner with SIGKILL — the identical hang-up the 2026-09-13 Ptyxis death delivered.
#
# It does NOT kill Ptyxis: that would destroy every real terminal on the desktop. Task 5.2 is
# that real kill, done by hand at a moment of the operator's choosing.
#
# HOST ONLY. EFFECT ON THE HOST: starts CCY's tmux server if it is not running, creates one
# throwaway session named ccy-acceptance-00111-<pid> and removes it again; nothing else.
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

Proves, on the HOST, that the deployed CCY tmux insulation works:
  1. the deployed library matches the repo and the tools it needs exist
  2. an insulated command lands in a tmux session whose server is in its own
     systemd user scope, not the terminal's
  3. SIGKILLing the terminal leaves the session alive and detached
  4. running the entry point again from the same project offers the session
     and Enter re-attaches it
  5. a second terminal attaching to the open session is bounced by the server
  6. answering the offer with n starts a second session for the project
  7. inside tmux, or without a terminal, the entry point is a no-op

Exit 0 = ACCEPTED, non-zero = REJECTED."

# deploy mode: the steps build on each other, so the first failure must stop the run.
plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it drives the deployed launcher library, tmux and systemd --user on this machine"
plan_start_log auto

STEPS="${PLAN_SCRIPT_DIR}/insulation-steps.bash"
STATE="${PLAN_RUN_DIR}/state"
readonly STEPS STATE

# Whatever happens, the throwaway session and fake terminals must not outlive the run.
trap 'bash "${STEPS}" cleanup "${STATE}"' EXIT

plan_deploy_leg "deployed library matches the repo; tmux, systemd-run, python3 present" \
    bash "${STEPS}" preconditions "${STATE}"
plan_deploy_leg "insulated command starts inside a fake terminal" \
    bash "${STEPS}" start "${STATE}"
plan_deploy_leg "session exists, attached, server in its own systemd user scope" \
    bash "${STEPS}" assert-created "${STATE}"
plan_deploy_leg "fake terminal SIGKILLed (pty hang-up)" \
    bash "${STEPS}" kill-terminal "${STATE}"
plan_deploy_leg "session survived, detached, process still running" \
    bash "${STEPS}" assert-survived "${STATE}"
plan_deploy_leg "second launch from the same project offers the session; Enter re-attaches it" \
    bash "${STEPS}" reattach "${STATE}"
plan_deploy_leg "a raw second attach to the open session is bounced by the server" \
    bash "${STEPS}" bounce "${STATE}"
plan_deploy_leg "answering the offer with n starts a -2 session, leaves the first alone" \
    bash "${STEPS}" offer-new "${STATE}"
plan_deploy_leg "the yes/no picker and ccy-sessions open and close cleanly" \
    bash "${STEPS}" pickers "${STATE}"
plan_deploy_leg "deployed launcher: --help prints and exits with no session" \
    bash "${STEPS}" real-help "${STATE}"
plan_deploy_leg "deployed launcher: enters tmux before its first prompt" \
    bash "${STEPS}" real-launch "${STATE}"
plan_deploy_leg "deployed host cc wrapper: enters tmux before its token chooser" \
    bash "${STEPS}" real-cc-launch "${STATE}"
plan_deploy_leg "no-op inside tmux and without a terminal" \
    bash "${STEPS}" not-applicable "${STATE}"
plan_deploy_leg "throwaway session removed" \
    bash "${STEPS}" cleanup "${STATE}"

printf '\nTask 5.2 is still yours: with a real ccy session running, kill Ptyxis\n'
printf '(pkill -x ptyxis), open a new terminal, cd to the project and run ccy.\n'
printf 'It must re-attach with the session intact.\n\n'

plan_finish
