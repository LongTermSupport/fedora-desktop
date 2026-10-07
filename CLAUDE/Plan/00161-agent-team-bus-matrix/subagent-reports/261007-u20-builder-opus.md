# U20 builder report: milestone M2, ccy members and the idle session woken

Branch: `agent-a038771a314366cc0-a62826f3` (worktree branch), one commit starting
"Plan 00161: U20", pushed; not merged into F44. Built in the container; nothing was run
on the host.

## What was built

| File                                                      | What                                                                                                                                                                                                                                                                                                                                                       |
| --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `_acceptance-u20.inc.bash` (new)                          | The M2 legs and checks: prerequisites, the checkouts, `add-member`, the session launcher and stdin pipes, the bounded poller, the room reader, M2.1-M2.4, the end of the sessions and the teardown                                                                                                                                                         |
| `u20_check.py` (new)                                      | Pure judgements and builders: the orders and send/human texts and their stream-json frames, `turns` (result lines), `status-field`, `notices` (the watcher's template parsed out of peer entries in the transcripts), `find-ping`/`expect-replies` over a `/messages` response, `expect-within-window`, `ccy-token` (name only), `scrub` (U01's redaction) |
| `test_u20_check.py` (new, written first)                  | 33 tests, all passing                                                                                                                                                                                                                                                                                                                                      |
| `acceptance.bash`                                         | Additive: header (M2 checks, what M2 creates and needs), usage text, sources the include, registers `u20_teardown_after_stop` before M1's teardown, the prerequisite leg before M1 starts, seven legs after M1.3, the ACCEPTED line                                                                                                                        |
| `PLAN.md`, journal 26-10-07 (via `mkplan.bash --journal`) | M2 line: U20 built, host run pending                                                                                                                                                                                                                                                                                                                       |

`_acceptance-steps.inc.bash` and `acceptance_check.py` are not changed: U20 reuses
`send_ping`, `human_login`, `human_logout`, `human_curl`, `evidence` and the `handle`,
`human-message` and `event-id` commands as they are.

## The harness choice: real ccy, headless, with stream-json input

Option (a) was chosen. `ccy --headless --prompt X` runs `claude --dangerously-skip-permissions -p X`
inside the normal ccy container (`podman run -i`). Claude Code 2.1.292's input code
(`getInputPrompt`) returns the stdin stream and ignores the prompt argument when the input
format is stream-json and stdin is not a terminal, so
`ccy --headless --prompt <placeholder> -- --input-format stream-json --output-format stream-json --verbose --model haiku`
gives exactly U01's session shape (a `-p` session that sits idle between turns and has the
inbox socket), but launched through the owner's real launcher, image, entrypoint (its
`PINGBUS_TEAMS` handling, `pingbus config check`, the `--plugin-dir` and `--settings` it
adds) and plugin. Option (b), host `claude` through `agent-bus-claude`, would have tested
the kit's launcher and not the ccy integration U20 exists to prove.

What makes ccy drivable without a terminal:

- `--token NAME --no-ssh --no-network` skip Quick Launch, the SSH key menu and the network
  menus and preflight; `--headless` skips the tmux insulation. The name comes from this
  checkout's `.claude/ccy/.last-launch.conf` (U01's `load_ccy_token`, which refuses a token
  ccy would refuse). ccy reads the file itself and passes the value into the container by
  environment name; the harness never handles the value.
- Each member's checkout is fresh (`git init`, no commits), under
  `<run dir>/agent-bus-acceptance/ccy-<m>`, so ccy names its project
  `agent-bus-acceptance-ccy-<m>`, and no project or zombie prompt applies.
- Each session's stdin is a fifo. All three launches start before any write end is opened,
  and the write end is opened read-write, so no session inherits another's input, a write
  never raises SIGPIPE, and closing one fd ends exactly one session (checked in the
  container with `cat` stand-ins). Nothing is written until `podman ps` shows the member's
  container running, so a launch prompt nobody accounted for reads nothing and the start
  fails at its bound with ccy's stderr shown, instead of eating the orders.

The session without the socket (ccy-c): its checkout's `.claude/ccy/ccy.env` (sourced by the
entrypoint) exports `CLAUDE_CODE_HARBOR_KITE=0`. In Claude Code 2.1.292's startup that
variable is the cross-session gate: when false, the inbox is not bound
("[uds-messaging] Skipped: cross-session messaging gate off"), so `CLAUDE_CODE_MESSAGING_SOCKET`
is unset and the plugin's SessionStart says "no inbox socket". It is an internal name; M2.3
fails, naming it, if ccy-c shows a watcher, so a rename cannot pass silently.

## What each leg does on the host, and its bound

| Leg                           | Does                                                                                                                                                                                                                                                                             | Bound                            |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------- |
| U20 prerequisites (before M1) | `/var/local/claude-yolo/claude-yolo` executable; `claude-yolo:latest` labelled with the launcher's `REQUIRED_CONTAINER_VERSION`; the image has the agent-bus `pingbus`; the ccy token name; removes any container labelled `ccy-project=agent-bus-acceptance-ccy-*`              | each command's own               |
| checkouts                     | three `git init` checkouts; `ccy.env.local` (`# based on` the installed dist version, `export PINGBUS_TEAMS=acceptance`); ccy-c's `ccy.env`                                                                                                                                      | local files only                 |
| add-member                    | `sudo -n agent-bus add-member acceptance --repo=ccy-<m> --host=acceptance --type=podman --role=… --out=<checkout>/.claude/ccy/pingbus/acceptance`                                                                                                                                | the admin tool's own             |
| start the sessions            | three ccy launches in parallel; each container running; orders written (a, b: act on notices; c: keep a background `pingbus wait`); a and b reach `wake=watcher`, all end their first turn, c reaches `wake=waiter`                                                              | 600 s per launch, 240 s per turn |
| M2.1                          | a is told to `send review <REF_PATH> --to <b>`; the review is in the room; b's transcript gets the watcher's notice; b's inbox empties; the M2.2 review goes out at once (M1's host member a, `review <REF_COMMIT>`); b's ack of a's review is in the room; b ends a second turn | 240 s per wait                   |
| M2.2                          | the second review and b's ack of it are in the room; b's transcript holds two notices of 1 pending (0 humans, 1 ping) with different numbers; the reviews are at most 20 s apart; nothing was written to b after its orders                                                      | 240 s per wait                   |
| M2.3                          | c still `wake=waiter`; M1's member a sends c a review; c's ack is in the room; c ends a second turn; c's transcript has no notice; nothing was written to c after its orders                                                                                                     | 240 s per wait                   |
| M2.4                          | the human posts an `m.text` mentioning a only, asking for an ack; a's ack (`--re` that event, `--to tester`) is in the room; after 30 s, every reply to it is a's, a's transcript has a notice counting a human, b's has none                                                    | 240 s, then a fixed 30 s         |
| end the sessions              | closes each input (claude exits, the container with it), then `podman rm -f` and the launcher killed if still there; copies transcripts and `watch.log` into `<run dir>/u20/<m>/`; removes the checkouts; scrubs the token value from `u20/` (a hit is a FAIL)                   | 60 s per session                 |

Every wait polls every 3 s and is bounded; the actual durations will be in the host run's
log. On any failure, `u20_teardown_after_stop` (registered before M1's teardown, so it runs
first) ends the sessions and removes the checkouts and containers; M1's teardown then purges
the team and with it the three podman members.

## What the owner must provide

- ccy installed and its image current: `deploy.bash`'s last leg (`play-claude-yolo.yml`)
  already does it, and `meta-deploy.bash` already runs `acceptance.bash` after it, so
  `meta-deploy.bash` needs no change.
- This checkout launched with ccy at least once, with a token that is not expired or
  expiring today (the sessions log in with it, by name).
- What ccy itself needs to launch with `--no-ssh`: GitHub's CLI logged in on the host (ccy
  falls back to `gh auth token`), as for any ccy launch.
- Some haiku usage on that token.

## Tests and QA

- `test_u20_check.py`: 33 tests, OK (orders and frames, turns among ccy banners, status
  fields, notices from peer entries only, same-count and no-notice verdicts, room pings
  matched on every field, the earliest match, replies only from the expected sender, the
  window bound, the token name never its value, an expired token refused, scrub names
  files only, usage).
- Unchanged and still passing: `test_acceptance_check.py` (33), `test_u01_probe.py` (73),
  `test_deploy_check.py` (19), `test_triage_probe.py` (57).
- `ruff check` clean on both new Python files; `shellcheck -x acceptance.bash _acceptance-u20.inc.bash _acceptance-steps.inc.bash` clean; `bash -n` clean.
- Not run (container rules): ccy, claude, the acceptance itself, `qa-all.bash`.

## Risks and open questions

1. **Background-task wake in `-p` mode (M2.3).** Whether a `-p` stream-json session starts
   a turn when a `run_in_background` command finishes while it is idle is not proven by
   U01. If it does not, M2.3 fails at "c's ack", which is itself the finding: the
   socket-less fallback would then need the driver to run `wait` between turns, as
   DESIGN.md section 5.4 already says for scripted `claude -p` agents.
2. **`CLAUDE_CODE_HARBOR_KITE`** is an internal Claude Code name (2.1.292). If renamed,
   M2.3 fails naming it; the fix is the new switch, not a weaker check.
3. **The 20 s window in M2.2** depends on b draining its inbox quickly (haiku, one `recv`).
   If b is slow, M2.2 fails with the gap and "run it again" rather than passing a run that
   does not show the notice number mattered.
4. **ccy's own launch paths.** Untried headless here: the token check container, the daily
   Claude Code update (inside the 600 s start bound), `gh auth token` with `--no-ssh`. A
   ccy failure shows its stderr and the leg fails.
5. **Ownership inside the container.** Rootless podman maps the desktop user to root in the
   container, so the bundle (owned by the desktop user) passes the entrypoint's
   `pingbus config check`; this is the first real ccy member, so it is first proven here.
6. **Model compliance.** The orders are narrow and every check is on recorded facts, but a
   model that ignores them makes a check reach its bound (a FAIL, never a false PASS).
7. **Evidence.** Transcripts are kept (scrubbed) under the untracked run directory; the
   token value is never in argv or the run log. The ccy token *name* appears in ccy's own
   banner in `session.out`, as in every ccy launch, and in the prerequisite leg's log line
   (U01 recorded it the same way).
8. **Open:** a `/messages` page of 500 events is assumed to cover the acceptance room (it
   holds well under 100); P8's `git check-ignore` of the ccy bundle paths is left to U27.
