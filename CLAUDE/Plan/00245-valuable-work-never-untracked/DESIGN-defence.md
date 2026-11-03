# Plan 00245 design: the defence against valuable work kept where git does not carry it

Status: design proposal for the coordinator. Not an implementation record. The sweep
ranking in section 6 waits on `RESEARCH-untracked-triage.md` from the sibling triage (see
6.1).

## 1. The class and its hazard

**Class: unrecorded asset.** A work product someone would want later (research, a
decision or a pending question to the owner, a review, a design, a diagnostic diff,
finished commits, a rescued copy) is kept only where git does not carry it to the
remote. The places are:

| Place                                 | Example from this week's incidents                                                     |
| ------------------------------------- | -------------------------------------------------------------------------------------- |
| A gitignored path in the repo         | research in `untracked/scratch/`, a decision sheet in `untracked/`                     |
| A path outside the repo               | `/tmp` (already denied by `project_containment`)                                       |
| A patch file standing in for a commit | a worker's diagnostic change saved as a `.patch` before the worktree was force-removed |
| A local-only branch or commit         | 11 finished commits on branches no remote held                                         |
| A dirty worktree                      | uncommitted, reviewed work in an abandoned worktree                                    |
| A stash                               | (`git stash` is already denied)                                                        |
| A daemon-owned ignored store          | `untracked/agent-reports/auto/`, `untracked/issue-reports/`                            |

**Hazard.** The loss is silent and permanent. A reaper, a pruner (`subagent_report_persistence`
prunes at 500 files / 30 days), `git worktree remove --force`, a discarded container or a
lost disk removes it with no error, and before that nobody can find it: nothing tracked
points at it, no review sees it, GitHub never has it. The owner's rule:
untracked scratch is for true scratch (a message body about to be consumed), never a log of
work or a store of assets.

**Why it keeps recurring.** The tooling steers agents into the class.

- `project_containment` denies `/tmp` and tells the agent to write to `untracked/scratch/`
  instead, with no word about what is allowed there.
- `markdown_organization` lists `untracked/` as an allowed markdown location ("ad-hoc
  temporary docs"), so a report written there passes every location check.
- `dispatch_declaration`'s non-plan fallback is the gitignored `untracked/agent-reports/`,
  and this repo's `CLAUDE/AgentNotes.md` repeats it as the instruction.
- `CLAUDE/AgentNotes.md` tells a session to keep work in progress "under `untracked/` (a
  patch, or a scratch copy)" while a host run is in progress, which is incident 2's shape.
- The docs-optimise skill writes its proposal as `proposal.patch` in scratch.
- Every guard checks whether a write is *safe*; none checks whether it is *kept*. An
  ignored path is the one place no content check, no review and no push gate looks.

The evidence that steering matters: `untracked/scratch/` holds over 3,500 files, about
1,100 of them over 4 KiB, 440 of them markdown; plus 96 registered worktrees.

## 2. What tells scratch from an asset

| Signal                                                                                                                                                                                   | Robust?                      | Gameable?                                                               | False positives                                                 |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------- | ----------------------------------------------------------------------- | --------------------------------------------------------------- |
| **Consumption**: the file was the input of a consuming command (`git commit -F`, `gh … --body-file`, `mkplan.bash --journal`, `git apply`, `jq`/`grep` of a capture) in the same session | High                         | Only by faking a consumer, which is a deliberate act a reviewer can see | Low; a capture read by `Read` must count as consumed            |
| **Path shape**: a name on a small allowlist (`msg-*`, `body-*`, `commit-*`, `journal-*`, `fields-*.json`, `*.log`, `*.out`, `*.txt` captures)                                            | Medium                       | Rename to a scratch shape                                               | Low if the list is short and documented                         |
| **Lifetime**: scratch older than N hours                                                                                                                                                 | High for *finding* survivors | No                                                                      | Long-running captures (plan-run logs); exempt registered stores |
| **Content**: headings, length, "report", "review", "decision", "findings", "owner"                                                                                                       | Low                          | Trivially                                                               | High: journal bodies and PR bodies have headings and are long   |
| **Extension**: `.md`, `.diff`, `.patch`, `.go`, `.py`, `.bash` in scratch                                                                                                                | Medium                       | Rename                                                                  | Medium: a body file is `.md`                                    |
| **Git state**: commits on no remote, dirty worktree, stash                                                                                                                               | High                         | No                                                                      | Low; a worktree in active use is the one case, handled by age   |

Conclusion: **consumption plus lifetime is the robust core**; path shape is the cheap
write-time filter; content and extension signals are advisory text inside a deny, never the
deciding fact. A defence keyed on content alone would be read past within a week.

## 3. Where each kind of asset goes instead

| Asset                                         | Tracked home                                                                               |
| --------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Plan research, design, triage                 | `<plan>/RESEARCH-*.md`, `<plan>/DESIGN-*.md`                                               |
| Sub-agent report for plan work                | `<plan>/subagent-reports/{yymmdd}-{agent}-{model}.md`                                      |
| Progress, findings in flight                  | `mkplan.bash --journal` (the body file is scratch, consumed at once)                       |
| Research with no plan yet                     | a new plan via `mkplan.bash` (committing a plan alone is encouraged)                       |
| A question or decision pending with the owner | a GitHub issue (anonymised), or a plan's decision section; never a sheet in `untracked/`   |
| A non-plan sub-agent report                   | a GitHub issue or PR comment, or a plan created for it                                     |
| A diagnostic change / work in progress        | a commit on a pushed branch (WIP commits are fine); never a patch file                     |
| A rescued branch or worktree                  | push the branch to the remote; never a copy in `untracked/`                                |
| Daemon upstream issue body                    | generated into scratch, consumed by `gh issue create --body-file`; the issue is the record |

**Install-specific material** (real names, hosts, captures) cannot be tracked in this public
repository. Two routes, and the choice is the owner's:

1. **Anonymise, then track** (the existing rule in CLAUDE.md "Public Repository"). Covers
   nearly all research and decisions; the raw capture stays scratch and is allowed to die.
2. **A private store that is pushed**: a private repository registered by the project as
   the home for raw captures that must survive, cloned under an ignored path the way
   `untracked/repos/` holds private clones today. It is tracked and backed up, just not here.
   Recommended only if route 1 proves insufficient; it needs an owner decision and a
   private remote.

## 4. The defence layers

| #   | Layer                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Catches                                                                                       | Misses                                                     | False-positive risk                                                                     | Lives in                                                                                                   |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | ---------------------------------------------------------- | --------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| L1  | **Docs and steering fixes**: the asset-home table above in a topic file; correct `AgentNotes.md` (WIP under `untracked/`, non-plan reports to `untracked/agent-reports/`); docs-optimise writes its proposal as a branch, not a patch                                                                                                                                                                                                                                       | The instruction that produces incidents 2 and 3                                               | Anything an agent does against the docs                    | None                                                                                    | this repo                                                                                                  |
| L2  | **`untracked/` audit (Go, `src-go/qa`)**: walks `untracked/` excluding registered stores (`repos/`, `worktrees/`, `plan-runs/`, `clones/`, daemon `auto/`), classifies each file by shape, age and extension, and fails on any file outside the scratch allowlist older than 24 hours, or any `.patch`/`.diff`/`.md` older than 24 hours                                                                                                                                    | The existing pile; survivors of every route, including Bash-authored files no write hook sees | Assets younger than 24 hours; assets in a registered store | Low with the age floor; a concurrent session's fresh scratch is under it                | this repo: blocking in `qa-all.bash` (local; CI's clean checkout has no `untracked/`, so CI cannot run it) |
| L3  | **Unpushed-work audit (Go)**: local branches with commits on no remote, worktrees with uncommitted changes or unpushed commits older than 24 hours, any stash                                                                                                                                                                                                                                                                                                               | Incident 3 (10 days of finished work in local branches and dirty worktrees)                   | Work younger than the floor                                | Low; an active worktree under 24 hours is quiet                                         | this repo: blocking in `qa-all.bash`, which already gates every push                                       |
| L4  | **Write-time path-shape guard (project handler, PreToolUse Write/Edit, and Bash-authored writes via the daemon's written-path accessor)**: a new file under `untracked/` whose name is not a scratch shape, or is not under a registered store, is denied; the deny names the tracked home for its kind (content signals choose the suggestion, not the verdict)                                                                                                            | Incidents 1, 2, 4 at the moment of writing                                                    | A determined rename to a scratch shape                     | Low if the allowlist is short; a new legitimate shape is added to config, owner-visible | this repo now as `.claude/hooks/handlers/pre_tool_use/`, then upstream                                     |
| L5  | **Consumption ledger and Stop/SubagentStop gate (daemon)**: PostToolUse records every file a session creates under the scratch directory and every file a later command consumes; a Stop or SubagentStop with created-but-unconsumed scratch files is denied, listing each with "consume it, promote it to a tracked home, or delete it"                                                                                                                                    | The class in general, including a renamed asset: being *kept* is what is blocked, not a name  | A file "consumed" by a fake consumer                       | Low; captures read back with `Read`/`grep` count as consumed                            | upstream                                                                                                   |
| L6  | **`dispatch_declaration` tightened**: `strict: true` here now; upstream, a declared destination under an ignored path is not a declaration (only a plan folder, a GitHub issue/PR, or a configured tracked report directory counts)                                                                                                                                                                                                                                         | A brief that sends a sub-agent's work to `untracked/agent-reports/`                           | An agent that ignores its brief (L4/L5 catch it)           | Low                                                                                     | strict flag here; the rest upstream                                                                        |
| L7  | **Daemon-owned ignored stores**: `subagent_report_persistence` stays as a crash net, but on a dispatch that declared a plan folder it also checks at SubagentStop that the declared tracked file exists (extend `subagent_report_path_verifier`), and the SessionStart sweep reports auto-saved replies with no tracked counterpart before the pruner deletes them; `issue-report` names its output a scratch body file and the guidance says the filed issue is the record | Incident 5: the daemon's own stores becoming the only copy                                    | —                                                          | Low                                                                                     | upstream                                                                                                   |
| L8  | **Destructive-removal guards**: deny `git worktree remove --force` (and `rm -rf` of a registered worktree) while it has uncommitted changes or commits on no remote; the existing `git branch -D` guard is the model                                                                                                                                                                                                                                                        | Incident 2's second half                                                                      | —                                                          | Low                                                                                     | upstream (`destructive_git`)                                                                               |
| L9  | **SessionStart sweep (daemon)**: one report of scratch survivors, unconsumed files from earlier sessions, unpushed branches and dirty worktrees, with the same disposition instruction                                                                                                                                                                                                                                                                                      | Drift between pushes                                                                          | —                                                          | It is a report, so read past if alone; it is the reminder, L2/L3 are the gate           | upstream                                                                                                   |

Already in place and kept: `project_containment` (outside the root), `git stash` denied,
`git branch -D` of an unpushed branch denied, the untracked Claude memory ban (the same class,
already decided once for one store).

## 5. Recommendation

**The minimum that makes the class impossible** is two gates that do not depend on reading
intent: **L5** (scratch you created must be consumed, promoted or deleted before you stop)
and **L2 + L3** (nothing asset-shaped survives 24 hours in `untracked/`, and no work lives
only in a local branch, worktree or stash, or the push gate fails). Everything else lowers
the rate at which those gates fire. Content heuristics stay as suggestion text inside a deny.

### Build here now (Defence Before Fix order)

1. **Detector**: a Go audit, `src-go/qa/untracked` (one package, two checks: the `untracked/`
   file audit and the unpushed-work audit), with table tests built on a fake tree and fake
   git. Prove it red on the incidents' shapes recreated as fixtures (a `.md` research file
   36 hours old in scratch, a `.patch`, an `untracked/owner-decisions-*.md`, a branch with
   commits on no remote, a dirty worktree), green on an allowlisted `msg-*.txt` and on a
   fresh file.
2. **Sweep**: run it on this checkout; disposition every finding (section 6).
3. **Blocking**: wire it into `scripts/qa-all.bash` (and `qa-changed.bash` for the
   unpushed-work half). State in `CLAUDE/QA.md` that it is local-only and why.
4. **Write-time guard (L4)** as a project handler in `.claude/hooks/handlers/pre_tool_use/`,
   test-first, configured with the scratch-shape allowlist and the registered stores.
5. **Config**: `dispatch_declaration.options.strict: true` in `.claude/hooks-daemon.yaml`;
   restart the daemon.
6. **Docs (L1)**: the asset-home table as the one source (a short section in
   `CLAUDE/AgentNotes.md` or a new topic file linked from CLAUDE.md, the owner's choice);
   correct the two `AgentNotes.md` passages and the docs-optimise skill; a one-line rule in
   CLAUDE.md under Critical Rules pointing at it.
7. **Only then** the original instances are fixed by the sweep's dispositions.

### Request upstream (hooks daemon), written project-neutral

> **Defect class: work kept only in ignored or unpushed places is lost silently.**
> The daemon steers agents into ignored paths (`project_containment` redirects to the
> scratch directory; `markdown_organization` allows markdown under it;
> `dispatch_declaration`'s non-plan fallback is an ignored directory) and keeps its own
> outputs there (`subagent_report_persistence`, `issue-report`), but no handler checks
> whether something written there was meant to be kept. Requested:
>
> 1. A consumption ledger and a Stop/SubagentStop gate: files a session creates under the
>    scratch directory must be consumed by a later command in the session, moved to a
>    tracked path, or deleted before the session stops; the deny lists each file.
> 2. A configurable scratch-shape allowlist and registered-store list, with a PreToolUse
>    deny (Write/Edit and Bash-authored writes) for any other new file under the ignored
>    root, naming the tracked home for its kind.
> 3. `dispatch_declaration`: a destination under an ignored path no longer counts as a
>    declaration; the fallback becomes a configured *tracked* report directory or a GitHub
>    issue. `project_containment` and `markdown_organization` guidance say the scratch
>    directory is for consumed, short-lived files only.
> 4. `subagent_report_path_verifier` checks the declared tracked report exists at
>    SubagentStop; the SessionStart sweep reports auto-saved replies with no tracked
>    counterpart before retention prunes them.
> 5. `destructive_git`: deny `git worktree remove --force` while the worktree has
>    uncommitted changes or commits no remote holds.
> 6. A SessionStart sweep of scratch survivors, unpushed branches and dirty worktrees.

## 6. Sweep plan for the existing pile

### 6.1 Inputs

The sibling triage, `RESEARCH-untracked-triage.md` in this folder, ranks the creation paths
and classifies what is in `untracked/`. It had not been written when this design was; its
creation-path ranking decides the order of L4's allowlist and L1's doc fixes, and should be
folded into section 1's "Why it keeps recurring" list when it lands.

### 6.2 Procedure

1. **Freeze the pile**: no reaper or pruner runs on `untracked/` until the sweep is done
   (the daemon's auto-report pruner included: raise its age cap for the duration, or copy
   the directory listing first).
2. **Git state first** (highest value, the incident-3 shape): list local branches with
   commits on no remote, worktrees with changes or unpushed commits, and stashes. Each is
   pushed as-is to a branch on the remote, then merged, closed, or recorded in a plan;
   nothing is copied into `untracked/` (`untracked/wip-rescue/` is itself an instance and
   is resolved by pushing what it rescued).
3. **Named assets at the top of `untracked/`** (decision sheets, hand-off notes, research
   and upgrade feedback): each is either anonymised into the plan or issue it belongs to,
   filed as a GitHub issue, or deleted as superseded. Decision sheets become owner-facing
   issues or plan decision sections.
4. **`untracked/scratch/`**: delete by shape (`qa-*.log`, `msg-*`, `commit-*`, `journal-*`
   bodies already consumed); review by hand only what survives the audit's asset filter
   (`.md` over 4 KiB, `.patch`/`.diff`, source files). Each survivor is promoted or deleted.
5. **Daemon stores**: for each auto-saved report from a plan dispatch, confirm the plan's
   `subagent-reports/` holds it; promote the missing ones that matter.
6. **Record**: one journal entry per sweep step via `mkplan.bash --journal`, counts and
   dispositions only, no install-specific names.
7. **Gate on**: when the audit reports zero findings, make it blocking (step 3 of section 5).

Nothing in this plan's sweep reads or quotes `untracked/repos/` contents; those are
registered private clones and are excluded from the audit.
