#!/usr/bin/env bash
# Drive the real scripts/release.bash against a signed git fixture (Plan 00153 Task 2.1).
#
# WHY THIS EXISTS. A release is a signed tag the unattended self-update will deploy, so the
# command that makes one must refuse everything that would put a wrong or unreleasable point
# behind that tag: a dirty tree, a branch that is not F<major>, a branch behind its remote, a
# commit CI has not passed, and a version number that does not follow from the last tag. The
# command works in two steps because the release branch is protected: `prepare` writes the
# changelog entry as a signed commit on a release branch and opens a pull request; `tag`,
# after that PR is merged, signs the tag on the release commit (not on GitHub's own merge
# commit) and publishes it.
#
# The fixture is a real repository with a bare origin and commits signed by a throwaway SSH
# key; a copy of release.bash runs inside it, so its repository root is the fixture's. Only
# `gh` is stubbed: it records what it was asked and answers the CI question from
# RELEASE_TEST_CI (success, failure, pending or none).
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

# Every git call here sees only the fixture's own config: a machine whose global config signs
# every commit would sign the "unsigned" fixtures too, and the refusal cases would test nothing.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
unset GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOOL="$SCRIPT_DIR/release.bash"
PRINCIPAL="owner@example.com"

if [ ! -x "$TOOL" ]; then
    echo "FAIL: $TOOL is missing or not executable" >&2
    exit 1
fi
for tool in git ssh-keygen; do
    if ! command -v "$tool" >/dev/null; then
        echo "FAIL: $tool is not on PATH; this test needs it" >&2
        exit 1
    fi
done

passed=0
failed=0
check() {
    local name="$1" ok="$2" detail="${3:-}"
    if [ "$ok" = yes ]; then
        passed=$((passed + 1))
        echo "  PASS: $name"
    else
        failed=$((failed + 1))
        echo "  FAIL: $name${detail:+ ($detail)}"
    fi
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

ssh-keygen -q -t ed25519 -N '' -C "$PRINCIPAL" -f "$work/key"
printf '%s %s\n' "$PRINCIPAL" "$(cut -d' ' -f1,2 "$work/key.pub")" >"$work/signers"

# A stub gh: records every call, answers the CI question from RELEASE_TEST_CI.
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$RELEASE_TEST_GH_LOG"
case "$1 $2" in
    "run list")
        case "${RELEASE_TEST_CI:-success}" in
            success) echo '[{"status":"completed","conclusion":"success"}]' ;;
            failure) echo '[{"status":"completed","conclusion":"failure"}]' ;;
            pending) echo '[{"status":"in_progress","conclusion":""}]' ;;
            none) echo '[]' ;;
        esac ;;
    "pr create") echo "https://example.invalid/pull/1" ;;
    "release create") ;;
    *) echo "stub gh: unexpected $*" >&2; exit 99 ;;
esac
STUB
chmod 755 "$work/bin/gh"
export RELEASE_TEST_GH_LOG="$work/gh.log"

# ok CMD...: true when the command succeeds; its output is kept in last.out, not discarded.
ok() {
    "$@" >"$work/last.out" 2>&1
}

# fixture NAME: a fresh clone on branch F44 with a signed first commit, origin bare.
fixture() {
    local dir="$work/$1"
    git init -q --bare "$dir.git"
    git init -q -b F44 "$dir"
    mkdir -p "$dir/scripts"
    cp "$TOOL" "$dir/scripts/release.bash"
    git -C "$dir" config user.name Owner
    git -C "$dir" config user.email "$PRINCIPAL"
    git -C "$dir" config gpg.format ssh
    git -C "$dir" config user.signingkey "$work/key.pub"
    git -C "$dir" config gpg.ssh.allowedSignersFile "$work/signers"
    git -C "$dir" config commit.gpgsign true
    git -C "$dir" config tag.gpgsign true
    git -C "$dir" remote add origin "$dir.git"
    echo base >"$dir/file"
    git -C "$dir" add -A
    git -C "$dir" commit -q -m "base"
    git -C "$dir" push -q origin F44
    : >"$RELEASE_TEST_GH_LOG"
    echo "$dir"
}

# commit DIR SUBJECT: one more signed commit, pushed.
commit() {
    echo "$2" >>"$1/file"
    git -C "$1" commit -q -am "$2"
    git -C "$1" push -q origin "$(git -C "$1" branch --show-current)"
}

# run DIR ARGS...: run the fixture's release.bash; sets rc and out.
run() {
    local dir="$1"
    shift
    out="$(cd "$dir" && PATH="$work/bin:$PATH" bash scripts/release.bash "$@" 2>&1)"
    rc=$?
}

# merge_release DIR VERSION: merge the release branch the way GitHub would (a merge commit the
# owner did not sign) and push, leaving F44 ready for `tag`.
merge_release() {
    local dir="$1" version="$2"
    git -C "$dir" switch -q F44
    git -C "$dir" -c commit.gpgsign=false merge -q --no-ff -m "Merge pull request" "release-$version"
    git -C "$dir" push -q origin F44
}

# seed_tag DIR VERSION: a release tag left by an earlier run.
seed_tag() {
    git -C "$1" tag -s -m "Release $2" "$2"
    git -C "$1" push -q origin "$2"
}

# expect NAME RC [TEXT]: pass when the last run exited RC and its output contains TEXT.
expect() {
    local name="$1" want="$2" text="${3:-}"
    if [ "$rc" -eq "$want" ] && [[ "$out" == *"$text"* ]]; then
        check "$name" yes
    else
        check "$name" no "rc=$rc, wanted $want${text:+ with \"$text\"}: $out"
    fi
}

# assert NAME CMD...: pass when the command succeeds; otherwise its output is the detail.
assert() {
    local name="$1"
    shift
    if "$@" >"$work/last.out" 2>&1; then
        check "$name" yes
    else
        check "$name" no "$(cat "$work/last.out")"
    fi
}

# same NAME A B: pass when the two strings are equal.
same() {
    if [ "$2" = "$3" ]; then check "$1" yes; else check "$1" no "'$2' != '$3'"; fi
}

# differs NAME A B: pass when the two strings differ.
differs() {
    if [ "$2" != "$3" ]; then check "$1" yes; else check "$1" no "both '$2'"; fi
}

# has NAME HAYSTACK NEEDLE / lacks NAME HAYSTACK NEEDLE: substring checks.
has() {
    if [[ "$2" == *"$3"* ]]; then check "$1" yes; else check "$1" no "no '$3' in: $2"; fi
}
lacks() {
    if [[ "$2" != *"$3"* ]]; then check "$1" yes; else check "$1" no "found '$3' in: $2"; fi
}

echo "== usage"
dir="$(fixture usage)"
run "$dir"
expect "no argument is a usage error" 64
run "$dir" bogus
expect "an unknown step is a usage error" 64
run "$dir" prepare
expect "prepare without a bump is a usage error" 64
run "$dir" prepare major
expect "major is not a bump (it follows the Fedora release)" 64
run "$dir" --help
expect "--help exits 0" 0

echo "== refusals before anything is written"
dir="$(fixture dirty)"
echo more >>"$dir/file"
run "$dir" prepare first --yes
expect "a dirty tree is refused" 1 "not clean"

dir="$(fixture branch)"
git -C "$dir" switch -q -c feature
run "$dir" prepare first --yes
expect "a branch that is not F<major> is refused" 1 "F<major>"

dir="$(fixture behind)"
git clone -q -b F44 "$dir.git" "$work/behind-other"
git -C "$work/behind-other" config user.name Other
git -C "$work/behind-other" config user.email other@example.com
echo x >>"$work/behind-other/file"
git -C "$work/behind-other" -c commit.gpgsign=false commit -q -am other
git -C "$work/behind-other" push -q origin F44
run "$dir" prepare first --yes
expect "a branch behind its remote is refused" 1 "behind"

dir="$(fixture ci-red)"
RELEASE_TEST_CI=failure run "$dir" prepare first --yes
expect "a commit whose CI failed is refused" 1 "CI"
RELEASE_TEST_CI=none run "$dir" prepare first --yes
expect "a commit with no CI run is refused" 1 "CI"
RELEASE_TEST_CI=pending run "$dir" prepare first --yes
expect "a commit whose CI is still running is refused" 1 "CI"

echo "== the version follows from the last tag"
dir="$(fixture version)"
run "$dir" prepare minor --yes
expect "minor with no release yet is refused (say first)" 1 "first"
seed_tag "$dir" 44.0.0
run "$dir" prepare first --yes
expect "first when a release exists is refused" 1 "already"
seed_tag "$dir" 44.9.0
seed_tag "$dir" 44.10.0
git -C "$dir" tag 44.11.0-rc1
commit "$dir" "a change"
run "$dir" prepare patch --yes --dry-run
expect "patch is numeric (44.10.0 beats 44.9.0; a -rc tag is ignored)" 0 "44.10.1"
run "$dir" prepare minor --yes --dry-run
expect "minor resets the patch" 0 "44.11.0"
if [ -z "$(git -C "$dir" branch --list 'release-*')" ] && [ -z "$(git -C "$dir" status --porcelain)" ]; then
    check "--dry-run creates no branch and changes nothing" yes
else
    check "--dry-run creates no branch and changes nothing" no
fi
if grep -q "pr create" "$RELEASE_TEST_GH_LOG"; then
    check "--dry-run opens no pull request" no
else
    check "--dry-run opens no pull request" yes
fi

echo "== prepare"
dir="$(fixture prepare)"
commit "$dir" "feature one"
commit "$dir" "fix two"
run "$dir" prepare first --yes
expect "prepare first succeeds" 0 "44.0.0"
same "it works on branch release-44.0.0" "$(git -C "$dir" branch --show-current)" "release-44.0.0"
assert "the release branch is pushed" git --git-dir="$dir.git" rev-parse -q --verify refs/heads/release-44.0.0
assert "the release commit is signed by the owner" git -C "$dir" verify-commit HEAD
same "its subject is 'Release 44.0.0'" "$(git -C "$dir" log -1 --format=%s)" "Release 44.0.0"
assert "CHANGELOG.md has the 44.0.0 entry" grep -q '^## 44.0.0' "$dir/CHANGELOG.md"
assert "a pull request is opened" grep -q "pr create" "$RELEASE_TEST_GH_LOG"
same "prepare creates no tag" "$(git -C "$dir" tag --list)" ""

echo "== the changelog lists what changed since the last tag"
dir="$(fixture changelog)"
seed_tag "$dir" 44.0.0
commit "$dir" "feature one"
commit "$dir" "fix two"
run "$dir" prepare minor --yes
entry="$(awk '/^## 44.1.0/{f=1;next} /^## /{f=0} f' "$dir/CHANGELOG.md")"
has "the first commit since the tag is listed" "$entry" "feature one"
has "the second commit since the tag is listed" "$entry" "fix two"
lacks "commits before the tag are not" "$entry" "base"

echo "== tag"
dir="$(fixture tag)"
commit "$dir" "feature one"
run "$dir" prepare first --yes
merge_release "$dir" 44.0.0
: >"$RELEASE_TEST_GH_LOG"
run "$dir" tag 44.0.0 --yes
expect "tag succeeds after the merge" 0
same "the tag is annotated" "$(git -C "$dir" cat-file -t 44.0.0)" "tag"
assert "the tag is signed" git -C "$dir" verify-tag 44.0.0
release_commit="$(git -C "$dir" log -1 --format=%H --grep='^Release 44.0.0$' F44)"
tagged_commit="$(git -C "$dir" rev-parse '44.0.0^{commit}')"
same "it points at the release commit" "$tagged_commit" "$release_commit"
differs "not at the merge commit on top of it" "$tagged_commit" "$(git -C "$dir" rev-parse F44)"
assert "the tag is pushed" git --git-dir="$dir.git" rev-parse -q --verify refs/tags/44.0.0
assert "a GitHub Release is created from it" grep -q "release create 44.0.0" "$RELEASE_TEST_GH_LOG"
run "$dir" tag 44.0.0 --yes
expect "tagging twice is refused" 1 "already exists"

dir="$(fixture tag-missing)"
run "$dir" tag 44.0.0 --yes
expect "tag with no release commit is refused" 1 "no commit"

dir="$(fixture tag-unsigned)"
git -C "$dir" -c commit.gpgsign=false commit -q --allow-empty -m "Release 44.0.0"
git -C "$dir" push -q origin F44
run "$dir" tag 44.0.0 --yes
if [ "$rc" -eq 1 ] && [ -z "$(git -C "$dir" tag --list)" ]; then
    check "an unsigned release commit is not tagged" yes
else
    check "an unsigned release commit is not tagged" no "rc=$rc $out"
fi

dir="$(fixture tag-red)"
run "$dir" prepare first --yes
merge_release "$dir" 44.0.0
RELEASE_TEST_CI=failure run "$dir" tag 44.0.0 --yes
if [ "$rc" -eq 1 ] && [ -z "$(git -C "$dir" tag --list)" ]; then
    check "a release commit whose CI failed is not tagged" yes
else
    check "a release commit whose CI failed is not tagged" no "rc=$rc $out"
fi

dir="$(fixture tag-branch)"
run "$dir" prepare first --yes
merge_release "$dir" 44.0.0
git -C "$dir" switch -q -c feature
run "$dir" tag 44.0.0 --yes
expect "tag from a branch that is not F<major> is refused" 1 "F<major>"

dir="$(fixture tag-version)"
run "$dir" tag banana
expect "tag with a malformed version is a usage error" 64
run "$dir" tag 45.0.0
expect "tag for another Fedora's major is refused" 1 "F44"

echo
echo "passed: $passed  failed: $failed"
[ "$failed" -eq 0 ]
