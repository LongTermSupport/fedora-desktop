#!/usr/bin/env bash
# m-e: COVERAGE was compared in one direction only. A check that RAN without being declared
# gave "COVERAGE: 10 of 9" and still ACCEPTED. This drives the real gate with a catalogue
# entry deleted, so a check genuinely runs undeclared, and asserts the verdict says so.
#
# Control included: the SAME gate with the catalogue intact must NOT print the new line —
# otherwise the assertion fires on every run and proves nothing.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_paths.inc.bash
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/_paths.inc.bash"
STUB_DIR="$(mktemp -d)"
MOUNT_ROOT="${STUB_DIR}/mnt/photos"
mkdir -p "${MOUNT_ROOT}"

# check [0] aborts without a mount, so nothing downstream — including the verdict block
# under test — would execute. Stub the two system probes it uses.
cat > "${STUB_DIR}/findmnt" << STUB
#!/usr/bin/env bash
for a in "\$@"; do
    if [ "\$a" = "SOURCE" ]; then
        printf '%s\n' "photos:PHOTO"
        exit 0
    fi
done
printf '%s\n' "${MOUNT_ROOT}"
STUB

bash -c 'sleep 60; :' \
    rclone mount --rc "--rc-addr=localhost:5573" photos:PHOTO "${MOUNT_ROOT}" &
FAKE_PID=$!
trap 'if [ -d "/proc/${FAKE_PID}" ]; then kill "${FAKE_PID}"; fi; rm -rf "${STUB_DIR}"' EXIT

cat > "${STUB_DIR}/pgrep" << STUB
#!/usr/bin/env bash
printf '%s\n' "${FAKE_PID}"
STUB
chmod +x "${STUB_DIR}/findmnt" "${STUB_DIR}/pgrep"
PATH="${STUB_DIR}:${PATH}"
export PATH

# The mutant gate: one catalogue entry removed, so check [6b] runs undeclared.
#
# It must live INSIDE the repo: the gate resolves its repo root with `git -C "$PLAN_DIR"
# rev-parse`, so a copy in /tmp dies at line 2 with "not a git repository" and the harness
# would report a clean mutant for a reason that has nothing to do with coverage.
MUTANT="${FALSIFY_SCRATCH}/acceptance-mutant.bash"
python3 - "${GATE}" "${MUTANT}" << 'PY'
import sys
src = open(sys.argv[1]).read()
line = '    "6b|vfs/refresh (rclone-cache-warm --fast\'s endpoint) authenticates"\n'
assert line in src, "catalogue entry not found — the harness is testing nothing"
open(sys.argv[2], 'w').write(src.replace(line, ''))
PY
chmod +x "${MUTANT}"

run_gate() {
    local gate="$1" out
    if out=$("${gate}" 2>&1); then
        printf '%s' "${out}"
        return 0
    fi
    printf '%s' "${out}"
    return 0
}

printf 'CONTROL — catalogue intact:\n'
control_out=$(run_gate "${GATE}")
control_flagged=0
if printf '%s' "${control_out}" | grep -q 'RAN BUT NOT DECLARED'; then control_flagged=1; fi
printf '%s\n' "${control_out}" | grep -E '^COVERAGE|RAN BUT NOT DECLARED' || printf '  (no COVERAGE line)\n'

printf '\nMUTANT — 6b removed from the catalogue:\n'
mutant_out=$(run_gate "${MUTANT}")
mutant_flagged=0
if printf '%s' "${mutant_out}" | grep -q 'RAN BUT NOT DECLARED: 6b'; then mutant_flagged=1; fi
printf '%s\n' "${mutant_out}" | grep -E '^COVERAGE|RAN BUT NOT DECLARED' || printf '  (no COVERAGE line)\n'

printf '\n'
if [ "${control_flagged}" -eq 0 ] && [ "${mutant_flagged}" -eq 1 ]; then
    printf 'MUTANT KILLED — an undeclared check is named, and a declared one is not flagged.\n'
    exit 0
fi
printf 'INCONCLUSIVE (control_flagged=%s mutant_flagged=%s)\n' "${control_flagged}" "${mutant_flagged}"
exit 1
