#!/usr/bin/env bash
# Unit-test the pre-commit hook's container version gate.
#
# WHY THIS TEST EXISTS. The entrypoint and the supervisor plugins are baked into the ccy image,
# and a running container only picks up a change to them through a container version bump. The
# gate rejects a commit that changes them without one, and a commit whose Dockerfile LABEL and
# launcher REQUIRED_CONTAINER_VERSION disagree. It runs the real hook against a throwaway
# repository holding a minimal copy of the ccy tree.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK_DIR="$SCRIPT_DIR/git-hooks"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

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

repo="$work/repo"
ccy="$repo/files/var/local/claude-yolo"
mkdir -p "$ccy/supervisor-plugins" "$repo/hooks"
cp -R "$HOOK_DIR/." "$repo/hooks/"
git -C "$repo" init -q
git -C "$repo" config user.name test
git -C "$repo" config user.email test@example.com

# write_launcher <ccy-version> <container-version>
write_launcher() {
    printf '#!/usr/bin/env bash\nCCY_VERSION="%s"\nREQUIRED_CONTAINER_VERSION="%s"\n' "$1" "$2" >"$ccy/claude-yolo"
}
# write_dockerfile <container-version> — the image copies the entrypoint, the plugin directory
# and one more file with a flag; a --from copy names a build-stage path, not a repo file.
write_dockerfile() {
    printf '%s\n' 'FROM scratch' "LABEL claude-yolo-version=\"$1\"" \
        'COPY entrypoint.sh /usr/local/bin/entrypoint.sh' \
        'COPY supervisor-plugins/ /opt/claude-yolo/supervisor-plugins/' \
        'COPY --chown=root:root guard-tool /opt/claude-yolo/guard-tool' \
        'COPY --from=builder /build/not-in-repo /usr/local/bin/not-in-repo' >"$ccy/Dockerfile"
}

write_launcher 1.0.0 2.0
write_dockerfile 2.0
printf '#!/usr/bin/env bash\necho start\n' >"$ccy/entrypoint.sh"
printf 'PLUGIN_API = 1\n' >"$ccy/supervisor-plugins/ccy_lifecycle.py"
printf '#!/usr/bin/env bash\necho guard\n' >"$ccy/guard-tool"
printf '#!/usr/bin/env bash\necho host\n' >"$ccy/host-only-tool"
git -C "$repo" add -A
git -C "$repo" commit -q --no-verify -m baseline

# hook_status — stage everything, run the real hook, print its exit status; reset the index.
hook_status() {
    local rc=0
    git -C "$repo" add -A
    (cd "$repo" && bash hooks/pre-commit) >"$work/hook.out" 2>&1 || rc=$?
    echo "$rc"
}
# baseline — put the tree back to the committed state.
baseline() {
    write_launcher 1.0.0 2.0
    write_dockerfile 2.0
    printf '#!/usr/bin/env bash\necho start\n' >"$ccy/entrypoint.sh"
    printf 'PLUGIN_API = 1\n' >"$ccy/supervisor-plugins/ccy_lifecycle.py"
    printf '#!/usr/bin/env bash\necho guard\n' >"$ccy/guard-tool"
    printf '#!/usr/bin/env bash\necho host\n' >"$ccy/host-only-tool"
    git -C "$repo" add -A
}
has() { if grep -q -F -- "$1" "$work/hook.out"; then echo yes; else echo no; fi; }

echo "=== the container version gate ==="

printf 'PLUGIN_API = 1\nVALUE = 2\n' >"$ccy/supervisor-plugins/ccy_lifecycle.py"
check "a plugin change without a container bump is rejected" "1" "$(hook_status)"
check "…and says to bump the container version" "yes" "$(has 'container version bump required')"
baseline

printf '#!/usr/bin/env bash\necho started\n' >"$ccy/entrypoint.sh"
check "an entrypoint change without a container bump is rejected" "1" "$(hook_status)"
baseline

printf '#!/usr/bin/env bash\n# why it starts\necho start\n' >"$ccy/entrypoint.sh"
check "a comment-only entrypoint change needs no bump" "0" "$(hook_status)"
baseline

printf 'PLUGIN_API = 1\nVALUE = 2\n' >"$ccy/supervisor-plugins/ccy_lifecycle.py"
write_dockerfile 2.1
write_launcher 1.0.1 2.1
check "a plugin change with both versions bumped together passes" "0" "$(hook_status)"
check "…and reports the bump" "yes" "$(has '2.0 → 2.1')"
baseline

printf 'PLUGIN_API = 1\nVALUE = 2\n' >"$ccy/supervisor-plugins/ccy_lifecycle.py"
write_dockerfile 2.1
check "a LABEL bump the launcher does not match is rejected" "1" "$(hook_status)"
check "…and names both values" "yes" "$(has '(2.1) and the launcher')"
baseline

printf '#!/usr/bin/env bash\necho guarded\n' >"$ccy/guard-tool"
check "any file the Dockerfile COPYs is guarded, not just a listed few" "1" "$(hook_status)"
baseline

printf '#!/usr/bin/env bash\necho host side\n' >"$ccy/host-only-tool"
check "a file in the ccy directory the image does not copy needs no container bump" "0" "$(hook_status)"
check "…and prints no container check" "no" "$(has 'container version bump requirement')"
baseline

printf 'unrelated\n' >"$repo/README.txt"
check "a commit touching no image file is not judged" "0" "$(hook_status)"
check "…and prints no container check" "no" "$(has 'container version bump requirement')"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
