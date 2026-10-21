Verdict: MERGE

# Plan 00209 M1, bus/matrix: adversarial review, round 2

Scope: `git diff f460abb7...HEAD -- src-go .golangci.yml` on `feat/bus-v2-m1-matrix` (head
71457ebf). Round 1 (head 30f5ecae) was FIX-FIRST with should-fix 1 and 2. 71457ebf answers
both, and notes 4, 7 and 8. What was run, from `src-go/`:

- `go test -race -count=1 -shuffle=on ./internal/bus/matrix/...`: pass (matrix 8.2 s,
  fakehs 1.5 s).
- `go test -race -tags tuwunel -run TestTuwunel -v`: compiles, skips (no binary named).
- `go vet -tags tuwunel ./internal/bus/matrix/...`: clean.
- `golangci-lint run --build-tags tuwunel ./internal/bus/matrix/...`: 0 issues.
- `untracked/scratch/m1review/execprobe2/main.go`: a probe of what `exec.Cmd.Run` returns
  after a cancel, set up the way `execx.OS.Run` sets it up (`Cancel` sends SIGTERM,
  `WaitDelay` is set).

## Round 1 should-fix 1 (clean SIGTERM shutdown counted as a failure): fixed, correct

`tuwunel_test.go:97-107`: `stoppedCleanly` now accepts `errors.Is(err, context.Canceled)`.

I checked that this does not swallow a bad shutdown. The probe ran four children and
cancelled each:

| child after SIGTERM               | `Run` error          | `context.Canceled`? | `*exec.ExitError`? |
| --------------------------------- | -------------------- | ------------------- | ------------------ |
| traps TERM, exits 0               | `context canceled`   | yes                 | no                 |
| traps TERM, exits 3               | `exit status 3`      | no                  | yes                |
| ignores TERM, killed at WaitDelay | `signal: killed`     | no                  | yes                |
| dies of the TERM                  | `signal: terminated` | no                  | yes                |

So `exec` reports `context.Canceled` only when the exit was 0. Each of the other three is an
`*execx.ExitError`, and of those only the SIGTERM one is still accepted. An exit 3 or a
SIGKILL at the grace limit still fails the cleanup.

`TestTuwunel`'s context is `WithTimeout(t.Context(), …)` (tuwunel_test.go:305), with `runCtx`
derived from it. If the deadline passes first, the error is `DeadlineExceeded`, which is not
accepted, as the new comment says. The fix is right and complete.

The commit also adds `retire` (tuwunel_test.go:336-361). It locks, unlocks and deactivates
bob on the real server, reads each state back, and expects bob's token to be refused after
the deactivation. That is the real-server proof round 1 asked for. It is still unconfirmed,
because M44 has never run against a binary.

## Round 1 should-fix 2 (fakehs guessing lock and deactivation answers): fixed for every path round 1 named

- `fakehs.go:561-564`: a request with a locked account's token is now `unmodelled`, where it
  used to answer 401 `M_USER_LOCKED`.
- `fakehs.go:666-669`: signing in to a deactivated account is now `unmodelled`, where it
  used to answer 403 `M_USER_DEACTIVATED`.
- `admin.go:265-267`: a token minted for a deactivated account is now `unmodelled`, where it
  used to be a working token.
- Setting the flags, and the sign-out on deactivation, stay modelled. M44's `retire` now
  checks the sign-out against the real server.
- `fakehs/admin_test.go:41-87` covers each change, including a check that locking and
  unlocking alone is not reported as unmodelled.

One gap is left in the same class (finding 1 below). It is a note and not a should-fix: any
use of the token it hands out goes straight to `unmodelled`.

## Notes 4, 7 and 8, taken: checked

- **Note 4.** `admin.go:146-151` leaves the `displayname` key out when the display name is
  empty. This is correct. No fake-backed test sends an empty display name, though; see
  finding 3.
- **Note 7.** `sync_test.go:210-221`: `waitHeld` now calls `t.Fatal` after 2000 × 1 ms, and
  it is called only from the test goroutine (lines 81 and 201), so `Fatal` is legal there.
  The second half of note 7 is not addressed: "held" still means "received", not "waiting".
  It is carried below.
- **Note 8.** The package doc line (`client.go:12-13`) is right in substance but too narrow;
  see finding 2.

## Full hunt, re-run

Since round 1, only 71457ebf has changed `src-go`; `.golangci.yml` is unchanged. I re-read
the transport (client.go:140-160, 325-440) and `Send` (member.go:102-150). Every round 1
"holds" still holds:

- **Token leaks.** The token goes only in the `Authorization` header, set by a closure. The
  `*url.Error` that `Do` returns is dropped (`lost`). Errors carry no server text.
- **Redirects.** `CheckRedirect` returns `ErrUseLastResponse`, and `judge` refuses every
  3xx. `Proxy` is nil.
- **Bounded reads.** Answers are read through `LimitReader(maxAnswerBytes+1)`. Response
  headers are capped by `MaxResponseHeaderBytes`. Compression is off.
- **429s and retries.** A 429 gets at most `maxTries`, and a wait over `maxRateWait` gives up
  at once. `Send` retries only `answerLost` (a transient `ErrUnreachable`), keeps the same
  transaction ID, and never retries after a cancel.
- **Contexts and goroutines.** Each try has a `WithTimeout` with a deferred `cancel`, and the
  body is read under it.
- **fakehs drift.** The replay test (`replay_test.go`) passes over the 74 fixture files. The
  fix commit adds no answer text that the fixtures lack: its new answers are all
  `unmodelled` 500s.
- **.golangci.yml scope.** Unchanged from round 1, and still correct.

## New findings

### 1. note: a locked account can still sign in, and be minted a token, in fakehs

`fakehs/fakehs.go:640-671` (`login`) never reads `u.locked`, and neither does
`fakehs/admin.go:253-268` (`mint`). So after `locked: true`, a password sign-in answers 200
with a fresh token, and so does a mint. Neither of these is in any H4 recording. Synapse
refuses a locked account's sign-in with 401 `M_USER_LOCKED`, and nothing here shows what
Tuwunel 1.9 does. This is the same class as round 1 should-fix 2.

The damage is limited. The first request made with that token is `unmodelled` (fakehs.go:562),
so a test that uses it fails `CheckModelled`. But a host flow that only asks "is the sign-in
refused after a lock?" would pass against the fake.

Fix: in `login` and `mint`, return `unmodelled(...)` when `u.locked`, next to the
deactivated checks, and add both cases to `TestALockedAccountsRequestIsUnmodelled`.

### 2. note: the new package-doc sentence on net/http resends is narrower than net/http

`client.go:12-13` says net/http "resends a GET, once, when a reused keep-alive connection
closes before any answer byte".

Two parts of that are too narrow. The transport retries for as long as it keeps picking
reused connections that turn out dead, so with more than one stale idle connection in the
pool it resends more than once. And the resend is not limited to GET: a PUT or POST is also
resent when none of it was written, since `bytes.Reader` bodies get a `GetBody`.

The behaviour is safe either way. Round 1 note 8 explains why, and
TestSendRetriesALostAnswer shows that a written PUT is not replayed. This is a doc accuracy
point only.

Fix: change "a GET, once" to "an idempotent request (or one none of which was written)".

### 3. note: the fix commit's tests have small gaps

- No fake-backed test calls `CreateAccount` with an empty display name (only
  `tuwunel_test.go:223` does, and it skips), so the branch that leaves the key out is never
  run.
- In `fakehs/admin_test.go`, the mint and reactivation checks after the sign-in check rely
  on status 500 alone. `CheckModelled` keeps the first entry (fakehs.go:266-273), so the
  `CheckModelled() == nil` half of the reactivation check is already settled by the sign-in
  before it.
- The test name `TestDeactivationRevokesAndRefusesLogin` still says "refuses login", but the
  sign-in is now unmodelled.

Fix: add an `admin_test.go` case that checks the PUT body has no `displayname` key for `""`.
Rename the test, for example `TestDeactivationRevokesAndLeavesSignInUnmodelled`.

## Open notes from round 1

- 3: other unrecorded answer texts (413 text, `M_BAD_JSON` text, empty-display-name default,
  kick and receipt texts, 404 for a wrong method) are still not labelled as unrecorded.
- 5: TestCloseLeavesNoGoroutine still spends a fixed 2 s on `settle(0)`.
- 6: TestAnswerCap is still the slowest test, because it decodes a 32 MiB answer.
- 7 (second half): `waitHeld` still treats "received" as "held"; the hold itself is not
  observed.
- 9: `Config.Token` and `Registration.Password` still print in clear under `%v`.
- 10: fakehs keeps every request, the Authorization header included, with no trim.
- 11: devices, joined_rooms, kick, leave and receipt are still proven only by v1 code; M44
  (now including lock and deactivation) has never run against a real Tuwunel.

## Findings by severity

- blocker: 0
- should-fix: 0
- note: 3 new (findings 1-3), plus 7 open notes carried from round 1
