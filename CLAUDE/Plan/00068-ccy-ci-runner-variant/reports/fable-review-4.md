# Plan 00068 — Hostile Review, Round 4 (fable)

Reviewed against the tree at HEAD `2589c47` on branch `plan-00066-ccy-ci-runner`. Every
`file:line` cited below was re-read directly from source in this session:
`files/var/local/claude-yolo/{claude-yolo,entrypoint.sh,Dockerfile}`,
`playbooks/imports/play-claude-yolo.yml`, and — because a claim in this round turns on
consumer-repo behaviour — `/workspace/tasks/runner-ccy-{base,project}-image.yml`,
`/workspace/playbooks/runner-vm.yml`, and
`/workspace/untracked/repos/actions-hub/.github/workflows/ci.yml` (checked out at `7da52b0`,
branch `main`, dated 2026-07-29).

**Verdict up front: the loop is still not quiet.** One new BLOCKER, two new MAJORs, one MINOR.
The BLOCKER is not a citation slip — it is a load-bearing claim that turns out to describe dead
code, discovered by reading the file the plan already cites rather than re-deriving its content.
The two MAJORs are propagation gaps the same shape as D7's, in locations D7 did not check.

---

## 1. BLOCKER — the "image LABEL identity" half of "the two things ccy must supply" is unspecified, and its supporting evidence is dead code in the one consumer this plan names

**The claim, and where it lives.** After D6 retracted R10 item 1, PLAN.md:364-365 states plainly:
*"Items 2 (the image `LABEL` identity) and 3 (the CI entrypoint) are what `ccy` must supply."*
This is now the entire surviving justification for the plan's shrunk thesis — per the brief's own
framing, `ccy` contributes exactly two things to CI. Item 3 (the CI entrypoint, Decision 6) is well
specified (round2-restatement.md §2, confirmed in Finding "What holds up" below). Item 2 is not.

**(a) The LABEL key name is never specified, anywhere in this plan.** Task 3.3's Option C
(`reports/phase3-image-layering.md`, "Task 3.3 — the platform-vs-repo layer…") says: *"Every
ccy-built image — base, latest, ci, and every project image — carries a label recording the digest
of the Dockerfile it was built from, and the digest of its base."* No label **key** is ever named.
D5 (`PLAN.md:308-333`) argues from `ci.yml:97`/`:99` (D8's corrected line numbers) but likewise never
quotes the actual label key being read there. This matters because it is not a green field:

- `files/var/local/claude-yolo/Dockerfile:38,248` — the **base** image already carries
  `LABEL claude-yolo-dockerfile-hash="${DOCKERFILE_HASH}"` (unnamespaced, md5-based per
  `common.bash`'s `DOCKERFILE_HASH` computation).
- `/workspace/tasks/runner-ccy-project-image.yml:193` — lts-infra's **own** project-image build
  already writes `--label=lts.ccy.dockerfile-sha256={{ runner_ccy_project_df_sha }}` (sha256-based,
  `lts.`-namespaced), read back at `:115` via `podman image inspect --format '{{ index .Config.Labels "lts.ccy.dockerfile-sha256" }}'`.
- `actions-hub/.github/workflows/ci.yml:99` — reads the **same** key,
  `lts.ccy.dockerfile-sha256`, off a *different* image (`actions-hub-ccy:latest`), and the comment
  at `:82-83` says this was **deliberately** kept identical to lts-infra's key so "the two cannot
  drift."

So there are already **two divergent, independently-invented LABEL conventions** in production for
the same underlying fact (a Dockerfile's content identity, recorded so a staleness check can read it
without host-local state) — one on `ccy`'s own base image (`claude-yolo-dockerfile-hash`, md5), one
shared by two *consumer* repos (`lts.ccy.dockerfile-sha256`, sha256). Task 3.3 Option C proposes a
label scheme for `ccy`'s project images and `Dockerfile.ci` without citing either existing
convention, without picking an algorithm, and without saying whether the new label reuses
`lts.ccy.dockerfile-sha256` (the one two consumers already agreed on) or invents a third. If it
invents a third, this design produces exactly the fragmentation R10/D6 claim to be curing — a
project's staleness identity would then be checkable three different ways depending on which image
you inspect. This is not a nitpick: it is the one place in the plan's current, much-shrunk thesis
where "what `ccy` must supply" is still hand-waved rather than specified, and it is the brief's
explicit question ("who writes it, who reads it, what exactly is compared?") landing on a real gap.

**(b) D5/D6's own supporting citation is a dead code path in the one consumer this plan names.**
This is the sharper problem. Reading `actions-hub/.github/workflows/ci.yml:79-108` in full (not
just the two cited lines) shows the comment immediately above the cited code:

```yaml
# ccy's "own Dockerfile, else baseline" rule, kept identical to lts-infra's
# implementation so the two cannot drift. This repo ships NO `.claude/ccy/Dockerfile`, so
# it resolves to the baseline every time. The stale-image branch is carried anyway: the
# day this repo grows a Dockerfile the guard is already in place, rather than being
# something someone has to remember.
```

Verified independently, not taken on the comment's word: `ls untracked/repos/actions-hub/.claude/ccy/`
→ `No such file or directory`, on `main` at `7da52b0` (2026-07-29). **The `sha256sum` vs.
`lts.ccy.dockerfile-sha256` comparison D5 cites as proof that "CI answers the question from a
checkout plus the image" has never executed in `actions-hub`'s CI.** It is a guard for a future
state, not a working mechanism today. D5's own words: *"CI answers the question from a checkout plus
the image... and never has host-local cache state available to it at all."* That is true of the code
as written; it is not yet true of anything `actions-hub`'s CI has ever done, because the branch that
does it has never run. Round 3's own citation-discipline finding (D8) checked the **line numbers**
of this exact citation and pronounced it fixed — but D8, fable-review-3's "what holds up" section
(which independently re-verified D5), and D6/R10 all cite this code as if its execution were
established, and none of them read the four-line comment directly above it that says otherwise.

**Why this is the recurring failure mode, a seventh time.** `.claude/rules/bash-standards.md` §9: *"a
true statement about a check presented as a stronger statement about the world."* D5's claim is true
about what the *code says* it will do. It is false as a claim about what CI currently *does*. Six
prior instances of this exact shape are already recorded in this plan's own corrections (C3/C4, the
Round-1 census; E3; D1/D6's provision-time claim; D2's citation; D4; D5's own "two users" clause).
This is the same class of error, caught this time not by re-deriving a fact but by reading four
extra lines of the file already cited.

**What this changes.** It does not retract R10 item 2's *design merit* — moving `ccy`'s own
`$HOME/.cache`-based project staleness into an image LABEL is still a real, independently worthwhile
fix (Task 3.3 already says so). What it retracts is the confidence that "ccy must supply the LABEL
identity" is a settled, proven-pattern requirement for unblocking any *named* consumer today: the one
consumer this plan cites by name for this exact mechanism has never exercised it. And it surfaces a
real design gap this plan has not yet closed: which of the two (soon possibly three) label
conventions is canonical.

**What the plan must change.** Either (i) Task 3.3 Option C explicitly adopts
`lts.ccy.dockerfile-sha256` (sha256, `lts.`-namespaced) as the shared key across `ccy`'s own project
images and the consumer's images — the two existing implementations already agree on it, so this is
the "one canonical convention" outcome the plan's own framing wants — and states this is a
**deliberate cross-repo namespace** despite `ccy` itself using unnamespaced keys elsewhere
(`claude-yolo-version`, `claude-yolo-dockerfile-hash`); or (ii) it picks a `ccy`-owned key and states
explicitly that `lts-infra`/`actions-hub` would need to migrate onto it, which is new coordination
work this design-only plan has not scoped. Either way, D5's evidence citation should be corrected to
say what it actually shows: a specified, not-yet-exercised mechanism, not a proven one.

---

## 2. MAJOR — Task 7.5's ordering claim was never updated for D6, a fourth propagation location D7 did not check

D7 (Round 3) found the propagation commit `1fb9efd` missed Task 7.4 and Phase 2's intro, named both
by number, and both are now fixed (`PLAN.md:665-679`, `1161-1171`). Task 7.5 (`PLAN.md:1236-1244`) is
a **fourth** location carrying the same stale framing, and D7 did not name it:

> *"Revised order: **token-by-value → Phase 2 → unattended proof**, with Phases 3/4/5 parallel
> throughout... Phase 2 can demonstrate nothing unattended until it exists."*

This sentence still presents "proving unattended" as a load-bearing milestone in the overall
CI-design sequence. After D6, `claude-yolo` is never invoked anywhere on the CI path — so "unattended
proof" is not a CI milestone at all; it is purely a desktop-hardening deliverable (exactly as Task
7.4's own pointer note now says of C7/C8/C10, and as Phase 2's intro now says of the whole 46-site
apparatus). Task 7.5 sits one screen away from both corrected locations and was not touched.

**What the plan must change.** Add the same style of pointer used at Task 7.4 and Phase 2's intro:
state that "token-by-value → Phase 2 → unattended proof" is now a **desktop-only** ordering (still
correct on its own terms — Phase 2 does need token-by-value to exist first, and both need to exist
before unattended *desktop* launches can be verified), not a CI-readiness sequence.

---

## 3. MAJOR — the Success Criteria's "four capabilities" line understates what D6 actually found

`PLAN.md:1318-1323`:

> *"The four capabilities each have a specified interface, and it is stated which are CI-only and
> which are generally useful. Non-interactivity (Phase 2), image layering (Phase 3), MCP (Task 4.1),
> egress (Task 5.1/5.2). CI-only: none of the four — egress is explicitly desktop-usable (Decision
> 3), MCP explicitly so (Task 4.3), and only where the MCP binary comes from is CI-specific."*

This was accurate when written (Round 2), when all four capabilities were understood to serve CI to
some degree, none of them exclusively. Post-D6 it is stale in a way "CI-only: none of the four" does
not surface: **one of the four (non-interactivity) now has *zero* CI relevance**, not merely
"not CI-only." The criterion's own framing — listing all four side by side as if they differ only in
*how exclusively* they serve CI — actively obscures the asymmetry D6 introduced. A reader checking
this Success Criterion in isolation (exactly the failure mode D7's brief warns about) would come away
believing Phase 2 is a dual-purpose CI/desktop capability like the other three, when it is not.

**What the plan must change.** Restate as: three of the four (image layering, MCP, egress) serve CI
in some form; the fourth (non-interactivity) turned out, per D6, to have no CI role at all and is
retained purely as desktop hardening.

---

## 4. MINOR — E10's four citations and D5/D8's line numbers hold up; sampled and re-verified

Per the brief's instruction to verify a sample of load-bearing citations independently:

- `claude-yolo:2792` — `claude --dangerously-skip-permissions "${CLAUDE_CMD_ARGS[@]}"`. Matches.
- `entrypoint.sh:245` — `"bypassPermissionsModeAccepted": true`. Matches exactly.
- `entrypoint.sh:257-263` (cited range for the `hasTrustDialogAccepted` block) — the actual `jq`
  block runs `entrypoint.sh:259-265` (`trust_updated=$(jq ...)` through the confirming `echo`); the
  cited range starts two lines into the surrounding comment rather than the code itself. Not
  materially wrong — the block described is the right one — but not pixel-exact either.
- `entrypoint.sh:269-274` (ccy.env sourcing) and `:280-282` (`CCY_CLAUDE_WRAPPER` exec) — **exact**,
  confirmed line-for-line: the `if [ -f "$_ccy_env_file" ]; then … fi` block is precisely
  `269-274`, and the `if [[ -n "${CCY_CLAUDE_WRAPPER:-}" ]]; then / read -ra / exec` block is
  precisely `280-282`.
- `files/var/local/claude-yolo/Dockerfile:215` — `ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]`. Matches.
- D8's fix (`actions-hub/ci.yml:97` for the `sha256sum`, `:99` for the label read) — matches
  exactly on re-read; the surrounding lines (79-108) are what surfaced Finding 1 above.

No new citation-accuracy defects found in this sample; the one real problem in this area (Finding 1)
is about what the cited code *does when run*, not about whether the citation points at the right
lines.

---

## What holds up

- **Decision 6 / the CI entrypoint (item 3 of "the two things") is well specified**, including the
  OCI-inheritance question the brief asked about directly. `round2-restatement.md` §7 states the
  inheritance claim's actual scope precisely: *"proven for the templates this repo ships, plus OCI
  inheritance semantics, plus two independent live confirmations... does not prove a project could
  not override ENTRYPOINT — only that nothing in the repo's guidance suggests doing so."* Task 3.1
  (`phase3-image-layering.md`) is explicit that `Dockerfile.ci` *ships* the CI entrypoint without
  making it the default `ENTRYPOINT`, and that selection happens via the caller's own `--entrypoint`
  — consistent with `Dockerfile:215` only setting the desktop `ENTRYPOINT`, and with no other
  `ENTRYPOINT` directive anywhere in `Dockerfile.ci`'s described contents. This is the one half of
  "the two things" that is not hand-waved.
- **D6's core retraction is now fully and correctly propagated** to the two locations D7 named
  (Phase 2 intro `PLAN.md:665-679`, Task 7.4 intro `PLAN.md:1161-1171`) — re-verified in this round.
  Findings 2 and 3 above are *additional* locations, not a re-report of what D7 already fixed.
- **No over-retraction found.** Phase 2 and token-by-value are consistently described as
  "desktop-only hardening... worth doing on their own merits," never as cancelled, in every location
  checked (`PLAN.md:262-264, 366-370, 670-679, 1161-1171`).
- **The Goals section is not stale.** "Establish, with cited evidence, exactly what `ccy` lacks for
  unattended CI use" is satisfied whether the answer is "much" or "little" — it does not promise a
  particular-sized finding, so the three-round shrinkage does not orphan it.
- **The "two things" framing's first half (the CI entrypoint) is sufficient on its own terms** for
  what R10 originally called item 3. Only item 2 (this round's Finding 1) is under-specified.

---

**MATERIAL FINDINGS: yes.**
