#!/usr/bin/env bash
# Unit-test ccy_env_local_dist_sync (files/var/local/claude-yolo/lib/common.bash).
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
    (cd "$1" && bash -c '. "$1"; ccy_env_local_dist_sync' _ "$LIB" >"$work/sync.log" 2>&1)
    echo "$?"
}

# current_version: the version the library writes, read from the library itself.
current_version() {
    bash -c '. "$1"; echo "$CCY_ENV_LOCAL_DIST_VERSION"' _ "$LIB"
}

log_has() {
    if grep -qF -- "$1" "$work/sync.log"; then echo yes; else echo no; fi
}

version=$(current_version)
check "the library declares a whole-number dist version" "yes" \
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
(cd "$proj" && bash -c '. "$1"; ccy_env_local_dist_sync' _ "$LIB" 2>/dev/null) >"$work/stdout.log"
check "the sync prints nothing on stdout" "" "$(cat "$work/stdout.log")"

# A directory where the dist goes makes the write fail even for root.
rm "$dist"
mkdir "$dist"
check "a dist that cannot be written: the sync fails" "1" "$(run_sync "$proj")"
check "a dist that cannot be written: the launch says so" "yes" "$(log_has "Could not write")"
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

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
