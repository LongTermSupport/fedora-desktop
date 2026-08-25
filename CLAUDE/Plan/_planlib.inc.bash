# shellcheck shell=bash
#
# _planlib.inc.bash — sourced helper library for plan-folder orchestrators.
#
# WHAT THIS IS
#   The one tested implementation of the safety-critical primitives a plan's
#   `deploy.bash` / `verify.bash` / `triage.bash` / `acceptance.bash` needs:
#   script-relative repo-root resolution, the host-vs-container guard, sudo
#   priming, a tee'd run log with a deterministic drain, /dev/tty prompts, the
#   change gate, and fail-fast-vs-continue leg semantics. A conforming
#   orchestrator is BOOTSTRAP + MODE + LEGS and hand-rolls none of it.
#
#   Standards, rule by rule: ../PlanScriptStandards.md
#   Tests:                   ../../scripts/test-planlib.bash
#   Sibling implementation:  lts-infra's CLAUDE/Plan/_planlib.inc.bash
#   Donor of the concept:    ballicom-infra's CLAUDE/Plan/_planlib.bash
#
# THE INCIDENT THIS EXISTS TO PREVENT
#   Plan 00068's triage.bash resolved its repo root with
#   `git rev-parse --show-toplevel` — following this repo's own PlanWorkflow.md,
#   which recommended it. `git rev-parse` is CWD-relative, NOT script-relative.
#   The operator ran the script by path from a DIFFERENT repo's root, so it
#   resolved to that repo, wrote its report there, and the probe meant to detect
#   deployed-vs-checkout launcher drift compared against a path that does not
#   exist there — reporting "Could not checksum both files" instead of failing.
#   The guidance was followed faithfully, so fixing that one script fixed nothing.
#
#   `_plan_find_repo_root` walks up from the CALLING SCRIPT's own directory, uses
#   the filesystem only (never `git`), and STOPS AT THE REPOSITORY BOUNDARY.
#
#   The boundary bound matters MORE here than anywhere. This repo is routinely
#   checked out INSIDE another one (lts-infra keeps it at
#   untracked/repos/fedora-desktop), and both repos have an `ansible.cfg` at their
#   root. An unbounded walk from a plan script here would sail past this repo and
#   find LTS-INFRA's marker — then read its files, write reports into it, and
#   appear to work. Failing loudly beats operating on the wrong repo.
#
# CANONICAL BOOTSTRAP (copy verbatim; see PlanScriptStandards.md R1)
#
#   #!/usr/bin/env bash
#   set -euo pipefail
#   scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
#   repoRoot="${scriptDir}"
#   while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
#     if [[ -e "${repoRoot}/.git" ]]; then
#       printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
#       exit 1
#     fi
#     repoRoot="$(dirname "${repoRoot}")"
#   done
#   [[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
#   # shellcheck source-path=SCRIPTDIR
#   # shellcheck source=../_planlib.inc.bash
#   source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
#   plan_init "${BASH_SOURCE[0]}"
#
#   Both shellcheck lines are source DIRECTIVES, not suppressions: source-path
#   makes the relative path resolve from the script's own dir rather than from
#   the linter's cwd. Without it, `-x` cannot follow this file and every check
#   that depends on following it silently lapses. (A comment line must not START
#   with the word shellcheck either — the linter reads that as a directive.)
#
# WHERE PLAN SCRIPTS RUN — THE HOST, NOT THE CCY CONTAINER
#   Claude works inside a CCY podman container that bind-mounts this repo. It
#   AUTHORS and VALIDATES plan scripts; the HUMAN runs them on the host. This is
#   not a formality:
#
#     A nested `podman` result is not evidence about the host. An attempt to
#     answer "is a missing --device fatal?" from inside the container failed with
#     a userns/subuid error during an image pull, BEFORE it ever reached the
#     device check — a non-zero exit that says nothing whatsoever about the
#     question asked. Run on the host, the same probe answered it definitively.
#
#   `plan_require_host` turns that lesson into an enforced guard instead of a
#   comment nobody reads. A script whose findings would be meaningless from inside
#   a container calls it first and refuses to produce a confident wrong answer.
#
# ANSIBLE HERE IS PLAIN AND LOCAL
#   Unlike lts-infra (which must go through shellscripts/ansible-run.bash for
#   vault-id, inventory and a vaulted connect key), this repo's ansible.cfg
#   already supplies `inventory = ./environment/localhost`,
#   `vault_password_file = ./vault-pass.secret`, `vault_identity = localhost` and
#   `transport = local`. So `plan_ansible_playbook` simply runs `ansible-playbook`
#   from the repo root. There is no wrapper to delegate to and inventing one would
#   be scope creep — but the cd-to-root IS load-bearing, because every one of
#   those ansible.cfg paths is relative to it.
#
# SUDO, NOT SSH
#   Plays run against localhost with `become`, so there is no ssh-agent to load.
#   The equivalent hazard is the SUDO password prompt: issued after the tee
#   redirect it is flooded and garbled, exactly as a passphrase prompt would be.
#   `plan_prime_sudo` primes the sudo timestamp on /dev/tty BEFORE the log opens,
#   and the ordering is enforced rather than documented.
#
# RUN LOGS ARE NOT COMMITTED
#   ballicom-infra pipes its run log through a secret scrubber before committing
#   it — committed run logs are valuable BECAUSE the scrubber makes them safe.
#   This repo has no scrubber, and a plan run can stream vault-decrypted values.
#   So logs land in `<script>-runs/<timestamp>/`, CLAUDE/Plan/.gitignore excludes
#   `*-runs/`, and `_plan_finalize_log` says UNSCRUBBED on every run. Shipping the
#   donor's shape with the scrub step silently omitted would be a control that
#   degrades to a no-op — worse than an absent one, because people build on it.
#
# STDERR HYGIENE (CLAUDE/StderrHygiene.md)
#   Every function that emits a CAPTURED VALUE keeps stdout pure and puts its
#   diagnostics on stderr: `_plan_find_repo_root`, `_plan_strip_cr`,
#   `_plan_in_container`. The banners, leg headers and prompts DO use stdout, and
#   that is correct under the same document's explicit carve-out: a plan
#   orchestrator's entire job is to print a run report for a human, and nothing
#   captures it — "the human report is the payload". Do not "fix" them to stderr.
#
# DELIBERATE DEVIATION: THREE FUNCTIONS CALL `exit`
#   `plan_deploy_leg`, `plan_finish` and `plan_parse_common_flags --help`. For the
#   legs "abort the whole run" IS the contract, and delegating it to the caller
#   (`|| exit 1` at every call site) reintroduces the exact catastrophe the
#   primitive exists to prevent — one forgotten guard and a failed leg flows on to
#   the next. The library still sets NO shell options; the caller owns those.
#   scripts/test-planlib.bash pins the deviation to exactly those three.
#
# TTY BEHAVIOUR MUST BE CHECKED BY HAND (a tty cannot be faked from a container)
#   The tests cover the no-tty paths via `setsid`. On a real terminal, after
#   touching plan_confirm / plan_prime_sudo / plan_start_log, check:
#     1. `sudo -k` then run a script calling plan_prime_sudo: ONE clean sudo
#        prompt BEFORE any tee'd output.
#     2. Re-run immediately: "already primed", NO second prompt.
#     3. A gated script: the confirmation appears AFTER the banner (ordered), a
#        wrong/empty answer aborts before the first mutating leg, -y skips it.
#     4. Ctrl-C mid-run: the log still holds every line up to the interrupt.

# Guard against double-sourcing. `return` at file scope is valid because this
# block only ever runs while being sourced.
if [[ -n "${PLANLIB_SOURCED:-}" ]]; then
    return 0
fi

PLANLIB_VERSION="1.0.0"
PLANLIB_SOURCED=1
export PLANLIB_VERSION

# ── run state ────────────────────────────────────────────────────────────────────────────
PLAN_SCRIPT_DIR=""
PLAN_REPO_ROOT=""
PLAN_MODE=""
PLAN_LOG_STARTED=0
PLAN_RUN_DIR=""
PLAN_RUN_LOG=""
PLAN_GATE_PASSED=0
PLAN_SUDO_PRIMED=0
PLAN_CHECK="${PLAN_CHECK:-0}"
PLAN_ASSUME_YES="${PLAN_ASSUME_YES:-0}"
PLAN_USAGE="${PLAN_USAGE:-}"
# Reason the /dev/tty open probe failed, so a fatal message can quote it instead of
# leaving the operator guessing.
PLAN_TTY_PROBE_ERR=""
# tee-drain plumbing: the PID of the background log writer, plus a once-guard so the
# finalize handler cannot run twice (EXIT after a signal).
PLAN_TEE_PID=""
PLAN_TRAP_DONE=0
# Space-separated names of gather legs that failed; drives the final exit code.
PLAN_FAILED_LEGS=""
# Extra args threaded into every ansible invocation (currently just --check).
PLAN_CHECK_ARGS=()
# Args left over after plan_parse_common_flags consumes the shared vocabulary.
PLAN_REMAINING_ARGS=()
# Filesystem markers that mean "this process is inside a container". Overridable so the
# tests can exercise both branches; podman writes /run/.containerenv, docker /.dockerenv.
PLAN_CONTAINER_MARKERS=(/run/.containerenv /.dockerenv)

# Exported so a plan script (or a play it invokes) can read the run's declared state without
# re-deriving it. PLAN_SUDO_PRIMED in particular lets a leg decide whether an unattended
# `become` task can be expected to succeed.
export PLAN_MODE PLAN_CHECK PLAN_GATE_PASSED PLAN_SUDO_PRIMED

# ── internals: loud failure and banners ──────────────────────────────────────────────────

# _plan_err <message> — print loudly and return 1. It never calls `exit`, so callers choose
# between fatal (deploy) and record-and-continue (gather), and every path stays testable.
# Call it as `_plan_err "..." || return 1`: that form is errexit-safe, whereas a bare call
# followed by `return 1` would abort the caller before the return.
_plan_err() {
    printf '[FATAL] %s\n' "$*" >&2
    return 1
}

_plan_banner() {
    printf '============================================================\n'
    printf '==> %s\n' "$*"
    printf '============================================================\n'
}

# _plan_tty_openable — can the controlling terminal actually be OPENED? Existence is not
# enough: /dev/tty exists even with no controlling terminal (e.g. under `setsid </dev/null`),
# where opening it fails with ENXIO. The probe CAPTURES the failure reason into
# PLAN_TTY_PROBE_ERR rather than discarding it, so the fatal message can quote it. `true` (a
# regular builtin) is used rather than `:` or `exec` so a failed redirect cannot take the
# shell down.
_plan_tty_openable() {
    local probeErr=""
    if probeErr="$( { true >/dev/tty; } 2>&1 )"; then
        PLAN_TTY_PROBE_ERR=""
        return 0
    fi
    PLAN_TTY_PROBE_ERR="${probeErr}"
    return 1
}

# ── pure helpers (no process IO; exercised directly by the tests) ─────────────────────────

# _plan_find_repo_root <start_dir> — walk up from <start_dir> to this repo's ansible.cfg,
# filesystem-only, BOUNDED BY THE REPOSITORY BOUNDARY. Echoes the root, or returns 1.
#
# Order matters: the marker is tested BEFORE the boundary, because a repo root holds both.
# The boundary test is a plain -e so it catches a worktree's `.git` FILE as well as a normal
# `.git` directory — and involves no `git` command, which is the point.
_plan_find_repo_root() {
    local dir="$1"
    while [[ "${dir}" != "/" ]]; do
        if [[ -e "${dir}/ansible.cfg" ]]; then
            printf '%s' "${dir}"
            return 0
        fi
        if [[ -e "${dir}/.git" ]]; then
            return 1
        fi
        dir="$(dirname "${dir}")"
    done
    return 1
}

# _plan_strip_cr <string> — drop a single TRAILING carriage return. A terminal in raw/mixed
# mode can leave a CR on a reply, which silently breaks an exact token match.
_plan_strip_cr() {
    local s="$1"
    printf '%s' "${s%$'\r'}"
}

# _plan_in_container <marker>... — pure predicate over the marker paths given. Echoes the
# first marker found and returns 0; returns 1 if none exist. Markers are arguments rather
# than hardcoded so BOTH branches are testable — a container check that can only be exercised
# one way is a check nobody has actually verified.
_plan_in_container() {
    local m
    for m in "$@"; do
        if [[ -e "${m}" ]]; then
            printf '%s' "${m}"
            return 0
        fi
    done
    return 1
}

# _plan_mode_allows_leg <mode> <legkind> — pure state machine. deploy mode allows only
# deploy legs; gather mode allows only gather legs.
_plan_mode_allows_leg() {
    local mode="$1" legkind="$2"
    [[ "${mode}" == "${legkind}" ]]
}

# ── init ─────────────────────────────────────────────────────────────────────────────────

# plan_init "${BASH_SOURCE[0]}" — resolve the repo layout from the CALLING SCRIPT's own
# location, never from the cwd. Idempotent. Fails loudly rather than guessing.
plan_init() {
    # NOTE: no apostrophes in a ${var:?word} message. The word is quote-processed, so an
    # apostrophe opens a quoted section and breaks the parse of everything after it.
    local callerSource="${1:?plan_init requires the calling script BASH_SOURCE[0]}"
    local callerDir=""
    if ! callerDir="$(cd "$(dirname "${callerSource}")" && pwd -P)"; then
        _plan_err "plan_init could not resolve the directory holding ${callerSource}" || return 1
    fi
    PLAN_SCRIPT_DIR="${callerDir}"
    if ! PLAN_REPO_ROOT="$(_plan_find_repo_root "${PLAN_SCRIPT_DIR}")"; then
        _plan_err "no ansible.cfg between ${PLAN_SCRIPT_DIR} and its repository boundary — this script is not inside a fedora-desktop checkout, or the checkout is incomplete. The walk stops at the repo boundary ON PURPOSE: this repo is often nested inside another one that ALSO has an ansible.cfg, and resolving to that parent would appear to work." || return 1
    fi
    export PLAN_SCRIPT_DIR PLAN_REPO_ROOT
    return 0
}

# plan_mode <deploy|gather> — declare the run's nature up front. deploy enables fail-fast
# legs and REQUIRES the change gate before the first ansible invocation; gather enables
# record-and-continue legs and FORBIDS the gate.
plan_mode() {
    local mode="${1:?plan_mode requires deploy or gather}"
    case "${mode}" in
        deploy | gather) ;;
        *)
            _plan_err "plan_mode must be 'deploy' or 'gather', got '${mode}'" || return 1
            ;;
    esac
    PLAN_MODE="${mode}"
    export PLAN_MODE
    return 0
}

# ── the host-vs-container guard ───────────────────────────────────────────────────────────

# plan_require_host <why> — refuse to continue inside a container.
#
# Claude's CCY container bind-mounts this repo, so a plan script is trivially runnable from
# it — and for anything that probes the container engine, the host's devices, systemd, or the
# deployed `/var/local/...` tree, the answer obtained there is not an answer about the host.
# The failure mode is not a missing result; it is a CONFIDENT WRONG one: a nested podman probe
# died on a userns/subuid error during an image pull and returned a non-zero exit that had
# nothing to do with the question being asked.
#
# So a script whose findings would be meaningless in a container calls this FIRST. It names
# the marker it found and what to do instead, rather than failing cryptically.
plan_require_host() {
    local why="${1:-this script probes host state}"
    local marker=""
    if marker="$(_plan_in_container "${PLAN_CONTAINER_MARKERS[@]+"${PLAN_CONTAINER_MARKERS[@]}"}")"; then
        _plan_err "refusing to run inside a container (found ${marker}): ${why}. A result obtained here would not be evidence about the host — it would be a confident wrong answer. Run this on the HOST, in a normal terminal, from the repo checkout." || return 1
    fi
    printf '==> host check: no container marker found, running on the host\n'
    return 0
}

# ── sudo priming (the localhost analogue of loading an ssh key) ────────────────────────────

# plan_prime_sudo — prime the sudo timestamp BEFORE the run log opens.
#
# Plays here run against localhost with `become`, so a password prompt can appear mid-run.
# After the tee redirect that prompt is flooded and garbled, which is the same defect a
# post-log ssh passphrase prompt has — so the ordering is enforced, not documented.
# Idempotent: if the timestamp is already valid there is no second prompt. No openable
# terminal is fatal in deploy mode and a loud recorded warning in gather mode (a read-only
# gather may legitimately have nothing that needs root).
plan_prime_sudo() {
    if [[ "${PLAN_LOG_STARTED}" -eq 1 ]]; then
        _plan_err "plan_prime_sudo must be called BEFORE plan_start_log — a sudo prompt issued after the tee redirect is flooded and garbled" || return 1
    fi
    local probe=""
    if probe="$(sudo -n -v 2>&1)"; then
        printf '==> sudo timestamp already valid, not prompting\n'
        PLAN_SUDO_PRIMED=1
        return 0
    fi
    if ! _plan_tty_openable; then
        if [[ "${PLAN_MODE}" == "gather" ]]; then
            printf '[WARN] sudo needs a password but there is no controlling terminal (%s) — continuing, gather mode. Remedy: run "sudo -v" first. (%s)\n' \
                "${PLAN_TTY_PROBE_ERR}" "${probe}" >&2
            return 0
        fi
        _plan_err "sudo needs a password but there is no controlling terminal (${PLAN_TTY_PROBE_ERR}). Remedy: run 'sudo -v' in your terminal, then re-run this script." || return 1
    fi
    printf '==> priming sudo (you may be prompted once, before the run log opens)\n'
    # No `</dev/tty` here: sudo opens the controlling terminal itself for the password prompt
    # (which is why `echo pw | sudo` needs -S). Redirecting stdin would add nothing, and the
    # openability probe above is what actually establishes that a terminal exists.
    if ! sudo -v; then
        if [[ "${PLAN_MODE}" == "gather" ]]; then
            printf '[WARN] could not prime sudo (continuing — gather mode)\n' >&2
            return 0
        fi
        _plan_err "could not prime sudo — aborting before anything runs" || return 1
    fi
    PLAN_SUDO_PRIMED=1
    return 0
}

# ── the run log ──────────────────────────────────────────────────────────────────────────

# _plan_finalize_log — EXIT/signal handler. Points stdout/stderr back at the real terminal
# (which closes the fifo's write end so the log writer sees EOF), WAITS for the writer to
# flush every buffered byte, then tells the operator where the log is and that it is
# unscrubbed. Runs at most once.
#
# The wait is not belt-and-braces. Without it the final buffered chunk — the lines written
# as the run was dying, i.e. the ones that matter — can be missing from the file. A `>(…)`
# process substitution cannot be waited on at all, which is why start_log uses a named pipe
# and keeps the writer's PID.
_plan_finalize_log() {
    if [[ "${PLAN_TRAP_DONE}" -eq 1 ]]; then
        return 0
    fi
    PLAN_TRAP_DONE=1
    if [[ "${PLAN_LOG_STARTED}" -ne 1 ]]; then
        return 0
    fi
    exec 1>&9 2>&9
    if [[ -n "${PLAN_TEE_PID}" ]]; then
        if ! wait "${PLAN_TEE_PID}"; then
            printf '[WARN] the run-log writer exited non-zero (it may have been killed by the same signal); %s may be short\n' \
                "${PLAN_RUN_LOG}" >&2
        fi
    fi
    printf '\n==> run log (UNSCRUBBED, gitignored): %s\n' "${PLAN_RUN_LOG}" >&2
    printf '==> this repo has no run-log secret scrubber, so plan run logs are NEVER committed.\n' >&2
    printf '==> read it in place; do not force-add it to git.\n' >&2
    exec 9>&-
}

# _plan_on_signal <SIG> — drain and report, then re-raise the signal's default disposition
# so the exit status still reflects the signal and the handler cannot run twice.
_plan_on_signal() {
    local sig="$1"
    _plan_finalize_log
    trap - EXIT "${sig}"
    kill "-${sig}" "$$"
}

# plan_start_log [auto|<path>] — open the tee'd run log AND arm the drain handler in ONE
# call, so a log can never be opened without the handler that finalises it. `auto` puts it in
# a per-run timestamped directory under the plan folder, so no run clobbers a previous run's
# forensics.
#
# After this call the terminal is "dirty": every prompt must go through plan_confirm.
plan_start_log() {
    local where="${1:-auto}" base="" fifo="" stamp=""
    if [[ -z "${PLAN_SCRIPT_DIR}" ]]; then
        _plan_err "plan_start_log called before plan_init" || return 1
    fi
    if [[ "${where}" == "auto" ]]; then
        base="$(basename "$0")"
        base="${base%.bash}"
        stamp="$(date '+%Y%m%d-%H%M%S')"
        PLAN_RUN_DIR="${PLAN_SCRIPT_DIR}/${base}-runs/${stamp}"
        PLAN_RUN_LOG="${PLAN_RUN_DIR}/${base}.log"
    else
        PLAN_RUN_DIR="$(dirname "${where}")"
        PLAN_RUN_LOG="${where}"
    fi
    if ! mkdir -p "${PLAN_RUN_DIR}"; then
        _plan_err "could not create the run directory ${PLAN_RUN_DIR}" || return 1
    fi
    export PLAN_RUN_DIR PLAN_RUN_LOG

    # Keep the REAL stderr on fd 9 so the finalize handler can still reach the console after
    # stdout/stderr have been pointed at the fifo.
    exec 9>&2

    # ansible emits ANSI only to a TTY, and our stdout is about to become a fifo — so without
    # this the console would go monochrome. Force colour on when the real terminal is a TTY;
    # the file branch below strips ANSI, so the log stays clean either way.
    if [[ -t 9 ]]; then
        export ANSIBLE_FORCE_COLOR=1
    fi

    fifo="${PLAN_RUN_DIR}/.planlib-tee.fifo"
    if [[ -e "${fifo}" ]]; then
        rm -f "${fifo}"
    fi
    if ! mkfifo "${fifo}"; then
        _plan_err "could not create the run-log fifo ${fifo}" || return 1
    fi

    # Split the stream: fd 3 carries raw bytes (with colour) to the real console, while the
    # file branch goes through an ANSI stripper that fflush()es so the log updates live.
    # Wrapping the pipeline in `{ …; } 3>&1 &` yields a SINGLE waitable PID whose completion
    # means the stripper flushed the WHOLE log — which is what makes the drain deterministic.
    # The reader starts BEFORE the write-open below, so the open rendezvous rather than
    # blocking forever.
    { tee /dev/fd/3 <"${fifo}" \
        | awk '{ gsub(/\033\[[0-9;]*[A-Za-z]/, ""); print; fflush() }' \
            >"${PLAN_RUN_LOG}"; } 3>&1 &
    PLAN_TEE_PID=$!
    exec >"${fifo}" 2>&1
    # The fd survives the unlink and the reader holds the other end; removing the path just
    # keeps a stray fifo out of the plan folder.
    rm -f "${fifo}"
    PLAN_LOG_STARTED=1

    # Arm for EXIT *and* the fatal signals. EXIT alone would miss a Ctrl-C, and the lines
    # lost would be exactly the ones written as the run died.
    trap '_plan_finalize_log' EXIT
    trap '_plan_on_signal INT' INT
    trap '_plan_on_signal TERM' TERM
    trap '_plan_on_signal HUP' HUP

    printf '==> run log: %s\n' "${PLAN_RUN_LOG}"
    printf '==> (sudo, if needed, was primed before this log opened)\n'
    return 0
}

# ── prompts and the change gate ──────────────────────────────────────────────────────────

# plan_confirm <prompt> [expected-token] — ask for typed consent. Returns 0 only on an exact
# token match.
#
# The prompt TEXT goes to ORDINARY STDOUT so it flows through the tee in order, behind the
# banner and log lines that precede it. Writing it straight to /dev/tty would be unbuffered,
# bypass the tee, and race AHEAD of still-buffered output — the prompt then appears above its
# own banner. The trailing newline is mandatory: a partial line block-buffers in the tee/awk
# pipeline and the run wedges with no visible prompt at all.
#
# The REPLY is read from /dev/tty because ansible legs drain the inherited stdin, so a plain
# `read` later in the run would misfire.
plan_confirm() {
    local prompt="${1:?plan_confirm requires a prompt}" expected="${2:-yes}" reply=""
    if [[ "${PLAN_ASSUME_YES}" == "1" ]]; then
        printf '==> %s [auto-confirmed via -y/--yes/PLAN_ASSUME_YES]\n' "${prompt}"
        return 0
    fi
    if ! _plan_tty_openable; then
        _plan_err "cannot prompt for '${prompt}': no controlling terminal (${PLAN_TTY_PROBE_ERR}). Re-run from a terminal, or pass -y/--yes (PLAN_ASSUME_YES=1) to consent non-interactively." || return 1
    fi
    printf '\n%s\n>>> type "%s" and press Enter to proceed: \n' "${prompt}" "${expected}"
    IFS= read -r reply </dev/tty
    reply="$(_plan_strip_cr "${reply}")"
    if [[ "${reply}" == "${expected}" ]]; then
        return 0
    fi
    printf '==> not confirmed (got "%s", expected "%s")\n' "${reply}" "${expected}" >&2
    return 1
}

# plan_gate_change <description> — the one gate a state-changing run passes before its first
# mutating leg.
#
# The axis is whether THE RUN changes state, not which environment it targets: this repo
# configures the operator's OWN workstation, so there is no safe environment to practise on
# and no "prod" to single out. `plan_mode deploy` gates once; `plan_mode gather` must not gate
# at all, because a read-only play changes nothing and a pointless prompt teaches operators to
# type through gates.
#
# Skipped under --check: a dry run changes nothing.
plan_gate_change() {
    local desc="${1:-a change to this machine}"
    if [[ "${PLAN_MODE}" == "gather" ]]; then
        _plan_err "plan_gate_change called in gather mode — a read-only run changes nothing, so there is nothing to gate. Use 'plan_mode deploy' for a state-changing run." || return 1
    fi
    if [[ "${PLAN_MODE}" != "deploy" ]]; then
        _plan_err "plan_gate_change requires 'plan_mode deploy' to be declared first (mode is '${PLAN_MODE:-unset}')" || return 1
    fi
    if [[ "${PLAN_CHECK}" == "1" ]]; then
        printf '==> [--check] change gate for "%s" auto-passed (a dry run changes nothing)\n' "${desc}"
        PLAN_GATE_PASSED=1
        export PLAN_GATE_PASSED
        return 0
    fi
    if plan_confirm "THIS MACHINE WILL BE CHANGED: ${desc}." "change-this-machine"; then
        PLAN_GATE_PASSED=1
        export PLAN_GATE_PASSED
        return 0
    fi
    _plan_err "change gate not confirmed — aborting before the first mutating leg" || return 1
}

# _plan_assert_change_allowed — the last line of defence. In deploy mode nothing may reach
# ansible until the gate has passed, so a script that forgets plan_gate_change fails here
# instead of reconfiguring the machine unannounced. Gather mode passes straight through.
_plan_assert_change_allowed() {
    if [[ "${PLAN_MODE}" == "deploy" ]] && [[ "${PLAN_GATE_PASSED}" -ne 1 ]]; then
        _plan_err "a deploy-mode ansible run was attempted before the change gate passed — call plan_gate_change '<what changes>' first" || return 1
    fi
    return 0
}

# ── ansible (plain and local; ansible.cfg supplies inventory and vault) ───────────────────

# plan_ansible_playbook <playbook> [args...] — run a playbook from the REPO ROOT.
#
# The cd is load-bearing, not tidiness: ansible.cfg's inventory, roles_path, fact cache and
# vault_password_file are all RELATIVE paths, so running from anywhere else silently picks up
# different (or missing) ones. Runs in a subshell so the caller's cwd is untouched, and with
# stdin closed so ansible cannot drain the script's own stdin out from under a later prompt.
plan_ansible_playbook() {
    local play="${1:?plan_ansible_playbook requires a playbook path}"
    shift
    if [[ -z "${PLAN_REPO_ROOT}" ]]; then
        _plan_err "plan_ansible_playbook called before plan_init" || return 1
    fi
    if [[ ! -e "${play}" ]]; then
        _plan_err "playbook not found: ${play}" || return 1
    fi
    _plan_assert_change_allowed || return 1
    (
        cd "${PLAN_REPO_ROOT}" || exit 1
        exec ansible-playbook "${PLAN_CHECK_ARGS[@]+"${PLAN_CHECK_ARGS[@]}"}" "${play}" "$@" </dev/null
    )
}

# plan_ansible_adhoc [args...] — run an ad-hoc ansible command from the repo root. `-v` is
# the default because a verify leg needs the actual VALUE in the log, not just `changed:`.
plan_ansible_adhoc() {
    if [[ -z "${PLAN_REPO_ROOT}" ]]; then
        _plan_err "plan_ansible_adhoc called before plan_init" || return 1
    fi
    _plan_assert_change_allowed || return 1
    (
        cd "${PLAN_REPO_ROOT}" || exit 1
        exec ansible -v "${PLAN_CHECK_ARGS[@]+"${PLAN_CHECK_ARGS[@]}"}" "$@" </dev/null
    )
}

# ── legs ─────────────────────────────────────────────────────────────────────────────────

# plan_deploy_leg <name> <cmd...> — fail-fast leg for a state-changing run. On failure it
# prints [ABORT] and terminates the whole run immediately, so nothing downstream of a broken
# leg ever executes.
#
# It MUST be a bare top-level statement. Inside $(...), a pipeline, or ( ), its abort would
# terminate only the subshell and control would flow straight on to the NEXT leg — the run
# would look like it aborted while actually continuing. BASH_SUBSHELL detects that misuse and
# takes the whole run down rather than half-obeying.
plan_deploy_leg() {
    local name="${1:?plan_deploy_leg requires a leg name}"
    shift
    if [[ "${BASH_SUBSHELL}" -ne 0 ]]; then
        printf '[FATAL] plan_deploy_leg "%s" was invoked inside a subshell, pipeline or command substitution (BASH_SUBSHELL=%s). A failed leg would terminate only the subshell and the run would continue to the NEXT leg. Call it as a bare top-level statement.\n' \
            "${name}" "${BASH_SUBSHELL}" >&2
        kill -TERM "$$"
        exit 1
    fi
    if ! _plan_mode_allows_leg "${PLAN_MODE}" "deploy"; then
        _plan_err "plan_deploy_leg used but the mode is '${PLAN_MODE:-unset}'. Declare 'plan_mode deploy' — a state-changing run is fail-fast." || return 1
    fi
    _plan_banner "[deploy leg] ${name}"
    if ! "$@"; then
        printf '[ABORT] leg "%s" failed — stopping here, nothing further will run\n' "${name}" >&2
        exit 1
    fi
    return 0
}

# plan_gather_leg <name> <cmd...> — record-and-continue leg for a READ-ONLY run. A failure is
# recorded by name and drives a non-zero final exit, so collecting as much as possible never
# turns into reporting success.
plan_gather_leg() {
    local name="${1:?plan_gather_leg requires a leg name}"
    shift
    if ! _plan_mode_allows_leg "${PLAN_MODE}" "gather"; then
        _plan_err "plan_gather_leg used but the mode is '${PLAN_MODE:-unset}'. Declare 'plan_mode gather' — only a read-only run may continue past a failed leg." || return 1
    fi
    _plan_banner "[gather leg] ${name}"
    if ! "$@"; then
        printf '[WARN] gather leg "%s" failed (continuing)\n' "${name}" >&2
        PLAN_FAILED_LEGS="${PLAN_FAILED_LEGS}${PLAN_FAILED_LEGS:+ }${name}"
    fi
    return 0
}

# ── closing ──────────────────────────────────────────────────────────────────────────────

# plan_list_reports — name the report files the run produced, so the operator knows what to
# read without hunting.
plan_list_reports() {
    if [[ -z "${PLAN_RUN_DIR}" ]] || [[ ! -d "${PLAN_RUN_DIR}" ]]; then
        return 0
    fi
    local f found=0
    for f in "${PLAN_RUN_DIR}"/*report*; do
        if [[ -e "${f}" ]]; then
            if [[ "${found}" -eq 0 ]]; then
                printf '==> reports produced:\n'
                found=1
            fi
            printf '    %s\n' "${f}"
        fi
    done
    return 0
}

# plan_finish — the closing statement of every plan script: list reports, summarise failed
# legs, and terminate with a status that AGREES with the text. It ends the run deliberately,
# so anything written after a plan_finish call is dead code.
plan_finish() {
    plan_list_reports
    if [[ -n "${PLAN_FAILED_LEGS}" ]]; then
        printf '==> FAILED legs: %s\n' "${PLAN_FAILED_LEGS}" >&2
        if [[ -n "${PLAN_RUN_LOG}" ]]; then
            printf '==> run log: %s\n' "${PLAN_RUN_LOG}"
        fi
        exit 1
    fi
    printf '==> all legs OK\n'
    if [[ -n "${PLAN_RUN_LOG}" ]]; then
        printf '==> run log: %s\n' "${PLAN_RUN_LOG}"
    fi
    exit 0
}

# ── shared flag vocabulary ───────────────────────────────────────────────────────────────

# plan_parse_common_flags "$@" — consume the flags every plan script shares and leave the
# rest in PLAN_REMAINING_ARGS. Set PLAN_USAGE first to get a useful --help.
#
#   --check     thread --check into every ansible invocation (dry run)
#   -y|--yes    consent non-interactively (skips plan_confirm / plan_gate_change prompts)
#   -h|--help   print PLAN_USAGE and stop
plan_parse_common_flags() {
    PLAN_REMAINING_ARGS=()
    local arg
    for arg in "$@"; do
        case "${arg}" in
            --check)
                PLAN_CHECK=1
                PLAN_CHECK_ARGS=("--check")
                export PLAN_CHECK
                ;;
            -y | --yes)
                PLAN_ASSUME_YES=1
                export PLAN_ASSUME_YES
                ;;
            -h | --help)
                if [[ -n "${PLAN_USAGE}" ]]; then
                    printf '%s\n' "${PLAN_USAGE}"
                else
                    printf 'usage: %s [--check] [-y|--yes] [-h|--help] [plan-specific args...]\n' \
                        "$(basename "$0")"
                fi
                exit 0
                ;;
            *)
                PLAN_REMAINING_ARGS+=("${arg}")
                ;;
        esac
    done
    return 0
}
