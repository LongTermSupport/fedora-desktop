#!/usr/bin/env bash
set -euo pipefail

# meta-deploy.bash — run a named list of plans' triage / deploy / acceptance scripts.
#
# A convenience wrapper for working on several plans at once, nothing more: the durable
# behaviour lives in each plan's own scripts. It exists so the operator types one command
# instead of four paths.
#
#   CLAUDE/Plan/meta-deploy.bash              run everything in PLANS below
#   CLAUDE/Plan/meta-deploy.bash --list       show what would run, change nothing
#
# THE LIST IS HARD-CODED, on purpose. Deriving it from plan status made the schedule a
# thing to reason about rather than read: plans stayed in it because nobody had closed
# them, and the only way to know what a run would do was to run it. Editing one line is
# the whole interface.
#
# There is no way to select a single plan either. To run one plan's deploy, run that
# plan's deploy.bash — a flag for that is a worse way of typing one path.
#
# HOST ONLY. Every plan script refuses to run in the CCY container by design.

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
PLAN_ROOT="${scriptDir}"

# --- the list ----------------------------------------------------------------
# Plan folder names, run in this order. STANDING RULE (CLAUDE/AgentNotes.md): this list
# is exactly what needs running NOW. Add a run when it is pending, remove it once it has
# run and is done, leave out what cannot usefully run yet, then ask the owner to run this
# script. Every step a run needs lives in the plan's own scripts, so this is the only
# command the owner is handed. The order is by dependency. A plan with none of
# triage.bash, deploy.bash and acceptance.bash has nothing for this script to do. A triage-only plan is run once, read-only. Words after the
# folder name are passed to that plan's triage.bash, both runs.
#
# An entry may instead be a playbook path under playbooks/, for a change no plan owns. It is
# run as one unit through its own shebang, which goes through run.bash, exactly as
# `./playbooks/imports/<play>.yml` does by hand.
PLANS=(
    # Plan 00135: enables the ccy session restore unit, once localhost.yml declares
    # ccy_restore_sessions: true, so the sessions running now come back after the
    # reboot-with-update that follows (Tasks 5.2 and 5.3).
    "playbooks/imports/play-claude-yolo.yml"
)

LIST_ONLY=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --list) LIST_ONLY=1; shift ;;
        -h | --help)
            awk '/^# meta-deploy/,/^$/' "${BASH_SOURCE[0]}"; exit 0 ;;
        *)
            printf '[FATAL] unknown argument: %s\n' "$1" >&2; exit 64 ;;
    esac
done

# A named plan that is not there is a typo or a rename, and silently skipping it would mean
# a green run that deployed less than the list says. Checked before anything executes, so
# the whole list is validated rather than failing partway through a deploy.
PLAN_DIRS=()
declare -A TRIAGE_ARGS=()
for planEntry in "${PLANS[@]}"; do
    read -r -a entryWords <<<"${planEntry}"
    planName="${entryWords[0]}"
    if [[ "${planName}" == playbooks/*.yml ]]; then
        if [[ ! -x "${repoRoot}/${planName}" ]]; then
            printf '[FATAL] no executable playbook at %s\n' "${repoRoot}/${planName}" >&2
            exit 1
        fi
        PLAN_DIRS+=("${repoRoot}/${planName}")
        TRIAGE_ARGS["${repoRoot}/${planName}"]=""
        continue
    fi
    planDir="${PLAN_ROOT}/${planName}"
    TRIAGE_ARGS["${planDir}"]="${entryWords[*]:1}"
    if [[ ! -d "${planDir}" ]]; then
        printf '[FATAL] no such plan folder: %s\n' "${planDir}" >&2
        printf '        It may have been archived into Completed/ or renamed. Fix the\n' >&2
        printf '        PLANS list at the top of this script.\n' >&2
        exit 1
    fi
    if [[ ! -x "${planDir}/deploy.bash" && ! -x "${planDir}/acceptance.bash" && ! -x "${planDir}/triage.bash" ]]; then
        printf '[FATAL] %s ships no executable triage.bash, deploy.bash or acceptance.bash\n' "${planName}" >&2
        exit 1
    fi
    PLAN_DIRS+=("${planDir}")
done

if [[ ${#PLAN_DIRS[@]} -eq 0 ]]; then
    printf 'The PLANS list is empty. Nothing to run.\n'
    exit 0
fi

printf '%d plan(s):\n' "${#PLAN_DIRS[@]}"
for planDir in "${PLAN_DIRS[@]}"; do
    printf '  %s %s\n' "$(basename "${planDir}")" "${TRIAGE_ARGS["${planDir}"]}"
done
printf '\n'

if [[ "${LIST_ONLY}" == "1" ]]; then
    printf '%s\n' '--list changed nothing.'
    exit 0
fi

# There is no confirmation prompt. Running this script IS the consent — a second
# gate is ceremony that only ever hangs the batch until someone notices it.
# PLAN_ASSUME_YES is still exported so any plan script that asks its own question
# does not stop the run either.
export PLAN_ASSUME_YES=1

# --- run ---------------------------------------------------------------------
# Most plan scripts write their own run log, but not all: 00099's acceptance.bash
# deliberately does not source the plan library, so a PASS from it leaves no evidence
# anywhere except the operator's scrollback. Captured here so every unit in the batch has
# a record regardless.
#
# `cmd | tee` and NOT `> >(tee …)`: the shell waits for every member of a pipeline, but it
# cannot wait on a process substitution, so the latter loses the last chunk — reliably the
# part naming what failed. That is the whole subject of Plan 00130.
CAPTURE_DIR="${repoRoot}/untracked/plan-runs/_meta-deploy/$(date +%Y%m%d-%H%M%S)"
mkdir -p "${CAPTURE_DIR}"
printf 'capturing each unit to %s\n\n' "${CAPTURE_DIR}"

RESULTS=()

# A COUNT of bad units, not a sum of their exit codes. Summing would wrap at 256, so a
# batch whose codes happened to total exactly that would exit 0 and report success for a
# run that failed. Counting cannot alias.
BAD=0

for planDir in "${PLAN_DIRS[@]}"; do
    planName="$(basename "${planDir}")"

    if [[ "${planDir}" == *.yml ]]; then
        printf '\n===> %s\n' "${planName}"
        if "${planDir}" 2>&1 | tee "${CAPTURE_DIR}/${planName}.log"; then
            status="${PIPESTATUS[0]}"
        else
            status="${PIPESTATUS[0]}"
        fi
        if [[ "${status}" == "0" ]]; then
            RESULTS+=("PASS  ${planName}")
        else
            RESULTS+=("FAIL  ${planName} (exit ${status})")
            BAD=$((BAD + 1))
        fi
        continue
    fi

    triageArgs=()
    read -r -a triageArgs <<<"${TRIAGE_ARGS["${planDir}"]}"

    # ACCEPTANCE IS SKIPPED WHEN THE DEPLOY FAILED, and this is not tidiness.
    # acceptance.bash judges the machine's CURRENT state, so after a failed deploy it
    # judges whatever the last successful deploy left behind — and returns PASS for code
    # that never landed. That is a green verdict about something that did not happen,
    # which is worse than a red one. It has already occurred: plan 032 reported
    # "FAIL deploy / PASS acceptance" in one run.
    #
    # Fail-fast, per CLAUDE.md: never decouple dependent operations.
    # TRIAGE BRACKETS THE DEPLOY, before and after.
    #
    # Two reasons, and neither is tidiness. First, some gates need the pair: plan 00112's
    # check [5] asks whether the deploy REMOVED anything from the enabled list, which no
    # single reading can answer — without a before and an after it reports COULD NOT
    # ESTABLISH, so under a deploy-then-acceptance harness that plan can never exit 0
    # however healthy the machine is. Second, when something does fail, the pair is the
    # evidence: one reading says what the host looks like, two say what the deploy did.
    #
    # Triage is read-only and renders no verdict, so its exit status is recorded but does
    # NOT gate the deploy — a fact-finder that could not collect everything is not a
    # reason to refuse to deploy.
    if [[ -x "${planDir}/triage.bash" ]]; then
        printf '\n===> %s / triage.bash (before)\n' "${planName}"
        if "${planDir}/triage.bash" "${triageArgs[@]}" 2>&1 | tee "${CAPTURE_DIR}/${planName}-triage-before.log"; then
            RESULTS+=("PASS  ${planName}/triage.bash (before)")
        else
            RESULTS+=("note  ${planName}/triage.bash (before) exit ${PIPESTATUS[0]} — read-only, does not gate the deploy")
        fi
    fi

    deployOk=1
    if [[ -x "${planDir}/deploy.bash" ]]; then
        printf '\n===> %s / deploy.bash\n' "${planName}"
        # PIPESTATUS[0] is the script's status, not tee's. It is read in both branches
        # because `if ! cmd` would invert the status before $? could be examined.
        if "${planDir}/deploy.bash" 2>&1 | tee "${CAPTURE_DIR}/${planName}-deploy.log"; then
            status="${PIPESTATUS[0]}"
        else
            status="${PIPESTATUS[0]}"
        fi
        if [[ "${status}" == "0" ]]; then
            RESULTS+=("PASS  ${planName}/deploy.bash")
        else
            RESULTS+=("FAIL  ${planName}/deploy.bash (exit ${status})")
            deployOk=0
            BAD=$((BAD + 1))
        fi
    fi

    # The second half of the bracket — and it must come BEFORE acceptance, since the
    # gates that want a pair read it from triage's own run logs.
    if [[ -x "${planDir}/triage.bash" && -x "${planDir}/deploy.bash" && "${deployOk}" == "1" ]]; then
        printf '\n===> %s / triage.bash (after)\n' "${planName}"
        if "${planDir}/triage.bash" "${triageArgs[@]}" 2>&1 | tee "${CAPTURE_DIR}/${planName}-triage-after.log"; then
            RESULTS+=("PASS  ${planName}/triage.bash (after)")
        else
            RESULTS+=("note  ${planName}/triage.bash (after) exit ${PIPESTATUS[0]} — read-only, does not gate acceptance")
        fi
    fi

    if [[ -x "${planDir}/acceptance.bash" ]]; then
        if [[ "${deployOk}" == "1" ]]; then
            printf '\n===> %s / acceptance.bash\n' "${planName}"
            if "${planDir}/acceptance.bash" 2>&1 | tee "${CAPTURE_DIR}/${planName}-acceptance.log"; then
                status="${PIPESTATUS[0]}"
            else
                status="${PIPESTATUS[0]}"
            fi
            # EXIT 2 IS A THIRD VERDICT, NOT A FAILURE. An acceptance gate here may answer
            # ACCEPTED (0), REJECTED (1) or COULD NOT ESTABLISH (2) — the last meaning
            # nothing failed but some check had no evidence to judge on. Collapsing it into
            # FAIL reports a broken machine where the truth is an unanswered question, and
            # that is precisely the distinction those gates were written to preserve. It
            # still counts as bad, because an unanswered question is not a pass.
            case "${status}" in
                0)
                    RESULTS+=("PASS  ${planName}/acceptance.bash")
                    ;;
                2)
                    RESULTS+=("UNSET ${planName}/acceptance.bash (exit 2 — COULD NOT ESTABLISH: nothing failed, but some check had no evidence)")
                    BAD=$((BAD + 1))
                    ;;
                *)
                    RESULTS+=("FAIL  ${planName}/acceptance.bash (exit ${status})")
                    BAD=$((BAD + 1))
                    ;;
            esac
        else
            printf '\n===> %s / acceptance.bash SKIPPED — its deploy failed\n' "${planName}"
            RESULTS+=("SKIP  ${planName}/acceptance.bash (deploy failed; a verdict here would judge the previous deploy)")
            # A skipped acceptance is a plan left unjudged, so it counts as bad. Exiting 0
            # with an unjudged plan in the batch is the same false green in a new place.
            BAD=$((BAD + 1))
        fi
    fi
done

# The summary is written into the capture directory as well as the terminal, so the agent
# that asked for the run reads the verdict itself instead of the owner pasting it back.
# summary.txt appearing is also the marker that the whole batch has finished.
if [[ "${BAD}" -gt 0 ]]; then
    closing="$(printf '%d of %d unit(s) failed or were skipped.' "${BAD}" "${#RESULTS[@]}")"
else
    closing="$(printf 'all %d unit(s) passed.' "${#RESULTS[@]}")"
fi
{
    printf '\n==============================================================\n'
    printf 'Summary\n'
    printf '==============================================================\n'
    printf '%s\n' "${RESULTS[@]}"
    printf '\n%s\n' "${closing}"
} | tee "${CAPTURE_DIR}/summary.txt"

# A batch is only useful if a failure anywhere is visible in the exit status. The count is
# incremented where each failure happens, rather than re-derived by grepping the summary
# this script printed itself — parsing your own output makes the exit status a function of
# formatting, and a later tweak to a label would silently change what the batch reports.
if [[ "${BAD}" -gt 0 ]]; then
    exit 1
fi
exit 0
