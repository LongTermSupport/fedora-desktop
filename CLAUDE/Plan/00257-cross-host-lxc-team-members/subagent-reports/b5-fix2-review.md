**Verdict: FIX-BEFORE-MERGE**

I reviewed `13ccc3519..70bd36893` through git only. The code is right; what blocks merge is that the deploy queue's description still says the old thing.

### Should fix
1. **The host-run description in the deploy queue is out of date.** `CLAUDE/Plan/meta-deploy.list:45-52` still describes the `play-teamchat.yml` run as ccy 3.122.28 / container 2.84.34 with "per-source connection cap … default 256". It does not say that this run moves the terminator to 1024 slots, writes a new `teamchat-tls@.service` with `LimitNOFILE=2112`, and refuses over-cap connections at once with no reclaim. The rule is that this list must be exact, so the comment needs updating to 3.122.31 / 2.84.37 and the new behaviour.

### Nits
2. **The spec claims slightly more than the code does.** `SPEC-cross-network.md:133-134` says "one client … never holds every slot". The cap is per source address, so a client with 4 addresses fills all 1024 slots. With IPv6 that is trivial. The docs and changelog say "no one address", which is accurate; the spec should say the same.
3. **One RED claim depends on timing.** `TestT9AFloodAtTheCapHoldsNoSlot` only fails against the old reclaim code if the flood's waiters already hold slots when the other source dials, and nothing in the test makes sure of that. On the new code it is deterministic.
4. **One ride-out test passes with or without the fix.** In `TestT9aSeatsBehindOneAddressRideOutASuspend` the "dead" connections are live loopback connections, so it proves admission under the cap, not a real ride-out. The report says it is green on both versions, and PLAN.md B.2c now owns the real suspend test. This is honest, just weak evidence.
5. **An unrelated line was unwrapped.** `INSTALL-PATH.md:38` had a line about `--credential password` joined onto one long line in b3eabaec0, which has nothing to do with this change.

### Checked and clean
- **The reclaim is fully gone.** No `Probe`, `ProbeLimit`, `bySource`, `released`, keepalive functions, "liveness" or "reclaim" is left in `src-go/internal/teamchat`, `docs/agent-bus-cross-network.md`, SPEC X18 or the package doc. The 3.122.28 changelog entry is history and is superseded by 3.122.30.
- **`admit` (`tlsterm.go`) is correct.**
  - It checks the source's share under the lock before trying a slot, and the slot send never blocks.
  - If the slot is full, the share is never taken, so nothing leaks.
  - `defer release()` is set before `track`, so release runs exactly once on every path.
  - One harmless window: between release giving back the share and the slot, a new connection can be refused when a slot is about to free. That errs on the side of refusing.
- **No path lets one address fill the slots.** 256 is the most any address can hold, below 1024, and both the spec and the tests now enforce that.
- **The suspend ride-out argument holds.**
  - A NAT has to reach 256 connections before its cap binds, so doubling across a suspend leaves room for many seats.
  - The idle timer fires even while a write to a dead peer is blocked, so dead connections do get freed.
  - The member side's backoff exists (SY8 in `teamsync.go:26-30`, 1 s up to 30 s).
- **The homeserver descriptor claim is accurate.** I fetched Tuwunel v1.9.4 myself: `src/main/server.rs:61` calls `maximize_fd_limit().expect(...)`, and `limits.rs` raises the soft limit to the hard one. No repo unit sets `LimitNOFILE` for the homeserver, so it gets systemd's 1024:524288 default.
- **The chunk-size fix matches RFC 9112 §7.1.1.** Spaces and tabs are trimmed only before a `;`; `5 `, ` ;x=1` and `5 x;y=1` stay refused.
- **The ADMIN-1 dashed-team test** pins both the accepted cgroup path and the refused one with the dash escaped.
- **The new tests run the production path.** I checked the defaults test, the idle re-admit test and the whitespace test.
- **Versions agree in every commit.** 3.122.29/2.84.35, 3.122.30/2.84.36 and 3.122.31/2.84.37 each match across the launcher's `REQUIRED_CONTAINER_VERSION`, the Dockerfile label and one changelog entry each. Unit-file golden is 2112 = 2×1024+64; INSTALL-PATH says "above 2048".
- **Docs, spec X18, PLAN.md, AUDIT.md (B.2d row) and the changelog** all agree on 1024/256 and on the reclaim being gone.
- **Public safety:** the added lines contain only 127.0.0.x, 192.0.2.10, 203.0.113.9 and `ops-a`. No names, paths or emails.

### Mechanical gates
- **Go tests:** `go test -count=1 -race` passed for tlsterm, tlsverb, unitfiles, ctl, paths and declare. I ran them in the worktree after confirming it matched 70bd36893 exactly before and after the run.
- **plan-qa --sweep:** did not run against this branch. The worktree has no `.claude/hooks-daemon/bin`, so I ran it from `/workspace`, which is on F44. Its 2 block findings are in plans 00244 and 00238, not in this diff. The coordinator should run it on the branch.
- **qa-all.bash:** not run. It is blocked inside a sub-agent (R-SUBAGENT-FULL-QA), so the coordinator or CI needs to run it.
- **syntax-check:** not applicable, since no playbook changed.