# Plan 00063 — Technical decisions

Supporting document for [PLAN.md](PLAN.md). Owner decisions and their rationale,
in the order they were taken. The mechanisms they select are specified in
[DESIGN.md](DESIGN.md); the discussion that led to each is in `JOURNAL/`.

## Decision 1: Env vars as the single headless input channel

**Context**: Headless mode needs config without a TTY; options were env vars, an
answers file, or Ansible extra-vars passthrough.
**Decision** (owner): environment variables (`RUN_BASH_*`). 12-factor, drops
directly into cloud-init `user-data` and CI, matches the existing
`gh-account-setup.bash` env pattern, no new file format to parse or secure. An
answers file is an explicit Non-Goal.
**Date**: 2026-07-23

## Decision 2: GitHub is mandatory to configure; the "configured empty" half is deferred

**Context**: Round 2 showed GitHub-mandatory is the dominant headless complexity
and security driver, and grounding showed GitHub is not strictly required to
provision: the repo is public (clones over HTTPS with zero auth; the SSH clone is
only for push-back), and `playbook-main.yml` needs `localhost.yml` (identity plus
vault), not `github_accounts`.
**Decision** (owner): keep GitHub mandatory to configure. Headless must set
`RUN_BASH_GITHUB_ACCOUNTS` explicitly; unset fails fast. The target end-state is
that an explicit `none` clones over HTTPS and skips the whole GitHub/SSH block.
**Refinement** (owner, Task 1.6): the round-3 coverage audit found the empty path
is not a no-op skip. It breaks `playbook-main.yml` on a no-GitHub box through two
latent server-profile bugs (`play-github-cli-multi.yml:42` ungated
`gh --version`; `play-lxc-install-config.yml:240` `git@` clone). So headless v1
is GitHub-configured and token-required: a single account plus
`RUN_BASH_GITHUB_TOKEN_FILE` are mandatory, and `RUN_BASH_GITHUB_ACCOUNTS=none`
fails fast naming the follow-up. The empty path is delivered by a separate plan
(fix the two plays, then re-enable). Landed in `run.bash` v1.9.1.
**Date**: 2026-07-23

## Decision 3: Plan first, then a hostile Opus review loop, then execute

**Context**: `run.bash` is a critical bootstrap; a regression bricks a provision.
**Decision** (owner): write the plan, then run an adversarial review loop with
independent Opus agents (coverage lens and security lens) before any code is
written, and only execute once the loop converges. The loop ran three rounds; see
`JOURNAL/` for each round's judgement.
**Date**: 2026-07-23

## Decision 4: Secrets travel as `0600` file pointers; the literal env form is allowed but guarded

**Context**: The round-1 security audit proved that literal secrets in env are
exposed two ways on exactly the cloud-init use case Decision 1 targets: cloud-init
persists `user-data` to `/var/lib/cloud/instance/user-data.txt` and the metadata
service, world-readable indefinitely; and every child process inherits an
exported secret via `/proc/PID/environ`.
**Decision** (owner-confirmed): support both forms; docs recommend file-based. For
each secret (GitHub token, vault password, SSH passphrase) accept a literal env
var and a `*_FILE` path var; `*_FILE` takes precedence and `--help-run-headless`
advises it. The literal form is guarded by V3.10 (hard-fail on cloud, loud warn
elsewhere, fail if both set, never fall back on an unreadable file, unset before
the first child). Even the file must be delivered out-of-band on cloud (V3.1),
never via `write_files`. Mirrors the repo's existing `VAULT_PASS_FILE`.
**Date**: 2026-07-23

## Decision 5: Headless requires NOPASSWD sudo and a scoped GitHub token; single account in v1

**Context**: The round-1 coverage audit proved the load-bearing interactive walls
are not bash prompts: direct `sudo` (password) and `gh` device-code OAuth.
**Decision**: headless requires NOPASSWD:ALL sudo (probed at startup with
`sudo -k -n true`, fail fast if absent; the default cloud user has it) and a
scoped GitHub token file (`gh auth login --with-token`). v1 supports a single
GitHub account; multiple accounts fail fast rather than silently partially
authenticating. Both are documented preconditions in `--help-run-headless`.
**Date**: 2026-07-23

## Decision 6: The SSH key keeps its passphrase, loaded via ssh-agent plus `SSH_ASKPASS`

**Context**: The owner chose to keep `~/.ssh/id` passphrase-protected at rest but
doubted ssh-agent plus askpass was really best. Grounding: there is no file or
stdin passphrase flag for `ssh-add` or `ssh-keygen`; `SSH_ASKPASS` (with
`SSH_ASKPASS_REQUIRE=force`) is the only non-interactive mechanism to load a
passphrase-protected key, and `ssh-keygen -P/-N` only accept the passphrase via
argv. The mechanism is not a free choice.
**Decision**: read the passphrase from `RUN_BASH_GITHUB_SSH_PASSPHRASE[_FILE]`,
generate the key, start an ssh-agent spanning keygen, clone and pull, load the
key with a transient `SSH_ASKPASS` helper that reads the `0600` file, and kill the
agent right after the last pull with the EXIT trap as backstop (V3.12, V3.13).
Stated residuals, accepted for a dedicated same-uid provision box: the passphrase
is transiently visible in `/proc/PID/cmdline` during `ssh-keygen`, and the askpass
helper momentarily handles it.
**Follow-on** (after Task 1.6): since v1 always configures GitHub and the key is
passphrase-protected, `RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE` is required in
preflight, mirroring the interactive flow's no-empty-passphrase rule. Landed in
`run.bash` v1.9.2.
**Date**: 2026-07-23
