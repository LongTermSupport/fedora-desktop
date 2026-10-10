# Plan 00169: ccy image keeps its tools current

**Status**: In Progress (Phase 1 accepted on the host; Phase 3 next)
**Created**: 2026-10-09
**Owner**: joseph
**Priority**: High

## Overview

The ccy image is built `FROM node:lts-slim`, which is Debian 12 bookworm, and it ships
git **2.39.5**. A host session (git 2.48 or later) created a worktree with
`--relative-paths`, and that wrote `extensions.relativeWorktrees` into the shared
`.git/config`. The container's git then refused the whole repository. Every new ccy
session started with no git and no hooks daemon, and Plan 00161's acceptance failed
(M2.7). Nothing in ccy noticed that a tool had fallen behind the host. Claude Code is the
only tool with an update mechanism (the launcher's daily in-place npm update).

The research ([RESEARCH-base-image.md](RESEARCH-base-image.md)) finds that Debian was
never chosen: the first ccy commit picked `node:20-slim` for Node, and Debian came with
it. No stable Debian release ships git 2.48 or later, backports included (trixie is
2.47.3). The floating `node:lts-slim` tag still resolves to bookworm (measured digest
equality). Fedora 44, the host's release, ships git 2.56, ShellCheck 0.11.0 and
ImageMagick 7. The project image fetches ShellCheck and ImageMagick 7 by hand to work
around Debian (its ImageMagick comment still says Fedora 43); git is not fetched.

This plan does three things. It makes a too-old container fail loudly at launch, on
either base. It makes images refresh their packages without anyone editing the
Dockerfile, while keeping the container-version rules intact. And it moves the image to
Fedora at the host's release (D1), so the container's tools are the desktop's tools.

## Goals

- A ccy session whose container git cannot read `/workspace` refuses to start and prints
  git's own error. It never starts a session with no git.
- The base image has a floor for critical tools (git, Node). A build that cannot meet a
  floor fails and keeps the previous image.
- Images pick up distro package updates by being rebuilt on a schedule, with no
  Dockerfile edit and no version bump.
- Version policy: trusted, stable tools float on distro packages. Pins are kept only
  where a stated compatibility or integrity reason demands one, and are registered for
  drift reports.
- The image is Fedora at the host's release, and every template, doc and
  client-project path that taught apt is ported or fails with a migration message.

## Non-Goals

- Changing the daily in-place Claude Code update (`auto_update_claude_code`). It works;
  only the age label it must not reset is added.
- Unpinning the project image's QA tools (`.qa-versions`). Those pins exist so the QA
  verdict does not depend on where it runs (Plan 00071), which is a compatibility reason.
- Porting other projects' `.claude/ccy/Dockerfile`s. Those live in other repos. This plan
  makes their breakage explicit and documents the fix.
- Arm64 images.

## Context & Background

- Evidence, package tables and the full Debian-assumption inventory:
  [RESEARCH-base-image.md](RESEARCH-base-image.md).
- Incident record: the Plan 00166 journal (26-10-09) and Plan 00161 (M2.7).
- Version-bump rules: [CLAUDE/ContainerRules.md](../../ContainerRules.md). A
  Dockerfile or `entrypoint.sh` change bumps `LABEL claude-yolo-version` and
  `REQUIRED_CONTAINER_VERSION`. A launcher or `lib/` change bumps `CCY_VERSION`.
- Pin manifest: `vars/version-pins.yml`, read by `scripts/check-pinned-versions.bash`.

## Owner decisions (recorded; detail in the journal)

- **D1 Base image:** Fedora, the full image `registry.fedoraproject.org/fedora:<release>`
  (not fedora-minimal, so plain `dnf` is there). Phase 3-alt is not taken.
- **D2 Fedora release:** the host's own release, passed as a build arg; the image is
  rebuilt when the host upgrades.
- **D3 Node:** through nvm, not Fedora's `nodejs`. The `node >= 22` floor stays.
- **D4 Refresh:** GitHub Actions builds the base image weekly (and on Dockerfile pushes and
  by hand) and publishes it to GHCR only if the floors and basics checks pass; ccy pulls it
  and builds locally only as the fallback. The owner confirms the package name and
  visibility before the first publish.
- **D5 gh:** from GitHub's rpm repo.
- **D6 Migration:** a major ccy version (the owner's call); Task 3.7 makes that a major
  `CCY_VERSION` and container `3.0`. The launcher refuses an
  apt-based project Dockerfile and names the fix (Task 3.8); agents in those projects port
  them. No legacy Debian tag.

## Tasks

### Phase 1: Fail fast when the container cannot read the repository (either base)

- [x] ✅ **Task 1.1**: `triage.bash` (read-only, host, with its probes in
  `triage-probe.bash`; host run 2026-10-10, journalled). Record the host git version, the
  repository's `extensions.*` keys, each ccy image's git version, its
  `claude-yolo-version`, its base-layer digest against the registry's current digest
  for `node:lts-slim`, and whether `podman build` re-pulled `FROM` (it is expected not to
  without `--pull`).
- [x] ✅ **Task 1.2**: In `entrypoint.sh`, directly after git is configured, run
  `git -C /workspace rev-parse --git-dir`. On failure, print to stderr that the
  container's git (`git --version`) cannot read `/workspace`, followed by git's own
  captured message verbatim, then `exit 1` before Claude starts. Unconditional: the
  launcher only starts from a directory holding `.git` (`check_git_repo`). Unit test
  `scripts/test-ccy-git-preflight.bash`, in `qa-all.bash`.
  - [x] ✅ Container 2.50, CCY 3.91.0; changelog entry and the `docs/ccy.md` entrypoint step.
- [x] ✅ **Task 1.3**: `acceptance.bash` (host, run as `deploy.bash`'s last leg; 24 of 24
  on the host 2026-10-10, journalled). Make a throwaway repository under
  `untracked/`, set `extensions.relativeWorktrees=true` and
  `core.repositoryformatversion=1`, and launch ccy non-interactively there. Assert a
  non-zero exit and that the output contains git's `unknown repository extension` line.
  Also launch in a clean throwaway repository and assert the session starts. Print a
  coverage line. Once the image's git is 2.48 or later (Phase 3) the same repository must
  open instead, and the script says the refusal is then covered by the unit test only.

### Phase 2: Decide

- [x] ✅ **Task 2.1**: Owner answers D1-D6 (2026-10-09, journalled); see
  [Owner decisions](#owner-decisions-recorded-detail-in-the-journal).

### Phase 3 (D1 = Fedora): port the base image

- [ ] ⬜ **Task 3.1**: Build spike in a scratch Dockerfile under the plan folder,
  `FROM registry.fedoraproject.org/fedora:44`. `dnf install` the full package map from
  RESEARCH Section 4, less `nodejs`/`npm` (D3: nvm). Install Node LTS through nvm and put
  `node`/`npm` and the global npm bin on the PATH every process sees (non-login shells,
  the entrypoint, hooks), not only an interactive shell's. Record which names resolve
  (`tini` at `/usr/bin/tini`), npm's global prefix, and the agent-browser `REAL` path,
  and log it.
- [ ] ⬜ **Task 3.2**: Rewrite `files/var/local/claude-yolo/Dockerfile`'s base stage:
  - `ARG FEDORA_RELEASE` and `FROM registry.fedoraproject.org/fedora:${FEDORA_RELEASE}`
  - drop the `APT::Sandbox` line
  - the apt layers become `dnf install -y --setopt=install_weak_deps=False … && dnf clean all`
  - yq and uv come from Fedora packages; gh per D5
  - fix the PHPantom and tzdata comments, which talk about Debian
- [ ] ⬜ **Task 3.3**: Chromium dependencies come from Chrome for Testing's own `rpm.deps`
  after Chrome is unpacked (keep container 2.51's curl fetch; agent-browser's own download
  times out on a slow link). Do not use `--with-deps`, whose dnf list names two
  packages missing in F44 and calls `sudo`. The layer fails if any provide does not
  resolve. `rpm.deps` line 2 is a rich boolean (`(libgtk-3.so.0()(64bit) or libgtk-4.so.1()(64bit))`), so each provide is passed to dnf quoted.
- [ ] ⬜ **Task 3.4**: The launcher passes `FEDORA_RELEASE` from the host's
  `/etc/os-release` `VERSION_ID` to both build sites (`build_container_with_hash` and
  `play-claude-yolo.yml`'s build task). The image records it as a label. A mismatch with
  the host triggers a rebuild, per D2. Bump `CCY_VERSION`.
- [ ] ⬜ **Task 3.5**: Port this repo's `.claude/ccy/Dockerfile` to dnf:
  - ImageMagick 7 from dnf, which replaces the AppImage
  - ShellCheck from dnf only if its version equals `.qa-versions`; otherwise keep the
    upstream binary
  - change the apt cache mounts to dnf ones
- [ ] ⬜ **Task 3.6**: Port the templates (`Dockerfile.project-template`,
  `Dockerfile.example-ansible`, `Dockerfile.example-golang`), the generator prompt and
  snippets in `lib/dockerfile-custom.bash`, `CUSTOM-DOCKERFILES.txt`,
  `ccy-startup-info.txt`, `CLAUDE/ContainerRules.md` ("Where a Missing Tool Goes") and
  `docs/containerization.md` to dnf. Also the launcher's `claude-yolo:2108` message
  ("Tip: apt/npm packages are cached between builds for speed"), a `CCY_VERSION` bump.
- [ ] ⬜ **Task 3.7**: Make it a major container version (`3.0`) and a major `CCY_VERSION`
  (D6). The changelog entry says that existing project Dockerfiles must be rebuilt for
  Fedora (apt to dnf), and how.
- [ ] ⬜ **Task 3.8**: Implement the D6 migration guard. Before building a project image on
  a Fedora base, the launcher checks the project Dockerfile for `apt-get` or `dpkg`. If it
  finds either, it stops and names the file, the line and `CUSTOM-DOCKERFILES.txt`.
  Shellcheck-clean and covered by a `scripts/test-*.bash` case.
- [ ] ⬜ **Task 3.9**: The basics every session can rely on (owner: "ensure it then has
  expected basic tooling included"). The Fedora container image is smaller than an
  install (no `procps`, `which`, `less`, `iproute` and the like are promised). Write the
  list as a tracked file beside the floors file (Task 4.1): every command a session or
  agent is expected to find (start from what the Debian image provides today, measured
  in the Task 3.1 spike: shell utilities, `ps`, `which`, `less`, `ip`, `ss`, `hostname`,
  `file`, `unzip`, `xz`, `jq`, `yq`, `curl`, `wget`, `git`, `gh`, `ssh`, `rsync`, an
  editor, `make`, `gcc`, `python3`, `pip`, `uv`, `node`, `npm`). A base-stage `RUN` runs
  `command -v` on each and fails the build naming every one missing. `docs/ccy.md` lists
  them, so agents in other projects know what they can count on.

### Phase 3-alt (D1 = Debian): keep Debian, build git — not taken (D1 = Fedora)

- [ ] ⬜ **Task 3A.1**: Add a `git-builder` stage that builds the latest stable git tag
  from kernel.org, resolved at build time (not hardcoded), into `/usr/local`, with its
  `libexec/git-core` helpers and `git-remote-https`. Remove the apt `git` package from
  the base layer.
- [ ] ⬜ **Task 3A.2**: Move `FROM` to `node:lts-trixie-slim` (stable, not oldstable).
  Bump the container version and write the changelog entry.

### Phase 4: Refresh without edits (both bases)

- [ ] ⬜ **Task 4.1**: Add a tracked floors file under `files/var/local/claude-yolo/`
  (`git 2.48`, `node 22`). A final base-stage `RUN` compares each tool's version against
  it and fails the build naming the tool, what it has and the floor. On Debian without
  Phase 3-alt this check must fail. Prove that with a spike before relying on it.
- [ ] ⬜ **Task 4.2**: Add `LABEL claude-yolo-built-at` in the last layer, next to the
  Dockerfile hash (PERF-02 placement), from a build arg. Confirm that
  `update_claude_inplace`'s `podman commit` keeps the label, so the daily Claude update
  does not reset the image's age.
- [ ] ⬜ **Task 4.3**: Add a non-interactive `ccy --refresh-images`. It rebuilds the base
  images with `--pull=always --no-cache` under the **same** recipe, so the version label
  and hash are unchanged and no bump is owed. Project images then rebuild through the
  existing "base image updated" path on their next launch. A build or floor failure
  leaves the old image tagged and exits non-zero. Bump `CCY_VERSION`.
- [ ] ⬜ **Task 4.4**: The D4 trigger, in GitHub Actions. A workflow under
  `.github/workflows/` builds the base image from `files/var/local/claude-yolo/` weekly
  on a schedule, on every push that changes it, and by hand (`workflow_dispatch`), once
  per supported Fedora release (D2: the release is a tag, e.g. `:f44`). It runs the
  floors (4.1) and basics (3.9) checks, and publishes to the GitHub Container Registry
  (`ghcr.io/<owner>/claude-yolo:<release>` plus a dated tag) only if they pass, so a bad
  build is never pulled. The built-at label (4.2) and the recipe hash are set as now.
  OWNER: the first publish makes a public package; confirm the name and visibility
  before it runs.
- [ ] ⬜ **Task 4.6**: The launcher pulls the published image for the host's release
  when it is newer than the local one (compare built-at labels), and builds locally only
  when the registry cannot be reached or the local Dockerfile differs from the published
  recipe hash (an unpushed edit being tested). A pull failure with no usable local image
  fails loud. `ccy --refresh-images` (4.3) becomes "pull, else build". This also takes
  the long Chrome download off the owner's machine. Bump `CCY_VERSION`; covered by a
  `scripts/test-*.bash` case with a stub registry answer.
- [ ] ⬜ **Task 4.5**: Document the rule in `CLAUDE/ContainerRules.md`. The container
  version identifies the recipe, and a refresh rebuilds the same recipe with newer
  packages, so it needs no bump. Any edit to the Dockerfile or `entrypoint.sh` still
  needs one.

### Phase 5: Version policy and pins

- [ ] ⬜ **Task 5.1**: Write the policy into `CLAUDE/ContainerRules.md`:
  - distro packages float, refreshed by Phase 4
  - Claude Code, agent-browser, Chrome for Testing and the LSPs float on upstream
    `latest` at build time
  - Lightpanda stays pinned by sha256, because its silent-failure profile was measured
    on 0.3.6 (Plan 00097)
  - the PHPantom tag stays pinned for a reproducible source build
  - the project image's QA tools stay pinned via `.qa-versions`
- [ ] ⬜ **Task 5.2**: Register the Dockerfile pins (Lightpanda, PHPantom, and the project
  image's yq, gitleaks and ImageMagick if any survive Phase 3) in `vars/version-pins.yml`
  so `check-pinned-versions.bash` reports their drift. Extend `qa-version-pins.bash` if
  its "playbook declares var" check has to accept a Dockerfile `ARG`.

### Phase 6: Relative worktrees

- [ ] ⬜ **Task 6.1**: If the "no relative worktrees" rule has landed in
  `CLAUDE/Worktree.md` by then, add its lifting condition. Every git that opens the repo
  must be 2.48 or later: the host, and every ccy image on every machine that mounts a
  checkout, past the Phase 4 floor. Lift it only after `triage.bash` shows that on the
  host. Note what lifting gains: container worktrees then resolve on the host too (RESEARCH
  Section 6).

### Phase 7: Deploy and accept

- [ ] 🔄 **Task 7.1**: Write `deploy.bash` (runs `play-claude-yolo.yml`, then
  `acceptance.bash`) on `_planlib.inc.bash` (written for Phase 1). Add this plan to
  `CLAUDE/Plan/meta-deploy.bash` `PLANS` in the same commit as the change it deploys.
- [ ] ⬜ **Task 7.2**: Extend `acceptance.bash`:
  - the Phase 1 checks
  - container `git --version` meets the floor
  - a relative-worktree repository opens in the container
  - `agent-browser-headless` loads a page and screenshots it
  - `agent-browser-lite-headless` reads text
  - `agent-browser-headed` opens and closes a window, NOT ESTABLISHABLE without a
    Wayland session, so it is named for the owner
  - the LSP binaries exist
  - `ccy --refresh-images` leaves the version label unchanged and advances `built-at`
  - the project image builds
- [ ] ⬜ **Task 7.3**: `./scripts/qa-all.bash`, then the `qa-reviewer` agent over the full
  diff. Resolve every BLOCK and FIX-BEFORE-MERGE finding.

## Technical Decisions

### Decision 1: base image (owner decision D1)

**Context**: the container's git must be able to read a repository the host's git has
written to. Only Claude Code is updated in place today.

**Options considered**:

- **A, Debian**: the smallest change to the recipe. It cannot reach git 2.48 through any
  stable package. It needs a source-built git that nobody security-patches for us. It
  keeps the project image's three workarounds, and keeps drifting from the desktop.
- **B, Fedora at the host's release**: same distro and versions as the desktop. git
  2.56, ShellCheck 0.11 and IM7 come as packages, and yq and uv stop being network
  installs (wget, curl). Chrome for Testing ships `rpm.deps`. The cost: every apt-based project
  Dockerfile breaks, the docs and templates need a rewrite. Node is nvm (D3), so it
  does not follow Fedora.

**Decision**: B, Fedora (the owner, 2026-10-09). Debian was incidental, not chosen, and
fixing git on Debian means owning a source build, which is exactly the X.Y.Z maintenance
the owner does not want.

## Success Criteria

- [x] A repository the container's git cannot read stops the ccy launch, printing git's
  own message (acceptance).
- [ ] The container's git is 2.48 or later and opens a repository with
  `extensions.relativeWorktrees` (acceptance).
- [ ] A refresh rebuild changes no tracked file, needs no version bump, and advances
  `claude-yolo-built-at`.
- [ ] A build below a tool floor fails and leaves the previous image in use.
- [ ] Every pin left in the Dockerfiles has a stated reason and a `vars/version-pins.yml` row.
- [ ] QA passes (`./scripts/qa-all.bash`), and `qa-reviewer` reports no BLOCK.

## Risks & Mitigations

| Risk                                                                       | Impact | Probability | Mitigation                                                                                              |
| -------------------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------- |
| Every client project's apt-based Dockerfile fails after the Fedora switch  | H      | H           | Task 3.8 guard names the file and the fix; major container version; changelog; D6                       |
| Chromium headed/Wayland misbehaves on a Fedora userland                    | H      | L           | Chrome for Testing is distro-neutral and ships `rpm.deps`; Task 7.2 exercises all three modes           |
| A refresh pulls in a regression (new git, Node, Python)                    | M      | M           | Floors and the build checks fail the build and keep the old image; `--refresh-images` reports it        |
| nvm's Node is on a shell PATH only, so a non-interactive process misses it | M      | M           | Task 3.1 puts `node`/`npm` on the PATH every process sees, and checks it from a hook and the entrypoint |
| Python 3.14 breaks an image-side script                                    | L      | L           | The host already runs F44's Python for the same hooks-daemon and supervisor code                        |
| A refresh rebuild costs a Chrome download and minutes of build             | M      | H           | GitHub Actions builds it (D4, Task 4.4); the owner's machine pulls the result                           |
| The registry is down or the package is deleted                             | M      | L           | Task 4.6 builds locally when the pull fails; fails loud only with no usable image at all                |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00169-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan and research written (this commit).
- Phase 1 entrypoint git preflight, container 2.50 / CCY 3.91.0: `db5bfd39`.
