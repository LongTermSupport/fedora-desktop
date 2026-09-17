#!/usr/bin/env bash
# B1: rclone_rc_addr_for_mount must answer for a path INSIDE the mount, which is
# what ftp-camera's find_mount_path hands it (target + the remote's path offset).
#
# Stubs findmnt and the /proc walk rather than needing a live rclone mount. The
# library's own code is the thing under test; only the two system probes are faked,
# and they are faked to the shapes the real ones produce.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_paths.inc.bash
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/_paths.inc.bash"

STUB_DIR="$(mktemp -d)"
trap 'rm -rf "${STUB_DIR}"' EXIT

MOUNT_ROOT="${STUB_DIR}/mnt/photos"
OFFSET_PATH="${MOUNT_ROOT}/PHOTO/LIBRARY"
mkdir -p "${OFFSET_PATH}"

# findmnt --target <path> resolves any path to the mount containing it; findmnt with
# no --target lists mounts. Both shapes are used by the code under test.
cat > "${STUB_DIR}/findmnt" << STUB
#!/usr/bin/env bash
for a in "\$@"; do
    if [ "\$a" = "--target" ]; then
        printf '%s\n' "${MOUNT_ROOT}"
        exit 0
    fi
done
printf '%s\n' "${MOUNT_ROOT}"
STUB

# A REAL process carrying the unit's argv shape, because the library reads
# /proc/<pid>/cmdline directly — the shell opens that path itself, so no stub of
# `tr` or `cat` can stand in for it. `bash -c` lets the argv be dictated exactly,
# and play-rclone.yml puts the mountpoint LAST.
# The trailing `:` matters: `bash -c 'sleep 30' …` EXECS sleep and replaces itself,
# so /proc/<pid>/cmdline reads `sleep 30` and the argv under test is gone. A second
# statement defeats that optimisation and bash stays, argv intact.
bash -c 'sleep 30; :' \
    rclone mount --rc "--rc-addr=localhost:5573" photos:PHOTO/LIBRARY "${MOUNT_ROOT}" &
FAKE_PID=$!
# Inline, not a named function: a function reached only from a trap reads as
# unreachable (SC2317) and suppressions are banned in this repo.
trap 'if [ -d "/proc/${FAKE_PID}" ]; then kill "${FAKE_PID}"; fi; rm -rf "${STUB_DIR}"' EXIT

cat > "${STUB_DIR}/pgrep" << STUB
#!/usr/bin/env bash
printf '%s\n' "${FAKE_PID}"
STUB

chmod +x "${STUB_DIR}/findmnt" "${STUB_DIR}/pgrep"
PATH="${STUB_DIR}:${PATH}"
export PATH

RC_LIB_PATH="$RC_LIB_SRC"
# shellcheck source=/dev/null
source "${RC_LIB_PATH}"

printf 'A: called with the MOUNT ROOT (what triage.bash passes)\n'
if addr=$(rclone_rc_addr_for_mount "${MOUNT_ROOT}"); then
    printf '   -> %s\n' "${addr}"
    a_ok=1
else
    printf '   -> FAILED\n'
    a_ok=0
fi

printf 'B: called with a path INSIDE the mount (what ftp-camera passes)\n'
if addr=$(rclone_rc_addr_for_mount "${OFFSET_PATH}"); then
    printf '   -> %s\n' "${addr}"
    b_ok=1
else
    printf '   -> FAILED\n'
    b_ok=0
fi

printf '\nC control: a path in NO mount must still fail\n'
cat > "${STUB_DIR}/findmnt" << 'STUB'
#!/usr/bin/env bash
exit 1
STUB
chmod +x "${STUB_DIR}/findmnt"
c_err="${STUB_DIR}/c.err"
if rclone_rc_addr_for_mount "/nowhere/at/all" > "${c_err}" 2>&1; then
    printf '   -> WRONGLY SUCCEEDED\n'
    c_ok=0
else
    printf '   -> correctly failed: %s\n' "$(cat "${c_err}")"
    c_ok=1
fi

printf '\nexpected: A ok, B ok, C fails\n'
if [ "${a_ok}" -eq 1 ] && [ "${b_ok}" -eq 1 ] && [ "${c_ok}" -eq 1 ]; then
    printf 'B1 FIXED — both inputs resolve, and a non-mount still refuses\n'
    exit 0
fi
printf 'B1 NOT FIXED (A=%s B=%s C=%s)\n' "${a_ok}" "${b_ok}" "${c_ok}"
exit 1
