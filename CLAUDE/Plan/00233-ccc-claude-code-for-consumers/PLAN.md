# Plan 00233: ccc, Claude Code containerised for consumers

**Status**: Not Started (design written; awaiting review before any build)
**Created**: 2026-10-27
**Owner**: joseph
**Priority**: Medium

## Overview

The owner asked for "a new ccc command - claude code consumer. runs in podman, normalises to a
folder in home dir, (can be a repo for internal tracking purposes but no forced github) -
becomes a safe default way to run claude code containerised for general chat etc".

Today a consumer install (`./run.bash --consumer`, Plan 00228) gets no way to run Claude Code
at all. `play-podman.yml` and `play-claude-yolo.yml` are both `audience: developer`, and ccy
could not serve a consumer even if it were installed: it refuses to start outside a git
repository root (`files/var/local/claude-yolo/lib/common.bash` `check_git_repo`), and its
container entrypoint exits without `GH_TOKEN` and a working `gh auth login`
(`files/var/local/claude-yolo/entrypoint.sh` lines 30-33 and 64-127). Host `cc` falls back to
the fedora-desktop checkout outside a repository (Plan 00167), which is a developer's answer.

This plan adds `ccc`: a Go command that runs Claude Code in rootless podman, always in one
folder, `~/Claude`, with nothing from the host but that folder and the Claude login. It
reuses ccy's image, token pool and Go packages, and none of ccy's developer prerequisites: no
GitHub, no SSH keys, no `gh` tokens, no agent bus, no project Dockerfiles. It is started from
a terminal as `ccc` or from the desktop's app grid as "Claude".

## Goals

- `ccc`, run from any directory, starts Claude Code in a rootless podman container whose
  working directory is `~/Claude`, created on first use.
- `~/Claude` is a local git repository with no remote; ccc never runs `gh`, `ssh` or any
  forge call.
- The container sees only `~/Claude` and ccc's own Claude state, carries only the Claude
  token, and has no host networking, no published ports and no device or socket passthrough.
- One Claude login (a long-lived token from `claude setup-token`) is made on first use, kept
  in the token pool ccy already uses, and reused on every later run.
- A consumer install offers it on the optional-play menu; a developer install can install it
  too, and shares the image ccy already builds.
- A desktop launcher, "Claude", opens ccc in a terminal window.

## Non-Goals

- A per-container network policy of its own. Issue #114 (another worker) designs ccy's
  policy, blocking private ranges by default; ccc adopts that policy when it lands (Design 4).
- Any developer feature: project Dockerfiles, compose networks, SSH, GitHub accounts, the
  agent team bus, `--teams`, session restore after reboot, multiple projects.
- A second image or a second Dockerfile. ccc uses `claude-yolo:latest`.
- A graphical chat client. ccc is Claude Code's own terminal UI.
- Changing ccy's behaviour. Anything shared that moves is a refactor with ccy's tests held.

## Context & Background

- ccy: the bash launcher `files/var/local/claude-yolo/claude-yolo` (about 4,000 lines, on the
  bash-limits ratchet), its `lib/*.bash`, its Go front `src-go/cmd/claude-yolo` +
  `src-go/internal/ccylaunch`, and the image built from `files/var/local/claude-yolo/Dockerfile`
  by `play-claude-yolo.yml`.
- The token pool `~/.claude-tokens/ccy/tokens/<name>.<YYYY-MM-DD>.token` is already shared by
  ccy, cc and speech-to-text (`src-go/internal/wsitoken`), through `src-go/internal/tokens`.
- ccy's token creation runs `claude setup-token` with `--entrypoint claude`, which bypasses
  `entrypoint.sh` (`lib/token-management.bash` `create_token`); its insistence on `GH_TOKEN`
  is the bash function's own check, not something the login needs.
- The Go toolchain is `audience: all` (`play-golang.yml`), so a consumer box can build Go
  commands through `tasks/build-go-command.yml`.
- The ctrl+z hazard (CLAUDE/ContainerRules.md): Ink stops Claude Code with SIGSTOP on ctrl+z,
  which a container cannot recover from. ccy's guard is the hooks-daemon supervisor deployed
  into each project; `~/Claude` has no hooks daemon, so ccc needs its own guard (Design 9).

## Design

### 1. How ccc relates to ccy: a new Go command beside ccylaunch, not a mode of the launcher

**Decision**: `src-go/cmd/ccc` + `src-go/internal/ccc`, a Go program that runs `podman run`
itself. It is not a `--consumer` mode of `claude-yolo`.

- A mode of the bash launcher is ruled out: the launcher is on the ratchet allowlist and may
  only shrink, and every consumer branch would run beside four thousand lines of GitHub, SSH,
  compose and bus logic that a consumer must never reach.
- A mode of the Go front (`ccylaunch`) is ruled out: the front exists to answer a few flags
  and hand over to the bash launcher; a consumer launch would be a second launcher inside it,
  and every ccc change would then demand a CCY version bump.
- What ccc reuses, rather than copies: the image (`claude-yolo:latest`), `internal/tokens`
  (pool reading, file naming, expiry), `internal/engine` (podman calls, the SELinux relabel
  verdict), `internal/execx`, `internal/term`, `internal/gitx`, `internal/fsx`, and the image
  build lock (Design 7).
- ccc does **not** use `entrypoint.sh`. It runs `--entrypoint /usr/bin/tini` and starts
  `claude` directly, after writing the few settings the entrypoint would have written into
  its own state directory on the host (Design 6). So the image is unchanged, and
  `entrypoint.sh` (764 lines, on the ratchet) does not grow a consumer branch.

### 2. Podman for the consumer profile

**Decision**: `play-podman.yml` becomes `audience: all`; its Docker-compatibility parts stay
developer-only, behind `when: install_audience == 'developer'`.

- Rootless podman is what ccc needs, and podman is already on Fedora Workstation; the play's
  `podman system migrate`, the stale `tempdirs` clean and `podman info` check are repairs a
  consumer needs as much as a developer.
- `podman-compose` (a pip `--user` install) and the user `podman.socket` (a Docker-API socket)
  serve compose and Docker tooling only, so they keep the developer guard as tasks rather than
  the play.
- No second podman play, no consumer subset file: the play already exists and is small.

### 3. The folder: `~/Claude`, and its git behaviour

**Decision**: `~/Claude`, fixed, wherever ccc is started from.

- Why `~/Claude`: it is visible in Files at the top of the home folder beside `Documents` and
  `Downloads`, named after the product the user knows, capitalised like the XDG folders. A
  dotfolder would hide the user's own files from them; `~/Documents/Claude` buries it a level
  down and mixes it into whatever syncs or backs up `Documents` by other means.
- Normalisation: ccc ignores its working directory; the container's `/workspace` is always
  `~/Claude`. It refuses (fails fast, naming the path) if `~/Claude` exists and is not a
  directory, or is a symlink to somewhere outside the home folder.
- First use: create `~/Claude` (0700), `git init` it with branch `main`, set a
  repository-local identity (`user.name` from the account's full name, `user.email`
  `<login>@localhost`) so no global git config is needed, write a short `CLAUDE.md` saying
  this is a general-purpose folder for someone who may not write code, and make the first
  commit. An existing folder that is not a repository is initialised the same way; its files
  are not moved.
- Tracking: at the end of each session ccc commits whatever changed (`git add -A`, message
  `ccc session <start time>`), on the host, so every session is a restorable snapshot. It
  never adds a remote, never pushes, never fetches. A commit that fails stops ccc with git's
  words; a session with no changes makes no commit.

### 4. Network

- The container joins the network ccy uses by default, never `--network host`, and publishes
  no ports. No `host.containers.internal` alias is added.
- Adopting #114: ccc must not grow its own blocking. When #114 lands, its policy is applied
  through whatever single component it ships (a named network, a Go package that returns the
  `podman run` network arguments, or nftables rules keyed on a container label). ccc's
  network choice is one function in `internal/ccc` returning its `podman run` arguments, so
  adopting the policy is a change to that function and its test, with the private-range block
  on by default and no ccc option to turn it off. Until then the container can reach the
  LAN, which is the main reason for the permission mode in Design 8.

### 5. Login

**Decision**: the long-lived token from `claude setup-token`, in ccy's pool, under the name
`ccc`.

- First use with no usable token named `ccc` (none, or expired by its file stamp): ccc
  explains in plain words what is about to happen, runs `claude setup-token` in a throwaway
  container (`--entrypoint claude`, no mounts), the user signs in through the browser link and
  pastes the code back, and ccc stores the token as `ccc.<expiry>.token`, 0600, through the
  `tokens` package's naming. A failed or cancelled sign-in is re-offered in a bounded loop
  (CLAUDE/InteractiveScripts.md), then exits naming what to do.
- The token goes to the container by name (`-e CLAUDE_CODE_OAUTH_TOKEN`, value in ccc's child
  environment only), never in the `podman run` argv, as ccy does (BSH-09).
- Why not Claude Code's in-app `/login` persisted in the state directory: two ccc windows
  would share one refresh token and race on its rotation, logging one out; the pool token has
  no refresh. It also keeps one login story across ccc, ccy and cc on a developer box, which
  can point `ccc --token NAME` at an existing pool token.
- The pool directory `~/.claude-tokens/ccy/tokens` is created by the ccc play (0700), as the
  ccy play does today.

### 6. What is mounted

| Host                        | Container      | Mode | Why                                                  |
| --------------------------- | -------------- | ---- | ---------------------------------------------------- |
| `~/Claude`                  | `/workspace`   | rw   | the user's folder; Claude's working directory        |
| `~/.local/share/ccc/claude` | `/var/lib/ccc` | rw   | Claude Code's config and history (CLAUDE_CONFIG_DIR) |

Nothing else: no SSH, gitconfig, `gh`, docker or podman socket, GPU, display, clipboard,
D-Bus or agent-bus mount, and no `GH_TOKEN`. SELinux relabelling follows ccy's verdict
through `internal/engine` (`:z` where enforcing). Claude Code's state is kept outside
`~/Claude` so the user's folder and its git snapshots never hold transcripts or credentials.
Before each run ccc merges into the state's `.claude.json` what ccy's entrypoint writes into
its own: `/workspace` trusted (`hasTrustDialogAccepted`) and onboarding complete, so the user
meets no developer-facing dialogs.

### 7. The image and keeping Claude Code current

- ccc uses `claude-yolo:latest`. On a developer box it is the image ccy already built; on a
  consumer box the ccc play builds it. Building it needs the build-context staging that is
  inside `play-claude-yolo.yml` today, so that staging and the build move into one task file,
  `tasks/claude-yolo-image.yml`, imported by `play-claude-yolo.yml` and by the ccc play
  (DRY). `scripts/test-ccy-container-version-hook.bash` reads what the play stages into the
  image, so it follows the move.
- A missing image is not built by ccc: it fails, naming the play to re-run.
- Claude Code freshness: ccc does what ccy's `update_claude_inplace` does (compare the
  image's `claude --version` with the latest release, `npm install -g` in place and commit)
  under the same image build lock, so a ccc and a ccy launch never update one image at once.
  The lock's Go side is `internal/ccylaunch/buildlock.go` today; the lock-taking half moves to
  a shared package (`internal/imagelock`) both use. Update failures stop the launch; there is
  no "start stale and warn".
- The image is large (Node, language servers, browsers). Accepted for one image; a slimmer
  build target is a later plan if a consumer laptop proves too small.

### 8. Permission mode inside the container

**Decision (open for the owner)**: `acceptEdits` until #114's private-range block is applied
to ccc; then `bypassPermissions`, as ccy runs.

- `acceptEdits` lets Claude create and change files in `~/Claude` without asking (every
  session is a git snapshot, so edits are undoable) and asks before commands and web fetches.
- The container is the real boundary. Once it cannot reach the LAN, prompts about shell
  commands inside a sandbox that holds only `~/Claude` add little for someone who cannot
  judge a shell command, and `bypassPermissions` is the friendlier default.
- Without the LAN block, a prompt-injected session in bypass mode could reach a router or NAS
  on the local network, so the prompt stays until the network is closed.

### 9. The ctrl+z guard

**Decision (open for the owner)**: ccc holds the terminal and filters it. It runs `podman run -it` on a pseudo-terminal of its own and copies the user's keystrokes to it with the `0x1a`
SUSP byte removed, and ignores SIGTSTP itself. This is the supervisor's `strip_suspend()`
done on the host side. It needs no image change and nothing from the hooks daemon, and it
uses `golang.org/x/sys/unix`, already a module dependency. The alternative, a guard binary
baked into the image, needs a container version bump and duplicates the daemon's supervisor
inside ccy's image.

### 10. How a consumer starts it

- Terminal: `ccc`. Options are kept to `--help`, `--token NAME` and `--continue`, which
  resumes the last conversation; with no option it starts a new one, and `/resume` inside
  Claude Code lists earlier ones from the state directory.
- Desktop: `~/.local/share/applications/ccc.desktop`, `Name=Claude`, `Terminal=true`,
  `Exec=ccc`, an icon from the icon theme, so GNOME opens it in the default terminal without
  this repo naming one. Closing the window ends the container (`--rm`) and ccc still makes the
  session's commit before it exits.

### 11. Install and version bumps

- A new optional play, `playbooks/imports/optional/common/play-claude-code-consumer.yml`,
  `scope: gnome` (it installs a desktop entry), `audience: all`. It imports `play-golang.yml`
  and `play-podman.yml`, imports `tasks/claude-yolo-image.yml`, builds `ccc` into
  `~/.local/bin` with `tasks/build-go-command.yml`, creates the token pool directory and
  `~/.local/share/ccc/claude` (0700), and deploys the desktop entry. Optional, not core: it
  needs a Claude subscription and a large image, and not every consumer has or wants either.
- CCY version: no image file, `entrypoint.sh` or launcher changes, so no container version
  bump. `internal/ccylaunch` and the shared packages in the front's dependency set
  (`tokens`, `engine`, `execx`, ...) are under the CCY gate: moving the build lock out of
  `ccylaunch` and anything added to `tokens` take a CCY_VERSION patch bump with a
  `docs/ccy-changelog.md` entry, in the commit that makes them.
- `ccc` itself carries no version of its own (YAGNI).

## Tasks

### Phase 1: Design review

- [ ] ⬜ **Task 1.1**: Coordinator and owner review the design; settle the open questions
  below and record the answers in this plan

### Phase 2: ccc core, tests first

- [ ] ⬜ **Task 2.1**: Folder normalisation and first use (`~/Claude` resolved from the home
  folder whatever the working directory; refusals for a non-directory or an escaping symlink;
  `git init`, local identity, seeded `CLAUDE.md`, first commit), tests first
- [ ] ⬜ **Task 2.2**: Session snapshot commit at exit, including after the window is closed
- [ ] ⬜ **Task 2.3**: Token: pick the `ccc` token from the pool; the `setup-token` first-use
  flow with a bounded retry; `--token NAME`
- [ ] ⬜ **Task 2.4**: State directory and its `.claude.json` merge (trust, onboarding)
- [ ] ⬜ **Task 2.5**: The `podman run` argument builder (mounts, env by name, network
  function, labels, permission mode), each argument held by a test
- [ ] ⬜ **Task 2.6**: The pseudo-terminal proxy that strips `0x1a` (Design 9)
- [ ] ⬜ **Task 2.7**: Claude Code in-place update under the shared image lock; move the lock
  into `internal/imagelock` with a CCY patch bump
- [ ] ⬜ **Task 2.8**: Run `./scripts/qa-changed.bash`; the Go gate passes

### Phase 3: IaC

- [ ] ⬜ **Task 3.1**: `play-podman.yml` to `audience: all`, compose and socket developer-only
- [ ] ⬜ **Task 3.2**: Extract `tasks/claude-yolo-image.yml` from `play-claude-yolo.yml`; move
  `test-ccy-container-version-hook.bash` with it
- [ ] ⬜ **Task 3.3**: `play-claude-code-consumer.yml` and the desktop entry
- [ ] ⬜ **Task 3.4**: `ansible-playbook --syntax-check` on the changed plays; QA

### Phase 4: Docs and host verification

- [ ] ⬜ **Task 4.1**: User docs: a consumer page for ccc and a pointer in `docs/ccy.md`
- [ ] ⬜ **Task 4.2**: `deploy.bash`, `triage.bash` and `acceptance.bash` (folder created,
  repository with no remote, only the two mounts, no `GH_TOKEN`, token reused on a second
  run, ctrl+z inert, desktop entry present), queued in `CLAUDE/Plan/meta-deploy.list`
- [ ] ⬜ **Task 4.3**: Adopt #114's policy in the network function when it lands
- [ ] ⬜ **Task 4.4**: `qa-reviewer` over the plan's full diff

## Dependencies

- Depends on: Plan 00228 (consumer install, In Progress) for the audience model and the
  consumer optional-play menu.
- Related: issue #114 (ccy network policy) for Design 4 and Design 8; Plan 00226 (cc under
  the supervisor) for the ctrl+z guard ccy and cc rely on.

## Open Questions for the Owner

1. Folder name: `~/Claude` (recommended) or another?
2. Permission mode (Design 8): `acceptEdits` until the LAN is blocked, then bypass?
3. ctrl+z guard (Design 9): host-side terminal filter in ccc, or a guard baked into the
   image?
4. Snapshot commit after every session (Design 3): wanted, or git only when the user asks?
5. Optional play (Design 11), or core on a consumer install?

## Success Criteria

- [ ] `ccc` from any directory opens Claude Code in `~/Claude`, created on first use
- [ ] A consumer install with no GitHub account can install and run it
- [ ] The container's mounts are exactly the two in Design 6, and it has no `GH_TOKEN`
- [ ] A second run reuses the first run's login without asking
- [ ] ctrl+z does not freeze a session
- [ ] QA passes, and `qa-reviewer` raises no BLOCK or FIX-BEFORE-MERGE finding

## Risks & Mitigations

| Risk                                                         | Impact | Probability | Mitigation                                                   |
| ------------------------------------------------------------ | ------ | ----------- | ------------------------------------------------------------ |
| The image is too large for a small consumer laptop           | M      | M           | Optional play; a slimmer build target as a later plan        |
| LAN reachable until #114 lands                               | M      | H           | `acceptEdits` until then (Design 8)                          |
| Moving the image build out of the ccy play breaks ccy builds | H      | L           | One task file imported by both; ccy's version-hook test held |
| Snapshot commits of large files the user drops in            | L      | M           | Revisit with a size rule if it is seen                       |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00233-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Design written for review
