# freeze toolkit (podfreeze, lxcfreeze): functional specification for the Go port

Programme P7 of [INVENTORY.md](INVENTORY.md). Method: Plan 00191's
(`CLAUDE/Plan/00191-go-cli-tooling/SPEC-ccy-teams.md`): the spec states what the bash does today,
numbers the latent bugs, and says which a Go port fixes deliberately and which it keeps.

Sources, all at commit 61737bd1. `L<n>` is a line of the file named by its prefix:

| prefix | file                                              | lines |
| ------ | ------------------------------------------------- | ----- |
| `PF`   | `files/home/.local/bin/podfreeze`                 | 925   |
| `LF`   | `files/home/.local/bin/lxcfreeze`                 | 953   |
| `FC`   | `files/home/.local/lib/freeze/freeze-common.bash` | 823   |
| `TL`   | `scripts/test-freezelib.bash`                     | 1039  |
| `TP`   | `scripts/test-podfreeze.bash`                     | 1107  |
| `TX`   | `scripts/test-lxcfreeze.bash`                     | 736   |

Toolkit size: 2701 lines of tool (about 1300 of them comments), 2882 lines of suites, 543 cases.
Deployed by `playbooks/imports/optional/common/play-podfreeze.yml` and `play-lxcfreeze.yml`, which
both include `tasks/deploy-freeze-lib.yml`. Nothing else sources or calls the toolkit; the only
other mentions are docs, QA wiring and a CCY comment (section 1).

Two tools, one library. `FC` is sourced by both and holds everything the user touches (menu,
drill-down, verb derivation, dry run, act loop). Each tool supplies its engine through seven
`freeze_hook_*` functions plus six `FREEZE_*` settings. Section 3 specifies the library once;
sections 4 and 5 specify the two engines.

"stdout" below means a tool's stdout. Only these go to **stdout**: the usage text (`--help`), the
`list` report, and the dry-run block (`DRY RUN` line plus table). Everything else, including every
table printed around an act, every prompt and every `==> ` line, goes to **stderr**
(CLAUDE/StderrHygiene.md). No tool uses colour anywhere.

---

## 1. Deployment, wiring and who else mentions the toolkit

- `play-podfreeze.yml` (hosts desktop, `become: true`, tag `podfreeze`): ensure `~/.local/bin`
  (0755, user-owned); include `tasks/deploy-freeze-lib.yml`; `copy` `podfreeze` to
  `~/.local/bin/podfreeze` (0755, user:user); remove `~/.local/bin/podman-freeze` (pre-rename
  binary); a debug "ready" message.
- `play-lxcfreeze.yml` (tag `lxcfreeze`): the same, with `lxcfreeze`, no removal task.
- `tasks/deploy-freeze-lib.yml`: `dnf` install `fzf` (tag `packages`); ensure
  `~/.local/lib/freeze` (0755); `copy` the library to `~/.local/lib/freeze/freeze-common.bash`
  (0644, a library: sourced, never executed). fzf is installed with the library so a fresh host
  running either play alone gets the same picker (Plan 00122 Task 3.5).
- Neither play is imported by `playbook-main.yml`; both are optional and run directly.
- `scripts/qa-deployed-drift.bash` L232: pair `files/home/.local/lib/freeze/*` against
  `~/.local/lib/freeze`, fix-play `play-podfreeze.yml`. (The tools themselves are covered by the
  generic `.local/bin` loop of that script.)
- `scripts/qa-all.bash` L1179-1227: three hard gates `freezelib`, `lxcfreeze`, `podfreeze`, each
  running its suite and reporting a case count via `qa_gate_case_count`.
- Prose that names the toolkit and must change at cut-over: `docs/playbooks.md` L1211-1300,
  `docs/ccy.md` L1395-1398 (the `podfreeze freeze --github <gh-username>` example; stays true),
  `CLAUDE/ContainerEngines.md` L155-161, `CLAUDE/QA.md` L127-129, a comment at
  `files/var/local/claude-yolo/claude-yolo` L3674-3688 (explains the labels podfreeze reads; stays
  true), `CLAUDE/AgentNotes.md` L466/L475 (bug-class table rows), L1028-1035 (the `lxc-attach`
  stderr rule, section 7.5).
- **The CCY labels podfreeze reads** (`claude-yolo` L3689-3699, L3897-3899): every session
  container is run with `--label ccy=true`, `ccy-github=<user|none>`, `ccy-token=<name|none>`,
  `ccy-ssh-keys=<space-separated basenames|none>`. That is the whole contract between the launcher
  and `podfreeze`; the Go port must read exactly these four label names and the word `none`.

---

## 2. The command line

Both tools: `TOOL [list|freeze|thaw] [target] [options]`. Words and flags may come in any order.

### 2.1 Usage text (verbatim; `-h`/`--help` prints it to stdout and exits 0)

`usage()` is a function of `echo` lines (`PF` L197-246, `LF` L239-285), **not** the header
comment, so the Go binary embeds exactly these texts (`go:embed`). podfreeze:

```
Usage: podfreeze [list|freeze|thaw] [target] [options]

  Freeze (pause) and thaw (unpause) Podman containers — one, a whole
  network, or every CCY (Claude) session at once.

  Run with no target for a menu of GROUPS, each row saying what
  choosing it does right now ("FREEZE 8", "THAW 3"). Choosing one
  opens the group: the first row acts on all of it, the rest are its
  containers if you want only some. It acts, then returns to the
  menu, until you quit.

  The verb is derived: anything running is frozen, a set with nothing
  running is thawed. So the same choice twice toggles it. There is no
  confirmation prompt — freezing is undone by doing it again.

  Freezing is the cgroup freezer, not suspend-to-disk: frozen
  containers do not survive a reboot.

Verbs (optional — omit to let the state decide):
  list               Show running and frozen containers, then exit.
  freeze             Pause the target containers.
  thaw               Unpause the target containers.

Targets (exactly one, or none for the menu):
  NAME...            One or more container names.
  --ccy              Every CCY container.
  --network NET      Every container attached to network NET.
  --github ID        Every CCY session running as GitHub account ID.
  --token LABEL      Every CCY session using that Anthropic token.
  --ssh-key NAME     Every CCY session with that SSH key mounted.
  --all              Every running/paused container.

  The last three read labels CCY stamps on each session at launch, so
  they only see sessions started by CCY 3.40.0 or later. An unknown
  value is an error listing the known ones, never an empty set.

Options:
  -n, --dry-run      Print the resolved set and exit; change nothing.
  -h, --help         Show this help and exit.

Examples:
  podfreeze                       menu: pick a group, it acts, repeat
  podfreeze list
  podfreeze --ccy                 toggle every Claude session
  podfreeze freeze --ccy          explicitly freeze them
  podfreeze freeze --network mynet -n
  podfreeze freeze --github <gh-username>
  podfreeze thaw --all
```

lxcfreeze (`LF` L239-285) is the same shape; the differing lines. One line of the text is NOT
verbatim: the last line of the root paragraph, "You will be prompted once per run.", becomes
"sudo is asked again if its timestamp has expired." because bug 6 makes the promise untrue (the
Go tool readies sudo again at the start of each refresh and act batch). The three lxcfreeze help
scenarios (`lxc-help`, `lxc-help-short`, `lxc-help-inside-container`) are bug 6 deviations for
exactly this line.

```
Usage: lxcfreeze [list|freeze|thaw] [target] [options]

  Freeze and thaw LXC containers — one, a whole bridge, or all of them.
  The Podman counterpart is 'podfreeze'; this is its LXC sibling, and is
  separate because this repo's LXC is rootful and Podman here is not.

  Run with no target for a menu of GROUPS, each row saying what choosing
  it does right now ("FREEZE 3", "THAW 1"). Choosing one opens the
  group: the first row acts on all of it, the rest are its containers if
  you want only some. It acts, then returns to the menu, until you quit.

  The verb is derived: ... (as podfreeze, three lines)

  Freezing is the cgroup freezer, not suspend-to-disk: frozen containers
  do NOT survive a reboot.

  Every operation needs root, including the read-only inventory:
  /var/lib/lxc is root-only. You will be prompted once per run.

Verbs (optional — omit to let the state decide):
  list               Show running and frozen containers, then exit.
  freeze             Freeze the target containers.
  thaw               Thaw the target containers.

Targets (exactly one, or none for the menu):
  NAME...            One or more container names.
  --bridge BR        Every container whose first interface is on BR.
  --all              Every running or frozen container.

  A STOPPED container is listed by neither: it can be neither frozen nor
  thawed, so it is not a candidate.

Options:  (as podfreeze)
Examples:
  lxcfreeze                       menu: pick a group, it acts, repeat
  lxcfreeze list
  lxcfreeze --all                 toggle everything
  lxcfreeze freeze --all
  lxcfreeze freeze --bridge lxcbr0 -n
  lxcfreeze thaw mycontainer
```

The implementer copies both byte for byte from the bash `echo` lines (the elided parts are copied
from `LF` L251-253, L275-276). No suite asserts the usage text today (gap, section 9.3).

### 2.2 Argument parsing (`PF` L813-869; `LF` `parse_args` L855-898)

Left to right, one token at a time; the first problem exits at once, so **order matters**
(bug 16). Every `die` is `<tool>: <msg>` on stderr, exit 1.

| token                                           | action                                                                                                                                                                                         |
| ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `-h`, `--help`                                  | print usage to stdout, exit 0 (works inside a container: parsing precedes the host guard)                                                                                                      |
| `-n`, `--dry-run`                               | `DRY_RUN=1`                                                                                                                                                                                    |
| `--ccy` (podfreeze), `--all`                    | `set_target_mode`                                                                                                                                                                              |
| `--network NET` (pod), `--bridge BR` (lxc)      | `set_target_mode`, then take the next token verbatim; missing -> `die "--network needs a network name — see 'podfreeze --help'."` / `"--bridge needs a bridge name — see 'lxcfreeze --help'."` |
| `--github ID`, `--token L`, `--ssh-key N` (pod) | `set_target_mode "$1"`, axis = flag without `--`, take next token verbatim; missing -> `die "--<axis> needs a value — see 'podfreeze --help'."`                                                |
| `list`, `freeze`, `thaw`                        | if a verb is already set: `die "only one verb at a time — already have '<verb>'."`; else set it                                                                                                |
| any other `-*`                                  | `die "unknown option '<tok>' — see '<tool> --help'."` (so `--bridge` on podfreeze and `--ccy` on lxcfreeze are unknown options; `-nh` is unknown)                                              |
| anything else                                   | a container name, appended to the names list (may repeat; may be the empty string)                                                                                                             |

`set_target_mode X`: if a mode is already set -> `die "choose one target only — '<old>' and '<X>' are mutually exclusive."` Modes are the flag text (`--ccy`, `--all`, `--network`, `--github`, `--token`, `--ssh-key`, `--bridge`). After the loop, if any names were collected, `set_target_mode "names"` (so `podfreeze --ccy foo` -> `... '--ccy' and 'names' are mutually exclusive.`). Repeating one flag (`--ccy --ccy`) is the same refusal.

### 2.3 Order of operations after parsing (`PF` L875-925; `LF` `main` L900-946)

1. `freeze_assert_contract` (wiring check; disappears in Go).
2. `assert_on_host` (section 3.2).
3. engine preflight (section 4.1 / 5.1).
4. `load_inventory` (engine).
5. verb `list`: if a target mode is set -> `die "'list' takes no target — it reports everything."`; else `do_list`, exit 0.
6. no target mode: `interactive_loop` (section 3.9); its status is the exit status.
7. target mode: resolve to a selection (section 3.5), then `do_action "${ACTION:-$(infer_action)}"`; `do_action`'s status is the exit status. An explicit `freeze`/`thaw` verb wins over the derived one.

A verb with no target (`podfreeze freeze`) is the interactive menu with that explicit verb
driving every label (`target_effect` and `row_verb` read `ACTION`). `-n` with no target is also
the interactive menu, each pick previewed rather than acted on (bug 21).

### 2.4 Exit codes

| code | when                                                                                                                                                                    |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0    | success; `--help`; `list`; "Nothing to do"; a dry run; quitting the menu with no failed act; fzf Esc/Ctrl-C/no-match at the group menu (quit) and drill-down (back)     |
| 1    | every `die` (usage mistakes and engine failures alike, there is no 64); any container failed in `do_action`; a menu session in which an act failed (status carried out) |
| 130  | only an uncaught SIGINT while a numbered prompt `read` is waiting (the shell dies by the signal; nothing traps it)                                                      |

Usage mistakes exit **1**, not 64 (bug 10).

---

## 3. The shared library (`FC`)

### 3.1 The contract (L40-122)

Settings a tool declares **before** sourcing: `FREEZE_TOOL`, `FREEZE_STATE_RUNNING`,
`FREEZE_STATE_FROZEN`, `FREEZE_HOST_ONLY_NOTE`, `FREEZE_TARGET_HINT` (all required; an empty one
exits 1 with `freeze-common.bash: <NAME> is not set.` plus three explanation lines);
`FREEZE_LIST_NOTE`, `FREEZE_FREEZE_NOTE` (optional, may be empty). The two state words must
differ (else exit 1 naming the word). Sourcing the library runs nothing and sets no shell
options; executing it directly prints "this is a library — source it, do not run it." and exits 1.
Hooks: `preflight`, `refresh`, `menu_rows`, `select KEY`, `act VERB NAME`, `table_header`,
`table_row INDEX`; a missing hook is `die "internal error: <hook> is not defined.` ... (L194-205).

`FREEZE_SELECT_GONE=2` is the one recoverable status `select` may return (a network, bridge or
identity value that vanished between menu draw and choice); any other non-zero is fatal (L79-88).

| setting                       | podfreeze                                                                                                                                            | lxcfreeze                                                                                                                                          |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `FREEZE_TOOL`                 | `podfreeze`                                                                                                                                          | `lxcfreeze`                                                                                                                                        |
| `FREEZE_STATE_RUNNING/FROZEN` | `running` / `paused`                                                                                                                                 | `RUNNING` / `FROZEN`                                                                                                                               |
| `FREEZE_HOST_ONLY_NOTE`       | `  The host's podman is not reachable from in here, so every container would\n  look absent. Freezing your own session would also be unrecoverable.` | `  The host's LXC is not reachable from in here, so every container would look\n  absent and this tool would report a machine with nothing on it.` |
| `FREEZE_TARGET_HINT`          | `NAME..., --ccy,\n  --network NET, or --all`                                                                                                         | `NAME..., --bridge BR,\n  or --all`                                                                                                                |
| `FREEZE_LIST_NOTE`            | (empty)                                                                                                                                              | `  A STOPPED container is not listed: it can be neither frozen nor thawed.`                                                                        |
| `FREEZE_FREEZE_NOTE`          | (empty)                                                                                                                                              | the 5-line note in 5.5                                                                                                                             |

Constants: `MAX_TRIES=3`; `ALL_ROW_KEY='*all*'` (a leading `*` cannot start a container name).

### 3.2 `assert_on_host` (L182-189)

Refuses when any of: file `/run/.containerenv` exists, file `/.dockerenv` exists, env `container`
non-empty. Message (exit 1): `<tool>: this is a container — run <tool> on the HOST.` newline
`<FREEZE_HOST_ONLY_NOTE>`. The two file paths are **arguments** with the real paths as defaults, a
deliberate test seam that an environment variable cannot loosen (L174-181). Called bare by both
tools; a comment in each (`PF` L879-889, `LF` L907-911) explains why passing `"$@"` would be
dangerous. In Go this is a struct field on the app's environment, never an env var or a flag.

### 3.3 Inventory and the pure decisions (L131-374)

Inventory = parallel arrays `INV_NAME`, `INV_STATE` (+ the engine's own, indexed alike). A Go
`Inventory` is a slice of entries with a name -> index map; no exported parallel slices.

- `inventory_index_of name`: exact string match; a prefix/suffix/empty string never matches.
- `count_in_state state names...`: how many of **those names** are in that state; unknown names
  count 0; **duplicates count twice** (pinned by TP, bug 4).
- `infer_action` over `SELECTED`: `freeze` if any selected name is in the running state, else
  `thaw` (empty selection and all-vanished selection give `thaw`).
- `target_effect names...` with `r`=running count, `f`=frozen count:
  - `ACTION=freeze`: `FREEZE <r>` if r>0 else `nothing to freeze`;
  - `ACTION=thaw`: `THAW   <f>` (three spaces) if f>0 else `nothing to thaw`;
  - derived: `FREEZE <r>` if r>0, else `THAW   <f>` if f>0, else `nothing to do`.
    `FREEZE n` and `THAW   n` are the same width. The three refusals are three distinct strings.
- `row_verb name`: unknown name -> `?` (whatever the verb). `ACTION=freeze`: running -> `FREEZE`
  else `-`; `ACTION=thaw`: frozen -> `THAW` else `-`; derived: running -> `FREEZE` else `THAW`.
- `freeze_partition verb names...` -> three ordered buckets, resetting all three first. `freeze`
  wants the running state, `thaw` the frozen one. A name absent from the inventory is
  **vanished**; a present name not in the wanted state is **skipped**; the rest are **act**.
  Order is the order given, not inventory order.
- `select_all`: every inventory name in inventory order (empty inventory -> empty, not unset).
- `select_names names...`: keeps the given order and duplicates. If any name is unknown, nothing
  is selected: stderr `<tool>: not a running or frozen container: <unknown names, space-joined>`,
  `  Run '<tool> list' to see what is available.`, then `FREEZE_LIST_NOTE` if non-empty, exit 1.
  One unknown name is fatal even when the others exist and only the unknown ones are named.

### 3.4 Table printing (L417-427)

`print_table names...`: header `printf '  %-34s %-8s %s\n' NAME STATE <table_header>`, then for
each name present in the inventory (unknown names add no row)
`printf '  %-34s %-8s %s\n' name state <table_row i>`. Engine columns are padded **including the
last one**, so lines end in trailing spaces; goldens must preserve them.

### 3.5 Selection by target (section 2.3 step 7)

`--all` -> `select_all`; `--ccy` / `--network` / `--github|--token|--ssh-key` / `--bridge` ->
engine selectors (4.4, 5.4); names -> `select_names`. The CLI turns an engine selector's non-zero
return into `exit 1` ("`set -e` does not apply to a command in a condition", `PF` L911-918).

### 3.6 `do_list` (`PF` L647-674, `LF` L648-687), stdout

```
=== frozen (<n>) ===
  (none)                                  # when n=0
<print_table of the frozen entries>       # else, then:

  Thaw them all with: <tool> thaw --all   # only when n>0

=== running (<n>) ===
  (none)  |  <print_table of the running entries>
```

"frozen" = entries in the frozen state; **everything else** counts as running (the inventory holds
only those two states). `lxcfreeze` appends, only when both the inventory **and** the
unreadable list are empty: a blank line, `  LXC is installed and has no running or frozen containers.`,
`  (A STOPPED container is not counted — see 'sudo lxc-ls -f'.)`.

### 3.7 `do_action verb` (L681-763)

1. empty `SELECTED` -> `die "nothing matched — the selection is empty.` newline
   `  Run '<tool> list' to see what is running and what is frozen."`
2. partition. Stderr: if vanished non-empty `Skipped — no longer present (gone since the inventory was read): <names>`; if skipped non-empty `Skipped — not currently <wanted state word>: <names>`
   (the word is the engine's, e.g. `running`, `paused`, `RUNNING`).
3. nothing to act on -> stderr `Nothing to do — every selected container is already in the requested state.`, return 0.
4. **dry run**: stdout `DRY RUN — <n> container(s) would be <FROZEN|THAWED>:` then `print_table`
   of the act set to stdout; return 0.
5. else stderr `<n> container(s) being <FROZEN|THAWED>:` then the table (stderr). **No
   confirmation prompt, deliberately.**
6. each target in order: `out="$(hook_act verb name 2>&1)"`; success -> stderr `  ✓ <name>`;
   failure -> `  ✗ <name> — <out>` (combined stdout+stderr of the hook, trailing newlines
   stripped) and count it. Every target is attempted; a failure never stops the batch.
7. after a **freeze**: stderr blank line, `  Thaw them with: <tool> thaw <act names space-joined>`
   and, if `FREEZE_FREEZE_NOTE` is set, a blank line and the note. After a thaw nothing (bug 7).
8. failures>0 -> stderr `<tool>: <k> of <n> container(s) failed — see above.`, return 1; else 0.

### 3.8 Group menu and drill-down

`freeze_menu_row key description members...` appends `key` and the label
`printf '%-38s %s' description target_effect(members)`. An empty group still gets its row
("nothing to do").

**`pick_target`** (L458-524) returns the chosen key on stdout or the word `quit`:

1. Terminal guard: `[ ! -t 2 ] || [ ! -r /dev/tty ]` -> `die "no terminal to ask on — name a target explicitly: <HINT>.` newline `  See '<tool> --help'."` (bug 12).
2. rebuild keys/labels via `hook_menu_rows`; zero rows -> `die "internal error: the menu has no rows to offer."`
3. if `fzf` is on PATH: labels one per line on fzf's stdin, argv exactly
   `fzf --height=50% --reverse --prompt=<tool>:<space> --header=ENTER acts on the chosen group — ESC to quit`
   (prompt is `"<tool>: "`). Non-zero fzf exit -> `quit` (bug 2). Output line matched to a label
   by whole-string equality -> its key; no match -> `die "internal error: chosen entry not found."`
4. else numbered: stderr `What to act on:` then `printf '  %2d) %s\n'` per label; loop with
   attempt counter starting at 1: prompt (stderr, no newline) `Choice [1-<n>, q to quit]: `, read a
   line from **`/dev/tty`**; EOF -> stderr newline, `quit`; `q`/`Q`/`quit` -> `quit`; a string of
   digits in 1..n -> that key; anything else (empty, non-digits, out of range) -> stderr
   `  Not one of the options — enter a number between 1 and <n>, or q.`, attempt++, and when
   attempt > 3 `die "giving up after 3 attempts — nothing changed."` (so three bad replies).

**`drill_into_group names...`** (L578-675) returns the chosen names, one per line, or non-zero for
back/cancel:

1. empty group -> return 1 silently, before any terminal check.
2. same terminal guard and `die` as above.
3. rows: first `printf '%-34s %s' '*all*' target_effect(group)`; then for each name that resolves
   in the inventory, `printf '%-34s %-8s %s %s' name state <table_row i> row_verb(name)`. `rows`
   is the list actually printed (a non-resolving name is skipped and must not shift numbering).
4. fzf path: argv `fzf --multi --height=60% --reverse --prompt=In this group:<space> --header=ENTER on the first row acts on all — TAB picks specific ones, ESC goes back`.
   Non-zero or empty selection -> return 1 (back). For each selected line, first word == `*all*`
   -> whole group (in group order) and return 0; else collect first words; none -> back.
5. numbered: stderr `In this group:` then numbered rows (`  %2d) %s`); prompt
   `Choice [ENTER or 1 for all, numbers for specific, b to go back]: ` from `/dev/tty`; EOF ->
   newline + back; `b B back q Q quit` -> back; empty / `1` / `a A all` -> the whole group;
   otherwise `parse_member_choice reply rows...`; failure -> stderr two lines
   `  Not a valid choice — 1 (or ENTER) for all, 2-<total+1> for` and
   `  specific containers, or 'b' to go back.`, same 3-try limit and giving-up `die`.

**`parse_member_choice reply rows...`** (L550-576): split on commas and whitespace; every token
must be all digits in 2..(len(rows)+1), row `k` addressing `rows[k-2]`; any bad token rejects the
**whole** reply; no tokens rejects; result in typed order, duplicates kept (bug 4). `1`, `0`, `-1`,
`abc`, `2abc`, empty, blanks, `2,abc`, `2,0`, `2,99`, `99` are all refused.

### 3.9 `interactive_loop` (L768-823)

```
loop:
  hook_refresh
  inventory empty            -> stderr "No running or frozen containers."; return 0   (drops status, bug 8)
  key = pick_target
  key == quit                -> return status
  hook_select key:
     2 (GONE)                -> stderr blank line; continue
     other non-zero          -> die "the select hook failed (status <n>) resolving '<key>'"
  selection empty            -> stderr "Nothing in that group." + blank; continue
  picked = drill_into_group selection...   (non-zero -> stderr blank; continue)
  do_action(ACTION or infer_action); non-zero -> status = 1
  stderr blank line
```

The inventory is re-read on every pass so labels describe the machine now.

---

## 4. podfreeze: the Podman engine (`PF`)

### 4.1 Preflight

`assert_podman`: `podman` not on PATH -> `die "podman is not installed.` / `  It is declared in playbooks/imports/play-podman.yml. Deploy it with:` / `    ansible-playbook playbooks/imports/play-podman.yml`
/ `  Do NOT install it by hand."` Rootless; never escalates; no sudo anywhere.

### 4.2 `load_inventory` (L291-364) — two queries, field separator `|`

1. `podman ps --all --format "{{.Names}}|{{.State}}|{{.Networks}}"` (stdout and stderr merged,
   bug 1). Failure -> `die "podman ps failed: <output>"`. Per line, `IFS='|' read name state nets`:
   skip an empty name; keep only state `running` or `paused` (everything else, `exited`,
   `created`, a warning line, is dropped); networks empty -> the word `none` (stored verbatim
   otherwise, `podman, appnet` included); `IS_CCY[name]=1` if the name matches
   `^.+_(yolo|browser)(_[0-9]+)?$` (needs a project prefix; `yolo`, `_yolo`, `proj_yolo_x`,
   `proj_yoloo`, `proj_browser_` are **not** sessions).
2. `podman ps --all --filter label=ccy=true --format "{{.Names}}|{{index .Labels \"ccy-github\"}}|{{index .Labels \"ccy-token\"}}|{{index .Labels \"ccy-ssh-keys\"}}"`.
   Failure -> `die "podman ps --filter label=ccy=true failed: <output>"`. Per line
   `IFS='|' read name github token sshkeys extra`: skip empty name; **non-empty `extra`** ->
   `die "a CCY identity label on '<name>' contains '|', which\n  makes its labels ambiguous to parse. Rename the offending SSH key or token\n  config so it has no '|', then relaunch that session."`; set `IS_CCY=1` and `HAS_LABELS=1`
   (the label is authoritative over the name pattern, and these maps may hold names **not** in
   the inventory, e.g. exited sessions: they are in the maps but in no group); store github /
   token / ssh-keys unless empty or the literal `none` (a labelled session with `none` on every
   axis therefore has empty identity maps but `HAS_LABELS=1`, which is how it differs from a
   pre-3.40.0 session).

Both queries run on every refresh. The legacy image label `claude-yolo-version` is deliberately
not consulted (L175-180).

### 4.3 Columns

Header `printf '%-4s %-22s' CCY NETWORKS`; row `printf '%-4s %-22s' <CCY|-> <networks string>`.
`ccy_marker`: `CCY` if `IS_CCY[name]==1` (unknown name -> `-`).

### 4.4 Groups and selectors

- `ccy_names`: inventory names with `IS_CCY==1`, inventory order.
- `build_network_map` (rebuilt each refresh): for each inventory entry whose networks is not
  `none`, split on `,`, strip all spaces, skip empties, append the name under each network. A
  container on two networks joins both; `none` is never a network; rebuilding replaces.
- identity: `identity_value_of axis name` (axis `github|token|ssh-key`, else `die "internal error: unknown identity axis '<axis>'."`); `identity_matches` is whole-string for github/token (note:
  empty matches empty, guarded only by the caller) and **whole-word** for ssh-key (split on
  whitespace, no globbing, no prefix match, empty never matches); `identity_values axis`:
  distinct values present in the inventory, sorted (`sort -u`, locale-dependent, bug 9), ssh keys
  split into words (unquoted, bug 3); `identity_names axis value`: inventory-order members.
- `identity_axis_discriminates axis` (exit 0 = offer its rows): no values -> no; more than one
  value -> yes; exactly one value -> yes only if it covers fewer sessions than there are CCY
  sessions (**coverage, not cardinality**, `PF` L548-576).
- `unlabelled_ccy_names`: `IS_CCY==1 && HAS_LABELS!=1`, inventory order.
- `warn_identity_blind_spot` (stderr, only if any): `podfreeze: NOTE — <n> CCY session(s) carry no identity labels,` / `  so they are in no identity group and are NOT in this selection:` /
  one `    <name>` per session / `  They predate CCY 3.40.0. Use --ccy to cover every session regardless`
  / `  of identity, or relaunch them to make them selectable by identity.`
- `select_ccy`: `SELECTED = ccy_names`.
- `select_network NET`: run `podman network ls --format {{.Name}}` (failure -> `die "podman network ls failed: <out>"`); `NET` must be a **literal** whole-line member (`grep -qxF`); else stderr
  `podfreeze: no such network: '<NET>'`, `  Known networks:`, `    <name>` per line, **return 1**
  (does not exit; `SELECTED` untouched). Then `podman ps --all --filter network=<NET> --format {{.Names}}` (failure -> `die "podman ps --filter network=<NET> failed: <out>"`) and `SELECTED`
  = those names that are in the inventory, in the query's order (bug 13).
- `select_identity axis want`: `want` must be a member of `identity_values`. If not: stderr
  `podfreeze: no running or frozen CCY session has --<axis> '<want>'`; if the axis has **no**
  values: `  No session carries a <axis> label at all. Sessions started by` / `  a CCY older than 3.40.0 are unlabelled — relaunch to label them.`; else `  Known values:` and `    <v>` per value;
  then **`exit 1`** (even from the menu, bug 5). On success `SELECTED = identity_names` then
  `warn_identity_blind_spot`.

### 4.5 Menu hooks

- `menu_rows` order: `ccy` "all CCY (Claude) containers"; `all` "all containers"; for each
  network in use, sorted, `net:<net>` "network: <net>"; for each axis in the order `github token ssh-key` that `identity_axis_discriminates`, for each sorted value `id:<axis>:<value>`
  "<axis>: <value>" (the value may contain `:`; only the axis is split off).
- `select KEY`: `ccy`, `all`; `net:*` -> `select_network`, non-zero -> return GONE (2); `id:*` ->
  `select_identity`; anything else `die "internal error: unknown target key '<key>'."`
- `act VERB NAME`: `podman pause NAME` for freeze, `podman unpause NAME` for thaw (combined output
  becomes the `✗` text).

---

## 5. lxcfreeze: the LXC engine (`LF`)

### 5.1 Preflight (L456-493)

- `assert_lxc`: each of `lxc-ls lxc-info lxc-freeze lxc-unfreeze` missing is collected; any ->
  `die "LXC is not installed on this host — missing: <names space-joined>` / `  This is not 'no containers': nothing here can answer the question at all.` / `  LXC is declared in playbooks/imports/play-lxc-install-config.yml, which` / `  playbook-main.yml imports. Deploy it with:` / `    ansible-playbook playbooks/imports/play-lxc-install-config.yml` / `  Do NOT install it by hand."`
- `assert_sudo`: no `sudo` -> `die "no sudo on this host, and every LXC query needs root."`;
  `sudo -n true` succeeding (stderr discarded) -> silent; else stderr `==> LXC here is rootful: /var/lib/lxc is root-only, so even listing containers` and `==> needs root. Authenticating once for this run.`, then `sudo true` (interactive; its own prompt) and on refusal `die "sudo was refused, so no LXC state could be read.` / `  Nothing was changed. This is NOT 'no containers found' — the question was never asked, and reporting an empty machine here would be a guess."`

### 5.2 `load_inventory` (L506-578) — all through plain `sudo`, stderr merged into stdout (bug 1)

1. `sudo lxc-ls -1` (failure -> `die "lxc-ls failed, so the container list is unknown: <out>"`).
2. For each non-empty line `name`:
   - `sudo lxc-info -n <name> -s`; non-zero (the container vanished, or lxc-info refused) -> name
     goes to **`INV_UNREADABLE`**, no inventory entry.
   - `lxcf_parse_state`: first line matching `^\s*State:`; trimmed value; accepted words
     `RUNNING FROZEN STOPPED`, anything else (empty, `ABORTING`, no State line) is "no state". `grep`
     exiting >1 -> `die "grep failed (status <n>) reading the state from lxc-info output"`.
   - state `RUNNING`/`FROZEN` -> continue; `STOPPED` -> dropped silently (by design, named in
     `FREEZE_LIST_NOTE`); no state -> `INV_UNREADABLE`.
   - `sudo cat /var/lib/lxc/<name>/config` status captured: rc != 0 -> bridge label
     `(could not read)`; else `lxcf_parse_bridge`: first line `^\s*lxc\.net\.0\.link\s*=`, value
     after `=` trimmed (`#`-commented keys, `lxc.net.1.link` and the pre-2.1 `lxc.network.link`
     do not match; `lxc.include` is not followed, bug 17); empty -> `(no network)`.
   - `sudo lxc-info -n <name> -iH` status captured: rc != 0 -> `(unknown)`; else the first IPv4
     (`lxcf_parse_ipv4`: first dotted quad bounded by whitespace/line ends; IPv6 skipped; error
     text never matches; none -> **empty string**, the load-bearing blank: a RUNNING row with a
     blank address is a lease that expired during a freeze). A failed address probe never drops the
     container.
3. Append name, state, bridge label, address. After the loop, `warn_unreadable` (stderr, if any):
   `lxcfreeze: NOTE — <n> container(s) could not be read,` / `  so they are in NO group, NO count above and NO selection:` / `    <name>` per name / `  Either they went away between 'lxc-ls' and 'lxc-info' — a real race, and` / `  harmless — or 'sudo lxc-info -n NAME -s' fails for them. Run that to see` / `  which. A container of unknown state is never sent a verb.` The refresh runs
   this on every pass, so the note repeats each time round the loop (bug 25).

Constants: `BRIDGE_NONE="(no network)"`, `BRIDGE_UNREADABLE="(could not read)"`,
`IPV4_UNREADABLE="(unknown)"`; each contains whitespace, which a Linux interface name cannot, so
filtering on these exact strings is safe. `TABLE_COL_WIDTH=18` (must exceed the longest label, 16).

### 5.3 Columns

Header `printf '%-18s%-18s' BRIDGE IPV4`; row `printf '%-18s%-18s' <bridge label> <address>`
(no separator space between the two columns).

### 5.4 Groups and selectors

- `bridge_names`: bridge labels in use excluding the two non-bridge labels, `sort -u` (locale, bug 9).
- `select_bridge BR`: `BR` must be in `bridge_names`; else stderr
  `lxcfreeze: no running or frozen container is on any bridge.` (none in use) or
  `lxcfreeze: no running or frozen container is on bridge '<BR>'.` + `  In use right now: <names space-joined>`;
  **return 1**. On success `SELECTED` = inventory-order members whose label equals `BR`
  (rebuilt, not appended).

### 5.5 Menu hooks and act

- `menu_rows`: `all` "all LXC containers"; then for each bridge `bridge:<br>` "bridge: <br>" with its
  members. No per-container rows (the drill-down is that screen).
- `select`: `all`; `bridge:*` -> `select_bridge`, non-zero -> GONE (2); else `die "internal error: unknown target key '<key>'."`
- `act freeze NAME`: `sudo lxc-freeze -n NAME`.
- `act thaw NAME`: `sudo lxc-unfreeze -n NAME || return $?` — **load-bearing**: the renewal must
  not run, and the hook must not report success, after a failed unfreeze — then `renew_dhcp_lease`.
- `renew_dhcp_lease NAME` (L789-821): `sudo lxc-attach -n NAME -- nmcli -t -f DEVICE,TYPE device status` (both streams captured); failure -> print `thawed, but the DHCP lease was not renewed — nmcli failed: <out>`, return 1. Devices = first colon-field of lines whose second field is
  `ethernet`; none -> `thawed, but the DHCP lease was not renewed — NetworkManager manages no ethernet device`, return 1. For each device `sudo lxc-attach -n NAME -- nmcli device connect <dev>` (both streams captured); failure -> `thawed, but the DHCP lease on <dev> was not renewed — nmcli failed: <out>`, return 1. These messages become the `✗ <name> — ...` text, and make the
  thaw (which DID happen) count as a failure with exit 1 (bug 19).
- `FREEZE_FREEZE_NOTE` (printed after the thaw hint on a freeze):

```
  While frozen, the container answers nothing: every ssh session into it
  dies with the frozen TCP connection, and so does any agent socket forwarded
  over one. Thaw renews the DHCP lease, so the address comes back even after a
  freeze longer than the lease — the connections do not. Reconnect after thawing
  rather than trusting an old session.
```

---

## 6. External commands (every one becomes an `execx` call)

| engine | argv                                                                                                                                                           | notes                                                                       |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| podman | `podman ps --all --format {{.Names}}\|{{.State}}\|{{.Networks}}`                                                                                               | every refresh                                                               |
| podman | `podman ps --all --filter label=ccy=true --format {{.Names}}\|{{index .Labels "ccy-github"}}\|{{index .Labels "ccy-token"}}\|{{index .Labels "ccy-ssh-keys"}}` | every refresh                                                               |
| podman | `podman network ls --format {{.Name}}`                                                                                                                         | `--network` (CLI and menu row)                                              |
| podman | `podman ps --all --filter network=<NET> --format {{.Names}}`                                                                                                   | after the check above                                                       |
| podman | `podman pause <NAME>` / `podman unpause <NAME>`                                                                                                                | per target, combined output kept                                            |
| lxc    | `sudo -n true` / `sudo true`                                                                                                                                   | preflight                                                                   |
| lxc    | `sudo lxc-ls -1`; `sudo lxc-info -n N -s`; `sudo cat /var/lib/lxc/N/config`; `sudo lxc-info -n N -iH`                                                          | every refresh; 1 + 3 per running/frozen container                           |
| lxc    | `sudo lxc-freeze -n N` / `sudo lxc-unfreeze -n N`                                                                                                              | per target                                                                  |
| lxc    | `sudo lxc-attach -n N -- nmcli -t -f DEVICE,TYPE device status`; `... -- nmcli device connect DEV`                                                             | thaw only; **both streams must be pipes** (section 7.5)                     |
| both   | `fzf` (two argv shapes, 3.8); `command -v` for `podman`, `fzf`, `sudo`, `lxc-*`                                                                                | fzf optional (numbered fallback); picker draws on /dev/tty, labels on stdin |

The internal `grep`, `sort`, `awk`, `wc` calls are the bash's own plumbing and become Go code.

---

## 7. Files, environment, state, sudo, signals

1. **Reads**: `/run/.containerenv`, `/.dockerenv` (existence); `/var/lib/lxc/<name>/config` (via
   `sudo cat`, root-only); `/dev/tty` (numbered prompts). **Writes: none.** No config file, no
   record, no cache, no lock.
2. **Environment read**: `PATH` (tool lookups), `container` (guard, 3.2). Nothing else:
   not `HOME`, not `NO_COLOR`, not `TERM` (fzf reads it). `LXC_CONFIG_DIR=/var/lib/lxc` is an
   unconditional assignment, not an override point.
3. **Deployed paths**: tools `~/.local/bin/{podfreeze,lxcfreeze}` mode 0755; library
   `~/.local/lib/freeze/freeze-common.bash` mode 0644 (retired by the port).
4. **sudo/root**: podfreeze never; lxcfreeze for every engine call, with the single up-front
   authentication in 5.1 and plain `sudo` thereafter (bug 6).
5. **`lxc-attach` rule** (AgentNotes L1028-1035): `lxc-attach` chowns the file its stderr points
   at, so both streams must be pipes. `renew_dhcp_lease` captures with `$( ... 2>&1)`; a Go
   `execx.Cmd` with nil `Stdout`/`Stderr` captures through `bytes.Buffer`, i.e. pipes — never pass
   `os.Stderr` to these two calls.
6. **State and locking**: the freeze state lives in the engine (cgroup freezer, held by
   podman/LXC), not in the tools. There is no concurrency control: two runs race harmlessly (a
   second `pause` of a paused container is a `✗`). Frozen containers do not survive a reboot.
7. **Signals**: nothing is trapped. Ctrl-C at a numbered `read` kills the shell (130); in fzf it
   is a normal leave.
8. **Interactivity**: terminal = `-t 2` (stderr) plus a readable `/dev/tty`; reads come from
   `/dev/tty`, never stdin, so the tool still works with stdin piped.

---

## 8. User-facing strings the suites assert (keep verbatim)

From TL: `refuses to load` messages name the offending setting; `source it`; `<tool> list` and
`<tool> --help` in no-terminal/unknown-name errors; `not currently running` / `not currently paused`; `no longer present`; `✓ charlie`; `<tool> thaw charlie`; `DRY RUN`; `NAME` on stdout of a
dry run; `✗ charlie`; `cgroup freezer unavailable` (the engine's text); `2 of 2`; `already in the requested state`; `Reconnect after thawing`; `Thaw them with:`; `select hook failed`; `status 1`;
`Nothing in that group.`; `No running or frozen containers.`; `not reachable from in here`.
From TP: `podfreeze list` in the unknown-name error; `1 CCY session(s)`; `--ccy`; `3.40.0`;
`relaunch`; the `|` in the separator error; `Cannot connect to Podman socket` and `permission denied reading labels` passed through; `nosuchnet`/`appnet` in the network error. From TX:
`grep failed (status 2)`; `STOPPED` in the list note; `any bridge`; `thawed, but`; `ssh`,
`Reconnect`, `lease` in the freeze note; `lxc-freeze -n` / `lxc-unfreeze -n` / `nmcli ...` argv.

---

## 9. The oracle suites

### 9.1 What they are, and the finding that shapes the whole port

All three are **white-box unit suites of bash functions**: they `source` the library or the
tool's definitions, fill the inventory globals directly, and call functions. **No case runs a
tool as a command line.** Nothing exercises argument parsing, `usage`, `do_list`, `assert_podman`,
`assert_lxc`, `assert_sudo`, `lxc` `load_inventory`, `pick_target`'s or `drill_into_group`'s
terminal branches, or the real `main` flow. `TX` also asserts on the **text of function bodies**
(`declare -f freeze_hook_act | grep 'lxc-freeze -n'`), which has no meaning in Go. So, unlike
Plan 00191's `test-ccy-teams-command.bash`, these suites cannot "drive the Go binary": there is no
black-box suite to point at it. Section 12 says what parity means here.

Counts (run at this commit, all green): `TL` 240, `TP` 187, `TX` 116 = 543 cases.

### 9.2 Case map and disposition

| suite section (cases)                                                                                                                                                    | spec section | Go disposition                                                                                                                                                  |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `TL` contract: refuses to load without an engine (15)                                                                                                                    | 3.1          | adapt: `Config.Validate()` returns an error naming the field; same 5 required + 1 optional + identical-words                                                    |
| `TL` library not a program, errexit/nounset survive sourcing (4)                                                                                                         | 3.1          | retire (no sourcing in Go)                                                                                                                                      |
| `TL` decisions, run under BOTH vocabularies: index_of 7, count_in_state 5, infer_action 6, target_effect 11, row_verb 9, partition 11, select_all/names 3 (52 x 2 = 104) | 3.3          | port as table tests parametrised over `{running,paused}` and `{RUNNING,FROZEN}`                                                                                 |
| `TL` select_names unknown name (8)                                                                                                                                       | 3.3          | port                                                                                                                                                            |
| `TL` print_table 7, freeze_menu_row 7                                                                                                                                    | 3.4, 3.8     | port                                                                                                                                                            |
| `TL` no terminal is a named failure (7)                                                                                                                                  | 3.8          | port (terminal state injected, not probed)                                                                                                                      |
| `TL` do_action: empty 3, act/report 9, freeze note 4, thaw mirror 3, no-op 3, dry run 4, failing engine 5 (31)                                                           | 3.7          | port; the note case becomes a line-count assertion on stderr (keep the "an empty note adds exactly nothing" intent)                                             |
| `TL` parse_member_choice (24)                                                                                                                                            | 3.8          | port                                                                                                                                                            |
| `TL` interactive_loop (15)                                                                                                                                               | 3.9          | port with a scripted menu; the "BROKE vs GONE" pair becomes a typed `ErrGroupGone` vs any other error                                                           |
| `TL` freeze_assert_contract: each hook named (8)                                                                                                                         | 3.1          | retire (an `Engine` interface is checked by the compiler)                                                                                                       |
| `TL` assert_on_host: 4 signals + 2 default-path text checks (8)                                                                                                          | 3.2          | port 6; adapt 2 to assert the default marker paths are the real ones                                                                                            |
| `TL` constants (2)                                                                                                                                                       | 3.1          | port                                                                                                                                                            |
| `TP` fixture reset (3)                                                                                                                                                   | -            | retire (bash associative-array leakage)                                                                                                                         |
| `TP` index_of 7, ccy_marker 4, ccy_names 2, select_all/ccy 3, select_names 7                                                                                             | 3.3, 4       | port; **2 cases change deliberately** (duplicates, bug 4)                                                                                                       |
| `TP` build_network_map 8, network_names 2                                                                                                                                | 4.4          | port                                                                                                                                                            |
| `TP` count_in_state 8, target_effect 12, row_verb 10, infer_action 8                                                                                                     | 3.3          | port (also covered once more by `TL`; keep both as the same table under the podman words)                                                                       |
| `TP` identity_value_of 8, identity_matches 11 + 3 + 5, values 5, names 6, discriminates 6, unlabelled 3, blind-spot 5, select_identity 10 (62)                           | 4.4          | port; the glob case (5) is subsumed by "ssh-key `*` is a literal word"                                                                                          |
| `TP` load_inventory: states 4, name pattern 10, label pass 11, separator fatal 5, failed query 4 (34)                                                                    | 4.2          | port with an `execx.Fake` scripted for the two `ps` calls; the separator-fatal case also gains the trailing-\`                                                  |
| `TP` select_network: unknown returns 5, known 4, literal match 2, failed query 2 (13)                                                                                    | 4.4          | port; the 4 "known" cases adapt if selection moves to the inventory map (bug 13)                                                                                |
| `TP` constants (4)                                                                                                                                                       | 3.1, 4.2     | port 2 (sentinel, retry budget); the field-separator pair stays while \`                                                                                        |
| `TX` declared itself 6                                                                                                                                                   | 5            | port 4; retire 2 (STATE\_\* vs FREEZE_STATE\_\* are one constant)                                                                                               |
| `TX` parse_state 6 + 7, parse_bridge 9, bridge_label 9, bridge_names 3, select_bridge 9                                                                                  | 5.2, 5.4     | port                                                                                                                                                            |
| `TX` table hook 12, IPV4 column 14, IPV4 label 7, freeze note 4                                                                                                          | 5.3, 5.5     | port (column cut by the width constant, as the bash does)                                                                                                       |
| `TX` menu hook 6, select hook 5                                                                                                                                          | 5.5          | port                                                                                                                                                            |
| `TX` act hook text 9, preflight text 2                                                                                                                                   | 5.5, 5.1     | **replace** with argv assertions on an `execx.Fake` (freeze -> `sudo -n lxc-freeze -n X`, thaw -> unfreeze then both `nmcli` calls; preflight runs both guards) |
| `TX` act hook RUN 8                                                                                                                                                      | 5.5          | port (the unfreeze-fails-so-renewal-does-not-run guard is the most valuable case in the file)                                                                   |

Totals: port 498, adapt or replace 28, retire 17 (and 2 of the ported cases change on purpose, bug 4).

### 9.3 Gaps in the oracle (new cases the Go port must add; none exist today)

Argument parsing (each `die` of 2.2, order-dependence, `-h` mid-line, repeated flags); usage text
equals the embedded file; `do_list` both tools (empty, mixed, the LXC "no running or frozen" note
only when nothing was unreadable); `assert_podman`/`assert_lxc` (each missing tool named);
`assert_sudo` (cached, prompted, refused, no sudo); lxc `load_inventory` end to end (vanished
container, unknown state word, STOPPED dropped, config unreadable, address probe failed,
`sudo:` noise on stderr); `renew_dhcp_lease` each message; the numbered menus (every reply form,
EOF, three-strikes) and both fzf argv shapes with their output parse; exit codes; list-with-target
order (bug 22); the one-shot flows for every target kind; `--dry-run` into the interactive menu.
Expect 60 to 80 new cases.

---

## 10. Latent bugs

F = a Go port fixes it deliberately (and changes any pinned case in the same commit, with the
reason); K = kept for parity; the owner's decision, if any, is in section 15.

01. **F. stderr merged into parsed stdout** (`PF` L294, L331, L436, L459; `LF` L509, L529, L552,
    L564). A podman warning becomes a junk record in the label pass; a `sudo: unable to resolve host` line from `sudo lxc-ls -1` becomes a "container" that `lxc-info` then rejects, so it is
    listed as unreadable. Capture stdout and stderr separately; stderr only feeds error text.
02. **F. fzf failure is treated as a normal leave** (`FC` L476-482, L614-619): any non-zero
    (fzf crashed, exit 2, would not open the tty) is `quit`/`back` with exit 0. Only fzf's 1
    (no match) and 130 (Esc/Ctrl-C) are leaves; anything else is a failure.
03. **F. unquoted `for word in $value` in `identity_values`** (`PF` L529): the very glob bug fixed
    in `identity_matches` (L508) survives here, so an ssh-key label of `*` lists the working
    directory's files as keys in the menu. Split on whitespace only.
04. **F. duplicate names are acted on twice** (`select_names`, `2,2` in the drill-down): the
    second `pause` fails (`✗`, exit 1) and the label says `FREEZE 2` for one container. `TP` pins
    the duplicate as "what the tool does" (L357-359, L438-440). De-duplicate, first occurrence
    wins; change those two cases.
05. **F. `select_identity` exits the whole session** on a vanished value, even from the menu
    (`PF` L597), where networks and bridges re-prompt via GONE. Return GONE from the menu path.
06. **F. plain `sudo` after the one-time authentication** (`LF` L509, L529, L552, L564, L792,
    L815, L837, L839): a menu left idle past sudo's timestamp makes the next refresh prompt for a
    password with no explanation, inside a `$( )` capture. Re-run `sudo.Ready` at the start of each
    refresh and act batch, then use `sudo -n` throughout (the Plan 00191 convention). Failure text
    keeps the `NOT 'no containers'` sentence.
07. **F. "Thaw them with" and the freeze note describe attempts, not successes** (`FC` L742-753):
    a freeze where every target failed still prints `Thaw them with: ... <the failed names>` and
    the DHCP/ssh note. List only successes; print neither when there were none.
08. **F. status lost on an emptied machine** (`FC` L774-777): the loop returns 0 for "No running or
    frozen containers." even after an earlier failed act. Return the carried status.
09. **F. locale-dependent ordering** of networks, identity values and bridges (`sort`, `sort -u`).
    Go sorts bytewise; the order of menu rows stops depending on `LC_COLLATE`.
10. **K. usage mistakes exit 1, not 64** (every `die`). The repo convention (`term.ExitUsage`) is
    64\. Kept so cut-over changes no observable status; section 15 D3.
11. **K. quit semantics**: Esc at the group menu and EOF at a numbered prompt exit 0; a numbered
    prompt interrupted by Ctrl-C dies 130 with no newline. Go: SIGINT -> `ExitCancelled` (130)
    with a newline; everything else as above.
12. **F. terminal guard `-r /dev/tty`** (`FC` L461, L588): true when the device node is readable
    even with no controlling terminal; the later `read < /dev/tty` fails and is read as EOF, so
    the tool quits silently with exit 0. Open `/dev/tty` at the guard and fail with the
    no-terminal message.
13. **F. two sources of truth for a network group**: the menu counts members from
    `{{.Networks}}` (`build_network_map`) but the selection comes from a second
    `podman ps --filter network=` call. They can disagree (a race, or the filter's own
    semantics), so a row can promise `FREEZE 3` and act on another set. Select from the
    refreshed inventory map; keep `podman network ls` for the existence check; the fourth podman
    query goes.
14. **F. separator detection is incomplete** (`PF` L335-347): a ssh-key basename ending in `|`
    leaves `extra` empty and is silently mis-split. Split into exactly four fields and refuse any
    other count.
15. **K. a flag's value is the next token verbatim** (`--network -n`, `--github --ccy`): the
    result is a clean "no such network" / "unknown value" error afterwards. Kept.
16. **K. parsing is order-dependent**: `--bogus --help` dies, `--help --bogus` prints usage; a
    second target flag dies before a later `-h` is reached. Kept (and now pinned by new cases).
17. **K. `lxcf_parse_bridge` limits**: `lxc.include` is not followed, so a container whose network
    is configured in an included file reads `(no network)`; the first of repeated keys wins.
    Documented limitation, kept.
18. **F. IPv4 parse accepts `999.1.1.1` and `-o -m1` can yield two matches from one line.** Parse
    whitespace-separated fields with `netip.ParseAddr`, first IPv4.
19. **K. a thaw whose DHCP renewal fails reports `✗` and exits 1**, though the container IS
    thawed (and every thaw of a container without NetworkManager reports it). Documented
    behaviour (`docs/playbooks.md` L1237); messages kept verbatim; section 15 D6.
20. **K. fixed column widths** (`%-34s`, `%-38s`, `%-22s`, `%-18s`): names or labels wider than the
    column overflow and misalign the effect column. Cosmetic; widths kept, Go pads by rune.
21. **K. `-n` with no target** enters the interactive menu and previews each pick, though the
    help says "Print the resolved set and exit". Kept; the Go usage text is unchanged.
22. **F. `list` with a target is rejected after preflight and a full inventory load** (`PF`
    L893-896, `LF` L916-919): `lxcfreeze list --all` asks for a sudo password before saying it
    takes no target. Reject before any external command.
23. **K. `[ -n "$container" ]` guard** trusts the ambient variable: an unrelated exported
    `container` refuses on a host. Kept; the failure is loud and the cure is `unset`.
24. **N. library lookup** (`freeze_lib_path`, `PF` L120-164, `LF` L128-169): a candidate-path search
    with its own error message; disappears with the sourced library.
25. **F. the unreadable note repeats**: `warn_unreadable` runs inside every `load_inventory`, and
    entering the menu loads twice (`LF` L914 then the loop's refresh), so a real unreadable
    container is announced twice on start. Load once per pass; warn once per load.
26. **F. startup loads twice** (`PF` L891 then the loop refresh): two podman round trips (and, for
    lxcfreeze, up to `1 + 3N` sudo calls twice) before the first menu. The first load serves the
    one-shot paths; the menu reuses it for its first pass.

---

## 11. Proposed Go design

### 11.1 Layout (on `src-go/` as it stands)

- `cmd/podfreeze/main.go`, `cmd/lxcfreeze/main.go`: **two binaries**, thin mains
  (`os.Exit(podfreeze.NewApp().Run(os.Args[1:]))`) like `cmd/ccy-sessions`. Reasons: the two plays
  stay independent (either alone yields a working tool, as today); no `argv[0]` dispatch to break
  under a copy or symlink; the lxc binary never needs podman and vice versa. Cost: two small builds.
- `internal/freeze/` — the library, engine-neutral: `Config` (tool name, state words, notes, hint,
  with `Validate`), `Inventory` and `Entry`, the pure decisions (3.3), `Partition`,
  `ParseMemberChoice`, table and label formatting (3.4, 3.8), `DoAction` (3.7), `List` (3.6),
  `Loop` (3.9), the group menu and drill-down over a `Chooser` interface, and `Engine`:

```go
type Engine interface {
    Preflight(ctx context.Context) error
    Load(ctx context.Context) (Inventory, error)         // one refresh; owns its columns and warnings
    Groups(inv Inventory) []Group                         // key, description, members
    Select(ctx context.Context, inv Inventory, key string) ([]string, error) // ErrGroupGone when it went away
    Act(ctx context.Context, verb Verb, name string) (output string, err error)
    Columns() (header string)
    Row(e Entry) string
}
```

- `internal/podfreeze/` — podman engine: the two `ps` queries and parsers, `network ls`, the CCY
  name pattern, identity axes, `Select*`, CLI parsing for `--ccy --network --github --token --ssh-key`, embedded `usage.txt`, `NewApp`.
- `internal/lxcfreeze/` — LXC engine: the four state/config/address parsers, the three labels,
  `RenewDHCP`, `Select`, CLI parsing for `--bridge`, embedded `usage.txt`, `NewApp`.
  (A shared `internal/freeze/cli` parsing helper takes the target-mode and verb rules of 2.2 so
  they are written once; each engine registers its own flags.)

### 11.2 Reuse of existing packages

| need                                                                                                                             | package                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| -------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| every external call                                                                                                              | `execx.Runner` (`execx.Fake` in tests); `execx.Combined` for `pause`/`unpause`/`lxc-*` (combined text); `Run` with nil writers for stdout/stderr separately (bug 1)                                                                                                                                                                                                                                                                                                                                           |
| exit codes, plain stderr lines                                                                                                   | `term` (`ExitOK`, `ExitFailure`; `Printf` for the `<tool>: ...` lines). `term.PrintError` is **not** used: it adds a coloured `ERROR:` label these tools never print                                                                                                                                                                                                                                                                                                                                          |
| root                                                                                                                             | `sudo.Ready(ctx, why)` before each refresh and act batch, then `sudo -n` per call. **One addition:** a `Capture(ctx, argv...) (execx.Result, error)` that runs `sudo -n argv` and returns stdout and stderr without copying stderr to the terminal (`AsRoot` prints it; these callers must keep it for `INV_UNREADABLE` and the `✗` text)                                                                                                                                                                     |
| fzf                                                                                                                              | `ui.Picker` is single-select with the house style (border, Exit row, `q` abort). The freeze menus use a plain `--height --reverse --prompt --header` fzf and a `--multi` second screen. Add `ui.Picker.Choose(ctx, ChooseSpec{Prompt, Header, Height string; Multi bool; Rows []string}) ([]string, error)` emitting exactly the legacy argv, with the same `ErrLeave` mapping (1, 130 leave; others fail) and `ErrFzfMissing` -> numbered fallback. House style for these tools is a follow-up decision (D2) |
| numbered prompts                                                                                                                 | a small `ui` line reader over an injected `/dev/tty` opener (EOF -> `io.EOF`), so tests feed scripted lines                                                                                                                                                                                                                                                                                                                                                                                                   |
| `gum`, `systemd`, `tmuxccy`, `registry`, `agentbus`, `declare`, `gitx`, `marker`, `screen`, `transcript`, `restore`, `sessprobe` | not used                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |

### 11.3 App environment (the test seams)

`freeze.Env{Out, Err io.Writer; Getenv func(string) string; Stat func(string) (os.FileInfo, error) (container markers); LookPath func(string) (string, error); OpenTTY func() (io.ReadWriteCloser, error); IsTerminal func(any) bool; Runner execx.Runner; ContainerEnvPath, DockerEnvPath string}`.
Production `NewApp` fills the real values; tests fill fakes. Nothing is reachable from the command
line or the environment that loosens the host guard (3.2).

### 11.4 Build and tests

`go build -trimpath -buildvcs=false`; **no `-ldflags -X`**: unlike `ccy-teams` the freeze tools need
no checkout path. Test-first (the TDD hook), no discarded errors, no `//nolint`, coverage floor via
`scripts/qa-go.bash`. Every flow is tested through `execx.Fake` and a scripted chooser, without a
terminal.

---

## 12. Parity strategy (how "the existing suite is the oracle" is met here)

Because the suites are white-box (9.1), parity has three legs, in this order:

1. **Port the 543 cases** by the table in 9.2 into `go test` table tests, parametrised where the
   bash ran twice (the two state vocabularies). This is the decisions oracle. Each retired case is
   listed; each changed case (bug 4, 13) names its reason in the commit.
2. **Golden capture from the bash, before it is deleted.** One small, temporary harness
   (`scripts/freeze-golden-capture.bash`, retired in the cut-over commit; it is test tooling for a
   program being removed, and the bash line-in-the-sand allowlist counts it as such) runs the
   **shipped** tools under fakes on `PATH` (`podman`, `sudo`, `lxc-ls`, `lxc-info`, `lxc-freeze`,
   `lxc-unfreeze`, `lxc-attach`, `nmcli`, `fzf`, driven by answer files) and a `script -qec`
   pseudo-terminal for the numbered prompts. The container guard is bypassed by the harness, never
   by the tools: `lxcfreeze` is sourced (its `BASH_SOURCE == $0` guard) and `assert_on_host` is
   redefined before `main` runs; `podfreeze` has no `main`, so the harness runs a **copy** with its
   one bare `assert_on_host` line replaced by `:` (the existing `TP` boundary technique, the
   shipped file untouched). It records stdout, stderr and exit status per scenario into
   `src-go/internal/freeze/testdata/golden/`. Scenarios (about 40): each usage error; `--help`;
   `list` (both tools, empty, mixed, unreadable); every target kind one-shot (act, skip, vanish,
   nothing-to-do, dry run, partial failure, all failed); each preflight failure; the menu with fzf
   absent (every reply form, EOF, three strikes, drill-down, GONE) and with a fake fzf (both argv
   shapes). Go replays the same inputs through `execx.Fake` and a scripted tty and compares bytes,
   including trailing spaces. Differences that fix a numbered bug (1-9, 12-14, 18, 22, 25, 26) are
   recorded as expected deviations in the golden file's header, not silently tolerated.
3. **Host acceptance** (the only place a real engine exists): a plan-local `acceptance.bash`
   captures, **before** the cut-over deploy, `podfreeze list`, `podfreeze freeze --all -n` and the
   `lxcfreeze` equivalents into `untracked/`, and diffs them against the Go binaries' output after
   `meta-deploy`; then freezes and thaws one throwaway container per engine with each binary.
   This also settles what no fake can: that `podman ps --format` and `lxc-info` emit what the
   parsers expect (the gap `TP` and `TX` both state).

---

## 13. Cut-over

One feature branch, one merge, after full QA and review (Plan 00192 Phase 3):

1. Add Go packages, mains and tests; `scripts/qa-go.bash` green; golden replay green.
2. Rewrite `tasks/deploy-freeze-lib.yml` as `tasks/deploy-freeze.yml` taking `freeze_tool`
   (`podfreeze` or `lxcfreeze`): `dnf` fzf (kept, tag `packages`); ensure the scratch build
   directory; `go build -trimpath -buildvcs=false -o ~/.cache/ccy-go-build/<tool> ./cmd/<tool>`
   with `chdir: "{{ root_dir }}/src-go"`, `become: false`, `HOME`, `GOTOOLCHAIN: local`,
   `GOFLAGS: -mod=readonly`, `GOCACHE` exactly as `play-claude-yolo.yml` L891-908 does for
   `ccy-teams` (`changed_when: false`, the `copy` reports the change); `copy` the built file to
   `~/.local/bin/<tool>` (0755, user-owned) with `remote_src: true`, so an unchanged source reports
   ok; remove `~/.local/lib/freeze/freeze-common.bash` and the empty `~/.local/lib/freeze`
   (`state: absent`). The old `copy` of the bash tool in each play is deleted.
3. Each play: add `- import_playbook: ../../play-golang.yml` above its play (the toolchain's one
   source of truth, imported by `play-claude-yolo.yml` L5 for the same reason), include the new task
   file with `freeze_tool`, keep the `podman-freeze` removal in `play-podfreeze.yml`. Both binaries
   land at the **same paths**, so muscle memory, PATH and docs examples are unchanged.
4. Delete `files/home/.local/bin/{podfreeze,lxcfreeze}`, `files/home/.local/lib/freeze/`,
   `scripts/test-{freezelib,podfreeze,lxcfreeze}.bash` and the golden harness; remove the three
   gates from `qa-all.bash` (`qa-go.bash` covers the new code) and the freeze pair from
   `qa-deployed-drift.bash` L232 (and its explanatory paragraph); remove the three entries from
   the bash ratchet allowlist (Plan 00192 Task 1.3); update `docs/playbooks.md`,
   `CLAUDE/ContainerEngines.md`, `CLAUDE/QA.md`, `src-go/CLAUDE.md` (its tool list and
   "ccy-teams also gets `-X`" line stay accurate; add the freeze tools), `CLAUDE/AgentNotes.md`
   L466/L475 (the bug-class rows cite bash functions that no longer exist).
5. `CLAUDE/Plan/meta-deploy.bash`: add the plan (or the two plays) to `PLANS` in the same commit
   and tell the owner `./CLAUDE/Plan/meta-deploy.bash` will rebuild and install both binaries and
   remove the library. Never a hand install.
6. A new deploy on a host that still has the old bash tool replaces it in place (`copy` is an
   atomic rename; a running bash instance finishes on its open file). A host that has the Go
   binary but not the library is fine: nothing sources it.

### 13.1 What carries over unchanged

Nothing needs migrating, because the tools keep **no state and no configuration** (section 7.1):
no record, cache, lock or config file is read or written. The only durable facts are in the
engines: which containers are paused or frozen is held by podman and LXC, so containers frozen by
the bash tool are thawed by the Go one (and the reverse) with no step in between. The CCY labels
(section 1) are written by the launcher and read live on every refresh. The only things the cut-over
changes on disk are the two binaries (now ELF, same paths and mode) and the removed library.

---

## 14. Behaviour the port must not lose (checklist for review)

Derived verb; no confirmation prompt; group menu first, members behind a drill-down whose first
row is "all"; a group row's label equals what choosing it will do (same function); three buckets
(act, skipped, vanished) reported separately; one unknown name is fatal; every target attempted
and each failure named with the engine's own words; exit 1 on any failure; `list` and `-n` on
stdout, all else on stderr; "could not tell" is never reported as "none" (`(could not read)`,
`(unknown)`, the unreadable note, `NOT 'no containers'`); stopped containers excluded by design and
said so; never run inside a container; the `unfreeze`-failed-so-no-renewal guard; the coverage (not
cardinality) rule for offering identity rows.

---

## 15. Decisions for the owner

- **D1. One binary or two.** Proposed two (11.1).
- **D2. fzf look.** Proposed legacy flags now (parity); the house style (`Exit` row, `q` abort,
  border) later, which needs a multi-select variant for the drill-down.
- **D3. Usage-error exit status.** Proposed keep 1 at cut-over (bug 10); moving to 64 is a
  one-line follow-up that changes an observable status.
- **D4. Template parsing vs `podman ps --format json`.** Proposed keep the `|` templates (identical
  argv, bug 14 closed in Go); JSON would remove the separator class entirely but changes the
  query and depends on podman's JSON field names across versions. A follow-up with the host
  acceptance run as its proof.
- **D5. `sudo.Capture`.** Proposed: add it to `internal/sudo` (11.2) rather than reaching around
  the package from the lxc engine.
- **D6. A successful thaw whose DHCP renewal fails** (bug 19). Proposed keep (documented); option:
  report it as a warning with exit 0.
