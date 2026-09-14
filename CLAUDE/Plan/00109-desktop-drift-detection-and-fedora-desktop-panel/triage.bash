#!/usr/bin/env bash
set -euo pipefail
# triage.bash — FACT-FINDING for Plan 00109. Renders no verdict (PlanScriptStandards R9).
#
# Answers Task 0.2's open question and nothing else: are the orphaned
# /usr/src/evdi-1.14.* source trees reclaimable, and WHO OWNS THEM — an rpm, or
# nothing at all? The cleanup cannot be written until that is known, because the
# two answers need opposite mechanisms: an rpm-owned tree is removed by removing
# the package, and an unowned one by deleting the directory. Guessing would make
# the play either a no-op or a fight with the package manager.
#
# It also records what the host-health probe sees, so a HOST run of Phase 3's
# checks can be compared against what this plan believes about the machine.
#
# It removes nothing, registers nothing and runs no playbook. Not quite read-only,
# and the difference is worth stating rather than glossing: running the login report
# below does a `git fetch` in this checkout (refs only — never the working tree) and
# stamps the fetch clock in the ledger directory. `--no-handoff` keeps it from
# overwriting an existing handoff file, which is the one write an operator might have
# been about to read.
# Its stdout IS the payload (CLAUDE/StderrHygiene.md's report-command exception).
#
#   CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/triage.bash

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

# Every fact here is about THIS machine's DKMS state and rpm database. A container
# has neither, so a run there would report "no orphaned trees" and close Task 0.2
# on the strength of looking at the wrong computer.
plan_require_host 'every fact below is this host DKMS state and rpm ownership; a container has neither'
plan_mode gather
plan_start_log auto

# Record-and-continue: a missing tool is DATA about this host, not a reason to stop
# collecting. Each probe prints its own rc so nothing reads as absent when it was
# merely unasked.
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "${label}" "${rc}" "${out:-(no output)}"
    return 0
}

printf '════════════════════════════════════════════════════════════\n'
printf ' Plan 00109 — DKMS orphan and host-health triage\n'
printf ' running kernel: %s\n' "$(uname -r)"
printf '════════════════════════════════════════════════════════════\n\n'

# ── Task 0.2: the orphaned source trees ──────────────────────────────────────────

# Straight through `probe`, not `ls … || echo "(none)"`: the fallback text would be
# substituting a value for a failure the rc line already reports, and with no trees
# present the unexpanded glob in `ls`'s own error says "none" more precisely.
# `-ld` rather than `-1d` because a tree's date is triage data — it says which
# kernel era left it behind.
probe 'every evdi source tree under /usr/src, with dates' ls -ld /usr/src/evdi-*

probe 'what DKMS currently has registered' dkms status

# The question Task 0.2 turns on. `rpm -qf` on each tree says whether a package
# owns it; "file ... is not owned by any package" is the reclaimable case, and it
# is a NON-ZERO rc, which is why every probe prints its own.
printf '### rpm ownership of each evdi source tree\n'
_found=0
for _tree in /usr/src/evdi-*; do
    [[ -e "${_tree}" ]] || continue
    _found=1
    if _owner="$(rpm -qf "${_tree}" 2>&1)"; then
        printf '%s  OWNED BY  %s\n' "${_tree}" "${_owner}"
    else
        printf '%s  UNOWNED   (%s)\n' "${_tree}" "${_owner}"
    fi
done
if [[ "${_found}" -eq 0 ]]; then
    printf '(no /usr/src/evdi-* trees on this host — Task 0.2 may already be moot)\n'
fi
printf '\n'

# A tree still registered in DKMS is NOT reclaimable whatever rpm says, and this
# is the gate the cleanup must carry. Printed separately so the two facts cannot
# be conflated by a reader.
probe 'DKMS module directories under /var/lib/dkms/evdi' ls -1 /var/lib/dkms/evdi

probe 'the installed displaylink package, whose version tracks evdi not the pin' \
    rpm -q displaylink

# ── Phase 3: what the login-time surface actually sees here ──────────────────────
#
# Not a verdict — the report IS the fact. Run from the repo root so the helpers
# package imports, and with --no-notify so a triage run never pops a desktop
# notification at the operator.
printf '### the login-time health report, as it would run at login\n'
if (cd "${PLAN_REPO_ROOT}" \
    && python3 -m helpers.host_health.login_report --no-notify --no-handoff 2>&1); then
    printf '(exit 0 — the report found nothing to say, which is the clean case)\n'
else
    printf '(exit non-zero — the findings above are what a login would surface)\n'
fi
printf '\n'

# Resolved here rather than inside a `bash -c` string, so the path this run
# actually looked at appears in the log.
_ledgerDir="${XDG_STATE_HOME:-${HOME}/.local/state}/fedora-desktop/play-ledger"
printf '### the play-run ledger, if this host has one yet\n%s\n' "${_ledgerDir}"
if [[ -d "${_ledgerDir}" ]]; then
    probe 'ledger directory contents' ls -la "${_ledgerDir}"
else
    printf '(absent — no play has been run since the callback plugin landed)\n\n'
fi

plan_finish
