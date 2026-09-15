#!/bin/bash
# CCY session registry: which sessions were running when the machine went down.
#
# WHY: Plan 00111 insulated a session from its TERMINAL — the tmux server owns the pty, so a
# dead emulator detaches a session instead of destroying it. A host REBOOT still takes
# everything, and for a session that is meant to run permanently that is the remaining gap.
#
# HOW: `ccy` writes one record here at the moment its container is about to start, and the
# EXIT trap removes it. Anything still recorded at boot was running when the machine went
# down. That is exact, where a shutdown hook is racy and a timer has a window. The record is
# written at the point of no return — after every prompt and every resolution — so a launcher
# killed mid-start leaves nothing, which is correct: a session that never started is not a
# session to restore.
#
# The three properties that make a record safe to act on unattended:
#
#   - A PARTIAL WRITE IS NEVER A RECORD. The writer writes a dotted `.tmp.<pid>` file and
#     `mv`s it into place; same directory, so that is rename(2) and atomic. A reader globs
#     `*.record`, which the temp name cannot match. Belt and braces, the last line is a
#     literal `end=1` terminator the reader REQUIRES, so a write that never went through the
#     rename at all — a hand-edit, a restore from backup — is caught too.
#   - A BAD RECORD IS QUARANTINED, NOT SKIPPED. Validation failure is a refusal with a reason
#     naming the file. Skipping would be the "skip and warn" this repo bans; deleting would
#     destroy the only evidence of why.
#   - THE RESTORE CONFIGURATION IS STORED BY VALUE, NOT ARGV. The quick-launch path supplies
#     the token, keys and network from `.claude/ccy/.last-launch.conf` with an EMPTY argv, so
#     a record built from argv would describe nothing about most real sessions. Argv is kept
#     as evidence only; nothing reads it to build a command.
#
# Records are plain `key=value` lines on purpose — `ccy-sessions restore-status` prints from
# them and an operator debugs from them. That is only safe because a newline in a value is
# REFUSED on write: one would split a field across two lines and silently corrupt every field
# after it. Genuinely multi-valued fields are base64 of a NUL-joined vector, because every
# delimiter is a character someone's path is allowed to contain.
#
# Design, alternatives and the failure modes each decision prevents:
# CLAUDE/Plan/00123-ccy-session-registry-and-reboot-restore/DESIGN-failure-modes.md (D1-D9).
#
# Requires print_error (common-pure.bash, always loaded first). Every function's stdout is
# its return value; diagnostics go to stderr (CLAUDE/StderrHygiene.md).

# The record layout. Bump this when a field's MEANING changes; a reader refuses a schema it
# does not know rather than guessing at an unfamiliar layout.
CCY_REGISTRY_SCHEMA=1

# Mirrors SSH_AGENT_SENTINEL (lib/ssh-handling.bash), which is the source of truth: it is
# `readonly` there and this library is also sourced by ccy-sessions-restore, which has no
# reason to load 1,100 lines of SSH handling. scripts/test-ccy-session-registry.bash greps
# the value out of that file and fails if the two ever disagree, so this copy cannot drift
# silently.
CCY_REGISTRY_SSH_AGENT_SENTINEL="ssh-agent"

# Every key a record must carry to be acted on. Each one decides something: where to start
# the session, whether it is a survivor of an earlier boot (D1), and whether it may be
# restored at all. A record missing any of them cannot be acted on, so it is refused.
CCY_REGISTRY_REQUIRED_KEYS=(project_dir boot_id restore)

# The writer owns these two, so a caller passing either is a caller with a stale idea of the
# format — refused rather than quietly overridden.
CCY_REGISTRY_RESERVED_KEYS=(schema end)

# ── the flag classification ───────────────────────────────────────────────────────────────
#
# DURABLE flags describe the session's SHAPE and are reconstructed for a restore. ONE-SHOT
# flags mean something only for the launch that typed them.
#
# This is the one list in this library that could go stale, so it is the one list with a test
# behind it: scripts/test-ccy-session-registry.bash DERIVES every flag from the launcher's own
# argument parser and fails when one appears in neither array, or in both, or names a flag the
# launcher does not parse. CLAUDE/ContainerRules.md records two incidents in this very program
# where a hand-written list of its own parts was a file short of the truth — one of them for
# two months — so a flag added to `ccy` without a restore decision fails qa-all.bash at commit
# time instead of being dropped from every restored session in silence.
CCY_REGISTRY_DURABLE_FLAGS=(
    --token --ssh-key --ssh-agent --no-ssh --network --no-network
    --github-443 --engine --disable-custom-docker
    --supervise --no-supervise
)
CCY_REGISTRY_ONESHOT_FLAGS=(
    --rebuild --headless --prompt --debug --no-restore
    --create-token --update-token --list-tokens --export-token
    --connect --custom --custom-docker --top --prevent
    --help --version
)

# ccy_registry_flag_class <flag> — `durable`, `oneshot`, `both` or `unknown`, on stdout.
#
# The classification lives here rather than in the test so that the two arrays have a reader
# in the library that owns them, and so the test asserts against the same answer any other
# caller would get. `both` is reported rather than silently preferring one: a flag in both
# arrays is an editing mistake whose consequence — a one-shot flag replayed into every
# restored session — is exactly what the classification exists to prevent.
ccy_registry_flag_class() {
    local flag="${1:?ccy_registry_flag_class requires a flag}" candidate
    local durable=no oneshot=no
    for candidate in "${CCY_REGISTRY_DURABLE_FLAGS[@]}"; do
        [[ "$candidate" == "$flag" ]] && durable=yes
    done
    for candidate in "${CCY_REGISTRY_ONESHOT_FLAGS[@]}"; do
        [[ "$candidate" == "$flag" ]] && oneshot=yes
    done
    if [[ "$durable" == yes && "$oneshot" == yes ]]; then
        printf 'both\n'
    elif [[ "$durable" == yes ]]; then
        printf 'durable\n'
    elif [[ "$oneshot" == yes ]]; then
        printf 'oneshot\n'
    else
        printf 'unknown\n'
    fi
}

# ── where the state lives ─────────────────────────────────────────────────────────────────

# ccy_registry_root — the state root. XDG, so it is per-user and survives a reboot.
ccy_registry_root() {
    printf '%s\n' "${XDG_STATE_HOME:-$HOME/.local/state}/ccy"
}

# ccy_registry_sessions_dir — the live records: one file per tmux session.
ccy_registry_sessions_dir() {
    printf '%s/sessions\n' "$(ccy_registry_root)"
}

# ccy_registry_restore_dir — the restore service's own tree: attempted/, retired/,
# malformed/ and the last-run note.
ccy_registry_restore_dir() {
    printf '%s/restore\n' "$(ccy_registry_root)"
}

# ccy_registry_mkdir <dir> — create it 0700. A record names project directories and token
# NAMES (never a token value), but a path is still information about this machine.
ccy_registry_mkdir() {
    local dir="${1:?ccy_registry_mkdir requires a directory}" err
    if ! err=$(mkdir -p "$dir" 2>&1); then
        print_error "could not create $dir: $err"
        return 1
    fi
    # The mode is applied in a separate step, and only to the leaf. `mkdir -p -m` would apply
    # it to the deepest directory alone anyway (SC2174), and the parents here are shared XDG
    # directories — tightening ~/.local/state to 0700 would be this library reaching well
    # outside what it owns.
    if ! err=$(chmod 700 "$dir" 2>&1); then
        print_error "could not set permissions on $dir: $err"
        return 1
    fi
}

# ── identity ──────────────────────────────────────────────────────────────────────────────

# ccy_registry_boot_id — this boot's kernel id, on stdout.
#
# A record carries the boot it was written under, so the restore can tell a SURVIVOR of an
# earlier boot from a session that is running right now (D1). Without it, a restore run by
# hand mid-session would start a second claude on a live conversation — the one thing Plan
# 00111's single-attach rule exists to make impossible.
ccy_registry_boot_id() {
    local id
    if ! id=$(<"/proc/sys/kernel/random/boot_id"); then
        print_error "could not read /proc/sys/kernel/random/boot_id"
        return 1
    fi
    if [[ -z "$id" ]]; then
        print_error "/proc/sys/kernel/random/boot_id was empty"
        return 1
    fi
    printf '%s\n' "$id"
}

# ccy_registry_boot_time — the epoch second the CURRENT boot started, from /proc/stat's btime.
#
# This is what "stale" has to be measured against, and getting it wrong was a real defect: age
# was measured from the record's own mtime, which is when the SESSION STARTED. A session running
# permanently for a fortnight — the exact case this whole feature exists for — was therefore
# retired as stale at the reboot it was supposed to survive, while a session started an hour
# before a reboot six months ago was not.
#
# The question "is this record left over from some long-ago boot" is about WHEN ITS BOOT WAS,
# not how long its session had been running, so the record carries its boot's start time and the
# comparison is boot-to-boot.
ccy_registry_boot_time() {
    local btime
    if ! btime=$(awk '$1 == "btime" { print $2; found = 1; exit } END { exit found ? 0 : 1 }' /proc/stat); then
        print_error "could not read btime from /proc/stat"
        return 1
    fi
    printf '%s\n' "$btime"
}

# ccy_registry_slug <path> — a filesystem-safe, collision-free name for a project DIRECTORY.
#
# Keyed on the directory, never on the project name. lib/tmux-session.bash records why: two
# checkouts of one repository share a project name, so anything keyed on the name conflates
# them — and project "app" would claim a slug belonging to a project genuinely called
# "app-2". The readable half is the sanitised basename; the hash of the FULL path is what
# makes two same-named directories distinct.
ccy_registry_slug() {
    local path="${1:?ccy_registry_slug requires a path}" base digest
    base=$(basename "$path")
    base="${base//[^A-Za-z0-9._-]/_}"
    if ! digest=$(printf '%s' "$path" | sha256sum | cut -c1-12); then
        print_error "could not hash the path $path"
        return 1
    fi
    printf '%s-%s\n' "${base:-root}" "$digest"
}

# ccy_registry_fingerprint <dir> — the project's ROOT COMMIT, or the sentinel `no-commits`.
#
# Claude's conversation state lives in the project's own .claude/ccy, so a directory deleted
# and re-used for a different repository would hand the new project the old one's conversation
# on `--continue`. The root commit is what distinguishes them: it survives rebases, branch
# switches, remote renames and re-clones, and differs between unrelated repositories (D7).
#
# A directory that is not a git work tree is REFUSED, not given a value: `ccy` itself refuses
# to run outside a repository, so a fingerprint invented here would describe a session that
# could never start.
ccy_registry_fingerprint() {
    local dir="${1:?ccy_registry_fingerprint requires a directory}" probe root
    if ! probe=$(git -C "$dir" rev-parse --is-inside-work-tree 2>&1); then
        print_error "$dir is not a git work tree: $probe"
        return 1
    fi
    if [[ "$probe" != "true" ]]; then
        print_error "$dir is not a git work tree (rev-parse said '$probe')"
        return 1
    fi
    # A repository with no commits has no root commit. That is a normal state for a freshly
    # initialised project, so it gets a sentinel rather than a refusal — and the comparison
    # at restore time runs only when BOTH sides have a real hash, so a project making its
    # first commit is not mistaken for a different project.
    if ! probe=$(git -C "$dir" rev-parse --verify HEAD 2>&1); then
        printf 'no-commits\n'
        return 0
    fi
    if ! root=$(git -C "$dir" rev-list --max-parents=0 HEAD 2>&1); then
        print_error "could not resolve the root commit of $dir: $root"
        return 1
    fi
    # An octopus-rooted history has several root commits, one per line. The first is stable
    # for a given repository, which is all this needs.
    printf '%s\n' "${root%%$'\n'*}"
}

# ── argv encoding: faithful for any value, including spaces and newlines ──────────────────

# ccy_registry_encode_argv [args...] — base64 of the NUL-joined vector, on stdout. Empty for
# no arguments, so an absent value and an empty vector look the same to a reader, which is
# what they mean.
ccy_registry_encode_argv() {
    if [[ $# -eq 0 ]]; then
        return 0
    fi
    local encoded
    if ! encoded=$(printf '%s\0' "$@" | base64 -w0); then
        print_error "could not base64-encode an argument vector"
        return 1
    fi
    printf '%s\n' "$encoded"
}

# ccy_registry_decode_argv <base64> — the vector back, NUL-delimited on stdout. Read it with
# `mapfile -t -d ''`; a newline delimiter could not carry a value containing one.
ccy_registry_decode_argv() {
    local encoded="${1-}" err
    if [[ -z "$encoded" ]]; then
        return 0
    fi
    if ! err=$(printf '%s' "$encoded" | base64 -d 2>&1 >&3); then
        print_error "could not base64-decode an argument vector: $err"
        return 1
    fi
} 3>&1

# ── writing a record ──────────────────────────────────────────────────────────────────────

# ccy_registry_write <dir> <name> <key>=<value> [...] — write one record atomically.
#
# Fields are written in the order given, after the schema line and before the terminator, so
# a record reads top-to-bottom the way it was assembled.
ccy_registry_write() {
    local dir="${1:?ccy_registry_write requires a directory}"
    local name="${2:?ccy_registry_write requires a record name}"
    shift 2

    if [[ "$name" == *"/"* || "$name" == *$'\n'* || "$name" == .* ]]; then
        print_error "invalid record name '$name': no slashes, newlines or leading dots"
        return 1
    fi

    local -a required_seen=() seen_keys=()
    local body="" pair key value reserved req
    for pair in "$@"; do
        if [[ "$pair" != *"="* ]]; then
            print_error "invalid field '$pair': expected key=value"
            return 1
        fi
        key="${pair%%=*}"
        value="${pair#*=}"
        if [[ ! "$key" =~ ^[a-z][a-z0-9_]*$ ]]; then
            print_error "invalid field key '$key': lower-case letters, digits and underscores only"
            return 1
        fi
        for reserved in "${CCY_REGISTRY_RESERVED_KEYS[@]}"; do
            if [[ "$key" == "$reserved" ]]; then
                print_error "field '$key' is written by the registry itself and cannot be passed in"
                return 1
            fi
        done
        # The whole record format rests on this refusal. A newline would split one field
        # across two lines, so every field after it would parse as something else — a record
        # that looks valid and describes a different session.
        if [[ "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
            print_error "field '$key' contains a newline, which a line-based record cannot carry"
            return 1
        fi
        # A repeated key is ambiguous: the reader takes the first, so a caller that meant the
        # second would get a record describing a session it did not launch — silently, because
        # both lines are individually well-formed. Refusing is the only answer that cannot be
        # read two ways.
        if [[ " ${seen_keys[*]-} " == *" $key "* ]]; then
            print_error "field '$key' was given more than once; a record holds one value per key"
            return 1
        fi
        seen_keys+=("$key")
        for req in "${CCY_REGISTRY_REQUIRED_KEYS[@]}"; do
            if [[ "$key" == "$req" ]]; then
                required_seen+=("$key")
            fi
        done
        body+="${key}=${value}"$'\n'
    done

    for req in "${CCY_REGISTRY_REQUIRED_KEYS[@]}"; do
        if [[ " ${required_seen[*]-} " != *" $req "* ]]; then
            print_error "cannot write record '$name': required field '$req' is missing"
            return 1
        fi
    done

    ccy_registry_mkdir "$dir" || return 1

    # Dotted AND suffixed, so an abandoned in-flight write is invisible to `*.record` for two
    # independent reasons. $$ keeps two concurrent writers off each other's temp file.
    local tmp="${dir}/.${name}.record.tmp.$$" err
    # `2>&1 >"$tmp"` in THIS order: stderr is redirected to the substitution's stdout (captured
    # into err) and only then is stdout sent to the file. Written the other way round — as it
    # was — the file receives both, so the diagnostic ends up INSIDE the record and `err` is
    # always empty, leaving a write failure reported with no reason attached to it.
    if ! err=$( { printf 'schema=%s\n%send=1\n' "$CCY_REGISTRY_SCHEMA" "$body"; } 2>&1 >"$tmp"); then
        print_error "could not write $tmp: $err"
        rm -f "$tmp"
        return 1
    fi
    if ! err=$(chmod 600 "$tmp" 2>&1); then
        print_error "could not set permissions on $tmp: $err"
        rm -f "$tmp"
        return 1
    fi
    # rename(2), because the temp file is in the same directory: a concurrent reader sees
    # either the previous record or this one, never a mixture of the two.
    if ! err=$(mv -f "$tmp" "${dir}/${name}.record" 2>&1); then
        print_error "could not install ${dir}/${name}.record: $err"
        rm -f "$tmp"
        return 1
    fi
}

# ccy_registry_remove <dir> <name> — drop a record. Called from the launcher's EXIT trap, so
# it must not fail: `rm -f` succeeds on an absent file, and absent is the correct terminal
# state, so this cannot disturb the exit status the trap is preserving.
ccy_registry_remove() {
    local dir="${1:?ccy_registry_remove requires a directory}"
    local name="${2:?ccy_registry_remove requires a record name}"
    rm -f "${dir}/${name}.record"
}

# ── reading a record ──────────────────────────────────────────────────────────────────────

# _ccy_registry_raw_field <file> <key> — the value on stdout, or return 1 when the key is
# absent, saying nothing either way. The silent half of ccy_registry_field, so the defaulting
# path below needs no output redirect.
_ccy_registry_raw_field() {
    local file="$1" key="$2" value
    if ! value=$(awk -v k="$key" '
        index($0, k "=") == 1 { print substr($0, length(k) + 2); found = 1; exit }
        END { exit found ? 0 : 1 }
    ' "$file"); then
        return 1
    fi
    printf '%s\n' "$value"
}

# ccy_registry_field <file> <key> — the value on stdout.
#
# An absent key returns non-zero and says so. It does NOT return an empty string: "this
# session has no token" and "this record has no token field" are different facts, and a
# caller that cannot tell them apart would restore a session with no token and call it a
# success.
ccy_registry_field() {
    local file="${1:?ccy_registry_field requires a file}"
    local key="${2:?ccy_registry_field requires a key}"
    if [[ ! -f "$file" ]]; then
        print_error "no such record: $file"
        return 1
    fi
    local value
    if ! value=$(_ccy_registry_raw_field "$file" "$key"); then
        print_error "record $file has no field '$key'"
        return 1
    fi
    printf '%s\n' "$value"
}

# ccy_registry_field_default <file> <key> <default> — the value, or the default when the key
# is absent. For fields whose absence has a defined meaning (no network, no token), where the
# default IS the answer rather than a guess.
ccy_registry_field_default() {
    local file="${1:?ccy_registry_field_default requires a file}"
    local key="${2:?ccy_registry_field_default requires a key}"
    local default="${3-}" value
    if value=$(_ccy_registry_raw_field "$file" "$key"); then
        printf '%s\n' "$value"
    else
        printf '%s\n' "$default"
    fi
}

# ccy_registry_validate <file> — is this a record this code may act on?
#
# Every failure names the file and the reason, because at boot the only place an operator will
# read it is the journal. A caller MUST treat a refusal as "quarantine and report", never as
# "skip": absence of a check is not a passing check.
ccy_registry_validate() {
    local file="${1:?ccy_registry_validate requires a file}" schema key last
    if [[ ! -f "$file" ]]; then
        print_error "$file: not a regular file, so it is not a record"
        return 1
    fi
    # Checked FIRST, because it is the only check that can see a truncated write that never
    # went through the atomic rename. Everything below it would happily pass on a record whose
    # last half is missing.
    last=$(awk 'END { print }' "$file")
    if [[ "$last" != "end=1" ]]; then
        print_error "$file: incomplete record — the 'end=1' terminator is missing, so the write did not finish"
        return 1
    fi
    if ! schema=$(_ccy_registry_raw_field "$file" schema); then
        print_error "$file: no schema field, so its layout is unknown"
        return 1
    fi
    if [[ "$schema" != "$CCY_REGISTRY_SCHEMA" ]]; then
        print_error "$file: schema $schema is not $CCY_REGISTRY_SCHEMA — written by a different version of ccy, so its fields cannot be trusted to mean what this code expects"
        return 1
    fi
    for key in "${CCY_REGISTRY_REQUIRED_KEYS[@]}"; do
        if ! _ccy_registry_raw_field "$file" "$key" >/dev/null; then
            print_error "$file: required field '$key' is missing"
            return 1
        fi
    done
}

# ccy_registry_collect <dir> <array-name> — fill the named array with the directory's records,
# sorted; return non-zero when the listing itself failed. The only listing API.
#
# It is NOT built on `mapfile < <(some_streaming_lister)`, and that is the whole point. A process
# substitution's exit status is not observable, so a registry this could not READ produced an
# empty array and each caller read that as "there are no records" — which in the boot service
# meant exiting 0 with "nothing to restore" on a machine whose sessions it never saw. That is the
# "could not tell" / "nothing to do" collapse, in the one place with nobody watching.
#
# The find writes to a temp FILE rather than a command substitution because the output is
# NUL-delimited — that delimiter chosen precisely because a path may contain anything else — and
# `$(…)` strips NUL bytes, silently gluing every record path into one.
#
# The caller names its own array rather than reading a shared global: one function, no global to
# clobber between two consumers, and nothing kept alive merely to have something read it. Two
# consequences of the nameref are worth knowing: it cannot write through `$( … )`, which is a
# subshell, and the names `_ccy_collect_dir`, `_ccy_collect_out`, `_ccy_collect_tmp` and
# `_ccy_collect_err` are RESERVED — passing one as the array name would have the function write
# to its own local and return an empty array with no error.
#
# Globs `*.record` only, so an in-flight `.tmp.<pid>` write is never offered to a reader. A
# directory that does not exist yields an empty array and SUCCEEDS — `ccy` has simply never
# recorded a session here, which is a real answer. A directory that exists and cannot be read is
# a FAILURE, and the two must never look alike.
ccy_registry_collect() {
    local _ccy_collect_dir="${1:?ccy_registry_collect requires a directory}"
    local -n _ccy_collect_out="${2:?ccy_registry_collect requires an array name}"
    local _ccy_collect_tmp _ccy_collect_err
    _ccy_collect_out=()
    [[ -d "$_ccy_collect_dir" ]] || return 0
    if ! _ccy_collect_tmp=$(mktemp 2>&1); then
        print_error "could not create a temporary file to list $_ccy_collect_dir: $_ccy_collect_tmp"
        return 1
    fi
    # Sorted, so a report reads the same way twice and a diff of two runs means something;
    # readdir order is whatever the filesystem feels like. LC_ALL=C so the order does not move
    # between machines with different collations.
    #
    # `set -o pipefail` INSIDE the subshell, and it is load-bearing. The find is now stage one of
    # a pipeline, so without it a failing find is masked by a succeeding sort and this returns 0
    # with no records — the precise collapse this function's header forbids. It cannot be left to
    # the caller: this library sets no shell options by design, the launcher runs `set -e` alone,
    # and a guarantee that depends on an option the caller happens to have set is not a guarantee.
    _ccy_collect_err=$(
        set -o pipefail
        { find "$_ccy_collect_dir" -maxdepth 1 -name '*.record' -type f -print0 |
            LC_ALL=C sort -z; } 2>&1 >"$_ccy_collect_tmp"
    ) || {
        print_error "could not list records in $_ccy_collect_dir: $_ccy_collect_err"
        rm -f "$_ccy_collect_tmp"
        return 1
    }
    # find exits 0 having still reported a per-entry failure on stderr (an unreadable
    # subdirectory, a racing unlink), so a clean exit status alone is not a complete listing.
    if [[ -n "$_ccy_collect_err" ]]; then
        print_error "could not fully list records in $_ccy_collect_dir: $_ccy_collect_err"
        rm -f "$_ccy_collect_tmp"
        return 1
    fi
    if [[ -s "$_ccy_collect_tmp" ]]; then
        mapfile -t -d '' _ccy_collect_out <"$_ccy_collect_tmp"
    fi
    rm -f "$_ccy_collect_tmp"
}


# ccy_registry_retire <file> <dest-dir> <reason> — move a record out of the live set, with the
# reason recorded inside it.
#
# A record the restore will not act on is never simply deleted. Deleting it destroys the only
# evidence of why a session did not come back, and the operator is left with a machine that
# restored three of four sessions and no way to learn which one. The reason line goes BEFORE
# the terminator, so a retired record is still a valid record and still readable by everything
# above.
ccy_registry_retire() {
    local file="${1:?ccy_registry_retire requires a file}"
    local dest="${2:?ccy_registry_retire requires a destination directory}"
    local reason="${3:?ccy_registry_retire requires a reason}"
    if [[ "$reason" == *$'\n'* ]]; then
        print_error "a retirement reason cannot contain a newline"
        return 1
    fi
    ccy_registry_mkdir "$dest" || return 1
    local name tmp err
    name=$(basename "$file")
    tmp="${dest}/.${name}.tmp.$$"
    # Two corrections in one line, both of which made this silently succeed on a failed read —
    # inside the consume-before-start step the whole no-loop guarantee rests on.
    #
    # `awk`, not `grep -v`: grep exits 1 when it selects no lines, so an edge-case record would
    # have been reported as a failure it was not.
    #
    # `2>&1 >"$tmp"` in THIS order: written `>"$tmp" 2>&1`, both streams went to the file, so a
    # failing read had its error text written INTO the new record, `err` was empty, and the
    # group's status was the trailing printf's — always 0. The result was a well-formed record
    # containing a diagnostic, and a success return.
    if ! err=$( { awk '$0 != "end=1"' "$file" &&
        printf 'retired_reason=%s\nend=1\n' "$reason"; } 2>&1 >"$tmp"); then
        print_error "could not stage the retired record $tmp: $err"
        rm -f "$tmp"
        return 1
    fi
    if ! err=$(mv -f "$tmp" "${dest}/${name}" 2>&1); then
        print_error "could not install the retired record ${dest}/${name}: $err"
        rm -f "$tmp"
        return 1
    fi
    if ! err=$(rm -f "$file" 2>&1); then
        print_error "retired record kept at ${dest}/${name} but the original $file could not be removed: $err"
        return 1
    fi
}

# ── reconstructing the launch ─────────────────────────────────────────────────────────────

# ccy_registry_restore_flags <file> — the argument vector for restoring this session,
# NUL-delimited on stdout. Read it with `mapfile -t -d ''`.
#
# Built from the record's RESOLVED configuration, not from its recorded argv — see the header:
# a session started by accepting quick launch has an argv that describes none of its
# configuration.
#
# `--supervise` and `--continue` are unconditional and are the whole point: the first re-arms
# the PTY supervisor so the restored session is nudged back to work, the second makes it the
# SAME conversation rather than a fresh one. A record marked no-restore produces nothing at
# all — the restore service checks that too, but a value this consequential is refused at both
# layers.
ccy_registry_restore_flags() {
    local file="${1:?ccy_registry_restore_flags requires a file}"
    ccy_registry_validate "$file" || return 1

    local restore
    restore=$(ccy_registry_field_default "$file" restore no)
    if [[ "$restore" != "yes" ]]; then
        print_error "$file is marked restore=$restore, so it has no restore command line"
        return 1
    fi

    local -a flags=()
    local token keys no_ssh network no_network gh443 engine no_custom key

    token=$(ccy_registry_field_default "$file" token_name "")
    if [[ -n "$token" ]]; then
        flags+=(--token "$token")
    fi

    keys=$(ccy_registry_field_default "$file" ssh_keys_b64 "")
    if [[ -n "$keys" ]]; then
        local -a key_list=()
        # Decoded TWICE, deliberately: once to check the status, once for the data.
        #
        # Read straight from a process substitution the decode status is invisible, so a corrupt
        # ssh_keys_b64 yielded an EMPTY key list and a session restored with no SSH keys and no
        # complaint — while the caller's "did this produce any flags at all" guard still passed,
        # because --supervise and --continue are appended unconditionally.
        #
        # And it cannot be captured into a variable instead: the payload is NUL-delimited, and
        # `$(…)` strips NUL bytes — which silently glued every key path into one. The check is a
        # separate pass for that reason. `>/dev/null` here discards the DATA, which this pass
        # does not want; stderr and the exit status both still flow.
        if ! ccy_registry_decode_argv "$keys" >/dev/null; then
            print_error "$file: ssh_keys_b64 could not be decoded, so this session's SSH keys are unknown"
            return 1
        fi
        mapfile -t -d '' key_list < <(ccy_registry_decode_argv "$keys")
        for key in "${key_list[@]}"; do
            [[ -n "$key" ]] || continue
            # The forwarded-agent sentinel shares the SSH_KEYS array with real key paths
            # (lib/ssh-handling.bash), so it has to be recognised here rather than passed on
            # as a filename that does not exist.
            if [[ "$key" == "$CCY_REGISTRY_SSH_AGENT_SENTINEL" ]]; then
                flags+=(--ssh-agent)
            else
                flags+=(--ssh-key "$key")
            fi
        done
    fi

    no_ssh=$(ccy_registry_field_default "$file" no_ssh no)
    [[ "$no_ssh" == "yes" ]] && flags+=(--no-ssh)

    network=$(ccy_registry_field_default "$file" network "")
    [[ -n "$network" ]] && flags+=(--network "$network")

    no_network=$(ccy_registry_field_default "$file" no_network no)
    [[ "$no_network" == "yes" ]] && flags+=(--no-network)

    gh443=$(ccy_registry_field_default "$file" github_443 no)
    [[ "$gh443" == "yes" ]] && flags+=(--github-443)

    engine=$(ccy_registry_field_default "$file" engine "")
    [[ -n "$engine" ]] && flags+=(--engine "$engine")

    no_custom=$(ccy_registry_field_default "$file" disable_custom_docker no)
    [[ "$no_custom" == "yes" ]] && flags+=(--disable-custom-docker)

    # The supervisor mode is HONOURED, not overridden.
    #
    # Issue 44 asks for restore with `--supervise`, and that is right for the default case: an
    # unattended session needs the armed supervisor's nudge to get back to work, and the
    # default is unarmed. But `ccy --no-supervise` is an explicit opt-out of the supervisor
    # ENTIRELY, ctrl+z guard included — silently arming it on a restored session would hand the
    # operator auto-compaction and goal injection they deliberately turned off. So the mode is
    # recorded at launch and replayed here; only a session that expressed no preference gets
    # the arming this feature exists to provide.
    case "$(ccy_registry_field_default "$file" supervise default)" in
    off) flags+=(--no-supervise) ;;
    *) flags+=(--supervise) ;;
    esac
    flags+=(--continue)
    printf '%s\0' "${flags[@]}"
}

# ── the unattended guard ──────────────────────────────────────────────────────────────────

# ccy_unattended_fatal <what-was-being-decided> — die, naming the decision.
#
# Called by the launcher's `read` shadow when a human prompt is reached with CCY_UNATTENDED
# set. A detached tmux session still has a pty, so every terminal check in the launcher passes
# and every prompt behaves as if someone were watching — with nobody to answer, the session
# would sit there for ever: present in `ccy-sessions`, `claude` never started, the supervisor
# that was meant to nudge it back to work never running. A restore that silently produces a
# parked shell is worse than no restore, because the operator believes their work resumed.
#
# So this exits. The prompt text lands in the journal, which is the one place an operator will
# look after a boot.
ccy_unattended_fatal() {
    local what="${1:-an interactive decision}"
    print_error "this session is running unattended (CCY_UNATTENDED=1) and cannot answer: ${what}"
    echo "Nothing was started. Attach a terminal and run ccy here to answer it, or resolve the underlying condition." >&2
    exit 78
}
