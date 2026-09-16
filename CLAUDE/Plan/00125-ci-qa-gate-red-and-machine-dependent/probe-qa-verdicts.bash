#!/usr/bin/env bash
set -euo pipefail
# probe-qa-verdicts.bash — the probe body for Plan 00125's triage.bash.
#
#   probe-qa-verdicts.bash <run-dir> <repo-root> [branch]
#
# Runs ./scripts/qa-all.bash here, downloads the newest COMPLETED QA workflow log for a
# branch, and puts the two side by side stage by stage. Renders no verdict — that is
# triage.bash's contract and this is its body, kept in its own file so each probe is
# independently runnable and so shellcheck can see every call site.
#
# Everything it writes goes under <run-dir>, which is gitignored and per-run.

if [[ "$#" -lt 2 ]]; then
    printf '[FATAL] usage: %s <run-dir> <repo-root> [branch]\n' "$(basename "$0")" >&2
    exit 1
fi

RUN_DIR="$1"
REPO_ROOT="$2"
BRANCH="${3:-}"

QA_HERE="${RUN_DIR}/qa-all-here.txt"
QA_THERE="${RUN_DIR}/qa-all-ci.txt"
REPORT="${RUN_DIR}/stage-verdict-report.txt"

# A missing tool is an IaC gap, not something to engineer around
# (CLAUDE.md, "Missing Dependencies"). Degrading to a local-only report would print a
# table with one side blank, which reads as "no divergence" — the misleading empty
# result CLAUDE/PlanTriage.md names.
require_tool() {
    local tool="$1" why="$2"
    if ! command -v "${tool}" > /dev/null; then
        printf '[FATAL] %s is not installed, and %s\n' "${tool}" "${why}" >&2
        exit 1
    fi
}

# ── which machine is this? ───────────────────────────────────────────────────────
#
# Reported, never assumed. Every difference below is a difference BETWEEN two machines,
# so a report that does not say which one it ran on is unreadable next to another copy
# of itself.
printf '### which machine this is\n'
printf 'kernel        %s\n' "$(uname -r)"
printf 'uid           %s\n' "$(id -u)"
printf 'python3       %s\n' "$(python3 --version)"
if [[ -e /run/.containerenv ]]; then
    printf 'location      container (/run/.containerenv)\n'
elif [[ -e /.dockerenv ]]; then
    printf 'location      container (/.dockerenv)\n'
elif [[ -n "${container:-}" ]]; then
    printf 'location      container (the container environment variable is set)\n'
else
    printf 'location      host (no container marker)\n'
fi
# The uid-derived runtime socket decides which D-Bus branch the gnome helpers take, and
# its presence is exactly what differed between a container and a runner.
if [[ -S "/run/user/$(id -u)/bus" ]]; then
    printf 'session bus   present for this uid\n'
else
    printf 'session bus   none for this uid\n'
fi
printf '\n'

# ── what qa-all.bash says here ───────────────────────────────────────────────────
#
# A non-zero exit is DATA: a red local run is one of the two things being compared, not
# a failure of the fact-finding. Captured to a file so the whole run lands in the report
# directory for a human (PlanScriptStandards R10).
printf '### qa-all.bash on this machine\n'
qa_rc=0
(cd "${REPO_ROOT}" && ./scripts/qa-all.bash) > "${QA_HERE}" 2>&1 || qa_rc=$?
printf 'exited %d; full output: %s\n\n' "${qa_rc}" "${QA_HERE}"

# ── what the last completed CI run said ──────────────────────────────────────────
printf '### the CI side\n'
require_tool gh 'the CI half of this comparison cannot be fetched without it'
require_tool jq 'the run list is JSON'

if [[ -z "${BRANCH}" ]]; then
    BRANCH="$(cd "${REPO_ROOT}" && git rev-parse --abbrev-ref HEAD)"
fi
printf 'newest COMPLETED QA run on %s\n' "${BRANCH}"

runs="$(cd "${REPO_ROOT}" && gh run list --branch "${BRANCH}" --workflow QA \
    --limit 20 --json databaseId,headSha,status,conclusion)"
newest="$(printf '%s' "${runs}" | jq -r '[.[] | select(.status=="completed")][0]')"
run_id="$(printf '%s' "${newest}" | jq -r '.databaseId // ""')"
head_sha="$(printf '%s' "${newest}" | jq -r '.headSha // ""')"
conclusion="$(printf '%s' "${newest}" | jq -r '.conclusion // ""')"

if [[ -z "${run_id}" || "${run_id}" == "null" ]]; then
    printf '[FATAL] no completed QA run found on %s in the last 20\n' "${BRANCH}" >&2
    exit 1
fi
ci_label="CI run ${run_id} (${head_sha:0:8}, ${conclusion})"
printf '%s\n' "${ci_label}"

# Without this, a checkout that is merely AHEAD of the compared commit shows up as a
# divergence on every stage that counts files, and a reader chases four phantom rows
# before reaching the two that matter.
local_head="$(cd "${REPO_ROOT}" && git rev-parse HEAD)"
dirty="$(cd "${REPO_ROOT}" && git status --porcelain)"
dirty_note=""
if [[ -n "${dirty}" ]]; then
    dirty_note=" plus uncommitted changes"
fi
if [[ "${local_head}" != "${head_sha}" || -n "${dirty}" ]]; then
    printf '\nNOTE: this checkout is not the tree CI ran.\n'
    printf '  here  %s%s\n' "${local_head:0:8}" "${dirty_note}"
    printf '  CI    %s\n' "${head_sha:0:8}"
    printf '  A stage that COUNTS things (files, tests) can differ for that reason alone.\n'
    printf '  Commit and push, then re-run, before treating a count as machine-dependent.\n'
fi

# The whole log, not --log-failed. A stage that PASSED in CI and differs here is exactly
# the case a failure-only capture cannot see, and it is the case that hid the js stage
# counting a different number of files on each machine.
(cd "${REPO_ROOT}" && gh run view "${run_id}" --log) > "${QA_THERE}" 2>&1

# An empty capture would compare as "CI ran no stages", reading as a divergence on every
# row rather than as a broken download.
if [[ ! -s "${QA_THERE}" ]]; then
    printf '[FATAL] the downloaded CI log is empty: %s\n' "${QA_THERE}" >&2
    exit 1
fi
printf 'full CI log: %s\n\n' "${QA_THERE}"

# ── the comparison ───────────────────────────────────────────────────────────────
#
# The parsing and diffing live in a tested helper rather than here: text parsing is the
# "complex logic" helpers/CLAUDE.md requires be extracted and driven by tests.
printf '### stage by stage\n'
(cd "${REPO_ROOT}" && python3 -m helpers.qa_environment.verdicts \
    --here "${QA_HERE}" --there "${QA_THERE}" \
    --here-label "this machine ($(uname -r))" \
    --there-label "${ci_label}") | tee "${REPORT}"
