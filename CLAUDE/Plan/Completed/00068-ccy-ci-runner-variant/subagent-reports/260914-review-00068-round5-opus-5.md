# QA Review — Plan 00068 closing work, round 5 (`1c24390b`)

**Reviewer**: qa-reviewer (Opus 5) · **Date**: 2026-09-14 · **Read-only review**

**Verdict**: **FIX-BEFORE-MERGE** — do not flip to Complete yet. Two one-line edits stand between
here and APPROVE.

Round 4's findings 1, 2, 3, 4, 5 and nits 6–10 are all genuinely discharged; I re-derived each
rather than taking the author's account and every claim checked out (details under *Checked and
clean*). The commit's own failure mode — fixing where you were reading and missing the consumer one
file out — has happened again, in the same shape as round 4's finding 2. The sweep I was asked to do
found the `≈6` claim alive in **two live files**, one of them the plan the commit was editing.

## Blocking

### 1. `00113 PLAN.md:18` still forwards the ≈6 as a fact — in the Overview of the file whose Task 0.2 this commit rewrote to forbid exactly that

```
The product is `files/var/local/claude-yolo/claude-yolo`, the launcher. Today it
negotiates with a human at roughly six credential-resolution sites, assumes a TTY,
```

Fifty lines below, `00113 PLAN.md:67-74` now reads *"Start from no number. `reports/ci-flow.md`'s
'about six' is **superseded**, not merely caveated … §6 puts 13 network and compose sites back in
play"*. The Overview is unqualified, and it carries the specific claim §6 reopens: *credential
resolution* sites. `ci-flow.md:101` says *"nothing may forward `≈6` as a fact in the meantime."*

This is the first paragraph an implementer reads, in a `Not Started` plan, and it is the same defect
round 4 blocked on at `00068 PLAN.md:98-99` — one file out, and this time the commit had the file
open. A grep for `≈6`/`about six` misses it: the spelling is "roughly six".

**Fix**: one clause — "at what 00068 derived as roughly six credential-resolution sites, a figure
Task 0.2 supersedes and measures" — or delete the number from the Overview.

### 2. `00068 DECISIONS.md:78-79` forwards it too, in the live requirements document, in the same file as §6

```
   does. `reports/ci-flow.md` derives that the flow reaches about 6 of the 46, all credential
   resolution; the guarded primitive is still the right mechanism, with six callers.
```

`DECISIONS.md:3-8` declares itself *"the durable material … the requirements CI places on `ccy`"* —
not an archive (that is `PLAN_archive.md:1`, correctly framed and correctly left alone). §3 item 1 is
a live requirement; §6, 200 lines down in the same document, is the reversal that supersedes it, and
the two never meet. `00068 PLAN.md:34` points the Goals at §3, and `00113 PLAN.md:32` names
`DECISIONS.md` as the **first** thing to read before starting.

So after this commit the plan says in `PLAN.md` "nothing may forward ≈6", in `ci-flow.md` "nothing
may forward `≈6` as a fact", and in `DECISIONS.md` "the flow reaches about 6 of the 46, all
credential resolution".

**Fix**: mark it in place — "(superseded by §6; Plan 00113 Task 0.2 measures the real figure)" — or
strike the sentence. Both clauses need it: the count *and* "all credential resolution".

Everything else forwarding the figure is legitimately historical and correctly left alone:
`PLAN_archive.md:68`/`:417` (header: *"Kept verbatim as the historical record"*), the `26-08-01` and
`26-09-14` journals (append-only), `ci-flow.md:91` (inside the struck section, two lines above its
own supersession note).

## Should fix

### 3. `ci-flow.md:106` — nit 9's completed enumeration ends in a pointer at the wrong pair

> … **(e) token resolution**, **(e) `select_token` interactive** and **(d) migration**. The last two
> of those are the rows that made up the `≈6`

The last two of that list are `select_token` (1 site) and migration (1 site) = 2 sites. The rows that
made up the ≈6 are **token resolution (5, YES)** and **`select_token` (1, YES)**; migration is the
"Probably" row. Round 4 named the pair correctly ("one of the two rows that make up the ≈6");
completing the enumeration pushed migration to the end and the positional reference followed it.

New text, introduced by this commit, in a commit whose entire thesis is that positional references
decay and names do not. **Fix**: "The token-resolution and `select_token` rows are the ones that made
up the `≈6`".

### 4. The line-number lesson was not generalised past §6 — the same two constructs are still cited by number 7 and 14 lines below the fix

`DECISIONS.md:309-311` now cites the leftovers by construct. Immediately below:

- `DECISIONS.md:316` — *"and `:2741` runs `container_cmd rm -f "$CONTAINER_NAME"` as leftover
  cleanup"*. Today `claude-yolo:2741` is `GENERIC_FOLDERS="projects|repos|work|src|code|dev|home"`;
  the construct is at `:3014`.
- `DECISIONS.md:323-324` — *"`save_launch_config` (`:2607`, body at `:368-392`)"*. Today `:2607` is
  `break 2` inside the network-selection menu; the call is `:2880`, the body `:454`.

Same in the report 00113 reads second: `ci-flow.md`'s flow table cites `save_launch_config` `:2607`,
the `rm -f` net `:2741`, `stty` `:2726-2729`, `--device /dev/dri` `:2767`, `GUI_MOUNTS`
`:2697-2721`, `:864`, `:870`, `:1033`, `:1430` — I checked all of them and every one is stale,
several landing on plausible-looking neighbours (`:2767` is now an engine/no-network test; `:870` is
`exit $?`). Unlike `DECISIONS.md:10-11`, **`ci-flow.md` carries no "treat as pointers" caveat at
all** — `:8` only says which file they refer to.

This is AgentNotes' *"a lesson written down beside the thing it fixed, never generalised"*. Not a
request for a full conversion of two documents at freeze time. **Minimum**: fix §7 items 1 and 2 to
the constructs §6 already names (they are literally the same two), and give `ci-flow.md` the
`DECISIONS.md:10-11` caveat.

### 5. `00113 PLAN.md` got three task rewrites and no entry in its own `JOURNAL/`

Tasks 0.2, 0.4 and 2.5 were rewritten by this commit. `00113-Journal-26-09-14.md` ends at
**`11:55 · handoff · — ready to start at Phase 0, on HOST`** — and `CLAUDE/PlanJournalling.md:82-85`
makes the last entry of the newest day-file the resumer's entry point. An implementer resuming 00113
reads a hand-off that predates three rewrites of the tasks they are about to run. The precedent is in
that same file: `11:50 · finding · P0` records exactly this class of cross-plan correction in 00113's
journal, not 00068's.

**Fix**: one appended entry in `00113-Journal-26-09-14.md`.

### 6. `00113 PLAN.md:196-198` points at line numbers §6 no longer has

> Its line numbers were re-verified 2026-09-14 after drifting ~40 lines; the function names are the
> durable reference.

True of the Keep column; the Drop column now deliberately has none, and says so. **Fix**: "§6's
`network-management.bash` numbers were re-verified 2026-09-14; its launcher references are
constructs, not numbers, on purpose."

## Nits

7. `DECISIONS.md:311` — *"the `stty -g` / `stty susp undef` save-and-restore pair"*. `stty susp undef`
   disables the suspend key; the restore is `stty "$_CCY_STTY_SAVED"` in `cleanup()`
   (`claude-yolo:1918`). "save-and-disable pair, restored in `cleanup()`" is what the code does
   (`:2999-3002`).
8. The `12:41` journal entry drops the `## HH:MM · CATEGORY · REF` grammar
   (`CLAUDE/PlanJournalling.md:58`) that all twelve preceding entries in the file use. Convention, not
   daemon-enforced — but it is the only entry in the plan that breaks it, and it is a `correction`.
9. `00068 PLAN.md:59-61` lists four of the five reports under Context; `ci-tool-surface.md` is
   missing — the deliverable behind the success criterion about to be ticked.
10. `claude-yolo:2091` is written by this commit into `00068 PLAN.md:147` and `00113 PLAN.md:212`.
    Today that line is `echo "  (could not list networks — …)"` — network-adjacent and plausible, the
    precise hazard §6's new note describes. It is defensible as a §4.3(c) *row key*, and Task 2.5
    pairs it with the durable description ("reached only inside the cross-engine mismatch wizard");
    the success criterion does not. Add four words there.
11. `ci-flow.md`'s derivation table maps **33** of the census's 46 sites — (f)'s 5 and (e)'s
    Dockerfile-authoring 4 / debug-layer 3 / container-manager 1 never appear in it. The new *"the
    complete list"* is complete with respect to the **table's rows** (verified: all 8 non-superseded
    rows plus the `:822` carve-out, 9 items, none missing), but the table is a partial view of the
    population it says it maps. Low stakes — the whole section is superseded and 00113 Task 0.2
    measures — but the sentence claims completeness one level above where it holds.

## Checked and clean

- **Round-4 finding 1 (Task 2.5)** — discharged and **true against source**, not just internally
  consistent. The census's `:2091` is the `Start services with podman-compose up -d? [Y/n]:` prompt:
  the census-era snapshot (`0dde4f0c`, 2026-07-31 21:23) has it at `:2085`/`:2086`, with `:2266`
  landing on `:2260` at the identical +6 offset, so the mapping is verified rather than assumed.
  Today it is `claude-yolo:2347`, inside `if [[ "$CROSS_ENGINE_MISMATCH" = true ]]` (`:2229`) →
  option 1 of the `Select option [1-4]` wizard (`:2260-2414`). Exactly one row superseded
  (`ci-required-config.md:284`), §4.3(c)'s two rows are network rows (today `:2522`/`:2557`, both
  under the `MATCHING_NETWORKS` + `-z "$SPECIFIED_NETWORK"` auto-discovery gate at `:2508`, populated
  by the project-name glob) with verdicts intact (`:306-312`), and `:2091` explicitly excluded with
  its reason (`:314-318`). Task 2.5's three-way split is correct and correctly reasoned.
- **Round-4 finding 2 (Task 3.5)** — discharged in `00068 PLAN.md:101-105`; the bullet now states
  supersession, the 13 re-opened sites, and that any returning site is not credential resolution.
  (The claim's survival elsewhere is findings 1 and 2 above, not a defect in this bullet.)
- **Round-4 finding 3 (census)** — every element verified: one superseded row
  (`ci-required-config.md:284`), rationale correction on two network rows (`:306`), `:2091` unmarked
  by design (`:314`), `ci-flow.md`'s two passages (`:23-33` struck + noted, `:69`/`:93-108`),
  `mcp-and-egress.md:3-19` three dead premises and `:288`, `host-run-verdicts.md:8`. The claims it
  makes are true.
- **Round-4 finding 4 (§6 constructs)** — every converted citation greps to what it claims, and
  nothing became unverifiable: `[[ "$net" == *"$PROJECT_NAME"* ]]` occurs exactly **four** times in
  `claude-yolo` (`:2191`, `:2213`, `:2368`, `:2699`) and zero times in `lib/`;
  `read -rp "Select option [1-4]: "` at `:2260` with `"Choose cleanup method [a/b]: "` genuinely
  beneath and nested inside it at `:2279`; `save_launch_config` has exactly one call site (`:2880`);
  `container_cmd rm -f "$CONTAINER_NAME"` is unique at `:3014` under *"Safety net: remove any
  leftover container"*; the `stty` pair at `:3000-3001`. The Keep column's eight
  `network-management.bash` numbers and `:287`/`:290`/`:586` all still resolve correctly.
- **Round-4 finding 5 / Task 0.2** — discharged; 6 + 13 = 19 makes the stated range honest. Task
  0.4's replacement citation is correct: the `exit 1` at `claude-yolo:2864` does close the branch
  printing `CCY_SKIP_NETWORK_PREFLIGHT=1 ccy` at `:2861`.
- **Round-4 nit 6** — correct and honestly handled: round 3's report does say `:2819`
  (`260914-review-00068-round3-opus-5.md:129`), the correction is a **new** 12:41 entry rather than an
  edit, and it credits the error to the author rather than the reviewer.
- **Round-4 nits 7, 8, 10** — `ci-flow.md:69` heading marked; `ci-required-config.md:321` now
  singular and names the row; "four today" verified.
- **Success criterion "The CI tool surface is specified per event"** — met. `ci-tool-surface.md` §2
  gives Class A (`push`/`pull_request`) and Class B (`issues`/`issue_comment`) with per-class tables,
  §3 server-granularity MCP, §5 four assertions each able to fail. Safe to tick, and with Task 3.4
  already ✅ leaving it unticked is the inconsistency.
- **Public-repo safety** — clean. No email, home path, RFC1918 address, hostname, container or
  project name in any added line across the seven files.
- **Plan Commit Rule / branch** — clean for this work: no untracked or modified `00068`/`00113`
  files, branch level with `origin/F44` (0/0). The dirty files in `git status` belong to Plans 00109
  and 00112, other sessions.
- **README index** — `CLAUDE/Plan/README.md:47` (00113) and `:157` (00068) both present and accurate.
- **CCY version bump** — not triggered; the diff is seven markdown files, nothing under `files/`.

## Mechanical gates

- `scripts/qa-all.bash`: **PASS**, exit 0, 829 files.
- `hooks-daemon plan-qa --sweep`: exit 1, **0 block / 2 advise** — 00046 path-existence,
  journal-freshness for twelve other plans. Neither 00068 nor 00113 appears.
- `hooks-daemon docs-qa --sweep`: exit 1, **0 block / 47 advise** — the 00068 entries are the nine
  deliberately-dead links in `00068-Journal-26-07-30.md` (append-only, answered in the 09:18/11:05
  entries) and one duplicate block in `26-07-29`. Unchanged by this commit.
- `ansible-playbook --syntax-check`: **not triggered** (no playbook in the diff); `qa-all`'s
  ansible-syntax stage ran 79 playbooks green anyway.
- `qa-helper-tests.bash`, `check_extension_compat`, extension ESLint: **not triggered** (no
  `helpers/`, `tests/helpers/`, `extensions/` or `metadata.json` in the diff); `qa-all` ran
  helper-tests (993) and extension-compat (4) green regardless.

## The decision asked for

**Not safe to mark Complete as it stands** — but the gap is small and specific. Blocking findings 1
and 2 are one edited sentence each (`00113 PLAN.md:18`, `00068 DECISIONS.md:78-79`). Land those and
the plan is safe to freeze: tick `00068 PLAN.md:156` and flip the header to `Complete` in the same
commit. The criterion itself is substantively met, re-verified this round.

Should-fix 3, 4, 5 and 6 are cheap and belong in that same commit — 3 and 4 in particular, because
both are new-or-untouched inaccuracies in files that freeze with the plan.

## Review hygiene

Nothing in the working tree was modified by this review except this report file. Sweep output went to
`/tmp/echd-captures/`.
