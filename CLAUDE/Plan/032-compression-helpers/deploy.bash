#!/usr/bin/env bash
# Plan 032 — deploy.bash
#
# Installs the `ouch` backend and the compress/uncompress wrappers on this host, which is
# the single thing PLAN.md has been waiting on since it was filed. Phases 1-3 are done;
# Phase 4 was six things for a human to try by hand, and acceptance.bash now asserts all
# six, so this script plus that gate is the whole remainder.
#
# WHAT THIS CHANGES: the pinned ouch static binary is downloaded to /opt/ouch-<version>/,
# and /usr/local/bin/compress and /usr/local/bin/uncompress are written 0755 root:root.
# The part that is not a file copy — /usr/local/bin/ouch is a SYMLINK written with
# force:true, so an existing ouch symlink at that path is REPOINTED at the pinned version.
# No archive anywhere on this host is created, extracted or modified.
#
# The play PREFLIGHTS the ncompress package and refuses while it is installed, because that
# package ships /usr/bin/compress and /usr/bin/uncompress. That refusal is the play's, not
# this script's, and it is a hard failure rather than a warning — so a host with ncompress
# stops here with the dnf command to run, instead of installing wrappers that something
# else shadows.
#
# WHERE TO RUN: on the HOST, in a terminal, from this checkout. Enforced by
# plan_require_host (R2) — Ansible is never run in the CCY container.
#
# Usage: ./CLAUDE/Plan/032-compression-helpers/deploy.bash [-h|--help] [--check] [-y|--yes]
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

readonly PLAY="playbooks/imports/optional/common/play-compression-helpers.yml"

PLAN_USAGE="usage: deploy.bash [-h|--help] [--check] [-y|--yes]

Runs ${PLAY} on the HOST:

  * downloads the pinned ouch static musl binary into /opt/ouch-<version>/ and
    symlinks it to /usr/local/bin/ouch, asserting the version matches the pin
  * installs files/usr/local/bin/{compress,uncompress} to /usr/local/bin, 0755 root
  * refuses outright if the ncompress package is installed, because it ships
    /usr/bin/compress and would shadow the wrappers

--check previews without changing anything. -y/--yes consents to the change gate
non-interactively, which is what untracked/meta-deploy.bash passes when it runs
this as part of a batch.

Then run acceptance.bash, which renders the verdict and covers PLAN.md Phase 4's
six user-testing items.

EXIT STATUS
  0  the play completed
  1  the play failed
 64  usage error"

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible, which writes to /opt, /usr/local/bin and queries this machine's rpm database"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "${PLAY} — leg 1, converge" \
    plan_ansible_playbook "${PLAY}"

# Leg 2 is the Success Criterion "playbook is idempotent (second run is a no-op)", and it
# is here rather than in acceptance.bash because a second converge CHANGES STATE — a
# read-only gate must not run it. acceptance.bash says so in its own help text rather than
# quietly omitting the criterion.
#
# Not --check either: check mode SKIPS command: tasks, so a dry run cannot tell "this task
# would do nothing" from "this task was not evaluated". The `ouch --version` verify step is
# exactly such a task.
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '==> [--check] leg 2 SKIPPED. It exists to answer the idempotency criterion,\n'
    printf '==> and check mode skips command: tasks — so a dry run cannot distinguish a\n'
    printf '==> task that would do nothing from one that was never evaluated.\n'
else
    plan_deploy_leg "${PLAY} — leg 2, the idempotency run" \
        plan_ansible_playbook "${PLAY}"

    printf '\n==> LEG 2 IS THE IDEMPOTENCY EVIDENCE. Its PLAY RECAP must report changed=0.\n'
    printf '==> The unarchive is guarded by creates: and both copies compare content, so\n'
    printf '==> any changed=N above zero on a second run names a task that is not\n'
    printf '==> declarative yet.\n'
fi

printf '\n==> Next: acceptance.bash. It asserts PLAN.md Phase 4 items 2-7 against the\n'
printf '==> wrappers this run just installed, plus the --gz/--7z and conflicting-flag\n'
printf '==> criteria, so the plan closes on a run rather than on somebody trying six\n'
printf '==> things by hand.\n'

plan_finish
