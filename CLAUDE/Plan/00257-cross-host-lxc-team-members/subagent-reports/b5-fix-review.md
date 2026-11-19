**Verdict: FIX-BEFORE-MERGE**

## QA Review: B.5 review fixes (`c825276bd..13ccc3519`, 4 commits)

I read everything through git at `13ccc3519`, not from the working tree. I ran no tests and no gates: `qa-all.bash` is blocked inside a sub-agent (R-SUBAGENT-FULL-QA), and running `go test` would have needed a temporary worktree.

### Should fix (all in the dead-connection reclaim, `src-go/internal/teamchat/tlsterm/tlsterm.go`)

1. **A connection waiting on the reclaim keeps a global slot for up to 4 s, so one source can again fill every slot.**
   - `admit` takes a slot at `tlsterm.go:352`, then waits in `reclaim` (`:377`, up to `ProbeLimit`) before it refuses and gives the slot back.
   - A client whose held connections are alive answers every probe, so each extra connection it opens holds a slot for the full 4 s. Opening `MaxConns - cap` of them every 4 s keeps all 256 slots full.
   - Before this change the refusal was immediate. This undoes SEC-4 for exactly the teams that set a lower `connections_per_source`.
   - Fix: check the per-source share before taking the slot, or give the slot back while waiting. Also allow only one reclaim per source at a time.

2. **Two reclaims at once undo each other's probe.**
   - Without a `Probe` configured, `reclaim` defers `restoreKeepalive(held)` (`:403`).
   - If reclaim A finds room and returns, it puts the 15 s / 9-probe keepalive back on connections that a concurrent reclaim B is still probing. B then times out and is refused, while dead connections are still held.
   - Several seats reconnecting at once is the very situation T9a exists for, so this race is the normal case.
   - The `once.Do` fake probe in the test hides it.

3. **The keepalive does not reclaim the most common dead connection, and the docs say it does.**
   - Linux sends no keepalive probe while a socket has unacknowledged data in flight. This is from the kernel source (`tcp_keepalive_timer` skips when `packets_out` is non-zero); I did not run it.
   - A member's `/sync` long-poll is answered within 30 s of its host suspending, so its server-side connection usually has unacknowledged data and is only freed at `IdleLimit`.
   - The fix report admits this. But `docs/agent-bus-cross-network.md:83` and SPEC-cross-network X18 (`:137`) say such connections "are reclaimed" with no caveat.
   - Fix: also set `TCP_USER_TIMEOUT` for the length of the probe, or state the limit in both documents.

4. **No test shows the production probe failing a dead peer.**
   - The NAT-behind-suspend test the coordinator asked for (`gate_test.go:144`, `:153`) uses a fake `Probe` that simply closes the connections.
   - `TestT9aTheLivenessCheckIsAShortKeepalive` (`:187`) only shows that a *live* connection survives the probe, plus some arithmetic on the constants.
   - So the claim at the heart of the fix is never exercised on the real path.

5. **With the default cap, the reclaim never runs and SEC-4 is off.**
   - `DefaultMaxPerSource = DefaultMaxConns` (`:71`). With both at 256, a source reaches its cap only when every slot is already taken, so `admit` refuses on the slot check before the reclaim can run.
   - Exhaustion of the global slots by dead connections has no reclaim at all.
   - The author raised this for the coordinator in the journal entry and the fix report, but PLAN.md marks B.5 done without recording it. It needs a ruling, or recording as an open item in PLAN.md.

### Nits
6. `reports/AUDIT.md` has no B.2d row. HOST-4 appears only in a parenthetical on the B.5 row.
7. `chunkSize` refuses whitespace before `;` (the test case `"5 ;x=1"`), which RFC 9112 §7.1.1 allows. That errs on the safe side, but the comment claims RFC 7.1's grammar.
8. No ADMIN-1 test uses a team id with a dash. By my reading the path is still correct, because the instance part is not escaped in the cgroup path.

### Resolved and clean
- **ADMIN-1:** the cgroup v2 path must now match exactly, and the `\x2d` escaping is pinned against a hand-written literal. The new user@-unit case would fail against the old suffix check, and the ctl unit has no cgroup namespace (`unitfiles.go:154`).
- **HOST-4** is Task B.2d, with a design and the ruling recorded.
- **Docs:** the cross-network doc now describes the request gate and the limits.
- **HOST-7:** `meta-deploy.list` and the docs both carry the warning.
- **Gate nits:** HTTP/1.0 with a transfer encoding is refused, the chunk-size parse is strict, and there is a body-then-admin test.
- **`connections_per_source` validation:** the declaration refuses 0, 257 and a string. The CLI refuses `0`, `257`, `+4` and `040`. A changed cap changes the terminator's digest and restarts it. The play passes the setting through as JSON, so validation is in Go.
- **Flaky test:** `AwaitDueFrom` matches on the arming function's full name and is unit-tested.
- **`project_shared_due` analyzer:** now covers any package's limits, with a red fixture, internal and external tests, and its blind spots written down.
- **Versions:** each changelog entry has one version. `CCY_VERSION` is 3.122.28, and the Dockerfile `LABEL` and `REQUIRED_CONTAINER_VERSION` both read 2.84.34.
- **Public safety:** only 127.0.0.1, 192.0.2.10 and 203.0.113.9 appear, and no paths, names or emails.

### Mechanical gates
- `qa-all.bash`, `plan-qa --sweep` and `--syntax-check`: not run (sub-agent block; reading only through git). The coordinator must run them.