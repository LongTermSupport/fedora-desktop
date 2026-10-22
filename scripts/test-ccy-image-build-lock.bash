#!/usr/bin/env bash
# Test ccy's image build lock: files/var/local/claude-yolo/lib/image-build-lock.bash (the lock,
# its record and the wait), lib/image-build.bash (the checks a launch re-runs once it holds the
# lock), the launcher's own project build and in-place update run under it, and the Go front's
# `ccy --build-lock`, which reads the record and stops a hung build. Plan 00216.
#
# WHY THIS EXISTS. Two ccy sessions started at the same moment each rebuilt the container image.
# Only one launch may build an image at a time; every other waits for that build to finish and
# then uses its result without building again. The fake podman here builds slowly, writes every
# build and commit to a log, and keeps its images as files, so each race is a real one: every
# launch is its own bash process, the lock is a real flock and the wait a real wait.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

for tool in go timeout flock; do
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
# has <label> <file> <text> — the file holds the text; lacks — it does not.
has() {
    if grep -qF -- "$3" "$2"; then check "$1" yes yes; else check "$1" yes "no: $(cat "$2")"; fi
}
lacks() {
    if grep -qF -- "$3" "$2"; then check "$1" absent "present: $(cat "$2")"; else check "$1" absent absent; fi
}

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/ccy-image-build-lock-test.XXXXXX")"
# Output a case does not judge goes here, never to /dev/null, so a failing case can be read.
QUIET="$SCRATCH/quiet.log"
kill_fakes() {
    local pid
    if [ -f "$SCRATCH/state/podman.pids" ]; then
        while read -r pid; do kill -9 "$pid" 2>>"$QUIET"; done <"$SCRATCH/state/podman.pids"
    fi
    if [ -f "$SCRATCH/state/linger.pid" ]; then
        kill -9 "$(cat "$SCRATCH/state/linger.pid")" 2>>"$QUIET"
    fi
}
cleanup() {
    kill_fakes
    rm -rf "$SCRATCH"
}
trap cleanup EXIT

BIN="$SCRATCH/bin"
mkdir -p "$BIN"
echo "== the Go front"
build_out="$(cd "$REPO_ROOT/src-go" && go build -trimpath -buildvcs=false -ldflags "-X main.Version=test" -o "$BIN/claude-yolo" ./cmd/claude-yolo 2>&1)"
build_rc=$?
check "claude-yolo builds" "0" "$build_rc"
if [ "$build_rc" -ne 0 ]; then
    echo "$build_out" >&2
    exit 1
fi

# The fake engine. An image is a file in $TEST_STATE/images holding "<id> <version> <hash>
# <Claude Code version>": a build has Claude Code 2.0.0, a commit (the in-place update) 2.0.1,
# and `run --entrypoint claude <image> --version` says which.
# `build` and `commit` append a line to builds.log naming the image and the launch (its parent,
# the launcher's bash), sleep $TEST_BUILD_SLEEP, then write the image; `build` fails when
# $TEST_STATE/build-fails exists, and with $TEST_BUILD_LINGER leaves a child behind that
# outlives it, as podman's pause process can. Every fake's pid goes to podman.pids for cleanup.
cat >"$BIN/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo "$$" >>"$TEST_STATE/podman.pids"
img_file() { printf '%s/images/%s' "$TEST_STATE" "${1//[:\/]/_}"; }
case "$1" in
image)
    shift 2
    format="" name=""
    while [ $# -gt 0 ]; do
        case "$1" in
        --format) format="$2"; shift 2 ;;
        *) name="$1"; shift ;;
        esac
    done
    f="$(img_file "$name")"
    [ -f "$f" ] || { echo "Error: $name: image not known" >&2; exit 125; }
    read -r id version hash claude <"$f"
    case "$format" in
    *claude-yolo-version*) echo "$version" ;;
    *dockerfile-hash*) echo "$hash" ;;
    *.Id*) echo "$id" ;;
    *) echo "[]" ;;
    esac ;;
build)
    shift
    tag="" hash="" dockerfile="" ctx=""
    while [ $# -gt 0 ]; do
        case "$1" in
        -t) tag="$2"; shift 2 ;;
        -f) dockerfile="$2"; shift 2 ;;
        --build-arg) case "$2" in DOCKERFILE_HASH=*) hash="${2#DOCKERFILE_HASH=}" ;; esac; shift 2 ;;
        --target) shift 2 ;;
        --*) shift ;;
        *) ctx="$1"; shift ;;
        esac
    done
    [ -n "$dockerfile" ] || dockerfile="$ctx/Dockerfile"
    version="$(awk -F'"' '/^LABEL claude-yolo-version=/ {print $2}' "$dockerfile")"
    echo "build $tag by $PPID" >>"$TEST_STATE/builds.log"
    if [ -n "${TEST_BUILD_LINGER:-}" ]; then
        sleep 300 </dev/null >>"$TEST_STATE/linger.log" 2>&1 &
        echo "$!" >"$TEST_STATE/linger.pid"
    fi
    sleep "${TEST_BUILD_SLEEP:-0}"
    if [ -e "$TEST_STATE/build-fails" ]; then echo "Error: the build failed" >&2; exit 1; fi
    printf '%s %s %s 2.0.0\n' "id-$RANDOM$RANDOM" "${version:-0}" "${hash:-unknown}" >"$(img_file "$tag")" ;;
run)
    if [ "${!#}" = --version ]; then
        args=("$@")
        read -r id version hash claude <"$(img_file "${args[$# - 2]}")"
        echo "$claude (Claude Code)"
    fi ;;
commit)
    image="${!#}"
    echo "commit $image by $PPID" >>"$TEST_STATE/builds.log"
    sleep "${TEST_BUILD_SLEEP:-0}"
    f="$(img_file "$image")"
    read -r id version hash claude <"$f"
    printf '%s %s %s 2.0.1\n' "id-$RANDOM$RANDOM" "$version" "$hash" >"$f" ;;
rm) ;;
*) echo "fake podman: unexpected call: $*" >&2; exit 97 ;;
esac
EOF
chmod +x "$BIN/podman"

CONTEXT="$SCRATCH/context"
PROJECT="$SCRATCH/project"
mkdir -p "$CONTEXT" "$PROJECT/.claude/ccy"
reset_world() {
    kill_fakes
    rm -rf "$SCRATCH/state" "$SCRATCH/run"
    mkdir -p "$SCRATCH/state/images" "$SCRATCH/run"
    chmod 700 "$SCRATCH/run"
    : >"$SCRATCH/state/builds.log"
    printf 'FROM scratch\nLABEL claude-yolo-version="%s"\n' "${1:-9.9.9}" >"$CONTEXT/Dockerfile"
}

export TEST_STATE="$SCRATCH/state"
export TEST_CONTEXT="$CONTEXT"
export XDG_RUNTIME_DIR="$SCRATCH/run"
export CCY_IMAGE_BUILD_REPORT_SECONDS=1
export PATH="$BIN:$PATH"
export LIB_DIR
LOCK="$SCRATCH/run/ccy/image-builds/claude-yolo_latest.lock"

# The launcher's own functions that run under the lock, lifted from it: the in-place update
# (with the temporary container's removal) and the project image's build. A function ends at
# the first "}" indented as its opening line is.
lift() {
    awk -v start="$1" '
        !p && $0 ~ start { p = 1; match($0, /^ */); close_line = substr($0, 1, RLENGTH) "}" }
        p { print }
        p && $0 == close_line { exit }' "$LAUNCHER"
}
lift '^update_claude_inplace[(][)] [{]$' >"$SCRATCH/update.bash"
lift '^remove_temp_container[(][)] [{]$' >>"$SCRATCH/update.bash"
lift '^ +run_project_build[(][)] [{]$' >"$SCRATCH/project-build.bash"
# The launch scripts below call the lock as the launcher does; these are the launcher's own lines.
mapfile -t launcher_calls <<'EOF'
ccy_image_build_locked "$IMAGE_NAME" ccy_base_image_current "$REQUIRED_CONTAINER_VERSION" -- ccy_base_image_build ""
ccy_image_build_locked "$IMAGE_NAME" false -- ccy_base_image_build --no-cache
ccy_image_build_locked "$image_name" ccy_claude_is "$image_name" "$latest_version" -- update_claude_inplace "$image_name"
ccy_image_build_locked "$IMAGE_NAME" --unless-changed "$IMAGE_NAME" -- update_claude_inplace "$IMAGE_NAME"
project_current=(ccy_project_image_current "$rebuild_reason" "$PROJECT_IMAGE_NAME"
ccy_image_build_locked "$PROJECT_IMAGE_NAME" "${project_current[@]}" -- run_project_build
EOF
for line in "${launcher_calls[@]}"; do
    has "the launcher calls: ${line:0:60}" "$LAUNCHER" "$line"
done
check "update_claude_inplace was found in the launcher" yes "$(grep -q 'container_cmd commit' "$SCRATCH/update.bash" && echo yes || echo no)"
check "run_project_build was found in the launcher" yes "$(grep -q 'container_cmd build' "$SCRATCH/project-build.bash" && echo yes || echo no)"

# One launch's base image step, as the launcher runs it. $1 is the check (current: the
# launcher's own; force: a --rebuild, which always builds). Prints "ready <image id>".
cat >"$SCRATCH/base.bash" <<'EOF'
source "$LIB_DIR/common.bash" || exit 90
source "$LIB_DIR/image-build-lock.bash" || exit 91
source "$LIB_DIR/image-build.bash" || exit 92
IMAGE_NAME=claude-yolo:latest
BASE_IMAGE_NAME=claude-yolo:base
DOCKERFILE_DIR="$TEST_CONTEXT"
REQUIRED_CONTAINER_VERSION="${TEST_VERSION:-9.9.9}"
VERSION_CHECK_CACHE="$TEST_STATE/update-checks"
set -e
if [ "${1:-current}" = force ]; then
    ccy_image_build_locked "$IMAGE_NAME" false -- ccy_base_image_build --no-cache
else
    ccy_image_build_locked "$IMAGE_NAME" ccy_base_image_current "$REQUIRED_CONTAINER_VERSION" -- ccy_base_image_build ""
fi
echo "ready $(podman image inspect "$IMAGE_NAME" --format '{{.Id}}')"
EOF

# One launch's in-place update of claude-yolo:latest. $1 restart: as the restart path and
# --rebuild=claude run it (--unless-changed). $1 daily: as auto_update_claude_code runs it, which
# has asked the image's Claude Code version and the latest one first (ccy_claude_is).
cat >"$SCRATCH/update-launch.bash" <<'EOF'
source "$LIB_DIR/common.bash" || exit 90
source "$LIB_DIR/image-build-lock.bash" || exit 91
source "$LIB_DIR/image-build.bash" || exit 92
debug() { :; }
source "$TEST_UPDATE" || exit 93
set -e
if [ "$1" = daily ]; then
    podman run --rm --entrypoint claude claude-yolo:latest --version
    ccy_image_build_locked claude-yolo:latest ccy_claude_is claude-yolo:latest 2.0.1 -- update_claude_inplace claude-yolo:latest
else
    ccy_image_build_locked claude-yolo:latest --unless-changed claude-yolo:latest -- update_claude_inplace claude-yolo:latest
fi
echo "ready $(podman image inspect claude-yolo:latest --format '{{.Id}}')"
EOF
export TEST_UPDATE="$SCRATCH/update.bash"

# One launch's project image step: the launcher's check and its own build function.
cat >"$SCRATCH/project.bash" <<'EOF'
source "$LIB_DIR/common.bash" || exit 90
source "$LIB_DIR/image-build-lock.bash" || exit 91
source "$LIB_DIR/image-build.bash" || exit 92
source "$TEST_PROJECT_BUILD" || exit 93
cd "$TEST_PROJECT" || exit 94
PROJECT_IMAGE_NAME=claude-yolo:proj
PROJECT_DOCKERFILE=.claude/ccy/Dockerfile
PROJECT_DOCKERFILE_HASH_FILE="$TEST_STATE/proj-hash"
PROJECT_BASE_IMAGE_FILE="$TEST_STATE/proj-base"
current_project_hash="$(md5sum "$PROJECT_DOCKERFILE" | cut -c1-16)"
project_dockerfile_hash="$current_project_hash"
current_base_image=base-id-1
rebuild_reason="${1:-first time}"
build_log="$(mktemp "$TEST_STATE/build-XXXXXX.log")"
set -e
ccy_image_build_locked "$PROJECT_IMAGE_NAME" ccy_project_image_current "$rebuild_reason" "$PROJECT_IMAGE_NAME" \
    "$PROJECT_DOCKERFILE_HASH_FILE" "$current_project_hash" "$PROJECT_BASE_IMAGE_FILE" "$current_base_image" -- run_project_build
echo "ready $(podman image inspect "$PROJECT_IMAGE_NAME" --format '{{.Id}}')"
EOF
export TEST_PROJECT_BUILD="$SCRATCH/project-build.bash" TEST_PROJECT="$PROJECT"
printf 'FROM claude-yolo:latest\nRUN true\n' >"$PROJECT/.claude/ccy/Dockerfile"

run_launch() { timeout 60 bash "$SCRATCH/$1" "${@:2}"; }
builds() { wc -l <"$TEST_STATE/builds.log" | tr -d ' '; }
# distinct <out files> — how many different images the launches ended on ("ready <id>").
distinct() { grep -h '^ready ' "$@" | sort -u | wc -l | tr -d ' '; }
alive() { if kill -0 "$1" 2>>"$QUIET"; then echo yes; else echo no; fi; }
lock_free() { if flock -n "$LOCK" true; then echo yes; else echo no; fi; }
# wait_for <pid> <file> <text> — up to 10 s for the text to appear in the file, written by the
# launch <pid>; a launch that has ended first will never write it.
wait_for() {
    local tries=100
    while [ "$tries" -gt 0 ]; do
        if grep -qF -- "$3" "$2" 2>>"$QUIET"; then return 0; fi
        if ! kill -0 "$1" 2>>"$QUIET"; then
            echo "  (launch $1 ended before '$3' appeared in $2)" >&2
            return 1
        fi
        sleep 0.1
        tries=$((tries - 1))
    done
    echo "  (gave up waiting for '$3' in $2)" >&2
    return 1
}
holder_pid() { awk '{print $1}' "$LOCK"; }
# race <script> [args] — two launches of the script at once; prints both exit codes.
race() {
    run_launch "$@" >"$SCRATCH/a.out" 2>"$SCRATCH/a.err" &
    local pid_a=$! rc_a rc_b
    run_launch "$@" >"$SCRATCH/b.out" 2>"$SCRATCH/b.err" &
    local pid_b=$!
    wait "$pid_a"
    rc_a=$?
    wait "$pid_b"
    rc_b=$?
    cat "$SCRATCH/a.err" "$SCRATCH/b.err" >"$SCRATCH/ab.err"
    echo "$rc_a $rc_b"
}

echo "== two launches at once build the missing image once"
reset_world
export TEST_BUILD_SLEEP=2
check "both launches succeed" "0 0" "$(race base.bash)"
check "one build_container_with_hash: the base stage and the full image, once" 2 "$(builds)"
check "both builds by one launch" 1 "$(awk '{print $4}' "$TEST_STATE/builds.log" | sort -u | wc -l | tr -d ' ')"
check "both launches use the same image" 1 "$(distinct "$SCRATCH/a.out" "$SCRATCH/b.out")"
has "the waiter said whose build it waits for" "$SCRATCH/ab.err" "to finish building claude-yolo:latest"
has "the waiter was told how to stop it" "$SCRATCH/ab.err" "ccy --build-lock"
has "the waiter said it used the holder's image" "$SCRATCH/ab.err" "claude-yolo:latest was built while this launch waited"
lacks "nothing on stdout but the result" "$SCRATCH/a.out" "waiting"
lacks "nothing on the other's stdout but the result" "$SCRATCH/b.out" "waiting"
check "the record is emptied once the build is done" 0 "$(wc -c <"$LOCK" | tr -d ' ')"
check "the lock directory is private" 700 "$(stat -c %a "$SCRATCH/run/ccy/image-builds")"
check "the lock file is private" 600 "$(stat -c %a "$LOCK")"

echo "== a launch that finds the image current builds nothing and waits for nothing"
run_launch base.bash >"$SCRATCH/c.out" 2>"$SCRATCH/c.err"
check "it succeeds" 0 "$?"
check "no build" 2 "$(builds)"
lacks "no wait" "$SCRATCH/c.err" "waiting"

echo "== a version bump: two launches at once rebuild once"
reset_world 9.9.9
run_launch base.bash >>"$QUIET" 2>&1
printf 'FROM scratch\nLABEL claude-yolo-version="9.9.10"\n' >"$CONTEXT/Dockerfile"
: >"$TEST_STATE/builds.log"
export TEST_VERSION=9.9.10
check "both launches succeed" "0 0" "$(race base.bash)"
check "one rebuild" 2 "$(builds)"
check "both use the rebuilt image" 1 "$(distinct "$SCRATCH/a.out" "$SCRATCH/b.out")"
unset TEST_VERSION

echo "== a forced rebuild waits for the build before it, then builds its own"
reset_world
run_launch base.bash force >>"$QUIET" 2>"$SCRATCH/a.err" &
pid_a=$!
wait_for "$pid_a" "$TEST_STATE/builds.log" "build claude-yolo:base"
run_launch base.bash force >>"$QUIET" 2>"$SCRATCH/b.err"
rc_b=$?
wait "$pid_a"
check "both succeed" "0 0" "$? $rc_b"
check "two whole builds, one after the other" 4 "$(builds)"
has "the second waited" "$SCRATCH/b.err" "to finish building claude-yolo:latest"

echo "== two in-place updates at once: one commit, both on its image"
reset_world
run_launch base.bash >>"$QUIET" 2>&1
: >"$TEST_STATE/builds.log"
check "both succeed" "0 0" "$(race update-launch.bash restart)"
check "one commit" 1 "$(grep -c '^commit claude-yolo:latest' "$TEST_STATE/builds.log")"
check "both use the updated image" 1 "$(distinct "$SCRATCH/a.out" "$SCRATCH/b.out")"

echo "== two daily updates at once: one commit, judged by the Claude Code version"
reset_world
run_launch base.bash >>"$QUIET" 2>&1
: >"$TEST_STATE/builds.log"
check "both succeed" "0 0" "$(race update-launch.bash daily)"
check "one commit" 1 "$(grep -c '^commit claude-yolo:latest' "$TEST_STATE/builds.log")"
check "both use the updated image" 1 "$(distinct "$SCRATCH/a.out" "$SCRATCH/b.out")"
run_launch update-launch.bash daily >>"$QUIET" 2>&1
check "an image already at the latest version is not updated again" 1 "$(grep -c '^commit claude-yolo:latest' "$TEST_STATE/builds.log")"

echo "== two project launches at once build the project image once"
reset_world
check "both succeed" "0 0" "$(race project.bash)"
check "one project build" 1 "$(grep -c '^build claude-yolo:proj' "$TEST_STATE/builds.log")"
check "both use it" 1 "$(distinct "$SCRATCH/a.out" "$SCRATCH/b.out")"
check "the hash file was written by the build, under the lock" "$(md5sum "$PROJECT/.claude/ccy/Dockerfile" | cut -c1-16)" "$(cat "$TEST_STATE/proj-hash")"
check "the project image's lock is its own" yes "$([ -f "$SCRATCH/run/ccy/image-builds/claude-yolo_proj.lock" ] && echo yes || echo no)"
run_launch project.bash "--rebuild flag" >>"$QUIET" 2>&1
check "a --rebuild of the project image builds again" 2 "$(grep -c '^build claude-yolo:proj' "$TEST_STATE/builds.log")"

echo "== builds of different images do not wait for each other"
reset_world
TEST_BUILD_SLEEP=4 run_launch base.bash >>"$QUIET" 2>&1 &
pid_a=$!
wait_for "$pid_a" "$TEST_STATE/builds.log" "build claude-yolo:base"
TEST_BUILD_SLEEP=0 run_launch project.bash >>"$QUIET" 2>"$SCRATCH/b.err"
check "the project build ran" 0 "$?"
check "while the base build still runs" yes "$(alive "$pid_a")"
lacks "without waiting" "$SCRATCH/b.err" "waiting"
wait "$pid_a"

echo "== a child the build leaves behind does not hold the lock"
reset_world
TEST_BUILD_LINGER=1 TEST_BUILD_SLEEP=0 run_launch base.bash force >>"$QUIET" 2>&1
check "the build succeeded" 0 "$?"
check "its child still runs" yes "$(alive "$(cat "$TEST_STATE/linger.pid")")"
check "the lock is free" yes "$(lock_free)"

echo "== a failed build fails the launch and frees the lock"
reset_world
touch "$TEST_STATE/build-fails"
TEST_BUILD_SLEEP=0 run_launch base.bash >>"$QUIET" 2>"$SCRATCH/a.err"
check "the launch fails" 1 "$?"
check "the lock is free" yes "$(lock_free)"
check "the record is emptied" 0 "$(wc -c <"$LOCK" | tr -d ' ')"
rm -f "$TEST_STATE/build-fails"

echo "== a holder killed mid-build: the waiter builds the image itself"
reset_world
TEST_BUILD_SLEEP=30 run_launch base.bash >>"$QUIET" 2>&1 &
pid_a=$!
wait_for "$pid_a" "$TEST_STATE/builds.log" "build claude-yolo:base"
victim="$(holder_pid)"
TEST_BUILD_SLEEP=0 run_launch base.bash >"$SCRATCH/b.out" 2>"$SCRATCH/b.err" &
pid_b=$!
wait_for "$pid_b" "$SCRATCH/b.err" "process $victim to finish building"
has "the waiter named the holder" "$SCRATCH/b.err" "process $victim to finish building claude-yolo:latest"
kill -9 "$victim"
check "the holder was killed" 0 "$?"
wait "$pid_b"
check "the waiter succeeds" 0 "$?"
check "the waiter built the image" 1 "$(grep -c "^build claude-yolo:latest by" "$TEST_STATE/builds.log")"
has "and uses it" "$SCRATCH/b.out" "ready id-"

echo "== a record whose process has gone is named stale and ignored"
reset_world
true &
dead=$!
wait "$dead"
mkdir -p "$(dirname "$LOCK")"
chmod 700 "$(dirname "$LOCK")"
printf '%s 1 1 claude-yolo:latest\n' "$dead" >"$LOCK"
flock "$LOCK" sleep 2 &
holder=$!
sleep 0.3
TEST_BUILD_SLEEP=0 run_launch base.bash force >>"$QUIET" 2>"$SCRATCH/b.err"
check "the waiter succeeds" 0 "$?"
has "it says the record is stale" "$SCRATCH/b.err" "process $dead, which has gone: that record is stale and is ignored"
wait "$holder"

echo "== the play's form: one build under the lock, from bash -c"
reset_world
# The script is the play's, word for word: its own $0 and $@, expanded by the inner bash.
play_script="source \"\$0\" && ccy_image_build_locked \"\$@\""
timeout 60 bash -c "$play_script" "$LIB_DIR/image-build-lock.bash" \
    claude-yolo:latest false -- podman build --build-arg DOCKERFILE_HASH=x -t claude-yolo:latest "$CONTEXT"
check "it succeeds" 0 "$?"
check "one build" 1 "$(builds)"
check "the record is emptied" 0 "$(wc -c <"$LOCK" | tr -d ' ')"

echo "== the lock refuses a directory that is not the user's own"
reset_world
mkdir -p "$SCRATCH/elsewhere" "$SCRATCH/run/ccy"
ln -s "$SCRATCH/elsewhere" "$SCRATCH/run/ccy/image-builds"
TEST_BUILD_SLEEP=0 run_launch base.bash force >>"$QUIET" 2>"$SCRATCH/a.err"
check "the launch fails" 1 "$?"
has "naming the lock" "$SCRATCH/a.err" "image build lock"
check "nothing was built" 0 "$(builds)"

echo "== ccy --build-lock"
reset_world
"$BIN/claude-yolo" --build-lock </dev/null >"$SCRATCH/s.out" 2>"$SCRATCH/s.err"
check "no build running: exit 0" 0 "$?"
has "it says so" "$SCRATCH/s.out" "No image build is running."
TEST_BUILD_SLEEP=30 run_launch base.bash force >>"$QUIET" 2>&1 &
pid_a=$!
wait_for "$pid_a" "$TEST_STATE/builds.log" "build claude-yolo:base"
victim="$(holder_pid)"
printf 'n\n' | "$BIN/claude-yolo" --build-lock >"$SCRATCH/s.out" 2>"$SCRATCH/s.err"
check "answered no: exit 0" 0 "$?"
has "it names the live holder" "$SCRATCH/s.out" "claude-yolo:latest is being built by process $victim"
has "it left it" "$SCRATCH/s.out" "Nothing was stopped."
check "the build still runs" yes "$(alive "$victim")"
printf 'y\n' | "$BIN/claude-yolo" --build-lock >"$SCRATCH/s.out" 2>"$SCRATCH/s.err"
check "answered yes: exit 0" 0 "$?"
has "it says the build was stopped" "$SCRATCH/s.out" "The build of claude-yolo:latest by process $victim was stopped."
check "the holder is gone at once" no "$(alive "$victim")"
check "the lock is free at once" yes "$(lock_free)"
if wait "$pid_a"; then stopped_rc=0; else stopped_rc=1; fi
check "the stopped launch failed" 1 "$stopped_rc"
: >"$TEST_STATE/builds.log"
TEST_BUILD_SLEEP=30 run_launch base.bash force >>"$QUIET" 2>&1 &
pid_a=$!
wait_for "$pid_a" "$TEST_STATE/builds.log" "build claude-yolo:base"
second="$(holder_pid)"
check "a new holder wrote its record" yes "$([ -n "$second" ] && [ "$second" != "$victim" ] && [ "$(alive "$second")" = yes ] && echo yes || echo "no: '$second'")"
kill -9 "$second"
check "the new holder was killed" 0 "$?"
wait "$pid_a"
"$BIN/claude-yolo" --build-lock </dev/null >"$SCRATCH/s.out" 2>"$SCRATCH/s.err"
check "a killed holder's record: exit 0" 0 "$?"
has "it is named stale, by its own pid" "$SCRATCH/s.out" "the record of process $second is stale"
has "and ignored" "$SCRATCH/s.out" "No image build is running."

printf 'passed: %d\nfailed: %d\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
