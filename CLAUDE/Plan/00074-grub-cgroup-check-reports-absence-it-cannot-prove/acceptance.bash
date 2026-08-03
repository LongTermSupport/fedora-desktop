#!/usr/bin/env bash
#
# Plan 00074 — acceptance tests for the legacy-grub cgroup check.
#
# PURPOSE
#   Drive `check_legacy_grub_cgroup` through ALL FOUR states a real box can be in, and
#   assert each produces its own distinct outcome:
#
#     clean   grubby ran, no legacy args        -> exit 0, "No legacy cgroup"  (unchanged)
#     legacy  grubby ran, args removed OK       -> exit 0, "removed"           (unchanged)
#     stuck   grubby ran, removal FAILED        -> ABORT non-zero              (defect 2)
#     broken  grubby could not run at all       -> ABORT non-zero              (defect 1)
#
#   The two "unchanged" rows matter as much as the two fixes: the refactor must be inert on
#   every path that works today.
#
# WHERE TO RUN
#   Anywhere, including a CCY container. It reaches no host, runs no Ansible, touches no
#   bootloader and needs no real `grubby` — the stub IS the mechanism, not a compromise.
#
# WHY A STUB IS NOT A WEAKER TEST HERE
#   Unlike Plan 00073 (where a container genuinely cannot prove a password obtains
#   privilege), this state space is exhaustive and fully drivable. A real box exhibits
#   exactly ONE of the four states, so a host run would exercise strictly LESS than this.
#   What a host adds is confidence that real `grubby` output lands in the state expected —
#   a question about the fixture's realism, not about the logic. That is Task 3.3.
#
# IDEMPOTENCE
#   Read-only and re-runnable. Everything happens in a mktemp dir removed on exit.
#
# USAGE
#   ./CLAUDE/Plan/00074-.../acceptance.bash [-h|--help]
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
# shellcheck source-path=SCRIPTDIR/..
# shellcheck source-path=SCRIPTDIR/../..
# shellcheck source=_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="Usage: acceptance.bash [-h|--help] [-y|--yes] [--check]

Plan 00074 acceptance: the legacy-grub cgroup check, driven through all four states via a
stub grubby. Read-only; reaches no host; needs no real grubby; safe in a container."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
  printf '[FATAL] unexpected argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
  printf '%s\n' "${PLAN_USAGE}" >&2
  exit 64
fi
plan_start_log auto

RUN_BASH="${repoRoot}/run.bash"
readonly RUN_BASH
[[ -f "${RUN_BASH}" ]] || { printf '[FATAL] run.bash not found at %s\n' "${RUN_BASH}" >&2; exit 1; }

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-cases.inc.bash
source "${scriptDir}/_acceptance-cases.inc.bash"

GRUB_STUB_DIR="$(mktemp -d)"
trap 'rm -rf "${GRUB_STUB_DIR}"' EXIT
make_grub_fixtures "${GRUB_STUB_DIR}"

# ── The two paths that already work — these must NOT change ─────────────────────────

plan_gather_leg "clean box: reports no legacy config, exits 0" \
  expect_grub clean zero "No legacy cgroup configuration found" ""

plan_gather_leg "legacy args present and removable: reports success, exits 0" \
  expect_grub legacy zero "removed successfully" "Failed to remove"

# ── Defect 2: a removal that verifiably failed must ABORT, not print and continue ────
#
# `error()` is `echo -e` and nothing else — it does not exit. So today this state prints
# "Failed to remove cgroup configuration", prints manual instructions, and the installer
# runs on to completion and exits 0. The forbidden string is the success message, because
# the failure mode to rule out is claiming both at once.
plan_gather_leg "removal failed: aborts instead of continuing" \
  expect_grub stuck nonzero "still present after removal" "removed successfully"

# ── Defect 1: a grubby that could not run must not report an absence ─────────────────
#
# Today `2>/dev/null` discards the reason, stdout is empty, the grep matches nothing, and
# the run prints "No legacy cgroup configuration found" — a check that could not look,
# reporting exactly what a check that looked and found nothing reports. Asserting the new
# message alone would be satisfied by code that printed BOTH, so the old claim is
# explicitly forbidden, and grubby's own words must appear rather than a generic message.
plan_gather_leg "grubby cannot run: aborts, and does not claim absence" \
  expect_grub broken nonzero "unable to open /boot/loader/entries" "No legacy cgroup configuration found"

printf '\nNOTE: these four states are exhaustive and the stub drives all of them, so the\n'
printf 'LOGIC is fully proven here. What a real host adds is confidence that real grubby\n'
printf 'output falls into the state the fixture models (Task 3.3) — a question about the\n'
printf 'fixture, not about the code.\n'

plan_finish
