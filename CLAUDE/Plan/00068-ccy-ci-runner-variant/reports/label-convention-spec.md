# Plan 00068 — the image `LABEL` convention, specified (closes D10 half (a))

D10 found that this plan has been specifying the *idea* of a `LABEL` convention rather than a
convention. Task 3.3's Option C names no key. Two conventions already exist in production. This
document names the keys, the algorithm, the writer, the reader, the comparison, and the migration
that avoids creating a third.

Every fact below was read from source for this document.

---

## 1. What is labelled today — and Round 4's framing needs one correction

Round 4 described *"two already-divergent `LABEL` conventions"* risking a third. That is
directionally right and imprecise in a way that matters: **the two labels do not identify the same
fact, so they are not rivals.** There are three facts in play, two conventions, and one gap.

| #   | Fact                                                       | Lives on             | Convention today                                                                                                                 |
| --- | ---------------------------------------------------------- | -------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| 1   | which ccy `Dockerfile` built the **base** image            | `claude-yolo:latest` | `claude-yolo-version` + `claude-yolo-dockerfile-hash` (`Dockerfile:36`, `:248`) — **works**                                      |
| 2   | which project `Dockerfile` built the **project** image     | `claude-yolo:<proj>` | **`$HOME/.cache/…-dockerfile-hash`** (`claude-yolo:1454`) — *not a label*; lts-infra/actions-hub use `lts.ccy.dockerfile-sha256` |
| 3   | which **base version** the project image was built against | `claude-yolo:<proj>` | **`$HOME/.cache/…-base-version`** (`claude-yolo:1455`) — *not a label*, and **no convention exists anywhere**                    |

Fact 1 is already solved and is not in scope. **Facts 2 and 3 are the defect**, and both are read
at `claude-yolo:1473-1485` and written at **two** duplicated sites (`:1520-1521` and `:1581-1582`).

**Fact 3 is the one nobody has named — including Round 4, lts-infra and the consumer.** Every
discussion of "put the staleness identity in a `LABEL`" has meant fact 2. But the rebuild decision
reads *both* cache files (`:1495` project hash, `:1499` base version), so moving only fact 2 into a
label leaves half the decision in `$HOME` and the defect substantially intact. A spec that solves
fact 2 alone would have been accepted by every round so far and would not have worked.

---

## 2. The namespace decision

`lts.ccy.dockerfile-sha256` cannot simply be adopted upstream. `ccy` is **upstream** of lts-infra;
an upstream project taking a downstream consumer's vendor-prefixed namespace inverts the
dependency, and would leave ccy's own labels split across two naming styles
(`claude-yolo-version` unprefixed, `lts.ccy.*` prefixed).

**Decision: ccy defines the canonical keys in its existing unprefixed style, and the consumers
migrate onto them.**

| Fact | Canonical key                           | Value                                                 |
| ---- | --------------------------------------- | ----------------------------------------------------- |
| 2    | `claude-yolo-project-dockerfile-sha256` | full lowercase hex sha256 of `.claude/ccy/Dockerfile` |
| 3    | `claude-yolo-project-base-version`      | the `claude-yolo-version` of the base at build time   |

This is a third *name*, which D10 warned against — so the warning is answered explicitly rather
than ignored: **it is only acceptable because it comes with a migration that removes the second
one** (§5). A new key that leaves the old key in place is what D10 forbids, and is not what this
specifies.

## 3. The algorithm decision — sha256, not ccy's md5-16

ccy currently hashes with `md5sum … | cut -c1-16` (`common.bash:469`, `:552`; `claude-yolo:1471`).
The new labels use **full sha256**, matching what lts-infra and actions-hub already compute.

Reasons, in order of weight:

1. **The consumers already emit and read sha256.** Choosing md5-16 would force a migration on the
   two repos that got this right, to adopt the weaker option.
2. **Truncation to 16 hex chars is 64 bits.** For a staleness check this is not a security boundary
   and md5 is not being used as one — but there is no reason to specify a *new* field at 64 bits
   when the alternative costs nothing.
3. **Fact 1 keeps md5-16 unchanged.** `claude-yolo-dockerfile-hash` is compared by
   `validate_container_version` (`common.bash:465-469`) and is not in scope; changing it would
   invalidate every existing desktop image and force a global rebuild. The two algorithms coexist
   because they answer two different questions on two different images — deliberately, and noted
   here so a later reader does not "tidy" it into a single algorithm and trigger that rebuild.

## 4. Writer, reader, comparison

**Writer.** Whoever builds the project image, at build time, via `--label`:

```
podman build \
  --label claude-yolo-project-dockerfile-sha256="$(sha256sum .claude/ccy/Dockerfile | awk '{print $1}')" \
  --label claude-yolo-project-base-version="$(podman image inspect claude-yolo:latest \
      --format '{{index .Config.Labels "claude-yolo-version"}}')" \
  -t claude-yolo:<project> -f .claude/ccy/Dockerfile .
```

Both writers exist today and both must set both labels: ccy's launcher (`claude-yolo:1520-1521`,
`:1581-1582` — the two duplicated sites, which should collapse to one) and lts-infra's
`runner-ccy-project-image.yml`.

**Reader.** Anyone deciding whether the project image is stale — ccy's launcher at `:1487-1529`,
lts-infra's provisioning task, and a CI job with only a checkout and a container engine.

**Comparison.** The image is stale if **either** differs:

| Check | Wanted (from checkout / base)                       | Have (from the project image's labels)  |
| ----- | --------------------------------------------------- | --------------------------------------- |
| 2     | `sha256sum .claude/ccy/Dockerfile`                  | `claude-yolo-project-dockerfile-sha256` |
| 3     | `claude-yolo-version` label on `claude-yolo:latest` | `claude-yolo-project-base-version`      |

**A missing label must be treated as STALE, never as a pass.** An image built before this
convention has neither key, and `podman image inspect --format '{{index .Config.Labels "…"}}'`
returns an empty string for an absent label rather than failing. An implementation that compares
`""` to `""` — which happens when the *wanted* side also fails to compute — reports "fresh" for two
unknowns. That is the plan's own recurring failure mode expressed as code: a check that fires and
does not discriminate. **The comparison must therefore assert that the wanted value is non-empty
before comparing**, and treat an empty have-value as a rebuild trigger.

## 5. Migration — how this ends with one convention rather than three

1. ccy's launcher writes both new labels at build time, and reads them in preference to the cache
   files. The `$HOME/.cache` files are no longer written.
2. ccy reads a project image with **neither** new label as stale (§4), so the first launch after
   upgrade rebuilds once. That is the whole upgrade cost, and it is self-healing.
3. lts-infra's `runner-ccy-project-image.yml` and actions-hub's `ci.yml` switch from
   `lts.ccy.dockerfile-sha256` to `claude-yolo-project-dockerfile-sha256`, and **add** the
   base-version check they do not currently have.
4. `lts.ccy.dockerfile-sha256` is then emitted by nobody and read by nobody, and is deleted.

Step 4 is what makes this a replacement rather than an addition. **If step 4 is not scheduled, this
specification should be rejected** — a third live convention is strictly worse than the two that
exist now.

## 6. What this does NOT settle

- **Nothing here has been run.** Every claim is from source reading. Per D10, the previously-cited
  "proof" that a CI job does this comparison (`actions-hub/ci.yml:97`,`:99`) is a branch that has
  **never executed** — that repo ships no `.claude/ccy/Dockerfile` and returns at the baseline path
  (`:91-95`). The `LABEL` approach is a design two repos wrote, not a design either has run.
- **The empty-label behaviour in §4 is asserted from the documented behaviour of
  `--format '{{index .Config.Labels …}}'`, not measured.** It is the single most consequential
  claim here, because getting it wrong produces a check that always passes. It belongs in the
  hardware-proof checklist as a group-A item: build an image without the labels, run the
  comparison, and confirm it reports STALE.
- **Whether any existing box has project images without these labels** is a matter of rebuild
  history, exactly as with `claude-yolo:base` (Phase 3 §0.2), and is answerable only on a host.
