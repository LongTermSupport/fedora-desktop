#!/usr/bin/env bash
# Does check [6]'s new input shape actually exercise the client's resolution?
#
# The gate now resolves from "$rc_mount/." instead of reusing check [0]'s RC_ADDR. That is
# only worth anything if a library WITHOUT the B1 normalisation fails on it — otherwise the
# new call is a second spelling of the old one and the gate is still blind to B1.
#
# Runs the real library against a real process carrying the mount unit's argv, twice: once
# as shipped, once with the normalisation block cut out (the B1 mutant).
set -euo pipefail

LIB_REAL="/workspace/files/home/.local/bin/rclone-rc-auth.bash"
STUB_DIR="$(mktemp -d)"
MOUNT_ROOT="${STUB_DIR}/mnt/photos"
mkdir -p "${MOUNT_ROOT}"

cat > "${STUB_DIR}/findmnt" << STUB
#!/usr/bin/env bash
printf '%s\n' "${MOUNT_ROOT}"
STUB

bash -c 'sleep 45; :' \
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

# The mutant: the same library with the mount-root normalisation removed. Built by cutting
# the marked block rather than by hand, so it differs from the shipped file in exactly the
# one property under test.
LIB_MUTANT="${STUB_DIR}/rclone-rc-auth-mutant.bash"
python3 - "${LIB_REAL}" "${LIB_MUTANT}" << 'PY'
import sys
src = open(sys.argv[1]).read()
start = src.index('    local mount_root=""')
end = src.index('    mountpoint="$mount_root"\n') + len('    mountpoint="$mount_root"\n')
open(sys.argv[2], 'w').write(src[:start] + src[end:])
PY

# Each library is exercised in its own subshell: sourcing two definitions of the same
# function into one shell would test whichever loaded last, twice.
resolve() {
    local lib="$1" arg="$2"
    bash -c '
        set -uo pipefail
        . "$1"
        if addr=$(rclone_rc_addr_for_mount "$2" 2>/dev/null); then
            printf "%s\n" "$addr"
            exit 0
        fi
        exit 1
    ' _ "$lib" "$arg"
}

report() {
    local label="$1" lib="$2" arg="$3"
    local addr
    if addr=$(resolve "$lib" "$arg"); then
        printf '  %-34s -> %s\n' "$label" "$addr"
        return 0
    fi
    printf '  %-34s -> FAILED\n' "$label"
    return 1
}

printf 'SHIPPED library:\n'
root_ok=0
if report "mount root (check [0] input)" "${LIB_REAL}" "${MOUNT_ROOT}"; then root_ok=1; fi
client_ok=0
if report 'inside the mount (rc_mount + "/.")' "${LIB_REAL}" "${MOUNT_ROOT}/."; then client_ok=1; fi

printf '\nB1 MUTANT (normalisation removed):\n'
mut_root_ok=0
if report "mount root (check [0] input)" "${LIB_MUTANT}" "${MOUNT_ROOT}"; then mut_root_ok=1; fi
mut_client_ok=0
if report 'inside the mount (rc_mount + "/.")' "${LIB_MUTANT}" "${MOUNT_ROOT}/."; then mut_client_ok=1; fi

printf '\n'
if [ "${root_ok}" -eq 1 ] && [ "${client_ok}" -eq 1 ] \
    && [ "${mut_root_ok}" -eq 1 ] && [ "${mut_client_ok}" -eq 0 ]; then
    printf 'MUTANT KILLED — check [6]'"'"'s input fails on the B1 mutant while check [0]'"'"'s still passes,\n'
    printf 'which is exactly the blindness the reviewer found: the old gate only ever used the\n'
    printf 'mount root, so it could not see the client break.\n'
    exit 0
fi
printf 'INCONCLUSIVE (shipped root=%s client=%s / mutant root=%s client=%s)\n' \
    "${root_ok}" "${client_ok}" "${mut_root_ok}" "${mut_client_ok}"
exit 1
