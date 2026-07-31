# Plan 00068 — Round 5 hostile review

Primary target: `reports/label-convention-spec.md` (written to close D10's half (a), never before
reviewed). Secondary: D14's own claim, a sweep for a sixth propagation-gap instance and an eighth
meta-bug instance, and a citation-discipline sample.

Every source claim below was re-read directly, not carried over from prior rounds' citations.
Files checked: `files/var/local/claude-yolo/Dockerfile`, `files/var/local/claude-yolo/claude-yolo`,
`files/var/local/claude-yolo/lib/common.bash`, `files/var/local/claude-yolo/entrypoint.sh`
(all read in full or by cited range), plus `/workspace/untracked/repos/actions-hub/.github/workflows/ci.yml`.
**`lts-infra` is not checked out in this environment** — every claim that depends on it is marked
unverified below rather than asserted either way, per this round's brief.

---

## 1. BLOCKER — D14's "closes D10's half (a)" overclaims what `label-convention-spec.md` itself says is unresolved

`label-convention-spec.md` §5 (Migration) ends with its own stated rejection condition, verbatim:

> "Step 4 is what makes this a replacement rather than an addition. **If step 4 is not scheduled,
> this specification should be rejected** — a third live convention is strictly worse than the two
> that exist now."

Step 4 is *"`lts.ccy.dockerfile-sha256` is then emitted by nobody and read by nobody, and is
deleted"* — action inside `lts-infra` and `actions-hub`, two repos this plan's own Non-Goals
forbid touching (*"No work on the `actions-hub` side… tracked in that repo's own plan"*). Step 3
(both repos switching their `ci.yml`/task to the new key) is a precondition for step 4 and is
equally outside this plan's power to schedule.

**Nothing in the plan tree schedules steps 3–4.** I grepped the whole plan folder for `00026`,
`runner-ccy-project-image.yml`, and `scheduled` (see below) — the only reference to a concrete
lts-infra plan number is **D6**, about a *different* question (whether that file should be
*deleted*), and D6 is explicit that the question is *"that repo's call, not this plan's"*. No
document anywhere — not `label-convention-spec.md`, not D14, not the README index row — names a
tracking item in `lts-infra` or `actions-hub` for the label migration itself.

```
$ grep -n "00026\|runner-ccy-project-image\|scheduled" reports/*.md PLAN.md
reports/label-convention-spec.md:87:   `runner-ccy-project-image.yml`.
reports/label-convention-spec.md:113:  3. lts-infra's `runner-ccy-project-image.yml` and actions-hub's `ci.yml` switch from …
reports/label-convention-spec.md:118:  Step 4 is what makes this a replacement… **If step 4 is not scheduled, this specification should be rejected**
PLAN.md:215: R10 — three concrete gaps block lts-infra deleting its duplicate (its Plan 00026 Task 3.3)
PLAN.md:372-375: … lts-infra Plan 00026 Task 3.3's premise should be re-examined against this; it is that repo's call, not this plan's.
```

So by the document's own stated criterion, the specification currently **should be rejected** —
yet D14 (the authoritative PLAN.md text, Task 3.3) states unconditionally:

> "Option C is now actually specified — see `reports/label-convention-spec.md`, which **closes
> D10's half (a)**."

That is a true statement about the *design* (keys, algorithm, writer, reader, comparison are all
now named) presented as a stronger statement about the *state of the world* (that D10's underlying
worry — a third live convention — is resolved). It is not resolved: as written today, adopting this
spec **creates** a third key name (`claude-yolo-project-dockerfile-sha256` /
`claude-yolo-project-base-version`) alongside the still-live `lts.ccy.dockerfile-sha256`, with no
committed mechanism to retire the old one. §2 of the spec is honest about this in isolation
(*"This is a third name, which D10 warned against… it is only acceptable because it comes with a
migration that removes the second one"*) — but D14 drops that condition when it reports the outcome
upward into PLAN.md.

**This is the eighth instance of `bash-standards.md` §9's recurring failure mode** (`.claude/rules/bash-standards.md`
§9: *"a true statement about a check presented as a stronger statement about the world"*) — here
transposed from code to plan-document review: a true statement about a *design* ("the convention is
now specified") presented as a stronger statement about a *risk* being closed ("closes D10's half
(a)").

**What the corrected claim should be:** "Option C's design is now specified — keys, algorithm,
writer, reader, comparison. **The convention-proliferation risk D10 raised is not closed**: it
depends on a migration in two repos this plan cannot act on, and nothing currently tracks that
migration. Until a companion item exists in `lts-infra`/`actions-hub`'s own plans, treat this as
*specified in principle, contingent in practice* — the same honest three-part status D10 itself used
for the proof half, now needed for the proliferation half too."

---

## 2. Verified correct — §1's "not rivals" reframing of Round 4's table

Round 4 (D10) presented two labels side by side (`claude-yolo-dockerfile-hash` on the base image,
`lts.ccy.dockerfile-sha256` on the project image) under the heading *"two already-divergent `LABEL`
conventions"*, which reads as though they compete for the same role.

I re-read the source rather than trust either round's framing:

- `Dockerfile:36` — `LABEL claude-yolo-version="2.22"` (unconditional, base stage).
- `Dockerfile:248` — `LABEL claude-yolo-dockerfile-hash="${DOCKERFILE_HASH}"` (end of `full` stage).
- `common.bash:456-469` — `validate_container_version()` reads exactly these two labels off
  `claude-yolo:latest` and compares against `REQUIRED_CONTAINER_VERSION` (`claude-yolo:39`,
  `"2.22"` — matches the Dockerfile label) and a freshly computed `md5sum … | cut -c1-16`
  (`common.bash:469`).
- `claude-yolo:1436` calls `validate_container_version "$IMAGE_NAME" … "$REQUIRED_CONTAINER_VERSION"`
  on the **base** image, confirmed working end to end.
- `claude-yolo:1450-1455` — the **project**-image section sets `PROJECT_DOCKERFILE_HASH_FILE` and
  `PROJECT_BASE_VERSION_FILE` under `$HOME/.cache`, and the project-image build path
  (`claude-yolo:1547-1554`, `run_project_build`) passes **no `--label` flag at all** — ccy's own
  launcher has never written a label for the project-image hash. `lts.ccy.dockerfile-sha256` is a
  convention the *consumers* invented for a fact ccy itself only tracks via `$HOME/.cache`.

So the two labels genuinely name different facts on different images, and ccy has never had a
production label-based convention for the project-image fact at all — only the consumers do. The
spec's correction is accurate, not a rationalisation to dodge Round 4: it narrows the scope of what
needs resolving (facts 2 and 3, not fact 1) without denying that a real gap exists — and, per
Finding 1 above, it does not use this accurate reframing to quietly drop D10's actual ask.

---

## 3. Verified — Fact 3 (the second cache file) is real, reachable, and previously unnamed

```
claude-yolo:1455   PROJECT_BASE_VERSION_FILE="$HOME/.cache/claude-yolo-${PROJECT_NAME}-base-version"
claude-yolo:1478   current_base_version=$(container_cmd image inspect "claude-yolo:latest" --format '{{index .Config.Labels "claude-yolo-version"}}' …)
claude-yolo:1483-1485   previous_base_version read from PROJECT_BASE_VERSION_FILE
claude-yolo:1499   elif [ "$current_base_version" != "$previous_base_version" ]; then … should_build=true
claude-yolo:1520-1521, :1581-1582   both write sites: echo "$current_project_hash" > …; echo "$current_base_version" > …
```

This is genuinely reachable, ordinary production code — it fires on every `ccy` invocation for any
project with `.claude/ccy/Dockerfile` (`claude-yolo:1457`), unlike D10(b)'s dead-code citation into
`actions-hub/ci.yml`. The spec's claim that this fact "no round and neither consumer had named" is
plausible from what is visible in this repo and in `actions-hub`:

```
$ grep -rn "base-version\|base_version\|claude-yolo-version" /workspace/untracked/repos/actions-hub
(no matches)
```

`actions-hub` confirmed clean. **`lts-infra` could not be checked in this environment (not present)
— unverified**, not confirmed, for that repo's own comparison logic. Earlier rounds (Round 2–4
reports) quote exact line numbers from `lts-infra` files, so that repo was accessible in a prior
session; it is not accessible in this one. Flagging as unverified rather than asserting the spec
right or wrong on that specific point.

---

## 4. Verified — the md5-16 vs sha256 split (§3) is correctly reasoned

- `common.bash:469`, `common.bash:552`, `claude-yolo:1471`, `claude-yolo:1539` — all four existing
  hash computations use `md5sum … | cut -d' ' -f1 | cut -c1-16`. Confirmed exactly as cited.
- `common.bash:465-469` is the only comparison gated on the *fixed* `REQUIRED_CONTAINER_VERSION`,
  so it is genuinely out of the new spec's scope — changing that algorithm forces every existing
  desktop image through a rebuild, exactly as claimed (it fails `hash_match` while `version_match`
  can still hold, landing in the `elif $version_match && ! $hash_match` branch,
  `common.bash:500-517`, which is real code, not a hypothetical).
- One nuance the spec doesn't mention, immaterial to its conclusion: that branch prints a
  "DEVELOPER ERROR: Dockerfile modified without version bump" warning (`common.bash:503-505`) —
  slightly more alarming than "invalidate" suggests, but only if `REQUIRED_CONTAINER_VERSION` is
  *not* bumped alongside the algorithm change. Any real rollout would bump it (mandatory per
  `CLAUDE/ContainerRules.md`), which routes through the friendlier "Normal upgrade" branch instead.
  Not a defect — worth one sentence so a later reader doesn't treat "invalidate" as more dramatic
  than the actual failure mode.

---

## 5. MINOR — §4's non-empty assertion is a stated requirement, not a specified mechanism

§4 states: *"The comparison must therefore assert that the wanted value is non-empty before
comparing, and treat an empty have-value as a rebuild trigger."* This is the right requirement, and
§6 correctly flags it as the single most consequential unmeasured claim in the document. But unlike
the Writer subsection (concrete `podman build --label …` shell) and unlike this plan's own bar
elsewhere (Task 2.1 assigns one of three named outcomes to every site), the Comparison subsection
gives no analogous concrete form — no sentinel scheme, no pseudocode showing where the assertion
sits relative to the two lookups. `common.bash:463-466`'s existing pattern (`2>/dev/null || echo "0"/"unknown"`) is a real precedent in this codebase for handling a missing-image case, but it
does **not** cover the missing-*label*-on-a-present-image case this spec's own hazard describes —
so it cannot be pointed to as "already solved this shape." A later implementer has a correct
requirement and no worked example to implement it against. Fill the gap with the same rigor as the
Writer example, or explicitly hand it to the hardware-proof checklist as the concrete artifact
(§6 already gestures at this but doesn't commit to producing it).

---

## 6. Citation discipline — sample checked, all reachable and accurate

| Citation                              | Checked against                                 | Result                                                                                                                                                                                                      |
| ------------------------------------- | ----------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Dockerfile:36`, `:248`               | source, read directly                           | exact match                                                                                                                                                                                                 |
| `common.bash:456-472`, `:537-567`     | source, read directly                           | exact match, reachable (called at `claude-yolo:1436`)                                                                                                                                                       |
| `claude-yolo:1452-1530`, `:1570-1590` | source, read directly                           | exact match, reachable (fires whenever `.claude/ccy/Dockerfile` exists)                                                                                                                                     |
| `entrypoint.sh:257-263` (D13's fix)   | source, read directly                           | confirmed — block genuinely begins at the `:255` comment as D13 says                                                                                                                                        |
| `actions-hub/ci.yml:97,:99` (D10)     | source, read directly                           | confirmed unreachable, per D10 — the `:82-95` comment says so outright                                                                                                                                      |
| D14's "origin" claim re: Task 3.3     | `git grep` for the retracted "two users" clause | confirmed both echoed files (`phase3-image-layering.md:194`, `round2-restatement.md:268`) carry `[corrected per D5]`, and Task 3.3 in PLAN.md carries the corrected text with `[corrected per D5; see D14]` |

No citation in this sample pointed at unreached or misdescribed code. D8/D10's citation-defect
pattern does not repeat here.

---

## 7. Sweep for a sixth propagation-gap instance and other stray CI-framing — none found

Grepped the full plan tree for `build-and-exit`, `mandatory for CI`, `CI enabler`,
`actions-hub deletes`, `motivating example`. Every hit outside the deliberately-preserved Round-1
body text (`## Overview`, left untouched by convention) already carries a correction note or sits
inside a correction block itself. `task74-capabilities.md:90` ("C8's mandatory for CI stands") is
body text preceding the file's own `⚠ CORRECTIONS APPLIED AFTER THIS DOCUMENT WAS WRITTEN` section
(line 197), which retracts it per D9's method — consistent with the documented discipline, not a
miss. No sixth instance found.

---

## 8. What this round did not find

- No defect in the key names, the sha256 choice, the writer shell, or the reader/comparison logic
  themselves — all technically sound and grounded in source.
- No new dead-code citation.
- No fourth surviving instance of the retracted "two users" clause.
- No evidence the spec's §1 correction is a rationalisation — verified independently as accurate.

---

## Summary

| #   | Severity | Finding                                                                                                                                                                                                                                                                                                                                                  |
| --- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | BLOCKER  | D14's "closes D10's half (a)" overstates what `label-convention-spec.md` itself says is unresolved — the migration's step 4 is unscheduled, unactionable from this plan, and the document's own rejection condition is left unresolved. Eighth instance of the "true statement about a check presented as a stronger statement about the world" pattern. |
| 2   | MINOR    | §4's non-empty-assertion requirement is correctly stated but not given a concrete mechanism, unlike the Writer subsection and unlike this plan's own Task 2.1 precedent.                                                                                                                                                                                 |
| —   | note     | Fact-3 claim ("no LABEL convention anywhere") is unverified for `lts-infra` in this session (repo not checked out here); confirmed for `fedora-desktop` and `actions-hub`.                                                                                                                                                                               |

**MATERIAL FINDINGS: yes**
