#!/usr/bin/env bash
# Plan 00185 — acceptance.bash (Task 2.2)
#
# PURPOSE: render the VERDICT on what deploy.bash left behind (CLAUDE/PlanScriptStandards.md
# R9): fontconfig gives Noto Color Emoji for the emoji of #60 under each generic family and
# under the named text font a terminal asks for, both as the single best match (fc-match)
# and in the ordered list (fc-match -a, what Pango and so every GTK terminal walks), while
# the digits, '#', '*' and the space stay on the text font. The same for the font Ptyxis is
# set to (read from its settings), and common text symbols stay on a text font there, and the
# system font caches are readable by every user. HOST
# ONLY, as the desktop user; no sudo, and it changes nothing.
#
# NOT ESTABLISHABLE by a script, and left as a named open item for Plan 00219's desktop
# check: the issue's printf drawing a line graph, a spool of thread and a package in a new
# terminal window.
#
# EXIT STATUS: 0 ACCEPTED, every declared check ran and passed; 1 REJECTED; 64 usage.
#
# Usage: ./acceptance.bash [-h|--help]
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
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

PLAN_USAGE="usage: acceptance.bash [-h|--help]

The Plan 00185 acceptance gate. Run deploy.bash first. It checks the deployed fallback file,
the colour emoji font, and what fontconfig picks for the emoji of #60 and for digits.

EXIT STATUS
  0  ACCEPTED — every declared check ran and passed
  1  REJECTED — a check failed, or a declared check never ran
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it asks this desktop's fontconfig which font draws each character"
plan_start_log auto

for tool in fc-match fc-list cmp gsettings grep find; do
    if ! command -v "${tool}" >/dev/null; then
        printf '[FATAL] %s not found: fontconfig comes from play-fonts.yml and gsettings from GLib; deploy, do not install by hand\n' "${tool}" >&2
        exit 1
    fi
done

readonly DECLARED=19
readonly COLOUR="Noto Color Emoji"
readonly DEPLOYED=/etc/fonts/conf.d/69-colour-emoji-fallback.conf
readonly SOURCE="${PLAN_REPO_ROOT}/files/etc/fonts/conf.d/69-colour-emoji-fallback.conf"
# The issue's code points (chart up, bar chart, thread, package) and the hammer it drew.
readonly EMOJI=(1f4c8 1f4ca 1f9f5 1f4e6 1f528)
# The keycap bases Noto Color Emoji carries: digits, '#', '*' and the space.
readonly KEYCAP_BASES=(30 39 23 2a 20)
PASS=0
FAIL=0

ok() {
    PASS=$((PASS + 1))
    printf '✓ %s\n' "$1"
}
bad() {
    FAIL=$((FAIL + 1))
    printf '✗ %s\n' "$1"
    if [[ -n "${2:-}" ]]; then
        printf '    %s\n' "$2"
    fi
}
verdict() {
    if [[ "$2" == "$3" ]]; then
        ok "$1"
    else
        bad "$1" "want: $2 | got: $3"
    fi
}
# family_for <request> — the family fontconfig's single best match picks. A failing fc-match
# becomes the value a check rejects, so the verdict lines are still printed.
family_for() {
    fc-match -f '%{family[0]}' "$1" || printf 'fc-match failed (exit %s)' "$?"
}
# escaped <family> — the family in fontconfig name syntax, where '-' would start the size.
escaped() {
    local family="$1"
    family="${family//\\/\\\\}"
    family="${family//-/\\-}"
    family="${family//:/\\:}"
    printf '%s' "${family//,/\\,}"
}
# emoji_misses <request> — each emoji whose single best match is not the colour font.
emoji_misses() {
    local cp got misses=""
    for cp in "${EMOJI[@]}"; do
        got="$(family_for "$1:charset=${cp}")"
        if [[ "${got}" != "${COLOUR}" ]]; then misses+="U+${cp^^}=${got}; "; fi
    done
    printf '%s' "${misses}"
}
# keycap_misses <request> <text font> — each keycap base not drawn from the text font.
keycap_misses() {
    local cp got misses=""
    for cp in "${KEYCAP_BASES[@]}"; do
        got="$(family_for "$1:charset=${cp}")"
        if [[ "${got}" != "$2" ]]; then misses+="U+${cp^^}=${got}; "; fi
    done
    printf '%s' "${misses}"
}

echo "== the deployed file and the font"
if [[ ! -f "${DEPLOYED}" ]]; then
    bad "1. ${DEPLOYED} is deployed" "missing: run deploy.bash"
elif cmp -s "${SOURCE}" "${DEPLOYED}"; then
    ok "1. ${DEPLOYED} is the repo's file"
else
    bad "1. ${DEPLOYED} is the repo's file" "it differs from ${SOURCE}: run deploy.bash"
fi
colour_fonts="$(fc-list -f '%{color}\n' "${COLOUR}")"
verdict "2. ${COLOUR} is installed and is a colour font" "True" "${colour_fonts%%$'\n'*}"
colour_langs="$(fc-list -f '%{lang}' "${COLOUR}")"
case "|${colour_langs//,/|}|" in
    *"|en|"*) ok "3. the font cache records en for ${COLOUR} (the scan edit applied)" ;;
    *) bad "3. the font cache records en for ${COLOUR} (the scan edit applied)" \
        "got lang '${colour_langs}': the caches predate the file; deploy.bash rebuilds them" ;;
esac

echo "== the single best match for the emoji of #60"
verdict "4. monospace gives ${COLOUR} for every one" "" "$(emoji_misses monospace)"
verdict "5. sans-serif gives ${COLOUR} for every one" "" "$(emoji_misses 'sans\-serif')"
verdict "6. serif gives ${COLOUR} for every one" "" "$(emoji_misses serif)"
mono_text="$(family_for monospace)"
sans_text="$(family_for 'sans\-serif')"
verdict "7. the named monospace text font (${mono_text}) gives ${COLOUR} for every one" "" \
    "$(emoji_misses "$(escaped "${mono_text}")")"

echo "== digits, '#', '*' and the space stay on the text font"
verdict "8. monospace keeps them on ${mono_text}" "" "$(keycap_misses monospace "${mono_text}")"
verdict "9. sans-serif keeps them on ${sans_text}" "" "$(keycap_misses 'sans\-serif' "${sans_text}")"
verdict "10. ${mono_text} keeps them on itself" "" "$(keycap_misses "$(escaped "${mono_text}")" "${mono_text}")"

echo "== the ordered list a GTK terminal walks, for the monospace text font"
ordered="$(fc-match -a -f '%{family[0]}\n' "$(escaped "${mono_text}")")"
verdict "11. it starts with ${mono_text}" "${mono_text}" "${ordered%%$'\n'*}"
# first_emoji_in <ordered list> — the first of the emoji-capable fonts in it, which must be the
# colour one.
first_emoji_in() {
    local family
    while IFS= read -r family; do
        case "${family}" in
            "${COLOUR}" | Symbola | "Noto Emoji" | "Noto Sans Symbols 2")
                printf '%s' "${family}"
                return
                ;;
        esac
    done <<<"$1"
    printf 'none of them'
}
verdict "12. ${COLOUR} comes before Symbola, Noto Emoji and Noto Sans Symbols 2" "${COLOUR}" "$(first_emoji_in "${ordered}")"
# serif's own list: Fedora's 65-gdouros-symbola.conf prefers Symbola for serif, and the file
# moves it after the colour font. Check 6 is the single best match; this is the list a
# program asking for serif walks.
verdict "19. serif's ordered list has ${COLOUR} before Symbola, Noto Emoji and Noto Sans Symbols 2" "${COLOUR}" \
    "$(first_emoji_in "$(fc-match -a -f '%{family[0]}\n' serif)")"

echo "== the font the terminal (Ptyxis) is set to, which reaches the fallback by its own route"
# A font with no generic family of its own (Source Code Pro is in neither 45-latin.conf nor
# 60-latin.conf) gets sans-serif from 49-sansserif.conf, so it takes the sans-serif rule.
terminal_font=""
# A missing schema or key is a failed check 13 with the reason, not an abort.
if ! use_system_font="$(gsettings get org.gnome.Ptyxis use-system-font 2>&1)"; then
    terminal_setting="gsettings failed: ${use_system_font}"
elif [[ "${use_system_font}" == "true" ]]; then
    terminal_setting="$(gsettings get org.gnome.desktop.interface monospace-font-name 2>&1)" ||
        terminal_setting="gsettings failed: ${terminal_setting}"
else
    terminal_setting="$(gsettings get org.gnome.Ptyxis font-name 2>&1)" ||
        terminal_setting="gsettings failed: ${terminal_setting}"
fi
terminal_setting="${terminal_setting#\'}"
terminal_setting="${terminal_setting%\'}"
# A Pango font description ends in the size: "Source Code Pro 11".
if [[ "${terminal_setting}" =~ ^(.+)[[:space:]]+[0-9]+([.][0-9]+)?$ ]]; then
    terminal_font="${BASH_REMATCH[1]}"
fi
# first_covering <request> <code point> — the first font in the ordered list for the request
# that has the character: the font a GTK terminal draws it with.
first_covering() {
    local family covering
    covering="$(fc-list -f '%{family[0]}\n' ":charset=$2")"
    while IFS= read -r family; do
        if grep -Fxq -- "${family}" <<<"${covering}"; then
            printf '%s' "${family}"
            return
        fi
    done < <(fc-match -a -f '%{family[0]}\n' "$1")
    printf 'none of them'
}
terminal_request="$(escaped "${terminal_font}")"
# A generic name as the setting ("Monospace 11") stands for the font it resolves to.
case "${terminal_font,,}" in
    monospace | sans | sans-serif | "sans serif" | serif)
        terminal_font="$(family_for "${terminal_request}")"
        terminal_request="$(escaped "${terminal_font}")"
        ;;
esac
if [[ -z "${terminal_font}" ]]; then
    bad "13. the terminal font is read from its settings and installed" "got '${terminal_setting}', not '<family> <size>'"
elif [[ "$(family_for "${terminal_request}")" != "${terminal_font}" ]]; then
    bad "13. the terminal font is read from its settings and installed" \
        "'${terminal_font}' is set, but fontconfig gives '$(family_for "${terminal_request}")' for it"
    terminal_font=""
else
    ok "13. the terminal font is read from its settings and installed: ${terminal_font}"
fi
if [[ -z "${terminal_font}" ]]; then
    for check in "14. the ordered list draws the emoji of #60 from ${COLOUR}" \
        "15. the single best match gives ${COLOUR} for them" \
        "16. the keycap bases stay on the terminal font" \
        "17. U+2714, U+2713 and U+2600 come from a text font"; do
        bad "${check}" "no terminal font to ask about (check 13)"
    done
else
    terminal_misses=""
    for cp in "${EMOJI[@]}"; do
        got="$(first_covering "${terminal_request}" "${cp}")"
        if [[ "${got}" != "${COLOUR}" ]]; then terminal_misses+="U+${cp^^}=${got}; "; fi
    done
    verdict "14. the ordered list for ${terminal_font} draws every emoji of #60 from ${COLOUR}" "" "${terminal_misses}"
    verdict "15. the single best match for ${terminal_font} gives ${COLOUR} for every one" "" \
        "$(emoji_misses "${terminal_request}")"
    verdict "16. ${terminal_font} keeps the keycap bases (digits, hash, asterisk, space) on itself" "" \
        "$(keycap_misses "${terminal_request}" "${terminal_font}")"
    # The known cost: a text symbol that no font in the family list has now comes from the
    # colour font. Check marks and the sun must still come from a text font in that list.
    symbol_misses=""
    for cp in 2714 2713 2600; do
        got="$(first_covering "${terminal_request}" "${cp}")"
        if [[ "${got}" == "${COLOUR}" ]]; then symbol_misses+="U+${cp^^}=${got}; "; fi
    done
    verdict "17. ${terminal_font} draws U+2714, U+2713 and U+2600 from a text font" "" "${symbol_misses}"
    if [[ -n "${symbol_misses}" ]]; then
        printf '    %s\n' "Not a deploy fault: the documented cost of the fallback reaches the terminal font." \
            "The owner decides: accept it, or put a text font with these symbols in the sans-serif list."
    fi
fi

echo "== the system font caches every user reads"
# A rebuild run as root under a restrictive umask leaves them root-only, and fontconfig then
# silently uses each user's stale cache instead (the cause of checks 3 to 7 and 15 failing).
unreadable_caches="$(find /usr/lib/fontconfig/cache -maxdepth 1 -type f ! -perm -0444 -printf '%f %m; ')" ||
    unreadable_caches="find failed (exit $?)"
verdict "18. every file in /usr/lib/fontconfig/cache is readable by every user" "" "${unreadable_caches}"

echo
ran=$((PASS + FAIL))
echo "COVERAGE: ${ran} of ${DECLARED} checks executed"
echo "NOT ESTABLISHABLE here, left for Plan 00219's desktop check:"
printf '%s\n' "  - printf '\\U0001F4C8 \\U0001F9F5 \\U0001F4E6\\n' in a NEW terminal window draws a line"
echo "    graph, a spool of thread and a package, in colour."
if [[ "${ran}" -ne "${DECLARED}" ]]; then
    echo "VERDICT: REJECTED — ${ran} of ${DECLARED} declared checks ran; an incomplete gate establishes nothing."
    PLAN_FAILED_LEGS="coverage"
elif [[ "${FAIL}" -ne 0 ]]; then
    echo "VERDICT: REJECTED — ${FAIL} of ${ran} checks failed."
    PLAN_FAILED_LEGS="acceptance"
else
    echo "VERDICT: ACCEPTED — ${PASS} of ${DECLARED} checks passed."
fi
plan_finish
