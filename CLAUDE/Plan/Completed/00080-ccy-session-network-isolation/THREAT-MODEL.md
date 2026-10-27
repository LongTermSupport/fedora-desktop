# Plan 00080: threat model for the shared CCY bridge (Task 2.1)

This is written from host facts, not from documentation. F-numbers are in `PLAN.md`. The
per-probe evidence for run 4 (F29 to F38) is in
`subagent-reports/260924-triage-findings-opus-5.5.md`. The research's earlier draft
(`research/findings.md` §4) was written before any host probe. This document replaces it
as the decision input.

**Bottom line**: **little in practice today, and one bind flag away from something real.**
The path between sessions is open and measured (F32). Across three fleet snapshots, nothing
has been listening at the end of it (F22, F26, F30). The exposure is latent, not live.

## Scope

- **In**: what one CCY session on the default `podman` bridge can do to another session on
  that bridge.
- **Out** (non-goals of the plan): internet egress, container-to-host exposure, sessions that
  deliberately join a project network (`--network`, `--connect`), and the token model. All of
  these are covered elsewhere (`docs/ccy.md`'s security model, Plan 00068).

## Precondition: session A is already compromised

The attacker has code execution inside session A. This can come from prompt injection, a
malicious dependency, or a hostile page loaded by the browser tool. `docs/ccy.md` already
concedes that a compromised session can exfiltrate its **own** credentials. This model asks
only what the **shared bridge adds** to that, reaching into session B.

Two facts make the precondition cheap:

- sessions run `--dangerously-skip-permissions`, so no human sees the step where an injected
  instruction opens a socket to a neighbour;
- CCY has no control and no record of intra-bridge traffic (research F19: no per-session
  network, no `isolate`, no `icc`-style switch).

## What A can do to B over the bridge (measured)

| Capability                               | Status on this host         | Evidence                                                                                                                                                                       |
| ---------------------------------------- | --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Route to B's IP without the gateway      | **Yes**                     | F4: on-link route for the whole `/16`                                                                                                                                          |
| Open TCP to a port B binds on `0.0.0.0`  | **Yes, not filtered**       | F32 (P6 `REACHED`)                                                                                                                                                             |
| Resolve B by name                        | **No**                      | H2: `dns_enabled: false` (F25, P2 run 4)                                                                                                                                       |
| Find B without names                     | **Yes, cheaply**            | addresses are allocated low and close together in one `/16`, so a sweep is trivial                                                                                             |
| Reach anything B binds on loopback only  | **No**                      | loopback is per network namespace; F30's database is in this class                                                                                                             |
| Read B's env (tokens), mounts, SSH keys  | **No, not via the network** | these are process environment and bind mounts, with no network surface (research F16)                                                                                          |
| L2 tricks (ARP spoofing of B or gateway) | **Not probed**              | depends on `CAP_NET_RAW` in the session; CCY sets no cap flags, so Podman's defaults apply. B's egress is TLS (F5), which bounds the gain to disruption and plaintext metadata |

## What B currently exposes (measured)

- **Nothing reachable.** Three snapshots: 6 of 6, 6 of 6 and 5 of 5 sessions with no listener
  a neighbour can reach (F22, F26, F30).
- **One service that would be reachable if bound differently.** A session runs a database on
  its default port, bound to loopback on IPv4 and IPv6 (F26, F30). This is research threat
  case 3 ("a database a session started inside its own container") seen in real use. Its bind
  address is the only thing keeping it off the bridge.
- **The neighbours differ in project and in identity.** Everything on the bridge is a CCY
  session (F29). The 5 sessions are 5 different projects on two GitHub identities. So the
  bridge is a path across a project boundary, and in one pairing across an identity
  boundary. The sessions otherwise respect those boundaries: they have separate mounts and a
  token per session.

## What would become exposed, concretely, if B binds `0.0.0.0`

These are the listeners a coding session can plausibly start. None was observed. Each one
turns the latent path into a live one:

1. **A static or dev server over the project tree.** `python -m http.server` binds all
   interfaces by default. Frameworks' `--host` flags (Vite, Next and similar) and Docker-habit
   `0.0.0.0` binds do the same. What leaks: B's source, `.git/`, and any `.env` or credential
   file in the tree. These belong to a **different project**, and possibly a different
   identity, from A's.
2. **A database started inside B** with `listen_addresses='*'` or its equivalent. Dev
   databases usually run with default credentials or `trust` authentication. What leaks: read
   and write access to B's data.
3. **A browser debugging port** (Chrome DevTools Protocol). This gives full remote control of
   B's browser: pages, cookies, storage, JavaScript execution. Chromium binds it to loopback
   by default. It is serious only if something overrides that. Nothing has been observed to.
4. **Anything unauthenticated "because it is only localhost"**: admin routes, metrics
   endpoints, language servers, and so on.

In every case the gain is **lateral**: A reaches data or control belonging to B's project.
It is **not** escalation to B's tokens or SSH keys, which have no network surface. The
exception is where B's own service serves or holds them (case 1's `.env`, case 3's
cookies).

## What the bridge does not change

- **A lone session gains nothing.** The bridge only matters with 2 or more sessions, and the
  fleet usually has 5 or 6.
- **Host and internet exposure is identical** under every option. Out of scope.
- **Sessions that join a project network** sit with that project's services on purpose. They
  are unaffected by any change to the *default*.

## Weighting

The factors that push the risk **up**:

- the population is dense and valuable: 5 or 6 concurrent sessions, each with live push
  credentials, spread over several projects and more than one identity;
- nobody is watching (`--dangerously-skip-permissions`), and nothing would reveal a
  neighbour connection;
- the triggering condition is **one ordinary developer action**, a dev server started with
  `--host`, and it is invisible.

The factors that push it **down**:

- three snapshots have found **zero** reachable listeners. The one real service is bound to
  loopback;
- the most valuable assets (tokens, SSH keys) have **no network surface at all**;
- the attacker must already own session A. At that point A's own credentials are already
  lost, and the bridge adds only B's *served* data.
- `--no-network` already gives a session full isolation on demand (F2b), at the cost of
  `--connect`.

**The honest answer**: the shared bridge is a real path and currently leads nowhere. It turns
"any session that ever runs `0.0.0.0` dev tooling" into "reachable, without anyone knowing,
by every other session, across projects and identities". Whether that justifies building
something is Task 2.2's question. The options and their evidence are in `DECISIONS.md`.
