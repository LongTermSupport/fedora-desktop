# QA Review — Plan 00068 closing work, round 4 (`e22c1fb8`)

**Reviewer**: qa-reviewer (Opus 5) · **Date**: 2026-09-14 · **Read-only review**

**Verdict**: **BLOCK** — do not mark Complete.

Round 3's findings 1, 2, 3, 4, 5, 6 and nits 7, 9, 10 are discharged **in the reports**. The fix
stopped at the report boundary: the two `PLAN.md` files that consume those reports still assert the
pre-fix state, and one of them instructs an implementer to do the exact thing the corrected note
exists to prevent. Round 3's nit 8 was addressed by re-adding launcher line numbers, and all five
launcher numbers this commit wrote were stale three minutes after the commit.

## Blocking

### 1. `00113 PLAN.md:203-205` tells the implementer §4.3(c) carries a supersession note and that §6 overrides it — the commit removed that note and preserved the verdict

```
- `reports/ci-required-config.md` §4.3(c) and §4.3(f) specified the **opposite**
  ("do not start") until 2026-09-14 and now carry supersession notes. If a
  surviving 00068 report still tells you not to start compose, §6 wins
```

After this commit, §4.3(c) carries **no** supersession note. `ci-required-config.md:306` reads
*"**RATIONALE CORRECTED, verdicts unchanged** — the two **network** rows of §4.3(c)"*, and
`:314-317` states in terms that `claude-yolo:2091` — the one §4.3(c) row that actually says
*"starts compose services unasked"* (`:234`) — is **not** in the note and its verdict stands,
because §6 item 2 requires CI to start *declared* services.

Task 2.5's catch-all is therefore aimed straight at `:2091`: it is a surviving 00068 report telling
you not to start compose, and the bullet says §6 wins. It does not. Round 3's finding 2 was *"a
`SUPERSEDED` marker on a row that still applies is a false statement carrying authority — and
`00113` Task 2.5 sends an implementer to these notes."* The marker was removed from the report and
the same false claim now lives in Task 2.5 itself, one file further from the evidence.

Verified against source: `:2091` is today `claude-yolo:2347`,
`read -rp "Start services with podman-compose up -d? [Y/n]: "`, nested inside the cross-engine
mismatch wizard block (`:2228-2500`) — one of §6's Drop constructs, exactly as the corrected note
says.

**Fix**: Task 2.5's third bullet must say §4.3(f)'s `_do_compose_start` row is superseded; that
§4.3(c) is a rationale correction with its verdicts intact; and that `claude-yolo:2091`'s "do not
start" is **not** overridden by §6. Delete or narrow the "if a surviving report still tells you not
to start compose, §6 wins" catch-all — as written it is a licence to override the row the commit
just protected.

### 2. `00068 PLAN.md:98-99` forwards `≈6` as a delivered outcome, after `ci-flow.md:101` forbids exactly that

`ci-flow.md:100-101`, added by this commit:

> The `≈6` was derived under the deferral and is superseded with it. **Plan 00113 Task 0.2 measures
> the real figure**; **nothing may forward `≈6` as a fact in the meantime.**

`00068 PLAN.md:97-103`, Task 3.5, ticked ✅, untouched by this commit:

> Requirement 1 re-derived: **about 6 reachable prompt sites, not 46, all credential resolution**.
> Compose and networking are CI requirements; what CI drops is the negotiation ([DECISIONS.md §6]).
> The site count is a derivation, not a measurement: confirm by instrumenting the CI path before
> implementing.

Both halves are in the same bullet with nothing connecting them — the reader is handed the
superseded figure and the decision that supersedes it, and told neither that the second invalidates
the first. *"All of them credential resolution"* is the specific claim §6 reopens: `ci-flow.md:96`
says 13 network/compose sites must be re-judged, and any that come back are not credential
resolution.

The bullet's own caveat ("a derivation, not a measurement") is the caveat `ci-flow.md:108-109` names
as insufficient: *"the note above is that defect landing anyway, by a route the caveat did not
cover."*

This is `PLAN.md` — the file that freezes at Complete and becomes the plan's permanent description.

**Fix**: Task 3.5's bullet must state that the ≈6 was derived under the compose/networking deferral
§6 reversed, is superseded with it, and is measured by 00113 Task 0.2.

### 3. Success Criterion `00068 PLAN.md:141-147` — the criterion is honest; the census under it is not, and it restates the over-reach this commit removed

The reword itself is **not** goalpost-moving. "Describes a live mechanism **or carries a supersession
note naming what replaced it**" is the correct standard for a historical report, and I verified it
holds: markers exist in `ci-flow.md:27`/`:93`, `ci-required-config.md:284`/`:306`,
`host-run-verdicts.md:8`, `mcp-and-egress.md:3-19`/`:288`, and `ci-tool-surface.md` is live. Say that
plainly — the criterion is defensible.

The explanatory sentence is not:

> what remained were three passages that a later owner decision reversed in part — `ci-flow.md`'s
> compose/networking deferral and the derivation resting on it, and **`ci-required-config.md`'s two
> compose rows** — each now marked rather than silently wrong.

- **Exactly one** `ci-required-config.md` row is superseded: `_do_compose_start` (`:284`,
  *"SUPERSEDED for `_do_compose_start` only"*).
- The other two rows the sentence must mean are §4.3(c)'s `:2266`/`:2301` — which the commit
  established are **network** rows, not compose rows, and which are expressly **not** reversed
  (*"verdicts unchanged"*). The actual compose row in §4.3(c), `:2091`, is marked nowhere by design.
- The census is also incomplete as a population claim: `mcp-and-egress.md:19` carries *"three dead
  premises"* and `:288` a supersession, and `host-run-verdicts.md:8` marks a superseded run. Those
  are surviving-report passages later decisions reversed in part, and the sentence says there were
  three in total.

So the plan's highest-authority statement of what was fixed re-asserts, in the durable record, the
same "two rows reversed" over-reach the commit's body text spent four paragraphs retracting.

**Fix**: keep the criterion; replace the census with what is true — one superseded row in
`ci-required-config.md` plus a rationale correction on two network rows whose verdicts stand; two
passages in `ci-flow.md`; and the pre-existing markers in `mcp-and-egress.md` and
`host-run-verdicts.md`.

## Should fix

### 4. Every launcher line number this commit wrote is already wrong — `DECISIONS.md:286-288`, `00113 PLAN.md:101`

Plan 00118 (`f5002614`, 11:57) landed three minutes after this commit (11:54) and shifted
`claude-yolo` by +19 lines. Verified against the current file:

| Cited              | Claimed                | Current content at that line                     | Correct today                                       |
| ------------------ | ---------------------- | ------------------------------------------------ | --------------------------------------------------- |
| `claude-yolo:2172` | project-name heuristic | `AUTO_CONNECT_NETWORK="$COMPOSE_NETWORK"`        | `:2191`                                             |
| `claude-yolo:2194` | project-name heuristic | a bare `fi`                                      | `:2213`                                             |
| `claude-yolo:2241` | mismatch wizard        | `echo "  • $net (Docker only)"`                  | `:2279` (`Choose cleanup method [a/b]`)             |
| `claude-yolo:2260` | mismatch wizard        | `read -rp "Select option [1-4]: "`               | correct by coincidence — now the *first* wizard prompt, where `:2241` was |
| `claude-yolo:2845` | preflight `exit 1`     | `echo "  2. Check if rootless podman is using pasta…"` | `:2864`                                        |

All five were **correct at commit time** and all are qualified — `DECISIONS.md:291-294` says
*"re-check any number before citing it"* and `00113:101` says *"at the time of writing"*. That is why
this is not blocking. But the first two now land on network-adjacent lines that look plausible to a
reader spot-checking them, which is worse than no citation: a wrong pointer that reads as right.

Round 3's nit 8 asked for corrected numbers *or* a statement of why they went. The half-life of a
launcher number in this repo is now measured in minutes. **Fix**: drop the launcher line numbers from
§6's Drop column and cite the constructs by an anchor that survives — the `$PROJECT_NAME` glob test,
`read -rp "Select option [1-4]"`, `CCY_SKIP_NETWORK_PREFLIGHT` — which is precisely what this commit
did correctly for Task 0.4's preflight and then did not generalise two files away.

### 5. `00113 PLAN.md:67-70` (Task 0.2) still quotes the superseded caveat and calibrates on the deferral-era number

> `reports/ci-flow.md` says about six, and says of itself that this is "a derivation, not a
> measurement: confirm by instrumenting the CI path before implementing". A derivation that turns out
> to be fourteen changes Phase 1's shape.

It quotes `ci-flow.md:105-107` — the caveat — and not `:93-103`, the supersession note this commit
added directly above it. Task 0.2 does measure, so no implementer is misled into skipping it; but
"fourteen" was the surprise case under the deferral, and §6 puts 13 more sites back in play, so the
range is 6–19. The task anchors the implementer to a number the report it cites has retracted.

## Nits

6. `00113 PLAN.md:103-104` and the `11:56` journal entry both say *"the review's correction to
   `:2845` … had moved again by the time I checked"*. Round 3's correction was **`:2819`**
   (`260914-review-00068-round3-opus-5.md:129`); I confirmed `:2819` was the preflight `exit 1` at
   `c1e27b20`, and 00116 (11:39/11:42) moved it to `:2845`. `:2845` is the author's own re-verified
   number, not the review's — the sentence credits the review with a number it never wrote, and
   describes as "moved" a number that was correct when written.
7. `ci-flow.md:69` — the section heading `## Requirement 1, re-derived: ~6 sites, not 46` still
   asserts the superseded figure, 24 lines above its own supersession note. A reader scanning
   headings gets the retracted claim unqualified.
8. `ci-required-config.md:321` — *"**Superseded with the rows above**"* is still plural; exactly one
   row above is now superseded.
9. `ci-flow.md:101-103`'s "these rows stand" enumeration names seven groups and omits
   `(e) select_token interactive` (1 site), one of the two rows that make up the ≈6. Not ambiguous
   enough to mislead, but an enumeration placed next to a superseded set should be complete or not be
   an enumeration.
10. `DECISIONS.md:286` cites two of the four project-name glob sites in the launcher (there are four:
    `:2191`, `:2213`, `:2368`, `:2699` today) without saying it is a sample.

## Checked and clean

- **Round-3 finding 1, report half** — discharged. `ci-flow.md:23-25` is struck through and carries a
  §6 supersession note (`:27-33`); the derivation table note (`:93-103`) marks the right four
  row-groups and its arithmetic is right: (b) 4 + (c) 4 + (e) network selection 4 + (e)
  engine/network recovery 2 = 14, minus `:822` = **13**. The lead's list — `:822`, SSH-key,
  `create_token`, token-export, Dockerfile-authoring, token-resolution, migration — is left standing
  and none of it is over-superseded.
- **Round-3 finding 2** — discharged and factually sound. Verified `:2266`/`:2301` are today
  `claude-yolo:2522`/`:2557` (`Connect to this network?`), both under the
  `MATCHING_NETWORKS`/`-z "$SPECIFIED_NETWORK"` auto-discovery gate at `:2508`, populated by the
  project-name match at `:2191`. `:2091` is today `:2347`, inside the wizard. The note's reasoning
  matches the source exactly.
- **Round-3 finding 3** — discharged. `check_project_containers_startup` is out of the note;
  `ci-required-config.md:290`'s *"the two `docker-health.bash` rows"* is accurate — the table at
  `:280-281` has exactly two (`show_zombie_container_tui`, `check_project_containers_startup`).
- **Round-3 finding 4** — discharged, arithmetic verified. `ci-tool-surface.md:109-115`: measured
  config `Bash,Edit,Write` = three names → 28, 29 − 3 = 26 (matches `DECISIONS.md:246`); class A three
  names (`:38`), class B four (`:69`); *"neither class's string has been measured"* is correct.
- **Round-3 finding 5** — discharged. `ci-tool-surface.md:165` now reads *"The per-class tool
  vocabulary, by name"* and `:172-174` requires capture per class under that class's deny string.
- **Round-3 finding 6** — discharged in design. Task 0.4 cites the preflight by name
  (`CCY_SKIP_NETWORK_PREFLIGHT`) and states why; the dropped restricted-egress condition is replaced
  (`:105-107`). Only the number decayed (finding 4).
- **Round-3 nit 7** — discharged. `DECISIONS.md:301` now reads `connect_to_network` (`:105`),
  matching the table and `lib/network-management.bash:105`.
- **Round-3 nit 9** — discharged and accurate. `ci-tool-surface.md:18-20`'s mapping checks out
  against §5: assertion 1 = flag names, 2 and 3 = tool names, 4 = MCP server vocabulary.
- **Round-3 nit 10** — discharged. The time-inversion note sits at the point of inversion.
- **`DECISIONS.md §6` Keep column** — all eight numbers verified correct against the **current**
  `lib/network-management.bash`: `get_expected_network_name:15`, `connect_to_network:105`,
  `network_has_running_containers:464`, `has_compose_files:488`, `_do_compose_start:545`,
  `_compose_already_running:653`, `offer_compose_start:707`, `ensure_network_dns:743`. Also
  `read -rp` at `:586`, menus at `:287`/`:290`, `check_project_containers_startup` at
  `docker-health.bash:500`.
- **Success Criterion `PLAN.md:151`** — *"The CI tool surface is specified per event"* is
  substantively met and **safe to tick** at the status flip: `ci-tool-surface.md:31-77` gives two
  classes with per-class present/absent tables, `:79-95` server-granularity MCP with
  `--strict-mcp-config`, `:121-158` four assertions each specified to be able to fail. Its Task 3.4
  (`PLAN.md:104`) is already ticked, so leaving the criterion unticked is the inconsistency, not
  ticking it.
- **Journal honesty (`11:56`)** — honest, not flattering. Names each defect in the first person and
  as its own (*"I swept `ci-required-config.md` because that is where I had been reading"*, *"Round 2
  asked for one function and I marked two, disclosing it in neither the journal nor the commit
  message"*, *"I wrote a paper derivation into the paragraph whose thesis is 'capture, do not
  compute'"*). Its only slip is nit 6.
- **Public-repo safety** — clean. No email, home path, RFC1918 address, hostname, container or
  project name in any added line across all 8 files.
- **Plan Commit Rule** — clean for this work. No untracked `CLAUDE/Plan/00068`/`00113` files; branch
  level with `origin/F44` (0/0). The two modified files in `git status` are Plan 00109's, another
  session's.
- **README index** — `CLAUDE/Plan/README.md:47` (00113) and `:157` (00068) both present under Active
  Plans, both accurate.

## Mechanical gates

- `qa-all.bash`: **PASS**, exit 0, **825** files.
- `plan-qa --sweep`: exit 1, **0 block / 2 advise** — 00046 path-existence and journal-freshness;
  neither in 00068 or 00113.
- `docs-qa --sweep`: exit 1, **0 block / 47 advise** — the 00068 entries are the 9 append-only dead
  links in `00068-Journal-26-07-30.md` and one duplicate-block in `26-07-29`, both pre-existing and
  answered in the journal.
- `ansible-playbook --syntax-check`: **not triggered** — 8 markdown files, no playbook. `qa-all`'s
  `ansible-syntax` stage ran 79 playbooks green regardless.
- `qa-helper-tests.bash`, `check_extension_compat`, extension ESLint: **not triggered** — no
  `helpers/`, `tests/helpers/`, `extensions/` or `metadata.json` in the diff. `qa-all` ran
  helper-tests (943) and extension-compat (4) anyway, both green.
- CCY version bump: **not triggered** — nothing under `files/var/local/claude-yolo/` is in
  `e22c1fb8`.

**Minimum to flip to APPROVE**: findings 1, 2 and 3. Finding 1 is the one that reaches an
implementer. Findings 4 and 5 are small edits in two files and should ride along.

## Review hygiene

Nothing in the working tree was modified by this review except this report file. Sweep output went to
`/tmp/echd-captures/`.
