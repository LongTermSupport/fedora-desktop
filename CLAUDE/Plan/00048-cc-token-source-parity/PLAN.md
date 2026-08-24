# Plan 00048: Host `cc` Token-Source Parity with `ccy`

**Status**: In Progress (Phases 1-3 done; Phase 4 awaits a host deploy)
**Created**: 2026-06-10
**Owner**: joseph
**Priority**: Medium

## Overview

The host `cc` shortcut today is a single-line alias —
`alias cc='claude update && claude'` — and it authenticates through the
host's `~/.claude/` OAuth state (the standard browser-based flow that
`claude` writes on first login). The containerised `ccy` wrapper, by
contrast, has a fully developed named-token system at
`~/.claude-tokens/ccy/tokens/`: tokens are filenames of shape
`NAME.YYYY-MM-DD.token`, expiry is colour-coded, expired tokens get a
"renew" option in the chooser, and the selected token is injected as
`CLAUDE_CODE_OAUTH_TOKEN` into the container.

The user wants `cc` to share that token chooser experience so that
switching between Claude accounts on the host feels the same as inside
`ccy`. The implementation already exists in pure-bash form at
`files/var/local/claude-yolo/lib/token-management.bash`, and the
file-system-only functions in that library
(`list_tokens`, `select_token`, `is_token_valid`, `colorize_expiry`,
`export_token`, `export_tokens_interactive`) are container-independent
and host-safe. Token *creation* uses the container (`validate_token`
runs `claude --version` inside the image), so that path stays delegated
to `ccy --create-token` — `cc` does not learn to create tokens, only to
choose among existing ones.

`cc` becomes a real wrapper script at `/var/local/claude-code/cc`
(mirroring `/var/local/claude-yolo/claude-yolo`) so the bashrc alias
stays a single human-readable line. The chooser presents a "Desktop"
pseudo-option at the bottom that, when selected, exports nothing and
falls through to the standard host `claude` behaviour (current `cc`
semantics preserved). If the token pool is empty, "Desktop" is the only
option and the script prints inline instructions for creating tokens
with `ccy --create-token`.

This plan was originally drafted as a **sibling** of Plan 00036
(terminal-UX env-var parity), but during plan review 00036 was
cancelled — both of its target env vars had been independently
obsoleted (`CLAUDE_CODE_NO_FLICKER` dropped from ccy in commit
`a32c3d3` via Plan 00047 Path D; `CLAUDE_CODE_DISABLE_MOUSE` now
owned by Plan 00047). 00048 is therefore the sole active vehicle
for cc/ccy parity work. The "container-only vars MUST NOT export
on host" guardrail that 00036 captured (`IS_SANDBOX`,
`CCY_DISABLE_SUSPEND`) is preserved here as an explicit Non-Goal.
See [Plan 00036](../00036-cc-ccy-parity/PLAN.md) for the
cancellation rationale.

## Goals

- Host `cc` invocation presents the same token chooser UX as `ccy` does
  for token selection, sharing the same token pool at
  `~/.claude-tokens/ccy/tokens/`
- A "Desktop" option in the chooser means "use my host `~/.claude/`
  state" (i.e. current `cc` behaviour) — explicit, not silent
- When the pool is empty, "Desktop" is the only option and a one-shot
  instruction block explains how to create tokens with `ccy --create-token`
- Maximum code reuse: the chooser is the existing
  `select_token()` function from
  `files/var/local/claude-yolo/lib/token-management.bash`, not a parallel
  re-implementation
- `cc` becomes a wrapper script at `/var/local/claude-code/cc`; the
  bashrc-side alias stays one line so `type cc` is still glanceable
- No change to `ccy` behaviour or token storage layout

## Non-Goals

- Teaching `cc` to *create* tokens. Token creation runs `claude setup-token` inside a container for OAuth isolation; that stays
  `ccy --create-token`-only. `cc` may print "use `ccy --create-token`
  to add tokens" but does not execute it.
- Moving the token pool. Tokens live where they live today
  (`~/.claude-tokens/ccy/tokens/`) — no migration, no rename, no
  `~/.claude-tokens/shared/`. (Considered and rejected — see Technical
  Decision 1.)
- Exporting `CLAUDE_CODE_NO_FLICKER` or `CLAUDE_CODE_DISABLE_MOUSE`
  from the wrapper. `NO_FLICKER` was dropped from ccy entirely
  (commit `a32c3d3`, Plan 00047 Path D); `DISABLE_MOUSE` is owned by
  Plan 00047 and entangled with the wheel-history bug — out of scope
  for `cc`. If a future per-invocation export becomes necessary, the
  wrapper is the right home, but not in this plan.
- Exporting container-only env vars from the wrapper. `IS_SANDBOX=1`
  bypasses Claude Code's root-detection (`entrypoint.sh:99`) and
  `CCY_DISABLE_SUSPEND=1` (`entrypoint.sh:103`) is paired with the
  Ink ctrl+z Dockerfile patch. Both are container-only by design;
  the host wrapper MUST NOT export either. Hard guardrail lifted
  from cancelled Plan 00036.
- Replacing `claude update && claude` semantics. The wrapper still
  runs `claude update` then `claude "$@"` after token selection (or
  with no token env in the Desktop case).
- Persisting the user's chooser selection across invocations. Each `cc`
  invocation re-prompts — same as `ccy` (no `--remember` flag, no state
  file).
- Adding `--token NAME` / `--list-tokens` / `--export-token` style
  flags to `cc`. If the user wants those, they already have them on
  `ccy`. This plan is about the interactive default only. (Could be
  added later if demand emerges.)
- Touching Plan 00036's bashrc include or the sentinel check. The
  `cc` function shape IS touched indirectly via Task 3.3 (cross-edit
  to 00036's Task 2.1), but the env-var export pattern and sentinel
  stay in 00036's scope.
- Resetting host `~/.claude/` profile state when switching tokens.
  The host CLI accumulates per-account state in `~/.claude/`; this
  plan only swaps the OAuth token, not the profile cache. Selecting
  token X while `~/.claude/` still references account Y may surface
  as a visible "selected token vs active profile" drift in the
  Claude UI. Documented limitation — out of scope to fix here.

## Context & Background

### Where things live today

- **`cc` alias** — `playbooks/imports/play-claude-code.yml:51-59`
  (`blockinfile` into `~/.bashrc` under marker
  `# ANSIBLE MANAGED: Claude Code Integration`). Current block:
  ```bash
  # Claude Code CLI alias and PATH
  export PATH="$HOME/.local/bin:$PATH"
  alias cc='claude update && claude'
  ```
- **`ccy` wrapper** — `files/var/local/claude-yolo/claude-yolo`,
  deployed to `/var/local/claude-yolo/claude-yolo` by
  `playbooks/imports/play-claude-yolo.yml:280-289`. Alias set in
  `files/home/bashrc-includes/claude-yolo.bash:9`:
  `alias ccy='/var/local/claude-yolo/claude-yolo'`.
- **Token storage** — `~/.claude-tokens/ccy/tokens/NAME.YYYY-MM-DD.token`
  (`CCY_ROOT="$HOME/.claude-tokens/ccy"` and
  `TOKEN_DIR="$CCY_ROOT/tokens"`, declared in `claude-yolo:111-112`).
- **Token-management library** —
  `files/var/local/claude-yolo/lib/token-management.bash`. Functions
  relevant to `cc`:
  - `colorize_expiry(expiry_date)` — pure date math + ANSI output (lib)
  - `list_tokens(token_dir, tool_name)` — lists tokens in a dir (lib)
  - `select_token(token_dir)` — interactive picker, sets
    `SELECTED_TOKEN` global, returns 0 on success (lib:440-584). The
    signature is one arg but every existing caller in `claude-yolo`
    passes two (e.g. `select_token "$TOKEN_DIR" "ccy"` at
    `claude-yolo:921, 1025`); the second is currently ignored.
  - `export_token` / `export_tokens_interactive` — token-export
    snippet (out of scope for `cc` but available)
  - `create_token` / `validate_token` — **container-dependent**, use
    `container_cmd` to run `claude --version` and `claude setup-token`.
    `cc` cannot use these; delegation to `ccy --create-token` is the
    intended path.
- **Common library (`common.bash`) — transitive dep**
  - `is_token_valid(token_file)` lives at **`common.bash:441-464`**,
    NOT in `token-management.bash`. `select_token` references it as
    a free symbol; the ccy wrapper resolves it because it sources
    `common.bash` before `token-management.bash`. **The `cc` wrapper
    must do the same**, or define a local `is_token_valid` shim.
  - `print_error` at `common.bash:49` is used throughout
    `token-management.bash` (lines 128, 159, 165, 272, 305, 319, 336,
    375, 399, 596, 613, 619, 629, 635, 661, 678).
  - `container_cmd` at `common.bash:40` is used in `validate_token`
    and `create_token` (out of scope for `cc`).
  - **`common.bash:30-34` runs `exit 1` at file scope** if
    `$CONTAINER_ENGINE` (default `podman`) is not on `PATH`. Sourcing
    `common.bash` on a host without podman therefore TERMINATES the
    sourcing shell. This is a hard blocker for naively sourcing the
    file from `cc`; see Decision 4 for the remediation path.
- **`select_token` unbound-var hazard** — at
  `token-management.bash:552, 564`, the function references
  `$GH_TOKEN` and `$IMAGE_NAME` inside the renew/create branches.
  These are exported by the ccy wrapper but unset on a bare host. Even
  if those branches are removed for host mode, the references must be
  guarded (or the wrapper must pre-export stubs) so that nothing
  crashes under `set -u`.
- **Token injection mechanism** — `ccy` reads the selected token file
  and passes it as `-e "CLAUDE_CODE_OAUTH_TOKEN=$CLAUDE_OAUTH_TOKEN"`
  to `podman run` (`claude-yolo:2567`). On the host, the equivalent
  is `export CLAUDE_CODE_OAUTH_TOKEN=$(cat …)` before exec'ing
  `claude`. The Claude Code CLI reads the same env var on both
  surfaces.
- **`select_token` Desktop-option gap** — the current implementation
  in `token-management.bash:440-584` offers only:
  - Numbered valid tokens (1, 2, 3, …)
  - `r1`, `r2`, … renew expired tokens (calls `create_token`)
  - `0` create new token (calls `create_token`)
    Options 0 and r\* require a container, so they cannot be reused
    verbatim on the host. This plan needs a small refactor (see
    Technical Decision 2).
- **Bashrc-includes auto-source loop** —
  `playbooks/imports/play-basic-configs.yml:143-156` already sources
  every file in `~/.bashrc-includes/`. The existing
  `claude-yolo.bash` include lives there. `cc`'s alias currently does
  NOT use a bashrc include — it's `blockinfile`-d directly into
  `~/.bashrc`. This plan leaves the alias location alone (still
  blockinfile), only changes its value.

### Why a wrapper script (not just inline bash in the alias)

Two reasons:

1. The chooser logic needs to source
   `/var/local/claude-yolo/lib/token-management.bash` and call
   `select_token`. That is multiple lines of bash with control flow,
   not a one-liner.
2. ~~Plan 00036 composition argument — obsolete after 00036 was
   cancelled.~~ Wrapper-script form stands on its own merit: keeps
   `~/.bashrc` clean, single source of truth for what `cc` does,
   and gives a natural per-invocation hook point if any future plan
   needs to gate behaviour on terminal detection (e.g. the
   `CLAUDE_CODE_DISABLE_MOUSE` decision Plan 00047 is still
   resolving).

### Cross-references

- [Plan 00036](../00036-cc-ccy-parity/PLAN.md) — **Cancelled**.
  Originally a parallel parity plan covering env vars; both vars
  obsoleted independently. Retained for history; this plan inherits
  its container-only-vars guardrail.
- [Plan 00047](../00047-claude-code-mouse-wheel-pageup/PLAN.md) —
  mouse-wheel-PageUp work. **Does NOT block this plan**, because
  this plan does not export `CLAUDE_CODE_DISABLE_MOUSE=1`.
- `files/var/local/claude-yolo/lib/token-management.bash` — the
  library this plan reuses.
- `playbooks/imports/play-claude-code.yml` — where the alias is
  defined; this plan modifies it.
- `playbooks/imports/play-claude-yolo.yml:223-243` — pattern for
  deploying lib files to `/var/local/claude-yolo/lib/`. This plan
  mirrors it for `/var/local/claude-code/`.

## Tasks

### Phase 1: Library refactor (mode arg + host-safe sourcing)

Two structural changes ship together. (1) `select_token()` gets a
`mode` arg so the same function serves ccy (`container`) and `cc`
(`host`). (2) The host-safe helpers `cc` needs are factored into a
new `common-pure.bash` so `cc` can source library code without
triggering `common.bash`'s podman-check `exit 1`.

- [x] ✅ **Task 1.0**: Factor `common-pure.bash` (audit-mandated):

  - [x] ✅ Created `files/var/local/claude-yolo/lib/common-pure.bash`
    containing `print_error`, `is_token_valid`, `COLOR_RED`,
    `COLOR_RESET` (the minimum surface `cc` needs).
  - [x] ✅ Edited `common.bash` to source `common-pure.bash` BEFORE
    the podman-check block, removed the duplicated function bodies,
    and dropped the orphan `export -f` lines for the moved functions.
    Used `# shellcheck source-path=SCRIPTDIR` to satisfy SC1091.
  - [ ] ⬜ Verify ccy still works end-to-end after the move
    (Phase 4 covers this — runs on host).
  - [x] ✅ Added `common-pure.bash` to the lib install loop in
    `play-claude-yolo.yml`.

- [x] ✅ **Task 1.1**: Added `mode` arg to `select_token` (default
  `container` so no existing caller changes). Host mode:
  Desktop pseudo-option (`d`), empty-pool short-circuit with banner,
  no create/renew branches, expired tokens shown read-only with
  "renew with: ccy --create-token" hint. Container-mode behaviour
  unchanged.

- [x] ✅ **Task 1.2**: Updated both `select_token` callers in
  `files/var/local/claude-yolo/claude-yolo` (lines 921, 1025) to
  pass `"container"` instead of the previously-ignored `"ccy"`.

- [x] ✅ **Task 1.3**: Audit complete pre-implementation:

  - [x] ✅ Confirmed `is_token_valid` was at `common.bash:441` and
    is now hosted in `common-pure.bash`.
  - [x] ✅ Confirmed `print_error` (was `common.bash:49`) is now in
    `common-pure.bash` and reachable from every `cc` code path.
  - [x] ✅ Documented source order: `common-pure.bash` then
    `token-management.bash` (no `common.bash` needed by `cc` at all).
  - [x] ✅ Host mode in `select_token` never references
    `$GH_TOKEN`/`$IMAGE_NAME` (branches are gated by `mode`).
    Belt-and-braces: wrapper still pre-exports empty stubs.

- [x] ✅ **Task 1.4**: Bumped `CCY_VERSION` 3.16.2 → 3.17.0 (minor:
  new capability — common-pure factoring + host mode added). Version
  comment names the lib change explicitly.

### Phase 2: Create the `cc` wrapper script

- [x] ✅ **Task 2.1**: Created `files/var/local/claude-code/cc`
  with all design points landed:
  - `set -e` only (not `set -u`); defensive `: "${GH_TOKEN:=}" "${IMAGE_NAME:=}"` stubs anyway.
  - Sources `common-pure.bash` then `token-management.bash` (no
    `common.bash` — bypasses the podman-check entirely).
  - Non-interactive shell (`[[ ! -t 0 ]] || [[ ! -t 1 ]]`) →
    skip chooser, go straight to plain claude.
  - `CCY_ROOT` and `TOKEN_DIR` defined locally.
  - Graceful degrade when `/var/local/claude-yolo/lib/` is missing
    → plain `claude update && exec claude "$@"`.
  - Calls `select_token "$TOKEN_DIR" "host"`.
  - `claude update` runs BEFORE the token export; `exec claude "$@"` runs WITH `CLAUDE_CODE_OAUTH_TOKEN` set.
  - Validates `sk-ant-oat01-` prefix and 90-120 byte length.
  - One-line banner: "Token: NAME" or "Token: Desktop (host
    ~/.claude/)".
- [x] ✅ **Task 2.2**: Banner copy lives in
  `_select_token_host_empty_pool_banner` inside
  `token-management.bash`. Says: "No tokens available — using
  Desktop (host ~/.claude/ OAuth)", names the missing dir, and
  points at `ccy --create-token`.
- [x] ✅ **Task 2.3**: `shellcheck -x` clean on the wrapper.
  `# shellcheck source-path=SCRIPTDIR` directive added so relative
  source paths follow correctly in lint.
- [ ] ⬜ **Task 2.4**: Host-side smoke test — deferred to Phase 4
  (CCY container has no `claude` binary and no token pool).

### Phase 3: Ansible deployment

- [x] ✅ **Task 3.1**: Added wrapper-deploy tasks to
  `playbooks/imports/play-claude-code.yml` — "Create
  /var/local/claude-code Directory" and "Install cc Wrapper Script",
  both `become: true`, root-owned, mode 0755. Also added
  `common-pure.bash` to the lib install loop in
  `play-claude-yolo.yml`.
- [x] ✅ **Task 3.2**: Updated the `blockinfile` to
  `alias cc='/var/local/claude-code/cc'` with a comment pointing
  at the wrapper. `claude update` responsibility moved into the
  wrapper. (Plan 00036 cancelled, so no shadow risk to coordinate.)
- [x] ✅ **Task 3.3**: ~~(Removed)~~ Plan 00036 cancelled — no
  coordination needed.
- [x] ✅ **Task 3.4**: `./scripts/qa-all.bash` clean. Also ran
  `ansible-playbook --syntax-check` on both modified playbooks per
  the project convention (memory: `project_ansible_219_task_name_colons`).

### Phase 4: Deploy and verify (user runs on host)

CCY container can only edit & commit. Deployment and live testing
happen on the host.

- [x] ✅ **Task 4.1**: User ran
  `ansible-playbook playbooks/imports/play-claude-code.yml` on host
- [x] ✅ **Task 4.2**: `cc` alias resolves and launches the wrapper
  (chooser renders) — confirmed on host
- [ ] ⬜ **Task 4.3**: Empty-pool sanity (test rig): rename
  `~/.claude-tokens/ccy/tokens/` to `…-bak/`, run `cc --version`,
  verify the instructional banner prints and Desktop fallback works;
  restore the dir afterwards
- [x] ✅ **Task 4.4**: Populated-pool sanity: chooser shows the
  named tokens plus the Desktop option — confirmed on host
- [x] ✅ **Task 4.5**: Token-selection sanity: a named token
  authenticates and `claude` answers (no 401) — confirmed on host
  after the stale-credential shadow fix (see 2026-06-18 note)
- [ ] ⬜ **Task 4.6**: Desktop-selection sanity: pick Desktop from
  the chooser, verify `claude` launches using host `~/.claude/`
  state (current pre-plan behaviour preserved)
- [ ] ⬜ **Task 4.7**: `ccy` regression check: run `ccy` (no flags)
  and verify its token chooser still shows the create/renew
  options as before — no behaviour change from the lib refactor

### Phase 5: Documentation & commit

- [x] ✅ **Task 5.1**: ~~(Obsolete — 00036 cancelled.)~~
- [x] ✅ **Task 5.2**: Searched `grep -rn '\bcc\b' docs/ README.md CLAUDE.md` and `grep -lE 'alias cc|claude update' docs/*.md README.md` — zero matches. No user-facing docs reference the
  `cc` shortcut, so nothing to update.
- [ ] 🔄 **Task 5.3**: Plan status flipped to "In Progress" with
  Phases 1-3 ✅; final flip to Complete happens after Phase 4
  verification on host.
- [ ] 🔄 **Task 5.4**: Code + plan state being committed together
  in this session (Phase 1-3 implementation commit).

## Dependencies

- **Supersedes**: [Plan 00036](../00036-cc-ccy-parity/PLAN.md)
  (Cancelled). Originally intended as a sibling; on review 00036's
  target env vars had both been independently obsoleted and the
  bashrc-include approach was a worse architectural fit than this
  plan's wrapper. 00036's only durable contribution — the
  container-only-vars guardrail — is preserved as a Non-Goal above.
- **Not blocked by**: [Plan 00047](../00047-claude-code-mouse-wheel-pageup/PLAN.md)
  (mouse-wheel-PageUp). This plan does not export
  `CLAUDE_CODE_DISABLE_MOUSE=1` and therefore does not propagate
  the wheel-history bug to host shells.
- **Builds on**: existing `token-management.bash` library — relies
  on its current API being stable

## Technical Decisions

### Decision 1: Share `~/.claude-tokens/ccy/tokens/` rather than introducing a shared dir

**Context**: Options considered were
(a) read from ccy's existing dir,
(b) move tokens to a neutral `~/.claude-tokens/shared/`,
(c) give `cc` its own pool at `~/.claude-tokens/cc/`.

**Decision**: Option (a). The user's stated goal is parity ("same
tokens"); a separate pool defeats that. A migration to a neutral
location requires a CCY version bump (token dir is hard-coded in the
ccy wrapper), a one-shot move task in Ansible, and ongoing maintenance
of a back-compat read path. Reading from the existing dir is zero
migration, zero risk, immediately useful.

**Date**: 2026-06-10

### Decision 2: Extend `select_token` with a mode vs. add `select_token_host`

**Context**: The existing `select_token` mixes UI rendering with
container-dependent actions (`create_token` / renew). Host `cc` needs
the UI but not those actions.

**Options Considered**:

1. Add a `mode` arg to `select_token`; branch on it for the action
   menu items.
2. Factor the rendering into a helper, then `select_token` and a new
   `select_token_host` are both thin wrappers around the renderer.
3. Duplicate the function into `select_token_host` and let them
   drift independently.

**Decision**: Option 1 (`mode` arg) with a default of `container` so
no existing caller signature changes. Branch on `mode` for the
menu-item lines (`token-management.bash:511-517` expired-renew menu,
`520, 545-566` action branches). Host mode must hard-disable both
the renew (`r*`) and create (`0`) branches AND avoid even
referencing `$GH_TOKEN`/`$IMAGE_NAME` (delete those lines from the
host branch, not just gate them — the references themselves trip
`set -u`). Option 2 (helper-factor) is more invasive than warranted
for one extra menu item; revisit if the function gains a third mode.
Option 3 is rejected — divergence is the cost of duplication.

**Date**: 2026-06-10

### Decision 3: Wrapper script at `/var/local/claude-code/cc`, not a bash function in a bashrc include

**Context**: User answered the design question directly: wrapper
script, mirroring `ccy`'s layout.

**Why this is right**:

- Multi-line chooser logic doesn't belong in `~/.bashrc`
- Sourcing `token-management.bash` from a wrapper script keeps
  imports out of the user's shell startup
- `type cc` stays glanceable: one line pointing at the script
- Composes cleanly with Plan 00036's function (which can call the
  wrapper rather than `claude` directly)
- Mirrors the `ccy` mental model: alias → script → real CLI

**Date**: 2026-06-10

### Decision 4: Source ccy's lib from `/var/local/claude-yolo/lib/` rather than copy it to `/var/local/claude-code/lib/`

**Context**: `cc` and `ccy` both need the chooser code. Two ways to
share it: (a) sourcer reaches into the other tool's lib directory;
(b) copy the file into a third neutral location.

**Critical complication discovered post-draft (audit finding)**:
sourcing the lib is NOT a single-file affair.

1. `select_token` references `is_token_valid`, which lives in
   `common.bash:441-464` — NOT in `token-management.bash`. So `cc`
   must source `common.bash` BEFORE `token-management.bash`, or
   define a local `is_token_valid` shim.
2. `common.bash:30-34` runs `exit 1` at file scope when
   `$CONTAINER_ENGINE` (default podman) is not on `PATH`. Sourcing
   that file on a host without podman would TERMINATE the user's
   shell. Stderr suppression does not fix this — `exit` ignores
   `2>/dev/null`.

**Decision**: Option (a) stands, but **factor the host-safe helpers
into a new `files/var/local/claude-yolo/lib/common-pure.bash`** that
contains only the pure functions `cc` needs: `print_error`,
`is_token_valid`, and any other free symbols `select_token` reaches
for. `common.bash` then sources `common-pure.bash` at the top so
nothing in ccy moves. `cc` sources only `common-pure.bash` +
`token-management.bash` — no podman check, no `exit 1` risk.

Rationale for factoring over stderr-suppression / subshell tricks:

- A `set -e` wrapper that sources a file which calls `exit 1` will
  always terminate the wrapper itself; there is no clean local
  recovery.
- Even sourcing inside `bash -c '…'` does not isolate the parent
  because the wrapper itself IS the bash shell being terminated.
- Factoring is one new file and a `source common-pure.bash` line at
  the top of `common.bash`. Trivial, reversible, no behaviour change
  for ccy.

If the user-pool dir `~/.claude-tokens/ccy/tokens/` is missing or
empty, the wrapper short-circuits to Desktop before any lib code
runs anyway — so on a `play-claude-code.yml`-only host the source
path is rarely hit. But it MUST be safe when it is hit, because the
user can `mkdir ~/.claude-tokens/ccy/tokens/` independently.

**Verify before implementation**: confirm `playbook-main.yml`
imports both `play-claude-code.yml` and `play-claude-yolo.yml`. Plan
draft asserted this without reading the file.

**Date**: 2026-06-10

### Decision 5: No `--token NAME` flag on `cc` (yet)

**Context**: `ccy --token NAME` lets the user skip the chooser non-
interactively. `cc` could mirror this.

**Decision**: Out of scope. The user's request was for the chooser.
Adding flags is an obvious follow-up if the chooser feels heavy in
practice, but YAGNI — add when actually needed. Documented here so
the next plan-bumper has the option pre-considered.

**Date**: 2026-06-10

## Success Criteria

- [ ] `files/var/local/claude-code/cc` exists, deploys to
  `/var/local/claude-code/cc`, mode 0755 root-owned
- [ ] `playbooks/imports/play-claude-code.yml` alias line reads
  `alias cc='/var/local/claude-code/cc'`
- [ ] `token-management.bash` exposes a host-safe entry point
  (`select_token` with mode arg, or `select_token_host`) that does
  not invoke `create_token`
- [ ] Running `cc` with at least one valid token in
  `~/.claude-tokens/ccy/tokens/` shows the chooser with the
  Desktop option appended
- [ ] Selecting a named token exports `CLAUDE_CODE_OAUTH_TOKEN`
  before `claude` launches; the active Claude account matches the
  token's identity
- [ ] Selecting Desktop runs `claude` with no env override; the
  active account is whatever host `~/.claude/` says
- [ ] Empty-pool path prints the instruction banner and falls
  through to Desktop without prompting
- [ ] `ccy` chooser still works exactly as before — create-token
  option, renew options, named-token selection all intact
- [ ] `qa-all.bash` clean on all bash changes

## Risks & Mitigations

| Risk                                                                                                              | Impact | Probability | Mitigation                                                                                                                                                                                                                                             |
| ----------------------------------------------------------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Refactoring `select_token` regresses `ccy` chooser                                                                | High   | Low         | Phase 1 keeps the existing default behaviour container-mode; Phase 4 includes an explicit `ccy` regression task                                                                                                                                        |
| Host `cc` reads `~/.claude-tokens/ccy/tokens/` before `play-claude-yolo` has ever run, dir doesn't exist          | Low    | Med         | Wrapper treats missing dir same as empty pool — Desktop is the only option, instruction banner explains how to create tokens                                                                                                                           |
| `/var/local/claude-yolo/lib/token-management.bash` missing because user only deployed `play-claude-code.yml`      | Low    | Low         | Wrapper detects missing lib path, falls through to plain `claude update && claude "$@"` with a one-line note. No hard failure on a stripped-down install                                                                                               |
| Token-management.bash starts depending on `IMAGE_NAME` / `container_cmd` in a way that host-mode can't satisfy    | Med    | Low         | Phase 1.3 audits this explicitly; if a hidden dep appears, host-mode wrapper provides stubs                                                                                                                                                            |
| CCY version bump triggered by lib edit — easy to forget                                                           | Low    | Med         | Lib lives under `files/var/local/claude-yolo/`. Pre-commit hook for ccy version is on the wrapper script, NOT the lib, so a lib-only edit may NOT trigger the hook. Bump `CCY_VERSION` manually and note in commit. Check pre-commit hook scope first. |
| User confused by "Desktop" label                                                                                  | Low    | Low         | Inline help text in the chooser disambiguates ("Desktop (host ~/.claude/ OAuth — current `cc` default behaviour)")                                                                                                                                     |
| Composition with Plan 00036 breaks because both plans want to redefine `cc`                                       | Med    | Med         | Task 3.3 explicitly captures the composition decision: 00036's function calls `/var/local/claude-code/cc` rather than `claude`. Document at landing time of either plan; the second plan to land carries the burden of verifying composition.          |
| Non-interactive `cc` invocation (scripts, CI, piped stdin) hangs on `read -p` inside the chooser                  | Med    | Med         | Wrapper detects non-interactive shell early (`[[ ! -t 0 ]] \|\| [[ ! -t 1 ]]`) and skips the chooser, going straight to Desktop fallback. Captured as a sub-bullet of Task 2.1.                                                                        |
| Sourcing `common.bash` terminates the shell on a host without podman (`common.bash:30-34` `exit 1` at file scope) | High   | Med         | Decision 4 mandates a new `common-pure.bash` factoring; `cc` sources only the pure helpers, never the podman-check block. `common.bash` keeps its current behaviour for ccy. Captured as Task 1.0.                                                     |
| Token swap leaves host `~/.claude/` profile cache mismatched with chosen token — visible UI drift                 | Low    | High        | Documented as a Non-Goal limitation. Not fixed in this plan; would require resetting `~/.claude/` per-token-swap which is a much larger change. User-facing: surface in chooser help text if it becomes a recurring confusion.                         |

## Timeline

- Phase 1: Refactor `select_token` to add Desktop option
- Phase 2: Create the `cc` wrapper script
- Phase 3: Ansible deployment
- Phase 4: Deploy and verify on host
- Phase 5: Documentation & commit

## Notes & Updates

### 2026-06-10

- Plan created. Design decisions captured from user via
  `AskUserQuestion`: share ccy's token pool, "Desktop" pseudo-option
  in chooser, wrapper script at `/var/local/claude-code/cc`, sibling
  plan (not extension/supersede) to Plan 00036.
- Research confirmed `cc` is a pure alias today in
  `playbooks/imports/play-claude-code.yml:51-59`. Token system is in
  `files/var/local/claude-yolo/lib/token-management.bash`. File-system
  functions (`list_tokens`, `select_token`, `is_token_valid`,
  `colorize_expiry`) are container-independent. `create_token` and
  `validate_token` use `container_cmd` — must stay
  `ccy --create-token`-only.
- Token injection on host is just `export CLAUDE_CODE_OAUTH_TOKEN=…`
  before exec — same env var that ccy passes via `-e` to podman.
- Composition with Plan 00036: orthogonal scopes (env vars vs token
  chooser). Plan 00036's function will call
  `/var/local/claude-code/cc` instead of `claude` directly so both
  contributions stack cleanly. Documented in Decision 3 and Task 3.3.
- Not blocked by Plan 00047 — this plan does NOT export
  `CLAUDE_CODE_DISABLE_MOUSE=1`, so the wheel-history bug does not
  propagate to host shells.

### 2026-06-10 — Audit pass (opus Plan agent) and edits applied

Plan put through an aggressive review by an opus Plan sub-agent
immediately after first draft. Verdict was "needs-edits-and-yes".
Material findings, each verified against the live code, and the edit
applied:

- **`is_token_valid` location wrong**: plan said it lived in
  `token-management.bash`; actually `common.bash:441-464`. Fixed in
  Context "Common library (`common.bash`) — transitive dep" bullet.
  Without this fix the wrapper would have failed at the first
  `select_token` call.
- **`common.bash:30-34` `exit 1` at file scope**: sourcing it on a
  host without podman would terminate the user's shell, NOT just
  print stderr. Fixed in Decision 4 — new strategy is to factor a
  `common-pure.bash` containing only the helpers `cc` needs, and
  have `common.bash` source it. New Task 1.0 captures this.
- **Unbound `$GH_TOKEN`/`$IMAGE_NAME` in `select_token`**
  (`token-management.bash:552, 564`): would crash under `set -u`.
  Task 1.3 now mandates guarding or removing these references; Task
  2.1 adds defensive `: "${GH_TOKEN:=}" "${IMAGE_NAME:=}"` stubs.
- **Non-interactive `cc` would hang on `read -p`**: added tty-check
  sub-bullet to Task 2.1 and a new row in Risks.
- **`claude update` runs with token env set**: split into two steps
  in Task 2.1 — `claude update` runs BEFORE the token export, then
  `exec claude "$@"` runs WITH it.
- **Composition with Plan 00036 is coupled, not orthogonal**: 00036's
  function shadows this plan's alias. Task 3.3 upgraded from "add a
  Notes entry" to "hard cross-edit Plan 00036's Task 2.1 to call
  `/var/local/claude-code/cc` and drop its duplicate
  `claude update`".
- **Pre-commit hook ONLY checks `claude-yolo` wrapper, not lib
  files** (verified at `scripts/git-hooks/pre-commit:58-60`): new
  Task 1.4 makes the manual CCY version bump explicit; the existing
  Risks row already flagged this.
- **Decision 2 (`mode` vs wrapper) was punting**: collapsed to
  Option 1 (mode arg with default `container`) so no existing caller
  changes.
- **Decision 4 referenced `playbook-main.yml` ordering without
  reading the file**: noted as a verification step inside Decision
  4\. Implementation must confirm.
- **Docs search was conditional**: Task 5.2 changed to a positive
  `grep -rn '\bcc\b'` task rather than "if mentions exist".
- **Profile-state drift between selected token and host
  `~/.claude/`**: added as Non-Goal and Risks row.

Plan README index entry left unchanged — the one-line summary still
fits the post-audit shape. No new external dependencies, no new
phases, but Phase 1 grew from 3 tasks to 5 (added 1.0 common-pure
factor, 1.4 version-bump discipline).

### 2026-06-10 — Plan 00036 cancelled, this plan no longer a "sibling"

Verified the user's hunch that the env vars driving 00036 had been
abandoned. Findings from `files/var/local/claude-yolo/entrypoint.sh`:

- `CLAUDE_CODE_NO_FLICKER=1` — removed in commit `a32c3d3` (Plan
  00047 Path D); line 106 carries an explanatory NOTE confirming the
  removal.
- `CLAUDE_CODE_DISABLE_MOUSE=1` — still exported at line 125 but
  owned by Plan 00047, which is mid-rework on the wheel-history bug
  it causes.
- `IS_SANDBOX=1` and `CCY_DISABLE_SUSPEND=1` — still exported,
  container-only, MUST NOT export on host.

Grep confirmed no script in this repo invokes `claude` outside the
`cc` path, so 00036's "global bashrc-include export for any shell
that runs claude" rationale had no real consumers — the wrapper
covers every actual invocation.

Plan 00036 marked Cancelled with full rationale in its Notes &
Updates. The container-only-vars guardrail was lifted into this
plan's Non-Goals. Task 3.3 (cross-edit to 00036) and Task 5.1 (note
to 00036) struck through as obsolete. Cross-references and
Dependencies sections rewritten to reflect supersede status.

### 2026-06-10 — Phases 1-3 executed

Implementation landed in a single session, in plan order:

**Phase 1 (library refactor)**

- New: `files/var/local/claude-yolo/lib/common-pure.bash` — pure
  helpers only (`print_error`, `is_token_valid`, `COLOR_RED`,
  `COLOR_RESET`). `# shellcheck source-path=SCRIPTDIR` directive
  added wherever it's sourced.
- `common.bash` sources `common-pure.bash` near the top, BEFORE
  the podman-check at lines 30-34. Duplicated `print_error` and
  `is_token_valid` bodies removed. Orphan `export -f` lines for
  those two functions removed. Other COLOR constants (GREEN,
  YELLOW, BLUE, BOLD) stayed in `common.bash` because only its
  own functions consume them (SC2034 hygiene).
- `token-management.bash`: `select_token` gained a `mode` arg
  (default `"container"` so no caller signature breaks). Host
  mode adds a Desktop pseudo-option (`d`), short-circuits empty
  pool to Desktop with a banner printed by new helper
  `_select_token_host_empty_pool_banner`, and never reaches the
  renew/create branches (so `$GH_TOKEN`/`$IMAGE_NAME` are
  unreachable on host). Container-mode menu, prompts, and return
  values are byte-identical to the prior implementation.
- `claude-yolo` wrapper: both `select_token` callsites (lines
  921, 1025) updated to pass `"container"` explicitly. Previously
  passed `"ccy"` which was silently ignored.
- `CCY_VERSION` bumped 3.16.2 → 3.17.0 (minor: net-new
  capability — host mode + common-pure factor).

**Phase 2 (wrapper script)**

- New: `files/var/local/claude-code/cc`. Sources only
  `common-pure.bash` + `token-management.bash` (skipping
  `common.bash` entirely). Graceful degrade if ccy lib missing
  → plain `claude update && claude`. Non-interactive shell
  detection skips the chooser. `claude update` runs BEFORE the
  token export. Token validated for `sk-ant-oat01-` prefix and
  90-120 byte length before being exported as
  `CLAUDE_CODE_OAUTH_TOKEN`. `exec claude "$@"` at the tail.
- `shellcheck -x` clean.

**Phase 3 (Ansible)**

- `play-claude-yolo.yml` lib install loop now lists
  `common-pure.bash` first.
- `play-claude-code.yml` gained two tasks (directory create +
  wrapper copy) before the bashrc `blockinfile`, which now
  contains `alias cc='/var/local/claude-code/cc'`.
- `qa-all.bash` clean; `ansible-playbook --syntax-check`
  clean on both modified playbooks.

**Phase 4** is host-only — user runs
`ansible-playbook playbooks/imports/play-claude-code.yml`
(and `play-claude-yolo.yml` so the new `common-pure.bash` lands)
and works through Tasks 4.1-4.7. CCY container has no `claude`
binary and no token pool, so no in-container smoke test is
possible.

**Phase 5.1-5.2** done in this session. 5.3-5.4 pending the
commit that closes this entry.

### 2026-06-10 — Design pivot: hard-couple via fail-fast (no graceful degrade)

User flagged that the soft-coupling-with-graceful-degrade approach
violates KISS and the project's fail-fast HARD RULE. Rework lands
the wrapper-only `cc` as a hard dependency on ccy:

- **Removed from `cc` wrapper**: graceful-degrade for missing
  `/var/local/claude-yolo/lib/` (used to fall through to plain
  `claude update && claude`). If the lib is missing, bash's
  `source` under `set -e` exits with an error — that is fail-fast.
- **Removed from `cc` wrapper**: non-interactive `[[ ! -t 0 ]] || [[ ! -t 1 ]]` → Desktop fallback. Replaced with an explicit
  exit 1 + message ("cc requires an interactive shell; call
  `claude` directly for scripts"). No silent degrade, no chooser
  hang, clear next step.
- **Added preflight assert to `play-claude-code.yml`**: a `stat`
  task with `failed_when: not ccy_lib_files.stat.exists` loops
  over `common-pure.bash` and `token-management.bash`. Fails the
  deploy immediately if the lib is missing, rather than at first
  `cc` invocation.
- **Flipped `playbook-main.yml` import order**: `play-claude-yolo`
  now imports before `play-claude-code`, so the lib is on disk
  before the preflight assert runs.

Considered: merging `play-claude-code.yml` and `play-claude-yolo.yml`
into a single play. Rejected — `play-claude-yolo.yml` is ~300 lines
of container plumbing (podman config, image build trigger, bashrc
includes); inlining it into the small upstream-CLI install play
would entangle two distinct concerns and bloat the latter.
Keeping them separate with a hard deploy-time assert + a
documented import-order constraint is leaner.

Risks-table row "Wrapper detects missing lib path, falls through
to plain claude update && claude" is now obsolete — that
behaviour was deleted. Left in the historical table as a marker
of the rejected approach; superseded by the fail-fast checks.

Decision 4's "verify playbook-main.yml imports both" verification
step is now satisfied AND tightened: the import order is fixed,
documented inline in playbook-main.yml, and protected by the
preflight assert.

### 2026-06-18 — Phase 4 host deploy surfaced a stale-credential shadow bug

First real host deploy of `play-claude-code.yml`. The chooser, token
selection, and `hasCompletedOnboarding` seeding all worked, but every
named-token session failed at the API call with
`Please run /login · API Error: 401 Invalid authentication credentials`,
while the identical token authenticates fine in the `ccy` container.

Root cause: the host `~/.claude/.credentials.json` held an **expired**
`claudeAiOauth` block from a previous Desktop `/login` (subscriptionType
`max`, expired ~4 June). claude consults that on-disk credential even with
`CLAUDE_CODE_OAUTH_TOKEN` exported and `/status` reporting the env token as
the active method — so the stale entry shadows the injected token and 401s.
The `ccy` container never has this file, which is exactly why the same token
works there. Env-var precedence was ruled out as the cause: no
`ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN` set, no `apiKeyHelper` in settings.

Fix (in `files/var/local/claude-code/cc`): in **named-token mode only**, drop
any higher-precedence auth env vars (`unset ANTHROPIC_API_KEY ANTHROPIC_AUTH_TOKEN`) and **park `~/.claude/.credentials.json` aside for the
session**, restoring it on exit via a `trap … EXIT INT TERM`. The launch was
changed from `exec claude` to `claude "$@"` so the trap actually fires. A
self-heal at startup restores the parked file if a prior session was
hard-killed before its trap ran. The Desktop pseudo-option leaves the file
untouched.

Why not `CLAUDE_CONFIG_DIR` isolation: its scope is undocumented and likely
relocates the whole config tree, which would orphan the user's host
`settings.json` / MCP servers / project history. Parking only the one
credential file is surgical and preserves everything else.

Confirmed on host after re-deploy: named-token `cc` now authenticates
(no 401). Tasks 4.1/4.2/4.4/4.5 ✅. Optional sanity checks remain:
4.3 (empty-pool banner), 4.6 (Desktop-selection), 4.7 (`ccy` chooser
regression).

Follow-up (same session): the hooks-daemon status line showed no token
for host `cc`, while `ccy` shows it. Not a daemon bug — the
`account_display` status handler reads `LAST_TOKEN` from
`~/.claude/.last-launch.conf`, which `ccy` writes via its launch config
but host `cc` never did (it only echoed the name). Added a
`write_status_token` helper to `cc` that writes that key on launch
(token name for a named token, `Desktop` for the Desktop option), so the
host status line reaches parity with `ccy` (👤 \<name>).
