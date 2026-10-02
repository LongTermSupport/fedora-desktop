#!/usr/bin/env bash
# Unit-test the restore-only SSH_ASKPASS (Plan 00135 Task 6.2): how a ccy session restored
# after a reboot on a headless server unlocks its passphrase-protected SSH key with nobody
# at the keyboard.
#
# WHY THIS EXISTS. A restored session used to stop at ssh-add's passphrase prompt, twice:
# once on the host (ccy's probe agent) and once in the container's entrypoint. On a server
# nobody answers either, so the session never came back. The fix feeds the vault's
# github_ssh_passphrase to ssh-add through SSH_ASKPASS, the way `run.bash --headless`
# already does, and ONLY on the restore path. Every promise that makes that safe is a thing
# that fails silently on a live machine, so each is driven here:
#
#   - the restore path unlocks through askpass, and an ordinary launch never does;
#   - the passphrase reaches ssh-add and nothing else: not argv, not a log, not the terminal;
#   - a missing or wrong passphrase on a server fails loudly and at once, rather than leaving
#     a session waiting at a prompt (real ssh-add asks a wrong askpass again for ever);
#   - every transient copy of the passphrase is gone once its key is added.
#
# The REAL ssh-keygen, ssh-agent and ssh-add are used, with a throwaway encrypted key, so the
# askpass protocol is OpenSSH's own and not a stub's idea of it. A recording shim sits in
# front of ssh-add to see its argv and environment, then runs the real one.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
LIB_DIR="$CCY_DIR/lib"
LAUNCHER="$CCY_DIR/claude-yolo"
ENTRYPOINT="$CCY_DIR/entrypoint.sh"
PLAY="$REPO_ROOT/playbooks/imports/play-claude-yolo.yml"
DROPIN="$REPO_ROOT/files/home/.config/systemd/user/ccy-sessions-restore.service.d/ssh-unlock.conf"

for f in "$LIB_DIR/common-pure.bash" "$LIB_DIR/session-registry.bash" "$LIB_DIR/ssh-handling.bash" \
    "$LAUNCHER" "$ENTRYPOINT" "$PLAY"; do
    if [ ! -f "$f" ]; then
        echo "FAIL: $f not found" >&2
        exit 1
    fi
done
for tool in ssh-keygen ssh-agent ssh-add timeout; do
    if ! command -v "$tool" >/dev/null; then
        echo "FAIL: $tool is not installed; this suite drives the real OpenSSH tools." >&2
        exit 1
    fi
done
REAL_SSH_ADD="$(command -v ssh-add)"

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
yes_if() { if "$@"; then echo yes; else echo no; fi; }

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/restore-askpass-test.XXXXXX")"
cleanup() { rm -rf "$SCRATCH"; }
trap cleanup EXIT

# The fixture passphrase has spaces and a quote, so a word-split or an unquoted expansion on
# its way to ssh-add shows up as a failed unlock.
PP="fixture pass'phrase 7"
KEYS="$SCRATCH/keys"
mkdir -p "$KEYS"
ssh-keygen -q -t ed25519 -N "$PP" -C restore-fixture -f "$KEYS/github_fixture"
ssh-keygen -q -t ed25519 -N "a different passphrase" -C other-fixture -f "$KEYS/github_other"
PPFILE="$SCRATCH/restore-ssh-passphrase"
printf '%s' "$PP" >"$PPFILE"
chmod 600 "$PPFILE"

# ── the ssh-add shim: records argv and the askpass environment, then runs the real one ──
BIN="$SCRATCH/bin"
mkdir -p "$BIN"
cat >"$BIN/ssh-add" <<'EOF'
#!/usr/bin/env bash
printf 'argv=%s|SSH_ASKPASS=%s|SSH_ASKPASS_REQUIRE=%s\n' "$*" "${SSH_ASKPASS:-unset}" \
    "${SSH_ASKPASS_REQUIRE:-unset}" >>"$SHIM_LOG"
if [ -n "${SHIM_RECORD_ONLY:-}" ]; then exit 0; fi
exec "$REAL_SSH_ADD" "$@"
EOF
chmod 755 "$BIN/ssh-add"

# drive <case> [args] — run one case in a FRESH shell holding the real libraries, under a
# timeout (a prompt loop shows up as a timeout, not as a hung suite), stdin closed, and no
# askpass inherited from whoever runs the suite. stdout+stderr of the case go to $OUT.
DRIVER="$SCRATCH/driver.bash"
cat >"$DRIVER" <<'EOF'
set -uo pipefail
source "$LIB_DIR/common-pure.bash"
source "$LIB_DIR/session-registry.bash"
source "$LIB_DIR/ssh-handling.bash"
# common.bash's command_exists, which that library defines; it needs a container engine to
# be sourced at all, so the one function the probe agent uses is restated here.
command_exists() { command -v "$1" >/dev/null; }
case_name="$1"
shift
case "$case_name" in
probe-unlock)
    # $@ = keys. TEST_RESTORE_FILE stands for what ccy_restore_passphrase_take set at the
    # top of the launch: assigned after sourcing, since the library initialises it empty.
    RESTORE_SSH_PASSPHRASE_FILE="${TEST_RESTORE_FILE:-}"
    SSH_KEYS=("$@")
    _probe_unlock_keys ccy
    rc=$?
    if [ -n "$CCY_PROBE_AGENT_SOCK" ]; then
        SSH_AUTH_SOCK="$CCY_PROBE_AGENT_SOCK" "$REAL_SSH_ADD" -l >"$AGENT_LIST" 2>&1
    fi
    _probe_agent_stop >/dev/null
    exit "$rc"
    ;;
add-key-interactive)
    _probe_agent_add_key "$1"
    ;;
stage)
    ccy_restore_askpass_stage "$1"
    ;;
container)
    # What build_ssh_mounts_and_validate left in SSH_RUN_OPTS must survive the append.
    SSH_RUN_OPTS=(-e EARLIER=1)
    ccy_restore_askpass_container "$1" || exit 1
    printf 'DIR=%s\n' "$CCY_RESTORE_ASKPASS_DIR"
    printf 'OPT=%s\n' "${SSH_RUN_OPTS[@]}"
    ;;
take)
    # $1 = the launch's SESSION_RESTORE; CCY_RESTORE_SSH_PASSPHRASE_FILE from the environment.
    ccy_restore_passphrase_take "$1" || exit 1
    printf 'FILE=%s|ENV=%s\n' "$RESTORE_SSH_PASSPHRASE_FILE" "${CCY_RESTORE_SSH_PASSPHRASE_FILE:-unset}"
    ;;
*)
    echo "driver: unknown case $case_name" >&2
    exit 98
    ;;
esac
EOF
OUT="$SCRATCH/out"
SHIM_LOG="$SCRATCH/shim.log"
AGENT_LIST="$SCRATCH/agent.list"
RUNTIME="$SCRATCH/runtime"
mkdir -p "$RUNTIME"
chmod 700 "$RUNTIME"
DRIVE_ENV=()
drive() {
    rm -f "$SHIM_LOG" "$AGENT_LIST"
    : >"$SHIM_LOG"
    env -u SSH_ASKPASS -u SSH_ASKPASS_REQUIRE -u CCY_RESTORE_PP_FILE -u DISPLAY \
        -u CCY_RESTORE_SSH_PASSPHRASE_FILE -u TEST_RESTORE_FILE \
        PATH="$BIN:$PATH" LIB_DIR="$LIB_DIR" REAL_SSH_ADD="$REAL_SSH_ADD" SHIM_LOG="$SHIM_LOG" \
        AGENT_LIST="$AGENT_LIST" XDG_RUNTIME_DIR="$RUNTIME" "${DRIVE_ENV[@]}" \
        timeout 30 bash "$DRIVER" "$@" </dev/null >"$OUT" 2>&1
}
leaks() { yes_if grep -qF -- "$PP" "$@"; }
staged_left() { find "$RUNTIME" -mindepth 1 -maxdepth 1 -name 'ccy-askpass.*' | grep -c .; }

echo "=== the askpass helper answers ssh-add's first question, and nothing else ==="
drive stage "$PPFILE"
rc=$?
STAGE="$(cat "$OUT")"
check "staging succeeds" "0" "$rc"
check "the stage is under XDG_RUNTIME_DIR" "yes" "$(yes_if test "${STAGE#"$RUNTIME"/}" != "$STAGE")"
check "the stage directory is owner-only" "700" "$(stat -c %a "$STAGE" 2>&1)"
check "the passphrase copy is owner-only" "600" "$(stat -c %a "$STAGE/pp" 2>&1)"
check "the helper is executable by its owner only" "700" "$(stat -c %a "$STAGE/askpass" 2>&1)"
check "the helper's own text holds no passphrase" "no" "$(leaks "$STAGE/askpass")"
answer="$(CCY_RESTORE_PP_FILE="$STAGE/pp" "$STAGE/askpass" "Enter passphrase for /root/.ssh/key_0: " 2>"$SCRATCH/helper.err")"
check "asked for a key's passphrase, it prints exactly the passphrase on stdout" "$PP" "$answer"
check "and writes nothing to stderr" "" "$(cat "$SCRATCH/helper.err")"
answer="$(CCY_RESTORE_PP_FILE="$STAGE/pp" "$STAGE/askpass" "Bad passphrase, try again for /root/.ssh/key_0: " 2>"$SCRATCH/helper.err")"
rc=$?
check "a retry after a wrong passphrase is refused (ssh-add would ask for ever)" "1" "$rc"
check "and the refusal prints no passphrase" "" "$answer"
check "the refusal is explained on stderr without the passphrase" "yes:no" \
    "$(yes_if test -s "$SCRATCH/helper.err"):$(leaks "$SCRATCH/helper.err")"
answer="$(CCY_RESTORE_PP_FILE="$STAGE/pp" "$STAGE/askpass" "Are you sure you want to continue connecting (yes/no)? " 2>"$SCRATCH/helper.err")"
rc=$?
check "any other question is refused" "1:" "$rc:$answer"
answer="$(env -u CCY_RESTORE_PP_FILE "$STAGE/askpass" "Enter passphrase for k: " 2>"$SCRATCH/helper.err")"
rc=$?
check "with no passphrase file named, it fails rather than answering empty" "yes" \
    "$(yes_if test "$rc" -ne 0 -a -z "$answer")"
rm -rf "$STAGE"

echo ""
echo "=== the restore path: the probe agent is unlocked through askpass, unattended ==="
DRIVE_ENV=(TEST_RESTORE_FILE="$PPFILE")
drive probe-unlock "$KEYS/github_fixture"
rc=$?
check "the key unlocks with stdin closed and no terminal" "0" "$rc"
check "the probe agent holds it" "yes" "$(yes_if grep -q restore-fixture "$AGENT_LIST")"
check "ssh-add was run through askpass, forced" "1" "$(grep -c 'SSH_ASKPASS_REQUIRE=force' "$SHIM_LOG")"
check "the passphrase is not in ssh-add's argv" "no" "$(leaks "$SHIM_LOG")"
check "nor in anything the launcher printed" "no" "$(leaks "$OUT")"
check "the probe's passphrase copy is gone once the key is added" "0" "$(staged_left)"

drive probe-unlock "$KEYS/github_fixture" "$KEYS/github_other"
rc=$?
check "a key the passphrase does not open fails the launch" "1" "$rc"
check "the failure names that key" "yes" "$(yes_if grep -q "github_other" "$OUT")"
check "and still prints no passphrase" "no" "$(leaks "$OUT")"
check "no passphrase copy is left behind on failure" "0" "$(staged_left)"

DRIVE_ENV=(TEST_RESTORE_FILE="$SCRATCH/no-such-file")
drive probe-unlock "$KEYS/github_fixture"
rc=$?
check "a passphrase file gone by launch time fails loudly, not at a prompt" "1" "$rc"
check "and names the file" "yes" "$(yes_if grep -qF "$SCRATCH/no-such-file" "$OUT")"
check "ssh-add was never asked" "0" "$(grep -c . "$SHIM_LOG")"

echo ""
echo "=== an ordinary launch never uses askpass ==="
DRIVE_ENV=(SHIM_RECORD_ONLY=1)
drive add-key-interactive "$KEYS/github_fixture"
check "ssh-add sees no SSH_ASKPASS" "1" "$(grep -c 'SSH_ASKPASS=unset|' "$SHIM_LOG")"
check "and no SSH_ASKPASS_REQUIRE" "1" "$(grep -c 'SSH_ASKPASS_REQUIRE=unset' "$SHIM_LOG")"
DRIVE_ENV=()
drive probe-unlock "$KEYS/github_fixture"
rc=$?
check "without a terminal an ordinary launch unlocks nothing, as before" "0:0" "$rc:$(grep -c . "$SHIM_LOG")"
check "and stages no passphrase" "0" "$(staged_left)"

echo ""
echo "=== the launcher takes the passphrase file only from a restore ==="
DRIVE_ENV=()
drive take false
check "ordinary launch, nothing set: no askpass" "FILE=|ENV=unset" "$(cat "$OUT")"
DRIVE_ENV=(CCY_RESTORE_SSH_PASSPHRASE_FILE="$PPFILE")
drive take false
check "the file named on a launch that is not a restore is refused" "1" "$?"
drive take true
check "a restore takes it, and removes it from the environment" "FILE=$PPFILE|ENV=unset" "$(cat "$OUT")"
DRIVE_ENV=(CCY_RESTORE_SSH_PASSPHRASE_FILE="$SCRATCH/no-such-file")
drive take true
rc=$?
check "a restore whose passphrase file is missing fails" "1" "$rc"
check "and names it" "yes" "$(yes_if grep -qF "$SCRATCH/no-such-file" "$OUT")"
chmod 644 "$PPFILE"
DRIVE_ENV=(CCY_RESTORE_SSH_PASSPHRASE_FILE="$PPFILE")
drive take true
check "a passphrase file others can read is refused" "1" "$?"
chmod 600 "$PPFILE"
: >"$SCRATCH/empty-pp"
chmod 600 "$SCRATCH/empty-pp"
DRIVE_ENV=(CCY_RESTORE_SSH_PASSPHRASE_FILE="$SCRATCH/empty-pp")
drive take true
check "an empty passphrase file is refused" "1" "$?"

echo ""
echo "=== the container: its entrypoint unlocks through the same helper, then removes it ==="
DRIVE_ENV=()
drive container "$PPFILE"
check "the container stage is built" "0" "$?"
CDIR="$(grep '^DIR=' "$OUT" | cut -d= -f2-)"
OPTS="$(grep '^OPT=' "$OUT" | cut -d= -f2- | tr '\n' ' ')"
check "it mounts the stage at /run/ccy/restore-askpass" "yes" \
    "$(yes_if grep -qF -- "-v $CDIR:/run/ccy/restore-askpass" <<<"$OPTS")"
check "and points ssh-add in the container at the helper, forced" "yes" \
    "$(yes_if grep -qF -- "-e SSH_ASKPASS=/run/ccy/restore-askpass/askpass -e SSH_ASKPASS_REQUIRE=force -e CCY_RESTORE_PP_FILE=/run/ccy/restore-askpass/pp" <<<"$OPTS")"
check "the options already in SSH_RUN_OPTS are kept" "yes" "$(yes_if grep -qF -- "-e EARLIER=1 -v" <<<"$OPTS")"
check "the passphrase is not in the engine's argv" "no" "$(leaks <<<"$OPTS")"
# The entrypoint's own steps, against the stage: its ssh-add with the environment the run
# options give it, then its finishing function, extracted from entrypoint.sh as written.
finish_def="$(awk '/^restore_askpass_finish\(\) \{/,/^\}/' "$ENTRYPOINT")"
check "entrypoint.sh defines restore_askpass_finish" "yes" "$(yes_if test -n "$finish_def")"
CAGENT="$(ssh-agent -s)"
csock="$(printf '%s' "$CAGENT" | grep -oP 'SSH_AUTH_SOCK=\K[^;]+')"
cpid="$(printf '%s' "$CAGENT" | grep -oP 'SSH_AGENT_PID=\K[0-9]+')"
SSH_AUTH_SOCK="$csock" SSH_ASKPASS="$CDIR/askpass" SSH_ASKPASS_REQUIRE=force CCY_RESTORE_PP_FILE="$CDIR/pp" \
    timeout 30 "$REAL_SSH_ADD" "$KEYS/github_fixture" </dev/null >"$SCRATCH/c-add.out" 2>&1
check "the container's ssh-add unlocks the key unattended" "0" "$?"
check "and prints no passphrase" "no" "$(leaks "$SCRATCH/c-add.out")"
if ! kill "$cpid"; then
    echo "  (the fixture agent $cpid was already gone)" >&2
fi
# finish-runner: the extracted function in a strict shell, as the entrypoint runs it, then
# what is left of the askpass environment.
FINISH="$SCRATCH/finish.bash"
printf '%s\n' "$finish_def" >"$SCRATCH/finish-def.bash"
cat >"$FINISH" <<'EOF'
set -euo pipefail
source "$FINISH_DEF"
restore_askpass_finish
printf '%s|%s|%s' "${SSH_ASKPASS:-unset}" "${SSH_ASKPASS_REQUIRE:-unset}" "${CCY_RESTORE_PP_FILE:-unset}"
EOF
fin="$(FINISH_DEF="$SCRATCH/finish-def.bash" SSH_ASKPASS="$CDIR/askpass" SSH_ASKPASS_REQUIRE=force \
    CCY_RESTORE_PP_FILE="$CDIR/pp" bash "$FINISH" 2>&1)"
check "finishing leaves no askpass in the environment the agent inherits" "unset|unset|unset" "$fin"
check "the passphrase copy is gone once the key is added" "absent" "$(test -e "$CDIR/pp" && echo present || echo absent)"
check "and so is the helper" "absent" "$(test -e "$CDIR/askpass" && echo present || echo absent)"
fin="$(env -u SSH_ASKPASS -u SSH_ASKPASS_REQUIRE -u CCY_RESTORE_PP_FILE \
    FINISH_DEF="$SCRATCH/finish-def.bash" bash "$FINISH" 2>&1)"
check "an ordinary container start passes through finishing untouched" "unset|unset|unset" "$fin"
rm -rf "$CDIR"

echo ""
echo "=== the wiring: each piece is called where the production path runs ==="
# Order inside entrypoint.sh: the finish comes after the key loop and before Claude starts.
add_line="$(grep -nF "ssh-add \"\$key\"" "$ENTRYPOINT" | cut -d: -f1 | sort -n | tail -n 1)"
fin_line="$(grep -nx 'restore_askpass_finish' "$ENTRYPOINT" | cut -d: -f1)"
exec_line="$(grep -nxF "exec \"\$@\"" "$ENTRYPOINT" | cut -d: -f1)"
check "entrypoint finishes after its ssh-add and before exec" "yes" \
    "$(yes_if test -n "$add_line" -a -n "$fin_line" -a -n "$exec_line" -a "${fin_line:-0}" -gt "${add_line:-0}" -a "${fin_line:-0}" -lt "${exec_line:-0}")"
check "the launcher takes the passphrase file from the restore marker" "1" \
    "$(grep -cxF "ccy_restore_passphrase_take \"\$SESSION_RESTORE\" || exit 1" "$LAUNCHER")"
check "the launcher stages the container's askpass on a restore" "1" \
    "$(grep -cF "ccy_restore_askpass_container \"\$RESTORE_SSH_PASSPHRASE_FILE\" || exit 1" "$LAUNCHER")"
check "the engine is given SSH_RUN_OPTS, which carries those options" "1" \
    "$(grep -cF "\"\${SSH_RUN_OPTS[@]}\"" "$LAUNCHER")"
check "the launcher's cleanup removes the container stage" "yes" \
    "$(yes_if grep -q 'CCY_RESTORE_ASKPASS_DIR' <<<"$(awk '/^cleanup\(\) \{/,/^\}/' "$LAUNCHER")")"
# The drop-in names the file the play writes: one path, written in two places.
check "the restore unit's drop-in exists" "yes" "$(yes_if test -f "$DROPIN")"
dropin_path="$(grep -oP '^Environment=CCY_RESTORE_SSH_PASSPHRASE_FILE=\K.*' "$DROPIN" 2>&1)"
play_path="$(grep -oP '^    ccy_restore_ssh_passphrase_file: "\K[^"]*' "$PLAY")"
check "the drop-in's path is the one the play writes" "/home/{{ user_login }}${dropin_path#%h}" "$play_path"

echo ""
echo "=== ccy-sessions restore: the server's passphrase file reaches ccy sessions, checked first ==="
# shellcheck source=/dev/null
source "$LIB_DIR/common-pure.bash"
# shellcheck source=/dev/null
source "$LIB_DIR/session-registry.bash"
export CCY_STATE_DIR="$SCRATCH/state"
printf 'boot-one\n' >"$SCRATCH/boot_id"
export CCY_BOOT_ID_FILE="$SCRATCH/boot_id"
ccy_tmux_list() { printf ''; }
STARTED_LOG="$SCRATCH/started"
ccy_tmux_start_detached() {
    printf '%q ' "$@" >>"$STARTED_LOG"
    printf '\n' >>"$STARTED_LOG"
}
PROJ="$SCRATCH/project"
mkdir -p "$PROJ"
ccy_registry_write "ccy-proj" "$PROJ" /launch/ccy ccy yes --ssh-key "$KEYS/github_fixture"
ccy_registry_write "cc-proj" "$PROJ" /launch/cc cc yes
unset CCY_RESTORE_SSH_PASSPHRASE_FILE

rm -f "$STARTED_LOG"
out="$(ccy_registry_restore 2>&1)"
check "desktop (no drop-in): the restore succeeds" "0" "$?"
check "and no session is given a passphrase file" "0" "$(grep -c CCY_RESTORE_SSH_PASSPHRASE_FILE "$STARTED_LOG")"

rm -f "$STARTED_LOG"
out="$(CCY_RESTORE_SSH_PASSPHRASE_FILE="$PPFILE" ccy_registry_restore 2>&1)"
check "server: the restore succeeds" "0" "$?"
check "the ccy session gets the file on its command" "1" \
    "$(grep -cF "ccy-proj $(printf '%q' "$PROJ") env CCY_SESSION_RESTORE=1 CCY_RESTORE_SSH_PASSPHRASE_FILE=$(printf '%q' "$PPFILE") /launch/ccy " "$STARTED_LOG")"
check "the cc session does not (it starts no container and loads no key)" "1" \
    "$(grep -cF "cc-proj $(printf '%q' "$PROJ") env CCY_SESSION_RESTORE=1 /launch/cc " "$STARTED_LOG")"
check "the path is passed, never the passphrase" "no" "$(leaks "$STARTED_LOG")"

rm -f "$STARTED_LOG"
out="$(CCY_RESTORE_SSH_PASSPHRASE_FILE="$SCRATCH/no-such-file" ccy_registry_restore 2>&1)"
check "server with the passphrase file missing: the restore fails" "1" "$?"
check "before anything is started" "absent" "$(test -e "$STARTED_LOG" && echo present || echo absent)"
check "naming the file" "yes" "$(yes_if grep -qF "$SCRATCH/no-such-file" <<<"$out")"
check "and the play that writes it" "yes" "$(yes_if grep -qF play-claude-yolo.yml <<<"$out")"

echo ""
echo "RESULT: passed: $passed failed: $failed"
if [ "$failed" -ne 0 ]; then
    exit 1
fi
