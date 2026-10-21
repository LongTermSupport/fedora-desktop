#!/usr/bin/env bash
# Plan 00210 — check-convert.bash: one acceptance check of the INSTALLED convert, run as a leg
# of acceptance.bash. It works only in WORKDIR, on a copy of the fixture.
#
#   list    `convert notes.md` lists docx
#   refuse  `convert notes.md --to pdf` exits 64 and names docx
#   docx    `convert notes.md --to docx` writes notes.docx, which pandoc reads back with every
#           element of the fixture: headings, bullet and numbered lists, a table, bold,
#           italic and the code block (read as pandoc's JSON syntax tree, so each element is
#           checked by its type, not by how a writer happens to print it)
#
# Usage: check-convert.bash list|refuse|docx FIXTURE WORKDIR
set -euo pipefail

[[ $# -eq 3 ]] || {
    printf 'usage: check-convert.bash list|refuse|docx FIXTURE WORKDIR\n' >&2
    exit 64
}
check="$1"
fixture="$2"
workDir="$3"
# By full path: /usr/bin/convert is ImageMagick's, so PATH order must not decide which runs.
convertBin="${HOME}/.local/bin/convert"

[[ -x "${convertBin}" ]] || {
    printf 'FAIL: %s is not installed; run playbooks/imports/optional/common/play-cli-tools.yml\n' "${convertBin}" >&2
    exit 1
}
mkdir -p "${workDir}"
cp "${fixture}" "${workDir}/notes.md"
cd "${workDir}"

case "${check}" in
list)
    listing="$("${convertBin}" notes.md)"
    printf '%s\n' "${listing}"
    [[ "${listing}" == *"docx  "* ]] || {
        printf 'FAIL: the listing does not offer docx\n' >&2
        exit 1
    }
    ;;
refuse)
    rc=0
    errText="$("${convertBin}" notes.md --to pdf 2>&1)" || rc=$?
    printf 'exit %d: %s\n' "${rc}" "${errText}"
    [[ "${rc}" -eq 64 && "${errText}" == *"it can become: docx"* ]] || {
        printf 'FAIL: want exit 64 naming docx\n' >&2
        exit 1
    }
    ;;
docx)
    rm -f notes.docx
    written="$("${convertBin}" notes.md --to docx)"
    [[ "${written}" == "notes.docx" && -s notes.docx ]] || {
        printf 'FAIL: convert printed %q and notes.docx is missing or empty\n' "${written}" >&2
        exit 1
    }
    types="$(pandoc --from=docx --to=json notes.docx | jq -r '[.. | objects | .t? | strings] | unique | join(" ")')"
    printf 'element types read back: %s\n' "${types}"
    missing=()
    for want in Header BulletList OrderedList Table Strong Emph CodeBlock; do
        [[ " ${types} " == *" ${want} "* ]] || missing+=("${want}")
    done
    plain="$(pandoc --from=docx --to=plain notes.docx)"
    for text in "Fixture Heading One" "Second Level Heading" "alpha item" "second step" "Cell B2" "strong words" "leaning words" "echo fixture-code"; do
        [[ "${plain}" == *"${text}"* ]] || missing+=("text '${text}'")
    done
    [[ ${#missing[@]} -eq 0 ]] || {
        printf 'FAIL: not preserved in the DOCX: %s\n' "${missing[*]}" >&2
        exit 1
    }
    printf 'every fixture element survived the conversion\n'
    ;;
*)
    printf 'unknown check %s\n' "${check}" >&2
    exit 64
    ;;
esac
