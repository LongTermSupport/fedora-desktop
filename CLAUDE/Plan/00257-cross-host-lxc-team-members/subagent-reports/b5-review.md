I couldn't write the report to `/workspace/untracked/worktrees/sealed-carry/CLAUDE/Plan/00257-cross-host-lxc-team-members/subagent-reports/b5-review.md`. My role is read-only: no Write tool, and Bash writes are forbidden. **No file exists at that path**, so please save the review below there yourself.

**Verdict: FIX-BEFORE-MERGE**

## QA Review — Task B.5 (`f583bedbe..dbee8d71d`, 6 commits)

### Blocking
1. **ADMIN-1 is not closed: any local user can pass the peer check.** `src-go/internal/teamchat/ctl/peer.go:188` accepts a process whose cgroup path merely *ends in* `/teamchat-server@<team>.service`.
   - The real unit lives at `/system.slice/system-teamchat\x2dserver.slice/teamchat-server@<team>.service` (`peer_test.go:73`).
   - A user can start their own user unit (or make a cgroup in their delegated `user@<uid>.service` subtree) called `teamchat-server@<team>.service`, then bind the team's port while the homeserver is down.
   - The socket's uid and that process's euid are both the user's, so `unitRunsAs` (`peer.go:141-169`) returns true, and the admin token, registration key and admin password go to the squatter.
   - This was found by reading the code; I did not run it, as there is no systemd in this container. The comment at `peer.go:27-28` (DynamicUser, "which no process outside the unit holds") is not what the code checks.
   - Fix: require the cgroup path to equal the system unit's path exactly (`0::/system.slice/system-teamchat\x2dserver.slice/<unit>`). Better still, also require the uid to be the unit's dynamic user. Add a red test for a `/user.slice/.../user@1000.service/app.slice/teamchat-server@alpha.service` process. `TestADMIN1ASquatterIsRefused` (`peer_test.go:107-120`) only tries `/user.slice/x.scope`.

### Should fix
2. **HOST-4 is deferred, but no open task owns it, and deferring is the owner's call.** `PLAN.md:105` marks B.5 done with "HOST-4 deferred to the suspend work", but B.2a, B.2b and B.2c (`PLAN.md:98-100`) never mention HOST-4. Either get the owner's yes and record HOST-4 in a named open task, or leave B.5 open. The Defence Before Fix rule says leaving a known finding unfixed is the owner's decision.
3. **The user docs no longer match the terminator.** `docs/agent-bus-cross-network.md:68-70` still says the terminator "relays the bytes to the homeserver". It now relays only `/_matrix/` and `/.well-known/matrix/`, refuses upgrades and CONNECT, caps each source at 64 connections, and closes a client that sends nothing within 10 s. The author's report admits this (b5-report.md:172). The docs should move with the behaviour, not wait for B.11.
4. **The 64-per-source cap and resume.** The member client has no `MaxConnsPerHost` (`matrix/transport.go:155-170`). After a member host suspends, the server-side ends of its dead connections stay counted until `IdleLimit` (2 min, `tlsterm.go:58`) while the members reconnect, so the old and new connections overlap.
   - Several seats behind one NAT address will hit the cap during a reconnect storm, and the refusals show up as outages.
   - The report calls it "about 30 seats" (b5-report.md:168-171) but does not count this overlap. `MaxPerSource` cannot be configured from `teamchat tls`.
   - Under the no-degrade rule, this is a decision for the owner: either a flag or host variable for it, or the owner's yes to the limit.
5. **HOST-7 can now refuse declarations that applied before.** `hostnet/route.go:44-56` refuses any declared network with a longer route on another interface inside it, for example a broad VPN prefix that contains a podman or LXC bridge prefix. That is the intended fix, but it is a behaviour change that hits at the next apply (exit 70), and nothing shows the live declarations were checked against it. The deploy note in `meta-deploy.list` should warn about it.

### Nits
6. The chunk-size parse at `tlsterm/gate.go:219` uses `strconv.ParseInt(...,16,...)` after `TrimSpace`. It accepts `+a` and surrounding whitespace, which a strict parser rejects. Safe in practice (a homeserver that rejects it closes the connection), but it is a gap between how the gate and the homeserver read the same bytes.
7. A `Transfer-Encoding` header on an HTTP/1.0 request is accepted (`gate.go:107-110`). RFC 9112 §6.1 says to treat that as faulty.
8. No test pipelines a chunked or Content-Length body followed by an admin request. `TestT10ALaterRequestIsJudgedToo` (`gate_test.go:231`) only covers a request with no body before the admin one.
9. Browser preconnect sockets (Element) will be closed at `RequestLimit` (10 s). The browser retries, so this is harmless.

### Checked and clean
- **HOST-2/3:**
  - The `terminator` digest set (base unit, drop-in, program) is compared at `tlsunit.go:85`, and `owed` drives both the reload and the restart.
  - The record is written after the start.
  - `programDigest` follows the `/usr/local/bin/teamchat` link that `ExecStart` runs.
  - An old `units.json` without the set causes one restart.
  - The play widens the scope on `tc_kit_current is changed`, with `.results` guarded for runs skipped by tags.
- **HOST-1:**
  - The stamp is written (`apply.go:1002`) before `syncHealthFilter` (`apply.go:982`), so a first apply includes its own addresses.
  - Remove re-renders the drop-in, under a host-wide flock.
  - The reload is recorded by digest, so a failed reload is finished by the next verb.
  - The reach probe asks `/_matrix/client/versions` (`matrix/reach.go:26`), which the gate allows.
- **SEC-5 framing:**
  - Requests with both CL and TE, several CLs, a signed CL, a TE other than chunked, folded headers, bare LF, `Upgrade` and CONNECT are all refused.
  - Every pipelined request is judged by `readHead` (`tlsterm.go:480`).
  - `%2e` dot segments are caught.
  - The admin paths (`/_synapse/admin`, `/_tuwunel/`) are used only on loopback (`hostnet/probe.go:30`, `matrix/admin.go:16`).
- **SEC-4 accounting:** `release` is deferred straight after `admit` (`tlsterm.go:287`), so no path leaks a slot. `LimitNOFILE=576` covers 256 pairs of connections.
- **Suspend:** the timers run on Go's monotonic clock, which does not advance during suspend, so the 10 s `RequestLimit` cannot expire across one.
- **HOST-7:** IPv4 and IPv6 are handled, loopback host routes are excluded, and `Zone` and `Route` agree whenever there is no split.
- **ccy bump:** `CCY_VERSION` 3.122.25, and `LABEL` and `REQUIRED_CONTAINER_VERSION` both read 2.84.32, with a changelog entry.
- **meta-deploy.list:** queues `play-teamchat.yml` then `play-claude-yolo.yml`, each with its description.
- **Public safety:** only RFC 5737, 2001:db8 and loopback addresses appear in the diff; no names or home paths.
- **Plan and journal:** they agree with each other.

### Mechanical gates
- **Go tests:** `go test -race -count=1` over tlsterm, ctl, host, hostnet, unitfiles, paths, carry, admin and v1names all passed.
- **`qa-all.bash`:** not run. The hooks daemon blocks a full QA run inside a sub-agent (R-SUBAGENT-FULL-QA), so the coordinator has to run it.
- **`--syntax-check play-teamchat.yml`:** could not run. `ansible-playbook` and `scripts/qa-ansible-syntax.bash` both exit because `vault-pass.secret` is missing from this worktree, and I may not create it. This still needs to be run.
- **`plan-qa --sweep`:** no findings for 00257.