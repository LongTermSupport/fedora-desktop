#!/usr/bin/env bash
# acceptance.bash — the pass/fail gate for Plan 00092 (CCY child-claude spawn mode).
#
# Renders the VERDICT (PlanScriptStandards R9). Every leg is one invariant from
# SECURITY-MODEL.md, so a red leg names the security property that broke rather than a
# symptom. Read-only: it changes nothing about the container it is judging.
#
# WHERE TO RUN: INSIDE the CCY container. Enforced by plan_require_container (R2) — run on
# the host, every probe would answer about the wrong machine and go vacuously green.
#
# IT MUST PASS TWICE, in two different containers:
#   1. with CCY_CHILD_CLAUDE=1 in .claude/ccy/ccy.env   -> the capability works, nothing leaked
#   2. with that line removed, in a container started AFTERWARDS
#                                                       -> the capability is gone, not dormant
# Run 2 is the one that catches the real bug: /root/.claude is host-persisted, so a skill
# installed by run 1 survives unless it is actively removed.
#
# Usage: ./CLAUDE/Plan/00092-ccy-child-claude-spawn-mode/acceptance.bash
#          [--expect enabled|disabled] [-h|--help]
#
# --expect overrides the auto-detection, which reads CCY_CHILD_CLAUDE from the environment
# the entrypoint built. Use it to assert the state you BELIEVE the container is in: if the
# detection and your belief disagree, that disagreement is itself the finding.
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
set -euo pipefail
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: acceptance.bash [--expect enabled|disabled] [-h|--help]

Judges Plan 00092's security invariants inside the CCY container.
Must pass with the mode enabled AND, in a later container, with it disabled."

plan_mode gather
plan_parse_common_flags "$@"

plan_require_container "it probes this container PATH, its PID 1 and what the entrypoint installed"

# ── which state are we judging? ───────────────────────────────────────────────────────────
#
# Auto-detected from the environment the entrypoint built. CCY_CHILD_CLAUDE comes from the
# project ccy.env, and ccy.env values DO survive into a Bash-tool shell — only the credential
# name is scrubbed — so this reads the real thing rather than a proxy for it.
EXPECT=""
if [[ "${CCY_CHILD_CLAUDE:-}" == "1" ]]; then
    EXPECT="enabled"
else
    EXPECT="disabled"
fi

wantExpect=""
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    if [[ -n "${wantExpect}" ]]; then
        wantExpect=""
        case "${arg}" in
            enabled | disabled)
                if [[ "${arg}" != "${EXPECT}" ]]; then
                    printf '[FATAL] you asked for --expect %s but this container detects %s.\n' \
                        "${arg}" "${EXPECT}" >&2
                    printf '        CCY_CHILD_CLAUDE is %s. That disagreement IS the finding:\n' \
                        "${CCY_CHILD_CLAUDE:-unset}" >&2
                    printf '        either ccy.env did not take effect, or the container predates the change.\n' >&2
                    exit 1
                fi
                ;;
            *)
                printf '[FATAL] --expect takes enabled or disabled, got %s\n' "${arg}" >&2
                exit 1
                ;;
        esac
        continue
    fi
    case "${arg}" in
        --expect) wantExpect=1 ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 1
            ;;
    esac
done
if [[ -n "${wantExpect}" ]]; then
    printf '[FATAL] --expect needs a value: enabled or disabled\n' >&2
    exit 1
fi

plan_start_log auto

printf '==> judging this container in its %s state (CCY_CHILD_CLAUDE=%s)\n' \
    "${EXPECT}" "${CCY_CHILD_CLAUDE:-unset}"

PROBE="${PLAN_SCRIPT_DIR}/probe-invariant.bash"

# ── invariants that hold in BOTH states ───────────────────────────────────────────────────
#
# These are the confidentiality invariants. They are not conditional on the feature being on,
# because "the token did not leak" must be true of every session either way — and running
# them in the disabled state is what proves they can still detect a leak at all.
plan_gather_leg "I1 no on-disk copy of the token" bash "${PROBE}" I1
plan_gather_leg "I2 token in no process argv" bash "${PROBE}" I2
plan_gather_leg "I3 token in no inherited variable" bash "${PROBE}" I3

if [[ "${EXPECT}" == "enabled" ]]; then
    plan_gather_leg "I4 wrapper leaks the token on no stream" bash "${PROBE}" I4
    plan_gather_leg "I5 arguments verbatim, authority unchanged" bash "${PROBE}" I5
    plan_gather_leg "I7 spawn depth is bounded" bash "${PROBE}" I7
    plan_gather_leg "functional: a child authenticates and answers" \
        bash "${PLAN_SCRIPT_DIR}/probe-functional.bash"
else
    # I6 is only meaningful here, and it is the invariant most likely to be broken: it fails
    # exactly when a previous enabled session left its artefacts in the host-persisted
    # /root/.claude tree.
    plan_gather_leg "I6 disabled means absent, not dormant" bash "${PROBE}" I6
fi

plan_finish
