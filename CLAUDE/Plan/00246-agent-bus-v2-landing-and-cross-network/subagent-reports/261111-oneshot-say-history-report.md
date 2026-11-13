# Task 8.11: one-shot `say` and `history` for a member with no session running

Asked for by a downstream estate's verify-x30 (X30's message leg: two workers on different
networks message each other with the coordinator absent, driven by a script).

## What was true before

- `teamchat say`, `history` and every agent verb talk to the link over `link.sock`
  (`member/client`); with no link they exit 69 at once. There is no keeper-less path, on
  purpose: Plan 00209 DESIGN-v2-B BD2 rejected "a degraded direct path that reads the store and
  the token itself" (a fallback beside the link, and a second writer of a store whose only
  guard is the seat lock, B 2.5).
- The keeper itself is the one-shot. `teamchat run --plain` takes the seat lock, starts the
  link, waits for its `ready` (stores loaded, socket bound), runs CMD with stdin, stdout and
  stderr inherited and `TEAMCHAT_RUNTIME` set, ends the session when CMD exits, and exits with
  CMD's status (B 2.4.1). `x30-lab.bash` already drove the leg as
  `teamchat run --plain --kind host --state DIR --seat T/S -- sh -c '... | teamchat say --to H'`.
- A second keeper on a held seat is refused at step 6: exit 75, `seat-held`, before CMD runs.
  The running session is untouched. Nothing can race a running keeper or corrupt its store.
- `history` is safe at any moment of a run: it verifies the roster on demand
  (`teamsync.rosterFor`) and reads the room.

## The gap

`ready` does not wait for the team's first contact with the homeserver. A say that reaches the
link before it is answered at once:

- 20 `unreachable`, "team T has not been verified yet", when the seat has no roster cache (a
  seat's first session: always the case for a freshly issued relay seat); or
- `queued` with a cache, the run's send recovery not yet made (n3d-D19). The one-shot then
  ends, the link shuts down without draining the outbox, and the message is posted only at the
  seat's next session.

On the e2e tests' fakehs the say wins only because the race-built test binary starts slowly.
Across networks (TLS, several round trips) the say loses. Red proof:
`TestE2EOneShotSayWaitsForFirstContact` gates the homeserver's main state read
(`fakehs.Gate`). The one-shot say exited 20: "team alpha has not been verified yet".

## The change

n3d-D19 (review A m2, pinned by `TestN3dD19ASendBeforeTheRecoveryIsQueuedAtOnce`) says an
agent's send is never held for a sender that cannot post yet. That decision is kept for every
say that does not ask otherwise. The script opts in instead:

- `teamchat say ... --wait-contact` (wire `say.wait_contact`): the link holds the say until
  each team it may go to has had its first contact in the run, at most 10 s
  (`engine/firstcontact.go`, `firstContactWait`). It logs one `link.log` line per team waited
  on. A team's first contact is its sends recovered and its store loaded, which follow verification in that order (a seat's first session has no store until that Load; the gate's first run caught a say released between the two, 20 "not synced yet", fixed with `TestASayWaitingForContactWaitsForTheFirstStore`). The wait also
  ends early when the team is down, stopped, removed or refused, or has no team goroutine.
  After the wait the say answers exactly as one without the flag.
- Only `say` takes the flag, because that is all the leg needs. `ping`, `reply` and `report`
  are unchanged.
- Container 2.84.5 / CCY 3.118.7: teamchat is built into the image.

## Proof

- `engine/firstcontact_test.go`:
  - posted once the team is reached;
  - queued when the team goes down during the wait;
  - 20 once the bound passes on the fake clock;
  - no wait without the flag;
  - no wait for a team already reached.
- `client/send_test.go`: the request frames `wait_contact`. `cli/verbs_test.go`: the usage line.
- `wiring/e2e_oneshot_test.go`, every process real over fakehs:
  - a first one-shot say on a fresh seat is posted and received by the coordinator, and a second
    one-shot's `history` lists it;
  - the gated row, on a seat with no cache and then with one, is posted after the contact;
  - a one-shot on a held seat exits 75 and posts nothing.
- No detector can express this defect class (an answer given from a state not yet known at a
  run's start; it is a timing property of one engine's state). It is stated here and handled
  conventionally. `history` was checked and has no such window.

## What a script runs

```
printf '%s\n' "$text" | teamchat run --plain --kind host --state "$DIR" --seat "$TEAM/$SEAT" \
    -- teamchat say --to "$NAME" --wait-contact --team "$TEAM"
teamchat run --plain --kind host --state "$DIR" --seat "$TEAM/$SEAT" -- teamchat history --team "$TEAM"
```

- The say's stdout is one `sent` record. Success is exit 0 with `"queued":false` and an `id`. A
  script must treat `"queued":true` as not delivered (the server was down for the whole wait).
- Exit 75 `seat-held` means a session holds the seat. Use that session (its own
  `TEAMCHAT_RUNTIME`) or wait for it to end.
- `history` defaults to main, 20 items, newest first (`--room`, `--limit`).
- `$DIR/$TEAM/` must hold the seat (`seat.json`, `token`). No production command takes a pool
  seat placed by `seat place-issued` into a session yet (Plan 00209 Task 4.2). `x30-guest.bash stage-seat` copies it.
- `--kind host` names the member kind for a failed link's recovery text only.

## Left

- X30's run itself.
- The pool-seat take (Task 4.2).
- Each one-shot is a session, so the team's room sees that seat's session-start and session-end
  notices once per run.
- Dogfood note for the coordinator: on a one-line edit, the hooks daemon's lint-on-edit reports
  `x30-lab.bash`'s pre-existing shellcheck info findings as a blocking failure. These are SC2329
  for functions the plan library calls indirectly, and SC2016. The repository's `qa-bash` gate
  is the judge of that file. The hook's severity threshold is the fault. It was not filed from
  this seat.
