#!/usr/bin/env bash
# Test the workspace relabel preflight, workspace_relabel_preflight in lib/common.bash.
#
# Sources the launcher's libraries from THIS repo (not the deployed /var/local copy).
#
# WHY THIS TEST EXISTS. ccy mounts the project with SELinux's shared relabel (:z), and
# rootless podman relabels every entry under it inside its user namespace, where it may
# relabel only what a uid that namespace maps owns. One root-owned entry in a project made
# podman refuse the container — `lsetxattr(label=…) …: operation not permitted`, exit 126 —
# so ccy could not start there at all. The preflight finds those entries first, explains
# them, prints the commands, and offers to give them to the user.
#
# No fixture is chowned: that needs root, and this runs as whoever runs qa-all. Every
# fixture belongs to the user running it, and the stub podman's uid map decides who is
# foreign. MAP_FOREIGN leaves this user out, so each fixture entry is foreign exactly as a
# root-owned one is on a desktop. Every walk before the fix is the real find. The stub sudo
# runs the fix and marks it done; the stub find then reports what a walk would find once
# the entries are the user's: nothing. A pseudo-terminal from script(1) answers the prompt.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"
REAL_FIND="$(command -v find)"

WORK="$(mktemp -d)"
cleanup() {
    chmod -R u+rwx "$WORK"
    rm -rf "$WORK"
}
trap cleanup EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label"
        echo "        wanted: $(printf '%q' "$want")"
        echo "        got:    $(printf '%q' "$got")"
    fi
}
contains() {
    local label="$1" needle="$2" haystack="$3"
    if [[ "$haystack" == *"$needle"* ]]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label (no $(printf '%q' "$needle") in the output)"
        printf '%s\n' "$haystack" | awk '{ print "        | " $0 }'
    fi
}
lacks() {
    local label="$1" needle="$2" haystack="$3"
    if [[ "$haystack" != *"$needle"* ]]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label ($(printf '%q' "$needle") is in the output)"
        printf '%s\n' "$haystack" | awk '{ print "        | " $0 }'
    fi
}

# ── the pure part: which owners are outside the namespace ────────────────────────────────
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$LIB_DIR/common-pure.bash"

echo "=== relabel_foreign_owner_args ==="
if [ "$(declare -F relabel_foreign_owner_args)" != "relabel_foreign_owner_args" ]; then
    failed=$((failed + 1))
    echo "  FAIL: relabel_foreign_owner_args is not defined after sourcing common-pure.bash"
else
    args() { relabel_foreign_owner_args "$1" | paste -sd' '; }
    DESKTOP_MAP="$(printf '%10s %10s %10s\n' 0 1000 1 1 524288 65536)"
    check "a desktop's map: you, or your subordinate range" \
        '! ( -uid 1000 -o ( -uid +524287 -uid -589824 ) )' "$(args "$DESKTOP_MAP")"
    check "a range starting at uid 0 has no lower bound" \
        '! ( -uid -10 )' "$(args '0 0 10')"
    check "a one-uid range at 0 is that uid" '! ( -uid 0 )' "$(args '0 0 1')"
    relabel_foreign_owner_args "" >/dev/null
    check "an empty map is refused, not read as 'everyone is foreign'" "1" "$?"
    relabel_foreign_owner_args "Error: cannot connect to podman" >/dev/null
    check "error text where the map should be is refused" "1" "$?"
    relabel_foreign_owner_args "0 1000" >/dev/null
    check "a line short of its count is refused" "1" "$?"
fi

# ── the launcher-facing part, against a stub podman and sudo ─────────────────────────────
ME="$(id -u)"
MYGROUP="$(id -g)"
OTHER=$((ME + 7))
MAP_MINE="$(printf '0 %s 1\n1 200000 100' "$ME")"
MAP_FOREIGN="$(printf '0 %s 1\n1 200000 100' "$OTHER")"
FOREIGN_ARGS="\\! \\( -uid $OTHER -o \\( -uid +199999 -uid -200100 \\) \\)"
LABEL="system_u:object_r:container_file_t:s0"

STUB_BIN="$WORK/bin"
mkdir -p "$STUB_BIN"
cat >"$STUB_BIN/podman" <<'STUB'
#!/usr/bin/env bash
# Only the one call the preflight makes; anything else is a test defect.
printf '%s\n' "$*" >>"$STUB_PODMAN_LOG"
if [ "$*" = "unshare cat /proc/self/uid_map" ]; then
    if [ -n "${STUB_MAP_WARNING:-}" ]; then
        echo "$STUB_MAP_WARNING" >&2
    fi
    if [ "${STUB_MAP_RC:-0}" -ne 0 ]; then
        echo "Error: stub podman failed" >&2
        exit "$STUB_MAP_RC"
    fi
    cat "$STUB_MAP_FILE"
    exit 0
fi
echo "stub podman: unexpected call: $*" >&2
exit 99
STUB
cat >"$STUB_BIN/docker" <<'STUB'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >>"$STUB_PODMAN_LOG"
exit 0
STUB
cat >"$STUB_BIN/sudo" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_SUDO_LOG"
[ "${STUB_SUDO_RC:-0}" -eq 0 ] || exit "$STUB_SUDO_RC"
"$@" || exit
touch "$STUB_FIXED_MARK"
STUB
# The real find until the stub sudo has run the fix. After it: nothing is foreign any
# more, unless STUB_STILL_FOREIGN keeps the real walk, or STUB_WALK_AFTER_FIX_RC fails it.
cat >"$STUB_BIN/find" <<STUB
#!/usr/bin/env bash
if [ -e "\$STUB_FIXED_MARK" ] && [ "\${STUB_STILL_FOREIGN:-0}" != 1 ]; then
    if [ "\${STUB_WALK_AFTER_FIX_RC:-0}" -ne 0 ]; then
        echo "find: stub failure on the walk after the fix" >&2
        exit "\$STUB_WALK_AFTER_FIX_RC"
    fi
    exit 0
fi
exec "$REAL_FIND" "\$@"
STUB
# A find that fails outright.
FAIL_BIN="$WORK/fail-bin"
mkdir -p "$FAIL_BIN"
printf '#!/usr/bin/env bash\necho "find: stub failure" >&2\nexit 1\n' >"$FAIL_BIN/find"
chmod +x "$STUB_BIN"/* "$FAIL_BIN/find"

export STUB_PODMAN_LOG="$WORK/podman.log" STUB_SUDO_LOG="$WORK/sudo.log"
export STUB_MAP_FILE="$WORK/uid_map" STUB_FIXED_MARK="$WORK/fixed"
# Per-case switches, set before a case and put back after it.
export STUB_SUDO_RC=0 STUB_STILL_FOREIGN=0 STUB_WALK_AFTER_FIX_RC=0 STUB_MAP_RC=0 STUB_MAP_WARNING=""
EXTRA_PATH=""
ENGINE=""
set_map() { printf '%s\n' "$1" >"$STUB_MAP_FILE"; }
reset_logs() {
    : >"$STUB_PODMAN_LOG"
    : >"$STUB_SUDO_LOG"
    rm -f "$STUB_FIXED_MARK"
}

# Runs workspace_relabel_preflight in a fresh shell with the libraries sourced from this
# repo, on a pseudo-terminal fed <answer>. LABELLED lists paths whose label already is the
# shared one; everything else reads as user_home_t, as a desktop project does before its
# first relabelled launch. EXTRA_PATH, when set, goes in front of the stubs; ENGINE picks
# the engine. Prints the combined output, then "rc=<n>" on the last line.
run_preflight() {
    local dir="$1" answer="$2" relabel="${3-z}" labelled="${4:-}"
    local driver="$WORK/driver.bash"
    cat >"$driver" <<DRIVER
set -uo pipefail
export PATH="${EXTRA_PATH:+$EXTRA_PATH:}$STUB_BIN:\$PATH" CCY_CONTAINER_ENGINE="${ENGINE:-podman}"
source "$LIB_DIR/common.bash"
file_selinux_label() {
    case " $labelled " in
        *" \$1 "*) printf '%s\n' "$LABEL" ;;
        *) printf '%s\n' "unconfined_u:object_r:user_home_t:s0" ;;
    esac
}
CCY_MOUNT_RELABEL="$relabel"
workspace_relabel_preflight "$dir"
echo "rc=\$?"
DRIVER
    script -qec "bash $(printf '%q' "$driver")" /dev/null <<<"$answer" 2>&1 | tr -d '\r'
}
last_rc() { printf '%s\n' "$1" | awk '/^rc=/ { rc = $0 } END { print rc }'; }
fresh_project() {
    local p="$WORK/project-$1"
    mkdir -p "$p/plans/triage-runs/run-1" "$p/src"
    touch "$p/src/main.sh" "$p/plans/triage-runs/run-1/log"
    printf '%s\n' "$p"
}

echo ""
echo "=== workspace_relabel_preflight ==="
defined="$(PATH="$STUB_BIN:$PATH" bash -c "source '$LIB_DIR/common.bash'; declare -F workspace_relabel_preflight" 2>&1)"
if [ "$defined" != "workspace_relabel_preflight" ]; then
    failed=$((failed + 1))
    echo "  FAIL: workspace_relabel_preflight is not defined after sourcing common.bash"
else
    p="$(fresh_project main)"
    check "every fixture entry is this user's, so only the map makes one foreign" "$ME" \
        "$("$REAL_FIND" "$p" -printf '%U\n' | sort -u | paste -sd' ')"
    entries="$("$REAL_FIND" "$p" | wc -l)"

    reset_logs
    set_map "$MAP_MINE"
    out="$(run_preflight "$p" "")"
    check "a project the map covers entirely starts, with no question" "rc=0" "$(last_rc "$out")"
    check "  and prints nothing" "rc=0" "$(printf '%s\n' "$out" | awk 'NF' | paste -sd'|')"

    reset_logs
    set_map "$MAP_FOREIGN"
    out="$(run_preflight "$p" "" "")"
    check "no relabel (SELinux off, or a forwarded agent): nothing to check, even with foreign entries" \
        "rc=0" "$(last_rc "$out")"
    check "  podman is not even asked" "" "$(cat "$STUB_PODMAN_LOG")"

    reset_logs
    ENGINE=docker
    out="$(run_preflight "$p" "")"
    ENGINE=""
    check "docker: no podman unshare to read a map from, so nothing is checked" "rc=0" "$(last_rc "$out")"
    check "  and no engine is asked" "" "$(cat "$STUB_PODMAN_LOG")"

    reset_logs
    out="$(run_preflight "$p" "n")"
    check "foreign entries, answered n: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  the problem is explained" "podman cannot relabel this project" "$out"
    contains "  the foreign entries are counted" "$entries entries belong to someone else" "$out"
    contains "  each entry is named with its owner" "($ME:$MYGROUP)  $p/plans/triage-runs/run-1" "$out"
    contains "  the look command is printed" "find $p $FOREIGN_ARGS -printf" "$out"
    contains "  the fix command is printed" \
        "sudo find $p $FOREIGN_ARGS -exec chown -h $ME:$MYGROUP {} +" "$out"
    contains "  the question is the registered prompt" "$CCY_PROMPT_RELABEL_FIX" "$out"
    contains "  a no says what to do next" "Run the fix, or answer y next time" "$out"
    check "  sudo was not run" "" "$(cat "$STUB_SUDO_LOG")"

    reset_logs
    out="$(run_preflight "$p" "")"
    check "foreign entries, Enter alone is no" "rc=1" "$(last_rc "$out")"

    reset_logs
    out="$(run_preflight "$p" "$(printf 'maybe\nwhat\nhuh')")"
    check "three answers that are neither y nor n: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  each is re-asked" "Answer y or n." "$out"
    contains "  and giving up says so, not 'answered no'" "no y or n after 3 answers" "$out"
    check "  sudo was not run" "" "$(cat "$STUB_SUDO_LOG")"

    reset_logs
    out="$(run_preflight "$p" "$(printf 'maybe\ny')")"
    check "a bad answer then y: fixed, and the launch goes on" "rc=0" "$(last_rc "$out")"
    check "  sudo ran the printed fix" \
        "find $p ! ( -uid $OTHER -o ( -uid +199999 -uid -200100 ) ) -exec chown -h $ME:$MYGROUP {} +" \
        "$(cat "$STUB_SUDO_LOG")"
    contains "  and says so" "now yours" "$out"

    reset_logs
    set_map "$MAP_FOREIGN"
    STUB_SUDO_RC=1
    out="$(run_preflight "$p" "y")"
    STUB_SUDO_RC=0
    check "y, but the fix fails: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  and says so" "the fix failed" "$out"

    reset_logs
    STUB_STILL_FOREIGN=1
    out="$(run_preflight "$p" "y")"
    STUB_STILL_FOREIGN=0
    check "y, the fix runs, but the entries are still foreign: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  and says so" "still not yours after the fix" "$out"

    reset_logs
    STUB_WALK_AFTER_FIX_RC=1
    out="$(run_preflight "$p" "y")"
    STUB_WALK_AFTER_FIX_RC=0
    check "y, the fix runs, but the walk after it fails: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  and says so" "could not look through $p again" "$out"
    lacks "  and does not claim the entries are fixed" "now yours" "$out"

    reset_logs
    set_map "$(printf '0 %s 1\n1 %s 5' "$OTHER" "$ME")"
    out="$(run_preflight "$p" "")"
    check "entries owned by a subordinate uid are relabelled by podman: no question" "rc=0" "$(last_rc "$out")"

    reset_logs
    set_map "$MAP_FOREIGN"
    all="$("$REAL_FIND" "$p" | paste -sd' ')"
    out="$(run_preflight "$p" "" z "$all")"
    check "foreign entries already carrying the shared label, in a labelled project: podman skips them" \
        "rc=0" "$(last_rc "$out")"

    out="$(run_preflight "$p" "n" z "${all#"$p" }")"
    check "the same entries in a project not yet labelled: podman relabels every entry, so they block" \
        "rc=1" "$(last_rc "$out")"

    reset_logs
    set_map "$MAP_MINE"
    STUB_MAP_RC=3
    out="$(run_preflight "$p" "")"
    STUB_MAP_RC=0
    check "podman cannot report its uid map: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  and says why" "cannot read the uid map" "$out"

    reset_logs
    STUB_MAP_WARNING='level=warning msg="stub warning"'
    out="$(run_preflight "$p" "")"
    STUB_MAP_WARNING=""
    check "a warning from podman is not read as part of the map" "rc=0" "$(last_rc "$out")"

    reset_logs
    EXTRA_PATH="$FAIL_BIN"
    out="$(run_preflight "$p" "")"
    EXTRA_PATH=""
    check "the walk fails: the launch stops rather than guess" "rc=1" "$(last_rc "$out")"
    contains "  and says so" "could not look through all of $p" "$out"

    p="$(fresh_project closed)"
    mkdir -p "$p/db-data/base"
    touch "$p/db-data/base/table"
    chmod 000 "$p/db-data"
    reset_logs
    set_map "$MAP_MINE"
    out="$(run_preflight "$p" "")"
    check "a directory this user cannot open, owned inside the map: passed over, the launch goes on" \
        "rc=0" "$(last_rc "$out")"
    lacks "  with no find error" "Permission denied" "$out"

    reset_logs
    set_map "$MAP_FOREIGN"
    out="$(run_preflight "$p" "n")"
    check "the same directory, owned outside the map: it blocks" "rc=1" "$(last_rc "$out")"
    contains "  and it is listed" "  $p/db-data" "$out"
    lacks "  with no find error" "Permission denied" "$out"
    chmod 755 "$p/db-data"

    p="$(fresh_project notty)"
    reset_logs
    set_map "$MAP_FOREIGN"
    out="$(PATH="$STUB_BIN:$PATH" CCY_CONTAINER_ENGINE=podman bash -c "
        source '$LIB_DIR/common.bash'
        file_selinux_label() { echo user_home_t; }
        CCY_MOUNT_RELABEL=z
        workspace_relabel_preflight '$p' </dev/null
        echo rc=\$?" 2>&1)"
    check "no terminal to ask on: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  and points at the printed fix" "no terminal to ask on" "$out"
    check "  and sudo was not run" "" "$(cat "$STUB_SUDO_LOG")"
fi

echo ""
echo "=== the launcher runs it ==="
launch_line="$(awk '/workspace_relabel_preflight "\$PWD"/ { print NR; exit }' "$LAUNCHER")"
mounts_line="$(awk '/^DOCKER_MOUNTS=\(/ { print NR; exit }' "$LAUNCHER")"
forward_line="$(awk '/^if \[ "\$\{SSH_AGENT_FORWARDED:-0\}" = "1" \]; then$/ { f = NR }
    f && NR == f + 1 && /^    CCY_MOUNT_RELABEL=""$/ { print f; exit }' "$LAUNCHER")"
ssh_line="$(awk '/^build_ssh_mounts_and_validate "ccy"/ { print NR; exit }' "$LAUNCHER")"
check "the launcher calls the preflight on the workspace" "yes" "$([ -n "$launch_line" ] && echo yes || echo no)"
check "  before the workspace mount is built" "yes" \
    "$([ -n "$launch_line" ] && [ -n "$mounts_line" ] && [ "$launch_line" -lt "$mounts_line" ] && echo yes || echo no)"
check "  and stops the launch when it refuses" "yes" \
    "$(awk -v n="$launch_line" 'NR == n && /\|\| exit 1/ { found = 1 } END { print found ? "yes" : "no" }' "$LAUNCHER")"
check "a forwarded agent (labelling disabled) drops the relabel, once the agent is known and before the preflight" \
    "yes" "$([ -n "$forward_line" ] && [ -n "$ssh_line" ] && [ -n "$launch_line" ] \
        && [ "$ssh_line" -lt "$forward_line" ] && [ "$forward_line" -lt "$launch_line" ] && echo yes || echo no)"

echo ""
echo "ccy relabel-preflight: passed: $passed  failed: $failed"
if [ "$failed" -ne 0 ]; then
    exit 1
fi
