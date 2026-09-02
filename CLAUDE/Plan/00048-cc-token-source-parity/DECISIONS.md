# Plan 00048 — Decisions, Research and Risk Register

Supporting document for [PLAN.md](PLAN.md). Durable design rationale and the
code-level findings that shaped it. The full original plan prose is in
[PLAN_archive.md](PLAN_archive.md).

## Where things live

- **`cc` alias** — `playbooks/imports/play-claude-code.yml`, `blockinfile` into
  `~/.bashrc` under marker `# ANSIBLE MANAGED: Claude Code Integration`. Before
  this plan it was `alias cc='claude update && claude'`; now it is
  `alias cc='/var/local/claude-code/cc'`.
- **`ccy` wrapper** — `files/var/local/claude-yolo/claude-yolo`, deployed to
  `/var/local/claude-yolo/claude-yolo` by `playbooks/imports/play-claude-yolo.yml`.
- **Token storage** — `~/.claude-tokens/ccy/tokens/NAME.YYYY-MM-DD.token`
  (`CCY_ROOT` / `TOKEN_DIR` in `claude-yolo`).
- **Token-management library** — `files/var/local/claude-yolo/lib/token-management.bash`.
  Host-safe functions: `colorize_expiry`, `list_tokens`, `select_token`,
  `export_token`, `export_tokens_interactive`. Container-dependent (use
  `container_cmd`): `create_token`, `validate_token` — these stay
  `ccy --create-token`-only.
- **Pure helpers** — `files/var/local/claude-yolo/lib/common-pure.bash`
  (`print_error`, `is_token_valid`, `COLOR_RED`, `COLOR_RESET`), sourced by
  `common.bash` before its podman check and by `cc` directly.
- **Token injection** — `ccy` passes `-e CLAUDE_CODE_OAUTH_TOKEN=…` to
  `podman run`; on the host `cc` exports the same variable before launching
  `claude`. The CLI reads the same env var on both surfaces.
- **Lib deploy pattern** — `play-claude-yolo.yml` lib install loop deploys to
  `/var/local/claude-yolo/lib/`; `play-claude-code.yml` mirrors it for
  `/var/local/claude-code/`.

## Audit findings that changed the design (pre-implementation)

An opus Plan sub-agent reviewed the first draft. Findings verified against
live code:

- `is_token_valid` lived in `common.bash`, not `token-management.bash`, so
  `select_token` cannot be sourced alone.
- `common.bash` ran `exit 1` at file scope when `$CONTAINER_ENGINE` (podman) is
  not on `PATH`. Sourcing it from a host wrapper would terminate the wrapper;
  stderr suppression does not help because `exit` ignores redirection. This
  drove Decision 4.
- `select_token` referenced `$GH_TOKEN` / `$IMAGE_NAME` in its renew/create
  branches, which are unset on a bare host. Host mode never reaches those
  branches, and `cc` pre-exports empty stubs as belt-and-braces.
- A non-interactive `cc` would hang on `read -p` in the chooser.
- `claude update` must run before the token export, not with it.
- The pre-commit hook checks only the `claude-yolo` wrapper for a CCY version
  bump, not the lib files, so a lib-only edit must bump `CCY_VERSION` manually.
- Selecting token X while host `~/.claude/` still references account Y can
  surface as profile drift in the Claude UI. Accepted as a Non-Goal.

## Plan 00036 cancellation

00048 was drafted as a sibling of Plan 00036 (env-var parity). Review showed
both of 00036's target env vars were independently obsoleted:
`CLAUDE_CODE_NO_FLICKER` was dropped from ccy in commit `a32c3d3` (Plan 00047
Path D) and `CLAUDE_CODE_DISABLE_MOUSE` is owned by Plan 00047. Grep confirmed
no script in this repo invokes `claude` outside the `cc` path, so 00036's
global-export rationale had no consumers. 00036 was cancelled; its
container-only-vars guardrail (`IS_SANDBOX`, `CCY_DISABLE_SUSPEND` must not be
exported on the host) is preserved as a 00048 Non-Goal. See
[Plan 00036](../Cancelled/00036-cc-ccy-parity/PLAN.md).

## Technical decisions

### Decision 1: Share `~/.claude-tokens/ccy/tokens/` rather than a shared dir

Options: (a) read ccy's existing dir, (b) move tokens to
`~/.claude-tokens/shared/`, (c) a separate `~/.claude-tokens/cc/` pool.
Chose (a). Parity means the same tokens; a separate pool defeats that. A
migration needs a CCY bump, a one-shot Ansible move and a back-compat read
path. Reading the existing dir is zero migration and immediately useful.
(2026-06-10)

### Decision 2: `mode` arg on `select_token`, not a `select_token_host` twin

Options: (1) add a `mode` arg and branch on it for the action menu items;
(2) factor rendering into a helper with two thin wrappers; (3) duplicate the
function. Chose (1) with default `container` so no existing caller changes.
Host mode hard-disables the renew (`r*`) and create (`0`) branches and never
references `$GH_TOKEN` / `$IMAGE_NAME`. Option 2 is more invasive than one
extra menu item warrants; revisit if a third mode appears. Option 3 rejected:
divergence is the cost of duplication. (2026-06-10)

### Decision 3: Wrapper script at `/var/local/claude-code/cc`

User chose a wrapper mirroring `ccy`'s layout. Multi-line chooser logic does
not belong in `~/.bashrc`; sourcing the lib from a script keeps imports out of
shell startup; `type cc` stays a one-liner; it mirrors the alias → script →
real CLI model. It also gives a natural per-invocation hook point if a future
plan needs terminal-detection gating. (2026-06-10)

### Decision 4: Source ccy's lib, via a new `common-pure.bash`

`cc` and `ccy` share the chooser code. Copying the file to a third location
was rejected in favour of sourcing from `/var/local/claude-yolo/lib/`. Because
`common.bash` exits at file scope without podman, the host-safe helpers were
factored into `common-pure.bash`; `common.bash` sources it at the top so
nothing in ccy moves, and `cc` sources only `common-pure.bash` plus
`token-management.bash`. Subshell or stderr tricks cannot isolate a `set -e`
wrapper from a sourced `exit 1`. `playbook-main.yml` imports
`play-claude-yolo` before `play-claude-code` so the lib is on disk before the
deploy-time preflight assert. (2026-06-10)

### Decision 5: No `--token NAME` flag on `cc` yet

The request was for the interactive chooser. `ccy` already has the flags;
YAGNI until demand emerges. (2026-06-10)

### Decision 6: Hard-couple `cc` to ccy via fail-fast, no graceful degrade

The first implementation fell through to plain `claude update && claude` when
the ccy lib was missing and silently skipped the chooser on a non-interactive
shell. The user flagged this as violating KISS and the fail-fast HARD RULE.
Rework: a missing lib makes `source` fail under `set -e`; a non-interactive
shell exits 1 with "cc requires an interactive shell; call `claude` directly
for scripts"; `play-claude-code.yml` gained a preflight `stat` assert over
`common-pure.bash` and `token-management.bash` that fails the deploy; the
`playbook-main.yml` import order was flipped and documented inline. Merging
the two plays was considered and rejected because `play-claude-yolo.yml` is
several hundred lines of container plumbing that would bloat the small
upstream-CLI play. (2026-06-10)

### Decision 7: Park `~/.claude/.credentials.json` in named-token mode

First host deploy: every named-token session hit
`API Error: 401 Invalid authentication credentials` while the same token
worked in `ccy`. Root cause: the host `.credentials.json` held an expired
`claudeAiOauth` block from a previous Desktop `/login`, and claude consults it
even with `CLAUDE_CODE_OAUTH_TOKEN` exported. Env-var precedence was ruled out
(no `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN`, no `apiKeyHelper`). Fix in
`cc`, named-token mode only: unset the higher-precedence auth vars, park the
credentials file aside for the session and restore it via
`trap … EXIT INT TERM` (launch changed from `exec claude` to `claude "$@"` so
the trap fires), with a startup self-heal for a hard-killed prior session.
Desktop mode leaves the file untouched. `CLAUDE_CONFIG_DIR` isolation was
rejected: its scope is undocumented and would likely orphan host
`settings.json`, MCP servers and project history. (2026-06-18)

### Decision 8: Write `LAST_TOKEN` for the status line

The hooks-daemon `account_display` status handler reads `LAST_TOKEN` from
`~/.claude/.last-launch.conf`, which `ccy` writes but host `cc` did not. `cc`
gained a `write_status_token` helper that writes the token name, or `Desktop`,
on launch so the host status line reaches parity. (2026-06-18)

## Risks and mitigations

| Risk                                                                               | Impact | Probability | Mitigation                                                                                                |
| ---------------------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------------- |
| Refactoring `select_token` regresses the `ccy` chooser                             | High   | Low         | Container mode is the default and byte-identical; Task 4.7 is an explicit `ccy` regression check          |
| Token dir absent because `play-claude-yolo` never ran                              | Low    | Med         | Missing dir is treated as an empty pool: Desktop is the only option, banner explains `ccy --create-token` |
| ccy lib missing on a `play-claude-code.yml`-only host                              | Low    | Low         | Deploy-time preflight assert fails the play; at runtime `source` fails under `set -e` (Decision 6)        |
| `token-management.bash` grows a hidden container dependency host mode cannot meet  | Med    | Low         | Task 1.3 audit; wrapper pre-exports `GH_TOKEN` / `IMAGE_NAME` stubs                                       |
| Lib-only edit misses the CCY version bump                                          | Low    | Med         | Pre-commit hook only covers the wrapper; bump `CCY_VERSION` manually (done: 3.16.2 → 3.17.0)              |
| User confused by the "Desktop" label                                               | Low    | Low         | Inline help text: "Desktop (host ~/.claude/ OAuth — current `cc` default behaviour)"                      |
| Non-interactive `cc` hangs on `read -p`                                            | Med    | Med         | Explicit exit 1 with a message naming `claude` as the scriptable entry point (Decision 6)                 |
| Sourcing `common.bash` terminates the shell on a host without podman               | High   | Med         | `common-pure.bash` factoring (Decision 4); `cc` never sources the podman-check block                      |
| Token swap leaves host `~/.claude/` profile cache mismatched with the chosen token | Low    | High        | Documented Non-Goal; credentials-file parking (Decision 7) fixes the 401, not the cosmetic drift          |
