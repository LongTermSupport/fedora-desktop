#!/usr/bin/env bash
#
# Plan 032 — acceptance gate. Renders a VERDICT.
#
# Phase 4 was written as six things for a human to try by hand, and that is why this plan
# has sat "awaiting host deployment" since it was filed. None of the six needs human
# judgement: every one is a filesystem assertion — did this path appear, does it contain
# what went in, did the second run refuse. So they are assertions here, and the plan closes
# on a run rather than on somebody being at a keyboard.
#
# Exercises the DEPLOYED /usr/local/bin/{compress,uncompress}, not the repo copies. A gate
# reading files/usr/local/bin/ would pass on a host where the play never ran.
#
# Writes only inside its own temp directory, which is removed on the way out — including
# on Ctrl-C, because teardown is registered with plan_on_cleanup rather than trapped (R4).
#
# Every wrapper invocation captures stdout AND stderr into a variable and quotes it back in
# the failure message. Discarding it would leave "compress exited non-zero" as the whole
# report, when the wrapper's own `compress: no such file or directory` says which of a
# dozen causes it was.
#
# Usage: acceptance.bash [--help]
# Exit 0 = ACCEPTED, 1 = REJECTED.

set -euo pipefail

# ONE list of this gate's checks. --help and the COVERAGE arithmetic both read it, so a
# check cannot be described in the help text and absent from the coverage arithmetic.
CHECK_CATALOGUE=(
    "0|precondition: ouch is deployed and its version matches the playbook's pin"
    "1|compress and uncompress are deployed, executable, and match the repo copies"
    "2|compress FOLDER produces FOLDER.tar.xz (Phase 4: compress folder to xz)"
    "3|compress --zip FOLDER produces FOLDER.zip (Phase 4: compress folder to zip)"
    "4|compress FILE produces FILE.tar.xz (Phase 4: compress single file)"
    "5|uncompress FOLDER.tar.xz lands in its own folder and floods nothing"
    "6|uncompress of a FLAT zip lands in its own folder (the tarbomb case)"
    "7|both wrappers refuse to overwrite without --force, and destroy nothing"
    "8|PATH resolves compress to /usr/local/bin, not a shadowing /usr/bin copy"
    "9|compress --gz and --7z produce their declared extensions"
    "10|compress with two algorithm flags exits 2 and names both (fail fast)"
)

EXPECTED_CHECKS=()
for entry in "${CHECK_CATALOGUE[@]}"; do
    EXPECTED_CHECKS+=("${entry%%|*}")
done

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 032 — acceptance gate

Usage: acceptance.bash [--help]

Checks, against the DEPLOYED wrappers in /usr/local/bin:
EOF
            for entry in "${CHECK_CATALOGUE[@]}"; do
                printf '  %-4s%s\n' "${entry%%|*}." "${entry#*|}"
            done
            cat << 'EOF'

Checks 2-7 are PLAN.md Phase 4's six "user testing" items, as assertions. They
need no human judgement and never did. Checks 9-10 cover the remaining Success
Criteria about the other algorithm flags and the conflicting-flag hard error.

NOT covered here: playbook idempotency. deploy.bash runs the play twice for
that, because a second converge is a state change and belongs in the deploy
script, not in a read-only gate.

The verdict carries a COVERAGE line counting the declared checks against what
actually ran, so a check that stops executing is visible rather than absorbed
into a lower pass count. An incomplete run is REJECTED even with no failures,
and so is a run that executes a check this list does not name.

Everything is written inside a temp directory that is removed on the way out.

Exit 0 = ACCEPTED, 1 = REJECTED.
EOF
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: acceptance.bash --help" >&2
            exit 1
            ;;
    esac
done

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
plan_mode gather
# R2. Every check below runs the DEPLOYED wrappers and the ouch binary the play installs.
# In the container none of those exist, so the run would not fail — it would report nine
# absences as findings about a host it never touched.
plan_require_host "it runs the deployed /usr/local/bin/compress and uncompress, and the ouch binary the play installs"
plan_start_log auto

readonly PLAY="$PLAN_REPO_ROOT/playbooks/imports/optional/common/play-compression-helpers.yml"
readonly REPO_COMPRESS="$PLAN_REPO_ROOT/files/usr/local/bin/compress"
readonly REPO_UNCOMPRESS="$PLAN_REPO_ROOT/files/usr/local/bin/uncompress"
readonly DEP_COMPRESS="/usr/local/bin/compress"
readonly DEP_UNCOMPRESS="/usr/local/bin/uncompress"

WORK="$(mktemp -d)"
readonly WORK
# Registered as a cleanup AND called on the normal path below, so a run that reaches its
# verdict has already removed the tree rather than relying on the handler, and a run killed
# mid-check still gets it. `rm -rf` on a path already gone is a no-op, so calling it twice
# is safe — which is what makes both routes available.
remove_work_tree() {
    rm -rf -- "$WORK"
    return 0
}
plan_on_cleanup remove_work_tree

RAN_CHECKS=()
PASS=0
FAIL=0

# Announce a check AND record that it ran. Every numbered section starts here; a section
# that prints its own header instead is invisible to COVERAGE.
check() {
    RAN_CHECKS+=("$1")
    echo "[$1] $2"
}
ok() {
    echo "  PASS  $1"
    PASS=$((PASS + 1))
}
bad() {
    echo "  FAIL  $1" >&2
    if [ $# -gt 1 ]; then
        echo "        $2" >&2
    fi
    FAIL=$((FAIL + 1))
}
# A fixture directory with known, non-empty content. Used by checks 2, 3 and 5, so the
# "does what went in come back out" assertion has something to be about.
make_fixture_dir() {
    local dir="$1"
    mkdir -p "$dir/nested"
    printf 'alpha\n' > "$dir/one.txt"
    printf 'beta\n' > "$dir/nested/two.txt"
}

# Run a wrapper in a directory, capturing combined output and the real status.
#
# `if cmd; then rc=0; else rc=$?; fi`, never `if ! cmd; then rc=$?` — the `!` inverts the
# status before `$?` is read, so the second form records 0 for every failure. That idiom
# once made a falsification harness vouch for the very bug it existed to disprove.
RUN_OUT=""
run_in() {
    local dir="$1"
    shift
    local rc
    if RUN_OUT="$(cd "$dir" && "$@" 2>&1)"; then
        rc=0
    else
        rc=$?
    fi
    return "$rc"
}

echo "=============================================================="
echo "Plan 032 acceptance — compress / uncompress"
echo "=============================================================="
echo

# ── 0. the backend ────────────────────────────────────────────────────────────────────────
check 0 "ouch is deployed and its version matches the playbook's pin"
# The pin is READ from the play, never repeated here. A second copy of a version number
# agrees with the first until someone bumps one of them, and then this gate fails for a
# reason that has nothing to do with the host.
pinned_version="$(awk -F'"' '/^ *ouchVersion:/ { print $2 }' "$PLAY")"
ouch_path=""
if [ -z "$pinned_version" ]; then
    bad "could not read ouchVersion from the playbook" "$PLAY"
elif ! ouch_path="$(command -v ouch)"; then
    bad "ouch is not on PATH" "run deploy.bash — the play installs it to /usr/local/bin/ouch"
else
    ouch_version_out="$(ouch --version)"
    if [[ "$ouch_version_out" == *"$pinned_version"* ]]; then
        ok "$ouch_path reports '$ouch_version_out', matching the pinned $pinned_version"
    else
        bad "ouch reports '$ouch_version_out' but the play pins $pinned_version" \
            "the host is running a different build from the one this plan tested"
    fi
fi
echo

# ── 1. the wrappers themselves ────────────────────────────────────────────────────────────
check 1 "compress and uncompress are deployed, executable, and match the repo copies"
wrappers_ok=1
for pair in "$DEP_COMPRESS:$REPO_COMPRESS" "$DEP_UNCOMPRESS:$REPO_UNCOMPRESS"; do
    deployed="${pair%%:*}"
    source_file="${pair#*:}"
    if [ ! -f "$deployed" ]; then
        bad "$deployed is absent" "run deploy.bash"
        wrappers_ok=0
    elif [ ! -x "$deployed" ]; then
        bad "$deployed is not executable"
        wrappers_ok=0
    elif ! cmp -s "$source_file" "$deployed"; then
        bad "$deployed has drifted from $source_file" \
            "the repo has a fix the host never received — run deploy.bash"
        wrappers_ok=0
    else
        ok "$deployed is present, executable, and byte-identical to the repo copy"
    fi
done

# Every check below runs these two scripts. Continuing past their absence would produce
# seven more failures that all say the same thing, burying check 1's one real cause under
# noise — and would let a future reader think seven properties had been tested.
if [ "$wrappers_ok" -ne 1 ]; then
    echo
    echo "==============================================================" >&2
    echo "REJECTED — the deployed wrappers are missing or drifted, so checks 2-10" >&2
    echo "  were NOT ATTEMPTED. They would each have failed for this one reason." >&2
    echo "COVERAGE: ${#RAN_CHECKS[@]} of ${#EXPECTED_CHECKS[@]} checks executed" >&2
    echo "  Run deploy.bash, then re-run this gate." >&2
    echo "==============================================================" >&2
    exit 1
fi
echo

# ── 2. compress a folder, default xz ──────────────────────────────────────────────────────
check 2 "compress FOLDER produces FOLDER.tar.xz"
make_fixture_dir "$WORK/proj"
if run_in "$WORK" "$DEP_COMPRESS" proj; then
    if [ -f "$WORK/proj.tar.xz" ] && [ -s "$WORK/proj.tar.xz" ]; then
        ok "proj.tar.xz created, $(stat -c %s "$WORK/proj.tar.xz") bytes"
    else
        bad "compress exited 0 but proj.tar.xz is missing or empty" "$RUN_OUT"
    fi
else
    bad "compress proj exited non-zero" "$RUN_OUT"
fi
echo

# ── 3. compress a folder, zip ─────────────────────────────────────────────────────────────
check 3 "compress --zip FOLDER produces FOLDER.zip"
make_fixture_dir "$WORK/zipproj"
if run_in "$WORK" "$DEP_COMPRESS" --zip zipproj; then
    if [ -f "$WORK/zipproj.zip" ] && [ -s "$WORK/zipproj.zip" ]; then
        ok "zipproj.zip created, $(stat -c %s "$WORK/zipproj.zip") bytes"
    else
        bad "compress --zip exited 0 but zipproj.zip is missing or empty" "$RUN_OUT"
    fi
else
    bad "compress --zip zipproj exited non-zero" "$RUN_OUT"
fi
echo

# ── 4. compress a single file ─────────────────────────────────────────────────────────────
check 4 "compress FILE produces FILE.tar.xz"
printf 'a single file, compressed\n' > "$WORK/notes.log"
if run_in "$WORK" "$DEP_COMPRESS" notes.log; then
    if [ -f "$WORK/notes.log.tar.xz" ] && [ -s "$WORK/notes.log.tar.xz" ]; then
        ok "notes.log.tar.xz created, $(stat -c %s "$WORK/notes.log.tar.xz") bytes"
    else
        bad "compress exited 0 but notes.log.tar.xz is missing or empty" "$RUN_OUT"
    fi
else
    bad "compress notes.log exited non-zero" "$RUN_OUT"
fi
echo

# ── 5. round-trip the xz archive ──────────────────────────────────────────────────────────
check 5 "uncompress FOLDER.tar.xz lands in its own folder and floods nothing"
mkdir -p "$WORK/x5"
if [ ! -f "$WORK/proj.tar.xz" ]; then
    bad "no proj.tar.xz to extract — check 2 did not produce one" \
        "this check asserts nothing about extraction; it was not run against an archive"
else
    cp "$WORK/proj.tar.xz" "$WORK/x5/"
    # This check establishes the ROUND TRIP — what went in comes back out, intact, under a
    # folder of the archive's name. It deliberately does NOT establish tarbomb protection,
    # and cannot: `compress proj` archives one top-level member called `proj`, so extracting
    # into ./proj/ and extracting into the cwd produce the SAME tree. Measured with a
    # flooding extractor, this check passes either way. Check 6 is where tarbomb protection
    # is tested, against a FLAT archive, which is the only shape that can tell them apart.
    #
    # The count is still taken, because it catches the other direction: an extractor that
    # scattered EXTRA entries around the archive.
    before5="$(find "$WORK/x5" -mindepth 1 -maxdepth 1 | wc -l)"
    if run_in "$WORK/x5" "$DEP_UNCOMPRESS" proj.tar.xz; then
        after5="$(find "$WORK/x5" -mindepth 1 -maxdepth 1 | wc -l)"
        if [ ! -d "$WORK/x5/proj" ]; then
            bad "uncompress exited 0 but ./proj/ was not created" "$RUN_OUT"
        elif [ "$after5" -ne $((before5 + 1)) ]; then
            bad "extraction added $((after5 - before5)) entries to the working directory, expected 1" \
                "$(find "$WORK/x5" -mindepth 1 -maxdepth 1 -printf '%f ')"
        else
            # The members are located by search, not at a fixed depth, because the real
            # round-trip DOUBLE-NESTS: `compress proj` archives a member called `proj`, and
            # `uncompress` extracts it into a new `./proj/`, so the file lands at
            # ./proj/proj/one.txt. Decision 5 accepts that deliberately ("1 for safety" —
            # no magic flatten). Asserting the flattened path instead would have made this
            # check fail on correct behaviour, which is how it was first written.
            #
            # Contents are compared, not just presence: an extractor that created the right
            # names with empty files would satisfy a `-f` test.
            found_one="$(find "$WORK/x5/proj" -name one.txt -type f -print -quit)"
            found_two="$(find "$WORK/x5/proj" -path '*/nested/two.txt' -type f -print -quit)"
            if [ -z "$found_one" ] || [ -z "$found_two" ]; then
                bad "./proj/ was created but does not contain what went in" \
                    "$(find "$WORK/x5/proj" -printf '%P ')"
            elif [ "$(cat "$found_one")" != "alpha" ] || [ "$(cat "$found_two")" != "beta" ]; then
                bad "./proj/ contains the expected names but not the expected contents" \
                    "one.txt='$(cat "$found_one")' two.txt='$(cat "$found_two")'"
            else
                ok "extracted to ./proj/ with both members and their contents, adding exactly one entry to the cwd"
            fi
        fi
    else
        bad "uncompress proj.tar.xz exited non-zero" "$RUN_OUT"
    fi
fi
echo

# ── 6. the tarbomb case ───────────────────────────────────────────────────────────────────
check 6 "uncompress of a FLAT zip lands in its own folder (the tarbomb case)"
mkdir -p "$WORK/x6/src"
printf 'one\n' > "$WORK/x6/src/loose-one.txt"
printf 'two\n' > "$WORK/x6/src/loose-two.txt"
# Built with the backend directly, because the wrapper deliberately CANNOT produce this
# shape — `compress` always archives a single named PATH, so its zips always nest. The
# tarbomb is what a zip from ELSEWHERE looks like, and that is the case the wrapper's
# guarantee exists for. A fixture built by the thing under test would test nothing.
if run_in "$WORK/x6/src" ouch compress -- loose-one.txt loose-two.txt ../bomb.zip; then
    # The fixture is ASSERTED flat before it is used. A zip that happened to nest would make
    # this check pass without exercising tarbomb protection at all — a green result that
    # says nothing, which is the failure mode this whole gate is written against.
    # `ouch list` prints an `Archive: "<path>"` header before the members, and that path
    # contains slashes — so testing the whole output for `/` matched the header every time
    # and reported a genuinely flat fixture as nested. The header is dropped and only the
    # MEMBER lines are tested.
    listing="$(ouch list "$WORK/x6/bomb.zip" | awk '!/^Archive:/')"
    if [ -z "$listing" ]; then
        bad "ouch list named no members, so the fixture cannot be checked for flatness" \
            "without that check this check would assert nothing"
    elif printf '%s\n' "$listing" | grep -q '/'; then
        bad "the fixture zip is not flat, so this check would not test tarbomb protection" \
            "$(printf '%s' "$listing" | tr '\n' ' ')"
    else
        before6="$(find "$WORK/x6" -mindepth 1 -maxdepth 1 | wc -l)"
        if run_in "$WORK/x6" "$DEP_UNCOMPRESS" bomb.zip; then
            after6="$(find "$WORK/x6" -mindepth 1 -maxdepth 1 | wc -l)"
            if [ ! -d "$WORK/x6/bomb" ]; then
                bad "uncompress exited 0 but ./bomb/ was not created" "$RUN_OUT"
            elif [ "$after6" -ne $((before6 + 1)) ]; then
                bad "a flat zip flooded the working directory: $((after6 - before6)) new entries" \
                    "$(find "$WORK/x6" -mindepth 1 -maxdepth 1 -printf '%f ')"
            elif [ ! -f "$WORK/x6/bomb/loose-one.txt" ] || [ ! -f "$WORK/x6/bomb/loose-two.txt" ]; then
                bad "./bomb/ was created but the loose members are not in it" \
                    "$(find "$WORK/x6/bomb" -printf '%P ')"
            else
                ok "a zip with two top-level members extracted to ./bomb/, flooding nothing"
            fi
        else
            bad "uncompress bomb.zip exited non-zero" "$RUN_OUT"
        fi
    fi
else
    bad "could not build the flat zip fixture with ouch" \
        "without it this check cannot test tarbomb protection, so it asserts nothing: $RUN_OUT"
fi
echo

# ── 7. overwrite refusal ──────────────────────────────────────────────────────────────────
check 7 "both wrappers refuse to overwrite without --force, and destroy nothing"
# Refusing is only half of it. A wrapper that deleted the archive and THEN refused would
# pass an exit-status-only assertion, so the bytes are checked to be unchanged as well.
before_sum="$(sha256sum "$WORK/proj.tar.xz" | cut -d' ' -f1)"
if run_in "$WORK" "$DEP_COMPRESS" proj; then
    compress_rc=0
else
    compress_rc=$?
fi
after_sum="$(sha256sum "$WORK/proj.tar.xz" | cut -d' ' -f1)"
if [ "$compress_rc" -eq 0 ]; then
    bad "compress silently overwrote an existing proj.tar.xz" "it must refuse without --force"
elif [ "$before_sum" != "$after_sum" ]; then
    bad "compress refused (exit $compress_rc) but the existing archive changed anyway"
else
    ok "compress refused with exit $compress_rc and left the existing archive byte-identical"
fi

if run_in "$WORK/x5" "$DEP_UNCOMPRESS" proj.tar.xz; then
    uncompress_rc=0
else
    uncompress_rc=$?
fi
if [ "$uncompress_rc" -eq 0 ]; then
    bad "uncompress silently replaced an existing ./proj/" "it must refuse without --force"
elif [ -z "$(find "$WORK/x5/proj" -name one.txt -type f -print -quit)" ]; then
    # Located by search, for the same reason check 5 locates it by search: the real round
    # trip double-nests, so the member is at ./proj/proj/one.txt. This assertion carried
    # the flattened path after check 5 had been corrected for it — the identical wrong
    # assumption, left standing two checks further down.
    bad "uncompress refused (exit $uncompress_rc) but the existing folder was emptied anyway" \
        "$(find "$WORK/x5/proj" -printf '%P ')"
else
    ok "uncompress refused with exit $uncompress_rc and left the existing folder intact"
fi
echo

# ── 8. the shadowing hazard the play preflights ───────────────────────────────────────────
check 8 "PATH resolves compress to /usr/local/bin, not a shadowing /usr/bin copy"
# The play refuses to install while ncompress is present, because that package ships
# /usr/bin/compress. That preflight guards INSTALLATION; this guards the state afterwards,
# which is a different fact — a package installed LATER shadows the wrapper with the play
# none the wiser, and every check above still passes because they call the path directly.
resolved_compress=""
resolved_uncompress=""
if ! resolved_compress="$(command -v compress)"; then
    resolved_compress="(not on PATH)"
fi
if ! resolved_uncompress="$(command -v uncompress)"; then
    resolved_uncompress="(not on PATH)"
fi
if [ "$resolved_compress" = "$DEP_COMPRESS" ] && [ "$resolved_uncompress" = "$DEP_UNCOMPRESS" ]; then
    ok "PATH resolves both wrappers to /usr/local/bin"
else
    bad "PATH resolves compress to '$resolved_compress' and uncompress to '$resolved_uncompress'" \
        "something shadows the wrappers — check for the ncompress package"
fi
echo

# ── 9. the other algorithm flags ──────────────────────────────────────────────────────────
check 9 "compress --gz and --7z produce their declared extensions"
# Success Criteria name --gz and --7z alongside --xz and --zip. The wrapper maps each flag
# to an extension in one case statement, so a mapping typo is silent: the archive is still
# written, just under a name nothing downstream expects.
for algo_pair in "--gz:tar.gz" "--7z:7z"; do
    algo_flag="${algo_pair%%:*}"
    algo_ext="${algo_pair#*:}"
    algo_dir="alt${algo_ext//./}"
    make_fixture_dir "$WORK/$algo_dir"
    if run_in "$WORK" "$DEP_COMPRESS" "$algo_flag" "$algo_dir"; then
        if [ -f "$WORK/$algo_dir.$algo_ext" ] && [ -s "$WORK/$algo_dir.$algo_ext" ]; then
            ok "compress $algo_flag produced $algo_dir.$algo_ext"
        else
            bad "compress $algo_flag exited 0 but $algo_dir.$algo_ext is missing or empty" \
                "$(find "$WORK" -maxdepth 1 -name "$algo_dir*" -printf '%f ')"
        fi
    else
        bad "compress $algo_flag $algo_dir exited non-zero" "$RUN_OUT"
    fi
done
echo

# ── 10. fail fast on conflicting flags ────────────────────────────────────────────────────
check 10 "compress with two algorithm flags exits 2 and names both"
make_fixture_dir "$WORK/conflict"
if run_in "$WORK" "$DEP_COMPRESS" --xz --zip conflict; then
    conflict_rc=0
else
    conflict_rc=$?
fi
# Exit 2 alone is not enough: the wrapper exits 2 for a missing file, an unknown option and
# an absent backend too. The message has to name both flags, or a passing check would be
# consistent with the conflict never being detected at all.
if [ "$conflict_rc" -eq 0 ]; then
    bad "compress --xz --zip exited 0" "conflicting algorithm flags must be a hard error"
elif [ "$conflict_rc" -ne 2 ]; then
    bad "compress --xz --zip exited $conflict_rc, expected 2" "$RUN_OUT"
elif [[ "$RUN_OUT" != *"--xz"* ]] || [[ "$RUN_OUT" != *"--zip"* ]]; then
    bad "compress exited 2 but the message does not name both flags" "$RUN_OUT"
elif [ -e "$WORK/conflict.tar.xz" ] || [ -e "$WORK/conflict.zip" ]; then
    bad "compress refused but wrote an archive anyway" \
        "$(find "$WORK" -maxdepth 1 -name 'conflict.*' -printf '%f ')"
else
    ok "compress --xz --zip exited 2, named both flags, and wrote nothing"
fi
echo

# Every check is done with the fixtures, so the tree goes now rather than at exit. The
# cleanup registration above still covers a run killed before this point.
remove_work_tree

# ── coverage ──────────────────────────────────────────────────────────────────────────────
#
# Coverage is stated, not inferred. A check that is deleted, renumbered, or skipped by an
# early exit disappears from RAN_CHECKS and is NAMED here — and an incomplete run is
# REJECTED even with zero failures, because a gate that did not run all of its checks has
# not established what it claims to.
missing=()
for expected in "${EXPECTED_CHECKS[@]}"; do
    case " ${RAN_CHECKS[*]} " in
        *" $expected "*) ;;
        *) missing+=("$expected") ;;
    esac
done

# The other direction: a check that RAN without being declared. Compared one way only,
# adding a check and forgetting the declaration prints "COVERAGE: 10 of 9" and accepts.
undeclared=()
for ran in "${RAN_CHECKS[@]+"${RAN_CHECKS[@]}"}"; do
    case " ${EXPECTED_CHECKS[*]} " in
        *" $ran "*) ;;
        *) undeclared+=("$ran") ;;
    esac
done

duplicates=()
seen_checks=""
for ran in "${RAN_CHECKS[@]+"${RAN_CHECKS[@]}"}"; do
    case " ${seen_checks} " in
        *" $ran "*) duplicates+=("$ran") ;;
        *) seen_checks="${seen_checks}${seen_checks:+ }$ran" ;;
    esac
done

catalogue_duplicates=()
seen_declared=""
for declared_id in "${EXPECTED_CHECKS[@]}"; do
    case " ${seen_declared} " in
        *" $declared_id "*) catalogue_duplicates+=("$declared_id") ;;
        *) seen_declared="${seen_declared}${seen_declared:+ }$declared_id" ;;
    esac
done

echo "=============================================================="
echo "COVERAGE: ${#RAN_CHECKS[@]} of ${#EXPECTED_CHECKS[@]} checks executed" \
    "(${PASS} assertion(s) passed, ${FAIL} failed)"
if [ "${#missing[@]}" -ne 0 ]; then
    echo "  NOT RUN: ${missing[*]}" >&2
fi
if [ "${#undeclared[@]}" -ne 0 ]; then
    echo "  RAN BUT NOT DECLARED: ${undeclared[*]}" >&2
fi
if [ "${#duplicates[@]}" -ne 0 ]; then
    echo "  RAN MORE THAN ONCE: ${duplicates[*]}" >&2
fi
if [ "${#catalogue_duplicates[@]}" -ne 0 ]; then
    echo "  DECLARED MORE THAN ONCE: ${catalogue_duplicates[*]}" >&2
fi
# The counts themselves, compared directly. The four named conditions above each describe a
# KNOWN way they can disagree; this one holds whether or not the cause has a name yet, which
# is the only form of the assertion a fifth cause cannot walk past.
if [ "${#RAN_CHECKS[@]}" -ne "${#EXPECTED_CHECKS[@]}" ]; then
    echo "  COUNT MISMATCH: ${#RAN_CHECKS[@]} executed against ${#EXPECTED_CHECKS[@]} declared" >&2
    count_disagrees=1
else
    count_disagrees=0
fi

verdict_ok=0
if [ "$FAIL" -eq 0 ] && [ "${#missing[@]}" -eq 0 ] && [ "${#undeclared[@]}" -eq 0 ] \
    && [ "${#duplicates[@]}" -eq 0 ] && [ "${#catalogue_duplicates[@]}" -eq 0 ] \
    && [ "$count_disagrees" -eq 0 ]; then
    echo "ACCEPTED — every declared check ran exactly once and every assertion passed."
    echo "  PLAN.md Phase 4's six user-testing items are checks 2-7; the remaining"
    echo "  Success Criteria about --gz/--7z and conflicting flags are 9-10."
    verdict_ok=1
# The NAMED causes first, the unnamed fallback LAST. Checked first, the generic "the counts
# disagree" preempts every specific verdict, because each named cause ALSO makes the counts
# differ — so the specific wording becomes reachable only when two faults cancel out.
elif [ "$FAIL" -eq 0 ] && [ "${#duplicates[@]}" -ne 0 ]; then
    echo "REJECTED — ${#duplicates[@]} check id(s) ran more than once." >&2
elif [ "$FAIL" -eq 0 ] && [ "${#catalogue_duplicates[@]}" -ne 0 ]; then
    echo "REJECTED — ${#catalogue_duplicates[@]} check id(s) are declared more than once." >&2
elif [ "$FAIL" -eq 0 ] && [ "${#undeclared[@]}" -ne 0 ]; then
    echo "REJECTED — ${#undeclared[@]} check(s) ran that this gate does not declare." >&2
elif [ "$FAIL" -eq 0 ] && [ "${#missing[@]}" -ne 0 ]; then
    echo "REJECTED — no assertion failed, but ${#missing[@]} declared check(s) never ran." >&2
elif [ "$FAIL" -eq 0 ] && [ "$count_disagrees" -ne 0 ]; then
    echo "REJECTED — the executed and declared check counts disagree, for a reason this" >&2
    echo "  gate has no name for. Read the COVERAGE line above." >&2
else
    echo "REJECTED — $FAIL assertion(s) failed, $PASS passed." >&2
    echo "  Run deploy.bash, then re-run this gate." >&2
fi
echo "=============================================================="

# Recorded as a leg so plan_finish's exit status agrees with the verdict printed above, and
# names the run log on the way out. The leg is `test`, an external command: a shell function
# passed here is an indirection shellcheck cannot follow, and in a script ending in
# plan_finish every such body is then reported unreachable.
plan_gather_leg "the acceptance verdict" test "$verdict_ok" -eq 1
plan_finish
