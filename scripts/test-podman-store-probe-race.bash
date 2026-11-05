#!/usr/bin/env bash
# Test the rootless store ownership probe (tasks/podman-store-ownership-probe.yml) against a tree
# that changes while find walks it, as a running container's upper dir does (issue #152).
#
# GNU find under -ignore_readdir_race makes every stat test false for an entry that vanished
# between readdir and stat, so a NEGATED stat test is true and the probe printed a deleted file of
# the user's own as "owned outside the subuid range". The probe now puts -links +0 ahead of the
# negations: it is false for an entry that cannot be stat'ed, and once the stat succeeded find
# reuses it for every later test, so the negations judge a file that existed.
#
# The probe's real argv is read from the task file (only the templated values are substituted),
# so this tests what the play runs. The race is a race: the unguarded form is run until it prints
# a vanished file (it did 13 times in 300 runs on the host), bounded; the guarded form must print
# nothing in GUARDED_RUNS runs over the same churn, and must still find a file that is really
# owned outside the range.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TASK="$(cd "$SCRIPT_DIR/.." && pwd)/tasks/podman-store-ownership-probe.yml"
TASK_NAME="Find a File in the Store Owned Outside the User's uid and subuid Range"
GUARDED_RUNS=400
UNGUARDED_MAX_RUNS=4000
CHURN_FILES=200

# The shell may define find as a function wrapping another implementation; the play runs GNU find.
FIND="$(type -P find)" || { echo "FAIL: no find on PATH" >&2; exit 1; }
if ! find_version="$("$FIND" --version 2>&1)" || [[ $find_version != *"GNU findutils"* ]]; then
    echo "FAIL: $FIND is not GNU findutils, which is what the play runs: $find_version" >&2
    exit 1
fi

WORK_DIR="$(mktemp -d)"
CHURN_PID=""
cleanup() {
    [ -n "$CHURN_PID" ] && kill "$CHURN_PID" 2>/dev/null
    wait 2>/dev/null
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "ok: $label"
    else
        failed=$((failed + 1))
        echo "FAIL: $label: want [$want], got [$got]"
    fi
}

# probe_argv DIR USER_UID: the task's find argv, NUL-separated, with the store path, the user's
# uid and the subuid range 100000-165535 put in for the templates.
probe_argv() {
    python3 -I - "$TASK" "$TASK_NAME" "$1" "$2" <<'PY'
import sys, yaml
task_file, name, store, uid = sys.argv[1:]
task = next(t for t in yaml.safe_load(open(task_file)) if t.get("name") == name)
out = []
for item in task["ansible.builtin.command"]["argv"]:
    if not isinstance(item, str):
        sys.exit(f"argv item {item!r} is not a string: YAML read it as {type(item).__name__}, quote it")
    if "{{" in item:
        if "podman_store_home" in item:
            item = store
        elif "getent_passwd" in item:
            item = uid
        elif item.startswith("+"):
            item = "+99999"
        elif item.startswith("-"):
            item = "-165536"
        else:
            sys.exit(f"unknown template in the probe argv: {item}")
    out.append(item)
sys.stdout.write("\0".join(out))
PY
}

me="$(id -u)"
churn="$WORK_DIR/overlay"
mkdir -p "$churn"
if ! probe_argv "$churn" "$me" >"$WORK_DIR/argv"; then
    echo "FAIL: cannot read the probe's argv from $TASK" >&2
    exit 1
fi
mapfile -d '' guarded <"$WORK_DIR/argv"
check "the probe's argv is read from the task" "find" "${guarded[0]:-none}"
unguarded=("${guarded[@]}")
guard_at=-1
for i in "${!guarded[@]}"; do
    if [ "${guarded[$i]}" = "-links" ] && [ "${guarded[$((i + 1))]:-}" = "+0" ]; then
        guard_at=$i
        break
    fi
done
first_negation=-1
for i in "${!guarded[@]}"; do
    if [ "${guarded[$i]}" = "!" ]; then
        first_negation=$i
        break
    fi
done
check "the probe tests -links +0 before its first negation" "yes" \
    "$([ "$guard_at" -ge 0 ] && [ "$guard_at" -lt "$first_negation" ] && echo yes || echo no)"
if [ "$guard_at" -ge 0 ]; then
    unguarded=("${guarded[@]:0:guard_at}" "${guarded[@]:guard_at+2}")
fi

# An entry really owned outside the range is still found. As root the file is given to a spare
# uid, and the directory above it is root's, which the probe excludes. As a user the probe is
# told the user is someone else, so the directory itself, the user's own, is the first entry it
# meets owned outside the range, and -quit stops there.
real="$WORK_DIR/real"
mkdir -p "$real"
: >"$real/foreign"
other_uid=4243
[ "$me" = "$other_uid" ] && other_uid=4244
expect_found="$real"
if [ "$me" = 0 ]; then
    chown 4242 "$real/foreign" || check "a test file can be given to uid 4242" "yes" "no"
    expect_found="$real/foreign"
fi
if ! probe_argv "$real" "$other_uid" >"$WORK_DIR/argv"; then
    echo "FAIL: cannot read the probe's argv from $TASK" >&2
    exit 1
fi
mapfile -d '' control <"$WORK_DIR/argv"
check "the probe finds an entry owned outside the range" "$expect_found" \
    "$("$FIND" "${control[@]:1}" 2>&1)"

# The churn: files of the test's own uid (never a match) created and deleted while find walks.
(
    for _ in $(seq 1 1000000); do
        [ -e "$WORK_DIR/stop" ] && exit 0
        for n in $(seq 1 "$CHURN_FILES"); do
            : >"$churn/tmp.$n"
        done
        rm -f "$churn"/tmp.*
    done
) &
CHURN_PID=$!

# Each run's stdout and stderr together: under -ignore_readdir_race find says nothing and exits 0
# for a vanished entry, so any text at all is a match (or an error, which fails the run too).
reproduced=0
runs=0
while [ "$runs" -lt "$UNGUARDED_MAX_RUNS" ] && [ "$reproduced" = 0 ]; do
    runs=$((runs + 1))
    if ! out="$("$FIND" "${unguarded[@]:1}" 2>&1)"; then
        check "the unguarded probe runs" "exit 0" "failed: $out"
        break
    fi
    [ -n "$out" ] && reproduced=1
done
echo "unguarded form: printed a vanished file after $runs run(s)"
check "without the guard the probe prints a vanished file (the defect reproduces)" 1 "$reproduced"

false_matches=0
for _ in $(seq 1 "$GUARDED_RUNS"); do
    if ! out="$("$FIND" "${guarded[@]:1}" 2>&1)" || [ -n "$out" ]; then
        false_matches=$((false_matches + 1))
        [ "$false_matches" -le 3 ] && echo "  guarded form printed: $out"
    fi
done
check "with the guard the probe prints nothing in $GUARDED_RUNS runs over the churn" 0 "$false_matches"

: >"$WORK_DIR/stop"
wait "$CHURN_PID"
CHURN_PID=""

echo "passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
