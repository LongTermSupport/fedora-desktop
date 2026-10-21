# Review: feat/ignored-human-visible at dca54b96 (Plan 00194 Task 2.0f)

Reviewed against F44 50782762. Nothing changed. Builder's report:
subagent-reports/261019-ignored-human-visible-opus.md.

## Verdict: MERGE

Delivery is untouched (the only behavioural branch added is the `elif` after ACCEPT/DROP in
`syncer._receive`, which appends a log line and nothing else). The three calls are sound.
Findings below are non-blocking notes for the merge commit or a follow-up.

## The builder's three calls

1. **Log only messages naming no agent of the team** (`protocol.reached_no_agent`): right.
   The literal reading ("every not-addressed human message, per member") would make
   `ignored=` count ordinary traffic for other agents. The predicate as written makes the
   count mean "reached no one", which is what the owner needed from the incident.
2. **No rotation or size cap**: right, and recorded in §9 "Unheard". `dropped.log` has none
   (`inbox.log_drop` is a plain `O_APPEND`); introducing a cap for one log only would be the
   inconsistency. One short ASCII line per unheard message per member.
3. **Status line unchanged**: right. A lifetime count with no "seen" state would light up
   after the first typed `@room` and never go dark. `pingbus status` is the right place.

## Predicate, per addressing shape (read `_addressed`, `sender_class`, `Context.roles`)

| Shape                                                | 08h outcome                          | Logged? | Correct? |
| ---------------------------------------------------- | ------------------------------------ | ------- | -------- |
| `m.mentions` absent (typed `@room`, typed handle)    | IGNORE not-addressed, every member   | yes     | yes      |
| `m.mentions: {}` / `user_ids` not a list / malformed | IGNORE not-addressed                 | yes     | yes      |
| `room: false`                                        | IGNORE not-addressed                 | yes     | yes      |
| `user_ids` naming only humans                        | IGNORE not-addressed                 | yes     | yes      |
| naming an agent of another team (not in `roles`)     | IGNORE not-addressed                 | yes     | yes      |
| naming a departed member (removed from the record)   | IGNORE not-addressed                 | yes     | yes      |
| naming a parked member (keeps its role, spec §8)     | IGNORE not-addressed on live members | no      | see F2   |
| naming another live agent                            | IGNORE not-addressed                 | no      | yes      |
| `room: true`                                         | ACCEPT everywhere                    | no      | yes      |
| reply whose body is only the quote fallback          | IGNORE `empty` at the addressee      | no      | see F3   |
| ping / agent text for others, edits, non-dicts       | n/a                                  | no      | yes      |

`sender_class(...) != SENDER_HUMAN` excludes agents and strangers, so a non-member's text
never reaches the log (it is already a `sender` drop upstream, 04h). `event.get("sender")` in
the syncer is safe because `reached_no_agent` returns False for a non-dict first. The
`is_agent_user_id` conjunct is redundant with `user in ctx.roles` (the record validator
already guarantees role keys are agent IDs) but harmless.

## Double counting and idempotency

- The append runs inside `_receive`, before `commit_batch` saves `next_batch`. A crash between
  the two re-reads the same events on restart and logs them again. This is **at-least-once,
  not idempotent per event ID**, and it is exactly `dropped.log`'s guarantee (`log_drop` sits
  at the same point). `seen` (inbox + consumed IDs) is not consulted for ignored events, and
  an ignored event is never stored, so there is no cheap per-event-ID dedupe without a new
  state file. Acceptable as-is; see F1.
- A normal watcher restart does not double-log: it resumes from the saved token, and Matrix
  does not redeliver events before `since`. `_fill_gap` reads `since..prev_batch` and
  prepends, so a limited timeline is seen once.
- `history` (the other `validate_event` caller) and `validate` never call `log_ignored`; the
  only callers of the new functions are `syncer._receive` and `cli._status_team`.

## Concurrency and locks

`_receive` runs only from `sync_once`, which `recv`, `wait` and `watch` call while holding the
team's sync `lock` (`inbox.acquire_lock(state, kind)`), so one process per team appends.
The append is a single `write` of one short line on an `O_APPEND|O_NOFOLLOW` fd, so even a
concurrent append would not interleave. `status` reads `ignored_count` lock-free, as
`drop_count` already does; a torn read can only under-count by one line.

## Content, permissions, shape

- Never the body: the fields are team, event ID, sender, reason, ms; the event ID and sender
  are grammar-checked (`_LOG_USER_RE`, `is_event_id`) and written as `-` on failure, so an
  event cannot inject a field or a line. `test_syncer` asserts a `MARKER` body is absent.
- Same `_append_log` path as `dropped.log`: `FILE_MODE` 0600 (asserted), state dir 0700,
  symlink and non-regular-file refusal (asserted for both write and count).

## Docs

- Notation paragraph, §7, §9 08h + "Unheard", §12 layout, §13 `status` all agree with each
  other and with the code (one reason code, five fields, not a drop, append-only). The §13
  `TEAM` field order matches `_status_team`. `test_protocol_doc` has no table for the new
  constant, and none is needed.
- `docs/agent-bus.md` says "Each agent's **watcher** writes one line": `recv` and `wait` also
  write it (same `_receive`). Minor wording (F4).
- `docs/ccy-changelog.md` 3.99.12.17 / 2.77.13 entry is consistent with the launcher comment.

## Versions and deploy

- `CCY_VERSION` 3.99.12.17 + comment, `REQUIRED_CONTAINER_VERSION` 2.77.13, Dockerfile
  `LABEL` 2.77.13: all three moved together (placeholders, to be renumbered at merge).
- The container-version gate covers `helpers/pingbus`: `test-ccy-container-version-hook.bash`
  asserts a `helpers/pingbus` change and a deletion without a bump are rejected, and that the
  gate's path list is read from the real play's zipapp build.
- `meta-deploy.list`: lines 1-21 are byte-identical to F44 (diffed). Two entries appended
  after `play-cli-tools.yml`, agent-bus before claude-yolo, each with a description.

## Plan

PLAN.md Task 2.0f added (in progress), journal `action` entry present, report linked.

## Tests run (this worktree, detached at dca54b96)

- `PYTHONPATH=. env -u PINGBUS_HOME -u PINGBUS_TEAMS -u PINGBUS_MEMBERSHIP python3 -m unittest discover -s tests/helpers/pingbus`: 1597 tests, 1 failure:
  `test_relay_cli.WatchRelayTest.test_no_relay_confirmed_for_120_seconds_while_items_wait_is_stalled`.
  Rerun alone: OK in 0.95 s, at a host load average of ~30. The branch does not touch the
  relay; a load-sensitive flake, not a regression (F5).
- `bash scripts/test-ccy-lifecycle.bash`: passed 223, failed 0.
- qa-all not run, as instructed.

## Findings (none blocking)

1. **At-least-once, not idempotent per event ID.** A crash between the append and the token
   save re-logs the batch's unheard events on restart. Same guarantee as `dropped.log`;
   worth one sentence in §9 "Unheard" so a reader does not take `ignored=` as exact.
2. **A message naming only a parked member is not logged**, because a parked member keeps
   its role (§8) and so counts as an agent that receives. It will reach that member when its
   seat resumes from the stored token, unless stale by then. Defensible; say so in the
   "Unheard" paragraph, since the owner will meet it.
3. **An addressed reply that is only the quote fallback (`empty`) is logged by no one.** The
   addressee ignores it as `empty`; every other member sees a mention naming an agent and
   logs nothing. It did reach no agent, but the human did pick a pill, so the failure mode is
   different from the incident's. Leave as is or add `empty` to `UNHEARD_REASONS` in a
   follow-up; not for this branch.
4. `docs/agent-bus.md`: "Each agent's watcher writes one line" — `recv` and `wait` write it
   too. Say "Each agent" or "each member's sync".
5. The `test_relay_cli` stalled-relay test is load-sensitive (fails in the full run under load
   ~30, passes alone). Pre-existing, outside this branch's files; a candidate for the same
   treatment the open pager test got in 8c2de08c2.
6. `reached_no_agent`: `is_agent_user_id(user, ctx.server_name)` is implied by
   `user in ctx.roles`. Harmless; could be dropped for clarity.
