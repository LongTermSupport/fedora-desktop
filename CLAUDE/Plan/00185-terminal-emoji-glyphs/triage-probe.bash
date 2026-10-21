#!/usr/bin/env bash
# Plan 00185 — triage-probe.bash: the probes behind triage.bash, one fact group per call.
#
# Usage: triage-probe.bash <probe> <report-file>
#   terminal    the terminal this run is in: its environment variables and parent chain
#   processes   every running terminal emulator (start time, args), versions, VTE packages
#   fonts       emoji font packages, install times, rpm -V, font files incl. user font dirs
#   fontconfig  fc-match -s emoji, fc-match and fc-list per code point, emoji config files
#   cache       fontconfig cache file ages against the emoji font files' mtimes
#   mapped      the emoji font files each running terminal has mapped, (deleted) included
#   settings    each terminal's font and renderer settings and the GPU renderer environment
#
# Read-only: it reads files, /proc and the rpm database and runs fc-match, fc-list and each
# terminal's --version. Each fact goes to stdout and is appended to the report. A probe that
# cannot establish its fact exits non-zero, so triage.bash names the leg.
set -euo pipefail

probe="${1:?usage: triage-probe.bash <probe> <report-file>}"
report="${2:?usage: triage-probe.bash <probe> <report-file>}"

readonly FENCE='```'
# The issue's code points: 1f4c8 chart (drawn as a hammer), 1f4ca bar chart (drawn as a
# knife), 1f9f5 thread, 1f4e6 package, 1f528 hammer (what 1f4c8 became, 0x60 above it).
readonly CODEPOINTS=(1f4c8 1f4ca 1f9f5 1f4e6 1f528)
readonly TERMINALS=(ptyxis ptyxis-agent gnome-terminal-server kitty ghostty alacritty foot code)

# terminal_pids — "pid name" for each of this user's processes whose executable is a terminal
# emulator. Matched on /proc/<pid>/exe, not on comm, which the kernel cuts at 15 characters.
terminal_pids() {
    local dir exe name want
    for dir in /proc/[0-9]*; do
        [[ -O "${dir}" ]] || continue
        if exe="$(readlink "${dir}/exe" 2>&1)"; then
            name="${exe##*/}"
            for want in "${TERMINALS[@]}"; do
                if [[ "${name}" == "${want}" ]]; then
                    printf '%s %s\n' "${dir#/proc/}" "${name}"
                fi
            done
        fi
    done
}

fact() {
    printf '%s\n' "$1" | tee -a "${report}"
}

block() {
    printf '\n%s:\n\n%s\n%s\n%s\n' "$1" "${FENCE}" "$2" "${FENCE}" | tee -a "${report}"
}

# run <title> <cmd...> — a fenced block of the command's output; a non-zero exit is data.
run() {
    local title="$1" out rc
    shift
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    block "${title} (rc=${rc})" "${out:-(no output)}"
}

need() {
    if ! command -v "$1" >/dev/null; then
        printf '[ERROR] %s is not installed; it comes from the fontconfig package, which\n' "$1" >&2
        printf '  play-ms-fonts.yml declares. Deploy that play; do NOT install it by hand.\n' >&2
        return 1
    fi
}

probe_terminal() {
    local pid="$$" line
    run "terminal environment" grep -E '^(TERM|TERM_PROGRAM|TERM_PROGRAM_VERSION|COLORTERM|VTE_VERSION|KITTY_PID|KITTY_WINDOW_ID|GHOSTTY_RESOURCES_DIR|GHOSTTY_BIN_DIR|ALACRITTY_WINDOW_ID|ALACRITTY_LOG|PTYXIS_VERSION|GNOME_TERMINAL_SCREEN|VSCODE_PID|VSCODE_INJECTION|TMUX|STY|XDG_SESSION_TYPE|WAYLAND_DISPLAY|DISPLAY|GSK_RENDERER|GDK_BACKEND|GDK_DEBUG|LIBGL_ALWAYS_SOFTWARE|LANG|LC_ALL|LC_CTYPE)=' <(env | sort)
    local chain=""
    while [[ "${pid}" -gt 1 ]]; do
        line="$(ps -o pid=,ppid=,comm=,args= -p "${pid}")"
        chain+="${line}"$'\n'
        pid="$(ps -o ppid= -p "${pid}" | tr -d ' ')"
    done
    block "parent process chain of this run (pid ppid comm args)" "${chain}"
}

probe_processes() {
    local name bin pid rest
    while read -r pid rest; do
        run "terminal ${rest} pid ${pid} (pid, start, elapsed s, args)" ps -o pid=,lstart=,etimes=,args= -p "${pid}"
    done < <(terminal_pids)
    run "boot time" uptime -s
    for bin in ptyxis gnome-terminal kitty ghostty alacritty foot code; do
        if command -v "${bin}" >/dev/null; then
            run "${bin} --version" "${bin}" --version
        else
            fact "- ${bin}: not on PATH"
        fi
    done
    run "VTE and terminal packages (rpm -q)" rpm -q vte291 vte291-gtk4 vte-profile ptyxis gnome-terminal kitty ghostty alacritty foot code
    run "flatpak terminals" flatpak list --app --columns=application,version,origin
    for name in vte291 vte291-gtk4 ptyxis kitty ghostty; do
        run "rpm -q --last ${name} (install time)" rpm -q --last "${name}"
    done
}

probe_fonts() {
    local pkg dir
    local -a pkgs
    readarray -t pkgs < <(rpm -qa --qf '%{NAME}\n' '*emoji*' '*twemoji*' | sort -u)
    block "installed emoji font packages" "${pkgs[*]:-(none)}"
    for pkg in "${pkgs[@]}"; do
        run "rpm -q --last ${pkg}" rpm -q --last "${pkg}"
        run "rpm -ql ${pkg}" rpm -ql "${pkg}"
        run "rpm -V ${pkg} (empty = files match the package)" rpm -V "${pkg}"
        run "rpm -q --changelog ${pkg} (top)" awk 'NR<=12' <(rpm -q --changelog "${pkg}")
    done
    run "dnf history of emoji font packages" dnf history list google-noto-color-emoji-fonts
    for dir in "${HOME}/.local/share/fonts" "${HOME}/.fonts" /usr/local/share/fonts; do
        if [[ -d "${dir}" ]]; then
            run "font files under ${dir} (mtime, size, path)" find "${dir}" -type f -printf '%TY-%Tm-%Td %TH:%TM %s %p\n'
        else
            fact "- ${dir}: absent"
        fi
    done
    run "emoji font files system-wide (mtime, size, path)" find /usr/share/fonts -type f -iname '*emoji*' -printf '%TY-%Tm-%Td %TH:%TM %s %p\n'
}

probe_fontconfig() {
    local cp
    need fc-match
    run "fc-match --version" fc-match --version
    run "fc-match -s emoji (first 15 of the fallback chain)" awk 'NR<=15' <(fc-match -s -f '%{file} | %{family} | %{fontversion}\n' emoji)
    run "fc-match -s monospace (first 15)" awk 'NR<=15' <(fc-match -s -f '%{file} | %{family}\n' monospace)
    for cp in "${CODEPOINTS[@]}"; do
        run "fc-match monospace:charset=${cp}" fc-match -f '%{file} | %{family} | %{fontversion}\n' "monospace:charset=${cp}"
        run "fc-match emoji:charset=${cp}" fc-match -f '%{file} | %{family} | %{fontversion}\n' "emoji:charset=${cp}"
        run "fc-list :charset=${cp}" fc-list -f '%{file} | %{family} | %{fontversion} | color=%{color}\n' ":charset=${cp}"
    done
    run "fc-list :color=true" fc-list -f '%{file} | %{family} | %{fontformat}\n' ':color=true'
    run "fontconfig files that mention emoji (system and user)" grep -rils emoji /etc/fonts /usr/share/fontconfig "${HOME}/.config/fontconfig"
    run "user fontconfig directory" find "${HOME}/.config/fontconfig" -type f -printf '%TY-%Tm-%Td %TH:%TM %p\n'
    run "user fonts.conf" cat "${HOME}/.config/fontconfig/fonts.conf"
}

probe_cache() {
    need fc-cache
    run "fc-cache --version" fc-cache --version
    run "emoji font files (mtime)" find /usr/share/fonts -type f -iname '*emoji*' -printf '%TY-%Tm-%Td %TH:%TM:%TS %p\n'
    run "system fontconfig caches (mtime, path)" find /usr/lib/fontconfig/cache /var/cache/fontconfig -type f -printf '%TY-%Tm-%Td %TH:%TM:%TS %p\n'
    run "user fontconfig caches (mtime, path)" find "${HOME}/.cache/fontconfig" -type f -printf '%TY-%Tm-%Td %TH:%TM:%TS %p\n'
    run "font directories (mtime; a cache older than its directory is stale)" find /usr/share/fonts -maxdepth 1 -type d -iname '*emoji*' -printf '%TY-%Tm-%Td %TH:%TM:%TS %p\n'
}

probe_mapped() {
    local name pid found=0 maps
    while read -r pid name; do
        found=1
        if maps="$(grep -iE 'emoji|\.(ttf|otf|ttc)' "/proc/${pid}/maps")"; then
            block "${name} pid ${pid}: mapped font files (unique; (deleted) = replaced under it)" \
                "$(awk '{ $1=$2=$3=$4=$5=""; print }' <<<"${maps}" | sort -u)"
        else
            fact "- ${name} pid ${pid}: no font file mapped"
        fi
    done < <(terminal_pids)
    if [[ "${found}" -eq 0 ]]; then
        printf '[ERROR] no running terminal emulator found (%s); run this from the terminal that shows the bug\n' "${TERMINALS[*]}" >&2
        return 1
    fi
}

probe_settings() {
    local file
    for file in "${HOME}/.config/kitty/kitty.conf" "${HOME}/.config/ghostty/config" \
        "${HOME}/.config/alacritty/alacritty.toml" "${HOME}/.config/foot/foot.ini"; do
        if [[ -f "${file}" ]]; then
            run "${file}: font and renderer lines" grep -inE 'font|emoji|symbol_map|narrow_symbols|renderer|gpu|linux_display_server|grapheme' "${file}"
        else
            fact "- ${file}: absent"
        fi
    done
    run "dconf /org/gnome/Ptyxis/ (profiles and fonts)" dconf dump /org/gnome/Ptyxis/
    run "dconf /org/gnome/terminal/legacy/profiles:/ (fonts)" dconf dump /org/gnome/terminal/legacy/profiles:/
    run "gsettings monospace-font-name" gsettings get org.gnome.desktop.interface monospace-font-name
    run "gsettings font-antialiasing and font-hinting" gsettings list-recursively org.gnome.desktop.interface
    run "VS Code terminal font and GPU settings" grep -iE 'terminal\.integrated\.(font|gpu|rendererType)|editor\.fontFamily' "${HOME}/.config/Code/User/settings.json"
    run "GPU (lspci display controllers)" lspci -nnk -d ::0300
    run "GPU (lspci 3D controllers)" lspci -nnk -d ::0302
    run "Mesa and GTK packages" rpm -q mesa-dri-drivers gtk4 gtk3 harfbuzz freetype cairo pango
}

case "${probe}" in
    terminal) probe_terminal ;;
    processes) probe_processes ;;
    fonts) probe_fonts ;;
    fontconfig) probe_fontconfig ;;
    cache) probe_cache ;;
    mapped) probe_mapped ;;
    settings) probe_settings ;;
    *)
        printf '[FATAL] unknown probe: %s\n' "${probe}" >&2
        exit 64
        ;;
esac
