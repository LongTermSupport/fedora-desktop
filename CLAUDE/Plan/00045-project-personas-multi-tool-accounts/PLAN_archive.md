> Archived on 2026-09-02: superseded by the lean PLAN.md alongside. Kept verbatim as the historical record.

# Plan 00045: Project Personas — Unified Multi-Account Wrangling Across Tools

**Status**: Not Started (awaiting Phase 1 decision gate)
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
  (`project_personas` in `localhost.yml`) — flat platform-keyed
  schema using full platform names (`github`, `cloudflare`,
  `amazon_web_services`, …), no `tools:` wrapper, no CLI-binary
  abbreviations
- New platform support is **additive**: declare a platform key per
  persona, run the per-platform playbook, get the same
  `<cli>-<alias>` UX shape as `gh-<alias>` provides today
- **Six platforms wired up in this plan**:
  - **github** (existing — migrated from `github_accounts`)
  - **cloudflare** (new — `wrangler-<alias>`)
  - **fly_io** (new — `flyctl-<alias>`)
  - **supabase** (new — `supabase-<alias>`)
  - **amazon_web_services** (new — `aws-<alias>` via `AWS_PROFILE`)
  - **google_cloud_platform** (new — `gcloud-<alias>` via
    `CLOUDSDK_ACTIVE_CONFIG_NAME`)
- **Smart top-level `git` and `gh`** auto-route to the correct
  `<cli>-<alias>` based on the cwd git remote's org, looked up
  against each persona's `github.use_for_orgs` declaration.
  Per-alias wrappers stay as escape hatches; smart wrappers are
  the daily-driver path. GitHub-only for now (other platforms have
  no equivalent of "remote URL implies persona" signal).
- `play-github-cli-multi.yml` consumes `project_personas`
  directly; users on the legacy `github_accounts` schema get a
  **fail-fast error with the exact YAML to paste** to migrate.
  No silent shim, no two-source-of-truth confusion.
- Secrets for platforms that need per-invocation env-var injection
  (cloudflare, fly_io, supabase, future API-token platforms) are
  stored in the **GNOME Keyring** via `secret-tool`. Not in
  `localhost.yml`, not in plaintext on disk, not in shell history.
- Platforms that use config-file profiles (`amazon_web_services`,
  `google_cloud_platform`) get a thinner wrapper that sets the
  active-profile env var per call (no keyring lookup needed —
  credentials stay in the platform's native config file with its
  own permissions model).
- Per-persona setup script (`scripts/persona-setup.bash`) handles
  the full add-a-persona flow end-to-end across all in-scope
  platforms: edit `localhost.yml` → auth each declared platform →
  store any required secrets in keyring → verify
- Pattern is documented well enough that adding a **seventh**
  platform (e.g. npm_registry, microsoft_azure, docker_hub) is a
  single new playbook + platform field, not an architectural
  decision

## Non-Goals

- **Not implementing the following platforms in this plan**
  (deferred to follow-up plans that reuse this plan's architecture):
  - `npm_registry` — the publish-auth use case adds `.npmrc`
    per-scope handling that doesn't fit the env-var-per-call
    template cleanly. Easy add later.
  - `microsoft_azure` — only worth doing if the user actively uses
    Azure; the env-var pattern works but the playbook is non-trivial
    (multiple env var combos depending on auth mode).
  - `docker_hub` — single-active `~/.docker/config.json` is fiddly;
    either rewrite the config per call (race-condition-prone) or
    `docker --config <per-alias-dir>` (verbose, breaks `docker compose`
    UX). Worth its own design plan.
- **Not implementing smart auto-routing for non-GitHub platforms.**
  AWS/GCP/Cloudflare have no equivalent of "git remote URL → org",
  so smart routing would need a per-directory marker (`.persona`
  file, `direnv` `.envrc`, etc.). User has rejected the
  custom-file approach. If per-directory env defaults become
  desirable later, the right tool is `direnv` (already-installed
  upstream project, no invention needed) — see Decision 7.
- Not renaming or restructuring the existing
  `play-github-cli-multi.yml` machinery — it stays put, the schema
  detection at the top is the only change to its public contract.
- Not changing the SSH key naming convention
  (`~/.ssh/github_<alias>`) — out of scope, would touch Plan 00035's
  surface.
- Not building a GUI/wizard — CLI-only, consistent with `run.bash`.
- Not encrypting persona-level metadata beyond what's already
  encrypted in `localhost.yml` (vault behaviour unchanged).

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

Refined schema (see 2026-05-29 update at bottom for the design decisions behind this shape):

```yaml
project_personas:
  <alias-a>:
    name: "<Display Name A>"
    github:
      username: <gh-username-a>
      use_for_orgs:
        - <org-x>
        - <org-y>
    cloudflare:
      account_id: "a1b2c3d4e5f6..."
      account_name: "<Display Name A>"
  <alias-b>:
    name: "<Display Name B>"
    github:
      username: <gh-username-b>
      use_for_orgs:
        - <org-z>
    cloudflare:
      account_id: "0123456789ab..."
      account_name: "<Display Name B>"
  <alias-c>:
    name: "<Display Name C>"
    github:
      username: <gh-username-c>
      # no use_for_orgs — this persona owns no orgs, only its personal namespace
    # no cloudflare key — this persona has no Cloudflare account
```

**Shape rules:**

- Top-level key is the **alias** (matches existing `github_accounts` keys, so the SSH key path stays `~/.ssh/github_<alias>`).
- Each persona has a `name` field plus N **platform keys** at the persona top level — no `tools:` wrapper.
- Platform keys use the **full platform name**, not the CLI binary (`github` not `gh`, `cloudflare` not `wrangler`, `amazon_web_services` not `aws`, `google_cloud_platform` not `gcp`). A few extra tokens buys vastly more clarity for humans and LLMs reading the YAML.
- `github.use_for_orgs` (optional list of org names) declares which orgs this persona owns. Drives auto-routing in the smart `git` / `gh` wrappers (Decision 5). Absent or empty list → persona is reachable only via explicit `git-<alias>` / `gh-<alias>` or by personal-repo namespace match.
- Per-CLI-shaped sub-fields are fine *inside* the platform block where genuinely needed (e.g. `cloudflare.api_token_keyring_key` is wrangler-shaped). The platform key itself stays platform-named.

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

- [ ] ⬜ **Research GNOME Keyring + `secret-tool` for token
  storage** — confirm `secret-tool` is in F43 base or comes from
  a known package (libsecret), confirm it's already
  installed/usable on the daily-driver, confirm the lookup
  works at shell function call time without an interactive
  unlock prompt (the keyring is unlocked at GNOME login on
  this machine). Output: notes for Decision 4.
- [ ] ⬜ **Research env-var-per-call CLIs (cloudflare / fly_io /
  supabase)** — confirm each supports a pure env-var auth mode
  that overrides any active OAuth session:
  - `wrangler`: `CLOUDFLARE_API_TOKEN` + `CLOUDFLARE_ACCOUNT_ID`
  - `flyctl`: `FLY_API_TOKEN`
  - `supabase`: `SUPABASE_ACCESS_TOKEN`
    Capture minimum-scope token requirements per platform for use
    in the per-persona setup prompts. Output: short notes file in
    this plan dir.
- [ ] ⬜ **Research named-profile CLIs (amazon_web_services /
  google_cloud_platform)** — confirm the active-profile selection
  env var works per call without mutating the config file:
  - `aws`: `AWS_PROFILE=<alias>` selects a stanza from
    `~/.aws/credentials` / `~/.aws/config`
  - `gcloud`: `CLOUDSDK_ACTIVE_CONFIG_NAME=<alias>` selects a
    config from `gcloud config configurations list`
    Confirm both CLIs respect the env var even when the user has
    run `aws configure` / `gcloud config set ...` interactively.
    Output: notes file.
- [ ] ⬜ **Confirm in-scope platform list with user** — proposed
  set: `github`, `cloudflare`, `fly_io`, `supabase`,
  `amazon_web_services`, `google_cloud_platform`. Confirmed
  out-of-scope (deferred to follow-up plans): `npm_registry`,
  `microsoft_azure`, `docker_hub`. User can adjust the cut list
  before Phase 2 begins.
- [ ] ⬜ **Decision gate** — present Phase 1 research + the open
  decisions (Decision 1: schema shape — settled 2026-05-29;
  Decision 3: wrangler auth method — settled; Decision 4: secret
  storage — settled, awaiting `secret-tool` availability
  confirmation; Decision 5: smart router scope — settled
  github-only 2026-05-29; Decision 6: in-scope platform list;
  Decision 7: direnv-as-future-possibility for non-GitHub smart
  routing) to user. Block remaining phases on user approval.

### Phase 2: Schema definition + gh playbook fail-fast migration

- [ ] ⬜ **Define `project_personas` schema** in a comment block
  at the top of `localhost.yml`. Documents required vs optional
  keys per platform with worked examples for all six in-scope
  platforms. Explicitly states the three shape rules: flat
  platform keys at persona top level, full platform names (no
  CLI binaries / abbreviations), `github.use_for_orgs` for org
  ownership.
- [ ] ⬜ **Populate `project_personas`** in `localhost.yml` for
  the existing aliases currently in `localhost.yml`'s
  `github_accounts`, initially with only the `github` platform
  entry — usernames mirror today's `github_accounts` exactly.
  `use_for_orgs` populated based on which aliases own which orgs
  (user decides — playbook can warn but cannot infer).
- [ ] ⬜ **Remove the legacy `github_accounts` block** from
  `localhost.yml` once `project_personas` is populated. Single
  source of truth from the user's side — no two YAML blocks
  declaring the same thing.
- [ ] ⬜ **Update `play-github-cli-multi.yml` schema detection**
  — at the top of the play, add a preflight that:
  1. If `project_personas is defined and project_personas | length > 0` → derive a local `_gh_accounts` fact from it
     (`{alias: p.github.username for alias, p in project_personas.items() if p.github is defined}`)
     and use it throughout the rest of the play.
  2. Else if legacy `github_accounts is defined` → **fail
     fast** with a printable YAML snippet showing the exact
     `project_personas` block the user should paste to
     migrate, plus the one-line instruction to delete the
     `github_accounts` block.
  3. Else → existing "no accounts configured" skip path.
- [ ] ⬜ **Add `use_for_orgs` collision check** to the gh
  playbook preflight — iterate
  `project_personas[*].github.use_for_orgs`, fail-fast at
  playbook-time if any org appears under two personas.
  Catches the misconfiguration before the smart router can act
  on it.
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
  byte-identical to before the migration for the same accounts.
  (`diff` the two outputs.) `use_for_orgs` adds new content but
  does not alter the existing `gh-<alias>` / `git-<alias>`
  generation.

### Phase 3: Platform CLI base installs

One install playbook per CLI binary, idempotent, pinned where
versions matter for reproducibility. These are independent of the
multi-account machinery — they just put the binary on `$PATH`.

- [ ] ⬜ **`playbooks/imports/optional/common/play-wrangler.yml`** —
  installs `wrangler` globally via npm under the user's nvm context
  (follows `play-nvm-install.yml` precedent), pinned version.
- [ ] ⬜ **`playbooks/imports/optional/common/play-flyctl.yml`** —
  installs `flyctl` via the upstream installer script
  (`curl -L https://fly.io/install.sh`) with `creates:` guard for
  idempotency. Pinned version via the installer's
  `FLYCTL_INSTALL_VERSION` env var.
- [ ] ⬜ **`playbooks/imports/optional/common/play-supabase-cli.yml`** —
  installs `supabase` CLI via the upstream tarball release
  (follows `yq` precedent). Pinned version.
- [ ] ⬜ **`playbooks/imports/optional/common/play-aws-cli.yml`** —
  installs AWS CLI v2 via the upstream bundle (NOT pip — the bundle
  is the upstream-recommended path on Linux). Pinned version.
- [ ] ⬜ **`playbooks/imports/optional/common/play-gcloud.yml`** —
  installs `gcloud` via the Google Cloud SDK repo + dnf (Fedora
  package available since F39). Pinned version.
- [ ] ⬜ **Add each new install playbook to `playbook-main.yml`** as
  opt-in imports (commented out by default, matches `play-ddev.yml`
  precedent — user uncomments per platform they actually use).
- [ ] ⬜ **Verify installs** with `wrangler --version`,
  `flyctl version`, `supabase --version`, `aws --version`,
  `gcloud --version` on host.

### Phase 4: Per-platform multi-account playbooks

All six platforms share a common template shape — preflight, read
`project_personas`, filter by platform key declared, generate a
per-alias bash function include. The differences are confined to:
(a) the env-var set the function exports, (b) the keyring
attribute scheme, (c) the health-check API call.

**4.0 — Common template extraction** (do first, then instantiate)

- [ ] ⬜ **Extract a Jinja macro / include task** that takes
  `(platform_name, cli_binary, env_var_map, keyring_attr, health_call)`
  parameters and emits a per-alias bash function. Keeps the six
  platform playbooks DRY. Lives in
  `playbooks/imports/_shared/persona-bash-function-template.j2`
  (or similar — exact path settled in implementation).

**4.1 — `play-cloudflare-multi.yml`** (env-var-per-call, keyring-stored token)

- [ ] ⬜ **Preflight**: `secret-tool` on PATH; wrangler installed.

- [ ] ⬜ **Filter**: personas with `cloudflare` key declared.

- [ ] ⬜ **Token presence check**: for each, assert
  `secret-tool lookup persona <alias> platform cloudflare attr api_token`
  resolves. Fail-fast with `persona-setup.bash --set-token=<alias> cloudflare` if missing.

- [ ] ⬜ **Generate `~/.bashrc-includes/cloudflare-aliases.inc.bash`**
  with per-alias `wrangler-<alias>` functions:

  ```bash
  function wrangler-<alias>() {
      local token
      if ! token=$(secret-tool lookup persona <alias> platform cloudflare attr api_token 2>/dev/null); then
          echo "ERROR: no cloudflare API token stored for persona '<alias>'." >&2
          echo "Run: persona-setup.bash --set-token=<alias> cloudflare" >&2
          return 1
      fi
      CLOUDFLARE_API_TOKEN="$token" \
      CLOUDFLARE_ACCOUNT_ID="<account_id from project_personas>" \
          command wrangler "$@"
  }
  ```

- [ ] ⬜ **Helper functions** in the same include:
  `cloudflare-list`, `cloudflare-whoami-<alias>`, `cloudflare-status`.

- [ ] ⬜ **Per-account health audit**: `wrangler whoami` under each
  persona's env vars. Fail-fast with exact remediation per persona.

**4.2 — `play-fly-io-multi.yml`** (env-var-per-call, keyring-stored token)

- [ ] ⬜ Same shape as 4.1 but env vars are
  `FLY_API_TOKEN="$token"` only (fly_io has no equivalent of
  account-id). Per-alias function: `flyctl-<alias>`.
- [ ] ⬜ Keyring scheme: `persona=<alias> platform=fly_io attr=api_token`.
- [ ] ⬜ Health audit: `flyctl auth whoami`.

**4.3 — `play-supabase-multi.yml`** (env-var-per-call, keyring-stored token)

- [ ] ⬜ Same shape. Env var: `SUPABASE_ACCESS_TOKEN="$token"`.
  Per-alias function: `supabase-<alias>`.
- [ ] ⬜ Keyring scheme: `persona=<alias> platform=supabase attr=access_token`.
- [ ] ⬜ Health audit: `supabase projects list` (or the cheapest
  read-only call available — confirm in Phase 1 research).

**4.4 — `play-amazon-web-services-multi.yml`** (named-profile pattern, no keyring)

- [ ] ⬜ **Preflight**: AWS CLI installed. NO `secret-tool` check
  — this platform stores credentials in `~/.aws/credentials` per
  the AWS-native model.

- [ ] ⬜ **Filter**: personas with `amazon_web_services` declared.

- [ ] ⬜ **Profile presence check**: for each, assert a stanza named
  `[<alias>]` exists in `~/.aws/credentials` (or `~/.aws/config`
  for SSO). Fail-fast with `persona-setup.bash --aws-configure=<alias>`
  if missing — setup script delegates to interactive `aws configure --profile=<alias>`.

- [ ] ⬜ **Generate `~/.bashrc-includes/amazon-web-services-aliases.inc.bash`**:

  ```bash
  function aws-<alias>() {
      AWS_PROFILE=<alias> command aws "$@"
  }
  ```

  No keyring lookup needed — `aws` itself reads the profile from
  its native config. The wrapper just sets the active profile.

- [ ] ⬜ **Helper functions**: `aws-list`, `aws-whoami-<alias>`
  (`AWS_PROFILE=<alias> aws sts get-caller-identity`), `aws-status`.

- [ ] ⬜ **Per-account health audit**: `aws sts get-caller-identity`
  per profile.

**4.5 — `play-google-cloud-platform-multi.yml`** (named-config pattern, no keyring)

- [ ] ⬜ Same shape as 4.4 but using gcloud's
  `gcloud config configurations` model. Env var:
  `CLOUDSDK_ACTIVE_CONFIG_NAME=<alias>`. Per-alias function:
  `gcloud-<alias>`.
- [ ] ⬜ Preflight: gcloud CLI installed. Assert a configuration
  named `<alias>` exists in `gcloud config configurations list`.
  Fail-fast with `persona-setup.bash --gcloud-init=<alias>` if
  missing.
- [ ] ⬜ Health audit: `gcloud auth list --filter=status:ACTIVE`
  under the persona's env var.

### Phase 4.5: Smart top-level `git` and `gh` (GitHub only)

- [ ] ⬜ **Add `_persona_extract_org_from_cwd_remote` helper bash
  function** to a shared include
  (`~/.bashrc-includes/persona-router.inc.bash`). Logic: run
  `command git remote get-url origin 2>/dev/null` → extract the
  GitHub owner with a single regex (handles both
  `git@github.com:<owner>/<repo>.git` and
  `https://github.com/<owner>/<repo>.git` formats) → print owner
  or nothing.

- [ ] ⬜ **Add `_persona_lookup_alias_for_org` helper** —
  takes `(platform_name, org_name)`, reads a generated lookup
  file written by `play-github-cli-multi.yml` (a simple
  `<org>=<alias>` text file at
  `~/.config/persona-router/github-orgs.map`), prints the
  matching alias or nothing. Pure file I/O, no parsing on every
  call.

- [ ] ⬜ **Generate `github-orgs.map`** in
  `play-github-cli-multi.yml`. Iterate
  `project_personas[*].github.use_for_orgs`, emit one
  `<org>=<alias>` line per entry. Fail-fast at playbook-time if
  any org appears twice (the collision check from Phase 2).

- [ ] ⬜ **Implement smart `git()` and `gh()` functions** in the
  same include:

  ```bash
  function git() {
      local org alias
      org=$(_persona_extract_org_from_cwd_remote)
      if [ -n "$org" ]; then
          alias=$(_persona_lookup_alias_for_org github "$org")
          if [ -n "$alias" ]; then
              command git-"$alias" "$@"
              return
          fi
      fi
      command git "$@"
  }
  function gh() {
      # detect org from cwd remote OR from `gh` argv positional
      # (e.g. `gh repo view <org>/<repo>`, `gh issue list --repo <org>/<repo>`)
      # → delegate to gh-<alias>
      # No match → command gh "$@"
  }
  ```

- [ ] ⬜ **Add `persona-here` diagnostic** to the same include —
  prints which persona would be used for the cwd's remote.
  Debugs "wrong user pushed".

- [ ] ⬜ **Verify smart routing** on host with at least two
  GitHub personas owning different orgs:

  - `cd ~/Projects/<org-a>/repo && git remote -v` → smart `git`
    delegates to `git-<alias-a>`
  - `cd ~/Projects/<org-z>/repo && git remote -v` → smart `git`
    delegates to `git-<alias-b>`
  - `cd ~/Projects/unclaimed-org/repo && git remote -v` → smart
    `git` passes through to default `command git`

### Phase 5: Per-persona setup script (multi-platform dispatcher)

- [ ] ⬜ **Create `scripts/persona-setup.bash`** as the top-level
  dispatcher. Each platform's setup logic is a separate
  sub-function (`_setup_github`, `_setup_cloudflare`,
  `_setup_fly_io`, `_setup_supabase`, `_setup_amazon_web_services`,
  `_setup_google_cloud_platform`) — avoids one god-script,
  enforces consistent UX per platform.
- [ ] ⬜ **Implement `--add=<alias>` mode** — interactive
  walk-through: prompt for display name, then for each in-scope
  platform prompt "set up for <platform>? y/n", delegate to the
  per-platform setup flow if yes, append the persona block to
  `localhost.yml`.
- [ ] ⬜ **Implement `--set-token=<alias> <platform>` mode** —
  for keyring-stored platforms (`cloudflare`, `fly_io`,
  `supabase`). Reads a token from stdin (or prompts with
  `read -s`), validates with one read-only API call, stores via
  `secret-tool store --label="<platform> token for <alias>" persona <alias> platform <platform> attr <attr-name>`.
  Never echoes, never writes to disk, never accepts the token
  as an argv parameter (rejects `$1` looking like a token).
- [ ] ⬜ **Implement `--aws-configure=<alias>` mode** — delegates
  to `aws configure --profile=<alias>` for the interactive AWS
  credential entry (AWS native path; no keyring involved).
- [ ] ⬜ **Implement `--gcloud-init=<alias>` mode** — delegates
  to `gcloud config configurations create <alias>` followed by
  the standard interactive init, producing a configuration named
  `<alias>` in `gcloud config configurations list`.
- [ ] ⬜ **Implement `--setup-all` mode** — iterates
  `project_personas`, for each persona × platform combo runs the
  platform's setup flow if not already complete. Idempotent —
  re-running with everything in place is a no-op with success
  logs.
- [ ] ⬜ **Implement `--check` mode** — read-only health
  verification across all personas × platforms. Reports a per-cell
  status table (`<alias-a>/github: OK`,
  `<alias-a>/cloudflare: MISSING TOKEN`,
  `<alias-b>/amazon_web_services: PROFILE NOT FOUND`, etc.).
  No prompts, no writes.
- [ ] ⬜ **Hook into `run.bash`** — `persona-setup.bash --setup-all`
  runs as part of the fresh-install flow, replacing the current
  `gh-account-setup.bash --setup-all` call site. Pre-existing gh
  setup logic is retained internally as `_setup_github`.

### Phase 6: Docs

- [ ] ⬜ **New doc `docs/project-personas.md`** covering: the
  schema (with worked examples for all six in-scope platforms),
  the smart `git`/`gh` routing model with `use_for_orgs`, the
  keyring-based token storage model, and the
  named-profile-pattern (AWS/GCP) as the alternative auth shape.
- [ ] ⬜ **Migration sub-section** within
  `docs/project-personas.md`: the YAML shape change from
  `github_accounts` to `project_personas`, the fail-fast message
  users see on first run, the one-line YAML edit that resolves it.
- [ ] ⬜ **Add to `docs/README.md` index** under a new
  "Multi-account tooling" section linking to the new doc.
- [ ] ⬜ **Update `CLAUDE/AnsibleStyle.md`** with the persona-reading
  convention: "playbooks that consume `project_personas` must
  filter by the platform key (e.g. `p.github is defined`), use
  the full platform name (no CLI binaries), and fail-fast on
  the legacy `github_accounts` schema".
- [ ] ⬜ **Document the "add a seventh platform" recipe** — a
  6-step checklist for the next contributor adding `npm_registry`
  / `microsoft_azure` / `docker_hub` / etc., covering: (1) full
  platform name conventions (e.g. `microsoft_azure` not `azure`),
  (2) which template shape applies (env-var-per-call vs
  named-profile vs single-active-config), (3) the new
  `play-<platform>-multi.yml`, (4) bash function template
  invocation, (5) keyring attribute scheme `persona=<alias> platform=<platform> attr=<…>`, (6) `persona-setup.bash`
  sub-function entry.
- [ ] ⬜ **Document direnv as the future option** for per-directory
  smart routing on non-GitHub platforms (see Decision 7) — link
  to upstream `direnv` docs, note this repo doesn't ship
  `direnv` integration but the schema supports it cleanly
  (users can write `.envrc` files that set
  `CLOUDFLARE_API_TOKEN=$(secret-tool lookup ...)` etc.).

### Phase 7: QA & validation

- [ ] ⬜ `./scripts/qa-all.bash` passes for all changed
  bash/ansible files.
- [ ] ⬜ `ansible-playbook --syntax-check` passes for all
  new/modified playbooks.
- [ ] ⬜ **Verify gh fail-fast path** on host — temporarily restore
  the legacy `github_accounts` block, run the playbook, confirm
  the error message contains a copy-pasteable migration YAML
  including `use_for_orgs` placeholders.
- [ ] ⬜ **Verify `use_for_orgs` collision detection** — declare
  the same org under two personas, run the playbook, expect
  fail-fast naming the conflicting alias names.
- [ ] ⬜ **Verify gh byte-identical output** — deploy on host
  post-migration, `diff` `gh-aliases.inc.bash` against
  pre-migration output for the same accounts.
- [ ] ⬜ **Verify smart router** with at least two GitHub personas
  on different orgs:
  - `cd ~/Projects/<org-a>/repo && git remote -v` → smart `git`
    delegates correctly; verify with `persona-here`.
  - `gh repo view <org-a>/repo` → smart `gh` delegates correctly.
  - `cd /tmp && git init && git config user.email` → smart `git`
    passes through (no remote = no routing).
- [ ] ⬜ **Verify env-var-per-call platforms** (cloudflare /
  fly_io / supabase) work for at least two personas each with
  real accounts; confirm via `strace`/`/proc/<pid>/environ` that
  tokens only appear in the CLI subprocess env, never the parent
  shell.
- [ ] ⬜ **Verify named-profile platforms** (aws / gcloud) work
  per-alias — `aws-<alias> sts get-caller-identity` and
  `gcloud-<alias> auth list` return different identities for
  different aliases.
- [ ] ⬜ **Verify `--check` mode catches deliberately-broken
  personas** — revoke a cloudflare token, expect clear failure;
  delete an aws profile, expect clear failure; verify the table
  output shows pass/fail per persona × platform cell.
- [ ] ⬜ **Verify keyring lookup latency** is acceptable (one
  `secret-tool lookup` per env-var-per-call invocation — should
  be \<10ms; if not, cache for the shell session).

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

### Decision 1: Schema shape — flat platform keys, full platform names

**Context**: Three sub-decisions on the YAML shape, resolved together on 2026-05-29.

**1a. Alias-keyed vs tool-keyed top level**

Options:

1. Alias-keyed map (`<alias>: { … per-platform … }`)
2. Tool-keyed map (`github: { <alias>: … }, cloudflare: { <alias>: … }`)

**Decided**: Option 1. Keeps each persona's full declaration in one place, mirrors how `github_accounts` is structured today (alias is primary key), reads naturally as "persona X owns these platform identities".

**1b. With `tools:` wrapper vs flat platform keys**

Options:

1. `<alias>: { name, tools: { github: …, cloudflare: … } }`
2. `<alias>: { name, github: …, cloudflare: … }` (flat)

**Decided**: Option 2 (flat). The `tools:` wrapper is YAGNI nesting — platform names don't collide with metadata fields (`name`, future `email`, future `default`), so the playbook can enumerate platforms by treating any persona key not in a small known-metadata list as a platform. One fewer level of indirection in every YAML edit, every Jinja expression, and every diff hunk.

**1c. Platform key naming — CLI binary vs platform name vs full platform name**

Options:

1. CLI binary name: `gh`, `wrangler`, `aws`, `gcloud`
2. Short platform name: `github`, `cloudflare`, `aws`, `gcp`
3. Full platform name: `github`, `cloudflare`, `amazon_web_services`, `google_cloud_platform`

**Decided**: Option 3 (full platform name).

- Option 1 fractures the identity: `gh` and `git` and `clone-<alias>` all use one identity but keying by `gh` makes it look CLI-specific.
- Option 2 is jargon-y: `aws` reads as "AWS the platform / the CLI / a region / an account" depending on context. Same for `gcp`, `azure`.
- Option 3 (`amazon_web_services`, `google_cloud_platform`, `microsoft_azure`) is unambiguous one-glance. Per the user (2026-05-29): *"a few tokens = much greater clarity for humans + LLMs"*.

Examples of correct keys: `github`, `cloudflare`, `amazon_web_services`, `google_cloud_platform`, `microsoft_azure`, `npm_registry`, `docker_hub`, `fly_io`. `github` and `cloudflare` are already the platforms' full marketing/legal names, so no expansion needed.

**Status**: ✅ Decided 2026-05-29.

### Decision 5: Smart top-level `git` and `gh` with `use_for_orgs` auto-routing

**Context**: Once `use_for_orgs` declares which persona owns which GitHub org, the per-alias wrappers (`git-<alias>`, `gh-<alias>`) can stop being the primary interface. The default `git` and `gh` commands the user types thousands of times a day become smart functions that auto-route to the correct sub-alias based on the cwd's git remote.

**Decided**: Wrap `git` and `gh` as shell functions that:

1. Detect the relevant org from context (cwd's `origin` remote URL for `git`; cwd remote OR `gh` argv positional like `gh repo view <org>/<repo>` for `gh`).
2. Look up `project_personas[*].github.use_for_orgs` for a match.
3. If a single match found → delegate to `git-<alias>` / `gh-<alias>`.
4. If no match → pass through to the real `command git` / `command gh` (current default-active gh account; no surprise routing).
5. If multiple matches → fail-fast at function-call time naming the collision; user fixes `use_for_orgs` lists.

The per-alias wrappers (`git-<alias>`, `gh-<alias>`, `clone-<alias>`) stay as **escape hatches** for forcing a specific persona when smart routing would pick a different one (e.g. when intentionally operating across personas, or when in a directory outside any git repo).

```bash
function git() {
    local org alias
    org=$(_persona_extract_org_from_cwd_remote)
    if [ -n "$org" ]; then
        alias=$(_persona_lookup_alias_for_org github "$org")
        if [ -n "$alias" ]; then
            command git-"$alias" "$@"
            return
        fi
    fi
    command git "$@"
}
```

**Why**: removes the cognitive load of "did I call `git-foo` or `git-bar`?" — the answer is determined declaratively by `use_for_orgs`. Misrouting incidents (commits as the wrong identity) become impossible for any org the user has declared. Repos in unclaimed orgs fall through to default behaviour, preserving the existing escape hatch.

**Edge cases**:

- **Personal repos** (`<gh-username>/*`): the persona's `github.username` already declares ownership of that namespace, no `use_for_orgs` entry needed. Smart router falls back to username matching after org matching.
- **Org-list collisions**: two personas claiming the same org → fail-fast at **playbook deployment time** (preferred — catches the bug before it can misroute) AND at function-call time (defence in depth, in case `localhost.yml` is edited without re-running the playbook).
- **No git remote in cwd**: smart `git` passes through unchanged (e.g. `git init`, `git config --global`).
- **`gh` calls without a repo argument**: e.g. `gh auth status`, `gh api user`. Fall through to `command gh`.

**Date**: 2026-05-29 (added after the parent decisions were settled — pure additive feature, doesn't change Phase 2-4 work, adds a new Phase 4.5 between wrangler and persona-setup script).

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

### Decision 6: In-scope platform set for this plan

**Context**: Original plan covered only `github` + `cloudflare`. User asked (2026-05-29) to expand to "most common ones in one go" rather than fragmenting into N follow-up plans.

**Considered platforms**, grouped by auth pattern:

| Platform                | Pattern                        | Decision |
| ----------------------- | ------------------------------ | -------- |
| `github`                | OAuth + SSH (native multi)     | ✅ IN    |
| `cloudflare`            | API token, env-var-per-call    | ✅ IN    |
| `fly_io`                | API token, env-var-per-call    | ✅ IN    |
| `supabase`              | Access token, env-var-per-call | ✅ IN    |
| `amazon_web_services`   | Named profile + `AWS_PROFILE`  | ✅ IN    |
| `google_cloud_platform` | Named config + env var         | ✅ IN    |
| `npm_registry`          | `.npmrc` per-scope + token     | ⬜ DEFER |
| `microsoft_azure`       | Multiple auth modes            | ⬜ DEFER |
| `docker_hub`            | Single-active config file      | ⬜ DEFER |

**Decided**: Six platforms in scope. The token-env-var-per-call platforms (cloudflare, fly_io, supabase) and named-profile platforms (AWS, GCP) all fit the Phase 4 template architecture without new design — adding them in this plan costs the platform-specific config plus one new playbook each, not architectural work.

**Why defer the bottom three**:

- `npm_registry`: publish auth lives in `.npmrc` files which can be project-scoped (`.npmrc` in project root) or registry-scoped (`@scope:registry=...`). The single env-var-per-call template doesn't fit cleanly. Easy to add later as a follow-up plan once the user actually needs it.
- `microsoft_azure`: only worth doing if the user actually uses Azure. The `az` CLI supports multiple auth modes (device code, service principal, managed identity) and the env-var combos differ per mode. Non-trivial playbook.
- `docker_hub`: `~/.docker/config.json` is single-active. Per-call switching requires either rewriting the config file (race-condition-prone with parallel `docker` invocations) or `docker --config <per-alias-dir>` (verbose, breaks `docker compose` UX). Worth its own design plan.

**Migration friendliness**: all six in-scope platforms can be added to `localhost.yml` via the same fail-fast message format — the playbook prints copy-pasteable YAML for whichever platforms the user has aliases declared for in any deprecated `<platform>_accounts`-style legacy block (only `github_accounts` exists today; others are greenfield).

**Date**: 2026-05-29

### Decision 7: Smart routing scope — GitHub only; direnv as future option for other platforms

**Context**: Decision 5 introduces smart top-level `git` / `gh` wrappers that auto-route based on `use_for_orgs`. Other platforms (cloudflare, fly_io, supabase, AWS, GCP) have no equivalent "cwd-implies-persona" signal — there's no `wrangler remote get-url` or "AWS account ID embedded in the cwd". User considered (2026-05-29) whether to invent a per-directory marker file (`.persona` or similar) and **rejected the custom-file approach**: *"no random files"*.

**Decided**: Smart routing is **GitHub-only** in this plan. For non-GitHub platforms, the user explicitly invokes the per-alias wrapper (`wrangler-<alias>`, `aws-<alias>`, etc.) when working with that persona.

**Future option (shelved, not implemented)**: if per-directory defaults become desirable for non-GitHub platforms later, the right tool is **`direnv`** (already a widely-used upstream project, no invention needed). Users would write `.envrc` files in project directories:

```bash
# Example .envrc in a Cloudflare Worker project directory
export CLOUDFLARE_API_TOKEN=$(secret-tool lookup persona <alias> platform cloudflare attr api_token)
export CLOUDFLARE_ACCOUNT_ID=<alias>-account-id
```

`direnv` auto-loads `.envrc` on cwd change (with user approval gate via `direnv allow`) and unloads on leaving. This gives the same per-cwd routing behaviour as smart `git`/`gh` but without us inventing a file format or shipping integration code.

**Why direnv-shelf-not-implement**:

- It's a real dependency to install, configure, and document.
- It's not needed today — explicit per-alias wrappers are fine for cloudflare/fly/aws/gcp.
- The `project_personas` schema is already compatible with direnv (just expose helpers users can call from their `.envrc`); we don't need to design the schema around direnv.

**Date**: 2026-05-29

## Success Criteria

- [ ] `project_personas` is the **only** identity map in
  `localhost.yml` after migration; the legacy `github_accounts`
  block is removed.
- [ ] Schema follows the three settled rules: flat platform keys
  at persona top level (no `tools:` wrapper), full platform names
  (`github`, `cloudflare`, `amazon_web_services` etc., never
  `gh` / `wrangler` / `aws`), `github.use_for_orgs` declarations
  for any persona that owns GitHub orgs.
- [ ] `play-github-cli-multi.yml` run before migration fail-fasts
  with a copy-pasteable migration block; run after migration
  produces byte-identical `gh-aliases.inc.bash` to the
  pre-migration output (no regression in the gh UX).
- [ ] `use_for_orgs` collision check at playbook deploy time:
  declaring the same org under two personas fail-fasts with both
  alias names in the error message.
- [ ] **Smart `git` / `gh`** auto-route to the correct
  `git-<alias>` / `gh-<alias>` based on cwd remote-org or `gh`
  argv positional, for any org declared in any persona's
  `use_for_orgs`. Unclaimed orgs pass through to default
  behaviour. Verified on host with at least two personas.
- [ ] **`persona-here` diagnostic** prints the correct persona for
  the cwd's remote (and "(default)" when no match).
- [ ] **Env-var-per-call platforms** — `wrangler-<alias>`,
  `flyctl-<alias>`, `supabase-<alias>` bash functions work for at
  least two personas each with real accounts on the host, no
  stateful logins required, tokens fetched from the GNOME Keyring
  at call time (never read from disk in plaintext after
  initial `secret-tool store`).
- [ ] **Named-profile platforms** — `aws-<alias>` and
  `gcloud-<alias>` correctly select the named profile/configuration
  per call; `aws-<alias> sts get-caller-identity` and
  `gcloud-<alias> auth list` return different identities for
  different aliases.
- [ ] `persona-setup.bash --check` produces a clear pass/fail
  report for every persona × platform combination, and
  `--set-token` correctly stores and validates a token without
  ever writing it to disk in plaintext.
- [ ] **Adding a seventh platform** (e.g. `npm_registry` in a
  follow-up plan) requires only: a new `play-<platform>-multi.yml`
  - a new `<platform>` key per persona that needs it + (if
    secret-bearing) a per-platform sub-function in
    `persona-setup.bash`. No architectural changes.
- [ ] `./scripts/qa-all.bash` passes.

## Risks & Mitigations

| Risk                                                                                                       | Impact                                                                                                                                                                     | Mitigation                                                                                                                                                                                                                                    |
| ---------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Wrangler has no real per-account isolation — only one active OAuth at a time                               | High — confirms the API-token + env-var route                                                                                                                              | Phase 1 research confirms up front; API tokens with per-persona keyring entries is the chosen path, well-trodden by `gh` itself                                                                                                               |
| `secret-tool` / GNOME Keyring unavailable or locked at function call time                                  | High — `wrangler-<alias>` would fail at call time                                                                                                                          | Phase 1 confirms `secret-tool` is installed and the keyring is unlocked at GNOME login on the daily-driver; Decision 4 fallback to 0600 files if not viable                                                                                   |
| Token leak via shell history if user accidentally pastes it on the command line                            | High                                                                                                                                                                       | `persona-setup.bash --set-token` reads from stdin (or `read -s`), never accepts the token as an argv parameter; documented in `--help` and refused if `$1` looks like a token                                                                 |
| Token leak via env var visibility in `ps -e` / `/proc/<pid>/environ`                                       | Low — `/proc/<pid>/environ` is mode `0400` and owned by the process user, so only the same user or root can read it. This is the same threat surface as today's gh tokens. | Use `command wrangler` (not `env wrangler` or `KEY=val command`) so env vars live only on the wrangler subprocess, never exported into the parent shell                                                                                       |
| Persona schema accretes per-platform flags ad-hoc, becomes a kitchen sink                                  | Medium long-term                                                                                                                                                           | Phase 6 "add a seventh platform" recipe enforces a consistent shape: each new platform gets a documented `<platform>` schema entry (full platform name), template variant choice, and reviewers reject ad-hoc fields without playbook support |
| User on legacy `github_accounts` ignores the fail-fast message and downgrades the playbook to make it work | Low                                                                                                                                                                        | Fail-fast message includes the rationale ("two sources of truth would silently diverge"); migration is one block edit, not a refactor                                                                                                         |
| User has only one Cloudflare account today; building multi-account is YAGNI                                | Low — the schema generalises regardless                                                                                                                                    | Even with one account, the abstraction lets npm/aws/etc. land cheaply later. Phase 1 user confirms before commit                                                                                                                              |

## Timeline

- **Phase 1**: Research & decision gate (foundational, blocks all
  later phases). Now spans six platforms — expect more research
  hours than the original gh+wrangler scope but each is a quick
  upstream-docs read.
- **Phase 2**: Schema definition + gh playbook fail-fast migration
  - `use_for_orgs` collision check (must precede any per-platform
    playbook so the schema is settled).
- **Phase 3**: Platform CLI base installs (wrangler, flyctl,
  supabase, aws, gcloud). Six small playbooks, independent of the
  multi-account machinery.
- **Phase 4**: Per-platform multi-account playbooks. Common
  template extraction first (4.0), then five platform
  instantiations (4.1–4.5).
- **Phase 4.5**: Smart top-level `git` / `gh` routing — depends
  on Phase 2's `use_for_orgs` collision check + the
  `github-orgs.map` lookup file generation.
- **Phase 5**: Per-persona setup script. Multi-platform dispatcher;
  can start once Phase 2 and Phase 4.0 (template) are settled.
- **Phase 6**: Docs (concurrent with Phase 5).
- **Phase 7**: QA & validation (gates plan completion).

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

### 2026-05-29 — schema + smart router refinements, scope expansion

Three rounds of refinement feedback from the user, settled in one session:

**1. Schema rules locked in (Decision 1 rewritten as 1a/1b/1c)**:

- **Flat platform keys at persona top level** — the original draft nested platforms under `tools:`. User pointed out the `tools:` wrapper is YAGNI nesting: platform names don't collide with metadata fields. Removed.
- **Platform-name keys, not CLI-binary keys** — the original draft used `tools.gh` and `tools.wrangler`. User pointed out these are CLI binaries, not platform identities. Renamed to `github` / `cloudflare`.
- **Full platform names, not abbreviations** — user: *"a few tokens = much greater clarity for humans + LLMs"*. So `amazon_web_services` not `aws`, `google_cloud_platform` not `gcp`, `microsoft_azure` not `azure`. `github` and `cloudflare` are already full names.

**2. `use_for_orgs` + smart top-level `git`/`gh` (Decisions 5 and 7)**:

- New field `github.use_for_orgs: [<org>, …]` per persona declares org ownership.
- Smart `git` and `gh` shell functions auto-route to `git-<alias>` / `gh-<alias>` based on cwd remote-org match. Per-alias wrappers stay as escape hatches.
- User: *"agree stick to only git having smart capabilities as its cheap and fast to get remote + parse org"*. Smart routing is **GitHub-only** in this plan — other platforms have no cwd→persona signal.
- User: *"no random files, we could explore direnv if we wanted per dev defaults"*. Custom per-directory marker files (`.persona`, etc.) rejected. **`direnv` shelved as the future option** if non-GitHub per-cwd routing becomes desirable. Documented in Decision 7 and the Phase 6 docs task.

**3. Scope expansion (Decision 6)**:

- User: *"if we're going to do this, lets wire in tools for common platforms like this, i guess we may as well do them all in one go? at least most common ones"*.
- Original plan: `github` + `cloudflare` only. New scope: **six platforms** — `github`, `cloudflare`, `fly_io`, `supabase`, `amazon_web_services`, `google_cloud_platform`.
- Token-env-var-per-call platforms (cloudflare, fly_io, supabase) all reuse the Phase 4 template architecture as-is.
- Named-profile platforms (AWS, GCP) introduce a thinner variant of the template — no keyring, env var sets the active profile, credentials live in the CLI's native config file.
- Phases 3 + 4 + 5 restructured around this: Phase 3 grows from 1 install to 5 installs; Phase 4 grows from 1 playbook to 6 (1 common template + 5 platform instantiations); Phase 5 grows the setup-script dispatcher to cover all platforms; Phase 7 QA expanded to verify every platform end-to-end.
- Still deferred (each is its own follow-up plan when actually needed): `npm_registry` (`.npmrc`-per-scope quirk), `microsoft_azure` (multiple auth modes), `docker_hub` (single-active config awkwardness).

**4. Issue + plan kept in sync**:

- Updated tracking issue #22 with the de-nesting, full platform names, `use_for_orgs`, smart router, and scope expansion all in one comprehensive comment.
- This plan rewritten to match: Phases 1, 2, 3, 4, 4.5 (new), 5, 6, 7 all reflect the expanded scope; Decisions 1, 5, 6, 7 added/rewritten; Success Criteria expanded to cover every platform + smart router.

### Still-deferred platforms (for future plans, not in scope here)

After Decision 6, only three platforms remain deferred — each with a specific reason that needs its own design work:

- **`npm_registry`** — multi-account publish auth lives in `.npmrc` files, which can be project-scoped (`.npmrc` in project root) or registry-scoped (`@scope:registry=...`). The env-var-per-call template doesn't fit cleanly. Easy to add later as a follow-up plan that introduces a third template variant (file-template-per-cwd).
- **`microsoft_azure`** — only worth doing if the user actively uses Azure. The `az` CLI supports multiple auth modes (device code, service principal, managed identity, federated credential) and the env-var combos differ per mode. Non-trivial playbook; defer until the user has a concrete use case.
- **`docker_hub`** — `~/.docker/config.json` is single-active. Per-call switching requires either rewriting the config file per invocation (race-condition-prone with parallel `docker` invocations) or `docker --config <per-alias-dir>` (verbose, breaks `docker compose` UX). Worth its own design plan that wrestles with that trade-off.

Each of these has its own native multi-account mechanism; the `project_personas` schema is forward-compatible — the user can add `npm_registry: { …fields… }` blocks today and the schema will accept them; the playbook just won't process them until a follow-up plan adds the per-platform handler.
