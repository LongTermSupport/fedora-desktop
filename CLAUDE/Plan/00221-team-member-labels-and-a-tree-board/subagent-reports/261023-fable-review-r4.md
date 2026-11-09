# Adversarial review, round 4: Plan 00221 (labels, tree board)

Reviewer: Fable. Scope: commit 22b3e0a57 (the round-3 answers) and its consequences, checked
against the code on this branch. Round 3 is `subagent-reports/261023-fable-review-r3.md`. Paths
are relative to the repository root. Nothing built; read-only review. Plan 00222 was READY in
round 3 and 22b3e0a57 touches only its journal.

## Verdict

**Plan 00221: READY.** Every round-3 finding is closed. The diff introduces nothing new of
weight; two nits below, neither stops READY.

## Round 3 closure

| R3  | State  | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| --- | ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| N7  | closed | `DESIGN.md:88-92` names the one difference as the value only: `_VALUE` (`checkout.py:77`, `[A-Za-z0-9._-]*`) has no space; for `CCY_TEAM_LABELS` a quoted value is L1 labels joined by single spaces, a bare value is one label, `HOOKS_DAEMON_HOSTNAME` unchanged. `PLAN.md:96-101` Task 1.2 tests `"infra front back"` giving three labels, a bare multi-word value refused, and the `HOOKS_DAEMON_HOSTNAME` reading unchanged. Matches the code: `_ASSIGN_RE` (`:78-82`) uses `_VALUE` in all three alternatives, so the port's widening must be a parameter, as L9 now implies                                                             |
| m1  | closed | `DESIGN.md:319` row renamed "host_vars readers (L8, L12)", names `agent_bus_lxc/members.py`, `declare.py`'s `_LXC_FIELDS` writer, and `lxc.go` learning the LXC `label`                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| m2  | closed | `DESIGN.md:117-121` splits readers (`members.py` `parse_members`, `lxc.go`) from writers (`_LXC_FIELDS`, lxc-add) and says the writer failure is silent; `PLAN.md:111-115` Task 2.2 adds the write-and-read-back test. Confirmed: `declare.py:1234` comment "in the order they are written", and `_lxc_values` (`:1243`) also takes its flag names from `_LXC_FIELDS`, so a `--label` flag absent from the tuple never reaches the values dict: the writer classification is right. `check_lxc` (`:1254`) still delegates refusal to `members.parse_members`; `EDITABLE_KEYS` (`:1041`) is the team entry, not LXC, so no LXC reader is missed |
| m3  | closed | `DESIGN.md:131` rewrapped into `:137-140`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |

## New findings

### Nits (do not stop READY)

**n1. Two residual over-length lines from the rewraps.** `DESIGN.md:139` (103 columns) and
`PLAN.md:100` (105 columns) sit in ~95-column prose; the rewrap of m3 left its third line
long and the Task 1.2 insertion did the same. Re-break each once.

**n2. Row label vs content.** `DESIGN.md:319` is headed "host_vars readers" yet now lists
`_LXC_FIELDS` as a writer. Accurate as written (it says "writer"); if wanted, head the row
"host_vars readers and writers". Cosmetic.

## Also verified this round

- `members.py:112` `OPTIONAL_KEYS` and `lxc.go:14` `lxcOptional` are still the identical
  eight keys, so the parity test Task 2.2 relies on has a real baseline.
- Journals for both plans carry the round-3 entry via the daemon stamp (11:08); no hand stamp.
- No install-specific names, no time estimates, no new file edits outside the two plan folders.
