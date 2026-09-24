#!/usr/bin/env bash
# Unit-test the Ctrl+R history search (Plan 00138): the ranker
# (files/home/.local/bin/bash-history-rank) and the recorder and key binding
# (files/home/bashrc-includes/history-search.bash).
#
# WHAT IS AT STAKE. The recorder runs at every prompt of every terminal, so a defect is
# either silent data corruption (commands filed under the wrong directory, a secret the
# user kept out of history written anyway) or a broken prompt. The cases below drive a
# real interactive bash, because PROMPT_COMMAND, history numbering, HISTCONTROL and
# HISTIGNORE only behave as they do on a host inside one.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RANKER="$REPO_ROOT/files/home/.local/bin/bash-history-rank"
INCLUDE="$REPO_ROOT/files/home/bashrc-includes/history-search.bash"

for f in "$RANKER" "$INCLUDE"; do
    if [ ! -f "$f" ]; then
        echo "FAIL: $f does not exist" >&2
        exit 1
    fi
done
if ! command -v gawk >/dev/null; then
    echo "FAIL: gawk not found — the ranker needs it and this gate cannot pass without it." >&2
    exit 1
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

# The `$` is assembled so shellcheck does not read the inner shell's expansions as this
# script's own unquoted ones.
dollar='$'

# ── the ranker ───────────────────────────────────────────────────────────────────────
echo "== ranker"
repo="$WORK_DIR/repo"
mkdir -p "$repo/sub" "$WORK_DIR/elsewhere"
git -C "$repo" init --quiet
context="$WORK_DIR/rank-context"
history="$WORK_DIR/rank-history"
{
    printf '1\t0\t%s\t%s\0' "$WORK_DIR/elsewhere" "cmd-elsewhere-new"
    printf '2\t0\t%s\t%s\0' "$repo" "cmd-repo-root"
    printf '3\t0\t%s\t%s\0' "$repo/sub" "cmd-here-old"
    printf '4\t1\t%s\t%s\0' "$repo/sub" "cmd-here-always-fails"
    printf '5\t0\t%s\t%s\0' "$repo/sub" "cmd-here-new"
    printf '6\t0\t%s\t%s\0' "$WORK_DIR/elsewhere" "cmd-also-here"
    printf '7\t0\t%s\t%s\0' "$repo/sub" "cmd-also-here"
    printf '8\t0\t%s\t%s\0' "$WORK_DIR/elsewhere" "cmd-elsewhere-newest"
    printf '9\t0\t%s\t%s\0' "$WORK_DIR/elsewhere" "cmd	with	tabs"
} >"$context"
printf '%s\n' "cmd-history-only" "#1700000000" "cmd-multi-line" "second line" "#1700000001" "cmd-here-old" >"$history"

mapfile -d '' ranked < <("$RANKER" "$context" "$history" "$repo/sub")
check "this directory first, newest first" \
    "cmd-also-here|cmd-here-new|cmd-here-old" "${ranked[0]-}|${ranked[1]-}|${ranked[2]-}"
check "a command that always failed sinks to the bottom of its tier" "cmd-here-always-fails" "${ranked[3]-}"
check "the same repository comes next" "cmd-repo-root" "${ranked[4]-}"
check "everything else follows, newest first, tabs intact" \
    "cmd	with	tabs|cmd-elsewhere-newest" "${ranked[5]-}|${ranked[6]-}"
multi_found=no
for entry in "${ranked[@]}"; do
    if [ "$entry" = $'cmd-multi-line\nsecond line' ]; then multi_found=yes; fi
done
check "a multi-line entry stays one entry" "yes" "$multi_found"
check "history recorded before the recorder existed is still searchable" "1" \
    "$(printf '%s\n' "${ranked[@]}" | grep -c '^cmd-history-only$')"
check "each command appears once" "${#ranked[@]}" \
    "$(printf '%s\0' "${ranked[@]}" | sort -zu | tr -cd '\0' | wc -c)"

outside="$(mktemp -d)"
mapfile -d '' ranked_outside < <("$RANKER" "$context" "$history" "$outside")
check "outside any repository, nothing is promoted to the repository tier" \
    "cmd	with	tabs" "${ranked_outside[0]-}"
rmdir "$outside"

# rc_and_stderr <cmd...> — "<exit status>:<whether stderr said anything>"; stdout is the
# payload and must be empty on a refusal.
rc_and_stderr() {
    local err rc
    err="$("$@" 2>&1 1>"$WORK_DIR/refusal-stdout")"
    rc=$?
    printf '%s:%s:%s' "$rc" "$([ -n "$err" ] && echo explained || echo silent)" "$(wc -c <"$WORK_DIR/refusal-stdout")"
}
check "a wrong argument count is a usage error, explained, with nothing on stdout" \
    "64:explained:0" "$(rc_and_stderr "$RANKER" "$context")"
check "a missing input file is refused, not ranked as empty" \
    "66:explained:0" "$(rc_and_stderr "$RANKER" "$WORK_DIR/no-such-file" "$history" "$repo")"

# ── the recorder, in a real interactive shell ────────────────────────────────────────
echo "== recorder"
home="$WORK_DIR/home"
state="$home/.local/state/bash"
mkdir -p "$state" "$WORK_DIR/start" "$WORK_DIR/A" "$WORK_DIR/B" "$WORK_DIR/bin"
chmod 0700 "$state"
# The include calls the ranker by name, as ~/.local/bin puts it on PATH on a host.
ln -s "$RANKER" "$WORK_DIR/bin/bash-history-rank"

# run_shell <home> <cwd> <script> — an interactive bash reading the script from a pipe,
# with none of the harness's own dotfiles. Its stdout, where the PROBE lines go.
run_shell() {
    local shell_home="$1" cwd="$2" script="$3"
    (cd "$cwd" && printf '%s\n' "$script" |
        HOME="$shell_home" PATH="$WORK_DIR/bin:$PATH" bash --norc --noprofile -i 2>/dev/null)
}

# probe_status stands in for bash-git-prompt's setLastCommandState, which sits in the
# array BEFORE the recorder and returns 0: the recorder must still record the command's
# own status (bash hands each element the command's $?), and must not disturb the probe.
recorder_script="$(cat <<EOF
HISTFILE=$state/history; HISTCONTROL=ignoreboth; HISTIGNORE=ls; HISTTIMEFORMAT='%F %T  '; seen_status=unset; probe_status() { seen_status=${dollar}?; }; PROMPT_COMMAND=(probe_status); source $INCLUDE
cd $WORK_DIR/A
echo one
 echo kept-out-by-a-leading-space
ls
false
printf 'PROBE-STATUS:%s\n' "${dollar}seen_status"
cd $WORK_DIR/B
source $INCLUDE
printf 'PROBE-PC:%s\n' "${dollar}{PROMPT_COMMAND[*]}"
EOF
)"
recorder_out="$(run_shell "$home" "$WORK_DIR/start" "$recorder_script")"

check "a hook sharing the prompt still sees the command's own exit status" "1" \
    "$(printf '%s\n' "$recorder_out" | awk -F: '/^PROBE-STATUS:/ { print $2 }')"
check "sourcing twice adds the recorder once, after the hooks already there" \
    "probe_status __history_search_record" \
    "$(printf '%s\n' "$recorder_out" | awk '/^PROBE-PC:/ { sub(/^PROBE-PC:/, ""); print }')"

records="$(tr '\0' '\n' <"$state/context" | awk -F'\t' '{ print $2 "|" $3 "|" $4 }')"
expected_records="0|$WORK_DIR/start|cd $WORK_DIR/A
0|$WORK_DIR/A|echo one
1|$WORK_DIR/A|false
0|$WORK_DIR/A|printf 'PROBE-STATUS:%s\n' \"${dollar}seen_status\"
0|$WORK_DIR/A|cd $WORK_DIR/B
0|$WORK_DIR/B|source $INCLUDE
0|$WORK_DIR/B|printf 'PROBE-PC:%s\n' \"${dollar}{PROMPT_COMMAND[*]}\""
check "each command is filed under the directory it started in, with its status; the
        leading-space and HISTIGNORE commands are absent and nothing is recorded twice" \
    "$expected_records" "$records"
check "every record carries an epoch" "7" \
    "$(tr '\0' '\n' <"$state/context" | awk -F'\t' '$1 ~ /^[0-9]+$/ && length($1) >= 10' | wc -l)"
check "the context file is private to the user" "600" "$(stat -c %a "$state/context")"

# ── the Ctrl+R function, against a stub fzf ──────────────────────────────────────────
# The stub records its arguments and answers with the first candidate it was handed, or
# exits 130 as fzf does on Esc.
echo "== Ctrl+R"
cat >"$WORK_DIR/bin/fzf" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$@" >"${STUB_ARGS_FILE}"
if [ "${STUB_MODE:-pick}" = esc ]; then exit 130; fi
IFS= read -r -d '' first
printf '%s\n' "$first"
STUB
chmod 0755 "$WORK_DIR/bin/fzf"

search_home="$WORK_DIR/search-home"
search_state="$search_home/.local/state/bash"
mkdir -p "$search_state"
chmod 0700 "$search_state"
printf '%s\n' "older-command" >"$search_state/history"
printf '1\t0\t%s\t%s\0' "$WORK_DIR/B" "cmd-for-B" >"$search_state/context"
chmod 0600 "$search_state/context"

search_script="$(cat <<EOF
HISTFILE=$search_state/history; export STUB_ARGS_FILE=$WORK_DIR/stub-args; source $INCLUDE
cd $WORK_DIR/B
READLINE_LINE=typed; READLINE_POINT=5; __history_search; printf 'PROBE-LINE:%s|%s\n' "${dollar}READLINE_LINE" "${dollar}READLINE_POINT"
READLINE_LINE=typed; READLINE_POINT=5; STUB_MODE=esc __history_search; printf 'PROBE-ESC:%s|%s\n' "${dollar}READLINE_LINE" "${dollar}READLINE_POINT"
printf 'PROBE-BIND:%s\n' "${dollar}(for m in emacs-standard vi-insert vi-command; do bind -m ${dollar}m -X; done | grep -c __history_search)"
EOF
)"
search_out="$(run_shell "$search_home" "$WORK_DIR/start" "$search_script")"
check "Ctrl+R puts this directory's command on the line, for review, cursor at the end" \
    "cmd-for-B|9" "$(printf '%s\n' "$search_out" | awk '/^PROBE-LINE:/ { sub(/^PROBE-LINE:/, ""); print }')"
check "Esc leaves the typed line and cursor alone" \
    "typed|5" "$(printf '%s\n' "$search_out" | awk '/^PROBE-ESC:/ { sub(/^PROBE-ESC:/, ""); print }')"
check "what was typed becomes the search query" "1" "$(grep -cx -- '--query=typed' "$WORK_DIR/stub-args")"
check "Ctrl+R is bound in the emacs and both vi keymaps" "3" \
    "$(printf '%s\n' "$search_out" | awk -F: '/^PROBE-BIND:/ { print $2 }')"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
