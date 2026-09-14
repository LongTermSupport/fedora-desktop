# Plan 00116: ccy uses a project's deploy key and a forwarded ssh-agent, so a box with no GitHub account identity can still run it

**Status**: In Progress
**Created**: 2026-09-14
**Owner**: joseph
**Priority**: High

## Overview

ccy's whole idea of a GitHub identity is a `~/.ssh/github_<alias>` key written by
`play-github-cli-multi.yml`. That is right for a laptop. It is wrong for a headless box built in
`RUN_BASH_GITHUB_ACCOUNTS=none` mode, which is the mode the headless runbook exists for: such a box
holds **no** account key on purpose. What it does hold is a per-repository **deploy key** — a
read-only key registered on one repository — wired through an ssh-config `Host` alias so that
`git@<alias>:owner/repo.git` clones with it. And when a person is logged into that box over
`ssh -A`, the session carries a **forwarded ssh-agent** that can push as that person, with nothing
persisted on the box.

Today ccy sees neither. It lists `~/.ssh/github_*`, finds nothing, warns "No github\_ SSH keys
found", and even when handed the deploy key by `--ssh-key` it would probe it with `-F /dev/null`,
so the alias the remote URL names would not resolve inside the container and the clone could not
even fetch. The agent it never considers: its own probe agent is deliberately private, and the
container starts a fresh agent of its own.

This plan makes ccy understand both, without giving the box an identity it does not have:

- **The remote's alias is resolved, not pattern-matched.** `ssh -G <alias>` says what host, port
  and identity file the user's own ssh config binds the alias to. If the host is GitHub, that
  identity file is the project's key: ccy offers it (and selects it outright when it is the only
  candidate), mounts it, and hands the container the `Host` stanza it needs so the alias keeps
  working inside. A deploy key authenticates as `owner/repo`, not as a user, and ccy says so
  instead of trying to map it to a `gh-token-<alias>` function.
- **`--ssh-agent` forwards the session's agent into the container.** The identity is whatever the
  agent signs as, probed the same way a key is. On an SELinux-enforcing host the container's
  `container_t` domain may not connect to a socket served by an unconfined process, and relabelling
  the socket file does not change that (measured: `:z` moved the file to `container_file_t` and
  the connect was still denied), so the flag disables SELinux labelling for that one container and
  says so in the launch banner.

The `gh` token is unchanged: ccy already honours an exported `GH_TOKEN`. What changes is that a
token arriving that way is now cross-checked against the agent's identity when there is one, the
same check a `github_` key gets, and the "not authenticated" error names `GH_TOKEN` as the other
way in.

## Goals

- A project whose remote uses an ssh-config alias bound to GitHub is fetched and pushed inside
  ccy with the key the alias names, from a box with no `github_` key and no `gh` login.
- `ccy --ssh-agent` mounts `$SSH_AUTH_SOCK`, the container uses it, and `git push` inside the
  container authenticates as the forwarded identity.
- A deploy-key identity (`Hi owner/repo!`) is reported as a deploy key and never confused with a
  user account.
- Everything above is unit-tested with a stub `ssh` on `PATH`; nothing in the tests talks to
  GitHub.

## Non-Goals

- Minting, registering or rotating deploy keys. That is the provisioning side's job.
- Making the container's `gh` work without a token. The entrypoint's `gh auth login` stays.
- Agent forwarding on hosts whose sshd forbids it. That is sshd configuration, not ccy's.

## Tasks

### Phase 1: Deploy keys

- [x] ✅ **Task 1.1**: `resolve_github_ssh_alias <url>` in `lib/ssh-handling.bash`: for a
  `git@<host>:` or `ssh://git@<host>/` remote whose host is not GitHub literally, run
  `ssh -G <host>` and, when its `hostname` is `github.com` or `ssh.github.com`, return the alias,
  hostname, port and the first identity file that exists. `parse_github_owner_repo` accepts the
  alias form on the same condition.
- [x] ✅ **Task 1.2**: `discover_and_select_ssh_keys` offers the alias key: as the default row of
  the menu when `github_` keys also exist, selected outright when they do not. The "No github\_
  SSH keys" warning is reached only when there is neither.
- [x] ✅ **Task 1.3**: `build_ssh_mounts_and_validate` probes an alias key against the alias's own
  host and port, recognises the `owner/repo` answer as a deploy key, leaves `GITHUB_USERNAME`
  empty for it, and renders the container's `Host <alias>` stanza (`SSH_CONFIG_EXTRA`) plus the
  `[host]:port` pairs the entrypoint must pin (`SSH_KNOWN_HOSTS_PINS`).
- [x] ✅ **Task 1.4**: `entrypoint.sh` appends `SSH_CONFIG_EXTRA` to `~/.ssh/config` and pins
  every host in `SSH_KNOWN_HOSTS_PINS` with the same fetched GitHub keys.

### Phase 2: Forwarded agent

- [x] ✅ **Task 2.1**: `--ssh-agent` flag; refused with a clear message when `SSH_AUTH_SOCK` is
  unset or `ssh-add -l` lists no key. The interactive menu offers the agent as a row whenever it
  has keys.
- [x] ✅ **Task 2.2**: Identity probe through the agent (`IdentityAgent=$SSH_AUTH_SOCK`,
  `IdentitiesOnly=no`), with the same 22-then-443 fallback a key gets.
- [x] ✅ **Task 2.3**: The container gets the socket mounted, `SSH_AUTH_SOCK` pointed at it,
  `--security-opt label=disable`, and a banner line saying so. The entrypoint starts no agent of
  its own when one is forwarded; mounted keys are then wired by `IdentityFile` stanzas instead of
  `ssh-add`, so nothing is ever added to the person's agent.
- [x] ✅ **Task 2.4**: An exported `GH_TOKEN` is cross-checked against the agent's login when the
  agent gave one; the no-token error names `GH_TOKEN` as the alternative to `gh auth login`.

### Phase 3: Proof and docs

- [x] ✅ **Task 3.1**: `scripts/test-ccy-ssh-handling.bash` — stub `ssh` and `ssh-add` on `PATH`;
  covers alias resolution, owner/repo parsing, deploy-key identity classification, stanza
  rendering, agent refusal paths. Wired into `qa-all.bash` beside the other ccy tests.
- [x] ✅ **Task 3.2**: `docs/ccy.md`: "SSH and GitHub" describes deploy-key aliases and
  `--ssh-agent`, the command table and troubleshooting table gain rows, the security model names
  what a forwarded agent widens.
- [ ] ⬜ **Task 3.3**: Live: on a headless box holding only a deploy key, `ccy` in that project
  selects the key unprompted and `git fetch` inside the container succeeds; with `--ssh-agent`
  from an `ssh -A` session, `git push --dry-run` authenticates as the person.
- [x] ✅ **Task 3.4**: Versions: `CCY_VERSION` minor bump; `LABEL claude-yolo-version` and
  `REQUIRED_CONTAINER_VERSION` bumped together for the entrypoint change.

## Success Criteria

- [ ] From a project whose remote is `git@<alias>:owner/repo.git`, on a box with no `github_`
  key, `ccy` starts without the "No github\_ SSH keys" warning and `git fetch` works inside.
- [ ] `ccy --ssh-agent` inside an `ssh -A` session: `ssh -T git@github.com` inside the container
  answers with the person's login.
- [ ] `./scripts/qa-all.bash` green, including the new test.

## Delivery & Milestones

- Plan opened; live measurements of the SELinux socket denial recorded in the journal.
