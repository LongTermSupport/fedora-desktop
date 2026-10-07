# Plan 00161: seats design reworked for the owner's answers

Design only; nothing is built. The owner answered the five questions of the seats design
(`261007-seats-design-opus.md`) on 2026-10-07, and two answers reverse it: a seat is a
durable role whose name is reused, and seats are provisioned organically rather than by IaC.
DESIGN.md now describes the result: §1, §2, §4 (admin commands), §5.1-§5.3, §5.5, §5.6
(rewritten), §9, §11, §12 (U20, U29-U33, waves, milestone M2, section "U20"), D18, D33-D43
and "Owner questions". PLAN.md: Tasks 1.6-1.8, M2, success criterion 1.

## The answers and where they landed

| Answer                                                                                         | Design                                                                                                                                                                                    |
| ---------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Widen protocol v1, no v2                                                                    | Kept (D37); the "never reissued" half of the old D37 is gone.                                                                                                                             |
| 2. A seat is a role: names reused, one live session per seat, history readable, numbers reused | D40: park on removal, return with the same account; registry v2 `parked` (not `retired`); D36: unnamed launches take the lowest free number; D43 and U33: `pingbus history` from the room |
| 3. Local-host teams only                                                                       | D42: `join` refuses a team whose homeserver is not on this host; the ccy row of §5.2 lost `<wg_ip>`                                                                                       |
| 4. Organic provisioning                                                                        | D33, D41, §5.6: `agent-bus join` and `leave`; `agent-bus seat add`, `take`, `remove`, `list`; `ccy --seat` creates on first use; IaC optional and none built                              |
| 5. U20 reuses the owner's launch choices; criterion 1 confirmed                                | D38 now also refuses a passphrase key before a headless launch, naming `ssh-add`; PLAN.md criterion 1 reworded without "provisioned by the play"                                          |
| 6. U20 in the real checkout, organic path                                                      | §12 "U20" rewritten: join, `seat add`, `ccy --seat`, and `leave` to restore the checkout                                                                                                  |

## Dropped, because the organic flow does not need it

- The managed block in `ccy.env.local`, `PINGBUS_SEATS`, `PINGBUS_NAMED_SEATS` and
  `named_only`. The seat record is now the directory `seats/<seat>/`, and every seat is in
  every team its checkout joined, so one `PINGBUS_TEAMS` is enough. Role seats are never
  taken by an unnamed launch, which is what `named_only` existed for.
- `agent-bus-install seats`, `render seats`, `agent_bus_ccy_checkouts`, the play's seats
  step and its `localhost.yml.dist` placeholder: v1 builds no IaC for checkouts. `join` needs
  no privilege, so a play can call it later.
- Registry `retired` and "a removed seat comes back as `dev2`": reversed by answer 2.
- `pingbus seat list --root`: the host-side list is `agent-bus seat list`, from the same
  `seat.py`.
- First-free seat selection inside the container: the launcher picks on the host, so the
  container only claims the seat it is given, and restarts and restores keep their seat.

## Kept

The seat lock (`flock` on `seats/<seat>/seat.lock`, inherited, released with the
container), `pingbus seat exec`, `--seat`/`--no-bus`, per-seat `PINGBUS_HOME`, the
`ccy-seat` label, the SessionStart seat line and `SEAT` lines, headless Quick Launch, and
U20's watcher-stop fallback leg (D39).

## Facts behind the main decisions

- **Park rather than deactivate and reactivate.** `admin.py` deactivates on
  `remove-member` (`_remove_account`), and `add-member` refuses a handle whose account
  exists (`api.account(user_id) is not None`); `bootstrap` already says "a deactivated
  account cannot return" for humans. The H4 fixtures (`tests/helpers/pingbus/fixtures/tuwunel/`)
  record account creation (011, 014, 015, 017), the password reset with `logout_devices`
  (029, with 030 showing the old token refused) and the admin login mint (016, 018), but no
  reactivation. So park = that password reset (token revoked) plus a registry flag;
  return = the mint for the same account, the pair `rotate_token` already uses. The account
  never leaves the room, so the room's history stays readable to it. `remove-member` keeps
  its meaning: retired for good.
- **History from the room.** `consumed/` (`inbox.py`) keeps every item `recv` drained, but a
  parked or lost seat loses its local state, and a first sync takes `next_batch` only
  (`syncer.py`), so the past is not re-delivered. `pingbus history` therefore pages
  `/messages` (fixture 072), filters to the seat, and applies `recv`'s checks. The room is
  created with the `private_chat` preset (`admin.py` `_ensure_room`), whose
  `history_visibility` is `shared` by the Matrix spec; U20's M2.8 proves on Tuwunel that a
  returned seat reads its earlier traffic.
- **Where the address comes from.** The `agent-bus` wrapper runs every admin command as
  root (`run_admin_tool`), and the team's `team.json` is unreadable to the user, so the
  checkout commands take `<bus_ip>` from `agentbus0` (the ccy address, settled by H1), and
  `add-member` still checks it is one of the team's listen addresses.
- **Who writes the checkout.** The wrapper's root path refuses an existing `--out` and
  only places new directories; the checkout commands run as the user (refused as root), edit
  `ccy.env.local` atomically, refuse a symlink, and reach root only through `sudo agent-bus add-member|park-member|set-role`.

## Gaps the rework records rather than hides

- Seats in one checkout are not isolated from each other (one user, one workspace): every
  session can read every seat's token. §5.5 and §9 say so.
- A session already running when `join` first creates `ccy.env.local` can write it (the
  read-only bind is taken at launch). `join` names running containers; the launcher names
  each team before creating an account in it. §5.3 and §9 say so.
- Two unnamed launches at the same moment can pick the same free number; the second fails
  its claim with exit 75 (fail fast, relaunch). U20 launches its unnamed sessions one after
  another.

## Effect on units

| Unit          | Effect                                                                                                                                                                    |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| U29           | Registry v2 `parked` (not `retired`); `add-member --seat` creates or returns; new `park-member`.                                                                          |
| U30           | `seat exec` claims only the seat it is given; no variables to parse beyond `PINGBUS_SEAT`; seats read from the directory; SessionStart points at `pingbus history`.       |
| U31           | The launcher picks and creates the seat on the host (`agent-bus seat take`), records it for restart/restore; passphrase keys refused headless.                            |
| U32           | Rewritten: host commands `join`, `leave`, `seat list`, `seat add`, `seat take`, `seat remove` in `helpers/agent_bus/checkout.py`; no installer, play or host_vars change. |
| U33 (new)     | `pingbus history`, `HISTORY` lines.                                                                                                                                       |
| U20           | Organic path; new checks M2.7 (a later session is the same member and reads history), M2.8 (remove and return), M2.9 (numbers reused); M2.10 the checkout unchanged.      |
| U22           | No seats step.                                                                                                                                                            |
| U23, U24, U27 | No change.                                                                                                                                                                |

Build order: {U23, U29} → {U30, U33} → {U31, U32} → {U20} → {U24} → {U27} → {U28}.

## Open owner questions (DESIGN.md "Owner questions")

1. An unnamed `ccy` when every numbered seat is held creates the next number, rather than
   refusing. Recommended: create.
2. Every seat is in every team its checkout joined, so U20 needs this checkout joined to no
   other team while it runs. Recommended: accept for v1.
3. Answer 3 limits ccy seats to local teams; U24 and success criterion 3 (a team on another
   machine, non-ccy members) stay in v1. Recommended: they stay.
