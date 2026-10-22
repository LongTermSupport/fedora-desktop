#!/usr/bin/env bash
# Unit-test how ccy hands the caller's exported GH_TOKEN to the launcher it re-runs inside
# tmux (files/var/local/claude-yolo/lib/token-handoff.bash, with the terminal insulation in
# lib/tmux-session.bash).
#
# WHY THIS EXISTS. The launcher takes the export off GH_TOKEN at its first line, so no program
# it starts is given the token. But an interactive launch re-runs the launcher inside a tmux
# session, and that inner launcher is where GitHub is set up: with the export gone it never
# saw the token, fell back to gh's login and, on a box with no login, told the user to export
# GH_TOKEN, which they had done. Exporting it again is no answer: tmux's server keeps the
# environment of the client that started it and gives it to every later session, so one
# launch's token would reach another's panes. So the outer launcher writes the token to a
# fresh 0600 file in a 0700 directory of its own, gives the inner launcher only the file's
# path, on the pane's command, and the inner one reads it and deletes it before anything
# else. These cases drive the launcher's own entry point (read from the launcher, so this
# tests what it runs) under `script`, because insulation applies only on a terminal, with a
# fake tmux that runs the pane's command and logs what it was given.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

for lib in common-pure.bash session-registry.bash tmux-session.bash; do
    if [ ! -f "$LIB_DIR/$lib" ]; then
        echo "FAIL: $lib not found at $LIB_DIR/$lib" >&2
        exit 1
    fi
done
if [ -z "$(command -v script)" ]; then
    echo "FAIL: script (util-linux) is needed to give the launcher a terminal" >&2
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

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/token-handoff-test.XXXXXX")"
cleanup() { rm -rf "$SCRATCH"; }
trap cleanup EXIT

TOKEN="handoff-test-token-$$-not-a-real-one"

echo "=== the launcher's entry point ==="
# The call the launcher wraps a session with, and whether it loads the hand-off library.
ENTRY_CALL=" \"\$PROJECT_NAME\" \"\$SCRIPT_DIR/claude-yolo\" \"\$@\" || exit 1"
ENTRY="$(grep -F -- "$ENTRY_CALL" "$LAUNCHER" | awk '{ print $1 }')"
check "the launcher has one terminal-insulation entry point" "1" "$(grep -c . <<<"$ENTRY")"
LIBS=(common-pure session-registry tmux-session)
if grep -qE '^CCY_LIBS=\((.* )?token-handoff( |\))' "$LAUNCHER"; then
    LIBS=(token-handoff "${LIBS[@]}")
fi
check "the launcher loads lib/token-handoff.bash" "yes" \
    "$( [[ " ${LIBS[*]} " == *" token-handoff "* ]] && echo yes || echo no)"
# The outer launcher's EXIT trap does not survive its exec of tmux attach-session, so the
# inner launcher must take the file before anything that can end it: the first library it
# sources, ahead of common.bash and of the check that every library is present.
first_source="$(grep -nE "^source \"\\\$SCRIPT_DIR/lib/" "$LAUNCHER" | head -n 1)"
presence_check="$(grep -nF 'Required library not found' "$LAUNCHER" | head -n 1 | cut -d: -f1)"
check "and sources it first, before common.bash and the library presence check" "yes" \
    "$( [[ "$first_source" == *lib/token-handoff.bash* ]] && [ "${first_source%%:*}" -lt "${presence_check:-0}" ] \
        && echo yes || echo no)"

# ── the fakes ─────────────────────────────────────────────────────────────────────────
BIN="$SCRATCH/bin"
mkdir -p "$BIN"
export T_LOG="$SCRATCH/log" T_STATE="$SCRATCH/tmux-sessions" T_LIB="$LIB_DIR" T_LIBS="${LIBS[*]}"
export T_INNER="$SCRATCH/inner-launcher" T_TOKEN="$TOKEN" T_FZF=esc

# tmux: every call logs its argv and whether GH_TOKEN or the hand-off variable is in its
# environment (a server it started would keep them for every pane). new-session runs the
# pane's command at once, after noting the modes of the hand-off file it names.
cat >"$BIN/tmux" <<'EOF'
#!/usr/bin/env bash
{ printf 'ARGV'; printf ' %q' "$@"; printf '\n'
  printf 'ENV GH_TOKEN=%s HANDOFF=%s\n' "${GH_TOKEN-unset}" "${CCY_GH_TOKEN_HANDOFF-unset}"; } >>"$T_LOG"
[ "$1" = -L ] && shift 2
case "$1" in
list-sessions) [ -f "$T_STATE" ] && cat "$T_STATE"; exit 0 ;;
list-clients) echo "can't find session: $3" >&2; exit 1 ;;
new-session)
    while [ "$#" -gt 0 ] && [ "$1" != -- ]; do shift; done
    shift
    pane=()
    while [ "$#" -gt 0 ] && [ "$1" != ';' ]; do pane+=("$1"); shift; done
    for a in "${pane[@]}"; do
        case "$a" in CCY_GH_TOKEN_HANDOFF=*)
            f="${a#*=}"
            printf 'MODES file=%s dir=%s\n' "$(stat -c %a "$f")" "$(stat -c %a "$(dirname "$f")")" >>"$T_LOG" ;;
        esac
    done
    "${pane[@]}" </dev/null >>"$T_LOG" 2>&1
    exit 0 ;;
attach-session) [ -f "$T_STATE" ] && exit 1; exit 0 ;;
esac
exit 0
EOF
cat >"$BIN/systemd-run" <<'EOF'
#!/usr/bin/env bash
while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
shift
exec "$@"
EOF
cat >"$BIN/systemd-escape" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "${!#}"
EOF
# fzf: Esc, or the first row offered.
cat >"$BIN/fzf" <<'EOF'
#!/usr/bin/env bash
first="$(head -n 1)"
[ "$T_FZF" = esc ] && exit 130
printf '%s\n' "$first"
EOF
# The launcher re-run inside the session: its first line, the libraries, then what it holds.
cat >"$T_INNER" <<'EOF'
#!/usr/bin/env bash
export -n GH_TOKEN CLAUDE_CODE_OAUTH_TOKEN CLAUDE_OAUTH_TOKEN
for lib in $T_LIBS; do
    # shellcheck source=/dev/null
    source "$T_LIB/$lib.bash" || { echo "LOAD failed at $lib"; exit 1; }
done
printf 'INNER GH_TOKEN=%s HANDOFF=%s CHILD=%s\n' "${GH_TOKEN-unset}" "${CCY_GH_TOKEN_HANDOFF-unset}" \
    "$(bash -c 'printf %s "${GH_TOKEN-unset}/${CCY_GH_TOKEN_HANDOFF-unset}"')"
EOF
# The outer launcher: the libraries, the caller's token as the launcher holds it after its
# first line (a plain variable), then the entry point.
DRIVER="$SCRATCH/driver.bash"
cat >"$DRIVER" <<'EOF'
#!/usr/bin/env bash
for lib in $T_LIBS; do
    # shellcheck source=/dev/null
    source "$T_LIB/$lib.bash" || exit 1
done
GH_TOKEN="$T_CALLER_TOKEN"
"$1" proj "$T_INNER" --no-ssh
rc=$?
printf 'RETURNED rc=%s GH_TOKEN=%s\n' "$rc" "${GH_TOKEN-unset}" >>"$T_LOG"
exit "$rc"
EOF
chmod 755 "$BIN"/* "$T_INNER"

PROJ="$SCRATCH/project"
RUNTIME="$SCRATCH/run"
mkdir -p "$PROJ" "$RUNTIME"
chmod 700 "$RUNTIME"

# drive <terminal|none> <caller-token> — run the outer launcher; stdout+stderr in $OUT.
drive() {
    local how="$1"
    rm -f "$T_LOG"
    : >"$T_LOG"
    local -a env=(env -u TMUX -u TMUX_PANE -u GH_TOKEN PATH="$BIN:$PATH" XDG_RUNTIME_DIR="$RUNTIME"
        CCY_STATE_DIR="$SCRATCH/state" T_CALLER_TOKEN="$2")
    if [ "$how" = terminal ]; then
        OUT="$(cd "$PROJ" && "${env[@]}" script -qec "bash $(printf '%q' "$DRIVER") $ENTRY" /dev/null </dev/null 2>&1)"
    else
        OUT="$(cd "$PROJ" && "${env[@]}" bash "$DRIVER" "$ENTRY" </dev/null 2>&1)"
    fi
}
line() { grep -m1 "^$1" "$T_LOG"; }
argv_leaks() { grep '^ARGV' "$T_LOG" | grep -cF -- "$TOKEN"; }
env_lines_given() { grep '^ENV' "$T_LOG" | grep -vc '^ENV GH_TOKEN=unset HANDOFF=unset$'; }
left_in_runtime() { find "$RUNTIME" -mindepth 1 | grep -c .; }

echo ""
echo "=== a new session: the inner launcher gets the caller's token, tmux does not ==="
rm -f "$T_STATE"
drive terminal "$TOKEN"
check "the inner launcher holds the caller's GH_TOKEN, and no child of it does" \
    "INNER GH_TOKEN=$TOKEN HANDOFF=unset CHILD=unset/unset" "$(line INNER)"
check "the token is in no tmux or systemd-run argv" "0" "$(argv_leaks)"
check "no tmux call has GH_TOKEN or the hand-off path in its environment" "0" "$(env_lines_given)"
check "the hand-off file is 0600 in a 0700 directory" "MODES file=600 dir=700" "$(line MODES)"
check "and nothing is left in the runtime directory" "0" "$(left_in_runtime)"

echo ""
echo "=== Esc at the offer: the outer launcher exits and removes the file ==="
printf 'ccy-proj 0 %s\n' "$PROJ" >"$T_STATE"
T_FZF=esc drive terminal "$TOKEN"
check "no inner launcher ran" "" "$(line INNER)"
check "nothing is left in the runtime directory" "0" "$(left_in_runtime)"

echo ""
echo "=== an attach refused: the launcher runs in this terminal, with the token ==="
printf 'ccy-proj 0 %s\n' "$PROJ" >"$T_STATE"
export T_FZF=first
drive terminal "$TOKEN"
export T_FZF=esc
check "the launcher it runs instead holds the caller's GH_TOKEN" \
    "INNER GH_TOKEN=$TOKEN HANDOFF=unset CHILD=unset/unset" "$(tr -d '\r' <<<"$OUT" | grep -m1 '^INNER')"
check "and nothing is left in the runtime directory" "0" "$(left_in_runtime)"

echo ""
echo "=== no terminal: no session, and the launcher keeps its token ==="
rm -f "$T_STATE"
drive none "$TOKEN"
check "the entry point returns with GH_TOKEN still set" "RETURNED rc=0 GH_TOKEN=$TOKEN" "$(line RETURNED)"
check "no session was started" "0" "$(grep -c '^ARGV.* new-session ' "$T_LOG")"
check "nothing is left in the runtime directory" "0" "$(left_in_runtime)"

echo ""
echo "=== no token: nothing is written, and the inner launcher asks gh as before ==="
drive terminal ""
check "the inner launcher has no GH_TOKEN" "INNER GH_TOKEN=unset HANDOFF=unset CHILD=unset/unset" "$(line INNER)"
check "no hand-off file was named" "" "$(line MODES)"

echo ""
echo "=== the session record names the Go front, not the bash it hands over to ==="
# Deployed, /var/local/claude-yolo/claude-yolo is the Go front and the bash is claude-yolo.bash
# beside it, run by the front with its own path as $0. A record, and the pane, must name the
# front, or every restore and team restart would skip it. The launcher's own SCRIPT_DIR line
# and insulation line run here from a file called claude-yolo.bash, with a front beside it.
INSTALL="$SCRATCH/install"
mkdir -p "$INSTALL"
{
    printf '#!/usr/bin/env bash\n'
    grep -m1 '^SCRIPT_DIR=' "$LAUNCHER"
    cat <<'EOF'
for lib in $T_LIBS; do source "$T_LIB/$lib.bash" || exit 1; done
PROJECT_NAME=proj
set -- --no-ssh
EOF
    grep -E '^ +ccy_token_handoff_insulate ' "$LAUNCHER"
} >"$INSTALL/claude-yolo.bash"
cat >"$INSTALL/claude-yolo" <<'EOF'
#!/usr/bin/env bash
for lib in $T_LIBS; do
    # shellcheck source=/dev/null
    source "$T_LIB/$lib.bash" || exit 1
done
printf 'FRONT %s\n' "$(grep -h '^launcher=' "$CCY_STATE_DIR"/sessions/*)"
EOF
chmod 755 "$INSTALL/claude-yolo" "$INSTALL/claude-yolo.bash"
rm -f "$T_STATE"
: >"$T_LOG"
OUT="$(cd "$PROJ" && env -u TMUX -u TMUX_PANE -u GH_TOKEN PATH="$BIN:$PATH" XDG_RUNTIME_DIR="$RUNTIME" \
    CCY_STATE_DIR="$SCRATCH/front-state" \
    script -qec "bash $(printf '%q' "$INSTALL/claude-yolo.bash")" /dev/null </dev/null 2>&1)"
check "the record's launcher is the front" "FRONT launcher=$INSTALL/claude-yolo" "$(line FRONT)"
check "and the pane runs the front" "1" \
    "$(grep '^ARGV.* new-session ' "$T_LOG" | grep -cF -- "ccy-tmux $INSTALL/claude-yolo --no-ssh")"

echo ""
echo "=== a runtime directory that is not there fails the launch, starting nothing ==="
: >"$T_LOG"
OUT="$(cd "$PROJ" && env -u TMUX -u TMUX_PANE PATH="$BIN:$PATH" XDG_RUNTIME_DIR="$SCRATCH/no-such-run" \
    CCY_STATE_DIR="$SCRATCH/state" T_CALLER_TOKEN="$TOKEN" \
    script -qec "bash $(printf '%q' "$DRIVER") $ENTRY" /dev/null </dev/null 2>&1)"
check "the entry point fails" "rc=1" "$(grep -o 'RETURNED rc=[0-9]*' "$T_LOG" | cut -d' ' -f2)"
check "saying where it could not write" "yes" \
    "$(grep -qF "$SCRATCH/no-such-run" <<<"$OUT" && echo yes || echo no)"
check "and no session was started" "0" "$(grep -c '^ARGV.* new-session ' "$T_LOG")"

echo ""
echo "=== the inner side refuses a hand-off it cannot trust ==="
# Only a file the outer launcher made is taken: <runtime>/ccy-gh-token.XXXXXX/token, the
# directory the user's, 0700 and no link, the file the user's, 0600, one link and no link.
# Anything else is refused loudly and left where it is, and the launch ends.
# take <path> — load the libraries as a launcher given that hand-off path does.
take() {
    (cd "$PROJ" && env -u GH_TOKEN XDG_RUNTIME_DIR="$RUNTIME" CCY_GH_TOKEN_HANDOFF="$1" bash "$T_INNER" 2>&1)
    echo "rc=$?"
}
# handoff <suffix> <dir-mode> <file-mode> — a hand-off directory and file in the runtime one.
handoff() {
    mkdir -p "$RUNTIME/ccy-gh-token.$1"
    printf '%s\n' "$TOKEN" >"$RUNTIME/ccy-gh-token.$1/token"
    chmod "$3" "$RUNTIME/ccy-gh-token.$1/token"
    chmod "$2" "$RUNTIME/ccy-gh-token.$1"
    printf '%s' "$RUNTIME/ccy-gh-token.$1/token"
}
# refused <label> <path> <out> — the launch ended, naming the variable, the token unread.
refused() {
    check "$1 is refused, ending the launch" "rc=1" "$(tail -n 1 <<<"$3")"
    check "  with an error naming CCY_GH_TOKEN_HANDOFF and the path" "yes" \
        "$(errors="$(grep -F 'ERROR' <<<"$3")"; [[ "$errors" == *"CCY_GH_TOKEN_HANDOFF=$2"* ]] && echo yes || echo no)"
    check "  and nothing ran with the token" "0" "$(grep -c "^INNER" <<<"$3")"
    if [ -e "$2" ]; then
        check "  and the file is left in place" "$TOKEN" "$(cat "$2")"
    fi
}
printf '%s\n' "$TOKEN" >"$SCRATCH/real"
chmod 600 "$SCRATCH/real"
refused "a file of the user's outside the pattern" "$SCRATCH/real" "$(take "$SCRATCH/real")"
ln -s "$SCRATCH/real" "$SCRATCH/link"
refused "a symlink" "$SCRATCH/link" "$(take "$SCRATCH/link")"
refused "a missing file" "$SCRATCH/missing" "$(take "$SCRATCH/missing")"
mkdir -p "$RUNTIME/other.abcdef"
chmod 700 "$RUNTIME/other.abcdef"
printf '%s\n' "$TOKEN" >"$RUNTIME/other.abcdef/token"
chmod 600 "$RUNTIME/other.abcdef/token"
refused "a file in a runtime subdirectory not made by ccy" "$RUNTIME/other.abcdef/token" \
    "$(take "$RUNTIME/other.abcdef/token")"
mkdir -p "$SCRATCH/elsewhere"
chmod 700 "$SCRATCH/elsewhere"
printf '%s\n' "$TOKEN" >"$SCRATCH/elsewhere/token"
chmod 600 "$SCRATCH/elsewhere/token"
ln -s "$SCRATCH/elsewhere" "$RUNTIME/ccy-gh-token.Linked"
refused "a hand-off directory that is a symlink" "$RUNTIME/ccy-gh-token.Linked/token" \
    "$(take "$RUNTIME/ccy-gh-token.Linked/token")"
check "  and the directory it points at keeps its file" "$TOKEN" "$(cat "$SCRATCH/elsewhere/token")"
f="$(handoff Mode64 700 644)"
refused "a hand-off file of mode 0644" "$f" "$(take "$f")"
f="$(handoff Dir755 755 600)"
refused "a hand-off directory of mode 0755" "$f" "$(take "$f")"
f="$(handoff TwoLnk 700 600)"
ln "$f" "$SCRATCH/second-link"
refused "a hand-off file with a second hard link" "$f" "$(take "$f")"
f="$(handoff Dotdot 700 600)"
refused "a path that climbs out with .." "$RUNTIME/ccy-gh-token.Dotdot/../ccy-gh-token.Dotdot/token" \
    "$(take "$RUNTIME/ccy-gh-token.Dotdot/../ccy-gh-token.Dotdot/token")"
# A relative path is resolved from the launch's directory, never from the runtime one.
mkdir -p "$PROJ/abcdef" "$PROJ/ccy-gh-token.Relatv"
for d in abcdef ccy-gh-token.Relatv; do
    printf '%s\n' "$TOKEN" >"$PROJ/$d/token"
    chmod 600 "$PROJ/$d/token"
    chmod 700 "$PROJ/$d"
done
out="$(take abcdef/token)"
refused "a relative XXXXXX/token" abcdef/token "$out"
check "  and the file in the launch's directory survives" "$TOKEN" "$(cat "$PROJ/abcdef/token")"
out="$(take ccy-gh-token.Relatv/token)"
refused "a relative ccy-gh-token.XXXXXX/token" ccy-gh-token.Relatv/token "$out"
check "  and the file in the launch's directory survives" "$TOKEN" "$(cat "$PROJ/ccy-gh-token.Relatv/token")"
rm -rf "$PROJ/abcdef" "$PROJ/ccy-gh-token.Relatv"
mkdir -p "$SCRATCH/other-base/ccy-gh-token.OtherB"
printf '%s\n' "$TOKEN" >"$SCRATCH/other-base/ccy-gh-token.OtherB/token"
chmod 600 "$SCRATCH/other-base/ccy-gh-token.OtherB/token"
chmod 700 "$SCRATCH/other-base/ccy-gh-token.OtherB"
refused "a hand-off shaped file under a base that is no runtime or temporary directory" \
    "$SCRATCH/other-base/ccy-gh-token.OtherB/token" "$(take "$SCRATCH/other-base/ccy-gh-token.OtherB/token")"
# An empty file is ccy's in shape but holds no token: refused loudly, and removed.
mkdir -p "$RUNTIME/ccy-gh-token.Empty1"
: >"$RUNTIME/ccy-gh-token.Empty1/token"
chmod 600 "$RUNTIME/ccy-gh-token.Empty1/token"
chmod 700 "$RUNTIME/ccy-gh-token.Empty1"
out="$(take "$RUNTIME/ccy-gh-token.Empty1/token")"
check "an empty hand-off file ends the launch" "rc=1" "$(tail -n 1 <<<"$out")"
check "  saying it is empty" "yes" \
    "$(errors="$(grep -F 'ERROR' <<<"$out")"; [[ "$errors" == *"$RUNTIME/ccy-gh-token.Empty1/token"*empty* ]] && echo yes || echo no)"
check "  and nothing ran" "0" "$(grep -c "^INNER" <<<"$out")"
check "  and it and its directory are gone" "no" "$( [ -e "$RUNTIME/ccy-gh-token.Empty1" ] && echo yes || echo no)"
# A file holding only a newline is as empty as an empty one: the same error, the same removal.
mkdir -p "$RUNTIME/ccy-gh-token.Newlin"
printf '\n' >"$RUNTIME/ccy-gh-token.Newlin/token"
chmod 600 "$RUNTIME/ccy-gh-token.Newlin/token"
chmod 700 "$RUNTIME/ccy-gh-token.Newlin"
out="$(take "$RUNTIME/ccy-gh-token.Newlin/token")"
check "a newline-only hand-off file ends the launch" "rc=1" "$(tail -n 1 <<<"$out")"
check "  saying it is empty" "yes" \
    "$(errors="$(grep -F 'ERROR' <<<"$out")"; [[ "$errors" == *"$RUNTIME/ccy-gh-token.Newlin/token"*empty* ]] && echo yes || echo no)"
check "  and it and its directory are gone" "no" "$( [ -e "$RUNTIME/ccy-gh-token.Newlin" ] && echo yes || echo no)"
# The user is what id -u says, not the EUID an environment exports (bash takes EUID from it).
# take_as <euid> <path> — as take, with that EUID exported.
take_as() {
    (cd "$PROJ" && env -u GH_TOKEN EUID="$1" XDG_RUNTIME_DIR="$RUNTIME" CCY_GH_TOKEN_HANDOFF="$2" bash "$T_INNER" 2>&1)
    echo "rc=$?"
}
REAL_UID="$(id -u)"
f="$(handoff Forgd1 700 600)"
out="$(take_as $((REAL_UID + 1)) "$f")"
check "a genuine hand-off is taken whatever EUID the environment exports" "1 0" \
    "$(grep -c "^INNER" <<<"$out") $(grep -c 'CCY_GH_TOKEN_HANDOFF' <<<"$out")"
check "  and it is gone" "no" "$( [ -e "$f" ] && echo yes || echo no)"
if [ "$REAL_UID" = 0 ]; then
    f="$(handoff Forgd2 700 600)"
    chown -R 65534 "$RUNTIME/ccy-gh-token.Forgd2"
    refused "another user's hand-off under a forged EUID" "$f" "$(take_as 65534 "$f")"
fi
# The pane's environment is the tmux server's, not the outer launcher's: a hand-off made
# under TMPDIR with no runtime directory is still taken by a launcher that has one.
ALT_TMP="$SCRATCH/alt-tmp"
mkdir -p "$ALT_TMP"
chmod 700 "$ALT_TMP"
# give-only: the outer launcher's give, its EXIT trap cleared as its exec of tmux clears it.
cat >"$SCRATCH/give-only" <<'EOF'
#!/usr/bin/env bash
source "$T_LIB/token-handoff.bash" && ccy_token_handoff_give && trap - EXIT && printf %s "$CCY_GH_TOKEN_HANDOFF"
EOF
f="$(cd "$PROJ" && env -u XDG_RUNTIME_DIR TMPDIR="$ALT_TMP" GH_TOKEN="$TOKEN" bash "$SCRATCH/give-only")"
check "a hand-off made under TMPDIR, with no runtime directory, is in TMPDIR" "yes" \
    "$( [[ "$f" == "$ALT_TMP/ccy-gh-token."??????/token ]] && [ -f "$f" ] && echo yes || echo no)"
out="$(cd "$PROJ" && env -u GH_TOKEN XDG_RUNTIME_DIR="$RUNTIME" TMPDIR="$ALT_TMP" CCY_GH_TOKEN_HANDOFF="$f" \
    bash "$T_INNER" 2>&1)"
check "  is taken by a launcher whose environment has a runtime directory" \
    "INNER GH_TOKEN=$TOKEN HANDOFF=unset CHILD=unset/unset" "$(grep -m1 '^INNER' <<<"$out")"
check "  and it and its directory are gone" "0" "$(find "$ALT_TMP" -mindepth 1 | grep -c .)"
f="$(handoff GoodOn 700 600)"
out="$(take "$f")"
check "a hand-off file the launcher made is taken" \
    "INNER GH_TOKEN=$TOKEN HANDOFF=unset CHILD=unset/unset" "$(grep -m1 '^INNER' <<<"$out")"
check "  and it and its directory are gone" "no" "$( [ -e "$RUNTIME/ccy-gh-token.GoodOn" ] && echo yes || echo no)"
find "$RUNTIME" -mindepth 1 -delete

echo ""
echo "=== the real launcher takes the file before anything that can end it ==="
# A new session's outer launcher has exec'd tmux, so its EXIT trap is gone; the inner one
# must take (and delete) the file even when it then fails: a library is missing here, and
# common.bash, which runs before the library check, can fail first (no container engine).
COPY="$SCRATCH/launcher-copy"
mkdir -p "$COPY"
cp -r "$REPO_ROOT/files/var/local/claude-yolo/lib" "$COPY/lib"
cp "$LAUNCHER" "$COPY/claude-yolo"
rm "$COPY/lib/agent-bus-seats.bash"
# real <handoff-path> — the copied launcher given that path; its output, then rc=N.
real() {
    (cd "$PROJ" && env -u GH_TOKEN XDG_RUNTIME_DIR="$RUNTIME" CCY_GH_TOKEN_HANDOFF="$1" \
        bash "$COPY/claude-yolo" --version 2>&1)
    echo "rc=$?"
}
out="$(real "$(handoff RealLn 700 600)")"
check "the launcher fails" "rc=1" "$(tail -n 1 <<<"$out")"
check "  having taken the hand-off file first" "0" "$(left_in_runtime)"
f="$(handoff RealNo 700 644)"
out="$(real "$f")"
check "a hand-off it refuses ends the real launcher" "rc=1" "$(tail -n 1 <<<"$out")"
check "  before anything else, naming the variable" "yes" \
    "$(errors="$(grep -F 'ERROR' <<<"$out")"; [[ "$errors" == "ERROR: CCY_GH_TOKEN_HANDOFF=$f"* ]] && echo yes || echo no)"
find "$RUNTIME" -mindepth 1 -delete

echo ""
echo "=== --debug: the log holds no token, and only its owner can read it ==="
# The launcher's own lines that start the debug log, run as it runs them, then set -x over
# the hand-off (any token shape) and lines shaped like the launcher's other token lines (a
# GitHub and a Claude token, as gh and the token store give them).
DEBUG_SETUP="$SCRATCH/debug-setup.bash"
grep -E '^ *(DEBUG_LOG=|exec > >\(tee)' "$LAUNCHER" >"$DEBUG_SETUP"
check "the launcher starts its debug log in two lines" "2" "$(grep -c . "$DEBUG_SETUP")"
GH_SHAPED="gh""o_$(printf 'Q%.0s' {1..36})"
CLAUDE_SHAPED="sk-""ant-oat01-$(printf 'R%.0s' {1..40})"
DEBUG_TMP="$SCRATCH/debug-tmp"
mkdir -p "$DEBUG_TMP"
cat >"$SCRATCH/debug-driver.bash" <<'EOF'
#!/usr/bin/env bash
source "$T_LIB/token-handoff.bash" || exit 1
# The common umask, under which a plain redirect makes a file anyone can read.
umask 022
TMPDIR="$T_DEBUG_TMP"
# shellcheck source=/dev/null
source "$T_DEBUG_SETUP"
printf '%s\n' "$DEBUG_LOG" >"$T_DEBUG_TMP/name"
GH_TOKEN="$T_CALLER_TOKEN"
read -r GH_SHAPED CLAUDE_OAUTH_TOKEN <"$T_DEBUG_TMP/shaped"
set -x
ccy_token_handoff_give && ccy_token_handoff_take
[ -n "${GH_TOKEN:+set}" ]
CLAUDE_CODE_OAUTH_TOKEN="$CLAUDE_OAUTH_TOKEN" GH_TOKEN="$GH_SHAPED" true
set +x
echo "DEBUG-DRIVER-END"
EOF
printf '%s %s\n' "$GH_SHAPED" "$CLAUDE_SHAPED" >"$DEBUG_TMP/shaped"
(cd "$PROJ" && env XDG_RUNTIME_DIR="$RUNTIME" T_DEBUG_TMP="$DEBUG_TMP" T_DEBUG_SETUP="$DEBUG_SETUP" \
    T_CALLER_TOKEN="$TOKEN" bash "$SCRATCH/debug-driver.bash" >"$DEBUG_TMP/out" 2>&1)
DEBUG_LOG_FILE="$DEBUG_TMP/no-name-written"
[ ! -f "$DEBUG_TMP/name" ] || DEBUG_LOG_FILE="$(cat "$DEBUG_TMP/name")"
for _ in $(seq 50); do
    grep -qs 'DEBUG-DRIVER-END' "$DEBUG_LOG_FILE" && break
    sleep 0.1
done
check "the debug log was written" "yes" "$(grep -qs 'DEBUG-DRIVER-END' "$DEBUG_LOG_FILE" && echo yes || echo no)"
check "  and traced the hand-off" "yes" "$(grep -qs 'ccy_token_handoff_take' "$DEBUG_LOG_FILE" && echo yes || echo no)"
check "  holding no hand-off token" "0" "$(grep -csF -- "$TOKEN" "$DEBUG_LOG_FILE")"
check "  no GitHub-shaped token" "0" "$(grep -csF -- "$GH_SHAPED" "$DEBUG_LOG_FILE")"
check "  no Claude-shaped token" "0" "$(grep -csF -- "$CLAUDE_SHAPED" "$DEBUG_LOG_FILE")"
check "  readable only by its owner" "600" "$(stat -c %a "$DEBUG_LOG_FILE" 2>&1)"
find "$RUNTIME" -mindepth 1 -delete

echo ""
echo "=== the custom-Dockerfile modes re-run the launcher with the token ==="
# Those modes run before insulation and end in exec, which gives the new launcher only what
# is exported. Each exec must set the token in its own environment.
DFC="$LIB_DIR/dockerfile-custom.bash"
read -r EXEC_CALL <<'EOF'
exec "$script_path"
EOF
read -r EXEC_GIVEN <<'EOF'
GH_TOKEN="${GH_TOKEN:-}" exec "$script_path"
EOF
check "dockerfile-custom.bash re-runs the launcher by exec four times" "4" "$(grep -cF -- "$EXEC_CALL" "$DFC")"
check "and every one is given the caller's GH_TOKEN" "4" "$(grep -cF -- "$EXEC_GIVEN" "$DFC")"
# What they re-run is the Go front beside the launcher, never $0 (deployed, claude-yolo.bash).
read -r FRONT_ARG <<'EOF'
"$SCRIPT_DIR/claude-yolo" ".claude/ccy" "ccy"
EOF
read -r DOLLAR0_ARG <<'EOF'
"$0" ".claude/ccy" "ccy"
EOF
check "the launcher gives both modes the front to re-run, not \$0" "2 0" \
    "$(grep -cF -- "$FRONT_ARG" "$LAUNCHER") $(grep -cF -- "$DOLLAR0_ARG" "$LAUNCHER")"
cat >"$SCRATCH/show-token" <<'EOF'
#!/usr/bin/env bash
printf '%s' "${GH_TOKEN-unset}"
EOF
cat >"$SCRATCH/exec-prefixed" <<'EOF'
#!/usr/bin/env bash
export -n GH_TOKEN
GH_TOKEN="${GH_TOKEN:-}" exec "$1"
EOF
chmod 755 "$SCRATCH/show-token" "$SCRATCH/exec-prefixed"
check "a prefix on exec reaches the program bash runs, with the variable unexported" "$TOKEN" \
    "$(env GH_TOKEN="$TOKEN" "$SCRATCH/exec-prefixed" "$SCRATCH/show-token")"

echo ""
echo "passed: $passed"
echo "failed: $failed"
[ "$failed" -eq 0 ]
