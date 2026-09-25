#!/usr/bin/env bash
# Test the workspace relabel preflight (CCY 3.68.0).
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
# The container this runs in stands in for the host: files are chowned to a uid the stub
# podman's uid map leaves out, so they are "foreign" exactly as root's are on a desktop.
# A pseudo-terminal from script(1) answers the prompt.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

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
STUB_BIN="$WORK/bin"
mkdir -p "$STUB_BIN"
cat >"$STUB_BIN/podman" <<'STUB'
#!/usr/bin/env bash
# Only the one call the preflight makes; anything else is a test defect.
if [ "$*" = "unshare cat /proc/self/uid_map" ]; then
    if [ "${STUB_MAP_RC:-0}" -ne 0 ]; then
        echo "Error: stub podman failed" >&2
        exit "$STUB_MAP_RC"
    fi
    printf '%s\n' "$STUB_UID_MAP"
    exit 0
fi
echo "stub podman: unexpected call: $*" >&2
exit 99
STUB
cat >"$STUB_BIN/sudo" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_SUDO_LOG"
exec "$@"
STUB
chmod +x "$STUB_BIN/podman" "$STUB_BIN/sudo"

ME="$(id -u)"
MYGROUP="$(id -g)"
FOREIGN=4242
# The stub map covers this uid and 100-199, and leaves FOREIGN out.
STUB_UID_MAP="$(printf '0 %s 1\n1 100 100' "$ME")"
LABEL="system_u:object_r:container_file_t:s0"

# Runs workspace_relabel_preflight in a fresh shell with the libraries sourced from this
# repo, on a pseudo-terminal fed <answer>. LABELLED lists paths whose label already is the
# shared one; everything else reads as user_home_t, as a desktop project does before its
# first relabelled launch. Prints the combined output, then "rc=<n>" on the last line.
run_preflight() {
    local dir="$1" answer="$2" relabel="${3-z}" labelled="${4:-}"
    local driver="$WORK/driver.bash"
    cat >"$driver" <<DRIVER
set -uo pipefail
export PATH="$STUB_BIN:\$PATH" CCY_CONTAINER_ENGINE=podman
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
    STUB_UID_MAP="$STUB_UID_MAP" STUB_SUDO_LOG="$WORK/sudo.log" STUB_MAP_RC="${STUB_MAP_RC:-0}" \
        script -qec "bash $(printf '%q' "$driver")" /dev/null <<<"$answer" 2>&1 | tr -d '\r'
}
last_rc() { printf '%s\n' "$1" | awk '/^rc=/ { rc = $0 } END { print rc }'; }
fresh_project() {
    local p="$WORK/project-$1"
    rm -rf "$p"
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
    p="$(fresh_project clean)"
    out="$(run_preflight "$p" "")"
    check "a project you own entirely starts, with no question" "rc=0" "$(last_rc "$out")"
    check "  and prints nothing" "rc=0" "$(printf '%s\n' "$out" | awk 'NF' | paste -sd'|')"

    p="$(fresh_project off)"
    chown -R "$FOREIGN:$FOREIGN" "$p/plans/triage-runs"
    out="$(run_preflight "$p" "" "")"
    check "no relabel (SELinux off): nothing to check, even with foreign entries" "rc=0" "$(last_rc "$out")"

    p="$(fresh_project refuse)"
    chown -R "$FOREIGN:$FOREIGN" "$p/plans/triage-runs"
    out="$(run_preflight "$p" "n")"
    check "foreign entries, answered n: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  the problem is explained" "podman cannot relabel 3 entries" "$out"
    contains "  each entry is named with its owner" "($FOREIGN:$FOREIGN)  $p/plans/triage-runs/run-1" "$out"
    contains "  the look command is printed" \
        "find $p \\! \\( -uid $ME -o \\( -uid +99 -uid -200 \\) \\) -printf" "$out"
    contains "  the fix command is printed" \
        "sudo find $p \\! \\( -uid $ME -o \\( -uid +99 -uid -200 \\) \\) -exec chown -h $ME:$MYGROUP {} +" "$out"
    contains "  the question is the registered prompt" "$CCY_PROMPT_RELABEL_FIX" "$out"
    check "  nothing was changed" "$FOREIGN" "$(stat -c %u "$p/plans/triage-runs/run-1/log")"
    check "  sudo was not run" "no" "$([ -s "$WORK/sudo.log" ] && echo yes || echo no)"

    out="$(run_preflight "$p" "")"
    check "foreign entries, Enter alone is no" "rc=1" "$(last_rc "$out")"

    out="$(run_preflight "$p" "$(printf 'maybe\nwhat\nhuh')")"
    check "three answers that are neither y nor n: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  each is re-asked" "Answer y or n." "$out"
    check "  and nothing was changed" "$FOREIGN" "$(stat -c %u "$p/plans/triage-runs/run-1/log")"

    out="$(run_preflight "$p" "$(printf 'maybe\ny')")"
    check "a bad answer then y: fixed, and the launch goes on" "rc=0" "$(last_rc "$out")"
    check "  the entries are yours now" "$ME:$MYGROUP $ME:$MYGROUP $ME:$MYGROUP" \
        "$(stat -c %u:%g "$p/plans/triage-runs" "$p/plans/triage-runs/run-1" "$p/plans/triage-runs/run-1/log" | paste -sd' ')"
    check "  sudo ran the printed fix" \
        "find $p ! ( -uid $ME -o ( -uid +99 -uid -200 ) ) -exec chown -h $ME:$MYGROUP {} +" \
        "$(cat "$WORK/sudo.log")"
    contains "  and says so" "now yours" "$out"
    : >"$WORK/sudo.log"

    p="$(fresh_project subuid)"
    chown -R 150:150 "$p/plans"
    out="$(run_preflight "$p" "")"
    check "entries owned by a subordinate uid are relabelled by podman: no question" "rc=0" "$(last_rc "$out")"

    p="$(fresh_project labelled)"
    chown -R "$FOREIGN:$FOREIGN" "$p/plans/triage-runs"
    out="$(run_preflight "$p" "" z "$p $p/plans/triage-runs $p/plans/triage-runs/run-1 $p/plans/triage-runs/run-1/log")"
    check "foreign entries already carrying the shared label, in a labelled project: podman skips them" \
        "rc=0" "$(last_rc "$out")"

    out="$(run_preflight "$p" "n" z "$p/plans/triage-runs $p/plans/triage-runs/run-1 $p/plans/triage-runs/run-1/log")"
    check "the same entries in a project not yet labelled: podman relabels every entry, so they block" \
        "rc=1" "$(last_rc "$out")"

    out="$(STUB_MAP_RC=3 run_preflight "$p" "")"
    check "podman cannot report its uid map: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  and says why" "cannot read the uid map" "$out"

    p="$(fresh_project notty)"
    chown -R "$FOREIGN:$FOREIGN" "$p/plans/triage-runs"
    out="$(PATH="$STUB_BIN:$PATH" CCY_CONTAINER_ENGINE=podman STUB_UID_MAP="$STUB_UID_MAP" bash -c "
        source '$LIB_DIR/common.bash'
        file_selinux_label() { echo user_home_t; }
        CCY_MOUNT_RELABEL=z
        workspace_relabel_preflight '$p' </dev/null
        echo rc=\$?" 2>&1)"
    check "no terminal to ask on: the launch stops" "rc=1" "$(last_rc "$out")"
    contains "  and points at the printed fix" "no terminal to ask on" "$out"
    check "  and nothing was changed" "$FOREIGN" "$(stat -c %u "$p/plans/triage-runs/run-1/log")"
fi

echo ""
echo "=== the launcher runs it ==="
launch_line="$(awk '/workspace_relabel_preflight "\$PWD"/ { print NR; exit }' "$LAUNCHER")"
mounts_line="$(awk '/^DOCKER_MOUNTS=\(/ { print NR; exit }' "$LAUNCHER")"
check "the launcher calls the preflight on the workspace" "yes" "$([ -n "$launch_line" ] && echo yes || echo no)"
check "  before the workspace mount is built" "yes" \
    "$([ -n "$launch_line" ] && [ -n "$mounts_line" ] && [ "$launch_line" -lt "$mounts_line" ] && echo yes || echo no)"
check "  and stops the launch when it refuses" "yes" \
    "$(awk -v n="$launch_line" 'NR == n && /\|\| exit 1/ { found = 1 } END { print found ? "yes" : "no" }' "$LAUNCHER")"

echo ""
echo "ccy relabel-preflight: passed: $passed  failed: $failed"
if [ "$failed" -ne 0 ]; then
    exit 1
fi
