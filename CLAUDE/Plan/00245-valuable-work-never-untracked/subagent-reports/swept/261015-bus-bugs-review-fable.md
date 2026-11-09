# Review: fix/pingbus-short-name-reply (13531b88) — pingbus short --to / missing REPLY

Verdict: **MERGE** (two optional follow-ups, neither blocks).

## Root-cause claim — verified independently

Installed `/usr/local/bin/pingbus` (symlink to `/opt/claude-yolo/optional/agent-bus/pingbus`,
a zipapp baked into the image) inspected with a scratch script:

- `cli.py`: no `REPLY` string, no `_agents_named`, `_user_ids(names, server_name)` maps each
  name to `@<name>:<server>` with no record lookup (the pre-M3 code).
- `protocol.py`: no `REPLY`, no `addressable`.

Branch export (`git archive`) run: `tests.helpers.pingbus.test_cli_reply`, `test_cli`,
`test_protocol` — 223 tests, OK. The same two new tests run against the modules extracted from
the installed zipapp: `4 != 0 : pingbus: refused: target` and `['PEER'] != ['PEER', 'REPLY']`,
exactly the live symptoms. The claim "image predates container 2.56" cannot be read from a
version marker inside this container (none is installed), but the zipapp lacks every M3
feature and M3 shipped in 2.56, so the dating follows.

## Refusal message — leaks nothing, stderr, exit unchanged

- `_explained_targets` builds the list from `record.roles`/`record.humans` of the team being
  addressed only; `_localpart` strips the server; sender excluded; `admin`/server user never
  listed (`addressable` false). No other team, no homeserver detail.
- `failure()` returns `(EXIT_REFUSED, "refused: target: …")`; tests assert
  `(code, out) == (4, "")`, so stdout stays empty and the text goes to stderr.
- `addressable` is a faithful refactor of both old loops (`_check_targets` with the verb,
  `_check_text_targets` with None); full suite green confirms no behaviour change.
- Only a bare `Refusal("target")` (detail None) is re-explained; `_addressees`'s own
  `--to-coordinator` refusal arrives with `to` empty and gets the "no coordinator" text.

## Tests exercise the production path

Both new tests go through `BusCase.run_cli` (cli.main → cmd_say/cmd_recv against a fake
homeserver), including the two-team `--team` form and the inbox-read path under a held lock.
ruff 0.16.8 on the three changed Python files: clean.

## Versions, deploy list, docs, names

- Dockerfile label 2.60→2.61 and `REQUIRED_CONTAINER_VERSION` 2.60→2.61 together; CCY
  3.98.5→3.98.6. Container bump is necessary: the Dockerfile builds the pingbus zipapp into
  the image (line 398-405), so a changed `helpers/pingbus` needs a new image.
- `meta-deploy.list`: `playbooks/imports/play-claude-yolo.yml` exists on the branch; the
  comment states what the run does and that running sessions keep their old image.
- `docs/ccy-changelog.md` 3.98.6 entry present and accurate. Journal entry appended.
- No install-specific names anywhere in the diff (placeholders `<team>`, `<repo>.<seat>`).

## Findings (non-blocking)

1. `_explained_targets` over-limit text uses `len(to)` of the yielded addressees, but the
   validator counted the list after `with_coordinator_copy`; a worker naming 16 addressees
   including a human gets "takes at most 16 addressees, not 16". Cosmetic edge case.
2. SPEC-many-teams.md S-D4 still says the bare `refused: target` "stays" for validator
   refusals; this branch enriches it. One-line spec update for plan/code consistency.
3. The open relay-mode gap (`_deliver` writes no REPLY line) is recorded in the journal and
   the opus report but not as a PLAN.md task; it should land in the plan so it is not lost.
4. The 3.98.2 changelog entry was reflowed (two lines joined) — formatter noise, harmless.
