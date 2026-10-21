#!/usr/bin/env bash
# Plan 00209 — acceptance-s1.bash: the host check of stage 1 (ccy-teams reads the v1 team, seat
# and declare state in-process), after play-claude-yolo.yml has installed the new ccy-teams.
#
#   S0 the installed ~/.local/bin/ccy-teams is byte for byte this checkout's build, and the
#      checkout has stage 1;
#   S1 the ccy-teams from before stage 1 is built into the run directory from git: the first
#      parent of the first commit on HEAD's first-parent line that brings src-go/internal/bus in.
#      Building it, rather than keeping a copy of the installed binary aside before the play,
#      needs no run before the play and no ordering in meta-deploy.list, and the old binary is
#      always the one stage 1 replaced, whatever was installed. Both run this checkout's
#      helpers/agent_bus/declare.py (the old one has this checkout baked in, as the play bakes
#      it), so the comparison is old Go against new Go over the current Python: right for stage
#      1, which changed no Python, and a later stage that changes Python cannot reuse it as is;
#   S2 `--list` from the old and the new binary, a pair a round for three rounds (old first in
#      odd rounds, new first in even ones): a difference in bytes or exit code in every round is
#      a departure, one in some rounds only is a live change between the halves of a pair and
#      a note; the new one is faster (median, with the load average beside it). The old
#      figures were 9-23 s on this host;
#   S3 an open of each, in the same rounds, timed to its first screen. PATH starts with
#      _s1-shim.bash under the names env, agent-bus, fzf and gum, which log every declare.py run
#      and every agent-bus call and answer the first screen with Esc. Each new open starts
#      exactly one `declare status` and no `agent-bus seat list`; the old opens must start a
#      seat list and more than one declare.py, which proves the shims see them;
#   S4 every seat whose live member.json sets limits.report_per_hour, in a checkout a session
#      record names, is on the new `--list` and not refused.
#
# RUN ON THE HOST, in a terminal, as the desktop user (not root): the opens draw on /dev/tty, so
# meta-deploy.bash must run in one. The one prompt is sudo's, before the log opens (R3), so the
# member rosters read the same for both binaries. If ccy-teams asks a question when it opens (a
# team to describe, an update to apply), S3 is not judged: answer it in ccy-teams and run again.
# An update offer comes back on every open while a team is out of date, so S3 cannot be judged
# until play-agent-bus.yml has applied the update (its meta-deploy.list entry runs earlier).
#
# WHAT IT CHANGES: nothing. It builds two binaries and keeps every output in its run directory.
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (each failed check is named); 2 NOT JUDGED (nothing failed,
# but S3 or S4 had no evidence to judge on: an open asked a question first, no old open showed
# the shims seeing a seat list, no terminal, or no live member.json sets report_per_hour; each
# named), which meta-deploy.bash reports as COULD NOT ESTABLISH; 64 usage.
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

PLAN_USAGE="usage: acceptance-s1.bash [-h|--help]

Plan 00209 stage 1 on the host: the installed ccy-teams is this checkout's (S0); against the
ccy-teams from before stage 1, built from git (S1), --list prints the same bytes and is faster
(S2), an open is faster and starts one declare status and no seat list (S3); a live member.json
with report_per_hour set is listed (S4). Exit 0 ACCEPTED, 1 REJECTED, 2 NOT JUDGED (nothing
failed, but S3 or S4 had no evidence), 64 usage. Prompts for sudo once, before the run log
opens; run it in a terminal."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: every check only reads\n' >&2
    exit 64
fi

plan_require_host "it times the host's installed ccy-teams on the live teams, checkouts and sessions"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: ccy-teams reads that user'\''s sessions\n' >&2
    exit 1
fi
for tool in go git tar timeout gum agent-bus sha256sum cmp diff paste; do
    if ! command -v "${tool}" >/dev/null; then
        printf '[FATAL] %s is not installed: its play has not run on this host\n' "${tool}" >&2
        exit 1
    fi
done

INSTALLED="${HOME}/.local/bin/ccy-teams"
REAL_AGENT_BUS="$(command -v agent-bus)"
REAL_GUM="$(command -v gum)"
RUNS=3
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-s1.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-s1.inc.bash"

plan_prime_sudo
plan_start_log auto
REPORT="${PLAN_RUN_DIR}/acceptance-s1-report.md"
NEW_REF="${PLAN_RUN_DIR}/ccy-teams-checkout"
OLD_BIN="${PLAN_RUN_DIR}/ccy-teams-before-stage-1"
SHIMS="${PLAN_RUN_DIR}/shims"
printf '# Plan 00209 stage 1 acceptance\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"

plan_gather_leg "S0 the installed ccy-teams is this checkout's build" check_installed
plan_gather_leg "S1 the ccy-teams from before stage 1, built from git" build_old
plan_gather_leg "S2 --list prints the same bytes, faster" check_list
plan_gather_leg "S3 an open is faster and starts one declare status and no seat list" judge S3 check_open
plan_gather_leg "S4 a live member.json with report_per_hour set is listed" judge S4 check_report_per_hour

if [[ -n "${PLAN_FAILED_LEGS}" ]]; then
    printf '\nREJECTED: %s%s\n' "${PLAN_FAILED_LEGS}" "${NOT_JUDGED:+ (not judged: ${NOT_JUDGED})}" | tee -a "${REPORT}"
elif [[ -n "${NOT_JUDGED}" ]]; then
    # plan_finish knows only failed legs and would exit 0; a check with no evidence is exit 2.
    printf '\nNOT JUDGED: %s (nothing failed; the reasons are in the run log)\n' "${NOT_JUDGED}" | tee -a "${REPORT}"
    plan_list_reports
    printf '==> run log: %s\n' "${PLAN_RUN_LOG}"
    exit 2
else
    printf '\nACCEPTED: stage 1 lists the same, opens faster and starts no Python seat list\n' | tee -a "${REPORT}"
fi
plan_finish
