# R82: chat-client-info, the hole punch for flatpak info

## What was built

- `src-go/cmd/chat-client-info` (thin main) and `src-go/internal/chatclientinfo` (the app).
- It takes no arguments: any argument is exit 64 before anything runs.
- It runs one fixed argv table through `execx`, each call bounded to 30 s (`CallBound`, a
  constant):
  - `flatpak ps --columns=instance,pid,application`
  - `flatpak info <id>` for each client of the embedded `internal/matrixclients` list
  - `flatpak info --show-permissions <id>` for each of those clients
- `flatpak` is `/usr/bin/flatpak`, run with `CleanEnv`. The environment is `PATH=/usr/bin:/bin`
  and `LC_ALL=C`, plus `HOME` and `XDG_RUNTIME_DIR=/run/user/<uid>` taken from the password
  database.
- Values in the permissions' `[Environment]` section are printed as `<hidden>`.
- A client that is not installed is reported as "not installed", exit 0.
- Any other failure is exit 1, with flatpak's cause on stderr and nothing on stdout.
- No app id is spelt in the code or the tests: the ids come from the list. So the
  `matrix-client-named-by-hand` rule holds.

## Tests (red seen, then green)

- `app_test.go`:
  - the exact argv table, and a run that makes exactly those calls;
  - any argument refused;
  - no argv holds `run`, `enter`, `override`, `kill`, `update`, `uninstall`, `permission-set`,
    `make-current` or other mutating verbs, or a path;
  - a client not installed;
  - each kind of flatpak failure, and flatpak itself missing;
  - every call carries a deadline no further off than `CallBound`;
  - the full path and the clean, fixed environment;
  - environment values hidden.
- `hole_test.go`:
  - the app-id `Bash` denies and the `Read`/`Edit` data denies are still in
    `cc-teams-settings.json`;
  - `flatpak run --command=sh`, `flatpak enter`, `flatpak info` and a data-path `cat` naming any
    client are still refused by the Bash globs;
  - the command's own path passes every Bash deny;
  - the play installs it where `InstallPath` says.
- `cmd/chat-client-info/main_test.go`: an account that cannot be read, an argument refused, and a
  run that makes exactly `Table()`'s calls.
- `go test -race` passed on chatclientinfo, cmd/chat-client-info and matrixclients.
  golangci-lint reported 0 issues. Coverage: 94.4% (package), 80% (main).

## Deploy

- `play-claude-yolo.yml` builds it with `tasks/build-go-command.yml` into
  `/var/local/claude-code/chat-client-info` (root, 0755), beside `cc-claude`.
- The play was already queued in `meta-deploy.list`; its description now names R82.
- `scripts/go-binary-suites.tsv` has the row `chat-client-info qa-go`.
- No ccy version bump: the image and the launcher do not build this package. The reviewer agrees.

## Review

- qa-reviewer verdict: FIX-BEFORE-MERGE, with two should-fix findings and two nits
  (`261126-r82-qa-review.md`):
  - PATH and environment inherited from the caller;
  - a locale-dependent "not installed" match;
  - `[Environment]` values printed;
  - a weak main test.
- All four were fixed test-first before the first commit (4862b3b32).

## Open

- Not verified on a host: whether `flatpak ps` needs anything beyond `XDG_RUNTIME_DIR`, such as
  `DBUS_SESSION_BUS_ADDRESS`. This container has no flatpak. If the first host run prints an
  error from `ps`, add that variable to `env()`.
- `qa-changed.bash` results that are not this change's:
  - `ansible-syntax` could not run: no vault file in the container (issue #215, commented).
  - The `go` gate cannot run on arm64 (#426).
  - `qa-private-names` and therefore `ccy-version-bump` could not build, because of another
    agent's uncommitted `src-go/qa/privatenames` work.
