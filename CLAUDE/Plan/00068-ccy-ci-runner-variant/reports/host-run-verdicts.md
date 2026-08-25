# Plan 00068 — verdicts from the HOST runs of `triage.bash` (Task 1.1)

Triage renders **no verdicts** by design (`PlanScriptStandards` R9) — the probes state facts and
stop. This document is where those facts become decisions. It covers two host runs:

| Run               | Legs     | Status                                                                   |
| ----------------- | -------- | ------------------------------------------------------------------------ |
| `20260731-214921` | 1 of 4 ✗ | **Superseded.** Two probes measured the wrong thing — see §4             |
| `20260731-225344` | 4 of 4 ✓ | **Authoritative.** `all legs OK` — every probe reached a definite answer |

---

## 1. E6 — ccy's unconditional `--device /dev/dri` is a CONFIRMED blocker for a headless runner

Two facts, from the authoritative run:

- `--device /dev/dri:/dev/dri` on this host → **exit 0, accepted** (`/dev/dri` exists here:
  `card0`, `card1`, `renderD128`, `renderD129`).
- `--device /dev/plan00068-definitely-absent:…` → **exit 125**,
  `Error: stat …: no such file or directory`.

**Verdict.** `podman` refuses the command line *before any container runs* when a `--device` path
is absent. `ccy` passes `--device /dev/dri:/dev/dri` unconditionally (`claude-yolo:2773`), and a
headless runner VM has no `/dev/dri`. So **every `ccy` invocation on a headless runner aborts at
125 before Claude starts.** This is a hard blocker for the plan's central goal, not a degradation.

**The fix is already in the codebase as a pattern**: `GUI_MOUNTS` is assembled conditionally at
`claude-yolo:2703-2727`. `--device` must be built the same way — included when the node exists,
omitted when it does not. No new flag, no new concept.

*Note on the exit code: 125 is `podman`'s reserved "the engine failed before the container ran".
That distinction is what makes this a device finding rather than a container finding — see §4.*

## 2. C3 — `--network pasta:…` and `--network <name>` ARE mutually exclusive. Measured, not borrowed.

The four results that license it — all from the authoritative run:

| Case                                       | Result                        |
| ------------------------------------------ | ----------------------------- |
| `--network pasta:-T,3128` alone            | **exit 0 — accepted**         |
| `--network podman` alone                   | **exit 0 — accepted**         |
| `--network pasta:-T,3128 --network podman` | **exit 125 — engine refused** |
| `--network podman --network pasta:-T,3128` | **exit 125 — engine refused** |

```
cannot set multiple networks without bridge network mode, selected mode pasta: invalid argument
can only set extra network names, selected mode pasta conflicts with bridge: invalid argument
```

**Verdict.** Both singles succeed, both orderings of the combination are refused by the engine
itself. That is exactly the pattern C3 asserts, and it is now **the first direct measurement of it
under `ccy`'s container shape** rather than a claim borrowed from another repo's runner. The two
orderings produce *different* error messages — two distinct refusal paths — which additionally
rules out "last flag wins", the third behaviour the probe was designed to detect.

**Consequence for Task 5.1**: its specified HARD ERROR when `--egress` and `--network` are combined
is **grounded**. It was previously resting on a borrowed claim.

**C1 and C2 remain borrowed and unverified** — both need a listener on the host, which would stop
the probe being read-only. Recorded as open, not quietly closed by an easier adjacent probe.

## 3. Group F — the label reader, and why it changes nothing

F1 confirmed: an absent label returns **exit 0 with 0 bytes** — silent empty, so the hazard the
spec assumed is real, and the naive comparison `[[ "$wanted" == "$have" ]]` returns **FRESH** when
both sides are unknown. The prescribed comparison (assert `wanted` non-empty first) returns
**REBUILD**. The positive control discriminates: label `2.22` → FRESH against `2.22`, STALE
against `9.99`. F3: `claude-yolo:latest` on this host does carry `claude-yolo-version=2.22`.

**Verdict: no action.** These were only ever blockers for the `LABEL` identity convention, which
`D33` retired (`PLAN.md` Decision 2 and a Non-Goal). The measurements stand as a correct answer to
a question this plan no longer asks. They are kept because *"the reader returns empty rather than
erroring"* is a genuine trap for anyone who does use labels — including whoever eventually looks at
the **desktop** `$HOME/.cache` staleness question, which is out of scope here.

## 4. Why the first run was superseded — the probe, not the host, was wrong

`20260731-214921` reported `--device /dev/dri` as **"EXIT 1 — REJECTED"** and declared C3
**UNANSWERED**. Both were artefacts of the probe.

The probes passed `true` as the **command**, which does not replace an `ENTRYPOINT` — and the ccy
image sets one (`Dockerfile:215`, `tini` → `entrypoint.sh`). So every container started, ran the
desktop entrypoint, and died on `ERROR: GH_TOKEN environment variable not set` (exit 1). The probe
read that as the flag being rejected.

Fixed by `--entrypoint true` plus discriminating **125 from everything else**. The effect:

| Case                | First run                | Authoritative run |
| ------------------- | ------------------------ | ----------------- |
| `--device /dev/dri` | exit 1 → "REJECTED" ✗    | exit 0 → accepted |
| pasta alone         | exit 1 → baseline failed | exit 0 → accepted |
| podman alone        | exit 1 → baseline failed | exit 0 → accepted |

**The combination results were byte-identical across both runs** — exit 125, same two messages.
Nothing about the host changed. What changed is that the baselines now succeed, which is what
licenses attributing the 125s to exclusivity rather than to pasta being unavailable.

**Two design choices earned their keep**, and both were arguments the probe made against its own
convenience:

1. **Running the single-flag baselines at all.** Without them the first run would have shown a
   failing combination and it would have been easy to bank C3 as confirmed. The probe instead
   refused, printing *"This is not a refutation of C3 and must not be recorded as one — it is the
   measurement not being available here."* The convenient reading and the correct one pointed the
   same way; only the baselines showed the reasoning was unsound.
2. **Discriminating exit 125.** A container that runs and then fails says nothing about flag
   support. Collapsing "non-zero" into "rejected" is precisely what produced the false C3 answer
   and the false `--device` answer.

The lesson is the plan's own recurring one (`.claude/rules/bash-standards.md` §9): **a true
statement about a check is not a statement about the world.** "The command exited non-zero" was
true in both runs. "The flag was rejected" was false in both.
