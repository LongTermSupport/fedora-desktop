# Plan 00068 — the design in one page (Task 6.5)

Held until the audit loop went quiet, because a finding could have invalidated it. Round 7 returned
no material findings after Rounds 1–6 each found blockers. This is what survived.

---

## What `ccy` is

Three layers, and the distinction decides everything else:

| Layer          | What it is                                             | Reachable from a project `Dockerfile`? |
| -------------- | ------------------------------------------------------ | -------------------------------------- |
| **Image**      | toolchain — packages, LSP servers, `gh`, `yq`          | **Yes — the seam, by design**          |
| **Entrypoint** | in-container session prep, `gh` auth, trust assertions | **No — inherited** (`Dockerfile:215`)  |
| **Launcher**   | credential resolution, prompts, container argv, egress | **No — never enters the image**        |

The owner's steer — *"each project gets its own ccy runner the normal way"* — is an **image**
mechanism. It therefore delivers tooling excellently and **cannot deliver safety**, because safety
lives in the other two layers. Critically, the entrypoint is *inside* the image, so a project
taking the base image the normal way also takes `GH_TOKEN`-or-die, `gh auth login`, the checkout
symlink, and four separate "this workspace is trusted" assertions — whether it wants them or not.

## What `ccy` owes CI: exactly two things

Three consecutive rounds shrank this. Round 1 took the original thesis, Round 2 the
launcher-mediated CI path, Round 3 provisioning as well. **The `claude-yolo` launcher is never on
the CI path at all** — provisioning calls `podman build` directly, and job time is the caller's own
`podman run`.

1. **An image `LABEL` identity convention**, so staleness is answerable from the image rather than
   from host-user-local `$HOME/.cache` state. Specified in
   [label-convention-spec.md](label-convention-spec.md) — and the specification found that the
   rebuild decision reads **two** cache files, not one (the project Dockerfile hash *and* the base
   version it was built against), the second of which had no convention in any repo.
2. **A CI entrypoint** shipped in the image and selected explicitly, so callers stop hand-rolling
   `--entrypoint` — a defect three codebases have hand-rolled and two got wrong in production.
   Specified in [ci-entrypoint-spec.md](ci-entrypoint-spec.md) — which, until D29, this deliverable
   did not have: seven rounds specified how it is shipped and selected and never what it contains.

> # ⚠ SUPERSEDED BY D33 — read [project-drives-the-image.md](project-drives-the-image.md) first
>
> **"The two things" below are wrong, and the ownership model underneath them was wrong.** The owner
> has corrected it: **Ansible provides the VM; the PROJECT drives the image; we provide a safe
> mechanism for running project podman containers as CI workloads.**
>
> - **Deliverable 1 (the `LABEL` convention) is retired** — `podman build` already answers staleness.
>   It only looked necessary because Task 3.4 assumed the image was built out of band by Ansible.
> - **Deliverable 2 (the CI entrypoint) survives**, and so does everything in Phases 4/5 and Task
>   7.4 — the MCP interface, `--egress` and its proof, concurrency safety, and the decision that CI
>   must not write `.claude/ccy/` into the checkout. **That is the safe mechanism, and it is where
>   this plan's real value accumulated.**
> - **Phase 3 needs re-doing rather than patching.** "How would a project add a new tool for CI?"
>   has no answer under an Ansible-built image, and that is a direct contradiction of the founding
>   steer this plan exists to serve. **Done: [safe-run-mechanism.md](safe-run-mechanism.md)** — a
>   small Ansible-deployed runner that hardens `podman run` for a project-built image, with the CI
>   entrypoint **bind-mounted from the VM** so no image anywhere changes.
> - The MCP-on-desktop problem **disappears**: a project that wants MCP puts it in its own CI
>   Dockerfile, and desktop never sees it. D31's overlay is withdrawn; **Task 3.3's "no overlay"
>   decision stands.**

> **The governing constraint, added after the above and overriding it where they conflict (D31):**
> **desktop ccy must not be degraded in any way — no context bloat, no MCP, nothing.** MCP's cost is
> not image size, it is tool surface in every interactive session's context. This disqualifies the
> layering the plan had specified, in which a project opts into CI by changing the `FROM` line of the
> same Dockerfile that builds its desktop image. Corrected shape — CI layers **above** the project
> image as a per-project leaf, `claude-yolo-ci:<project>`, which desktop never builds:
> [ci-layering-corrected.md](ci-layering-corrected.md). It retires D30 and G1 and reopens Task 3.3.

## What is NOT in scope, and why

- **No permission surface** (Decision 4). `ccy`'s posture is a coherent four-point trust model whose
  premise is that the operator owns the workspace — not a loose default a flag could tighten. The
  price is stated: **`ccy` in CI is for trusted automation only**, and it is not a replacement for a
  fail-closed sandbox for untrusted checkouts.
- ~~**No overlay on project Dockerfiles** — the existing per-project seam is the mechanism.~~
  **Reopened by D31.** The seam is a *project-wide* choice, not a CI-time one: `.claude/ccy/Dockerfile`
  is the project's only Dockerfile and also builds the desktop image, so routing CI through it puts
  MCP on desktop. The corrected design layers the CI payload **above** the project image
  ([ci-layering-corrected.md](ci-layering-corrected.md)) — which is an overlay, stated as such and
  pending decision.
- **Phase 2 (`--non-interactive`) and token-by-value are desktop-only hardening.** Real defects
  worth fixing; no longer CI enablers.

## Honest status — three parts, not one

| Aspect                                     | Status                                                                                                                                                                                                                                                              |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Design**                                 | Specified — deliverable 1: keys, algorithm, writer, reader, comparison; deliverable 2: name, path, all 18 behaviours, caller form (D29). This row previously read "entrypoint **mechanism**" — true, and read by everyone as though the artifact were specified too |
| **The `LABEL`'s convention-proliferation** | **Contingent** — needs a migration in two repos this plan cannot schedule                                                                                                                                                                                           |
| **Behaviour in reality**                   | **Unproven** — nothing here has been run                                                                                                                                                                                                                            |

**Nothing in this plan has been executed.** Every claim is from source reading. The hardware-proof
checklist ([hardware-proof-checklist.md](hardware-proof-checklist.md)) lists what must be
demonstrated before any implementation task may be ticked; group F's **F1** — that an absent label
yields an empty string, so a naive comparison of two empties reports FRESH — is the single most
consequential unmeasured claim, because getting it wrong produces a check that always passes.

**`lts-infra` is not checked out here**, so a class of load-bearing citations — including one half
of D6's proof and the whole of D5's — is *previously verified, not currently verifiable*. See
[cross-repo-citation-status.md](cross-repo-citation-status.md).

## What this plan actually taught

Seven rounds found twelve instances of one failure mode: **a true statement about a check presented
as a stronger statement about the world.** A citation can be accurate and point at code that has
never executed (D10). A quality gate can be met while the work it governs is false (D15). A guard
one line above a citation can make an unconditional claim conditional (D16) — and so can a design
decision two rounds earlier in the same document (D21).

The countermeasure that worked is mechanical, not attentional: **before citing a line as evidence,
read the guard that decides whether it runs, and ask whether the enclosing file is on the path the
claim describes.**

A second defect appeared **ten** times: corrections were written correctly and never propagated to
the tasks they governed. The fix is equally mechanical — **after writing a correction that assigns
work, grep for the task that owes it.** Four of the ten were found that way in a single sweep, on a
list that was never hidden: every correction states what it assigns, and seven hostile review rounds
audited the *design* while taking the record of prior repairs at face value.
