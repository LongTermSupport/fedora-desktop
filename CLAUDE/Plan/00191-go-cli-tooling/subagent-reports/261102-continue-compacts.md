# Compact then exit on close (Plan 00191 Task 4.12)

Branch `feat/ccy-continue-compact`, from F44 at b83158ca8. Not plan work beyond Task 4.12.

## How the request changed

1. Owner, 2026-11-02: `ccy --continue` should start with `/compact`, `--no-compact` to opt out.
2. Owner: inject it with tmux send-keys once the session is up, never as the initial prompt.
3. Owner: a compaction of a cold conversation reads the whole context uncached; the cheap moment
   is at close, while the cache is warm. Measure first.
4. Owner: abandon the startup compaction entirely (it pays the same uncached read the first turn
   would). Build only compact then exit on close, the default for `ccy-teams shutdown` and a ccy
   stop command, `--no-compact` the close path's escape hatch.
5. Owner: also a close action on a `ccy-sessions` row; one neutral package all three call
   (`internal/sessclose`, beside `tmuxccy` and `sessprobe`), with no tmux calls of its own.

No startup-compaction code was written, so nothing of it had to be removed.

## Measurement

`untracked/scratch/compact-measure/measure.py` (not tracked) over the 38 main-thread
transcripts under this container's `~/.claude/projects`, read-only, numbers only. A turn's usage
is counted once per message id; sub-agent transcripts and sidechain entries are left out.

| turn                                  | n      | input (median) | cache_creation (median / max) | cache_read (median / max) |
| ------------------------------------- | ------ | -------------- | ----------------------------- | ------------------------- |
| cold: more than 1 h after the last    | 19     | 2              | 78,307 / 374,791              | 13,036 / 282,815          |
| warm: less than 60 s after the last   | 16,036 | 2              | 851 / 299,725                 | 245,122 / 526,539         |

- A cold turn reads only the system prompt (~12,000-17,000 tokens) from the cache and writes the
  whole conversation to it again: 15 of the 19 cold turns wrote 36,000-375,000 tokens. Two
  wrote under 500 and read 97,000 and 283,000: their gap was just over an hour (1.0 h), still
  inside a one-hour cache lifetime. Two recorded no usage at all (zero counts).
- A warm turn reads the conversation from the cache and writes only what is new.
- 71 compact_boundary entries: 70 manual, 1 auto. preTokens mostly 395,000-530,000 (lowest
  45,145), postTokens 6,679-22,027, durationMs 40,900-254,421 (most 50,000-90,000).
- A compaction's own API usage is not recorded. The compact_boundary entry carries only
  compactMetadata (trigger, preTokens, postTokens, durationMs, cumulativeDroppedTokens,
  preservedSegment, preservedMessages, preCompactDiscoveredTools) and no usage field, and no
  assistant entry with usage is written for the summarising request. Its size can only be
  inferred: it sends preTokens of context, so warm it is a cache read of ~400,000-530,000 tokens,
  cold an uncached write of the same.

What this says for the design: compacting on resume costs one cold write of the whole context,
the cost the first turn pays anyway; compacting at close costs a cache read of it (a tenth of the
input price for a read, against 1.25x or 2x for a write), and the next resume then writes only
~10,000-20,000 tokens instead of ~400,000.

## Mechanism (owner-decided)

tmux send-keys into the session's own pane, as `ccy-teams shutdown` already types `/exit`,
`ccy-sessions set-going` types `/compact <instruction>` after a reboot and `ccy-teams wind-down`
types its `/compact <instruction>` (WC2). Alternatives rejected by the owner: `/compact` as
claude's initial prompt argument, the in-container supervisor, a SessionStart hook.

Proven here: the readiness signal (`screen.ClaudeState` Ready on a framed empty box) and the
completion signal (`transcript.CompactedSince`, a main-thread compact_boundary after the typed
time) are the ones set-going and wind-down already run on the host; `/compact <argument>` with
the space closing the completion menu is set-going's and wind-down's form. Not proven here: a
real Claude Code session taking it at close (no real session was started against the API). The
host check: `ccy-teams shutdown <team>` on a team with a live session, then the session's
transcript ends in a compact_boundary followed only by the `/exit` entries.

## Built (test first)

- `transcript.CompactedLast(path)` (`src-go/internal/transcript`): true when the conversation's
  last main-thread entry recording a context size is a compact_boundary, or none records one (no
  turn taken). Such a session has nothing to compact at close, so the close goes straight to
  `/exit` instead of typing a `/compact` that Claude Code would refuse (no boundary would follow,
  and the close would fail for nothing). Red: undefined; green: `TestCompactedLast`.
- `conversation.Transcript(rec, claudeHome)` (`src-go/internal/conversation`): the session's own
  transcript (its SessionStart record's id, never the newest in a shared folder). It was
  `winddown.transcriptOf`'s body; winddown now calls it, so the lookup exists once.

## Design for what is left

`internal/sessclose` (package doc C1-Cn as the spec), built on `tmuxccy` and `sessprobe`, no tmux
argv of its own:

- `Closer{Tmux *tmuxccy.Tmux, Registry func() (*registry.Store, error), Transcripts (CompactedLast,
  CompactedSince), ClaudeHome string, Now, Sleep, Waits}`; `Waits{Submit, Poll, Prompt, Compact,
  Exit}` with Compact bounded at 10 min (the longest measured compaction took 254 s).
- `Close(ctx, []Target{Name, NoCompact}) []Result{Outcome (exited, stopped, failed), Note,
  Compacted}`: the per-session loop.
- Moves from `internal/teams/shutdown/stop.go`, which holds the whole Escape-then-type and
  wait-for-exit logic (SD10-SD13): `job`, `stopAll`, `liveSessions`, `step`, `act`, `typeExit`,
  `answerBackgroundWork`, `timedOut`, `ended`, `closeWindow`, `hardStop`. `checkSeats`,
  `judgeSeats` and `heldSeats` stay in shutdown (seats are a team matter). shutdown's job keeps
  its seats and wraps a sessclose result.
- The compact phase, before `/exit`, when the target is not NoCompact: the transcript is found
  with `conversation.Transcript`; `CompactedLast` true goes straight to `/exit`; at a Ready box
  `/compact <close instruction>` is typed (text, Submit pause, Enter), the time kept; `Typed`
  after it is one more Enter, as shutdown's re-entry; the end is `CompactedSince(file, typedAt)`,
  or, where the session has no recorded conversation (a launch without the bus), the pane busy
  then ready again, the source named in the note. Not seen within Waits.Compact: failed, "its
  /compact did not complete within 600 s, so /exit was not typed and it is still running: attach
  it (ccy-sessions)", never killed. A transcript that cannot be read: failed the same way, with
  `--no-compact` named as how to end it uncompacted.
- From `internal/teams/winddown/compact.go`: `idleAgain` (busy then ready, or ready for the settle
  bound) and the re-entry are the same pane logic; the package should export it and winddown
  call it, so the completion wait exists once. winddown's queue, floor and window stay its own.
- Callers: `ccy-teams shutdown <team> [--yes] [--no-compact]` (SD spec amended: compact then
  exit is the default); `ccy-teams stop <session>... [--yes] [--no-compact]` or a `ccy --stop`
  front flag that runs the same package (the ccy stop command; ccy has none today: `--top`
  kills containers, ccy-sessions' End kills the tmux session); `ccy-sessions`, a row action
  "Close (compact, then exit)" beside End (Ctrl-X, the hard kill, which stays). One test per
  caller shows it calls sessclose (a fake Closer seam).
- Still duplicated after that: shutdown's and winddown's `seconds()` helpers and their two
  `Waits` types; `set-going`'s own look/ready loop in `internal/sessions/setgoing.go`, which
  types `/compact` after a reboot with a floor and its own manifest states, is a third copy of
  the ready-then-type step and should move onto the package's readiness wait later.
- Then: `ccy --help`/help.txt only if a ccy flag is added (the bash launcher's help heredoc is
  on the bash-limits ratchet, so any added help line must be paid for by a removed one), the
  ccy-teams and ccy-sessions usage texts, docs/ccy.md, docs/ccy-changelog.md, the CCY version
  bump (F44 is at 3.104.16; ccy-teams and ccy-sessions are built by play-claude-yolo.yml), and a
  `changed-plays` entry for play-claude-yolo.yml in CLAUDE/Plan/meta-deploy.list.

## Process note

`Edit`/`Write` of `PLAN.md` and of this report were refused by the markdown location handler
(R-MARKDOWN-WRONG-LOCATION) in this doubly nested worktree path; both files were written from
`untracked/scratch/compact-measure/` by a copy instead. The commit-time plan and docs checks
still ran over them.
