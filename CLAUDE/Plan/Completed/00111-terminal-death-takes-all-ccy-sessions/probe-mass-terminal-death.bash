#!/usr/bin/env bash
# probe-mass-terminal-death.bash — for an incident where MANY terminals died at once, gather
# the facts that separate a memory kill from a terminal-emulator death.
#
# WHY THIS EXISTS: on 2026-09-13 every terminal died simultaneously and the presenting theory
# was the OOM killer. It was not OOM — mutter severed the Wayland connection to Ptyxis, which
# owns every window and tab in one process. The distinction is not academic: an OOM diagnosis
# leads to memory limits or reduced concurrency, neither of which touches the real fault. That
# determination took a long manual dig through the journal; this probe makes it a single run.
#
# Fact-finding only: appends to the report file given as $1 and renders no verdict
# (PlanScriptStandards R9). Every leg is READ-ONLY — it reads the journal and /proc, and
# changes nothing.
#
# Normally invoked as a leg of triage.bash. Runnable standalone:
#   ./probe-mass-terminal-death.bash /tmp/report.md
#
# An ABSENT signal is a real answer here and is reported as such: "no kernel OOM lines" is the
# finding that rules memory out, not a probe that failed to run. Several tools used below
# signal "found nothing" with a non-zero exit; each such status is interpreted explicitly
# rather than discarded, so a genuine tool failure still surfaces as unanswered.
#
# Nothing here discards a command's stderr. Tool diagnostics flow to this script's stderr,
# where the caller's run log records them (R13) — a probe that silences the very tools it
# depends on cannot tell "found nothing" from "could not look".
#
# EXIT CODES:
#   0  every question below reached a definite answer (including a definite "absent")
#   1  a question could not be answered — the fact-finding is incomplete, not the host broken
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

REPORT="${1:-}"
if [[ -z "${REPORT}" ]]; then
    printf 'usage: probe-mass-terminal-death.bash <report-file>\n' >&2
    exit 64
fi

# A container has no view of the host's kernel ring, systemd-oomd, coredumps or compositor.
# Answering these questions in a container would produce confident nonsense (R2).
plan_require_host "it reads the host kernel ring, systemd-oomd, coredumps and the compositor's own log"

INCOMPLETE=0

out() { printf '%s\n' "$*" >>"${REPORT}"; }

# count_matches <extended-regex> <text> — number of matching lines, case-insensitive.
#
# grep exits 1 to mean "no match", which is a definite ANSWER to every question below, and 2+
# to mean it actually failed. Interpreting the status is the point: a permissive fallback that
# swallowed every non-zero status alike would report a broken grep as "zero matches" and
# quietly invent a clean bill of health for the host.
count_matches() {
    local pattern="${1:?count_matches requires a pattern}" text="${2-}" n rc=0
    n="$(printf '%s\n' "${text}" | grep -i -c -E "${pattern}")" || rc=$?
    case "${rc}" in
        0) printf '%s\n' "${n}" ;;
        1) printf '0\n' ;;
        *)
            printf '[ERROR] grep failed with exit %s for pattern: %s\n' "${rc}" "${pattern}" >&2
            return "${rc}"
            ;;
    esac
}

# have_tool <name> — is the tool on PATH? Uses a captured value rather than a discarded
# redirect, so nothing is silenced.
have_tool() {
    [[ -n "$(command -v "${1:?have_tool requires a name}")" ]]
}

# ── Question 1: did the kernel OOM killer fire? ───────────────────────────────────────────

OOM_RE='out of memory|oom-kill|oom_reaper|Killed process|invoked oom'
readonly OOM_RE

out ""
out "## 1. Kernel OOM killer"
out ""

kernelLines=""
if kernelLines="$(journalctl --no-pager -b -k)"; then
    kernelTotal="$(printf '%s\n' "${kernelLines}" | wc -l)"
    oomHits="$(count_matches "${OOM_RE}" "${kernelLines}")"
    if [[ "${oomHits}" -eq 0 ]]; then
        out "**No kernel OOM activity.** Zero matches across ${kernelTotal} kernel-ring lines"
        out "for: out of memory, oom-kill, oom_reaper, Killed process, invoked oom."
        out ""
        out "This rules out the in-kernel OOM killer for the current boot."
    else
        out "**Kernel OOM activity present: ${oomHits} matching line(s)** of ${kernelTotal}."
        out ""
        out '```'
        printf '%s\n' "${kernelLines}" | grep -i -E "${OOM_RE}" >>"${REPORT}"
        out '```'
    fi
else
    out "Could not read the kernel ring (journalctl -b -k failed). **Unanswered.**"
    printf '[INCOMPLETE] could not read the kernel ring\n' >&2
    INCOMPLETE=1
fi

# ── Question 2: did systemd-oomd act? ─────────────────────────────────────────────────────
#
# systemd-oomd kills on PSI pressure WITHOUT the kernel OOM killer ever firing, so question 1
# alone does not rule out a memory kill. This is the leg that closes that gap.

OOMD_RE='Killed|Failed|pressure|swap'
readonly OOMD_RE

out ""
out "## 2. systemd-oomd (userspace OOM killer)"
out ""

oomdLog=""
if oomdLog="$(journalctl --no-pager -b -u systemd-oomd --output=short-iso)"; then
    oomdTotal="$(printf '%s\n' "${oomdLog}" | wc -l)"
    oomdActs="$(count_matches "${OOMD_RE}" "${oomdLog}")"
    if [[ "${oomdActs}" -eq 0 ]]; then
        out "**systemd-oomd took no action.** ${oomdTotal} line(s) in its unit log, none"
        out "mentioning a kill, memory pressure or swap."
        out ""
        out "Together with question 1 this rules out a memory kill from either killer."
    else
        out "**systemd-oomd logged ${oomdActs} action/pressure line(s)** of ${oomdTotal}:"
        out ""
        out '```'
        printf '%s\n' "${oomdLog}" | grep -i -E "${OOMD_RE}" >>"${REPORT}"
        out '```'
    fi
else
    out "Could not read the systemd-oomd unit log. **Unanswered.**"
    printf '[INCOMPLETE] could not read the systemd-oomd unit log\n' >&2
    INCOMPLETE=1
fi

# ── Question 3: did anything die on a signal? ─────────────────────────────────────────────
#
# A SIGKILL from either OOM killer leaves a coredump entry; a clean exit(1) does not. This
# distinguishes "was killed" from "chose to exit", which is exactly the 2026-09-13 distinction.

out ""
out "## 3. Coredumps (did anything die on a signal?)"
out ""

if ! have_tool coredumpctl; then
    out "coredumpctl is not on PATH, so the signal-death question is **unanswered**."
    printf '[INCOMPLETE] coredumpctl is not installed\n' >&2
    INCOMPLETE=1
else
    # coredumpctl exits non-zero when it finds nothing, which is a definite ANSWER. The status
    # is captured and interpreted rather than allowed to abort under set -e. stderr is folded
    # into the capture here on purpose: "No coredumps found." is the answer and it arrives on
    # stderr, so discarding it would lose the finding itself.
    coreOut=""
    coreRc=0
    coreOut="$(coredumpctl --no-pager --since=-24h list 2>&1)" || coreRc=$?
    if printf '%s\n' "${coreOut}" | grep -q -i 'no coredumps found'; then
        out "**No coredumps in the last 24h.** Nothing died on a signal, so nothing was"
        out "SIGKILLed — consistent with a process choosing to exit rather than being killed."
    elif [[ "${coreRc}" -eq 0 ]]; then
        out "Coredumps present in the last 24h:"
        out ""
        out '```'
        printf '%s\n' "${coreOut}" >>"${REPORT}"
        out '```'
    else
        out "coredumpctl failed (exit ${coreRc}); the signal-death question is **unanswered**:"
        out ""
        out '```'
        printf '%s\n' "${coreOut}" >>"${REPORT}"
        out '```'
        printf '[INCOMPLETE] coredumpctl failed with exit %s\n' "${coreRc}" >&2
        INCOMPLETE=1
    fi
fi

# ── Question 4: was there memory headroom? ────────────────────────────────────────────────

out ""
out "## 4. Memory headroom now"
out ""
out "Present-tense state. It does not prove what was true at the moment of an incident, but a"
out "machine that idles with most of its RAM free and swap untouched makes memory exhaustion"
out "an unlikely explanation for a past event on the same workload."
out ""
out '```'
if free -h >>"${REPORT}" && swapon --show >>"${REPORT}"; then
    out '```'
else
    out '```'
    out ""
    out "Could not read memory/swap state. **Unanswered.**"
    printf '[INCOMPLETE] could not read memory/swap state\n' >&2
    INCOMPLETE=1
fi

# ── Question 5: did the compositor drop a Wayland client? ─────────────────────────────────
#
# THE leg that found the 2026-09-13 cause. mutter logs "WL: error in client communication
# (pid N)" when it severs a client for a protocol violation. The client then exits on its own,
# leaving no coredump — which is why questions 1-3 all come back clean and the event still
# looks inexplicable without this leg.

WL_RE='WL: error in client communication'
readonly WL_RE

out ""
out "## 5. Wayland client communication errors (all boots)"
out ""

wlLog=""
if wlLog="$(journalctl --no-pager --output=short-iso --grep="${WL_RE}")"; then
    wlHits="$(count_matches "${WL_RE}" "${wlLog}")"
    if [[ "${wlHits}" -eq 0 ]]; then
        out "**No Wayland client-communication errors on record**, across every retained boot."
        out "The compositor has severed no client, so this is not the cause of a mass"
        out "terminal death."
    else
        out "**${wlHits} Wayland client-communication error(s) on record.** Each is the"
        out "compositor severing a client for a protocol violation; the client then exits by"
        out "itself, leaving no coredump. If a pid below is a terminal emulator that owns"
        out "multiple windows or tabs, every one of them died with it."
        out ""
        out '```'
        printf '%s\n' "${wlLog}" >>"${REPORT}"
        out '```'
    fi
else
    out "Could not search the journal for Wayland client errors. **Unanswered.**"
    printf '[INCOMPLETE] could not search for Wayland client errors\n' >&2
    INCOMPLETE=1
fi

# ── Question 6: has the terminal emulator restarted? ──────────────────────────────────────
#
# A terminal whose process is much younger than the graphical session has died and been
# relaunched — the fingerprint of the incident, visible without knowing when it happened.

out ""
out "## 6. Terminal emulator vs compositor uptime"
out ""
out "A terminal emulator process markedly younger than gnome-shell has died and been"
out "relaunched since login. Where that emulator hosts every window in one process, its"
out "restart IS the mass terminal death."
out ""

# ps -C exits 1 and prints nothing when no such process exists, which is an answer ("not
# running"), not a failure — so no redirect is needed to keep the output clean.
shellStart=""
if shellStart="$(ps -C gnome-shell -o lstart= --no-headers)"; then
    out "- gnome-shell started: $(printf '%s' "${shellStart}" | tr -s ' ')"
else
    out "- gnome-shell: not running (not a GNOME session, or not running now)"
fi

termFound=0
for term in ptyxis gnome-terminal- kgx konsole alacritty kitty wezterm-gui foot; do
    termStart=""
    if termStart="$(ps -C "${term}" -o lstart= --no-headers)"; then
        out "- ${term} started: $(printf '%s' "${termStart}" | tr -s ' ' | tr '\n' ';')"
        termFound=1
    fi
done
if [[ "${termFound}" -eq 0 ]]; then
    out "- no known terminal emulator process found by name"
fi

# ── Question 7: is any CCY session insulated from its terminal? ───────────────────────────
#
# The defence this plan exists to build, stated as a measurable fact. A pty owned by a
# ptyxis-spawn-*.scope dies with the tab; one owned by a tmux server or a systemd unit does
# not. This leg is what the plan's acceptance check can assert against.

out ""
out "## 7. Are CCY sessions insulated from terminal death?"
out ""

if ! have_tool tmux; then
    out "- tmux: **not installed**, so no session can be insulated by it"
else
    # The session list is captured rather than discarded — when a server IS running, which
    # sessions exist is exactly what an operator recovering from a crash needs to see. tmux
    # exits non-zero with "no server running on ..." on stderr, which is the answer.
    tmuxOut=""
    tmuxRc=0
    tmuxOut="$(tmux ls 2>&1)" || tmuxRc=$?
    if [[ "${tmuxRc}" -eq 0 ]]; then
        out "- tmux: **server running**, with these sessions:"
        out ""
        out '```'
        printf '%s\n' "${tmuxOut}" >>"${REPORT}"
        out '```'
    else
        out "- tmux: installed, **no server running** — nothing is insulated by it"
        out "  (tmux ls exit ${tmuxRc}: ${tmuxOut})"
    fi
fi
out ""

# pgrep exits 1 when nothing matches — again an answer, not a failure.
claudePids=""
claudeRc=0
claudePids="$(pgrep -x claude)" || claudeRc=$?
if [[ "${claudeRc}" -gt 1 ]]; then
    out "pgrep failed (exit ${claudeRc}); session classification is **unanswered**."
    printf '[INCOMPLETE] pgrep failed with exit %s\n' "${claudeRc}" >&2
    INCOMPLETE=1
elif [[ -z "${claudePids}" ]]; then
    out "No claude process is running, so there is no session to classify."
else
    out "Each claude process below is classified by the cgroup that owns it. A"
    out "ptyxis-spawn-*.scope ancestor means the session dies with its tab; a tmux server or a"
    out "systemd --user unit means it survives."
    out ""
    out '```'
    for pid in ${claudePids}; do
        # A process can exit between pgrep and here, so readability is tested rather than
        # assumed; the absence is reported, not hidden.
        if [[ -r "/proc/${pid}/cgroup" ]]; then
            cg="$(cat "/proc/${pid}/cgroup")"
        else
            cg="unreadable (process exited during the probe, or permission denied)"
        fi
        if ! ttyName="$(ps -o tty= -p "${pid}" --no-headers)"; then
            ttyName="gone"
        fi
        ttyName="$(printf '%s' "${ttyName}" | tr -d ' ')"
        case "${cg}" in
            *ptyxis-spawn-*) verdict="TAB-BOUND — dies with the terminal" ;;
            *tmux*) verdict="tmux-owned — survives the terminal" ;;
            *libpod-*) verdict="container scope — check the pty owner, not the container" ;;
            *) verdict="other cgroup — inspect" ;;
        esac
        printf 'pid %-8s tty=%-8s %s\n' "${pid}" "${ttyName:-?}" "${verdict}" >>"${REPORT}"
        printf '    cgroup: %s\n' "${cg}" >>"${REPORT}"
    done
    out '```'
    out ""
    out "A tty of ? on a live claude marks an ORPHAN: the process survived but its pty did"
    out "not, so there is nothing to attach to and the session cannot be driven."
fi

# ── close ─────────────────────────────────────────────────────────────────────────────────

if [[ "${INCOMPLETE}" -ne 0 ]]; then
    printf '[INCOMPLETE] one or more questions could not be answered; see %s\n' "${REPORT}" >&2
    exit 1
fi
printf '[OK] all questions answered; appended to %s\n' "${REPORT}" >&2
exit 0
