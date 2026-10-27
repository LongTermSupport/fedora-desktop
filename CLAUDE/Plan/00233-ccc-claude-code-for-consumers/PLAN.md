# Plan 00233: ccc, Claude Code containerised for consumers

**Status**: In Progress (built; host deploy and acceptance open)
**Created**: 2026-10-27
**Owner**: joseph
**Priority**: Medium

## Overview

The owner asked for "a new ccc command - claude code consumer. runs in podman, normalises to a
folder in home dir, (can be a repo for internal tracking purposes but no forced github) -
becomes a safe default way to run claude code containerised for general chat etc", and on
review: "its basically ccy, but without the github coupling".

A consumer install (`./run.bash --consumer`, Plan 00228) had no way to run Claude Code:
`play-podman.yml` and `play-claude-yolo.yml` were `audience: developer`, ccy refuses to start
outside a git repository root (`lib/common.bash` `check_git_repo`), and its entrypoint exits
without `GH_TOKEN` and a working `gh auth login` (`entrypoint.sh` lines 30-33, 64-127).

`ccc` is a Go command that runs Claude Code in rootless podman, always in `~/CCC`, with
nothing from the host but that folder, the agent's own state and its sign-in token. It reuses
ccy's image, token pool, permission mode and Go packages and none of ccy's developer
prerequisites. It is started as `ccc` in a terminal or as "Claude" from the app grid.

## Goals

- `ccc`, run from any directory, starts Claude Code in rootless podman in `~/CCC`, made on
  first use as a local git repository with no remote.
- The container sees only `~/CCC` and the agent's state, carries only the sign-in token, and
  has no host networking, no published ports and no device or socket passthrough.
- One sign-in, made on first use, kept in ccy's token pool and reused on every later run.
- Core on every install, podman with it; on a developer box the image is ccy's.
- The agent is a parameter: a later `ccc --opencode` in the same folder is one table entry.

## Non-Goals

- A network policy of its own: issue #114 / Plan 00232 owns it; ccc has one seam for it.
- Building `--opencode`, or any developer feature (project Dockerfiles, compose, SSH, GitHub,
  the agent bus, tmux, session restore after reboot).
- A second image or Dockerfile.

## Owner Decisions

1. **Folder**: `~/CCC` — "future it might not be just claude, it might be ccc --opencode". The
   agent is a parameter (`internal/ccc/agent.go`, `agents`), Claude the default.
2. **Permission mode**: "Full bypass now", as ccy: `--dangerously-skip-permissions`.
3. **History**: a snapshot commit at session end; a shipped `.gitignore` (caches,
   `node_modules`, venvs, build output, OS junk, secret-looking files); never a remote.
4. **Install**: core for consumers, podman with it. `play-claude-code-consumer.yml` is
   imported by `playbook-main.yml` with `audience: all` (the audience key has no
   consumer-only value, so a developer install gets ccc too; its image is ccy's).

## Design

01. **A new Go command beside ccylaunch** (`src-go/cmd/ccc`, `src-go/internal/ccc`), not a
    mode of the bash launcher (on the ratchet) or of the Go front (which hands over to the
    bash). It runs `podman run` itself, with `--entrypoint /usr/bin/tini`, so ccy's
    `entrypoint.sh` (GitHub-bound, on the ratchet) is not used and the image is unchanged.
02. **Podman**: `play-podman.yml` is `audience: all`; podman-compose and the Docker-API socket
    carry `when: install_audience == 'developer'`.
03. **Folder** (C3 in the package doc): `~/CCC` whatever the working directory; refused when a
    symbolic link or not a folder; a non-repository is made one (branch `main`, a
    repository-local identity `<full name>` / `<login>@localhost`, `.gitignore` and the
    agent's guide file `CLAUDE.md` when absent, a first commit); files there are kept.
04. **Network**: `networkArgs()` in `internal/ccc/run.go`, the network ccy uses by default
    (`podman`), no host network, no ports. It is the one seam Plan 00232 changes when its
    policy lands, with no ccc option to turn it off.
05. **Sign-in** (C5): the latest usable `ccc.<date>.token` in `~/.claude-tokens/ccy/tokens`
    (mentioned within 14 days of expiry); none is a `claude setup-token` in a throwaway
    container with nothing mounted, saved as `ccc.<today+90>.token`, 0600, ccy's estimate. A
    failed sign-in is offered again `[Y/n]`, at most three. `--token NAME` uses another pool
    token and never signs in. The token goes by name (`-e CLAUDE_CODE_OAUTH_TOKEN`).
06. **Mounts**: `~/CCC` at `/workspace` and `~/.local/share/ccc/<agent>` at `/var/lib/ccc`
    (`CLAUDE_CONFIG_DIR`), both `:z`; nothing else. Before each run ccc records in the state's
    `.claude.json` what ccy's entrypoint records: onboarding done, the bypass accepted,
    `/workspace` trusted.
07. **Image and version check** (C4): `claude-yolo:latest`, staged and built by
    `tasks/claude-yolo-image.yml`, which `play-claude-yolo.yml` and the ccc play both import.
    ccc refuses an image whose `claude-yolo-version` label is older than the launcher's
    `REQUIRED_CONTAINER_VERSION`, which the play reads from the launcher and builds in.
08. **ctrl+z guard** (`internal/ccc/guard.go`): the session runs on ccc's own pseudo-terminal;
    keys are copied in raw mode with the `0x1a` byte removed, the window size followed.
09. **Start**: `ccc [--claude] [--continue] [--token NAME]`; the app grid entry "Claude" runs
    it with `Terminal=true`.
10. **Versions**: no image or entrypoint change, so no container bump. `internal/gitx` (in the
    ccy front's build closure) gained four methods: CCY_VERSION 3.100.36.

## ccy Parity ("basically ccy, without the GitHub coupling")

| ccy feature                                | In ccc | Why                                                                    |
| ------------------------------------------ | ------ | ---------------------------------------------------------------------- |
| Token pool, expiry stamps, expiry warning  | Yes    | Same pool and `tokens` package; warned within 14 days                  |
| Token creation by `setup-token`            | Yes    | Same flow, no `GH_TOKEN` (the login never needed it)                   |
| Permission bypass, `IS_SANDBOX`            | Yes    | Owner decision 2                                                       |
| Claude state persisted and pre-trusted     | Yes    | Outside the folder, so snapshots never hold transcripts or credentials |
| Image version check                        | Yes    | Against `REQUIRED_CONTAINER_VERSION`, built in by the play             |
| ctrl+z guard                               | Yes    | ccc's own guard; the folder has no hooks-daemon supervisor             |
| Host time zone, auto-compact window 600000 | Yes    | `TZ` from `/etc/localtime`; `ccclaude.DefaultWindow`                   |
| Claude Code updated in place in the image  | Later  | Task 5.1: needs the image build lock shared out of `ccylaunch`         |
| Shutdown warning for running sessions      | Later  | Task 5.2: the containers carry `ccc=true`; Plan 00231 owns the warning |
| Network DNS repair (`ensure_network_dns`)  | No     | A VPN-specific developer fix; Plan 00232 owns the network              |
| tmux, restore after reboot, supervisor arm | No     | A closed window is a finished session; `--continue` resumes            |
| Picker of many tokens, usage report        | No     | One sign-in; `--token NAME` for the rest                               |
| SSH, gh, project Dockerfiles, compose, bus | No     | GitHub-coupled or developer-only                                       |

## Tasks

### Phase 1: Design and decisions

- [x] ✅ **Task 1.1**: Design reviewed; owner decisions recorded above

### Phase 2: ccc, tests first

- [x] ✅ **Task 2.1**: Folder, repository and `.gitignore` (C3)
- [x] ✅ **Task 2.2**: Snapshot commit at session end (C8)
- [x] ✅ **Task 2.3**: Token from the pool, sign-in with bounded retry, `--token` (C5)
- [x] ✅ **Task 2.4**: State and `.claude.json` (C6)
- [x] ✅ **Task 2.5**: `podman run` arguments, network seam, version check (C4, C7)
- [x] ✅ **Task 2.6**: The pseudo-terminal ctrl+z guard
- [x] ✅ **Task 2.7**: `cmd/ccc`, `gitx` additions, CCY 3.100.36, the seam rule's podman owner

### Phase 3: IaC

- [x] ✅ **Task 3.1**: `play-podman.yml` to `audience: all`
- [x] ✅ **Task 3.2**: `tasks/claude-yolo-image.yml` extracted from `play-claude-yolo.yml`
- [x] ✅ **Task 3.3**: `play-claude-code-consumer.yml`, imported by `playbook-main.yml`
- [ ] ⬜ **Task 3.4**: `ansible-playbook --syntax-check` on the changed plays (no
  `ansible-playbook` in this container; the host's `qa-all.bash` runs it)

### Phase 4: Host verification

- [ ] ⬜ **Task 4.1**: `deploy.bash` and `acceptance.bash` through `meta-deploy.bash`
- [ ] ⬜ **Task 4.2**: The host checks below, by the host agent or the owner
- [ ] ⬜ **Task 4.3**: `qa-reviewer` over the plan's diff

### Phase 5: Follow-ups

- [ ] ⬜ **Task 5.1**: Claude Code updated in place in the image under the shared build lock
- [ ] ⬜ **Task 5.2**: The shutdown warning counts `ccc=true` containers (with Plan 00231)
- [ ] ⬜ **Task 5.3**: Adopt Plan 00232's policy in `networkArgs()` when it lands

## Host Checks

`acceptance.bash` checks what a script can: `ccc --help`; the build carries the image version
(no terminal is refused for the terminal, not the version); the image is at least
`REQUIRED_CONTAINER_VERSION`; the token pool and state are 0700; the app grid entry runs ccc in
a terminal and validates; podman is rootless. NOT ESTABLISHABLE by it, so done by hand:

1. `ccc` from any directory: a sign-in on first use, then Claude Code in `~/CCC`.
2. A second `ccc`: no sign-in.
3. While it runs: `podman ps --filter label=ccc=true --format '{{.Mounts}}'` lists only
   `~/CCC` and `~/.local/share/ccc/claude`; `podman exec <name> printenv GH_TOKEN` prints
   nothing.
4. ctrl+z does nothing; resizing the window redraws the session.
5. After `/exit`: `git -C ~/CCC log --oneline | cat` shows the snapshot, `git -C ~/CCC remote`
   prints nothing.
6. "Claude" in the app grid opens the same in a terminal window.

## Dependencies

- Depends on: Plan 00228 (consumer install) for the audience model.
- Related: Plan 00232 / issue #114 (network policy, Task 5.3); Plan 00231 (shutdown
  warning, Task 5.2); Plan 00226 (the supervisor's ctrl+z guard for cc).

## Success Criteria

- [ ] `ccc` from any directory opens Claude Code in `~/CCC`, made on first use
- [ ] A consumer install with no GitHub account installs and runs it
- [ ] The container's mounts are exactly the two above, and it has no `GH_TOKEN`
- [ ] A second run reuses the first run's sign-in
- [ ] ctrl+z does not freeze a session
- [ ] QA passes, and `qa-reviewer` raises no BLOCK or FIX-BEFORE-MERGE finding

## Risks & Mitigations

| Risk                                           | Impact | Probability | Mitigation                                                     |
| ---------------------------------------------- | ------ | ----------- | -------------------------------------------------------------- |
| The image is large for a small consumer laptop | M      | M           | One image; a slimmer build target as a later plan if seen      |
| Bypass mode while the LAN is reachable         | M      | H           | Owner decision; Plan 00232's block lands in `networkArgs()`    |
| Moving the image tasks breaks the ccy play     | H      | L           | One task file imported by both; the version-hook test walks it |
| Claude Code in the image ages until a rebuild  | M      | M           | Task 5.1                                                       |
| A process left holding the session's terminal  | L      | L           | The output drain is bounded (`drainWait`)                      |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00233-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Design for review: bda19d3e6
- The ccc package and gitx additions: 72e1641ad
