# Adversarial review, round 2: Plans 00221 (labels, tree board) and 00222 (worker tool restrictions)

Reviewer: Fable. Scope: the two plan folders after commit 82e401dcc on branch
`agent-a73de92448810ff4c-98c4ef93` (plan only, nothing built). Round 1 is
`subagent-reports/261023-fable-review-r1.md` in either folder. Checked against v1 code on this
branch, Plan 00209's design on `feat/bus-v2-n3`, and the vendored Claude Code docs. Paths are
relative to the repository root; `A`, `D` are 00209's DESIGN-v2 chapters.

## Verdicts

- **Plan 00221: FIX-FIRST.** Every round-1 finding is closed. Two new majors, both of the
  class round 1 found (a reader the key must reach, a parser that already exists), each a
  small edit. No blocker.
- **Plan 00222: READY.** Every round-1 finding is closed; two minors remain, neither stops it.

## Round 1 closure

### Plan 00221

| R1  | State  | Evidence                                                                                                                                                                                                                                                                     |
| --- | ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1  | closed | DESIGN L8 names `teamfile.py`, `declare.py` (`EDITABLE_KEYS`, confirmed `declare.py:1041`), the Go reader; PLAN Task 1.3 orders readers before the dist; section 4 gains the "host_vars readers" row. `teamfile.py:345-347` does refuse an unknown key, so the premise holds |
| M2  | closed | L9 now parses one exact line, never sources; Task 1.2 tests the refused form; OA2 says "parsed (never sourced)". See new N2: the parser it needs already exists                                                                                                              |
| M3  | closed | T6/T8/T9 split into fzf (always expanded, hidden fields, no relaunch) and the 00204 frame (collapse, parent-keeping filter); Task 5.4 and OA3 agree. `picker.go:185-186` (`--with-nth=1`, no `--nth`) confirms hidden fields stay searchable                                 |
| M4  | closed | T2 takes the system word (D 3.9 confirmed at `DESIGN-v2-D-migration.md:312-314`); T12 bounds the byte pin; Dependencies names D 3.9                                                                                                                                          |
| M5  | closed | L15 and section 4 record stage 1 keeping the call and the two later replacements; Dependencies names Task 7.1. The citation is wrong, see N3; the conclusion is right                                                                                                        |
| m1  | closed | T14 locates the defect (`board/collect.go:516`) and gates Task 5.2 on the merge; the worker stays unnamed, which is acceptable now the defect is                                                                                                                             |
| m2  | closed | section 4 LXC row: label stays per entry, "as its role" not copied                                                                                                                                                                                                           |
| m3  | closed | L21: after `role:` in v2; A 13.2 confirmed (`DESIGN-v2-A-protocol.md:1789-1792`: `kind:`, `from:`, `role:`, `team:`)                                                                                                                                                         |
| m4  | closed | Task 5.6 names the sentence (`docs/agent-bus.md:511`) and the entrypoint message; see N5 on whether the latter needs changing                                                                                                                                                |
| m5  | closed | T12 and Task 5.5 name the F44 build at the branch point and `check_list`'s method                                                                                                                                                                                            |
| m6  | closed | L5 states the only control and the grammar's ceiling                                                                                                                                                                                                                         |
| m7  | closed | Success Criterion 4 moved to a Handoffs section                                                                                                                                                                                                                              |

### Plan 00222

| R1  | State  | Evidence                                                                                                                                                                                                                                            |
| --- | ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1  | closed | R6 and Task 3.1 name the three readers, `EDITABLE_KEYS` for Task 3.2, and the share with 00221 Task 1.3                                                                                                                                             |
| M2  | closed | RESEARCH 2.1 "The live-reload variant" weighs the route and gives the three reasons; the `docs/agent-bus.md` quote is exact (`docs/agent-bus.md:689`)                                                                                               |
| m1  | closed | R2 says the pre-`active` window allows (guard exits 0); matches `hooks.json:18-49`                                                                                                                                                                  |
| m2  | closed | R2 names the record, `PINGBUS_MEMBERSHIP` + applied file, `PINGBUS_TEAMS` + `PINGBUS_HOME` (`agent-bus-claude:78-86` confirmed: env file sets `PINGBUS_TEAMS`, `PINGBUS_HOME` defaulted, no membership), `inbox.cached_role`, `statusline/roles.go` |
| m3  | closed | R6: informational under A 3.1, with the "stricter, never looser" argument                                                                                                                                                                           |
| m4  | closed | section 3 table: `SendMessage` reasoning no longer cites the deny                                                                                                                                                                                   |
| m5  | closed | Task 1.1 names the two facts the docs do not state; RESEARCH 4 cites `hooks.md:296`, `:1996`                                                                                                                                                        |
| m6  | closed | Task 4.2 names `entrypoint.sh`'s kit-file list (confirmed, the `for _ccy_need in` loop) and the CCY bump; `playbooks/imports/play-claude-yolo.yml` exists                                                                                           |
| m7  | closed | Success Criterion 1 names the five tools and a byte comparison against R4's text                                                                                                                                                                    |

## Plan 00221: new findings

### Major

**N1. L12's `agent_bus_lxc_members[].label` has two readers that refuse an unknown key, and
Task 2.2 names neither.** Round 1's M1 was about the team entry; the same defect sits on the
LXC entry. `helpers/agent_bus_lxc/members.py:109-112` lists the keys and `:431` computes
`unknown = set(entry) - REQUIRED_KEYS - OPTIONAL_KEYS` and refuses; the Go port already on this
branch does the same (`src-go/internal/bus/declare/lxc.go:13-14`, `:130-131`, tested at
`lxc_test.go:49-50`: "unknown port"). An entry carrying `label:` stops `lxc-members` and so the
board. `00221/PLAN.md:106-107` (Task 2.2) says only "applied each run, refused with
`role: coordinator`". Fix: Task 2.2 (or Task 1.3) names `members.py` `OPTIONAL_KEYS`,
`lxc.go` `lxcOptional` and its `keyProblem` test, and the parity test (`parity_test.go`) so the
Python and Go readers accept the same entry; `lxc-add`'s write side (`members.py:789` region)
gets the field. The section-4 "host_vars readers" row gains the LXC reader.

**N2. L9 designs a second parser for a file that already has one, with a different grammar.**
`helpers/agent_bus/checkout.py:93-117` (`role_from_env_local`) already reads `ccy.env.local`
on the host without sourcing it: plain assignment of a literal (bare, double- or single-quoted,
`_ASSIGN_RE` at `:78`), any other line naming the variable refused, a second assignment
refused. `docs/agent-bus.md:446` and `:583` already describe the host reading
`HOOKS_DAEMON_HOSTNAME` from that file. L9 (`00221/DESIGN.md`, the `CCY_TEAM_LABELS` bullet)
specifies `^(export )?CCY_TEAM_LABELS="<labels>"$`, double quotes only, "two of them" refused
without saying whether that means two lines or one per file, and adds the tracked `ccy.env`
as a second source without saying which file wins when both set it. Two grammars for one file
class is what the project's Go-tooling and DRY rules exist to stop, and the Go chooser
(`src-go/internal/teams/chooser`) would port `_ASSIGN_RE` anyway. Fix: L9 says "read as
`checkout.py`'s `role_from_env_local` reads `HOOKS_DAEMON_HOSTNAME` (one assignment of a
literal, else refused naming the line), generalised to the variable name, in Go beside the
chooser with a parity test against the Python"; states precedence (`ccy.env.local` over
`ccy.env`, the entrypoint's own order, or the union); and the "relaxes the rule" sentence is
softened, since the host already reads one bus-relevant variable from `ccy.env.local`.

### Minor

**N3. L15's stage-1 citation is misattributed.** `SPEC-view.md:4-5` and `:18`
(`feat/bus-v2-n3`): the "one Python call" stage 1 keeps is `declare status` for the APPLIED
verdict (V40), not `sudo -n agent-bus list`; SPEC-view never names `agent-bus list` or
`board/fetch.go`. The conclusion (the appended field survives stage 1 because stage 1 does not
touch that call) is right; the sentence should say that instead of quoting V40's call.

**N4. "Nothing here edits 00209's files" (`00221/PLAN.md:66`) now contradicts Task 1.3.**
`src-go/internal/bus/declare` is on this branch (F44), so Task 1.3 edits 00209's stage-1 code
directly while Task 7.1 is in flight on `feat/bus-v2-s1-declare` (branch exists here). Say:
"the Go reader is on F44 and is edited here; Task 7.1's open branch is told, to avoid a
conflicting merge". Same sentence in `00222/PLAN.md` Dependencies.

**N5. Task 5.6's entrypoint edit is not needed and drags a CCY bump with it.** The refusal
at `entrypoint.sh:570` says "a session's agent team bus seats come only from the host"; it
does not quote "nothing about the bus goes in `ccy.env.local`". `CCY_TEAM_LABELS` sets no seat
(L9's own words), so the message stays true. Drop the edit and the bump; keep the
`docs/agent-bus.md:511` reword. (If the owner wants the message to mention labels anyway, the
bump stands, but then say it is a choice, not a correction.)

**N6. T12's "which changes `--list` for every team" is an inference.** D 3.9 says the board
has a `system` column; it does not say `--list` gains it. Write "if Task 4.3 gives `--list` the
column", or ask 00209 to state it, so the byte pin's end is a fact.

## Plan 00222: new findings

### Minor

**N1. R2's pre-`active` sentence applies to membership sessions only.** The guard
`[ -z "$PINGBUS_MEMBERSHIP" ] || [ -e "$PINGBUS_HOME/active" ] || exit 0` runs the hook
unconditionally when `PINGBUS_MEMBERSHIP` is empty, which is the `agent-bus-claude` case
(`agent-bus-claude:85-86` exports `PINGBUS_TEAMS`, `PINGBUS_HOME`, never membership). So for
those sessions there is no allowed window: R2's fail-closed rule applies from the first call.
Add "(ccy and `cc`; an `agent-bus-claude` session is judged from its first call)".

**N2. Same wording as 00221 N4.** `00222/PLAN.md` Dependencies: "Nothing here edits 00209's
files" against Task 3.1 editing `src-go/internal/bus/declare` on this branch.

## Cross-plan

The shared-reader ownership is now stated in both plans (00221 Task 1.3, 00222 Task 3.1).
00221 N1 adds a fourth reader pair (LXC, Python and Go) to 00221 alone; 00222 has no LXC key
and is unaffected.

## Also verified this round

- `hooks.json:8-49`: every entry carries the guard as R3 says; no PreToolUse entry exists.
- `declare.py:8` ("Any other shape is refused") and `queries.go:1-10` (line reader of one
  subset) are as the plans cite; the Go team reader validates by shape, `teamfile.py:345` by
  key, so Task 1.3's "read, not refused" test is the right one for both.
- `docs/agent-bus.md:689`: the cc denies quote in RESEARCH 2.1 is exact.
- `playbooks/imports/play-agent-bus.yml`, `play-claude-yolo.yml` exist as Task 4.2 names them.
- No install-specific names, no time estimates, decisions still split with defaults.
