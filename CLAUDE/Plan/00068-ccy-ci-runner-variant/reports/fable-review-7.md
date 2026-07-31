# Plan 00068 — Round 7 hostile review

**Scope note.** `lts-infra` is not checked out in this workspace (confirmed:
`/workspace/untracked/repos/` lists this repo and `actions-hub` only). Every Tier C claim
in `reports/cross-repo-citation-status.md` is therefore UNVERIFIABLE IN THIS WORKSPACE and
is not re-checked or ranked here. Tier A/B — `files/var/local/claude-yolo/*`, `playbooks/`,
`vars/`, and `actions-hub` — was worked from source.

**A note on branch state.** The branch moved during this review. The brief cites HEAD
`1febd68` (Round 6 corrections, D21–D22). By the time this review started, two more
commits existed — `bf13259` ("predicted the propagation defect, went looking, found it")
and `e9642d2` ("Plan 00068 D23: a correction that asserted it had filed something it had
not") — plus an uncommitted working-tree fix to `reports/round2-restatement.md` (a stray
citation line-number and an un-propagated D2 retraction). This review audits the document
as it stands now, including that material, since that is what is actually on disk. None
of it was produced by this review; it predates it. Where it bears on the brief's targets
it is reported as **verified**, not as a new finding.

---

## 1. D21 — verified, holds up

**The code trace.** `entrypoint.sh:111` (`curl … https://api.github.com/meta`) is
unguarded; on failure `github_ssh_keys` is empty, the `else` at `:130-133` fires and
appends `StrictHostKeyChecking accept-new`. Confirmed by direct read
(`files/var/local/claude-yolo/entrypoint.sh:111-133`). D20's trace was accurate; this was
never in question.

**The retraction (CI → desktop-only).** Confirmed from source, not from the plan's own
prior text:

- Task 5.3 (PLAN.md, DONE block): *"Under the Decision 6 CI entrypoint the minimum boot
  allowlist is EMPTY"*, with D1's own gloss immediately under it: *"no ccy code is on a
  CI job's path… with the CI entrypoint it need contain nothing at all."*
- D1 (Round 2 corrections): *"Job time: the caller's own `podman run --entrypoint` …
  `claude-yolo` involved? no — never on the argv."* Task 5.1 is explicitly rescoped to
  "desktop and provision-time only" on this basis.
- Checked the repo for an actual `Dockerfile.ci` / CI-entrypoint script: **none exists**
  (`find … -iname "*ci*"` under `files/var/local/claude-yolo/` returns nothing, and
  `Dockerfile.ci`/`ci-entrypoint` do not appear anywhere under `files/` or `playbooks/`).
  Decision 6 is design-only, exactly as D21 states — this is not an assumption, it is an
  absence confirmed by search.

Given that: on the *designed* path (Decision 6 implemented, caller invokes it correctly),
`entrypoint.sh` is desktop-and-provision-time-only, and CI egress restriction (Task
5.1/5.2, caller's own podman argv) genuinely cannot interact with it. The retraction is
correct, not an over-swing — D20(2)'s original CI attribution really was wrong, for the
reason D21 gives.

**The residual (mis-selected `--entrypoint` runs the desktop entrypoint anyway).**
Checked against `reports/round2-restatement.md:61-68`, which quotes the consumer's
`run-sandbox.sh:375-402`: *"A command placed after the image name does NOT override an
ENTRYPOINT — podman appends it as ARGUMENTS to it… The platform's entrypoint was never
reached."* That is a real, cited, production occurrence of exactly the failure mode the
residual describes (a caller believing it selected a different entrypoint and actually
running the desktop one). The residual is correctly scoped: it is not reinstating D20(2)'s
CI framing as an obligation on Tasks 5.1/5.2 — it is naming a *caller bug class* outside
`ccy`'s control (a downstream repo mishandling `--entrypoint`, already a Non-Goal to fix:
*"No work on the `actions-hub` side"*), used as motivation for Decision 6 (give callers a
correct primitive so they stop hand-rolling it). That disposition is sound. **This is now
also correctly filed at Task 3.1** (via D23, see below) rather than living only in the
correction block — so the "quietly reinstate CI" risk the brief asked about does not
materialise; if anything the opposite defect existed for a short window (D23 fixes it).

**Severity (MINOR).** Checked `reports/fable-review-1.md:353-375` (§7, "MINOR — E8 is
accurate but incomplete…"): Round 1 rated the same `entrypoint.sh:111`/`:130-133` pair
MINOR, for the same reason (non-fatal, soft-fail fallback). D21's re-rating to MINOR is a
return to that baseline, correctly cited, and the stated reasoning (on-path attacker
required, first-connection-only exposure, no key material disclosed) is unchanged by
anything since. Defensible.

**"Phase 5 owes exactly one thing."** Checked whether this undercounts. D20(2) originally
billed Phase 5 for two items: the allowlist entry, and "fatal when the caller declares
CI." D21 keeps the first (correctly — it is now also recorded as an outstanding
obligation directly on Task 5.2, PLAN.md:1037-1053, not only in the correction block) and
withdraws the second because there is no CI declaration mechanism on this file's path to
gate. I looked for a *third* obligation the plan quietly drops along with the CI framing —
specifically, whether the "wrong shape under this repo's fail-fast rule" language the plan
uses (Task 5.2 addendum, and again in the D21 block) implies an owed fix to
`entrypoint.sh`'s non-fatal fallback itself, independent of CI. I traced this seriously
enough to check `fable-review-6.md:140-146` ("What Phase 5 actually owes, if this is
accepted") — Round 6's own hostile pass considered this exact question and reached the
same three-item list D21 states: correct the framing, fold the allowlist entry into Task
5.2, drop the CI-conditioned fatal remedy. No fourth item, and the codebase has a standing
precedent this plan itself cites elsewhere (`common.bash:463-466`'s `|| echo "unknown"`
fallback) for non-fatal degradation on a boot-time network probe. I considered this a
candidate finding and am not raising it: it is a defensible design-scope judgment that a
hostile round already tested, not a claim contradicted by the document's own text.

## 2. D22 — verified, holds up

`label-convention-spec.md` §2 (line 51-53 as currently on disk) now reads: *"it is only
acceptable **if** a migration removing the second one is scheduled and carried out (§5)"*,
with an inline correction note pointing at D22 and the status-correction section. Checked
the whole document end-to-end for any other place asserting the pre-D17 confidence: the
title (line 1: *"D10 half (a) NOT closed"*), §5's own rejection condition, and the closing
**⚠ STATUS CORRECTION (D17)** table (Contingent / Unproven) all agree with the corrected
§2 sentence. Checked `PLAN.md:938-939` (Task 3.3, the other place this spec's status is
summarised) — also consistent, carries its own inline retraction of "closes D10's half
(a)". No stray instance found anywhere else in the plan folder (grepped for `closes D10`,
`closes half (a)`, and the exact "comes with a migration" phrase across the plan tree —
the only remaining hits are inside review reports quoting the *original*, pre-correction
wording as evidence, which is correct and expected, not a live assertion).

## 3. Secondary target — the three-row table in `cross-repo-citation-status.md`

The table (added with D21, lines 128-132) decomposes the miss into three questions: "can I
read the source" (the tier table), "does the cited line run on the claimed path" (D15's
reachability standard), and "is the enclosing file on that path" (answered by neither).

Checked this against D15's actual wording (PLAN.md:1436-1440): *"cited to a `file:line`
that is **reachable on the path the claim describes**."* That phrasing is broad enough
that, read literally, it already covers file-level reachability — D15 does not say
"line-level" or exclude the enclosing file. So the three-row table's middle cell ("does
the cited line run") is a narrower gloss on D15 than D15's own text, and the table's
"Neither" answer for the third question is really describing how D15 was *applied* (D16
traced an in-file conditional; D20 traced two in-file guards) rather than a gap in what
D15 *says*. D21 itself gets this right in its own prose: *"The reachability standard
applied one level too shallow"* — an application failure, not a categorical omission.

This is a real but narrow tension between the table's tidy three-question framing and
D21's own more accurate "applied too shallow" framing. I do not rank it as a finding: the
table does not misstate any source-code fact, and as a description of this plan's actual
citation *practice* (three consecutive applications of the standard that all stopped at
in-file guards) it is accurate. It reads as a slightly more flattering account of *why*
the miss happened than "the standard wasn't followed all the way" would, but both
readings arrive at the same corrective action, and this is exactly the "arguably could be
framed better" territory the brief warned against promoting to a ranked finding.

## 4. Meta-bug hunt (twelfth+ instance)

D23 already exists on the branch as a self-found twelfth instance (PLAN.md:1635-1641): a
correction that asserted in the present tense *"it is recorded as a `Dockerfile.ci`/
Decision 6 motivation"* when nothing had been recorded there. I verified this the way the
brief asks — checked Task 3.1 in the version of PLAN.md before `e9642d2`
(`git show e9642d2 -- PLAN.md`) and confirmed the residual genuinely did not exist there
before that commit; D23's self-diagnosis is accurate, and the fix (actually adding the
paragraph at Task 3.1, PLAN.md:879-894) is correctly filed and consistent with what the
residual claims. I looked for a thirteenth instance in the same hunting mode — re-reading
every corrected sentence introduced since D17 for a claim about the *record* (as opposed
to the world or the plan) that isn't itself true — and did not find one. The
`round2-restatement.md` working-tree edit (E10 row 3's line range `257-263` → `255-263`,
and D2's citation withdrawal now propagated into the source document itself rather than
only the PLAN.md correction block) is the same species of fix, already applied and
verified correct against `entrypoint.sh:255-263` by direct read.

## 5. Propagation-defect check (D7, D9, D11, D12, D14, D22, D23…)

Applied the brief's instruction literally: reports carry trailing correction sections, so
body text preceding one is not automatically stale. Checked `label-convention-spec.md` end
to end (above) and `cross-repo-citation-status.md` end to end (its "Correction to the Tier
B result (D21)" section at the bottom is a genuine trailing append, and the body above it
— the D20 resolved table — is not contradicted by it, only re-scoped). No new propagation
gap found in either document.

---

## Summary

D21 and D22 both verify against source. The residual and severity claims in D21 are
correctly scoped; "Phase 5 owes exactly one thing" was checked seriously as a possible
undercount and found to be a considered, already-hostile-reviewed design boundary rather
than a dropped obligation. D23, found and applied earlier in this same window by other
work on the branch, is itself correct and does not require further correction. The
three-row citation-status table is a defensible, if slightly tidier-than-strictly-accurate,
description of the same failure D21 already characterises more precisely in its own prose
— not a misdescription worth ranking.

No BLOCKER, MAJOR, or MINOR finding from this round.

MATERIAL FINDINGS: no
