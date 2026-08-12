#!/usr/bin/env bash
#
# Plan 00070 acceptance — the PASS/FAIL gate for the two-engine browser stack.
#
# triage.bash gathers facts and renders no verdict. THIS script renders the
# verdict: it asserts that what the plan promised is what the image actually
# delivers, and exits non-zero if any assertion fails. See CLAUDE/PlanTriage.md
# for the split.
#
# It uses NO overrides anywhere — every browser command is exactly what an agent
# would type after reading the browsing skill. That is the point: an acceptance
# test that reaches past the shipped configuration proves the engine works while
# saying nothing about whether the DELIVERY works.
#
# What it asserts, in order:
#
#   A. the image ships all three artifacts, pinned to the versions the repo declares
#   B. both engines execute JavaScript end-to-end and return the rendered DOM
#   C. agent-browser-lite really drives Lightpanda (not a silent Chromium fallback)
#   D. the documented boundary still holds — Lightpanda still fails SILENTLY on
#      pixels, and Chromium still renders truthfully. If upstream ever fixes the
#      silent failure, this FAILS on purpose: the skill would then be teaching a
#      warning that is no longer true.
#   E. the guidance is reachable AT RUNTIME, not merely present in the repo
#   F. the repo's version pins agree with what is installed
#
# Side effects: opens and closes browser sessions, serves fixtures on 127.0.0.1,
# writes a log into this plan's logs/. Changes nothing in the image or the repo.

set -euo pipefail

# --- argument parsing FIRST, before any environment resolution (PlanTriage.md) ---

CONTAINER=""

usage() {
    cat <<'USAGE'
Plan 00070 acceptance — pass/fail gate for the CCY two-engine browser stack.

Usage:
  acceptance.bash [options]

RUN IT FROM THE HOST, like every other plan script in this repo. The browsers it
tests only exist inside the CCY image, so a host run automatically re-executes
this same script inside a running CCY container (the repo is bind-mounted there
at /workspace, so it is the very same file). Running it inside a container also
works and skips the dispatch.

Options:
  --container <name>   Use this CCY container instead of auto-detecting
  -h, --help           Show this help

Exit status:
  0  every assertion passed
  1  at least one assertion failed (the summary names which)

Output:
  Writes a full report to <plan folder>/logs/browser-engine-acceptance.log
  (that directory is gitignored — it captures live container state).

Requires, inside the container: agent-browser, curl, python3, timeout, convert.
Requires, on the host: podman (or docker) with a CCY container running.
USAGE
}

while [ $# -gt 0 ]; do
    case "$1" in
        --container)
            if [ $# -lt 2 ]; then
                echo "ERROR: --container needs a value." >&2
                exit 1
            fi
            CONTAINER="$2"
            shift 2
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        *)
            echo "ERROR: unknown option '$1'. See --help." >&2
            exit 1
            ;;
    esac
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --- host dispatch: run from the host, assert inside the container ---------------

if [ ! -f /run/.containerenv ] && [ ! -f /.dockerenv ]; then
    engine=""
    for candidate in podman docker; do
        if command -v "$candidate" > /dev/null; then
            engine="$candidate"
            break
        fi
    done
    if [ -z "$engine" ]; then
        echo "ERROR: neither podman nor docker is on PATH — cannot reach a CCY container." >&2
        echo "  Podman is installed by playbooks/imports/play-podman.yml." >&2
        exit 1
    fi

    repo_root="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"
    project="$(basename "$repo_root")"
    script_in_container="/workspace/${PLAN_DIR#"$repo_root"/}/$(basename "${BASH_SOURCE[0]}")"

    if [ -z "$CONTAINER" ]; then
        running="$("$engine" ps --format '{{.Names}} {{.Image}}')"
        mine="$(printf '%s\n' "$running" | awk -v p="^${project}_yolo" '$1 ~ p {print $1}')"
        if [ -z "$mine" ]; then
            mine="$(printf '%s\n' "$running" | awk '$2 ~ /claude-yolo/ {print $1}')"
            if [ -n "$mine" ]; then
                echo "NOTE: no ${project}_yolo container running; falling back to any CCY container." >&2
            fi
        fi
        count="$(printf '%s\n' "$mine" | grep -c .)"
        if [ "$count" -eq 0 ]; then
            echo "ERROR: no running CCY container found — nothing to accept." >&2
            echo "  Start one, then re-run this script from the host:" >&2
            echo "    cd $repo_root && ccy" >&2
            exit 1
        fi
        if [ "$count" -gt 1 ]; then
            echo "ERROR: several CCY containers are running; pick one with --container:" >&2
            printf '%s\n' "$mine" | while IFS= read -r name; do
                printf '    %s\n' "$name" >&2
            done
            exit 1
        fi
        CONTAINER="$mine"
    fi

    echo "Host run — dispatching into CCY container '$CONTAINER' via $engine." >&2
    exec "$engine" exec "$CONTAINER" "$script_in_container"
fi

# --- environment ---

REPORTS_DIR="$PLAN_DIR/logs"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/browser-engine-acceptance.log"
exec > >(tee "$LOG") 2>&1

REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"
DOCKERFILE="$REPO_ROOT/files/var/local/claude-yolo/Dockerfile"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

WORK="$(mktemp -d)"
FIXTURES="$WORK/fixtures"
SERVER_PID=""
PORT=8738

PASSES=0
FAILURES=0
FAILED_LIST=""

pass() {
    printf '  [PASS] %s\n' "$1"
    PASSES=$((PASSES + 1))
}

# A failure records WHY, not just that. The detail line is what makes the log
# actionable without re-running anything.
fail() {
    printf '  [FAIL] %s\n' "$1"
    if [ -n "${2:-}" ]; then
        printf '         %s\n' "$2"
    fi
    FAILURES=$((FAILURES + 1))
    FAILED_LIST="$FAILED_LIST
    - $1"
}

snippet() {
    printf '%s' "$1" | tr '\n' ' ' | cut -c1-160
}

# Run a command, capture combined output, never let a non-zero status abort the
# gate — the status is the thing being asserted about.
try() {
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    TRY_OUT="$out"
    TRY_RC="$rc"
    return 0
}

# Reports rather than hides a failure: a close that fails leaves a daemon holding
# a browser, which would skew the very next assertion.
close_sessions() {
    local out
    if ! command -v agent-browser > /dev/null; then
        return 0
    fi
    if out="$(agent-browser close --all 2>&1)"; then
        return 0
    fi
    printf '  (note: close --all did not succeed: %s)\n' "$(snippet "$out")"
}

cleanup() {
    if [ -n "$SERVER_PID" ] && ps -p "$SERVER_PID" > /dev/null; then
        kill "$SERVER_PID"
    fi
    close_sessions
    rm -rf "$WORK"
}
trap cleanup EXIT

echo "==============================================================================="
echo " Plan 00070 — browser engine ACCEPTANCE"
echo " host: $(uname -srm)   date: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
echo "==============================================================================="
echo

# --- dependency gate: a missing tool is an IaC gap, never a skip ---------------

for tool in agent-browser curl python3 timeout convert; do
    if ! command -v "$tool" > /dev/null; then
        echo "ERROR: '$tool' is not installed in this container." >&2
        echo "  The CCY image is built by files/var/local/claude-yolo/Dockerfile." >&2
        echo "  Add it there and rebuild — do NOT install it by hand." >&2
        exit 1
    fi
done

# --- A. shipped artifacts --------------------------------------------------------

echo "## A. The image ships the engine"
echo

if [ -x /usr/local/bin/lightpanda ]; then
    pass "lightpanda binary installed and executable"
else
    fail "lightpanda binary installed and executable" "/usr/local/bin/lightpanda missing or not +x"
fi

if [ -x /usr/local/bin/agent-browser-lite ]; then
    pass "agent-browser-lite wrapper installed and executable"
else
    fail "agent-browser-lite wrapper installed and executable" "/usr/local/bin/agent-browser-lite missing or not +x"
fi

if [ -f /root/.agent-browser/lightpanda.json ]; then
    try python3 -c "import json,sys; c=json.load(open('/root/.agent-browser/lightpanda.json')); sys.exit(0 if c.get('engine')=='lightpanda' else 1)"
    if [ "$TRY_RC" -eq 0 ]; then
        pass "lightpanda config is valid JSON declaring engine=lightpanda"
    else
        fail "lightpanda config is valid JSON declaring engine=lightpanda" "$(snippet "$TRY_OUT")"
    fi
else
    fail "lightpanda config is valid JSON declaring engine=lightpanda" "/root/.agent-browser/lightpanda.json missing"
fi

# The wrapper must point at the config; a wrapper that lost its --config would
# silently run Chromium while every caller believed it was running Lightpanda.
if grep -q -- '--config /root/.agent-browser/lightpanda.json' /usr/local/bin/agent-browser-lite; then
    pass "wrapper selects the lightpanda config"
else
    fail "wrapper selects the lightpanda config" "no --config /root/.agent-browser/lightpanda.json in the wrapper"
fi

# Without its own namespace the light engine shares a daemon with Chromium and
# whichever ran first wins — see the interleave assertion in section C.
if grep -q -- '--namespace lightpanda' /usr/local/bin/agent-browser-lite; then
    pass "wrapper isolates the lightpanda daemon namespace"
else
    fail "wrapper isolates the lightpanda daemon namespace" \
        "no --namespace in the wrapper — the two engines will fight over one daemon"
fi

# Chromium must remain untouched — the plan added an engine, it did not replace one.
try agent-browser --version
if [ "$TRY_RC" -eq 0 ]; then
    pass "agent-browser (Chromium default) still present: $(snippet "$TRY_OUT")"
else
    fail "agent-browser (Chromium default) still present" "$(snippet "$TRY_OUT")"
fi
echo

# --- B/C/D need fixtures ---------------------------------------------------------

mkdir -p "$FIXTURES"

cat > "$FIXTURES/dom.html" <<'HTML'
<!doctype html><meta charset=utf-8><title>dom</title>
<body><p id=t>nothing</p>
<script>document.getElementById('t').textContent = 'MARKER-DOM';</script>
</body>
HTML

cat > "$FIXTURES/data.json" <<'JSON'
{"value": "MARKER-FETCH"}
JSON

cat > "$FIXTURES/fetch.html" <<'HTML'
<!doctype html><meta charset=utf-8><title>fetch</title>
<body><p id=t>nothing</p>
<script>
fetch('data.json').then(function (r) { return r.json(); }).then(function (d) {
  document.getElementById('t').textContent = d.value;
});
</script>
</body>
HTML

# Flat, unmistakable colours so a screenshot is checked by histogram, not by eye.
cat > "$FIXTURES/vis.html" <<'HTML'
<!doctype html><meta charset=utf-8><title>vis</title>
<body style="background:#ffffff">
<h1 style="color:#cc0000;font-size:48px">VISIBLE HEADING</h1>
<p style="color:#006600;font-size:24px">Some green body text for the render test.</p>
<div style="width:300px;height:120px;background:#0000cc"></div>
</body>
HTML

python3 -m http.server "$PORT" --directory "$FIXTURES" --bind 127.0.0.1 > "$WORK/http.log" 2>&1 &
SERVER_PID=$!
sleep 2

if ! ps -p "$SERVER_PID" > /dev/null; then
    echo "ERROR: the fixture web server failed to start on port $PORT." >&2
    echo "  Without it every browser assertion below would fail for the wrong" >&2
    echo "  reason, which is worse than not running them." >&2
    cat "$WORK/http.log" >&2
    exit 1
fi

echo "## B. Both engines execute JavaScript end-to-end"
echo

# $1 = command word (agent-browser | agent-browser-lite), rest = extra flags
render_marker() {
    local cmd="$1" fixture="$2" marker="$3"
    shift 3
    local out rc
    close_sessions
    if out="$(timeout 90 "$cmd" "$@" open "http://127.0.0.1:$PORT/$fixture" 2>&1 \
        && timeout 90 "$cmd" "$@" wait 1000 2>&1 \
        && timeout 90 "$cmd" "$@" get text body 2>&1)"; then
        rc=0
    else
        rc=$?
    fi
    RENDER_OUT="$out"
    RENDER_RC="$rc"
    printf '%s' "$out" | grep -q "$marker"
}

if render_marker agent-browser-lite dom.html MARKER-DOM; then
    pass "agent-browser-lite renders an inline DOM write (MARKER-DOM)"
else
    fail "agent-browser-lite renders an inline DOM write (MARKER-DOM)" \
        "rc=$RENDER_RC got: $(snippet "$RENDER_OUT")"
fi

if render_marker agent-browser-lite fetch.html MARKER-FETCH; then
    pass "agent-browser-lite completes fetch() + JSON + render (MARKER-FETCH)"
else
    fail "agent-browser-lite completes fetch() + JSON + render (MARKER-FETCH)" \
        "rc=$RENDER_RC got: $(snippet "$RENDER_OUT")"
fi

# Chromium is asserted headless here purely so the gate is safe to run unattended;
# headed is its shipped default and is what an agent gets interactively.
if render_marker agent-browser dom.html MARKER-DOM --headed false; then
    pass "agent-browser (Chromium) renders an inline DOM write (MARKER-DOM)"
else
    fail "agent-browser (Chromium) renders an inline DOM write (MARKER-DOM)" \
        "rc=$RENDER_RC got: $(snippet "$RENDER_OUT")"
fi
echo

echo "## C. agent-browser-lite really drives Lightpanda"
echo
echo "   A silent fallback to Chromium would pass every test above while"
echo "   delivering none of the benefit, so prove the process identity."
echo

close_sessions
try timeout 90 agent-browser-lite open "http://127.0.0.1:$PORT/dom.html"
if [ "$TRY_RC" -ne 0 ]; then
    fail "a lightpanda process is running behind agent-browser-lite" \
        "could not open a session: $(snippet "$TRY_OUT")"
else
    lp_procs=0
    if pgrep -c lightpanda > /dev/null; then
        lp_procs="$(pgrep -c lightpanda)"
    fi
    if [ "$lp_procs" -ge 1 ]; then
        pass "a lightpanda process is running behind agent-browser-lite ($lp_procs)"
    else
        fail "a lightpanda process is running behind agent-browser-lite" \
            "no lightpanda process found — the wrapper is falling back to Chromium"
    fi
fi
close_sessions
echo

# REGRESSION GUARD: the skill tells the agent it can "fall back freely" between the
# engines. Before the namespace fix that advice was false — a lite session left the
# shared daemon bound to Lightpanda, and the next Chromium call died with "Custom
# Chrome arguments (--args) are not supported with Lightpanda" (3/3 reproductions),
# which `close --all` did NOT cure because the close returns before the daemon exits.
# Interleave the engines with NO closes at all: that is what an agent actually does.

# Deliberately does NOT call close_sessions — the whole assertion is that no
# teardown is needed. render_marker() closes first, so it cannot be reused here.
render_marker_noclose() {
    local cmd="$1" fixture="$2" marker="$3"
    shift 3
    local out rc
    if out="$(timeout 90 "$cmd" "$@" open "http://127.0.0.1:$PORT/$fixture" 2>&1 \
        && timeout 90 "$cmd" "$@" get text body 2>&1)"; then
        rc=0
    else
        rc=$?
    fi
    RENDER_OUT="$out"
    RENDER_RC="$rc"
    printf '%s' "$out" | grep -q "$marker"
}


# One retry per call, and it is REPORTED when used.
#
# Launching Chromium in this container fails occasionally with "Could not
# configure browser: Failed to connect" — measured 0/12 trials in isolation
# (36 launches) but roughly 1-in-2 across a FULL gate run, so it is a
# cumulative-load flake in agent-browser/Chromium, not something the engine
# split introduced. A gate that cries wolf every other run gets ignored, but
# swallowing the retry silently would hide a real degradation, so a retried
# call still passes AND prints that it needed retrying. A genuine regression
# (e.g. the namespace flag removed) fails both attempts, every round.
retry_render() {
    if render_marker_noclose "$@"; then
        return 0
    fi
    RETRY_NOTE="$RETRY_NOTE
         (retried after: $(snippet "$RENDER_OUT"))"
    sleep 3
    render_marker_noclose "$@"
}

interleave_ok=1
interleave_note=""
RETRY_NOTE=""
for round in 1 2 3; do
    if ! retry_render agent-browser-lite dom.html MARKER-DOM; then
        interleave_ok=0
        interleave_note="round $round lite: rc=$RENDER_RC $(snippet "$RENDER_OUT")"
        break
    fi
    if ! retry_render agent-browser dom.html MARKER-DOM --headed false; then
        interleave_ok=0
        interleave_note="round $round chromium: rc=$RENDER_RC $(snippet "$RENDER_OUT")"
        break
    fi
done
if [ "$interleave_ok" -eq 1 ]; then
    pass "the engines interleave freely (3 rounds, no teardown between them)$RETRY_NOTE"
else
    fail "the engines interleave freely (3 rounds, no teardown between them)" "$interleave_note"
fi
close_sessions
echo

echo "## D. The documented boundary still holds"
echo
echo "   The skill warns that Lightpanda fails SILENTLY on pixels. If upstream"
echo "   ever fixes that, these assertions fail ON PURPOSE — the skill would be"
echo "   teaching a warning that is no longer true, and must be rewritten."
echo

# ONE capture attempt. Echoes how many of the page's three flat colours the PNG
# contains (0-3), or "err:<reason>" if the capture itself did not complete.
capture_colours() {
    local cmd="$1"
    shift
    local shot hist found colour out
    shot="$WORK/vis-$(basename "$cmd").png"
    rm -f "$shot"

    close_sessions
    if ! out="$(timeout 90 "$cmd" "$@" open "http://127.0.0.1:$PORT/vis.html" 2>&1)"; then
        echo "err:open ($(snippet "$out"))"
        return 0
    fi
    if ! out="$(timeout 90 "$cmd" "$@" wait 1500 2>&1)"; then
        echo "err:wait ($(snippet "$out"))"
        return 0
    fi
    if ! out="$(timeout 90 "$cmd" "$@" screenshot "$shot" 2>&1)"; then
        echo "err:screenshot ($(snippet "$out"))"
        return 0
    fi
    if [ ! -f "$shot" ]; then
        echo 0
        return 0
    fi

    hist="$(convert "$shot" -format %c -depth 8 histogram:info:-)"
    found=0
    for colour in CC0000 006600 0000CC; do
        if printf '%s' "$hist" | grep -qi "#$colour"; then
            found=$((found + 1))
        fi
    done
    echo "$found"
}

# BEST of N attempts, because `open` returns before Chromium has necessarily
# painted and a fixed settle wait does not close the window under load (a 1500ms
# wait still produced 1/3 on one full-gate run). Taking the best attempt removes
# the race from BOTH assertions below, and it makes the Lightpanda one STRONGER
# rather than weaker: it must fail to render on *every* attempt, so a pass can no
# longer be an artifact of capturing too early.
#
# Sets the globals BEST_COLOURS and SHOT_TRACE rather than echoing a value: a
# command substitution runs in a SUBSHELL, so a trace assigned inside one would
# never reach the caller (it cost a run of "SHOT_TRACE: unbound variable" to
# remember that).
best_colours() {
    local attempts="$1"
    shift
    local attempt result
    BEST_COLOURS=0
    SHOT_TRACE=""
    for attempt in $(seq 1 "$attempts"); do
        result="$(capture_colours "$@")"
        SHOT_TRACE="$SHOT_TRACE #$attempt=$result"
        case "$result" in
            [0-9]*)
                if [ "$result" -gt "$BEST_COLOURS" ]; then BEST_COLOURS="$result"; fi
                if [ "$BEST_COLOURS" -eq 3 ]; then break; fi
                ;;
        esac
    done
}

best_colours 4 agent-browser --headed false
chrome_best="$BEST_COLOURS"
if [ "$chrome_best" -eq 3 ]; then
    pass "agent-browser screenshot truthfully depicts the page (3/3 colours; attempts:$SHOT_TRACE)"
else
    fail "agent-browser screenshot truthfully depicts the page" \
        "best was $chrome_best/3 over 4 attempts:$SHOT_TRACE"
fi

# Inverted on purpose: Lightpanda must NOT render. Four attempts, so a pass means
# "never rendered", not "we happened to look too early".
best_colours 4 agent-browser-lite
lite_best="$BEST_COLOURS"
if [ "$lite_best" -eq 3 ]; then
    fail "agent-browser-lite screenshot is still a placeholder (skill warning accurate)" \
        "it now renders truthfully (attempts:$SHOT_TRACE) — the browsing skill's warning is STALE and must be rewritten"
else
    pass "agent-browser-lite screenshot is still a placeholder (skill warning accurate) — best $lite_best/3 over 4 attempts:$SHOT_TRACE"
fi

# The exit status is the trap the skill exists to warn about: it must stay 0.
close_sessions
try timeout 90 agent-browser-lite open "http://127.0.0.1:$PORT/vis.html"
try timeout 90 agent-browser-lite screenshot "$WORK/exitcheck.png"
if [ "$TRY_RC" -eq 0 ]; then
    pass "agent-browser-lite screenshot still exits 0 (the silent-failure trap is real)"
else
    fail "agent-browser-lite screenshot still exits 0 (the silent-failure trap is real)" \
        "it now exits $TRY_RC — the skill should say 'check the exit code' instead"
fi
close_sessions
echo

echo "## E. The guidance is reachable AT RUNTIME"
echo
echo "   Not 'present in the repo' — reachable by the agent inside a live"
echo "   session. A skill the agent cannot load teaches nobody anything."
echo

RUNTIME_SKILL="/root/.claude/skills/browsing/SKILL.md"
if [ -f "$RUNTIME_SKILL" ]; then
    pass "browsing skill is loadable at $RUNTIME_SKILL"
    if grep -q 'need to SEE' "$RUNTIME_SKILL"; then
        pass "browsing skill teaches the visibility-first rule"
    else
        fail "browsing skill teaches the visibility-first rule" \
            "no 'need to SEE' text — the runtime copy predates container 2.24"
    fi
    if grep -q 'agent-browser-lite' "$RUNTIME_SKILL"; then
        pass "browsing skill names agent-browser-lite"
    else
        fail "browsing skill names agent-browser-lite" "the runtime copy predates container 2.23"
    fi
else
    fail "browsing skill is loadable at $RUNTIME_SKILL" \
        "ABSENT — the image bakes it into /root/.claude/skills/, which entrypoint.sh then replaces with a symlink to /workspace/.claude/ccy"
fi

GUIDE=/opt/claude-yolo/docs/CCY-GUIDE.txt
if [ -f "$GUIDE" ] && grep -q 'SEE this happening' "$GUIDE"; then
    pass "CCY-GUIDE.txt carries the visibility-first rule"
else
    fail "CCY-GUIDE.txt carries the visibility-first rule" "missing or predates container 2.24"
fi

# The skill points the agent here instead of keeping a copy that drifts, so the
# pointer has to actually resolve.
try timeout 60 agent-browser skills get core --full
if [ "$TRY_RC" -eq 0 ] && [ "${#TRY_OUT}" -gt 1000 ]; then
    pass "agent-browser ships its own command reference (${#TRY_OUT} chars)"
else
    fail "agent-browser ships its own command reference" \
        "rc=$TRY_RC len=${#TRY_OUT} — the skill points at a reference that does not resolve"
fi

for obsolete in COMMANDLINE-USAGE.md EXAMPLES.md; do
    if [ -e "/root/.claude/skills/browsing/$obsolete" ]; then
        fail "obsolete $obsolete is gone from the image" \
            "still present — it documents an 'agent-browser run' syntax the CLI does not have"
    else
        pass "obsolete $obsolete is gone from the image"
    fi
done
echo

echo "## F. Repo pins agree with what is installed"
echo

dockerfile_value() {
    awk -v key="$1" '$0 ~ "^ARG " key "=" { sub(/^ARG [A-Z_]+=/, ""); gsub(/"/, ""); print; exit }' "$DOCKERFILE"
}

pinned_lp="$(dockerfile_value LIGHTPANDA_VERSION)"
try /usr/local/bin/lightpanda version
installed_lp="$(printf '%s' "$TRY_OUT" | tr -d '[:space:]')"
if [ -n "$pinned_lp" ] && [ "$pinned_lp" = "$installed_lp" ]; then
    pass "installed Lightpanda ($installed_lp) matches the Dockerfile pin"
else
    fail "installed Lightpanda matches the Dockerfile pin" \
        "Dockerfile pins '$pinned_lp', container runs '$installed_lp' — rebuild the image"
fi

label_ver="$(awk -F'"' '/^LABEL claude-yolo-version=/ {print $2; exit}' "$DOCKERFILE")"
required_ver="$(awk -F'"' '/^REQUIRED_CONTAINER_VERSION=/ {print $2; exit}' "$LAUNCHER")"
if [ -n "$label_ver" ] && [ "$label_ver" = "$required_ver" ]; then
    pass "Dockerfile label ($label_ver) matches REQUIRED_CONTAINER_VERSION"
else
    fail "Dockerfile label matches REQUIRED_CONTAINER_VERSION" \
        "label='$label_ver' required='$required_ver' — a mismatch causes an endless rebuild loop"
fi
echo

echo "==============================================================================="
echo " VERDICT"
echo
printf '  passed: %d    failed: %d\n' "$PASSES" "$FAILURES"
echo

# The failure path is the conditional one so the script does not END on a bare
# top-level `exit` — shellcheck's reachability pass treats every function in such
# a file as dead code and floods SC2317 (triage.bash is clean for this reason).
if [ "$FAILURES" -ne 0 ]; then
    echo "  REJECTED — failing assertions:$FAILED_LIST"
    echo
    echo " Full log: $LOG"
    echo "==============================================================================="
    exit 1
fi

echo "  ACCEPTED — the shipped image delivers what Plan 00070 promised."
echo
echo " Full log: $LOG"
echo "==============================================================================="
