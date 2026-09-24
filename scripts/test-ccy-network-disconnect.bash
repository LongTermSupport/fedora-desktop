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
# The container engine is a stub function. It answers `ps`, `inspect` and `network ls` from
# the fixture below, and records every `network connect`/`disconnect` it is asked for, so a
# case can assert that a refusal made no change at all.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"

for lib in common-pure.bash network-management.bash; do
    if [ ! -f "$LIB_DIR/$lib" ]; then
        echo "FAIL: $lib not found at $LIB_DIR/$lib" >&2
        exit 1
    fi
done

# shellcheck source=/dev/null
source "$LIB_DIR/common-pure.bash"
# shellcheck source=/dev/null
source "$LIB_DIR/network-management.bash"

SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT
export HOME="$SCRATCH/home"
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
# STUB_NETS[n]     the networks container n is attached to, space-separated
# STUB_ALL_NETS    every network the engine knows, space-separated, for `network ls`
# STUB_FAIL_VERB   "disconnect" or "connect": that verb fails with the engine's own message
CALLS="$SCRATCH/calls"
declare -A STUB_NETS=()
container_cmd() {
    local words=()
    case "$1 ${2:-}" in
        "ps --format")
            [ -n "$STUB_PS" ] && printf '%s\n' "$STUB_PS"
            return 0
            ;;
        "network ls")
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
    if [ "$1" = "inspect" ]; then
        if [ -z "${STUB_NETS[$2]+set}" ]; then
            echo "Error: no such container $2" >&2
            return 125
        fi
        read -r -a words <<<"${STUB_NETS[$2]}"
        [ "${#words[@]}" -gt 0 ] && printf '%s\n' "${words[@]}"
        return 0
    fi
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
    rm -f "$(get_network_persistence_file)"
}
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

reset
STUB_PS=""
save_network_preference wrong-network
run_disconnect "" ""
check "with no container and no name, the saved default is cleared" "(none)" "$(saved)"

reset
STUB_PS=""
save_network_preference right-network
run_disconnect "" wrong-network
check "with no container, a name that is not the saved default is refused" "1" "$RC"
check "and the saved default is kept" "right-network" "$(saved)"

echo ""
echo "=== --connect says what it saved, and how to undo it ==="
reset
OUT="$(connect_to_network right-network "_yolo" "ccy" 2>&1)"
check "a connect saves the default" "right-network" "$(saved)"
check "and says every plain launch will reconnect" "yes" "$(says "every plain ccy launch in this project connects to it" "$OUT")"
check "and names the undo" "yes" "$(says "ccy --disconnect right-network" "$OUT")"

echo ""
echo "passed: $passed   failed: $failed"
[ "$failed" -eq 0 ]
