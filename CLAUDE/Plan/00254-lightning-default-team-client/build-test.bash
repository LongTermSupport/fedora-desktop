#!/usr/bin/env bash
# build-test.bash — Plan 00254 Task 2.2: build Lightning's thread-panel QML test with and
# without the thread-root patch, and prove the new case red on the old QML, green on the new.
#
# WHAT IT DOES, in order (fail-fast: each step needs the one before):
#   1. clones Lightning (public, GitHub mirror) at the pinned upstream commit the patch was
#      written on, into this run's directory under untracked/plan-runs/;
#   2. builds a podman image from Containerfile.build-test (Fedora 44 with the Qt 6 devel RPMs;
#      no Rust, no WebRTC);
#   3. in that image, applies only the patch's test, builds the test and runs it: it must FAIL;
#   4. applies the QML fix, rebuilds, runs it again (it must pass), then builds every target
#      and runs every test whose name contains "thread" (none may fail).
# build-test-report.txt in the run directory has the exact totals the upstream drafts quote.
#
# WHERE TO RUN: on the HOST (plan_require_host, R2): the container has no podman.
#
# EFFECT ON THE HOST: a podman image `localhost/fedora-desktop-lightning-build:plan00254`
# (about 2 GB) and the clone and build tree in the run directory (a few GB, removed with the
# run directory). Nothing is installed on the host and nothing is published. No sudo.
#
# Usage: ./CLAUDE/Plan/00254-lightning-default-team-client/build-test.bash [-h|--help]
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

PLAN_USAGE="usage: build-test.bash [-h|--help]

Builds Lightning's thread-panel QML test in a podman container, before and after the
thread-root patch (Plan 00254 Task 2.2). Run on the HOST. Writes build-test-report.txt
under untracked/plan-runs/."
plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
  printf 'build-test.bash: unknown argument %s (see --help)\n' "${PLAN_REMAINING_ARGS[0]}" >&2
  exit 2
fi

plan_require_host "it builds and runs a podman image"
plan_start_log auto

readonly UPSTREAM_URL="https://github.com/Mizerd/lightning.git"
readonly UPSTREAM_COMMIT="e6ea127a4975ec87f76d21723fecd9fb9b2f27d5"
readonly IMAGE="localhost/fedora-desktop-lightning-build:plan00254"
readonly SRC="${PLAN_RUN_DIR}/src"

# One phase of build-test-inner.bash in the image, on the same source tree; the phase follows.
readonly RUN_PHASE=(podman run --rm --network=none
  -v "${SRC}:/src:Z" -v "${PLAN_SCRIPT_DIR}:/plan:ro,z" -v "${PLAN_RUN_DIR}:/out:z"
  "${IMAGE}" bash /plan/build-test-inner.bash)

plan_deploy_leg "clone Lightning at ${UPSTREAM_COMMIT:0:8}" \
  git clone --quiet --filter=blob:none "${UPSTREAM_URL}" "${SRC}"
plan_deploy_leg "check out ${UPSTREAM_COMMIT:0:8}" \
  git -C "${SRC}" -c advice.detachedHead=false checkout --quiet "${UPSTREAM_COMMIT}"
plan_deploy_leg "build the toolchain image" \
  podman build --pull=newer -t "${IMAGE}" -f "${PLAN_SCRIPT_DIR}/Containerfile.build-test" "${PLAN_SCRIPT_DIR}"
plan_deploy_leg "red: the new case fails on the unpatched QML" "${RUN_PHASE[@]}" red
plan_deploy_leg "green: the new case and every thread test pass on the patched QML" "${RUN_PHASE[@]}" green
plan_finish
