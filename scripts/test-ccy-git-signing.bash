#!/usr/bin/env bash
# Unit-test how ccy carries commit signing into the container (Plan 00139).
#
# play-git-configure-and-tools.yml signs every commit and tag with an SSH key named in
# user.signingkey, and play-github-cli-multi.yml includes a key per GitHub account for a
# repository whose remote is that account's github.com-<alias> host. ccy copies
# ~/.gitconfig into the container, so the copy names HOST paths the container cannot see.
# stage_git_signing_key (lib/ssh-handling.bash) asks git which key the project gets,
# copies it into the directory that carries the gitconfig copy, which ccy mounts
# read-only, and appends a section to the copy naming it. When signing is on but there is
# no usable key, it refuses the launch: a container that started anyway would fail every
# commit it tried to make.
#
# Sources the library from THIS repo, so a change is verified before the play runs.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the
# full picture, and each result is checked explicitly (as test-ccy-ssh-handling.bash).
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LAUNCHER="${CCY_GIT_SIGNING_LAUNCHER:-$REPO_ROOT/files/var/local/claude-yolo/claude-yolo}"
LIB_DIR="$(dirname "$LAUNCHER")/lib"
PURE_LIB="$LIB_DIR/common-pure.bash"
SSH_LIB="$LIB_DIR/ssh-handling.bash"
# Config set through the environment outranks GIT_CONFIG_GLOBAL, so an inherited one would
# reach the real-key case below as if the staged gitconfig had said it.
unset GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS

for f in "$LAUNCHER" "$PURE_LIB" "$SSH_LIB"; do
    if [ ! -f "$f" ]; then
        echo "FAIL: not found: $f" >&2
        exit 1
    fi
done

# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$PURE_LIB"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/ssh-handling.bash
source "$SSH_LIB"

if ! declare -F stage_git_signing_key >/dev/null; then
    echo "FAIL: stage_git_signing_key is not defined after sourcing $SSH_LIB" >&2
    exit 1
fi

WORK="$(mktemp -d "$REPO_ROOT/untracked/ccy-git-signing-fixtures.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAIL=0
pass() { PASS=$((PASS + 1)); echo "  ✓ $1"; }
fail() { FAIL=$((FAIL + 1)); echo "  ✗ $1" >&2; }

MOUNT=/tmp/claude-config-import

# One case directory: a fake HOME holding a key and a ~/.gitconfig written from the config
# lines given, a project directory, and a stage dir holding a copy of that ~/.gitconfig, as
# the launcher makes it.
new_case() {
    CASE="$WORK/$1"
    mkdir -p "$CASE/home/.ssh" "$CASE/stage" "$CASE/project"
    printf 'PRIVATE-KEY-BYTES-%s\n' "$1" >"$CASE/home/.ssh/signing"
    chmod 600 "$CASE/home/.ssh/signing"
    : >"$CASE/home/.gitconfig"
    shift
    local line
    for line in "$@"; do
        git config --file "$CASE/home/.gitconfig" "${line%%=*}" "${line#*=}"
    done
    cp "$CASE/home/.gitconfig" "$CASE/stage/gitconfig"
}

# git as the launcher's host user sees it: the case's ~/.gitconfig and nothing else.
case_git() {
    HOME="$CASE/home" GIT_CONFIG_GLOBAL="$CASE/home/.gitconfig" GIT_CONFIG_NOSYSTEM=1 "$@"
}

run_stage() {
    OUT="$(case_git stage_git_signing_key "$CASE/stage/gitconfig" "$CASE/stage" "$MOUNT" "$CASE/project" 2>&1)"
    RC=$?
}

# The key git signs with in the container: the copy is its global config there.
signingkey_in_copy() {
    GIT_CONFIG_GLOBAL="$CASE/stage/gitconfig" GIT_CONFIG_NOSYSTEM=1 \
        git -C "$CASE/project" config --get user.signingkey
}

echo "== signing on, SSH key present"
new_case on-present gpg.format=ssh "user.signingkey=$WORK/on-present/home/.ssh/signing" \
    commit.gpgsign=true tag.gpgsign=true
run_stage
if [ "$RC" -eq 0 ]; then pass "accepted"; else fail "refused (rc=$RC): $OUT"; fi
if cmp -s "$CASE/home/.ssh/signing" "$CASE/stage/git-signing-key"; then
    pass "the key is staged byte-for-byte"
else
    fail "the staged key is missing or differs"
fi
mode="$(stat -c '%a' "$CASE/stage/git-signing-key" 2>/dev/null)" || mode="absent"
if [ "$mode" = "600" ]; then pass "the staged key is 0600"; else fail "the staged key is $mode, not 600"; fi
if [ "$(signingkey_in_copy)" = "$MOUNT/git-signing-key" ]; then
    pass "the copy names the mounted key"
else
    fail "the copy names '$(signingkey_in_copy)', not $MOUNT/git-signing-key"
fi
if [ "$(git config --file "$CASE/stage/gitconfig" --type=bool --get commit.gpgsign)" = "true" ]; then
    pass "commit.gpgsign is left on"
else
    fail "commit.gpgsign was changed"
fi

echo "== a repository on a GitHub account's host gets that account's key"
new_case account gpg.format=ssh "user.signingkey=$WORK/account/home/.ssh/signing" commit.gpgsign=true
printf 'ACCOUNT-KEY-BYTES\n' >"$CASE/home/.ssh/github_work_signing"
chmod 600 "$CASE/home/.ssh/github_work_signing"
mkdir -p "$CASE/home/.config/git"
printf '[user]\n\tsigningkey = %s\n' "$CASE/home/.ssh/github_work_signing" >"$CASE/home/.config/git/work.gitconfig"
printf '[includeIf "hasconfig:remote.*.url:git@github.com-work:*/**"]\n\tpath = %s\n' \
    "$CASE/home/.config/git/work.gitconfig" >"$CASE/home/.config/git/accounts.gitconfig"
printf '[include]\n\tpath = %s\n' "$CASE/home/.config/git/accounts.gitconfig" >>"$CASE/home/.gitconfig"
cp "$CASE/home/.gitconfig" "$CASE/stage/gitconfig"
git init -q "$CASE/project"
git -C "$CASE/project" remote add origin git@github.com-work:example/example.git
run_stage
if [ "$RC" -eq 0 ] && cmp -s "$CASE/home/.ssh/github_work_signing" "$CASE/stage/git-signing-key"; then
    pass "the account's key is staged, not the machine's"
else
    fail "the account's key was not staged (rc=$RC): $OUT"
fi
# The include files are readable here, as they would not be in the container; the
# appended section must win over them anyway.
if [ "$(signingkey_in_copy)" = "$MOUNT/git-signing-key" ]; then
    pass "the copy signs with the mounted key, even where the account include resolves"
else
    fail "the copy signs with '$(signingkey_in_copy)', not $MOUNT/git-signing-key"
fi
git -C "$CASE/project" remote set-url origin git@github.com:example/example.git
cp "$CASE/home/.gitconfig" "$CASE/stage/gitconfig"   # a fresh copy, as every launch makes
run_stage
if [ "$RC" -eq 0 ] && cmp -s "$CASE/home/.ssh/signing" "$CASE/stage/git-signing-key"; then
    pass "a repository on plain github.com gets the machine key"
else
    fail "a plain github.com repository did not get the machine key (rc=$RC): $OUT"
fi

echo "== a key named in the project's own git config, which the container can write"
for signing_setting in commit.gpgsign=true user.name=someone; do
    new_case "project-local-${signing_setting%%=*}" gpg.format=ssh \
        "user.signingkey=$WORK/project-local-${signing_setting%%=*}/home/.ssh/signing" "$signing_setting"
    printf 'NOT-A-SIGNING-KEY\n' >"$CASE/home/.ssh/id"
    git init -q "$CASE/project"
    git -C "$CASE/project" config user.signingkey "$CASE/home/.ssh/id"
    run_stage
    if [ "$RC" -ne 0 ] && [ ! -e "$CASE/stage/git-signing-key" ]; then
        pass "refused and nothing staged (${signing_setting%%=*} set)"
    else
        fail "a project-local key was accepted (${signing_setting%%=*} set, rc=$RC)"
    fi
    case "$OUT" in
        *"local git config"*) pass "the refusal names the project's local config (${signing_setting%%=*} set)" ;;
        *) fail "the refusal does not name the local config: $OUT" ;;
    esac
done

echo "== a ~/.gitconfig whose last line has no newline"
new_case no-final-newline gpg.format=ssh "user.signingkey=$WORK/no-final-newline/home/.ssh/signing" \
    commit.gpgsign=true
printf '[commit]\n\tgpgsign = true' >>"$CASE/home/.gitconfig"
cp "$CASE/home/.gitconfig" "$CASE/stage/gitconfig"
run_stage
if [ "$RC" -eq 0 ] &&
    [ "$(git config --file "$CASE/stage/gitconfig" --type=bool --get-all commit.gpgsign | sort -u)" = "true" ] &&
    [ "$(signingkey_in_copy)" = "$MOUNT/git-signing-key" ]; then
    pass "the appended section leaves the last line intact and names the mounted key"
else
    fail "the copy is broken after the append (rc=$RC): $(git config --file "$CASE/stage/gitconfig" --list 2>&1 | tr '\n' ' ')"
fi

echo "== a key path written with ~/"
new_case tilde gpg.format=ssh "user.signingkey=~/.ssh/signing" commit.gpgsign=true
run_stage
if [ "$RC" -eq 0 ] && cmp -s "$CASE/home/.ssh/signing" "$CASE/stage/git-signing-key"; then
    pass "a tilde path is expanded against HOME and the key staged"
else
    fail "a tilde-path key was not staged (rc=$RC): $OUT"
fi

echo "== signing on, key file missing"
new_case on-missing gpg.format=ssh "user.signingkey=$WORK/on-missing/home/.ssh/absent" commit.gpgsign=true
run_stage
if [ "$RC" -ne 0 ]; then pass "refused"; else fail "accepted a missing key"; fi
case "$OUT" in
    *play-git-configure-and-tools.yml*play-github-cli-multi.yml*) pass "the refusal names the plays that generate the keys" ;;
    *) fail "the refusal does not name the play: $OUT" ;;
esac
if [ ! -e "$CASE/stage/git-signing-key" ]; then pass "nothing staged"; else fail "a key was staged"; fi

echo "== tag signing alone on, key file missing"
new_case tag-only gpg.format=ssh "user.signingkey=$WORK/tag-only/home/.ssh/absent" tag.gpgsign=true
run_stage
if [ "$RC" -ne 0 ]; then pass "refused"; else fail "accepted a missing key with tag.gpgsign on"; fi

echo "== signing on, user.signingkey unset"
new_case on-unset gpg.format=ssh commit.gpgsign=true
run_stage
if [ "$RC" -ne 0 ]; then pass "refused"; else fail "accepted signing with no key"; fi

echo "== signing on, OpenPGP format"
new_case on-openpgp "user.signingkey=$WORK/on-openpgp/home/.ssh/signing" commit.gpgsign=true
run_stage
if [ "$RC" -ne 0 ]; then pass "refused: only SSH signing works in the container"; else fail "accepted an OpenPGP signing setup"; fi

echo "== signing on, a literal key:: public key (needs an agent)"
new_case on-literal gpg.format=ssh "user.signingkey=key::ssh-ed25519 AAAAC3Nza" commit.gpgsign=true
run_stage
if [ "$RC" -ne 0 ]; then pass "refused"; else fail "accepted a literal public key with no agent"; fi

echo "== signing off, no key configured"
new_case off-none user.name=someone
before="$(cat "$CASE/stage/gitconfig")"
run_stage
if [ "$RC" -eq 0 ]; then pass "accepted"; else fail "refused (rc=$RC): $OUT"; fi
if [ "$(cat "$CASE/stage/gitconfig")" = "$before" ] && [ ! -e "$CASE/stage/git-signing-key" ]; then
    pass "the copy is untouched and nothing staged"
else
    fail "the copy changed or a key was staged"
fi

echo "== signing off, SSH key configured (git commit -S still works)"
new_case off-present gpg.format=ssh "user.signingkey=$WORK/off-present/home/.ssh/signing"
run_stage
if [ "$RC" -eq 0 ] && [ "$(signingkey_in_copy)" = "$MOUNT/git-signing-key" ]; then
    pass "staged and repointed"
else
    fail "not staged (rc=$RC): $OUT"
fi

echo "== a real key, signing a real commit through the repointed copy"
# The cases above use placeholder key bytes. This one stages a real key with the mount
# point set to the stage directory itself, so the path the copy names exists here as it
# would in the container, and git signs with nothing but that copy for its global config.
new_case real gpg.format=ssh "user.signingkey=$WORK/real/home/.ssh/signing" \
    commit.gpgsign=true tag.gpgsign=true user.name=Signer user.email=signer@example.com
rm -f "$CASE/home/.ssh/signing"
ssh-keygen -q -t ed25519 -N "" -C signer@example.com -f "$CASE/home/.ssh/signing"
OUT="$(case_git stage_git_signing_key "$CASE/stage/gitconfig" "$CASE/stage" "$CASE/stage" "$CASE/project" 2>&1)"
RC=$?
if [ "$RC" -eq 0 ]; then pass "a real key is accepted"; else fail "a real key was refused (rc=$RC): $OUT"; fi
printf 'signer@example.com namespaces="git" %s\n' "$(cut -d' ' -f1,2 "$CASE/home/.ssh/signing.pub")" \
    >"$CASE/allowed_signers"
# The host path is gone, as it is inside the container: only a repointed copy can sign.
rm -f "$CASE/home/.ssh/signing"
repo="$CASE/repo"
git init -q "$repo"
GIT_CONFIG_GLOBAL="$CASE/stage/gitconfig" GIT_CONFIG_NOSYSTEM=1 \
    git -C "$repo" commit -q --allow-empty -m "signed through the copy"
GIT_CONFIG_GLOBAL="$CASE/stage/gitconfig" GIT_CONFIG_NOSYSTEM=1 \
    git -C "$repo" tag -m "signed tag" v1
if GIT_CONFIG_GLOBAL="$CASE/stage/gitconfig" GIT_CONFIG_NOSYSTEM=1 \
    git -C "$repo" -c gpg.ssh.allowedSignersFile="$CASE/allowed_signers" verify-commit HEAD 2>/dev/null; then
    pass "a plain commit is signed, and verifies against the key's public half"
else
    fail "a plain commit through the copy is not signed by the staged key"
fi
if GIT_CONFIG_GLOBAL="$CASE/stage/gitconfig" GIT_CONFIG_NOSYSTEM=1 \
    git -C "$repo" -c gpg.ssh.allowedSignersFile="$CASE/allowed_signers" verify-tag v1 2>/dev/null; then
    pass "a plain annotated tag is signed, and verifies"
else
    fail "a plain annotated tag through the copy is not signed by the staged key"
fi

echo "== the launcher"
# Staged after the EXIT trap is set, so a refusal still removes the directory holding the
# gitconfig copy and any key already in it.
trap_line="$(grep -n '^trap cleanup EXIT' "$LAUNCHER" | cut -d: -f1)"
stage_call="stage_git_signing_key \"\$CONFIG_TEMP/gitconfig\" \"\$CONFIG_TEMP\" /tmp/claude-config-import \"\$PWD\""
stage_line="$(grep -nF "$stage_call" "$LAUNCHER" | cut -d: -f1)"
if [ -n "$stage_line" ]; then pass "stages the project's key into CONFIG_TEMP"; else fail "never calls stage_git_signing_key on CONFIG_TEMP for the project"; fi
if [ -n "$trap_line" ] && [ -n "$stage_line" ] && [ "$stage_line" -gt "$trap_line" ]; then
    pass "after the cleanup trap is set"
else
    fail "not after 'trap cleanup EXIT' (trap line ${trap_line:-none}, stage line ${stage_line:-none})"
fi
# The session's one run site. A key staged after it would reach no container.
run_line="$(grep -n "^container_cmd run \\\$DOCKER_FLAGS --rm" "$LAUNCHER" | cut -d: -f1)"
if [ -n "$run_line" ] && [ -n "$stage_line" ] && [ "$stage_line" -lt "$run_line" ]; then
    pass "before the session's container run"
else
    fail "not before the session's container run (stage line ${stage_line:-none}, run line ${run_line:-none})"
fi
if grep -qF -- "-v \"\$CONFIG_TEMP:/tmp/claude-config-import:ro" "$LAUNCHER"; then
    pass "CONFIG_TEMP is mounted read-only at /tmp/claude-config-import"
else
    fail "the CONFIG_TEMP mount is no longer read-only at /tmp/claude-config-import"
fi

echo ""
echo "RESULT: passed: $PASS failed: $FAIL"
[ "$FAIL" -eq 0 ]
