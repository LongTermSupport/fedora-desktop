#!/usr/bin/env bash
# Round 3, findings 3 and 4.
#
#   4. `findmnt --target` can emit MORE THAN ONE row on an over-mounted target — a
#      restarted rclone unit stacked on its own mountpoint. The library now takes the first
#      row by parameter expansion (not `| head -n1`, which under a caller running `set -e`
#      without `pipefail` would turn a findmnt failure into exit 0 and an EMPTY mountpoint).
#   3. A check id emitted TWICE gave `COVERAGE: 10 of 9`, missing=0, undeclared=0, and
#      still ACCEPTED — the same self-contradicting line m-e claimed to have fixed, from
#      the one cause the two list comparisons cannot see between them.
#
# Each section drives the real code and includes the control: the fix must also leave a
# correct run alone.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_paths.inc.bash
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/_paths.inc.bash"
LIB_REAL="$RC_LIB_SRC"
STUB_DIR="$(mktemp -d)"
MOUNT_ROOT="${STUB_DIR}/mnt/photos"
mkdir -p "${MOUNT_ROOT}/PHOTO/LIBRARY"

bash -c 'sleep 90; :' \
    rclone mount --rc "--rc-addr=localhost:5573" photos:PHOTO "${MOUNT_ROOT}" &
FAKE_PID=$!
MUTANT_GATE="${FALSIFY_SCRATCH}/acceptance-dup-mutant.bash"
trap 'if [ -d "/proc/${FAKE_PID}" ]; then kill "${FAKE_PID}"; fi; rm -rf "${STUB_DIR}"; rm -f "${MUTANT_GATE}"' EXIT

cat > "${STUB_DIR}/pgrep" << STUB
#!/usr/bin/env bash
printf '%s\n' "${FAKE_PID}"
STUB
chmod +x "${STUB_DIR}/pgrep"

# ── 4. the over-mounted target ────────────────────────────────────────────────────────
printf '=== finding 4: findmnt emits two rows on an over-mounted target ===\n'

# Two identical rows, which is what a real stacked mount produces. `SOURCE` must still
# answer singly, because the gate asks for it separately.
cat > "${STUB_DIR}/findmnt" << STUB
#!/usr/bin/env bash
for a in "\$@"; do
    if [ "\$a" = "SOURCE" ]; then
        printf '%s\n' "photos:PHOTO"
        exit 0
    fi
done
printf '%s\n%s\n' "${MOUNT_ROOT}" "${MOUNT_ROOT}"
STUB
chmod +x "${STUB_DIR}/findmnt"
PATH="${STUB_DIR}:${PATH}"
export PATH

# The mutant keeps the whole multi-line output, which is the shape before the fix.
python3 - "${LIB_REAL}" "${STUB_DIR}/lib-allrows.bash" << 'PY'
import sys
src = open(sys.argv[1]).read()
old = '    local mount_root="${findmnt_out%%$\'\\n\'*}"\n'
assert old in src, "first-row expansion not found — this harness is testing nothing"
open(sys.argv[2], 'w').write(src.replace(old, '    local mount_root="$findmnt_out"\n'))
PY

resolve() {
    bash -c '
        set -uo pipefail
        . "$1"
        if addr=$(rclone_rc_addr_for_mount "$2" 2>/dev/null); then
            printf "%s\n" "$addr"
            exit 0
        fi
        exit 1
    ' _ "$1" "$2"
}

shipped_ok=0
if addr=$(resolve "${LIB_REAL}" "${MOUNT_ROOT}/PHOTO"); then
    printf '  shipped (first row only) -> %s\n' "${addr}"
    shipped_ok=1
else
    printf '  shipped (first row only) -> FAILED\n'
fi

mutant_ok=0
if addr=$(resolve "${STUB_DIR}/lib-allrows.bash" "${MOUNT_ROOT}/PHOTO"); then
    printf '  mutant (keeps both rows) -> %s\n' "${addr}"
    mutant_ok=1
else
    printf '  mutant (keeps both rows) -> FAILED (this is the pre-fix behaviour)\n'
fi

f4_ok=0
if [ "${shipped_ok}" -eq 1 ] && [ "${mutant_ok}" -eq 0 ]; then
    printf '  MUTANT KILLED — the two-row case resolves now and did not before\n'
    f4_ok=1
else
    printf '  NOT ESTABLISHED (shipped=%s mutant=%s)\n' "${shipped_ok}" "${mutant_ok}"
fi

# ── 3. the duplicate check id ─────────────────────────────────────────────────────────
printf '\n=== finding 3: the same check id emitted twice ===\n'

# Single-row findmnt again, so check [0] behaves normally and the verdict block is reached.
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
chmod +x "${STUB_DIR}/findmnt"

# Emit check 4 a second time — the shape a copy-paste of a whole section produces.
python3 - "${GATE}" "${MUTANT_GATE}" << 'PY'
import sys
src = open(sys.argv[1]).read()
anchor = 'check 5 "rclone-tail --once reports live figures"\n'
assert anchor in src, "check 5 announcement not found — this harness is testing nothing"
open(sys.argv[2], 'w').write(
    src.replace(anchor, 'check 4 "rclone-cache-status reports live figures"\n' + anchor))
PY
chmod +x "${MUTANT_GATE}"

run_gate() {
    local gate="$1" out
    if ! out=$("${gate}" 2>&1); then
        printf '%s' "${out}"
        return 0
    fi
    printf '%s' "${out}"
    return 0
}

printf '  CONTROL — unmodified gate:\n'
control_out=$(run_gate "${GATE}")
if ! printf '%s\n' "${control_out}" | grep -E '^COVERAGE|RAN MORE THAN ONCE' | sed 's/^/    /'; then
    printf '    (no COVERAGE line — the verdict block was never reached)\n'
fi
control_flagged=0
if printf '%s' "${control_out}" | grep -q 'RAN MORE THAN ONCE'; then control_flagged=1; fi

printf '  MUTANT — check 4 emitted twice:\n'
mutant_out=$(run_gate "${MUTANT_GATE}")
if ! printf '%s\n' "${mutant_out}" | grep -E '^COVERAGE|RAN MORE THAN ONCE|^REJECTED|^ACCEPTED' | sed 's/^/    /'; then
    printf '    (no COVERAGE line — the verdict block was never reached)\n'
fi
mutant_flagged=0
if printf '%s' "${mutant_out}" | grep -q 'RAN MORE THAN ONCE: 4'; then mutant_flagged=1; fi
mutant_rejected=0
if printf '%s' "${mutant_out}" | grep -q '^REJECTED'; then mutant_rejected=1; fi

f3_ok=0
if [ "${control_flagged}" -eq 0 ] && [ "${mutant_flagged}" -eq 1 ] && [ "${mutant_rejected}" -eq 1 ]; then
    printf '  MUTANT KILLED — a repeated id is named AND rejected; a clean run is not flagged\n'
    f3_ok=1
else
    printf '  NOT ESTABLISHED (control_flagged=%s mutant_flagged=%s rejected=%s)\n' \
        "${control_flagged}" "${mutant_flagged}" "${mutant_rejected}"
fi

printf '\n'
if [ "${f4_ok}" -eq 1 ] && [ "${f3_ok}" -eq 1 ]; then
    printf 'BOTH ROUND-3 FIXES ESTABLISHED\n'
    exit 0
fi
printf 'AT LEAST ONE FIX NOT ESTABLISHED (finding4=%s finding3=%s)\n' "${f4_ok}" "${f3_ok}"
exit 1
