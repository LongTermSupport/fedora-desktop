#!/usr/bin/env bash
# Unit-test how ccy carries commit signing into the container (Plan 00139 D5).
#
# The host signs every commit and tag with a login key, through the ssh-agent that holds it
# unlocked: ~/.ssh/id by default, and github_<alias> in a repository whose remote is that
# account's github.com-<alias> host. ccy copies ~/.gitconfig into the container, so the copy
# names HOST paths the container cannot see. configure_git_signing (lib/ssh-handling.bash)
# appends a section naming a key the container can sign with through an agent:
#   - a key-file identity: the mounted key, which the container's own agent holds, or the
#     forwarded agent must hold;
#   - a forwarded agent only: the public half of the key git on the host picks for the
#     project, which the agent must hold.
# No private key is copied in for signing. Signing that is on with no usable key refuses
# the launch: a container that started anyway would fail every commit it tried to make.
#
# Sources the library from THIS repo, so a change is verified before the play runs. A stub
# ssh-add stands in for the forwarded agent; the real-key case starts an agent of its own.
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

if ! declare -F configure_git_signing >/dev/null; then
    echo "FAIL: configure_git_signing is not defined after sourcing $SSH_LIB" >&2
    exit 1
fi

WORK="$(mktemp -d "$REPO_ROOT/untracked/ccy-git-signing-fixtures.XXXXXX")"
REAL_AGENT_PID=""
trap '[ -n "$REAL_AGENT_PID" ] && kill "$REAL_AGENT_PID"; rm -rf "$WORK"' EXIT

PASS=0
FAIL=0
pass() { PASS=$((PASS + 1)); echo "  ✓ $1"; }
fail() { FAIL=$((FAIL + 1)); echo "  ✗ $1" >&2; }

IN_CONTAINER=/root/.ssh/key_0
ED_A="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAA"
ED_B="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBBBB"

# A stub ssh-add: `ssh-add -L` prints STUB_AGENT_KEYS and exits STUB_AGENT_RC.
mkdir -p "$WORK/bin"
cat >"$WORK/bin/ssh-add" <<'STUB'
#!/usr/bin/env bash
printf '%s' "${STUB_AGENT_KEYS:-}"
exit "${STUB_AGENT_RC:-0}"
STUB
chmod 700 "$WORK/bin/ssh-add"

# One case directory: a fake HOME holding a login key and a ~/.gitconfig written from the
# config lines given, a project directory, and a stage dir holding a copy of that
# ~/.gitconfig, as the launcher makes it.
new_case() {
    CASE="$WORK/$1"
    mkdir -p "$CASE/home/.ssh" "$CASE/stage" "$CASE/project"
    printf 'PRIVATE-KEY-BYTES-%s\n' "$1" >"$CASE/home/.ssh/id"
    printf '%s %s\n' "$ED_A" "$1" >"$CASE/home/.ssh/id.pub"
    chmod 600 "$CASE/home/.ssh/id"
    : >"$CASE/home/.gitconfig"
    shift
    local line
    for line in "$@"; do
        git config --file "$CASE/home/.gitconfig" "${line%%=*}" "${line#*=}"
    done
    cp "$CASE/home/.gitconfig" "$CASE/stage/gitconfig"
}

# git as the launcher's host user sees it: the case's ~/.gitconfig and nothing else, and
# the stub agent on PATH.
case_git() {
    HOME="$CASE/home" GIT_CONFIG_GLOBAL="$CASE/home/.gitconfig" GIT_CONFIG_NOSYSTEM=1 \
        PATH="$WORK/bin:$PATH" "$@"
}

# run_cfg <primary> <forwarded> [project]: the launcher's call, for a key-file primary
# (mounted at IN_CONTAINER) or the agent sentinel.
run_cfg() {
    local in_container=""
    [ -n "$1" ] && [ "$1" != "$SSH_AGENT_SENTINEL" ] && in_container="$IN_CONTAINER"
    OUT="$(case_git configure_git_signing "$CASE/stage/gitconfig" "${3:-$CASE/project}" \
        "$1" "$in_container" "$2" 2>&1)"
    RC=$?
}

# The key the copy names, the container's global config: its last user.signingkey.
signingkey_in_copy() {
    git config --file "$CASE/stage/gitconfig" --get user.signingkey
}

staged_nothing() {
    [ "$(find "$CASE/stage" -mindepth 1 ! -name gitconfig | wc -l)" -eq 0 ]
}

# The host's own value, written as the play writes it, with a literal ~.
ID_LITERAL="$(printf '\176/.ssh/id')"
SIGNING_ON=(gpg.format=ssh "user.signingkey=$ID_LITERAL" commit.gpgsign=true tag.gpgsign=true)

echo "== a key-file identity: the container signs with it, through its own agent"
new_case key-file "${SIGNING_ON[@]}"
run_cfg "$CASE/home/.ssh/github_work" 0
if [ "$RC" -eq 0 ]; then pass "accepted"; else fail "refused (rc=$RC): $OUT"; fi
if [ "$(signingkey_in_copy)" = "$IN_CONTAINER" ]; then
    pass "the copy names the mounted identity"
else
    fail "the copy names '$(signingkey_in_copy)', not $IN_CONTAINER"
fi
if staged_nothing; then pass "no key file is staged beside the copy"; else fail "a file was staged: $(ls "$CASE/stage")"; fi
case "$OUT" in
    *"Commit signing: github_work, the session's SSH identity"*) pass "the launch names the key" ;;
    *) fail "the launch does not name the key: $OUT" ;;
esac
if [ "$(git config --file "$CASE/stage/gitconfig" --type=bool --get commit.gpgsign)" = "true" ]; then
    pass "commit.gpgsign is left on"
else
    fail "commit.gpgsign was changed"
fi

echo "== a key-file identity ignores a key the project's own config names"
new_case key-file-planted "${SIGNING_ON[@]}"
git init -q "$CASE/project"
git -C "$CASE/project" config user.signingkey "$CASE/home/.ssh/id"
run_cfg "$CASE/home/.ssh/github_work" 0
if [ "$RC" -eq 0 ] && [ "$(signingkey_in_copy)" = "$IN_CONTAINER" ]; then
    pass "the session's identity signs, whatever the project names"
else
    fail "rc=$RC, copy names '$(signingkey_in_copy)': $OUT"
fi

echo "== a key-file identity with a forwarded agent: the agent must hold it"
new_case key-file-forwarded "${SIGNING_ON[@]}"
printf '%s work\n' "$ED_B" >"$CASE/home/.ssh/github_work.pub"
STUB_AGENT_KEYS="$ED_B someone
" run_cfg "$CASE/home/.ssh/github_work" 1
if [ "$RC" -eq 0 ] && [ "$(signingkey_in_copy)" = "$IN_CONTAINER" ]; then
    pass "accepted when the agent holds it"
else
    fail "rc=$RC: $OUT"
fi
new_case key-file-forwarded-missing "${SIGNING_ON[@]}"
printf '%s work\n' "$ED_B" >"$CASE/home/.ssh/github_work.pub"
STUB_AGENT_KEYS="$ED_A other
" run_cfg "$CASE/home/.ssh/github_work" 1
if [ "$RC" -ne 0 ] && staged_nothing; then pass "refused when the agent does not"; else fail "accepted (rc=$RC)"; fi
case "$OUT" in
    *"ssh-add $CASE/home/.ssh/github_work"*) pass "the refusal says how to load it" ;;
    *) fail "the refusal has no ssh-add remedy: $OUT" ;;
esac

echo "== a forwarded agent only: the key git picks for the project, as its public half"
new_case agent-only "${SIGNING_ON[@]}"
printf '%s work\n' "$ED_B" >"$CASE/home/.ssh/github_work.pub"
mkdir -p "$CASE/home/.config/git"
printf '[user]\n\tsigningkey = %s\n' "$CASE/home/.ssh/github_work" >"$CASE/home/.config/git/work.gitconfig"
printf '[includeIf "hasconfig:remote.*.url:git@github.com-work:*/**"]\n\tpath = %s\n' \
    "$CASE/home/.config/git/work.gitconfig" >>"$CASE/home/.gitconfig"
cp "$CASE/home/.gitconfig" "$CASE/stage/gitconfig"
git init -q "$CASE/project"
git -C "$CASE/project" remote add origin git@github.com-work:example/example.git
STUB_AGENT_KEYS="$ED_A machine
$ED_B work
" run_cfg "$SSH_AGENT_SENTINEL" 1
if [ "$RC" -eq 0 ] && [ "$(signingkey_in_copy)" = "key::$ED_B" ]; then
    pass "the account's key, as a key:: literal the agent can sign with"
else
    fail "rc=$RC, copy names '$(signingkey_in_copy)': $OUT"
fi
case "$OUT" in
    *"Commit signing: github_work, the key git picks for this project"*) pass "the launch names it" ;;
    *) fail "the launch does not name it: $OUT" ;;
esac
git -C "$CASE/project" remote set-url origin git@github.com:example/example.git
cp "$CASE/home/.gitconfig" "$CASE/stage/gitconfig"
STUB_AGENT_KEYS="$ED_A machine
" run_cfg "$SSH_AGENT_SENTINEL" 1
if [ "$RC" -eq 0 ] && [ "$(signingkey_in_copy)" = "key::$ED_A" ]; then
    pass "a plain github.com repository gets the machine key"
else
    fail "rc=$RC, copy names '$(signingkey_in_copy)': $OUT"
fi

echo "== a forwarded agent only, and it does not hold the key"
new_case agent-missing "${SIGNING_ON[@]}"
STUB_AGENT_KEYS="$ED_B other
" run_cfg "$SSH_AGENT_SENTINEL" 1
if [ "$RC" -ne 0 ]; then pass "refused"; else fail "accepted a key the agent does not hold"; fi
case "$OUT" in
    *"does not hold id"*"ssh-add $CASE/home/.ssh/id"*) pass "the refusal names the key and how to load it" ;;
    *) fail "the refusal is unclear: $OUT" ;;
esac

echo "== a forwarded agent that cannot be read"
new_case agent-unreadable "${SIGNING_ON[@]}"
STUB_AGENT_RC=2 STUB_AGENT_KEYS="Could not open a connection to your authentication agent." \
    run_cfg "$SSH_AGENT_SENTINEL" 1
if [ "$RC" -ne 0 ]; then pass "refused"; else fail "accepted an agent that cannot be read"; fi
case "$OUT" in
    *"cannot be read"*) pass "the refusal says the agent cannot be read" ;;
    *) fail "the refusal is unclear: $OUT" ;;
esac

echo "== a forwarded agent only, and the key has no public half"
new_case agent-no-pub "${SIGNING_ON[@]}"
rm -f "$CASE/home/.ssh/id.pub"
STUB_AGENT_KEYS="$ED_A machine
" run_cfg "$SSH_AGENT_SENTINEL" 1
if [ "$RC" -ne 0 ]; then pass "refused"; else fail "accepted a key with no .pub"; fi

echo "== a forwarded agent only, and the host names a key:: literal"
new_case agent-literal gpg.format=ssh "user.signingkey=key::$ED_B" commit.gpgsign=true
STUB_AGENT_KEYS="$ED_B held
" run_cfg "$SSH_AGENT_SENTINEL" 1
if [ "$RC" -eq 0 ] && [ "$(signingkey_in_copy)" = "key::$ED_B" ]; then
    pass "the literal is passed on"
else
    fail "rc=$RC, copy names '$(signingkey_in_copy)': $OUT"
fi

echo "== a forwarded agent only: a key the project's own config names, which the container can write"
# refused_by <label> <text the refusal must carry>: expect a refusal that leaves the copy
# naming no key and names where the setting lives.
refused_by() {
    if [ "$RC" -ne 0 ] && [ "$(signingkey_in_copy)" = "$ID_LITERAL" ]; then
        pass "$1: refused, and the copy is not repointed"
    else
        fail "$1: accepted (rc=$RC)"
    fi
    case "$OUT" in
        *"$2"*) pass "$1: the refusal names $2" ;;
        *) fail "$1: the refusal does not name $2: $OUT" ;;
    esac
}
# remedy_clears <label> <config file>: run the command the refusal printed, as the user
# would paste it, and expect it to remove the key from that file. Matching the text alone
# passed a remedy that pointed at the wrong file.
remedy_clears() {
    local remedy remedy_out
    remedy="$(printf '%s\n' "$OUT" | awk '/^    git config --file /')"
    if [ -z "$remedy" ]; then
        fail "$1: the refusal prints no git config --file remedy: $OUT"
        return
    fi
    if ! remedy_out="$(case_git bash -c "$remedy" 2>&1)"; then
        fail "$1: the printed remedy failed: $remedy: $remedy_out"
        return
    fi
    if git config --file "$2" --get user.signingkey >/dev/null; then
        fail "$1: the printed remedy left the key in $2: $remedy"
    else
        pass "$1: the printed remedy removes the key"
    fi
}
AGENT_HOLDS_ID="$ED_A machine
"
for signing_setting in commit.gpgsign=true tag.gpgsign=true; do
    new_case "local-${signing_setting%%.*}" gpg.format=ssh "user.signingkey=$ID_LITERAL" "$signing_setting"
    git init -q "$CASE/project"
    git -C "$CASE/project" config user.signingkey "$CASE/home/.ssh/id"
    STUB_AGENT_KEYS="$AGENT_HOLDS_ID" run_cfg "$SSH_AGENT_SENTINEL" 1
    refused_by "the project's local config (${signing_setting%%=*})" "local git config"
done
remedy_clears "the project's local config" "$CASE/project/.git/config"

new_case via-include "${SIGNING_ON[@]}"
git init -q "$CASE/project"
printf '[user]\n\tsigningkey = %s\n' "$CASE/home/.ssh/id" >"$CASE/project/planted.gitconfig"
git -C "$CASE/project" config include.path "$CASE/project/planted.gitconfig"
STUB_AGENT_KEYS="$AGENT_HOLDS_ID" run_cfg "$SSH_AGENT_SENTINEL" 1
refused_by "an [include] in .git/config" "planted.gitconfig"
remedy_clears "an [include] in .git/config" "$CASE/project/planted.gitconfig"

# git names the repository's own config relative to its top level, not to the directory
# ccy was started in.
new_case in-subdir "${SIGNING_ON[@]}"
git init -q "$CASE/repo"
mkdir -p "$CASE/repo/sub dir"
git -C "$CASE/repo" config user.signingkey "$CASE/home/.ssh/id"
STUB_AGENT_KEYS="$AGENT_HOLDS_ID" run_cfg "$SSH_AGENT_SENTINEL" 1 "$CASE/repo/sub dir"
refused_by "a project in a subdirectory" "local git config"
remedy_clears "a project in a subdirectory" "$CASE/repo/.git/config"

# With no work tree (a bare repository, or ccy started inside .git) git names the config
# relative to the git directory instead.
new_case bare "${SIGNING_ON[@]}"
git init -q --bare "$CASE/repo.git"
git -C "$CASE/repo.git" config user.signingkey "$CASE/home/.ssh/id"
STUB_AGENT_KEYS="$AGENT_HOLDS_ID" run_cfg "$SSH_AGENT_SENTINEL" 1 "$CASE/repo.git"
refused_by "a bare repository" "local git config"
remedy_clears "a bare repository" "$CASE/repo.git/config"

# git quotes a path like this one in its plain output, so the remedy must not take it
# from there.
new_case odd-path "${SIGNING_ON[@]}"
ODD="$CASE/project/café it's"
mkdir -p "$ODD"
printf '[user]\n\tsigningkey = %s\n' "$CASE/home/.ssh/id" >"$ODD/planted.gitconfig"
git init -q "$CASE/project"
git -C "$CASE/project" config include.path "$ODD/planted.gitconfig"
STUB_AGENT_KEYS="$AGENT_HOLDS_ID" run_cfg "$SSH_AGENT_SENTINEL" 1
refused_by "an include path git would quote" "planted.gitconfig"
remedy_clears "an include path git would quote" "$ODD/planted.gitconfig"

new_case via-worktree "${SIGNING_ON[@]}"
git init -q "$CASE/project"
git -C "$CASE/project" config extensions.worktreeConfig true
git -C "$CASE/project" config --worktree user.signingkey "$CASE/home/.ssh/id"
STUB_AGENT_KEYS="$AGENT_HOLDS_ID" run_cfg "$SSH_AGENT_SENTINEL" 1
refused_by "a worktree config" "config.worktree"

new_case via-env "${SIGNING_ON[@]}"
OUT="$(GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=user.signingkey GIT_CONFIG_VALUE_0="$CASE/home/.ssh/id" \
    STUB_AGENT_KEYS="$AGENT_HOLDS_ID" case_git configure_git_signing "$CASE/stage/gitconfig" \
    "$CASE/project" "$SSH_AGENT_SENTINEL" "" 1 2>&1)"
RC=$?
refused_by "GIT_CONFIG_COUNT in the environment" "GIT_CONFIG_COUNT"

echo "== signing on, no SSH identity at all"
new_case no-identity "${SIGNING_ON[@]}"
run_cfg "" 0
if [ "$RC" -ne 0 ]; then pass "refused"; else fail "accepted signing with nothing to sign with"; fi
new_case tag-only-no-identity gpg.format=ssh "user.signingkey=$ID_LITERAL" tag.gpgsign=true
run_cfg "" 0
if [ "$RC" -ne 0 ]; then pass "refused with only tag.gpgsign on"; else fail "accepted with tag.gpgsign on"; fi

echo "== signing on, OpenPGP format"
new_case on-openpgp "user.signingkey=$ID_LITERAL" commit.gpgsign=true
run_cfg "$CASE/home/.ssh/github_work" 0
if [ "$RC" -ne 0 ]; then pass "refused: only SSH signing works in the container"; else fail "accepted an OpenPGP signing setup"; fi

echo "== signing off"
new_case off gpg.format=ssh "user.signingkey=$ID_LITERAL" user.name=someone
before="$(cat "$CASE/stage/gitconfig")"
run_cfg "" 0
if [ "$RC" -eq 0 ] && [ "$(cat "$CASE/stage/gitconfig")" = "$before" ] && staged_nothing; then
    pass "accepted, the copy untouched, nothing staged"
else
    fail "rc=$RC, or the copy changed: $OUT"
fi

echo "== a ~/.gitconfig whose last line has no newline"
new_case no-final-newline "${SIGNING_ON[@]}"
printf '[commit]\n\tgpgsign = true' >>"$CASE/home/.gitconfig"
cp "$CASE/home/.gitconfig" "$CASE/stage/gitconfig"
run_cfg "$CASE/home/.ssh/github_work" 0
if [ "$RC" -eq 0 ] &&
    [ "$(git config --file "$CASE/stage/gitconfig" --type=bool --get-all commit.gpgsign | sort -u)" = "true" ] &&
    [ "$(signingkey_in_copy)" = "$IN_CONTAINER" ]; then
    pass "the appended section leaves the last line intact and names the identity"
else
    fail "the copy is broken after the append (rc=$RC): $(git config --file "$CASE/stage/gitconfig" --list 2>&1 | tr '\n' ' ')"
fi

echo "== a real passphrase-protected key, signing a real commit and tag through an agent"
# The cases above use placeholder keys. This one uses a real key with a passphrase, as
# ~/.ssh/id is, loaded into an agent of the test's own. The copy names the key's path as
# the container would name the mounted file, and git signs with nothing but that copy.
new_case real gpg.format=ssh "user.signingkey=$ID_LITERAL" commit.gpgsign=true tag.gpgsign=true \
    user.name=Signer user.email=signer@example.com
rm -f "$CASE/home/.ssh/id" "$CASE/home/.ssh/id.pub"
ssh-keygen -q -t ed25519 -N "test-passphrase" -C signer@example.com -f "$CASE/home/.ssh/key_0"
printf '#!/bin/sh\necho test-passphrase\n' >"$CASE/askpass"
chmod 700 "$CASE/askpass"
printf 'signer@example.com namespaces="git" %s\n' "$(cut -d' ' -f1,2 "$CASE/home/.ssh/key_0.pub")" \
    >"$CASE/allowed_signers"
OUT="$(case_git configure_git_signing "$CASE/stage/gitconfig" "$CASE/project" \
    "$CASE/home/.ssh/key_0" "$CASE/home/.ssh/key_0" 0 2>&1)"
RC=$?
if [ "$RC" -eq 0 ]; then pass "a real key is accepted"; else fail "a real key was refused (rc=$RC): $OUT"; fi
repo="$CASE/repo"
git init -q "$repo"
agent_env="$(ssh-agent -s)"
REAL_AGENT_SOCK="$(printf '%s\n' "$agent_env" | awk -F'[=;]' '/^SSH_AUTH_SOCK=/ { print $2 }')"
REAL_AGENT_PID="$(printf '%s\n' "$agent_env" | awk -F'[=;]' '/^SSH_AGENT_PID=/ { print $2 }')"
# in_copy_git <git args>: git with the copy as its only config, the test's agent, and an
# askpass that refuses, so nothing can prompt or open a dialog.
in_copy_git() {
    SSH_AUTH_SOCK="$REAL_AGENT_SOCK" SSH_ASKPASS=/bin/false SSH_ASKPASS_REQUIRE=force \
        GIT_CONFIG_GLOBAL="$CASE/stage/gitconfig" GIT_CONFIG_NOSYSTEM=1 \
        setsid git -C "$repo" "$@" </dev/null
}
if in_copy_git commit -q --allow-empty -m "the agent is empty" 2>/dev/null; then
    fail "a commit was signed with the key not in the agent"
else
    pass "with the key not in the agent, the commit fails rather than prompting"
fi
if SSH_AUTH_SOCK="$REAL_AGENT_SOCK" SSH_ASKPASS="$CASE/askpass" SSH_ASKPASS_REQUIRE=force \
    setsid ssh-add -q "$CASE/home/.ssh/key_0" </dev/null; then
    pass "the key is loaded into the test's agent"
else
    fail "could not load the key into the test's agent"
fi
if in_copy_git commit -q --allow-empty -m "signed through the agent" &&
    in_copy_git -c gpg.ssh.allowedSignersFile="$CASE/allowed_signers" verify-commit HEAD 2>/dev/null; then
    pass "a plain commit is signed through the agent, and verifies"
else
    fail "a plain commit through the copy is not signed by the key"
fi
if in_copy_git tag -m "signed tag" v1 &&
    in_copy_git -c gpg.ssh.allowedSignersFile="$CASE/allowed_signers" verify-tag v1 2>/dev/null; then
    pass "a plain annotated tag is signed, and verifies"
else
    fail "a plain annotated tag through the copy is not signed by the key"
fi

echo "== the launcher"
# Configured after the EXIT trap is set, so a refusal still removes the directory holding
# the gitconfig copy.
trap_line="$(grep -n '^trap cleanup EXIT' "$LAUNCHER" | cut -d: -f1)"
cfg_line="$(grep -n "^if ! configure_git_signing \"\\\$CONFIG_TEMP/gitconfig\" \"\\\$PWD\" \"\\\$signing_primary\"" "$LAUNCHER" | cut -d: -f1)"
if [ -n "$cfg_line" ]; then pass "configures signing on the CONFIG_TEMP copy for the project"; else fail "never calls configure_git_signing on CONFIG_TEMP"; fi
if [ -n "$trap_line" ] && [ -n "$cfg_line" ] && [ "$cfg_line" -gt "$trap_line" ]; then
    pass "after the cleanup trap is set"
else
    fail "not after 'trap cleanup EXIT' (trap line ${trap_line:-none}, call line ${cfg_line:-none})"
fi
# The session's one run site. Signing configured after it would reach no container.
run_line="$(grep -n "^container_cmd run \\\$DOCKER_FLAGS --rm" "$LAUNCHER" | cut -d: -f1)"
if [ -n "$run_line" ] && [ -n "$cfg_line" ] && [ "$cfg_line" -lt "$run_line" ]; then
    pass "before the session's container run"
else
    fail "not before the session's container run (call line ${cfg_line:-none}, run line ${run_line:-none})"
fi
if grep -qF "signing_primary=\"\${SSH_KEYS[0]:-}\"" "$LAUNCHER" &&
    grep -qF "signing_in_container=\"\${SSH_KEY_PATHS[0]}\"" "$LAUNCHER"; then
    pass "the primary identity, and its mounted path, are what it passes"
else
    fail "the launcher no longer passes SSH_KEYS[0] and SSH_KEY_PATHS[0]"
fi
if grep -qF 'git-signing-key' "$LAUNCHER" "$SSH_LIB"; then
    fail "a private signing key is still staged into the container (git-signing-key)"
else
    pass "no private key is staged for signing"
fi

echo ""
echo "RESULT: passed: $PASS failed: $FAIL"
[ "$FAIL" -eq 0 ]
