#!/usr/bin/env bash
# Unit-test the ccy session registry (files/var/local/claude-yolo/lib/session-registry.bash).
#
# WHY THIS EXISTS. Restore after a reboot reads nothing but this registry: a record that is
# still there at boot is the only evidence a session was running when the machine went down.
# So the record has to appear when a session starts, vanish when the session ENDS, and stay
# put when the session is KILLED — and the arguments it replays must be the ones that are
# safe to replay. `--prevent` in a record would switch ccy off for the project it restores;
# `--rebuild` would rebuild the image on every boot; `--prompt` would re-send a stale
# instruction. None of that can be caught on a live machine except by suffering it, so the
# filter and the file format are pure functions driven here.
#
# The restore side is tested as a TRANSLATION: records plus a live-session listing in, the
# commands that would run out. A real `systemd --user` unit cannot be exercised in the CCY
# container; the decisions can, and the decisions are where the bugs live.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"

for lib in common-pure.bash session-registry.bash; do
    if [ ! -f "$LIB_DIR/$lib" ]; then
        echo "FAIL: $lib not found at $LIB_DIR/$lib" >&2
        exit 1
    fi
done

# shellcheck source=/dev/null
source "$LIB_DIR/common-pure.bash"
# shellcheck source=/dev/null
source "$LIB_DIR/session-registry.bash"

for fn in ccy_registry_dir ccy_registry_replay_args ccy_registry_wants_restore \
    ccy_registry_launch_args ccy_registry_write ccy_registry_remove ccy_registry_read \
    ccy_registry_trampoline ccy_registry_restore_args ccy_registry_restore; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: $fn is not defined after sourcing the library" >&2
        exit 1
    fi
done

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

# Every filesystem case runs against a private state directory, never the real one.
mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/registry-test.XXXXXX")"
cleanup() { rm -rf "$SCRATCH"; }
trap cleanup EXIT
export CCY_STATE_DIR="$SCRATCH/state"

# joined <cmd...> — the command's stdout lines joined with "|", so a list is one comparable word.
joined() {
    local out
    out="$("$@")"
    printf '%s' "${out//$'\n'/|}"
}

# fresh_dir <env assignments...> — ccy_registry_dir in a fresh shell with only the given
# environment, so the fallbacks can be exercised without disturbing this shell's.
fresh_dir() {
    local script
    printf -v script 'source %q; source %q; ccy_registry_dir' \
        "$LIB_DIR/common-pure.bash" "$LIB_DIR/session-registry.bash"
    env -u CCY_STATE_DIR -u XDG_STATE_HOME "$@" bash -c "$script" 2>&1
}

echo ""
echo "=== where the registry lives ==="
check "CCY_STATE_DIR wins" "$SCRATCH/state/sessions" "$(ccy_registry_dir)"
check "XDG_STATE_HOME when CCY_STATE_DIR is unset" "/x/state/ccy/sessions" "$(fresh_dir XDG_STATE_HOME=/x/state)"
check "HOME fallback when neither is set" "/h/.local/state/ccy/sessions" "$(fresh_dir HOME=/h)"
# The same refusal helpers/play_ledger/ledger.py makes: a relative state home would put the
# registry somewhere that depends on the cwd of whoever launched, and restore would read an
# empty directory and report success.
if fresh_dir XDG_STATE_HOME=relative/state >"$SCRATCH/relative.out"; then
    check "a relative XDG_STATE_HOME is refused" "refused" "accepted"
else
    check "a relative XDG_STATE_HOME is refused" "refused" "refused"
fi

echo ""
echo "=== the replay filter: what a restored ccy session is started with ==="
check "settings are kept" "--token|work|--ssh-key|/k/id|--network|net1|--engine|podman|--no-ssh|--github-443|--no-network|--supervise" \
    "$(joined ccy_registry_replay_args ccy --token work --ssh-key /k/id --network net1 --engine podman --no-ssh --github-443 --no-network --supervise)"
check "nothing in, nothing out" "" "$(joined ccy_registry_replay_args ccy)"

# THE case that makes the filter load-bearing: --prevent writes `never` into the project's
# allowed-hostnames file. Replayed, a restore would switch ccy off for the project it was
# bringing back.
check "--prevent is dropped" "--token|work" "$(joined ccy_registry_replay_args ccy --token work --prevent)"
check "--rebuild and --rebuild=MODE are dropped" "--token|work" \
    "$(joined ccy_registry_replay_args ccy --rebuild --token work --rebuild=project)"
check "exit-without-a-session modes are dropped" "" \
    "$(joined ccy_registry_replay_args ccy --create-token --list-tokens --custom --custom-docker --top --headless --debug --disable-custom-docker)"
check "--update-token takes its value with it" "--token|work" \
    "$(joined ccy_registry_replay_args ccy --update-token personal --token work)"
check "--update-token=NAME is dropped" "--token|work" \
    "$(joined ccy_registry_replay_args ccy --update-token=personal --token work)"
check "--export-token takes its value with it" "--no-ssh" \
    "$(joined ccy_registry_replay_args ccy --export-token personal --no-ssh)"
check "--connect takes its value with it" "--no-ssh" \
    "$(joined ccy_registry_replay_args ccy --connect backend --no-ssh)"
check "--prompt takes its text with it" "--token|work" \
    "$(joined ccy_registry_replay_args ccy --prompt 'fix the tests' --token work)"
# The agent socket is a different path after a reboot, so replaying the flag would point at
# a socket that no longer exists. Dropped, and the launcher then asks as it would for a
# first run.
check "--ssh-agent is dropped" "--token|work" "$(joined ccy_registry_replay_args ccy --ssh-agent --token work)"
check "--no-restore is dropped (it is not the launcher's flag)" "--token|work" \
    "$(joined ccy_registry_replay_args ccy --no-restore --token work)"

# A bare word is a first message to claude — stale on replay — UNLESS it follows a flag ccy
# does not know, in which case it is that flag's value (`--model opus`). Cutting `opus` off
# `--model` would leave claude refusing to start.
check "a bare first message is dropped" "--token|work" \
    "$(joined ccy_registry_replay_args ccy 'summarise the diff' --token work)"
check "a value after a claude flag is kept" "--model|opus|--token|work" \
    "$(joined ccy_registry_replay_args ccy --model opus --token work)"
check "a second bare word after that value is dropped" "--model|opus" \
    "$(joined ccy_registry_replay_args ccy --model opus 'and then this')"

# Everything after `--` is claude's, raw, and stays exactly as typed — including words that
# would be one-shot in ccy's own position.
check "-- and everything after it are kept verbatim" "--token|work|--|--prevent|--rebuild|some text" \
    "$(joined ccy_registry_replay_args ccy --token work -- --prevent --rebuild 'some text')"

# cc hands every argument to claude, so none of ccy's one-shot set means anything there.
check "cc arguments pass through untouched" "--model|opus|--prevent|a note" \
    "$(joined ccy_registry_replay_args cc --model opus --prevent 'a note')"

echo ""
echo "=== --no-restore: an opt-out for a one-off session ==="
if ccy_registry_wants_restore --token work; then
    check "no flag means restore" "yes" "yes"
else
    check "no flag means restore" "yes" "no"
fi
if ccy_registry_wants_restore --token work --no-restore; then
    check "--no-restore means no restore" "no" "yes"
else
    check "--no-restore means no restore" "no" "no"
fi
if ccy_registry_wants_restore --token work -- --no-restore; then
    check "--no-restore after -- is claude's problem, not an opt-out" "yes" "yes"
else
    check "--no-restore after -- is claude's problem, not an opt-out" "yes" "no"
fi
# The argv the launcher is actually re-executed with: the opt-out removed, nothing else
# touched — the launcher never learns the flag existed.
check "launch args drop only --no-restore" "--rebuild|--token|work|--|--no-restore" \
    "$(joined ccy_registry_launch_args --rebuild --no-restore --token work -- --no-restore)"

echo ""
echo "=== the record: written on start, read back whole ==="
SPACED="$SCRATCH/a dir with spaces"
mkdir -p "$SPACED"
ccy_registry_write "ccy-proj" "$SPACED" "/var/local/claude-yolo/claude-yolo" ccy yes --token work --model opus
rec="$(ccy_registry_dir)/ccy-proj"
if [ -f "$rec" ]; then
    check "record file exists, named for the session" "present" "present"
else
    check "record file exists, named for the session" "present" "absent"
fi
check "record is private to the user" "600" "$(stat -c %a "$rec")"
if ccy_registry_read "$rec"; then
    check "read succeeds" "ok" "ok"
else
    check "read succeeds" "ok" "failed"
fi
check "name round-trips" "ccy-proj" "$REC_NAME"
check "a directory with spaces round-trips" "$SPACED" "$REC_DIR"
check "launcher round-trips" "/var/local/claude-yolo/claude-yolo" "$REC_LAUNCHER"
check "prefix round-trips" "ccy" "$REC_PREFIX"
check "restore flag round-trips" "yes" "$REC_RESTORE"
check "arguments round-trip in order" "--token|work|--model|opus" "$(IFS='|'; printf '%s' "${REC_ARGS[*]}")"
check "an argument with spaces round-trips" "--prompt-ish|two words here" \
    "$(ccy_registry_write "ccy-sp" "$SPACED" /l cc yes --prompt-ish 'two words here' && ccy_registry_read "$(ccy_registry_dir)/ccy-sp" && IFS='|' && printf '%s' "${REC_ARGS[*]}")"
check "a record with no arguments reads back an empty list" "0" \
    "$(ccy_registry_write "ccy-none" "$SPACED" /l ccy yes && ccy_registry_read "$(ccy_registry_dir)/ccy-none" && printf '%s' "${#REC_ARGS[@]}")"

# One field per line is the format, so a newline inside a value cannot be represented. That
# is refused at write time — never written as two lines that read back as something else.
if ccy_registry_write "ccy-nl" "$SPACED" /l ccy yes --token $'a\nb' 2>"$SCRATCH/newline.err"; then
    check "an argument containing a newline is refused" "refused" "written"
else
    check "an argument containing a newline is refused" "refused" "refused"
fi
if [ -f "$(ccy_registry_dir)/ccy-nl" ]; then
    check "and leaves no record behind" "absent" "present"
else
    check "and leaves no record behind" "absent" "absent"
fi
# Overwriting is the normal case: the same session name is reused after the old session
# ended, and the record must describe the NEW session.
ccy_registry_write "ccy-proj" "$SPACED" /l ccy no --token other
ccy_registry_read "$(ccy_registry_dir)/ccy-proj"
check "a rewrite replaces the record" "no|--token|other" "$REC_RESTORE|$(IFS='|'; printf '%s' "${REC_ARGS[*]}")"

printf 'not a record\n' >"$(ccy_registry_dir)/junk"
if ccy_registry_read "$(ccy_registry_dir)/junk" 2>"$SCRATCH/junk.err"; then
    check "a file without the header is rejected" "rejected" "accepted"
else
    check "a file without the header is rejected" "rejected" "rejected"
fi
printf 'ccy-session-record 1\nname=x\ncolour=blue\n' >"$(ccy_registry_dir)/unknown-key"
if ccy_registry_read "$(ccy_registry_dir)/unknown-key" 2>"$SCRATCH/unknown.err"; then
    check "an unknown key is rejected, not ignored" "rejected" "accepted"
else
    check "an unknown key is rejected, not ignored" "rejected" "rejected"
fi
printf 'ccy-session-record 1\nname=x\nprefix=ccy\nrestore=yes\nlauncher=/l\n' >"$(ccy_registry_dir)/no-dir"
if ccy_registry_read "$(ccy_registry_dir)/no-dir" 2>"$SCRATCH/nodir.err"; then
    check "a record missing its directory is rejected" "rejected" "accepted"
else
    check "a record missing its directory is rejected" "rejected" "rejected"
fi
rm -f "$(ccy_registry_dir)/junk" "$(ccy_registry_dir)/unknown-key" "$(ccy_registry_dir)/no-dir"

echo ""
echo "=== removal ==="
ccy_registry_remove "ccy-proj"
if [ -f "$(ccy_registry_dir)/ccy-proj" ]; then
    check "remove deletes the record" "absent" "present"
else
    check "remove deletes the record" "absent" "absent"
fi
if ccy_registry_remove "ccy-proj" 2>"$SCRATCH/remove.err"; then
    check "removing an absent record is not an error" "ok" "ok"
else
    check "removing an absent record is not an error" "ok" "failed"
fi

echo ""
echo "=== the trampoline: gone on exit, kept on a kill ==="
# The trampoline is the bash -c string the tmux pane runs. It runs the launcher, removes
# the record when the launcher RETURNS, and holds the window on a non-zero status. The
# whole feature rests on the kill case: a tmux server taken down by a reboot never lets the
# pane's bash reach the removal, and that is precisely the record restore must find.
tramp_rec="$(ccy_registry_dir)/ccy-tramp"
ccy_registry_write "ccy-tramp" "$SPACED" /l ccy yes
tramp="$(ccy_registry_trampoline "$tramp_rec" ccy)"
out="$(bash -c "$tramp" ccy-tmux true </dev/null 2>&1)"
rc=$?
check "a clean exit returns the command's status" "0" "$rc"
check "and prints nothing" "" "$out"
if [ -f "$tramp_rec" ]; then
    check "a clean exit removes the record" "absent" "present"
else
    check "a clean exit removes the record" "absent" "absent"
fi

ccy_registry_write "ccy-tramp" "$SPACED" /l ccy yes
out="$(bash -c "$tramp" ccy-tmux sh -c 'exit 7' </dev/null 2>&1)"
rc=$?
check "a failing exit returns the command's status" "7" "$rc"
check "and says so, holding the window" "ccy exited with status 7. Press Enter to close this session." "${out##*$'\n'}"
if [ -f "$tramp_rec" ]; then
    check "a failing exit still removes the record (the session ENDED)" "absent" "present"
else
    check "a failing exit still removes the record (the session ENDED)" "absent" "absent"
fi

ccy_registry_write "ccy-tramp" "$SPACED" /l ccy yes
bash -c "$tramp" ccy-tmux sleep 30 </dev/null >"$SCRATCH/killed.out" 2>&1 &
tramp_pid=$!
# Give the trampoline time to start its command, then take it down the way a reboot does.
for _ in 1 2 3 4 5 6 7 8 9 10; do
    if pgrep -P "$tramp_pid" >"$SCRATCH/children"; then break; fi
    sleep 0.1
done
kill -KILL "$tramp_pid"
wait "$tramp_pid" 2>"$SCRATCH/wait.err"
if pkill -P "$tramp_pid" 2>"$SCRATCH/pkill.err"; then :; fi
if [ -f "$tramp_rec" ]; then
    check "a KILLED trampoline leaves the record — the case restore reads" "present" "present"
else
    check "a KILLED trampoline leaves the record — the case restore reads" "present" "absent"
fi
ccy_registry_remove "ccy-tramp"

# A record path with a space or a quote must survive the trip through bash -c.
odd_rec="$(ccy_registry_dir)/ccy-odd name"
ccy_registry_write "ccy-odd name" "$SPACED" /l ccy yes
out="$(bash -c "$(ccy_registry_trampoline "$odd_rec" ccy)" ccy-tmux true </dev/null 2>&1)"
if [ -f "$odd_rec" ]; then
    check "a record path with a space is removed correctly" "absent" "present"
else
    check "a record path with a space is removed correctly" "absent" "absent"
fi

echo ""
echo "=== restore arguments: the recorded set plus what a restore needs ==="
check "ccy: --supervise and --continue are appended" "--token|work|--supervise|--continue" \
    "$(joined ccy_registry_restore_args ccy --token work)"
check "ccy: --supervise is not doubled" "--supervise|--token|work|--continue" \
    "$(joined ccy_registry_restore_args ccy --supervise --token work)"
check "ccy: --no-supervise is respected, not contradicted" "--no-supervise|--continue" \
    "$(joined ccy_registry_restore_args ccy --no-supervise)"
check "ccy: --continue is not doubled" "--continue|--supervise" \
    "$(joined ccy_registry_restore_args ccy --continue)"
check "ccy: --resume already names a conversation" "--resume|abc|--supervise" \
    "$(joined ccy_registry_restore_args ccy --resume abc)"
# cc runs claude on the host and forwards every argument to it; --supervise is ccy's flag
# and would reach claude as an unknown option.
check "cc: only --continue is appended" "--model|opus|--continue" \
    "$(joined ccy_registry_restore_args cc --model opus)"
check "empty in: just the additions" "--supervise|--continue" "$(joined ccy_registry_restore_args ccy)"

echo ""
echo "=== restore: records and live sessions in, decisions out ==="
# The executor asks two questions of the outside world — which sessions are live, and how to
# start one — and both are stubbed here, so what runs is every decision and nothing else.
LIVE=""
LIST_RC=0
ccy_tmux_list() {
    printf '%s' "$LIVE"
    return "$LIST_RC"
}
# The executor runs inside a command substitution below, so the stub records to a file the
# parent shell can read, not to a variable the subshell would take with it.
STARTED_LOG="$SCRATCH/started"
ccy_tmux_start_detached() {
    printf '%q ' "$@" >>"$STARTED_LOG"
    printf '\n' >>"$STARTED_LOG"
}
started_log() {
    if [ -f "$STARTED_LOG" ]; then cat "$STARTED_LOG"; fi
}
check "the live-listing stub answers" "" "$(ccy_tmux_list)"

rm -rf "$SCRATCH/state"
GONE="$SCRATCH/deleted checkout"
KEEP="$SCRATCH/kept checkout"
mkdir -p "$KEEP"
ccy_registry_write "ccy-alpha" "$KEEP" /launch/ccy ccy yes --token work
ccy_registry_write "ccy-beta" "$KEEP" /launch/ccy ccy yes
ccy_registry_write "cc-gamma" "$KEEP" /launch/cc cc yes --model opus
ccy_registry_write "ccy-oneoff" "$KEEP" /launch/ccy ccy no
ccy_registry_write "ccy-orphan" "$GONE" /launch/ccy ccy yes

LIVE=$'ccy-beta 0 '"$KEEP"$'\n'
rm -f "$STARTED_LOG"
out="$(ccy_registry_restore 2>&1)"
rc=$?
check "a vanished directory fails the run (loudly)" "1" "$rc"
check "and is named" "yes" "$([[ "$out" == *"ccy-orphan"* && "$out" == *"$GONE"* ]] && echo yes || echo no)"
check "a record whose session is already live is skipped" "yes" \
    "$([[ "$out" == *"ccy-beta"* && "$out" == *"already running"* ]] && echo yes || echo no)"
check "a no-restore record is skipped" "yes" \
    "$([[ "$out" == *"ccy-oneoff"* && "$out" == *"no-restore"* ]] && echo yes || echo no)"

# The failure above did not stop the others: every startable record was started, with the
# right launcher, directory and arguments, and the skipped ones were not.
started_alpha="ccy-alpha $(printf '%q' "$KEEP") /launch/ccy --token work --supervise --continue "
started_gamma="cc-gamma $(printf '%q' "$KEEP") /launch/cc --model opus --continue "
STARTED="$(started_log)"
check "startable ccy record started with restore args" "yes" "$([[ "$STARTED" == *"$started_alpha"* ]] && echo yes || echo no)"
check "startable cc record started with cc's restore args" "yes" "$([[ "$STARTED" == *"$started_gamma"* ]] && echo yes || echo no)"
check "the live one was not started" "no" "$([[ "$STARTED" == *"ccy-beta"* ]] && echo yes || echo no)"
check "the no-restore one was not started" "no" "$([[ "$STARTED" == *"ccy-oneoff"* ]] && echo yes || echo no)"
check "the orphan was not started" "no" "$([[ "$STARTED" == *"ccy-orphan"* ]] && echo yes || echo no)"
check "exactly two sessions started" "2" "$(printf '%s' "$STARTED" | grep -c .)"

# The orphan's record is deliberately LEFT: deleting it would make the next boot report
# success on a session that was never brought back. The operator removes it, or the
# directory comes back.
if [ -f "$(ccy_registry_dir)/ccy-orphan" ]; then
    check "a failed record is kept for the operator" "present" "present"
else
    check "a failed record is kept for the operator" "present" "absent"
fi

# Dry run: the same decisions printed, nothing started.
rm -f "$STARTED_LOG"
out="$(ccy_registry_restore --dry-run 2>&1)"
check "dry run starts nothing" "" "$(started_log)"
check "dry run names what it would start" "yes" \
    "$([[ "$out" == *"would start ccy-alpha"* && "$out" == *"would start cc-gamma"* ]] && echo yes || echo no)"

rm -rf "$SCRATCH/state"
out="$(ccy_registry_restore 2>&1)"
rc=$?
check "no registry directory at all is a clean no-op" "0" "$rc"
check "and says nothing was recorded" "yes" "$([[ "$out" == *"nothing to restore"* ]] && echo yes || echo no)"

# A listing failure is a failure: starting sessions on top of an unknown live set could
# double up every one of them.
mkdir -p "$SCRATCH/state/sessions"
ccy_registry_write "ccy-alpha" "$KEEP" /launch/ccy ccy yes
LIST_RC=1
rm -f "$STARTED_LOG"
out="$(ccy_registry_restore 2>&1)"
rc=$?
check "an unreadable live listing stops the restore" "1" "$rc"
check "and starts nothing" "" "$(started_log)"

echo ""
echo "──────────────────────────────────────────────────────────────"
printf 'passed: %d   failed: %d\n' "$passed" "$failed"
if [ "$passed" -eq 0 ]; then
    echo "ERROR: zero tests ran — discovery is broken, not the code clean" >&2
    exit 1
fi
if [ "$failed" -ne 0 ]; then
    exit 1
fi
echo "OK"
