#!/usr/bin/env bash
set -euo pipefail

# triage.bash — FACT-FINDING (+ logging) for the rootless podman store, Plan 00062.
#
# Scope: establish grounded facts. It gathers and logs store state; it renders
# NO pass/fail verdict — confirming the store is OK after a fix is a separate
# gate (acceptance.bash / verify.bash), run post-repair.
#
# Run this in YOUR OWN interactive session on the HOST (NOT via Ansible/sudo -u):
# the store's usability depends on your live user session, so probing it here
# gives the true state an `ansible become_user` probe cannot.
#
# It changes NOTHING — no prune, no reset, no removal. Safe to re-run and safe
# with live CCY sessions. Its whole job is to print + log a report, so its
# stdout IS the payload (CLAUDE/StderrHygiene.md exception for report commands).
#
#   CLAUDE/Plan/00062-disk-reclaim-tui/triage.bash
#
# Pattern: CLAUDE/PlanWorkflow.md → "Plan-Local Scripts & Artifacts".
#
# It WRITES ITS OWN REPORT to untracked/reports/ (gitignored scratch inside the
# repo tree). That directory is bind-mounted into the CCY container, so the agent
# assisting on this plan can read the report directly at the same repo-relative
# path — no copy-paste of terminal output required.

REPO_ROOT="$(git rev-parse --show-toplevel)"
STORAGE="$HOME/.local/share/containers/storage"
TEMPDIRS="$STORAGE/overlay/tempdirs"

# Write the full report into the repo's gitignored reports dir AND to the
# terminal. Fixed filename = latest run; readable by the agent at
# untracked/reports/reclaim-podman-triage.log. tee to a file (never /dev/null).
REPORTS_DIR="$REPO_ROOT/untracked/reports"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/reclaim-podman-triage.log"
exec > >(tee "$LOG") 2>&1

LAST_RC=0
# Combined-output capture without hiding errors: prints the command's rc and its
# stdout+stderr. A non-zero rc is DATA here, not a failure — so we branch on it,
# never abort. LAST_RC exposes the rc to the caller for the verdict.
probe() {
    local label="$1"; shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    LAST_RC=$rc
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

echo "════════════════════════════════════════════════════════════"
echo " Plan 00062 — rootless podman store triage"
echo " repo: $REPO_ROOT"
echo " user: $(id -un) (uid $(id -u))   host: $(uname -n)"
echo "════════════════════════════════════════════════════════════"
echo

probe "disk usage of the containers storage filesystem" df -h "$STORAGE"

echo "### stale temp dirs under overlay/tempdirs"
if [ -d "$TEMPDIRS" ]; then
    # Count top-level entries with a glob (no external command → set -e safe).
    shopt -s nullglob dotglob
    _tempentries=("$TEMPDIRS"/*)
    shopt -u nullglob dotglob
    echo "present — ${#_tempentries[@]} top-level entr(y/ies) at $TEMPDIRS"
else
    echo "absent — no overlay/tempdirs directory (already cleared, or never wedged)"
fi
echo

# The critical probe: does the store load in THIS session?
probe "podman system df" podman system df
DF_RC=$LAST_RC
probe "podman info" podman info
probe "podman images" podman images
probe "podman ps (running containers)" podman ps

echo "════════════════════════════════════════════════════════════"
echo " Facts captured"
echo "════════════════════════════════════════════════════════════"
echo " podman system df exit code: $DF_RC   (0 = the store loaded)"
echo " overlay/tempdirs: $([ -d "$TEMPDIRS" ] && echo present || echo absent)"
echo
echo " Report written to: $LOG"
echo " (repo-relative: untracked/reports/reclaim-podman-triage.log — the agent"
echo "  reads it directly; no need to copy-paste anything.)"
