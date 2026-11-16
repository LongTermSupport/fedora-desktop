#!/usr/bin/env bash
# test-raw-prune.bash — raw-prune (files/home/.local/bin/raw-prune) deletes only true orphans.
#
# raw-prune deletes every RAW whose filename stem has no JPG anywhere under the folder, so a JPG
# it fails to see is a RAW it deletes. Driven over scratch trees with synthetic files:
#   - stems match case-insensitively and across folders, for every extension spelling;
#   - --dry-run lists only the orphans and deletes nothing; --yes deletes only the orphans;
#   - a folder find cannot read (it may hold a RAW's JPG) stops the run before any delete
#     (fedora-desktop#295); skipped as root, who reads every folder.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOL="${REPO_ROOT}/files/home/.local/bin/raw-prune"
TMPROOT="$(mktemp -d)"
cleanup() { chmod -R u+rwx "${TMPROOT}"; rm -rf "${TMPROOT}"; }
trap cleanup EXIT
PASSED=0
FAILED=0

check() {
    if [[ "$2" == "$3" ]]; then
        printf 'PASS: %s\n' "$1"
        PASSED=$((PASSED + 1))
    else
        printf 'FAIL: %s\n  expected: %s\n  got:      %s\n' "$1" "$3" "$2"
        FAILED=$((FAILED + 1))
    fi
}

# A tree: RAWs matched by a JPG of any case in another folder, and two orphans.
make_tree() {
    local tree="$1"
    mkdir -p "${tree}/raw" "${tree}/jpg/deeper"
    touch "${tree}/raw/IMG_1.CR2" "${tree}/jpg/img_1.jpg" \
        "${tree}/raw/Pic2.Nef" "${tree}/jpg/deeper/PIC2.JPEG" \
        "${tree}/raw/orphan3.dng" "${tree}/raw/ORPHAN4.ARW" "${tree}/raw/notes.txt"
}
remaining() { (cd "$1" && find . -type f | LC_ALL=C sort | tr '\n' ' '); }
lines_naming() { awk -v pat="$1" '$0 ~ pat { n++ } END { print n + 0 }'; }

TREE="${TMPROOT}/dry"
make_tree "${TREE}"
out="$(bash "${TOOL}" --dry-run "${TREE}" 2>&1)"
check "dry run lists both orphans" "$(lines_naming 'orphan3[.]dng|ORPHAN4[.]ARW' <<<"${out}")" 2
check "dry run lists no matched RAW" "$(lines_naming 'IMG_1[.]CR2|Pic2[.]Nef' <<<"${out}")" 0
check "dry run deletes nothing" "$(remaining "${TREE}")" \
    "./jpg/deeper/PIC2.JPEG ./jpg/img_1.jpg ./raw/IMG_1.CR2 ./raw/ORPHAN4.ARW ./raw/Pic2.Nef ./raw/notes.txt ./raw/orphan3.dng "

TREE="${TMPROOT}/yes"
make_tree "${TREE}"
bash "${TOOL}" --yes "${TREE}" >"${TMPROOT}/yes.out" 2>&1
check "--yes deletes only the orphans" "$(remaining "${TREE}")" \
    "./jpg/deeper/PIC2.JPEG ./jpg/img_1.jpg ./raw/IMG_1.CR2 ./raw/Pic2.Nef ./raw/notes.txt "

if [[ "$(id -u)" -eq 0 ]]; then
    printf 'SKIP: an unreadable folder (root reads every folder)\n'
else
    TREE="${TMPROOT}/unreadable"
    make_tree "${TREE}"
    mkdir -p "${TREE}/locked"
    touch "${TREE}/locked/orphan3.jpg" "${TREE}/locked/ORPHAN4.jpg"
    chmod 000 "${TREE}/locked"
    rc=0
    bash "${TOOL}" --yes "${TREE}" >"${TMPROOT}/unreadable.out" 2>&1 || rc=$?
    chmod 700 "${TREE}/locked"
    check "an unreadable folder stops the run" "$([[ ${rc} -ne 0 ]] && echo refused || echo "ran (status ${rc})")" refused
    check "an unreadable folder deletes nothing" "$(remaining "${TREE}")" \
        "./jpg/deeper/PIC2.JPEG ./jpg/img_1.jpg ./locked/ORPHAN4.jpg ./locked/orphan3.jpg ./raw/IMG_1.CR2 ./raw/ORPHAN4.ARW ./raw/Pic2.Nef ./raw/notes.txt ./raw/orphan3.dng "
fi

printf '\npassed: %d   failed: %d\n' "${PASSED}" "${FAILED}"
if [[ "${FAILED}" -gt 0 ]]; then
    printf 'test-raw-prune: FAILED\n'
    exit 1
fi
printf 'test-raw-prune: PASSED\n'
