#!/usr/bin/env bash
# Drive the real files/usr/local/sbin/agent-bus-install against a temporary root
# (Plan 00161, DESIGN.md sections 3.2-3.7, unit U16).
#
# WHY THIS EXISTS. The installer is the one place a homeserver host is built: the binary
# behind its pinned hash, the units, the firewalld rules, the drop-in, the restart rule and
# the readiness step. Its decisions only show on a real host, so this runs the real script
# under AGENT_BUS_INSTALL_TEST_PREFIX, which roots every path it writes under a scratch
# directory and skips the root check, with stub systemctl, firewall-cmd, nmcli, curl, ip,
# ss, dnf, rpm, getent, useradd, zstd, uname, systemd-run and agent-bus first on PATH. The
# stubs keep their state in files and log every call; the agent-bus stub runs the real
# `render` commands from this checkout, so team-file validation is the real one.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TOOL="$REPO_ROOT/files/usr/local/sbin/agent-bus-install"

if [[ ! -x $TOOL ]]; then
    echo "FAIL: $TOOL is missing or not executable" >&2
    exit 1
fi
for tool in python3 jq sha256sum tar; do
    if ! command -v "$tool" >/dev/null; then
        echo "FAIL: $tool is not on PATH; this test needs it" >&2
        exit 1
    fi
done

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [[ $got == "$want" ]]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %q\n        got:  %q\n' "$label" "$want" "$got" >&2
    fi
}
says() {
    if grep -qE -- "$1" "$2"; then echo yes; else echo no; fi
}
count() {
    grep -cE -- "$1" "$2"
}
yes_if() {
    if "$@"; then echo yes; else echo no; fi
}

SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT
STUBS="$SCRATCH/stubs"
mkdir -p "$STUBS"
OUT="$SCRATCH/out"
ERR="$SCRATCH/err"
RC=0

# ---------------------------------------------------------------- the source tree
# A copy of what `software --source` reads, with a pin whose hash is the fixture asset's.
SOURCE="$SCRATCH/source"
mkdir -p "$SOURCE/helpers" "$SOURCE/files/usr/local/share/agent-bus" \
    "$SOURCE/files/etc/systemd/system" "$SOURCE/files/usr/local/bin" "$SOURCE/files/usr/local/sbin"
cp -r "$REPO_ROOT/helpers/pingbus" "$REPO_ROOT/helpers/agent_bus" "$SOURCE/helpers/"
cp "$REPO_ROOT"/files/etc/systemd/system/agent-bus-* "$SOURCE/files/etc/systemd/system/"
cp "$REPO_ROOT/files/usr/local/share/agent-bus/resolv.conf" "$SOURCE/files/usr/local/share/agent-bus/"
cp "$REPO_ROOT/files/usr/local/bin/agent-bus" "$SOURCE/files/usr/local/bin/"
cp "$TOOL" "$SOURCE/files/usr/local/sbin/"
ASSET="$SCRATCH/asset.zst"
printf 'not really tuwunel, a test fixture\n' >"$ASSET"
ASSET_SHA=$(sha256sum "$ASSET" | awk '{print $1}')
write_pin() {
    cat >"$SOURCE/files/usr/local/share/agent-bus/tuwunel.pin" <<EOF
# test pin
tuwunel_version: v$1
tuwunel_sha256_x86_64: $2
tuwunel_sha256_aarch64: $(printf '%064d' 0)
EOF
}
write_pin 1.9.3 "$ASSET_SHA"

# ---------------------------------------------------------------- stubs
stub() {
    local name=$1
    {
        cat <<HEAD
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$name \$*" >>"\$STUB_DIR/log"
HEAD
        cat
    } >"$STUBS/$name"
    chmod 0755 "$STUBS/$name"
}

stub systemctl <<'EOF'
units=$STUB_DIR/units
mkdir -p "$units"
cmd=$1
shift
args=()
for a in "$@"; do [[ $a == --* ]] || args+=("$a"); done
case $cmd in
    daemon-reload) ;;
    enable) for u in "${args[@]}"; do touch "$units/$u.enabled"; done ;;
    disable)
        for u in "${args[@]}"; do
            rm -f "$units/$u.enabled"
            [[ $* != *--now* ]] || rm -f "$units/$u.active"
        done ;;
    is-enabled) [[ -e $units/${args[0]}.enabled ]] ;;
    start | restart)
        for u in "${args[@]}"; do
            if [[ -e $STUB_DIR/fail-start/$u ]]; then
                rm -f "$units/$u.active"; touch "$units/$u.failed"
            else
                rm -f "$units/$u.failed"; touch "$units/$u.active"
            fi
        done ;;
    stop) for u in "${args[@]}"; do rm -f "$units/$u.active"; done ;;
    kill)
        # SIGUSR2: the homeserver runs admin_signal_execute and writes a numbered backup.
        team=${args[0]#agent-bus-hs@}; team=${team%.service}
        meta=$AGENT_BUS_INSTALL_TEST_PREFIX/var/lib/agent-bus/$team/backups/meta
        mkdir -p "$meta"
        n=$(find "$meta" -mindepth 1 -maxdepth 1 | wc -l)
        touch "$meta/$((n + 1))" ;;
    show)
        prop=${args[0]}; u=${args[1]}
        [[ $prop != -P ]] || { prop=${args[1]}; u=${args[2]}; }
        case $prop in
            ActiveState)
                if [[ -e $units/$u.active ]]; then echo active
                elif [[ -e $units/$u.failed ]]; then echo failed
                else echo inactive; fi ;;
            UnitFileState) if [[ -e $units/$u.enabled ]]; then echo enabled; else echo disabled; fi ;;
            *) echo "stub-$prop" ;;
        esac ;;
    *) echo "systemctl stub: unexpected $cmd" >&2; exit 99 ;;
esac
EOF

stub firewall-cmd <<'EOF'
fw=$STUB_DIR/fw
mkdir -p "$fw"
store=runtime zone="" op="" rule=""
for a in "$@"; do
    case $a in
        --permanent) store=permanent ;;
        --zone=*) zone=${a#--zone=} ;;
        --get-default-zone) echo public; exit 0 ;;
        --get-zone-of-interface=*)
            iface=${a#*=}
            if [[ -e $STUB_DIR/zone-of/$iface ]]; then cat "$STUB_DIR/zone-of/$iface"; exit 0; fi
            echo "no zone"; exit 2 ;;
        --query-rich-rule=*) op=query; rule=${a#*=} ;;
        --add-rich-rule=*) op=add; rule=${a#*=}
            if [[ -e $STUB_DIR/fw-fail-add ]] && [[ $rule == *"$(cat "$STUB_DIR/fw-fail-add")"* ]]; then
                echo "Error: COMMAND_FAILED" >&2; exit 1
            fi ;;
        --remove-rich-rule=*) op=remove; rule=${a#*=} ;;
        *) echo "firewall-cmd stub: unexpected $a" >&2; exit 99 ;;
    esac
done
file=$fw/$store.$zone
touch "$file"
case $op in
    query) if grep -qFx -- "$rule" "$file"; then echo yes; else echo no; exit 1; fi ;;
    add) grep -qFx -- "$rule" "$file" || printf '%s\n' "$rule" >>"$file"; echo success ;;
    remove) awk -v r="$rule" '$0 != r' "$file" >"$file.new"; mv "$file.new" "$file"; echo success ;;
esac
EOF

stub nmcli <<'EOF'
nm=$STUB_DIR/nm
mkdir -p "$nm"
case "$*" in
    "-t -f NAME connection show")
        [[ ! -e $nm/addresses ]] || echo agentbus0
        echo "Wired connection 1" ;;
    "-g ipv4.addresses,ipv6.addresses connection show agentbus0") cat "$nm/addresses" ;;
    "-g GENERAL.STATE connection show agentbus0") [[ ! -e $nm/up ]] || echo activated ;;
    "connection up agentbus0") touch "$nm/up"; echo "Connection successfully activated" ;;
    connection\ add* | connection\ modify*)
        v4="" v6="" prev=""
        for a in "$@"; do
            [[ $prev != ipv4.addresses ]] || v4=$a
            [[ $prev != ipv6.addresses ]] || v6=$a
            prev=$a
        done
        printf '%s\n%s\n' "$v4" "$v6" >"$nm/addresses"
        echo "Connection 'agentbus0' successfully added." ;;
    *) echo "nmcli stub: unexpected $*" >&2; exit 99 ;;
esac
EOF

stub curl <<'EOF'
out="" url="" prev=""
for a in "$@"; do
    [[ $prev != -o ]] || out=$a
    [[ $a != http* ]] || url=$a
    prev=$a
done
case $url in
    http://127.0.0.1:*/_tuwunel/server_version)
        if compgen -G "$STUB_DIR/units/agent-bus-hs@*.active" >/dev/null \
            || compgen -G "$STUB_DIR/units/agent-bus-restore-*.active" >/dev/null; then
            echo '{"name":"Tuwunel","version":"1.9.3"}'
        else
            echo "curl: (7) Failed to connect" >&2; exit 7
        fi ;;
    https://github.com/matrix-construct/tuwunel/releases/download/*) cp "$STUB_DIR/serve-asset" "$out" ;;
    *) echo "curl stub: unexpected url $url" >&2; exit 99 ;;
esac
EOF

stub ip <<'EOF'
case "$*" in
    "-j addr show") cat "$STUB_DIR/ip-addr.json" ;;
    "-j route get "*) printf '[{"dst":"%s","dev":"%s"}]\n' "$4" "$(cat "$STUB_DIR/route-dev")" ;;
    *) echo "ip stub: unexpected $*" >&2; exit 99 ;;
esac
EOF

# ss answers only while a homeserver unit is up, with the addresses the test planted.
stub ss <<'EOF'
if compgen -G "$STUB_DIR/units/agent-bus-hs@*.active" >/dev/null; then
    port=${*##*:}
    port=${port%% *}
    while read -r addr; do
        [[ -z $addr ]] || printf 'LISTEN 0 4096 %s:%s 0.0.0.0:*\n' "$addr" "$port"
    done <"$STUB_DIR/ss-addresses"
fi
EOF

stub rpm <<'EOF'
[[ $1 == -q && $2 == --quiet ]] || { echo "rpm stub: unexpected $*" >&2; exit 99; }
[[ -e $STUB_DIR/rpm/$3 ]]
EOF

stub dnf <<'EOF'
mkdir -p "$STUB_DIR/rpm"
for a in "$@"; do
    [[ $a == -* || $a == install ]] || touch "$STUB_DIR/rpm/$a"
done
EOF

stub getent <<'EOF'
if [[ -e $STUB_DIR/user-$2 ]]; then
    echo "$2:x:990:990::/var/lib/agent-bus:/usr/sbin/nologin"
    exit 0
fi
exit 2
EOF

stub useradd <<'EOF'
touch "$STUB_DIR/user-${*: -1}"
EOF

stub zstd <<'EOF'
out="" prev=""
for a in "$@"; do
    [[ $prev != -o ]] || out=$a
    prev=$a
done
cp "${*: -1}" "$out"
EOF

stub uname <<'EOF'
[[ $* == -m ]]
echo x86_64
EOF

# A transient unit: a name still loaded (active, or failed and not collected) cannot be
# reused; a run that fails is left failed unless --collect was given.
stub systemd-run <<'EOF'
unit="" collect=0
for a in "$@"; do
    [[ $a != --unit=* ]] || unit=${a#--unit=}
    [[ $a != --collect ]] || collect=1
done
if [[ -e $STUB_DIR/units/$unit.active || -e $STUB_DIR/units/$unit.failed ]]; then
    echo "Failed to start transient service unit: Unit $unit was already loaded" >&2
    exit 1
fi
if [[ -e $STUB_DIR/fail-start/$unit ]]; then
    ((collect)) || touch "$STUB_DIR/units/$unit.failed"
else
    touch "$STUB_DIR/units/$unit.active"
fi
echo "Running as unit: $unit"
EOF

# The admin tool: the real renders from this checkout; the team commands are logged.
stub agent-bus <<'EOF'
case $1 in
    render) cd "$REPO_ROOT" && exec python3 -m helpers.agent_bus.cli "$@" ;;
    bootstrap)
        if [[ -e $STUB_DIR/bootstrap-changes ]]; then printf 'CHANGED\tbootstrap stub\n'; fi ;;
    *) echo "agent-bus stub: unexpected $1" >&2; exit 99 ;;
esac
EOF

# ---------------------------------------------------------------- runner
new_root() {
    ROOT="$SCRATCH/root-$1"
    STUB_DIR="$SCRATCH/state-$1"
    mkdir -p "$ROOT/etc" "$STUB_DIR/units" "$STUB_DIR/fail-start" "$STUB_DIR/zone-of"
    printf 'NAME="Fedora Linux"\nID=fedora\nVERSION_ID=44\n' >"$ROOT/etc/os-release"
    touch "$STUB_DIR/units/firewalld.service.active" "$STUB_DIR/units/NetworkManager.service.active"
    cp "$ASSET" "$STUB_DIR/serve-asset"
    echo eth0 >"$STUB_DIR/route-dev"
    printf '%s\n' '[{"ifname":"lo","addr_info":[{"local":"127.0.0.1"}]},' \
        '{"ifname":"agentbus0","addr_info":[{"local":"192.0.2.10"}]},' \
        '{"ifname":"wg0","addr_info":[{"local":"2001:db8::1"}]}]' >"$STUB_DIR/ip-addr.json"
    printf '127.0.0.1\n192.0.2.10\n' >"$STUB_DIR/ss-addresses"
    : >"$STUB_DIR/log"
}
run() {
    : >"$STUB_DIR/log"
    env PATH="$STUBS:$PATH" STUB_DIR="$STUB_DIR" REPO_ROOT="$REPO_ROOT" \
        AGENT_BUS_INSTALL_TEST_PREFIX="$ROOT" "$TOOL" "$@" >"$OUT" 2>"$ERR"
    RC=$?
}
LOG() { echo "$STUB_DIR/log"; }
mode() { stat -c %a "$1"; }
exists() { yes_if test -e "$1"; }
same() { yes_if cmp -s "$1" "$2"; }

team_file() {
    # team_file NAME LISTEN_JSON ALLOW_JSON [SERVER_NAME]
    local extra=""
    if [[ -n ${4:-} ]]; then extra=", \"server_name\": \"$4\""; fi
    cat <<EOF
{"team": "$1", "port": 8448, "listen": $2, "allow_from": $3, "humans": ["owner"],
 "repos": [{"repo": "example/project", "branches": ["main"]}],
 "path_prefixes": ["CLAUDE/Plan/", "docs/"], "forge_api": "https://api.github.com"$extra}
EOF
}

# ================================================================ argument parsing
echo "== argument parsing"
new_root args
run
check "no subcommand is a usage error (64)" "64" "$RC"
run --help
check "--help exits 0" "0" "$RC"
check "--help prints the usage on stdout" "yes" "$(says '^Usage:' "$OUT")"
run frobnicate
check "an unknown subcommand is a usage error (64)" "64" "$RC"
run software
check "software without --source is a usage error (64)" "64" "$RC"
run software --source "$SOURCE" --frob
check "an unknown option is a usage error (64)" "64" "$RC"
run software --source
check "an option without its value is a usage error (64)" "64" "$RC"
run team
check "team without --team-file is a usage error (64)" "64" "$RC"
run remove --team Bad_Name
check "a malformed team name is a usage error (64)" "64" "$RC"
run restore --team alpha --backup latest
check "a non-numeric backup id is a usage error (64)" "64" "$RC"
for bad in 0.0.0.0 :: 192.0.2.300 2001:db8::zz 127.0.0.1 192.0.2.010 ""; do
    run software --source "$SOURCE" --bus-address="$bad"
    check "--bus-address '$bad' is refused (64)" "64" "$RC"
done
check "and nothing was run for any of them" "0" "$(count . "$(LOG)")"

printf 'ID=debian\n' >"$ROOT/etc/os-release"
run software --source "$SOURCE"
check "a host that is not Fedora is refused (78)" "78" "$RC"
check "the refusal names os-release" "yes" "$(says 'os-release' "$ERR")"

if [[ $EUID -eq 0 ]]; then
    OUTSIDE="$(mktemp -d)"
    chmod 755 "$OUTSIDE"
    install -m 755 "$TOOL" "$OUTSIDE/agent-bus-install"
    runuser -u nobody -- "$OUTSIDE/agent-bus-install" check --team alpha >"$OUT" 2>"$ERR"
    RC=$?
    rm -rf "$OUTSIDE"
else
    env -u AGENT_BUS_INSTALL_TEST_PREFIX "$TOOL" check --team alpha >"$OUT" 2>"$ERR"
    RC=$?
fi
check "a non-root caller is refused (77)" "77" "$RC"

# ================================================================ software
echo "== software"
new_root sw
run software --source "$SOURCE" --bus-address=192.0.2.10
check "software succeeds" "0" "$RC"
[[ $RC -eq 0 ]] || cat "$ERR" >&2
L=$ROOT/usr/local/lib/agent-bus
check "the binary is installed under its version" "755" "$(mode "$L/tuwunel-1.9.3")"
check "the binary is the verified asset, decompressed" "yes" "$(same "$L/tuwunel-1.9.3" "$ASSET")"
check "tuwunel points at the pinned version" "tuwunel-1.9.3" "$(readlink "$L/tuwunel")"
check "agent-bus.pyz is built" "755" "$(mode "$L/agent-bus.pyz")"
check "pingbus.pyz is built" "755" "$(mode "$L/pingbus.pyz")"
check "the agent-bus zipapp runs" "agent-bus" "$(python3 -I "$L/agent-bus.pyz" version | awk '{print $1}')"
check "/usr/local/bin/pingbus points at the zipapp" "$L/pingbus.pyz" "$(readlink -f "$ROOT/usr/local/bin/pingbus")"
check "the admin wrapper is installed" "yes" "$(same "$ROOT/usr/local/bin/agent-bus" "$REPO_ROOT/files/usr/local/bin/agent-bus")"
check "the installer installs itself" "755" "$(mode "$ROOT/usr/local/sbin/agent-bus-install")"
check "the kit carries pingbus" "yes" "$(same "$ROOT/usr/local/share/agent-bus/kit/pingbus" "$L/pingbus.pyz")"
for unit in agent-bus-hs@.service agent-bus-backup@.service agent-bus-backup@.timer; do
    check "unit $unit is installed 0644" "644" "$(mode "$ROOT/etc/systemd/system/$unit")"
done
check "the resolver stub is installed" "nameserver 127.0.0.1" "$(grep -v '^#' "$ROOT/usr/local/share/agent-bus/resolv.conf")"
check "the pin is installed" "yes" "$(same "$ROOT/usr/local/share/agent-bus/tuwunel.pin" "$SOURCE/files/usr/local/share/agent-bus/tuwunel.pin")"
check "/var/lib/agent-bus is 0700" "700" "$(mode "$ROOT/var/lib/agent-bus")"
check "the asset was downloaded once" "1" "$(count '^curl .*releases/download/v1.9.3/v1.9.3-release-all-x86_64-v1-linux-gnu-tuwunel.zst' "$(LOG)")"
check "missing packages are installed with dnf" "yes" "$(says '^dnf install -y .*python3.*firewalld.*NetworkManager.*zstd.*curl.*jq.*tcpdump' "$(LOG)")"
check "the system user is created" "yes" "$(says '^useradd --system .*agent-bus$' "$(LOG)")"
check "systemd is reloaded for the new units" "yes" "$(says '^systemctl daemon-reload' "$(LOG)")"
check "the dummy interface is added with the bus address" "yes" \
    "$(says '^nmcli connection add type dummy ifname agentbus0 con-name agentbus0 .*ipv4.addresses 192.0.2.10/32' "$(LOG)")"
check "and brought up" "yes" "$(says '^nmcli connection up agentbus0' "$(LOG)")"
check "the run says CHANGED" "yes" "$(says $'^CHANGED\t' "$OUT")"
check "stdout carries only CHANGED lines" "0" "$(grep -cv $'^CHANGED\t' "$OUT")"

run software --source "$SOURCE" --bus-address=192.0.2.10
check "a second software run succeeds" "0" "$RC"
check "a second run downloads nothing" "0" "$(count '^curl ' "$(LOG)")"
check "a second run prints no CHANGED" "" "$(cat "$OUT")"
check "a second run installs no package and adds no user" "0" "$(count '^(dnf|useradd) ' "$(LOG)")"
check "a second run does not reload systemd" "0" "$(count '^systemctl daemon-reload' "$(LOG)")"
check "a second run leaves the interface alone" "0" "$(count '^nmcli connection (add|modify|up)' "$(LOG)")"

run software --source "$SOURCE" --bus-address=192.0.2.11
check "a new bus address modifies the connection" "yes" "$(says '^nmcli connection modify agentbus0 .*ipv4.addresses 192.0.2.11/32' "$(LOG)")"
check "and says CHANGED" "yes" "$(says $'^CHANGED\t.*agentbus0' "$OUT")"

echo "== software: a download that does not match the pinned hash"
new_root badhash
write_pin 1.9.4 "$(printf '%064d' 1)"
run software --source "$SOURCE"
check "the run fails" "1" "$RC"
check "the failure names the sha256" "yes" "$(says 'sha256' "$ERR")"
check "nothing is installed under that version" "no" "$(exists "$ROOT/usr/local/lib/agent-bus/tuwunel-1.9.4")"
check "and no tuwunel link exists" "no" "$(yes_if test -L "$ROOT/usr/local/lib/agent-bus/tuwunel")"
write_pin 1.9.3 "$ASSET_SHA"

echo "== software: an installed binary whose recorded hash differs from the pin"
new_root rehash
run software --source "$SOURCE"
check "first install" "0" "$RC"
NEW_ASSET="$SCRATCH/asset2.zst"
printf 'a rebuilt release asset\n' >"$NEW_ASSET"
cp "$NEW_ASSET" "$STUB_DIR/serve-asset"
write_pin 1.9.3 "$(sha256sum "$NEW_ASSET" | awk '{print $1}')"
run software --source "$SOURCE"
check "a changed pinned hash downloads again" "1" "$(count '^curl .*releases/download' "$(LOG)")"
check "and installs the new bytes" "yes" "$(same "$ROOT/usr/local/lib/agent-bus/tuwunel-1.9.3" "$NEW_ASSET")"
write_pin 1.9.3 "$ASSET_SHA"

# ================================================================ team
echo "== team: refused team files"
new_root team
run software --source "$SOURCE"
check "software for the team cases" "0" "$RC"
TF="$SCRATCH/team.json"
refused() {
    local label=$1
    run team --team-file "$TF"
    check "$label is refused (78)" "78" "$RC"
    check "  and no team directory is made" "no" "$(exists "$ROOT/var/lib/agent-bus/alpha")"
    check "  and no unit is started" "0" "$(count '^systemctl (start|restart|enable)' "$(LOG)")"
}
team_file alpha '["192.0.2.300"]' '[]' >"$TF"
refused "a malformed listen address"
team_file alpha '["0.0.0.0"]' '[]' >"$TF"
refused "the IPv4 wildcard listen address"
team_file alpha '["::"]' '[]' >"$TF"
refused "the IPv6 wildcard listen address"
team_file alpha '["192.0.2.10"]' '["192.0.2.1/24"]' >"$TF"
refused "a CIDR with host bits"
team_file alpha '["192.0.2.10"]' '["198.51.100.0/33"]' >"$TF"
refused "a CIDR with an impossible prefix"
team_file alpha '["192.0.2.10"]' '["198.51.100.0"]' >"$TF"
refused "a CIDR with no prefix length"
printf '{"team": "alpha", ' >"$TF"
refused "a team file that is not JSON"
team_file alpha '["198.51.100.7"]' '[]' >"$TF"
refused "a listen address on no interface"
check "  and the refusal names the address" "yes" "$(says '198.51.100.7' "$ERR")"

echo "== team: first install"
team_file alpha '["192.0.2.10"]' '["198.51.100.0/24"]' >"$TF"
echo wg0 >"$STUB_DIR/route-dev"
echo trusted >"$STUB_DIR/zone-of/wg0"
run team --team-file "$TF"
check "team succeeds" "0" "$RC"
[[ $RC -eq 0 ]] || cat "$ERR" >&2
T=$ROOT/var/lib/agent-bus/alpha
check "the team directory is 0700" "700" "$(mode "$T")"
for sub in db backups secrets; do
    check "  $sub/ is 0700" "700" "$(mode "$T/$sub")"
done
check "tuwunel.toml is rendered" "yes" "$(says '^address = \["127.0.0.1", "192.0.2.10"\]$' "$T/tuwunel.toml")"
check "tuwunel.toml is 0640" "640" "$(mode "$T/tuwunel.toml")"
check "team.json is the canonical team file" "alpha.agent-bus.internal" "$(jq -r .server_name "$T/team.json")"
SECRET_FILE=$T/secrets/registration_shared_secret
check "the shared secret is 0600" "600" "$(mode "$SECRET_FILE")"
check "the shared secret is 64 bytes as hex" "yes" "$(says '^[0-9a-f]{128}$' "$SECRET_FILE")"
check "  with no trailing newline" "128" "$(stat -c %s "$SECRET_FILE")"
SECRET_BEFORE=$(cat "$SECRET_FILE")
DROPIN=$ROOT/etc/systemd/system/agent-bus-hs@alpha.service.d/network.conf
check "the drop-in allows loopback, the listen address and allow_from" "yes" \
    "$(says '^IPAddressAllow=127.0.0.1/32 ::1/128 192.0.2.10/32 198.51.100.0/24$' "$DROPIN")"
check "the drop-in waits for the listen interface" "yes" "$(says '^After=sys-subsystem-net-devices-agentbus0.device$' "$DROPIN")"
check "the rich rule is added in the zone of the CIDR's interface" "yes" \
    "$(says '^firewall-cmd --permanent --zone=trusted --add-rich-rule=rule family="ipv4" source address="198.51.100.0/24" port port="8448" protocol="tcp" accept$' "$(LOG)")"
check "  and to the runtime configuration" "yes" \
    "$(says '^firewall-cmd --zone=trusted --add-rich-rule=rule family="ipv4" source address="198.51.100.0/24"' "$(LOG)")"
check "nothing else is opened" "1" "$(count '^firewall-cmd --permanent .*--add-' "$(LOG)")"
check "the homeserver is enabled" "yes" "$(says '^systemctl enable agent-bus-hs@alpha.service' "$(LOG)")"
check "the backup timer is enabled" "yes" "$(says '^systemctl enable agent-bus-backup@alpha.timer' "$(LOG)")"
check "the homeserver is started" "1" "$(count '^systemctl (start|restart) agent-bus-hs@alpha.service' "$(LOG)")"
check "readiness is polled" "yes" "$(says '^curl .*http://127.0.0.1:8448/_tuwunel/server_version' "$(LOG)")"
check "bootstrap runs" "yes" "$(says '^agent-bus bootstrap alpha$' "$(LOG)")"
check "the run says CHANGED" "yes" "$(says $'^CHANGED\t' "$OUT")"
check "stdout carries only CHANGED lines" "0" "$(grep -cv $'^CHANGED\t' "$OUT")"
check "no secret reaches stdout or stderr" "0" "$(cat "$OUT" "$ERR" | grep -cF "$SECRET_BEFORE")"

echo "== team: nothing changed"
run team --team-file "$TF"
check "a second run succeeds" "0" "$RC"
check "a second run neither starts nor restarts the homeserver" "0" "$(count '^systemctl (start|restart|stop)' "$(LOG)")"
check "a second run prints no CHANGED" "" "$(cat "$OUT")"
check "a second run adds no rule" "0" "$(count '^firewall-cmd .*--(add|remove)-rich-rule' "$(LOG)")"
check "a second run does not reload systemd" "0" "$(count '^systemctl daemon-reload' "$(LOG)")"
check "the shared secret is kept" "$SECRET_BEFORE" "$(cat "$SECRET_FILE")"
check "readiness is still checked" "yes" "$(says '_tuwunel/server_version' "$(LOG)")"
touch "$STUB_DIR/bootstrap-changes"
run team --team-file "$TF"
check "bootstrap's own CHANGED lines are passed on" "yes" "$(says $'^CHANGED\tbootstrap stub$' "$OUT")"
rm -f "$STUB_DIR/bootstrap-changes"

echo "== team: a stopped homeserver is started"
HS_ALPHA=agent-bus-hs@alpha.service
rm -f "$STUB_DIR/units/$HS_ALPHA.active"
run team --team-file "$TF"
check "it is started" "1" "$(count '^systemctl start agent-bus-hs@alpha.service' "$(LOG)")"
check "and the run says CHANGED" "yes" "$(says $'^CHANGED\t' "$OUT")"

echo "== team: allow_from changed"
team_file alpha '["192.0.2.10"]' '["203.0.113.0/24"]' >"$TF"
run team --team-file "$TF"
check "the run succeeds" "0" "$RC"
check "the drop-in changed, so the homeserver restarts" "1" "$(count '^systemctl restart agent-bus-hs@alpha.service' "$(LOG)")"
check "systemd is reloaded" "yes" "$(says '^systemctl daemon-reload' "$(LOG)")"
check "the old rule is removed" "yes" "$(says '^firewall-cmd --permanent --zone=trusted --remove-rich-rule=.*198.51.100.0/24' "$(LOG)")"
check "the new rule is added" "yes" "$(says '^firewall-cmd --permanent --zone=trusted --add-rich-rule=.*203.0.113.0/24' "$(LOG)")"
check "the permanent zone holds only the new rule" "1" "$(count . "$STUB_DIR/fw/permanent.trusted")"
check "and the run says CHANGED" "yes" "$(says $'^CHANGED\t' "$OUT")"

echo "== team: a new binary restarts the homeserver"
write_pin 1.9.4 "$ASSET_SHA"
run software --source "$SOURCE"
check "software installs the new version" "tuwunel-1.9.4" "$(readlink "$ROOT/usr/local/lib/agent-bus/tuwunel")"
run team --team-file "$TF"
check "the next team run restarts the homeserver" "1" "$(count '^systemctl restart agent-bus-hs@alpha.service' "$(LOG)")"
check "and says CHANGED" "yes" "$(says $'^CHANGED\t' "$OUT")"
write_pin 1.9.3 "$ASSET_SHA"

echo "== team: a changed base unit restarts the homeserver"
HS_SRC="$SOURCE/files/etc/systemd/system/agent-bus-hs@.service"
cp "$HS_SRC" "$SCRATCH/hs-unit.orig"
printf '# a hardening fix\n' >>"$HS_SRC"
run software --source "$SOURCE"
check "software installs the changed unit and reloads" "yes" "$(says '^systemctl daemon-reload' "$(LOG)")"
check "  but restarts nothing itself" "0" "$(count '^systemctl restart' "$(LOG)")"
run team --team-file "$TF"
check "the next team run succeeds" "0" "$RC"
check "  and restarts the homeserver" "1" "$(count '^systemctl restart agent-bus-hs@alpha.service' "$(LOG)")"
run team --team-file "$TF"
check "the run after that does not restart it" "0" "$(count '^systemctl (start|restart)' "$(LOG)")"
cp "$SCRATCH/hs-unit.orig" "$HS_SRC"
run software --source "$SOURCE"
run team --team-file "$TF"

echo "== team: the server name never changes"
team_file alpha '["192.0.2.10"]' '["203.0.113.0/24"]' other.agent-bus.internal >"$TF"
run team --team-file "$TF"
check "a different server_name is refused (78)" "78" "$RC"
check "the installed team.json is kept" "alpha.agent-bus.internal" "$(jq -r .server_name "$T/team.json")"
team_file alpha '["192.0.2.10"]' '["203.0.113.0/24"]' >"$TF"

echo "== team: the homeserver binds an address the team file does not list"
printf '127.0.0.1\n192.0.2.10\n198.51.100.9\n' >"$STUB_DIR/ss-addresses"
run team --team-file "$TF"
check "an unexpected bound address fails the run" "1" "$RC"
check "the failure names the address" "yes" "$(says '198.51.100.9' "$ERR")"
check "bootstrap does not run" "0" "$(count '^agent-bus bootstrap' "$(LOG)")"
printf '127.0.0.1\n192.0.2.10\n' >"$STUB_DIR/ss-addresses"

echo "== team: a unit that fails to start (as an unknown tuwunel.toml key makes it)"
new_root failing
run software --source "$SOURCE"
team_file beta '["192.0.2.10"]' '[]' >"$TF"
touch "$STUB_DIR/fail-start/agent-bus-hs@beta.service"
run team --team-file "$TF"
check "the readiness step fails the run" "1" "$RC"
check "the failure names the unit and its state" "yes" "$(says 'agent-bus-hs@beta.service is failed' "$ERR")"
check "it points at the unit's log, without scanning it" "yes" "$(says 'journalctl -u agent-bus-hs@beta.service' "$ERR")"
check "bootstrap does not run" "0" "$(count '^agent-bus bootstrap' "$(LOG)")"

echo "== team: a firewalld add that fails part-way"
new_root fwfail
run software --source "$SOURCE"
team_file alpha '["192.0.2.10"]' '["198.51.100.0/24", "203.0.113.0/24"]' >"$TF"
echo '203.0.113.0/24' >"$STUB_DIR/fw-fail-add"
run team --team-file "$TF"
check "the failing add fails the run" "1" "$RC"
check "the first rule is in firewalld" "1" "$(count '198.51.100.0/24' "$STUB_DIR/fw/permanent.public")"
FW_RECORD=$ROOT/var/lib/agent-bus-install/alpha/firewalld.rules
check "  and in the record remove reads" "1" "$(count '198.51.100.0/24' "$FW_RECORD")"
rm -f "$STUB_DIR/fw-fail-add"
run remove --team alpha
check "remove then succeeds" "0" "$RC"
check "  and deletes the rule the failed run added" "0" "$(count . "$STUB_DIR/fw/permanent.public")"
check "  from the runtime configuration too" "0" "$(count . "$STUB_DIR/fw/runtime.public")"

echo "== team: a port another team holds"
new_root ports
run software --source "$SOURCE"
team_file alpha '["192.0.2.10"]' '[]' >"$TF"
run team --team-file "$TF"
check "the first team installs" "0" "$RC"
team_file gamma '["192.0.2.10"]' '[]' >"$TF"
run team --team-file "$TF"
check "a second team on the same port is refused (78)" "78" "$RC"
check "the refusal names the other team" "yes" "$(says 'alpha' "$ERR")"

echo "== team: needs firewalld and NetworkManager"
new_root nofw
run software --source "$SOURCE"
rm -f "$STUB_DIR/units/firewalld.service.active"
team_file alpha '["192.0.2.10"]' '[]' >"$TF"
run team --team-file "$TF"
check "team refuses without firewalld running" "1" "$RC"
check "the refusal names firewalld" "yes" "$(says 'firewalld' "$ERR")"

echo "== team: needs software first"
new_root nosw
run team --team-file "$TF"
check "team refuses before software ran" "1" "$RC"
check "the refusal says to run software" "yes" "$(says 'software' "$ERR")"

# ================================================================ check, backup, restore, remove
echo "== check"
new_root life
run software --source "$SOURCE"
team_file alpha '["192.0.2.10"]' '["198.51.100.0/24"]' >"$TF"
run team --team-file "$TF"
check "install for the life-cycle cases" "0" "$RC"
run check --team alpha
check "check passes on a fresh install" "0" "$RC"
check "check prints the listen addresses" "yes" "$(says $'^CHECK\tlisten\tok\t' "$OUT")"
check "check prints the firewalld rules" "yes" "$(says $'^CHECK\tfirewalld\tok\t' "$OUT")"
check "check prints the unit's IP filter" "yes" "$(says $'^CHECK\tIPAddressAllow\t' "$OUT")"
check "check changes nothing" "0" \
    "$(count '^(systemctl (start|restart|stop|enable|disable|daemon-reload)|firewall-cmd .*--(add|remove)-)' "$(LOG)")"
rm "$STUB_DIR/fw/permanent.public"
run check --team alpha
check "a missing rich rule fails check" "1" "$RC"
check "  and the line says FAIL" "yes" "$(says $'^CHECK\tfirewalld\tFAIL\t' "$OUT")"
run team --team-file "$TF"
check "team puts the missing rule back" "1" "$(count '^firewall-cmd --permanent --zone=public --add-rich-rule' "$(LOG)")"

echo "== backup-now"
run backup-now --team alpha
check "backup-now succeeds" "0" "$RC"
[[ $RC -eq 0 ]] || cat "$ERR" >&2
check "it signals the homeserver" "yes" "$(says '^systemctl kill --kill-whom=main --signal=SIGUSR2 agent-bus-hs@alpha.service' "$(LOG)")"
TARS=$ROOT/var/lib/agent-bus-install/alpha/backups
TAR=$TARS/agent-bus-state-1.tar
check "the state tar is in root's own directory, 0600" "600" "$(mode "$TAR")"
check "  which is 0700" "700" "$(mode "$TARS")"
check "  and not in the agent-bus user's backups/" "0" \
    "$(find "$ROOT/var/lib/agent-bus/alpha/backups" -name '*.tar' | wc -l)"
check "it holds the shared secret" "1" "$(tar -tf "$TAR" | grep -cx 'secrets/registration_shared_secret')"
check "it holds team.json" "1" "$(tar -tf "$TAR" | grep -cx 'team.json')"
check "it says which backup" "yes" "$(says $'^CHANGED\tbackup 1 of team alpha' "$OUT")"

echo "== restore"
run restore --team alpha --backup 7
check "an unknown backup id is refused (78)" "78" "$RC"
echo "tampered" >"$ROOT/var/lib/agent-bus/alpha/registry.json"
run restore --team alpha --backup 1
check "restore succeeds" "0" "$RC"
[[ $RC -eq 0 ]] || cat "$ERR" >&2
check "the homeserver is stopped first" "yes" "$(says '^systemctl stop agent-bus-hs@alpha.service' "$(LOG)")"
check "the binary runs once with --restore-backup as agent-bus" "yes" \
    "$(says '^systemd-run .*--property=User=agent-bus .*/tuwunel --restore-backup 1$' "$(LOG)")"
check "  as a collected transient unit" "yes" "$(says '^systemd-run --unit=agent-bus-restore-alpha.service --collect ' "$(LOG)")"
for property in NoNewPrivileges=yes ProtectSystem=strict ReadWritePaths=/var/lib/agent-bus/alpha \
    SocketBindDeny=any IPAddressDeny=any 'IPAddressAllow=127.0.0.1/32 ::1/128' SocketBindAllow=tcp:8448 \
    TemporaryFileSystem=/run/systemd/resolve:ro \
    BindReadOnlyPaths=/usr/local/share/agent-bus/resolv.conf:/run/systemd/resolve/resolv.conf \
    Environment=TUWUNEL_CONFIG=/var/lib/agent-bus/alpha/tuwunel.toml; do
    check "  under the homeserver's sandbox: $property" "1" "$(grep -cF -- "--property=$property " "$(LOG)")"
done
check "  with no ExecStart taken from the unit" "0" "$(grep -cF -- '--property=ExecStart' "$(LOG)")"
check "the one-off run is stopped" "yes" "$(says '^systemctl stop agent-bus-restore-alpha.service' "$(LOG)")"
check "the homeserver is started again" "yes" "$(says '^systemctl start agent-bus-hs@alpha.service' "$(LOG)")"
check "and bootstrapped" "yes" "$(says '^agent-bus bootstrap alpha' "$(LOG)")"
check "state files absent from the backup are removed" "no" "$(exists "$ROOT/var/lib/agent-bus/alpha/registry.json")"
check "the restored secret is the backed-up one, 0600" "600" "$(mode "$ROOT/var/lib/agent-bus/alpha/secrets/registration_shared_secret")"
check "the restored secrets/ is 0700" "700" "$(mode "$ROOT/var/lib/agent-bus/alpha/secrets")"
check "the restored team.json is 0640" "640" "$(mode "$ROOT/var/lib/agent-bus/alpha/team.json")"

echo "== restore: a failed one-off run, then a retry"
touch "$STUB_DIR/fail-start/agent-bus-restore-alpha.service"
run restore --team alpha --backup 1
check "a restore whose one-off run fails fails" "1" "$RC"
check "  naming the one-off unit" "yes" "$(says 'agent-bus-restore-alpha.service is' "$ERR")"
rm -f "$STUB_DIR/fail-start/agent-bus-restore-alpha.service"
run restore --team alpha --backup 1
check "the retry succeeds (no failed unit left to block the name)" "0" "$RC"
[[ $RC -eq 0 ]] || cat "$ERR" >&2

echo "== restore: a tampered state tar is refused"
cp "$TAR" "$SCRATCH/good.tar"
# make_tar OUT NAME:TYPE:MODE... — TYPE f (file) or d (directory) or l (symlink to /etc).
make_tar() {
    python3 -I - "$@" <<'PY'
import io, sys, tarfile
with tarfile.open(sys.argv[1], "w") as archive:
    for spec in sys.argv[2:]:
        name, kind, mode = spec.split(":")
        info = tarfile.TarInfo(name)
        info.mode = int(mode, 8)
        if kind == "d":
            info.type = tarfile.DIRTYPE
            archive.addfile(info)
        elif kind == "l":
            info.type = tarfile.SYMTYPE
            info.linkname = "/etc"
            archive.addfile(info)
        else:
            data = b"x"
            info.size = len(data)
            archive.addfile(info, io.BytesIO(data))
PY
}
GOOD_MEMBERS=(secrets:d:700 secrets/registration_shared_secret:f:600 team.json:f:640)
refused_tar() {
    local label=$1
    shift
    make_tar "$TAR" "$@"
    run restore --team alpha --backup 1
    check "$label is refused (78)" "78" "$RC"
    check "  and the homeserver is not stopped" "0" "$(count '^systemctl stop' "$(LOG)")"
    check "  and nothing is run" "0" "$(count '^systemd-run' "$(LOG)")"
}
refused_tar "a stray member" "${GOOD_MEMBERS[@]}" etc/passwd:f:644
check "  and the refusal names it" "yes" "$(says 'etc/passwd' "$ERR")"
refused_tar "a setuid file" "${GOOD_MEMBERS[@]}" secrets/sh:f:4755
check "  and the refusal names the mode" "yes" "$(says 'setuid' "$ERR")"
refused_tar "a member climbing out" "${GOOD_MEMBERS[@]}" secrets/../../x:f:600
refused_tar "a symlink" "${GOOD_MEMBERS[@]}" registry.json:l:777
refused_tar "a tar without the secrets" team.json:f:640
check "the team's secret survived every refusal" "yes" \
    "$(yes_if test -s "$ROOT/var/lib/agent-bus/alpha/secrets/registration_shared_secret")"
cp "$SCRATCH/good.tar" "$TAR"

echo "== remove"
run remove --team alpha
check "remove succeeds" "0" "$RC"
check "both units are disabled and stopped" "yes" \
    "$(says '^systemctl disable --now agent-bus-hs@alpha.service agent-bus-backup@alpha.timer' "$(LOG)")"
check "the drop-in is gone" "no" "$(exists "$ROOT/etc/systemd/system/agent-bus-hs@alpha.service.d")"
check "the rich rule is removed" "0" "$(count . "$STUB_DIR/fw/permanent.public")"
check "the data stays without --purge" "yes" "$(exists "$ROOT/var/lib/agent-bus/alpha/db")"
check "remove says CHANGED" "yes" "$(says $'^CHANGED\t' "$OUT")"
run remove --team alpha
check "a second remove succeeds" "0" "$RC"
check "  and changes nothing" "" "$(cat "$OUT")"
run remove --team alpha --purge
check "--purge removes the data" "no" "$(exists "$ROOT/var/lib/agent-bus/alpha")"

echo
printf 'passed: %d failed: %d\n' "$passed" "$failed"
[[ $failed -eq 0 ]]
