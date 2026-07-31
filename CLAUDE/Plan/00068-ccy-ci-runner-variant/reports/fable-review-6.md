# Plan 00068 — Round 6 hostile review

Scope constraint honoured: `lts-infra` is not checked out here. Every claim below is checked
against source files present in this workspace (`files/var/local/claude-yolo/*`, `PLAN.md`,
`reports/*.md`, `JOURNAL/*.md`) or is explicitly marked unverifiable. Per this round's brief,
PLAN.md is cited by task number / correction ID, never by line; source files and reports are
cited by exact line.

---

## 1. BLOCKER — D20(2)'s "new security finding" misapplies the reachability standard it exists to

enforce: entrypoint.sh never runs on the CI path this plan itself designed, so the finding's own
CI framing is false by this plan's own prior corrections

### The claim under test

D20(2) (PLAN.md) states: *"An egress-restricted CI environment is exactly the condition that
triggers it \[`entrypoint.sh:111`'s fallback\] — and egress restriction is what Tasks 5.1/5.2 of
this plan design. Unless `api.github.com` is on the allowlist, the plan's own egress work silently
converts host-key pinning into TOFU."* It then assigns Phase 5 two obligations: add
`api.github.com` to the allowlist, and **make the fallback fatal when the caller declares CI**.

The JOURNAL entry that produced this (`JOURNAL/00068-Journal-26-07-30.md:1081-1082`) is even more
direct: *"The condition that triggers it is an egress-restricted environment — which is precisely
what Tasks 5.1/5.2 of this plan design."*

### What the trace itself gets right

I re-read `entrypoint.sh:91-143` directly. The mechanics are exactly as D20 describes:

- `entrypoint.sh:111` — `github_meta=$(curl -sL --max-time 5 https://api.github.com/meta 2>/dev/null) || github_meta=""` — unguarded, runs on every desktop-entrypoint invocation.
- `entrypoint.sh:113-115` — if `$github_meta` is empty, `github_ssh_keys` stays empty too.
- `entrypoint.sh:130-133` — the `else` branch appends `StrictHostKeyChecking accept-new` to
  `github_ssh_directives`, with only a stderr warning (`:131`), not a fatal exit.
- `entrypoint.sh:137-143` — those directives are written into `~/.ssh/config` unconditionally
  whenever the array is non-empty.

So: **the code-level trace is correct.** A failed `api.github.com/meta` fetch does downgrade SSH
host-key checking from pinning to trust-on-first-use, non-fatally. That part of D20(2) holds.

### What is wrong: the CI attribution

This plan already settled, twice, that `entrypoint.sh` — the file D20(2) traces — is **not** on
the CI path at all:

1. **D1 (Round 2)** drew the invocation table that this plan has used ever since:

   | Phase          | Who invokes                                | `claude-yolo` involved?    |
   | -------------- | ------------------------------------------ | -------------------------- |
   | Provision time | Ansible, to build the project image        | yes — build-and-exit       |
   | Job time       | the caller's own `podman run --entrypoint` | **no — never on the argv** |

   At CI job time, `claude-yolo` (the launcher that execs `entrypoint.sh` inside the container) is
   never invoked. The caller runs the image directly with its own `--entrypoint`.

2. **Decision 6 (adopted per R7, Round 2, `reports/round2-restatement.md:150-191`)** is exactly the
   consequence of that table: *"a second, small CI entrypoint beside `entrypoint.sh`"*, scoped to
   *"prepare nothing, assert nothing about trust, exec what you were told."* Its own scope
   paragraph (`round2-restatement.md:189-191`) states plainly: *"The CI entrypoint is the
   trusted-automation counterpart to the desktop entrypoint, not a security boundary. It omits the
   §2 trust assertions because it never makes them."* `entrypoint.sh`'s GitHub SSH-key setup block
   (lines 91-143) is squarely inside that omitted material — it is session-prep for an interactive
   human, not something the "exec what you were told" CI entrypoint does.

3. **Task 5.3 (PLAN.md, marked DONE, written after Decision 6)** draws the conclusion explicitly:
   *"Under the desktop entrypoint: `api.github.com` — three fatal touchpoints
   (`entrypoint.sh:14-17`, `:33-36`, `:53-56`) plus one soft (`:111`, falls back at `:130-133`)...
   Under the Decision 6 CI entrypoint the minimum boot allowlist is EMPTY — it prepares nothing and
   authenticates nothing."* Task 5.3 itself attributes `entrypoint.sh:111` to the **desktop**
   entrypoint and states in so many words that the CI entrypoint has zero GitHub touchpoints on its
   boot path.

4. **Task 5.1's own rescoping (also D1, Round 2)** removes the other half of D20(2)'s premise:
   *"Task 5.1's deliverable is desktop and provision-time only; CI egress is the caller's own
   podman argv."* The `--egress` flag Tasks 5.1/5.2 specify is a `claude-yolo` **launcher** flag.
   Per the same D1 table, the launcher is never on the CI job-time argv, so `--egress` cannot be
   the mechanism that restricts a CI job's network — the CI caller's own `podman run` invocation is.

5. **D6** (cited in `reports/cross-repo-citation-status.md:37-38`) independently establishes that
   **provisioning-time image builds don't invoke the launcher either** — they are a plain
   `podman build`, not a `podman run` through `entrypoint.sh`. So there is no lifecycle stage —
   provision-time build or CI job-time run — at which `entrypoint.sh:111` is reachable from CI.

Putting these together: D20(2)'s "new security finding" traces a real, correctly-described code
path (`entrypoint.sh:111`/`:130-133`) and then attributes it to a CI scenario that this plan's own
Decision 6, Task 5.3, and the D1 rescoping of Task 5.1 — all written and marked DONE **before**
D20 — already establish cannot occur. **D20(2) directly contradicts Task 5.3's own resolution
within the same PLAN.md document, without engaging it.**

### Why this is the eleventh instance of the meta-bug, not a new class of error

`.claude/rules/bash-standards.md` §9: *a true statement about a check presented as a stronger
statement about the world.* Here: "the fallback at `:130-133` exists and is non-fatal" (true,
verified above) is presented as "an egress-restricted **CI** environment is exactly the condition
that triggers it" (false — CI never reaches this code at all, per this plan's own adopted design).
The running count per the team lead's brief was six before D5, D10 seventh, D15 eighth, D16 ninth,
D17 tenth. **This is the eleventh.**

It is also the exact shape D16 discovered and the brief asked me to hunt for again: *"a guard one
line above a cited block can make an unconditional-sounding claim conditional."* Here the guard is
not one line above the citation — it is the entire CI/desktop entrypoint split this plan built two
rounds earlier. D20 traced the two *local* guards inside `entrypoint.sh` (lines 111 and 130-133,
correctly) but never traced the **outer** reachability question — is `entrypoint.sh` invoked from
CI at all — even though this plan had already answered it, twice, on the record. The reachability
standard was applied one level too shallow.

### Is the underlying security concern real anywhere?

Yes — on the **desktop** path, scoped correctly. `entrypoint.sh` genuinely does run for every
interactive `ccy` session, and if a user opts into `--egress` (Decision 3: *"independent of CI, and
useful on the desktop"*) with a proxy allowlist (Task 5.2) that does not include `api.github.com`,
the fetch at `:111` fails and the session's SSH config genuinely downgrades to
`StrictHostKeyChecking accept-new` for that session — silently past a stderr line few desktop users
read. That is a real, correctly-traced, **desktop-scoped** finding.

Its severity should also be stated honestly rather than assumed maximal: the downgrade only matters
against an on-path attacker able to intercept the *specific* SSH TCP connection to `github.com` at
first use — it does not disclose the user's private key, does not downgrade encryption, and GitHub's
host keys are among the most widely mirrored and easy-to-cross-check keys on the internet. Round 1
rated this MINOR when it first found the same line (`reports/fable-review-1.md:353-375`,
`reports/round2-restatement.md:317-319`, both pre-Decision-6) and explicitly said it *"does not
contradict E7's 'GitHub must be allowed or the container never starts' framing."* Nothing in D20's
retrace changes that severity assessment on the merits — it changes only the (mistaken) claim about
*where* it applies.

### Corrected claim

*"`entrypoint.sh:111`'s soft-fail path (`:130-133`) is a real, desktop-only finding: when a desktop
`--egress` session's proxy allowlist omits `api.github.com`, SSH host-key checking silently
downgrades from GitHub's pinned keys to trust-on-first-use for that session. This is unreachable
from CI: Decision 6's CI entrypoint never runs `entrypoint.sh` (D1's invocation table; Task 5.3's
own conclusion that the CI boot allowlist is EMPTY), and Task 5.1's `--egress` flag was itself
rescoped by D1 to desktop/provision-time only, so CI egress restriction is the caller's own podman
argv and has no interaction with this code path at all. Phase 5's obligation is therefore:
**Task 5.2's default desktop `--egress` allowlist should include `api.github.com`** so the fallback
essentially never fires; a 'fatal when caller declares CI' remedy is not applicable — there is no
CI invocation of this file to make fatal."*

### What Phase 5 actually owes, if this is accepted

Nothing changes about the design's soundness — this is a documentation/attribution defect, not a
missing control. The fix is confined to: (a) correct D20(2)'s framing so it says "desktop, via
`--egress`" rather than "CI"; (b) fold the `api.github.com` allowlist entry into Task 5.2's desktop
default allowlist (not a new CI-only obligation); (c) drop the "fatal when caller declares CI"
remedy — there is no CI declaration mechanism reachable from this file to gate.

---

## 2. MINOR — `label-convention-spec.md` §2 still states the pre-D17 confidence in one place D17

did not touch

D17's correction (`reports/label-convention-spec.md:165-187`) is honest and thorough: the title
line already warns *"D10 half (a) NOT closed"*, and the closing table correctly downgrades
"Convention-proliferation risk (D10)" to **Contingent**, stating explicitly that *"nothing anywhere
schedules"* the cross-repo migration steps. Round 5's BLOCKER (the PLAN.md-level "closes D10"
overclaim, Task 3.3) is fully retracted at the PLAN.md layer.

But §2 of the same document (`reports/label-convention-spec.md:50-53`) still reads, unedited by
D17:

> "This is a third *name*, which D10 warned against — so the warning is answered explicitly rather
> than ignored: **it is only acceptable because it comes with a migration that removes the second
> one** (§5)."

Read on its own — and a reader working top-down hits this well before the D17 correction at the
bottom — this states the acceptability condition **is met**: the design "comes with" a migration
that removes the old key. D17 establishes that is not true today: the migration is specified
(§5) but unscheduled and outside this plan's power to schedule (§5's own rejection condition, and
D17's "Prerequisite before this spec may be adopted" line). "Comes with" reads as present-tense and
settled; "is specified alongside, contingent on an unscheduled cross-repo migration" is what D17
says is actually true.

This is not equivalent to Round 5's finding — D17 already fixed the PLAN.md-level claim and the
document's own headline/closing status table. This is a narrower, residual instance: one sentence
inside the document's body that was true when written (before D14/D17 existed) and was not revisited
when the correction was appended, per this plan's own documented failure mode (append reads as
complete; it doesn't prompt a re-read of what it falsifies — see
`JOURNAL/00068-Journal-26-07-30.md:1103-1107`, D20's own retrospective on exactly this pattern).

**Corrected claim**: replace "it is only acceptable because it comes with a migration that removes
the second one" with "it is only acceptable *if* a migration removing the second one is scheduled
and carried out — which, per the status correction below, nothing currently does."

---

## 3. Sweep for a fifth untraced guard (D15/D16/D20's reachability standard) — nothing beyond

Finding 1

I re-traced the other citations this round's brief flagged as high-yield:

- `entrypoint.sh:14-17`, `:33-36`, `:53-56` (E7's "three fatal touchpoints") — read directly,
  confirmed genuinely unconditional `exit 1` on failure, no guard above them. Matches Task 5.3
  verbatim.
- `claude-yolo:2792` (`claude --dangerously-skip-permissions "${CLAUDE_CMD_ARGS[@]}"`, E10 row 1)
  — read `claude-yolo:2745-2792` directly: unconditional, at the tail of the single `container_cmd run` invocation, no guard.
- `entrypoint.sh:254-263` (E10 row 3, "sets `hasTrustDialogAccepted = true`, unconditionally") —
  read directly: runs unconditionally after the `.claude.json` creation block (`:239-252`),
  regardless of which branch of that `if` fired. Confirmed unconditional as claimed.
- `claude-yolo:2747` (D20(3)) — read directly, confirmed unconditional, matches D20's "clears"
  verdict.
- `entrypoint.sh:240-252`/`round2-restatement.md:100-104`'s "re-created fresh in every container"
  claim about `/root/.claude.json` — checked the mechanism: `/root/.claude.json` is a sibling of
  the `/root/.claude` symlink (`:195`, pointed at `/workspace/.claude/ccy`), not inside it, and
  nothing else in the entrypoint or Dockerfile persists bare `/root/*` files across container
  restarts. The claim holds: a fresh container has no pre-existing `/root/.claude.json`, so the
  `if [ ! -f ... ]` branch at `:240` is taken every time in practice, even though the code is
  syntactically conditional. Not a defect.

No sixth guard found beyond Finding 1. Finding 1 is unusual among this plan's guard-tracing
findings in that the untraced condition is not a line of code one level above the citation — it is
a design decision (Decision 6) two rounds earlier in the same document that the citing text failed
to cross-reference.

---

## 4. What this round did not find

- No defect in `entrypoint.sh:269-274`/`:280-282` (D20(1)) — re-read directly, confirms D20's
  "both guards are satisfiable by the checkout" finding exactly as stated.
- No new dead-code citation, no new "two-empties no-op" gap beyond what `label-convention-spec.md`
  §4 already documents.
- No evidence that D19/`cross-repo-citation-status.md`'s tier classification is wrong — spot-checked
  several Tier A citations against source and all matched.
- No issue with D20(3) (`claude-yolo:2747` clears) — confirmed.

---

## Summary

| #   | Severity | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| --- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | BLOCKER  | D20(2) attributes `entrypoint.sh:111`'s SSH host-key TOFU fallback to "an egress-restricted CI environment" and to Tasks 5.1/5.2, but this plan's own D1 (invocation table), Decision 6 (separate CI entrypoint), and Task 5.3 ("Decision 6 CI entrypoint... boot allowlist is EMPTY") — all written earlier in the same PLAN.md and marked DONE — already establish that `entrypoint.sh` is never invoked on the CI path at all, and that Task 5.1's `--egress` flag was rescoped to desktop/provision-time only. The finding is real but desktop-scoped only (triggered by `--egress` with an allowlist missing `api.github.com`), and its proposed remedy ("fatal when caller declares CI") targets a code path CI never reaches. Eleventh instance of the recurring "true statement about a check presented as a stronger statement about the world" pattern. |
| 2   | MINOR    | `label-convention-spec.md:50-53` (§2) still asserts, present-tense, that the third-key design "comes with" a migration that removes the old key — the acceptability condition D17's own closing correction says is unmet today (unscheduled, outside this plan's power). D17 fixed the document's headline and closing status table but not this earlier sentence.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| —   | note     | Sweep for a fifth untraced load-bearing guard in Tier A/B material found nothing beyond Finding 1's structural (not line-level) gap; five other citations spot-checked and confirmed exactly as claimed.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |

**MATERIAL FINDINGS: yes**
