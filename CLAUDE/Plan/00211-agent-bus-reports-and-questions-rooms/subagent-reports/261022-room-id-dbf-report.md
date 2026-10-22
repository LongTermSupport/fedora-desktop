# Defence Before Fix report: homeserver-list-narrow-grammar

## Specification followed

Method specification 1.1.0, the cached copy (`refresh-spec.bash`: SPEC.md cached 1.1.0,
DETECTOR-SPEC.md cached 1.0.0, TOOLING-SPEC.md cached 0.2.0).

## Toolchain

The repository has no DBF manifest declaration (no `composer.json` or `package.json` entry) and
no conforming DBF toolchain from the register. Its own QA (`scripts/qa-changed.bash`,
`scripts/qa-all.bash`) wires semgrep rules through Go gates, one gate per rule file, and runs
every `tests/helpers/**/test_*.py` through `scripts/qa-helper-tests.bash`. The rule is a
test-based AST detector in that helper-test suite: it reads code without running it, has a
single-rule harness (`python3 -m unittest tests.helpers.pingbus.test_identifier_grammar`), prints
a stable identifier, and resolves through `CLAUDE/QA.md` "homeserver-list-narrow-grammar". There
is no listing command for active defences; the CLAUDE/QA.md headings are the nearest thing. The
project had recorded no calibration, so this report assumes: first-party Python only, findings
block, no exceptions.

## Defect and class

Originating instance: `helpers/agent_bus/admin.py:417` (`_Api.joined_rooms`, F44 f2c59399), which
called `all(protocol.is_room_id(r) for r in rooms)` on `GET /_matrix/client/v3/joined_rooms`.
Class: a list from the homeserver validated with the bus's own identifier grammar, which is
narrower than the protocol's, so one legitimate foreign item refuses the whole answer. Hazard: a
valid homeserver answer is refused wholesale. Here Tuwunel 1.9.4's server admin room is room
version 11 (`src/service/admin/create.rs`: `RoomId::new_v1`, `RoomVersionId::V11`), and Tuwunel
joins each new server admin to it, so every team's side-room discovery stopped with rc 70.

## Rule

- Detector: a Python `ast` check, `tests/helpers/pingbus/test_identifier_grammar.py`.
- Identifier: `homeserver-list-narrow-grammar`.
- Message: `<file>:<line>: homeserver-list-narrow-grammar: a homeserver list checked with the bus's own identifier grammar refuses the whole answer for one foreign item; see CLAUDE/QA.md "homeserver-list-narrow-grammar"`.
- Documentation: `CLAUDE/QA.md`, the `homeserver-list-narrow-grammar` section.
- Scope: in `helpers/` and `CLAUDE/Plan/` Python, a list drawn from a homeserver answer (a call
  to a `HOMESERVER_CALLS` method: `.call`, `.transport`, `._request`, `.sync`, `.messages`,
  `.get_state`, `.get_event`, `.state`, `.room_state`, `.members`, `.joined_rooms`, `.account`;
  or a local name bound from one, followed through assignments) and checked with `is_room_id` or
  `is_event_id` so that one item refuses it all. That means inside `all()`/`any()`, or in a
  loop's `if` that raises.
- Widened after review round 1 (Opus). The first cut knew only `.call`/`.transport`/`._request`,
  never examined the syncer, and missed the loop-and-raise form. The widened cut first flagged
  `pingbus/cli.py:1189`, which loops over `--re` CLI arguments, not a homeserver answer, so it
  does not carry the hazard. That is why the rule follows the data from the call. It still finds
  `admin.py:417` at F44 and nothing in F44's `syncer.py`.
- Excluded, because it does not carry the hazard: a list that does not come from a homeserver
  answer.
  `protocol.py:1265` (the `agent_bus.rooms` index) and `:1452` (an agent text's `re`) check
  content the bus itself wrote, and the bus's grammar is that content's specification. Also
  excluded: a single-value check, such as a `createRoom` answer for a room the bus asked to be
  version 12.
- Next wider rule considered: flag every `all()`/`any()` over `is_room_id`/`is_event_id`
  anywhere. Not built, because it would flag those two bus-protocol checks, which are correct as
  they are.

## Proof

- `RuleProof.test_the_rule_fires_on_exactly_the_marked_lines` covers the kept fixture: three
  `ruleid` lines (`all`, `any`, loop-and-raise) and four `ok` shapes.
- On the red commit 4cd5167b, `python3 tests/helpers/pingbus/test_identifier_grammar.py` gives
  `FAIL: test_no_finding ... helpers/agent_bus/admin.py:417: homeserver-list-narrow-grammar: ...`.
  The fixture proof passes on that commit.

## Independent search

`defence-before-fix:independent-searcher` was dispatched before the rule file was written, and
was kept out of it. Its report, `subagent-reports/261022-room-id-independent-search.md`, is
committed with the fix rather than with the red commit, so the order rests on this statement, not
on git history. It found 13 sites, of which only `admin.py:417` refuses wholesale.

- Text search found every call site of the named checks, including the Go regexps.
- Reading classified each site: wholesale or per item, and homeserver-sourced or bus-written.
  Grep could not tell those apart.

The search found no instance the rule missed, so the rule was not widened. It flagged three
latent per-item sites, judged free of this hazard:

- `syncer.py:659` ignores a non-v12 invite.
- `matrix.py:326` checks a single `event_id`.
- `syncer.py:733` checks a single member key.

## Sweep and count

- Scope: all first-party Python under `helpers/` and `CLAUDE/Plan/`, recorded here as the
  project's decision.
- Go: `src-go`, vendor excluded, has no homeserver client. Its two room-ID regexps
  (`seat/member.go:61`, `statusline/member.go:43`) read bus-written `member.json`.
- Count: 1 instance.

## Fixes

One instance, `admin.py:417`, examined individually. It now uses the spec grammar
(`protocol.is_any_room_id`), then selects the version-12 candidates. The rest are returned apart
and named on stderr by `_find_side_room`. Nothing was suppressed.

## Decisions referred to the owner

None. No baseline, suppression or narrowing was needed.

## Permanence

The rule runs in `scripts/qa-helper-tests.bash`, which runs in `qa-changed.bash` (for changed
Python) and in `qa-all.bash`. On the fix commit, `qa-changed.bash` passed and the whole helper
suite passed (5132 tests, 2 skipped).

## Toolchain and detector gaps

- The repository has no DBF toolchain: no manifest declaration, no defence listing and no
  identifier resolver beyond the documentation headings.
- Semgrep gates here need a Go gate per rule file. That is why the rule is test-based.

## The original defect

- The fake homeserver (`tests/helpers/pingbus/fake_admin_api.py`) now joins a newly registered
  server admin to a version-11 `!opaque:server` admin room, as Tuwunel does.
- Red, before the fix: all 27 side-room tests raised the exact host error, `AdminError: list the admin's rooms: the homeserver answered with no list of room IDs`.
- The new tests are in `ServerAdminRoomTest`:
  - discovery beside the admin room, with the stderr note;
  - idempotence;
  - garbage still refused, with the same message;
  - a 403 room is not a side room.
- `test_protocol.py` adds the spec-grammar cases for `is_any_room_id`.
- Fix commit 5f86e270. CCY 3.99.14.12, container 2.78.15.
