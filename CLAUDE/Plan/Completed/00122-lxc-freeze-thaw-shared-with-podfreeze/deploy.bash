#!/usr/bin/env bash
# Plan 00122 — deploy.bash
#
# PURPOSE: put this plan's three artefacts on the HOST — the shared freeze library and
# the two tools that source it. HOST ONLY (CLAUDE/PlanScriptStandards.md R2): Ansible
# never runs in the CCY container, and every remaining task in this plan is blocked on
# exactly that (Task 4.6, and the freeze/thaw success criteria, all need a real engine).
#
# EFFECT ON THE HOST — what actually changes:
#   - fzf is installed where absent. It belongs to the LIBRARY, not to either tool
#     (tasks/deploy-freeze-lib.yml, included by both plays), because the library is what
#     chooses between the fzf picker and the numbered menu.
#   - ~/.local/lib/freeze/ is created and freeze-common.bash written into it, mode 0644:
#     it is sourced, never executed.
#   - ~/.local/bin/podfreeze and ~/.local/bin/lxcfreeze are written, mode 0755.
#   - the pre-rename ~/.local/bin/podman-freeze is removed if it is still present.
#   - ~/.local/bin/ is created if it does not exist.
# Nothing else: no container is started, stopped, frozen or thawed by this script.
#
# ORDER IS DELIBERATE. podfreeze goes first. It is the daily tool, Phase 4's extraction
# rewrote 698 lines of it, and the library is a NEW file it now cannot start without —
# so if the library deploy is wrong, it fails on the tool whose breakage is loudest
# rather than on the newer one nothing depends on yet.
#
# BOTH PLAYS, NOT ONE. They are separate files on purpose (Task 4.5): each owns a tool
# with its own name, docs anchor and dependencies, and the one artefact they genuinely
# share is included by both. Running only one of them leaves the OTHER tool at its
# pre-extraction build on this host while the repo says otherwise — the deployed-vs-
# checkout drift that scripts/qa-deployed-drift.bash exists to catch.
#
# THEN RUN ./acceptance.bash, in the same session. It is the gate that verifies a real
# container freezes, thaws and comes back, which is the evidence Phase 4 and Phase 5 are
# waiting on and which no suite in this repo can produce.
#
# Usage: ./CLAUDE/Plan/00122-lxc-freeze-thaw-shared-with-podfreeze/deploy.bash
#          [-h|--help] [--check] [-y|--yes]
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
# shellcheck source=../../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: deploy.bash [-h|--help] [--check] [-y|--yes]

Runs both freeze plays on the HOST, podfreeze first:

  playbooks/imports/optional/common/play-podfreeze.yml
  playbooks/imports/optional/common/play-lxcfreeze.yml

Between them they install fzf where absent, write the shared library to
~/.local/lib/freeze/freeze-common.bash (0644), and write ~/.local/bin/podfreeze
and ~/.local/bin/lxcfreeze (0755). Both tools source that library at startup and
neither starts without it, which is why both plays are run rather than one.

--check previews without changing anything. -y/--yes skips the change gate.

Run ./acceptance.bash afterwards — it is the verdict, and it needs the deployed
copies this script puts in place."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible, which installs a package and writes both freeze tools and their shared library into this machine's own home directory"
plan_prime_sudo
plan_start_log auto

# podfreeze first — see ORDER IS DELIBERATE above. A failed leg aborts the run, so a
# broken library deploy never reaches the second play (R7).
plan_deploy_leg "play-podfreeze.yml — podfreeze plus the shared freeze library and fzf" \
    plan_ansible_playbook playbooks/imports/optional/common/play-podfreeze.yml

plan_deploy_leg "play-lxcfreeze.yml — lxcfreeze, including the same shared library" \
    plan_ansible_playbook playbooks/imports/optional/common/play-lxcfreeze.yml

plan_finish
