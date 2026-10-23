# Adversarial review, round 1: Plans 00221 (labels, tree board) and 00222 (worker tool restrictions)

Reviewer: Fable. Scope: the two plan folders as they stand on branch
`agent-a73de92448810ff4c-98c4ef93` (plan only, nothing built). Checked against the v1 code and
docs on this branch, Plan 00209's design on `feat/bus-v2-n3` at e3b6c30b4, and the vendored
Claude Code docs under the hooks daemon's `remote-docs/` (all fetched 2026-10-17).

Paths below are relative to the repository root. `00221/DESIGN.md` means
`CLAUDE/Plan/00221-team-member-labels-and-a-tree-board/DESIGN.md`; `00222/PLAN.md` and
`00222/RESEARCH-mechanisms.md` are under `CLAUDE/Plan/00222-worker-tool-restrictions/`.
`A`, `B`, `C`, `D` are 00209's DESIGN-v2 chapters on `feat/bus-v2-n3`.

## Verdicts

- **Plan 00221: FIX-FIRST.** Two majors in the design (host_vars readers and the fzf tree) and
  one in the dependencies (00209 stage 1 is replacing the very reads L8 and L15 extend). No
  blocker.
- **Plan 00222: FIX-FIRST.** One major (the same host_vars reader gap for `worker_blocked_tools`)
  and one major in the research (the live-reload route is dismissed without being weighed). No
  blocker. Every vendored-doc citation checked says what the plan says.

## What was verified and holds

Facts about v1, confirmed in code:

- The team record's exact key set and the "unknown key = untrusted" rule:
  `docs/agent-bus-protocol.md` §8 (the table and "Unknown keys ... make the room untrusted").
  The colour's route as admin room state outside the record: §8 "Colour" and 00217 SPEC C8.
- Seat grammar `[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11}` and named seats as identity: protocol
  §3 table rows `agent handle` and `seat`.
- Registry version 2 with exact keys, `update_registry` under `flock`:
  `helpers/agent_bus/registry.py:39-41, 327-345`.
- The role the status line reads is `inbox.cached_role` from the bundle's cached record, read on
  every render in Python (`helpers/pingbus/statusline.py:323-352`) and in Go
  (`src-go/internal/pingbus/statusline/roles.go:64-96, 271-272`). 00222 R2's "live role" source
  exists.
- `hooks.json` has no PreToolUse entry; every entry carries the
  `[ -z "$PINGBUS_MEMBERSHIP" ] || [ -e "$PINGBUS_HOME/active" ] || exit 0` guard
  (`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/hooks/hooks.json`).
- Three launchers pass the plugin and a `--settings` file: `entrypoint.sh:592`,
  `cc-teams.bash:143`, `agent-bus-claude:96`. `seat.py:447-449` records the M2 spike ("reads a
  `--settings` file once, at start").
- `told-roles.json` exists (`helpers/pingbus/membership.py:71-72`); the role templates are in
  `helpers/pingbus/hooks.py:134-150`.
- The KIND mislabel is real: `board/collect.go:516` sets `Kind: KindCcy` for every checkout
  seat, `cc` host seats included; `agentbus/parse.go:50-59` maps only `podman` to `ccy` for
  roster members.
- `ui.PickSpec` has hidden `Tags` (`src-go/internal/ui/picker.go:53-55`) and runs fzf with
  `--with-nth=1` over a delimiter (`picker.go:186`), so hidden fields are searchable: T10's
  "indented lines with hidden tags" is feasible.
- Palette names teal, blue, indigo, violet, magenta exist (`helpers/pingbus/colours.py:22-30`).
- `ccy-teams --list` is read by 00201's D1 (`_acceptance-m7.inc.bash:436`), 00203
  (`_acceptance-robust.inc.bash:201`) and compared byte for byte old build against new by
  00209 stage 1 (`_acceptance-s1.inc.bash:181-213`, `check_list`). T12's premise holds.
- InteractiveScripts rule 14 is as cited (`CLAUDE/InteractiveScripts.md:99-115`); T6, T7 and
  L17 comply on paper (every key beside a drawn action).

Facts about v2, confirmed on `feat/bus-v2-n3`:

- A 3.1: unknown keys inside a known major are ignored; a trust-narrowing key needs a feature.
  A 5.1: the roster's `members` values carry `role`, `human_text`, `parked`; "Unknown keys are
  ignored (section 3)". 00221's "label is informational under A 3.1" is a fair reading.
- B 1.5 B25/B27 and B 2.8: v2's hooks are SessionStart, UserPromptSubmit, Stop, SessionEnd,
  "no PostToolUse"; there is no PreToolUse. 00222 correctly files one as a requirement on 00209.
- B 2.9: the status line rows are `👥 <team> (<role>)` from `status.json`, keeping 00217's
  member name. 00221 correctly files the label piece as a B27 requirement.
- C 3.6.3 (member verbs), 3.8 (`teamchat_teams`, `teamchat_lxc_members`, no `label`, no
  `worker_blocked_tools`, and "LXC entries lose `role`": the coordinator is a team-level key),
  3.11.3 (`members[].kind`, `.role`), 3.13 (the board reads files, no sudo). Both plans state
  these as requirements for 00209, not as facts; correct.
- D 5.1: roles are copied from `registry.json`; nothing about labels. The carry requirement is
  correctly stated as new.
- PLAN Phase 7 Task 7.3: 00213's golden "is loosened to what callers parse when the Go status
  line takes over" (the oracle 00221 Task 3.1 re-records).

Vendored Claude Code docs: every cited line checked, all accurate.

| Citation in 00222                   | Says what the plan says                                                                                                             |
| ----------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `permissions.md:76`                 | yes: bare tool name removes the tool from context                                                                                   |
| `permissions.md:730`                | yes: denied at any level cannot be allowed at another                                                                               |
| `settings-reference.md:1706`        | yes: deny blocks in every mode including bypassPermissions                                                                          |
| `settings-reference.md:1586, :1403` | yes: `--disallowedTools` adds session deny rules; survives mid-session reloads (since v2.1.257)                                     |
| `settings.md:576, :643`             | yes: `--settings` sits above user/project/local, below managed                                                                      |
| `settings.md:594-596`               | yes: reload covers user, project, local and managed; the flag file is not listed                                                    |
| `sub-agents.md:41`                  | yes: built-in subagents inherit permission rules                                                                                    |
| `sub-agents.md:443-446`             | yes: first filter removes `AskUserQuestion`; forks skip both filters                                                                |
| `sub-agents.md:454`                 | yes: background built-in list excludes the whole catalogue of section 3                                                             |
| `hooks.md:1760`                     | yes: PreToolUse matches `AskUserQuestion`, `ExitPlanMode`, everything but `EndConversation`                                         |
| `tools-reference.md:143`            | yes: questions stay open until answered                                                                                             |
| `tools-reference.md` table          | yes: `EnterPlanMode` permission No, `ExitPlanMode` Yes (lines 40, 42); `PushNotification` 52, `RemoteTrigger` 55, `SendUserFile` 60 |
| `EndConversation` cannot be denied  | yes, but at `permissions.md:78` and `tools-reference.md:295` (the plan says "the permissions overview"; line 700 also says it)      |

Two doc facts the research did not cite that support it: `hooks.md:296` ("Hooks from settings
files, managed policy settings, and plugins also run inside subagents ... `PreToolUse` ... fire
the same configured hooks") and `hooks.md:1996` (precedence `deny` > `defer` > `ask` > `allow`).
Both strengthen section 4 and R3; cite them.

## Plan 00221 findings

### Major

**M1. L8/L9's host_vars `labels:` key has three readers; the plan names one.**
Evidence: Task 1.2 names `teamfile.py` and `localhost.yml.dist` only (`00221/PLAN.md:86-88`).
But `agent_bus_teams` entries are also read by `helpers/agent_bus/declare.py`'s line reader
(declare.py:1-10, 230-442 per SPEC-view; `EDITABLE_KEYS` at declare.py:1043, 1764) which
`ccy-teams` runs for every board open and settings edit, and 00209 Task 7.1 (In Progress,
`feat/bus-v2-s1-declare`) is porting that reader to Go as `internal/bus/declare`, "a line
reader of one subset ... that refuses every other shape" (SPEC-view.md §1 and V3-V8). A
`labels:` list the Python reader or the Go port does not know is refused, and the board stops
drawing the team. Fix: Task 1.2 names all three (`teamfile.py`, `declare.py` read and write
side, and a requirement on 00209 Task 7.1 / SPEC-view for the key), with the order (readers
before the dist example), and DESIGN section 4 gains a row for "host_vars reader" marking the
Go line reader as the thing that must learn the key.

**M2. L9's `CCY_TEAM_LABELS` in `ccy.env` cannot be "read on the host" as described.**
Evidence: `ccy.env` and `ccy.env.local` are bash files sourced only by `entrypoint.sh` inside
the container (`files/var/local/claude-yolo/claude-yolo:249-250`, `:3862`; no host-side
source of either file exists in `claude-yolo`). The host-side chooser (`ccy-teams choose-teams`,
Go) would have to parse a bash file without executing it, or the bash launcher would have to
source an untrusted checkout file on the host, which is exactly what the entrypoint's
`PINGBUS_*` refusal (`entrypoint.sh:570-576`) exists to avoid for the bus. Either the design
names the parse (one `^export CCY_TEAM_LABELS=...` line, exact grammar, anything else ignored
and said) or OA2's default moves to the team's `labels:` (L8) alone, with `ccy.env` dropped.
Fix: decide, and say in L9 how the value gets from the file to the chooser.

**M3. T9 (filter keeps each match's parents) and T6/T8 (expand/collapse) are not what fzf
does, and the design does not say how it gets them.** Evidence: `ui/picker.go:172-186` runs
one fzf over a fixed row list; fzf filters rows independently and has no tree. Keeping a
matched worker's team line means either the parents are re-rendered into every child's hidden
field (so the team line itself still disappears), or the picker is relaunched per keystroke.
Expand/collapse on fzf is a relaunch with a new row set, which the picker's redraw cost test
(`board/redraw_cost_test.go`) and 00203's open-speed work constrain. Fix: T9 states what fzf
delivers (children carry team and label in hidden searchable fields; parents are not kept until
the Bubble Tea frame) and T6/T8 state that expand/collapse is a relaunch with the cursor kept,
with a bound on its cost, or T6-T9 are moved to the 00204 frame and the fzf renderer gets the
flat-with-indent form only.

**M4. T13 says the tree is built once for both systems, but D 3.9 gives the v2 board a
`system` column (`agent-bus` / `teamchat`) and two-system teams; the tree has no place for
it.** Evidence: D.md §3.9 ("ccy-teams lists the teams of both systems in one board, with a
`system` column"); `00221/DESIGN.md` T1-T3 and the sketch have no system field, and T12 pins
`--list` bytes that D 3.9 itself will change. Fix: T2 (the team line) takes a system word when
more than one system is live; T12 says the byte pin holds "until 00209 Task 4.3 adds the
system column", and the 00209 requirements list in PLAN Dependencies names D 3.9.

**M5. L15 extends `sudo -n agent-bus list`, which 00209 is replacing in two steps the plan
does not record.** Evidence: the board reads rosters through `agent-bus list`
(`board/fetch.go:37-53`); SPEC-view (stage 1, Task 7.1, In Progress) keeps that one call now
and "Stage 4 (the reader, Task 5.1) adds its part"; v2 proper reads `status.json` (C 3.13).
DESIGN section 4 marks L15 replaced by `status.json` only. Fix: the row says "stage 1 keeps
`agent-bus list` (field appended, safe); stage 4's reader must carry `label`; v2 reads
`members[].label`", and the Dependencies bullet names Task 7.1.

### Minor

**m1. The KIND fix "by a separate worker" is unnamed and unverifiable.** `00221/PLAN.md:23, 51, 65` and DESIGN T14. No branch or plan on this checkout carries it. Fix: name the plan or
task (or the handle of the seat) so T14's "corrected row data" has a definition to test
against; until then Task 5.2's table test can only encode the plan's own table.

**m2. L12 says an LXC entry's `label:` is applied "as its role does", but v2 removes `role`
from LXC entries.** C 3.8: "LXC entries lose `role`"; the coordinator is a team key. The v1
half is right (`declare.py:1233-1234` has `role` among `_LXC_FIELDS`); the v2 row in section 4
should say the label stays per entry while the role does not, so 00209 does not copy the
"as its role" shape.

**m3. L21 puts `label:` "on the line after `from:`"; v2's entry already has a `role:` line
there.** A 13.2 line table: `kind:`, `from:`, `role:`, `team:`. Say where `label:` goes in
v2's order (after `role:` is the natural place) so the requirement on 00209 is exact.

**m4. OA2 relaxes a documented rule without naming the doc line to change.**
`docs/agent-bus.md:510-511`: "nothing about the bus goes in `ccy.env.local`". Task 5.6 should
name that sentence; the entrypoint's `PINGBUS_*` refusal message (`entrypoint.sh:570`) also
quotes the rule and would become half-true.

**m5. Success Criteria "`--list` bytes unchanged for an unlabelled team" is testable only
against a build the plan does not name.** Say "against the F44 build at the branch point,
by 00209 `check_list`'s method" so Task 5.5 has a procedure.

**m6. Prompt-injection and forge safety are sound but one hole is unstated.** L1's grammar is
`[a-z0-9-]{1,16}`, so the label cannot carry an instruction, and L14 re-checks the grammar on
read. But L25's template tells the agent the label is "internal to the team, so do not put it
in commits, pull requests or issues"; nothing enforces it, and `pingbus` writes nothing of it
to the forge (L5 verified: `forge.py` never emits member facts). State in L5 that the only
control is the template sentence plus the grammar's ceiling on what a leak could cost (16
lower-case characters), and that a team using client names as labels accepts that.

**m7. Plan form.** Tasks are tests-first (each names the test), decisions are split with
defaults, no time estimates, no install-specific names (`projectx`, `site-b` are
placeholders; `fedora-desktop.2` is the permitted self-reference; `Owner: joseph` matches every
other plan's header). Success Criterion 4 ("the 00209 requirements are taken into its plan by
its owner seat") is not something this plan can satisfy; make it a handoff item, not a
criterion.

## Plan 00222 findings

### Major

**M1. R6's `worker_blocked_tools` in `agent_bus_teams` has the same three readers as
00221 M1.** `00222/PLAN.md:116-117` (Task 3.1) names `teamfile.py` and the dist; `declare.py`'s
read and write side and 00209 Task 7.1's Go line reader (SPEC-view V3-V8) refuse a key they do
not know, and the board stops drawing the team. Fix: as 00221 M1, plus a settings-screen edit
path (Task 3.2) that goes through `declare.py`'s `EDITABLE_KEYS`.

**M2. RESEARCH 2.1 dismisses the live-reload route without weighing it.**
`permissions.md:76` (cited for a different fact) says a bare deny "added mid-session" takes
effect "from its next tool call on", and `settings.md:594` says Claude Code watches user,
project and local settings files and applies `permissions` edits live. So a hot, pure-Claude-
Code mechanism exists: the keeper rewrites `permissions.deny` in a reloadable settings file
on each role change. The real reasons it does not fit are that those files are per checkout or
per user, not per session (two sessions in one checkout with different roles would fight over
one file), that the kit must not write the user's or the project's settings, and that a deny
cannot be lifted by a hook once another file still carries it (`permissions.md:730`). Say
that in 2.1, or OB2's alternative is incomplete and the owner cannot judge it.

### Minor

**m1. R2's "fail closed" and R3's guard disagree at one state.** The guard
`[ -e "$PINGBUS_HOME/active" ] || exit 0` lets every call through while a seated session's
keeper has not yet written `active` (and after it is removed); R2 says a seated session whose
role cannot be read is denied. Decide which wins in that window (exit 0 = allow is the
pragmatic answer, since no team record exists yet) and say so in R2.

**m2. R2 misnames the role source.** "`PINGBUS_HOME`'s per-team state" is not where the
role is: the Python and Go status lines read the bundle's cached team record
(`statusline.py:323-352` via `inbox.cached_role`; `roles.go:86-96, 271-272`), and the teams
list comes from the session's record or from `PINGBUS_MEMBERSHIP` + the applied file
(`statusline.py:138-156, 280-296`). Name those so Task 2.2's reader reuses them exactly;
`agent-bus-claude` sessions have `PINGBUS_TEAMS` and `PINGBUS_HOME` but no membership
(`agent-bus-claude:78-86`), so the reader needs both paths.

**m3. The roster key's class under A 3.1 is not stated.** 00221 says "informational"; 00222
says only "a roster key". A `worker_blocked_tools` an old v2 member ignores makes it apply the
default set, which is stricter than an opt-out and never looser, so "informational" is
defensible; say it, with that argument, so 00209's key registry gets it right.

**m4. Section 3's `SendMessage` reasoning cites a fact that supports the opposite.** "A bare
deny would remove `SendMessage`" is true but irrelevant: the chosen mechanism is a hook, not a
deny, and would not remove anything. Keep the conclusion (not a role question) and drop the
deny argument.

**m5. Task 1.1's host check should also prove the mode interaction.** ccy runs
`--dangerously-skip-permissions` (`claude-yolo`, 5 occurrences; `entrypoint.sh`, 1). The docs
say deny rules hold in bypass (`settings-reference.md:1706`) and PreToolUse hooks run in
subagents (`hooks.md:296`), but neither line says a hook's `permissionDecision: "deny"` holds in
bypassPermissions. The task already lists bypass; add that this is the fact it proves, and
record the vendored doc's silence.

**m6. The deployed kit's required-file list is a CCY change.** `entrypoint.sh:581` refuses a
kit missing `pingbus-status-line`; adding `pingbus-tool-guard` there is an edit under
`files/var/local/claude-yolo/`, which the version-bump rule covers. Task 4.2 should name it
rather than "the bumps the pre-commit hook asks for".

**m7. Plan form.** Tests-first, decisions split with defaults, no time estimates, no
install-specific names. Success Criterion 1 should name the five tools and the message's fixed
template (R4) so the host check in Task 4.4 is a comparison, not a judgement.

## Cross-plan

- Both plans say "no shared code" with each other, yet both add a key to `agent_bus_teams`
  that the same three readers must learn, both add an admin room-state event of the colour's
  shape, and both extend the syncer and `role_lines`. Build the readers' change once (one task,
  owned by whichever plan goes first) and have the other depend on it.
- Neither plan lists 00209 Task 7.1 (stage 1, In Progress) as a dependency, and it is the
  one 00209 task that is about to change v1 code both plans edit.

## Scratch

Exported 00209 chapters for this review are under `untracked/scratch/r221/` (not tracked).
