# Plan 00121: run log secret scrubber and token scenario

**Status**: In Progress
**Created**: 2026-09-15
**Owner**: joseph
**Priority**: High

## Overview

Two gaps sit either side of the same boundary, and this repo has neither.

**Secrets are scanned at the git boundary only.** `scripts/git-hooks/lib/secret-scan.bash`
(356 lines, 29 tests) and `.gitleaks.toml` stop a secret entering version control. Nothing
operates on **runtime artefacts** — VM console logs, provisioning transcripts, run logs — which
live under `untracked/` and are never committed, so the commit gate never sees them. Plan 00110
records the consequence: `CLAUDE/Plan/.gitignore` and `PlanScriptStandards.md` R4 are both built
on "there is no run-log secret scrubber", and `DESIGN.md:329-337` keeps a PAT-bearing run's
transcript off the shared mount for exactly that reason.

**Plan 00063 has two obligations nothing can currently discharge.** Tasks 3.3 and 3.4 — the
GitHub-token path, and the SSH path's agent teardown, askpass removal and secret-file unlinking.
The six default VM scenarios each hardcode `RUN_BASH_GITHUB_ACCOUNTS=none`, so no PAT and no
passphrase ever exists in the guest and the assertions would pass by absence; 00110
`DESIGN.md:1590-1596` is explicit that a green grep there "passes because the thing it searches
for does not exist". `DESIGN.md:1554-1555` already names the route — the opt-in, human-gated
`server-github-token` scenario, designed and never built.

Production use does not close them either. A real deployment proves the clone works; it does not
prove the agent was torn down, the askpass helper removed, or the secret files unlinked. None of
those is visible from a run that succeeded — which is how `run.bash`'s teardown returned 0 with
the agent still holding an unlocked key until v1.21.0.

## Goals

- A **fail-closed** scrubber for runtime artefacts: redact known secret values, verify the
  redaction, and **refuse to publish** on any residual match.
- One detection engine, not two: reuse `hook_build_private_denylist` and
  `hook_scan_text_for_private` rather than a second matcher that drifts from the first.
- Build `server-github-token` so Plan 00063's Tasks 3.3 and 3.4 are **proven, not asserted**.

## Non-Goals

- **Not** relaxing the off-mount rule for PAT-bearing runs. A scrubber is fail-open by nature —
  it writes a file it *believes* is clean, and a miss is silent. Moving transcripts onto the
  shared mount on the strength of a new, unproven control would build the credential channel the
  bridge exists to prevent. `server-github-token` stays host-CLI-only regardless.
- Not a replacement for the pre-commit scanner. Different boundary, same engine.

## Key Decisions

**Redact by known value first, pattern second.** A PAT has a recognisable shape (`ghp_`,
`github_pat_`); an SSH passphrase is arbitrary text and has none, so pattern matching alone
cannot cover it. The run *supplied* both secrets and therefore knows their literal values —
redacting known literals is reliable, and pattern matching is the backstop for a shape nobody
anticipated, not the primary mechanism.

**Verify-and-refuse, not scrub-and-hope.** The output of a scrub is a file someone will trust.
If the verification pass finds anything, the artefact is not published and the run says so.

## Tasks

### Phase 1: The scrubber

- [ ] ⬜ **Task 1.1**: Decide where it lives and what consumes it — a helper under `helpers/`
  driven by the plan and vmtest scripts, versus a bash library beside `secret-scan.bash`. The
  existing engine is bash and the consumers are bash. Record the decision before writing code
- [ ] ⬜ **Task 1.2**: Known-value redaction, test-first: given the secret files a run was
  handed, replace every occurrence in an artefact with a stable placeholder
- [ ] ⬜ **Task 1.3**: The verification pass — re-scan the redacted artefact and refuse on any
  residual match. **The task this plan turns on**, and it must be falsified against an artefact
  carrying a secret the redactor deliberately missed
- [ ] ⬜ **Task 1.4**: Pattern backstop reusing `hook_scan_text_for_private`, so there is one
  matcher and one allowlist
- [ ] ⬜ **Task 1.5**: Wire into `qa-all.bash` with its own falsification fixture

### Phase 2: `server-github-token`

- [ ] ⬜ **Task 2.1**: Read 00110's contract before writing any of it — `DESIGN.md:329-337`
  (off-mount artefacts, verdict-plus-pointer response) and `:1553-1555` (which criteria it
  discharges)
- [ ] ⬜ **Task 2.2**: The scenario entry and its secret-file plumbing into the guest
- [ ] ⬜ **Task 2.3**: Off-mount logging to the host-local run directory, with only a verdict
  and a pointer returned
- [ ] ⬜ **Task 2.4**: Opt-in gating that refuses to run from the bridge — and a test proving
  the refusal, since a gate that only ever permits is this repo's cardinal defect
- [ ] ⬜ **Task 2.5**: The in-guest assertions: agent gone, askpass helper gone, secret files
  unlinked, no secret bytes in the environment or cloud-init `user-data`
- [ ] 🚫 **Task 2.6**: **HUMAN, needs a real credential** — run it. An agent must not create or
  handle the PAT, and the artefacts are off-mount by design, so the verdict comes from the
  operator

### Phase 3: Close out

- [ ] ⬜ **Task 3.1**: Discharge Plan 00063 Tasks 3.3 and 3.4 from the scenario's verdict
- [ ] ⬜ **Task 3.2**: `hl_cleanup:421` uses `rm -f` while its comment and the help text both
  say "shred" — reconcile the code with the claim, in whichever direction is right
- [ ] ⬜ **Task 3.3**: `hl_resolve_secret:134-140` accepts a literal secret on a non-cloud box,
  which is half of why 00063's "no secret bytes" criterion cannot be ticked from the environment
  side alone

## Success Criteria

- [ ] The scrubber refuses to publish an artefact that still contains a supplied secret, proven
  by a fixture where redaction was deliberately incomplete.
- [ ] One detection engine and one allowlist, shared with the pre-commit scanner.
- [ ] `server-github-token` runs from the host CLI, refuses to run from the bridge, and writes
  nothing secret-bearing to the shared mount.
- [ ] Plan 00063 Tasks 3.3 and 3.4 are discharged by a run id, not by an assertion.
- [ ] `./scripts/qa-all.bash` passes; the `qa-reviewer` agent has reviewed the result.

## Dependencies

- Plan 00063 (the obligations being discharged) — In Progress.
- Plan 00110 (the lab, the scenario contract, the bridge boundary) — Complete.
- Plan 00081 (the secret scanner whose engine is reused) — Complete.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00121-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan created; scope split from Plan 00063's Phase 3 obligations.
