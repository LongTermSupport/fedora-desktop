# Plan 00045: Project Personas — Unified Multi-Account Wrangling Across Tools

**Status**: ⬜ Not Started (awaiting Phase 1 decision gate)
**Created**: 2026-05-26
**Owner**: joseph
**Priority**: Medium
**Type**: Architecture + multi-phase implementation
**Tracking Issue**: [LongTermSupport/fedora-desktop#22](https://github.com/LongTermSupport/fedora-desktop/issues/22)

## Overview

The repo has a battle-tested multi-account pattern for GitHub: a
`github_accounts: {alias: username}` map in `localhost.yml` drives
per-alias SSH keys, per-alias bash functions (`gh-<alias-a>`, `git-<alias-a>`,
`clone-<alias-a>`, etc.), and a per-account scope audit in
`play-github-cli-multi.yml`. That pattern landed via Plans 00034 and
00035 and is the canonical way this machine talks to multiple GitHub
identities without crossing wires.

The user now has the same pain on **Cloudflare**: multiple Cloudflare
accounts, each needing their own `wrangler` auth context, and wants
the same shape of UX — `wrangler-<alias-a>` / `wrangler-lts` style
aliases that route every `wrangler` invocation to the correct
account without manual switching. Other CLIs are likely next (npm
publish accounts, aws profiles, gcloud configs, supabase, fly).
Hand-rolling each from scratch repeats work and creates four
slightly-different patterns the user has to remember.

This plan introduces a unifying abstraction: a top-level
`project_personas` map in `localhost.yml` that declares each
identity once and lists which tools that identity is used with.
Per-tool playbooks read `project_personas` **directly** — there is
no compat shim. `play-github-cli-multi.yml` is updated to prefer
`project_personas`; if it finds the legacy `github_accounts` map
instead, it fail-fasts with the exact migration the user must
make. The user owns the upgrade; the playbook owns the
detection. KISS.

For tools without native multi-session auth (wrangler is the
first such case — its OAuth is one-active-account-at-a-time),
the per-alias bash function explicitly exports the credentials
into the subprocess env (`CLOUDFLARE_API_TOKEN=… wrangler …`).
Secrets are stored in the **GNOME Keyring** via `secret-tool`,
not in `localhost.yml` and not in plaintext on disk. Per-persona
non-secret metadata (account IDs, display names) lives in
`project_personas` as normal YAML.

## Goals

- Single source of truth for multi-account identity declarations
  (`project_personas` in `localhost.yml`)
- New tool support is **additive**: declare a tool field per persona,
  run the per-tool playbook, get the same `tool-<alias>` UX shape as
  `gh-<alias>` provides today
- First new tool wired up: **wrangler** (Cloudflare Workers CLI),
  with `wrangler-<alias>` bash functions mirroring the `gh-<alias>`
  pattern
- `play-github-cli-multi.yml` consumes `project_personas`
  directly; users on the legacy `github_accounts` schema get a
  **fail-fast error with the exact YAML to paste** to migrate.
  No silent shim, no two-source-of-truth confusion.
- Secrets for tools that need per-invocation env-var injection
  (wrangler, future API-token-based tools) are stored in the
  **GNOME Keyring** via `secret-tool`. Not in `localhost.yml`,
  not in plaintext on disk, not in shell history.
- Per-persona setup script (`scripts/persona-setup.bash` or
  extend `gh-account-setup.bash`) handles the full add-a-persona
  flow end-to-end: edit `localhost.yml` → auth each tool →
  store any required secrets in keyring → verify
- Pattern is documented well enough that adding a third tool
  (e.g. npm) is a single new playbook + tool field, not an
  architectural decision

## Non-Goals

- Not implementing npm / aws / gcloud / supabase / fly support in
  this plan — only **gh (compat)** and **wrangler (new)**. The
  pattern is the deliverable; further tools come in follow-up plans
- Not renaming or restructuring the existing
  `play-github-cli-multi.yml` machinery — it stays put, reads its
  data from `github_accounts` as today (which is derived from
  `project_personas` during the rollout)
- Not changing the SSH key naming convention
  (`~/.ssh/github_<alias>`) — out of scope, would touch Plan 00035's
  surface
- Not building a GUI/wizard — CLI-only, consistent with `run.bash`
- Not encrypting persona-level metadata beyond what's already
  encrypted in `localhost.yml` (vault behaviour unchanged)

## Context & Background

### Existing pattern (what we're generalising)

`environment/localhost/host_vars/localhost.yml`:

```yaml
github_accounts:
  <alias-a>: "<gh-username-a>"
  lts: "LTSCommerce"
  joseph: "joseph-uk"
  <alias-b>: "<gh-username-b>"
```

`playbooks/imports/play-github-cli-multi.yml` reads that map and,
via Jinja templating into a `.bashrc-includes/gh-aliases.inc.bash`
block, generates per-alias bash functions: `gh-<alias-a>`, `git-<alias-a>`,
`clone-<alias-a>`, `remote-<alias-a>`, `gh-token-<alias-a>`,
`gh-<alias-a>-token-phpstorm`, plus per-alias SSH `Host` blocks in
`~/.ssh/config`. The `git()` wrapper auto-detects the right
account from the repo's remote via `git-account-helper`.

The same playbook also runs a per-account OAuth scope audit
(`github_required_scopes` var) to catch missing scopes before they
break a workflow, and uses `gh ssh-key add` to upload pubkeys
programmatically rather than the old paste-into-browser step.

### Why generalise

Each tool has its own authentication and switching ergonomics, but
the **shape** of the multi-account UX is the same one across all
of them:

1. List of named identities (aliases)
2. Each identity has a per-tool credential / account ID
3. User wants `<tool>-<alias>` bash functions to run the tool as
   that identity without manual switching
4. User wants a single setup command per persona that auths all
   their tools at once
5. User wants `<tool>-status` / `<tool>-list` helpers and a
   `<tool>-whoami`

Rebuilding this shape per tool is repetitive and slightly drifts
each time. A common spine reduces that drift to zero.

### Per-tool identity values are different

GitHub: each alias maps to a **username**. SSH key generation,
`gh auth switch`, and the API user lookup all key off that.

Cloudflare / wrangler: each alias maps to a **Cloudflare Account
ID** (a 32-char hex string) plus optionally an API token name.
Wrangler 3.x has native multi-account support via
`CLOUDFLARE_ACCOUNT_ID` env var per-command and `wrangler login` /
`wrangler logout` for OAuth, plus `CLOUDFLARE_API_TOKEN` for
non-interactive (CI) auth. The active account is whichever the
current OAuth session is bound to.

This means the persona schema can't just be `gh: true` — it has to
carry per-tool identity values. The proposed schema below handles
that.

### Proposed schema (user's draft, refined)

User proposed:

```yaml
personas:
  joseph-personal:
    alias: joseph
    gh: true
    wrangler: true
```

Refined to carry per-tool identity values:

```yaml
project_personas:
  joseph:
    name: "Joseph Personal"
    tools:
      gh:
        username: joseph-uk
      wrangler:
        account_id: "a1b2c3d4e5f6..."
        account_name: "Joseph Personal"
  <alias-a>:
    name: "<Display Name A>"
    tools:
      gh:
        username: <gh-username-a>
      wrangler:
        account_id: "0123456789ab..."
        account_name: "<Display Name A>"
  lts:
    name: "Long Term Support"
    tools:
      gh:
        username: LTSCommerce
      # no wrangler entry — LTS has no Cloudflare account
```

Top-level key is the **alias** (matches existing
`github_accounts` keys, so the SSH key path stays
`~/.ssh/github_joseph`). The `tools` map declares which tools that
persona is set up for, and carries each tool's identity value.

### Relevant prior plans

- **Plan 00034** — `config_github_account` tracking in
  `localhost.yml`. Set the precedent that account identity is
  declared in `localhost.yml`, not inferred from active session.
- **Plan 00035** — gh multi-account hardening. Established the
  per-account OAuth scope audit, programmatic SSH key upload via
  `gh ssh-key add`, and `scripts/gh-account-setup.bash` as the
  per-account setup primitive. This plan extends that primitive
  to be per-**persona** (all tools at once).

### Relevant code locations

- `environment/localhost/host_vars/localhost.yml` — schema lives
  here (vault-encrypted values stay vault-encrypted)
- `playbooks/imports/play-github-cli-multi.yml` — existing gh
  template, ~1120 lines, generates `gh-aliases.inc.bash`
- `scripts/gh-account-setup.bash` — per-account interactive
  setup; this plan widens it to per-persona
- `vars/` — likely home for a new `vars/persona-defaults.yml` if
  we go that route

## Tasks

### Phase 1: Research & Decision Gate

- [ ] ⬜ **Research wrangler native multi-account capabilities** —
  read upstream wrangler docs and source. Confirm: (a) does
  `wrangler login` support a per-config-dir model like gh
  (probably no — wrangler stores OAuth in a single
  `~/.config/.wrangler/config/default.toml`), (b) does
  `CLOUDFLARE_ACCOUNT_ID` + `CLOUDFLARE_API_TOKEN` set per
  invocation fully override any active OAuth session, (c) what
  scopes/permissions a per-persona API token needs for typical
  Workers / Pages / KV / R2 / D1 use. Output: short notes file
  in this plan dir.
- [ ] ⬜ **Research GNOME Keyring + `secret-tool` for token
  storage** — confirm `secret-tool` is in F43 base or comes from
  a known package (libsecret), confirm it's already
  installed/usable on the daily-driver, confirm the lookup
  works at shell function call time without an interactive
  unlock prompt (the keyring is unlocked at GNOME login on
  this machine). Output: notes for Decision 4.
- [ ] ⬜ **Decide tool selection for this plan** — confirm
  wrangler is the only new tool in scope. List candidate
  follow-up tools (npm, aws, gcloud, supabase, fly) in Notes
  section so future plans can pick them up without redoing
  this analysis.
- [ ] ⬜ **Decision gate** — present Phase 1 research + the
  three open decisions (1: schema shape — already proposed; 3:
  wrangler auth method; 4: secret storage mechanism) to user.
  Block remaining phases on user approval. Update Tasks below
  if research changes the plan shape.

### Phase 2: Schema definition + gh playbook fail-fast detection

- [ ] ⬜ **Define `project_personas` schema** in a comment block
  at the top of `localhost.yml`. Documents required vs optional
  keys per tool, with worked examples for `gh` and `wrangler`.
- [ ] ⬜ **Populate `project_personas`** in `localhost.yml` for
  the existing aliases currently in `localhost.yml`'s
  `github_accounts`, initially with only the `gh` tool entry —
  values mirror today's `github_accounts` exactly.
- [ ] ⬜ **Remove the legacy `github_accounts` block** from
  `localhost.yml` once `project_personas` is populated. Single
  source of truth from the user's side — no two YAML blocks
  declaring the same thing.
- [ ] ⬜ **Update `play-github-cli-multi.yml` schema detection**
  — at the top of the play, add a preflight that:
  1. If `project_personas is defined and project_personas | length > 0` → derive a local `_gh_accounts` fact from it
     (`{alias: tools.gh.username for alias, p in project_personas.items() if p.tools.gh is defined}`)
     and use it throughout the rest of the play.
  2. Else if legacy `github_accounts is defined` → **fail
     fast** with a printable YAML snippet showing the exact
     `project_personas` block the user should paste to
     migrate, plus the one-line instruction to delete the
     `github_accounts` block.
  3. Else → existing "no accounts configured" skip path.
- [ ] ⬜ **Rename internal references** inside
  `play-github-cli-multi.yml` from `github_accounts` to the
  derived `_gh_accounts` fact — mechanical find-and-replace,
  no logic change. Reduces the risk of someone re-adding a
  direct `github_accounts` consumer.
- [ ] ⬜ **Add a one-line note** in
  `scripts/gh-account-setup.bash --add` that appends to
  `project_personas` (not `github_accounts`) going forward.
- [ ] ⬜ **Verify gh side end-to-end** on host — generated
  `gh-aliases.inc.bash` after the migration must be
  byte-identical to before the migration for the same four
  accounts. (`diff` the two outputs.)

### Phase 3: Wrangler base install

- [ ] ⬜ **Create `playbooks/imports/optional/common/play-wrangler.yml`**
  — installs wrangler globally via npm under the user's nvm
  context (follows `play-nvm-install.yml` precedent). Pinned
  version with `npm install -g wrangler@X.Y.Z` for
  reproducibility.
- [ ] ⬜ **Add wrangler to `playbook-main.yml`** as an opt-in
  import (commented out by default — user uncomments after
  confirming they want it; matches `play-ddev.yml` precedent).
- [ ] ⬜ **Verify install** with `wrangler --version` on host.

### Phase 4: Wrangler multi-account playbook + bash functions

- [ ] ⬜ **Create `playbooks/imports/optional/common/play-wrangler-multi.yml`**
  — mirrors `play-github-cli-multi.yml` shape:

  - Preflight: assert `secret-tool` is on PATH (libsecret
    installed); assert wrangler version meets minimum
  - Read `project_personas`, filter to personas with
    `tools.wrangler` declared
  - Preflight: for each such persona, assert a token is
    already stored in the keyring under the agreed attribute
    scheme (`persona=<alias> tool=wrangler attr=api-token`).
    Fail-fast with the exact `persona-setup.bash --set-token=<alias> wrangler` command if missing — playbook
    must NOT prompt for tokens itself (separation of concerns:
    setup script handles interactive secret entry; playbook
    only deploys derived artifacts)
  - Generate `~/.bashrc-includes/wrangler-aliases.inc.bash`
    with per-alias bash functions (see next task)

- [ ] ⬜ **Implement `wrangler-<alias>` function shape** —
  every call explicitly exports the persona's credentials into
  the subprocess env, so the function works regardless of any
  prior `wrangler login` state. Function body:

  ```bash
  function wrangler-<alias-a>() {
      local token
      if ! token=$(secret-tool lookup persona <alias-a> tool wrangler attr api-token 2>/dev/null); then
          echo "ERROR: no wrangler API token stored for persona '<alias-a>'." >&2
          echo "Run: persona-setup.bash --set-token=<alias-a> wrangler" >&2
          return 1
      fi
      CLOUDFLARE_API_TOKEN="$token" \
      CLOUDFLARE_ACCOUNT_ID="<account_id from project_personas>" \
          command wrangler "$@"
  }
  ```

  Key properties:

  - Token never written to disk in plaintext after initial
    `secret-tool store`
  - Token never appears in process listings outside the
    `wrangler` subprocess (env vars on `command wrangler` are
    only visible to that process's `/proc/<pid>/environ`,
    readable only by the same user/root — same threat surface
    as today's gh tokens)
  - No state leaks between calls (no global env vars set, no
    `wrangler login` switch needed)
  - Each per-alias function is a separate templated block, so
    `wrangler-lts`, `wrangler-joseph` etc. share zero state

- [ ] ⬜ **Generate helper functions** in the same include:

  - `wrangler-list` — lists personas with `tools.wrangler`
    declared, shows account name + account ID (non-secret)
  - `wrangler-whoami` — runs `wrangler whoami` under the
    currently-active alias context (requires user to specify
    which alias to query — there is no "currently active" in
    this model since every call is per-alias-prefixed)
  - `wrangler-status` — iterates all wrangler personas, makes
    one read-only API call each (e.g. `wrangler whoami`),
    reports pass/fail per persona

- [ ] ⬜ **Per-account health audit** in the playbook — for
  each persona with wrangler configured, verify the keyring
  token works end-to-end via one read-only call (e.g.
  `wrangler whoami` with the persona's env vars exported).
  Fail-fast with the exact remediation command per persona,
  same pattern as the gh scope audit.

### Phase 5: Per-persona setup script

- [ ] ⬜ **Create `scripts/persona-setup.bash`** as the
  top-level dispatcher (separate from the now-stable
  `gh-account-setup.bash`, which it calls into for the gh
  tool). Keeps each tool's setup logic isolated and avoids
  growing one file into a god-script.
- [ ] ⬜ **Implement `--add=<alias>` mode** — interactive
  walk-through: prompt for display name, prompt per supported
  tool ("set up for gh? y/n", "set up for wrangler? y/n"),
  delegate to the per-tool setup flow if yes, append the
  persona block to `localhost.yml`.
- [ ] ⬜ **Implement `--set-token=<alias> <tool>` mode** —
  reads a token from stdin (or prompts with `read -s`),
  validates it with one read-only API call, then stores it in
  the GNOME Keyring via `secret-tool store --label="<tool> token for <alias>" persona <alias> tool <tool> attr api-token`.
  Never echoes the token, never writes it to disk, never
  passes it on the command line where another user could
  see it in `ps`.
- [ ] ⬜ **Implement `--setup-all` mode** — iterates
  `project_personas`, for each persona × tool combo runs the
  tool's auth/keygen flow if not already complete (e.g. for
  wrangler: check keyring for token; if missing, prompt
  user to paste one and validate it). Idempotent — re-running
  with everything in place is a no-op with success logs.
- [ ] ⬜ **Implement `--check` mode** — read-only health
  verification across all personas × tools. Reports a per-cell
  status table (`<alias-a>/gh: OK`, `<alias-a>/wrangler: MISSING TOKEN`,
  `lts/gh: SCOPES INCOMPLETE`, etc.). No prompts, no writes.
- [ ] ⬜ **Hook into `run.bash`** — `persona-setup.bash --setup-all` runs as part of the fresh-install flow,
  in the right ordering relative to the existing gh setup
  (replaces the current `gh-account-setup.bash --setup-all`
  call site).

### Phase 6: Docs

- [ ] ⬜ **Migration note** in `docs/` — short page covering:
  the YAML shape change from `github_accounts` to
  `project_personas`, the fail-fast message users will see on
  first run after upgrade, and the keyring-based token storage
  model for tools like wrangler.
- [ ] ⬜ **Add to `docs/README.md` index** under a new
  "Multi-account tooling" section.
- [ ] ⬜ **Update `CLAUDE/AnsibleStyle.md`** if the
  persona-reading pattern introduces a convention worth
  codifying for future tool integrations (e.g. "playbooks that
  consume `project_personas` must filter by `tools.<name>` and
  fail-fast on legacy schema").
- [ ] ⬜ **Document the "add a new tool" recipe** — a 5-step
  checklist for the next contributor adding npm / aws / etc.,
  covering: (1) per-tool fields in `project_personas`, (2) new
  `play-<tool>-multi.yml`, (3) bash function template shape
  with explicit env-var export if secret-bearing, (4) keyring
  attribute scheme `persona=<alias> tool=<tool> attr=<…>`,
  (5) `persona-setup.bash` dispatcher entry.

### Phase 7: QA & validation

- [ ] ⬜ `./scripts/qa-all.bash` passes for all changed
  bash/ansible files
- [ ] ⬜ `ansible-playbook --syntax-check` passes for all
  new/modified playbooks
- [ ] ⬜ Verify gh fail-fast path on host — temporarily restore
  the legacy `github_accounts` block, run the playbook, confirm
  the error message contains a copy-pasteable migration YAML
- [ ] ⬜ Deploy on host post-migration, verify gh behaviour is
  byte-identical to before (`diff` `gh-aliases.inc.bash` outputs)
- [ ] ⬜ Deploy on host, verify wrangler `wrangler-<alias>`
  functions work for at least two personas with real Cloudflare
  accounts; confirm via `strace`/`/proc` that the token only
  appears in the wrangler subprocess env, not in the parent shell
- [ ] ⬜ Verify `--check` mode catches a deliberately broken
  persona (e.g. revoke a token, expect clear failure)
- [ ] ⬜ Verify keyring lookup latency is acceptable (one
  `secret-tool lookup` per `wrangler-<alias>` call — should be
  \<10ms; if not, cache for the shell session)

## Dependencies

- **Depends on**: Plan 00035 (gh multi-account hardening) —
  this plan modifies `play-github-cli-multi.yml` (the schema
  detection at the top). Plan 00035's remaining phases edit
  different sections of the same file; coordinate ordering so
  edits don't collide. The `github_accounts` consumption
  pattern this plan changes is itself stable.
- **Blocks**: any future plan to add npm / aws / gcloud /
  supabase / fly multi-account support — those will use this
  plan's `project_personas` schema and bash function template
  - keyring pattern.
- **Related**: Plan 00034 (`config_github_account` in
  `localhost.yml`) — same principle (identity declared in
  config, not inferred from session).

## Technical Decisions

### Decision 1: Schema shape (top-level alias key vs nested under `tools`)

**Context**: Two viable shapes for the persona map.

**Options**:

1. Alias-keyed map with `tools` sub-map per persona
   (recommended above)
2. Tool-keyed map, alias nested inside (`gh: {<alias-a>: {...}}, wrangler: {<alias-a>: {...}}`)

**Recommendation**: Option 1. Keeps each persona's full
declaration in one place, easier to read at a glance, and
mirrors how `github_accounts` is structured today (alias is
the primary key).

**Status**: Awaiting Phase 1 user approval.

### Decision 2: KISS — fail-fast detection, no compat shim

**Context**: User explicitly rejected the compat-shim approach
on review (2026-05-26): *"lets not have complicated shims/compat
etc - KISS let the user handle upgrading the config as required"*.

**Decided**: `play-github-cli-multi.yml` reads `project_personas`
directly. If only the legacy `github_accounts` block is present,
the playbook **fail-fasts** with a printable YAML snippet showing
the exact `project_personas` migration block, plus the
instruction to delete the legacy block. The user makes the
one-time edit. No two-source-of-truth, no silent derivation.

**Why KISS wins here**: only one user (and one machine) needs
to migrate today. The cost of a "complicated shim" — Jinja
resolver, QA assertion that derived equals manual, risk of
subtle divergence — buys nothing for a one-machine migration.
The fail-fast message IS the migration guide.

**Date**: 2026-05-26

### Decision 3: Wrangler auth method (API token + env-var injection)

**Context**: Wrangler supports both `wrangler login` (OAuth)
and `CLOUDFLARE_API_TOKEN` env var. OAuth stores a single
active session in `~/.config/.wrangler/`, so switching accounts
means logout/login cycles — incompatible with the
per-invocation `wrangler-<alias>` model.

**Decided**: API tokens, one per persona, injected via env vars
on every invocation. `wrangler-<alias-a>` exports
`CLOUDFLARE_API_TOKEN=… CLOUDFLARE_ACCOUNT_ID=…` only on the
`command wrangler` subprocess. No stateful login switching.

**Why**: matches the user's stated need *"we will need to have
something that explicitly exports vars eg the bash functions"*.
Also matches what `gh-<alias-a>` does conceptually (every call is
scoped to one persona, no implicit "currently active" state to
get wrong).

**Date**: 2026-05-26 (subject to Phase 1 confirmation that
wrangler doesn't have an undocumented per-config-dir mode)

### Decision 4: Secret storage — GNOME Keyring via `secret-tool`

**Context**: Per-persona API tokens are sensitive credentials
that need to be available to bash functions at call time but
must not sit on disk in plaintext.

**Options considered**:

1. **Plaintext files in `~/.config/personas/<alias>/<tool>.env`
   mode 0600** — dirt-simple, same threat model as `~/.ssh/`
   keys today. Downside: any process running as the user can
   `cat` the file; any backup tool grabs them unencrypted.
2. **GNOME Keyring via `secret-tool` (libsecret)** — encrypted
   at rest, unlocked at GNOME login (so no per-call password
   prompt on the daily-driver), per-user isolation enforced
   by the keyring daemon (other users on the box can't read
   it, even with file access). Standard CLI:
   `secret-tool store --label="…" persona <alias> tool <tool> attr api-token`
   / `secret-tool lookup persona <alias> tool <tool> attr api-token`.
   Already what `gh` itself uses internally for its OAuth
   tokens on this machine.
3. **`pass` (passwordstore.org)** — encrypted at rest with
   GPG, well-regarded, but adds a GPG-keyring dependency and
   another CLI for the user to learn.
4. **`ansible-vault encrypt_string` in `localhost.yml`** —
   already the pattern for `github_ssh_passphrase`. Downside:
   `vault-pass.secret` sits on disk, so the encryption-at-rest
   guarantee is only as strong as that file's permissions.
   Also awkward to decrypt at bash-function call time (would
   need to spawn `ansible-vault decrypt` per call).

**Decided**: Option 2 (GNOME Keyring). Right balance of
security + ergonomics for a single-user GNOME desktop. Per-call
lookup is fast (\<10ms), no interactive unlock prompt because
the keyring is unlocked at GNOME login, encrypted at rest,
backups don't grab plaintext tokens, and we get free
per-tool/per-persona attribute scoping via `secret-tool`'s
key-value attribute scheme.

**Fallback**: if Phase 1 finds `secret-tool` isn't reliably
available on the deployment target, fall back to Option 1
(0600 files in `~/.config/personas/`) — same threat model as
SSH keys, acceptable for the daily-driver use case.

**Date**: 2026-05-26 (subject to Phase 1 keyring-availability
research)

## Success Criteria

- [ ] `project_personas` is the **only** identity map in
  `localhost.yml` after migration; the legacy `github_accounts`
  block is removed
- [ ] `play-github-cli-multi.yml` run before migration
  fail-fasts with a copy-pasteable migration block; run after
  migration produces byte-identical `gh-aliases.inc.bash`
  to the pre-migration output (no regression in the gh UX)
- [ ] `wrangler-<alias>` bash functions work for at least two
  Cloudflare accounts on the host, with no manual `wrangler login` between commands, and with API tokens fetched from
  the GNOME Keyring at call time (never read from disk)
- [ ] `persona-setup.bash --check` produces a clear pass/fail
  report for every persona × tool combination, and
  `--set-token` correctly stores and validates a token
  without ever writing it to disk in plaintext
- [ ] Adding a third tool (in a follow-up plan) requires
  only: a new `play-<tool>-multi.yml` + a new `tools.<tool>`
  field per persona that needs it + (if secret-bearing) a
  per-tool entry in `persona-setup.bash`'s setup dispatcher
- [ ] `./scripts/qa-all.bash` passes

## Risks & Mitigations

| Risk                                                                                                       | Impact                                                                                                                                                                     | Mitigation                                                                                                                                                                        |
| ---------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Wrangler has no real per-account isolation — only one active OAuth at a time                               | High — confirms the API-token + env-var route                                                                                                                              | Phase 1 research confirms up front; API tokens with per-persona keyring entries is the chosen path, well-trodden by `gh` itself                                                   |
| `secret-tool` / GNOME Keyring unavailable or locked at function call time                                  | High — `wrangler-<alias>` would fail at call time                                                                                                                          | Phase 1 confirms `secret-tool` is installed and the keyring is unlocked at GNOME login on the daily-driver; Decision 4 fallback to 0600 files if not viable                       |
| Token leak via shell history if user accidentally pastes it on the command line                            | High                                                                                                                                                                       | `persona-setup.bash --set-token` reads from stdin (or `read -s`), never accepts the token as an argv parameter; documented in `--help` and refused if `$1` looks like a token     |
| Token leak via env var visibility in `ps -e` / `/proc/<pid>/environ`                                       | Low — `/proc/<pid>/environ` is mode `0400` and owned by the process user, so only the same user or root can read it. This is the same threat surface as today's gh tokens. | Use `command wrangler` (not `env wrangler` or `KEY=val command`) so env vars live only on the wrangler subprocess, never exported into the parent shell                           |
| Persona schema accretes per-tool flags ad-hoc, becomes a kitchen sink                                      | Medium long-term                                                                                                                                                           | Phase 6 "add a new tool" recipe enforces a consistent shape: each new tool gets a documented `tools.<name>` schema entry; reviewers reject ad-hoc fields without playbook support |
| User on legacy `github_accounts` ignores the fail-fast message and downgrades the playbook to make it work | Low                                                                                                                                                                        | Fail-fast message includes the rationale ("two sources of truth would silently diverge"); migration is one block edit, not a refactor                                             |
| User has only one Cloudflare account today; building multi-account is YAGNI                                | Low — the schema generalises regardless                                                                                                                                    | Even with one account, the abstraction lets npm/aws/etc. land cheaply later. Phase 1 user confirms before commit                                                                  |

## Timeline

- Phase 1: Research & decision gate (foundational, blocks all
  later phases)
- Phase 2: Schema definition + gh playbook fail-fast detection
  (must precede any per-tool playbook so the schema is settled)
- Phase 3-4: Wrangler base install, then wrangler multi-account
- Phase 5: Per-persona setup script (can start once Phases 2+4
  shape is settled)
- Phase 6: Docs (concurrent with Phase 5)
- Phase 7: QA & validation (gates plan completion)

## Notes & Updates

### 2026-05-26 — plan created

- Originated from user request: "I need gh like multi account
  wrangler … wonder if we need a global 'project-personas'
  object which gh and wrangler and other tools can be included
  in".
- Confirmed `github_accounts` exists in `localhost.yml` with
  the user's existing alias set; no `wrangler` /
  `cloudflare-account` references anywhere in the repo
  (greenfield). `play-cloudflare-warp.yml` is unrelated (it's
  the Cloudflare WARP VPN client, not Workers).
- Plan 00035 sets the conventions this plan extends — same
  alias-keyed shape, same per-account audit-then-fail pattern,
  same template-driven bash function generation.
- Status held at ⬜ pending Phase 1 decision gate.

### 2026-05-26 — review feedback applied

- User rejected the compat-shim approach (Decision 2 rewritten):
  *"lets not have complicated shims/compat etc - KISS let the
  user handle upgrading the config as required"*. Plan now uses
  fail-fast schema detection in `play-github-cli-multi.yml` with
  a copy-pasteable migration block; the user is responsible for
  the one-line edit to `localhost.yml`. Phase 2 rewritten
  accordingly (no resolver playbook, no derivation logic, no QA
  divergence assertion).
- User confirmed wrangler is fundamentally unlike gh: no
  per-config-dir OAuth, needs explicit env-var injection per
  invocation (Decision 3). Phase 4 rewritten around the
  `CLOUDFLARE_API_TOKEN=… command wrangler "$@"` pattern.
- User asked for proposal on secure secret storage. Added
  Decision 4 comparing plaintext files / GNOME Keyring / pass /
  ansible-vault; recommended GNOME Keyring via `secret-tool`
  with a fallback to 0600 files if keyring isn't viable. Phase
  1 adds a research task to confirm `secret-tool` availability;
  Phase 5 setup script gains a `--set-token` mode that reads
  from stdin and stores via `secret-tool` without ever writing
  the token to disk.
- Updated tracking issue #22 to match the revised design.

### Candidate follow-up tools (noted for future plans, not in scope here)

- **npm** — multi-account publish (organisation-scoped tokens
  in `.npmrc`)
- **aws** — profile-based (`~/.aws/credentials` named profiles,
  `AWS_PROFILE` env var)
- **gcloud** — `gcloud config configurations` per identity
- **supabase** — per-project tokens in `~/.supabase`
- **fly.io** — per-org tokens (`flyctl auth token`)

Each of these has its own native multi-account mechanism; the
persona-resolver pattern wraps over the top to give all of them
the same `<tool>-<alias>` shell ergonomics.
