Verdict: FIX-FIRST

# F6 review 2: fix/membership-strict-json (2282ced4, 12568828 on f460abb7)

One blocking finding (1). The branch claims the Go and Python readers give the same verdict on
every file, and they do not: the Go accepts a desired file that pingbus refuses. The fix is a
few lines. Everything else holds or is a nit.

## What holds (with evidence)

- **Tests and lint pass.** `go test -race -count=1 -cover` passes over `./internal/teams/...`,
  `./internal/pingbus/...` and `./cmd/ccy-teams/...`. `golangci-lint run` over
  `teams/membership/...` and `teams/hot/...` reports 0 issues. `go vet` is clean.
- **The four earlier findings are fixed.**
  - F1: `Told` and `ToldEntry` are typed, and they are decoded with exact keys
    (`report.go:123-128`). `TestToldRoleIsWhatTheSessionWasToldOfItsRole` now asserts that
    `"told":{"roles":7}` is refused. The table covers a told key in the wrong case, an unknown
    told key, a duplicate in told, and a told with no generation or no roles. pingbus agrees
    on all of them.
  - F2: the reflection walker is gone.
  - F3: the bounds, closed sets and handle grammar are now checked. Each is pinned against
    `membership.py` and `protocol.py` (`TestTheFileContractIsPingbusOwn`,
    `TestTheGrammarIsPingbusOwn`).
  - F4: an unknown key and a miscased key get different messages, and keys are checked in
    `slices.Sorted(maps.Keys(m))` order (`strict.go:56-66`). Under the old map-order code,
    `TestTheFirstBadKeyInOrderIsTheOneReported` (20 iterations, 3 bad keys) would have failed
    with near certainty.
- **The parity test is real.** `TestPingbusReadsTheCasesTheSameWay` runs pingbus's own
  `read_desired` and `read_applied` over every table case (`python3 -I`). Any other exception
  crashes the helper, and that fails the test. `TestReadersTakeExactlyWhatTheWritersWrite`
  runs the Go reader over the same table. Together they force Go == table == Python on every
  row.
- **Independent probes agree except where listed.** I wrote 38 probe files beyond the table
  (in `untracked/scratch/f6probe/`, with the generator `gen.py`) and ran both readers on them:
  - Agreeing cases: a BOM, a non-breaking space before the object, surrounding whitespace, an
    escaped key (`"v"`), a duplicate given through an escape, `-0`, a 4096-code-point
    multibyte checkout (and 4097), NEL and U+2028 in the checkout, NaN, `1e0`, duplicate
    farewells, a seat that is also a farewell, a session of `..` or `a@b+c.d`, an int64-max
    `written_ms`, a handle in capitals, a handle with a named seat, a trailing `\n` in a
    handle, team or why, holder `--`, a told generation `0.0`, `{}`, duplicate seats in one
    team, a seat both held and refused, a 63- or 64-character host, a host ending in `-`, seat
    1000000, a 25-character team.
  - The integer past int64 differs as documented.
  - Three cases differ: findings 1 and 2.
- **The callers still behave correctly** with the stricter reader and the typed Told:
  - `discover.go:240`, `restart/jobs.go:212` and `hot/hot.go:462` use `Read` and either
    report the error or test `os.ErrNotExist`.
  - `hot/add.go` (`waitHeld`/`whyNot` surface `repErr`), `hot/role.go:255` (retries until
    told) and `hot/cc.go:139` (`Inbound`) use `ReadReport`.
  - `hot/list.go:176` falls back to `Report{}` on error (see the nits).
  - `ToldRole` on a nil `Told` returns `""`, which matches the old behaviour for a report with
    no told.
  - `Why` dropping its `Holder != ""` guard is safe, because every Report a caller gets has
    passed `check`.
  - `pingbus/statusline` does not use this package's readers. Its own pyjson reader is
    untouched, and only `writers_test.go` imports `membership`.
- **The design is sound.** Explicit decoding over pyjson values does remove encoding/json's
  leniencies: null for any field, missing treated as zero, any-case keys, and the last
  duplicate winning. A reflection walker would have had to reimplement the tag rules and
  would still carry those leniencies. The cost of explicit decoding is that a key list sits
  beside the struct tags. That drift is caught by `TestWriteThenReadRoundTrips` and
  `TestAMarshalledReportReadsBack` (DeepEqual).
- **The coverage claim is mostly right, and the failure is not this branch's doing.**
  - go-qa's narrowed coverage counts only the statements of the *changed* packages
    (`go-qa-ci internal/tools/diff.go measuredOf` uses `Diff.Changed`). Those are membership,
    plus hot because `hot_test.go` changed. Importers are tested, but their statements are
    not counted.

  - I reproduced the run with `-coverpkg` over the affected packages:

    | Measured         | HEAD      | base f460abb7 |
    | ---------------- | --------- | ------------- |
    | membership + hot | **86.1%** | 84.7%         |
    | membership alone | 93.6%     | 87.8%         |

  - The 86.1% is therefore exactly membership + hot. The shortfall is hot's pre-existing 84%.

  - Any change that touches hot fails the floor at base too. A membership-only change at base
    would also have failed (87.8%), and one now passes.

  - The branch had to touch `hot_test.go`, because the `Told` type change breaks its compile.
    It raises the measured figure.

  - The commit body is wrong only in naming the scope (see the nits).

## Findings

1. **BLOCKING: `desired.go:117` (`checkEntries`). A desired seat whose team or seat holds a
   comma is accepted by Go `Read` and `Write`, and refused by pingbus.**

   - The check is `ParseSeats(Canonical(d.Seats))`. It joins the entries with `,` and splits
     them again, so a comma inside a value turns into two valid entries.

   - Probes:

     - `"seats":[{"team":"a,1@b","seat":"1"}]` becomes `1@a,1@b`, which is two valid seats.
       Go says ok; `read_desired` says refused.
     - `{"team":"b","seat":"1@a,2"}` becomes `1@a,2@b`. Again Go says ok and pingbus says
       refused.

   - Why it matters: this is the exact property the branch claims (Go never accepts what
     Python refuses), and the table does not cover it.

   - What follows: `ccy-teams` hot/list/discover treat the file as a normal membership with
     the team `a,1@b`, while the session's pingbus refuses its whole membership (exit 78). And
     because `Write` runs the same `check`, `hot` would write such a file back
     (`hot.go:593`).

   - The defect predates the branch (the old `check` used the same round trip), but the
     branch is what claims the parity.

   - Fix: judge each entry directly, as `_names` does. For each seat:

     - require `seatName.MatchString(s.Seat) && teamName.MatchString(s.Team)`;
     - require the team not already seen.

     Drop the Canonical round trip. The minimal alternative is to also require
     `len(parsed) == len(d.Seats)`.

     Add these table rows, which pingbus already refuses:

     - `desired: a team with a comma` (`"team":"a,1@b"`, false);
     - `desired: a seat with a comma` (`"seat":"1@a,2"` with `"team":"b"`, false).

2. **NON-BLOCKING: `desired.go:36` (`isCheckout`). A checkout with lone-surrogate escapes is
   measured in WTF-8 bytes, not code points.**

   - pyjson keeps `"\ud800"` as the 3-byte form, and `utf8.RuneCountInString` counts that as
     3 runes. Python counts it as 1 code point.
   - Probe: a checkout of `"/"` followed by 2000 `\ud800` escapes. pingbus says ok (2001 code
     points); Go says refused (6001 runes).
   - The Go refuses what Python accepts, and this is not the one documented departure.
   - No writer can produce it: Go's `json.Marshal` never emits a lone surrogate escape.
   - Fix, either:
     - count a `0xED 0xA0-0xBF xx` sequence as one character (or count code points through
       pyjson's own rune view); or
     - name it as a second departure in the `strict.go` doc comment, with a test as for the
       int64 one.

## Nits

- **Commit body, QA paragraph.** "86.1% over the scope packages cmd/ccy-teams, teams,
  teams/hot, teams/membership, teams/picker" names the wrong scope. The 86.1% is over the
  statements of teams/membership and teams/hot only, which are the changed packages; the
  others are tested but not counted. The rest of the paragraph stands. Measured on base, the
  same scope is 84.7%, so the failure predates the branch.
- **`strict.go:15`.** "every one of them present" reads as covering optional keys too
  (`inbound`, `told`). Say "every required one present".
- **`strict.go:118-136`.** `integer` uses `strconv.Atoi(n.Lit)` while `integer64` uses
  `n.Int64()`. These are two paths to one rule. `integer` could be `integer64` converted to
  int, which is the same on 64-bit and has one definition.
- **`membership_test.go:198`.** `TestTheGrammarIsPingbusOwn` pins repo, host, seat and types,
  but not `HANDLE_SEP = "+"`. The `\+` in `handleName` (`membership.go:45`) is unpinned.
- **Message order.** For an object that has both an unknown key and a missing key, Go names
  the unknown key and Python (`_object`) names the missing one. The verdict is the same; only
  the message differs.
- **`hot/list.go:176-178` (pre-existing).** A report error is dropped silently
  (`rep = Report{}`), so a report the stricter reader now refuses shows every role as `-` with
  no word of why. Consider putting `repErr` in `row.State`.
- **`isCheckout` (pre-existing).** It accepts a checkout with invalid UTF-8. `json.Marshal`
  writes each bad byte as U+FFFD, so `Read` returns a different checkout from the one `Write`
  was given. `utf8.ValidString` in `isCheckout` would refuse it at `Write`.
- **Python side, out of scope.** `read_desired`/`read_applied` crash with `RecursionError` on
  nesting around 5000 deep, instead of raising `MembershipError`. The Go refuses it, as
  pyjson documents. Worth an issue against pingbus, not this branch.
- **Not verified.** I did not re-run the commit's "27 of those cases were judged wrongly" claim
  against the old reader.
