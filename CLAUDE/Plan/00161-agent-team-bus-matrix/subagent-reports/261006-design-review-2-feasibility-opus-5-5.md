# Plan 00161 design review 2: feasibility

Scope: revised DESIGN.md, PROTOCOL.md, PLAN.md, revision report, wave-1 branches.
All eight owner answers are reflected, except one ownership fact behind answer 8 (B2).

## Blockers

- **B1 The wake message can be silently dropped (DESIGN §6, PROTOCOL §15).** The spike
  found identical socket messages are dropped. A counts-only template repeats exactly: "1
  pending", then `recv`, then "1 pending" again. The second message is lost and the idle
  session never wakes. Fix: add a monotonic sequence number to the template (it carries no
  content). U01 must measure the dedupe window.
- **B2 `ccy.env.local.dist` is now ccy's, not the daemon's (§5.3, D18, U21, U23).**
  ccy 3.83.0 writes it from `ccy_env_local_dist_text` in `lib/common.bash`, versioned by
  `CCY_ENV_LOCAL_DIST_VERSION`. Fix: U21 adds a commented `PINGBUS_TEAMS` block there,
  raises the dist version and bumps CCY. Drop the daemon #88 request from U23. State that
  `ccy.env.local` is placed by the install's IaC, never by hand or by an agent (docs/ccy.md).
- **B3 The unit gives up when a WireGuard address comes up late (§3.5).**
  `Restart=on-failure` with the default start limit (5 starts in 10 s) leaves the unit
  failed for good. Fix: render `RestartSec=5s` and `StartLimitIntervalSec=0`, plus
  `After=`/`Wants=` the WireGuard device unit in the drop-in.
- **B4 `add-member` cannot write the bundle (§3.2, §4, §5.1).** The wrapper drops to
  `agent-bus`, which cannot write into a user's 0700 home or checkout, yet the bundle must
  be owned by the user who invoked it. Fix: the `agent-bus` side writes the bundle to stdout
  (that is its payload). The root wrapper writes it with `install -o $SUDO_UID -m 0600`.
- **B5 PLAN.md is out of date.** Phase 2 still lists U00-U30 and "M3 warden and control
  room", and a success criterion still reads "no agent can send free text". Fix: copy the
  §12 milestones into PLAN.md and use "no agent's free text reaches another agent". Commit
  this with the design (Plan Commit Rule).

## Probes to add before build

- **H4 `+` localparts.** Tuwunel checks usernames with `validate_strict`
  [tuwunel §3]. Create a real `x.1+h.podman` handle through both `v1/register` and
  `PUT v2/users`. If either refuses it, the handle grammar changes in U02.
- **H4/U16 unknown config keys.** Tuwunel warns on an unknown key and carries on, so a typo
  would quietly drop a security setting. Start Tuwunel with the exact rendered file and fail
  if the log has any unknown-key line.
- **H3 resolver stub.** On Fedora, `/etc/resolv.conf` is a symlink into
  `/run/systemd/resolve`, and §3.5 makes that directory inaccessible. Bind the stub onto
  `/run/systemd/resolve/stub-resolv.conf` instead, or put a `TemporaryFileSystem=` over the
  directory. Test this on Workstation and on Server.
- **U01 is not fully a container probe.** The wave-1 run had no child credential, so Stop
  never fired. The socket's "one turn starts" leg also needs a credentialed session. Mark U01
  as C plus a credentialed or host leg.
- **H7 phone.** Record whether the phone client sets `m.mentions`. If it does not, the
  human's text addresses no agent. Also check the TLS fallback itself: Android apps ignore
  user-installed CAs by default. The alternative is a public DNS-01 certificate for a name
  that resolves to a WireGuard address.
- **H1** must also cover ccy's named project networks (`--network <project>-network`, a
  rootless bridge), not only the default network and `--no-network`.

## Other findings

- **Watcher liveness (§6 guard, PROTOCOL §12).** `watch.pid` sits on the bind-mounted
  `/workspace`. PIDs mean nothing across container PID namespaces, and a restarted container
  reuses them. Fix: decide liveness with a non-blocking `flock` on `lock`.
- **Multi-team (§2, §6).** One watcher that polls each team in turn for 30 s adds up to 30 s
  of latency per extra team. Fix: long-poll every team at once (one thread per team, stdlib).
  Make busy a per-team result.
- **Installer idempotency (§3.2, §3.4 step 3).** "(re)start" on every play run restarts
  every homeserver. Fix: restart only on a `CHANGED` render, and skip the download when the
  pinned version directory's hash already matches.
- **PROTOCOL §7** says only humans can send `@room` "by power levels". The server does not
  enforce `m.mentions.room`; the sender-class check does. Change the wording.
- **U02 reuse (§12).** That branch turned PROTOCOL.md into a stub and wrote
  `docs/agent-team-bus-protocol.md`. Do not merge it. Take `protocol.py` and the tests from
  it, and rebuild the doc from the new PROTOCOL.md.
- **Sound as designed:** the system unit with an IP filter instead of Quadlet; pasta
  connecting from the host's own namespace (the source is the bus address, which is
  allowed); the docker, LXC and libvirt bridge paths; the `agent_bus` prefix; and room v12
  creator power with `@`-keyed status.

## For the coordinator: paste for the infra agent

In the infra project's IaC, place the untracked `<checkout>/.claude/ccy/ccy.env.local`:

```bash
# based on ccy.env.local.dist version 1
export HOOKS_DAEMON_HOSTNAME=<role>
```

Requires ccy 3.83.0 or later on that host. The daemon resolves `HOOKS_DAEMON_HOSTNAME`, then
`CCY_HOST_HOSTNAME`, then the hostname. Match `<role>` in `hosts:` in
`.claude/hooks-daemon.yaml`.
