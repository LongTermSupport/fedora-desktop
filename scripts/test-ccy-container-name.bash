#!/usr/bin/env bash
# Test how ccy's launcher chooses its container name (get_next_container_name in
# files/var/local/claude-yolo/lib/common.bash, which asks `ccy-teams internal
# claim-container-name`).
#
# WHY THIS EXISTS. Two launches started at the same moment in one project (two sessions opened
# together, or a boot restore of several) both listed the containers, both found the same lowest
# free suffix, and the second failed at podman's create with "the container name ... is already
# in use" (exit 125). The name is chosen long before podman creates the container, so the choice
# has to be a claim: a second launcher must not be handed a name the first holds until the first
# launcher's process has ended. The fake podman here sleeps inside `ps` to hold the race window
# open, and its create refuses a name already created, as the real one does.
#
# Every launch is its own `bash -c` process, as real launches are: the claim belongs to the
# launcher's process ($$), and background subshells of one shell would share it.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"

for tool in go timeout; do
    if [ -z "$(command -v "$tool")" ]; then
        echo "FAIL: $tool is needed by this test" >&2
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

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/ccy-container-name-test.XXXXXX")"
cleanup() { rm -rf "$SCRATCH"; }
trap cleanup EXIT

BIN="$SCRATCH/bin"
mkdir -p "$BIN"
echo "== the Go binary"
build_out="$(cd "$REPO_ROOT/src-go" && go build -trimpath -buildvcs=false -ldflags "-X main.RootDir=$REPO_ROOT" -o "$BIN/ccy-teams" ./cmd/ccy-teams 2>&1)"
build_rc=$?
check "ccy-teams builds" "0" "$build_rc"
if [ "$build_rc" -ne 0 ]; then
    echo "$build_out" >&2
    exit 1
fi

# The fake engine: containers are directories in $TEST_STATE/containers. `ps` lists them after
# $TEST_PS_SLEEP seconds, or fails when $TEST_STATE/ps-fails exists; `create --name N` makes N
# with mkdir, which refuses a name already made, and then fails with podman's message and status.
cat >"$BIN/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s %s\n' "$(basename "$0")" "$*" >>"$TEST_STATE/calls.log"
case "$1" in
ps)
    [ ! -e "$TEST_STATE/ps-fails" ] || { echo "Error: the engine is down" >&2; exit 125; }
    sleep "${TEST_PS_SLEEP:-0}"
    ls -1 "$TEST_STATE/containers" ;;
create)
    name="$3"
    if ! mkdir "$TEST_STATE/containers/$name" 2>>"$TEST_STATE/mkdir.err"; then
        echo "Error: creating container storage: the container name \"$name\" is already in use by 0123abcd. You have to remove that container to be able to reuse that name: that name is already in use" >&2
        exit 125
    fi ;;
*) echo "fake podman: unexpected call: $*" >&2; exit 97 ;;
esac
EOF
chmod +x "$BIN/podman"
cp "$BIN/podman" "$BIN/docker"

reset_world() {
    rm -rf "$SCRATCH/state" "$SCRATCH/run"
    mkdir -p "$SCRATCH/state/containers" "$SCRATCH/run"
    chmod 700 "$SCRATCH/run"
    : >"$SCRATCH/state/calls.log"
}

export TEST_STATE="$SCRATCH/state"
export XDG_RUNTIME_DIR="$SCRATCH/run"
export CCY_LIB="$LIB_DIR"
export PATH="$BIN:$PATH"
export LIB_DIR

# One launch: choose the name as the launcher does, then create the container. Prints the name,
# or "create-failed <name>" (podman's message goes to $TEST_STATE/create.err); the launcher's
# process lives on for $2 seconds after creating, as a session's would.
cat >"$SCRATCH/launch.bash" <<'EOF'
source "$LIB_DIR/common.bash" || exit 90
name=$(get_next_container_name "$1" yolo) || exit 91
if podman create --name "$name" 2>>"$TEST_STATE/create.err"; then echo "$name"; else echo "create-failed $name"; fi
sleep "${2:-0}"
EOF
launch() { timeout 60 bash "$SCRATCH/launch.bash" "$@"; }

# A relaunch: the restart path execs the launcher again in the same process, so one process
# chooses twice.
cat >"$SCRATCH/relaunch.bash" <<'EOF'
source "$LIB_DIR/common.bash" || exit 90
a=$(get_next_container_name proj yolo) || exit 91
b=$(get_next_container_name proj yolo) || exit 92
echo "$a $b"
EOF

echo "== two launches at once in one project choose different names"
reset_world
mkdir "$TEST_STATE/containers/proj_yolo"
export TEST_PS_SLEEP=0.5
launch proj 2 >"$SCRATCH/a.out" &
pid_a=$!
launch proj 2 >"$SCRATCH/b.out" &
pid_b=$!
wait "$pid_a"
wait "$pid_b"
got="$(sort "$SCRATCH/a.out" "$SCRATCH/b.out" | tr '\n' ' ')"
check "both created, as _1 and _2" "proj_yolo_1 proj_yolo_2 " "$got"

echo "== four launches at once all start"
reset_world
for i in 1 2 3 4; do
    launch proj 2 >"$SCRATCH/l$i.out" &
done
wait
got="$(sort "$SCRATCH"/l?.out | tr '\n' ' ')"
check "four distinct names, none refused" "proj_yolo proj_yolo_1 proj_yolo_2 proj_yolo_3 " "$got"
unset TEST_PS_SLEEP

echo "== a name is free again once its launcher has gone and its container with it"
reset_world
got="$(launch proj 0)"
check "the first launch takes the base name" "proj_yolo" "$got"
rm -rf "$TEST_STATE/containers/proj_yolo"
got="$(launch proj 0)"
check "the next launch gets it back" "proj_yolo" "$got"

echo "== an existing container keeps its name taken even with no launcher"
reset_world
mkdir "$TEST_STATE/containers/proj_yolo" "$TEST_STATE/containers/proj_yolo_1"
got="$(launch proj 0)"
check "the lowest free suffix" "proj_yolo_2" "$got"

echo "== a relaunch by exec (the same process) gets its own claim back"
reset_world
got="$(timeout 60 bash "$SCRATCH/relaunch.bash")"
check "the same name both times" "proj_yolo proj_yolo" "$got"

echo "== another project is not affected"
reset_world
launch proj 2 >"$SCRATCH/p.out" &
pid_p=$!
sleep 0.5
got="$(launch other 0)"
wait "$pid_p"
check "the other project takes its base name" "other_yolo" "$got"

echo "== docker sessions use the same claim"
reset_world
mkdir "$TEST_STATE/containers/proj_yolo"
got="$(CCY_CONTAINER_ENGINE=docker launch proj 0)"
check "docker asked, next free name" "proj_yolo_1" "$got"
check "the engine asked was docker" "docker ps -a --format {{.Names}}" "$(head -n 1 "$TEST_STATE/calls.log")"

echo "== the claim fails loudly"
reset_world
touch "$TEST_STATE/ps-fails"
err="$(launch proj 0 2>&1 >/dev/null)"
rc=$?
check "an engine that cannot list containers stops the launch" "91" "$rc"
check "and says why" "yes" "$([[ "$err" == *"the engine is down"* ]] && echo yes || echo "no: $err")"
rm -f "$TEST_STATE/ps-fails"
err="$(XDG_RUNTIME_DIR="" launch proj 0 2>&1 >/dev/null)"
rc=$?
check "no runtime directory stops the launch" "91" "$rc"
check "and says why" "yes" "$([[ "$err" == *XDG_RUNTIME_DIR* ]] && echo yes || echo "no: $err")"

echo ""
echo "passed: $passed"
echo "failed: $failed"
[ "$failed" -eq 0 ]
