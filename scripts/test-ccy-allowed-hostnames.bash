#!/usr/bin/env bash
# Unit-test check_allowed_hostname (files/var/local/claude-yolo/lib/common.bash), the
# launcher's rule that .claude/ccy/allowed-hostnames names the hosts ccy may run on.
#
# WHY THIS TEST EXISTS. The rule is a restriction, so every way of failing to read it must stop
# the launch rather than lift the restriction (fedora-desktop#293). The launcher calls it as
# `check_allowed_hostname || exit 1`, which turns errexit off inside it: a rule file that could
# not be opened used to run its read loop zero times and read as "no restriction", and a last
# line with no newline (a `never` typed without one) was dropped the same way. Runs the real
# function in throwaway project folders, each in its own bash, as the launcher runs it.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
chmod 755 "$work"

# The library is copied where the unprivileged case below can read it; the checkout may not be.
cp -r "$SCRIPT_DIR/../files/var/local/claude-yolo/lib" "$work/lib"
chmod -R a+rX "$work/lib"
LIB="$work/lib/common.bash"
# The launcher's own call, `check_allowed_hostname || exit 1`, run in the project folder.
cat >"$work/launch.bash" <<'EOF'
source "$1" || exit 90
cd "$2" || exit 91
check_allowed_hostname || exit 1
EOF
chmod 644 "$work/launch.bash"

# common.bash checks for its container engine when sourced; a stub satisfies it.
mkdir -p "$work/bin"
printf '#!/usr/bin/env bash\nexit 0\n' >"$work/bin/podman"
chmod 755 "$work/bin/podman" "$work/bin"
export PATH="$work/bin:$PATH" CCY_CONTAINER_ENGINE=podman

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
        echo "        output: $(cat "$work/check.log")"
    fi
}

# project <name> [content]: a project folder, with an allowed-hostnames file when content is
# given (printf'd as is, so a last line without a newline stays without one).
project() {
    local dir="$work/$1"
    mkdir -p "$dir/.claude/ccy"
    if [ "$#" -ge 2 ]; then
        printf '%s' "$2" >"$dir/.claude/ccy/allowed-hostnames"
    fi
    chmod -R a+rX "$dir"
    printf '%s' "$dir"
}

# verdict <dir> [prefix...]: run check_allowed_hostname in <dir> under the launcher's own
# `|| exit 1`, as this host, through the optional prefix (setpriv). Prints the exit status;
# what the function said is in $work/check.log.
verdict() {
    local dir="$1"
    shift
    "$@" bash "$work/launch.bash" "$LIB" "$dir" >"$work/check.log" 2>&1
    echo "$?"
}

host=$(hostname)

echo "== a file with a pattern for this host, and one without"
check "this host listed: allowed" "0" "$(verdict "$(project mine "$host"$'\n')")"
check "another host only: refused" "1" "$(verdict "$(project other $'some-other-host\n')")"
check "no file: no restriction" "0" "$(verdict "$(project none)")"

echo "== a last line with no newline is read"
check "never with no newline: refused" "1" "$(verdict "$(project never-nonl never)")"
check "and named as never" "yes" "$(grep -q 'disabled for this project' "$work/check.log" && echo yes || echo no)"
check "this host with no newline: allowed" "0" "$(verdict "$(project mine-nonl "$host")")"

echo "== a file that is there but cannot be read stops the launch"
locked=$(project locked "$host"$'\n')
chmod 000 "$locked/.claude/ccy/allowed-hostnames"
prefix=()
if [ "$(id -u)" -eq 0 ]; then
    # root reads a file whatever its mode, so the case runs as nobody
    prefix=(setpriv --reuid=65534 --regid=65534 --clear-groups)
fi
check "unreadable: refused" "1" "$(verdict "$locked" "${prefix[@]}")"
check "and the file is named" "yes" "$(grep -q 'allowed-hostnames cannot be read' "$work/check.log" && echo yes || echo no)"

# load_project_mounts reads .claude/ccy/mounts the same way and had the same hazard: an
# unopenable file read as "no mounts" and the launch went on (fedora-desktop#293).
echo "== a project mounts file that is there but cannot be read stops the launch"
cat >"$work/mounts.bash" <<'EOF'
source "$1" || exit 90
cd "$2" || exit 91
load_project_mounts || exit 1
EOF
chmod 644 "$work/mounts.bash"
mounts=$(project mounts)
printf '%s\n' "$work/lib:/mnt/lib:ro" >"$mounts/.claude/ccy/mounts"
chmod 000 "$mounts/.claude/ccy/mounts"
"${prefix[@]}" bash "$work/mounts.bash" "$LIB" "$mounts" >"$work/check.log" 2>&1
check "unreadable mounts: refused" "1" "$?"
check "and the mounts file is named" "yes" "$(grep -q 'mounts cannot be read' "$work/check.log" && echo yes || echo no)"

echo ""
echo "passed: $passed   failed: $failed"
[ "$failed" -eq 0 ]
