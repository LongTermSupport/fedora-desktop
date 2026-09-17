#!/usr/bin/env bash
# Round 3, finding 1: check [6]'s old stand-in `$rc_mount/.` killed only the TOTAL removal
# of the normalisation. Two weaker normalisations — a `${p%/.}` suffix strip and a
# `realpath` canonicalisation — satisfied it while leaving ftp-camera --copy broken exactly
# as B1 left it. `realpath` is what a later author "simplifying" findmnt --target reaches
# for, and the gate would wave it through.
#
# The gate now resolves a REAL subdirectory of the mount. This drives four library variants
# against both inputs and asserts the new one kills every mutant while the old one does not.
set -euo pipefail

LIB_REAL="/workspace/files/home/.local/bin/rclone-rc-auth.bash"
STUB_DIR="$(mktemp -d)"
MOUNT_ROOT="${STUB_DIR}/mnt/photos"
# A real subdirectory, which is what `find -mindepth 1 -maxdepth 1 -type d` hands the gate.
REAL_CHILD="${MOUNT_ROOT}/PHOTO"
mkdir -p "${REAL_CHILD}/LIBRARY"

# findmnt --target resolves ANY path to its mount root — that is the behaviour the shipped
# library depends on and the mutants below replace.
cat > "${STUB_DIR}/findmnt" << STUB
#!/usr/bin/env bash
printf '%s\n' "${MOUNT_ROOT}"
STUB

bash -c 'sleep 60; :' \
    rclone mount --rc "--rc-addr=localhost:5573" photos:PHOTO/LIBRARY "${MOUNT_ROOT}" &
FAKE_PID=$!
trap 'if [ -d "/proc/${FAKE_PID}" ]; then kill "${FAKE_PID}"; fi; rm -rf "${STUB_DIR}"' EXIT

cat > "${STUB_DIR}/pgrep" << STUB
#!/usr/bin/env bash
printf '%s\n' "${FAKE_PID}"
STUB
chmod +x "${STUB_DIR}/findmnt" "${STUB_DIR}/pgrep"
PATH="${STUB_DIR}:${PATH}"
export PATH

# Each mutant replaces the whole normalisation block with a different — and plausible —
# way of "tidying up" the path. Built by cutting the marked block, so each differs from the
# shipped file in exactly the property under test.
build_mutant() {
    local name="$1" replacement="$2"
    python3 - "${LIB_REAL}" "${STUB_DIR}/lib-${name}.bash" "${replacement}" << 'PY'
import sys
src = open(sys.argv[1]).read()
start = src.index('    local findmnt_out=""')
end = src.index('    mountpoint="$mount_root"\n') + len('    mountpoint="$mount_root"\n')
body = sys.argv[3] + '\n' if sys.argv[3] else ''
open(sys.argv[2], 'w').write(src[:start] + body + src[end:])
PY
    printf '%s\n' "${STUB_DIR}/lib-${name}.bash"
}

# Double-quoted with escaped dollars: these strings are DATA (bash source for the mutant),
# and a single-quoted literal containing `$` reads to shellcheck as a failed expansion.
LIB_SUFFIX=$(build_mutant suffix "    mountpoint=\"\${mountpoint%/.}\"")
LIB_REALPATH=$(build_mutant realpath "    mountpoint=\$(realpath -m \"\$mountpoint\")")
LIB_NONE=$(build_mutant none '')

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

# Prints PASS when the input resolves (which, for a mutant, means the gate is BLIND to it).
verdict() {
    if resolve "$1" "$2" > /dev/null; then
        printf 'resolves'
        return 0
    fi
    printf 'FAILS   '
    return 1
}

printf '%-34s %-12s %s\n' 'library variant' 'OLD input' 'NEW input'
printf '%-34s %-12s %s\n' '' '(root + /.)' '(a real subdirectory)'
printf -- '---------------------------------------------------------------------\n'

old_kills=""
new_kills=""
for entry in "shipped:${LIB_REAL}" "mutant %/. strip:${LIB_SUFFIX}" \
    "mutant realpath:${LIB_REALPATH}" "mutant none:${LIB_NONE}"; do
    label="${entry%%:*}"
    lib="${entry#*:}"
    printf '%-34s ' "${label}"
    old_ok=1
    if ! verdict "${lib}" "${MOUNT_ROOT}/."; then old_ok=0; fi
    printf '    '
    new_ok=1
    if ! verdict "${lib}" "${REAL_CHILD}"; then new_ok=0; fi
    printf '\n'
    case "${label}" in
        mutant*)
            if [ "${old_ok}" -eq 0 ]; then old_kills="${old_kills} ${label}"; fi
            if [ "${new_ok}" -eq 0 ]; then new_kills="${new_kills} ${label}"; fi
            ;;
        shipped)
            if [ "${old_ok}" -eq 0 ] || [ "${new_ok}" -eq 0 ]; then
                printf '\nHARNESS BROKEN — the shipped library must resolve BOTH inputs\n'
                exit 1
            fi
            ;;
    esac
done

printf '\nmutants killed by the OLD input:%s\n' "${old_kills:- (none)}"
printf 'mutants killed by the NEW input:%s\n' "${new_kills:- (none)}"

# `grep -o`, not `grep -c`: the kills are accumulated on ONE line, so -c counts that single
# line and reports 1 however many mutants died. Counting occurrences is the question here.
count_kills() {
    if ! printf '%s' "$1" | grep -o 'mutant' | wc -l; then
        printf '0\n'
    fi
}
new_count=$(count_kills "${new_kills}")
old_count=$(count_kills "${old_kills}")

if [ "${new_count}" -eq 3 ] && [ "${old_count}" -eq 1 ]; then
    printf '\nFIXED — the new input kills all three, the old one killed only total removal.\n'
    printf 'The shipped library passes both, so the assertion is not one that fails on everything.\n'
    exit 0
fi
printf '\nNOT ESTABLISHED — new killed %s, old killed %s; expected 3 and 1\n' \
    "${new_count}" "${old_count}"
exit 1
