#!/usr/bin/env bash
# Plan 00265 R79: finish the private-name guard's setup on this host, in one run.
#
# WHAT IT CHANGES, in order, failing fast at each step:
#   1. Asks for each private name that is not a team name (a client or project name), one at a
#      time with the echo off, until an empty line, and adds each to private_name_terms in
#      environment/localhost/host_vars/localhost.yml, vault-encrypted, through
#      scripts/vault-list.bash (Go: src-go/internal/vaultlist). A hand-written
#      <git common dir>/private-terms-pinned (the stopgap, which pin refuses) has its names
#      imported in the same step, so none is lost. Existing items stay; nothing else in the file
#      changes; the list is decrypted again after the write to check it. What keeps the names
#      out of every log (this one's and meta-deploy's tee) is that they are read with the echo
#      off and only counts are printed.
#   2. Moves the hand-written stopgap aside, only once step 1 has written and checked the list;
#      it is put back if the play fails, and deleted once pin has written the new list.
#   3. Runs playbooks/imports/play-git-hooks-security.yml, whose qa-private-names pin writes the
#      pinned list again from host_vars, the bus host's served teams and private_name_terms.
#   4. Prints how many names are pinned, never the names.
#
# It pins only the checkout it runs from: pin writes into the git directory of the checkout the
# play runs in. A clone that cannot run plays needs pin run in it by whatever provisions it.
#
# Safe to run twice: a name already listed is not added again, an empty first line adds
# nothing, and a generated pinned list is not the stopgap, so it is neither imported nor removed.
# --check leaves host_vars and the stopgap alone and runs the play with --check.
#
# WHERE TO RUN: on the HOST, at a terminal (meta-deploy runs it as its R79 entry).
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
# Copy this block verbatim into every plan script. See CLAUDE/PlanScriptStandards.md R1.
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

PLAN_USAGE="usage: r79-private-names.bash [--check] [-h|--help]"
plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -ne 0 ]]; then
    printf '[FATAL] unexpected argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi
plan_require_host "it edits this host's vaulted host_vars and runs a play on this host"

play="playbooks/imports/play-git-hooks-security.yml"
commonDir="$(git -C "${PLAN_REPO_ROOT}" rev-parse --path-format=absolute --git-common-dir)"
pinned="${commonDir}/private-terms-pinned"
aside="${pinned}.r79-hand-written"
qaNames="${PLAN_REPO_ROOT}/scripts/qa-private-names.bash"

# pin's own header test decides (qa-private-names pinned-state): absent, generated, hand-written.
pinnedState() { (cd "${PLAN_REPO_ROOT}" && "${qaNames}" pinned-state); }
state="$(pinnedState)"
handWritten=0
if [[ "${state}" == "hand-written" ]]; then
    handWritten=1
fi

if [[ "${PLAN_CHECK:-0}" == "1" ]]; then
    printf '==> --check: private_name_terms and the pinned list are left alone (hand-written: %s)\n' "${handWritten}"
else
    if [[ ! -t 0 ]]; then
        printf '[FATAL] step 1 asks for names at a terminal, and stdin is not one; run this from a terminal\n' >&2
        exit 1
    fi
    importArgs=()
    if [[ "${handWritten}" == "1" ]]; then
        printf '==> the pinned list was written by hand: its names are imported into private_name_terms first\n'
        importArgs=(--import "${pinned}")
    fi
    printf '==> step 1: private names that are not team names (client or project names) into private_name_terms\n'
    if ! "${PLAN_REPO_ROOT}/scripts/vault-list.bash" add private_name_terms "${importArgs[@]}"; then
        printf '[ABORT] private_name_terms was not written and checked (above); the pinned list and host_vars are as they were\n' >&2
        exit 1
    fi
fi

plan_prime_sudo
plan_start_log auto

# The hand-written list is moved aside, not deleted, until pin has written the new one: a failed
# play puts it back, so the guard never runs with no pinned list (its names are already in
# private_name_terms, so a re-run imports nothing new).
restoreHandWritten() {
    if [[ -e "${aside}" && ! -e "${pinned}" ]]; then
        mv -- "${aside}" "${pinned}"
        printf '==> the run did not finish: the hand-written pinned list is back in place; run this again\n' >&2
    fi
}
if [[ "${handWritten}" == "1" && "${PLAN_CHECK:-0}" != "1" ]]; then
    plan_on_cleanup restoreHandWritten
    plan_deploy_leg "move the hand-written pinned list aside (its names are now in private_name_terms)" mv -- "${pinned}" "${aside}"
fi
plan_deploy_leg "write the pinned list (${play})" plan_ansible_playbook "${play}"

if [[ "${PLAN_CHECK:-0}" != "1" ]]; then
    # A play that passed but wrote no list must not leave the guard without one either.
    restoreHandWritten
    state="$(pinnedState)"
    if [[ "${state}" != "generated" ]]; then
        printf '[ABORT] the play passed but the pinned list is %s, not generated\n' "${state}" >&2
        exit 1
    fi
    if [[ -e "${aside}" ]]; then
        rm -f -- "${aside}"
    fi
    printf '==> pinned names: %s (the names are not shown)\n' "$(awk '!/^#/ && NF { n++ } END { print n + 0 }' "${pinned}")"
fi
plan_finish
