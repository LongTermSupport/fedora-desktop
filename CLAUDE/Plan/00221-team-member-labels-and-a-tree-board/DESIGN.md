# Plan 00221 design: worker labels, and ccy-teams as a tree

This is the design that [PLAN.md](PLAN.md)'s tasks build. Each expectation has a number (L1...
for labels, T1... for the tree board) so tests and reviews can cite it. Wherever a part is v1
only and v2 (Plan 00209, `teamchat`) replaces it, the text says so (section 4).

Sources read: [docs/agent-bus.md](../../../docs/agent-bus.md) (seats, roles, the status line),
[agent-bus-protocol.md](../../../docs/agent-bus-protocol.md) §3 (identifiers), §8 (the team
record, the description and the colour as admin room state) and §15 (the delivery envelope);
Plan 00217's [SPEC-team-colours.md](../00217-team-colours-in-the-status-line/SPEC-team-colours.md)
(the route a display-only team fact takes); Plans 00177, 00201 and 00203 (the board, the seat
questions, hot control); Plan 00204's [RESEARCH.md](../00204-go-tui-polish/RESEARCH.md) section 4
(the Bubble Tea frame); Plan 00209's DESIGN-v2 chapters A (2.3 handles, 3.1 versioning, 5.1 the
roster), B (1.5 B27 the status line) and C (3.6.3 members, 3.8 host_vars, 3.11.3 the status
record, 3.13 what ccy-teams reads), as they stand on `feat/bus-v2-n3` at e3b6c30b4 and on F44.

## 1. What exists today

- A member is named by its handle, `<repo>.<seat>+<host>.<type>`. `<seat>` is a number or a
  name (`[a-z][a-z0-9]{0,11}`, protocol §3), so `ccy --teams infra@<team>` already gives a
  seat the handle `projectx.infra+local.podman`. That is identity, not a label: it is fixed for
  the life of the member (a new name is a new account with no history), it is part of every
  reply command, and handles reach public forge text. A label is the opposite: a mutable,
  display-only note on an existing seat, and nothing ever addresses a member by it.
- Each member has a role (`coordinator` or `worker`), kept by root in the team's registry
  (`/var/lib/agent-bus/<team>/registry.json`, version 2), set by `sudo agent-bus set-role`
  (from `ccy-teams` or the seat question of `ccy --teams`), published in the `agent_bus.team`
  record, and told to the session at SessionStart and by the role notice.
- The `agent_bus.team` record has an exact key set: a member that meets an unknown key distrusts
  the team. Display-only facts therefore travel apart from it as admin-written room state that
  an older member ignores: the description (`m.room.topic`, Plan 00201) and the colour
  (`agent_bus.colour`, Plan 00217).
- `ccy-teams` is a flat fzf table (`src-go/internal/teams/board/table.go`): one row per team,
  seat, member or person, columns TEAM MEMBER KIND STATE APPLIED SESSION RESTORE WHERE, a menu
  per row on Enter. It is the bash tool's layout carried into Go (Plan 00204 RESEARCH 1.2):
  nobody designed it. `ccy-teams --list` prints the same table; Plans 00201, 00203 and 00209's
  acceptance scripts read it, and 00209's stage-1 acceptance compares its bytes with an older
  build.

## 2. Labels

### 2.1 What a label is

- **L1 Grammar.** 1 to 16 characters of `[a-z0-9-]`, starting and ending with a letter or digit
  (`^[a-z0-9](?:[a-z0-9-]{0,14}[a-z0-9])?$`). Typed input is folded to lower case; a space or
  `_` is refused with the suggestion of `-` (`front end` → "use front-end"). Printable ASCII, so
  it passes every envelope and hook template's grammar check, adds a known width to the status
  line, and cannot carry an instruction of any length.
- **L2 One per seat per team.** A label belongs to a member (a handle) in one team. A session
  on two teams may carry two labels, or one, or none.
- **L3 Workers only.** A coordinator has no label. `set-label` refuses one ("the coordinator
  has no label: it is the team's coordinator"). Making a labelled worker the coordinator clears
  its label, and the confirm screen says so before it happens ("fedora-desktop.2's label infra
  is cleared"). Making the coordinator a worker sets no label; the flow offers one.
- **L4 Parked and removed.** A parked member keeps its label (it keeps its role too); a removed
  member's label goes with it.
- **L5 Team-internal.** A label is shown to the team's humans and agents only. pingbus never
  writes it to the forge, and the agent's context says it is the owner's internal note, so it
  is not repeated in public text (it may name a client or a project the forge must not see).
  That sentence is the only control on what the agent itself writes; L1's grammar bounds what
  a leak could cost (16 lower-case characters, no instruction), and a team that uses client
  names as labels accepts that.
- **L6 Owner-set only.** Only the owner sets a label, through the host (sections 2.3, 2.4).
  There is no pingbus verb for it, so an agent cannot label itself or another.

### 2.2 Predefined label sets

- **L7 Suggestions, never a gate.** Wherever a label is asked for, the suggestions are offered
  first and free text (L1) is always allowed.
- **L8 The team's set** is an optional `labels:` list in the team's `agent_bus_teams` entry
  (host_vars), each item L1's grammar, at most 16, no duplicates; checked by the team file's
  rules (`teamfile.parse_team_file`), so the play and `ccy-teams` refuse the same values. It is
  a suggestion list only: it is not published to members and grants nothing. **Three readers
  must learn the key before any entry carries it**, or the board stops drawing the team:
  `teamfile.py`, `declare.py`'s line reader and write side (`EDITABLE_KEYS`), and the Go port
  of that reader on 00209 Task 7.1 (`internal/bus/declare`, SPEC-view V3-V8, which refuses an
  unknown shape). Plan 00222's `worker_blocked_tools` needs the same three; whichever plan
  builds first owns the shared change (one task), the other depends on it.
- **L9 The checkout's set** (owner decision OA2): `CCY_TEAM_LABELS="infra front back"` in the
  checkout's `.claude/ccy/ccy.env` (committed, shared by everyone who works in the repository:
  the mono-repo case) or `ccy.env.local` (this machine). Both are bash files that only the
  container's entrypoint sources; the host never sources them, and this keeps it so. The
  host-side seat question (`ccy-teams choose-teams` and `ccy-teams`' Set label, Go) **parses**
  one line without executing anything: exactly `^(export )?CCY_TEAM_LABELS="<labels>"$`,
  labels separated by single spaces. Any other line naming `CCY_TEAM_LABELS` (two of them, a
  substitution, single quotes) is named on stderr and the file gives no suggestions. Each label
  is re-checked by L1 (a bad one is named and skipped, never applied). They are suggestions
  only: a checkout file can be written by the sessions in that checkout, so nothing in it is
  applied without the owner choosing it. Inside the container the variable is set and unused.
  This relaxes "nothing about the bus goes in `ccy.env.local`" (`docs/agent-bus.md`, seats)
  for suggestions alone; that sentence and the entrypoint's `PINGBUS_*` refusal message,
  which quotes the rule, are reworded (Task 5.6); the refusal itself is unchanged, and
  `CCY_TEAM_LABELS` sets no seat, no team and no label by itself.
- **L10 Order.** The checkout's set first (it describes this repository), then the team's set,
  without repeats, then labels already used in the team.

### 2.3 Where a label is stored and how it travels (v1)

- **L11 The store** is the team's registry, root-owned, beside the roles: version 3 adds
  `labels`, a map of handle to label, at most one entry per member, only for workers (L3). A
  version 2 file loads as version 3 with no labels. `update_registry`'s lock covers every
  change, as for roles.
- **L12 Setting it.** `sudo agent-bus set-label <team> <handle> --label=<label>` and
  `sudo agent-bus set-label <team> <handle> --clear`, password sudo as `set-role` is. It refuses
  a coordinator (L3), a removed member, a bad label (L1). `set-role --role=coordinator` on a
  labelled member clears the label in the same registry update (L3). A member declared in
  `agent_bus_lxc_members` takes the entry's optional `label:` on every play run, as its role
  does; `label:` on an entry with `role: coordinator` is refused by the play.
- **L13 Publishing it.** The admin keeps the main room's `agent_bus.labels` state event (key
  `""`) at `{"labels": {"<user ID>": "<label>", ...}}`, every labelled member of the registry,
  or `{}` when none is labelled and the event exists. It writes only on a change and reports
  `labels`. As with the colour (00217 C8): `state_default` 100 lets only the admin write it, it
  is not part of the team record, and a change restarts no homeserver.
- **L14 Reading it.** A member's syncer reads the event from the admin alone, when it verifies
  the room and when a sync carries that type. An event from anyone else, or whose content is
  not exactly that shape with every key a user ID of this server and every value L1's grammar,
  is no labels, never a loss of trust. It keeps `<bundle>/state/labels.json`
  (`{"v": 1, "labels": {...}}`, mode 0600, atomic; removed with no labels and on a loss of
  trust). A pingbus older than this ignores the event type: it shows no labels, nothing else
  changes.
- **L15 The board** reads labels where it reads roles: `sudo -n agent-bus list <team>` gains a
  `label` field per member (fields only appended), so the read-only sudoers rule is unchanged.
  That call is kept by 00209's stage 1 (Task 7.1, SPEC-view, "one Python call is kept"); the
  in-process reader SPEC-view's stage 4 adds later, and v2's `status.json`, replace it, and
  each must carry the label (section 4).

### 2.4 Where it is set (the owner's flows)

- **L16 At seat take.** `ccy --teams` and `cc --teams`, after the coordinator question, ask
  "Label for <handle> (what this worker is for)": the suggestions of L10, then "Type a label",
  then "No label" (the default for a seat with none) or "Keep <label>" (the default for one
  that has one). No sudo unless the answer changes the label, as for the role. Not asked of
  the coordinator. A launch with nobody to answer (headless, restore, restart) asks nothing and
  keeps the label. `<seat>@<team>:<label>` on the command line is not added (YAGNI): the label
  is a property of the seat, kept across launches, so it is asked once.
- **L17 In ccy-teams.** A worker's row menu has **Set label…** (suggestions, type, or Clear),
  with a shortcut drawn beside it (InteractiveScripts rule 14). The Running sessions view's
  per-team actions gain the same. `ccy-teams session label <session> <team> <label>|--clear`
  is the scriptable form, as `session role` is for roles.
- **L18 In lxc-add.** The Role step is followed by an optional Label step for a worker, written
  to the entry's `label:`.

### 2.5 Where it is shown

- **L19 Status line** (both implementations, Plan 00213's parity oracle re-recorded):
  `alpha: fedora-desktop.2 [infra] (w)`; the label in square brackets between the member and
  its role, in the team's colour, part of the team's piece so it wraps whole (00217 C15, C17).
  No label, no brackets: the bytes of an unlabelled piece are unchanged.
- **L20 pingbus status** prints a `label=<label>` field at the end of this member's TEAM line
  (fields are only ever appended), and the member listing shows each member's label.
- **L21 The envelope.** An entry from a labelled agent carries `label: <label>` on the line
  after `from:` (v1). In v2's entry, whose lines are `kind:`, `from:`, `role:`, `team:` (A
  13.2), it goes after `role:`. The first line of the delivery is unchanged. A human's entries never have one.
- **L22 The launch block** (which teams the session is on, with seat, role and what each is
  for) adds `label infra` after the role.
- **L23 Element.** No change: display names are handles, set by the admin only (protocol §3);
  a label in the display name would change a name humans mention by.
- **L24 ccy-teams.** In the tree (T3) and, for `--list`, inside the MEMBER cell after the
  handle (`projectx.2+local.podman [infra]`), so a table with no label has exactly today's
  bytes (T12).

### 2.6 How the agent sees it

- **L25 At SessionStart**, for each team where this member is a labelled worker, one fixed
  template line: `agent-bus: in team {team} the owner labelled this seat {label}: a short note of what this worker is for. Keep to that area unless the coordinator asks otherwise; the label is internal to the team, so do not put it in commits, pull requests or issues.`
- **L26 On a change**, the same line (or `agent-bus: in team {team} this seat's label {label} was cleared`) through the role notice's two paths (the membership envelope, else the next
  UserPromptSubmit), recorded in `told.json` / `told-roles.json` so it is told once.
- **L27 Its teammates' labels** are in `pingbus status` (L20) and in each envelope (L21), so the
  coordinator can dispatch by area without a lookup.

## 3. The tree board

### 3.1 The shape

The owner's sketch, made real:

```text
╭─ ccy-teams ─────────────────────────────────────────────────────────────────────╮
│ Teams                                                                           │
│ 2 teams · 6 members · in ~/work/projectx                                        │
├─────────────────────────────────────────────────────────────────────────────────┤
│ ▾ alpha        up · up to date · port 8448          Builds the projectx site    │
│   ├ coordinator                                                                 │
│   │  └ projectx.3+local.podman          ccy (podman)  held   projectx-3         │
│   ├ workers                                                                     │
│   │  ├ 1  infra   projectx.1+local.podman    ccy (podman)  held   projectx-1    │
│   │  ├ 2  front   projectx.2+local.podman    ccy (podman)  free                 │
│   │  ├ 5  docs    projectx.5+local.host      cc (host)     held   cc-projectx   │
│   │  └ 4  ·       projectx.4+site-b.lxc      lxc           active               │
│   └ people                                                                      │
│      └ owner                            in room · 2 signed in                   │
│ ▸ beta         down · out of date (config)          Reviews the docs            │
│   Running sessions (3)                                                          │
├─────────────────────────────────────────────────────────────────────────────────┤
│ 2 front: [ Set label  L ] [ New session  ^N ] [ More…  Enter ] [ Help ? ] [ q ] │
╰─────────────────────────────────────────────────────────────────────────────────╯
```

- **T1 Levels.** Team, then three fixed groups in this order: coordinator (zero or one member),
  workers, people. A group with nothing in it shows one dim line (`no coordinator: Make a worker the coordinator`, from the team's menu; `no workers`; `no people`), so its place never moves.
- **T2 The team line** keeps today's facts: homeserver state, APPLIED, port, description. Once
  v2 is installed beside v1, it also names the team's system (`agent-bus` or `teamchat`), the
  word 00209's board adds as a column (D 3.9).
- **T3 A worker line** is `<seat>  <label or ·>  <handle>  <environment>  <state>  <session>`
  (the environment is section 3.4's ENV), the seat
  first (the owner's `1 (infra) projectx.1...`), the label in its own column so labels line up.
  A member with no checkout seat (an LXC agent) shows its handle's seat part. The coordinator
  line has no seat or label column.
- **T4 Restore, Where and parked** move to the line's detail (the context band and Help, Plan
  00204 RESEARCH 4.5), so the line is never wider than the terminal; nothing is cut (the
  existing rule): on a narrow terminal the session and kind go to the detail first.
- **T5 Running sessions** stays the last line (and Tab), as today.

### 3.2 Keys and controls

fzf is one process over a fixed list of rows that it filters row by row, with no tree. So the
keys come in two steps, and each says which:

- **T6** ↑/↓ move by line; Enter opens the line's menu, unchanged in content (fzf and frame).
  Expand and collapse (→/← on a team, ← to a line's parent) are the frame's only (Plan 00204
  Task 4.1): on fzf every team is drawn expanded, so nothing needs a relaunch and the picker's
  redraw cost (`board/redraw_cost_test.go`) is unchanged. Every key is also a drawn control:
  the frame's footer is the selected line's actions (Plan 00204 4.1, rule 14), with the
  commonest first; on fzf the header names them, as today.
- **T7** Every shortcut of today keeps its meaning (Ctrl-N, Ctrl-U, Ctrl-R, Ctrl-X, Ctrl-P,
  Ctrl-E, Tab, `?`, Esc/q). **L** is Set label on a worker line; it is in that line's menu and
  footer and nowhere else.
- **T8** In the frame, teams that cover the folder ccy-teams runs in are expanded at start, the
  others collapsed; with one team, it is expanded. Expansion is not remembered between runs.
- **T9** Typing filters. On fzf each member line carries its team, label and handle in its
  hidden, searchable fields (`ui.PickSpec` Tags, fzf `--with-nth=1`), so `infra` or `alpha`
  finds the right workers, but a matched line's team and group lines are not kept. The frame
  keeps each match's parents.

### 3.3 What is kept, and how it is built

- **T10 One model, two renderers.** A pure `board.Tree` built from the `board.Snapshot` rows
  (team → groups → lines), with the line's actions, tested without a terminal. Rendered first
  on fzf (indented lines with hidden tags, which `ui.PickSpec` already supports), and later on
  Plan 00204's Bubble Tea frame (its Task 3.1 renderer and Task 4.1), with no change to the
  model (owner decision OA3).
- **T11 What is kept.** Every row's menu, every flow behind it (create, apply, settings,
  lxc-add, seats, people, wind-down, shutdown, restart, Running sessions), status colours, the
  `need sudo` handling, `NO_COLOR`, the offers made on the way in.
- **T12 `--list` stays the table** that scripts and the acceptance suites of Plans 00201, 00203
  and 00209 read, its bytes unchanged for a team with no labels (L24), measured as 00209
  stage 1's `check_list` does: the F44 build at this plan's branch point and the new build,
  over the same fixture, compared byte for byte. The pin holds until 00209 Task 4.3 adds its
  `system` column (D 3.9), which changes `--list` for every team. The tree is the interactive
  board only.
- **T13 Both systems.** v2's board (Plan 00209 Task 4.3) reads its rows from the status records
  (C 3.13); it feeds the same `board.Tree`, so the tree is built once for both systems.

### 3.4 Where a member runs, and colour

The owner, with a screenshot of today's board: every agent seat shows KIND `ccy`, a `cc`
session on the desktop host (`<repo>.2+local.host`, in a `cc-...` tmux session) included, and
"cc on the host and ccy in podman must show differently". That mislabel is a current v1
defect (`board/collect.go:516` sets `KindCcy` for every checkout seat, `cc` host seats
included), fixed now by a separate worker the coordinator assigned, not on this branch when
this was written. This plan takes the corrected data as given and designs only how it is
shown: Task 5.2 starts from that fix merged, and its tests are what ENV's input means.

- **T14 ENV, one field per agent line.** Where the member runs, in words a person reads:

  | Source (the corrected row data)                                        | ENV shown                      |
  | ---------------------------------------------------------------------- | ------------------------------ |
  | a seat whose handle ends `.podman`, held or last held by a ccy session | `ccy (podman)`                 |
  | a seat whose handle ends `.host`, held or last held by a `cc` session  | `cc (host)`                    |
  | a `.host` member that is not a desktop `cc` seat (agent-bus-claude)    | `host`                         |
  | `.lxc`, `.docker`, `.vm`                                               | `lxc`, `docker`, `vm`          |
  | anything the row cannot say                                            | `unknown` (red), never a guess |

  The handle's `<type>` suffix is the floor (protocol §3), and the seat record's kind refines
  it (ccy against cc) when it carries one. ENV replaces the flat KIND column's agent words; a
  team line and a person line have no ENV. `--list` keeps its KIND column as the separate fix
  leaves it (T12).

- **T15 Colour by meaning, legible on a dark terminal.** State words keep today's
  green/yellow/red (working, wants a look, not working; `board/table.go`). ENV words take one
  fixed hue each from Plan 00217's palette, which is already measured at WCAG AA on the
  reference dark backgrounds (00217 C2) and stays clear of the state hues (C3): `ccy (podman)`
  teal, `cc (host)` magenta (the less isolated kind, so it stands out), `lxc` blue, `docker`
  and `vm` indigo, `host` violet. Each team's name is drawn in its team colour (00217 C5, C7),
  so the board and the status line agree on which team is which. Group headings
  (coordinator, workers, people) are bold, empty-group lines and `·` dim.

- **T16 Colour never carries meaning alone.** Every coloured fact is also a word (`cc (host)`,
  `held`, `down`), so `NO_COLOR`, a piped run or a colour-blind reader loses nothing. Under
  `NO_COLOR` the board draws no colour codes at all and the same layout; a test strips the codes
  from a coloured render and requires it equal to the `NO_COLOR` render.

- **T17 Same palette in both renderers.** 24-bit colour from the palette on fzf (`--ansi`, as
  today) and through Lip Gloss in Plan 00204's frame, one table of hues in one Go package, so
  the fzf and Bubble Tea boards cannot drift.

## 4. v1 now, v2 later

| Part                                                                       | v1 (this plan)                               | v2 (Plan 00209)                                                                                                                                                                                   |
| -------------------------------------------------------------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Grammar, workers only, suggestions, flows, display forms (L1-L10, L16-L27) | built here                                   | carried as they are                                                                                                                                                                               |
| Store (L11)                                                                | registry v3 `labels`                         | **replaced**: the roster's member entry gains `label` (A 5.1), an informational key (A 3.1: unknown keys ignored, no feature needed); `teamchat-admin member label` (C 3.6.3)                     |
| Route to members (L13, L14)                                                | `agent_bus.labels` room state, `labels.json` | **replaced**: the roster itself; the link keeps it in its state; no separate event                                                                                                                |
| LXC declaration (L12)                                                      | `agent_bus_lxc_members[].label`              | **replaced**: `teamchat_lxc_members[].label` (C 3.8); the label stays per entry although v2's entries lose `role` (the coordinator is a team key there), so the "as its role" shape is not copied |
| host_vars readers (L8)                                                     | `teamfile.py`, `declare.py`                  | stage 1's Go line reader (00209 Task 7.1, `internal/bus/declare`) must learn `labels:` before any entry carries it; v2's `teamchat_teams[]` takes the same key (C 3.8)                            |
| Board read (L15)                                                           | `agent-bus list` field                       | kept by stage 1 (Task 7.1); **replaced** by SPEC-view's later in-process reader, then by `status.json` `members[].label` (C 3.11.3); each carries it                                              |
| Status line, envelope, context (L19-L26)                                   | pingbus (Python and the Go status line)      | **replaced** by teamchat's own (B27, A's envelope, B's hooks), same text                                                                                                                          |
| Migration                                                                  | -                                            | the carry plan (D 5) copies each v1 label to the v2 roster                                                                                                                                        |
| The tree board (T1-T17)                                                    | built here, in `ccy-teams`                   | kept: 00209 Task 4.3 feeds it                                                                                                                                                                     |

The rows marked **replaced** are v1 code with a known end: they live while the team is on v1
and go when it migrates. They are built because teams stay on v1 until the owner migrates each
one (00209 Phase 6), and the owner wants labels now; each mirrors the colour route of Plan
00217, so the cost is one more of a known shape. If the owner prefers to wait for v2 (OA1), the
plan drops those rows and builds the tree board and the 00209 requirements alone.
