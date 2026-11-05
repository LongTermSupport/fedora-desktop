# Scope review: what v2 brings, the minimum v2, and what to prove first

The owner's question, after a v2 estimate of about 30-40 worker sessions: what does v2 bring, how
can it be simplified, and what uncertainty can be retired before the build. The immediate goal is
team members on other networks, declared as explicit CIDR networks, with per-seat tokens over TLS
and seat tokens issued over ssh (direction 7). Read only; citations are `path:line` at F44
`b83158ca8`, or `<branch>:path:line` where the fact lives only on a v2 branch. A **session** is one
background worker in its own worktree, carrying one slice through spec, build, review and
qa-changed.

**One fact changes the arithmetic, and F44's PLAN.md does not show it.** Most of v2 is already
built, on branches that were never merged. `feat/bus-v2-n3e` holds 146,720 added lines under
`src-go/internal/teamchat` and its two commands (tests included), 726 commits off F44. On that
branch N1 is done (Tasks 1.1-1.4a), N2 is built as inert code (slices n2a-n2g, with no play or
unit because of gate A), and N3's n3a-n3e are built and reviewed. F44's PLAN.md still shows Tasks
1.1-1.4 and 3.1 as open (`PLAN.md:196-205, 244`, against `feat/bus-v2-n3f:…/PLAN.md:153-187, 346-421`).
The branches have had no commit since the n3e entries of the 26th (`feat/bus-v2-n3e:…/JOURNAL/00209-Journal-26-10-26.md`),
and F44 has moved 854 commits and 83,556 lines of `src-go` since. A 30-40 session figure fits the
whole design from zero. It does not fit what remains.

## 1. What v2 brings

| Capability v1 lacks                                                                                               | Needed for the immediate goal?                                                                                                                 |
| ----------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| No Python or bash on any bus path; one Go binary per side (direction 4)                                           | No. It is direction 4's goal, not direction 7's.                                                                                               |
| A board that opens in about 0.1 s from status records, with no sudo (C 3.13, DC4)                                 | No.                                                                                                                                            |
| Seats in a store outside every checkout, one session directory mounted per container (DC5)                        | Partly. It keeps a coordinator's token from sibling workers. A remote machine already holds only its own seats.                                |
| A login per seat take and a logout per release, so a released holder's token is dead (DC16, K13)                  | Useful for a less-trusted remote member, not required. v1 already gives one token per seat.                                                    |
| The owner's password on every change to a team's security fields, `allow_from` and `listen` included (DC18)       | Close. A CIDR list that opens the homeserver to other networks is exactly such a field.                                                        |
| A keeper and a link: transcript-proven delivery, `disconnected` announced for every member kind (B 2.4-2.11, BD9) | No. v1 delivers today.                                                                                                                         |
| A uid per homeserver, the admin token in a child process, team locks, roll forward, staged restore (C 6.3)        | No. These are hardening and robustness.                                                                                                        |
| A stable team id and rename; the side rooms and LXC guests without Python 3.11 (TID, A 4.3, C 3.9)                | No. The handle rule is already met in v1: its site label comes from the checkout, never the hostname (`helpers/agent_bus_lxc/derive.py:9-11`). |
| Cross-network membership: a `networks` CIDR list, TLS with a pinned team CA, a seat keyed on its URL, ssh issue   | **Yes, but neither system has it.** v2 records it as owed and not built (`feat/bus-v2-remote-seats-note:…/DESIGN-v2-C-host.md:1481-1512`).     |

**In short:** the goal itself needs only the last row, and that row is new code in either system.

## 2. The minimum v2

Keep what is built (N1, N2, N3 a-e) and finish it in a narrower shape:

- **Cut-over by drain and recreate, not D 5's migration.** Each v1 team is stopped once it is
  quiet. Its history is exported and its server kept as the read-only archive (OD-D4 and OD-D6,
  unchanged). A v2 team is created under the same id, and the play relaunches its members. This
  drops the fence, the carry plan, M0-M8 and rollback (D 5.2-5.7), and the dual-system board and
  launcher (D 3.9, OD-D2). Direction 5 already settles it: "I am the single user", one step for
  every team. The price is that a ping still open at the switch is asked again; it is not carried.
- **Defer:** n3f's side rooms (Plan 00211), but only if the owner accepts their absence for a
  while, since v1 teams use them now. Also N5 (host `cc`, docker, VM), DC21's seat timer, the
  `teamchat-coexist` footprint scenario beyond its N2 stage, and INT3's long-name ids.
- **Add the cross-network delta:** one `networks:` list feeding listen, allow_from, the unit
  filter, firewalld and the URL check; a team CA in the bundle; the seat take keyed on the URL;
  `member add|reissue` over ssh.

**Re-estimate: 14-18 sessions, plus 4-5 for the spikes in section 4.** The parts: merge the branches
up onto F44 (2); finish n3e and run the N3 acceptance in the VM lab (2); plays, units, install and
host checks (3); the ccy kit, the launcher and a v2-only ccy-teams (3-4); the cross-network delta
(3); drain and recreate, then v1 removed (2-3). The full design adds 8-12 more: N6 as designed,
n3f, the dual board.

## 3. Is the Go rewrite on the critical path?

**(a) The cross-network delta on v1, evolved in place.** It costs 6-9 sessions and is cheap on its
face. v1 already has per-seat tokens and CIDR `allow_from`. It already drives the bind, the unit's
`IPAddressAllow=` and firewalld rich rules from `listen` and `allow_from`. It accepts `https://`, and
its `add-member --out=-` streams a bundle to stdout, so it can run over ssh. The repository's own
rules raise that price, though. `agent-bus-install` is on the bash ratchet and may not grow
(`scripts/bash-limits.allowlist:45`). Reworking the `agent-bus` CLI's `checkout.py` triggers
"ported to Go when it is next reworked" (`CLAUDE.md:81`). So (a) becomes partial Go ports into a
design that v2 then replaces. Its wire differs, so not one built v2 package can be reused. (a) leaves
behind all 25.8k lines of Python and bash. It leaves the board's open time where stage 1 put it,
which has not been re-measured on the host. It also strands the v2 branches. Direction 7 rules it
out as stated.

**(b) The full v2 as designed** costs 22-30 sessions from here. It retires everything v1 has, but
it carries the merge-up risk and N6's machinery.

**Recommendation: the minimum v2 of section 2, 18-23 sessions with its spikes.** The expensive part
is already built and reviewed. The cross-network work costs about the same in either system.
Every session spent in (a) goes into code that direction 4 deletes.

**What would change it:**

- The merge-up spike fails: re-cut the branches by package, and add 2-4 sessions.
- The owner needs a remote member before v2 lands: the v1 `host`-member stopgap, which is his
  exception to direction 7, not ours.
- The owner refuses drain and recreate: add D 5's carry of open pings only, 2-3 sessions.

## 4. Uncertainty to retire before the build

| Spike (size)                  | What it proves                                                                                                                                                                                                                                              | Where                                         | If it fails                                                                                                                                                   |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| S1 merge-up (1)               | `feat/bus-v2-n3e` merges onto current F44, the teamchat packages build, and qa-changed passes under F44's newer project rules.                                                                                                                              | container                                     | Re-cut by package (proto and matrix, then admin, then member), 2-4 more sessions; re-estimate before any build.                                               |
| S2 Tuwunel, N2 (0.5)          | M44 and the server half of HC-A1 to A7 already passed in the container against the pinned Tuwunel 1.9.4. Re-run M44 after S1. The owner's Element steps (HC-A2 display, HC-A3, HC-A5) are still open.                                                       | container; the owner at the screen            | Each check already has its fallback (`DESIGN-v2-A-protocol.md:1970-1976`); none changes the shape.                                                            |
| S3 TLS with a team CA (1)     | The pinned Tuwunel serves TLS itself from a team-CA certificate on its `networks` addresses, plain HTTP on loopback. Go's client pins the bundle's CA. Element Desktop can trust a private CA per profile.                                                  | container (server, Go); host (Element)        | Without Tuwunel TLS: a small Go terminator unit, +1 session. Without Element trust: humans stay on the homeserver host over loopback, and remote humans wait. |
| S4 ssh issue (0.5)            | `member add\|reissue` runs over ssh with password sudo (a tty), and the bundle comes back on stdout into the remote pool. No secret reaches argv, logs or the remote's other seats.                                                                         | VM lab, two guests                            | A one-shot owner-only pickup the remote fetches over ssh, or the broker (Plan 00197); DC16's "password in the remote pool only" is re-decided.                |
| S5 the CIDR source (1)        | One `networks:` list derives the bind addresses (this host's addresses inside it), allow_from, `IPAddressAllow=`, the firewalld zone per CIDR, and the URL check, on two routed subnets. It covers an address that changes (DHCP) and a CIDR with no route. | VM lab, two subnets                           | Keep an explicit `listen` beside `networks`, checked to be a subset of it.                                                                                    |
| S6 cut-over (1)               | A read-only host triage counts the live teams and their member kinds, anonymised. The VM lab runs drain and recreate on a v1 team with one LXC and one ccy member: stop, archive, v2 team under the same id, relaunch, acceptance probe.                    | host (one triage run via meta-deploy); VM lab | D 5's carry for open pings only (+2-3), or the full N6 if a live team cannot be drained.                                                                      |
| S0 gates A and B (no session) | The owner confirms the irreversible names (OD-D1, TID, DC1, DC5, DC15, OD-D5, OD-A7) and INT6. Without that, no play or unit merges.                                                                                                                        | the owner                                     | Nothing installs; the build stops at inert code.                                                                                                              |

A seventh reading costs nothing: time a `ccy-teams` open on the host now, with stage 1 in F44. If
it is already near a second, the board is no longer a reason for v2's urgency.

## Evidence

### v2's built state (branches, not F44)

- The size: `git diff --shortstat F44...feat/bus-v2-n3e -- src-go/internal/teamchat src-go/cmd/teamchat src-go/cmd/teamchat-admin`
  gives 589 files and 146,720 insertions. `git rev-list --count F44..feat/bus-v2-n3e` gives 726, and
  the reverse gives 854. F44's `src-go` drift since the merge base is 956 files and +83,556 lines.
- N1 is done, HC-A1 to A7's server checks were run against Tuwunel 1.9.4, and M44 passes with
  `-race -count=3`: `feat/bus-v2-n3f:CLAUDE/Plan/00209-agent-bus-v2-in-go/PLAN.md:153-197`.
- N2 is built as inert code: n2a-n2g, the rooms engine, apply, member operations, seats, health,
  Element, the ctl child, D-Bus units and backups, with `-tags tuwunel` runs passing. What is missing is
  the play, units and tmpfiles under gate A (`…/PLAN.md:207-342`).
- N3: n3a, n3b and n3c are built and merged into `feat/bus-v2-n3`, and n3d is ready to merge
  (`38b7c3615`). n3e (delivery, hooks, status line) is in its last review fixes
  (`feat/bus-v2-n3e:…/JOURNAL/00209-Journal-26-10-26.md`). n3f's spec is READY
  (`feat/bus-v2-n3f`, `efcc6e995`) (`…/PLAN.md:346-421`).
- The `teamchat-keeps-off-v1` gate and the go-deps rows exist on the branch
  (`…/PLAN.md:157-160, 214-216`).
- Remote seats are recorded as owed and not built in N3 to N6, with no TLS:
  `feat/bus-v2-remote-seats-note:…/DESIGN-v2-C-host.md:1481-1512`. v2's stated no-TLS rule is at
  `DESIGN-v2-C-host.md:402-405`, and Task 0.6's TLS and ssh advice is at
  `subagent-reports/261030-task-0.6-remote-homeserver.md:54-69`.

### What v1 already does toward the goal

- One account and token per seat; a member elsewhere is created with `add-member … --out=-`, a tar
  on stdout: `docs/agent-bus.md:446-458`.
- The team schema has `listen` and CIDR `allow_from`, canonical and without host bits:
  `helpers/agent_bus/teamfile.py:42, 167-198`.
- They drive the unit's `IPAddressDeny=any` and `IPAddressAllow=` (`helpers/agent_bus/render.py:192-205`),
  firewalld rich rules per CIDR with zone lookup and "no route to allow_from"
  (`files/usr/local/sbin/agent-bus-install:545-597`), and the bound-address assertion (`:698-699`).
- `https://` base URLs are accepted, verified against system roots only, with no pinned CA
  (`helpers/pingbus/config.py:581-612`; `helpers/pingbus/matrix.py:102-105` builds a default
  opener). The Go mirror is `src-go/internal/bus/seat/baseurl.go`.
- What refuses a remote seat: `_check_team` wants the homeserver unit active on this host
  (`helpers/agent_bus/checkout.py:701-716`), and the address is read from the local `agentbus0`
  (`:494-505`, used at `:247-252`).
- "There is no TLS … Use WireGuard": `docs/agent-bus.md:379-384`.

### The rules that raise path (a)'s price

- `files/usr/local/sbin/agent-bus-install` is ratcheted at 988 code lines with `assoc,data`
  (`scripts/bash-limits.allowlist:45`). It may shrink, never grow (`CLAUDE.md`, "Tooling Is Go").
- "A standalone Python CLI is ported to Go when it is next reworked" (`CLAUDE.md:81`).
  `checkout.py` belongs to the `agent-bus` CLI that ccy-teams runs.
- Direction 4 wants the Python and bash gone (`PLAN.md:30-34`). The v1 trees are 13,942 lines in
  `helpers/pingbus`, 9,293 in `helpers/agent_bus` and `helpers/agent_bus_lxc`, and 2,579 in the
  installer, `docs/agent-bus.md` and the play, by `wc -l`.

### The open time

- v1's 9.3-23.0 s was measured under load 30-39 (`JOURNAL/00209-Journal-26-10-19.md:44`). C 3.13
  attributes it to 22 Python starts and T+H sudo calls (`DESIGN-v2-C-host.md:2274-2276`).
- Stage 1 is in F44 (`feat/bus-v2-s1-rebuilt` is 0 commits ahead). It cut an open from 6 Python runs
  and 3 `systemctl` calls to 1 and 1 (`subagent-reports/261021-s1-rebuild.md:72-99`). The host
  re-measurement is still open (PLAN.md Task 7.1).

### What drain and recreate drops

- D 5.2's per-item carry table, the fence (nftables and `teamchat-fence@`), the state machine,
  M0-M8 and rollback: `DESIGN-v2-D-migration.md:243-248, 511-1046`. History is not imported in
  either case (OD-D4, `:1264`). Direction 5's "one step, which the plays run" is at `PLAN.md:35-39`.

### Where the estimates come from

- They are calibrated on what the build has spent: 195 sub-agent reports dated the 22nd to the 26th on
  `feat/bus-v2-n3e`, for roughly 12 slices (n2a-n2g, n3a-n3e). That is about one session per slice,
  spec and review rounds included. The figures in sections 2 and 3 count slices of the same
  size. The basis of the 30-40 figure is not in any tracked file read here.
