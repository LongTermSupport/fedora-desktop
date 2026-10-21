# m2-hooks-golden: the pingbus hooks parity oracle (Plan 00209 stage 2, Task 3.1)

Branch `feat/bus-v2-m2-hooks-golden`. The spec of record, `SPEC-pingbus-hooks.md` (commit
3acac8c5), is merged in with `--no-ff`. Nothing else from `feat/bus-v2-m2-hooks` is on this
branch. The package is `src-go/internal/pingbus/hooks/golden` and is test only. The stand-in
package `src-go/internal/pingbus/hooks` stays in place until integration (see the handoff it
came with).

## What is there

- `testdata/scenarios.json`: 88 scenarios and 140 steps, built on 6 fragments (`base`,
  `team-a`, `team-b`, `socket`, `seat-a`, `seat-b`) and 2 rooms the fake homeserver serves.
  Each scenario's `about` names the hooks.py (or cli, inbox, config, membership) lines and the
  spec's H and HB numbers it covers. The fixture contents were made with helpers.pingbus itself
  (the stored inbox form, the team record dump, `Applied.to_json`), so each world holds exactly
  what v1 writes. The generator was a scratch script and is not committed: `scenarios.json` is
  the source from now on.
- `testdata/golden/<scenario>/<n>.*`: recorded by `go test -race ./internal/pingbus/hooks/golden -update`
  from the zipapp built from this checkout. Each scenario was run twice and the two runs were
  identical. A second `-update` after the commit left the tree unchanged.
- `testdata/deviations.json` and `testdata/deviation/`: the departures the Go is judged against
  (below).
- Results: `-impl=python` passes. The default in-process run skips `TestHooksGolden` while
  `hooks.Placeholder` is set. `./scripts/qa-changed.bash` passes (go-qa on both modules).

## Coverage, by event

- **session-start**:
  - no socket;
  - socket with the watcher spawned (the mark is written and the transcript recorded);
  - socket with every team held by `watch`, and by `wait`;
  - a socket with no token;
  - no `pingbus` on PATH (`watcher_failed`);
  - seat lines through a session-home link: own seat, sibling free and then held (a real flock
    on its `seat.lock`), and history from `outbox.json` and from `consumed/`;
  - an unreadable sibling;
  - about.json: valid, `v: true`, and malformed (`state`);
  - coordinator with questions and reports joined;
  - worker with many coordinators, and with none;
  - questions refused for human_text; rooms pending; rooms malformed;
  - two teams, and two teams with one untrusted;
  - sessions dir 0755 (`record_failed`); no session_id;
  - keeper.lock free, then held;
  - untrusted (no team.json); `state/lock` a symlink;
  - config: bad member.json, an unknown member key, a token at 0640, PINGBUS_TEAMS unset, two
    bundles sharing a user_id, PINGBUS_DELIVERY unknown;
  - internal (`state` a regular file: ENOTDIR);
  - pending counts; a refused transcript path; the stalled and missing marks.
- **membership** (kind `cc`, checkout in the world):
  - session-start writes conversation.json, socket.json and told.json;
  - prompt notices: joined (with about and role) told once, left with the farewell reason, a
    role change;
  - a refused desired file (a duplicate key); `PINGBUS_MEMBERSHIP=off`.
- **conversation**: the sources startup, compact, other and non-string; a bad session_id;
  outside the membership; a relative `PINGBUS_MEMBERSHIP`.
- **prompt**:
  - humans, peer and ping counted;
  - rejected: a garbled file and a misnamed file; skipped: a writer's tmp file;
  - nothing pending; a questions-room item (coordinator);
  - delivery live (team lock held as `watch`), then released (restart and counts);
  - the revive: no mark, grace, the respawn window, older, clock gone back, watch lock held, a
    non-digit mark;
  - role notice outside the membership: told once, and an unreadable told-roles.json;
  - told.lock held by another teller;
  - transcript recorded, and refused (newline);
  - delivery unknown; keeper stopped.
- **stop**:
  - `stop_hook_active` true, missing, 0, `"false"`, null and false;
  - pending blocks once per set, again for a new set, and the reset;
  - delivery live;
  - the failure window: one window for every class, reopened at 600 s and when the clock goes
    back;
  - no waker per 600 s, and a `wait` holder;
  - watcher_failed; restarted (systemMessage), alone and with a block;
  - the guard: a directory (unwritable, no block), malformed, invalid, `v: 1.0`;
  - config; home unresolved (blocks every time); delivery unknown.
- **stdin**: garbled, a top-level array, over 1 MiB (prefix plus padding), a BOM, duplicate keys
  in both orders, and empty stdin for every event.
- **post-tool**: `{}`, and the config systemMessage.
- **session-end**:
  - clear and resume;
  - the goodbye posted with marks forgotten, said-goodbye.json and outbox.json written, and
    fired again in the same session;
  - a new session id over the same claim; no session_id twice;
  - homeserver 500 and then a retry; 403 (loses trust); wrong team record; slow (no answer
    within 4 s); rate limited;
  - two seats in parallel, and two seats with one room unknown (404, loses trust);
  - a lock that names no claim; a plain bundle; config (internal); the sessions dir a file.

## Comparison (as the package doc states it)

- stdout and exit are compared byte for byte.
- Files are every entry under the world (kind, mode, content or target, with `{world}` and
  `{hs}` placeholders), byte for byte.
- Requests are compared byte for byte: method, the path **as sent** (percent-encoding kept),
  the transaction `{txn}`, the query, Authorization, Content-Type, and the raw body text. They
  are grouped by room in sorted order, with each room's arrival order kept (H94).
- stderr is compared by shape: the `pingbus: hook <event>: ` prefix and the first three words.
  A session-end's lines are compared as a multiset (H91).

## Departures pinned (deviations.json)

- `dropped_files`: `pingbus/goodbye.json` (HB1, B7).
- 11 pinned steps, each derived from the Python record and written by hand:
  - **HB1, the attempt form.** A failed goodbye leaves the attempt form
    `{"v":1,"attempt_claimed_ms":N}`. Pinned in: homeserver-fails/1, forbidden/1,
    wrong-team/1, slow/1, rate-limited/1, and one-team-fails/1 (team-b's seat only).
  - **HB1, once per claim.** Go skips a claim that is already recorded, where v1 posts again.
    Pinned in: new-session/2, no-session-id/2 and homeserver-fails/2 (all three: stdout `{}`,
    no stderr, no requests, the previous step's files).
  - **HB3 (end-no-claim/2).** One stderr line and no goodbye. A no-op post-tool step first puts
    the world before the step on record.
  - **HB4 (end-sessions-dir-a-file/1).** One stderr line, then the goodbye goes on. The Go
    expectation is end-goodbye-new-session/1's record plus the `sessions` file (the same world
    and payload).
- **The HB1 same-session repeat is not pinned.** end-goodbye/2 and /3 differ from v1 only by
  goodbye.json, because both reach the said form.
- The default run checks that every pinned record decodes strictly. `-update-deviations`
  rewrites them from the real Go once it lands. **Review that diff against the reasons above;
  do not accept it blindly.**

## What I changed from the predecessor's harness, and why

- **Request path.** The homeserver records `r.URL.EscapedPath()`, not the decoded path. H84
  requires every segment percent-encoded, and the decoded path would hide a Go client that sends
  a raw `!`.
- **Request body and Content-Type.** The body is the raw text, not canonical JSON: H84 and H94
  compare Python's `json.dumps` bytes, separators and key order included. Content-Type is
  recorded too.
- **Request order.** Requests are grouped by room (H94): two parallel goodbyes interleave.
- **session-end stderr** is compared as a multiset, and two recordings may order its lines
  differently (H91).
- **Oversized stdin.** `stdin_repeat` takes a `prefix`, so the oversized payload is a real
  `{"stop_hook_active": false}` that would block if it were read.
- **Pinned records are re-encoded.** They now go through the same decode and re-encode as
  recorded ones (before, only unpinned ones did), so a hand-written record needs no Go encoder
  formatting. `TestEveryPinnedStepHasItsRecord` also decodes them.

Everything else is the predecessor's design, unchanged.

## Re-recording, the fakehs swap, integration

- **Re-record:** `go test -race ./internal/pingbus/hooks/golden -update` (about 35 s; the slow
  goodbye takes 4 s). Then review the diff.

- **fakehs swap:** when `bus/matrix/fakehs` reaches this branch, it replaces
  `homeserver_test.go`. It must serve:

  - create, team and power levels from `rooms`;
  - 404 for topic and `agent_bus.rooms`;
  - PUT send;
  - the failures `500`, `403`, `slow` and `wrong-team`.

  It must also record the same request fields.

- **Integration:** delete `pending_test.go` when the real `hooks` package lands, and the
  default run then judges it.

## Oddities and open questions

1. **No host-type member, against the task text.** The spec's H95 and HB6 exclude it: a plain
   `host` bundle runs v1's Element check against the real passwd home of the running uid, so
   the result depends on the machine. Go unit tests cover it. If a scenario is still wanted, it
   needs an injected home, which the oracle does not have.
2. **`teams_none` cannot be reached from a hook.** With no team left, the survey refuses the
   membership (`config`) before `_membership_now` runs. Step 6 of membership-start-and-notices
   pins that, and only the watcher can say `teams_none`. Worth a note in the spec's H76.
3. **`PINGBUS_MEMBERSHIP=off` is `config` even with PINGBUS_TEAMS set.** This follows H17, but
   that line of the task text ("membership off") does not show it.
4. **The unknown event exits 64, not 2.** `cli.main` maps argparse (HB9).
5. **`SESSION_ID_RE` is defined twice in hooks.py** (`{1,64}`, then `{1,128}`); the later one
   wins (HB5).
6. **A goodbye that fails on its first GET makes one request, with no retry**, for 500 as for
   403\.
7. **Not covered, timing-dependent:** two session-end hooks firing at once, and the SIGINT and
   SIGHUP dispositions. These are Go-only tests per the spec.
