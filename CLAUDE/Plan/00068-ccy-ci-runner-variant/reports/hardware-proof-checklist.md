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

| #   | Claim                                                                 | How to settle it                                                                                 |
| --- | --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| B1  | `select_token` **spins** on EOF (does not abort)                      | `ccy --headless --prompt x < /dev/null` with ≥2 token files present; observe hang vs exit        |
| B2  | The 32 abort sites abort **undiagnosably**                            | same, with exactly one token file; capture stdout/stderr and confirm no message names the prompt |
| B3  | The `alpine`/`google.com` preflight is reached and is fatal           | run with a network whose egress is restricted; expect the `exit 1` at `claude-yolo:2597`         |
| B4  | `--no-network` skips the preflight but leaves the container networked | `ccy --no-network`, then from inside the container attempt any outbound connection               |

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

The specific failure to guard against is the one this plan has now caught five times in its own
text: *a true statement about a check presented as a stronger statement about the world.* Reading
`claude-yolo:2514-2517` correctly tells you what the code does; it does not tell you what a
container on a real host can reach. Both are needed, and only one of them is in this repo.
