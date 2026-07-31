# Plan 00068 — what a later implementation plan must PROVE on real hardware (Task 6.5, part 2)

Task 6.5 has two halves: a one-page restatement of the design, and this — the list of things that
must be demonstrated on real hardware before any implementation task may be marked ✅. The
restatement is deliberately held until the Round-2 audit reports, since that is the half an audit
finding could invalidate. This half is stable: it is a list of open questions, and a hostile
finding would *add* to it rather than change what is already here.

**Why this list exists at all.** Every item below is something this plan currently asserts from
*source reading* or from *another repo's measurement*. None of it has been run. The plan's own
discipline — Task 1.1's preamble, and the `/dev/dri` result that overturned an inference — is that
a nested-container result is not evidence about the host, and a code path is not evidence about a
running system.

**All of these are HOST runs.** This session is inside a podman container; `triage.bash` already
refuses to run there by design, creating no report rather than a half-written one that could be
mistaken for evidence.

---

## A. Claims this plan makes from code paths, which runtime state could contradict

| #   | Claim                                                           | Command that settles it                                           | Why it matters                                                                                         |
| --- | --------------------------------------------------------------- | ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| A1  | `claude-yolo:base` does not exist on an Ansible-provisioned box | `podman image ls claude-yolo --format '{{.Repository}}:{{.Tag}}'` | Three documents offer `FROM claude-yolo:base`. If absent, that contract is broken today (Phase 3 §0.2) |
| A2  | `claude-yolo:full` is not a tag                                 | same output — confirm `full` is absent and `latest` present       | Decision 1 and Task 3.1 both named it; corrected on source reading alone                               |
| A3  | The version gate does not rebuild on a provisioned box          | run `ccy --help` on a freshly-converged box, observe no build     | If it *does* rebuild, A1 resolves itself and Phase 3 step 5 is unnecessary                             |
| A4  | The project image tag is `claude-yolo:<dirname>`                | `podman image ls` in a project with `.claude/ccy/Dockerfile`      | Task 3.2's collision residual depends on it                                                            |

**A1 is the load-bearing one.** If it returns `claude-yolo:base`, my inference is wrong and Phase 3
§0.2 must be retracted — which is the outcome I would most want to know about.

## B. Behaviour classified but never executed

| #   | Claim                                                                                              | How to settle it                                                                                                                                                                               |
| --- | -------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1  | `select_token` **spins** on EOF (does not abort)                                                   | `ccy --headless --prompt x < /dev/null` with ≥2 token files present; observe hang vs exit                                                                                                      |
| B2  | The 32 abort sites abort **undiagnosably**                                                         | same, with exactly one token file; capture stdout/stderr and confirm no message names the prompt                                                                                               |
| B3  | The preflight is fatal **when a network is selected** — and is SKIPPED ENTIRELY when none is (D16) | two runs: (a) podman default — expect `exit 1` at `claude-yolo:2597` on restricted egress; (b) **Docker with no `--network`, no compose, no auto-connect** — expect the preflight never to run |
| B4  | `--no-network` skips the preflight but leaves the container networked                              | `ccy --no-network`, then from inside the container attempt any outbound connection                                                                                                             |

B1 and B4 are the two that would change a decision if they came back the other way. B4 in
particular: if `--no-network` *does* isolate, R11 is wrong and Task 5.1 shrinks back to a naming
problem.

## C. Cross-repo measurements reused, never re-measured under `ccy`

| #   | Claim                                                             | Note                                                                                                                               |
| --- | ----------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| C1  | `pasta:-T,3128` forwards exactly one port                         | Measured by the consumer on **its** runner. `ccy`'s container shape differs — writable `/workspace` bind mount, desktop entrypoint |
| C2  | `--map-host-loopback` exposes the host's entire loopback          | Same provenance (probe V9). Reused as the reason for rejection, so if wrong the rejection is unfounded                             |
| C3  | `--network pasta:…` and `--network <name>` are mutually exclusive | The basis for Task 5.1's "hard error" specification — the single most consequential borrowed claim                                 |

**C3 is the one to re-measure first.** The entire specification of `--egress`/`--network`
interaction rests on it, and it is the only borrowed claim that drives a *hard failure* rather
than a warning.

> **C3 now has a probe** — [`../probe-network.bash`](../probe-network.bash), wired in as a
> `triage.bash` leg. The design point is the baselines, not the combination: it runs each
> `--network` flag **alone** as well as together, in **both orders**, because a failing
> combination is equally consistent with pasta simply being unavailable on the host, and
> because "rejected" and "last flag wins" are different behaviours one ordering cannot
> distinguish. If either single-flag baseline fails the probe reports C3 **UNANSWERED** rather
> than banking the convenient reading. It also enumerates the outcome where the flags turn out
> **not** to be exclusive — which would invalidate Task 5.1's hard-error specification, and is
> the result most worth having.
>
> **C1 and C2 remain borrowed and unverified, deliberately.** Both need a listener on the host
> to mean anything, and a probe that opens host sockets is no longer read-only. Recorded as
> open rather than closed by a probe that measured something adjacent and easier.

## D. The proof battery that must exist before egress may be called a control

Specified in `phase45-mcp-and-egress.md` Task 5.4. Three probes, and **each must assert a specific
outcome, not merely non-zero**:

1. An allowlisted host returns a **real HTTP status** through the proxy (`401`/`404` counts — it
   proves TLS reached the origin).
2. A non-allowlisted host is refused with **`403` from the proxy** — explicitly *not* a timeout. A
   timeout proves only that something failed, which is the weaker claim that gets mistaken for the
   stronger one.
3. A direct `:443` bypass attempt from the workload uid is **DROPPED**. Without this, 1 and 2
   together prove only that the proxy works *when used*.

## E. Prerequisites — things that must work before any of the above can be attempted

| #   | Item                                   | Status                                                                                       |
| --- | -------------------------------------- | -------------------------------------------------------------------------------------------- |
| E1  | Task 1.1's remaining `triage.bash` run | **Outstanding — needs a human.** Image provenance, deployed-vs-checkout drift, prompt census |
| E2  | A token supplied by value              | Blocks every unattended item in B — without it, B1 is what you hit first                     |

---

## The rule this list is written to enforce

**No implementation task may be marked ✅ on the strength of anything in this plan alone.** Every
document here closes with its own "what this does not settle", and this checklist is the union of
those sections made actionable.

The specific failure to guard against is the one this plan has now caught seven times in its own
text: *a true statement about a check presented as a stronger statement about the world.* Reading
`claude-yolo:2514-2517` correctly tells you what the code does; it does not tell you what a
container on a real host can reach. Both are needed, and only one of them is in this repo.

---

## ⚠ CORRECTIONS APPLIED AFTER THIS DOCUMENT WAS WRITTEN

This report is preserved as written (line numbers are cited by later review rounds). The
correction blocks at the head of [../PLAN.md](../PLAN.md) are AUTHORITATIVE where they differ.
Appended per **D9**, which found that none of the six reports carried any correction note.

- **E2 ("a token supplied by value") is now desktop-only** per D6 — it gates the unattended items
  in group B, but no CI job reaches the launcher. The B-group probes remain worth running.
- Everything else here is unaffected: this document is a list of open questions, and the
  corrections since have only added to it.

---

## F. Added by the `LABEL` convention spec (D10 / [label-convention-spec.md](label-convention-spec.md))

| #   | Claim                                                                                                          | Command that settles it                                                                             | Why it matters                                                                                                                                                    |
| --- | -------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| F1  | An **absent** label yields an empty string from `--format '{{index .Config.Labels "…"}}'` rather than an error | build an image with no such label, then inspect it                                                  | **The load-bearing one.** If both sides compute empty, the staleness check compares `""` to `""` and reports FRESH — a check that fires and does not discriminate |
| F2  | A project image built before the convention is treated as STALE, not as a pass                                 | run the comparison against a pre-convention project image; expect a rebuild trigger                 | This is F1's consequence, and it is what makes the migration self-healing rather than silently broken                                                             |
| F3  | `claude-yolo:latest` actually carries `claude-yolo-version` on a provisioned box                               | `podman image inspect claude-yolo:latest --format '{{index .Config.Labels "claude-yolo-version"}}'` | Fact 3's *wanted* side reads this label; if it is empty the base-version check degrades to a no-op                                                                |

**F1 is the single most consequential unproven claim in the specification**, for the same reason
group D exists: a control that always passes looks identical to a control that works, right up
until it is needed.

### F now has an executable probe (appended after the fact)

Group F was added by the `LABEL` spec, one day after `probe-engine.bash` and
`probe-launcher.bash` were written — so for its whole life it named the most consequential open
question in the specification and had **nothing on disk able to answer it**. That gap is closed
by [`../probe-label.bash`](../probe-label.bash), wired in as `triage.bash`'s third leg.

| Item | How the probe settles it                                                                                                                                                                                                                                |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| F1   | Builds three `FROM scratch` label carriers — **no labels** (nil map), **one unrelated label** (non-nil map, key absent — what a real pre-convention image looks like), **both keys** (control) — and records exit code and **byte count** for each read |
| F2   | **Executes both comparison shapes** against the pre-convention image: the naive one, to see whether the two-empties no-op actually reproduces, and the prescribed non-empty-assertion one                                                               |
| F3   | Reads `claude-yolo-version` off the real `claude-yolo:latest`, with absence recorded as a finding rather than a failure                                                                                                                                 |

Two caveats, stated because this checklist exists to stop exactly this kind of slippage:

- **It is the one probe in this plan that is not read-only.** It builds and removes three
  images, and refuses to run rather than reuse or overwrite a tag that already exists.
  `triage.bash`'s header claimed "nothing is built" for the whole script; that claim was
  corrected in the same change rather than left to rot.
- **Written is not run.** Its container refusal is verified; every measurement above is still
  **unmeasured** until the owner runs it on the host. A probe that exists is not evidence.

---

## Coverage map — which groups have a probe, and why the rest do not

Written after F and C were given probes, so that the groups still without one are *recorded as
uncovered* rather than merely absent from the script. An unexplained gap gets re-derived by every
subsequent reader; worse, it gets mistaken for a decision.

| Group | Probe                            | Status                                                                                      |
| ----- | -------------------------------- | ------------------------------------------------------------------------------------------- |
| A     | `probe-engine.bash` (provenance) | **Covered.** Its image-provenance section already prints exactly what A1, A2 and A4 ask for |
| B     | —                                | **Not automatable safely — see below**                                                      |
| C     | `probe-network.bash`             | **C3 covered.** C1/C2 need a host listener, so they stay borrowed                           |
| D     | —                                | **Premature.** Needs proxy infrastructure that does not exist yet                           |
| E     | `probe-launcher.bash` (E1)       | E1 covered; E2 is a credential the owner supplies, not a measurement                        |
| F     | `probe-label.bash`               | **Covered**                                                                                 |

**Why group B has no probe, stated rather than skipped.** B1–B4 all require actually running
`ccy`, and two properties make that unsafe to hand over as a script:

- **The discriminating signal is not the exit code.** A spin bounded by `timeout` exits 124 — and
  so does a run that *did not* spin and instead launched a real Claude session that `timeout` then
  killed. The two outcomes are distinguishable only by inspecting captured output, so the probe
  would have to launch a real container and a real session to learn anything, consuming quota and
  producing side effects on the owner's machine.
- **B1 and B2 need a specific token-file population** (≥2 files for B1, exactly 1 for B2). Creating
  or removing those files means a script manipulating the owner's credential store — which this
  plan will not do, by the same rule that keeps it from handling plaintext credentials.

Group B is therefore an **interactive investigation with the owner**, not a hand-over script. That
is a real remaining gap, and B1 and B4 are the two the checklist already identifies as able to
change a decision if they come back the other way.

**Why group D has no probe.** It specifies the proof battery that must exist *before egress may be
called a control* — three probes against a proxy that has not been built. It is a requirement on a
future implementation, not a measurement available on today's host.
