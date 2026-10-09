#!/usr/bin/env bash
# probe.bash — Plan 00164: one triage leg, named by its first argument. Run by triage.bash
# (each leg is a command, not a function, so shellcheck sees every probe called). Read-only.
#
#   probe.bash settings              the extension keys that decide the paste and its Enter
#   probe.bash deployed <repo-root>  whether the deployed wsi and wsi-stream are the checkout's
#   probe.bash dictations <count>    focus / paste / Enter lines of the last <count>
#                                    dictations with a focus loss, and of the last 2 without
set -euo pipefail

LOG_DIR="${HOME}/.local/share/speech-to-text"
BIN_DIR="${HOME}/.local/bin"

# probe <label> <command...> — a non-zero exit is data, printed with its output
probe() {
    local label="$1" out rc
    shift
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "${label}" "${rc}" "${out:-(no output)}"
}

show_settings() {
    local key
    if [[ ! -x "${BIN_DIR}/wsi-setting" ]]; then
        printf '[FATAL] %s/wsi-setting is not deployed: run play-speech-to-text.yml\n' "${BIN_DIR}" >&2
        return 1
    fi
    for key in debug-mode auto-paste auto-enter streaming-mode streaming-startup-mode \
            continuous-dictation dictation-paste-interval-seconds stop-grace-seconds \
            paste-default-mode paste-ctrl-v-apps paste-save-apps claude-enabled; do
        probe "${key}" "${BIN_DIR}/wsi-setting" "${key}"
    done
}

compare_deployed() {
    local repo_root="$1" name
    for name in wsi wsi-stream; do
        probe "deployed ${name} is the checkout's (cmp)" \
            cmp "${BIN_DIR}/${name}" "${repo_root}/files/home/.local/bin/${name}"
    done
}

# The dictations in the log, split at each panel launch line, keeping only the lines about
# focus, the paste key, the paste, the Enter and the save: never the dictated text. A
# launch line is cut to the recorder's name and flags. The panel logs a "Paste into" line
# each time the recorder asks, so a focus loss shows every poll, to the millisecond.
show_dictations() {
    local want="$1" logs=() f
    for f in "${LOG_DIR}/debug.log.old" "${LOG_DIR}/debug.log"; do
        if [[ -f "${f}" ]]; then
            logs+=("${f}")
        fi
    done
    if [[ "${#logs[@]}" -eq 0 ]]; then
        printf '[FATAL] no debug log in %s: turn on Debug Logging in the panel, dictate, re-run\n' "${LOG_DIR}" >&2
        return 1
    fi
    printf 'Logs read, oldest first: %s\n\n' "${logs[*]}"
    awk -v want="${want}" '
        function flush() {
            if (block == "") return
            if (lost) { nl++; lossy[nl] = block }
            else if (pasted) { nn++; normal[nn] = block }
            block = ""; lost = 0; pasted = 0
        }
        /\[EXT\] Launching/ {
            flush()
            line = $0
            sub(/ [^ ]*\/\.local\/bin\//, " ", line)
            n++
            block = sprintf("--- dictation %d ---\n%s\n", n, line)
            next
        }
        block == "" { next }
        /lost focus|focus back|Pasting into|Paste into|Paste simulated|Simulating (Ctrl|Shift|Enter)|Pasting a chunk|chunk was not pasted|before the Enter|Enter key|Save simulated|Nothing was pasted|would not take focus|was closed|ydotool|Auto-paste failed|Recording aborted/ {
            block = block $0 "\n"
            if ($0 ~ /lost focus/) lost = 1
            if ($0 ~ /Paste simulated|Pasting into|Enter key/) pasted = 1
        }
        END {
            flush()
            printf "### dictations in the log: %d; with a focus loss: %d; pasted with none: %d\n\n", n, nl, nn
            printf "### THE LAST %d WITH A FOCUS LOSS (read these first)\n\n", want
            for (i = (nl > want ? nl - want + 1 : 1); i <= nl; i++) print lossy[i]
            printf "### THE LAST 2 PASTED WITH NO FOCUS LOSS (for comparison)\n\n"
            for (i = (nn > 2 ? nn - 1 : 1); i <= nn; i++) print normal[i]
        }
    ' "${logs[@]}"
}

case "${1:-}" in
    settings) show_settings ;;
    deployed) compare_deployed "${2:?deployed needs the repo root}" ;;
    dictations) show_dictations "${2:?dictations needs a count}" ;;
    *)
        printf 'usage: probe.bash settings | deployed <repo-root> | dictations <count>\n' >&2
        exit 2
        ;;
esac
