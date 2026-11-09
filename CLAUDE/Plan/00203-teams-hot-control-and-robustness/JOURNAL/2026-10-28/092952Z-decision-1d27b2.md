## 09:29 · decision · —   — 3.20: typed /compact for every kind (owner direction)

Task 3.20 now types `/compact` into every session the wind-down reaches, supervised or not.

The owner, directly: "maybe for now we do need to allow typing into sessions because blocking
on hooks daemon will slow us down a lot". This replaces the earlier ruling in
SPEC-team-wind-down.md section 7 (WC1, WC8) that a supervised session waits for a
hooks-daemon supervisor request filed upstream first. The supervisor route stays as the later
direction of travel (WC8, WC9), a follow-up and not a precondition, and nothing is filed
upstream in 3.20.

Also settled in the revised section 7:

- the floor has one home: the play's value in ccy-sessions-set-going.service, one parser
  (internal/compactfloor) shared by set-going and wind-down, which reads the unit's
  environment;
- an LXC member's transcript is in its container, which ccy-teams does not read, so it is
  compacted whatever its context and its end is read from its pane (busy, then idle again),
  bounded;
- compactions run one at a time, coordinator first by priority (a coordinator still answering
  does not hold up a worker that has answered);
- until 3.16 brings its window, a 15-minute window (TW1's default) bounds the whole wind-down;
  a session still answering or still compacting at its end is reported, not a failure.
