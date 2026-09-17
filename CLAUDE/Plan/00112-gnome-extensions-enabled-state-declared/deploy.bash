#!/usr/bin/env bash
# Plan 00112 — deploy.bash
#
# PURPOSE: run the play this plan changed, so the declared enabled list lands on the HOST.
# HOST ONLY (CLAUDE/PlanScriptStandards.md R2) — Ansible never runs in the CCY container.
#
# EFFECT ON THE HOST: play-gnome-shell-extensions.yml installs the extension installer and
# its DNF packages, fetches the seven extensions.gnome.org extensions (skipping any already
# current), compiles their GSettings schemas, copies the custom extension, and then — the
# part this plan added — writes every deployed UUID into org.gnome.shell enabled-extensions,
# ADDITIVELY: nothing already in the list is removed. It then verifies every deployed UUID's
# live state and writes the Space Bar and Dash to Dock dconf keys.
#
# Run triage.bash BEFORE and AFTER this, and diff the two reports: that is how Task 2.1's
# "the host's own list is unchanged (nothing removed)" is evidenced rather than asserted.
# Run this twice to evidence idempotency — the second run must report no change for the
# 'Declare Deployed Extensions Enabled' task.
#
# Usage: ./deploy.bash [-h|--help] [--check]
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

PLAN_USAGE="usage: deploy.bash [-h|--help] [--check]

Runs two plays on the HOST, in this order:
  1. play-gnome-shell-extensions.yml, twice. The change this plan makes is the
     'Declare Deployed Extensions Enabled' task: every deployed UUID is added to
     org.gnome.shell enabled-extensions, additively. The second run asserts
     changed=0, which is Task 2.1's idempotency requirement.
  2. play-vm-test-lab.yml, because vmtest copies its guest checker from the
     host's DEPLOYED copy — so Task 2.2 certifies nothing until this lands.

--check previews without changing anything and skips the idempotency pass.

Run triage.bash before and after, and diff the reports."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's GNOME session and dconf database"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-gnome-shell-extensions.yml" \
    plan_ansible_playbook playbooks/imports/play-gnome-shell-extensions.yml

# ── second pass: idempotency, asserted rather than left to the operator ───────────────────
# Task 2.1 asks for the play to run TWICE, because one run cannot distinguish a play that
# settles from a play that churns — and a play that REMOVED something only on a re-run would
# look identical to a single clean run's evidence. Running it here rather than asking the
# operator to remember is what lets the batch harness discharge the task: it invokes
# deploy.bash once, with no arguments.
#
# The recap is parsed, not eyeballed. `changed=0` on every host is the whole claim.
#
# This leg is deliberately NOT plan_deploy_leg: the verdict comes from the log's recap, so
# the run must continue far enough to read it even when ansible-playbook exits non-zero.
# Every path below ends in an explicit exit — a non-zero run, a missing log, an absent recap
# and a non-zero changed count are four different failures and each says which it is.
if [[ "${#PLAN_CHECK_ARGS[@]}" -gt 0 ]]; then
    printf '\n[skip] idempotency second pass — --check changes nothing, so a second\n' >&2
    printf '       preview could only repeat the first.\n' >&2
else
    _plan_banner "[deploy leg] play-gnome-shell-extensions.yml (second pass)"
    secondLog="${PLAN_RUN_DIR}/idempotency-second-pass.log"
    secondStatus=0
    plan_ansible_playbook playbooks/imports/play-gnome-shell-extensions.yml \
        > "${secondLog}" 2>&1 || secondStatus=$?

    if [[ -s "${secondLog}" ]]; then
        cat "${secondLog}"
    fi

    if [[ "${secondStatus}" -ne 0 ]]; then
        printf '\n[ABORT] the idempotency second pass exited %s. The first pass changed the\n' "${secondStatus}" >&2
        printf '        host, so this is a play that breaks on the state it just created.\n' >&2
        exit 1
    fi

    if [[ ! -s "${secondLog}" ]]; then
        printf '[FATAL] the second pass produced no log at %s. Unproven is not proven.\n' "${secondLog}" >&2
        exit 1
    fi

    if ! grep -q '^PLAY RECAP' "${secondLog}"; then
        printf '[FATAL] no PLAY RECAP in %s — the run never reached its own summary, so\n' "${secondLog}" >&2
        printf '        there is no changed count to read.\n' >&2
        exit 1
    fi

    # Count the host lines before judging them. `grep | grep -qv` on an EMPTY recap finds
    # nothing to object to and passes — a verdict of "no host changed" derived from no hosts
    # at all, which is the 0-of-0 vacuous pass this repo keeps re-finding. The population is
    # asserted first, so the assertion can only be made about something.
    recapHosts=0
    while IFS= read -r _line; do
        recapHosts=$((recapHosts + 1))
    done < <(grep -E '^[^ ]+ +: +ok=' "${secondLog}")

    if [[ "${recapHosts}" -eq 0 ]]; then
        printf '[FATAL] the second pass recap in %s lists no hosts. "Nothing changed" across\n' "${secondLog}" >&2
        printf '        zero hosts is not evidence of convergence.\n' >&2
        exit 1
    fi

    if grep -E '^[^ ]+ +: +ok=' "${secondLog}" | grep -qv 'changed=0 '; then
        printf '\n[FATAL] the second pass reported changes — the play does not converge:\n' >&2
        grep -E '^[^ ]+ +: +ok=' "${secondLog}" >&2
        printf '\n        Task 2.1 asks for no change on the second run. A play that changes\n' >&2
        printf '        something every time cannot evidence that it removed nothing.\n' >&2
        exit 1
    fi

    printf '\n[idempotency] second pass: changed=0 on each of %s host(s).\n' "${recapHosts}"
fi

# ── the lab, because Task 2.2 is meaningless without it ───────────────────────────────────
# `vmtest` copies its guest checker from the HOST'S DEPLOYED COPY, not from the checkout
# under test, so until this play runs the lab certifies a run against a checker that
# predates this plan — which is how `20260914T100220Z` passed 16/16 while declaring
# `COVERAGE: 8 of 1`. Task 2.1 asks for this play by name for that reason.
#
# It is here rather than left to the operator because of the precedent one plan over:
# Plan 00094 changed a file and deployed only the other play, so the repo fix never
# reached the host and stayed broken for weeks. A deploy that covers one of the two plays
# its own plan changed is the same omission waiting to repeat.
#
# After the idempotency pass, not before: that assertion is about the extensions play
# converging, and a second play in between would put its changes in the recap being judged.
plan_deploy_leg "play-vm-test-lab.yml" \
    plan_ansible_playbook playbooks/imports/optional/common/play-vm-test-lab.yml

plan_finish
