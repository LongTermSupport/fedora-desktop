#!/usr/bin/env bash
# Unit-test the container limits, lib/container-limits.bash: the limit set every ccy container is
# started with (CCY_LIMIT_ARGS): the agents slice, the pids limit (CCY_PIDS_LIMIT) and the memory
# caps (CCY_MEMORY_MAX, else the machine's RAM less what the desktop keeps).
#
# Sources the library from THIS repo, and checks the launcher calls it and passes the set on
# the session container's run, so the text under test is the text that ships.
#
# WHY THIS TEST EXISTS (docs/ccy.md, "Container limits"). Each case below is a way a limit could
# again be left to the engine (no value, a value the engine reads as unlimited or rejects only
# after the session is built), picked silently for a machine too small for it, or applied to
# something else. That the settled values reach the session's tmux pane is tested in
# test-ccy-token-handoff.bash, beside the token hand-off.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
LIB="$CCY_DIR/lib/container-limits.bash"
LAUNCHER="$CCY_DIR/claude-yolo"

if [ ! -f "$LIB" ]; then
    echo "FAIL: library not found at $LIB" >&2
    exit 1
fi
print_error() { printf 'ERROR: %s\n' "$*" >&2; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# The slice's unit, where the play installs it: an XDG_CONFIG_HOME of the test's own, and a
# HOME of its own for the case with no XDG_CONFIG_HOME.
mkdir -p "$work/config/systemd/user" "$work/home/.config/systemd/user"
printf '[Slice]\n' >"$work/config/systemd/user/agents.slice"
printf '[Slice]\n' >"$work/home/.config/systemd/user/agents.slice"
export XDG_CONFIG_HOME="$work/config"

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}

# limits [value|-] — ccy_container_limits in a fresh shell with CCY_PIDS_LIMIT set to the value
# ("-" leaves it unset): its status and the limit it settled on, as "<rc> <limit>".
limits() {
    (
        # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
        source "$LIB"
        if [ "$1" = - ]; then unset CCY_PIDS_LIMIT; else CCY_PIDS_LIMIT="$1"; fi
        CCY_MEMORY_MAX=4g
        rc=0
        ccy_container_limits 2>"$work/err" || rc=$?
        printf '%s %s' "$rc" "${CCY_PIDS_LIMIT:-}"
    )
}

# memory <MemTotal kB> [value|-] — ccy_memory_limit over a fake /proc/meminfo with that MemTotal
# and CCY_MEMORY_MAX set to the value: "<rc> <MiB printed>".
memory() {
    printf 'MemFree:  123 kB\nMemTotal:       %s kB\nSwapTotal: 0 kB\n' "$1" >"$work/meminfo"
    (
        # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
        source "$LIB"
        if [ "${2:--}" = - ]; then unset CCY_MEMORY_MAX; else CCY_MEMORY_MAX="$2"; fi
        rc=0
        out=$(ccy_memory_limit "$work/meminfo" 2>"$work/err") || rc=$?
        printf '%s %s' "$rc" "$out"
    )
}

GIB64=67108864

echo "=== the pids limit: the default ==="
check "unset: the documented default" "0 16384" "$(limits -)"
check "empty: the default, as for every other CCY_ setting" "0 16384" "$(limits '')"
check "the default is above podman's 2048 and still a bound a fork bomb stops at" "yes" "$(
    # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
    source "$LIB"
    [ "$CCY_PIDS_LIMIT_DEFAULT" -gt 2048 ] && [ "$CCY_PIDS_LIMIT_DEFAULT" -le "$CCY_PIDS_LIMIT_MAX" ] && echo yes
)"

echo "=== the pids limit: an override ==="
check "a whole number in range is used as given" "0 8192" "$(limits 8192)"
check "the smallest allowed" "0 256" "$(limits 256)"
check "the largest allowed" "0 32768" "$(limits 32768)"

echo "=== the pids limit: refused before anything is built; it cannot be turned off ==="
for bad in 0 -1 max unlimited none 2k 1e4 ' 4096' '4096 ' 04096 255 32769 4194304 99999999 '4096;id'; do
    got=$(limits "$bad")
    check "'$bad' refused" "1" "${got%% *}"
done
limits nope >/dev/null
check "the refusal names the variable and what it takes" "1" "$(grep -c 'CCY_PIDS_LIMIT=nope is not a pids limit: a whole number from 256 to 32768; it cannot be turned off' "$work/err")"

echo "=== the memory cap: from the machine's RAM ==="
check "64 GiB: the RAM less the desktop's 16 GiB" "0 49152" "$(memory "$GIB64")"
check "32 GiB: 16 GiB" "0 16384" "$(memory 33554432)"
check "20 GiB: exactly the 4 GiB floor" "0 4096" "$(memory 20971520)"
check "an empty override is no override" "0 49152" "$(memory "$GIB64" '')"
for small in 20971519 16777216 8388608; do
    check "$small kB: under the floor, refused" "1 " "$(memory "$small")"
done
memory 16777216 >/dev/null
check "  the refusal names CCY_MEMORY_MAX and the floor, and picks no value" "1" "$(grep -c 'this machine has 16384 MiB of RAM: less the 16384 MiB kept for the desktop, that is under the 4096 MiB floor.*Set CCY_MEMORY_MAX on this host' "$work/err")"
printf 'MemFree: 1 kB\n' >"$work/nototal"
check "a meminfo with no MemTotal is refused" "1" "$(
    # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
    source "$LIB"
    ccy_memory_limit "$work/nototal" 2>/dev/null
    echo "$?"
)"

echo "=== the memory cap: an override ==="
for pair in 8g:8192 8G:8192 8192m:8192 8192M:8192 4g:4096 64g:65536 8388608k:8192 8589934592:8192 8589934592b:8192 8589935000:8192 40000m:40000; do
    check "CCY_MEMORY_MAX=${pair%%:*}" "0 ${pair#*:}" "$(memory "$GIB64" "${pair%%:*}")"
done

echo "=== the memory cap: refused; it cannot be turned off ==="
for bad in 0 -1 max 00 3g 4095m 65g 65537m 1t '8 g' ' 8g' '8g ' 8gb 8.5g 08g +8g 1e10 9999999999999g 8gi; do
    check "CCY_MEMORY_MAX='$bad' refused" "1 " "$(memory "$GIB64" "$bad")"
done
memory "$GIB64" max >/dev/null
check "the refusal names the variable, what it takes and the machine's RAM" "1" "$(grep -c 'CCY_MEMORY_MAX=max is not a memory cap: bytes, or a number with k, m or g, from 4096m to this machine.s 65536 MiB of RAM; it cannot be turned off' "$work/err")"

echo "=== the slice and the argument set ==="
check "the set every run passes: slice, pids, memory and the soft limit 2 GiB below" \
    "--cgroup-parent agents.slice --pids-limit 16384 --memory 4096m --memory-reservation 2048m|4096m" "$(
        # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
        source "$LIB"
        unset CCY_PIDS_LIMIT
        CCY_MEMORY_MAX=4g
        ccy_container_limits 2>/dev/null && printf '%s|%s' "${CCY_LIMIT_ARGS[*]}" "$CCY_MEMORY_MAX"
    )"
check "no slice unit: refused, naming the play" "1|1" "$(
    # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
    source "$LIB"
    CCY_MEMORY_MAX=4g XDG_CONFIG_HOME="$work/none" ccy_container_limits 2>"$work/err"
    printf '%s|%s' "$?" "$(grep -c "agents.slice is not installed ($work/none/systemd/user/agents.slice is missing).*Deploy it with playbooks/imports/play-claude-yolo.yml" "$work/err")"
)"

check "with no XDG_CONFIG_HOME, the unit is looked for under HOME/.config" "0|1" "$(
    # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
    source "$LIB"
    XDG_CONFIG_HOME='' HOME="$work/home" ccy_agents_slice_check 2>/dev/null
    printf '%s|' "$?"
    XDG_CONFIG_HOME='' HOME="$work/none" ccy_agents_slice_check 2>/dev/null
    echo "$?"
)"

echo "=== the launcher ==="
check "the launcher lists the library in CCY_LIBS" "1" "$(grep -c -E '^CCY_LIBS=\(.* container-limits( |\))' "$LAUNCHER")"
check "the launcher sources it" "1" "$(grep -c -x -F "source \"\$SCRIPT_DIR/lib/container-limits.bash\"" "$LAUNCHER")"
# Straight after the library is loaded: before arguments are read, --help's probe and --teams'
# seat checks included, and above every engine run in the file.
check "the limits are settled as soon as it is loaded, before any container runs" "yes" "$(awk '/^source "[$]SCRIPT_DIR\/lib\/container-limits[.]bash"$/ {s=NR} /^ccy_container_limits [|][|] exit 1$/ {l=NR} /(container_cmd|ccy_agent_container) run / && !r {r=NR} END {print (s && l == s + 1 && l < r) ? "yes" : "no"}' "$LAUNCHER")"
check "the session container's run passes the whole set" "1" "$(grep -c -F "ccy_agent_container run \$DOCKER_FLAGS --rm \"\${CCY_LIMIT_ARGS[@]}\" \\" "$LAUNCHER")"

echo "=== the slice's unit, as the play renders it ==="
SLICE_TEMPLATE="$REPO_ROOT/files/home/.config/systemd/user/agents.slice.j2"
PLAY="$REPO_ROOT/playbooks/imports/play-claude-yolo.yml"
# The play's two values, rendered into the template by name: what Ansible writes for a machine
# whose facts give memtotal_mb 65536 (64 GiB) and no override.
rendered=""
if [ -f "$SLICE_TEMPLATE" ]; then
    rendered=$(<"$SLICE_TEMPLATE")
    rendered=${rendered//"{{ ccy_agents_slice_max_mb }}"/49152}
    rendered=${rendered//"{{ ccy_agents_slice_high_mb }}"/48128}
fi
check "the unit caps the slice at the max, and throttles 1 GiB below it" \
    "[Unit]|Description=ccy containers, capped together (docs/ccy.md, Container limits)|[Slice]|MemoryMax=49152M|MemoryHigh=48128M" \
    "$(grep -v -e '^#' -e '^$' <<<"$rendered" | paste -sd '|')"
check "the unit sets nothing for systemd-oomd" "0" "$(grep -c '^ManagedOOM' <<<"$rendered")"
check "the template holds no other placeholder" "0" "$(grep -c '{{' <<<"$rendered")"
# The play's arithmetic is the library's: the slice's max is the RAM less the desktop's share,
# which is a lone container's default cap, and the throttle is 1 GiB below it.
# shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
CCY_MEMORY_DESKTOP_MIB_EXPECTED=$(source "$LIB" && echo "$CCY_MEMORY_DESKTOP_MIB")
# shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
CCY_MEMORY_FLOOR_MIB_EXPECTED=$(source "$LIB" && echo "$CCY_MEMORY_FLOOR_MIB")
check "the play's max is the RAM less the library's desktop share, or the host's override" "1" \
    "$(grep -c -x -F "    ccy_agents_slice_max_mb: \"{{ (ccy_agents_slice_memory_max_mb | default((ansible_facts['memtotal_mb'] | int) - $CCY_MEMORY_DESKTOP_MIB_EXPECTED)) | int }}\"" "$PLAY")"
check "the play's throttle is 1 GiB below the max" "1" \
    "$(grep -c -x -F "    ccy_agents_slice_high_mb: \"{{ (ccy_agents_slice_max_mb | int) - 1024 }}\"" "$PLAY")"
check "the play refuses a max under the library's floor" "1" \
    "$(grep -c -x -F "          - ccy_agents_slice_max_mb | int >= $CCY_MEMORY_FLOOR_MIB_EXPECTED" "$PLAY")"
check "the play installs the unit where the library looks for it" "1" \
    "$(grep -c -x -F '        dest: "/home/{{ user_login }}/.config/systemd/user/agents.slice"' "$PLAY")"

echo "=== the user manager: no memory-pressure kill ==="
# Fedora's systemd-oomd-defaults gives user@.service ManagedOOMMemoryPressure=kill (50% for
# 20 s) in user@.service.d/10-oomd-user-service-defaults.conf; oomd then kills the leaf cgroup
# with the highest reclaim rate anywhere under user@UID.service, desktop apps included, and the
# slice's MemoryHigh throttle is pressure that rolls up to it. The drop-in turns that off.
OOMD_DROPIN_NAME=50-no-memory-pressure-kill.conf
OOMD_DROPIN="$REPO_ROOT/files/etc/systemd/system/user@.service.d/$OOMD_DROPIN_NAME"
check "the drop-in sets user@.service's ManagedOOMMemoryPressure to auto" "[Service]|ManagedOOMMemoryPressure=auto" \
    "$(grep -v -e '^#' -e '^$' "$OOMD_DROPIN" 2>/dev/null | paste -sd '|')"
check "it is read after Fedora's 10-oomd-user-service-defaults.conf, so it wins" "yes" \
    "$(LC_ALL=C; [[ "$OOMD_DROPIN_NAME" > 10-oomd-user-service-defaults.conf ]] && echo yes || echo no)"
TWEAKS="$REPO_ROOT/playbooks/imports/play-systemd-user-tweaks.yml"
check "the play installs it beside the system's user@.service" "1" \
    "$(grep -c -x -F "        dest: /etc/systemd/system/user@.service.d/$OOMD_DROPIN_NAME" "$TWEAKS")"
check "the play no longer writes the user manager's user.slice drop-in, which no cgroup reads" "0" \
    "$(grep -c -F 'blockinfile' "$TWEAKS")"
check "the play reads the value back from user@UID.service" "1" \
    "$(grep -c -F -e "- \"user@{{ ansible_facts['getent_passwd'][user_login][1] }}.service\"" "$TWEAKS")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
