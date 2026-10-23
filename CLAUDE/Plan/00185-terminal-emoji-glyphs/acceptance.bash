#!/usr/bin/env bash
# Plan 00185 — acceptance.bash (Task 2.2)
#
# PURPOSE: render the VERDICT on what deploy.bash left behind (CLAUDE/PlanScriptStandards.md
# R9): fontconfig gives Noto Color Emoji for the emoji of #60 under each generic family and
# under the named text font a terminal asks for, both as the single best match (fc-match)
# and in the ordered list (fc-match -a, what Pango and so every GTK terminal walks), while
# the digits, '#', '*' and the space stay on the text font. HOST ONLY, as the desktop user;
# no sudo, and it changes nothing.
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

for tool in fc-match fc-list cmp; do
    if ! command -v "${tool}" >/dev/null; then
        printf '[FATAL] %s not found: fontconfig comes from play-fonts.yml; deploy it, do not install by hand\n' "${tool}" >&2
        exit 1
    fi
done

readonly DECLARED=12
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
# family_for <request> — the family fontconfig's single best match picks.
family_for() {
    fc-match -f '%{family[0]}' "$1"
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
# The first of the emoji-capable fonts in that list must be the colour one.
first_emoji_font=""
while IFS= read -r family; do
    case "${family}" in
        "${COLOUR}" | Symbola | "Noto Emoji" | "Noto Sans Symbols 2")
            first_emoji_font="${family}"
            break
            ;;
    esac
done <<<"${ordered}"
verdict "12. ${COLOUR} comes before Symbola, Noto Emoji and Noto Sans Symbols 2" "${COLOUR}" "${first_emoji_font:-none of them}"

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
