#!/usr/bin/env bash
# Plan 00203 — acceptance.bash: the automated part of milestone M2 (SPEC-hot-control.md section
# 6, M2 a-f), the launcher and entrypoint half of hot control. The owner's part (what the
# screen shows, the sessions to start) is ACCEPTANCE.md, "M2".
#
# RUN ON THE HOST, as the desktop user, on an SELinux Enforcing host, after play-claude-yolo.yml
# (which the meta-deploy entry for this plan runs first), through CLAUDE/Plan/meta-deploy.bash or
#   ./CLAUDE/Plan/00203-teams-hot-control-and-robustness/acceptance.bash
#
# WHAT IT CHANGES: nothing. It inspects three running ccy sessions the owner started in ccy's
# tmux (ACCEPTANCE.md): a plain `ccy`, a `ccy --teams <seat>@<team>` and a `ccy --no-bus`. The
# only writes it attempts are into the plain session's read-only membership mount, which must
# fail (H3).
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (a leg failed, and names itself); 2 COULD NOT ESTABLISH
# (a session to inspect is not running; nothing was looked at); 64 usage.
set -euo pipefail

# CHECKS (each a gather leg; m2-check.bash holds the probes):
#   SELinux    the host is Enforcing
#   (a)        the --teams session holds every desired seat; its active flag is up
#   (b)        the plain session reads /run/ccy/membership/membership.json (no seat); a touch,
#              a write and a remount there fail (H3); no active flag; the guarded status line
#   (c)        a prompt's hook in the plain session ends in the shell (no python3, no pingbus);
#              at most one pingbus process (the keeper); idle CPU as the --no-bus session's
#   (d)        the session image is the launcher's REQUIRED_CONTAINER_VERSION, with the M2
#              entrypoint
#   (e)        the --no-bus session: PINGBUS_MEMBERSHIP=off, no mount, no pingbus, no plugin or
#              settings
#   (f)        the H34 spike's result is in the spec

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

Plan 00203 M2, the automated acceptance of the bus kit in every ccy session: inspects a plain
ccy, a ccy --teams and a ccy --no-bus session, each started in ccy's tmux and left running
(ACCEPTANCE.md, M2). Changes nothing.
Exit 0 ACCEPTED, 1 REJECTED, 2 COULD NOT ESTABLISH (a session is not running), 64 usage."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -ne 0 ]]; then
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it inspects the host's running ccy containers and its SELinux mode"
CHECK=("${PLAN_SCRIPT_DIR}/m2-check.bash")

sessions_rc=0
sessions="$("${CHECK[@]}" find)" || sessions_rc=$?
if [[ "${sessions_rc}" -ne 0 ]]; then
    exit "${sessions_rc}"
fi
plan_start_log auto

plain="$(awk '$1 == "plain" { print $2 }' <<<"${sessions}")"
seated="$(awk '$1 == "seated" { print $2 }' <<<"${sessions}")"
nobus="$(awk '$1 == "nobus" { print $2 }' <<<"${sessions}")"
printf '==> sessions inspected:\n%s\n' "${sessions}" | tee "${PLAN_RUN_DIR}/m2-sessions-report.txt"

plan_gather_leg "SELinux Enforcing" "${CHECK[@]}" selinux
plan_gather_leg "(a) ccy --teams holds its seats" "${CHECK[@]}" seated "${seated}"
plan_gather_leg "(b)(c) a plain ccy carries an inert kit and a read-only membership" "${CHECK[@]}" plain "${plain}"
plan_gather_leg "(c) idle CPU unchanged" "${CHECK[@]}" cpu "${plain}" "${nobus}"
plan_gather_leg "(d) the image the launcher requires" "${CHECK[@]}" image "${plain}"
plan_gather_leg "(e) --no-bus loads nothing" "${CHECK[@]}" nobus "${nobus}"
plan_gather_leg "(f) the spike's result is recorded" "${CHECK[@]}" spike "${PLAN_SCRIPT_DIR}/SPEC-hot-control.md"
plan_finish
