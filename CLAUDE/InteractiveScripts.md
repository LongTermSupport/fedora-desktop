# Human-Friendly Interactive Script Rules

Standardised rules for **human-interactive scripts** deployed by this repo. These
are not suggestions — interactive scripts in this repo MUST follow them so the UX
is consistent across every helper.

## Scope — what these rules apply to

They apply to any script a **human runs and is prompted by at a terminal**:

- Extensionless executables under `files/home/.local/bin/` (e.g. `ssh-with-key`,
  `ssh-keys-rekey`, `clean-paste`).
- `*.bash` files that are **executed** by a human (not the sourced-library ones).

They do **NOT** apply to:

- **Sourced libraries** (e.g. `ssh-with-agent.bash` — sourced, never run
  directly). These have no prompts of their own.
- **Non-interactive automation** — Ansible-invoked helpers, cron scripts, CI
  glue. Those must be fully non-interactive and fail fast (no prompts at all).

**Litmus test:** if the script calls `read`, prompts for a passphrase, shows a
menu, or asks a `[y/N]` question — it is interactive and these rules bind it.

## Core principle

> **Strict validation, friendly recovery.** Validate input *strictly*, but do
> **NOT** abort the whole run on a *recoverable* input mistake. Show a clear,
> specific error and **re-prompt in a loop**. Reserve hard aborts for genuinely
> unrecoverable states.

This does **not** weaken the project's #1 fail-fast rule — it refines it for
humans. A typo is not a failure condition; an unsupported environment is. Retry
the former, fail fast on the latter.

## The Rules

01. **Re-prompt, don't abort, on recoverable input errors.** A mistyped
    passphrase, a mismatched confirmation, an out-of-range menu choice, a
    malformed value → print a one-line error and loop back to the *same* prompt.
    The user keeps their place; they do not have to restart the whole command.

02. **Bounded retries.** Every retry loop has a cap (`MAX_TRIES`, default **3**).
    On exhaustion, print a clear give-up message and exit non-zero. This honours
    fail-fast (it *does* eventually fail) while a piped or runaway input cannot
    spin forever.

03. **Always offer a clean exit.** `read` returning EOF (Ctrl-D, or a non-TTY with
    no more input) MUST abort cleanly with a message — never loop on empty input.
    Check `read`'s exit status; treat EOF as "cancelled, no changes made". Ctrl-C
    must never leave half-finished state (use a `trap` if partial work is
    possible).

04. **Distinguish recoverable vs fatal — never loop on the unfixable.** A typo is
    recoverable → retry. A genuinely unsupported environment (mixed data the
    script can't handle, a missing dependency, no TTY when one is required) is
    fatal → fail fast with an explanation and, where possible, the exact command
    to resolve it. Looping on something a retry can never fix is a bug.

05. **Self-explanatory prompts.** State what is expected and show any default
    inline: `New passphrase (empty = remove passphrase): `, `Host [localhost]: `,
    `Proceed? [y/N] `. The user should never have to guess the accepted input.

06. **Confirm before destructive or irreversible actions** unless `-y`/`--yes` was
    given. First print a concise summary of exactly what will change (counts,
    paths). Default the confirmation to the **safe** answer (`[y/N]` → default No).

07. **Read secrets silently, from the terminal.** Use `read -rs` and read secrets
    from `/dev/tty` (so they work even when stdout is captured and cannot be
    shoulder-surfed from a log). Never echo a secret, never pass it in `argv`,
    never write it to a file or log.

08. **Prompts and diagnostics → stderr; machine output → stdout.** Keep the script
    pipe-friendly: a caller capturing stdout must get data, not prompts. This is
    the interactive-script facet of the repo-wide **stderr hygiene** standard
    (stdout = the captured payload; all chatter → `>&2`) — see
    [StderrHygiene.md](StderrHygiene.md) for the general rule and patterns.

09. **Friendly, specific error messages.** Say *what* was wrong and *what to do
    next*: "Those didn't match — let's try again." not "Aborting: mismatch.". No
    bare "Invalid" / "Error" with no guidance.

10. **`-h`/`--help` always works**, lists every option, and gives examples.
    **Unknown options fail fast** with a one-line error pointing at `--help`.

11. **A non-interactive escape hatch exists.** Provide flags (`-y`, explicit value
    flags) so the script can run unattended. If required input is missing and
    there is no TTY, fail with a clear message — do **not** hang waiting on a
    prompt that can never be answered.

12. **Back up before irreversible change, and say where.** Where feasible, snapshot
    what you are about to overwrite, report the backup location, and on a
    mid-operation failure roll back what was already changed.

13. **Degrade cleanly when not a TTY.** Guard colour/cursor escapes behind
    `[ -t 2 ]` (or `[ -t 1 ]`). Output piped to a file or another program must be
    plain text, never raw terminal escape codes.

## Canonical patterns

Reuse these shapes verbatim so every script behaves identically.

### Silent secret read with EOF handling

```bash
# Echoes the secret on stdout; prompt + newline on stderr. Returns non-zero on
# EOF (Ctrl-D / no TTY) so callers can treat it as "cancelled".
prompt_secret() {
    local _prompt="$1" _val
    if ! read -rsp "$_prompt" _val < /dev/tty; then
        echo >&2
        return 1
    fi
    echo >&2
    printf '%s' "$_val"
}
```

### Bounded retry loop (validate strictly, re-prompt on failure)

```bash
MAX_TRIES=3
attempt=1
while :; do
    if ! value="$(prompt_secret 'New passphrase: ')"; then
        echo "Cancelled — no input. No changes made." >&2
        exit 1                       # rule 3: clean exit on EOF
    fi
    if ! confirm="$(prompt_secret 'Confirm new passphrase: ')"; then
        echo "Cancelled. No changes made." >&2
        exit 1
    fi
    [ "$value" = "$confirm" ] && break          # valid -> proceed

    echo "  Those didn't match — let's try again." >&2   # rule 9
    attempt=$((attempt + 1))
    if [ "$attempt" -gt "$MAX_TRIES" ]; then            # rule 2
        echo "Giving up after $MAX_TRIES attempts. No changes made." >&2
        exit 1
    fi
done
```

### Confirm-before-destructive prompt (default safe)

```bash
if [ "$ASSUME_YES" -ne 1 ]; then
    printf 'Proceed with %d change(s)? [y/N] ' "$count" >&2
    if ! read -r reply < /dev/tty; then reply=""; fi    # EOF -> default No
    case "$reply" in
        y|Y|yes|YES) ;;
        *) echo "Aborted. No changes made." >&2; exit 1 ;;
    esac
fi
```

### Capturing a probe without an error-hiding redirect

To check whether a command succeeds while suppressing its noisy output, capture
combined output into a variable — do **not** use `2>/dev/null` (it is an
error-hiding pattern blocked by the hooks daemon, and it discards the reason):

```bash
if probe="$(some-check "$arg" 2>&1)"; then
    ok=1
else
    echo "  Check failed: $probe" >&2     # the captured reason is useful
fi
```

## Adoption

`ssh-keys-rekey` is the reference implementation of these rules. Existing
interactive helpers under `files/home/.local/bin/` predate this document; bring
them into line whenever they are next touched, rather than mass-rewriting them in
one go.
