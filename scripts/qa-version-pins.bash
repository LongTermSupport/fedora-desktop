#!/usr/bin/bash
# Validate the tracked upstream version-pin manifest (Plan 00109, Task 2.2).
#
# vars/version-pins.yml says where every pinned upstream version lives. Two
# consumers read it — scripts/check-pinned-versions.bash (pin vs upstream) and
# Plan 00109's installed-vs-pinned check (pin vs this host) — and neither is run
# by qa-all: the first needs an authenticated gh, the second needs a real host.
# So without this gate a manifest that had drifted away from the playbooks would
# surface only when somebody happened to run a review tool.
#
# It checks two different things, and the second is the one that rots:
#   * the manifest's own shape, via helpers/version_pins/manifest.py (stdlib-only,
#     so this script owns the YAML conversion, using the PyYAML that Ansible
#     itself depends on and qa-ansible-syntax.bash already assumes);
#   * that every row still points at a playbook that exists and still declares
#     its var. A row naming a var that was renamed reports the OLD value for ever.
#
# Four control checks make the gate falsifiable (CLAUDE/QA.md "Changing a Gate"):
# an empty manifest must be rejected AND rejected FOR being empty, a row pointing
# at a missing file must fail, a row naming an undeclared var must fail for a
# different reason, and the row dispatch must fail loudly rather than silently.
# A gate that passes when its validator never ran is worse than none — and so is
# one that fails without saying why, which is what the fourth control exists for.
#
# Coverage is compared, not assumed: the count the validator declares is held
# against the number of rows that reached the on-disk check. Agreeing totals that
# nothing compares are how a gate reports "9 pins" and checks one.
#
#   ./scripts/qa-version-pins.bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

MANIFEST="vars/version-pins.yml"

yaml_to_json() {
    python3 -c 'import json, sys, yaml; json.dump(yaml.safe_load(sys.stdin), sys.stdout)'
}

validate() {
    python3 -m helpers.version_pins.manifest
}

# Control 1: an empty manifest must be rejected, or the validator is not judging.
# The REASON is asserted, not just the marker: any breakage upstream of the validator
# (a yaml_to_json failure feeding it empty stdin, say) also produces an INVALID marker,
# so a marker-only assertion passes while the control never tested emptiness at all.
control_out=""
if control_out="$(printf 'version_pins: []\n' | yaml_to_json | validate 2>&1)"; then
    echo "ERROR: the validator accepted a manifest with no pins; the gate cannot be trusted" >&2
    exit 1
fi
if [[ "$control_out" != *VERSION-PINS-INVALID* ]]; then
    echo "ERROR: the validator rejected the control manifest without its INVALID marker: $control_out" >&2
    exit 1
fi
if [[ "$control_out" != *"is empty"* ]]; then
    echo "ERROR: the empty-manifest control was rejected for some OTHER reason, so it" \
         "never tested emptiness: $control_out" >&2
    exit 1
fi

# Converted in its own step rather than piped straight into the validator. In one
# pipeline a YAML failure went to the terminal while the validator, handed empty
# stdin, reported a JSON decode error — so the gate's diagnosis named a problem the
# manifest did not have. Its stderr goes to a file, not into the payload: merging the
# two streams is the very defect the login report had.
conversion_error="$(mktemp)"
trap 'rm -f "$conversion_error"' EXIT
json=""
if ! json="$(yaml_to_json <"$MANIFEST" 2>"$conversion_error")"; then
    echo "ERROR: $MANIFEST is not valid YAML:" >&2
    cat "$conversion_error" >&2
    exit 1
fi

# The real manifest. Its rows are the payload; the marker is asserted. Both marker
# and rows are on the validator's stdout, so nothing is merged here either.
if ! out="$(printf '%s' "$json" | validate)"; then
    echo "ERROR: $MANIFEST is invalid:" >&2
    echo "$out" >&2
    exit 1
fi
if [[ "$out" != VERSION-PINS-OK* ]]; then
    echo "ERROR: validator exited 0 without its OK marker for $MANIFEST: $out" >&2
    exit 1
fi

# Every row must still resolve on disk. `pin_is_live` is a function so the
# controls below can exercise the same code the real rows go through, rather
# than a copy of it that could agree with a broken original.
pin_is_live() {
    local file="$1" var="$2"
    [ -f "$file" ] || return 1
    grep -qE "^[[:space:]]*${var}[[:space:]]*:" "$file" || return 2
    return 0
}

# Control 2: a row naming a file that does not exist must fail the check, and a
# row naming a var that is not in its file must fail it for a DIFFERENT reason.
# Same-status controls would not tell the two failures apart.
if pin_is_live "playbooks/imports/no-such-playbook-00109.yml" "any_version"; then
    echo "ERROR: the on-disk check passed a playbook that does not exist" >&2
    exit 1
fi
if pin_is_live "$MANIFEST" "definitely_not_a_declared_var_00109"; then
    echo "ERROR: the on-disk check passed a var that is not declared" >&2
    exit 1
fi

# One row's verdict AND its message. A function so control 4 below drives the same
# dispatch the real rows go through — the controls above exercise `pin_is_live`, not
# this `case`, and with the status read from `$?` inside `if ! …` every arm missed and
# the gate exited non-zero in silence. A gate that fails without saying why is barely
# a gate, so the control asserts the message, not just the status.
check_row() {
    local file="$1" var="$2" status=0
    pin_is_live "$file" "$var" || status=$?
    [ "$status" -eq 0 ] && return 0
    case "$status" in
        1) echo "ERROR: $MANIFEST names a playbook that does not exist: $file" >&2 ;;
        2) echo "ERROR: $MANIFEST names '$var', which $file no longer declares" >&2 ;;
        *) echo "ERROR: $MANIFEST row '$file' '$var' failed with status $status" >&2 ;;
    esac
    return 1
}

# Control 4: a bad row must fail the dispatch AND name the reason on stderr.
control_status=0
control_out="$(check_row "playbooks/imports/no-such-playbook-00109.yml" "any_version" 2>&1)" \
    || control_status=$?
if [ "$control_status" -eq 0 ]; then
    echo "ERROR: the row dispatch passed a playbook that does not exist" >&2
    exit 1
fi
if [[ "$control_out" != *"does not exist"* ]]; then
    echo "ERROR: the row dispatch failed without naming the reason: [$control_out]" >&2
    exit 1
fi

rows=0
while IFS='|' read -r file var _repo _extra _note; do
    [ -z "$file" ] && continue
    rows=$((rows + 1))
    check_row "$file" "$var" || exit 1
done < <(printf '%s\n' "$out" | tail -n +2)

marker="${out%%$'\n'*}"
# The count the validator DECLARED, held against the rows that actually arrived. The
# zero guard below catches a total collapse; this catches the partial one, where the
# marker says nine and one row is checked. Both numbers were already printed on the
# same line, disagreeing, with nothing comparing them — this repo's most-repeated
# defect, in the gate written to prevent it.
declared="$(printf '%s\n' "$marker" | awk '{print $2}')"
if ! [[ "$declared" =~ ^[0-9]+$ ]]; then
    echo "ERROR: could not read the declared pin count from the marker: [$marker]" >&2
    exit 1
fi

# `VERSION-PINS-OK 9 pin(s), 1 with install state tracked, 8 declared untracked`
tracked="$(printf '%s\n' "$marker" | awk '{print $4}')"
if ! [[ "$tracked" =~ ^[0-9]+$ ]]; then
    echo "ERROR: could not read the tracked pin count from the marker: [$marker]" >&2
    exit 1
fi
# A coverage FLOOR. Every pin may legitimately be declared untracked one at a time,
# and at the end of that road the login-time check compares nothing, finds nothing,
# and this gate still exits 0 — a drift axis that has quietly stopped existing, which
# is the whole subject of Plan 00109. One tracked pin is the minimum that makes the
# axis real.
if [ "$tracked" -eq 0 ]; then
    echo "ERROR: $MANIFEST declares $declared pin(s) and tracks the installed version" \
         "of NONE of them, so the installed-vs-pinned check compares nothing and" \
         "cannot fail. Give at least one pin an 'installed:' block." >&2
    exit 1
fi

if [ "$rows" -eq 0 ]; then
    echo "ERROR: no rows reached the on-disk check, so nothing was verified" >&2
    exit 1
fi
if [ "$rows" -ne "$declared" ]; then
    echo "ERROR: $MANIFEST declares $declared pin(s) but $rows row(s) reached the" \
         "on-disk check, so $((declared - rows)) were never verified" >&2
    exit 1
fi

echo "$MANIFEST: $marker — COVERAGE: $rows of $declared resolve to a live playbook var"
