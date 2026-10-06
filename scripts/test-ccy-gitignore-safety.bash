#!/usr/bin/env bash
# Unit-test check_ccy_gitignore_safety (files/var/local/claude-yolo/lib/common.bash).
#
# WHY THIS TEST EXISTS. .claude/ccy/ holds session data and tokens, so its generated
# .gitignore ignores everything and lets through only the files a project may track. The
# tracked ccy.env.local.dist must be let through, the untracked ccy.env.local beside it must
# stay ignored, and a project whose .gitignore predates the dist file must be given its
# exception. Runs the real function in a throwaway repository.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="$SCRIPT_DIR/../files/var/local/claude-yolo/lib/common.bash"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# common.bash checks for its container engine when sourced; a stub satisfies it.
mkdir -p "$work/bin"
printf '#!/usr/bin/env bash\nexit 0\n' >"$work/bin/podman"
chmod 755 "$work/bin/podman"
export PATH="$work/bin:$PATH" CCY_CONTAINER_ENGINE=podman

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
        echo "        check output: $(cat "$work/check.log")"
    fi
}

# new_repo <dir>: a git repository with an empty .claude/ccy/.
new_repo() {
    mkdir -p "$1/.claude/ccy"
    git -C "$1" init -q
    git -C "$1" config user.name test
    git -C "$1" config user.email test@example.com
}

# run_check <dir>: run the real check there; prints its exit code. Its own output goes
# to check.log, which a failing case prints.
run_check() {
    (cd "$1" && bash -c '. "$1"; check_ccy_gitignore_safety' _ "$LIB" >"$work/check.log" 2>&1)
    echo "$?"
}

# ignored <dir> <file>: "ignored" or "tracked-ok", as git sees the path.
ignored() {
    if git -C "$1" check-ignore -q ".claude/ccy/$2"; then echo ignored; else echo tracked-ok; fi
}

fresh="$work/fresh"
new_repo "$fresh"
check "fresh project: the check passes" "0" "$(run_check "$fresh")"
check "fresh project: ccy.env.local.dist may be tracked" "tracked-ok" "$(ignored "$fresh" ccy.env.local.dist)"
check "fresh project: ccy.env.local stays ignored" "ignored" "$(ignored "$fresh" ccy.env.local)"
check "fresh project: ccy.env may be tracked" "tracked-ok" "$(ignored "$fresh" ccy.env)"
check "fresh project: session data stays ignored" "ignored" "$(ignored "$fresh" history.jsonl)"

old="$work/old"
new_repo "$old"
printf '%s\n' '*' '!.gitignore' '!Dockerfile' '!ccy.env' '!mounts' >"$old/.claude/ccy/.gitignore"
check "older .gitignore: the check passes" "0" "$(run_check "$old")"
check "older .gitignore: given the dist exception" "tracked-ok" "$(ignored "$old" ccy.env.local.dist)"
check "older .gitignore: ccy.env.local still ignored" "ignored" "$(ignored "$old" ccy.env.local)"

tracked="$work/tracked"
new_repo "$tracked"
run_check "$tracked" >"$work/first-run.code"
printf '# placeholder\n' >"$tracked/.claude/ccy/ccy.env.local.dist"
git -C "$tracked" add -f .claude/ccy/.gitignore .claude/ccy/ccy.env.local.dist
git -C "$tracked" commit -q -m dist
check "a tracked ccy.env.local.dist is not flagged" "0" "$(run_check "$tracked")"

printf 'export X=1\n' >"$tracked/.claude/ccy/ccy.env.local"
git -C "$tracked" add -f .claude/ccy/ccy.env.local
git -C "$tracked" commit -q -m local
check "a tracked ccy.env.local is flagged" "1" "$(run_check "$tracked")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
