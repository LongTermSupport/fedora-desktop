#!/usr/bin/env bash
# Unit-test scripts/qa-ccy-merge-commits.bash (issue #311): CI judges every merge commit of a range
# by the pre-commit ccy version gates, as if each were being committed, so a merge made with
# --no-verify, or completed by a client with no hooks, cannot carry an unbumped ccy change past the
# protected line. It runs against throwaway repositories whose merges were all committed with
# --no-verify, and the real hook, libraries and Go tool of this checkout.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOOL="$SCRIPT_DIR/qa-ccy-merge-commits.bash"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}
has() { if grep -q -F -- "$1" "$work/out"; then echo yes; else echo no; fi; }

# repo — a fresh repository at ccy 1.0.0 with two libraries on F44; prints its path.
repo() {
    local m
    m=$(mktemp -d -p "$work" repo.XXXXXX)
    git -C "$m" init -q -b F44
    git -C "$m" config user.name test
    git -C "$m" config user.email test@example.com
    mkdir -p "$m/files/var/local/claude-yolo/lib"
    put "$m" 1.0.0 a 0 b 0 base
    echo "$m"
}
# put <repo> <ccy-version> <lib> <value> [<lib> <value>...] <message> — set and commit, no hooks.
put() {
    local m="$1" lib="$1/files/var/local/claude-yolo/lib"
    printf '#!/usr/bin/env bash\nCCY_VERSION="%s"\n' "$2" >"$m/files/var/local/claude-yolo/claude-yolo"
    shift 2
    while [ "$#" -gt 1 ]; do
        printf 'VALUE=%s\n' "$2" >"$lib/$1.bash"
        shift 2
    done
    git -C "$m" add -A
    git -C "$m" commit -q --no-verify -m "$1"
}
# judge <repo> <tip> <range> — run the tool in the repository; print its status.
judge() {
    local rc=0
    (cd "$1" && "$TOOL" "$2" "$3") >"$work/out" 2>&1 || rc=$?
    echo "$rc"
}

echo "=== qa-ccy-merge-commits (issue #311) ==="

# F44 and a work branch both move to 1.0.1 with different library changes: identical version lines,
# so git merges cleanly, and the merge owes a bump it never made.
m=$(repo)
git -C "$m" switch -q -c work
put "$m" 1.0.1 b 1 work
git -C "$m" switch -q F44
put "$m" 1.0.1 a 1 f44
tip=$(git -C "$m" rev-parse F44)
git -C "$m" switch -q work
git -C "$m" merge -q --no-verify --no-ff -m sync F44
sync=$(git -C "$m" rev-parse HEAD)
check "an unbumped merge committed past the hook is refused" "1 yes" "$(judge "$m" "$tip" "$tip..HEAD") $(has 'CCY version bump required')"
check "…and the refusal names the merge" "yes" "$(has "✗ merge $sync is refused")"

put "$m" 1.0.2 b 2 bumped
git -C "$m" switch -q F44
git -C "$m" merge -q --no-verify --no-ff -m land work
land=$(git -C "$m" rev-parse HEAD)
check "the bump made after it does not excuse it, and the later merge is judged too" "1 yes yes no" \
    "$(judge "$m" "$tip" "$tip..HEAD") $(has "✗ merge $sync is refused") $(has "🔍 merge $land") $(has "✗ merge $land")"

# Two syncs of F44 changing the same library: the second merge's checkout must not trip over the
# first merge's tree, left staged in the scratch clone.
m=$(repo)
git -C "$m" switch -q -c work
put "$m" 1.0.1 b 1 work
git -C "$m" switch -q F44
put "$m" 1.0.1 a 1 f44
git -C "$m" switch -q work
git -C "$m" merge -q --no-verify --no-ff --no-commit F44
put "$m" 1.0.2 a 1 b 1 sync-one
one=$(git -C "$m" rev-parse HEAD)
git -C "$m" switch -q F44
put "$m" 1.0.3 a 2 f44-again
tip=$(git -C "$m" rev-parse F44)
git -C "$m" switch -q work
git -C "$m" merge -q --no-verify --no-ff --no-commit F44
put "$m" 1.0.4 a 2 b 1 sync-two
two=$(git -C "$m" rev-parse HEAD)
first_tip=$(git -C "$m" rev-parse F44~1)
check "two syncs changing one library are both judged, and pass" "0 yes yes" \
    "$(judge "$m" "$first_tip" "$first_tip..HEAD") $(has "🔍 merge $one") $(has "🔍 merge $two")"

# The same work merged above F44's version passes, as does a range with no merge at all.
m=$(repo)
git -C "$m" switch -q -c work
put "$m" 1.0.1 b 1 work
git -C "$m" switch -q F44
put "$m" 1.0.1 a 1 f44
tip=$(git -C "$m" rev-parse F44)
git -C "$m" switch -q work
git -C "$m" merge -q --no-verify --no-ff --no-commit F44
put "$m" 1.0.2 a 1 b 1 sync
check "a merge above F44's version passes" "0" "$(judge "$m" "$tip" "$tip..HEAD")"
check "a range with no merge passes" "0 yes" "$(judge "$m" "$tip" "$tip..$tip") $(has 'no merge commits')"

# The tip is the line before the merges: one that is not a commit cannot judge.
check "a tip that is not a commit cannot judge" "2" "$(judge "$m" 0000000000000000000000000000000000000000 "$tip..HEAD")"
check "a missing argument is a usage error" "64" "$(judge "$m" "$tip" "")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
