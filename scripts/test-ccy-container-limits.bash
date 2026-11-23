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

# memory <MemTotal kB> [value|-] [slice text] — ccy_memory_limit over a fake /proc/meminfo with
# that MemTotal, a slice unit holding the text (default: the play's unit for 64 GiB) and
# CCY_MEMORY_MAX set to the value: "<rc> <MiB printed>".
memory() {
    printf 'MemFree:  123 kB\nMemTotal:       %s kB\nSwapTotal: 0 kB\n' "$1" >"$work/meminfo"
    printf '%b' "${3-[Unit]\nDescription=x\n\n[Slice]\nMemoryMax=49152M\nMemoryHigh=48128M\n}" >"$work/slice"
    (
        # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
        source "$LIB"
        if [ "${2:--}" = - ]; then unset CCY_MEMORY_MAX; else CCY_MEMORY_MAX="$2"; fi
        rc=0
        out=$(ccy_memory_limit "$work/meminfo" "$work/slice" 2>"$work/err") || rc=$?
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

echo "=== the memory cap: the slice's MemoryMax ==="
check "the play's unit on 64 GiB: its MemoryMax" "0 49152" "$(memory "$GIB64")"
check "an empty override is no override" "0 49152" "$(memory "$GIB64" '')"
check "exactly the 2 GiB floor" "0 2048" "$(memory 4194304 - '[Slice]\nMemoryMax=2048M\nMemoryHigh=1536M\n')"
check "the last MemoryMax wins, as systemd reads it" "0 6144" "$(memory "$GIB64" - '[Slice]\nMemoryMax=8192M\nMemoryMax=6144M\n')"
check "a last line with no newline is read" "0 4096" "$(memory "$GIB64" - '[Slice]\nMemoryMax=4096M')"
for bad in '' '[Slice]\n' '[Slice]\nMemoryMax=infinity\n' '[Slice]\nMemoryMax=8G\n' '[Slice]\nMemoryMax=0M\n' '[Slice]\nMemoryMax= 4096M\n' '[Slice]\n#MemoryMax=4096M\n'; do
    check "slice '$bad': no MemoryMax in the play's form, refused" "1 " "$(memory "$GIB64" - "$bad")"
done
memory "$GIB64" - '[Slice]\n' >/dev/null
check "  the refusal names MemoryMax and the play" "1" "$(grep -c 'agents.slice has no MemoryMax=<MiB>M line.*Deploy it with playbooks/imports/play-claude-yolo.yml' "$work/err")"
for bad in '[Slice]\nMemoryMax=2047M\n' '[Slice]\nMemoryMax=65537M\n'; do
    check "slice '$bad': out of bounds, refused" "1 " "$(memory "$GIB64" - "$bad")"
done
memory 8388608 - '[Slice]\nMemoryMax=9000M\n' >/dev/null
check "  the refusal names the host_vars setting, the floor and the RAM, and picks no value" "1" "$(grep -c "agents.slice's MemoryMax is 9000 MiB.*from the 2048 MiB floor to this machine's 8192 MiB of RAM. Set ccy_agents_slice_memory_max_mb in this host's host_vars" "$work/err")"
check "an override needs no MemoryMax in the slice" "0 3072" "$(memory "$GIB64" 3g '[Slice]\n')"
check "a slice unit that cannot be read is refused, naming it" "1|1" "$(
    # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
    source "$LIB"
    unset CCY_MEMORY_MAX
    ccy_memory_limit "$work/meminfo" "$work/no-such-unit" 2>"$work/err" >/dev/null
    printf '%s|%s' "$?" "$(grep -c "could not read $work/no-such-unit" "$work/err")"
)"
printf 'MemFree: 1 kB\n' >"$work/nototal"
check "a meminfo with no MemTotal is refused" "1" "$(
    # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
    source "$LIB"
    ccy_memory_limit "$work/nototal" 2>/dev/null
    echo "$?"
)"

echo "=== the memory cap: an override ==="
for pair in 8g:8192 8G:8192 8192m:8192 8192M:8192 4g:4096 2g:2048 3g:3072 64g:65536 8388608k:8192 8589934592:8192 8589934592b:8192 8589935000:8192 40000m:40000; do
    check "CCY_MEMORY_MAX=${pair%%:*}" "0 ${pair#*:}" "$(memory "$GIB64" "${pair%%:*}")"
done

echo "=== the memory cap: refused; it cannot be turned off ==="
for bad in 0 -1 max 00 1g 2047m 65g 65537m 1t '8 g' ' 8g' '8g ' 8gb 8.5g 08g +8g 1e10 9999999999999g 8gi; do
    check "CCY_MEMORY_MAX='$bad' refused" "1 " "$(memory "$GIB64" "$bad")"
done
memory "$GIB64" max >/dev/null
check "the refusal names the variable, what it takes and the machine's RAM" "1" "$(grep -c 'CCY_MEMORY_MAX=max is not a memory cap: bytes, or a number with k, m or g, from 2048m to this machine.s 65536 MiB of RAM; it cannot be turned off' "$work/err")"

echo "=== the slice and the argument set ==="
# settled <CCY_MEMORY_MAX> — the set ccy_container_limits fills, and the cap it settled.
settled() {
    (
        # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
        source "$LIB"
        unset CCY_PIDS_LIMIT
        CCY_MEMORY_MAX="$1"
        ccy_container_limits 2>/dev/null && printf '%s|%s' "${CCY_LIMIT_ARGS[*]}" "$CCY_MEMORY_MAX"
    )
}
check "the set every run passes: slice, pids, memory and the soft limit 2 GiB below" \
    "--cgroup-parent agents.slice --pids-limit 16384 --memory 8192m --memory-reservation 6144m|8192m" "$(settled 8g)"
check "a cap under 4 GiB: the soft limit half below" \
    "--cgroup-parent agents.slice --pids-limit 16384 --memory 3000m --memory-reservation 1500m|3000m" "$(settled 3000m)"
check "at the floor: the soft limit half below" \
    "--cgroup-parent agents.slice --pids-limit 16384 --memory 2048m --memory-reservation 1024m|2048m" "$(settled 2g)"
printf '[Slice]\nMemoryMax=3072M\nMemoryHigh=2304M\n' >"$work/config/systemd/user/agents.slice"
check "no override: the installed slice's MemoryMax, read where the play puts it" \
    "--cgroup-parent agents.slice --pids-limit 16384 --memory 3072m --memory-reservation 1536m|3072m" "$(settled '')"
printf '[Slice]\n' >"$work/config/systemd/user/agents.slice"
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
# The slice's max is a lone container's default cap (the launcher reads it): the host's
# override, else the RAM less the desktop's 16 GiB, else 4 GiB or three quarters of the RAM.
# The throttle is 1 GiB below it, or a quarter below on a slice under 4 GiB.
# shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
CCY_MEMORY_FLOOR_MIB_EXPECTED=$(source "$LIB" && echo "$CCY_MEMORY_FLOOR_MIB")
# shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/container-limits.bash
CCY_AGENTS_SLICE_SETTING_EXPECTED=$(source "$LIB" && echo "$CCY_AGENTS_SLICE_SETTING")
check "the play's RAM is the host's facts" "1" \
    "$(grep -c -x -F "    ccy_agents_slice_ram_mb: \"{{ ansible_facts['memtotal_mb'] | int }}\"" "$PLAY")"
check "the play's max is the host's override (the setting the library names), else the default" "1" \
    "$(grep -c -x -F "    ccy_agents_slice_max_mb: \"{{ ($CCY_AGENTS_SLICE_SETTING_EXPECTED | default([(ccy_agents_slice_ram_mb | int) - 16384, [(ccy_agents_slice_ram_mb | int) * 3 // 4, 4096] | min] | max)) | int }}\"" "$PLAY")"
check "the play's throttle is 1 GiB below the max, or a quarter where that is less" "1" \
    "$(grep -c -x -F "    ccy_agents_slice_high_mb: \"{{ (ccy_agents_slice_max_mb | int) - ([1024, (ccy_agents_slice_max_mb | int) // 4] | min) }}\"" "$PLAY")"
# The default, worked the way the play's expression is, for the machines it must serve: each
# is at or over the floor, at or under the RAM, and never shrinks as the RAM grows.
slice_default() { local a=$(($1 - 16384)) b=$(($1 * 3 / 4)); b=$((b < 4096 ? b : 4096)); echo $((a > b ? a : b)); }
previous=0 ok=yes
for ram in 3700 3900 7800 11900 16000 20479 20480 20481 24000 26000 28000 32000 65536; do
    d=$(slice_default "$ram")
    { [ "$d" -ge "$CCY_MEMORY_FLOOR_MIB_EXPECTED" ] && [ "$d" -le "$ram" ] && [ "$d" -ge "$previous" ]; } || ok="no at $ram MiB: $d"
    previous=$d
done
check "the default fits every machine from a 4 GiB server up and only grows with the RAM" "yes" "$ok"
check "  a 4 GiB server (3900 MiB): three quarters" "2925" "$(slice_default 3900)"
check "  an 8 GiB server: 4 GiB" "4096" "$(slice_default 7800)"
check "  a 28 GiB machine: the RAM less 16 GiB, as before" "11616" "$(slice_default 28000)"
check "the play refuses a max under the library's floor" "1" \
    "$(grep -c -x -F "          - ccy_agents_slice_max_mb | int >= $CCY_MEMORY_FLOOR_MIB_EXPECTED" "$PLAY")"
check "the play refuses a max over the RAM" "1" \
    "$(grep -c -x -F "          - ccy_agents_slice_max_mb | int <= ccy_agents_slice_ram_mb | int" "$PLAY")"
# A refused machine is left as it was: the assert is the play's first task, before any install.
check "the play asserts the slice's size before its first task changes anything" \
    "- name: Assert The Agents Slice Leaves Room For A Container" \
    "$(awk '/^  tasks:$/ {t=1; next} t && /^    - name: / {sub(/^    /, ""); print; exit}' "$PLAY")"
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
