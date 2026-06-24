#!/usr/bin/env bash
# L2 host integration acceptance test for the container-process watchdog (Plan 00055).
#
# HOST-ONLY — this script starts REAL throwaway containers and a session bus. It
# CANNOT run inside the CCY container (/workspace); run it on the Fedora HOST. See
# CLAUDE/Plan/00055-container-process-watchdog/testing.md section 4.
#
# What it proves, per PRESENT engine (podman always; docker/lxc gated on presence):
#   - a real CPU-pinned container process is detected and attributed correctly
#     (engine, container_name, in-container PID, cmd, age, cpu, exec_hint);
#   - the BEHAVIOURAL SAFETY guarantee: the spinner is STILL ALIVE after the scan
#     (the tool is reporting-only and must never kill a finding);
#   - the allowlist suppresses a known container end-to-end (zero findings);
#   - the FindingsChanged DBus signal fires on the session bus;
#   - the systemd user units pass `systemd-analyze --user verify`.
#
# Thresholds are overridden (CW_AGE_S=1 CW_CPU_PCT=5) so NOTHING waits 15 minutes.
# A burner self-caps via `timeout`; an EXIT trap force-removes every cw-test-*
# container even on failure, so two runs leave no strays (idempotent).
#
# Prints a PASS/FAIL line per assertion and exits non-zero on the first FAIL.
#
# Fail-fast / no error-hiding: this script never silences a command with the
# error-suppression idioms this repo forbids. Probe output is captured with 2>&1
# into a variable and shown on failure; presence is tested with command -v.

set -euo pipefail

# --------------------------------------------------------------------------- #
# Thresholds — low so a few-second burner trips both gates immediately.
# --------------------------------------------------------------------------- #
export CW_AGE_S=1
export CW_CPU_PCT=5

# Burner lifetime: long enough to be sampled across a scan + the post-scan
# liveness assert, short enough to self-clean if the trap is ever bypassed.
BURNER_TTL_S=30
# Small, ubiquitous image with a POSIX shell.
BURNER_IMAGE="${CW_TEST_IMAGE:-docker.io/library/alpine:latest}"
# Two background spinners + wait → reads as CPU-pinned (>1 core) within seconds.
BURNER_CMD='timeout '"$BURNER_TTL_S"' sh -c "while :; do :; done & while :; do :; done & wait"'

FAILED=0

# --------------------------------------------------------------------------- #
# Output helpers (plain text when not a TTY).
# --------------------------------------------------------------------------- #
if [ -t 1 ]; then
    C_PASS=$'\033[32m'; C_FAIL=$'\033[31m'; C_HDR=$'\033[1m'; C_OFF=$'\033[0m'
else
    C_PASS=''; C_FAIL=''; C_HDR=''; C_OFF=''
fi

hdr()  { echo; echo "${C_HDR}=== $* ===${C_OFF}"; }
pass() { echo "  ${C_PASS}PASS${C_OFF}: $*"; }
# fail records the failure and returns non-zero so callers can react, but does
# NOT exit — every assertion's verdict prints for a present engine, then a
# non-zero overall exit at the end.
fail() { echo "  ${C_FAIL}FAIL${C_OFF}: $*"; FAILED=1; return 1; }
skip() { echo "  SKIP: $*"; }

# assert_eq <label> <expected> <actual>
assert_eq() {
    local label="$1" expected="$2" actual="$3"
    if [ "$expected" = "$actual" ]; then
        pass "$label (= $actual)"
    else
        fail "$label: expected [$expected], got [$actual]"
    fi
}

# assert_ge <label> <value> <floor>  (integer >=)
assert_ge() {
    local label="$1" value="$2" floor="$3"
    if [[ "$value" =~ ^-?[0-9]+$ ]] && [ "$value" -ge "$floor" ]; then
        pass "$label ($value >= $floor)"
    else
        fail "$label: expected integer >= $floor, got [$value]"
    fi
}

# assert_contains <label> <haystack> <needle>
assert_contains() {
    local label="$1" haystack="$2" needle="$3"
    case "$haystack" in
        *"$needle"*) pass "$label (contains '$needle')" ;;
        *)           fail "$label: '$needle' not found in [$haystack]" ;;
    esac
}

# --------------------------------------------------------------------------- #
# Dependency + CLI resolution (fail-fast on a missing required tool — IaC gap).
# --------------------------------------------------------------------------- #
need() {
    if ! command -v "$1" >/dev/null; then
        echo "MISSING REQUIRED TOOL: $1 — install it via the relevant Ansible play and re-run." >&2
        exit 2
    fi
}
need jq
need timeout

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Resolve the CLI robustly: prefer the deployed wrapper on PATH; else the module
# form. CW[] is the argv prefix every invocation reuses.
declare -a CW
if command -v container-watch >/dev/null; then
    CW=(container-watch)
    echo "Using CLI: container-watch (on PATH)"
elif command -v python3 >/dev/null && [ -d "$REPO_ROOT/helpers/containerwatch" ]; then
    CW=(env "PYTHONPATH=$REPO_ROOT" python3 -m helpers.containerwatch.cli)
    echo "Using CLI: python3 -m helpers.containerwatch.cli (PYTHONPATH=$REPO_ROOT)"
else
    echo "MISSING REQUIRED TOOL: container-watch CLI (no wrapper on PATH and no repo module) — deploy play-container-watch.yml." >&2
    exit 2
fi

cw() { "${CW[@]}" "$@"; }

# A best-effort container remove that never aborts the surrounding flow but also
# never silently hides a real error: it captures combined output and prints it
# only when the remove actually failed. Used by cleanup + per-engine teardown.
# Usage: best_effort_rm <description> <command...>
best_effort_rm() {
    local desc="$1"; shift
    local out
    if out="$("$@" 2>&1)"; then
        return 0
    fi
    echo "  note: $desc reported: ${out:-<no output>}" >&2
    return 0
}

# --------------------------------------------------------------------------- #
# Cleanup — idempotent, runs on every exit (success OR failure). Force-removes
# every cw-test-* container under each present engine, plus the throwaway config
# dir. Errors are surfaced (not hidden) but never abort cleanup.
# --------------------------------------------------------------------------- #
TMP_CONFIG_DIR="$(mktemp -d -t cw-acceptance-config.XXXXXX)"

# shellcheck disable=SC2317  # body reached only via the EXIT trap, not inline
cleanup() {
    local ids out
    if command -v podman >/dev/null; then
        if ids="$(podman ps -aq --filter 'name=cw-test-' 2>&1)" && [ -n "$ids" ]; then
            # shellcheck disable=SC2086  # intentional word-split of the id list
            best_effort_rm "podman rm -f" podman rm -f $ids
        fi
    fi
    if command -v docker >/dev/null; then
        if ids="$(docker ps -aq --filter 'name=cw-test-' 2>&1)" && [ -n "$ids" ]; then
            # shellcheck disable=SC2086
            best_effort_rm "docker rm -f" docker rm -f $ids
        fi
    fi
    if command -v lxc-ls >/dev/null; then
        if out="$(sudo lxc-ls -1 2>&1)"; then
            while IFS= read -r name; do
                case "$name" in
                    cw-test-*) best_effort_rm "lxc-destroy $name" sudo lxc-destroy -f -n "$name" ;;
                esac
            done <<< "$out"
        fi
    fi
    rm -rf "$TMP_CONFIG_DIR"
}
trap cleanup EXIT

# --------------------------------------------------------------------------- #
# Engine-presence probes (legitimate gating — an absent engine is SKIPPED, not
# faked). Each returns 0 only when the engine can actually start a container.
# Probe output is captured (not hidden); shown only when a probe is inconclusive.
# --------------------------------------------------------------------------- #
have_podman() {
    command -v podman >/dev/null || return 1
    local out
    if out="$(podman info 2>&1)"; then return 0; fi
    echo "  (podman present but 'podman info' failed: $(printf '%s' "$out" | head -n1))" >&2
    return 1
}

have_docker() {
    command -v docker >/dev/null || return 1
    local out
    if out="$(docker info 2>&1)"; then return 0; fi
    echo "  (docker present but 'docker info' failed — daemon down or not in docker group: $(printf '%s' "$out" | head -n1))" >&2
    return 1
}

have_lxc() {
    command -v lxc-create >/dev/null || return 1
    command -v lxc-attach >/dev/null || return 1
    command -v lxc-info   >/dev/null || return 1
    local out
    # LXC is rootful here — needs non-interactive sudo to drive it.
    if out="$(sudo -n id 2>&1)"; then return 0; fi
    echo "  (lxc tools present but non-interactive sudo unavailable: $(printf '%s' "$out" | head -n1))" >&2
    return 1
}

# --------------------------------------------------------------------------- #
# scan helper: run `scan --once --json`, capture stdout, fail-fast on a crash.
# --------------------------------------------------------------------------- #
scan_json() {
    local out
    if ! out="$(cw scan --once --json 2>&1)"; then
        echo "  scan crashed:" >&2
        echo "$out" >&2
        return 1
    fi
    printf '%s' "$out"
}

# =========================================================================== #
# Per-engine block — parametrised over the engine name + its container runner.
# Args: $1 engine ("podman"|"docker").
# LXC differs enough (no `run -d` image model) that it gets its own block below.
# =========================================================================== #
run_oci_engine_block() {
    local engine="$1"
    local burner="cw-test-burner"
    local json gt_pids gt_in_pid finding count out

    hdr "$engine — start burner + assert detection"

    # 1. Start the throwaway CPU-burner.
    if ! out="$("$engine" run -d --rm --name "$burner" "$BURNER_IMAGE" sh -c "$BURNER_CMD" 2>&1)"; then
        fail "$engine: could not start burner '$burner' from image $BURNER_IMAGE: $out"
        return 0
    fi
    pass "$engine: started burner '$burner'"

    # Give the spinners a moment so CPU sampling reads them as hot, and age >= 1.
    sleep 2

    # 2. Ground truth: the in-container PID(s) of the spinner (`sh` busy-loop).
    #    pgrep inside the container returns container-namespace PIDs.
    if ! gt_pids="$("$engine" exec "$burner" pgrep -f 'while' 2>&1)"; then
        fail "$engine: could not read ground-truth in-container PID (pgrep): $gt_pids"
        return 0
    fi
    gt_in_pid="$(printf '%s\n' "$gt_pids" | head -n1)"
    pass "$engine: ground-truth in-container spinner PID(s): $(printf '%s' "$gt_pids" | tr '\n' ' ') (lowest $gt_in_pid)"

    # 3. Scan.
    if ! json="$(scan_json)"; then
        fail "$engine: scan --once --json failed"
        return 0
    fi

    # 4. Assertions via jq.
    count="$(printf '%s' "$json" | jq --arg n "$burner" '[.findings[] | select(.container_name == $n)] | length')"
    assert_eq "$engine: exactly one finding for $burner" "1" "$count"

    if [ "$count" != "1" ]; then
        echo "  (report findings dump for triage):" >&2
        printf '%s' "$json" | jq '.findings' >&2
        return 0
    fi

    finding="$(printf '%s' "$json" | jq -c --arg n "$burner" '.findings[] | select(.container_name == $n)')"

    assert_eq "$engine: engine field"   "$engine" "$(printf '%s' "$finding" | jq -r '.engine')"
    assert_eq "$engine: container_name" "$burner"  "$(printf '%s' "$finding" | jq -r '.container_name')"

    # container_pid must equal one of the ground-truth in-container spinner PIDs.
    local cpid
    cpid="$(printf '%s' "$finding" | jq -r '.container_pid')"
    if printf '%s\n' "$gt_pids" | grep -qx "$cpid"; then
        pass "$engine: container_pid ($cpid) matches a ground-truth in-container PID"
    else
        fail "$engine: container_pid ($cpid) not in ground-truth set [$(printf '%s' "$gt_pids" | tr '\n' ' ')]"
    fi

    assert_contains "$engine: cmd contains the spinner" "$(printf '%s' "$finding" | jq -r '.cmd')" "while"
    assert_ge       "$engine: age_s >= 1"               "$(printf '%s' "$finding" | jq -r '.age_s')" "1"

    # cpu_pct should read high (multi-spinner). Floor at the test CPU threshold (5).
    assert_ge       "$engine: cpu_pct high"             "$(printf '%s' "$finding" | jq -r '.cpu_pct | floor')" "5"

    # exec_hint must be engine-correct AND name the container.
    local hint
    hint="$(printf '%s' "$finding" | jq -r '.exec_hint')"
    case "$engine" in
        podman) assert_contains "$engine: exec_hint is engine-correct" "$hint" "podman exec" ;;
        docker) assert_contains "$engine: exec_hint is engine-correct" "$hint" "docker exec" ;;
    esac
    assert_contains "$engine: exec_hint names the container" "$hint" "$burner"

    # 5. BEHAVIOURAL SAFETY — the spinner MUST still be alive after the scan.
    #    Re-pgrep inside the container; a reporting-only tool never kills it.
    hdr "$engine — SAFETY: burner survives the scan (reporting-only)"
    local still
    if still="$("$engine" exec "$burner" pgrep -f 'while' 2>&1)" && [ -n "$still" ]; then
        pass "$engine: spinner STILL ALIVE after scan — tool did not terminate it"
    else
        fail "$engine: spinner is GONE after scan — a reporting-only tool must NOT terminate findings! ($still)"
    fi

    # 6. Allowlist suppression — add the burner to a temp allowlist, re-scan → 0.
    hdr "$engine — allowlist suppresses $burner end-to-end"
    mkdir -p "$TMP_CONFIG_DIR/container-watch"
    printf '{"allowlist": [{"container_name": "%s"}]}\n' "$burner" \
        > "$TMP_CONFIG_DIR/container-watch/config.json"
    local sup_json sup_count
    if sup_json="$(XDG_CONFIG_HOME="$TMP_CONFIG_DIR" cw scan --once --json 2>&1)"; then
        sup_count="$(printf '%s' "$sup_json" | jq --arg n "$burner" \
            '[.findings[] | select(.container_name == $n)] | length')"
        assert_eq "$engine: allowlisted $burner suppressed" "0" "$sup_count"
    else
        fail "$engine: allowlisted scan crashed: $sup_json"
    fi
    # Remove the temp config so it does not leak into later engine blocks.
    rm -f "$TMP_CONFIG_DIR/container-watch/config.json"

    # 7. Clean the burner now (the trap is the safety net; this keeps runs tidy).
    best_effort_rm "$engine rm -f $burner" "$engine" rm -f "$burner"
    pass "$engine: burner removed"
}

# =========================================================================== #
# LXC block — system container; different lifecycle to the OCI engines.
# =========================================================================== #
run_lxc_block() {
    local engine="lxc"
    local burner="cw-test-burner-lxc"
    local json finding count gt_pids gt_in_pid out

    hdr "$engine — start burner + assert detection"

    # Minimal system container via the portable download template. If the
    # template/network is unavailable this is a legitimate environmental SKIP
    # (not a faked pass) — we cannot exercise lxc without an image.
    if ! out="$(sudo lxc-create -n "$burner" -t download -- \
            --dist alpine --release edge --arch amd64 2>&1)"; then
        skip "$engine: could not create test container (template/network unavailable): $(printf '%s' "$out" | tail -n1)"
        return 0
    fi
    if ! out="$(sudo lxc-start -n "$burner" 2>&1)"; then
        fail "$engine: could not start container $burner: $out"
        return 0
    fi
    pass "$engine: started container '$burner'"

    # Launch the bounded spinner inside the container (detached).
    if ! out="$(sudo lxc-attach -n "$burner" -- sh -c \
            "timeout $BURNER_TTL_S sh -c 'while :; do :; done & while :; do :; done & wait' &" 2>&1)"; then
        echo "  note: lxc-attach spinner launch reported: $out" >&2
    fi
    sleep 2

    if ! gt_pids="$(sudo lxc-attach -n "$burner" -- pgrep -f 'while' 2>&1)"; then
        fail "$engine: could not read ground-truth in-container PID: $gt_pids"
        return 0
    fi
    gt_in_pid="$(printf '%s\n' "$gt_pids" | head -n1)"
    pass "$engine: ground-truth in-container spinner PID(s): $(printf '%s' "$gt_pids" | tr '\n' ' ') (lowest $gt_in_pid)"

    if ! json="$(scan_json)"; then
        fail "$engine: scan --once --json failed"
        return 0
    fi

    count="$(printf '%s' "$json" | jq --arg n "$burner" '[.findings[] | select(.container_name == $n)] | length')"
    assert_eq "$engine: exactly one finding for $burner" "1" "$count"
    if [ "$count" != "1" ]; then
        printf '%s' "$json" | jq '.findings' >&2
        return 0
    fi

    finding="$(printf '%s' "$json" | jq -c --arg n "$burner" '.findings[] | select(.container_name == $n)')"
    assert_eq       "$engine: engine field"   "lxc"     "$(printf '%s' "$finding" | jq -r '.engine')"
    assert_eq       "$engine: container_name" "$burner" "$(printf '%s' "$finding" | jq -r '.container_name')"
    assert_contains "$engine: cmd contains the spinner" "$(printf '%s' "$finding" | jq -r '.cmd')" "while"
    assert_ge       "$engine: age_s >= 1"     "$(printf '%s' "$finding" | jq -r '.age_s')" "1"
    assert_ge       "$engine: cpu_pct high"   "$(printf '%s' "$finding" | jq -r '.cpu_pct | floor')" "5"

    local hint
    hint="$(printf '%s' "$finding" | jq -r '.exec_hint')"
    assert_contains "$engine: exec_hint is engine-correct" "$hint" "lxc-attach"
    assert_contains "$engine: exec_hint names the container" "$hint" "$burner"

    hdr "$engine — SAFETY: burner survives the scan (reporting-only)"
    local still
    if still="$(sudo lxc-attach -n "$burner" -- pgrep -f 'while' 2>&1)" && [ -n "$still" ]; then
        pass "$engine: spinner STILL ALIVE after scan — tool did not terminate it"
    else
        fail "$engine: spinner is GONE after scan — reporting-only tool must NOT terminate findings! ($still)"
    fi

    best_effort_rm "lxc-destroy $burner" sudo lxc-destroy -f -n "$burner"
    pass "$engine: container destroyed"
}

# =========================================================================== #
# Cross-cutting: DBus emission on a PRIVATE session bus.
# =========================================================================== #
run_dbus_block() {
    hdr "DBus — FindingsChanged fires on the session bus"

    if ! command -v gdbus >/dev/null; then
        skip "DBus: gdbus not present — skipping DBus emission check"
        return 0
    fi
    if ! command -v dbus-run-session >/dev/null; then
        skip "DBus: dbus-run-session not present — skipping (cannot isolate a private bus)"
        return 0
    fi

    # Run the whole check inside a private session bus so we never touch the
    # developer's real session. Inside: start a monitor in the background, drive
    # `scan --inject` to deterministically emit the signal, then grep the capture.
    local fixture monitor_out scan_out rc
    fixture="$(mktemp -t cw-inject-finding.XXXXXX.json)"
    monitor_out="$(mktemp -t cw-dbus-monitor.XXXXXX.log)"
    scan_out="$(mktemp -t cw-dbus-scan.XXXXXX.log)"

    # Minimal synthetic finding (reserved placeholders only — public repo).
    cat > "$fixture" <<'JSON'
[
  {
    "host_pid": 2124472,
    "container_pid": 12,
    "engine": "podman",
    "rootless": true,
    "owner_uid": 1000,
    "container_id": "0000000000000000000000000000000000000000000000000000000000000000",
    "container_name": "project-a_yolo",
    "argv0": "ugrep",
    "cmd": "ugrep -G --hidden -rl pattern /",
    "age_s": 6916,
    "cpu_pct": 1116,
    "rss_kb": 35784,
    "exec_hint": "podman exec -it project-a_yolo ps -o pid,%cpu,args -p 12"
  }
]
JSON

    # The CW argv is interpolated into the inner script, safely quoted.
    local cw_quoted="${CW[*]@Q}"
    rc=0
    # The inner script is single-quoted on purpose: $CW_BUS_* and $mon_pid must
    # expand inside the INNER bash (under the private session bus), not out here.
    # Only $cw_quoted is spliced in via an outer expansion. SC2016 is therefore
    # the intended behaviour, not a bug.
    # shellcheck disable=SC2016
    CW_BUS_FIXTURE="$fixture" CW_BUS_MON="$monitor_out" CW_BUS_SCAN="$scan_out" \
    dbus-run-session -- bash -c '
        set -uo pipefail
        gdbus monitor --session \
            --dest org.fedoradesktop.ContainerWatch \
            > "$CW_BUS_MON" 2>&1 &
        mon_pid=$!
        # Give the monitor a beat to subscribe.
        sleep 1
        '"$cw_quoted"' scan --inject "$CW_BUS_FIXTURE" > "$CW_BUS_SCAN" 2>&1
        scan_rc=$?
        # Give the signal time to land in the monitor log, then stop the monitor.
        sleep 1
        if kill "$mon_pid"; then :; fi
        # Reap the monitor; it was just SIGTERMed, so a non-zero wait status is
        # expected and intentionally not treated as an error.
        if wait "$mon_pid"; then :; fi
        exit "$scan_rc"
    ' || rc=$?

    if [ "$rc" -ne 0 ]; then
        fail "DBus: private-session driver / scan exited non-zero ($rc)"
        echo "  --- scan output ---" >&2
        while IFS= read -r line; do echo "    $line" >&2; done < "$scan_out"
    elif grep -q 'FindingsChanged' "$monitor_out"; then
        pass "DBus: FindingsChanged signal observed on the session bus"
    else
        fail "DBus: FindingsChanged NOT observed — monitor capture follows"
        echo "  --- gdbus monitor capture ---" >&2
        while IFS= read -r line; do echo "    $line" >&2; done < "$monitor_out"
    fi

    rm -f "$fixture" "$monitor_out" "$scan_out"
}

# =========================================================================== #
# Cross-cutting: systemd user-unit verification (static, no real session).
# =========================================================================== #
run_systemd_block() {
    hdr "systemd — user units verify"

    if ! command -v systemd-analyze >/dev/null; then
        skip "systemd: systemd-analyze not present — skipping unit verify"
        return 0
    fi

    local svc="$REPO_ROOT/files/home/.config/systemd/user/container-watch.service"
    local tmr="$REPO_ROOT/files/home/.config/systemd/user/container-watch.timer"
    if [ ! -f "$svc" ] || [ ! -f "$tmr" ]; then
        fail "systemd: unit files not found at $svc / $tmr"
        return 0
    fi

    # Verify the repo-source unit files directly (absolute paths). --user picks
    # the user-manager directive set; warnings about %h or absolute ExecStart are
    # advisory — we only fail on a non-zero verify exit.
    local out
    if out="$(systemd-analyze --user verify "$svc" "$tmr" 2>&1)"; then
        pass "systemd: container-watch.{service,timer} verify cleanly"
    else
        fail "systemd: verify reported problems:"
        while IFS= read -r line; do echo "    $line" >&2; done <<< "$out"
    fi
}

# =========================================================================== #
# MAIN
# =========================================================================== #
echo "container-watch L2 acceptance — thresholds CW_AGE_S=$CW_AGE_S CW_CPU_PCT=$CW_CPU_PCT"
echo "Burner image: $BURNER_IMAGE  TTL: ${BURNER_TTL_S}s"

# Podman — always attempt (it is the repo default engine); SKIP if not usable.
hdr "Engine gate: podman"
if have_podman; then
    pass "podman present and usable"
    run_oci_engine_block "podman"
else
    skip "podman: not present"
fi

# Docker — only if the daemon/group is actually usable.
hdr "Engine gate: docker"
if have_docker; then
    pass "docker present and usable"
    run_oci_engine_block "docker"
else
    skip "docker: not present"
fi

# LXC — only if lxc-* tooling + non-interactive sudo are available.
hdr "Engine gate: lxc"
if have_lxc; then
    pass "lxc present and usable"
    run_lxc_block
else
    skip "lxc: not present"
fi

# Cross-cutting checks (engine-independent).
run_dbus_block
run_systemd_block

hdr "RESULT"
if [ "$FAILED" -ne 0 ]; then
    echo "${C_FAIL}ACCEPTANCE FAILED${C_OFF} — one or more assertions did not pass."
    exit 1
fi
echo "${C_PASS}ACCEPTANCE PASSED${C_OFF} — all assertions for every PRESENT engine passed; no stray cw-test-* containers."
exit 0
