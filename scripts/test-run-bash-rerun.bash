#!/usr/bin/env bash
# Drive `run.bash --rerun` end to end (Plan 00141): list every play that has run here as a
# numbered menu, read the operator's pick from stdin, run the pick in menu order through the
# single-play runner under the play lock, and stop at the first failure. A mistyped answer
# is named and asked again, a bounded number of times.
#
# The REAL run.bash and the REAL lock helper run in a throwaway checkout. Which plays exist
# and their states come from helpers/play_ledger/changed_plays.py --all, tested on its own
# (tests/helpers/play_ledger/test_changed_plays.py); here a stand-in prints the marker
# lines a case writes. ansible-playbook, sudo (NOPASSWD), git and whoami are stubs, and
# the operator's answers arrive on stdin.
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
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/rerun-plays-test.XXXXXX")"
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
for name in play-a play-b play-c play-d; do
    printf -- '- hosts: localhost\n  tasks: []\n' >"$CHECKOUT/playbooks/imports/$name.yml"
done
A="playbooks/imports/play-a.yml"
B="playbooks/imports/play-b.yml"
C="playbooks/imports/play-c.yml"
D="playbooks/imports/play-d.yml"

RUNTIME="$SCRATCH/runtime"
mkdir -m 700 "$RUNTIME"
LOCK="$RUNTIME/fedora-desktop-plays.lock"
LOG="$SCRATCH/calls.log"
MARKERS="$SCRATCH/markers"
FAKE_HOME="$SCRATCH/home"
mkdir -p "$FAKE_HOME/.local/bin"

BIN="$SCRATCH/bin"
mkdir -p "$BIN"
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
cat >"$BIN/git" <<'EOF'
#!/usr/bin/env bash
if [ "$*" = "-C $TEST_CHECKOUT status --porcelain" ]; then
    printf '%s' "${TEST_DIRTY:-}"
    exit "${TEST_GIT_RC:-0}"
fi
echo "fake git: unexpected call: $*" >&2
exit 97
EOF
chmod 755 "$BIN/ansible-playbook" "$BIN/sudo" "$BIN/whoami" "$BIN/git"

# run_rerun <stdin text> [env assignments...] -- <run.bash args...>
run_rerun() {
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
        TEST_LOG="$LOG" TEST_LOCK="$LOCK" TEST_MARKERS="$MARKERS" TEST_CHECKOUT="$CHECKOUT" "${assigns[@]}" \
        bash "$CHECKOUT/run.bash" "$@" 2>&1)"
    rc=$?
}
calls() { if [ -f "$LOG" ]; then cat "$LOG"; fi; }
plays_run() { calls | awk '/^play: / { printf "%s ", $2 }'; }
menu_row() { grep -E "^ +[* ] +$1\) " <<<"$out"; }

# A: stale, B: current, C: failed, D: cannot tell
printf 'PLAY stale %s\nPLAY current %s\nPLAY failed %s\nPLAY unresolved %s\n' "$A" "$B" "$C" "$D" >"$MARKERS"

echo "=== the menu ==="
run_rerun 'q\n' -- --rerun
check "q exits 0" "0" "$rc"
check "asks the judge once, for all plays, about this checkout" "1" \
    "$(calls | grep -c "^judge: --repo-root $CHECKOUT --all$")"
check "runs nothing" "" "$(plays_run)"
check "says nothing was run" "yes" "$(yes_if grep -q 'Nothing was run' <<<"$out")"
check "numbers every play, in the judge's order" "yes" \
    "$(yes_if grep -Eq "1\) $A.*2\) $B.*3\) $C.*4\) $D" <<<"$(tr '\n' ' ' <<<"$out")")"
check "marks a changed play" "yes" "$(yes_if grep -q '^  \* *1) .*changed since' <<<"$out")"
check "marks a failed play" "yes" "$(yes_if grep -q '^  \* *3) .*last run failed' <<<"$out")"
check "does not mark an unchanged play" "yes" "$(yes_if grep -q '^    *2) .*unchanged' <<<"$out")"
check "does not mark a play it cannot judge" "yes" "$(yes_if grep -q '^    *4) .*cannot tell' <<<"$out")"

echo "=== picks ==="
run_rerun '3\n' -- --rerun
check "one number: exits 0" "0" "$rc"
check "runs just that play" "play-c.yml " "$(plays_run)"
check "under the play lock" "1" "$(calls | grep -c 'lock=held')"
check "and says it finished" "yes" "$(yes_if grep -q 'All 1 selected play(s) ran' <<<"$out")"

run_rerun '4 1\n' -- --rerun
check "several numbers run in menu order, not typed order" "play-a.yml play-d.yml " "$(plays_run)"
run_rerun '3,1,3\n' -- --rerun
check "commas work and a repeated number runs once" "play-a.yml play-c.yml " "$(plays_run)"
run_rerun 'a\n' -- --rerun
check "a runs every play marked changed or failed, and no other" "play-a.yml play-c.yml " "$(plays_run)"
run_rerun 'A\n' -- --rerun
check "a is case-insensitive" "play-a.yml play-c.yml " "$(plays_run)"
run_rerun 'Q\n' -- --rerun
check "Q quits too" "" "$(plays_run)"

echo "=== a failure stops the run ==="
run_rerun '1 2 3\n' FAKE_FAIL_PLAY=play-b.yml -- --rerun
check "exits with the failed play's status" "4" "$rc"
check "runs up to and including the failure" "play-a.yml play-b.yml " "$(plays_run)"
check "names the one not run" "yes" "$(yes_if grep -q "Not run: $C" <<<"$out")"

echo "=== a mistyped answer is named and asked again ==="
run_rerun 'banana\n2\n' -- --rerun
check "a typo then a valid pick: exits 0" "0" "$rc"
check "runs the valid pick" "play-b.yml " "$(plays_run)"
check "names the typo" "yes" "$(yes_if grep -q "'banana' is not a choice" <<<"$out")"
run_rerun '9\n0\n1\n' -- --rerun
check "out-of-range numbers are refused, then a valid one runs" "play-a.yml " "$(plays_run)"
check "the error states the range" "yes" "$(yes_if grep -q 'numbers from 1 to 4' <<<"$out")"
run_rerun '\n1\n' -- --rerun
check "an empty answer is refused, not treated as a choice" "play-a.yml " "$(plays_run)"
run_rerun '1 x\n1\n' -- --rerun
check "one bad word refuses the whole answer; nothing half-runs" "play-a.yml " "$(plays_run)"
run_rerun 'q 1\nq\n' -- --rerun
check "q cannot be combined with a number" "yes" "$(yes_if grep -q 'q quits on its own' <<<"$out")"
check "and nothing ran" "" "$(plays_run)"
run_rerun 'a 1\n1\n' -- --rerun
check "a cannot be combined with a number" "yes" "$(yes_if grep -q 'a means every play marked' <<<"$out")"
run_rerun 'x\ny\nz\n1\n' -- --rerun
check "three bad answers in a row: gives up, exit 1" "1" "$rc"
check "runs nothing, even though a valid answer was next" "" "$(plays_run)"
check "says it gave up" "yes" "$(yes_if grep -q 'Giving up after 3' <<<"$out")"
run_rerun '' -- --rerun
check "end of input cancels: exit 1, nothing run" "1" "$rc"
check "and says nothing was run" "yes" "$(yes_if grep -q 'nothing was run' <<<"$out")"

echo "=== a menu with nothing marked ==="
printf 'PLAY current %s\nPLAY current %s\n' "$A" "$B" >"$MARKERS"
run_rerun 'a\nq\n' -- --rerun
check "a has nothing to pick, and says so" "yes" "$(yes_if grep -q 'No play is marked' <<<"$out")"
check "nothing ran" "" "$(plays_run)"
run_rerun '2\n' -- --rerun
check "a current play can still be picked by number" "play-b.yml " "$(plays_run)"

echo "=== plays that are gone ==="
printf 'PLAY stale %s\nGONE %s\n' "$A" "$C" >"$MARKERS"
run_rerun '1\n' -- --rerun
check "the gone play is named" "yes" "$(yes_if grep -q "no longer in the checkout.*" <<<"$out")"
check "and is not numbered in the menu" "0" "$(menu_row 2 | grep -c .)"
check "the rest runs" "play-a.yml " "$(plays_run)"

echo "=== a dirty checkout ==="
printf 'PLAY stale %s\n' "$A" >"$MARKERS"
run_rerun '1\n' TEST_DIRTY=" M docs/x.md" -- --rerun
check "warns, still runs" "play-a.yml " "$(plays_run)"
check "says the checkout has uncommitted changes" "yes" "$(yes_if grep -q 'uncommitted changes' <<<"$out")"
run_rerun '1\n' TEST_GIT_RC=128 -- --rerun
check "git status failing: refused" "1" "$rc"
check "and runs nothing" "" "$(plays_run)"

echo "=== nothing has run here ==="
: >"$MARKERS"
run_rerun '' -- --rerun
check "exits 0" "0" "$rc"
check "says there is nothing to re-run" "yes" "$(yes_if grep -q 'nothing to re-run' <<<"$out")"

echo "=== the judge cannot answer ==="
printf 'PLAY stale %s\n' "$A" >"$MARKERS"
run_rerun '1\n' TEST_JUDGE_RC=2 TEST_JUDGE_ERR="changed-plays: the ledger cannot be read" -- --rerun
check "refuses" "1" "$rc"
check "runs nothing" "" "$(plays_run)"
check "passes on the judge's reason" "yes" "$(yes_if grep -q 'the ledger cannot be read' <<<"$out")"

echo "=== markers it does not know ==="
printf 'PLAY stale %s\nMAYBE %s\n' "$A" "$B" >"$MARKERS"
run_rerun '1\n' -- --rerun
check "an unknown line: refused" "1" "$rc"
printf 'PLAY odd %s\n' "$A" >"$MARKERS"
run_rerun '1\n' -- --rerun
check "an unknown state: refused" "1" "$rc"
check "and nothing ran" "" "$(plays_run)"

echo "=== flags it does not combine with ==="
printf 'PLAY stale %s\n' "$A" >"$MARKERS"
run_rerun '1\n' -- --rerun "$A"
check "with a playbook path: refused" "1" "$rc"
run_rerun '1\n' -- --rerun --optional-only
check "with --optional-only: refused" "1" "$rc"
run_rerun '1\n' -- --rerun --headless
check "headless: refused (it asks before running anything)" "1" "$rc"
run_rerun '1\n' -- --rerun --changed
check "with --changed: refused" "1" "$rc"
check "none of those ran anything" "" "$(plays_run)"

echo "=== --help ==="
run_rerun '' -- --help
check "--help documents --rerun" "yes" "$(yes_if grep -q -- '--rerun' <<<"$out")"

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
