#!/usr/bin/env bash
# probe-pointer.bash — gather FACTS about the pointer path, from kernel interrupt to compositor.
#
# Fact-finding only: appends to the report file given as $2 and renders no verdict
# (PlanScriptStandards R9). READ-ONLY: changes no setting, service or file outside the report
# and the run directory. The one transient exception is capture's private kernel tracing
# instance, which exists only for the capture and is removed however the script ends.
#
# Normally invoked as a leg of triage.bash. Runnable standalone:
#   ./probe-pointer.bash snapshot <report-file>
#   ./probe-pointer.bash capture  <report-file> <seconds> <run-dir>
#
# capture records libinput events for the POINTER devices only (touchpad, its mouse node and
# the TrackPoint). The keyboard is never opened, so no keystroke can reach the capture file.
# It needs root to read /dev/input and the kernel tracing filesystem; the sudo timestamp must
# already be primed (triage.bash does that before its log opens), so this script only ever
# calls `sudo -n`.
#
# EXIT CODES:
#   0  every probe reached a definite answer
#   1  a probe could not be answered — the fact-finding is incomplete, not the system broken
#  64  usage error
set -euo pipefail

# ── R1 bootstrap ──────────────────────────────────────────────────────────────────────────
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

usage() {
    printf 'usage: probe-pointer.bash snapshot <report-file>\n' >&2
    printf '       probe-pointer.bash capture <report-file> <seconds> <run-dir>\n' >&2
    exit 64
}

MODE="${1:-}"
REPORT="${2:-}"
[[ -n "${MODE}" && -n "${REPORT}" ]] || usage

plan_require_host "it reads the host's input devices, interrupts, GPU power state and journal"

INCOMPLETE=0

out() { printf '%s\n' "$*" >>"${REPORT}"; }
sec() {
    out ""
    out "### $1"
    if [[ -n "${2:-}" ]]; then out "READ THIS FOR: $2"; fi
    out ""
}
# failed <label> <rc> — a probe that could not answer is recorded, never dropped.
failed() {
    out "**PROBE FAILED** (rc=$2): $1"
    printf '[INCOMPLETE] %s (rc=%s)\n' "$1" "$2" >&2
    INCOMPLETE=1
}

# grep_or_none <text> <grep-args...> — grep the text; "no match" is an answer, a grep
# error is not. The input is captured first so a failing producer is never masked.
grep_or_none() {
    local text="$1" rc
    shift
    if printf '%s\n' "${text}" | grep "$@"; then return 0; else rc=$?; fi
    if [[ "${rc}" -eq 1 ]]; then
        echo "(none)"
        return 0
    fi
    return "${rc}"
}

# irq_total <name-regex> — summed per-CPU count of the first IRQ line whose name matches.
irq_total() {
    awk -v re="$1" '
        $1 ~ /^[0-9]+:$/ && $0 ~ re {
            s = 0
            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }
            print s; found = 1; exit
        }
        END { if (!found) exit 1 }' /proc/interrupts
}

# event_node <name-regex> — /dev/input/eventN whose device name matches.
event_node() {
    local e
    for e in /sys/class/input/event*; do
        if grep -qE "$1" "${e}/device/name"; then
            printf '/dev/input/%s\n' "$(basename "${e}")"
            return 0
        fi
    done
    return 1
}

dgpu_dir() {
    local d
    for d in /sys/bus/pci/devices/*; do
        if [[ "$(cat "${d}/vendor")" == "0x10de" && "$(cat "${d}/class")" == 0x03* ]]; then
            printf '%s\n' "${d}"
            return 0
        fi
    done
    return 1
}

show_load() {
    cat /proc/loadavg
    local p
    for p in cpu memory io; do printf '%s: %s\n' "${p}" "$(head -n 1 "/proc/pressure/${p}")"; done
    echo
    ps -eo pid,pcpu,pmem,etime,comm --sort=-pcpu | awk 'NR <= 16'
}

show_shell_threads() {
    local pid
    pid="$(pgrep -x gnome-shell)"
    local threads
    threads="$(ps -L -o tid=,pcpu=,cputime=,comm= -p "${pid}")"
    printf '  TID  %%CPU     TIME COMMAND\n'
    sort -k2 -rn <<<"${threads}" | awk 'NR <= 10'
}

# show_dgpu_holders — processes with an NVIDIA node open. A holder can wake the dGPU from
# runtime suspend, and a resume has been measured at about 1.6s.
show_dgpu_holders() {
    local p f target
    for p in /proc/[0-9]*; do
        [[ -r "${p}/comm" ]] || continue
        for f in "${p}"/fd/*; do
            if target="$(readlink "${f}")"; then
                case "${target}" in
                    /dev/nvidia*) printf '%s %s %s\n' "${p#/proc/}" "$(cat "${p}/comm")" "${target}" ;;
                esac
            fi
        done
    done | sort -u
}

show_displays() {
    local c
    for c in /sys/class/drm/card*-*; do
        if [[ "$(cat "${c}/status")" == "connected" ]]; then
            printf '%s enabled=%s first-mode=%s\n' "$(basename "${c}")" "$(cat "${c}/enabled")" "$(head -n 1 "${c}/modes")"
        fi
    done
}

show_dgpu() {
    local d
    if ! d="$(dgpu_dir)"; then
        echo "no NVIDIA display-class PCI device present"
        return 0
    fi
    printf '%s runtime_status=%s control=%s\n' "${d}" "$(cat "${d}/power/runtime_status")" "$(cat "${d}/power/control")"
}

show_thermal() {
    printf 'cpu0 package_throttle_count=%s core_throttle_count=%s\n' \
        "$(cat /sys/devices/system/cpu/cpu0/thermal_throttle/package_throttle_count)" \
        "$(cat /sys/devices/system/cpu/cpu0/thermal_throttle/core_throttle_count)"
    local z
    for z in /sys/class/thermal/thermal_zone*; do
        printf '%s %sC\n' "$(cat "${z}/type")" "$(($(cat "${z}/temp") / 1000))"
    done | sort -u
}

show_pointer_config() {
    local serio f s
    serio="$(dirname "$(dirname "$(readlink -f "$(dirname "$(grep -lE 'TrackPoint' /sys/class/input/input*/name)")")")")"
    for f in sensitivity rate resolution protocol firmware_id; do
        if [[ -r "${serio}/${f}" ]]; then printf 'trackpoint %s=%s\n' "${f}" "$(cat "${serio}/${f}")"; fi
    done
    for s in mouse touchpad pointingstick; do
        gsettings list-recursively "org.gnome.desktop.peripherals.${s}" | grep -E ' (speed|accel-profile) '
    done
}

show_quirks() {
    local product
    product="$(cat /sys/class/dmi/id/product_version)"
    printf 'DMI product_version: %s\n' "${product}"
    printf 'libinput: %s\n' "$(rpm -q libinput)"
    if [[ -d /etc/libinput ]]; then
        printf -- '--- /etc/libinput (local overrides)\n'
        cat /etc/libinput/*
    else
        echo "--- /etc/libinput does not exist (no local overrides)"
    fi
    # The shipped quirks match on the DMI product version with spaces removed.
    printf -- '--- shipped quirk sections matching pvr%s ((none) = no shipped quirk names this model)\n' "${product// /}"
    local quirks
    quirks="$(cat /usr/share/libinput/*.quirks)"
    grep_or_none "${quirks}" -B4 -A4 "pvr${product// /}:"
}

show_kernel_log() {
    local log
    log="$(journalctl --no-pager -k --since "-30min")"
    grep_or_none "${log}" -iE 'i2c|hid|elan|psmouse|trackpoint|nvidia|NVRM|i915|evdi|acpi|thermal'
}

show_shell_log() {
    local log stage other
    log="$(journalctl --no-pager --since "-30min" _COMM=gnome-shell)"
    echo "--- 'Can't update stage views' warnings per minute (last 30 min)"
    stage="$(grep_or_none "${log}" "Can't update stage views")"
    if [[ "${stage}" == "(none)" ]]; then echo "${stage}"; else cut -c1-12 <<<"${stage}" | uniq -c; fi
    echo "--- every other gnome-shell line (last 30 min)"
    other="$(grep_or_none "${log}" -v "Can't update stage views")"
    cut -c1-220 <<<"${other}"
}

snapshot() {
    out ""
    out "## Snapshot at $(date '+%F %T %Z')"
    local rc

    sec "load and pressure" "any non-zero 'some' avg10 means contention; a process near 100% is a suspect"
    if show_load >>"${REPORT}" 2>&1; then :; else rc=$?; failed "load and pressure" "${rc}"; fi

    sec "gnome-shell threads" "the compositor and KMS threads; a pegged thread points at the compositor"
    if show_shell_threads >>"${REPORT}" 2>&1; then :; else rc=$?; failed "gnome-shell threads" "${rc}"; fi

    sec "connected displays"
    if show_displays >>"${REPORT}" 2>&1; then :; else rc=$?; failed "connected displays" "${rc}"; fi

    sec "NVIDIA dGPU runtime power state" "'resuming' or 'active' with nothing using it suggests a wake stall"
    if show_dgpu >>"${REPORT}" 2>&1; then :; else rc=$?; failed "dGPU state" "${rc}"; fi

    sec "processes holding the NVIDIA dGPU open" "any of these can wake the dGPU; gnome-shell here means the compositor itself uses it"
    if show_dgpu_holders >>"${REPORT}" 2>&1; then :; else rc=$?; failed "dGPU holders" "${rc}"; fi

    sec "thermal"
    if show_thermal >>"${REPORT}" 2>&1; then :; else rc=$?; failed "thermal" "${rc}"; fi

    sec "pointer configuration" "TrackPoint sysfs and GNOME speed; defaults are sensitivity=128 and speed 0.0"
    if show_pointer_config >>"${REPORT}" 2>&1; then :; else rc=$?; failed "pointer configuration" "${rc}"; fi

    sec "libinput quirks for this model" "an AttrTrackpointMultiplier line here is what scales TrackPoint speed"
    if show_quirks >>"${REPORT}" 2>&1; then :; else rc=$?; failed "libinput quirks" "${rc}"; fi

    sec "kernel log, input/GPU/ACPI, last 30 min"
    if show_kernel_log >>"${REPORT}" 2>&1; then :; else rc=$?; failed "kernel log" "${rc}"; fi

    sec "gnome-shell log, last 30 min"
    if show_shell_log >>"${REPORT}" 2>&1; then :; else rc=$?; failed "gnome-shell log" "${rc}"; fi
}

# sample_rates <seconds> — one line per second of interrupt, GPE and power-state deltas.
sample_rates() {
    local secs="$1" i gpu=""
    local i2c elan tp gfx gpe thr
    local n_i2c n_elan n_tp n_gfx n_gpe n_thr
    if gpu="$(dgpu_dir)"; then :; else gpu=""; fi
    snap() {
        i2c="$(irq_total 'i2c_designware')"
        elan="$(irq_total 'ELAN')"
        tp="$(irq_total ' 12-edge .*i8042')"
        gfx="$(irq_total 'i915')"
        gpe="$(awk '{print $1}' /sys/firmware/acpi/interrupts/sci)"
        thr="$(cat /sys/devices/system/cpu/cpu0/thermal_throttle/package_throttle_count)"
    }
    printf 'time      i2c/s  touchpad/s  trackpoint/s  i915/s  acpi-sci/s  throttle+  dgpu\n'
    snap
    for ((i = 0; i < secs; i++)); do
        n_i2c="${i2c}" n_elan="${elan}" n_tp="${tp}" n_gfx="${gfx}" n_gpe="${gpe}" n_thr="${thr}"
        sleep 1
        snap
        printf '%s %6d %11d %13d %7d %11d %10d  %s\n' "$(date +%T)" \
            "$((i2c - n_i2c))" "$((elan - n_elan))" "$((tp - n_tp))" "$((gfx - n_gfx))" \
            "$((gpe - n_gpe))" "$((thr - n_thr))" \
            "$(if [[ -n "${gpu}" ]]; then cat "${gpu}/power/runtime_status"; else echo n/a; fi)"
    done
}

# watch_dgpu <seconds> <gpu-dir> — every runtime-PM transition with a millisecond timestamp,
# plus the newest processes at each wake, since a freshly started one is the likeliest opener.
watch_dgpu() {
    local secs="$1" gpu="$2" end state prev=""
    end=$((SECONDS + secs))
    while ((SECONDS < end)); do
        state="$(cat "${gpu}/power/runtime_status")"
        if [[ "${state}" != "${prev}" ]]; then
            printf '%s %s\n' "$(date +%T.%3N)" "${state}"
            if [[ "${state}" == "resuming" ]]; then
                ps -eo pid=,etimes=,comm= --sort=etimes | awk 'NR <= 5 { print "    newest process:", $0 }'
            fi
            prev="${state}"
        fi
        sleep 0.1
    done
}

# stall_seconds <events> <report> — join the wall-clock-prefixed motion events with the
# per-second table. A second with real finger motion (>=40 libinput motions) but few i915
# interrupts is a second the compositor was not presenting while the cursor should move. A
# resting finger keeps the touchpad IRQ rate high with no motions, so it is excluded.
stall_seconds() {
    awk '
        FNR == NR {
            if ($3 == "POINTER_MOTION" && $2 ~ /^-?event/) m[$1]++
            next
        }
        /^time / { inTable = 1; next }
        inTable && !/^[0-9][0-9]:/ { inTable = 0 }
        inTable {
            if (m[$1] >= 40) {
                moving++
                if ($5 < 40) { stalls++; rows = rows sprintf("%s  motions=%d  touchpad/s=%d  i915/s=%d  dgpu=%s\n", $1, m[$1], $3, $5, $NF) }
            }
        }
        END {
            printf "seconds with finger motion: %d; of those with i915/s < 40: %d\n", moving, stalls
            printf "%s", rows
        }' "$1" "$2"
}

# summarise_events <file> — per device: a histogram of gaps between consecutive motion events.
# A healthy touchpad reports every ~7ms, so moving smoothly lands in "<20ms". Finger lifts and
# pauses land in ">=300ms". The 20-300ms buckets are the stutter band.
summarise_events() {
    awk '
        $3 == "POINTER_MOTION" {
            for (i = 4; i <= NF; i++) if ($i ~ /^\+[0-9.]+s$/) { t = substr($i, 2) + 0; break }
            # libinput marks a device change with a leading "-"; it is the same device.
            dev = $2
            sub(/^-/, "", dev)
            n[dev]++
            if (dev in last) {
                g = (t - last[dev]) * 1000
                if (g < 20) b1[dev]++
                else if (g < 50) b2[dev]++
                else if (g < 100) b3[dev]++
                else if (g < 300) b4[dev]++
                else b5[dev]++
            }
            last[dev] = t
        }
        END {
            printf "%-9s %8s %7s %9s %10s %11s %8s\n", "device", "motions", "<20ms", "20-50ms", "50-100ms", "100-300ms", ">=300ms"
            for (d in n) printf "%-9s %8d %7d %9d %10d %11d %8d\n", d, n[d], b1[d], b2[d], b3[d], b4[d], b5[d]
        }' "$1"
}

# ── background recorders and their teardown ──────────────────────────────────────────────
# A Ctrl-C at the terminal reaches none of capture's recorders on its own: a non-interactive
# bash starts every background job with SIGINT and SIGQUIT ignored, `timeout` moves itself
# into its own process group, and sudo runs its command on a separate pty. Left alone they
# outlive the run, and because they inherited the run log's pipe as stderr, triage.bash's
# log drain then waits on them for the rest of the capture, so the run appears to ignore the
# Ctrl-C. They are therefore stopped here, explicitly, however this script ends.
#
# This script opens no run log, so the library's trap and plan_on_cleanup are not armed in
# this process (PlanScriptStandards R4 concerns scripts that do); it owns its traps instead.
BG_STOP_PIDS=() # producers, sent SIGTERM on teardown
BG_WAIT_PIDS=() # every background job, reaped on teardown (readers end on EOF)
BG_TRACE_PIDS=() # the display trace's reader and aggregator, reaped before its instance goes
TRACE_INSTANCE=""
FIFOS=()

stop_background() {
    local pid rc ok=0 f
    for pid in "${BG_STOP_PIDS[@]+"${BG_STOP_PIDS[@]}"}"; do
        # Fails only for a job already reaped by capture's own wait, which is then finished.
        if ! kill -TERM "${pid}" 2>/dev/null; then
            printf '[teardown] background job %s had already ended\n' "${pid}" >&2
        fi
    done
    for pid in "${BG_WAIT_PIDS[@]+"${BG_WAIT_PIDS[@]}"}"; do
        if wait "${pid}"; then rc=0; else rc=$?; fi
        printf '[teardown] background job %s ended (rc=%s)\n' "${pid}" "${rc}" >&2
    done
    BG_STOP_PIDS=()
    BG_WAIT_PIDS=()
    # Checked, not assumed: set-up records the instance before creating it, so a set-up that
    # failed part-way is still torn down.
    if [[ -n "${TRACE_INSTANCE}" ]] && sudo -n test -d "${TRACE_INSTANCE}"; then
        if sudo -n rmdir "${TRACE_INSTANCE}"; then
            TRACE_INSTANCE=""
        else
            printf '[TEARDOWN FAILED] kernel tracing instance %s is still present\n' "${TRACE_INSTANCE}" >&2
            ok=1
        fi
    fi
    for f in "${FIFOS[@]+"${FIFOS[@]}"}"; do rm -f "${f}"; done
    FIFOS=()
    return "${ok}"
}

# On a signal: note it in the report, tear down, then re-raise it with the default
# disposition, so triage.bash sees this leg die of the signal and runs its own handler instead
# of carrying on to the next leg. A signal usually lands inside sample_rates, whose output is
# redirected into the report, so teardown's messages go to the stderr saved here instead.
arm_teardown() {
    exec {TEARDOWN_FD}>&2
    trap 'stop_background 2>&"${TEARDOWN_FD}"' EXIT
    trap 'trap - EXIT INT; out "**CAPTURE INTERRUPTED** (SIGINT): recorders stopped, nothing below was analysed"; if ! stop_background 2>&"${TEARDOWN_FD}"; then echo "[WARN] teardown incomplete" >&"${TEARDOWN_FD}"; fi; kill -INT "$$"' INT
    trap 'trap - EXIT TERM; out "**CAPTURE INTERRUPTED** (SIGTERM): recorders stopped, nothing below was analysed"; if ! stop_background 2>&"${TEARDOWN_FD}"; then echo "[WARN] teardown incomplete" >&"${TEARDOWN_FD}"; fi; kill -TERM "$$"' TERM
    trap 'trap - EXIT HUP; out "**CAPTURE INTERRUPTED** (SIGHUP): recorders stopped, nothing below was analysed"; if ! stop_background 2>&"${TEARDOWN_FD}"; then echo "[WARN] teardown incomplete" >&"${TEARDOWN_FD}"; fi; kill -HUP "$$"' HUP
}

# start_display_trace <seconds> <out-file> — record, per second, what the display engine did
# with the cursor: pointer-device interrupts, hardware-cursor plane moves, primary-plane
# updates, from kernel tracepoints in a private tracing instance. This is the
# layer libinput cannot see: whether the compositor turned input into cursor movement on the
# panel's pipe. Returns non-zero when the instance cannot be set up.
start_display_trace() {
    local secs="$1" out="$2" tpIrq inst fifo wall up
    tpIrq="$(awk '$0 ~ /ELAN/ { sub(":", "", $1); print $1; exit }' /proc/interrupts)"
    [[ -n "${tpIrq}" ]] || return 1
    inst="/sys/kernel/tracing/instances/plan00143-$$"
    # boot clock, so /proc/uptime converts trace time to wall time; buffer_percent 0 hands
    # events to the reader as they arrive instead of when the buffer is half full.
    TRACE_INSTANCE="${inst}"
    sudo -n sh -c '
        set -e
        d="$1"
        mkdir "$d"
        echo boot >"$d/trace_clock"
        echo 0 >"$d/buffer_percent"
        echo 4096 >"$d/buffer_size_kb"
        echo "irq == $2 || irq == 12" >"$d/events/irq/irq_handler_entry/filter"
        echo 1 >"$d/events/irq/irq_handler_entry/enable"
        echo 1 >"$d/events/i915/intel_plane_update_arm/enable"
        echo 1 >"$d/tracing_on"' sh "${inst}" "${tpIrq}" || return 1
    read -r wall < <(date +%s.%N)
    read -r up _ </proc/uptime
    fifo="${out}.fifo"
    mkfifo "${fifo}"
    FIFOS+=("${fifo}")
    aggregate_display_trace "$(awk -v w="${wall}" -v u="${up}" 'BEGIN { printf "%.3f", w - u }')" \
        "${tpIrq}" <"${fifo}" >"${out}" &
    BG_WAIT_PIDS+=("$!")
    BG_TRACE_PIDS+=("$!")
    timeout "${secs}" sudo -n cat "${inst}/trace_pipe" >"${fifo}" &
    BG_STOP_PIDS+=("$!")
    BG_WAIT_PIDS+=("$!")
    BG_TRACE_PIDS+=("$!")
}

# aggregate_display_trace <wall-offset> <touchpad-irq> — one row per second of trace:
# interrupts from each pointer device, cursor-plane moves (a change of on-screen position),
# primary-plane updates, the longest gap between consecutive cursor moves ending in that
# second while input kept arriving (3+ interrupts in the gap), and the longest wait from a
# TrackPoint interrupt to the next cursor move. The TrackPoint only reports while pressed, so
# its latency is not inflated by a finger resting on the touchpad, which reports continuously
# without moving anything; the gap column is, so read it only in seconds with motion.
aggregate_display_trace() {
    awk -v off="$1" -v tpIrq="$2" '
        function flush() {
            if (sec == "") return
            printf "%s %6d %6d %6d %7d %8d %8d\n", strftime("%T", sec + off), tp, tk, mv, pri, gap, lat
            tp = tk = mv = pri = gap = lat = 0
            fflush()
        }
        BEGIN {
            print "time     tp-irq tk-irq cursor primary maxgapms tklatms"
            sec = ""
        }
        {
            ts = ""
            for (i = 1; i <= NF; i++) if ($i ~ /^[0-9]+\.[0-9]+:$/) { ts = substr($i, 1, length($i) - 1) + 0; ev = $(i + 1); break }
            if (ts == "") next
            s = int(ts)
            if (sec != "" && s != sec) flush()
            sec = s
            if (ev == "irq_handler_entry:") {
                inputs++
                if ($0 ~ (" irq=" tpIrq " ")) tp++
                else { tk++; if (tkPend == "") tkPend = ts }
            } else if (ev == "intel_plane_update_arm:" && $0 ~ /, cursor [A-Z],/) {
                if ($NF != lastPos) {
                    mv++
                    lastPos = $NF
                    if (lastMove != "" && inputs >= 3 && (ts - lastMove) * 1000 > gap) gap = int((ts - lastMove) * 1000)
                    lastMove = ts
                    inputs = 0
                    if (tkPend != "") { if ((ts - tkPend) * 1000 > lat) lat = int((ts - tkPend) * 1000); tkPend = "" }
                }
            } else if (ev == "intel_plane_update_arm:" && $0 ~ /, plane 1[A-Z],/) {
                pri++
            }
        }
        END { flush() }'
}

# cursor_join <events> <display-trace> — for every second with real finger or stick motion
# (>=40 libinput motions), how many times the hardware cursor actually moved on the panel.
cursor_join() {
    awk '
        FNR == NR {
            if ($3 == "POINTER_MOTION" && $2 ~ /^-?event/) m[$1]++
            next
        }
        /^time / { next }
        { mv[$1] = $4; pri[$1] = $5; gap[$1] = $6; lat[$1] = $7; seen += $4 }
        END {
            for (t in m) if (m[t] >= 40) {
                moving++
                if (mv[t] + 0 < m[t] / 4) { few++; rows = rows sprintf("%s  motions=%d  cursor-moves=%d  primary=%d  maxgap=%sms  tklat=%sms\n", t, m[t], mv[t], pri[t], gap[t] + 0, lat[t] + 0) }
            }
            printf "hardware-cursor moves in the whole trace: %d\n", seen
            printf "seconds with pointer motion: %d; of those with cursor moves < motions/4: %d\n", moving, few
            printf "%s", rows | "sort"
        }' "$1" "$2"
}

capture() {
    local secs="$1" runDir="$2" rc touchpad mousenode trackpoint events pid readerPid gpu watchPid="" transitions
    local display eventsFifo
    out ""
    out "## Timed capture, ${secs}s, starting $(date '+%F %T %Z')"

    if ! touchpad="$(event_node 'Touchpad')"; then failed "no touchpad event node found" 1; fi
    if ! mousenode="$(event_node 'ELAN.*Mouse')"; then failed "no touchpad mouse event node found" 1; fi
    if ! trackpoint="$(event_node 'TrackPoint')"; then failed "no TrackPoint event node found" 1; fi
    if [[ "${INCOMPLETE}" -ne 0 ]]; then return 0; fi
    if ! command -v libinput >/dev/null; then
        failed "libinput CLI not on PATH (it ships with the libinput package)" 127
        return 0
    fi
    if ! sudo -n true; then
        failed "sudo is not primed; run through triage.bash, which primes it before logging" 1
        return 0
    fi

    arm_teardown
    events="${runDir}/libinput-pointer-events.txt"
    printf '==> MOVE THE TOUCHPAD, then the TrackPoint, for the next %ss\n' "${secs}"
    # Each event line is prefixed with the wall-clock second, so it joins the per-second table
    # exactly; libinput's own timestamps are relative to an unrecorded start. A named pipe, not
    # a pipeline, so that timeout's own PID is known and teardown can stop it.
    eventsFifo="${events}.fifo"
    mkfifo "${eventsFifo}"
    FIFOS+=("${eventsFifo}")
    awk '{ print strftime("%T"), $0; fflush() }' <"${eventsFifo}" >"${events}" &
    readerPid=$!
    BG_WAIT_PIDS+=("${readerPid}")
    timeout "${secs}" sudo -n libinput debug-events \
        --device "${touchpad}" --device "${mousenode}" --device "${trackpoint}" >"${eventsFifo}" 2>&1 &
    pid=$!
    BG_STOP_PIDS+=("${pid}")
    BG_WAIT_PIDS+=("${pid}")
    transitions="${runDir}/dgpu-transitions.txt"
    if gpu="$(dgpu_dir)"; then
        watch_dgpu "${secs}" "${gpu}" >"${transitions}" 2>&1 &
        watchPid=$!
        BG_STOP_PIDS+=("${watchPid}")
        BG_WAIT_PIDS+=("${watchPid}")
    fi
    display="${runDir}/display-trace.txt"
    local traceOk=1
    if ! start_display_trace "${secs}" "${display}"; then
        traceOk=0
        failed "could not set up the kernel tracing instance for the display trace" 1
    fi

    sec "per-second rates during the capture" \
        "touchpad/s near 0 while moving = the pad stopped reporting; acpi-sci/s or throttle+ spikes at onset = H3; dgpu flipping state = H3"
    if sample_rates "${secs}" >>"${REPORT}" 2>&1; then :; else rc=$?; failed "rate sampling" "${rc}"; fi

    # Every recorder is bounded by the same duration, so these waits end with the capture.
    if wait "${pid}"; then rc=0; else rc=$?; fi
    local libinputRc="${rc}" watchRc=0
    if wait "${readerPid}"; then :; else rc=$?; failed "libinput event reader" "${rc}"; fi
    if [[ -n "${watchPid}" ]]; then
        if wait "${watchPid}"; then watchRc=0; else watchRc=$?; fi
    fi
    # Everything capture started is reaped; teardown now only removes the tracing instance
    # and the named pipes, after the trace reader it waits for has ended on schedule.
    BG_STOP_PIDS=()
    BG_WAIT_PIDS=("${BG_TRACE_PIDS[@]+"${BG_TRACE_PIDS[@]}"}")
    if stop_background; then :; else rc=$?; failed "display-trace teardown" "${rc}"; fi

    # 124 is timeout ending the capture on schedule, which is the expected outcome.
    if [[ "${libinputRc}" -ne 0 && "${libinputRc}" -ne 124 ]]; then
        failed "libinput debug-events exited unexpectedly; see ${events}" "${libinputRc}"
        return 0
    fi

    if [[ -n "${watchPid}" ]]; then
        sec "dGPU runtime-PM transitions (100ms resolution)" \
            "a resuming->active span is a wake; one that overlaps the lag, lasting about as long as the freeze, supports H5"
        if [[ "${watchRc}" -eq 0 ]]; then cat "${transitions}" >>"${REPORT}"; else failed "dGPU watcher" "${watchRc}"; fi
    fi

    if [[ "${traceOk}" -eq 1 ]]; then
        sec "cursor on the panel: display-engine trace joined with libinput motion" \
            "seconds listed here had pointer motion but the hardware cursor barely moved = the delay is in the compositor or display commit path (H1/H5/H6); motion with the cursor moving normally while it looked laggy = after the plane update (panel self-refresh, H6). 'hardware-cursor moves ... 0' means the cursor is composited, so read the primary column in ${display} instead"
        local cursorRows
        if cursorRows="$(cursor_join "${events}" "${display}")"; then out "${cursorRows}"; else rc=$?; failed "cursor join" "${rc}"; fi
        out "Per-second display trace: ${display}"
    fi

    sec "libinput motion-event timing per device (${touchpad} touchpad, ${mousenode} touchpad mouse node, ${trackpoint} TrackPoint)" \
        "counts in the 20-300ms stutter band while the cursor lagged = the device or kernel path (H2); nearly all <20ms while it lagged = the delay is AFTER libinput (compositor, H1/H5)"
    if summarise_events "${events}" >>"${REPORT}" 2>&1; then :; else rc=$?; failed "event summary" "${rc}"; fi

    local stalls
    if stalls="$(stall_seconds "${events}" "${REPORT}")"; then
        sec "compositor stall seconds (finger moving, screen not presenting)" \
            "the lag signature for H1/H5; a run of these rows is an episode, and the dgpu column says whether a wake coincided"
        out "${stalls}"
    else
        rc=$?
        failed "stall join" "${rc}"
    fi
    out ""
    out "Raw pointer events: ${events}"
}

case "${MODE}" in
    snapshot)
        snapshot
        ;;
    capture)
        [[ "${3:-}" =~ ^[0-9]+$ && -n "${4:-}" ]] || usage
        capture "$3" "$4"
        ;;
    *)
        usage
        ;;
esac

exit "${INCOMPLETE}"
