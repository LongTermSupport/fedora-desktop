#!/usr/bin/env bash
# clear-ledger-broken.bash — clear the play ledger's BROKEN marker left by the private play
# run on 26-10-12 from a linked worktree. The ledger refused that play as outside the
# worktree's root and marked itself BROKEN; it now skips such a play with one
# LEDGER-PLAY-NOT-TRACKED line instead, so the cause is fixed. Safe: that worktree is gone,
# only a sentinel whose reason is that outside-root refusal is cleared (any other reason
# fails the run and stays), and clearing only says recording may resume (the plays not
# recorded meanwhile stay unrecorded). Changes only the ledger's sentinel under $XDG_STATE_HOME.
#
# WHERE TO RUN: on the HOST, where the ledger lives. Enforced by plan_require_host (R2).
#
# Usage: ./CLAUDE/Plan/00170-unifi-network-tooling/clear-ledger-broken.bash [--check] [-h|--help]
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

PLAN_USAGE="usage: clear-ledger-broken.bash [--check] [-h|--help]

Clears the play ledger's BROKEN marker on THIS machine. --check clears nothing."

plan_mode deploy
plan_parse_common_flags "$@"

plan_require_host "the play ledger is host state under \$XDG_STATE_HOME"
plan_start_log auto

if [[ "${PLAN_CHECK}" == 1 ]]; then
  printf -- '--check: the BROKEN marker is left as it is.\n'
  plan_finish
fi

# python3 -m resolves the helpers package off the current directory. --only-if-reason
# makes the clear exact: no sentinel is a clean no-op, and a sentinel whose reason is not
# the old outside-root refusal is left in place and fails this leg with [FATAL], because
# its cause is not the one fixed here (tested in test_check_freshness.TestClearBroken).
cd "${PLAN_REPO_ROOT}"
plan_deploy_leg "clear the play ledger BROKEN marker (outside-root reason only)" \
  python3 -m helpers.play_ledger.check_freshness --clear-broken \
  --only-if-reason "is outside the repo root"

plan_finish
