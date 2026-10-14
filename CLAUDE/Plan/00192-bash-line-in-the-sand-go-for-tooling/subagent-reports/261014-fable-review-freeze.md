# Adversarial review: `go-port/freeze` (freeze toolkit Go rewrite) before merge into F44

Scope: `git diff F44...go-port/freeze` (worktree at HEAD `d1f133f34`). Read: every production Go
file under `internal/freeze`, `internal/freeze/{app,cli,golden}`, `internal/podfreeze`,
`internal/lxcfreeze`, `internal/sudo`, `internal/ui/choose.go`, `internal/execx`; the three deleted
bash tools at `F44`; the plays, `tasks/build-go-command.yml`, `tasks/deploy-freeze.yml`, the plan
`deploy.bash`, `meta-deploy.bash`, `ACCEPTANCE-freeze.md`, `SPEC-freeze.md` sections 6, 7, 10, 13,
14, 15, `deviations.json`, the QA scripts and docs diffs. Ran: `go test` on every touched package
(all green), `go vet` (clean), `ansible-playbook --syntax-check` on the three plays (all parse; run
from the worktree config with a placeholder vault file, never executed).

Verdict: **do not merge yet.** Two findings change observable behaviour in ways no numbered bug
covers and one of them makes the owner's acceptance procedure fail on its own terms. Everything
else is should-fix or nit, and the port is otherwise careful, well-tested and faithful.

---

## Blockers

### B1. `lxcfreeze` now refuses to ask for a sudo password unless BOTH stdin and stderr are terminals; `ACCEPTANCE-freeze.md` step 3 redirects stderr and will fail

Evidence

- `src-go/internal/sudo/sudo.go:60`: `if !s.Term.IsTerminal(s.Stdin) || !s.Term.IsTerminal(s.Term.Err) { ... return ErrNotReady }`.
- `src-go/internal/lxcfreeze/engine.go:155-158`: `Load` calls `Sudo.Ready` and turns `ErrNotReady` into the fatal "sudo was refused, so no LXC state could be read".
- Deleted bash `files/home/.local/bin/lxcfreeze` (F44) L481-493: `sudo -n true || { log ...; sudo true || die ...; }`. Plain `sudo` prompts on `/dev/tty` and needs neither stdin nor stderr to be a terminal.
- `CLAUDE/Plan/00192-.../ACCEPTANCE-freeze.md:35-38`: `lxcfreeze list > ... 2> /dev/null` and `lxcfreeze freeze --all -n > ... 2> /dev/null` after the Go deploy. With stderr on `/dev/null`, `FileIsTerminal` is false, `Ready` prints its "no terminal ... Run sudo -v first" line into `/dev/null` and the run exits 1 with an empty stdout file. The before/after `diff` then reports a difference that is not the parser defect the doc says it would be. It only passes if sudo's timestamp happens to be cached from step 2.
- This is also a plain non-interactive regression: `echo | lxcfreeze list`, or any wrapper that pipes stderr, worked under bash and fails under Go. It is not one of the 26 numbered bugs in SPEC section 10 (bug 6 is about re-prompting after expiry, not about which fds must be terminals), and the golden deviation `lxc-sudo-needs-password-no-terminal` hides it because the capture ran with no terminal at all (`capture.py:93-96`, `has_terminal()` opens `/dev/tty`), so bash and Go both failed there and the stderr text was pinned.

Fix (either, preferably both)

1. `sudo.Ready`: decide "can ask" the way sudo itself does, by whether `/dev/tty` can be opened (the freeze `Env` already has `OpenTTY`; `sudo` can take an `OpenTTY func() (io.ReadCloser, error)` seam or a `CanPrompt func() bool`), not by `IsTerminal(Stdin) && IsTerminal(Err)`. Keep the stderr check only for printing the explanation. Add a unit test: stderr not a terminal, `/dev/tty` openable, password wanted -> `sudo -v` is run.
2. `ACCEPTANCE-freeze.md` steps 1 and 3: drop `2> /dev/null` (stderr carries the sudo explanation the doc tells the owner to look for), or run `sudo -v` immediately before step 3 and say why.

### B2. Ctrl-C mid-batch is reported as N container failures (exit 1), not a cancellation (exit 130), and in the menu it then dies on the refresh

Evidence

- `src-go/internal/freeze/action.go:107-125` (`actOn`): the loop never consults `ctx.Err()`.
- `src-go/internal/execx/execx.go:86-114`: `exec.CommandContext` with an already-cancelled context fails at `Start` with `context.Canceled`, returned as `fmt.Errorf("run %s: %w", ...)`.
- `src-go/internal/freeze/app/app.go:42`: `signal.NotifyContext(..., os.Interrupt)` cancels the context on Ctrl-C.
- `src-go/internal/freeze/errors.go:16-23`: `ErrSomeFailed` is tested before `context.Canceled`.

Trace: `podfreeze freeze --all` over six containers, Ctrl-C after the second `podman pause`. The terminal delivers SIGINT to the running `podman` too (it dies), the context cancels, and `actOn` goes on to print `✗ c3 — run podman: context canceled` four times, then `podfreeze: 5 of 6 container(s) failed — see above.`, exit 1. In the menu (`loop.go:51-56`) the pass returns `ErrSomeFailed`, the loop carries on, `Refresh` runs `podman ps` on the cancelled context and the session ends with `podfreeze: podman ps failed: run podman: context canceled`. Bash died 130 at the signal with the honest half-frozen state on screen. SPEC 7.7 says "Go: SIGINT -> ExitCancelled (130)"; the chooser honours that (`chooser.go:270`) but the act loop does not. `lxcfreeze thaw` adds a further lie: a cancel between `lxc-unfreeze` and the renewal prints "thawed, but the DHCP lease was not renewed — nmcli failed: run sudo: context canceled" (`renew.go:25`).

Fix: in `actOn`, before each `Act`, `if err := ctx.Err(); err != nil { return done, failed, err }` (change the signature to return the error) and have `doAction` return `ctx.Err()` after printing the thaw hint for what did succeed; in `pass`, return `(true, ctx.Err())` when the context is done before `Refresh`. `ExitCode` already maps `context.Canceled` to 130. Tests: a Fake whose `Act` cancels the context on the second call; assert two `✓`, no `✗`, the thaw hint names only the two, and `errors.Is(err, context.Canceled)`; the same through `Loop`.

---

## Should-fix

### S1. The `lxc-*`/`nmcli` ownership rows in `project_seam_commands` are documentation, not a gate (the known gap, assessed)

Evidence

- `src-go/qa/rules/table.go:40-45` adds `lxc-ls`, `lxc-info`, `lxc-freeze`, `lxc-unfreeze`, `lxc-attach`, `nmcli` -> `internal/lxcfreeze`.
- The real tree never builds an `execx.Cmd` for any of them: every call is `e.Sudo.Capture(ctx, "lxc-info", "-n", name, "-s")` (`engine.go:160,198,210,214,217,288`, `renew.go:23,32`). The rule's only hook is `isCmdType` on a composite literal or a `.Name` assignment (`seamcommands.go:117-160`), and `judgeProgram` stops at `sudo` because `internal/sudo` is its owner ("the owner's own program is trusted").
- The fixture `testdata/src/fix/internal/lxcfreeze/cmds.go` proves the rows against a `Cmd{Name: "lxc-info"}` that production never writes. `CLAUDE/GoProjectRules.md:11-13` says exactly this is not acceptable: "a rule that holds only against the original spelling of the defect is documentation, not a gate."
- Consequence: any package may today call `sudo.Capture(ctx, "lxc-freeze", "-n", x)` or `sudo.AsRoot(ctx, "nmcli", ...)` and the gate is green. `Capture`/`AsRoot`/`Run` are a generic run-anything-as-root API.

Proposed rule fix (DBF shape)

1. Add a `programRunners` table to `table.go`: `{pkg: "internal/sudo", recv: "Sudo", method: "Capture", argIndex: 0}`, same for `AsRoot` and `Run` (index into the variadic `argv`). Resolve the callee by `types.Info` (`calledFunc` already exists) so an alias or method value does not hide it.
2. In `runSeamCommands`, also visit `*ast.CallExpr`; when the callee is a runner, take `call.Args[argIndex]` (or the first element of a `...` slice argument) as the program expression and feed it to `checkProgram(pass, progExpr, restArgs, true, dynamicOK)`. The constant requirement, the wrapper walk (`sudo lxc-attach -n N -- nmcli` is itself a wrapper chain: `lxc-attach` should be added to `wrappers` with `until: "--"`, so the program judged is `nmcli`) and the owner table all apply unchanged.
3. Red fixtures: a package outside `internal/lxcfreeze` calling `sudo.Capture(ctx, "lxc-ls", "-1")` -> flagged; `sudo.Capture(ctx, prog)` with a run-time `prog` -> "not a compile-time constant" unless a `dynamicNames` row; `lxcfreeze` itself clean; a method value `f := s.Capture; f(ctx, "lxc-ls")` flagged.
4. Red on the real tree: expected zero findings today, which is the point; record the control plant (a `sudo.Capture(ctx, "nmcli", ...)` in `internal/freeze`) in `GoProjectRules.md`'s Control section as the other plants are.
5. Update the rule doc string and `GoProjectRules.md` to say the runner methods are judged, replacing "The owner's own program is trusted".

### S2. sudo timestamp expiry mid-batch becomes N misleading per-container failures

Evidence: `lxcfreeze/engine.go:187-193` (`runAct`) returns `FailureText` for any error; `sudo.Capture` wraps a `-n` refusal as `ErrNeedsPassword` (`sudo.go:81-83`) but `Act` never looks. `Load` does (`probe`, `engine.go:230`, fatal). A batch of ten freezes whose timestamp lapses after the third prints `✗ name — sudo: a password is required` seven times and "7 of 10 failed". Not reachable by the golden fakes (the harness caches sudo for the run).
Fix: in `Act`, `if errors.Is(err, sudo.ErrNeedsPassword) { return "", freeze.Die(<the BeforeAct refusal text>) }` so the batch ends with the one honest line, or have `Engine.Act` return a sentinel the library treats as "stop the batch". Unit test with a Fake that answers `sudo -n lxc-freeze` with the refusal on the second call.

### S3. Automate the "save the bash output BEFORE deploying" step, or the owner loses it

Evidence: `ACCEPTANCE-freeze.md:8-18` and the `meta-deploy.bash:67-71` comment both rely on the owner reading a comment before running a batch script whose entire design is "running it is the consent, no prompts" (`meta-deploy.bash:152-156`). Once `deploy.bash` has run, the bash tools are gone and step 3's comparison has no baseline.
Fix: `deploy.bash` detects a bash `podfreeze`/`lxcfreeze` at `~/.local/bin` (`head -c2 == '#!'`) and writes the four "before" captures itself under `untracked/freeze-acceptance/` before `plan_deploy_leg`; add an `acceptance.bash` that writes the "after" captures and diffs, so `meta-deploy.bash` renders a PASS/FAIL for this plan instead of nothing. Both scripts are plan-folder scripts (exempt from the bash ratchet). Remember B1 when writing the lxcfreeze capture lines.

### S4. `Entry.Data any` + `recordOf` is not strictly typed and fails silently

Evidence: `inventory.go:10-14` (`Data any`); `podfreeze/engine.go:76-82` and `lxcfreeze/engine.go:68-74` (`recordOf` returns a zero `record` on a failed type assertion). A zero record renders as `-`/`none` (podman) or `""`/`""` (lxc) with no error, so an entry built by the wrong engine, or a future refactor that changes the record type, degrades the table and the groups without a word. The owner's bar is "strictly typed".
Fix: make the inventory generic over the engine's record (`Inventory[R any]`, `Entry[R]`, `Engine[R]` with `Row(Entry[R])`), which also removes the `any` from `Engine.Row`; or, minimally, have `recordOf` panic with the engine name (an engine's own entries are the only ones it should ever see, so a mismatch is a programming error, not data).

### S5. `Config.Validate` is never called in production

Evidence: `config.go:46` defines it; only `config_test.go` calls it. The bash refused to load with a missing setting (`freeze-common.bash:101-122`); the Go `Config()` constructors are literals, so a future engine that leaves `StateFrozen` empty compares states against `""` with nothing saying so.
Fix: call `tool.Cfg.Validate()` at the top of `app.ExecuteClosing` (an internal error, exit 1), or delete it (YAGNI). Either; not a tested-but-unwired function.

### S6. `qa-changed.bash` statement fold: one of the two folds is ratchet gaming

Evidence: `scripts/qa-changed.bash:392` `PICKED=() PICKED_WHY=()` (two assignments packed onto one simple command so `Stmts` drops by one); `:375` the `continue` folded into the `if` condition (a real simplification). The report (`261014-freeze-sync-f44-sonnet.md:9`) mentions only the second. The allowlist record went 588 -> 586 statements; the `if` fold alone would have been 587 < 588. The measurer's own doc (`bashlimits/measure.go:14`) says statements are counted "so ';' cannot pack a program" — packing assignments onto one line is the same move by another syntax.
Fix: restore the two `PICKED=()` / `PICKED_WHY=()` lines; the record will still be lower than the base. Allowlist otherwise correct: three deleted-file entries removed, every other record only moved down, measurer version unchanged.

### S7. Deviations are each a real F-marked bug, with one caveat on the `lxc-help*` trio

Checked all 57 entries in `deviations.json` against SPEC section 10: bugs 1, 2, 4, 6, 7, 13, 14, 22, 26 are all marked F, and each `why` matches the bug's text. Caveat: `lxc-help`, `lxc-help-short`, `lxc-help-inside-container` pin a usage-text change ("You will be prompted once per run" -> "sudo is asked again if its timestamp has expired") under bug 6. That is a correct consequence of the fix but it is a user-visible wording change the spec section 2.1 ("verbatim") did not list; add one line to SPEC 2.1 or 10.06 saying the lxcfreeze usage paragraph changes with it, so the spec is the source of truth the golden claims it is. The wave-4 report also says "54 differ"; the file has 57 (the three help scenarios were added later): fix the number.

---

## Nits

- N1. `App`/`NewApp`/`Run`/`RunContext`/`tool()` are duplicated almost line for line in `podfreeze/app.go` and `lxcfreeze/app.go` (the lxc one adds `Stdin` and the sudo asker). One `app.New(env, chooser, tool)` returning a struct with those methods removes ~60 lines; the engine-specific part is only `tool()`.
- N2. Stringly-typed seams: `cli.Parsed.Mode` is compared with `"--all"`, `"--network"` literals in both engines' `SelectTarget`; menu keys are `"net:"+x`, `"id:"+axis+":"+value` and parsed back with `strings.Cut` (`podfreeze/select.go:63`, `lxcfreeze/select.go:60`). A typed `Group.Key` (small struct or an engine-defined type) would make the `Select` switch exhaustive and delete the "internal error: unknown target key" branches.
- N3. `Inventory.Entries()` copies the slice on every call (`inventory.go:39`); podfreeze `Groups` calls it through `ccyNames`, `networkMap`, `identityValues`/`identityNames`/`discriminates` roughly `4 + 3*(values+1)` times per menu redraw. Trivial at these sizes, but an `All() iter.Seq[Entry]` costs nothing and the owner asked for performance.
- N4. `lxcfreeze.Load` is `1 + 3N` serial `sudo -n` execs (`engine.go:160-182`), the same cost as bash. The Go port could probe containers concurrently (bounded `errgroup`) and cut menu latency by roughly N×; order is kept by indexing into a pre-sized slice. Parity is met; "high performance" is not improved where it was cheap to.
- N5. `lxcfreeze.App.plain` (`app.go:59-64`) forces `NO_COLOR=1` to stop `term.Say` colouring sudo's explanation. Fine, but it is a workaround for `term.Term` having colour the freeze tools never want; a `Term{Plain: true}` or a `Say` variant would say what is meant.
- N6. `Partition`'s `Vanished` bucket and the "Skipped — no longer present" line are unreachable from every production path: both selection routes (`SelectNames`, the drill-down's `screen.Names`) filter through the same inventory the act uses, and nothing re-reads between prompt and act. The bash comment (`freeze-common.bash:598-603`) said the same. Keep it (it is the defence for the day a refresh lands between the two), but the golden report's "not covered by the replay" framing should say it is unreachable, not merely uncovered.
- N7. `TTYChooser.readLine` leaks the blocked `ReadString` goroutine after a cancel (`chooser.go:264-268`). Harmless because the process exits, and the comment says so; noted only because a future caller that recovers from a cancel would get a stale line on the next prompt.
- N8. `tests/helpers/self_update/test_affected_plays.py:131-141` still uses `tasks/deploy-freeze-lib.yml` and `files/lib/freeze.bash` as synthetic fixture names. Harmless (it is a fixture, not a path the test reads) but it now reads as if the file still exists; rename to a neutral fixture.
- N9. `build-go-command.yml:50-56`: the `argv` is a Jinja list rendered through a folded scalar. It works under Ansible 2.19's native templating (the report says it was checked with the templar and the plays parse), but a reader cannot tell from the file that `argv` receives a list and not a string; a short comment saying so would save the next person the templar check.
- N10. `GOTOOLCHAIN: local` with `go 1.26` in `go.mod` relies on Fedora's `golang` package being >= 1.26 (`play-golang.yml` pins nothing). Pre-existing (ccy-sessions had the same), not introduced here.

---

## What was checked and found sound

Behavioural parity (read against the deleted bash, not the spec): exact-match selection
(`Inventory.IndexOf` is a map lookup; no prefix, suffix or empty match); one unknown name fatal;
de-duplication (bug 4); derived verb, `TargetEffect`, `RowVerb`, three buckets, the act loop's
per-target reporting and the "N of M failed" line; `list` layout; group order (CCY, all,
networks bytewise, identity axes that discriminate by coverage not cardinality); identity
word-match on ssh-key only; the unlabelled-session blind-spot note on both menu and CLI paths;
`--network` existence check kept, fourth podman query gone (bug 13); lxc parsers (`State:` first
line only, `lxc.net.0.link` not commented/not `.1`/not pre-2.1, first IPv4 by `netip`, `(could not read)` vs `(no network)` vs blank vs `(unknown)`); STOPPED dropped silently, unknown state word
unreadable and named once per load; unfreeze-failed-so-no-renewal guard with the renewal's own
three messages verbatim; both `lxc-attach` streams are pipes (`sudo.Capture` passes no writers;
`TestActGivesEveryCommandPipesNeverAFileOrTheTerminal`); the container guard with unloosenable
paths; numbered prompts read `/dev/tty` not stdin, EOF -> quit/back with a newline, three bad
replies -> give up (fatal at the group menu, back to the menu from the drill-down, matching bash's
accidental behaviour and documented); fzf label round-trip by whole-string equality; the
`*all*` sentinel; exit codes (usage 1 kept per D3, failures 1, quit/EOF 0, Ctrl-C at a prompt 130).

Deployment: `tasks/build-go-command.yml` runs every task `become: false` under a `become: true`
play, builds into the user's cache, `copy remote_src` checksum-compares so a second run is `ok`;
`-trimpath -buildvcs=false` keep the bytes stable; tags propagate through the nested
`import_tasks` (the dnf task carries both `podfreeze` and `packages`); `play-golang.yml` is
imported by both freeze plays and `play-claude-yolo.yml`; `~/.local/lib/freeze` removal is
`state: absent`; the `podman-freeze` removal is kept; binaries land at the old paths so PATH,
docs examples and the systemd units are untouched; nothing else on the host references the
library (grep of `files/`, `docs/`, `CLAUDE/`). Replacing the `play-claude-yolo.yml` line in
`meta-deploy.bash` with the plan entry is consistent with the rule in the script's header (a
plan's own `deploy.bash` is preferred over a bare play, and it runs that play first). All three
plays pass `--syntax-check`.

QA wiring: `go-binary-suites.tsv` rows `qa-go` for both tools; `qa-changed.bash` skips them and
`qa-go` runs whole on any `src-go/` change; `test-qa-changed.bash` asserts `check go` and no bash
suite for them; the three `qa-all.bash` gates and the drift pair are gone with their docs; golden
replay is inside `go test` so it is in the gate; `TestEveryScenarioHasARecordedGolden` and the
"pinned but identical" check keep the deviation list honest.

Docs: `docs/playbooks.md`, `CLAUDE/ContainerEngines.md`, `CLAUDE/QA.md`, `CLAUDE/AgentNotes.md`
(rows 4 and 12, the lxc-attach rule), `src-go/CLAUDE.md`, `CLAUDE/GoProjectRules.md`, the play
headers and `PLAN.md` Task 3.1 all describe the branch as it is. The only inaccuracies are B1
(acceptance step 3) and S7 (the 54/57 count and the unlisted usage-text change).

Golden coverage gaps, as asked: Ctrl-C at a prompt (unit tests only; the pseudo-terminal capture
cannot compare a signal death with an exit code; see B2 for the act-loop case that has no test at
all); the vanished bucket (unreachable, N6); a real engine (acceptance only); sudo expiry
mid-batch (S2, no test); a network that exists with no inventory member (dies "nothing matched",
same as bash, not pinned).
