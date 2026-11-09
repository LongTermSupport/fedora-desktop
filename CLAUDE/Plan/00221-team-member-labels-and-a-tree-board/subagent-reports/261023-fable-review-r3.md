# Adversarial review, round 3: Plans 00221 (labels, tree board) and 00222 (worker tool restrictions)

Reviewer: Fable. Scope: commit c5d7811bf (the round-2 answers) and its consequences, checked
against the code on this branch. Round 2 is `subagent-reports/261023-fable-review-r2.md`. Paths
are relative to the repository root. Nothing built; read-only review.

## Verdicts

- **Plan 00221: FIX-FIRST.** Every round-2 finding is closed. One new major, introduced by the
  N2 fix: the grammar it now reuses cannot accept the value it is given. One-sentence edit.
- **Plan 00222: READY.** Both round-2 minors closed; nothing new.

## Round 2 closure

### Plan 00221

| R2  | State  | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| --- | ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| N1  | closed | DESIGN L12 (`:113-117`) and PLAN Task 2.2 (`:109-112`) name `members.py` `parse_members`, `declare.py` `_LXC_FIELDS`, `lxc.go` with its parity test, and lxc-add's writer. Confirmed: `members.py:112` `OPTIONAL_KEYS`, `:431` unknown-key refusal; `lxc.go:14` `lxcOptional`, `:121-131` `keyProblem`; `declare.py:1234` `_LXC_FIELDS`, `:1402` `render_lxc_entry` writes only those fields. See m1, m2 for two loose ends                                                                                                                                                                   |
| N2  | closed | L9 (`DESIGN.md:83-92`) reuses `checkout.py` `role_from_env_local` / `_ASSIGN_RE` (`checkout.py:78-82`, `:93-117` confirmed: bare/dq/sq literal, `export` optional, comment lines skipped, other mention refused, second assignment refused), Go port with a parity test, `ccy.env.local` wins. Precedence matches `entrypoint.sh:431-432`, `:445` (`.local` sourced second). No Go port exists yet (`lxcadd/checkout.go`, `ccylaunch/inrepo.go` do not parse the variable), so the port is not a duplicate. The relaxation sentence is softened. See N7: the reused grammar refuses the value |
| N3  | closed | L15 (`DESIGN.md:131-133`): stage 1 keeps `declare status`; `agent-bus list` left in place                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| N4  | closed | `00221/PLAN.md:65-67`: edits none of 00209's design or plan files; the Go reader on F44 is coordinated with Task 7.1                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| N5  | closed | Task 5.6 (`PLAN.md:145-147`) drops the entrypoint edit and the bump; L9 says the message stays true. Confirmed against `entrypoint.sh:570` (message names seats only)                                                                                                                                                                                                                                                                                                                                                                                                                         |
| N6  | closed | T12 (`DESIGN.md:256-258`): "expected to change", "00209's to say"                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |

### Plan 00222

| R2  | State  | Evidence                                                                                                                                                                                                                                                                                                                                                                                                   |
| --- | ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| N1  | closed | R2 (`PLAN.md:62-66`): the pre-`active` allow is for membership sessions; an `agent-bus-claude` session is judged from its first call. Confirmed: `plugin/pingbus/hooks/hooks.json:18,29,39,49` guard `[ -z "$PINGBUS_MEMBERSHIP" ] \|\| [ -e "$PINGBUS_HOME/active" ] \|\| exit 0` runs the hook when membership is empty; `agent-bus-claude:84-86` sets `PINGBUS_TEAMS`, `PINGBUS_HOME`, never membership |
| N2  | closed | `00222/PLAN.md:96-97`: same wording as 00221 N4                                                                                                                                                                                                                                                                                                                                                            |

## Plan 00221: new findings

### Major

**N7. The grammar L9 now reuses cannot accept L9's own example.** `checkout.py:77`
`_VALUE = r"[A-Za-z0-9._-]*"` is the whole value class of `_ASSIGN_RE` (`:78-82`), bare or
quoted: it has no space. `CCY_TEAM_LABELS="infra front back"` (`DESIGN.md:90`) does not match,
so `role_from_env_local`'s rule refuses it as "a form that only running the file could read"
(`:104-107`), and every file with more than one label gives no suggestions. The fix in c5d7811bf
says "generalised to the variable name" (DESIGN L9) but the value class must be generalised too,
and bare and quoted forms then differ: a quoted value may hold L1 labels joined by single
spaces; a bare `CCY_TEAM_LABELS=infra front` is not one assignment in bash and must be refused
(or only a single bare label accepted). Fix: L9 adds "the value class widened to L1's labels
joined by single spaces inside the quotes; a bare value is one label only"; Task 1.2's
"each accepted form" test includes the design's own three-label example, a bare multi-word line
refused, and the Python `role_from_env_local` left unchanged for `HOOKS_DAEMON_HOSTNAME` (the
widened class is the generalised function's parameter, not a loosening of the role's).

### Minor

**m1. Section 4's "host_vars readers" row still names only the team readers.** Round 2 N1's
fix listed the row gaining the LXC reader; `DESIGN.md:314` is unchanged (`teamfile.py`,
`declare.py`, the Go line reader). The LXC row at `:313` covers the key, not its readers. Add
`members.py` / `lxc.go` to the row or a "parity" note to `:313`.

**m2. `declare.py`'s `_LXC_FIELDS` is a writer's field order, not a refusing reader.**
L12 (`DESIGN.md:114-116`) says "each refusing a key it does not know"; `declare.py:1234` is the
tuple `render_lxc_entry` (`:1402`) and `_replace_lxc` (`:1449`) write from, and `check_lxc`
(`:1254`) delegates refusal to `members.parse_members`. The consequence is worse than a refusal:
an `lxc-add --label` would be silently dropped from the written entry, and `lxc_differences`
(`:1327`) would then report it on every compare. Keep `_LXC_FIELDS` in the list; say "the
write side (`_LXC_FIELDS`, else the field is silently not written)".

**m3. `DESIGN.md:131` is one unwrapped 200-character line** among 95-column prose; wrap it.

## Also verified this round

- `lxc.go:21` `roleVar` is used only for the two-hosts refusal (`:328`); no Go reader of
  `ccy.env.local`'s assignments exists on this branch, so Task 1.2's port is the first.
- Both journals carry the round-2 entry via the daemon's stamp; no hand-written stamp.
- No install-specific names, no time estimates introduced by the diff.
