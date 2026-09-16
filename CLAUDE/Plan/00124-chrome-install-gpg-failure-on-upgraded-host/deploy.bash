#!/usr/bin/env bash
# Plan 00124 — deploy.bash
#
# PUT THE CHROME SIGNING-KEY FIX ON THIS HOST, then run the play a SECOND time, for
# real, because that second run is the plan's remaining blocking question.
#
# Issue #45 stacked two causes: dnf5 validating a URL-installed package against the
# synthetic @commandline repo, which has no keys configured; and a Google primary key
# imported under F41 that never received the signing subkey the current Chrome package
# is signed by. Both fixes are in playbooks/imports/play-browsers.yml, driven by
# helpers/rpm_keys/subkeys.py.
#
# WHY IT RUNS THE PLAY TWICE. Task 4.1 (Chrome installs) is confirmed; Task 4.2 asks
# what one run cannot: does a SECOND run report the key tasks as ok rather than changed?
# The refresh path erases a key and re-imports it, so a broken idempotency gate does not
# fail — it erases and re-imports for ever while reporting green. Leg 2 IS that second
# run, so the same command that does the work produces the evidence.
#
# WHY LEG 2 IS REAL AND NOT --check. Check mode cannot answer Task 4.2 for the two tasks
# the question is about: get_url issues a HEAD and compares the sha1 of that empty body
# against the file on disk, so unless the server answers 304 it reports `changed` for an
# identical file; and the erase is a `command:`, which check mode SKIPS, so a dry run
# cannot tell "the loop was empty" from "it did not run" (the play says so itself).
# acceptance.bash judges those two from host state and observes the rest from a check
# run. Run it after this.
#
# WHAT THIS CHANGES: everything play-browsers.yml does, which is more than this plan's
# part — the change gate below names it. The part that is not a file edit: a stale Google
# key is ERASED FROM THE RPM KEYRING and the published one IMPORTED INTO IT. That keyring
# is what rpm and dnf verify every package against, and the change persists.
#
# play-nvidia.yml is deliberately NOT run. This plan added the same shared
# tasks/ensure-gnupg2.yml include to it (Task 3.3), and the play below already reaches
# that outcome; running a driver play would change things this plan does not need changed.
#
# WHERE TO RUN: on the HOST, in a terminal, from this checkout. Enforced by
# plan_require_host (R2) — Ansible is never run in the CCY container.
#
# Usage: ./CLAUDE/Plan/00124-chrome-install-gpg-failure-on-upgraded-host/deploy.bash [-h|--help] [--check] [-y|--yes]
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

readonly PLAY="playbooks/imports/play-browsers.yml"

PLAN_USAGE="usage: deploy.bash [-h|--help] [--check] [-y|--yes]

Runs playbooks/imports/play-browsers.yml on the HOST, TWICE:

  leg 1  converge — fetch Google's published signing key, erase a STALE copy from
         the rpm keyring and import the current one, take ownership of
         /etc/default/google-chrome and /etc/yum.repos.d/google-chrome.repo, and
         install Chrome, Brave and Vivaldi if absent.
  leg 2  the SAME play again, unchanged. This is PLAN.md Task 4.2: a second run
         must report the key tasks as ok rather than changed. Read leg 2's task
         lines and PLAY RECAP in the run log.

--check previews leg 1 and SKIPS leg 2, because a dry run cannot answer Task 4.2:
get_url compares a HEAD response body against the file, and the erase is a command:
that check mode skips. -y/--yes consents to the change gate non-interactively.

Then run acceptance.bash, which renders the verdict.

EXIT STATUS
  0  every leg completed
  1  a leg failed — nothing after it ran (deploy legs are fail-fast)
 64  usage error"

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's rpm keyring, /etc/pki/rpm-gpg, /etc/yum.repos.d and dnf"
plan_prime_sudo
plan_start_log auto

plan_gate_change "gnupg2 installed if absent; Google's published signing key written to /etc/pki/rpm-gpg/RPM-GPG-KEY-google-chrome; a STALE Google key ERASED FROM THIS HOST'S RPM KEYRING and the published one IMPORTED INTO IT — that keyring is what rpm and dnf verify every package against, and the change persists; /etc/default/google-chrome written with repo_add_once=false so Chrome's own scriptlet stops re-adding its repository; /etc/yum.repos.d/google-chrome.repo written to point at the local key; and google-chrome-stable, brave-browser and vivaldi-stable installed if absent, the last two adding their own repositories"

plan_deploy_leg "${PLAY} — leg 1, converge" \
    plan_ansible_playbook "${PLAY}"

if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '==> [--check] leg 2 SKIPPED. It exists to answer Task 4.2 — does a second run\n'
    printf '==> report the key tasks as ok rather than changed? — and a dry run cannot\n'
    printf '==> answer that: get_url compares a HEAD response body against the file on\n'
    printf '==> disk, and the erase is a command: that check mode skips. Re-run without\n'
    printf '==> --check to produce that evidence.\n'
else
    plan_deploy_leg "${PLAY} — leg 2, the second run PLAN.md Task 4.2 asks for" \
        plan_ansible_playbook "${PLAY}"

    printf '\n==> LEG 2 IS THE EVIDENCE. In its output above, these tasks must read ok,\n'
    printf '==> not changed:\n'
    printf '==>   Fetch Google%s Published Signing Key\n' "'"
    printf '==>   Remove The Stale Google Signing Key   (no items — the loop is empty)\n'
    printf '==>   Import Google Chrome Signing Key\n'
    printf '==>   Stop Chrome%s Scriptlet Re-Adding Its Own Repository\n' "'"
    printf '==>   Add Google Chrome Repository\n'
    printf '==>   Install Google Chrome\n'
    printf '==> acceptance.bash renders the verdict on that; it does not need this log.\n'
fi

plan_finish
