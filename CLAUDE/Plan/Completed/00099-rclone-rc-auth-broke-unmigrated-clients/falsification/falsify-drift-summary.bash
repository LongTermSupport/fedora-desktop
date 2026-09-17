#!/usr/bin/env bash
# m-b: the drift gate's FAILING summary omitted NOT_DEPLOYED, which its passing summary
# states always and on purpose. Drives the real failing path and asserts all three
# categories appear — plus a control on the passing path, so the assertion is not one that
# passes on any output containing the words.
#
# Runs the gate against a fake HOME populated to force one drifted file and several
# not-deployed ones. Nothing in the real container's home is touched.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_paths.inc.bash
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/_paths.inc.bash"

# This harness targets the shared drift gate rather than the plan's own acceptance gate, so
# it overrides GATE from the include deliberately.
GATE="${REPO_ROOT}/scripts/qa-deployed-drift.bash"
WORK="${FALSIFY_SCRATCH}/drift-harness"
rm -rf "${WORK}"
mkdir -p "${WORK}/home/.local/bin"

# The gate skips entirely inside a CCY container. Cut that branch so the comparison logic
# under test actually runs; everything else is the shipped file.
#
# The copy must sit in scripts/: the gate derives REPO_ROOT from its OWN location, so a
# copy under untracked/scratch/ resolves the repo root to untracked/ and dies looking for
# files/ there. Removed again on exit so it never lingers in a tracked directory.
MUTANT="${REPO_ROOT}/scripts/qa-deployed-drift-harness.bash"
trap 'rm -f "${MUTANT}"' EXIT
python3 - "${GATE}" "${MUTANT}" << 'PY'
import sys
src = open(sys.argv[1]).read()
block = '''if [ "$REPO_ROOT" = "/workspace" ]; then
    echo "⚠ deployed-drift: skipped (CCY container — no deployed copies to compare);" \\
        "${#TEMPLATES[@]} template(s) verified to map to a playbook dest:"
    exit 0
fi
'''
assert block in src, "container skip block not found — the harness would be testing the skip, not the comparison"
open(sys.argv[2], 'w').write(src.replace(block, ''))
PY
chmod +x "${MUTANT}"

# The gate exits non-zero when it finds drift, which is the case under test — so the status
# is captured and reported rather than allowed to kill the harness.
run_with_home() {
    local home="$1" out status=0
    if ! out=$(HOME="${home}" "${MUTANT}" 2>&1); then
        status=$?
    fi
    printf '%s' "${out}"
    return "${status}"
}

show_summary() {
    if ! printf '%s\n' "$1" | grep -E '^[✓✗]'; then
        printf '  (no summary line)\n'
    fi
}

printf 'CONTROL — nothing deployed, so nothing drifts (passing path):\n'
control_out=""
if ! control_out=$(run_with_home "${WORK}/home"); then
    printf '  (gate exited non-zero)\n'
fi
show_summary "${control_out}"

# Now deploy ONE repo-owned script with its bytes changed, so exactly one file drifts.
first_src=""
for candidate in "${BIN_SRC}"/*; do
    if [ -f "${candidate}" ]; then
        first_src="${candidate}"
        break
    fi
done
if [ -z "${first_src}" ]; then
    printf 'HARNESS BROKEN — no repo-owned script to copy\n'
    exit 1
fi
cp "${first_src}" "${WORK}/home/.local/bin/$(basename "${first_src}")"
printf '\n# drift introduced by the harness\n' >> "${WORK}/home/.local/bin/$(basename "${first_src}")"

printf '\nFAILING PATH — one deployed file drifted:\n'
fail_out=""
if ! fail_out=$(run_with_home "${WORK}/home"); then
    printf '  (gate exited non-zero, as it should)\n'
fi
show_summary "${fail_out}"

summary_line=""
if ! summary_line=$(printf '%s\n' "${fail_out}" | grep -E '^✗ deployed-drift'); then
    printf '\nINCONCLUSIVE — the failing path was never reached\n'
    exit 1
fi

missing=""
case "${summary_line}" in *"differ from the repo"*) ;; *) missing="${missing} drifted-count" ;; esac
case "${summary_line}" in *"not installed on this host"*) ;; *) missing="${missing} not-deployed" ;; esac
case "${summary_line}" in *"template(s) not byte-comparable"*) ;; *) missing="${missing} templates" ;; esac

printf '\n'
if [ -z "${missing}" ]; then
    printf 'FIXED — the failing summary names all three categories, as the passing one does.\n'
    exit 0
fi
printf 'STILL INCOMPLETE — the failing summary omits:%s\n' "${missing}"
exit 1
