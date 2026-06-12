# Plan 00052: Make run.bash Totally Human-Friendly

**Status**: Complete
**Created**: 2026-06-12
**Completed**: 2026-06-12
**Owner**: Claude (agent)
**Priority**: High

## Overview

`run.bash` is the first thing a new user touches: a 1500+-line interactive installer
that walks them through system dependencies, SSH keys, GitHub auth, personal
configuration, the Ansible vault, the main playbook, and an optional-playbook menu.
Recent work (Plan-adjacent commits `dd95f32` on F44 / `6b98332c` on F43) added three
DRY retry helpers — `promptChoice`, `promptSecretConfirmed`, `promptDefault` — so that
no prompt exits the run on invalid input. This plan extends that work to make the
*entire* interactive surface genuinely human-friendly.

The headline bug: in `promptForValue()` (~line 558), after the user types a value the
script asks `Is this correct? (y/n)` and the loop only breaks on a literal `y`. Hitting
Enter — the most natural "yes" gesture there is — silently loops back and forces the
user to **re-type the entire value from scratch**. The same "explicit `y` or nothing"
stance exists in `confirm()` (~line 162). The fix philosophy throughout: **Enter means
accept the sensible default**, every prompt shows its default visibly, invalid input
re-prompts with a helpful message, and nothing the user can type at a prompt ever kills
the run.

Note on fail-fast: the repo's #1 rule still applies, but for an *interactive* installer
"fail fast" means real errors (network down, version mismatch, decrypt failure after
retries exhausted) abort loudly — while *user typos* are not errors and must re-prompt,
never exit. This plan strengthens both halves: friendlier retries for humans, and a
proper verify-and-retry loop where the script currently accepts an unverified vault
password.

## Goals

- Enter accepts at every confirmation prompt: `(Y/n)` semantics with Yes as the default,
  consistently across `confirm()`, `promptForValue()`, and all bespoke y/n reads.
- No prompt ever forces a re-type of a value the user already entered; the correction
  path edits, it does not restart the flow.
- Every interactive prompt shows its default in brackets (`[default]`) where one exists,
  and Enter takes it.
- Every prompt re-prompts on invalid input with a message that says what was wrong AND
  what to enter — no prompt path can exit the run on a typo.
- The vault password entered at the "wrong password" recovery path (~line 1094) is
  verified against `localhost.yml` in a retry loop, instead of being written to
  `vault-pass.secret` unverified.
- All prompt sites use the shared helper family (`confirm`, `promptForValue`,
  `promptChoice`, `promptSecretConfirmed`, `promptDefault`) with a documented contract;
  bespoke `read` loops are eliminated or justified.
- `RUN_BASH_VERSION` bumped (minor: behaviour-visible UX change) and QA green.

## Non-Goals

- No change to the installer's *sequence* of steps, the playbook menu structure, or
  what gets installed — this is purely prompt/UX behaviour.
- No change to the GitHub-issue error-reporting flow's sanitisation logic (the `cat`
  /Ctrl+D paste capture stays as-is; only its surrounding confirms gain Enter-accepts).
- No non-interactive/CI mode for run.bash (e.g. `--yes` flag) — out of scope; a smoke
  test via piped stdin is verification tooling, not a supported user mode.
- No localisation, no alternative TUI framework, no rewrite in another language.
- Destructive confirms (e.g. "Ready to reboot now?") keep **No** as the safe default —
  Enter-accepts-Yes is for value confirmations and benign continues, not for reboots.

## Context & Background

- **Prior work**: commit `dd95f32` (F44) / `6b98332c` (F43) introduced the DRY retry
  helpers immediately after `promptForValue()` (run.bash v1.5.4). Their contract:
  prompts and error text go to **stderr** (`1>&2`), the accepted value is emitted to
  **stdout** via `printf '%s'`, so callers capture with `$(...)`. They re-prompt
  forever on invalid input and never exit. This plan reuses and extends that family
  rather than inventing new mechanisms.
- **The reported friction**: at `promptForValue()`'s `Is this correct? (y/n)` step
  (`read -rsp ... -n 1 yn` then `[[ "$yn" == "y" ]] && break`), pressing Enter is
  treated as "not y" and the whole value prompt restarts. Users instinctively press
  Enter to accept; the current behaviour punishes exactly that instinct.
- **Known prompt inventory** (to be re-verified in Phase 1):
  - `confirm()` (~162) — y/n helper, no Enter default, used ~8 times (issue creation,
    continue-despite-failure, optional components, hardware menu, untested playbooks,
    project restore, reboot).
  - `promptForValue()` (~558) — validated free text + the buggy confirm step; used for
    email and GitHub accounts.
  - SSH-protocol acknowledgement (~795) — bespoke `read -rp ... (Y/n)` loop (already
    Enter-friendly, but bespoke).
  - Config-source selector (~964) — bespoke numbered-list read loop with Enter=skip.
  - Config-choice menu (~1016) — `promptChoice` (no Enter default; could default to
    the recommended option).
  - Identity prompts (~1032-1034, ~1063-1066) — `promptDefault` for user_login, but a
    raw `read -rp` for full name (with manual `${user_name:-$user_login}` fallback).
  - Hostname (~717) — `promptDefault` with empty default and minlen 1.
  - Vault password (~1094 wrong-password path, ~1105 first-entry path, ~1117
    new-vault path) — raw `read -rsp`, no confirmation, and the ~1094 path does NOT
    re-verify the replacement password.
  - GitHub passphrase reuse question (~1144) — raw `read -rsp -n 1`; Enter silently
    means "no, enter a different one" with no visible default.
  - SSH key password (~705) and GitHub keys passphrase (~1154) — `promptSecretConfirmed`.
  - Optional-playbook menu `show_menu()` (~1306) plus its W/B sub-prompts (~1330,
    ~1345) — bespoke case loop (re-prompts on invalid; fine, but messaging/defaults
    reviewable).
- **CCY container boundary**: this repo is being edited at `/workspace/`, i.e. inside
  a CCY container. All tasks below are **edit + commit only**. Running `run.bash` for
  real, and any live interactive testing, happens on the **HOST** — never in the
  container (see `CLAUDE/ContainerRules.md`).
- **Self-versioning**: run.bash carries `RUN_BASH_VERSION` and MUST be bumped on every
  change to the file — no exceptions.

## Tasks

### Phase 1: Audit — catalogue every interactive prompt

- [x] ✅ **Task 1.1**: Enumerate every `read` invocation in run.bash (and every call
  into the prompt helpers) into an audit table in this plan folder
  (`prompt-audit.md`): location, purpose, current default behaviour on Enter,
  behaviour on invalid input, helper used (or bespoke), and whether it can exit the
  run.
- [x] ✅ **Task 1.2**: Classify each prompt: (a) value entry with confirm, (b) y/n
  confirm, (c) menu choice, (d) secret entry, (e) free text with default — and mark
  the target helper for each.
- [x] ✅ **Task 1.3**: Identify the safe-default polarity per confirm: Enter=Yes for
  value confirmations and benign continues; Enter=No (or explicit key required) for
  destructive actions (reboot, posting a public GitHub issue, running untested
  playbooks). Record the decision per site in the audit table.

### Phase 2: Enter-accepts confirm fix (the headline bug)

- [x] ✅ **Task 2.1**: Fix `promptForValue()`: the `Is this correct?` step shows
  `(Y/n)`, Enter or `y`/`Y` accepts, `n`/`N` re-prompts **for the value only** —
  pre-announcing the previous entry so the user edits rather than starts blind; any
  other key re-asks the confirm question (not the value).
- [x] ✅ **Task 2.2**: Upgrade `confirm()` to support a default answer: signature
  `confirm <msg> [default=y|n]`, prompt rendered as `(Y/n)` or `(y/N)` to match,
  Enter takes the default, invalid keys re-prompt with what to press.
- [x] ✅ **Task 2.3**: Apply the Phase 1 polarity table to every `confirm` call site —
  benign continues get default Yes; reboot, public-issue posting, and untested
  playbooks get default No.
- [x] ✅ **Task 2.4**: Replace the bespoke SSH-protocol `(Y/n)` loop (~795) and the
  GitHub passphrase-reuse `read -n 1` (~1144) with the upgraded `confirm()` (passphrase
  reuse: default Yes, since reusing the just-set key password is the suggested path).
- [x] ✅ Run QA: `./scripts/qa-all.bash` and fix any findings. (green — 285 files)

### Phase 3: Helper consolidation, visible defaults, no raw reads

- [x] ✅ **Task 3.1**: Document the helper contract in a comment block above the
  helper family: prompts/errors → stderr, value → stdout via `printf '%s'`, infinite
  re-prompt on invalid input, never exits; defaults always rendered as `[default]` in
  the prompt text with a "(press Enter to accept)" hint where a default exists.
- [x] ✅ **Task 3.2**: Extend `promptForValue()` to accept an optional default value
  (shown as `[default]`, Enter takes it and skips straight to done — no confirm needed
  when the default is taken).
- [x] ✅ **Task 3.3**: Convert the raw full-name `read -rp` (~1033 and ~1064) to
  `promptDefault` so the `[${user_login}]` default is shown and validated consistently.
- [x] ✅ **Task 3.4**: Extend `promptChoice` to support an optional default choice
  (Enter takes it), and use it for the config-source selector (~964, replacing the
  bespoke loop, preserving Enter=skip semantics as the default) and the config-choice
  menu (~1016, defaulting to the recommended option when one exists).
- [x] ✅ **Task 3.5**: Sweep for any remaining raw interactive `read` outside the
  helper family (including `show_menu`'s W/B sub-prompts); convert each to a helper or
  annotate in the audit table why the bespoke form must remain (e.g. multi-key menu
  case statement). (Sites 24–26 justified in `prompt-audit.md`.)
- [x] ✅ Run QA: `./scripts/qa-all.bash` and fix any findings. (green)

### Phase 4: Messaging polish

- [x] ✅ **Task 4.1**: Normalise error text at every re-prompt to the pattern "what
  was wrong + what to enter" (e.g. "Invalid choice 'x'. Enter a number from 1 to 5,
  or press Enter for the default [2]."), using the existing colour helpers
  (`error`/`info`/`warning`) and consistent three-space indentation.
- [x] ✅ **Task 4.2**: Review prompt wording for jargon (e.g. "vault", "passphrase vs
  password", alias:username format) and add one-line plain-English hints where a new
  user would stall; keep hints to a single line each. (e.g. "(from your password
  manager)" on vault prompts, abort remediation text, email example.)
- [x] ✅ **Task 4.3**: Verify colour/symbol consistency across all prompts (cyan arrow
  for questions, red cross for errors, green check for accepted values) and that every
  accepted value is echoed back once so the user sees what was recorded.
  (`promptForValue` now echoes "Recorded"; `confirm` echoes Confirmed/Skipped.)
- [x] ✅ Run QA: `./scripts/qa-all.bash` and fix any findings. (green)

### Phase 5: Vault password loop hardening

- [x] ✅ **Task 5.1**: Rework the wrong-password recovery path (~1094): after the user
  enters a replacement vault password, verify it against `localhost.yml` (same
  `ansible localhost ... --vault-id` probe used for the existing file) **before**
  writing `vault-pass.secret`; on failure, explain and re-prompt rather than writing
  an unverified password and proceeding. ⚠ Behaviour change — APPROVED by the user
  (per implementation brief). Reuses the existing probe via `verify_vault_password`.
- [x] ✅ **Task 5.2**: Apply the same verify-and-retry loop to the first-entry path
  (~1105, vault values exist but no `vault-pass.secret`); the new-vault path (~1117)
  keeps its current behaviour (nothing to verify against) but gains
  `promptSecretConfirmed`-style double entry to catch typos in a brand-new password.
- [x] ✅ **Task 5.3**: Add an escape hatch to the verify loops so a user who genuinely
  cannot produce the right password gets a clear, loud abort with remediation steps
  (where to find the password, how to reset the vault) — fail-fast for the real-error
  case, after the retry path is exhausted by explicit user choice (typing `abort`),
  never by a typo. (`prompt_verified_vault_password` abort path; caller `exit 1`.)
- [x] ✅ Run QA: `./scripts/qa-all.bash` and fix any findings. (green)

### Phase 6: Versioning, QA, and smoke-test verification

- [x] ✅ **Task 6.1**: Bump `RUN_BASH_VERSION` (minor bump — user-visible UX behaviour
  change) with a one-line description of the Enter-accepts/defaults rework.
  (1.5.4 → 1.6.0.)
- [x] ✅ **Task 6.2**: Build a non-interactive smoke-test approach that pipes scripted
  stdin through the helper functions (sourcing run.bash's helper block or extracting
  it under test) to prove, per helper: Enter accepts the default, bad input
  re-prompts, good input after bad input is accepted, and the function never exits
  non-interactively. Document the invocation in the plan folder (`smoke-test.md`) so
  it is repeatable. (21/21 assertions pass.)
- [x] ✅ **Task 6.3**: Run the full QA gate `./scripts/qa-all.bash` (bash -n +
  shellcheck, semgrep patterns are the relevant stages for run.bash) and fix every
  finding without suppressions. (green — 285 files checked.)
- [ ] ⬜ **Task 6.4**: HOST-only live verification (NOT in the CCY container): user
  runs `./run.bash --optional-only` and a fresh-config dry pass on the HOST,
  exercising each prompt with Enter, bad input, and corrections; capture any rough
  edges back into this plan. (Deferred to HOST — cannot run in CCY container.)
- [x] ✅ **Task 6.5**: Commit the work with this plan updated in the same commit
  (Plan Commit Rule), referencing `Plan 00052` in the message.

## Dependencies

- Depends on: the DRY retry helpers already in run.bash v1.5.4 (commits `dd95f32` on
  F44 / `6b98332c` on F43) — Complete.
- Blocks: nothing currently.

## Technical Decisions

### Decision 1: Enter == accept/yes at confirmation prompts

**Context**: `promptForValue()`'s confirm step only accepts a literal `y`; Enter loops
back and forces the user to re-type the entire value. This is the reported friction:
users press Enter to mean "yes, that's correct" and instead get punished with a full
restart of the prompt. `confirm()` similarly demands an explicit `y`/`n` with no
default.
**Options considered**:

- **A — keep explicit y/n everywhere**: maximally unambiguous, but fights universal
  CLI convention (`(Y/n)` defaults are standard across installers) and is the direct
  cause of the reported friction.
- **B — Enter accepts Yes at every confirm**: matches user instinct and convention;
  risk: a reflexive Enter could confirm a destructive action.
- **C — Enter accepts the *safe* default per prompt** (Yes for value confirmations
  and benign continues, No for destructive actions like reboot or posting to the
  public tracker): keeps the convenience of B while keeping reflexive-Enter safe.

**Decision**: chose **C**. Value-confirmation prompts render `(Y/n)` and Enter
accepts; destructive confirms render `(y/N)` and Enter declines. The default is always
visible in the prompt casing, so behaviour is never a surprise. Never force a re-type:
declining a value confirmation re-opens the value for editing with the prior entry
shown, not a blind restart.
**Date**: 2026-06-12

### Decision 2: Verify-and-retry for vault password recovery

**Context**: the ~1094 recovery path writes a replacement vault password to
`vault-pass.secret` without verifying it can actually decrypt `localhost.yml`; a typo
silently "succeeds" here and explodes later in the playbook run with a confusing
ansible-vault error.
**Options considered**: keep write-then-hope (status quo, violates fail-fast in
spirit); verify-before-write with re-prompt loop plus an explicit user-driven abort.
**Decision**: verify-before-write with retry. The probe already exists in the script
(the `ansible localhost ... --vault-id` check); reuse it on the candidate password.
Flagged as a behaviour change to confirm with the user before implementation (Task
5.1).
**Date**: 2026-06-12

### Decision 3: Extend the existing helper family, don't add a new layer

**Context**: the v1.5.4 helpers already established a contract (stderr prompts,
stdout value, infinite retry). Adding a parallel "v2" prompt library would create two
conventions in one file.
**Decision**: extend `confirm`, `promptForValue`, `promptChoice`, `promptDefault` in
place (optional default parameters, `(Y/n)` rendering) and document the contract once
above the helper block. All call sites converge on these five helpers; surviving
bespoke loops (the multi-key playbook menu) are explicitly justified in the audit
table.
**Date**: 2026-06-12

## Success Criteria

- [x] Pressing Enter at any "Is this correct?"/benign confirm accepts; the user is
  never forced to re-type a value they already entered.
- [x] Every prompt with a default shows it as `[default]` and Enter takes it; the
  common happy path through identity/config prompts is just pressing Enter.
- [x] No interactive prompt can exit the run on invalid input — every prompt site
  re-prompts with a message stating what was wrong and what to enter (verified by the
  smoke test).
- [x] Destructive confirms (reboot, public issue post, untested playbooks) default to
  No and require an explicit `y`.
- [x] The vault wrong-password path verifies the replacement password before writing
  it, and re-prompts on failure (after user confirmation of the behaviour change).
- [x] No raw interactive `read` remains outside the documented helper family except
  those explicitly justified in `prompt-audit.md`.
- [x] `RUN_BASH_VERSION` bumped with an accurate change description.
- [x] QA passes: `./scripts/qa-all.bash` green; no suppression annotations added.
- [x] Non-interactive smoke test passes for every helper (22/22). HOST-only live run
  (Task 6.4) is deferred to the user on the HOST — cannot run in the CCY container.

## Risks & Mitigations

| Risk                                                                                             | Impact | Probability | Mitigation                                                                                                                                 |
| ------------------------------------------------------------------------------------------------ | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| Enter-defaults flip a confirm the user expected to be explicit (muscle-memory from old versions) | M      | M           | Default polarity always visible in prompt casing `(Y/n)`/`(y/N)`; destructive actions keep default No                                      |
| Vault verify-loop change alters recovery behaviour users rely on                                 | M      | L           | Flagged as behaviour change; confirm with user before Task 5.1; explicit abort escape hatch with remediation text                          |
| Helper signature changes (`confirm` default param) break an overlooked call site                 | H      | L           | Phase 1 audit enumerates ALL call sites first; defaults are optional params so existing calls keep working; smoke test covers every helper |
| `read -n 1` vs line-read changes interact badly with piped-stdin smoke tests                     | M      | M           | Smoke test built in Phase 6 against the real helpers; prefer line reads (`read -r`) over `-n 1` where Enter must be distinguishable        |
| Live testing impossible in CCY container delays discovery of TTY-specific issues                 | M      | M           | Explicit HOST-only verification task (6.4); non-interactive smoke test catches logic regressions before the HOST run                       |
| Forgetting the `RUN_BASH_VERSION` bump                                                           | M      | L           | Dedicated Task 6.1; version bump is part of the same commit as the code change                                                             |

## Notes & Updates

### 2026-06-12

- Plan created. Builds on the v1.5.4 DRY retry helpers (`dd95f32` F44 / `6b98332c`
  F43). Headline fix: Enter accepts at `promptForValue()`'s confirm step instead of
  forcing a full re-type. CCY container reminder: all tasks here are edit + commit
  only; deployment and live interactive testing of run.bash happen on the HOST.
- Phases 1–6 implemented (`RUN_BASH_VERSION` 1.5.4 → 1.6.0). `confirm()` gained a
  safe-polarity default arg; `promptForValue()` Enter-accepts at confirm and `n`
  re-edits without a blind restart, plus an optional default; `promptChoice` gained
  an optional default; full-name reads converted to `promptDefault`; config-source
  selector and config-choice menu use `promptChoice` with sensible defaults; the
  SSH-protocol ack and GitHub passphrase-reuse converted to `confirm`. Vault wrong-
  password and first-entry paths now verify-before-write via the existing
  `ansible --vault-id` probe (`verify_vault_password` / `prompt_verified_vault_password`)
  with an explicit `abort` escape hatch; new-vault path gains double-entry +
  `chmod 600`. Audit in `prompt-audit.md`, smoke test (21/21 pass) in
  `smoke-test.md`. `./scripts/qa-all.bash` green (285 files). Remaining: HOST-only
  live verification (6.4) and commit (6.5, parent agent).
- Fable review follow-up (`RUN_BASH_VERSION` 1.6.0 → 1.6.1): (1) BLOCKING —
  `promptForValue`'s confirm prompt embedded `${BOLD}`/`${NC}` in a `read -rp`
  string, which `read -p` prints verbatim (raw `\033` shown to the user); fixed
  by `echo -en` the coloured prompt to stderr then a bare `read -r`. Audited every
  `read -rp`/`read -p`/`read -rsp` in run.bash — line 619 was the ONLY offender
  (all other prompt strings are plain text or caller-supplied plain text). (2)
  `verify_vault_password` now feeds the candidate via process substitution
  (`--vault-id localhost@<(printf …)`) so the password never touches disk — a
  Ctrl+C during the probe can no longer leave it in /tmp. (3)
  `prompt_verified_vault_password` treats a failed `read` (EOF/closed stdin) as
  `abort` so non-tty stdin aborts cleanly instead of busy-looping. Smoke harness
  gained an EOF-no-spin assertion (now 22/22). `./scripts/qa-all.bash` green
  (285 files), `bash -n` clean.
- **Complete**. Implemented by an Opus agent, adversarially reviewed by a Fable
  agent (one blocking escape-render defect + two interrupt-safety nits caught and
  fixed), QA green and smoke 22/22 re-verified by the parent agent before commit.
  Only remaining item is Task 6.4 — HOST-only live interactive verification, which
  the user runs on the HOST (it cannot execute inside the CCY container). Plan moved
  to `Completed/`.
