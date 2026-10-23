# n3d slice review B: forge, teamsync, client, wiring and the small files

Reviewer B, at 825d2ab62, the slice `git diff 6a0435bde HEAD -- src-go`, my half: member/forge,
member/teamsync (forge.go, session.go, the answers, ln6.go), member/client/send.go, member/wiring
(the send and failpoint e2e rows), member/link, keeper/seats.go, member/cli, member/team,
member/wire/request.go, paths, failpoint/names.go, guest/places.go, .golangci.yml.

## Verdict: FIX-FIRST

Counts: blocker 0, major 3, minor 4, nit 3. One out-of-slice note, and one note for reviewer A.

Run: `go test -race -count=2` on forge, teamsync, client, link, cli, team, wire, paths, failpoint,
keeper: all pass. guest fails under `-count=2` from code n3d did not touch (see the out-of-slice
note). The send and failpoint e2e rows (`TestE2EPingAnswered`, `TestE2ESayAndReply`,
`TestE2EQueued*`, `TestFailpointChargeBeforeSend`, `TestFailpointSendBeforeRecord`) and
`TestTheTeamsConfig...` pass with `-race -count=2`.

What holds up well. The token: it reaches only the `Authorization` header (the source test is
sound: the composite literal is not a selector, so the one `credential` read is `header`'s). It
is redacted under every verb. gh's stderr and stdout never reach an error. Redirects off the
origin are refused before the other origin is asked anything, and the scheme is compared too.
The receive-side proofs: every value from the ping is grammar-checked before it reaches a URL
(owner, repo, SHA, path with no `.`/`..` segments, branch `[A-Za-z0-9._/-]` with `branchSafe`
on top, so `..` cannot be put in a compare). Forks are refused in every class. A renamed or
transferred repository fails the name or owner test after the same-origin redirect. A PR proof
needs the head repository to be the reference's own. Compare's `behind`/`ahead` direction is
right both ways. No cache entry proves anything its re-check would refuse: the repository and
pull facts are tested again for the asking class. The teamsync budget really cancels the checks
it gives up on (`defer cancel()` after `collect`), the results channel is buffered, and a check
the team's own end cuts short drops nothing.

## Major

### B-M1. A rate wait is slept past the caller's bound, so a rate-limited send exits forge-down

`member/forge/forge.go:499-507`, mapped at `member/engine/send.go:492-496`; send deadline 30 s
(`member/wire/request.go:66`), receive budget 30 s (`member/teamsync/forge.go:17`).

`get` sleeps any wait of at most 60 s (`wait > maxRateWait` is strict, so a `Retry-After: 60`
is slept), up to twice per request. Neither caller allows that much. GitHub's secondary rate
limit usually answers 403 with `Retry-After: 60`. Scenario: a `ping review ... --ref` meets that
answer and sleeps 60 s. The link cancels the request's context at its 30 s bound (through
`clock.AfterFunc`, so the context has no `Deadline()` that the forge could read). `Sleep`
returns the context's error, `Check` returns "forge: the check ended", and the engine's
`err != nil` arm answers **20 forge-down**. That is the wrong exit: it should be forge-rate, and
the help row for 20 tells the agent the link keeps retrying (see B-m4). On the receive side the
same wait uses up the whole 30 s budget in every batch while the limit lasts, and the drop is
logged `unresolved` when the cause was `forge-rate`. Nothing remembers the rate answer the way
`downUntil` remembers an outage.

Fix: keep rate health beside the down health. After a rate answer, set `ratedUntil = now + wait`,
and have every check before then return `forge-rate` at once with no request (the same shape as
n3d-D29). Give `Check` the caller's remaining bound explicitly (the engine knows the request's
deadline, and teamsync knows its budget), and treat a wait longer than what remains as `forge-rate`
without sleeping. Test: a 403 `Retry-After: 60` under a 30 s send gives exit forge-rate, not 20,
and a second check inside the wait makes no request.

### B-M2. A hostile peer can spend the member's GitHub quota; nothing bounds forge calls per sender

`member/teamsync/forge.go:35-79`; `member/forge/forge.go:285-305, 356-374`; the flood limit
`proto/limits.go:33` (`recv_per_sender_minute`, default 60, at most 600).

The only bound on receive checks is the per-sender flood window. Negative results are never
cached (spec SD5-3). Identical checks in one batch are not deduplicated. Every receiver a ping
names runs its own check with its own login. Each uncached commit check costs one request per
proving branch, and a listed repository may have 8 of them (`roster.go:27`). Scenario: one
hostile room peer sends the flood limit's 60 pings per window, each `commit:<listed repo>@<random 40-hex>`. The repository is cached, but 8 compares are made per ping: 480
requests per flood window, against a primary limit of 5000 per hourly window, so the quota runs
out long before its window resets. After that, every check in every team of the link is
`forge-rate` until GitHub's reset: honest referenced pings are dropped and referenced sends
refused. The owner's own `gh` and other tools share the same token's quota. This is the "member
calls the forge without bound" case the brief asks about. The calls are bounded per flood
window, but not against the budget they spend.

Fix (one or more of these):

- a per-sender forge budget in teamsync, for example at most N uncached checks per sender per
  flood window; past it, the ping drops `unresolved` with no request;
- deduplicate identical `ForgeCheck`s within a batch;
- a reserve on `X-Ratelimit-Remaining` below which receive checks stop asking (sends keep the
  rest);
- a short negative cache keyed by (reference, branch, class). This one changes "a negative
  result is never cached" (SD5-3), so the owner has to agree to it.

### B-M3. One slow or 5xx answer on a path the attacker chooses marks the forge down for every team

`member/forge/forge.go:171-173` (markDown), `510-511` (any 5xx is ErrDown), `558` (any
transport error or the 15 s cut is ErrDown).

Health is global to the link (n3d-D29), but the trigger can be any one request, and the hostile
ping chooses the path: the compare's base and head, or the contents path. Scenario: a peer pings
`commit:org/big@<SHA>`, where SHA is a commit in the fork network far from the default branch.
If GitHub's answer to that compare takes past the 15 s bound, or is a 5xx (a diff too large to
generate), `markDown` fires. Within the 60 s health window that follows, every honest referenced
ping in **every** team is dropped `unresolved` with no retry (n3d-D10 never holds them; they are
lost and the sender gets a TIMEOUT). Every referenced send exits 20. One such ping per health
window keeps it up. Whether the trigger is reliable depends on how GitHub behaves, which I could
not check from here. The blast radius is clear from the code alone.

Fix: take health only from evidence that the forge itself is unreachable: dial or TLS failures,
and a cut or 5xx on the repository GET (cheap, and not shaped by the attacker beyond the repo
name). A cut or 5xx on a compare or contents request is `unresolved` for that check only, with no
`markDown`. Test: a compare held past the 15 s bound makes that check unresolved, and an
immediate second check of another reference still asks.

## Minor

### B-m1. The login lock is held across the gh run, cannot be cancelled, and gh has no bound of its own

`member/forge/token.go:68-90`.

`Login.Token` holds `l.mu` while `gh auth token` runs. `sync.Mutex` cannot be cancelled. gh is
bounded only by the first caller's context, and gh can hang on a locked keyring or a D-Bus
secret service prompt. Scenario: teamsync's batch starts gh first, under its 30 s budget, and
the keyring hangs. A send arriving meanwhile waits on the mutex beyond its own deadline, by up to
that 30 s budget. Also, a failed login is not remembered, so every batch pays the full hang
again. This also breaks the package doc's "holds no lock across a request, so one slow check
holds only its own team" (`forge.go:6-7`).

Fix: run gh with its own bound on the clock seam (for example a 10 s bound). Wait for it through
a single-flight that the caller's context can cancel (a channel guarding the run, with a
`select` on `ctx.Done()`). Hold a login failure for a short span, as `downUntil` does, so a hung
keyring costs one wait per span and not one per batch.

### B-m2. The e2e failpoint rows pass whether or not n3d-D4's search works

`member/wiring/e2e_failpoints_test.go:346-368`.

After the SIGKILL the keeper restarts the link with the same seat token and device. fakehs
remembers transaction ids per (user, device, room, endpoint) (`matrix/fakehs/fakehs.go:178-181`)
and keeps them across the restart. On both points (`tried` is set at the charge,
`engine/send.go` `sendingOf`), a `RecoverSends` that found nothing would re-PUT the same txn,
and fakehs would return the first event. So "in main exactly once" holds whether the recovery's
search matched or not. The only case n3d-D4 exists for, a homeserver that forgets transaction
ids (B section 4) or a reissued device, is not exercised end to end.

Fix: add a row where fakehs forgets its txns across the restart (or the seat is reissued a new
device), and assert exactly once.

### B-m3. A send cut off after its charge exits 69 with "team messages wait on the server"

`member/client/client.go:205-212`, `member/client/nolink.go:14-15, 43-48`.

When the link dies after the request was written (the failpoint rows' case), the client's
"close before done" is `noLink()`, whose text tells an agent the link is restarting. The send is
in fact charged and will be posted after the restart. An agent that retries with the text
slightly changed (the persisted duplicate window only catches identical content) posts twice.
Fix: for `reply`/`say`/`ping` whose request reached the link, say the send may already have been
made and to check `status` (outbox and queued) before sending again.

### B-m4. The help row for exit 20 says the link keeps retrying, but a forge-down send is refused, not queued

`member/exits/exits.go:155`.

`forge-down` was added to the `ExitUnreachable` row, whose recovery text is "the link keeps
retrying on its own". That is true for `unreachable` (the send is queued, n3d-D2). It is false
for `forge-down`: nothing is charged and nothing retries. An agent that reads the exit table
(`help exit-codes`) and trusts it does not resend, and the message is lost. The per-error text
(`engine/send.go:57`, "try again later") is right. Fix: give forge-down its own row or a
recovery text that says to send again.

## Nit

- **B-n1.** `member/forge/forge.go:326-337`: the branch fetch does not check the answered name.
  `branches/main/protection` (the branch "main/protection" passes `ValidBranch` and
  `branchSafe`) is the protection endpoint. With an admin token it answers 200 and is cached as an
  existing branch. The later compare names a ref that does not exist, so no false proof follows,
  but require `v.Name == b`.
- **B-n2.** `member/teamsync/forge.go:121-130`: `loginSaid` is a field of `Team`, so forge-login
  is said once per team. n3d-D10 and the handoff (line 656) say once per link. Either move the
  flag to the checker or the link, or reword D10.
- **B-n3.** `member/forge/forge.go:99-100, 180`: `down` is redundant with `downUntil`, whose zero
  value already reads as "not down" (`Boot() < 0` is never true). Drop the bool.

## Out of slice

- `member/guest/output_test.go:79` `TestRegister` panics "guest register-test registered twice"
  under `-count=2`: it uses a global registry and does not reset it. That file is not in n3d's
  diff, but the package is in my half, so this is reported for the lead to file.

## For reviewer A (engine code that my code calls)

- `member/engine/sender.go:431`: `recoverTried` reads one backward page of 100 events from main,
  posted by anyone. n3d-D4 says "one page of 100 own events back". In a busy room, a tried send
  can lie beyond the page, and the re-send then depends on the server remembering the txn, which
  is exactly the risk D4 is there to cover.
