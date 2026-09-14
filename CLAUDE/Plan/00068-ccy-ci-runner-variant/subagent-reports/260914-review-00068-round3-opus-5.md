# QA Review — Plan 00068 closing work, round 3 (`c1e27b20`)

**Reviewer**: qa-reviewer (Opus 5) · **Date**: 2026-09-14 · **Read-only review**

**Verdict**: **BLOCK** — do not mark Complete. Round-2 findings 2, 3, 5, 6, 7, 8, 9 and 10 are
discharged. Finding 1's fix reaches the wrong rows in one report and misses the other report
entirely; finding 4's fix is right in design and carries one unmeasured number. Success Criterion
`PLAN.md:141` is still ticked and still false.

## Blocking

### 1. `reports/ci-flow.md:23-25` is a surviving report specifying the retracted mechanism — unmarked, and Decision 6 points a reader straight at it

```
Networking and compose are **deferred entirely** (lts-infra Plan 00030: the case study does not
need them), so steps 5–7 pass no `--network` and the flow never enters `:1789-2498` or the
preflight at `:2518-2593`.
```

`DECISIONS.md:279` says the opposite in the owner's words, and `DECISIONS.md:270` opens §6 with
*"Full flow in `reports/ci-flow.md`"* — so §6's own pointer lands on the contradiction.
`ci-flow.md` carries **no** supersession marker anywhere
(`grep -rn -i "superseded\|retracted" reports/` returns markers in `host-run-verdicts.md`,
`mcp-and-egress.md` and `ci-required-config.md` only). The sweep covered one of the two
contradicted reports.

Ordering makes it worse, not better: `9f514222` (Decision 6) landed at **06:12:12**, `e67abde2`
(this line) at **07:24:55**, same day, and `git merge-base --is-ancestor 9f514222 e67abde2`
confirms the decision was already in the branch. This was written contradicted, not overtaken.

It is load-bearing, not decorative. The derivation table at `:66-79` marks four groups
"**No** — not on the CI path" — (b) 4 sites, (c) 4, (e) network selection 4, (e) engine/network
recovery 2 — of which 13 are excluded *because* compose and networking were deferred. That
exclusion is what produces "≈6 reachable sites", and `00113 PLAN.md:67-70` (Task 0.2) forwards the
six. §6 restores the kept half of that block to the CI path, so the derivation must be re-run, not
inherited.

`00113 PLAN.md:196-197`'s catch-all — *"If a surviving 00068 report still tells you not to start
compose, §6 wins"* — is a good instinct and partially covers Task 2.5, but it says nothing about
*networking deferred* and does not reach Task 0.2, which cites `ci-flow.md` directly.

**Fix**: a supersession note on `ci-flow.md:23-25` and on the four table rows at `:68-73`, pointing
at `DECISIONS.md §6`, stating that the ≈6 figure was derived under the deferral and is superseded
by Task 0.2's measurement.

### 2. The `§4.3(c)` supersession note refutes itself one sentence later — `reports/ci-required-config.md:300-306`

The note supersedes rows `claude-yolo:2091`, `:2266`, `:2301`, then says: *"Auto-**discovery** of a
network is still wrong for CI — §6 drops the project-name heuristic, the mismatch wizard and the
selection menus."* All three superseded rows **are** those constructs:

- `:2266`/`:2301` → today `claude-yolo:2477`/`:2512`, under the banner
  `"Docker Network Auto-Detection"`, gated at `:2463` on
  `[ ${#MATCHING_NETWORKS[@]} -gt 0 ] && [ -z "$SPECIFIED_NETWORK" ]`. `MATCHING_NETWORKS` is
  populated at `claude-yolo:2146` by `[[ "$net" == *"$PROJECT_NAME"* ]]` — the project-name
  heuristic §6's Drop column names.
- `:2091` → today `claude-yolo:2302`, reached only inside the cross-engine mismatch wizard
  (`Select option [1-4]` at `:2215`, `Choose cleanup method [a/b]` at `:2234`) — also §6's Drop
  column. Its stated reason is *"starts compose services unasked"*, which §6 does not reverse: §6
  item 2 requires CI to start **declared** services, not discovered ones.

Only the two connect rows carry the *"opposite of a restricted egress posture"* rationale the note
argues against; `:2091` never did, so the note supersedes three rows on a justification that fits
two — and the verdict on all three ("wrong for CI") survives §6 intact.

A `SUPERSEDED` marker means "this no longer applies". Here it does apply, and
`00113 PLAN.md:195-197` sends the implementer to these notes.

**Fix**: this is a rationale correction, not a supersession. Leave the three verdicts standing;
replace the dead egress reason on `:2266`/`:2301` with "§6 drops auto-discovery"; drop `:2091` from
the note.

### 3. `check_project_containers_startup` was swept into the supersession with no authority for it — `reports/ci-required-config.md:284-285`

The note reads *"SUPERSEDED for `_do_compose_start`, **and for the
`check_project_containers_startup` row above it**"*, justified by *"§6's keep column names
`_do_compose_start` explicitly"* — which says nothing about the other row.

§6's table is scoped to `lib/network-management.bash`; `check_project_containers_startup` is
`docker-health.bash:500` and has nothing to do with compose. It lists **CCY's own containers for
the project** (`--filter "name=${project_name}_${suffix}"`, `:508`) and offers
`[c] Continue / [s] Stop all / [m] Manage / [q] Quit` (`:542-545`). Its row's reason — *"a CI job
declares its own services; starting found ones is unasked-for state"* — is exactly what `00113`
Task 2.7 affirms ("CI **declares** its services rather than discovering them").

Round 2 asked for `_do_compose_start` only. Neither the journal at `11:10` nor the commit message
discloses the extension.

**Fix**: remove `check_project_containers_startup` from the note.

### Consequence for the status flip

`PLAN.md:141` — *"Every surviving report describes a live mechanism"* — remains ticked ✅ and false,
for finding 1 directly and findings 2/3 in the other direction. It must be corrected or unticked
before Complete.

## Should fix

### 4. `ci-tool-surface.md:108-109` attributes a measurement to class B that was not made under class B's deny string

> "removing `Bash` **for class B** adds `Glob` and `Grep`, so subtraction would predict 26 names
> where 28 were measured"

`DECISIONS.md:246` records the measurement as `--disallowedTools Bash,Edit,Write` — **three** names
→ 28. Class B's must-be-absent list (`ci-tool-surface.md:68`) is `Bash`, `Edit`, `Write`,
`NotebookEdit` — **four**. 29 − 4 = 25, not 26, and no measurement exists under class B's actual
string. Class A's list is a different three (`Edit`, `Write`, `NotebookEdit`) and withdraws no
capability, so neither class matches the measured config.

`00113 PLAN.md:76-79` repeats the 26/28 pair without the class attribution and is fine; only §4
binds it to class B. The argument survives; the number presented as class B's does not — in the
paragraph whose whole thesis is "capture, do not compute".

**Fix**: drop "for class B", or state that the measured config was `Bash,Edit,Write` and neither
class's string has been measured yet — which is Task 0.3's job.

### 5. `ci-tool-surface.md:157` still names the *default* vocabulary as the open item — the one thing §1 now says must not be captured

§6 heads its bullet *"The concrete **default** tool vocabulary"* and closes *"Plan 00113 Task 0.3
captures **them** from the binary about to run"*. The §1 note added by this commit says the expected
set must be *"captured per class, with that class's `--disallowedTools` string applied, **not
computed from a default vocabulary**"*, and Task 0.3 now reads *"never just the default"*. §6 was
not touched by this commit (the diff has two hunks, §1 and §4), so the "what this does not settle"
section — where a reader goes for exactly this — still describes the superseded model.

### 6. `00113 PLAN.md:100` cites `claude-yolo:2597` for B3's expected `exit 1`; today that line is a compose-file glob

`claude-yolo:2597` is `if [ -f "$pattern" ]; then` inside the compose-file scan at `:2596-2600`.
The preflight block starts at `:2732` and its `exit 1` is at **`claude-yolo:2819`**. The citation is
verbatim from `5785c55f^:reports/hardware-proof-checklist.md:39` (2026-07-31) and has drifted with
the launcher.

This commit added a *"re-check the numbers before citing them"* caveat to `DECISIONS.md §6` and then
wrote a new, unverified launcher line number into a live Phase 0 task in the same commit. The
original also conditioned it on *"restricted egress"*, a posture Decision 8 dropped — worth saying
which condition replaces it.

## Nits

7. `DECISIONS.md:299` still says `connect_to_network` (`:98`) while the table nine lines above now
   says `:105`. The re-verification pass corrected the table and not the prose in the same section;
   before this commit both were consistently wrong, now they disagree. Correct value: `:105`.
8. §6's Drop column lost its line citations rather than gaining corrected ones — the mismatch
   wizard's `:1973-2243` and the menus' `:271`, `:274` were deleted. The menus are
   `network-management.bash:287`/`:290` today; the wizard reference pointed at `claude-yolo`, not
   the file the table names, which is presumably why it went. Worth saying so in the caveat rather
   than silently dropping.
9. `ci-tool-surface.md:19` — *"Every assertion below is on **tool names**, never on a count"* —
   fixes what round 2 named, but assertion 1 is on **flag** names, not tool names. Residual
   imprecision, not the defect that was reported.
10. The `11:10` journal entry sits below entries stamped `11:20`, `11:35` and `11:45`, so the log
    now reads backwards. Its closing paragraph explains why (the earlier times were chosen, this one
    is `date -u`, and the commit is `11:13:41 UTC`), so it is honest — but a one-line note at the
    point of inversion would spare the next reader the puzzle.

## Checked and clean

- **Round-2 finding 2** — Task 0.4's rewrite (`00113:84-105`) is faithful to
  `00068-Journal-26-07-31.md:118-131`. Both constraints carried verbatim in substance: the exit code
  does not discriminate (`:121-125`), nothing scripts the token store (`:126-128`), and `:130`'s
  *"interactive investigation with the owner, not a hand-over script"* is quoted as the task's
  opening. Marked 🚫.
- **Round-2 finding 3** — discharged. `00113:102-105` scopes the substitution to Task 0.2 and states
  why Task 3.2 may not serve.
- **Round-2 finding 4 (design half)** — coherent end to end across the four places that matter: §1's
  new note, §2's class-B row `:69`, §4's two-artefact rewrite `:103-111`, §5 assertion 2 `:124-126`,
  Task 0.3 `:72-82` and Task 2.1 `:149-155` all now say *captured per class under that class's deny
  string*. Only §6 and the class-B number (findings 4, 5) lag.
- **Round-2 finding 6** — verified against source. Task 2.5's keep list is §6's keep column exactly,
  all seven, including the two round 2 said were missing. The drop of `network-management.bash:586`
  is real: `read -rp "Start services with $compose_name up -d? [Y/n]: "` inside `_do_compose_start`
  (`:545-601`). No other `read` in a kept function is reachable when a network is declared —
  `connect_to_network`'s prompts at `:165`, `:287`, `:290` are all under `[ -z "$network_name" ]`
  guards (`:148`, `:187`).
- **Round-2 finding 9** — all eight `DECISIONS.md §6` table numbers verified correct against
  `lib/network-management.bash`: `get_expected_network_name:15`, `connect_to_network:105`,
  `network_has_running_containers:464`, `has_compose_files:488`, `_do_compose_start:545`,
  `_compose_already_running:653`, `offer_compose_start:707`, `ensure_network_dns:743`.
- **Round-2 findings 7, 8, 10** — Task 2.9 is now Task 0.7 in Phase 0 and gone from Phase 2b;
  `CLAUDE/Plan/README.md:155` names three live mechanisms and no retracted one; the new journal entry
  is `date -u`.
- **Success Criterion `PLAN.md:145`** — substantively met and **safe to tick** at the status flip
  once the blocking items are cleared: two classes with per-class present/absent tables (`:30-76`),
  server-granularity MCP (`:78-94`), four assertions each specified to be able to fail (`:113-150`).
- **`claude-yolo:822` reuse row** — correctly left standing (`ci-required-config.md:305-306`).
  Nothing in §6 touches launch-config reuse; the prompt is `claude-yolo:922`,
  `"Use same configuration? [Y/n]"`.
- **§4.3(b)'s two compose rows** — checked and defensible, not findings. `:2428`→`claude-yolo:2636`
  is gated at `:2593` on `[ -z "$SPECIFIED_NETWORK" ]`, a discovery path CI does not take;
  `:2818`→`claude-yolo:3105` is gated at `:3082` on `CCY_COMPOSE_WAS_STARTED`, which is exactly §6's
  "tear down only what CI started".
- **Journal honesty** — the `11:10` entry names each defect as its own ("Three more, all mine",
  "I quoted the sentence that supported the task and skipped the two that ruled out its method",
  "I generalised from the wrong section") without flattering itself. Its only gap is scope: it
  records the supersession as narrower than the edit actually was (findings 2, 3).
- **Public-repo safety** — clean. No non-`example.com` email, home path, RFC1918 address, hostname,
  container or project name in any added line. The only address in `git show` is the commit author
  trailer.
- **Plan Commit Rule** — clean. Working tree clean, branch level with `origin/F44`, both plan README
  rows present and accurate (`:45`, `:155`).

## Mechanical gates

- `qa-all.bash`: **PASS**, exit 0, **817** files (813 at the lead's run; other sessions' work has
  landed since).
- `plan-qa --sweep`: exit 1, **0 block / 2 advise** — 00046 path-existence and journal-freshness;
  neither in 00068 or 00113.
- `docs-qa --sweep`: exit 1, **0 block** — the 9 append-only dead links in
  `00068-Journal-26-07-30.md` and the `26-07-29` duplicate-block, both answered in the journal.
- `ansible-playbook --syntax-check`: **not triggered** — 7 markdown files, no playbook. `qa-all`'s
  `ansible-syntax` stage ran 79 playbooks green regardless.
- `qa-helper-tests.bash`, `check_extension_compat`, extension ESLint: **not triggered** — no
  `helpers/`, `tests/helpers/`, `extensions/` or `metadata.json` in the diff. `qa-all` ran
  helper-tests (900) and extension-compat (4) anyway, both green.
- CCY version bump: **not triggered** — nothing under `files/var/local/claude-yolo/` is in the diff.

**Minimum to flip to APPROVE**: findings 1, 2 and 3, plus a correct `PLAN.md:141`. Findings 4–6 are
small edits in two files and should ride along.

## Review hygiene

Working tree otherwise unchanged by this review; sweep output went to `/tmp/echd-captures/`. This
report file is the only thing this review created.
