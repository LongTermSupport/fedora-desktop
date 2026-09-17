#!/usr/bin/env bash
# Plan 00124 — triage.bash
#
# CONFIRM THE CHROME SIGNING-KEY FIX STILL HOLDS on this host, and keep the
# diagnostic that found it in the first place.
#
# Issue #45 was two stacked causes: dnf5 validating a URL-installed package
# against the synthetic @commandline repo, which has no keys configured, and a
# Google primary key imported under F41 that never received the signing subkey the
# current Chrome package is signed by. Both are fixed in
# playbooks/imports/play-browsers.yml, driven by helpers/rpm_keys/subkeys.py, and
# Chrome now installs on the affected host.
#
# The live question is PLAN.md Task 4.2: does a SECOND run report the key tasks as
# ok rather than changed? Section 2 gathers exactly the facts that decide it, by
# running the same module the play runs.
# Sections 1 and 3 are the original diagnostic, kept because they are what a
# regression would need.
#
# FACT-FINDING ONLY. It renders no verdict (CLAUDE/PlanScriptStandards.md R9, and
# CLAUDE/AgentNotes.md) — the pass/fail reading belongs in an acceptance gate, not
# here. A non-zero exit from this script means a probe did not answer, NOT that the
# host is broken.
#
# WHERE TO RUN: on the HOST, in a terminal, from this checkout. Enforced by
# plan_require_host (R2), not merely asked for in a comment: the CCY container has
# no rpm keyring and no dnf, so every probe there would answer about the wrong
# machine and answer confidently.
#
# WHAT IT CHANGES: nothing on this host. It installs nothing, removes nothing and
# reconfigures nothing, and no probe needs root. The only writes are into the run
# directory under untracked/plan-runs/ — the run log, the report, and copies of the
# key files being compared. The only network access is a single HTTPS GET of the
# published Google signing key, so the on-host copy can be compared against it.
#
# Usage: ./CLAUDE/Plan/00124-chrome-install-gpg-failure-on-upgraded-host/triage.bash [-h|--help]
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

PLAN_USAGE="usage: triage.bash [-h|--help]

Gathers the facts that say whether Plan 00124's Chrome signing-key fix still
holds on this host:

  1. what this checkout carries — which shape the Chrome tasks in
     play-browsers.yml have, and whether the key-refresh helper is wired in;
  2. the Google signing key on this host — what the rpm keyring holds, whether
     it carries the subkey the current package is signed by, whether the fetched
     key file still matches what Google publishes, and what the play's own
     staleness decision says. This is the section Task 4.2 turns on;
  3. repo files, dnf and the installed package — the original diagnostic, kept
     for a regression.

Host-only and read-only; no probe needs root. Safe to re-run. Writes its report
into untracked/plan-runs/ and names it on completion.

EXIT STATUS
  0  every probe answered
  1  at least one probe could not answer; the failing leg names itself. The
     FACT-FINDING is incomplete — it is not a statement about the host.
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "every fact below comes from this host's rpm keyring, /etc/pki/rpm-gpg and dnf, none of which a container has"

plan_start_log auto

# The report lands in the per-run directory (R10): inside the repo, so the agent
# reads it at the same path the operator sees; under untracked/, so raw host state
# is never committed; and per-run, so a re-run never overwrites the evidence of the
# run before it.
REPORT="${PLAN_RUN_DIR}/plan-00124-chrome-key-report.md"
readonly REPORT

{
    cat <<'HEADER'
# Plan 00124 — does the Chrome signing-key fix still hold?

Generated on the HOST by
CLAUDE/Plan/00124-chrome-install-gpg-failure-on-upgraded-host/triage.bash.

READ THIS FOR: Task 4.2 — whether a second run reports the key tasks as ok rather
than changed. Section 2 holds those facts and says which task each one predicts.

This is fact-finding. It renders no verdict: a section that could not answer says
so by name, and the run then exits non-zero to mean the fact-finding was
incomplete, not that the host is broken.

HEADER
    printf -- '- checkout: %s\n' "${PLAN_REPO_ROOT}"
    printf -- '- run directory: %s\n' "${PLAN_RUN_DIR}"
    printf -- '- generated: %s\n' "$(date --iso-8601=seconds)"
} >"${REPORT}"

plan_gather_leg "what this checkout carries" \
    bash "${PLAN_SCRIPT_DIR}/probe-chrome.bash" "${REPORT}" checkout

plan_gather_leg "the Google signing key on this host (Task 4.2)" \
    bash "${PLAN_SCRIPT_DIR}/probe-chrome.bash" "${REPORT}" key

plan_gather_leg "repo files, dnf and the installed package" \
    bash "${PLAN_SCRIPT_DIR}/probe-chrome.bash" "${REPORT}" host

plan_finish
