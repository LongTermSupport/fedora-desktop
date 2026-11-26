# Task 1.3: the read-only traffic probe (built, not run)

## What was built

- `teamchat-admin team traffic --team T --since <RFC 3339> --until <RFC 3339> [--json]`: a
  root verb of the existing admin program. Usage, bounds and what it cannot count:
  [bus-host-commands.md](../bus-host-commands.md#the-traffic-probe-counts-per-hour-task-13).
- `src-go/internal/teamchat/traffic`: the window (at most 7 days), the backward `/messages`
  read (500 events a page, at most 1,000 pages a room, failing rather than answering a part),
  the classification (type, typed key, relation `thread`/`reply`/`edit`, membership), the
  child's wire form with a strict decode and `Validate`, the table and the JSON document.
- `src-go/internal/teamchat/admin/trafficverb.go`: the verb, the child op `team.traffic`, and
  root's check of the child's answer (`trafficAsked`: the window asked, nothing else). The
  answer plumbing is in `child.go`, `answerops.go` and `serve.go`, and the verb table is in
  `run.go` and `verbs.go`.

## Why a verb of `teamchat-admin`, not a new binary

The `teamchat-ctl` child already opens a team's admin client from its own `admin.token` and
reads `rooms.json` (`Opener`, `roomsOf`), and `carry.tail` already reads `/messages` through
it. Root never holds the token, and no argv or environment carries it. A new binary would
have needed a token route of its own. Bodies are parsed only in the child and dropped there:
the wire answer holds names of a fixed form and counts.

## What it cannot count

`m.read` receipts, typing and presence are ephemeral, and no `/messages` read sees them.
"Receipt" is counted as `teamchat.receipt` events (typed key). "Presence change" is counted as
membership events, the nearest record in history. The probe's output says both.

## Tests (test first, fakehs)

- `traffic/traffic_test.go`: counts per room, hour, type, typed key, relation and membership
  over a fakehs team with page size 2. Also: the page bound failing, a homeserver failure,
  the catch-alls for names of another form, malformed events, the window's bounds, the wire
  round trip and strict decode, `Validate`'s refusals, and table and JSON output holding no
  body.
- `admin/trafficverb_test.go`: the verb end to end through the in-process child over fakehs
  (table and `--json`), usage errors (64), a lying child refused, a team not applied (70), and
  the child's own argument checks. `run_test.go`'s verb table holds `team traffic`.

## Owed

The runs of Task 1.3: the busy team for 25 Nov and a quieter team over the same day, by the
bus host's admin seat, once the teamchat play's `admin` tag has installed this binary (queued
in `meta-deploy.list` with the existing `play-teamchat.yml` entry).
