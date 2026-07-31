# Plan 00068 — resolving the finding numbers (`R`, `D`, `Decision`)

This plan carries **three independent numbering schemes**. One was deleted from `PLAN.md` while
five reports still cited it, one is defined only in the append-only `JOURNAL/`, and two of the
three collide on the same labels. A reader who meets `R9`, `D6` or `Decision 6` in a report
currently cannot resolve it from the tracked tree without git archaeology.

That is a direct breach of this plan's Success Criterion 1 — *"every claim is cited to a
`file:line` or explicitly marked unverified"*. A citation to a number that no longer exists
satisfies neither branch: it is not resolvable, and it does not announce that it isn't.

`PLAN.md:181-182` already states the governing rule, for the Phase 3 tasks:

> Kept as cancelled rather than deleted because `reports/fable-review-*.md` cite them by number.

**The `R1`–`R11` block was deleted in violation of that same rule.** This document repairs that
and states the collisions plainly. It changes no decision.

---

## 1. The three schemes

| Scheme       | Range   | Defined in                                            | Resolvable today?                           |
| ------------ | ------- | ----------------------------------------------------- | ------------------------------------------- |
| `R1`–`R11`   | Round 2 | `PLAN.md` — **DELETED** at `9a53608`                  | **No** — restored in §3 below               |
| `D1`–`D33`   | ongoing | `JOURNAL/` day-files (e.g. `D6` at `26-07-30.md:728`) | Only by searching three journal files       |
| `Decision N` | 1–6     | `PLAN.md` **and** `reports/round2-restatement.md`     | **Ambiguous** — two documents, two meanings |

## 2. The collisions — read this before citing any `Decision N`

### 2.1 `Decision 5` and `Decision 6` mean different things in different documents

| N   | `PLAN.md`                                           | `reports/round2-restatement.md`                        | Same?     |
| --- | --------------------------------------------------- | ------------------------------------------------------ | --------- |
| 4   | no permission surface (`:126`)                      | `ccy` does NOT grow a permission surface (`:110`)      | ✅ yes    |
| 5   | egress restriction is independent of CI (`:133`)    | the trusted-only scope is asserted (`:135`)            | ❌ **no** |
| 6   | the fail-fast contract reuses ccy's shapes (`:137`) | a second CI entrypoint beside `entrypoint.sh` (`:150`) | ❌ **no** |

**The live consequence.** `round2-restatement.md:358` closes with *"Decisions 4, 5 and 6 stand."*
Read against `PLAN.md` — which is where a reader looks up a decision — that sentence endorses two
propositions the report never made, and its own `Decision 6` (a second CI entrypoint) was
**retracted** by `PLAN.md` Task 3.6. The sentence is true of the document it sits in and false of
the plan. This is the signature meta-bug of this plan (`.claude/rules/bash-standards.md` §9): a
true statement about a local scope presented as a statement about the world.

**`round2-restatement.md` is preserved as written** — later rounds cite its line numbers, and its
own footer says so. The fix is this key, not an edit to it.

### 2.2 `D6` is not `Decision 6`

`D6` is a **review finding** (`JOURNAL/00068-Journal-26-07-30.md:728`) which retracted `R10` item 1.
It is cited as `D6` in 12 places across `reports/`. It abbreviates exactly like `Decision 6`, which
names two other things (§2.1). **When citing, write `D6` for a review finding and never abbreviate
`Decision 6`.**

---

## 3. `R1`–`R11` — restored verbatim

Recovered from `git show 9a53608^:CLAUDE/Plan/00068-ccy-ci-runner-variant/PLAN.md` lines 164–226,
unaltered. The status column is this document's contribution; the quoted text is not edited.

| ID    | Subject                                                  | Status today                                                           | Now lives in                     |
| ----- | -------------------------------------------------------- | ---------------------------------------------------------------------- | -------------------------------- |
| `R1`  | The steer settles Task 3.3 — the project Dockerfile seam | **Stands** — `PLAN.md` Task 3.3, "decided: no overlay. Still correct." | `round2-restatement.md` §0, §3   |
| `R2`  | `ccy` is three layers                                    | **Stands**                                                             | `round2-restatement.md` §1       |
| `R3`  | The entrypoint is inside the image                       | **Stands**                                                             | `round2-restatement.md` §1.1     |
| `R4`  | E10 — four trust assertions, not one                     | **Stands**, strengthened by `D20`                                      | `round2-restatement.md` §2       |
| `R5`  | Decision 4 — no permission surface                       | **Stands, but OPEN** — `PLAN.md` open decision 1 may reopen it         | `round2-restatement.md` §2 D4    |
| `R6`  | Trusted-only scope is asserted, not inferred             | **Stands** — inference would be the banned `_armed` shape              | `round2-restatement.md` §2 D5    |
| `R7`  | A second, small CI entrypoint — ADOPT                    | **RETRACTED** — `PLAN.md` Task 3.6; `ccy` uses its own entrypoint      | `round2-restatement.md` §2 D6    |
| `R8`  | "Ad-hoc or full-blown" resolves per capability           | **Stands**                                                             | `round2-restatement.md` §3, §3.1 |
| `R9`  | **The split is by TIME, not by feature**                 | **Stands** — and see the note below                                    | `round2-restatement.md` §4       |
| `R10` | Three gaps block lts-infra deleting its duplicate        | **Item 1 RETRACTED by `D6`**; items 2 and 3 stand                      | `round2-restatement.md` §4.1     |
| `R11` | `--no-network` does not isolate                          | **Stands** — carried by Task 7.4 (C8)                                  | `round2-restatement.md` §6       |

> **Note on `R9`.** The owner's steer of 2026-07-31 — *"the ccy image is built from the ccy
> dockerfile / if that is out of date then ccy should rebuild as it does normally"* — **agrees**
> with `R9`. The by-time split is not a parallel system built beside `ccy`: `round2-restatement.md`
> §4 describes the provision-time build as *"exactly the owner's 'normal way', executed by the
> machine that will run the jobs."* Same Dockerfile, same staleness logic, same rebuild — run while
> the egress window is open rather than at job time, when squid refuses it.

### The recovered text

> **R1 — the steer settles Task 3.3.** The per-project `.claude/ccy/Dockerfile` seam is the
> mechanism and is not to be replaced. The "mandatory platform overlay for the general case"
> branch is dead; it survives only for untrusted checkouts, which is a different plan.
>
> **R2 — `ccy` is THREE layers, and the steer is about exactly one.** Image / entrypoint /
> launcher. The steer is an **image** mechanism, so it delivers **tooling** — which is what it
> was built for and is genuinely excellent at. Safety lives entirely in the other two layers.
>
> **R3 — the entrypoint is INSIDE the image, so you cannot take one without the other.**
> `Dockerfile:215` sets `ENTRYPOINT`; none of the three project-facing templates declares one, so
> a project image built the normal way runs the **desktop** entrypoint — `GH_TOKEN`-or-die, `gh auth login`, the checkout symlink, the trust flags. Confirmed three independent ways, including
> two live productions failures in other repos (report §1.1). This is the structural reason half
> the steer cannot be satisfied by the mechanism the steer endorses.
>
> **R4 — E10, and it is stronger than C1 stated.** `ccy` asserts "this workspace is trusted" in
> **four** places, not one: `claude-yolo:2792` (`--dangerously-skip-permissions`, unconditional),
> `entrypoint.sh:245` (`bypassPermissionsModeAccepted`), `entrypoint.sh:257-263`
> (`hasTrustDialogAccepted`, unconditional), and `entrypoint.sh:269-274` — which **sources
> `/workspace/.claude/ccy/ccy.env` as shell** and then `exec`s `CCY_CLAUDE_WRAPPER` from it
> (`:280-282`). The last is new to this plan: **the checked-out tree controls the command that
> runs.** So the posture is not a loose default a flag could tighten; it is a coherent, deliberate
> trust model whose premise is that the operator owns the workspace.
>
> **R5 — Decision 4: `ccy` does NOT grow a permission surface.** C1 required this be answered.
> Two opposite postures in one artifact is the defect C2 already found; a launcher flag would not
> reach the entrypoint half anyway; and the estate already has stronger containment one trust
> boundary up (lts-infra `RUNNER-VM-DESIGN.md` §5.4/§6.4). **The price, stated: `ccy` in CI is for
> TRUSTED automation only, and is not a replacement for the consumer's sandbox.** This plan stops
> using that repo's deletion as its motivating example.
>
> **R6 — Decision 5: the trusted-only scope is asserted, not documented.** A required
> caller-supplied declaration with no default and no inference from `GITHUB_EVENT_NAME`; absence
> is a hard stop. Inference would be the banned `_armed` shape.
>
> **R7 — Decision 6: a second, small CI entrypoint beside `entrypoint.sh` — ADOPT.** The third
> option Decision 1 never considered. It fixes the problem at the layer it lives in (R3), and it
> is the correction for a defect now made three times by three codebases hand-rolling
> `--entrypoint /bin/bash`. Scoped deliberately: *prepare nothing, assert nothing about trust,
> exec what you were told*. It is **not** the consumer's fail-closed sandbox — Decision 4 declined
> that.
>
> **R8 — "ad-hoc or full-blown" resolves per capability, and two of four fit neither.** Tooling:
> done already. MCP: both routes viable, both need net-new wiring (E4 re-confirmed — zero matches).
> Permissions: neither route. Egress: ad-hoc only, being a runtime property.
>
> **R9 — the split is by TIME, not by feature.** Image build belongs at **provision** time
> (Ansible, wide egress armed for that window only); job time runs an already-built image. This
> closes **C6**: the runner is JIT-ephemeral *registration* on a *persistent* VM, so a
> provision-time build is coherent, and **registry support is declared out of scope** — confirmed
> costless, since searching the launcher and all 7 libs for engine `push`/`pull` returns zero.
>
> **R10 — three concrete gaps block lts-infra deleting its duplicate** (its Plan 00026 Task 3.3):
> a non-interactive build-and-exit mode; a build identity readable **from the image** (a `LABEL`,
> not `$HOME/.cache/claude-yolo-*-dockerfile-hash` at `claude-yolo:1454`, which is host-user-local
> and cannot answer CI's question); and a supported way to run a command without the desktop
> entrypoint. The second is a real defect independent of CI.
>
> **R11 — `--no-network` does not isolate, and that is worse than the `--network` trap.** C8's
> "mandatory" conclusion holds (preflight is fatal: `claude-yolo:2529`, `exit 1` at `:2597`), but
> at `:2514-2517` the flag merely leaves `NETWORK_FLAG` empty, so podman's default network still
> applies. There is no `--network none` in the codebase. **Task 5.1's naming problem is therefore
> three-way**, and `--no-network` is the dangerous one because its name is a safety promise it
> does not keep.

---

## 4. Rule going forward

1. **Never delete a numbered finding that another document cites.** Mark it retracted in place —
   the rule `PLAN.md:181-182` already applies to Phase 3's tasks.
2. **`D` for review findings, `R` for Round-2 findings, `Decision N` written out in full.** Do not
   abbreviate `Decision N` to `DN`.
3. **A `Decision N` citation names its document.** `PLAN.md` Decision 6 and
   `round2-restatement.md` Decision 6 are different propositions and both are live text.
