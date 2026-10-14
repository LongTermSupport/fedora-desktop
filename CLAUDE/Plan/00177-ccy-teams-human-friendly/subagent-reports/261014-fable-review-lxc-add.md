# Adversarial review: `feat/ccy-teams-lxc-member` (Plan 00177 "Phase 8", `ccy-teams lxc-add`)

Branch `origin/feat/ccy-teams-lxc-member` (96789a85, 68da37b9) against `origin/F44`. Reviewer: Fable.
Method: read every changed file, ran the real `declare.py` over crafted host_vars, ran the suites
named below. Nothing in the branch was edited.

## Verdict

**Not mergeable as is.** The file-editing core (`declare.py`) is sound and well defended, but the
interactive flow has one hard UX defect (F1: a container's user already in one team can never be
added to a second team from `ccy-teams`), two gaps where the play's rules demand a `seat` that the
flow has no way to give (F2, F3), and three housekeeping conflicts (plan numbering, meta-deploy
duplicate, host-word suggestion contradicting its own hint). Fix F1 to F3 and the list items, then
re-run the suites; the rest is nits.

## What was run

| Check                                                                   | Result                                                                               |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| `go test ./...` (src-go)                                                | all ok                                                                               |
| `scripts/qa-go.bash`                                                    | GO-OK, 42 packages, coverage 94.4%, lint clean, project rules clean                  |
| `tests.helpers.agent_bus.test_declare`                                  | 77 OK                                                                                |
| `tests.helpers.agent_bus_lxc.test_members`                              | 59 OK                                                                                |
| `tests.helpers.agent_bus.test_cli`, `test_member_docs`, `test_registry` | 27, 21, 50 OK                                                                        |
| `ruff check` on declare.py and its test                                 | clean                                                                                |
| `scripts/test-ccy-teams.bash`                                           | passed 156, failed 0                                                                 |
| `scripts/test-ccy-teams-command.bash`                                   | passed 336, failed 0 (run as `bash <file>`: tracked 100644 on F44 too, pre-existing) |
| `scripts/qa-bash-limits.bash`                                           | BASH-LIMITS-OK; the branch changes no bash file                                      |
| diff scan for install-specific names                                    | none found (only `container-a/b/c`, `/srv/agent/...`, example addresses)             |

`qa-all.bash` was not run, as instructed.

## Findings, ranked

### F1 (high): a user already in one team cannot be added to a second team; the user question is refused three times with a misleading message

`helpers/agent_bus/declare.py:886-903` (`check_lxc`) judges the partial entry as
`dict(_LXC_TEMPLATE, **given)`, so at the **user** question (Go passes team, container and user
only: `src-go/internal/teams/lxcadd/adder.go:286`) the template `host: "template"` is compared by
`members.parse_members` against the host the same `(container, user)` already has in another team.

Failing input (probed with the real helper):

```text
host_vars: alpha has container-a/agent with host worker
$ declare lxc-check --file hv.yml --team beta --container container-a --user agent
declare: agent_bus_lxc_members gives agent@container-a two hosts; its env file sets one HOOKS_DAEMON_HOSTNAME
exit 64
$ declare lxc-add --file hv.yml --team beta --container container-a --user agent --host worker --checkout /srv/agent/one --dry-run
exit 0        <- the full entry is valid; only the partial check is wrong
```

In `ccy-teams lxc-add beta` the person types `agent`, is told about "two hosts" (nothing they
typed), retries, and after three tries gets "Giving up after 3 tries; nothing was changed."
`members.py` explicitly supports one container/user in several teams (`_groups`, `merge_env`
`add=teams`), so this is the main multi-team case, blocked.

Fix (declare.py side, keeps the duplicate detection): in `check_lxc`, when `context` is true and
`host` is not given, take the template's host from the existing entry of the same
`(container, user)` if there is one. Add the case to `test_check_judges_what_it_is_given` and a Go
rig test where `lxc-members` lists the member in another team.

### F2 (medium): a container whose name gives no seat can never be added; the message says "set seat" and the flow has no seat question

`members.default_seat` (`helpers/agent_bus_lxc/members.py:288-293`) refuses a name that does not
start with a letter after stripping. `lxcadd` never asks for a seat and `answers.entry()`
(`adder.go:95-101`) never sets `Seat`.

Failing input: a running container named `2024box` (a valid LXC name, `_CONTAINER_RE` accepts it).

```text
$ declare lxc-check --file hv.yml --team beta --container 2024box --user agent
declare: container 2024box gives no seat name ([1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11}); set seat
exit 64
```

The person is asked the user three times and given up on. Fix: when the container's default seat
is unusable (or on any refusal mentioning seat), ask a "Seat" question (same `askValid` pattern,
checked with `LxcCheck{Seat: answer}`) and pass `--seat`. The Python side already accepts `--seat`.

### F3 (medium): two containers sharing their first 12 letters collide on the default seat; refused only at the summary, after every question, with no way to set a seat

`default_seat` truncates to 12 characters; `parse_members` refuses one seat for two containers in
one team and host. The host question checks `Host` alone (`adder.go:338`), so the collision surfaces
only in `summary` -> `LxcAddDryRun` (`adder.go:424`), which returns the error instead of re-asking,
ending the run with exit 64.

Failing input: `longcontainername-one` already in alpha with host `worker`; add
`longcontainername-two`, user `agent`, host `worker`:

```text
declare: agent_bus_lxc_members gives seat longcontaine of team alpha to two containers; set seat on one
exit 64
```

Same remedy as F2 (a seat question), and the dry-run refusal in `summary` should be a
"refused, nothing was changed" message rather than a bare declare line.

### F4 (medium, design): the suggested host word is the container's name, which the same screen says never to use

`declare.py lxc-suggest-host` returns `registry.resolve_host(container)` (`declare.py:980-985`);
`askHost` offers it as the default answer while the hint beside it says "a role word such as
worker, never a machine's hostname, because handles appear in public forge text"
(`adder.go:336-337`). Container names are typically install-specific (`my-server-01` probed ->
`my-server-01` suggested). The default answer therefore steers the person towards what the hint
forbids, and Enter accepts it. Either suggest `worker` (the role word, which is also the play's
documented example) and mention the container name only as a fallback, or drop the suggestion.
`docs/agent-bus-lxc-members.md:94` and `usage.txt` describe the current behaviour, so they change
with it.

### F5 (list item): Plan 00177 has two unmerged "Phase 8"s

`origin/F44`'s PLAN.md ends at Phase 7. `origin/feat/ccy-teams-hot-add` (c4c79c83) adds
"Phase 8: Add a running ccy session to a team, without restarting it" and names Phase 8 in its
commit message and in Plan 00194's cross-reference. This branch adds "Phase 8: Add an LXC
container to a team" with tasks 8.1 to 8.5 and journal refs `T8.1`, `T8.3`. Whichever merges
second conflicts in PLAN.md and leaves two `T8.x` task sets in the journal. Recommendation: this
branch renumbers to **Phase 9** (Tasks 9.1 to 9.5; the journal's `T8.1`/`T8.3` entries are
append-only, so add one `decision` entry saying Phase 8 here became Phase 9); it is the cheaper
rename, as hot-add's number is already referenced from another plan.

### F6 (list item): `meta-deploy.list` entry duplicates what 00196 already runs

The branch adds `playbooks/imports/play-claude-yolo.yml` after `00196-team-coordinator-role`
(`CLAUDE/Plan/meta-deploy.list:34-38`). `00196/deploy.bash:62-65` runs `play-agent-bus.yml` then
`play-claude-yolo.yml`, and `meta-deploy.bash` runs from the merged checkout, so that same run
builds the new `ccy-teams` with `lxc-add`. Two consecutive runs of the same play do the same work
twice. The list should be, in order:

1. `playbooks/imports/play-agent-bus.yml` (00161 follow-up, unchanged)
2. `00196-team-coordinator-role`, with its comment extended by one sentence: "Its
   play-claude-yolo.yml run also builds the ccy-teams that has `lxc-add` (Plan 00177); then try
   `ccy-teams lxc-add <team>`."
3. `00161-agent-team-bus-matrix/acceptance.bash` (unchanged)

Only if 00196 has already been run and removed from the queue by the time this merges does a
standalone `play-claude-yolo.yml` entry belong there.

### F7 (low): the host question cannot see a host conflict; it is caught late

`lxc-check --host other` passes (probed, exit 0) even when the same container/user is declared
with `worker` elsewhere; the conflict surfaces in the summary dry-run as a run-ending error.
Resolved for free by F1's fix if the host question also passes team, container and user.

### F8 (low): names read from inside a container reach the terminal unfiltered

`lxcfreeze.Lister.Users` (`src-go/internal/lxcfreeze/lister.go:112-126`) keeps any passwd field 1
that is not a colon or newline; `askUser` prints them in the hint ("It has: ..."). A hostile
container's passwd can put terminal escape bytes there. Nothing is executed and the written value
is re-checked by `_USER_RE` before it reaches the file, so this is a display issue only. Filter in
`ordinaryUser` with the same `[a-z_][a-z0-9_-]{0,31}` the play accepts. Also, `members.py` runs
`lxc-attach --clear-env -- env -i PATH=...`; the Lister runs `--clear-env -- getent passwd` with no
PATH and relies on lxc-attach's default. Match the play's form.

### F9 (nit): dead client method, unused flag, spacing

- `declare.Client.LxcRemove` (`src-go/internal/declare/lxc.go:86`) has no caller outside its
  test; YAGNI says drop it until a remove flow exists (the Python `lxc-remove` stays: the play's
  `state: absent` route needs it).
- `lxc-suggest-host` requires `--file` and reads it only to fail if missing; harmless.
- `declare.py:995`: `new =mark_lxc_absent(` (missing space; ruff's pinned ruleset does not flag E225).

## What held up

- **File safety.** Lines outside the edited list are kept byte for byte (tested with a prefix and
  suffix including `other_key` and a comment inside the list); the new entry is read back through
  the same parser before anything is written (`add_lxc`, `mark_lxc_absent`); a vault-tagged value
  anywhere in the list is refused with 78 and nothing written; an inline `agent_bus_lxc_members: []`
  is refused rather than guessed at (probed); the write is backup, `O_EXCL` temp, fsync,
  `os.replace`, mode kept. The read-then-write window is unlocked, as it was for `create`; a
  concurrent hand edit would be overwritten but kept in the backup. Pre-existing, acceptable.
- **Rule parity.** Every rule comes from `members.parse_members` imported, judged with the entries
  already in the file; declare adds only the team-present check, host-as-the-play-reads-it, and a
  checkout character set that the line reader keeps whole (a path with a space or `#` is refused
  with a clear message, probed). `orchestrator` is written as `coordinator`. One coordinator per
  team, duplicate (team, container, user), absent entries not counting: all tested red/green and
  probed.
- **Root/sudo.** Every command is an argv list through `sudo.Capture`/`execx`; container names come
  from `lxc-ls` and users from `getent passwd`, and both are re-validated by the play's regexes
  before being written or passed on. Nothing read from a container is executed on the host.
- **UX.** No hidden key (asserted); re-prompt bounded at 3 with "nothing was changed"; Ctrl-C at
  each of the five questions leaves no write (asserted at every position); a declined summary
  writes nothing; a failed apply leaves the entry declared and prints the recovery
  (`ccy-teams apply <team>`, and the gh login hint) without the "ccy --teams" step.
- **Tests** assert behaviour: the exact `lxc-add` argv, declare-before-apply order, outputs on
  stdout/stderr, exit codes, the real `declare.py` over a scratch copy. `teamtest.SkipStyle` is
  not masking anything: `gum style` prints and takes no input, so skipping an answer for it is the
  more faithful model; the older flows' tests queue a dummy answer for it, which is the latent
  oddity, not this opt-in.
- **Docs** describe what the code does (sudo first, running containers only, role offered only
  with no coordinator, backup beside the file). They will need F4's and F5's edits.
