#!/usr/bin/env bash
# acceptance.bash — Plan 00140's PASS/FAIL gate: through the session guard, the
# patterns that leaked browsers leave at most one browser per browser command, and
# everything that must keep working does.
#
# WHERE TO RUN: inside a ccy container (plan_require_container). The browsers exist
# only in the image. Uses agent-browser-headless and agent-browser-lite-headless ONLY:
# never headed, which would put windows on the owner's desktop.
#
#   ./acceptance.bash              the INSTALLED wrappers (after the host deploy and
#                                  image rebuild): also asserts they route through the
#                                  guard and that the installed guard is this checkout's
#   ./acceptance.bash --checkout   this checkout's guard in front of the installed
#                                  binary, before any deploy
#
# Changes nothing in the image or the repo. It opens and closes browser sessions, and
# it refuses to start while any session is open, so it cannot close someone else's.
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
  if [[ -e "${repoRoot}/.git" ]]; then
    printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
    exit 1
  fi
  repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: acceptance.bash [--checkout] [-h|--help]"
plan_parse_common_flags "$@"
MODE=installed
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    case "$arg" in
        --checkout) MODE=checkout ;;
        *) printf 'unknown argument: %s\n%s\n' "$arg" "$PLAN_USAGE" >&2; exit 1 ;;
    esac
done

if ! plan_require_container "the browsers and their wrappers exist only inside the ccy image"; then
    echo "VERDICT: COULD NOT ESTABLISH on the host. Start a NEW ccy session in this repo" >&2
    echo "         (one started before the image rebuild still runs the old wrappers) and" >&2
    echo "         run ./acceptance.bash inside it." >&2
    exit 2
fi
plan_start_log auto

GUARD="${PLAN_REPO_ROOT}/files/var/local/claude-yolo/agent-browser-session-guard"
INSTALLED_GUARD=/opt/claude-yolo/agent-browser-session-guard
REAL="$(npm root -g)/agent-browser/bin/agent-browser-linux-x64"
[[ -x "$REAL" ]] || { echo "[FATAL] agent-browser binary not found at $REAL" >&2; exit 1; }
PAGE='data:text/html,<title>t</title>hello'
TOTAL=0
RAN=0
FAILED=()

# hl / lite: the two browser commands under test, in the chosen mode.
hl() {
    if [[ "$MODE" == checkout ]]; then
        bash "$GUARD" agent-browser-headless "$REAL" --namespace headless --headed false -- "$@"
    else
        agent-browser-headless "$@"
    fi
}
lite() {
    if [[ "$MODE" == checkout ]]; then
        bash "$GUARD" agent-browser-lite-headless "$REAL" --namespace lightpanda \
            --config /root/.agent-browser/lightpanda.json -- "$@"
    else
        agent-browser-lite-headless "$@"
    fi
}

# browser_roots <chrome|lightpanda>: how many browser root processes run, from /proc.
# A Chromium root is a chrome process without --type= (every child carries one).
browser_roots() {
    local want="$1" n=0 d exe cmd
    for d in /proc/[0-9]*; do
        exe="$(readlink "$d/exe" 2> /dev/null)" || continue
        case "$exe" in
            */"$want") ;;
            *) continue ;;
        esac
        cmd="$(tr '\0' ' ' < "$d/cmdline")" || continue
        [[ "$cmd" == *--type=* ]] || n=$((n + 1))
    done
    echo "$n"
}

check() {
    local desc="$1" ok="$2"
    RAN=$((RAN + 1))
    if [[ "$ok" == 1 ]]; then
        printf '  [PASS] %s\n' "$desc"
    else
        printf '  [FAIL] %s\n' "$desc"
        FAILED+=("$desc")
    fi
}

# expect_rc <description> <rc> <command...>: run it, compare the exit status.
expect_rc() {
    local desc="$1" want="$2" rc=0 out
    shift 2
    out="$("$@" 2>&1)" || rc=$?
    if [[ "$rc" == "$want" ]]; then check "$desc (exit $rc)" 1; else
        check "$desc (exit $rc, wanted $want)" 0
        printf '         %s\n' "$(printf '%s' "$out" | tr '\n' ' ' | cut -c1-240)"
    fi
}

close_everything() {
    hl close --all > /dev/null
    lite close --all > /dev/null
}

# The two shapes an agent types that expect_rc cannot express as one argv.
hl_env_session() { AGENT_BROWSER_SESSION=task-c hl "$@"; }
close_then_open() { hl close --all > /dev/null && hl --session task-b open "$PAGE"; }

echo "== mode: $MODE"
for listing in "$(hl --json session list)" "$(lite --json session list)"; do
    if [[ "$(jq '.data.sessions | length' <<< "$listing")" != 0 ]]; then
        echo "[FATAL] a browser session is already open: $listing" >&2
        echo "        Refusing to start: this run closes every session when it finishes." >&2
        exit 1
    fi
done
plan_on_cleanup close_everything

TOTAL=14
[[ "$MODE" == installed ]] && TOTAL=$((TOTAL + 4))

if [[ "$MODE" == installed ]]; then
    echo "== the installed wrappers route through the installed guard"
    for w in agent-browser-headed agent-browser-headless agent-browser-lite-headless; do
        if grep -q "^exec ${INSTALLED_GUARD} ${w} .* -- \"\$@\"$" "/usr/local/bin/$w"; then
            check "$w execs the guard" 1
        else
            check "$w execs the guard" 0
        fi
    done
    if cmp -s "$GUARD" "$INSTALLED_GUARD"; then check "installed guard is this checkout's" 1; else
        check "installed guard is this checkout's" 0
    fi
fi

echo "== headless: the leaking patterns"
expect_rc "first session (--session task-a) opens" 0 hl --session task-a open "$PAGE"
expect_rc "the default session is refused while task-a is open" 3 hl open "$PAGE"
expect_rc "AGENT_BROWSER_SESSION=task-c is refused" 3 hl_env_session open "$PAGE"
expect_rc "reusing task-a works" 0 hl --session task-a get url
check "exactly one Chromium root after all that (have $(browser_roots chrome))" "$([[ "$(browser_roots chrome)" == 1 ]] && echo 1 || echo 0)"

echo "== headless: nothing that starts no browser is refused"
expect_rc "session list" 0 hl session list
expect_rc "skills list" 0 hl skills list
expect_rc "--version" 0 hl --version

echo "== headless: close then open a new session in one chain"
expect_rc "close --all && --session task-b open" 0 close_then_open
check "still exactly one Chromium root (have $(browser_roots chrome))" "$([[ "$(browser_roots chrome)" == 1 ]] && echo 1 || echo 0)"

echo "== lite: the same leak"
expect_rc "first lite session opens" 0 lite --session task-a open "$PAGE"
expect_rc "a second lite session is refused" 3 lite --session task-b open "$PAGE"
check "exactly one Lightpanda (have $(browser_roots lightpanda))" "$([[ "$(browser_roots lightpanda)" == 1 ]] && echo 1 || echo 0)"

echo "== close --all leaves nothing"
close_everything
sleep 1
check "no Chromium or Lightpanda left ($(browser_roots chrome)/$(browser_roots lightpanda))" \
    "$([[ "$(browser_roots chrome)$(browser_roots lightpanda)" == 00 ]] && echo 1 || echo 0)"

echo
echo "COVERAGE: ${RAN} of ${TOTAL} checks executed"
if (( RAN != TOTAL )); then
    echo "VERDICT: REJECTED (incomplete run)"
    exit 1
fi
if (( ${#FAILED[@]} )); then
    printf 'VERDICT: REJECTED (%d failed)\n' "${#FAILED[@]}"
    printf '  - %s\n' "${FAILED[@]}"
    exit 1
fi
echo "VERDICT: ACCEPTED"
