# Plan 00270: go repo split

**Status**: Not Started (design; Q2, Q4 and Q7 decided, Q1, Q3, Q5, Q6, Q8 and Q9 open, repository creation waits on the owner)
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
[INVENTORY.md](INVENTORY.md). The CI each repository gets is in [CI.md](CI.md). The owner
questions and answers are in [OWNER-QUESTIONS.md](OWNER-QUESTIONS.md).

## Goals

- Three repositories (names are placeholders until Q1), extracted leaves first. Each starts
  from one clean initial commit of the code as it stands (no history carried, Q2), with its
  own module path, tagged releases, the CI in [CI.md](CI.md) and the full hooks-daemon and
  planning setup from its first commits. Its first plan carries a `HANDOVER.md` that says
  where the code came from and how to read its history in fedora-desktop.
- The new repositories carry the v2 bus only, absolutely clean: no v1 bus code, no v1 parity
  tests, no v1 compatibility shims (Q4).
- fedora-desktop builds and installs the same commands from pinned versions, and every
  capability it has today still works. Once a carve-out is proven, fedora-desktop gets a plain
  deletion commit of the moved code. Its history is never rewritten or force-pushed.
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

- [ ] 🔄 **Task 0.1**: The owner answers Q1 to Q9 and creates the repositories (the
  team's open question 31). The answers are recorded in this plan's Technical Decisions.
  Q2, Q4 and Q7 are answered; the others are open.

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
- [ ] ⬜ **Task 1.5**: Keep the v1 bus out of ccy's tree (Q4). The v1 packages (`agentbus`,
  `bus`, `declare`, `marker`, `pingbus`, `cmd/pingbus-status-line`) are the group `stays-v1`
  in INVENTORY.md and stay in fedora-desktop. ccy-teams' edges to them (`teams` →
  `agentbus`, `declare`, `pingbus`; `teams/membership` → `pingbus/pyjson`; `lightning` →
  `bus/seat`, `bus/pycompat`) are cut: the seat install goes
  through the provisioner hook (Task 1.2), and the v1 reads are removed from the packages that
  move, or the code that needs them stays here. The boundary rule (Task 1.1) fails any import
  from a moving package into `stays-v1`. Removing v1 itself is not part of this task.
  - Acceptance for Phase 1: `go build ./...`, `./scripts/qa-changed.bash` on each commit,
    full `qa-all.bash` green on F44, the boundary rule blocking with an empty ratchet, and
    every command in `scripts/go-binary-suites.tsv` passing its suite unchanged.

### Phase 2: extract kit (the leaf)

- [ ] ⬜ **Task 2.1**: A freeze gate here: a commit touching a tree listed in
  `src-go/extracted-trees.tsv` fails, naming the repository it now lives in. It is proved red,
  and the row is added before the extraction.
- [ ] ⬜ **Task 2.2**: Clean import, no history (Q2). A plan-folder script `extract.bash kit`
  copies the repository's paths, as they stand at one named fedora-desktop commit, into a new
  empty repository under `untracked/split/kit/`, never this checkout. It runs the
  private-names scan and gitleaks over that tree, and fails before any commit if either
  finds something. The tree is then committed as one initial commit whose message names the
  source sha. No `git filter-repo`, no `git subtree`, and no step for a human to run.
- [ ] ⬜ **Task 2.3**: In the new repository, from its first commits: the module path
  (`github.com/LongTermSupport/<kit>`), imports rewritten by a Go tool over `go/ast` (never
  `sed`), and the CI in [CI.md](CI.md). The full hooks-daemon setup: the daemon installed, its
  config and handlers, and the planning workflow (`CLAUDE/Plan/`, `mkplan.bash`, plan QA), with
  a `CLAUDE.md` pointing at the shared rules. Then tag `v0.1.0`.
- [ ] ⬜ **Task 2.4**: The new repository's first plan carries `HANDOVER.md`: the fedora-desktop
  commit sha the code was taken from, the source paths and where each landed, and the exact
  commands to read its history in fedora-desktop (for example
  `git log --follow <sha> -- <path>` and `git blame <sha> -- <path>`, run in a fedora-desktop
  clone).
- [ ] ⬜ **Task 2.5**: Switch fedora-desktop over: `require` the pinned tag, `go mod vendor`,
  add a `go-deps.allowlist` row, and rewrite the imports with the same tool, while the old tree
  still sits beside it unused.
- [ ] ⬜ **Task 2.6**: Once the carve-out is proven (fedora-desktop builds from the new
  repository, no capability is lost, CI is green on F44 and F44-arm), one plain deletion commit
  removes the moved tree from fedora-desktop, and the freeze gate's row (Task 2.1) goes in with
  it. Never a force-push or a history rewrite.
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

- [ ] ⬜ **Task 4.1**: The Phase 2 steps for ccy's Go packages (v2 only: none of the
  `stays-v1` group, Task 1.5), `files/var/local/claude-yolo/`, `files/opt/claude-yolo/` (bar
  teamchat's optional tree), `docs/ccy*.md`, and the ccy project rules and gates
  (`ccyversionbase`, `lxcattach`, `nokill`). The repository requires kit and teamchat.
  Before the initial commit, `extract.bash` fails if the tree holds a v1 package, a v1 parity
  test (`bus/pycompat`, `pingbus/pyjson`) or a v1 name.
- [ ] ⬜ **Task 4.2**: Release payload (Q6): the binaries for both architectures plus a
  `ccy-<version>.tar.gz` of the image sources, bash launcher and plugins, signed and
  checksummed. `CCY_VERSION` becomes the release tag, and the version-bump rule moves with it.
- [ ] ⬜ **Task 4.3**: `play-claude-yolo.yml` and the freeze plays install the pinned release
  (the version and the checksum both pinned) in place of the checkout copy. ccy-teams'
  provisioner hook is set to this repository's agent-bus play.
  - Acceptance: every ccy verb, ccy-teams verb, the supervisor, restore across a reboot and
    the network policy pass the same checks as before the move, on both lines (F44 and
    F44-arm). The old checkout path is removed only after that, by Task 2.6's plain deletion
    commit.

### Phase 5: contained mode (#441), built in the ccy repository

- [ ] ⬜ **Task 5.1**: The launch seam `podman | contained`. Above the seam everything is
  shared code ([DESIGN-contained-mode.md](DESIGN-contained-mode.md) section 1).
- [ ] ⬜ **Task 5.2**: The three-part proof and its refusal (section 2), test-first, with a
  fixture per failing check. A plain host must be shown to refuse.
- [ ] ⬜ **Task 5.3**: Host-side network policy on the container's veth (Q7, approved:
  host nftables on the veth, written by this repository's LXC play), the inside
  probe, and the shared tool inventory manifest checked by `ccy doctor --contained`.
- [ ] ⬜ **Task 5.4**: The in-container `ccy-restore.service` and its order after
  `lxc@<name>.service` (Plan 00266 C.2). Resume is by conversation id (#431).

### Phase 6: migrate the LXC seats

- [ ] ⬜ **Task 6.1**: A throwaway seat, then the five declared seats, then the running one,
  each with its rollback (section 3). Each step is queued in `meta-deploy.list`.

### Phase 7: v1 retirement in fedora-desktop (gated, NOT approved)

- [ ] ⬜ **Task 7.1**: Prove nothing still uses the v1 bus here: no play, seat, command,
  test or doc reaches the `stays-v1` packages, `helpers/pingbus/` or the v1 plays. The proof is
  a plan-folder triage script, not a reading. Then tell the coordinator, and stop.
- [ ] ⬜ **Task 7.2**: Remove v1 from fedora-desktop, only if the owner approves it after
  Task 7.1's report. The owner has not approved it.

### Phase 8: close

- [ ] ⬜ **Task 8.1**: The `qa-reviewer` agent over the whole of fedora-desktop's diff, and
  over each new repository's first release.

## Dependencies

- Builds on GitHub issue #441 (its design is DESIGN-contained-mode.md) and #431.
- Meets Plan 00266 C.2 (`lxc@<name>.service`) in Task 5.4. Plan 00209's v1 retirement is not
  a dependency: the v1 bus stays in fedora-desktop (Q4), and Phase 7 is gated.
- In-flight teamchat plans (00266, 00267, 00269) keep landing in `src-go/internal/teamchat/`
  until Task 3.1's freeze. The freeze waits until they are quiet or finished.

## Open questions for the owner

Each question, its recommendation and the owner's verbatim answer are in
[OWNER-QUESTIONS.md](OWNER-QUESTIONS.md). State:

- **Decided** (owner, 2026-11-26): **Q2** history: no history split; one clean initial commit
  per repository, a `HANDOVER.md`, and a plain deletion commit here (Tasks 2.2 to 2.6). **Q4**
  v1 bus: the new repositories carry v2 only; v1 stays here, its removal not approved (Tasks
  1.5, 4.1, Phase 7). **Q7** network policy: approved as proposed (Task 5.3).
- **Open:** Q1 names, Q3 shared gates, Q5 how a host installs the Go commands, Q6 ccy's
  non-Go payload, Q8 teamchat apart from ccy, Q9 order of ccy and contained mode.

## Success Criteria

- [ ] Each new repository is green on its CI (both architectures) and has a signed release.
- [ ] Each new repository has the full hooks-daemon and planning setup, a first plan with
  `HANDOVER.md`, and no v1 bus code.
- [ ] fedora-desktop holds each moved tree's plain deletion commit, with no history rewritten.
- [ ] fedora-desktop builds every command from pinned versions, and `qa-all.bash` is green on
  F44 and F44-arm.
- [ ] Nothing in INVENTORY.md's "stays" group or any play lost a capability.
- [ ] An LXC seat runs ccy in contained mode, and a plain host refuses contained mode with the
  reason.
- [ ] Every existing LXC seat is migrated with its conversation kept.

## Risks & Mitigations

The risk table, with impact, probability and mitigation, is in [RISKS.md](RISKS.md). The two
high-impact ones: an install-specific name leaking into a new public repository, and contained
mode turning yolo on for a plain host.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00270-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Design drafted (this commit)
