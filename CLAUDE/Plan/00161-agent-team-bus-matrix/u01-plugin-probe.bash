#!/usr/bin/env bash
# u01-plugin-probe.bash — probe U01 (DESIGN.md sections 6 and 13): does Claude Code load the
# hooks of a plugin installed the phpantom-lsp way (copied into <config>/plugins/<name> and
# enabled with a bare enabledPlugins key)? The answer picks the D13 route: the plugin, or
# the fallback of user-level hooks merged into settings.json by the entrypoint.
#
# Changes nothing outside its own run directory: the throwaway plugin and a scratch Claude
# config directory are built under untracked/plan-runs/, and the child claude is pointed at
# that directory, never at the live /root/.claude. Read-only and safe to re-run.
#
# A child turn spends subscription quota (one short haiku turn per variant). Without
# CCY_CHILD_CLAUDE (ccy-claude on PATH) the child has no credential, and the report says
# "undetermined" for any question the failed turn cannot answer.
#
# WHERE TO RUN: INSIDE a CCY container (plan_require_container), ideally one with
# CCY_CHILD_CLAUDE=1 in ccy.env.
#
# Usage: ./CLAUDE/Plan/00161-agent-team-bus-matrix/u01-plugin-probe.bash [-h|--help]
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
  if [[ -e "${repoRoot}/.git" ]]; then
    printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
    exit 1
  fi
  repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: u01-plugin-probe.bash [-h|--help]"
plan_mode gather
plan_parse_common_flags "$@"

plan_require_container "it asks how the Claude Code inside the CCY image loads plugin hooks"
plan_start_log auto

REPORT="${PLAN_RUN_DIR}/u01-report.txt"
RUN="${PLAN_SCRIPT_DIR}/u01-plugin-probe-run.bash"
plan_gather_leg "checker self-test" "${PLAN_SCRIPT_DIR}/test-u01-plugin-probe-check.bash"
# Controls first: the plugin verdict is read against the user-settings control of the same cwd.
for where in neutral project; do
  plan_gather_leg "${where} cwd: control, hooks in user settings.json (the D13 fallback)" \
    "${RUN}" "${PLAN_RUN_DIR}/${where}-user-settings" user-settings "${where}" "${REPORT}"
  plan_gather_leg "${where} cwd: control, plugin loaded with --plugin-dir" \
    "${RUN}" "${PLAN_RUN_DIR}/${where}-plugin-dir" plugin-dir "${where}" "${REPORT}"
  plan_gather_leg "${where} cwd: plugin installed the phpantom-lsp way (the D13 route)" \
    "${RUN}" "${PLAN_RUN_DIR}/${where}-plugin" plugin "${where}" "${REPORT}" \
    "${PLAN_RUN_DIR}/${where}-user-settings"
done
plan_finish
