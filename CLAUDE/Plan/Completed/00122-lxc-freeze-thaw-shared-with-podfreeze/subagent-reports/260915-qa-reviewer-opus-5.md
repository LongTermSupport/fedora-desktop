# QA Review — Plan 00122 Phase 4: the extracted `freeze-common.bash` menu layer and the `lxcfreeze` sibling

**Verdict**: FIX-BEFORE-MERGE

Reviewed: `git diff 9b6c8d55^..HEAD` restricted to the freeze tools, the shared library,
the three suites, the two plays, `tasks/deploy-freeze-lib.yml`, `PLAN.md` + `JOURNAL/`,
and the `docs/playbooks.md` / `CLAUDE/ContainerEngines.md` entries.

Mechanical gates all pass; everything below is what they cannot see. Ranked most severe
first, 11 findings.

---

## 1. `sudo lxc-info` failure is laundered into "this container does not exist"

`files/home/.local/bin/lxcfreeze:386`

```bash
state="$(lxcf_parse_state "$(sudo lxc-info -n "$name" -s 2>&1 || true)")"
```

`|| true` discards the status; the parser correctly answers `""`; lines 387-390 then
`continue` and the container is **dropped from the inventory with no message at all**.
Fail-fast HARD RULE: no same-line `# FAIL-FAST-OK:` annotation.

The same file already demonstrates the right pattern twice — `config_rc=0; config="$(sudo cat …)" || config_rc=$?` at `:396`, and `lxcf_bridge_label`'s three-way answer at
`:283-295`. So "could not tell" is carried for the *bridge* and silently folded into
"absent" for the *state*, in the same loop.

**Failure scenario**: `lxc-ls -1` lists 5 containers; `lxc-info -n web -s` exits non-zero
for one (malformed config, monitor socket busy, SELinux denial). `lxcfreeze list` prints
`=== running (4) ===` with no mention of `web`; `lxcfreeze freeze --all` freezes 4 of 5
and says nothing. `freeze_partition`'s VANISHED bucket never fires, because `web` was
never selected.

**Fix**: capture the status as the config read already does, and disclose the unreadable
containers on stderr. `podfreeze`'s `warn_identity_blind_spot` (`podfreeze:602-613`) is
the precedent for exactly this class — disclose, do not block.

## 2. The comment at `lxcfreeze:483-486` states a guarantee that is false

> `assert_lxc` and `assert_sudo` have already ruled out the other two reasons this could
> be empty.

They have not. A third reason exists and is unguarded: every `lxc-info` call failing
*after* the two guards ran, which finding #1 makes silent. `do_list` then prints
`LXC is installed and has no running or frozen containers.` — a confident claim about a
host derived from probes that all failed.

This is the shape `CLAUDE/AgentNotes.md` calls "a discarded failure signal becomes a
confident wrong answer", asserted in a comment as though it had been ruled out.

## 3. `lxcf_parse_state`'s grep collapses "no match" with "grep itself failed"

`files/home/.local/bin/lxcfreeze:233`

```bash
value="$(printf '%s\n' "$raw" | grep -m1 -E '^[[:space:]]*State:' || true)"
```

`grep` exits 1 for no-match (an answer) and >= 2 for a real error. `|| true` makes them
one. **`lxcf_parse_bridge`, 23 lines below at `:256-260`, does this correctly** —
`|| grep_rc=$?` then `if [ "$grep_rc" -gt 1 ]`, with a comment explaining why. Two
grep-status handlers in one file, one right and one wrong.

**Gate gap, worth reporting separately**: neither `|| true` is caught, because
`.semgrep/bash-conventions.yml:235` anchors the pattern to end-of-line —
`(?m)^.*\|\|[ \t]*(true|:)[ \t]*(#.*)?$`. Both instances sit inside `$( )`, so `)`
follows `true` and the repo's #1-rule gate structurally cannot see them. That rule also
has no `# FAIL-FAST-OK:` escape at all, unlike its sibling at `:31`.

## 4. `PLAN.md` Goals and Non-Goals assert the opposite of what shipped

Three false statements, all still live:

- `PLAN.md:57` — "`podfreeze` is **not touched**: not one line, so its behaviour cannot
  regress." `podfreeze` is 698 lines changed in this diff.
- `PLAN.md:64` — Non-Goals: "**Touching `podfreeze` at all** … not its name, not its
  groups, not its rootless behaviour, and not a library extracted out of it."
- `PLAN.md:39` — the SCOPE blockquote: "the shared library is **not** built here and
  `podfreeze` is **not** refactored. `lxcfreeze` is standalone and will re-implement the
  menu, the derived verb and the dry run for itself."

`PLAN.md:66` ("De-duplicating the two tools. Deferred to Phase 4") is stale in the same
way — Phase 4 is done, not deferred.

The Success Criteria list got an explicit *"Phase 4 supersedes it"* note; Goals,
Non-Goals and the SCOPE quote did not. This is `AgentNotes.md` -> "Completed narrative in
a PLAN is where superseded reasoning survives", in a file every session reads in full and
indexes as current state.

## 5. The extracted menu layer has zero behavioural coverage in any of the three suites

`pick_target` and `drill_into_group` are the *only* thing Phase 4 exists to share, and
nothing executes either one's body.

Total coverage across all three suites:

- `scripts/test-freezelib.bash:575-599` — both refuse without a terminal. That is all.
- `scripts/test-freezelib.bash:705-718` — **both are replaced with stubs** so
  `interactive_loop` can be tested around them.
- `scripts/test-podfreeze.bash:30-31` — declares both out of scope.
- `scripts/test-lxcfreeze.bash` — no mention of either.

So the fzf branch, the numbered branch, the `2,4,5` member parse, the `token - 2`
arithmetic, the `b`/`q`/ENTER keys and the bounded retry — the entire UX the owner
complained about — ship on one host run of `lxcfreeze` and nothing else.

`scripts/test-freezelib.bash:694-697` claims *"Plan 00122's whole complaint was about
this layer, so it is not left to a host to find out"*, in the very block that stubs that
layer out.

**Fix**: extract the pure part — parse a member-choice string plus a bound into a name
list — into a named function so it can be driven directly. `identity_axis_discriminates`
(`podfreeze:531-535`) was extracted from `pick_target` for exactly this reason, and its
own comment says why: *"a predicate inlined there is a predicate the unit test can only
re-implement, and a re-implementation asserts nothing about the code that ships."*

## 6. `podfreeze`'s drill-down member row lost a column width

`files/home/.local/lib/freeze/freeze-common.bash:526-528`

| | |
| --- | --- |
| was (`podfreeze` @385c47ec:894-896) | `printf '%-34s %-8s %-4s %-22s %s'` — name, state, CCY, **NETWORKS padded to 22**, verb |
| now | `printf '%-34s %-8s %s %s'` — name, state, `freeze_hook_table_row` (`podfreeze:678-681` -> `%-4s %s`, unpadded), verb |

The `FREEZE`/`THAW` column is no longer a column.

**Scenario**: a group with `web` on `podman` and `db` on `myapp_default,podman` — the
verb previously sat at a fixed offset for both; it now sits 17 characters apart. Same
raggedness in `lxcfreeze`, where `(no network)` is 12 characters against `lxcbr0`'s 6.

`print_table` survived the move byte-identical (verified: old `%-34s %-8s %-4s %s` with
literal `CCY`/`NETWORKS` == new `%-34s %-8s %s` plus the hook's `%-4s %s` header). This
row did not.

It matters less for the pixels than for what it demonstrates: this is the one region the
pin explicitly does not cover, it changed, and `PLAN.md:171` argues Task 4.1b exists so
the extraction's guarantee can be *"no behaviour change at all"* — a guarantee the suite
cannot give for this region and which is already untrue in it.

## 7. `freeze_hook_select` is called in an `if` condition, suspending errexit through the whole hook

`files/home/.local/lib/freeze/freeze-common.bash:716`

Before the extraction, `select_ccy` / `select_all` / `select_identity` ran as plain
commands in a `case` under `set -e` (`podfreeze` @385c47ec:1112-1132). They now run
inside `if ! freeze_hook_select "$key"`, which suspends errexit for the **entire function
body**. Verified empirically:

```
$ bash -c "set -euo pipefail; f() { false; echo REACHED; return 0; }; if ! f; then echo nonzero; else echo zero; fi"
REACHED
zero
```

Any internal failure in a hook is now reclassified as the library's documented "the group
went away — re-prompt", and `:717-718` prints only a blank line, so the user sees an
unexplained menu redraw. The library cannot distinguish "that group no longer exists"
from "the hook broke", and the contract at `:57` does not oblige a hook to have explained
itself before returning non-zero.

## 8. `fzf` self-containment is applied to one play and not its sibling, and the deploy message claims otherwise

`playbooks/imports/optional/common/play-podfreeze.yml:42-52` installs `fzf` with the
explicit reasoning *"so the play is self-contained rather than depending on
play-open-command.yml having been run"*. `play-lxcfreeze.yml` installs nothing
(`tasks/deploy-freeze-lib.yml:16-17`: "podfreeze installs fzf; lxcfreeze needs nothing"),
yet the shared library branches on `have fzf` at `freeze-common.bash:444` and `:531`.

`play-lxcfreeze.yml:121-123` then prints, unconditionally:
*"Both source ~/.local/lib/freeze/freeze-common.bash, so both offer the same menu,
drill-down and keys."* `docs/playbooks.md`'s new lxcfreeze entry repeats it:
*"same menu, same drill-down, same keys as `podfreeze`, because it is the same code"*.

**Scenario**: a fresh host, `ansible-playbook play-lxcfreeze.yml` alone — the exact case
`tasks/deploy-freeze-lib.yml:10-12` says must work standalone. No `fzf` -> numbered menu,
`b`/`q`/`2,4,5`. `podfreeze` on a full `playbook-main.yml` host -> fzf, TAB, ESC. Two
tools, different habits, reintroduced through the dependency rather than the code.

**Mitigating**: `playbooks/imports/play-claude-yolo.yml:416-421` is a core import of
`playbook-main.yml` and installs `fzf`, so anyone who ran the main playbook is fine. But
that is precisely the transitive dependency the podfreeze play deliberately refused to
rely on, and the claim in the message is stated without the condition.

## 9. Two stale figures in `PLAN.md`

- `:163` — "`scripts/test-podfreeze.bash`, 183 cases". It is **187** (confirmed by
  `qa-all.bash`), and `:195` says 187. Task 4.1b added the four.
- `:185` — "Seven named hooks and **five declared settings**". The contract block at
  `freeze-common.bash:46-51` declares **six**; five are *required* by the loop at
  `:80-89`, and `FREEZE_LIST_NOTE` is the sixth — `lxcfreeze:96` sets it and
  `select_names` (`freeze-common.bash:370-372`) prints it.

Note on `:195` — "passes **187/187 with the file byte-identical** — `git diff` touches not
one line of it": the subject is the *suite*, and that is TRUE
(`git diff 385c47ec..HEAD -- scripts/test-podfreeze.bash` is empty). But a reader
arriving from the false `PLAN.md:57` will read "the file" as `podfreeze` and take the two
sentences as reinforcing each other. Worth rewording once #4 is fixed.

## 10. `2> /dev/null` in `freeze_lib_path`

`files/home/.local/bin/podfreeze:114`, `files/home/.local/bin/lxcfreeze:106`

```bash
dir="$(cd -- "$(dirname -- "$src")" 2> /dev/null && pwd -P)" || continue
```

Discarding stderr makes a real resolution failure (permissions on a parent, a deleted
cwd) indistinguishable from "this candidate does not apply". The tool then prints "the
shared freeze library is missing" naming a path that may not be the problem.
`CLAUDE/AgentNotes.md` -> "Bash conventions the gates only partly enforce" bans
`2>/dev/null` outright: capture into a variable and report the reason.

## 11. `drill_into_group`'s row numbering assumes every group member resolves

`files/home/.local/lib/freeze/freeze-common.bash:525` and `:590`

`i="$(inventory_index_of "$name")" || continue` skips a member when building `menu`, but
the bounds check and index arithmetic use `total="${#group[@]}"` and
`group[$(( token - 2 ))]`. One skipped member silently shifts every row below it, so
typing `4` acts on the container shown at row 5.

Currently unreachable — every selector filters through the inventory — and carried over
verbatim from the pre-extraction tool, so **not a regression**. Flagged because the guard
is written as if the case can happen, and if it ever does the result is *acting on a
container nobody chose*, silently. Either drop the `|| continue` and let a missing member
be a named internal error, or build the row->name mapping as an array so it cannot skew.

---

## Checked and clean

- **IaC placement**: `tasks/deploy-freeze-lib.yml` is in the right place and follows the
  established `tasks/ensure-jq.yml` precedent (3 existing includers). Included by both
  plays; either play alone produces a working tool. `play-lxcfreeze.yml` is a justified
  separate file and the reasoning at `:16-30` is honest about failing the repo's own
  "independent lifecycle" test and why the name wins — a rename would churn the
  `#play-podfreezeyml` anchor `docs/ccy.md` links, plus Plan 00079's PLAN.md and
  deploy.bash. **Not** an ordering problem; **not** a play that should have been an edit.
- **`container_engine`**: not applicable — these are two engine-named user tools, not a
  play choosing an engine. Nothing hardcodes an engine where the variable belongs.
- **No engine leak into the shared half**: confirmed — no `podman`, `lxc`, `pause`,
  `unpause` or `if <engine>` anywhere in `freeze-common.bash`. The claim in
  `CLAUDE/ContainerEngines.md` holds as written.
- **Stderr hygiene**: no violations found. `pick_target` and `drill_into_group` emit only
  their answer on stdout and every prompt, menu and diagnostic on stderr; `print_table`
  is payload and callers redirect it (`freeze-common.bash:663`); `die`, `log`, the
  load-time refusals and `do_action`'s reporting all go to stderr; `usage` / `do_list` /
  `--dry-run` are correctly the printing-for-a-human exception. `have()` redirects
  `command -v`'s stdout.
- **Interactive UX**: bounded retry (MAX_TRIES=3) with re-prompt on bad input in both
  menus; EOF exits cleanly; `b` backs out one level rather than quitting; hard abort
  reserved for no-TTY and exhausted retries. Matches `CLAUDE/InteractiveScripts.md`.
- **Two state vocabularies**: no place in the library assumes one. Every comparison goes
  through `FREEZE_STATE_RUNNING` / `FREEZE_STATE_FROZEN`, the two-words-must-differ
  assertion at `:96-101` is real and load-bearing, and `test-freezelib.bash` drives every
  decision under both. `lxcf_parse_state` correctly refuses unknown and transient LXC
  states (ABORTING/STARTING/STOPPING) rather than guessing which side of the line they
  fall on, and has a discrimination control at `test-lxcfreeze.bash:222-226` so the
  negatives cannot pass against a parser that returns empty unconditionally.
- **The pin's integrity**: `git diff 385c47ec..HEAD -- scripts/test-podfreeze.bash` is
  **empty** — byte-identical across the extraction commit, 187/187 passing. The only pin
  edits are in `385c47ec`, the bug-fix commit, and they are correctly written as
  before/after flips with the fix's own regression cases added (multi-key matching still
  works; the literal network name is still accepted).
- **Public repo**: clean. No usernames, hostnames, container names, project names,
  emails, private IPs or checkout paths in any of the code, plays, tests, `PLAN.md` or
  the 430-line journal. Examples use `lxcbr0` / `virbr1` / `mynet` / `<gh-username>`.
- **Plan Commit Rule**: working tree clean, no untracked `CLAUDE/Plan/` dirs, README
  index row present at `CLAUDE/Plan/README.md:41`. Status correctly `In Progress` with
  Tasks 3.5 and 4.6 open and both genuinely open.
- **Docs/QA bookkeeping**: `CLAUDE/QA.md`'s 32->35 and 25->28 counts both check out
  against the actual gate list (7 jq-merged + 28 separate). `qa-deployed-drift.bash` was
  correctly widened to cover the new library — the right generalisation, since a deployed
  tool whose own bytes match the repo can still source a stale menu, and the `.local/bin`
  loop cannot see it.
- **Modes/shebangs**: library tracked `100644` matching its `0644` deploy (correct — it
  is sourced, and `:63-68` refuses to run as a program); both tools `100755`; both plays
  `100755` with the correct shebang; `shellcheck -x` clean on all three bash files; the
  library IS discovered by `qa-bash.bash` despite being mode-0644 with a shebang
  (confirmed in `/tmp/qa-results.json`), so `AgentNotes.md` row 8 does not recur here.

## Mechanical gates

| Gate | Result |
| --- | --- |
| `scripts/qa-all.bash` | **PASS** — 906 files, 35 gates. `freezelib: 204`, `lxcfreeze: 74`, `podfreeze: 187` |
| `hooks-daemon plan-qa --sweep` | **PASS for 00122** — 3 advisories, all other plans (00046 path, 00125 README row, journal freshness) |
| `ansible-playbook --syntax-check` | **PASS** on `play-lxcfreeze.yml` and `play-podfreeze.yml` |
| `shellcheck -x` | **clean** on `freeze-common.bash`, `podfreeze`, `lxcfreeze` |
| `qa-helper-tests.bash` | ran inside `qa-all` (1399 tests) — **not triggered** by this diff, which touches no `helpers/` or `tests/helpers/` |
| `helpers.gnome.check_extension_compat` | **not triggered** — no `extensions/` metadata change |
| `extensions/` ESLint | **not triggered** — no extension JS change |
