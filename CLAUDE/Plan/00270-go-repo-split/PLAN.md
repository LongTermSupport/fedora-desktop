# Plan 00270: go repo split

**Status**: Not Started (design; owner questions Q1 to Q9 open, repository creation waits on the owner)
**Created**: 2026-11-26
**Owner**: dev
**Priority**: Medium

## Overview

The owner, verbatim: "I think it woudl be a gret time to etract the ccy stuff into a
standalone repo, and maybe some other fedora-desktop stuff as well? I'd suggest we extract
things that are in go, and then the move to go becomes more of an ectract and refactor at the
same time going forards. full go-qa-ci and proper CI etc for each repo."

This plan splits the Go code under `src-go/` into standalone public repositories under the
same GitHub organisation, each with full CI of its own. fedora-desktop keeps the Ansible plays
and the host-specific tools, and consumes the extracted code at pinned versions. It also
carries the design GitHub issue #441 asks for: ccy as a standalone repository, plus an
"already contained" mode in which an LXC container is the sandbox, so LXC seats run real ccy
with every verb ([DESIGN-contained-mode.md](DESIGN-contained-mode.md)).

This plan is design and tasks. Nothing is built until the owner answers the questions below.
The per-package inventory, the dependency graph and every edge to cut are in
[INVENTORY.md](INVENTORY.md). The CI each repository gets is in [CI.md](CI.md).

## Goals

- Three repositories (names are placeholders until Q1), extracted leaves first, each with
  its history, its own module path, tagged releases and the CI in [CI.md](CI.md).
- fedora-desktop builds and installs the same commands from pinned versions, and every
  capability it has today still works.
- ccy runs in an LXC seat in contained mode, proved and not assumed, with the existing seats
  migrated one at a time with a rollback ([DESIGN-contained-mode.md](DESIGN-contained-mode.md)).

## Non-Goals

- Extracting the desktop tools (`wsi`, `stt`, `rclone`, `nord`, `ssh-with-key` and the rest of
  INVENTORY.md's "stays" group). They are this host's build. They remain here and import kit.
- Porting the remaining bash to Go as part of a move. A port happens in the new repository
  later, under its own plan ("extract, then refactor").
- Any deploy to the laptop this weekend.

## The proposed repositories

| Placeholder            | What it is                                                                                                                                                                                                                                           | Kind                                                                                                                                               |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| **kit**                | the shared leaves: `execx`, `term`, `ui`, `stdinread`, `fsx`, `gitx`, `shellword`, `ttyecho`, `duration`, `ghlogin`, the generic halves of `sudo` and `systemd`, the clock seam, test helpers                                                        | library module only                                                                                                                                |
| **teamchat**           | the v2 agent-team bus: `internal/teamchat/**` (proto, member, admin, host), `cmd/teamchat`, `cmd/teamchat-admin`, the plugin and skill under `files/opt/claude-yolo/optional/teamchat/`, the v2 bus docs                                             | both: binaries (static, amd64 and arm64) and a library for ccy-teams (the public packages in INVENTORY.md)                                         |
| **ccy**                | the launcher (`claude-yolo` bash and Go front, `files/var/local/claude-yolo/`, the image, supervisor plugins, `files/opt/claude-yolo/`), sessions, registry, restore, conversation, transcript, ccy-teams, ccc, cc, the freeze tools, `docs/ccy*.md` | both: binaries and a release payload (the image sources and bash), plus a library for the few desktop tools that read `ccyenv` and `matrixclients` |
| go-qa-ci (exists)      | already a standalone repository, required by `src-go/qa/go.mod`                                                                                                                                                                                      | gains the generic project rules and repository gates (Q3)                                                                                          |
| fedora-desktop (stays) | every play, `tasks/`, `vars/`, `environment/`, `helpers/`, the host-specific tools, `src-go/qa`'s gates for this repository's own trees, the tests of the plays against the pinned releases                                                          | the host build                                                                                                                                     |

## How work proceeds

- **No new branch in this repository** while the parked pool is at its cap. Work here is
  committed and pushed to `ci/<topic>` (CLAUDE.md "QA Mandatory Before Commits"), then moved
  to F44 once CI is green.
- Work in a new repository is done in its clone under `untracked/split/<repo>/`, on that
  repository's own `ci/<topic>` flow.
- Creating the GitHub repositories waits on the owner (the team's open question 31). Phase 1 needs
  none of them.
- Every host run is queued in `CLAUDE/Plan/meta-deploy.list`. None is queued for this
  weekend.

## Tasks

### Phase 0: owner decisions

- [ ] ⬜ **Task 0.1**: The owner answers Q1 to Q9 and creates the repositories (the
  team's open question 31). The answers are recorded in this plan's Technical Decisions.

### Phase 1: cut the boundaries inside fedora-desktop (no repository needed, no behaviour change)

- [ ] ⬜ **Task 1.1**: Defence Before Fix for the class "an import across a planned repository
  boundary". A project rule (`project_repo_boundary`, `src-go/qa/rules`) reads the group of
  each package from one table and fails an import that runs against the order kit ←
  teamchat ← ccy ← stays. Prove it red on today's edges (`stdinread` → `teamchat/clock`), then
  record the known edges as the ratchet.
- [ ] ⬜ **Task 1.2**: Cut each edge in INVENTORY.md's "Cross-boundary edges" table, one
  commit per edge with its tests. The clock moves to kit. `systemd` and `sudo` are split.
  ccy-teams gets the provisioner hook in place of `main.RootDir`. `matrixclients`,
  `compactfloor` and `hostcheckout` take their values from config. Checkout-reading tests
  move to `testdata/`. When the ratchet is empty, the rule blocks.
- [ ] ⬜ **Task 1.3**: Make the teamchat packages ccy imports into teamchat's API: the list in
  INVENTORY.md. Each is documented as a stable surface, and the rest of teamchat stays
  unexported.
- [ ] ⬜ **Task 1.4**: Sort the 67 project rules (`qa/rules/rules_test.go` asserts the count) by scope (INVENTORY.md's qa table): generic,
  teamchat, ccy or this repository. Upstream the generic rules and gates into go-qa-ci (Q3).
  - Acceptance for Phase 1: `go build ./...`, `./scripts/qa-changed.bash` on each commit,
    full `qa-all.bash` green on F44, the boundary rule blocking with an empty ratchet, and
    every command in `scripts/go-binary-suites.tsv` passing its suite unchanged.

### Phase 2: extract kit (the leaf)

- [ ] ⬜ **Task 2.1**: A freeze gate here: a commit touching a tree listed in
  `src-go/extracted-trees.tsv` fails, naming the repository it now lives in. It is proved red,
  and the row is added before the extraction.
- [ ] ⬜ **Task 2.2**: History-preserving extraction, **run by the owner** (Q2). A plan-folder
  script `extract.bash kit` prints the `git filter-repo --path ... --path-rename ...` command
  for a fresh clone under `untracked/split/`, never this checkout, and the push to the new
  repository. `git subtree split` cannot follow these packages, because they sit under many
  prefixes and their history predates any common one.
- [ ] ⬜ **Task 2.3**: In the new repository: the module path (`github.com/LongTermSupport/<kit>`),
  imports rewritten by a Go tool over `go/ast` (never `sed`), `CLAUDE.md`, the hooks daemon,
  the CI in [CI.md](CI.md), and tag `v0.1.0`.
- [ ] ⬜ **Task 2.4**: Switch fedora-desktop over: delete the tree, `require` the pinned tag,
  `go mod vendor`, add a `go-deps.allowlist` row, rewrite the imports with the same tool.
  - Acceptance: the Phase 1 checks again, `go list -deps ./...` naming the kit module at the
    pinned tag, and a commit to the deleted path refused by the freeze gate.

### Phase 3: extract teamchat

- [ ] ⬜ **Task 3.1**: The Phase 2 steps for teamchat. The paths are `src-go/internal/teamchat/`,
  the two commands, the plugin and skill, the v2 bus docs, and the teamchat project rules and
  tagged stages. The repository requires kit.
- [ ] ⬜ **Task 3.2**: Release `v0.1.0`: static binaries for amd64 and arm64, `sha256sums.txt`,
  signed ([CI.md](CI.md)).
- [ ] ⬜ **Task 3.3**: fedora-desktop consumes it (Q5). `play-teamchat.yml` and the member kit
  build the commands from the pinned module through `tasks/build-go-command.yml`, as that task
  builds an external pinned module today (from the module proxy, `-mod=readonly`, checked by
  `go.sum`; `src-go/go.mod` has no `tool` directive). `teamchat/installplay`'s tests stay here and test the play against the
  pinned module.
  - Acceptance: the play's tests pass, `teamchat-cross-build` passes for both architectures,
    and a `meta-deploy.list` entry is queued (not this weekend). After deploy, the running
    team's seats answer `teamchat` verbs at the new version.

### Phase 4: extract ccy

- [ ] ⬜ **Task 4.1**: Decide the v1 bus first (Q4). Then run the Phase 2 steps for ccy's Go
  packages, `files/var/local/claude-yolo/`, `files/opt/claude-yolo/` (bar teamchat's
  optional tree), `docs/ccy*.md`, and the ccy project rules and gates (`ccyversionbase`,
  `lxcattach`, `nokill`). The repository requires kit and teamchat.
- [ ] ⬜ **Task 4.2**: Release payload (Q6): the binaries for both architectures plus a
  `ccy-<version>.tar.gz` of the image sources, bash launcher and plugins, signed and
  checksummed. `CCY_VERSION` becomes the release tag, and the version-bump rule moves with it.
- [ ] ⬜ **Task 4.3**: `play-claude-yolo.yml` and the freeze plays install the pinned release
  (the version and the checksum both pinned) in place of the checkout copy. ccy-teams'
  provisioner hook is set to this repository's agent-bus play.
  - Acceptance: every ccy verb, ccy-teams verb, the supervisor, restore across a reboot and
    the network policy pass the same checks as before the move, on both lines (F44 and
    F44-arm). The old checkout path is removed only after that.

### Phase 5: contained mode (#441), built in the ccy repository

- [ ] ⬜ **Task 5.1**: The launch seam `podman | contained`. Above the seam everything is
  shared code ([DESIGN-contained-mode.md](DESIGN-contained-mode.md) section 1).
- [ ] ⬜ **Task 5.2**: The three-part proof and its refusal (section 2), test-first, with a
  fixture per failing check. A plain host must be shown to refuse.
- [ ] ⬜ **Task 5.3**: Host-side network policy on the container's veth (Q7), the inside
  probe, and the shared tool inventory manifest checked by `ccy doctor --contained`.
- [ ] ⬜ **Task 5.4**: The in-container `ccy-restore.service` and its order after
  `lxc@<name>.service` (Plan 00266 C.2). Resume is by conversation id (#431).

### Phase 6: migrate the LXC seats

- [ ] ⬜ **Task 6.1**: A throwaway seat, then the five declared seats, then the running one,
  each with its rollback (section 3). Each step is queued in `meta-deploy.list`.

### Phase 7: close

- [ ] ⬜ **Task 7.1**: The `qa-reviewer` agent over the whole of fedora-desktop's diff, and
  over each new repository's first release.

## Dependencies

- Builds on GitHub issue #441 (its design is DESIGN-contained-mode.md) and #431.
- Meets Plan 00266 C.2 (`lxc@<name>.service`) in Task 5.4, and Plan 00209 (the v1 bus
  retirement) in Q4.
- In-flight teamchat plans (00266, 00267, 00269) keep landing in `src-go/internal/teamchat/`
  until Task 3.1's freeze. The freeze waits until they are quiet or finished.

## Open questions for the owner

- **Q1. Names.** Recommendation: `ccy`, `teamchat` and `lts-go-kit`, all public, MIT like this
  repository. Not `go-kit`: that is the name of a well-known Go project.
- **Q2. History.** Recommendation: full history by `git filter-repo`, run by you in a fresh
  clone from the command the plan script prints. The other option is a fresh import whose
  first commit names the source sha, which loses `git log` and `blame` in the new repository.
- **Q3. Shared gates** (bash limits, private names, push gate, branch hygiene, go-deps,
  toolchain). Recommendation: upstream them into go-qa-ci, which every repository already
  pins, rather than a fourth repository or a copy per repository.
- **Q4. The v1 bus** (`agentbus`, `bus`, `declare`, `marker`, `pingbus`, and `lightning`'s v1
  reads). Recommendation: retire it before ccy is extracted, so ccy does not carry Python
  parity tests against this repository. If that is far off, the v1 packages stay here behind
  the provisioner hook.
- **Q5. How a host installs the Go commands.** Recommendation: the plays build them from the
  pinned module (from the proxy, `-mod=readonly`, `go.sum`-checked: the same
  `tasks/build-go-command.yml` route an external module takes today). The signed release binaries are what an LXC or another host installs.
- **Q6. ccy's non-Go payload** (image sources, bash, plugins). Recommendation: a signed
  release tarball pinned by version and sha256 in the play, rather than a git submodule.
- **Q7. Who writes an LXC seat's network policy.** Recommendation: this repository's host-side
  LXC play, on the container's veth, because this host owns the policy. Not the other
  repository's provisioner, and never nftables inside the container.
- **Q8. teamchat inside ccy, or apart.** Recommendation: apart. The homeserver side has
  nothing to do with ccy, and the two release on different cadences.
- **Q9. Order of ccy and contained mode.** Recommendation: extract ccy first with no
  behaviour change (Phase 4), then build contained mode there (Phase 5). That way a fault
  points to one change.

## Success Criteria

- [ ] Each new repository is green on its CI (both architectures) and has a signed release.
- [ ] fedora-desktop builds every command from pinned versions, and `qa-all.bash` is green on
  F44 and F44-arm.
- [ ] Nothing in INVENTORY.md's "stays" group or any play lost a capability.
- [ ] An LXC seat runs ccy in contained mode, and a plain host refuses contained mode with the
  reason.
- [ ] Every existing LXC seat is migrated with its conversation kept.

## Risks & Mitigations

| Risk                                                                  | Impact | Probability | Mitigation                                                                                                   |
| --------------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------ |
| An install-specific name leaks into a new public repository's history | H      | M           | the private-names scan over the filtered history **before** the first push (Task 2.2's script), and gitleaks |
| Concurrent teamchat work lands in a tree just extracted               | M      | H           | the freeze gate (Task 2.1) refuses it and names the new repository                                           |
| A pinned version drifts behind a fix                                  | M      | M           | the pins join `scripts/check-pinned-versions.bash` (the update-versions skill)                               |
| Contained mode turns yolo on for a host                               | H      | L           | three independent checks with no override, and a plain-host refusal test (Task 5.2)                          |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00270-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Design drafted (this commit)
