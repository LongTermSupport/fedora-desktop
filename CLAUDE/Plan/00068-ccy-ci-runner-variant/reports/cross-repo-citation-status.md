# Plan 00068 — which of this plan's citations can be stood behind TODAY (D19)

D19 found that **`lts-infra` is not checked out in this workspace**. Listing
`/workspace/untracked/repos/` shows this repo and `actions-hub` (both cited below) alongside
several unrelated checkouts, but **no `lts-infra` directory**.

This plan's headline discipline is *"every claim is either cited to a `file:line` or explicitly
marked unverified with a named probe"* (Success Criterion 1, and see D15 on why that wording was
insufficient). A plan holding that standard must be able to say **which of its citations it can
still check**. This document is that inventory. It exists because recording D19 as a paragraph and
then carrying on citing lts-infra in the present tense would be the same omission this plan keeps
correcting.

**This is not a claim that any citation below is wrong.** Round 3 verified several when the repo
was present, and everything cross-checkable against this repo and `actions-hub` has held. It is a
claim about **status**.

---

## The three tiers

| Tier  | Meaning                                                                                  | Can D15's reachability standard be applied? |
| ----- | ---------------------------------------------------------------------------------------- | ------------------------------------------- |
| **A** | Source is in this workspace — re-readable and re-checkable right now                     | **Yes**                                     |
| **B** | Source is in this workspace but the cited path's *reachability* has not been traced      | Yes, and it has not been done               |
| **C** | **Source is NOT in this workspace** — read in an earlier session, not re-verifiable here | **No**                                      |

---

## Tier C — the ungrounded set (`lts-infra`, not checked out)

Every citation below was read in an **earlier session**. None can be re-verified in this
environment. Grouped by what rests on them.

| Citation                                                                                       | What rests on it                                                                                                                             | Consequence if wrong                                                                             |
| ---------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `runner-ccy-project-image.yml:172-195` (`runuser … podman build`)                              | **One half of D6's central proof** — that provisioning never invokes the launcher                                                            | D6's retraction weakens; the build-and-exit retraction would need re-argument                    |
| `runner-ccy-base-image.yml:119`, `:141` (fetches/reads, never runs)                            | The other half of the same D6 proof                                                                                                          | same as above                                                                                    |
| `runner-ccy-project-image.yml:90,107,176,258,324` + `runner.yml:114` (`runner_user: "runner"`) | **D5's retraction** of the "two different users" premise                                                                                     | D5's retraction would itself be wrong — the original clause might stand                          |
| `runner-ccy-project-image.yml:287-300` (`--entrypoint /bin/bash` IS LOAD-BEARING)              | Decision 6's "three codebases hand-rolled it, two got it wrong" argument                                                                     | Decision 6 loses one of its two production witnesses                                             |
| `runner-ccy-project-image.yml:19-23`, `:136-243`, `:296-300`                                   | Phase 3's constraint framing and Task 3.4's "mirrors the existing technique"                                                                 | Phase 3's steady-state design loses its precedent                                                |
| `RUNNER-VM-DESIGN.md` §5.4, §6.4, §7.4, §9 T1/T2                                               | Decision 4's containment context (**already withdrawn by D2**), the JIT-ephemeral-on-persistent-VM point, and Task 5.4's proof-battery shape | §7.4 loss would weaken the image-store-survives-jobs claim; §9 is a borrowed shape, not evidence |
| `lts.ccy.dockerfile-sha256` emitted by `runner-ccy-project-image.yml`                          | `label-convention-spec.md` §1 fact 2, and migration steps 3–4                                                                                | The migration's starting state is unconfirmed on one of its two sides                            |

**The two most consequential are the first three rows**, because D5 and D6 are *retractions* — and
a retraction resting on unverifiable evidence is a weaker thing than a claim resting on it. If the
`runner_user` citation is wrong, D5 retracted a true clause.

**Partial mitigation, stated so it is not overclaimed**: `lts.ccy.dockerfile-sha256` is
independently confirmed in `actions-hub/.github/workflows/ci.yml:99`, which **is** in this
workspace. That corroborates the key's existence and spelling on the consumer side. It says nothing
about what `runner-ccy-project-image.yml` does.

## Tier A — verifiable here, and re-verified this session

- `files/var/local/claude-yolo/Dockerfile:36`, `:215`, `:231`, `:248`
- `files/var/local/claude-yolo/lib/common.bash:456-472`, `:478-481`, `:487-498`, `:537-567`
- `files/var/local/claude-yolo/claude-yolo:1424-1442`, `:1452-1455`, `:1470-1530`, `:1796`,
  `:2505-2535`, `:2597`
- `files/var/local/claude-yolo/entrypoint.sh:255-263`
- `playbooks/imports/play-claude-yolo.yml:338-343`
- `vars/container-defaults.yml:10`
- `actions-hub/.github/workflows/ci.yml:82-99` — **cited as unreachable**, which is itself the
  verified finding (D10)

## Tier B — the three guards that needed tracing *(now DONE — results at the end, D20)*

D16 traced the preflight (`claude-yolo:2529`/`:2597`) and found it engine-conditional on the first
attempt. The same trace had not been done for the three below when this document was written; all
three have since been traced and **two changed something**:

- `entrypoint.sh:111` (the `api.github.com/meta` fetch, described as soft-fail with a fallback at
  `:130-133`) — is the fallback path actually reached, or is the soft-fail claim untraced?
- `claude-yolo:2747` (`container_cmd rm -f … | grep -q`) — under what conditions is this reached?
- `entrypoint.sh:269-274`/`:280-282` (sourcing `ccy.env`, `exec`ing `CCY_CLAUDE_WRAPPER`) —
  **E10 row 4 and Decision 4 both rest on this**, and it is guarded; the guard has not been read.

**The last one matters most.** E10's fourth trust assertion is the one that decided §5 of the
restatement and Decision 4's scope. D16's lesson is that a guard one line above a cited block can
make an unconditional-sounding claim conditional, and that trace has not been run here.

---

## What would clear this

1. **Check out `lts-infra` into this workspace** — then every Tier C row becomes Tier A/B and can
   be re-read, and D15's reachability standard can be applied to the two retractions that currently
   rest on unverifiable evidence.
2. ~~**Trace the three Tier B guards** — doable now, no external dependency.~~ **DONE (D20)** —
   see the resolved table at the end. Two of the three changed something, including a security
   downgrade this plan's own egress design would trigger.

Until (1), no round of the audit loop can ground a finding that turns on `lts-infra`, and any
review that appears to do so is reasoning from this plan's own prior text rather than from source.

---

## Tier B — RESOLVED (D20). All three traced.

| Guard                              | Result                                                                                                        |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `entrypoint.sh:269-274`/`:280-282` | **Confirms E10 row 4 and strengthens it** — both guards are satisfiable by the (attacker-controlled) checkout |
| `entrypoint.sh:111` → `:130-133`   | **NEW FINDING** — unreachable `api.github.com` ⇒ `StrictHostKeyChecking accept-new`; see D20(2)               |
| `claude-yolo:2747`                 | Clears — unconditional                                                                                        |

Tier B is now empty. **Tier C remains, and is only clearable by checking out `lts-infra`.**
