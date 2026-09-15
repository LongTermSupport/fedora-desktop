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

- [x] ✅ **Task 1.1**: A bash library under `scripts/lib/`, sourcing the existing hook engine.
  A Python helper — the repo's usual default — would mean porting the matcher and the allowlist,
  giving two detectors that agree until the day they do not. `scripts/git-hooks/lib/` is the
  wrong home because a run-log scrubber is not a git hook. Reasoning in the journal
- [x] ✅ **Task 1.2**: Known-value redaction — `scrub_redact` in
  `scripts/lib/run-log-scrub.bash`. Literal **bytes**, not text and not a pattern: an artefact
  may carry console control codes or invalid UTF-8, and a secret is an opaque byte string.
  Every occurrence, the file's trailing newline treated as the file's and not the value's, an
  empty secret file refused, and the write atomic so a crash cannot leave a part-redacted
  artefact that looks finished. Secrets arrive as file paths, never in argv
- [x] ✅ **Task 1.3**: `scrub_verify` re-reads the artefact and refuses if any supplied secret
  survives, naming which secret file and never the value — the message is read by a human and
  may be pasted into a ticket. An unreadable or empty secret file is also a refusal: a secret
  that could not be checked has not been shown to be absent
- [x] ✅ **Task 1.4**: `scrub_backstop` over `hook_scan_text_for_private`, sourced rather than
  reimplemented, so there is one matcher and one allowlist. Reports the **field name**, never
  the value. Two guards it would be wrong without: an empty denylist is refused, because the
  engine returns 0 early on one and a wrapper passing that through would scan zero tokens and
  report clean; and the artefact is projected to printable text first, because the engine reads
  through a command substitution that drops NUL bytes, so a console log could otherwise carry
  an identifier straight past the scan
- [x] ✅ **Task 1.5**: `scripts/test-run-log-scrub.bash`, 20 assertions, wired into
  `qa-all.bash`. Falsified on six mutants — a `scrub_verify` that never refuses, regex instead
  of literal matching, a tolerated empty secret, first-occurrence-only replacement, a dropped
  binary projection, and a tolerated empty denylist — each caught by the assertion written for
  it, against a clean 20/20 baseline

### Phase 2: `server-github-token`

- [x] ✅ **Task 2.1**: Read 00110's contract before writing any of it — `DESIGN.md:329-337`
  (off-mount artefacts, verdict-plus-pointer response) and `:1553-1555` (which criteria it
  discharges). Three findings change the build: a `planned` count alone puts a scenario on the
  **bridge** allowlist (`scenarios.py:339`), so `runnable` must split from bridge-reachable;
  `vmtest:860` enforces that same file, so excluding the scenario there would block the human's
  CLI too; and `bridge_run.py:220-228` would archive a PAT-bearing transcript onto the shared
  mount, with the allowlist as the only thing preventing it. Findings in the journal
- [x] ✅ **Task 2.2**: The scenario entry and its secret-file plumbing. The manifest can
  declare `host_only`, splitting "the host may run it" from "the sandbox may ask for it", and
  both enumerations derive from that one flag so they are disjoint by construction.
  `server-github-token` is declared (`bridge=6 host_only=1`). Secrets travel by `scp` into
  guest tmpfs and are named to `run.bash` by **path**, so they are absent from the guest's
  argv and from cloud-init `user-data`. `qa-vmtest-manifest.bash` now ties the manifest's
  `planned` to the checker's `PLANNED` — two declarations of one number, previously reconciled
  only minutes into a VM run
- [x] ✅ **Task 2.3**: Off-mount logging. Host-CLI runs already write to
  `~/.local/share/vmtest/runs/<run-id>/`, the path `DESIGN.md:330` names, so the work was not a
  new route but proving the archive step unreachable — `bridge_run` cannot dispatch the
  scenario at all. Phase 1's scrubber then redacts and **re-verifies** the transcript and
  console log before the run is judged; a residual match aborts rather than publishing
- [x] ✅ **Task 2.4**: Opt-in gating that refuses to run from the bridge. **Three independent
  gates**, none load-bearing alone: the scenario is off the bridge allowlist so the watcher
  rejects it; `bridge_run` refuses it from the manifest before dispatch; and
  `host_only_preflight` requires credential options the bridge's hardcoded argv cannot carry.
  Ten mutants falsified across the two suites, including a refusal placed after the run and
  an absent host-only list read as permission
- [x] ✅ **Task 2.5**: The in-guest assertions —
  `guest-acceptance-server-github-token.bash`, 12 checks. Its own script, because the shared
  server checker asserts `github_accounts: {}`, definitionally false here. Half assert the
  credential worked (SSH remote, `gh auth status`, a passphrase-protected key); half assert
  nothing survived (no agent, no socket, no askpass helper, no `/tmp/.github_ssh_pp`, no bytes
  in any process environment, in cloud-init `user-data`, or on disk). The host hands the
  secrets back in a 0600 needles file the checker unlinks before its first scan — "no secret
  bytes survived" cannot be checked by a script that does not know the bytes
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

- [x] The scrubber refuses to publish an artefact that still contains a supplied secret, proven
  by a fixture where redaction was deliberately incomplete.
- [x] One detection engine and one allowlist, shared with the pre-commit scanner.
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
- Phase 1 delivered: `scripts/lib/run-log-scrub.bash`, its falsified 20-assertion suite, and
  the `run-log-scrub` stage in `qa-all.bash`. The off-mount Non-Goal is unchanged by it.
