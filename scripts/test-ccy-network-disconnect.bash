#!/usr/bin/env bash
# Unit-test `ccy --disconnect` and the saved default network it undoes
# (files/var/local/claude-yolo/lib/network-management.bash).
#
# WHY THIS EXISTS. `ccy --connect NET` does two things: it attaches NET to the running
# container, and it saves NET as the project's default, which every later plain `ccy` in
# that project connects to again. A wrong --connect therefore sticks. --disconnect is the
# undo, and the half that matters most is the one a user cannot see: whether the saved
# default was cleared, kept, or never there. So every case below checks the saved default as
# well as the engine calls.
#
# The container engine is a stub function. It answers `ps`, `container inspect` and
# `network ls` from the fixture below, and records every `network connect`/`disconnect` it
# is asked for, so a case can assert that a refusal made no change at all. `container
# inspect` answers only the exact Go template the library sends, in the shape the real
# engine prints it (one line, a space after each name), so a changed template fails here.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"

for lib in common-pure.bash network-management.bash session-registry.bash; do
    if [ ! -f "$LIB_DIR/$lib" ]; then
        echo "FAIL: $lib not found at $LIB_DIR/$lib" >&2
        exit 1
    fi
done

# shellcheck source=/dev/null
source "$LIB_DIR/common-pure.bash"
# shellcheck source=/dev/null
source "$LIB_DIR/network-management.bash"
# shellcheck source=/dev/null
source "$LIB_DIR/session-registry.bash"

SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT
export HOME="$SCRATCH/home"
# The session registry: a restore replays each record's arguments, --network included.
export CCY_STATE_DIR="$SCRATCH/state"
mkdir -p "$HOME" "$SCRATCH/projects/demo"
# get_project_name reads the working directory: a generic parent gives the bare "demo".
cd "$SCRATCH/projects/demo" || exit 1
export CONTAINER_ENGINE=podman

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
says() { if grep -qF -- "$1" <<<"$2"; then echo yes; else echo no; fi; }

# ── the engine stub ──────────────────────────────────────────────────────────────────────
#
# STUB_PS          running container names, one per line
# STUB_PS_FAILS    non-empty: `ps` fails, with the engine's own message on stderr
# STUB_NETS[n]     the networks container n is attached to, space-separated
# STUB_ALL_NETS    every network the engine knows, space-separated, for `network ls`
# STUB_LS_FAILS    non-empty: `network ls` fails, with the engine's own message on stderr
# STUB_FAIL_VERB   "disconnect" or "connect": that verb fails with the engine's own message
CALLS="$SCRATCH/calls"
declare -A STUB_NETS=()
# \$ is a literal $: these are Go template variables, not shell ones.
INSPECT_TEMPLATE="{{range \$k, \$v := .NetworkSettings.Networks}}{{\$k}} {{end}}"
container_cmd() {
    local words=()
    case "$1 ${2:-}" in
        "ps --format")
            if [ -n "${STUB_PS_FAILS:-}" ]; then
                echo "Error: cannot connect to Podman: simulated ps failure" >&2
                return 125
            fi
            [ -n "$STUB_PS" ] && printf '%s\n' "$STUB_PS"
            return 0
            ;;
        "container inspect")
            if [ "${4:-}" != "--format" ] || [ "${5:-}" != "$INSPECT_TEMPLATE" ]; then
                echo "container_cmd stub: container inspect called with an unexpected template: $*" >&2
                return 99
            fi
            if [ -z "${STUB_NETS[$3]+set}" ]; then
                echo "Error: no such container $3" >&2
                return 125
            fi
            read -r -a words <<<"${STUB_NETS[$3]}"
            [ "${#words[@]}" -gt 0 ] && printf '%s ' "${words[@]}"
            printf '\n'
            return 0
            ;;
        "network ls")
            if [ -n "${STUB_LS_FAILS:-}" ]; then
                echo "Error: cannot connect to Podman: simulated network ls failure" >&2
                return 125
            fi
            read -r -a words <<<"$STUB_ALL_NETS"
            printf '%s\n' "${words[@]}"
            return 0
            ;;
        "network disconnect" | "network connect")
            echo "$2 $3 $4" >>"$CALLS"
            if [ "${STUB_FAIL_VERB:-}" = "$2" ]; then
                echo "Error: $2 $3 from $4: netavark: simulated engine failure" >&2
                return 125
            fi
            return 0
            ;;
    esac
    echo "container_cmd stub: unexpected call: $*" >&2
    return 99
}

# reset: one running container on the default network and the wrong project network.
reset() {
    : >"$CALLS"
    STUB_PS="demo_yolo"
    STUB_NETS=([demo_yolo]="podman wrong-network")
    STUB_ALL_NETS="podman wrong-network right-network"
    STUB_FAIL_VERB=""
    STUB_PS_FAILS=""
    STUB_LS_FAILS=""
    : >"$TTY"
    rm -f "$(get_network_persistence_file)"
    rm -rf .claude "$CCY_STATE_DIR"
}

# The Quick Launch config, in the shape the launcher's save_launch_config writes it. A plain
# ccy offers it (and a session restore takes it unasked), joining LAST_NETWORK.
QL=".claude/ccy/.last-launch.conf"
write_quick_launch() {
    mkdir -p .claude/ccy
    cat >"$QL" <<EOF
# CCY Launch Configuration
# Config Version: 3
SAVED_CONFIG_VERSION=3
SAVED_CCY_VERSION="9.9.9"
SAVED_CCY_HASH="0123abcd"
LAST_TOKEN="demo-token"
LAST_SSH_KEYS="key-one key-two"
LAST_NETWORK="$1"
LAST_LAUNCH_DATE="2026-09-24"
EOF
    chmod 600 "$QL"
}
# What the launcher reads: it sources the file, so this does too.
quick_launch_network() {
    # shellcheck source=/dev/null
    (source "$QL" && printf '%s' "${LAST_NETWORK-unset}")
}
quick_launch_rest() { grep -v '^LAST_NETWORK=' "$QL"; }
QL_REST_WANT="$(printf '%s\n' '# CCY Launch Configuration' '# Config Version: 3' 'SAVED_CONFIG_VERSION=3' \
    'SAVED_CCY_VERSION="9.9.9"' 'SAVED_CCY_HASH="0123abcd"' 'LAST_TOKEN="demo-token"' \
    'LAST_SSH_KEYS="key-one key-two"' 'LAST_LAUNCH_DATE="2026-09-24"')"

# A session record, as the launcher writes it: write_record NAME DIR PREFIX [replay-args...]
write_record() { ccy_registry_write "$1" "$2" /usr/local/bin/ccy "$3" yes "${@:4}"; }
# The arguments a record replays, space-joined (the record must read cleanly).
record_args() {
    ccy_registry_read "$CCY_STATE_DIR/sessions/$1" || { echo "(unreadable)"; return; }
    printf '%s' "${REC_ARGS[*]}"
}
# The y/N confirmation reads the terminal, not stdin. CCY_TTY points it at this file, which
# a case fills with the answers typed; an empty file is end of input.
TTY="$SCRATCH/tty"
export CCY_TTY="$TTY"
saved() {
    local f
    f="$(get_network_persistence_file)"
    if [ -f "$f" ]; then cat "$f"; else echo "(none)"; fi
}
calls() { tr '\n' ';' <"$CALLS"; }
# run_disconnect INPUT NETWORK: the function under test, with INPUT on stdin; sets RC, OUT, ERR.
run_disconnect() {
    OUT="$(disconnect_from_network "$2" "_yolo" "ccy" <<<"$1" 2>"$SCRATCH/err")"
    RC=$?
    ERR="$(cat "$SCRATCH/err")"
}

if ! declare -F disconnect_from_network >/dev/null; then
    echo "FAIL: disconnect_from_network is not defined after sourcing network-management.bash" >&2
    exit 1
fi

echo "=== a named network ==="
reset
save_network_preference wrong-network
run_disconnect "" wrong-network
check "a named, attached network is detached" "0" "$RC"
check "and the engine was asked for exactly that" "disconnect wrong-network demo_yolo;" "$(calls)"
check "and the saved default that named it is cleared" "(none)" "$(saved)"
check "and the output says the default was cleared" "yes" "$(says "Cleared the saved default network: wrong-network" "$OUT")"

reset
save_network_preference right-network
run_disconnect "" wrong-network
check "with another network saved as the default, the detach still happens" "0" "$RC"
check "and that saved default is kept" "right-network" "$(saved)"
check "and the output names the default that stays" "yes" "$(says "right-network" "$OUT")"

reset
run_disconnect "" wrong-network
check "with no saved default, the detach happens" "0" "$RC"
check "and the output says there is none" "yes" "$(says "No saved default network" "$OUT")"

reset
save_network_preference wrong-network
run_disconnect "" right-network
check "a named network the container is not on is refused" "1" "$RC"
check "and nothing is detached" "" "$(calls)"
check "and the refusal names what IS attached" "yes" "$(says "wrong-network" "$ERR")"
check "and the saved default is untouched" "wrong-network" "$(saved)"

echo ""
echo "=== the engine refuses ==="
reset
save_network_preference wrong-network
STUB_FAIL_VERB=disconnect
run_disconnect "" wrong-network
check "an engine failure fails the command" "1" "$RC"
check "and the engine's own message is shown" "yes" "$(says "simulated engine failure" "$ERR")"
check "and the saved default is NOT cleared, since nothing was detached" "wrong-network" "$(saved)"

echo ""
echo "=== the picker ==="
reset
run_disconnect "1" ""
check "one project network: choice 1 detaches it" "0" "$RC"
check "and it was that network" "disconnect wrong-network demo_yolo;" "$(calls)"
check "the engine's default network is never offered" "no" "$(says "podman" "$ERR")"

reset
STUB_NETS[demo_yolo]="podman alpha-network wrong-network"
run_disconnect "2" ""
check "several networks: choice 2 detaches the second" "disconnect wrong-network demo_yolo;" "$(calls)"
check "and both were offered" "yes" "$(says "alpha-network" "$ERR")"

reset
run_disconnect $'x\n9\n1' ""
check "a typo and an out-of-range choice re-prompt, then a good choice works" "0" "$RC"
check "and only the good choice reached the engine" "disconnect wrong-network demo_yolo;" "$(calls)"
check "and the typo was named" "yes" "$(says "'x' is not a choice" "$ERR")"

reset
save_network_preference wrong-network
run_disconnect $'x\ny\nz' ""
check "three bad answers give up" "1" "$RC"
check "and nothing is detached" "" "$(calls)"
check "and the saved default is untouched" "wrong-network" "$(saved)"

reset
OUT="$(disconnect_from_network "" "_yolo" "ccy" </dev/null 2>"$SCRATCH/err")"
RC=$?
check "end of input cancels" "1" "$RC"
check "and nothing is detached" "" "$(calls)"

reset
STUB_NETS[demo_yolo]="podman"
run_disconnect "" ""
check "a container on no project network has nothing to pick" "0" "$RC"
check "and nothing is detached" "" "$(calls)"
check "and it says so" "yes" "$(says "no project network to disconnect" "$OUT")"

echo ""
echo "=== several containers for the project ==="
reset
STUB_PS=$'demo_yolo\ndemo_yolo_2'
STUB_NETS[demo_yolo_2]="podman wrong-network"
run_disconnect "" wrong-network
check "every container on the network is detached" "disconnect wrong-network demo_yolo;disconnect wrong-network demo_yolo_2;" "$(calls)"

reset
STUB_PS=$'demo_yolo\ndemo_yolo_2'
STUB_NETS[demo_yolo_2]="podman"
run_disconnect "" wrong-network
check "a container not on it is left alone" "disconnect wrong-network demo_yolo;" "$(calls)"

echo ""
echo "=== no running container ==="
reset
STUB_PS=""
run_disconnect "" wrong-network
check "with no container and no saved default, it is refused" "1" "$RC"
check "and says no container is running" "yes" "$(says "No running containers found for project: demo" "$ERR")"

reset
STUB_PS=""
save_network_preference wrong-network
run_disconnect "" wrong-network
check "with no container, the saved default it names is still cleared" "0" "$RC"
check "and is gone" "(none)" "$(saved)"
check "and nothing reached the engine" "" "$(calls)"

# No container and no name: nothing says WHICH network was meant, so clearing the saved
# default is confirmed on the terminal first, and anything but a yes keeps it.
reset
STUB_PS=""
save_network_preference wrong-network
printf 'y\n' >"$TTY"
run_disconnect "" ""
check "no container, no name: a yes clears the saved default" "(none)" "$(saved)"
check "and succeeds" "0" "$RC"
check "and the question names the saved network" "yes" "$(says "saved default network is wrong-network" "$ERR")"

reset
STUB_PS=""
save_network_preference wrong-network
printf 'n\n' >"$TTY"
run_disconnect "" ""
check "no container, no name: a no keeps the saved default" "wrong-network" "$(saved)"
check "and fails, since nothing was done" "1" "$RC"
check "and says it was kept" "yes" "$(says "Kept the saved default network: wrong-network" "$ERR")"

reset
STUB_PS=""
save_network_preference wrong-network
printf '\n' >"$TTY"
run_disconnect "" ""
check "no container, no name: a bare Enter is the default No" "wrong-network" "$(saved)"

reset
STUB_PS=""
save_network_preference wrong-network
run_disconnect "" ""
check "no container, no name: end of input keeps the saved default" "wrong-network" "$(saved)"
check "and fails" "1" "$RC"

reset
STUB_PS=""
save_network_preference wrong-network
printf 'maybe\ny\n' >"$TTY"
run_disconnect "" ""
check "no container, no name: a typo re-prompts, then a yes clears" "(none)" "$(saved)"
check "and the typo was named" "yes" "$(says "'maybe' is not y or n" "$ERR")"

reset
STUB_PS=""
save_network_preference wrong-network
printf 'a\nb\nc\ny\n' >"$TTY"
run_disconnect "" ""
check "no container, no name: three bad answers give up and keep it" "wrong-network" "$(saved)"

reset
STUB_PS=""
save_network_preference wrong-network
CCY_TTY="$SCRATCH/no-such-terminal" run_disconnect "" ""
check "no container, no name, no terminal: the saved default is kept" "wrong-network" "$(saved)"
check "and fails" "1" "$RC"

reset
STUB_PS=""
save_network_preference wrong-network
printf 'y\n' >"$TTY"
run_disconnect "" ""
check "the answer is read from the terminal, not stdin" "(none)" "$(saved)"

reset
STUB_PS=""
save_network_preference right-network
run_disconnect "" wrong-network
check "with no container, a name that is not the saved default is refused" "1" "$RC"
check "and the saved default is kept" "right-network" "$(saved)"

echo ""
echo "=== the container's last network is never detached ==="
# The usual undo: the session was launched with --network <saved>, so that network is the
# container's only one. Detaching it would cut the session off entirely, the Claude API
# included, so it is refused; the saved default that names it is still cleared, so the next
# launch does not join it again.
reset
STUB_NETS[demo_yolo]="wrong-network"
save_network_preference wrong-network
run_disconnect "" wrong-network
check "a container's only network is not detached" "1" "$RC"
check "and nothing reached the engine" "" "$(calls)"
check "and the refusal says the session would lose all networking" "yes" "$(says "lose all networking" "$ERR")"
check "and says what to do instead" "yes" "$(says "ccy --no-network" "$ERR")"
check "but the saved default that names it is cleared" "(none)" "$(saved)"
check "and the output says so" "yes" "$(says "Cleared the saved default network: wrong-network" "$OUT")"

reset
STUB_NETS[demo_yolo]="wrong-network"
save_network_preference right-network
run_disconnect "" wrong-network
check "the last network is refused with another default saved" "1" "$RC"
check "and that other default is kept" "right-network" "$(saved)"

reset
STUB_NETS[demo_yolo]="wrong-network"
run_disconnect "1" ""
check "picked from the list, a container's only network is still refused" "1" "$RC"
check "and nothing reached the engine" "" "$(calls)"

reset
STUB_PS=$'demo_yolo\ndemo_yolo_2'
STUB_NETS[demo_yolo_2]="wrong-network"
run_disconnect "" wrong-network
check "one container would be stranded: none is detached" "" "$(calls)"
check "and the refusal names that container" "yes" "$(says "demo_yolo_2" "$ERR")"

echo ""
echo "=== the engine cannot list its containers ==="
# An engine failure is not "no containers": read that way, a bare --disconnect would clear
# the saved default and report success.
reset
STUB_PS_FAILS=1
save_network_preference wrong-network
printf 'y\n' >"$TTY"
run_disconnect "" ""
check "bare --disconnect fails when ps fails" "1" "$RC"
check "and the saved default is untouched" "wrong-network" "$(saved)"
check "and the engine's own message is shown" "yes" "$(says "simulated ps failure" "$ERR")"
check "and it says the containers could not be listed" "yes" "$(says "could not list the running containers" "$ERR")"

reset
STUB_PS_FAILS=1
save_network_preference wrong-network
run_disconnect "" wrong-network
check "named --disconnect fails when ps fails" "1" "$RC"
check "and the saved default is untouched" "wrong-network" "$(saved)"
check "and nothing reached the engine" "" "$(calls)"

reset
STUB_PS_FAILS=1
OUT="$(connect_to_network right-network "_yolo" "ccy" 2>&1)"
RC=$?
check "--connect fails when ps fails" "1" "$RC"
check "and saves no default" "(none)" "$(saved)"
check "and says the containers could not be listed" "yes" "$(says "could not list the running containers" "$OUT")"
check "and nothing reached the engine" "" "$(calls)"

echo ""
echo "=== --connect says what it saved, and how to undo it ==="
reset
OUT="$(connect_to_network right-network "_yolo" "ccy" 2>&1)"
check "a connect saves the default" "right-network" "$(saved)"
check "and says every plain launch will reconnect" "yes" "$(says "every plain ccy launch in this project connects to it" "$OUT")"
check "and names the undo" "yes" "$(says "ccy --disconnect right-network" "$OUT")"

echo ""
echo "=== the Quick Launch config forgets the network too ==="
# A launch onto a network writes it into the Quick Launch config as LAST_NETWORK; the next
# plain ccy offers that config (default Yes) and a session restore takes it unasked. So the
# saved default is only one of the places a network comes back from.
reset
save_network_preference wrong-network
write_quick_launch wrong-network
run_disconnect "" wrong-network
check "a detach clears LAST_NETWORK when it names that network" "" "$(quick_launch_network)"
check "and every other line of the config is kept" "$QL_REST_WANT" "$(quick_launch_rest)"
check "and the config keeps its private mode" "600" "$(stat -c %a "$QL")"
check "and the output says Quick Launch forgot it" "yes" "$(says "Quick Launch" "$OUT")"

reset
write_quick_launch right-network
run_disconnect "" wrong-network
check "LAST_NETWORK naming another network is left alone" "right-network" "$(quick_launch_network)"

reset
write_quick_launch wrong-network
run_disconnect "" wrong-network
check "with no saved default, Quick Launch still forgets the network" "" "$(quick_launch_network)"
check "and the detach succeeds" "0" "$RC"

reset
STUB_NETS[demo_yolo]="wrong-network"
save_network_preference wrong-network
write_quick_launch wrong-network
run_disconnect "" wrong-network
check "the last-network refusal clears LAST_NETWORK too" "" "$(quick_launch_network)"
check "and still refuses" "1" "$RC"
check "and no longer tells the user to wait for a default that is already gone" "no" "$(says "once no saved default" "$ERR")"

reset
write_quick_launch wrong-network
STUB_FAIL_VERB=disconnect
run_disconnect "" wrong-network
check "an engine refusal leaves the Quick Launch config alone" "wrong-network" "$(quick_launch_network)"

reset
STUB_PS=""
write_quick_launch wrong-network
run_disconnect "" wrong-network
check "no container, named: a network only Quick Launch remembers is forgotten" "" "$(quick_launch_network)"
check "and that succeeds" "0" "$RC"

reset
STUB_PS=""
write_quick_launch wrong-network
run_disconnect "" ""
check "no container, no name, no saved default: the refusal names the Quick Launch network" "yes" "$(says "ccy --disconnect wrong-network" "$ERR")"
check "and changes nothing" "wrong-network" "$(quick_launch_network)"

# The config is rewritten beside itself and moved into place; a rewrite that cannot be made
# is a failure that says so, never a quiet "done". The temporary name uses $$, which is this
# shell's pid in the function's subshell too, so a directory there makes the write fail.
reset
save_network_preference wrong-network
write_quick_launch wrong-network
mkdir "$QL.tmp.$$"
run_disconnect "" wrong-network
check "a Quick Launch config that cannot be rewritten fails the command" "1" "$RC"
check "and says which file" "yes" "$(says ".last-launch.conf" "$ERR")"
check "and the config is unchanged" "wrong-network" "$(quick_launch_network)"
rmdir "$QL.tmp.$$"

# A hand-edited LAST_NETWORK line (trailing space, a CR from another editor, a comment) still
# reads as naming the network, but is not the exact line the rewrite replaces. The command must
# then fail and name the line, never report the network cleared while the config still names it.
for variant in "space" "cr" "comment"; do
    reset
    save_network_preference wrong-network
    write_quick_launch wrong-network
    case "$variant" in
        space) suffix=" " ;;
        cr) suffix=$'\r' ;;
        comment) suffix=" # hand-edited" ;;
    esac
    awk -v s="$suffix" '/^LAST_NETWORK=/ { print $0 s; next } { print }' "$QL" >"$QL.new" && mv "$QL.new" "$QL"
    run_disconnect "" wrong-network
    check "a hand-edited LAST_NETWORK line ($variant) fails the command" "1" "$RC"
    check "and it does not claim Quick Launch was cleared ($variant)" "no" "$(says "Cleared wrong-network from the Quick Launch" "$OUT")"
    check "and it names the file and the line ($variant)" "yes" "$(says "$QL still names wrong-network on the line" "$ERR")"
done

echo ""
echo "=== a restore record forgets the network too ==="
# A session started with --network NET replays it on the restore after a reboot. After
# --disconnect NET, that record would bring NET back.
reset
STUB_NETS[demo_yolo]="podman wrong-network"
write_record demo_yolo "$PWD" ccy --network wrong-network --token demo-token
run_disconnect "" wrong-network
check "a detach removes --network NET from this project's restore record" "--token demo-token" "$(record_args demo_yolo)"
check "and the output names the record" "yes" "$(says "demo_yolo" "$OUT")"
mapfile -t replay < <(ccy_registry_restore_args ccy --token demo-token)
check "so a restore replays no --network at all" "no" "$(says "--network" "$(printf '%s ' "${replay[@]}")")"

reset
write_record demo_yolo "$PWD" ccy --ssh-key key-one --network wrong-network --model opus --no-ssh
run_disconnect "" wrong-network
check "only the --network pair goes; every other flag and value stays in order" \
    "--ssh-key key-one --model opus --no-ssh" "$(record_args demo_yolo)"

reset
write_record demo_yolo "$PWD" ccy --network right-network
run_disconnect "" wrong-network
check "a record on another network is left alone" "--network right-network" "$(record_args demo_yolo)"

reset
write_record other_yolo "$SCRATCH/projects/other" ccy --network wrong-network
run_disconnect "" wrong-network
check "another project's record is left alone" "--network wrong-network" "$(record_args other_yolo)"

reset
write_record demo_yolo "$PWD" ccy -- --network wrong-network
run_disconnect "" wrong-network
check "words after -- are claude's and are left alone" "-- --network wrong-network" "$(record_args demo_yolo)"

reset
write_record demo_cc "$PWD" cc --network wrong-network
run_disconnect "" wrong-network
check "a cc record (every word is claude's) is left alone" "--network wrong-network" "$(record_args demo_cc)"

reset
STUB_NETS[demo_yolo]="wrong-network"
write_record demo_yolo "$PWD" ccy --network wrong-network
run_disconnect "" wrong-network
check "the last-network refusal also removes it from the restore record" "" "$(record_args demo_yolo)"

reset
STUB_PS=""
write_record demo_yolo "$PWD" ccy --network wrong-network
run_disconnect "" wrong-network
check "no container, named: a network only a restore record remembers is forgotten" "" "$(record_args demo_yolo)"
check "and that succeeds" "0" "$RC"

reset
write_record demo_yolo "$PWD" ccy --network wrong-network
mkdir -p "$CCY_STATE_DIR/sessions"
printf 'not a record\n' >"$CCY_STATE_DIR/sessions/broken"
run_disconnect "" wrong-network
check "an unreadable record fails the command, since it may still name the network" "1" "$RC"
check "and names that record" "yes" "$(says "sessions/broken" "$ERR")"
check "but the readable one is still fixed" "" "$(record_args demo_yolo)"

echo ""
echo "=== no terminal to ask on ==="
reset
STUB_PS=""
save_network_preference wrong-network
CCY_TTY="$SCRATCH/no-such-terminal" run_disconnect "" ""
check "no terminal: it says there was no terminal to ask on" "yes" "$(says "no terminal" "$ERR")"
check "and says to name the network instead" "yes" "$(says "ccy --disconnect wrong-network" "$ERR")"
check "and bash's raw redirection error is not what the user reads" "no" "$(says "no-such-terminal: No such file or directory" "$ERR")"
check "but the reason is kept" "yes" "$(says "(No such file or directory)" "$ERR")"

echo ""
echo "=== --connect: an engine that cannot list its networks ==="
# A `network ls` that fails is not an answer. Read as one it says "Network not found", "no
# longer exists" or "No user-defined networks found", and none of those is the cause.
reset
STUB_LS_FAILS=1
OUT="$(connect_to_network right-network "_yolo" "ccy" 2>&1)"
RC=$?
check "named --connect fails when network ls fails" "1" "$RC"
check "and says the networks could not be listed" "yes" "$(says "could not list its networks" "$OUT")"
check "and does not claim the network is missing" "no" "$(says "Network not found" "$OUT")"
check "and connects nothing" "" "$(calls)"
check "and saves nothing" "(none)" "$(saved)"

reset
STUB_LS_FAILS=1
save_network_preference right-network
OUT="$(connect_to_network "" "_yolo" "ccy" 2>&1 </dev/null)"
RC=$?
check "bare --connect with a saved default fails when network ls fails" "1" "$RC"
check "and does not claim the saved network is gone" "no" "$(says "no longer exists" "$OUT")"
check "and says the networks could not be listed" "yes" "$(says "could not list its networks" "$OUT")"

reset
STUB_LS_FAILS=1
OUT="$(connect_to_network "" "_yolo" "ccy" 2>&1 </dev/null)"
RC=$?
check "bare --connect with no saved default fails when network ls fails" "1" "$RC"
check "and does not claim there are no networks" "no" "$(says "No user-defined networks found" "$OUT")"
check "and says the networks could not be listed" "yes" "$(says "could not list its networks" "$OUT")"

echo ""
echo "passed: $passed   failed: $failed"
[ "$failed" -eq 0 ]
