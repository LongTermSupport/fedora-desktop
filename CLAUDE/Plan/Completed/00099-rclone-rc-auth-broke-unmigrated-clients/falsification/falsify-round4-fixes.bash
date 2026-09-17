#!/usr/bin/env bash
# Round 4, findings 1 and 3.
#
#   1. Check [6]'s stand-in was one component deep; find_mount_path returns N. A fixed-depth
#      normalisation (`dirname`, `${p%/*}`) satisfied it and left ftp-camera --copy broken.
#      Check [6] now invokes `ftp-camera --copy-preflight` — the client's own code — so
#      there is no stand-in left to defeat. Proven by showing the mutant that beat the old
#      input does NOT beat the client, because the client is the thing under test.
#   3. A duplicated CHECK_CATALOGUE entry gave `COVERAGE: 9 of 10` and ACCEPTED. The counts
#      are now compared directly, which no further cause can walk past.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_paths.inc.bash
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/_paths.inc.bash"
LIB_REAL="$RC_LIB_SRC"
STUB_DIR="$(mktemp -d)"
MOUNT_ROOT="${STUB_DIR}/mnt/photos"
CHILD="${MOUNT_ROOT}/PHOTO"
DEEP="${CHILD}/LIBRARY/photos"
mkdir -p "${DEEP}"

bash -c 'sleep 90; :' \
    rclone mount --rc "--rc-addr=localhost:5573" photos: "${MOUNT_ROOT}" &
FAKE_PID=$!
MUTANT_GATE="${FALSIFY_SCRATCH}/acceptance-catalogue-dup.bash"
trap 'if [ -d "/proc/${FAKE_PID}" ]; then kill "${FAKE_PID}"; fi; rm -rf "${STUB_DIR}"; rm -f "${MUTANT_GATE}"' EXIT

cat > "${STUB_DIR}/pgrep" << STUB
#!/usr/bin/env bash
printf '%s\n' "${FAKE_PID}"
STUB
chmod +x "${STUB_DIR}/pgrep"

# ── 1. depth ──────────────────────────────────────────────────────────────────────────
printf '=== finding 1: a fixed-depth normalisation beat the one-level stand-in ===\n'

# A findmnt that answers `dirname` of its target — the mutant the reviewer used. It is a
# plausible "simplification of findmnt --target" and it resolves a depth-1 path correctly
# while failing a depth-3 one.
cat > "${STUB_DIR}/findmnt-dirname" << 'STUB'
#!/usr/bin/env bash
prev=""
for a in "$@"; do
    if [ "$prev" = "--target" ]; then
        dirname "$a"
        exit 0
    fi
    prev="$a"
done
exit 1
STUB
# The honest one: resolve any path to the mount root.
cat > "${STUB_DIR}/findmnt-true" << STUB
#!/usr/bin/env bash
printf '%s\n' "${MOUNT_ROOT}"
STUB
chmod +x "${STUB_DIR}/findmnt-dirname" "${STUB_DIR}/findmnt-true"

resolve() {
    local findmnt_impl="$1" arg="$2" bindir
    bindir="$(mktemp -d)"
    cp "${findmnt_impl}" "${bindir}/findmnt"
    cp "${STUB_DIR}/pgrep" "${bindir}/pgrep"
    local out=1
    if PATH="${bindir}:${PATH}" bash -c '
        set -uo pipefail
        . "$1"
        rclone_rc_addr_for_mount "$2" 2>/dev/null
    ' _ "${LIB_REAL}" "${arg}" > /dev/null; then
        out=0
    fi
    rm -rf "${bindir}"
    return "${out}"
}

verdict() {
    if resolve "$1" "$2"; then printf 'resolves'; else printf 'FAILS   '; fi
}

printf '%-22s %-12s %-12s\n' 'findmnt behaviour' 'depth 1' 'depth 3 (client)'
printf -- '------------------------------------------------------\n'
printf '%-22s ' 'true --target'
verdict "${STUB_DIR}/findmnt-true" "${CHILD}"
printf '    '
verdict "${STUB_DIR}/findmnt-true" "${DEEP}"
printf '\n'
printf '%-22s ' 'dirname (the mutant)'
mut_d1=1; mut_d3=1
if ! resolve "${STUB_DIR}/findmnt-dirname" "${CHILD}"; then mut_d1=0; fi
if [ "${mut_d1}" -eq 1 ]; then printf 'resolves'; else printf 'FAILS   '; fi
printf '    '
if ! resolve "${STUB_DIR}/findmnt-dirname" "${DEEP}"; then mut_d3=0; fi
if [ "${mut_d3}" -eq 1 ]; then printf 'resolves'; else printf 'FAILS   '; fi
printf '\n'

f1_ok=0
if [ "${mut_d1}" -eq 1 ] && [ "${mut_d3}" -eq 0 ]; then
    printf '\n  CONFIRMED: the mutant passes a depth-1 input and fails the client shape, so\n'
    printf '  the previous gate input could not have caught it.\n'
    f1_ok=1
else
    printf '\n  INCONCLUSIVE — the reviewer mutant did not reproduce (d1=%s d3=%s)\n' \
        "${mut_d1}" "${mut_d3}"
fi

# The fix is structural, not a better stand-in: check [6] runs the client. Assert the two
# things that makes true, since no mount exists here to run it against.
printf '\n  check [6] now invokes the client:\n'
client_calls=0
if grep -q -- 'ftp-camera" --copy-preflight' "${GATE}"; then client_calls=1; fi
guard_present=0
if grep -q -- "has no --copy-preflight" "${GATE}"; then guard_present=1; fi
standin_gone=1
if grep -qE "rclone_rc_addr_for_mount \"[\$](client_input|rc_mount)" "${GATE}"; then standin_gone=0; fi
printf '    invokes ftp-camera --copy-preflight : %s\n' "${client_calls}"
printf '    refuses a build without the flag    : %s\n' "${guard_present}"
printf '    no stand-in resolution left in gate : %s\n' "${standin_gone}"

# And the client's preflight really is the code --copy runs.
preflight_shared=0
if grep -q 'copy_preflight > /dev/null || return 1' "${BIN_SRC}/ftp-camera"; then
    preflight_shared=1
fi
printf '    --copy calls the same function      : %s\n' "${preflight_shared}"

if [ "${client_calls}" -eq 1 ] && [ "${guard_present}" -eq 1 ] \
    && [ "${standin_gone}" -eq 1 ] && [ "${preflight_shared}" -eq 1 ]; then
    printf '  FIXED — there is no proxy left for a normalisation mutant to satisfy.\n'
else
    f1_ok=0
    printf '  NOT FIXED — a stand-in or an unshared preflight remains.\n'
fi

# ── 3. the fourth COVERAGE cause ──────────────────────────────────────────────────────
printf '\n=== finding 3: a duplicated CHECK_CATALOGUE entry ===\n'

cat > "${STUB_DIR}/findmnt" << STUB
#!/usr/bin/env bash
for a in "\$@"; do
    if [ "\$a" = "SOURCE" ]; then
        printf '%s\n' "photos:"
        exit 0
    fi
done
printf '%s\n' "${MOUNT_ROOT}"
STUB
chmod +x "${STUB_DIR}/findmnt"
PATH="${STUB_DIR}:${PATH}"
export PATH

python3 - "${GATE}" "${MUTANT_GATE}" << 'PY'
import sys
src = open(sys.argv[1]).read()
line = '    "7|no repo-owned script has drifted from its deployed copy"\n'
assert line in src, "catalogue entry not found — this harness is testing nothing"
open(sys.argv[2], 'w').write(src.replace(line, line + line))
PY
chmod +x "${MUTANT_GATE}"

run_gate() {
    local out
    if ! out=$("$1" 2>&1); then printf '%s' "${out}"; return 0; fi
    printf '%s' "${out}"
}

printf '  CONTROL — unmodified catalogue:\n'
control_out=$(run_gate "${GATE}")
if ! printf '%s\n' "${control_out}" | grep -E '^COVERAGE|DECLARED MORE THAN ONCE|COUNT MISMATCH' | sed 's/^/    /'; then
    printf '    (no COVERAGE line)\n'
fi
control_flagged=0
if printf '%s' "${control_out}" | grep -qE 'DECLARED MORE THAN ONCE|COUNT MISMATCH'; then control_flagged=1; fi

printf '  MUTANT — check 7 declared twice:\n'
mutant_out=$(run_gate "${MUTANT_GATE}")
if ! printf '%s\n' "${mutant_out}" | grep -E '^COVERAGE|DECLARED MORE THAN ONCE|COUNT MISMATCH|^ACCEPTED|^REJECTED' | sed 's/^/    /'; then
    printf '    (no COVERAGE line)\n'
fi
mutant_flagged=0
if printf '%s' "${mutant_out}" | grep -q 'DECLARED MORE THAN ONCE: 7'; then mutant_flagged=1; fi
mutant_accepted=0
if printf '%s' "${mutant_out}" | grep -q '^ACCEPTED'; then mutant_accepted=1; fi

f3_ok=0
if [ "${control_flagged}" -eq 0 ] && [ "${mutant_flagged}" -eq 1 ] && [ "${mutant_accepted}" -eq 0 ]; then
    printf '  MUTANT KILLED — the duplicated declaration is named and the run is not ACCEPTED.\n'
    f3_ok=1
else
    printf '  NOT ESTABLISHED (control=%s mutant=%s accepted=%s)\n' \
        "${control_flagged}" "${mutant_flagged}" "${mutant_accepted}"
fi

printf '\n'
if [ "${f1_ok}" -eq 1 ] && [ "${f3_ok}" -eq 1 ]; then
    printf 'BOTH ROUND-4 FINDINGS ADDRESSED\n'
    exit 0
fi
printf 'NOT FULLY ESTABLISHED (finding1=%s finding3=%s)\n' "${f1_ok}" "${f3_ok}"
exit 1
