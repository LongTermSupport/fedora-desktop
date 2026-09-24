# Plan 00080 Task 1.3: host triage run 4 (`--reachability`), findings (opus-5.5)

**Run**: the 2026-09-24 host batch, `triage.bash --reachability`. It ran all passive probes
(P1 to P5, P13) and all active probes (P6 to P12). It reached `END OF REPORT`. Its cleanup
reported "no probe containers remain" and "no probe networks remain". The raw log stays under
`untracked/plan-runs/` because it is unscrubbed.

**Anonymisation**: sessions, projects, GitHub accounts and networks are placeholders here.
Addresses, MACs, container IDs, the user's home path and the network names printed by P13 are
left out. Only counts, versions, rc values and generic facts (a port number, a flag) are
quoted.

Each finding below gives **Evidence** (the log line it rests on, scrubbed) and **Does not
establish** (the limit of what it shows). A probe that is broken or confounded is recorded
as a **probe defect**, not a finding about the host.

---

## Passive probes (re-run)

### P1 (U1): versions, unchanged from F24

- **Evidence**: `podman version 5.8.7`; backend `netavark 1.17.2`; `aardvark-dns 1.17.1`;
  rootless network cmd `pasta`.
- **Reading**: F24 still holds. The host is below the netavark 2.0 / Podman 6.0 line.
- **Does not establish**: how this netavark version *behaves* between two bridges. That was
  P7's job, and P7 is confounded (F33).

### P2 (H2, F4): the default network as configured (F29, part 1)

- **Evidence**: `"driver": "bridge"`, `"dns_enabled": false`, `"internal": false`,
  `"ipv6_enabled": false`, one `/16` subnet, and no `options`/isolate key. It has 5 container
  entries, each with one `eth0` on that `/16`.
- **Reading**: H2 is confirmed again. There is no name resolution on the shared bridge, so
  any cross-session reach is by IP only.
- **Does not establish**: that sessions cannot *discover* each other. The addresses are
  allocated low in the subnet and close together, so sweeping them is cheap.

### P3 (U10, U11): who is on the shared bridge (F29, part 2)

- **Evidence**: 5 rows, all `running`, all CCY images, and every row carries `project=` and
  `github=` labels. There are 5 distinct projects. Four sessions are on GitHub account A and
  one is on account B.
- **F29**: **everything on the shared bridge is a CCY session.** Nothing else was on it at
  this snapshot, so U10 is answered for now. The 5 neighbours are 5 *different projects* on
  *two* GitHub identities. So cross-session reach crosses project boundaries, and in one
  pairing it crosses identity boundaries.
- **Does not establish**: that a non-CCY container never joins `podman`. Any
  `podman run --network podman` would. It also does not establish that two sessions on the
  same account hold the same token or the same scopes. The label names the account, not the
  credential.

### P4 (U3, H3): listeners inside each session (F30)

- **Evidence**: `COVERAGE: probing 5 CCY session(s); 5 carry ccy=true.` Four sessions show
  `(no listening sockets)`. One session (container-B) shows `LISTEN 127.0.0.1:5432`, plus
  the same port on the IPv6 loopback. (The `/proc/net/tcp6` hex for that line decodes to
  `::1`.)
- **F30**: this is the third snapshot in a row with **no listener a neighbour can reach**
  (F22: 6 of 6; F26: 6 of 6; now 5 of 5). The one listener is a database on its default port,
  bound to loopback on both address families. That is the case the research threat model
  called "a database a session started inside its own container". It **exists** in real
  use, and only its bind address keeps it off the bridge.
- **Does not establish**: that no session *ever* binds `0.0.0.0`. This is still a snapshot,
  and no dev server was running. P4 also covers TCP only, so UDP listeners were not examined.

### P5 (U10): network inventory

- **Evidence**: 5 networks. One project network (network N1) has **12** members, `podman`
  has 5, and three have 0.
- **Reading**: same shape as F27. N1 has grown from 9 members to 12. It is a compose stack
  that this repo does not own.
- **Does not establish**: whether any N1 member is a CCY session. P4's `ccy=true` selection
  is not network-filtered and found exactly the 5 sessions on `podman`, so **no live CCY
  session was on a project network at this snapshot**.

### P13: persisted network preferences (F31)

- **Evidence**: `projects with a recorded network preference : 4`, `DEFAULT bridge : 0`,
  `NON-default network : 4`. There are two distinct networks between them (1 + 3).
- **Code reading** (this session, source-grounded): `save_network_preference` is called
  **only** from `connect_to_network`, the `ccy --connect` path
  (`lib/network-management.bash:389`, `:417`, `:428`). At the next launch the preference is
  read and applied **as a launch-time `--network <pref>`** (`claude-yolo:2186-2199`).
- **F31**: P13 counts **projects where `ccy --connect` has succeeded at least once**, now 4
  (up from 3 in F28). It does **not** count how often `--connect` runs mid-session. After the
  first successful connect, those projects join their network **at launch**, a path that
  does not need the shared bridge (research §5, Option 4: "`--network <compose-net>` at
  launch still works"). The snapshot agrees: none of the 5 live sessions is on a project
  network.
- **What this does to F28**: F28 said *"Option 4 would break real use"*. That still holds for
  the **first** attach of a session launched without a network. It is narrower than F28
  implied. What Option 4 loses is the *mid-session* attach. The *steady-state* auto-join of
  the 4 recorded projects survives it. This matters to Task 2.2 (see `../DECISIONS.md`).
- **Does not establish**: when the preferences were written (there are no timestamps), or how
  often `--connect` is used now.

---

## Active probes (first run)

### P6 (U2, H1): same bridge, TCP, by IP (F32)

- **Evidence**: `same bridge -> listener (expect REACHED) (rc=0)` → `REACHED`.
- **F32**: **H1 is confirmed on this host.** A container on the shared `podman` bridge opened a
  TCP connection to another container's port bound on all interfaces, by IP, with nothing on
  the host filtering it.
- **Does not establish**: this was tested between two throwaway probe containers, not between
  two CCY sessions. They are on the same network, and CCY adds no firewall or capability
  flags of its own (no `--cap-add`/`--cap-drop` in the launcher), so the same result is
  **expected but not observed** for sessions. It tested one TCP port, not UDP.

### P7 (U1, F8): second network to the first. **Probe defect: inconclusive** (F33)

- **Evidence**: `other network -> listener (expect NO output) (rc=1)` → `(no output)`.
- **Why this is not a finding**: the P6 listener is `nc -l -p 8080 -e echo REACHED`. BusyBox
  `nc -l` without its keep-listening option serves **one** connection and exits, and the
  container's `sh -c` exits with it. P6 used up that one connection. So P7 was probably
  aimed at a listener that no longer existed. Its silence cannot tell "blocked by isolation"
  apart from "nothing there". The log never recorded the listener's state after P6.
  Corroboration (inference, not proof): P12's rootless netns holds **20** veths. That matches
  5 sessions + 12 N1 members + 1 (the kill probe) + 2 (the dual-homed connect probe), with
  **no** veth left for the listener.
- **The result is also against expectation.** F24 puts the host below netavark 2.0. Upstream
  evidence (podman#26913, rootless 5.6.0 / netavark 1.16.1) shows cross-network traffic
  *accepted* at that level. A genuine "isolated" result here would contradict that. It would
  need a sound probe before anyone relied on it.
- **Consequence**: whether a fresh bridge is isolated from `podman` on this host is **not
  established**. Any option that creates a network (2 or 5) must pass `--opt isolate=`
  explicitly. Its acceptance test must prove isolation with a **multi-connection** listener,
  plus a **positive control after** the negative test, so the listener is shown to be still
  alive.

### P8 (U6): `network connect` from a user-created bridge (F34)

- **Evidence**: `container on a user-created bridge (rc=0)`;
  `network connect onto a second network (rc=0)`.
- **F34**: a container on a user-created bridge can be `network connect`ed to a second
  network. **U6 is answered: `--connect` survives a per-session network** of default options.
- **Does not establish**: that the container got a working interface. rc=0 only; nothing was
  sent over the second network. It also does not cover a network created with
  `--opt isolate=…`, the network Option 2 would actually create.

### P9 (U5, H5): egress and host alias from a fresh network (F35)

- **Evidence**: `EGRESS-OK`. `host.containers.internal` resolves to a link-local address,
  pasta's default host mapping.
- **F35**: a freshly created bridge has outbound HTTP and resolves the host alias. **H5 is
  confirmed in part.**
- **Does not establish**: HTTPS to the Anthropic API specifically; an actual connection to a
  host service (only name resolution was tested); reach to a compose service's *published*
  port (U7, netavark#709). None of it was tested on a network created with `--opt isolate=`
  or `--disable-dns`, which is what Option 2 would create.

### P10 (U9): DNS on a fresh network (F36)

- **Evidence**: `dns=true subnets=<a /24 from Podman's default pool> opts={}`.
- **F36**: a fresh user-created network is **DNS-enabled with no options set**, so no isolate
  option either (consistent with F24). The first half of U9's premise is confirmed.
  `ensure_network_dns()` would act on such a network.
- **Does not establish**: that it would add the public resolvers. The script's format string
  dropped the research's `servers=` field, so whether the network has DNS servers configured
  (the condition `ensure_network_dns` checks) was not printed.

### P11 (U4, H4): does `--rm` survive a SIGKILL of the client? **Probe defect: measured nothing** (F37)

- **Evidence**:
  - `start a container to kill (rc=0)`
  - `kill the podman client process (rc=1)` → `(no output)`
  - `did --rm still fire? (rc=0)` → `ccy80-probe-kill running`
  - `can the network be removed while that is so? (rc=2)` →
    `… has associated containers with it … network is being used`
- **Why this is not a finding about `--rm`**: in three ways the probe never set up the case
  it asks about.
  1. The container was started as `podman run -d --name … sleep 300`, **without `--rm`**
     (`triage.bash:474`; the research's version had it). There was no `--rm` to fire.
  2. `-d` detaches, so the podman client **exits as soon as the container starts**. By the
     `pkill` there was no client process to kill. `pkill` rc=1 means "no process matched".
  3. So **nothing was killed**. `running` is just `sleep 300` still running inside its
     300-second window. It is not a container that outlived a crash.
- **The one valid fact (F37)**: `podman network rm` **refuses, with rc=2, while any container
  is attached**. That confirms research F20 on this host. It is documented behaviour, not a
  leak measurement.
- **Does not establish**: H4 in either direction. The leak rate of a per-session network under
  SIGKILL, OOM or power loss is **unknown**.
- **What a sound P11 would need** (for Task 3.1, only if Task 2.2 creates networks): start the
  container **attached, with `--rm`**, in the background. Kill the *client* and check the
  container's state. Then kill the container's **conmon** (the process that runs `--rm`'s
  cleanup on container exit) and check again. Power loss cannot be probed. It is covered by
  the existing launch-time reaper (research F18), which would need a network sweep added
  after it reaps containers (F21b's ordering).
  **[INFERRED, not observed here]**: Podman runs `--rm` cleanup from conmon's exit command,
  not from the client. So killing the client alone is not expected to strand anything. The
  cases that matter are conmon's death and a host that stops.

### P12 (U8): where a leak would live (F38)

- **Evidence**: `ls` of `~/.config/containers/networks/` and
  `~/.local/share/containers/networks/` both returned **rc=2, no such directory**. The
  rootless netns lists `lo`, the host uplink, **3 bridges**, and 20 veths.
- **Probe defect (paths)**: the missing directories are **not** evidence that no network
  configs exist. The host has 5 user networks plus the probe network. The probe looked in the
  wrong places. On rootless netavark they are expected under the storage graph root
  (`~/.local/share/containers/storage/networks/`) **[INFERRED, not observed]**.
- **F38**: there are 3 bridges in the rootless netns at the time of P12, and exactly 3
  networks had members then: `podman`, N1, and the probe network (still held by the running
  kill probe). The **3 networks with no members have no bridge interface.** So a network that
  nothing is attached to costs a config file and a subnet allocation, **not** a live
  interface. The research F10 already ruled out subnet exhaustion.
- **Does not establish**: the mapping of each bridge to its network was matched by count, not
  printed. And nothing actually leaked in this run, so a *stranded* network (one whose
  container also survived) was not observed.

---

## Hypotheses: status after run 4

| #       | Status                              | Rests on          | Not established                                                                                          |
| ------- | ----------------------------------- | ----------------- | -------------------------------------------------------------------------------------------------------- |
| H1      | **Confirmed on host**               | F32 (P6)          | session-to-session (probe containers were used); UDP                                                     |
| H2      | **Confirmed on host** (again)       | F25, P2 this run  | that sessions cannot find each other by a cheap sweep (they can)                                         |
| H3      | **No reachable listener, 3rd time** | F22, F26, F30     | that none ever appears; a loopback-only DB shows a service is one bind-flag away                         |
| H4      | **NOT settled: probe defect**       | F37 (P11 invalid) | everything about the leak rate; only matters if Task 2.2 creates networks                                |
| H5      | **Partly confirmed**                | F35 (P9)          | the API over HTTPS, host service connect, published ports (U7), a network with `isolate`/`--disable-dns` |
| Version | Answered passively                  | F24               | the empirical cross-network behaviour: P7 was confounded (F33)                                           |

Success criterion 1 ("H1 to H5 settled by a HOST run") is **not met**. H4 is open and H5 is
partial. Both matter **only** if Task 2.2 picks an option that creates networks (2 or 5), so
they become preconditions of Task 3.1, not blockers of the decision.

## Probe defects to fix before any re-run

1. **P7**: use a listener that keeps listening (BusyBox `nc -lk`, or a `while` loop). Add a
   same-bridge **positive control after** the cross-network attempt, so the listener's
   survival is shown.
2. **P11**: `--rm` on the kill container, run attached in the background, then kill the
   client, then kill conmon, checking the state after each. Mind the wrapper-pid trap when
   capturing the client pid.
3. **P12**: list the graph-root `networks/` directory (resolve it from `podman info`, not a
   guessed path). Print each network's `network_interface` so bridges map by name, not count.
4. **P10**: restore the `servers=` field.
