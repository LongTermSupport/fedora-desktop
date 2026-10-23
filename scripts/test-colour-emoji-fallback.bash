#!/usr/bin/env bash
# The colour emoji fallback that play-fonts.yml deploys (Plan 00185, issue #60).
#
# For a character its own font lacks, a program takes another font from fontconfig, either
# the first that has it in the ordered list (FcFontSort, as Pango and so every GTK terminal
# does) or the single best match for the character (FcFontMatch, as fc-match does). On the
# host both gave Symbola or the monochrome Noto Emoji for every emoji, never Noto Color
# Emoji. Neither generic family lists the colour font, and in the single best match a font
# supporting the locale's language (Symbola has Latin letters, so "en") ranks above any weak
# family entry while Noto Color Emoji has no letters. A strong entry would rank above the
# language, but would draw the digits, '#' and '*' from the emoji font (it has them for the
# keycaps). The deployed file adds the font WEAKLY after each generic family and records
# "en" for it when fontconfig scans it; both paths, and the digits, are checked here.
#
# The fonts are fixtures, made here as BDF text files, so the result depends on nothing the
# machine has installed: a text font with Latin letters, a Symbola stand-in with letters and
# the emoji, a monochrome emoji font, and a font called Noto Color Emoji with digits and the
# emoji but no letters. fontconfig runs with a sealed config naming only them, the generic
# aliases a distribution provides, and the real deployed file, read where it is in the repo.
#
# Each query is checked twice: without the file (it must reproduce the defect, or the
# fixture proves nothing) and with it. Needs fontconfig's fc-cache, fc-match and fc-list.
#
# `set -e` is deliberately NOT used: every case runs so the summary is complete, and each
# result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CONF="$REPO_ROOT/files/etc/fonts/conf.d/69-colour-emoji-fallback.conf"

for tool in fc-cache fc-match fc-list; do
    if ! command -v "$tool" >/dev/null; then
        echo "ERROR: $tool not found: this suite runs fontconfig itself (the fontconfig package)" >&2
        exit 2
    fi
done
if [ ! -f "$CONF" ]; then
    echo "FAIL: $CONF not found" >&2
    exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/fonts"

# The issue's code points (chart up, bar chart, thread, package) and the hammer it drew.
EMOJI=(1f4c8 1f4ca 1f9f5 1f4e6 1f528)
LATIN=()
for ((cp = 0x20; cp <= 0x7e; cp++)); do LATIN+=("$(printf '%x' "$cp")"); done
for ((cp = 0xa0; cp <= 0xff; cp++)); do LATIN+=("$(printf '%x' "$cp")"); done
KEYCAP_BASES=(20 23 2a 30 31 32 33 34 35 36 37 38 39)

# write_bdf <file> <family> <hex code point>... — a blank 8x8 bitmap font with those characters.
write_bdf() {
    local file="$1" family="$2" cp
    shift 2
    {
        printf 'STARTFONT 2.1\nFONT -fixture-%s-medium-r-normal--8-80-75-75-c-80-iso10646-1\n' "$family"
        printf 'SIZE 8 75 75\nFONTBOUNDINGBOX 8 8 0 0\nSTARTPROPERTIES 5\n'
        printf 'FAMILY_NAME "%s"\nSPACING "C"\nPIXEL_SIZE 8\n' "$family"
        printf 'CHARSET_REGISTRY "ISO10646"\nCHARSET_ENCODING "1"\nENDPROPERTIES\nCHARS %d\n' "$#"
        for cp in "$@"; do
            printf 'STARTCHAR u%s\nENCODING %d\nSWIDTH 500 0\nDWIDTH 8 0\nBBX 8 8 0 0\nBITMAP\n' "$cp" "$((16#$cp))"
            printf '00\n%.0s' 1 2 3 4 5 6 7 8
            printf 'ENDCHAR\n'
        done
        printf 'ENDFONT\n'
    } >"$file"
}
write_bdf "$work/fonts/text.bdf" "Fixture Mono" "${LATIN[@]}"
write_bdf "$work/fonts/symbola.bdf" "Fixture Symbols" "${LATIN[@]}" "${EMOJI[@]}"
write_bdf "$work/fonts/mono-emoji.bdf" "Noto Emoji" "${KEYCAP_BASES[@]}" "${EMOJI[@]}"
write_bdf "$work/fonts/colour-emoji.bdf" "Noto Color Emoji" "${KEYCAP_BASES[@]}" "${EMOJI[@]}"

# sealed_config <name> [include] — a config naming only the fixtures, and each generic family
# preferring the text font as a distribution's own aliases do; the text font falls back to
# monospace as 45-latin.conf has it. Each config has its own cache, built fresh.
sealed_config() {
    local name="$1" include="${2:-}" generic
    mkdir -p "$work/$name"
    {
        printf '<?xml version="1.0"?>\n<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">\n<fontconfig>\n'
        printf '<dir>%s/fonts</dir>\n<cachedir>%s/%s/cache</cachedir>\n' "$work" "$work" "$name"
        for generic in monospace sans-serif serif; do
            printf '<alias><family>%s</family><prefer><family>Fixture Mono</family></prefer></alias>\n' "$generic"
        done
        printf '<alias><family>Fixture Mono</family><default><family>monospace</family></default></alias>\n'
        if [ -n "$include" ]; then printf '<include ignore_missing="no">%s</include>\n' "$include"; fi
        printf '</fontconfig>\n'
    } >"$work/$name/fonts.conf"
    if ! FONTCONFIG_FILE="$work/$name/fonts.conf" fc-cache -f >"$work/$name/fc-cache.log" 2>&1; then
        echo "ERROR: fc-cache failed for the $name config:" >&2
        cat "$work/$name/fc-cache.log" >&2
        exit 2
    fi
}
sealed_config without
sealed_config with "$CONF"

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
    else
        failed=$((failed + 1))
        echo "FAIL: $label: want '$want', got '$got'"
    fi
}
# family_for <config> <pattern> — the family fc-match picks.
family_for() {
    FONTCONFIG_FILE="$work/$1/fonts.conf" fc-match -f '%{family[0]}' "$2"
}

fixture_count="$(FONTCONFIG_FILE="$work/with/fonts.conf" fc-list -f '%{family[0]}\n' | grep -c .)"
check "fontconfig indexes the four fixture fonts" 4 "$fixture_count"

# The defect, reproduced: without the file the English-capable symbol font wins every emoji.
for cp in "${EMOJI[@]}"; do
    check "without the file, monospace:charset=$cp" "Fixture Symbols" "$(family_for without "monospace:charset=$cp")"
done

colour_langs="$(FONTCONFIG_FILE="$work/with/fonts.conf" fc-list -f '%{lang}' 'Noto Color Emoji')"
case "|${colour_langs//,/|}|" in
    *"|en|"*) check "the scan records en for Noto Color Emoji" yes yes ;;
    *) check "the scan records en for Noto Color Emoji" "a langset with en" "$colour_langs" ;;
esac

# The fix: the colour font for every emoji, under each generic family and a named font.
# A request is fontconfig name syntax, where an unescaped '-' starts the size: "sans-serif"
# would be read as the family "sans".
for request in monospace 'sans\-serif' serif "Fixture Mono"; do
    for cp in "${EMOJI[@]}"; do
        check "with the file, $request:charset=$cp" "Noto Color Emoji" "$(family_for with "$request:charset=$cp")"
    done
done

# The guard: digits, '#', '*' and the space still come from the text font.
for request in monospace 'sans\-serif' "Fixture Mono"; do
    for cp in 30 39 23 2a 20; do
        check "with the file, $request:charset=$cp stays on the text font" "Fixture Mono" "$(family_for with "$request:charset=$cp")"
    done
done

# What a GTK terminal does: the whole ordered list for its font, first match per character.
# The text font leads; the colour font comes before the English-capable symbol font only
# with the file, and before the monochrome emoji font.
# first_of <config> <family>... — whichever of the families comes first in the ordered list.
first_of() {
    local config="$1" line wanted
    shift
    while IFS= read -r line; do
        for wanted in "$@"; do
            if [ "$line" = "$wanted" ]; then
                printf '%s' "$line"
                return
            fi
        done
    done < <(FONTCONFIG_FILE="$work/$config/fonts.conf" fc-match -a -f '%{family[0]}\n' 'Fixture Mono')
    printf 'none of them'
}
check "the ordered list starts with the text font" "Fixture Mono" \
    "$(first_of with 'Fixture Mono' 'Noto Color Emoji' 'Fixture Symbols' 'Noto Emoji')"
check "without the file, the symbol font comes before the colour font" "Fixture Symbols" \
    "$(first_of without 'Noto Color Emoji' 'Fixture Symbols')"
check "with the file, the colour font comes before the symbol font" "Noto Color Emoji" \
    "$(first_of with 'Noto Color Emoji' 'Fixture Symbols')"
check "with the file, the colour font comes before the monochrome emoji font" "Noto Color Emoji" \
    "$(first_of with 'Noto Color Emoji' 'Noto Emoji')"

echo "passed: $passed"
echo "failed: $failed"
[ "$failed" -eq 0 ]
