# Plan 00183: the prompt shows up-to-date when the tracking ref is stale

**Status**: In Progress
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Low

Addresses #52.

## Overview

In a checkout not fetched for two weeks, the prompt showed the clean, up-to-date marker
(`[main|✔]`). A `git pull` a second later fast-forwarded through two weeks of commits (#52).
The prompt compares `HEAD` with the local tracking ref, which moves only on a fetch, so it
reports "not behind" truthfully against stale information.

The prompt is `bash-git-prompt`, cloned and sourced by `play-git-configure-and-tools.yml`.
Upstream it has a periodic remote fetch (`GIT_PROMPT_FETCH_REMOTE_STATUS`, with a timeout
between fetches), which the play does not set. So the first question is why that fetch did
not keep the ref fresh: it may be off, or it may be failing silently. The remote is reached
over SSH with a passphrase-protected key held in the agent, and an SSH session may have no
agent at all. The cause is unknown, so this plan triages first.

## Goals

- The ahead/behind counts in the prompt are fresh, or visibly marked stale.
- A fetch never blocks the prompt and never asks for a passphrase.
- The behaviour is set by the play, not by hand.

## Non-Goals

- Replacing `bash-git-prompt`.
- Fetching repositories nobody has `cd`'d into.

## Tasks

### Phase 1: Triage

- [x] ✅ **Task 1.1**: `triage.bash` (probes in `triage-probe.bash`): the installed
  `bash-git-prompt` commit and its fetch code, every `GIT_PROMPT_` setting the start-up files
  give it, the fetch timer's tools, the deployed `git-fetch-age`, every `FETCH_HEAD` of each
  checkout's repository with its size and age, the tracking refs and their reflog, the SSH
  agent and its keys, and whether the remote answers (`git ls-remote`, no terminal, no
  askpass) with the agent and with none. Read-only, never fetches. Written and lint-clean;
  host run passed. The linked-worktree lead is settled
  without it (Decision 2): current upstream reads the common dir's `FETCH_HEAD`, so in a
  linked worktree it fetches at every prompt rather than never.

### Phase 2: Decide and build

- [x] ✅ **Task 2.1**: Decided without the triage: a staleness mark, no new fetch (Decision 1).
- [x] ✅ **Task 2.2**: The mark is `git-fetch-age` (`src-go/cmd/git-fetch-age`,
  `src-go/internal/fetchage`, A1-A13 in its package doc, tests first), called from the PS1 in
  `files/var/local/ps1-prompt` and built and installed root-owned to `/usr/local/bin` by
  `play-basic-configs.yml`, which now imports `play-golang.yml`. It reads files only, so a
  missing agent can neither block nor prompt.
- [ ] 🔄 **Task 2.3**: `play-basic-configs.yml` ran on the host at F44 c4cab141 (ok=68,
  changed=3, failed=0; `git-fetch-age` rebuilt, Go 1.26.10 on the host); docs
  (`docs/configuration.md`, `docs/playbooks.md`, `src-go/CLAUDE.md`) done. Acceptance passed
  on the host, and `qa-all.bash` passed on c4cab141. The `qa-reviewer` ran (round 1,
  FIX-FIRST, `subagent-reports/261022-qa-reviewer-round1.md`); its findings are fixed: the
  play pins bash-git-prompt to a commit and asserts the `async_run` override still holds.
  Left: `qa-all.bash` on the merged fixes; the host run of `play-git-configure-and-tools.yml`
  and this plan's acceptance through meta-deploy (the pin and the assert have not run on a
  host yet); then the owner's hand check, the yellow mark in a stale checkout's prompt. The hand
  check should expect
  `[fetch failed or running]` to show briefly on a prompt drawn during each five-minute
  background fetch; it lasting past the next prompt means the fetch failed
  (`~/.cache/git-prompt-fetch.log` has why).
- [x] ✅ **Task 2.4**: the root fetch bug (Decision 2): a failed fetch empties `FETCH_HEAD`
  and dates it now, so the mark read the prompt's own failing fetch as fresh. Done in code,
  test first (A14, A15 in `internal/fetchage`): an empty `FETCH_HEAD` is
  ` [fetch failed or running]`, and the last fetch is the newest one with a record in it.
  `acceptance.bash` (checks in `check-fetch-age.bash`) proves the installed binary on the
  host, with a real failing fetch and the deployed PS1. Deployed on the host; its acceptance
  legs install, marks (incl. empty FETCH_HEAD), failed (a real failed fetch) passed. The
  acceptance re-run passed on the host, all six legs (the `prompt` leg's checker bug, a
  prefix assignment to the readonly `PROMPT_FILE`, was fixed).
- [x] ✅ **Task 2.5**: the prompt's background fetch can ask for nothing (Goal 2, Decision 2).
  Done in code, test first: `git-prompt-fetch` (`src-go/internal/promptfetch`, F1-F6) runs
  `git fetch` with ssh in BatchMode, added to the ssh command git would use
  (`GIT_SSH_COMMAND`, else `core.sshCommand`, else `GIT_SSH`, else `ssh`), with
  `GIT_TERMINAL_PROMPT=0`, no askpass and stdin from `/dev/null`.
  `play-git-configure-and-tools.yml` builds it root-owned to `/usr/local/bin` and defines, after
  sourcing `gitprompt.sh`, the `async_run` bash-git-prompt runs its fetch through, so the fetch
  is `git-prompt-fetch` with its words in `~/.cache/git-prompt-fetch.log`. Seen in the lab: the
  ssh stand-in was handed `-o BatchMode=yes`. The acceptance's `fetcher` and `override` checks
  prove it on the host. Deployed on the host (`play-git-configure-and-tools.yml` passed); the
  `fetcher` (BatchMode, no terminal) and `override` (async_run through git-prompt-fetch)
  legs passed. The acceptance re-run passed, all six legs.

## Technical Decisions

### Decision 1: a staleness mark, not a background fetch

**Context**: Task 2.1 was the owner's pick after the triage. The work was handed over with
the rule that, while the plan leaves the choice open, the option taken must never make the
prompt slow or hang it on the network, and no fetch may ask for a passphrase.

**Options considered**: (A) a throttled, background, non-blocking fetch from the prompt:
fresh counts, but every prompt in a checkout risks a network process, an SSH key not in the
agent needs `BatchMode`/`GIT_TERMINAL_PROMPT=0` to stay silent, and a failing fetch is then
silent too, which is the very defect of #52 (a stale ref shown as current). (B) a visible
mark from the age of the last fetch: no network, no process, no credentials; the counts stay
stale until a fetch, but they are no longer read as current. (C) both.

**Decision**: B. It is the only option that cannot slow, hang or prompt, and it also exposes
a failing upstream fetch rather than hiding it. Details:

- Measured as the newest `FETCH_HEAD` of the repository: git keeps `FETCH_HEAD` per
  worktree while the tracking refs are shared, so a fetch in any worktree counts.
- Threshold one hour (bash-git-prompt's own fetch, when it works, runs every five minutes).
- A repository with no remote has no mark; a remote with no fetch recorded (a fresh clone)
  is marked `[no fetch recorded]`, the truth rather than a guess from another file's age.
- Go, not bash: `files/var/local/ps1-prompt` is already over the bash limits on the ratchet
  allowlist and may not grow, so the logic is a Go command and the prompt only gains a
  `$(...)` in its PS1 string. It starts in about 10 ms in the CCY container.

**Date**: 2026-10-19

### Decision 2: an empty FETCH_HEAD is a failed or running fetch, never a fetch

**Context**: reproduced in the container with bash-git-prompt at upstream HEAD and at the
commit before its April 2026 rework, a local remote, and an ssh stand-in that asks for a
passphrase on `/dev/tty` as ssh does with no key in the agent. With a remote that needs no
key, the prompt's fetch works (in the checkout and in a linked worktree). With the
passphrase, the background fetch writes `Enter passphrase for key:` into the terminal, cannot
read the answer (it is not the terminal's foreground process group), fails, and
`&> /dev/null` hides the failure: `origin/main` stays stale and the prompt shows `✔`. And git
empties `FETCH_HEAD` before it contacts the remote (`builtin/fetch.c` `do_fetch` truncates it
first; seen with git 2.39 and in git master), so the failure leaves an empty, newly dated
`FETCH_HEAD`. bash-git-prompt's five-minute timer and `git-fetch-age` both read that as a
fresh fetch, so the mark never showed in exactly #52's case.

**Decision**: `git-fetch-age` reads size as well as date. An empty `FETCH_HEAD` is shown as
` [fetch failed or running]`, however young; the last fetch is the newest `FETCH_HEAD` with
a record in it. A prompt drawn while a working fetch runs is marked too, which is true: its
counts are not yet fresh. So is a successful fetch that matches no ref (a remote with no
branches yet), which also leaves `FETCH_HEAD` empty; the file cannot tell it from a failure,
and it is rare enough to accept (A14 says so).

Upstream's fetch asking for a passphrase breaks Goal 2, so it is changed too, without
replacing the fetch (Decision 1 still holds: no fetch of our own, upstream's timer decides
when). bash-git-prompt runs that fetch, and nothing else, through its `async_run`; the play
defines `async_run` after sourcing it to run `git-prompt-fetch`, which puts ssh in BatchMode
on top of whatever ssh command git would use, so a per-account `core.sshCommand` key still
works. A fetch that would need a passphrase now fails without a word on the terminal, and the
mark shows it. A newer upstream could use `async_run` for more, so the play checks
bash-git-prompt out at a pinned commit (`bash_git_prompt_version`, the one this was written
against) and asserts, before the override is written, that the installed `gitprompt.sh` calls
`async_run` only for the fetch; the acceptance checks it too. The triage still records
whether the host's agent holds the key.

**Date**: 2026-10-21

## Success Criteria

- [ ] A checkout whose tracking refs were not fetched recently is visibly marked stale.
- [ ] With no agent, the prompt draws at once and asks for nothing.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00183-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #52.
- Mark merged: 92ec019d; deployed at c3391ba9.
- Failed-fetch mark and `git-prompt-fetch` (Tasks 2.4, 2.5) merged: e40671403.
- Deployed at c4cab141 (`play-basic-configs.yml`, ok=68 changed=3 failed=0); `qa-all.bash`
  passed on c4cab141.
- qa-reviewer round 1: FIX-FIRST; answered on `fix/00183-review-findings`.
