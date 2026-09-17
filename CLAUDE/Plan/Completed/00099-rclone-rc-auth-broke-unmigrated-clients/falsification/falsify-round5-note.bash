#!/usr/bin/env bash
# Round 5, blocking: check [6] called `note`, which the gate never defined. Under
# `set -euo pipefail` that is exit 127 mid-check — no COVERAGE line, no verdict — and it
# fires on the multi-mount branch, which check [0]'s own comment calls the normal case.
#
# Nothing caught it: shellcheck does not resolve command names, and no harness executed
# check [6] at all — the round-4 harness verified it with five GREPS of the gate's text,
# which is precisely the "a check whose clean result is indistinguishable from a blind
# one" class this plan is named for, applied to my own falsification.
#
# So this one EXECUTES the block, in both branches, with `note` defined and removed.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_paths.inc.bash
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/_paths.inc.bash"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

# Lift check [6]'s block verbatim, with the gate's real helper definitions, and drive it
# against a stub client. Extraction asserted, so a moved anchor fails loudly.
python3 - "${GATE}" "${WORK}/check6.bash" << 'PY'
import sys
src = open(sys.argv[1]).read()

helpers_start = src.index('ok() {')
helpers_end = src.index('# Temp files are removed on the way out')
helpers = src[helpers_start:helpers_end]
assert 'note() {' in helpers, "note is still not defined among the gate's helpers"

start = src.index('check 6 "ftp-camera copy preflight authenticates"')
end = src.index("echo\n\n# --- 6b.", start)
block = src[start:end]
assert '--copy-preflight' in block, "extracted the wrong block"

harness = """#!/usr/bin/env bash
set -euo pipefail
PASS=0
FAIL=0
RAN_CHECKS=()
TEMP_FILES=()
BIN="$1"
RC_ADDR="$2"
RC_LIB="$3"
check() { RAN_CHECKS+=("$1"); echo "[$1] $2"; }
""" + helpers + """
""" + block + """
echo "PASS=$PASS FAIL=$FAIL"
"""
open(sys.argv[2], 'w').write(harness)
PY
chmod +x "${WORK}/check6.bash"

# A stub client that carries the strings the gate greps for and answers the preflight.
mkdir -p "${WORK}/bin"
cat > "${WORK}/bin/ftp-camera" << 'STUB'
#!/usr/bin/env bash
# rclone_rc_available --copy-preflight
printf '%s\n' "${STUB_ADDR:-localhost:5573}"
STUB
chmod +x "${WORK}/bin/ftp-camera"
: > "${WORK}/rc-lib.bash"

# `if cmd; then rc=0; else rc=$?; fi` — NOT `if ! cmd; then rc=$?`. The `!` inverts the
# status before `$?` is read, so the second form records 0 for every failure. It did
# exactly that here and reported the dead mutant as exit 0, which would have vouched for
# the bug this harness exists to prove — a discarded failure signal inside the
# falsification of a discarded failure signal.
run_case() {
    local label="$1" gate_addr="$2" stub_addr="$3" script="$4" out rc=0
    if out=$(STUB_ADDR="${stub_addr}" bash "${script}" "${WORK}/bin" "${gate_addr}" "${WORK}/rc-lib.bash" 2>&1); then
        rc=0
    else
        rc=$?
    fi
    printf '  %-34s exit=%-4s %s\n' "${label}" "${rc}" \
        "$(printf '%s\n' "${out}" | grep -E '^  (PASS|FAIL|NOTE)' | tr '\n' ' ')"
    return "${rc}"
}

printf 'SHIPPED gate (note defined):\n'
agree_ok=0
if run_case "addresses agree" "localhost:5573" "localhost:5573" "${WORK}/check6.bash"; then agree_ok=1; fi
differ_ok=0
if run_case "addresses DIFFER (the branch)" "localhost:5572" "localhost:5573" "${WORK}/check6.bash"; then differ_ok=1; fi

# The mutant: the fix removed. This is the state round 4 shipped.
python3 - "${WORK}/check6.bash" "${WORK}/check6-nonote.bash" << 'PY'
import re, sys
src = open(sys.argv[1]).read()
out = re.sub(r'note\(\) \{\n.*?\n\}\n', '', src, count=1, flags=re.S)
assert out != src, "could not remove note() — the mutant is not a mutant"
open(sys.argv[2], 'w').write(out)
PY

printf '\nMUTANT (note undefined — what round 4 shipped):\n'
mut_agree=0
if run_case "addresses agree" "localhost:5573" "localhost:5573" "${WORK}/check6-nonote.bash"; then mut_agree=1; fi
mut_differ_rc=0
if run_case "addresses DIFFER (the branch)" "localhost:5572" "localhost:5573" "${WORK}/check6-nonote.bash"; then
    mut_differ_rc=0
else
    mut_differ_rc=$?
fi

printf '\n'
if [ "${agree_ok}" -eq 1 ] && [ "${differ_ok}" -eq 1 ] \
    && [ "${mut_agree}" -eq 1 ] && [ "${mut_differ_rc}" -eq 127 ]; then
    printf 'MUTANT KILLED — without note() the differing-address branch dies with exit 127\n'
    printf 'and prints no verdict; with it, both branches complete. The agreeing branch is\n'
    printf 'unaffected either way, which is why nothing noticed.\n'
    exit 0
fi
printf 'NOT ESTABLISHED (agree=%s differ=%s mutant_agree=%s mutant_differ_exit=%s)\n' \
    "${agree_ok}" "${differ_ok}" "${mut_agree}" "${mut_differ_rc}"
exit 1
