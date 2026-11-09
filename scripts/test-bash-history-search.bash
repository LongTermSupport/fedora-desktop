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

# Reached through a symlink, the recorder stores the logical $PWD while git names the
# physical top level; the repository tier has to match either spelling.
ln -s "$repo" "$WORK_DIR/via-link"
link_context="$WORK_DIR/link-context"
{
    printf '1\t0\t%s\t%s\0' "$WORK_DIR/via-link" "cmd-repo-via-link"
    printf '2\t0\t%s\t%s\0' "$WORK_DIR/elsewhere" "cmd-elsewhere-after-link"
} >"$link_context"
mapfile -d '' ranked_link < <("$RANKER" "$link_context" "$history" "$WORK_DIR/via-link/sub")
check "the repository tier holds when the path goes through a symlink" \
    "cmd-repo-via-link" "${ranked_link[0]-}"

# A link INTO a repository (stow-style dotfiles: <home>/.config/tool -> dotfiles/.config/tool)
# ends in git's prefix too. Stripping it would make the whole home the repository, and every
# command run anywhere under it would be promoted.
stow_home="$WORK_DIR/stow-home"
mkdir -p "$stow_home/dotfiles/.config/tool" "$stow_home/.config" "$stow_home/unrelated"
git -C "$stow_home/dotfiles" init --quiet
ln -s "$stow_home/dotfiles/.config/tool" "$stow_home/.config/tool"
stow_context="$WORK_DIR/stow-context"
{
    printf '1\t0\t%s\t%s\0' "$stow_home/dotfiles" "cmd-in-dotfiles-repo"
    printf '2\t0\t%s\t%s\0' "$stow_home/unrelated" "cmd-unrelated-newer"
} >"$stow_context"
mapfile -d '' ranked_stow < <("$RANKER" "$stow_context" "$history" "$stow_home/.config/tool")
check "a link into a repository does not promote the directory above the link" \
    "cmd-in-dotfiles-repo" "${ranked_stow[0]-}"

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

# The include does nothing for root, which keeps stock Ctrl+R, and a ccy container runs
# this gate as root. There the shell runs in a user namespace as uid 1000, which is root
# outside it, so it owns the fixtures and the include sees a desktop user.
as_desktop_user=()
if [ "$(id -u)" -eq 0 ]; then
    as_desktop_user=(unshare --user --map-user=1000 --map-group=1000)
    # The shells' stderr is discarded below, so a namespace that cannot be made would
    # otherwise show only as a column of unexplained failures.
    if ! unshare_err="$("${as_desktop_user[@]}" true 2>&1)"; then
        echo "FAIL: running as root, and a user namespace for uid 1000 cannot be made: $unshare_err" >&2
        exit 1
    fi
fi

# run_shell <home> <cwd> <script> — an interactive bash reading the script from a pipe,
# with none of the harness's own dotfiles. Its stdout, where the PROBE lines go.
run_shell() {
    local shell_home="$1" cwd="$2" script="$3"
    (cd "$cwd" && printf '%s\n' "$script" |
        HOME="$shell_home" PATH="$WORK_DIR/bin:$PATH" "${as_desktop_user[@]}" bash --norc --noprofile -i 2>/dev/null)
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

check "a hook already in the prompt keeps seeing the command's own exit status" "1" \
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

# ── nohist=1 and histclean: the typed line is kept out of history and of the recorder ─────
# The real HISTIGNORE line of the profile.d file is fed to the interactive shell, so the test
# fails if the patterns there stop covering the prefix (in any case) or histclean. histclean
# is a stub: the real one is a Go binary, tested in src-go.
echo "== nohist=1"
PROFILE="$REPO_ROOT/files/etc/profile.d/zz_lts-fedora-desktop.bash"
ignore_line="$(grep -m1 '^export HISTIGNORE=' "$PROFILE")"
printf '#!/usr/bin/env bash\nexit 0\n' >"$WORK_DIR/bin/histclean"
chmod +x "$WORK_DIR/bin/histclean"
nohist_home="$WORK_DIR/nohist-home"
nohist_state="$nohist_home/.local/state/bash"
mkdir -p "$nohist_state"
nohist_script="$(cat <<EOF
HISTFILE=$nohist_state/history; HISTCONTROL=ignoredups; HISTTIMEFORMAT='%F %T  '
$ignore_line
source $INCLUDE
echo shown-control
nohist=1 echo hidden-a
nohist=1; echo hidden-b
NOHIST=1 echo hidden-c
NoHIst=1; echo hidden-d
nOhIsT=1
nohist=0 echo shown-misspelt
 nohist=1 echo shown-leading-space
histclean hidden-pattern
histclean
echo shown-after
history
EOF
)"
nohist_out="$(run_shell "$nohist_home" "$WORK_DIR/start" "$nohist_script")"
hidden_re='nohist=1|NOHIST=1|NoHIst=1|nOhIsT=1|histclean|hidden-'
visible_re='shown-(control|misspelt|leading-space|after)$'
check "lines that start with nohist=1 in any case, and histclean lines, are not in the shell's history" "0" \
    "$(printf '%s\n' "$nohist_out" | grep -v -E 'HISTIGNORE|source |shown-' | grep -c -E "^ *[0-9]+  .*(${hidden_re})")"
check "a misspelt prefix and a prefix not first on the line are recorded (the control)" "4" \
    "$(printf '%s\n' "$nohist_out" | grep -c -E "^ *[0-9]+  [0-9-]+ [0-9:]+  .*${visible_re}")"
check "the hidden lines are not in the history file" "0" \
    "$(grep -v -E 'HISTIGNORE|source |shown-' "$nohist_state/history" | grep -c -E "$hidden_re")"
check "nor in the Ctrl+R context file; the recorded lines are" "0:4" \
    "$(tr '\0' '\n' <"$nohist_state/context" | grep -v -E 'HISTIGNORE|source |shown-' | grep -c -E "$hidden_re"):$(tr '\0' '\n' <"$nohist_state/context" | grep -c -E "$visible_re")"

# ── the Ctrl+R function, against a stub fzf ──────────────────────────────────────────
# The stub records its arguments and answers with the first candidate it was handed, or
# exits 130 as fzf does on Esc.
echo "== Ctrl+R"
cat >"$WORK_DIR/bin/fzf" <<'STUB'
#!/usr/bin/env bash
printf 'ARG:%s\n' "$@" >"${STUB_ARGS_FILE}"
printf 'ENV:%s\n' "${__history_search_query-unset}" >>"${STUB_ARGS_FILE}"
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
READLINE_LINE=secretline; READLINE_POINT=5; __history_search; printf 'PROBE-LINE:%s|%s\n' "${dollar}READLINE_LINE" "${dollar}READLINE_POINT"
READLINE_LINE=secretline; READLINE_POINT=5; STUB_MODE=esc __history_search; printf 'PROBE-ESC:%s|%s\n' "${dollar}READLINE_LINE" "${dollar}READLINE_POINT"
printf 'PROBE-BIND:%s\n' "${dollar}(for m in emacs-standard vi-insert vi-command; do bind -m ${dollar}m -X; done | grep -c __history_search)"
EOF
)"
search_out="$(run_shell "$search_home" "$WORK_DIR/start" "$search_script")"
check "Ctrl+R puts this directory's command on the line, for review, cursor at the end" \
    "cmd-for-B|9" "$(printf '%s\n' "$search_out" | awk '/^PROBE-LINE:/ { sub(/^PROBE-LINE:/, ""); print }')"
check "Esc leaves the typed line and cursor alone" \
    "secretline|5" "$(printf '%s\n' "$search_out" | awk '/^PROBE-ESC:/ { sub(/^PROBE-ESC:/, ""); print }')"
# The typed line can be half a secret (`export TOKEN=...`). fzf's argv is readable by every
# local user through /proc; its environment is not, so the query must travel there.
check "what was typed reaches fzf through its environment" "1" "$(grep -cx 'ENV:secretline' "$WORK_DIR/stub-args")"
check "the picker's footer names both helpers" "1" \
    "$(grep -c -E '^ARG:--footer=nohist=1 .+histclean' "$WORK_DIR/stub-args")"
check "what was typed appears in none of fzf's arguments" "0" "$(awk '/^ARG:/ && /line/ && /secret/' "$WORK_DIR/stub-args" | wc -l)"
# Without this bind the environment variable is set and never read: the query would
# silently stop working while both checks above stayed green.
check "fzf is told to load its query from that environment variable" "1" \
    "$(grep -cxF "ARG:--bind=start:transform-query:printf %s \"${dollar}{__history_search_query}\"" "$WORK_DIR/stub-args")"
check "Ctrl+R is bound in the emacs and both vi keymaps" "3" \
    "$(printf '%s\n' "$search_out" | awk -F: '/^PROBE-BIND:/ { print $2 }')"

# ── Enter: what was typed, until the person moves to a match ────────────────────────
# The real fzf in a real terminal (tmux), since which row Enter takes is fzf's own state.
echo "== Enter takes the query"
if ! command -v tmux >/dev/null; then
    echo "FAIL: tmux not found — these cases drive the real fzf and cannot pass without it." >&2
    exit 1
fi
if ! command -v fzf >/dev/null; then
    echo "FAIL: fzf not found — these cases drive the real fzf (tasks/install-fzf.yml installs it; CI installs it in .github/workflows/qa.yml)." >&2
    exit 1
fi
mkdir -p "$WORK_DIR/realbin"
ln -s "$RANKER" "$WORK_DIR/realbin/bash-history-rank"
# fzf before 0.63 has no --footer and refuses it; a shim drops that one cosmetic flag so the
# fzf of an older machine can still be driven. The pinned fzf (vars/fzf-pin.yml) has it.
if [[ "$(fzf --help)" != *--footer* ]]; then
    printf '%s\n' '#!/bin/bash' 'args=()' \
        "for a in \"${dollar}@\"; do [[ \"${dollar}a\" == --footer=* ]] || args+=(\"${dollar}a\"); done" \
        "exec $(command -v fzf) \"${dollar}{args[@]}\"" >"$WORK_DIR/realbin/fzf"
    chmod 0755 "$WORK_DIR/realbin/fzf"
fi
printf '#!/bin/bash\nHISTFILE=%s; source %s; cd %s; PS1="READY> "\n' \
    "$search_state/pick-history" "$INCLUDE" "$WORK_DIR/B" >"$WORK_DIR/pick-rc"
pick_sock="$WORK_DIR/pick-sock"

# wait_screen <regex> — until a line of the pane matches; 0 when it did, 1 after about ten seconds.
wait_screen() {
    local _
    for _ in $(seq 300); do
        if grep -q -E -- "$1" <<<"$(tmux -S "$pick_sock" capture-pane -t p -p)"; then return 0; fi
        sleep 0.1
    done
    return 1
}

# pick <key>... — opens Ctrl+R's function, sends each key (t:TEXT is typed text, else a tmux
# key name), reads the screen, presses Enter, and leaves the line in pick_line, the screen
# before Enter in pick_screen.
pick() {
    local key
    rm -f "$WORK_DIR/pick-out"
    printf '%s\n' "cmd-two" "cmd-three" >"$search_state/pick-history"
    printf '1\t0\t%s\t%s\0' "$WORK_DIR/B" "cmd-for-B" >"$search_state/context"
    tmux -S "$pick_sock" kill-server 2>/dev/null
    tmux -S "$pick_sock" new-session -d -s p -x 100 -y 24 \
        "${as_desktop_user[@]}" env "HOME=$search_home" "PATH=$WORK_DIR/realbin:$PATH" bash --rcfile "$WORK_DIR/pick-rc" -i
    wait_screen "READY>"
    tmux -S "$pick_sock" send-keys -t p "READLINE_LINE=; __history_search; printf '%s' \"${dollar}READLINE_LINE\" >$WORK_DIR/pick-out" Enter
    wait_screen "^history"
    for key in "$@"; do
        if [ "${key#t:}" != "$key" ]; then tmux -S "$pick_sock" send-keys -t p -l -- "${key#t:}"; else tmux -S "$pick_sock" send-keys -t p "$key"; fi
        sleep 0.5
    done
    pick_screen="$(tmux -S "$pick_sock" capture-pane -t p -p)"
    tmux -S "$pick_sock" send-keys -t p Enter
    for _ in $(seq 300); do [ -e "$WORK_DIR/pick-out" ] && break; sleep 0.1; done
    if [ ! -e "$WORK_DIR/pick-out" ]; then
        printf 'pick %s: the line never came back; the screen was:\n%s\n' "$*" \
            "$(tmux -S "$pick_sock" capture-pane -t p -p)" >&2
    fi
    pick_line=""
    if [ -e "$WORK_DIR/pick-out" ]; then pick_line="$(cat "$WORK_DIR/pick-out")"; fi
    tmux -S "$pick_sock" kill-server 2>/dev/null
}

pick t:cmd
check "Enter on a query that matches takes the query as typed, not the top match" "cmd" "$pick_line"
check "the prompt says Enter takes what was typed" "1" "$(printf '%s\n' "$pick_screen" | grep -c 'Enter: as typed')"
pick t:cmd Down
check "after arrowing down, Enter takes the highlighted match" "cmd-three" "$pick_line"
check "the prompt says Enter takes this match" "1" "$(printf '%s\n' "$pick_screen" | grep -c 'Enter: this match')"
pick t:cmd Down t:-t
check "typing after a move returns Enter to the query" "cmd-t" "$pick_line"
check "and the prompt goes back with it" "1" "$(printf '%s\n' "$pick_screen" | grep -c 'Enter: as typed')"
pick t:zzz-no-match
check "Enter with no match takes the query" "zzz-no-match" "$pick_line"
pick
check "Enter with nothing typed takes the highlighted match" "cmd-for-B" "$pick_line"
check "and the prompt says so" "1" "$(printf '%s\n' "$pick_screen" | grep -c 'Enter: this match')"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
