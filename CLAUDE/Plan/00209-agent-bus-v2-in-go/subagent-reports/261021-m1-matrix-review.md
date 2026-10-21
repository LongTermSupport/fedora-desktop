Verdict: FIX-FIRST

# Plan 00209 M1, bus/matrix: adversarial review, round 1

Scope: `git diff f460abb7...HEAD -- src-go .golangci.yml` on `feat/bus-v2-m1-matrix` (head
30f5ecae). What was run, from `src-go/`:

- `go test -race -count=1 ./internal/bus/matrix/...`: pass.
- `go test -race -count=5 -shuffle=on` (TestAnswerCap and TestCloseLeavesNoGoroutine skipped
  because they are slow): pass, no flakes.
- `go vet -tags tuwunel`: clean.
- the project-rules vet driver (`qa/driver`) with `-tags tuwunel`: clean.
- `golangci-lint run --build-tags tuwunel ./internal/bus/matrix/...`: 0 issues.

## What holds (no finding)

- **Token leaks (hunt 1).** The token travels only in `Authorization`, set by a closure
  (client.go:159-161). `isToken` refuses anything outside 0x21-0x7e, so the header cannot be
  split. `Error` keeps no query, no server `error` text and no lower-level error; the `http.Do`
  error, which is a `*url.Error` holding the URL and query, is dropped (client.go:378-381).
  `Client.Format` and `Token.Format` cover every verb, and `Admin` picks up `Format` through
  the embedded `*Client`. `Token.MarshalText` refuses. `Register` sends only the MAC, never the
  shared secret. TestTokenNeverPrinted covers `*c`, `c`, the error, the session and the token
  under six verbs.
- **Redirects (hunt 2).** `CheckRedirect` returns `ErrUseLastResponse` and `judge` refuses
  every 3xx. `Proxy: nil` is set. TestRedirectRefused shows the target receives nothing for
  307, 302 and 308.
- **Bounded reads (hunt 3).** Every answer is capped at 32 MiB + 1, error bodies included, and
  the headers at 64 KiB. Compression is off. fakehs reads at most 16 MiB of a request body,
  and `http.Server`'s default 1 MiB header limit and a 10 s `ReadHeaderTimeout` apply.
- **Retries (hunt 4).** A 429 gets at most 3 tries. A wait over 60 s, or one too large to
  parse, gives up at once. `Send` retries only a transient `Unreachable` (MB10). A
  cancellation is never transient and the `Clock` wait checks the context, so nothing retries
  after a cancel. The transaction ID stays the same on every retry. A 429 inside a send is not
  also retried by `Send`.
- **net/http's own replay (hunt 4).** The client needs no change here; see note 8.
- **Goroutines and contexts (hunt 5).** Every try has a `WithTimeout` with a deferred cancel,
  and the body is read under it. fakehs `Stop` sets `closing` under its lock before
  `active.Wait`, so every `Add` happens before the `Wait`. A held sync gives up the server lock
  while it waits and wakes on `r.done` when the connection closes. `blockingServer` releases
  its handlers before `httptest.Server.Close`. Every channel wait in a test is bounded by a
  client time limit.
- **.golangci.yml (hunt 7).** The gosec entry is limited to three files, one linter and
  `G401|G505`. The reason is true: the protocol fixes HMAC-SHA1. fakehs cannot import
  `matrix.RegistrationMAC`, because the `matrix` tests import fakehs and that would be an
  import cycle, and the replay keeps its own copy on purpose so that it checks the fake
  independently. The tagliatelle change is a per-package `overrides` to `snake`, not an
  exclusion, so a case is still enforced; the wire really is snake_case, and lint passing over
  `next_batch` and the other tags shows the override takes effect.

## Findings

### 1. should-fix: tuwunel_test.go reports a clean SIGTERM shutdown as a failure

`src-go/internal/bus/matrix/tuwunel_test.go:97-104` (with :70-73 and :76-85).

Evidence. On cleanup, `stop()` cancels `runCtx`, and `execx.OS.Run` sends SIGTERM through
`cmd.Cancel`. When a child exits with status 0 after the context was cancelled, `exec.Cmd.Run`
returns `ctx.Err()`. I ran `untracked/scratch/m1review/execprobe/main.go`: a `sh` child that
traps TERM and exits 0, run through `exec.CommandContext` with the same `Cancel` and
`WaitDelay`. It printed `err=context canceled exit=0`. `execx` hands that back as
`fmt.Errorf("run %s: %w", ...)` (execx.go:146-147). That is not an `*execx.ExitError`, so
`stoppedCleanly` returns false and the cleanup calls `t.Errorf`.

`helpers/agent_bus/render.py:29` records that "Tuwunel exits 0 on SIGTERM". So every
successful run against a real Tuwunel 1.9.x would fail at cleanup. The commit message says the
test has only ever been seen to skip, so this has never run.

Fix: in `stoppedCleanly`, also accept `errors.Is(err, context.Canceled)`, which is the exit-0
case, alongside the SIGTERM signal case. A cleaner alternative is to keep the cleanup's own
`stop()` separate from the test's 2-minute deadline, so that a timeout still fails the test.

The rest of the M44 harness matches the repository. `TUWUNEL_CONFIG`, the bare top-level keys
and the key names are the ones `render.py` renders and H4 started Tuwunel with.
`max_request_size` is at Tuwunel's floor. `allow_room_creation = true` is a deliberate
departure, and the comment says so. These parts could not be confirmed without a real binary:

- that `database_path` is created when missing
- that `GET /_synapse/admin/v2/users/{id}/devices` exists and lists a device for an
  admin-minted token (H4 recorded neither; see note 11)

### 2. should-fix: fakehs models lock and deactivation that no recording supports, and v1 refused them

Evidence:

- `fakehs/fakehs.go:560-563`: a locked account's token gets 401 `M_USER_LOCKED` "This account
  has been locked.".
- `fakehs/fakehs.go:665-667`: logging in to a deactivated account gets 403
  `M_USER_DEACTIVATED`.
- `fakehs/admin.go:197-229`: `deactivated` and `locked` are accepted on PUT v2/users, and
  deactivation logs out every device.
- `fakehs/admin.go:253-266`: `mint` gives a working token for a deactivated account, so that
  account can act again. Real servers refuse this; it is not modelled either way, and nothing
  says so.

None of this is in the 73 fixtures; grep finds no `USER_LOCKED`, `DEACTIVATED` or `locked`
outside the user objects. The v1 fake deliberately refused it
(`tests/helpers/pingbus/fake_admin_api.py:9`: "Fields H4 did not exercise (`deactivated`,
`locked`, ...) raise"). The package doc (fakehs.go:9-11) says a request beyond the model "is
answered 500 and recorded ... rather than guessed at". These answers are guesses.

This matters because `bus/admin` will be built and tested against the fake. Synapse answers a
locked account with 401 plus `soft_logout`, and whether Tuwunel 1.9 enforces `locked` at all
is not shown anywhere in the repository. A host flow that trusts "locked means 401" would pass
against the fake and could fail on the real server.

Fix: either make these paths `unmodelled(...)` as v1 did, or keep them, label each one in the
code as an unrecorded assumption, and add `SetLocked`, `SetDeactivated`, and a mint after
deactivation to the M44 real-server test so Tuwunel proves the model. Also make `mint` refuse
a deactivated account, or call it unmodelled.

### 3. note: other answers are invented, and the code does not say so

Each of these follows a spec item but uses text no fixture holds:

- the 413 body "Request body too large." (`fakehs/http.go:178`, `fakehs/fakehs.go:506`; M40
  sets the size, not the text)
- `M_BAD_JSON` "username and password must be strings" (`fakehs/admin.go:94`)
- an empty display name becoming the localpart (`fakehs/fakehs.go:588-590`)
- the kick and receipt texts (`fakehs/rooms.go:460,475,649`)
- a known path with the wrong method answering 404 rather than 405 (`fakehs/fakehs.go:532`)

`leave` and `receipt` are labelled correctly as unrecorded (rooms.go:439-440, 638-639). The
others should be labelled the same way, so that a later caller does not take them for recorded
Tuwunel text.

### 4. note: CreateAccount always sends `"displayname"`, even when it is empty

`src-go/internal/bus/matrix/admin.go:147`. H4 always sent a non-empty display name (fixtures
011, 015, 017, 019). tuwunel_test.go:221 passes `""`, so the first real exercise of an empty
display name is an untested request. fakehs quietly turns it into the localpart (see note 3).

Fix: leave the key out when `displayName == ""`.

### 5. note: TestCloseLeavesNoGoroutine always spends 2 s doing nothing

`src-go/internal/bus/matrix/client_test.go:655`. `settle(0)` loops until the goroutine count is
at most 0, which never happens, so it always runs all 400 × 5 ms. The test took 2.36 s.

Fix: measure `before` with a single `runtime.NumGoroutine()`, or settle against a plateau.

### 6. note: TestAnswerCap takes about 14 s under -race

`client_test.go:416-430`. It builds two answers of about 32 MiB, and decodes the first into
`map[string]any`. That is 60% of the package's test time.

Fix: decode into `json.RawMessage`, or check the cap at the `once` level without decoding.

### 7. note: waitHeld gives up silently

`src-go/internal/bus/matrix/sync_test.go:211-215`. After 2 s it returns without failing. Also,
a request is recorded (`enter`) before its sync waits. So "held" really means "received", and
the event, or the restart, can land before the wait begins. Both tests still pass in that
order, but they then do not test what their names say.

Fix: `t.Fatal` when the request never arrives. If the hold matters, also wait for the fake to
report that it is waiting, for example a test hook or a `Requests()` entry recorded at the
wait.

### 8. note: net/http replays idempotent GETs on a reused connection; the client needs no change

The transport replays a request on a reused connection only when nothing was written, or when
the request is replayable: GET, HEAD, OPTIONS or TRACE, or one with an Idempotency-Key header.
PUT and POST are therefore replayed only when no byte of them was written, which is safe.
TestSendRetriesALostAnswer, which expects exactly one 1 s wait and two identical PUTs after a
DropAfter, confirms that PUT is not replayed.

A replayed GET (`/sync` with the same `since`, whoami, state reads) is idempotent on the
server. It stays inside the same per-try time limit, because the try's context covers the
whole `Do`, and it goes only to the same origin. So the only effect is that a held sync
dropped by a restart is silently re-held on the restarted server instead of failing. For a
bus, that is the desired behaviour.

The fix in TestSyncDroppedByARestart (`m.Close()` first, so the held sync is on a new
connection) is correct. Suggest one sentence in the package doc (client.go:9-11) so that a
later test author does not rediscover it.

### 9. note: secrets supplied as input print in clear

`Config.Token` (client.go:91) and `Registration.Password` (admin.go:65) are plain strings.
`%v` of a `Config` or a `Registration` shows them. MB12 covers only tokens the server hands
out.

Fix (optional): add a `Format` method on `Registration`, and a doc line on `Config` saying it
must not be printed.

### 10. note: fakehs keeps every request, Authorization header included

`fakehs/http.go:130`. Nothing ever trims the slice. That is acceptable for tests and needed by
M42. It is recorded here only so that nobody reuses fakehs in a long-running harness.

### 11. note: some endpoints are proven only by v1 code, not by any recording

H4 never recorded `v2/users/{id}/devices`, `joined_rooms`, kick, leave, or receipt. Their
shapes come from the v1 fakes and `admin.py`. The only real-server check, M44, has never run
(finding 1).

## Findings by severity

- blocker: 0
- should-fix: 2 (findings 1 and 2)
- note: 9 (findings 3-11)
