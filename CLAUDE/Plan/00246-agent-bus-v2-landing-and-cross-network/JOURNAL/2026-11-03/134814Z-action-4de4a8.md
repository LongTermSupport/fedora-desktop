## 13:48 · action · 4922b3217   — S9 done: teamchat seat place-issued

Slice S9 of Task 8.8 (`teamchat seat place-issued`, SPEC-cross-network X21 to X23) is built and
committed on `spike/v2-cross-network`: the member verb `seat place-issued --checkout DIR`, which
reads one issued seat on stdin (the answer of `teamchat-admin member add`, `return` or `reissue`,
or its bundle alone) and places it in this user's seat pool for the checkout's real path, under
the allocation lock, the credential only in the pool. No ssh and no `execx` code was written: X21
has the owner pipe it in as themself, which overrides the code map's "push command through execx".

Three agents worked on it and two stopped before it was done: the first at its context budget,
after `paths.CheckoutKeyOf` and a handoff, the second on the account's weekly usage limit, which
resets on Nov 6 at 23:00 UTC and stops every further agent until then. The `Place` flow, the verb,
the contract test and the docs row were written by the lead from the committed red tests. The
package tests pass as root with `-race` and as uid 65534; lint and the project rules are clean in
the default, `busfailpoints` and `tuwunel` builds; six mutation checks each went red on the
intended test. One test fixture was wrong (the lock-held test asserted the lock free while it held
it), and a project rule found one map-ordered return in a test helper. The report is
`subagent-reports/261103-crossnet-placeissued.md`.

Left to the owner, in the report: the credential kind is the seat store's and the only production
credential is the password until DESIGN-v2 7.1 is decided (X23 reads as a per-seat choice); the
pipe produces the answer of `member add`, which place-issued reads as well as the bare document;
and a team that declares `networks` and no `url` applies cleanly and then fails at `member add`,
because the real bundle maker refuses an `http` URL that is not loopback.
