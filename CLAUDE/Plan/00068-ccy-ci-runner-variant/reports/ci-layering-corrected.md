# Plan 00068 — CI layers ON TOP of the project image, not underneath it (D31)

**Status: proposed.** It inverts Tasks 3.1 and 3.2 (both ✅) and reopens Task 3.3's overlay
rejection. Recorded here for a decision, not applied.

## The constraint that invalidates the current design

Stated by the owner, verbatim in substance:

- desktop ccy is **fundamental, highly tuned, works brilliantly** — it is what runs the interactive
  sessions, including the one this plan is being written in;
- **"YOU ARE NOT ALLOWED to degrade desktop ccy in ANY WAY — no context bloat, no MCP added,
  nothing"**;
- CI must have **the same tooling stack**, run deterministic QA tools **and** claude-powered tasks
  (issue triage, PR review);
- CI must be **more restricted**, not less;
- a project's `.claude/ccy/Dockerfile` is **not guaranteed to exist**;
- a separate `Dockerfile.ci` and some tooling duplication would be acceptable if it made things
  easier.

**The cost of MCP on desktop is not image size — it is context.** An MCP server registers its tools
into every session's context window, permanently, whether or not they are used. That is the "context
bloat" the constraint names, and it is why "one binary is only a few MB" was never the right
measurement.

## Two designs are eliminated by that constraint, including the one in the plan

**Eliminated: bake MCP into `claude-yolo:latest`.** Proposed in conversation before the constraint
was stated. It degrades every desktop session directly. Withdrawn.

**Eliminated: the design currently specified in Task 3.2.** This one matters more, because it is
already recorded as ✅. Task 3.2 says a project opts into CI by writing `FROM claude-yolo:ci` in its
`.claude/ccy/Dockerfile`. That file is the project's **only** Dockerfile — `claude-yolo:1452`,
consumed at `:1457` to build `claude-yolo:<project>`, which `:1633` then selects as the image the
**desktop** session runs. So:

> Under the specified design, any project that opts into CI gives its desktop developers the GitHub
> MCP server, in every interactive session, permanently.

That is not a wart to be fixed by fact 4 or by the §4.2 key discipline. It is disqualifying, and it
disqualifies the direction, not the details.

**Why it was not caught.** The `:ci` tag was reasoned about as *"a base for CI images"*. It is
reachable only through the project's single Dockerfile, so selecting it is not a CI-time choice at
all — it is a **project-wide, permanent** one that desktop inherits. Seven rounds discussed which
base CI builds from and never asked what the base flip does to the desktop session on the other side
of the same file. A true statement about the CI path, taken as a statement about the project — the
plan's recurring defect, in the one place it costs the most.

## The corrected shape

The CI payload belongs in a layer **above** the project image. Desktop never builds it, never pulls
it, never sees it.

```
claude-yolo:latest                    ccy base — UNCHANGED, byte for byte
      │
      │  only if .claude/ccy/Dockerfile exists  (claude-yolo:1457)
      ▼
claude-yolo:<project>                 desktop project image — UNCHANGED, byte for byte
      │                               ← this is what an interactive `ccy` session runs (:1633)
      │
      │  ccy-owned CI layer:  files/var/local/claude-yolo/Dockerfile.ci
      │  FROM = whatever :1457/:1633 already resolved (project image, else claude-yolo:latest)
      ▼
claude-yolo-ci:<project>              CI leaf — built and run ONLY by CI
        + MCP server binary, sha256-pinned
        + /usr/local/bin/ccy-ci-entrypoint.sh
        + MCP registration, written container-local (never into /workspace)
        + ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/ccy-ci-entrypoint.sh"]
```

### Why each constraint is satisfied structurally, not by discipline

| Constraint                           | How                                                                                                                                                   |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| desktop not degraded **in any way**  | Neither `claude-yolo:latest` nor `claude-yolo:<project>` gains a single instruction. The CI payload exists only in a tag desktop never builds         |
| CI has the **same tooling stack**    | The CI leaf *is* the project image, plus a layer. Not a copy of it — the same layers. Zero duplication, and it cannot drift                           |
| project Dockerfile **may be absent** | `FROM` is the image `:1457`/`:1633` already resolve. Absent ⇒ `claude-yolo:latest`. No new resolution rule                                            |
| CI **more restricted**               | The CI entrypoint drops auth, SSH, `ccy.env` sourcing and the `/workspace` symlink (D29). Lockdown lives in the leaf, where it cannot leak to desktop |
| tooling duplication acceptable       | **Not needed.** The offer is declined — deriving from the project image is strictly better than duplicating it                                        |

## What this retires

**D30 evaporates.** The wrong-base staleness bug exists only because a project image could be `FROM claude-yolo:ci`. Under D31 no project image is ever `FROM` anything but `claude-yolo:latest`, so the
hard-coded literal at `claude-yolo:1477` becomes **correct** rather than a latent defect, and
`claude-yolo-project-base-image` (fact 4) is unnecessary for the project image.

**G1 evaporates.** `--entrypoint` dropping `tini` was a consequence of shipping the CI entrypoint in
an image whose default `ENTRYPOINT` had to stay desktop. The CI leaf is a **leaf** — nothing pulls it
expecting ccy desktop semantics — so it can simply set:

```
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/ccy-ci-entrypoint.sh"]
```

`tini` is preserved by construction, and callers stop hand-rolling `--entrypoint` because there is
nothing left to hand-roll. This is Decision 6's stated goal — *"callers stop hand-rolling
`--entrypoint`"* — reached properly. Task 3.1's objection to overriding `ENTRYPOINT` was
*"it would silently change behaviour for anything that pulls this tag expecting ccy semantics"*; that
objection is sound for a shared base and vacuous for a per-project CI leaf.

**Task 3.2's tag-collision residual evaporates.** There is no shared `claude-yolo:ci` tag for a
project directory named `ci` to collide with. The leaf lives in a separate repository namespace,
`claude-yolo-ci:<project>`, so it cannot collide with `claude-yolo:<anything>` either — which a
`claude-yolo:<project>-ci` suffix scheme would (project `foo-ci` desktop vs project `foo` CI).

**§4.2's inherited-label trap is killed by construction**, and generalised into a rule worth keeping:

> **Every layer writes its own uniquely-named labels, and no layer ever reads a key it might have
> inherited.** Inheritance is invisible to `image inspect` — an inherited value and a self-set value
> are indistinguishable — so any comparison that reads a possibly-inherited key can silently compare
> a parent's answer.

The CI leaf therefore records `claude-yolo-ci-layer-sha256` and `claude-yolo-ci-parent-digest`
(the resolved parent image's digest) under names no ancestor uses. **F4 is still worth measuring** —
the rule above is the mitigation for inheritance, which remains unproven.

## A cross-contamination path this closes, which was previously only tidiness

D29 specified that the CI entrypoint must **not** reproduce `entrypoint.sh:195`'s
`ln -sf /workspace/.claude/ccy /root/.claude`, on the grounds that CI would write session state into
the checkout. Under the desktop-purity constraint that becomes load-bearing rather than tidy:

MCP registration is written to `/root/.claude/`. **With** the symlink, a CI job writes its MCP
configuration into `/workspace/.claude/ccy/` — the project's own tree. Anything that persists there
(committed, cached between runs, or merely left dirty) is read by the **desktop** session next time.
The symlink is a direct MCP-into-desktop path. Dropping it is now a constraint, not a preference.

## Open, and not mine to settle

1. **Task 3.3 rejected a ccy-owned overlay on the project image. This proposal is one.** Stated
   plainly rather than slipped past. The rejection was reasoned *given Decision 4*, and was about a
   **security** overlay constraining an untrusted project Dockerfile; this is a **payload-separation**
   overlay whose purpose is keeping CI-only material out of desktop. Same mechanism, different job.
   Task 3.3's own caveat — *"reverse Decision 4 and the overlay returns immediately"* — plus the new
   "CI should be more restricted" steer, means the security motivation is at least partly reopened
   too.
2. **Decision 4 (no permission surface) may need revisiting for CI specifically.** "More locked down"
   and `--dangerously-skip-permissions` point in opposite directions. Out of scope to settle here,
   but it should not be assumed settled just because Decision 4 is recorded as ✅.
3. **A project-level `.claude/ccy/Dockerfile.ci` extension point is deliberately NOT specified.** The
   owner offered it as acceptable; the layered design does not need it, and YAGNI says do not build
   the extension until a project wants CI-only extra tooling. Noted as available, not designed.

## Status

Design only, and **unbuilt** — as with everything else in this plan. It changes what the
implementation plan should implement; it does not change anything on disk.
