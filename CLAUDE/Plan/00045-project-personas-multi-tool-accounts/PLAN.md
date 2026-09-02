# Plan 00045: Project Personas — Unified Multi-Account Wrangling Across Tools

**Status**: Not Started (awaiting Phase 1 decision gate)
**Created**: 2026-05-26
**Owner**: joseph
**Priority**: Medium
**Type**: Architecture + multi-phase implementation
**Tracking Issue**: [LongTermSupport/fedora-desktop#22](https://github.com/LongTermSupport/fedora-desktop/issues/22)

> Full original plan text, including the dated notes, is preserved verbatim in
> [PLAN_archive.md](PLAN_archive.md). Design rationale, the schema, the
> per-platform wrapper shapes and the risk register live in
> [DECISIONS.md](DECISIONS.md).

## Overview

The repo has a battle-tested multi-account pattern for GitHub: a
`github_accounts` map in `localhost.yml` drives per-alias SSH keys, per-alias
bash functions (`gh-<alias>`, `git-<alias>`, `clone-<alias>`) and a per-account
scope audit in `play-github-cli-multi.yml` (Plans 00034 and 00035). The user has
the same pain on Cloudflare and will have it on other CLIs next.

This plan introduces a single `project_personas` map in `localhost.yml` that
declares each identity once, with one flat platform-named key per platform the
persona uses. Per-platform playbooks read it directly. There is no compat shim:
`play-github-cli-multi.yml` fail-fasts on the legacy `github_accounts` map with
the exact migration YAML to paste. For CLIs without native multi-session auth,
the per-alias bash function injects credentials into the subprocess env from the
GNOME Keyring via `secret-tool`. Named-profile CLIs (AWS, GCP) get a thinner
wrapper that sets the active-profile env var per call.

## Goals

- Single source of truth for identity declarations: `project_personas` in
  `localhost.yml`, flat platform keys, full platform names, no `tools:` wrapper.
- New platform support is additive: declare a platform key, run the platform
  playbook, get the same `<cli>-<alias>` UX shape as `gh-<alias>`.
- Six platforms wired up: `github` (migrated), `cloudflare` (`wrangler-<alias>`),
  `fly_io` (`flyctl-<alias>`), `supabase` (`supabase-<alias>`),
  `amazon_web_services` (`aws-<alias>`), `google_cloud_platform` (`gcloud-<alias>`).
- Smart top-level `git` and `gh` auto-route to the right `<cli>-<alias>` from the
  cwd remote's org via `github.use_for_orgs`. GitHub-only. Per-alias wrappers
  stay as escape hatches.
- `play-github-cli-multi.yml` consumes `project_personas` directly and
  fail-fasts on legacy `github_accounts` with copy-pasteable migration YAML.
- Secrets for env-var-injection platforms live in the GNOME Keyring, never in
  `localhost.yml`, on disk in plaintext, or in shell history.
- `scripts/persona-setup.bash` handles the whole add-a-persona flow across all
  in-scope platforms.
- Adding a seventh platform is one playbook plus one platform field, not an
  architectural decision.

## Non-Goals

- Not implementing `npm_registry`, `microsoft_azure` or `docker_hub` (deferred;
  reasons in [DECISIONS.md](DECISIONS.md) Decision 6).
- Not implementing smart auto-routing for non-GitHub platforms. Custom
  per-directory marker files are rejected; `direnv` is the shelved future option
  (Decision 7).
- Not renaming or restructuring `play-github-cli-multi.yml` beyond the schema
  detection at the top.
- Not changing the `~/.ssh/github_<alias>` key naming convention (Plan 00035's surface).
- Not building a GUI or wizard. CLI-only, consistent with `run.bash`.
- Not encrypting persona-level metadata beyond existing `localhost.yml` vault behaviour.

## Dependencies

- **Depends on**: Plan 00035 (gh multi-account hardening). Both plans edit
  `play-github-cli-multi.yml`; coordinate ordering so edits do not collide.
- **Blocks**: any future per-platform multi-account plan (npm, azure, docker_hub).
- **Related**: Plan 00034 (`config_github_account` in `localhost.yml`).

## Tasks

### Phase 1: Research & Decision Gate

- [ ] ⬜ **Research GNOME Keyring + `secret-tool`**: confirm it is in the F43
  base or from libsecret, installed on the daily-driver, and usable at shell
  function call time without an unlock prompt. Output: notes for Decision 4.
- [ ] ⬜ **Research env-var-per-call CLIs (cloudflare / fly_io / supabase)**:
  confirm `CLOUDFLARE_API_TOKEN` + `CLOUDFLARE_ACCOUNT_ID`, `FLY_API_TOKEN` and
  `SUPABASE_ACCESS_TOKEN` override any active OAuth session; capture
  minimum-scope token requirements per platform. Output: notes file in this dir.
- [ ] ⬜ **Research named-profile CLIs (amazon_web_services /
  google_cloud_platform)**: confirm `AWS_PROFILE` and
  `CLOUDSDK_ACTIVE_CONFIG_NAME` select a profile per call without mutating
  config, even after interactive `aws configure` / `gcloud config set`. Output: notes file.
- [ ] ⬜ **Confirm in-scope platform list with user**: the six above in, the
  three deferred out. User can adjust before Phase 2.
- [ ] ⬜ **Decision gate**: present Phase 1 research and Decisions 1 to 7
  ([DECISIONS.md](DECISIONS.md)) to the user. Block remaining phases on approval.

### Phase 2: Schema definition + gh playbook fail-fast migration

- [ ] ⬜ **Define `project_personas` schema** in a comment block at the top of
  `localhost.yml`, with worked examples for all six platforms and the three
  shape rules.
- [ ] ⬜ **Populate `project_personas`** for the existing `github_accounts`
  aliases with only the `github` platform entry; `use_for_orgs` filled by the user.
- [ ] ⬜ **Remove the legacy `github_accounts` block** from `localhost.yml`.
- [ ] ⬜ **Update `play-github-cli-multi.yml` schema detection**: derive a local
  `_gh_accounts` fact from `project_personas`; fail fast on legacy
  `github_accounts` with a printable migration snippet; otherwise the existing
  "no accounts configured" path.
- [ ] ⬜ **Add `use_for_orgs` collision check** to the gh preflight: fail fast
  if any org appears under two personas.
- [ ] ⬜ **Rename internal references** in `play-github-cli-multi.yml` from
  `github_accounts` to the derived `_gh_accounts` fact.
- [ ] ⬜ **Add a one-line note** in `scripts/gh-account-setup.bash --add` that
  it appends to `project_personas` going forward.
- [ ] ⬜ **Verify gh side end-to-end** on host: generated `gh-aliases.inc.bash`
  byte-identical before and after migration for the same accounts.

### Phase 3: Platform CLI base installs

One idempotent, version-pinned install playbook per CLI binary under
`playbooks/imports/optional/common/`.

- [ ] ⬜ **`play-wrangler.yml`**: global npm install under the user's nvm context.
- [ ] ⬜ **`play-flyctl.yml`**: upstream installer with a `creates:` guard and
  `FLYCTL_INSTALL_VERSION` pin.
- [ ] ⬜ **`play-supabase-cli.yml`**: upstream tarball release (follows `yq` precedent).
- [ ] ⬜ **`play-aws-cli.yml`**: AWS CLI v2 upstream bundle, not pip.
- [ ] ⬜ **`play-gcloud.yml`**: Google Cloud SDK repo + dnf.
- [ ] ⬜ **Add each to `playbook-main.yml`** as opt-in imports, commented out by
  default (`play-ddev.yml` precedent).
- [ ] ⬜ **Verify installs** with each CLI's `--version` on host.

### Phase 4: Per-platform multi-account playbooks

All platforms share one shape: preflight, read `project_personas`, filter by
platform key, generate a per-alias bash function include. Differences are the
env-var set, the keyring attribute scheme and the health-check call
([DECISIONS.md](DECISIONS.md), "Per-platform wrapper shapes").

**4.0 — Common template extraction**

- [ ] ⬜ **Extract a Jinja macro / include task** taking
  `(platform_name, cli_binary, env_var_map, keyring_attr, health_call)` that
  emits a per-alias bash function, at
  `playbooks/imports/_shared/persona-bash-function-template.j2` or similar.

**4.1 — `play-cloudflare-multi.yml`** (env-var-per-call, keyring token)

- [ ] ⬜ **Preflight**: `secret-tool` on PATH; wrangler installed.
- [ ] ⬜ **Filter**: personas with `cloudflare` declared.
- [ ] ⬜ **Token presence check** via `secret-tool lookup`; fail fast with the
  `persona-setup.bash --set-token` remediation if missing.
- [ ] ⬜ **Generate `~/.bashrc-includes/cloudflare-aliases.inc.bash`** with
  per-alias `wrangler-<alias>` functions.
- [ ] ⬜ **Helper functions**: `cloudflare-list`, `cloudflare-whoami-<alias>`, `cloudflare-status`.
- [ ] ⬜ **Per-account health audit**: `wrangler whoami` per persona, fail fast
  with per-persona remediation.

**4.2 — `play-fly-io-multi.yml`** (env-var-per-call, keyring token)

- [ ] ⬜ Same shape as 4.1 with `FLY_API_TOKEN` only; function `flyctl-<alias>`.
- [ ] ⬜ Keyring scheme: `persona=<alias> platform=fly_io attr=api_token`.
- [ ] ⬜ Health audit: `flyctl auth whoami`.

**4.3 — `play-supabase-multi.yml`** (env-var-per-call, keyring token)

- [ ] ⬜ Same shape with `SUPABASE_ACCESS_TOKEN`; function `supabase-<alias>`.
- [ ] ⬜ Keyring scheme: `persona=<alias> platform=supabase attr=access_token`.
- [ ] ⬜ Health audit: `supabase projects list` or the cheapest read-only call.

**4.4 — `play-amazon-web-services-multi.yml`** (named profile, no keyring)

- [ ] ⬜ **Preflight**: AWS CLI installed. No `secret-tool` check.
- [ ] ⬜ **Filter**: personas with `amazon_web_services` declared.
- [ ] ⬜ **Profile presence check**: `[<alias>]` stanza in `~/.aws/credentials`
  or `~/.aws/config`; fail fast with `persona-setup.bash --aws-configure=<alias>`.
- [ ] ⬜ **Generate `~/.bashrc-includes/amazon-web-services-aliases.inc.bash`**
  with `aws-<alias>` functions that set `AWS_PROFILE`.
- [ ] ⬜ **Helper functions**: `aws-list`, `aws-whoami-<alias>`, `aws-status`.
- [ ] ⬜ **Per-account health audit**: `aws sts get-caller-identity` per profile.

**4.5 — `play-google-cloud-platform-multi.yml`** (named config, no keyring)

- [ ] ⬜ Same shape as 4.4 with `CLOUDSDK_ACTIVE_CONFIG_NAME`; function `gcloud-<alias>`.
- [ ] ⬜ Preflight: gcloud installed; configuration `<alias>` exists; fail fast
  with `persona-setup.bash --gcloud-init=<alias>`.
- [ ] ⬜ Health audit: `gcloud auth list --filter=status:ACTIVE` under the persona's env var.

### Phase 4.5: Smart top-level `git` and `gh` (GitHub only)

- [ ] ⬜ **Add `_persona_extract_org_from_cwd_remote`** to
  `~/.bashrc-includes/persona-router.inc.bash`: parse the GitHub owner from the
  `origin` URL (SSH and HTTPS forms).
- [ ] ⬜ **Add `_persona_lookup_alias_for_org`**: reads the generated
  `~/.config/persona-router/github-orgs.map` (`<org>=<alias>` lines).
- [ ] ⬜ **Generate `github-orgs.map`** in `play-github-cli-multi.yml` from
  `use_for_orgs`; fail fast on a duplicated org.
- [ ] ⬜ **Implement smart `git()` and `gh()` functions** per Decision 5:
  delegate to `<cli>-<alias>` on a match, else `command <cli>`.
- [ ] ⬜ **Add `persona-here` diagnostic**: prints which persona the cwd's remote routes to.
- [ ] ⬜ **Verify smart routing** on host with two personas owning different
  orgs plus an unclaimed org that passes through.

### Phase 5: Per-persona setup script (multi-platform dispatcher)

- [ ] ⬜ **Create `scripts/persona-setup.bash`** as a dispatcher with one
  `_setup_<platform>` sub-function per platform.
- [ ] ⬜ **Implement `--add=<alias>`**: interactive walk-through per platform,
  appends the persona block to `localhost.yml`.
- [ ] ⬜ **Implement `--set-token=<alias> <platform>`** for keyring platforms:
  token from stdin or `read -s`, validated with one read-only call, stored via
  `secret-tool store`; never echoed, never written to disk, never accepted on argv.
- [ ] ⬜ **Implement `--aws-configure=<alias>`**: delegates to `aws configure --profile`.
- [ ] ⬜ **Implement `--gcloud-init=<alias>`**: creates and initialises a gcloud configuration.
- [ ] ⬜ **Implement `--setup-all`**: idempotent iteration over every persona × platform.
- [ ] ⬜ **Implement `--check`**: read-only per-cell status table, no prompts, no writes.
- [ ] ⬜ **Hook into `run.bash`**: `persona-setup.bash --setup-all` replaces the
  `gh-account-setup.bash --setup-all` call site; gh logic retained as `_setup_github`.

### Phase 6: Docs

- [ ] ⬜ **New doc `docs/project-personas.md`**: schema with six worked
  examples, smart routing model, keyring token storage, named-profile pattern.
- [ ] ⬜ **Migration sub-section** in that doc: `github_accounts` to
  `project_personas`, the fail-fast message, the one-line fix.
- [ ] ⬜ **Add to `docs/README.md` index** under a "Multi-account tooling" section.
- [ ] ⬜ **Update `CLAUDE/AnsibleStyle.md`** with the persona-reading convention
  (filter by platform key, full platform names, fail fast on legacy schema).
- [ ] ⬜ **Document the "add a seventh platform" recipe**: naming, template
  variant, new playbook, template invocation, keyring scheme, setup sub-function.
- [ ] ⬜ **Document direnv as the future option** for non-GitHub per-directory
  routing (Decision 7).

### Phase 7: QA & validation

- [ ] ⬜ `./scripts/qa-all.bash` passes for all changed bash/ansible files.
- [ ] ⬜ `ansible-playbook --syntax-check` passes for all new/modified playbooks.
- [ ] ⬜ **Verify gh fail-fast path** on host with the legacy block restored;
  the error contains copy-pasteable migration YAML including `use_for_orgs`.
- [ ] ⬜ **Verify `use_for_orgs` collision detection** names both aliases.
- [ ] ⬜ **Verify gh byte-identical output** post-migration.
- [ ] ⬜ **Verify smart router**: cwd remote routing, `gh repo view <org>/repo`
  routing, and pass-through in a repo with no remote, checked with `persona-here`.
- [ ] ⬜ **Verify env-var-per-call platforms** for two personas each; confirm
  via `/proc/<pid>/environ` that tokens appear only in the CLI subprocess.
- [ ] ⬜ **Verify named-profile platforms** return different identities per alias.
- [ ] ⬜ **Verify `--check` catches deliberately-broken personas** (revoked
  token, deleted profile) in the per-cell table.
- [ ] ⬜ **Verify keyring lookup latency** is acceptable per invocation; cache
  for the shell session if not.

## Success Criteria

- [ ] `project_personas` is the only identity map in `localhost.yml`; the legacy
  `github_accounts` block is removed.
- [ ] Schema follows the three settled rules: flat platform keys, full platform
  names, `github.use_for_orgs` for org ownership.
- [ ] `play-github-cli-multi.yml` fail-fasts before migration with a
  copy-pasteable block and produces byte-identical `gh-aliases.inc.bash` after.
- [ ] `use_for_orgs` collision check fail-fasts at deploy time naming both aliases.
- [ ] Smart `git` / `gh` route correctly for every declared org and pass through
  for unclaimed orgs, verified on host with two personas.
- [ ] `persona-here` prints the correct persona, or "(default)" on no match.
- [ ] `wrangler-<alias>`, `flyctl-<alias>`, `supabase-<alias>` work for two
  personas each with tokens fetched from the keyring at call time.
- [ ] `aws-<alias>` and `gcloud-<alias>` return different identities per alias.
- [ ] `persona-setup.bash --check` reports every persona × platform cell;
  `--set-token` never writes a token to disk in plaintext.
- [ ] Adding a seventh platform needs only a new playbook, a platform key and,
  if secret-bearing, a setup sub-function.
- [ ] `./scripts/qa-all.bash` passes.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00045-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1 decision gate approved by the user
- Phase 2 gh migration deployed with byte-identical output verified
- All six platform playbooks deployed and health audits green
- `persona-setup.bash` wired into `run.bash`
- Docs landed and qa-reviewer pass recorded
