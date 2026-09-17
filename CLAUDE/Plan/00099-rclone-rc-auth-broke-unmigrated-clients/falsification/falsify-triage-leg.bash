#!/usr/bin/env bash
# CONTROL for S-b/S-c: with an RC address discoverable, triage's one leg must PASS and the
# run must exit 0. Without this, "exit 1" proves nothing — a script that always fails its
# leg reports an incomplete gather on a perfectly complete one.
#
# Stubs only the two system probes (findmnt, pgrep), to the shapes the real ones produce,
# and runs the host-guard-free copy of the real script.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_paths.inc.bash
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/_paths.inc.bash"

STUB_DIR="$(mktemp -d)"
MOUNT_ROOT="${STUB_DIR}/mnt/photos"
mkdir -p "${MOUNT_ROOT}"

# The no-guard copy is DERIVED from the current triage.bash on every run, with only the
# container refusal removed, and the removal is asserted.
#
# It used to be a hand-made file left in untracked/scratch/. By the time this harness was
# next run that copy was four review rounds out of date, so the control it produced was a
# statement about a script that no longer existed — green, and carrying no information.
# That is the same failure this plan is named for, sitting in the evidence for it.
#
# The copy sits in the PLAN DIRECTORY, not in the temp tree. triage.bash resolves the repo
# root by walking up from its OWN location looking for ansible.cfg (R1), so a copy under
# /tmp dies with "no ansible.cfg above /tmp/..." before reaching a single probe — and the
# harness would then be reporting on a bootstrap failure while claiming to report on the
# gather leg. Same reason falsify-drift-summary.bash keeps its mutant in scripts/.
#
# Removed on exit, including on Ctrl-C, so a deliberately guard-free copy never lingers
# beside the real script where a reader could mistake it for one.
NOGUARD="${PLAN_DIR}/triage-noguard-harness.bash"
# Armed BEFORE the file is written, so a failure inside the generator cannot leave it
# behind. The kill and the temp tree join it once the stub process exists.
trap 'rm -f "${NOGUARD}"' EXIT
python3 - "${TRIAGE}" "${NOGUARD}" << 'PY'
import re, sys
src = open(sys.argv[1]).read()
out = re.sub(r'^plan_require_host .*\n', '', src, count=1, flags=re.M)
assert out != src, "no plan_require_host line to remove — has triage.bash changed shape?"
assert 'plan_require_host' not in out, "more than one host guard; the copy is not guard-free"
open(sys.argv[2], 'w').write(out)
PY
chmod +x "${NOGUARD}"

# Two call shapes are used: `-t fuse.rclone` (list rclone mounts) and `--target <path>`
# (resolve a path to its mount root). Both must answer, or the discovery under test is not
# the thing being exercised.
cat > "${STUB_DIR}/findmnt" << STUB
#!/usr/bin/env bash
printf '%s\n' "${MOUNT_ROOT}"
STUB

# A REAL process carrying the mount unit's argv, because the library reads
# /proc/<pid>/cmdline directly. The trailing \`:\` defeats bash's exec optimisation, which
# would otherwise replace the shell with sleep and lose the argv under test.
bash -c 'sleep 45; :' \
    rclone mount --rc "--rc-addr=localhost:5573" photos:PHOTO/LIBRARY "${MOUNT_ROOT}" &
FAKE_PID=$!
trap 'if [ -d "/proc/${FAKE_PID}" ]; then kill "${FAKE_PID}"; fi; rm -rf "${STUB_DIR}"; rm -f "${NOGUARD}"' EXIT

cat > "${STUB_DIR}/pgrep" << STUB
#!/usr/bin/env bash
printf '%s\n' "${FAKE_PID}"
STUB

chmod +x "${STUB_DIR}/findmnt" "${STUB_DIR}/pgrep"
PATH="${STUB_DIR}:${PATH}"
export PATH

if out=$("${NOGUARD}" 2>&1); then rc=0; else rc=$?; fi

printf 'EXIT=%s\n' "${rc}"
# grep -c style: no match is a RESULT here, not an error, so the status is consumed rather
# than discarded.
if ! printf '%s\n' "${out}" | grep -E 'RC address:|FAILED legs|all legs OK'; then
    # The run's own output, not just "no markers". A harness that reports the absence of
    # what it looked for, and withholds what was actually there, sends the reader back to
    # re-run it by hand — which is how the stale copy this used to exercise went unnoticed.
    printf '(none of the marker lines appeared — the run said this instead:)\n'
    printf '%s\n' "${out}" | awk '{ print "    " $0 }'
fi

if [ "${rc}" -eq 0 ] && printf '%s\n' "${out}" | grep -q 'RC address: localhost:5573'; then
    printf '\nCONTROL PASSES — a discoverable address gives a passing leg and exit 0\n'
    exit 0
fi
printf '\nCONTROL FAILED — the leg cannot distinguish a complete gather from an incomplete one\n'
exit 1
