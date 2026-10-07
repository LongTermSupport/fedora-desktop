# U29 builder report: seat handles, registry v2, park and return

Branch `seats-integration` (created from `origin/F44`, pushed). The commit is the branch
commit whose message starts "Plan 00161: U29".

## What was built

| File                            | Change                                                                                                                                                                                                                                                                                                   |
| ------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `helpers/pingbus/protocol.py`   | `HANDLE_SEAT_PATTERN` (`[1-9][0-9]{0,5}\|[a-z][a-z0-9]{0,11}`) in the handle pattern as `(?P<seat>…)`; `Handle.seat` (string), `Handle.n` now a property (the number of a numbered seat, else `None`); `format_handle` takes a seat; `is_seat_name`.                                                     |
| `helpers/agent_bus/registry.py` | Version 2: `parked` (a sorted list, each a member); v1 loads as v2 with nothing parked; the counter key is called `prefix` (`PREFIX_PATTERN`, `prefix_of`; `seat_of`/`SEAT_PATTERN` gone); `add_member(..., seat=)`, `park`, `unpark`, `is_parked`.                                                      |
| `helpers/agent_bus/admin.py`    | `add_member(..., seat=)`: a parked handle returns (unparked under the lock, then a mint; re-parked if the mint fails); a current one is refused before any account call; `park_member`; `rotate_token` refuses a parked member; `list` prints `active`/`parked`. `Bundle` carries `role` and `returned`. |
| `helpers/agent_bus/cli.py`      | `add-member --seat=S` (bad seat: 64), `park-member TEAM HANDLE` (`CHANGED` lines); the stderr line says `added` or `returned`.                                                                                                                                                                           |
| `files/usr/local/bin/agent-bus` | Usage lines only (`[--seat=S]`, `park-member`); `park-member` passes through to the tool unchanged.                                                                                                                                                                                                      |
| `docs/agent-bus-protocol.md`    | §3: the pattern, a `seat` row, both example forms, `<seat>` building rules (counter or `--seat`, park and return, `local` as a ccy seat's `<host>`, D49); §8: a named seat in `roles`, a parked member keeps it, older pingbus treats it as untrusted.                                                   |
| `docs/agent-bus.md`             | "Running the team": `park-member`, `list`'s parked field, `remove-member` is for good.                                                                                                                                                                                                                   |
| `DESIGN.md`                     | D51 (below). The table formatter realigned the decisions table, so the diff there is wide.                                                                                                                                                                                                               |

## Tests (all seen failing first, then passing)

- `test_protocol.py`: named-seat handles (both forms, `-`/`_`/`.`/`@`, a leading `0`, a
  leading digit before letters, 12 vs 13 characters), `is_seat_name`, `format_handle`
  with a seat, the seat pattern in the handle pattern; every pre-U29 case unchanged.
- `test_protocol_doc.py`: the `seat` row, `<seat>` in the separator sentence (no `<n>`
  left in §3), every handle example in the spec parses and §3 shows both forms, §3
  names `--seat`, `local` and parking, §8 names a named seat.
- `test_registry.py`: named seat, the counter moved past a numbered `--seat`, current and
  parked handles refused by `add_member`, park then return (role kept, equal to before),
  park/unpark refusals and idempotent park, removing a parked member, v1 parse and file
  load, v2 refusals (`parked` missing, not a list, not a member, twice, not a string).
- `test_admin.py` `SeatTest`: `--seat` builds the handle (no counter entry); a numbered
  seat moves the counter; park revokes (old token 401), keeps role, record and room
  membership, no kick or deactivation, `list` says `parked`, a re-park revokes again
  without a registry change; return with the same account (no account creation call),
  the earlier role despite `--role=worker`, a new token, the old one refused; a current
  seat refused with no write call; a deactivated (removed) seat refused with no account
  created; a parked seat whose account was deactivated is refused and stays parked; a
  failed mint on return leaves it parked; `park-member` of an unknown or removed handle
  refused. `MemberCommandsTest`: `rotate-token` of a parked member refused; `list`'s
  fifth field.
- `test_cli.py`: `--seat`, refusal 70, `park-member` markers, `list`, return; bad seats
  64; unknown handle 70; the secret-hygiene run now includes a seat, a park and a return.
- `test_wrapper.py`: `park-member` passes through, `--seat` reaches the tool, the usage
  lines.

Runs: `tests.helpers.agent_bus.*` 226 OK; `tests.helpers.pingbus.*` 806 OK (every module);
the plan folder's `test_acceptance_check.py` 49 OK and `test_u20_check.py` 33 OK. ruff
0.16.8 (the pin) clean on every touched Python file; shellcheck clean on the wrapper.

## Decisions taken (D51)

- **"The counter skips numbers issued with `--seat`"** is implemented as: a numbered
  seat above its prefix's counter raises the counter to it. No record of explicit numbers
  is needed and no number is ever issued twice by the counter. A number below the counter
  that was never issued may still be named with `--seat`; one that was issued and removed
  still has its deactivated account, which `add-member` refuses.
- **A parked member stays in `members`** with its role (its role stays in the team record,
  as section 5.5 says), and `parked` lists it. A return keeps that role whatever `--role`
  the launcher passes: the return is the same member (D40).
- **`park-member` revokes first, then marks parked**, so a failure between the two is
  completed by a re-run; parking a parked member revokes again and reports no registry
  change.
- **`rotate-token` refuses a parked member**: otherwise it would be live while the
  registry says parked.
- **`agent-bus list` gains a fifth field** (`active`/`parked`), so "`list` shows both"
  (section 5.5) also says which handle is parked. Nothing in the repository parsed the
  four-field form except the two tests updated here; U32's `seat list` should read the
  fifth field if it uses `list`.

## Left for other units

- `admin.NEXT_STEPS["podman"]` (the bundle README text for a ccy member) still describes
  U19's `.claude/ccy/pingbus/<team>/` and `PINGBUS_TEAMS` in `ccy.env.local`. That text is
  the opt-in U31/U32 remove, so it is theirs to rewrite with the `seats/<team>/<seat>/`
  layout; `test_member_docs.py` will hold it to the docs.
- No CCY change, so no version bump. No host run belongs to U29 alone: its code reaches
  the host in the `agent-bus` zipapp (`agent-bus-install software`) and the ccy image's
  pingbus, both exercised by U20's host run.
