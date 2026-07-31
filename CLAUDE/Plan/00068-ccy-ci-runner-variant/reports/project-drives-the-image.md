# Plan 00068 — the project drives the image (D33)

**Status: proposed, pending the owner's confirmation of the reading below.** It retires deliverable
1 outright, withdraws D31, moots D30 and D32, and reverses the direction of Tasks 3.1, 3.2 and 3.4
— all ✅. The largest correction this plan has taken.

## The owner's model, restated for correction

> *"no — the project drives the image. ansible provides the VM and we provide a safe mechanism for
> project podman containers to run CI workloads."*

Three ownership boundaries, which the plan had drawn in the wrong places:

| Layer         | Owner                     | Responsibility                                                                     |
| ------------- | ------------------------- | ---------------------------------------------------------------------------------- |
| **The VM**    | Ansible (this estate)     | podman, the ccy base image, egress controls, the uid fence, the safe-run mechanism |
| **The image** | **the project**           | its own Dockerfile, its own tooling, its own MCP servers if it wants them          |
| **The run**   | **us**, via the mechanism | hardened `podman run` of whatever the project built                                |

## Why the LABEL convention was wrong — deliverable 1 is retired

The owner's instinct — *"why are we using a label — this feels wrong"* — is correct, and the reason
is sharper than taste:

**The label re-implements what the container engine already does.** `podman build` *is* the staleness
check. It rebuilds exactly the layers whose inputs changed, and is a fast cache hit otherwise. The
two properties the owner asked to have confirmed — a project Dockerfile change triggers a rebuild,
and a subsequent run starts quickly — are **native properties of `build`**, not things this project
needs to construct, specify, migrate two consumer repos onto, or prove with F1–F4.

The convention only looked necessary because Task 3.4 assumed the image was built **out of band, by
Ansible**. Once the image is built somewhere the checkout is not, something else has to answer *"is
this image current?"* — and that invented question is what the whole convention answers. Remove the
assumption and the question does not arise.

There is a second lesson in what it was modelled on. ccy's desktop mechanism
(`$HOME/.cache/claude-yolo-<project>-dockerfile-hash`, `claude-yolo:1454`) exists to avoid *invoking*
a build at all, and that optimisation is precisely where the "staleness state lives outside the
image" defect comes from. The plan correctly identified that defect — and then proposed porting the
workaround into CI, the one context that never needed either the optimisation or the state.

**Retired**: fact 4 (D30), §4.2's inheritance analysis as a *requirement*, and proof obligations
F1–F4 as CI blockers. The §4.2 rule (*each layer writes uniquely-named labels; never read a key you
might have inherited*) survives as general guidance for anyone who does use labels. Whether ccy's
**desktop** launcher should move its own staleness state into a label remains a real, pre-existing
desktop question — but it is not something ccy owes CI, and the owner's no-desktop-churn constraint
means it is not this plan's business.

## Why Task 3.4 was wrong at the ownership level

Task 3.4 specifies the CI image as *"built by **Ansible**, never per-job"*, on the sound-sounding
grounds that build-time fetches need egress a locked-down job must not have.

D32 treated the resulting problem as **scheduling** — *when* does the rebuild happen? — and tabled
three options for it. That was the wrong axis. The owner's question exposes it in one line:

> *"so how would a project add a new tool for CI purposes?"*

- **Ansible-built**: it cannot. It files a change against this repo and waits for a re-provision.
- **Project-driven**: it edits its own Dockerfile.

The first answer is a direct contradiction of the founding steer this plan exists to serve — *"allow
each project to have its OWN ccy runner in the NORMAL WAY — dockerfile customisation, custom tooling
etc etc — its perfect"*. Phase 3 walked away from that, and seven review rounds did not catch it
because every round audited the design against itself rather than against the steer.

**The egress tension behind 3.4 is real and survives**, but as a distinction between *phases of the
job* rather than a reason to move ownership:

- **build phase** — needs registry and package egress, by nature;
- **workload run** — locked down.

Same `arm → build → drop` shape the estate already uses, owned by the job rather than by
provisioning.

## What this dissolves

| Item                                             | Fate                                                                                     |
| ------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| **Deliverable 1** — LABEL identity convention    | **Retired.** The engine answers it                                                       |
| **D30** — `:ci` base breaks staleness            | **Moot.** No ccy-owned base for a project to build `FROM`                                |
| **D31** — CI layer above the project image       | **Withdrawn.** Unnecessary; **Task 3.3's "no overlay" decision stands**                  |
| **D32** — who builds the per-project image, when | **Answered.** The project, in its job, in a build phase with build-time egress           |
| **Tasks 3.1 / 3.2** — `Dockerfile.ci` as a base  | Wrong **direction**, not wrong details                                                   |
| **G1** — `--entrypoint` drops `tini`             | Survives in changed form: the safe-run mechanism owns the argv, so it must handle `tini` |
| **F1–F4**                                        | No longer CI blockers                                                                    |

**The MCP-on-desktop problem disappears entirely.** A project that wants the GitHub MCP puts it in
its own CI Dockerfile. Desktop never sees it. No ccy-owned layer, no base fork, no overlay, and
nothing for this plan to keep out of the desktop image because it never enters one.

## What `ccy` actually owes CI, restated

1. **The safe run mechanism** — a hardened `podman run` of whatever the project built: egress
   posture, uid fence, concurrency-safe container naming (C7), no `.claude/ccy/` writes into the
   checkout (Task 7.4, already decided **no** on evidence), `tini` as PID 1. **Substantially designed
   already** across Task 4.1, Tasks 5.1–5.4 and Task 7.4 — that work is unaffected by this correction
   and is where the plan's real value has accumulated.
2. **A CI entrypoint** — something must run *inside* the container to set the onboarding and trust
   state, or `claude` blocks on prompts with no TTY (D29, and unmeasured as G2/G3).

## The one new open question this creates

If the project builds the image, **where does the CI entrypoint come from?**

| Option                                                       | Consequence                                                                                           |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------- |
| The project `COPY`s it                                       | Needs a source the project can reach, and every project duplicates it                                 |
| `claude-yolo:latest` ships it as an inert file               | One shell script, never executed on desktop, no context cost — but the constraint said *nothing*      |
| **The safe-run mechanism bind-mounts it read-only** *(rec.)* | Desktop stays **byte-identical**; the mechanism already constructs the argv, so it handles `tini` too |

The third is recommended: it keeps the desktop-purity constraint absolute rather than argued, and it
puts the entrypoint in the same hand that owns the `podman run` line — which is where G1's `tini`
problem gets solved by construction rather than by asking callers to be careful.

## Honesty about this document

This is a re-architecture proposed on four sentences of steer. The reading at the top is stated
explicitly so it can be corrected rather than assumed, and nothing here is applied: Tasks 3.1, 3.2
and 3.4 remain ✅ and unedited in substance, with pointers added. If the reading is right, Phase 3
needs re-doing rather than patching, and that belongs to the implementation plan.
