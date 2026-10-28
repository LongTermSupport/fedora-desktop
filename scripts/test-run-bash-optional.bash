#!/usr/bin/env bash
# The developer path's optional plays go on past a failure (Plan 00228 Task 6.6): the headless
# RUN_BASH_OPTIONAL_PLAYBOOKS list and the attended menu's A, W and B choices run every chosen
# play, record each outcome, and end with a summary naming every installed and failed play and a
# non-zero exit when any failed. A play ended by a signal, or by Ctrl-C (Ansible's exit 99),
# stops at once, after the summary. --rerun's optional part is driven through the real run.bash
# in scripts/test-run-bash-rerun.bash.
#
# The functions under test are extracted out of run.bash with awk and sourced (run.bash itself
# provisions on load); each is bounded by its `name() {` or `name(){` line and the first `^}`
# after it, so a rename fails the extraction loudly. ansible-playbook and sudo (NOPASSWD) are
# stubs on PATH; the checkout is a throwaway one under a fake home.
#
# `set -e` is deliberately NOT used here: every case must run so the summary shows the whole
# picture, and each result is checked explicitly. The code under test runs in a child bash under
# run.bash's own options (errexit, nounset, pipefail, IFS of newline and tab).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_BASH="$REPO_ROOT/run.bash"

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

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/optional-plays-test.XXXXXX")"
trap 'rm -rf "$SCRATCH"' EXIT

FNS="$SCRATCH/fns.bash"
: >"$FNS"
for fn in info success warning error hl_abort fatal confirm run_playbook_with_issue_option \
    run_playbook optional_summary _parse_number_list show_menu hl_run_optional_playbooks; do
    awk -v fn="$fn" '($0 == fn "() {" || $0 == fn "(){") {p=1} p {print} p && /^\}/ {exit}' \
        "$RUN_BASH" >>"$FNS"
    if ! grep -qE "^${fn}\(\) ?\{" "$FNS"; then
        echo "FAIL: could not extract ${fn} from run.bash" >&2
        exit 1
    fi
done

FAKE_HOME="$SCRATCH/home"
CHECKOUT="$FAKE_HOME/Projects/fedora-desktop"
OPT="playbooks/imports/optional/common"
mkdir -p "$CHECKOUT/$OPT"
for name in play-a play-b play-c; do
    printf -- '- hosts: localhost\n  tasks: []\n' >"$CHECKOUT/$OPT/$name.yml"
done

BIN="$SCRATCH/bin"
LOG="$SCRATCH/calls.log"
mkdir -p "$BIN"
cat >"$BIN/ansible-playbook" <<'EOF'
#!/usr/bin/env bash
play="${1##*/}"
printf 'play: %s %s\n' "$play" "${*:2}" >>"$TEST_LOG"
for spec in ${FAKE_RC:-}; do
    if [ "${spec%%=*}" = "$play" ]; then exit "${spec#*=}"; fi
done
exit 0
EOF
cat >"$BIN/sudo" <<'EOF'
#!/usr/bin/env bash
[ "$*" = "-k -n true" ] && exit 0
echo "fake sudo: unexpected call: $*" >&2
exit 97
EOF
chmod 755 "$BIN/ansible-playbook" "$BIN/sudo"

# The child shell: run.bash's options and the globals the functions read, then the case body.
cat >"$SCRATCH/harness.bash" <<'EOF'
set -euo pipefail
IFS=$'\n\t'
RED="" GREEN="" YELLOW="" CYAN="" BOLD="" NC="" CHECK="ok" CROSS="x" ARROW=">" INFO="i" WARN="!"
RUN_BASH_VERSION=test HEADLESS="${HEADLESS:-false}" CONSUMER_BOX=false ASSUME_YES=false
HL_SUDO_OPTS=() HL_SUDO_PW_FILE="" OPTIONAL_RESULTS=()
# shellcheck source=/dev/null
source "$1"
cd "$HOME/Projects/fedora-desktop"
case "$2" in
    headless)
        hl_run_optional_playbooks
        ;;
    menu)
        mapfile -t pbs < <(printf '%s\n' playbooks/imports/optional/common/*.yml)
        show_menu Common "${pbs[@]}"
        ;;
esac
optional_summary
echo REACHED-THE-END
EOF

# run_case <stdin text> <headless|menu> [env assignments...] — sets out and rc.
run_case() {
    local input="$1" what="$2"
    shift 2
    : >"$LOG"
    out="$(printf '%b' "$input" | env HOME="$FAKE_HOME" PATH="$BIN:/usr/bin:/bin" TEST_LOG="$LOG" "$@" \
        bash "$SCRATCH/harness.bash" "$FNS" "$what" 2>&1)"
    rc=$?
}
plays_run() { awk '/^play: / { printf "%s ", $2 }' "$LOG"; }
said() { yes_if grep -qF -- "$1" <<<"$out"; }

echo "=== headless RUN_BASH_OPTIONAL_PLAYBOOKS ==="
run_case '' headless HEADLESS=true RUN_BASH_OPTIONAL_PLAYBOOKS="a b c"
check "every play passes: exit 0" "0" "$rc"
check "runs them in the order given" "play-a.yml play-b.yml play-c.yml " "$(plays_run)"
check "the summary names each one installed" "yes" "$(said "$OPT/play-c.yml: installed")"
check "and the run goes on to the end" "yes" "$(said REACHED-THE-END)"

run_case '' headless HEADLESS=true RUN_BASH_OPTIONAL_PLAYBOOKS="a b c" FAKE_RC="play-b.yml=2"
check "a failed play: exit 1" "1" "$rc"
check "the plays after it still run" "play-a.yml play-b.yml play-c.yml " "$(plays_run)"
check "the summary names the failed play and its status" "yes" "$(said "$OPT/play-b.yml: failed (exit 2)")"
check "and the ones installed" "yes" "$(said "$OPT/play-a.yml: installed")"
check "says how many failed" "yes" "$(said "1 of 3 optional play(s) failed")"
check "names the command to retry the failed one" "yes" "$(said "./run.bash $OPT/play-b.yml")"
check "never asks to file an issue unattended" "no" "$(said "GitHub issue")"
check "and the run ends there, not at the end" "no" "$(said REACHED-THE-END)"

run_case '' headless HEADLESS=true RUN_BASH_OPTIONAL_PLAYBOOKS="a nosuch c"
check "an unknown name: refused" "1" "$rc"
check "before any play runs" "" "$(plays_run)"

run_case '' headless HEADLESS=true RUN_BASH_OPTIONAL_PLAYBOOKS="a b c" FAKE_RC="play-b.yml=130"
check "a play ended by a signal: its status is the run's" "130" "$rc"
check "nothing after it runs" "play-a.yml play-b.yml " "$(plays_run)"
check "the summary says it stopped part way" "yes" "$(said "$OPT/play-b.yml: stopped part way (exit 130)")"

run_case '' headless HEADLESS=true RUN_BASH_OPTIONAL_PLAYBOOKS="a b c" FAKE_RC="play-a.yml=99"
check "Ctrl-C in a play (Ansible's exit 99) stops the run too" "99" "$rc"
check "and nothing after it runs" "play-a.yml " "$(plays_run)"

echo "=== the attended menu ==="
run_case 'A\n' menu FAKE_RC="play-a.yml=4"
check "A with a failure: exit 1" "1" "$rc"
check "A runs every play" "play-a.yml play-b.yml play-c.yml " "$(plays_run)"
check "A's summary names the failed play" "yes" "$(said "$OPT/play-a.yml: failed (exit 4)")"
check "and the installed ones" "yes" "$(said "$OPT/play-c.yml: installed")"
check "and points at the menu again" "yes" "$(said "./run.bash --optional-only")"

run_case 'W\n1 3\n' menu FAKE_RC="play-a.yml=4"
check "W with a failure: exit 1" "1" "$rc"
check "W runs the whitelisted plays, past the failure" "play-a.yml play-c.yml " "$(plays_run)"
check "W's summary says 1 of 2 failed" "yes" "$(said "1 of 2 optional play(s) failed")"

run_case 'B\n2\n' menu FAKE_RC="play-a.yml=4 play-c.yml=5"
check "B with two failures: exit 1" "1" "$rc"
check "B runs every play not skipped" "play-a.yml play-c.yml " "$(plays_run)"
check "B's summary names the first failure" "yes" "$(said "$OPT/play-a.yml: failed (exit 4)")"
check "and the second" "yes" "$(said "$OPT/play-c.yml: failed (exit 5)")"
check "and says 2 of 2 failed" "yes" "$(said "2 of 2 optional play(s) failed")"

run_case 'A\n' menu
check "A with every play passing: exit 0" "0" "$rc"
check "and the run goes on" "yes" "$(said REACHED-THE-END)"
check "says nothing failed" "no" "$(said "failed")"

run_case 'S\n' menu
check "nothing chosen: exit 0" "0" "$rc"
check "no summary is printed" "no" "$(said "what happened")"

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
