## 11:23 · finding · —   — the merged tree's first -race run: a hang, a missing wire, a test race

The first `go test -race -shuffle=on ./internal/teamchat/...` over the merged tree found three
defects, none of them a conflict. All three are fixed in the merge commit.

1. A hang. n3e made `units.session.close` wait for `dispatch`, which ends when the
   connection closes the signal channel it was given (the `units.Conn` contract). n2's test
   fake `busConn` in `admin/wire` ignored `Signal` and its `Close` did nothing, so the first
   refused `StopUnit` blocked `TestRestoreEngineSeamsReachTheHost` until the 20-minute
   package timeout. The goroutine dump showed `close` waiting on `dispatch` and `dispatch` on
   the channel. The fake now keeps the contract, as `units`' own `fakeConn` does.
2. A missing wire. `lxc.Deps.Guard` was set only by the admin verb layer, but the wiring
   test holds that `System.LXC()` builds every seam except `Roster` and `Refresh`. The
   wiring (`wire/lxc.go`) now sets `restore.Guard(root)` beside `host.Deps.Guard`, and the
   admin test world sets its own, so the verb layer no longer overrides it.
3. A test race in n3e's `fakeinbox`: a delivery that ends in a refusal records that frame
   after the connection is closed (pinned by `TestTheLastFrameIsRecordedOnceTheConnectionIsClosed`),
   and two tests read the frames as soon as they had read the close. The old form failed 32
   of 900 stressed runs (`-count=300 -cpu 1,2,4`); both tests now wait for the frame
   (`settled`), and the new form passed 50 shuffled runs at `-cpu 1,2,4`.

Defence Before Fix: neither class has a syntax shape a rule could match, because the contract
is prose on an interface (1) and the ordering is a fact of one fake (3); both are written up
in `CLAUDE/GoProjectRules.md` with what holds them instead.
