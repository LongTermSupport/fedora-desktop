#!/usr/bin/env bash
# Test `ccy --token-usage [--json]`: every token's usage limits, printed without the menu.
#
# Runs the real launcher from THIS repo (not the deployed /var/local copy), from a scratch
# directory that is not a repository, with stdin from /dev/null, HOME pointed at a scratch
# home holding placeholder token files, and curl, podman, docker and tmux replaced by stubs
# on PATH. The curl stub answers from fixture response headers and logs which placeholder
# it was handed; the others only log that they were called. No request leaves the machine
# and no real credential is involved.
#
# WHAT IT PINS. The command reads usage for every token name and exits without a terminal,
# a repository, a container or tmux. A name with two files is read with the earliest-dated
# one (the file `ccy --token NAME` launches with) and the other is reported as shadowed and
# never sent. An expired token is never sent. Percentages are the menu's normalised 0-100
# figures, under either CCY_USAGE_SCALE, and a value the scale cannot produce is null, not a
# number. Exit 0 when any token was read, 1 when none was, 64 on a bad option. Token values
# never reach stdout or stderr.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"

for f in "$LAUNCHER" "$LIB_DIR/common-pure.bash" "$LIB_DIR/token-management.bash"; do
    if [ ! -f "$f" ]; then
        echo "FAIL: not found: $f" >&2
        exit 1
    fi
done
if ! command -v python3 > /dev/null; then
    echo "FAIL: python3 is needed to parse the JSON output" >&2
    exit 1
fi

# Under the repo's gitignored scratch area rather than /tmp, as the sibling usage test does.
WORK="$(mktemp -d "$REPO_ROOT/untracked/ccy-token-usage-command.XXXXXX")"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

STUB_BIN="$WORK/bin"
CALLS="$WORK/calls"
NOT_A_REPO="$WORK/not-a-repo"
mkdir -p "$STUB_BIN" "$NOT_A_REPO"

# curl: the token arrives as a curl config line on stdin, exactly as _usage_fetch_one sends
# it. The answer depends on the placeholder, so one pool covers every response shape.
cat >"$STUB_BIN/curl" <<STUB
#!/usr/bin/env bash
set -euo pipefail
hdr=""
while [ "\$#" -gt 0 ]; do
    case "\$1" in
        --dump-header) hdr="\$2"; shift ;;
    esac
    shift
done
config="\$(cat)"
token="\${config#*Bearer }"
token="\${token%\"*}"
printf 'curl %s\n' "\$token" >>"$CALLS"
now="\$(date +%s)"
ok_headers() {
    printf 'HTTP/2 200\r\ncontent-type: application/json\r\n' >"\$hdr"
    if [ -n "\$1" ]; then
        printf 'anthropic-ratelimit-unified-5h-utilization: %s\r\n' "\$1" >>"\$hdr"
        printf 'anthropic-ratelimit-unified-5h-reset: %s\r\n' "\$((now + 14400))" >>"\$hdr"
    fi
    printf 'anthropic-ratelimit-unified-7d-utilization: %s\r\n' "\$2" >>"\$hdr"
    printf 'anthropic-ratelimit-unified-7d-reset: %s\r\n' "\$((now + 518400))" >>"\$hdr"
    printf 'anthropic-ratelimit-unified-representative-claim: five_hour\r\n\r\n' >>"\$hdr"
    printf '200'
}
case "\$token" in
    placeholder-denied)
        printf 'HTTP/2 401\r\ncontent-type: application/json\r\n\r\n' >"\$hdr"
        printf '401'
        ;;
    placeholder-unreachable)
        printf '000'
        exit 7
        ;;
    placeholder-percent) ok_headers 34 8 ;;
    placeholder-renewed) ok_headers 0.77 0.66 ;;
    placeholder-over) ok_headers 1.01 0.005 ;;
    placeholder-partial) ok_headers '' 0.08 ;;
    *) ok_headers 0.34 0.08 ;;
esac
STUB
for tool in podman docker tmux; do
    printf '#!/usr/bin/env bash\nprintf "%s %%s\\n" "$*" >>"%s"\nexit 1\n' "$tool" "$CALLS" >"$STUB_BIN/$tool"
done
chmod 755 "$STUB_BIN"/*

NEXT_YEAR="$(date -d '+365 days' +%Y-%m-%d)"
LATER="$(date -d '+500 days' +%Y-%m-%d)"
YESTERDAY="$(date -d '-1 day' +%Y-%m-%d)"

# new_home <name> <file=placeholder>... — a scratch home whose token store holds those files.
new_home() {
    local home="$WORK/$1" spec
    shift
    mkdir -p "$home/.claude-tokens/ccy/tokens"
    for spec in "$@"; do
        printf '%s\n' "${spec#*=}" >"$home/.claude-tokens/ccy/tokens/${spec%%=*}"
    done
    printf '%s' "$home"
}

PASSED=0
FAILED=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        PASSED=$((PASSED + 1))
        printf '  PASS  %s\n' "$label"
    else
        FAILED=$((FAILED + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got"
    fi
}
has() { if [[ "$2" == *"$1"* ]]; then echo yes; else echo no; fi; }

OUT=""
ERR=""
RC=0
# run_ccy <home> [env assignments...] -- <args...>
run_ccy() {
    local home="$1"; shift
    local -a envs=()
    while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do envs+=("$1"); shift; done
    shift
    : >"$CALLS"
    RC=0
    OUT=$(cd "$NOT_A_REPO" && env HOME="$home" PATH="$STUB_BIN:$PATH" TZ=UTC \
        CCY_CONTAINER_ENGINE=podman "${envs[@]}" bash "$LAUNCHER" "$@" </dev/null 2>"$WORK/stderr") || RC=$?
    ERR=$(cat "$WORK/stderr")
}

# Flattens the JSON on stdin into `<token>.<key>=<json value>` lines plus `.<key>=` top-level
# lines; prints `INVALID ...` when it is not exactly one JSON object.
flatten() {
    python3 -I -c '
import json, sys
try:
    doc = json.loads(sys.stdin.read())
except ValueError as exc:
    print("INVALID " + str(exc))
    sys.exit(0)
if not isinstance(doc, dict):
    print("INVALID not an object")
    sys.exit(0)
for key, value in doc.items():
    if key == "tokens":
        print(".token_count=" + str(len(value)))
    else:
        print("." + key + "=" + json.dumps(value))
for tok in doc.get("tokens", []):
    for key, value in tok.items():
        print(tok["name"] + "." + key + "=" + json.dumps(value))
'
}
FLAT=""
field() {
    local line
    while IFS= read -r line; do
        if [[ "$line" == "$1="* ]]; then
            printf '%s' "${line#*=}"
            return 0
        fi
    done <<<"$FLAT"
    printf '<absent>'
}
curl_calls() { grep -c '^curl ' "$CALLS"; }
# Every directory, and every file with its size and mtime, under a home: the usage cache
# aside, two snapshots differ exactly when the run created or changed something there.
home_snapshot() {
    find "$1" -path "$1/.claude-tokens/ccy/usage-cache" -prune \
        -o -type d -printf 'd %P\n' -o -type f -printf 'f %P %s %T@\n' | sort
}
called_with() { if grep -qxF "curl $1" "$CALLS"; then echo yes; else echo no; fi; }
leaks() {
    local p found=""
    for p in placeholder-work placeholder-renewed placeholder-denied placeholder-old; do
        [[ "$OUT$ERR" == *"$p"* ]] && found+="$p "
    done
    printf '%s' "$found"
}

ISO='"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z"'

echo ""
echo "=== --json over a mixed pool: one readable, one refused, one expired, one shadowed ==="
MIXED=$(new_home mixed "work.$NEXT_YEAR.token=placeholder-work" \
    "work.$LATER.token=placeholder-renewed" "personal.$NEXT_YEAR.token=placeholder-denied" \
    "old.$YESTERDAY.token=placeholder-old")
MIXED_BEFORE=$(home_snapshot "$MIXED")
run_ccy "$MIXED" -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "exit 0 when at least one token was read" "0" "$RC"
check "stdout is one JSON object" "no" "$(has INVALID "$FLAT")"
check "schema is 1" "1" "$(field .schema)"
check "no global error" "null" "$(field .error)"
check "scale is the default" '"fraction"' "$(field .scale)"
check "one entry per token name" "3" "$(field .token_count)"
check "work: status ok" '"ok"' "$(field work.status)"
check "work: 0.34 reads as 34" "34" "$(field work.five_hour_pct)"
check "work: 0.08 reads as 8" "8" "$(field work.seven_day_pct)"
check "work: figures belong to the earliest-dated file" "\"work.$NEXT_YEAR.token\"" "$(field work.file)"
check "work: expires is that file's date" "\"$NEXT_YEAR\"" "$(field work.expires)"
check "work: the later file is listed as shadowed" "[\"work.$LATER.token\"]" "$(field work.shadowed)"
check "work: binding limit" '"five_hour"' "$(field work.binding_limit)"
check "work: http status" "200" "$(field work.http_status)"
check "work: reason is null" "null" "$(field work.reason)"
if [[ "$(field work.resets)" =~ ^\{\"five_hour\":\ $ISO,\ \"seven_day\":\ $ISO\}$ ]]; then r=yes; else r="no: $(field work.resets)"; fi
check "work: both resets are ISO-8601 UTC" "yes" "$r"
if [[ "$(field work.fetched_at)" =~ ^$ISO$ ]]; then r=yes; else r="no: $(field work.fetched_at)"; fi
check "work: fetched_at is ISO-8601 UTC" "yes" "$r"
check "personal: unavailable" '"unavailable"' "$(field personal.status)"
check "personal: says why" "yes" "$(has "not authorised" "$(field personal.reason)")"
check "personal: no figures" "null null" "$(field personal.five_hour_pct) $(field personal.seven_day_pct)"
check "personal: http status 401" "401" "$(field personal.http_status)"
check "old: unavailable" '"unavailable"' "$(field old.status)"
check "old: reason names the expiry" "yes" "$(has "expired on $YESTERDAY" "$(field old.reason)")"
check "the earliest work file was sent" "yes" "$(called_with placeholder-work)"
check "the shadowed work file was never sent" "no" "$(called_with placeholder-renewed)"
check "the expired token was never sent" "no" "$(called_with placeholder-old)"
check "exactly two requests" "2" "$(curl_calls)"
check "no token value on stdout or stderr" "" "$(leaks)"
check "no engine or tmux call" "" "$(grep -v '^curl ' "$CALLS")"
check "nothing written to the working directory" "" "$(find "$NOT_A_REPO" -mindepth 1 -print -quit)"
check "the usage cache was written" "yes" \
    "$([ -f "$MIXED/.claude-tokens/ccy/usage-cache/work.result" ] && echo yes || echo no)"
check "the cache holds one entry per name read and no part-files" "personal.result work.result" \
    "$(find "$MIXED/.claude-tokens/ccy/usage-cache" -mindepth 1 -printf '%f\n' | sort | xargs)"
check "nothing else in HOME created or changed (no session record, no Quick Launch)" "" \
    "$(diff <(printf '%s\n' "$MIXED_BEFORE") <(home_snapshot "$MIXED"))"
check "--json writes nothing to stderr" "" "$ERR"

echo ""
echo "=== a second run inside the cache lifetime sends nothing ==="
run_ccy "$MIXED" -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "exit 0" "0" "$RC"
check "no request" "0" "$(curl_calls)"
check "same figures from the cache" "34 8" "$(field work.five_hour_pct) $(field work.seven_day_pct)"

echo ""
echo "=== CCY_USAGE_TTL=0 forces a fresh read, and the fresh figures are used ==="
run_ccy "$MIXED" CCY_USAGE_TTL=0 -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "exit 0" "0" "$RC"
check "both readable tokens re-sent" "2" "$(curl_calls)"
check "work: ok from this run's read" '"ok" 34' "$(field work.status) $(field work.five_hour_pct)"

echo ""
echo "=== an out-of-date entry whose refresh did not happen is not passed off as current ==="
# An empty token file is skipped by usage_prime_cache's worker, so the hour-old entry
# below, written with this very file, is all that is left to read.
STALE=$(new_home stale "s.$NEXT_YEAR.token=")
mkdir -p "$STALE/.claude-tokens/ccy/usage-cache"
printf 's.%s.token\n200\n0.34\t%s\t0.08\t%s\tfive_hour\n' "$NEXT_YEAR" "$(date +%s)" "$(date +%s)" \
    >"$STALE/.claude-tokens/ccy/usage-cache/s.result"
touch -d '1 hour ago' "$STALE/.claude-tokens/ccy/usage-cache/s.result"
run_ccy "$STALE" -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "exit 1" "1" "$RC"
check "unavailable, not the hour-old figures" '"unavailable" null' "$(field s.status) $(field s.five_hour_pct)"
check "says the read did not complete" "yes" "$(has "did not complete" "$(field s.reason)")"
check "no request (the file is empty)" "0" "$(curl_calls)"

echo ""
echo "=== the table ==="
run_ccy "$MIXED" -- --token-usage
check "exit 0" "0" "$RC"
header=$(head -n1 <<<"$OUT")
check "header names the columns" "yes" \
    "$([[ "$header" =~ ^NAME\ +EXPIRES\ +5-HOUR\ +RESETS\ +7-DAY\ +RESETS\ +STATUS$ ]] && echo yes || echo "no: $header")"
work_row=$(grep -E "^work +$NEXT_YEAR " <<<"$OUT")
check "work row: 34% and 8%" "yes" \
    "$([[ "$work_row" =~ \ 34%\ .*\ 8%\  ]] && echo yes || echo "no: $work_row")"
check "work row: reset in words with the time" "yes" "$(has "in 4 hours (" "$work_row")"
check "work row: binding limit" "yes" "$(has "ok; binding: 5-hour limit" "$work_row")"
check "personal row says why" "yes" "$(has "unavailable: this token was not authorised to read it" "$OUT")"
check "shadowed row names the file a launch uses" "yes" \
    "$(has "shadowed: ccy --token work launches with work.$NEXT_YEAR.token" "$OUT")"
status_col=${header%%STATUS*}
misaligned=""
while IFS= read -r line; do
    [[ "${line:${#status_col}}" =~ ^(STATUS|ok|unavailable:|shadowed:) ]] || misaligned+="[$line] "
done <<<"$OUT"
check "every row's status starts in the STATUS column" "" "$misaligned"
check "one row per name plus the shadowed file and the header" "5" "$(wc -l <<<"$OUT" | tr -d ' ')"
check "no colour codes" "no" "$(has $'\033' "$OUT")"
check "progress goes to stderr" "yes" "$(has "Reading usage for 2 account(s)" "$ERR")"
check "no token value on stdout or stderr" "" "$(leaks)"

echo ""
echo "=== no token readable is a failure, and says so ==="
DENIED=$(new_home denied "a.$NEXT_YEAR.token=placeholder-denied" "b.$NEXT_YEAR.token=placeholder-unreachable")
run_ccy "$DENIED" -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "exit 1" "1" "$RC"
check "still one JSON object" "no" "$(has INVALID "$FLAT")"
check "global error" '"usage could not be read for any token"' "$(field .error)"
check "each token says why" '"unavailable" "unavailable"' "$(field a.status) $(field b.status)"
check "unreachable: says so" '"could not reach the API"' "$(field b.reason)"
check "unreachable: no HTTP status (curl's 000 is not one)" "null" "$(field b.http_status)"
run_ccy "$DENIED" -- --token-usage
check "table: exit 1" "1" "$RC"
check "table: error on stderr" "yes" "$(has "ccy --token-usage: usage could not be read for any token" "$ERR")"

EMPTY=$(new_home empty)
run_ccy "$EMPTY" -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "empty pool: exit 1" "1" "$RC"
check "empty pool: error names it" "yes" "$(has "no tokens in" "$(field .error)")"
check "empty pool: tokens is an empty list" "0" "$(field .token_count)"

run_ccy "$MIXED" CCY_TOKEN_USAGE=0 -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "CCY_TOKEN_USAGE=0: exit 1" "1" "$RC"
check "CCY_TOKEN_USAGE=0: error names it" "yes" "$(has "CCY_TOKEN_USAGE=0" "$(field .error)")"
check "CCY_TOKEN_USAGE=0: no request" "0" "$(curl_calls)"

# No curl: PATH holds only `date`, the one external tool usage_report runs before it asks
# for curl, so the absence is real rather than simulated.
NOCURL_BIN="$WORK/nocurl-bin"
mkdir -p "$NOCURL_BIN"
ln -s "$(command -v date)" "$NOCURL_BIN/date"
RC=0
OUT=$(env PATH="$NOCURL_BIN" "$(command -v bash)" -c "
    source '$LIB_DIR/common-pure.bash'
    source '$LIB_DIR/token-management.bash'
    usage_report '$MIXED/.claude-tokens/ccy/tokens' --json
" 2>"$WORK/stderr") || RC=$?
FLAT=$(flatten <<<"$OUT")
check "no curl: exit 1" "1" "$RC"
check "no curl: error names the remedy" '"needs curl (run play-claude-yolo.yml)"' "$(field .error)"

echo ""
echo "=== the scale switch, over-limit, a missing bucket, an awkward name ==="
PERCENT=$(new_home percent "p.$NEXT_YEAR.token=placeholder-percent")
run_ccy "$PERCENT" CCY_USAGE_SCALE=percent -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "percent scale: 34 reads as 34" "34 8" "$(field p.five_hour_pct) $(field p.seven_day_pct)"
check "percent scale is reported" '"percent"' "$(field .scale)"
rm -rf "$PERCENT/.claude-tokens/ccy/usage-cache"
run_ccy "$PERCENT" -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "fraction scale refuted by 34: no number" "null null" "$(field p.five_hour_pct) $(field p.seven_day_pct)"
check "fraction scale refuted by 34: SCALE MISMATCH warning" "yes" "$(has "SCALE MISMATCH" "$(field p.warnings)")"
check "fraction scale refuted by 34: unavailable, exit 1" '"unavailable" 1' "$(field p.status) $RC"

ODD=$(new_home odd "over.$NEXT_YEAR.token=placeholder-over" "partial.$NEXT_YEAR.token=placeholder-partial" \
    "we\"ird.$NEXT_YEAR.token=placeholder-work")
run_ccy "$ODD" -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "awkward name: still valid JSON" "no" "$(has INVALID "$FLAT")"
check "awkward name: read" '"ok"' "$(field 'we"ird.status')"
check "over the limit: 1.01 reads as 101" "101" "$(field over.five_hour_pct)"
check "under one percent keeps its decimals (0.005 reads as 0.5)" "0.5" "$(field over.seven_day_pct)"
check "missing 5-hour bucket: null, not shifted" "null 8" "$(field partial.five_hour_pct) $(field partial.seven_day_pct)"
check "missing 5-hour bucket: said" "yes" "$(has "5-hour: the API did not report it" "$(field partial.warnings)")"
check "missing 5-hour bucket: still ok" '"ok"' "$(field partial.status)"

echo ""
echo "=== a renewal shadowed by an expired file of the same name ==="
RENEWAL=$(new_home renewal "w.$YESTERDAY.token=placeholder-old" "w.$NEXT_YEAR.token=placeholder-work" \
    "w.backup.token=placeholder-old" "wx.$NEXT_YEAR.token=placeholder-work")
run_ccy "$RENEWAL" -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "w: reported with the expired file a launch would use" "\"w.$YESTERDAY.token\" \"unavailable\"" \
    "$(field w.file) $(field w.status)"
check "w: the renewal is listed as shadowed" "yes" "$(has "w.$NEXT_YEAR.token" "$(field w.shadowed)")"
check "w: flagged as a shadowed renewal, naming the file to remove" "yes" \
    "$(has "SHADOWED RENEWAL: ccy launches with this unusable file, not the later one; remove w.$YESTERDAY.token" "$(field w.warnings)")"
check "wx is its own name, read normally" '"ok"' "$(field wx.status)"
check "only wx sent: neither the expired file nor the shadowed renewal" "1 no" \
    "$(curl_calls) $(called_with placeholder-old)"
run_ccy "$MIXED" -- --token-usage --json
FLAT=$(flatten <<<"$OUT")
check "a usable file that shadows a later one is flagged too" "yes" \
    "$(has "shadows a later file of this name" "$(field work.warnings)")"

echo ""
echo "=== the launcher's warning when --token NAME has other files ==="
RC=0
OUT=$(bash -c "
    source '$LIB_DIR/common-pure.bash'
    source '$LIB_DIR/token-management.bash'
    token_shadow_warning '$RENEWAL/.claude-tokens/ccy/tokens/w.$YESTERDAY.token'
" 2>&1) || RC=$?
check "warning: exit 0" "0" "$RC"
check "warning names the other dated file" "yes" "$(has "Token w has 1 other file(s): w.$NEXT_YEAR.token" "$OUT")"
check "warning names the file in use" "yes" "$(has "ccy uses the earliest-dated, w.$YESTERDAY.token" "$OUT")"
check "warning ignores an undated file and another name" "no no" "$(has backup "$OUT") $(has wx. "$OUT")"
OUT=$(bash -c "
    source '$LIB_DIR/common-pure.bash'
    source '$LIB_DIR/token-management.bash'
    token_shadow_warning '$RENEWAL/.claude-tokens/ccy/tokens/wx.$NEXT_YEAR.token'
" 2>&1)
check "no warning for a name with one file" "" "$OUT"
check "the launcher calls it where --token NAME picks its file" "1" \
    "$(grep -cF "token_shadow_warning \"\$SELECTED_TOKEN\"" "$LAUNCHER")"

echo ""
echo "=== one cache entry never mixes two files' fetches ==="
# The menu fetches every valid file, so two files of one name are fetched at once into one
# entry. Whichever wins, its file name, status and figures must be from the same fetch.
TWO=$(new_home two "work.$NEXT_YEAR.token=placeholder-work" "work.$LATER.token=placeholder-renewed")
mixed_entries=0
for round in 1 2 3 4 5 6 7 8 9 10; do
    rm -rf "$TWO/.claude-tokens/ccy/usage-cache"
    env PATH="$STUB_BIN:$PATH" bash -c "
        source '$LIB_DIR/common-pure.bash'
        source '$LIB_DIR/token-management.bash'
        usage_prime_cache '$TWO/.claude-tokens/ccy/tokens' \
            '$TWO/.claude-tokens/ccy/tokens/work.$NEXT_YEAR.token' \
            '$TWO/.claude-tokens/ccy/tokens/work.$LATER.token'
    "
    mapfile -t entry <"$TWO/.claude-tokens/ccy/usage-cache/work.result"
    case "${entry[0]}:${entry[2]%%$'\t'*}" in
        "work.$NEXT_YEAR.token:0.34" | "work.$LATER.token:0.77") ;;
        *) mixed_entries=$((mixed_entries + 1)); printf '        round %s: %q\n' "$round" "${entry[*]}" ;;
    esac
done
check "10 concurrent rounds, every entry consistent" "0" "$mixed_entries"

echo ""
echo "=== options ==="
run_ccy "$MIXED" -- --token-usage --bogus
check "an unknown option: exit 64" "64" "$RC"
check "an unknown option: says the usage" "yes" "$(has "usage: ccy --token-usage [--json]" "$ERR")"
check "an unknown option: no request" "0" "$(curl_calls)"
run_ccy "$MIXED" -- --help
check "--help lists the option" "yes" "$(has "--token-usage [--json]" "$OUT")"
check "--help documents the JSON keys" "yes" "$(has "five_hour_pct" "$OUT")"

echo ""
printf 'passed: %d   failed: %d\n' "$PASSED" "$FAILED"
if [ "$PASSED" -eq 0 ]; then
    echo "FAIL: no case passed" >&2
    exit 1
fi
if [ "$FAILED" -ne 0 ]; then
    exit 1
fi
