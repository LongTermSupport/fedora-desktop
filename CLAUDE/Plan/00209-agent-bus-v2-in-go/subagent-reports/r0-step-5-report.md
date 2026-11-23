# R0 step 5, part A: report (partial)

Branch `v2-questions`, commit ce05b2df1 (pushed). CCY 3.123.5, container 2.84.52.

## Built (test first, green)

- **Records:** the `question` record's `n` may be null (only `ask-human --check` uses it); it
  gains `why`, `for`, `waiting_since_ms` and `retitled`. A new `bump` record
  (`{"q","bumped","why"}`), whose `why` is `answered` or `young` exactly when it was skipped. The
  pinned JSON tests are updated.
- **Wire:** the args and 30 s deadlines of `ask-human`, `thread`, `close`, `retitle` and `bump`;
  `questions` keeps 60 s.
- **Command line:** `ask` is renamed `ask-human` (`--for`, `--check`, `--skeleton`). `thread`
  takes `--note` (its `--ask` and `--close` are gone). New: `close Q<n>...`, `retitle Q<n>`,
  `bump [Q<n>...] --waiting --note --min-age`, and `questions --waiting --for --digest`. The last
  positional argument may repeat. `run.go`'s `unbuilt` list is empty.
- **Client:**
  - `--skeleton` prints A 10.2's form with no session.
  - Text read from stdin is capped at 8000 bytes for a question and 4000 for a thread post or a
    retitle.
  - `close` reads a note only from a stdin that is not a terminal.
  - `bump` takes question names or `--waiting`, never both; `--min-age` defaults to 3600.
  - When `close` did not retitle a question, the client writes the stderr line, because the
    link has no stderr channel.
- **Wiring:** the six verbs are added to `clientVerbs`. The coordinator's hook text now names
  `ask-human`. `team.PlacementOf` (moved out of teamsync) and `proto.DecodeEvent` are exported
  for the engine.

## Not built: the engine

The link answers the six ops with 1 internal ("does not serve ... yet"). The full design and the
next steps are in `untracked/scratch/trackB-handoff-4.md`, inside the worktree.

## Departures from handoff 3

- The op is `ask-human`, not `ask`: the client's op name is the verb's name.
- `--check` cannot print a plain body inside a JSON Lines answer, so the handoff proposes a
  small `draft` record.
- `retitled: false` reaches stderr through the client, as described above.

## Notes

- There are no duty `hint` records: the roster on the wire carries no duty.
- `member/kit/skill.txt` still names `teamchat ask` and `thread --close`. It is step 8's text.
- No defect class needed a project rule.
