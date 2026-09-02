# Plan 00048: Host `cc` Token-Source Parity with `ccy`

**Status**: In Progress (Phases 1-3 done; Phase 4 awaits a host deploy)
**Created**: 2026-06-10
**Owner**: joseph
**Priority**: Medium

## Overview

The host `cc` shortcut used to be a one-line alias (`claude update && claude`)
authenticating through the host's `~/.claude/` OAuth state. The containerised
`ccy` wrapper has a named-token system at `~/.claude-tokens/ccy/tokens/` with a
colour-coded chooser. This plan gives `cc` the same chooser, backed by the same
token pool, so switching Claude accounts on the host feels the same as in `ccy`.

`cc` is now a wrapper script at `/var/local/claude-code/cc` (mirroring
`/var/local/claude-yolo/claude-yolo`). It reuses `select_token()` from
`files/var/local/claude-yolo/lib/token-management.bash` in a new `host` mode,
which adds a "Desktop" pseudo-option meaning "use host `~/.claude/` state".
Token creation stays `ccy --create-token`-only; with an empty pool, Desktop is
the only option and a banner explains how to create tokens.

Full rationale, audit findings, the risk register and the Plan 00036
cancellation story: [DECISIONS.md](DECISIONS.md). The original long-form plan,
including all dated progress notes, is kept verbatim in
[PLAN_archive.md](PLAN_archive.md).

## Goals

- Host `cc` presents the same token chooser UX as `ccy`, sharing the pool at
  `~/.claude-tokens/ccy/tokens/`
- A "Desktop" option means "use my host `~/.claude/` state" (pre-plan `cc`
  behaviour) — explicit, not silent
- Empty pool: Desktop is the only option and a one-shot banner explains
  `ccy --create-token`
- Maximum reuse: the chooser is the existing `select_token()`, not a parallel
  re-implementation
- `cc` is a wrapper script at `/var/local/claude-code/cc`; the bashrc alias
  stays one line so `type cc` is glanceable
- No change to `ccy` behaviour or token storage layout

## Non-Goals

- Teaching `cc` to create tokens (needs a container; stays `ccy --create-token`)
- Moving the token pool (see Decision 1 in [DECISIONS.md](DECISIONS.md))
- Exporting `CLAUDE_CODE_NO_FLICKER` or `CLAUDE_CODE_DISABLE_MOUSE` from the
  wrapper (the former was dropped from ccy; the latter belongs to
  [Plan 00047](../Completed/00047-claude-code-mouse-wheel-pageup/PLAN.md))
- Exporting container-only env vars (`IS_SANDBOX`, `CCY_DISABLE_SUSPEND`) on
  the host — hard guardrail inherited from cancelled
  [Plan 00036](../Cancelled/00036-cc-ccy-parity/PLAN.md)
- Replacing `claude update && claude` semantics
- Persisting the chooser selection across invocations
- Adding `--token NAME` / `--list-tokens` style flags to `cc` (Decision 5)
- Resetting host `~/.claude/` profile state when switching tokens; the visible
  "selected token vs active profile" drift is a documented limitation

## Tasks

### Phase 1: Library refactor (mode arg + host-safe sourcing)

- [x] ✅ **Task 1.0**: Factor `common-pure.bash` (audit-mandated; Decision 4)
  - [x] ✅ Created `files/var/local/claude-yolo/lib/common-pure.bash` with
    `print_error`, `is_token_valid`, `COLOR_RED`, `COLOR_RESET`
  - [x] ✅ `common.bash` sources it before the podman check; duplicated
    bodies and orphan `export -f` lines removed
  - [ ] ⬜ Verify ccy still works end-to-end after the move (Phase 4, host)
  - [x] ✅ Added `common-pure.bash` to the lib install loop in
    `play-claude-yolo.yml`
- [x] ✅ **Task 1.1**: `mode` arg on `select_token` (default `container`);
  host mode adds Desktop (`d`), empty-pool banner, no create/renew branches
- [x] ✅ **Task 1.2**: Both `select_token` callers in `claude-yolo` pass
  `"container"` explicitly
- [x] ✅ **Task 1.3**: Pre-implementation audit complete
  - [x] ✅ `is_token_valid` and `print_error` hosted in `common-pure.bash`
  - [x] ✅ Source order documented: `common-pure.bash` then
    `token-management.bash`; `cc` never sources `common.bash`
  - [x] ✅ Host mode never references `$GH_TOKEN` / `$IMAGE_NAME`; wrapper
    still pre-exports empty stubs
- [x] ✅ **Task 1.4**: `CCY_VERSION` bumped 3.16.2 → 3.17.0

### Phase 2: Create the `cc` wrapper script

- [x] ✅ **Task 2.1**: Created `files/var/local/claude-code/cc`: sources
  `common-pure.bash` + `token-management.bash`, calls
  `select_token "$TOKEN_DIR" "host"`, runs `claude update` before the token
  export, validates the `sk-ant-oat01-` prefix and length, prints a one-line
  "Token: NAME" / "Token: Desktop" banner. Fail-fast on a missing lib or a
  non-interactive shell (Decision 6); parks `~/.claude/.credentials.json` in
  named-token mode (Decision 7); writes `LAST_TOKEN` for the status line
  (Decision 8)
- [x] ✅ **Task 2.2**: Empty-pool banner copy in
  `_select_token_host_empty_pool_banner` (`token-management.bash`)
- [x] ✅ **Task 2.3**: `shellcheck -x` clean on the wrapper
- [ ] ⬜ **Task 2.4**: Host-side smoke test — deferred to Phase 4 (CCY
  container has no `claude` binary and no token pool)

### Phase 3: Ansible deployment

- [x] ✅ **Task 3.1**: `play-claude-code.yml` creates `/var/local/claude-code`
  and installs the wrapper (root, 0755), with a preflight `stat` assert on
  the ccy lib files; `playbook-main.yml` imports `play-claude-yolo` first
- [x] ✅ **Task 3.2**: `blockinfile` alias is now
  `alias cc='/var/local/claude-code/cc'`
- [x] ✅ **Task 3.3**: ~~(Removed)~~ Plan 00036 cancelled — no coordination
  needed
- [x] ✅ **Task 3.4**: `./scripts/qa-all.bash` clean;
  `ansible-playbook --syntax-check` clean on both modified playbooks

### Phase 4: Deploy and verify (user runs on host)

- [x] ✅ **Task 4.1**: User ran
  `ansible-playbook playbooks/imports/play-claude-code.yml` on host
- [x] ✅ **Task 4.2**: `cc` alias resolves and launches the wrapper (chooser
  renders) — confirmed on host
- [ ] ⬜ **Task 4.3**: Empty-pool sanity: rename
  `~/.claude-tokens/ccy/tokens/` to `…-bak/`, run `cc --version`, verify the
  banner prints and Desktop fallback works; restore the dir afterwards
- [x] ✅ **Task 4.4**: Populated-pool sanity: chooser shows named tokens plus
  Desktop — confirmed on host
- [x] ✅ **Task 4.5**: Token-selection sanity: a named token authenticates
  with no 401 — confirmed on host after the stale-credential fix (Decision 7)
- [ ] ⬜ **Task 4.6**: Desktop-selection sanity: pick Desktop, verify `claude`
  launches with host `~/.claude/` state
- [ ] ⬜ **Task 4.7**: `ccy` regression check: `ccy` chooser still offers
  create/renew as before

### Phase 5: Documentation & commit

- [x] ✅ **Task 5.1**: ~~(Obsolete — 00036 cancelled.)~~
- [x] ✅ **Task 5.2**: Docs search for `cc` / `alias cc` / `claude update`
  across `docs/`, `README.md`, `CLAUDE.md` — zero matches, nothing to update
- [ ] 🔄 **Task 5.3**: Plan status flipped to "In Progress" with Phases 1-3
  ✅; final flip to Complete after Phase 4 verification on host
- [ ] 🔄 **Task 5.4**: Code + plan state committed together (Phase 1-3
  implementation commit)

## Dependencies

- **Supersedes**: [Plan 00036](../Cancelled/00036-cc-ccy-parity/PLAN.md)
  (Cancelled); its container-only-vars guardrail survives as a Non-Goal
- **Not blocked by**:
  [Plan 00047](../Completed/00047-claude-code-mouse-wheel-pageup/PLAN.md);
  this plan does not export `CLAUDE_CODE_DISABLE_MOUSE=1`
- **Builds on**: the `token-management.bash` library API staying stable

## Success Criteria

- [ ] `files/var/local/claude-code/cc` exists and deploys to
  `/var/local/claude-code/cc`, mode 0755 root-owned
- [ ] `play-claude-code.yml` alias line reads `alias cc='/var/local/claude-code/cc'`
- [ ] `token-management.bash` exposes a host-safe entry point that never
  invokes `create_token`
- [ ] With at least one valid token, `cc` shows the chooser with Desktop
  appended
- [ ] Selecting a named token exports `CLAUDE_CODE_OAUTH_TOKEN` before
  `claude` launches and the active account matches the token
- [ ] Selecting Desktop runs `claude` with no env override
- [ ] Empty-pool path prints the banner and falls through to Desktop without
  prompting
- [ ] `ccy` chooser still works exactly as before
- [ ] `qa-all.bash` clean on all bash changes

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00048-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan drafted, audited by an opus Plan sub-agent, Plan 00036 cancelled in
  its favour
- Phases 1-3 landed: `common-pure.bash` factor, `select_token` host mode,
  `cc` wrapper, Ansible deploy tasks, CCY 3.17.0
- Design pivot to hard fail-fast coupling with ccy (Decision 6)
- First host deploy; stale-credential 401 shadow bug found and fixed
  (Decision 7); status-line `LAST_TOKEN` parity (Decision 8)
- Remaining: Tasks 1.0 (ccy end-to-end), 2.4, 4.3, 4.6, 4.7, then Phase 5
  close-out
