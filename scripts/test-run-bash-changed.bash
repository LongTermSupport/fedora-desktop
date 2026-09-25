#!/usr/bin/env bash
# Drive `run.bash --changed` end to end (Plan 00141): list the plays whose inputs changed
# since they ran here, ask once, run them in order through the single-play runner, and
# stop at the first failure.
#
# The REAL run.bash and the REAL lock helper run in a throwaway checkout. Which plays
# changed is decided by helpers/play_ledger/changed_plays.py, tested on its own
# (tests/helpers/play_ledger/test_changed_plays.py); here a stand-in prints the marker
# lines a case writes. ansible-playbook, sudo (NOPASSWD) and whoami are stubs, and the
# answer to the confirmation arrives on stdin.
#
# `set -e` is deliberately NOT used: every case must run so the summary shows the whole
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %q\n        got:  %q\n' "$label" "$want" "$got" >&2
    fi
}
yes_if() { if "$@"; then echo yes; else echo no; fi; }

for tool in flock python3; do
    if ! command -v "$tool" >/dev/null; then
        echo "FAIL: $tool is required by the code under test and is not installed" >&2
        exit 1
    fi
done

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/changed-plays-test.XXXXXX")"
cleanup() { rm -rf "$SCRATCH"; }
trap cleanup EXIT

# ── a throwaway checkout: the real run.bash and lock helper, a stand-in for the judge ──
CHECKOUT="$SCRATCH/checkout"
mkdir -p "$CHECKOUT/helpers/play_lock" "$CHECKOUT/helpers/play_ledger" "$CHECKOUT/playbooks/imports"
cp "$REPO_ROOT/run.bash" "$CHECKOUT/run.bash"
cp "$REPO_ROOT/helpers/play_lock/lock.py" "$CHECKOUT/helpers/play_lock/lock.py"
: >"$CHECKOUT/helpers/__init__.py"
: >"$CHECKOUT/helpers/play_ledger/__init__.py"
cat >"$CHECKOUT/helpers/play_ledger/changed_plays.py" <<'EOF'
import os, sys
with open(os.environ["TEST_LOG"], "a") as log:
    log.write("judge: " + " ".join(sys.argv[1:]) + "\n")
with open(os.environ["TEST_MARKERS"]) as markers:
    sys.stdout.write(markers.read())
if os.environ.get("TEST_JUDGE_ERR"):
    sys.stderr.write(os.environ["TEST_JUDGE_ERR"] + "\n")
sys.exit(int(os.environ.get("TEST_JUDGE_RC", "0")))
EOF
for name in play-a play-b play-c; do
    printf -- '- hosts: localhost\n  tasks: []\n' >"$CHECKOUT/playbooks/imports/$name.yml"
done
A="playbooks/imports/play-a.yml"
B="playbooks/imports/play-b.yml"
C="playbooks/imports/play-c.yml"

RUNTIME="$SCRATCH/runtime"
mkdir -m 700 "$RUNTIME"
LOCK="$RUNTIME/fedora-desktop-plays.lock"
LOG="$SCRATCH/calls.log"
MARKERS="$SCRATCH/markers"
FAKE_HOME="$SCRATCH/home"
mkdir -p "$FAKE_HOME/.local/bin"

BIN="$SCRATCH/bin"
mkdir -p "$BIN"
# ansible-playbook records each play it is given and whether the play lock was held;
# FAKE_FAIL_PLAY names a play that fails with exit 4.
cat >"$BIN/ansible-playbook" <<'EOF'
#!/usr/bin/env bash
play="$1"
if flock -n "$TEST_LOCK" true; then lock=free; else lock=held; fi
printf 'play: %s lock=%s\n' "${play##*/}" "$lock" >>"$TEST_LOG"
if [ -n "${FAKE_FAIL_PLAY:-}" ] && [ "${play##*/}" = "$FAKE_FAIL_PLAY" ]; then exit 4; fi
exit 0
EOF
cat >"$BIN/sudo" <<'EOF'
#!/usr/bin/env bash
[ "$*" = "-k -n true" ] && exit 0
echo "fake sudo: unexpected call: $*" >&2
exit 97
EOF
printf '#!/usr/bin/env bash\necho tester\n' >"$BIN/whoami"
chmod 755 "$BIN/ansible-playbook" "$BIN/sudo" "$BIN/whoami"

# run_changed <stdin text> [env assignments...] -- <run.bash args...>
run_changed() {
    local input="$1"
    shift
    local -a assigns=()
    while [ "$1" != "--" ]; do
        assigns+=("$1")
        shift
    done
    shift
    : >"$LOG"
    out="$(printf '%b' "$input" | env -u RUN_BASH_HEADLESS -u FEDORA_DESKTOP_PLAY_LOCK_FD \
        HOME="$FAKE_HOME" PATH="$BIN:/usr/bin:/bin" XDG_RUNTIME_DIR="$RUNTIME" \
        TEST_LOG="$LOG" TEST_LOCK="$LOCK" TEST_MARKERS="$MARKERS" "${assigns[@]}" \
        bash "$CHECKOUT/run.bash" "$@" 2>&1)"
    rc=$?
}
calls() { if [ -f "$LOG" ]; then cat "$LOG"; fi; }
plays_run() { calls | awk '/^play: / { printf "%s ", $2 }'; }

echo "=== two changed plays, confirmed ==="
printf 'RUN %s\nRUN %s\n' "$A" "$B" >"$MARKERS"
run_changed 'y\n' -- --changed
check "exits 0" "0" "$rc"
check "asks the judge once, about this checkout" "1" "$(calls | grep -c "^judge: --repo-root $CHECKOUT$")"
check "runs both, in the judge's order" "play-a.yml play-b.yml " "$(plays_run)"
check "each under the play lock" "2" "$(calls | grep -c 'lock=held')"
check "lists them before asking" "yes" "$(yes_if grep -q "$A" <<<"$out")"
check "and says it finished" "yes" "$(yes_if grep -q 'All 2 changed play(s) ran' <<<"$out")"

echo "=== declined ==="
run_changed 'n\n' -- --changed
check "exits 0" "0" "$rc"
check "runs nothing" "" "$(plays_run)"

echo "=== a failure stops the run ==="
printf 'RUN %s\nRUN %s\nRUN %s\n' "$A" "$B" "$C" >"$MARKERS"
run_changed 'y\nn\n' FAKE_FAIL_PLAY=play-b.yml -- --changed
check "exits with the failed play's status" "4" "$rc"
check "runs up to and including the failure, and no further" "play-a.yml play-b.yml " "$(plays_run)"
check "names the play that failed" "yes" "$(yes_if grep -q 'play-b' <<<"$out")"
check "and the one not run" "yes" "$(yes_if grep -q "Not run: $C" <<<"$out")"

echo "=== nothing changed ==="
: >"$MARKERS"
run_changed '' -- --changed
check "exits 0" "0" "$rc"
check "runs nothing" "" "$(plays_run)"
check "says nothing changed" "yes" "$(yes_if grep -q 'No play has changed since it last ran here' <<<"$out")"

echo "=== plays the judge cannot follow, and plays that are gone ==="
printf 'UNRESOLVED %s %s:12: {{ root_dir }}/vars/x.yml\nGONE %s\nRUN %s\n' "$B" "$B" "$C" "$A" >"$MARKERS"
run_changed 'y\n' -- --changed
check "exits 0" "0" "$rc"
check "runs only the changed play" "play-a.yml " "$(plays_run)"
check "names the play it cannot judge, and the reference" "yes" \
    "$(yes_if grep -q "$B.*vars/x.yml" <<<"$out")"
check "names the play that is gone" "yes" "$(yes_if grep -q "$C" <<<"$out")"
printf 'UNRESOLVED %s %s:12: ref\n' "$B" "$B" >"$MARKERS"
run_changed '' -- --changed
check "with only those, nothing runs and it still names them" "yes" \
    "$([ -z "$(plays_run)" ] && grep -q "$B" <<<"$out" && echo yes || echo no)"

echo "=== the judge cannot answer ==="
printf 'RUN %s\n' "$A" >"$MARKERS"
run_changed 'y\n' TEST_JUDGE_RC=2 TEST_JUDGE_ERR="changed-plays: the ledger cannot be read" -- --changed
check "refuses" "1" "$rc"
check "runs nothing, not even what it printed" "" "$(plays_run)"
check "passes on the judge's reason" "yes" "$(yes_if grep -q 'the ledger cannot be read' <<<"$out")"

echo "=== a marker it does not know ==="
printf 'RUN %s\nMAYBE %s\n' "$A" "$B" >"$MARKERS"
run_changed 'y\n' -- --changed
check "refuses" "1" "$rc"
check "and runs nothing" "" "$(plays_run)"

echo "=== flags it does not combine with ==="
printf 'RUN %s\n' "$A" >"$MARKERS"
run_changed 'y\n' -- --changed "$A"
check "with a playbook path: refused" "1" "$rc"
run_changed 'y\n' -- --changed --optional-only
check "with --optional-only: refused" "1" "$rc"
run_changed 'y\n' -- --changed --headless
check "headless: refused (it asks before running anything)" "1" "$rc"
check "none of those ran anything" "" "$(plays_run)"

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
