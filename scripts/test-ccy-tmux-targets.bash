#!/usr/bin/env bash
# Test the tmux targets ccy names its sessions by against a REAL tmux server (Plan 00135
# Task 7.1, fedora-desktop#69).
#
# WHY THIS EXISTS. `ccy-sessions verify-restore` once captured a pane with `-t "=name"`.
# Real tmux refuses that ("can't find pane: =name"): `=name` is a SESSION target, and a
# pane command needs `=name:`. The suites that cover ccy-sessions run it under a fake tmux,
# and that fake stripped the `=`, so they passed while every verify-restore on a host
# failed on its first session. A fake can only be as right as its author's idea of tmux;
# this suite asks tmux itself.
#
# HOW. A private tmux server, on a label of its own under a socket directory of its own
# (never the server a user or a ccy session is running on), holds one session with `cat`
# in its pane. Every `-t`/`-s` target in ccy-sessions and lib/tmux-session.bash is read
# out of the source, not retyped here, and run through the library's own ccy_tmux against
# that server: a pane command's form must reach the pane, a session command's form the
# session. The bare `=name` pane form must be refused, so the #69 defect cannot come back
# with this suite green. The library's own readers (ccy_tmux_list, _ccy_tmux_clients) are
# run too, because they recognise tmux's error wording and only real tmux has it.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TOOL="$REPO_ROOT/files/home/.local/bin/ccy-sessions"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
SOURCES=("$TOOL" "$LIB_DIR/tmux-session.bash")

for f in "${SOURCES[@]}" "$LIB_DIR/common-pure.bash"; do
    if [ ! -f "$f" ]; then
        echo "FAIL: $f does not exist" >&2
        exit 1
    fi
done
if ! command -v tmux >/dev/null; then
    echo "FAIL: tmux not found — this suite drives a real tmux server and cannot pass without it." >&2
    echo "  In a ccy session it comes from .claude/ccy/Dockerfile: start ccy again so the project image is rebuilt." >&2
    echo "  On the host, play-claude-yolo.yml installs it; in CI, .github/workflows/qa.yml." >&2
    exit 1
fi

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

# ── the private server ────────────────────────────────────────────────────────────────
# mktemp's default directory, not the repo: a unix socket path is limited to 107 bytes, and
# a checkout can sit deep enough to pass that.
SOCK_DIR="$(mktemp -d)"
export TMUX_TMPDIR="$SOCK_DIR"
unset TMUX TMUX_PANE

# shellcheck source=/dev/null
source "$LIB_DIR/common-pure.bash"
# shellcheck source=/dev/null
source "$LIB_DIR/tmux-session.bash"
# A label of its own as well as a directory of its own: either alone keeps this suite off
# every real server.
CCY_TMUX_SOCKET="ccy-target-test-$$"

cleanup() {
    tmux -L "$CCY_TMUX_SOCKET" kill-server 2>/dev/null
    rm -rf "$SOCK_DIR"
}
trap cleanup EXIT

NAME="ccy-target-test"
WORK="$SOCK_DIR/project"
mkdir -p "$WORK"

# Before the server exists: the library reads "no server" as an empty list, not a failure.
rc=0
out="$(ccy_tmux_list 2>&1)" || rc=$?
check "ccy_tmux_list with no server: empty, status 0" "0:" "$rc:$out"

if ! out="$(ccy_tmux -f /dev/null new-session -d -s "$NAME" -c "$WORK" -x 80 -y 24 cat 2>&1)"; then
    echo "FAIL: the private tmux server could not start: $out" >&2
    exit 1
fi
socket="$(ccy_tmux display-message -p -t "=${NAME}:" '#{socket_path}')"
case "$socket" in
"$SOCK_DIR"/*) ;;
*)
    echo "FAIL: the test server's socket is $socket, outside $SOCK_DIR; stopping before touching it." >&2
    exit 1
    ;;
esac

# ── the library's readers, against real tmux wording ──────────────────────────────────
check "ccy_tmux_list names the session, unattached, in its directory" \
    "$NAME 0 $WORK" "$(ccy_tmux_list)"
rc=0
out="$(_ccy_tmux_clients "$NAME" 2>&1)" || rc=$?
check "_ccy_tmux_clients on a session with no terminal: none, status 0" "0:" "$rc:$out"
rc=0
out="$(_ccy_tmux_clients "$NAME-gone" 2>&1)" || rc=$?
check "_ccy_tmux_clients on a session that does not exist: silent status 2" "2:" "$rc:$out"

# ── every target in the source ────────────────────────────────────────────────────────
# One record per call: "<file>:<line> <subcommand> <target template>".
# D is a literal dollar sign, so the source's own variable references can be spelt here.
D='$'
TARGET_RE="(ccy_tmux|tmux -L \"[${D}]CCY_TMUX_SOCKET\") ([a-z-]+) .*-[ts] \"([^\"]*)\""
calls=()
for f in "${SOURCES[@]}"; do
    n=0
    while IFS= read -r line; do
        n=$((n + 1))
        if [[ "$line" =~ $TARGET_RE ]]; then
            calls+=("${f#"$REPO_ROOT"/}:$n ${BASH_REMATCH[2]} ${BASH_REMATCH[3]}")
        fi
    done <"$f"
done

# the template with the session's name in it; a template naming anything else is refused.
fill() {
    local t="$1" name="$2"
    t="${t//"${D}{name}"/$name}"
    t="${t//"${D}name"/$name}"
    t="${t//"${D}1"/$name}"
    case "$t" in
    *"$D"*) return 1 ;;
    esac
    printf '%s\n' "$t"
}

pane_template=""
for call in "${calls[@]}"; do
    read -r where sub template <<<"$call"
    if ! target="$(fill "$template" "$NAME")"; then
        check "$where: $sub's target $template names the session" "a session name" "$template"
        continue
    fi
    rc=0
    case "$sub" in
    capture-pane)
        out="$(ccy_tmux capture-pane -p -t "$target" 2>&1)" || rc=$?
        pane_template="$template"
        ;;
    send-keys)
        out="$(ccy_tmux send-keys -t "$target" -l " " 2>&1)" || rc=$?
        ;;
    list-clients)
        out="$(ccy_tmux list-clients -t "$target" 2>&1)" || rc=$?
        ;;
    kill-session)
        ccy_tmux new-session -d -s "$NAME-kill" -c "$WORK" cat
        target="$(fill "$template" "$NAME-kill")"
        out="$(ccy_tmux kill-session -t "$target" 2>&1)" || rc=$?
        if ccy_tmux has-session -t "=$NAME-kill" 2>/dev/null; then
            rc=99 out="the session outlived kill-session"
        fi
        ;;
    attach-session | detach-client)
        # Needing a terminal (attach) or an attached one (detach), these are resolved the
        # way tmux resolves every target-session, through has-session.
        out="$(ccy_tmux has-session -t "$target" 2>&1)" || rc=$?
        ;;
    *)
        rc=98 out="a tmux command this suite does not know; add it to the case above"
        ;;
    esac
    check "$where: $sub -t $template reaches the session (real tmux)" "0" "$rc${out:+ ($out)}"
done
have_calls=no
if [ "${#calls[@]}" -gt 0 ]; then
    have_calls=yes
fi
check "the source still holds tmux targets to test" "yes" "$have_calls"
have_pane=no
if [ -n "$pane_template" ]; then
    have_pane=yes
fi
check "a capture-pane target was among them" "yes" "$have_pane"

# ── the pane form reaches the pane, and the bare form is refused (#69) ────────────────
if [ -n "$pane_template" ]; then
    pane_target="$(fill "$pane_template" "$NAME")"
    marker="typed-through-$$"
    ccy_tmux send-keys -t "$pane_target" -l "$marker"
    ccy_tmux send-keys -t "$pane_target" Enter
    seen=no
    for _ in $(seq 50); do
        screen="$(ccy_tmux capture-pane -p -t "$pane_target")"
        if [[ "$screen" == *"$marker"* ]]; then
            seen=yes
            break
        fi
        sleep 0.1
    done
    check "keys sent to $pane_template show on the screen captured from it" "yes" "$seen"

    bare="${pane_target%:}"
    for sub in capture-pane send-keys; do
        rc=0
        if [ "$sub" = capture-pane ]; then
            out="$(ccy_tmux capture-pane -p -t "$bare" 2>&1)" || rc=$?
        else
            out="$(ccy_tmux send-keys -t "$bare" -l x 2>&1)" || rc=$?
        fi
        refused=no
        if [ "$rc" -ne 0 ] && [[ "$out" == *"can't find pane"* ]]; then
            refused=yes
        fi
        check "$sub -t $bare (no colon) is refused: can't find pane" "yes" "$refused${out:+ ($out)}"
    done
fi

echo ""
echo "ccy tmux targets, against $(tmux -V)"
printf 'passed: %d   failed: %d\n' "$passed" "$failed"
if [ "$passed" -eq 0 ]; then
    echo "ERROR: zero tests ran — discovery is broken, not the code clean" >&2
    exit 1
fi
if [ "$failed" -ne 0 ]; then
    exit 1
fi
echo "OK"
