#!/usr/bin/env bash
# probe-launcher.bash — is the host running the launcher we are reading, and where are its
# interactive prompt sites?
#
# THIS IS THE PROBE THAT FAILED. The original hand-rolled triage.bash resolved its repo root
# with `git rev-parse --show-toplevel`, which answers about the CWD. Run by path from
# lts-infra's root it compared the deployed launcher against
# <lts-infra>/files/var/local/claude-yolo/claude-yolo — a path that does not exist there — and
# reported "Could not checksum both files" instead of failing. The check meant to catch
# launcher drift became a shrug.
#
# Two things are fixed here. The root now comes from the R1 bootstrap (script-relative,
# boundary-bounded), so the compared path is always THIS repo's. And "could not determine" is
# now a NON-ZERO exit rather than a printed shrug, so an incomplete comparison can never read
# as a clean one (CLAUDE.md fail-fast; StderrHygiene-adjacent honesty).
#
# Fact-finding only: appends to the report file given as $1, renders no verdict. READ-ONLY.
#
#   ./probe-launcher.bash /tmp/report.md
#
# EXIT CODES:
#   0  every fact was established (including "not deployed", which IS a fact)
#   1  a comparison could not be made — the fact-finding is incomplete
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

REPORT="${1:-}"
if [[ -z "${REPORT}" ]]; then
    printf 'usage: probe-launcher.bash <report-file>\n' >&2
    exit 64
fi

# The deployed tree only exists on the host; inside a container this would compare nothing and
# say so confidently.
plan_require_host "it compares the DEPLOYED launcher tree against this checkout"

DEPLOYED_DIR="/var/local/claude-yolo"
DEPLOYED="${DEPLOYED_DIR}/claude-yolo"
SOURCE="${PLAN_REPO_ROOT}/files/var/local/claude-yolo/claude-yolo"
INCOMPLETE=0

out() { printf '%s\n' "$*" >>"${REPORT}"; }

out ""
out "## Deployed ccy vs this checkout"
out ""
out "Repo root resolved from this SCRIPT (not the cwd): \`${PLAN_REPO_ROOT}\`"
out ""

if [[ ! -e "${SOURCE}" ]]; then
    # Given the bootstrap resolved a real fedora-desktop root, this file must exist. If it
    # does not, the checkout is broken — a fact worth failing on, not working around.
    out "**The checkout is incomplete**: \`${SOURCE}\` does not exist, even though the repo"
    out "root resolved to \`${PLAN_REPO_ROOT}\`. Comparison impossible."
    printf '[INCOMPLETE] checkout missing %s\n' "${SOURCE}" >&2
    exit 1
fi

if [[ ! -f "${DEPLOYED}" ]]; then
    out "\`${DEPLOYED}\` is not present — **ccy is not deployed on this host**. That is a"
    out "definite answer, not a failed check, so nothing further can drift."
else
    dSum=""
    sSum=""
    if ! dSum="$(sha256sum "${DEPLOYED}" 2>&1)"; then
        out "Could not checksum the DEPLOYED launcher: ${dSum}"
        INCOMPLETE=1
    fi
    if ! sSum="$(sha256sum "${SOURCE}" 2>&1)"; then
        out "Could not checksum the CHECKOUT launcher: ${sSum}"
        INCOMPLETE=1
    fi
    if [[ "${INCOMPLETE}" -eq 0 ]]; then
        out '```'
        out "deployed: ${dSum}"
        out "source:   ${sSum}"
        out '```'
        if [[ "${dSum%% *}" == "${sSum%% *}" ]]; then
            out ""
            out "IDENTICAL — line-number citations against this checkout describe the running launcher."
        else
            out ""
            out "**DIFFERENT** — the deployed launcher is not this checkout, so every line-number"
            out "citation describes the checkout rather than what actually runs. Reconcile before"
            out "designing against them (\`git log\` the file, or re-run the deploy play)."
        fi
    fi
fi

# ── prompt-site census ────────────────────────────────────────────────────────────────────
# Counts BOTH spellings. An earlier census searched only `read -rp` and missed nine `read -r -p`
# sites — the entire token-management subsystem — and then reported a total as if it were
# complete. Both patterns, every time.
out ""
out "## Interactive prompt sites"
out ""

census() {
    local label="$1" tree="$2" hits=""
    out "### ${label}"
    out ""
    if [[ ! -d "${tree}" ]]; then
        out "\`${tree}\` is absent — no census possible for this tree."
        return 0
    fi
    out '```'
    if hits="$(grep -rn -E 'read +(-[a-zA-Z]+ +)*-p|read +-[a-zA-Z]*p' "${tree}" 2>&1)"; then
        out "${hits}"
        out ""
        out "count: $(printf '%s\n' "${hits}" | grep -c .)"
    else
        # grep exits 1 for "no matches" and >1 for a real error. Only the latter is a problem.
        local rc=$?
        if [[ "${rc}" -eq 1 ]]; then
            out "no prompt sites matched."
            out "count: 0"
        else
            out "grep failed (exit ${rc}): ${hits}"
            INCOMPLETE=1
        fi
    fi
    out '```'
    return 0
}

census "DEPLOYED tree (${DEPLOYED_DIR}) — what actually runs" "${DEPLOYED_DIR}"
out ""
census "CHECKOUT tree — what we are reading" "${PLAN_REPO_ROOT}/files/var/local/claude-yolo"
out ""
out "The two counts are expected to MATCH. A deployed-only file is a stale orphan from an"
out "older deploy (the deploy copies but does not prune), and reasoning about it as if it were"
out "current is how a fixed defect looks unfixed."

if [[ "${INCOMPLETE}" -ne 0 ]]; then
    printf '[INCOMPLETE] a launcher fact could not be established; see %s\n' "${REPORT}" >&2
    exit 1
fi
printf '==> launcher probes complete: %s\n' "${REPORT}"
