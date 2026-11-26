#!/usr/bin/env bash
# Unit-test ccy_env_local_dist_sync and ccy_env_local_mount_args
# (files/var/local/claude-yolo/lib/common.bash), which ask the Go front (F25), built here.
#
# WHY THIS TEST EXISTS. ccy owns .claude/ccy/, so ccy writes the tracked template
# ccy.env.local.dist on every launch: commented placeholders for one install's overrides,
# never secrets, never sourced. A real ccy.env.local (placed by the install's own IaC)
# names the dist version it was based on, and ccy warns when the dist has moved on. Runs
# the real function in a throwaway project.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="$SCRIPT_DIR/../files/var/local/claude-yolo/lib/common.bash"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# common.bash checks for its container engine when sourced; a stub satisfies it.
mkdir -p "$work/bin"
printf '#!/usr/bin/env bash\nexit 0\n' >"$work/bin/podman"
chmod 755 "$work/bin/podman"
export PATH="$work/bin:$PATH" CCY_CONTAINER_ENGINE=podman

# The library asks the Go front beside the launcher ($SCRIPT_DIR/claude-yolo) for these files.
front="$work/front"
if ! go -C "$SCRIPT_DIR/../src-go" build -o "$front/claude-yolo" ./cmd/claude-yolo; then
    echo "FAIL: could not build the Go front (src-go/cmd/claude-yolo)" >&2
    exit 1
fi

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
        echo "        sync output: $(cat "$work/sync.log")"
    fi
}

# run_sync <dir>: run the real function there; prints its exit code. Its stdout and
# stderr go to sync.log.
run_sync() {
    (cd "$1" && SCRIPT_DIR="$front" bash -c '. "$1"; ccy_env_local_dist_sync' _ "$LIB" >"$work/sync.log" 2>&1)
    echo "$?"
}

# current_version: the version the front writes, read from the dist it writes.
current_version() {
    local probe="$work/probe"
    mkdir -p "$probe/.claude/ccy"
    run_sync "$probe" >/dev/null
    awk 'NR == 1 { print $NF }' "$probe/.claude/ccy/ccy.env.local.dist"
}

log_has() {
    if grep -qF -- "$1" "$work/sync.log"; then echo yes; else echo no; fi
}

version=$(current_version)
check "the front writes a whole-number dist version" "yes" \
    "$([[ "$version" =~ ^[0-9]+$ ]] && echo yes || echo no)"

proj="$work/proj"
mkdir -p "$proj/.claude/ccy"
dist="$proj/.claude/ccy/ccy.env.local.dist"

check "fresh project: the sync passes" "0" "$(run_sync "$proj")"
check "fresh project: the dist is written" "yes" "$([ -f "$dist" ] && echo yes || echo no)"
check "fresh project: the launch says it wrote the dist" "yes" "$(log_has "ccy.env.local.dist")"
check "the dist's first line names its version" \
    "# ccy.env.local.dist version ${version}" "$(awk 'NR==1' "$dist")"
check "the dist documents the hooks daemon role override" "yes" \
    "$(grep -q 'HOOKS_DAEMON_HOSTNAME' "$dist" && echo yes || echo no)"
check "the dist is at least version 3, the one that took the agent team bus out" "yes" \
    "$([ "$version" -ge 3 ] && echo yes || echo no)"
# A team is given by each launch (ccy --teams), so the checkout's file has nothing to say
# about the bus, and the entrypoint refuses a bus variable it sets.
check "the dist names no agent team bus variable" "0" "$(grep -c 'PINGBUS' "$dist")"
check "the dist says the role also names a new seat's host" "yes" \
    "$(grep -qF 'ccy --teams' "$dist" && echo yes || echo no)"
check "the dist sets nothing: every non-blank line is a comment" "0" \
    "$(grep -cv '^[[:space:]]*\(#\|$\)' "$dist")"
check "the dist is valid bash" "0" "$(bash -n "$dist" >"$work/sync.log" 2>&1; echo $?)"

before=$(cat "$dist")
check "an up-to-date dist: the sync passes" "0" "$(run_sync "$proj")"
check "an up-to-date dist: nothing is said" "" "$(cat "$work/sync.log")"
check "an up-to-date dist: left as it was" "$before" "$(cat "$dist")"

printf '# ccy.env.local.dist version 0\n# old text\n' >"$dist"
check "an old dist: the sync passes" "0" "$(run_sync "$proj")"
check "an old dist: rewritten to the current one" "$before" "$(cat "$dist")"
check "an old dist: the launch says it was updated" "yes" "$(log_has "Updating .claude/ccy/ccy.env.local.dist")"

newer=$((version + 1))
printf '# ccy.env.local.dist version %s\n# from a newer ccy\n' "$newer" >"$dist"
check "a dist from a newer ccy: the sync passes" "0" "$(run_sync "$proj")"
check "a dist from a newer ccy: not rewritten down" "# ccy.env.local.dist version ${newer}" "$(awk 'NR==1' "$dist")"
check "a dist from a newer ccy: the launch says this ccy is older" "yes" "$(log_has "written by a newer ccy")"
printf '# based on ccy.env.local.dist version %s\n' "$version" >"$proj/.claude/ccy/ccy.env.local"
run_sync "$proj" >"$work/newer.code"
check "a dist from a newer ccy: a local file based on this ccy's version is warned" "yes" \
    "$(log_has "based on ccy.env.local.dist version ${version}; the dist is now version ${newer}")"
rm "$proj/.claude/ccy/ccy.env.local"
printf '%s\n' "$before" >"$dist"

rm "$dist"
(cd "$proj" && SCRIPT_DIR="$front" bash -c '. "$1"; ccy_env_local_dist_sync' _ "$LIB" 2>/dev/null) >"$work/stdout.log"
check "the sync prints nothing on stdout" "" "$(cat "$work/stdout.log")"

# A directory where the dist goes makes the write fail even for root.
rm "$dist"
mkdir "$dist"
check "a dist that cannot be written: the sync fails" "1" "$(run_sync "$proj")"
check "a dist that cannot be written: the launch says so" "yes" "$(log_has "$proj/.claude/ccy/ccy.env.local.dist is not a regular file")"
rmdir "$dist"
run_sync "$proj" >"$work/rewrite.code"

local_file="$proj/.claude/ccy/ccy.env.local"
printf 'export HOOKS_DAEMON_HOSTNAME=role-a\n' >"$local_file"
check "a local file with no based-on line: the sync passes" "0" "$(run_sync "$proj")"
check "a local file with no based-on line: warned" "yes" "$(log_has "has no '# based on ccy.env.local.dist version N' line")"

printf '# based on ccy.env.local.dist version 0\nexport HOOKS_DAEMON_HOSTNAME=role-a\n' >"$local_file"
check "a local file based on an older dist: the sync passes" "0" "$(run_sync "$proj")"
check "a local file based on an older dist: warned with both versions" "yes" \
    "$(log_has "based on ccy.env.local.dist version 0; the dist is now version ${version}")"

printf '# based on ccy.env.local.dist version %s\nexport HOOKS_DAEMON_HOSTNAME=role-a\n' "$version" >"$local_file"
check "a local file based on the current dist: the sync passes" "0" "$(run_sync "$proj")"
check "a local file based on the current dist: nothing is said" "" "$(cat "$work/sync.log")"
check "the local file is never written by ccy" \
    "# based on ccy.env.local.dist version ${version}" "$(awk 'NR==1' "$local_file")"

# mount_args <dir> [relabel]: the tokens ccy_env_local_mount_args builds there, one per
# line, or "exit N" when it fails. CONFIG_TEMP is a fresh private directory, $work/cfg.
mount_args() {
    rm -rf "$work/cfg"
    mkdir -m 700 "$work/cfg"
    # The library resets CCY_MOUNT_RELABEL when sourced, so it is set after.
    (cd "$1" && SCRIPT_DIR="$front" CONFIG_TEMP="$work/cfg" bash -c '
        . "$1"
        CCY_MOUNT_RELABEL="$2"
        ccy_env_local_mount_args || { echo "exit $?"; exit 0; }
        printf "%s\n" "${CCY_ENV_LOCAL_MOUNT[@]}"' _ "$LIB" "${2:-}" 2>"$work/sync.log")
}

check "the read-only mount binds a private copy of ccy.env.local, never the checkout path" \
    "-v
$work/cfg/ccy.env.local:/workspace/.claude/ccy/ccy.env.local:ro" "$(mount_args "$proj")"
check "the copy holds the checkout's file" "$(cat "$local_file")" "$(cat "$work/cfg/ccy.env.local")"
check "the read-only mount carries the SELinux relabel when there is one" \
    "-v
$work/cfg/ccy.env.local:/workspace/.claude/ccy/ccy.env.local:ro,z" "$(mount_args "$proj" z)"
rm "$local_file"
check "no ccy.env.local: no mount" "" "$(mount_args "$proj")"
mkdir "$local_file"
check "a ccy.env.local that is not a file stops the launch" "exit 1" "$(mount_args "$proj")"
rmdir "$local_file"

# Plan 00266 review finding 1: a container plants a link at ccy.env.local to a host file. The
# launch stops naming the file, binds nothing and copies nothing.
printf 'HOST ONLY\n' >"$work/host-only"
ln -s "$work/host-only" "$local_file"
check "a linked ccy.env.local stops the launch" "exit 1" "$(mount_args "$proj")"
check "a linked ccy.env.local: the launch names it" "yes" "$(log_has "$proj/.claude/ccy/ccy.env.local")"
check "a linked ccy.env.local: nothing is copied" "" "$(ls -A "$work/cfg")"
rm "$local_file"

# Plan 00266 review finding 6: a link at the dist is refused, never written through.
rm "$dist"
ln -s "$work/host-only" "$dist"
check "a linked dist: the sync fails" "1" "$(run_sync "$proj")"
check "a linked dist: the launch names it" "yes" "$(log_has "$proj/.claude/ccy/ccy.env.local.dist is a link")"
check "a linked dist: the host file is untouched" "HOST ONLY" "$(cat "$work/host-only")"
rm "$dist"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
