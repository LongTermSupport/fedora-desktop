#!/usr/bin/env bash
# Unit-test the entrypoint's project env step: .claude/ccy/ccy.env, then ccy.env.local.
#
# WHY THIS TEST EXISTS. ccy.env is tracked and shared by every clone; ccy.env.local is the
# untracked per-checkout file sourced after it, so its values win. The test cuts the step out
# of the real entrypoint (between its PROJECT-ENV markers), points it at a throwaway project
# directory, and runs it with each combination of files present.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENTRYPOINT="$SCRIPT_DIR/../files/var/local/claude-yolo/entrypoint.sh"

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

step="$work/step.bash"
awk -v dir="$work/ccy" '
    /^# >>> PROJECT-ENV/ { on = 1; next }
    /^# <<< PROJECT-ENV/ { on = 0 }
    on { gsub("/workspace/.claude/ccy", dir); print }
' "$ENTRYPOINT" >"$step"
if [ ! -s "$step" ]; then
    echo "FAIL: no PROJECT-ENV block found in $ENTRYPOINT"
    exit 1
fi

# run_step: source the step in a fresh shell, then print what the session would see.
run_step() {
    bash -c '. "$1" >/dev/null; printf "%s|%s|%s" "${A:-}" "${B:-}" "${C:-}"' _ "$step"
}

mkdir -p "$work/ccy"

check "neither file: nothing set" "||" "$(run_step)"

cat >"$work/ccy/ccy.env" <<'EOF'
export A=project
export B="${B:-project}"
EOF
check "ccy.env only" "project|project|" "$(run_step)"

printf 'export B=local\nexport C=local\n' >"$work/ccy/ccy.env.local"
check "ccy.env.local wins over ccy.env and adds its own" "project|local|local" "$(run_step)"

rm "$work/ccy/ccy.env"
check "ccy.env.local without ccy.env" "|local|local" "$(run_step)"

cat >"$work/ccy/ccy.env.local" <<'EOF'
export A="$A-then-local"
EOF
printf 'export A=project\n' >"$work/ccy/ccy.env"
check "ccy.env.local sees ccy.env's values" "project-then-local||" "$(run_step)"

cat >"$work/ccy/ccy.env.local" <<'EOF'
declare -x D=declared
EOF
got="$(bash -c '. "$1" >/dev/null; printf "%s" "${D:-}"' _ "$step")"
check "a declare in ccy.env.local stays global" "declared" "$got"

order="$(bash -c '. "$1"' _ "$step" | awk '{print $NF}' | awk -F/ '{print $NF}' | paste -sd, -)"
check "both are announced, ccy.env first" "ccy.env,ccy.env.local" "$order"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
