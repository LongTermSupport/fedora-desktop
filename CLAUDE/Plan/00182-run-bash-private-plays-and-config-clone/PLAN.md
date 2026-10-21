# Plan 00182: run.bash runs private plays, from a local clone of the config repo

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Medium

Addresses #49, #97 and #104.

## Overview

Some plays cannot be public: they leak something or depend on a private artefact. They live
in the private config repo (`<account>/fedora-desktop-config`) under `playbooks/`, where
nothing runs them. #49 asks `run.bash` to find those plays and run them all, unconditionally,
after the optional plays, in both the interactive and the headless flows. Two hard
constraints come with it. A private play's failure output must never reach
`create_github_issue`, which posts to this public tracker. And the privacy gate on the config
repo stays a hard requirement. Single-play mode (Plan 00114) restricts paths to
`<repo>/playbooks/*.yml` and would reject a private play. The repo name is built from four
separate literals today and should become one constant.

#97 proposes how the plays get onto disk: a working clone of the config repo at
`untracked/fedora-desktop-config/`, with `environment/localhost/host_vars/localhost.yml` a
symlink to the clone's `hosts/<hostname>.yml`. A change to the host's settings then becomes
an ordinary commit in the config repo instead of a `gh api` upload. Private plays are on disk
to run, and `untracked/` keeps everything out of the public index. It must not live under
`untracked/repos/`, which the hooks daemon governs as read-only reference clones. #97 lists
five inconsistencies to settle on the way, mostly in `fedora-install/push.bash`: a different
upload path, a README naming a script that does not exist, file content on argv, no `.private`
check, and a `projects.manifest` scan that would record the nested clone.

The two issues share the clone, the privacy gate and the repo-name constant, so they are one
plan: #97's clone is how #49 finds its plays. Plan 00159 also describes
`~/.config/fedora-desktop/` as a checkout of the private config repo. Whether that and this
clone are one checkout or two is settled before building.

## Goals

- One local working clone of the config repo, privacy-gated, with `localhost.yml` a symlink
  into it and changes committed and pushed with git.
- `run.bash` runs every private play after the optional plays, interactive and headless.
- No private play's output can reach a public GitHub issue.
- One constant for the config repo's name; `push.bash`'s five inconsistencies settled.

## Non-Goals

- A menu or per-play opt-in for private plays (#49: if one is present, it is wanted).
- Moving any public play into the config repo.
- Anything under `untracked/repos/`.

## Tasks

### Phase 1: Triage and decisions

- [ ] ⬜ **Task 1.1**: `triage.bash`: where `run.bash`, `push.bash` and `pull-projects.bash`
  read and write the config repo today (functions, not line numbers), and what the privacy
  gate checks.
- [ ] ⬜ **Task 1.2**: Owner decides: one clone or two (this and Plan 00159's
  `~/.config/fedora-desktop/`); how a host with no clone yet is bootstrapped; and how a
  private play's failure is reported instead of an issue.

### Phase 2: The clone (tests first)

- [ ] ⬜ **Task 2.1**: One constant for the config repo name; the four literals use it.
- [ ] ⬜ **Task 2.2**: Clone or update `untracked/fedora-desktop-config/` behind the privacy
  gate; symlink `localhost.yml`; refuse loudly on a non-private repo or a dirty clone.
- [ ] ⬜ **Task 2.3**: `push.bash`: one upload path (`hosts/<hostname>.yml`), the README's
  script name, content off argv, the `.private` check, the clone excluded from the
  `projects.manifest` scan.

### Phase 3: Private plays

- [ ] ⬜ **Task 3.1**: Interactive and headless flows enumerate and run the clone's
  `playbooks/*.yml` after the optional plays, without `show_menu`.
- [ ] ⬜ **Task 3.2**: The runner never offers or creates a GitHub issue for a private play.
  A test proves it.
- [ ] ⬜ **Task 3.3**: Single-play mode accepts a private play path inside the clone.
- [ ] ⬜ **Task 3.4**: `RUN_BASH_VERSION` minor bump, `docs/run-bash-changelog.md`, docs;
  `qa-all.bash`; the `qa-reviewer` agent.

### Phase 4: Automatic backup of localhost.yml (#104)

- [ ] ⬜ **Task 4.1**: Issue #104, "Back up localhost.yml to the private config repo
  automatically": every change to `localhost.yml` (now also written by `ccy-teams create`
  through `helpers/agent_bus/declare.py`) reaches the private config repo as a commit that
  says what changed, with no step from the owner. Phase 2's clone and symlink (#97) deliver
  the first half: the live file is the clone's working copy, so `git diff` shows every unsaved
  change, there is no copy to forget, the clone sits under `untracked/` (visible in the ccy
  container), and the privacy gate is checked before a push. Left for this task, after the
  owner picks from the issue's options: the commit-and-push trigger ((a) every writer commits
  through one shared command, (b) a systemd path unit after a quiet period, or (c)
  `meta-deploy.bash` and `run.bash` commit pending changes at start); a push that fails loudly
  with the primary account's token (Plan 00139); retiring the local
  `localhost.yml.backup.*` files once history exists; and a fresh install without the repo
  keeping today's plain file. Seats and team membership are out of scope (Plan 00197).

## Success Criteria

- [ ] A private play in the clone runs in both flows with no prompt.
- [ ] A failing private play's output never reaches a public issue (tested).
- [ ] `localhost.yml` edits show up as a diff in the clone and are pushed with git.
- [ ] No install-specific name appears anywhere in the diff.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00182-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #49 and #97.
