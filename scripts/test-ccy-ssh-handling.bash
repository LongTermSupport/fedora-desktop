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
          ssh_agent_usable ssh_agent_holds_key _github_probe_identity github_identity_is_deploy_key; do
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
        # STUB_SSH_HANG stands in for an agent that asks before it signs and gets no answer.
        if [ -n "${STUB_SSH_HANG:-}" ]; then
            sleep "$STUB_SSH_HANG"
        fi
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
# As ssh -G really prints it: the tilde unexpanded (measured on OpenSSH 10).
printf 'hostname ssh.github.com\nport 443\nuser git\nidentityfile ~/keys/project_a\n' \
    > "$WORK/ssh-G/gh-alias-tilde"

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

out=$(HOME="$WORK" resolve_github_ssh_alias gh-alias-tilde); rc=$?
expected=$(printf 'ssh.github.com\t443\t%s' "$WORK/keys/project_a")
if [ "$rc" -eq 0 ] && [ "$out" = "$expected" ]; then
    pass "a '~/' IdentityFile (as ssh -G prints it) is expanded against HOME before the existence test"
else
    fail "tilde alias: rc=$rc out='$out' (wanted '$expected')"
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

# An unattended launch (a --max-age restart, a reboot restore) has nobody to answer an agent
# that asks before signing, so the probe is bounded there and says why it gave up; an attended
# launch is not bounded, because a person can confirm.
start=$SECONDS
got=$(CCY_UNATTENDED_LAUNCH=true CCY_UNATTENDED_PROBE_SECONDS=2 STUB_SSH_HANG=30 \
      SSH_AUTH_SOCK="$sock" STUB_GREETING="Hi late! You've successfully authenticated, but GitHub does not provide shell access." \
      _github_probe_identity ssh-agent github.com 22 2>"$WORK/probe-timeout.err")
elapsed=$((SECONDS - start))
if [ -z "$got" ] && [ "$elapsed" -lt 20 ]; then pass "unattended: a probe that never answers gives up (${elapsed}s)"; else fail "unattended hang: got '$got' after ${elapsed}s"; fi
if grep -q 'no answer' "$WORK/probe-timeout.err"; then pass "…and says it got no answer"; else fail "unattended hang: no reason on stderr"; fi
got=$(CCY_UNATTENDED_LAUNCH=true CCY_UNATTENDED_PROBE_SECONDS=10 \
      STUB_GREETING="Hi prompt! You've successfully authenticated, but GitHub does not provide shell access." \
      _github_probe_identity "$KEY_DIR/project_a" github.com 22)
if [ "$got" = "prompt" ]; then pass "unattended: a probe that answers in time → login"; else fail "unattended answer → '$got'"; fi
got=$(CCY_UNATTENDED_LAUNCH=false STUB_SSH_HANG=3 \
      STUB_GREETING="Hi patient! You've successfully authenticated, but GitHub does not provide shell access." \
      _github_probe_identity "$KEY_DIR/project_a" github.com 22)
if [ "$got" = "patient" ]; then pass "attended: a slow answer is still waited for"; else fail "attended slow → '$got'"; fi

if github_identity_is_deploy_key "owner/repo"; then pass "owner/repo is a deploy key"; else fail "owner/repo not classified as deploy key"; fi
if ! github_identity_is_deploy_key "someone"; then pass "a login is not a deploy key"; else fail "login classified as deploy key"; fi
if ! github_identity_is_deploy_key ""; then pass "empty is not a deploy key"; else fail "empty classified as deploy key"; fi

# ── discover_and_select_ssh_keys: the menu (Plan 00145) ──────────────────────
# Three account keys, no deploy-key alias, no agent. STUB_WORKING is what the push
# probe reports. Each run is a subshell, so the stubs and HOME never leak out; it
# leaves the menu text in menu.out and "rc=N" plus the chosen keys in menu.keys.
hdr "discover_and_select_ssh_keys (key menu)"

MENU_HOME="$WORK/menu-home"
mkdir -p "$MENU_HOME/.ssh"
# Real key pairs, so the agent-held cases can match a fingerprint; the stubs above shadow
# ssh and ssh-add only, ssh-keygen is the real one.
if ! command -v ssh-keygen >/dev/null; then
    echo "FAIL: ssh-keygen is not installed; the agent-held cases need it" >&2
    exit 1
fi
for alias in alpha beta gamma; do
    ssh-keygen -q -t ed25519 -N '' -C "fixture-$alias" -f "$MENU_HOME/.ssh/github_$alias"
done
# What `ssh-add -l` prints for a key: bits, fingerprint, comment, type.
agent_line() { ssh-keygen -lf "$1.pub"; }
K_ALPHA="$MENU_HOME/.ssh/github_alpha"
K_BETA="$MENU_HOME/.ssh/github_beta"
K_GAMMA="$MENU_HOME/.ssh/github_gamma"

run_menu() {
    local working="$1" input="$2"
    (
        HOME="$MENU_HOME"
        PROBE_LOG_DIR="$WORK"
        SSH_KEYS=()
        detect_project_github_alias() {
            [ -n "${STUB_ALIAS_KEY:-}" ] || return 1
            GITHUB_ALIAS_KEY="$STUB_ALIAS_KEY"
            GITHUB_ALIAS_HOST=gh-alias-a GITHUB_ALIAS_HOSTNAME=ssh.github.com GITHUB_ALIAS_PORT=443
            return 0
        }
        ssh_agent_usable() {
            SSH_AGENT_PROBE_OUTPUT="${STUB_AGENT_LIST:-}"
            [ -n "$SSH_AGENT_PROBE_OUTPUT" ]
        }
        get_project_remote_url() { echo "git@github.com:owner/repo.git"; }
        probe_gh_keys_for_remote() { [ -n "$working" ] && printf '%s\n' "$working"; return 0; }
        discover_and_select_ssh_keys ccy < <(printf '%b' "$input") > "$WORK/menu.out" 2>&1
        printf 'rc=%s\n' "$?" > "$WORK/menu.keys"
        printf '%s\n' "${SSH_KEYS[@]}" >> "$WORK/menu.keys"
    )
}
menu_chose() {
    local expected
    expected="$(printf 'rc=0\n%s' "$1")"
    [ "$(cat "$WORK/menu.keys")" = "$expected" ]
}
menu_says() { grep -qF -- "$1" "$WORK/menu.out"; }

run_menu "$K_BETA" '\n'
if menu_chose "$K_BETA" && ! menu_says "$K_ALPHA" && ! menu_says "$K_GAMMA"; then
    pass "one key can push → the menu lists only it, and ENTER takes it"
else
    fail "one pusher, ENTER: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

run_menu "$K_BETA" '1\n'
if menu_chose "$K_BETA"; then pass "one key can push → 1 takes it"; else fail "one pusher, 1: $(tr '\n' ' ' < "$WORK/menu.keys")"; fi

run_menu "$K_BETA" 'a\n2\n'
if menu_chose "$K_BETA" && menu_says "$K_ALPHA" && ! menu_says "Use it anyway"; then
    pass "a → every identity is listed; a key that can push is taken without a question"
else
    fail "a then the pusher: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

run_menu "$K_BETA" 'a\n1\nn\n1\ny\n'
if menu_chose "$K_ALPHA" && menu_says "cannot push to this remote"; then
    pass "a → a key that cannot push is asked about; n re-prompts, y takes it"
else
    fail "a then a non-pusher: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

run_menu "$K_BETA" 'a\n3\n\n2\n'
if menu_chose "$K_BETA"; then
    pass "ENTER at the question means no, and the next pick is taken"
else
    fail "ENTER at the question: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

run_menu "$K_BETA" '0\n'
if menu_chose ""; then pass "0 in the short list → no key"; else fail "0: $(tr '\n' ' ' < "$WORK/menu.keys")"; fi

run_menu "$K_BETA" '9\nx\n1\n'
if menu_chose "$K_BETA" && menu_says "Invalid selection"; then
    pass "an out-of-range or non-numeric pick re-prompts"
else
    fail "bad picks: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

run_menu "$(printf '%s\n%s' "$K_ALPHA" "$K_GAMMA")" '2\n'
if menu_chose "$K_GAMMA" && ! menu_says "$K_BETA"; then
    pass "two keys can push → only those two are listed"
else
    fail "two pushers: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

run_menu "" '1\n'
if menu_chose "$K_ALPHA" && menu_says "$K_GAMMA" && ! menu_says "Use it anyway"; then
    pass "no key can push → every identity is listed and taken without a question"
else
    fail "no pusher: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

run_menu "$K_BETA" 'a\n\n'
if menu_chose "$K_BETA"; then
    pass "ENTER after a takes the key that can push, not the first listed"
else
    fail "ENTER after a: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

run_menu "" '99999999999999999999\n9223372036854775809\n1\n'
if menu_chose "$K_ALPHA" && [ "$(grep -c 'Invalid selection' "$WORK/menu.out")" -eq 2 ]; then
    pass "a number too long to compare is invalid, never a silent pick"
else
    fail "overflowing pick: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

run_menu "$K_BETA" '7\n8\n9\n1\n'
if [ "$(head -1 "$WORK/menu.keys")" = "rc=1" ] && [ -z "$(tail -n +2 "$WORK/menu.keys" | tr -d '\n')" ] \
        && menu_says "Giving up after 3 invalid selections"; then
    pass "three invalid picks end the menu with rc 1 and no key"
else
    fail "three bad picks: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

run_menu "$K_BETA" ''
if [ "$(head -1 "$WORK/menu.keys")" = "rc=1" ] && menu_says "Input closed"; then
    pass "closed input ends the menu with rc 1"
else
    fail "closed input: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

STUB_ALIAS_KEY="$KEY_DIR/project_a" run_menu "$K_BETA" 'a\n1\ny\n'
if menu_chose "$KEY_DIR/project_a" && menu_says "was not checked for push access" \
        && ! menu_says "cannot push to this remote"; then
    pass "the remote's own key, never probed, is called unchecked rather than unable to push"
else
    fail "unprobed alias key: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

# ── ssh_agent_holds_key ──────────────────────────────────────────────────────
hdr "ssh_agent_holds_key"

SSH_AGENT_PROBE_OUTPUT="$(agent_line "$K_ALPHA")"
if ssh_agent_holds_key "$K_ALPHA"; then pass "a key whose fingerprint the agent lists is held"; else fail "listed key not held"; fi
if ! ssh_agent_holds_key "$K_BETA"; then pass "a key the agent does not list is not held"; else fail "unlisted key reported held"; fi

SSH_AGENT_PROBE_OUTPUT="$(agent_line "$K_BETA")"$'\n'"$(agent_line "$K_ALPHA")"
if ssh_agent_holds_key "$K_ALPHA" && ssh_agent_holds_key "$K_BETA" && ! ssh_agent_holds_key "$K_GAMMA"; then
    pass "several agent identities: each listed key is held, the unlisted one is not"
else
    fail "multi-identity agent"
fi

cp "$K_ALPHA" "$WORK/no-pub-key"
if ! ssh_agent_holds_key "$WORK/no-pub-key"; then pass "a key with no .pub beside it counts as not held"; else fail "key with no .pub reported held"; fi

printf 'not a public key\n' > "$WORK/bad-pub-key.pub"
: > "$WORK/bad-pub-key"
if ! ssh_agent_holds_key "$WORK/bad-pub-key"; then pass "an unreadable .pub counts as not held"; else fail "unreadable .pub reported held"; fi
SSH_AGENT_PROBE_OUTPUT=""

# ── discover_and_select_ssh_keys: an agent that already holds an account key ──
hdr "discover_and_select_ssh_keys (agent already unlocked)"

AGENT_HOLDS_ALPHA="$(agent_line "$K_ALPHA")"
AGENT_HOLDS_BETA="$(agent_line "$K_BETA")"

STUB_AGENT_LIST="$AGENT_HOLDS_ALPHA" run_menu "" '\n'
if menu_chose "ssh-agent" && menu_says "already unlocked, no passphrase asked" && menu_says "← default"; then
    pass "no key can push, agent holds an account key → the agent is the default and ENTER takes it"
else
    fail "agent default: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

STUB_AGENT_LIST="$AGENT_HOLDS_ALPHA" run_menu "" '1\n'
if menu_chose "$K_ALPHA" && menu_says "$K_ALPHA  (also in your ssh-agent, which asks no passphrase)" \
        && ! menu_says "$K_BETA  (also in your ssh-agent"; then
    pass "only the key the agent holds is marked as also in the agent"
else
    fail "agent marker: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

STUB_AGENT_LIST="unrelated-fingerprint" run_menu "" '\n'
if [ "$(head -1 "$WORK/menu.keys")" = "rc=1" ] && menu_says "No default available" && ! menu_says "already unlocked"; then
    pass "an agent holding no account key gets no default and no unlocked claim, as before"
else
    fail "unrelated agent: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

STUB_AGENT_LIST="$AGENT_HOLDS_ALPHA" run_menu "$K_BETA" '\n'
if menu_chose "$K_BETA" && ! menu_says "the session's ssh-agent"; then
    pass "a verified pusher stays the default, and an agent holding a different key stays off the short list"
else
    fail "pusher beats agent: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

STUB_AGENT_LIST="$AGENT_HOLDS_BETA" run_menu "$K_BETA" '\n'
if menu_chose "$K_BETA" && menu_says "the session's ssh-agent" && menu_says "$K_BETA  ✓ has push access to this remote  (also in your ssh-agent" \
        && menu_says "EVERY key the agent holds"; then
    pass "the agent holds the key that can push → listed beside it, saying it exposes every key it holds; the key file is the default"
else
    fail "agent holds the pusher, ENTER: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

# The remote's alias key IS an account key: one file is one line, with the account key's marks.
STUB_AGENT_LIST="$AGENT_HOLDS_BETA" STUB_ALIAS_KEY="$K_BETA" run_menu "$K_BETA" '\n'
if menu_chose "$K_BETA" && [ "$(grep -cF -- ") $K_BETA" "$WORK/menu.out")" -eq 1 ] \
        && menu_says "the project remote's key" && menu_says "✓ has push access to this remote" \
        && menu_says "(also in your ssh-agent"; then
    pass "an alias key that is also an account key is listed once, carrying push and agent marks, and is the default"
else
    fail "alias is an account key: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

STUB_ALIAS_KEY="$K_BETA" run_menu "$K_BETA" '\n'
if menu_chose "$K_BETA" && [ "$(grep -cF -- ") $K_BETA" "$WORK/menu.out")" -eq 1 ]; then
    pass "with no agent the alias-and-account key is listed once and is the default"
else
    fail "alias is an account key, no agent: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

STUB_AGENT_LIST="$AGENT_HOLDS_BETA" run_menu "$K_BETA" '2\n'
if menu_chose "ssh-agent" && ! menu_says "Use it anyway"; then
    pass "the agent, listed beside a pusher it holds, is taken without the cannot-push question"
else
    fail "agent beside pusher, 2: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

STUB_AGENT_LIST="$AGENT_HOLDS_ALPHA" STUB_ALIAS_KEY="$KEY_DIR/project_a" run_menu "" '\n'
if menu_chose "$KEY_DIR/project_a"; then
    pass "the remote's own key stays the default over an agent holding an account key"
else
    fail "alias beats agent: $(tr '\n' ' ' < "$WORK/menu.keys")"
fi

# ── ccy_askpass_passphrase_supply: the user's SSH_ASKPASS answers for one key ──
hdr "ccy_askpass_passphrase_supply"

PP_SECRET="fixture-passphrase-$RANDOM"
K_LOCKED="$WORK/locked_key"
ssh-keygen -q -t ed25519 -N "$PP_SECRET" -C fixture-locked -f "$K_LOCKED"
ASKPASS_OK="$WORK/askpass-ok"
ASKPASS_NONE="$WORK/askpass-none"
ASKPASS_LOG="$WORK/askpass.prompts"
cat > "$ASKPASS_OK" <<ASKPASS_BODY
#!/bin/sh
printf '%s\\n' "\$1" >> "$ASKPASS_LOG"
printf '%s\\n' "$PP_SECRET"
ASKPASS_BODY
cat > "$ASKPASS_NONE" <<'ASKPASS_BODY'
#!/bin/sh
exit 1
ASKPASS_BODY
chmod 0700 "$ASKPASS_OK" "$ASKPASS_NONE"

# run_supply <askpass-or-empty> <terminal true|false> <key>...: prints rc, the supplied file's
# existence and content, and whether the discard removed it.
run_supply() {
    local askpass="$1" terminal="$2"
    shift 2
    (
        XDG_RUNTIME_DIR="$WORK/run"
        mkdir -p "$XDG_RUNTIME_DIR"
        RESTORE_SSH_PASSPHRASE_FILE=""
        SSH_KEYS=("$@")
        SSH_ASKPASS="$askpass"
        [ -n "$askpass" ] || unset SSH_ASKPASS
        ccy_has_terminal() { [ "$terminal" = true ]; }
        ccy_askpass_passphrase_supply > "$WORK/supply.out" 2>&1
        rc=$?
        printf 'rc=%s\n' "$rc"
        printf 'file=%s\n' "${RESTORE_SSH_PASSPHRASE_FILE:+set}"
        if [ -n "$RESTORE_SSH_PASSPHRASE_FILE" ]; then
            printf 'mode=%s\n' "$(stat -c %a -- "$RESTORE_SSH_PASSPHRASE_FILE")"
            printf 'content=%s\n' "$(cat -- "$RESTORE_SSH_PASSPHRASE_FILE")"
        fi
        ccy_askpass_passphrase_discard
        printf 'left=%s\n' "$(find "$XDG_RUNTIME_DIR" -name 'ccy-pp.*' | wc -l)"
        printf 'after=%s\n' "${RESTORE_SSH_PASSPHRASE_FILE:+set}"
    ) > "$WORK/supply.result"
}
supply_has() { grep -qxF -- "$1" "$WORK/supply.result"; }

: > "$ASKPASS_LOG"
run_supply "$ASKPASS_OK" true "$K_LOCKED"
if supply_has "rc=0" && supply_has "file=set" && supply_has "mode=600" \
        && supply_has "content=$PP_SECRET" && supply_has "left=0" && supply_has "after=" \
        && grep -qF "Enter passphrase for $K_LOCKED" "$ASKPASS_LOG" \
        && grep -qF "no prompt, here or in the container" "$WORK/supply.out"; then
    pass "one encrypted key + a helper that answers → an owner-only file holding the answer, the helper asked ssh-add's own question, discard removes it"
else
    fail "supply: $(tr '\n' ' ' < "$WORK/supply.result") / $(cat "$WORK/supply.out")"
fi
if ! grep -qF -- "$PP_SECRET" "$WORK/supply.out"; then
    pass "the passphrase never appears in what the launcher prints"
else
    fail "passphrase printed"
fi

: > "$ASKPASS_LOG"
run_supply "$ASKPASS_OK" true "$K_ALPHA"
if supply_has "rc=0" && supply_has "file=" && [ ! -s "$ASKPASS_LOG" ]; then
    pass "a key with no passphrase is not asked about, and nothing is supplied"
else
    fail "unencrypted key: $(tr '\n' ' ' < "$WORK/supply.result")"
fi

: > "$ASKPASS_LOG"
run_supply "$ASKPASS_OK" true "$K_LOCKED" "$K_ALPHA"
if supply_has "file=" && [ ! -s "$ASKPASS_LOG" ]; then
    pass "two selected keys → the helper is not asked (one answer cannot be assumed to fit both)"
else
    fail "two keys: $(tr '\n' ' ' < "$WORK/supply.result")"
fi

: > "$ASKPASS_LOG"
run_supply "$ASKPASS_OK" true "ssh-agent"
if supply_has "file=" && [ ! -s "$ASKPASS_LOG" ]; then
    pass "the forwarded agent is never asked about"
else
    fail "agent sentinel: $(tr '\n' ' ' < "$WORK/supply.result")"
fi

: > "$ASKPASS_LOG"
run_supply "$ASKPASS_OK" false "$K_LOCKED"
if supply_has "file=" && [ ! -s "$ASKPASS_LOG" ]; then
    pass "no terminal → the helper is not asked"
else
    fail "no terminal: $(tr '\n' ' ' < "$WORK/supply.result")"
fi

: > "$ASKPASS_LOG"
run_supply "" true "$K_LOCKED"
if supply_has "rc=0" && supply_has "file="; then
    pass "no SSH_ASKPASS → nothing supplied, ordinary prompting"
else
    fail "no askpass: $(tr '\n' ' ' < "$WORK/supply.result")"
fi

run_supply "$WORK/not-a-program" true "$K_LOCKED"
if supply_has "rc=0" && supply_has "file="; then
    pass "SSH_ASKPASS naming something that is not executable → nothing supplied"
else
    fail "askpass not executable: $(tr '\n' ' ' < "$WORK/supply.result")"
fi

run_supply "$ASKPASS_NONE" true "$K_LOCKED"
if supply_has "rc=0" && supply_has "file=" && supply_has "left=0" \
        && grep -qF "gave no passphrase" "$WORK/supply.out"; then
    pass "a helper that gives no answer → a note, no file left behind, ordinary prompting"
else
    fail "helper refuses: $(tr '\n' ' ' < "$WORK/supply.result") / $(cat "$WORK/supply.out")"
fi

run_supply "$ASKPASS_OK" true "$WORK/no-such-key"
if supply_has "rc=0" && supply_has "file="; then
    pass "a key file that is missing is not mistaken for an encrypted one"
else
    fail "missing key: $(tr '\n' ' ' < "$WORK/supply.result")"
fi

# ── Summary ──────────────────────────────────────────────────────────────────
echo ""
echo "ccy ssh-handling: passed: $passed  failed: $failed"
echo "(build_ssh_mounts_and_validate itself is not driven here: it calls gh and"
echo " prompts; its pieces above are what this suite vouches for.)"
if [ "$failed" -ne 0 ]; then
    exit 1
fi
