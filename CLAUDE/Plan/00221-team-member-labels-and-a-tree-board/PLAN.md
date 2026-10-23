# Plan 00221: team member labels and a tree board

**Status**: Not Started (plan only; owner decisions OA1-OA4 open, defaults stated)
**Created**: 2026-10-23
**Owner**: joseph
**Priority**: Medium

## Overview

The owner: "currently we can only select predefined names/roles. This is fine and stable, but
it would be nice to be able to give them labels, for human and agent visibility, to give some
idea what the worker is for. The coordinator should not be labelled, but workers could be:
theme, area, plan, whatever. E.g. a mono repo might contain infra, front and back end - that
might fit 3 team members, but seat number 1,2,3 tells us nothing. Arbitrary labels would be
nice, or perhaps predefined labels in ccy.env / ccy.env.local."

And on `ccy-teams`: "the UX is not brilliant - I wonder if the Go stuff faithfully reproduced
the bash interface instead of something slicker? In particular the UX should be more tree like:
TEAM / coordinator: / workers: / 1 (infra) projectX.1.desktop / 2 (front) projectX.2.podman".
It did: the board is the bash tool's flat fzf table carried into Go (Plan 00204 RESEARCH 1.2).
Later, with a screenshot: every agent seat shows `ccy`, a `cc` session on the host included;
"cc on the host and ccy in podman must show differently", and colour should make kinds and
states clearer. The mislabel itself is being fixed now in v1 by a separate worker; this plan
designs the presentation on the corrected data.

This plan gives each worker seat an optional, owner-set **label** (a short tag such as `infra`),
shown to humans and agents wherever the seat is shown, and redraws `ccy-teams` as a **tree**:
team, then its coordinator, its workers (seat, label, handle, where it runs) and its people, in
colour that reads on a dark terminal and loses nothing without it. The design, with every
expectation numbered, is [DESIGN.md](DESIGN.md).

## Goals

- A worker seat can carry one label per team, from suggestions or typed (DESIGN L1-L10); the
  coordinator never has one (L3).
- The label is set where the seat and role are set: the seat question of `ccy --teams` and
  `cc --teams`, `ccy-teams`, and lxc-add (L16-L18).
- It is shown on the status line, in `pingbus status`, in each envelope from the seat, in the
  launch block and on the board (L19-L24), and the agent is told its own label (L25-L27).
- `ccy-teams` is a tree with keyboard and drawn controls (T1-T9), built once as a model that
  fzf renders now and Plan 00204's frame renders later (T10), for v1 and v2 alike (T13).
- Each agent line says where it runs (`ccy (podman)`, `cc (host)`, `lxc`, ...), and kinds,
  states and teams are coloured from measured palettes, every colour also a word (T14-T17).
- `ccy-teams --list` and every unlabelled status line keep today's bytes (T12, L19).

## Non-Goals

- Labels for the coordinator, for humans, or set by an agent (L3, L6).
- Addressing a member by label (`--to infra`): a label is display only, never identity.
- Changing handles or named seats (`infra@<team>` already exists and stays identity).
- Fixing the KIND mislabel of `cc` seats: a separate v1 fix, taken as given here (T14).
- The Bubble Tea frame itself: Plan 00204 Tasks 3.1 and 4.1.
- Plan 00209's files: its requirements are listed below for it to take.

## Dependencies

- **Plan 00209 (v2), being built by another seat.** DESIGN section 4 lists which v1 parts v2
  replaces (store, route, LXC key, board read, renderers) and which carry over (grammar, rules,
  flows, text, the tree). Requirements for 00209, to be taken when it builds each part: the
  roster member key `label` (A 5.1, informational under A 3.1), `teamchat-admin member label`
  (C 3.6.3), `teamchat_lxc_members[].label` (C 3.8), `status.json` `members[].label`
  (C 3.11.3; its `kind` already gives T14's ENV), B27's status line piece, the envelope line
  and the SessionStart line (the envelope's `label:` after v2's `role:` line, A 13.2), the
  migration's carry of each label (D 5), `labels:` in `teamchat_teams[]` (C 3.8), and the
  board's `system` word on the tree's team line (D 3.9). Nothing here edits 00209's files.
- **Plan 00209 Task 7.1 (stage 1, in progress)** is porting `declare.py`'s host_vars reader to
  Go and keeps the `agent-bus list` call: the reader must learn `labels:` (L8) and the list's
  appended field must survive it (L15). It is the one 00209 task changing v1 code this plan
  edits; Task 1.3 is coordinated with its owner seat.
- **The v1 KIND fix** (separate worker the coordinator assigned, not on this branch): T14
  reads its corrected row data; Task 5.2 starts after it merges.
- **Plan 00204 (Go TUI).** The tree is the list screen of its Task 4.1; this plan's model
  (T10) and hue table (T17) are what that screen draws.
- **Plan 00217 (team colours).** The label route copies its colour route; the status line
  piece extends its C15; T15 reuses its measured palette.
- **Plan 00213.** The status line's parity oracle is re-recorded from the Python.
- **Plan 00222 (worker tool restrictions)** adds `worker_blocked_tools` to `agent_bus_teams`,
  the same three readers' change as Task 1.3, and an admin room-state event of the same
  shape: whichever plan builds first owns the shared reader change, the other depends on it.

## Tasks

Every build task is test first: the failing test is committed or shown before the code.

### Phase 0: Decisions

- [ ] ⬜ **Task 0.1**: The owner answers OA1-OA4 (or the defaults stand); record each in the
  journal and adjust the tasks (OA1 "wait" drops Phase 2's v1-only parts, DESIGN section 4).

### Phase 1: The label rules

- [ ] ⬜ **Task 1.1**: The grammar and fold (L1) in `helpers/pingbus/protocol.py` and a Go copy
  beside the status line, with a parity test (00217 C6's pattern).
- [ ] ⬜ **Task 1.2**: `CCY_TEAM_LABELS` parsed (not sourced) by the host-side chooser as
  suggestions (L9, L10): tests for the exact line, a refused form named, a bad item named and
  skipped.
- [ ] ⬜ **Task 1.3**: The team's `labels:` list (L8) taught to every host_vars reader before
  `localhost.yml.dist` shows it: `teamfile.py`, `declare.py` (read and `EDITABLE_KEYS`), and
  00209 Task 7.1's Go line reader, with a test in each that an entry carrying the key is read,
  not refused. Shared with Plan 00222's `worker_blocked_tools` (Dependencies).

### Phase 2: Store and route (v1; replaced by v2, DESIGN section 4)

- [ ] ⬜ **Task 2.1**: Registry version 3 `labels`, loading version 2 (L11); `set-label`, the
  coordinator refusal and the clear on promotion (L3, L12), with registry and CLI tests.
- [ ] ⬜ **Task 2.2**: `agent_bus_lxc_members[].label`, applied each run, refused with
  `role: coordinator` (L12).
- [ ] ⬜ **Task 2.3**: The admin keeps `agent_bus.labels` (L13); the syncer reads it and keeps
  `labels.json` (L14); the protocol spec §8 names it; `agent-bus list` gains `label` (L15).

### Phase 3: Shown and told

- [ ] ⬜ **Task 3.1**: The status line piece in Python and Go, the oracle re-recorded (L19).
- [ ] ⬜ **Task 3.2**: `pingbus status` (L20), the envelope's `label:` line with
  SPEC-inject's format updated (L21), the launch block (L22).
- [ ] ⬜ **Task 3.3**: The SessionStart line and the change notice (L25, L26).

### Phase 4: Setting it

- [ ] ⬜ **Task 4.1**: The seat question's label step (L16), with chooser tests for the
  defaults, the suggestions and "no answer, no change".
- [ ] ⬜ **Task 4.2**: `ccy-teams` Set label, `session label`, lxc-add's step (L17, L18).

### Phase 5: The tree board

- [ ] ⬜ **Task 5.1**: `board.Tree` from the snapshot rows, its groups, empty lines and actions
  (T1-T5, T10), model tests first.
- [ ] ⬜ **Task 5.2**: ENV from the corrected row data (T14), a table test over every handle
  type and the ccy/cc split, `unknown` for a row that cannot say.
- [ ] ⬜ **Task 5.3**: The hue table and the painter (T15-T17): a test that stripping the codes
  from a coloured render gives the `NO_COLOR` render, and that every hue is one of 00217's
  measured palette.
- [ ] ⬜ **Task 5.4**: The fzf rendering: every team expanded, hidden searchable fields, the
  header's actions (T6, T9's fzf half); every current menu and shortcut reachable (T11),
  proven by the existing picker tests passing against the tree. Expand, collapse and
  parent-keeping filter are 00204 Task 4.1's.
- [ ] ⬜ **Task 5.5**: `--list` unchanged without labels (T12): the F44 build at the branch
  point and the new one over one fixture, compared byte for byte as 00209 stage 1's
  `check_list` does; 00201's D1 reader passes.
- [ ] ⬜ **Task 5.6**: docs/agent-bus.md (Getting started, seats, the status line; the seats
  sentence "nothing about the bus goes in `ccy.env.local`" reworded for L9), the entrypoint's
  `PINGBUS_*` refusal message that quotes it (a `files/var/local/claude-yolo/` edit, so the
  CCY version bump), and the picker's help text.

### Phase 6: Deploy and review

- [ ] ⬜ **Task 6.1**: The version bumps the pre-commit hook asks for; the meta-deploy entry
  (play-agent-bus.yml for the admin, pingbus and the kit; play-claude-yolo.yml for ccy-teams;
  the ccy image takes the new pingbus at its next build).
- [ ] ⬜ **Task 6.2**: Adversarial review of the diff until MERGE, then `qa-reviewer`.
- [ ] ⬜ **Task 6.3**: On the host: label a worker from `ccy-teams`; its status line, a peer's
  envelope and `pingbus status` show it without a restart; the tree shows it; promoting it
  clears it; a `cc --teams` seat shows `cc (host)` beside `ccy (podman)` seats, in colour and
  under `NO_COLOR`.

## Decisions

### Owner only

| ID  | Decision                                                       | Default (recommended)                                                                                                                                                                                                                                     |
| --- | -------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| OA1 | Labels in v1 now, or only when v2 lands                        | **v1 now**: teams stay on v1 until each is migrated; the v1-only parts (DESIGN section 4) are small, of the colour route's known shape, and their end is stated                                                                                           |
| OA2 | Predefined labels in `ccy.env` / `ccy.env.local`               | **Yes, as suggestions only** (L9), one exact line parsed (never sourced) on the host, never applied by themselves; plus the team's `labels:` in host_vars (L8). It relaxes "nothing about the bus in ccy.env.local" for suggestions alone                 |
| OA3 | The tree on fzf now, or wait for Plan 00204's Bubble Tea frame | **fzf now, frame later** from the same model (T10): the owner sees the tree (always expanded, flat filter) without waiting for 00204 Tasks 3.1 and 4.1; collapse and parent-keeping filter come with the frame; only the thin fzf renderer is thrown away |
| OA4 | Free text, or only from a predefined set                       | **Both**: suggestions first, typed text always allowed (L7)                                                                                                                                                                                               |

### Coordinator-defaultable

| ID  | Decision                             | Default                                                                                              |
| --- | ------------------------------------ | ---------------------------------------------------------------------------------------------------- |
| CA1 | Grammar and length                   | `[a-z0-9-]`, 1-16, lower-cased input (L1)                                                            |
| CA2 | A labelled worker made coordinator   | the label is cleared, said on the confirm screen (L3)                                                |
| CA3 | Display forms                        | status line `[infra]`; envelope `label:` line; status `label=` field (L19-L21)                       |
| CA4 | Where the label is told to the agent | SessionStart line and change notice, fixed templates (L25, L26)                                      |
| CA5 | `--list`                             | stays the table; label inside MEMBER only when set (T12, L24)                                        |
| CA6 | Expansion at start                   | covering teams expanded, others collapsed (T8)                                                       |
| CA7 | Label in Element display names       | no (L23)                                                                                             |
| CA8 | ENV words and hues                   | `ccy (podman)` teal, `cc (host)` magenta, `lxc` blue, `docker`/`vm` indigo, `host` violet (T14, T15) |

## Success Criteria

- [ ] A worker labelled `infra` shows `[infra]` on its status line, `label: infra` in every
  envelope it sends, and in `pingbus status`, `ccy-teams` and the launch block; its own
  session is told the label.
- [ ] The coordinator cannot be labelled, and promotion clears a label.
- [ ] `ccy-teams` shows team → coordinator → workers (seat, label, handle, ENV) → people, with
  every existing action reachable; `cc (host)` and `ccy (podman)` read differently with and
  without colour; `--list` bytes unchanged for an unlabelled team.
- [ ] `qa-changed.bash` passes on each commit; review at MERGE; `qa-reviewer` passes.

## Handoffs

- The 00209 requirements under Dependencies go to 00209's owner seat through the coordinator;
  this plan cannot take them into 00209's files itself.

## Delivery & Milestones

- Plan and design: branch `agent-a73de92448810ff4c-98c4ef93`.
