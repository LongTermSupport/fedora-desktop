# Plan 00080 — Research: CCY session network isolation

**Scope**: research only. No verdict is rendered here; the decision gate is Task 2.2.

**Environment constraint**: this was researched from inside a CCY container, where
`podman` is not installed and not reachable. **No runtime behaviour was observed.**
Every claim below is tagged with its source, and everything that would need a live
Podman to confirm is in [Unverified premises](#2-unverified-premises) with a probe in
[Probes needed on the HOST](#3-probes-needed-on-the-host).

Tag key: `[SOURCE: …]` = read from a file or an upstream document ·
`[VENDOR CLAIM]` = upstream's own statement about its behaviour, not independently
checked · `[INFERRED]` = reasoning over sourced facts · `[UNVERIFIED — needs host probe]`.

---

## 1. Facts

### A. What sharing the default `podman` bridge actually permits

**F1 — The rootless Podman default is *not* a bridge. It is pasta, and pasta isolates
containers from each other.**
"Pasta is the default network setup for rootless containers and pods." … "with pasta
networking, containers are isolated from each other by default. Unlike the bridge
approach, there is no virtual network."
[SOURCE: https://github.com/containers/podman/blob/main/docs/tutorials/basic_networking.md]
The `--network` option doc agrees: pasta "is the default for rootless containers and only
supported in rootless mode", while bridge "is the default for rootful containers".
[SOURCE: https://raw.githubusercontent.com/containers/podman/main/docs/source/markdown/options/network.md]
`containers.conf` sets `default_rootless_network_cmd = "pasta"`.
[SOURCE: https://raw.githubusercontent.com/containers/common/main/docs/containers.conf.5.md]

**F2 — The shared bridge is a CCY decision, not a Podman default.** When the engine is
Podman and `--no-network` was not given and no other network was selected, the launcher
*explicitly* sets the default network:

```bash
elif [[ "$CONTAINER_ENGINE" = "podman" ]] && [[ "$NO_NETWORK_MODE" != true ]]; then
    SELECTED_NETWORK="podman"
    NETWORK_FLAG="--network podman"
fi
```

[SOURCE: `files/var/local/claude-yolo/claude-yolo:2677-2680`]

> This corrects the premise currently written in `PLAN.md`'s Overview ("It is Podman's
> default for a container started with no network flag, not a choice CCY makes"). The
> opposite is the case: **without CCY's line 2678, a rootless CCY session would get pasta
> and would already be isolated.** This is the single most consequential finding in this
> report, because it means "self-scoped by default" is a matter of *undoing* an explicit
> choice, and the reason that choice was made (F3) is the thing that must be preserved.

**F3 — Why the bridge was chosen: `ccy --connect` does not work under pasta.** The line in
F2 was added in CCY 2.6.0 to fix `ccy --connect` failing with
`"pasta" is not supported: invalid network mode` — "Podman's default 'pasta' mode for
rootless containers doesn't support connecting to additional networks after container
start … Automatically use `--network podman` (default bridge network) … This ensures
containers can join additional networks later."
[SOURCE: git commit `ea7ba129`, `files/var/local/claude-yolo/claude-yolo`]
So the shared L2 domain is a **side effect of preserving `--connect`**, not a decision
about isolation. Any option that removes the bridge must answer for `--connect`.

**F4 — The default `podman` network has no DNS. H2 is confirmed on documentation.**
"The default 'podman' network with netavark is memory-only. It does not support dns
resolution because of backwards compatibility with Docker."
[SOURCE: https://github.com/containers/podman/blob/main/docs/tutorials/basic_networking.md]
The repo already encodes this: `ensure_network_dns()` returns early for the `podman`
network — "Skip for default podman network (uses pasta's DNS proxy, not aardvark-dns)".
[SOURCE: `files/var/local/claude-yolo/lib/network-management.bash:751-753`]
**Consequence**: cross-session reachability today is **by IP only**. One session cannot
resolve another by container name.

**F5 — User-created bridge networks have DNS *enabled* by default.** `--disable-dns`
"Disables the DNS plugin for this network which if enabled, can perform container to
container name resolution."
[SOURCE: https://man.archlinux.org/man/podman-network-create.1.en]
[VENDOR CLAIM, corroborated] "the default podman network has DNS disabled
(`dns_enabled: false`), user-created networks have DNS enabled by default".
[INFERRED] A per-session network (Option 2) would therefore be DNS-*enabled* — but with a
single member, so it grants no new name resolution to anyone.

**F6 — Containers on the same bridge network can initiate communications with each
other.** "Within a bridge network, containers can initiate communications with each other
and externally."
[SOURCE: https://github.com/containers/podman/blob/main/docs/tutorials/basic_networking.md]
This is H1, on documentation. It still needs a host probe (U2) because firewall state on
this specific machine is not derivable from docs.

**F7 — `isolate` is a *cross-network* control. It says nothing about traffic inside one
network.** The bridge driver option:

> `isolate`: This option isolates bridge networks from other bridge networks. …
> `strict`: Block traffic to and from all other bridge networks. This is the default when
> the option is omitted. `true`: Block traffic only between networks that also have
> isolation enabled (`true` or `strict`). `false`: Do not isolate the network; allow
> traffic to other bridge networks.

[SOURCE: https://man.archlinux.org/man/podman-network-create.1.en]
[INFERRED] There is no documented equivalent of Docker's
`com.docker.network.bridge.enable_icc=false` for netavark, so **`isolate` at any value does
not reduce intra-network reachability**. Putting `isolate=strict` on the shared `podman`
network would therefore do nothing for this plan's problem. This answers the brief's
"be precise" question directly.

**F8 — Strict isolation is new. Which side of the change this host is on decides
everything about Option 2.** Netavark 2.0 (required by, and only supported with, Podman
6.0): "The bridge network driver now defaults to strict isolation mode, this means
different networks can no longer talk to each other by default. To restore the previous
behavior the network must set the `isolate=false` option."
[VENDOR CLAIM: netavark 2.0 release notes, via
https://github.com/containers/netavark/releases]
Before that change, cross-network traffic was *permitted* even between distinct rootless
bridge networks: containers/podman#26913 reports, on **rootless** Podman 5.6.0 with
netavark 1.16.1, that a container on `test1` could be reached from `test2`, with the
FORWARD chain showing `ACCEPT all -- anywhere 10.89.0.x/24` and `… 10.89.1.x/24`.
[SOURCE: https://github.com/containers/podman/issues/26913]
[INFERRED] **On a pre-6.0 Podman, a per-session network would create the appearance of
isolation without the substance** unless CCY explicitly passes `--opt isolate=…`. The host's
Podman/netavark version is U1 and is the gating unknown.

**F9 — Strict isolation has a documented side-effect on published ports.** netavark#709:
with isolation, "I can't access the published port from a container in another isolated
network which is something that does work with docker where networks are isolated by
default" — the host could reach the port, a container on another isolated network could
not. Closed by PR #1483.
[SOURCE: https://github.com/containers/netavark/issues/709]
[INFERRED] Relevant because a plausible current CCY workflow is "agent in session A talks
to a compose service via its published host port". Whether that still works under a
per-session isolated network on *this* Podman version is U7.

**F10 — Address space is not a constraint.** `default_network = "podman"`,
`default_subnet = "10.88.x.x/16"`, and `default_subnet_pools` =
`10.89.x.x/16`@/24, `10.90.x.x/15`@/24, `10.92.x.x/14`@/24, `10.96.x.x/11`@/24,
`10.128.x.x/9`@/24. (Final octets are written `x` throughout this document: these are
upstream Podman defaults, not host addresses, but the repo's pre-commit secret scanner
rejects any literal `10.a.b.c` — see `CLAUDE/ExampleValues.md`.)
[SOURCE: https://raw.githubusercontent.com/containers/common/main/docs/containers.conf.5.md]
[INFERRED] That is 256 + 512 + 1,024 + 8,192 + 32,768 ≈ **42,752 allocatable /24s**. A leak
rate of one network per crash cannot exhaust this in any realistic timeframe.
Subnet-pool exhaustion should be **removed as a decision input**; the plan's risk table
currently lists it and it does not deserve the row. Interface-name space is a separate
question (U8) but is bounded by the same count.

### B. What CCY does today

**F11 — Network selection order in the launcher** [SOURCE: `claude-yolo:1947-2667`]:

1. `--network NET` (or a `NETWORK_FROM_CONFIG` value restored from the saved launch
   config): verified to exist, compose services optionally started, then
   `NETWORK_FLAG="--network NET"`. A missing network with an explicit flag is a hard
   error and exits 1 (`claude-yolo:2036-2042`).
2. `--no-network`: prints "Skipping network connection" and leaves `NETWORK_FLAG`
   **empty** (`claude-yolo:2044-2047`).
3. Otherwise: saved per-project network preference
   (`~/.claude-tokens/ccy/projects/<sha256-prefix>/network`,
   `network-management.bash:70-101`) → else auto-detect a network whose name contains the
   project name → else offer compose start → else nothing.
4. Fallback (F2): Podman + not `--no-network` ⇒ `--network podman`.

**F12 — `ccy --no-network` is already an isolation mode, and it is misnamed.** With
`NETWORK_FLAG` empty the container runs with no `--network` flag at all, so it takes the
rootless default = pasta (F1) — full outbound internet, no shared L2 domain.
[INFERRED from `claude-yolo:2044-2047` + F1] The costs are that `--connect` will not work
afterwards (F3) and the internet preflight is skipped (F13).

**F13 — Every launch with a network runs a preflight container.**
`podman run --rm --network "$SELECTED_NETWORK" alpine wget -q -O- --timeout=10 http://google.com`,
and a failure exits 1 with a debugging block.
[SOURCE: `claude-yolo:2698-2705`] Skippable via `CCY_SKIP_NETWORK_PREFLIGHT=1`.
[INFERRED] Under Option 2 the per-session network must exist *before* this line, and the
preflight becomes a test of a brand-new network on every single launch.

**F14 — `ensure_network_dns()` mutates any DNS-enabled network that has no DNS servers,
adding `1.1.1.1` and `8.8.8.8`.** It is called for the selected network on every launch
and skips only the `podman` network.
[SOURCE: `files/var/local/claude-yolo/lib/network-management.bash:743-802`;
call site `claude-yolo:2683-2685`]
[INFERRED] A per-session network is DNS-enabled by default (F5), so **Option 2 would
silently start pointing every CCY session's DNS at two public resolvers**, where today the
default-network path is skipped and resolution goes via pasta's DNS proxy. That is a
behaviour change with a privacy dimension and it is not obvious from the diff that would
implement Option 2. Either pass `--disable-dns` at create time or extend the skip.

**F15 — Nothing in a CCY session is published to the host.** The run invocation has no
`-p`/`--publish` at all; the only `-p` in the file is Claude Code's own headless prompt
flag (`claude -p "$PROMPT_TEXT"`, `claude-yolo:2804`).
[SOURCE: `claude-yolo:2988-3015`, `claude-yolo:2804`]
[INFERRED] So a listener inside a session is reachable **only** from other containers on
the same bridge. The shared bridge is not merely one exposure among many — for a CCY
session it is *the* network exposure surface.

**F16 — Run invocation, for reference** [SOURCE: `claude-yolo:2988-3015`]: `--rm`,
`--name`, five `ccy*` labels, `$NETWORK_FLAG` (line 2995), `--device /dev/dri`, the mount
arrays, then `-e CLAUDE_CODE_OAUTH_TOKEN`, `-e GH_TOKEN`, and the rest, ending in
`claude --dangerously-skip-permissions`. Tokens are environment variables; SSH keys are
read-only mounts; the project tree is a read-write mount at `/workspace`
(`claude-yolo:1923-1926`).

**F17 — Multiple concurrent sessions per project are normal.**
`get_next_container_name()` suffixes a counter when `<project>_yolo` already exists,
counting stopped containers too.
[SOURCE: `files/var/local/claude-yolo/lib/common.bash:599-617`]

**F18 — A crash-cleanup hook already exists at launch.**
`clean_stale_containers_startup "yolo"` runs early — "Clean up containers left over from
unclean shutdowns (battery death, crash) … stopped/dead containers where `--rm` never
fired — auto-removed".
[SOURCE: `claude-yolo:1282-1284`; implementation
`files/var/local/claude-yolo/lib/docker-health.bash:288+`]
[INFERRED] This is the natural place to hang a stale-*network* reaper, and its existence
means the "CCY has no crash-recovery path" objection to Option 2 is not true today.

**F19 — Nothing in CCY isolates sessions from each other today**, beyond what pasta would
have done had F2 not overridden it. There is no per-session network, no `isolate` option
passed anywhere, and no `icc`-style control.
[SOURCE: absence of any `network create` or `--opt` in `claude-yolo` and
`lib/network-management.bash` — the launcher only ever *selects* or *connects to*
pre-existing networks]

### C. Cleanup mechanics (the decisive risk for Option 2)

**F20 — `podman network rm` refuses while the network is in use.** Exit status **2** is
"The network is in use by a container or a Pod". `--force` "removes all containers that
use the named network. If the container is running, the container is stopped and
removed."
[SOURCE: https://docs.podman.io/en/latest/markdown/podman-network-rm.1.html]
[INFERRED] A blind `--force` in a CCY exit path would be a container-killing primitive,
not a cleanup primitive — dangerous if the network name ever collides.

**F21 — `podman network prune` removes "a network which has no containers connected or
configured to connect to it", and never removes the default `podman` network.**
[SOURCE: https://docs.podman.io/en/latest/markdown/podman-network-prune.1.html]
[INFERRED] Three consequences. (a) A leaked *network* whose container was `--rm`'d away is
prunable. (b) A leaked network whose **container also leaked** (power loss, `--rm` never
fired — the exact case F18 exists for) is **not** prunable, because a stopped container is
still "configured to connect". Order matters: reap the container first, then the network.
(c) A blanket `podman network prune` is unsafe for CCY to run automatically — it would
also remove a user's empty compose networks.

**F22 — Rootless bridge networks live in one shared "rootless network namespace".** "When
rootless containers are run, network operations will be executed inside an extra network
namespace", joinable with `podman unshare --rootless-netns`.
[SOURCE: https://github.com/containers/podman/blob/main/docs/tutorials/basic_networking.md]
[INFERRED] This is why F8 matters so much: separate bridges are separate subnets inside
*one* namespace with IP forwarding, so isolation between them is a firewall-rule property,
not a namespace property.

### D. The `podfreeze` consequence

**F23 — `podfreeze` builds its network menu from the container inventory, not from
`podman network ls`, so it only offers networks something is attached to.**
[SOURCE: `files/home/.local/bin/podfreeze:320-346`, `build_network_map()`] Its own header
already names the failure mode: "a group row says 'network: podman — FREEZE 5' and names
nothing, which is precisely where a live Claude session hides inside an app network".
[SOURCE: `files/home/.local/bin/podfreeze:746-747`]
[INFERRED] Per option:

- **Status quo / Option 6** → the `podman` row stays ≈ "every session that did not join a
  project network". Worth relabelling; low signal but not wrong.
- **Option 2 (per-session)** → the `podman` row loses its CCY members and the menu grows
  one single-member row per live session. That is strictly worse as a *grouping* menu (a
  group of one is not a group), though `--ccy` and the `ccy-project` label already give
  the useful grouping. The row does not "dissolve" so much as **fragment**.
- **Option 4 (pasta default)** → CCY sessions appear on **no** network row at all. They
  would be selectable only via `--ccy` / label filters. The `podman` row would then
  contain only genuinely non-CCY containers, which is the cleanest outcome for
  `podfreeze`.

---

## 2. Unverified premises

These are the claims the analysis leans on that could **not** be confirmed from inside the
container. Treat every one as capable of overturning a conclusion above.

| #       | Premise                                                                                                                                       | Why it matters                                                                                                                        |
| ------- | --------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| **U1**  | The host's Podman and netavark major versions. Everything about F8 (isolation default) turns on ≥ 6.0 / ≥ 2.0.                                | **Gating.** Pre-6.0, Option 2 needs an explicit `--opt isolate=` or it isolates nothing.                                               |
| **U2**  | That two containers on the shared `podman` bridge on **this** host can actually open a TCP connection to each other (H1).                     | The whole premise of the plan. Documented (F6) is not observed.                                                                       |
| **U3**  | What a CCY session actually **listens** on, and on which address (H3). No repo evidence exists for `agent-browser`, dev servers, or anything. | If everything binds `127.0.0.1`, F6 is inert and the honest answer is "no exposure". **Most important unknown after U1.**              |
| **U4**  | Whether `--rm` fires when the `podman run` process is `SIGKILL`ed, OOM-killed, or the machine loses power.                                    | Sets the leak rate for Option 2 and decides whether F21(b) is the common case or the rare one.                                        |
| **U5**  | That a per-session bridge reaches the internet and `host.containers.internal` identically to the shared `podman` bridge (H5).                 | If not, Option 2 breaks Claude Code itself. Partially self-probed by F13's own preflight.                                             |
| **U6**  | That `podman network connect` works on a container already attached to a *user-created* bridge (i.e. `--connect` survives Option 2).          | Option 2's entire justification for keeping a bridge at all (F3).                                                                     |
| **U7**  | That a container on an isolated per-session network can still reach a compose service's **published** host port (F9 / netavark#709 / PR 1483). | A silent regression for the "agent talks to my app" workflow.                                                                         |
| **U8**  | Where rootless network configs are stored, whether a leaked config also leaks a bridge interface, and the practical cap on interface names.   | Sizes the leak's real cost. F10 rules out subnet exhaustion; ifname space is separate.                                                |
| **U9**  | Whether `ensure_network_dns()` would in fact fire on a fresh per-session network and add `1.1.1.1`/`8.8.8.8` (F14 is a code reading).         | A behaviour change nobody asked for, easy to ship accidentally.                                                                       |
| **U10** | What else is on the shared `podman` bridge besides CCY sessions (compose leftovers, ad-hoc `podman run`, devtools containers).                | Determines the true blast radius; seven sessions was a count of CCY, not of members.                                                  |
| **U11** | Whether the seven observed sessions were the same project / same tokens / same SSH keys.                                                      | Sessions sharing one identity have far less to take from each other.                                                                  |
| **U12** | Per-launch latency cost of `network create` plus the F13 preflight against a cold network.                                                    | Option 2's ongoing UX tax, paid on every launch.                                                                                      |
| **U13** | Whether anything on the host (scripts, docs, muscle memory) depends on CCY sessions being addressable on the `podman` bridge.                 | An isolation change would break it silently.                                                                                          |

---

## 3. Probes needed on the HOST

Read-only, re-runnable, non-destructive — suitable for this plan's `triage.bash` per
`CLAUDE/PlanTriage.md`. **P6–P12 create and then remove throwaway containers and one
throwaway network**, so they are the one active section and belong behind an explicit
`--reachability` flag, not in the passive default run. Nothing here touches a live
session except `podman exec` in P4, which only reads.

### Passive (default run)

```bash
# P1 (U1) — versions and backend. Decides whether isolation is default-strict (F8).
podman --version
podman info --format '{{.Host.NetworkBackend}}'
podman info --format '{{.Host.NetworkBackendInfo.Backend}} {{.Host.NetworkBackendInfo.Version}}'
podman info --format '{{.Host.NetworkBackendInfo.DNS.Package}} {{.Host.NetworkBackendInfo.DNS.Version}}'
podman info --format '{{.Host.RootlessNetworkCmd}}'
rpm -q podman netavark aardvark-dns

# P2 (F4, U1) — the default network's real config: DNS off? isolate? subnet?
podman network inspect podman

# P3 (U10, U11) — who is actually on the shared bridge right now. The second command
#   re-states it with the CCY labels, so "seven sessions" can be decomposed into
#   projects/identities rather than merely counted.
podman ps --all --filter network=podman \
    --format '{{.Names}}\t{{.State}}\t{{.Image}}'
podman ps --all --filter network=podman \
    --format '{{.Names}}\t{{.Labels.ccy}}\t{{.Labels.ccy-project}}\t{{.Labels.ccy-github}}'

# P4 (U3) — THE decisive probe. What does a live CCY session listen on?
#   Run for each live session name from P3. 0.0.0.0/:: is reachable from the bridge;
#   127.0.0.1 is not. (ss may be absent from the image — the /proc fallback always works.)
podman exec <session_name> sh -c 'ss -lntp || cat /proc/net/tcp /proc/net/tcp6'

# P5 (U10) — every network and its member count, for blast-radius composition.
podman network ls --format '{{.Name}}\t{{.Driver}}\t{{.NetworkID}}'
for n in $(podman network ls --format '{{.Name}}'); do
    printf '%s\t%s\n' "$n" "$(podman network inspect "$n" --format '{{len .Containers}}')"
done
```

### Active (behind `--reachability`)

```bash
# P6 (U2, H1) — can two containers on the shared bridge reach each other's TCP port?
#   The listener binds 0.0.0.0 deliberately: this measures the NETWORK, not a bind choice.
podman run -d --rm --name ccy80-probe-listener --network podman \
    docker.io/library/alpine sh -c 'nc -l -p 8080 -e echo REACHED'
LISTENER_IP="$(podman inspect ccy80-probe-listener \
    --format '{{.NetworkSettings.Networks.podman.IPAddress}}')"
podman run --rm --network podman docker.io/library/alpine \
    sh -c "nc -w 3 $LISTENER_IP 8080"      # prints REACHED  =>  H1 confirmed

# P7 (U1, F7, F8) — is a SECOND network isolated from the first by default?
#   Reaching across means this host predates the strict-isolation default, and Option 2
#   must pass --opt isolate= explicitly. Run BEFORE removing the listener.
podman network create ccy80-probe-net
podman run --rm --network ccy80-probe-net docker.io/library/alpine \
    sh -c "nc -w 3 $LISTENER_IP 8080"
podman rm -f ccy80-probe-listener

# P8 (U6) — does `network connect` work on a container on a user-created bridge?
podman run -d --rm --name ccy80-probe-connect --network ccy80-probe-net \
    docker.io/library/alpine sleep 60
podman network connect podman ccy80-probe-connect ; echo "connect rc=$?"
podman rm -f ccy80-probe-connect

# P9 (U5) — internet + host reachability from a fresh per-session-style network.
podman run --rm --network ccy80-probe-net docker.io/library/alpine \
    sh -c 'wget -q -O- --timeout=10 http://google.com >/dev/null && echo EGRESS-OK; \
           getent hosts host.containers.internal || echo NO-HOST-ALIAS'

# P10 (U9) — would a fresh network be DNS-enabled (so F14 would add 1.1.1.1/8.8.8.8)?
podman network inspect ccy80-probe-net \
    --format 'dns={{.DNSEnabled}} servers={{json .NetworkDNSServers}} opts={{json .Options}}'

# P11 (U4, F20, F21b) — does --rm survive SIGKILL of the podman CLIENT process?
podman run -d --rm --name ccy80-probe-kill --network ccy80-probe-net \
    docker.io/library/alpine sleep 300
pkill -KILL -f 'podman .*ccy80-probe-kill'
podman ps --all --filter name=ccy80-probe-kill --format '{{.Names}}\t{{.State}}'
podman network rm ccy80-probe-net ; echo "network rm rc=$? (2 => still in use, F20/F21b)"
podman rm -f ccy80-probe-kill
podman network rm ccy80-probe-net

# P12 (U8) — where network configs live, and whether a leak leaves an interface behind.
ls -la "${XDG_CONFIG_HOME:-$HOME/.config}/containers/networks/"
ls -la "$HOME/.local/share/containers/networks/"
podman unshare --rootless-netns ip -br link show

# P13 (U7) — can a container on an isolated network reach another container's PUBLISHED
#   host port? (netavark#709.) Needs a real published-port container; if none is up, the
#   probe must FAIL with "no published-port container found" rather than print nothing.
```

**Cleanup contract**: every probe object is named `ccy80-probe-*`, so the script can
finish by asserting that `podman ps -a --filter name=ccy80-probe-` and
`podman network ls --filter name=ccy80-probe-` are both empty, and **say so** rather than
assuming its own `rm` calls worked.

---

## 4. Threat model

### What cross-session reachability actually gains an attacker

The precondition is that **one session is already compromised** — by prompt injection, a
malicious dependency, or a hostile page fetched by the browser tool. That is not a
stretch: `docs/ccy.md` names it explicitly ("not a sandbox against … a prompt-injection or
supply-chain attack that exfiltrates the credentials it is given")
[SOURCE: `docs/ccy.md:145-152`]. The question here is only what the *second* session adds.

Given a compromised session A on the shared bridge, the gain is **lateral reach to
whatever session B is listening on `0.0.0.0`**. Concretely, in descending order of
severity:

1. **A browser/debugger control port.** A Chrome DevTools Protocol endpoint reachable over
   the network is a full remote-control primitive — read any page, read cookies and local
   storage, navigate to `file://`, execute JavaScript. If anything in the CCY image ever
   binds a CDP port to `0.0.0.0`, that is the finding, and it is a serious one. **This is
   U3 and it is unverified in both directions.** Chromium's default is `127.0.0.1`, which
   would make it a non-issue; the repo contains no evidence either way.
2. **A dev server serving session B's project tree.** Reads of source, `.env` files served
   as static assets, admin routes with no auth because "it is only localhost". Read-only in
   practice, but potentially credential-bearing.
3. **A database or service B started inside its own container.** Usually
   default-credentialed, because it was never meant to leave localhost.
4. **Scanning.** With no DNS (F4), A must sweep the `/16` for live IPs and open ports. That
   is trivially cheap and completely unlogged.

### What it does **not** gain

This is the more important half, and the honest answer is: **most of what matters is not on
the network at all.**

- **Not the Anthropic OAuth token or the `gh` token.** Both are environment variables of
  the target container's own process tree (F16). Reaching an HTTP port does not read
  another container's `/proc/*/environ`; that would require code execution *inside* B.
- **Not the SSH private keys.** Read-only bind mounts inside B's filesystem namespace. Not
  network-reachable.
- **Not the project tree.** A bind mount, not a share. Reachable only if B happens to be
  serving it over HTTP (case 2 above).
- **Not the host.** Container-to-host exposure is unchanged by this; the sessions share an
  L2 domain with each other, not with the host's services.
- **Not an escalation for a *single* session.** A lone CCY session on the shared bridge
  with nothing else attached has nothing to reach.

### Honest weighting

Without U3, the honest position is: **the shared bridge converts one compromised session
into a foothold against the network-facing surface of its neighbours, and that surface is
plausibly empty most of the time.** Three things push the risk up rather than down, and
they are why the question deserves an answer rather than a shrug:

- The population is unusually valuable per host. Seven simultaneous sessions each holding
  live push credentials is not a normal container fleet.
- Sessions are *unattended by design* — `--dangerously-skip-permissions` (F16) means
  nobody is watching the moment an injected instruction opens a socket to a neighbour.
- The cost of removing the exposure is now known to be small (F2/F12): it is a flag, not an
  architecture.

Set against that: nothing in this report demonstrates a *reachable listener* today. The
finding that would settle it in either direction is P4, and it is one command.

Two things are **not** part of this threat model and should not be smuggled in: outbound
internet reach (explicitly a non-goal of this plan) and container-to-host exposure
(covered by the existing security model in `docs/ccy.md`).

---

## 5. Options

| # | Option                                                   | What it changes                                                                                                             | What it costs                                                                                                                                                                          | What it risks                                                                                                                                                                                                | On a crash                                                                                                                                                                                                                | `podfreeze` consequence                                    |
| - | -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| 1 | **Status quo** — shared `podman` bridge (F2)             | Nothing                                                                                                                     | None                                                                                                                                                                                   | Every session in one L2 domain (F6); severity depends entirely on U3                                                                                                                                         | Nothing to leak; the existing reaper handles the container (F18)                                                                                                                                                          | `podman` row stays; relabel it (one line)                  |
| 2 | **Per-session network** — create + attach + remove       | One bridge per session, DNS-enabled by default (F5), e.g. `ccy-<project>-<n>`                                                | `network create`/`rm` per launch and exit; the F13 preflight now runs against a cold network every launch (U12); `ensure_network_dns` would add public resolvers unless handled (F14/U9) | **Isolates nothing on pre-6.0 Podman unless `--opt isolate=` is passed (F8/U1).** May break access to other containers' published ports (F9/U7). `--connect` assumed to still work (U6)                       | The leak case. `--rm` covers the container only; a killed client may leave both, and a leftover *stopped* container makes the network un-prunable (F21b/U4). Needs a reaper hung off F18. Subnet exhaustion is NOT a real risk (F10) | Fragments into one single-member row per live session       |
| 3 | **`--network none`**                                     | No interfaces at all                                                                                                        | —                                                                                                                                                                                      | **Non-starter, stated explicitly**: "the container has no network connectivity" [SOURCE: podman `--network` docs] ⇒ no `api.anthropic.com`, so Claude Code cannot function; the F13 preflight would fail first | n/a                                                                                                                                                                                                                       | n/a                                                        |
| 4 | **Pasta default** — delete the F2 override               | Reverts to Podman's own rootless default; containers isolated from each other by design (F1), full outbound retained        | Zero implementation. `--network <compose-net>` at launch still works                                                                                                                   | **Breaks `ccy --connect`** — the exact bug F3 fixed. A real, used workflow (second terminal, mid-session DB access)                                                                                           | Nothing to leak — no object is ever created                                                                                                                                                                               | CCY sessions vanish from every network row; cleanest menu   |
| 5 | **Per-*project* network** (sessions of one project share) | Cross-project isolation, intra-project sharing                                                                              | Same machinery as 2, fewer objects; needs a "last session out" lifecycle rule                                                                                                          | Same F8/U1 dependency as 2; weaker isolation than 2 for the same complexity                                                                                                                                  | Leaks fewer networks than 2, but "when is it safe to remove" is harder to answer                                                                                                                                          | One row per project — arguably the most useful menu        |
| 6 | **`isolate=strict` on the shared `podman` network**      | Cross-*network* isolation only                                                                                              | Trivial                                                                                                                                                                                | **Does not address this plan's problem at all** (F7) — intra-network reachability is untouched. Listed so it is ruled out on evidence rather than left open                                                   | n/a                                                                                                                                                                                                                       | No change                                                  |
| 7 | **`--network host`**                                     | Container shares the host netns                                                                                             | —                                                                                                                                                                                      | Strictly worse — full access to host loopback services. Rejected                                                                                                                                             | n/a                                                                                                                                                                                                                       | n/a                                                        |
| 8 | **`--internal` per-session network**                     | Bridge with external access restricted [SOURCE: podman-network-create]                                                      | —                                                                                                                                                                                      | Same non-starter as 3, for the same reason — no egress, no Anthropic API. CCY's own preflight already warns about `internal: true` in compose files (`claude-yolo:2732-2736`)                                 | n/a                                                                                                                                                                                                                       | n/a                                                        |

**The shape of the trade** is that Options 2 and 4 both deliver isolation and differ almost
entirely on `--connect`: Option 4 is free and loses it; Option 2 costs a lifecycle and
keeps it. Option 2 is essentially "keep F3's benefit, drop F3's side effect".

---

## 6. Open questions for the human

1. **How much is `ccy --connect` worth?** It is the sole reason the shared bridge exists
   (F3). If mid-session attach is rare, Option 4 is free isolation and a *deletion*. If it
   is routine, Option 2 is the only way to keep it. Nothing in the repo records how often
   it is used.
2. **Is per-session the right grain, or per-project (Option 5)?** Two sessions on the same
   project already share the same tokens, the same SSH key and the same working tree —
   isolating them from each other buys little, and Option 5 halves the object churn.
3. **Should implementation wait on P4?** If nothing in a CCY session binds `0.0.0.0`, the
   honest write-up is "exposure is theoretical", and Option 1 plus a documented note may be
   the proportionate answer. P4 is one command and would reframe the whole plan.
4. **If per-session networks land, what should the reaper's policy be?** Sweep `ccy-*`
   networks with zero containers at launch (safe, bounded, reuses F18's existing hook); or
   an exit trap (does not survive SIGKILL); or both. A blanket `podman network prune` is
   **not** safe — it would take the user's empty compose networks too (F21c).
5. **Is the DNS behaviour change (F14) acceptable?** A per-session network is DNS-enabled,
   so today's code would add `1.1.1.1`/`8.8.8.8` to every session. Pass `--disable-dns` at
   create time, extend `ensure_network_dns`'s skip, or accept it deliberately.
6. **The `PLAN.md` Overview needs correcting before Phase 2.** It states the shared bridge
   is Podman's default rather than CCY's explicit choice; F2 and F3 say otherwise, and the
   decision reads differently once that is fixed. The Risks table's subnet-exhaustion row
   should also go (F10).

---

## Sources

- [Podman basic networking tutorial](https://github.com/containers/podman/blob/main/docs/tutorials/basic_networking.md)
- [`--network` option documentation](https://raw.githubusercontent.com/containers/podman/main/docs/source/markdown/options/network.md)
- [`podman network create` (man)](https://man.archlinux.org/man/podman-network-create.1.en) · [docs.podman.io copy](https://docs.podman.io/en/latest/markdown/podman-network-create.1.html)
- [`podman network rm`](https://docs.podman.io/en/latest/markdown/podman-network-rm.1.html)
- [`podman network prune`](https://docs.podman.io/en/latest/markdown/podman-network-prune.1.html)
- [`containers.conf(5)`](https://raw.githubusercontent.com/containers/common/main/docs/containers.conf.5.md)
- [netavark releases / 2.0 notes](https://github.com/containers/netavark/releases)
- [containers/podman#26913 — cross-network traffic accepted, rootless 5.6.0](https://github.com/containers/podman/issues/26913)
- [containers/netavark#709 — isolation blocks access to published ports](https://github.com/containers/netavark/issues/709)
- Repo: `files/var/local/claude-yolo/claude-yolo`, `files/var/local/claude-yolo/lib/network-management.bash`, `files/var/local/claude-yolo/lib/common.bash`, `files/var/local/claude-yolo/lib/docker-health.bash`, `files/home/.local/bin/podfreeze`, `docs/ccy.md`, git commit `ea7ba129`
