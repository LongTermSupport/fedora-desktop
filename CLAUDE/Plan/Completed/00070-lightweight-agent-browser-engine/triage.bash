#!/usr/bin/env bash
#
# Plan 00070 triage — establish grounded facts about the browser engines available
# to the CCY container. READ-ONLY with respect to the repo and the deployed system:
# it installs nothing into the image, changes no shipped config, and starts nothing
# that outlives the run. Safe to re-run.
#
# It answers three questions the plan cannot decide without evidence:
#
#   1. What does the Chromium we already ship actually cost (size, startup, RSS)?
#   2. Does `agent-browser --engine lightpanda` work, and what does IT cost?
#   3. How much real-world JavaScript does each engine actually execute?
#
# Question 3 is the decisive one. A lighter engine that cannot run a modern page is
# not a cheaper option, it is a wrong answer — so the JS fidelity matrix below runs
# both engines against the same locally-served fixtures, each of which renders a
# marker string ONLY if a specific JS capability works.
#
# This script renders NO verdict. It reports what is. Picking an engine is the
# plan's Phase 2 decision gate. See CLAUDE/PlanTriage.md.
#
# The Lightpanda binary is fetched to a cache dir OUTSIDE the repo purely so the
# probe can run. That is NOT the install path: if this plan adopts Lightpanda, it
# arrives as a layer in files/var/local/claude-yolo/Dockerfile, per the repo's
# Infrastructure-as-Code rule.

set -euo pipefail

# --- argument parsing FIRST, before any environment resolution (PlanTriage.md) ---

LP_VERSION="0.3.6"
SKIP_DOWNLOAD=0
CONTAINER=""

usage() {
    cat <<'USAGE'
Plan 00070 triage — browser engine facts for the CCY container.

Usage:
  triage.bash [options]

RUN IT FROM THE HOST, like every other plan script in this repo. The browsers it
measures only exist inside the CCY image, so a host run automatically re-executes
this same script inside a running CCY container (the repo is bind-mounted there at
/workspace, so it is the very same file). Running it inside a container also works
and skips the dispatch.

Options:
  --container <name>   Use this CCY container instead of auto-detecting
  --lp-version <ver>   Lightpanda release to probe (default: 0.3.6)
  --no-download        Fail instead of fetching Lightpanda if it is not cached
  -h, --help           Show this help

Output:
  Writes a full report to <plan folder>/logs/browser-engine-triage.log
  (that directory is gitignored — it captures live container state). The plan
  folder is inside the repo, so a report written inside the container lands on
  the host at the same path.

Requires, inside the container: agent-browser, curl, python3, timeout, convert.
Requires, on the host: podman (or docker) with a CCY container running.
USAGE
}

while [ $# -gt 0 ]; do
    case "$1" in
        --lp-version)
            if [ $# -lt 2 ]; then
                echo "ERROR: --lp-version needs a value." >&2
                exit 1
            fi
            LP_VERSION="$2"
            shift 2
            ;;
        --container)
            if [ $# -lt 2 ]; then
                echo "ERROR: --container needs a value." >&2
                exit 1
            fi
            CONTAINER="$2"
            shift 2
            ;;
        --no-download)
            SKIP_DOWNLOAD=1
            shift
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

# --- host dispatch: run from the host, measure inside the container ---------------
#
# Plan scripts in this repo are HOST-run (CLAUDE/Plan/CLAUDE.md), but the browsers
# this one measures exist only inside the CCY image. Rather than refuse — an earlier
# version did, and told the user to go and edit the Dockerfile, which was the wrong
# fix for the wrong problem — a host run re-executes THIS SAME FILE inside a running
# CCY container. The repo is bind-mounted there at /workspace, so it is literally the
# same script, and the report it writes into the plan's logs/ appears on the host at
# the same path.
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
        # CCY containers are named <project>_yolo[_N] and built from a claude-yolo:*
        # image. Prefer one belonging to THIS repo so a multi-project box does not get
        # measured through the wrong container.
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
            echo "ERROR: no running CCY container found — nothing to measure." >&2
            echo "  Start one, then re-run this script from the host:" >&2
            echo "    cd $repo_root && ccy" >&2
            echo >&2
            echo "  If the image predates Plan 00070 (container 2.23), deploy first:" >&2
            echo "    ansible-playbook playbooks/imports/play-claude-yolo.yml" >&2
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
    echo "  (the browsers only exist in there; the report lands in this plan's logs/)" >&2
    exec "$engine" exec "$CONTAINER" "$script_in_container" \
        --lp-version "$LP_VERSION" ${SKIP_DOWNLOAD:+--no-download}
fi

# --- environment ---

REPORTS_DIR="$PLAN_DIR/logs"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/browser-engine-triage.log"
exec > >(tee "$LOG") 2>&1

WORK="$(mktemp -d)"
CACHE="${XDG_CACHE_HOME:-$HOME/.cache}/plan-00070"
mkdir -p "$CACHE"
LP_BIN="$CACHE/lightpanda-$LP_VERSION"
FIXTURES="$WORK/fixtures"
CLEAN_CONFIG="$WORK/clean-config.json"
SERVER_PID=""
PORT=8737

# Close any browser sessions this probe opened. Reports rather than hides a
# failure — a close that fails leaves a daemon holding memory, which would skew
# the very RSS numbers this script exists to measure.
close_sessions() {
    local out
    # The EXIT trap calls this even when the script bails out early, so on a host run
    # it would otherwise append a confusing "agent-browser: command not found" note
    # underneath the real error. Nothing was opened, so there is nothing to close.
    if ! command -v agent-browser > /dev/null; then
        return 0
    fi
    if out="$(agent-browser close --all 2>&1)"; then
        return 0
    fi
    printf '  (note: close --all did not succeed: %s)\n' \
        "$(printf '%s' "$out" | tr '\n' ' ' | cut -c1-120)"
}

cleanup() {
    if [ -n "$SERVER_PID" ] && ps -p "$SERVER_PID" > /dev/null; then
        kill "$SERVER_PID"
    fi
    close_sessions
    rm -rf "$WORK"
}
trap cleanup EXIT

# Capture a command's output and exit status as DATA. A non-zero status is a
# finding, not a failure — never let it abort the run.
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

echo "==============================================================================="
echo " Plan 00070 — browser engine triage"
echo " host: $(uname -srm)   date: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
echo "==============================================================================="
echo

# --- dependency gate: a missing tool is an IaC gap, never a skip ---------------
#
# Reaching here means we ARE inside a container (a host run dispatched above), so a
# missing tool really is a gap in the image, and the Dockerfile really is the fix.

for tool in agent-browser curl python3 timeout convert; do
    if ! command -v "$tool" > /dev/null; then
        echo "ERROR: '$tool' is not installed in this container." >&2
        echo "  The CCY image is built by files/var/local/claude-yolo/Dockerfile." >&2
        echo "  Add it there and rebuild — do NOT install it by hand." >&2
        exit 1
    fi
done

echo "## 0. Environment"
echo
probe "agent-browser version" agent-browser --version
probe "node version" node --version
probe "glibc version" getconf GNU_LIBC_VERSION
echo "WAYLAND_DISPLAY=${WAYLAND_DISPLAY:-<unset>}  DISPLAY=${DISPLAY:-<unset>}"
echo

# --- 1. what we already ship ---------------------------------------------------

echo "## 1. Chromium (the engine CCY ships today)"
echo
echo "### shipped agent-browser config  (/root/.agent-browser/config.json)"
if [ -f /root/.agent-browser/config.json ]; then
    cat /root/.agent-browser/config.json
else
    echo "(absent)"
fi
echo
probe "browsers dir size" du -sh /root/.agent-browser/browsers
probe "npm package size" du -sh /usr/local/lib/node_modules/agent-browser
echo

# --- 2. lightpanda availability -------------------------------------------------

echo "## 2. Lightpanda binary"
echo
echo "### READ THIS FOR: whether agent-browser's built-in --engine lightpanda"
echo "###   can be used at all in this image, and what blocks it."
echo
echo "### does this agent-browser build advertise an alternative engine?"
if agent-browser --help 2>&1 | grep -- "--engine"; then
    echo "  ^ alternative engine support IS present in this build."
else
    echo "  NOT PRESENT — this agent-browser build has no alternative engine support."
fi
echo

echo "### what the SHIPPED config does to the lightpanda path"
probe "lightpanda under the shipped config (expect a rejection)" \
    agent-browser --engine lightpanda read https://example.com
echo

if [ ! -x "$LP_BIN" ]; then
    if [ "$SKIP_DOWNLOAD" -eq 1 ]; then
        echo "ERROR: Lightpanda $LP_VERSION not cached at $LP_BIN and --no-download was given." >&2
        exit 1
    fi
    echo "### fetching Lightpanda $LP_VERSION to the probe cache (not an install)"
    url="https://github.com/lightpanda-io/browser/releases/download/${LP_VERSION}/lightpanda-x86_64-linux"
    if curl -fsSL -o "$LP_BIN.part" "$url"; then
        chmod 755 "$LP_BIN.part"
        mv "$LP_BIN.part" "$LP_BIN"
        echo "  fetched: $url"
    else
        rm -f "$LP_BIN.part"
        echo "ERROR: could not fetch $url — no Lightpanda facts can be established." >&2
        exit 1
    fi
    echo
fi

probe "lightpanda binary size" ls -la "$LP_BIN"
probe "lightpanda runs on this glibc" "$LP_BIN" version
echo

# --- 3. cost comparison ---------------------------------------------------------

# Build the per-engine agent-browser invocation prefix.
#
# CRITICAL, learned the hard way: `agent-browser read <url>` does NOT render the
# page. Its own --help says it "fetches a URL as agent-readable text" — an HTTP
# GET plus HTML-to-text extraction, with no browser involved. A first version of
# this script used it and produced a fidelity table in which CHROMIUM failed every
# JavaScript test, which is obviously false and was the tell that the harness, not
# the engines, was broken. The rendering path is `open <url>` followed by an
# extraction command (`get text`, or a bare `read` with no URL, which does read the
# active tab's rendered DOM). Everything below goes through `open`.
engine_prefix() {
    local engine="$1"
    if [ "$engine" = "lightpanda" ]; then
        printf '%s\n' --config "$CLEAN_CONFIG" --engine lightpanda \
            --executable-path "$LP_BIN" --session t70lp
    else
        printf '%s\n' --config "$CLEAN_CONFIG" --headed false --session t70ch
    fi
}

# Time a cold open + rendered-text extraction for one engine and report wall time,
# process count and summed RSS. Summed RSS DOUBLE-COUNTS shared pages across a
# multi-process browser, so it is an upper bound — stated so it is not over-read.
#
# Sample memory WHILE the command runs, not after it. A first version sampled once
# on return and reported Lightpanda at "0 kB / 0 processes" — its process had already
# exited by then. That is precisely the misleading-empty-result failure PlanTriage.md
# warns about: it reads as a measurement when it is an artefact of sampling too late.
sample_peak() {
    local pattern="$1" flag="$2" outfile="$3"
    local peak=0 peakproc=0 cur curproc
    while [ -f "$flag" ]; do
        cur="$(ps -eo rss,comm --no-headers | awk -v p="$pattern" '$2 ~ p {s+=$1} END {print s+0}')"
        curproc="$(ps -eo comm --no-headers | awk -v p="$pattern" '$1 ~ p {n++} END {print n+0}')"
        if [ "$cur" -gt "$peak" ]; then peak="$cur"; fi
        if [ "$curproc" -gt "$peakproc" ]; then peakproc="$curproc"; fi
        sleep 0.1
    done
    # Trailing newline is required: `read` returns non-zero at EOF without one,
    # which under `set -e` aborts the whole run mid-measurement.
    printf '%s %s\n' "$peak" "$peakproc" > "$outfile"
}

#
# NOTE the argument order: the ENGINE KEY comes first and must be exactly
# "chromium" or "lightpanda", because engine_prefix dispatches on it with a
# case-sensitive string compare. A first version passed the display label
# ("LIGHTPANDA") as the key, which silently fell through to the chromium branch —
# so the row labelled Lightpanda was measuring Chromium, and the only reason it was
# caught is that the RSS came back as 0 for a pattern no chrome process matches.
# Display text is a separate argument for exactly this reason.
measure_engine() {
    local engine="$1" label="$2" url="$3" pattern="$4"
    local start end rc out procs rss flag outfile sampler
    local -a pre
    mapfile -t pre < <(engine_prefix "$engine")

    close_sessions
    sleep 1

    flag="$WORK/sampling.$engine"
    outfile="$WORK/peak.$engine"
    touch "$flag"
    sample_peak "$pattern" "$flag" "$outfile" &
    sampler=$!

    start=$(date +%s%3N)
    if out="$(timeout 90 agent-browser "${pre[@]}" open "$url" 2>&1 \
        && timeout 90 agent-browser "${pre[@]}" get text body 2>&1)"; then
        rc=0
    else
        rc=$?
    fi
    end=$(date +%s%3N)

    rm -f "$flag"
    wait "$sampler"
    read -r rss procs < "$outfile"

    if [ "$rss" -eq 0 ]; then
        printf 'ERROR: measured 0 kB for engine key "%s" (pattern "%s").\n' "$engine" "$pattern" >&2
        printf '  No process matched, so this engine did not run — refusing to report a\n' >&2
        printf '  zero as if it were a measurement. Check the engine key spelling.\n' >&2
        exit 1
    fi

    printf '### %s — cold open + rendered text of %s  (rc=%d)\n' "$label" "$url" "$rc"
    printf '    wall time      : %d ms\n' "$((end - start))"
    printf '    peak processes : %s\n' "$procs"
    printf '    peak summed RSS: %s kB (~%s MB, UPPER BOUND — shared pages double-counted)\n' \
        "$rss" "$((rss / 1024))"
    printf '    output:\n%s\n\n' "$out"
}

echo "## 3. Cost of one page fetch, engine vs engine"
echo
echo "### READ THIS FOR: the size of the prize. Same page, same CLI, same output"
echo "###   format — only the engine differs."
echo

echo '{}' > "$CLEAN_CONFIG"

measure_engine chromium "CHROMIUM (headless)" "https://example.com" "chrome"
measure_engine lightpanda "LIGHTPANDA" "https://example.com" "lightpanda"

# --- 4. JS fidelity matrix -------------------------------------------------------

echo "## 4. JavaScript fidelity matrix"
echo
echo "### READ THIS FOR: THE DECIDING EVIDENCE. Each fixture renders its marker"
echo "###   ONLY if that JS capability works. A missing marker = that capability is"
echo "###   broken in that engine. Footprint is irrelevant if this table is bad."
echo

mkdir -p "$FIXTURES"

cat > "$FIXTURES/static.html" <<'HTML'
<!doctype html><meta charset=utf-8><title>static</title>
<body><p>MARKER-STATIC</p></body>
HTML

cat > "$FIXTURES/dom.html" <<'HTML'
<!doctype html><meta charset=utf-8><title>dom</title>
<body><p id=t>nothing</p>
<script>document.getElementById('t').textContent = 'MARKER-DOM';</script>
</body>
HTML

cat > "$FIXTURES/defer.html" <<'HTML'
<!doctype html><meta charset=utf-8><title>defer</title>
<body><p id=t>nothing</p>
<script>
document.addEventListener('DOMContentLoaded', function () {
  document.getElementById('t').textContent = 'MARKER-DEFER';
});
</script>
</body>
HTML

cat > "$FIXTURES/timeout.html" <<'HTML'
<!doctype html><meta charset=utf-8><title>timeout</title>
<body><p id=t>nothing</p>
<script>setTimeout(function () {
  document.getElementById('t').textContent = 'MARKER-TIMEOUT';
}, 300);</script>
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

cat > "$FIXTURES/modern.html" <<'HTML'
<!doctype html><meta charset=utf-8><title>modern</title>
<body><p id=t>nothing</p>
<script type="module">
class Box { #v = 'MARKER'; get v() { return this.#v; } }
const b = new Box();
const parts = [b.v, 'MODERN'];
const label = parts?.join?.('-') ?? 'broken';
document.querySelector('#t').textContent = label;
</script>
</body>
HTML

cat > "$FIXTURES/shadow.html" <<'HTML'
<!doctype html><meta charset=utf-8><title>shadow</title>
<body><my-el></my-el>
<script>
customElements.define('my-el', class extends HTMLElement {
  connectedCallback() {
    this.attachShadow({mode: 'open'}).innerHTML = '<p>MARKER-SHADOW</p>';
  }
});
</script>
</body>
HTML

cat > "$FIXTURES/react.html" <<'HTML'
<!doctype html><meta charset=utf-8><title>react</title>
<body><div id=root>nothing</div>
<script crossorigin src="https://unpkg.com/react@18/umd/react.production.min.js"></script>
<script crossorigin src="https://unpkg.com/react-dom@18/umd/react-dom.production.min.js"></script>
<script>
window.addEventListener('load', function () {
  var e = React.createElement;
  ReactDOM.createRoot(document.getElementById('root')).render(e('p', null, 'MARKER-REACT'));
});
</script>
</body>
HTML

# Flat, unmistakable colours so a screenshot can be checked by histogram rather than
# by eye — see check_screenshot_truthful().
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
    echo "  Without it the fidelity matrix would print an all-empty table, which" >&2
    echo "  reads as 'both engines are broken' — refusing to emit that." >&2
    cat "$WORK/http.log" >&2
    exit 1
fi

# Open one fixture in one engine, let async work settle, then read the RENDERED
# DOM and report whether the marker appeared. Both engines get the same 1s grace
# so the setTimeout fixture is a fair test rather than a race.
check_marker() {
    local engine="$1" fixture="$2" marker="$3"
    local out rc verdict
    local -a pre
    mapfile -t pre < <(engine_prefix "$engine")

    if out="$(timeout 90 agent-browser "${pre[@]}" open "http://127.0.0.1:$PORT/$fixture" 2>&1 \
        && timeout 90 agent-browser "${pre[@]}" wait 1000 2>&1 \
        && timeout 90 agent-browser "${pre[@]}" get text body 2>&1)"; then
        rc=0
    else
        rc=$?
    fi

    if printf '%s' "$out" | grep -q "$marker"; then
        verdict="YES"
    else
        verdict="no"
    fi
    printf '  %-11s %-14s %-16s marker=%-4s rc=%d\n' "$engine" "$fixture" "$marker" "$verdict" "$rc"
    if [ "$verdict" = "no" ]; then
        printf '      got: %s\n' "$(printf '%s' "$out" | tr '\n' ' ' | cut -c1-160)"
    fi
    close_sessions
}

FIXTURE_SPECS=(
    "static.html:MARKER-STATIC:plain HTML, no JS needed (control)"
    "dom.html:MARKER-DOM:synchronous inline DOM write"
    "defer.html:MARKER-DEFER:DOMContentLoaded listener"
    "timeout.html:MARKER-TIMEOUT:setTimeout 300ms"
    "fetch.html:MARKER-FETCH:fetch() + JSON + render"
    "modern.html:MARKER-MODERN:ES module, private field, optional chaining"
    "shadow.html:MARKER-SHADOW:custom element + shadow DOM"
    "react.html:MARKER-REACT:React 18 UMD from CDN, client render"
)

for spec in "${FIXTURE_SPECS[@]}"; do
    fixture="${spec%%:*}"
    rest="${spec#*:}"
    marker="${rest%%:*}"
    desc="${rest#*:}"
    echo "### $fixture — $desc"
    check_marker chromium "$fixture" "$marker"
    check_marker lightpanda "$fixture" "$marker"
    echo
done

# --- 5. real-world sample --------------------------------------------------------

echo "## 5. Real-world pages"
echo
echo "### READ THIS FOR: whether fixture results survive contact with real sites."
echo "###   Output length is a proxy for how much of the page actually rendered."
echo

sample_site() {
    local engine="$1" url="$2"
    local out rc chars
    local -a pre
    mapfile -t pre < <(engine_prefix "$engine")

    if out="$(timeout 90 agent-browser "${pre[@]}" open "$url" 2>&1 \
        && timeout 90 agent-browser "${pre[@]}" wait 1500 2>&1 \
        && timeout 90 agent-browser "${pre[@]}" get text body 2>&1)"; then
        rc=0
    else
        rc=$?
    fi
    chars="$(printf '%s' "$out" | wc -c)"
    printf '  %-11s %-46s rc=%d  chars=%s\n' "$engine" "$url" "$rc" "$chars"
    if [ "$rc" -ne 0 ] || [ "$chars" -lt 200 ]; then
        printf '      got: %s\n' "$(printf '%s' "$out" | tr '\n' ' ' | cut -c1-200)"
    fi
    close_sessions
}

for site in \
    "https://example.com" \
    "https://news.ycombinator.com" \
    "https://en.wikipedia.org/wiki/Web_browser_engine" \
    "https://react.dev"; do
    echo "### $site"
    sample_site chromium "$site"
    sample_site lightpanda "$site"
    echo
done

# --- 6. capability boundaries ----------------------------------------------------

echo "## 6. Capability boundaries"
echo
echo "### READ THIS FOR: what the light engine CANNOT do. The scan research describes"
echo "###   Lightpanda as DOM-only with no layout/render pipeline. If true, anything"
echo "###   that needs pixels (screenshot, pdf) or box geometry should fail, while"
echo "###   DOM-level work (snapshot refs, clicking, eval) should still work. This"
echo "###   section is what the agent-facing decision rule gets written from."
echo

capability() {
    local engine="$1" label="$2"
    shift 2
    local out rc
    local -a pre
    mapfile -t pre < <(engine_prefix "$engine")

    if out="$(timeout 90 agent-browser "${pre[@]}" "$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '  %-11s %-22s rc=%-3d %s\n' "$engine" "$label" "$rc" \
        "$(printf '%s' "$out" | tr '\n' ' ' | cut -c1-110)"
}

for engine in chromium lightpanda; do
    echo "### $engine"
    close_sessions
    capability "$engine" "open fixture" open "http://127.0.0.1:$PORT/dom.html"
    capability "$engine" "eval JS" eval "document.title"
    capability "$engine" "get text" get text body
    capability "$engine" "get html" get html body
    capability "$engine" "snapshot (a11y tree)" snapshot -i
    capability "$engine" "get box (geometry)" get box body
    capability "$engine" "screenshot (pixels)" screenshot "$WORK/shot-$engine.png"
    capability "$engine" "pdf (paged layout)" pdf "$WORK/out-$engine.pdf"
    if [ -f "$WORK/shot-$engine.png" ]; then
        printf '  %-11s %-22s produced %s bytes\n' "$engine" "screenshot file" \
            "$(wc -c < "$WORK/shot-$engine.png")"
    else
        printf '  %-11s %-22s NO FILE PRODUCED\n' "$engine" "screenshot file"
    fi
    echo
done
close_sessions

# A screenshot that exits 0 and writes a PNG has NOT necessarily captured the page.
# Verify the PIXELS: vis.html paints three unmistakable flat colours, so the render is
# truthful only if those exact colours appear in the image. This check exists because
# `screenshot` on a DOM-only engine was observed returning rc=0 with a cheerful
# "✓ Screenshot saved" while writing a placeholder image instead of the page — a
# failure invisible to any agent that trusts the exit status.
echo "### screenshot CORRECTNESS (does the PNG actually depict the page?)"
echo "###   vis.html paints #CC0000 heading, #006600 text, #0000CC box. All three"
echo "###   must appear in a truthful render. rc=0 alone proves nothing here."
echo

check_screenshot_truthful() {
    local engine="$1"
    local shot hist found colour dims ncolours out
    local -a pre
    mapfile -t pre < <(engine_prefix "$engine")
    shot="$WORK/vis-$engine.png"

    close_sessions
    if ! out="$(timeout 90 agent-browser "${pre[@]}" open "http://127.0.0.1:$PORT/vis.html" 2>&1)"; then
        printf '  %-11s open failed: %s\n' "$engine" \
            "$(printf '%s' "$out" | tr '\n' ' ' | cut -c1-100)"
        return 0
    fi
    if ! out="$(timeout 90 agent-browser "${pre[@]}" screenshot "$shot" 2>&1)"; then
        printf '  %-11s screenshot failed: %s\n' "$engine" \
            "$(printf '%s' "$out" | tr '\n' ' ' | cut -c1-100)"
        return 0
    fi

    printf '  %-11s screenshot reported: %s\n' "$engine" \
        "$(printf '%s' "$out" | tr '\n' ' ' | cut -c1-70)"

    if [ ! -f "$shot" ]; then
        printf '  %-11s NO PNG WRITTEN despite success being reported\n' "$engine"
        return 0
    fi

    hist="$(convert "$shot" -format %c -depth 8 histogram:info:-)"
    dims="$(identify -format '%wx%h' "$shot")"
    ncolours="$(printf '%s\n' "$hist" | wc -l)"
    found=0
    for colour in CC0000 006600 0000CC; do
        if printf '%s' "$hist" | grep -qi "#$colour"; then
            found=$((found + 1))
        fi
    done

    printf '  %-11s %s  page colours present: %d/3  distinct colours: %s\n' \
        "$engine" "$dims" "$found" "$ncolours"
    if [ "$found" -lt 3 ]; then
        printf '      ^^ NOT A TRUTHFUL RENDER despite rc=0. Dominant colours: %s\n' \
            "$(printf '%s\n' "$hist" | sort -rn | head -3 | tr -s ' ' | tr '\n' ' ')"
    fi
}

check_screenshot_truthful chromium
check_screenshot_truthful lightpanda
echo
close_sessions

# --- 7. deployed integration ------------------------------------------------------

echo "## 7. Deployed integration (Plan 00070 Phase 3)"
echo
echo "### READ THIS FOR: whether the IMAGE actually ships Lightpanda. Everything above"
echo "###   drives a binary from a probe cache via --executable-path, which proves the"
echo "###   engine works but says nothing about whether the Dockerfile installed it."
echo "###   This section uses NO overrides — exactly what an agent would type."
echo

deployed_check() {
    local label="$1" path="$2"
    if [ -e "$path" ]; then
        printf '  %-28s PRESENT  %s\n' "$label" "$path"
    else
        printf '  %-28s ABSENT   %s\n' "$label" "$path"
    fi
}

deployed_check "lightpanda binary" /usr/local/bin/lightpanda
deployed_check "lightpanda config" /root/.agent-browser/lightpanda.json
deployed_check "agent-browser-lite wrapper" /usr/local/bin/agent-browser-lite
echo

if command -v agent-browser-lite > /dev/null; then
    probe "agent-browser-lite, no overrides: open fixture" \
        agent-browser-lite open "http://127.0.0.1:$PORT/dom.html"
    probe "agent-browser-lite, no overrides: rendered text (expect MARKER-DOM)" \
        agent-browser-lite get text body
    close_sessions
    probe "installed lightpanda version" /usr/local/bin/lightpanda version
else
    echo "  agent-browser-lite is NOT on PATH — this image predates Plan 00070's"
    echo "  Dockerfile change (container 2.23). Deploy on the HOST and start a fresh"
    echo "  ccy session, which rebuilds the image:"
    echo "    ansible-playbook playbooks/imports/play-claude-yolo.yml"
    echo
    echo "  Sections 1-6 above are still valid — they drive the engine binary directly."
fi
echo

echo "==============================================================================="
echo " END OF REPORT"
echo
echo " READ FIRST: section 4 (JS fidelity matrix). It is the deciding evidence."
echo " Section 3 sizes the prize; section 4 says whether the prize is real;"
echo " section 6 says where the cheap engine stops and Chromium must take over;"
echo " section 7 says whether the image actually ships it yet."
echo " No verdict is rendered here — see PLAN.md Phase 2 for the decision gate."
echo " Full log: $LOG"
echo "==============================================================================="
