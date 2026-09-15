#!/usr/bin/env bash
# Unit-test the CCY session registry (files/var/local/claude-yolo/lib/session-registry.bash).
#
# WHY THIS EXISTS. The registry is what decides, at boot, which sessions were running when
# the machine went down — and a restore service acts on its answer with no human watching.
# Three properties therefore have to hold mechanically rather than by inspection:
#
#   1. A PARTIAL WRITE IS NEVER A RECORD. A killed writer or a lost power rail must not
#      leave a file the reader treats as a session. Cases below cover the temp-file name,
#      the missing terminator, the unknown schema and the absent required key — each of
#      which must be a refusal, and each of which is independently sufficient.
#   2. THE RESTORE FLAGS ARE RECONSTRUCTED FAITHFULLY. A session restored with the wrong
#      token, the wrong key or no network is not the session that was running. Values with
#      spaces have to survive, which is why the multi-valued fields are base64 rather than
#      a delimiter someone's path can contain.
#   3. THE FLAG CLASSIFICATION CANNOT GO STALE. This is the case that matters most in a
#      year's time. CLAUDE/ContainerRules.md records two incidents in this very program
#      where a hand-written list of its own parts was a file short of the truth — one for
#      two months. So the last case DERIVES the flag set from the launcher's own parser and
#      fails when a flag is unclassified, instead of trusting a list to stay complete.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly. Nothing is discarded either — a probe that
# is expected to fail is captured by `probe` below, so its reason is available to report.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
LIB="$CCY_DIR/lib/session-registry.bash"
PURE="$CCY_DIR/lib/common-pure.bash"
LAUNCHER="$CCY_DIR/claude-yolo"

for required in "$LIB" "$PURE" "$LAUNCHER"; do
    if [ ! -f "$required" ]; then
        echo "FAIL: required file not found: $required" >&2
        exit 1
    fi
done

# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$PURE"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/session-registry.bash
source "$LIB"

for fn in ccy_registry_write ccy_registry_field ccy_registry_validate ccy_registry_list \
    ccy_registry_slug ccy_registry_fingerprint ccy_registry_encode_argv \
    ccy_registry_decode_argv ccy_registry_restore_flags ccy_registry_flag_class \
    ccy_registry_retire ccy_registry_remove ccy_registry_boot_id; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: $fn is not defined after sourcing the library" >&2
        exit 1
    fi
done

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

# probe <cmd...> — run it, capture BOTH streams into PROBE_OUT, return its status. This is
# the repo's pattern for checking whether something fails without an error-hiding redirect
# (CLAUDE/InteractiveScripts.md): the reason is kept, not thrown away, so a case that fails
# for an unexpected reason says so instead of quietly passing.
PROBE_OUT=""
probe() {
    PROBE_OUT="$("$@" 2>&1)"
}

# yesno <cmd...> — "yes" when the command succeeds, "no: <reason>" when it does not.
yesno() {
    if probe "$@"; then
        printf 'yes'
    else
        printf 'no: %s' "$PROBE_OUT"
    fi
}

WORK="$(mktemp -d)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

SESSIONS="$WORK/sessions"
mkdir -p "$SESSIONS"

# write_record <name> [field=value...] — a minimally valid record, with any given field
# REPLACING its default rather than being appended after it. Appending would produce a record
# with the key twice, which the library now refuses outright — and before it did, the reader
# took the first line, so an override was silently ignored and three cases below passed on the
# default they meant to change. Returns the writer's status so a refusal can be asserted.
write_record() {
    local name="$1"
    shift
    local -a fields=("project_dir=/projects/demo" "boot_id=0000-boot" "restore=yes")
    local given key i replaced
    for given in "$@"; do
        key="${given%%=*}"
        replaced=no
        for i in "${!fields[@]}"; do
            if [ "${fields[$i]%%=*}" = "$key" ]; then
                fields[i]="$given"
                replaced=yes
            fi
        done
        [ "$replaced" = no ] && fields+=("$given")
    done
    ccy_registry_write "$SESSIONS" "$name" "${fields[@]}"
}

# ── the ordinary round trip ──────────────────────────────────────────────────────────

check "a valid record is written" "yes" "$(yesno write_record ccy-demo "token_name=work")"
REC="$SESSIONS/ccy-demo.record"
check "the record lands under its session name" "yes" "$([ -f "$REC" ] && echo yes || echo no)"
check "project_dir round-trips" "/projects/demo" "$(ccy_registry_field "$REC" project_dir)"
check "token_name round-trips" "work" "$(ccy_registry_field "$REC" token_name)"
check "the schema is stamped by the writer" "$CCY_REGISTRY_SCHEMA" \
    "$(ccy_registry_field "$REC" schema)"
check "the terminator is the last line" "end=1" "$(awk 'END { print }' "$REC")"

# A record is human-readable on purpose — restore-status prints from it and an operator
# debugs from it — so the format stays plain key=value and only the genuinely multi-valued
# fields are encoded. That trade is only safe because a newline is refused on write below.
check "the record is plain key=value, not encoded" "yes" \
    "$(grep -qx 'project_dir=/projects/demo' "$REC" && echo yes || echo no)"

# An absent key is a non-zero return, NOT an empty string: "no token" and "the field is
# missing" are different facts, and a caller that cannot tell them apart would restore a
# session with no token and call it success.
if probe ccy_registry_field "$REC" no_such_key; then
    check "an absent key returns non-zero" "non-zero" "zero, printed '$PROBE_OUT'"
else
    check "an absent key returns non-zero" "non-zero" "non-zero"
fi

# A field whose value is legitimately empty must still be FOUND. Distinguishing "present
# and empty" from "absent" is the same distinction one level down.
check "an empty token_name is still written" "yes" "$(yesno write_record ccy-empty "token_name=")"
if probe ccy_registry_field "$SESSIONS/ccy-empty.record" token_name; then
    check "a present-but-empty field is found" "found" "found"
else
    check "a present-but-empty field is found" "found" "reported absent: $PROBE_OUT"
fi

# ── values a real machine actually produces ──────────────────────────────────────────

# A home directory with a space in it is ordinary on a desktop.
check "a record with a spaced path is written" "yes" \
    "$(yesno write_record ccy-spaced "project_dir=/home/a user/My Projects/app")"
check "a path with spaces round-trips" "/home/a user/My Projects/app" \
    "$(ccy_registry_field "$SESSIONS/ccy-spaced.record" project_dir)"

# A newline would split one field into two lines and silently corrupt every field after
# it, so the writer refuses rather than producing a record that parses as something else.
if probe write_record ccy-newline "project_dir=/tmp/one
two"; then
    check "a newline in a value is refused" "refused" "accepted"
else
    check "a newline in a value is refused" "refused" "refused"
fi
check "the newline refusal explains itself" "yes" \
    "$(printf '%s' "$PROBE_OUT" | grep -qi 'newline' && echo yes || echo "no: $PROBE_OUT")"
check "a refused write leaves no record" "no" \
    "$([ -f "$SESSIONS/ccy-newline.record" ] && echo yes || echo no)"
check "a refused write leaves no temp file" "0" \
    "$(find "$SESSIONS" -maxdepth 1 -name '.*.tmp.*' | wc -l)"

# ── the partial-write cases: four independent refusals ───────────────────────────────

# 1. The in-flight temp name. `rename` makes the swap atomic, so a reader can only ever
#    see the old record or the new one — but a writer killed BEFORE the rename leaves its
#    temp file behind, and that file must be invisible to the listing.
printf 'schema=%s\nproject_dir=/projects/half\n' "$CCY_REGISTRY_SCHEMA" \
    >"$SESSIONS/.ccy-half.record.tmp.999"
mapfile -t -d '' listed < <(ccy_registry_list "$SESSIONS")
listed_names=""
for path in "${listed[@]}"; do
    listed_names+="$(basename "$path") "
done
check "an abandoned temp file is not listed" "no" \
    "$(printf '%s' "$listed_names" | grep -q 'half' && echo yes || echo no)"
check "the valid records are still listed" "yes" \
    "$(printf '%s' "$listed_names" | grep -q 'ccy-demo.record' && echo yes || echo no)"

# 2. A truncated record that somehow bypassed the rename — a hand-edit, a restore from
#    backup, a filesystem that reordered. The terminator is the only thing that can catch
#    it, which is why the reader requires it rather than treating it as decoration.
printf 'schema=%s\nproject_dir=/projects/x\nboot_id=b\nrestore=yes\n' "$CCY_REGISTRY_SCHEMA" \
    >"$SESSIONS/ccy-truncated.record"
if probe ccy_registry_validate "$SESSIONS/ccy-truncated.record"; then
    check "a record with no terminator is refused" "refused" "accepted"
else
    check "a record with no terminator is refused" "refused" "refused"
fi
# The refusal has to say which file and why, or an operator reading the journal at boot
# learns only that something was wrong.
check "the refusal names the file" "yes" \
    "$(printf '%s' "$PROBE_OUT" | grep -q 'ccy-truncated' && echo yes || echo "no: $PROBE_OUT")"
check "the refusal explains itself" "yes" \
    "$(printf '%s' "$PROBE_OUT" | grep -qi 'terminator\|incomplete\|truncated' && echo yes || echo "no: $PROBE_OUT")"

# 3. A schema this code does not understand. Guessing at an unknown layout is how a future
#    field gets read as the wrong thing.
printf 'schema=99\nproject_dir=/projects/x\nboot_id=b\nrestore=yes\nend=1\n' \
    >"$SESSIONS/ccy-future.record"
if probe ccy_registry_validate "$SESSIONS/ccy-future.record"; then
    check "an unknown schema is refused" "refused" "accepted"
else
    check "an unknown schema is refused" "refused" "refused"
fi

# 4. A required key absent. Every one of them is load-bearing for a restore decision, so
#    a record missing any of them cannot be acted on.
for missing in project_dir boot_id restore; do
    body=""
    for pair in "project_dir=/projects/x" "boot_id=b" "restore=yes"; do
        [ "${pair%%=*}" = "$missing" ] && continue
        body+="$pair"$'\n'
    done
    printf 'schema=%s\n%send=1\n' "$CCY_REGISTRY_SCHEMA" "$body" \
        >"$SESSIONS/ccy-missing.record"
    if probe ccy_registry_validate "$SESSIONS/ccy-missing.record"; then
        check "a record missing '$missing' is refused" "refused" "accepted"
    else
        check "a record missing '$missing' is refused" "refused" "refused"
    fi
done
rm -f "$SESSIONS/ccy-missing.record"

# A valid record must of course pass, or every case above proves nothing.
check "a valid record passes validation" "yes" "$(yesno ccy_registry_validate "$REC")"

# ── the slug: keyed on the DIRECTORY, never the project name ──────────────────────────
#
# lib/tmux-session.bash:69 records why: two checkouts of one repo share a project name, so
# anything keyed on the name conflates them. Two directories with the same basename must
# therefore slug differently.
a="$(ccy_registry_slug /home/u/work/app)"
b="$(ccy_registry_slug /home/u/other/app)"
check "same basename, different directory, different slug" "differ" \
    "$([ "$a" != "$b" ] && echo differ || echo "same: $a")"
check "a slug is filesystem-safe" "yes" \
    "$(printf '%s' "$a" | grep -qE '^[A-Za-z0-9_.-]+$' && echo yes || echo "no: $a")"
check "a slug is stable across calls" "$a" "$(ccy_registry_slug /home/u/work/app)"

# ── argv encoding: evidence only, but it must be faithful ────────────────────────────
encoded="$(ccy_registry_encode_argv --token "a b" $'multi\nline' '')"
mapfile -t -d '' decoded < <(ccy_registry_decode_argv "$encoded")
check "argv count round-trips" "4" "${#decoded[@]}"
check "an argument with a space round-trips" "a b" "${decoded[1]}"
check "an argument with a newline round-trips" "$(printf 'multi\nline')" "${decoded[2]}"
check "an empty argument round-trips" "" "${decoded[3]}"
check "an empty argv encodes to an empty string" "" "$(ccy_registry_encode_argv)"

# ── the project fingerprint (D7): a reused directory must be detectable ───────────────
REPO_A="$WORK/repo-a"
mkdir -p "$REPO_A"
git -C "$REPO_A" init -q
check "a repository with no commits reports the sentinel" "no-commits" \
    "$(ccy_registry_fingerprint "$REPO_A")"
git -C "$REPO_A" -c user.email=test@example.com -c user.name=test \
    commit -q --allow-empty -m first
root_a="$(git -C "$REPO_A" rev-list --max-parents=0 HEAD)"
check "a repository reports its root commit" "$root_a" "$(ccy_registry_fingerprint "$REPO_A")"
git -C "$REPO_A" -c user.email=test@example.com -c user.name=test \
    commit -q --allow-empty -m second
check "the fingerprint is unchanged by later commits" "$root_a" \
    "$(ccy_registry_fingerprint "$REPO_A")"

REPO_B="$WORK/repo-b"
mkdir -p "$REPO_B"
git -C "$REPO_B" init -q
git -C "$REPO_B" -c user.email=test@example.com -c user.name=test \
    commit -q --allow-empty -m other
check "an unrelated repository fingerprints differently" "differ" \
    "$([ "$(ccy_registry_fingerprint "$REPO_B")" != "$root_a" ] && echo differ || echo same)"

# A directory that is not a git work tree cannot be fingerprinted, and that is a refusal
# rather than a value: ccy refuses to run outside a repository (claude-yolo:119), so a
# fingerprint invented here would describe a session that could never start.
if probe ccy_registry_fingerprint "$WORK"; then
    check "a non-repository is refused a fingerprint" "refused" "accepted: $PROBE_OUT"
else
    check "a non-repository is refused a fingerprint" "refused" "refused"
fi

# ── restore flags: what the service actually hands the launcher ───────────────────────

# flags_of <field=value...> — the reconstructed argv for a record built from these fields,
# joined with U+00B7 so a comparison reads clearly and a space inside a value is visible.
flags_of() {
    local name="flagcase"
    rm -f "$SESSIONS/$name.record"
    if ! probe write_record "$name" "$@"; then
        printf 'WRITE-REFUSED: %s' "$PROBE_OUT"
        return 0
    fi
    local -a out=()
    mapfile -t -d '' out < <(ccy_registry_restore_flags "$SESSIONS/$name.record")
    local IFS='·'
    printf '%s' "${out[*]}"
}

# The bare case. --supervise is what re-arms the PTY supervisor so the restored session is
# nudged back to work, and --continue is what makes it the SAME conversation; neither is
# optional, so both are unconditional.
check "a bare record still asks for supervise and continue" "--supervise·--continue" \
    "$(flags_of)"
check "a token is reconstructed by name" "--token·work·--supervise·--continue" \
    "$(flags_of "token_name=work")"
check "no-ssh is reconstructed" "--no-ssh·--supervise·--continue" \
    "$(flags_of "no_ssh=yes")"
check "no-network is reconstructed" "--no-network·--supervise·--continue" \
    "$(flags_of "no_network=yes")"
check "a network name is reconstructed" "--network·devnet·--supervise·--continue" \
    "$(flags_of "network=devnet")"
check "the 443 route is reconstructed" "--github-443·--supervise·--continue" \
    "$(flags_of "github_443=yes")"
check "an explicit engine is reconstructed" "--engine·docker·--supervise·--continue" \
    "$(flags_of "engine=docker")"
check "the custom-Dockerfile opt-out is reconstructed" \
    "--disable-custom-docker·--supervise·--continue" \
    "$(flags_of "disable_custom_docker=yes")"

# An SSH key path with a space in it is why the key list is base64 and not a delimited
# string: any delimiter is a character someone's path is allowed to contain.
keys="$(ccy_registry_encode_argv "/home/a user/.ssh/github_work")"
check "an ssh key path with a space survives reconstruction" \
    "--ssh-key·/home/a user/.ssh/github_work·--supervise·--continue" \
    "$(flags_of "ssh_keys_b64=$keys")"

two_keys="$(ccy_registry_encode_argv /k/one /k/two)"
check "every ssh key gets its own flag" \
    "--ssh-key·/k/one·--ssh-key·/k/two·--supervise·--continue" \
    "$(flags_of "ssh_keys_b64=$two_keys")"

# The forwarded-agent sentinel is stored in the same list as real key paths
# (lib/ssh-handling.bash:274), so it has to be recognised rather than passed as a filename
# that does not exist.
agent="$(ccy_registry_encode_argv "$CCY_REGISTRY_SSH_AGENT_SENTINEL")"
check "the ssh-agent sentinel becomes --ssh-agent, not --ssh-key" \
    "--ssh-agent·--supervise·--continue" "$(flags_of "ssh_keys_b64=$agent")"

check "a full configuration is reconstructed in order" \
    "--token·work·--ssh-key·/k/one·--network·devnet·--github-443·--engine·podman·--supervise·--continue" \
    "$(flags_of "token_name=work" "ssh_keys_b64=$(ccy_registry_encode_argv /k/one)" \
        "network=devnet" "github_443=yes" "engine=podman")"

# A record marked no-restore must never produce a command line at all. The restore service
# checks this too, but a value this consequential is worth refusing at both layers.
rm -f "$SESSIONS/norestore.record"
check "a no-restore record is written" "yes" "$(yesno write_record norestore "restore=no")"
# stdout goes to a file rather than a command substitution here: the flags are NUL-delimited,
# and `$(...)` drops null bytes with a warning, so a capture would misreport what was produced.
if ccy_registry_restore_flags "$SESSIONS/norestore.record" \
    >"$WORK/norestore.out" 2>"$WORK/norestore.err"; then
    check "a no-restore record yields no flags" "refused" "produced $(wc -c <"$WORK/norestore.out") bytes"
else
    check "a no-restore record yields no flags" "refused" "refused"
fi
check "the no-restore refusal explains itself" "yes" \
    "$(grep -qi 'restore=no' "$WORK/norestore.err" && echo yes || echo "no: $(cat "$WORK/norestore.err")")"

# A repeated key is ambiguous and is refused on write. Before that refusal existed the reader
# took the first line, so a caller that meant the second got a record describing a session it
# did not launch — with both lines individually well-formed, and nothing to see.
if probe ccy_registry_write "$SESSIONS" dupe \
    "project_dir=/a" "boot_id=b" "restore=yes" "project_dir=/b"; then
    check "a repeated key is refused" "refused" "accepted"
else
    check "a repeated key is refused" "refused" "refused"
fi
check "the repeated-key refusal names the key" "yes" \
    "$(printf '%s' "$PROBE_OUT" | grep -q "project_dir" && echo yes || echo "no: $PROBE_OUT")"

# ── THE STALENESS GUARD: every ccy flag must be classified ───────────────────────────
#
# Derived from the launcher's own parser, never from a list kept alongside it. A flag added
# to ccy without a decision about whether it survives a restore would otherwise be dropped
# from every restored session silently — and the two incidents in ContainerRules.md say
# that is exactly what happens to a hand-written list of a program's parts.
# The pattern matches the launcher's own `elif [ "$arg" = "--flag" ]` / `[[ ... == ]]`
# branches. It deliberately starts at `arg"` rather than at the variable's `$`: in an ERE a
# dollar is the end-of-line anchor, so a pattern carrying one literally matches nothing and
# the derivation would silently find zero flags — a staleness guard that had itself gone
# inert. `"$arg"` is the only variable of this shape in the launcher, so dropping the sigil
# costs no precision.
mapfile -t parsed_flags < <(
    grep -oE 'arg" ==? "--[a-z0-9-]+' "$LAUNCHER" |
        grep -oE '\-\-[a-z0-9-]+' | sort -u
)
check "the launcher's flags were found at all" "many" \
    "$([ "${#parsed_flags[@]}" -ge 20 ] && echo many || echo "only ${#parsed_flags[@]}")"

classified_once=0
unclassified=""
double=""
for flag in "${parsed_flags[@]}"; do
    case "$(ccy_registry_flag_class "$flag")" in
    both) double+="$flag " ;;
    unknown) unclassified+="$flag " ;;
    *) classified_once=$((classified_once + 1)) ;;
    esac
done
check "every launcher flag is classified durable or one-shot" "" "$unclassified"
check "no launcher flag is classified both ways" "" "$double"
check "the classification covers every parsed flag" "${#parsed_flags[@]}" "$classified_once"

# The mirror of the same check. A manifest entry naming a flag the launcher does not parse
# is a rename that happened on one side only, and it would make the restore pass an
# argument ccy rejects — a restore that fails at the last step for a reason nobody expects.
missing_from_launcher=""
for flag in "${CCY_REGISTRY_DURABLE_FLAGS[@]}" "${CCY_REGISTRY_ONESHOT_FLAGS[@]}"; do
    found=no
    for parsed in "${parsed_flags[@]}"; do
        [ "$parsed" = "$flag" ] && found=yes
    done
    [ "$found" = no ] && missing_from_launcher+="$flag "
done
check "no classified flag is unknown to the launcher" "" "$missing_from_launcher"

# The ssh-agent sentinel is defined `readonly` in lib/ssh-handling.bash, and the registry
# keeps its own copy because ccy-sessions-restore has no reason to load 1,100 lines of SSH
# handling. A copy is a thing that drifts, so the two are compared here rather than trusted:
# if they ever disagreed, every forwarded-agent session would be restored with --ssh-key
# pointing at a file that does not exist.
sentinel_source="$(grep -oE 'SSH_AGENT_SENTINEL="[^"]+"' "$CCY_DIR/lib/ssh-handling.bash" |
    grep -oE '"[^"]+"' | tr -d '"')"
check "the ssh-agent sentinel was found in ssh-handling.bash" "found" \
    "$([ -n "$sentinel_source" ] && echo found || echo missing)"
check "the registry's sentinel matches ssh-handling.bash" "$sentinel_source" \
    "$CCY_REGISTRY_SSH_AGENT_SENTINEL"

# ── retirement: a record the restore will not act on keeps its reason ─────────────────
#
# Never a delete. Deleting destroys the only evidence of why a session did not come back, and
# leaves the operator with a machine that restored three of four and no way to learn which.
RETIRED="$WORK/retired"
check "a record to retire is written" "yes" "$(yesno write_record ccy-retireme)"
check "retiring succeeds" "yes" \
    "$(yesno ccy_registry_retire "$SESSIONS/ccy-retireme.record" "$RETIRED" directory-gone)"
check "the retired record leaves the live set" "no" \
    "$([ -f "$SESSIONS/ccy-retireme.record" ] && echo yes || echo no)"
check "the retired record arrives under the same name" "yes" \
    "$([ -f "$RETIRED/ccy-retireme.record" ] && echo yes || echo no)"
check "the reason is recorded inside it" "directory-gone" \
    "$(ccy_registry_field "$RETIRED/ccy-retireme.record" retired_reason)"
# It must still be a valid record: restore-status reads retired records to tell the operator
# what happened, and a retirement that corrupted the file would make that impossible.
check "a retired record is still valid" "yes" \
    "$(yesno ccy_registry_validate "$RETIRED/ccy-retireme.record")"
check "a retired record keeps its original fields" "/projects/demo" \
    "$(ccy_registry_field "$RETIRED/ccy-retireme.record" project_dir)"
check "the terminator is still last after retiring" "end=1" \
    "$(awk 'END { print }' "$RETIRED/ccy-retireme.record")"

# ── removal, which runs inside the launcher's EXIT trap ──────────────────────────────
#
# It must succeed on an already-absent record: the trap fires on every exit path, including
# ones where no record was ever written, and a failure there would disturb the exit status the
# trap is preserving.
check "removing an existing record succeeds" "yes" "$(yesno write_record ccy-removeme)"
check "the removal itself succeeds" "yes" "$(yesno ccy_registry_remove "$SESSIONS" ccy-removeme)"
check "the record is gone" "no" \
    "$([ -f "$SESSIONS/ccy-removeme.record" ] && echo yes || echo no)"
check "removing an absent record still succeeds" "yes" \
    "$(yesno ccy_registry_remove "$SESSIONS" never-existed)"

# ── the boot id, which is what tells a survivor from a live session (D1) ─────────────
boot_id="$(ccy_registry_boot_id)"
check "the boot id is non-empty" "yes" "$([ -n "$boot_id" ] && echo yes || echo no)"
check "the boot id is stable within one boot" "$boot_id" "$(ccy_registry_boot_id)"

# ── listing a directory that does not exist vs one that cannot be read ───────────────
#
# D8's distinction one level down. "ccy has never recorded a session on this machine" is a
# real answer and succeeds empty; a directory that exists and cannot be read is "could not
# tell" and must fail rather than report zero records.
mapfile -t -d '' absent_listing < <(ccy_registry_list "$WORK/never-created")
check "listing an absent directory succeeds with nothing" "0" "${#absent_listing[@]}"

# Every durable flag must actually be reachable from a record field, or "durable" is a
# label with no mechanism behind it. `--no-restore` is deliberately one-shot: it says
# "do not record this session", so it can never appear in a restore command.
reachable="$(flags_of "token_name=t" "ssh_keys_b64=$(ccy_registry_encode_argv /k)" \
    "no_ssh=yes" "network=n" "no_network=yes" "github_443=yes" "engine=podman" \
    "disable_custom_docker=yes")"
unreachable=""
for flag in "${CCY_REGISTRY_DURABLE_FLAGS[@]}"; do
    case "·$reachable·" in
    *"·$flag·"*) ;;
    *) unreachable+="$flag " ;;
    esac
done
# --ssh-agent and --ssh-key are alternatives within one field, so the agent case is
# asserted on its own above and excluded from this sweep rather than fudged into it.
unreachable="${unreachable/--ssh-agent /}"
check "every durable flag is reachable from a record" "" "$unreachable"

# ── the unattended read guard (D6) ───────────────────────────────────────────────────
#
# The guard lives in the launcher rather than in this library, because it is the launcher's
# control flow being protected and a reader of that file must see it. It is exercised HERE,
# from the launcher's own source, because it is the mechanism that decides whether a restored
# session runs or hangs — and the only thing standing between "restore works" and a row of
# parked shells that look restored.
#
# The functions are lifted out of the launcher rather than reimplemented: a copy of the guard
# in a test file is a test that passes while the real one is broken.
guard_src="$(awk '/^_ccy_read_guard\(\) \{/,/^}/' "$LAUNCHER")"
read_src="$(awk '/^read\(\) \{/,/^}/' "$LAUNCHER")"
check "the guard was found in the launcher" "yes" \
    "$([ -n "$guard_src" ] && echo yes || echo no)"
check "the read shadow was found in the launcher" "yes" \
    "$([ -n "$read_src" ] && echo yes || echo no)"

# run_guard <env> <script> — the launcher's two functions plus a snippet, in a child bash.
# A child, because the fatal path calls exit and would otherwise take this test with it.
run_guard() {
    local unattended="$1" snippet="$2"
    # The newline between the two function bodies is load-bearing: `$(...)` strips the trailing
    # one, so a bare concatenation yields `}read() {` on a single line and the whole eval is a
    # syntax error — which still lets the snippet run against the REAL builtin, so every case
    # would report a plausible-looking answer about a guard that was never installed.
    # Both libraries, because the guard's fatal path calls ccy_unattended_fatal from
    # session-registry.bash — the same ordering dependency the launcher has, where the sources
    # come before the shadow is defined.
    CCY_UNATTENDED="$unattended" bash -c '
        set -uo pipefail
        source "$1"
        source "$2"
        eval "$3"
        eval "$4"
    ' _ "$PURE" "$LIB" "${guard_src}"$'\n'"${read_src}" "$snippet" 2>&1
}

# The snippets are quoted heredocs, so every `$` in them is the literal source text a child
# shell will run rather than something this file expands. Inline single-quoted strings would
# say the same thing, but the linter reads a `$` in one as a mistaken expansion.
snippet_prompt=$(
    cat <<'SNIP'
printf "hello\n" | { read -rp "Say: " v; printf "got=[%s]" "$v"; }
SNIP
)
snippet_split=$(
    cat <<'SNIP'
read -ra p <<< "a b c"; printf "%s" "${#p[@]}"
SNIP
)
snippet_split2=$(
    cat <<'SNIP'
read -ra p <<< "x y"; printf "%s:%s" "${#p[@]}" "${p[0]}"
SNIP
)
snippet_pipe=$(
    cat <<'SNIP'
printf "l1\nl2\n" | while read -r l; do printf "%s" "$l"; done
SNIP
)
snippet_rp=$(
    cat <<'SNIP'
read -rp "Use same configuration? [Y/n] " v; printf "REACHED"
SNIP
)
snippet_rsp=$(
    cat <<'SNIP'
read -rsp "Passphrase: " v; printf "REACHED"
SNIP
)
snippet_p=$(
    cat <<'SNIP'
read -p "Choose: " v; printf "REACHED"
SNIP
)
snippet_empty_prompt=$(
    cat <<'SNIP'
read -rp "" v; printf "REACHED"
SNIP
)
snippet_into_flag=$(
    cat <<'SNIP'
printf "v\n" | { read -r flag; printf "flag=[%s]" "$flag"; }
SNIP
)
snippet_into_idx=$(
    cat <<'SNIP'
printf "w\n" | { read -r idx; printf "idx=[%s]" "$idx"; }
SNIP
)

# Attended — nothing changes. This is the case that must not regress: the shadow is on every
# interactive run too, and a mistake here would break every prompt in ccy.
check "attended: a prompt is answered normally" "got=[hello]" \
    "$(run_guard "" "$snippet_prompt")"
check "attended: a non-prompt read still splits" "3" "$(run_guard "" "$snippet_split")"

# Unattended — a prompt is fatal, and NAMES ITSELF, because the journal is the only place an
# operator will read it after a boot.
guard_out="$(run_guard 1 "$snippet_rp")"
check "unattended: a -rp prompt does not fall through" "no" \
    "$(printf '%s' "$guard_out" | grep -q REACHED && echo yes || echo no)"
check "unattended: the fatal names the prompt" "yes" \
    "$(printf '%s' "$guard_out" | grep -q 'Use same configuration' && echo yes || echo "no: $guard_out")"
check "unattended: a bundled -rsp prompt is caught too" "no" \
    "$(printf '%s' "$(run_guard 1 "$snippet_rsp")" | grep -q REACHED && echo yes || echo no)"
check "unattended: a separate -p prompt is caught too" "no" \
    "$(printf '%s' "$(run_guard 1 "$snippet_p")" | grep -q REACHED && echo yes || echo no)"
# An empty prompt still blocks for ever, so the guard fires on the flag, not on the text.
check "unattended: an EMPTY prompt is still fatal" "no" \
    "$(printf '%s' "$(run_guard 1 "$snippet_empty_prompt")" | grep -q REACHED && echo yes || echo no)"

# The other half, and the one that would break ccy if it were wrong: a `read` that is not a
# question to a human must pass through untouched.
check "unattended: a non-prompt read is untouched" "2:x" "$(run_guard 1 "$snippet_split2")"
check "unattended: while-read over a pipe is untouched" "l1l2" "$(run_guard 1 "$snippet_pipe")"

# The shadow must declare NO locals of its own: `builtin read` assigns to the variable the
# CALLER named, so a local with the same name in that scope would swallow the value instead.
# `flag` and `idx` are the guard's own variable names, which makes them the exact collisions
# a careless implementation would produce — and the corruption would be silent, affecting
# only whichever caller happened to pick that name.
check "unattended: a caller reading into 'flag' is not swallowed" "flag=[v]" \
    "$(run_guard 1 "$snippet_into_flag")"
check "unattended: a caller reading into 'idx' is not swallowed" "idx=[w]" \
    "$(run_guard 1 "$snippet_into_idx")"
check "attended: a caller reading into 'flag' is not swallowed" "flag=[v]" \
    "$(run_guard "" "$snippet_into_flag")"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
