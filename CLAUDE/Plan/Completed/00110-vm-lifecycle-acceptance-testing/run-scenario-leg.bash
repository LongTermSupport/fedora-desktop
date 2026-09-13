#!/usr/bin/env bash
# run-scenario-leg.bash — one acceptance leg: run a scenario through the deployed `vmtest`
# and assert the verdict (and, for a non-pass, the failure stage) that DESIGN.md says it
# must produce. Renders a per-leg verdict line; acceptance.bash sums them (R9).
#
# Normally invoked as a leg of acceptance.bash. Runnable standalone, on the HOST:
#   ./run-scenario-leg.bash <scenario-id> <expected-verdict> <expected-stage|-> \
#                           <expected-transcript-regex|-> <report-file>
#
# The expectation is the falsifiability proof of T3.4: a negative scenario that came back
# `pass` or `error` fails this leg just as loudly as a positive one that came back `fail`.
# The transcript regex is the second half of that proof: a negative scenario must fail FOR
# ITS OWN REASON. The first acceptance run showed why — every scenario, positive and
# negative, died on the same detached-HEAD `git pull` in run.bash, so all three negatives
# would have gone red at the right stage while proving nothing about the thing each one
# names. The regex pins the failure to the message the design says must appear.
# Nothing is read from the checkout; the deployed copies decide.
#
# EXIT CODES:
#   0  the scenario produced exactly the expected verdict and stage
#   1  it did not, or the run could not be judged; the report says which
#  64  usage error
set -euo pipefail

# ── R1 bootstrap ──────────────────────────────────────────────────────────────────────────
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

SCENARIO="${1:-}"
EXPECTED_VERDICT="${2:-}"
EXPECTED_STAGE="${3:-}"
EXPECTED_REGEX="${4:-}"
REPORT="${5:-}"
if [[ -z "${SCENARIO}" || -z "${EXPECTED_VERDICT}" || -z "${EXPECTED_STAGE}" || -z "${EXPECTED_REGEX}" || -z "${REPORT}" ]]; then
    printf 'usage: run-scenario-leg.bash <scenario-id> <expected-verdict> <expected-stage|-> <expected-transcript-regex|-> <report-file>\n' >&2
    exit 64
fi

plan_require_host "it boots a guest on the host's hypervisor through the deployed vmtest CLI"

out() { printf '%s\n' "$*" >>"${REPORT}"; }

VMTEST="${HOME}/.local/bin/vmtest"
[[ -x "${VMTEST}" ]] || {
    out "- ${SCENARIO}: **UNANSWERED** — ${VMTEST} is not deployed (run deploy.bash first)"
    exit 1
}

# `vmtest run` exits 0 only on pass; a non-zero exit here is DATA for the negative
# scenarios, so it is captured, not fatal.
runLine=""
runRc=0
if runLine="$("${VMTEST}" run "${SCENARIO}" 2>>"${REPORT}.stderr" | grep -E '^VMTEST-RUN ')"; then
    runRc=0
else
    runRc=$?
fi
if [[ -z "${runLine}" ]]; then
    out "- ${SCENARIO}: **UNANSWERED** — vmtest run printed no VMTEST-RUN line (exit ${runRc}); see ${REPORT}.stderr"
    exit 1
fi

# VMTEST-RUN <run-id> verdict=<v> response=<path>
read -r _ runId verdictField responseField <<<"${runLine}"
verdict="${verdictField#verdict=}"
response="${responseField#response=}"
stage="-"
if [[ "${verdict}" != "pass" ]]; then
    stage="$(grep -oE '"stage": "[a-z]+"' "${response}" | grep -oE '[a-z]+"$' | tr -d '"')" || stage="?"
fi

if [[ "${verdict}" != "${EXPECTED_VERDICT}" || "${stage}" != "${EXPECTED_STAGE}" ]]; then
    out "- ${SCENARIO}: **NOT as expected** — got verdict ${verdict}, stage ${stage}; expected ${EXPECTED_VERDICT}/${EXPECTED_STAGE} (run ${runId}, response ${response})"
    exit 1
fi
if [[ "${EXPECTED_REGEX}" != "-" ]]; then
    transcript="$(dirname "${response}")/transcript.log"
    if ! grep -qE "${EXPECTED_REGEX}" "${transcript}"; then
        out "- ${SCENARIO}: **NOT as expected** — verdict ${verdict}/${stage} is right, but the transcript does not contain /${EXPECTED_REGEX}/: it failed for some OTHER reason (run ${runId}, ${transcript})"
        exit 1
    fi
fi
matched=""
if [[ "${EXPECTED_REGEX}" != "-" ]]; then
    matched=", transcript matches /${EXPECTED_REGEX}/"
fi
out "- ${SCENARIO}: **as expected** — verdict ${verdict}, stage ${stage}${matched} (run ${runId}, response ${response})"
exit 0
