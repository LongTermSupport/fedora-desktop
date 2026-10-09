#!/usr/bin/env bash
# Unit-test ccy_host_time_zone (files/var/local/claude-yolo/lib/common-pure.bash), Plan 00165.
#
# WHY THIS EXISTS. A container shares the host's kernel clock but not its time zone: with
# no TZ and the image's /etc/localtime pointing at Etc/UTC, every ccy session showed UTC
# while the desktop clock showed local time, an hour apart in British Summer Time. ccy now
# passes the host's IANA zone name in as TZ. The derivation must never quietly produce UTC:
# an unresolvable zone is a refusal, not a default, because a wrong zone looks exactly like
# a right one until somebody compares two clocks.
#
# The value is interpolated into a `podman run -e` argument, so the grammar check and the
# "the zone exists under zoneinfo" check are the guards. The zoneinfo directory is an
# argument, so a fake one is built here and no case depends on the machine running it.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB="$REPO_ROOT/files/var/local/claude-yolo/lib/common-pure.bash"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

if [ ! -f "$LIB" ]; then
    echo "FAIL: common-pure.bash not found at $LIB" >&2
    exit 1
fi

# shellcheck source=/dev/null
source "$LIB"

if ! declare -F ccy_host_time_zone >/dev/null; then
    echo "FAIL: ccy_host_time_zone is not defined after sourcing the library" >&2
    exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
zi="$work/zoneinfo"
mkdir -p "$zi/Europe" "$zi/America/Argentina" "$zi/Etc"
touch "$zi/Europe/London" "$zi/Europe/Paris" "$zi/America/Argentina/Buenos_Aires" \
    "$zi/Etc/UTC" "$zi/Etc/GMT+1" "$zi/UTC"

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

# resolved <timedatectl-value> <localtime-link> — the zone, or "REFUSED" on a non-zero exit.
resolved() {
    local out
    if out="$(ccy_host_time_zone "$1" "$2" "$zi" 2>/dev/null)"; then
        printf '%s' "$out"
    else
        printf 'REFUSED'
    fi
}

# ── the host zone is passed through ──────────────────────────────────────────────────
check "timedatectl's zone is used" "Europe/London" \
    "$(resolved Europe/London /usr/share/zoneinfo/Europe/London)"
check "timedatectl wins over a disagreeing link" "Europe/Paris" \
    "$(resolved Europe/Paris /usr/share/zoneinfo/Europe/London)"
check "a three-part zone name is kept" "America/Argentina/Buenos_Aires" \
    "$(resolved America/Argentina/Buenos_Aires '')"
check "a zone with a plus sign is kept" "Etc/GMT+1" "$(resolved Etc/GMT+1 '')"
check "a host that really is on UTC gets UTC" "UTC" "$(resolved UTC '')"

# ── the /etc/localtime link is the fallback ──────────────────────────────────────────
check "no timedatectl answer: the absolute link's zone" "Europe/London" \
    "$(resolved '' /usr/share/zoneinfo/Europe/London)"
check "no timedatectl answer: a relative link's zone" "Europe/London" \
    "$(resolved '' ../usr/share/zoneinfo/Europe/London)"
check "an invalid timedatectl answer falls back to the link" "Europe/London" \
    "$(resolved 'n/a' /usr/share/zoneinfo/Europe/London)"
check "a timedatectl zone missing from zoneinfo falls back to the link" "Europe/London" \
    "$(resolved Mars/Olympus_Mons /usr/share/zoneinfo/Europe/London)"

# ── refusals: never a silent UTC ─────────────────────────────────────────────────────
check "neither source gives anything: refused" "REFUSED" "$(resolved '' '')"
check "a link outside zoneinfo is refused" "REFUSED" "$(resolved '' /etc/some-file)"
check "a zone that does not exist anywhere is refused" "REFUSED" \
    "$(resolved Mars/Olympus_Mons /usr/share/zoneinfo/Mars/Olympus_Mons)"
check "a path-climbing name is refused" "REFUSED" "$(resolved '../../etc/passwd' '')"
check "a leading slash is refused" "REFUSED" "$(resolved /Europe/London '')"
check "a space is refused" "REFUSED" "$(resolved 'Europe/London x' '')"
check "a shell metacharacter is refused" "REFUSED" "$(resolved 'Europe/London;id' '')"
check "a POSIX TZ string is refused" "REFUSED" "$(resolved 'GMT0BST,M3.5.0/1,M10.5.0' '')"
check "a newline is refused" "REFUSED" "$(resolved 'Europe/London
Europe/Paris' '')"
# A directory under zoneinfo is not a zone (Europe alone would make glibc fall back to UTC).
check "a zoneinfo directory is not a zone" "REFUSED" "$(resolved Europe '')"

# ── the refusal says why, on stderr, and prints nothing on stdout ────────────────────
err="$(ccy_host_time_zone '' '' "$zi" 2>&1 >/dev/null)"
case "$err" in
    *"time zone"*) check "the refusal explains itself" "yes" "yes" ;;
    *) check "the refusal explains itself" "yes" "no: ${err}" ;;
esac
if out="$(ccy_host_time_zone '' '' "$zi" 2>/dev/null)"; then
    check "a refusal prints nothing on stdout" "REFUSED" "unexpectedly accepted: ${out}"
else
    check "a refusal prints nothing on stdout" "" "$out"
fi

# ── the launcher wires it in ─────────────────────────────────────────────────────────
# The run argv carries TZ, and nothing else in the launcher sets it, so a project's
# ccy.env / ccy.env.local `export TZ=...` (sourced by the entrypoint after this
# environment is in place) is what overrides it.
# Built from a variable: a literal `$` in single quotes is read by the linter as a mistaken
# expansion (SC2016); the point here is the literal text in the launcher.
dollar='$'
check "launcher: the run argv passes TZ from the resolved host zone" "1" \
    "$(grep -c -F -- "-e \"TZ=${dollar}CCY_HOST_TZ\"" "$LAUNCHER")"
check "launcher: the zone is resolved by ccy_host_time_zone" "1" \
    "$(grep -c -F "CCY_HOST_TZ=\"${dollar}(ccy_host_time_zone " "$LAUNCHER")"
check "launcher: no --tz flag competes with TZ" "0" \
    "$(grep -v -E '^[[:space:]]*#' "$LAUNCHER" | grep -c -E -- '--tz[= ]')"

# ── an explicit TZ in ccy.env / ccy.env.local wins ───────────────────────────────────
# Run the entrypoint's PROJECT-ENV block (the same markers test-ccy-project-env.bash uses)
# against a fake /workspace with TZ already set as the launcher would set it.
ENTRYPOINT="$REPO_ROOT/files/var/local/claude-yolo/entrypoint.sh"
block="$(awk '/^# >>> PROJECT-ENV$/{f=1;next} /^# <<< PROJECT-ENV$/{f=0} f' "$ENTRYPOINT")"
if [ -z "$block" ]; then
    check "entrypoint: PROJECT-ENV block found" "found" "missing"
else
    ws="$work/ws"
    mkdir -p "$ws/.claude/ccy"
    # The block announces each file it sources on stdout, so TZ is read back through a file.
    env_tz() {
        TZ=Europe/London bash -c "${block//\/workspace/$ws}"'
printf %s "$TZ" >"$1"' _ "$work/tz" >/dev/null
        cat "$work/tz"
    }
    printf 'export TZ=America/Argentina/Buenos_Aires\n' > "$ws/.claude/ccy/ccy.env"
    check "ccy.env's TZ overrides the launcher's" "America/Argentina/Buenos_Aires" "$(env_tz)"
    printf 'export TZ=Europe/Paris\n' > "$ws/.claude/ccy/ccy.env.local"
    check "ccy.env.local's TZ overrides both" "Europe/Paris" "$(env_tz)"
fi

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
