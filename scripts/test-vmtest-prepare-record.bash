#!/usr/bin/env bash
# Unit-test the fixture→checker record contract (Plan 00109 Task 3.2).
#
#   ./scripts/test-vmtest-prepare-record.bash
#
# WHY THIS EXISTS. `guest-prepare-server-host-health-kernel-change.bash` writes a record
# and `guest-acceptance-server-host-health-kernel-change.bash` sources it. That seam is
# the one thing no mutant, no gate and no lint could see, and it was broken in both
# directions at once:
#
#   * the fixture wrote `KEY=value` unquoted, and one recorded value ends in
#     `(system scope)` — the text `probe_results` produces for a failed unit. `(` is a
#     shell metacharacter, so `source` exited 2 and every key after that line was unset;
#   * the checker tested the record for EXISTENCE and then sourced it without judging the
#     result, so a half-loaded record read as a loaded one. It then died on `set -u`
#     part-way through the checks, emitting no `VMTEST-CHECKS-DONE` — a verdict of
#     `error` that points a reader at the lab rather than at the fixture.
#
# Both sides are exercised here against the REAL scripts: the fixture's own `record`
# writes the file, and the real checker reads it. The acceptance case comes FIRST, so
# every refusal below is a change from a known-good baseline.
#
# What this does NOT do is judge the checks themselves — most of them cannot pass outside
# a guest. What it pins is that the ACCOUNTING completes: planned declared, every check
# emitted, `VMTEST-CHECKS-DONE` printed. A record that cannot be read has to stop the run
# before any of that, loudly.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the whole
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LAB="$REPO_ROOT/files/home/.local/share/vmtest"
FIXTURE="$LAB/guest-prepare-server-host-health-kernel-change.bash"
CHECKER="$LAB/guest-acceptance-server-host-health-kernel-change.bash"

for required in "$FIXTURE" "$CHECKER"; do
    if [ ! -f "$required" ]; then
        echo "FAIL: $required not found" >&2
        exit 1
    fi
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

passed=0
failed=0

report() {
    local status="$1" name="$2" detail="${3:-}"
    if [ "$status" = pass ]; then
        passed=$((passed + 1))
        printf 'ok   %s\n' "$name"
    else
        failed=$((failed + 1))
        printf 'FAIL %s%s\n' "$name" "${detail:+ — $detail}" >&2
    fi
}

# The fixture's OWN writer, extracted rather than reimplemented — a copy here would agree
# with itself while the deployed one wrote something else. Bounded by its definition line
# and the first `^}` after it, so a rename fails loudly at extraction.
awk '$0 ~ /^record\(\) \{/ {p=1} p {print} p && /^\}/ {exit}' "$FIXTURE" > "$work/record.bash"
if ! grep -q '^record() {' "$work/record.bash"; then
    echo "FAIL: could not extract record() from the fixture" >&2
    exit 1
fi

# The checker's own list of what it requires, likewise read from the file. A list copied
# here would keep passing after the checker started reading a key nobody writes.
mapfile -t REQUIRED < <(awk '/^REQUIRED_KEYS=\(/ {p=1; next} p && /^\)/ {exit} p {for (i = 1; i <= NF; i++) print $i}' "$CHECKER")
if [ "${#REQUIRED[@]}" -lt 20 ]; then
    echo "FAIL: extracted only ${#REQUIRED[@]} required keys from the checker" >&2
    exit 1
fi

# Which keys the checker allows to be blank, read from the checker itself. Derived rather
# than judged here: a copy of that list would let this test write a record the real
# checker rejects, or accept one it would not.
mapfile -t OPTIONAL < <(awk '/^MAY_BE_EMPTY=\(/ {p=1; next} p && /^\)/ {exit} p {for (i = 1; i <= NF; i++) if ($i ~ /^[A-Z][A-Z0-9_]+$/) print $i}' "$CHECKER")

may_be_blank() {
    local wanted="$1" key
    for key in "${OPTIONAL[@]}"; do
        [ "$key" = "$wanted" ] && return 0
    done
    return 1
}

# One hostile value per shape a recorded value can actually take. The parenthesised one is
# not hypothetical — it is the exact text `probe_results.failed_unit_findings` produces.
hostile_for() {
    if may_be_blank "$1"; then
        printf ''
        return
    fi
    case "$1" in
        PREPARED_FIXTURE_FINDING) printf 'vmtest-health-fixture.service: failed (system scope)' ;;
        PREPARED_TIMER_ENABLED) printf 'enabled' ;;
        PREPARED_TIMER_AFTER) printf 'disabled' ;;
        *_RC | PREPARED_SCP_BYTES) printf '0' ;;
        *_SHA) printf 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef' ;;
        *_B64) printf 'e30=' ;;
        *KERNEL) printf '6.17.0-63.fc44.x86_64' ;;
        *) printf "a 'quoted' value with \$(echo substitution) and a (paren)" ;;
    esac
}

# The fixture's `record` writes to `${EVIDENCE}`, so that is the seam it is driven through
# here — the real function, in this shell, with nothing reimplemented around it.
# Exported because shellcheck cannot see the sourced function reading it, and because that
# function is the thing under test rather than something written here.
export EVIDENCE=""
# shellcheck source=/dev/null
source "$work/record.bash"

# write_record <destination> <writer> — build a complete record with every required key.
# `writer` is `real` (the fixture's `record`) or `raw` (the unquoted form that broke).
write_record() {
    local destination="$1" writer="$2" key value
    : > "$destination"
    EVIDENCE="$destination"
    for key in "${REQUIRED[@]}"; do
        value="$(hostile_for "$key")"
        if [ "$writer" = real ]; then
            record "$key" "$value"
        else
            printf '%s=%s\n' "$key" "$value" >> "$destination"
        fi
    done
}

# run_checker <record-file> — the REAL checker, with HOME pointed at a scratch tree so it
# reads that record and touches nothing else. Bounded: several of its probes reach for a
# network or an sshd that is not here, and each must fail rather than hang.
CHECKER_RC=0
CHECKER_OUT=""
run_checker() {
    local record="$1" home="$work/home"
    rm -rf "${home:?}"
    mkdir -p "$home/.vmtest"
    cp "$record" "$home/.vmtest/host-health-prepared.env"
    CHECKER_OUT="$(HOME="$home" VMTEST_COMMIT=0000000000000000000000000000000000000000 \
        timeout 120 bash "$CHECKER" 2>&1)"
    CHECKER_RC=$?
}

# ── 1. a complete record written by the fixture's own writer is READ ──────────────────
# The baseline. Individual checks fail here — there is no guest — but the ACCOUNTING has
# to complete, because that is what separates "the scenario failed" from "the harness
# broke", and a reader is sent to a different file by each.
write_record "$work/good.env" real
run_checker "$work/good.env"
planned_line="$(printf '%s\n' "$CHECKER_OUT" | grep -c '^VMTEST-CHECK-PLANNED 15$')"
done_line="$(printf '%s\n' "$CHECKER_OUT" | grep -c '^VMTEST-CHECKS-DONE total=15 ')"
if [ "$planned_line" = 1 ] && [ "$done_line" = 1 ]; then
    report pass a-complete-record-lets-the-checker-finish-its-accounting
else
    report fail a-complete-record-lets-the-checker-finish-its-accounting \
        "planned=$planned_line done=$done_line rc=$CHECKER_RC"
fi

# ── 2. the value that broke it round-trips exactly ────────────────────────────────────
finding="$(printf '%s\n' "$CHECKER_OUT" | grep -c 'vmtest-health-fixture.service: failed (system scope)')"
if [ "$finding" -gt 0 ]; then
    report pass a-parenthesised-value-survives-the-record
else
    report fail a-parenthesised-value-survives-the-record \
        "the failed-unit text never reached a check's detail"
fi

# ── 3. every required key is readable after sourcing ──────────────────────────────────
missing=""
for key in "${REQUIRED[@]}"; do
    # shellcheck source=/dev/null
    if ! value="$(source "$work/good.env" && printf '%s' "${!key-__UNSET__}")"; then
        missing="$missing $key(source-failed)"
    elif [ "$value" = "__UNSET__" ]; then
        missing="$missing $key"
    fi
done
if [ -z "$missing" ]; then
    report pass every-required-key-survives-sourcing "${#REQUIRED[@]} keys"
else
    report fail every-required-key-survives-sourcing "unset after sourcing:$missing"
fi

# ── 4. the unquoted form is REFUSED, not half-loaded ──────────────────────────────────
# The regression case. An unquoted record loads the keys before the metacharacter and
# drops the rest, so the checker must reject the file rather than judge what happened to
# survive — and it must do so before emitting a single check.
write_record "$work/raw.env" raw
run_checker "$work/raw.env"
emitted="$(printf '%s\n' "$CHECKER_OUT" | grep -c '^VMTEST-CHECK ')"
if [ "$CHECKER_RC" = 70 ] && [ "$emitted" = 0 ]; then
    report pass an-unparseable-record-stops-the-run
elif [ "$CHECKER_RC" = 70 ]; then
    report fail an-unparseable-record-stops-the-run "refused, but after emitting $emitted check(s)"
else
    report fail an-unparseable-record-stops-the-run "rc=$CHECKER_RC, $emitted check(s) emitted"
fi

# ── 5. an INCOMPLETE record is refused, and names what is missing ─────────────────────
# Distinct from case 4: this file parses perfectly. Without the completeness assertion it
# would source cleanly and every absent key would read as its default — and for at least
# one check the defaulted answer was the passing one.
write_record "$work/short.env" real
grep -v '^PREPARED_FIXTURE_FINDING=' "$work/short.env" > "$work/short2.env"
mv "$work/short2.env" "$work/short.env"
run_checker "$work/short.env"
if [ "$CHECKER_RC" = 70 ] && printf '%s\n' "$CHECKER_OUT" | grep -q 'PREPARED_FIXTURE_FINDING'; then
    report pass an-incomplete-record-is-refused-by-name
else
    report fail an-incomplete-record-is-refused-by-name "rc=$CHECKER_RC: $CHECKER_OUT"
fi

# ── 5a. a record that is COMPLETE but still not shell is refused, and for that reason ──
# The case that separates the two guards. Every required key is present and set — bash
# executes a sourced file command by command, so a syntax error at the END runs everything
# before it — and the completeness assertion is therefore satisfied. Only judging
# `source`'s own status catches this, and without it the run would continue on a record
# whose tail was silently dropped.
write_record "$work/tail.env" real
printf 'this is ( not shell\n' >> "$work/tail.env"
run_checker "$work/tail.env"
if [ "$CHECKER_RC" = 70 ] && printf '%s\n' "$CHECKER_OUT" | grep -q 'not readable as shell'; then
    report pass a-complete-but-unparseable-record-is-refused-as-unparseable
else
    report fail a-complete-but-unparseable-record-is-refused-as-unparseable \
        "rc=$CHECKER_RC: $CHECKER_OUT"
fi

# ── 5aa. a key that is set but BLANK is refused, unless it is allowed to be ───────────
# The shape the completeness assertion could not see. `PREPARED_RUNNING_KERNEL=''`
# satisfies set-ness, and then check 9's `*"collected under kernel ${…}"*` degrades to a
# prefix the real rendered line contains — so the check passes having verified one of the
# two kernels it is named for. Measured, not argued.
write_record "$work/blank.env" real
grep -v '^PREPARED_RUNNING_KERNEL=' "$work/blank.env" > "$work/blank2.env"
printf "PREPARED_RUNNING_KERNEL=''\n" >> "$work/blank2.env"
mv "$work/blank2.env" "$work/blank.env"
run_checker "$work/blank.env"
if [ "$CHECKER_RC" = 70 ] && printf '%s\n' "$CHECKER_OUT" | grep -q 'PREPARED_RUNNING_KERNEL'; then
    report pass a-blank-value-is-refused-by-name
else
    report fail a-blank-value-is-refused-by-name "rc=$CHECKER_RC: $CHECKER_OUT"
fi

# ── 5ab. and a key that IS allowed to be blank still passes ───────────────────────────
# The control for the case above. Without it, a rule that refused every empty value would
# look identical — and it would refuse a silent clean login, which is the one capture
# whose emptiness is the finding.
if [ "${#OPTIONAL[@]}" -gt 0 ]; then
    write_record "$work/blankok.env" real
    run_checker "$work/blankok.env"
    if printf '%s\n' "$CHECKER_OUT" | grep -q '^VMTEST-CHECKS-DONE '; then
        report pass a-blank-value-that-is-allowed-still-runs "${OPTIONAL[0]} is blank"
    else
        report fail a-blank-value-that-is-allowed-still-runs "rc=$CHECKER_RC"
    fi
else
    report fail a-blank-value-that-is-allowed-still-runs "MAY_BE_EMPTY is empty; the control proves nothing"
fi

# ── 5b. every record key the checker READS is one it declares REQUIRED ────────────────
# The class, rather than this instance of it. Both blocking defects reduced to the same
# thing: a value the checker read without having established it was there. The
# completeness assertion only covers keys somebody remembered to list, so a key read but
# not declared is the next one of these — and it is static, so it costs a grep.
declared=" ${REQUIRED[*]} "
undeclared=""
while read -r key; do
    [[ -n "$key" ]] || continue
    case "$declared" in
        *" $key "*) ;;
        *) undeclared="$undeclared $key" ;;
    esac
done < <(grep -oE '\$\{(PREPARED|CLEAN|FINDING)_[A-Z0-9_]+' "$CHECKER" |
    cut -c3- | sort -u)
if [ -z "$undeclared" ]; then
    report pass every-key-the-checker-reads-is-declared-required
else
    report fail every-key-the-checker-reads-is-declared-required \
        "read but not in REQUIRED_KEYS:$undeclared"
fi

# ── 5c. a key that may be EMPTY has to say so ─────────────────────────────────────────
# A blank value is not the same as a missing one, and for a check that compares by
# substring it is worse: the pattern degrades to a prefix and passes. The checker's
# MAY_BE_EMPTY list is the opt-in, so it must be a subset of what it requires — a name
# there that nothing requires is a rule with no subject.
orphans=""
for key in "${OPTIONAL[@]}"; do
    case "$declared" in
        *" $key "*) ;;
        *) orphans="$orphans $key" ;;
    esac
done
if [ "${#OPTIONAL[@]}" -gt 0 ] && [ -z "$orphans" ]; then
    report pass may-be-empty-names-only-required-keys "${#OPTIONAL[@]} key(s) may be blank"
else
    report fail may-be-empty-names-only-required-keys \
        "${#OPTIONAL[@]} listed; not required:$orphans"
fi

# ── 6. an absent record is refused too ────────────────────────────────────────────────
rm -rf "${work:?}/home"
mkdir -p "$work/home/.vmtest"
CHECKER_OUT="$(HOME="$work/home" VMTEST_COMMIT=0000000000000000000000000000000000000000 \
    timeout 120 bash "$CHECKER" 2>&1)"
CHECKER_RC=$?
if [ "$CHECKER_RC" = 70 ]; then
    report pass an-absent-record-stops-the-run
else
    report fail an-absent-record-stops-the-run "rc=$CHECKER_RC"
fi

printf 'passed: %d\n' "$passed"
if [ "$failed" -gt 0 ]; then
    printf 'failed: %d\n' "$failed" >&2
    exit 1
fi
exit 0
