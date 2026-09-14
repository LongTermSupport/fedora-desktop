#!/usr/bin/env bash
# Unit-test ccy's deploy-key alias resolution and forwarded-agent handling
# (Plan 00116, CCY 3.54.0).
#
# Sources lib/ssh-handling.bash from THIS repo (not the deployed /var/local copy)
# so a fix can be verified before running the playbook.
#
# WHY THIS TEST EXISTS. A box provisioned with per-repository deploy keys and no
# GitHub account holds no ~/.ssh/github_* key, and ccy used to see nothing else:
# it warned "No github_ SSH keys found" in a project whose remote named a
# perfectly good key through an ssh-config alias. The functions under test are
# what ccy now asks instead — ssh itself, via `ssh -G`, for what the alias means.
# Every case here runs against a STUB ssh / ssh-add placed first on PATH, so the
# suite needs no network, no agent and no real key.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports
# the full picture, and each result is checked explicitly. (Same shape as
# scripts/test-ccy-token-mode.bash.)
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
PURE_LIB="$LIB_DIR/common-pure.bash"
SSH_LIB="$LIB_DIR/ssh-handling.bash"

for lib in "$PURE_LIB" "$SSH_LIB"; do
    if [ ! -f "$lib" ]; then
        echo "FAIL: library not found at $lib" >&2
        exit 1
    fi
done

# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$PURE_LIB"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/ssh-handling.bash
source "$SSH_LIB"

for fn in resolve_github_ssh_alias remote_ssh_host parse_github_owner_repo \
          detect_project_github_alias render_ssh_alias_stanza compose_ssh_alias_exports \
          ssh_agent_usable _github_probe_identity github_identity_is_deploy_key; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: $fn is not defined after sourcing the libraries" >&2
        echo "      (the function is absent, not merely broken)" >&2
        exit 1
    fi
done

# mktemp, not "$$": this repo is bind-mounted into containers, and a PID in one
# namespace is not unique across them.
WORK="$(mktemp -d "$REPO_ROOT/untracked/ccy-ssh-handling-fixtures.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# ── Stub ssh / ssh-add ───────────────────────────────────────────────────────
# The stub answers `ssh -G <alias>` from a per-alias fixture file and `ssh -T`
# with a canned GitHub greeting chosen by the STUB_GREETING variable. Anything
# else is an error, so a code path that reaches the real network shows up as
# a failure here rather than as a slow, flaky pass.
STUB_BIN="$WORK/bin"
mkdir -p "$STUB_BIN" "$WORK/ssh-G"
cat > "$STUB_BIN/ssh" <<'STUB'
#!/usr/bin/env bash
set -u
mode=""
target=""
for a in "$@"; do
    case "$a" in
        -G) mode="G" ;;
        -T) mode="T" ;;
        -*) ;;
        *) target="$a" ;;
    esac
done
case "$mode" in
    G)
        f="${STUB_SSH_G_DIR:?}/$target"
        if [ -f "$f" ]; then cat "$f"; exit 0; fi
        # Real ssh -G still prints defaults for an unknown host; mirror that.
        printf 'hostname %s\nport 22\nuser %s\nidentityfile ~/.ssh/id_ed25519\n' "$target" "${USER:-nobody}"
        exit 0
        ;;
    T)
        printf '%s\n' "${STUB_GREETING:?}" >&2
        exit 1
        ;;
    *)
        echo "stub ssh: unexpected invocation: $*" >&2
        exit 99
        ;;
esac
STUB
cat > "$STUB_BIN/ssh-add" <<'STUB'
#!/usr/bin/env bash
exit "${STUB_SSH_ADD_RC:?}"
STUB
chmod 700 "$STUB_BIN/ssh" "$STUB_BIN/ssh-add"
export PATH="$STUB_BIN:$PATH"
export STUB_SSH_G_DIR="$WORK/ssh-G"

# Fixture key the alias points at, and an alias whose key is missing.
KEY_DIR="$WORK/keys"
mkdir -p "$KEY_DIR"
: > "$KEY_DIR/project_a"
chmod 600 "$KEY_DIR/project_a"
printf 'hostname ssh.github.com\nport 443\nuser git\nidentityfile %s\nidentitiesonly yes\n' \
    "$KEY_DIR/project_a" > "$WORK/ssh-G/gh-alias-a"
printf 'hostname github.com\nport 22\nuser git\nidentityfile %s\nidentityfile %s\n' \
    "$KEY_DIR/does_not_exist" "$KEY_DIR/project_a" > "$WORK/ssh-G/gh-alias-second-key"
printf 'hostname github.com\nport 22\nuser git\nidentityfile %s\n' \
    "$KEY_DIR/does_not_exist" > "$WORK/ssh-G/gh-alias-missing"
printf 'hostname gitlab.example.com\nport 22\nuser git\nidentityfile %s\n' \
    "$KEY_DIR/project_a" > "$WORK/ssh-G/not-github"

passed=0
failed=0
pass() { passed=$((passed + 1)); echo "  PASS: $*"; }
fail() { failed=$((failed + 1)); echo "  FAIL: $*"; }
hdr()  { echo ""; echo "=== $* ==="; }

# ── resolve_github_ssh_alias ─────────────────────────────────────────────────
hdr "resolve_github_ssh_alias"

out=$(resolve_github_ssh_alias gh-alias-a); rc=$?
expected=$(printf 'ssh.github.com\t443\t%s' "$KEY_DIR/project_a")
if [ "$rc" -eq 0 ] && [ "$out" = "$expected" ]; then
    pass "GitHub alias over 443 → hostname, port, existing key"
else
    fail "GitHub alias over 443: rc=$rc out='$out'"
fi

out=$(resolve_github_ssh_alias gh-alias-second-key); rc=$?
expected=$(printf 'github.com\t22\t%s' "$KEY_DIR/project_a")
if [ "$rc" -eq 0 ] && [ "$out" = "$expected" ]; then
    pass "first EXISTING identity file wins, not the first listed"
else
    fail "second-key alias: rc=$rc out='$out'"
fi

out=$(resolve_github_ssh_alias gh-alias-missing); rc=$?
expected=$(printf 'github.com\t22\t')
if [ "$rc" -eq 0 ] && [ "$out" = "$expected" ]; then
    pass "GitHub alias with no existing key → empty key field, still rc 0 (caller must fail loudly)"
else
    fail "missing-key alias: rc=$rc out='$out'"
fi

out=$(resolve_github_ssh_alias not-github); rc=$?
if [ "$rc" -eq 1 ] && [ -z "$out" ]; then
    pass "alias bound to a non-GitHub host → rc 1, no output"
else
    fail "non-github alias: rc=$rc out='$out'"
fi

for literal in github.com ssh.github.com; do
    out=$(resolve_github_ssh_alias "$literal"); rc=$?
    if [ "$rc" -eq 1 ] && [ -z "$out" ]; then
        pass "literal $literal is not an alias → rc 1"
    else
        fail "literal $literal: rc=$rc out='$out'"
    fi
done

out=$(resolve_github_ssh_alias ""); rc=$?
if [ "$rc" -eq 1 ]; then
    pass "empty host → rc 1"
else
    fail "empty host: rc=$rc"
fi

# ── remote_ssh_host ──────────────────────────────────────────────────────────
hdr "remote_ssh_host"

check_host() {
    local url="$1" want="$2" got
    got=$(remote_ssh_host "$url")
    if [ "$got" = "$want" ]; then
        pass "$url → '${want}'"
    else
        fail "$url → '$got' (wanted '$want')"
    fi
}
check_host "git@gh-alias-a:owner/repo.git" "gh-alias-a"
check_host "ssh://git@gh-alias-a/owner/repo.git" "gh-alias-a"
check_host "ssh://git@gh-alias-a:443/owner/repo.git" "gh-alias-a"
check_host "git@github.com:owner/repo.git" "github.com"
check_host "https://github.com/owner/repo" ""
check_host "" ""

# ── parse_github_owner_repo ──────────────────────────────────────────────────
hdr "parse_github_owner_repo"

check_parse() {
    local url="$1" want="$2" got rc
    got=$(parse_github_owner_repo "$url"); rc=$?
    if [ -n "$want" ]; then
        if [ "$rc" -eq 0 ] && [ "$got" = "$want" ]; then
            pass "$url → $want"
        else
            fail "$url → rc=$rc '$got' (wanted '$want')"
        fi
    else
        if [ "$rc" -ne 0 ] && [ -z "$got" ]; then
            pass "$url → not GitHub"
        else
            fail "$url → rc=$rc '$got' (wanted no match)"
        fi
    fi
}
# The forms that always worked must keep working.
check_parse "git@github.com:owner/repo.git" "owner/repo"
check_parse "git@github.com-work:owner/repo.git" "owner/repo"
check_parse "https://github.com/owner/repo.git" "owner/repo"
check_parse "ssh://git@github.com:443/owner/repo.git" "owner/repo"
# The alias forms, accepted only when ssh -G says the alias is GitHub.
check_parse "git@gh-alias-a:owner/repo.git" "owner/repo"
check_parse "ssh://git@gh-alias-a/owner/repo.git" "owner/repo"
check_parse "git@not-github:owner/repo.git" ""
check_parse "https://gitlab.example.com/owner/repo.git" ""

# ── detect_project_github_alias ──────────────────────────────────────────────
hdr "detect_project_github_alias"

make_repo() {
    local dir="$1" url="$2"
    git init -q "$dir"
    git -C "$dir" remote add origin "$url"
}
make_repo "$WORK/repo-alias" "git@gh-alias-a:owner/repo.git"
make_repo "$WORK/repo-literal" "git@github.com:owner/repo.git"
make_repo "$WORK/repo-missing" "git@gh-alias-missing:owner/repo.git"
make_repo "$WORK/repo-other" "git@not-github:owner/repo.git"

GITHUB_ALIAS_HOST=""; GITHUB_ALIAS_HOSTNAME=""; GITHUB_ALIAS_PORT=""; GITHUB_ALIAS_KEY=""
if detect_project_github_alias "$WORK/repo-alias" \
   && [ "$GITHUB_ALIAS_HOST" = "gh-alias-a" ] \
   && [ "$GITHUB_ALIAS_HOSTNAME" = "ssh.github.com" ] \
   && [ "$GITHUB_ALIAS_PORT" = "443" ] \
   && [ "$GITHUB_ALIAS_KEY" = "$KEY_DIR/project_a" ]; then
    pass "alias remote → all four GITHUB_ALIAS_* globals set"
else
    fail "alias remote: host='$GITHUB_ALIAS_HOST' hostname='$GITHUB_ALIAS_HOSTNAME' port='$GITHUB_ALIAS_PORT' key='$GITHUB_ALIAS_KEY'"
fi

GITHUB_ALIAS_HOST="stale"; GITHUB_ALIAS_KEY="stale"
if ! detect_project_github_alias "$WORK/repo-literal" && [ -z "$GITHUB_ALIAS_HOST" ] && [ -z "$GITHUB_ALIAS_KEY" ]; then
    pass "literal github.com remote → rc 1 and globals cleared"
else
    fail "literal remote: rc=0 or globals not cleared (host='$GITHUB_ALIAS_HOST')"
fi

if ! detect_project_github_alias "$WORK/repo-other" && [ -z "$GITHUB_ALIAS_HOST" ]; then
    pass "non-GitHub alias remote → rc 1"
else
    fail "non-github remote: rc=0 or host set ('$GITHUB_ALIAS_HOST')"
fi

err=$(detect_project_github_alias "$WORK/repo-missing" 2>&1 >/dev/null); rc=$?
if [ "$rc" -eq 2 ] && [[ "$err" == *"gh-alias-missing"* ]] && [[ "$err" == *"does_not_exist"* ]]; then
    pass "GitHub alias whose key file is missing → rc 2 naming alias and path (fail fast, not 'no keys')"
else
    fail "missing-key remote: rc=$rc err='$err'"
fi

if ! detect_project_github_alias "$WORK" 2>/dev/null; then
    pass "not a git repo → rc 1"
else
    fail "not a git repo: rc=0"
fi

# ── render_ssh_alias_stanza / compose_ssh_alias_exports ──────────────────────
hdr "render_ssh_alias_stanza"

want=$'Host gh-alias-a\n    HostName ssh.github.com\n    Port 443\n    User git\n    IdentityFile /root/.ssh/key_0\n    IdentitiesOnly yes'
got=$(render_ssh_alias_stanza gh-alias-a ssh.github.com 443 /root/.ssh/key_0 yes)
if [ "$got" = "$want" ]; then
    pass "stanza with IdentitiesOnly yes (no agent)"
else
    fail "stanza (yes):"$'\n'"$got"
fi

want=$'Host gh-alias-a\n    HostName github.com\n    Port 22\n    User git\n    IdentityFile /root/.ssh/key_1'
got=$(render_ssh_alias_stanza gh-alias-a github.com 22 /root/.ssh/key_1 no)
if [ "$got" = "$want" ]; then
    pass "stanza without IdentitiesOnly (agent forwarded: agent identities are tried first)"
else
    fail "stanza (no):"$'\n'"$got"
fi

want=$'Host gh-alias-a\n    HostName ssh.github.com\n    Port 443\n    User git'
got=$(render_ssh_alias_stanza gh-alias-a ssh.github.com 443 "" yes)
if [ "$got" = "$want" ]; then
    pass "stanza with no mounted alias key → no IdentityFile, no IdentitiesOnly (container agent answers)"
else
    fail "stanza (no key):"$'\n'"$got"
fi

hdr "compose_ssh_alias_exports"

SSH_CONFIG_EXTRA_B64=""; SSH_KNOWN_HOSTS_PINS=""
compose_ssh_alias_exports gh-alias-a ssh.github.com 443 /root/.ssh/key_0 yes
decoded=$(printf '%s' "$SSH_CONFIG_EXTRA_B64" | base64 -d)
if [ "$decoded" = "$(render_ssh_alias_stanza gh-alias-a ssh.github.com 443 /root/.ssh/key_0 yes)" ] \
   && [ "$SSH_KNOWN_HOSTS_PINS" = "ssh.github.com:443" ]; then
    pass "443 alias → base64 stanza and a [ssh.github.com]:443 pin"
else
    fail "443 alias: pins='$SSH_KNOWN_HOSTS_PINS' decoded='$decoded'"
fi

SSH_CONFIG_EXTRA_B64=""; SSH_KNOWN_HOSTS_PINS=""
compose_ssh_alias_exports gh-alias-a github.com 22 /root/.ssh/key_0 yes
if [ -z "$SSH_KNOWN_HOSTS_PINS" ]; then
    pass "github.com:22 alias → no extra pin (the entrypoint already pins github.com)"
else
    fail "22 alias: pins='$SSH_KNOWN_HOSTS_PINS'"
fi

# ── ssh_agent_usable ─────────────────────────────────────────────────────────
hdr "ssh_agent_usable"

sock="$WORK/agent.sock"
: > "$sock"

( unset SSH_AUTH_SOCK; STUB_SSH_ADD_RC=0 ssh_agent_usable ); rc=$?
if [ "$rc" -eq 1 ]; then pass "SSH_AUTH_SOCK unset → 1"; else fail "unset sock: rc=$rc"; fi

SSH_AUTH_SOCK="$WORK/nope" STUB_SSH_ADD_RC=0 ssh_agent_usable; rc=$?
if [ "$rc" -eq 1 ]; then pass "SSH_AUTH_SOCK names nothing on disk → 1"; else fail "missing sock: rc=$rc"; fi

SSH_AUTH_SOCK="$sock" STUB_SSH_ADD_RC=2 ssh_agent_usable; rc=$?
if [ "$rc" -eq 1 ]; then pass "ssh-add cannot connect (rc 2) → 1"; else fail "rc2: rc=$rc"; fi

SSH_AUTH_SOCK="$sock" STUB_SSH_ADD_RC=1 ssh_agent_usable; rc=$?
if [ "$rc" -eq 1 ]; then pass "agent with no identities (rc 1) → 1"; else fail "rc1: rc=$rc"; fi

SSH_AUTH_SOCK="$sock" STUB_SSH_ADD_RC=0 ssh_agent_usable; rc=$?
if [ "$rc" -eq 0 ]; then pass "agent with identities (rc 0) → 0"; else fail "rc0: rc=$rc"; fi

# ── _github_probe_identity / github_identity_is_deploy_key ──────────────────
hdr "_github_probe_identity"

got=$(STUB_GREETING="Hi someone! You've successfully authenticated, but GitHub does not provide shell access." \
      _github_probe_identity "$KEY_DIR/project_a" github.com 22)
if [ "$got" = "someone" ]; then pass "account key → login"; else fail "account key → '$got'"; fi

got=$(STUB_GREETING="Hi owner/repo! You've successfully authenticated, but GitHub does not provide shell access." \
      _github_probe_identity "$KEY_DIR/project_a" ssh.github.com 443)
if [ "$got" = "owner/repo" ]; then pass "deploy key → owner/repo"; else fail "deploy key → '$got'"; fi

got=$(STUB_GREETING="git@github.com: Permission denied (publickey)." \
      _github_probe_identity "$KEY_DIR/project_a" github.com 22)
if [ -z "$got" ]; then pass "rejected key → empty"; else fail "rejected key → '$got'"; fi

got=$(SSH_AUTH_SOCK="$sock" STUB_GREETING="Hi person! You've successfully authenticated, but GitHub does not provide shell access." \
      _github_probe_identity ssh-agent github.com 22)
if [ "$got" = "person" ]; then pass "the ssh-agent sentinel probes through the agent → login"; else fail "agent probe → '$got'"; fi

if github_identity_is_deploy_key "owner/repo"; then pass "owner/repo is a deploy key"; else fail "owner/repo not classified as deploy key"; fi
if ! github_identity_is_deploy_key "someone"; then pass "a login is not a deploy key"; else fail "login classified as deploy key"; fi
if ! github_identity_is_deploy_key ""; then pass "empty is not a deploy key"; else fail "empty classified as deploy key"; fi

# ── Summary ──────────────────────────────────────────────────────────────────
echo ""
echo "ccy ssh-handling: passed: $passed  failed: $failed"
echo "(build_ssh_mounts_and_validate itself is not driven here: it calls gh and"
echo " prompts; its pieces above are what this suite vouches for.)"
if [ "$failed" -ne 0 ]; then
    exit 1
fi
