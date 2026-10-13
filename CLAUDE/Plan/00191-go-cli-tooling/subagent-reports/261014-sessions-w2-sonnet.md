# ccy-sessions Go port (Task 4.1)

Commits: 577c6cb6 (port), c82a7d5c (suite Go modes), plus the plan/report commit.

## Suites (pass counts, 0 failures)

| Suite           | bash | go  |
| --------------- | ---- | --- |
| reboot          | 283  | 284 |
| take-over       | 73   | 31  |
| tmux-targets    | 18   | 20  |
| restore-askpass | 90   | 91  |

Go counts include a "binary builds" check; take-over adds a tmuxccy unit-test check in place of the library-only cases.

## Mode-conditional changes

- take-over: library sourcing and library cases are bash-only; Go mode runs `go test ./internal/tmuxccy/`.
- tmux-targets: Go mode scans the library and `panes.go`, and drives the binary on a real tmux. A pre-existing suite bug (bare-name check could never pass) was fixed in both modes.
- reboot: the compact text is read from `setgoing.go` in Go mode.
- restore-askpass: library function calls become `CCY_LIB=... ccy-sessions restore` in Go mode.
- Spec bugs fixed rather than ported: 9.1-9.6, 9.9, 9.10, 9.12-9.15 (9.13: the cc trampoline now says "cc exited").

## Shared package changes

- ui picker: fzf exit 1 or 130 gives ErrLeave; missing fzf gives ErrFzfMissing.
- execx: `Fake.OnFunc` for stateful scripted answers.
- restore: `LockSetGoing` creates its state directory.

## Gates

- `go vet`, `go test -race -cover` green; `internal/sessions` coverage 94.9%.
- golangci-lint: 0 issues in my packages; 6 goconst findings in `internal/teams`, pre-existing and out of scope.
- `qa-changed.bash`: bash-syntax and shellcheck passed; stopped at the semgrep check because semgrep is not installed (an IaC gap owned by `playbooks/imports/play-python.yml`; not installed by hand). The ruff, unit-test, ansible-syntax, docs and go checks listed before it ran; the suite step did not complete.

## Behavioural decisions

- verify-restore re-reads the manifest each poll.
- The "(the countdown had already ended)" message is dropped.
- Unreadable registry records are reported first.
