## QA Review: R82 `chat-client-info` hole (uncommitted tree on v2-smoke)

**Verdict**: FIX-BEFORE-MERGE

### Should fix
1. **`flatpak` is found through the caller's `PATH`, and the caller's environment is passed on, so the "no other verb" claim is false.** The cause is `src-go/internal/chatclientinfo/app.go:122`, which gives `execx.Cmd{Name: flatpak, ...}` with no `CleanEnv` and no absolute path.
   - `execx.OS.Run` (`src-go/internal/execx/execx.go:177`) calls `exec.CommandContext(ctx, c.Name, …)`, which looks the name up in `PATH`. `secret.go:148` passes the parent's environment through unchanged.
   - So a seat can run `PATH=<dir>:$PATH /var/local/claude-code/chat-client-info`. That command line matches no deny. A shim named `flatpak` in that directory then receives `info <app id>` and can run anything with the id: `run --command=sh`, `enter`. The command becomes a way to pass the app id into a program the caller wrote.
   - The deny is already a plain string match that a determined agent can get past (`docs/agent-bus.md` says so). But `app.go:64-65`, the package doc (`app.go:4-8`), `REGISTER.md:1569-1578` and `docs/agent-bus.md:1087-1094` all claim a caller can never point it at another verb or app. That is not true.
   - **Fix:** run `/usr/bin/flatpak` by absolute path with `CleanEnv: true` and a minimal fixed environment (`PATH=/usr/bin:/bin`, `LC_ALL=C`, plus `HOME` and `XDG_RUNTIME_DIR`/`DBUS_SESSION_BUS_ADDRESS` if `flatpak ps` needs them). Add a test that asserts the `Cmd` the runner receives has an absolute `Name` and `CleanEnv`. No current test looks at `Name` or the environment: `TestNoArgvHoldsARefusedVerb` checks only `Table()`.
2. **Whether a client counts as "not installed" depends on the caller's locale** (`app.go:37`, `app.go:132`). `isNotInstalled` looks for the English words in flatpak's stderr, and `LANG`/`LC_*` are inherited. Under a translated locale, a missing client becomes exit 1 instead of a "not installed" line. The fix in item 1 (`LC_ALL=C` under `CleanEnv`) also closes this.

### Nits
3. **`--show-permissions` output is printed unfiltered** (`app.go:107-110`). Its `[Environment]` section shows every `flatpak override --env=` value. Today the repo sets only non-secret ones (`tasks/lightning-team-ca.yml:128-129`, the `SSL_CERT_*` values). A future token passed this way would reach the agent's transcript. Either note this in the package doc, or drop the `[Environment]` values.
4. **`TestRunReadsTheEmbeddedClients` is a weak check** (`src-go/cmd/chat-client-info/main_test.go:31`). It only asserts at least 3 calls. Comparing against `chatclientinfo.Table(list.Clients)` would tie `main` to the real list.

### Checked and clean
- **The argument refusal is real:** any argument means exit 64 before anything runs. Tested with `--help`, an app id and `""`.
- **Each call has its own 30 s limit:** `app.go:120`, tested.
- **Nothing reaches stdout on failure:** the whole report is built before any of it is printed.
- **Errors carry flatpak's cause to stderr**, and no error is thrown away.
- **Deny rules are untouched:** `cc-teams-settings.json` is not in the diff. `hole_test.go` checks the exact `Bash(*<id>*)`, `Read` and `Edit` deny strings for every embedded client, so narrowing any of them fails the test. It also matches the deny globs against the `run`, `enter`, `info` and data-path `cat` commands, and against the tool's own path.
- **Placement:** the tool is installed by `play-claude-yolo.yml`, the same play that deploys `cc-teams-settings.json` (lines 431-441). It sits beside `cc-project-dir` and `cc-claude`, uses `tasks/build-go-command.yml`, is root-owned with mode 0755, and the play creates the directory earlier (line 361). No new play was needed.
- **Naming:** `chat-client-info` says what it does.
- **Version bump:** none needed. Nothing under `files/var/local/claude-yolo/` changed, and the image does not build this package.
- **`go-binary-suites.tsv`:** the row is present (`chat-client-info	qa-go`), as `src-go/CLAUDE.md` requires.
- **`meta-deploy.list`:** the note is added under the existing `play-claude-yolo.yml` entry, worded correctly.
- **Docs:** `src-go/CLAUDE.md`, `CLAUDE/QA.md` and `docs/agent-bus.md` are updated. The R82 row moves from open to fixed. `FIXCOMMIT` is a placeholder the author still has to fill in.
- **Public repo:** no install-specific names. The only app ids are the public client ids already in the settings file.
- **Out of scope:** the `src-go/internal/lightning/lightning_test.go` hunk belongs to the other agent and was not reviewed.

### Mechanical gates
- **`go test -race -count=1` on both packages:** ok. `go vet`: clean.
- **`qa-docs.bash`:** exit 0. **`qa-bash-limits.bash`:** exit 0. **`qa-ansible.bash`:** exit 0.
- **Syntax check:** `qa-ansible-syntax.bash play-claude-yolo.yml` could not run here, because the vault password file is missing in this container. The changed hunk copies the existing `import_tasks` blocks beside it. CI or the host must run this check.
- **`plan-qa --sweep`:** 3 block findings, all in other plans (00244, 00262, 00238). None in 00265.
- **Not run:** `qa-all.bash` and `go-qa`/golangci-lint. A sub-agent may not run the full suite; that is the coordinator's job.
