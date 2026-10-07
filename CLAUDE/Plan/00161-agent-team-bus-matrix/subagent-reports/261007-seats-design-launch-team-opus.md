# Plan 00161: seats design, team given at launch

Design only; nothing is built. The owner answered the rework's three questions on
2026-10-07 (A, B, C below). Answer A removes the checkout-level opt-in the rework had
introduced. DESIGN.md now reflects this in: the v1 scope note at the top, §1, §2, §3.3,
§5.1-§5.3, §5.5, §5.6 (rewritten), §9, §10, §11, §12 (U19, U20, U24, U27, U31, U32, waves,
milestones M2 and M3, section "U20"), D18, D28, D33, D34, D36, D41, D42, the new D44-D46,
and "Owner questions". PLAN.md: the overview, Non-Goals, Tasks 1.8 and 1.9, M2, M3 and the
success criteria.

## The answers and where they landed

| Answer                                                                           | Design                                                                                                                                                                                                                                                                                                                      |
| -------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A. A session is in a team only when launched with it; seat creation stays in ccy | D44: `ccy --team <team>` (repeatable) `[--seat <seat>]`. A plain `ccy` is in no team in any checkout. `--seat` without `--team` is exit 64. With `--team` and no `--seat`, ccy takes the lowest free numbered seat and creates the next one if none is free. The launcher passes the teams and the seat into the container. |
| B. Every seat in every team the checkout joined: "yes for v1"                    | Re-read under A as D45: a seat is one name and one `PINGBUS_HOME` across teams. In each team it has its own account under the same handle. It holds one seat lock, and a launch adds the bundle for any team it names that the seat lacks.                                                                                  |
| C. v1 = teams on this laptop; cross-machine later                                | D46: U24 and success criterion 3 move to a later phase. Their design is kept and marked "later phase". U23 (LXC, docker, VM on this host) stays in v1.                                                                                                                                                                      |

## Flags (A)

- `--team <team>`, repeatable, maps to `PINGBUS_TEAMS=a,b`. Each team must have its
  homeserver active on this host, or the launch is refused naming the play or installer.
- `--seat <seat>` without `--team` is a usage error (exit 64) before anything runs. Ignoring
  it would start a session the human believes is on the bus, which is the banned
  skip-and-warn pattern.
- `--team` without `--seat` takes the lowest free numbered seat of the checkout, whatever
  teams that seat already has, and creates the next number when all are held. The rework's
  question 1 (create or refuse) is settled by A: the human asked for the team, so ccy
  creates a seat.
- Neither flag is a Quick Launch choice, and neither is read from the checkout. Restart and
  restore arguments carry `--team ... --seat <picked>`, so a plain session comes back plain.
- `--no-bus` is dropped: a launch without `--team` is already off the bus.

## Is a checkout-level opt-in still needed? No (YAGNI)

The following are removed: `agent-bus join` and `leave`, `PINGBUS_TEAMS` and `PINGBUS_HOST`
in `ccy.env.local`, `agent-bus seat add`, and U19's commented `PINGBUS_TEAMS` block in ccy's
dist text (U31 removes it in dist version 3). The reason: a recorded team list would only
repeat what the launch already says, and it could disagree with the flag. Nothing the bus
needs is written into a checkout any more.

What is left on the host is `agent-bus seat take` (the launcher's call), `seat list` and
`seat remove`. `seat remove` also deletes `seats/` and `.claude/ccy/pingbus/` once they are
empty, so removing the last seat leaves the checkout as it was. A role other than `worker` is
set with the existing `sudo agent-bus set-role`.

## A session never changes its own team or seat

- The teams and the seat are decided by the host launcher from its flags and passed with
  `podman run -e`.
- The entrypoint records the bus variables before it sources `ccy.env` (tracked, so
  writable by the session) and `ccy.env.local`. If either file set or changed one of them,
  it refuses to start.
- The seat handle's `<host>` was `PINGBUS_HOST`, which is gone. It is now the
  `HOOKS_DAEMON_HOSTNAME` that `ccy.env.local` assigns. The launcher reads it by parsing the
  file, never by sourcing it. ccy binds that file read-only since 3.84.0. The hooks daemon in
  the session already names itself from the same line, so the handle and the session's role
  agree.
- One gap remains, recorded in §9: in a checkout with no `ccy.env.local`, a session could
  create one, and its value would label a seat that a later launch creates for the first time.
  Two things limit this. The launcher prints each handle before it creates the account. Once a
  seat has handles, a different `<host>` is refused for it.

## Why one seat across teams (B)

pingbus already models a member in several teams: one `PINGBUS_HOME`, one directory per team,
and one watcher with a thread per team. A seat per (team, name) would give a two-team session
two homes, two locks and perhaps two numbers, and nothing in v1 needs that. Under one identity
the durable role (D40) stays a single thing.

A seat's bundle for a team that the current launch does not name sits idle. When a later
launch names that team, it resumes from the saved sync position, so the role's pending
traffic is still delivered. A bundle for a team that has since been purged does no harm until
a launch names the team; then `config check` refuses it and names `agent-bus seat remove`.

## U20 under the new design

- **No setup step.** M2.0 is now the first launch into each seat: `acca`, `accb` and `accc`
  are created by `ccy --team acceptance --seat <name>` with `sudo -n`, after the harness has
  run `sudo -v`. The harness then runs `sudo agent-bus set-role ... orchestrator` for `acca`
  before any session gets its orders.
- **New checks.** M2.5 adds `--seat` without `--team` (exit 64, nothing changed). The new
  M2.10 launches a plain `ccy` in the same checkout. The transcript's tool result shows
  `pingbus` not found (127), the `init` line lists no pingbus plugin, and the seat list and
  member list are unchanged. M2.11 is the checkout-unchanged check. `ccy.env.local` is also
  checked unchanged at M2.0, because no step writes to it.
- **Cleanup.** `agent-bus seat remove` runs for `acca`, `accb`, `accc`, `1` and `2`, then
  the team is purged.
- **Owner prerequisites.** Two change:
  - The checkout's `ccy.env.local` must assign `HOOKS_DAEMON_HOSTNAME`.
  - The owner's plain `ccy` sessions no longer have to stay out of the checkout during the
    run. Only a `ccy --team acceptance` launch would interfere.
- **Handles.** They now carry the checkout's own role in place of the literal `acceptance`.
  The evidence stays under `untracked/plan-runs/`.

## Scope (C)

- U24 is marked as a later phase. It is in no v1 wave, and U27 now needs U23 instead of U24.
- M3 covers U21-U23.
- §10 no longer lists a WireGuard leg.
- The placement convention's server case and the `<wg_ip>` paths in §3.3 are marked "later
  phase".
- PLAN.md moves success criterion 3 under "Later phase, not required to close this plan". It
  replaces it with a same-host criterion that U23 proves: LXC, docker and a VM on this host.
- P2's test VM is a guest on this host, so it stays in v1.

## Effect on units

| Unit | Effect                                                                                                                                                                             |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| U19  | Built. Its `ccy.env.local` opt-in is superseded and U31 removes it.                                                                                                                |
| U20  | Sessions are launched by `ccy --team acceptance [--seat]`, with no join step. New checks: `--seat` without `--team`, and a plain `ccy` on no team. Cleanup uses `seat remove`.     |
| U24  | Later phase. Not built in v1.                                                                                                                                                      |
| U27  | Needs U23 and U25 (and U26 if built). No longer needs U24.                                                                                                                         |
| U29  | No change.                                                                                                                                                                         |
| U30  | No change, apart from wording: a missing bundle now names `ccy --team <team> --seat <seat>`.                                                                                       |
| U31  | Adds `--team` and `--seat` (the latter refused without `--team`); drops `--no-bus`. The entrypoint takes bus variables only from the launcher. Dist version 3 drops the bus block. |
| U32  | Now only `seat take`, `seat list` and `seat remove`, plus reading the role from `ccy.env.local`. `join`, `leave` and `seat add` are dropped, and no `ccy.env.local` edit remains.  |
| U33  | No change.                                                                                                                                                                         |

Build order: {U23, U29} → {U30, U33} → {U31, U32} → {U20} → {U27} → {U28}.

## Open owner question (DESIGN.md "Owner questions", PLAN.md Task 1.9)

1. Does the phone (probe H7, and U26 if H7 needs TLS) leave v1 together with cross-machine
   teams? The phone reaches the homeserver over WireGuard, which is the network path D46
   defers. Recommended: yes. v1 humans use Element Desktop on this machine, which success
   criterion 2 already names.
