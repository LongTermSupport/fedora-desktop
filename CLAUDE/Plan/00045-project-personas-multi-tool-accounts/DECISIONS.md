# Plan 00045 — Design Decisions, Schema and Risk Register

Supporting document for [PLAN.md](PLAN.md). Holds the durable design rationale
that was extracted from the original plan document; the verbatim original is
[PLAN_archive.md](PLAN_archive.md).

## Background: the pattern being generalised

`environment/localhost/host_vars/localhost.yml` today carries a
`github_accounts: {alias: username}` map. `playbooks/imports/play-github-cli-multi.yml`
reads it and, via Jinja templating into `.bashrc-includes/gh-aliases.inc.bash`,
generates per-alias bash functions (`gh-<alias>`, `git-<alias>`, `clone-<alias>`,
`remote-<alias>`, `gh-token-<alias>`, `gh-<alias>-token-phpstorm`) plus per-alias
SSH `Host` blocks in `~/.ssh/config`. The same playbook runs a per-account OAuth
scope audit (`github_required_scopes`) and uploads pubkeys with `gh ssh-key add`.
That pattern landed via Plans 00034 and 00035.

Every multi-account CLI has the same UX shape: named identities, a per-tool
credential or account ID per identity, `<tool>-<alias>` functions that run the
tool as that identity, one setup command per persona, and `<tool>-status` /
`<tool>-list` / `<tool>-whoami` helpers. Rebuilding that per tool drifts each
time; a common spine reduces the drift to zero.

Per-tool identity values differ. GitHub keys off a username. Cloudflare keys off
a 32-char hex Account ID plus an API token; wrangler's OAuth session is
one-active-account-at-a-time, so the schema must carry per-tool identity values
rather than a boolean per tool.

## The `project_personas` schema

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

Shape rules:

- Top-level key is the **alias** (matches the existing `github_accounts` keys, so
  the SSH key path stays `~/.ssh/github_<alias>`).
- Each persona has a `name` field plus N **platform keys** at the persona top
  level. No `tools:` wrapper.
- Platform keys use the **full platform name**, never the CLI binary: `github`
  not `gh`, `cloudflare` not `wrangler`, `amazon_web_services` not `aws`,
  `google_cloud_platform` not `gcp`.
- `github.use_for_orgs` (optional list) declares which orgs the persona owns and
  drives the smart `git` / `gh` router (Decision 5). Absent or empty means the
  persona is reachable only via `git-<alias>` / `gh-<alias>` or by personal-repo
  namespace match.
- CLI-shaped sub-fields are fine *inside* a platform block where genuinely needed
  (e.g. `cloudflare.api_token_keyring_key`). The platform key itself stays
  platform-named.

## Decision 1: Schema shape — flat platform keys, full platform names

Three sub-decisions resolved together on 2026-05-29.

**1a. Alias-keyed vs tool-keyed top level.** Options: alias-keyed
(`<alias>: { … per-platform … }`) or tool-keyed (`github: { <alias>: … }`).
Decided alias-keyed: keeps each persona's full declaration in one place, mirrors
`github_accounts`, reads as "persona X owns these platform identities".

**1b. `tools:` wrapper vs flat platform keys.** Decided flat. The wrapper is
YAGNI nesting: platform names do not collide with metadata fields (`name`,
future `email`, future `default`), so a playbook can enumerate platforms by
treating any persona key not in a small known-metadata list as a platform. One
fewer level in every YAML edit, Jinja expression and diff hunk.

**1c. Platform key naming.** Options: CLI binary (`gh`, `wrangler`, `aws`,
`gcloud`), short platform name (`github`, `aws`, `gcp`), full platform name
(`github`, `amazon_web_services`, `google_cloud_platform`). Decided full name.
The CLI-binary option fractures identity (`gh`, `git` and `clone-<alias>` share
one identity). The short option is jargon: `aws` reads as platform, CLI, region
or account depending on context. Per the user: *"a few tokens = much greater
clarity for humans + LLMs"*. Correct keys: `github`, `cloudflare`,
`amazon_web_services`, `google_cloud_platform`, `microsoft_azure`,
`npm_registry`, `docker_hub`, `fly_io`.

## Decision 2: KISS — fail-fast detection, no compat shim

The user rejected a compat shim on review (2026-05-26): *"lets not have
complicated shims/compat etc - KISS let the user handle upgrading the config as
required"*.

Decided: `play-github-cli-multi.yml` reads `project_personas` directly. If only
the legacy `github_accounts` block is present, the playbook fail-fasts with a
printable YAML snippet showing the exact `project_personas` migration block plus
the instruction to delete the legacy block. Only one user and one machine need
to migrate; a Jinja resolver, a QA assertion that derived equals manual, and the
risk of subtle divergence buy nothing. The fail-fast message IS the migration
guide.

## Decision 3: Wrangler auth — API token + env-var injection

Wrangler supports `wrangler login` (OAuth, single active session in
`~/.config/.wrangler/`) and `CLOUDFLARE_API_TOKEN`. OAuth switching means
logout/login cycles, incompatible with a per-invocation `wrangler-<alias>` model.

Decided: one API token per persona, injected via env vars on every invocation.
`wrangler-<alias>` exports `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`
only on the `command wrangler` subprocess. No stateful login switching. Matches
the user's need *"we will need to have something that explicitly exports vars eg
the bash functions"* and matches what `gh-<alias>` does conceptually. Subject to
Phase 1 confirming wrangler has no undocumented per-config-dir mode.

## Decision 4: Secret storage — GNOME Keyring via `secret-tool`

Options considered:

1. **Plaintext `~/.config/personas/<alias>/<tool>.env` mode 0600.** Same threat
   model as `~/.ssh/` keys. Any process as the user can `cat` it; backups grab
   it unencrypted.
2. **GNOME Keyring via `secret-tool` (libsecret).** Encrypted at rest, unlocked
   at GNOME login (no per-call prompt), per-user isolation by the keyring
   daemon. `gh` itself already stores its OAuth tokens there on this machine.
3. **`pass` (passwordstore.org).** GPG-encrypted, well-regarded, but adds a GPG
   keyring dependency and another CLI to learn.
4. **`ansible-vault encrypt_string` in `localhost.yml`.** Already the pattern for
   `github_ssh_passphrase`, but `vault-pass.secret` sits on disk and decrypting
   at bash-function call time would spawn `ansible-vault` per call.

Decided: Option 2. Fast per-call lookup, no unlock prompt, encrypted at rest,
free per-tool/per-persona attribute scoping via `secret-tool` key-value
attributes. Keyring attribute scheme:
`persona=<alias> platform=<platform> attr=<attr-name>`.

Fallback: if Phase 1 finds `secret-tool` is not reliably available on the
deployment target, use Option 1 (0600 files) — acceptable for the daily-driver.

## Decision 5: Smart top-level `git` and `gh` with `use_for_orgs` auto-routing

Once `use_for_orgs` declares org ownership, the default `git` and `gh` commands
become shell functions that:

1. Detect the relevant org from context: cwd `origin` remote URL for `git`; cwd
   remote OR a `gh` argv positional such as `gh repo view <org>/<repo>` for `gh`.
2. Look up `project_personas[*].github.use_for_orgs` for a match.
3. Single match: delegate to `git-<alias>` / `gh-<alias>`.
4. No match: pass through to `command git` / `command gh` (no surprise routing).
5. Multiple matches: fail fast naming the collision.

Per-alias wrappers stay as escape hatches for forcing a specific persona.

Reference shape of the router:

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

The org lookup reads a generated `<org>=<alias>` text file at
`~/.config/persona-router/github-orgs.map`, written by
`play-github-cli-multi.yml`, so no YAML parsing happens per call.

Why: removes "did I call `git-foo` or `git-bar`?"; misrouted commits become
impossible for any declared org; unclaimed orgs keep default behaviour.

Edge cases:

- **Personal repos** (`<gh-username>/*`): `github.username` already declares
  that namespace; the router falls back to username matching after org matching.
- **Org-list collisions**: fail fast at playbook deploy time (preferred) AND at
  function-call time (defence in depth if `localhost.yml` is edited without a
  re-deploy).
- **No git remote in cwd**: pass through unchanged (`git init`, `git config --global`).
- **`gh` calls without a repo argument** (`gh auth status`, `gh api user`): pass through.

Decided 2026-05-29. Pure additive feature; it adds Phase 4.5.

## Decision 6: In-scope platform set

The user asked (2026-05-29) to cover "most common ones in one go" rather than
fragmenting into follow-up plans.

| Platform                | Pattern                        | Decision |
| ----------------------- | ------------------------------ | -------- |
| `github`                | OAuth + SSH (native multi)     | IN       |
| `cloudflare`            | API token, env-var-per-call    | IN       |
| `fly_io`                | API token, env-var-per-call    | IN       |
| `supabase`              | Access token, env-var-per-call | IN       |
| `amazon_web_services`   | Named profile + `AWS_PROFILE`  | IN       |
| `google_cloud_platform` | Named config + env var         | IN       |
| `npm_registry`          | `.npmrc` per-scope + token     | DEFER    |
| `microsoft_azure`       | Multiple auth modes            | DEFER    |
| `docker_hub`            | Single-active config file      | DEFER    |

The token-env-var platforms and the named-profile platforms all fit the Phase 4
template without new design; each costs platform-specific config plus one
playbook.

Why defer the bottom three:

- `npm_registry`: publish auth lives in `.npmrc` files that can be
  project-scoped or registry-scoped (`@scope:registry=...`). Needs a third
  template variant (file-template-per-cwd). Easy follow-up once needed.
- `microsoft_azure`: only worth doing if the user actively uses Azure. The `az`
  CLI has several auth modes (device code, service principal, managed identity,
  federated credential) with different env-var combos per mode.
- `docker_hub`: `~/.docker/config.json` is single-active. Per-call switching
  means rewriting the config (race-prone with parallel `docker`) or
  `docker --config <dir>` (verbose, breaks `docker compose` UX). Needs its own
  design plan.

The schema is forward-compatible: a user can add `npm_registry: { … }` blocks
today and nothing breaks; a follow-up plan adds the per-platform handler.

Migration friendliness: all six in-scope platforms use the same fail-fast
message format; only `github_accounts` exists as a legacy block today, the
others are greenfield.

## Decision 7: Smart routing is GitHub-only; direnv is the shelved future option

Non-GitHub platforms have no "cwd implies persona" signal. The user considered
a per-directory marker file (`.persona` or similar) and rejected it: *"no random
files"*.

Decided: smart routing is GitHub-only in this plan. For other platforms the
user invokes `wrangler-<alias>`, `aws-<alias>`, etc. explicitly.

Shelved, not implemented: if per-directory defaults become desirable later, the
right tool is **`direnv`** (an existing upstream project, nothing to invent).
Users would write an `.envrc` that exports the platform token from
`secret-tool lookup …` and the account ID; `direnv` loads it on cwd change
behind `direnv allow` and unloads on leaving. It is a real dependency to
install and document, it is not needed today, and the schema is already
compatible with it, so it stays on the shelf.

## Per-platform wrapper shapes

**Env-var-per-call (cloudflare, fly_io, supabase).** The function looks the
token up with `secret-tool lookup persona <alias> platform <platform> attr <attr>`,
fails with the `persona-setup.bash --set-token=<alias> <platform>` remediation if
absent, then runs `command <cli> "$@"` with the token (and for cloudflare the
account ID from `project_personas`) set only on that subprocess. Env vars:
`CLOUDFLARE_API_TOKEN` + `CLOUDFLARE_ACCOUNT_ID`; `FLY_API_TOKEN`;
`SUPABASE_ACCESS_TOKEN`. Health calls: `wrangler whoami`, `flyctl auth whoami`,
`supabase projects list` (or the cheapest read-only call found in Phase 1).

**Named-profile (amazon_web_services, google_cloud_platform).** No keyring. The
function sets `AWS_PROFILE=<alias>` or `CLOUDSDK_ACTIVE_CONFIG_NAME=<alias>` and
runs the CLI; credentials stay in `~/.aws/credentials` / gcloud's named
configurations with their native permissions model. Health calls:
`aws sts get-caller-identity`, `gcloud auth list --filter=status:ACTIVE`.

## Risks and mitigations

| Risk                                                                               | Impact                                                                                              | Mitigation                                                                                                                   |
| ---------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Wrangler has no per-account isolation, only one active OAuth                       | High. Confirms the API-token + env-var route                                                        | Phase 1 research confirms up front; per-persona keyring tokens is the chosen path                                            |
| `secret-tool` / GNOME Keyring unavailable or locked at call time                   | High. `wrangler-<alias>` would fail at call time                                                    | Phase 1 confirms `secret-tool` is installed and the keyring unlocks at GNOME login; Decision 4 fallback to 0600 files        |
| Token leak via shell history if pasted on the command line                         | High                                                                                                | `--set-token` reads from stdin or `read -s`, never argv; refused if `$1` looks like a token                                  |
| Token leak via `ps -e` / `/proc/<pid>/environ`                                     | Low. `/proc/<pid>/environ` is 0400 and owned by the process user; same surface as today's gh tokens | Set env vars only on the `command wrangler` subprocess, never exported into the parent shell                                 |
| Persona schema accretes ad-hoc per-platform flags                                  | Medium long-term                                                                                    | Phase 6 "add a seventh platform" recipe enforces a consistent shape; reviewers reject ad-hoc fields without playbook support |
| User on legacy `github_accounts` ignores the fail-fast and downgrades the playbook | Low                                                                                                 | Fail-fast message includes the rationale; migration is one block edit                                                        |
| User has one Cloudflare account today; multi-account is YAGNI                      | Low. The schema generalises regardless                                                              | Even with one account the abstraction lets later platforms land cheaply; Phase 1 user confirmation before commit             |

## Relevant code locations

- `environment/localhost/host_vars/localhost.yml` — schema lives here.
- `playbooks/imports/play-github-cli-multi.yml` — existing gh template, generates `gh-aliases.inc.bash`.
- `scripts/gh-account-setup.bash` — per-account interactive setup; widened to per-persona by Phase 5.
- `vars/` — likely home for a `vars/persona-defaults.yml` if needed.
