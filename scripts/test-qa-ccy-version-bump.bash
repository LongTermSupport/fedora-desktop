#!/usr/bin/env bash
# Unit-test scripts/qa-ccy-version-bump.bash (issue #428): qa-changed (and so CI's diff mode) judges
# the change from its base by the pre-commit hook's ccy version gates, so a ccy image source changed
# where no hook ran cannot reach a protected line without the container version bump. It runs
# against throwaway repositories whose commits were all made with --no-verify, and the real hook,
# libraries and Go tools of this checkout. The last cases hold qa-changed's routing row for the
# check over every path the hook counts as an image or program input in this checkout.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TOOL="$SCRIPT_DIR/qa-ccy-version-bump.bash"
CTX=files/var/local/claude-yolo

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

# versions <repo> <ccy> <required> <label> — write the launcher's two versions and the image LABEL.
versions() {
    printf '#!/usr/bin/env bash\nCCY_VERSION="%s"\nREQUIRED_CONTAINER_VERSION="%s"\n' "$2" "$3" >"$1/$CTX/claude-yolo"
    printf 'FROM scratch\nCOPY entrypoint.sh /usr/local/bin/\nLABEL claude-yolo-version="%s"\n' "$4" >"$1/$CTX/Dockerfile"
}
# commit <repo> <message> — stage everything and commit with no hooks.
commit() {
    git -C "$1" add -A
    git -C "$1" commit -q --no-verify -m "$2"
}
# repo — a fresh repository at ccy 1.0.0, container 1.0.0, on F44; prints its path.
repo() {
    local m
    m=$(mktemp -d -p "$work" repo.XXXXXX)
    git -C "$m" init -q -b F44
    git -C "$m" config user.name test
    git -C "$m" config user.email test@example.com
    mkdir -p "$m/$CTX/lib"
    versions "$m" 1.0.0 1.0.0 1.0.0
    echo 'echo one' >"$m/$CTX/entrypoint.sh"
    echo 'VALUE=0' >"$m/$CTX/lib/a.bash"
    echo 'notes' >"$m/README.txt"
    commit "$m" base
    echo "$m"
}
# judge <repo> <arg>... — run the tool in the repository; print its status.
judge() {
    local m="$1" rc=0
    shift
    (cd "$m" && "$TOOL" "$@") >"$work/out" 2>&1 || rc=$?
    echo "$rc"
}

echo "=== qa-ccy-version-bump (issue #428) ==="

# An image source changed with no bump, committed past the hook: the range is refused.
m=$(repo)
base=$(git -C "$m" rev-parse HEAD)
echo 'echo two' >"$m/$CTX/entrypoint.sh"
commit "$m" "fix the entrypoint"
check "an image source changed without the container bump is refused" "1 yes" "$(judge "$m" "$base" HEAD) $(has 'CCY-IMAGE-INPUT')"
check "…and the refusal names the file" "yes" "$(has "$CTX/entrypoint.sh")"

# The same range with the bump made in a later commit: the range is judged as one change.
versions "$m" 1.0.1 1.0.1 1.0.1
commit "$m" bump
check "the bump in a later commit of the range passes" "0 yes" "$(judge "$m" "$base" HEAD) $(has 'container version bumped: 1.0.0 → 1.0.1')"

# The reverse: the source change and the bump in one commit, the bump reverted in a later one.
m=$(repo)
base=$(git -C "$m" rev-parse HEAD)
echo 'echo two' >"$m/$CTX/entrypoint.sh"
versions "$m" 1.0.1 1.0.1 1.0.1
commit "$m" "fix the entrypoint with the bump"
versions "$m" 1.0.0 1.0.0 1.0.0
commit "$m" "revert the bump"
check "a bump reverted later in the range is refused" "1 yes" "$(judge "$m" "$base" HEAD) $(has 'CCY-IMAGE-INPUT')"

# The LABEL moved but REQUIRED_CONTAINER_VERSION did not.
m=$(repo)
base=$(git -C "$m" rev-parse HEAD)
echo 'echo two' >"$m/$CTX/entrypoint.sh"
versions "$m" 1.0.1 1.0.0 1.0.1
commit "$m" "half a bump"
check "a LABEL that differs from REQUIRED_CONTAINER_VERSION is refused" "1 yes" "$(judge "$m" "$base" HEAD) $(has 'differ')"

# A launcher library changed without CCY_VERSION.
m=$(repo)
base=$(git -C "$m" rev-parse HEAD)
echo 'VALUE=1' >"$m/$CTX/lib/a.bash"
commit "$m" "change a library"
check "a ccy library changed without CCY_VERSION is refused" "1 yes" "$(judge "$m" "$base" HEAD) $(has 'CCY version bump required')"

# Nothing of ccy changed.
m=$(repo)
base=$(git -C "$m" rev-parse HEAD)
echo 'more notes' >"$m/README.txt"
commit "$m" notes
check "a change to nothing ccy builds from passes" "0" "$(judge "$m" "$base" HEAD)"
check "a range with no change passes" "0" "$(judge "$m" HEAD HEAD)"

# The work tree, as qa-changed runs it: no REV.
m=$(repo)
echo 'echo two' >"$m/$CTX/entrypoint.sh"
check "an uncommitted image change without the bump is refused" "1 yes" "$(judge "$m" HEAD) $(has 'CCY-IMAGE-INPUT')"
check "…and the real index is left as it was" "" "$(git -C "$m" diff --cached --name-only)"
echo 'echo one' >"$m/$CTX/entrypoint.sh"
echo 'new' >"$m/$CTX/lib/b.bash"
check "an untracked new ccy library counts in the work tree" "1 yes" "$(judge "$m" HEAD) $(has "$CTX/lib/b.bash")"
rm "$m/$CTX/lib/b.bash"
check "a clean work tree passes" "0" "$(judge "$m" HEAD)"

# What it cannot judge, and its usage.
check "a base that is not a commit cannot be judged" "2" "$(judge "$m" no-such-ref)"
check "a REV that is not a commit cannot be judged" "2" "$(judge "$m" HEAD no-such-ref)"
check "no base is a usage error" "64" "$(judge "$m")"
check "three arguments are a usage error" "64" "$(judge "$m" HEAD HEAD HEAD)"

# qa-changed starts the check by a routing row of path roots; every path the hook counts in this
# checkout must sit under one of them, or a change to it would never be judged.
row=$(awk -F'\t' '$1 == "also" && $3 == "ccy-version-bump" { print $2 }' "$SCRIPT_DIR/qa-routing.tsv")
IFS=, read -r -a roots <<<"$row"
under() {
    local root
    for root in "${roots[@]}"; do
        if [[ "$root" == *'*' && "$1" == "${root%'*'}"* ]] || [ "$1" = "$root" ]; then return 0; fi
    done
    return 1
}
inputs=$(awk -F'"' '/^CCY_(SCRIPT|LIB_DIR|HELPER_DIR)=/ { print $2 "/x" }' "$SCRIPT_DIR/git-hooks/pre-commit")
inputs+=$'\n'"$CTX/Dockerfile"$'\n'"vars/fzf-pin.yml"$'\n'"files/opt/claude-yolo/x"$'\n'"$CTX/x"
while IFS= read -r spec; do
    inputs+=$'\n'"${spec#:(*)}"
done <"$REPO_ROOT/$CTX/image-source-paths"
missed=""
while IFS= read -r path; do
    if [ -n "$path" ] && ! under "$path"; then missed+="$path "; fi
done <<<"$inputs"
check "the routing row names its roots" "yes" "$(if [ "${#roots[@]}" -gt 3 ]; then echo yes; else echo no; fi)"
check "every input the hook counts is under a routing root" "" "$missed"

echo
echo "passed: $passed"
echo "failed: $failed"
[ "$failed" -eq 0 ]
