#!/usr/bin/env bash
# The colour emoji fallback that play-fonts.yml deploys (Plan 00185, issue #60).
#
# For a character its own font lacks, a program takes another font from fontconfig, either
# the first that has it in the ordered list (FcFontSort, as Pango and so every GTK terminal
# does) or the single best match for the character (FcFontMatch, as fc-match does). On the
# host both gave Symbola or the monochrome Noto Emoji for every emoji, never Noto Color
# Emoji. Neither generic family lists the colour font, and in the single best match a font
# supporting the language of the locale (Symbola has Latin letters, so "en") ranks above any
# weak family entry while Noto Color Emoji has no letters. A strong entry would rank above
# the language, but would draw the digits, the hash and the asterisk from the emoji font (it
# has them for the keycaps). The deployed file adds the font WEAKLY after each generic
# family, which orders the list, and records "en" for it when fontconfig scans it, which
# settles the single best match; both paths, and the digits, are checked here.
#
# The fonts are fixtures, made here as BDF text files, so the result depends on nothing the
# machine has installed: a text font for each generic family, a terminal font with no
# generic family of its own (Source Code Pro on the host, which reaches sans-serif through
# 49-sansserif.conf), a Symbola stand-in with letters and the emoji, a monochrome emoji
# font, and a font called Noto Color Emoji with digits and the emoji but no letters.
# fontconfig runs with a sealed config naming only them, the rules a distribution provides
# in the order Fedora has them, and the real deployed file, read where it is in the repo.
#
# Each rule in the file is load-bearing here: removing any one turns a check red. Fonts that
# tie are ordered by scan order, so the file of the Symbola stand-in sorts first and a tie
# goes to it, never to the colour font by accident of a file name.
#
# Each query is checked twice: without the file (it must reproduce the defect, or the
# fixture proves nothing) and with it. Needs fc-cache, fc-match and fc-list (fontconfig).
#
# `set -e` is deliberately NOT used: every case runs so the summary is complete, and each
# result is checked explicitly.
set -uo pipefail

# The single best match depends on the language: run in English whatever the locale of the
# caller, since the file levels the languages for "en" only.
export FC_LANG=en

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
# Text symbols (heavy check mark, sun) that the colour font also has, as on the host.
SYMBOLS=(2714 2600)

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
# The file names set the scan order, and so how ties fall: the symbol fonts first.
write_bdf "$work/fonts/a-symbols.bdf" "Symbola" "${LATIN[@]}" "${EMOJI[@]}" "${SYMBOLS[@]}"
write_bdf "$work/fonts/b-mono-emoji.bdf" "Noto Emoji" "${KEYCAP_BASES[@]}" "${EMOJI[@]}"
write_bdf "$work/fonts/m-mono.bdf" "Fixture Mono" "${LATIN[@]}"
write_bdf "$work/fonts/m-sans.bdf" "Fixture Sans" "${LATIN[@]}" "${SYMBOLS[@]}"
write_bdf "$work/fonts/m-serif.bdf" "Fixture Serif" "${LATIN[@]}"
write_bdf "$work/fonts/m-code.bdf" "Fixture Code" "${LATIN[@]}"
write_bdf "$work/fonts/m-late.bdf" "Fixture Late" "${LATIN[@]}"
write_bdf "$work/fonts/z-colour-emoji.bdf" "Noto Color Emoji" "${KEYCAP_BASES[@]}" "${EMOJI[@]}" "${SYMBOLS[@]}"

# sealed_config <name> [include] — a config naming only the fixtures, with the rules of a
# distribution in the order Fedora has them: each text font gets its generic family as a
# default (45-latin.conf), a font with none gets sans-serif (49-sansserif.conf), each generic
# family prefers its text font (60-latin.conf), serif prefers Symbola (65-gdouros-symbola.conf,
# gdouros-symbola-fonts), then the file under test at 69, then a later
# file preferring one more text font for each family (as 69-unifont.conf does). Each config has
# its own cache.
sealed_config() {
    local name="$1" include="${2:-}" pair generic
    mkdir -p "$work/$name"
    {
        printf '<?xml version="1.0"?>\n<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">\n<fontconfig>\n'
        printf '<dir>%s/fonts</dir>\n<cachedir>%s/%s/cache</cachedir>\n' "$work" "$work" "$name"
        for pair in "Fixture Mono=monospace" "Fixture Sans=sans-serif" "Fixture Serif=serif"; do
            printf '<alias><family>%s</family><default><family>%s</family></default></alias>\n' "${pair%%=*}" "${pair#*=}"
        done
        printf '<match target="pattern">'
        for generic in sans-serif serif monospace; do
            printf '<test qual="all" name="family" compare="not_eq"><string>%s</string></test>' "$generic"
        done
        printf '<edit name="family" mode="append_last"><string>sans-serif</string></edit></match>\n'
        for pair in "Fixture Mono=monospace" "Fixture Sans=sans-serif" "Fixture Serif=serif"; do
            printf '<alias><family>%s</family><prefer><family>%s</family></prefer></alias>\n' "${pair#*=}" "${pair%%=*}"
        done
        # 65-gdouros-symbola.conf, as Fedora's gdouros-symbola-fonts ships it.
        printf '<alias><family>serif</family><prefer><family>Symbola</family></prefer></alias>\n'
        printf '<alias><family>Symbola</family><default><family>serif</family></default></alias>\n'
        if [ -n "$include" ]; then printf '<include ignore_missing="no">%s</include>\n' "$include"; fi
        for generic in monospace sans-serif serif; do
            printf '<alias><family>%s</family><prefer><family>Fixture Late</family></prefer></alias>\n' "$generic"
        done
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
# first_of <config> <request> <family>... — whichever of the families comes first in the
# ordered list for the request, which is what a GTK terminal walks for a missing character.
first_of() {
    local config="$1" request="$2" line wanted
    shift 2
    while IFS= read -r line; do
        for wanted in "$@"; do
            if [ "$line" = "$wanted" ]; then
                printf '%s' "$line"
                return
            fi
        done
    done < <(FONTCONFIG_FILE="$work/$config/fonts.conf" fc-match -a -f '%{family[0]}\n' "$request")
    printf 'none of them'
}

fixture_count="$(FONTCONFIG_FILE="$work/with/fonts.conf" fc-list -f '%{family[0]}\n' | grep -c .)"
check "fontconfig indexes the eight fixture fonts" 8 "$fixture_count"

# The colour font goes after the generic name, so a text font a later file prefers for the
# family still comes before it.
for request in monospace "sans\\-serif" serif "Fixture Code"; do
    check "with the file, the ordered list for $request has the later preferred font before the colour font" \
        "Fixture Late" "$(first_of with "$request" "Fixture Late" "Noto Color Emoji")"
done

colour_langs="$(FONTCONFIG_FILE="$work/with/fonts.conf" fc-list -f '%{lang}' 'Noto Color Emoji')"
case "|${colour_langs//,/|}|" in
    *"|en|"*) check "the scan records en for Noto Color Emoji" yes yes ;;
    *) check "the scan records en for Noto Color Emoji" "a langset with en" "$colour_langs" ;;
esac

# Each generic family, its text font, and the terminal font that has no generic family, as
# "request=text font". A request is fontconfig name syntax, where an unescaped dash starts
# the size: "sans-serif" would be read as the family "sans", so it is written escaped.
REQUESTS=("monospace=Fixture Mono" "sans\\-serif=Fixture Sans" "serif=Fixture Serif"
    "Fixture Mono=Fixture Mono" "Fixture Code=Fixture Code")
EMOJI_FONTS=("Noto Color Emoji" "Symbola" "Noto Emoji")

for pair in "${REQUESTS[@]}"; do
    request="${pair%%=*}"
    text="${pair#*=}"
    # The single best match: the defect without the file, the colour font with it.
    for cp in "${EMOJI[@]}"; do
        check "without the file, $request:charset=$cp" "Symbola" "$(family_for without "$request:charset=$cp")"
        check "with the file, $request:charset=$cp" "Noto Color Emoji" "$(family_for with "$request:charset=$cp")"
    done
    # The ordered list: it starts with the text font; the colour font comes first of the
    # emoji-capable fonts only with the file.
    check "with the file, the ordered list for $request starts with $text" "$text" \
        "$(first_of with "$request" "$text" "${EMOJI_FONTS[@]}")"
    check "without the file, the ordered list for $request puts a monochrome font first" "Symbola" \
        "$(first_of without "$request" "${EMOJI_FONTS[@]}")"
    check "with the file, the ordered list for $request puts the colour font first" "Noto Color Emoji" \
        "$(first_of with "$request" "${EMOJI_FONTS[@]}")"
    # The guard: the digits, the hash, the asterisk and the space stay on the text font.
    for cp in 30 39 23 2a 20; do
        check "with the file, $request:charset=$cp stays on $text" "$text" "$(family_for with "$request:charset=$cp")"
    done
done

# A text symbol the terminal font lacks still comes from the text font of the sans-serif
# list it reaches, not from the colour font, in the single best match and the ordered list.
for cp in "${SYMBOLS[@]}"; do
    check "with the file, Fixture Code:charset=$cp stays on Fixture Sans" "Fixture Sans" \
        "$(family_for with "Fixture Code:charset=$cp")"
done
check "with the file, the ordered list for Fixture Code has Fixture Sans before the colour font" "Fixture Sans" \
    "$(first_of with "Fixture Code" "Fixture Sans" "Noto Color Emoji" "Symbola")"

# Fedora's 65-gdouros-symbola.conf prefers Symbola for serif, and the file moves it to the end
# of the serif list; a request for Symbola by name is left alone.
for cp in "${EMOJI[@]}"; do
    check "with the file, Symbola:charset=$cp stays on Symbola" "Symbola" "$(family_for with "Symbola:charset=$cp")"
done

echo "passed: $passed"
echo "failed: $failed"
[ "$failed" -eq 0 ]
