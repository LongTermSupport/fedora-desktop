## 12:08 · action · —   — S8 done: the seat key, the creation probe, the no-roster take

Slice S8 of Task 8.8 (the seat key and the remote take, SPEC-cross-network X7 to X11) is done and
committed on `spike/v2-cross-network`. The seat package now records a team's homeserver in the
seat store and refuses a second one that names a different URL or server name (X8, X9);
creating a seat for a URL this machine cannot reach fails at once naming the URL, and the probe
runs over TLS with the bundle's CA only when a seat is created, never on a reissue (X10); a take
works on a machine that holds only the seat store (`StateNoRoster` is taken like an active
seat, a parked or retired one is found by its refused login) (X7, X11); and a first login
answered with an auth refusal is a reissue.

It was built test first: 19 seat tests were written red, the four remaining edits made them
green, three older tests were wrong once the code ran and were corrected, and eight mutation
checks each went red on the intended tests. Lint, vet and the project rules are clean in the
default, `busfailpoints` and `tuwunel` builds, and `go test -race -shuffle=on -count=3` passes
for `seat` and `sweep`. The report is `subagent-reports/261103-crossnet-seatkey-2.md`.

Left to the owner (also in the report): whether a take whose login finds the homeserver down
should release `held/` as a refused login does (today a retry for a session whose record is
restorable is `ErrHeld`); where `checkout.json` keeps the checkout's real path; a way to move a
team to a new URL; the launcher's behaviour with the homeserver down; whether a no-roster take
that gets `M_USER_DEACTIVATED` quarantines the seat; and the creation probe running under the
allocation lock for up to 15 seconds.
