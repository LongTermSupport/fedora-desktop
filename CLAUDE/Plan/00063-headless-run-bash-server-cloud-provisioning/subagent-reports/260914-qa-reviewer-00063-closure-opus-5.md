# QA Review — Plan 00063 closure diff (`PLAN.md`, uncommitted)

**Verdict: FIX-BEFORE-MERGE.** Not safe to mark Complete as written. The closure is
honest in intent and says so repeatedly, but in four places it claims more than the
evidence supports, and in one of those the narrowing is invisible because the wording
that named the obligation was deleted in the same edit.

## Blocking for this closure

### 1. CONFIRMED — Task 3.4 discharges teardown assertions production use cannot reach, and the enumeration saying so was deleted in the same edit

The removed Task 3.4 enumerated "`hl_ssh_agent_start`/`hl_ssh_agent_stop` bracketing the
run, the transient `SSH_ASKPASS` helper gone afterwards, `hl_cleanup` firing on EXIT, and
the secret files deleted after use." `PLAN.md:172-175` replaces all of it with "a real
deployment clones over SSH on every run."

A successful clone proves `hl_ssh_agent_start` (`/workspace/run.bash:436-462`) and the
`ssh-add` load. It proves nothing about `hl_ssh_agent_stop` (`run.bash:467-474`),
`hl_cleanup` (`run.bash:420-428`), askpass-helper removal, or secret-file deletion. Worse,
`run.bash:471` warns and continues on a non-zero teardown:

```
471:    warning "ssh-agent teardown returned non-zero (agent may already be gone): ${_o}"
```

so a broken teardown still exits 0 and is indistinguishable from a working one in exactly
the evidence being cited — a check that cannot fail.

Independent corroboration from the lab's own design,
`/workspace/CLAUDE/Plan/Completed/00110-vm-lifecycle-acceptance-testing/DESIGN.md:1555`:

> Secret files unlinked after use; no `ssh-agent` left behind (Tasks 2.2, 2.4) — **Only via
> `server-github-token`** … With `GITHUB_ACCOUNTS=none` there is no token file and no SSH
> passphrase file to unlink and no `ssh-agent` to leave behind, so the assertions would pass
> by absence.

And `PLAN.md:112` now points Task 2.2's outstanding "delete-after-use and ssh-agent
teardown" at Task 3.4, which covers neither. That is the same shape this plan's own
`JOURNAL/00063-Journal-26-09-14.md` 11:05 entry says a review returned BLOCK on this
morning (obligations transferred to a task that verifies none of them).

**Fix:** restore the enumeration and mark the teardown / delete-after-use half
undischarged — or discharge it in-container. Both functions are extractable by the
awk-extract-and-source pattern with no credential; `run.bash:165` shows the precedent
already in use on this very file.

### 2. CONFIRMED — "No secret bytes enter the environment or cloud-init `user-data`" is ticked on citations covering neither half

`PLAN.md:217-220`. Both cited lines are real and I verified them:

- `run.bash:373` — `unset RUN_BASH_VAULT_PASSWORD RUN_BASH_GITHUB_TOKEN RUN_BASH_GITHUB_SSH_PASSPHRASE RUN_BASH_SUDO_PASSWORD`
- `run.bash:2254` — `printf '%s' "$HL_GITHUB_TOKEN" | gh auth login --with-token`

But the criterion is not established by them:

1. **The cloud-init `user-data` half is untouched by either line.** The only guard is
   `hl_is_cloud` (`run.bash:104-106`, `[[ -d /var/lib/cloud/instance ]]`) refusing a
   literal at `run.bash:135-138`. The closure does not cite it.
2. **`hl_resolve_secret:134-140` accepts a literal on a non-cloud box**, with a warning
   only. So "Values come from `0600` file pointers" describes how the operator invoked it,
   not a property the code enforces.
3. `:373` only stops *children* inheriting; the literals were in this process's environment
   until that line.

`00110/DESIGN.md:1553` is explicit — "**NOT dischargeable by the default scenarios** … 
Dischargeable only by `server-github-token`" — and `:1590-1596` adds "A green grep here
would be a check that passes because the thing it searches for does not exist."

**Fix:** restate the evidence as inspection of the `*_FILE` path plus the `hl_is_cloud`
literal refusal, and say it was not exercised against real secrets — or leave it unticked.

## Should fix

### 3. CONFIRMED — "structurally cannot" is false

`PLAN.md:180-182`. `00110/DESIGN.md:336` — "`server-github-token` is host-CLI-only and
never reaches the bridge"; `:1554` — "Only via the opt-in, human-gated
`server-github-token` scenario (§10)". Specified, deliberately not built. This plan's own
journal 09:14 says exactly that: "a seventh scenario (`server-github-token`, specified in
Plan 00110 DESIGN.md §10 but deliberately never built or put on the bridge) plus a
throwaway PAT the owner provides."

What **is** true, verified: the six built scenarios all hardcode `none`
(`files/home/.local/bin/vmtest:749` desktop path, `:942` server path) and no scenario can
override it, because `RUN_ENV_KEYS` (`/workspace/helpers/vmtest/scenarios.py:70`) is a
closed three-key list — `RUN_BASH_PROVISIONING_PROFILE`, `RUN_BASH_OPTIONAL_PLAYBOOKS`,
`RUN_BASH_REBOOT` — enforced at `:230-231`.

**Fix:** "the lab as built does not reach them, and the scenario that would needs a real
PAT the owner must supply" — not "waiting for something that could never arrive."

### 4. CONFIRMED — the missing-value criterion downgrades its own evidence and files a follow-up that already shipped

`PLAN.md:206-210` says "Verified by run and by inspection, **not** by a test";
`PLAN.md:186-189` proposes mechanising it with the awk-extract-and-source pattern.

`CLAUDE/Plan/00063-headless-run-bash-server-cloud-provisioning/acceptance.bash:79-129`
already does it: 10 `expect_fail` cases covering missing email, malformed email, missing
`RUN_BASH_GITHUB_ACCOUNTS`, missing `RUN_BASH_GITHUB_TOKEN_FILE`, missing SSH passphrase,
both-forms-set, unreadable `*_FILE`, and the NOPASSWD gate — asserting exit non-zero **and**
the message text. This plan's own Task 2.8 (`PLAN.md:135-140`) cites it. The
awk-extract-and-source precedent is also already live on `run.bash` itself (`run.bash:165`).

Net: the closure replaced 10 asserted gates with "three invocations" that carry no run id,
no artefact and no journal entry, then filed a follow-up to build what already exists.

**Fix:** cite `acceptance.bash` and its 10 gates; delete the follow-up note.

### 5. CONFIRMED — `[x] ❌` misuses the repo's own icon legend

`CLAUDE/Plan/README.md:30` defines ❌ as "FAILED — Attempted but failed (requires rework)".
Task 3.2 (`PLAN.md:157`) and the desktop criterion (`PLAN.md:221`) were never attempted, so
❌ asserts something false. `CLAUDE/Plan/README.md:33` — "💤 DORMANT — Paused indefinitely,
blocked on an external/human decision" — is the accurate marker.

`[x]` + ❌ + "not formally verified" is three disagreeing signals on one line, and any
reader or checkbox-counting tool sees a tick. Answering the question directly: **no, it is
not honest** — it is a completed-looking tick over an unattempted item.

**Fix:** `[ ] 💤` with the same prose. If a Complete plan may not hold an unticked box, the
desktop item belongs in Non-Goals or a follow-up plan, not ticked.

### 6. CONFIRMED (as an asymmetry) — production use is cited with no observable

Task 3.1 (`PLAN.md:147-156`) carries run ids (`20260913T170901Z-server-fast-provision`).
Tasks 3.3/3.4 (`PLAN.md:164-175`) carry "the owner reports", with no date, no host count,
no artefact. The Status line (`PLAN.md:3-4`) is the only source and is second-hand.

**Fix:** record what the owner can attest — a date and "N hosts provisioned via the
headless path". That needs no repo name and stays inside the public-repo rules.

### 7. CONFIRMED — the version evidence cannot fail

`PLAN.md:229-232` offers "version 1.20.2" as proof that `RUN_BASH_VERSION` was bumped.
1.20.2 is Plan 00119's (`docs/run-bash-changelog.md:18`); this plan's bump is 1.10.0
(`docs/run-bash-changelog.md:187` — "1.10.0 — headless flows through the full body (Plan
00063)"), already listed under Delivery & Milestones. A shared counter's current value
reads identically whether or not this plan bumped anything.

Same shape for the hygiene half: I measured 19 `2>/dev/null` and 18 `sed` occurrences in
`run.bash`; a repo total cannot distinguish pre-existing from new. `|| true` = 0 is the one
conclusive figure of the three.

**Fix:** cite 1.10.0; cite Task 2.8's per-slice check for the hygiene half, or drop the
totals.

### 8. CONFIRMED — the closure reverses a same-day journal decision with no journal entry

`JOURNAL/00063-Journal-26-09-14.md` 09:14 ends "So this plan is blocked on a human for
everything that remains, and honestly so." The last entry is 11:05; `PLAN.md` was modified
at 20:41. Nothing records the reversal or its basis.

**Fix:** append a `decision` entry naming production use as the new evidence source and
stating what it does and does not cover.

### 9. CONFIRMED — plan-tree hygiene, with a correction to the dispatch's item 8

`plan-qa --sweep` (exit 1, 3 advise / 0 block):

```
- [advise] location-status-coherence [00063-headless-run-bash-server-cloud-provisioning]:
  Plan folder has a terminal status (Complete) but is still in the active root
```

Owed: (a) `git mv CLAUDE/Plan/00063-… CLAUDE/Plan/Completed/`; (b) move
`CLAUDE/Plan/README.md:130` from `## Active Plans` to `## Completed Plans` (`:166`) and
repoint the link to `Completed/00063-…/`.

**Correction: there are no statistics to update.** `CLAUDE/Plan/README.md` has no counts
block — its headings are Plan Workflow, Active Plans, Completed Plans, Cancelled Plans,
Archive, Creating New Plans, Plan Workflow Integration, References, and nothing numeric.
The daemon's "update the README row/stats" string is generic boilerplate.

Also: the existing row text describes the goal. Every other Completed row (`:168`, `:170`,
`:171`) states an outcome. Rewrite it to say what shipped and that the desktop-interactive
regression was closed unverified.

## Nits

- **`--help-run-headless` documents every input, but three are ungreppable.** Re-derived
  independently: 23 `RUN_BASH_*` tokens in `run.bash`, minus the script's own
  `RUN_BASH_VERSION` = 22 inputs; 19 appear literally in the help block (`run.bash:892-1028`).
  The three absent are the literal secret forms, written at `run.bash:973-974` as bare
  suffixes — `_GITHUB_TOKEN`, `_GITHUB_SSH_PASSPHRASE`, `_SUDO_PASSWORD`. So the claim is
  substantively true, but by string comparison the cross-check is 19 of 22 and needs a human
  to resolve the rest; a user grepping the help for `RUN_BASH_SUDO_PASSWORD` finds nothing.
  Spelling them in full costs one line.
- `hl_cleanup:421` is `rm -f`, while its own comment (`run.bash:415`) and the help text
  (`run.bash:972`) say "shred" / "Shredded on every exit path". Outside the diff, but the
  closure discharges "delete-after-use", so the wording overstates what runs.
- The plan cites `gh-account-setup.bash` bare; it lives at `/workspace/scripts/`, not under
  `files/home/.local/bin/`.

## Checked and clean

- **Public-repo leak sweep: clean, and not blind.** Sourced
  `scripts/git-hooks/lib/secret-scan.bash`; `hook_build_private_denylist` returned 0 with
  **10 entries** (so it was loaded, not empty), and `hook_scan_text_for_private` over
  `PLAN.md`, `DESIGN.md`, `DECISIONS.md`, `PLAN_archive.md`, `acceptance.bash` and all four
  `JOURNAL/` files produced **zero** field names. Independent greps: no non-`example.com`
  email (only `git@github.com` in `PLAN_archive.md:201` and the 26-07-23 journal, a standard
  remote); no real home path (`/home/.local` at `PLAN.md:160` is the repo path
  `files/home/.local/bin/vmtest`); no `/Users/`. "a separate infrastructure repository" is
  unnamed throughout. `joseph` in the Owner field is the maintainer's public handle,
  permitted per `CLAUDE/AgentNotes.md`. The denylist temp file was removed immediately.
- **Dispatch item 1: CONFIRMED, desktop included.** `vmtest:749` (`session_provision`,
  the desktop path) and `:942` (the server SSH path) both hardcode
  `RUN_BASH_GITHUB_ACCOUNTS=none`, and the manifest allowlist forbids a scenario override.
- **Dispatch item 2: CONFIRMED** as literal facts — see Blocking #2 for what they do and do
  not prove.
- **Dispatch item 4: CONFIRMED.** `scripts/gh-account-setup.bash:287-291` errors, names
  `RUN_BASH_GITHUB_TOKEN_FILE`, and `exit 1` — before the `gh auth login --web` device flow
  at `:305`. Its `== "true"` comparison is correct because `run.bash:2746` passes the
  normalised `RUN_BASH_HEADLESS="$HEADLESS"` to the child, not the operator's raw `=1`.
- **Dispatch item 5: not a contradiction.** "code-complete at v1.10.0" is correct
  (`docs/run-bash-changelog.md:187`); 1.11.0 onward belong to Plans 00073, 00065, 00108 and
  00119. The defect is the criterion citing 1.20.2 — see Should fix #7.
- `--help` cross-references: three, at `run.bash:858`, `:874`, `:886`. Deep-dive length: 137
  lines exactly (`:892`–`:1028`). "QA green at 860 files" and "`|| true` zero occurrences":
  both verbatim correct.

## Mechanical gates

- **`scripts/qa-all.bash`**: PASS, exit 0 — "✓ QA passed: 860 files checked". The shellcheck
  168-advisory line and the semgrep partial-parse list are pre-existing and non-failing.
- **`hooks-daemon plan-qa --sweep`**: exit 1, 3 advise / 0 block. One is this plan's
  `location-status-coherence`; the 00046 path-existence item and the 12-plan
  journal-freshness item are pre-existing and not this diff's.
- **`ansible-playbook --syntax-check`**: **not triggered** — the diff is one markdown file,
  no playbook. qa-all's `ansible-syntax` stage covered all 81 repo-wide regardless (78 under
  `playbooks/imports/`, 3 elsewhere).
- **`qa-helper-tests.bash` / `check_extension_compat` / ESLint**: **not triggered** — no
  `helpers/`, `tests/helpers/`, extension `metadata.json` or extension JS in the diff.
  qa-all ran helper-tests (1230 tests) and extension-compat anyway; both green.

Scratch artefacts from the gate runs are at `/workspace/untracked/scratch/qa00063/`
(gitignored) — delete when done.
