# Task 2.1: the load generator

## Where it lives, and why

`src-go/cmd/teamchat-loadgen` (a thin main) over `src-go/internal/teamchat/loadgen`. It is a
command of its own, not a verb of `teamchat-admin`: that program is deployed to bus hosts and
holds a team's admin authority, so a load verb inside it would ship to production one flag away
from a real team. This one is run from the checkout (`go run ./cmd/teamchat-loadgen`); no play
builds it. `scripts/go-binary-suites.tsv` has its row (`qa-go`), as every `cmd/` needs one.

## The target rule (LG1)

- `--homeserver` and `--server-name` are required; no default, no environment fallback.
- The server name is a lower-case DNS name under `.test` (RFC 6761, never delegated). A team's
  server is `<team>.teamchat.internal`, so it is refused.
- The URL is plain http to a loopback IP literal (the matrix client's own rule for plain http).
  A bus host's team server also listens on loopback, so the name is the distinguishing check.
- The setup registers the team's admin with the server's registration secret (read from a file).
  The registered user ID must be on the given name, or the setup stops before anything else is
  written. A server that already has the admin refuses the setup (fresh servers only).
- `run` re-checks the team file against the same rule.

## Use

1. `teamchat-loadgen setup --homeserver http://127.0.0.1:PORT --server-name lab.test --registration-key-file FILE --agents N --dir DIR` makes the team and writes `DIR/team.json`
   (0600, in a new 0700 directory) with the tokens and the human's password for Lightning's
   sign-in.
2. `teamchat-loadgen run --dir DIR --pings 60 --for 2h` gives patterns, each in units an hour.
3. `teamchat-loadgen run --dir DIR --replay traffic.json [--speed N] [--skip-unmade]` replays
   the probe's counts.

stdout carries the tally as JSON; progress goes to stderr.

## Patterns and replay

The package doc's LG4 and LG6 list them. A replay row the generator cannot make (state,
membership, other types and relations) refuses the replay unless `--skip-unmade` is given, and
each row left out is named on stderr. Task 1.3a's exact window (order and spacing) is not in the
hourly JSON, so replaying it is Task 2.3's input, not built here.

## Tests

The tests run on `fakehs` with a step clock, so no wall clock is read. They cover:

- every pattern's shape as the probe counts it;
- the receipts by every agent, the presence flaps and the big body's size;
- setup, refusals and the team file;
- the rates, the replay table, spacing, speed and bounds;
- a replay round trip (the probe's counts of the generated rooms equal the replayed rows);
- fault injection at each request class;
- the CLI and the interrupt.

The new `traffic.FromJSON` is tested in `traffic/fromjson_test.go`.

`tuwunel_test.go` (tag `tuwunel`) is built, vetted, linted and run through the project rules.
It was **not run**: the container has no Tuwunel binary.

On the touched packages, `go test -race`, `go vet`, `golangci-lint` (untagged and
`--build-tags tuwunel`) and the project rules driver (untagged and `-tags tuwunel`) are clean.
Package coverage is 90.1%.

## Faults met

- The error-hiding hook blocks `x, _ := f()` even where the discarded value is a bool, not an
  error. The workaround is a helper or a named variable. This is not filed.
- Not a fault, a fit: the matrix member client takes plain http to a loopback IP only, so the
  target rule follows it. A lab server on another machine would need https and a CA, which is
  not built.
