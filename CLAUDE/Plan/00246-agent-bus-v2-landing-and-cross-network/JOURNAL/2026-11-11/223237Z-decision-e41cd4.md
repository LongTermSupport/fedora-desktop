## 22:32 · decision · T8.11   — one-shot say and history: run --plain, and say --wait-contact

Task 8.11, X30's scripted message leg. Downstream asked for a one-shot `say` and `history` for a
member with no keeper running, or a decision that the session round trip is the proof.

Finding: no keeper-less path is needed, and none is built. Plan 00209 BD2 already decided
against one (a direct store and token reader would be a fallback beside the link, and a second
writer of a store the seat lock guards). The keeper is the one-shot:
`teamchat run --plain --kind host --state DIR --seat T/S -- teamchat say --to N --wait-contact --team T`,
text on stdin, exits with the verb's status and prints its record; `-- teamchat history --team T`
likewise. A seat a running session holds is refused at once, 75 `seat-held`, before anything is
posted. `x30-lab.bash` already drove the leg this way.

Gap found (red: `TestE2EOneShotSayWaitsForFirstContact`, the homeserver's main state read gated,
the one-shot say exited 20 `unreachable`, "team alpha has not been verified yet"): a say that
reaches the link before its team's first contact in the run is answered 20 (no roster cache)
or `queued` (a cache, the recovery not yet made), and the one-shot then ends with it in the
outbox, posted only at the seat's next session. In the e2e tests the race is won only because
the race-built test binary starts slowly; a homeserver a few round trips away loses it.

Decision: n3d-D19 (review A m2, pinned by TestN3dD19ASendBeforeTheRecoveryIsQueuedAtOnce) says
an agent's send is never held for a sender that cannot post yet, so the default stays. A script
opts in with `say --wait-contact`: the link holds that say for its teams' first contact, at most
10 s, released early when the team recovers its sends, goes down, stops, is removed or refused;
it then answers exactly as a say without the flag. Only `say` takes it (what the leg needs).
Class: an answer given from a state not yet known at the start of a run; no detector can express
it (a timing property of one engine's state), so it is said so and handled conventionally;
`history` was checked and has no such window (it verifies its roster on demand).
