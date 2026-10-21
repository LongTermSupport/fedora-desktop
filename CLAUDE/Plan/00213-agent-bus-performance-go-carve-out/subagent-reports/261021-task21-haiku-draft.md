# Task 2.1 first draft: `pingbus status-line` in Go (Haiku draft)

Branch `agent-ab2645ff6857639ee-d8b563ca`. The draft is a WIP commit: qa-changed has not run and
golangci-lint still reports about 13 findings in `src-go/internal/pingbus/statusline/`. The full
handoff is in the worktree's `untracked/scratch/`, which is not tracked.

## Done

- **F2, golden test.** `src-go/internal/pingbus/statusline/golden/golden_test.go` judges, per
  step, stdout and exit byte for byte, and stderr byte for byte except the role line, which is
  compared by shape (S24, S37). `-impl=python` judges the zipapp, `-impl=<binary>` any command,
  and `-update` re-records from Python. `capture.py` is deleted; S40, PLAN Task 2.2 and
  `src-go/CLAUDE.md` now name the test.
- **Go implementation** in `src-go/internal/pingbus/statusline/`: the render (inner line, bus
  segment, wrapping, down marks, membership, session record, settings search, role read in
  part). `cmd/pingbus-status-line/` is a thin main that relays signals as the render's cancel.
- **Process seam.** The inner command runs through `execx` with a new `Cmd.NewSession` field:
  it starts in its own session and a cancel kills the whole group with SIGKILL. Two tests in
  `execx_test.go` cover the group and the kill.
- **`.golangci.yml`:** a tagliatelle exemption for the golden test, the same pattern the repo
  uses for other snake_case wire formats.

## Golden result

| Implementation                 | Passing    | Failing |
| ------------------------------ | ---------- | ------- |
| Python zipapp (`-impl=python`) | 104 of 104 | none    |
| Stand-in `/bin/echo hi`        | 0          | all 104 |
| Go `statusline.Run` (default)  | 101        | 3       |
| Go binary via `-impl`          | 101        | 3       |

The three failures are role-validation gaps: `role-team-record-invalid`,
`role-team-record-other-team` and `role-member-bad-base-url`. `-update` reproduces the committed
golden with no diff.

## Not done

1. **Bus-segment TTL cache (S32 to S36).** Not implemented. The cache scenarios pass only
   because nothing is cached, so they prove nothing about invalidation.
2. **Role validation (S21, S22).** `parseMember` does not check base_url, plain_http_hosts,
   the user_id handle, admin, room or limits. The team-record field checks are absent, and so
   are the host member and Element paths.
3. **Lint.** About 13 findings remain, listed in the handoff. None is a design issue.
4. **qa-changed** has not been run.

## Doubts (non-boilerplate parts)

- **Process group, timeout and kill.** `execx.Run` with `NewSession` kills the group on cancel,
  and the 10 s timeout is a `context.WithTimeoutCause`. Unverified: a grandchild that escapes the
  group but holds the stdout pipe. A non-ExitError from execx (`ErrWaitDelay`) currently bubbles
  up as a render error, where Python would keep waiting on the pipe. That needs a decision.
- **Signals.** `main` relays SIGTERM, SIGINT and SIGHUP as `context.Cause`. The golden covers a
  signal sent after the inner starts. A signal that lands before the inner starts is covered
  only by the reasoning that execx returns the cancelled context before `Start`; no test pins it.
- **On-disk formats.** The settings search, session record (ownership, mode, ASCII, distinct
  names), membership files (`v == 1` accepts `1.0` and `true`, as Python does) and down marks
  follow the Python. The role reader's token check and `member.json` duplicate-key walk follow
  it too. Not ported: the team-record field checks, and UTF-16/32 detection for `team.json`.
- **JSON.** `encoding/json` agrees with Python on duplicate keys (last wins) and on invalid
  UTF-8 (the payload is refused first). It differs on NaN, Infinity and lone surrogates (S23).
- **Width.** S27's measure uses a range table, not `unicodedata`. It is exact for the segment's
  own text (ASCII, team names, the bus label) and approximate elsewhere.
- **Stderr shape.** The shape rule is in `golden_test.go` `stderrMatches`. It takes the team names
  from each item before the first ": ". A message that contains "; " would be parsed wrongly on
  both sides alike.
- **Attribution.** This commit carries the attribution the session's reminder gives (Haiku 5.5),
  not the Opus line in the brief, since the reminder is the one about this session's commits.
