# sudo NOPASSWD probe fix

Cause: `sudo.Ready` treated a failing `sudo -n -v` as "needs a password" and ran `sudo -v`. `-v`
fails on hosts where only some sudoers entries are NOPASSWD, though commands run unprompted.

Fix (src-go/internal/sudo/sudo.go): when `sudo -n -v` fails, probe `sudo -n true`; if it
succeeds, `Ready` returns nil with no output and no `sudo -v`. Only when both fail does it go on to
the no-terminal refusal or the prompt (the "says why" text is unchanged and printed only then).
`-n -v` succeeding makes no further probe.

Other callers: every sudo use in src-go goes through `internal/sudo` (the project rules enforce it;
teams' flow `SudoReady` and lxcfreeze call `Ready`), so the one fix covers them. No other place
assumes `-n -v` failing means a password is needed.

Tests (internal/sudo/sudo_test.go): new red-then-green tests for the NOPASSWD world (no prompt,
no `sudo -v`) and for `-n -v` success (no `-n true` probe). Existing prompt/no-terminal tests now
script `-n true` as failing. Golden: the freeze golden machine models only cached/uncached/refused
sudo with `-n true` behaving like `-n -v`; a NOPASSWD scenario would need a bash-recorded capture,
so none was added.

Checks: gofmt clean, go vet, `go test -race ./...` pass, project rules driver clean.
golangci-lint not run (not on PATH). qa-all not run.
